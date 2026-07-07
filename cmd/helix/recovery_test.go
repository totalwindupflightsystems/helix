package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunRecovery_Help(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runRecovery([]string{"--help"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("help exit code: got %d, want 0", rc)
	}
	if !strings.Contains(stdout.String(), "Error recovery runbook") {
		t.Errorf("expected help header in stdout, got: %q", stdout.String())
	}
}

func TestRunRecovery_Components(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runRecovery([]string{"components"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("components exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "components in registry") {
		t.Errorf("expected 'components in registry' in output; got:\n%s", out)
	}
	// Should list at least one component (Forgejo, Chimera, etc.)
	if !strings.Contains(out, "  ") {
		t.Errorf("expected indented component list; got:\n%s", out)
	}
}

func TestRunRecovery_ComponentsJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runRecovery([]string{"components", "--json"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("components --json exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(out, "{") {
		t.Errorf("expected JSON object, got: %s", out)
	}
	if !strings.Contains(out, `"components":[`) {
		t.Errorf("expected components array key in JSON: %s", out)
	}
}

func TestRunRecovery_Matrix(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runRecovery([]string{"matrix"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("matrix exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Helix Recovery Matrix") {
		t.Errorf("expected matrix header in output; got:\n%s", stdout.String())
	}
}

func TestRunRecovery_MatrixJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runRecovery([]string{"matrix", "--json"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("matrix --json exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(out, "{") {
		t.Errorf("expected JSON object, got: %s", out)
	}
	if !strings.Contains(out, `"entry_count":`) {
		t.Errorf("expected entry_count key in JSON: %s", out)
	}
}

func TestRunRecovery_Lookup_ByID(t *testing.T) {
	var stdout, stderr bytes.Buffer
	// FJ-001 is a typical Forgejo failure ID in the registry
	rc := runRecovery([]string{"lookup", "--id", "FJ-001"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("lookup --id exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Forgejo") && !strings.Contains(stdout.String(), "forgejo") {
		t.Errorf("expected Forgejo reference in lookup output; got:\n%s", stdout.String())
	}
}

func TestRunRecovery_Lookup_ByID_NotFound(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runRecovery([]string{"lookup", "--id", "ZZZ-999"}, &stdout, &stderr)
	if rc == 0 {
		t.Fatalf("expected non-zero exit for missing ID; got 0")
	}
	if !strings.Contains(stderr.String(), "no entry with id") {
		t.Errorf("expected error message in stderr; got: %s", stderr.String())
	}
}

func TestRunRecovery_Lookup_ByComponent(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runRecovery([]string{"lookup", "--component", "Forgejo"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("lookup --component exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Forgejo") {
		t.Errorf("expected Forgejo in lookup output; got:\n%s", stdout.String())
	}
}

func TestRunRecovery_Lookup_NoFilter(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runRecovery([]string{"lookup"}, &stdout, &stderr)
	if rc == 0 {
		t.Fatalf("expected non-zero exit when no --id, --component, or --mode specified")
	}
	if !strings.Contains(stderr.String(), "requires --id") {
		t.Errorf("expected 'requires --id' error; got: %s", stderr.String())
	}
}

func TestRunRecovery_Scenarios(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runRecovery([]string{"scenarios"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("scenarios exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "DR scenarios") {
		t.Errorf("expected DR scenarios header; got:\n%s", out)
	}
}

func TestRunRecovery_ScenariosJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runRecovery([]string{"scenarios", "--json"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("scenarios --json exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(out, "{") {
		t.Errorf("expected JSON object, got: %s", out)
	}
	if !strings.Contains(out, `"scenarios":[`) {
		t.Errorf("expected scenarios array in JSON: %s", out)
	}
}

func TestRunRecovery_KeyRotation(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runRecovery([]string{"key-rotation"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("key-rotation exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "key rotation procedure") {
		t.Errorf("expected key rotation procedure header; got:\n%s", out)
	}
	// Spec §10.3 mandates 5 steps
	if !strings.Contains(out, "1.") || !strings.Contains(out, "5.") {
		t.Errorf("expected 5-step numbering; got:\n%s", out)
	}
}

func TestRunRecovery_KeyRotationJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runRecovery([]string{"key-rotation", "--json"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("key-rotation --json exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if !strings.Contains(out, `"steps":[`) {
		t.Errorf("expected steps array in JSON: %s", out)
	}
}

func TestRunRecovery_Scaling(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runRecovery([]string{"scaling"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("scaling exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "scaling model") {
		t.Errorf("expected scaling model header; got:\n%s", out)
	}
	if !strings.Contains(out, "max_concurrent_agents") {
		t.Errorf("expected max_concurrent_agents in output; got:\n%s", out)
	}
}

func TestRunRecovery_Scaling_WithCores(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runRecovery([]string{"scaling", "--cores", "16", "--cores-per-agent", "0.8"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("scaling --cores exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "max_concurrent_agents: 20") {
		t.Errorf("expected max_concurrent_agents 20 (16/0.8); got:\n%s", out)
	}
}

func TestRunRecovery_ScalingJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runRecovery([]string{"scaling", "--json"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("scaling --json exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if !strings.Contains(out, `"max_concurrent_agents":20`) {
		t.Errorf("expected max_concurrent_agents:20 in JSON: %s", out)
	}
}

func TestRunRecovery_Severity_SEV1(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runRecovery([]string{"severity", "--severity", "SEV-1"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("severity SEV-1 exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "SEV-1") {
		t.Errorf("expected SEV-1 in output; got: %s", stdout.String())
	}
}

func TestRunRecovery_Severity_Lowercase(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runRecovery([]string{"severity", "--severity", "sev-2"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("severity sev-2 lowercase exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "SEV-2") {
		t.Errorf("expected SEV-2 in output; got: %s", stdout.String())
	}
}

func TestRunRecovery_Severity_Invalid(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runRecovery([]string{"severity", "--severity", "SEV-9"}, &stdout, &stderr)
	if rc == 0 {
		t.Fatalf("expected non-zero exit for invalid severity; got 0")
	}
}

func TestRunRecovery_Severity_NoFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runRecovery([]string{"severity"}, &stdout, &stderr)
	if rc == 0 {
		t.Fatalf("expected non-zero exit when --severity missing")
	}
}

func TestParseRecoveryFlags(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantSub   string
		wantComp  string
		wantID    string
		wantMode  string
		wantSev   string
		wantCores int
		wantCPA   float64
		wantJSON  bool
		wantRC    int
	}{
		{"default", []string{}, "matrix", "", "", "", "", 0, 0, false, 0},
		{"components sub", []string{"components"}, "components", "", "", "", "", 0, 0, false, 0},
		{"lookup with id", []string{"lookup", "--id", "FJ-001"}, "lookup", "", "FJ-001", "", "", 0, 0, false, 0},
		{"lookup with component", []string{"lookup", "--component", "Forgejo"}, "lookup", "Forgejo", "", "", "", 0, 0, false, 0},
		{"lookup with mode", []string{"lookup", "--mode", "timeout"}, "lookup", "", "", "timeout", "", 0, 0, false, 0},
		{"severity SEV-1", []string{"severity", "--severity", "SEV-1"}, "severity", "", "", "", "SEV-1", 0, 0, false, 0},
		{"scaling with cores", []string{"scaling", "--cores", "32", "--cores-per-agent", "1.0"}, "scaling", "", "", "", "", 32, 1.0, false, 0},
		{"json flag", []string{"matrix", "--json"}, "matrix", "", "", "", "", 0, 0, true, 0},
		{"unknown sub", []string{"badname"}, "", "", "", "", "", 0, 0, false, 2},
		{"unknown flag", []string{"matrix", "--bad"}, "", "", "", "", "", 0, 0, false, 2},
		{"help", []string{"--help"}, "matrix", "", "", "", "", 0, 0, false, 0},
		{"severity missing value", []string{"severity", "--severity"}, "severity", "", "", "", "", 0, 0, false, 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, rc := parseRecoveryFlags(tc.args, &bytes.Buffer{}, &bytes.Buffer{})
			if rc != tc.wantRC {
				t.Fatalf("rc: got %d, want %d", rc, tc.wantRC)
			}
			if rc != 0 {
				return
			}
			if f.subcommand != tc.wantSub {
				t.Errorf("subcommand: got %q, want %q", f.subcommand, tc.wantSub)
			}
			if f.component != tc.wantComp {
				t.Errorf("component: got %q, want %q", f.component, tc.wantComp)
			}
			if f.id != tc.wantID {
				t.Errorf("id: got %q, want %q", f.id, tc.wantID)
			}
			if f.mode != tc.wantMode {
				t.Errorf("mode: got %q, want %q", f.mode, tc.wantMode)
			}
			if f.severity != tc.wantSev {
				t.Errorf("severity: got %q, want %q", f.severity, tc.wantSev)
			}
			if f.cores != tc.wantCores {
				t.Errorf("cores: got %d, want %d", f.cores, tc.wantCores)
			}
			if f.coresPer != tc.wantCPA {
				t.Errorf("coresPer: got %v, want %v", f.coresPer, tc.wantCPA)
			}
			if f.jsonOut != tc.wantJSON {
				t.Errorf("jsonOut: got %v, want %v", f.jsonOut, tc.wantJSON)
			}
		})
	}
}

func TestRunRecoveryWithDryRun_PassThrough(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runRecoveryWithDryRun([]string{"components"}, &stdout, &stderr, true)
	if err != nil {
		t.Fatalf("expected nil error from runRecoveryWithDryRun on components; got %v", err)
	}
	if !strings.Contains(stdout.String(), "components in registry") {
		t.Errorf("expected components output via WithDryRun; got: %s", stdout.String())
	}
}
