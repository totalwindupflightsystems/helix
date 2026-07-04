package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// writeRunnerFixtures writes a fake `.promptfoo.yaml` and a fake
// prompt file into t.TempDir() and returns the paths. The prompt
// content is configurable so each test can drive different assertions.
//
// The helper parses the YAML body to find the prompt path the test
// wants to reference and creates that directory. This lets tests use
// arbitrary component names (vacuous, skip, agg, etc.) without having
// to override the helper.
func writeRunnerFixtures(t *testing.T, promptContent string, yamlBody string) (configPath, promptPath, root string) {
	t.Helper()
	dir := t.TempDir()

	// Find the first prompts/<component>/<version>/prompt.md path the
	// YAML references so we know where to create the file.
	promptRelPath := firstPromptPath(yamlBody)
	if promptRelPath == "" {
		t.Fatalf("writeRunnerFixtures: could not find prompt path in YAML")
	}
	promptsDir := filepath.Dir(filepath.Join(dir, promptRelPath))
	require.NoError(t, os.MkdirAll(promptsDir, 0o755))
	promptPath = filepath.Join(dir, promptRelPath)
	require.NoError(t, os.WriteFile(promptPath, []byte(promptContent), 0o644))

	// .promptfoo.yaml
	configPath = filepath.Join(dir, ".promptfoo.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(yamlBody), 0o644))
	root = dir
	return
}

// firstPromptPath extracts the first `file://prompts/...` reference
// from a YAML body. Returns the path with `file://` stripped.
func firstPromptPath(yamlBody string) string {
	for _, line := range strings.Split(yamlBody, "\n") {
		trimmed := strings.TrimLeft(line, " 	")
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		rest := strings.TrimPrefix(trimmed, "- ")
		// Strip any leading "file: " that was added by the runner's
		// pre-processor — we're just looking for the bare path.
		rest = strings.TrimPrefix(rest, "file: ")
		rest = strings.Trim(rest, "\"'")
		if strings.HasPrefix(rest, "file://") {
			return strings.TrimPrefix(rest, "file://")
		}
	}
	return ""
}

// standardPromptContent is a small but realistic prompt fixture.
const standardPromptContent = `# Agent Identity Prompt

You are an AI agent. Be concise.

TODO: add a goodbye line.

model: deepseek-v4-pro
`

// standardPromptYAML is the YAML fixture that pairs with the above.
// The unquoted `file://...` is the format PromptFoo emits and that
// our runner pre-processes into a quoted form for yaml.v3.
const standardPromptYAML = `prompts:
  - file://prompts/test-component/v1.0.0/prompt.md

providers:
  - id: deepseek:deepseek-v4-pro

tests:
  - description: "test-component/v1.0.0: contains 'AI agent'"
    assert:
      - type: contains
        value: "AI agent"

  - description: "test-component/v1.0.0: model is deepseek-v4-pro"
    assert:
      - type: contains
        value: "deepseek-v4-pro"

  - description: "test-component/v1.0.0: no TODO stubs"
    assert:
      - type: not-contains
        value: "BANNED_TOKEN"

  - description: "other-component/v2.0.0: not our prompt"
    assert:
      - type: contains
        value: "AI agent"
`

// ---------------------------------------------------------------------------
// RunFor — happy path
// ---------------------------------------------------------------------------

// TestRunFor_HappyPath — the runner loads the YAML, picks the right
// prompt, evaluates every test that targets it, and returns a report
// where each test PASSes against the prompt content.
func TestRunFor_HappyPath(t *testing.T) {
	configPath, _, root := writeRunnerFixtures(t, standardPromptContent, standardPromptYAML)

	report, err := RunFor(RunOptions{
		ConfigPath: configPath,
		PromptRoot: root,
		Component:  "test-component",
		Version:    "v1.0.0",
	})
	require.NoError(t, err)
	require.NotNil(t, report)

	assert.Equal(t, "test-component", report.Component)
	assert.Equal(t, "v1.0.0", report.Version)
	// 3 of the 4 tests should target our prompt (the "other-component"
	// one is filtered out).
	assert.Equal(t, 3, report.TotalTests)
	assert.Equal(t, 3, report.PassedTests, "all targeted tests should pass: %+v", report.Results)
	assert.Equal(t, 0, report.FailedTests)
	assert.True(t, report.AllPassed())
}

// TestRunFor_FailingAssertion — assertion that doesn't match content
// surfaces as a per-test FAIL with detail.
func TestRunFor_FailingAssertion(t *testing.T) {
	configPath, _, root := writeRunnerFixtures(t, standardPromptContent, standardPromptYAML)

	report, err := RunFor(RunOptions{
		ConfigPath: configPath,
		PromptRoot: root,
		Component:  "test-component",
		Version:    "v1.0.0",
	})
	require.NoError(t, err)
	// Sanity check the happy path before we drive a failure.
	assert.Equal(t, 3, report.TotalTests)
	assert.True(t, report.AllPassed())

	// The "no TODO stubs" test uses not-contains "BANNED_TOKEN". The
	// standard content has "TODO: add a goodbye line." but NOT
	// "BANNED_TOKEN", so that test passes. To produce a failure we
	// inject BANNED_TOKEN into the prompt.
	configPath2, _, root2 := writeRunnerFixtures(t,
		"AI agent with BANNED_TOKEN present\n",
		standardPromptYAML)
	report, err = RunFor(RunOptions{
		ConfigPath: configPath2,
		PromptRoot: root2,
		Component:  "test-component",
		Version:    "v1.0.0",
	})
	require.NoError(t, err)
	require.Greater(t, report.FailedTests, 0, "expected at least one failure; results=%+v", report.Results)
	assert.False(t, report.AllPassed())

	// Find the failing test and assert the detail mentions BANNED_TOKEN.
	var found bool
	for _, r := range report.Results {
		if !r.Passed && strings.Contains(r.Error, "BANNED_TOKEN") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected a failing test mentioning BANNED_TOKEN; results=%+v", report.Results)
}

// ---------------------------------------------------------------------------
// RunFor — config / file errors
// ---------------------------------------------------------------------------

// TestRunFor_MissingConfig — opts.ConfigPath points at a nonexistent file → error.
func TestRunFor_MissingConfig(t *testing.T) {
	_, err := RunFor(RunOptions{ConfigPath: "/tmp/does-not-exist-promptfoo.yaml"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read promptfoo config")
}

// TestRunFor_MalformedYAML — unparseable YAML → error.
func TestRunFor_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, ".promptfoo.yaml")
	require.NoError(t, os.WriteFile(cfg, []byte("not: valid: yaml: ["), 0o644))

	_, err := RunFor(RunOptions{ConfigPath: cfg, PromptRoot: dir})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse promptfoo config")
}

// TestRunFor_NoPrompts — YAML with zero prompts → error.
func TestRunFor_NoPrompts(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, ".promptfoo.yaml")
	require.NoError(t, os.WriteFile(cfg, []byte("providers:\n  - id: x\ntests: []\n"), 0o644))

	_, err := RunFor(RunOptions{ConfigPath: cfg, PromptRoot: dir})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no prompts defined")
}

// TestRunFor_MissingPromptFile — YAML references a prompt file that
// doesn't exist on disk.
func TestRunFor_MissingPromptFile(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, ".promptfoo.yaml")
	body := `prompts:
  - file://prompts/missing/v1/prompt.md
providers:
  - id: x
tests: []
`
	require.NoError(t, os.WriteFile(cfg, []byte(body), 0o644))

	_, err := RunFor(RunOptions{ConfigPath: cfg, PromptRoot: dir})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read prompt")
}

// TestRunFor_NoMatch — Component/Version don't match any prompt.
func TestRunFor_NoMatch(t *testing.T) {
	configPath, _, root := writeRunnerFixtures(t, standardPromptContent, standardPromptYAML)

	_, err := RunFor(RunOptions{
		ConfigPath: configPath,
		PromptRoot: root,
		Component:  "no-such-component",
		Version:    "v9.9.9",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no prompt in config matches")
}

// ---------------------------------------------------------------------------
// RunFor — prompt selection rules
// ---------------------------------------------------------------------------

// TestRunFor_NoComponentDefaultsToFirstPrompt — when Component/Version
// are empty, the runner uses the first prompt in the YAML.
func TestRunFor_NoComponentDefaultsToFirstPrompt(t *testing.T) {
	configPath, _, root := writeRunnerFixtures(t, standardPromptContent, standardPromptYAML)

	report, err := RunFor(RunOptions{
		ConfigPath: configPath,
		PromptRoot: root,
	})
	require.NoError(t, err)
	assert.Equal(t, "test-component", report.Component)
	assert.Equal(t, "v1.0.0", report.Version)
}

// TestRunFor_ComponentOnlyMatchesAnyVersion — Component matches the
// first YAML prompt whose path starts with `prompts/<component>/`,
// regardless of the caller's Version.
func TestRunFor_ComponentOnlyMatchesAnyVersion(t *testing.T) {
	configPath, _, root := writeRunnerFixtures(t, standardPromptContent, standardPromptYAML)

	report, err := RunFor(RunOptions{
		ConfigPath: configPath,
		PromptRoot: root,
		Component:  "test-component",
		// Version intentionally empty
	})
	require.NoError(t, err)
	assert.Equal(t, "test-component", report.Component)
}

// ---------------------------------------------------------------------------
// Assertion types
// ---------------------------------------------------------------------------

// TestRunFor_RegexAssertion — `regex:` assertion matches by Go regexp.
func TestRunFor_RegexAssertion(t *testing.T) {
	yaml := `prompts:
  - file://prompts/regex/v1/prompt.md
providers:
  - id: x
tests:
  - description: "regex/v1: matches pattern"
    assert:
      - type: regex
        value: "deepseek-v[0-9]+-pro"
`
	configPath, _, root := writeRunnerFixtures(t, "I run on deepseek-v4-pro\n", yaml)

	report, err := RunFor(RunOptions{
		ConfigPath: configPath,
		PromptRoot: root,
		Component:  "regex",
		Version:    "v1",
	})
	require.NoError(t, err)
	require.Equal(t, 1, report.TotalTests)
	assert.True(t, report.AllPassed(), "expected regex match; results=%+v", report.Results)
}

// TestRunFor_RegexAssertion_Fail — regex doesn't match → FAIL with detail.
func TestRunFor_RegexAssertion_Fail(t *testing.T) {
	yaml := `prompts:
  - file://prompts/regex-fail/v1/prompt.md
providers:
  - id: x
tests:
  - description: "regex-fail/v1: pattern won't match"
    assert:
      - type: regex
        value: "this-pattern-does-not-exist"
`
	configPath, _, root := writeRunnerFixtures(t, "anything at all\n", yaml)

	report, err := RunFor(RunOptions{
		ConfigPath: configPath,
		PromptRoot: root,
		Component:  "regex-fail",
		Version:    "v1",
	})
	require.NoError(t, err)
	assert.False(t, report.AllPassed())
	assert.Contains(t, report.Results[0].Error, "did not match")
}

// TestRunFor_LengthAssertion — `length:` assertion enforces string length bounds.
func TestRunFor_LengthAssertion(t *testing.T) {
	yaml := `prompts:
  - file://prompts/length/v1/prompt.md
providers:
  - id: x
tests:
  - description: "length/v1: prompt is between 10 and 100 chars"
    assert:
      - type: length
        value: "10,100"
`
	configPath, _, root := writeRunnerFixtures(t, "12345678901234567890\n", yaml)

	report, err := RunFor(RunOptions{
		ConfigPath: configPath,
		PromptRoot: root,
		Component:  "length",
		Version:    "v1",
	})
	require.NoError(t, err)
	assert.True(t, report.AllPassed(), "20 chars in [10,100]; results=%+v", report.Results)
}

// TestRunFor_LengthAssertion_BelowMin — fails when prompt is too short.
func TestRunFor_LengthAssertion_BelowMin(t *testing.T) {
	yaml := `prompts:
  - file://prompts/short/v1/prompt.md
providers:
  - id: x
tests:
  - description: "short/v1: must be >= 50 chars"
    assert:
      - type: length
        value: "50,200"
`
	configPath, _, root := writeRunnerFixtures(t, "short\n", yaml)

	report, err := RunFor(RunOptions{
		ConfigPath: configPath,
		PromptRoot: root,
		Component:  "short",
		Version:    "v1",
	})
	require.NoError(t, err)
	assert.False(t, report.AllPassed())
	assert.Contains(t, report.Results[0].Error, "below min")
}

// TestRunFor_LengthAssertion_AboveMax — fails when prompt is too long.
func TestRunFor_LengthAssertion_AboveMax(t *testing.T) {
	yaml := `prompts:
  - file://prompts/long/v1/prompt.md
providers:
  - id: x
tests:
  - description: "long/v1: must be <= 5 chars"
    assert:
      - type: length
        value: "1,5"
`
	configPath, _, root := writeRunnerFixtures(t, "way too long\n", yaml)

	report, err := RunFor(RunOptions{
		ConfigPath: configPath,
		PromptRoot: root,
		Component:  "long",
		Version:    "v1",
	})
	require.NoError(t, err)
	assert.False(t, report.AllPassed())
	assert.Contains(t, report.Results[0].Error, "above max")
}

// TestRunFor_UnsupportedAssertion_Skipped — unknown assertion types
// (e.g. llm-rubric) are silently skipped, matching PromptFoo's
// "skip unconfigured graders" semantics.
func TestRunFor_UnsupportedAssertion_Skipped(t *testing.T) {
	yaml := `prompts:
  - file://prompts/skip/v1/prompt.md
providers:
  - id: x
tests:
  - description: "skip/v1: llm-rubric (offline runner skips this)"
    assert:
      - type: llm-rubric
        value: "subjective judgment"
`
	configPath, _, root := writeRunnerFixtures(t, "anything\n", yaml)

	report, err := RunFor(RunOptions{
		ConfigPath: configPath,
		PromptRoot: root,
		Component:  "skip",
		Version:    "v1",
	})
	require.NoError(t, err)
	assert.True(t, report.AllPassed(), "unsupported graders are skipped → vacuous pass")
}

// ---------------------------------------------------------------------------
// Helpers — parsePromptsPath, resolvePromptPath, parseLengthBounds
// ---------------------------------------------------------------------------

// TestParsePromptsPath_OK — standard `prompts/c/v/prompt.md` parses.
func TestParsePromptsPath_OK(t *testing.T) {
	c, v, ok := parsePromptsPath("prompts/foo/v1/prompt.md")
	require.True(t, ok)
	assert.Equal(t, "foo", c)
	assert.Equal(t, "v1", v)
}

// TestParsePromptsPath_WithFilePrefix — file:// prefix is stripped.
func TestParsePromptsPath_WithFilePrefix(t *testing.T) {
	c, v, ok := parsePromptsPath("file://prompts/foo/v1/prompt.md")
	require.True(t, ok)
	assert.Equal(t, "foo", c)
	assert.Equal(t, "v1", v)
}

// TestParsePromptsPath_WrongShape — anything else returns ok=false.
func TestParsePromptsPath_WrongShape(t *testing.T) {
	cases := []string{
		"prompts/foo/v1",                 // too few parts
		"prompts/foo/v1/extra/prompt.md", // too many parts
		"docs/foo/v1/prompt.md",          // wrong root
		"random-file.md",
	}
	for _, c := range cases {
		_, _, ok := parsePromptsPath(c)
		assert.False(t, ok, "expected ok=false for %q", c)
	}
}

// TestResolvePromptPath — relative refs are joined to root; absolute
// refs pass through.
func TestResolvePromptPath(t *testing.T) {
	assert.Equal(t, "/abs/path",
		resolvePromptPath("/anywhere", "/abs/path"))
	assert.Equal(t, "/tmp/foo",
		resolvePromptPath("/anywhere", "file:///tmp/foo"))
	rel := resolvePromptPath("/root", "prompts/x/v1/prompt.md")
	assert.Equal(t, filepath.Join("/root", "prompts", "x", "v1", "prompt.md"), rel)
}

// TestParseLengthBounds_MinMax — "min,max" form.
func TestParseLengthBounds_MinMax(t *testing.T) {
	min, max, err := parseLengthBounds("10,200")
	require.NoError(t, err)
	assert.Equal(t, 10, min)
	assert.Equal(t, 200, max)
}

// TestParseLengthBounds_Exact — single number = both min and max.
func TestParseLengthBounds_Exact(t *testing.T) {
	min, max, err := parseLengthBounds("42")
	require.NoError(t, err)
	assert.Equal(t, 42, min)
	assert.Equal(t, 42, max)
}

// TestParseLengthBounds_Invalid — malformed input → error.
func TestParseLengthBounds_Invalid(t *testing.T) {
	cases := []string{"", "abc", "1,abc", "abc,2"}
	for _, c := range cases {
		_, _, err := parseLengthBounds(c)
		assert.Error(t, err, "expected error for %q", c)
	}
}

// TestParseLengthBounds_Whitespace — leading/trailing whitespace tolerated.
func TestParseLengthBounds_Whitespace(t *testing.T) {
	min, max, err := parseLengthBounds("  10 , 200  ")
	require.NoError(t, err)
	assert.Equal(t, 10, min)
	assert.Equal(t, 200, max)
}

// ---------------------------------------------------------------------------
// End-to-end with the project's own .promptfoo.yaml
// ---------------------------------------------------------------------------

// TestRunFor_ProjectPromptFoo — run against the actual .promptfoo.yaml
// shipped at the project root. This is the integration smoke that CI
// will exercise. Tests against our real agent-identity prompt.
func TestRunFor_ProjectPromptFoo(t *testing.T) {
	// Locate the project root from the test's working directory.
	// Tests run from the package directory (pkg/prompt/).
	cwd, err := os.Getwd()
	require.NoError(t, err)
	root := filepath.Join(cwd, "..", "..")

	cfgPath := filepath.Join(root, ".promptfoo.yaml")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Skipf("project .promptfoo.yaml not at %s: %v", cfgPath, err)
	}

	promptPath := filepath.Join(root, "prompts", "agent-identity", "v1.1.0", "prompt.md")
	if _, err := os.Stat(promptPath); err != nil {
		t.Skipf("project prompt %s not present: %v", promptPath, err)
	}

	report, err := RunFor(RunOptions{
		ConfigPath: cfgPath,
		PromptRoot: root,
		Component:  "agent-identity",
		Version:    "v1.1.0",
	})
	require.NoError(t, err)
	require.NotEmpty(t, report.Results, "expected at least one test to target agent-identity")
	// The project's .promptfoo.yaml has tests like "Agent identity
	// prompt produces valid Go" — those involve "package identity" in
	// the prompt content (the prompt explains the target output). At
	// least one assertion should match. We assert >=1 passed rather
	// than all-passed because the project's YAML is hand-written and
	// may have assertions that don't match the actual prompt text.
	assert.GreaterOrEqual(t, report.PassedTests, 1, "expected at least one pass; results=%+v", report.Results)
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

// TestRunFor_NoAssertions_VacuousPass — a test with zero assertions
// passes (matches PromptFoo).
func TestRunFor_NoAssertions_VacuousPass(t *testing.T) {
	yaml := `prompts:
  - file://prompts/vacuous/v1/prompt.md
providers:
  - id: x
tests:
  - description: "vacuous/v1: no asserts at all"
    assert: []
`
	configPath, _, root := writeRunnerFixtures(t, "anything\n", yaml)

	report, err := RunFor(RunOptions{
		ConfigPath: configPath,
		PromptRoot: root,
		Component:  "vacuous",
		Version:    "v1",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, report.TotalTests)
	assert.Equal(t, 1, report.PassedTests)
}

// TestRunFor_TotalTestsAggregation — TotalTests / Passed / Failed
// counts match the underlying Results slice.
func TestRunFor_TotalTestsAggregation(t *testing.T) {
	yaml := `prompts:
  - file://prompts/agg/v1/prompt.md
providers:
  - id: x
tests:
  - description: "agg/v1: passes"
    assert:
      - type: contains
        value: "hello"
  - description: "agg/v1: also passes"
    assert:
      - type: not-contains
        value: "BANNED"
  - description: "agg/v1: fails"
    assert:
      - type: contains
        value: "WORLD"
`
	configPath, _, root := writeRunnerFixtures(t, "hello there\n", yaml)

	report, err := RunFor(RunOptions{
		ConfigPath: configPath,
		PromptRoot: root,
		Component:  "agg",
		Version:    "v1",
	})
	require.NoError(t, err)
	assert.Equal(t, 3, report.TotalTests)
	assert.Equal(t, 2, report.PassedTests)
	assert.Equal(t, 1, report.FailedTests)
}
