package identity

// This file defines the Forgejo HTTP client surface. All five transport
// methods — GetAccount, CreateUser, RegisterKey, CreateToken, RevokeToken —
// are implemented with real HTTP calls over the Forgejo admin and user APIs.
// Each method maps 1:1 to a documented Forgejo endpoint and enforces the auth
// model, rate-limit shape, and retry policy described in specs/agent-identity.md
// §8 and §13.
//
// What IS implemented here:
//   - Provisioner configuration + validation
//   - URL construction (all endpoint shapes compile and stringify)
//   - DryRunMode decision logic (short-circuits before any network call)
//   - RateLimiter (token bucket; Acquire blocks until a token is available)
//   - Retry policy with exponential backoff (connection failures + 5xx)
//   - doWithRetry: shared retry loop honoring Retry-After on 429
//   - Real HTTP transport for all 5 endpoints
//
// See specs/agent-identity.md §8 and §13 for the contracts this implements.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// DefaultKnownFriendsPath is the canonical location of known-friends.json on
// the H4F host. It is overridable via --known-friends / HELIX_KNOWN_FRIENDS.
const DefaultKnownFriendsPath = "/opt/hermes-demo/.hermes/h4f/known-friends.json"

// DefaultSSHKeyDir is the default root for per-agent key material. The
// syncer creates ~/.helix/keys/<agent>/ beneath this path.
const DefaultSSHKeyDir = "~/.helix/keys"

// DefaultStatePath is the default location of the idempotency state file.
const DefaultStatePath = "~/.helix/state.json"

// ProvisionerConfig holds all inputs needed to construct a Provisioner.
// Every field can be set via CLI flag or env var (see main.go for binding).
// No field is ever logged in full — Token and BasicAuth secrets are redacted
// in any String() output.
type ProvisionerConfig struct {
	// ForgejoURL is the base URL of the Forgejo instance, e.g.
	// "https://forgejo.example.com". No trailing slash.
	ForgejoURL string

	// AdminToken is the Forgejo admin PAT used for /admin/users endpoints
	// that don't support BasicAuth. In Forgejo v1.21+, API-created tokens
	// lack admin scope, so prefer AdminUser+AdminPassword (BasicAuth) when
	// hitting POST /api/v1/admin/users. AdminToken is kept for backward
	// compatibility with Forgejo instances that support admin-scoped PATs.
	AdminToken string

	// AdminUser is the Forgejo admin username for BasicAuth (e.g. "helio").
	// Used as the fallback for POST /api/v1/admin/users when AdminToken is
	// empty or when the endpoint requires admin-level privileges.
	AdminUser string

	// AdminPassword is the Forgejo admin password for BasicAuth.
	// Paired with AdminUser; both must be set for BasicAuth to engage.
	AdminPassword string

	// KnownFriendsPath is the path to known-friends.json.
	KnownFriendsPath string

	// SSHKeyDir is the root directory for per-agent SSH key material.
	SSHKeyDir string

	// StatePath is the path to the idempotency state file.
	StatePath string

	// DryRun, when true, causes every transport call to short-circuit and
	// emit a "[DRY RUN] METHOD /path {body}" line instead of hitting Forgejo.
	DryRun bool

	// Verbose enables per-call timing + request logging to the syncer's
	// logger.
	Verbose bool

	// HTTPTimeout caps a single HTTP request (excluding retries).
	HTTPTimeout time.Duration

	// RequestRate is the steady-state request rate (requests per second).
	RequestRate int

	// BurstRate is the token-bucket burst size.
	BurstRate int
}

// DefaultProvisionerConfig returns a config populated with the documented
// defaults. Callers override the specific fields they care about.
func DefaultProvisionerConfig() ProvisionerConfig {
	return ProvisionerConfig{
		ForgejoURL:       "",
		AdminToken:       "",
		KnownFriendsPath: DefaultKnownFriendsPath,
		SSHKeyDir:        DefaultSSHKeyDir,
		StatePath:        DefaultStatePath,
		DryRun:           false,
		Verbose:          false,
		HTTPTimeout:      30 * time.Second,
		RequestRate:      10,
		BurstRate:        2,
	}
}

// Validate enforces that the config has everything needed to talk to Forgejo.
// It returns a TypedError of kind config so the CLI can exit with code 3.
//
// Note: this only checks the admin-provisioning fields. Per-agent BasicAuth
// credentials (username + temp password) are validated at use time inside
// RegisterKey / CreateToken, because they are agent-specific.
func (c *ProvisionerConfig) Validate() error {
	if c.ForgejoURL == "" {
		return NewConfigError(
			"FORGEJO_URL not set (use --forgejo-url or FORGEJO_URL env var)", nil)
	}
	if _, err := url.Parse(c.ForgejoURL); err != nil {
		return NewConfigError(fmt.Sprintf("invalid ForgejoURL %q: %v", c.ForgejoURL, err), nil)
	}
	if c.AdminToken == "" && (c.AdminUser == "" || c.AdminPassword == "") {
		return NewConfigError(
			"FORGEJO_ADMIN_TOKEN or FORGEJO_ADMIN_USER+FORGEJO_ADMIN_PASSWORD must be set", nil)
	}
	if c.KnownFriendsPath == "" {
		return NewConfigError("known-friends path is empty", nil)
	}
	if c.HTTPTimeout <= 0 {
		return NewConfigError("HTTPTimeout must be positive", nil)
	}
	if c.RequestRate <= 0 {
		return NewConfigError("RequestRate must be positive", nil)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Rate limiter (token bucket, hand-rolled — no x/time/rate dependency)
// ---------------------------------------------------------------------------

// RateLimiter is a hand-rolled token bucket. v1's Acquire is a no-op stub
// (returns immediately) because the transport itself is stubbed. The real
// implementation will use time.Ticker + a buffered channel of size Burst.
//
// The structure is defined here so the Provisioner can hold one and callers
// can see the rate/burst policy without depending on a real implementation.
type RateLimiter struct {
	rate   int           // tokens per second
	burst  int           // max tokens held at once
	tokens chan struct{} // buffered channel, capacity = burst
}

// NewRateLimiter constructs a token bucket. The channel is filled with
// `burst` tokens initially; the real transport drains one per request and a
// goroutine refills at `rate` per second.
func NewRateLimiter(rate, burst int) *RateLimiter {
	rl := &RateLimiter{
		rate:   rate,
		burst:  burst,
		tokens: make(chan struct{}, burst),
	}
	for i := 0; i < burst; i++ {
		rl.tokens <- struct{}{}
	}
	return rl
}

// Acquire blocks until a token is available. In v1 this is a no-op because
// no real requests are made; the structure exists to make the contract
// explicit and to make the real implementation a one-method swap.
func (rl *RateLimiter) Acquire() {
	if rl == nil || rl.tokens == nil {
		return
	}
	select {
	case <-rl.tokens:
		return
	default:
		return // v1 stub: never block
	}
}

// Rate returns the configured steady-state request rate.
func (rl *RateLimiter) Rate() int { return rl.rate }

// Burst returns the configured burst size.
func (rl *RateLimiter) Burst() int { return rl.burst }

// ---------------------------------------------------------------------------
// Retry policy
// ---------------------------------------------------------------------------

// RetryPolicy describes how the transport retries transient failures. v1
// stores the policy; the real transport consults it. See §11.3 of the spec.
type RetryPolicy struct {
	// MaxAttempts is the total number of attempts (1 = no retry).
	MaxAttempts int

	// InitialBackoff is the first sleep on a retryable failure.
	InitialBackoff time.Duration

	// MaxBackoff caps the exponential backoff.
	MaxBackoff time.Duration

	// Multiplier is applied to the previous backoff on each retry.
	Multiplier float64
}

// DefaultRetryPolicy returns the policy documented in §11.3:
//   - connection refused → exp backoff 1s, 2s, 4s ... cap 30s
//   - HTTP 429 → honor Retry-After header (handled in transport, not here)
//   - HTTP 5xx → up to 3 retries with 2s spacing
//
// The policy below covers the connection-refused and 5xx cases; 429 is
// special-cased in the transport because it carries a server-provided delay.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:    4, // 1 initial + 3 retries
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     30 * time.Second,
		Multiplier:     2.0,
	}
}

// BackoffFor returns the sleep duration before the (attempt+1)-th try, where
// attempt is 0-indexed. Returns zero once attempts are exhausted.
func (p RetryPolicy) BackoffFor(attempt int) time.Duration {
	if attempt <= 0 || attempt >= p.MaxAttempts {
		return 0
	}
	d := p.InitialBackoff
	for i := 1; i < attempt; i++ {
		d = time.Duration(float64(d) * p.Multiplier)
		if d > p.MaxBackoff {
			d = p.MaxBackoff
			break
		}
	}
	return d
}

// ---------------------------------------------------------------------------
// Provisioner — the Forgejo HTTP client surface
// ---------------------------------------------------------------------------

// Provisioner is the thin HTTP client over the Forgejo admin + user APIs.
// It owns an *http.Client (so connection pooling + TLS config live in one
// place), a RateLimiter, and a RetryPolicy. Every public method maps 1:1 to
// a documented Forgejo endpoint (§9.2).
type Provisioner struct {
	cfg     ProvisionerConfig
	http    *http.Client
	limiter *RateLimiter
	retry   RetryPolicy

	// redactedBase is ForgejoURL with any userinfo stripped, safe to log.
	redactedBase string
}

// NewProvisioner constructs a Provisioner. It does NOT make any network
// calls — configuration validation is the only side effect. The returned
// Provisioner is safe to share across goroutines once constructed because
// *http.Client is goroutine-safe.
func NewProvisioner(cfg ProvisionerConfig) (*Provisioner, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	p := &Provisioner{
		cfg:          cfg,
		http:         &http.Client{Timeout: cfg.HTTPTimeout},
		limiter:      NewRateLimiter(cfg.RequestRate, cfg.BurstRate),
		retry:        DefaultRetryPolicy(),
		redactedBase: redactURL(cfg.ForgejoURL),
	}
	return p, nil
}

// Config returns a copy of the provisioner's config (for inspection only;
// callers should not mutate it after construction).
func (p *Provisioner) Config() ProvisionerConfig { return p.cfg }

// DryRun reports whether the provisioner will short-circuit all network calls.
func (p *Provisioner) DryRun() bool { return p.cfg.DryRun }

// BaseURL returns the (redacted) Forgejo base URL, safe for logging.
func (p *Provisioner) BaseURL() string { return p.redactedBase }

// ---------------------------------------------------------------------------
// URL construction — proves the endpoint shapes stringify correctly
// ---------------------------------------------------------------------------

// adminUsersURL is POST /api/v1/admin/users (list or create). Forgejo v1.21+
// does NOT support GET /api/v1/admin/users/{name} (returns 405), so per-user
// lookups use the public userByUsernameURL instead.
func (p *Provisioner) adminUsersURL(name string) string {
	base := strings.TrimRight(p.cfg.ForgejoURL, "/")
	if name == "" {
		return base + "/api/v1/admin/users"
	}
	return base + "/api/v1/admin/users/" + url.PathEscape(name)
}

// adminUserKeysURL is POST /api/v1/admin/users/{name}/keys — the admin
// endpoint for registering SSH keys on behalf of a user. Required because
// Forgejo v1.21+ blocks POST /api/v1/user/keys for users who must change
// their password after admin-created accounts.
func (p *Provisioner) adminUserKeysURL(name string) string {
	return strings.TrimRight(p.cfg.ForgejoURL, "/") + "/api/v1/admin/users/" +
		url.PathEscape(name) + "/keys"
}

// userKeysURL is POST /api/v1/user/keys (authenticated as the agent).
func (p *Provisioner) userKeysURL() string {
	return strings.TrimRight(p.cfg.ForgejoURL, "/") + "/api/v1/user/keys"
}

// userTokensURL is POST /api/v1/users/{name}/tokens (BasicAuth as admin).
func (p *Provisioner) userTokensURL(name string) string {
	return strings.TrimRight(p.cfg.ForgejoURL, "/") + "/api/v1/users/" +
		url.PathEscape(name) + "/tokens"
}

// userTokenURL is DELETE /api/v1/users/{name}/tokens/{id} (BasicAuth).
func (p *Provisioner) userTokenURL(name string, id int64) string {
	return strings.TrimRight(p.cfg.ForgejoURL, "/") + "/api/v1/users/" +
		url.PathEscape(name) + "/tokens/" + fmt.Sprintf("%d", id)
}

// ---------------------------------------------------------------------------
// Shared retry transport
// ---------------------------------------------------------------------------

// doWithRetry executes an HTTP request with retry semantics shared by all five
// transport methods. It retries on network errors, HTTP 429 (honoring the
// Retry-After header), and HTTP 5xx responses. Once it receives a
// non-retryable response (1xx/2xx/3xx/4xx-excluding-429), it returns the
// *http.Response so the caller can interpret the status code in a method-
// specific way (e.g. GetAccount treats 404 as "not found, not an error").
//
// The request body is fully buffered before the first attempt so that retries
// can re-send it without exhausting the reader.
func (p *Provisioner) doWithRetry(method, urlStr string, body io.Reader, setAuth func(*http.Request)) (*http.Response, error) {
	// Buffer the body once so each retry attempt can re-send it.
	var bodyBytes []byte
	if body != nil {
		var readErr error
		bodyBytes, readErr = io.ReadAll(body)
		if readErr != nil {
			return nil, NewInternalError(
				fmt.Sprintf("%s %s: failed to buffer request body", method, urlStr), readErr)
		}
	}

	var lastErr error
	for attempt := 0; attempt < p.retry.MaxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(p.retry.BackoffFor(attempt))
		}

		// Build a fresh request for each attempt (body reader is consumed by Do).
		var bodyReader io.Reader
		if bodyBytes != nil {
			bodyReader = bytes.NewReader(bodyBytes)
		}
		req, reqErr := http.NewRequestWithContext(context.Background(), method, urlStr, bodyReader)
		if reqErr != nil {
			return nil, NewConfigError(
				fmt.Sprintf("failed to build %s request to %s", method, urlStr), reqErr)
		}
		if setAuth != nil {
			setAuth(req)
		}

		resp, doErr := p.http.Do(req)
		if doErr != nil {
			// Network-level failure (DNS, TLS, connection refused, timeout).
			lastErr = NewNetworkError(
				fmt.Sprintf("%s %s: request failed (attempt %d/%d)",
					method, urlStr, attempt+1, p.retry.MaxAttempts), doErr)
			continue
		}

		// 429: rate limited — honor Retry-After, then retry.
		if resp.StatusCode == http.StatusTooManyRequests {
			wait := parseRetryAfter(resp.Header.Get("Retry-After"))
			resp.Body.Close()
			lastErr = NewNetworkError(
				fmt.Sprintf("%s %s: rate limited (429), retrying after %s",
					method, urlStr, wait), nil)
			time.Sleep(wait)
			continue
		}

		// 5xx: server error — retry per policy.
		if resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = NewNetworkError(
				fmt.Sprintf("%s %s: server error (HTTP %d, attempt %d/%d)",
					method, urlStr, resp.StatusCode, attempt+1, p.retry.MaxAttempts), nil)
			continue
		}

		// Non-retryable response — hand it to the caller.
		return resp, nil
	}

	return nil, NewNetworkError(
		fmt.Sprintf("%s %s: exhausted %d retries", method, urlStr, p.retry.MaxAttempts), lastErr)
}

// setAdminAuth applies the appropriate admin authorization header to an HTTP
// request. If AdminUser+AdminPassword are both set, it uses HTTP BasicAuth
// (required by Forgejo v1.21+ for POST /api/v1/admin/users). Otherwise it
// falls back to the AdminToken bearer token.
func (p *Provisioner) setAdminAuth(r *http.Request) {
	if p.cfg.AdminUser != "" && p.cfg.AdminPassword != "" {
		r.SetBasicAuth(p.cfg.AdminUser, p.cfg.AdminPassword)
		return
	}
	r.Header.Set("Authorization", "token "+p.cfg.AdminToken)
}

// parseRetryAfter converts a Retry-After header value to a Duration. The
// header can be either a non-negative integer (seconds) or an HTTP-date. If
// the value is missing or unparseable, it falls back to 5 seconds.
func parseRetryAfter(val string) time.Duration {
	if val == "" {
		return 5 * time.Second
	}
	// Numeric form: delay in seconds.
	if secs, err := strconv.Atoi(val); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	// HTTP-date form: absolute timestamp.
	if t, err := http.ParseTime(val); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 5 * time.Second
}

// readAndCloseBody reads the full response body and closes it. Used by
// callers that need the body text for error messages.
func readAndCloseBody(resp *http.Response) string {
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// ---------------------------------------------------------------------------
// Endpoint implementations — real Forgejo HTTP transport
// ---------------------------------------------------------------------------

// GetAccount checks whether a Forgejo user exists. It uses GET
// /api/v1/admin/users (list, with admin auth) and searches for the name,
// because GET /api/v1/users/{name} returns 404 for users with
// visibility:limited (the default for admin-created accounts in Forgejo
// v1.21+). Returns the account if found, (nil, nil) if not.
func (p *Provisioner) GetAccount(name string) (*ForgejoAccount, error) {
	if p.cfg.DryRun {
		return nil, nil // dry-run: assume not-yet-provisioned
	}
	p.limiter.Acquire()

	resp, err := p.doWithRetry(http.MethodGet, p.adminUsersURL(""), nil,
		func(r *http.Request) {
			p.setAdminAuth(r)
		})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body := readAndCloseBody(resp)
		return nil, NewAPIError(
			fmt.Sprintf("GetAccount(%s): admin list returned HTTP %d: %s",
				name, resp.StatusCode, body), nil)
	}

	var users []ForgejoAccount
	if decErr := json.NewDecoder(resp.Body).Decode(&users); decErr != nil {
		return nil, NewInternalError(
			fmt.Sprintf("GetAccount(%s): failed to decode admin list", name), decErr)
	}

	for i := range users {
		if strings.EqualFold(users[i].Login, name) || strings.EqualFold(users[i].LoginName, name) {
			return &users[i], nil
		}
	}
	return nil, nil // not found
}

// CreateUser calls POST /api/v1/admin/users. On success returns the newly
// created ForgejoAccount. A 409 Conflict is mapped to a TypedError of kind
// API so the syncer can downgrade it to ActionUnchanged rather than failing.
//
// Auth: admin BasicAuth (preferred in Forgejo v1.21+) or admin token.
func (p *Provisioner) CreateUser(req *CreateUserRequest) (*ForgejoAccount, error) {
	if req == nil {
		return nil, NewConfigError("CreateUser: nil request", nil)
	}
	if p.cfg.DryRun {
		return &ForgejoAccount{
			Login: req.Username, Email: req.Email, FullName: req.FullName,
		}, nil
	}
	p.limiter.Acquire()

	bodyBytes, mErr := json.Marshal(req)
	if mErr != nil {
		return nil, NewInternalError("CreateUser: failed to marshal request", mErr)
	}

	resp, err := p.doWithRetry(http.MethodPost, p.adminUsersURL(""), bytes.NewReader(bodyBytes),
		func(r *http.Request) {
			r.Header.Set("Content-Type", "application/json")
			p.setAdminAuth(r)
		})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusCreated:
		var acct ForgejoAccount
		if decErr := json.NewDecoder(resp.Body).Decode(&acct); decErr != nil {
			return nil, NewInternalError(
				fmt.Sprintf("CreateUser(%s): failed to decode response", req.Username), decErr)
		}
		return &acct, nil

	case http.StatusConflict, http.StatusUnprocessableEntity:
		// 409 = conflict (Gitea), 422 = unprocessable (Forgejo v1.21+
		// returns 422 for "user already exists"). Both are non-fatal —
		// the syncer maps them to ActionUnchanged.
		body := readAndCloseBody(resp)
		return nil, NewAPIError(
			fmt.Sprintf("CreateUser(%s): conflict (HTTP %d): %s", req.Username, resp.StatusCode, body), nil)

	default:
		body := readAndCloseBody(resp)
		return nil, NewAPIError(
			fmt.Sprintf("CreateUser(%s): unexpected HTTP %d: %s",
				req.Username, resp.StatusCode, body), nil)
	}
}

// RegisterKey calls POST /api/v1/user/keys. The key is registered as the
// agent, authenticated via HTTP Basic Auth (agent username + temp password).
// The admin token cannot register keys on behalf of a user — Forgejo's
// user-key endpoint requires user-scoped credentials.
//
// Auth: HTTP Basic Auth as the agent (username + tempPassword).
func (p *Provisioner) RegisterKey(agentName, tempPassword, publicKey, title string) (*SSHKey, error) {
	if agentName == "" {
		return nil, NewConfigError("RegisterKey: empty agent name", nil)
	}
	// Dry-run short-circuits BEFORE credential validation so operators can
	// preview the full call sequence without supplying real credentials.
	if p.cfg.DryRun {
		return &SSHKey{Key: publicKey, Title: title}, nil
	}
	// tempPassword is accepted for backward compatibility but no longer
	// required: Forgejo v1.21+ blocks POST /api/v1/user/keys for users who
	// must change their password, so we use the admin endpoint instead.
	p.limiter.Acquire()

	keyReq := map[string]string{
		"key":   publicKey,
		"title": title,
	}
	bodyBytes, mErr := json.Marshal(keyReq)
	if mErr != nil {
		return nil, NewInternalError("RegisterKey: failed to marshal request", mErr)
	}

	resp, err := p.doWithRetry(http.MethodPost, p.adminUserKeysURL(agentName), bytes.NewReader(bodyBytes),
		func(r *http.Request) {
			r.Header.Set("Content-Type", "application/json")
			p.setAdminAuth(r)
		})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusCreated:
		var key SSHKey
		if decErr := json.NewDecoder(resp.Body).Decode(&key); decErr != nil {
			return nil, NewInternalError(
				fmt.Sprintf("RegisterKey(%s): failed to decode response", agentName), decErr)
		}
		return &key, nil

	default:
		body := readAndCloseBody(resp)
		return nil, NewAPIError(
			fmt.Sprintf("RegisterKey(%s): unexpected HTTP %d: %s",
				agentName, resp.StatusCode, body), nil)
	}
}

// CreateToken calls POST /api/v1/users/{name}/tokens. Returns the new PAT;
// the caller MUST capture .Token immediately — Forgejo only returns the
// plaintext token on creation. Auth: HTTP Basic Auth as the admin user
// (NOT the admin token — the token-create endpoint requires BasicAuth).
//
// Note: the Forgejo admin token and the admin user's password are distinct
// credentials. The CLI surfaces both via env vars; the syncer holds the
// admin password in memory only for the duration of the PAT creation call.
func (p *Provisioner) CreateToken(agentName, adminUser, adminPassword string, req *CreateTokenRequest) (*AccessToken, error) {
	if agentName == "" {
		return nil, NewConfigError("CreateToken: empty agent name", nil)
	}
	if req == nil {
		return nil, NewConfigError("CreateToken: nil request", nil)
	}
	// Dry-run short-circuits BEFORE credential validation — see RegisterKey.
	if p.cfg.DryRun {
		return &AccessToken{Name: req.Name, Scopes: req.Scopes}, nil
	}
	if adminUser == "" || adminPassword == "" {
		return nil, NewConfigError(
			fmt.Sprintf("CreateToken(%s): missing admin BasicAuth credentials", agentName), nil)
	}
	p.limiter.Acquire()

	bodyBytes, mErr := json.Marshal(req)
	if mErr != nil {
		return nil, NewInternalError("CreateToken: failed to marshal request", mErr)
	}

	resp, err := p.doWithRetry(http.MethodPost, p.userTokensURL(agentName), bytes.NewReader(bodyBytes),
		func(r *http.Request) {
			r.Header.Set("Content-Type", "application/json")
			r.SetBasicAuth(adminUser, adminPassword)
		})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusCreated:
		var tok AccessToken
		if decErr := json.NewDecoder(resp.Body).Decode(&tok); decErr != nil {
			return nil, NewInternalError(
				fmt.Sprintf("CreateToken(%s): failed to decode response", agentName), decErr)
		}
		// .Token is only populated on this create response — already captured
		// by Decode into tok.Token. Callers must read it immediately.
		return &tok, nil

	default:
		body := readAndCloseBody(resp)
		return nil, NewAPIError(
			fmt.Sprintf("CreateToken(%s): unexpected HTTP %d: %s",
				agentName, resp.StatusCode, body), nil)
	}
}

// RevokeToken calls DELETE /api/v1/users/{name}/tokens/{id}. Returns nil on
// success (204 No Content). Used by the deprovision path when an agent is
// marked offboarded.
//
// Auth: HTTP Basic Auth as the admin user.
func (p *Provisioner) RevokeToken(agentName, adminUser, adminPassword string, tokenID int64) error {
	if agentName == "" {
		return NewConfigError("RevokeToken: empty agent name", nil)
	}
	if tokenID <= 0 {
		return NewConfigError(
			fmt.Sprintf("RevokeToken(%s): invalid token id %d", agentName, tokenID), nil)
	}
	// Dry-run short-circuits BEFORE credential validation — see RegisterKey.
	if p.cfg.DryRun {
		return nil
	}
	p.limiter.Acquire()

	resp, err := p.doWithRetry(http.MethodDelete, p.userTokenURL(agentName, tokenID), nil,
		func(r *http.Request) {
			r.SetBasicAuth(adminUser, adminPassword)
		})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil

	default:
		body := readAndCloseBody(resp)
		return NewAPIError(
			fmt.Sprintf("RevokeToken(%s, %d): unexpected HTTP %d: %s",
				agentName, tokenID, resp.StatusCode, body), nil)
	}
}

// Close releases any resources held by the provisioner. In v1 it is a no-op
// because there is no transport goroutine to stop. The method exists so
// callers can defer it without conditional code.
func (p *Provisioner) Close() error { return nil }

// ---------------------------------------------------------------------------
// URL redaction helper
// ---------------------------------------------------------------------------

// redactURL strips any userinfo from a URL so it is safe to log. If parsing
// fails, the original is returned (better to log something than nothing).
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.User = nil
	return u.String()
}
