package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Default Checks Tests
// =============================================================================

func TestDefaultChecks_Count(t *testing.T) {
	checks := DefaultChecks()
	// 14 deployment + 8 operational = 22
	if len(checks) != 22 {
		t.Errorf("DefaultChecks() returned %d checks, want 22", len(checks))
	}
}

func TestDefaultChecks_DeploymentCount(t *testing.T) {
	checks := DefaultChecks()
	deployment := 0
	for _, c := range checks {
		if c.Category == CategoryDeployment {
			deployment++
		}
	}
	if deployment != 14 {
		t.Errorf("Deployment checks = %d, want 14", deployment)
	}
}

func TestDefaultChecks_OperationalCount(t *testing.T) {
	checks := DefaultChecks()
	operational := 0
	for _, c := range checks {
		if c.Category == CategoryOperational {
			operational++
		}
	}
	if operational != 8 {
		t.Errorf("Operational checks = %d, want 8", operational)
	}
}

func TestDefaultChecks_AllHaveIDs(t *testing.T) {
	checks := DefaultChecks()
	seen := make(map[string]bool)
	for _, c := range checks {
		if c.ID == "" {
			t.Error("check has empty ID")
		}
		if seen[c.ID] {
			t.Errorf("duplicate check ID: %s", c.ID)
		}
		seen[c.ID] = true
		if c.Name == "" {
			t.Errorf("check %s has empty Name", c.ID)
		}
		if c.Description == "" {
			t.Errorf("check %s has empty Description", c.ID)
		}
	}
}

func TestDefaultChecks_SeverityValues(t *testing.T) {
	checks := DefaultChecks()
	for _, c := range checks {
		if c.Category == CategoryDeployment && c.Severity != StatusFail {
			t.Errorf("deployment check %s has severity %q, want FAIL", c.ID, c.Severity)
		}
		if c.Category == CategoryOperational && c.Severity != StatusWarn {
			t.Errorf("operational check %s has severity %q, want WARN", c.ID, c.Severity)
		}
	}
}

// =============================================================================
// Hardening Checker Tests
// =============================================================================

func TestNewHardeningChecker_AllChecksRegistered(t *testing.T) {
	c := NewHardeningChecker()
	checks := c.ListChecks()
	if len(checks) != 22 {
		t.Errorf("ListChecks() returned %d, want 22", len(checks))
	}
}

func TestHardeningChecker_Run_NoFunctions(t *testing.T) {
	c := NewHardeningChecker()
	report := c.Run()

	// Skipped checks don't fail the report — they're just not verified
	if report.SkippedCount != 22 {
		t.Errorf("SkippedCount = %d, want 22", report.SkippedCount)
	}
	if report.PassedCount != 0 {
		t.Errorf("PassedCount = %d, want 0", report.PassedCount)
	}
	if report.FailedCount != 0 {
		t.Errorf("FailedCount = %d, want 0", report.FailedCount)
	}
	// AllPassed should be true since nothing actually failed
	if !report.AllPassed {
		t.Error("AllPassed = false, want true (skips don't fail)")
	}
}

func TestHardeningChecker_Run_AllPass(t *testing.T) {
	c := NewHardeningChecker()
	for _, check := range c.ListChecks() {
		c.RegisterCheck(check.ID, func() (CheckStatus, string) {
			return StatusPass, "check passed"
		})
	}
	report := c.Run()

	if !report.AllPassed {
		t.Error("AllPassed = false, want true")
	}
	if report.PassedCount != 22 {
		t.Errorf("PassedCount = %d, want 22", report.PassedCount)
	}
	if report.FailedCount != 0 {
		t.Errorf("FailedCount = %d, want 0", report.FailedCount)
	}
}

func TestHardeningChecker_Run_AllFail(t *testing.T) {
	c := NewHardeningChecker()
	for _, check := range c.ListChecks() {
		c.RegisterCheck(check.ID, func() (CheckStatus, string) {
			return StatusFail, "check failed"
		})
	}
	report := c.Run()

	if report.AllPassed {
		t.Error("AllPassed = true, want false")
	}
	if report.FailedCount != 22 {
		t.Errorf("FailedCount = %d, want 22", report.FailedCount)
	}
	if report.PassedCount != 0 {
		t.Errorf("PassedCount = %d, want 0", report.PassedCount)
	}
}

func TestHardeningChecker_Run_Mixed(t *testing.T) {
	c := NewHardeningChecker()
	checks := c.ListChecks()
	for i, check := range checks {
		id := check.ID
		if i%3 == 0 {
			c.RegisterCheck(id, func() (CheckStatus, string) {
				return StatusPass, "ok"
			})
		} else if i%3 == 1 {
			c.RegisterCheck(id, func() (CheckStatus, string) {
				return StatusFail, "broken"
			})
		} else {
			c.RegisterCheck(id, func() (CheckStatus, string) {
				return StatusWarn, "warning"
			})
		}
	}
	report := c.Run()

	if report.AllPassed {
		t.Error("AllPassed = true, want false (some checks fail)")
	}
	// 22 checks: 8 pass (i%3==0: 0,3,6,9,12,15,18,21), 7 fail, 7 warn
	if report.PassedCount != 8 {
		t.Errorf("PassedCount = %d, want 8", report.PassedCount)
	}
	if report.FailedCount != 7 {
		t.Errorf("FailedCount = %d, want 7", report.FailedCount)
	}
	if report.WarningCount != 7 {
		t.Errorf("WarningCount = %d, want 7", report.WarningCount)
	}
}

func TestHardeningChecker_Run_SomeSkipped(t *testing.T) {
	c := NewHardeningChecker()
	checks := c.ListChecks()
	// Register functions for first 5 only
	for i := 0; i < 5 && i < len(checks); i++ {
		id := checks[i].ID
		c.RegisterCheck(id, func() (CheckStatus, string) {
			return StatusPass, "ok"
		})
	}
	report := c.Run()

	if report.PassedCount != 5 {
		t.Errorf("PassedCount = %d, want 5", report.PassedCount)
	}
	if report.SkippedCount != 17 {
		t.Errorf("SkippedCount = %d, want 17", report.SkippedCount)
	}
	// Skipped checks don't fail the report
	if !report.AllPassed {
		t.Error("AllPassed = false, want true (skips don't fail)")
	}
}

func TestHardeningChecker_RunCheck(t *testing.T) {
	c := NewHardeningChecker()
	c.RegisterCheck("admin-password-strength", func() (CheckStatus, string) {
		return StatusPass, "password is 40 chars"
	})
	result, ok := c.RunCheck("admin-password-strength")
	if !ok {
		t.Fatal("RunCheck returned ok=false for existing check")
	}
	if result.Status != StatusPass {
		t.Errorf("Status = %q, want PASS", result.Status)
	}
	if result.Detail != "password is 40 chars" {
		t.Errorf("Detail = %q, want custom message", result.Detail)
	}
}

func TestHardeningChecker_RunCheck_NotFound(t *testing.T) {
	c := NewHardeningChecker()
	_, ok := c.RunCheck("nonexistent-check")
	if ok {
		t.Error("RunCheck returned ok=true for non-existent check")
	}
}

func TestHardeningChecker_RunCheck_NoFunction(t *testing.T) {
	c := NewHardeningChecker()
	result, ok := c.RunCheck("admin-password-strength")
	if !ok {
		t.Fatal("RunCheck returned ok=false for existing check")
	}
	if result.Status != StatusSkip {
		t.Errorf("Status = %q, want SKIP", result.Status)
	}
}

// =============================================================================
// Hardening Report Tests
// =============================================================================

func TestHardeningReport_FailedChecks(t *testing.T) {
	c := NewHardeningChecker()
	c.RegisterCheck("admin-password-strength", func() (CheckStatus, string) {
		return StatusFail, "too short"
	})
	c.RegisterCheck("reverse-proxy-tls", func() (CheckStatus, string) {
		return StatusFail, "no TLS"
	})
	for _, check := range c.ListChecks() {
		if check.ID != "admin-password-strength" && check.ID != "reverse-proxy-tls" {
			c.RegisterCheck(check.ID, func() (CheckStatus, string) {
				return StatusPass, "ok"
			})
		}
	}
	report := c.Run()

	failed := report.FailedChecks()
	if len(failed) != 2 {
		t.Errorf("FailedChecks() = %d, want 2", len(failed))
	}
}

func TestHardeningReport_WarningChecks(t *testing.T) {
	c := NewHardeningChecker()
	c.RegisterCheck("h4f-bridge-cron", func() (CheckStatus, string) {
		return StatusWarn, "not running"
	})
	for _, check := range c.ListChecks() {
		if check.ID != "h4f-bridge-cron" {
			c.RegisterCheck(check.ID, func() (CheckStatus, string) {
				return StatusPass, "ok"
			})
		}
	}
	report := c.Run()

	warnings := report.WarningChecks()
	if len(warnings) != 1 {
		t.Errorf("WarningChecks() = %d, want 1", len(warnings))
	}
}

func TestHardeningReport_FormatReport(t *testing.T) {
	c := NewHardeningChecker()
	for _, check := range c.ListChecks() {
		c.RegisterCheck(check.ID, func() (CheckStatus, string) {
			return StatusPass, "ok"
		})
	}
	report := c.Run()
	output := report.FormatReport()

	if !strings.Contains(output, "PASS") {
		t.Error("FormatReport missing PASS")
	}
	if !strings.Contains(output, "Deployment:") {
		t.Error("FormatReport missing deployment category")
	}
	if !strings.Contains(output, "Operational:") {
		t.Error("FormatReport missing operational category")
	}
	if !strings.Contains(output, "Summary:") {
		t.Error("FormatReport missing summary")
	}
}

func TestHardeningReport_FormatReport_WithFailures(t *testing.T) {
	c := NewHardeningChecker()
	c.RegisterCheck("admin-password-strength", func() (CheckStatus, string) {
		return StatusFail, "password is only 10 chars"
	})
	for _, check := range c.ListChecks() {
		if check.ID != "admin-password-strength" {
			c.RegisterCheck(check.ID, func() (CheckStatus, string) {
				return StatusPass, "ok"
			})
		}
	}
	report := c.Run()
	output := report.FormatReport()

	if !strings.Contains(output, "FAIL") {
		t.Error("FormatReport missing FAIL")
	}
	if !strings.Contains(output, "password is only 10 chars") {
		t.Error("FormatReport missing failure detail")
	}
}

// =============================================================================
// CheckResult Tests
// =============================================================================

func TestCheckResult_IsPassed(t *testing.T) {
	tests := []struct {
		status CheckStatus
		want   bool
	}{
		{StatusPass, true},
		{StatusSkip, true},
		{StatusFail, false},
		{StatusWarn, false},
	}
	for _, tt := range tests {
		result := CheckResult{Status: tt.status}
		if got := result.IsPassed(); got != tt.want {
			t.Errorf("IsPassed() for %q = %v, want %v", tt.status, got, tt.want)
		}
	}
}

// =============================================================================
// File Permission Checker Tests
// =============================================================================

func TestCheckFilePermissions_CorrectPerms(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.env")
	if err := os.WriteFile(path, []byte("KEY=value"), 0600); err != nil {
		t.Fatal(err)
	}
	status, detail := CheckFilePermissions(path, 0600)
	if status != StatusPass {
		t.Errorf("Status = %q, want PASS: %s", status, detail)
	}
}

func TestCheckFilePermissions_WrongPerms(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.env")
	if err := os.WriteFile(path, []byte("KEY=value"), 0644); err != nil {
		t.Fatal(err)
	}
	status, _ := CheckFilePermissions(path, 0600)
	if status != StatusFail {
		t.Errorf("Status = %q, want FAIL (perms are 0644, not 0600)", status)
	}
}

func TestCheckFilePermissions_FileNotFound(t *testing.T) {
	status, _ := CheckFilePermissions("/nonexistent/path/file.env", 0600)
	if status != StatusFail {
		t.Errorf("Status = %q, want FAIL (file not found)", status)
	}
}

func TestCheckFileExists_Exists(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "exists.txt")
	if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	status, _ := CheckFileExists(path)
	if status != StatusPass {
		t.Errorf("Status = %q, want PASS", status)
	}
}

func TestCheckFileExists_NotFound(t *testing.T) {
	status, _ := CheckFileExists("/nonexistent/file.txt")
	if status != StatusFail {
		t.Errorf("Status = %q, want FAIL", status)
	}
}

// =============================================================================
// Hardening Summary Tests
// =============================================================================

func TestHardeningSummary_AllPass(t *testing.T) {
	c := NewHardeningChecker()
	for _, check := range c.ListChecks() {
		c.RegisterCheck(check.ID, func() (CheckStatus, string) {
			return StatusPass, "ok"
		})
	}
	report := c.Run()
	summary := NewHardeningSummary(report)

	if summary.TotalChecks != 22 {
		t.Errorf("TotalChecks = %d, want 22", summary.TotalChecks)
	}
	if summary.Passed != 22 {
		t.Errorf("Passed = %d, want 22", summary.Passed)
	}
	if summary.Failed != 0 {
		t.Errorf("Failed = %d, want 0", summary.Failed)
	}
	if summary.PassRate != 1.0 {
		t.Errorf("PassRate = %f, want 1.0", summary.PassRate)
	}
	if len(summary.CriticalGaps) != 0 {
		t.Errorf("CriticalGaps = %v, want empty", summary.CriticalGaps)
	}
}

func TestHardeningSummary_WithFailures(t *testing.T) {
	c := NewHardeningChecker()
	c.RegisterCheck("admin-password-strength", func() (CheckStatus, string) {
		return StatusFail, "too short"
	})
	c.RegisterCheck("reverse-proxy-tls", func() (CheckStatus, string) {
		return StatusFail, "no TLS"
	})
	for _, check := range c.ListChecks() {
		if check.ID != "admin-password-strength" && check.ID != "reverse-proxy-tls" {
			c.RegisterCheck(check.ID, func() (CheckStatus, string) {
				return StatusPass, "ok"
			})
		}
	}
	report := c.Run()
	summary := NewHardeningSummary(report)

	if summary.Failed != 2 {
		t.Errorf("Failed = %d, want 2", summary.Failed)
	}
	if len(summary.CriticalGaps) != 2 {
		t.Errorf("CriticalGaps = %d, want 2", len(summary.CriticalGaps))
	}
}

func TestHardeningSummary_FormatSummary(t *testing.T) {
	c := NewHardeningChecker()
	for _, check := range c.ListChecks() {
		c.RegisterCheck(check.ID, func() (CheckStatus, string) {
			return StatusPass, "ok"
		})
	}
	report := c.Run()
	summary := NewHardeningSummary(report)
	output := summary.FormatSummary()

	if !strings.Contains(output, "22/22") {
		t.Errorf("FormatSummary() missing count: %s", output)
	}
	if !strings.Contains(output, "100%") {
		t.Errorf("FormatSummary() missing percentage: %s", output)
	}
}

func TestHardeningSummary_Empty(t *testing.T) {
	report := &HardeningReport{Results: []CheckResult{}}
	summary := NewHardeningSummary(report)
	if summary.PassRate != 0 {
		t.Errorf("PassRate = %f, want 0 (no checks)", summary.PassRate)
	}
}

// =============================================================================
// Port Check Test (skipped by default)
// =============================================================================

func TestCheckPortNotPublic_DefaultSkipped(t *testing.T) {
	status, _ := CheckPortNotPublic(3000)
	if status != StatusSkip {
		t.Errorf("Status = %q, want SKIP (placeholder)", status)
	}
}

// =============================================================================
// Deterministic Ordering Test
// =============================================================================

func TestHardeningChecker_DeterministicOrder(t *testing.T) {
	c := NewHardeningChecker()
	for _, check := range c.ListChecks() {
		c.RegisterCheck(check.ID, func() (CheckStatus, string) {
			return StatusPass, "ok"
		})
	}
	report1 := c.Run()
	report2 := c.Run()

	for i := range report1.Results {
		if report1.Results[i].Check.ID != report2.Results[i].Check.ID {
			t.Errorf("Result %d: ID mismatch %q vs %q (non-deterministic order)",
				i, report1.Results[i].Check.ID, report2.Results[i].Check.ID)
		}
	}
}

// =============================================================================
// Timestamp Test
// =============================================================================

func TestHardeningReport_Timestamp(t *testing.T) {
	c := NewHardeningChecker()
	report := c.Run()

	if report.CheckedAt.IsZero() {
		t.Error("CheckedAt is zero")
	}
	if report.CheckedAt.After(time.Now().Add(time.Second)) {
		t.Error("CheckedAt is in the future")
	}
}

func TestCheckResult_Timestamp(t *testing.T) {
	c := NewHardeningChecker()
	c.RegisterCheck("admin-password-strength", func() (CheckStatus, string) {
		return StatusPass, "ok"
	})
	result, _ := c.RunCheck("admin-password-strength")
	if result.CheckedAt.IsZero() {
		t.Error("CheckedAt is zero")
	}
}
