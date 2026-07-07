package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/helix/pkg/forcemerge"
)

// ============================================================================
// flag parsing
// ============================================================================

func TestParseForceMergeFlags_Defaults(t *testing.T) {
	f, help, rc := parseForceMergeFlags(nil)
	if rc != fmExitOK {
		t.Fatalf("rc=%d want 0", rc)
	}
	if help {
		t.Errorf("help=true want false")
	}
	if f.subcommand != "help" {
		t.Errorf("subcommand=%q want help", f.subcommand)
	}
	if f.auditPath != forcemerge.DefaultAuditPath {
		t.Errorf("auditPath=%q want %q", f.auditPath, forcemerge.DefaultAuditPath)
	}
	if f.confidence != 0 {
		t.Errorf("confidence=%d want 0", f.confidence)
	}
}

func TestParseForceMergeFlags_Help(t *testing.T) {
	_, help, _ := parseForceMergeFlags([]string{"--help"})
	if !help {
		t.Errorf("--help did not set help=true")
	}
}

func TestParseForceMergeFlags_AllOptions(t *testing.T) {
	f, _, rc := parseForceMergeFlags([]string{
		"record",
		"--path", "/tmp/x.jsonl",
		"--pr-url", "https://g/pulls/1",
		"--merge-sha", "abc",
		"--human", "alice",
		"--justification", "spec justification meeting the 20 char floor",
		"--json",
	})
	if rc != fmExitOK {
		t.Fatalf("rc=%d want 0", rc)
	}
	if f.subcommand != "record" {
		t.Errorf("subcommand=%q want record", f.subcommand)
	}
	if f.auditPath != "/tmp/x.jsonl" {
		t.Errorf("auditPath=%q", f.auditPath)
	}
	if !f.jsonOut {
		t.Errorf("jsonOut=false")
	}
	if f.prURL == "" || f.mergeSHA == "" || f.human == "" || f.just == "" {
		t.Errorf("required fields not captured: %+v", f)
	}
}

func TestParseForceMergeFlags_UnknownFlag(t *testing.T) {
	_, _, rc := parseForceMergeFlags([]string{"record", "--bogus", "x"})
	if rc != fmExitError {
		t.Errorf("rc=%d want %d", rc, fmExitError)
	}
}

func TestParseForceMergeFlags_ConfidenceParse(t *testing.T) {
	f, _, rc := parseForceMergeFlags([]string{"review", "--confidence", "85"})
	if rc != fmExitOK {
		t.Fatalf("rc=%d", rc)
	}
	if f.confidence != 85 {
		t.Errorf("confidence=%d want 85", f.confidence)
	}
}

func TestParseForceMergeFlags_BadConfidence(t *testing.T) {
	_, _, rc := parseForceMergeFlags([]string{"review", "--confidence", "abc"})
	if rc != fmExitError {
		t.Errorf("rc=%d want %d", rc, fmExitError)
	}
}

// ============================================================================
// runForceMerge — path, record, review, report
// ============================================================================

func TestRunForceMerge_Path(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runForceMerge([]string{"path"}, &out, &errOut)
	if rc != fmExitOK {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
	got := strings.TrimSpace(out.String())
	if got == "" {
		t.Errorf("path output empty")
	}
	if !strings.HasSuffix(got, "forcemerge-audit.jsonl") {
		t.Errorf("path=%q does not end with forcemerge-audit.jsonl", got)
	}
}

func TestRunForceMerge_PathOverride(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runForceMerge([]string{"path", "--path", "/tmp/helix-fm-test.jsonl"}, &out, &errOut)
	if rc != fmExitOK {
		t.Fatalf("rc=%d", rc)
	}
	if got := strings.TrimSpace(out.String()); got != "/tmp/helix-fm-test.jsonl" {
		t.Errorf("path=%q want /tmp/helix-fm-test.jsonl", got)
	}
}

func TestRunForceMerge_Help(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runForceMerge([]string{"help"}, &out, &errOut)
	if rc != fmExitOK {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(out.String(), "helix forcemerge") {
		t.Errorf("help missing title: %q", out.String())
	}
}

func TestRunForceMerge_UnknownSubcommand(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runForceMerge([]string{"bogus"}, &out, &errOut)
	if rc != fmExitError {
		t.Errorf("rc=%d want %d", rc, fmExitError)
	}
}

func TestRunForceMerge_RecordMissingFlags(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runForceMerge([]string{"record"}, &out, &errOut)
	if rc != fmExitError {
		t.Errorf("rc=%d want %d (missing flags should error)", rc, fmExitError)
	}
	if !strings.Contains(errOut.String(), "required") {
		t.Errorf("stderr=%q missing 'required' message", errOut.String())
	}
}

func TestRunForceMerge_RecordShortJustification(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fm.jsonl")

	var out, errOut bytes.Buffer
	rc := runForceMerge([]string{
		"record",
		"--path", path,
		"--pr-url", "https://g/pulls/1",
		"--merge-sha", "abc123",
		"--human", "alice",
		"--justification", "too short",
	}, &out, &errOut)
	if rc != fmExitFail {
		t.Errorf("rc=%d want %d (short justification must fail validation)", rc, fmExitFail)
	}
	if !strings.Contains(errOut.String(), "justification") {
		t.Errorf("stderr=%q missing 'justification' message", errOut.String())
	}
}

func TestRunForceMerge_RecordAndReport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fm.jsonl")

	// Step 1: Record one force-merge.
	var out, errOut bytes.Buffer
	rc := runForceMerge([]string{
		"record",
		"--path", path,
		"--pr-url", "https://forge.example.com/owner/repo/pulls/42",
		"--merge-sha", "deadbeef",
		"--human", "alice",
		"--justification", "emergency hotfix for production outage in payments service",
	}, &out, &errOut)
	if rc != fmExitOK {
		t.Fatalf("record rc=%d stderr=%s", rc, errOut.String())
	}
	if !strings.Contains(out.String(), "recorded force-merge") {
		t.Errorf("stdout=%q", out.String())
	}

	// Verify the file contains the JSONL line.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), `"human_identity":"alice"`) {
		t.Errorf("log missing human_identity: %s", data)
	}

	// Step 2: Append a review verdict.
	out.Reset()
	errOut.Reset()
	rc = runForceMerge([]string{
		"review",
		"--path", path,
		"--pr-url", "https://forge.example.com/owner/repo/pulls/42",
		"--merge-sha", "deadbeef",
		"--reviewer", "conscientiousness/gpt-5",
		"--verdict", "PASSED",
		"--reason", "override was justified by P0 outage",
		"--confidence", "85",
	}, &out, &errOut)
	if rc != fmExitOK {
		t.Fatalf("review rc=%d stderr=%s", rc, errOut.String())
	}

	// Step 3: Build report.
	out.Reset()
	errOut.Reset()
	rc = runForceMerge([]string{"report", "--path", path}, &out, &errOut)
	if rc != fmExitOK {
		t.Fatalf("report rc=%d stderr=%s", rc, errOut.String())
	}
	if !strings.Contains(out.String(), "deadbeef") && !strings.Contains(out.String(), "alice") {
		t.Errorf("report stdout=%q missing merge/human data", out.String())
	}
}

func TestRunForceMerge_ReportJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fm.jsonl")

	// Seed: one audit + one review.
	seed := `{"pr_url":"https://g/pulls/1","human_identity":"bob","justification":"spec justification meeting the 20 char floor","merge_sha":"sha1","timestamp":"2026-01-15T10:00:00Z"}
{"pr_url":"https://g/pulls/1","merge_sha":"sha1","reviewer":"conscientiousness/x","status":"PASSED","reason":"ok","confidence":90,"timestamp":"2026-01-15T11:00:00Z"}
`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var out, errOut bytes.Buffer
	rc := runForceMerge([]string{"report", "--path", path, "--json"}, &out, &errOut)
	if rc != fmExitOK {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
	var rep forcemerge.AuditReport
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &rep); err != nil {
		t.Fatalf("json: %v\nstdout=%s", err, out.String())
	}
	if rep.TotalMerges != 1 {
		t.Errorf("TotalMerges=%d want 1", rep.TotalMerges)
	}
	if rep.PendingReviewCount != 0 {
		t.Errorf("PendingReviewCount=%d want 0", rep.PendingReviewCount)
	}
	if rep.FailedReviewCount != 0 {
		t.Errorf("FailedReviewCount=%d want 0", rep.FailedReviewCount)
	}
}

func TestRunForceMerge_ReportPendingExitsFail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fm.jsonl")

	// One audit entry, NO review → pending.
	seed := `{"pr_url":"https://g/pulls/2","human_identity":"carol","justification":"spec justification meeting the 20 char floor","merge_sha":"sha2","timestamp":"2026-02-01T09:00:00Z"}
`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var out, errOut bytes.Buffer
	rc := runForceMerge([]string{"report", "--path", path}, &out, &errOut)
	if rc != fmExitFail {
		t.Errorf("rc=%d want %d (pending reviews should fail)", rc, fmExitFail)
	}
	if !strings.Contains(out.String(), "Pending reviews: 1") {
		t.Errorf("stdout=%q missing pending count", out.String())
	}
}

func TestRunForceMerge_ReportFailedExitsFail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fm.jsonl")

	// One audit + one FAILED review.
	seed := `{"pr_url":"https://g/pulls/3","human_identity":"dave","justification":"spec justification meeting the 20 char floor","merge_sha":"sha3","timestamp":"2026-03-01T09:00:00Z"}
{"pr_url":"https://g/pulls/3","merge_sha":"sha3","reviewer":"conscientiousness/x","status":"FAILED","reason":"override not justified","confidence":92,"timestamp":"2026-03-01T10:00:00Z"}
`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var out, errOut bytes.Buffer
	rc := runForceMerge([]string{"report", "--path", path}, &out, &errOut)
	if rc != fmExitFail {
		t.Errorf("rc=%d want %d (failed review should fail)", rc, fmExitFail)
	}
}

func TestRunForceMerge_ReportMissingFile(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runForceMerge([]string{"report", "--path", "/tmp/nonexistent-helix-fm-test.jsonl"}, &out, &errOut)
	if rc != fmExitFail {
		t.Errorf("rc=%d want %d (missing file should fail)", rc, fmExitFail)
	}
	if !strings.Contains(errOut.String(), "not found") {
		t.Errorf("stderr=%q missing 'not found'", errOut.String())
	}
}

func TestRunForceMerge_ReportMonthFilter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fm.jsonl")

	seed := `{"pr_url":"https://g/pulls/4","human_identity":"eve","justification":"spec justification meeting the 20 char floor","merge_sha":"sha4","timestamp":"2026-01-15T10:00:00Z"}
{"pr_url":"https://g/pulls/5","human_identity":"eve","justification":"spec justification meeting the 20 char floor","merge_sha":"sha5","timestamp":"2026-04-15T10:00:00Z"}
`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var out, errOut bytes.Buffer
	rc := runForceMerge([]string{"report", "--path", path, "--month", "2026-01", "--json"}, &out, &errOut)
	// Jan entry has no review → pending → exit fmExitFail. Don't
	// assert rc explicitly — verify via the parsed report below.
	if rc != fmExitOK && rc != fmExitFail {
		t.Errorf("unexpected rc=%d stderr=%s", rc, errOut.String())
	}
	var rep forcemerge.AuditReport
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &rep); err != nil {
		t.Fatalf("json: %v", err)
	}
	if _, ok := rep.ByMonth["2026-04"]; ok {
		t.Errorf("month filter leaked April data: %+v", rep.ByMonth)
	}
	if _, ok := rep.ByMonth["2026-01"]; !ok {
		t.Errorf("month filter dropped January data: %+v", rep.ByMonth)
	}
}

func TestRunForceMerge_ReviewInvalidVerdict(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fm.jsonl")

	var out, errOut bytes.Buffer
	rc := runForceMerge([]string{
		"review",
		"--path", path,
		"--pr-url", "https://g/pulls/1",
		"--merge-sha", "abc",
		"--reviewer", "x",
		"--verdict", "BOGUS",
	}, &out, &errOut)
	if rc != fmExitError {
		t.Errorf("rc=%d want %d (invalid verdict)", rc, fmExitError)
	}
}

func TestRunForceMerge_ReviewMissingFlags(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runForceMerge([]string{"review", "--pr-url", "x"}, &out, &errOut)
	if rc != fmExitError {
		t.Errorf("rc=%d want %d", rc, fmExitError)
	}
}

func TestRunForceMerge_DryRunWrapper(t *testing.T) {
	var out, errOut bytes.Buffer
	err := runForceMergeWithDryRun([]string{"help"}, &out, &errOut, true)
	if err != nil {
		t.Errorf("dry-run help returned err=%v", err)
	}
}

func TestRunForceMerge_DryRunWrapperExitsAsError(t *testing.T) {
	var out, errOut bytes.Buffer
	// Invalid subcommand → fmExitError → should wrap as errExit.
	err := runForceMergeWithDryRun([]string{"bogus"}, &out, &errOut, true)
	if err == nil {
		t.Errorf("bogus subcommand should produce err")
	}
}

func TestRunForceMerge_DryRunWrapperFailReturnsNil(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fm.jsonl")
	seed := `{"pr_url":"https://g/pulls/9","human_identity":"frank","justification":"spec justification meeting the 20 char floor","merge_sha":"sha9","timestamp":"2026-05-01T10:00:00Z"}
`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Pending review → rc=fmExitFail → wrapper returns nil (not errExit).
	var out, errOut bytes.Buffer
	if err := runForceMergeWithDryRun([]string{"report", "--path", path}, &out, &errOut, true); err != nil {
		t.Errorf("pending report should not produce err, got %v", err)
	}
}
