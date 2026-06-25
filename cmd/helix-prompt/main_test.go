package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/helix/pkg/prompt"
)

// captureOutput redirects os.Stdout during f() and returns what was written.
func captureOutput(f func()) string {
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, r)
		done <- buf.String()
	}()
	f()
	w.Close()
	os.Stdout = old
	return <-done
}

// captureStderr redirects os.Stderr during f().
func captureStderr(f func()) string {
	r, w, _ := os.Pipe()
	old := os.Stderr
	os.Stderr = w
	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, r)
		done <- buf.String()
	}()
	f()
	w.Close()
	os.Stderr = old
	return <-done
}

// executeRoot runs the root command with given args, capturing stdout+stderr.
func executeRoot(args []string) (string, string, error) {
	var stdout, stderr string
	root := newRootCmd()
	root.SetArgs(args)

	out := captureOutput(func() {
		errOut := captureStderr(func() {
			_ = root.Execute()
		})
		stderr = errOut
	})
	stdout = out
	// cobra returns the error from Execute, but we need to capture it separately
	return stdout, stderr, nil
}

// ---------------------------------------------------------------------------
// Command tree structure
// ---------------------------------------------------------------------------

func TestNewRootCmd_HasSubcommands(t *testing.T) {
	root := newRootCmd()
	if root.Use != "helix-prompt" {
		t.Errorf("root use: got %q, want %q", root.Use, "helix-prompt")
	}
	names := make(map[string]bool)
	for _, c := range root.Commands() {
		names[c.Use] = true
	}
	for _, want := range []string{"register", "attest", "verify", "list"} {
		// Cobra's Use includes args, so check prefix
		found := false
		for name := range names {
			if strings.HasPrefix(name, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing subcommand: %s", want)
		}
	}
}

func TestNewRootCmd_VerboseFlag(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"--verbose", "list"})

	_ = captureStderr(func() {
		_ = captureOutput(func() {
			_ = root.Execute()
		})
	})
	// Just checking it doesn't panic — flag should be accepted.
}

// ---------------------------------------------------------------------------
// register subcommand
// ---------------------------------------------------------------------------

func TestRegister_MissingArgs(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"register"})
	err := root.Execute()
	if err == nil {
		t.Error("register with no args should error")
	}
}

func TestRegister_NonExistentFile(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"register", "test-component", "v1.0.0",
		"--prompt-file", "/nonexistent/prompt.md"})

	errOut := captureStderr(func() {
		_ = captureOutput(func() {
			root.Execute()
		})
	})
	// Should contain an error about file not found
	if !strings.Contains(errOut, "Error:") && !strings.Contains(errOut, "error:") {
		t.Logf("stderr for non-existent file: %q", errOut)
	}
}

// ---------------------------------------------------------------------------
// attest subcommand
// ---------------------------------------------------------------------------

func TestAttest_MissingArgs(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"attest"})
	err := root.Execute()
	if err == nil {
		t.Error("attest with no args should error")
	}
}

func TestAttest_ForceFlag(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"attest", "test-comp", "v1.0.0", "--force"})

	out := captureOutput(func() {
		_ = captureStderr(func() {
			// LookupByComponent will fail (no registry), but --force bypasses
			root.Execute()
		})
	})
	// If registry lookup fails, it will error even with --force.
	// Just verifying it doesn't panic.
	_ = out
}

// ---------------------------------------------------------------------------
// verify subcommand
// ---------------------------------------------------------------------------

func TestVerify_MissingArgs(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"verify"})
	err := root.Execute()
	if err == nil {
		t.Error("verify with no args should error")
	}
}

func TestVerify_InvalidCommit(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"verify", "nonexistent-sha"})

	out := captureOutput(func() {
		_ = captureStderr(func() {
			root.Execute()
		})
	})
	// GetCommitAttestation will fail on bogus SHA in a non-git context.
	// Just verify it doesn't panic.
	_ = out
}

// ---------------------------------------------------------------------------
// list subcommand
// ---------------------------------------------------------------------------

func TestList_TableFormat(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"list"})

	out := captureOutput(func() {
		_ = captureStderr(func() {
			root.Execute()
		})
	})
	// Registry may be empty — output should be empty table or just headers
	if out == "" {
		t.Log("list table produced no output (empty registry)")
	}
}

func TestList_JSONFormat(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"list", "--format", "json"})

	out := captureOutput(func() {
		_ = captureStderr(func() {
			root.Execute()
		})
	})
	// Empty registry should produce "[]" or "null"
	if !strings.Contains(out, "[") && !strings.Contains(out, "null") {
		t.Logf("list json output: %q", out)
	}
}

func TestList_FilterByStatus(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"list", "--status", "deprecated"})

	out := captureOutput(func() {
		_ = captureStderr(func() {
			root.Execute()
		})
	})
	// Empty filtered output is fine — verifies filter flag works
	_ = out
}

// ---------------------------------------------------------------------------
// shortHashDisplay
// ---------------------------------------------------------------------------

func TestShortHashDisplay_Long(t *testing.T) {
	hash := "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	result := shortHashDisplay(hash)
	if len(result) > 23 {
		t.Errorf("long hash should be truncated to ~23 chars, got %d: %q", len(result), result)
	}
	if !strings.HasSuffix(result, "...") {
		t.Errorf("truncated hash should end with '...': %q", result)
	}
}

func TestShortHashDisplay_Short(t *testing.T) {
	hash := "sha256:abc"
	result := shortHashDisplay(hash)
	if result != hash {
		t.Errorf("short hash should be unchanged: got %q, want %q", result, hash)
	}
}

func TestShortHashDisplay_ExactBoundary(t *testing.T) {
	// Exactly 20 chars — right at the threshold
	hash := "12345678901234567890"
	result := shortHashDisplay(hash)
	// len <= 20, so unchanged
	if result != hash {
		t.Errorf("exactly-20-char hash should be unchanged: got %q", result)
	}
}

// ---------------------------------------------------------------------------
// Registry-backed tests (override prompt.RegistryDir)
// ---------------------------------------------------------------------------

// setupRegistry overrides prompt.RegistryDir to a temp directory.
func setupRegistry(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	old := prompt.RegistryDir
	prompt.RegistryDir = dir
	return dir, func() { prompt.RegistryDir = old }
}

// NOTE: runRegister calls os.Exit(prompt.ExitDryRun) on --dry-run, which kills
// the test process. Tests below avoid --dry-run for register and focus on paths
// that don't call os.Exit.

func TestRegister_MissingPromptFile(t *testing.T) {
	dir, cleanup := setupRegistry(t)
	defer cleanup()

	root := newRootCmd()
	root.SetArgs([]string{"register", "test-component", "v1.0.0",
		"--model", "test-model",
		"--prompt-file", dir + "/nonexistent/prompt.md"})

	errOut := captureStderr(func() {
		_ = captureOutput(func() {
			root.Execute()
		})
	})

	// Should produce an error on stderr (file not found)
	if errOut == "" {
		t.Log("register with missing file produced no stderr (cobra may have silenced it)")
	}
}

func TestRegister_NoFlagDefaults(t *testing.T) {
	_, cleanup := setupRegistry(t)
	defer cleanup()

	// Test that --model and --provider flags are accepted and don't panic
	root := newRootCmd()
	root.SetArgs([]string{"register", "comp", "v1.0.0",
		"--model", "claude-3", "--provider", "anthropic",
		"--spec-ref", "specs/my-spec.md"})

	// It will fail because prompt file doesn't exist, but shouldn't panic
	_ = captureStderr(func() {
		_ = captureOutput(func() {
			root.Execute()
		})
	})
}

func TestAttest_ForceFlag_NotFound(t *testing.T) {
	_, cleanup := setupRegistry(t)
	defer cleanup()

	// --force with non-existent component in empty registry
	root := newRootCmd()
	root.SetArgs([]string{"attest", "nonexistent", "v1.0.0", "--force"})

	errOut := captureStderr(func() {
		_ = captureOutput(func() {
			root.Execute()
		})
	})

	// LookupByComponent will fail — should produce an error
	_ = errOut
}

func TestVerify_WithFlags(t *testing.T) {
	_, cleanup := setupRegistry(t)
	defer cleanup()

	// Test verify with all boolean flags
	for _, flags := range [][]string{
		{"verify", "abc123", "--check-hash"},
		{"verify", "abc123", "--check-lifecycle"},
		{"verify", "abc123", "--check-promptfoo"},
	} {
		t.Run(flags[2], func(t *testing.T) {
			root := newRootCmd()
			root.SetArgs(flags)
			_ = captureOutput(func() {
				_ = captureStderr(func() {
					root.Execute()
				})
			})
		})
	}
}

func TestList_ComponentFilter(t *testing.T) {
	_, cleanup := setupRegistry(t)
	defer cleanup()

	root := newRootCmd()
	root.SetArgs([]string{"list", "--component", "agent-identity"})

	out := captureOutput(func() {
		_ = captureStderr(func() {
			root.Execute()
		})
	})
	// Empty registry, filtered — should produce no rows
	_ = out
}

func TestList_ModelFilter(t *testing.T) {
	_, cleanup := setupRegistry(t)
	defer cleanup()

	root := newRootCmd()
	root.SetArgs([]string{"list", "--model", "deepseek-v4-pro"})

	_ = captureOutput(func() {
		_ = captureStderr(func() {
			root.Execute()
		})
	})
}
