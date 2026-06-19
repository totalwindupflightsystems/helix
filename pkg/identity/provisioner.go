// Package identity — Forgejo API client and provisioning logic.
//
// This file is the STUB implementation. The contract is complete; the bodies
// return ErrNotImplemented for every method that hits the network. Real
// implementation lands after spec review. See specs/agent-identity.md §7
// for the integration test matrix that must pass before this stub is replaced.
package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// tokenBucket is a hand-rolled rate limiter (stdlib-only). The real
// implementation lives in provisioner_bucket.go (out of scope for the
// stub). Defined here as a named type so the Provisioner compiles.
//
// Spec reference: specs/agent-identity.md §7.4 (Rate Limiting).
type tokenBucket struct {
	ratePerSec int
}

func newTokenBucket(ratePerSec int) *tokenBucket {
	return &tokenBucket{ratePerSec: ratePerSec}
}

// Wait blocks until a token is available or ctx is cancelled. Stub returns
// immediately; real impl uses time.Ticker + a buffered channel.
func (b *tokenBucket) Wait(ctx context.Context) error {
	return nil
}

// ErrNotImplemented is returned by every stubbed method. The CLI checks for
// this and exits with a clear "build not done yet" message rather than a
// confusing nil-response panic.
var ErrNotImplemented = errors.New("identity: method not implemented (stub)")

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// ProvisionerConfig holds runtime configuration. All fields are populated
// from environment variables / CLI flags in cmd/helix-identity/main.go;
// nothing here may be hardcoded.
type ProvisionerConfig struct {
	// ForgejoBaseURL is the root of the Forgejo instance, e.g.
	// "https://forge.helixloop.dev". No trailing slash.
	ForgejoBaseURL string

	// AdminToken is a Forgejo admin user's personal access token. Used for
	// /api/v1/admin/* endpoints. Source: HELIX_FORGEJO_ADMIN_TOKEN env.
	AdminToken string

	// HTTPClient allows tests to inject a mock. Defaults to a stdlib client
	// with a 30s timeout.
	HTTPClient *http.Client

	// RateLimitPerSec is the conservative ceiling for outbound API calls.
	// Forgejo's default rate limit is 10 req/s per token; we stay below it.
	RateLimitPerSec int

	// DryRun short-circuits all mutating requests. The provisioner still
	// validates inputs and prints intended actions to the logger.
	DryRun bool

	// SSHKeyDir is where SSH keypairs are persisted. Created if absent.
	SSHKeyDir string
}

// Validate returns nil if cfg is usable, or a descriptive error otherwise.
func (c *ProvisionerConfig) Validate() error {
	if c.ForgejoBaseURL == "" {
		return errors.New("identity: ForgejoBaseURL is required (HELIX_FORGEJO_URL)")
	}
	if _, err := url.Parse(c.ForgejoBaseURL); err != nil {
		return fmt.Errorf("identity: ForgejoBaseURL invalid: %w", err)
	}
	if c.AdminToken == "" {
		return errors.New("identity: AdminToken is required (HELIX_FORGEJO_ADMIN_TOKEN)")
	}
	if c.RateLimitPerSec <= 0 {
		c.RateLimitPerSec = 5
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Provisioner
// ---------------------------------------------------------------------------

// Provisioner is the Forgejo-side half of helix-identity. It owns the HTTP
// client, rate limiter, and dry-run flag. The Syncer (syncer.go) drives it.
type Provisioner struct {
	cfg     ProvisionerConfig
	limiter *tokenBucket
}

// NewProvisioner constructs a Provisioner. Returns an error if cfg.Validate
// fails — callers should treat this as a hard CLI error (exit 2).
func NewProvisioner(cfg ProvisionerConfig) (*Provisioner, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Provisioner{
		cfg:     cfg,
		limiter: newTokenBucket(cfg.RateLimitPerSec),
	}, nil
}

// Provision performs the full create-or-update flow for a single agent.
// It is idempotent: re-running on an already-provisioned agent returns
// Action=Unchanged (or Action=Updated if any field drifted).
//
// Order of operations:
//
//  1. GET /api/v1/users/{name}  — does the user already exist?
//  2. POST /api/v1/admin/users  — create if missing
//  3. PATCH /api/v1/admin/users/{name} — sync bio/website/email if drifted
//  4. POST /api/v1/user/keys   — add SSH public key (idempotent by fingerprint)
//  5. BasicAuth as the new user → POST /api/v1/users/{name}/tokens
//     (mints the long-lived PAT; temp password is discarded)
//
// On any failure mid-flow, the function returns a partial ProvisioningResult
// with Action=ActionFailed and Error populated. The Syncer decides whether
// to continue with the next agent.
func (p *Provisioner) Provision(ctx context.Context, a *Agent) (*ProvisioningResult, error) {
	res := &ProvisioningResult{
		Agent:  a.Name,
		Action: ActionSkipped,
	}
	start := time.Now()
	defer func() { res.Duration = time.Since(start) }()

	if a == nil || a.Name == "" {
		res.Action = ActionFailed
		res.Error = "nil agent or empty name"
		return res, errors.New(res.Error)
	}
	if !a.Status.IsValid() {
		res.Action = ActionFailed
		res.Error = fmt.Sprintf("invalid status %q", a.Status)
		return res, errors.New(res.Error)
	}

	// Dry-run short-circuit. We still want to validate inputs and print
	// intended actions, but no HTTP calls.
	if p.cfg.DryRun {
		res.Action = ActionSkipped
		return res, nil // nil error: dry-run is a "successful preview"
	}

	// Steps 1–5 are stubbed. Each returns ErrNotImplemented until the
	// build pass lands.
	if _, err := p.userExists(ctx, a.Name); err != nil {
		res.Action = ActionFailed
		res.Error = err.Error()
		return res, err
	}
	res.Action = ActionCreated
	return res, ErrNotImplemented
}

// Deprovision revokes the agent's PAT and marks the Forgejo account inactive.
// SSH keys are archived (renamed to *.offboarded) rather than deleted, so we
// can prove the agent's git history remains attributable.
//
// Endpoints used:
//
//	DELETE /api/v1/users/{name}/tokens/{id}   — revoke each PAT
//	PATCH  /api/v1/admin/users/{name}        — set active=false
//	GET    /api/v1/users/{name}/keys         — list keys to archive
//
// We do NOT call DELETE /api/v1/admin/users/{name} in v1 — historical PRs
// and comments would orphan. See specs/agent-identity.md §6.5 for the
// deprovisioning lifecycle rationale.
func (p *Provisioner) Deprovision(ctx context.Context, a *Agent) (*ProvisioningResult, error) {
	res := &ProvisioningResult{
		Agent:  a.Name,
		Action: ActionDeprovisioned,
	}
	start := time.Now()
	defer func() { res.Duration = time.Since(start) }()

	if p.cfg.DryRun {
		return res, nil
	}
	return res, ErrNotImplemented
}

// Keygen generates a fresh ed25519 keypair, persists the encrypted PEM + the
// OpenSSH public key line, and returns the KeyPair. Does NOT register the key
// with Forgejo — that is a separate step in Provision.Provision.
//
// File layout under SSHKeyDir:
//
//	<name>.pub          — OpenSSH authorized_keys line
//	<name>.key.pem      — PKCS#8 PEM (unencrypted in v1; see types.go)
//	<name>.fingerprint  — SHA256:... fingerprint
//
// In v2 we will add <name>.key.pem.enc with passphrase-derived AES — out
// of scope for v1.
func (p *Provisioner) Keygen(a *Agent) (*KeyPair, error) {
	if a == nil || a.Name == "" {
		return nil, errors.New("identity: nil agent or empty name")
	}
	// Real implementation calls GenerateKeyPair (defined later in this
	// file once the spec is approved) and writes the three files.
	return nil, ErrNotImplemented
}

// ---------------------------------------------------------------------------
// Forgejo API methods (one per documented endpoint)
// ---------------------------------------------------------------------------

// userExists returns (true, nil) if the user exists in Forgejo,
// (false, nil) if a 404 came back, and (false, err) for any other failure.
//
// Endpoint: GET /api/v1/users/{name}
// Auth:     none (public lookup; admin token still works fine)
func (p *Provisioner) userExists(ctx context.Context, name string) (bool, error) {
	return false, ErrNotImplemented
}

// createUser POSTs to /api/v1/admin/users. See CreateUserRequest for the
// payload. On success, returns the new ForgejoAccount.
//
// Auth: admin token required (BasicAuth as admin user OR ?access_token=
// query param OR Authorization: token <admin-pat> header — we use header).
func (p *Provisioner) createUser(ctx context.Context, req CreateUserRequest) (*ForgejoAccount, error) {
	return nil, ErrNotImplemented
}

// updateUser PATCHes /api/v1/admin/users/{name} to sync bio, website,
// location, full_name. We only call this when fields drift — see
// Provisioner.Provision step 3.
func (p *Provisioner) updateUser(ctx context.Context, name string, req CreateUserRequest) (*ForgejoAccount, error) {
	return nil, ErrNotImplemented
}

// addSSHKey POSTs to /api/v1/user/keys (auth: as the agent user, or admin).
// We use admin auth here to keep the flow single-token.
//
// Idempotency: list keys first, compare fingerprint, skip if present.
//   GET  /api/v1/users/{name}/keys
//   POST /api/v1/users/{name}/keys   (or /api/v1/user/keys as admin)
func (p *Provisioner) addSSHKey(ctx context.Context, name string, pub OpenSSHKey) (*SSHKey, error) {
	return nil, ErrNotImplemented
}

// mintToken uses BasicAuth (username + temp password) to call
// /api/v1/users/{name}/tokens. The response body contains the actual token
// string in the `token` field — Forgejo returns this ONLY on creation.
//
// After this call returns successfully, the temp password is no longer
// needed and may be forgotten. We do not log or persist it.
func (p *Provisioner) mintToken(ctx context.Context, name, tempPassword string, scopes []string) (*AccessToken, error) {
	return nil, ErrNotImplemented
}

// ---------------------------------------------------------------------------
// HTTP helper (real impl)
// ---------------------------------------------------------------------------

// doRequest is the single chokepoint for all Forgejo HTTP calls. The stub
// does not call it; real impl uses it everywhere. It applies:
//   - rate limiting via p.limiter
//   - admin token auth header
//   - ctx-aware cancellation
//   - JSON encode/decode
//   - structured error wrapping (status code + body excerpt)
func (p *Provisioner) doRequest(ctx context.Context, method, path string, body, out any) (*http.Response, error) {
	if err := p.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("identity: rate limiter: %w", err)
	}
	full := p.cfg.ForgejoBaseURL + path
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("identity: marshal body: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, full, reader)
	if err != nil {
		return nil, fmt.Errorf("identity: build request: %w", err)
	}
	req.Header.Set("Authorization", "token "+p.cfg.AdminToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	// Stub: do not actually send. Real impl returns client.Do(req) and
	// decodes out if non-nil.
	return nil, ErrNotImplemented
}

// ---------------------------------------------------------------------------
// OpenSSH public key marshaling (stdlib-only)
// ---------------------------------------------------------------------------

// OpenSSHKey is a typed wrapper around the marshaled public key bytes,
// preventing accidental misuse of raw ed25519 keys as authorized_keys lines.
type OpenSSHKey struct {
	// Line is the full authorized_keys entry, e.g.
	//   "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA... helix-identity\n"
	Line string

	// Fingerprint is the SHA256:... fingerprint, computed locally so we
	// don't need a round-trip to compare against Forgejo's listing.
	Fingerprint string
}

// sshKeyTypeEd25519 is the SSH wire-format key type identifier. Per RFC 8709.
const sshKeyTypeEd25519 = "ssh-ed25519"

// sshEd25519KeyLen is the fixed public-key length for ed25519 (32 bytes).
const sshEd25519KeyLen = 32

// avatarSeed is a deterministic short string derived from an agent name,
// used as the identicon seed in Forgejo's avatar generation. Forgejo
// generates identicons server-side from the username, so we just pass the
// username — this constant exists only to document the intent.
//
// See specs/agent-identity.md §4.4 (Avatar Generation).
const avatarSeed = "identicon"