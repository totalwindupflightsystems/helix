package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/health"
)

// ============================================================================
// parseAlertsFlags tests
// ============================================================================

func TestParseAlertsFlags_Defaults(t *testing.T) {
	f, rc := parseAlertsFlags([]string{})
	if rc != alertsExitOK {
		t.Fatalf("rc = %d, want %d", rc, alertsExitOK)
	}
	if f.subcommand != "notify" {
		t.Errorf("subcommand = %q, want %q", f.subcommand, "notify")
	}
	if len(f.notifiers) != 1 || f.notifiers[0] != "stdout" {
		t.Errorf("notifiers = %v, want [stdout]", f.notifiers)
	}
	if f.dryRun {
		t.Error("dryRun should default to false")
	}
	if f.jsonOut {
		t.Error("jsonOut should default to false")
	}
}

func TestParseAlertsFlags_DryRun(t *testing.T) {
	f, _ := parseAlertsFlags([]string{"--dry-run"})
	if !f.dryRun {
		t.Error("dryRun should be true")
	}
}

func TestParseAlertsFlags_JSON(t *testing.T) {
	f, _ := parseAlertsFlags([]string{"--json"})
	if !f.jsonOut {
		t.Error("jsonOut should be true")
	}
}

func TestParseAlertsFlags_Quiet(t *testing.T) {
	f, _ := parseAlertsFlags([]string{"--quiet"})
	if !f.quiet {
		t.Error("quiet should be true")
	}
}

func TestParseAlertsFlags_QuietShort(t *testing.T) {
	f, _ := parseAlertsFlags([]string{"-q"})
	if !f.quiet {
		t.Error("quiet should be true with -q")
	}
}

func TestParseAlertsFlags_MetricsFile(t *testing.T) {
	f, _ := parseAlertsFlags([]string{"--metrics-file", "/tmp/metrics.json"})
	if f.metricsFile != "/tmp/metrics.json" {
		t.Errorf("metricsFile = %q, want %q", f.metricsFile, "/tmp/metrics.json")
	}
}

func TestParseAlertsFlags_MetricsFileMissingValue(t *testing.T) {
	_, rc := parseAlertsFlags([]string{"--metrics-file"})
	if rc != alertsExitError {
		t.Errorf("rc = %d, want %d", rc, alertsExitError)
	}
}

func TestParseAlertsFlags_Notifier(t *testing.T) {
	f, _ := parseAlertsFlags([]string{"--notifier", "file", "--notifier", "telegram"})
	if len(f.notifiers) != 3 { // stdout default + file + telegram
		t.Fatalf("expected 3 notifiers, got %d: %v", len(f.notifiers), f.notifiers)
	}
}

func TestParseAlertsFlags_NotifierMissingValue(t *testing.T) {
	_, rc := parseAlertsFlags([]string{"--notifier"})
	if rc != alertsExitError {
		t.Errorf("rc = %d, want %d", rc, alertsExitError)
	}
}

func TestParseAlertsFlags_FilePath(t *testing.T) {
	f, _ := parseAlertsFlags([]string{"--file-path", "/custom/path.jsonl"})
	if f.filePath != "/custom/path.jsonl" {
		t.Errorf("filePath = %q, want %q", f.filePath, "/custom/path.jsonl")
	}
}

func TestParseAlertsFlags_Timeout(t *testing.T) {
	f, _ := parseAlertsFlags([]string{"--timeout", "10s"})
	if f.timeout != 10*time.Second {
		t.Errorf("timeout = %v, want 10s", f.timeout)
	}
}

func TestParseAlertsFlags_InvalidTimeout(t *testing.T) {
	_, rc := parseAlertsFlags([]string{"--timeout", "not-a-duration"})
	if rc != alertsExitError {
		t.Errorf("rc = %d, want %d", rc, alertsExitError)
	}
}

func TestParseAlertsFlags_UnknownFlag(t *testing.T) {
	_, rc := parseAlertsFlags([]string{"--bogus"})
	if rc != alertsExitError {
		t.Errorf("rc = %d, want %d", rc, alertsExitError)
	}
}

func TestParseAlertsFlags_Subcommand(t *testing.T) {
	f, _ := parseAlertsFlags([]string{"list-rules"})
	if f.subcommand != "list-rules" {
		t.Errorf("subcommand = %q, want %q", f.subcommand, "list-rules")
	}
}

// ============================================================================
// runAlerts — list-rules
// ============================================================================

func TestRunAlerts_ListRules(t *testing.T) {
	stdout, stderr, rc := runAlertsCapture([]string{"list-rules"})
	if rc != alertsExitOK {
		t.Fatalf("rc = %d, stderr = %s", rc, stderr)
	}
	if !strings.Contains(stdout, "HighCostAgent") {
		t.Errorf("stdout missing HighCostAgent: %s", stdout)
	}
	if !strings.Contains(stdout, "GateFailureSpike") {
		t.Errorf("stdout missing GateFailureSpike: %s", stdout)
	}
	if !strings.Contains(stdout, "PRStuck") {
		t.Errorf("stdout missing PRStuck: %s", stdout)
	}
	if !strings.Contains(stdout, "AgentDown") {
		t.Errorf("stdout missing AgentDown: %s", stdout)
	}
	if !strings.Contains(stdout, "CostAnomaly") {
		t.Errorf("stdout missing CostAnomaly: %s", stdout)
	}
	if !strings.Contains(stdout, "5 alert rules configured") {
		t.Errorf("stdout missing rule count: %s", stdout)
	}
}

// ============================================================================
// runAlerts — notify
// ============================================================================

func TestRunAlerts_Notify_NoMetricsFile(t *testing.T) {
	_, stderr, rc := runAlertsCapture([]string{"notify"})
	if rc != alertsExitError {
		t.Fatalf("rc = %d, want %d", rc, alertsExitError)
	}
	if !strings.Contains(stderr, "--metrics-file is required") {
		t.Errorf("stderr missing required message: %s", stderr)
	}
}

func TestRunAlerts_Notify_MissingFile(t *testing.T) {
	_, stderr, rc := runAlertsCapture([]string{"--metrics-file", "/nonexistent/file.json"})
	if rc != alertsExitError {
		t.Fatalf("rc = %d, want %d", rc, alertsExitError)
	}
	if !strings.Contains(stderr, "error:") {
		t.Errorf("stderr should contain error: %s", stderr)
	}
}

func TestRunAlerts_Notify_NoCritical(t *testing.T) {
	snap := health.MetricsSnapshot{
		Timestamp:    time.Now(),
		AgentCosts:   map[string]float64{"agent-1": 1.0},  // below 5.0 threshold → resolved
		AgentUptimes: map[string]float64{"agent-1": 3600}, // running → resolved
	}
	path := encodeAlertsSnapshot(t, snap)
	stdout, stderr, rc := runAlertsCapture([]string{"--metrics-file", path})
	if rc != alertsExitOK {
		t.Fatalf("rc = %d, want %d, stderr = %s", rc, alertsExitOK, stderr)
	}
	if !strings.Contains(stdout, "resolved") {
		t.Errorf("stdout should show resolved alerts: %s", stdout)
	}
}

func TestRunAlerts_Notify_CriticalFiring(t *testing.T) {
	snap := health.MetricsSnapshot{
		Timestamp:    time.Now(),
		AgentUptimes: map[string]float64{"agent-1": 0}, // AgentDown → critical firing
	}
	path := encodeAlertsSnapshot(t, snap)
	_, _, rc := runAlertsCapture([]string{"--metrics-file", path})
	if rc != alertsExitCritical {
		t.Fatalf("rc = %d, want %d (critical firing)", rc, alertsExitCritical)
	}
}

func TestRunAlerts_Notify_DryRunNoCriticalExit(t *testing.T) {
	snap := health.MetricsSnapshot{
		Timestamp:    time.Now(),
		AgentUptimes: map[string]float64{"agent-1": 0}, // AgentDown → critical
	}
	path := encodeAlertsSnapshot(t, snap)
	_, _, rc := runAlertsCapture([]string{"--metrics-file", path, "--dry-run"})
	// Dry run should NOT exit with critical code even if critical alerts firing
	if rc != alertsExitOK {
		t.Fatalf("rc = %d, want %d (dry run should not exit critical)", rc, alertsExitOK)
	}
}

func TestRunAlerts_Notify_JSON(t *testing.T) {
	snap := health.MetricsSnapshot{
		Timestamp:  time.Now(),
		AgentCosts: map[string]float64{"agent-1": 10.0}, // HighCostAgent firing (warning)
	}
	path := encodeAlertsSnapshot(t, snap)
	stdout, _, rc := runAlertsCapture([]string{"--metrics-file", path, "--json"})
	if rc != alertsExitOK {
		t.Fatalf("rc = %d, want %d", rc, alertsExitOK)
	}
	// Verify it's valid JSON
	var report map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &report); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, stdout)
	}
	if _, ok := report["summary"]; !ok {
		t.Error("JSON missing summary field")
	}
	if _, ok := report["notified"]; !ok {
		t.Error("JSON missing notified field")
	}
}

func TestRunAlerts_Notify_Quiet(t *testing.T) {
	snap := health.MetricsSnapshot{
		Timestamp:  time.Now(),
		AgentCosts: map[string]float64{"agent-1": 10.0},
	}
	path := encodeAlertsSnapshot(t, snap)
	stdout, _, _ := runAlertsCapture([]string{"--metrics-file", path, "--quiet"})
	if stdout != "" {
		t.Errorf("quiet should suppress stdout, got: %s", stdout)
	}
}

func TestRunAlerts_Notify_FileNotifier(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "alerts.jsonl")
	snap := health.MetricsSnapshot{
		Timestamp:    time.Now(),
		AgentUptimes: map[string]float64{"agent-1": 0}, // AgentDown → critical
	}
	path := encodeAlertsSnapshot(t, snap)
	_, _, rc := runAlertsCapture([]string{
		"--metrics-file", path,
		"--notifier", "file",
		"--file-path", filePath,
	})
	if rc != alertsExitCritical {
		t.Fatalf("rc = %d, want %d", rc, alertsExitCritical)
	}
	// Verify file was written
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Error("file notifier should have written alerts")
	}
	if !strings.Contains(string(data), "AgentDown") {
		t.Errorf("file should contain AgentDown: %s", data)
	}
}

func TestRunAlerts_Notify_UnknownNotifier(t *testing.T) {
	snap := health.MetricsSnapshot{
		Timestamp: time.Now(),
	}
	path := encodeAlertsSnapshot(t, snap)
	_, stderr, rc := runAlertsCapture([]string{
		"--metrics-file", path,
		"--notifier", "bogus",
	})
	if rc != alertsExitError {
		t.Fatalf("rc = %d, want %d", rc, alertsExitError)
	}
	if !strings.Contains(stderr, "unknown notifier") {
		t.Errorf("stderr should mention unknown notifier: %s", stderr)
	}
}

func TestRunAlerts_Notify_TelegramNotConfigured(t *testing.T) {
	// Ensure env vars are not set
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TELEGRAM_CHAT_ID", "")

	snap := health.MetricsSnapshot{
		Timestamp:  time.Now(),
		AgentCosts: map[string]float64{"agent-1": 1.0}, // no critical
	}
	path := encodeAlertsSnapshot(t, snap)
	_, stderr, rc := runAlertsCapture([]string{
		"--metrics-file", path,
		"--notifier", "telegram",
	})
	// Should skip telegram (not configured) and fall back to stdout (default)
	if rc != alertsExitOK {
		t.Fatalf("rc = %d, want %d, stderr = %s", rc, alertsExitOK, stderr)
	}
	if !strings.Contains(stderr, "telegram notifier not configured") {
		t.Errorf("stderr should warn about telegram: %s", stderr)
	}
}

func TestRunAlerts_Notify_MultiNotifier(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "multi.jsonl")
	snap := health.MetricsSnapshot{
		Timestamp:    time.Now(),
		AgentUptimes: map[string]float64{"agent-1": 0}, // AgentDown → critical
	}
	path := encodeAlertsSnapshot(t, snap)
	_, stderr, rc := runAlertsCapture([]string{
		"--metrics-file", path,
		"--notifier", "stdout",
		"--notifier", "file",
		"--file-path", filePath,
	})
	if rc != alertsExitCritical {
		t.Fatalf("rc = %d, want %d", rc, alertsExitCritical)
	}
	// JSON alert lines go to stderr (stdout notifier writes to stderr by design)
	if !strings.Contains(stderr, "AgentDown") {
		t.Errorf("stderr should contain AgentDown: %s", stderr)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "AgentDown") {
		t.Errorf("file should contain AgentDown: %s", data)
	}
}

func TestRunAlerts_Help(t *testing.T) {
	stdout, _, rc := runAlertsCapture([]string{"help"})
	if rc != alertsExitOK {
		t.Fatalf("rc = %d, want %d", rc, alertsExitOK)
	}
	if !strings.Contains(stdout, "helix alerts") {
		t.Errorf("stdout missing title: %s", stdout)
	}
	if !strings.Contains(stdout, "notify") {
		t.Errorf("stdout missing notify: %s", stdout)
	}
	if !strings.Contains(stdout, "list-rules") {
		t.Errorf("stdout missing list-rules: %s", stdout)
	}
}

func TestRunAlerts_UnknownSubcommand(t *testing.T) {
	_, stderr, rc := runAlertsCapture([]string{"bogus-subcommand"})
	if rc != alertsExitError {
		t.Fatalf("rc = %d, want %d", rc, alertsExitError)
	}
	if !strings.Contains(stderr, "unknown subcommand") {
		t.Errorf("stderr should mention unknown subcommand: %s", stderr)
	}
}

// ============================================================================
// runAlertsWithDryRun tests
// ============================================================================

func TestRunAlertsWithDryRun_GlobalDryRun(t *testing.T) {
	snap := health.MetricsSnapshot{
		Timestamp:    time.Now(),
		AgentUptimes: map[string]float64{"agent-1": 0}, // critical
	}
	path := encodeAlertsSnapshot(t, snap)
	var stdout, stderr strings.Builder
	err := runAlertsWithDryRun([]string{"--metrics-file", path}, &stdout, &stderr, true)
	// Dry run → no critical exit code, so no errExit
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "DRY RUN") {
		t.Errorf("stdout should contain DRY RUN: %s", stdout.String())
	}
}

func TestRunAlertsWithDryRun_GlobalDryRunCriticalStillExits(t *testing.T) {
	// Even with global dry run, if the command itself would exit critical,
	// dry-run suppresses it. Let's verify the global flag injects --dry-run.
	snap := health.MetricsSnapshot{
		Timestamp:    time.Now(),
		AgentUptimes: map[string]float64{"agent-1": 0},
	}
	path := encodeAlertsSnapshot(t, snap)
	var stdout, stderr strings.Builder
	err := runAlertsWithDryRun([]string{"--metrics-file", path}, &stdout, &stderr, true)
	if err != nil {
		t.Fatalf("dry run should not produce error exit: %v", err)
	}
}

func TestRunAlertsWithDryRun_NoGlobalDryRun(t *testing.T) {
	snap := health.MetricsSnapshot{
		Timestamp:    time.Now(),
		AgentUptimes: map[string]float64{"agent-1": 0}, // critical
	}
	path := encodeAlertsSnapshot(t, snap)
	var stdout, stderr strings.Builder
	err := runAlertsWithDryRun([]string{"--metrics-file", path}, &stdout, &stderr, false)
	if err == nil {
		t.Fatal("expected errExit for critical firing without dry run")
	}
	exitErr, ok := err.(errExit)
	if !ok {
		t.Fatalf("expected errExit, got %T: %v", err, err)
	}
	if exitErr.code != alertsExitCritical {
		t.Errorf("errExit code = %d, want %d", exitErr.code, alertsExitCritical)
	}
}

// ============================================================================
// Integration: full pipeline with real alert evaluation
// ============================================================================

func TestRunAlerts_Notify_FullPipeline(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "integration.jsonl")

	snap := health.MetricsSnapshot{
		Timestamp:          time.Now(),
		AgentCosts:         map[string]float64{"agent-1": 10.0, "agent-2": 2.0},
		GatePassRates:      map[string]float64{"tier1": 0.5},
		PRCycleTimes:       map[string]float64{"repo-1": 8000},
		AgentUptimes:       map[string]float64{"agent-1": 0, "agent-2": 3600},
		CostPerPR:          map[string]float64{"repo-1": 50.0},
		WeeklyAvgCostPerPR: map[string]float64{"repo-1": 10.0},
	}
	path := encodeAlertsSnapshot(t, snap)

	_, stderr, rc := runAlertsCapture([]string{
		"--metrics-file", path,
		"--notifier", "stdout",
		"--notifier", "file",
		"--file-path", filePath,
	})

	// Should exit critical (AgentDown + GateFailureSpike are critical)
	if rc != alertsExitCritical {
		t.Fatalf("rc = %d, want %d", rc, alertsExitCritical)
	}

	// JSON alert lines go to stderr (stdout notifier writes to stderr)
	if !strings.Contains(stderr, "AgentDown") {
		t.Errorf("stderr missing AgentDown: %s", stderr)
	}
	if !strings.Contains(stderr, "GateFailureSpike") {
		t.Errorf("stderr missing GateFailureSpike: %s", stderr)
	}

	// File should have the alerts
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "AgentDown") {
		t.Errorf("file missing AgentDown: %s", data)
	}
}
