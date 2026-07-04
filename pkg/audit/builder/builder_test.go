package builder

import (
	"strings"
	"testing"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/audit"
)

// =============================================================================
// Test helpers
// =============================================================================

// fullBuild assembles a complete AuditEvidence via the builder for
// downstream-validation tests. The timestamps are deterministic so test
// output is stable. Returns the builder so callers can inspect state.
func fullBuild(t *testing.T) *Builder {
	t.Helper()
	ts := time.Date(2026, 7, 3, 21, 30, 0, 0, time.UTC)
	b := New("42")
	b.Issue("https://forgejo.example.com/repo/issues/42", "alice", ts, "Fix bug")
	b.AxiomWorkItem("wi-007", "run-9", "/path/to/plan.yaml", "agent-1", "agent-2")
	b.Lock("lock-3", "/worktree/feat-x", ts)
	b.Session("sess-1", "minimax-m3", "trace-1", 1000, 500, 0.05)
	b.Commit("abc123", "phash", "minimax-m3", "chash", "agent-1", 95, 0.05, true)
	b.Verdict(true, []string{"secrets", "lint", "tests", "build"}, "COMPLETE", 0, ts)
	b.PR(42, "https://forgejo/.../issues/42", "specs/foo.md", "bundle-1")
	b.ChimeraReview("ct-1", "auto", "APPROVE", []string{"minimax-m3", "glm-5.2"}, 0, 0.95)
	b.Conscientiousness("rep-1", "DEFENSIBLE", []string{"injection", "xss"}, 0)
	b.PromptFoo(10, 10, 0, "actions-run-1", []audit.PromptFooResult{
		{TestCase: "test-1", Passed: true, Model: "minimax-m3", Variance: 0.01},
	})
	b.CoApprovals(
		&audit.ApprovalRecord{Reviewer: "alice", TrustLevel: 100, Timestamp: ts},
		&audit.ApprovalRecord{Reviewer: "agent-1", TrustLevel: 80, Timestamp: ts},
	)
	b.Merge("merge-sha", "squash", "https://pages.example.com/pr/42", "trace-final", ts)
	return b
}

// =============================================================================
// New + Build
// =============================================================================

func TestNew_ReturnsBuilderWithEmptyEvidence(t *testing.T) {
	b := New("42")
	if b.prRef != "42" {
		t.Errorf("prRef = %q, want 42", b.prRef)
	}
	if b.ev == nil {
		t.Fatal("ev is nil")
	}
	if b.Build() == nil {
		t.Fatal("Build() returned nil")
	}
	if b.IsComplete() {
		t.Error("new builder should not be complete")
	}
}

func TestBuild_ReturnsUnderlyingEvidence(t *testing.T) {
	b := New("42")
	got := b.Build()
	if got != b.ev {
		t.Error("Build() should return the underlying evidence pointer")
	}
}

// =============================================================================
// Each step's setter
// =============================================================================

func TestIssue_Step1_RecordsEvidence(t *testing.T) {
	ts := time.Now()
	b := New("1").Issue("https://x", "alice", ts, "Title")
	if b.ev.ForgejoIssue == nil {
		t.Fatal("ForgejoIssue is nil")
	}
	if b.ev.ForgejoIssue.IssueURL != "https://x" {
		t.Errorf("IssueURL = %q", b.ev.ForgejoIssue.IssueURL)
	}
}

func TestIssue_NoOpOnZeroValues(t *testing.T) {
	b := New("1").Issue("", "", time.Time{}, "")
	if b.ev.ForgejoIssue != nil {
		t.Error("expected ForgejoIssue to remain nil for zero inputs")
	}
}

func TestAxiomWorkItem_Step2_RecordsEvidence(t *testing.T) {
	b := New("1").AxiomWorkItem("wi-1", "run-1", "plan.yaml", "agent-a", "agent-b")
	if b.ev.AxiomWorkItem == nil {
		t.Fatal("AxiomWorkItem is nil")
	}
	if len(b.ev.AxiomWorkItem.AgentIDs) != 2 {
		t.Errorf("AgentIDs size = %d, want 2", len(b.ev.AxiomWorkItem.AgentIDs))
	}
}

func TestLock_Step3_RecordsEvidence(t *testing.T) {
	ts := time.Now()
	b := New("1").Lock("lock-1", "/worktree", ts)
	if b.ev.RalphLoop == nil {
		t.Fatal("RalphLoop is nil")
	}
	if b.ev.RalphLoop.LockID != "lock-1" {
		t.Errorf("LockID = %q", b.ev.RalphLoop.LockID)
	}
	if !b.ev.RalphLoop.LockAcquiredAt.Equal(ts) {
		t.Errorf("LockAcquiredAt = %v, want %v", b.ev.RalphLoop.LockAcquiredAt, ts)
	}
	if !b.ev.RalphLoop.LockReleasedAt.IsZero() {
		t.Error("LockReleasedAt should be zero until Release() is called")
	}
}

func TestRelease_RecordsReleaseTimestamp(t *testing.T) {
	acq := time.Now()
	rel := acq.Add(time.Minute)
	b := New("1").Lock("l", "/w", acq).Release(rel)
	if !b.ev.RalphLoop.LockReleasedAt.Equal(rel) {
		t.Errorf("LockReleasedAt = %v, want %v", b.ev.RalphLoop.LockReleasedAt, rel)
	}
}

func TestRelease_NoOpWhenNoLockAcquired(t *testing.T) {
	b := New("1").Release(time.Now())
	if b.ev.RalphLoop != nil {
		t.Error("Release should not create a RalphLoop record")
	}
}

func TestSession_Step4_RecordsEvidence(t *testing.T) {
	b := New("1").Session("s1", "minimax-m3", "trace-1", 1000, 500, 0.05)
	if b.ev.OpenCodeSession == nil {
		t.Fatal("OpenCodeSession is nil")
	}
	if b.ev.OpenCodeSession.TokensInput != 1000 {
		t.Errorf("TokensInput = %d", b.ev.OpenCodeSession.TokensInput)
	}
	if b.ev.OpenCodeSession.CostUSD != 0.05 {
		t.Errorf("CostUSD = %v", b.ev.OpenCodeSession.CostUSD)
	}
}

func TestCommit_Step5_RecordsEvidence(t *testing.T) {
	b := New("1").Commit("sha1", "phash", "minimax-m3", "chash", "agent-1", 95, 0.05, true)
	if b.ev.GitCommit == nil {
		t.Fatal("GitCommit is nil")
	}
	if b.ev.GitCommit.SHA != "sha1" {
		t.Errorf("SHA = %q", b.ev.GitCommit.SHA)
	}
	if !b.ev.GitCommit.AttestationFound {
		t.Error("AttestationFound should be true")
	}
	if b.ev.GitCommit.Confidence != 95 {
		t.Errorf("Confidence = %d", b.ev.GitCommit.Confidence)
	}
}

func TestVerdict_Step6_RecordsEvidence(t *testing.T) {
	ts := time.Now()
	b := New("1").Verdict(true, []string{"secrets", "lint"}, "COMPLETE", 0, ts)
	if b.ev.GitReinsVerdict == nil {
		t.Fatal("GitReinsVerdict is nil")
	}
	if !b.ev.GitReinsVerdict.Tier1Passed {
		t.Error("Tier1Passed should be true")
	}
	if b.ev.GitReinsVerdict.Tier2Verdict != "COMPLETE" {
		t.Errorf("Tier2Verdict = %q", b.ev.GitReinsVerdict.Tier2Verdict)
	}
}

func TestPR_Step7_RecordsEvidence(t *testing.T) {
	b := New("1").PR(42, "https://x", "spec.md", "bundle-1")
	if b.ev.PRMetadata == nil {
		t.Fatal("PRMetadata is nil")
	}
	if b.ev.PRMetadata.PRIndex != 42 {
		t.Errorf("PRIndex = %d", b.ev.PRMetadata.PRIndex)
	}
}

func TestChimeraReview_Step8_RecordsEvidence(t *testing.T) {
	b := New("1").ChimeraReview("ct-1", "auto", "APPROVE",
		[]string{"minimax-m3", "glm-5.2"}, 0, 0.95)
	if b.ev.ChimeraReview == nil {
		t.Fatal("ChimeraReview is nil")
	}
	if len(b.ev.ChimeraReview.WorkerModels) != 2 {
		t.Errorf("WorkerModels size = %d", len(b.ev.ChimeraReview.WorkerModels))
	}
	if b.ev.ChimeraReview.Score != 0.95 {
		t.Errorf("Score = %v", b.ev.ChimeraReview.Score)
	}
}

func TestConscientiousness_Step9_RecordsEvidence(t *testing.T) {
	b := New("1").Conscientiousness("rep-1", "DEFENSIBLE",
		[]string{"injection"}, 3)
	if b.ev.Conscientiousness == nil {
		t.Fatal("Conscientiousness is nil")
	}
	if b.ev.Conscientiousness.Mitigations != 3 {
		t.Errorf("Mitigations = %d", b.ev.Conscientiousness.Mitigations)
	}
}

func TestPromptFoo_Step10_RecordsEvidence(t *testing.T) {
	results := []audit.PromptFooResult{{TestCase: "t1", Passed: true}}
	b := New("1").PromptFoo(5, 4, 1, "actions-1", results)
	if b.ev.PromptFooCI == nil {
		t.Fatal("PromptFooCI is nil")
	}
	if b.ev.PromptFooCI.TotalTests != 5 {
		t.Errorf("TotalTests = %d", b.ev.PromptFooCI.TotalTests)
	}
}

func TestCoApprovals_Step11_RecordsEvidence(t *testing.T) {
	ts := time.Now()
	b := New("1").CoApprovals(
		&audit.ApprovalRecord{Reviewer: "alice", TrustLevel: 100, Timestamp: ts},
		&audit.ApprovalRecord{Reviewer: "agent-1", TrustLevel: 80, Timestamp: ts},
	)
	if b.ev.CoApprovals == nil {
		t.Fatal("CoApprovals is nil")
	}
	if b.ev.CoApprovals.HumanApproval == nil || b.ev.CoApprovals.AgentApproval == nil {
		t.Error("expected both HumanApproval and AgentApproval to be set")
	}
}

func TestCoApprovals_BothNil_NoOp(t *testing.T) {
	b := New("1").CoApprovals(nil, nil)
	if b.ev.CoApprovals != nil {
		t.Error("expected CoApprovals to remain nil")
	}
}

func TestMerge_Step12_RecordsEvidence(t *testing.T) {
	ts := time.Now()
	b := New("1").Merge("merge-sha", "squash", "https://pages", "trace-final", ts)
	if b.ev.Merge == nil {
		t.Fatal("Merge is nil")
	}
	if b.ev.Merge.MergeSHA != "merge-sha" {
		t.Errorf("MergeSHA = %q", b.ev.Merge.MergeSHA)
	}
	if b.ev.Merge.PagesURL != "https://pages" {
		t.Errorf("PagesURL = %q", b.ev.Merge.PagesURL)
	}
}

// =============================================================================
// Fluent chaining
// =============================================================================

func TestFluentChain_AllTwelveSteps(t *testing.T) {
	ts := time.Now()
	b := New("42").
		Issue("u", "c", ts, "t").
		AxiomWorkItem("wi", "run", "plan", "a").
		Lock("lock", "/w", ts).
		Session("s", "m", "trace", 1, 1, 0.01).
		Commit("sha", "ph", "m", "ch", "a", 1, 0.01, true).
		Verdict(true, []string{"x"}, "COMPLETE", 0, ts).
		PR(1, "u", "spec", "b").
		ChimeraReview("t", "f", "APPROVE", []string{"m"}, 0, 0.5).
		Conscientiousness("r", "DEFENSIBLE", []string{"v"}, 0).
		PromptFoo(1, 1, 0, "ar", nil).
		CoApprovals(&audit.ApprovalRecord{Reviewer: "h"},
			&audit.ApprovalRecord{Reviewer: "a"}).
		Merge("sha", "squash", "url", "trace", ts)

	if !b.IsComplete() {
		t.Errorf("full chain should be complete; missing: %v", b.MissingSteps())
	}
}

// =============================================================================
// MissingSteps / PresentSteps / Completion / IsComplete
// =============================================================================

func TestMissingSteps_EmptyBuilder_ReturnsAllTwelve(t *testing.T) {
	b := New("1")
	missing := b.MissingSteps()
	if len(missing) != 12 {
		t.Errorf("MissingSteps size = %d, want 12", len(missing))
	}
}

func TestMissingSteps_AfterOneStep_ReturnsEleven(t *testing.T) {
	b := New("1").Issue("u", "c", time.Now(), "t")
	missing := b.MissingSteps()
	if len(missing) != 11 {
		t.Errorf("MissingSteps size = %d, want 11", len(missing))
	}
	if missing[0] != audit.StepForgejoIssue {
		// ForgejoIssue should NOT be in missing.
		for _, m := range missing {
			if m == audit.StepForgejoIssue {
				t.Error("ForgejoIssue should not be in missing")
			}
		}
	}
}

func TestMissingSteps_FullChain_ReturnsEmpty(t *testing.T) {
	_ = fullBuild(t)
	b := New("42")
	ts := time.Now()
	b.Issue("u", "c", ts, "t").
		AxiomWorkItem("wi", "r", "p", "a").
		Lock("l", "/w", ts).
		Session("s", "m", "t", 1, 1, 0.01).
		Commit("s", "p", "m", "c", "a", 1, 0.01, true).
		Verdict(true, []string{"x"}, "COMPLETE", 0, ts).
		PR(1, "u", "s", "b").
		ChimeraReview("t", "f", "APPROVE", []string{"m"}, 0, 0.5).
		Conscientiousness("r", "DEFENSIBLE", []string{"v"}, 0).
		PromptFoo(1, 1, 0, "ar", nil).
		CoApprovals(&audit.ApprovalRecord{Reviewer: "h"},
			&audit.ApprovalRecord{Reviewer: "a"}).
		Merge("s", "sq", "u", "t", ts)
	if !b.IsComplete() {
		t.Errorf("expected complete; missing: %v", b.MissingSteps())
	}
}

func TestPresentSteps_EmptyBuilder_ReturnsEmpty(t *testing.T) {
	b := New("1")
	if got := b.PresentSteps(); len(got) != 0 {
		t.Errorf("PresentSteps size = %d, want 0", len(got))
	}
}

func TestCompletion_ReturnsCorrectRatio(t *testing.T) {
	b := New("1").Issue("u", "c", time.Now(), "t")
	present, total := b.Completion()
	if present != 1 || total != 12 {
		t.Errorf("Completion() = (%d, %d), want (1, 12)", present, total)
	}
}

// =============================================================================
// Integration with audit.AuditEvidence validation
// =============================================================================

func TestBuilder_ProducesValidAuditEvidence(t *testing.T) {
	b := fullBuild(t)
	if !b.IsComplete() {
		t.Errorf("fullBuild should be complete; missing: %v", b.MissingSteps())
	}
	// The produced evidence has all 12 fields populated.
	ev := b.Build()
	if ev.ForgejoIssue == nil || ev.Merge == nil {
		t.Error("expected all 12 step evidence fields populated")
	}
}

// =============================================================================
// FormatProgress
// =============================================================================

func TestFormatProgress_EmptyBuilder_ShowsAllMissing(t *testing.T) {
	b := New("42")
	got := b.FormatProgress()
	// Verify missing steps present.
	if !strings.Contains(got, "Step 1: Forgejo Issue") {
		t.Errorf("missing Step 1 in progress: %s", got)
	}
	if !strings.Contains(got, "Step 12:") {
		t.Errorf("missing Step 12 in progress: %s", got)
	}
	if strings.Contains(got, "all steps present") {
		t.Errorf("empty builder should not say 'all steps present': %s", got)
	}
}

func TestFormatProgress_FullBuilder_ShowsAllPresent(t *testing.T) {
	ts := time.Now()
	b := New("42").
		Issue("u", "c", ts, "t").
		AxiomWorkItem("wi", "r", "p", "a").
		Lock("l", "/w", ts).
		Session("s", "m", "t", 1, 1, 0.01).
		Commit("s", "p", "m", "c", "a", 1, 0.01, true).
		Verdict(true, []string{"x"}, "COMPLETE", 0, ts).
		PR(1, "u", "s", "b").
		ChimeraReview("t", "f", "APPROVE", []string{"m"}, 0, 0.5).
		Conscientiousness("r", "DEFENSIBLE", []string{"v"}, 0).
		PromptFoo(1, 1, 0, "ar", nil).
		CoApprovals(&audit.ApprovalRecord{Reviewer: "h"},
			&audit.ApprovalRecord{Reviewer: "a"}).
		Merge("s", "sq", "u", "t", ts)
	got := b.FormatProgress()
	if !strings.Contains(got, "12/12 complete") {
		t.Errorf("expected 12/12 complete in: %s", got)
	}
	if !strings.Contains(got, "all steps present") {
		t.Errorf("expected 'all steps present' in: %s", got)
	}
}

// =============================================================================
// Step ordering
// =============================================================================

func TestMissingSteps_ReturnsStepsInOrder(t *testing.T) {
	b := New("1").Merge("s", "sq", "u", "t", time.Now()) // only step 12
	missing := b.MissingSteps()
	// All steps except 12, in step order.
	for i, id := range missing {
		want := audit.StepID(i + 1)
		if id != want {
			t.Errorf("missing[%d] = %d, want %d", i, id, want)
		}
	}
}

func TestPresentSteps_ReturnsStepsInOrder(t *testing.T) {
	ts := time.Now()
	b := New("1").
		Merge("s", "sq", "u", "t", ts).
		Issue("u", "c", ts, "t")
	present := b.PresentSteps()
	if len(present) != 2 {
		t.Fatalf("expected 2 present steps, got %d", len(present))
	}
	if present[0] != audit.StepForgejoIssue {
		t.Errorf("present[0] = %d, want 1 (ForgejoIssue)", present[0])
	}
	if present[1] != audit.StepMerge {
		t.Errorf("present[1] = %d, want 12 (Merge)", present[1])
	}
}
