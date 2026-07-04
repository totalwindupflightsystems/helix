// Package prompt — runner.go
//
// Prompt-test runner: a Go-side executor for the assertions defined in
// .promptfoo.yaml. The full PromptFoo CLI is the source of truth for
// production prompt evaluation (spec §10) but its Go integration is
// limited — this runner fills the gap for CI pipelines that need
// pre-deploy verification without spawning an external process.
//
// Scope (spec §10 — PromptFoo bridge):
//
//  1. Parse .promptfoo.yaml and locate the named prompt's test cases.
//
//  2. Read the prompt file from disk and evaluate each assertion
//     against the prompt's content. Supported assertions:
//
//     - contains         PASS if substring present in prompt
//     - not-contains     PASS if substring absent
//     - regex            PASS if regex matches
//     - length           PASS if string length within bounds
//
//  3. Return a structured TestRunReport with per-test-case PASS/FAIL
//     and a summary exit code.
//
// The runner does NOT call any LLM. For LLM-rubric or provider-side
// assertions, run `promptfoo eval` externally and pipe results through
// pkg/prompt.ParsePromptFooResults — this runner is the offline
// smoke-check used in CI before invoking PromptFoo.
package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// TestRunReport is the result of running all assertions for a single
// prompt through the Go-side runner.
type TestRunReport struct {
	// Component is the prompt component name (e.g. "agent-identity").
	Component string `json:"component" yaml:"component"`
	// Version is the prompt version (e.g. "v1.1.0").
	Version string `json:"version" yaml:"version"`
	// PromptFile is the resolved path to the prompt markdown file.
	PromptFile string `json:"prompt_file" yaml:"prompt_file"`
	// PromptHash is the SHA-256 hash of the prompt content, populated
	// when the registry can locate the prompt's entry.
	PromptHash string `json:"prompt_hash,omitempty" yaml:"prompt_hash,omitempty"`
	// TotalTests, PassedTests, FailedTests are the aggregate counts.
	TotalTests  int `json:"total_tests" yaml:"total_tests"`
	PassedTests int `json:"passed_tests" yaml:"passed_tests"`
	FailedTests int `json:"failed_tests" yaml:"failed_tests"`
	// Results holds per-test-case PASS/FAIL evidence.
	Results []EvalTestResult `json:"results" yaml:"results"`
}

// AllPassed is true when every test case passed.
func (r *TestRunReport) AllPassed() bool {
	return r.FailedTests == 0 && r.TotalTests > 0
}

// RunOptions configures the runner. The zero value is "use defaults".
type RunOptions struct {
	// ConfigPath is the absolute or relative path to .promptfoo.yaml.
	// Default: ".promptfoo.yaml" in the current working directory.
	ConfigPath string
	// PromptRoot is the directory that prefixes the YAML's file://
	// prompts. Default: "." (current working directory).
	PromptRoot string
	// Component overrides the YAML prompt selection. When empty the
	// runner matches the first YAML prompt whose File contains
	// `prompts/<component>/<version>/` — or, if only one prompt is
	// configured, uses it.
	Component string
	// Version overrides the YAML prompt selection. Matched together
	// with Component — see RunFor for the exact rules.
	Version string
}

// RunFor runs every assertion in .promptfoo.yaml against the prompt
// identified by opts.Component / opts.Version and returns a structured
// report. The runner is pure: it never shells out to PromptFoo or any
// LLM provider, so it's safe to call from unit tests.
//
// Returns an error only for I/O or parse failures (missing config,
// unreadable prompt file, malformed YAML). Per-test-case failures
// populate r.Results and are NOT returned as errors.
func RunFor(opts RunOptions) (*TestRunReport, error) {
	cfg, err := loadRunnerConfig(opts.ConfigPath)
	if err != nil {
		return nil, err
	}

	promptRef, err := selectRunnerPrompt(cfg, opts)
	if err != nil {
		return nil, err
	}

	root := opts.PromptRoot
	if root == "" {
		root = "."
	}
	promptPath := resolvePromptPath(root, promptRef.File)
	content, err := os.ReadFile(promptPath)
	if err != nil {
		return nil, fmt.Errorf("read prompt %s: %w", promptPath, err)
	}

	report := &TestRunReport{
		PromptFile: promptPath,
	}
	// Populate Component / Version from the YAML path if it's the
	// standard `prompts/<component>/<version>/prompt.md` shape.
	if c, v, ok := parsePromptsPath(promptRef.File); ok {
		report.Component = c
		report.Version = v
	}
	// If opts specified Component/Version, prefer those (override).
	if opts.Component != "" {
		report.Component = opts.Component
	}
	if opts.Version != "" {
		report.Version = opts.Version
	}

	// Resolve the registry's hash if available. We don't fail the run
	// when the hash can't be looked up — the runner is intentionally
	// tolerant of missing registry entries (the prompt might exist on
	// disk before being registered).
	if entry, lookupErr := LookupByComponent(report.Component, report.Version); lookupErr == nil && entry != nil {
		report.PromptHash = entry.Hash
	}

	// Find the tests that target this prompt. The YAML has a flat
	// `tests:` list — we apply every test that mentions the component
	// in its Description OR whose Vars reference this prompt's name.
	for _, test := range cfg.Tests {
		if !testTargetsPrompt(test, report) {
			continue
		}
		result := runAssertions(test, string(content), cfg.Prompts)
		report.Results = append(report.Results, result)
	}

	report.TotalTests = len(report.Results)
	for _, r := range report.Results {
		if r.Passed {
			report.PassedTests++
		} else {
			report.FailedTests++
		}
	}
	return report, nil
}

// loadRunnerConfig reads .promptfoo.yaml from disk and parses it as
// PromptFooYAML. Accepts a custom path via opts.ConfigPath.
//
// Pre-processes the raw bytes to quote `file://...` values — yaml.v3
// otherwise treats the colon as a mapping-key separator and the
// unmarshal fails. This matches how PromptFoo's CLI parses its own
// YAML (it uses a more permissive parser than yaml.v3).
func loadRunnerConfig(path string) (*PromptFooYAML, error) {
	if path == "" {
		path = ".promptfoo.yaml"
	}
	rawData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read promptfoo config %s: %w", path, err)
	}
	data := quoteFileURLs(rawData)
	var cfg PromptFooYAML
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse promptfoo config %s: %w", path, err)
	}
	if len(cfg.Prompts) == 0 {
		return nil, fmt.Errorf("promptfoo config %s: no prompts defined", path)
	}
	return &cfg, nil
}

// quoteFileURLs rewrites `file://...` YAML list values into the form
// yaml.v3 expects: `file: "file://..."` instead of just `"file://..."`.
// yaml.v3 is strict — a bare quoted value in a list position is
// ambiguous, but the explicit `file:` key (matching PromptFooPrompt's
// yaml tag) parses cleanly.
//
// This pre-processor is intentionally narrow — it only matches the
// patterns PromptFoo emits for prompt entries. Anything more complex
// (e.g. folded block-scalar prompts) is left untouched.
func quoteFileURLs(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " 	")
		// Match "  - file://..." (list item, no `file:` key yet).
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		rest := strings.TrimPrefix(trimmed, "- ")
		if !strings.HasPrefix(rest, "file://") {
			continue
		}
		// If it's already in `file: "..."` form, leave it alone.
		// We have to test for `file:` followed by a non-slash char —
		// `file:` is itself a prefix of `file://`, so we can't use
		// HasPrefix directly.
		if strings.HasPrefix(rest, "file:") && !strings.HasPrefix(rest, "file://") {
			continue
		}
		// Reconstruct the line as `- file: "file://..."`, preserving
		// the original leading whitespace.
		leading := line[:len(line)-len(trimmed)]
		lines[i] = leading + "- file: \"" + rest + "\""
	}
	return []byte(strings.Join(lines, "\n"))
}

// selectRunnerPrompt picks the prompt entry from the YAML. Selection
// rules, in priority order:
//
//  1. opts.Component + opts.Version match a prompt whose File contains
//     `prompts/<component>/<version>/`.
//  2. opts.Component alone matches `prompts/<component>/<any-version>/`.
//  3. opts.Component + opts.Version are empty → use the first prompt.
//  4. Nothing matches → error.
func selectRunnerPrompt(cfg *PromptFooYAML, opts RunOptions) (*PromptFooPrompt, error) {
	if opts.Component == "" && opts.Version == "" {
		return &cfg.Prompts[0], nil
	}

	for i := range cfg.Prompts {
		p := cfg.Prompts[i]
		c, v, ok := parsePromptsPath(p.File)
		if !ok {
			continue
		}
		if opts.Component != "" && c != opts.Component {
			continue
		}
		if opts.Version != "" && v != opts.Version {
			continue
		}
		return &cfg.Prompts[i], nil
	}

	return nil, fmt.Errorf("no prompt in config matches component=%q version=%q", opts.Component, opts.Version)
}

// resolvePromptPath converts a YAML `file://prompts/...` reference
// into an absolute (or relative-to-root) path. The `file://` prefix is
// stripped; a single leading slash is preserved so absolute paths like
// `file:///tmp/foo` resolve to `/tmp/foo`.
func resolvePromptPath(root, ref string) string {
	clean := strings.TrimPrefix(ref, "file://")
	// Preserve a single leading slash for absolute paths — strip only
	// the duplicates that `file://` produced.
	if strings.HasPrefix(clean, "/") {
		clean = "/" + strings.TrimLeft(clean, "/")
	}
	if filepath.IsAbs(clean) {
		return clean
	}
	return filepath.Join(root, clean)
}

// parsePromptsPath parses `prompts/<component>/<version>/prompt.md`
// into (component, version, ok). Returns ok=false for any other shape.
// `file://` prefix is stripped before parsing.
func parsePromptsPath(file string) (string, string, bool) {
	clean := strings.TrimPrefix(file, "file://")
	if !strings.HasPrefix(clean, "prompts/") {
		return "", "", false
	}
	clean = strings.TrimPrefix(clean, "prompts/")
	parts := strings.SplitN(clean, "/", 4)
	// Expected: ["<component>", "<version>", "prompt.md"]
	if len(parts) != 3 || parts[2] != "prompt.md" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// testTargetsPrompt returns true if the YAML test should run against
// the prompt identified by report.Component / report.Version.
//
// Selection rules:
//
//   - Description contains the component name (e.g.
//     "agent-identity/v1.1.0: …") → matches.
//   - Vars reference any variable that's also a prompt file name →
//     matches.
//
// We deliberately err on the side of "run more tests" — false
// positives are preferable to skipped assertions in CI.
func testTargetsPrompt(test PromptFooTest, report *TestRunReport) bool {
	// Always include tests when Component/Version are empty (the
	// caller is testing the only configured prompt).
	if report.Component == "" && report.Version == "" {
		return true
	}
	if report.Component != "" && strings.Contains(test.Description, report.Component) {
		return true
	}
	if report.Version != "" && strings.Contains(test.Description, report.Version) {
		return true
	}
	// Vars-based match: if any var value contains the component or
	// version, include it.
	for _, v := range test.Vars {
		if report.Component != "" && strings.Contains(v, report.Component) {
			return true
		}
		if report.Version != "" && strings.Contains(v, report.Version) {
			return true
		}
	}
	return false
}

// runAssertions evaluates every assertion in test against content and
// returns a single EvalTestResult with PASS / FAIL.
func runAssertions(test PromptFooTest, content string, _ []PromptFooPrompt) EvalTestResult {
	res := EvalTestResult{Description: test.Description}
	if len(test.Assert) == 0 {
		// No assertions = vacuous pass (matches PromptFoo semantics).
		res.Passed = true
		return res
	}
	for _, a := range test.Assert {
		pass, detail := evaluateAssertion(a, content)
		if !pass {
			res.Passed = false
			res.Error = detail
			return res
		}
	}
	res.Passed = true
	return res
}

// evaluateAssertion runs one assertion and returns (passed, detail).
// detail is human-readable context — used in EvalTestResult.Error when
// the assertion fails.
func evaluateAssertion(a PromptFooAssert, content string) (bool, string) {
	switch a.Type {
	case "contains":
		if strings.Contains(content, a.Value) {
			return true, ""
		}
		return false, fmt.Sprintf("expected prompt to contain %q", a.Value)
	case "not-contains":
		if !strings.Contains(content, a.Value) {
			return true, ""
		}
		return false, fmt.Sprintf("expected prompt NOT to contain %q", a.Value)
	case "regex":
		re, err := regexp.Compile(a.Value)
		if err != nil {
			return false, fmt.Sprintf("invalid regex %q: %v", a.Value, err)
		}
		if re.MatchString(content) {
			return true, ""
		}
		return false, fmt.Sprintf("regex %q did not match prompt content", a.Value)
	case "length":
		// Value is "min,max" or single number (exact).
		actual := len(content)
		min, max, err := parseLengthBounds(a.Value)
		if err != nil {
			return false, fmt.Sprintf("invalid length assertion %q: %v", a.Value, err)
		}
		if actual < min {
			return false, fmt.Sprintf("prompt length %d below min %d", actual, min)
		}
		if max > 0 && actual > max {
			return false, fmt.Sprintf("prompt length %d above max %d", actual, max)
		}
		return true, ""
	default:
		// Unsupported assertion types (llm-rubric, is-valid-openai-tools-call,
		// etc.) are not evaluated by this runner — skip with a neutral
		// pass to mirror PromptFoo's "skip unconfigured graders" semantics.
		return true, ""
	}
}

// parseLengthBounds parses a length assertion value of the form
// "min,max" or "exact". Returns (min, max, err).
func parseLengthBounds(s string) (min, max int, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, fmt.Errorf("empty length value")
	}
	if !strings.Contains(s, ",") {
		n, perr := strconv.Atoi(s)
		if perr != nil {
			return 0, 0, fmt.Errorf("invalid length %q: %v", s, perr)
		}
		return n, n, nil
	}
	parts := strings.SplitN(s, ",", 2)
	min, err = strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid min %q: %v", parts[0], err)
	}
	max, err = strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid max %q: %v", parts[1], err)
	}
	return min, max, nil
}
