package negotiate

import (
	"testing"
	"time"
)

func TestValidateVeto_AllConditionsMet(t *testing.T) {
	agent := Agent{Name: "alice", TrustLevel: 75}
	body := `VETO: spec §8.1 requires 3-model review but only 2 models were used.
Test: go test ./pkg/review/ -run TestMultiModel -v
AC-012 (review_depth) is marked PASS but only 2 models ran instead of 3.`

	result := ValidateVeto(agent, body)
	if !result.Valid {
		t.Errorf("expected valid veto, got errors: %v", result.Errors)
	}
	if result.SpecRef == "" {
		t.Error("expected non-empty SpecRef")
	}
	if result.TestCmd == "" {
		t.Error("expected non-empty TestCmd")
	}
	if result.ACEvidence == "" {
		t.Error("expected non-empty ACEvidence")
	}
}

func TestValidateVeto_LowTrust(t *testing.T) {
	agent := Agent{Name: "rookie", TrustLevel: 50}
	body := `VETO: spec §3.1 says something.
Test: go test ./... 
AC-001 violated`

	result := ValidateVeto(agent, body)
	if result.Valid {
		t.Error("expected invalid veto for low trust agent")
	}
	found := false
	for _, e := range result.Errors {
		if contains(e, "trust_level") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected trust_level error in: %v", result.Errors)
	}
}

func TestValidateVeto_ExactTrust70(t *testing.T) {
	agent := Agent{Name: "border", TrustLevel: 70}
	body := `VETO: spec §4.2 is violated.
Test: go test ./pkg/auth/ -run TestAuth
AC-005 is violated`

	result := ValidateVeto(agent, body)
	// Trust 70 should pass condition 1 — errors should be about other conditions
	for _, e := range result.Errors {
		if contains(e, "trust_level") {
			t.Errorf("trust_level 70 should pass veto, got error: %s", e)
		}
	}
}

func TestValidateVeto_NoSpecRef(t *testing.T) {
	agent := Agent{Name: "bob", TrustLevel: 80}
	body := `VETO: This is wrong.
Test: go test ./...
AC-003 violated`

	result := ValidateVeto(agent, body)
	if result.Valid {
		t.Error("expected invalid veto without spec reference")
	}
	found := false
	for _, e := range result.Errors {
		if contains(e, "spec section") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected spec section error in: %v", result.Errors)
	}
}

func TestValidateVeto_NoTestCommand(t *testing.T) {
	agent := Agent{Name: "bob", TrustLevel: 80}
	body := `VETO: spec §5.1 is violated.
The auth module doesn't handle concurrent access.
AC-007 is violated`

	result := ValidateVeto(agent, body)
	if result.Valid {
		t.Error("expected invalid veto without test command")
	}
	found := false
	for _, e := range result.Errors {
		if contains(e, "reproducible evidence") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected reproducible evidence error in: %v", result.Errors)
	}
}

func TestValidateVeto_NoACRef(t *testing.T) {
	agent := Agent{Name: "bob", TrustLevel: 80}
	body := `VETO: spec §5.1 is violated.
Test: go test ./pkg/auth/ -run TestConcurrent`

	result := ValidateVeto(agent, body)
	if result.Valid {
		t.Error("expected invalid veto without AC reference")
	}
	found := false
	for _, e := range result.Errors {
		if contains(e, "acceptance criterion") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected acceptance criterion error in: %v", result.Errors)
	}
}

func TestValidateVeto_AllConditionsFail(t *testing.T) {
	agent := Agent{Name: "noob", TrustLevel: 30}
	body := `VETO: I don't like this`

	result := ValidateVeto(agent, body)
	if result.Valid {
		t.Error("expected invalid veto")
	}
	if len(result.Errors) < 3 {
		t.Errorf("expected at least 3 errors (spec, test, AC), got %d: %v", len(result.Errors), result.Errors)
	}
}

// --- VetoTracker tests ---

func TestVetoTracker_RecordFrivolousVeto(t *testing.T) {
	tracker := NewVetoTracker()
	now := time.Now()

	tracker.RecordFrivolousVeto("alice", 42, "spec §3", now)

	count := tracker.FrivolousCount("alice", now)
	if count != 1 {
		t.Errorf("expected 1 frivolous veto, got %d", count)
	}
}

func TestVetoTracker_FrivolousCount_WindowExpired(t *testing.T) {
	tracker := NewVetoTracker()
	now := time.Now()

	// Record a veto 100 days ago — outside the 90-day window
	old := now.Add(-100 * 24 * time.Hour)
	tracker.RecordFrivolousVeto("alice", 1, "spec §1", old)

	count := tracker.FrivolousCount("alice", now)
	if count != 0 {
		t.Errorf("expected 0 frivolous vetoes within window, got %d", count)
	}
}

func TestVetoTracker_FrivolousCount_WithinWindow(t *testing.T) {
	tracker := NewVetoTracker()
	now := time.Now()

	// Record a veto 50 days ago — inside the 90-day window
	recent := now.Add(-50 * 24 * time.Hour)
	tracker.RecordFrivolousVeto("alice", 1, "spec §1", recent)

	count := tracker.FrivolousCount("alice", now)
	if count != 1 {
		t.Errorf("expected 1 frivolous veto within window, got %d", count)
	}
}

func TestVetoTracker_ShouldCapTrust_BelowThreshold(t *testing.T) {
	tracker := NewVetoTracker()
	now := time.Now()

	// 2 frivolous vetoes — below threshold of 3
	tracker.RecordFrivolousVeto("alice", 1, "spec §1", now)
	tracker.RecordFrivolousVeto("alice", 2, "spec §2", now)

	if tracker.ShouldCapTrust("alice", now) {
		t.Error("expected no trust cap with only 2 frivolous vetoes")
	}
}

func TestVetoTracker_ShouldCapTrust_AtThreshold(t *testing.T) {
	tracker := NewVetoTracker()
	now := time.Now()

	// 3 frivolous vetoes — exactly at threshold
	tracker.RecordFrivolousVeto("alice", 1, "spec §1", now)
	tracker.RecordFrivolousVeto("alice", 2, "spec §2", now)
	tracker.RecordFrivolousVeto("alice", 3, "spec §3", now)

	if !tracker.ShouldCapTrust("alice", now) {
		t.Error("expected trust cap with 3 frivolous vetoes")
	}
}

func TestVetoTracker_ApplyTrustCap_CapsHighTrust(t *testing.T) {
	tracker := NewVetoTracker()
	now := time.Now()

	tracker.RecordFrivolousVeto("alice", 1, "spec §1", now)
	tracker.RecordFrivolousVeto("alice", 2, "spec §2", now)
	tracker.RecordFrivolousVeto("alice", 3, "spec §3", now)

	result := tracker.ApplyTrustCap("alice", 85, now)
	if result != 69 {
		t.Errorf("expected trust capped to 69, got %d", result)
	}
}

func TestVetoTracker_ApplyTrustCap_NoCapBelowThreshold(t *testing.T) {
	tracker := NewVetoTracker()
	now := time.Now()

	// Only 1 frivolous veto
	tracker.RecordFrivolousVeto("alice", 1, "spec §1", now)

	result := tracker.ApplyTrustCap("alice", 85, now)
	if result != 85 {
		t.Errorf("expected trust unchanged at 85, got %d", result)
	}
}

func TestVetoTracker_ApplyTrustCap_AlreadyBelow69(t *testing.T) {
	tracker := NewVetoTracker()
	now := time.Now()

	tracker.RecordFrivolousVeto("alice", 1, "spec §1", now)
	tracker.RecordFrivolousVeto("alice", 2, "spec §2", now)
	tracker.RecordFrivolousVeto("alice", 3, "spec §3", now)

	// Agent with trust 50 — below the cap, so no change
	result := tracker.ApplyTrustCap("alice", 50, now)
	if result != 50 {
		t.Errorf("expected trust unchanged at 50 (below cap), got %d", result)
	}
}

func TestVetoTracker_DoesNotCountValidVetoes(t *testing.T) {
	tracker := NewVetoTracker()
	now := time.Now()

	tracker.RecordValidVeto("alice", 1, "spec §1", now)
	tracker.RecordValidVeto("alice", 2, "spec §2", now)
	tracker.RecordValidVeto("alice", 3, "spec §3", now)

	count := tracker.FrivolousCount("alice", now)
	if count != 0 {
		t.Errorf("expected 0 frivolous count for valid vetoes, got %d", count)
	}
}

func TestVetoTracker_ConcurrentAccess(t *testing.T) {
	tracker := NewVetoTracker()
	now := time.Now()

	done := make(chan bool)
	for i := 0; i < 50; i++ {
		go func(n int) {
			tracker.RecordFrivolousVeto("concurrent-agent", n, "spec §1", now)
			_ = tracker.FrivolousCount("concurrent-agent", now)
			done <- true
		}(i)
	}
	for i := 0; i < 50; i++ {
		<-done
	}

	count := tracker.FrivolousCount("concurrent-agent", now)
	if count != 50 {
		t.Errorf("expected 50 concurrent frivolous vetoes, got %d", count)
	}
}

func TestVetoTracker_WindowBoundary90Days(t *testing.T) {
	tracker := NewVetoTracker()
	now := time.Now()

	// Exactly 90 days ago — should be included (After checks strictly greater)
	boundary := now.Add(-89 * 24 * time.Hour)
	tracker.RecordFrivolousVeto("alice", 1, "spec §1", boundary)

	count := tracker.FrivolousCount("alice", now)
	if count != 1 {
		t.Errorf("expected 1 at 89 days, got %d", count)
	}
}

// --- Penalty and weight tests ---

func TestFrivolousPenalty(t *testing.T) {
	if FrivolousPenalty() != -5 {
		t.Errorf("expected -5 penalty, got %d", FrivolousPenalty())
	}
}

func TestVetoWeight_TrustBelow90(t *testing.T) {
	agent := Agent{Name: "alice", TrustLevel: 75}
	if VetoWeight(agent) != 1.0 {
		t.Errorf("expected weight 1.0 for trust 75, got %f", VetoWeight(agent))
	}
}

func TestVetoWeight_Trust90Plus(t *testing.T) {
	agent := Agent{Name: "elite", TrustLevel: 92}
	if VetoWeight(agent) != 1.5 {
		t.Errorf("expected weight 1.5 for trust 92, got %f", VetoWeight(agent))
	}
}

func TestVetoWeight_TrustExactly90(t *testing.T) {
	agent := Agent{Name: "border", TrustLevel: 90}
	if VetoWeight(agent) != 1.5 {
		t.Errorf("expected weight 1.5 for trust 90 (boundary), got %f", VetoWeight(agent))
	}
}

// --- Body parsing tests ---

func TestExtractSpecRef_WithSectionMarker(t *testing.T) {
	body := "This violates spec §8.1.2"
	ref := extractSpecRef(body)
	if ref == "" {
		t.Error("expected non-empty spec ref")
	}
}

func TestExtractSpecRef_WithFilePath(t *testing.T) {
	body := "See specs/auth.md §3"
	ref := extractSpecRef(body)
	if ref == "" {
		t.Error("expected non-empty spec ref for file path")
	}
}

func TestExtractSpecRef_Empty(t *testing.T) {
	body := "This is just wrong"
	ref := extractSpecRef(body)
	if ref != "" {
		t.Errorf("expected empty spec ref, got %q", ref)
	}
}

func TestExtractTestCommand_GoTest(t *testing.T) {
	body := "Test: go test ./pkg/auth/ -run TestSession"
	cmd := extractTestCommand(body)
	if cmd == "" {
		t.Error("expected non-empty test command")
	}
}

func TestExtractTestCommand_Pytest(t *testing.T) {
	body := "Test: pytest tests/test_auth.py -v"
	cmd := extractTestCommand(body)
	if cmd == "" {
		t.Error("expected non-empty test command for pytest")
	}
}

func TestExtractTestCommand_Empty(t *testing.T) {
	body := "No test here"
	cmd := extractTestCommand(body)
	if cmd != "" {
		t.Errorf("expected empty test command, got %q", cmd)
	}
}

func TestExtractACEvidence_WithHyphen(t *testing.T) {
	body := "AC-012 is violated"
	ref := extractACEvidence(body)
	if ref == "" {
		t.Error("expected non-empty AC evidence")
	}
}

func TestExtractACEvidence_WithSpace(t *testing.T) {
	body := "AC 007 is violated"
	ref := extractACEvidence(body)
	if ref == "" {
		t.Error("expected non-empty AC evidence for space format")
	}
}

func TestExtractACEvidence_Empty(t *testing.T) {
	body := "No AC reference"
	ref := extractACEvidence(body)
	if ref != "" {
		t.Errorf("expected empty AC evidence, got %q", ref)
	}
}

// --- Full integration scenario ---

func TestVetoTracker_FullLifecycle(t *testing.T) {
	tracker := NewVetoTracker()
	now := time.Now()

	// Agent starts with trust 75, can veto
	agent := Agent{Name: "alice", TrustLevel: 75}

	// First frivolous veto
	tracker.RecordFrivolousVeto(agent.Name, 1, "spec §1", now.Add(-60*24*time.Hour))
	if tracker.ShouldCapTrust(agent.Name, now) {
		t.Error("should not cap trust after 1 frivolous veto")
	}

	// Second frivolous veto
	tracker.RecordFrivolousVeto(agent.Name, 2, "spec §2", now.Add(-30*24*time.Hour))
	if tracker.ShouldCapTrust(agent.Name, now) {
		t.Error("should not cap trust after 2 frivolous vetoes")
	}

	// Third frivolous veto — triggers cap
	tracker.RecordFrivolousVeto(agent.Name, 3, "spec §3", now)

	if !tracker.ShouldCapTrust(agent.Name, now) {
		t.Fatal("should cap trust after 3 frivolous vetoes")
	}

	capped := tracker.ApplyTrustCap(agent.Name, 75, now)
	if capped != 69 {
		t.Errorf("expected trust capped to 69, got %d", capped)
	}

	// Trust cap means the agent loses veto power
	agent.TrustLevel = capped
	if agent.CanVeto() {
		t.Error("agent with capped trust 69 should not be able to veto")
	}
}

func TestVetoTracker_OldVetoesExpire(t *testing.T) {
	tracker := NewVetoTracker()
	now := time.Now()

	// 2 old frivolous vetoes (100 days ago — outside window)
	tracker.RecordFrivolousVeto("alice", 1, "spec §1", now.Add(-100*24*time.Hour))
	tracker.RecordFrivolousVeto("alice", 2, "spec §2", now.Add(-95*24*time.Hour))

	// 1 recent frivolous veto
	tracker.RecordFrivolousVeto("alice", 3, "spec §3", now.Add(-10*24*time.Hour))

	// Only the recent one counts
	count := tracker.FrivolousCount("alice", now)
	if count != 1 {
		t.Errorf("expected 1 count (only recent), got %d", count)
	}

	if tracker.ShouldCapTrust("alice", now) {
		t.Error("should not cap trust — only 1 recent veto")
	}
}

// --- helper ---

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
