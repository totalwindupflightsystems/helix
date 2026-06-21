package negotiate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// DetectConflict
// =============================================================================

func TestDetectConflict(t *testing.T) {
	tests := []struct {
		name     string
		a, b     Verdict
		want     bool
	}{
		{name: "different_verdicts", a: VerdictApproved, b: VerdictRequestChanges, want: true},
		{name: "same_verdicts_approved", a: VerdictApproved, b: VerdictApproved, want: false},
		{name: "same_verdicts_request_changes", a: VerdictRequestChanges, b: VerdictRequestChanges, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectConflict(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("DetectConflict(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// =============================================================================
// IsVeto
// =============================================================================

func TestIsVeto(t *testing.T) {
	highTrust := Agent{Name: "trusted", TrustLevel: 80}
	lowTrust := Agent{Name: "untrusted", TrustLevel: 10}
	midTrust := Agent{Name: "borderline", TrustLevel: 70}

	tests := []struct {
		name  string
		agent Agent
		body  string
		want  bool
	}{
		{name: "veto_high_trust", agent: highTrust, body: "VETO: This breaks security", want: true},
		{name: "veto_low_trust", agent: lowTrust, body: "VETO: Objection", want: false},
		{name: "veto_exact_boundary_70", agent: midTrust, body: "VETO: Bad design", want: true},
		{name: "no_veto_prefix", agent: highTrust, body: "I object to this", want: false},
		{name: "veto_whitespace_before", agent: highTrust, body: "  VETO: With spaces", want: true},
		{name: "veto_lowercase", agent: highTrust, body: "veto: not uppercase", want: false},
		{name: "empty_body", agent: highTrust, body: "", want: false},
		{name: "only_veto", agent: highTrust, body: "VETO:", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsVeto(tt.agent, tt.body)
			if got != tt.want {
				t.Errorf("IsVeto(%+v, %q) = %v, want %v", tt.agent, tt.body, got, tt.want)
			}
		})
	}
}

// =============================================================================
// NewNegotiator
// =============================================================================

func TestNewNegotiator(t *testing.T) {
	agentA := Agent{Name: "agent-a", TrustLevel: 50}
	agentB := Agent{Name: "agent-b", TrustLevel: 50}

	t.Run("creates_negotiator_with_conflicting_verdicts", func(t *testing.T) {
		dir := t.TempDir()
		auditPath := filepath.Join(dir, "audit.jsonl")

		n, err := NewNegotiator(42, agentA, agentB, VerdictApproved, VerdictRequestChanges,
			"http://localhost:8765", auditPath)
		if err != nil {
			t.Fatalf("NewNegotiator error: %v", err)
		}
		defer n.Audit.Close()

		if n.Neg.PRNumber != 42 {
			t.Errorf("PRNumber = %d, want 42", n.Neg.PRNumber)
		}
		if n.Neg.State != StateConflictDetected {
			t.Errorf("State = %s, want conflict_detected", n.Neg.State)
		}
		if n.Neg.Round != 0 {
			t.Errorf("Round = %d, want 0", n.Neg.Round)
		}
		if n.Neg.VerdictA != VerdictApproved {
			t.Errorf("VerdictA = %s, want APPROVED", n.Neg.VerdictA)
		}
		if n.Neg.VerdictB != VerdictRequestChanges {
			t.Errorf("VerdictB = %s, want REQUEST_CHANGES", n.Neg.VerdictB)
		}
		if n.Arbiter == nil {
			t.Error("Arbiter is nil")
		}
		if n.Debate == nil {
			t.Error("Debate is nil")
		}
		if n.Audit == nil {
			t.Error("Audit is nil")
		}
		if n.config.ChimeraURL != "http://localhost:8765" {
			t.Errorf("config.ChimeraURL = %q, want http://localhost:8765", n.config.ChimeraURL)
		}
		if n.config.MaxRounds != 3 {
			t.Errorf("config.MaxRounds = %d, want 3", n.config.MaxRounds)
		}
	})

	t.Run("no_conflict_error", func(t *testing.T) {
		dir := t.TempDir()
		auditPath := filepath.Join(dir, "audit.jsonl")

		_, err := NewNegotiator(1, agentA, agentB, VerdictApproved, VerdictApproved,
			"http://localhost:8765", auditPath)
		if err == nil {
			t.Fatal("expected error for no conflict, got nil")
		}
		if !strings.Contains(err.Error(), "INVALID_STATE") {
			t.Errorf("error should contain INVALID_STATE, got: %v", err)
		}
		if !strings.Contains(err.Error(), "APPROVED") {
			t.Errorf("error should mention verdict, got: %v", err)
		}
	})

	t.Run("bad_audit_path", func(t *testing.T) {
		auditPath := "/nonexistent/root/owned/dir/audit.jsonl"
		_, err := NewNegotiator(1, agentA, agentB, VerdictApproved, VerdictRequestChanges,
			"http://localhost:8765", auditPath)
		if err == nil {
			t.Fatal("expected error for bad audit path, got nil")
		}
	})
}

// =============================================================================
// NewNegotiatorFromConfig
// =============================================================================

func TestNewNegotiatorFromConfig(t *testing.T) {
	t.Run("creates_from_config_with_defaults", func(t *testing.T) {
		dir := t.TempDir()
		cfg := NegotiationConfig{
			ChimeraURL: "http://localhost:8765",
			MaxRounds:  0, // zero triggers default (3)
			Timeout:    0, // zero triggers default (30m)
			AuditPath:  filepath.Join(dir, "audit.jsonl"),
		}
		pr := PRContext{
			PRNumber: 99,
			AgentPositions: []Position{
				{Agent: "agent-a", Verdict: "APPROVED", Evidence: "tests pass"},
				{Agent: "agent-b", Verdict: "REQUEST_CHANGES", Evidence: "missing error handling"},
			},
			SpecRef: "specs/pr-negotiation.md",
		}

		n, err := NewNegotiatorFromConfig(cfg, pr)
		if err != nil {
			t.Fatalf("NewNegotiatorFromConfig error: %v", err)
		}
		defer n.Audit.Close()

		if n.config.MaxRounds != 3 {
			t.Errorf("MaxRounds default = %d, want 3", n.config.MaxRounds)
		}
		if n.config.Timeout != 30*time.Minute {
			t.Errorf("Timeout default = %v, want 30m", n.config.Timeout)
		}
		if n.Neg.PRNumber != 99 {
			t.Errorf("PRNumber = %d, want 99", n.Neg.PRNumber)
		}
		if n.Neg.State != StateConflictDetected {
			t.Errorf("State = %s, want conflict_detected", n.Neg.State)
		}
	})

	t.Run("no_conflict_error_from_config", func(t *testing.T) {
		dir := t.TempDir()
		cfg := NegotiationConfig{
			ChimeraURL: "http://localhost:8765",
			MaxRounds:  3,
			AuditPath:  filepath.Join(dir, "audit.jsonl"),
		}
		pr := PRContext{
			PRNumber: 1,
			AgentPositions: []Position{
				{Agent: "a", Verdict: "APPROVED", Evidence: "good"},
				{Agent: "b", Verdict: "APPROVED", Evidence: "great"},
			},
		}

		_, err := NewNegotiatorFromConfig(cfg, pr)
		if err == nil {
			t.Fatal("expected error for no conflict, got nil")
		}
	})

	t.Run("too_few_positions", func(t *testing.T) {
		dir := t.TempDir()
		cfg := NegotiationConfig{
			ChimeraURL: "http://localhost:8765",
			AuditPath:  filepath.Join(dir, "audit.jsonl"),
		}
		pr := PRContext{
			PRNumber: 1,
			AgentPositions: []Position{
				{Agent: "solo", Verdict: "APPROVED", Evidence: "only one"},
			},
		}

		_, err := NewNegotiatorFromConfig(cfg, pr)
		if err == nil {
			t.Fatal("expected error for too few positions, got nil")
		}
		if !strings.Contains(err.Error(), "at least 2") {
			t.Errorf("error should mention 'at least 2', got: %v", err)
		}
	})

	t.Run("keeps_user_provided_config_values", func(t *testing.T) {
		dir := t.TempDir()
		cfg := NegotiationConfig{
			ChimeraURL: "http://custom:9999",
			MaxRounds:  5,
			Timeout:    15 * time.Minute,
			AuditPath:  filepath.Join(dir, "audit.jsonl"),
		}
		pr := PRContext{
			PRNumber: 7,
			AgentPositions: []Position{
				{Agent: "a", Verdict: "APPROVED", Evidence: "ok"},
				{Agent: "b", Verdict: "REQUEST_CHANGES", Evidence: "not ok"},
			},
		}

		n, err := NewNegotiatorFromConfig(cfg, pr)
		if err != nil {
			t.Fatalf("NewNegotiatorFromConfig error: %v", err)
		}
		defer n.Audit.Close()

		if n.config.MaxRounds != 5 {
			t.Errorf("MaxRounds = %d, want 5", n.config.MaxRounds)
		}
		if n.config.Timeout != 15*time.Minute {
			t.Errorf("Timeout = %v, want 15m", n.config.Timeout)
		}
		if n.config.ChimeraURL != "http://custom:9999" {
			t.Errorf("ChimeraURL = %q, want http://custom:9999", n.config.ChimeraURL)
		}
	})
}

// =============================================================================
// Advance — state machine
// =============================================================================

func makeTestNegotiator(t *testing.T) (*Negotiator, func()) {
	t.Helper()
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")

	agentA := Agent{Name: "agent-a", TrustLevel: 50}
	agentB := Agent{Name: "agent-b", TrustLevel: 50}

	n, err := NewNegotiator(1, agentA, agentB, VerdictApproved, VerdictRequestChanges,
		"http://localhost:8765", auditPath)
	if err != nil {
		t.Fatalf("NewNegotiator error: %v", err)
	}
	return n, func() { n.Audit.Close() }
}

func TestAdvance_NormalFlow(t *testing.T) {
	t.Run("idle_error", func(t *testing.T) {
		n, cleanup := makeTestNegotiator(t)
		defer cleanup()
		n.Neg.State = StateIdle

		_, err := n.Advance()
		if err == nil {
			t.Fatal("expected error advancing from idle, got nil")
		}
		if !strings.Contains(err.Error(), "cannot advance from idle") {
			t.Errorf("error = %v, want 'cannot advance from idle'", err)
		}
	})

	t.Run("conflict_detected_to_round1", func(t *testing.T) {
		n, cleanup := makeTestNegotiator(t)
		defer cleanup()
		// Starts in StateConflictDetected

		state, err := n.Advance()
		if err != nil {
			t.Fatalf("Advance error: %v", err)
		}
		if state != StateRound1 {
			t.Errorf("state = %s, want round_1", state)
		}
		if n.Neg.Round != 1 {
			t.Errorf("Round = %d, want 1", n.Neg.Round)
		}
	})

	t.Run("round1_to_round2_no_concession", func(t *testing.T) {
		n, cleanup := makeTestNegotiator(t)
		defer cleanup()
		n.Neg.State = StateRound1
		n.Neg.Round = 1
		// No CONCEDE: marker in debate rounds

		state, err := n.Advance()
		if err != nil {
			t.Fatalf("Advance error: %v", err)
		}
		if state != StateRound2 {
			t.Errorf("state = %s, want round_2", state)
		}
		if n.Neg.Round != 2 {
			t.Errorf("Round = %d, want 2", n.Neg.Round)
		}
	})

	t.Run("round1_to_resolved_with_concession", func(t *testing.T) {
		n, cleanup := makeTestNegotiator(t)
		defer cleanup()
		n.Neg.State = StateRound1
		n.Neg.Round = 1
		n.Debate.Rounds = []Round{
			{Number: 1, Agent: "agent-b", Body: "CONCEDE: You're right about the error handling"},
		}

		state, err := n.Advance()
		if err != nil {
			t.Fatalf("Advance error: %v", err)
		}
		if state != StateResolved {
			t.Errorf("state = %s, want resolved", state)
		}
	})

	t.Run("round2_to_round3_no_concession", func(t *testing.T) {
		n, cleanup := makeTestNegotiator(t)
		defer cleanup()
		n.Neg.State = StateRound2
		n.Neg.Round = 2

		state, err := n.Advance()
		if err != nil {
			t.Fatalf("Advance error: %v", err)
		}
		if state != StateRound3 {
			t.Errorf("state = %s, want round_3", state)
		}
		if n.Neg.Round != 3 {
			t.Errorf("Round = %d, want 3", n.Neg.Round)
		}
	})

	t.Run("round2_to_resolved_with_concession", func(t *testing.T) {
		n, cleanup := makeTestNegotiator(t)
		defer cleanup()
		n.Neg.State = StateRound2
		n.Neg.Round = 2
		n.Debate.Rounds = []Round{
			{Number: 2, Agent: "agent-a", Body: "I CONCEDE: good point about the tests"},
		}

		state, err := n.Advance()
		if err != nil {
			t.Fatalf("Advance error: %v", err)
		}
		if state != StateResolved {
			t.Errorf("state = %s, want resolved", state)
		}
	})

	t.Run("round3_to_deadlock_no_concession", func(t *testing.T) {
		n, cleanup := makeTestNegotiator(t)
		defer cleanup()
		n.Neg.State = StateRound3
		n.Neg.Round = 3

		state, err := n.Advance()
		if err != nil {
			t.Fatalf("Advance error: %v", err)
		}
		if state != StateDeadlock {
			t.Errorf("state = %s, want deadlock", state)
		}
	})

	t.Run("round3_to_resolved_with_concession", func(t *testing.T) {
		n, cleanup := makeTestNegotiator(t)
		defer cleanup()
		n.Neg.State = StateRound3
		n.Neg.Round = 3
		n.Debate.Rounds = []Round{
			{Number: 3, Agent: "agent-b", Body: "CONCEDE:"},
		}

		state, err := n.Advance()
		if err != nil {
			t.Fatalf("Advance error: %v", err)
		}
		if state != StateResolved {
			t.Errorf("state = %s, want resolved", state)
		}
	})

	t.Run("deadlock_to_chimera_tiebreak", func(t *testing.T) {
		n, cleanup := makeTestNegotiator(t)
		defer cleanup()
		n.Neg.State = StateDeadlock
		n.Neg.Round = 3

		state, err := n.Advance()
		if err != nil {
			t.Fatalf("Advance error: %v", err)
		}
		if state != StateChimeraTiebreak {
			t.Errorf("state = %s, want chimera_tiebreak", state)
		}
	})

	t.Run("chimera_tiebreak_to_resolved_with_mock_arbiter", func(t *testing.T) {
		// Start an httptest server that returns a valid Chimera response.
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"APPROVE","confidence":0.92,"summary":"approved after arbiter deliberation","trace":{"source":"test","duration":0.1,"total_tokens":500}}`))
		}))
		defer ts.Close()

		n, cleanup := makeTestNegotiator(t)
		defer cleanup()
		n.Neg.State = StateChimeraTiebreak
		n.Neg.Round = 3
		// Replace arbiter client with one pointing at test server
		n.Arbiter = NewArbiterClient(ts.URL)

		state, err := n.Advance()
		if err != nil {
			t.Fatalf("Advance error: %v", err)
		}
		if state != StateResolved {
			t.Errorf("state = %s, want resolved", state)
		}
		if n.ChimeraResult == nil {
			t.Fatal("ChimeraResult is nil after tiebreak")
		}
		if n.ChimeraResult.Verdict != "APPROVE" {
			t.Errorf("ChimeraVerdict.Verdict = %q, want APPROVE", n.ChimeraResult.Verdict)
		}
	})

	t.Run("chimera_tiebreak_escalation_on_error", func(t *testing.T) {
		// Server that returns 503 to trigger Escalate
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer ts.Close()

		n, cleanup := makeTestNegotiator(t)
		defer cleanup()
		n.Neg.State = StateChimeraTiebreak
		n.Arbiter = NewArbiterClient(ts.URL)

		state, err := n.Advance()
		if err == nil {
			t.Fatal("expected error from Chimera escalation, got nil")
		}
		if state != StateEscalated {
			t.Errorf("state = %s, want escalated", state)
		}
		if !strings.Contains(err.Error(), "CHIMERA_UNAVAILABLE") {
			t.Errorf("error should contain CHIMERA_UNAVAILABLE, got: %v", err)
		}
	})

	t.Run("already_resolved_error", func(t *testing.T) {
		n, cleanup := makeTestNegotiator(t)
		defer cleanup()
		n.Neg.State = StateResolved

		_, err := n.Advance()
		if err == nil {
			t.Fatal("expected error from already resolved, got nil")
		}
		if !strings.Contains(err.Error(), "already resolved") {
			t.Errorf("error = %v, want 'already resolved'", err)
		}
	})

	t.Run("already_escalated_error", func(t *testing.T) {
		n, cleanup := makeTestNegotiator(t)
		defer cleanup()
		n.Neg.State = StateEscalated

		_, err := n.Advance()
		if err == nil {
			t.Fatal("expected error from already escalated, got nil")
		}
		if !strings.Contains(err.Error(), "already escalated") {
			t.Errorf("error = %v, want 'already escalated'", err)
		}
	})
}

// =============================================================================
// Escalate
// =============================================================================

func TestEscalate(t *testing.T) {
	t.Run("sets_state_and_logs", func(t *testing.T) {
		n, cleanup := makeTestNegotiator(t)
		defer cleanup()

		err := n.Escalate("human needed for complex conflict")
		if err != nil {
			t.Fatalf("Escalate error: %v", err)
		}
		if n.Neg.State != StateEscalated {
			t.Errorf("State = %s, want escalated", n.Neg.State)
		}

		// Verify audit log was written
		info, statErr := os.Stat(n.config.AuditPath)
		if statErr != nil {
			t.Fatalf("audit file not found: %v", statErr)
		}
		if info.Size() == 0 {
			t.Error("audit file is empty after Escalate")
		}
	})

	t.Run("escalate_with_nil_audit", func(t *testing.T) {
		n, _ := makeTestNegotiator(t)
		// Don't use cleanup — we're nil'ing Audit
		n.Audit.Close()
		n.Audit = nil // remove audit logger

		err := n.Escalate("emergency")
		if err != nil {
			t.Fatalf("Escalate with nil audit error: %v", err)
		}
		if n.Neg.State != StateEscalated {
			t.Errorf("State = %s, want escalated", n.Neg.State)
		}
	})
}

// =============================================================================
// Resolve
// =============================================================================

func TestResolve(t *testing.T) {
	t.Run("sets_state_and_logs", func(t *testing.T) {
		n, cleanup := makeTestNegotiator(t)
		defer cleanup()

		err := n.Resolve(VerdictApproved)
		if err != nil {
			t.Fatalf("Resolve error: %v", err)
		}
		if n.Neg.State != StateResolved {
			t.Errorf("State = %s, want resolved", n.Neg.State)
		}

		info, _ := os.Stat(n.config.AuditPath)
		if info.Size() == 0 {
			t.Error("audit file is empty after Resolve")
		}
	})

	t.Run("resolve_with_nil_audit", func(t *testing.T) {
		n, _ := makeTestNegotiator(t)
		// Don't use cleanup — we're nil'ing Audit
		n.Audit.Close()
		n.Audit = nil

		err := n.Resolve(VerdictRequestChanges)
		if err != nil {
			t.Fatalf("Resolve with nil audit error: %v", err)
		}
		if n.Neg.State != StateResolved {
			t.Errorf("State = %s, want resolved", n.Neg.State)
		}
	})
}

// =============================================================================
// setState (internal, tested via Advance/Escalate/Resolve but also directly)
// =============================================================================

func TestSetState(t *testing.T) {
	n, cleanup := makeTestNegotiator(t)
	defer cleanup()

	n.setState(StateRound2)
	if n.Neg.State != StateRound2 {
		t.Errorf("State = %s, want round_2", n.Neg.State)
	}

	// Audit should have logged the transition
	info, _ := os.Stat(n.config.AuditPath)
	if info.Size() == 0 {
		t.Error("audit file is empty after setState")
	}
}

// =============================================================================
// hasConcession
// =============================================================================

func TestHasConcession(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		n, cleanup := makeTestNegotiator(t)
		defer cleanup()
		n.Debate.Rounds = []Round{
			{Number: 1, Agent: "a", Body: "Objection!"},
			{Number: 2, Agent: "b", Body: "CONCEDE: you're right"},
		}

		if !n.hasConcession() {
			t.Error("hasConcession() = false, want true")
		}
	})

	t.Run("not_found", func(t *testing.T) {
		n, cleanup := makeTestNegotiator(t)
		defer cleanup()
		n.Debate.Rounds = []Round{
			{Number: 1, Agent: "a", Body: "I stand by my position"},
			{Number: 2, Agent: "b", Body: "No concession here"},
		}

		if n.hasConcession() {
			t.Error("hasConcession() = true, want false")
		}
	})

	t.Run("empty_rounds", func(t *testing.T) {
		n, cleanup := makeTestNegotiator(t)
		defer cleanup()

		if n.hasConcession() {
			t.Error("hasConcession() with empty rounds = true, want false")
		}
	})
}

// =============================================================================
// buildArbiterPrompt
// =============================================================================

func TestBuildArbiterPrompt(t *testing.T) {
	n, cleanup := makeTestNegotiator(t)
	defer cleanup()
	n.Neg.VerdictA = VerdictApproved
	n.Neg.VerdictB = VerdictRequestChanges
	n.Debate.Rounds = []Round{
		{Number: 1, Agent: "agent-a", Body: "Tests pass, spec met."},
		{Number: 1, Agent: "agent-b", Body: "Missing error handling."},
	}

	prompt := n.buildArbiterPrompt()

	if !strings.Contains(prompt, "AGENT REVIEWS") {
		t.Error("prompt missing AGENT REVIEWS header")
	}
	if !strings.Contains(prompt, "agent-a") {
		t.Error("prompt missing agent-a")
	}
	if !strings.Contains(prompt, "agent-b") {
		t.Error("prompt missing agent-b")
	}
	if !strings.Contains(prompt, "DEBATE TRANSCRIPT") {
		t.Error("prompt missing DEBATE TRANSCRIPT header")
	}
	if !strings.Contains(prompt, "QUESTION") {
		t.Error("prompt missing QUESTION header")
	}
	if !strings.Contains(prompt, "APPROVE or REJECT") {
		t.Error("prompt missing decision question")
	}
}

// =============================================================================
// buildChimeraPrompt
// =============================================================================

func TestBuildChimeraPrompt(t *testing.T) {
	tests := []struct {
		name      string
		positions []Position
		want      []string
	}{
		{
			name: "two_positions",
			positions: []Position{
				{Agent: "agent-a", Verdict: "APPROVED", Evidence: "all tests pass"},
				{Agent: "agent-b", Verdict: "REQUEST_CHANGES", Evidence: "missing spec reference"},
			},
			want: []string{"AGENT POSITIONS", "agent-a", "agent-b", "APPROVED", "REQUEST_CHANGES", "all tests pass", "missing spec reference", "QUESTION", "APPROVED or REJECTED"},
		},
		{
			name:      "empty_positions",
			positions: []Position{},
			want:      []string{"AGENT POSITIONS", "QUESTION"},
		},
		{
			name: "three_positions",
			positions: []Position{
				{Agent: "a", Verdict: "APPROVED", Evidence: "good"},
				{Agent: "b", Verdict: "APPROVED", Evidence: "great"},
				{Agent: "c", Verdict: "REQUEST_CHANGES", Evidence: "bad"},
			},
			want: []string{"a", "b", "c", "good", "great", "bad"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := buildChimeraPrompt(tt.positions)
			for _, w := range tt.want {
				if !strings.Contains(prompt, w) {
					t.Errorf("prompt missing %q\nprompt:\n%s", w, prompt)
				}
			}
		})
	}
}

// =============================================================================
// allPositionsAgree
// =============================================================================

func TestAllPositionsAgree(t *testing.T) {
	tests := []struct {
		name      string
		positions []Position
		want      bool
	}{
		{name: "empty", positions: []Position{}, want: true},
		{name: "single", positions: []Position{{Verdict: "APPROVED"}}, want: true},
		{name: "all_agree", positions: []Position{{Verdict: "APPROVED"}, {Verdict: "APPROVED"}, {Verdict: "APPROVED"}}, want: true},
		{name: "one_disagrees", positions: []Position{{Verdict: "APPROVED"}, {Verdict: "APPROVED"}, {Verdict: "REQUEST_CHANGES"}}, want: false},
		{name: "all_disagree", positions: []Position{{Verdict: "APPROVED"}, {Verdict: "REQUEST_CHANGES"}, {Verdict: "APPROVED"}}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := allPositionsAgree(tt.positions)
			if got != tt.want {
				t.Errorf("allPositionsAgree(%v) = %v, want %v", tt.positions, got, tt.want)
			}
		})
	}
}

// =============================================================================
// collectPositionEvidence
// =============================================================================

func TestCollectPositionEvidence(t *testing.T) {
	tests := []struct {
		name      string
		positions []Position
		wantLen   int
		want      []string
	}{
		{
			name:      "empty",
			positions: []Position{},
			wantLen:   0,
		},
		{
			name: "all_have_evidence",
			positions: []Position{
				{Evidence: "e1"},
				{Evidence: "e2"},
			},
			wantLen: 2,
			want:    []string{"e1", "e2"},
		},
		{
			name: "some_empty",
			positions: []Position{
				{Evidence: "e1"},
				{Evidence: ""},
				{Evidence: "e3"},
			},
			wantLen: 2,
			want:    []string{"e1", "e3"},
		},
		{
			name: "all_empty",
			positions: []Position{
				{Evidence: ""},
				{Evidence: ""},
			},
			wantLen: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectPositionEvidence(tt.positions)
			if len(got) != tt.wantLen {
				t.Errorf("len = %d, want %d", len(got), tt.wantLen)
			}
			if tt.want != nil {
				for i, w := range tt.want {
					if got[i] != w {
						t.Errorf("evidence[%d] = %q, want %q", i, got[i], w)
					}
				}
			}
		})
	}
}

// =============================================================================
// extractWinningEvidence
// =============================================================================

func TestExtractWinningEvidence(t *testing.T) {
	positions := []Position{
		{Agent: "a", Verdict: "APPROVED", Evidence: "tests pass"},
		{Agent: "b", Verdict: "APPROVED", Evidence: "spec compliance"},
		{Agent: "c", Verdict: "REQUEST_CHANGES", Evidence: "missing handler"},
		{Agent: "d", Verdict: "APPROVED", Evidence: ""},
	}

	t.Run("extract_approved", func(t *testing.T) {
		got := extractWinningEvidence(positions, "APPROVED")
		if len(got) != 2 {
			t.Errorf("len = %d, want 2", len(got))
		}
		if got[0] != "tests pass" {
			t.Errorf("got[0] = %q, want 'tests pass'", got[0])
		}
		if got[1] != "spec compliance" {
			t.Errorf("got[1] = %q, want 'spec compliance'", got[1])
		}
	})

	t.Run("extract_request_changes", func(t *testing.T) {
		got := extractWinningEvidence(positions, "REQUEST_CHANGES")
		if len(got) != 1 {
			t.Errorf("len = %d, want 1", len(got))
		}
		if got[0] != "missing handler" {
			t.Errorf("got[0] = %q, want 'missing handler'", got[0])
		}
	})

	t.Run("extract_nonexistent_verdict", func(t *testing.T) {
		got := extractWinningEvidence(positions, "NONEXISTENT")
		if len(got) != 0 {
			t.Errorf("len = %d, want 0", len(got))
		}
	})

	t.Run("empty_positions", func(t *testing.T) {
		got := extractWinningEvidence([]Position{}, "APPROVED")
		if len(got) != 0 {
			t.Errorf("len = %d, want 0", len(got))
		}
	})
}

// =============================================================================
// Negotiate — full protocol with httptest mock
// =============================================================================

func TestNegotiate(t *testing.T) {
	t.Run("all_agree_immediate_resolution", func(t *testing.T) {
		n, cleanup := makeTestNegotiator(t)
		defer cleanup()

		pr := PRContext{
			PRNumber: 42,
			AgentPositions: []Position{
				{Agent: "agent-a", Verdict: "APPROVED", Evidence: "looks good"},
				{Agent: "agent-b", Verdict: "APPROVED", Evidence: "spec compliant"},
			},
			SpecRef: "specs/pr-negotiation.md",
		}

		resolution, err := n.Negotiate(context.Background(), pr)
		if err != nil {
			t.Fatalf("Negotiate error: %v", err)
		}
		if resolution.Verdict != "APPROVED" {
			t.Errorf("Verdict = %q, want APPROVED", resolution.Verdict)
		}
		if !strings.Contains(resolution.Reasoning, "All 2 agents agree") {
			t.Errorf("Reasoning = %q, want 'All 2 agents agree'", resolution.Reasoning)
		}
		if resolution.TieBreaker != "" {
			t.Errorf("TieBreaker = %q, want empty (no Chimera needed)", resolution.TieBreaker)
		}
		if len(resolution.WinningEvidence) != 2 {
			t.Errorf("winning evidence len = %d, want 2", len(resolution.WinningEvidence))
		}
	})

	t.Run("too_few_positions", func(t *testing.T) {
		n, cleanup := makeTestNegotiator(t)
		defer cleanup()

		pr := PRContext{
			PRNumber: 1,
			AgentPositions: []Position{
				{Agent: "solo", Verdict: "APPROVED"},
			},
		}

		_, err := n.Negotiate(context.Background(), pr)
		if err == nil {
			t.Fatal("expected error for too few positions, got nil")
		}
	})

	t.Run("disagree_escalates_to_chimera", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"REJECT","confidence":0.85,"summary":"rejected due to missing tests","trace":{"source":"test","duration":0.2,"total_tokens":300}}`))
		}))
		defer ts.Close()

		n, cleanup := makeTestNegotiator(t)
		defer cleanup()
		n.Arbiter = NewArbiterClient(ts.URL)

		pr := PRContext{
			PRNumber: 42,
			AgentPositions: []Position{
				{Agent: "agent-a", Verdict: "APPROVED", Evidence: "tests pass"},
				{Agent: "agent-b", Verdict: "REQUEST_CHANGES", Evidence: "no integration tests"},
			},
			SpecRef: "specs/pr-negotiation.md",
		}

		resolution, err := n.Negotiate(context.Background(), pr)
		if err != nil {
			t.Fatalf("Negotiate error: %v", err)
		}
		if resolution.Verdict != "REJECT" {
			t.Errorf("Verdict = %q, want REJECT", resolution.Verdict)
		}
		if resolution.TieBreaker == "" {
			t.Error("TieBreaker should not be empty (Chimera was used)")
		}
		if !strings.Contains(resolution.TieBreaker, "Chimera arbiter") {
			t.Errorf("TieBreaker = %q, should mention Chimera", resolution.TieBreaker)
		}
		if n.Neg.State != StateResolved {
			t.Errorf("State = %s, want resolved", n.Neg.State)
		}
	})
}

// =============================================================================
// EscalateToChimera
// =============================================================================

func TestEscalateToChimera(t *testing.T) {
	t.Run("successful_chimera_call", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"APPROVE","confidence":0.95,"summary":"approved","trace":{"source":"test","duration":0.1,"total_tokens":200}}`))
		}))
		defer ts.Close()

		n, cleanup := makeTestNegotiator(t)
		defer cleanup()
		n.Arbiter = NewArbiterClient(ts.URL)

		positions := []Position{
			{Agent: "a", Verdict: "APPROVED", Evidence: "good"},
			{Agent: "b", Verdict: "REQUEST_CHANGES", Evidence: "bad"},
		}

		verdict, err := n.EscalateToChimera(context.Background(), positions)
		if err != nil {
			t.Fatalf("EscalateToChimera error: %v", err)
		}
		if verdict.Verdict != "APPROVE" {
			t.Errorf("Verdict = %q, want APPROVE", verdict.Verdict)
		}
		if verdict.Confidence != 0.95 {
			t.Errorf("Confidence = %f, want 0.95", verdict.Confidence)
		}
	})

	t.Run("chimera_unavailable", func(t *testing.T) {
		n, cleanup := makeTestNegotiator(t)
		defer cleanup()
		n.Arbiter = NewArbiterClient("http://127.0.0.1:19999") // unreachable

		positions := []Position{
			{Agent: "a", Verdict: "APPROVED"},
			{Agent: "b", Verdict: "REQUEST_CHANGES"},
		}

		_, err := n.EscalateToChimera(context.Background(), positions)
		if err == nil {
			t.Fatal("expected error from unreachable Chimera, got nil")
		}
		if !strings.Contains(err.Error(), "CHIMERA_UNAVAILABLE") {
			t.Errorf("error should contain CHIMERA_UNAVAILABLE, got: %v", err)
		}
	})
}

// =============================================================================
// TransitionTable
// =============================================================================

func TestTransitionTable(t *testing.T) {
	// Verify key transitions are defined
	tests := []struct {
		state     State
		condition string
		want      State
	}{
		{StateIdle, "conflict_detected", StateConflictDetected},
		{StateConflictDetected, "start", StateRound1},
		{StateRound1, "continue", StateRound2},
		{StateRound1, "concede", StateResolved},
		{StateRound2, "continue", StateRound3},
		{StateRound2, "concede", StateResolved},
		{StateRound3, "continue", StateDeadlock},
		{StateRound3, "concede", StateResolved},
		{StateDeadlock, "tiebreak", StateChimeraTiebreak},
		{StateChimeraTiebreak, "resolved", StateResolved},
	}

	for _, tt := range tests {
		t.Run(string(tt.state)+"_"+tt.condition, func(t *testing.T) {
			transitions, ok := TransitionTable[tt.state]
			if !ok {
				t.Fatalf("state %s not in TransitionTable", tt.state)
			}
			got, ok := transitions[tt.condition]
			if !ok {
				t.Fatalf("condition %q not found for state %s", tt.condition, tt.state)
			}
			if got != tt.want {
				t.Errorf("TransitionTable[%s][%q] = %s, want %s", tt.state, tt.condition, got, tt.want)
			}
		})
	}
}

// =============================================================================
// ChimeraVerdict -> Escalate and Resolve (value semantics)
// =============================================================================

func TestEscalateToChimera_VerdictFields(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"APPROVE","confidence":0.72,"summary":"Approved via arbiter","trace":{"source":"chimera","duration":0.55,"total_tokens":1000}}`))
	}))
	defer ts.Close()

	n, cleanup := makeTestNegotiator(t)
	defer cleanup()
	n.Arbiter = NewArbiterClient(ts.URL)

	verdict, err := n.EscalateToChimera(context.Background(), []Position{
		{Agent: "a", Verdict: "APPROVED", Evidence: "evidence"},
		{Agent: "b", Verdict: "REQUEST_CHANGES", Evidence: "counter-evidence"},
	})
	if err != nil {
		t.Fatalf("EscalateToChimera error: %v", err)
	}

	if verdict.Verdict != "APPROVE" {
		t.Errorf("Verdict = %q, want APPROVE", verdict.Verdict)
	}
	if verdict.Confidence != 0.72 {
		t.Errorf("Confidence = %f, want 0.72", verdict.Confidence)
	}
	if verdict.Trace != "Approved via arbiter" {
		t.Errorf("Trace = %q, want 'Approved via arbiter'", verdict.Trace)
	}
	// Cost should be tokens * 0.00000032
	expectedCost := float64(1000) * 0.00000032
	if verdict.Cost != expectedCost {
		t.Errorf("Cost = %f, want %f (1000 tokens * 0.00000032)", verdict.Cost, expectedCost)
	}
}
