package negotiate

import (
	"testing"
)

func TestTrustDeltas(t *testing.T) {
	tests := []struct {
		name         string
		eventType    string
		wonTiebreak  bool
		wantDelta    int
		wantNil      bool
	}{
		{name: "concede_evidence", eventType: "concede_evidence", wantDelta: 1},
		{name: "win_tiebreak", eventType: "win_tiebreak", wantDelta: 2},
		{name: "lose_tiebreak_evidence", eventType: "lose_tiebreak_evidence", wantDelta: 0},
		{name: "lose_tiebreak_no_evidence", eventType: "lose_tiebreak_no_evidence", wantDelta: -5},
		{name: "frivolous_veto", eventType: "frivolous_veto", wantDelta: -5},
		{name: "missed_round", eventType: "missed_round", wantDelta: -2},
		{name: "three_strikes", eventType: "three_strikes", wantDelta: -10},
		{name: "unknown_event", eventType: "nonexistent_event", wantNil: true},
		{name: "empty_event", eventType: "", wantNil: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deltas := TrustDeltas(tt.eventType, tt.wonTiebreak)
			if tt.wantNil {
				if deltas != nil {
					t.Errorf("TrustDeltas(%q, %v) = %v, want nil", tt.eventType, tt.wonTiebreak, deltas)
				}
				return
			}
			if len(deltas) != 1 {
				t.Fatalf("TrustDeltas(%q, %v) returned %d results, want 1", tt.eventType, tt.wonTiebreak, len(deltas))
			}
			if deltas[0].Delta != tt.wantDelta {
				t.Errorf("TrustDeltas(%q, %v).Delta = %d, want %d", tt.eventType, tt.wonTiebreak, deltas[0].Delta, tt.wantDelta)
			}
			if deltas[0].Reason == "" {
				t.Errorf("TrustDeltas(%q, %v).Reason is empty", tt.eventType, tt.wonTiebreak)
			}
		})
	}
}

// TestTrustDeltas_AllEventsHaveReasons verifies every valid event type returns a
// non-empty Reason string.
func TestTrustDeltas_AllEventsHaveReasons(t *testing.T) {
	events := []string{
		"concede_evidence",
		"win_tiebreak",
		"lose_tiebreak_evidence",
		"lose_tiebreak_no_evidence",
		"frivolous_veto",
		"missed_round",
		"three_strikes",
	}
	for _, event := range events {
		t.Run(event, func(t *testing.T) {
			deltas := TrustDeltas(event, false)
			if len(deltas) != 1 || deltas[0].Reason == "" {
				t.Errorf("event %q: empty reason", event)
			}
		})
	}
}

func TestApplyTrust(t *testing.T) {
	tests := []struct {
		name    string
		current TrustLevel
		delta   int
		want    TrustLevel
	}{
		{name: "positive_delta", current: 50, delta: 10, want: 60},
		{name: "negative_delta", current: 50, delta: -10, want: 40},
		{name: "zero_delta", current: 50, delta: 0, want: 50},
		{name: "clamp_floor_at_zero", current: 3, delta: -5, want: 0},
		{name: "clamp_floor_deep_negative", current: 0, delta: -100, want: 0},
		{name: "clamp_ceiling_at_100", current: 95, delta: 10, want: 100},
		{name: "clamp_ceiling_deep_positive", current: 100, delta: 50, want: 100},
		{name: "zero_current_positive_delta", current: 0, delta: 5, want: 5},
		{name: "zero_current_negative_delta", current: 0, delta: -1, want: 0},
		{name: "boundary_current", current: 100, delta: 0, want: 100},
		{name: "max_delta", current: 0, delta: 100, want: 100},
		{name: "min_delta", current: 100, delta: -100, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApplyTrust(tt.current, tt.delta)
			if got != tt.want {
				t.Errorf("ApplyTrust(%d, %d) = %d, want %d", tt.current, tt.delta, got, tt.want)
			}
		})
	}
}

// TestApplyTrust_TypeAssertion verifies TrustLevel is compatible with int operations.
func TestApplyTrust_TypeAssertion(t *testing.T) {
	// Verify int(TrustLevel) and TrustLevel(int) round-trip
	for i := 0; i <= 100; i++ {
		lvl := TrustLevel(i)
		if int(lvl) != i {
			t.Errorf("int(TrustLevel(%d)) = %d, want %d", i, int(lvl), i)
		}
	}
}
