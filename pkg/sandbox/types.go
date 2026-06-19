// Package sandbox provides Bubblewrap-based process isolation for the Helix platform.
//
// This package implements the sandbox primitive that every Helix platform action
// flows through — agent spawns, code execution, and tool invocation. It wraps
// bubblewrap (bwrap) with configurable isolation levels, cgroup v2 resource limits,
// and a structured error taxonomy.
//
// Design goals:
//   - Zero daemon: every invocation is a fresh process.
//   - Zero config: all parameters come from CLI flags or defaults.
//   - Zero images: uses the host filesystem with bind mounts.
//   - Stdlib only: no external Go dependencies.
package sandbox

import "errors"

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

// BwrapBinary is the default path to the bubblewrap binary. It can be overridden
// via the HELIX_SANDBOX_BWRAP environment variable.
const BwrapBinary = "/usr/bin/bwrap"

// DefaultSessionRoot is the base directory under which per-session workspaces
// are created. Each session gets /tmp/helix-sandbox/<session-id>/.
const DefaultSessionRoot = "/tmp/helix-sandbox"

// BwrapVersion is the minimum supported bubblewrap version. The implementation
// relies on flags available since this version.
const BwrapVersion = "0.11.1"

// CgroupV2MountPoint is where cgroups v2 is expected to be mounted.
const CgroupV2MountPoint = "/sys/fs/cgroup"

// ---------------------------------------------------------------------------
// IsolationLevel
// ---------------------------------------------------------------------------

// IsolationLevel controls the degree of sandboxing applied to the child process.
type IsolationLevel string

const (
	// IsolationNone runs the command directly on the host with no sandboxing.
	// This is for debugging only and must never be used for untrusted code.
	IsolationNone IsolationLevel = "none"

	// IsolationWorkspace provides filesystem isolation with a private workspace
	// directory, no network access, and a private PID namespace. GPU
	// pass-through is allowed by default. This is the standard level for agent
	// code execution.
	IsolationWorkspace IsolationLevel = "workspace"

	// IsolationFull provides the strictest isolation: all workspace restrictions
	// plus no GPU access, a more restricted /proc, and tighter cgroup limits.
	// This is for untrusted code that must not reach any host resource.
	IsolationFull IsolationLevel = "full"
)

// ValidIsolationLevels returns the set of isolation levels accepted by the CLI.
func ValidIsolationLevels() []IsolationLevel {
	return []IsolationLevel{IsolationNone, IsolationWorkspace, IsolationFull}
}

// IsValid reports whether level is a recognised isolation level string.
func (l IsolationLevel) IsValid() bool {
	switch l {
	case IsolationNone, IsolationWorkspace, IsolationFull:
		return true
	default:
		return false
	}
}

// HasNetwork reports whether the isolation level permits network access.
// Only IsolationNone allows network; workspace and full are fully isolated.
func (l IsolationLevel) HasNetwork() bool {
	return l == IsolationNone
}

// HasGPU is removed — Helix agents use API inference, not local GPU.
// GPU pass-through is never needed.

// HasPIDNamespace reports whether the isolation level runs in a private PID namespace.
func (l IsolationLevel) HasPIDNamespace() bool {
	return l == IsolationWorkspace || l == IsolationFull
}

// String implements fmt.Stringer for IsolationLevel.
func (l IsolationLevel) String() string {
	return string(l)
}

// ---------------------------------------------------------------------------
// Error taxonomy
// ---------------------------------------------------------------------------

// ErrConfigInvalid is returned when the sandbox configuration fails validation —
// for example, an unrecognised isolation level, a non-positive time limit, or a
// missing session ID.
//
// Usage:
//
//	if cfg.Isolation == "bad" {
//	    return sandbox.ErrConfigInvalid
//	}
var ErrConfigInvalid = errors.New("sandbox: configuration is invalid")

// ErrSetupFailed is returned when a required resource cannot be created or
// prepared — typically the per-session workspace directory or the cgroup
// hierarchy.
var ErrSetupFailed = errors.New("sandbox: setup failed")

// ErrExecutionFailed is returned when the bubblewrap binary (or, in isolation
// level "none", the raw command) exits with a non-zero status or cannot be
// started.
var ErrExecutionFailed = errors.New("sandbox: execution failed")

// ErrTimeoutExceeded is returned when the child process runs longer than the
// configured time limit. The process is sent SIGKILL before this error is
// returned.
var ErrTimeoutExceeded = errors.New("sandbox: time limit exceeded")

// ErrBwrapNotFound is returned when the bubblewrap binary cannot be located at
// the expected path or is not executable.
var ErrBwrapNotFound = errors.New("sandbox: bubblewrap binary not found")

// ErrNotImplemented is returned by stub functions that have not yet been wired
// to the real system call. The actual bwrap invocation is intentionally stubbed
// in this deliverable.
var ErrNotImplemented = errors.New("sandbox: not implemented")

// ---------------------------------------------------------------------------
// Exit codes
// ---------------------------------------------------------------------------

// Exit codes used by the CLI for machine-readable error reporting.
const (
	ExitOK             = 0
	ExitConfigError    = 2
	ExitSetupError     = 3
	ExitBwrapNotFound  = 4
	ExitExecutionError = 5
	ExitTimeout        = 6
	ExitInternalError  = 70 // EX_SOFTWARE
)
