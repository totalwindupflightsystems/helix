# pkg/forgejo — Forgejo Client Reference

`import "github.com/totalwindupflightsystems/helix/pkg/forgejo"`

Forgejo REST API client wrapper used by `helix-identity`, `helix-negotiate`,
and `helix-marketplace`. All methods use a shared `doRequest` core that
inherits BasicAuth, circuit-breaker, and retryable-error handling.

## Client

```go
func NewClient(baseURL, username, password string) *Client
```

`Client` wraps the Forgejo REST API with BasicAuth and circuit-breaker support.
Constructor options (chainable, all optional):

```go
func (c *Client) WithCircuitBreaker(cb CircuitBreaker) *Client
func (c *Client) WithHTTPClient(client *http.Client) *Client
func (c *Client) WithRateLimiter(rl RateLimiter) *Client
```

Branch and PR lifecycle (used by the dispatcher Ralph Loop):

```go
func (c *Client) CreateBranch(ctx context.Context, owner, repo, newBranchName, fromRef string) (*CreateBranchResponse, error)
func (c *Client) DeleteBranch(ctx context.Context, owner, repo, branch string) error
func (c *Client) CreatePR(ctx context.Context, owner, repo, head, base, title, body string) (*CreatePRResponse, error)
func (c *Client) ClosePR(ctx context.Context, owner, repo string, prNumber int64) (*CreatePRResponse, error)
func (c *Client) ListPRs(ctx context.Context, owner, repo, state string) ([]PullRequest, error)
func (c *Client) MergePR(ctx context.Context, owner, repo string, prNumber int64) error
func (c *Client) GetPRReviews(ctx context.Context, owner, repo string, prNumber int64) ([]PRReview, error)
func (c *Client) CreatePRReview(ctx context.Context, owner, repo string, prNumber int64, ...) error
func (c *Client) PostCommitStatus(ctx context.Context, owner, repo, sha string, status CommitStatusRequest) error
```

Users, repos, SSH keys, and PATs (agent provisioning):

```go
func (c *Client) CreateUser(ctx context.Context, req CreateUserRequest) (*User, error)
func (c *Client) GetUser(ctx context.Context, username string) (*User, error)
func (c *Client) DeleteUser(ctx context.Context, username string) error
func (c *Client) CreateRepo(ctx context.Context, req CreateRepoRequest) (*Repository, error)
func (c *Client) GetRepo(ctx context.Context, owner, repo string) (*Repository, error)
func (c *Client) DeleteRepo(ctx context.Context, owner, repo string) error
func (c *Client) CreateSSHKey(ctx context.Context, title, publicKey string) (*SSHKey, error)
func (c *Client) ListSSHKeys(ctx context.Context) ([]SSHKey, error)
func (c *Client) CreatePAT(ctx context.Context, username, tokenName string) (*PAT, error)
```

## Branch Protection

```go
func NewBranchProtectionEnforcer(client *Client, config TierProtectionConfig) *BranchProtectionEnforcer
func NewBranchProtectionEnforcerWithDefaults(client *Client) *BranchProtectionEnforcer
func DefaultTierProtectionConfig() TierProtectionConfig
```

Configures Forgejo branch protection rules per trust tier (specs
`SPECIFICATION.md` §13.2): agents push to `feat/*` branches, `main` is
protected with review requirements.

## Webhooks

```go
func NewWebhookHandler(opts ...WebhookOption) *WebhookHandler
func WithEventHandler(handler EventHandler) WebhookOption
func WithWebhookSecret(secret string) WebhookOption

func (h *WebhookHandler) HandleRequest(r *http.Request) WebhookResult
func (h *WebhookHandler) HandleBody(header http.Header, body []byte) WebhookResult
func (h *WebhookHandler) VerifySignature(header http.Header, body []byte) bool
```

Payload parsing helpers:

```go
func ParseWebhook(body []byte) (*WebhookPayload, error)
func ParsePRInfo(payload *WebhookPayload) (*PREventInfo, error)
func ParsePushInfo(payload *WebhookPayload) (*PushEventInfo, error)
func ParseReviewInfo(payload *WebhookPayload) (*ReviewEventInfo, error)
func ParseEventType(header http.Header) EventType
func MapEventType(event string) EventType
```

## PR Status Manager

```go
func NewPRStatusManager(client *Client) *PRStatusManager
```

## Notable Functions and Errors

```go
var ErrCircuitOpen = fmt.Errorf("circuit breaker open — service unavailable")
func IsAlreadyExists(err error) bool
func BranchRef(name string) string
func FormatDeploymentComment(dep *DeploymentStatus) string
func FormatReviewComment(v *ReviewVerdict) string
```

## Key Types

`APIError`, `Branch`, `BranchProtectionRule`, `CommitStatus`,
`CommitStatusRequest`, `CommitStatusState`, `CreateBranchRequest`,
`CreateBranchResponse`, `CreatePRRequest`, `CreatePRResponse`,
`CreatePRReviewRequest`, `CreateRepoRequest`, `CreateUserRequest`,
`DeploymentStatus`, `EventType`, `PAT`, `PREventInfo`, `PRReview`,
`ParsedReview`, `PullRequest`, `PushEventInfo`, `Repository`,
`ReviewEventInfo`, `ReviewVerdict`, `SSHKey`, `TierProtectionConfig`,
`TokenBucket`, `TrustTier`, `User`, `WebhookAction`, `WebhookPayload`,
`WebhookResult`, plus the `CircuitBreaker`, `EventHandler`, and
`RateLimiter` interfaces.

## Example

```go
client := forgejo.NewClient("http://localhost:3030", username, password)
repo, err := client.CreateRepo(ctx, forgejo.CreateRepoRequest{
    Name: "agent-repo", Private: true,
})
pr, err := client.CreatePR(ctx, owner, repo.Name,
    "feat/feature-branch", "main", "Title", "Body")
```

## Full exported signatures (from `go doc`)

```go
package forgejo // import "github.com/totalwindupflightsystems/helix/pkg/forgejo"

Package forgejo — branch.go

Branch/PR primitives used by the dispatcher Ralph Loop. These methods extend the
Client with the lifecycle operations needed to open a PR as part of a work item:
create a feature branch from a base ref, then open the PR targeting that base.

All methods use the same doRequest core as the rest of the package — they
inherit BasicAuth, circuit-breaker, and retryable-error handling.

Package forgejo — branch_protection.go

BranchProtectionEnforcer configures Forgejo branch protection rules per trust
tier, ensuring agents can only push to feat/* branches and main is protected
with appropriate review requirements.

Per specs/SPECIFICATION.md §13.2 (Day 9-10: scoped permissions):

    "Wire scoped permissions: feat/* push, main block, PR open"

Per specs/SPECIFICATION.md §5 (IAM):

    Agents push to branches, not main. Branch protection enforces this.

Package forgejo provides a Go client wrapper for the Forgejo REST API. Used by
helix-identity, helix-negotiate, and helix-marketplace.

Based on specs/cross-component-wiring.md §2 and specs/agent-identity.md.

Package forgejo — pull_request.go

PR primitives used by the dispatcher Ralph Loop. Mirrors the Forgejo REST API
for creating a pull request from an existing feature branch back into a base
branch.

Endpoints:

    POST /api/v1/repos/{owner}/{repo}/pulls

All methods use the same doRequest core as the rest of the package — they
inherit BasicAuth, circuit-breaker, and retryable-error handling.

var ErrCircuitOpen = fmt.Errorf("circuit breaker open — service unavailable")
func BranchRef(name string) string
func FormatDeploymentComment(dep *DeploymentStatus) string
func FormatReviewComment(v *ReviewVerdict) string
func IsAlreadyExists(err error) bool
type APIError struct{ ... }
type Branch struct{ ... }
type BranchProtectionEnforcer struct{ ... }
    func NewBranchProtectionEnforcer(client *Client, config TierProtectionConfig) *BranchProtectionEnforcer
    func NewBranchProtectionEnforcerWithDefaults(client *Client) *BranchProtectionEnforcer
type BranchProtectionRule struct{ ... }
type CircuitBreaker interface{ ... }
type Client struct{ ... }
    func NewClient(baseURL, username, password string) *Client
type CommitStatus struct{ ... }
type CommitStatusRequest struct{ ... }
type CommitStatusState string
    const StatusStatePending CommitStatusState = "pending" ...
type CreateBranchRequest struct{ ... }
type CreateBranchResponse struct{ ... }
type CreatePRRequest struct{ ... }
type CreatePRResponse struct{ ... }
type CreatePRReviewRequest struct{ ... }
type CreateRepoRequest struct{ ... }
type CreateUserRequest struct{ ... }
type DeploymentStatus struct{ ... }
type EventHandler interface{ ... }
type EventType string
    const EventPROpened EventType = "pull_request_opened" ...
    func MapEventType(event string) EventType
    func ParseEventType(header http.Header) EventType
type NoOpHandler struct{}
type NoopCircuitBreaker struct{}
type NoopRateLimiter struct{}
type PAT struct{ ... }
type PREventInfo struct{ ... }
    func ParsePRInfo(payload *WebhookPayload) (*PREventInfo, error)
type PRReview struct{ ... }
type PRStatusManager struct{ ... }
    func NewPRStatusManager(client *Client) *PRStatusManager
type ParsedReview struct{ ... }
    func ParsePRReviews(reviews []PRReview) []ParsedReview
    func ParseReviewBody(body string) *ParsedReview
type PullRequest struct{ ... }
type PushEventInfo struct{ ... }
    func ParsePushInfo(payload *WebhookPayload) (*PushEventInfo, error)
type RateLimiter interface{ ... }
type Repository struct{ ... }
type ReviewEventInfo struct{ ... }
    func ParseReviewInfo(payload *WebhookPayload) (*ReviewEventInfo, error)
type ReviewVerdict struct{ ... }
type SSHKey struct{ ... }
type TierProtectionConfig struct{ ... }
    func DefaultTierProtectionConfig() TierProtectionConfig
type TokenBucket struct{ ... }
    func NewTokenBucket(rps, burst int) *TokenBucket
type TrustTier string
    const TierProvisional TrustTier = "provisional" ...
type User struct{ ... }
type WebhookAction string
    const ActionProcessed WebhookAction = "processed" ...
type WebhookHandler struct{ ... }
    func NewWebhookHandler(opts ...WebhookOption) *WebhookHandler
type WebhookOption func(*WebhookHandler)
    func WithEventHandler(handler EventHandler) WebhookOption
    func WithWebhookSecret(secret string) WebhookOption
type WebhookPayload struct{ ... }
    func ParseWebhook(body []byte) (*WebhookPayload, error)
type WebhookResult struct{ ... }
```
