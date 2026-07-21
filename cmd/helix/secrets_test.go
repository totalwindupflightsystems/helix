package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	fage "filippo.io/age"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/totalwindupflightsystems/helix/pkg/config"
	"github.com/totalwindupflightsystems/helix/pkg/security/store"
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

// =============================================================================
// secrets_crud.go — parseCrudFlags
// =============================================================================

func TestParseCrudFlags_Set(t *testing.T) {
	var buf bytes.Buffer
	f, err := parseCrudFlags([]string{"set", "foo", "bar"}, &buf, &buf)
	require.NoError(t, err)
	assert.Equal(t, "set", f.subcommand)
	assert.Equal(t, "foo", f.key)
	assert.Equal(t, "bar", f.value)
}

func TestParseCrudFlags_Get(t *testing.T) {
	var buf bytes.Buffer
	f, err := parseCrudFlags([]string{"get", "foo"}, &buf, &buf)
	require.NoError(t, err)
	assert.Equal(t, "get", f.subcommand)
	assert.Equal(t, "foo", f.key)
}

func TestParseCrudFlags_Delete(t *testing.T) {
	var buf bytes.Buffer
	f, err := parseCrudFlags([]string{"delete", "foo"}, &buf, &buf)
	require.NoError(t, err)
	assert.Equal(t, "delete", f.subcommand)
	assert.Equal(t, "foo", f.key)
}

func TestParseCrudFlags_List(t *testing.T) {
	var buf bytes.Buffer
	f, err := parseCrudFlags([]string{"list"}, &buf, &buf)
	require.NoError(t, err)
	assert.Equal(t, "list", f.subcommand)
}

func TestParseCrudFlags_Rotate(t *testing.T) {
	var buf bytes.Buffer
	f, err := parseCrudFlags([]string{"rotate", "/tmp/newkey.txt"}, &buf, &buf)
	require.NoError(t, err)
	assert.Equal(t, "rotate", f.subcommand)
	assert.Equal(t, "/tmp/newkey.txt", f.newKeyPath)
}

func TestParseCrudFlags_Init(t *testing.T) {
	var buf bytes.Buffer
	f, err := parseCrudFlags([]string{"init"}, &buf, &buf)
	require.NoError(t, err)
	assert.Equal(t, "init", f.subcommand)
}

func TestParseCrudFlags_Set_MissingValue(t *testing.T) {
	var buf bytes.Buffer
	_, err := parseCrudFlags([]string{"set", "foo"}, &buf, &buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "<key> <value>")
}

func TestParseCrudFlags_Get_MissingKey(t *testing.T) {
	var buf bytes.Buffer
	_, err := parseCrudFlags([]string{"get"}, &buf, &buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "<key>")
}

func TestParseCrudFlags_Delete_MissingKey(t *testing.T) {
	var buf bytes.Buffer
	_, err := parseCrudFlags([]string{"delete"}, &buf, &buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "<key>")
}

func TestParseCrudFlags_Rotate_MissingPath(t *testing.T) {
	var buf bytes.Buffer
	_, err := parseCrudFlags([]string{"rotate"}, &buf, &buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "<new-key-path>")
}

func TestParseCrudFlags_Set_ExtraArgs(t *testing.T) {
	var buf bytes.Buffer
	_, err := parseCrudFlags([]string{"set", "a", "b", "c"}, &buf, &buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected extra arguments")
}

func TestParseCrudFlags_List_ExtraArgs(t *testing.T) {
	var buf bytes.Buffer
	_, err := parseCrudFlags([]string{"list", "extra"}, &buf, &buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected arguments")
}

func TestParseCrudFlags_Init_ExtraArgs(t *testing.T) {
	var buf bytes.Buffer
	_, err := parseCrudFlags([]string{"init", "extra"}, &buf, &buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected arguments")
}

func TestParseCrudFlags_Flags(t *testing.T) {
	var buf bytes.Buffer
	f, err := parseCrudFlags([]string{"get", "--provider", "sops", "--store", "/tmp/s.yaml",
		"--key-path", "/tmp/k.txt", "foo"}, &buf, &buf)
	require.NoError(t, err)
	assert.Equal(t, "sops", f.provider)
	assert.Equal(t, "/tmp/s.yaml", f.storePath)
	assert.Equal(t, "/tmp/k.txt", f.keyPath)
	assert.Equal(t, "foo", f.key)
}

func TestParseCrudFlags_EnvDefaults(t *testing.T) {
	t.Setenv(envSecretsStorePath, "/env/store.yaml")
	t.Setenv(envSecretsKeyPath, "/env/key.txt")
	t.Setenv(envSecretsProvider, "sops")

	var buf bytes.Buffer
	f, err := parseCrudFlags([]string{"get", "foo"}, &buf, &buf)
	require.NoError(t, err)
	assert.Equal(t, "sops", f.provider)
	assert.Equal(t, "/env/store.yaml", f.storePath)
	assert.Equal(t, "/env/key.txt", f.keyPath)
}

func TestParseCrudFlags_FlagsOverrideEnv(t *testing.T) {
	t.Setenv(envSecretsProvider, "env")

	var buf bytes.Buffer
	f, err := parseCrudFlags([]string{"get", "--provider", "sops", "foo"}, &buf, &buf)
	require.NoError(t, err)
	assert.Equal(t, "sops", f.provider, "--provider must beat env")
}

func TestParseCrudFlags_Empty(t *testing.T) {
	var buf bytes.Buffer
	_, err := parseCrudFlags(nil, &buf, &buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing secrets subcommand")
}

// =============================================================================
// resolveSecretsConfig
// =============================================================================

func TestResolveSecretsConfig_Defaults(t *testing.T) {
	provider, storePath, keyPath := resolveSecretsConfig(crudFlags{}, nil)
	assert.Equal(t, "env", provider)
	assert.Contains(t, storePath, "secrets.enc.yaml")
	assert.Contains(t, keyPath, "age.txt")
}

func TestResolveSecretsConfig_ConfigWins(t *testing.T) {
	cfg := &config.Config{
		Secrets: config.SecretsConfig{
			Provider:    "sops",
			StorePath:   "/cfg/store.yaml",
			SOPSKeyPath: "/cfg/key.txt",
		},
	}
	provider, storePath, keyPath := resolveSecretsConfig(crudFlags{}, cfg)
	assert.Equal(t, "sops", provider)
	assert.Equal(t, "/cfg/store.yaml", storePath)
	assert.Equal(t, "/cfg/key.txt", keyPath)
}

func TestResolveSecretsConfig_FlagWins(t *testing.T) {
	cfg := &config.Config{
		Secrets: config.SecretsConfig{
			Provider: "sops", StorePath: "/cfg/s.yaml", SOPSKeyPath: "/cfg/k.txt",
		},
	}
	f := crudFlags{provider: "env", storePath: "/flag/s.yaml", keyPath: "/flag/k.txt"}
	provider, storePath, keyPath := resolveSecretsConfig(f, cfg)
	assert.Equal(t, "env", provider)
	assert.Equal(t, "/flag/s.yaml", storePath)
	assert.Equal(t, "/flag/k.txt", keyPath)
}

// =============================================================================
// runCrud — env provider
// =============================================================================

func TestRunCrud_Env_Get(t *testing.T) {
	t.Setenv("HELIX_TEST_FOO", "bar")
	var stdout, stderr bytes.Buffer
	rc := runCrud(crudFlags{subcommand: "get", key: "HELIX_TEST_FOO"}, &stdout, &stderr)
	assert.Equal(t, 0, rc)
	assert.Equal(t, "bar\n", stdout.String())
}

func TestRunCrud_Env_Get_Missing(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runCrud(crudFlags{subcommand: "get", key: "HELIX_NOPE_DOES_NOT_EXIST"}, &stdout, &stderr)
	assert.Equal(t, 1, rc)
	assert.Contains(t, stderr.String(), "not set in environment")
}

func TestRunCrud_Env_Set_Rejected(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runCrud(crudFlags{subcommand: "set", key: "X", value: "Y"}, &stdout, &stderr)
	assert.Equal(t, 1, rc)
	assert.Contains(t, stderr.String(), "cannot persist")
}

func TestRunCrud_Env_Delete_Rejected(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runCrud(crudFlags{subcommand: "delete", key: "X"}, &stdout, &stderr)
	assert.Equal(t, 1, rc)
	assert.Contains(t, stderr.String(), "cannot unset")
}

func TestRunCrud_Env_List(t *testing.T) {
	t.Setenv("HELIX_LIST_A", "1")
	t.Setenv("HELIX_LIST_B", "2")
	var stdout, stderr bytes.Buffer
	rc := runCrud(crudFlags{subcommand: "list"}, &stdout, &stderr)
	assert.Equal(t, 0, rc)
	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	assert.Contains(t, lines, "HELIX_LIST_A")
	assert.Contains(t, lines, "HELIX_LIST_B")
}

func TestRunCrud_Env_Rotate_Rejected(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runCrud(crudFlags{subcommand: "rotate", newKeyPath: "/tmp/x"}, &stdout, &stderr)
	assert.Equal(t, 1, rc)
	assert.Contains(t, stderr.String(), "no key to rotate")
}

func TestRunCrud_Env_Init_Rejected(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runCrud(crudFlags{subcommand: "init"}, &stdout, &stderr)
	assert.Equal(t, 1, rc)
	assert.Contains(t, stderr.String(), "nothing to initialise")
}

func TestRunCrud_UnknownProvider(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runCrud(crudFlags{subcommand: "get", key: "X", provider: "vault"}, &stdout, &stderr)
	assert.Equal(t, 2, rc)
	assert.Contains(t, stderr.String(), "unsupported provider")
}

func TestRunCrud_Help(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runCrud(crudFlags{subcommand: "get", showHelp: true}, &stdout, &stderr)
	assert.Equal(t, 0, rc)
	assert.Contains(t, stdout.String(), "Usage:")
}

// =============================================================================
// runCrud — sops provider (real age keys, real SOPS)
// =============================================================================

// writeTestAgeKey generates a fresh age identity at path and returns
// the identity. Mirrors the helper in pkg/security/store/sops_test.go
// but lives here because this is package main.
func writeTestAgeKey(t *testing.T, path string) {
	t.Helper()
	id, err := fage.GenerateX25519Identity()
	require.NoError(t, err, "generate age identity")
	contents := "# created: 2026-07-21T17:00:00Z\n" +
		"# public key: " + id.Recipient().String() + "\n" +
		id.String() + "\n"
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
}

// newSOPSCrudFixture builds a CRUD-ready test harness: a temp dir
// containing an age key + empty encrypted store, SOPS_AGE_KEY_FILE
// pointing at the key, and the CRUD flags wired to use them.
func newSOPSCrudFixture(t *testing.T) (storePath, keyPath string) {
	t.Helper()
	dir := t.TempDir()
	keyPath = filepath.Join(dir, "age.txt")
	storePath = filepath.Join(dir, "secrets.enc.yaml")

	writeTestAgeKey(t, keyPath)
	t.Setenv("SOPS_AGE_KEY_FILE", keyPath)

	// Seed an empty encrypted store so the SOPS smoke-test decrypt
	// at NewSOPSStore succeeds. Use the store package directly.
	s, err := store.NewSOPSStore(storePath, keyPath)
	require.NoError(t, err)
	// Set a marker and delete it to force the initial file write.
	require.NoError(t, s.Set(context.Background(), "__init__", ""))
	require.NoError(t, s.Delete(context.Background(), "__init__"))
	return storePath, keyPath
}

func TestRunCrud_SOPS_SetGet(t *testing.T) {
	storePath, keyPath := newSOPSCrudFixture(t)

	var stdout, stderr bytes.Buffer
	cf := crudFlags{
		subcommand: "set",
		key:        "forgejo_token",
		value:      "sk-or-v1-abc",
		provider:   "sops",
		storePath:  storePath,
		keyPath:    keyPath,
	}
	rc := runCrud(cf, &stdout, &stderr)
	require.Equal(t, 0, rc, "set failed: %s", stderr.String())
	assert.Contains(t, stdout.String(), "set forgejo_token")

	// Get it back.
	var stdout2, stderr2 bytes.Buffer
	cf = crudFlags{subcommand: "get", key: "forgejo_token", provider: "sops",
		storePath: storePath, keyPath: keyPath}
	rc = runCrud(cf, &stdout2, &stderr2)
	require.Equal(t, 0, rc, "get failed: %s", stderr2.String())
	assert.Equal(t, "sk-or-v1-abc\n", stdout2.String())
}

func TestRunCrud_SOPS_Get_NotFound(t *testing.T) {
	storePath, keyPath := newSOPSCrudFixture(t)
	var stdout, stderr bytes.Buffer
	cf := crudFlags{subcommand: "get", key: "does_not_exist", provider: "sops",
		storePath: storePath, keyPath: keyPath}
	rc := runCrud(cf, &stdout, &stderr)
	assert.Equal(t, 1, rc)
	assert.Contains(t, stderr.String(), "not found")
}

func TestRunCrud_SOPS_Delete(t *testing.T) {
	storePath, keyPath := newSOPSCrudFixture(t)

	// Seed a secret.
	require.NoError(t, runCrudSilent(crudFlags{subcommand: "set", key: "k1", value: "v1",
		provider: "sops", storePath: storePath, keyPath: keyPath}))

	var stdout, stderr bytes.Buffer
	rc := runCrud(crudFlags{subcommand: "delete", key: "k1", provider: "sops",
		storePath: storePath, keyPath: keyPath}, &stdout, &stderr)
	assert.Equal(t, 0, rc, "delete: %s", stderr.String())
	assert.Contains(t, stdout.String(), "deleted k1")

	// Follow-up get must miss.
	var stdout2, stderr2 bytes.Buffer
	rc = runCrud(crudFlags{subcommand: "get", key: "k1", provider: "sops",
		storePath: storePath, keyPath: keyPath}, &stdout2, &stderr2)
	assert.Equal(t, 1, rc)
}

func TestRunCrud_SOPS_Delete_Idempotent(t *testing.T) {
	storePath, keyPath := newSOPSCrudFixture(t)
	var stdout, stderr bytes.Buffer
	// Delete a key that was never set.
	rc := runCrud(crudFlags{subcommand: "delete", key: "never_set", provider: "sops",
		storePath: storePath, keyPath: keyPath}, &stdout, &stderr)
	assert.Equal(t, 0, rc, "idempotent delete should succeed: %s", stderr.String())
	assert.Contains(t, stdout.String(), "deleted never_set")
}

func TestRunCrud_SOPS_List(t *testing.T) {
	storePath, keyPath := newSOPSCrudFixture(t)
	require.NoError(t, runCrudSilent(crudFlags{subcommand: "set", key: "alpha", value: "1",
		provider: "sops", storePath: storePath, keyPath: keyPath}))
	require.NoError(t, runCrudSilent(crudFlags{subcommand: "set", key: "beta", value: "2",
		provider: "sops", storePath: storePath, keyPath: keyPath}))

	var stdout, stderr bytes.Buffer
	rc := runCrud(crudFlags{subcommand: "list", provider: "sops",
		storePath: storePath, keyPath: keyPath}, &stdout, &stderr)
	require.Equal(t, 0, rc, "list: %s", stderr.String())
	keys := strings.Fields(stdout.String())
	assert.Contains(t, keys, "alpha")
	assert.Contains(t, keys, "beta")
	// SOPS store also leaves the __init__ marker? No — we deleted it
	// in newSOPSCrudFixture. Sanity: at least alpha + beta present.
	assert.GreaterOrEqual(t, len(keys), 2)
}

func TestRunCrud_SOPS_SetUpdate(t *testing.T) {
	storePath, keyPath := newSOPSCrudFixture(t)
	require.NoError(t, runCrudSilent(crudFlags{subcommand: "set", key: "k", value: "v1",
		provider: "sops", storePath: storePath, keyPath: keyPath}))
	require.NoError(t, runCrudSilent(crudFlags{subcommand: "set", key: "k", value: "v2",
		provider: "sops", storePath: storePath, keyPath: keyPath}))

	var stdout, stderr bytes.Buffer
	rc := runCrud(crudFlags{subcommand: "get", key: "k", provider: "sops",
		storePath: storePath, keyPath: keyPath}, &stdout, &stderr)
	require.Equal(t, 0, rc)
	assert.Equal(t, "v2\n", stdout.String(), "set should have updated the value")
}

func TestRunCrud_SOPS_Rotate(t *testing.T) {
	storePath, keyPath := newSOPSCrudFixture(t)
	require.NoError(t, runCrudSilent(crudFlags{subcommand: "set", key: "secret", value: "keep-me",
		provider: "sops", storePath: storePath, keyPath: keyPath}))

	// Generate a second key for rotation target.
	newKey := filepath.Join(filepath.Dir(keyPath), "age2.txt")
	writeTestAgeKey(t, newKey)

	var stdout, stderr bytes.Buffer
	rc := runCrud(crudFlags{subcommand: "rotate", newKeyPath: newKey, provider: "sops",
		storePath: storePath, keyPath: keyPath}, &stdout, &stderr)
	require.Equal(t, 0, rc, "rotate: %s", stderr.String())
	assert.Contains(t, stdout.String(), "rotated to")

	// Point SOPS at the new key — Get should still return the value.
	t.Setenv("SOPS_AGE_KEY_FILE", newKey)
	var stdout2, stderr2 bytes.Buffer
	rc = runCrud(crudFlags{subcommand: "get", key: "secret", provider: "sops",
		storePath: storePath, keyPath: newKey}, &stdout2, &stderr2)
	require.Equal(t, 0, rc, "get after rotate: %s", stderr2.String())
	assert.Equal(t, "keep-me\n", stdout2.String())
}

func TestRunCrud_SOPS_AutoInit_Key(t *testing.T) {
	// When the age key file is absent, runCrud auto-generates one
	// before opening the store. We can't rely on age-keygen being
	// installed in CI — skip if missing.
	if _, err := exec.LookPath("age-keygen"); err != nil {
		t.Skip("age-keygen not installed; skipping auto-init test")
	}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "age.txt")
	storePath := filepath.Join(dir, "secrets.enc.yaml")

	var stdout, stderr bytes.Buffer
	rc := runCrud(crudFlags{subcommand: "list", provider: "sops",
		storePath: storePath, keyPath: keyPath}, &stdout, &stderr)
	require.Equal(t, 0, rc, "auto-init + list: %s", stderr.String())
	assert.Contains(t, stdout.String(), "generated age key", "should report auto-init")
	assert.True(t, fileExists(keyPath), "age key should have been created")
}

// runCrudSilent is a tiny helper for test fixtures that need to seed
// state without inspecting output. Returns the exit code; non-zero is
// surfaced to the caller via require.NoError in tests.
func runCrudSilent(f crudFlags) error {
	var stderr bytes.Buffer
	if rc := runCrud(f, io.Discard, &stderr); rc != 0 {
		return fmt.Errorf("runCrud %s rc=%d: %s", f.subcommand, rc, stderr.String())
	}
	return nil
}

// =============================================================================
// end-to-end dispatch (runSecrets → CRUD)
// =============================================================================

func TestRunSecrets_CRUD_SetGet_Env(t *testing.T) {
	t.Setenv("HELIX_E2E", "secret-value")
	var stdout, stderr bytes.Buffer
	rc := runSecrets([]string{"get", "HELIX_E2E"}, &stdout, &stderr)
	assert.Equal(t, 0, rc, stderr.String())
	assert.Equal(t, "secret-value\n", stdout.String())
}

func TestRunSecrets_CRUD_BadSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runSecrets([]string{"frobnicate"}, &stdout, &stderr)
	assert.Equal(t, 2, rc)
}

func TestRunSecrets_CRUD_Set_MissingValue(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runSecrets([]string{"set", "only-key"}, &stdout, &stderr)
	assert.Equal(t, 2, rc)
	assert.Contains(t, stderr.String(), "<key> <value>")
}

// =============================================================================
// expandCLIHome / fileExists helpers
// =============================================================================

func TestExpandCLIHome(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")
	assert.Equal(t, "/home/testuser/x.txt", expandCLIHome("~/x.txt"))
	assert.Equal(t, "/abs/path", expandCLIHome("/abs/path"))
	assert.Equal(t, "", expandCLIHome(""))
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	assert.False(t, fileExists(p))
	require.NoError(t, os.WriteFile(p, []byte("x"), 0o600))
	assert.True(t, fileExists(p))
}

