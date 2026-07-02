package health

import (
	"testing"
	"time"
)

// ============================================================================
// AlertConfig
// ============================================================================

func TestDefaultAlertConfig(t *testing.T) {
	c := DefaultAlertConfig()
	if c.HighCostAgentThreshold != 5.0 {
		t.Errorf("HighCostAgentThreshold = %g, want 5.0", c.HighCostAgentThreshold)
	}
	if c.GateFailureSpikeThreshold != 0.7 {
		t.Errorf("GateFailureSpikeThreshold = %g, want 0.7", c.GateFailureSpikeThreshold)
	}
	if c.GateFailureSpikeGate != "tier1" {
		t.Errorf("GateFailureSpikeGate = %s, want tier1", c.GateFailureSpikeGate)
	}
	if c.PRStuckThreshold != 7200 {
		t.Errorf("PRStuckThreshold = %g, want 7200", c.PRStuckThreshold)
	}
	if c.CostAnomalyMultiplier != 3.0 {
		t.Errorf("CostAnomalyMultiplier = %g, want 3.0", c.CostAnomalyMultiplier)
	}
}

// ============================================================================
// AlertEngine — HighCostAgent
// ============================================================================

func TestEvaluateRules_HighCostAgent_Firing(t *testing.T) {
	engine := NewAlertEngine()
	snap := NewMetricsSnapshot()
	snap.AgentCosts["agent-7"] = 10.0 // > 5.0 threshold

	results := engine.EvaluateRules(snap)

	found := false
	for _, r := range results {
		if r.Rule.Name == "HighCostAgent" && r.Labels["agent"] == "agent-7" {
			if r.IsFiring() {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected HighCostAgent firing for agent-7")
	}
}

func TestEvaluateRules_HighCostAgent_Resolved(t *testing.T) {
	engine := NewAlertEngine()
	snap := NewMetricsSnapshot()
	snap.AgentCosts["agent-7"] = 3.0 // < 5.0 threshold

	results := engine.EvaluateRules(snap)

	found := false
	for _, r := range results {
		if r.Rule.Name == "HighCostAgent" && r.Labels["agent"] == "agent-7" {
			if !r.IsFiring() {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected HighCostAgent resolved for agent-7")
	}
}

func TestEvaluateRules_HighCostAgent_AtThreshold(t *testing.T) {
	engine := NewAlertEngine()
	snap := NewMetricsSnapshot()
	snap.AgentCosts["agent-7"] = 5.0 // exactly at threshold, should NOT fire (>)

	results := engine.EvaluateRules(snap)

	for _, r := range results {
		if r.Rule.Name == "HighCostAgent" && r.Labels["agent"] == "agent-7" {
			if r.IsFiring() {
				t.Error("expected NOT firing at threshold (uses >)")
			}
		}
	}
}

func TestEvaluateRules_HighCostAgent_MultipleAgents(t *testing.T) {
	engine := NewAlertEngine()
	snap := NewMetricsSnapshot()
	snap.AgentCosts["agent-1"] = 3.0
	snap.AgentCosts["agent-2"] = 8.0
	snap.AgentCosts["agent-3"] = 12.0

	results := engine.EvaluateRules(snap)

	firingCount := 0
	for _, r := range results {
		if r.Rule.Name == "HighCostAgent" && r.IsFiring() {
			firingCount++
		}
	}
	if firingCount != 2 {
		t.Errorf("expected 2 firing HighCostAgent alerts, got %d", firingCount)
	}
}

// ============================================================================
// AlertEngine — GateFailureSpike
// ============================================================================

func TestEvaluateRules_GateFailureSpike_Firing(t *testing.T) {
	engine := NewAlertEngine()
	snap := NewMetricsSnapshot()
	snap.GatePassRates["tier1"] = 0.5 // < 0.7

	results := engine.EvaluateRules(snap)

	found := false
	for _, r := range results {
		if r.Rule.Name == "GateFailureSpike" {
			if r.IsFiring() {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected GateFailureSpike firing")
	}
}

func TestEvaluateRules_GateFailureSpike_Resolved(t *testing.T) {
	engine := NewAlertEngine()
	snap := NewMetricsSnapshot()
	snap.GatePassRates["tier1"] = 0.9 // > 0.7

	results := engine.EvaluateRules(snap)

	for _, r := range results {
		if r.Rule.Name == "GateFailureSpike" {
			if r.IsFiring() {
				t.Error("expected GateFailureSpike resolved")
			}
		}
	}
}

func TestEvaluateRules_GateFailureSpike_MissingGate(t *testing.T) {
	engine := NewAlertEngine()
	snap := NewMetricsSnapshot()
	// Don't set tier1 rate — should not produce a GateFailureSpike result

	results := engine.EvaluateRules(snap)

	for _, r := range results {
		if r.Rule.Name == "GateFailureSpike" {
			t.Error("expected no GateFailureSpike when gate rate is missing")
		}
	}
}

func TestEvaluateRules_GateFailureSpike_CustomGate(t *testing.T) {
	engine := NewAlertEngineWithConfig(AlertConfig{
		GateFailureSpikeThreshold: 0.7,
		GateFailureSpikeGate:      "tier2",
		HighCostAgentThreshold:    5.0,
		PRStuckThreshold:          7200,
		CostAnomalyMultiplier:     3.0,
	})
	snap := NewMetricsSnapshot()
	snap.GatePassRates["tier1"] = 0.3
	snap.GatePassRates["tier2"] = 0.3 // should trigger, not tier1

	results := engine.EvaluateRules(snap)

	found := false
	for _, r := range results {
		if r.Rule.Name == "GateFailureSpike" {
			found = true
			if !r.IsFiring() {
				t.Error("expected GateFailureSpike firing for tier2")
			}
		}
	}
	if !found {
		t.Error("expected a GateFailureSpike result")
	}
}

// ============================================================================
// AlertEngine — PRStuck
// ============================================================================

func TestEvaluateRules_PRStuck_Firing(t *testing.T) {
	engine := NewAlertEngine()
	snap := NewMetricsSnapshot()
	snap.PRCycleTimes["helix"] = 8000 // > 7200

	results := engine.EvaluateRules(snap)

	found := false
	for _, r := range results {
		if r.Rule.Name == "PRStuck" && r.Labels["repo"] == "helix" {
			if r.IsFiring() {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected PRStuck firing for helix")
	}
}

func TestEvaluateRules_PRStuck_Resolved(t *testing.T) {
	engine := NewAlertEngine()
	snap := NewMetricsSnapshot()
	snap.PRCycleTimes["helix"] = 3600 // < 7200

	results := engine.EvaluateRules(snap)

	for _, r := range results {
		if r.Rule.Name == "PRStuck" && r.Labels["repo"] == "helix" {
			if r.IsFiring() {
				t.Error("expected PRStuck resolved")
			}
		}
	}
}

// ============================================================================
// AlertEngine — AgentDown
// ============================================================================

func TestEvaluateRules_AgentDown_Firing(t *testing.T) {
	engine := NewAlertEngine()
	snap := NewMetricsSnapshot()
	snap.AgentUptimes["agent-dead"] = 0 // down

	results := engine.EvaluateRules(snap)

	found := false
	for _, r := range results {
		if r.Rule.Name == "AgentDown" && r.Labels["agent"] == "agent-dead" {
			if r.IsFiring() {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected AgentDown firing")
	}
}

func TestEvaluateRules_AgentDown_Resolved(t *testing.T) {
	engine := NewAlertEngine()
	snap := NewMetricsSnapshot()
	snap.AgentUptimes["agent-alive"] = 3600 // alive

	results := engine.EvaluateRules(snap)

	for _, r := range results {
		if r.Rule.Name == "AgentDown" && r.Labels["agent"] == "agent-alive" {
			if r.IsFiring() {
				t.Error("expected AgentDown resolved")
			}
		}
	}
}

// ============================================================================
// AlertEngine — CostAnomaly
// ============================================================================

func TestEvaluateRules_CostAnomaly_Firing(t *testing.T) {
	engine := NewAlertEngine()
	snap := NewMetricsSnapshot()
	snap.CostPerPR["helix"] = 15.0
	snap.WeeklyAvgCostPerPR["helix"] = 3.0 // threshold = 3 * 3 = 9

	results := engine.EvaluateRules(snap)

	found := false
	for _, r := range results {
		if r.Rule.Name == "CostAnomaly" && r.Labels["repo"] == "helix" {
			if r.IsFiring() {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected CostAnomaly firing")
	}
}

func TestEvaluateRules_CostAnomaly_Resolved(t *testing.T) {
	engine := NewAlertEngine()
	snap := NewMetricsSnapshot()
	snap.CostPerPR["helix"] = 5.0
	snap.WeeklyAvgCostPerPR["helix"] = 3.0 // threshold = 9

	results := engine.EvaluateRules(snap)

	for _, r := range results {
		if r.Rule.Name == "CostAnomaly" && r.Labels["repo"] == "helix" {
			if r.IsFiring() {
				t.Error("expected CostAnomaly resolved")
			}
		}
	}
}

func TestEvaluateRules_CostAnomaly_NoWeeklyAvg(t *testing.T) {
	engine := NewAlertEngine()
	snap := NewMetricsSnapshot()
	snap.CostPerPR["helix"] = 100.0
	// No weekly avg — should skip

	results := engine.EvaluateRules(snap)

	for _, r := range results {
		if r.Rule.Name == "CostAnomaly" {
			t.Error("expected no CostAnomaly when weekly avg is missing")
		}
	}
}

func TestEvaluateRules_CostAnomaly_ZeroWeeklyAvg(t *testing.T) {
	engine := NewAlertEngine()
	snap := NewMetricsSnapshot()
	snap.CostPerPR["helix"] = 5.0
	snap.WeeklyAvgCostPerPR["helix"] = 0 // division by zero guard

	results := engine.EvaluateRules(snap)

	for _, r := range results {
		if r.Rule.Name == "CostAnomaly" {
			t.Error("expected no CostAnomaly when weekly avg is zero")
		}
	}
}

// ============================================================================
// AlertEngine — All Rules Together
// ============================================================================

func TestEvaluateRules_AllFiring(t *testing.T) {
	engine := NewAlertEngine()
	snap := NewMetricsSnapshot()
	snap.AgentCosts["agent-1"] = 10.0
	snap.GatePassRates["tier1"] = 0.5
	snap.PRCycleTimes["helix"] = 8000
	snap.AgentUptimes["agent-2"] = 0
	snap.CostPerPR["chimera"] = 30.0
	snap.WeeklyAvgCostPerPR["chimera"] = 5.0

	results := engine.EvaluateRules(snap)

	summary := SummarizeResults(results)
	if summary.Firing < 5 {
		t.Errorf("expected at least 5 firing alerts, got %d", summary.Firing)
	}
}

func TestEvaluateRules_EmptySnapshot(t *testing.T) {
	engine := NewAlertEngine()
	snap := NewMetricsSnapshot()

	results := engine.EvaluateRules(snap)

	if len(results) != 0 {
		t.Errorf("expected 0 results for empty snapshot, got %d", len(results))
	}
}

func TestEvaluateRules_CustomConfig(t *testing.T) {
	engine := NewAlertEngineWithConfig(AlertConfig{
		HighCostAgentThreshold:    1.0, // very low
		GateFailureSpikeThreshold: 0.9,
		GateFailureSpikeGate:      "tier1",
		PRStuckThreshold:          100, // very low
		CostAnomalyMultiplier:     1.5,
	})
	snap := NewMetricsSnapshot()
	snap.AgentCosts["agent-1"] = 2.0  // > 1.0
	snap.GatePassRates["tier1"] = 0.8 // < 0.9
	snap.PRCycleTimes["helix"] = 200  // > 100

	results := engine.EvaluateRules(snap)

	firingCount := 0
	for _, r := range results {
		if r.IsFiring() {
			firingCount++
		}
	}
	if firingCount < 3 {
		t.Errorf("expected at least 3 firing with low thresholds, got %d", firingCount)
	}
}

// ============================================================================
// AlertResult Methods
// ============================================================================

func TestAlertResult_IsFiring(t *testing.T) {
	r := AlertResult{State: AlertFiring}
	if !r.IsFiring() {
		t.Error("expected IsFiring=true for firing state")
	}

	r2 := AlertResult{State: AlertResolved}
	if r2.IsFiring() {
		t.Error("expected IsFiring=false for resolved state")
	}
}

// ============================================================================
// AlertSummary
// ============================================================================

func TestSummarizeResults(t *testing.T) {
	results := []AlertResult{
		{Rule: AlertRule{Name: "A", Severity: AlertCritical}, State: AlertFiring},
		{Rule: AlertRule{Name: "B", Severity: AlertWarning}, State: AlertFiring},
		{Rule: AlertRule{Name: "C", Severity: AlertWarning}, State: AlertResolved},
	}

	s := SummarizeResults(results)
	if s.Total != 3 {
		t.Errorf("Total = %d, want 3", s.Total)
	}
	if s.Firing != 2 {
		t.Errorf("Firing = %d, want 2", s.Firing)
	}
	if s.Resolved != 1 {
		t.Errorf("Resolved = %d, want 1", s.Resolved)
	}
	if s.Critical != 1 {
		t.Errorf("Critical = %d, want 1", s.Critical)
	}
	if s.Warning != 1 {
		t.Errorf("Warning = %d, want 1", s.Warning)
	}
}

func TestAlertSummary_HasFiring(t *testing.T) {
	s := AlertSummary{Firing: 1}
	if !s.HasFiring() {
		t.Error("expected HasFiring=true")
	}

	s2 := AlertSummary{Firing: 0}
	if s2.HasFiring() {
		t.Error("expected HasFiring=false")
	}
}

func TestAlertSummary_HasCritical(t *testing.T) {
	s := AlertSummary{Critical: 1}
	if !s.HasCritical() {
		t.Error("expected HasCritical=true")
	}

	s2 := AlertSummary{Critical: 0, Warning: 2}
	if s2.HasCritical() {
		t.Error("expected HasCritical=false")
	}
}

func TestAlertSummary_FormatSummary_AllFiring(t *testing.T) {
	s := AlertSummary{Total: 5, Firing: 3, Critical: 1, Warning: 2}
	msg := s.FormatSummary()
	if msg == "" {
		t.Error("expected non-empty summary")
	}
}

func TestAlertSummary_FormatSummary_AllResolved(t *testing.T) {
	s := AlertSummary{Total: 5, Firing: 0}
	msg := s.FormatSummary()
	if msg == "" {
		t.Error("expected non-empty summary")
	}
}

// ============================================================================
// AlertEngine — Config
// ============================================================================

func TestAlertEngine_Config(t *testing.T) {
	customConfig := AlertConfig{
		HighCostAgentThreshold:    10.0,
		GateFailureSpikeThreshold: 0.5,
		GateFailureSpikeGate:      "tier2",
		PRStuckThreshold:          3600,
		CostAnomalyMultiplier:     2.0,
	}
	engine := NewAlertEngineWithConfig(customConfig)
	c := engine.Config()
	if c.HighCostAgentThreshold != 10.0 {
		t.Errorf("HighCostAgentThreshold = %g", c.HighCostAgentThreshold)
	}
	if c.GateFailureSpikeGate != "tier2" {
		t.Errorf("GateFailureSpikeGate = %s", c.GateFailureSpikeGate)
	}
}

func TestAlertEngine_SetConfig(t *testing.T) {
	engine := NewAlertEngine()
	newConfig := DefaultAlertConfig()
	newConfig.HighCostAgentThreshold = 20.0
	engine.SetConfig(newConfig)

	c := engine.Config()
	if c.HighCostAgentThreshold != 20.0 {
		t.Errorf("HighCostAgentThreshold = %g, want 20.0", c.HighCostAgentThreshold)
	}
}

// ============================================================================
// NewMetricsSnapshot
// ============================================================================

func TestNewMetricsSnapshot(t *testing.T) {
	snap := NewMetricsSnapshot()
	if snap.AgentCosts == nil {
		t.Error("expected non-nil AgentCosts")
	}
	if snap.GatePassRates == nil {
		t.Error("expected non-nil GatePassRates")
	}
	if snap.PRCycleTimes == nil {
		t.Error("expected non-nil PRCycleTimes")
	}
	if snap.AgentUptimes == nil {
		t.Error("expected non-nil AgentUptimes")
	}
	if snap.CostPerPR == nil {
		t.Error("expected non-nil CostPerPR")
	}
	if snap.WeeklyAvgCostPerPR == nil {
		t.Error("expected non-nil WeeklyAvgCostPerPR")
	}
	if snap.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

// ============================================================================
// Sorted Results
// ============================================================================

func TestEvaluateResults_Sorted(t *testing.T) {
	engine := NewAlertEngine()
	snap := NewMetricsSnapshot()
	snap.AgentCosts["agent-z"] = 10.0
	snap.AgentCosts["agent-a"] = 10.0
	snap.AgentUptimes["agent-m"] = 0

	results := engine.EvaluateRules(snap)

	// Results should be sorted by rule name
	for i := 1; i < len(results); i++ {
		if results[i].Rule.Name < results[i-1].Rule.Name {
			t.Errorf("results not sorted: %s before %s", results[i-1].Rule.Name, results[i].Rule.Name)
		}
	}
}

// ============================================================================
// Timestamps
// ============================================================================

func TestEvaluateRules_TimestampSet(t *testing.T) {
	engine := NewAlertEngine()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	snap := NewMetricsSnapshot()
	snap.Timestamp = now
	snap.AgentCosts["agent-1"] = 10.0

	results := engine.EvaluateRules(snap)

	for _, r := range results {
		if r.IsFiring() && !r.FiredAt.Equal(now) {
			t.Errorf("expected FiredAt = %v, got %v", now, r.FiredAt)
		}
	}
}

// ============================================================================
// Concurrent Access
// ============================================================================

func TestAlertEngine_ConcurrentConfig(t *testing.T) {
	engine := NewAlertEngine()

	done := make(chan bool, 2)

	go func() {
		for i := 0; i < 100; i++ {
			engine.SetConfig(AlertConfig{
				HighCostAgentThreshold: float64(i),
			})
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_ = engine.Config()
		}
		done <- true
	}()

	<-done
	<-done
}
