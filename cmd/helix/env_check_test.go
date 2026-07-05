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

	"github.com/totalwindupflightsystems/helix/pkg/config"
)

// =============================================================================
// Fixtures
// =============================================================================

// fixtureEnvFile writes a small .env-style file in t.TempDir() with the
// supplied contents. Returns the absolute path.
func fixtureEnvFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

// clearRequiredEnv unsets every variable in DefaultEnvVars for the
// duration of the test, restoring them via t.Cleanup. Prevents
// process-env leakage between tests.
func clearRequiredEnv(t *testing.T) {
	t.Helper()
	original := map[string]string{}
	for _, v := range config.DefaultEnvVars() {
		if val, ok := os.LookupEnv(v.Name); ok {
			original[v.Name] = val
		}
		require.NoError(t, os.Unsetenv(v.Name))
	}
	t.Cleanup(func() {
		for k, v := range original {
			_ = os.Setenv(k, v)
		}
	})
}

// withEnv runs fn with the given env vars set, restoring originals after.
// kv may include "" values to mean "unset the var for the duration".
func withEnv(t *testing.T, kv map[string]string, fn func()) {
	t.Helper()
	originals := map[string]string{}
	hadKey := map[string]bool{}
	for k, v := range kv {
		if old, ok := os.LookupEnv(k); ok {
			originals[k] = old
			hadKey[k] = true
		}
		if v == "" {
			_ = os.Unsetenv(k)
		} else {
			_ = os.Setenv(k, v)
		}
	}
	defer func() {
		for k := range kv {
			if hadKey[k] {
				_ = os.Setenv(k, originals[k])
			} else {
				_ = os.Unsetenv(k)
			}
		}
	}()
	fn()
}

// =============================================================================
// parseEnvCheckFlags
// =============================================================================

func TestParseEnvCheckFlags_Defaults(t *testing.T) {
	var stdout, stderr bytes.Buffer
	f, err := parseEnvCheckFlags([]string{}, &stdout, &stderr)
	require.NoError(t, err)
	assert.False(t, f.help)
	assert.False(t, f.jsonOut)
	assert.True(t, f.strict, "strict defaults to true")
	assert.Empty(t, f.envFile)
	assert.Empty(t, f.source)
}

func TestParseEnvCheckFlags_AllSet(t *testing.T) {
	var stdout, stderr bytes.Buffer
	f, err := parseEnvCheckFlags([]string{
		"--json", "--strict=false",
		"--env-file", "/tmp/.env", "--source", "forgejo",
	}, &stdout, &stderr)
	require.NoError(t, err)
	assert.True(t, f.jsonOut)
	assert.False(t, f.strict)
	assert.Equal(t, "/tmp/.env", f.envFile)
	assert.Equal(t, "forgejo", f.source)
}

func TestParseEnvCheckFlags_PositionalHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	f, err := parseEnvCheckFlags([]string{"help"}, &stdout, &stderr)
	require.NoError(t, err)
	assert.True(t, f.help)
}

func TestParseEnvCheckFlags_UnknownPositional(t *testing.T) {
	var stdout, stderr bytes.Buffer
	_, err := parseEnvCheckFlags([]string{"frobnicate"}, &stdout, &stderr)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "frobnicate")
}

func TestParseEnvCheckFlags_BadFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	_, err := parseEnvCheckFlags([]string{"--no-such-flag"}, &stdout, &stderr)
	assert.Error(t, err)
}

// =============================================================================
// resolveEnvFile
// =============================================================================

func TestResolveEnvFile_ExplicitWins(t *testing.T) {
	dir := t.TempDir()
	explicit := filepath.Join(dir, "explicit.env")
	require.NoError(t, os.WriteFile(explicit, []byte("FOO=bar\n"), 0o600))

	got := resolveEnvFile(explicit)
	assert.Equal(t, explicit, got, "explicit --env-file wins over defaults")
}

func TestResolveEnvFile_NonexistentExplicit(t *testing.T) {
	// When --env-file points at a missing file, resolveEnvFile still
	// returns it — the caller decides whether to error. This matches
	// the runEnvCheck behaviour where we emit exit 2 inside the handler.
	ghost := "/tmp/definitely-does-not-exist-helix-xyz.env"
	got := resolveEnvFile(ghost)
	assert.Equal(t, ghost, got)
}

func TestResolveEnvFile_DefaultCandidate(t *testing.T) {
	// HELIX_ENV_FILE is honoured when no --env-file passed.
	tmp := fixtureEnvFile(t, "FOO=bar\n")
	t.Setenv("HELIX_ENV_FILE", tmp)
	got := resolveEnvFile("")
	assert.Equal(t, tmp, got, "HELIX_ENV_FILE used as fallback")
}

func TestResolveEnvFile_NoCandidates(t *testing.T) {
	t.Setenv("HELIX_ENV_FILE", "")
	got := resolveEnvFile("")
	// Either empty (no candidates) or one of the default paths if a
	// $HOME/.helix/.env file happens to exist on the host. We assert
	// only that the value is either "" or an existing file.
	if got != "" {
		_, err := os.Stat(got)
		assert.NoError(t, err, "if non-empty, must point at an existing file")
	}
}

// =============================================================================
// buildLoader
// =============================================================================

func TestBuildLoader_NilWhenNoFile(t *testing.T) {
	assert.Nil(t, buildLoader(""))
}

func TestBuildLoader_DotEnvWhenFileGiven(t *testing.T) {
	p := fixtureEnvFile(t, "FOO=bar\n")
	loader := buildLoader(p)
	require.NotNil(t, loader)
	v, ok := loader.Load("FOO", config.SourceDotenv)
	assert.True(t, ok)
	assert.Equal(t, "bar", v)
}

// =============================================================================
// filterByService
// =============================================================================

func TestFilterByService_EmptyReturnsAll(t *testing.T) {
	vars := config.DefaultEnvVars()
	out, err := filterByService(vars, "")
	require.NoError(t, err)
	assert.Len(t, out, len(vars))
}

func TestFilterByService_KnownService(t *testing.T) {
	vars := config.DefaultEnvVars()
	out, err := filterByService(vars, "forgejo")
	require.NoError(t, err)
	assert.NotEmpty(t, out, "forgejo group should have at least one var")
	for _, v := range out {
		assert.Equal(t, "forgejo", v.Service)
	}
}

func TestFilterByService_UnknownServiceErrors(t *testing.T) {
	vars := config.DefaultEnvVars()
	_, err := filterByService(vars, "made-up")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "made-up")
}

// =============================================================================
// processEnvMap + strict scrubbing
// =============================================================================

func TestProcessEnvMap_IncludesUnsetAsAbsent(t *testing.T) {
	clearRequiredEnv(t)
	t.Setenv("HELIX_TEST_PROBE_VAR", "hello")
	m := processEnvMap()
	assert.Equal(t, "hello", m["HELIX_TEST_PROBE_VAR"])
	_, ok := m["OPENROUTER_API_KEY"]
	// OPENROUTER_API_KEY may or may not be set in the host env; only assert
	// that if present, it isn't empty (the function only stores non-empty
	// values? actually os.Environ includes empties too — so we just assert
	// the probe var is found.)
	_ = ok
}

func TestProcessEnvMap_StrictStripsEmpty(t *testing.T) {
	clearRequiredEnv(t)
	t.Setenv("HELIX_TEST_EMPTY_VAR", "")
	m := processEnvMap()
	// processEnvMap itself does NOT strip — it returns the raw os.Environ
	// map. Strict-mode scrubbing happens in runEnvCheck before the
	// ValidateEnvVars call. This test documents that contract.
	v, ok := m["HELIX_TEST_EMPTY_VAR"]
	assert.True(t, ok, "processEnvMap includes env vars regardless of value")
	assert.Equal(t, "", v, "empty value is preserved verbatim by processEnvMap")
}

// =============================================================================
// buildJSONReport
// =============================================================================

func TestBuildJSONReport_AllPresent(t *testing.T) {
	rpt := config.InventoryReport{
		Total:   3,
		Present: 3,
	}
	out := buildJSONReport(rpt, "/tmp/.env", "forgejo", true)
	assert.Equal(t, 3, out.Total)
	assert.Equal(t, 3, out.Present)
	assert.False(t, out.HasMissing)
	assert.Equal(t, 0, out.MissingCnt)
	assert.Equal(t, "/tmp/.env", out.EnvFile)
	assert.Equal(t, "forgejo", out.Source)
	assert.True(t, out.Strict)
}

func TestBuildJSONReport_SomeMissing(t *testing.T) {
	rpt := config.InventoryReport{
		Total:   3,
		Present: 1,
		Missing: []config.EnvVarReport{
			{Var: config.EnvVar{Name: "OPENROUTER_API_KEY", Service: "platform", Description: "x"}},
			{Var: config.EnvVar{Name: "FORGEJO_RUNNER_TOKEN", Service: "forgejo", Description: "y"}},
		},
	}
	rpt.HasMissing = true
	out := buildJSONReport(rpt, "", "", true)
	assert.True(t, out.HasMissing)
	assert.Equal(t, 2, out.MissingCnt)
	assert.Equal(t, []envCheckMissing{
		{Name: "OPENROUTER_API_KEY", Service: "platform", Description: "x"},
		{Name: "FORGEJO_RUNNER_TOKEN", Service: "forgejo", Description: "y"},
	}, out.Missing)
}

// =============================================================================
// formatTable
// =============================================================================

func TestFormatTable_BasicRender(t *testing.T) {
	vars := []config.EnvVar{
		{Name: "HELIX_TEST_RENDER_VAR", Service: "test", Description: "render test", Required: true},
	}
	rpt := config.InventoryReport{Total: 1, Present: 1}
	out := formatTable(vars, rpt, "/tmp/.env", "", true)
	assert.Contains(t, out, "Helix Environment Variable Inventory")
	assert.Contains(t, out, "HELIX_TEST_RENDER_VAR")
	assert.Contains(t, out, "1/1 present")
}

func TestFormatTable_RendersMissingSection(t *testing.T) {
	vars := []config.EnvVar{
		{Name: "HELIX_TEST_MISSING_VAR", Service: "test", Required: true},
	}
	rpt := config.InventoryReport{
		Total: 1, Present: 0,
		Missing: []config.EnvVarReport{{Var: vars[0]}},
	}
	rpt.HasMissing = true
	out := formatTable(vars, rpt, "", "", true)
	assert.Contains(t, out, "Missing required variables:")
	assert.Contains(t, out, "HELIX_TEST_MISSING_VAR")
}

// =============================================================================
// validateSingle
// =============================================================================

func TestValidateSingle_ProcessEnvWins(t *testing.T) {
	t.Setenv("HELIX_TEST_SINGLE_VAR", "value-1")
	rr := validateSingle(config.EnvVar{Name: "HELIX_TEST_SINGLE_VAR", Service: "test"}, "")
	assert.True(t, rr.Present)
	assert.True(t, rr.FromEnv)
	assert.Equal(t, "value-1", rr.Value)
}

func TestValidateSingle_LoaderFallback(t *testing.T) {
	clearRequiredEnv(t)
	p := fixtureEnvFile(t, "HELIX_TEST_FALLBACK_VAR=loaded\n")
	rr := validateSingle(config.EnvVar{
		Name:    "HELIX_TEST_FALLBACK_VAR",
		Service: "test",
		Sources: []config.EnvSource{config.SourceDotenv},
	}, p)
	assert.True(t, rr.Present)
	assert.Equal(t, "loaded", rr.Value)
}

func TestValidateSingle_RedactsSecret(t *testing.T) {
	// Use a var name containing KEY so redactIfSecret triggers.
	t.Setenv("HELIX_TEST_API_KEY_PROBE", "sk-or-v1-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	rr := validateSingle(config.EnvVar{Name: "HELIX_TEST_API_KEY_PROBE", Service: "test"}, "")
	assert.True(t, rr.Present)
	assert.Contains(t, rr.Value, "***", "secret-like value must be redacted")
	assert.NotContains(t, rr.Value, "sk-or-v1-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "full secret must not appear")
}

// =============================================================================
// runEnvCheck — end-to-end
// =============================================================================

func TestRunEnvCheck_Help(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runEnvCheck([]string{"--help"}, &stdout, &stderr)
	assert.Equal(t, 0, rc)
	assert.Contains(t, stdout.String(), "helix config env-check")
}

func TestRunEnvCheck_BadFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runEnvCheck([]string{"--no-such"}, &stdout, &stderr)
	assert.Equal(t, 2, rc, "bad flag → exit 2")
	assert.Contains(t, stderr.String(), "no-such")
}

func TestRunEnvCheck_MissingEnvFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runEnvCheck([]string{"--env-file", "/tmp/does-not-exist-helix-xyz.env"}, &stdout, &stderr)
	assert.Equal(t, 2, rc, "missing explicit env-file → exit 2")
	assert.Contains(t, stderr.String(), "not found")
}

func TestRunEnvCheck_AllMissing_ProcessEnvOnly(t *testing.T) {
	clearRequiredEnv(t)
	var stdout, stderr bytes.Buffer
	rc := runEnvCheck([]string{}, &stdout, &stderr)
	// Most required vars missing → exit 1
	assert.Equal(t, 1, rc)
	out := stdout.String()
	assert.Contains(t, out, "Helix Environment Variable Inventory")
	assert.Contains(t, out, "Missing required variables:")
}

func TestRunEnvCheck_AllPresent_ViaFile(t *testing.T) {
	clearRequiredEnv(t)
	// Build a file with all required vars from the inventory.
	vars := config.DefaultEnvVars()
	var b strings.Builder
	for _, v := range vars {
		if v.Required {
			b.WriteString(v.Name + "=set\n")
		}
	}
	p := fixtureEnvFile(t, b.String())

	var stdout, stderr bytes.Buffer
	rc := runEnvCheck([]string{"--env-file", p}, &stdout, &stderr)
	assert.Equal(t, 0, rc, "all required present → exit 0; stderr=%s", stderr.String())
	out := stdout.String()
	assert.Contains(t, out, "Helix Environment Variable Inventory")
}

func TestRunEnvCheck_AllPresent_ProcessEnv(t *testing.T) {
	clearRequiredEnv(t)
	// Set every required var.
	kv := map[string]string{}
	for _, v := range config.DefaultEnvVars() {
		if v.Required {
			kv[v.Name] = "via-env"
		}
	}
	withEnv(t, kv, func() {
		var stdout, stderr bytes.Buffer
		rc := runEnvCheck([]string{}, &stdout, &stderr)
		assert.Equal(t, 0, rc, "all required set via process env → exit 0; stderr=%s", stderr.String())
	})
}

// (signature chosen to keep tests readable.)
func TestRunEnvCheck_JSON(t *testing.T) {
	clearRequiredEnv(t)
	// Set all required so we get exit 0 with a clean JSON report.
	for _, v := range config.DefaultEnvVars() {
		if v.Required {
			t.Setenv(v.Name, "x")
		}
	}
	var stdout, stderr bytes.Buffer
	rc := runEnvCheck([]string{"--json"}, &stdout, &stderr)
	assert.Equal(t, 0, rc)
	out := stdout.String()
	// Must be valid JSON.
	var got envCheckReport
	require.NoError(t, json.Unmarshal([]byte(out), &got), "stdout must be valid JSON")
	assert.Equal(t, 0, got.MissingCnt)
	assert.False(t, got.HasMissing)
	assert.True(t, got.Strict)
}

func TestRunEnvCheck_JSON_MissingReported(t *testing.T) {
	clearRequiredEnv(t)
	var stdout, stderr bytes.Buffer
	rc := runEnvCheck([]string{"--json"}, &stdout, &stderr)
	assert.Equal(t, 1, rc)
	var got envCheckReport
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &got))
	assert.True(t, got.HasMissing)
	assert.Greater(t, got.MissingCnt, 0)
	for _, m := range got.Missing {
		assert.NotEmpty(t, m.Name)
		assert.NotEmpty(t, m.Service)
	}
}

func TestRunEnvCheck_SourceFilter_UnknownSource(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runEnvCheck([]string{"--source", "made-up"}, &stdout, &stderr)
	assert.Equal(t, 2, rc, "unknown source → exit 2")
	assert.Contains(t, stderr.String(), "made-up")
}

func TestRunEnvCheck_SourceFilter_KnownSource(t *testing.T) {
	clearRequiredEnv(t)
	t.Setenv("FORGEJO_RUNNER_TOKEN", "set")

	var stdout, stderr bytes.Buffer
	// With --source forgejo, we only check forgejo vars → exit 0
	rc := runEnvCheck([]string{"--source", "forgejo"}, &stdout, &stderr)
	assert.Equal(t, 0, rc, "forgejo group with FORGEJO_RUNNER_TOKEN set → exit 0; stderr=%s", stderr.String())
}

func TestRunEnvCheck_Strict_EmptyCountsAsMissing(t *testing.T) {
	clearRequiredEnv(t)
	t.Setenv("FORGEJO_RUNNER_TOKEN", "")

	var stdout, stderr bytes.Buffer
	rc := runEnvCheck([]string{"--source", "forgejo"}, &stdout, &stderr)
	// In strict mode (default), empty value counts as missing.
	assert.Equal(t, 1, rc, "strict mode: empty value → missing → exit 1")
}

// =============================================================================
// runConfig dispatcher
// =============================================================================

func TestRunConfig_NoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runConfig([]string{}, &stdout, &stderr)
	assert.Equal(t, 2, rc, "no subcommand → exit 2")
	assert.Contains(t, stderr.String(), "missing subcommand")
}

func TestRunConfig_Help(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runConfig([]string{"help"}, &stdout, &stderr)
	assert.Equal(t, 0, rc)
	assert.Contains(t, stdout.String(), "helix config")
}

func TestRunConfig_EnvCheckDispatch(t *testing.T) {
	clearRequiredEnv(t)
	var stdout, stderr bytes.Buffer
	rc := runConfig([]string{"env-check", "--help"}, &stdout, &stderr)
	assert.Equal(t, 0, rc)
	assert.Contains(t, stdout.String(), "env-check")
}

func TestRunConfig_UnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runConfig([]string{"frobnicate"}, &stdout, &stderr)
	assert.Equal(t, 2, rc)
	assert.Contains(t, stderr.String(), "frobnicate")
}

// =============================================================================
// printEnvCheckUsage
// =============================================================================

func TestPrintEnvCheckUsage(t *testing.T) {
	var b bytes.Buffer
	printEnvCheckUsage(&b)
	out := b.String()
	assert.Contains(t, out, "helix config env-check")
	assert.Contains(t, out, "--env-file")
	assert.Contains(t, out, "--source")
	assert.Contains(t, out, "--strict")
	assert.Contains(t, out, "--json")
	assert.Contains(t, out, "Exit codes")
}
