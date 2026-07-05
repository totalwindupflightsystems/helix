// Tests for cmd/helix/pipeline.go — covers flag parsing, dry-run (validate),
// full run, show-from-file, JSON output, error paths, and the integrated
// subcommand dispatch in main.go.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/helix/pkg/coordinator"
	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

// -----------------------------------------------------------------------------
// trustTierFromString — small but security-relevant mapping
// -----------------------------------------------------------------------------

func TestTrustTierFromString(t *testing.T) {
	tests := []struct {
		in   string
		want trust.TrustTier
	}{
		{"provisional", trust.TierProvisional},
		{"Provisional", trust.TierProvisional},
		{" PROVISIONAL ", trust.TierProvisional},
		{"observed", trust.TierObserved},
		{"trusted", trust.TierTrusted},
		{"veteran", trust.TierVeteran},
		{"", trust.TierProvisional},      // empty defaults safely
		{"bogus", trust.TierProvisional}, // unknown defaults safely
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := trustTierFromString(tc.in); got != tc.want {
				t.Errorf("trustTierFromString(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// parsePipelineSpecFile — round-trip + error paths
// -----------------------------------------------------------------------------

func TestParsePipelineSpecFile_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.json")
	spec := pipelineSpec{
		PRURL:        "https://forgejo.local/helix/helix/pulls/42",
		AgentID:      "agent-7",
		AgentTier:    "trusted",
		CommitMsg:    "feat: wire pipeline CLI",
		Diff:         "diff --git a/x b/x\n+new line",
		ChangedFiles: []string{"cmd/helix/pipeline.go", "cmd/helix/pipeline_test.go"},
	}
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writeFile: %v", err)
	}

	got, err := parsePipelineSpecFile(path)
	if err != nil {
		t.Fatalf("parsePipelineSpecFile: %v", err)
	}
	if got.PRURL != spec.PRURL {
		t.Errorf("PRURL = %q, want %q", got.PRURL, spec.PRURL)
	}
	if got.AgentTier != spec.AgentTier {
		t.Errorf("AgentTier = %q, want %q", got.AgentTier, spec.AgentTier)
	}
	if len(got.ChangedFiles) != 2 {
		t.Errorf("ChangedFiles len = %d, want 2", len(got.ChangedFiles))
	}
}

func TestParsePipelineSpecFile_MissingFile(t *testing.T) {
	_, err := parsePipelineSpecFile(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "read spec") {
		t.Errorf("error %q should mention 'read spec'", err.Error())
	}
}

func TestParsePipelineSpecFile_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	_, err := parsePipelineSpecFile(path)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "decode spec") {
		t.Errorf("error %q should mention 'decode spec'", err.Error())
	}
}

// -----------------------------------------------------------------------------
// pipeline run — flag parsing
// -----------------------------------------------------------------------------

func TestParsePipelineRunFlags_HappyPath(t *testing.T) {
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	f, showHelp, err := parsePipelineRunFlags([]string{"--spec", "/tmp/spec.json", "--json"}, stdout, stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if showHelp {
		t.Fatal("showHelp should be false")
	}
	if f.specPath != "/tmp/spec.json" {
		t.Errorf("specPath = %q, want /tmp/spec.json", f.specPath)
	}
	if !f.asJSON {
		t.Error("asJSON should be true")
	}
}

func TestParsePipelineRunFlags_Help(t *testing.T) {
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	_, showHelp, err := parsePipelineRunFlags([]string{"--help"}, stdout, stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !showHelp {
		t.Error("showHelp should be true on --help")
	}
}

func TestParsePipelineRunFlags_PositionalRejected(t *testing.T) {
	_, _, err := parsePipelineRunFlags([]string{"--spec", "/tmp/x.json", "extra"}, &strings.Builder{}, &strings.Builder{})
	if err == nil {
		t.Fatal("expected error for positional argument, got nil")
	}
}

// -----------------------------------------------------------------------------
// pipeline run — end-to-end with a real spec file
// -----------------------------------------------------------------------------

func TestRunPipelineRun_TableOutput(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	spec := pipelineSpec{
		PRURL:     "https://forgejo.local/helix/helix/pulls/100",
		AgentID:   "agent-table",
		AgentTier: "trusted",
		CommitMsg: "feat: table-output test",
	}
	data, _ := json.Marshal(spec)
	_ = os.WriteFile(specPath, data, 0o644)

	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	rc := runPipelineRun([]string{"--spec", specPath}, stdout, stderr)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0 (stderr: %s)", rc, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"Pipeline lifecycle result", "Decision:", "STAGE", "cost_estimate", "review"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestRunPipelineRun_JSONOutput(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	spec := pipelineSpec{
		PRURL:     "https://forgejo.local/helix/helix/pulls/101",
		AgentID:   "agent-json",
		AgentTier: "veteran",
		CommitMsg: "feat: json-output test",
	}
	data, _ := json.Marshal(spec)
	_ = os.WriteFile(specPath, data, 0o644)

	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	rc := runPipelineRun([]string{"--spec", specPath, "--json"}, stdout, stderr)
	if rc != 0 {
		t.Fatalf("rc = %d (stderr: %s)", rc, stderr.String())
	}
	// Must be valid JSON that decodes back to LifecycleResult.
	var got coordinator.LifecycleResult
	if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
		t.Fatalf("output is not valid LifecycleResult JSON: %v\nraw: %s", err, stdout.String())
	}
	if got.AgentID != spec.AgentID {
		t.Errorf("AgentID = %q, want %q", got.AgentID, spec.AgentID)
	}
}

func TestRunPipelineRun_MissingSpec(t *testing.T) {
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	rc := runPipelineRun([]string{"--spec", "/nonexistent/spec.json"}, stdout, stderr)
	if rc != 1 {
		t.Errorf("rc = %d, want 1 for missing spec file", rc)
	}
	if !strings.Contains(stderr.String(), "read spec") {
		t.Errorf("stderr = %q, want it to contain 'read spec'", stderr.String())
	}
}

func TestRunPipelineRun_NoSpecFlag(t *testing.T) {
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	rc := runPipelineRun([]string{}, stdout, stderr)
	if rc != 2 {
		t.Errorf("rc = %d, want 2 for missing --spec", rc)
	}
	if !strings.Contains(stderr.String(), "--spec is required") {
		t.Errorf("stderr = %q, want it to mention --spec required", stderr.String())
	}
}

func TestRunPipelineRun_PositionalArgsRejected(t *testing.T) {
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	rc := runPipelineRun([]string{"--spec", "/tmp/x.json", "extra"}, stdout, stderr)
	if rc != 2 {
		t.Errorf("rc = %d, want 2 for positional arg", rc)
	}
}

// -----------------------------------------------------------------------------
// pipeline show
// -----------------------------------------------------------------------------

func TestRunPipelineShow_FromFile(t *testing.T) {
	// First produce a result via run + JSON, then render it via show.
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	spec := pipelineSpec{
		PRURL:   "https://forgejo.local/helix/helix/pulls/200",
		AgentID: "agent-show",
	}
	data, _ := json.Marshal(spec)
	_ = os.WriteFile(specPath, data, 0o644)

	produceStdout, produceStderr := &strings.Builder{}, &strings.Builder{}
	if rc := runPipelineRun([]string{"--spec", specPath, "--json"}, produceStdout, produceStderr); rc != 0 {
		t.Fatalf("runPipelineRun: rc=%d stderr=%s", rc, produceStderr.String())
	}

	statePath := filepath.Join(dir, "result.json")
	if err := os.WriteFile(statePath, []byte(produceStdout.String()), 0o644); err != nil {
		t.Fatalf("writeFile: %v", err)
	}

	showStdout, showStderr := &strings.Builder{}, &strings.Builder{}
	if rc := runPipelineShow([]string{statePath}, showStdout, showStderr); rc != 0 {
		t.Fatalf("runPipelineShow: rc=%d stderr=%s", rc, showStderr.String())
	}
	if !strings.Contains(showStdout.String(), "agent-show") {
		t.Errorf("show output missing agent id\nfull:\n%s", showStdout.String())
	}
}

func TestRunPipelineShow_MissingPath(t *testing.T) {
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	rc := runPipelineShow([]string{}, stdout, stderr)
	if rc != 2 {
		t.Errorf("rc = %d, want 2 for missing path", rc)
	}
}

func TestRunPipelineShow_BadJSON(t *testing.T) {
	dir := t.TempDir()
	badPath := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(badPath, []byte("not-json"), 0o644)
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	rc := runPipelineShow([]string{badPath}, stdout, stderr)
	if rc != 1 {
		t.Errorf("rc = %d, want 1 for malformed JSON", rc)
	}
}

// -----------------------------------------------------------------------------
// pipeline validate
// -----------------------------------------------------------------------------

func TestRunPipelineValidate_TableOutput(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	spec := pipelineSpec{PRURL: "x", AgentID: "validate-agent", AgentTier: "observed"}
	data, _ := json.Marshal(spec)
	_ = os.WriteFile(specPath, data, 0o644)

	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	rc := runPipelineValidate([]string{"--spec", specPath}, stdout, stderr)
	if rc != 0 {
		t.Fatalf("rc = %d (stderr: %s)", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "validate-agent") {
		t.Errorf("output missing agent id\nfull:\n%s", stdout.String())
	}
}

func TestRunPipelineValidate_JSONOutput(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	spec := pipelineSpec{PRURL: "x", AgentID: "validate-json"}
	data, _ := json.Marshal(spec)
	_ = os.WriteFile(specPath, data, 0o644)

	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	rc := runPipelineValidate([]string{"--spec", specPath, "--json"}, stdout, stderr)
	if rc != 0 {
		t.Fatalf("rc = %d (stderr: %s)", rc, stderr.String())
	}
	var got coordinator.LifecycleResult
	if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
		t.Fatalf("validate JSON output not parseable: %v", err)
	}
}

func TestRunPipelineValidate_MissingSpec(t *testing.T) {
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	rc := runPipelineValidate([]string{"--spec", "/nope.json"}, stdout, stderr)
	if rc != 1 {
		t.Errorf("rc = %d, want 1 for missing spec", rc)
	}
}

func TestRunPipelineValidate_NoSpecFlag(t *testing.T) {
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	rc := runPipelineValidate([]string{}, stdout, stderr)
	if rc != 2 {
		t.Errorf("rc = %d, want 2 for missing --spec", rc)
	}
}

// -----------------------------------------------------------------------------
// runPipeline — top-level subcommand dispatcher
// -----------------------------------------------------------------------------

func TestRunPipeline_NoArgs_PrintsUsage(t *testing.T) {
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	rc := runPipeline([]string{}, stdout, stderr)
	if rc != 0 {
		t.Errorf("rc = %d, want 0 (usage should exit 0)", rc)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Errorf("stdout should contain Usage:\n%s", stdout.String())
	}
}

func TestRunPipeline_Help(t *testing.T) {
	stdout, _ := &strings.Builder{}, &strings.Builder{}
	rc := runPipeline([]string{"help"}, stdout, &strings.Builder{})
	if rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
}

func TestRunPipeline_UnknownSubcommand(t *testing.T) {
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	rc := runPipeline([]string{"frobnicate"}, stdout, stderr)
	if rc != 2 {
		t.Errorf("rc = %d, want 2", rc)
	}
	if !strings.Contains(stderr.String(), "unknown subcommand") {
		t.Errorf("stderr should mention unknown subcommand\n%s", stderr.String())
	}
}

// -----------------------------------------------------------------------------
// pipelineExitCode + pipelineStageDetail — small helpers
// -----------------------------------------------------------------------------

func TestPipelineExitCode_NoFailure_ReturnsZero(t *testing.T) {
	r := &coordinator.LifecycleResult{
		Stages: []coordinator.StageResult{
			{Name: coordinator.StageCostEstimate, Status: coordinator.StatusPassed},
		},
	}
	if got := pipelineExitCode(r); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestPipelineExitCode_HasFailure_ReturnsOne(t *testing.T) {
	r := &coordinator.LifecycleResult{
		Stages: []coordinator.StageResult{
			{Name: coordinator.StageCostEstimate, Status: coordinator.StatusFailed},
		},
	}
	if got := pipelineExitCode(r); got != 1 {
		t.Errorf("got %d, want 1", got)
	}
}

func TestPipelineStageDetail(t *testing.T) {
	tests := []struct {
		name string
		in   coordinator.StageResult
		want string
	}{
		{
			name: "error wins",
			in:   coordinator.StageResult{Error: "boom", Message: "later"},
			want: "boom",
		},
		{
			name: "message when no error",
			in:   coordinator.StageResult{Message: "all good"},
			want: "all good",
		},
		{
			name: "skipped marker",
			in:   coordinator.StageResult{Skipped: true},
			want: "skipped",
		},
		{
			name: "dash fallback",
			in:   coordinator.StageResult{},
			want: "-",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := pipelineStageDetail(tc.in); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// printPipelineUsage — should never panic, should mention all subcommands
// -----------------------------------------------------------------------------

func TestPrintPipelineUsage(t *testing.T) {
	out := &strings.Builder{}
	printPipelineUsage(out)
	for _, want := range []string{"run", "show", "validate", "--spec", "--json"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("usage missing %q", want)
		}
	}
}
