package verify

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// BreachReporter — spec §Behavior Contracts + §Evidence Bundles
// ---------------------------------------------------------------------------

func makeBreach(contractName, agent string, rollback bool) Breach {
	return Breach{
		ContractName: contractName,
		Agent:        agent,
		MergeCommit:  "abcdef1234567890abcdef1234567890abcdef12",
		Timestamp:    time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
		FailedChecks: []CheckResult{
			{
				Assertion: Assertion{
					Metric: "success_rate",
					Op:     "gte",
					Value:  0.99,
					Window: "1h",
				},
				MeasuredValue: 0.85,
				Passed:        false,
				Reason:        "measured 0.8500 < threshold 0.9900 (expected >=)",
			},
			{
				Assertion: Assertion{
					Metric: "p99_latency_ms",
					Op:     "lte",
					Value:  500,
					Window: "5m",
				},
				MeasuredValue: 850,
				Passed:        false,
				Reason:        "measured 850.0000 > threshold 500.0000 (expected <=)",
			},
		},
		ShouldRollback: rollback,
		ShouldNotify:   true,
	}
}

func makeMetrics() MetricsSnapshot {
	return MetricsSnapshot{
		SuccessRate:     0.85,
		P99LatencyMs:    850,
		P50LatencyMs:    200,
		ErrorCount:      15,
		NewErrorTypes:   2,
		MemoryGrowthPct: 5.2,
		RequestCount:    10000,
		Timestamp:       time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
	}
}

func makeDrift() []DriftReport {
	return []DriftReport{
		{Metric: "success_rate", Baseline: 0.99, Current: 0.85, DriftPct: -14.14, ThresholdPct: 2.0, Exceeds: true},
		{Metric: "p99_latency_ms", Baseline: 300, Current: 850, DriftPct: 183.33, ThresholdPct: 10.0, Exceeds: true},
	}
}

// --- ReportFromBreach tests ---

func TestReportFromBreach_Shadow(t *testing.T) {
	reporter := NewBreachReporter()
	breach := makeBreach("auth-contract", "agent-1", true)

	data := reporter.ReportFromBreach(breach, PhaseShadow, makeMetrics(), makeDrift(), "")

	if data.ContractName != "auth-contract" {
		t.Errorf("expected contract name 'auth-contract', got %q", data.ContractName)
	}
	if data.AgentID != "agent-1" {
		t.Errorf("expected agent 'agent-1', got %q", data.AgentID)
	}
	if data.DeploymentPhase != PhaseShadow {
		t.Errorf("expected phase shadow, got %s", data.DeploymentPhase)
	}
	if len(data.FailedAssertions) != 2 {
		t.Errorf("expected 2 failed assertions, got %d", len(data.FailedAssertions))
	}
	if data.RecommendedAction != BreachActionRollback {
		t.Errorf("expected rollback action in shadow, got %s", data.RecommendedAction)
	}
	if data.RollbackTriggered != true {
		t.Error("expected rollback triggered")
	}
}

func TestReportFromBreach_Canary(t *testing.T) {
	reporter := NewBreachReporter()
	breach := makeBreach("api-contract", "agent-2", false)

	data := reporter.ReportFromBreach(breach, PhaseCanary, makeMetrics(), nil, "")

	if data.RecommendedAction != BreachActionInvestigate {
		t.Errorf("expected investigate in canary without rollback, got %s", data.RecommendedAction)
	}
}

func TestReportFromBreach_CanaryWithRollback(t *testing.T) {
	reporter := NewBreachReporter()
	breach := makeBreach("api-contract", "agent-2", true)

	data := reporter.ReportFromBreach(breach, PhaseCanary, makeMetrics(), nil, "")

	if data.RecommendedAction != BreachActionRollback {
		t.Errorf("expected rollback in canary with rollback, got %s", data.RecommendedAction)
	}
}

func TestReportFromBreach_SteadyState(t *testing.T) {
	reporter := NewBreachReporter()
	breach := makeBreach("prod-contract", "agent-3", true)

	data := reporter.ReportFromBreach(breach, PhaseSteadyState, makeMetrics(), nil, "")

	if data.RecommendedAction != BreachActionRollback {
		t.Errorf("expected rollback in steady-state with rollback, got %s", data.RecommendedAction)
	}
}

func TestReportFromBreach_SteadyStateNoRollback(t *testing.T) {
	reporter := NewBreachReporter()
	breach := makeBreach("prod-contract", "agent-3", false)

	data := reporter.ReportFromBreach(breach, PhaseSteadyState, makeMetrics(), nil, "")

	if data.RecommendedAction != BreachActionInvestigate {
		t.Errorf("expected investigate in steady-state without rollback, got %s", data.RecommendedAction)
	}
}

func TestReportFromBreach_Unknown(t *testing.T) {
	reporter := NewBreachReporter()
	breach := makeBreach("contract", "agent", false)

	data := reporter.ReportFromBreach(breach, PhaseUnknown, makeMetrics(), nil, "")

	if data.RecommendedAction != BreachActionInvestigate {
		t.Errorf("expected investigate for unknown phase, got %s", data.RecommendedAction)
	}
}

func TestReportFromBreach_FailedAssertions(t *testing.T) {
	reporter := NewBreachReporter()
	breach := makeBreach("c", "a", true)

	data := reporter.ReportFromBreach(breach, PhaseShadow, makeMetrics(), nil, "")

	if len(data.FailedAssertions) != 2 {
		t.Fatalf("expected 2 failed assertions, got %d", len(data.FailedAssertions))
	}

	fa := data.FailedAssertions[0]
	if fa.Metric != "success_rate" {
		t.Errorf("expected metric 'success_rate', got %q", fa.Metric)
	}
	if fa.Operator != "gte" {
		t.Errorf("expected operator 'gte', got %q", fa.Operator)
	}
	if fa.ExpectedValue != 0.99 {
		t.Errorf("expected expected 0.99, got %.4f", fa.ExpectedValue)
	}
	if fa.ActualValue != 0.85 {
		t.Errorf("expected actual 0.85, got %.4f", fa.ActualValue)
	}
	if fa.Window != "1h" {
		t.Errorf("expected window '1h', got %q", fa.Window)
	}
}

func TestReportFromBreach_EvidenceLink(t *testing.T) {
	reporter := NewBreachReporter()
	breach := makeBreach("c", "a", true)

	data := reporter.ReportFromBreach(breach, PhaseShadow, makeMetrics(), nil,
		"~/.helix/evidence/review-123.json")

	if data.EvidenceBundleLink != "~/.helix/evidence/review-123.json" {
		t.Errorf("expected evidence link, got %q", data.EvidenceBundleLink)
	}
}

func TestReportFromBreach_DriftSummary(t *testing.T) {
	reporter := NewBreachReporter()
	breach := makeBreach("c", "a", true)
	drift := makeDrift()

	data := reporter.ReportFromBreach(breach, PhaseShadow, makeMetrics(), drift, "")

	if len(data.DriftSummary) != 2 {
		t.Errorf("expected 2 drift entries, got %d", len(data.DriftSummary))
	}
}

func TestReportFromBreach_RollbackReason(t *testing.T) {
	reporter := NewBreachReporter()
	breach := makeBreach("c", "a", true)

	data := reporter.ReportFromBreach(breach, PhaseShadow, makeMetrics(), nil, "")

	if data.RollbackReason == "" {
		t.Error("expected non-empty rollback reason when rollback is triggered")
	}
}

func TestReportFromBreach_NoRollbackReason(t *testing.T) {
	reporter := NewBreachReporter()
	breach := makeBreach("c", "a", false)

	data := reporter.ReportFromBreach(breach, PhaseShadow, makeMetrics(), nil, "")

	if data.RollbackReason != "" {
		t.Error("expected empty rollback reason when rollback is not triggered")
	}
}

// --- recommendAction tests ---

func TestRecommendAction_AllPhases(t *testing.T) {
	reporter := NewBreachReporter()
	breach := makeBreach("c", "a", false)
	breachRollback := makeBreach("c", "a", true)

	tests := []struct {
		phase          DeploymentPhase
		rollback       bool
		expectedAction BreachAction
	}{
		{PhaseShadow, true, BreachActionRollback},
		{PhaseShadow, false, BreachActionInvestigate},
		{PhaseCanary, true, BreachActionRollback},
		{PhaseCanary, false, BreachActionInvestigate},
		{PhaseSteadyState, true, BreachActionRollback},
		{PhaseSteadyState, false, BreachActionInvestigate},
		{PhaseUnknown, false, BreachActionInvestigate},
	}

	for _, tt := range tests {
		b := breach
		if tt.rollback {
			b = breachRollback
		}
		action, reason := reporter.recommendAction(b, tt.phase)
		if action != tt.expectedAction {
			t.Errorf("phase=%s rollback=%v: expected %s, got %s", tt.phase, tt.rollback, tt.expectedAction, action)
		}
		if reason == "" {
			t.Errorf("phase=%s rollback=%v: expected non-empty reason", tt.phase, tt.rollback)
		}
	}
}

// --- FormatBreachReport tests ---

func TestFormatBreachReport_BasicStructure(t *testing.T) {
	reporter := NewBreachReporter()
	breach := makeBreach("auth", "agent-1", true)
	data := reporter.ReportFromBreach(breach, PhaseShadow, makeMetrics(), makeDrift(),
		"evidence-link")

	report := FormatBreachReport(data)

	requiredSubstrings := []string{
		"🚨 Behavior Contract Breach Detected",
		"**Contract:**",
		"**Agent:**",
		"**Phase:**",
		"Recommended Action:",
		"Failed Assertions",
		"Metrics at Breach",
		"Drift Summary",
		"Evidence Bundle",
		"auto-generated",
	}

	for _, s := range requiredSubstrings {
		if !strings.Contains(report, s) {
			t.Errorf("report missing required section: %q", s)
		}
	}
}

func TestFormatBreachReport_NoDrift(t *testing.T) {
	reporter := NewBreachReporter()
	breach := makeBreach("c", "a", false)
	data := reporter.ReportFromBreach(breach, PhaseShadow, makeMetrics(), nil, "")

	report := FormatBreachReport(data)

	if strings.Contains(report, "### Drift Summary") {
		t.Error("should not contain drift summary section when drift is nil")
	}
}

func TestFormatBreachReport_NoEvidenceLink(t *testing.T) {
	reporter := NewBreachReporter()
	breach := makeBreach("c", "a", false)
	data := reporter.ReportFromBreach(breach, PhaseShadow, makeMetrics(), nil, "")

	report := FormatBreachReport(data)

	if strings.Contains(report, "### Evidence Bundle") {
		t.Error("should not contain evidence section when link is empty")
	}
}

func TestFormatBreachReport_FailedAssertionsTable(t *testing.T) {
	reporter := NewBreachReporter()
	breach := makeBreach("c", "a", true)
	data := reporter.ReportFromBreach(breach, PhaseShadow, makeMetrics(), nil, "")

	report := FormatBreachReport(data)

	if !strings.Contains(report, "| Metric | Operator |") {
		t.Error("should contain assertions table header")
	}
	if !strings.Contains(report, "success_rate") {
		t.Error("should contain the metric name in table")
	}
	if !strings.Contains(report, "FAIL") {
		t.Error("should contain FAIL status")
	}
}

func TestFormatBreachReport_MetricsTable(t *testing.T) {
	reporter := NewBreachReporter()
	breach := makeBreach("c", "a", false)
	data := reporter.ReportFromBreach(breach, PhaseShadow, makeMetrics(), nil, "")

	report := FormatBreachReport(data)

	if !strings.Contains(report, "Success Rate") {
		t.Error("should contain success rate")
	}
	if !strings.Contains(report, "P99 Latency") {
		t.Error("should contain P99 latency")
	}
	if !strings.Contains(report, "85.0000%") { // 0.85 * 100
		t.Error("should contain formatted success rate percentage")
	}
}

func TestFormatBreachReport_RollbackWarning(t *testing.T) {
	reporter := NewBreachReporter()
	breach := makeBreach("c", "a", true)
	data := reporter.ReportFromBreach(breach, PhaseShadow, makeMetrics(), nil, "")

	report := FormatBreachReport(data)

	if !strings.Contains(report, "Auto-rollback has been triggered") {
		t.Error("should contain rollback warning")
	}
}

func TestFormatBreachReport_NoRollbackWarning(t *testing.T) {
	reporter := NewBreachReporter()
	breach := makeBreach("c", "a", false)
	data := reporter.ReportFromBreach(breach, PhaseShadow, makeMetrics(), nil, "")

	report := FormatBreachReport(data)

	if strings.Contains(report, "Auto-rollback has been triggered") {
		t.Error("should NOT contain rollback warning when rollback=false")
	}
}

func TestFormatBreachReport_MergeCommitShortSHA(t *testing.T) {
	reporter := NewBreachReporter()
	breach := makeBreach("c", "a", false)
	breach.MergeCommit = "abcdef1234567890abcdef1234567890abcdef12"
	data := reporter.ReportFromBreach(breach, PhaseShadow, makeMetrics(), nil, "")

	report := FormatBreachReport(data)

	if !strings.Contains(report, "abcdef1") {
		t.Error("should contain short SHA in merge commit")
	}
}

// --- PhaseFromState tests ---

func TestPhaseFromState(t *testing.T) {
	tests := []struct {
		state    ShadowState
		expected DeploymentPhase
	}{
		{StateShadowing, PhaseShadow},
		{StateShadowPassed, PhaseShadow},
		{StateShadowFailed, PhaseShadow},
		{StateCanaried, PhaseCanary},
		{StatePromoted, PhaseSteadyState},
		{StateRolledBack, PhaseUnknown},
		{StateIdle, PhaseUnknown},
	}

	for _, tt := range tests {
		got := PhaseFromState(tt.state)
		if got != tt.expected {
			t.Errorf("PhaseFromState(%s) = %s, expected %s", tt.state, got, tt.expected)
		}
	}
}

// --- shortSHA tests ---

func TestShortSHA(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"abcdef1234567890", "abcdef1"},
		{"abc", "abc"},          // shorter than 7
		{"abcdefg", "abcdefg"},  // exactly 7
		{"abcdefgh", "abcdefg"}, // 8 chars
		{"", ""},
	}

	for _, tt := range tests {
		got := shortSHA(tt.input)
		if got != tt.expected {
			t.Errorf("shortSHA(%q) = %q, expected %q", tt.input, got, tt.expected)
		}
	}
}

// --- BreachSummary tests ---

func TestBreachSummary(t *testing.T) {
	reporter := NewBreachReporter()
	breach := makeBreach("auth-contract", "agent-1", true)
	data := reporter.ReportFromBreach(breach, PhaseShadow, makeMetrics(), nil, "")

	summary := BreachSummary(data)

	if !strings.Contains(summary, "[BREACH]") {
		t.Error("summary should start with [BREACH]")
	}
	if !strings.Contains(summary, "auth-contract") {
		t.Error("summary should contain contract name")
	}
	if !strings.Contains(summary, "agent-1") {
		t.Error("summary should contain agent ID")
	}
	if !strings.Contains(summary, "shadow") {
		t.Error("summary should contain phase")
	}
	if !strings.Contains(summary, "rollback") {
		t.Error("summary should contain action")
	}
}

// --- Integration: full pipeline test ---

func TestBreachReport_FullPipeline(t *testing.T) {
	// This simulates the full breach detection → reporting pipeline.
	monitor := NewMonitor()
	contract := &BehaviorContract{
		Contract: ContractBody{
			Name:        "critical-api",
			Agent:       "trusted-agent",
			MergeCommit: "deadbeef1234567890",
			Assertions: []Assertion{
				{Metric: "success_rate", Op: "gte", Value: 0.99, Window: "5m"},
				{Metric: "p99_latency_ms", Op: "lte", Value: 200, Window: "5m"},
				{Metric: "error_count", Op: "eq", Value: 0, Window: "1m"},
			},
			BreachAction: "rollback",
		},
	}
	monitor.RegisterContract(contract)

	// Evaluate against bad metrics — all 3 should fail.
	badMetrics := map[string]float64{
		"success_rate":   0.80,
		"p99_latency_ms": 500,
		"error_count":    5,
	}

	breaches := monitor.Evaluate(badMetrics)
	if len(breaches) != 1 {
		t.Fatalf("expected 1 breach, got %d", len(breaches))
	}

	// Generate report from the breach.
	reporter := NewBreachReporter()
	data := reporter.ReportFromBreach(
		breaches[0],
		PhaseCanary,
		makeMetrics(),
		makeDrift(),
		"~/.helix/evidence/review-456.json",
	)

	// Verify report content.
	if data.ContractName != "critical-api" {
		t.Errorf("expected contract 'critical-api', got %q", data.ContractName)
	}
	if data.AgentID != "trusted-agent" {
		t.Errorf("expected agent 'trusted-agent', got %q", data.AgentID)
	}
	if len(data.FailedAssertions) != 3 {
		t.Errorf("expected 3 failed assertions, got %d", len(data.FailedAssertions))
	}

	// Format and verify the markdown report.
	report := FormatBreachReport(data)
	if !strings.Contains(report, "critical-api") {
		t.Error("report should contain contract name")
	}
	if !strings.Contains(report, "trusted-agent") {
		t.Error("report should contain agent ID")
	}
	if !strings.Contains(report, "review-456") {
		t.Error("report should contain evidence link")
	}
}

func TestBreachReport_EmptyFailedChecks(t *testing.T) {
	reporter := NewBreachReporter()
	breach := Breach{
		ContractName: "empty-contract",
		Agent:        "agent",
		Timestamp:    time.Now().UTC(),
		FailedChecks: []CheckResult{}, // no failed checks
	}

	data := reporter.ReportFromBreach(breach, PhaseShadow, makeMetrics(), nil, "")

	if len(data.FailedAssertions) != 0 {
		t.Errorf("expected 0 failed assertions, got %d", len(data.FailedAssertions))
	}

	// Should still produce a valid report
	report := FormatBreachReport(data)
	if !strings.Contains(report, "empty-contract") {
		t.Error("report should contain contract name even with no failures")
	}
}
