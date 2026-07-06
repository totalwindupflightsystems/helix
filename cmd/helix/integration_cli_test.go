package main

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunIntegration_Help verifies --help shows help text.
func TestRunIntegration_Help(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runIntegration([]string{"--help"}, &stdout, &stderr)
	assert.Equal(t, integExitOK, rc)
	assert.Contains(t, stdout.String(), "helix integration")
	assert.Contains(t, stdout.String(), "test")
	assert.Contains(t, stdout.String(), "list")
}

// TestRunIntegration_HelpSubcommand verifies help subcommand.
func TestRunIntegration_HelpSubcommand(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runIntegration([]string{"help"}, &stdout, &stderr)
	assert.Equal(t, integExitOK, rc)
	assert.Contains(t, stdout.String(), "helix integration")
}

// TestRunIntegration_DefaultToHelp verifies no subcommand defaults to help.
func TestRunIntegration_DefaultToHelp(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runIntegration([]string{}, &stdout, &stderr)
	assert.Equal(t, integExitOK, rc)
	assert.Contains(t, stdout.String(), "helix integration")
}

// TestRunIntegration_UnknownSubcommand verifies error for unknown subcommand.
func TestRunIntegration_UnknownSubcommand(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runIntegration([]string{"bogus"}, &stdout, &stderr)
	assert.Equal(t, integExitError, rc)
	assert.Contains(t, stderr.String(), "unknown subcommand")
}

// TestRunIntegration_UnknownFlag verifies error for unknown flag.
func TestRunIntegration_UnknownFlag(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runIntegration([]string{"--bogus"}, &stdout, &stderr)
	assert.Equal(t, integExitError, rc)
}

// TestRunIntegrationList verifies listing all scenarios.
func TestRunIntegrationList(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runIntegration([]string{"list"}, &stdout, &stderr)
	assert.Equal(t, integExitOK, rc)
	output := stdout.String()
	assert.Contains(t, output, "forgejo-connectivity")
	assert.Contains(t, output, "chimera-connectivity")
	assert.Contains(t, output, "full-loop")
}

// TestRunIntegrationList_JSON verifies JSON listing.
func TestRunIntegrationList_JSON(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runIntegration([]string{"list", "--json"}, &stdout, &stderr)
	assert.Equal(t, integExitOK, rc)
	output := stdout.String()
	assert.Contains(t, output, "forgejo-connectivity")
	assert.Contains(t, output, "\"service\":")
}

// TestRunIntegrationList_ServiceFilter verifies service filter.
func TestRunIntegrationList_ServiceFilter(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runIntegration([]string{"list", "--service", "forgejo"}, &stdout, &stderr)
	assert.Equal(t, integExitOK, rc)
	output := stdout.String()
	assert.Contains(t, output, "forgejo-connectivity")
	assert.Contains(t, output, "full-loop")
	assert.NotContains(t, output, "chimera-connectivity")
}

// TestRunIntegrationList_ServiceFilter_Chimera verifies chimera filter.
func TestRunIntegrationList_ServiceFilter_Chimera(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runIntegration([]string{"list", "--service", "chimera", "--json"}, &stdout, &stderr)
	assert.Equal(t, integExitOK, rc)
	output := stdout.String()
	assert.Contains(t, output, "chimera-connectivity")
	assert.NotContains(t, output, "forgejo-connectivity")
}

// TestRunIntegrationTest_Unreachable verifies all scenarios skip when services unreachable.
func TestRunIntegrationTest_Unreachable(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runIntegration([]string{
		"test",
		"--forgejo-url", "http://localhost:59999", // unreachable port
		"--chimera-url", "http://localhost:59998", // unreachable port
		"--timeout", "1s",
	}, &stdout, &stderr)
	// When services are unreachable, all tests SKIP → exit code 0
	assert.Equal(t, integExitOK, rc)
	output := stdout.String()
	assert.Contains(t, output, "unreachable")
}

// TestRunIntegrationTest_Unreachable_JSON verifies JSON output when unreachable.
func TestRunIntegrationTest_Unreachable_JSON(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runIntegration([]string{
		"test",
		"--forgejo-url", "http://localhost:59999",
		"--chimera-url", "http://localhost:59998",
		"--timeout", "1s",
		"--json",
	}, &stdout, &stderr)
	assert.Equal(t, integExitOK, rc)
	output := stdout.String()
	assert.Contains(t, output, "\"reachable\": false")
	assert.Contains(t, output, "\"skipped\":")
}

// TestRunIntegrationTest_ServiceFilter verifies service filter applies to test.
func TestRunIntegrationTest_ServiceFilter(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runIntegration([]string{
		"test",
		"--service", "chimera",
		"--forgejo-url", "http://localhost:59999",
		"--chimera-url", "http://localhost:59998",
		"--timeout", "1s",
	}, &stdout, &stderr)
	assert.Equal(t, integExitOK, rc)
	output := stdout.String()
	assert.Contains(t, output, "chimera-connectivity")
	// Should not have forgejo tests
	assert.NotContains(t, output, "forgejo-connectivity")
}

// TestParseIntegFlags_ServiceFilter verifies --service flag.
func TestParseIntegFlags_ServiceFilter(t *testing.T) {
	flags, rc := parseIntegFlags([]string{"test", "--service", "forgejo"})
	require.Equal(t, integExitOK, rc)
	assert.Equal(t, "forgejo", flags.service)
	assert.Equal(t, "test", flags.subcommand)
}

// TestParseIntegFlags_JSON verifies --json flag.
func TestParseIntegFlags_JSON(t *testing.T) {
	flags, rc := parseIntegFlags([]string{"list", "--json"})
	require.Equal(t, integExitOK, rc)
	assert.True(t, flags.jsonOut)
}

// TestParseIntegFlags_Timeout verifies --timeout flag.
func TestParseIntegFlags_Timeout(t *testing.T) {
	flags, rc := parseIntegFlags([]string{"test", "--timeout", "10s"})
	require.Equal(t, integExitOK, rc)
	assert.Equal(t, 10*time.Second, flags.timeout)
}

// TestParseIntegFlags_InvalidTimeout verifies error for bad timeout.
func TestParseIntegFlags_InvalidTimeout(t *testing.T) {
	_, rc := parseIntegFlags([]string{"test", "--timeout", "not-a-duration"})
	assert.Equal(t, integExitError, rc)
}

// TestParseIntegFlags_MissingServiceValue verifies error for missing --service value.
func TestParseIntegFlags_MissingServiceValue(t *testing.T) {
	_, rc := parseIntegFlags([]string{"test", "--service"})
	assert.Equal(t, integExitError, rc)
}

// TestParseIntegFlags_ForgejoURL verifies --forgejo-url flag.
func TestParseIntegFlags_ForgejoURL(t *testing.T) {
	flags, rc := parseIntegFlags([]string{"test", "--forgejo-url", "http://my-forgejo:3000"})
	require.Equal(t, integExitOK, rc)
	assert.Equal(t, "http://my-forgejo:3000", flags.forgejoURL)
}

// TestParseIntegFlags_ChimeraURL verifies --chimera-url flag.
func TestParseIntegFlags_ChimeraURL(t *testing.T) {
	flags, rc := parseIntegFlags([]string{"test", "--chimera-url", "http://my-chimera:8000"})
	require.Equal(t, integExitOK, rc)
	assert.Equal(t, "http://my-chimera:8000", flags.chimeraURL)
}

// TestParseIntegFlags_DefaultURLs verifies default URLs from env.
func TestParseIntegFlags_DefaultURLs(t *testing.T) {
	flags, rc := parseIntegFlags([]string{"test"})
	require.Equal(t, integExitOK, rc)
	assert.NotEmpty(t, flags.forgejoURL)
	assert.NotEmpty(t, flags.chimeraURL)
}

// TestAllScenarios verifies scenario list structure.
func TestAllScenarios(t *testing.T) {
	scenarios := allScenarios()
	assert.GreaterOrEqual(t, len(scenarios), 3)
	for _, s := range scenarios {
		assert.NotEmpty(t, s.Name)
		assert.NotEmpty(t, s.Service)
		assert.NotEmpty(t, s.Description)
	}
}

// TestRunIntegrationWithDryRun_Success verifies no error wrapper on success.
func TestRunIntegrationWithDryRun_Success(t *testing.T) {
	var stdout, stderr strings.Builder
	err := runIntegrationWithDryRun([]string{"list"}, &stdout, &stderr, false)
	assert.NoError(t, err)
}

// TestRunIntegrationWithDryRun_ErrorPropagated verifies invocation errors propagate.
func TestRunIntegrationWithDryRun_ErrorPropagated(t *testing.T) {
	var stdout, stderr strings.Builder
	err := runIntegrationWithDryRun([]string{"bogus"}, &stdout, &stderr, false)
	assert.Error(t, err)
}

// TestCountByStatus verifies the status counter.
func TestCountByStatus(t *testing.T) {
	results := []integResult{
		{Status: "PASS"},
		{Status: "PASS"},
		{Status: "FAIL"},
		{Status: "SKIP"},
	}
	assert.Equal(t, 2, countByStatus(results, "PASS"))
	assert.Equal(t, 1, countByStatus(results, "FAIL"))
	assert.Equal(t, 1, countByStatus(results, "SKIP"))
	assert.Equal(t, 0, countByStatus(results, "ERROR"))
}

// TestIsPortOpen_Unreachable verifies unreachable port returns false.
func TestIsPortOpen_Unreachable(t *testing.T) {
	assert.False(t, isPortOpen("http://localhost:59999"))
}

// TestIsPortOpen_BadURL verifies malformed URL returns false.
func TestIsPortOpen_BadURL(t *testing.T) {
	assert.False(t, isPortOpen(""))
	assert.False(t, isPortOpen("not-a-url"))
}
