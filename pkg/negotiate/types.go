// Package negotiate implements the Helix agent-to-agent PR negotiation protocol.
//
// See specs/pr-negotiation.md for the full design: structured debate across 3
// evidence-bound rounds, deadlock detection, Chimera arbiter tie-break,
// trust-weighted voting, and evidence requirements.
package negotiate

import "time"

// State is a negotiation state machine state.
type State string

const (
	StateIdle             State = "idle"
	StateConflictDetected State = "conflict_detected"
	StateRound1           State = "round_1"
	StateRound2           State = "round_2"
	StateRound3           State = "round_3"
	StateDeadlock         State = "deadlock"
	StateChimeraTiebreak  State = "chimera_tiebreak"
	StateResolved         State = "resolved"
	StateEscalated        State = "escalated"
)

// Verdict is an agent's position on a PR.
type Verdict string

const (
	VerdictApproved       Verdict = "APPROVED"
	VerdictRequestChanges Verdict = "REQUEST_CHANGES"
)

// TrustLevel is an integer 0-100 representing agent trust.
type TrustLevel int

// Agent represents a negotiating agent.
type Agent struct {
	Name        string     `json:"name"`
	TrustLevel  TrustLevel `json:"trust_level"`
	ForgejoUser string     `json:"forgejo_username"`
	Tier        string     `json:"tier"`
}

// Capability methods — what the agent can do in negotiation per spec §11.

// CanComment returns true for all agents.
func (a Agent) CanComment() bool { return true }

// CanRequestChanges requires trust >= 30.
func (a Agent) CanRequestChanges() bool { return a.TrustLevel >= 30 }

// CanParticipate requires trust >= 30.
func (a Agent) CanParticipate() bool { return a.TrustLevel >= 30 }

// CanTriggerNegotiation requires trust >= 50.
func (a Agent) CanTriggerNegotiation() bool { return a.TrustLevel >= 50 }

// CanVeto requires trust >= 70.
func (a Agent) CanVeto() bool { return a.TrustLevel >= 70 }

// Negotiation represents an active negotiation between two agents.
type Negotiation struct {
	PRNumber  int       `json:"pr_number"`
	AgentA    Agent     `json:"agent_a"`
	AgentB    Agent     `json:"agent_b"`
	VerdictA  Verdict   `json:"verdict_a"`
	VerdictB  Verdict   `json:"verdict_b"`
	State     State     `json:"state"`
	Round     int       `json:"round"`
	StartedAt time.Time `json:"started_at"`
}

// Round is one debate round in a negotiation.
type Round struct {
	Number   int       `json:"number"`
	Agent    string    `json:"agent"`
	Body     string    `json:"body"`
	Evidence int       `json:"evidence_count"`
	Time     time.Time `json:"time"`
}

// DebateEvent is one line in the JSONL debate transcript.
type DebateEvent struct {
	Round         int       `json:"round,omitempty"`
	Type          string    `json:"type"`
	Agent         string    `json:"agent,omitempty"`
	Body          string    `json:"body,omitempty"`
	EvidenceCount int       `json:"evidence_count,omitempty"`
	VerdictA      string    `json:"verdict_a,omitempty"`
	VerdictB      string    `json:"verdict_b,omitempty"`
	Verdict       string    `json:"verdict,omitempty"`
	Outcome       string    `json:"outcome,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
}

// TrustDelta represents a trust adjustment from a negotiation event.
type TrustDelta struct {
	Agent  string `json:"agent"`
	Delta  int    `json:"delta"`
	Reason string `json:"reason"`
}

// ChimeraVerdict is the tie-break result from Chimera's arbiter formation.
type ChimeraVerdict struct {
	Verdict    string  `json:"verdict"` // "APPROVE" or "REJECT"
	Confidence float64 `json:"confidence"`
	Cost       float64 `json:"cost"`
	Trace      string  `json:"trace"`
}

// Position is an agent's stated position in a negotiation (spec §7.2).
type Position struct {
	Agent    string `json:"agent"`
	Verdict  string `json:"verdict"`  // "APPROVED" or "REQUEST_CHANGES"
	Evidence string `json:"evidence"` // free-text evidence summary
}

// PRContext carries the inputs for a single negotiation (spec §3).
type PRContext struct {
	PRNumber       int        `json:"pr_number"`
	AgentPositions []Position `json:"agent_positions"`
	SpecRef        string     `json:"spec_ref"`
}

// Resolution is the outcome of a negotiation (spec §7.1 terminal state).
type Resolution struct {
	Verdict         string   `json:"verdict"`
	Reasoning       string   `json:"reasoning"`
	WinningEvidence []string `json:"winning_evidence"`
	TieBreaker      string   `json:"tie_breaker"` // empty if resolved without Chimera
}

// NegotiationConfig configures a Negotiator (spec §3.3, §9).
type NegotiationConfig struct {
	ChimeraURL     string        // Chimera base URL (e.g. "http://localhost:8765")
	MaxRounds      int           // max debate rounds (default 3)
	Timeout        time.Duration // global negotiation timeout (default 30m)
	AuditPath      string        // JSONL audit log path
	Verbose        bool          // log every state transition
}
