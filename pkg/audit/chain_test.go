package audit

import (
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Test Helpers
// =============================================================================

func mustParseTime(s string) time.Time {
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic("failed to parse time " + s + ": " + err.Error())
	}
	return ts
}

// completeEvidence returns a fully populated AuditEvidence with valid data
// for all 12 steps. Individual tests can nil out or modify fields to test
// specific failure modes.
func completeEvidence() *AuditEvidence {
	return &AuditEvidence{
		ForgejoIssue: &ForgejoIssueEvidence{
			IssueURL:  "https://forgejo.example.com/repo/issues/42",
			Creator:   "bane",
			Timestamp: mustParseTime("2026-06-19T12:00:00Z"),
			Title:     "Add agent identity provisioning",
		},
		AxiomWorkItem: &AxiomWorkItemEvidence{
			PlanYAMLRef: ".axiom/plans/issue-42.yaml",
			AgentIDs:    []string{"agent-sandbox-7"},
			RunID:       "run-abc123",
			WorkItemID:  "wi-042",
		},
		RalphLoop: &RalphLoopEvidence{
			LockID:         "lock-xyz789",
			WorktreePath:   "/worktrees/agent-sandbox-7",
			LockAcquiredAt: mustParseTime("2026-06-19T12:05:00Z"),
			LockReleasedAt: mustParseTime("2026-06-19T12:30:00Z"),
		},
		OpenCodeSession: &OpenCodeSessionEvidence{
			SessionID:       "session-def456",
			Model:           "glm-5.2",
			TokensInput:     50000,
			TokensOutput:    8000,
			CostUSD:         0.42,
			LangFuseTraceID: "trace-lf-001",
		},
		GitCommit: &GitCommitEvidence{
			SHA:              "abc123def456",
			AttestationFound: true,
			PromptHash:       "sha256:abcdef1234567890",
			Model:            "glm-5.2",
			ContextHash:      "sha256:0987654321fedcba",
			AgentID:          "agent-sandbox-7",
			Confidence:       85,
			CostUSD:          0.42,
		},
		GitReinsVerdict: &GitReinsVerdictEvidence{
			Tier1Passed:   true,
			Tier1Checks:   []string{"secrets", "lint", "tests", "build"},
			Tier2Verdict:  "COMPLETE",
			Tier2Findings: 3,
			VerdictTime:   mustParseTime("2026-06-19T12:35:00Z"),
		},
		PRMetadata: &PRMetadataEvidence{
			PRIndex:          42,
			LinkedIssueURL:   "https://forgejo.example.com/repo/issues/42",
			SpecRef:          "specs/agent-identity.md",
			EvidenceBundleID: "eb-001",
		},
		ChimeraReview: &ChimeraReviewEvidence{
			TraceID:      "chimera-trace-001",
			Formation:    "code-review-standard",
			WorkerModels: []string{"anthropic/claude-sonnet-4", "google/gemini-2.5-pro", "openai/gpt-5.2"},
			Verdict:      "APPROVE",
			Findings:     2,
			Score:        0.82,
		},
		Conscientiousness: &ConscientiousnessEvidence{
			ReportID:      "cs-report-001",
			AttackVectors: []string{"prompt-injection", "credential-exfil", "supply-chain"},
			Verdict:       "DEFENSIBLE",
			Mitigations:   3,
		},
		PromptFooCI: &PromptFooCIEvidence{
			TotalTests:   10,
			PassedTests:  10,
			FailedTests:  0,
			ActionsRunID: "forgejo-actions-42",
			Results: []PromptFooResult{
				{TestCase: "test-case-1", Passed: true, Model: "glm-5.2", Variance: 0.0},
				{TestCase: "test-case-2", Passed: true, Model: "glm-5.2", Variance: 0.01},
			},
		},
		CoApprovals: &CoApprovalEvidence{
			HumanApproval: &ApprovalRecord{
				Reviewer:   "bane",
				TrustLevel: 100,
				Timestamp:  mustParseTime("2026-06-19T13:00:00Z"),
			},
			AgentApproval: &ApprovalRecord{
				Reviewer:   "agent-reviewer-1",
				TrustLevel: 75,
				Timestamp:  mustParseTime("2026-06-19T12:55:00Z"),
			},
		},
		Merge: &MergeEvidence{
			MergeSHA:        "merge-sha-789",
			Strategy:        "squash",
			Timestamp:       mustParseTime("2026-06-19T13:05:00Z"),
			PagesURL:        "https://pages.helixloop.dev/repo",
			LangFuseTraceID: "trace-lf-final",
		},
	}
}

// =============================================================================
// Step Name Tests
// =============================================================================

func TestStepName(t *testing.T) {
	tests := []struct {
		id   StepID
		want string
	}{
		{StepForgejoIssue, "Forgejo Issue"},
		{StepAxiomWorkItem, "Axiom Work Item"},
		{StepRalphLoop, "Ralph Loop"},
		{StepOpenCodeSession, "OpenCode Session"},
		{StepGitCommit, "Git Commit"},
		{StepGitReinsVerdict, "GitReins Verdict"},
		{StepPRMetadata, "PR Metadata"},
		{StepChimeraReview, "Chimera Review"},
		{StepConscientiousness, "Conscientiousness Report"},
		{StepPromptFooCI, "PromptFoo CI"},
		{StepCoApprovals, "Co-Approvals"},
		{StepMerge, "Merge"},
	}
	for _, tt := range tests {
		if got := StepName(tt.id); got != tt.want {
			t.Errorf("StepName(%d) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestStepName_UnknownStep(t *testing.T) {
	got := StepName(StepID(99))
	want := "Step 99"
	if got != want {
		t.Errorf("StepName(99) = %q, want %q", got, want)
	}
}

func TestAllSteps_Count(t *testing.T) {
	steps := AllSteps()
	if len(steps) != 12 {
		t.Errorf("AllSteps() returned %d steps, want 12", len(steps))
	}
}

func TestAllSteps_Ordered(t *testing.T) {
	steps := AllSteps()
	for i, s := range steps {
		if int(s) != i+1 {
			t.Errorf("AllSteps()[%d] = Step %d, want Step %d", i, s, i+1)
		}
	}
}

// =============================================================================
// Complete Evidence — Happy Path
// =============================================================================

func TestCheck_AllStepsPass(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	report := checker.Check(42, evidence)

	if !report.AllPassed {
		t.Errorf("AllPassed = false, want true")
		t.Logf("Report:\n%s", report.FormatReport())
	}
	if report.TotalIssues != 0 {
		t.Errorf("TotalIssues = %d, want 0", report.TotalIssues)
	}
	if len(report.Steps) != 12 {
		t.Errorf("Steps count = %d, want 12", len(report.Steps))
	}
	if len(report.FailedSteps()) != 0 {
		t.Errorf("FailedSteps = %v, want empty", report.FailedSteps())
	}
	if len(report.MissingSteps()) != 0 {
		t.Errorf("MissingSteps = %v, want empty", report.MissingSteps())
	}
}

func TestCheck_PRIndex(t *testing.T) {
	checker := NewChecker()
	report := checker.Check(99, completeEvidence())
	if report.PRIndex != 99 {
		t.Errorf("PRIndex = %d, want 99", report.PRIndex)
	}
}

// =============================================================================
// Missing Evidence Tests — Each Step Nilled Out
// =============================================================================

func TestCheck_MissingForgejoIssue(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.ForgejoIssue = nil
	report := checker.Check(42, evidence)

	if report.AllPassed {
		t.Error("AllPassed = true, want false (missing evidence)")
	}
	missing := report.MissingSteps()
	if len(missing) != 1 || missing[0] != StepForgejoIssue {
		t.Errorf("MissingSteps = %v, want [StepForgejoIssue]", missing)
	}
}

func TestCheck_MissingAxiomWorkItem(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.AxiomWorkItem = nil
	report := checker.Check(42, evidence)

	if report.AllPassed {
		t.Error("AllPassed = true, want false")
	}
}

func TestCheck_MissingRalphLoop(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.RalphLoop = nil
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false")
	}
}

func TestCheck_MissingOpenCodeSession(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.OpenCodeSession = nil
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false")
	}
}

func TestCheck_MissingGitCommit(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.GitCommit = nil
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false")
	}
}

func TestCheck_MissingGitReinsVerdict(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.GitReinsVerdict = nil
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false")
	}
}

func TestCheck_MissingPRMetadata(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.PRMetadata = nil
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false")
	}
}

func TestCheck_MissingChimeraReview(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.ChimeraReview = nil
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false")
	}
}

func TestCheck_MissingConscientiousness(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.Conscientiousness = nil
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false")
	}
}

func TestCheck_MissingPromptFooCI(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.PromptFooCI = nil
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false")
	}
}

func TestCheck_MissingCoApprovals(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.CoApprovals = nil
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false")
	}
}

func TestCheck_MissingMerge(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.Merge = nil
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false")
	}
}

func TestCheck_AllMissing(t *testing.T) {
	checker := NewChecker()
	evidence := &AuditEvidence{}
	report := checker.Check(42, evidence)

	if report.AllPassed {
		t.Error("AllPassed = true, want false (no evidence)")
	}
	if len(report.MissingSteps()) != 12 {
		t.Errorf("MissingSteps count = %d, want 12", len(report.MissingSteps()))
	}
	if report.TotalIssues != 12 {
		t.Errorf("TotalIssues = %d, want 12 (one per missing step)", report.TotalIssues)
	}
}

// =============================================================================
// Validation Failure Tests
// =============================================================================

func TestCheck_ForgejoIssue_EmptyURL(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.ForgejoIssue.IssueURL = ""
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false (empty URL)")
	}
}

func TestCheck_ForgejoIssue_ZeroTimestamp(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.ForgejoIssue.Timestamp = time.Time{}
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false (zero timestamp)")
	}
}

func TestCheck_AxiomWorkItem_NoAgents(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.AxiomWorkItem.AgentIDs = []string{}
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false (no agents)")
	}
}

func TestCheck_GitCommit_NoAttestation(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.GitCommit.AttestationFound = false
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false (no attestation)")
	}
}

func TestCheck_GitCommit_ConfidenceOutOfRange(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.GitCommit.Confidence = 150
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false (confidence > 100)")
	}
}

func TestCheck_GitReinsVerdict_Tier1Failed(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.GitReinsVerdict.Tier1Passed = false
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false (Tier 1 failed)")
	}
}

func TestCheck_GitReinsVerdict_InvalidTier2Verdict(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.GitReinsVerdict.Tier2Verdict = "MAYBE"
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false (invalid Tier 2 verdict)")
	}
}

func TestCheck_ChimeraReview_TooFewModels(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.ChimeraReview.WorkerModels = []string{"anthropic/claude-sonnet-4"}
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false (only 1 model, need 3+)")
	}
}

func TestCheck_ChimeraReview_ScoreOutOfRange(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.ChimeraReview.Score = 1.5
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false (score > 1)")
	}
}

func TestCheck_Conscientiousness_InvalidVerdict(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.Conscientiousness.Verdict = "MAYBE"
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false (invalid verdict)")
	}
}

func TestCheck_PromptFooCI_TestCountMismatch(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.PromptFooCI.PassedTests = 8
	evidence.PromptFooCI.FailedTests = 1
	// total should be 10, but 8+1=9 != 10
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false (test count mismatch)")
	}
}

func TestCheck_PromptFooCI_FailedTests(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.PromptFooCI.PassedTests = 9
	evidence.PromptFooCI.FailedTests = 1
	// now 9+1=10=total, but there are failures
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false (1 test failed)")
	}
}

func TestCheck_CoApprovals_MissingHuman(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.CoApprovals.HumanApproval = nil
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false (no human approval)")
	}
}

func TestCheck_CoApprovals_MissingAgent(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.CoApprovals.AgentApproval = nil
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false (no agent approval)")
	}
}

func TestCheck_Merge_EmptySHA(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.Merge.MergeSHA = ""
	report := checker.Check(42, evidence)
	if report.AllPassed {
		t.Error("AllPassed = true, want false (empty merge SHA)")
	}
}

// =============================================================================
// CheckStep Tests
// =============================================================================

func TestCheckStep_Valid(t *testing.T) {
	checker := NewChecker()
	result := checker.CheckStep(StepMerge, completeEvidence().Merge)
	if !result.Present {
		t.Error("Present = false, want true")
	}
	if !result.Valid {
		t.Error("Valid = false, want true")
	}
}

func TestCheckStep_Missing(t *testing.T) {
	checker := NewChecker()
	result := checker.CheckStep(StepMerge, nil)
	if result.Present {
		t.Error("Present = true, want false")
	}
	if result.Valid {
		t.Error("Valid = true, want false")
	}
}

func TestCheckStep_InvalidEvidence(t *testing.T) {
	checker := NewChecker()
	merge := &MergeEvidence{MergeSHA: "", Strategy: "", Timestamp: time.Time{}}
	result := checker.CheckStep(StepMerge, merge)
	if !result.Present {
		t.Error("Present = false, want true")
	}
	if result.Valid {
		t.Error("Valid = true, want false (empty fields)")
	}
	if len(result.Issues) == 0 {
		t.Error("Issues = empty, want non-empty")
	}
}

// =============================================================================
// FormatReport Tests
// =============================================================================

func TestFormatReport_Pass(t *testing.T) {
	checker := NewChecker()
	report := checker.Check(42, completeEvidence())
	output := report.FormatReport()

	if !strings.Contains(output, "PASS") {
		t.Errorf("FormatReport() missing PASS: %s", output)
	}
	if !strings.Contains(output, "PR #42") {
		t.Errorf("FormatReport() missing PR index: %s", output)
	}
	if !strings.Contains(output, "Step 1: Forgejo Issue") {
		t.Errorf("FormatReport() missing Step 1: %s", output)
	}
	if !strings.Contains(output, "Step 12: Merge") {
		t.Errorf("FormatReport() missing Step 12: %s", output)
	}
}

func TestFormatReport_Fail(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.ForgejoIssue = nil
	evidence.Merge.MergeSHA = ""
	report := checker.Check(42, evidence)
	output := report.FormatReport()

	if !strings.Contains(output, "FAIL") {
		t.Errorf("FormatReport() missing FAIL: %s", output)
	}
	if !strings.Contains(output, "MISSING") {
		t.Errorf("FormatReport() missing MISSING for nil evidence: %s", output)
	}
	if !strings.Contains(output, "merge SHA is empty") {
		t.Errorf("FormatReport() missing issue detail: %s", output)
	}
}

// =============================================================================
// Ledger Tests
// =============================================================================

func TestLedger_AppendAndEntries(t *testing.T) {
	checker := NewChecker()
	ledger := NewLedger()

	// Pass
	report1 := checker.Check(1, completeEvidence())
	ledger.Append(report1)

	// Fail
	evidence2 := completeEvidence()
	evidence2.ForgejoIssue = nil
	report2 := checker.Check(2, evidence2)
	ledger.Append(report2)

	entries := ledger.Entries()
	if len(entries) != 2 {
		t.Fatalf("Entries() returned %d, want 2", len(entries))
	}
	// Entries sorted by timestamp — both are ~same time, so order may vary
	// but both should be present
}

func TestLedger_PassRate(t *testing.T) {
	checker := NewChecker()
	ledger := NewLedger()

	// 2 passes
	ledger.Append(checker.Check(1, completeEvidence()))
	ledger.Append(checker.Check(2, completeEvidence()))

	// 1 fail
	ev3 := completeEvidence()
	ev3.Merge = nil
	ledger.Append(checker.Check(3, ev3))

	rate := ledger.PassRate()
	if rate != 2.0/3.0 {
		t.Errorf("PassRate() = %.4f, want %.4f", rate, 2.0/3.0)
	}
}

func TestLedger_EmptyPassRate(t *testing.T) {
	ledger := NewLedger()
	if rate := ledger.PassRate(); rate != 0 {
		t.Errorf("PassRate() = %f, want 0", rate)
	}
}

func TestLedger_RecentFailures(t *testing.T) {
	checker := NewChecker()
	ledger := NewLedger()

	// 1 pass then 2 fails
	ledger.Append(checker.Check(1, completeEvidence()))

	ev2 := completeEvidence()
	ev2.ForgejoIssue = nil
	ledger.Append(checker.Check(2, ev2))

	ev3 := completeEvidence()
	ev3.Merge = nil
	ledger.Append(checker.Check(3, ev3))

	failures := ledger.RecentFailures(5)
	if len(failures) != 2 {
		t.Fatalf("RecentFailures(5) returned %d, want 2", len(failures))
	}
}

func TestLedger_RecentFailures_LimitN(t *testing.T) {
	checker := NewChecker()
	ledger := NewLedger()

	for i := 1; i <= 5; i++ {
		ev := completeEvidence()
		ev.ForgejoIssue = nil
		ledger.Append(checker.Check(i, ev))
	}

	failures := ledger.RecentFailures(2)
	if len(failures) != 2 {
		t.Errorf("RecentFailures(2) returned %d, want 2", len(failures))
	}
}

func TestLedger_Len(t *testing.T) {
	checker := NewChecker()
	ledger := NewLedger()
	ledger.Append(checker.Check(1, completeEvidence()))
	ledger.Append(checker.Check(2, completeEvidence()))
	if ledger.Len() != 2 {
		t.Errorf("Len() = %d, want 2", ledger.Len())
	}
}

// =============================================================================
// ChainBuilder Tests
// =============================================================================

func TestChainBuilder_FullChain(t *testing.T) {
	ts := mustParseTime("2026-06-19T12:00:00Z")
	builder := NewChainBuilder().
		WithForgejoIssue(ForgejoIssueEvidence{
			IssueURL:  "https://example.com/1",
			Creator:   "bane",
			Timestamp: ts,
			Title:     "Test",
		}).
		WithAxiomWorkItem(AxiomWorkItemEvidence{
			PlanYAMLRef: "plan.yaml",
			AgentIDs:    []string{"a1"},
			RunID:       "r1",
			WorkItemID:  "w1",
		}).
		WithRalphLoop(RalphLoopEvidence{
			LockID:         "l1",
			WorktreePath:   "/wt",
			LockAcquiredAt: ts,
		}).
		WithOpenCodeSession(OpenCodeSessionEvidence{
			SessionID:       "s1",
			Model:           "glm-5.2",
			TokensInput:     100,
			TokensOutput:    20,
			LangFuseTraceID: "t1",
		}).
		WithGitCommit(GitCommitEvidence{
			SHA:              "abc",
			AttestationFound: true,
			PromptHash:       "sha256:x",
			Model:            "glm-5.2",
			AgentID:          "a1",
			Confidence:       80,
		}).
		WithGitReinsVerdict(GitReinsVerdictEvidence{
			Tier1Passed:  true,
			Tier1Checks:  []string{"secrets"},
			Tier2Verdict: "COMPLETE",
			VerdictTime:  ts,
		}).
		WithPRMetadata(PRMetadataEvidence{
			PRIndex:          42,
			LinkedIssueURL:   "https://example.com/1",
			SpecRef:          "spec.md",
			EvidenceBundleID: "eb1",
		}).
		WithChimeraReview(ChimeraReviewEvidence{
			TraceID:      "ct1",
			Formation:    "standard",
			WorkerModels: []string{"m1", "m2", "m3"},
			Verdict:      "APPROVE",
			Score:        0.9,
		}).
		WithConscientiousness(ConscientiousnessEvidence{
			ReportID:      "cs1",
			AttackVectors: []string{"injection"},
			Verdict:       "DEFENSIBLE",
		}).
		WithPromptFooCI(PromptFooCIEvidence{
			TotalTests:   5,
			PassedTests:  5,
			FailedTests:  0,
			ActionsRunID: "run1",
		}).
		WithCoApprovals(CoApprovalEvidence{
			HumanApproval: &ApprovalRecord{Reviewer: "bane", Timestamp: ts},
			AgentApproval: &ApprovalRecord{Reviewer: "a1", Timestamp: ts},
		}).
		WithMerge(MergeEvidence{
			MergeSHA:        "m1",
			Strategy:        "squash",
			Timestamp:       ts,
			LangFuseTraceID: "t2",
		})

	if !builder.IsComplete() {
		t.Error("IsComplete() = false, want true (all steps set)")
	}
	steps := builder.CompletedSteps()
	if len(steps) != 12 {
		t.Errorf("CompletedSteps() returned %d, want 12", len(steps))
	}

	ev := builder.Build()
	checker := NewChecker()
	report := checker.Check(42, ev)
	if !report.AllPassed {
		t.Errorf("Audit failed for complete chain:\n%s", report.FormatReport())
	}
}

func TestChainBuilder_PartialChain(t *testing.T) {
	ts := mustParseTime("2026-06-19T12:00:00Z")
	builder := NewChainBuilder().
		WithForgejoIssue(ForgejoIssueEvidence{
			IssueURL:  "https://example.com/1",
			Creator:   "bane",
			Timestamp: ts,
			Title:     "Test",
		}).
		WithMerge(MergeEvidence{
			MergeSHA:        "m1",
			Strategy:        "squash",
			Timestamp:       ts,
			LangFuseTraceID: "t2",
		})

	if builder.IsComplete() {
		t.Error("IsComplete() = true, want false (only 2 steps set)")
	}
	steps := builder.CompletedSteps()
	if len(steps) != 2 {
		t.Errorf("CompletedSteps() returned %d, want 2", len(steps))
	}

	ev := builder.Build()
	checker := NewChecker()
	report := checker.Check(42, ev)
	if report.AllPassed {
		t.Error("AllPassed = true, want false (incomplete chain)")
	}
	if len(report.MissingSteps()) != 10 {
		t.Errorf("MissingSteps count = %d, want 10", len(report.MissingSteps()))
	}
}

func TestChainBuilder_Empty(t *testing.T) {
	builder := NewChainBuilder()
	if builder.IsComplete() {
		t.Error("IsComplete() = true, want false")
	}
	if len(builder.CompletedSteps()) != 0 {
		t.Errorf("CompletedSteps() = %d, want 0", len(builder.CompletedSteps()))
	}
}

// =============================================================================
// StepEvidence Lookup Tests
// =============================================================================

func TestStepEvidence_AllSteps(t *testing.T) {
	ev := completeEvidence()
	for _, stepID := range AllSteps() {
		got := ev.StepEvidence(stepID)
		if got == nil {
			t.Errorf("StepEvidence(%d) = nil, want non-nil", stepID)
		}
	}
}

func TestStepEvidence_UnknownStep(t *testing.T) {
	ev := completeEvidence()
	got := ev.StepEvidence(StepID(99))
	if got != nil {
		t.Errorf("StepEvidence(99) = %v, want nil", got)
	}
}

// =============================================================================
// Custom Validator Tests
// =============================================================================

func TestNewCheckerWithValidators(t *testing.T) {
	customValidators := map[StepID]StepValidator{
		StepForgejoIssue: func(e interface{}) []string {
			return []string{"custom failure"}
		},
	}
	checker := NewCheckerWithValidators(customValidators)
	evidence := completeEvidence()
	report := checker.Check(42, evidence)

	if report.AllPassed {
		t.Error("AllPassed = true, want false (custom validator always fails)")
	}
	// Only Step 1 should have the custom failure; others have no validator
	step1 := report.Steps[0]
	if step1.Valid {
		t.Error("Step 1 should be invalid with custom validator")
	}
	if len(step1.Issues) != 1 || step1.Issues[0] != "custom failure" {
		t.Errorf("Step 1 issues = %v, want [custom failure]", step1.Issues)
	}
	// Steps without validators should be valid (present but no validation)
	step2 := report.Steps[1]
	if !step2.Valid {
		t.Errorf("Step 2 should be valid (no validator): %v", step2.Issues)
	}
}

// =============================================================================
// FailedSteps and MissingSteps
// =============================================================================

func TestFailedSteps_MultipleFailures(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.ForgejoIssue = nil
	evidence.Merge.MergeSHA = ""
	evidence.ChimeraReview.WorkerModels = []string{"m1"} // too few
	report := checker.Check(42, evidence)

	failed := report.FailedSteps()
	if len(failed) != 3 {
		t.Errorf("FailedSteps() = %d steps, want 3", len(failed))
	}
}

func TestMissingSteps_Subset(t *testing.T) {
	checker := NewChecker()
	evidence := completeEvidence()
	evidence.ForgejoIssue = nil
	evidence.AxiomWorkItem = nil
	report := checker.Check(42, evidence)

	missing := report.MissingSteps()
	if len(missing) != 2 {
		t.Errorf("MissingSteps() = %d, want 2", len(missing))
	}
	// Should be steps 1 and 2
	for _, m := range missing {
		if m != StepForgejoIssue && m != StepAxiomWorkItem {
			t.Errorf("MissingSteps() contains unexpected step %d", m)
		}
	}
}
