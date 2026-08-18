# pkg/identity — Agent Identity Reference

`import "github.com/totalwindupflightsystems/helix/pkg/identity"`

Agent identity subsystem (SPEC-022: Portable Agent Identity): self-signed
Helix Identity Documents (HIDs), Forgejo account provisioning, key
management, and OAuth2 registration.

Design constraints: stdlib + cobra only; `golang.org/x/crypto` deliberately
avoided (OpenSSH wire format serialized by hand with `crypto/ed25519` +
`crypto/x509`); no secrets stored in source.

## HID and Agent Identity

```go
func NewAgentIdentity(name string) (*AgentIdentity, ed25519.PrivateKey, error)
func ImportHID(path string) (*AgentIdentity, error)
func GenerateKeyPair() (*KeyPair, error)
func CreateBindingProof(hid *HID, privKey ed25519.PrivateKey, clientID, forgeURL string) (*OAuthBindingProof, error)
func VerifyBindingProof(hid *HID, proof *OAuthBindingProof) (bool, error)
func NewNostrEventFromHID(id *AgentIdentity) (*NostrEvent, error)
```

Every agent gets a self-signed JSON HID containing its public key, forge
handles, capabilities, and trust scores, signed with the agent's Ed25519
private key — verifiable by any party without a centralized registry.

## Provisioner (Forgejo admin + user APIs)

```go
func NewProvisioner(cfg ProvisionerConfig) (*Provisioner, error)
func DefaultProvisionerConfig() ProvisionerConfig
func (p *Provisioner) BaseURL() string
func (p *Provisioner) Config() ProvisionerConfig
func (p *Provisioner) DryRun() bool
func (p *Provisioner) Close() error
func (p *Provisioner) CreateUser(req *CreateUserRequest) (*ForgejoAccount, error)
func (p *Provisioner) GetAccount(name string) (*ForgejoAccount, error)
func (p *Provisioner) RegisterKey(agentName, tempPassword, publicKey, title string) (*SSHKey, error)
func (p *Provisioner) CreateToken(agentName, adminUser, adminPassword string, req *CreateTokenRequest) (*AccessToken, error)
func (p *Provisioner) RevokeToken(agentName, adminUser, adminPassword string, tokenID int64) error
```

Thin HTTP client over the Forgejo admin + user APIs. Every public method maps
1:1 to a documented Forgejo endpoint (§9.2). Owns an `*http.Client`,
`RateLimiter`, and `RetryPolicy`.

## Syncer (orchestration)

```go
func NewSyncer(cfg ProvisionerConfig, lg Logger) (*Syncer, error)
func (s *Syncer) Sync(kf *KnownFriends, opts SyncOptions) ([]ProvisioningResult, error)
func (s *Syncer) ProvisionOne(a *Agent, opts SyncOptions) (ProvisioningResult, error)
func (s *Syncer) DeprovisionOne(a *Agent, opts SyncOptions) (ProvisioningResult, error)
func (s *Syncer) KeyGenOnly(a *Agent) (ProvisioningResult, error)
func (s *Syncer) Provisioner() *Provisioner
func (s *Syncer) State() *StateFile
```

Orchestrates a sync run; constructed once per CLI invocation (state file is
not concurrency-safe).

## Forgejo OAuth2 Registration (ID-002)

```go
func NewForgejoOAuthRegistrar(baseURL, username, password string) *ForgejoOAuthRegistrar
func (r *ForgejoOAuthRegistrar) RegisterOAuthApp(ctx context.Context, hid *HID, redirectURI string) (*ForgejoOAuthApp, error)
func (r *ForgejoOAuthRegistrar) GetOAuthApp(ctx context.Context, appID int64) (*ForgejoOAuthApp, error)
func (r *ForgejoOAuthRegistrar) ListOAuthApps(ctx context.Context) ([]ForgejoOAuthApp, error)
func (r *ForgejoOAuthRegistrar) DeleteOAuthApp(ctx context.Context, appID int64) error
func (r *ForgejoOAuthRegistrar) ExchangeToken(ctx context.Context, clientID, clientSecret, code, redirectURI string) (*OAuthTokenResponse, error)
func (r *ForgejoOAuthRegistrar) WithHTTPClient(c *http.Client) *ForgejoOAuthRegistrar
```

Agents prove identity by registering an OAuth2 application in Forgejo,
exchanging authorization tokens, and binding the Forgejo app to the agent's
Ed25519 HID via a signed challenge. Safe for concurrent use.

```go
func NewOAuthCredentialStore(path string) *OAuthCredentialStore
func (s *OAuthCredentialStore) Store(fingerprint string, app *ForgejoOAuthApp)
func (s *OAuthCredentialStore) Get(fingerprint string) (*ForgejoOAuthApp, bool)
func (s *OAuthCredentialStore) Delete(fingerprint string)
func (s *OAuthCredentialStore) List() []string
func (s *OAuthCredentialStore) Count() int
func (s *OAuthCredentialStore) Load() error
func (s *OAuthCredentialStore) Save() error
```

Persists Forgejo OAuth2 credentials keyed by HID fingerprint (JSON file,
atomic writes) so components look up an agent's Forgejo identity without
re-registration.

## Known Friends

```go
const DefaultKnownFriendsPath = "~/.helix/known-friends.json"
func LoadKnownFriends(path string) (*KnownFriends, error)
```

## Key Management and Rotation

```go
func DefaultRotationPolicies() map[KeyType]RotationPolicy
func EvaluateKey(info KeyInfo, policy RotationPolicy, now time.Time) (needsRotation bool, reason RotationReason, urgency RotationUrgency)
func FormatRotationPlan(plan *RotationPlan) string
func GenerateTempPassword() (string, error)
func HashKey(rawKey string) string
func MaskToken(token string) string
func VerifyKeyHash(rawKey, storedHash string) bool
func NewAgentKeyRegistry() *AgentKeyRegistry
```

## Typed Errors

```go
func NewAPIError(msg string, cause error) *TypedError
func NewConfigError(msg string, cause error) *TypedError
func NewInternalError(msg string, cause error) *TypedError
func NewNetworkError(msg string, cause error) *TypedError
func NewPartialError(msg string, cause error) *TypedError
```

## Key Types

`AccessToken`, `Agent`, `AgentIdentity`, `AgentKeyRegistry`,
`AgentPermission`, `AgentState`, `AgentStatus`, `AgentTier`,
`CapabilityClaim`, `CreateOAuthAppRequest`, `CreateTokenRequest`,
`CreateUserRequest`, `ErrorKind`, `ForgeHandle`, `ForgejoAccount`,
`ForgejoOAuthApp`, `HID`, `KeyInfo`, `KeyPair`, `KeyStatus`, `KeyType`,
`KnownFriends`, `ModelPreferences`, `NostrCapability`, `NostrEvent`,
`NostrMetadata`, `OAuthBindingProof`, `OAuthTokenResponse`,
`PermissionDelta`, `PermissionExpansion`, `PermissionSet`,
`ProvisionerConfig`, `ProvisioningResult`, `RateLimiter`, `RetryPolicy`,
`RotationAction`, `RotationPlan`, `RotationPolicy`, `RotationReason`,
`RotationUrgency`, `SSHKey`, `Signature`, `StateFile`, `SyncAction`,
`SyncOptions`, `TierTransition`, `TrustSnapshot`.

## Example

```go
cfg := identity.DefaultProvisionerConfig()
cfg.ForgejoURL = "http://localhost:3030"
prov, err := identity.NewProvisioner(cfg)
acct, err := prov.CreateUser(identity.NewCreateUserRequest(agent, tempPassword))
```

## Full exported signatures (from `go doc`)

```go
package identity // import "github.com/totalwindupflightsystems/helix/pkg/identity"

Package identity — Forgejo OAuth2 registration layer for Helix agent identities
(ID-002: Forgejo OAuth Registration).

Agents prove identity by registering an OAuth2 application in Forgejo,
exchanging authorization tokens, and cryptographically binding the Forgejo app
to the agent's Ed25519 HID via a signed challenge. The resulting credentials
(client_id + client_secret) are stored keyed by the HID fingerprint so that any
component can look up an agent's Forgejo identity without re-registration.

Forgejo v1.21.11+ endpoints used:

    POST /api/v1/user/applications/oauth2 – create OAuth2 app
    GET  /api/v1/user/applications/oauth2/{id} – get app details
    DELETE /api/v1/user/applications/oauth2/{id} – delete app
    POST /login/oauth/access_token – exchange authorization code for token

Package identity — Helix Identity Document (HID) types and cryptographic
operations (SPEC-022: Portable Agent Identity).

Every Helix agent gets a self-signed JSON HID containing its public key, forge
handles, capabilities, and trust scores. The HID is signed with the agent's
Ed25519 private key and can be verified by any party without a centralized
registry.

This file defines AgentIdentity, HID, and all supporting types, plus the core
Sign / Verify / Import / Export / Fingerprint operations.

Package identity provides the Nostr compatibility bridge for Helix Identity
Documents (SPEC-022: Portable Agent Identity).

Package identity implements provisioning of Helix agent accounts in a
self-hosted Forgejo instance.

This file defines all data models, enums, constructors, and the ED25519 keypair
serialization helpers used across the identity subsystem. The HTTP transport
lives in provisioner.go; orchestration lives in syncer.go.

Design constraints:
  - Only stdlib + github.com/spf13/cobra may be imported.
  - golang.org/x/crypto is deliberately avoided; OpenSSH wire format is
    serialized by hand using crypto/ed25519 + crypto/x509.
  - No secrets are stored in source. All credentials arrive via env vars or
    runtime flags and are never marshaled to disk in plaintext beyond the
    per-agent private key file (mode 0600, owned by the operator).

const ExitOK = 0 ...
const DefaultKnownFriendsPath = "~/.helix/known-friends.json"
const DefaultSSHKeyDir = "~/.helix/keys"
const DefaultStatePath = "~/.helix/state.json"
const NostrKindMetadata = 0
const PATName = "helix-identity-pat"
const StateVersion = 1
var ErrNotImplemented = errors.New(...)
func DefaultRotationPolicies() map[KeyType]RotationPolicy
func EvaluateKey(info KeyInfo, policy RotationPolicy, now time.Time) (needsRotation bool, reason RotationReason, urgency RotationUrgency)
func FormatRotationPlan(plan *RotationPlan) string
func GenerateTempPassword() (string, error)
func HashKey(rawKey string) string
func MaskToken(token string) string
func VerifyBindingProof(hid *HID, proof *OAuthBindingProof) (bool, error)
func VerifyKeyHash(rawKey, storedHash string) bool
type AccessToken struct{ ... }
type Agent struct{ ... }
type AgentIdentity struct{ ... }
    func ImportHID(path string) (*AgentIdentity, error)
    func NewAgentIdentity(name string) (*AgentIdentity, ed25519.PrivateKey, error)
type AgentKeyRegistry struct{ ... }
    func NewAgentKeyRegistry() *AgentKeyRegistry
type AgentPermission struct{ ... }
    func DefaultPermission() AgentPermission
type AgentState struct{ ... }
type AgentStatus string
    const StatusActive AgentStatus = "active" ...
type AgentTier string
    const TierPro AgentTier = "pro" ...
type CapabilityClaim struct{ ... }
type CreateOAuthAppRequest struct{ ... }
type CreateTokenRequest struct{ ... }
    func NewCreateTokenRequest(a *Agent) *CreateTokenRequest
type CreateUserRequest struct{ ... }
    func NewCreateUserRequest(a *Agent, tempPassword string) *CreateUserRequest
type ErrorKind string
    const ErrKindConfig ErrorKind = "config" ...
type ForgeHandle struct{ ... }
type ForgejoAccount struct{ ... }
type ForgejoOAuthApp struct{ ... }
type ForgejoOAuthRegistrar struct{ ... }
    func NewForgejoOAuthRegistrar(baseURL, username, password string) *ForgejoOAuthRegistrar
type HID struct{ ... }
type KeyInfo struct{ ... }
type KeyPair struct{ ... }
    func GenerateKeyPair() (*KeyPair, error)
type KeyStatus string
    const KeyStatusActive KeyStatus = "active" ...
type KeyType string
    const KeyTypeSSH KeyType = "ssh" ...
type KnownFriends struct{ ... }
    func LoadKnownFriends(path string) (*KnownFriends, error)
type Logger interface{ ... }
type ModelPreferences struct{ ... }
type NostrCapability struct{ ... }
type NostrEvent struct{ ... }
    func NewNostrEventFromHID(id *AgentIdentity) (*NostrEvent, error)
type NostrMetadata struct{ ... }
type OAuthBindingProof struct{ ... }
    func CreateBindingProof(hid *HID, privKey ed25519.PrivateKey, clientID, forgeURL string) (*OAuthBindingProof, error)
type OAuthCredentialStore struct{ ... }
    func NewOAuthCredentialStore(path string) *OAuthCredentialStore
type OAuthTokenResponse struct{ ... }
type PermissionDelta struct{ ... }
type PermissionExpansion struct{}
    func NewPermissionExpansion() *PermissionExpansion
type PermissionSet struct{ ... }
type Provisioner struct{ ... }
    func NewProvisioner(cfg ProvisionerConfig) (*Provisioner, error)
type ProvisionerConfig struct{ ... }
    func DefaultProvisionerConfig() ProvisionerConfig
type ProvisioningResult struct{ ... }
type RateLimiter struct{ ... }
    func NewRateLimiter(rate, burst int) *RateLimiter
type RetryPolicy struct{ ... }
    func DefaultRetryPolicy() RetryPolicy
type RotationAction struct{ ... }
type RotationPlan struct{ ... }
type RotationPolicy struct{ ... }
type RotationReason string
    const RotationAgeExceeded RotationReason = "age_exceeded" ...
type RotationUrgency string
    const UrgencyImmediate RotationUrgency = "immediate" ...
type SSHKey struct{ ... }
type Signature struct{ ... }
type StateFile struct{ ... }
    func NewStateFile() *StateFile
type SyncAction string
    const ActionCreated SyncAction = "created" ...
type SyncOptions struct{ ... }
type Syncer struct{ ... }
    func NewSyncer(cfg ProvisionerConfig, lg Logger) (*Syncer, error)
type TierTransition struct{ ... }
type TrustSnapshot struct{ ... }
type TypedError struct{ ... }
    func NewAPIError(msg string, cause error) *TypedError
    func NewConfigError(msg string, cause error) *TypedError
    func NewInternalError(msg string, cause error) *TypedError
    func NewNetworkError(msg string, cause error) *TypedError
    func NewPartialError(msg string, cause error) *TypedError
```
