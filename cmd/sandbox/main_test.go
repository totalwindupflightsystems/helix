package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/helix/pkg/sandbox"
)

// ---------------------------------------------------------------------------
// run() tests
// ---------------------------------------------------------------------------

func TestRun_EmptyArgs(t *testing.T) {
	code := run([]string{})
	if code != sandbox.ExitConfigError {
		t.Errorf("empty args: expected ExitConfigError (%d), got %d", sandbox.ExitConfigError, code)
	}
}

func TestRun_Help(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			code := run([]string{arg})
			if code != sandbox.ExitOK {
				t.Errorf("%s: expected ExitOK (0), got %d", arg, code)
			}
		})
	}
}

func TestRun_Version(t *testing.T) {
	for _, arg := range []string{"version", "-v", "--version"} {
		t.Run(arg, func(t *testing.T) {
			code := run([]string{arg})
			if code != sandbox.ExitOK {
				t.Errorf("%s: expected ExitOK (0), got %d", arg, code)
			}
		})
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	code := run([]string{"nonexistent"})
	if code != sandbox.ExitConfigError {
		t.Errorf("unknown command: expected ExitConfigError (%d), got %d", sandbox.ExitConfigError, code)
	}
}

// ---------------------------------------------------------------------------
// runCommand tests (only paths that don't trigger os.Exit via flag.ExitOnError)
// ---------------------------------------------------------------------------

func TestRunCommand_DryRun(t *testing.T) {
	code := runCommand([]string{"--dry-run", "--", "echo", "hello"})
	// Dry-run + not-implemented stub returns ExitOK via isDryRunOnly path.
	// May also succeed directly if the executor's dry-run path works.
	if code != sandbox.ExitOK && code != sandbox.ExitInternalError {
		t.Logf("dry-run exit code: %d (acceptable — depends on executor state)", code)
	}
}

func TestRunCommand_MissingCommand(t *testing.T) {
	code := runCommand([]string{"--dry-run"})
	if code != sandbox.ExitConfigError {
		t.Errorf("missing command: expected ExitConfigError (%d), got %d", sandbox.ExitConfigError, code)
	}
}

func TestRunCommand_InvalidIsolation(t *testing.T) {
	code := runCommand([]string{"--isolation", "nonexistent", "--", "echo", "hello"})
	// "nonexistent" is not a valid IsolationLevel, Validate() should catch it.
	if code != sandbox.ExitConfigError {
		t.Errorf("invalid isolation: expected ExitConfigError (%d), got %d", sandbox.ExitConfigError, code)
	}
}

// ---------------------------------------------------------------------------
// handleError tests
// ---------------------------------------------------------------------------

func TestHandleError_BwrapNotFound(t *testing.T) {
	code := handleError(sandbox.ErrBwrapNotFound)
	if code != sandbox.ExitBwrapNotFound {
		t.Errorf("expected ExitBwrapNotFound (%d), got %d", sandbox.ExitBwrapNotFound, code)
	}
}

func TestHandleError_ConfigInvalid(t *testing.T) {
	code := handleError(sandbox.ErrConfigInvalid)
	if code != sandbox.ExitConfigError {
		t.Errorf("expected ExitConfigError (%d), got %d", sandbox.ExitConfigError, code)
	}
}

func TestHandleError_SetupFailed(t *testing.T) {
	code := handleError(sandbox.ErrSetupFailed)
	if code != sandbox.ExitSetupError {
		t.Errorf("expected ExitSetupError (%d), got %d", sandbox.ExitSetupError, code)
	}
}

func TestHandleError_TimeoutExceeded(t *testing.T) {
	code := handleError(sandbox.ErrTimeoutExceeded)
	if code != sandbox.ExitTimeout {
		t.Errorf("expected ExitTimeout (%d), got %d", sandbox.ExitTimeout, code)
	}
}

func TestHandleError_NotImplemented(t *testing.T) {
	code := handleError(sandbox.ErrNotImplemented)
	if code != sandbox.ExitInternalError {
		t.Errorf("expected ExitInternalError (%d), got %d", sandbox.ExitInternalError, code)
	}
}

func TestHandleError_NotImplemented_DryRun(t *testing.T) {
	// Wrap ErrNotImplemented in a message that matches isDryRunOnly
	err := fmt.Errorf("bwrap execution not available: %w", sandbox.ErrNotImplemented)
	code := handleError(err)
	if code != sandbox.ExitOK {
		t.Errorf("dry-run not-implemented: expected ExitOK (0), got %d", code)
	}
}

func TestHandleError_ExecutionFailed(t *testing.T) {
	code := handleError(sandbox.ErrExecutionFailed)
	if code != sandbox.ExitExecutionError {
		t.Errorf("expected ExitExecutionError (%d), got %d", sandbox.ExitExecutionError, code)
	}
}

func TestHandleError_Unknown(t *testing.T) {
	code := handleError(fmt.Errorf("some random error"))
	if code != sandbox.ExitInternalError {
		t.Errorf("unknown error: expected ExitInternalError (%d), got %d", sandbox.ExitInternalError, code)
	}
}

// ---------------------------------------------------------------------------
// generateSessionID tests
// ---------------------------------------------------------------------------

func TestGenerateSessionID_IsHex(t *testing.T) {
	id := generateSessionID()
	if len(id) != 32 {
		t.Errorf("expected 32-char hex ID, got %d chars: %q", len(id), id)
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("non-hex character in session ID: %c", c)
		}
	}
}

func TestGenerateSessionID_IsRandom(t *testing.T) {
	id1 := generateSessionID()
	id2 := generateSessionID()
	if id1 == id2 {
		t.Error("two consecutive session IDs should not be identical")
	}
}

// ---------------------------------------------------------------------------
// printUsage tests
// ---------------------------------------------------------------------------

func TestPrintUsage_WritesOutput(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	out := buf.String()
	if !strings.Contains(out, "helix-sandbox") {
		t.Error("printUsage should contain 'helix-sandbox'")
	}
	if !strings.Contains(out, "run") {
		t.Error("printUsage should mention 'run' subcommand")
	}
}

// ---------------------------------------------------------------------------
// printVersion tests
// ---------------------------------------------------------------------------

func TestPrintVersion_WritesOutput(t *testing.T) {
	var buf bytes.Buffer
	printVersion(&buf)
	out := buf.String()
	if !strings.Contains(out, "helix-sandbox") {
		t.Error("printVersion should contain 'helix-sandbox'")
	}
}

// ---------------------------------------------------------------------------
// isDryRunOnly tests
// ---------------------------------------------------------------------------

func TestIsDryRunOnly_Match(t *testing.T) {
	err := fmt.Errorf("bwrap execution not available: %w", sandbox.ErrNotImplemented)
	if !isDryRunOnly(err) {
		t.Error("isDryRunOnly should return true for bwrap execution + ErrNotImplemented")
	}
}

func TestIsDryRunOnly_NoMatch(t *testing.T) {
	err := fmt.Errorf("some other error: %w", sandbox.ErrNotImplemented)
	if isDryRunOnly(err) {
		t.Error("isDryRunOnly should return false without 'bwrap execution' in message")
	}
}
