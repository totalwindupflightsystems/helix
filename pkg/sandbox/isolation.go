package sandbox

import (
	"fmt"
	"os"
)

// ---------------------------------------------------------------------------
// MountPoint
// ---------------------------------------------------------------------------

// MountPoint describes a single bind mount for the bubblewrap sandbox.
type MountPoint struct {
	// Source is the path on the host filesystem. For synthetic mounts
	// (--proc, --dev) this is empty.
	Source string

	// Target is the path inside the sandbox where Source is mounted.
	Target string

	// ReadOnly marks the mount as read-only (--ro-bind) vs. writable (--bind).
	ReadOnly bool

	// Kind classifies the mount so the executor can emit the correct bwrap flag.
	Kind MountKind
}

// MountKind selects the bwrap flag family for a MountPoint.
type MountKind int

const (
	// MountBind is a --bind or --ro-bind mount (real host path → sandbox).
	MountBind MountKind = iota

	// MountProc is a --proc mount (synthetic procfs).
	MountProc

	// MountDev is a --dev mount (synthetic devtmpfs with a minimal device set).
	MountDev

	// MountTmpfs is a --tmpfs mount (in-memory filesystem).
	MountTmpfs
)

// ---------------------------------------------------------------------------
// Mount specification builders
// ---------------------------------------------------------------------------

// MountSpec is the complete set of mounts and bwrap options for one isolation
// level. The executor consumes this to build the bwrap argv.
type MountSpec struct {
	// Mounts is the ordered list of bind/proc/dev/tmpfs mounts.
	Mounts []MountPoint

	// UnshareNet, when true, adds --unshare-net.
	UnshareNet bool

	// UnsharePID, when true, adds --unshare-pid.
	UnsharePID bool

	// DieWithParent, when true, adds --die-with-parent so the sandbox is
	// killed when the parent (helix-sandbox) exits.
	DieWithParent bool

	// UnsetEnv, when true, clears all inherited environment variables inside
	// the sandbox. The caller should set a minimal environment via SetEnv.
	UnsetEnv bool

	// SetEnv is a map of environment variables to set inside the sandbox.
	SetEnv map[string]string
}

// BuildMountSpec constructs the complete mount specification for the given
// isolation level. It resolves all host paths from the config (workspace, tmp,
// GPU devices) and applies the level-specific restrictions.
func BuildMountSpec(cfg SandboxConfig) (*MountSpec, error) {
	switch cfg.Isolation {
	case IsolationNone:
		return nil, nil // No sandbox at all.
	case IsolationWorkspace:
		return buildWorkspaceSpec(cfg), nil
	case IsolationFull:
		return buildFullSpec(cfg), nil
	default:
		return nil, fmt.Errorf("%w: unknown isolation level %q", ErrConfigInvalid, cfg.Isolation)
	}
}

// buildWorkspaceSpec produces the mount spec for IsolationWorkspace.
//
// Layout:
//
//	--ro-bind /usr, /bin, /lib, /lib64, /etc/ld.so.cache, /etc/alternatives
//	--bind   workspace → /workspace
//	--bind   session-tmp → /tmp
//	--proc   /proc
//	--dev    /dev
//	--unshare-net --unshare-pid --die-with-parent
func buildWorkspaceSpec(cfg SandboxConfig) *MountSpec {
	spec := baseReadonlySpec()
	spec.Mounts = append(spec.Mounts,
		// Writable workspace.
		MountPoint{Source: cfg.WorkspaceDir(), Target: cfg.Workdir, ReadOnly: false, Kind: MountBind},
		// Writable /tmp inside the sandbox.
		MountPoint{Source: cfg.TmpDir(), Target: "/tmp", ReadOnly: false, Kind: MountBind},
	)

	// GPU device access — intentionally removed. Helix agents use API inference.

	spec.Mounts = append(spec.Mounts,
		MountPoint{Target: "/proc", Kind: MountProc},
		MountPoint{Target: "/dev", Kind: MountDev},
	)

	spec.UnshareNet = true
	spec.UnsharePID = true
	spec.DieWithParent = true

	return spec
}

// buildFullSpec produces the mount spec for IsolationFull.
//
// Same as workspace, but:
//   - No GPU device nodes.
//   - /dev is minimal (MountDev still provides a safe subset via bwrap).
//   - /proc is provided but bwrap restricts visibility by default.
func buildFullSpec(cfg SandboxConfig) *MountSpec {
	spec := baseReadonlySpec()
	spec.Mounts = append(spec.Mounts,
		MountPoint{Source: cfg.WorkspaceDir(), Target: cfg.Workdir, ReadOnly: false, Kind: MountBind},
		MountPoint{Source: cfg.TmpDir(), Target: "/tmp", ReadOnly: false, Kind: MountBind},
		// No GPU mounts — full isolation forbids device pass-through.
		MountPoint{Target: "/proc", Kind: MountProc},
		MountPoint{Target: "/dev", Kind: MountDev},
	)

	spec.UnshareNet = true
	spec.UnsharePID = true
	spec.DieWithParent = true
	// Full isolation clears the environment to prevent leaking secrets.
	spec.UnsetEnv = true
	spec.SetEnv = minimalEnv(cfg)

	return spec
}

// baseReadonlySpec returns the common read-only system mounts shared by
// workspace and full isolation. Only paths that exist on the host are included;
// missing directories are silently skipped (e.g., /lib64 on 32-bit systems).
func baseReadonlySpec() *MountSpec {
	candidates := []MountPoint{
		{Source: "/usr", Target: "/usr", ReadOnly: true, Kind: MountBind},
		{Source: "/bin", Target: "/bin", ReadOnly: true, Kind: MountBind},
		{Source: "/lib", Target: "/lib", ReadOnly: true, Kind: MountBind},
		{Source: "/lib64", Target: "/lib64", ReadOnly: true, Kind: MountBind},
		{Source: "/etc/ld.so.cache", Target: "/etc/ld.so.cache", ReadOnly: true, Kind: MountBind},
		{Source: "/etc/alternatives", Target: "/etc/alternatives", ReadOnly: true, Kind: MountBind},
	}

	var mounts []MountPoint
	for _, m := range candidates {
		if pathExists(m.Source) {
			mounts = append(mounts, m)
		}
	}

	return &MountSpec{Mounts: mounts}
}

// GPU support is intentionally removed — Helix agents use API inference.
// gpuMounts() is not needed for local GPU access.

// minimalEnv returns the minimal environment variables set inside a full-
// isolation sandbox. No host secrets are propagated.
func minimalEnv(cfg SandboxConfig) map[string]string {
	return map[string]string{
		"PATH":  "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME":  cfg.Workdir,
		"TERM":  "linux",
		"LANG":  "C.UTF-8",
		"HELIX": "1",
	}
}

// pathExists returns true if path exists on the host filesystem (file or dir).
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
