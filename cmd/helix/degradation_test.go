package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseDegradationFlags_Default(t *testing.T) {
	flags, rc := parseDegradationFlags([]string{})
	if rc != degradationExitOK {
		t.Fatalf("expected rc 0, got %d", rc)
	}
	if flags.subcommand != "list" {
		t.Errorf("expected subcommand list, got %s", flags.subcommand)
	}
}

func TestParseDegradationFlags_Check(t *testing.T) {
	flags, rc := parseDegradationFlags([]string{"check", "forgejo", "down"})
	if rc != degradationExitOK {
		t.Fatalf("expected rc 0, got %d", rc)
	}
	if flags.subcommand != "check" {
		t.Errorf("expected subcommand check, got %s", flags.subcommand)
	}
	if flags.service != "forgejo" {
		t.Errorf("expected service forgejo, got %s", flags.service)
	}
	if flags.state != "down" {
		t.Errorf("expected state down, got %s", flags.state)
	}
}

func TestParseDegradationFlags_JSON(t *testing.T) {
	flags, rc := parseDegradationFlags([]string{"--json"})
	if rc != degradationExitOK {
		t.Fatalf("expected rc 0, got %d", rc)
	}
	if !flags.jsonOut {
		t.Error("expected jsonOut true")
	}
}

func TestParseDegradationFlags_Help(t *testing.T) {
	_, rc := parseDegradationFlags([]string{"--help"})
	if rc != degradationExitOK {
		t.Fatalf("expected rc 0, got %d", rc)
	}
}

func TestParseDegradationFlags_UnknownFlag(t *testing.T) {
	_, rc := parseDegradationFlags([]string{"--bogus"})
	if rc != degradationExitError {
		t.Fatalf("expected rc 2, got %d", rc)
	}
}

func TestRunDegradation_Help(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runDegradation([]string{"help"}, &stdout, &stderr)
	if rc != degradationExitOK {
		t.Fatalf("expected rc 0, got %d", rc)
	}
	if !strings.Contains(stdout.String(), "helix degradation") {
		t.Error("expected help text to contain 'helix degradation'")
	}
}

func TestRunDegradation_List_Table(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runDegradation([]string{"list"}, &stdout, &stderr)
	if rc != degradationExitOK {
		t.Fatalf("expected rc 0, got %d", rc)
	}
	out := stdout.String()
	if !strings.Contains(out, "Degradation Policy Pack") {
		t.Error("expected header in output")
	}
}

func TestRunDegradation_List_JSON(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runDegradation([]string{"list", "--json"}, &stdout, &stderr)
	if rc != degradationExitOK {
		t.Fatalf("expected rc 0, got %d", rc)
	}
	var report map[string]interface{}
	if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	total, ok := report["TotalPolicies"].(float64)
	if !ok || total == 0 {
		t.Error("expected non-zero TotalPolicies")
	}
}

func TestRunDegradation_Check_Table(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runDegradation([]string{"check", "forgejo", "down"}, &stdout, &stderr)
	if rc != degradationExitOK {
		t.Fatalf("expected rc 0, got %d", rc)
	}
	out := stdout.String()
	if !strings.Contains(out, "forgejo") {
		t.Error("expected service name in output")
	}
}

func TestRunDegradation_Check_JSON(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runDegradation([]string{"check", "chimera", "degraded", "--json"}, &stdout, &stderr)
	if rc != degradationExitOK {
		t.Fatalf("expected rc 0, got %d", rc)
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["ShouldBlock"] == nil {
		t.Error("expected ShouldBlock field in JSON")
	}
}

func TestRunDegradation_Check_MissingArgs(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runDegradation([]string{"check", "forgejo"}, &stdout, &stderr)
	if rc != degradationExitError {
		t.Fatalf("expected rc 2 for missing state, got %d", rc)
	}
}

func TestRunDegradation_Check_NoPolicy(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runDegradation([]string{"check", "bogus-service", "down"}, &stdout, &stderr)
	if rc != degradationExitError {
		t.Fatalf("expected rc 2 for no policy, got %d", rc)
	}
}

func TestRunDegradation_UnknownSubcommand(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runDegradation([]string{"bogus"}, &stdout, &stderr)
	if rc != degradationExitError {
		t.Fatalf("expected rc 2, got %d", rc)
	}
}

func TestRunDegradationWithDryRun_GlobalDryRun(t *testing.T) {
	var stdout, stderr strings.Builder
	err := runDegradationWithDryRun([]string{"list"}, &stdout, &stderr, true)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(stdout.String(), "Degradation Policy Pack") {
		t.Error("expected list output even with dry-run")
	}
}

func TestRunDegradationWithDryRun_NoGlobalDryRun(t *testing.T) {
	var stdout, stderr strings.Builder
	err := runDegradationWithDryRun([]string{"list"}, &stdout, &stderr, false)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}
