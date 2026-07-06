package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Flag parsing tests
// ============================================================================

func TestParseVerifyFlags_DefaultsToHelp(t *testing.T) {
	flags, helpWanted, rc := parseVerifyFlags([]string{})
	assert.Equal(t, mgExitOK, rc)
	assert.False(t, helpWanted)
	assert.Equal(t, "help", flags.subcommand)
}

func TestParseVerifyFlags_ExplicitHelp(t *testing.T) {
	_, helpWanted, rc := parseVerifyFlags([]string{"--help"})
	assert.Equal(t, mgExitOK, rc)
	assert.True(t, helpWanted)
}

func TestParseVerifyFlags_AllFlags(t *testing.T) {
	flags, _, rc := parseVerifyFlags([]string{
		"shadow", "--agent", "agent-1", "--tier", "trusted", "--json",
	})
	require.Equal(t, mgExitOK, rc)
	assert.Equal(t, "shadow", flags.subcommand)
	assert.Equal(t, "agent-1", flags.agent)
	assert.Equal(t, "trusted", flags.tier)
	assert.True(t, flags.jsonOut)
}

func TestParseVerifyFlags_MissingFlagValue(t *testing.T) {
	for _, arg := range []string{"--agent", "--tier", "--path"} {
		_, _, rc := parseVerifyFlags([]string{arg})
		assert.Equal(t, mgExitError, rc, "missing value for %s", arg)
	}
}

func TestParseVerifyFlags_UnknownLongFlag(t *testing.T) {
	_, _, rc := parseVerifyFlags([]string{"shadow", "--bogus"})
	assert.Equal(t, mgExitError, rc)
}

// ============================================================================
// Subcommand dispatch tests
// ============================================================================

func TestRunVerify_Help(t *testing.T) {
	var out bytes.Buffer
	rc := runVerify([]string{"help"}, &out, &out)
	assert.Equal(t, mgExitOK, rc)
	assert.Contains(t, out.String(), "helix verify")
}

func TestRunVerify_HelpFlag(t *testing.T) {
	var out bytes.Buffer
	rc := runVerify([]string{"--help"}, &out, &out)
	assert.Equal(t, mgExitOK, rc)
	assert.Contains(t, out.String(), "helix verify")
}

func TestRunVerify_UnknownSubcommand(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runVerify([]string{"bogus"}, &out, &errBuf)
	assert.Equal(t, mgExitError, rc)
	assert.Contains(t, errBuf.String(), "unknown subcommand")
}

// ============================================================================
// shadow subcommand
// ============================================================================

func TestRunVerify_Shadow_MissingAgent(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runVerify([]string{"shadow"}, &out, &errBuf)
	assert.Equal(t, mgExitError, rc)
	assert.Contains(t, errBuf.String(), "--agent")
}

func TestRunVerify_Shadow_JSON(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runVerify([]string{"shadow", "--agent", "agent-1", "--tier", "trusted", "--json"}, &out, &errBuf)
	require.Equal(t, mgExitOK, rc)
	assert.Contains(t, out.String(), "\"agent\": \"agent-1\"")
	assert.Contains(t, out.String(), "\"tier\": \"trusted\"")
	assert.Contains(t, out.String(), "\"launched\": true")
	assert.Contains(t, out.String(), "\"state\"")
	assert.Contains(t, out.String(), "\"remaining\"")
}

func TestRunVerify_Shadow_Text(t *testing.T) {
	var out bytes.Buffer
	rc := runVerify([]string{"shadow", "--agent", "agent-1"}, &out, &out)
	require.Equal(t, mgExitOK, rc)
	s := out.String()
	// Should contain an icon + state label + agent + tier
	assert.Contains(t, s, "Shadow Deployment")
	assert.Contains(t, s, "Agent:")
	assert.Contains(t, s, "Tier:")
}

func TestRunVerify_Shadow_DefaultTier(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runVerify([]string{"shadow", "--agent", "a", "--json"}, &out, &errBuf)
	require.Equal(t, mgExitOK, rc)
	assert.Contains(t, out.String(), "\"tier\": \"provisional\"")
}

// ============================================================================
// canary subcommand
// ============================================================================

func TestRunVerify_Canary_MissingAgent(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runVerify([]string{"canary"}, &out, &errBuf)
	assert.Equal(t, mgExitError, rc)
	assert.Contains(t, errBuf.String(), "--agent")
}

func TestRunVerify_Canary_JSON(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runVerify([]string{"canary", "--agent", "agent-1", "--tier", "observed", "--json"}, &out, &errBuf)
	require.Equal(t, mgExitOK, rc)
	assert.Contains(t, out.String(), "\"agent\": \"agent-1\"")
	assert.Contains(t, out.String(), "\"tier\": \"observed\"")
	assert.Contains(t, out.String(), "\"decision\"")
	assert.Contains(t, out.String(), "\"canary_percentage\"")
	assert.Contains(t, out.String(), "\"ramp_steps\"")
}

func TestRunVerify_Canary_Text(t *testing.T) {
	var out bytes.Buffer
	rc := runVerify([]string{"canary", "--agent", "agent-1", "--tier", "veteran"}, &out, &out)
	require.Equal(t, mgExitOK, rc)
	s := out.String()
	assert.Contains(t, s, "Canary Promotion")
	assert.Contains(t, s, "Canary traffic:")
}

// ============================================================================
// contract subcommand
// ============================================================================

func TestRunVerify_Contract_MissingPath(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runVerify([]string{"contract"}, &out, &errBuf)
	assert.Equal(t, mgExitError, rc)
	assert.Contains(t, errBuf.String(), "--path")
}

func TestRunVerify_Contract_MissingFile(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runVerify([]string{"contract", "--path", "/nonexistent/path/file.yaml"}, &out, &errBuf)
	assert.Equal(t, mgExitError, rc)
	assert.Contains(t, errBuf.String(), "reading contract")
}

func TestRunVerify_Contract_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	require.NoError(t, os.WriteFile(path, []byte("this is: not: valid: yaml: ["), 0o600))

	var out, errBuf bytes.Buffer
	rc := runVerify([]string{"contract", "--path", path}, &out, &errBuf)
	assert.Equal(t, mgExitError, rc)
	assert.Contains(t, errBuf.String(), "parsing contract")
}

func TestRunVerify_Contract_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "good.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`contract:
  name: test-contract
  agent: agent-1
  merge_commit: abc123
  assertions:
    - metric: success_rate
      operator: gte
      value: 0.99
      window: 1h
    - metric: p99_latency_ms
      operator: lte
      value: 500
      window: 1h
  breach_action: rollback
`), 0o600))

	var out, errBuf bytes.Buffer
	rc := runVerify([]string{"contract", "--path", path}, &out, &errBuf)
	require.Equal(t, mgExitOK, rc)
	s := out.String()
	assert.Contains(t, s, "Contract VALID")
	assert.Contains(t, s, "test-contract")
	assert.Contains(t, s, "Assertions: 2")
	assert.Contains(t, s, "success_rate")
}

func TestRunVerify_Contract_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "good.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`contract:
  name: c
  agent: a
  merge_commit: abc
  assertions:
    - metric: success_rate
      operator: gte
      value: 0.99
      window: 1h
`), 0o600))

	var out, errBuf bytes.Buffer
	rc := runVerify([]string{"contract", "--path", path, "--json"}, &out, &errBuf)
	require.Equal(t, mgExitOK, rc)

	var data map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &data))
	assert.Equal(t, true, data["valid"])
	assert.Equal(t, "c", data["name"])
	assert.Equal(t, false, data["should_rollback"]) // no breach_action → no rollback
}

func TestRunVerify_Contract_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.yaml")
	// Valid YAML, but invalid contract (no assertions, no merge_commit).
	require.NoError(t, os.WriteFile(path, []byte(`contract:
  name: empty
  agent: a
`), 0o600))

	var out, errBuf bytes.Buffer
	rc := runVerify([]string{"contract", "--path", path, "--json"}, &out, &errBuf)
	// ParseContract calls Validate internally and returns error → mgExitError (2).
	// The contract subcommand reports parse/validate failures as invocation errors.
	assert.Equal(t, mgExitError, rc)

	// Output should be empty since the failure happens at ParseContract.
	assert.Empty(t, out.String())
	assert.Contains(t, errBuf.String(), "merge_commit")
}

// ============================================================================
// Help text
// ============================================================================

func TestPrintVerifyHelp_Content(t *testing.T) {
	var buf bytes.Buffer
	printVerifyHelp(&buf)
	s := buf.String()
	for _, want := range []string{"helix verify", "shadow", "canary", "contract", "--agent", "Exit codes:"} {
		assert.Contains(t, s, want)
	}
}

// ============================================================================
// Wrapper
// ============================================================================

func TestRunVerifyWithDryRun_OK(t *testing.T) {
	var out, errBuf bytes.Buffer
	err := runVerifyWithDryRun([]string{"help"}, &out, &errBuf, false)
	assert.NoError(t, err)
}

func TestRunVerifyWithDryRun_PropagatesError(t *testing.T) {
	var out, errBuf bytes.Buffer
	err := runVerifyWithDryRun([]string{"shadow"}, &out, &errBuf, false)
	assert.Error(t, err)
}
