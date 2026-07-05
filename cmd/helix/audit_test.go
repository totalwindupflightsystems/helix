package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/audit"
)

// makeFullEvidence returns a fully-populated AuditEvidence that passes all
// 12 step validators.
func makeFullEvidence() *audit.AuditEvidence {
	return &audit.AuditEvidence{
		ForgejoIssue: &audit.ForgejoIssueEvidence{
			IssueURL:  "http://forgejo:3000/org/repo/issues/1",
			Creator:   "wojons",
			Timestamp: time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC),
			Title:     "Add feature X",
		},
		AxiomWorkItem: &audit.AxiomWorkItemEvidence{
			PlanYAMLRef: ".memory-bank/plan.yaml",
			AgentIDs:    []string{"agent-1", "agent-2"},
			RunID:       "run-abc-123",
			WorkItemID:  "wi-001",
		},
		RalphLoop: &audit.RalphLoopEvidence{
			LockID:         "lock-uuid-123",
			WorktreePath:   "/workspace/repo-wi-001",
			LockAcquiredAt: time.Date(2026, 7, 5, 12, 5, 0, 0, time.UTC),
		},
		OpenCodeSession: &audit.OpenCodeSessionEvidence{
			SessionID:       "session-456",
			Model:           "deepseek-v4-pro",
			TokensInput:     45000,
			TokensOutput:    8200,
			CostUSD:         0.1064,
			LangFuseTraceID: "lf-trace-789",
		},
		GitCommit: &audit.GitCommitEvidence{
			SHA:              "abc123def456",
			AttestationFound: true,
			PromptHash:       "sha256:abc123",
			Model:            "deepseek-v4-pro",
			ContextHash:      "sha256:def456",
			AgentID:          "agent-sandbox-7",
			Confidence:       85,
			CostUSD:          0.1064,
		},
		GitReinsVerdict: &audit.GitReinsVerdictEvidence{
			Tier1Passed:   true,
			Tier1Checks:   []string{"secrets", "lint", "tests", "build"},
			Tier2Verdict:  "COMPLETE",
			Tier2Findings: 0,
			VerdictTime:   time.Date(2026, 7, 5, 12, 10, 0, 0, time.UTC),
		},
		PRMetadata: &audit.PRMetadataEvidence{
			PRIndex:          42,
			LinkedIssueURL:   "http://forgejo:3000/org/repo/issues/1",
			SpecRef:          "specs/trust-model.md",
			EvidenceBundleID: "eb-001",
		},
		ChimeraReview: &audit.ChimeraReviewEvidence{
			TraceID:      "chimera-trace-001",
			Formation:    "code-review-standard",
			WorkerModels: []string{"anthropic/claude-sonnet-4", "google/gemini-2.5-pro", "openai/gpt-5.2"},
			Verdict:      "APPROVE",
			Findings:     0,
			Score:        0.92,
		},
		Conscientiousness: &audit.ConscientiousnessEvidence{
			ReportID:      "conscience-rpt-001",
			AttackVectors: []string{"prompt-injection", "data-exfil"},
			Verdict:       "DEFENSIBLE",
			Mitigations:   2,
		},
		PromptFooCI: &audit.PromptFooCIEvidence{
			TotalTests:   5,
			PassedTests:  5,
			FailedTests:  0,
			ActionsRunID: "run-42",
			Results: []audit.PromptFooResult{
				{TestCase: "test-1", Passed: true, Model: "deepseek-v4-pro", Variance: 0},
			},
		},
		CoApprovals: &audit.CoApprovalEvidence{
			HumanApproval: &audit.ApprovalRecord{
				Reviewer:   "wojons",
				TrustLevel: 0,
				Timestamp:  time.Date(2026, 7, 5, 13, 0, 0, 0, time.UTC),
			},
			AgentApproval: &audit.ApprovalRecord{
				Reviewer:   "agent-sandbox-7",
				TrustLevel: 85,
				Timestamp:  time.Date(2026, 7, 5, 13, 5, 0, 0, time.UTC),
			},
		},
		Merge: &audit.MergeEvidence{
			MergeSHA:        "merge-sha-abc",
			Strategy:        "squash",
			Timestamp:       time.Date(2026, 7, 5, 13, 10, 0, 0, time.UTC),
			PagesURL:        "https://helixloop.dev/org/repo",
			LangFuseTraceID: "lf-final-trace-001",
		},
	}
}

// writeEvidenceFile marshals evidence to a temp JSON file and returns its path.
func writeEvidenceFile(t *testing.T, ev *audit.AuditEvidence) string {
	t.Helper()
	data, err := audit.MarshalEvidence(ev)
	if err != nil {
		t.Fatalf("MarshalEvidence: %v", err)
	}
	path := filepath.Join(t.TempDir(), "evidence.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// =============================================================================
// parseAuditFlags tests
// =============================================================================

func TestParseAuditFlags_Help(t *testing.T) {
	// --help prints to os.Stdout and returns OK.
	// We just verify the return code; the help output goes to the real stdout.
	f, rc := parseAuditFlags([]string{"--help"})
	if rc != auditExitOK {
		t.Fatalf("rc=%d, want %d", rc, auditExitOK)
	}
	if f.subcommand != "" {
		t.Errorf("subcommand=%q, want empty", f.subcommand)
	}
}

func TestParseAuditFlags_NoArgs(t *testing.T) {
	_, rc := parseAuditFlags([]string{})
	if rc != auditExitOK {
		t.Errorf("rc=%d, want %d", rc, auditExitOK)
	}
}

func TestParseAuditFlags_TraceWithEvidenceFile(t *testing.T) {
	f, rc := parseAuditFlags([]string{"trace", "--evidence-file", "/tmp/evidence.json", "--pr-index", "42"})
	if rc != auditExitOK {
		t.Fatalf("rc=%d, want %d", rc, auditExitOK)
	}
	if f.subcommand != "trace" {
		t.Errorf("subcommand=%q, want trace", f.subcommand)
	}
	if f.evidenceFile != "/tmp/evidence.json" {
		t.Errorf("evidenceFile=%q, want /tmp/evidence.json", f.evidenceFile)
	}
	if f.prIndex != 42 {
		t.Errorf("prIndex=%d, want 42", f.prIndex)
	}
}

func TestParseAuditFlags_UnknownFlag(t *testing.T) {
	_, rc := parseAuditFlags([]string{"trace", "--bogus"})
	if rc != auditExitError {
		t.Errorf("rc=%d, want %d", rc, auditExitError)
	}
}

func TestParseAuditFlags_EvidenceFileMissingValue(t *testing.T) {
	_, rc := parseAuditFlags([]string{"trace", "--evidence-file"})
	if rc != auditExitError {
		t.Errorf("rc=%d, want %d", rc, auditExitError)
	}
}

func TestParseAuditFlags_PRIndexMissingValue(t *testing.T) {
	_, rc := parseAuditFlags([]string{"trace", "--pr-index"})
	if rc != auditExitError {
		t.Errorf("rc=%d, want %d", rc, auditExitError)
	}
}

func TestParseAuditFlags_InvalidPRIndex(t *testing.T) {
	_, rc := parseAuditFlags([]string{"trace", "--pr-index", "not-a-number"})
	if rc != auditExitError {
		t.Errorf("rc=%d, want %d", rc, auditExitError)
	}
}

func TestParseAuditFlags_JSONFlag(t *testing.T) {
	f, rc := parseAuditFlags([]string{"steps", "--json"})
	if rc != auditExitOK {
		t.Fatalf("rc=%d", rc)
	}
	if !f.jsonOut {
		t.Error("jsonOut=false, want true")
	}
}

// =============================================================================
// runAuditSteps tests
// =============================================================================

func TestRunAuditSteps_HumanReadable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runAudit([]string{"steps"}, &stdout, &stderr)
	if rc != auditExitOK {
		t.Fatalf("rc=%d, want %d", rc, auditExitOK)
	}
	out := stdout.String()
	if !contains(out, "12-Step Audit Chain") {
		t.Errorf("output missing header: %s", out)
	}
	if !contains(out, "Forgejo Issue") {
		t.Errorf("output missing step name: %s", out)
	}
	if !contains(out, "Merge") {
		t.Errorf("output missing last step: %s", out)
	}
}

func TestRunAuditSteps_JSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runAudit([]string{"steps", "--json"}, &stdout, &stderr)
	if rc != auditExitOK {
		t.Fatalf("rc=%d, want %d", rc, auditExitOK)
	}
	var list []struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &list); err != nil {
		t.Fatalf("JSON unmarshal: %v\noutput: %s", err, stdout.String())
	}
	if len(list) != 12 {
		t.Errorf("len=%d, want 12", len(list))
	}
	if list[0].Name != "Forgejo Issue" {
		t.Errorf("list[0].Name=%q, want Forgejo Issue", list[0].Name)
	}
	if list[11].Name != "Merge" {
		t.Errorf("list[11].Name=%q, want Merge", list[11].Name)
	}
	for _, s := range list {
		if s.Description == "" {
			t.Errorf("step %d has empty description", s.ID)
		}
	}
}

// =============================================================================
// runAuditTrace tests
// =============================================================================

func TestRunAuditTrace_HappyPath(t *testing.T) {
	ev := makeFullEvidence()
	path := writeEvidenceFile(t, ev)
	var stdout, stderr bytes.Buffer
	rc := runAudit([]string{"trace", "--evidence-file", path, "--pr-index", "42"}, &stdout, &stderr)
	if rc != auditExitOK {
		t.Fatalf("rc=%d, want %d (all evidence valid). stderr: %s", rc, auditExitOK, stderr.String())
	}
	out := stdout.String()
	if !contains(out, "Audit Report") {
		t.Errorf("output missing header: %s", out)
	}
	if !contains(out, "PASS") {
		t.Errorf("output missing PASS: %s", out)
	}
}

func TestRunAuditTrace_JSON(t *testing.T) {
	ev := makeFullEvidence()
	path := writeEvidenceFile(t, ev)
	var stdout, stderr bytes.Buffer
	rc := runAudit([]string{"trace", "--evidence-file", path, "--pr-index", "42", "--json"}, &stdout, &stderr)
	if rc != auditExitOK {
		t.Fatalf("rc=%d, want %d. stderr: %s", rc, auditExitOK, stderr.String())
	}
	var report audit.AuditReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("JSON unmarshal: %v\noutput: %s", err, stdout.String())
	}
	if !report.AllPassed {
		t.Error("AllPassed=false, want true")
	}
	if len(report.Steps) != 12 {
		t.Errorf("len(Steps)=%d, want 12", len(report.Steps))
	}
	if report.PRIndex != 42 {
		t.Errorf("PRIndex=%d, want 42", report.PRIndex)
	}
}

func TestRunAuditTrace_MissingEvidence(t *testing.T) {
	// Empty evidence — all steps should fail
	ev := &audit.AuditEvidence{}
	path := writeEvidenceFile(t, ev)
	var stdout, stderr bytes.Buffer
	rc := runAudit([]string{"trace", "--evidence-file", path}, &stdout, &stderr)
	if rc != auditExitFail {
		t.Fatalf("rc=%d, want %d (audit fail)", rc, auditExitFail)
	}
	out := stdout.String()
	if !contains(out, "FAIL") {
		t.Errorf("output missing FAIL: %s", out)
	}
}

func TestRunAuditTrace_PartialEvidence(t *testing.T) {
	ev := makeFullEvidence()
	ev.Merge = nil // remove one step
	path := writeEvidenceFile(t, ev)
	var stdout, stderr bytes.Buffer
	rc := runAudit([]string{"trace", "--evidence-file", path}, &stdout, &stderr)
	if rc != auditExitFail {
		t.Fatalf("rc=%d, want %d", rc, auditExitFail)
	}
}

func TestRunAuditTrace_FileNotFound(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runAudit([]string{"trace", "--evidence-file", "/tmp/nonexistent-evidence-12345.json"}, &stdout, &stderr)
	if rc != auditExitNotFound {
		t.Errorf("rc=%d, want %d", rc, auditExitNotFound)
	}
}

func TestRunAuditTrace_MissingEvidenceFileFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runAudit([]string{"trace"}, &stdout, &stderr)
	if rc != auditExitError {
		t.Errorf("rc=%d, want %d", rc, auditExitError)
	}
}

func TestRunAuditTrace_MalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	var stdout, stderr bytes.Buffer
	rc := runAudit([]string{"trace", "--evidence-file", path}, &stdout, &stderr)
	if rc != auditExitError {
		t.Errorf("rc=%d, want %d", rc, auditExitError)
	}
}

func TestRunAuditTrace_InvalidEvidenceValues(t *testing.T) {
	ev := makeFullEvidence()
	ev.GitCommit.Confidence = 150 // out of range
	path := writeEvidenceFile(t, ev)
	var stdout, stderr bytes.Buffer
	rc := runAudit([]string{"trace", "--evidence-file", path}, &stdout, &stderr)
	if rc != auditExitFail {
		t.Fatalf("rc=%d, want %d", rc, auditExitFail)
	}
}

// =============================================================================
// runAuditValidate tests
// =============================================================================

func TestRunAuditValidate_Complete(t *testing.T) {
	ev := makeFullEvidence()
	path := writeEvidenceFile(t, ev)
	var stdout, stderr bytes.Buffer
	rc := runAudit([]string{"validate", "--evidence-file", path}, &stdout, &stderr)
	if rc != auditExitOK {
		t.Fatalf("rc=%d, want %d. stderr: %s", rc, auditExitOK, stderr.String())
	}
	out := stdout.String()
	if !contains(out, "COMPLETE") {
		t.Errorf("output missing COMPLETE: %s", out)
	}
}

func TestRunAuditValidate_Incomplete(t *testing.T) {
	ev := makeFullEvidence()
	ev.Merge = nil
	ev.ChimeraReview = nil
	path := writeEvidenceFile(t, ev)
	var stdout, stderr bytes.Buffer
	rc := runAudit([]string{"validate", "--evidence-file", path}, &stdout, &stderr)
	if rc != auditExitFail {
		t.Fatalf("rc=%d, want %d", rc, auditExitFail)
	}
	out := stdout.String()
	if !contains(out, "INCOMPLETE") {
		t.Errorf("output missing INCOMPLETE: %s", out)
	}
}

func TestRunAuditValidate_JSON(t *testing.T) {
	ev := makeFullEvidence()
	ev.Merge = nil
	path := writeEvidenceFile(t, ev)
	var stdout, stderr bytes.Buffer
	rc := runAudit([]string{"validate", "--evidence-file", path, "--json"}, &stdout, &stderr)
	if rc != auditExitFail {
		t.Fatalf("rc=%d, want %d", rc, auditExitFail)
	}
	var report struct {
		Complete     bool     `json:"complete"`
		CompletedN   int      `json:"completed_steps"`
		TotalSteps   int      `json:"total_steps"`
		MissingSteps []string `json:"missing_steps"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("JSON unmarshal: %v\noutput: %s", err, stdout.String())
	}
	if report.Complete {
		t.Error("Complete=true, want false")
	}
	if report.CompletedN != 11 {
		t.Errorf("CompletedN=%d, want 11", report.CompletedN)
	}
	if report.TotalSteps != 12 {
		t.Errorf("TotalSteps=%d, want 12", report.TotalSteps)
	}
	if len(report.MissingSteps) != 1 {
		t.Errorf("len(MissingSteps)=%d, want 1", len(report.MissingSteps))
	}
	if report.MissingSteps[0] != "Merge" {
		t.Errorf("MissingSteps[0]=%q, want Merge", report.MissingSteps[0])
	}
}

func TestRunAuditValidate_MissingFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runAudit([]string{"validate"}, &stdout, &stderr)
	if rc != auditExitError {
		t.Errorf("rc=%d, want %d", rc, auditExitError)
	}
}

func TestRunAuditValidate_FileNotFound(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runAudit([]string{"validate", "--evidence-file", "/tmp/nonexistent-validate-12345.json"}, &stdout, &stderr)
	if rc != auditExitNotFound {
		t.Errorf("rc=%d, want %d", rc, auditExitNotFound)
	}
}

// =============================================================================
// runAudit (dispatch) tests
// =============================================================================

func TestRunAudit_Help(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runAudit([]string{"help"}, &stdout, &stderr)
	if rc != auditExitOK {
		t.Errorf("rc=%d, want %d", rc, auditExitOK)
	}
	if !contains(stdout.String(), "helix audit") {
		t.Errorf("output missing help text: %s", stdout.String())
	}
}

func TestRunAudit_UnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runAudit([]string{"bogus"}, &stdout, &stderr)
	if rc != auditExitError {
		t.Errorf("rc=%d, want %d", rc, auditExitError)
	}
}

func TestRunAudit_NoSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runAudit([]string{}, &stdout, &stderr)
	if rc != auditExitOK {
		t.Errorf("rc=%d, want %d (defaults to help)", rc, auditExitOK)
	}
}

// =============================================================================
// runAuditWithDryRun tests
// =============================================================================

func TestRunAuditWithDryRun_Success(t *testing.T) {
	err := runAuditWithDryRun([]string{"steps"}, &bytes.Buffer{}, &bytes.Buffer{}, false)
	if err != nil {
		t.Errorf("err=%v, want nil", err)
	}
}

func TestRunAuditWithDryRun_AuditFail(t *testing.T) {
	ev := &audit.AuditEvidence{}
	path := writeEvidenceFile(t, ev)
	err := runAuditWithDryRun([]string{"trace", "--evidence-file", path}, &bytes.Buffer{}, &bytes.Buffer{}, false)
	if err == nil {
		t.Error("err=nil, want errExit")
	}
	if ee, ok := err.(errExit); ok {
		if ee.code != auditExitFail {
			t.Errorf("errExit.code=%d, want %d", ee.code, auditExitFail)
		}
	} else {
		t.Errorf("err type=%T, want errExit", err)
	}
}

func TestRunAuditWithDryRun_GlobalDryRun(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runAuditWithDryRun([]string{"steps"}, &stdout, &stderr, true)
	if err != nil {
		t.Errorf("err=%v, want nil (steps always succeeds)", err)
	}
}

// =============================================================================
// helpers
// =============================================================================
