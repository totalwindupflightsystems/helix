// test subcommand tests — verify the offline PromptFoo runner wiring.
//
// We exercise both the cobra command construction (TestNewRootCmd_HasTestSubcommand)
// and the runTest() handler (TestRunTest_*) by writing fixtures to t.TempDir()
// and redirecting os.Stdout via the existing captureOutput helper.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewRootCmd_HasTestSubcommand — the test subcommand is registered
// alongside the others in newRootCmd().
func TestNewRootCmd_HasTestSubcommand(t *testing.T) {
	root := newRootCmd()
	found := false
	for _, c := range root.Commands() {
		if strings.HasPrefix(c.Use, "test ") {
			found = true
			break
		}
	}
	if !found {
		t.Error("missing test subcommand")
	}
}

// TestRunTest_HappyPath — every assertion passes, report renders
// "Passed: N" line and runTest returns nil.
func TestRunTest_HappyPath(t *testing.T) {
	dir := t.TempDir()

	// Build a fake .promptfoo.yaml with two passing assertions.
	yamlBody := `prompts:
  - file://prompts/test-component/v1.0.0/prompt.md
providers:
  - id: deepseek:deepseek-v4-pro
tests:
  - description: "test-component/v1.0.0: contains AI agent"
    assert:
      - type: contains
        value: "AI agent"
  - description: "test-component/v1.0.0: no banned tokens"
    assert:
      - type: not-contains
        value: "BANNED_TOKEN"
`
	configPath := filepath.Join(dir, ".promptfoo.yaml")
	if err := os.WriteFile(configPath, []byte(yamlBody), 0o644); err != nil {
		t.Fatal(err)
	}
	// Build the referenced prompt file.
	promptPath := filepath.Join(dir, "prompts", "test-component", "v1.0.0", "prompt.md")
	if err := os.MkdirAll(filepath.Dir(promptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(promptPath, []byte("I am an AI agent.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := &testOptions{
		globalOptions: &globalOptions{},
		configPath:    configPath,
		promptRoot:    dir,
		component:     "test-component",
		version:       "v1.0.0",
	}

	output := captureOutput(func() {
		if err := runTest(opts); err != nil {
			t.Fatalf("runTest returned error on happy path: %v", err)
		}
	})

	for _, want := range []string{
		"Component:  test-component",
		"Version:    v1.0.0",
		"Total:      2",
		"Passed:     2",
		"Failed:     0",
		"✓ test-component/v1.0.0: contains AI agent",
		"✓ test-component/v1.0.0: no banned tokens",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, output)
		}
	}
}

// TestRunTest_FailingAssertion — when one assertion fails, runTest
// surfaces the failure in the report and returns the sentinel error.
func TestRunTest_FailingAssertion(t *testing.T) {
	dir := t.TempDir()

	yamlBody := `prompts:
  - file://prompts/test-component/v1.0.0/prompt.md
providers:
  - id: x
tests:
  - description: "test-component/v1.0.0: missing token"
    assert:
      - type: contains
        value: "EXPECTED_TOKEN"
`
	configPath := filepath.Join(dir, ".promptfoo.yaml")
	if err := os.WriteFile(configPath, []byte(yamlBody), 0o644); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(dir, "prompts", "test-component", "v1.0.0", "prompt.md")
	if err := os.MkdirAll(filepath.Dir(promptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(promptPath, []byte("some prompt without the expected token\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := &testOptions{
		globalOptions: &globalOptions{},
		configPath:    configPath,
		promptRoot:    dir,
		component:     "test-component",
		version:       "v1.0.0",
	}

	output := captureOutput(func() {
		_ = captureStderr(func() {
			if err := runTest(opts); err == nil {
				t.Fatal("runTest should return errPromptTestFailed when an assertion fails")
			}
		})
	})

	if !strings.Contains(output, "Failed:     1") {
		t.Errorf("output should show Failed: 1\nfull output:\n%s", output)
	}
	if !strings.Contains(output, "✗ test-component/v1.0.0: missing token") {
		t.Errorf("output should show failing test\nfull output:\n%s", output)
	}
	if !strings.Contains(output, "EXPECTED_TOKEN") {
		t.Errorf("output should mention the failing value\nfull output:\n%s", output)
	}
}

// TestRunTest_ConfigNotFound — missing .promptfoo.yaml surfaces an
// error (which runTest wraps as a generic test setup error) and
// prints the underlying message to stderr.
func TestRunTest_ConfigNotFound(t *testing.T) {
	dir := t.TempDir()
	opts := &testOptions{
		globalOptions: &globalOptions{},
		configPath:    filepath.Join(dir, "does-not-exist.yaml"),
		promptRoot:    dir,
		component:     "x",
		version:       "v1",
	}

	var gotErr error
	_ = captureOutput(func() {
		_ = captureStderr(func() {
			gotErr = runTest(opts)
		})
	})

	if gotErr == nil {
		t.Fatal("runTest should return error when config file is missing")
	}
	if !strings.Contains(gotErr.Error(), "test setup failed") {
		t.Errorf("error should be the setup-failed sentinel; got %q", gotErr.Error())
	}
}

// TestRunTest_NoMatchingPrompt — the YAML has a prompt but the
// caller's component/version doesn't match it.
func TestRunTest_NoMatchingPrompt(t *testing.T) {
	dir := t.TempDir()
	yamlBody := `prompts:
  - file://prompts/other-component/v1/prompt.md
providers:
  - id: x
tests: []
`
	configPath := filepath.Join(dir, ".promptfoo.yaml")
	if err := os.WriteFile(configPath, []byte(yamlBody), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := &testOptions{
		globalOptions: &globalOptions{},
		configPath:    configPath,
		promptRoot:    dir,
		component:     "test-component",
		version:       "v1.0.0",
	}

	var gotErr error
	_ = captureOutput(func() {
		_ = captureStderr(func() {
			gotErr = runTest(opts)
		})
	})
	if gotErr == nil {
		t.Fatal("runTest should error when no prompt matches")
	}
}

// TestRunTest_PromptFileMissing — the YAML references a prompt file
// that doesn't exist on disk.
func TestRunTest_PromptFileMissing(t *testing.T) {
	dir := t.TempDir()
	yamlBody := `prompts:
  - file://prompts/missing/v1/prompt.md
providers:
  - id: x
tests: []
`
	configPath := filepath.Join(dir, ".promptfoo.yaml")
	if err := os.WriteFile(configPath, []byte(yamlBody), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := &testOptions{
		globalOptions: &globalOptions{},
		configPath:    configPath,
		promptRoot:    dir,
		component:     "missing",
		version:       "v1",
	}

	var gotErr error
	_ = captureOutput(func() {
		_ = captureStderr(func() {
			gotErr = runTest(opts)
		})
	})
	if gotErr == nil {
		t.Fatal("runTest should error when prompt file is missing")
	}
}

// TestRunTest_DefaultsToFirstPrompt — when no component/version is
// passed in opts (i.e. the cobra command-line path with the
// positional args overriding), the runner uses the first prompt in
// the YAML.
func TestRunTest_DefaultsToFirstPrompt(t *testing.T) {
	dir := t.TempDir()
	yamlBody := `prompts:
  - file://prompts/auto/v1/prompt.md
providers:
  - id: x
tests:
  - description: "auto/v1: trivial"
    assert:
      - type: contains
        value: "hi"
`
	configPath := filepath.Join(dir, ".promptfoo.yaml")
	if err := os.WriteFile(configPath, []byte(yamlBody), 0o644); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(dir, "prompts", "auto", "v1", "prompt.md")
	if err := os.MkdirAll(filepath.Dir(promptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(promptPath, []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := &testOptions{
		globalOptions: &globalOptions{},
		configPath:    configPath,
		promptRoot:    dir,
		// Component/Version intentionally empty.
	}

	output := captureOutput(func() {
		if err := runTest(opts); err != nil {
			t.Fatalf("runTest should pass on trivial assertion; got %v", err)
		}
	})
	if !strings.Contains(output, "Component:  auto") {
		t.Errorf("Component should default from first prompt; got:\n%s", output)
	}
	if !strings.Contains(output, "Version:    v1") {
		t.Errorf("Version should default from first prompt; got:\n%s", output)
	}
}

// TestTestCmd_FlagsAreRegistered — verifies the cobra command exposes
// --config and --prompt-root flags with the documented defaults.
func TestTestCmd_FlagsAreRegistered(t *testing.T) {
	cmd := newTestCmd(&globalOptions{})
	if cmd == nil {
		t.Fatal("newTestCmd returned nil")
	}
	cfgFlag := cmd.Flags().Lookup("config")
	if cfgFlag == nil {
		t.Fatal("--config flag missing from test subcommand")
	}
	if cfgFlag.DefValue != ".promptfoo.yaml" {
		t.Errorf("--config default = %q, want .promptfoo.yaml", cfgFlag.DefValue)
	}
	rootFlag := cmd.Flags().Lookup("prompt-root")
	if rootFlag == nil {
		t.Fatal("--prompt-root flag missing from test subcommand")
	}
	if rootFlag.DefValue != "." {
		t.Errorf("--prompt-root default = %q, want .", rootFlag.DefValue)
	}
}

// TestTestCmd_RequiresTwoArgs — cobra ExactArgs(2) rejects wrong
// arity before runTest is even called.
func TestTestCmd_RequiresTwoArgs(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"test", "only-component"})
	err := root.Execute()
	if err == nil {
		t.Error("test subcommand should require two positional args")
	}
}

// TestErrPromptTestFailed_IsSentinel — the sentinel error is exported
// as a comparable value, used by cobra to translate to exit code 1.
func TestErrPromptTestFailed_IsSentinel(t *testing.T) {
	if errPromptTestFailed == nil {
		t.Fatal("errPromptTestFailed must be non-nil")
	}
	if errPromptTestFailed.Error() != "prompt test cases failed" {
		t.Errorf("errPromptTestFailed.Error() = %q, want %q",
			errPromptTestFailed.Error(), "prompt test cases failed")
	}
}
