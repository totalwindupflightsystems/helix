package coapproval

import (
	"testing"
	"time"
)

// ============================================================================
// Approval Methods
// ============================================================================

func TestApproval_IsExpired(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		timestamp time.Time
		expired   bool
	}{
		{"fresh approval", now.Add(-1 * time.Hour), false},
		{"exactly at expiry", now.Add(-24 * time.Hour), false},
		{"just past expiry", now.Add(-(24*time.Hour + time.Second)), true},
		{"very old", now.Add(-48 * time.Hour), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := Approval{Timestamp: tt.timestamp}
			if got := a.IsExpired(now); got != tt.expired {
				t.Errorf("IsExpired() = %v, want %v", got, tt.expired)
			}
		})
	}
}

func TestApproval_IsValidForCommit(t *testing.T) {
	a := Approval{CommitSHA: "abc123"}
	if !a.IsValidForCommit("abc123") {
		t.Error("expected valid for matching SHA")
	}
	if a.IsValidForCommit("def456") {
		t.Error("expected invalid for different SHA")
	}
}

func TestApproval_IsTrustedAgent(t *testing.T) {
	tests := []struct {
		name    string
		typ     ReviewerType
		trust   int
		trusted bool
	}{
		{"trusted agent", ReviewerAgent, 85, true},
		{"agent at threshold", ReviewerAgent, 70, true},
		{"agent below threshold", ReviewerAgent, 69, false},
		{"human not agent", ReviewerHuman, 100, false},
		{"agent zero trust", ReviewerAgent, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := Approval{Type: tt.typ, TrustScore: tt.trust}
			if got := a.IsTrustedAgent(); got != tt.trusted {
				t.Errorf("IsTrustedAgent() = %v, want %v", got, tt.trusted)
			}
		})
	}
}

func TestApproval_HasVetoPower(t *testing.T) {
	tests := []struct {
		name  string
		trust int
		veto  bool
	}{
		{"trust 95", 95, true},
		{"trust 90", 90, true},
		{"trust 89", 89, false},
		{"trust 70", 70, false},
		{"trust 0", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := Approval{TrustScore: tt.trust}
			if got := a.HasVetoPower(); got != tt.veto {
				t.Errorf("HasVetoPower() = %v, want %v", got, tt.veto)
			}
		})
	}
}

// ============================================================================
// CoApprovalGate — Construction
// ============================================================================

func TestNewCoApprovalGate(t *testing.T) {
	g := NewCoApprovalGate("https://forgejo/pr/42", "abc123")
	if g.PRURL() != "https://forgejo/pr/42" {
		t.Errorf("PRURL = %s", g.PRURL())
	}
	if g.CommitSHA() != "abc123" {
		t.Errorf("CommitSHA = %s", g.CommitSHA())
	}
	if g.ApprovalCount() != 0 {
		t.Error("expected 0 approvals on new gate")
	}
}

// ============================================================================
// CoApprovalGate — RecordApproval
// ============================================================================

func TestCoApprovalGate_RecordApproval(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")

	err := g.RecordApproval(Approval{
		ReviewerID: "human-1",
		Type:       ReviewerHuman,
		TrustScore: 100,
		CommitSHA:  "sha1",
	})
	if err != nil {
		t.Fatalf("RecordApproval failed: %v", err)
	}
	if g.ApprovalCount() != 1 {
		t.Errorf("expected 1 approval, got %d", g.ApprovalCount())
	}
}

func TestCoApprovalGate_RecordApproval_EmptyReviewerID(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	err := g.RecordApproval(Approval{
		ReviewerID: "",
		Type:       ReviewerHuman,
	})
	if err == nil {
		t.Error("expected error for empty reviewer_id")
	}
}

func TestCoApprovalGate_RecordApproval_OverwritesPrevious(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")

	_ = g.RecordApproval(Approval{
		ReviewerID: "agent-1",
		Type:       ReviewerAgent,
		TrustScore: 50,
		CommitSHA:  "sha1",
	})

	// Same reviewer updates their approval with higher trust
	_ = g.RecordApproval(Approval{
		ReviewerID: "agent-1",
		Type:       ReviewerAgent,
		TrustScore: 85,
		CommitSHA:  "sha1",
	})

	if g.ApprovalCount() != 1 {
		t.Errorf("expected 1 approval (overwrite), got %d", g.ApprovalCount())
	}

	a := g.GetApproval("agent-1")
	if a.TrustScore != 85 {
		t.Errorf("expected trust 85, got %d", a.TrustScore)
	}
}

// ============================================================================
// CoApprovalGate — Veto
// ============================================================================

func TestCoApprovalGate_RecordVeto_Success(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")

	// First add a human approval
	_ = g.RecordApproval(Approval{
		ReviewerID: "human-1",
		Type:       ReviewerHuman,
		TrustScore: 100,
		CommitSHA:  "sha1",
	})

	// Agent with veto power vetoes
	err := g.RecordVeto("agent-veteran", ReviewerAgent, 95, "sha1")
	if err != nil {
		t.Fatalf("RecordVeto failed: %v", err)
	}

	result := g.CheckEligibility()
	if !result.IsBlocked() {
		t.Error("expected BLOCKED after veto")
	}
	if !result.Vetoed {
		t.Error("expected Vetoed=true")
	}
	if result.VetoBy != "agent-veteran" {
		t.Errorf("expected veto by agent-veteran, got %s", result.VetoBy)
	}
}

func TestCoApprovalGate_RecordVeto_InsufficientTrust(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	err := g.RecordVeto("agent-low", ReviewerAgent, 50, "sha1")
	if err == nil {
		t.Error("expected error for veto without sufficient trust")
	}
}

func TestCoApprovalGate_RecordVeto_CannotUnveto(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")

	// Veto first
	_ = g.RecordVeto("agent-90", ReviewerAgent, 92, "sha1")

	// Try to un-veto by approving instead
	err := g.RecordApproval(Approval{
		ReviewerID: "agent-90",
		Type:       ReviewerAgent,
		TrustScore: 92,
		CommitSHA:  "sha1",
	})
	if err == nil {
		t.Error("expected error when trying to un-veto")
	}
}

func TestCoApprovalGate_VetoCount(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")

	_ = g.RecordApproval(Approval{ReviewerID: "h1", Type: ReviewerHuman, TrustScore: 100, CommitSHA: "sha1"})
	_ = g.RecordVeto("a1", ReviewerAgent, 91, "sha1")
	_ = g.RecordVeto("a2", ReviewerAgent, 95, "sha1")

	if g.VetoCount() != 2 {
		t.Errorf("expected 2 vetoes, got %d", g.VetoCount())
	}
	if g.ApprovalCount() != 1 {
		t.Errorf("expected 1 approval, got %d", g.ApprovalCount())
	}
}

// ============================================================================
// CoApprovalGate — CheckEligibility
// ============================================================================

func TestCheckEligibility_EmptyGate(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	r := g.CheckEligibility()

	if r.Decision != EligibilityBlocked {
		t.Errorf("expected BLOCKED, got %s", r.Decision)
	}
	if r.HumanOK {
		t.Error("expected HumanOK=false")
	}
	if r.AgentOK {
		t.Error("expected AgentOK=false")
	}
}

func TestCheckEligibility_OnlyHumanApproval(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	_ = g.RecordApproval(Approval{
		ReviewerID: "human-1",
		Type:       ReviewerHuman,
		TrustScore: 100,
		CommitSHA:  "sha1",
	})

	r := g.CheckEligibility()
	if r.Decision != EligibilityNeedsAgent {
		t.Errorf("expected NEEDS_AGENT, got %s", r.Decision)
	}
	if !r.HumanOK {
		t.Error("expected HumanOK=true")
	}
	if r.AgentOK {
		t.Error("expected AgentOK=false")
	}
}

func TestCheckEligibility_OnlyAgentApproval_Trusted(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	_ = g.RecordApproval(Approval{
		ReviewerID: "agent-1",
		Type:       ReviewerAgent,
		TrustScore: 85,
		CommitSHA:  "sha1",
	})

	r := g.CheckEligibility()
	if r.Decision != EligibilityNeedsHuman {
		t.Errorf("expected NEEDS_HUMAN, got %s", r.Decision)
	}
	if r.HumanOK {
		t.Error("expected HumanOK=false")
	}
	if !r.AgentOK {
		t.Error("expected AgentOK=true (trusted agent)")
	}
}

func TestCheckEligibility_BothSatisfied_TrustedAgent(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	_ = g.RecordApproval(Approval{
		ReviewerID: "human-1",
		Type:       ReviewerHuman,
		TrustScore: 100,
		CommitSHA:  "sha1",
	})
	_ = g.RecordApproval(Approval{
		ReviewerID: "agent-1",
		Type:       ReviewerAgent,
		TrustScore: 85,
		CommitSHA:  "sha1",
	})

	r := g.CheckEligibility()
	if !r.IsMergeable() {
		t.Errorf("expected ALLOWED, got %s", r.Decision)
	}
}

func TestCheckEligibility_BothSatisfied_AtThreshold(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	_ = g.RecordApproval(Approval{
		ReviewerID: "human-1",
		Type:       ReviewerHuman,
		TrustScore: 100,
		CommitSHA:  "sha1",
	})
	// Agent at exactly trust 70 (threshold)
	_ = g.RecordApproval(Approval{
		ReviewerID: "agent-1",
		Type:       ReviewerAgent,
		TrustScore: TrustedAgentThreshold,
		CommitSHA:  "sha1",
	})

	r := g.CheckEligibility()
	if !r.IsMergeable() {
		t.Errorf("expected ALLOWED at threshold, got %s", r.Decision)
	}
}

func TestCheckEligibility_UntrustedAgent_Insufficient(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	_ = g.RecordApproval(Approval{
		ReviewerID: "human-1",
		Type:       ReviewerHuman,
		TrustScore: 100,
		CommitSHA:  "sha1",
	})
	// Single untrusted agent (trust 50) — not enough
	_ = g.RecordApproval(Approval{
		ReviewerID: "agent-1",
		Type:       ReviewerAgent,
		TrustScore: 50,
		CommitSHA:  "sha1",
	})

	r := g.CheckEligibility()
	if r.AgentOK {
		t.Error("expected AgentOK=false for single untrusted agent")
	}
	if r.Decision != EligibilityNeedsAgent {
		t.Errorf("expected NEEDS_AGENT, got %s", r.Decision)
	}
}

func TestCheckEligibility_UntrustedAgents_TwoRequired(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	_ = g.RecordApproval(Approval{
		ReviewerID: "human-1",
		Type:       ReviewerHuman,
		TrustScore: 100,
		CommitSHA:  "sha1",
	})
	// Two untrusted agents — collectively sufficient
	_ = g.RecordApproval(Approval{
		ReviewerID: "agent-1",
		Type:       ReviewerAgent,
		TrustScore: 50,
		CommitSHA:  "sha1",
	})
	_ = g.RecordApproval(Approval{
		ReviewerID: "agent-2",
		Type:       ReviewerAgent,
		TrustScore: 40,
		CommitSHA:  "sha1",
	})

	r := g.CheckEligibility()
	if !r.AgentOK {
		t.Error("expected AgentOK=true for 2 untrusted agents")
	}
	if !r.IsMergeable() {
		t.Errorf("expected ALLOWED, got %s", r.Decision)
	}
}

func TestCheckEligibility_Vetoed(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	_ = g.RecordApproval(Approval{
		ReviewerID: "human-1",
		Type:       ReviewerHuman,
		TrustScore: 100,
		CommitSHA:  "sha1",
	})
	_ = g.RecordApproval(Approval{
		ReviewerID: "agent-1",
		Type:       ReviewerAgent,
		TrustScore: 85,
		CommitSHA:  "sha1",
	})
	// Veto overrides everything
	_ = g.RecordVeto("agent-veto", ReviewerAgent, 95, "sha1")

	r := g.CheckEligibility()
	if !r.IsBlocked() {
		t.Error("expected BLOCKED")
	}
	if !r.Vetoed {
		t.Error("expected Vetoed=true")
	}
}

func TestCheckEligibility_ExpiredApprovals(t *testing.T) {
	baseTime := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	g := NewCoApprovalGateWithClock("pr/1", "sha1", func() time.Time {
		return baseTime
	})

	// Approval from 25 hours ago (expired)
	_ = g.RecordApproval(Approval{
		ReviewerID: "human-1",
		Type:       ReviewerHuman,
		TrustScore: 100,
		CommitSHA:  "sha1",
		Timestamp:  baseTime.Add(-25 * time.Hour),
	})
	_ = g.RecordApproval(Approval{
		ReviewerID: "agent-1",
		Type:       ReviewerAgent,
		TrustScore: 85,
		CommitSHA:  "sha1",
		Timestamp:  baseTime.Add(-25 * time.Hour),
	})

	r := g.CheckEligibility()
	// Both expired → BLOCKED
	if r.Decision != EligibilityBlocked {
		t.Errorf("expected BLOCKED for expired approvals, got %s", r.Decision)
	}
}

func TestCheckEligibility_CommitMismatch(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	_ = g.RecordApproval(Approval{
		ReviewerID: "human-1",
		Type:       ReviewerHuman,
		TrustScore: 100,
		CommitSHA:  "sha1",
	})
	_ = g.RecordApproval(Approval{
		ReviewerID: "agent-1",
		Type:       ReviewerAgent,
		TrustScore: 85,
		CommitSHA:  "sha1",
	})

	// New push changes the commit
	g.UpdateCommit("sha2")

	r := g.CheckEligibility()
	// Approvals for sha1 are invalidated
	if r.Decision != EligibilityBlocked {
		t.Errorf("expected BLOCKED after commit change, got %s", r.Decision)
	}
}

func TestCheckEligibility_MultipleHumans(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	_ = g.RecordApproval(Approval{ReviewerID: "human-1", Type: ReviewerHuman, TrustScore: 100, CommitSHA: "sha1"})
	_ = g.RecordApproval(Approval{ReviewerID: "human-2", Type: ReviewerHuman, TrustScore: 100, CommitSHA: "sha1"})
	_ = g.RecordApproval(Approval{ReviewerID: "agent-1", Type: ReviewerAgent, TrustScore: 85, CommitSHA: "sha1"})

	r := g.CheckEligibility()
	if !r.IsMergeable() {
		t.Errorf("expected ALLOWED, got %s", r.Decision)
	}
	if r.HumanCount != 2 {
		t.Errorf("expected 2 humans, got %d", r.HumanCount)
	}
}

func TestCheckEligibility_NeedsAgentReason(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	_ = g.RecordApproval(Approval{ReviewerID: "human-1", Type: ReviewerHuman, TrustScore: 100, CommitSHA: "sha1"})
	_ = g.RecordApproval(Approval{ReviewerID: "agent-1", Type: ReviewerAgent, TrustScore: 50, CommitSHA: "sha1"})

	r := g.CheckEligibility()
	if r.Reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestCheckEligibility_NeedsAgent_NoAgents(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	_ = g.RecordApproval(Approval{ReviewerID: "human-1", Type: ReviewerHuman, TrustScore: 100, CommitSHA: "sha1"})

	r := g.CheckEligibility()
	if r.Decision != EligibilityNeedsAgent {
		t.Errorf("expected NEEDS_AGENT, got %s", r.Decision)
	}
}

// ============================================================================
// CoApprovalGate — IsSatisfied
// ============================================================================

func TestIsSatisfied_True(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	_ = g.RecordApproval(Approval{ReviewerID: "human-1", Type: ReviewerHuman, TrustScore: 100, CommitSHA: "sha1"})
	_ = g.RecordApproval(Approval{ReviewerID: "agent-1", Type: ReviewerAgent, TrustScore: 85, CommitSHA: "sha1"})

	if !g.IsSatisfied() {
		t.Error("expected IsSatisfied=true")
	}
}

func TestIsSatisfied_False(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	_ = g.RecordApproval(Approval{ReviewerID: "human-1", Type: ReviewerHuman, TrustScore: 100, CommitSHA: "sha1"})

	if g.IsSatisfied() {
		t.Error("expected IsSatisfied=false")
	}
}

// ============================================================================
// CoApprovalGate — RemoveApproval
// ============================================================================

func TestRemoveApproval_Exists(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	_ = g.RecordApproval(Approval{ReviewerID: "human-1", Type: ReviewerHuman, TrustScore: 100, CommitSHA: "sha1"})

	if !g.RemoveApproval("human-1") {
		t.Error("expected RemoveApproval=true")
	}
	if g.ApprovalCount() != 0 {
		t.Error("expected 0 approvals after removal")
	}
}

func TestRemoveApproval_NotExists(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	if g.RemoveApproval("nonexistent") {
		t.Error("expected RemoveApproval=false for non-existent reviewer")
	}
}

// ============================================================================
// CoApprovalGate — Reset
// ============================================================================

func TestReset(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	_ = g.RecordApproval(Approval{ReviewerID: "human-1", Type: ReviewerHuman, TrustScore: 100, CommitSHA: "sha1"})
	_ = g.RecordApproval(Approval{ReviewerID: "agent-1", Type: ReviewerAgent, TrustScore: 85, CommitSHA: "sha1"})
	_ = g.RecordVeto("agent-2", ReviewerAgent, 95, "sha1")

	g.Reset()

	if g.ApprovalCount() != 0 {
		t.Errorf("expected 0 approvals after reset, got %d", g.ApprovalCount())
	}
	if g.VetoCount() != 0 {
		t.Errorf("expected 0 vetoes after reset, got %d", g.VetoCount())
	}
}

// ============================================================================
// CoApprovalGate — AllApprovals / AllVetoes
// ============================================================================

func TestAllApprovals(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	_ = g.RecordApproval(Approval{ReviewerID: "human-1", Type: ReviewerHuman, TrustScore: 100, CommitSHA: "sha1"})
	_ = g.RecordApproval(Approval{ReviewerID: "agent-1", Type: ReviewerAgent, TrustScore: 85, CommitSHA: "sha1"})
	_ = g.RecordVeto("agent-2", ReviewerAgent, 95, "sha1")

	approvals := g.AllApprovals()
	if len(approvals) != 2 {
		t.Errorf("expected 2 approvals, got %d", len(approvals))
	}

	vetoes := g.AllVetoes()
	if len(vetoes) != 1 {
		t.Errorf("expected 1 veto, got %d", len(vetoes))
	}
}

func TestAllApprovals_Empty(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	if len(g.AllApprovals()) != 0 {
		t.Error("expected empty approvals slice")
	}
}

func TestAllVetoes_Empty(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	if len(g.AllVetoes()) != 0 {
		t.Error("expected empty vetoes slice")
	}
}

// ============================================================================
// CoApprovalGate — UpdateCommit
// ============================================================================

func TestUpdateCommit(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	_ = g.RecordApproval(Approval{ReviewerID: "human-1", Type: ReviewerHuman, TrustScore: 100, CommitSHA: "sha1"})

	g.UpdateCommit("sha2")

	if g.CommitSHA() != "sha2" {
		t.Errorf("expected sha2, got %s", g.CommitSHA())
	}

	// Old approval for sha1 should not count
	r := g.CheckEligibility()
	if r.HumanCount != 0 {
		t.Errorf("expected 0 valid humans after commit change, got %d", r.HumanCount)
	}
}

// ============================================================================
// CoApprovalGate — GetApproval
// ============================================================================

func TestGetApproval_Exists(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	_ = g.RecordApproval(Approval{ReviewerID: "agent-1", Type: ReviewerAgent, TrustScore: 85, CommitSHA: "sha1"})

	a := g.GetApproval("agent-1")
	if a == nil {
		t.Fatal("expected non-nil approval")
	}
	if a.TrustScore != 85 {
		t.Errorf("expected trust 85, got %d", a.TrustScore)
	}
}

func TestGetApproval_NotExists(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	if g.GetApproval("nonexistent") != nil {
		t.Error("expected nil for non-existent approval")
	}
}

// ============================================================================
// EligibilityResult — Summary
// ============================================================================

func TestEligibilityResult_Summary_Allowed(t *testing.T) {
	r := &EligibilityResult{
		Decision:   EligibilityAllowed,
		HumanCount: 1,
		AgentCount: 1,
	}
	summary := r.Summary()
	if summary == "" {
		t.Error("expected non-empty summary")
	}
}

func TestEligibilityResult_Summary_Blocked(t *testing.T) {
	r := &EligibilityResult{
		Decision: EligibilityBlocked,
		Vetoed:   true,
		VetoBy:   "agent-strict",
	}
	summary := r.Summary()
	if summary == "" {
		t.Error("expected non-empty summary")
	}
}

func TestEligibilityResult_Summary_Incomplete(t *testing.T) {
	r := &EligibilityResult{
		Decision:   EligibilityNeedsAgent,
		Reason:     "Waiting for agent approval",
		HumanCount: 1,
		AgentCount: 0,
	}
	summary := r.Summary()
	if summary == "" {
		t.Error("expected non-empty summary")
	}
}

func TestEligibilityResult_IsMergeable(t *testing.T) {
	tests := []struct {
		decision  MergeEligibility
		mergeable bool
	}{
		{EligibilityAllowed, true},
		{EligibilityBlocked, false},
		{EligibilityNeedsHuman, false},
		{EligibilityNeedsAgent, false},
	}

	for _, tt := range tests {
		r := &EligibilityResult{Decision: tt.decision}
		if got := r.IsMergeable(); got != tt.mergeable {
			t.Errorf("decision %s: IsMergeable() = %v, want %v", tt.decision, got, tt.mergeable)
		}
	}
}

// ============================================================================
// RecordApproval with auto-timestamp
// ============================================================================

func TestRecordApproval_AutoTimestamp(t *testing.T) {
	baseTime := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	g := NewCoApprovalGateWithClock("pr/1", "sha1", func() time.Time {
		return baseTime
	})

	// Approval without timestamp — should get auto-set
	_ = g.RecordApproval(Approval{
		ReviewerID: "human-1",
		Type:       ReviewerHuman,
		TrustScore: 100,
		CommitSHA:  "sha1",
		// Timestamp is zero
	})

	a := g.GetApproval("human-1")
	if !a.Timestamp.Equal(baseTime) {
		t.Errorf("expected timestamp %v, got %v", baseTime, a.Timestamp)
	}
}

// ============================================================================
// Integration: Full lifecycle
// ============================================================================

func TestFullLifecycle(t *testing.T) {
	g := NewCoApprovalGate("https://forgejo/pr/42", "abc123")

	// Initially blocked
	r := g.CheckEligibility()
	if r.Decision != EligibilityBlocked {
		t.Fatalf("initial: expected BLOCKED, got %s", r.Decision)
	}

	// Human approves
	_ = g.RecordApproval(Approval{
		ReviewerID: "human-1",
		Type:       ReviewerHuman,
		TrustScore: 100,
		CommitSHA:  "abc123",
	})
	r = g.CheckEligibility()
	if r.Decision != EligibilityNeedsAgent {
		t.Errorf("after human: expected NEEDS_AGENT, got %s", r.Decision)
	}

	// Agent with sufficient trust approves
	_ = g.RecordApproval(Approval{
		ReviewerID: "agent-7",
		Type:       ReviewerAgent,
		TrustScore: 75,
		CommitSHA:  "abc123",
	})
	r = g.CheckEligibility()
	if !r.IsMergeable() {
		t.Errorf("after agent: expected ALLOWED, got %s", r.Decision)
	}

	// New push invalidates approvals
	g.UpdateCommit("def456")
	r = g.CheckEligibility()
	if r.Decision != EligibilityBlocked {
		t.Errorf("after push: expected BLOCKED, got %s", r.Decision)
	}

	// Re-approve with new SHA
	_ = g.RecordApproval(Approval{
		ReviewerID: "human-1",
		Type:       ReviewerHuman,
		TrustScore: 100,
		CommitSHA:  "def456",
	})
	_ = g.RecordApproval(Approval{
		ReviewerID: "agent-7",
		Type:       ReviewerAgent,
		TrustScore: 75,
		CommitSHA:  "def456",
	})
	r = g.CheckEligibility()
	if !r.IsMergeable() {
		t.Errorf("re-approve: expected ALLOWED, got %s", r.Decision)
	}
}

func TestMixedTrustedAndUntrustedAgents(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	_ = g.RecordApproval(Approval{ReviewerID: "human-1", Type: ReviewerHuman, TrustScore: 100, CommitSHA: "sha1"})
	// 1 trusted + 1 untrusted — trusted alone satisfies agent side
	_ = g.RecordApproval(Approval{ReviewerID: "agent-trusted", Type: ReviewerAgent, TrustScore: 80, CommitSHA: "sha1"})
	_ = g.RecordApproval(Approval{ReviewerID: "agent-untrusted", Type: ReviewerAgent, TrustScore: 50, CommitSHA: "sha1"})

	r := g.CheckEligibility()
	if !r.IsMergeable() {
		t.Errorf("expected ALLOWED, got %s", r.Decision)
	}
	if r.AgentCount != 2 {
		t.Errorf("expected 2 agents, got %d", r.AgentCount)
	}
}

func TestHumanVeto(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")
	_ = g.RecordApproval(Approval{ReviewerID: "human-1", Type: ReviewerHuman, TrustScore: 100, CommitSHA: "sha1"})
	_ = g.RecordApproval(Approval{ReviewerID: "agent-1", Type: ReviewerAgent, TrustScore: 85, CommitSHA: "sha1"})

	// Human with veto power (trust 100 >= 90)
	err := g.RecordVeto("human-reviewer", ReviewerHuman, 100, "sha1")
	if err != nil {
		t.Fatalf("RecordVeto failed: %v", err)
	}

	r := g.CheckEligibility()
	if !r.IsBlocked() {
		t.Error("expected BLOCKED after human veto")
	}
}

func TestConcurrentApprovals(t *testing.T) {
	g := NewCoApprovalGate("pr/1", "sha1")

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			_ = g.RecordApproval(Approval{
				ReviewerID: "agent-" + string(rune('A'+idx)),
				Type:       ReviewerAgent,
				TrustScore: 80,
				CommitSHA:  "sha1",
			})
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	if g.ApprovalCount() != 10 {
		t.Errorf("expected 10 approvals, got %d", g.ApprovalCount())
	}
}
