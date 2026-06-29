package trust

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTestLedger(t *testing.T, events []TrustEvent) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer f.Close()
	for _, evt := range events {
		data, err := json.Marshal(evt)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		_, _ = f.Write(append(data, '\n'))
	}
	return path
}

func makeMergeEvent(agentID string, ts time.Time, scoreBefore, scoreAfter float64) TrustEvent {
	return TrustEvent{
		AgentID:   agentID,
		EventType: EventMergeSuccess,
		Timestamp: ts,
		Data: EventData{
			ScoreBefore: scoreBefore,
			ScoreAfter:  scoreAfter,
			Evidence:    []string{"merge-commit-sha"},
		},
	}
}

func makeIncidentEvent(agentID string, ts time.Time, scoreBefore, scoreAfter, attrWt float64) TrustEvent {
	return TrustEvent{
		AgentID:   agentID,
		EventType: EventIncidentAttrib,
		Timestamp: ts,
		Data: EventData{
			ScoreBefore:   scoreBefore,
			ScoreAfter:    scoreAfter,
			AttributionWt: attrWt,
			Evidence:      []string{"incident-report"},
		},
	}
}

func makeReviewEvent(agentID string, ts time.Time, scoreBefore, scoreAfter float64) TrustEvent {
	return TrustEvent{
		AgentID:   agentID,
		EventType: EventReviewConsensus,
		Timestamp: ts,
		Data: EventData{
			ScoreBefore: scoreBefore,
			ScoreAfter:  scoreAfter,
		},
	}
}

func makeRatingEvent(agentID string, ts time.Time, scoreBefore, scoreAfter float64) TrustEvent {
	return TrustEvent{
		AgentID:   agentID,
		EventType: EventHumanRating,
		Timestamp: ts,
		Data: EventData{
			ScoreBefore: scoreBefore,
			ScoreAfter:  scoreAfter,
		},
	}
}

func makeTierChangeEvent(agentID string, ts time.Time, oldTier, newTier string, scoreAfter float64) TrustEvent {
	return TrustEvent{
		AgentID:   agentID,
		EventType: EventTierChange,
		Timestamp: ts,
		Data: EventData{
			OldTier:    oldTier,
			NewTier:    newTier,
			ScoreAfter: scoreAfter,
		},
	}
}

// ─── GetSnapshot ───

func TestGetSnapshot_Empty(t *testing.T) {
	path := writeTestLedger(t, nil)
	snap, err := GetSnapshot(path, "agent-001")
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if snap.AgentID != "agent-001" {
		t.Fatalf("expected agent-001, got %s", snap.AgentID)
	}
	if snap.Score != TrustScore(0.5) {
		t.Fatalf("expected 0.5 neutral score, got %f", snap.Score)
	}
	if snap.Tier != TierProvisional {
		t.Fatalf("expected provisional tier, got %s", snap.Tier)
	}
	if snap.TotalEvents != 0 {
		t.Fatalf("expected 0 events, got %d", snap.TotalEvents)
	}
}

func TestGetSnapshot_WithEvents(t *testing.T) {
	now := time.Now().UTC()
	events := []TrustEvent{
		makeMergeEvent("agent-001", now.AddDate(0, 0, -20), 0.5, 0.55),
		makeMergeEvent("agent-001", now.AddDate(0, 0, -15), 0.55, 0.60),
		makeMergeEvent("agent-001", now.AddDate(0, 0, -10), 0.60, 0.65),
	}
	path := writeTestLedger(t, events)

	snap, err := GetSnapshot(path, "agent-001")
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if snap.Score != TrustScore(0.65) {
		t.Fatalf("expected 0.65, got %f", snap.Score)
	}
	if snap.TotalEvents != 3 {
		t.Fatalf("expected 3 events, got %d", snap.TotalEvents)
	}
}

func TestGetSnapshot_FiltersByAgent(t *testing.T) {
	now := time.Now().UTC()
	events := []TrustEvent{
		makeMergeEvent("agent-001", now.AddDate(0, 0, -10), 0.5, 0.7),
		makeMergeEvent("agent-002", now.AddDate(0, 0, -10), 0.5, 0.9),
	}
	path := writeTestLedger(t, events)

	snap, err := GetSnapshot(path, "agent-001")
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if snap.Score != TrustScore(0.7) {
		t.Fatalf("expected 0.7 for agent-001, got %f", snap.Score)
	}
	if snap.TotalEvents != 1 {
		t.Fatalf("expected 1 event for agent-001, got %d", snap.TotalEvents)
	}
}

func TestGetSnapshot_SnapshotTime(t *testing.T) {
	path := writeTestLedger(t, nil)
	snap, err := GetSnapshot(path, "agent-001")
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if snap.SnapshotTime.IsZero() {
		t.Fatal("SnapshotTime is zero")
	}
}

func TestGetSnapshot_LastActive(t *testing.T) {
	now := time.Now().UTC()
	ts := now.AddDate(0, 0, -10)
	events := []TrustEvent{
		makeMergeEvent("agent-001", ts, 0.5, 0.6),
	}
	path := writeTestLedger(t, events)

	snap, err := GetSnapshot(path, "agent-001")
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if snap.LastActive.IsZero() {
		t.Fatal("LastActive is zero")
	}
}

func TestGetSnapshot_NonexistentLedger(t *testing.T) {
	snap, err := GetSnapshot("/nonexistent/path/trust.jsonl", "agent-001")
	if err != nil {
		t.Fatalf("expected nil error for nonexistent ledger, got %v", err)
	}
	if snap == nil {
		t.Fatal("snapshot is nil")
	}
	if snap.Score != TrustScore(0.5) {
		t.Fatalf("expected 0.5 neutral score for empty ledger, got %f", snap.Score)
	}
}

// ─── ScoreBreakdown ───

func TestGetScoreBreakdown(t *testing.T) {
	now := time.Now().UTC()
	events := []TrustEvent{
		makeMergeEvent("agent-001", now.AddDate(0, 0, -60), 0.5, 0.55),
		makeMergeEvent("agent-001", now.AddDate(0, 0, -50), 0.55, 0.60),
	}
	path := writeTestLedger(t, events)

	breakdown, err := GetScoreBreakdown(path, "agent-001")
	if err != nil {
		t.Fatalf("GetScoreBreakdown: %v", err)
	}

	// Verify weight sums to 1.0
	totalWeight := breakdown.MergeSuccessRate.Weight +
		breakdown.IncidentTrack.Weight +
		breakdown.ReviewConsensus.Weight +
		breakdown.PromptIntegrity.Weight +
		breakdown.HumanFeedback.Weight +
		breakdown.Tenure.Weight
	if totalWeight < 0.99 || totalWeight > 1.01 {
		t.Fatalf("expected total weight ~1.0, got %f", totalWeight)
	}
}

func TestGetScoreBreakdown_MergeSuccess(t *testing.T) {
	now := time.Now().UTC()
	events := []TrustEvent{
		makeMergeEvent("agent-001", now.AddDate(0, 0, -10), 0.5, 0.6),
		makeMergeEvent("agent-001", now.AddDate(0, 0, -5), 0.6, 0.7),
	}
	path := writeTestLedger(t, events)

	breakdown, err := GetScoreBreakdown(path, "agent-001")
	if err != nil {
		t.Fatalf("GetScoreBreakdown: %v", err)
	}

	// 2 merges, 0 incidents → 1.0 merge success rate
	if breakdown.MergeSuccessRate.EstScore != 1.0 {
		t.Fatalf("expected merge success rate 1.0, got %f", breakdown.MergeSuccessRate.EstScore)
	}
}

func TestGetScoreBreakdown_Contribution(t *testing.T) {
	now := time.Now().UTC()
	events := []TrustEvent{
		makeMergeEvent("agent-001", now.AddDate(0, 0, -10), 0.5, 0.6),
	}
	path := writeTestLedger(t, events)

	breakdown, err := GetScoreBreakdown(path, "agent-001")
	if err != nil {
		t.Fatalf("GetScoreBreakdown: %v", err)
	}

	expected := DimensionWeights["merge_success_rate"] * breakdown.MergeSuccessRate.EstScore
	if breakdown.MergeSuccessRate.Contribution != expected {
		t.Fatalf("expected contribution %f, got %f", expected, breakdown.MergeSuccessRate.Contribution)
	}
}

// ─── TierHistory ───

func TestGetTierHistory(t *testing.T) {
	now := time.Now().UTC()
	events := []TrustEvent{
		makeTierChangeEvent("agent-001", now.AddDate(0, 0, -90), "provisional", "observed", 0.45),
		makeTierChangeEvent("agent-001", now.AddDate(0, 0, -30), "observed", "trusted", 0.70),
	}
	path := writeTestLedger(t, events)

	history, err := GetTierHistory(path, "agent-001")
	if err != nil {
		t.Fatalf("GetTierHistory: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 transitions, got %d", len(history))
	}
	if history[0].From != TierProvisional {
		t.Fatalf("expected first from provisional, got %s", history[0].From)
	}
	if history[0].To != TierObserved {
		t.Fatalf("expected first to observed, got %s", history[0].To)
	}
	if !history[0].IsPromote {
		t.Fatal("expected first to be promotion")
	}
	if !history[1].IsPromote {
		t.Fatal("expected second to be promotion")
	}
}

func TestGetTierHistory_Empty(t *testing.T) {
	path := writeTestLedger(t, nil)
	history, err := GetTierHistory(path, "agent-001")
	if err != nil {
		t.Fatalf("GetTierHistory: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("expected 0 transitions, got %d", len(history))
	}
}

func TestGetTierHistory_Demotion(t *testing.T) {
	now := time.Now().UTC()
	events := []TrustEvent{
		makeTierChangeEvent("agent-001", now.AddDate(0, 0, -90), "trusted", "observed", 0.45),
	}
	path := writeTestLedger(t, events)

	history, err := GetTierHistory(path, "agent-001")
	if err != nil {
		t.Fatalf("GetTierHistory: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 transition, got %d", len(history))
	}
	if history[0].IsPromote {
		t.Fatal("expected demotion")
	}
}

// ─── ScoreTrend ───

func TestScoreTrendOver_Up(t *testing.T) {
	now := time.Now().UTC()
	events := []TrustEvent{
		makeMergeEvent("agent-001", now.AddDate(0, 0, -20), 0.5, 0.55),
		makeMergeEvent("agent-001", now.AddDate(0, 0, -15), 0.55, 0.60),
		makeMergeEvent("agent-001", now.AddDate(0, 0, -10), 0.60, 0.70),
	}
	path := writeTestLedger(t, events)

	trend, err := ScoreTrendOver(path, "agent-001", 30*24*time.Hour)
	if err != nil {
		t.Fatalf("ScoreTrendOver: %v", err)
	}
	if trend.Direction != TrendUp {
		t.Fatalf("expected up trend, got %s", trend.Direction)
	}
	if trend.Delta <= 0 {
		t.Fatalf("expected positive delta, got %f", trend.Delta)
	}
	if trend.DataPoints != 3 {
		t.Fatalf("expected 3 data points, got %d", trend.DataPoints)
	}
}

func TestScoreTrendOver_Down(t *testing.T) {
	now := time.Now().UTC()
	events := []TrustEvent{
		makeMergeEvent("agent-001", now.AddDate(0, 0, -20), 0.5, 0.70),
		makeIncidentEvent("agent-001", now.AddDate(0, 0, -10), 0.70, 0.40, 1.0),
	}
	path := writeTestLedger(t, events)

	trend, err := ScoreTrendOver(path, "agent-001", 30*24*time.Hour)
	if err != nil {
		t.Fatalf("ScoreTrendOver: %v", err)
	}
	if trend.Direction != TrendDown {
		t.Fatalf("expected down trend, got %s", trend.Direction)
	}
}

func TestScoreTrendOver_Stable(t *testing.T) {
	now := time.Now().UTC()
	events := []TrustEvent{
		makeMergeEvent("agent-001", now.AddDate(0, 0, -20), 0.505, 0.510),
		makeMergeEvent("agent-001", now.AddDate(0, 0, -10), 0.510, 0.505),
	}
	path := writeTestLedger(t, events)

	trend, err := ScoreTrendOver(path, "agent-001", 30*24*time.Hour)
	if err != nil {
		t.Fatalf("ScoreTrendOver: %v", err)
	}
	if trend.Direction != TrendStable {
		t.Fatalf("expected stable trend, got %s (delta=%f)", trend.Direction, trend.Delta)
	}
}

func TestScoreTrendOver_NoEvents(t *testing.T) {
	path := writeTestLedger(t, nil)
	trend, err := ScoreTrendOver(path, "agent-001", 30*24*time.Hour)
	if err != nil {
		t.Fatalf("ScoreTrendOver: %v", err)
	}
	if trend.Direction != TrendStable {
		t.Fatalf("expected stable trend for no events, got %s", trend.Direction)
	}
	if trend.DataPoints != 0 {
		t.Fatalf("expected 0 data points, got %d", trend.DataPoints)
	}
}

// ─── GetRecentEvents ───

func TestGetRecentEvents(t *testing.T) {
	now := time.Now().UTC()
	events := []TrustEvent{
		makeMergeEvent("agent-001", now.AddDate(0, 0, -100), 0.5, 0.55),
		makeMergeEvent("agent-001", now.AddDate(0, 0, -50), 0.55, 0.60),
		makeMergeEvent("agent-001", now.AddDate(0, 0, -10), 0.60, 0.65),
		makeMergeEvent("agent-001", now.AddDate(0, 0, -5), 0.65, 0.70),
	}
	path := writeTestLedger(t, events)

	recent, err := GetRecentEvents(path, "agent-001", 30)
	if err != nil {
		t.Fatalf("GetRecentEvents: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("expected 2 recent events (within 30 days), got %d", len(recent))
	}
}

func TestGetRecentEvents_None(t *testing.T) {
	now := time.Now().UTC()
	events := []TrustEvent{
		makeMergeEvent("agent-001", now.AddDate(0, 0, -100), 0.5, 0.55),
	}
	path := writeTestLedger(t, events)

	recent, err := GetRecentEvents(path, "agent-001", 30)
	if err != nil {
		t.Fatalf("GetRecentEvents: %v", err)
	}
	if len(recent) != 0 {
		t.Fatalf("expected 0 recent events, got %d", len(recent))
	}
}

// ─── Full Snapshot Integration ───

func TestGetSnapshot_FullIntegration(t *testing.T) {
	now := time.Now().UTC()
	events := []TrustEvent{
		// Agent starts at 0.5, builds trust over 90 days
		makeMergeEvent("agent-001", now.AddDate(0, 0, -90), 0.5, 0.55),
		makeMergeEvent("agent-001", now.AddDate(0, 0, -80), 0.55, 0.60),
		makeReviewEvent("agent-001", now.AddDate(0, 0, -75), 0.60, 0.62),
		makeMergeEvent("agent-001", now.AddDate(0, 0, -70), 0.62, 0.65),
		makeTierChangeEvent("agent-001", now.AddDate(0, 0, -60), "provisional", "observed", 0.65),
		makeMergeEvent("agent-001", now.AddDate(0, 0, -50), 0.65, 0.68),
		makeRatingEvent("agent-001", now.AddDate(0, 0, -40), 0.68, 0.70),
		makeMergeEvent("agent-001", now.AddDate(0, 0, -30), 0.70, 0.72),
		makeTierChangeEvent("agent-001", now.AddDate(0, 0, -20), "observed", "trusted", 0.72),
		makeMergeEvent("agent-001", now.AddDate(0, 0, -10), 0.72, 0.75),
	}
	path := writeTestLedger(t, events)

	snap, err := GetSnapshot(path, "agent-001")
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}

	// Score should be latest
	if snap.Score != TrustScore(0.75) {
		t.Fatalf("expected score 0.75, got %f", snap.Score)
	}

	// Total events: 10
	if snap.TotalEvents != 10 {
		t.Fatalf("expected 10 total events, got %d", snap.TotalEvents)
	}

	// Tier history: 2 transitions
	if len(snap.TierHistory) != 2 {
		t.Fatalf("expected 2 tier transitions, got %d", len(snap.TierHistory))
	}

	// First transition: provisional → observed
	if snap.TierHistory[0].From != TierProvisional || snap.TierHistory[0].To != TierObserved {
		t.Fatalf("unexpected first transition: %s→%s",
			snap.TierHistory[0].From, snap.TierHistory[0].To)
	}

	// Score trend should be up
	if snap.ScoreTrend.Direction != TrendUp {
		t.Fatalf("expected up trend, got %s", snap.ScoreTrend.Direction)
	}

	// Score breakdown should have all dimensions populated
	if snap.ScoreBreakdown.MergeSuccessRate.Weight == 0 {
		t.Fatal("merge_success_rate weight is 0")
	}

	// Recent events within 30 days
	if len(snap.RecentEvents) == 0 {
		t.Fatal("expected recent events within 30 days")
	}
}

func TestGetSnapshot_WithIncident(t *testing.T) {
	now := time.Now().UTC()
	events := []TrustEvent{
		makeMergeEvent("agent-001", now.AddDate(0, 0, -60), 0.5, 0.70),
		makeIncidentEvent("agent-001", now.AddDate(0, 0, -10), 0.70, 0.40, 1.0),
	}
	path := writeTestLedger(t, events)

	snap, err := GetSnapshot(path, "agent-001")
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}

	// Score should reflect the incident penalty
	if snap.Score != TrustScore(0.40) {
		t.Fatalf("expected 0.40 after incident, got %f", snap.Score)
	}

	// Incident track dimension should be very low (recent incident)
	breakdown := snap.ScoreBreakdown
	if breakdown.IncidentTrack.EstScore > 0.2 {
		t.Fatalf("expected low incident track score, got %f", breakdown.IncidentTrack.EstScore)
	}
}
