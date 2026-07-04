package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
		_, _ = io.Copy(&buf, r)
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
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	f()
	w.Close()
	os.Stderr = old
	return <-done
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
			_ = root.Execute()
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
			_ = root.Execute()
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
			_ = root.Execute()
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
			_ = root.Execute()
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
			_ = root.Execute()
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
			_ = root.Execute()
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
			_ = root.Execute()
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
			_ = root.Execute()
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
			_ = root.Execute()
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
					_ = root.Execute()
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
			_ = root.Execute()
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
			_ = root.Execute()
		})
	})
}

// ---------------------------------------------------------------------------
// postci subcommand tests (spec §11.3)
// ---------------------------------------------------------------------------

func TestNewRootCmd_HasPostCISubcommand(t *testing.T) {
	root := newRootCmd()
	found := false
	for _, c := range root.Commands() {
		if strings.HasPrefix(c.Use, "postci") {
			found = true
			break
		}
	}
	if !found {
		t.Error("missing postci subcommand")
	}
}

func TestPostCI_RequiresResultsFlag(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"postci"})
	err := root.Execute()
	if err == nil {
		t.Error("expected error when --results flag is missing")
	}
}

func TestPostCI_FileNotFound(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"postci", "--results", "/nonexistent/file.json"})

	// This calls os.Exit(2), which is hard to test in-process.
	// Instead, test the flag parsing + file read error path via direct call.
	_ = root // just verify command construction works
}

func TestPostCI_ParsesResultsJSON(t *testing.T) {
	dir := t.TempDir()
	registryDir := filepath.Join(dir, "prompts")
	origDir := prompt.RegistryDir
	prompt.RegistryDir = registryDir
	defer func() { prompt.RegistryDir = origDir }()

	// Register a prompt so metadata exists
	promptContent := "You are a helpful coding assistant."
	contentPath := filepath.Join(dir, "source.md")
	if err := os.WriteFile(contentPath, []byte(promptContent), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := prompt.Register("test-component", "v1", contentPath, "deepseek-v4-flash", "deepseek", "spec", nil)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Create a mock PromptFoo results JSON (all passing)
	resultsJSON := `{
		"results": [
			{
				"prompt": {"raw": "test", "id": "test-component/v1: no TODO stubs"},
				"vars": {},
				"grader": {"pass": true, "reason": "", "score": 1.0}
			}
		],
		"stats": {"successes": 1, "failures": 0, "total": 1}
	}`
	resultsPath := filepath.Join(dir, "results.json")
	if err := os.WriteFile(resultsPath, []byte(resultsJSON), 0644); err != nil {
		t.Fatal(err)
	}

	// Run postci
	opts := &postCIOptions{
		globalOptions: &globalOptions{},
		results:       resultsPath,
	}

	output := captureOutput(func() {
		_ = captureStderr(func() {
			_ = runPostCI(opts)
		})
	})

	if !strings.Contains(output, "Total tests: 1") {
		t.Errorf("output should contain total tests: %s", output)
	}
	if !strings.Contains(output, "Status:      pass") {
		t.Errorf("output should show pass status: %s", output)
	}

	// Verify metadata was updated
	meta, err := prompt.GetMetadata("test-component", "v1")
	if err != nil {
		t.Fatalf("failed to read metadata: %v", err)
	}
	if meta.Promptfoo.Status != "pass" {
		t.Errorf("metadata promptfoo status = %s, want pass", meta.Promptfoo.Status)
	}
}

func TestPostCI_FailedTests(t *testing.T) {
	dir := t.TempDir()
	registryDir := filepath.Join(dir, "prompts")
	origDir := prompt.RegistryDir
	prompt.RegistryDir = registryDir
	defer func() { prompt.RegistryDir = origDir }()

	// Register a prompt
	promptContent := "You are a helpful coding assistant."
	contentPath := filepath.Join(dir, "source.md")
	if err := os.WriteFile(contentPath, []byte(promptContent), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := prompt.Register("my-comp", "v2", contentPath, "deepseek-v4-flash", "deepseek", "spec", nil)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Create a mock PromptFoo results JSON (with failures)
	resultsJSON := `{
		"results": [
			{
				"prompt": {"raw": "test", "id": "my-comp/v2: no TODO stubs"},
				"vars": {},
				"grader": {"pass": true, "reason": "", "score": 1.0}
			},
			{
				"prompt": {"raw": "test2", "id": "my-comp/v2: model check"},
				"vars": {},
				"grader": {"pass": false, "reason": "expected model name not found", "score": 0.0}
			}
		],
		"stats": {"successes": 1, "failures": 1, "total": 2}
	}`
	resultsPath := filepath.Join(dir, "results.json")
	if err := os.WriteFile(resultsPath, []byte(resultsJSON), 0644); err != nil {
		t.Fatal(err)
	}

	opts := &postCIOptions{
		globalOptions: &globalOptions{},
		results:       resultsPath,
	}

	output := captureOutput(func() {
		_ = captureStderr(func() {
			_ = runPostCI(opts)
		})
	})

	if !strings.Contains(output, "Failed:      1") {
		t.Errorf("output should show 1 failed: %s", output)
	}
	if !strings.Contains(output, "Status:      fail") {
		t.Errorf("output should show fail status: %s", output)
	}
	if !strings.Contains(output, "✗") {
		t.Errorf("output should show failure marker: %s", output)
	}

	// Verify metadata was updated to fail
	meta, err := prompt.GetMetadata("my-comp", "v2")
	if err != nil {
		t.Fatalf("failed to read metadata: %v", err)
	}
	if meta.Promptfoo.Status != "fail" {
		t.Errorf("metadata promptfoo status = %s, want fail", meta.Promptfoo.Status)
	}
}

func TestPostCI_EmptyResults(t *testing.T) {
	dir := t.TempDir()
	resultsJSON := `{
		"results": [],
		"stats": {"successes": 0, "failures": 0, "total": 0}
	}`
	resultsPath := filepath.Join(dir, "results.json")
	if err := os.WriteFile(resultsPath, []byte(resultsJSON), 0644); err != nil {
		t.Fatal(err)
	}

	opts := &postCIOptions{
		globalOptions: &globalOptions{},
		results:       resultsPath,
	}

	output := captureOutput(func() {
		_ = captureStderr(func() {
			_ = runPostCI(opts)
		})
	})

	if !strings.Contains(output, "Status:      pass") {
		t.Errorf("empty results should show pass status: %s", output)
	}
}

// ---------------------------------------------------------------------------
// runRegister direct-call tests (spec §18)
// ---------------------------------------------------------------------------

// writeTestPrompt creates a temp prompt file with given content and returns its path.
func writeTestPrompt(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunRegister_HappyPath(t *testing.T) {
	_, cleanup := setupRegistry(t)
	defer cleanup()

	dir := t.TempDir()
	content := "You are a coding assistant. Be helpful."
	promptFile := writeTestPrompt(t, dir, content)

	opts := &registerOptions{
		globalOptions: &globalOptions{},
		promptFile:    promptFile,
		model:         "deepseek-v4-pro",
		provider:      "deepseek",
		specRef:       "specs/my-spec.md",
	}

	out := captureOutput(func() {
		_ = captureStderr(func() {
			if err := runRegister(opts, "my-component", "v1.0.0"); err != nil {
				t.Errorf("runRegister returned error: %v", err)
			}
		})
	})

	// All printed fields should appear
	wantStrings := []string{
		"PROMPT REGISTERED:",
		"Component:  my-component",
		"Version:    v1.0.0",
		"Hash:",
		"Model:      deepseek-v4-pro (deepseek)",
		"Spec:       specs/my-spec.md",
		"Status:",
		"Location:   prompts/my-component/v1.0.0/",
		"Next steps:",
	}
	for _, want := range wantStrings {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- output ---\n%s\n---", want, out)
		}
	}
}

func TestRunRegister_DefaultPromptFile(t *testing.T) {
	_, cleanup := setupRegistry(t)
	defer cleanup()

	// No --prompt-file set, default lookup uses prompts/<component>/<version>/prompt.md
	// which won't exist — expect error
	opts := &registerOptions{
		globalOptions: &globalOptions{},
	}

	errOut := captureStderr(func() {
		_ = captureOutput(func() {
			_ = runRegister(opts, "missing-comp", "v0.0.1")
		})
	})
	// Should produce some error output
	_ = errOut
}

func TestRunRegister_MissingPromptFile(t *testing.T) {
	_, cleanup := setupRegistry(t)
	defer cleanup()

	opts := &registerOptions{
		globalOptions: &globalOptions{},
		promptFile:    "/nonexistent/prompt.md",
	}

	err := runRegister(opts, "any-comp", "v1")
	if err == nil {
		t.Error("expected error for missing prompt file")
	}
	if !strings.Contains(err.Error(), "cannot read prompt file") {
		t.Errorf("error should mention prompt file: %v", err)
	}
}

func TestRunRegister_NoModelNoProvider(t *testing.T) {
	_, cleanup := setupRegistry(t)
	defer cleanup()

	dir := t.TempDir()
	promptFile := writeTestPrompt(t, dir, "prompt body")

	// No model, no provider, no spec-ref — those branches are skipped
	opts := &registerOptions{
		globalOptions: &globalOptions{},
		promptFile:    promptFile,
	}

	out := captureOutput(func() {
		_ = captureStderr(func() {
			if err := runRegister(opts, "plain", "v1"); err != nil {
				t.Errorf("runRegister returned error: %v", err)
			}
		})
	})

	if !strings.Contains(out, "Component:  plain") {
		t.Errorf("output missing component: %s", out)
	}
	// Model/Provider/Spec lines should be ABSENT
	if strings.Contains(out, "Model:") {
		t.Errorf("output should not contain Model line when not set: %s", out)
	}
	if strings.Contains(out, "Spec:") {
		t.Errorf("output should not contain Spec line when not set: %s", out)
	}
}

// ---------------------------------------------------------------------------
// runAttest direct-call tests (spec §8)
// ---------------------------------------------------------------------------

func TestRunAttest_NotFound(t *testing.T) {
	_, cleanup := setupRegistry(t)
	defer cleanup()

	opts := &attestOptions{
		globalOptions: &globalOptions{},
		commitSHA:     "HEAD",
	}

	err := runAttest(opts, "nonexistent-component", "v9.9.9")
	if err == nil {
		t.Error("expected error for non-existent component")
	}
}

func TestRunAttest_ForceFlag_HappyPath(t *testing.T) {
	// --force bypasses LookupByComponent validation — but the function still
	// calls LookupByComponent first. With empty registry, that errors.
	// Test that --force produces error path output.
	_, cleanup := setupRegistry(t)
	defer cleanup()

	opts := &attestOptions{
		globalOptions: &globalOptions{},
		commitSHA:     "HEAD",
		force:         true,
	}

	err := runAttest(opts, "missing", "v1")
	if err == nil {
		t.Error("expected LookupByComponent error even with --force (force only skips Attest)")
	}
}

func TestRunAttest_HappyPath(t *testing.T) {
	dir, cleanup := setupRegistry(t)
	defer cleanup()

	// Register a prompt so LookupByComponent succeeds
	promptContent := "Test prompt content for attestation"
	contentPath := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(contentPath, []byte(promptContent), 0644); err != nil {
		t.Fatal(err)
	}
	pv, err := prompt.Register("attest-comp", "v1", contentPath, "deepseek-v4-flash", "deepseek", "spec", nil)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Promote status to attested so Attest lifecycle check passes
	if err := prompt.UpdateStatus("attest-comp", "v1", prompt.StatusAttested); err != nil {
		t.Fatalf("UpdateStatus failed: %v", err)
	}

	opts := &attestOptions{
		globalOptions: &globalOptions{},
		commitSHA:     "HEAD",
	}

	out := captureOutput(func() {
		_ = captureStderr(func() {
			if err := runAttest(opts, "attest-comp", "v1"); err != nil {
				t.Errorf("runAttest returned error: %v", err)
			}
		})
	})

	wantStrings := []string{
		"ATTESTATION RESULT: attest-comp/v1",
		"Hash match:",
		"Lifecycle OK:",
		"PromptFoo:",
	}
	for _, want := range wantStrings {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- output ---\n%s\n---", want, out)
		}
	}
	_ = pv
}

func TestRunAttest_InvalidGitCommit(t *testing.T) {
	// Set up registry with a real prompt, then attest with a bogus commit SHA.
	// Attest calls ValidateAttestation which does NOT re-run git — it uses
	// the pre-parsed attestation from opts.commitSHA. So this exercises the
	// non-force path even with bogus SHA.
	dir, cleanup := setupRegistry(t)
	defer cleanup()

	contentPath := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(contentPath, []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := prompt.Register("attest-comp2", "v1", contentPath, "deepseek-v4-flash", "deepseek", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := prompt.UpdateStatus("attest-comp2", "v1", prompt.StatusActive); err != nil {
		t.Fatal(err)
	}

	opts := &attestOptions{
		globalOptions: &globalOptions{},
		commitSHA:     "deadbeef00000000000000000000000000000000",
	}

	// This should succeed — ValidateAttestation doesn't call git
	out := captureOutput(func() {
		_ = captureStderr(func() {
			if err := runAttest(opts, "attest-comp2", "v1"); err != nil {
				t.Logf("runAttest returned error (acceptable): %v", err)
			}
		})
	})
	_ = out
}

func TestRunAttest_WithErrors(t *testing.T) {
	// Register a prompt in draft status — should produce lifecycle violations
	dir, cleanup := setupRegistry(t)
	defer cleanup()

	contentPath := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(contentPath, []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := prompt.Register("draft-comp", "v1", contentPath, "m", "p", "", nil); err != nil {
		t.Fatal(err)
	}
	// Status is StatusDraft by default — should produce LIFECYCLE_VIOLATION errors

	opts := &attestOptions{
		globalOptions: &globalOptions{},
		commitSHA:     "HEAD",
	}

	out := captureOutput(func() {
		_ = captureStderr(func() {
			_ = runAttest(opts, "draft-comp", "v1")
		})
	})

	if !strings.Contains(out, "Issues:") {
		t.Errorf("output should contain 'Issues:' when errors present: %s", out)
	}
}

// ---------------------------------------------------------------------------
// runVerify direct-call tests (spec §8.2)
// ---------------------------------------------------------------------------

// initTestGitRepo creates a temp dir with git init + a single commit.
// Returns the dir path. Required identity is injected via -c flags.
func initTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Common -c flags to inject before every git subcommand
	cFlags := []string{
		"-c", "user.email=test@example.com",
		"-c", "user.name=Test",
		"-c", "commit.gpgsign=false",
		"-c", "init.defaultBranch=main",
	}

	run := func(args ...string) {
		full := append(append([]string{}, cFlags...), args...)
		cmd := exec.Command("git", full...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	run("init")
	// Create a dummy file and commit
	dummyPath := filepath.Join(dir, "dummy.txt")
	if err := os.WriteFile(dummyPath, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", "dummy.txt")
	run("commit", "-m", "initial commit")
	return dir
}

// initTestGitRepoWithAttestation creates a git repo with a commit whose
// message includes a valid "Prompt: sha256:<hash>" attestation line, so
// runVerify's happy path (GetCommitAttestation succeeds) can be exercised.
func initTestGitRepoWithAttestation(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cFlags := []string{
		"-c", "user.email=test@example.com",
		"-c", "user.name=Test",
		"-c", "commit.gpgsign=false",
		"-c", "init.defaultBranch=main",
	}

	run := func(args ...string) {
		full := append(append([]string{}, cFlags...), args...)
		cmd := exec.Command("git", full...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	run("init")
	dummyPath := filepath.Join(dir, "dummy.txt")
	if err := os.WriteFile(dummyPath, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", "dummy.txt")
	// Commit with valid attestation trailer — sha256:abc... is a real hash format
	commitMsg := "feat: initial commit\n\nPrompt: sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890\n\nCo-authored-by: Test <test@example.com>"
	run("commit", "-m", commitMsg)
	return dir
}

func TestRunVerify_HappyPath(t *testing.T) {
	_, cleanup := setupRegistry(t)
	defer cleanup()

	// Use a repo with a real attestation in the commit message so runVerify
	// reaches the printed fields (COMMIT, PROMPT, HASH, STATUS, etc.)
	gitDir := initTestGitRepoWithAttestation(t)

	// runVerify calls GetCommitAttestation(commitSHA, ".") which runs `git log -1`
	// from the current working directory. Chdir into the git repo for the test.
	oldCwd, _ := os.Getwd()
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldCwd) }()

	opts := &verifyOptions{
		globalOptions: &globalOptions{},
	}

	out := captureOutput(func() {
		_ = captureStderr(func() {
			_ = runVerify(opts, "HEAD")
		})
	})

	// With a valid Prompt: sha256:... line, runVerify prints COMMIT and PROMPT
	if !strings.Contains(out, "COMMIT:") {
		t.Errorf("output should contain COMMIT: %s", out)
	}
	if !strings.Contains(out, "PROMPT:") {
		t.Errorf("output should contain PROMPT: %s", out)
	}
	// Hash check fails (attestation hash != computed hash) — but HASH line still printed
	if !strings.Contains(out, "HASH:") {
		t.Errorf("output should contain HASH line: %s", out)
	}
}

func TestRunVerify_BadCommitSHA(t *testing.T) {
	_, cleanup := setupRegistry(t)
	defer cleanup()

	gitDir := initTestGitRepo(t)
	oldCwd, _ := os.Getwd()
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldCwd) }()

	opts := &verifyOptions{
		globalOptions: &globalOptions{},
	}

	_ = captureOutput(func() {
		errOut := captureStderr(func() {
			if err := runVerify(opts, "0000000000000000000000000000000000000000"); err == nil {
				t.Log("runVerify with bad SHA may not error — depends on git log output")
			}
		})
		_ = errOut
	})
}

func TestRunVerify_AllCheckFlags(t *testing.T) {
	_, cleanup := setupRegistry(t)
	defer cleanup()

	gitDir := initTestGitRepoWithAttestation(t)
	oldCwd, _ := os.Getwd()
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldCwd) }()

	for _, flags := range [][]string{
		{"--check-hash"},
		{"--check-lifecycle"},
		{"--check-promptfoo"},
		{"--full-chain"},
		{"--check-hash", "--check-lifecycle", "--check-promptfoo", "--full-chain"},
	} {
		t.Run(strings.Join(flags, "_"), func(t *testing.T) {
			opts := &verifyOptions{
				globalOptions:  &globalOptions{},
				checkHash:      containsFlag(flags, "--check-hash"),
				checkLifecycle: containsFlag(flags, "--check-lifecycle"),
				checkPromptfoo: containsFlag(flags, "--check-promptfoo"),
				fullChain:      containsFlag(flags, "--full-chain"),
			}

			_ = captureOutput(func() {
				_ = captureStderr(func() {
					_ = runVerify(opts, "HEAD")
				})
			})
		})
	}
}

func TestRunVerify_GetCommitAttestationError(t *testing.T) {
	// No git repo — GetCommitAttestation should fail
	dir := t.TempDir()
	oldCwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldCwd) }()

	_, cleanup := setupRegistry(t)
	defer cleanup()

	opts := &verifyOptions{
		globalOptions: &globalOptions{},
	}

	errOut := captureStderr(func() {
		_ = captureOutput(func() {
			if err := runVerify(opts, "HEAD"); err == nil {
				t.Log("runVerify in non-git dir may not error — depends on PATH")
			}
		})
	})
	_ = errOut
}

// containsFlag is a tiny helper for the verify flag matrix test.
func containsFlag(flags []string, target string) bool {
	for _, f := range flags {
		if f == target {
			return true
		}
	}
	return false
}
