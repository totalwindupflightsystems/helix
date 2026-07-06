package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunAPI_Help verifies that --help shows the help text.
func TestRunAPI_Help(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runAPI([]string{"--help"}, &stdout, &stderr)
	assert.Equal(t, apiExitOK, rc)
	assert.Contains(t, stdout.String(), "helix api")
	assert.Contains(t, stdout.String(), "serve")
	assert.Contains(t, stdout.String(), "contracts")
}

// TestRunAPI_HelpSubcommand verifies the help subcommand.
func TestRunAPI_HelpSubcommand(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runAPI([]string{"help"}, &stdout, &stderr)
	assert.Equal(t, apiExitOK, rc)
	assert.Contains(t, stdout.String(), "helix api")
}

// TestRunAPI_DefaultToHelp verifies no subcommand defaults to help.
func TestRunAPI_DefaultToHelp(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runAPI([]string{}, &stdout, &stderr)
	assert.Equal(t, apiExitOK, rc)
	assert.Contains(t, stdout.String(), "helix api")
}

// TestRunAPI_UnknownSubcommand verifies error for unknown subcommand.
func TestRunAPI_UnknownSubcommand(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runAPI([]string{"bogus"}, &stdout, &stderr)
	assert.Equal(t, apiExitError, rc)
	assert.Contains(t, stderr.String(), "unknown subcommand")
}

// TestRunAPI_UnknownFlag verifies error for unknown flag.
func TestRunAPI_UnknownFlag(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runAPI([]string{"--bogus"}, &stdout, &stderr)
	assert.Equal(t, apiExitError, rc)
}

// TestRunAPIContracts_Text verifies text output of contracts listing.
func TestRunAPIContracts_Text(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runAPI([]string{"contracts"}, &stdout, &stderr)
	assert.Equal(t, apiExitOK, rc)
	output := stdout.String()
	assert.Contains(t, output, "Forgejo")
	assert.Contains(t, output, "Chimera")
	assert.Contains(t, output, "Conscientiousness")
	assert.Contains(t, output, "Hivemind")
	assert.Contains(t, output, "Muster")
}

// TestRunAPIContracts_JSON verifies JSON output of contracts listing.
func TestRunAPIContracts_JSON(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runAPI([]string{"contracts", "--json"}, &stdout, &stderr)
	assert.Equal(t, apiExitOK, rc)
	output := stdout.String()
	assert.Contains(t, output, "forgejo")
	assert.Contains(t, output, "chimera")
	assert.Contains(t, output, "base_url")
}

// TestRunAPIServices_Text verifies text output of services listing.
func TestRunAPIServices_Text(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runAPI([]string{"services"}, &stdout, &stderr)
	assert.Equal(t, apiExitOK, rc)
	output := stdout.String()
	assert.Contains(t, output, "forgejo")
	assert.Contains(t, output, "Forgejo")
}

// TestRunAPIServices_JSON verifies JSON output of services listing.
func TestRunAPIServices_JSON(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runAPI([]string{"services", "--json"}, &stdout, &stderr)
	assert.Equal(t, apiExitOK, rc)
	output := stdout.String()
	assert.Contains(t, output, "\"id\":")
	assert.Contains(t, output, "\"name\":")
}

// TestRunAPIValidate_NoArgs verifies error when validate has no service/endpoint.
func TestRunAPIValidate_NoArgs(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runAPI([]string{"validate"}, &stdout, &stderr)
	assert.Equal(t, apiExitError, rc)
	assert.Contains(t, stderr.String(), "requires")
}

// TestRunAPIValidate_UnknownService verifies error for unknown service.
func TestRunAPIValidate_UnknownService(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runAPI([]string{"validate", "nonexistent", "some-endpoint", "--body", "{}"}, &stdout, &stderr)
	assert.Equal(t, apiExitError, rc)
	assert.Contains(t, stderr.String(), "unknown service")
}

// TestRunAPIValidate_UnknownEndpoint verifies error for unknown endpoint.
func TestRunAPIValidate_UnknownEndpoint(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runAPI([]string{"validate", "forgejo", "nonexistent", "--body", "{}"}, &stdout, &stderr)
	assert.Equal(t, apiExitError, rc)
	assert.Contains(t, stderr.String(), "unknown endpoint")
}

// TestRunAPIValidate_Valid verifies a valid request passes validation.
func TestRunAPIValidate_Valid(t *testing.T) {
	var stdout, stderr strings.Builder
	body := `{"username":"agent-001","email":"a@b.com","password":"password123"}`
	rc := runAPI([]string{"validate", "forgejo", "create-user", "--body", body}, &stdout, &stderr)
	assert.Equal(t, apiExitOK, rc)
	assert.Contains(t, stdout.String(), "VALID")
}

// TestRunAPIValidate_Invalid verifies an invalid request fails validation.
func TestRunAPIValidate_Invalid(t *testing.T) {
	var stdout, stderr strings.Builder
	body := `{"username":"","email":"","password":"short"}`
	rc := runAPI([]string{"validate", "forgejo", "create-user", "--body", body}, &stdout, &stderr)
	assert.Equal(t, apiExitOK, rc)
	assert.Contains(t, stdout.String(), "INVALID")
}

// TestRunAPIValidate_JSON verifies JSON output of validation.
func TestRunAPIValidate_JSON(t *testing.T) {
	var stdout, stderr strings.Builder
	body := `{"username":"agent-001","email":"a@b.com","password":"password123"}`
	rc := runAPI([]string{"validate", "forgejo", "create-user", "--body", body, "--json"}, &stdout, &stderr)
	assert.Equal(t, apiExitOK, rc)
	output := stdout.String()
	assert.Contains(t, output, "\"valid\": true")
	assert.Contains(t, output, "\"endpoint\": \"Create User\"")
}

// TestRunAPIValidate_AllServices verifies validation works for all 5 services.
func TestRunAPIValidate_AllServices(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "forgejo",
			args: []string{"validate", "forgejo", "create-user", "--body",
				`{"username":"a","email":"a@b.com","password":"password123"}`},
		},
		{
			name: "chimera",
			args: []string{"validate", "chimera", "run-deliberation", "--body",
				`{"prompt":"test","formation":"auto"}`},
		},
		{
			name: "conscientiousness",
			args: []string{"validate", "conscientiousness", "evaluate-pr", "--body",
				`{"pr_diff":"diff","pr_context":{"repo":"helix","number":1},"adversarial_agents":["@x"]}`},
		},
		{
			name: "hivemind",
			args: []string{"validate", "hivemind", "write-memory", "--body",
				`{"agent_id":"a","repo":"helix","event_type":"merge","summary":"ok","resolution":"approved"}`},
		},
		{
			name: "muster",
			args: []string{"validate", "muster", "generate-mcp-tools", "--body",
				`{"openapi_spec_url":"https://example.com/spec.json","output_format":"json"}`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			rc := runAPI(tt.args, &stdout, &stderr)
			assert.Equal(t, apiExitOK, rc, "service %s: %s", tt.name, stderr.String())
		})
	}
}

// TestRunAPIWithDryRun_NoError verifies that a successful run returns no error.
func TestRunAPIWithDryRun_NoError(t *testing.T) {
	var stdout, stderr strings.Builder
	err := runAPIWithDryRun([]string{"services"}, &stdout, &stderr, false)
	assert.NoError(t, err)
}

// TestRunAPIWithDryRun_ErrorPropagated verifies errors propagate.
func TestRunAPIWithDryRun_ErrorPropagated(t *testing.T) {
	var stdout, stderr strings.Builder
	err := runAPIWithDryRun([]string{"bogus"}, &stdout, &stderr, false)
	assert.Error(t, err)
}

// TestSlugifyAPI verifies slug conversion.
func TestSlugifyAPI(t *testing.T) {
	assert.Equal(t, "create-user", slugifyAPI("Create User"))
	assert.Equal(t, "run-deliberation", slugifyAPI("Run Deliberation"))
	assert.Equal(t, "generate-mcp-tools", slugifyAPI("Generate MCP Tools"))
}

// TestParseAPIFlags_Addr verifies --addr flag parsing.
func TestParseAPIFlags_Addr(t *testing.T) {
	flags, _, rc := parseAPIFlags([]string{"serve", "--addr", ":8080"})
	require.Equal(t, apiExitOK, rc)
	assert.Equal(t, ":8080", flags.addr)
	assert.Equal(t, "serve", flags.subcommand)
}

// TestParseAPIFlags_MissingAddrValue verifies error for missing --addr value.
func TestParseAPIFlags_MissingAddrValue(t *testing.T) {
	_, _, rc := parseAPIFlags([]string{"serve", "--addr"})
	assert.Equal(t, apiExitError, rc)
}

// TestParseAPIFlags_Json verifies --json flag.
func TestParseAPIFlags_Json(t *testing.T) {
	flags, _, rc := parseAPIFlags([]string{"contracts", "--json"})
	require.Equal(t, apiExitOK, rc)
	assert.True(t, flags.jsonOut)
}

// TestParseAPIFlags_Body verifies --body flag.
func TestParseAPIFlags_Body(t *testing.T) {
	flags, _, rc := parseAPIFlags([]string{"validate", "forgejo", "create-user", "--body", `{"a":1}`})
	require.Equal(t, apiExitOK, rc)
	assert.Equal(t, `{"a":1}`, flags.body)
	assert.Equal(t, "validate", flags.subcommand)
	assert.Len(t, flags.args, 2)
}

// TestParseAPIFlags_MissingBodyValue verifies error for missing --body value.
func TestParseAPIFlags_MissingBodyValue(t *testing.T) {
	_, _, rc := parseAPIFlags([]string{"validate", "--body"})
	assert.Equal(t, apiExitError, rc)
}

// TestParseAPIFlags_Help verifies --help returns helpWanted=true.
func TestParseAPIFlags_Help(t *testing.T) {
	_, helpWanted, rc := parseAPIFlags([]string{"--help"})
	require.Equal(t, apiExitOK, rc)
	assert.True(t, helpWanted)
}
