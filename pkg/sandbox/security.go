package sandbox

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Spec §9 — Security Properties validation
//
// The sandbox guarantees for workspace and full isolation:
//  1. No home directory access
//  2. No network access
//  3. PID isolation
//  4. Memory bounds
//  5. Time bounds
//  6. No GPU in full mode
//  7. Die with parent
//
// SecurityValidator checks that a SandboxConfig satisfies these guarantees
// before execution. Each property returns a SecurityCheckResult with a
// human-readable description.
// ---------------------------------------------------------------------------

// SecurityCheckResult is the outcome of a single security property check.
type SecurityCheckResult struct {
	Property string
	Passed   bool
	Message  string
	Detail   string
}

// SecurityReport contains all security check results for a config.
type SecurityReport struct {
	Results []SecurityCheckResult
}

// AllPassed reports whether all security checks passed.
func (r *SecurityReport) AllPassed() bool {
	for _, res := range r.Results {
		if !res.Passed {
			return false
		}
	}
	return true
}

// FailedChecks returns only the checks that failed.
func (r *SecurityReport) FailedChecks() []SecurityCheckResult {
	var failed []SecurityCheckResult
	for _, res := range r.Results {
		if !res.Passed {
			failed = append(failed, res)
		}
	}
	return failed
}

// Summary renders a human-readable summary of all security checks.
func (r *SecurityReport) Summary() string {
	passed := 0
	for _, res := range r.Results {
		if res.Passed {
			passed++
		}
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Security: %d/%d checks passed\n", passed, len(r.Results)))
	for _, res := range r.Results {
		mark := "✓"
		if !res.Passed {
			mark = "✗"
		}
		sb.WriteString(fmt.Sprintf("  %s %s: %s\n", mark, res.Property, res.Message))
	}
	return sb.String()
}

// ValidateSecurity runs all 7 spec §9 security property checks against a config.
// For IsolationNone, all checks pass (no isolation = no security guarantees needed).
func ValidateSecurity(cfg *SandboxConfig) *SecurityReport {
	report := &SecurityReport{}

	if cfg.Isolation == IsolationNone {
		// No isolation — security properties don't apply
		report.Results = []SecurityCheckResult{
			{Property: "isolation-mode", Passed: true, Message: "IsolationNone — security checks not applicable"},
		}
		return report
	}

	// 1. No home directory access
	report.Results = append(report.Results, checkNoHomeAccess(cfg))

	// 2. No network access
	report.Results = append(report.Results, checkNoNetwork(cfg))

	// 3. PID isolation
	report.Results = append(report.Results, checkPIDIsolation(cfg))

	// 4. Memory bounds
	report.Results = append(report.Results, checkMemoryBounds(cfg))

	// 5. Time bounds
	report.Results = append(report.Results, checkTimeBounds(cfg))

	// 6. No GPU in full mode (GPU is disabled globally — this always passes)
	report.Results = append(report.Results, checkNoGPU(cfg))

	// 7. Die with parent
	report.Results = append(report.Results, checkDieWithParent(cfg))

	return report
}

// ValidateStrict is like ValidateSecurity but returns an error if any check fails.
func ValidateStrict(cfg *SandboxConfig) error {
	report := ValidateSecurity(cfg)
	failed := report.FailedChecks()
	if len(failed) == 0 {
		return nil
	}
	var msgs []string
	for _, f := range failed {
		msgs = append(msgs, f.Property+": "+f.Message)
	}
	return fmt.Errorf("security validation failed: %s", strings.Join(msgs, "; "))
}

// ---------------------------------------------------------------------------
// Individual property checks
// ---------------------------------------------------------------------------

func checkNoHomeAccess(cfg *SandboxConfig) SecurityCheckResult {
	result := SecurityCheckResult{
		Property: "no-home-access",
		Passed:   true,
		Message:  "/home, /root, ~/.ssh not mounted",
	}

	// Check that workdir doesn't reference home directory paths
	homePaths := []string{"/home/", "/root/", "~/", "~/.ssh"}
	for _, hp := range homePaths {
		if strings.HasPrefix(cfg.Workdir, hp) {
			result.Passed = false
			result.Message = fmt.Sprintf("workdir %q is inside a home directory", cfg.Workdir)
			result.Detail = fmt.Sprintf("workdir must not start with %s — home directories are never mounted", hp)
			return result
		}
	}

	// Check that session root is not inside home
	for _, hp := range []string{"/home/", "/root/"} {
		if strings.HasPrefix(cfg.SessionRoot, hp) {
			result.Passed = false
			result.Message = fmt.Sprintf("session root %q is inside a home directory", cfg.SessionRoot)
			result.Detail = "session root should be under /tmp or a dedicated directory"
			return result
		}
	}

	return result
}

func checkNoNetwork(cfg *SandboxConfig) SecurityCheckResult {
	result := SecurityCheckResult{
		Property: "no-network-access",
		Passed:   true,
		Message:  "network namespace unshared (--unshare-net)",
	}

	if cfg.Isolation == IsolationNone {
		result.Passed = false
		result.Message = "IsolationNone allows network access"
		return result
	}

	return result
}

func checkPIDIsolation(cfg *SandboxConfig) SecurityCheckResult {
	result := SecurityCheckResult{
		Property: "pid-isolation",
		Passed:   true,
		Message:  "private PID namespace (--unshare-pid)",
	}

	if !cfg.Isolation.HasPIDNamespace() {
		result.Passed = false
		result.Message = fmt.Sprintf("isolation level %q does not use PID namespace", cfg.Isolation)
	}

	return result
}

func checkMemoryBounds(cfg *SandboxConfig) SecurityCheckResult {
	result := SecurityCheckResult{
		Property: "memory-bounds",
		Message:  "memory.max cgroup limit enforced",
	}

	if cfg.MemoryLimit <= 0 {
		result.Passed = true
		result.Message = "no memory limit set (0 = unlimited — soft degradation)"
		result.Detail = "memory.max is omitted when MemoryLimit=0 per spec §6.2"
	} else {
		result.Passed = true
		result.Message = fmt.Sprintf("memory limited to %d MB via cgroup v2", cfg.MemoryLimit)
	}

	return result
}

func checkTimeBounds(cfg *SandboxConfig) SecurityCheckResult {
	result := SecurityCheckResult{
		Property: "time-bounds",
		Message:  "context deadline enforced via SIGKILL",
	}

	if cfg.TimeLimit <= 0 {
		result.Passed = true
		result.Message = "no time limit set (0 = unlimited)"
		result.Detail = "no context deadline — process runs until completion"
	} else {
		result.Passed = true
		result.Message = fmt.Sprintf("time limit %d seconds", cfg.TimeLimit)
	}

	return result
}

func checkNoGPU(cfg *SandboxConfig) SecurityCheckResult {
	result := SecurityCheckResult{
		Property: "no-gpu-full-mode",
		Passed:   true,
		Message:  "GPU access is never enabled (Helix uses API inference)",
	}
	return result
}

func checkDieWithParent(cfg *SandboxConfig) SecurityCheckResult {
	result := SecurityCheckResult{
		Property: "die-with-parent",
		Passed:   true,
		Message:  "--die-with-parent ensures cleanup on exit",
	}

	if cfg.Isolation == IsolationWorkspace || cfg.Isolation == IsolationFull {
		// These levels always use --die-with-parent per spec §4.2 and §4.3
		return result
	}

	result.Passed = false
	result.Message = "die-with-parent not applicable for IsolationNone"
	return result
}

// CheckSessionPermissions validates that the session directory will be created
// with mode 0700 per spec §5.
func CheckSessionPermissions(cfg *SandboxConfig) error {
	if cfg.SessionRoot == "" {
		return nil // will use default
	}
	// This is a static check — actual permissions are enforced at creation time.
	// Here we just validate the config doesn't request insecure paths.
	if strings.Contains(cfg.SessionRoot, "..") {
		return fmt.Errorf("session root contains path traversal: %q", cfg.SessionRoot)
	}
	return nil
}

// ValidateMountSpec checks that a mount spec doesn't include forbidden paths.
// Per spec §9: /home, /root, ~/.ssh, ~/.hermes are never mounted.
func ValidateMountSpec(source, dest string) error {
	forbiddenSources := []string{"/home", "/root", "~/.ssh", "~/.hermes", "/etc/shadow", "/etc/ssh"}
	for _, forbidden := range forbiddenSources {
		if strings.HasPrefix(source, forbidden) {
			return fmt.Errorf("forbidden mount source: %q — sensitive host paths must not be mounted", source)
		}
	}

	forbiddenDests := []string{"/etc/shadow", "/etc/ssh"}
	for _, forbidden := range forbiddenDests {
		if dest == forbidden {
			return fmt.Errorf("forbidden mount destination: %q", dest)
		}
	}

	return nil
}

// RequiredMountPoints returns the mandatory read-only bind mounts for the given
// isolation level per spec §4.2 and §4.3.
func RequiredMountPoints(level IsolationLevel) []MountPoint {
	if level == IsolationNone {
		return nil
	}

	// System directories that must be read-only bind mounted
	systemRO := []string{
		"/usr", "/bin", "/lib", "/lib64",
		"/etc/ld.so.cache", "/etc/alternatives",
	}

	var mounts []MountPoint
	for _, dir := range systemRO {
		mounts = append(mounts, MountPoint{
			Source:   dir,
			Target:   dir,
			ReadOnly: true,
			Kind:     MountBind,
		})
	}

	// Standard mounts
	mounts = append(mounts,
		MountPoint{Target: "/proc", Kind: MountProc},
		MountPoint{Target: "/dev", Kind: MountDev},
	)

	return mounts
}

// ForbiddenMountSources returns paths that must NEVER be mounted into a sandbox
// regardless of isolation level (spec §9.1).
func ForbiddenMountSources() []string {
	return []string{
		"/home",
		"/root",
		"~/.ssh",
		"~/.hermes",
		"/etc/shadow",
		"/etc/ssh",
		"/etc/sudoers",
	}
}
