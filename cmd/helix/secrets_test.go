package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Helpers
// =============================================================================

// writeFile is a tiny helper that writes content to dir/<name>, returning
// the absolute path. Any failure aborts the test.
func writeSecretTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

// fixtureSecretFile writes a single file with a known secret. Returns
// the directory created so callers can clean up.
func fixtureSecretFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeSecretTestFile(t, dir, "leak.go",
		`package leak
const OpenRouterKey = "sk-or-v1-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`)
	return dir
}

// fixtureCleanFile writes a file with NO secrets.
func fixtureCleanFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeSecretTestFile(t, dir, "safe.go",
		`package safe
const Greeting = "hello world"`)
	return dir
}

// =============================================================================
// severityForRule + parseSeverity
// =============================================================================

func TestSeverityForRule_KnownRules(t *testing.T) {
	cases := []struct {
		rule string
		want severityLevel
	}{
		{"openrouter-key", sevHigh},
		{"github-pat", sevHigh},
		{"env-assignment", sevMed},
		{"private-key", sevCritical},
		{"unknown-rule", sevHigh}, // default → high
	}
	for _, c := range cases {
		assert.Equal(t, c.want, severityForRule(c.rule), "rule=%s", c.rule)
	}
}

func TestSeverityRank_Ordering(t *testing.T) {
	assert.Equal(t, 0, sevLow.rank())
	assert.Equal(t, 1, sevMed.rank())
	assert.Equal(t, 2, sevHigh.rank())
	assert.Equal(t, 3, sevCritical.rank())
	// Unknown severity defaults to high (rank 2).
	assert.Equal(t, 2, severityLevel("garbage").rank())
}

func TestParseSeverity(t *testing.T) {
	cases := []struct {
		in   string
		want severityLevel
	}{
		{"low", sevLow},
		{"LOW", sevLow},
		{"  medium ", sevMed},
		{"med", sevMed},
		{"high", sevHigh},
		{"HIGH", sevHigh},
		{"critical", sevCritical},
		{"crit", sevCritical},
	}
	for _, c := range cases {
		got, err := parseSeverity(c.in)
		require.NoError(t, err, c.in)
		assert.Equal(t, c.want, got, c.in)
	}

	// Failures.
	for _, bad := range []string{"", "extreme", "minor"} {
		_, err := parseSeverity(bad)
		assert.Error(t, err, bad)
	}
}

// =============================================================================
// Glob excluder
// =============================================================================

func TestGlobExcluder_EmptyIsNoOp(t *testing.T) {
	g, err := newGlobExcluder(nil)
	require.NoError(t, err)
	assert.False(t, g.excludes("/anything.txt"))
}

func TestGlobExcluder_BasenameMatch(t *testing.T) {
	g, err := newGlobExcluder([]string{"*.bak"})
	require.NoError(t, err)
	assert.True(t, g.excludes("/tmp/foo.bak"))
	assert.False(t, g.excludes("/tmp/foo.go"))
}

func TestGlobExcluder_FullPathMatch(t *testing.T) {
	g, err := newGlobExcluder([]string{"secrets/*"})
	require.NoError(t, err)
	assert.True(t, g.excludes("secrets/key.txt"))
	assert.False(t, g.excludes("public/readme.md"))
}

func TestGlobExcluder_MultiplePatterns(t *testing.T) {
	g, err := newGlobExcluder([]string{"*.bak", "vendor/*", "*.tmp"})
	require.NoError(t, err)
	assert.True(t, g.excludes("a.bak"))
	assert.True(t, g.excludes("vendor/lib.go"), "vendor/* should match files directly under vendor/")
	assert.True(t, g.excludes("x.tmp"))
	assert.False(t, g.excludes("main.go"))
	assert.False(t, g.excludes("vendor/sub/lib.go"), "vendor/* is single-segment; subdir not matched")
}

func TestGlobExcluder_InvalidGlob(t *testing.T) {
	_, err := newGlobExcluder([]string{"["}) // '[' without matching ']' → invalid
	assert.Error(t, err)
}

func TestGlobExcluder_EmptyAndWhitespacePats(t *testing.T) {
	g, err := newGlobExcluder([]string{"", "   ", "*.bak"})
	require.NoError(t, err)
	assert.Equal(t, 1, len(g.patterns), "whitespace entries should be skipped")
	assert.True(t, g.excludes("x.bak"))
}

// =============================================================================
// parseSecretFlags
// =============================================================================

func TestParseSecretFlags_Defaults(t *testing.T) {
	var buf bytes.Buffer
	f, err := parseSecretFlags([]string{"scan", "/tmp/x"}, &buf, &buf)
	require.NoError(t, err)
	assert.Equal(t, "scan", f.subcommand)
	assert.Equal(t, "/tmp/x", f.path)
	assert.Equal(t, "table", f.format)
	assert.Equal(t, sevLow, f.minSeverity)
	assert.False(t, f.quiet)
}

func TestParseSecretFlags_AllFlags(t *testing.T) {
	var buf bytes.Buffer
	f, err := parseSecretFlags([]string{"scan", "--exclude", "*.bak", "--exclude", "vendor/*",
		"--min-severity", "high", "--format", "json", "--quiet", "/tmp/x"}, &buf, &buf)
	require.NoError(t, err)
	assert.Equal(t, "scan", f.subcommand)
	assert.Equal(t, "/tmp/x", f.path)
	assert.Equal(t, []string{"*.bak", "vendor/*"}, f.excludes)
	assert.Equal(t, sevHigh, f.minSeverity)
	assert.Equal(t, "json", f.format)
	assert.True(t, f.quiet)
}

func TestParseSecretFlags_HelpShorthand(t *testing.T) {
	var buf bytes.Buffer
	f, err := parseSecretFlags([]string{"help"}, &buf, &buf)
	require.NoError(t, err)
	assert.True(t, f.help)
}

func TestParseSecretFlags_UnknownSubcommand(t *testing.T) {
	var buf bytes.Buffer
	_, err := parseSecretFlags([]string{"wat"}, &buf, &buf)
	assert.Error(t, err)
}

func TestParseSecretFlags_MissingPath(t *testing.T) {
	var buf bytes.Buffer
	_, err := parseSecretFlags([]string{"scan"}, &buf, &buf)
	assert.Error(t, err)
}

func TestParseSecretFlags_ExtraArgs(t *testing.T) {
	var buf bytes.Buffer
	_, err := parseSecretFlags([]string{"scan", "/tmp/x", "extra"}, &buf, &buf)
	assert.Error(t, err)
}

func TestParseSecretFlags_InvalidFormat(t *testing.T) {
	var buf bytes.Buffer
	_, err := parseSecretFlags([]string{"scan", "--format", "yaml", "/tmp/x"}, &buf, &buf)
	assert.Error(t, err)
}

func TestParseSecretFlags_InvalidSeverity(t *testing.T) {
	var buf bytes.Buffer
	_, err := parseSecretFlags([]string{"scan", "--min-severity", "extreme", "/tmp/x"}, &buf, &buf)
	assert.Error(t, err)
}

func TestParseSecretFlags_InvalidExclude(t *testing.T) {
	var buf bytes.Buffer
	_, err := parseSecretFlags([]string{"scan", "--exclude", "[", "/tmp/x"}, &buf, &buf)
	assert.Error(t, err)
}

// =============================================================================
// Env-var defaults
// =============================================================================

func TestParseSecretFlags_EnvOverrides(t *testing.T) {
	t.Setenv(envSecretsExclude, "*.bak,vendor/*")
	t.Setenv(envSecretsMinSeverity, "high")
	t.Setenv(envSecretsFormat, "json")
	t.Setenv(envSecretsQuiet, "true")

	var buf bytes.Buffer
	f, err := parseSecretFlags([]string{"scan", "/tmp/x"}, &buf, &buf)
	require.NoError(t, err)
	assert.Equal(t, []string{"*.bak", "vendor/*"}, f.excludes)
	assert.Equal(t, sevHigh, f.minSeverity)
	assert.Equal(t, "json", f.format)
	assert.True(t, f.quiet)
}

func TestParseSecretFlags_FlagsOverrideEnv(t *testing.T) {
	t.Setenv(envSecretsMinSeverity, "high")
	t.Setenv(envSecretsFormat, "json")

	var buf bytes.Buffer
	f, err := parseSecretFlags([]string{"scan", "--min-severity", "low", "--format", "table", "/tmp/x"}, &buf, &buf)
	require.NoError(t, err)
	assert.Equal(t, sevLow, f.minSeverity, "flag should beat env")
	assert.Equal(t, "table", f.format, "flag should beat env")
}

// =============================================================================
// stringSliceFlag
// =============================================================================

func TestStringSliceFlag_Set(t *testing.T) {
	var f stringSliceFlag
	require.NoError(t, f.Set("a"))
	require.NoError(t, f.Set("b"))
	assert.True(t, f.set)
	assert.Equal(t, []string{"a", "b"}, f.values)
	assert.Equal(t, "a,b", f.String())
}

// =============================================================================
// runSecrets end-to-end
// =============================================================================

func TestRunSecrets_Help(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runSecrets([]string{"help"}, &stdout, &stderr)
	assert.Equal(t, 0, rc)
	assert.Contains(t, stdout.String(), "Usage:")
}

func TestRunSecrets_UnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runSecrets([]string{"wat"}, &stdout, &stderr)
	assert.Equal(t, 2, rc)
}

func TestRunSecrets_Scan_Clean(t *testing.T) {
	dir := fixtureCleanFile(t)
	var stdout, stderr bytes.Buffer
	rc := runSecrets([]string{"scan", dir}, &stdout, &stderr)
	assert.Equal(t, 0, rc)
	assert.Contains(t, stdout.String(), "(no secrets detected)")
}

func TestRunSecrets_Scan_Detects(t *testing.T) {
	dir := fixtureSecretFile(t)
	var stdout, stderr bytes.Buffer
	rc := runSecrets([]string{"scan", dir}, &stdout, &stderr)
	assert.Equal(t, 1, rc)
	assert.Contains(t, stdout.String(), "openrouter-key")
}

func TestRunSecrets_Scan_MissingPath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runSecrets([]string{"scan", "/nonexistent/path/abc123"}, &stdout, &stderr)
	assert.Equal(t, 2, rc, "missing path should be invocation error")
}

func TestRunSecrets_Scan_SeverityFilter(t *testing.T) {
	dir := fixtureSecretFile(t) // openrouter-key → sevHigh
	var stdout, stderr bytes.Buffer
	// min-severity=critical → openrouter-key (high) filtered out
	rc := runSecrets([]string{"scan", "--min-severity", "critical", dir}, &stdout, &stderr)
	assert.Equal(t, 0, rc)
	assert.Contains(t, stdout.String(), "(no secrets detected)")
}

func TestRunSecrets_Scan_Excludes(t *testing.T) {
	dir := fixtureSecretFile(t)
	var stdout, stderr bytes.Buffer
	// Exclude *.go → secret file skipped
	rc := runSecrets([]string{"scan", "--exclude", "*.go", dir}, &stdout, &stderr)
	assert.Equal(t, 0, rc)
}

func TestRunSecrets_Scan_ExcludeByBasename(t *testing.T) {
	dir := t.TempDir()
	// File that should match the exclusion
	writeSecretTestFile(t, dir, "nope.bak",
		`package bak
const OpenRouterKey = "sk-or-v1-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"`)
	// File that should NOT match
	writeSecretTestFile(t, dir, "leak.go",
		`package leak
const OpenRouterKey = "sk-or-v1-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`)

	var stdout, stderr bytes.Buffer
	rc := runSecrets([]string{"scan", "--exclude", "*.bak", dir}, &stdout, &stderr)
	assert.Equal(t, 1, rc, "should find only leak.go")
	assert.Contains(t, stdout.String(), "leak.go")
	assert.NotContains(t, stdout.String(), "nope.bak",
		"nope.bak should have been excluded by basename pattern")
}

func TestRunSecrets_Scan_Json(t *testing.T) {
	dir := fixtureSecretFile(t)
	var stdout, stderr bytes.Buffer
	rc := runSecrets([]string{"scan", "--format", "json", dir}, &stdout, &stderr)
	assert.Equal(t, 1, rc)

	var doc struct {
		Path     string `json:"path"`
		Files    int    `json:"files"`
		Findings []struct {
			Rule     string `json:"rule"`
			Severity string `json:"severity"`
			File     string `json:"file"`
			Line     int    `json:"line"`
			Column   int    `json:"column"`
			Snippet  string `json:"snippet"`
		} `json:"findings"`
	}
	err := json.Unmarshal(stdout.Bytes(), &doc)
	require.NoError(t, err)
	assert.Equal(t, dir, doc.Path)
	require.Len(t, doc.Findings, 1)
	assert.Equal(t, "openrouter-key", doc.Findings[0].Rule)
	assert.Equal(t, "high", doc.Findings[0].Severity)
	assert.GreaterOrEqual(t, doc.Findings[0].Line, 1)
}

func TestRunSecrets_Scan_Quiet(t *testing.T) {
	dir := fixtureSecretFile(t)
	var stdout, stderr bytes.Buffer
	rc := runSecrets([]string{"scan", "--quiet", dir}, &stdout, &stderr)
	assert.Equal(t, 1, rc)
	assert.Contains(t, stdout.String(), "Findings: 1")
	// No per-finding detail line in quiet mode.
	assert.NotContains(t, stdout.String(), "openrouter-key")
}

func TestRunSecrets_Scan_SingleFile(t *testing.T) {
	dir := fixtureSecretFile(t)
	p := filepath.Join(dir, "leak.go")
	var stdout, stderr bytes.Buffer
	rc := runSecrets([]string{"scan", p}, &stdout, &stderr)
	assert.Equal(t, 1, rc)
	assert.Contains(t, stdout.String(), "openrouter-key")
}

func TestRunSecrets_Scan_SingleFileMissing(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runSecrets([]string{"scan", "/no/such/file/abc.xyz"}, &stdout, &stderr)
	assert.Equal(t, 2, rc)
}

func TestRunSecrets_Scan_ParseErrorInvExitCode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runSecrets([]string{"scan"}, &stdout, &stderr) // no path
	assert.Equal(t, 2, rc)
	assert.Contains(t, stderr.String(), "missing required PATH")
}

func TestRunSecrets_WithDryRun(t *testing.T) {
	dir := fixtureSecretFile(t)
	var stdout, stderr bytes.Buffer
	err := runSecretsWithDryRun([]string{"scan", dir}, &stdout, &stderr, true)
	// errExit wraps non-zero; verify the wrapper is returned.
	require.Error(t, err)
	ee, ok := err.(errExit)
	require.True(t, ok, "expected errExit, got %T", err)
	assert.Equal(t, 1, ee.code)
}

func TestRunSecrets_WithDryRun_Clean(t *testing.T) {
	dir := fixtureCleanFile(t)
	var stdout, stderr bytes.Buffer
	err := runSecretsWithDryRun([]string{"scan", dir}, &stdout, &stderr, true)
	assert.NoError(t, err)
}

// =============================================================================
// list-rules
// =============================================================================

func TestRunSecrets_ListRules(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runSecrets([]string{"list-rules"}, &stdout, &stderr)
	assert.Equal(t, 0, rc)
	// Output is JSON
	text := stdout.String()
	assert.True(t, strings.HasPrefix(text, "["), "expected JSON array, got: %q", text)
	assert.Contains(t, text, "openrouter-key")
	assert.Contains(t, text, "severity")
	// Must be valid JSON.
	var rows []map[string]string
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &rows))
	assert.GreaterOrEqual(t, len(rows), 4)
}

// =============================================================================
// countFiles
// =============================================================================

func TestCountFiles_SingleFile(t *testing.T) {
	dir := t.TempDir()
	p := writeSecretTestFile(t, dir, "x.go", "package x")
	assert.Equal(t, 1, countFiles(p))
}

func TestCountFiles_Directory(t *testing.T) {
	dir := t.TempDir()
	writeSecretTestFile(t, dir, "a.go", "a")
	writeSecretTestFile(t, dir, "b.go", "b")
	writeSecretTestFile(t, dir, "subdir/c.go", "c")
	got := countFiles(dir)
	assert.Equal(t, 3, got)
}

func TestCountFiles_Nonexistent(t *testing.T) {
	assert.Equal(t, 0, countFiles("/no/such/place"))
}

func TestCountFiles_SkipsHiddenAndVenv(t *testing.T) {
	dir := t.TempDir()
	writeSecretTestFile(t, dir, "a.go", "a")
	writeSecretTestFile(t, dir, ".git/x.go", "x")         // hidden
	writeSecretTestFile(t, dir, ".venv/x.go", "x")        // venv
	writeSecretTestFile(t, dir, "node_modules/x.go", "x") // node
	got := countFiles(dir)
	assert.Equal(t, 1, got)
}

// =============================================================================
// scanWithExcludes
// =============================================================================

func TestScanWithExcludes_File(t *testing.T) {
	dir := fixtureSecretFile(t)
	p := filepath.Join(dir, "leak.go")
	g, err := newGlobExcluder([]string{"*.nope"})
	require.NoError(t, err)
	findings, err := scanWithExcludes(p, g)
	require.NoError(t, err)
	assert.Len(t, findings, 1, "exclusion shouldn't apply to explicit single file")
}

func TestScanWithExcludes_DirExclusion(t *testing.T) {
	dir := t.TempDir()
	writeSecretTestFile(t, dir, "a.go",
		`package a
const OpenRouterKey = "sk-or-v1-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`)
	g, err := newGlobExcluder([]string{"*.go"})
	require.NoError(t, err)
	findings, err := scanWithExcludes(dir, g)
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestScanWithExcludes_MissingPath(t *testing.T) {
	g, err := newGlobExcluder(nil)
	require.NoError(t, err)
	_, err = scanWithExcludes("/no/such", g)
	assert.Error(t, err)
}
