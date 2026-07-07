package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCI is the dispatcher; helpers below exercise individual subcommands.

func TestRunCI_Help(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runCI([]string{"--help"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("help exit code: got %d, want 0", rc)
	}
	if !strings.Contains(stdout.String(), "Forgejo Actions test workflow generator") {
		t.Errorf("expected help header in stdout, got: %q", stdout.String())
	}
}

func TestRunCI_Defaults(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runCI([]string{"defaults"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("defaults exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	out := stdout.String()
	wantSubstrings := []string{
		"Spec §12.5 defaults",
		"workflow_name",
		"runner_image",
		"go_version",
		"coverage_threshold",
	}
	for _, w := range wantSubstrings {
		if !strings.Contains(out, w) {
			t.Errorf("expected %q in defaults output; got:\n%s", w, out)
		}
	}
}

func TestRunCI_DefaultsJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runCI([]string{"defaults", "--json"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("defaults --json exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(out, "{") || !strings.HasSuffix(out, "}") {
		t.Errorf("expected JSON object, got: %s", out)
	}
	for _, key := range []string{`"name"`, `"runner"`, `"go_version"`, `"forgejo_image"`} {
		if !strings.Contains(out, key) {
			t.Errorf("expected key %s in JSON output: %s", key, out)
		}
	}
}

func TestRunCI_Render(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runCI([]string{"render"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("render exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	out := stdout.String()
	// YAML should contain key workflow sections
	for _, want := range []string{"name:", "jobs:", "on:", "runs-on:"} {
		if !strings.Contains(out, want) {
			t.Errorf("render output missing %q; got:\n%s", want, out)
		}
	}
}

func TestRunCI_RenderJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runCI([]string{"render", "--json"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("render --json exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(out, "{") {
		t.Errorf("expected JSON object, got: %s", out)
	}
	if !strings.Contains(out, `"unit_job":"unit"`) {
		t.Errorf("expected unit_job key in JSON: %s", out)
	}
	if !strings.Contains(out, `"coverage_gate":true`) {
		t.Errorf("expected coverage_gate true in JSON: %s", out)
	}
	if !strings.Contains(out, `"forgejo_service":true`) {
		t.Errorf("expected forgejo_service true in JSON: %s", out)
	}
}

func TestRunCI_Validate_DefaultPathMissing(t *testing.T) {
	// Test from a temp directory so .forgejo/workflows/test.yml doesn't exist
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldCwd) }()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var stdout, stderr bytes.Buffer
	rc := runCI([]string{"validate"}, &stdout, &stderr)
	if rc == 0 {
		t.Fatalf("expected non-zero exit when .forgejo/workflows/test.yml missing; got 0; stderr=%s", stderr.String())
	}
}

func TestRunCI_Validate_CustomPathOK(t *testing.T) {
	tmpDir := t.TempDir()
	wfPath := filepath.Join(tmpDir, "test.yml")
	// Minimal valid spec §12.5 workflow YAML
	yaml := `name: Test
on:
  push:
    branches: [master, main]
  pull_request:
    types: [opened, synchronize]
jobs:
  unit:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@v4
      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: "1.24"
      - name: Coverage gate
        run: |
          go tool cover -func=coverage.out
`
	if err := os.WriteFile(wfPath, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	var stdout, stderr bytes.Buffer
	rc := runCI([]string{"validate", "--path", wfPath}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("validate exit code: got %d, want 0; stderr=%s; stdout=%s", rc, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "VALID") {
		t.Errorf("expected VALID in output; got: %s", stdout.String())
	}
}

func TestRunCI_Validate_CustomPathFails(t *testing.T) {
	tmpDir := t.TempDir()
	wfPath := filepath.Join(tmpDir, "bad.yml")
	// Missing unit job — should fail validation
	yaml := `name: Test
on:
  push:
    branches: [master]
jobs:
  integration:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@v4
`
	if err := os.WriteFile(wfPath, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	var stdout, stderr bytes.Buffer
	rc := runCI([]string{"validate", "--path", wfPath}, &stdout, &stderr)
	if rc == 0 {
		t.Fatalf("expected non-zero exit for invalid workflow; got 0")
	}
	if !strings.Contains(stdout.String(), "INVALID") {
		t.Errorf("expected INVALID in output; got: %s", stdout.String())
	}
}

func TestParseCIFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantSub  string
		wantPath string
		wantJSON bool
		wantRC   int
	}{
		{"default", []string{}, "render", "", false, 0},
		{"render sub", []string{"render"}, "render", "", false, 0},
		{"validate sub", []string{"validate"}, "validate", "", false, 0},
		{"defaults sub", []string{"defaults"}, "defaults", "", false, 0},
		{"with path", []string{"validate", "--path", "/tmp/wf.yml"}, "validate", "/tmp/wf.yml", false, 0},
		{"with path= form", []string{"validate", "--path=/tmp/wf.yml"}, "validate", "/tmp/wf.yml", false, 0},
		{"with json", []string{"render", "--json"}, "render", "", true, 0},
		{"unknown sub", []string{"badname"}, "", "", false, 2},
		{"unknown flag", []string{"render", "--badflag"}, "", "", false, 2},
		{"help", []string{"--help"}, "render", "", false, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, rc := parseCIFlags(tc.args, &bytes.Buffer{}, &bytes.Buffer{})
			if rc != tc.wantRC {
				t.Fatalf("rc: got %d, want %d", rc, tc.wantRC)
			}
			if rc != 0 {
				return
			}
			if f.subcommand != tc.wantSub {
				t.Errorf("subcommand: got %q, want %q", f.subcommand, tc.wantSub)
			}
			if f.path != tc.wantPath {
				t.Errorf("path: got %q, want %q", f.path, tc.wantPath)
			}
			if f.jsonOut != tc.wantJSON {
				t.Errorf("jsonOut: got %v, want %v", f.jsonOut, tc.wantJSON)
			}
		})
	}
}

func TestParseCIFlags_PathMissingValue(t *testing.T) {
	// --path with no value should fail
	f, rc := parseCIFlags([]string{"validate", "--path"}, &bytes.Buffer{}, &bytes.Buffer{})
	if rc != ciExitError {
		t.Fatalf("rc: got %d, want %d", rc, ciExitError)
	}
	if f.subcommand != "validate" {
		t.Errorf("subcommand: got %q, want %q", f.subcommand, "validate")
	}
}

func TestRunCIWithDryRun_PassThrough(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runCIWithDryRun([]string{"defaults"}, &stdout, &stderr, true)
	if err != nil {
		t.Fatalf("expected nil error from runCIWithDryRun on defaults; got %v", err)
	}
	if !strings.Contains(stdout.String(), "Spec §12.5 defaults") {
		t.Errorf("expected defaults output via WithDryRun; got: %s", stdout.String())
	}
}

func TestRunCIWithDryRun_PropagatesExitCode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runCIWithDryRun([]string{"validate"}, &stdout, &stderr, false) // missing default file → error
	if err == nil {
		t.Fatalf("expected non-nil error from runCIWithDryRun on missing validate file")
	}
	if exitErr, ok := err.(errExit); ok {
		if exitErr.code == 0 {
			t.Errorf("expected non-zero exit code in errExit; got %d", exitErr.code)
		}
	}
}
