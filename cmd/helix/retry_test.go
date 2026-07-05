package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/retry"
)

// ============================================================================
// parseRetryFlags tests
// ============================================================================

func TestParseRetryFlags_Defaults(t *testing.T) {
	f, rc := parseRetryFlags([]string{})
	if rc != retryExitOK {
		t.Fatalf("rc = %d, want %d", rc, retryExitOK)
	}
	if f.subcommand != "status" {
		t.Errorf("subcommand = %q, want %q", f.subcommand, "status")
	}
}

func TestParseRetryFlags_JSON(t *testing.T) {
	f, _ := parseRetryFlags([]string{"--json"})
	if !f.jsonOut {
		t.Error("jsonOut should be true")
	}
}

func TestParseRetryFlags_Policy(t *testing.T) {
	f, _ := parseRetryFlags([]string{"--policy", "forgejo"})
	if f.policyName != "forgejo" {
		t.Errorf("policyName = %q, want %q", f.policyName, "forgejo")
	}
}

func TestParseRetryFlags_PolicyMissingValue(t *testing.T) {
	_, rc := parseRetryFlags([]string{"--policy"})
	if rc != retryExitError {
		t.Errorf("rc = %d, want %d", rc, retryExitError)
	}
}

func TestParseRetryFlags_FailureRate(t *testing.T) {
	f, _ := parseRetryFlags([]string{"chaos", "--failure-rate", "0.5"})
	if f.failureRate != 0.5 {
		t.Errorf("failureRate = %f, want 0.5", f.failureRate)
	}
}

func TestParseRetryFlags_FailureRateMissingValue(t *testing.T) {
	_, rc := parseRetryFlags([]string{"chaos", "--failure-rate"})
	if rc != retryExitError {
		t.Errorf("rc = %d, want %d", rc, retryExitError)
	}
}

func TestParseRetryFlags_FailureRateOutOfRange(t *testing.T) {
	_, rc := parseRetryFlags([]string{"chaos", "--failure-rate", "1.5"})
	if rc != retryExitError {
		t.Errorf("rc = %d, want %d", rc, retryExitError)
	}
}

func TestParseRetryFlags_Duration(t *testing.T) {
	f, _ := parseRetryFlags([]string{"chaos", "--duration", "30s"})
	if f.duration != 30*time.Second {
		t.Errorf("duration = %v, want 30s", f.duration)
	}
}

func TestParseRetryFlags_DurationMissingValue(t *testing.T) {
	_, rc := parseRetryFlags([]string{"--duration"})
	if rc != retryExitError {
		t.Errorf("rc = %d, want %d", rc, retryExitError)
	}
}

func TestParseRetryFlags_InvalidDuration(t *testing.T) {
	_, rc := parseRetryFlags([]string{"--duration", "not-a-duration"})
	if rc != retryExitError {
		t.Errorf("rc = %d, want %d", rc, retryExitError)
	}
}

func TestParseRetryFlags_UnknownFlag(t *testing.T) {
	_, rc := parseRetryFlags([]string{"--bogus"})
	if rc != retryExitError {
		t.Errorf("rc = %d, want %d", rc, retryExitError)
	}
}

func TestParseRetryFlags_ChaosDefaults(t *testing.T) {
	f, _ := parseRetryFlags([]string{"chaos", "--policy", "test"})
	if f.failureRate != 0.3 {
		t.Errorf("default failureRate = %f, want 0.3", f.failureRate)
	}
	if f.duration != 60*time.Second {
		t.Errorf("default duration = %v, want 60s", f.duration)
	}
}

// ============================================================================
// runRetry — status
// ============================================================================

func TestRunRetry_Status(t *testing.T) {
	// Use a fresh registry for testing
	reg := retry.NewRegistry()
	reg.Register("test-forgejo", retry.DefaultConfig())
	// We can't easily inject this into DefaultRegistry, so test the CLI
	// with the default registry by first registering a test policy.
	dr := retry.DefaultRegistry()
	dr.Register("test-status-policy", retry.DefaultConfig())

	stdout, _, rc := runRetryCapture([]string{"status"})
	if rc != retryExitOK {
		t.Fatalf("rc = %d, want %d", rc, retryExitOK)
	}
	if !strings.Contains(stdout, "POLICY") {
		t.Errorf("stdout missing header: %s", stdout)
	}
	if !strings.Contains(stdout, "CIRCUIT") {
		t.Errorf("stdout missing circuit header: %s", stdout)
	}
}

func TestRunRetry_StatusJSON(t *testing.T) {
	dr := retry.DefaultRegistry()
	dr.Register("test-json-policy", retry.DefaultConfig())

	stdout, _, rc := runRetryCapture([]string{"status", "--json"})
	if rc != retryExitOK {
		t.Fatalf("rc = %d, want %d", rc, retryExitOK)
	}
	var report retry.StatusReport
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &report); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, stdout)
	}
	if report.TotalPolicies == 0 {
		t.Error("expected at least 1 policy in JSON report")
	}
}

func TestRunRetry_StatusEmpty(t *testing.T) {
	// Test with a fresh registry — but the CLI uses DefaultRegistry
	// which is a singleton. So we can't truly test empty. But we can
	// verify the format works.
	stdout, _, rc := runRetryCapture([]string{"status"})
	if rc != retryExitOK {
		t.Fatalf("rc = %d, want %d", rc, retryExitOK)
	}
	if !strings.Contains(stdout, "policies") {
		t.Errorf("stdout should mention policies count: %s", stdout)
	}
}

// ============================================================================
// runRetry — help
// ============================================================================

func TestRunRetry_Help(t *testing.T) {
	stdout, _, rc := runRetryCapture([]string{"help"})
	if rc != retryExitOK {
		t.Fatalf("rc = %d, want %d", rc, retryExitOK)
	}
	if !strings.Contains(stdout, "helix retry") {
		t.Errorf("stdout missing title: %s", stdout)
	}
	if !strings.Contains(stdout, "status") {
		t.Errorf("stdout missing status: %s", stdout)
	}
	if !strings.Contains(stdout, "chaos") {
		t.Errorf("stdout missing chaos: %s", stdout)
	}
	if !strings.Contains(stdout, "reset") {
		t.Errorf("stdout missing reset: %s", stdout)
	}
}

// ============================================================================
// runRetry — reset
// ============================================================================

func TestRunRetry_ResetSpecific(t *testing.T) {
	dr := retry.DefaultRegistry()
	dr.Register("test-reset-policy", retry.DefaultConfig())
	dr.RecordResult("test-reset-policy", nil)

	stdout, _, rc := runRetryCapture([]string{"reset", "--policy", "test-reset-policy"})
	if rc != retryExitOK {
		t.Fatalf("rc = %d, want %d", rc, retryExitOK)
	}
	if !strings.Contains(stdout, "Reset stats for policy") {
		t.Errorf("stdout should confirm reset: %s", stdout)
	}
	stats := dr.Get("test-reset-policy")
	if stats.Attempts != 0 {
		t.Errorf("Attempts after reset = %d, want 0", stats.Attempts)
	}
}

func TestRunRetry_ResetAll(t *testing.T) {
	dr := retry.DefaultRegistry()
	dr.Register("test-reset-all-1", retry.DefaultConfig())
	dr.Register("test-reset-all-2", retry.DefaultConfig())

	stdout, _, rc := runRetryCapture([]string{"reset"})
	if rc != retryExitOK {
		t.Fatalf("rc = %d, want %d", rc, retryExitOK)
	}
	if !strings.Contains(stdout, "Reset stats for all policies") {
		t.Errorf("stdout should confirm reset all: %s", stdout)
	}
}

func TestRunRetry_ResetUnknownPolicy(t *testing.T) {
	_, stderr, rc := runRetryCapture([]string{"reset", "--policy", "nonexistent-policy-xyz"})
	if rc != retryExitError {
		t.Fatalf("rc = %d, want %d", rc, retryExitError)
	}
	if !strings.Contains(stderr, "not found") {
		t.Errorf("stderr should mention not found: %s", stderr)
	}
}

// ============================================================================
// runRetry — chaos
// ============================================================================

func TestRunRetry_Chaos_NoEnvVar(t *testing.T) {
	t.Setenv("HELIX_CHAOS_ENABLED", "")
	_, stderr, rc := runRetryCapture([]string{"chaos", "--policy", "test"})
	if rc != retryExitError {
		t.Fatalf("rc = %d, want %d", rc, retryExitError)
	}
	if !strings.Contains(stderr, "HELIX_CHAOS_ENABLED") {
		t.Errorf("stderr should mention env var: %s", stderr)
	}
}

func TestRunRetry_Chaos_NoPolicy(t *testing.T) {
	t.Setenv("HELIX_CHAOS_ENABLED", "1")
	_, stderr, rc := runRetryCapture([]string{"chaos"})
	if rc != retryExitError {
		t.Fatalf("rc = %d, want %d", rc, retryExitError)
	}
	if !strings.Contains(stderr, "--policy is required") {
		t.Errorf("stderr should mention required flag: %s", stderr)
	}
}

func TestRunRetry_Chaos_UnknownPolicy(t *testing.T) {
	t.Setenv("HELIX_CHAOS_ENABLED", "1")
	_, stderr, rc := runRetryCapture([]string{"chaos", "--policy", "nonexistent-chaos-xyz"})
	if rc != retryExitError {
		t.Fatalf("rc = %d, want %d", rc, retryExitError)
	}
	if !strings.Contains(stderr, "not found") {
		t.Errorf("stderr should mention not found: %s", stderr)
	}
}

func TestRunRetry_Chaos_ShortDuration(t *testing.T) {
	t.Setenv("HELIX_CHAOS_ENABLED", "1")
	dr := retry.DefaultRegistry()
	dr.Register("test-chaos-short", retry.DefaultConfig())

	// Use a very short duration so the test completes quickly
	stdout, _, rc := runRetryCapture([]string{
		"chaos",
		"--policy", "test-chaos-short",
		"--duration", "100ms",
		"--failure-rate", "0.5",
	})
	if rc != retryExitOK {
		t.Fatalf("rc = %d, want %d, stdout = %s", rc, retryExitOK, stdout)
	}
	if !strings.Contains(stdout, "Chaos injection started") {
		t.Errorf("stdout should show chaos start: %s", stdout)
	}
	if !strings.Contains(stdout, "Chaos injection complete") {
		t.Errorf("stdout should show chaos complete: %s", stdout)
	}
}

// ============================================================================
// runRetry — unknown subcommand
// ============================================================================

func TestRunRetry_UnknownSubcommand(t *testing.T) {
	_, stderr, rc := runRetryCapture([]string{"bogus"})
	if rc != retryExitError {
		t.Fatalf("rc = %d, want %d", rc, retryExitError)
	}
	if !strings.Contains(stderr, "unknown subcommand") {
		t.Errorf("stderr should mention unknown subcommand: %s", stderr)
	}
}

// ============================================================================
// runRetryWithDryRun tests
// ============================================================================

func TestRunRetryWithDryRun_Status(t *testing.T) {
	var stdout, stderr strings.Builder
	err := runRetryWithDryRun([]string{"status"}, &stdout, &stderr, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunRetryWithDryRun_ErrorExit(t *testing.T) {
	var stdout, stderr strings.Builder
	err := runRetryWithDryRun([]string{"bogus"}, &stdout, &stderr, false)
	if err == nil {
		t.Fatal("expected errExit for unknown subcommand")
	}
	exitErr, ok := err.(errExit)
	if !ok {
		t.Fatalf("expected errExit, got %T: %v", err, err)
	}
	if exitErr.code != retryExitError {
		t.Errorf("errExit code = %d, want %d", exitErr.code, retryExitError)
	}
}
