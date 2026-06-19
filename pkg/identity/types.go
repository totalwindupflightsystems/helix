// Package identity implements provisioning of Helix agent accounts in a
// self-hosted Forgejo instance.
//
// This file defines all data models, enums, constructors, and the ED25519
// keypair serialization helpers used across the identity subsystem. The HTTP
// transport lives in provisioner.go; orchestration lives in syncer.go.
//
// Design constraints:
//   - Only stdlib + github.com/spf13/cobra may be imported.
//   - golang.org/x/crypto is deliberately avoided; OpenSSH wire format is
//     serialized by hand using crypto/ed25519 + crypto/x509.
//   - No secrets are stored in source. All credentials arrive via env vars
//     or runtime flags and are never marshaled to disk in plaintext beyond
//     the per-agent private key file (mode 0600, owned by the operator).
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Sentinel / typed errors and exit codes
// ---------------------------------------------------------------------------

// ErrNotImplemented is returned by every transport method in this v1 stub.
// Real network calls land in a follow-up change after spec review; the stubs
// exist to lock the interface shape and prove the build.
var ErrNotImplemented = errors.New("identity: not implemented in v1 stub (network call pending review)")

// Exit codes are machine-readable so cron jobs can branch without parsing
// stderr. See specs/agent-identity.md §15 for the full taxonomy.
const (
	ExitOK             = 0 // success (or dry-run with nothing to do)
	ExitConnRefused    = 1 // Forgejo unreachable / network error
	ExitGeneral        = 2 // unspecified operational error
	ExitFileOrAuth     = 3 // FILE_NOT_FOUND, AUTH_FAILED, malformed config
	ExitPartialFailure = 4 // some agents provisioned, some failed
)

// ErrorKind tags a typed error so callers can map it to an exit code without
// string matching.
type ErrorKind string

const (
	ErrKindConfig   ErrorKind = "config"   // missing env, malformed flags
	ErrKindNetwork  ErrorKind = "network"  // timeout, connection refused, 5xx
	ErrKindAPI      ErrorKind = "api"      // 400/403/404/409 from Forgejo
	ErrKindPartial  ErrorKind = "partial"  // some agents failed during sync
	ErrKindInternal ErrorKind = "internal" // key generation, state write
)

// TypedError carries a kind + human message + optional cause. It is the only
// error type returned by the identity package so callers can switch on Kind.
type TypedError struct {
	Kind    ErrorKind
	Message string
	Cause   error
}

func (e *TypedError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *TypedError) Unwrap() error { return e.Cause }

// NewConfigError reports a configuration problem (missing env var, bad path).
func NewConfigError(msg string, cause error) *TypedError {
	return &TypedError{Kind: ErrKindConfig, Message: msg, Cause: cause}
}

// NewNetworkError reports a transport-level failure (DNS, TLS, timeout, 5xx).
func NewNetworkError(msg string, cause error) *TypedError {
	return &TypedError{Kind: ErrKindNetwork, Message: msg, Cause: cause}
}

// NewAPIError reports a Forgejo API error response (4xx other than 429).
func NewAPIError(msg string, cause error) *TypedError {
	return &TypedError{Kind: ErrKindAPI, Message: msg, Cause: cause}
}

// NewPartialError reports that a sync completed with at least one failure.
func NewPartialError(msg string, cause error) *TypedError {
	return &TypedError{Kind: ErrKindPartial, Message: msg, Cause: cause}
}

// ExitCode maps a TypedError to a process exit code per §15 of the spec.
// Unknown errors fall back to ExitGeneral.
func (e *TypedError) ExitCode() int {
	switch e.Kind {
	case ErrKindConfig:
		return ExitFileOrAuth
	case ErrKindNetwork:
		return ExitConnRefused
	case ErrKindAPI:
		// AUTH_FAILED (401) and FILE_NOT_FOUND share exit 3 per the spec.
		return ExitFileOrAuth
	case ErrKindPartial:
		return ExitPartialFailure
	default:
		return ExitGeneral
	}
}

// ---------------------------------------------------------------------------
// Enums: AgentStatus, AgentTier, SyncAction
// ---------------------------------------------------------------------------

// AgentStatus mirrors the "status" field in known-friends.json.
type AgentStatus string

const (
	StatusActive     AgentStatus = "active"
	StatusPending    AgentStatus = "pending"
	StatusOffboarded AgentStatus = "offboarded"
)

// Valid reports whether s is a recognized status value.
func (s AgentStatus) Valid() bool {
	switch s {
	case StatusActive, StatusPending, StatusOffboarded:
		return true
	}
	return false
}

// Provisionable reports whether an agent in this status should be provisioned
// during a sync run. Pending and offboarded agents are skipped (offboarded
// agents are handled by the deprovision path instead).
func (s AgentStatus) Provisionable() bool { return s == StatusActive }

// AgentTier mirrors the "tier" field in known-friends.json.
type AgentTier string

const (
	TierPro   AgentTier = "pro"
	TierFlash AgentTier = "flash"
)

// Valid reports whether t is a recognized tier value.
func (t AgentTier) Valid() bool {
	switch t {
	case TierPro, TierFlash:
		return true
	}
	return false
}

// SyncAction is the per-agent outcome of a sync run, surfaced in the result
// table and persisted for idempotency reconciliation.
type SyncAction string

const (
	ActionCreated       SyncAction = "created"
	ActionUpdated       SyncAction = "updated"
	ActionUnchanged     SyncAction = "unchanged"
	ActionDeprovisioned SyncAction = "deprovisioned"
	ActionSkipped       SyncAction = "skipped"
	ActionFailed        SyncAction = "failed"
)

// ---------------------------------------------------------------------------
// known-friends.json model
// ---------------------------------------------------------------------------

// ModelPreferences mirrors the "model_preferences" subobject.
type ModelPreferences struct {
	Chat     string `json:"chat,omitempty"`
	Vision   string `json:"vision,omitempty"`
	ImageGen string `json:"image_gen,omitempty"`
}

// Agent mirrors a single entry in known-friends.json. Name is populated from
// the map key when parsed via KnownFriends (the JSON "name" field is also
// accepted if present, for forward compatibility with a list-based schema).
type Agent struct {
	Name               string           `json:"name,omitempty"`
	DisplayName        string           `json:"display_name"`
	Status             AgentStatus      `json:"status"`
	Tier               AgentTier        `json:"tier"`
	OpenRouterKey      string           `json:"openrouter_key,omitempty"`
	CoolifyServiceUUID string           `json:"coolify_service_uuid,omitempty"`
	TelegramBotToken   string           `json:"telegram_bot_token,omitempty"`
	ModelPreferences   ModelPreferences `json:"model_preferences,omitempty"`
}

// Email synthesizes the agent's Forgejo account email per §5 of the spec.
// All agent accounts use the @helix-agents.local pseudo-domain; no real
// addresses are ever stored.
func (a *Agent) Email() string {
	name := a.Name
	if name == "" {
		name = string(a.Status) // defensive; should never happen
	}
	return name + "@helix-agents.local"
}

// KeyTitle builds the human-readable title registered with the SSH public key
// in Forgejo (visible in the UI's "SSH Keys" panel).
func (a *Agent) KeyTitle() string {
	return fmt.Sprintf("Helix Agent — %s (%s)", a.Name, a.Tier)
}

// Validate reports the first structural problem with an agent entry, or nil.
// This catches schema drift early so a sync run fails fast rather than
// producing half-formed Forgejo accounts.
func (a *Agent) Validate() error {
	if a.Name == "" {
		return NewConfigError("agent name is empty", nil)
	}
	if !a.Status.Valid() {
		return NewConfigError(
			fmt.Sprintf("agent %q has invalid status %q", a.Name, a.Status), nil)
	}
	if !a.Tier.Valid() {
		return NewConfigError(
			fmt.Sprintf("agent %q has invalid tier %q", a.Name, a.Tier), nil)
	}
	return nil
}

// KnownFriends is the top-level shape of known-friends.json.
type KnownFriends struct {
	Version   int               `json:"version"`
	UpdatedAt time.Time         `json:"updated_at,omitempty"`
	Agents    map[string]*Agent `json:"agents"`
}

// ActiveAgents returns agents whose status is "active", sorted by name for
// deterministic sync ordering (important for stable dry-run output and
// reproducible test fixtures).
func (k *KnownFriends) ActiveAgents() []*Agent {
	out := make([]*Agent, 0, len(k.Agents))
	for name, a := range k.Agents {
		if a == nil {
			continue
		}
		if a.Name == "" {
			a.Name = name // backfill from map key
		}
		if a.Status.Provisionable() {
			out = append(out, a)
		}
	}
	sortAgentsByName(out)
	return out
}

// OffboardedAgents returns agents whose status is "offboarded", sorted.
// These are processed by the deprovision path during sync.
func (k *KnownFriends) OffboardedAgents() []*Agent {
	out := make([]*Agent, 0, len(k.Agents))
	for name, a := range k.Agents {
		if a == nil {
			continue
		}
		if a.Name == "" {
			a.Name = name
		}
		if a.Status == StatusOffboarded {
			out = append(out, a)
		}
	}
	sortAgentsByName(out)
	return out
}

// AllAgents returns every agent (any status), sorted. Used by status display.
func (k *KnownFriends) AllAgents() []*Agent {
	out := make([]*Agent, 0, len(k.Agents))
	for name, a := range k.Agents {
		if a == nil {
			continue
		}
		if a.Name == "" {
			a.Name = name
		}
		out = append(out, a)
	}
	sortAgentsByName(out)
	return out
}

// ---------------------------------------------------------------------------
// Forgejo API wire types
// ---------------------------------------------------------------------------

// ForgejoAccount mirrors the user object returned by GET /admin/users/{name}
// and POST /admin/users. Field names match the Forgejo/Gitea API JSON keys
// exactly so the response unmarshals without remapping.
type ForgejoAccount struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	LoginName string `json:"login_name"`
	FullName  string `json:"full_name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
	// Forgejo returns ISO8601 strings for timestamps; we keep them as strings
	// to avoid coupling to a specific timezone layout in v1.
	Created string `json:"created"`
	IsAdmin bool   `json:"is_admin"`
}

// CreateUserRequest is the POST /api/v1/admin/users body. The visibility
// "limited" hides the account from the public user directory but keeps it
// usable by anyone who knows the login.
type CreateUserRequest struct {
	Username           string `json:"username"`
	LoginName          string `json:"login_name"`
	FullName           string `json:"full_name"`
	Email              string `json:"email"`
	Password           string `json:"password"`
	MustChangePassword bool   `json:"must_change_password"`
	SendNotify         bool   `json:"send_notify"`
	SourceID           int    `json:"source_id"`
	Visibility         string `json:"visibility"`
}

// NewCreateUserRequest builds the request body for an agent. The temporary
// password is supplied by the caller (generated by GenerateTempPassword);
// it is never stored and forces a change on first login per Forgejo policy.
func NewCreateUserRequest(a *Agent, tempPassword string) *CreateUserRequest {
	return &CreateUserRequest{
		Username:           a.Name,
		LoginName:          a.Name,
		FullName:           a.DisplayName,
		Email:              a.Email(),
		Password:           tempPassword,
		MustChangePassword: true,
		SendNotify:         false, // no real mailbox; @helix-agents.local
		SourceID:           0,     // local auth
		Visibility:         "limited",
	}
}

// SSHKey mirrors the response from POST /api/v1/user/keys.
type SSHKey struct {
	ID          int64  `json:"id"`
	Key         string `json:"key"`
	Title       string `json:"title"`
	Fingerprint string `json:"fingerprint"`
	Created     string `json:"created_at"`
}

// CreateTokenRequest is the POST /api/v1/users/{name}/tokens body. Scopes
// are derived from the agent's AgentPermission via PermissionScopes().
type CreateTokenRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

// AccessToken mirrors the PAT response. The Token field is ONLY populated on
// creation (POST returns the plaintext token once; subsequent GETs omit it).
// Callers must capture .Token immediately and never log it.
type AccessToken struct {
	ID     int64    `json:"id"`
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
	SHA1   string   `json:"sha1,omitempty"` // legacy field, kept for compatibility
	Token  string   `json:"token,omitempty"`
}

// PATName is the symbolic name registered with every agent PAT.
const PATName = "helix-identity-pat"

// ---------------------------------------------------------------------------
// Permission model
// ---------------------------------------------------------------------------

// AgentPermission is the internal, capability-style view of what an agent may
// do. It is not serialized directly; instead it is projected onto Forgejo PAT
// scopes via PermissionScopes(). Branch protection (no push to main, no solo
// merge) is enforced by Forgejo repository settings, not by this code — see
// specs/agent-identity.md §8.3 for the full rationale.
type AgentPermission struct {
	CanReadAllRepos       bool
	CanCreateRepos        bool
	CanPushToFeatBranches bool
	CanPushToMain         bool
	CanOpenPRs            bool
	CanMergeSolo          bool
}

// DefaultPermission returns the standard capability set for a Helix agent
// per §8.3 of the spec. All agents get the same baseline in v1; per-tier
// customization is a follow-up.
func DefaultPermission() AgentPermission {
	return AgentPermission{
		CanReadAllRepos:       true,
		CanCreateRepos:        true,
		CanPushToFeatBranches: true,
		CanPushToMain:         false,
		CanOpenPRs:            true,
		CanMergeSolo:          false,
	}
}

// PermissionScopes projects an AgentPermission onto the Forgejo PAT scope
// vocabulary. The mapping is intentionally minimal — least privilege per
// capability. Order is stable so dry-run output is reproducible.
func (p AgentPermission) PermissionScopes() []string {
	scopes := make([]string, 0, 8)
	if p.CanReadAllRepos || p.CanPushToFeatBranches || p.CanOpenPRs {
		scopes = append(scopes, "read:repository")
	}
	if p.CanPushToFeatBranches {
		scopes = append(scopes, "write:repository")
	}
	if p.CanOpenPRs || p.CanReadAllRepos {
		scopes = append(scopes, "read:issue")
	}
	if p.CanOpenPRs {
		scopes = append(scopes, "write:issue")
	}
	// Every agent registers an SSH key on first provision, which requires
	// user-scope write. Read:user is included for self-lookup.
	scopes = append(scopes, "read:user", "write:user")
	return scopes
}

// NewCreateTokenRequest builds a PAT creation request for an agent using the
// default permission set.
func NewCreateTokenRequest(a *Agent) *CreateTokenRequest {
	return &CreateTokenRequest{
		Name:   PATName,
		Scopes: DefaultPermission().PermissionScopes(),
	}
}

// ---------------------------------------------------------------------------
// Provisioning result + state file
// ---------------------------------------------------------------------------

// ProvisioningResult is the per-agent outcome of a sync run. The Syncer
// collects one per agent and uses them to render the summary table and to
// decide the process exit code.
type ProvisioningResult struct {
	AgentName      string          `json:"agent_name"`
	Status         AgentStatus     `json:"status"`
	Action         SyncAction      `json:"action"`
	Account        *ForgejoAccount `json:"account,omitempty"`
	SSHKeyID       int64           `json:"ssh_key_id,omitempty"`
	SSHFingerprint string          `json:"ssh_fingerprint,omitempty"`
	PATID          int64           `json:"pat_id,omitempty"`
	PATLastEight   string          `json:"pat_last_eight,omitempty"`
	Error          string          `json:"error,omitempty"` // error.Error(), if ActionFailed
	Duration       time.Duration   `json:"-"`
}

// Succeeded reports whether the agent's provisioning completed without error.
// "unchanged" and "skipped" also count as success — they are not failures.
func (r *ProvisioningResult) Succeeded() bool {
	return r.Action != ActionFailed
}

// AgentState is the per-agent entry in the on-disk state file
// (~/.helix/state.json). It records just enough to make subsequent syncs
// idempotent and to render the status table without re-hitting Forgejo.
type AgentState struct {
	ForgejoAccountID int64     `json:"forgejo_account_id"`
	SSHKeyID         int64     `json:"ssh_key_id"`
	SSHFingerprint   string    `json:"ssh_fingerprint"`
	PATLastEight     string    `json:"pat_last_eight"`
	PATID            int64     `json:"pat_id"`
	LastProvisioned  time.Time `json:"last_provisioned"`
}

// StateFile is the on-disk record of provisioned agents. Schema version is
// bumped on breaking changes; the loader rejects unknown versions loudly.
type StateFile struct {
	Version  int                    `json:"version"`
	LastSync time.Time              `json:"last_sync"`
	Agents   map[string]*AgentState `json:"agents"`
}

// StateVersion is the current state file schema version.
const StateVersion = 1

// NewStateFile returns an empty state file with the current schema version.
func NewStateFile() *StateFile {
	return &StateFile{
		Version: StateVersion,
		Agents:  make(map[string]*AgentState),
	}
}

// ---------------------------------------------------------------------------
// ED25519 keypair + OpenSSH / PEM serialization (no golang.org/x/crypto)
// ---------------------------------------------------------------------------

// KeyPair is the artifact of GenerateKeyPair: an ED25519 keypair serialized
// in the formats Forgejo and the filesystem expect.
type KeyPair struct {
	PublicKeyOpenSSH string // "ssh-ed25519 AAAA... helix-identity"
	PrivateKeyPEM    string // PKCS#8 PEM block ("PRIVATE KEY")
	PrivateKey       []byte // raw PKCS#8 bytes (for direct file writes)
	Fingerprint      string // "SHA256:<base64>" — OpenSSH-style
	Passphrase       string // empty in v1 (filesystem mode 0600 protects at rest)
}

// GenerateKeyPair produces a fresh ED25519 keypair serialized for Forgejo +
// filesystem use. It does not write to disk; the Syncer is responsible for
// materializing the files with the correct permissions.
//
// OpenSSH wire format is assembled by hand because golang.org/x/crypto/ssh is
// deliberately out of scope for v1. The format is:
//
//	string "ssh-ed25519"   (4-byte BE length prefix + bytes)
//	string <32-byte key>   (4-byte BE length prefix + bytes)
//
// The fingerprint is the base64-encoded SHA256 of that packed public-key
// blob, matching `ssh-keygen -l -f <file>`.
func GenerateKeyPair() (*KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, NewInternalError("ed25519 key generation failed", err)
	}

	packed := packSSHEd25519PublicKey(pub)
	pubOpenSSH := "ssh-ed25519 " + base64.StdEncoding.EncodeToString(packed) + " helix-identity"

	// PKCS#8 is preferred over PKCS#1 / SEC1 for ED25519 because it carries
	// the algorithm OID unambiguously. x509.MarshalPKCS8PrivateKey works for
	// ed25519.PrivateKey directly.
	pkcs8, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, NewInternalError("pkcs8 marshal failed", err)
	}
	pemBlock := &pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}
	pemStr := string(pem.EncodeToMemory(pemBlock))

	sum := sha256.Sum256(packed)
	fingerprint := "SHA256:" + base64.StdEncoding.EncodeToString(sum[:])
	// OpenSSH strips base64 padding from fingerprints.
	fingerprint = trimBase64Padding(fingerprint)

	return &KeyPair{
		PublicKeyOpenSSH: pubOpenSSH,
		PrivateKeyPEM:    pemStr,
		PrivateKey:       pkcs8,
		Fingerprint:      fingerprint,
		Passphrase:       "", // v1: unencrypted at rest, protected by mode 0600
	}, nil
}

// packSSHEd25519PublicKey assembles the SSH wire-format public key blob:
// length-prefixed "ssh-ed25519" followed by the length-prefixed 32-byte key.
func packSSHEd25519PublicKey(pub ed25519.PublicKey) []byte {
	const name = "ssh-ed25519"
	buf := make([]byte, 0, 4+len(name)+4+len(pub))
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(name)))
	buf = append(buf, lenBuf[:]...)
	buf = append(buf, name...)
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(pub)))
	buf = append(buf, lenBuf[:]...)
	buf = append(buf, pub...)
	return buf
}

// trimBase64Padding strips trailing '=' characters from a base64 string,
// matching OpenSSH's fingerprint representation.
func trimBase64Padding(s string) string {
	for len(s) > 0 && s[len(s)-1] == '=' {
		s = s[:len(s)-1]
	}
	return s
}

// GenerateTempPassword returns a 32-character random password suitable for
// use as a Forgejo account's initial password. The password is never logged
// and is discarded after the CreateAccount call completes.
func GenerateTempPassword() (string, error) {
	// Use a printable alphabet that avoids ambiguous characters and Forgejo
	// reserved characters. 32 chars from a 54-char alphabet gives ~185 bits
	// of entropy, well above the Forgejo minimum.
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", NewInternalError("temp password rand failed", err)
	}
	out := make([]byte, 32)
	for i, b := range buf {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out), nil
}

// MaskToken returns the last 8 characters of a token prefixed with "****",
// for safe display in status tables and state files. If the token is shorter
// than 8 chars, the whole thing is masked.
func MaskToken(token string) string {
	if len(token) <= 8 {
		return "****"
	}
	return "****" + token[len(token)-8:]
}

// ---------------------------------------------------------------------------
// small helpers
// ---------------------------------------------------------------------------

// sortAgentsByName sorts a slice of agents in-place by Name. Used everywhere
// we project a KnownFriends map into a deterministic slice for stable output
// (dry-run diffs, status tables, test fixtures).
func sortAgentsByName(agents []*Agent) {
	// Simple insertion sort: agent counts are tiny (single digits), so the
	// extra dependency of sort.Slice isn't worth it. Stable, deterministic,
	// and obvious.
	for i := 1; i < len(agents); i++ {
		for j := i; j > 0 && agents[j-1].Name > agents[j].Name; j-- {
			agents[j-1], agents[j] = agents[j], agents[j-1]
		}
	}
}

// NewInternalError is exported here (rather than above) to keep the error
// constructors grouped at the bottom of the file for readability.
func NewInternalError(msg string, cause error) *TypedError {
	return &TypedError{Kind: ErrKindInternal, Message: msg, Cause: cause}
}
