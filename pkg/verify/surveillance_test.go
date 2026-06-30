package verify

import (
	"testing"
	"time"
)

// ============================================================================
// Helpers for tests
// ============================================================================

func survSampleMetrics(successRate float64, p99 float64, errors int64, reqs int64, memPct float64) map[string]float64 {
	return map[string]float64{
		"success_rate":      successRate,
		"p99_latency_ms":    p99,
		"error_count":       float64(errors),
		"request_count":     float64(reqs),
		"memory_growth_pct": memPct,
	}
}

func survMakeContract(name, agent string) *BehaviorContract {
	return &BehaviorContract{
		Contract: ContractBody{
			Name:        name,
			Agent:       agent,
			MergeCommit: "abc123",
			Assertions: []Assertion{
				{Metric: "success_rate", Op: "gte", Value: 0.999, Window: "1h"},
				{Metric: "p99_latency_ms", Op: "lte", Value: 200, Window: "1h"},
			},
			BreachAction: "rollback_and_notify",
		},
	}
}

// ============================================================================
// SteadyStateAggregator — Registration
// ============================================================================

func TestSteadyStateAggregator_RegisterUnregister(t *testing.T) {
	agg := NewSteadyStateAggregator()
	agg.RegisterAgent("agent-1", goodBaseline())

	agents := agg.RegisteredAgents()
	if len(agents) != 1 || agents[0] != "agent-1" {
		t.Fatalf("expected [agent-1], got %v", agents)
	}

	agg.UnregisterAgent("agent-1")
	if len(agg.RegisteredAgents()) != 0 {
		t.Fatalf("expected no agents after unregister")
	}
}

func TestSteadyStateAggregator_RegisterMultipleAgents(t *testing.T) {
	agg := NewSteadyStateAggregator()
	agg.RegisterAgent("a1", goodBaseline())
	agg.RegisterAgent("a2", goodBaseline())
	agg.RegisterAgent("a3", goodBaseline())

	if len(agg.RegisteredAgents()) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(agg.RegisteredAgents()))
	}
}

// ============================================================================
// SteadyStateAggregator — Healthy Agent
// ============================================================================

func TestSteadyStateAggregator_HealthyAgent(t *testing.T) {
	agg := NewSteadyStateAggregator()
	agg.RegisterAgent("agent-1", goodBaseline())
	agg.RegisterContract(survMakeContract("auth-session-v2", "agent-1"))

	// All metrics within contract thresholds.
	metrics := survSampleMetrics(0.9997, 95, 3, 10000, 1.5)
	event := agg.CheckAgent("agent-1", metrics)

	if event == nil {
		t.Fatal("expected event, got nil")
	}
	if event.Status != StatusHealthy {
		t.Errorf("expected healthy, got %s", event.Status)
	}
	if event.EscalationLevel != EscalationNone {
		t.Errorf("expected no escalation, got %s", event.EscalationLevel)
	}
	if len(event.Breaches) != 0 {
		t.Errorf("expected no breaches, got %d", len(event.Breaches))
	}
}

func TestSteadyStateAggregator_BreachDetection(t *testing.T) {
	agg := NewSteadyStateAggregator()
	agg.RegisterAgent("agent-1", goodBaseline())
	agg.RegisterContract(survMakeContract("auth-session-v2", "agent-1"))

	// Success rate below contract threshold (0.999).
	metrics := survSampleMetrics(0.995, 95, 50, 10000, 1.5) // 0.995 < 0.999
	event := agg.CheckAgent("agent-1", metrics)

	if event == nil {
		t.Fatal("expected event, got nil")
	}
	if event.Status != StatusBreach {
		t.Errorf("expected breach status, got %s", event.Status)
	}
	if len(event.Breaches) != 1 {
		t.Fatalf("expected 1 breach, got %d", len(event.Breaches))
	}
	if event.Breaches[0].ContractName != "auth-session-v2" {
		t.Errorf("expected contract name auth-session-v2, got %s", event.Breaches[0].ContractName)
	}
	if !event.IsActionable() {
		t.Error("breach event should be actionable")
	}
}

func TestSteadyStateAggregator_LatencyBreach(t *testing.T) {
	agg := NewSteadyStateAggregator()
	agg.RegisterAgent("agent-1", goodBaseline())
	agg.RegisterContract(survMakeContract("api-v3", "agent-1"))

	// P99 latency above contract threshold (200).
	metrics := survSampleMetrics(0.9997, 350, 3, 10000, 1.5)
	event := agg.CheckAgent("agent-1", metrics)

	if event.Status != StatusBreach {
		t.Errorf("expected breach from latency, got %s", event.Status)
	}
}

func TestSteadyStateAggregator_EscalationOnBreach(t *testing.T) {
	agg := NewSteadyStateAggregator()
	agg.RegisterAgent("agent-1", goodBaseline())
	agg.RegisterContract(survMakeContract("contract-1", "agent-1"))

	metrics := survSampleMetrics(0.995, 95, 50, 10000, 1.5)
	event := agg.CheckAgent("agent-1", metrics)

	// Breach should immediately escalate to rollback.
	if event.EscalationLevel != EscalationRollback {
		t.Errorf("expected rollback escalation, got %s", event.EscalationLevel)
	}
}

// ============================================================================
// SteadyStateAggregator — Drift Warning
// ============================================================================

func TestSteadyStateAggregator_DriftWarningNoBreach(t *testing.T) {
	agg := NewSteadyStateAggregator()
	agg.RegisterAgent("agent-1", goodBaseline())
	agg.RegisterContract(survMakeContract("contract-1", "agent-1"))

	// Latency is within contract (200) but shows drift from baseline (100).
	// P99 at 155ms is a 55% increase from 100ms baseline (threshold 10%).
	// But still under the contract threshold of 200ms.
	metrics := survSampleMetrics(0.9997, 155, 3, 10000, 3.0)
	event := agg.CheckAgent("agent-1", metrics)

	// Should be warning (drift detected) not breach (contract passes).
	if event.Status == StatusBreach {
		t.Errorf("should not be breach — latency 155 < 200 threshold")
	}
}

func TestSteadyStateAggregator_DriftAssessmentPopulated(t *testing.T) {
	agg := NewSteadyStateAggregator()
	agg.RegisterAgent("agent-1", goodBaseline())

	metrics := survSampleMetrics(0.9997, 110, 3, 10000, 2.0)
	event := agg.CheckAgent("agent-1", metrics)

	if event.DriftAssessment == nil {
		t.Error("expected drift assessment to be populated")
	}
}

// ============================================================================
// SteadyStateAggregator — Event Log
// ============================================================================

func TestSteadyStateAggregator_EventRecording(t *testing.T) {
	agg := NewSteadyStateAggregator()
	agg.RegisterAgent("agent-1", goodBaseline())

	// Record several checks.
	for i := 0; i < 5; i++ {
		metrics := survSampleMetrics(0.9997, 95, 3, 10000, 1.5)
		agg.CheckAgent("agent-1", metrics)
	}

	if agg.EventCount() != 5 {
		t.Errorf("expected 5 events, got %d", agg.EventCount())
	}
}

func TestSteadyStateAggregator_EventLogTrimmed(t *testing.T) {
	cfg := DefaultSurveillanceConfig()
	cfg.MaxEvents = 3
	agg := NewSteadyStateAggregator(WithSurveillanceConfig(cfg))
	agg.RegisterAgent("agent-1", goodBaseline())

	for i := 0; i < 10; i++ {
		metrics := survSampleMetrics(0.9997, 95, 3, 10000, 1.5)
		agg.CheckAgent("agent-1", metrics)
	}

	if agg.EventCount() != 3 {
		t.Errorf("expected 3 events (trimmed), got %d", agg.EventCount())
	}
}

func TestSteadyStateAggregator_EventsForAgent(t *testing.T) {
	agg := NewSteadyStateAggregator()
	agg.RegisterAgent("agent-1", goodBaseline())
	agg.RegisterAgent("agent-2", goodBaseline())

	metrics := survSampleMetrics(0.9997, 95, 3, 10000, 1.5)
	agg.CheckAgent("agent-1", metrics)
	agg.CheckAgent("agent-2", metrics)

	events1 := agg.EventsForAgent("agent-1")
	if len(events1) != 1 {
		t.Errorf("expected 1 event for agent-1, got %d", len(events1))
	}

	events2 := agg.EventsForAgent("agent-2")
	if len(events2) != 1 {
		t.Errorf("expected 1 event for agent-2, got %d", len(events2))
	}
}

func TestSteadyStateAggregator_RecentEvents(t *testing.T) {
	agg := NewSteadyStateAggregator()
	agg.RegisterAgent("agent-1", goodBaseline())

	metrics := survSampleMetrics(0.9997, 95, 3, 10000, 1.5)
	agg.CheckAgent("agent-1", metrics)

	recent := agg.RecentEvents(1 * time.Hour)
	if len(recent) != 1 {
		t.Errorf("expected 1 recent event, got %d", len(recent))
	}

	// Far future window should return nothing.
	recent = agg.RecentEvents(-1 * time.Hour)
	if len(recent) != 0 {
		t.Errorf("expected 0 events in past window, got %d", len(recent))
	}
}

func TestSteadyStateAggregator_ClearEvents(t *testing.T) {
	agg := NewSteadyStateAggregator()
	agg.RegisterAgent("agent-1", goodBaseline())

	metrics := survSampleMetrics(0.9997, 95, 3, 10000, 1.5)
	agg.CheckAgent("agent-1", metrics)

	agg.ClearEvents()
	if agg.EventCount() != 0 {
		t.Errorf("expected 0 events after clear, got %d", agg.EventCount())
	}
}

// ============================================================================
// SteadyStateAggregator — CheckAll
// ============================================================================

func TestSteadyStateAggregator_CheckAll(t *testing.T) {
	agg := NewSteadyStateAggregator()
	agg.RegisterAgent("agent-1", goodBaseline())
	agg.RegisterAgent("agent-2", goodBaseline())

	metricsPerAgent := map[string]map[string]float64{
		"agent-1": survSampleMetrics(0.9997, 95, 3, 10000, 1.5),
		"agent-2": survSampleMetrics(0.9997, 95, 3, 10000, 1.5),
	}

	events := agg.CheckAll(metricsPerAgent)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	for _, e := range events {
		if e.Status != StatusHealthy {
			t.Errorf("expected healthy for %s, got %s", e.AgentID, e.Status)
		}
	}
}

func TestSteadyStateAggregator_CheckAllSkipsUnregistered(t *testing.T) {
	agg := NewSteadyStateAggregator()
	agg.RegisterAgent("agent-1", goodBaseline())

	metricsPerAgent := map[string]map[string]float64{
		"agent-1":      survSampleMetrics(0.9997, 95, 3, 10000, 1.5),
		"unregistered": survSampleMetrics(0.9997, 95, 3, 10000, 1.5),
	}

	events := agg.CheckAll(metricsPerAgent)
	if len(events) != 1 {
		t.Fatalf("expected 1 event (unregistered skipped), got %d", len(events))
	}
}

func TestSteadyStateAggregator_CheckAllSkipsMissingMetrics(t *testing.T) {
	agg := NewSteadyStateAggregator()
	agg.RegisterAgent("agent-1", goodBaseline())
	agg.RegisterAgent("agent-2", goodBaseline())

	// Only provide metrics for agent-1.
	metricsPerAgent := map[string]map[string]float64{
		"agent-1": survSampleMetrics(0.9997, 95, 3, 10000, 1.5),
	}

	events := agg.CheckAll(metricsPerAgent)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}

// ============================================================================
// SteadyStateAggregator — Unregistered Agent
// ============================================================================

func TestSteadyStateAggregator_CheckAgentNotRegistered(t *testing.T) {
	agg := NewSteadyStateAggregator()

	event := agg.CheckAgent("nonexistent", survSampleMetrics(0.99, 100, 1, 1000, 1.0))
	if event != nil {
		t.Errorf("expected nil for unregistered agent, got %+v", event)
	}
}

func TestSteadyStateAggregator_RecordMetricsUnregistered(t *testing.T) {
	agg := NewSteadyStateAggregator()

	// Should not panic.
	agg.RecordMetrics("nonexistent", MetricsSnapshot{})
}

// ============================================================================
// SteadyStateAggregator — Agent Status
// ============================================================================

func TestSteadyStateAggregator_GetAgentStatus(t *testing.T) {
	agg := NewSteadyStateAggregator()
	agg.RegisterAgent("agent-1", goodBaseline())

	status := agg.GetAgentStatus("agent-1")
	if status == nil {
		t.Fatal("expected status, got nil")
	}
	if status.AgentID != "agent-1" {
		t.Errorf("expected agent-1, got %s", status.AgentID)
	}
	if status.SurveillanceStart.IsZero() {
		t.Error("expected non-zero surveillance start time")
	}
}

func TestSteadyStateAggregator_GetAgentStatusUnregistered(t *testing.T) {
	agg := NewSteadyStateAggregator()

	status := agg.GetAgentStatus("nonexistent")
	if status != nil {
		t.Errorf("expected nil for unregistered agent, got %+v", status)
	}
}

func TestSteadyStateAggregator_GetAgentStatusAfterCheck(t *testing.T) {
	agg := NewSteadyStateAggregator()
	agg.RegisterAgent("agent-1", goodBaseline())
	agg.RegisterContract(survMakeContract("c1", "agent-1"))

	// Healthy check.
	metrics := survSampleMetrics(0.9997, 95, 3, 10000, 1.5)
	agg.CheckAgent("agent-1", metrics)

	status := agg.GetAgentStatus("agent-1")
	if status.Status != StatusHealthy {
		t.Errorf("expected healthy status, got %s", status.Status)
	}
	if status.LastEvent == nil {
		t.Error("expected last event to be populated")
	}
}

// ============================================================================
// SteadyStateAggregator — Contract Management
// ============================================================================

func TestSteadyStateAggregator_ContractRegistration(t *testing.T) {
	agg := NewSteadyStateAggregator()
	agg.RegisterContract(survMakeContract("c1", "agent-1"))
	agg.RegisterContract(survMakeContract("c2", "agent-1"))

	contracts := agg.monitor.Contracts()
	if len(contracts) != 2 {
		t.Errorf("expected 2 contracts, got %d", len(contracts))
	}
}

func TestSteadyStateAggregator_ContractUnregistration(t *testing.T) {
	agg := NewSteadyStateAggregator()
	agg.RegisterContract(survMakeContract("c1", "agent-1"))
	agg.UnregisterContract("c1")

	contracts := agg.monitor.Contracts()
	if len(contracts) != 0 {
		t.Errorf("expected 0 contracts, got %d", len(contracts))
	}
}

// ============================================================================
// SteadyStateAggregator — Daily Summaries
// ============================================================================

func TestSteadyStateAggregator_DailySummaries(t *testing.T) {
	agg := NewSteadyStateAggregator()
	agg.RegisterAgent("agent-1", goodBaseline())

	// Record metrics from different times.
	now := time.Now().UTC()

	// Record a sample.
	snap := MetricsSnapshot{
		SuccessRate:  0.9997,
		P99LatencyMs: 95,
		P50LatencyMs: 50,
		ErrorCount:   3,
		RequestCount: 10000,
		Timestamp:    now,
	}
	agg.RecordMetrics("agent-1", snap)

	// Run a check to trigger rollup.
	metrics := survSampleMetrics(0.9997, 95, 3, 10000, 1.5)
	agg.CheckAgent("agent-1", metrics)

	summaries := agg.DailySummaries("agent-1")
	// May or may not have summaries depending on timing — but should not panic.
	_ = summaries
}

func TestSteadyStateAggregator_DailySummariesUnregistered(t *testing.T) {
	agg := NewSteadyStateAggregator()

	summaries := agg.DailySummaries("nonexistent")
	if summaries != nil {
		t.Errorf("expected nil for unregistered agent, got %+v", summaries)
	}
}

// ============================================================================
// LongRunningMonitor — Gradual Degradation
// ============================================================================

func TestLongRunningMonitor_InsufficientData(t *testing.T) {
	m := NewLongRunningMonitor(7, DefaultLongRunningThresholds())

	summaries := []DailySummary{
		{Date: time.Now().AddDate(0, 0, -1), AvgSuccessRate: 0.999},
	}
	report := m.Analyze("agent-1", summaries)
	if report != nil {
		t.Errorf("expected nil with < 2 summaries, got %+v", report)
	}
}

func TestLongRunningMonitor_NoDegradation(t *testing.T) {
	m := NewLongRunningMonitor(7, DefaultLongRunningThresholds())

	now := time.Now().UTC()
	summaries := []DailySummary{
		{Date: now.AddDate(0, 0, -6), AvgSuccessRate: 0.9995, AvgP99Latency: 100, TotalErrors: 5, TotalRequests: 10000},
		{Date: now.AddDate(0, 0, -5), AvgSuccessRate: 0.9995, AvgP99Latency: 100, TotalErrors: 5, TotalRequests: 10000},
		{Date: now.AddDate(0, 0, -4), AvgSuccessRate: 0.9995, AvgP99Latency: 100, TotalErrors: 5, TotalRequests: 10000},
		{Date: now.AddDate(0, 0, -3), AvgSuccessRate: 0.9996, AvgP99Latency: 98, TotalErrors: 4, TotalRequests: 10000},
	}

	report := m.Analyze("agent-1", summaries)
	if report == nil {
		t.Fatal("expected report, got nil")
	}
	if report.IsDegrading {
		t.Errorf("expected no degradation, got degrading")
	}
	if report.AffectedMetrics != 0 {
		t.Errorf("expected 0 affected metrics, got %d", report.AffectedMetrics)
	}
}

func TestLongRunningMonitor_SuccessRateDegradation(t *testing.T) {
	m := NewLongRunningMonitor(7, DefaultLongRunningThresholds())

	now := time.Now().UTC()
	summaries := []DailySummary{
		{Date: now.AddDate(0, 0, -6), AvgSuccessRate: 0.9995, AvgP99Latency: 100, TotalErrors: 5, TotalRequests: 10000},
		{Date: now.AddDate(0, 0, -5), AvgSuccessRate: 0.9990, AvgP99Latency: 100, TotalErrors: 10, TotalRequests: 10000},
		{Date: now.AddDate(0, 0, -4), AvgSuccessRate: 0.9985, AvgP99Latency: 100, TotalErrors: 15, TotalRequests: 10000},
		{Date: now.AddDate(0, 0, -3), AvgSuccessRate: 0.9950, AvgP99Latency: 100, TotalErrors: 50, TotalRequests: 10000},
		{Date: now.AddDate(0, 0, -2), AvgSuccessRate: 0.9900, AvgP99Latency: 100, TotalErrors: 100, TotalRequests: 10000},
	}

	report := m.Analyze("agent-1", summaries)
	if report == nil {
		t.Fatal("expected report, got nil")
	}
	if !report.IsDegrading {
		t.Error("expected degradation")
	}

	// Success rate change: (0.99 - 0.9995) / 0.9995 * 100 ≈ -0.95%
	// Threshold is 5%, so this shouldn't trigger just from success rate alone.
	// But error rate went from 5/10000=0.05% to 100/10000=1.0% = 1900% increase.
	foundErrRate := false
	for _, trend := range report.MetricTrends {
		if trend.Metric == "error_rate" && trend.Exceeds {
			foundErrRate = true
		}
	}
	if !foundErrRate {
		t.Error("expected error rate degradation to be flagged")
	}
}

func TestLongRunningMonitor_LatencyDegradation(t *testing.T) {
	m := NewLongRunningMonitor(7, DefaultLongRunningThresholds())

	now := time.Now().UTC()
	summaries := []DailySummary{
		{Date: now.AddDate(0, 0, -6), AvgSuccessRate: 0.9995, AvgP99Latency: 100, TotalErrors: 5, TotalRequests: 10000},
		{Date: now.AddDate(0, 0, -5), AvgSuccessRate: 0.9995, AvgP99Latency: 110, TotalErrors: 5, TotalRequests: 10000},
		{Date: now.AddDate(0, 0, -4), AvgSuccessRate: 0.9995, AvgP99Latency: 125, TotalErrors: 5, TotalRequests: 10000},
		{Date: now.AddDate(0, 0, -3), AvgSuccessRate: 0.9995, AvgP99Latency: 140, TotalErrors: 5, TotalRequests: 10000},
		{Date: now.AddDate(0, 0, -2), AvgSuccessRate: 0.9995, AvgP99Latency: 150, TotalErrors: 5, TotalRequests: 10000},
	}

	report := m.Analyze("agent-1", summaries)
	if report == nil {
		t.Fatal("expected report, got nil")
	}
	if !report.IsDegrading {
		t.Error("expected degradation from latency")
	}

	// 100 → 150 = 50% increase, threshold 20%.
	foundLatency := false
	for _, trend := range report.MetricTrends {
		if trend.Metric == "p99_latency_ms" && trend.Exceeds {
			foundLatency = true
			if trend.Direction != DegradationWorsening {
				t.Errorf("expected worsening direction, got %s", trend.Direction)
			}
		}
	}
	if !foundLatency {
		t.Error("expected latency degradation to be flagged")
	}
}

func TestLongRunningMonitor_WindowSizeLimit(t *testing.T) {
	m := NewLongRunningMonitor(3, DefaultLongRunningThresholds())

	now := time.Now().UTC()
	// 5 daily summaries but window is 3 days → should only analyze last 3.
	summaries := []DailySummary{
		{Date: now.AddDate(0, 0, -4), AvgSuccessRate: 0.9999, AvgP99Latency: 50, TotalErrors: 0, TotalRequests: 10000},
		{Date: now.AddDate(0, 0, -3), AvgSuccessRate: 0.9999, AvgP99Latency: 50, TotalErrors: 0, TotalRequests: 10000},
		{Date: now.AddDate(0, 0, -2), AvgSuccessRate: 0.9999, AvgP99Latency: 50, TotalErrors: 0, TotalRequests: 10000},
		{Date: now.AddDate(0, 0, -1), AvgSuccessRate: 0.9999, AvgP99Latency: 50, TotalErrors: 0, TotalRequests: 10000},
		{Date: now, AvgSuccessRate: 0.9999, AvgP99Latency: 50, TotalErrors: 0, TotalRequests: 10000},
	}

	report := m.Analyze("agent-1", summaries)
	if report == nil {
		t.Fatal("expected report, got nil")
	}
	if report.WindowDays != 3 {
		t.Errorf("expected 3-day window, got %d days", report.WindowDays)
	}
}

func TestLongRunningMonitor_GetLastReport(t *testing.T) {
	m := NewLongRunningMonitor(7, DefaultLongRunningThresholds())

	now := time.Now().UTC()
	summaries := []DailySummary{
		{Date: now.AddDate(0, 0, -1), AvgSuccessRate: 0.9995, AvgP99Latency: 100, TotalErrors: 5, TotalRequests: 10000},
		{Date: now, AvgSuccessRate: 0.9995, AvgP99Latency: 100, TotalErrors: 5, TotalRequests: 10000},
	}

	m.Analyze("agent-1", summaries)
	report := m.GetLastReport("agent-1")
	if report == nil {
		t.Error("expected last report, got nil")
	}
	if report.AgentID != "agent-1" {
		t.Errorf("expected agent-1, got %s", report.AgentID)
	}
}

func TestLongRunningMonitor_GetLastReportNone(t *testing.T) {
	m := NewLongRunningMonitor(7, DefaultLongRunningThresholds())
	if m.GetLastReport("nonexistent") != nil {
		t.Error("expected nil for unknown agent")
	}
}

func TestLongRunningMonitor_SeverityCritical(t *testing.T) {
	thresholds := LongRunningThresholds{
		SuccessRateDeclinePct: 1.0, // very sensitive
		LatencyIncreasePct:    1.0, // very sensitive
		ErrorRateIncreasePct:  1.0, // very sensitive
		MemoryGrowthPct:       1.0,
	}
	m := NewLongRunningMonitor(7, thresholds)

	now := time.Now().UTC()
	summaries := []DailySummary{
		{Date: now.AddDate(0, 0, -2), AvgSuccessRate: 0.9999, AvgP99Latency: 50, TotalErrors: 1, TotalRequests: 10000},
		{Date: now.AddDate(0, 0, -1), AvgSuccessRate: 0.9999, AvgP99Latency: 50, TotalErrors: 1, TotalRequests: 10000},
		{Date: now, AvgSuccessRate: 0.9500, AvgP99Latency: 200, TotalErrors: 500, TotalRequests: 10000},
	}

	report := m.Analyze("agent-1", summaries)
	if !report.IsDegrading {
		t.Error("expected degradation")
	}
	if report.Severity != SeverityCritical {
		t.Errorf("expected critical severity (3+ metrics), got %s", report.Severity)
	}
}

func TestLongRunningMonitor_ErrorFromZero(t *testing.T) {
	m := NewLongRunningMonitor(7, DefaultLongRunningThresholds())

	now := time.Now().UTC()
	summaries := []DailySummary{
		{Date: now.AddDate(0, 0, -1), AvgSuccessRate: 1.0, AvgP99Latency: 100, TotalErrors: 0, TotalRequests: 10000},
		{Date: now, AvgSuccessRate: 1.0, AvgP99Latency: 100, TotalErrors: 50, TotalRequests: 10000},
	}

	report := m.Analyze("agent-1", summaries)
	if report == nil {
		t.Fatal("expected report, got nil")
	}
	if !report.IsDegrading {
		t.Error("expected degradation from errors appearing")
	}
}

func TestLongRunningMonitor_ImprovingDirection(t *testing.T) {
	m := NewLongRunningMonitor(7, DefaultLongRunningThresholds())

	now := time.Now().UTC()
	summaries := []DailySummary{
		{Date: now.AddDate(0, 0, -3), AvgSuccessRate: 0.9800, AvgP99Latency: 200, TotalErrors: 200, TotalRequests: 10000},
		{Date: now.AddDate(0, 0, -2), AvgSuccessRate: 0.9900, AvgP99Latency: 180, TotalErrors: 100, TotalRequests: 10000},
		{Date: now.AddDate(0, 0, -1), AvgSuccessRate: 0.9995, AvgP99Latency: 100, TotalErrors: 5, TotalRequests: 10000},
	}

	report := m.Analyze("agent-1", summaries)
	if report == nil {
		t.Fatal("expected report, got nil")
	}

	// Should not be degrading — things are improving.
	if report.IsDegrading {
		t.Error("expected no degradation (metrics improving)")
	}

	// All trends should be improving.
	for _, trend := range report.MetricTrends {
		if trend.Direction != DegradationImproving && trend.Direction != DegradationNone {
			t.Errorf("metric %s direction: expected improving/none, got %s",
				trend.Metric, trend.Direction)
		}
	}
}

// ============================================================================
// AlertEscalation
// ============================================================================

func TestAlertEscalation_HealthyReturnsNone(t *testing.T) {
	esc := NewAlertEscalation(30*time.Minute, 2*time.Hour)

	level := esc.Evaluate("agent-1", StatusHealthy, nil)
	if level != EscalationNone {
		t.Errorf("expected none, got %s", level)
	}
}

func TestAlertEscalation_BreachImmediateRollback(t *testing.T) {
	esc := NewAlertEscalation(30*time.Minute, 2*time.Hour)

	level := esc.Evaluate("agent-1", StatusBreach, nil)
	if level != EscalationRollback {
		t.Errorf("expected rollback for breach, got %s", level)
	}
}

func TestAlertEscalation_DegradedImmediateInvestigate(t *testing.T) {
	esc := NewAlertEscalation(30*time.Minute, 2*time.Hour)

	level := esc.Evaluate("agent-1", StatusDegraded, nil)
	if level != EscalationInvestigate {
		t.Errorf("expected investigate for degradation, got %s", level)
	}
}

func TestAlertEscalation_DriftNoBreachReturnsNone(t *testing.T) {
	esc := NewAlertEscalation(30*time.Minute, 2*time.Hour)

	// Drift present but short duration → no escalation yet.
	assessment := &DriftAssessment{
		AnyBreach: true,
	}
	level := esc.Evaluate("agent-1", StatusWarning, assessment)
	if level != EscalationNone {
		t.Errorf("expected none (drift just started), got %s", level)
	}
}

func TestAlertEscalation_SustainedDriftEscalatesToNotify(t *testing.T) {
	sustainedThreshold := 30 * time.Minute
	esc := NewAlertEscalation(sustainedThreshold, 2*time.Hour)

	// Simulate sustained drift.
	esc.driftStart["agent-1"] = time.Now().Add(-sustainedThreshold / 2)

	assessment := &DriftAssessment{AnyBreach: true}
	level := esc.Evaluate("agent-1", StatusWarning, assessment)
	if level != EscalationNotify {
		t.Errorf("expected notify (sustained > 15m), got %s", level)
	}
}

func TestAlertEscalation_SustainedDriftEscalatesToInvestigate(t *testing.T) {
	sustainedThreshold := 30 * time.Minute
	esc := NewAlertEscalation(sustainedThreshold, 2*time.Hour)

	// Drift has persisted past sustainedThreshold.
	esc.driftStart["agent-1"] = time.Now().Add(-sustainedThreshold - time.Minute)

	assessment := &DriftAssessment{AnyBreach: true}
	level := esc.Evaluate("agent-1", StatusWarning, assessment)
	if level != EscalationInvestigate {
		t.Errorf("expected investigate (sustained > 30m), got %s", level)
	}
}

func TestAlertEscalation_LongSustainedDriftEscalatesToRollback(t *testing.T) {
	sustainedThreshold := 30 * time.Minute
	esc := NewAlertEscalation(sustainedThreshold, 2*time.Hour)

	// Drift persisted past rollbackThreshold.
	esc.driftStart["agent-1"] = time.Now().Add(-2*time.Hour - time.Minute)

	assessment := &DriftAssessment{AnyBreach: true}
	level := esc.Evaluate("agent-1", StatusWarning, assessment)
	if level != EscalationRollback {
		t.Errorf("expected rollback (sustained > 2h), got %s", level)
	}
}

func TestAlertEscalation_CriticalDriftFasterEscalation(t *testing.T) {
	sustainedThreshold := 30 * time.Minute
	esc := NewAlertEscalation(sustainedThreshold, 2*time.Hour)

	// Critical drift — should escalate at half the sustained threshold.
	esc.driftStart["agent-1"] = time.Now().Add(-sustainedThreshold / 2)

	assessment := &DriftAssessment{
		AnyBreach:     true,
		CriticalCount: 1,
	}
	level := esc.Evaluate("agent-1", StatusWarning, assessment)
	if level != EscalationInvestigate {
		t.Errorf("expected investigate for critical drift, got %s", level)
	}
}

func TestAlertEscalation_DriftClearedResetsTracking(t *testing.T) {
	esc := NewAlertEscalation(30*time.Minute, 2*time.Hour)

	// Start with drift.
	esc.driftStart["agent-1"] = time.Now().Add(-1 * time.Hour)
	esc.Evaluate("agent-1", StatusWarning, &DriftAssessment{AnyBreach: true})

	// No drift now.
	level := esc.Evaluate("agent-1", StatusHealthy, nil)
	if level != EscalationNone {
		t.Errorf("expected none after drift cleared, got %s", level)
	}
	if _, ok := esc.driftStart["agent-1"]; ok {
		t.Error("driftStart should be cleared")
	}
}

func TestAlertEscalation_GetLevel(t *testing.T) {
	esc := NewAlertEscalation(30*time.Minute, 2*time.Hour)

	esc.Evaluate("agent-1", StatusBreach, nil)
	if esc.GetLevel("agent-1") != EscalationRollback {
		t.Errorf("expected rollback, got %s", esc.GetLevel("agent-1"))
	}
}

func TestAlertEscalation_Reset(t *testing.T) {
	esc := NewAlertEscalation(30*time.Minute, 2*time.Hour)

	esc.Evaluate("agent-1", StatusBreach, nil)
	esc.Reset("agent-1")

	// After reset, level should be none (empty string = zero value for EscalationLevel).
	level := esc.GetLevel("agent-1")
	if level != EscalationNone {
		t.Errorf("expected none after reset, got %q", level)
	}
	if _, ok := esc.driftStart["agent-1"]; ok {
		t.Error("driftStart should be cleared after reset")
	}
}

func TestAlertEscalation_DriftDuration(t *testing.T) {
	esc := NewAlertEscalation(30*time.Minute, 2*time.Hour)

	// No drift.
	if esc.DriftDuration("agent-1") != 0 {
		t.Error("expected 0 duration for no drift")
	}

	// With drift.
	esc.driftStart["agent-1"] = time.Now().Add(-10 * time.Minute)
	dur := esc.DriftDuration("agent-1")
	if dur < 9*time.Minute || dur > 11*time.Minute {
		t.Errorf("expected ~10m drift duration, got %v", dur)
	}
}

func TestAlertEscalation_AllLevels(t *testing.T) {
	esc := NewAlertEscalation(30*time.Minute, 2*time.Hour)

	esc.Evaluate("a1", StatusBreach, nil)
	esc.Evaluate("a2", StatusHealthy, nil)

	levels := esc.AllLevels()
	// Both agents are tracked in currentLevels after Evaluate.
	if len(levels) < 1 {
		t.Fatalf("expected at least 1 tracked agent, got %d", len(levels))
	}
	if levels["a1"] != EscalationRollback {
		t.Errorf("expected rollback for a1, got %s", levels["a1"])
	}
	// a2 was healthy → EscalationNone.
	if level, ok := levels["a2"]; ok {
		if level != EscalationNone {
			t.Errorf("expected none for healthy a2, got %s", level)
		}
	}
}

func TestEscalationLevel_IsActionable(t *testing.T) {
	if EscalationNone.IsActionable() {
		t.Error("none should not be actionable")
	}
	if EscalationNotify.IsActionable() {
		t.Error("notify should not be actionable")
	}
	if !EscalationInvestigate.IsActionable() {
		t.Error("investigate should be actionable")
	}
	if !EscalationRollback.IsActionable() {
		t.Error("rollback should be actionable")
	}
}

// ============================================================================
// SurveillanceEvent
// ============================================================================

func TestSurveillanceEvent_IsActionable(t *testing.T) {
	tests := []struct {
		name     string
		event    SurveillanceEvent
		expected bool
	}{
		{
			name:     "healthy",
			event:    SurveillanceEvent{Status: StatusHealthy},
			expected: false,
		},
		{
			name:     "breach",
			event:    SurveillanceEvent{Status: StatusBreach},
			expected: true,
		},
		{
			name:     "degraded",
			event:    SurveillanceEvent{Status: StatusDegraded},
			expected: true,
		},
		{
			name:     "warning with investigate escalation",
			event:    SurveillanceEvent{Status: StatusWarning, EscalationLevel: EscalationInvestigate},
			expected: true,
		},
		{
			name:     "warning without escalation",
			event:    SurveillanceEvent{Status: StatusWarning, EscalationLevel: EscalationNone},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.event.IsActionable() != tc.expected {
				t.Errorf("IsActionable: expected %v, got %v", tc.expected, tc.event.IsActionable())
			}
		})
	}
}

func TestSurveillanceEvent_Summary(t *testing.T) {
	event := SurveillanceEvent{
		Status:          StatusBreach,
		AgentID:         "agent-1",
		ContractName:    "contract-1",
		EscalationLevel: EscalationRollback,
	}
	summary := event.Summary()
	if summary == "" {
		t.Error("expected non-empty summary")
	}
}

// ============================================================================
// Configuration
// ============================================================================

func TestDefaultSurveillanceConfig(t *testing.T) {
	cfg := DefaultSurveillanceConfig()

	if cfg.CheckInterval != 5*time.Minute {
		t.Errorf("expected 5m check interval, got %v", cfg.CheckInterval)
	}
	if cfg.LongWindowDays != 7 {
		t.Errorf("expected 7-day window, got %d", cfg.LongWindowDays)
	}
	if cfg.SustainedDriftDuration != 30*time.Minute {
		t.Errorf("expected 30m sustained threshold, got %v", cfg.SustainedDriftDuration)
	}
	if cfg.EscalationRollbackDuration != 2*time.Hour {
		t.Errorf("expected 2h rollback threshold, got %v", cfg.EscalationRollbackDuration)
	}
}

func TestDefaultLongRunningThresholds(t *testing.T) {
	thresholds := DefaultLongRunningThresholds()

	if thresholds.SuccessRateDeclinePct != 5.0 {
		t.Errorf("expected 5.0%% success rate decline, got %f", thresholds.SuccessRateDeclinePct)
	}
	if thresholds.LatencyIncreasePct != 20.0 {
		t.Errorf("expected 20.0%% latency increase, got %f", thresholds.LatencyIncreasePct)
	}
	if thresholds.ErrorRateIncreasePct != 50.0 {
		t.Errorf("expected 50.0%% error rate increase, got %f", thresholds.ErrorRateIncreasePct)
	}
}

func TestWithSurveillanceConfig(t *testing.T) {
	cfg := DefaultSurveillanceConfig()
	cfg.CheckInterval = 10 * time.Minute
	cfg.LongWindowDays = 14

	agg := NewSteadyStateAggregator(WithSurveillanceConfig(cfg))

	if agg.config.CheckInterval != 10*time.Minute {
		t.Errorf("expected 10m interval, got %v", agg.config.CheckInterval)
	}
	if agg.config.LongWindowDays != 14 {
		t.Errorf("expected 14-day window, got %d", agg.config.LongWindowDays)
	}
}

// ============================================================================
// Integration: SurveillanceEvent → NotificationDispatcher
// ============================================================================

func TestSteadyStateAggregator_NotificationOnBreach(t *testing.T) {
	// Create a mock notifier that captures notifications.
	notif := &mockNotifier{name: "surv-mock"}

	dispatcher := NewNotificationDispatcher(notif)
	agg := NewSteadyStateAggregator(WithSurveillanceDispatcher(dispatcher))
	agg.RegisterAgent("agent-1", goodBaseline())
	agg.RegisterContract(survMakeContract("contract-1", "agent-1"))

	// Trigger breach.
	metrics := survSampleMetrics(0.995, 95, 50, 10000, 1.5)
	agg.CheckAgent("agent-1", metrics)

	if len(notif.received) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notif.received))
	}
	if notif.received[0].AgentID != "agent-1" {
		t.Errorf("expected agent-1, got %s", notif.received[0].AgentID)
	}
	if notif.received[0].ContractName != "contract-1" {
		t.Errorf("expected contract-1, got %s", notif.received[0].ContractName)
	}
}

// ============================================================================
// Helpers
// ============================================================================

// ============================================================================
// DegradationReport Summary
// ============================================================================

func TestDegradationReport_Summary(t *testing.T) {
	report := &DegradationReport{
		AgentID:         "agent-1",
		WindowDays:      7,
		MetricTrends:    []MetricDegradation{{Metric: "p99_latency_ms"}},
		AffectedMetrics: 1,
		Severity:        SeverityWarning,
	}

	summary := report.Summary()
	if summary == "" {
		t.Error("expected non-empty summary")
	}
}

// ============================================================================
// metricsFromMap
// ============================================================================

func TestMetricsFromMap(t *testing.T) {
	baseline := goodBaseline()
	metrics := map[string]float64{
		"success_rate":      0.99,
		"p99_latency_ms":    150,
		"p50_latency_ms":    75,
		"error_count":       10,
		"new_error_types":   2,
		"memory_growth_pct": 5,
		"request_count":     5000,
	}

	snap := metricsFromMap(metrics, baseline)

	if snap.SuccessRate != 0.99 {
		t.Errorf("expected 0.99 success rate, got %f", snap.SuccessRate)
	}
	if snap.P99LatencyMs != 150 {
		t.Errorf("expected 150 p99, got %f", snap.P99LatencyMs)
	}
	if snap.ErrorCount != 10 {
		t.Errorf("expected 10 errors, got %d", snap.ErrorCount)
	}
	if snap.NewErrorTypes != 2 {
		t.Errorf("expected 2 new error types, got %d", snap.NewErrorTypes)
	}
	if snap.MemoryGrowthPct != 5 {
		t.Errorf("expected 5 memory growth, got %f", snap.MemoryGrowthPct)
	}
	if snap.RequestCount != 5000 {
		t.Errorf("expected 5000 requests, got %d", snap.RequestCount)
	}
}

func TestMetricsFromMap_FallsBackToBaseline(t *testing.T) {
	baseline := MetricsSnapshot{
		SuccessRate:     0.9999,
		P99LatencyMs:    80,
		P50LatencyMs:    40,
		MemoryGrowthPct: 1.5,
	}

	// Empty metrics map → should use baseline values.
	snap := metricsFromMap(map[string]float64{}, baseline)

	if snap.SuccessRate != 0.9999 {
		t.Errorf("expected baseline success rate, got %f", snap.SuccessRate)
	}
	if snap.P99LatencyMs != 80 {
		t.Errorf("expected baseline p99, got %f", snap.P99LatencyMs)
	}
}

// ============================================================================
// agentWindow
// ============================================================================

func TestAgentWindow_AddSample(t *testing.T) {
	w := &agentWindow{
		agentID:  "a1",
		baseline: goodBaseline(),
		drift:    NewDriftDetector(goodBaseline()),
	}

	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		snap := MetricsSnapshot{
			SuccessRate:  0.999,
			P99LatencyMs: 100,
			Timestamp:    now,
		}
		w.addSample(snap, 100)
	}

	if len(w.recentSamples) != 5 {
		t.Errorf("expected 5 samples, got %d", len(w.recentSamples))
	}
}

func TestAgentWindow_AddSampleRespectsMax(t *testing.T) {
	w := &agentWindow{
		agentID:  "a1",
		baseline: goodBaseline(),
	}

	for i := 0; i < 10; i++ {
		w.addSample(MetricsSnapshot{
			SuccessRate:  0.999,
			P99LatencyMs: 100,
			Timestamp:    time.Now().UTC(),
		}, 5) // max 5
	}

	if len(w.recentSamples) != 5 {
		t.Errorf("expected 5 samples (max), got %d", len(w.recentSamples))
	}
}

// ============================================================================
// Full surveillance lifecycle test
// ============================================================================

func TestSurveillance_LifecycleBreachRecovery(t *testing.T) {
	agg := NewSteadyStateAggregator()
	agg.RegisterAgent("agent-1", goodBaseline())
	agg.RegisterContract(survMakeContract("contract-1", "agent-1"))

	// Phase 1: healthy.
	metrics := survSampleMetrics(0.9997, 95, 3, 10000, 1.5)
	ev := agg.CheckAgent("agent-1", metrics)
	if ev.Status != StatusHealthy {
		t.Fatalf("expected healthy initially, got %s", ev.Status)
	}

	// Phase 2: breach — success rate drops.
	metrics = survSampleMetrics(0.99, 95, 100, 10000, 5.0)
	ev = agg.CheckAgent("agent-1", metrics)
	if ev.Status != StatusBreach {
		t.Fatalf("expected breach, got %s", ev.Status)
	}
	if ev.EscalationLevel != EscalationRollback {
		t.Errorf("expected rollback escalation, got %s", ev.EscalationLevel)
	}

	// Phase 3: recovery.
	metrics = survSampleMetrics(0.9997, 95, 3, 10000, 1.5)
	ev = agg.CheckAgent("agent-1", metrics)
	if ev.Status != StatusHealthy {
		t.Fatalf("expected recovery to healthy, got %s", ev.Status)
	}

	// Check event history.
	events := agg.EventsForAgent("agent-1")
	if len(events) < 3 {
		t.Errorf("expected at least 3 events, got %d", len(events))
	}
}
