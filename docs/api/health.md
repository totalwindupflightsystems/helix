# pkg/health — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/health"`

Agent and platform health metrics

## Signatures (from `go doc`)

```go
package health // import "github.com/totalwindupflightsystems/helix/pkg/health"

Package health — agent_metrics.go

Per-agent Prometheus metrics collector implementing all 6 agent metrics from
specs/SPECIFICATION.md §8.4 (Agent metrics):

    helix_agent_tasks_total{agent, repo, status="completed|failed|blocked"}
    helix_agent_llm_calls_total{agent, model}
    helix_agent_tokens_used{agent, model, type="prompt|completion"}
    helix_agent_cost_total{agent, repo}
    helix_agent_sandbox_uptime_seconds{agent}
    helix_agent_worktree_count{agent}

The collector tracks metrics per agent, per repo, and per model. Prometheus text
exposition format with HELP/TYPE headers. Thread-safe via sync.RWMutex.

Package health — alert_rules.go

Platform alert rules engine implementing all 5 alert rules from
specs/SPECIFICATION.md §8.4 (Prometheus Metrics — Alert thresholds):

 1. HighCostAgent — rate(helix_agent_cost_total[1h]) > 5
 2. GateFailureSpike — rate(helix_gate_pass_rate{gate="tier1"}[15m]) < 0.7
 3. PRStuck — helix_pr_cycle_time_seconds > 7200
 4. AgentDown — helix_agent_sandbox_uptime_seconds == 0
 5. CostAnomaly — helix_cost_per_pr > (avg_over_time(helix_cost_per_pr[7d]) * 3)

The engine evaluates a MetricsSnapshot against configurable thresholds and
returns AlertResults with firing/resolved state.

Package health provides startup service-health validation for the Helix
platform. All Helix CLI tools call CheckServices at startup to fail-fast when
required services are unreachable.

Based on specs/cross-component-wiring.md §8 and specs/helix-config.md §7.

# Package health — notifier.go

Alert notifier with pluggable channels. Implements spec §8.4 alert routing:
firing alerts are fanned out to all configured notifiers.

Notifier implementations:
  - StdoutNotifier — JSON-line per alert to an io.Writer (default: stderr)
  - FileNotifier — append JSONL to ~/.helix/alerts.jsonl (mode 0o600)
  - MultiNotifier — fan-out, partial-success tolerant
  - TelegramNotifier — stub via Telegram Bot API (requires env vars)

Package health — platform_metrics.go

Platform-level Prometheus metrics recorder implementing all 7 platform metrics
from specs/SPECIFICATION.md §8.4 (Prometheus Metrics — Platform metrics):

    helix_pr_cycle_time_seconds{repo, quantile="0.5|0.95|0.99"}
    helix_gate_pass_rate{gate="tier1|tier2|chimera|conscientiousness|promptfoo"}
    helix_active_agents
    helix_queued_tasks
    helix_forgejo_api_latency_seconds{endpoint, quantile}
    helix_cost_per_pr{repo}
    helix_merge_rate{repo, period="hour|day|week"}

This recorder is the producer side: it records raw events (PR cycle times,
gate pass/fail, agent activity, task queue depth, API latencies, per-PR costs,
merge counts) and exposes them in Prometheus text exposition format. The
AlertEngine (alerts.go) consumes the same data via MetricsSnapshot.

Thread-safe via sync.RWMutex.

# Package health — prom.go

Prometheus exposition for Helix. Subcommand-invocation metrics, error counts,
and service-up gauges, all exposed via the `helix status --serve --addr :9095`
HTTP endpoint at /metrics.

Per docs/specs/SPECIFICATION.md §10.7 (Monitoring SLAs) and deployment.md §3
(Prometheus scraping). Renderer is hand-rolled — no Prometheus client_golang
dep, keeps the helix binary lean.

Thread-safety: all counters/gauges are guarded by a single mutex (PromStore.mu).
The HTTP handler acquires a read lock during scrape so concurrent scrapes don't
block writers.

Naming conventions follow Prometheus best practices:

    subcommand_duration_seconds_bucket{subcommand="..."}  histogram
    subcommand_invocations_total{subcommand="..."}        counter
    subcommand_rc_total{subcommand="...",rc="..."}        counter
    service_up{service="..."}                            gauge

`service` label value is one of: forgejo, chimera, langfuse, conscientiousness,
hivemind, prometheus, backup, disk, memory.

AutoApplicable safety: this file does NOT modify any other subsystem — it only
READS from the cache. Adding/updating metrics is safe to call concurrently from
anywhere; reads are O(1) per metric.

# Package health — remediation.go

Operator-facing remediation hints for Helix doctor failures. When `helix doctor`
flags a failing service without telling the operator how to fix it, triage time
balloons. RemediationRegistry maps each known failure type to a ranked set of
next-action steps plus a doc reference and severity.

Per docs/specs/SPECIFICATION.md §10.5 (Doctor) and the May/June 2026
field-feedback sessions (~30% of triage time lost to "no suggestion").

Design rules:

 1. Steps are DESCRIPTIVE — operators run them. helix doctor NEVER auto-fixes.
    The wrapper is purely advisory.
 2. Severity is derived from the check (not the operator). Critical → platform
    down, high → required service down, medium → degraded, low → informational.
 3. DocURLs reference local specs/ paths so ops work offline.
 4. The registry is process-global (defaultDrRegistry) but tests can construct
    isolated instances via NewRemediationRegistry.

Coupling: pkg/health/remediation.go has zero dependencies outside the standard
library and pkg/log. No HTTP, no db, no agent code. Pure data + formatting.

const DefaultTimeout = 5 * time.Second
var DefaultPromBuckets = []float64{ ... }
func CheckServices() error
func CheckServicesWithConfig(services []ServiceCheck) error
func FormatBreach(b *SLABreach) string
func FormatCostBreach(b *CostBreach) string
func FormatDashboard(report *DashboardReport) string
func FormatDegradationReport(report *DegradationReport) string
func FormatNotifyReport(report NotifyReport) string
func FormatRemediation(rem Remediation, w io.Writer)
func FormatRemediationJSON(rem Remediation) (string, error)
func NotifyReportToJSON(report NotifyReport) (string, error)
func ResetDefaultRegistry()
func StateEmoji(state HealthState) string
func TrimQuotes(s string) string
type APILatencyTarget struct{ ... }
type AgentMetricsCollector struct{ ... }
    func NewAgentMetricsCollector() *AgentMetricsCollector
type AgentMetricsSummary struct{ ... }
type AgentTaskStatus string
    const TaskCompleted AgentTaskStatus = "completed" ...
type AlertConfig struct{ ... }
    func DefaultAlertConfig() AlertConfig
type AlertEngine struct{ ... }
    func NewAlertEngine() *AlertEngine
    func NewAlertEngineWithConfig(config AlertConfig) *AlertEngine
type AlertResult struct{ ... }
type AlertRule struct{ ... }
type AlertSeverity string
    const AlertCritical AlertSeverity = "critical" ...
type AlertState string
    const AlertFiring AlertState = "firing" ...
type AlertSummary struct{ ... }
    func SummarizeResults(results []AlertResult) AlertSummary
type CapState string
    const CapAvailable CapState = "available" ...
type Capability string
    const CapWriteCode Capability = "write_code" ...
    func AllCapabilities() []Capability
type CapabilityStatus struct{ ... }
type CheckOutcome struct{ ... }
type Checker struct{ ... }
    func NewChecker(services []ServiceCheck) *Checker
type CostBreach struct{ ... }
type CostPerPRTarget struct{ ... }
type DashboardReport struct{ ... }
type DegradationReport struct{ ... }
    func EvaluateDegradation(downSubsystems, degradedSubsystems []string) *DegradationReport
    func EvaluateFromDashboard(dash *DashboardReport) *DegradationReport
type DegradationRule struct{ ... }
type FileNotifier struct{ ... }
    func NewFileNotifier(path string) (*FileNotifier, error)
type GateName string
    const GateTier1 GateName = "tier1" ...
    func AllGateNames() []GateName
type HealthReport struct{ ... }
type HealthState string
    const StateHealthy HealthState = "healthy" ...
type LatencyBudget struct{ ... }
type MergePeriod string
    const MergeHour MergePeriod = "hour" ...
type MergeThroughputTarget struct{ ... }
type MetricLine struct{ ... }
type MetricsSnapshot struct{ ... }
    func LoadMetricsSnapshotFromJSON(path string) (MetricsSnapshot, error)
    func NewMetricsSnapshot() MetricsSnapshot
type MetricsSource interface{ ... }
type MonitoringSLA struct{ ... }
type MultiNotifier struct{ ... }
    func NewMultiNotifier(notifiers ...Notifier) *MultiNotifier
type Notifier interface{ ... }
type NotifyEngine struct{ ... }
    func NewNotifyEngine(engine *AlertEngine, notifier Notifier) *NotifyEngine
type NotifyReport struct{ ... }
type PlatformHealthAggregator struct{ ... }
    func NewPlatformHealthAggregator(cacheTTL time.Duration) *PlatformHealthAggregator
type PlatformMetricsCollector struct{ ... }
    func NewPlatformMetricsCollector() *PlatformMetricsCollector
type PlatformMetricsRecorder struct{ ... }
    func NewPlatformMetricsRecorder() *PlatformMetricsRecorder
type PlatformMetricsSummary struct{ ... }
type PromStore struct{ ... }
    func NewPromStore() *PromStore
type Remediation struct{ ... }
type RemediationRegistry struct{ ... }
    func Default() *RemediationRegistry
    func NewRemediationRegistry() *RemediationRegistry
type RemediationReport struct{ ... }
    func BuildRemediationReport(reg *RemediationRegistry, checks []CheckOutcome) RemediationReport
type ReviewLatencyTarget struct{ ... }
type SLABreach struct{ ... }
    func CheckLatency(name, percentile string, budget LatencyBudget, actual time.Duration) *SLABreach
type SLARecorder struct{ ... }
    func NewSLARecorder(targets *SLATargets) *SLARecorder
type SLATargets struct{ ... }
    func DefaultSLATargets() *SLATargets
type SandboxStartupTarget struct{ ... }
type ServiceCheck struct{ ... }
    func DefaultServices() []ServiceCheck
type ServiceHealthAdapter struct{ ... }
type ServiceResult struct{ ... }
type Severity string
    const SeverityLow Severity = "low" ...
type Snapshot struct{ ... }
type StdoutNotifier struct{ ... }
    func NewStdoutNotifier(w io.Writer) *StdoutNotifier
type Step struct{ ... }
type StubMetricsSource struct{ ... }
    func NewStubMetricsSource(name string, lines []MetricLine) *StubMetricsSource
type SubsystemHealth interface{ ... }
type SubsystemStatus struct{ ... }
type SyncLatencyTarget struct{ ... }
type TelegramNotifier struct{ ... }
    func NewTelegramNotifier() *TelegramNotifier
type TokenType string
    const TokenPrompt TokenType = "prompt" ...
```

## Related

- [docs/api/README.md](README.md) — package index
