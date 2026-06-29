package sandbox

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// =============================================================================
// _killProcessGroup tests
// =============================================================================

// TestKillProcessGroup_NonExistentPID verifies that sending SIGKILL to a
// non-existent process group returns an error.
func TestKillProcessGroup_NonExistentPID(t *testing.T) {
	// PID 999999 is extremely unlikely to exist.
	err := _killProcessGroup(999999)
	if err == nil {
		t.Fatal("expected error for non-existent PID")
	}
}

// TestKillProcessGroup_ChildProcess verifies that _killProcessGroup successfully
// kills a child process by its PID. Starts a sleep process, kills it via
// _killProcessGroup, and verifies the process was terminated by signal.
func TestKillProcessGroup_ChildProcess(t *testing.T) {
	// Start a sleep process as a child.
	cmd := exec.Command("sleep", "60")
	// Put the child in its own process group so _killProcessGroup targets only it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	pid := cmd.Process.Pid
	if pid <= 0 {
		t.Fatalf("expected positive PID, got %d", pid)
	}

	// Kill the process group.
	err := _killProcessGroup(pid)
	if err != nil {
		t.Fatalf("_killProcessGroup(%d): %v", pid, err)
	}

	// Wait for the child — it should have been killed.
	waitErr := cmd.Wait()
	if waitErr == nil {
		t.Fatal("expected child process to be killed")
	}
	// The child should have exited due to SIGKILL.
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) {
		t.Fatalf("expected ExitError, got: %v (%T)", waitErr, waitErr)
	}

	// Verify it was SIGKILL (signal 9).
	status := exitErr.Sys().(syscall.WaitStatus)
	if !status.Signaled() {
		t.Error("expected child to be signaled")
	}
	if status.Signal() != syscall.SIGKILL {
		t.Errorf("expected SIGKILL (9), got: %v", status.Signal())
	}
}

// =============================================================================
// _findBwrapBinary tests
// =============================================================================

// TestFindBwrapBinary_EnvVarSet verifies that _findBwrapBinary returns the
// path from HELIX_SANDBOX_BWRAP when it points to an existing binary.
func TestFindBwrapBinary_EnvVarSet(t *testing.T) {
	// Use /bin/true which is guaranteed to exist on Linux.
	t.Setenv("HELIX_SANDBOX_BWRAP", "/bin/true")
	result := _findBwrapBinary()
	if result != "/bin/true" {
		t.Errorf("expected /bin/true, got %q", result)
	}
}

// TestFindBwrapBinary_EnvVarSetNonExistent verifies that when the env var
// points to a non-existent path, it falls through to other candidates.
func TestFindBwrapBinary_EnvVarSetNonExistent(t *testing.T) {
	t.Setenv("HELIX_SANDBOX_BWRAP", "/nonexistent/path/to/bwrap")
	// BwrapBinary is usually "bwrap" which won't be found via pathExists("bwrap")
	// unless it's in the PATH... actually pathExists doesn't use PATH, it uses
	// os.Stat directly. So this will return "" unless /usr/bin/bwrap exists.
	result := _findBwrapBinary()
	// Just verify it's not the env var path.
	if result == "/nonexistent/path/to/bwrap" {
		t.Error("should not return nonexistent env var path")
	}
}

// TestFindBwrapBinary_AllCandidatesEmpty verifies the fallback behavior when
// no candidates match.
func TestFindBwrapBinary_AllCandidatesEmpty(t *testing.T) {
	// Unset HELIX_SANDBOX_BWRAP and rely on BwrapBinary + common paths.
	_ = os.Unsetenv("HELIX_SANDBOX_BWRAP")
	result := _findBwrapBinary()
	// We don't know if bwrap is installed — just verify it doesn't panic.
	// If bwrap IS installed at a common path, result will be that path.
	// If not, result will be empty.
	if result != "" {
		// If it found something, it must be an existing path.
		if !pathExists(result) {
			t.Errorf("returned non-existent path: %q", result)
		}
	}
}

// =============================================================================
// _ensureBwrapAvailable tests
// =============================================================================

// TestEnsureBwrapAvailable_ExistingExecutable verifies that a real executable
// passes the check.
func TestEnsureBwrapAvailable_ExistingExecutable(t *testing.T) {
	err := _ensureBwrapAvailable("/bin/true")
	if err != nil {
		t.Errorf("expected nil for /bin/true, got: %v", err)
	}
}

// TestEnsureBwrapAvailable_NotFound verifies the error path when the binary
// doesn't exist.
func TestEnsureBwrapAvailable_NotFound(t *testing.T) {
	err := _ensureBwrapAvailable("/nonexistent/bwrap_path")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
	if !errors.Is(err, ErrBwrapNotFound) {
		t.Errorf("expected ErrBwrapNotFound, got: %v", err)
	}
	if !strings.Contains(err.Error(), "/nonexistent/bwrap_path") {
		t.Errorf("error message should contain path: %v", err)
	}
}

// TestEnsureBwrapAvailable_IsDirectory verifies the error when the path is a
// directory instead of a file.
func TestEnsureBwrapAvailable_IsDirectory(t *testing.T) {
	err := _ensureBwrapAvailable(t.TempDir())
	if err == nil {
		t.Fatal("expected error for directory path")
	}
	if !errors.Is(err, ErrBwrapNotFound) {
		t.Errorf("expected ErrBwrapNotFound, got: %v", err)
	}
	if !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("error message should mention directory: %v", err)
	}
}

// TestEnsureBwrapAvailable_NotExecutable verifies the error when the path is a
// regular file without execute permission.
func TestEnsureBwrapAvailable_NotExecutable(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "not-exec")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	f.Close()
	// Ensure no execute bits.
	if err := os.Chmod(f.Name(), 0o600); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	err = _ensureBwrapAvailable(f.Name())
	if err == nil {
		t.Fatal("expected error for non-executable file")
	}
	if !errors.Is(err, ErrBwrapNotFound) {
		t.Errorf("expected ErrBwrapNotFound, got: %v", err)
	}
	if !strings.Contains(err.Error(), "is not executable") {
		t.Errorf("error message should mention not executable: %v", err)
	}
}

// =============================================================================
// _execContext tests
// =============================================================================

// captureStdout runs fn with stdout redirected to a buffer and returns the output.
func captureStdout(fn func()) string {
	r, w, _ := os.Pipe()
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		done <- buf.String()
	}()

	fn()
	w.Close()
	return <-done
}

// TestExecContext_Success verifies that _execContext succeeds with /bin/true.
func TestExecContext_Success(t *testing.T) {
	ctx := context.Background()
	err := _execContext(ctx, "true", "")
	if err != nil {
		t.Errorf("expected nil for /bin/true, got: %v", err)
	}
}

// TestExecContext_CommandFailed verifies that _execContext returns an error
// for a command that exits non-zero (e.g., /bin/false or `false`).
func TestExecContext_CommandFailed(t *testing.T) {
	ctx := context.Background()

	// Use `false` (shell builtin or /bin/false) which exits 1.
	// _execContext calls exec.CommandContext which uses os/exec.LookPath.
	err := _execContext(ctx, "false", "")
	if err == nil {
		t.Fatal("expected error for exit code 1")
	}
	if errors.Is(err, ErrTimeoutExceeded) {
		t.Errorf("unexpected timeout error: %v", err)
	}
	if !errors.Is(err, ErrExecutionFailed) {
		t.Errorf("expected ErrExecutionFailed, got: %v (%T)", err, err)
	}
}

// TestExecContext_CommandNotFound verifies that _execContext returns an error
// for a non-existent command.
func TestExecContext_CommandNotFound(t *testing.T) {
	ctx := context.Background()
	err := _execContext(ctx, "nonexistent-command-xyz-12345", "")
	if err == nil {
		t.Fatal("expected error for nonexistent command")
	}
	if errors.Is(err, ErrTimeoutExceeded) {
		t.Errorf("unexpected timeout error: %v", err)
	}
	if !errors.Is(err, ErrExecutionFailed) {
		t.Errorf("expected ErrExecutionFailed, got: %v (%T)", err, err)
	}
}

// TestExecContext_TimeoutExceeded verifies that _execContext returns
// ErrTimeoutExceeded when the context deadline expires.
func TestExecContext_TimeoutExceeded(t *testing.T) {
	// Create a context that's already expired.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	// Use `sleep 1` — the context is already expired so Start/Wait
	// behavior depends on whether exec.CommandContext rejects the deadline
	// immediately.
	err := _execContext(ctx, "sleep", "10")
	if err == nil {
		t.Fatal("expected error for expired context")
	}

	// Could be ErrTimeoutExceeded (if Wait detects deadline exceeded)
	// or ErrExecutionFailed (if CommandContext fails before start, which
	// certain Go versions do for past deadlines).
	if !errors.Is(err, ErrTimeoutExceeded) && !errors.Is(err, ErrExecutionFailed) {
		t.Errorf("expected ErrTimeoutExceeded or ErrExecutionFailed, got: %v (%T)", err, err)
	}
}

// =============================================================================
// _joinPath tests
// =============================================================================

// TestJoinPath_Single verifies that _joinPath with a single element returns
// it unchanged (unless it's absolute, which it joins normally).
func TestJoinPath_Single(t *testing.T) {
	result := _joinPath("foo")
	if result != "foo" {
		t.Errorf("expected 'foo', got %q", result)
	}
}

// TestJoinPath_Multiple verifies that _joinPath joins multiple path elements.
func TestJoinPath_Multiple(t *testing.T) {
	tests := []struct {
		name     string
		elements []string
		want     string
	}{
		{"two parts", []string{"foo", "bar"}, "foo/bar"},
		{"three parts", []string{"a", "b", "c"}, "a/b/c"},
		{"with dot", []string{".", "config"}, "config"},
		{"with slash", []string{"/tmp", "sandbox"}, "/tmp/sandbox"},
		{"with trailing slash", []string{"/tmp/", "sandbox"}, "/tmp/sandbox"},
		{"empty elements", []string{"", ""}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := _joinPath(tc.elements...)
			expected := filepath.Join(tc.elements...)
			if result != expected {
				t.Errorf("_joinPath = %q, filepath.Join = %q", result, expected)
			}
			if result != tc.want {
				t.Errorf("_joinPath = %q, want %q", result, tc.want)
			}
		})
	}
}

// =============================================================================
// SetupSessionDir error paths
// =============================================================================

// TestSetupSessionDir_Verbose verifies that verbose logging is emitted when
// Config.Verbose is true.
func TestSetupSessionDir_Verbose(t *testing.T) {
	var logBuf bytes.Buffer
	cfg := SandboxConfig{
		SessionID:   "verbose-session",
		Isolation:   IsolationWorkspace,
		Workdir:     "/workspace",
		SessionRoot: t.TempDir(),
		BwrapPath:   "/usr/bin/bwrap",
		Verbose:     true,
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	exec.logger = &logBuf

	if err := exec.SetupSessionDir(); err != nil {
		t.Fatalf("SetupSessionDir: %v", err)
	}

	logStr := logBuf.String()
	if !strings.Contains(logStr, "[sandbox] creating directory") {
		t.Errorf("expected verbose log, got: %q", logStr)
	}
}

// TestSetupSessionDir_UnwritablePath verifies that SetupSessionDir returns an
// error when MkdirAll fails (e.g., a file exists where a directory is needed).
func TestSetupSessionDir_UnwritablePath(t *testing.T) {
	root := t.TempDir()
	sessionID := "blocked-session"

	// Create a FILE where the session directory should be.
	// SessionDir = SessionRoot/sessionID
	sessionDir := filepath.Join(root, sessionID)
	if err := os.WriteFile(sessionDir, []byte("block"), 0o600); err != nil {
		t.Fatalf("create blocking file: %v", err)
	}

	cfg := SandboxConfig{
		SessionID:   sessionID,
		Isolation:   IsolationWorkspace,
		Workdir:     "/workspace",
		SessionRoot: root,
		BwrapPath:   "/usr/bin/bwrap",
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	err = exec.SetupSessionDir()
	if err == nil {
		t.Fatal("expected error when MkdirAll encounters a file")
	}
	if !errors.Is(err, ErrSetupFailed) {
		t.Errorf("expected ErrSetupFailed, got: %v", err)
	}
}

// TestSetupSessionDir_VerboseErrorPath verifies verbose logging on error path.
func TestSetupSessionDir_VerboseErrorPath(t *testing.T) {
	var logBuf bytes.Buffer
	root := t.TempDir()
	sessionID := "verbose-error-session"

	// Block with a file.
	sessionDir := filepath.Join(root, sessionID)
	if err := os.WriteFile(sessionDir, []byte("block"), 0o600); err != nil {
		t.Fatalf("create blocking file: %v", err)
	}

	cfg := SandboxConfig{
		SessionID:   sessionID,
		Isolation:   IsolationWorkspace,
		Workdir:     "/workspace",
		SessionRoot: root,
		BwrapPath:   "/usr/bin/bwrap",
		Verbose:     true,
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	exec.logger = &logBuf

	err = exec.SetupSessionDir()
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrSetupFailed) {
		t.Errorf("expected ErrSetupFailed, got: %v", err)
	}
	// Verbose logs should have been emitted before the error.
	logStr := logBuf.String()
	if !strings.Contains(logStr, "[sandbox] creating directory") {
		t.Errorf("expected verbose log even on error, got: %q", logStr)
	}
}

// =============================================================================
// CleanupSessionDir verbose path
// =============================================================================

// TestCleanupSessionDir_Verbose verifies that verbose logging is emitted
// when Config.Verbose is true.
func TestCleanupSessionDir_Verbose(t *testing.T) {
	var logBuf bytes.Buffer
	cfg := SandboxConfig{
		SessionID:   "verbose-cleanup",
		Isolation:   IsolationWorkspace,
		Workdir:     "/workspace",
		SessionRoot: t.TempDir(),
		BwrapPath:   "/usr/bin/bwrap",
		Verbose:     true,
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	exec.logger = &logBuf

	// Setup so there's something to clean.
	if err := exec.SetupSessionDir(); err != nil {
		t.Fatalf("SetupSessionDir: %v", err)
	}

	logBuf.Reset()
	if err := exec.CleanupSessionDir(); err != nil {
		t.Fatalf("CleanupSessionDir: %v", err)
	}

	logStr := logBuf.String()
	if !strings.Contains(logStr, "[sandbox] removing session dir") {
		t.Errorf("expected verbose cleanup log, got: %q", logStr)
	}
}

// =============================================================================
// captureStdout helper (used by _execContext tests above)
// =============================================================================
var _ = captureStdout // suppress unused warning; helper available for future tests

// =============================================================================
// BwrapCommand error path (nil spec with non-None isolation)
// =============================================================================

// TestBwrapCommand_ErrorPath verifies that BwrapCommand propagates errors
// from BwrapArgs (nil spec with workspace isolation).
func TestBwrapCommand_ErrorPath(t *testing.T) {
	exec := &BwrapExecutor{
		Config: SandboxConfig{
			Isolation:   IsolationWorkspace,
			SessionRoot: t.TempDir(),
			Workdir:     "/workspace",
		},
		Spec: nil,
	}

	_, err := exec.BwrapCommand()
	if err == nil {
		t.Fatal("expected error for nil spec")
	}
	if !errors.Is(err, ErrConfigInvalid) {
		t.Errorf("expected ErrConfigInvalid, got: %v", err)
	}
}

// =============================================================================
// Run error path tests — covering remaining branches in Run()
// =============================================================================

// TestRun_BwrapNotFound verifies that Run returns ErrBwrapNotFound when the
// configured BwrapPath does not exist on the filesystem.
func TestRun_BwrapNotFound(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:   "test-bwrap-missing",
		Isolation:   IsolationWorkspace,
		Workdir:     "/workspace",
		SessionRoot: t.TempDir(),
		BwrapPath:   "/nonexistent/bwrap-99999",
		Command:     []string{"true"},
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	err = exec.Run(context.TODO())
	if err == nil {
		t.Fatal("expected error when BwrapPath is missing")
	}
	if !errors.Is(err, ErrBwrapNotFound) {
		t.Errorf("expected ErrBwrapNotFound, got: %v", err)
	}
}

// TestRun_SetupSessionDirError verifies that Run propagates setup errors
// and cleans up the session directory before returning.
func TestRun_SetupSessionDirError(t *testing.T) {
	root := t.TempDir()
	sessionID := "setup-fail-session"

	// Create a FILE where the session directory should be.
	sessionDir := filepath.Join(root, sessionID)
	if err := os.WriteFile(sessionDir, []byte("block"), 0o600); err != nil {
		t.Fatalf("create blocking file: %v", err)
	}

	cfg := SandboxConfig{
		SessionID:   sessionID,
		Isolation:   IsolationWorkspace,
		Workdir:     "/workspace",
		SessionRoot: root,
		BwrapPath:   "/bin/true",
		Command:     []string{"true"},
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	err = exec.Run(context.TODO())
	if err == nil {
		t.Fatal("expected error when SetupSessionDir fails")
	}
	if !errors.Is(err, ErrSetupFailed) {
		t.Errorf("expected ErrSetupFailed, got: %v", err)
	}
}

// TestRun_CgroupSetupWarning verifies that Run logs a warning (non-fatal)
// when cgroup setup fails, then continues to the bwrap path.
func TestRun_CgroupSetupWarning(t *testing.T) {
	var logBuf bytes.Buffer
	fakeRoot := t.TempDir()

	// Create a file where the cgroup helix dir should be.
	// CgroupPath = <CgroupRoot>/helix/<SessionID>
	helixDir := filepath.Join(fakeRoot, "helix")
	if err := os.WriteFile(helixDir, []byte("block-cgroup"), 0o600); err != nil {
		t.Fatalf("create blocking cgroup file: %v", err)
	}

	cfg := SandboxConfig{
		SessionID:   "cg-warn-session",
		Isolation:   IsolationWorkspace,
		Workdir:     "/workspace",
		SessionRoot: t.TempDir(),
		CgroupRoot:  fakeRoot, // controlled cgroup root
		BwrapPath:   "/bin/true",
		Command:     []string{"true"},
		Verbose:     true,
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	exec.logger = &logBuf

	// Run should log a cgroup warning but still succeed up to ErrNotImplemented.
	err = exec.Run(context.TODO())
	if err == nil {
		t.Fatal("expected error from Run (stub)")
	}
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented, got: %v", err)
	}
	logStr := logBuf.String()
	if !strings.Contains(logStr, "cgroup setup warning") {
		t.Errorf("expected cgroup setup warning in log, got: %q", logStr)
	}
}
