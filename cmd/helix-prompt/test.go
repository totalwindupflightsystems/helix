// test subcommand — runs the offline PromptFoo assertion runner
// against a registered prompt (spec §10, §10.2 PromptFoo bridge).
//
// Wraps pkg/prompt.RunFor so CI pipelines can pre-deploy verify a
// prompt without spawning the external `promptfoo eval` CLI. The
// offline runner only supports the static assertion types
// (contains, not-contains, regex, length) — for llm-rubric and
// other provider-side graders, run PromptFoo externally and pipe
// results through `helix-prompt postci --results`.

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/totalwindupflightsystems/helix/pkg/prompt"
)

// testOptions holds flags for the `helix-prompt test` subcommand.
type testOptions struct {
	*globalOptions
	configPath string
	promptRoot string
	component  string
	version    string
}

func newTestCmd(gOpts *globalOptions) *cobra.Command {
	opts := &testOptions{globalOptions: gOpts}
	cmd := &cobra.Command{
		Use:   "test <component> <version>",
		Short: "Run offline PromptFoo assertions against a registered prompt",
		Long: `test runs the Go-side PromptFoo assertion runner against the prompt
identified by <component>/<version>. It reads .promptfoo.yaml, locates
every test case targeting the named prompt, evaluates the static
assertions (contains / not-contains / regex / length), and prints a
PASS/FAIL report with per-test evidence.

This is the offline smoke-check used in CI before invoking the full
PromptFoo CLI (which adds LLM-rubric and provider-side graders).
Unsupported assertion types are silently skipped — matching
PromptFoo's "skip unconfigured graders" semantics.

Exit codes:
  0 — every targeted test case passed
  1 — one or more test cases failed
  2 — config / prompt file error`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.component = args[0]
			opts.version = args[1]
			return runTest(opts)
		},
	}
	cmd.Flags().StringVar(&opts.configPath, "config", ".promptfoo.yaml",
		"path to .promptfoo.yaml")
	cmd.Flags().StringVar(&opts.promptRoot, "prompt-root", ".",
		"directory that prefixes YAML file:// references")
	return cmd
}

// runTest invokes the offline runner and prints a structured report.
// It does NOT shell out to PromptFoo or any LLM provider — pure Go
// evaluation of static assertions against the on-disk prompt.
func runTest(opts *testOptions) error {
	report, err := prompt.RunFor(prompt.RunOptions{
		ConfigPath: opts.configPath,
		PromptRoot: opts.promptRoot,
		Component:  opts.component,
		Version:    opts.version,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		// Exit code 2 for config / file errors (matches the spec
		// §10.2 / postci exit-code pattern).
		return fmt.Errorf("test setup failed")
	}

	// Print the structured report.
	fmt.Printf("Prompt Test Run:\n")
	fmt.Printf("  Component:  %s\n", report.Component)
	fmt.Printf("  Version:    %s\n", report.Version)
	fmt.Printf("  Prompt:     %s\n", report.PromptFile)
	if report.PromptHash != "" {
		fmt.Printf("  Hash:       %s\n", report.PromptHash)
	}
	fmt.Printf("  Total:      %d\n", report.TotalTests)
	fmt.Printf("  Passed:     %d\n", report.PassedTests)
	fmt.Printf("  Failed:     %d\n", report.FailedTests)

	for _, r := range report.Results {
		if r.Passed {
			fmt.Printf("  ✓ %s\n", r.Description)
			continue
		}
		fmt.Printf("  ✗ %s\n", r.Description)
		if r.Error != "" {
			fmt.Printf("      %s\n", r.Error)
		}
	}

	if !report.AllPassed() {
		// Sentinel error → cobra propagates as exit code 1.
		return errPromptTestFailed
	}
	return nil
}

// errPromptTestFailed is the sentinel error returned when the offline
// runner reports one or more failing test cases.
var errPromptTestFailed = fmt.Errorf("prompt test cases failed")
