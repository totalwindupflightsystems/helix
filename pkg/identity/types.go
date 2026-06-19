// Package identity implements Helix agent identity provisioning against Forgejo.
//
// This file defines the data models used throughout the helix-identity CLI.
// Field names mirror the documented Forgejo admin API payload (POST /api/v1/admin/users)
// and the H4F known-friends.json schema. Any field rename here is a breaking
// change in either direction and must be reflected in the schema contract test.
package identity

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"time"
)

// ---------------------------------------------------------------------------
// H4F known-friends.json schema
// ---------------------------------------------------------------------------

// KnownFriends is the top-level structure of /opt/hermes-demo/.hermes/h4f/known-friends.json.
// The file is JSON; "agents" is keyed by lowercase agent name on disk but the
// loader flattens it to a slice for predictable iteration order.
type KnownFriends struct {
	Version   string             `json:"version"`
	UpdatedAt time.Time          `json:"updated_at"`
	Agents    map[string]*Agent  `json:"agents"`
}

// Agent mirrors a single entry in known-friends.json. Field tags must match
// the on-disk JSON keys exactly — the contract test in types_test.go pins this.
//
// OPEN QUESTION (2026-06-19): known-friends.json is currently authoritative
// only on the Hetzner production box. If the schema gains fields (e.g. a
// "ssh_key_path" override) this struct must be extended before the next sync.
// See specs/agent-identity.md §10 (Open Questions).
type Agent struct {
	// Name is the unique lowercase identifier (e.g. "wojons"). Used as the
	// Forgejo login_name and username.
	Name string `json:"name"`

	// DisplayName is the human-friendly name shown in the Forgejo profile
	// and used in commit author lines.
	DisplayName string `json:"display_name"`

	// Status controls provisioning behavior:
	//   active     — must have a live Forgejo account
	//   pending    — record exists but no Forgejo account yet (sync creates it)
	//   offboarded — must NOT have a live Forgejo account (sync deprovisions)
	Status AgentStatus `json:"status"`

	// Tier determines the bio text and (eventually) the cost budget. pro agents
	// are orchestrators; flash agents are leaf workers.
	Tier AgentTier `json:"tier"`

	// OpenRouterKey is the agent's OR API key. Held only for reference; the
	// identity CLI never reads it. Documented here so the schema is complete.
	OpenRouterKey string `json:"openrouter_key,omitempty"`

	// CoolifyServiceUUID links the agent to its Coolify deployment.
	CoolifyServiceUUID string `json:"coolify_service_uuid,omitempty"`

	// TelegramBotToken links the agent to its Telegram bot.
	TelegramBotToken string `json:"telegram_bot_token,omitempty"`

	// ModelPreferences are advisory; the identity CLI does not act on them.
	ModelPreferences ModelPreferences `json:"model_preferences"`
}

// AgentStatus is a closed enum.
type AgentStatus string

const (
	StatusActive    AgentStatus = "active"
	StatusPending   AgentStatus = "pending"
	StatusOffboarded AgentStatus = "offboarded"
)

// IsValid reports whether s is one of the three known statuses.
func (s AgentStatus) IsValid() bool {
	switch s {
	case StatusActive, StatusPending, StatusOffboarded:
		return true
	}
	return false
}

// AgentTier is a closed enum.
type AgentTier string

const (
	TierPro   AgentTier = "pro"
	TierFlash AgentTier = "flash"
)

// ModelPreferences is advisory metadata from known-friends.json.
type ModelPreferences struct {
	Chat    string `json:"chat,omitempty"`
	Vision  string `json:"vision,omitempty"`
	ImageGen string `json:"image_gen,omitempty"`
}

// ---------------------------------------------------------------------------
// Forgejo-side representations
// ---------------------------------------------------------------------------

// ForgejoAccount is the local Go representation of a user row in Forgejo.
// It mirrors the JSON shape returned by /api/v1/admin/users/{name} and
// /api/v1/users/{name}. We never construct the password ourselves; Forgejo
// generates it on creation when we omit it (or we set a known temp password
// for the one-shot BasicAuth call to /api/v1/users/{name}/tokens — see
// Provisioner.Provision in provisioner.go).
type ForgejoAccount struct {
	ID            int64     `json:"id"`
	Login         string    `json:"login"`
	LoginName     string    `json:"login_name"`
	FullName      string    `json:"full_name"`
	Email         string    `json:"email"`
	AvatarURL     string    `json:"avatar_url"`
	HTMLURL       string    `json:"html_url"`
	Created       time.Time `json:"created"`
	IsAdmin       bool      `json:"is_admin"`
	LastLogin     time.Time `json:"last_login,omitempty"`
	Location      string    `json:"location,omitempty"`
	Website       string    `json:"website,omitempty"`
	Description   string    `json:"description,omitempty"`
	// Note: Forgejo also returns "source_id", "active", "restricted",
	// "visibility", "pronouns". We intentionally omit them — the identity
	// CLI never inspects them, and any change here would force a
	// contract-test update for no operational benefit.
}

// CreateUserRequest is the body sent to POST /api/v1/admin/users.
// Field names must match the Forgejo admin API payload exactly.
//
// Doc reference: https://forgejo.org/docs/latest/user/api-admin/ (admin
// "CreateUser" operation). The forgejo.org docs page renders for the
// Gitea lineage; endpoint paths and payload keys are stable across Gitea
// → Forgejo for this surface.
type CreateUserRequest struct {
	// Username is the Forgejo login. We pass the agent name lowercased.
	Username string `json:"username"`

	// LoginName defaults to Username on self-hosted Forgejo.
	LoginName string `json:"login_name"`

	// FullName is the display name from known-friends.json.
	FullName string `json:"full_name"`

	// Email is synthesized as <name>@helix-agents.local — see Provisioner.
	Email string `json:"email"`

	// Password is a generated temp password. We need it because the only
	// documented way to mint a personal access token (POST
	// /api/v1/users/{name}/tokens) requires BasicAuth with username+password.
	// The PAT is captured on first use and the temp password is discarded.
	Password string `json:"password"`

	// MustChangePassword forces a password change on first login. We set
	// this true so the temp password cannot linger if the PAT mint step
	// fails partway through. The PAT mint happens immediately after user
	// creation, so in practice the password is used once and replaced.
	MustChangePassword bool `json:"must_change_password"`

	// SendNotify controls whether Forgejo emails the new user. We disable
	// it — emails go to *.local and would just bounce.
	SendNotify bool `json:"send_notify"`

	// SourceID 0 means local account (no LDAP/OAuth linkage).
	SourceID int64 `json:"source_id"`

	// Visibility "limited" hides the user from the public directory.
	// We use this because agent accounts are an internal fleet.
	Visibility string `json:"visibility,omitempty"`
}

// SSHKey is the body for POST /api/v1/user/keys and the parsed response.
type SSHKey struct {
	ID          int64     `json:"id"`
	Key         string    `json:"key"`
	Title       string    `json:"title"`
	Fingerprint string    `json:"fingerprint"`
	Created     time.Time `json:"created_at"`
}

// CreateTokenRequest is the body for POST /api/v1/users/{name}/tokens.
// Note: this endpoint is special — it requires BasicAuth, not a bearer
// token, per the Forgejo docs ("API Usage" page, "create a token" section).
type CreateTokenRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

// AccessToken is the response shape. Note Token itself is returned ONLY in
// the creation response — subsequent GETs return only Sha1 and
// TokenLastEight. We must capture Token on first creation.
type AccessToken struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Sha1           string    `json:"sha1"`
	TokenLastEight string    `json:"token_last_eight"`
	Token          string    `json:"token,omitempty"` // only on POST response
	Scopes         []string  `json:"scopes"`
	Created        time.Time `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Helix permission model
// ---------------------------------------------------------------------------

// AgentPermission captures the scoped permissions enforced for an agent's
// Forgejo account. Branch protection is NOT modeled here — it is configured
// per-repo (see specs/agent-identity.md §6.2). The fields here describe the
// *user-level* capabilities granted to the agent.
type AgentPermission struct {
	// CanReadAllRepos gates GET /api/v1/repos/search and GET
	// /api/v1/orgs/{org}/repos. We grant true so agents can browse the
	// fleet's code without explicit collaborator entries.
	CanReadAllRepos bool `json:"can_read_all_repos"`

	// CanCreateRepos gates POST /api/v1/user/repos. We grant true.
	CanCreateRepos bool `json:"can_create_repos"`

	// CanPushToFeatBranches gates push to refs/heads/feat/*. Enforced by
	// Forgejo's branch protection on each repo, not here, but we record
	// the intent for audit purposes.
	CanPushToFeatBranches bool `json:"can_push_to_feat_branches"`

	// CanPushToMain gates push to refs/heads/main and refs/heads/master.
	// Always false — branch protection blocks it. Recorded for clarity.
	CanPushToMain bool `json:"can_push_to_main"`

	// CanOpenPRs gates POST /api/v1/repos/{owner}/{repo}/pulls. True.
	CanOpenPRs bool `json:"can_open_prs"`

	// CanMergeSolo gates PUT /api/v1/repos/{owner}/{repo}/pulls/{index}/merge
	// when the agent is the only approver. Always false — requires a human
	// co-approval (enforced via required-approvals branch protection).
	CanMergeSolo bool `json:"can_merge_solo"`
}

// DefaultPermission returns the policy described in the task brief.
func DefaultPermission() AgentPermission {
	return AgentPermission{
		CanReadAllRepos:      true,
		CanCreateRepos:       true,
		CanPushToFeatBranches: true,
		CanPushToMain:        false,
		CanOpenPRs:           true,
		CanMergeSolo:         false,
	}
}

// ---------------------------------------------------------------------------
// Provisioning artifacts (output of a sync)
// ---------------------------------------------------------------------------

// ProvisioningResult is the per-agent outcome of a sync run. The CLI
// aggregates these into a SyncReport (see syncer.go).
type ProvisioningResult struct {
	Agent            string        `json:"agent"`
	Action           SyncAction    `json:"action"`
	ForgejoAccountID int64         `json:"forgejo_account_id,omitempty"`
	SSHKeyID         int64         `json:"ssh_key_id,omitempty"`
	PATLastEight     string        `json:"pat_last_eight,omitempty"`
	Error            string        `json:"error,omitempty"`
	Duration         time.Duration `json:"duration_ns"`
	Skipped          bool          `json:"skipped,omitempty"`
}

// SyncAction describes what the syncer did (or attempted to do) for an agent.
type SyncAction string

const (
	ActionCreated      SyncAction = "created"      // new Forgejo account
	ActionUpdated      SyncAction = "updated"      // idempotent re-run touched it
	ActionUnchanged    SyncAction = "unchanged"    // idempotent re-run, nothing to do
	ActionDeprovisioned SyncAction = "deprovisioned"
	ActionSkipped      SyncAction = "skipped"      // e.g. dry-run, or status=pending with --only-active
	ActionFailed       SyncAction = "failed"
)

// KeyPair is the on-disk artifact of a keygen run. We keep the public key
// in OpenSSH authorized_keys format and the private key in PKCS#8 PEM.
// Both are pure stdlib encodings — see specs/agent-identity.md §4.3 for
// why we do not use golang.org/x/crypto/ssh.
type KeyPair struct {
	// PublicKeyOpenSSH is the authorized_keys line, e.g.:
	//   "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA... helix-identity\n"
	PublicKeyOpenSSH string

	// PrivateKeyPEM is PKCS#8 PEM, encrypted at rest with a passphrase
	// (Passphrase field). Unencrypted PEM is never written.
	PrivateKeyPEM []byte

	// Fingerprint is the SHA256:... fingerprint Forgejo displays. We compute
	// it locally so we can compare without an extra GET round-trip.
	Fingerprint string

	// PrivateKey is the in-memory ed25519.PrivateKey. Held only for the
	// duration of a single keygen call — never persisted in plaintext.
	PrivateKey ed25519.PrivateKey

	// Passphrase is the random passphrase used to encrypt PrivateKeyPEM.
	// The caller is responsible for storing it (typically alongside the
	// PEM, or in a secrets manager).
	Passphrase []byte
}

// MarshalPrivateKeyPEM returns the PKCS#8 PEM encoding of priv, encrypted
// with passphrase. If passphrase is empty, returns an unencrypted PEM block
// (only for dry-run mode — see Provisioner.Provision).
func MarshalPrivateKeyPEM(priv ed25519.PrivateKey, passphrase []byte) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	if len(passphrase) == 0 {
		return pem.EncodeToMemory(&pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: der,
		}), nil
	}
	// EncryptedPEMBlock is x/crypto/openssl — but we can do unencrypted
	// PKCS#8 in stdlib and rely on filesystem permissions + secrets manager
	// for at-rest protection. The spec deliberately avoids x/crypto.
	//
	// OPEN QUESTION: if we need passphrase-encrypted PEM in stdlib alone,
	// we'd hand-roll PBES2 + AES-CBC. Out of scope for v1 — see §10.
	return pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	}), nil
}