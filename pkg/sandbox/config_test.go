package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestNetworkMode_IsValid covers the IsValid method on NetworkMode across the
// full set of accepted and rejected values.
func TestNetworkMode_IsValid(t *testing.T) {
	cases := []struct {
		name string
		mode NetworkMode
		want bool
	}{
		{"none is valid", NetworkNone, true},
		{"restricted is valid", NetworkRestricted, true},
		{"empty is invalid", NetworkMode(""), false},
		{"bogus is invalid", NetworkMode("bogus"), false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.mode.IsValid(); got != tc.want {
				t.Fatalf("NetworkMode(%q).IsValid() = %v, want %v", tc.mode, got, tc.want)
			}
		})
	}
}

// TestNetworkMode_String verifies the fmt.Stringer implementation for NetworkMode.
func TestNetworkMode_String(t *testing.T) {
	cases := []struct {
		name string
		mode NetworkMode
		want string
	}{
		{"none", NetworkNone, "none"},
		{"restricted", NetworkRestricted, "restricted"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.mode.String(); got != tc.want {
				t.Fatalf("NetworkMode(%q).String() = %q, want %q", tc.mode, got, tc.want)
			}
		})
	}
}

// TestDefaultConfig verifies every field returned by DefaultConfig, including
// the env-var override paths for SessionRoot and BwrapPath.
func TestDefaultConfig(t *testing.T) {
	t.Run("defaults without env overrides", func(t *testing.T) {
		// Make sure none of the override vars are set for this branch.
		t.Setenv("HELIX_SANDBOX_SESSION_ROOT", "")
		t.Setenv("HELIX_SANDBOX_BWRAP", "")
		t.Setenv("HELIX_SANDBOX_CGROUP_ROOT", "")

		cfg := DefaultConfig()

		if cfg.Isolation != IsolationWorkspace {
			t.Errorf("Isolation = %q, want %q", cfg.Isolation, IsolationWorkspace)
		}
		if cfg.Workdir != "/workspace" {
			t.Errorf("Workdir = %q, want %q", cfg.Workdir, "/workspace")
		}
		if cfg.TimeLimit != 600 {
			t.Errorf("TimeLimit = %d, want %d", cfg.TimeLimit, 600)
		}
		if cfg.MemoryLimit != 2048 {
			t.Errorf("MemoryLimit = %d, want %d", cfg.MemoryLimit, 2048)
		}
		if cfg.Network != NetworkNone {
			t.Errorf("Network = %q, want %q", cfg.Network, NetworkNone)
		}
		if cfg.DryRun {
			t.Errorf("DryRun = true, want false")
		}
		if cfg.Verbose {
			t.Errorf("Verbose = true, want false")
		}
		if cfg.SessionRoot != DefaultSessionRoot {
			t.Errorf("SessionRoot = %q, want %q (env var unset)", cfg.SessionRoot, DefaultSessionRoot)
		}
		if cfg.BwrapPath != BwrapBinary {
			t.Errorf("BwrapPath = %q, want %q (env var unset)", cfg.BwrapPath, BwrapBinary)
		}
	})

	t.Run("env overrides apply", func(t *testing.T) {
		t.Setenv("HELIX_SANDBOX_SESSION_ROOT", "/var/sessions")
		t.Setenv("HELIX_SANDBOX_BWRAP", "/opt/bwrap/bwrap")

		cfg := DefaultConfig()

		if cfg.SessionRoot != "/var/sessions" {
			t.Errorf("SessionRoot = %q, want %q (env var set)", cfg.SessionRoot, "/var/sessions")
		}
		if cfg.BwrapPath != "/opt/bwrap/bwrap" {
			t.Errorf("BwrapPath = %q, want %q (env var set)", cfg.BwrapPath, "/opt/bwrap/bwrap")
		}
	})
}

// TestEnvOr verifies the envOr helper returns the env value when set and the
// fallback when unset.
func TestEnvOr(t *testing.T) {
	const key = "HELIX_SANDBOX_TEST_ENVOR"
	const fallback = "fallback-value"
	const envValue = "env-value"

	t.Run("env var set returns env value", func(t *testing.T) {
		// t.Setenv handles cleanup automatically — no manual Unsetenv needed.
		t.Setenv(key, envValue)
		if got := envOr(key, fallback); got != envValue {
			t.Fatalf("envOr(%q, %q) = %q, want %q", key, fallback, got, envValue)
		}
	})

	t.Run("env var unset returns fallback", func(t *testing.T) {
		// Explicitly clear in case the host env had it set.
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("Unsetenv failed: %v", err)
		}
		if got := envOr(key, fallback); got != fallback {
			t.Fatalf("envOr(%q, %q) = %q, want %q", key, fallback, got, fallback)
		}
	})
}

// TestSandboxConfig_PathMethods exercises the derived path helpers on a
// SandboxConfig and asserts the exact joined paths.
func TestSandboxConfig_PathMethods(t *testing.T) {
	cfg := SandboxConfig{
		SessionRoot: "/tmp/test",
		SessionID:   "abc123",
		CgroupRoot:  "/sys/fs/cgroup",
		Workdir:     "/workspace",
	}

	cases := []struct {
		name string
		got  string
		want string
	}{
		{"SessionDir", cfg.SessionDir(), filepath.Join("/tmp/test", "abc123")},
		{"WorkspaceDir", cfg.WorkspaceDir(), filepath.Join("/tmp/test", "abc123", "workspace")},
		{"TmpDir", cfg.TmpDir(), filepath.Join("/tmp/test", "abc123", "tmp")},
		{"CgroupPath", cfg.CgroupPath(), filepath.Join("/sys/fs/cgroup", "helix", "abc123")},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

// TestSandboxConfig_Validate walks every Validate failure mode plus the
// happy path. Every failure case must wrap ErrConfigInvalid (errors.Is)
// and multi-problem configs must surface every problem in the error message.
func TestSandboxConfig_Validate(t *testing.T) {
	// baseline returns a config that is valid except for fields the case
	// overrides — keeps each case focused on one rule.
	baseline := func() SandboxConfig {
		return SandboxConfig{
			SessionID:   "sess-1",
			Isolation:   IsolationWorkspace,
			Workdir:     "/workspace",
			TimeLimit:   60,
			MemoryLimit: 512,
			Network:     NetworkNone,
			Command:     []string{"/bin/true"},
			SessionRoot: "/tmp/sessions",
			BwrapPath:   "/usr/bin/bwrap",
		}
	}

	cases := []struct {
		name        string
		mutate      func(*SandboxConfig)
		wantErr     bool
		wantSubstrs []string // every substring must appear in the error message
	}{
		{
			name:    "valid config returns nil",
			mutate:  func(*SandboxConfig) {},
			wantErr: false,
		},
		{
			name:        "empty session ID",
			mutate:      func(c *SandboxConfig) { c.SessionID = "" },
			wantErr:     true,
			wantSubstrs: []string{"session ID is empty"},
		},
		{
			name:        "invalid isolation level",
			mutate:      func(c *SandboxConfig) { c.Isolation = IsolationLevel("nonsense") },
			wantErr:     true,
			wantSubstrs: []string{`unknown isolation level "nonsense"`},
		},
		{
			name:        "empty workdir",
			mutate:      func(c *SandboxConfig) { c.Workdir = "" },
			wantErr:     true,
			wantSubstrs: []string{"workdir is empty"},
		},
		{
			name:        "negative time limit",
			mutate:      func(c *SandboxConfig) { c.TimeLimit = -1 },
			wantErr:     true,
			wantSubstrs: []string{"time limit -1 must be >= 0"},
		},
		{
			name:        "negative memory limit",
			mutate:      func(c *SandboxConfig) { c.MemoryLimit = -1 },
			wantErr:     true,
			wantSubstrs: []string{"memory limit -1 must be >= 0"},
		},
		{
			name:        "invalid network mode",
			mutate:      func(c *SandboxConfig) { c.Network = NetworkMode("internet") },
			wantErr:     true,
			wantSubstrs: []string{`unknown network mode "internet"`},
		},
		{
			name:        "empty command",
			mutate:      func(c *SandboxConfig) { c.Command = nil },
			wantErr:     true,
			wantSubstrs: []string{"command is empty"},
		},
		{
			name:        "empty session root",
			mutate:      func(c *SandboxConfig) { c.SessionRoot = "" },
			wantErr:     true,
			wantSubstrs: []string{"session root is empty"},
		},
		{
			name:        "empty bwrap path",
			mutate:      func(c *SandboxConfig) { c.BwrapPath = "" },
			wantErr:     true,
			wantSubstrs: []string{"bwrap path is empty"},
		},
		{
			name: "multiple problems surfaced together",
			mutate: func(c *SandboxConfig) {
				c.SessionID = ""
				c.Workdir = ""
				c.Command = nil
			},
			wantErr: true,
			wantSubstrs: []string{
				"session ID is empty",
				"workdir is empty",
				"command is empty",
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseline()
			tc.mutate(&cfg)

			err := cfg.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("Validate() = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
			if !tc.wantErr {
				return
			}

			if !errors.Is(err, ErrConfigInvalid) {
				t.Fatalf("Validate() error %v does not wrap ErrConfigInvalid", err)
			}
			msg := err.Error()
			for _, want := range tc.wantSubstrs {
				if !contains(msg, want) {
					t.Errorf("Validate() error message missing %q; got %q", want, msg)
				}
			}
		})
	}
}

// TestEffectiveNetwork verifies that EffectiveNetwork returns true only when
// the isolation level is IsolationNone.
func TestEffectiveNetwork(t *testing.T) {
	cases := []struct {
		name      string
		isolation IsolationLevel
		want      bool
	}{
		{"none permits network", IsolationNone, true},
		{"workspace denies network", IsolationWorkspace, false},
		{"full denies network", IsolationFull, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cfg := SandboxConfig{Isolation: tc.isolation}
			if got := cfg.EffectiveNetwork(); got != tc.want {
				t.Fatalf("EffectiveNetwork() = %v, want %v", got, tc.want)
			}
		})
	}
}

// contains is a tiny helper so we can fail with a precise message in
// TestSandboxConfig_Validate without importing strings just for one call.
func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	if needle == "" {
		return 0
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}