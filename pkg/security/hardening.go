// Package security implements the security hardening checklist verifier
// and incident response engine per spec §6.6 and §6.7.
//
// The hardening checker validates that all deployment and operational
// security measures from the spec are in place before the platform
// goes to production. Each check returns PASS/FAIL/WARN with detail.
package security

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// =============================================================================
// Check Status
// =============================================================================

// CheckStatus represents the result of a single hardening check.
type CheckStatus string

const (
	StatusPass CheckStatus = "PASS"
	StatusFail CheckStatus = "FAIL"
	StatusWarn CheckStatus = "WARN"
	StatusSkip CheckStatus = "SKIP"
)

// CheckCategory classifies checks as deployment or operational.
type CheckCategory string

const (
	CategoryDeployment  CheckCategory = "deployment"
	CategoryOperational CheckCategory = "operational"
)

// =============================================================================
// Hardening Check
// =============================================================================

// HardeningCheck is a single item on the security hardening checklist.
type HardeningCheck struct {
	ID          string        // Unique check identifier
	Name        string        // Human-readable name
	Category    CheckCategory // Deployment or operational
	Description string        // What the check verifies
	Severity    CheckStatus   // Default severity if check fails (FAIL or WARN)
}

// CheckResult is the outcome of running a hardening check.
type CheckResult struct {
	Check     HardeningCheck
	Status    CheckStatus
	Detail    string // Explanation of the result
	CheckedAt time.Time
}

// IsPassed returns true if the check passed or was skipped.
func (r CheckResult) IsPassed() bool {
	return r.Status == StatusPass || r.Status == StatusSkip
}

// =============================================================================
// Hardening Report
// =============================================================================

// HardeningReport aggregates all check results.
type HardeningReport struct {
	Results      []CheckResult
	AllPassed    bool
	FailedCount  int
	WarningCount int
	PassedCount  int
	SkippedCount int
	CheckedAt    time.Time
}

// FailedChecks returns all checks that failed (status = FAIL).
func (r *HardeningReport) FailedChecks() []CheckResult {
	var failed []CheckResult
	for _, res := range r.Results {
		if res.Status == StatusFail {
			failed = append(failed, res)
		}
	}
	return failed
}

// WarningChecks returns all checks that produced warnings.
func (r *HardeningReport) WarningChecks() []CheckResult {
	var warnings []CheckResult
	for _, res := range r.Results {
		if res.Status == StatusWarn {
			warnings = append(warnings, res)
		}
	}
	return warnings
}

// FormatReport renders the hardening report as a human-readable string.
func (r *HardeningReport) FormatReport() string {
	var b strings.Builder
	status := "PASS"
	if !r.AllPassed {
		status = "FAIL"
	}
	fmt.Fprintf(&b, "Security Hardening Report — %s\n", status)
	fmt.Fprintf(&b, "Checked: %s\n\n", r.CheckedAt.Format(time.RFC3339))

	// Group by category
	for _, cat := range []CheckCategory{CategoryDeployment, CategoryOperational} {
		// Capitalize first letter for display
		catName := string(cat)
		if len(catName) > 0 {
			catName = strings.ToUpper(catName[:1]) + catName[1:]
		}
		fmt.Fprintf(&b, "  %s:\n", catName)
		for _, res := range r.Results {
			if res.Check.Category != cat {
				continue
			}
			icon := "✓"
			switch res.Status {
			case StatusFail:
				icon = "✗"
			case StatusWarn:
				icon = "⚠"
			case StatusSkip:
				icon = "⊘"
			}
			fmt.Fprintf(&b, "    %s %s", icon, res.Check.Name)
			switch res.Status {
			case StatusPass:
				b.WriteString(" — OK\n")
			case StatusSkip:
				b.WriteString(" — SKIPPED\n")
			default:
				fmt.Fprintf(&b, " — %s\n", res.Detail)
			}
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "Summary: %d passed, %d failed, %d warnings, %d skipped\n",
		r.PassedCount, r.FailedCount, r.WarningCount, r.SkippedCount)
	return b.String()
}

// =============================================================================
// Hardening Checker
// =============================================================================

// CheckFunc is a function that validates a single hardening item.
// It returns a CheckStatus and a detail message.
type CheckFunc func() (CheckStatus, string)

// HardeningChecker runs all security hardening checks.
type HardeningChecker struct {
	checks map[string]HardeningCheck
	funcs  map[string]CheckFunc
}

// NewHardeningChecker creates a checker with all default checks from spec §6.6.
// The checks are registered but not yet executed — call Run() to execute them.
func NewHardeningChecker() *HardeningChecker {
	c := &HardeningChecker{
		checks: make(map[string]HardeningCheck),
		funcs:  make(map[string]CheckFunc),
	}
	for _, check := range DefaultChecks() {
		c.checks[check.ID] = check
	}
	return c
}

// RegisterCheck adds a custom check function for a registered check.
func (c *HardeningChecker) RegisterCheck(id string, fn CheckFunc) {
	c.funcs[id] = fn
}

// Run executes all registered checks and returns a hardening report.
// Checks without registered functions are marked as SKIP.
func (c *HardeningChecker) Run() *HardeningReport {
	report := &HardeningReport{
		CheckedAt: time.Now().UTC(),
		AllPassed: true,
	}

	// Sort checks by ID for deterministic output
	ids := make([]string, 0, len(c.checks))
	for id := range c.checks {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		check := c.checks[id]
		result := CheckResult{
			Check:     check,
			CheckedAt: time.Now().UTC(),
		}

		if fn, ok := c.funcs[id]; ok {
			status, detail := fn()
			result.Status = status
			result.Detail = detail
		} else {
			result.Status = StatusSkip
			result.Detail = "no check function registered"
		}

		switch result.Status {
		case StatusPass:
			report.PassedCount++
		case StatusFail:
			report.FailedCount++
			report.AllPassed = false
		case StatusWarn:
			report.WarningCount++
		case StatusSkip:
			report.SkippedCount++
		}

		report.Results = append(report.Results, result)
	}

	return report
}

// RunCheck executes a single check by ID.
func (c *HardeningChecker) RunCheck(id string) (CheckResult, bool) {
	check, exists := c.checks[id]
	if !exists {
		return CheckResult{}, false
	}
	result := CheckResult{
		Check:     check,
		CheckedAt: time.Now().UTC(),
	}
	if fn, ok := c.funcs[id]; ok {
		status, detail := fn()
		result.Status = status
		result.Detail = detail
	} else {
		result.Status = StatusSkip
		result.Detail = "no check function registered"
	}
	return result, true
}

// ListChecks returns all registered checks sorted by ID.
func (c *HardeningChecker) ListChecks() []HardeningCheck {
	ids := make([]string, 0, len(c.checks))
	for id := range c.checks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	checks := make([]HardeningCheck, 0, len(ids))
	for _, id := range ids {
		checks = append(checks, c.checks[id])
	}
	return checks
}

// =============================================================================
// Default Checks from Spec §6.6
// =============================================================================

// DefaultChecks returns all hardening checks defined in spec §6.6.
func DefaultChecks() []HardeningCheck {
	deployment := []HardeningCheck{
		{
			ID:          "admin-password-strength",
			Name:        "Forgejo admin password is strong (32+ chars random)",
			Category:    CategoryDeployment,
			Description: "Admin password must be 32+ random characters, stored in a password manager",
			Severity:    StatusFail,
		},
		{
			ID:          "reverse-proxy-tls",
			Name:        "Forgejo web UI behind reverse proxy (Caddy/Traefik) with TLS",
			Category:    CategoryDeployment,
			Description: "All external access must go through a TLS-terminating reverse proxy",
			Severity:    StatusFail,
		},
		{
			ID:          "port-binding-localhost",
			Name:        "Internal service ports bound to 127.0.0.1",
			Category:    CategoryDeployment,
			Description: "All internal services must bind to 127.0.0.1, not 0.0.0.0, except those needing Docker network access",
			Severity:    StatusFail,
		},
		{
			ID:          "userns-remap",
			Name:        "Docker user namespaces enabled (userns-remap)",
			Category:    CategoryDeployment,
			Description: "Docker user namespace remapping must be enabled for container isolation",
			Severity:    StatusFail,
		},
		{
			ID:          "no-privileged-containers",
			Name:        "No containers run with --privileged (except sandboxed DinD)",
			Category:    CategoryDeployment,
			Description: "No container should run with --privileged except DinD which is itself sandboxed",
			Severity:    StatusFail,
		},
		{
			ID:          "vpn-configured",
			Name:        "gluetun VPN configured for all agent sandboxes",
			Category:    CategoryDeployment,
			Description: "All agent sandbox containers must route network through gluetun VPN",
			Severity:    StatusFail,
		},
		{
			ID:          "env-file-perms",
			Name:        ".env files have permissions 600",
			Category:    CategoryDeployment,
			Description: "All .env files containing secrets must have file permissions 600",
			Severity:    StatusFail,
		},
		{
			ID:          "chimera-yaml-perms",
			Name:        "chimera.yaml has permissions 600",
			Category:    CategoryDeployment,
			Description: "chimera.yaml containing API keys must have file permissions 600",
			Severity:    StatusFail,
		},
		{
			ID:          "secrets-scanner-installed",
			Name:        "Secrets scanner (GitReins) installed in ALL repos",
			Category:    CategoryDeployment,
			Description: "GitReins pre-commit hook with secrets scanning must be installed before any agent commits",
			Severity:    StatusFail,
		},
		{
			ID:          "gitignore-coverage",
			Name:        ".gitignore covers all secret files in ALL repos",
			Category:    CategoryDeployment,
			Description: "Every repo must .gitignore .env, *.key, *.pem, chimera.yaml, .auth.json",
			Severity:    StatusFail,
		},
		{
			ID:          "branch-protection-main",
			Name:        "Branch protection on main in ALL repos (no direct push, approvals >= 2)",
			Category:    CategoryDeployment,
			Description: "main branch must have branch protection: no direct push, required approvals >= 2",
			Severity:    StatusFail,
		},
		{
			ID:          "ci-runner-isolation",
			Name:        "Forgejo Actions runners isolated (not on host)",
			Category:    CategoryDeployment,
			Description: "CI runners must run in isolated containers, not on the host machine",
			Severity:    StatusFail,
		},
		{
			ID:          "db-backups-daily",
			Name:        "LangFuse and Forgejo databases backed up daily",
			Category:    CategoryDeployment,
			Description: "Daily backups of LangFuse (Postgres) and Forgejo databases must be configured",
			Severity:    StatusFail,
		},
		{
			ID:          "ssh-key-only-auth",
			Name:        "SSH access to host restricted to key-based auth only",
			Category:    CategoryDeployment,
			Description: "SSH password authentication must be disabled, key-based auth only",
			Severity:    StatusFail,
		},
	}

	operational := []HardeningCheck{
		{
			ID:          "h4f-bridge-cron",
			Name:        "H4F bridge cron running every 5 minutes",
			Category:    CategoryOperational,
			Description: "H4F bridge cron (consistency + doctor + guardrail) must run every 5 minutes",
			Severity:    StatusWarn,
		},
		{
			ID:          "auto-repair-logging",
			Name:        "H4F auto-repair logging to /var/log/hermes-bridge.log",
			Category:    CategoryOperational,
			Description: "H4F bridge auto-repair must log to syslog-style log file",
			Severity:    StatusWarn,
		},
		{
			ID:          "key-budget-review",
			Name:        "OpenRouter key budgets reviewed weekly",
			Category:    CategoryOperational,
			Description: "Weekly review of OpenRouter key budgets to watch for budget creep",
			Severity:    StatusWarn,
		},
		{
			ID:          "trust-recalculation-daily",
			Name:        "Trust levels recalculated daily by Hivemind cron",
			Category:    CategoryOperational,
			Description: "Daily trust level recalculation via Hivemind cron job",
			Severity:    StatusWarn,
		},
		{
			ID:          "dependency-vuln-scan",
			Name:        "Dependency vulnerability scan runs in CI",
			Category:    CategoryOperational,
			Description: "CI must run govulncheck / npm audit / pip-audit for dependency vulnerabilities",
			Severity:    StatusWarn,
		},
		{
			ID:          "langfuse-cost-dashboards",
			Name:        "LangFuse cost dashboards reviewed weekly",
			Category:    CategoryOperational,
			Description: "Weekly review of LangFuse cost dashboards to detect anomalous spend",
			Severity:    StatusWarn,
		},
		{
			ID:          "failed-step-monitoring",
			Name:        "Failed step count monitored (spike = possible prompt injection)",
			Category:    CategoryOperational,
			Description: "Monitor failed step counts — spikes indicate possible prompt injection or model degradation",
			Severity:    StatusWarn,
		},
		{
			ID:          "force-merge-review",
			Name:        "force-merge label usage reviewed monthly",
			Category:    CategoryOperational,
			Description: "Monthly review of force-merge label usage — should be rare",
			Severity:    StatusWarn,
		},
	}

	return append(deployment, operational...)
}

// =============================================================================
// File Permission Checker — Helper for deployment checks
// =============================================================================

// CheckFilePermissions checks if a file has the expected permission bits.
// Returns PASS if the file exists and has the expected perms, FAIL otherwise.
func CheckFilePermissions(path string, want os.FileMode) (CheckStatus, string) {
	info, err := os.Stat(path)
	if err != nil {
		return StatusFail, fmt.Sprintf("file %s does not exist: %v", path, err)
	}
	got := info.Mode().Perm()
	if got != want {
		return StatusFail, fmt.Sprintf("file %s has perms %o, want %o", path, got, want)
	}
	return StatusPass, fmt.Sprintf("file %s has correct perms %o", path, want)
}

// CheckFileExists checks if a file exists.
// Returns PASS if the file exists, FAIL otherwise.
func CheckFileExists(path string) (CheckStatus, string) {
	_, err := os.Stat(path)
	if err != nil {
		return StatusFail, fmt.Sprintf("file %s does not exist: %v", path, err)
	}
	return StatusPass, fmt.Sprintf("file %s exists", path)
}

// CheckPortNotPublic checks if a port is NOT bound to 0.0.0.0.
// This is a placeholder — actual implementation would use netstat/ss.
// Returns PASS by default (caller should override with real check).
func CheckPortNotPublic(port int) (CheckStatus, string) {
	_ = port
	return StatusSkip, "port binding check not implemented — register custom function"
}

// =============================================================================
// Hardening Summary for CLI Output
// =============================================================================

// HardeningSummary provides a compact summary for CLI dashboards.
type HardeningSummary struct {
	TotalChecks  int
	Passed       int
	Failed       int
	Warnings     int
	Skipped      int
	PassRate     float64
	CriticalGaps []string // Names of failed deployment checks
}

// NewHardeningSummary creates a summary from a hardening report.
func NewHardeningSummary(report *HardeningReport) HardeningSummary {
	summary := HardeningSummary{
		TotalChecks: len(report.Results),
		Passed:      report.PassedCount,
		Failed:      report.FailedCount,
		Warnings:    report.WarningCount,
		Skipped:     report.SkippedCount,
	}
	if summary.TotalChecks > 0 {
		summary.PassRate = float64(summary.Passed) / float64(summary.TotalChecks)
	}
	for _, res := range report.FailedChecks() {
		if res.Check.Category == CategoryDeployment {
			summary.CriticalGaps = append(summary.CriticalGaps, res.Check.Name)
		}
	}
	return summary
}

// FormatSummary renders a compact one-line summary.
func (s HardeningSummary) FormatSummary() string {
	return fmt.Sprintf("Hardening: %d/%d passed (%.0f%%), %d failed, %d warnings",
		s.Passed, s.TotalChecks, s.PassRate*100, s.Failed, s.Warnings)
}
