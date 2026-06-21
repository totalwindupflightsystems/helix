package negotiate

import (
	"fmt"
	"strings"
	"time"
)

// Negotiator orchestrates the full negotiation lifecycle (spec §7.1).
// It coordinates the debate manager, Chimera arbiter client, and audit logger
// through the state machine: conflict_detected → round_1 → round_2 → round_3 →
// deadlock → chimera_tiebreak → resolved.
type Negotiator struct {
	Neg           *Negotiation
	Debate        *Debate
	Arbiter       *ArbiterClient
	Audit         *AuditLogger
	ChimeraResult *ChimeraVerdict
}

// NewNegotiator creates a Negotiator for two agents on a PR.
// Returns an error if both agents have the same verdict (no conflict).
func NewNegotiator(prNumber int, agentA, agentB Agent, verdictA, verdictB Verdict,
	arbiterURL, auditPath string) (*Negotiator, error) {
	if !DetectConflict(verdictA, verdictB) {
		return nil, fmt.Errorf("INVALID_STATE: no conflict (both agents have verdict %s)", verdictA)
	}

	neg := &Negotiation{
		PRNumber:  prNumber,
		AgentA:    agentA,
		AgentB:    agentB,
		VerdictA:  verdictA,
		VerdictB:  verdictB,
		State:     StateConflictDetected,
		Round:     0,
		StartedAt: time.Now(),
	}

	audit, err := NewAuditLogger(auditPath)
	if err != nil {
		return nil, err
	}

	return &Negotiator{
		Neg:     neg,
		Debate:  NewDebate(neg),
		Arbiter: NewArbiterClient(arbiterURL),
		Audit:   audit,
	}, nil
}

// DetectConflict returns true if the two verdicts differ (spec §7.1 trigger).
func DetectConflict(a, b Verdict) bool {
	return a != b
}

// IsVeto returns true if the comment body starts with "VETO:" and the agent
// has sufficient trust to veto (trust >= 70 per spec §8).
func IsVeto(agent Agent, body string) bool {
	return strings.HasPrefix(strings.TrimSpace(body), "VETO:") && agent.CanVeto()
}

// Advance runs one step of the state machine. Returns the new state.
// Terminal states (resolved, escalated) return an error and do not advance.
func (n *Negotiator) Advance() (State, error) {
	switch n.Neg.State {
	case StateIdle:
		return StateIdle, fmt.Errorf("cannot advance from idle state")

	case StateConflictDetected:
		n.Neg.Round = 1
		n.setState(StateRound1)

	case StateRound1:
		if n.hasConcession() {
			n.setState(StateResolved)
		} else {
			n.Neg.Round = 2
			n.setState(StateRound2)
		}

	case StateRound2:
		if n.hasConcession() {
			n.setState(StateResolved)
		} else {
			n.Neg.Round = 3
			n.setState(StateRound3)
		}

	case StateRound3:
		if n.hasConcession() {
			n.setState(StateResolved)
		} else {
			n.setState(StateDeadlock)
		}

	case StateDeadlock:
		n.setState(StateChimeraTiebreak)

	case StateChimeraTiebreak:
		verdict, err := n.callArbiter()
		if err != nil {
			_ = n.Escalate(fmt.Sprintf("chimera_unavailable: %v", err))
			return StateEscalated, err
		}
		n.ChimeraResult = verdict
		n.setState(StateResolved)

	case StateResolved, StateEscalated:
		return n.Neg.State, fmt.Errorf("negotiation already %s", n.Neg.State)
	}

	return n.Neg.State, nil
}

// Escalate marks the negotiation as escalated to human review (spec §12.2).
func (n *Negotiator) Escalate(reason string) error {
	n.Neg.State = StateEscalated
	if n.Audit != nil {
		_ = n.Audit.LogEvent(DebateEvent{
			Type:      "escalated",
			Outcome:   reason,
			Timestamp: time.Now(),
		})
	}
	return nil
}

// Resolve marks the negotiation as resolved with the given outcome.
func (n *Negotiator) Resolve(outcome Verdict) error {
	n.Neg.State = StateResolved
	if n.Audit != nil {
		_ = n.Audit.LogEvent(DebateEvent{
			Type:      "resolved",
			Outcome:   string(outcome),
			Timestamp: time.Now(),
		})
	}
	return nil
}

// TransitionTable maps current state + condition to next state (spec §7.1 table).
var TransitionTable = map[State]map[string]State{
	StateIdle:             {"conflict_detected": StateConflictDetected},
	StateConflictDetected: {"start": StateRound1},
	StateRound1:           {"continue": StateRound2, "concede": StateResolved},
	StateRound2:           {"continue": StateRound3, "concede": StateResolved},
	StateRound3:           {"continue": StateDeadlock, "concede": StateResolved},
	StateDeadlock:         {"tiebreak": StateChimeraTiebreak},
	StateChimeraTiebreak:  {"resolved": StateResolved},
}

// --- internal helpers ---

// setState updates the negotiation state and logs the transition.
func (n *Negotiator) setState(s State) {
	n.Neg.State = s
	if n.Audit != nil {
		_ = n.Audit.LogEvent(DebateEvent{
			Type:      string(s),
			Timestamp: time.Now(),
		})
	}
}

// hasConcession scans all posted rounds for a "CONCEDE:" marker.
func (n *Negotiator) hasConcession() bool {
	for _, r := range n.Debate.Rounds {
		if CheckConcession(r.Body) {
			return true
		}
	}
	return false
}

// callArbiter builds the deliberation prompt and calls Chimera.
func (n *Negotiator) callArbiter() (*ChimeraVerdict, error) {
	prompt := n.buildArbiterPrompt()
	return n.Arbiter.Deliberate(prompt)
}

// buildArbiterPrompt assembles the deliberation context per spec §9.2.
func (n *Negotiator) buildArbiterPrompt() string {
	var sb strings.Builder
	sb.WriteString("=== AGENT REVIEWS ===\n")
	sb.WriteString(fmt.Sprintf("Agent A (@%s, trust=%d): %s\n",
		n.Neg.AgentA.Name, n.Neg.AgentA.TrustLevel, n.Neg.VerdictA))
	sb.WriteString(fmt.Sprintf("Agent B (@%s, trust=%d): %s\n",
		n.Neg.AgentB.Name, n.Neg.AgentB.TrustLevel, n.Neg.VerdictB))
	sb.WriteString("\n=== DEBATE TRANSCRIPT ===\n")
	for _, r := range n.Debate.Rounds {
		sb.WriteString(fmt.Sprintf("Round %d [%s]: %s\n", r.Number, r.Agent, r.Body))
	}
	sb.WriteString("\n=== QUESTION ===\n")
	sb.WriteString("Resolve the conflict. Based on the spec, evidence, and debate: APPROVE or REJECT?\n")
	return sb.String()
}
