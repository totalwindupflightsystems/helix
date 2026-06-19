package identity

// This file defines the Forgejo HTTP client surface. Every method that would
// touch the network is a stub returning ErrNotImplemented — the goal of v1
// is to lock down the contract (URLs, request bodies, response types, auth
// model, rate-limit shape, retry policy) so the real transport can be dropped
// in without touching callers.
//
// What IS implemented here:
//   - Provisioner configuration + validation
//   - URL construction (proves the endpoint shapes compile and stringify)
//   - DryRunMode decision logic
//   - RateLimiter data structure (token bucket; stubbed Acquire is a no-op)
//   - Retry policy constants
//
// What is NOT implemented (returns ErrNotImplemented):
//   - CreateUser, GetAccount, RegisterKey, CreateToken, RevokeToken
//
// See specs/agent-identity.md §9.2 and §11 for the contract this stub models.

import (
	"fmt"
	"net/http"
	"net/url"
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

	// AdminToken is the Forgejo admin PAT used for /admin/users endpoints.
	// Sent as "Authorization: token <AdminToken>".
	AdminToken string

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
	if c.AdminToken == "" {
		return NewConfigError(
			"FORGEJO_ADMIN_TOKEN not set (use --admin-token or FORGEJO_ADMIN_TOKEN env var)", nil)
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

// adminUsersURL is POST/GET /api/v1/admin/users[/{name}].
func (p *Provisioner) adminUsersURL(name string) string {
	base := strings.TrimRight(p.cfg.ForgejoURL, "/")
	if name == "" {
		return base + "/api/v1/admin/users"
	}
	return base + "/api/v1/admin/users/" + url.PathEscape(name)
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
// Endpoint stubs — all return ErrNotImplemented in v1
// ---------------------------------------------------------------------------

// GetAccount calls GET /api/v1/admin/users/{name}. Returns the account if it
// exists, or (nil, nil) if it does not (404 is treated as "not found", not
// an error, so callers can use this as an idempotency probe).
//
// Auth: admin token in Authorization header.
func (p *Provisioner) GetAccount(name string) (*ForgejoAccount, error) {
	_ = p.adminUsersURL(name) // prove the URL stringifies
	if p.cfg.DryRun {
		return nil, nil // dry-run: assume not-yet-provisioned
	}
	return nil, ErrNotImplemented
}

// CreateUser calls POST /api/v1/admin/users. On success returns the newly
// created ForgejoAccount. A 409 Conflict is mapped to a TypedError of kind
// API so the syncer can downgrade it to ActionUnchanged rather than failing.
//
// Auth: admin token in Authorization header.
func (p *Provisioner) CreateUser(req *CreateUserRequest) (*ForgejoAccount, error) {
	_ = p.adminUsersURL("") // POST target
	if req == nil {
		return nil, NewConfigError("CreateUser: nil request", nil)
	}
	if p.cfg.DryRun {
		return &ForgejoAccount{
			Login: req.Username, Email: req.Email, FullName: req.FullName,
		}, nil
	}
	return nil, ErrNotImplemented
}

// RegisterKey calls POST /api/v1/user/keys. The key is registered as the
// agent, authenticated via HTTP Basic Auth (agent username + temp password).
// The admin token cannot register keys on behalf of a user — Forgejo's
// user-key endpoint requires user-scoped credentials.
//
// Auth: HTTP Basic Auth as the agent (username + tempPassword).
func (p *Provisioner) RegisterKey(agentName, tempPassword, publicKey, title string) (*SSHKey, error) {
	_ = p.userKeysURL()
	if agentName == "" {
		return nil, NewConfigError("RegisterKey: empty agent name", nil)
	}
	// Dry-run short-circuits BEFORE credential validation so operators can
	// preview the full call sequence without supplying real credentials.
	if p.cfg.DryRun {
		return &SSHKey{Key: publicKey, Title: title}, nil
	}
	if tempPassword == "" {
		return nil, NewConfigError(
			fmt.Sprintf("RegisterKey(%s): missing temp password", agentName), nil)
	}
	return nil, ErrNotImplemented
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
	_ = p.userTokensURL(agentName)
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
	return nil, ErrNotImplemented
}

// RevokeToken calls DELETE /api/v1/users/{name}/tokens/{id}. Returns nil on
// success (204 No Content). Used by the deprovision path when an agent is
// marked offboarded.
//
// Auth: HTTP Basic Auth as the admin user.
func (p *Provisioner) RevokeToken(agentName, adminUser, adminPassword string, tokenID int64) error {
	_ = p.userTokenURL(agentName, tokenID)
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
	_ = adminUser
	_ = adminPassword
	return ErrNotImplemented
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
