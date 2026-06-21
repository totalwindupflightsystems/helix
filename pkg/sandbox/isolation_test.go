package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// validForIsolation returns a SandboxConfig that passes Validate, used as a
// base for the mount-spec builders. Each builder under test consumes
// WorkspaceDir / TmpDir / Workdir, so we point them at a temp directory we
// create on demand.
func validForIsolation(t *testing.T) SandboxConfig {
	t.Helper()
	root := t.TempDir()
	cfg := DefaultConfig()
	cfg.SessionID = "test"
	cfg.SessionRoot = root
	cfg.Workdir = "/ws"
	cfg.Command = []string{"/bin/true"}
	cfg.Isolation = IsolationWorkspace
	cfg.Network = NetworkNone
	return cfg
}

// TestBuildMountSpec dispatches across every isolation level plus the invalid
// case. IsolationNone must return (nil, nil); workspace and full must return a
// usable spec; an unknown level must wrap ErrConfigInvalid.
func TestBuildMountSpec(t *testing.T) {
	t.Run("IsolationNone returns nil spec", func(t *testing.T) {
		cfg := validForIsolation(t)
		cfg.Isolation = IsolationNone

		spec, err := BuildMountSpec(cfg)
		if err != nil {
			t.Fatalf("BuildMountSpec() error = %v, want nil", err)
		}
		if spec != nil {
			t.Fatalf("BuildMountSpec() spec = %+v, want nil", spec)
		}
	})

	t.Run("IsolationWorkspace returns non-nil spec", func(t *testing.T) {
		cfg := validForIsolation(t)
		cfg.Isolation = IsolationWorkspace

		spec, err := BuildMountSpec(cfg)
		if err != nil {
			t.Fatalf("BuildMountSpec() error = %v, want nil", err)
		}
		if spec == nil {
			t.Fatalf("BuildMountSpec() spec = nil, want non-nil")
		}
	})

	t.Run("IsolationFull returns non-nil spec", func(t *testing.T) {
		cfg := validForIsolation(t)
		cfg.Isolation = IsolationFull

		spec, err := BuildMountSpec(cfg)
		if err != nil {
			t.Fatalf("BuildMountSpec() error = %v, want nil", err)
		}
		if spec == nil {
			t.Fatalf("BuildMountSpec() spec = nil, want non-nil")
		}
	})

	t.Run("invalid isolation wraps ErrConfigInvalid", func(t *testing.T) {
		cfg := validForIsolation(t)
		cfg.Isolation = IsolationLevel("nonsense")

		spec, err := BuildMountSpec(cfg)
		if err == nil {
			t.Fatalf("BuildMountSpec() error = nil, want error")
		}
		if !errors.Is(err, ErrConfigInvalid) {
			t.Fatalf("BuildMountSpec() error %v does not wrap ErrConfigInvalid", err)
		}
		if spec != nil {
			t.Fatalf("BuildMountSpec() spec = %+v, want nil on error", spec)
		}
	})
}

// TestBuildWorkspaceSpec checks the workspace-level flags and mounts: writable
// workspace, /proc + /dev, namespace unshares, DieWithParent, and UnsetEnv
// NOT set (workspace preserves a useful base env).
func TestBuildWorkspaceSpec(t *testing.T) {
	cfg := validForIsolation(t)
	spec := buildWorkspaceSpec(cfg)

	if spec == nil {
		t.Fatal("buildWorkspaceSpec() = nil, want non-nil")
	}

	// At least one writable mount must be present (workspace + tmp).
	writableCount := 0
	for _, m := range spec.Mounts {
		if !m.ReadOnly {
			writableCount++
		}
	}
	if writableCount == 0 {
		t.Errorf("buildWorkspaceSpec() mounts have no writable entries; want >= 1")
	}

	// Must include /proc and /dev synthetic mounts.
	hasProc := false
	hasDev := false
	for _, m := range spec.Mounts {
		if m.Target == "/proc" && m.Kind == MountProc {
			hasProc = true
		}
		if m.Target == "/dev" && m.Kind == MountDev {
			hasDev = true
		}
	}
	if !hasProc {
		t.Errorf("buildWorkspaceSpec() missing /proc MountProc")
	}
	if !hasDev {
		t.Errorf("buildWorkspaceSpec() missing /dev MountDev")
	}

	if !spec.UnshareNet {
		t.Errorf("buildWorkspaceSpec() UnshareNet = false, want true")
	}
	if !spec.UnsharePID {
		t.Errorf("buildWorkspaceSpec() UnsharePID = false, want true")
	}
	if !spec.DieWithParent {
		t.Errorf("buildWorkspaceSpec() DieWithParent = false, want true")
	}
	if spec.UnsetEnv {
		t.Errorf("buildWorkspaceSpec() UnsetEnv = true, want false (workspace preserves env)")
	}
}

// TestBuildFullSpec checks the full-isolation level: same writable mounts and
// namespace flags as workspace, plus UnsetEnv=true and a minimal SetEnv map.
func TestBuildFullSpec(t *testing.T) {
	cfg := validForIsolation(t)
	spec := buildFullSpec(cfg)

	if spec == nil {
		t.Fatal("buildFullSpec() = nil, want non-nil")
	}

	if !spec.UnsetEnv {
		t.Errorf("buildFullSpec() UnsetEnv = false, want true (full clears env)")
	}
	if spec.SetEnv == nil {
		t.Fatal("buildFullSpec() SetEnv = nil, want non-nil map")
	}

	required := []string{"PATH", "HOME", "TERM", "LANG", "HELIX"}
	for _, key := range required {
		if _, ok := spec.SetEnv[key]; !ok {
			t.Errorf("buildFullSpec() SetEnv missing %q; got %v", key, spec.SetEnv)
		}
	}

	// Same structural guarantees as workspace — re-check the namespace flags.
	if !spec.UnshareNet {
		t.Errorf("buildFullSpec() UnshareNet = false, want true")
	}
	if !spec.UnsharePID {
		t.Errorf("buildFullSpec() UnsharePID = false, want true")
	}
	if !spec.DieWithParent {
		t.Errorf("buildFullSpec() DieWithParent = false, want true")
	}
}

// TestBaseReadonlySpec verifies the common read-only system mounts: every
// emitted mount must be ReadOnly, the call must not panic on any host, and
// /usr (which exists on essentially every Linux system) must be among the
// emitted mounts.
func TestBaseReadonlySpec(t *testing.T) {
	spec := baseReadonlySpec()
	if spec == nil {
		t.Fatal("baseReadonlySpec() = nil, want non-nil")
	}

	// Every mount we emit must be ReadOnly.
	for i, m := range spec.Mounts {
		if !m.ReadOnly {
			t.Errorf("baseReadonlySpec() Mounts[%d] (%s) ReadOnly = false, want true", i, m.Target)
		}
	}

	// /usr should be present on essentially every Linux host where the
	// sandbox is built/tested. If absent, the test environment is exotic
	// enough to flag.
	hasUsr := false
	for _, m := range spec.Mounts {
		if m.Target == "/usr" && m.Source == "/usr" {
			hasUsr = true
			break
		}
	}
	if !hasUsr {
		t.Errorf("baseReadonlySpec() missing /usr mount (test environment lacks /usr?)")
	}
}

// TestPathExists checks the pathExists helper against a real temp file and a
// guaranteed non-existent path.
func TestPathExists(t *testing.T) {
	t.Run("existing file returns true", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "exists.txt")
		if err := os.WriteFile(file, []byte("hi"), 0o644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}
		if !pathExists(file) {
			t.Errorf("pathExists(%q) = false, want true", file)
		}
	})

	t.Run("existing directory returns true", func(t *testing.T) {
		dir := t.TempDir()
		if !pathExists(dir) {
			t.Errorf("pathExists(%q) = false, want true", dir)
		}
	})

	t.Run("non-existent path returns false", func(t *testing.T) {
		// Path under TempDir() but never created.
		missing := filepath.Join(t.TempDir(), "does-not-exist")
		if pathExists(missing) {
			t.Errorf("pathExists(%q) = true, want false", missing)
		}
	})
}

// TestMinimalEnv verifies the minimal environment map for full isolation:
// exactly the five expected keys with the expected values driven by the
// passed-in Workdir.
func TestMinimalEnv(t *testing.T) {
	cfg := SandboxConfig{Workdir: "/ws"}
	env := minimalEnv(cfg)
	if env == nil {
		t.Fatal("minimalEnv() = nil, want map")
	}

	cases := []struct {
		key  string
		want string
	}{
		{"PATH", "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"},
		{"HOME", "/ws"},
		{"TERM", "linux"},
		{"LANG", "C.UTF-8"},
		{"HELIX", "1"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.key, func(t *testing.T) {
			got, ok := env[tc.key]
			if !ok {
				t.Fatalf("minimalEnv() missing key %q; got %v", tc.key, env)
			}
			if got != tc.want {
				t.Errorf("minimalEnv()[%q] = %q, want %q", tc.key, got, tc.want)
			}
		})
	}

	// Should not propagate host secrets — only the five declared keys.
	if len(env) != len(cases) {
		t.Errorf("minimalEnv() has %d keys, want %d; got %v", len(env), len(cases), env)
	}
}
