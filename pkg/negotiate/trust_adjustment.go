package negotiate

// trust_adjustment.go implements the trust adjustment engine for negotiation
// events per specs/pr-negotiation.md §10.2 (Trust Adjustments from Negotiation)
// and §10.3 (Trust Floor and Ceiling).
//
// Trust adjustment table (spec §10.2):
//
//	Event                                         Delta
//	---                                          -----
//	Agent concedes with evidence-based reason      +1
//	Agent wins tie-break (Chimera agrees)          +2
//	Agent loses tie-break (Chimera disagrees)       0  (no penalty if evidence-backed)
//	Agent loses tie-break with NO evidence          -5 (frivolous objection)
//	Frivolous veto (Chimera finds no violation)    -5
//	Agent misses debate round (no post in timeout) -2
//	3 strikes in single negotiation                -10 + auto-concede
//
// Trust is clamped to [0, 100] (spec §10.3).

import (
	"fmt"
	"time"
)

// NegotiationEventType classifies events that affect trust during negotiation.
type NegotiationEventType string

const (
	EventConcessionEvidence NegotiationEventType = "concession_with_evidence"
	EventTieBreakWin        NegotiationEventType = "tie_break_win"
	EventTieBreakLossNoPen  NegotiationEventType = "tie_break_loss_no_penalty"
	EventTieBreakLossNoEv   NegotiationEventType = "tie_break_loss_no_evidence"
	EventFrivolousVeto      NegotiationEventType = "frivolous_veto"
	EventMissedRound        NegotiationEventType = "missed_round"
	EventThreeStrikes       NegotiationEventType = "three_strikes"
)

// TrustAdjustment represents a single trust delta event.
type TrustAdjustment struct {
	Agent     string               `json:"agent"`
	Delta     int                  `json:"delta"`
	Reason    NegotiationEventType `json:"reason"`
	Detail    string               `json:"detail,omitempty"`
	Timestamp time.Time            `json:"timestamp"`
}

// TrustFloor and TrustCeiling per spec §10.3.
const (
	TrustFloor   = 0
	TrustCeiling = 100
)

// TrustAdjustmentEngine computes trust deltas for all negotiation events
// per spec §10.2.
type TrustAdjustmentEngine struct{}

// NewTrustAdjustmentEngine creates a new engine.
func NewTrustAdjustmentEngine() *TrustAdjustmentEngine {
	return &TrustAdjustmentEngine{}
}

// DeltaForEvent returns the trust delta for a given event type (spec §10.2 table).
func (e *TrustAdjustmentEngine) DeltaForEvent(eventType NegotiationEventType) int {
	switch eventType {
	case EventConcessionEvidence:
		return 1
	case EventTieBreakWin:
		return 2
	case EventTieBreakLossNoPen:
		return 0
	case EventTieBreakLossNoEv:
		return -5
	case EventFrivolousVeto:
		return -5
	case EventMissedRound:
		return -2
	case EventThreeStrikes:
		return -10
	default:
		return 0
	}
}

// ApplyTrustDelta applies a delta to the current trust level, clamping to
// [0, 100] (spec §10.3 floor/ceiling). Returns the new trust level.
func ApplyTrustDelta(current int, delta int) int {
	newVal := current + delta
	if newVal < TrustFloor {
		return TrustFloor
	}
	if newVal > TrustCeiling {
		return TrustCeiling
	}
	return newVal
}

// CreateAdjustment builds a TrustAdjustment from the event type and context.
func (e *TrustAdjustmentEngine) CreateAdjustment(agent string, eventType NegotiationEventType, detail string, at time.Time) TrustAdjustment {
	return TrustAdjustment{
		Agent:     agent,
		Delta:     e.DeltaForEvent(eventType),
		Reason:    eventType,
		Detail:    detail,
		Timestamp: at,
	}
}

// NegotiationOutcome captures the outcome of a complete negotiation for
// computing all trust adjustments at once.
type NegotiationOutcome struct {
	WinnerAgent            string   // agent who won
	LoserAgent             string   // agent who lost
	WonViaTieBreak         bool     // resolved by Chimera
	LoserHadEvidence       bool     // loser backed with evidence
	ConcededAgent          string   // agent who conceded (empty if none)
	ConcededWithEvidence   bool     // concession was evidence-based
	FrivolousVetoAgent     string   // agent who made frivolous veto (empty if none)
	MissedRoundAgents      []string // agents who missed rounds
	StrikeAutoConcedeAgent string   // agent who got 3 strikes + auto-concede (empty if none)
}

// AdjustForNegotiationOutcome computes all trust deltas for both agents after
// a negotiation completes (spec §10.2). Returns adjustments in the order they
// should be applied.
func (e *TrustAdjustmentEngine) AdjustForNegotiationOutcome(outcome *NegotiationOutcome, at time.Time) []TrustAdjustment {
	if outcome == nil {
		return nil
	}

	var adjustments []TrustAdjustment

	// Concession with evidence
	if outcome.ConcededAgent != "" && outcome.ConcededWithEvidence {
		adjustments = append(adjustments, e.CreateAdjustment(
			outcome.ConcededAgent, EventConcessionEvidence,
			"constructive concession with evidence", at))
	}

	// Tie-break outcome
	if outcome.WonViaTieBreak {
		if outcome.WinnerAgent != "" {
			adjustments = append(adjustments, e.CreateAdjustment(
				outcome.WinnerAgent, EventTieBreakWin,
				"won Chimera tie-break", at))
		}
		if outcome.LoserAgent != "" {
			if outcome.LoserHadEvidence {
				adjustments = append(adjustments, e.CreateAdjustment(
					outcome.LoserAgent, EventTieBreakLossNoPen,
					"lost tie-break with evidence (no penalty)", at))
			} else {
				adjustments = append(adjustments, e.CreateAdjustment(
					outcome.LoserAgent, EventTieBreakLossNoEv,
					"frivolous objection (no evidence)", at))
			}
		}
	}

	// Frivolous veto
	if outcome.FrivolousVetoAgent != "" {
		adjustments = append(adjustments, e.CreateAdjustment(
			outcome.FrivolousVetoAgent, EventFrivolousVeto,
			"Chimera found no spec violation", at))
	}

	// Missed rounds
	for _, agent := range outcome.MissedRoundAgents {
		adjustments = append(adjustments, e.CreateAdjustment(
			agent, EventMissedRound,
			"missed debate round (no post within timeout)", at))
	}

	// 3 strikes → auto-concede
	if outcome.StrikeAutoConcedeAgent != "" {
		adjustments = append(adjustments, e.CreateAdjustment(
			outcome.StrikeAutoConcedeAgent, EventThreeStrikes,
			"3 strikes in single negotiation → auto-concede", at))
	}

	return adjustments
}

// TrustHistoryEntry records a trust adjustment for audit.
type TrustHistoryEntry struct {
	Agent         string               `json:"agent"`
	OldTrust      int                  `json:"old_trust"`
	NewTrust      int                  `json:"new_trust"`
	Delta         int                  `json:"delta"`
	Reason        NegotiationEventType `json:"reason"`
	Detail        string               `json:"detail,omitempty"`
	NegotiationID string               `json:"negotiation_id,omitempty"`
	Timestamp     time.Time            `json:"timestamp"`
}

// RecordTrustHistory applies a TrustAdjustment to an agent's current trust,
// records the transition as a TrustHistoryEntry, and returns the entry plus
// the new trust value.
func RecordTrustHistory(agent string, currentTrust int, adj TrustAdjustment, negotiationID string) (TrustHistoryEntry, int) {
	newTrust := ApplyTrustDelta(currentTrust, adj.Delta)
	entry := TrustHistoryEntry{
		Agent:         agent,
		OldTrust:      currentTrust,
		NewTrust:      newTrust,
		Delta:         adj.Delta,
		Reason:        adj.Reason,
		Detail:        adj.Detail,
		NegotiationID: negotiationID,
		Timestamp:     adj.Timestamp,
	}
	return entry, newTrust
}

// ApplyAdjustments applies a batch of trust adjustments to agents' current
// trust levels. The currentTrusts map is agent name → current trust. Returns
// the updated map and the audit history entries.
func ApplyAdjustments(adjustments []TrustAdjustment, currentTrusts map[string]int, negotiationID string) (map[string]int, []TrustHistoryEntry) {
	if currentTrusts == nil {
		currentTrusts = make(map[string]int)
	}
	result := make(map[string]int)
	for k, v := range currentTrusts {
		result[k] = v
	}

	var history []TrustHistoryEntry
	for _, adj := range adjustments {
		current := result[adj.Agent]
		entry, newTrust := RecordTrustHistory(adj.Agent, current, adj, negotiationID)
		result[adj.Agent] = newTrust
		history = append(history, entry)
	}

	return result, history
}

// Summary returns a human-readable summary of trust adjustments.
func TrustAdjustmentSummary(adjustments []TrustAdjustment) string {
	if len(adjustments) == 0 {
		return "no trust adjustments"
	}

	// Aggregate per agent
	totals := make(map[string]int)
	for _, adj := range adjustments {
		totals[adj.Agent] += adj.Delta
	}

	var sb []string
	for agent, total := range totals {
		sign := "+"
		if total < 0 {
			sign = ""
		}
		sb = append(sb, fmt.Sprintf("%s: %s%d", agent, sign, total))
	}

	result := "trust adjustments: "
	for i, s := range sb {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}

// AllEventTypes returns all valid negotiation event types for iteration/testing.
func AllEventTypes() []NegotiationEventType {
	return []NegotiationEventType{
		EventConcessionEvidence,
		EventTieBreakWin,
		EventTieBreakLossNoPen,
		EventTieBreakLossNoEv,
		EventFrivolousVeto,
		EventMissedRound,
		EventThreeStrikes,
	}
}

// EventDescription returns a human-readable description of an event type.
func EventDescription(eventType NegotiationEventType) string {
	switch eventType {
	case EventConcessionEvidence:
		return "Agent concedes with evidence-based reason"
	case EventTieBreakWin:
		return "Agent wins tie-break (Chimera agrees)"
	case EventTieBreakLossNoPen:
		return "Agent loses tie-break (Chimera disagrees) with evidence"
	case EventTieBreakLossNoEv:
		return "Agent loses tie-break with NO evidence (frivolous)"
	case EventFrivolousVeto:
		return "Frivolous veto (Chimera finds no spec violation)"
	case EventMissedRound:
		return "Agent misses debate round (no post within timeout)"
	case EventThreeStrikes:
		return "3 strikes in single negotiation"
	default:
		return string(eventType)
	}
}
