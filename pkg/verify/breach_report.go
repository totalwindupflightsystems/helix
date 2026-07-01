package verify

import (
	"fmt"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Breach Reporter (spec §Production Verification: Behavior Contracts + §Evidence Bundles)
// ---------------------------------------------------------------------------

// DeploymentPhase indicates which verification phase detected the breach.
type DeploymentPhase string

const (
	PhaseShadow      DeploymentPhase = "shadow"       // dark launch
	PhaseCanary      DeploymentPhase = "canary"       // partial traffic
	PhaseSteadyState DeploymentPhase = "steady_state" // full traffic 72h+
	PhaseUnknown     DeploymentPhase = "unknown"
)

// BreachAction is the suggested remediation for a breach.
type BreachAction string

const (
	BreachActionRollback    BreachAction = "rollback"
	BreachActionInvestigate BreachAction = "investigate"
	BreachActionWaive       BreachAction = "waive"
)

// BreachReportData holds all the structured data needed to render a Forgejo PR
// comment when a behavior contract is breached. It composes information from
// the Breach type, deployment state, drift report, and evidence bundle link.
type BreachReportData struct {
	ContractName    string          `json:"contract_name"`
	AgentID         string          `json:"agent_id"`
	DeploymentPhase DeploymentPhase `json:"deployment_phase"`
	Timestamp       time.Time       `json:"timestamp"`
	MergeCommit     string          `json:"merge_commit"`

	// Failed assertions with actual vs expected values.
	FailedAssertions []FailedAssertion `json:"failed_assertions"`

	// Metrics snapshot at breach time.
	Metrics MetricsSnapshot `json:"metrics_snapshot"`

	// Drift summary (optional — may be empty if no baseline comparison).
	DriftSummary []DriftReport `json:"drift_summary,omitempty"`

	// Recommended remediation action.
	RecommendedAction BreachAction `json:"recommended_action"`
	ActionReason      string       `json:"action_reason"`

	// Evidence bundle link (from pkg/review.EvidenceStore).
	EvidenceBundleLink string `json:"evidence_bundle_link,omitempty"`

	// Rollback information if rollback was triggered.
	RollbackTriggered bool   `json:"rollback_triggered"`
	RollbackReason    string `json:"rollback_reason,omitempty"`
}

// FailedAssertion pairs an assertion with its measured value for display.
type FailedAssertion struct {
	Metric        string  `json:"metric"`
	Operator      string  `json:"operator"`
	ExpectedValue float64 `json:"expected_value"`
	ActualValue   float64 `json:"actual_value"`
	Window        string  `json:"window,omitempty"`
	Reason        string  `json:"reason"`
}

// BreachReporter generates structured breach reports for Forgejo PR comments.
// It transforms raw Breach events and deployment context into human-readable
// markdown suitable for PR comment rendering.
type BreachReporter struct{}

// NewBreachReporter creates a BreachReporter.
func NewBreachReporter() *BreachReporter {
	return &BreachReporter{}
}

// ReportFromBreach builds a BreachReportData from a Monitor Breach event.
// deploymentPhase indicates which phase detected the breach. metrics is the
// snapshot at breach time. driftSummary and evidenceBundleLink are optional
// (may be nil/empty).
func (r *BreachReporter) ReportFromBreach(
	breach Breach,
	phase DeploymentPhase,
	metrics MetricsSnapshot,
	driftSummary []DriftReport,
	evidenceBundleLink string,
) BreachReportData {

	data := BreachReportData{
		ContractName:       breach.ContractName,
		AgentID:            breach.Agent,
		DeploymentPhase:    phase,
		Timestamp:          breach.Timestamp,
		MergeCommit:        breach.MergeCommit,
		Metrics:            metrics,
		DriftSummary:       driftSummary,
		EvidenceBundleLink: evidenceBundleLink,
		RollbackTriggered:  breach.ShouldRollback,
	}

	// Convert failed checks to display format.
	for _, fc := range breach.FailedChecks {
		data.FailedAssertions = append(data.FailedAssertions, FailedAssertion{
			Metric:        fc.Assertion.Metric,
			Operator:      fc.Assertion.Op,
			ExpectedValue: fc.Assertion.Value,
			ActualValue:   fc.MeasuredValue,
			Window:        fc.Assertion.Window,
			Reason:        fc.Reason,
		})
	}

	// Determine recommended action.
	data.RecommendedAction, data.ActionReason = r.recommendAction(breach, phase)
	if breach.ShouldRollback {
		data.RollbackReason = breach.Error()
	}

	return data
}

// recommendAction determines the appropriate remediation based on breach
// severity and deployment phase.
func (r *BreachReporter) recommendAction(breach Breach, phase DeploymentPhase) (BreachAction, string) {
	failedCount := len(breach.FailedChecks)

	// Shadow phase: rollback is safe, no user impact.
	if phase == PhaseShadow {
		if breach.ShouldRollback {
			return BreachActionRollback, fmt.Sprintf(
				"Shadow deployment breached %d assertion(s). Auto-rollback is safe — no production traffic affected.",
				failedCount)
		}
		return BreachActionInvestigate, fmt.Sprintf(
			"Shadow deployment detected %d assertion failure(s). Investigate before promoting to canary.",
			failedCount)
	}

	// Canary phase: rollback if configured, investigate otherwise.
	if phase == PhaseCanary {
		if breach.ShouldRollback {
			return BreachActionRollback, fmt.Sprintf(
				"Canary deployment breached %d assertion(s) with real traffic. Auto-rollback triggered to prevent further impact.",
				failedCount)
		}
		return BreachActionInvestigate, fmt.Sprintf(
			"Canary deployment showing %d assertion failure(s). Hold ramp and investigate.",
			failedCount)
	}

	// Steady-state: breach means production impact.
	if phase == PhaseSteadyState {
		if breach.ShouldRollback {
			return BreachActionRollback, fmt.Sprintf(
				"Steady-state surveillance detected %d assertion failure(s) in production. Immediate rollback required.",
				failedCount)
		}
		return BreachActionInvestigate, fmt.Sprintf(
			"Steady-state surveillance showing %d assertion failure(s). Production impact suspected — investigate immediately.",
			failedCount)
	}

	// Unknown phase: safe default.
	return BreachActionInvestigate, fmt.Sprintf(
		"Contract breach detected (%d assertion failures). Investigate deployment state.",
		failedCount)
}

// FormatBreachReport renders a BreachReportData as a structured markdown report
// suitable for Forgejo PR comments.
func FormatBreachReport(data BreachReportData) string {
	var sb strings.Builder

	// Header
	sb.WriteString("## 🚨 Behavior Contract Breach Detected\n\n")
	sb.WriteString(fmt.Sprintf("**Contract:** `%s`  \n", data.ContractName))
	sb.WriteString(fmt.Sprintf("**Agent:** `%s`  \n", data.AgentID))
	sb.WriteString(fmt.Sprintf("**Phase:** %s  \n", data.DeploymentPhase))
	sb.WriteString(fmt.Sprintf("**Time:** %s  \n", data.Timestamp.UTC().Format(time.RFC3339)))
	if data.MergeCommit != "" {
		sb.WriteString(fmt.Sprintf("**Merge Commit:** `%s`  \n", shortSHA(data.MergeCommit)))
	}
	sb.WriteString("\n")

	// Recommended action
	actionEmoji := map[BreachAction]string{
		BreachActionRollback:    "🔴",
		BreachActionInvestigate: "🟡",
		BreachActionWaive:       "⚪",
	}
	emoji := actionEmoji[data.RecommendedAction]
	if emoji == "" {
		emoji = "❓"
	}
	sb.WriteString(fmt.Sprintf("### %s Recommended Action: %s\n\n", emoji, string(data.RecommendedAction)))
	sb.WriteString(fmt.Sprintf("%s\n\n", data.ActionReason))

	if data.RollbackTriggered {
		sb.WriteString("> ⚠️ **Auto-rollback has been triggered.**\n")
		if data.RollbackReason != "" {
			sb.WriteString(fmt.Sprintf("> %s\n", data.RollbackReason))
		}
		sb.WriteString("\n")
	}

	// Failed assertions table
	if len(data.FailedAssertions) > 0 {
		sb.WriteString("### Failed Assertions\n\n")
		sb.WriteString("| Metric | Operator | Expected | Actual | Window | Status |\n")
		sb.WriteString("|--------|----------|----------|--------|--------|--------|\n")
		for _, fa := range data.FailedAssertions {
			sb.WriteString(fmt.Sprintf("| `%s` | %s | %.4f | %.4f | %s | ❌ FAIL |\n",
				fa.Metric, fa.Operator, fa.ExpectedValue, fa.ActualValue, fa.Window))
		}
		sb.WriteString("\n")
	}

	// Metrics snapshot
	sb.WriteString("### Metrics at Breach\n\n")
	m := data.Metrics
	sb.WriteString("| Metric | Value |\n")
	sb.WriteString("|--------|-------|\n")
	sb.WriteString(fmt.Sprintf("| Success Rate | %.4f%% |\n", m.SuccessRate*100))
	sb.WriteString(fmt.Sprintf("| P99 Latency | %.2fms |\n", m.P99LatencyMs))
	sb.WriteString(fmt.Sprintf("| P50 Latency | %.2fms |\n", m.P50LatencyMs))
	sb.WriteString(fmt.Sprintf("| Error Count | %d |\n", m.ErrorCount))
	sb.WriteString(fmt.Sprintf("| New Error Types | %d |\n", m.NewErrorTypes))
	sb.WriteString(fmt.Sprintf("| Memory Growth | %.2f%% |\n", m.MemoryGrowthPct))
	if m.RequestCount > 0 {
		sb.WriteString(fmt.Sprintf("| Request Count | %d |\n", m.RequestCount))
	}
	sb.WriteString("\n")

	// Drift summary
	if len(data.DriftSummary) > 0 {
		sb.WriteString("### Drift Summary\n\n")
		sb.WriteString("| Metric | Baseline | Current | Drift % | Threshold % | Exceeds |\n")
		sb.WriteString("|--------|----------|---------|---------|-------------|---------|\n")
		for _, d := range data.DriftSummary {
			exceedsStr := "no"
			if d.Exceeds {
				exceedsStr = "⚠️ yes"
			}
			sb.WriteString(fmt.Sprintf("| `%s` | %.4f | %.4f | %.2f%% | %.2f%% | %s |\n",
				d.Metric, d.Baseline, d.Current, d.DriftPct, d.ThresholdPct, exceedsStr))
		}
		sb.WriteString("\n")
	}

	// Evidence bundle link
	if data.EvidenceBundleLink != "" {
		sb.WriteString("### Evidence Bundle\n\n")
		sb.WriteString(fmt.Sprintf("📎 [%s](%s)\n\n", data.EvidenceBundleLink, data.EvidenceBundleLink))
	}

	// Footer
	sb.WriteString("---\n")
	sb.WriteString("*This breach report was auto-generated by Helix production verification.*\n")

	return sb.String()
}

// shortSHA returns a short git SHA for display. If the input is shorter than 7
// characters, it returns the input unchanged.
func shortSHA(sha string) string {
	if len(sha) <= 7 {
		return sha
	}
	return sha[:7]
}

// PhaseFromState converts a ShadowState to a DeploymentPhase for the reporter.
func PhaseFromState(state ShadowState) DeploymentPhase {
	switch state {
	case StateShadowing, StateShadowPassed, StateShadowFailed:
		return PhaseShadow
	case StateCanaried:
		return PhaseCanary
	case StatePromoted:
		return PhaseSteadyState
	case StateRolledBack:
		return PhaseUnknown // already rolled back, phase context lost
	default:
		return PhaseUnknown
	}
}

// BreachSummary provides a one-line summary of a breach for log output.
func BreachSummary(data BreachReportData) string {
	return fmt.Sprintf("[BREACH] contract=%s agent=%s phase=%s failed=%d action=%s",
		data.ContractName, data.AgentID, data.DeploymentPhase,
		len(data.FailedAssertions), data.RecommendedAction)
}
