package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// SandboxConfig
// ---------------------------------------------------------------------------

// SandboxConfig holds every parameter that controls sandbox execution. It is
// populated from CLI flags (see cmd/sandbox/main.go) or programmatically via
// DefaultConfig followed by field assignment.
type SandboxConfig struct {
	// SessionID uniquely identifies this sandbox session. It is used to name
	// the per-session workspace directory and the cgroup hierarchy. If left
	// empty, DefaultConfig generates a random UUID-style ID.
	SessionID string

	// Isolation controls the sandboxing level (none, workspace, full).
	Isolation IsolationLevel

	// Workdir is the path inside the sandbox that the command runs in. For
	// levels workspace/full this is the bind-mounted workspace directory.
	Workdir string

	// TimeLimit is the maximum wall-clock execution time in seconds. A value
	// of 0 means no limit. The child process is sent SIGKILL on expiry.
	TimeLimit int

	// MemoryLimit is the maximum resident set size in megabytes enforced via
	// cgroup v2 memory.max. A value of 0 means no limit.
	MemoryLimit int

	// Network controls whether the sandbox has network access.
	// "none" = fully isolated (--unshare-net), "restricted" = limited
	// (future: filtered namespace). Only IsolationNone honours unrestricted
	// network access; workspace and full always unshare the network namespace.
	Network NetworkMode

	// GPU support is intentionally removed — Helix agents run inference
	// via API, not local hardware. No GPU pass-through needed.

	// DryRun, when true, causes the executor to print the exact bwrap command
	// line to stdout without invoking bwrap. Essential for auditing and testing.
	DryRun bool

	// Verbose enables detailed logging of every mount operation and bwrap
	// argument to stderr.
	Verbose bool

	// Command is the command to execute inside the sandbox. It is the argv
	// following the "--" separator on the CLI.
	Command []string

	// SessionRoot is the base directory for session workspaces. Defaults to
	// /tmp/helix-sandbox. Override via HELIX_SANDBOX_SESSION_ROOT.
	SessionRoot string

	// BwrapPath overrides the bubblewrap binary location. Defaults to
	// /usr/bin/bwrap. Override via HELIX_SANDBOX_BWRAP.
	BwrapPath string

	// CgroupRoot overrides the cgroup v2 mount point. Defaults to
	// /sys/fs/cgroup. Override via HELIX_SANDBOX_CGROUP_ROOT.
	CgroupRoot string
}

// NetworkMode controls network access inside the sandbox.
type NetworkMode string

const (
	// NetworkNone fully isolates the network namespace (--unshare-net).
	NetworkNone NetworkMode = "none"

	// NetworkRestricted is reserved for future filtered-network support.
	// Currently behaves identically to NetworkNone but is accepted by the CLI.
	NetworkRestricted NetworkMode = "restricted"
)

// IsValid reports whether mode is a recognised network mode.
func (m NetworkMode) IsValid() bool {
	switch m {
	case NetworkNone, NetworkRestricted:
		return true
	default:
		return false
	}
}

// String implements fmt.Stringer.
func (m NetworkMode) String() string {
	return string(m)
}

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

// DefaultConfig returns a SandboxConfig populated with safe defaults. Callers
// should override SessionID and Command as needed.
func DefaultConfig() SandboxConfig {
	return SandboxConfig{
		SessionID:   "",
		Isolation:   IsolationWorkspace,
		Workdir:     "/workspace",
		TimeLimit:   600,
		MemoryLimit: 2048,
		Network:     NetworkNone,
	
		DryRun:      false,
		Verbose:     false,
		Command:     nil,
		SessionRoot: envOr("HELIX_SANDBOX_SESSION_ROOT", DefaultSessionRoot),
		BwrapPath:   envOr("HELIX_SANDBOX_BWRAP", BwrapBinary),
		CgroupRoot:  envOr("HELIX_SANDBOX_CGROUP_ROOT", CgroupV2MountPoint),
	}
}

// envOr returns the value of the environment variable key, or fallback if the
// variable is unset or empty.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ---------------------------------------------------------------------------
// Derived paths
// ---------------------------------------------------------------------------

// SessionDir returns the absolute path to this session's base directory:
//
//	<SessionRoot>/<SessionID>/
func (c SandboxConfig) SessionDir() string {
	return filepath.Join(c.SessionRoot, c.SessionID)
}

// WorkspaceDir returns the absolute path to the agent's workspace, which is
// bind-mounted at Workdir inside the sandbox:
//
//	<SessionRoot>/<SessionID>/workspace/
func (c SandboxConfig) WorkspaceDir() string {
	return filepath.Join(c.SessionDir(), "workspace")
}

// TmpDir returns the absolute path to the agent's private /tmp inside the
// sandbox:
//
//	<SessionRoot>/<SessionID>/tmp/
func (c SandboxConfig) TmpDir() string {
	return filepath.Join(c.SessionDir(), "tmp")
}

// CgroupPath returns the cgroup v2 path for this session:
//
//	<CgroupRoot>/helix/<SessionID>/
func (c SandboxConfig) CgroupPath() string {
	return filepath.Join(c.CgroupRoot, "helix", c.SessionID)
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

// Validate checks the configuration for correctness and returns a non-nil error
// wrapping ErrConfigInvalid if any constraint is violated.
func (c SandboxConfig) Validate() error {
	var problems []string

	if c.SessionID == "" {
		problems = append(problems, "session ID is empty")
	}

	if !c.Isolation.IsValid() {
		problems = append(problems, fmt.Sprintf("unknown isolation level %q", c.Isolation))
	}

	if c.Workdir == "" {
		problems = append(problems, "workdir is empty")
	}

	if c.TimeLimit < 0 {
		problems = append(problems, fmt.Sprintf("time limit %d must be >= 0", c.TimeLimit))
	}

	if c.MemoryLimit < 0 {
		problems = append(problems, fmt.Sprintf("memory limit %d must be >= 0", c.MemoryLimit))
	}

	if !c.Network.IsValid() {
		problems = append(problems, fmt.Sprintf("unknown network mode %q", c.Network))
	}

	if len(c.Command) == 0 {
		problems = append(problems, "command is empty")
	}

	if c.SessionRoot == "" {
		problems = append(problems, "session root is empty")
	}

	if c.BwrapPath == "" {
		problems = append(problems, "bwrap path is empty")
	}

	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrConfigInvalid, strings.Join(problems, "; "))
	}

	return nil
}

// Helix agents access models via API — no local GPU required.
// EffectiveGPU() removed intentionally.

// EffectiveNetwork returns whether the sandbox will have network access,
// accounting for the isolation level. Only IsolationNone provides network.
func (c SandboxConfig) EffectiveNetwork() bool {
	return c.Isolation.HasNetwork()
}
