package health

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// StdoutNotifier tests
// ============================================================================

func TestStdoutNotifier_Name(t *testing.T) {
	n := NewStdoutNotifier(nil)
	if n.Name() != "stdout" {
		t.Fatalf("Name() = %q, want %q", n.Name(), "stdout")
	}
}

func TestStdoutNotifier_DefaultWriter(t *testing.T) {
	// NewStdoutNotifier(nil) should default to os.Stderr — we can't easily
	// intercept os.Stderr here, so just verify the field is non-nil.
	n := NewStdoutNotifier(nil)
	if n.W == nil {
		t.Fatal("W should default to os.Stderr, got nil")
	}
}

func TestStdoutNotifier_Send(t *testing.T) {
	var buf bytes.Buffer
	n := NewStdoutNotifier(&buf)
	alert := AlertResult{
		Rule:      AlertRule{Name: "TestAlert", Severity: AlertWarning},
		State:     AlertFiring,
		Value:     42.0,
		Threshold: 10.0,
	}
	if err := n.Send(context.Background(), alert); err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "TestAlert") {
		t.Errorf("output doesn't contain alert name: %s", output)
	}
	if !strings.Contains(output, "\"firing\"") {
		t.Errorf("output doesn't contain firing state: %s", output)
	}
	// Should be valid JSON line
	var decoded AlertResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, output)
	}
	if decoded.Rule.Name != "TestAlert" {
		t.Errorf("decoded name = %q, want %q", decoded.Rule.Name, "TestAlert")
	}
}

func TestStdoutNotifier_SendContextCancelled(t *testing.T) {
	var buf bytes.Buffer
	n := NewStdoutNotifier(&buf)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	alert := AlertResult{Rule: AlertRule{Name: "X"}, State: AlertFiring}
	if err := n.Send(ctx, alert); err == nil {
		t.Fatal("expected context cancelled error, got nil")
	}
}

// ============================================================================
// FileNotifier tests
// ============================================================================

func TestFileNotifier_Send(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alerts.jsonl")
	n, err := NewFileNotifier(path)
	if err != nil {
		t.Fatalf("NewFileNotifier: %v", err)
	}
	if n.Name() != "file" {
		t.Fatalf("Name() = %q, want %q", n.Name(), "file")
	}
	if n.Path() != path {
		t.Fatalf("Path() = %q, want %q", n.Path(), path)
	}

	alert := AlertResult{
		Rule:      AlertRule{Name: "FileAlert", Severity: AlertCritical},
		State:     AlertFiring,
		Value:     100.0,
		Threshold: 50.0,
		FiredAt:   time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC),
	}
	if err := n.Send(context.Background(), alert); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Read back and verify
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("file is empty")
	}
	var decoded AlertResult
	if err := json.Unmarshal(bytes.TrimRight(data, "\n"), &decoded); err != nil {
		t.Fatalf("invalid JSON in file: %v\ndata: %s", err, data)
	}
	if decoded.Rule.Name != "FileAlert" {
		t.Errorf("decoded name = %q, want %q", decoded.Rule.Name, "FileAlert")
	}
}

func TestFileNotifier_MultipleSends(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "multi.jsonl")
	n, err := NewFileNotifier(path)
	if err != nil {
		t.Fatalf("NewFileNotifier: %v", err)
	}
	for i := 0; i < 3; i++ {
		alert := AlertResult{
			Rule:  AlertRule{Name: "Alert", Severity: AlertWarning},
			State: AlertFiring,
		}
		if err := n.Send(context.Background(), alert); err != nil {
			t.Fatalf("Send %d: %v", i, err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
}

func TestFileNotifier_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "deeper", "alerts.jsonl")
	n, err := NewFileNotifier(path)
	if err != nil {
		t.Fatalf("NewFileNotifier should create parent dirs: %v", err)
	}
	if n.Path() != path {
		t.Fatalf("Path = %q, want %q", n.Path(), path)
	}
}

func TestFileNotifier_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "perm.jsonl")
	_, err := NewFileNotifier(path)
	if err != nil {
		t.Fatalf("NewFileNotifier: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	mode := info.Mode().Perm()
	if mode != 0o600 {
		t.Errorf("file mode = %o, want 0o600", mode)
	}
}

func TestFileNotifier_DefaultPath(t *testing.T) {
	// Can't easily test the default path without messing with HOME,
	// but we can verify it doesn't panic when path is empty and HOME is set.
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	n, err := NewFileNotifier("")
	if err != nil {
		t.Fatalf("NewFileNotifier(\"\"): %v", err)
	}
	expected := filepath.Join(dir, ".helix", "alerts.jsonl")
	if n.Path() != expected {
		t.Errorf("Path = %q, want %q", n.Path(), expected)
	}
}

func TestFileNotifier_ContextCancelled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ctx.jsonl")
	n, err := NewFileNotifier(path)
	if err != nil {
		t.Fatalf("NewFileNotifier: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := n.Send(ctx, AlertResult{Rule: AlertRule{Name: "X"}, State: AlertFiring}); err == nil {
		t.Fatal("expected context cancelled error")
	}
}

// ============================================================================
// MultiNotifier tests
// ============================================================================

func TestMultiNotifier_Name(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	m := NewMultiNotifier(NewStdoutNotifier(&buf1), NewStdoutNotifier(&buf2))
	name := m.Name()
	if !strings.Contains(name, "stdout") {
		t.Errorf("Name should contain child names: %s", name)
	}
	if !strings.Contains(name, "multi") {
		t.Errorf("Name should start with multi: %s", name)
	}
}

func TestMultiNotifier_Send_AllSucceed(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	m := NewMultiNotifier(NewStdoutNotifier(&buf1), NewStdoutNotifier(&buf2))
	alert := AlertResult{Rule: AlertRule{Name: "Multi"}, State: AlertFiring}
	if err := m.Send(context.Background(), alert); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !strings.Contains(buf1.String(), "Multi") {
		t.Error("first notifier didn't receive alert")
	}
	if !strings.Contains(buf2.String(), "Multi") {
		t.Error("second notifier didn't receive alert")
	}
}

func TestMultiNotifier_Send_PartialFailure(t *testing.T) {
	var buf bytes.Buffer
	m := NewMultiNotifier(NewStdoutNotifier(&buf))
	// Add a notifier that always fails
	m.Add(&failingNotifier{})
	alert := AlertResult{Rule: AlertRule{Name: "Partial"}, State: AlertFiring}
	err := m.Send(context.Background(), alert)
	if err == nil {
		t.Fatal("expected error from failing notifier")
	}
	// The successful notifier should still have received it
	if !strings.Contains(buf.String(), "Partial") {
		t.Error("successful notifier should still receive alert")
	}
	if !strings.Contains(err.Error(), "failing") {
		t.Errorf("error should mention failing notifier: %v", err)
	}
}

func TestMultiNotifier_Add(t *testing.T) {
	m := NewMultiNotifier()
	if len(m.Notifiers()) != 0 {
		t.Fatalf("expected 0 notifiers, got %d", len(m.Notifiers()))
	}
	var buf bytes.Buffer
	m.Add(NewStdoutNotifier(&buf))
	if len(m.Notifiers()) != 1 {
		t.Fatalf("expected 1 notifier, got %d", len(m.Notifiers()))
	}
}

func TestMultiNotifier_Empty(t *testing.T) {
	m := NewMultiNotifier()
	err := m.Send(context.Background(), AlertResult{Rule: AlertRule{Name: "X"}, State: AlertFiring})
	if err != nil {
		t.Fatalf("empty multi should not error: %v", err)
	}
}

// failingNotifier always returns an error on Send.
type failingNotifier struct{}

func (f *failingNotifier) Name() string { return "failing" }
func (f *failingNotifier) Send(ctx context.Context, alert AlertResult) error {
	return context.DeadlineExceeded
}

// ============================================================================
// TelegramNotifier tests
// ============================================================================

func TestTelegramNotifier_Name(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
	t.Setenv("TELEGRAM_CHAT_ID", "test-chat")
	n := NewTelegramNotifier()
	if n == nil {
		t.Fatal("NewTelegramNotifier returned nil with env vars set")
	}
	if n.Name() != "telegram" {
		t.Fatalf("Name = %q, want %q", n.Name(), "telegram")
	}
}

func TestTelegramNotifier_NilWhenNoEnv(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TELEGRAM_CHAT_ID", "")
	n := NewTelegramNotifier()
	if n != nil {
		t.Fatal("expected nil when env vars not set")
	}
}

func TestTelegramNotifier_NilWhenOnlyToken(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "token")
	t.Setenv("TELEGRAM_CHAT_ID", "")
	n := NewTelegramNotifier()
	if n != nil {
		t.Fatal("expected nil when only token set")
	}
}

func TestTelegramNotifier_NilWhenOnlyChatID(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TELEGRAM_CHAT_ID", "chat")
	n := NewTelegramNotifier()
	if n != nil {
		t.Fatal("expected nil when only chat ID set")
	}
}

func TestTelegramNotifier_FormatMessage(t *testing.T) {
	n := &TelegramNotifier{BotToken: "tok", ChatID: "chat"}
	alert := AlertResult{
		Rule: AlertRule{
			Name:       "HighCostAgent",
			Severity:   AlertWarning,
			Annotation: "Agent spending too much",
		},
		State:     AlertFiring,
		Value:     10.5,
		Threshold: 5.0,
		FiredAt:   time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC),
		Labels:    map[string]string{"agent": "bot-1"},
	}
	msg := n.formatTelegramMessage(alert)
	if !strings.Contains(msg, "HighCostAgent") {
		t.Errorf("message missing alert name: %s", msg)
	}
	if !strings.Contains(msg, "firing") {
		t.Errorf("message missing state: %s", msg)
	}
	if !strings.Contains(msg, "10.50") {
		t.Errorf("message missing value: %s", msg)
	}
	if !strings.Contains(msg, "agent=bot-1") {
		t.Errorf("message missing labels: %s", msg)
	}
}

func TestTelegramNotifier_SendMissingConfig(t *testing.T) {
	n := &TelegramNotifier{}
	err := n.Send(context.Background(), AlertResult{Rule: AlertRule{Name: "X"}, State: AlertFiring})
	if err == nil {
		t.Fatal("expected error with missing config")
	}
	if !strings.Contains(err.Error(), "TELEGRAM_BOT_TOKEN") {
		t.Errorf("error should mention missing env vars: %v", err)
	}
}

// ============================================================================
// NotifyEngine tests
// ============================================================================

func TestNotifyEngine_EvaluateAndNotify(t *testing.T) {
	var buf bytes.Buffer
	engine := NewAlertEngine()
	notifier := NewStdoutNotifier(&buf)
	ne := NewNotifyEngine(engine, notifier)

	snapshot := MetricsSnapshot{
		Timestamp:    time.Now(),
		AgentCosts:   map[string]float64{"agent-1": 10.0}, // > 5.0 threshold → firing
		AgentUptimes: map[string]float64{"agent-1": 3600},
	}
	report := ne.EvaluateAndNotify(context.Background(), snapshot)
	if report.Summary.Total == 0 {
		t.Fatal("expected at least some alert results")
	}
	if report.Notified == 0 {
		t.Error("expected at least one notification sent")
	}
	if !strings.Contains(buf.String(), "HighCostAgent") {
		t.Errorf("stdout should contain HighCostAgent: %s", buf.String())
	}
}

func TestNotifyEngine_DryRun(t *testing.T) {
	var buf bytes.Buffer
	engine := NewAlertEngine()
	notifier := NewStdoutNotifier(&buf)
	ne := NewNotifyEngine(engine, notifier).WithDryRun(true)

	snapshot := MetricsSnapshot{
		Timestamp:  time.Now(),
		AgentCosts: map[string]float64{"agent-1": 10.0},
	}
	report := ne.EvaluateAndNotify(context.Background(), snapshot)
	if !report.DryRun {
		t.Error("report should show DryRun=true")
	}
	if report.Notified == 0 {
		t.Error("dry-run should still count notified alerts")
	}
	if buf.Len() > 0 {
		t.Errorf("dry-run should not send to notifier, but got: %s", buf.String())
	}
}

func TestNotifyEngine_OnlyFiring(t *testing.T) {
	var buf bytes.Buffer
	engine := NewAlertEngine()
	notifier := NewStdoutNotifier(&buf)
	ne := NewNotifyEngine(engine, notifier) // default: onlyFiring=true

	snapshot := MetricsSnapshot{
		Timestamp:    time.Now(),
		AgentCosts:   map[string]float64{"agent-1": 3.0}, // below threshold → resolved
		AgentUptimes: map[string]float64{"agent-1": 3600},
	}
	report := ne.EvaluateAndNotify(context.Background(), snapshot)
	if report.Notified != 0 {
		t.Errorf("expected 0 notified (all resolved), got %d", report.Notified)
	}
	if report.Skipped == 0 {
		t.Error("expected some skipped (resolved) alerts")
	}
}

func TestNotifyEngine_AllAlerts(t *testing.T) {
	var buf bytes.Buffer
	engine := NewAlertEngine()
	notifier := NewStdoutNotifier(&buf)
	ne := NewNotifyEngine(engine, notifier).WithOnlyFiring(false)

	snapshot := MetricsSnapshot{
		Timestamp:  time.Now(),
		AgentCosts: map[string]float64{"agent-1": 3.0}, // resolved
	}
	report := ne.EvaluateAndNotify(context.Background(), snapshot)
	if report.Notified == 0 {
		t.Error("expected notified alerts even when resolved (onlyFiring=false)")
	}
}

func TestNotifyEngine_WithMultiNotifier(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	engine := NewAlertEngine()
	multi := NewMultiNotifier(NewStdoutNotifier(&buf1), NewStdoutNotifier(&buf2))
	ne := NewNotifyEngine(engine, multi)

	snapshot := MetricsSnapshot{
		Timestamp:    time.Now(),
		AgentCosts:   map[string]float64{"agent-1": 10.0},
		AgentUptimes: map[string]float64{"agent-1": 0}, // AgentDown firing
	}
	report := ne.EvaluateAndNotify(context.Background(), snapshot)
	if len(report.Notifiers) != 2 {
		t.Errorf("expected 2 notifiers in report, got %d", len(report.Notifiers))
	}
	if report.Notified == 0 {
		t.Error("expected at least one notification")
	}
	if !strings.Contains(buf1.String(), "AgentDown") {
		t.Errorf("buf1 missing AgentDown: %s", buf1.String())
	}
	if !strings.Contains(buf2.String(), "AgentDown") {
		t.Errorf("buf2 missing AgentDown: %s", buf2.String())
	}
}

func TestNotifyEngine_NilNotifier(t *testing.T) {
	engine := NewAlertEngine()
	ne := NewNotifyEngine(engine, nil)
	snapshot := MetricsSnapshot{
		Timestamp:  time.Now(),
		AgentCosts: map[string]float64{"agent-1": 10.0},
	}
	report := ne.EvaluateAndNotify(context.Background(), snapshot)
	// Should not panic, just count without sending
	if report.Notified == 0 {
		t.Error("nil notifier should still count notified alerts")
	}
	if len(report.Errors) != 0 {
		t.Errorf("nil notifier should not produce errors, got: %v", report.Errors)
	}
}

func TestNotifyEngine_ErrorsCollected(t *testing.T) {
	engine := NewAlertEngine()
	multi := NewMultiNotifier(&failingNotifier{})
	ne := NewNotifyEngine(engine, multi)

	snapshot := MetricsSnapshot{
		Timestamp:  time.Now(),
		AgentCosts: map[string]float64{"agent-1": 10.0},
	}
	report := ne.EvaluateAndNotify(context.Background(), snapshot)
	if len(report.Errors) == 0 {
		t.Error("expected errors from failing notifier")
	}
}

// ============================================================================
// FormatNotifyReport tests
// ============================================================================

func TestFormatNotifyReport_Normal(t *testing.T) {
	report := NotifyReport{
		Summary: AlertSummary{
			Total:   3,
			Firing:  2,
			Warning: 2,
		},
		Notified:  2,
		Skipped:   1,
		Notifiers: []string{"stdout"},
	}
	output := FormatNotifyReport(report)
	if !strings.Contains(output, "2 warning alerts firing") {
		t.Errorf("output missing firing summary: %s", output)
	}
	if !strings.Contains(output, "Notified 2") {
		t.Errorf("output missing notified count: %s", output)
	}
}

func TestFormatNotifyReport_DryRun(t *testing.T) {
	report := NotifyReport{
		Summary:   AlertSummary{Total: 1, Firing: 1, Critical: 1},
		Notified:  1,
		DryRun:    true,
		Notifiers: []string{"telegram"},
	}
	output := FormatNotifyReport(report)
	if !strings.Contains(output, "DRY RUN") {
		t.Errorf("output missing DRY RUN marker: %s", output)
	}
}

func TestFormatNotifyReport_WithErrors(t *testing.T) {
	report := NotifyReport{
		Summary:   AlertSummary{Total: 1, Firing: 1},
		Notified:  0,
		Errors:    []string{"telegram: context deadline exceeded"},
		Notifiers: []string{"telegram"},
	}
	output := FormatNotifyReport(report)
	if !strings.Contains(output, "Errors (1)") {
		t.Errorf("output missing errors section: %s", output)
	}
	if !strings.Contains(output, "context deadline exceeded") {
		t.Errorf("output missing error detail: %s", output)
	}
}

// ============================================================================
// LoadMetricsSnapshotFromJSON tests
// ============================================================================

func TestLoadMetricsSnapshotFromJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.json")
	snap := MetricsSnapshot{
		Timestamp:          time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC),
		AgentCosts:         map[string]float64{"agent-1": 10.5},
		GatePassRates:      map[string]float64{"tier1": 0.5},
		PRCycleTimes:       map[string]float64{"repo-1": 8000},
		AgentUptimes:       map[string]float64{"agent-1": 0},
		CostPerPR:          map[string]float64{"repo-1": 50.0},
		WeeklyAvgCostPerPR: map[string]float64{"repo-1": 10.0},
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := LoadMetricsSnapshotFromJSON(path)
	if err != nil {
		t.Fatalf("LoadMetricsSnapshotFromJSON: %v", err)
	}
	if loaded.AgentCosts["agent-1"] != 10.5 {
		t.Errorf("AgentCosts[agent-1] = %v, want 10.5", loaded.AgentCosts["agent-1"])
	}
	if loaded.GatePassRates["tier1"] != 0.5 {
		t.Errorf("GatePassRates[tier1] = %v, want 0.5", loaded.GatePassRates["tier1"])
	}
}

func TestLoadMetricsSnapshotFromJSON_MissingFile(t *testing.T) {
	_, err := LoadMetricsSnapshotFromJSON("/nonexistent/path/file.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadMetricsSnapshotFromJSON_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not valid json}"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := LoadMetricsSnapshotFromJSON(path)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestLoadMetricsSnapshotFromJSON_EmptyMaps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")
	// Write a snapshot with nil maps
	if err := os.WriteFile(path, []byte(`{"timestamp":"2026-07-05T12:00:00Z"}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	loaded, err := LoadMetricsSnapshotFromJSON(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.AgentCosts == nil {
		t.Error("AgentCosts should be initialized to empty map")
	}
	if loaded.GatePassRates == nil {
		t.Error("GatePassRates should be initialized to empty map")
	}
}

func TestLoadMetricsSnapshotFromJSON_MissingTimestamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nots.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	loaded, err := LoadMetricsSnapshotFromJSON(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Timestamp.IsZero() {
		t.Error("Timestamp should default to time.Now() when missing")
	}
}

// ============================================================================
// NotifyReportToJSON tests
// ============================================================================

func TestNotifyReportToJSON(t *testing.T) {
	report := NotifyReport{
		Summary:   AlertSummary{Total: 2, Firing: 1, Critical: 1},
		Notified:  1,
		Notifiers: []string{"stdout"},
	}
	jsonStr, err := NotifyReportToJSON(report)
	if err != nil {
		t.Fatalf("NotifyReportToJSON: %v", err)
	}
	if !strings.Contains(jsonStr, "\"notified\"") {
		t.Errorf("JSON missing notified field: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, "\"summary\"") {
		t.Errorf("JSON missing summary field: %s", jsonStr)
	}
	// Should be valid JSON
	var decoded NotifyReport
	if err := json.Unmarshal([]byte(jsonStr), &decoded); err != nil {
		t.Fatalf("JSON is not valid: %v", err)
	}
}

// ============================================================================
// Integration test — full pipeline
// ============================================================================

func TestNotifyEngine_FullPipeline(t *testing.T) {
	var buf bytes.Buffer
	dir := t.TempDir()
	filePath := filepath.Join(dir, "alerts.jsonl")
	fileNotifier, err := NewFileNotifier(filePath)
	if err != nil {
		t.Fatalf("NewFileNotifier: %v", err)
	}
	engine := NewAlertEngine()
	multi := NewMultiNotifier(NewStdoutNotifier(&buf), fileNotifier)
	ne := NewNotifyEngine(engine, multi)

	snapshot := MetricsSnapshot{
		Timestamp:          time.Now(),
		AgentCosts:         map[string]float64{"agent-1": 10.0, "agent-2": 2.0},
		GatePassRates:      map[string]float64{"tier1": 0.5},
		PRCycleTimes:       map[string]float64{"repo-1": 8000},
		AgentUptimes:       map[string]float64{"agent-1": 0, "agent-2": 3600},
		CostPerPR:          map[string]float64{"repo-1": 50.0},
		WeeklyAvgCostPerPR: map[string]float64{"repo-1": 10.0},
	}
	report := ne.EvaluateAndNotify(context.Background(), snapshot)

	// Should have firing alerts: HighCostAgent(agent-1), GateFailureSpike, PRStuck, AgentDown(agent-1), CostAnomaly
	if report.Summary.Firing == 0 {
		t.Error("expected firing alerts")
	}
	if report.Notified == 0 {
		t.Error("expected notifications sent")
	}

	// Verify stdout got the alerts
	if !strings.Contains(buf.String(), "AgentDown") {
		t.Errorf("stdout missing AgentDown: %s", buf.String())
	}

	// Verify file got the alerts
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(fileData), "AgentDown") {
		t.Errorf("file missing AgentDown: %s", fileData)
	}
}
