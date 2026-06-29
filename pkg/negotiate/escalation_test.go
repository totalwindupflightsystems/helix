package negotiate

import (
	"strings"
	"testing"
)

func TestFormatEscalationComment_Timeout(t *testing.T) {
	data := &EscalationData{
		Reason:          EscalationTimeout,
		AgentA:          Agent{Name: "wojons", TrustLevel: 85},
		AgentB:          Agent{Name: "llopez", TrustLevel: 52},
		RoundsCompleted: 3,
		Deadlocked:      true,
		DebateLogPath:   "~/.helix/negotiations/44-20260629.jsonl",
		AgentAPosition:  Position{Agent: "wojons", Verdict: "APPROVED", Evidence: "CI supports Docker"},
		AgentBPosition:  Position{Agent: "llopez", Verdict: "REQUEST_CHANGES", Evidence: "Spec requires prod Postgres"},
	}

	comment := FormatEscalationComment(data)

	if !strings.Contains(comment, "Negotiation Escalated") {
		t.Error("missing header")
	}
	if !strings.Contains(comment, "timeout") {
		t.Error("missing reason")
	}
	if !strings.Contains(comment, "@wojons (trust=85)") {
		t.Error("missing agent A")
	}
	if !strings.Contains(comment, "@llopez (trust=52)") {
		t.Error("missing agent B")
	}
	if !strings.Contains(comment, "3/3") {
		t.Error("missing rounds")
	}
	if !strings.Contains(comment, "Deadlock:** yes") {
		t.Error("missing deadlock status")
	}
	if !strings.Contains(comment, "wojons position") {
		t.Error("missing agent A position")
	}
	if !strings.Contains(comment, "manual review required") {
		t.Error("missing default recommended action")
	}
}

func TestFormatEscalationComment_BudgetExhausted(t *testing.T) {
	data := &EscalationData{
		Reason:          EscalationBudgetExhausted,
		AgentA:          Agent{Name: "a1", TrustLevel: 60},
		AgentB:          Agent{Name: "a2", TrustLevel: 40},
		RoundsCompleted: 1,
		Deadlocked:      false,
	}
	comment := FormatEscalationComment(data)
	if !strings.Contains(comment, "budget_exhausted") {
		t.Error("missing budget reason")
	}
	if !strings.Contains(comment, "Deadlock:** no") {
		t.Error("missing deadlock status — expected 'Deadlock:** no'")
	}
}

func TestFormatEscalationComment_ChimeraUnavailable(t *testing.T) {
	data := &EscalationData{
		Reason:          EscalationChimeraUnavailable,
		AgentA:          Agent{Name: "a1", TrustLevel: 70},
		AgentB:          Agent{Name: "a2", TrustLevel: 50},
		RoundsCompleted: 3,
		Deadlocked:      true,
	}
	comment := FormatEscalationComment(data)
	if !strings.Contains(comment, "chimera_unavailable") {
		t.Error("missing chimera reason")
	}
}

func TestFormatEscalationComment_WithChimeraPrelim(t *testing.T) {
	data := &EscalationData{
		Reason:          EscalationTimeout,
		AgentA:          Agent{Name: "a1", TrustLevel: 80},
		AgentB:          Agent{Name: "a2", TrustLevel: 60},
		RoundsCompleted: 3,
		Deadlocked:      true,
		ChimeraPrelim: &ChimeraVerdict{
			Verdict:    "APPROVE",
			Confidence: 0.78,
		},
	}
	comment := FormatEscalationComment(data)
	if !strings.Contains(comment, "Chimera preliminary verdict") {
		t.Error("missing chimera preliminary")
	}
	if !strings.Contains(comment, "APPROVE") {
		t.Error("missing chimera verdict value")
	}
	if !strings.Contains(comment, "78%") {
		t.Error("missing confidence percentage")
	}
}

func TestFormatEscalationComment_NoDebateLog(t *testing.T) {
	data := &EscalationData{
		Reason: EscalationTimeout,
		AgentA: Agent{Name: "a", TrustLevel: 50},
		AgentB: Agent{Name: "b", TrustLevel: 50},
	}
	comment := FormatEscalationComment(data)
	if strings.Contains(comment, "Debate log:") {
		t.Error("should not contain debate log when empty")
	}
}

func TestEscalationExitCode(t *testing.T) {
	tests := []struct {
		reason EscalationReason
		code   int
	}{
		{EscalationTimeout, 4},
		{EscalationBudgetExhausted, 3},
		{EscalationChimeraUnavailable, 2},
		{EscalationReason("unknown"), 5},
	}
	for _, tc := range tests {
		if got := EscalationExitCode(tc.reason); got != tc.code {
			t.Errorf("ExitCode(%q) = %d, want %d", tc.reason, got, tc.code)
		}
	}
}

func TestEscalationMessage(t *testing.T) {
	msg := EscalationMessage(EscalationTimeout, "rounds=3")
	if !strings.Contains(msg, "NEGOTIATION_TIMEOUT") {
		t.Errorf("unexpected message: %q", msg)
	}

	msg = EscalationMessage(EscalationBudgetExhausted, "agent=a1 remaining=$0.50")
	if !strings.Contains(msg, "BUDGET_EXHAUSTED") {
		t.Errorf("unexpected message: %q", msg)
	}

	msg = EscalationMessage(EscalationChimeraUnavailable, "connection refused")
	if !strings.Contains(msg, "CHIMERA_UNAVAILABLE") {
		t.Errorf("unexpected message: %q", msg)
	}
}

func TestIsEscalatable(t *testing.T) {
	escalatable := []State{
		StateDeadlock, StateRound1, StateRound2, StateRound3,
		StateConflictDetected, StateChimeraTiebreak,
	}
	for _, s := range escalatable {
		if !IsEscalatable(s) {
			t.Errorf("state %q should be escalatable", s)
		}
	}

	notEscalatable := []State{StateIdle, StateResolved, StateEscalated}
	for _, s := range notEscalatable {
		if IsEscalatable(s) {
			t.Errorf("state %q should not be escalatable", s)
		}
	}
}

func TestEscalationFromNegotiator(t *testing.T) {
	n := &Negotiator{
		Neg: &Negotiation{
			PRNumber: 42,
			AgentA:   Agent{Name: "alpha", TrustLevel: 80},
			AgentB:   Agent{Name: "beta", TrustLevel: 60},
			VerdictA: VerdictApproved,
			VerdictB: VerdictRequestChanges,
			State:    StateDeadlock,
			Round:    3,
		},
	}

	data := EscalationFromNegotiator(n, EscalationTimeout)

	if data.Reason != EscalationTimeout {
		t.Errorf("Reason = %q", data.Reason)
	}
	if data.AgentA.Name != "alpha" {
		t.Errorf("AgentA = %q", data.AgentA.Name)
	}
	if data.RoundsCompleted != 3 {
		t.Errorf("RoundsCompleted = %d", data.RoundsCompleted)
	}
	if !data.Deadlocked {
		t.Error("should be deadlocked")
	}
	if data.AgentAPosition.Verdict != "APPROVED" {
		t.Errorf("AgentAPosition.Verdict = %q", data.AgentAPosition.Verdict)
	}
	if data.AgentBPosition.Verdict != "REQUEST_CHANGES" {
		t.Errorf("AgentBPosition.Verdict = %q", data.AgentBPosition.Verdict)
	}
}

func TestEscalationFromNegotiator_WithChimeraResult(t *testing.T) {
	n := &Negotiator{
		Neg: &Negotiation{
			AgentA: Agent{Name: "a", TrustLevel: 70},
			AgentB: Agent{Name: "b", TrustLevel: 50},
			State:  StateChimeraTiebreak,
			Round:  3,
		},
		ChimeraResult: &ChimeraVerdict{
			Verdict:    "REJECT",
			Confidence: 0.65,
		},
	}

	data := EscalationFromNegotiator(n, EscalationChimeraUnavailable)

	if data.ChimeraPrelim == nil {
		t.Fatal("ChimeraPrelim should not be nil")
	}
	if data.ChimeraPrelim.Verdict != "REJECT" {
		t.Errorf("ChimeraPrelim.Verdict = %q", data.ChimeraPrelim.Verdict)
	}
}

func TestEscalationFromNegotiator_WithAudit(t *testing.T) {
	n := &Negotiator{
		Neg: &Negotiation{
			AgentA: Agent{Name: "a", TrustLevel: 70},
			AgentB: Agent{Name: "b", TrustLevel: 50},
			State:  StateRound2,
			Round:  2,
		},
		Audit: &AuditLogger{path: "/tmp/test-audit.jsonl"},
	}

	data := EscalationFromNegotiator(n, EscalationTimeout)

	if data.DebateLogPath != "/tmp/test-audit.jsonl" {
		t.Errorf("DebateLogPath = %q", data.DebateLogPath)
	}
}

func TestBoolYesNo(t *testing.T) {
	if boolYesNo(true) != "yes" {
		t.Error("boolYesNo(true) should be 'yes'")
	}
	if boolYesNo(false) != "no" {
		t.Error("boolYesNo(false) should be 'no'")
	}
}

func TestEscalationMessage_UnknownReason(t *testing.T) {
	msg := EscalationMessage(EscalationReason("unknown"), "something")
	if !strings.Contains(msg, "INVALID_STATE") {
		t.Errorf("unexpected message: %q", msg)
	}
}

func TestEscalationTimestamp(t *testing.T) {
	ts := EscalationTimestamp()
	if ts == "" {
		t.Error("timestamp should not be empty")
	}
	// Should be RFC3339 format.
	if !strings.Contains(ts, "T") {
		t.Error("timestamp should be RFC3339 format")
	}
}

func TestFormatEscalationComment_FullTemplate(t *testing.T) {
	// Verify the comment matches the spec §12.2 template structure.
	data := &EscalationData{
		Reason:          EscalationTimeout,
		AgentA:          Agent{Name: "wojons", TrustLevel: 85},
		AgentB:          Agent{Name: "llopez", TrustLevel: 52},
		RoundsCompleted: 3,
		Deadlocked:      true,
		DebateLogPath:   "~/.helix/negotiations/44-timestamp.jsonl",
		AgentAPosition:  Position{Agent: "wojons", Verdict: "APPROVED", Evidence: "CI supports Docker as of PR #41"},
		AgentBPosition:  Position{Agent: "llopez", Verdict: "REQUEST_CHANGES", Evidence: "Testcontainers is isolated"},
	}

	comment := FormatEscalationComment(data)

	// Check all template elements from spec §12.2.
	required := []string{
		"Negotiation Escalated",
		"Human Review Required",
		"Reason:",
		"timeout",
		"Agents:",
		"@wojons",
		"@llopez",
		"trust=85",
		"trust=52",
		"Rounds completed:",
		"3/3",
		"Deadlock:",
		"Debate log:",
		"position:",
		"APPROVED",
		"REQUEST_CHANGES",
		"Recommended action:",
	}
	for _, r := range required {
		if !strings.Contains(comment, r) {
			t.Errorf("comment missing required element: %q\nComment:\n%s", r, comment)
		}
	}
}
