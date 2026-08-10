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
