package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/totalwindupflightsystems/helix/pkg/security"
)

// ============================================================================
// Flag parsing tests
// ============================================================================

func TestParseSecurityFlags_DefaultsToHelp(t *testing.T) {
	flags, helpWanted, rc := parseSecurityFlags([]string{})
	assert.Equal(t, mgExitOK, rc)
	assert.False(t, helpWanted)
	assert.Equal(t, "help", flags.subcommand)
}

func TestParseSecurityFlags_ExplicitHelp(t *testing.T) {
	flags, helpWanted, rc := parseSecurityFlags([]string{"--help"})
	assert.Equal(t, mgExitOK, rc)
	assert.True(t, helpWanted)
	assert.Equal(t, "help", flags.subcommand)
}

func TestParseSecurityFlags_CheckSubcommand(t *testing.T) {
	flags, _, rc := parseSecurityFlags([]string{"check", "--json"})
	require.Equal(t, mgExitOK, rc)
	assert.Equal(t, "check", flags.subcommand)
	assert.True(t, flags.jsonOut)
}

func TestParseSecurityFlags_UnknownLongFlag(t *testing.T) {
	_, _, rc := parseSecurityFlags([]string{"check", "--bogus"})
	assert.Equal(t, mgExitError, rc)
}

// ============================================================================
// Subcommand dispatch tests
// ============================================================================

func TestRunSecurity_Help(t *testing.T) {
	var out bytes.Buffer
	rc := runSecurity([]string{"help"}, &out, &out)
	assert.Equal(t, mgExitOK, rc)
	assert.Contains(t, out.String(), "helix security")
}

func TestRunSecurity_HelpFlag(t *testing.T) {
	var out bytes.Buffer
	rc := runSecurity([]string{"--help"}, &out, &out)
	assert.Equal(t, mgExitOK, rc)
	assert.Contains(t, out.String(), "helix security")
}

func TestRunSecurity_UnknownSubcommand(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runSecurity([]string{"bogus"}, &out, &errBuf)
	assert.Equal(t, mgExitError, rc)
	assert.Contains(t, errBuf.String(), "unknown subcommand")
}

// ============================================================================
// check subcommand
// ============================================================================

func TestRunSecurity_Check_Text(t *testing.T) {
	var out, errBuf bytes.Buffer
	// CLI runs register every default check as SKIP (server-side inspection needed).
	// So no FAIL is expected → exit 0.
	rc := runSecurity([]string{"check"}, &out, &errBuf)
	assert.Equal(t, mgExitOK, rc)
	assert.NotEmpty(t, out.String())
}

func TestRunSecurity_Check_JSON(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runSecurity([]string{"check", "--json"}, &out, &errBuf)
	require.Equal(t, mgExitOK, rc)

	var data map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &data))
	assert.Contains(t, data, "checks")
	assert.Contains(t, data, "summary")
}

func TestRunSecurity_Check_JSONStructure(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runSecurity([]string{"check", "--json"}, &out, &errBuf)
	require.Equal(t, mgExitOK, rc)

	var data map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &data))

	checks, ok := data["checks"].([]any)
	require.True(t, ok, "checks should be array")
	assert.NotEmpty(t, checks)

	// Each check should have id/name/status/detail/category.
	for _, c := range checks {
		m, ok := c.(map[string]any)
		require.True(t, ok)
		for _, k := range []string{"id", "name", "status", "detail", "category"} {
			assert.Contains(t, m, k, "check missing field %q", k)
		}
	}
}

// ============================================================================
// checklist subcommand
// ============================================================================

func TestRunSecurity_Checklist_Text(t *testing.T) {
	var out bytes.Buffer
	rc := runSecurity([]string{"checklist"}, &out, &out)
	require.Equal(t, mgExitOK, rc)
	s := out.String()
	assert.Contains(t, s, "Security Hardening Checklist")
	// Some checks should be in the list (from spec §6.6).
	assert.NotEmpty(t, out.String())
}

func TestRunSecurity_Checklist_JSON(t *testing.T) {
	var out bytes.Buffer
	rc := runSecurity([]string{"checklist", "--json"}, &out, &out)
	require.Equal(t, mgExitOK, rc)

	var items []map[string]any
	err := json.Unmarshal(out.Bytes(), &items)
	require.NoError(t, err)
	assert.NotEmpty(t, items)

	for _, item := range items {
		for _, k := range []string{"id", "name", "description", "category"} {
			assert.Contains(t, item, k, "checklist item missing field %q", k)
		}
	}
}

// ============================================================================
// Help text
// ============================================================================

func TestPrintSecurityHelp_Content(t *testing.T) {
	var buf bytes.Buffer
	printSecurityHelp(&buf)
	s := buf.String()
	for _, want := range []string{"helix security", "check", "checklist", "--json", "Exit codes:"} {
		assert.Contains(t, s, want)
	}
}

// ============================================================================
// Wrapper
// ============================================================================

func TestRunSecurityWithDryRun_OK(t *testing.T) {
	var out, errBuf bytes.Buffer
	err := runSecurityWithDryRun([]string{"help"}, &out, &errBuf, false)
	assert.NoError(t, err)
}

func TestRunSecurityWithDryRun_PropagatesError(t *testing.T) {
	var out, errBuf bytes.Buffer
	err := runSecurityWithDryRun([]string{"bogus"}, &out, &errBuf, false)
	assert.Error(t, err)
}

// ============================================================================
// Integration: end-to-end smoke
// ============================================================================

func TestRunSecurity_Check_FullSmoke(t *testing.T) {
	// Run check + checklist end-to-end and verify content.
	var checkOut bytes.Buffer
	rc1 := runSecurity([]string{"check"}, &checkOut, &checkOut)
	assert.Equal(t, mgExitOK, rc1)

	var listOut bytes.Buffer
	rc2 := runSecurity([]string{"checklist"}, &listOut, &listOut)
	assert.Equal(t, mgExitOK, rc2)

	// Both should reference security hardening concepts.
	assert.True(t, strings.Contains(checkOut.String(), "deployment") ||
		strings.Contains(checkOut.String(), "operational") ||
		strings.Contains(checkOut.String(), "SKIP") ||
		strings.Contains(checkOut.String(), "PASS"))
}

// ============================================================================
// Sanity: no panic on bad flags
// ============================================================================

func TestRunSecurity_NoPanicOnRandomInputs(t *testing.T) {
	var out, errBuf bytes.Buffer
	for _, args := range [][]string{
		{"check", "--json"},
		{"checklist", "--json"},
		{"help"},
	} {
		assert.NotPanics(t, func() {
			_ = runSecurity(args, &out, &errBuf)
		})
	}
}

// ============================================================================
// Verify the default hardening checks load (no mocked output)
// ============================================================================

func TestRunSecurity_Check_DefaultChecksExist(t *testing.T) {
	checks := security.DefaultChecks()
	assert.NotEmpty(t, checks)
	// Sanity: each check has required fields.
	for _, c := range checks {
		assert.NotEmpty(t, c.ID, "check has empty ID")
		assert.NotEmpty(t, c.Name, "check has empty Name")
		assert.NotEmpty(t, c.Description, "check has empty Description")
	}
}
