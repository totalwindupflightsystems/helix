package trust

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// =============================================================================
// Tier tests
// =============================================================================

func TestDetermineTier_ProvisionalByDefault(t *testing.T) {
	tier := DetermineTier(0.0, 0, 0)
	if tier != TierProvisional {
		t.Errorf("expected provisional, got %s", tier)
	}
}

func TestDetermineTier_ObservedMinimum(t *testing.T) {
	tier := DetermineTier(0.40, 100, 0)
	if tier != TierObserved {
		t.Errorf("expected observed, got %s", tier)
	}
}

func TestDetermineTier_ObservedHighScoreFewMerges(t *testing.T) {
	// Score meets observed but merges don't → provisional
	tier := DetermineTier(0.80, 50, 0)
	if tier == TierObserved {
		t.Error("expected NOT observed — only 50 merges")
	}
	if tier != TierProvisional {
		t.Errorf("expected provisional (only 50 merges with 0.80 score), got %s", tier)
	}
}

func TestDetermineTier_TrustedFull(t *testing.T) {
	tier := DetermineTier(0.65, 500, 0)
	if tier != TierTrusted {
		t.Errorf("expected trusted, got %s", tier)
	}
}

func TestDetermineTier_VeteranFull(t *testing.T) {
	tier := DetermineTier(0.85, 2000, 0)
	if tier != TierVeteran {
		t.Errorf("expected veteran, got %s", tier)
	}
}

func TestDetermineTier_VeteranAllowsOneIncident(t *testing.T) {
	tier := DetermineTier(0.85, 2000, 1)
	if tier != TierVeteran {
		t.Errorf("expected veteran (allows 1 incident in 180d), got %s", tier)
	}
}

func TestDetermineTier_VeteranBlockedByTwoIncidents(t *testing.T) {
	tier := DetermineTier(0.85, 2000, 2)
	if tier == TierVeteran {
		t.Error("expected NOT veteran — 2 incidents in 180d")
	}
	// Both Veteran (max 1) and Trusted/Observed (max 0) fail → Provisional
	if tier != TierProvisional {
		t.Errorf("expected provisional (all higher tiers reject 2 incidents), got %s", tier)
	}
}

func TestDetermineTier_ScoreJustBelowThreshold(t *testing.T) {
	tier := DetermineTier(0.39, 100, 0)
	if tier == TierObserved {
		t.Error("expected NOT observed — score 0.39 < 0.40 threshold")
	}
}

func TestDetermineTier_BelowAllThresholdsReturnsProvisional(t *testing.T) {
	tests := []struct {
		score  float64
		merges int
		inc180 int
		want   TrustTier
	}{
		{0.0, 0, 0, TierProvisional},
		{0.1, 0, 0, TierProvisional},
		{0.39, 100, 0, TierProvisional},
		{0.40, 99, 0, TierProvisional},
		{0.64, 500, 0, TierObserved},     // below trusted score (0.65), meets merges → observed
		{0.85, 1999, 0, TierTrusted},     // not enough merges for veteran
		{0.85, 2000, 2, TierProvisional}, // too many incidents for all tiers
	}
	for _, tc := range tests {
		got := DetermineTier(tc.score, tc.merges, tc.inc180)
		if got != tc.want {
			t.Errorf("DetermineTier(%.2f, %d, %d) = %s, want %s", tc.score, tc.merges, tc.inc180, got, tc.want)
		}
	}
}

// =============================================================================
// TrustScore calculation
// =============================================================================

func TestCalculate_AllMax(t *testing.T) {
	dims := DimensionScores{
		MergeSuccessRate: 1.0,
		IncidentTrack:    1.0,
		ReviewConsensus:  1.0,
		PromptIntegrity:  1.0,
		HumanFeedback:    1.0,
		Tenure:           1.0,
	}
	score := Calculate(dims)
	if score != 1.0 {
		t.Errorf("all-max dimensions should yield 1.0, got %f", score)
	}
}

func TestCalculate_AllZero(t *testing.T) {
	dims := DimensionScores{
		MergeSuccessRate: 0.0,
		IncidentTrack:    0.0,
		ReviewConsensus:  0.0,
		PromptIntegrity:  0.0,
		HumanFeedback:    0.0,
		Tenure:           0.0,
	}
	score := Calculate(dims)
	if score != 0.0 {
		t.Errorf("all-zero dimensions should yield 0.0, got %f", score)
	}
}

func TestCalculate_WeightsSumToOne(t *testing.T) {
	var sum float64
	for _, w := range DimensionWeights {
		sum += w
	}
	if math.Abs(sum-1.0) > 1e-10 {
		t.Errorf("weights sum to %f, expected 1.0", sum)
	}
}

func TestCalculate_WeightedCorrectly(t *testing.T) {
	// Set one dimension to 1.0, all others to 0.0
	// The result should be that dimension's weight.
	tests := []struct {
		name string
		dims DimensionScores
	}{
		{"merge_success_rate", DimensionScores{MergeSuccessRate: 1.0}},
		{"incident_attribution", DimensionScores{IncidentTrack: 1.0}},
		{"review_consensus", DimensionScores{ReviewConsensus: 1.0}},
		{"prompt_integrity", DimensionScores{PromptIntegrity: 1.0}},
		{"human_feedback", DimensionScores{HumanFeedback: 1.0}},
		{"tenure", DimensionScores{Tenure: 1.0}},
	}
	for _, tc := range tests {
		got := Calculate(tc.dims)
		want := DimensionWeights[tc.name]
		if math.Abs(float64(got)-want) > 1e-10 {
			t.Errorf("Calculate(%s=1) = %f, want weight %f", tc.name, got, want)
		}
	}
}

func TestCalculate_ClampsAboveOne(t *testing.T) {
	dims := DimensionScores{
		MergeSuccessRate: 2.0, // intentionally above 1.0
		IncidentTrack:    1.0,
		ReviewConsensus:  1.0,
		PromptIntegrity:  1.0,
		HumanFeedback:    1.0,
		Tenure:           1.0,
	}
	score := Calculate(dims)
	if score > 1.0 {
		t.Errorf("score should be clamped to [0,1], got %f", score)
	}
	if score != 1.0 {
		t.Errorf("all-max dimensions should yield 1.0 (clamped), got %f", score)
	}
}

func TestCalculate_ClampsBelowZero(t *testing.T) {
	dims := DimensionScores{
		MergeSuccessRate: -1.0,
		IncidentTrack:    -1.0,
		ReviewConsensus:  -1.0,
		PromptIntegrity:  -1.0,
		HumanFeedback:    -1.0,
		Tenure:           -1.0,
	}
	score := Calculate(dims)
	if score < 0.0 {
		t.Errorf("score should be clamped to [0,1], got %f", score)
	}
	if score != 0.0 {
		t.Errorf("all-negative dimensions should yield 0.0 (clamped), got %f", score)
	}
}

// =============================================================================
// Incident time-decay
// =============================================================================

func TestIncidentAttributionWeight_ExactDays(t *testing.T) {
	tests := []struct {
		days float64
		want float64
	}{
		{0, 1.0},
		{3, 1.0},
		{7, 1.0},
		{8, 0.50},
		{15, 0.50},
		{30, 0.50},
		{31, 0.10},
		{60, 0.10},
		{90, 0.10},
		{91, 0.0},
		{365, 0.0},
		{-1, 0.0},
	}
	for _, tc := range tests {
		got := IncidentAttributionWeight(tc.days)
		if math.Abs(got-tc.want) > 1e-10 {
			t.Errorf("IncidentAttributionWeight(%.0f) = %f, want %f", tc.days, got, tc.want)
		}
	}
}

func TestApplyIncidentPenalty_FullWeight(t *testing.T) {
	// 0.8 - (0.3 * 1.0) = 0.5
	score := ApplyIncidentPenalty(0.8, 1.0)
	if math.Abs(float64(score)-0.5) > 1e-10 {
		t.Errorf("expected 0.5, got %f", score)
	}
}

func TestApplyIncidentPenalty_HalfWeight(t *testing.T) {
	// 0.8 - (0.3 * 0.50) = 0.65
	score := ApplyIncidentPenalty(0.8, 0.50)
	if math.Abs(float64(score)-0.65) > 1e-10 {
		t.Errorf("expected 0.65, got %f", score)
	}
}

func TestApplyIncidentPenalty_TenthWeight(t *testing.T) {
	// 0.8 - (0.3 * 0.10) = 0.77
	score := ApplyIncidentPenalty(0.8, 0.10)
	if math.Abs(float64(score)-0.77) > 1e-10 {
		t.Errorf("expected 0.77, got %f", score)
	}
}

func TestApplyIncidentPenalty_ZeroWeight(t *testing.T) {
	// 0.8 - (0.3 * 0.0) = 0.8
	score := ApplyIncidentPenalty(0.8, 0.0)
	if math.Abs(float64(score)-0.8) > 1e-10 {
		t.Errorf("expected 0.8, got %f", score)
	}
}

func TestApplyIncidentPenalty_ClampAtZero(t *testing.T) {
	// 0.1 - (0.3 * 1.0) = -0.2 → clamped to 0
	score := ApplyIncidentPenalty(0.1, 1.0)
	if score != 0.0 {
		t.Errorf("expected 0.0 (clamped), got %f", score)
	}
}

// =============================================================================
// Inactivity decay
// =============================================================================

func TestApplyInactivityDecay_WithinGracePeriod(t *testing.T) {
	score := ApplyInactivityDecay(0.8, 20) // 20 days < 30 grace period
	if score != 0.8 {
		t.Errorf("expected no decay within grace period, got %f", score)
	}
}

func TestApplyInactivityDecay_ExactlyGracePeriod(t *testing.T) {
	score := ApplyInactivityDecay(0.8, 30)
	if score != 0.8 {
		t.Errorf("expected no decay at exactly grace period, got %f", score)
	}
}

func TestApplyInactivityDecay_OneWeekOver(t *testing.T) {
	// 30 grace + 7 days = 1 week → 0.8 - 0.05 = 0.75
	score := ApplyInactivityDecay(0.8, 37)
	if math.Abs(float64(score)-0.75) > 1e-10 {
		t.Errorf("expected 0.75 (one week decay), got %f", score)
	}
}

func TestApplyInactivityDecay_FourWeeks(t *testing.T) {
	// 30 grace + 28 days = 4 weeks → 0.8 - 4*0.05 = 0.6
	score := ApplyInactivityDecay(0.8, 58)
	if math.Abs(float64(score)-0.6) > 1e-10 {
		t.Errorf("expected 0.6 (4 weeks decay), got %f", score)
	}
}

func TestApplyInactivityDecay_ClampAtZero(t *testing.T) {
	// 0.1 - many weeks → clamped to 0
	score := ApplyInactivityDecay(0.1, 365)
	if score != 0.0 {
		t.Errorf("expected 0.0 (clamped), got %f", score)
	}
}

func TestApplyInactivityDecay_PartialWeekDropped(t *testing.T) {
	// 30 grace + 10 days = 1 full week (3 partial days dropped)
	score := ApplyInactivityDecay(0.8, 40)
	if math.Abs(float64(score)-0.75) > 1e-10 {
		t.Errorf("expected 0.75 (only full weeks count), got %f", score)
	}
}

// =============================================================================
// Tenure score
// =============================================================================

func TestTenureScore_ZeroDays(t *testing.T) {
	if s := TenureScore(0); s != 0 {
		t.Errorf("expected 0 for 0 days, got %f", s)
	}
}

func TestTenureScore_NegativeDays(t *testing.T) {
	if s := TenureScore(-1); s != 0 {
		t.Errorf("expected 0 for negative days, got %f", s)
	}
}

func TestTenureScore_OneDay(t *testing.T) {
	s := TenureScore(1) // log2(2) / log2(731)
	if s <= 0 || s >= 0.2 {
		t.Errorf("one day should give small positive score (~0.11), got %f", s)
	}
}

func TestTenureScore_OneYear(t *testing.T) {
	s := TenureScore(365) // log2(366) / log2(731)
	if s < 0.85 || s > 1.0 {
		t.Errorf("one year should give ~0.90, got %f", s)
	}
}

func TestTenureScore_TwoYears(t *testing.T) {
	s := TenureScore(730) // reference point
	if s > 1.0 || s < 0.95 {
		t.Errorf("two years should give ~1.0, got %f", s)
	}
}

func TestTenureScore_BeyondReference(t *testing.T) {
	s := TenureScore(1460) // 4 years
	if s != 1.0 {
		t.Errorf("beyond reference should cap at 1.0, got %f", s)
	}
}

// =============================================================================
// Incident track score
// =============================================================================

func TestIncidentTrackScore_Negative(t *testing.T) {
	if s := IncidentTrackScore(-1); s != 0 {
		t.Errorf("expected 0 for negative, got %f", s)
	}
}

func TestIncidentTrackScore_Zero(t *testing.T) {
	if s := IncidentTrackScore(0); s != 0.5 {
		t.Errorf("expected 0.5 (neutral start), got %f", s)
	}
}

func TestIncidentTrackScore_GrowsLinearly(t *testing.T) {
	prev := IncidentTrackScore(0)
	for merges := 1; merges < 10; merges++ {
		cur := IncidentTrackScore(merges)
		if cur <= prev {
			t.Errorf("expected growth at %d merges: %f <= %f", merges, cur, prev)
		}
		prev = cur
	}
}

func TestIncidentTrackScore_TenMerges(t *testing.T) {
	s := IncidentTrackScore(10)
	if math.Abs(s-0.9) > 1e-10 {
		t.Errorf("expected 0.9 at 10 merges (hits <100 branch), got %f", s)
	}
}

func TestIncidentTrackScore_NinetyNineMerges(t *testing.T) {
	s := IncidentTrackScore(99)
	if s != 0.9 {
		t.Errorf("expected 0.9 at 99 merges, got %f", s)
	}
}

func TestIncidentTrackScore_OneHundred(t *testing.T) {
	s := IncidentTrackScore(100)
	if s != 0.95 {
		t.Errorf("expected 0.95 at 100+ merges, got %f", s)
	}
}

// =============================================================================
// Merge success score
// =============================================================================

func TestMergeSuccessScore_NoMerges(t *testing.T) {
	if s := MergeSuccessScore(0, 0); s != 1.0 {
		t.Errorf("expected 1.0 (neutral), got %f", s)
	}
}

func TestMergeSuccessScore_AllSucceed(t *testing.T) {
	if s := MergeSuccessScore(10, 0); s != 1.0 {
		t.Errorf("expected 1.0, got %f", s)
	}
}

func TestMergeSuccessScore_HalfReverted(t *testing.T) {
	if s := MergeSuccessScore(5, 5); s != 0.5 {
		t.Errorf("expected 0.5, got %f", s)
	}
}

func TestMergeSuccessScore_AllReverted(t *testing.T) {
	if s := MergeSuccessScore(0, 10); s != 0.0 {
		t.Errorf("expected 0.0, got %f", s)
	}
}

// =============================================================================
// Demotion
// =============================================================================

func TestShouldDemote_ScoreAboveThreshold(t *testing.T) {
	// Veteran threshold is 0.85, score is 0.90
	if ShouldDemote(TierVeteran, 0.90, 10) {
		t.Error("should not demote when score is above threshold")
	}
}

func TestShouldDemote_ScoreBelowButNotEnoughDays(t *testing.T) {
	if ShouldDemote(TierVeteran, 0.80, 6) {
		t.Error("should not demote when only 6 consecutive days below threshold")
	}
}

func TestShouldDemote_ScoreBelowForSevenDays(t *testing.T) {
	if !ShouldDemote(TierVeteran, 0.80, 7) {
		t.Error("should demote when score below threshold for 7 consecutive days")
	}
}

func TestShouldDemote_ScoreBelowForTenDays(t *testing.T) {
	if !ShouldDemote(TierObserved, 0.30, 10) {
		t.Error("should demote when score below threshold for 10 days")
	}
}

func TestShouldDemote_ProvisionalCannotDemote(t *testing.T) {
	if ShouldDemote(TierProvisional, 0.0, 100) {
		t.Error("provisional should not demote further")
	}
}

func TestDemoteTo_VeteranToTrusted(t *testing.T) {
	if d := DemoteTo(TierVeteran); d != TierTrusted {
		t.Errorf("expected trusted, got %s", d)
	}
}

func TestDemoteTo_TrustedToObserved(t *testing.T) {
	if d := DemoteTo(TierTrusted); d != TierObserved {
		t.Errorf("expected observed, got %s", d)
	}
}

func TestDemoteTo_ObservedToProvisional(t *testing.T) {
	if d := DemoteTo(TierObserved); d != TierProvisional {
		t.Errorf("expected provisional, got %s", d)
	}
}

func TestDemoteTo_ProvisionalStaysProvisional(t *testing.T) {
	if d := DemoteTo(TierProvisional); d != TierProvisional {
		t.Errorf("expected provisional (floor), got %s", d)
	}
}

// =============================================================================
// Ledger
// =============================================================================

func TestLedger_AppendAndReplay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.jsonl")

	ledger, err := NewLedger(path)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}

	events := []TrustEvent{
		{
			AgentID:   "agent-1",
			EventType: EventMergeSuccess,
			Timestamp: time.Now().UTC(),
			Data:      EventData{ScoreBefore: 0.5, ScoreAfter: 0.52, Delta: 0.02, PRURL: "https://forgejo/pr/1"},
		},
		{
			AgentID:   "agent-1",
			EventType: EventIncidentPenalty,
			Timestamp: time.Now().UTC(),
			Data:      EventData{ScoreBefore: 0.72, ScoreAfter: 0.42, Delta: -0.30, AttributionWt: 1.0},
		},
		{
			AgentID:   "agent-2",
			EventType: EventTierChange,
			Timestamp: time.Now().UTC(),
			Data:      EventData{OldTier: "observed", NewTier: "trusted", ScoreBefore: 0.65, ScoreAfter: 0.65},
		},
	}

	for _, evt := range events {
		if err := ledger.Append(evt); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	ledger.Close()

	// Replay all
	replayed, err := Replay(path)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if len(replayed) != 3 {
		t.Fatalf("expected 3 events, got %d", len(replayed))
	}
	if replayed[0].AgentID != "agent-1" {
		t.Errorf("expected agent-1, got %s", replayed[0].AgentID)
	}
	if replayed[0].EventType != EventMergeSuccess {
		t.Errorf("expected merge_success, got %s", replayed[0].EventType)
	}
}

func TestLedger_ReplayEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")

	// File doesn't exist
	events, err := Replay(path)
	if err != nil {
		t.Fatalf("Replay on non-existent file: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}

	// File exists but empty
	_ = os.WriteFile(path, []byte{}, 0644)
	events, err = Replay(path)
	if err != nil {
		t.Fatalf("Replay on empty file: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestLedger_AppendReplayToScore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trust-replay.jsonl")

	ledger, _ := NewLedger(path)

	// Simulate an agent's lifecycle: start at 0.5 → merge success (+0.02) → incident (-0.30)
	_ = ledger.Append(TrustEvent{
		AgentID: "agent-A", EventType: EventMergeSuccess,
		Timestamp: time.Now().UTC(),
		Data:      EventData{ScoreBefore: 0.5, ScoreAfter: 0.52, Delta: 0.02},
	})
	_ = ledger.Append(TrustEvent{
		AgentID: "agent-A", EventType: EventIncidentPenalty,
		Timestamp: time.Now().UTC(),
		Data:      EventData{ScoreBefore: 0.52, ScoreAfter: 0.22, Delta: -0.30, AttributionWt: 1.0},
	})
	ledger.Close()

	// Replay to score should compute the final score
	score, err := ReplayToScore(path, "agent-A")
	if err != nil {
		t.Fatalf("ReplayToScore: %v", err)
	}

	// After merge: 0.52. After incident penalty: 0.52 - 0.30 = 0.22.
	if math.Abs(float64(score)-0.22) > 1e-10 {
		t.Errorf("expected ~0.22, got %f", score)
	}
}

func TestLedger_ReplayToScoreIgnoresOtherAgents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "multi-agent.jsonl")

	ledger, _ := NewLedger(path)
	_ = ledger.Append(TrustEvent{
		AgentID: "agent-B", EventType: EventMergeSuccess,
		Timestamp: time.Now().UTC(),
		Data:      EventData{ScoreBefore: 0.5, ScoreAfter: 0.9, Delta: 0.4},
	})
	_ = ledger.Append(TrustEvent{
		AgentID: "agent-C", EventType: EventMergeSuccess,
		Timestamp: time.Now().UTC(),
		Data:      EventData{ScoreBefore: 0.5, ScoreAfter: 0.6, Delta: 0.1},
	})
	ledger.Close()

	score, err := ReplayToScore(path, "agent-B")
	if err != nil {
		t.Fatalf("ReplayToScore: %v", err)
	}
	// agent-B's final score should be 0.9
	if math.Abs(float64(score)-0.9) > 1e-10 {
		t.Errorf("expected 0.9, got %f", score)
	}
}

// =============================================================================
// TrustScore.Clamp
// =============================================================================

func TestTrustScore_Clamp_WithinRange(t *testing.T) {
	if s := TrustScore(0.5).Clamp(); s != 0.5 {
		t.Errorf("expected 0.5, got %f", s)
	}
}

func TestTrustScore_Clamp_BelowZero(t *testing.T) {
	if s := TrustScore(-0.5).Clamp(); s != 0.0 {
		t.Errorf("expected 0.0 (clamped), got %f", s)
	}
}

func TestTrustScore_Clamp_AboveOne(t *testing.T) {
	if s := TrustScore(1.5).Clamp(); s != 1.0 {
		t.Errorf("expected 1.0 (clamped), got %f", s)
	}
}

// =============================================================================
// AllTiers
// =============================================================================

func TestAllTiers_Order(t *testing.T) {
	tiers := AllTiers()
	expected := []TrustTier{TierProvisional, TierObserved, TierTrusted, TierVeteran}
	if len(tiers) != len(expected) {
		t.Fatalf("expected %d tiers, got %d", len(expected), len(tiers))
	}
	for i, tier := range tiers {
		if tier != expected[i] {
			t.Errorf("position %d: expected %s, got %s", i, expected[i], tier)
		}
	}
}

// =============================================================================
// Integration: end-to-end scoring scenario
// =============================================================================

func TestIntegration_FullAgentLifecycle(t *testing.T) {
	// Simulate an agent's journey from Provisional to Trusted and back.

	// Day 1: New agent starts with neutral scores
	dims := DimensionScores{
		MergeSuccessRate: 1.0, // no reverted merges yet
		IncidentTrack:    0.5, // neutral — no incidents
		ReviewConsensus:  0.7,
		PromptIntegrity:  1.0,
		HumanFeedback:    0.5,
		Tenure:           TenureScore(1),
	}
	score := Calculate(dims)
	if score < 0.5 || score > 0.9 {
		t.Errorf("new agent score seems off: %f", score)
	}
	t.Logf("New agent score: %.4f", score)

	// After 100 successful merges, 100 days, good ratings
	dims100 := DimensionScores{
		MergeSuccessRate: 1.0,
		IncidentTrack:    IncidentTrackScore(100), // 0.95 — clean record
		ReviewConsensus:  0.85,
		PromptIntegrity:  1.0,
		HumanFeedback:    0.8,
		Tenure:           TenureScore(100),
	}
	score100 := Calculate(dims100)
	tier100 := DetermineTier(float64(score100), 100, 0)
	if tier100 != TierObserved {
		t.Errorf("after 100 clean merges, expected observed, got %s (score=%.4f)", tier100, score100)
	}
	t.Logf("After 100 merges: score=%.4f tier=%s", score100, tier100)

	// Incident happens: penalty
	penalized := ApplyIncidentPenalty(score100, 1.0) // full attribution weight
	tierAfterIncident := DetermineTier(float64(penalized), 100, 1)
	if tierAfterIncident == TierObserved {
		t.Logf("After incident, tier should be provisional (max 0 incidents): score=%.4f tier=%s", penalized, tierAfterIncident)
	}

	// 60 days later with inactivity decay
	decayed := ApplyInactivityDecay(penalized, 60+30+7) // 30 grace + 30 days + 7 = 1 extra week
	t.Logf("After incident + 67 days inactivity: score=%.4f", decayed)
}
