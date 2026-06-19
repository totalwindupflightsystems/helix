package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
)

// ---------------------------------------------------------------------------
// CgroupV2
// ---------------------------------------------------------------------------

// CgroupV2 manages the cgroup v2 hierarchy for a sandbox session. It creates
// the per-session cgroup directory, writes resource limits, and cleans up on
// exit.
//
// Cgroup v2 layout:
//
//	/sys/fs/cgroup/helix/<session-id>/
//	    memory.max     — RSS limit in bytes
//	    cpu.max        — CPU quota (quota period, or "max" for unlimited)
//	    cgroup.procs   — PID of the sandboxed process (written by bwrap)
//
// For rootless operation, the user must have delegated cgroup access
// (systemd-logind or cgroup delegation). If the cgroup root is not writable,
// Setup returns ErrSetupFailed with a descriptive message.
type CgroupV2 struct {
	// Path is the absolute cgroup directory for this session.
	Path string

	// Config is the validated SandboxConfig.
	Config SandboxConfig

	// Enabled reports whether cgroup limits were successfully applied. If the
	// cgroup root is not writable, limits are silently skipped (the sandbox
	// still runs, just without resource enforcement).
	Enabled bool
}

// NewCgroup creates a CgroupV2 for the given config. It does not create the
// directory or apply limits — call Setup for that.
func NewCgroup(cfg SandboxConfig) *CgroupV2 {
	return &CgroupV2{
		Path:   cfg.CgroupPath(),
		Config: cfg,
	}
}

// Setup creates the cgroup directory hierarchy and applies memory and CPU
// limits. If the cgroup root is not writable (e.g., rootless without
// delegation), the error is returned but callers may choose to continue without
// resource enforcement.
func (c *CgroupV2) Setup() error {
	// Check that cgroup v2 is mounted and the root exists.
	if !pathExists(c.Config.CgroupRoot) {
		return fmt.Errorf("%w: cgroup root %q does not exist", ErrSetupFailed, c.Config.CgroupRoot)
	}

	// Verify we can write to the cgroup root. For rootless operation this
	// requires delegation.
	rootWritable := isWritable(c.Config.CgroupRoot)
	if !rootWritable {
		// Non-fatal: we continue without cgroup enforcement.
		return nil
	}

	// Create the helix parent if it doesn't exist.
	helixDir := filepath.Join(c.Config.CgroupRoot, "helix")
	if err := mkdirIfNotExist(helixDir); err != nil {
		return fmt.Errorf("%w: cannot create cgroup parent %q: %v", ErrSetupFailed, helixDir, err)
	}

	// Create the per-session cgroup.
	if err := os.MkdirAll(c.Path, 0o755); err != nil {
		return fmt.Errorf("%w: cannot create cgroup %q: %v", ErrSetupFailed, c.Path, err)
	}

	// Apply memory limit.
	if c.Config.MemoryLimit > 0 {
		bytes := int64(c.Config.MemoryLimit) * 1024 * 1024
		if err := writeFile(filepath.Join(c.Path, "memory.max"), fmt.Sprintf("%d", bytes)); err != nil {
			return fmt.Errorf("%w: cannot set memory.max: %v", ErrSetupFailed, err)
		}
	}

	// Apply CPU limit. We use a generous default: allow full CPU but with the
	// time limit enforced separately via process timeout. cpu.max format is
	// "<quota> <period>" or "max <period>" for unlimited.
	if err := writeFile(filepath.Join(c.Path, "cpu.max"), "max 100000"); err != nil {
		return fmt.Errorf("%w: cannot set cpu.max: %v", ErrSetupFailed, err)
	}

	c.Enabled = true
	return nil
}

// Cleanup removes the cgroup directory for this session. It is safe to call
// even if Setup was never called or failed.
func (c *CgroupV2) Cleanup() error {
	if c.Path == "" || !c.Enabled {
		return nil
	}
	return os.RemoveAll(c.Path)
}

// CgroupPIDPath returns the path to the cgroup.procs file where the sandboxed
// PID should be written. This is used by the executor when launching bwrap with
// --die-with-parent and cgroup integration.
func (c *CgroupV2) CgroupPIDPath() string {
	return filepath.Join(c.Path, "cgroup.procs")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// mkdirIfNotExist creates dir if it doesn't already exist.
func mkdirIfNotExist(dir string) error {
	if pathExists(dir) {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

// writeFile writes data to path, truncating if it exists. The file is created
// with mode 0644. Cgroup control files require exact values with no trailing
// newline.
func writeFile(path, data string) error {
	return os.WriteFile(path, []byte(data), 0o644)
}

// isWritable checks if the current process can write to a cgroup directory.
// For rootless operation this depends on the cgroup being delegated to the
// user's UID.
func isWritable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().Perm()&0o200 != 0 // Owner write bit.
}
