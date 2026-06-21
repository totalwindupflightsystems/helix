package negotiate

// TrustDeltas returns all trust adjustments for a negotiation event per spec §10.2.
//
// The wonTiebreak flag provides tie-break context for callers that track which
// agent won. The eventType is authoritative — each event type maps to a fixed
// delta per the spec table:
//
//	concede_evidence           +1  (agent concedes with evidence-based reason)
//	win_tiebreak               +2  (agent wins tie-break, Chimera agrees)
//	lose_tiebreak_evidence      0  (agent loses but had evidence — no penalty)
//	lose_tiebreak_no_evidence  -5  (agent loses with no evidence — frivolous)
//	frivolous_veto             -5  (Chimera finds no spec violation)
//	missed_round               -2  (agent misses a debate round)
//	three_strikes             -10  (3 strikes in single negotiation)
func TrustDeltas(eventType string, wonTiebreak bool) []TrustDelta {
	switch eventType {
	case "concede_evidence":
		return []TrustDelta{{Delta: 1, Reason: "conceded with evidence-based reason"}}
	case "win_tiebreak":
		return []TrustDelta{{Delta: 2, Reason: "won tie-break (Chimera agreed)"}}
	case "lose_tiebreak_evidence":
		return []TrustDelta{{Delta: 0, Reason: "lost tie-break with evidence (no penalty per spec §10.2)"}}
	case "lose_tiebreak_no_evidence":
		return []TrustDelta{{Delta: -5, Reason: "lost tie-break without evidence (frivolous objection)"}}
	case "frivolous_veto":
		return []TrustDelta{{Delta: -5, Reason: "frivolous veto — no spec violation found"}}
	case "missed_round":
		return []TrustDelta{{Delta: -2, Reason: "missed debate round (no post within timeout)"}}
	case "three_strikes":
		return []TrustDelta{{Delta: -10, Reason: "three strikes in single negotiation — auto-concede"}}
	default:
		return nil
	}
}

// ApplyTrust adjusts a trust level by delta, clamping to [0, 100] per spec §10.3.
// Trust floor is 0 (cannot go negative); trust ceiling is 100.
func ApplyTrust(current TrustLevel, delta int) TrustLevel {
	t := int(current) + delta
	if t < 0 {
		t = 0
	}
	if t > 100 {
		t = 100
	}
	return TrustLevel(t)
}
