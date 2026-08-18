# pkg/verify — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/verify"`

Shadow deployment, canary promotion, behavior diff, auto-rollback

## Signatures (from `go doc`)

```go
package verify // import "github.com/totalwindupflightsystems/helix/pkg/verify"

Package verify implements post-merge production verification and behavior
contract monitoring per specs/production-verification.md.

Behavior contracts are YAML assertions committed alongside code that define
expected runtime behavior. The surveillance system continuously checks these
contracts and triggers auto-rollback on breach, integrating with the trust model
for agent accountability.

const ActionNotifyOnly = "notify_only" ...
const MinObservationFactor = 1.0
const SuccessRateTolerance = 0.001
func AllPassed(results []CheckResult) bool
func BreachSummary(data BreachReportData) string
func ComputeCanaryPercentage(tier string) float64
func FormatBreachComment(n *BreachNotification) string
func FormatBreachReport(data BreachReportData) string
func ShadowAutoRollbackTriggers(prodSuccessRate, shadowSuccessRate float64, prodP99, shadowP99 float64, ...) string
func TotalCanaryDuration(steps []CanaryStep) time.Duration
type AgentStatus struct{ ... }
type AlertEscalation struct{ ... }
    func NewAlertEscalation(sustainedThreshold, rollbackThreshold time.Duration) *AlertEscalation
type Assertion struct{ ... }
type AssertionOperator int
    const OpInvalid AssertionOperator = iota ...
    func Operator(s string) (AssertionOperator, error)
type BehaviorContract struct{ ... }
    func ParseContract(data []byte) (*BehaviorContract, error)
type Breach struct{ ... }
type BreachAction string
    const BreachActionRollback BreachAction = "rollback" ...
type BreachNotification struct{ ... }
type BreachReportData struct{ ... }
type BreachReporter struct{}
    func NewBreachReporter() *BreachReporter
type BreachSeverity string
    const SeverityNone BreachSeverity = "none" ...
type CanaryPromoter struct{ ... }
    func NewCanaryPromoter() *CanaryPromoter
type CanaryStep struct{ ... }
    func CanarySchedule(tier string) (shadowDuration time.Duration, steps []CanaryStep)
type ChannelResult struct{ ... }
type CheckName string
    const CheckContractPassed CheckName = "behavior_contract" ...
type CheckResult struct{ ... }
type Checker struct{}
    func NewChecker() *Checker
type ContractBody struct{ ... }
type DailySummary struct{ ... }
type DegradationDirection string
    const DegradationNone DegradationDirection = "none" ...
type DegradationReport struct{ ... }
type DeliveryStatus string
    const StatusDelivered DeliveryStatus = "delivered" ...
type DeploymentPhase string
    const PhaseShadow DeploymentPhase = "shadow" ...
    func PhaseFromState(state ShadowState) DeploymentPhase
type DeploymentTrace struct{ ... }
type DeploymentTracePipeline struct{ ... }
    func NewDeploymentTracePipeline() *DeploymentTracePipeline
type DifferentialReport struct{ ... }
type DriftAssessment struct{ ... }
    func AssessDeployment(dep *ShadowDeployment, sensitivity DriftSensitivity) *DriftAssessment
type DriftDetector struct{ ... }
    func NewDriftDetector(baseline MetricsSnapshot, opts ...DriftDetectorOption) *DriftDetector
type DriftDetectorOption func(*DriftDetector)
    func WithMaxSamples(n int) DriftDetectorOption
    func WithSensitivity(s DriftSensitivity) DriftDetectorOption
    func WithWindowSize(d time.Duration) DriftDetectorOption
type DriftReport struct{ ... }
    func DetectDrift(baseline, current map[string]float64, thresholdPct float64) []DriftReport
type DriftSensitivity struct{ ... }
    func DefaultSensitivity() DriftSensitivity
type EscalationLevel string
    const EscalationNone EscalationLevel = "none" ...
type FailedAssertion struct{ ... }
type ForgejoPRNotifier struct{ ... }
type IncidentStoreNotifier struct{ ... }
type LangFuseSpanExport struct{ ... }
type LangFuseTraceExport struct{ ... }
type LongRunningMonitor struct{ ... }
    func NewLongRunningMonitor(windowDays int, thresholds LongRunningThresholds) *LongRunningMonitor
type LongRunningThresholds struct{ ... }
    func DefaultLongRunningThresholds() LongRunningThresholds
type MetricDegradation struct{ ... }
type MetricDelta struct{ ... }
type MetricDriftReport struct{ ... }
type MetricsSnapshot struct{ ... }
type Monitor struct{ ... }
    func NewMonitor() *Monitor
type NotificationDispatcher struct{ ... }
    func NewNotificationDispatcher(channels ...Notifier) *NotificationDispatcher
type NotificationResult struct{ ... }
type Notifier interface{ ... }
type PromotionCheck struct{ ... }
type PromotionDecision string
    const PromotionReady PromotionDecision = "READY" ...
type PromotionResult struct{ ... }
type RampStep struct{ ... }
    func AutoRampSchedule(tier string) []RampStep
type ShadowConfig struct{ ... }
    func DefaultShadowConfig() ShadowConfig
type ShadowDeployment struct{ ... }
type ShadowManager struct{ ... }
    func NewShadowManager() *ShadowManager
type ShadowState string
    const StateIdle ShadowState = "idle" ...
type SpanInput struct{ ... }
type SteadyStateAggregator struct{ ... }
    func NewSteadyStateAggregator(opts ...SurveillanceOption) *SteadyStateAggregator
type SurveillanceConfig struct{ ... }
    func DefaultSurveillanceConfig() SurveillanceConfig
type SurveillanceEvent struct{ ... }
type SurveillanceOption func(*SteadyStateAggregator)
    func WithSurveillanceConfig(c SurveillanceConfig) SurveillanceOption
    func WithSurveillanceDispatcher(d *NotificationDispatcher) SurveillanceOption
    func WithSurveillanceMonitor(m *Monitor) SurveillanceOption
type SurveillanceStatus string
    const StatusHealthy SurveillanceStatus = "healthy" ...
type TraceSpan struct{ ... }
type TraceStage string
    const StageCommit TraceStage = "commit" ...
type TraceStatus string
    const TraceStatusSuccess TraceStatus = "success" ...
type TraceSummary struct{ ... }
type TrendDirection string
    const TrendStable TrendDirection = "stable" ...
type TrustLedgerNotifier struct{ ... }
```

## Related

- [docs/api/README.md](README.md) — package index
