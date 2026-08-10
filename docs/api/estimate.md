# pkg/estimate — Cost Estimator Reference

`import "github.com/totalwindupflightsystems/helix/pkg/estimate"`

Pre-flight cost estimator: estimates the token cost of an agent task BEFORE
execution, checks it against the agent's remaining weekly budget, and either
approves, denies, or escalates for human approval. Estimation is cache-aware
(fresh input tokens at full price, cache-hit tokens at 10x cheaper, cache-write
tokens at a future discount).

Design constraints (specs/cost-estimator.md §4): stdlib + cobra + yaml.v3 only;
no hardcoded prices (all pricing from `~/.helix/pricing.yaml`); estimates
always round UP to the nearest cent.

## Estimator

```go
type Estimator struct {
    Pricing *PricingYAML
    Tier    string // "pro", "flash", or "cold"
}
func NewEstimator(pricing *PricingYAML, tier string) *Estimator
func (e *Estimator) Estimate(task TaskDesc) (CostEstimate, error)
```

Tier cache ratios (spec §7.3): `pro` 60% hit / 50% write (default);
`flash` 80% hit / 70% write; `cold` 0% hit / 50% write (first 10 tasks,
spec §12.3). Cold-start selection is driven by `budget.IsNewAgent` in the CLI
layer.

## Approval Gate

```go
func NewApprovalGate(pricing *PricingYAML) *ApprovalGate
func (g *ApprovalGate) Evaluate(budget BudgetInfo, estimate CostEstimate) GateApprovalResult
func (g *ApprovalGate) EvaluateWithTrust(budget BudgetInfo, estimate CostEstimate, trustLevel int) GateApprovalResult
func (g *ApprovalGate) BatchEvaluate(budgets []BudgetInfo, estimate CostEstimate) map[string]GateApprovalResult
```

Spec §8.1 approval gate engine: evaluates estimated cost against remaining
budget and returns a structured decision with remaining-after projection and
remediation options. With a `PricingYAML` reference it suggests cheaper model
alternatives when a task is blocked.

## Budget Helpers

```go
func CheckBudget(budget BudgetInfo, estimate CostEstimate) ApprovalDecision
func RemainingBudget(b BudgetInfo) float64
func IsNewAgent(b BudgetInfo) bool
func ApprovalExitCode(d ApprovalDecision) int
```

## OpenRouter Key Lookup

```go
func NewOpenRouterClient(baseURL string) *OpenRouterClient
func (c *OpenRouterClient) GetKeyInfo(ctx context.Context, apiKey string) (*KeyInfo, error)
func (c *OpenRouterClient) GetKeyUsage(ctx context.Context, apiKey string) (float64, error)
func (c *OpenRouterClient) GetKeyLimit(ctx context.Context, apiKey string) (float64, error)
func (c *OpenRouterClient) GetKeyRemaining(ctx context.Context, apiKey string) (float64, error)
```

Queries `GET https://openrouter.ai/api/v1/key` with the agent's API key as a
Bearer token for real-time budget data (spec §9.1).

## Reconciliation and Drift

```go
func ActualCost(usage Usage, pricing *PricingYAML, provider, model string) (CostEstimate, error)
func ReconcileAgent(agentID string, usage Usage, pricing *PricingYAML, provider, model string, ...) (ReconciliationResult, error)
func ReconcilePipeline(agentID string, usage Usage, pricing *PricingYAML, provider, model string, ...) (ReconciliationResult, BudgetInfo, error)
func ReconcileDrift(estimated CostEstimate, actual CostEstimate) float64
func CheckRecalibration(records []EstimationLog, threshold float64, minTasks int) (bool, float64)
```

## Persistence

```go
func LoadPricing(path string) (*PricingYAML, error)
func WriteEstimationRecord(path string, entry EstimationLog) error
func ReadEstimationRecords(path string) ([]EstimationLog, error)
func NewEstimationLogger(verbose bool) *EstimationLogger
func NewEstimationLoggerWithWriter(verbose bool, w io.Writer) *EstimationLogger
```

## Periods and Calibration

```go
func DefaultPeriodConfig() PeriodConfig
func NewPeriodManager(config PeriodConfig) *PeriodManager
func NewCalibrator() *Calibrator
func NewCostAttributionModel() *CostAttributionModel
func NewDriftTracker() *DriftTracker
```

## Formatting and Errors

```go
const DriftThresholdPct = 10.0
var ErrAuthFailed = fmt.Errorf("openrouter: authentication failed (HTTP 401) — key may be dead")
var ErrRateLimited = fmt.Errorf("openrouter: rate limited (HTTP 429)")
func FormatCostReport(r CostReport) string
func FormatDriftReport(r DriftReport) string
func FormatGateResult(r GateApprovalResult) string
func FormatPeriodInfo(info PeriodInfo) string
func FormatReconciliation(r ReconciliationResult) string
func AllBlocked(results map[string]GateApprovalResult) bool
func AnyApproved(results map[string]GateApprovalResult) bool
func ExhaustionAction(level BudgetExhaustionLevel) string
```

## Key Types

`ApprovalDecision`, `ApprovalStatus`, `BlockedOption`,
`BudgetExhaustionLevel`, `BudgetInfo`, `CacheRatios`, `CalibrationRecord`,
`Calibrator`, `CheaperModel`, `CostAttribute`, `CostAttributionModel`,
`CostEntry`, `CostEstimate`, `CostHierarchyTier`, `CostReport`, `DriftEntry`,
`DriftReport`, `DriftTracker`, `EstimationLog`, `EstimationLogger`,
`GateApprovalResult`, `KeyInfo`, `ModelPrice`, `PeriodConfig`, `PeriodInfo`,
`PeriodManager`, `PricingYAML`, `ProviderPricing`, `ReconciliationResult`,
`TaskDefaults`, `TaskDesc`, `TaskType`, `TokenEstimate`, `Usage`.

## Example

```go
pricing, err := estimate.LoadPricing("~/.helix/pricing.yaml")
est := estimate.NewEstimator(pricing, "pro")
cost, err := est.Estimate(estimate.TaskDesc{
    TaskType: estimate.TaskSpec,
    Text:     "Write a Go HTTP server",
    Model:    "deepseek-v4-pro",
})
gate := estimate.NewApprovalGate(pricing)
decision := gate.Evaluate(budget, cost)
```
