package negotiate

import (
	"fmt"
	"strings"
	"time"
)

const maxRounds = 3

// RoundTimeout is the per-round timeout (5 minutes per spec §12.1).
const RoundTimeout = 5 * time.Minute

// NegotiationTimeout is the global negotiation timeout (30 minutes per spec §12.1).
const NegotiationTimeout = 30 * time.Minute

// Debate manages the debate rounds and evidence requirements (spec §7.2-7.5).
type Debate struct {
	Neg     *Negotiation
	Rounds  []Round
	Strikes map[string]int // agent name -> strike count
}

// NewDebate creates a debate manager for the given negotiation.
func NewDebate(neg *Negotiation) *Debate {
	return &Debate{
		Neg:     neg,
		Rounds:  []Round{},
		Strikes: make(map[string]int),
	}
}

// PostArgument records an agent's argument for the current round. Returns error
// if evidence count < 2 or no spec reference found (spec §7.2 evidence requirements).
// On validation failure the agent receives a strike (spec §7.5).
func (d *Debate) PostArgument(agent string, body string, evidenceCount int) error {
	if evidenceCount < 2 {
		d.Strike(agent)
		return fmt.Errorf("EVIDENCE_REQUIRED: agent=%s round=%d (need >=2 evidence items, got %d)",
			agent, d.Neg.Round, evidenceCount)
	}
	if !hasSpecReference(body) {
		d.Strike(agent)
		return fmt.Errorf("EVIDENCE_REQUIRED: agent=%s round=%d (no spec or test reference found in body)",
			agent, d.Neg.Round)
	}
	d.Rounds = append(d.Rounds, Round{
		Number:   d.Neg.Round,
		Agent:    agent,
		Body:     body,
		Evidence: evidenceCount,
		Time:     time.Now(),
	})
	return nil
}

// CheckConcession returns true if the body contains "CONCEDE:" (spec §7.3).
func CheckConcession(body string) bool {
	return strings.Contains(body, "CONCEDE:")
}

// AdvanceRound increments the round counter. Returns true if more rounds remain
// (i.e. the new round number is within the 3-round maximum per spec §7.1).
func (d *Debate) AdvanceRound() bool {
	d.Neg.Round++
	return d.Neg.Round <= maxRounds
}

// IsDeadlocked returns true after 3 rounds with no resolution (spec §7.4).
func (d *Debate) IsDeadlocked() bool {
	return d.Neg.Round >= maxRounds && d.Neg.State == StateDeadlock
}

// Strike adds a strike to an agent. Returns the new count (spec §7.5).
// Three strikes in a single negotiation triggers auto-concede.
func (d *Debate) Strike(agent string) int {
	d.Strikes[agent]++
	return d.Strikes[agent]
}

// hasSpecReference checks whether the body cites a spec file, spec section,
// test output, or acceptance criteria — satisfying the spec §7.2 requirement
// that at least one evidence item references a spec or test.
func hasSpecReference(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "spec") ||
		strings.Contains(lower, "§") ||
		strings.Contains(lower, "test") ||
		strings.Contains(lower, "ac-")
}
