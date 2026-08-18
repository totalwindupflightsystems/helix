# pkg/integration — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/integration"`

End-to-end integration test harnesses

## Signatures (from `go doc`)

```go
package integration // import "github.com/totalwindupflightsystems/helix/pkg/integration"

Package integration provides end-to-end integration test harnesses for the
Helix platform. These tests exercise the full agent lifecycle against real local
services (Forgejo, Chimera). They skip gracefully when the required service is
unreachable, so they pass cleanly in environments without one (e.g. CI) and run
for real against a live local Forgejo.

Usage:

    go test -short -count=1 ./pkg/integration/...   # run E2E suite (skips if Forgejo unreachable)
    go test -count=1 ./pkg/integration/...          # run full integration suite (TestFullLoop; skips if Forgejo unreachable)

Environment variables:

    GOAWAY=1 — skip real network calls even when not in -short mode
    FORGEJO_URL — override default http://localhost:3000
    CHIMERA_URL — override default http://localhost:8765

const CodeChimeraUnavailable = "CHIMERA_UNAVAILABLE" ...
const DefaultForgejoURL = "http://localhost:3030" ...
var ErrAxiomAuthFailed = fmt.Errorf("axiom: authentication failed (401)") ...
var ErrChimeraAuthFailed = fmt.Errorf("chimera: authentication failed (401)") ...
var ErrGitReinsAuthFailed = fmt.Errorf("gitreins: authentication failed (401)") ...
var ErrHivemindAuthFailed = fmt.Errorf("hivemind: authentication failed (401)") ...
var ErrMusterAuthFailed = fmt.Errorf("muster: authentication failed (401)") ...
var ErrAlreadyExists = fmt.Errorf("integration: user already exists")
func DefaultServiceEndpoints() map[string]ServiceEndpoint
func GetRetryAfter(err error) time.Duration
func IsCode(err error, code string) bool
func IsRetryable(err error) bool
type AcceptanceCriterion struct{ ... }
type AccessToken struct{ ... }
type AgentListing struct{ ... }
type AgentReview struct{ ... }
type AttackVector struct{ ... }
type Attestation struct{ ... }
type AuthConfig struct{ ... }
type AxiomAdapter interface{ ... }
type AxiomClient struct{ ... }
    func NewAxiomClient(baseURL, token string, opts ...AxiomClientOption) *AxiomClient
type AxiomClientOption func(*AxiomClient)
    func WithAxiomHTTPClient(c *http.Client) AxiomClientOption
    func WithAxiomTimeout(d time.Duration) AxiomClientOption
type AxiomResult struct{ ... }
type AxiomStatus struct{ ... }
type BudgetExhaustedError struct{ ... }
type CallerCallee struct{ ... }
type CheatAttempt struct{ ... }
type CheckResult struct{ ... }
type ChimeraAdapter interface{ ... }
type ChimeraAdapterClient struct{ ... }
    func NewChimeraAdapterClient(baseURL, token string, opts ...ChimeraClientOption) *ChimeraAdapterClient
type ChimeraClient struct{ ... }
    func NewChimeraClient(baseURL string) (*ChimeraClient, error)
type ChimeraClientOption func(*ChimeraAdapterClient)
    func WithChimeraHTTPClient(c *http.Client) ChimeraClientOption
    func WithChimeraTimeout(d time.Duration) ChimeraClientOption
type ChimeraHealth struct{ ... }
type ChimeraModel struct{ ... }
type ChimeraPR struct{ ... }
type ChimeraTrace struct{ ... }
type ChimeraVerdict struct{ ... }
type CircuitBreaker struct{ ... }
    func NewCircuitBreaker(maxFailures int, resetTimeout time.Duration) *CircuitBreaker
type CircuitOpenError struct{ ... }
type CmdResult struct{ ... }
type ConscientiousnessAdapter interface{ ... }
type ConscientiousnessClient struct{ ... }
    func NewConscientiousnessClient(baseURL, apiKey string, opts ...ConscientiousnessClientOption) *ConscientiousnessClient
type ConscientiousnessClientOption func(*ConscientiousnessClient)
    func WithConscientiousnessHTTPClient(c *http.Client) ConscientiousnessClientOption
    func WithConscientiousnessTimeout(d time.Duration) ConscientiousnessClientOption
type ConscientiousnessHealth struct{ ... }
type ConscientiousnessPR struct{ ... }
type ConscientiousnessVerdict struct{ ... }
type CostBreakdown struct{ ... }
type CreateUserRequest struct{ ... }
type ErrorContext struct{ ... }
type EvalOpts struct{ ... }
type EvalResult struct{ ... }
type Evidence struct{ ... }
type Finding struct{ ... }
type ForgejoAccount struct{ ... }
type ForgejoClient struct{ ... }
    func NewForgejoClient(baseURL, adminUser, adminPass string) (*ForgejoClient, error)
type Formation struct{ ... }
type GenerateOpts struct{ ... }
type GitReinsAdapter interface{ ... }
type GitReinsAdapterClient struct{ ... }
    func NewGitReinsAdapterClient(baseURL, apiKey string, opts ...GitReinsClientOption) *GitReinsAdapterClient
type GitReinsClientOption func(*GitReinsAdapterClient)
    func WithGitReinsHTTPClient(c *http.Client) GitReinsClientOption
    func WithGitReinsPricing(input, output, cacheRead, cacheWrite float64) GitReinsClientOption
    func WithGitReinsTimeout(d time.Duration) GitReinsClientOption
type GuardOpts struct{ ... }
type GuardResult struct{ ... }
type HiveTask struct{ ... }
type HivemindAdapter interface{ ... }
type HivemindClient struct{ ... }
    func NewHivemindClient(baseURL, token string, opts ...HivemindClientOption) *HivemindClient
type HivemindClientOption func(*HivemindClient)
    func WithHivemindHTTPClient(c *http.Client) HivemindClientOption
    func WithHivemindTimeout(d time.Duration) HivemindClientOption
type HivemindHealth struct{ ... }
type IntegrationTestSuite struct{ ... }
    func NewIntegrationTestSuite() *IntegrationTestSuite
type KobayashiMaruAdapter interface{ ... }
type LLMUsage struct{ ... }
type LangFuseAdapter interface{ ... }
type LangFuseClient struct{ ... }
    func NewLangFuseClient(baseURL, publicKey, secretKey string, opts ...LangFuseClientOption) *LangFuseClient
type LangFuseClientOption func(*LangFuseClient)
    func WithLangFuseHTTPClient(c *http.Client) LangFuseClientOption
    func WithLangFuseTimeout(d time.Duration) LangFuseClientOption
type LangFuseGeneration struct{ ... }
type LangFuseHealth struct{ ... }
type LangFuseIngestResult struct{ ... }
type LangFuseObservation struct{ ... }
type LangFuseTrace struct{ ... }
type LangFuseUsage struct{ ... }
type MCPTool struct{ ... }
type MaruMetrics struct{ ... }
type MemoryEntry struct{ ... }
type Mitigation struct{ ... }
type MusterAdapter interface{ ... }
type MusterClient struct{ ... }
    func NewMusterClient(baseURL, token string, opts ...MusterClientOption) *MusterClient
type MusterClientOption func(*MusterClient)
    func WithMusterHTTPClient(c *http.Client) MusterClientOption
    func WithMusterTimeout(d time.Duration) MusterClientOption
type MusterHealth struct{ ... }
type OpenCodeAdapter interface{ ... }
type OpenCodeHealth struct{ ... }
type OpenCodeTokens struct{ ... }
type PromptFooAdapter interface{ ... }
type PromptFooEvalResult struct{ ... }
type PromptFooPromptDef struct{ ... }
type PromptFooRunOpts struct{ ... }
type PromptFooTestResult struct{ ... }
type ReviewOpts struct{ ... }
type RunOpts struct{ ... }
type SSHKey struct{ ... }
type Scenario struct{ ... }
type ScenarioFinding struct{ ... }
type ScenarioOpts struct{ ... }
type ScenarioResult struct{ ... }
type ServiceEndpoint struct{ ... }
type ServiceError struct{ ... }
    func ClassifyError(caller, callee string, statusCode int, ctx ErrorContext) *ServiceError
    func ClassifyHTTP(service string, statusCode int, body string) *ServiceError
    func NewAuthFailedError(service string, cause error) *ServiceError
    func NewBranchConflictError(branch string) *ServiceError
    func NewBudgetExhaustedError(cost, remaining float64) *ServiceError
    func NewChimeraUnavailableError(cause error) *ServiceError
    func NewConnectionRefusedError(service string, attempt, maxAttempts int, retryAfter time.Duration, ...) *ServiceError
type ServiceUnavailableError struct{ ... }
type Session struct{ ... }
type SessionOpts struct{ ... }
type SessionResult struct{ ... }
type StageResult struct{ ... }
type Task struct{ ... }
type TaskResult struct{ ... }
type ToolParam struct{ ... }
type ToolResult struct{ ... }
type TraceFilter struct{ ... }
type Verdict struct{ ... }
type WorkItem struct{ ... }
```

## Related

- [docs/api/README.md](README.md) — package index
