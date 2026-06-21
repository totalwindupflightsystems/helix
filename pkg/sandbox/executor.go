package sandbox

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// ---------------------------------------------------------------------------
// BwrapExecutor
// ---------------------------------------------------------------------------

// BwrapExecutor builds the bubblewrap command line from a SandboxConfig and
// MountSpec, then executes it (or prints it for dry-run). The actual bwrap
// invocation is stubbed via ErrNotImplemented for the real-execution path in
// this deliverable; however, dry-run mode, command construction, session
// directory setup, and cgroup wiring are fully implemented.
type BwrapExecutor struct {
	Config SandboxConfig
	Spec   *MountSpec

	// stdout and stderr are where the child process's output goes. They default
	// to os.Stdout and os.Stderr.
	stdout io.Writer
	stderr io.Writer

	// logger receives verbose mount-operation messages when Config.Verbose is
	// true.
	logger io.Writer
}

// NewExecutor creates a BwrapExecutor for the given config. It builds the
// mount spec internally. Call Validate on the config before constructing.
func NewExecutor(cfg SandboxConfig) (*BwrapExecutor, error) {
	spec, err := BuildMountSpec(cfg)
	if err != nil {
		return nil, err
	}

	return &BwrapExecutor{
		Config: cfg,
		Spec:   spec,
		stdout: os.Stdout,
		stderr: os.Stderr,
		logger: os.Stderr,
	}, nil
}

// SetOutput overrides the stdout/stderr writers. Primarily for testing.
func (e *BwrapExecutor) SetOutput(stdout, stderr io.Writer) {
	if stdout != nil {
		e.stdout = stdout
	}
	if stderr != nil {
		e.stderr = stderr
	}
}

// ---------------------------------------------------------------------------
// Session directory management
// ---------------------------------------------------------------------------

// SetupSessionDir creates the per-session workspace directories on the host.
// The directory tree is:
//
//	<SessionRoot>/<SessionID>/
//	    workspace/    — bind-mounted at Config.Workdir inside the sandbox
//	    tmp/          — bind-mounted at /tmp inside the sandbox
func (e *BwrapExecutor) SetupSessionDir() error {
	dirs := []string{
		e.Config.SessionDir(),
		e.Config.WorkspaceDir(),
		e.Config.TmpDir(),
	}

	for _, d := range dirs {
		if e.Config.Verbose {
			fmt.Fprintf(e.logger, "[sandbox] creating directory %s\n", d)
		}
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("%w: cannot create session directory %q: %v",
				ErrSetupFailed, d, err)
		}
	}

	return nil
}

// CleanupSessionDir removes the entire session directory tree. Safe to call
// even if SetupSessionDir was never called.
func (e *BwrapExecutor) CleanupSessionDir() error {
	if e.Config.Verbose {
		fmt.Fprintf(e.logger, "[sandbox] removing session dir %s\n", e.Config.SessionDir())
	}
	return os.RemoveAll(e.Config.SessionDir())
}

// ---------------------------------------------------------------------------
// Command construction
// ---------------------------------------------------------------------------

// BwrapArgs builds the complete bwrap argument list for this executor's config
// and mount spec. For IsolationNone, it returns nil (no bwrap needed).
func (e *BwrapExecutor) BwrapArgs() ([]string, error) {
	if e.Config.Isolation == IsolationNone {
		return nil, nil
	}

	if e.Spec == nil {
		return nil, fmt.Errorf("%w: mount spec is nil", ErrConfigInvalid)
	}

	var args []string

	// Mount points.
	for _, m := range e.Spec.Mounts {
		mountArgs, err := e.mountToArgs(m)
		if err != nil {
			return nil, err
		}
		args = append(args, mountArgs...)
	}

	// Namespace flags.
	if e.Spec.UnshareNet {
		args = append(args, "--unshare-net")
		if e.Config.Verbose {
			fmt.Fprintln(e.logger, "[sandbox] unsharing network namespace")
		}
	}

	if e.Spec.UnsharePID {
		args = append(args, "--unshare-pid")
		if e.Config.Verbose {
			fmt.Fprintln(e.logger, "[sandbox] unsharing PID namespace")
		}
	}

	if e.Spec.DieWithParent {
		args = append(args, "--die-with-parent")
	}

	// Environment control.
	if e.Spec.UnsetEnv {
		args = append(args, "--clearenv")
	}
	for k, v := range e.Spec.SetEnv {
		args = append(args, "--setenv", k, v)
	}

	// Working directory inside the sandbox.
	args = append(args, "--chdir", e.Config.Workdir)

	// The command separator and the command itself.
	args = append(args, "--")
	args = append(args, e.Config.Command...)

	return args, nil
}

// mountToArgs converts a single MountPoint to its bwrap flag arguments.
func (e *BwrapExecutor) mountToArgs(m MountPoint) ([]string, error) {
	switch m.Kind {
	case MountBind:
		if m.ReadOnly {
			if e.Config.Verbose {
				fmt.Fprintf(e.logger, "[sandbox] ro-bind %s → %s\n", m.Source, m.Target)
			}
			return []string{"--ro-bind", m.Source, m.Target}, nil
		}
		if e.Config.Verbose {
			fmt.Fprintf(e.logger, "[sandbox] bind %s → %s\n", m.Source, m.Target)
		}
		return []string{"--bind", m.Source, m.Target}, nil

	case MountProc:
		if e.Config.Verbose {
			fmt.Fprintf(e.logger, "[sandbox] proc → %s\n", m.Target)
		}
		return []string{"--proc", m.Target}, nil

	case MountDev:
		if e.Config.Verbose {
			fmt.Fprintf(e.logger, "[sandbox] dev → %s\n", m.Target)
		}
		return []string{"--dev", m.Target}, nil

	case MountTmpfs:
		if e.Config.Verbose {
			fmt.Fprintf(e.logger, "[sandbox] tmpfs → %s\n", m.Target)
		}
		return []string{"--tmpfs", m.Target}, nil

	default:
		return nil, fmt.Errorf("%w: unknown mount kind %d", ErrConfigInvalid, m.Kind)
	}
}

// BwrapCommand returns the full command line as a single shell-escaped string.
// This is what dry-run prints to stdout.
func (e *BwrapExecutor) BwrapCommand() (string, error) {
	args, err := e.BwrapArgs()
	if err != nil {
		return "", err
	}
	if args == nil {
		// IsolationNone — just the raw command.
		return shellEscape(append([]string{}, e.Config.Command...)), nil
	}

	full := append([]string{e.Config.BwrapPath}, args...)
	return shellEscape(full), nil
}

// ---------------------------------------------------------------------------
// Dry-run
// ---------------------------------------------------------------------------

// DryRun prints the exact bwrap command line to the executor's stdout without
// executing anything. It also prints a structured summary of the session
// configuration to stderr.
func (e *BwrapExecutor) DryRun() error {
	cmd, err := e.BwrapCommand()
	if err != nil {
		return err
	}

	// Print session summary to stderr.
	fmt.Fprintf(e.stderr, "=== Helix Sandbox Dry Run ===\n")
	fmt.Fprintf(e.stderr, "Session ID:     %s\n", e.Config.SessionID)
	fmt.Fprintf(e.stderr, "Isolation:      %s\n", e.Config.Isolation)
	fmt.Fprintf(e.stderr, "Workdir:        %s\n", e.Config.Workdir)
	fmt.Fprintf(e.stderr, "Time limit:     %ds\n", e.Config.TimeLimit)
	fmt.Fprintf(e.stderr, "Memory limit:   %dMB\n", e.Config.MemoryLimit)
	fmt.Fprintf(e.stderr, "Network:        %s\n", e.Config.Network)
	fmt.Fprintf(e.stderr, "Session dir:    %s\n", e.Config.SessionDir())
	fmt.Fprintf(e.stderr, "Workspace:      %s\n", e.Config.WorkspaceDir())
	fmt.Fprintf(e.stderr, "Cgroup path:    %s\n", e.Config.CgroupPath())
	fmt.Fprintf(e.stderr, "Bwrap binary:   %s\n", e.Config.BwrapPath)
	fmt.Fprintf(e.stderr, "-----------------------------\n")

	// Print the actual command to stdout.
	fmt.Fprintln(e.stdout, cmd)

	return nil
}

// ---------------------------------------------------------------------------
// Execution (STUBBED)
// ---------------------------------------------------------------------------

// Run executes the sandbox command. In this deliverable, the actual bwrap
// invocation is stubbed: it returns ErrNotImplemented after performing all
// setup steps (session dir creation, cgroup wiring, dry-run output).
//
// The full implementation would:
//  1. Create session directories (SetupSessionDir)
//  2. Set up cgroup v2 limits (CgroupV2.Setup)
//  3. Build the bwrap command (BwrapArgs)
//  4. Start the process with a context timeout
//  5. Forward stdin/stdout/stderr
//  6. Wait for completion, enforcing the time limit
//  7. Clean up session dir and cgroup
//
// Returns ErrNotImplemented in all cases.
func (e *BwrapExecutor) Run(ctx context.Context) error {
	if e.Config.DryRun {
		return e.DryRun()
	}

	// Perform real setup so the dry-run path and the stub path exercise the
	// same code.
	if err := e.SetupSessionDir(); err != nil {
		_ = e.CleanupSessionDir()
		return err
	}

	cg := NewCgroup(e.Config)
	if err := cg.Setup(); err != nil {
		if e.Config.Verbose {
			fmt.Fprintf(e.logger, "[sandbox] cgroup setup warning: %v\n", err)
		}
		// Non-fatal — continue without cgroup enforcement.
	}
	defer func() { _ = cg.Cleanup() }()

	// Verify bwrap exists.
	if !pathExists(e.Config.BwrapPath) {
		_ = e.CleanupSessionDir()
		return fmt.Errorf("%w: %s", ErrBwrapNotFound, e.Config.BwrapPath)
	}

	// --- STUB: real execution is not implemented in this deliverable ---
	_ = e.CleanupSessionDir()
	return fmt.Errorf("%w: bwrap execution", ErrNotImplemented)
}

// RunWithTimeout is a convenience wrapper that applies the configured time
// limit as a context deadline before calling Run.
func (e *BwrapExecutor) RunWithTimeout() error {
	ctx := context.Background()
	if e.Config.TimeLimit > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(e.Config.TimeLimit)*time.Second)
		defer cancel()
	}
	return e.Run(ctx)
}

// ---------------------------------------------------------------------------
// Shell escaping
// ---------------------------------------------------------------------------

// shellEscape joins a command and its arguments into a single shell-escaped
// string, quoting any element that contains whitespace or shell metacharacters.
func shellEscape(cmd []string) string {
	parts := make([]string, len(cmd))
	for i, arg := range cmd {
		if needsQuoting(arg) {
			parts[i] = "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
		} else {
			parts[i] = arg
		}
	}
	return strings.Join(parts, " ")
}

// needsQuoting reports whether arg requires shell quoting.
func needsQuoting(arg string) bool {
	if arg == "" {
		return true
	}
	for _, r := range arg {
		switch r {
		case ' ', '\t', '\n', '\'', '"', '\\', '$', '`', '|', '&', ';',
			'<', '>', '(', ')', '*', '?', '[', ']', '#', '~', '=', '%':
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Unused but referenced for future implementation
// ---------------------------------------------------------------------------

// killProcessGroup sends SIGKILL to an entire process group. This is the
// timeout-enforcement mechanism for the full implementation.
func _killProcessGroup(pid int) error {
	// Negative PID kills the entire process group.
	return syscall.Kill(-pid, syscall.SIGKILL)
}

// findBwrapBinary searches for the bubblewrap binary at common locations.
func _findBwrapBinary() string {
	candidates := []string{
		os.Getenv("HELIX_SANDBOX_BWRAP"),
		BwrapBinary,
		"/usr/local/bin/bwrap",
		"/usr/bin/bwrap",
	}
	for _, c := range candidates {
		if c != "" && pathExists(c) {
			return c
		}
	}
	return ""
}

// ensureBwrapAvailable checks that the bwrap binary exists and is executable.
func _ensureBwrapAvailable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%w: %s (%v)", ErrBwrapNotFound, path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%w: %s is a directory", ErrBwrapNotFound, path)
	}
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("%w: %s is not executable", ErrBwrapNotFound, path)
	}
	return nil
}

// execContext is the real execution function (unexported, for future wiring).
// It starts the command, waits with the context, and handles timeout.
func _execContext(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Put the child in its own process group for timeout kill.
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%w: %v", ErrExecutionFailed, err)
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			_ = _killProcessGroup(cmd.Process.Pid)
			return ErrTimeoutExceeded
		}
		return fmt.Errorf("%w: %v", ErrExecutionFailed, err)
	}

	return nil
}

// joinPath is a convenience to avoid importing filepath in multiple files.
func _joinPath(elem ...string) string {
	return filepath.Join(elem...)
}
