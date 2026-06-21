package negotiate

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Negotiator orchestrates the full negotiation lifecycle (spec §7.1).
// It coordinates the debate manager, Chimera arbiter client, audit logger,
// and the new context-based Negotiate API through the state machine:
// conflict_detected → round_1 → round_2 → round_3 →
// deadlock → chimera_tiebreak → resolved.
type Negotiator struct {
	Neg           *Negotiation
	Debate        *Debate
	Arbiter       *ArbiterClient
	Audit         *AuditLogger
	ChimeraResult *ChimeraVerdict
	config        NegotiationConfig
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
		config: NegotiationConfig{
			ChimeraURL: arbiterURL,
			MaxRounds:  3,
			Timeout:    30 * time.Minute,
			AuditPath:  auditPath,
		},
	}, nil
}

// NewNegotiatorFromConfig creates a Negotiator from a NegotiationConfig and
// PRContext (the context-based API from Phase 1).
func NewNegotiatorFromConfig(cfg NegotiationConfig, pr PRContext) (*Negotiator, error) {
	if len(pr.AgentPositions) < 2 {
		return nil, fmt.Errorf("INVALID_STATE: need at least 2 agent positions, got %d", len(pr.AgentPositions))
	}

	posA := pr.AgentPositions[0]
	posB := pr.AgentPositions[1]
	verdictA := Verdict(posA.Verdict)
	verdictB := Verdict(posB.Verdict)

	if !DetectConflict(verdictA, verdictB) {
		return nil, fmt.Errorf("INVALID_STATE: no conflict (both agents have verdict %s)", verdictA)
	}

	if cfg.MaxRounds <= 0 {
		cfg.MaxRounds = 3
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Minute
	}

	agentA := Agent{Name: posA.Agent, TrustLevel: 50}
	agentB := Agent{Name: posB.Agent, TrustLevel: 50}

	neg := &Negotiation{
		PRNumber:  pr.PRNumber,
		AgentA:    agentA,
		AgentB:    agentB,
		VerdictA:  verdictA,
		VerdictB:  verdictB,
		State:     StateConflictDetected,
		Round:     0,
		StartedAt: time.Now(),
	}

	audit, err := NewAuditLogger(cfg.AuditPath)
	if err != nil {
		return nil, err
	}

	return &Negotiator{
		Neg:     neg,
		Debate:  NewDebate(neg),
		Arbiter: NewArbiterClient(cfg.ChimeraURL),
		Audit:   audit,
		config:  cfg,
	}, nil
}

// Negotiate runs the full negotiation protocol for the given PR context (spec §7).
//
// Decision logic:
//  1. If all positions agree → immediate resolution, no Chimera call
//  2. If positions disagree → escalate to Chimera arbiter formation
//  3. Resolution includes which evidence points won the argument
func (n *Negotiator) Negotiate(ctx context.Context, pr PRContext) (*Resolution, error) {
	if len(pr.AgentPositions) < 2 {
		return nil, fmt.Errorf("INVALID_STATE: need at least 2 agent positions, got %d", len(pr.AgentPositions))
	}

	// Phase 1: Check for agreement
	if allPositionsAgree(pr.AgentPositions) {
		verdict := pr.AgentPositions[0].Verdict
		evidence := collectPositionEvidence(pr.AgentPositions)
		return &Resolution{
			Verdict:         verdict,
			Reasoning:       fmt.Sprintf("All %d agents agree: %s", len(pr.AgentPositions), verdict),
			WinningEvidence: evidence,
			TieBreaker:      "",
		}, nil
	}

	// Phase 2: Agents disagree → escalate to Chimera
	verdict, err := n.EscalateToChimera(ctx, pr.AgentPositions)
	if err != nil {
		return nil, err
	}

	// Phase 3: Build resolution from Chimera verdict
	winningEvidence := extractWinningEvidence(pr.AgentPositions, verdict.Verdict)

	// Also advance the state machine to resolved
	n.ChimeraResult = verdict
	_ = n.Resolve(Verdict(verdict.Verdict))

	return &Resolution{
		Verdict:         verdict.Verdict,
		Reasoning:       verdict.Trace,
		WinningEvidence: winningEvidence,
		TieBreaker: fmt.Sprintf("Chimera arbiter (confidence %.2f, cost $%.4f)",
			verdict.Confidence, verdict.Cost),
	}, nil
}

// EscalateToChimera sends the conflicting positions to Chimera's arbiter
// formation for tie-break resolution (spec §9).
//
// The prompt assembled for Chimera includes:
//   - All agent positions (agent name, verdict, evidence)
//   - The spec reference from PR context
//   - A structured question asking for APPROVE or REJECT
func (n *Negotiator) EscalateToChimera(ctx context.Context, positions []Position) (*ChimeraVerdict, error) {
	prompt := buildChimeraPrompt(positions)
	return n.Arbiter.Deliberate(prompt)
}

// buildChimeraPrompt assembles the deliberation context per spec §9.2.
func buildChimeraPrompt(positions []Position) string {
	var sb strings.Builder
	sb.WriteString("=== AGENT POSITIONS ===\n")
	for _, p := range positions {
		sb.WriteString(fmt.Sprintf("Agent %s: %s\n  Evidence: %s\n\n", p.Agent, p.Verdict, p.Evidence))
	}
	sb.WriteString("=== QUESTION ===\n")
	sb.WriteString("Resolve the conflict. Based on the evidence and debate, should this PR be APPROVED or REJECTED?\n")
	return sb.String()
}

// allPositionsAgree returns true when every position has the same verdict.
func allPositionsAgree(positions []Position) bool {
	if len(positions) == 0 {
		return true
	}
	first := positions[0].Verdict
	for _, p := range positions[1:] {
		if p.Verdict != first {
			return false
		}
	}
	return true
}

// collectPositionEvidence gathers all evidence strings from positions.
func collectPositionEvidence(positions []Position) []string {
	var evidence []string
	for _, p := range positions {
		if p.Evidence != "" {
			evidence = append(evidence, p.Evidence)
		}
	}
	return evidence
}

// extractWinningEvidence returns evidence from agents whose verdict matches
// the final verdict — these are the points that "won the argument".
func extractWinningEvidence(positions []Position, verdict string) []string {
	var evidence []string
	for _, p := range positions {
		if p.Verdict == verdict && p.Evidence != "" {
			evidence = append(evidence, p.Evidence)
		}
	}
	return evidence
}
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
