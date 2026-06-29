package trust

import (
	"testing"
	"time"
)

func makePenaltyEvent(agentID string, ts time.Time, scoreBefore, scoreAfter, attrWt float64) TrustEvent {
	return TrustEvent{
		AgentID:   agentID,
		EventType: EventIncidentPenalty,
		Timestamp: ts,
		Data: EventData{
			ScoreBefore:   scoreBefore,
			ScoreAfter:    scoreAfter,
			AttributionWt: attrWt,
		},
	}
}

func makeDemotionEvent(agentID string, ts time.Time, oldTier, newTier string, scoreAfter float64) TrustEvent {
	return TrustEvent{
		AgentID:   agentID,
		EventType: EventDemotion,
		Timestamp: ts,
		Data: EventData{
			OldTier:     oldTier,
			NewTier:     newTier,
			ScoreAfter:  scoreAfter,
			ScoreBefore: scoreAfter + 0.1,
		},
	}
}

// =============================================================================
// GetRecoverySnapshot
// =============================================================================

func TestRecovery_NoEvents_HealthyNotRecovering(t *testing.T) {
	path := writeTestLedger(t, nil)
	snap, err := GetRecoverySnapshot(path, "agent-1")
	if err != nil {
		t.Fatalf("GetRecoverySnapshot: %v", err)
	}
	if snap.IsRecovering {
		t.Error("agent with no events should not be recovering")
	}
	if snap.RecoveryProgress != 100.0 {
		t.Errorf("expected progress=100, got %.1f", snap.RecoveryProgress)
	}
	if snap.RecoveryHealthLabel() != "healthy" {
		t.Errorf("expected healthy, got %s", snap.RecoveryHealthLabel())
	}
}

func TestRecovery_NoIncidents_HealthyNotRecovering(t *testing.T) {
	base := time.Now().Add(-60 * 24 * time.Hour)
	events := []TrustEvent{
		makeMergeEvent("a1", base, 0.5, 0.55),
		makeMergeEvent("a1", base.Add(10*24*time.Hour), 0.55, 0.60),
		makeMergeEvent("a1", base.Add(20*24*time.Hour), 0.60, 0.65),
	}
	path := writeTestLedger(t, events)

	snap, err := GetRecoverySnapshot(path, "a1")
	if err != nil {
		t.Fatalf("GetRecoverySnapshot: %v", err)
	}
	if snap.IsRecovering {
		t.Error("agent with no incidents should not be recovering")
	}
	if snap.RecoveryProgress != 100.0 {
		t.Errorf("expected progress=100, got %.1f", snap.RecoveryProgress)
	}
	if snap.IncidentCount != 0 {
		t.Errorf("expected 0 incidents, got %d", snap.IncidentCount)
	}
	if snap.RecoveryHealthLabel() != "healthy" {
		t.Errorf("expected healthy, got %s", snap.RecoveryHealthLabel())
	}
}

func TestRecovery_WithIncident_Recovering(t *testing.T) {
	base := time.Now().Add(-60 * 24 * time.Hour)
	incidentTime := time.Now().Add(-15 * 24 * time.Hour)
	events := []TrustEvent{
		makeMergeEvent("a1", base, 0.5, 0.60),
		makeMergeEvent("a1", base.Add(10*24*time.Hour), 0.60, 0.70),
		makeIncidentEvent("a1", incidentTime, 0.70, 0.55, 1.0),
		makeMergeEvent("a1", time.Now().Add(-7*24*time.Hour), 0.55, 0.58),
		makeMergeEvent("a1", time.Now().Add(-3*24*time.Hour), 0.58, 0.61),
	}
	path := writeTestLedger(t, events)

	snap, err := GetRecoverySnapshot(path, "a1")
	if err != nil {
		t.Fatalf("GetRecoverySnapshot: %v", err)
	}
	if !snap.IsRecovering {
		t.Error("expected agent to be recovering after incident")
	}
	if snap.PreIncidentScore != 0.70 {
		t.Errorf("expected pre-incident score=0.70, got %v", snap.PreIncidentScore)
	}
	if snap.ConsecutiveCleanMerges != 2 {
		t.Errorf("expected 2 clean merges post-incident, got %d", snap.ConsecutiveCleanMerges)
	}
	if snap.IncidentCount != 1 {
		t.Errorf("expected 1 incident, got %d", snap.IncidentCount)
	}
	if snap.DaysSinceLastIncident < 10 || snap.DaysSinceLastIncident > 20 {
		t.Errorf("expected ~15 days since incident, got %d", snap.DaysSinceLastIncident)
	}
}

func TestRecovery_WithPenaltyEvent_Detected(t *testing.T) {
	base := time.Now().Add(-90 * 24 * time.Hour)
	events := []TrustEvent{
		makeMergeEvent("a1", base, 0.5, 0.80),
		makePenaltyEvent("a1", base.Add(30*24*time.Hour), 0.80, 0.50, 1.0),
		makeMergeEvent("a1", time.Now().Add(-5*24*time.Hour), 0.50, 0.55),
	}
	path := writeTestLedger(t, events)

	snap, err := GetRecoverySnapshot(path, "a1")
	if err != nil {
		t.Fatalf("GetRecoverySnapshot: %v", err)
	}
	if !snap.IsRecovering {
		t.Error("expected recovering after penalty event")
	}
	if snap.PreIncidentScore != 0.80 {
		t.Errorf("expected pre-incident score=0.80, got %v", snap.PreIncidentScore)
	}
	if snap.IncidentCount < 1 {
		t.Errorf("expected at least 1 incident, got %d", snap.IncidentCount)
	}
}

func TestRecovery_WithDemotion_Detected(t *testing.T) {
	base := time.Now().Add(-120 * 24 * time.Hour)
	events := []TrustEvent{
		makeMergeEvent("a1", base, 0.5, 0.85),
		makeDemotionEvent("a1", base.Add(60*24*time.Hour), "veteran", "trusted", 0.60),
		makeMergeEvent("a1", time.Now().Add(-10*24*time.Hour), 0.60, 0.65),
	}
	path := writeTestLedger(t, events)

	snap, err := GetRecoverySnapshot(path, "a1")
	if err != nil {
		t.Fatalf("GetRecoverySnapshot: %v", err)
	}
	if !snap.IsRecovering {
		t.Error("expected recovering after demotion")
	}
	if snap.PreIncidentScore != 0.70 {
		// ScoreBefore in the demotion event is 0.60+0.1=0.70
		t.Errorf("expected pre-incident score=0.70, got %v", snap.PreIncidentScore)
	}
}

func TestRecovery_FullyRecovered_ScoreBackAbove(t *testing.T) {
	base := time.Now().Add(-60 * 24 * time.Hour)
	events := []TrustEvent{
		makeMergeEvent("a1", base, 0.5, 0.65),
		makeIncidentEvent("a1", base.Add(10*24*time.Hour), 0.65, 0.40, 1.0),
		makeMergeEvent("a1", base.Add(20*24*time.Hour), 0.40, 0.55),
		makeMergeEvent("a1", base.Add(30*24*time.Hour), 0.55, 0.65),
		makeMergeEvent("a1", base.Add(40*24*time.Hour), 0.65, 0.70),
	}
	path := writeTestLedger(t, events)

	snap, err := GetRecoverySnapshot(path, "a1")
	if err != nil {
		t.Fatalf("GetRecoverySnapshot: %v", err)
	}
	if snap.IsRecovering {
		t.Error("expected agent to be fully recovered (score >= pre-incident)")
	}
	if snap.RecoveryProgress != 100.0 {
		t.Errorf("expected progress=100, got %.1f", snap.RecoveryProgress)
	}
	if !snap.IsFullyRecovered() {
		t.Error("IsFullyRecovered() should be true")
	}
}

func TestRecovery_ProgressBetween0And100(t *testing.T) {
	base := time.Now().Add(-30 * 24 * time.Hour)
	events := []TrustEvent{
		makeMergeEvent("a1", base, 0.5, 0.75),
		makeIncidentEvent("a1", time.Now().Add(-10*24*time.Hour), 0.75, 0.45, 1.0),
		makeMergeEvent("a1", time.Now().Add(-5*24*time.Hour), 0.45, 0.50),
	}
	path := writeTestLedger(t, events)

	snap, err := GetRecoverySnapshot(path, "a1")
	if err != nil {
		t.Fatalf("GetRecoverySnapshot: %v", err)
	}
	if snap.RecoveryProgress < 0 || snap.RecoveryProgress > 100 {
		t.Errorf("progress should be 0-100, got %.1f", snap.RecoveryProgress)
	}
	if snap.RecoveryProgress == 100 {
		t.Error("agent still recovering should not be at 100%")
	}
	if snap.RecoveryProgress == 0 {
		t.Error("agent with some recovery should not be at 0%")
	}
}

// =============================================================================
// Multi-agent
// =============================================================================

func TestRecovery_MultipleAgents_Isolated(t *testing.T) {
	base := time.Now().Add(-60 * 24 * time.Hour)
	events := []TrustEvent{
		// a1 has an incident
		makeMergeEvent("a1", base, 0.5, 0.70),
		makeIncidentEvent("a1", base.Add(30*24*time.Hour), 0.70, 0.50, 1.0),
		makeMergeEvent("a1", base.Add(35*24*time.Hour), 0.50, 0.55),
		// a2 is clean
		makeMergeEvent("a2", base, 0.5, 0.60),
		makeMergeEvent("a2", base.Add(30*24*time.Hour), 0.60, 0.70),
	}
	path := writeTestLedger(t, events)

	snap1, err := GetRecoverySnapshot(path, "a1")
	if err != nil {
		t.Fatalf("a1: %v", err)
	}
	snap2, err := GetRecoverySnapshot(path, "a2")
	if err != nil {
		t.Fatalf("a2: %v", err)
	}
	if !snap1.IsRecovering {
		t.Error("a1 should be recovering")
	}
	if snap2.IsRecovering {
		t.Error("a2 should NOT be recovering (no incidents)")
	}
}

func TestRecovery_BatchAllAgents(t *testing.T) {
	base := time.Now().Add(-60 * 24 * time.Hour)
	events := []TrustEvent{
		makeMergeEvent("a1", base, 0.5, 0.70),
		makeIncidentEvent("a1", base.Add(30*24*time.Hour), 0.70, 0.50, 1.0),
		makeMergeEvent("a1", base.Add(40*24*time.Hour), 0.50, 0.55),
		makeMergeEvent("a2", base, 0.5, 0.60),
	}
	path := writeTestLedger(t, events)

	results, err := GetRecoveryBatch(path, []string{"a1", "a2"})
	if err != nil {
		t.Fatalf("GetRecoveryBatch: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if !results["a1"].IsRecovering {
		t.Error("a1 should be recovering")
	}
	if results["a2"].IsRecovering {
		t.Error("a2 should not be recovering")
	}
}

// =============================================================================
// Config variants
// =============================================================================

func TestRecovery_CustomConfig_AffectsProgress(t *testing.T) {
	base := time.Now().Add(-20 * 24 * time.Hour)
	events := []TrustEvent{
		makeMergeEvent("a1", base, 0.5, 0.80),
		makeIncidentEvent("a1", time.Now().Add(-10*24*time.Hour), 0.80, 0.50, 1.0),
		makeMergeEvent("a1", time.Now().Add(-5*24*time.Hour), 0.50, 0.55),
	}
	path := writeTestLedger(t, events)

	cfgLow := RecoveryConfig{
		CleanMergeRecoveryRate:      0.5, // very slow recovery
		DaysDecayRate:               0.1,
		TrendImprovementThreshold:   0.01,
		MinCleanMergesForRecovering: 1,
	}
	cfgHigh := RecoveryConfig{
		CleanMergeRecoveryRate:      10.0, // very fast recovery
		DaysDecayRate:               5.0,
		TrendImprovementThreshold:   0.01,
		MinCleanMergesForRecovering: 1,
	}

	snapLow, err := GetRecoverySnapshotWithConfig(path, "a1", cfgLow)
	if err != nil {
		t.Fatalf("low cfg: %v", err)
	}
	snapHigh, err := GetRecoverySnapshotWithConfig(path, "a1", cfgHigh)
	if err != nil {
		t.Fatalf("high cfg: %v", err)
	}
	if snapHigh.RecoveryProgress <= snapLow.RecoveryProgress {
		t.Errorf("high cfg should have higher progress: high=%.1f low=%.1f",
			snapHigh.RecoveryProgress, snapLow.RecoveryProgress)
	}
}

// =============================================================================
// Helpers: findLastIncidentOrDemotion
// =============================================================================

func TestFindLastIncidentOrDemotion_None(t *testing.T) {
	events := []TrustEvent{
		makeMergeEvent("a1", time.Now(), 0.5, 0.6),
		makeReviewEvent("a1", time.Now(), 0.6, 0.65),
	}
	if idx := findLastIncidentOrDemotion(events); idx != -1 {
		t.Errorf("expected -1 for no incidents, got %d", idx)
	}
}

func TestFindLastIncidentOrDemotion_IncidentAttrib(t *testing.T) {
	events := []TrustEvent{
		makeMergeEvent("a1", time.Now().Add(-20*24*time.Hour), 0.5, 0.6),
		makeIncidentEvent("a1", time.Now().Add(-10*24*time.Hour), 0.6, 0.4, 1.0),
		makeMergeEvent("a1", time.Now().Add(-5*24*time.Hour), 0.4, 0.45),
	}
	if idx := findLastIncidentOrDemotion(events); idx != 1 {
		t.Errorf("expected index 1, got %d", idx)
	}
}

func TestFindLastIncidentOrDemotion_Penalty(t *testing.T) {
	events := []TrustEvent{
		makeMergeEvent("a1", time.Now().Add(-20*24*time.Hour), 0.5, 0.8),
		makePenaltyEvent("a1", time.Now().Add(-10*24*time.Hour), 0.8, 0.5, 1.0),
	}
	if idx := findLastIncidentOrDemotion(events); idx != 1 {
		t.Errorf("expected index 1 for penalty, got %d", idx)
	}
}

func TestFindLastIncidentOrDemotion_Demotion(t *testing.T) {
	events := []TrustEvent{
		makeMergeEvent("a1", time.Now().Add(-20*24*time.Hour), 0.5, 0.85),
		makeDemotionEvent("a1", time.Now().Add(-10*24*time.Hour), "veteran", "trusted", 0.60),
	}
	if idx := findLastIncidentOrDemotion(events); idx != 1 {
		t.Errorf("expected index 1 for demotion, got %d", idx)
	}
}

func TestFindLastIncidentOrDemotion_MultiplePicksLast(t *testing.T) {
	events := []TrustEvent{
		makeIncidentEvent("a1", time.Now().Add(-30*24*time.Hour), 0.7, 0.5, 1.0),
		makeMergeEvent("a1", time.Now().Add(-20*24*time.Hour), 0.5, 0.6),
		makeIncidentEvent("a1", time.Now().Add(-10*24*time.Hour), 0.6, 0.4, 1.0),
		makeMergeEvent("a1", time.Now().Add(-5*24*time.Hour), 0.4, 0.45),
	}
	if idx := findLastIncidentOrDemotion(events); idx != 2 {
		t.Errorf("expected index 2 (last incident), got %d", idx)
	}
}

func TestFindLastIncidentOrDemotion_EmptySlice(t *testing.T) {
	if idx := findLastIncidentOrDemotion(nil); idx != -1 {
		t.Errorf("expected -1 for nil, got %d", idx)
	}
}

// =============================================================================
// countIncidents
// =============================================================================

func TestCountIncidents_None(t *testing.T) {
	events := []TrustEvent{
		makeMergeEvent("a1", time.Now(), 0.5, 0.6),
	}
	if c := countIncidents(events); c != 0 {
		t.Errorf("expected 0, got %d", c)
	}
}

func TestCountIncidents_MixedTypes(t *testing.T) {
	events := []TrustEvent{
		makeIncidentEvent("a1", time.Now(), 0.7, 0.5, 1.0),
		makePenaltyEvent("a1", time.Now(), 0.5, 0.4, 0.5),
		makeMergeEvent("a1", time.Now(), 0.4, 0.45),
		makeIncidentEvent("a1", time.Now(), 0.45, 0.3, 1.0),
	}
	if c := countIncidents(events); c != 3 {
		t.Errorf("expected 3 (2 attrib + 1 penalty), got %d", c)
	}
}

// =============================================================================
// Health labels
// =============================================================================

func TestHealthLabel_Healthy(t *testing.T) {
	snap := &RecoverySnapshot{IncidentCount: 0}
	if label := snap.RecoveryHealthLabel(); label != "healthy" {
		t.Errorf("expected healthy, got %s", label)
	}
}

func TestHealthLabel_Recovered(t *testing.T) {
	snap := &RecoverySnapshot{
		IncidentCount:    1,
		RecoveryProgress: 100.0,
		IsRecovering:     false,
	}
	if label := snap.RecoveryHealthLabel(); label != "recovered" {
		t.Errorf("expected recovered, got %s", label)
	}
}

func TestHealthLabel_RecoveringStrong(t *testing.T) {
	snap := &RecoverySnapshot{
		IncidentCount:    1,
		RecoveryProgress: 80.0,
		IsRecovering:     true,
	}
	if label := snap.RecoveryHealthLabel(); label != "recovering-strong" {
		t.Errorf("expected recovering-strong, got %s", label)
	}
}

func TestHealthLabel_Recovering(t *testing.T) {
	snap := &RecoverySnapshot{
		IncidentCount:    1,
		RecoveryProgress: 60.0,
		IsRecovering:     true,
	}
	if label := snap.RecoveryHealthLabel(); label != "recovering" {
		t.Errorf("expected recovering, got %s", label)
	}
}

func TestHealthLabel_RecoveringSlow(t *testing.T) {
	snap := &RecoverySnapshot{
		IncidentCount:    1,
		RecoveryProgress: 35.0,
		IsRecovering:     true,
	}
	if label := snap.RecoveryHealthLabel(); label != "recovering-slow" {
		t.Errorf("expected recovering-slow, got %s", label)
	}
}

func TestHealthLabel_RecoveringEarly(t *testing.T) {
	snap := &RecoverySnapshot{
		IncidentCount:    1,
		RecoveryProgress: 10.0,
		IsRecovering:     true,
	}
	if label := snap.RecoveryHealthLabel(); label != "recovering-early" {
		t.Errorf("expected recovering-early, got %s", label)
	}
}

func TestHealthLabel_AtRisk(t *testing.T) {
	snap := &RecoverySnapshot{
		IncidentCount:    1,
		RecoveryProgress: 5.0,
		IsRecovering:     false, // not on upward trend
	}
	if label := snap.RecoveryHealthLabel(); label != "at-risk" {
		t.Errorf("expected at-risk, got %s", label)
	}
}

// =============================================================================
// IsFullyRecovered
// =============================================================================

func TestIsFullyRecovered_True(t *testing.T) {
	snap := &RecoverySnapshot{
		RecoveryProgress: 100.0,
		IsRecovering:     false,
	}
	if !snap.IsFullyRecovered() {
		t.Error("expected fully recovered")
	}
}

func TestIsFullyRecovered_FalseStillRecovering(t *testing.T) {
	snap := &RecoverySnapshot{
		RecoveryProgress: 95.0,
		IsRecovering:     true,
	}
	if snap.IsFullyRecovered() {
		t.Error("still-recovering agent should not be fully recovered")
	}
}

func TestIsFullyRecovered_FalseLowProgress(t *testing.T) {
	snap := &RecoverySnapshot{
		RecoveryProgress: 50.0,
		IsRecovering:     false,
	}
	if snap.IsFullyRecovered() {
		t.Error("50% progress should not be fully recovered")
	}
}

// =============================================================================
// EstimatedDaysToRecover
// =============================================================================

func TestEstimatedDaysToRecover_PositiveWhenRecovering(t *testing.T) {
	base := time.Now().Add(-10 * 24 * time.Hour)
	events := []TrustEvent{
		makeMergeEvent("a1", base, 0.5, 0.80),
		makeIncidentEvent("a1", time.Now().Add(-5*24*time.Hour), 0.80, 0.45, 1.0),
		makeMergeEvent("a1", time.Now().Add(-2*24*time.Hour), 0.45, 0.50),
	}
	path := writeTestLedger(t, events)

	snap, err := GetRecoverySnapshot(path, "a1")
	if err != nil {
		t.Fatalf("GetRecoverySnapshot: %v", err)
	}
	if snap.IsRecovering && snap.EstimatedDaysToRecover <= 0 {
		t.Error("recovering agent should have positive estimated days")
	}
}

func TestEstimatedDaysToRecover_ZeroWhenFullyRecovered(t *testing.T) {
	base := time.Now().Add(-60 * 24 * time.Hour)
	events := []TrustEvent{
		makeMergeEvent("a1", base, 0.5, 0.65),
		makeIncidentEvent("a1", base.Add(10*24*time.Hour), 0.65, 0.40, 1.0),
		makeMergeEvent("a1", base.Add(30*24*time.Hour), 0.40, 0.65),
		makeMergeEvent("a1", base.Add(40*24*time.Hour), 0.65, 0.70),
	}
	path := writeTestLedger(t, events)

	snap, err := GetRecoverySnapshot(path, "a1")
	if err != nil {
		t.Fatalf("GetRecoverySnapshot: %v", err)
	}
	if snap.EstimatedDaysToRecover != 0 {
		t.Errorf("fully recovered agent should have 0 days, got %d", snap.EstimatedDaysToRecover)
	}
}

// =============================================================================
// PreIncidentScore fallback
// =============================================================================

func TestPreIncidentScore_FallbackWhenZero(t *testing.T) {
	// Create an incident event with ScoreBefore=0 (edge case)
	incidentEvent := TrustEvent{
		AgentID:   "a1",
		EventType: EventIncidentAttrib,
		Timestamp: time.Now().Add(-10 * 24 * time.Hour),
		Data: EventData{
			ScoreBefore: 0, // zero → should fall back to 0.5
			ScoreAfter:  0.2,
		},
	}
	events := []TrustEvent{
		makeMergeEvent("a1", time.Now().Add(-30*24*time.Hour), 0.5, 0.6),
		incidentEvent,
		makeMergeEvent("a1", time.Now().Add(-5*24*time.Hour), 0.2, 0.25),
	}
	path := writeTestLedger(t, events)

	snap, err := GetRecoverySnapshot(path, "a1")
	if err != nil {
		t.Fatalf("GetRecoverySnapshot: %v", err)
	}
	if snap.PreIncidentScore != 0.5 {
		t.Errorf("expected fallback pre-incident score=0.5, got %v", snap.PreIncidentScore)
	}
}

// =============================================================================
// DefaultRecoveryConfig
// =============================================================================

func TestDefaultRecoveryConfig_Values(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	if cfg.CleanMergeRecoveryRate <= 0 {
		t.Error("CleanMergeRecoveryRate should be positive")
	}
	if cfg.DaysDecayRate <= 0 {
		t.Error("DaysDecayRate should be positive")
	}
	if cfg.MinCleanMergesForRecovering < 1 {
		t.Error("MinCleanMergesForRecovering should be >= 1")
	}
}
