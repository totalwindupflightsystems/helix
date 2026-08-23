package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
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
	// Canonical Forgejo API probe — :3030, not DuckBrain's :3000.
	if cfg.ForgejoURL != "http://localhost:3030/api/v1/version" {
		t.Errorf("expected canonical Forgejo URL http://localhost:3030/api/v1/version, got %s", cfg.ForgejoURL)
	}
	if cfg.ChimeraURL == "" {
		t.Error("expected non-empty ChimeraURL")
	}
	// Chimera must be probed at the fast liveness endpoint, NOT the
	// slow /v1/health readiness check (up to ~10s under load) which
	// false-reports a healthy platform as FAIL at the 5s doctor probe
	// timeout (DF-017).
	if cfg.ChimeraURL != "http://localhost:8765/health" {
		t.Errorf("expected chimera liveness URL http://localhost:8765/health, got %s", cfg.ChimeraURL)
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
	if !strings.Contains(detail, "HTTP 200") {
		t.Errorf("expected 'HTTP 200' in detail, got %q", detail)
	}
}

func TestCheckHTTP_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ok, detail := checkHTTP(server.URL, 5*time.Second)
	if ok {
		t.Error("expected ok=false for 500")
	}
	if !strings.Contains(detail, "HTTP 500") {
		t.Errorf("expected 'HTTP 500' in detail, got %q", detail)
	}
}

// TestCheckHTTP_RouteMismatch — a 4xx on the probe path means the
// service IS reachable but the URL/path is wrong: FAIL with explicit
// "route mismatch" wording, distinct from a dead service.
func TestCheckHTTP_RouteMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	ok, detail := checkHTTP(server.URL, 5*time.Second)
	if ok {
		t.Error("expected ok=false for 404")
	}
	if !strings.Contains(detail, "route mismatch") {
		t.Errorf("expected 'route mismatch' in detail, got %q", detail)
	}
	if !strings.Contains(detail, "HTTP 404") {
		t.Errorf("expected 'HTTP 404' in detail, got %q", detail)
	}
}

// TestCheckHTTP_Timeout — a service that hangs (never responds) must be
// cut off at the timeout and reported as unreachable/timed out.
func TestCheckHTTP_Timeout(t *testing.T) {
	srv := hangingHTTPServer(t)

	start := time.Now()
	ok, detail := checkHTTP(srv.URL, 200*time.Millisecond)
	elapsed := time.Since(start)

	if ok {
		t.Error("expected ok=false for hanging server")
	}
	if !strings.Contains(detail, "timed out") {
		t.Errorf("expected 'timed out' in detail, got %q", detail)
	}
	if !strings.Contains(detail, "unreachable") {
		t.Errorf("expected 'unreachable' in detail, got %q", detail)
	}
	if elapsed > 2*time.Second {
		t.Errorf("checkHTTP must resolve at the timeout, took %s", elapsed)
	}
}

func TestCheckHTTP_Unreachable(t *testing.T) {
	ok, detail := checkHTTP("http://127.0.0.1:59999", 1*time.Second)
	if ok {
		t.Error("expected ok=false for unreachable server")
	}
	if !strings.Contains(detail, "unreachable") {
		t.Errorf("expected 'unreachable' in detail, got %q", detail)
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

// TestRunAllChecks_Concurrent — the 6 HTTP checks share one
// doctorHTTPTimeout and run concurrently (mirroring pkg/health.Checker),
// so a hanging service cannot stretch the run to N×timeout. With 6
// checks × 400ms, sequential execution would take ≥ 2.4s; concurrent
// execution completes in ~one timeout.
func TestRunAllChecks_Concurrent(t *testing.T) {
	old := doctorHTTPTimeout
	doctorHTTPTimeout = 400 * time.Millisecond
	defer func() { doctorHTTPTimeout = old }()

	srv := hangingHTTPServer(t)

	cfg := DoctorConfig{
		ForgejoURL:           srv.URL,
		ChimeraURL:           srv.URL,
		ConscientiousnessURL: srv.URL,
		HivemindURL:          srv.URL,
		LangFuseURL:          srv.URL,
		PrometheusURL:        srv.URL,
		// Disk/memory thresholds at 100 so host state can never add a
		// spurious FAIL to the exactly-6-fails assertion below (same
		// "never FAIL on any disk" pattern as TestCheckDiskUsage_Success).
		DiskPath:          t.TempDir(),
		MaxDiskUsagePct:   100,
		MaxMemUsagePct:    100,
		BackupPath:        t.TempDir(),
		MaxBackupAgeHours: 48,
	}

	start := time.Now()
	report := runAllChecks(cfg)
	elapsed := time.Since(start)

	require.NotNil(t, report, "expected non-nil report")
	require.Equal(t, 9, len(report.Results), "all 9 checks must run")
	assert.Equal(t, 6, report.Fail, "all 6 HTTP checks must fail against a hanging server")
	assert.Less(t, elapsed, 2*time.Second,
		"concurrent checks must finish in ~one timeout (400ms), took %s", elapsed)
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
	if !strings.Contains(result.Detail, "unreachable") {
		t.Errorf("expected 'unreachable' in detail, got %q", result.Detail)
	}
}

// TestCheckForgejo_RouteMismatch — Forgejo probe path returns 4xx: FAIL
// with "route mismatch" wording (service reachable, wrong path), not a
// generic HTTP error.
func TestCheckForgejo_RouteMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := DoctorConfig{ForgejoURL: server.URL}
	result := checkForgejo(cfg)
	if result.Status != "FAIL" {
		t.Errorf("expected FAIL, got %s", result.Status)
	}
	if !strings.Contains(result.Detail, "route mismatch") {
		t.Errorf("expected 'route mismatch' in detail, got %q", result.Detail)
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
// env-robust DiskPath selection for runDoctorWithConfig tests
// ============================================================================
//
// checkDiskUsage FAILs when the filesystem backing DiskPath is >=
// MaxDiskUsagePct full, so a bare t.TempDir() (default /tmp, on the root
// partition) made these tests trip on any host whose root fs is >= 90%
// full. The helpers below pick a DiskPath whose filesystem has headroom
// instead: a tmpfs subdir under /dev/shm (near-zero usage on any normal
// Linux host or ubuntu CI runner) with a fallback to the default temp
// dir and a skip-if-fs-full guard, keeping the tests deterministic on
// any host.

// fsUsedPct returns the used percentage of the filesystem backing path,
// mirroring checkDiskUsage's statfs math.
func fsUsedPct(path string) (float64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	total := stat.Blocks * uint64(stat.Bsize)
	if total == 0 {
		return 0, fmt.Errorf("statfs: zero total blocks for %s", path)
	}
	free := stat.Bfree * uint64(stat.Bsize)
	return float64(total-free) / float64(total) * 100, nil
}

// selectDoctorDiskPath creates a fresh temp dir under the first
// candidate base whose filesystem has used% < maxUsedPct, so the
// disk-usage check cannot FAIL on host state. It returns an error when
// no candidate qualifies (callers skip the test). probe is injectable
// for deterministic unit tests of the full-disk decision.
func selectDoctorDiskPath(maxUsedPct float64, bases []string, probe func(string) (float64, error)) (string, error) {
	for _, base := range bases {
		dir, err := os.MkdirTemp(base, "helix-doctor-*")
		if err != nil {
			continue // base missing or not writable — try the next one
		}
		used, err := probe(dir)
		if err == nil && used < maxUsedPct {
			return dir, nil
		}
		_ = os.RemoveAll(dir) // no headroom or unprobeable — try the next base
	}
	return "", fmt.Errorf("no writable temp filesystem with < %.0f%% used (checked: %v)", maxUsedPct, bases)
}

// doctorTestDiskBases lists temp-dir bases in preference order: a tmpfs
// subdir under /dev/shm first (Linux hosts and CI runners — near-zero
// usage regardless of how full the root partition is), then the default
// temp dir.
func doctorTestDiskBases() []string {
	if runtime.GOOS == "linux" {
		if fi, err := os.Stat("/dev/shm"); err == nil && fi.IsDir() {
			return []string{"/dev/shm", os.TempDir()}
		}
	}
	return []string{os.TempDir()}
}

// doctorTestDiskPath returns a DiskPath on a filesystem with headroom
// below maxUsedPct, or skips the test when no candidate qualifies (a
// host where every temp filesystem is >= maxUsedPct full cannot run the
// disk-usage check deterministically).
func doctorTestDiskPath(t *testing.T, maxUsedPct float64) string {
	t.Helper()
	dir, err := selectDoctorDiskPath(maxUsedPct, doctorTestDiskBases(), fsUsedPct)
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestFsUsedPct_BadPath(t *testing.T) {
	_, err := fsUsedPct("/nonexistent-disk-path-xyz-123")
	require.Error(t, err, "a nonexistent path must surface a statfs error, not a silent zero")
}

// TestSelectDoctorDiskPath_FullFilesystemReturnsError — when every
// candidate's filesystem is >= maxUsedPct full, selection must return an
// error so the caller skips: the full-disk case is handled
// deterministically, never as a spurious checkDiskUsage FAIL.
func TestSelectDoctorDiskPath_FullFilesystemReturnsError(t *testing.T) {
	bases := []string{t.TempDir(), t.TempDir()}
	_, err := selectDoctorDiskPath(90, bases, func(string) (float64, error) { return 95, nil })
	require.Error(t, err, "no candidate with headroom must yield an error (caller skips)")
}

// TestSelectDoctorDiskPath_PicksHeadroomCandidate — selection must skip
// a full candidate and pick the first one whose filesystem has headroom.
func TestSelectDoctorDiskPath_PicksHeadroomCandidate(t *testing.T) {
	bases := []string{t.TempDir(), t.TempDir()}
	calls := 0
	dir, err := selectDoctorDiskPath(90, bases, func(string) (float64, error) {
		calls++
		if calls == 1 {
			return 95, nil // first candidate's fs is full
		}
		return 10, nil // second candidate has headroom
	})
	require.NoError(t, err)
	require.Equal(t, 2, calls, "must probe both candidates")
	require.True(t, strings.HasPrefix(dir, bases[1]), "selected %s, want a dir under %s", dir, bases[1])
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
}

// TestSelectDoctorDiskPath_ProbeErrorSkipsCandidate — a statfs error
// disqualifies the candidate (the caller skips rather than FAIL).
func TestSelectDoctorDiskPath_ProbeErrorSkipsCandidate(t *testing.T) {
	_, err := selectDoctorDiskPath(90, []string{t.TempDir()}, func(string) (float64, error) {
		return 0, fmt.Errorf("statfs failed")
	})
	require.Error(t, err)
}

// TestDoctorTestDiskBases_DefaultTempDirFallback — the candidate list
// must always end with the default temp dir, and lead with /dev/shm on
// Linux hosts that have it.
func TestDoctorTestDiskBases_DefaultTempDirFallback(t *testing.T) {
	bases := doctorTestDiskBases()
	require.NotEmpty(t, bases)
	require.Equal(t, os.TempDir(), bases[len(bases)-1], "default temp dir must be the last resort")
	if runtime.GOOS == "linux" {
		if fi, err := os.Stat("/dev/shm"); err == nil && fi.IsDir() {
			require.Equal(t, "/dev/shm", bases[0], "tmpfs must be preferred on Linux")
		}
	}
}

// TestDoctorTestDiskPath_SelectedDirHasHeadroom — a path chosen by
// doctorTestDiskPath must never FAIL the disk check at the same
// threshold, on any host: selection guarantees headroom by construction.
func TestDoctorTestDiskPath_SelectedDirHasHeadroom(t *testing.T) {
	dir := doctorTestDiskPath(t, 90)
	require.NotEmpty(t, dir)
	result := checkDiskUsage(DoctorConfig{DiskPath: dir, MaxDiskUsagePct: 90})
	require.NotEqual(t, "FAIL", result.Status,
		"selected DiskPath must have headroom; got %q: %s", result.Status, result.Detail)
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
		// DiskPath sits on a filesystem with headroom (a tmpfs subdir
		// under /dev/shm when available), so the disk check cannot trip
		// on a host whose root partition is >= 90% full. BackupPath
		// empty → backup check WARNs but doesn't FAIL, so AllPassed()
		// stays true.
		DiskPath:        doctorTestDiskPath(t, 90),
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
		DiskPath:             doctorTestDiskPath(t, 90), // headroom fs — disk check can't spuriously FAIL
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
		DiskPath:             doctorTestDiskPath(t, 90), // headroom fs — disk check can't spuriously FAIL
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
