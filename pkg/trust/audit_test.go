package trust

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ============================================================================
// Test Helpers
// ============================================================================

func writeAuditLedger(t *testing.T, events []TrustEvent) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "trust-ledger.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create ledger: %v", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, evt := range events {
		if err := enc.Encode(evt); err != nil {
			t.Fatalf("encode event: %v", err)
		}
	}
	return path
}

func makeAuditMergeEvent(agentID string, ts time.Time, before, after float64) TrustEvent {
	return TrustEvent{
		AgentID:   agentID,
		EventType: EventMergeSuccess,
		Timestamp: ts,
		Data: EventData{
			ScoreBefore: before,
			ScoreAfter:  after,
		},
	}
}

func makeAuditTierEvent(agentID string, ts time.Time, oldTier, newTier string) TrustEvent {
	return TrustEvent{
		AgentID:   agentID,
		EventType: EventTierChange,
		Timestamp: ts,
		Data: EventData{
			OldTier: oldTier,
			NewTier: newTier,
		},
	}
}

func makeAuditIncidentEvent(agentID string, ts time.Time, before, after float64) TrustEvent {
	return TrustEvent{
		AgentID:   agentID,
		EventType: EventIncidentPenalty,
		Timestamp: ts,
		Data: EventData{
			ScoreBefore:   before,
			ScoreAfter:    after,
			AttributionWt: 0.3,
		},
	}
}

// ============================================================================
// Tests — AuditFinding
// ============================================================================

func TestAuditFinding_IsPassing(t *testing.T) {
	f := AuditFinding{Status: AuditPass}
	if !f.IsPassing() {
		t.Error("PASS should be passing")
	}
}

func TestAuditFinding_IsPassingFalse(t *testing.T) {
	f := AuditFinding{Status: AuditFail}
	if f.IsPassing() {
		t.Error("FAIL should not be passing")
	}
}

func TestAuditFinding_HasAnomaly(t *testing.T) {
	f := AuditFinding{Status: AuditAnomaly}
	if !f.HasAnomaly() {
		t.Error("ANOMALY should have anomaly")
	}
}

func TestAuditFinding_HasAnomalyFalse(t *testing.T) {
	f := AuditFinding{Status: AuditPass}
	if f.HasAnomaly() {
		t.Error("PASS should not have anomaly")
	}
}

// ============================================================================
// Tests — AuditReport
// ============================================================================

func TestAuditReport_IsHealthy(t *testing.T) {
	r := AuditReport{Summary: AuditSummary{Failed: 0}}
	if !r.IsHealthy() {
		t.Error("should be healthy with zero failures")
	}
}

func TestAuditReport_IsHealthyFalse(t *testing.T) {
	r := AuditReport{Summary: AuditSummary{Failed: 1}}
	if r.IsHealthy() {
		t.Error("should not be healthy with failures")
	}
}

func TestAuditReport_FindingByAgent(t *testing.T) {
	r := AuditReport{
		Findings: []AuditFinding{
			{AgentID: "a1", Status: AuditPass},
			{AgentID: "a2", Status: AuditFail},
		},
	}
	f := r.FindingByAgent("a2")
	if f == nil || f.AgentID != "a2" {
		t.Error("should find agent a2")
	}
}

func TestAuditReport_FindingByAgentNotFound(t *testing.T) {
	r := AuditReport{
		Findings: []AuditFinding{{AgentID: "a1"}},
	}
	if f := r.FindingByAgent("missing"); f != nil {
		t.Error("should return nil for missing agent")
	}
}

func TestAuditReport_FailedAgents(t *testing.T) {
	r := AuditReport{
		Findings: []AuditFinding{
			{AgentID: "a1", Status: AuditPass},
			{AgentID: "a2", Status: AuditFail},
			{AgentID: "a3", Status: AuditFail},
		},
	}
	failed := r.FailedAgents()
	if len(failed) != 2 {
		t.Errorf("expected 2 failed agents, got %d", len(failed))
	}
}

func TestAuditReport_FailedAgentsEmpty(t *testing.T) {
	r := AuditReport{
		Findings: []AuditFinding{{AgentID: "a1", Status: AuditPass}},
	}
	if len(r.FailedAgents()) != 0 {
		t.Error("should have no failed agents")
	}
}

func TestAuditReport_AnomalyAgents(t *testing.T) {
	r := AuditReport{
		Findings: []AuditFinding{
			{AgentID: "a1", Status: AuditPass},
			{AgentID: "a2", Status: AuditAnomaly},
		},
	}
	anomalies := r.AnomalyAgents()
	if len(anomalies) != 1 {
		t.Errorf("expected 1 anomaly agent, got %d", len(anomalies))
	}
}

func TestAuditReport_FormatReport(t *testing.T) {
	r := AuditReport{
		LedgerPath: "/tmp/ledger.jsonl",
		CheckedAt:  time.Now(),
		Findings: []AuditFinding{
			{AgentID: "a1", Status: AuditPass, ComputedScore: 0.75, ComputedTier: TierTrusted, EventCount: 5},
		},
		Summary: AuditSummary{Total: 1, Passed: 1},
	}
	report := r.FormatReport()
	if report == "" {
		t.Error("FormatReport should not be empty")
	}
}

// ============================================================================
// Tests — TrustAuditRunner construction
// ============================================================================

func TestNewTrustAuditRunner_Default(t *testing.T) {
	r := NewTrustAuditRunner("/tmp/ledger.jsonl")
	if r.ledgerPath != "/tmp/ledger.jsonl" {
		t.Error("ledgerPath mismatch")
	}
	if r.scoreTolerance != 0.01 {
		t.Errorf("default tolerance = %v, want 0.01", r.scoreTolerance)
	}
	if r.maxInactivityDays != 90 {
		t.Errorf("default maxInactivityDays = %v, want 90", r.maxInactivityDays)
	}
}

func TestNewTrustAuditRunner_WithOptions(t *testing.T) {
	r := NewTrustAuditRunner("/tmp/ledger.jsonl",
		WithScoreTolerance(0.05),
		WithMaxInactivityDays(30),
	)
	if r.scoreTolerance != 0.05 {
		t.Errorf("tolerance = %v, want 0.05", r.scoreTolerance)
	}
	if r.maxInactivityDays != 30 {
		t.Errorf("maxInactivityDays = %v, want 30", r.maxInactivityDays)
	}
}

// ============================================================================
// Tests — Run (full audit)
// ============================================================================

func TestRun_EmptyLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.jsonl")
	r := NewTrustAuditRunner(path)
	report, err := r.Run()
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if report.AgentCount != 0 {
		t.Errorf("expected 0 agents, got %d", report.AgentCount)
	}
	if !report.IsHealthy() {
		t.Error("empty ledger should be healthy")
	}
}

func TestRun_NonExistentLedger(t *testing.T) {
	r := NewTrustAuditRunner("/nonexistent/path/ledger.jsonl")
	report, err := r.Run()
	if err != nil {
		t.Fatalf("Run should not error on missing ledger: %v", err)
	}
	if report.AgentCount != 0 {
		t.Errorf("expected 0 agents, got %d", report.AgentCount)
	}
}

func TestRun_SingleAgentPass(t *testing.T) {
	now := time.Now().UTC()
	events := []TrustEvent{
		makeAuditMergeEvent("agent-001", now.AddDate(0, 0, -10), 0.5, 0.6),
		makeAuditMergeEvent("agent-001", now.AddDate(0, 0, -5), 0.6, 0.7),
	}
	path := writeAuditLedger(t, events)
	r := NewTrustAuditRunner(path)
	report, err := r.Run()
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if report.AgentCount != 1 {
		t.Fatalf("expected 1 agent, got %d", report.AgentCount)
	}
	f := report.FindingByAgent("agent-001")
	if f == nil {
		t.Fatal("expected finding for agent-001")
	}
	if f.Status != AuditPass && f.Status != AuditAnomaly {
		t.Errorf("expected PASS or ANOMALY, got %s", f.Status)
	}
	if f.EventCount != 2 {
		t.Errorf("expected 2 events, got %d", f.EventCount)
	}
}

func TestRun_MultipleAgents(t *testing.T) {
	now := time.Now().UTC()
	events := []TrustEvent{
		makeAuditMergeEvent("agent-a", now.AddDate(0, 0, -5), 0.5, 0.6),
		makeAuditMergeEvent("agent-b", now.AddDate(0, 0, -3), 0.5, 0.8),
	}
	path := writeAuditLedger(t, events)
	r := NewTrustAuditRunner(path)
	report, err := r.Run()
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if report.AgentCount != 2 {
		t.Errorf("expected 2 agents, got %d", report.AgentCount)
	}
}

func TestRun_AgentWithZeroEvents(t *testing.T) {
	// Agent appears in events but has 0 trust events (not in ledger at all)
	// This shouldn't happen from Run() since agentEvents only includes agents
	// that have events. Test that an empty ledger produces no findings.
	path := filepath.Join(t.TempDir(), "empty.jsonl")
	r := NewTrustAuditRunner(path)
	report, err := r.Run()
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(report.Findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(report.Findings))
	}
}

// ============================================================================
// Tests — Anomaly detection
// ============================================================================

func TestCheckScoreDrift_NoDrift(t *testing.T) {
	r := NewTrustAuditRunner("/tmp/ledger.jsonl")
	events := []TrustEvent{
		makeAuditMergeEvent("a1", time.Now(), 0.5, 0.7),
	}
	// Score should match the last score_after (with decay)
	a := r.checkScoreDrift("a1", events, TrustScore(0.7))
	// May or may not be nil depending on decay, but should not be "error" severity
	if a != nil && a.Severity == "error" {
		t.Errorf("expected no drift error for matching score, got %s", a.Detail)
	}
}

func TestCheckScoreDrift_WithDrift(t *testing.T) {
	r := NewTrustAuditRunner("/tmp/ledger.jsonl", WithScoreTolerance(0.001))
	events := []TrustEvent{
		makeAuditMergeEvent("a1", time.Now().AddDate(0, 0, -1), 0.5, 0.9),
	}
	a := r.checkScoreDrift("a1", events, TrustScore(0.5))
	if a == nil {
		t.Fatal("expected drift anomaly")
	}
	if a.Type != AnomalyScoreDrift {
		t.Errorf("expected score_drift, got %s", a.Type)
	}
}

func TestCheckScoreDrift_NoEvents(t *testing.T) {
	r := NewTrustAuditRunner("/tmp/ledger.jsonl")
	a := r.checkScoreDrift("a1", nil, TrustScore(0.5))
	if a != nil {
		t.Error("expected nil for no events")
	}
}

func TestCheckTierMismatch_NoTierEvents(t *testing.T) {
	r := NewTrustAuditRunner("/tmp/ledger.jsonl")
	events := []TrustEvent{
		makeAuditMergeEvent("a1", time.Now(), 0.5, 0.7),
	}
	a := r.checkTierMismatch(events, TierTrusted)
	if a != nil {
		t.Error("expected nil when no tier events")
	}
}

func TestCheckTierMismatch_Match(t *testing.T) {
	r := NewTrustAuditRunner("/tmp/ledger.jsonl")
	now := time.Now()
	events := []TrustEvent{
		makeAuditMergeEvent("a1", now.AddDate(0, 0, -10), 0.5, 0.7),
		makeAuditTierEvent("a1", now.AddDate(0, 0, -5), "provisional", "trusted"),
	}
	a := r.checkTierMismatch(events, TierTrusted)
	if a != nil {
		t.Errorf("expected nil for matching tier, got %s", a.Detail)
	}
}

func TestCheckTierMismatch_Mismatch(t *testing.T) {
	r := NewTrustAuditRunner("/tmp/ledger.jsonl")
	now := time.Now()
	events := []TrustEvent{
		makeAuditTierEvent("a1", now, "provisional", "trusted"),
	}
	a := r.checkTierMismatch(events, TierProvisional)
	if a == nil {
		t.Fatal("expected mismatch anomaly")
	}
	if a.Type != AnomalyTierMismatch {
		t.Errorf("expected tier_mismatch, got %s", a.Type)
	}
}

func TestCheckBackwardsScore_NoDrop(t *testing.T) {
	r := NewTrustAuditRunner("/tmp/ledger.jsonl")
	now := time.Now()
	events := []TrustEvent{
		makeAuditMergeEvent("a1", now.AddDate(0, 0, -5), 0.5, 0.6),
		makeAuditMergeEvent("a1", now.AddDate(0, 0, -1), 0.6, 0.7),
	}
	a := r.checkBackwardsScore("a1", events)
	if a != nil {
		t.Error("expected nil for upward score")
	}
}

func TestCheckBackwardsScore_JustifiedDrop(t *testing.T) {
	r := NewTrustAuditRunner("/tmp/ledger.jsonl")
	now := time.Now()
	events := []TrustEvent{
		makeAuditMergeEvent("a1", now.AddDate(0, 0, -5), 0.5, 0.7),
		makeAuditIncidentEvent("a1", now.AddDate(0, 0, -1), 0.7, 0.5),
	}
	a := r.checkBackwardsScore("a1", events)
	if a != nil {
		t.Error("expected nil for justified drop (incident)")
	}
}

func TestCheckBackwardsScore_UnjustifiedDrop(t *testing.T) {
	r := NewTrustAuditRunner("/tmp/ledger.jsonl")
	now := time.Now()
	events := []TrustEvent{
		makeAuditMergeEvent("a1", now.AddDate(0, 0, -5), 0.5, 0.8),
		makeAuditMergeEvent("a1", now.AddDate(0, 0, -1), 0.8, 0.4), // drop without incident
	}
	a := r.checkBackwardsScore("a1", events)
	if a == nil {
		t.Fatal("expected backwards_score anomaly")
	}
	if a.Type != AnomalyBackwardsScore {
		t.Errorf("expected backwards_score, got %s", a.Type)
	}
}

func TestCheckBackwardsScore_SingleEvent(t *testing.T) {
	r := NewTrustAuditRunner("/tmp/ledger.jsonl")
	events := []TrustEvent{
		makeAuditMergeEvent("a1", time.Now(), 0.5, 0.6),
	}
	a := r.checkBackwardsScore("a1", events)
	if a != nil {
		t.Error("expected nil for single event")
	}
}

func TestCheckNoActivity_Recent(t *testing.T) {
	r := NewTrustAuditRunner("/tmp/ledger.jsonl")
	events := []TrustEvent{
		makeAuditMergeEvent("a1", time.Now().AddDate(0, 0, -5), 0.5, 0.6),
	}
	a := r.checkNoActivity("a1", events)
	if a != nil {
		t.Error("expected nil for recent activity")
	}
}

func TestCheckNoActivity_Stale(t *testing.T) {
	r := NewTrustAuditRunner("/tmp/ledger.jsonl", WithMaxInactivityDays(30))
	events := []TrustEvent{
		makeAuditMergeEvent("a1", time.Now().AddDate(0, 0, -100), 0.5, 0.6),
	}
	a := r.checkNoActivity("a1", events)
	if a == nil {
		t.Fatal("expected no_activity anomaly")
	}
	if a.Type != AnomalyNoActivity {
		t.Errorf("expected no_activity, got %s", a.Type)
	}
}

func TestCheckNoActivity_EmptyEvents(t *testing.T) {
	r := NewTrustAuditRunner("/tmp/ledger.jsonl")
	a := r.checkNoActivity("a1", nil)
	if a != nil {
		t.Error("expected nil for empty events")
	}
}

// ============================================================================
// Tests — countCorruptedLines
// ============================================================================

func TestCountCorruptedLines_Clean(t *testing.T) {
	events := []TrustEvent{
		makeAuditMergeEvent("a1", time.Now(), 0.5, 0.6),
	}
	path := writeAuditLedger(t, events)
	r := NewTrustAuditRunner(path)
	count, err := r.countCorruptedLines()
	if err != nil {
		t.Fatalf("countCorruptedLines failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 corrupted, got %d", count)
	}
}

func TestCountCorruptedLines_WithCorruption(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupted.jsonl")
	// Write one valid and one corrupted line
	content := fmt.Sprintf(`{"agent_id":"a1","event_type":"merge_success","timestamp":"%s","data":{"trust_score_before":0.5,"trust_score_after":0.6}}`+"\n", time.Now().Format(time.RFC3339))
	content += `{INVALID JSON LINE}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	r := NewTrustAuditRunner(path)
	count, err := r.countCorruptedLines()
	if err != nil {
		t.Fatalf("countCorruptedLines failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 corrupted line, got %d", count)
	}
}

func TestCountCorruptedLines_NonExistent(t *testing.T) {
	r := NewTrustAuditRunner("/nonexistent/path.jsonl")
	count, err := r.countCorruptedLines()
	if err != nil {
		t.Fatalf("should not error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 for non-existent file, got %d", count)
	}
}

// ============================================================================
// Tests — groupEventsByAgent
// ============================================================================

func TestGroupEventsByAgent(t *testing.T) {
	events := []TrustEvent{
		makeAuditMergeEvent("a1", time.Now(), 0.5, 0.6),
		makeAuditMergeEvent("a2", time.Now(), 0.5, 0.7),
		makeAuditMergeEvent("a1", time.Now(), 0.6, 0.8),
	}
	grouped := groupEventsByAgent(events)
	if len(grouped) != 2 {
		t.Errorf("expected 2 agents, got %d", len(grouped))
	}
	if len(grouped["a1"]) != 2 {
		t.Errorf("expected 2 events for a1, got %d", len(grouped["a1"]))
	}
	if len(grouped["a2"]) != 1 {
		t.Errorf("expected 1 event for a2, got %d", len(grouped["a2"]))
	}
}

func TestGroupEventsByAgent_Empty(t *testing.T) {
	grouped := groupEventsByAgent(nil)
	if len(grouped) != 0 {
		t.Errorf("expected 0 agents, got %d", len(grouped))
	}
}

// ============================================================================
// Tests — auditAgent (integration)
// ============================================================================

func TestAuditAgent_CleanAgent(t *testing.T) {
	now := time.Now().UTC()
	events := []TrustEvent{
		makeAuditMergeEvent("agent-x", now.AddDate(0, 0, -10), 0.5, 0.6),
		makeAuditMergeEvent("agent-x", now.AddDate(0, 0, -5), 0.6, 0.7),
	}
	path := writeAuditLedger(t, events)
	r := NewTrustAuditRunner(path)
	finding := r.auditAgent("agent-x", events, events)
	// Score should match → PASS or ANOMALY (depending on decay)
	if finding.Status == AuditFail {
		t.Errorf("clean agent should not fail, got %s", finding.Status)
	}
	if finding.ComputedScore <= 0 {
		t.Error("computed score should be > 0")
	}
}

func TestAuditAgent_WithTierEvent(t *testing.T) {
	now := time.Now().UTC()
	events := []TrustEvent{
		makeAuditMergeEvent("agent-x", now.AddDate(0, 0, -90), 0.5, 0.7),
		makeAuditTierEvent("agent-x", now.AddDate(0, 0, -80), "provisional", "observed"),
		makeAuditMergeEvent("agent-x", now.AddDate(0, 0, -5), 0.7, 0.72),
	}
	path := writeAuditLedger(t, events)
	r := NewTrustAuditRunner(path)
	finding := r.auditAgent("agent-x", events, events)
	if finding.ComputedTier == "" {
		t.Error("expected non-empty tier")
	}
}

func TestRun_FullAuditWithMultipleAgentsAndStates(t *testing.T) {
	now := time.Now().UTC()
	events := []TrustEvent{
		// Agent A: clean, recent
		makeAuditMergeEvent("agent-a", now.AddDate(0, 0, -3), 0.5, 0.65),
		// Agent B: stale (100 days ago)
		makeAuditMergeEvent("agent-b", now.AddDate(0, 0, -100), 0.5, 0.6),
		// Agent C: has tier change
		makeAuditMergeEvent("agent-c", now.AddDate(0, 0, -30), 0.5, 0.7),
		makeAuditTierEvent("agent-c", now.AddDate(0, 0, -25), "provisional", "observed"),
	}
	path := writeAuditLedger(t, events)
	r := NewTrustAuditRunner(path)
	report, err := r.Run()
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if report.AgentCount != 3 {
		t.Errorf("expected 3 agents, got %d", report.AgentCount)
	}
	if report.Summary.Total != 3 {
		t.Errorf("expected 3 total findings, got %d", report.Summary.Total)
	}
	// Agent B should have an inactivity anomaly
	findingB := report.FindingByAgent("agent-b")
	if findingB == nil {
		t.Fatal("expected finding for agent-b")
	}
	if findingB.Status == AuditPass {
		// Agent B is 100 days stale → should be at least ANOMALY
		t.Errorf("expected agent-b to be anomaly (stale), got %s", findingB.Status)
	}
}

// ============================================================================
// Tests — Anomaly types
// ============================================================================

func TestAnomalyType_Constants(t *testing.T) {
	types := []AnomalyType{
		AnomalyScoreDrift, AnomalyMissingEvents, AnomalyCorruptedEntry,
		AnomalyTierMismatch, AnomalyBackwardsScore, AnomalyNoActivity,
	}
	for _, at := range types {
		if string(at) == "" {
			t.Errorf("anomaly type %v should not be empty", at)
		}
	}
}
