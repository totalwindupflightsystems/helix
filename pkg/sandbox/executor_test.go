package sandbox

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// =============================================================================
// NewExecutor tests
// =============================================================================

// TestNewExecutor_ValidConfig verifies that NewExecutor succeeds with a valid
// IsolationWorkspace config.
func TestNewExecutor_ValidConfig(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:   "test-session",
		Isolation:   IsolationWorkspace,
		Workdir:     "/workspace",
		SessionRoot: t.TempDir(),
		BwrapPath:   "/usr/bin/bwrap",
		CgroupRoot:  "/sys/fs/cgroup",
		Command:     []string{"echo", "hello"},
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exec == nil {
		t.Fatal("expected non-nil executor")
	}
	if exec.Spec == nil {
		t.Fatal("expected non-nil mount spec for workspace isolation")
	}
	if exec.Config.SessionID != "test-session" {
		t.Errorf("SessionID = %q, want %q", exec.Config.SessionID, "test-session")
	}
}

// TestNewExecutor_InvalidConfig verifies that NewExecutor returns an error for
// an invalid isolation level.
func TestNewExecutor_InvalidConfig(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:   "test-session",
		Isolation:   IsolationLevel("bogus"),
		Workdir:     "/workspace",
		SessionRoot: t.TempDir(),
	}

	_, err := NewExecutor(cfg)
	if err == nil {
		t.Fatal("expected error for invalid isolation level")
	}
}

// =============================================================================
// SetOutput tests
// =============================================================================

// TestSetOutput_Overrides verifies that SetOutput changes the stdout/stderr
// writers.
func TestSetOutput_Overrides(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:   "test-session",
		Isolation:   IsolationNone,
		SessionRoot: t.TempDir(),
		Command:     []string{"echo", "hello"},
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	var outBuf, errBuf bytes.Buffer
	exec.SetOutput(&outBuf, &errBuf)

	if exec.stdout != &outBuf {
		t.Error("stdout was not set")
	}
	if exec.stderr != &errBuf {
		t.Error("stderr was not set")
	}
}

// TestSetOutput_NilDoesNotOverride verifies that passing nil does not change
// the existing writer.
func TestSetOutput_NilDoesNotOverride(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:   "test-session",
		Isolation:   IsolationNone,
		SessionRoot: t.TempDir(),
		Command:     []string{"echo", "hello"},
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	origStdout := exec.stdout
	origStderr := exec.stderr

	exec.SetOutput(nil, nil)

	if exec.stdout != origStdout {
		t.Error("stdout was changed when nil was passed")
	}
	if exec.stderr != origStderr {
		t.Error("stderr was changed when nil was passed")
	}
}

// =============================================================================
// SetupSessionDir / CleanupSessionDir tests
// =============================================================================

// TestSetupSessionDir_CreatesDirs verifies that SetupSessionDir creates all
// required directories.
func TestSetupSessionDir_CreatesDirs(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:   "test-session-create",
		Isolation:   IsolationWorkspace,
		Workdir:     "/workspace",
		SessionRoot: t.TempDir(),
		BwrapPath:   "/usr/bin/bwrap",
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	if err := exec.SetupSessionDir(); err != nil {
		t.Fatalf("SetupSessionDir: %v", err)
	}

	// Verify directories exist
	for _, d := range []string{
		cfg.SessionDir(),
		cfg.WorkspaceDir(),
		cfg.TmpDir(),
	} {
		info, err := os.Stat(d)
		if err != nil {
			t.Errorf("directory %q does not exist: %v", d, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("path %q is not a directory", d)
		}
	}
}

// TestCleanupSessionDir_RemovesDir verifies that CleanupSessionDir removes the
// session directory.
func TestCleanupSessionDir_RemovesDir(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:   "test-session-cleanup",
		Isolation:   IsolationWorkspace,
		Workdir:     "/workspace",
		SessionRoot: t.TempDir(),
		BwrapPath:   "/usr/bin/bwrap",
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	if err := exec.SetupSessionDir(); err != nil {
		t.Fatalf("SetupSessionDir: %v", err)
	}
	if err := exec.CleanupSessionDir(); err != nil {
		t.Fatalf("CleanupSessionDir: %v", err)
	}

	// Session dir should be gone
	_, err = os.Stat(cfg.SessionDir())
	if !os.IsNotExist(err) {
		t.Errorf("expected session dir to be removed, got: %v", err)
	}
}

// TestCleanupSessionDir_NoOpOnNonExistent verifies that CleanupSessionDir
// succeeds even if the directory was never created.
func TestCleanupSessionDir_NoOpOnNonExistent(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:   "nonexistent-session",
		Isolation:   IsolationNone,
		SessionRoot: t.TempDir(),
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	// Should not fail even though SetupSessionDir was never called
	if err := exec.CleanupSessionDir(); err != nil {
		t.Fatalf("CleanupSessionDir on non-existent dir: %v", err)
	}
}

// =============================================================================
// BwrapArgs tests
// =============================================================================

// TestBwrapArgs_IsolationNone verifies that BwrapArgs returns nil for
// IsolationNone.
func TestBwrapArgs_IsolationNone(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:   "test-session",
		Isolation:   IsolationNone,
		SessionRoot: t.TempDir(),
		Command:     []string{"echo", "hello"},
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	args, err := exec.BwrapArgs()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if args != nil {
		t.Errorf("expected nil args for IsolationNone, got %v", args)
	}
}

// TestBwrapArgs_NilSpec verifies that BwrapArgs returns an error when Spec is
// nil (non-None isolation but config that can't build a spec).
func TestBwrapArgs_NilSpec(t *testing.T) {
	exec := &BwrapExecutor{
		Config: SandboxConfig{
			Isolation:   IsolationWorkspace,
			SessionRoot: t.TempDir(),
			Workdir:     "/workspace",
		},
		Spec: nil,
	}

	_, err := exec.BwrapArgs()
	if err == nil {
		t.Fatal("expected error for nil spec")
	}
	if !errors.Is(err, ErrConfigInvalid) {
		t.Errorf("expected ErrConfigInvalid, got: %v", err)
	}
}

// TestBwrapArgs_WorkspaceFlags verifies that BwrapArgs for workspace isolation
// includes the expected flags: --bind, --unshare-net, --unshare-pid,
// --die-with-parent, --chdir, and command args.
func TestBwrapArgs_WorkspaceFlags(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:   "test-session-flags",
		Isolation:   IsolationWorkspace,
		Workdir:     "/workspace",
		SessionRoot: t.TempDir(),
		BwrapPath:   "/usr/bin/bwrap",
		Command:     []string{"go", "test"},
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	args, err := exec.BwrapArgs()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Convert to string for easier assertion
	argStr := " " + strings.Join(args, " ") + " "

	if !strings.Contains(argStr, " --bind ") {
		t.Error("expected --bind flag")
	}
	if !strings.Contains(argStr, " --unshare-net ") {
		t.Error("expected --unshare-net flag")
	}
	if !strings.Contains(argStr, " --unshare-pid ") {
		t.Error("expected --unshare-pid flag")
	}
	if !strings.Contains(argStr, " --die-with-parent ") {
		t.Error("expected --die-with-parent flag")
	}
	if !strings.Contains(argStr, " --chdir ") {
		t.Error("expected --chdir flag")
	}
	if !strings.Contains(argStr, " -- go test") {
		t.Errorf("expected command 'go test' after '--', got: %v", args)
	}
}

// TestBwrapArgs_FullIsolationEnvVars verifies that full isolation includes
// --clearenv and --setenv flags.
func TestBwrapArgs_FullIsolationEnvVars(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:   "test-session-full",
		Isolation:   IsolationFull,
		Workdir:     "/workspace",
		SessionRoot: t.TempDir(),
		BwrapPath:   "/usr/bin/bwrap",
		Command:     []string{"true"},
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	args, err := exec.BwrapArgs()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argStr := " " + strings.Join(args, " ") + " "
	if !strings.Contains(argStr, " --clearenv ") {
		t.Error("expected --clearenv flag for full isolation")
	}
	if !strings.Contains(argStr, " --setenv ") {
		t.Error("expected --setenv flag for full isolation")
	}
}

// TestBwrapArgs_DieWithParent verifies the --die-with-parent flag is included
// in workspace isolation.
func TestBwrapArgs_DieWithParent(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:   "test-session-dwp",
		Isolation:   IsolationWorkspace,
		Workdir:     "/workspace",
		SessionRoot: t.TempDir(),
		BwrapPath:   "/usr/bin/bwrap",
		Command:     []string{"true"},
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	args, err := exec.BwrapArgs()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasFlag(args, "--die-with-parent") {
		t.Error("expected --die-with-parent flag")
	}
}

// =============================================================================
// BwrapCommand tests
// =============================================================================

// TestBwrapCommand_IsolationNone verifies that BwrapCommand returns only the
// raw command for IsolationNone.
func TestBwrapCommand_IsolationNone(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:   "test-session",
		Isolation:   IsolationNone,
		SessionRoot: t.TempDir(),
		Command:     []string{"echo", "hello"},
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	cmd, err := exec.BwrapCommand()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(cmd, "echo") || !strings.Contains(cmd, "hello") {
		t.Errorf("expected raw command, got: %q", cmd)
	}
	// Should NOT contain bwrap
	if strings.Contains(cmd, "bwrap") {
		t.Errorf("expected no bwrap in command, got: %q", cmd)
	}
}

// TestBwrapCommand_WorkspaceIncludesBwrap verifies that BwrapCommand includes
// the bwrap binary path for workspace isolation.
func TestBwrapCommand_WorkspaceIncludesBwrap(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:   "test-session",
		Isolation:   IsolationWorkspace,
		Workdir:     "/workspace",
		SessionRoot: t.TempDir(),
		BwrapPath:   "/usr/bin/bwrap",
		Command:     []string{"echo", "hello"},
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	cmd, err := exec.BwrapCommand()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(cmd, "/usr/bin/bwrap") {
		t.Errorf("expected bwrap path in command: %q", cmd)
	}
	if !strings.Contains(cmd, "echo") || !strings.Contains(cmd, "hello") {
		t.Errorf("expected command args: %q", cmd)
	}
}

// =============================================================================
// DryRun tests
// =============================================================================

// TestDryRun_PrintsToStdout verifies that DryRun prints the command to stdout.
func TestDryRun_PrintsToStdout(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:   "test-session-dry",
		Isolation:   IsolationNone,
		SessionRoot: t.TempDir(),
		Command:     []string{"echo", "hello"},
		DryRun:      true,
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	var outBuf, errBuf bytes.Buffer
	exec.SetOutput(&outBuf, &errBuf)

	if err := exec.DryRun(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outStr := outBuf.String()
	errStr := errBuf.String()

	if !strings.Contains(outStr, "echo") {
		t.Errorf("stdout should contain command, got: %q", outStr)
	}
	if !strings.Contains(errStr, "Helix Sandbox Dry Run") {
		t.Errorf("stderr should contain header, got: %q", errStr)
	}
}

// TestDryRun_PrintsStructuredSummary verifies that DryRun's stderr output
// contains structured session info.
func TestDryRun_PrintsStructuredSummary(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:   "dry-summary-session",
		Isolation:   IsolationFull,
		Workdir:     "/workspace",
		TimeLimit:   300,
		MemoryLimit: 1024,
		Network:     NetworkNone,
		SessionRoot: t.TempDir(),
		BwrapPath:   "/usr/bin/bwrap",
		Command:     []string{"true"},
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	var outBuf, errBuf bytes.Buffer
	exec.SetOutput(&outBuf, &errBuf)

	if err := exec.DryRun(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	errStr := errBuf.String()
	expectedFields := []string{
		"Session ID:", "dry-summary-session",
		"Isolation:", "full",
		"Workdir:", "/workspace",
		"Time limit:", "300s",
		"Memory limit:", "1024MB",
		"Network:", "none",
		"Bwrap binary:", "/usr/bin/bwrap",
	}
	for _, field := range expectedFields {
		if !strings.Contains(errStr, field) {
			t.Errorf("stderr missing %q: %q", field, errStr)
		}
	}
}

// =============================================================================
// Run tests
// =============================================================================

// TestRun_IsolationNone_Success verifies that Run succeeds for a simple
// command in IsolationNone mode (direct host execution).
func TestRun_IsolationNone_Success(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:   "test-session-run",
		Isolation:   IsolationNone,
		SessionRoot: t.TempDir(),
		BwrapPath:   "/bin/true",
		Command:     []string{"true"},
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	var outBuf, errBuf bytes.Buffer
	exec.SetOutput(&outBuf, &errBuf)

	err = exec.Run(context.Background())
	if err != nil {
		t.Fatalf("expected nil error for IsolationNone true, got: %v", err)
	}
}

// TestRun_IsolationNone_FailedCommand verifies that Run returns
// ErrExecutionFailed for a command that exits non-zero in IsolationNone mode.
func TestRun_IsolationNone_FailedCommand(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:   "test-session-fail",
		Isolation:   IsolationNone,
		SessionRoot: t.TempDir(),
		BwrapPath:   "/bin/true",
		Command:     []string{"false"},
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	var outBuf, errBuf bytes.Buffer
	exec.SetOutput(&outBuf, &errBuf)

	err = exec.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for exit-code-1 command")
	}
	if !errors.Is(err, ErrExecutionFailed) {
		t.Errorf("expected ErrExecutionFailed, got: %v", err)
	}
}

// TestRun_DryRunMode verifies that Run delegates to DryRun when DryRun is true.
func TestRun_DryRunMode(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:   "test-session-dryrun",
		Isolation:   IsolationNone,
		SessionRoot: t.TempDir(),
		Command:     []string{"echo", "hello"},
		DryRun:      true,
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	var outBuf, errBuf bytes.Buffer
	exec.SetOutput(&outBuf, &errBuf)

	// Use a real context for DryRun delegation
	// The Run function checks DryRun first, before needing context
	err = exec.Run(context.TODO())
	if err != nil {
		t.Fatalf("unexpected error in dry-run mode: %v", err)
	}
	if !strings.Contains(outBuf.String(), "echo") {
		t.Errorf("expected command in output: %q", outBuf.String())
	}
}

// =============================================================================
// RunWithTimeout tests
// =============================================================================

// TestRunWithTimeout_NoTimeLimit verifies that RunWithTimeout works without a
// configured time limit. With real execution and a simple command, it should
// succeed.
func TestRunWithTimeout_NoTimeLimit(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:   "test-session-ntl",
		Isolation:   IsolationNone,
		TimeLimit:   0, // no limit
		SessionRoot: t.TempDir(),
		BwrapPath:   "/bin/true",
		Command:     []string{"true"},
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	var outBuf, errBuf bytes.Buffer
	exec.SetOutput(&outBuf, &errBuf)

	err = exec.RunWithTimeout()
	if err != nil {
		t.Fatalf("expected nil error for IsolationNone true with no time limit, got: %v", err)
	}
}

// TestRunWithTimeout_WithTimeLimit verifies that RunWithTimeout applies the
// configured time limit. With real execution and a generous limit, the
// simple command should succeed.
func TestRunWithTimeout_WithTimeLimit(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:   "test-session-wtl",
		Isolation:   IsolationNone,
		TimeLimit:   30,
		SessionRoot: t.TempDir(),
		BwrapPath:   "/bin/true",
		Command:     []string{"true"},
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	var outBuf, errBuf bytes.Buffer
	exec.SetOutput(&outBuf, &errBuf)

	err = exec.RunWithTimeout()
	if err != nil {
		t.Fatalf("expected nil error for IsolationNone true with 30s limit, got: %v", err)
	}
}

// TestRunWithTimeout_TimedOut verifies that RunWithTimeout returns
// ErrTimeoutExceeded when the time limit is shorter than command runtime.
func TestRunWithTimeout_TimedOut(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:   "test-session-to",
		Isolation:   IsolationNone,
		TimeLimit:   1, // 1 second — sleep will exceed this
		SessionRoot: t.TempDir(),
		BwrapPath:   "/bin/true",
		Command:     []string{"sleep", "10"},
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	var outBuf, errBuf bytes.Buffer
	exec.SetOutput(&outBuf, &errBuf)

	err = exec.RunWithTimeout()
	if err == nil {
		t.Fatal("expected error for timed-out command")
	}
	if !errors.Is(err, ErrTimeoutExceeded) {
		t.Errorf("expected ErrTimeoutExceeded, got: %v", err)
	}
}

// =============================================================================
// shellEscape tests
// =============================================================================

// TestShellEscape_NoQuoting verifies that safe arguments are not quoted.
func TestShellEscape_NoQuoting(t *testing.T) {
	result := shellEscape([]string{"echo", "hello"})
	if result != "echo hello" {
		t.Errorf("expected 'echo hello', got %q", result)
	}
}

// TestShellEscape_WithWhitespace verifies that arguments with spaces are
// single-quoted.
func TestShellEscape_WithWhitespace(t *testing.T) {
	result := shellEscape([]string{"echo", "hello world"})
	if !strings.Contains(result, "'hello world'") {
		t.Errorf("expected quoted 'hello world', got %q", result)
	}
}

// TestShellEscape_SingleQuotes verifies that single quotes inside arguments are
// escaped.
func TestShellEscape_SingleQuotes(t *testing.T) {
	result := shellEscape([]string{"echo", "it's"})
	// Should contain single-quote escaping: 'it'\''s'
	if !strings.Contains(result, "'\\''") || !strings.Contains(result, "it") {
		t.Errorf("expected escaped single quotes, got %q", result)
	}
}

// TestShellEscape_SpecialChars verifies that special shell characters trigger
// quoting.
func TestShellEscape_SpecialChars(t *testing.T) {
	tests := []struct {
		name string
		arg  string
	}{
		{"dollar", "$HOME"},
		{"pipe", "a|b"},
		{"redirect_out", "a>b"},
		{"redirect_in", "a<b"},
		{"backtick", "`cmd`"},
		{"ampersand", "a&b"},
		{"semicolon", "a;b"},
		{"asterisk", "*.go"},
		{"parens", "(test)"},
		{"tab", "a\tb"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := shellEscape([]string{"echo", tc.arg})
			if !strings.Contains(result, "'") {
				t.Errorf("expected quoting for %q, got %q", tc.arg, result)
			}
		})
	}
}

// TestShellEscape_EmptyString verifies that empty strings are quoted.
func TestShellEscape_EmptyString(t *testing.T) {
	result := shellEscape([]string{"cmd", ""})
	if !strings.Contains(result, "''") {
		t.Errorf("expected quoted empty string, got %q", result)
	}
}

// =============================================================================
// needsQuoting tests
// =============================================================================

// TestNeedsQuoting_SafeChars verifies that safe characters don't trigger
// quoting.
func TestNeedsQuoting_SafeChars(t *testing.T) {
	safe := []string{
		"hello",
		"test123",
		"a.b.c",
		"path/to/file",
		"my-file_name",
		"ABC",
		"123",
	}
	for _, s := range safe {
		if needsQuoting(s) {
			t.Errorf("%q should not need quoting", s)
		}
	}
}

// TestNeedsQuoting_EmptyString verifies that empty strings need quoting.
func TestNeedsQuoting_EmptyString(t *testing.T) {
	if !needsQuoting("") {
		t.Error("empty string should need quoting")
	}
}

// TestNeedsQuoting_SpecialChars verifies that all special characters trigger
// quoting.
func TestNeedsQuoting_SpecialChars(t *testing.T) {
	special := []string{
		" ",
		"\t",
		"\n",
		"'",
		"\"",
		"\\",
		"$",
		"`",
		"|",
		"&",
		";",
		"<",
		">",
		"(",
		")",
		"*",
		"?",
		"[",
		"]",
		"#",
		"~",
		"=",
		"%",
	}
	for _, s := range special {
		if !needsQuoting("a" + s + "b") {
			t.Errorf("%q should need quoting", s)
		}
	}
}

// =============================================================================
// mountToArgs tests
// =============================================================================

// TestMountToArgs_Bind verifies that a writable bind mount produces --bind flag.
func TestMountToArgs_Bind(t *testing.T) {
	exec := &BwrapExecutor{
		Config: SandboxConfig{SessionRoot: t.TempDir()},
	}
	m := MountPoint{
		Source:   "/host/path",
		Target:   "/sandbox/path",
		ReadOnly: false,
		Kind:     MountBind,
	}

	args, err := exec.mountToArgs(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d: %v", len(args), args)
	}
	if args[0] != "--bind" || args[1] != "/host/path" || args[2] != "/sandbox/path" {
		t.Errorf("unexpected args: %v", args)
	}
}

// TestMountToArgs_RoBind verifies that a read-only bind mount produces --ro-bind.
func TestMountToArgs_RoBind(t *testing.T) {
	exec := &BwrapExecutor{
		Config: SandboxConfig{SessionRoot: t.TempDir()},
	}
	m := MountPoint{
		Source:   "/usr",
		Target:   "/usr",
		ReadOnly: true,
		Kind:     MountBind,
	}

	args, err := exec.mountToArgs(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if args[0] != "--ro-bind" {
		t.Errorf("expected --ro-bind, got %q", args[0])
	}
}

// TestMountToArgs_Proc verifies that a proc mount produces --proc flag.
func TestMountToArgs_Proc(t *testing.T) {
	exec := &BwrapExecutor{
		Config: SandboxConfig{SessionRoot: t.TempDir()},
	}
	m := MountPoint{
		Target: "/proc",
		Kind:   MountProc,
	}

	args, err := exec.mountToArgs(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if args[0] != "--proc" || args[1] != "/proc" {
		t.Errorf("unexpected args: %v", args)
	}
}

// TestMountToArgs_Dev verifies that a dev mount produces --dev flag.
func TestMountToArgs_Dev(t *testing.T) {
	exec := &BwrapExecutor{
		Config: SandboxConfig{SessionRoot: t.TempDir()},
	}
	m := MountPoint{
		Target: "/dev",
		Kind:   MountDev,
	}

	args, err := exec.mountToArgs(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if args[0] != "--dev" || args[1] != "/dev" {
		t.Errorf("unexpected args: %v", args)
	}
}

// TestMountToArgs_Tmpfs verifies that a tmpfs mount produces --tmpfs flag.
func TestMountToArgs_Tmpfs(t *testing.T) {
	exec := &BwrapExecutor{
		Config: SandboxConfig{SessionRoot: t.TempDir()},
	}
	m := MountPoint{
		Target: "/tmp",
		Kind:   MountTmpfs,
	}

	args, err := exec.mountToArgs(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if args[0] != "--tmpfs" || args[1] != "/tmp" {
		t.Errorf("unexpected args: %v", args)
	}
}

// TestMountToArgs_UnknownKind verifies that an unknown mount kind returns an error.
func TestMountToArgs_UnknownKind(t *testing.T) {
	exec := &BwrapExecutor{
		Config: SandboxConfig{SessionRoot: t.TempDir()},
	}
	m := MountPoint{
		Target: "/somewhere",
		Kind:   MountKind(99),
	}

	_, err := exec.mountToArgs(m)
	if err == nil {
		t.Fatal("expected error for unknown mount kind")
	}
	if !errors.Is(err, ErrConfigInvalid) {
		t.Errorf("expected ErrConfigInvalid, got: %v", err)
	}
}

// TestMountToArgs_Verbose verifies verbose logging for all five mount kinds.
func TestMountToArgs_Verbose(t *testing.T) {
	var buf bytes.Buffer
	exec := &BwrapExecutor{
		Config: SandboxConfig{
			SessionRoot: t.TempDir(),
			Verbose:     true,
		},
		logger: &buf,
	}

	tests := []struct {
		name    string
		m       MountPoint
		wantArg string
		wantLog string
	}{
		{
			name:    "bind writable",
			m:       MountPoint{Source: "/src", Target: "/dst", Kind: MountBind, ReadOnly: false},
			wantArg: "--bind",
			wantLog: "bind /src → /dst",
		},
		{
			name:    "bind read-only",
			m:       MountPoint{Source: "/src", Target: "/dst", Kind: MountBind, ReadOnly: true},
			wantArg: "--ro-bind",
			wantLog: "ro-bind /src → /dst",
		},
		{
			name:    "proc",
			m:       MountPoint{Target: "/proc", Kind: MountProc},
			wantArg: "--proc",
			wantLog: "proc → /proc",
		},
		{
			name:    "dev",
			m:       MountPoint{Target: "/dev", Kind: MountDev},
			wantArg: "--dev",
			wantLog: "dev → /dev",
		},
		{
			name:    "tmpfs",
			m:       MountPoint{Target: "/tmp", Kind: MountTmpfs},
			wantArg: "--tmpfs",
			wantLog: "tmpfs → /tmp",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf.Reset()
			args, err := exec.mountToArgs(tc.m)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if args[0] != tc.wantArg {
				t.Errorf("expected flag %q, got %q", tc.wantArg, args[0])
			}
			if !strings.Contains(buf.String(), tc.wantLog) {
				t.Errorf("expected log %q in %q", tc.wantLog, buf.String())
			}
		})
	}
}

// =============================================================================
// Helper
// =============================================================================

// hasFlag checks whether a specific flag string is present in the args slice.
func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}
