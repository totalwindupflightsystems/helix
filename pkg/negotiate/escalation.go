package negotiate

// escalation.go implements the escalation PR comment formatter per
// specs/pr-negotiation.md §12.2 (Escalation Format).
//
// When a negotiation is escalated to human review, a structured markdown
// comment is posted to the PR. This file provides the data structures and
// formatter for that comment.

import (
	"fmt"
	"strings"
	"time"
)

// EscalationReason is the cause of an escalation (spec §12.1).
type EscalationReason string

const (
	EscalationTimeout            EscalationReason = "timeout"
	EscalationBudgetExhausted    EscalationReason = "budget_exhausted"
	EscalationChimeraUnavailable EscalationReason = "chimera_unavailable"
)

// EscalationData carries all the context needed to render the escalation
// comment (spec §12.2).
type EscalationData struct {
	Reason          EscalationReason `json:"reason"`
	AgentA          Agent            `json:"agent_a"`
	AgentB          Agent            `json:"agent_b"`
	RoundsCompleted int              `json:"rounds_completed"`
	Deadlocked      bool             `json:"deadlocked"`
	DebateLogPath   string           `json:"debate_log_path"`
	AgentAPosition  Position         `json:"agent_a_position"`
	AgentBPosition  Position         `json:"agent_b_position"`
	ChimeraPrelim   *ChimeraVerdict  `json:"chimera_preliminary,omitempty"`
}

// FormatEscalationComment renders the spec §12.2 escalation PR comment template.
// The output is a markdown string suitable for posting via the Forgejo API.
func FormatEscalationComment(data *EscalationData) string {
	var sb strings.Builder

	sb.WriteString("## ⚠️ Negotiation Escalated — Human Review Required\n\n")

	sb.WriteString(fmt.Sprintf("**Reason:** %s\n", data.Reason))
	sb.WriteString(fmt.Sprintf("**Agents:** @%s (trust=%d), @%s (trust=%d)\n",
		data.AgentA.Name, data.AgentA.TrustLevel,
		data.AgentB.Name, data.AgentB.TrustLevel))
	sb.WriteString(fmt.Sprintf("**Rounds completed:** %d/3\n", data.RoundsCompleted))
	sb.WriteString(fmt.Sprintf("**Deadlock:** %s\n", boolYesNo(data.Deadlocked)))

	if data.DebateLogPath != "" {
		sb.WriteString(fmt.Sprintf("**Debate log:** %s\n", data.DebateLogPath))
	}

	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("**%s position:** %s — %s\n",
		data.AgentA.Name, data.AgentAPosition.Verdict, data.AgentAPosition.Evidence))
	sb.WriteString(fmt.Sprintf("**%s position:** %s — %s\n",
		data.AgentB.Name, data.AgentBPosition.Verdict, data.AgentBPosition.Evidence))

	sb.WriteString("\n**Recommended action:** ")
	if data.ChimeraPrelim != nil {
		sb.WriteString(fmt.Sprintf("Chimera preliminary verdict: %s (confidence: %.0f%%)",
			data.ChimeraPrelim.Verdict, data.ChimeraPrelim.Confidence*100))
	} else {
		sb.WriteString("manual review required")
	}
	sb.WriteString("\n")

	return sb.String()
}

// EscalationFromNegotiator builds an EscalationData from a Negotiator's
// current state, suitable for passing to FormatEscalationComment.
func EscalationFromNegotiator(n *Negotiator, reason EscalationReason) *EscalationData {
	data := &EscalationData{
		Reason:          reason,
		AgentA:          n.Neg.AgentA,
		AgentB:          n.Neg.AgentB,
		RoundsCompleted: n.Neg.Round,
		Deadlocked:      n.Neg.State == StateDeadlock,
	}

	if n.Audit != nil {
		data.DebateLogPath = n.Audit.path
	}

	// Build positions from the negotiation's stored verdicts.
	data.AgentAPosition = Position{
		Agent:    n.Neg.AgentA.Name,
		Verdict:  string(n.Neg.VerdictA),
		Evidence: "(see debate log)",
	}
	data.AgentBPosition = Position{
		Agent:    n.Neg.AgentB.Name,
		Verdict:  string(n.Neg.VerdictB),
		Evidence: "(see debate log)",
	}

	// Include Chimera preliminary verdict if available.
	if n.ChimeraResult != nil {
		data.ChimeraPrelim = n.ChimeraResult
	}

	return data
}

// boolYesNo returns "yes" or "no" for markdown rendering.
func boolYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// EscalationExitCode maps an escalation reason to the spec §14 exit code.
func EscalationExitCode(reason EscalationReason) int {
	switch reason {
	case EscalationTimeout:
		return 4
	case EscalationBudgetExhausted:
		return 3
	case EscalationChimeraUnavailable:
		return 2
	default:
		return 5 // INVALID_STATE
	}
}

// EscalationMessage formats the spec §14 exit message for the given reason.
func EscalationMessage(reason EscalationReason, detail string) string {
	switch reason {
	case EscalationTimeout:
		return fmt.Sprintf("NEGOTIATION_TIMEOUT: %s escalated=true", detail)
	case EscalationBudgetExhausted:
		return fmt.Sprintf("BUDGET_EXHAUSTED: %s", detail)
	case EscalationChimeraUnavailable:
		return fmt.Sprintf("CHIMERA_UNAVAILABLE: %s", detail)
	default:
		return fmt.Sprintf("INVALID_STATE: %s", detail)
	}
}

// IsTerminal checks if the negotiation is in a state where escalation is
// valid (deadlock or any active round with timeout/budget failure).
func IsEscalatable(state State) bool {
	switch state {
	case StateDeadlock, StateRound1, StateRound2, StateRound3,
		StateConflictDetected, StateChimeraTiebreak:
		return true
	default:
		return false
	}
}

// EscalationTimestamp formats the current time for audit log entries.
func EscalationTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339)
}
