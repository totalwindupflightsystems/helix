package sandbox

import (
	"testing"
)

// =============================================================================
// ValidIsolationLevels
// =============================================================================

func TestValidIsolationLevels(t *testing.T) {
	levels := ValidIsolationLevels()
	if len(levels) != 3 {
		t.Fatalf("ValidIsolationLevels() len = %d, want 3; got %v", len(levels), levels)
	}

	want := map[string]bool{
		string(IsolationNone):      false,
		string(IsolationWorkspace): false,
		string(IsolationFull):      false,
	}
	for _, l := range levels {
		s := string(l)
		if _, ok := want[s]; !ok {
			t.Errorf("ValidIsolationLevels() contains unexpected level %q", s)
		}
		want[s] = true
	}
	for s, found := range want {
		if !found {
			t.Errorf("ValidIsolationLevels() missing expected level %q", s)
		}
	}
}

// =============================================================================
// IsolationLevel.IsValid
// =============================================================================

func TestIsolationLevel_IsValid(t *testing.T) {
	tests := []struct {
		name  string
		level IsolationLevel
		want  bool
	}{
		{"none is valid", IsolationNone, true},
		{"workspace is valid", IsolationWorkspace, true},
		{"full is valid", IsolationFull, true},
		{"empty string is invalid", "", false},
		{"unknown is invalid", "foobar", false},
		{"partial match is invalid", "nonee", false},
		{"whitespace is invalid", " none", false},
		{"uppercase is invalid", "NONE", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.level.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

// =============================================================================
// IsolationLevel.HasNetwork
// =============================================================================

func TestIsolationLevel_HasNetwork(t *testing.T) {
	tests := []struct {
		name  string
		level IsolationLevel
		want  bool
	}{
		{"none has network", IsolationNone, true},
		{"workspace has no network", IsolationWorkspace, false},
		{"full has no network", IsolationFull, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.level.HasNetwork(); got != tt.want {
				t.Errorf("HasNetwork() = %v, want %v", got, tt.want)
			}
		})
	}
}

// =============================================================================
// IsolationLevel.HasPIDNamespace
// =============================================================================

func TestIsolationLevel_HasPIDNamespace(t *testing.T) {
	tests := []struct {
		name  string
		level IsolationLevel
		want  bool
	}{
		{"none has no PID namespace", IsolationNone, false},
		{"workspace has PID namespace", IsolationWorkspace, true},
		{"full has PID namespace", IsolationFull, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.level.HasPIDNamespace(); got != tt.want {
				t.Errorf("HasPIDNamespace() = %v, want %v", got, tt.want)
			}
		})
	}
}

// =============================================================================
// IsolationLevel.String
// =============================================================================

func TestIsolationLevel_String(t *testing.T) {
	tests := []struct {
		name  string
		level IsolationLevel
		want  string
	}{
		{"none stringifies", IsolationNone, "none"},
		{"workspace stringifies", IsolationWorkspace, "workspace"},
		{"full stringifies", IsolationFull, "full"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.level.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// =============================================================================
// Error sentinel identity
// =============================================================================

func TestSandboxErrorSentinels(t *testing.T) {
	// Verify each error is a distinct non-nil sentinel.
	errors := []struct {
		name string
		err  error
		msg  string
	}{
		{"ErrConfigInvalid", ErrConfigInvalid, "sandbox: configuration is invalid"},
		{"ErrSetupFailed", ErrSetupFailed, "sandbox: setup failed"},
		{"ErrExecutionFailed", ErrExecutionFailed, "sandbox: execution failed"},
		{"ErrTimeoutExceeded", ErrTimeoutExceeded, "sandbox: time limit exceeded"},
		{"ErrBwrapNotFound", ErrBwrapNotFound, "sandbox: bubblewrap binary not found"},
		{"ErrNotImplemented", ErrNotImplemented, "sandbox: not implemented"},
	}
	for _, e := range errors {
		t.Run(e.name, func(t *testing.T) {
			if e.err == nil {
				t.Errorf("%s is nil", e.name)
			}
			if e.err.Error() != e.msg {
				t.Errorf("%s.Error() = %q, want %q", e.name, e.err.Error(), e.msg)
			}
		})
	}
}

// =============================================================================
// Exit code constants are distinct
// =============================================================================

func TestSandboxExitCodes(t *testing.T) {
	codes := map[int]string{
		ExitOK:             "ExitOK",
		ExitConfigError:    "ExitConfigError",
		ExitSetupError:     "ExitSetupError",
		ExitBwrapNotFound:  "ExitBwrapNotFound",
		ExitExecutionError: "ExitExecutionError",
		ExitTimeout:        "ExitTimeout",
		ExitInternalError:  "ExitInternalError",
	}
	seen := map[int]bool{}
	for code, name := range codes {
		if seen[code] {
			t.Errorf("%s (%d) has duplicate exit code", name, code)
		}
		seen[code] = true
	}
	if len(seen) != len(codes) {
		t.Errorf("have %d exit codes, want %d distinct", len(seen), len(codes))
	}
}
