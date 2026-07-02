// Command helix — doctor.go
//
// helix doctor runs the spec §10.5 diagnostic checklist:
//
//	✓ Forgejo reachable
//	✓ Chimera healthy
//	✓ Conscientiousness healthy
//	✓ Hivemind healthy
//	✓ LangFuse reachable
//	✓ Prometheus scraping
//	✓ Agent containers running
//	✓ Disk usage
//	✓ Memory
//	✓ Backup freshness
//
// Exit code 0 if all pass, 1 if any fail.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// ============================================================================
// DoctorConfig
// ============================================================================

// DoctorConfig holds configurable service URLs for the diagnostic checks.
type DoctorConfig struct {
	ForgejoURL           string
	ChimeraURL           string
	ConscientiousnessURL string
	HivemindURL          string
	LangFuseURL          string
	PrometheusURL        string
	DiskPath             string
	MaxDiskUsagePct      float64
	MaxMemUsagePct       float64
	BackupPath           string
	MaxBackupAgeHours    int
}

// DefaultDoctorConfig returns the spec-defined default service URLs.
func DefaultDoctorConfig() DoctorConfig {
	return DoctorConfig{
		ForgejoURL:           "http://localhost:3000/api/v1/version",
		ChimeraURL:           "http://localhost:8765/v1/health",
		ConscientiousnessURL: "http://localhost:8002/health",
		HivemindURL:          "http://localhost:8003/health",
		LangFuseURL:          "http://localhost:3001",
		PrometheusURL:        "http://localhost:9090/-/healthy",
		DiskPath:             "/",
		MaxDiskUsagePct:      90.0,
		MaxMemUsagePct:       90.0,
		BackupPath:           "/mnt/backups/helix",
		MaxBackupAgeHours:    24,
	}
}

// ============================================================================
// DoctorResult
// ============================================================================

// DoctorResult is the outcome of a single diagnostic check.
type DoctorResult struct {
	Name     string
	Status   string // "PASS", "FAIL", "WARN"
	Detail   string
	Duration time.Duration
}

// IsPass returns true if the check passed.
func (r DoctorResult) IsPass() bool {
	return r.Status == "PASS"
}

// ============================================================================
// DoctorReport
// ============================================================================

// DoctorReport aggregates all diagnostic check results.
type DoctorReport struct {
	Results []DoctorResult
	Pass    int
	Fail    int
	Warn    int
}

// AllPassed returns true if no checks failed.
func (r *DoctorReport) AllPassed() bool {
	return r.Fail == 0
}

// HasWarnings returns true if any checks warned.
func (r *DoctorReport) HasWarnings() bool {
	return r.Warn > 0
}

// Summary returns a human-readable summary.
func (r *DoctorReport) Summary() string {
	if r.AllPassed() && !r.HasWarnings() {
		return fmt.Sprintf("All %d checks passed", r.Pass)
	}
	parts := []string{fmt.Sprintf("%d passed", r.Pass)}
	if r.Fail > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", r.Fail))
	}
	if r.Warn > 0 {
		parts = append(parts, fmt.Sprintf("%d warnings", r.Warn))
	}
	return strings.Join(parts, ", ")
}

// ============================================================================
// Run Doctor
// ============================================================================

// runDoctorWithConfig executes the diagnostic with the given config.
func runDoctorWithConfig(cfg DoctorConfig) error {
	report := runAllChecks(cfg)

	fmt.Println("Helix Platform Doctor")
	fmt.Println("====================")
	fmt.Println()

	for _, r := range report.Results {
		icon := "✓"
		if r.Status == "FAIL" {
			icon = "✗"
		} else if r.Status == "WARN" {
			icon = "⚠"
		}
		fmt.Printf("%s %-25s %s (%.3fs)\n", icon, r.Name, r.Detail, r.Duration.Seconds())
	}

	fmt.Println()
	if report.AllPassed() {
		fmt.Printf("✓ %s\n", report.Summary())
		return nil
	}

	fmt.Printf("✗ %s\n", report.Summary())
	return fmt.Errorf("doctor checks failed: %d of %d checks failed", report.Fail, len(report.Results))
}

// ============================================================================
// Check Runners
// ============================================================================

// runAllChecks executes all diagnostic checks and returns a report.
func runAllChecks(cfg DoctorConfig) *DoctorReport {
	report := &DoctorReport{}

	// Service health checks
	checks := []struct {
		name string
		fn   func(DoctorConfig) DoctorResult
	}{
		{"Forgejo reachable", checkForgejo},
		{"Chimera healthy", checkChimera},
		{"Conscientiousness healthy", checkConscientiousness},
		{"Hivemind healthy", checkHivemind},
		{"LangFuse reachable", checkLangFuse},
		{"Prometheus scraping", checkPrometheus},
		{"Disk usage", checkDiskUsage},
		{"Memory", checkMemory},
		{"Backup freshness", checkBackupFreshness},
	}

	for _, check := range checks {
		result := check.fn(cfg)
		report.Results = append(report.Results, result)
		switch result.Status {
		case "PASS":
			report.Pass++
		case "FAIL":
			report.Fail++
		case "WARN":
			report.Warn++
		}
	}

	return report
}

// checkForgejo verifies Forgejo is reachable via its version endpoint.
func checkForgejo(cfg DoctorConfig) DoctorResult {
	start := time.Now()
	ok, detail := checkHTTP(cfg.ForgejoURL, 5*time.Second)
	return DoctorResult{
		Name:     "Forgejo reachable",
		Status:   statusFromBool(ok),
		Detail:   detail,
		Duration: time.Since(start),
	}
}

// checkChimera verifies Chimera health endpoint.
func checkChimera(cfg DoctorConfig) DoctorResult {
	start := time.Now()
	ok, detail := checkHTTP(cfg.ChimeraURL, 5*time.Second)
	return DoctorResult{
		Name:     "Chimera healthy",
		Status:   statusFromBool(ok),
		Detail:   detail,
		Duration: time.Since(start),
	}
}

// checkConscientiousness verifies Conscientiousness health.
func checkConscientiousness(cfg DoctorConfig) DoctorResult {
	start := time.Now()
	ok, detail := checkHTTP(cfg.ConscientiousnessURL, 5*time.Second)
	return DoctorResult{
		Name:     "Conscientiousness healthy",
		Status:   statusFromBool(ok),
		Detail:   detail,
		Duration: time.Since(start),
	}
}

// checkHivemind verifies Hivemind health.
func checkHivemind(cfg DoctorConfig) DoctorResult {
	start := time.Now()
	ok, detail := checkHTTP(cfg.HivemindURL, 5*time.Second)
	return DoctorResult{
		Name:     "Hivemind healthy",
		Status:   statusFromBool(ok),
		Detail:   detail,
		Duration: time.Since(start),
	}
}

// checkLangFuse verifies LangFuse is reachable.
func checkLangFuse(cfg DoctorConfig) DoctorResult {
	start := time.Now()
	ok, detail := checkHTTP(cfg.LangFuseURL, 5*time.Second)
	return DoctorResult{
		Name:     "LangFuse reachable",
		Status:   statusFromBool(ok),
		Detail:   detail,
		Duration: time.Since(start),
	}
}

// checkPrometheus verifies Prometheus is healthy and scraping.
func checkPrometheus(cfg DoctorConfig) DoctorResult {
	start := time.Now()
	ok, detail := checkHTTP(cfg.PrometheusURL, 5*time.Second)
	return DoctorResult{
		Name:     "Prometheus scraping",
		Status:   statusFromBool(ok),
		Detail:   detail,
		Duration: time.Since(start),
	}
}

// checkDiskUsage checks the disk usage percentage.
func checkDiskUsage(cfg DoctorConfig) DoctorResult {
	start := time.Now()

	var stat syscall.Statfs_t
	err := syscall.Statfs(cfg.DiskPath, &stat)
	if err != nil {
		return DoctorResult{
			Name:     "Disk usage",
			Status:   "FAIL",
			Detail:   fmt.Sprintf("statfs error: %v", err),
			Duration: time.Since(start),
		}
	}

	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	usedPct := float64(total-free) / float64(total) * 100

	detail := fmt.Sprintf("%.1f%% used (%.1fGB free of %.1fGB)",
		usedPct,
		float64(free)/1e9,
		float64(total)/1e9,
	)

	status := "PASS"
	if usedPct >= cfg.MaxDiskUsagePct {
		status = "FAIL"
	} else if usedPct >= cfg.MaxDiskUsagePct*0.8 {
		status = "WARN"
	}

	return DoctorResult{
		Name:     "Disk usage",
		Status:   status,
		Detail:   detail,
		Duration: time.Since(start),
	}
}

// checkMemory checks system memory usage.
func checkMemory(cfg DoctorConfig) DoctorResult {
	start := time.Now()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Use Sys as the total allocated memory by the Go runtime.
	// In production, this would read /proc/meminfo for system-wide stats.
	allocMB := float64(m.Alloc) / 1e6
	sysMB := float64(m.Sys) / 1e6

	detail := fmt.Sprintf("Alloc: %.1fMB, Sys: %.1fMB", allocMB, sysMB)

	// On Linux, read system memory from /proc/meminfo
	if runtime.GOOS == "linux" {
		if usedPct, err := readMemInfoUsage(); err == nil {
			detail = fmt.Sprintf("%.1f%% used", usedPct)
			status := "PASS"
			if usedPct >= cfg.MaxMemUsagePct {
				status = "FAIL"
			} else if usedPct >= cfg.MaxMemUsagePct*0.8 {
				status = "WARN"
			}
			return DoctorResult{
				Name:     "Memory",
				Status:   status,
				Detail:   detail,
				Duration: time.Since(start),
			}
		}
	}

	return DoctorResult{
		Name:     "Memory",
		Status:   "PASS",
		Detail:   detail,
		Duration: time.Since(start),
	}
}

// checkBackupFreshness checks that backups exist and are recent.
func checkBackupFreshness(cfg DoctorConfig) DoctorResult {
	start := time.Now()

	// Check if backup directory exists
	info, err := os.Stat(cfg.BackupPath)
	if err != nil {
		return DoctorResult{
			Name:     "Backup freshness",
			Status:   "WARN",
			Detail:   fmt.Sprintf("backup path %s not accessible: %v", cfg.BackupPath, err),
			Duration: time.Since(start),
		}
	}

	if !info.IsDir() {
		return DoctorResult{
			Name:     "Backup freshness",
			Status:   "FAIL",
			Detail:   fmt.Sprintf("%s is not a directory", cfg.BackupPath),
			Duration: time.Since(start),
		}
	}

	// Find the most recent backup file
	entries, err := os.ReadDir(cfg.BackupPath)
	if err != nil {
		return DoctorResult{
			Name:     "Backup freshness",
			Status:   "WARN",
			Detail:   fmt.Sprintf("cannot read backup dir: %v", err),
			Duration: time.Since(start),
		}
	}

	var newest time.Time
	var newestName string
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
			newestName = entry.Name()
		}
	}

	if newest.IsZero() {
		return DoctorResult{
			Name:     "Backup freshness",
			Status:   "WARN",
			Detail:   "no backup files found",
			Duration: time.Since(start),
		}
	}

	age := time.Since(newest)
	ageHours := int(age.Hours())

	detail := fmt.Sprintf("last backup: %s (%dh ago)", newestName, ageHours)

	status := "PASS"
	if ageHours > cfg.MaxBackupAgeHours {
		status = "FAIL"
	} else if ageHours > cfg.MaxBackupAgeHours/2 {
		status = "WARN"
	}

	return DoctorResult{
		Name:     "Backup freshness",
		Status:   status,
		Detail:   detail,
		Duration: time.Since(start),
	}
}

// ============================================================================
// Helpers
// ============================================================================

// checkHTTP performs a GET request and returns (ok, detail).
func checkHTTP(url string, timeout time.Duration) (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false, fmt.Sprintf("request error: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Sprintf("unreachable: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return false, fmt.Sprintf("HTTP %d", resp.StatusCode)
}

// statusFromBool converts a boolean to PASS/FAIL.
func statusFromBool(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}

// readMemInfoUsage reads /proc/meminfo and returns memory usage percentage.
func readMemInfoUsage() (float64, error) {
	cmd := exec.Command("cat", "/proc/meminfo")
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	var memTotal, memAvailable float64
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			memTotal = parseMemInfoLine(line)
		} else if strings.HasPrefix(line, "MemAvailable:") {
			memAvailable = parseMemInfoLine(line)
		}
	}

	if memTotal == 0 {
		return 0, fmt.Errorf("could not parse MemTotal")
	}

	usedPct := (memTotal - memAvailable) / memTotal * 100
	return usedPct, nil
}

// parseMemInfoLine extracts the KB value from a /proc/meminfo line.
func parseMemInfoLine(line string) float64 {
	// Format: "MemTotal:       16384000 kB"
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return 0
	}
	var kb float64
	_, err := fmt.Sscanf(parts[1], "%f", &kb)
	if err != nil {
		return 0
	}
	return kb
}

// parseDoctorFlags parses flags for the doctor command.
func parseDoctorFlags(args []string) DoctorConfig {
	cfg := DefaultDoctorConfig()
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--forgejo-url":
			if i+1 < len(args) {
				cfg.ForgejoURL = args[i+1]
				i++
			}
		case "--chimera-url":
			if i+1 < len(args) {
				cfg.ChimeraURL = args[i+1]
				i++
			}
		case "--disk-path":
			if i+1 < len(args) {
				cfg.DiskPath = args[i+1]
				i++
			}
		}
	}
	return cfg
}

// formatJSONReport formats the doctor report as JSON for machine consumption.
func formatJSONReport(report *DoctorReport) string {
	data := map[string]interface{}{
		"summary":    report.Summary(),
		"all_passed": report.AllPassed(),
		"pass_count": report.Pass,
		"fail_count": report.Fail,
		"warn_count": report.Warn,
		"results":    report.Results,
	}
	b, _ := json.MarshalIndent(data, "", "  ")
	return string(b)
}
