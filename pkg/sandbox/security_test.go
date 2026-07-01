package sandbox

import (
	"strings"
	"testing"
)

func testConfig(isolation IsolationLevel) *SandboxConfig {
	return &SandboxConfig{
		SessionID:   "test-session",
		Isolation:   isolation,
		Workdir:     "/workspace",
		TimeLimit:   300,
		MemoryLimit: 1024,
		Network:     NetworkNone,
		SessionRoot: "/tmp/helix-sandbox",
		BwrapPath:   "/usr/bin/bwrap",
		CgroupRoot:  "/sys/fs/cgroup",
	}
}

func TestValidateSecurity_None(t *testing.T) {
	cfg := testConfig(IsolationNone)
	report := ValidateSecurity(cfg)

	if !report.AllPassed() {
		t.Error("IsolationNone should pass all checks")
	}
	if len(report.Results) != 1 {
		t.Errorf("expected 1 result (skip), got %d", len(report.Results))
	}
}

func TestValidateSecurity_Workspace_AllPassed(t *testing.T) {
	cfg := testConfig(IsolationWorkspace)
	report := ValidateSecurity(cfg)

	if !report.AllPassed() {
		t.Error("valid workspace config should pass all checks")
		failed := report.FailedChecks()
		for _, f := range failed {
			t.Logf("FAILED: %s: %s", f.Property, f.Message)
		}
	}
	if len(report.Results) != 7 {
		t.Errorf("expected 7 checks, got %d", len(report.Results))
	}
}

func TestValidateSecurity_Full_AllPassed(t *testing.T) {
	cfg := testConfig(IsolationFull)
	report := ValidateSecurity(cfg)

	if !report.AllPassed() {
		t.Error("valid full config should pass all checks")
		failed := report.FailedChecks()
		for _, f := range failed {
			t.Logf("FAILED: %s: %s", f.Property, f.Message)
		}
	}
}

func TestValidateSecurity_HomeAccessViolation(t *testing.T) {
	cfg := testConfig(IsolationWorkspace)
	cfg.Workdir = "/home/user/project"
	report := ValidateSecurity(cfg)

	failed := report.FailedChecks()
	found := false
	for _, f := range failed {
		if f.Property == "no-home-access" {
			found = true
			if !strings.Contains(f.Message, "home directory") {
				t.Errorf("unexpected message: %s", f.Message)
			}
		}
	}
	if !found {
		t.Error("expected no-home-access check to fail")
	}
}

func TestValidateSecurity_SessionRootHomeViolation(t *testing.T) {
	cfg := testConfig(IsolationWorkspace)
	cfg.SessionRoot = "/home/user/sessions"
	report := ValidateSecurity(cfg)

	failed := report.FailedChecks()
	found := false
	for _, f := range failed {
		if f.Property == "no-home-access" {
			found = true
		}
	}
	if !found {
		t.Error("expected no-home-access check to fail for session root in /home")
	}
}

func TestValidateSecurity_MemoryUnlimited(t *testing.T) {
	cfg := testConfig(IsolationWorkspace)
	cfg.MemoryLimit = 0
	report := ValidateSecurity(cfg)

	for _, res := range report.Results {
		if res.Property == "memory-bounds" {
			if !res.Passed {
				t.Error("memory-bounds with 0 should pass (soft degradation)")
			}
			if !strings.Contains(res.Message, "unlimited") && !strings.Contains(res.Message, "no memory limit") {
				t.Logf("message: %s", res.Message)
			}
		}
	}
}

func TestValidateSecurity_TimeUnlimited(t *testing.T) {
	cfg := testConfig(IsolationWorkspace)
	cfg.TimeLimit = 0
	report := ValidateSecurity(cfg)

	for _, res := range report.Results {
		if res.Property == "time-bounds" {
			if !res.Passed {
				t.Error("time-bounds with 0 should pass (no limit)")
			}
		}
	}
}

func TestValidateStrict_Passes(t *testing.T) {
	cfg := testConfig(IsolationWorkspace)
	err := ValidateStrict(cfg)
	if err != nil {
		t.Errorf("valid config should not error: %v", err)
	}
}

func TestValidateStrict_Fails(t *testing.T) {
	cfg := testConfig(IsolationWorkspace)
	cfg.Workdir = "/home/user/project"
	err := ValidateStrict(cfg)
	if err == nil {
		t.Error("invalid config should return error")
	}
	if !strings.Contains(err.Error(), "no-home-access") {
		t.Errorf("error should mention no-home-access: %v", err)
	}
}

func TestSecurityReport_Summary(t *testing.T) {
	cfg := testConfig(IsolationWorkspace)
	report := ValidateSecurity(cfg)
	summary := report.Summary()

	if !strings.Contains(summary, "checks passed") {
		t.Error("summary missing pass count")
	}
}

func TestSecurityReport_FailedChecks(t *testing.T) {
	cfg := testConfig(IsolationWorkspace)
	cfg.Workdir = "/home/user/project"
	report := ValidateSecurity(cfg)

	failed := report.FailedChecks()
	if len(failed) == 0 {
		t.Error("expected at least one failed check")
	}
}

func TestCheckSessionPermissions_OK(t *testing.T) {
	cfg := testConfig(IsolationWorkspace)
	err := CheckSessionPermissions(cfg)
	if err != nil {
		t.Errorf("valid session root should not error: %v", err)
	}
}

func TestCheckSessionPermissions_Traversal(t *testing.T) {
	cfg := testConfig(IsolationWorkspace)
	cfg.SessionRoot = "/tmp/../etc/passwd"
	err := CheckSessionPermissions(cfg)
	if err == nil {
		t.Error("path traversal should error")
	}
	if !strings.Contains(err.Error(), "traversal") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateMountSpec_Allowed(t *testing.T) {
	err := ValidateMountSpec("/usr", "/usr")
	if err != nil {
		t.Errorf("mounting /usr should be allowed: %v", err)
	}
}

func TestValidateMountSpec_ForbiddenSource(t *testing.T) {
	forbidden := []string{"/home", "/root", "/etc/shadow", "/etc/ssh"}
	for _, path := range forbidden {
		err := ValidateMountSpec(path, "/mnt")
		if err == nil {
			t.Errorf("mounting %s should be forbidden", path)
		}
		if !strings.Contains(err.Error(), "forbidden") {
			t.Errorf("error should mention 'forbidden': %v", err)
		}
	}
}

func TestValidateMountSpec_ForbiddenDest(t *testing.T) {
	err := ValidateMountSpec("/tmp", "/etc/shadow")
	if err == nil {
		t.Error("mounting to /etc/shadow should be forbidden")
	}
}

func TestRequiredMountPoints_None(t *testing.T) {
	mounts := RequiredMountPoints(IsolationNone)
	if mounts != nil {
		t.Error("IsolationNone should have no required mounts")
	}
}

func TestRequiredMountPoints_Workspace(t *testing.T) {
	mounts := RequiredMountPoints(IsolationWorkspace)
	if len(mounts) == 0 {
		t.Fatal("expected mounts for workspace")
	}

	foundUSR := false
	foundProc := false
	foundDev := false
	for _, m := range mounts {
		if m.Source == "/usr" && m.ReadOnly {
			foundUSR = true
		}
		if m.Target == "/proc" && m.Kind == MountProc {
			foundProc = true
		}
		if m.Target == "/dev" && m.Kind == MountDev {
			foundDev = true
		}
	}
	if !foundUSR {
		t.Error("missing /usr readonly bind mount")
	}
	if !foundProc {
		t.Error("missing /proc mount")
	}
	if !foundDev {
		t.Error("missing /dev mount")
	}
}

func TestForbiddenMountSources(t *testing.T) {
	sources := ForbiddenMountSources()
	if len(sources) == 0 {
		t.Fatal("expected forbidden mount sources")
	}

	checks := map[string]bool{
		"/home":    false,
		"/root":    false,
		"~/.ssh":   false,
		"/etc/ssh": false,
	}
	for _, s := range sources {
		if _, ok := checks[s]; ok {
			checks[s] = true
		}
	}
	for path, found := range checks {
		if !found {
			t.Errorf("%s should be in forbidden mount sources", path)
		}
	}
}
