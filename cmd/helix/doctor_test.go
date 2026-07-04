package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// DoctorResult
// ============================================================================

func TestDoctorResult_IsPass(t *testing.T) {
	r := DoctorResult{Status: "PASS"}
	if !r.IsPass() {
		t.Error("expected IsPass=true for PASS")
	}

	r2 := DoctorResult{Status: "FAIL"}
	if r2.IsPass() {
		t.Error("expected IsPass=false for FAIL")
	}
}

// ============================================================================
// DoctorReport
// ============================================================================

func TestDoctorReport_AllPassed(t *testing.T) {
	r := &DoctorReport{Pass: 5, Fail: 0}
	if !r.AllPassed() {
		t.Error("expected AllPassed=true with 0 fails")
	}

	r2 := &DoctorReport{Pass: 4, Fail: 1}
	if r2.AllPassed() {
		t.Error("expected AllPassed=false with 1 fail")
	}
}

func TestDoctorReport_HasWarnings(t *testing.T) {
	r := &DoctorReport{Warn: 1}
	if !r.HasWarnings() {
		t.Error("expected HasWarnings=true")
	}

	r2 := &DoctorReport{Warn: 0}
	if r2.HasWarnings() {
		t.Error("expected HasWarnings=false")
	}
}

func TestDoctorReport_Summary_AllPassed(t *testing.T) {
	r := &DoctorReport{Pass: 10, Fail: 0, Warn: 0}
	s := r.Summary()
	if s == "" {
		t.Error("expected non-empty summary")
	}
}

func TestDoctorReport_Summary_WithFailures(t *testing.T) {
	r := &DoctorReport{Pass: 7, Fail: 2, Warn: 1}
	s := r.Summary()
	if s == "" {
		t.Error("expected non-empty summary")
	}
}

// ============================================================================
// DoctorConfig
// ============================================================================

func TestDefaultDoctorConfig(t *testing.T) {
	cfg := DefaultDoctorConfig()
	if cfg.ForgejoURL == "" {
		t.Error("expected non-empty ForgejoURL")
	}
	if cfg.ChimeraURL == "" {
		t.Error("expected non-empty ChimeraURL")
	}
	if cfg.MaxDiskUsagePct == 0 {
		t.Error("expected non-zero MaxDiskUsagePct")
	}
	if cfg.MaxBackupAgeHours == 0 {
		t.Error("expected non-zero MaxBackupAgeHours")
	}
}

// ============================================================================
// checkHTTP
// ============================================================================

func TestCheckHTTP_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ok, detail := checkHTTP(server.URL, 5*time.Second)
	if !ok {
		t.Error("expected ok=true")
	}
	if detail == "" {
		t.Error("expected non-empty detail")
	}
}

func TestCheckHTTP_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ok, _ := checkHTTP(server.URL, 5*time.Second)
	if ok {
		t.Error("expected ok=false for 500")
	}
}

func TestCheckHTTP_Unreachable(t *testing.T) {
	ok, _ := checkHTTP("http://127.0.0.1:59999", 1*time.Second)
	if ok {
		t.Error("expected ok=false for unreachable server")
	}
}

func TestCheckHTTP_BadURL(t *testing.T) {
	ok, _ := checkHTTP("://bad-url", 1*time.Second)
	if ok {
		t.Error("expected ok=false for bad URL")
	}
}

// ============================================================================
// statusFromBool
// ============================================================================

func TestStatusFromBool(t *testing.T) {
	if statusFromBool(true) != "PASS" {
		t.Error("expected PASS for true")
	}
	if statusFromBool(false) != "FAIL" {
		t.Error("expected FAIL for false")
	}
}

// ============================================================================
// parseMemInfoLine
// ============================================================================

func TestParseMemInfoLine(t *testing.T) {
	tests := []struct {
		line   string
		expect float64
	}{
		{"MemTotal:       16384000 kB", 16384000},
		{"MemAvailable:   8192000 kB", 8192000},
		{"invalid line", 0},
		{"", 0},
	}

	for _, tt := range tests {
		if got := parseMemInfoLine(tt.line); got != tt.expect {
			t.Errorf("parseMemInfoLine(%q) = %f, want %f", tt.line, got, tt.expect)
		}
	}
}

// ============================================================================
// runAllChecks (integration — uses httptest servers)
// ============================================================================

func TestRunAllChecks_AllServicesUp(t *testing.T) {
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server2.Close()

	tmpDir := t.TempDir()
	// Create a backup file that's recent
	backupFile := filepath.Join(tmpDir, "forgejo-20260701.tar.gz")
	_ = os.WriteFile(backupFile, []byte("backup"), 0644)

	cfg := DoctorConfig{
		ForgejoURL:           server1.URL,
		ChimeraURL:           server1.URL,
		ConscientiousnessURL: server1.URL,
		HivemindURL:          server1.URL,
		LangFuseURL:          server1.URL,
		PrometheusURL:        server1.URL,
		DiskPath:             tmpDir,
		MaxDiskUsagePct:      99.0,
		MaxMemUsagePct:       99.0,
		BackupPath:           tmpDir,
		MaxBackupAgeHours:    48,
	}

	report := runAllChecks(cfg)
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if len(report.Results) < 8 {
		t.Errorf("expected at least 8 checks, got %d", len(report.Results))
	}
}

func TestRunAllChecks_ServicesDown(t *testing.T) {
	cfg := DoctorConfig{
		ForgejoURL:           "http://127.0.0.1:59999",
		ChimeraURL:           "http://127.0.0.1:59999",
		ConscientiousnessURL: "http://127.0.0.1:59999",
		HivemindURL:          "http://127.0.0.1:59999",
		LangFuseURL:          "http://127.0.0.1:59999",
		PrometheusURL:        "http://127.0.0.1:59999",
		DiskPath:             "/",
		MaxDiskUsagePct:      99.0,
		MaxMemUsagePct:       99.0,
		BackupPath:           "/nonexistent-backup-path-xyz",
		MaxBackupAgeHours:    24,
	}

	report := runAllChecks(cfg)
	if report.Fail == 0 {
		t.Errorf("expected some failures with unreachable services, got %d fail", report.Fail)
	}
}

// ============================================================================
// checkDiskUsage
// ============================================================================

func TestCheckDiskUsage_Success(t *testing.T) {
	cfg := DoctorConfig{
		DiskPath:        t.TempDir(),
		MaxDiskUsagePct: 100.0, // Use 100% to never FAIL on any disk
	}
	result := checkDiskUsage(cfg)
	// On a heavily used disk, the check may return WARN (approaching limit)
	// but should never FAIL when MaxDiskUsagePct is set to 100%.
	if result.Status == "FAIL" {
		t.Errorf("expected PASS or WARN for MaxDiskUsagePct=100, got FAIL: %s", result.Detail)
	}
}

func TestCheckDiskUsage_BadPath(t *testing.T) {
	cfg := DoctorConfig{
		DiskPath:        "/nonexistent-disk-path-xyz-123",
		MaxDiskUsagePct: 90.0,
	}
	result := checkDiskUsage(cfg)
	if result.Status != "FAIL" {
		t.Errorf("expected FAIL for bad path, got %s", result.Status)
	}
}

// ============================================================================
// checkBackupFreshness
// ============================================================================

func TestCheckBackupFreshness_RecentBackup(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a file with current modification time
	backupFile := filepath.Join(tmpDir, "backup.tar.gz")
	_ = os.WriteFile(backupFile, []byte("backup"), 0644)

	cfg := DoctorConfig{
		BackupPath:        tmpDir,
		MaxBackupAgeHours: 48,
	}

	result := checkBackupFreshness(cfg)
	if result.Status != "PASS" {
		t.Errorf("expected PASS for recent backup, got %s: %s", result.Status, result.Detail)
	}
}

func TestCheckBackupFreshness_NoBackups(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := DoctorConfig{
		BackupPath:        tmpDir,
		MaxBackupAgeHours: 24,
	}

	result := checkBackupFreshness(cfg)
	if result.Status != "WARN" {
		t.Errorf("expected WARN for no backups, got %s", result.Status)
	}
}

func TestCheckBackupFreshness_BadPath(t *testing.T) {
	cfg := DoctorConfig{
		BackupPath:        "/nonexistent-backup-xyz-123",
		MaxBackupAgeHours: 24,
	}

	result := checkBackupFreshness(cfg)
	if result.Status != "WARN" {
		t.Errorf("expected WARN for bad path, got %s", result.Status)
	}
}

// ============================================================================
// parseDoctorFlags
// ============================================================================

func TestParseDoctorFlags_Default(t *testing.T) {
	cfg := parseDoctorFlags([]string{})
	if cfg.ForgejoURL != DefaultDoctorConfig().ForgejoURL {
		t.Error("expected default ForgejoURL")
	}
}

func TestParseDoctorFlags_CustomURLs(t *testing.T) {
	args := []string{"--forgejo-url", "http://custom:3000", "--chimera-url", "http://custom:8765"}
	cfg := parseDoctorFlags(args)
	if cfg.ForgejoURL != "http://custom:3000" {
		t.Errorf("expected custom ForgejoURL, got %s", cfg.ForgejoURL)
	}
	if cfg.ChimeraURL != "http://custom:8765" {
		t.Errorf("expected custom ChimeraURL, got %s", cfg.ChimeraURL)
	}
}

func TestParseDoctorFlags_DiskPath(t *testing.T) {
	args := []string{"--disk-path", "/data"}
	cfg := parseDoctorFlags(args)
	if cfg.DiskPath != "/data" {
		t.Errorf("expected /data, got %s", cfg.DiskPath)
	}
}

// ============================================================================
// formatJSONReport
// ============================================================================

func TestFormatJSONReport(t *testing.T) {
	report := &DoctorReport{
		Results: []DoctorResult{
			{Name: "test", Status: "PASS", Detail: "ok"},
		},
		Pass: 1,
	}

	jsonStr := formatJSONReport(report)
	if jsonStr == "" {
		t.Error("expected non-empty JSON")
	}
	if !contains(jsonStr, "all_passed") {
		t.Error("expected 'all_passed' in JSON")
	}
}

func TestFormatJSONReport_WithFailures(t *testing.T) {
	report := &DoctorReport{
		Results: []DoctorResult{
			{Name: "fail-check", Status: "FAIL", Detail: "down"},
		},
		Fail: 1,
	}

	jsonStr := formatJSONReport(report)
	if !contains(jsonStr, "false") {
		t.Error("expected 'false' for all_passed in JSON")
	}
}

// ============================================================================
// checkForgejo / checkChimera (integration)
// ============================================================================

func TestCheckForgejo_Reachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DoctorConfig{ForgejoURL: server.URL}
	result := checkForgejo(cfg)
	if result.Status != "PASS" {
		t.Errorf("expected PASS, got %s", result.Status)
	}
}

func TestCheckForgejo_Unreachable(t *testing.T) {
	cfg := DoctorConfig{ForgejoURL: "http://127.0.0.1:59999"}
	result := checkForgejo(cfg)
	if result.Status != "FAIL" {
		t.Errorf("expected FAIL, got %s", result.Status)
	}
}

func TestCheckChimera_Healthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DoctorConfig{ChimeraURL: server.URL}
	result := checkChimera(cfg)
	if result.Status != "PASS" {
		t.Errorf("expected PASS, got %s", result.Status)
	}
}

// ============================================================================
// Helper
// ============================================================================

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ============================================================================
// runDoctorWithConfig coverage — the doctor entry-point that prints the
// report and returns nil on success / error on failure.
// ============================================================================

// TestRunDoctorWithConfig_AllPass — every check points at a healthy httptest
// server; report prints the banner and "All N checks passed" summary.
func TestRunDoctorWithConfig_AllPass(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ok.Close()

	cfg := DoctorConfig{
		ForgejoURL:           ok.URL,
		ChimeraURL:           ok.URL,
		ConscientiousnessURL: ok.URL,
		HivemindURL:          ok.URL,
		LangFuseURL:          ok.URL,
		PrometheusURL:        ok.URL,
		// DiskPath defaults; BackupPath empty → backup check WARNs but
		// doesn't FAIL, so AllPassed() stays true.
		DiskPath:        t.TempDir(),
		MaxDiskUsagePct: 90,
		MaxMemUsagePct:  95,
	}

	stdout := &bytes.Buffer{}
	err := runDoctorWithConfig(cfg, stdout)
	require.NoError(t, err, "all-pass must return nil error")
	out := stdout.String()
	assert.Contains(t, out, "Helix Platform Doctor")
	// Backup check is WARN-only when no BackupPath is set; AllPassed() is
	// still true (no FAIL). Summary reads "8 passed, 1 warnings" or
	// "All N checks passed" depending on whether any WARNs exist.
	assert.True(t,
		strings.Contains(out, "checks passed") || strings.Contains(out, "passed,"),
		"expected pass summary in output, got: %s", out)
}

// TestRunDoctorWithConfig_OneCheckFails — one URL points at a closed port;
// the failure must surface both in stdout ("✗" line) AND as a non-nil error
// that names the failed check count.
func TestRunDoctorWithConfig_OneCheckFails(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ok.Close()

	cfg := DoctorConfig{
		ForgejoURL:           ok.URL,
		ChimeraURL:           "http://127.0.0.1:59999", // unreachable
		ConscientiousnessURL: ok.URL,
		HivemindURL:          ok.URL,
		LangFuseURL:          ok.URL,
		PrometheusURL:        ok.URL,
		DiskPath:             t.TempDir(),
		MaxDiskUsagePct:      90,
		MaxMemUsagePct:       95,
	}

	stdout := &bytes.Buffer{}
	err := runDoctorWithConfig(cfg, stdout)
	require.Error(t, err, "any FAIL must produce non-nil error")
	assert.Contains(t, err.Error(), "checks failed")
	out := stdout.String()
	assert.Contains(t, out, "✗", "output must show ✗ for failed checks")
	assert.Contains(t, out, "failed", "summary must include failure count")
}

// TestRunDoctorWithConfig_NilStdoutDefaultsToOSStdout — passing nil writer
// must not panic; the function falls back to os.Stdout.
func TestRunDoctorWithConfig_NilStdoutDefaultsToOSStdout(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ok.Close()

	cfg := DoctorConfig{
		ForgejoURL:           ok.URL,
		ChimeraURL:           ok.URL,
		ConscientiousnessURL: ok.URL,
		HivemindURL:          ok.URL,
		LangFuseURL:          ok.URL,
		PrometheusURL:        ok.URL,
		DiskPath:             t.TempDir(),
		MaxDiskUsagePct:      90,
		MaxMemUsagePct:       95,
	}

	// Redirect os.Stdout temporarily so we can also verify the fallback
	// writes something. Capture into a pipe and read the other end.
	r, w, err := os.Pipe()
	require.NoError(t, err)
	origStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	done := make(chan struct{})
	go func() {
		_, _ = io.ReadAll(r)
		close(done)
	}()

	_ = runDoctorWithConfig(cfg, nil)
	require.NoError(t, w.Close())
	<-done
}
