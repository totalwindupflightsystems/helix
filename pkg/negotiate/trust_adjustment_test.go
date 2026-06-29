package negotiate

import (
	"testing"
	"time"
)

func TestDeltaForEvent_AllEvents(t *testing.T) {
	tests := []struct {
		event     NegotiationEventType
		wantDelta int
	}{
		{EventConcessionEvidence, 1},
		{EventTieBreakWin, 2},
		{EventTieBreakLossNoPen, 0},
		{EventTieBreakLossNoEv, -5},
		{EventFrivolousVeto, -5},
		{EventMissedRound, -2},
		{EventThreeStrikes, -10},
	}
	for _, tt := range tests {
		e := NewTrustAdjustmentEngine()
		got := e.DeltaForEvent(tt.event)
		if got != tt.wantDelta {
			t.Errorf("DeltaForEvent(%s) = %d, want %d", tt.event, got, tt.wantDelta)
		}
	}
}

func TestDeltaForEvent_Unknown(t *testing.T) {
	e := NewTrustAdjustmentEngine()
	if e.DeltaForEvent("unknown_event") != 0 {
		t.Error("unknown event should return delta 0")
	}
}

func TestApplyTrustDelta_NoClampNeeded(t *testing.T) {
	if got := ApplyTrustDelta(50, 5); got != 55 {
		t.Errorf("ApplyTrustDelta(50, 5) = %d, want 55", got)
	}
}

func TestApplyTrustDelta_FloorClamp(t *testing.T) {
	if got := ApplyTrustDelta(3, -10); got != TrustFloor {
		t.Errorf("ApplyTrustDelta(3, -10) = %d, want %d (floor)", got, TrustFloor)
	}
}

func TestApplyTrustDelta_CeilingClamp(t *testing.T) {
	if got := ApplyTrustDelta(98, 5); got != TrustCeiling {
		t.Errorf("ApplyTrustDelta(98, 5) = %d, want %d (ceiling)", got, TrustCeiling)
	}
}

func TestApplyTrustDelta_ExactFloor(t *testing.T) {
	if got := ApplyTrustDelta(0, 0); got != 0 {
		t.Errorf("ApplyTrustDelta(0, 0) = %d, want 0", got)
	}
}

func TestApplyTrustDelta_ExactCeiling(t *testing.T) {
	if got := ApplyTrustDelta(100, 0); got != 100 {
		t.Errorf("ApplyTrustDelta(100, 0) = %d, want 100", got)
	}
}

func TestApplyTrustDelta_NegativeToZero(t *testing.T) {
	if got := ApplyTrustDelta(-5, 0); got != 0 {
		t.Errorf("ApplyTrustDelta(-5, 0) = %d, want 0 (negative clamped)", got)
	}
}

func TestApplyTrustDelta_OverCeiling(t *testing.T) {
	if got := ApplyTrustDelta(105, 0); got != 100 {
		t.Errorf("ApplyTrustDelta(105, 0) = %d, want 100 (over-ceiling clamped)", got)
	}
}

func TestCreateAdjustment(t *testing.T) {
	e := NewTrustAdjustmentEngine()
	at := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	adj := e.CreateAdjustment("alice", EventTieBreakWin, "won debate", at)

	if adj.Agent != "alice" {
		t.Errorf("Agent = %s, want alice", adj.Agent)
	}
	if adj.Delta != 2 {
		t.Errorf("Delta = %d, want 2", adj.Delta)
	}
	if adj.Reason != EventTieBreakWin {
		t.Errorf("Reason = %s, want %s", adj.Reason, EventTieBreakWin)
	}
	if adj.Detail != "won debate" {
		t.Errorf("Detail = %s", adj.Detail)
	}
	if !adj.Timestamp.Equal(at) {
		t.Errorf("Timestamp = %v, want %v", adj.Timestamp, at)
	}
}

func TestAdjustForNegotiationOutcome_Nil(t *testing.T) {
	e := NewTrustAdjustmentEngine()
	adjs := e.AdjustForNegotiationOutcome(nil, time.Now())
	if adjs != nil {
		t.Error("nil outcome should return nil")
	}
}

func TestAdjustForNegotiationOutcome_Concession(t *testing.T) {
	e := NewTrustAdjustmentEngine()
	at := time.Now()
	outcome := &NegotiationOutcome{
		ConcededAgent:        "bob",
		ConcededWithEvidence: true,
	}
	adjs := e.AdjustForNegotiationOutcome(outcome, at)
	if len(adjs) != 1 {
		t.Fatalf("expected 1 adjustment, got %d", len(adjs))
	}
	if adjs[0].Agent != "bob" {
		t.Errorf("Agent = %s, want bob", adjs[0].Agent)
	}
	if adjs[0].Delta != 1 {
		t.Errorf("Delta = %d, want 1 (concession with evidence)", adjs[0].Delta)
	}
}

func TestAdjustForNegotiationOutcome_ConcessionNoEvidence(t *testing.T) {
	e := NewTrustAdjustmentEngine()
	outcome := &NegotiationOutcome{
		ConcededAgent:        "bob",
		ConcededWithEvidence: false,
	}
	adjs := e.AdjustForNegotiationOutcome(outcome, time.Now())
	// concession without evidence → no positive delta
	if len(adjs) != 0 {
		t.Errorf("concession without evidence should produce 0 adjustments, got %d", len(adjs))
	}
}

func TestAdjustForNegotiationOutcome_TieBreakWin(t *testing.T) {
	e := NewTrustAdjustmentEngine()
	outcome := &NegotiationOutcome{
		WinnerAgent:    "alice",
		WonViaTieBreak: true,
	}
	adjs := e.AdjustForNegotiationOutcome(outcome, time.Now())
	if len(adjs) != 1 {
		t.Fatalf("expected 1 adjustment, got %d", len(adjs))
	}
	if adjs[0].Agent != "alice" {
		t.Errorf("Agent = %s, want alice", adjs[0].Agent)
	}
	if adjs[0].Delta != 2 {
		t.Errorf("Delta = %d, want 2 (tie-break win)", adjs[0].Delta)
	}
}

func TestAdjustForNegotiationOutcome_TieBreakLossWithEvidence(t *testing.T) {
	e := NewTrustAdjustmentEngine()
	outcome := &NegotiationOutcome{
		LoserAgent:       "bob",
		WonViaTieBreak:   true,
		LoserHadEvidence: true,
	}
	adjs := e.AdjustForNegotiationOutcome(outcome, time.Now())
	if len(adjs) != 1 {
		t.Fatalf("expected 1 adjustment, got %d", len(adjs))
	}
	if adjs[0].Agent != "bob" {
		t.Errorf("Agent = %s, want bob", adjs[0].Agent)
	}
	if adjs[0].Delta != 0 {
		t.Errorf("Delta = %d, want 0 (loss with evidence, no penalty)", adjs[0].Delta)
	}
}

func TestAdjustForNegotiationOutcome_TieBreakLossNoEvidence(t *testing.T) {
	e := NewTrustAdjustmentEngine()
	outcome := &NegotiationOutcome{
		LoserAgent:       "bob",
		WonViaTieBreak:   true,
		LoserHadEvidence: false,
	}
	adjs := e.AdjustForNegotiationOutcome(outcome, time.Now())
	if len(adjs) != 1 {
		t.Fatalf("expected 1 adjustment, got %d", len(adjs))
	}
	if adjs[0].Agent != "bob" {
		t.Errorf("Agent = %s, want bob", adjs[0].Agent)
	}
	if adjs[0].Delta != -5 {
		t.Errorf("Delta = %d, want -5 (frivolous objection)", adjs[0].Delta)
	}
}

func TestAdjustForNegotiationOutcome_FullTieBreak(t *testing.T) {
	e := NewTrustAdjustmentEngine()
	outcome := &NegotiationOutcome{
		WinnerAgent:      "alice",
		LoserAgent:       "bob",
		WonViaTieBreak:   true,
		LoserHadEvidence: true,
	}
	adjs := e.AdjustForNegotiationOutcome(outcome, time.Now())
	if len(adjs) != 2 {
		t.Fatalf("expected 2 adjustments (winner + loser), got %d", len(adjs))
	}
	// Winner: +2, Loser: 0
	winnerFound := false
	loserFound := false
	for _, adj := range adjs {
		if adj.Agent == "alice" && adj.Delta == 2 {
			winnerFound = true
		}
		if adj.Agent == "bob" && adj.Delta == 0 {
			loserFound = true
		}
	}
	if !winnerFound {
		t.Error("missing winner +2 adjustment")
	}
	if !loserFound {
		t.Error("missing loser 0 adjustment")
	}
}

func TestAdjustForNegotiationOutcome_FrivolousVeto(t *testing.T) {
	e := NewTrustAdjustmentEngine()
	outcome := &NegotiationOutcome{
		FrivolousVetoAgent: "alice",
	}
	adjs := e.AdjustForNegotiationOutcome(outcome, time.Now())
	if len(adjs) != 1 {
		t.Fatalf("expected 1 adjustment, got %d", len(adjs))
	}
	if adjs[0].Agent != "alice" {
		t.Errorf("Agent = %s, want alice", adjs[0].Agent)
	}
	if adjs[0].Delta != -5 {
		t.Errorf("Delta = %d, want -5 (frivolous veto)", adjs[0].Delta)
	}
}

func TestAdjustForNegotiationOutcome_MissedRound(t *testing.T) {
	e := NewTrustAdjustmentEngine()
	outcome := &NegotiationOutcome{
		MissedRoundAgents: []string{"bob"},
	}
	adjs := e.AdjustForNegotiationOutcome(outcome, time.Now())
	if len(adjs) != 1 {
		t.Fatalf("expected 1 adjustment, got %d", len(adjs))
	}
	if adjs[0].Agent != "bob" {
		t.Errorf("Agent = %s, want bob", adjs[0].Agent)
	}
	if adjs[0].Delta != -2 {
		t.Errorf("Delta = %d, want -2 (missed round)", adjs[0].Delta)
	}
}

func TestAdjustForNegotiationOutcome_MultipleMissedRounds(t *testing.T) {
	e := NewTrustAdjustmentEngine()
	outcome := &NegotiationOutcome{
		MissedRoundAgents: []string{"alice", "bob"},
	}
	adjs := e.AdjustForNegotiationOutcome(outcome, time.Now())
	if len(adjs) != 2 {
		t.Fatalf("expected 2 adjustments, got %d", len(adjs))
	}
}

func TestAdjustForNegotiationOutcome_ThreeStrikes(t *testing.T) {
	e := NewTrustAdjustmentEngine()
	outcome := &NegotiationOutcome{
		StrikeAutoConcedeAgent: "bob",
	}
	adjs := e.AdjustForNegotiationOutcome(outcome, time.Now())
	if len(adjs) != 1 {
		t.Fatalf("expected 1 adjustment, got %d", len(adjs))
	}
	if adjs[0].Agent != "bob" {
		t.Errorf("Agent = %s, want bob", adjs[0].Agent)
	}
	if adjs[0].Delta != -10 {
		t.Errorf("Delta = %d, want -10 (3 strikes)", adjs[0].Delta)
	}
}

func TestAdjustForNegotiationOutcome_ComplexScenario(t *testing.T) {
	e := NewTrustAdjustmentEngine()
	outcome := &NegotiationOutcome{
		WinnerAgent:            "alice",
		LoserAgent:             "bob",
		WonViaTieBreak:         true,
		LoserHadEvidence:       false,
		MissedRoundAgents:      []string{"bob"},
		StrikeAutoConcedeAgent: "bob",
	}
	adjs := e.AdjustForNegotiationOutcome(outcome, time.Now())

	// Expected adjustments:
	// 1. alice: +2 (tie-break win)
	// 2. bob: -5 (frivolous objection)
	// 3. bob: -2 (missed round)
	// 4. bob: -10 (3 strikes)
	if len(adjs) != 4 {
		t.Fatalf("expected 4 adjustments, got %d", len(adjs))
	}

	bobTotal := 0
	for _, adj := range adjs {
		if adj.Agent == "bob" {
			bobTotal += adj.Delta
		}
	}
	if bobTotal != -17 {
		t.Errorf("bob total delta = %d, want -17 (-5 + -2 + -10)", bobTotal)
	}
}

func TestAdjustForNegotiationOutcome_EmptyOutcome(t *testing.T) {
	e := NewTrustAdjustmentEngine()
	outcome := &NegotiationOutcome{}
	adjs := e.AdjustForNegotiationOutcome(outcome, time.Now())
	if len(adjs) != 0 {
		t.Errorf("empty outcome should produce 0 adjustments, got %d", len(adjs))
	}
}

func TestRecordTrustHistory(t *testing.T) {
	at := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	adj := TrustAdjustment{
		Agent:     "alice",
		Delta:     2,
		Reason:    EventTieBreakWin,
		Detail:    "won debate",
		Timestamp: at,
	}

	entry, newTrust := RecordTrustHistory("alice", 70, adj, "neg-123")

	if entry.Agent != "alice" {
		t.Errorf("Agent = %s, want alice", entry.Agent)
	}
	if entry.OldTrust != 70 {
		t.Errorf("OldTrust = %d, want 70", entry.OldTrust)
	}
	if entry.NewTrust != 72 {
		t.Errorf("NewTrust = %d, want 72", entry.NewTrust)
	}
	if entry.Delta != 2 {
		t.Errorf("Delta = %d, want 2", entry.Delta)
	}
	if entry.NegotiationID != "neg-123" {
		t.Errorf("NegotiationID = %s, want neg-123", entry.NegotiationID)
	}
	if newTrust != 72 {
		t.Errorf("newTrust = %d, want 72", newTrust)
	}
}

func TestApplyAdjustments_Batch(t *testing.T) {
	at := time.Now()
	adjustments := []TrustAdjustment{
		{Agent: "alice", Delta: 2, Reason: EventTieBreakWin, Timestamp: at},
		{Agent: "bob", Delta: -5, Reason: EventTieBreakLossNoEv, Timestamp: at},
		{Agent: "bob", Delta: -2, Reason: EventMissedRound, Timestamp: at},
	}
	currentTrusts := map[string]int{
		"alice": 70,
		"bob":   50,
	}

	result, history := ApplyAdjustments(adjustments, currentTrusts, "neg-42")

	if result["alice"] != 72 {
		t.Errorf("alice = %d, want 72", result["alice"])
	}
	if result["bob"] != 43 {
		t.Errorf("bob = %d, want 43 (50-5-2)", result["bob"])
	}
	if len(history) != 3 {
		t.Errorf("history entries = %d, want 3", len(history))
	}

	// Verify history entries have correct negotiation ID
	for _, h := range history {
		if h.NegotiationID != "neg-42" {
			t.Errorf("history NegotiationID = %s, want neg-42", h.NegotiationID)
		}
	}
}

func TestApplyAdjustments_FloorClamp(t *testing.T) {
	at := time.Now()
	adjustments := []TrustAdjustment{
		{Agent: "low", Delta: -10, Reason: EventThreeStrikes, Timestamp: at},
	}
	currentTrusts := map[string]int{
		"low": 3,
	}

	result, history := ApplyAdjustments(adjustments, currentTrusts, "neg-1")

	if result["low"] != 0 {
		t.Errorf("low = %d, want 0 (floor clamp)", result["low"])
	}
	if history[0].OldTrust != 3 {
		t.Errorf("OldTrust = %d, want 3", history[0].OldTrust)
	}
	if history[0].NewTrust != 0 {
		t.Errorf("NewTrust = %d, want 0", history[0].NewTrust)
	}
}

func TestApplyAdjustments_NilMap(t *testing.T) {
	at := time.Now()
	adjustments := []TrustAdjustment{
		{Agent: "new", Delta: 2, Reason: EventTieBreakWin, Timestamp: at},
	}

	result, _ := ApplyAdjustments(adjustments, nil, "neg-1")
	if result["new"] != 2 {
		t.Errorf("new agent from nil map = %d, want 2", result["new"])
	}
}

func TestApplyAdjustments_Empty(t *testing.T) {
	result, history := ApplyAdjustments(nil, map[string]int{"a": 50}, "neg-1")
	if len(result) != 1 || result["a"] != 50 {
		t.Error("empty adjustments should return unchanged map")
	}
	if len(history) != 0 {
		t.Errorf("history entries = %d, want 0", len(history))
	}
}

func TestTrustAdjustmentSummary_Empty(t *testing.T) {
	s := TrustAdjustmentSummary(nil)
	if s != "no trust adjustments" {
		t.Errorf("summary = %q, want 'no trust adjustments'", s)
	}
}

func TestTrustAdjustmentSummary_WithAdjustments(t *testing.T) {
	at := time.Now()
	adjustments := []TrustAdjustment{
		{Agent: "alice", Delta: 2, Timestamp: at},
		{Agent: "bob", Delta: -5, Timestamp: at},
		{Agent: "bob", Delta: -2, Timestamp: at},
	}
	s := TrustAdjustmentSummary(adjustments)
	if s == "no trust adjustments" {
		t.Error("summary should not be empty for non-empty adjustments")
	}
	if !contains(s, "alice") {
		t.Error("summary should mention alice")
	}
	if !contains(s, "bob") {
		t.Error("summary should mention bob")
	}
	if !contains(s, "+2") {
		t.Error("summary should show alice's +2")
	}
	if !contains(s, "-7") {
		t.Error("summary should show bob's total -7")
	}
}

func TestAllEventTypes(t *testing.T) {
	types := AllEventTypes()
	if len(types) != 7 {
		t.Errorf("AllEventTypes count = %d, want 7", len(types))
	}
}

func TestEventDescription_KnownTypes(t *testing.T) {
	tests := []NegotiationEventType{
		EventConcessionEvidence,
		EventTieBreakWin,
		EventTieBreakLossNoPen,
		EventTieBreakLossNoEv,
		EventFrivolousVeto,
		EventMissedRound,
		EventThreeStrikes,
	}
	for _, et := range tests {
		desc := EventDescription(et)
		if desc == "" {
			t.Errorf("EventDescription(%s) should not be empty", et)
		}
		if desc == string(et) {
			t.Errorf("EventDescription(%s) should be human-readable, not just the event name", et)
		}
	}
}

func TestEventDescription_UnknownType(t *testing.T) {
	desc := EventDescription("unknown")
	if desc != "unknown" {
		t.Errorf("EventDescription(unknown) = %q, want 'unknown'", desc)
	}
}

// Integration test: full negotiation outcome → trust adjustments → applied
func TestTrustAdjustment_FullLifecycle(t *testing.T) {
	e := NewTrustAdjustmentEngine()
	at := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)

	// Simulate a negotiation where:
	// - alice wins via Chimera tie-break
	// - bob loses without evidence (frivolous objection)
	// - bob also missed a round and got 3 strikes
	outcome := &NegotiationOutcome{
		WinnerAgent:            "alice",
		LoserAgent:             "bob",
		WonViaTieBreak:         true,
		LoserHadEvidence:       false,
		MissedRoundAgents:      []string{"bob"},
		StrikeAutoConcedeAgent: "bob",
	}

	adjustments := e.AdjustForNegotiationOutcome(outcome, at)
	currentTrusts := map[string]int{
		"alice": 85,
		"bob":   60,
	}

	result, history := ApplyAdjustments(adjustments, currentTrusts, "neg-100")

	// alice: 85 + 2 = 87
	if result["alice"] != 87 {
		t.Errorf("alice = %d, want 87", result["alice"])
	}

	// bob: 60 - 5 - 2 - 10 = 43
	if result["bob"] != 43 {
		t.Errorf("bob = %d, want 43", result["bob"])
	}

	// 4 history entries (1 for alice, 3 for bob)
	if len(history) != 4 {
		t.Errorf("history entries = %d, want 4", len(history))
	}

	// Verify summary
	s := TrustAdjustmentSummary(adjustments)
	if !contains(s, "alice") || !contains(s, "bob") {
		t.Error("summary should mention both agents")
	}
}

func TestTrustAdjustmentSpecDeltas(t *testing.T) {
	// Verify exact deltas from spec §10.2 table
	e := NewTrustAdjustmentEngine()

	cases := []struct {
		event     NegotiationEventType
		wantDelta int
		desc      string
	}{
		{EventConcessionEvidence, 1, "concession with evidence → +1"},
		{EventTieBreakWin, 2, "tie-break win → +2"},
		{EventTieBreakLossNoPen, 0, "tie-break loss with evidence → 0"},
		{EventTieBreakLossNoEv, -5, "tie-break loss no evidence → -5"},
		{EventFrivolousVeto, -5, "frivolous veto → -5"},
		{EventMissedRound, -2, "missed round → -2"},
		{EventThreeStrikes, -10, "3 strikes → -10"},
	}

	for _, c := range cases {
		got := e.DeltaForEvent(c.event)
		if got != c.wantDelta {
			t.Errorf("%s: DeltaForEvent = %d, want %d", c.desc, got, c.wantDelta)
		}
	}
}
