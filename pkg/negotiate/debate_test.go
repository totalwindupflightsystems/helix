package negotiate

import (
	"strings"
	"testing"
)

// TestNewDebate verifies the constructor returns a fully initialized Debate.
func TestNewDebate(t *testing.T) {
	neg := &Negotiation{PRNumber: 42}
	d := NewDebate(neg)
	if d == nil {
		t.Fatal("NewDebate returned nil")
	}
	if d.Neg == nil {
		t.Error("Debate.Neg is nil, want non-nil")
	}
	if d.Neg != neg {
		t.Error("Debate.Neg should be the pointer passed in")
	}
	if d.Rounds == nil {
		t.Error("Debate.Rounds is nil, want empty slice")
	}
	if len(d.Rounds) != 0 {
		t.Errorf("Debate.Rounds len = %d, want 0", len(d.Rounds))
	}
	if d.Strikes == nil {
		t.Error("Debate.Strikes is nil, want initialized map")
	}
}

// TestPostArgument_valid verifies a fully spec-compliant argument is recorded.
func TestPostArgument_valid(t *testing.T) {
	neg := &Negotiation{PRNumber: 1, Round: 1}
	d := NewDebate(neg)
	body := "per spec §7.2 this is correct"
	err := d.PostArgument("alfa", body, 2)
	if err != nil {
		t.Fatalf("PostArgument returned unexpected error: %v", err)
	}
	if len(d.Rounds) != 1 {
		t.Fatalf("Rounds len = %d, want 1", len(d.Rounds))
	}
	r := d.Rounds[0]
	if r.Agent != "alfa" {
		t.Errorf("Rounds[0].Agent = %q, want %q", r.Agent, "alfa")
	}
	if r.Body != body {
		t.Errorf("Rounds[0].Body = %q, want %q", r.Body, body)
	}
	if r.Evidence != 2 {
		t.Errorf("Rounds[0].Evidence = %d, want 2", r.Evidence)
	}
	if r.Number != 1 {
		t.Errorf("Rounds[0].Number = %d, want 1", r.Number)
	}
	if d.Strikes["alfa"] != 0 {
		t.Errorf("Strikes[alfa] = %d, want 0 for valid argument", d.Strikes["alfa"])
	}
}

// TestPostArgument_few_evidence verifies evidence count < 2 triggers an error and a strike.
func TestPostArgument_few_evidence(t *testing.T) {
	neg := &Negotiation{Round: 1}
	d := NewDebate(neg)
	err := d.PostArgument("alfa", "per spec §7.2 this is correct", 1)
	if err == nil {
		t.Fatal("PostArgument with evidenceCount=1 should return error")
	}
	if !strings.Contains(err.Error(), "EVIDENCE_REQUIRED") {
		t.Errorf("error %q should contain EVIDENCE_REQUIRED", err.Error())
	}
	if d.Strikes["alfa"] != 1 {
		t.Errorf("Strikes[alfa] = %d, want 1", d.Strikes["alfa"])
	}
	if len(d.Rounds) != 0 {
		t.Errorf("Rounds len = %d, want 0 (failed post must not record)", len(d.Rounds))
	}
}

// TestPostArgument_no_spec_ref verifies missing spec/test reference triggers an error and a strike.
func TestPostArgument_no_spec_ref(t *testing.T) {
	neg := &Negotiation{Round: 1}
	d := NewDebate(neg)
	err := d.PostArgument("alfa", "just a comment no references", 2)
	if err == nil {
		t.Fatal("PostArgument with no spec reference should return error")
	}
	if !strings.Contains(err.Error(), "EVIDENCE_REQUIRED") {
		t.Errorf("error %q should contain EVIDENCE_REQUIRED", err.Error())
	}
	if d.Strikes["alfa"] != 1 {
		t.Errorf("Strikes[alfa] = %d, want 1", d.Strikes["alfa"])
	}
}

// TestPostArgument_strike_accumulation verifies strikes accumulate across multiple bad posts.
func TestPostArgument_strike_accumulation(t *testing.T) {
	neg := &Negotiation{Round: 1}
	d := NewDebate(neg)
	if err := d.PostArgument("alfa", "just a comment", 2); err == nil {
		t.Fatal("first bad post should error")
	}
	if err := d.PostArgument("alfa", "another comment", 1); err == nil {
		t.Fatal("second bad post should error")
	}
	if d.Strikes["alfa"] != 2 {
		t.Errorf("Strikes[alfa] = %d, want 2 after two failed posts", d.Strikes["alfa"])
	}
}

// TestCheckConcession_true verifies a body containing "CONCEDE:" is detected.
func TestCheckConcession_true(t *testing.T) {
	if !CheckConcession("I CONCEDE: you are right") {
		t.Error("CheckConcession(\"I CONCEDE: you are right\") = false, want true")
	}
}

// TestCheckConcession_false verifies a body without "CONCEDE:" returns false.
func TestCheckConcession_false(t *testing.T) {
	if CheckConcession("I disagree") {
		t.Error("CheckConcession(\"I disagree\") = true, want false")
	}
}

// TestCheckConcession_substring verifies CONCEDE: matching is substring-based.
func TestCheckConcession_substring(t *testing.T) {
	if !CheckConcession("cannot CONCEDE: prefix") {
		t.Error("CheckConcession should match \"CONCEDE:\" as substring (spec §7.3)")
	}
}

// TestAdvanceRound_round1 verifies advancing from round 1 increments to round 2 and reports more rounds remain.
func TestAdvanceRound_round1(t *testing.T) {
	neg := &Negotiation{Round: 1}
	d := NewDebate(neg)
	if got := d.AdvanceRound(); !got {
		t.Error("AdvanceRound from round 1 should return true (round 2 <= 3)")
	}
	if d.Neg.Round != 2 {
		t.Errorf("Neg.Round = %d, want 2", d.Neg.Round)
	}
}

// TestAdvanceRound_round3 verifies advancing from round 3 increments to round 4 and reports no rounds remain.
func TestAdvanceRound_round3(t *testing.T) {
	neg := &Negotiation{Round: 3}
	d := NewDebate(neg)
	if got := d.AdvanceRound(); got {
		t.Error("AdvanceRound from round 3 should return false (round 4 > 3)")
	}
	if d.Neg.Round != 4 {
		t.Errorf("Neg.Round = %d, want 4", d.Neg.Round)
	}
}

// TestAdvanceRound_overflow verifies advancing past maxRounds always returns false.
func TestAdvanceRound_overflow(t *testing.T) {
	neg := &Negotiation{Round: 5}
	d := NewDebate(neg)
	if got := d.AdvanceRound(); got {
		t.Error("AdvanceRound from round 5 should return false")
	}
}

// TestIsDeadlocked_true verifies deadlock at round 3 with StateDeadlock.
func TestIsDeadlocked_true(t *testing.T) {
	neg := &Negotiation{Round: 3, State: StateDeadlock}
	d := NewDebate(neg)
	if !d.IsDeadlocked() {
		t.Error("IsDeadlocked() = false, want true (Round=3 State=StateDeadlock)")
	}
}

// TestIsDeadlocked_wrong_state verifies the state must be StateDeadlock.
func TestIsDeadlocked_wrong_state(t *testing.T) {
	neg := &Negotiation{Round: 3, State: StateRound3}
	d := NewDebate(neg)
	if d.IsDeadlocked() {
		t.Error("IsDeadlocked() = true, want false (state is StateRound3, not StateDeadlock)")
	}
}

// TestIsDeadlocked_low_round verifies round must be >= maxRounds.
func TestIsDeadlocked_low_round(t *testing.T) {
	neg := &Negotiation{Round: 2, State: StateDeadlock}
	d := NewDebate(neg)
	if d.IsDeadlocked() {
		t.Error("IsDeadlocked() = true, want false (round=2 < 3)")
	}
}

// TestStrike_increment verifies a single Strike call increments from 0 to 1.
func TestStrike_increment(t *testing.T) {
	neg := &Negotiation{}
	d := NewDebate(neg)
	if got := d.Strike("alfa"); got != 1 {
		t.Errorf("Strike(\"alfa\") = %d, want 1", got)
	}
	if d.Strikes["alfa"] != 1 {
		t.Errorf("Strikes[alfa] = %d, want 1", d.Strikes["alfa"])
	}
}

// TestStrike_three verifies three Strike calls accumulate to 3 (auto-concede threshold).
func TestStrike_three(t *testing.T) {
	neg := &Negotiation{}
	d := NewDebate(neg)
	var lastReturn int
	for i := 0; i < 3; i++ {
		lastReturn = d.Strike("alfa")
	}
	if lastReturn != 3 {
		t.Errorf("third Strike return = %d, want 3", lastReturn)
	}
	if d.Strikes["alfa"] != 3 {
		t.Errorf("Strikes[alfa] = %d, want 3 after three Strike calls", d.Strikes["alfa"])
	}
}
