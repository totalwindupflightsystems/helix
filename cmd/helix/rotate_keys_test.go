package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/totalwindupflightsystems/helix/pkg/identity"
)

// writeStateFile writes a JSON state file to a temp directory and returns its path.
func writeStateFile(t *testing.T, entries []stateFileEntry) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "key-state.json")
	data := stateFileData{Keys: entries}
	raw, err := json.MarshalIndent(data, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0o644))
	return path
}

// TestRunRotateKeys_NoStateFile_Error verifies that running without --state-file fails.
func TestRunRotateKeys_NoStateFile_Error(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runRotateKeys([]string{}, &stdout, &stderr)
	assert.Equal(t, rotateExitError, rc)
	assert.Contains(t, stderr.String(), "requires --state-file")
}

// TestRunRotateKeys_FileNotFound verifies exit code 3 when file is missing.
func TestRunRotateKeys_FileNotFound(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runRotateKeys([]string{"--state-file", "/nonexistent/path/keys.json"}, &stdout, &stderr)
	assert.Equal(t, rotateExitNotFound, rc)
	assert.Contains(t, stderr.String(), "not found")
}

// TestRunRotateKeys_Help verifies that --help shows the help text.
func TestRunRotateKeys_Help(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runRotateKeys([]string{"--help"}, &stdout, &stderr)
	assert.Equal(t, rotateExitOK, rc)
	assert.Contains(t, stdout.String(), "helix identity rotate-keys")
	assert.Contains(t, stdout.String(), "--state-file")
	assert.Contains(t, stdout.String(), "--execute")
}

// TestRunRotateKeys_AllKeysHealthy verifies that healthy keys produce no actions.
func TestRunRotateKeys_AllKeysHealthy(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-10 * 24 * time.Hour).Format(time.RFC3339)

	entries := []stateFileEntry{
		{AgentName: "agent-001", KeyType: "ssh", KeyHash: "sha256:abc", CreatedAt: recent, Status: "active"},
		{AgentName: "agent-001", KeyType: "pat", KeyHash: "sha256:def", CreatedAt: recent,
			ExpiresAt: now.Add(90 * 24 * time.Hour).Format(time.RFC3339), Status: "active"},
		{AgentName: "agent-002", KeyType: "openrouter", KeyHash: "sha256:ghi", CreatedAt: recent, Status: "active"},
	}
	path := writeStateFile(t, entries)

	var stdout, stderr strings.Builder
	rc := runRotateKeys([]string{"--state-file", path, "--at", now.Format(time.RFC3339)}, &stdout, &stderr)
	assert.Equal(t, rotateExitOK, rc)
	assert.Contains(t, stdout.String(), "0 need rotation")
}

// TestRunRotateKeys_DeadKey_ImmediateRotation verifies that dead keys trigger immediate rotation.
func TestRunRotateKeys_DeadKey_ImmediateRotation(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-5 * 24 * time.Hour).Format(time.RFC3339)

	entries := []stateFileEntry{
		{AgentName: "agent-001", KeyType: "openrouter", KeyHash: "sha256:abc", CreatedAt: recent, Status: "dead"},
	}
	path := writeStateFile(t, entries)

	var stdout, stderr strings.Builder
	rc := runRotateKeys([]string{"--state-file", path, "--at", now.Format(time.RFC3339)}, &stdout, &stderr)
	assert.Equal(t, rotateExitAction, rc)
	assert.Contains(t, stdout.String(), "immediate")
	assert.Contains(t, stdout.String(), "agent-001")
	assert.Contains(t, stdout.String(), "openrouter")
}

// TestRunRotateKeys_ExpiredKey_ImmediateRotation verifies that expired PATs trigger immediate rotation.
func TestRunRotateKeys_ExpiredKey_ImmediateRotation(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-100 * 24 * time.Hour).Format(time.RFC3339)
	expired := now.Add(-10 * 24 * time.Hour).Format(time.RFC3339)

	entries := []stateFileEntry{
		{AgentName: "agent-001", KeyType: "pat", KeyHash: "sha256:abc", CreatedAt: old, ExpiresAt: expired, Status: "active"},
	}
	path := writeStateFile(t, entries)

	var stdout, stderr strings.Builder
	rc := runRotateKeys([]string{"--state-file", path, "--at", now.Format(time.RFC3339)}, &stdout, &stderr)
	assert.Equal(t, rotateExitAction, rc)
	assert.Contains(t, stdout.String(), "immediate")
}

// TestRunRotateKeys_ExpiringSoon_HighUrgency verifies that soon-to-expire PATs trigger high urgency.
func TestRunRotateKeys_ExpiringSoon_HighUrgency(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-80 * 24 * time.Hour).Format(time.RFC3339)
	soon := now.Add(3 * 24 * time.Hour).Format(time.RFC3339)

	entries := []stateFileEntry{
		{AgentName: "agent-001", KeyType: "pat", KeyHash: "sha256:abc", CreatedAt: old, ExpiresAt: soon, Status: "active"},
	}
	path := writeStateFile(t, entries)

	var stdout, stderr strings.Builder
	rc := runRotateKeys([]string{"--state-file", path, "--at", now.Format(time.RFC3339)}, &stdout, &stderr)
	assert.Equal(t, rotateExitAction, rc)
	assert.Contains(t, stdout.String(), "high")
	assert.Contains(t, stdout.String(), "expiring")
}

// TestRunRotateKeys_AgeExceeded verifies that old SSH keys trigger age-exceeded rotation.
func TestRunRotateKeys_AgeExceeded(t *testing.T) {
	now := time.Now().UTC()
	veryOld := now.Add(-120 * 24 * time.Hour).Format(time.RFC3339)

	entries := []stateFileEntry{
		{AgentName: "agent-001", KeyType: "ssh", KeyHash: "sha256:abc", CreatedAt: veryOld, Status: "active"},
	}
	path := writeStateFile(t, entries)

	var stdout, stderr strings.Builder
	rc := runRotateKeys([]string{"--state-file", path, "--at", now.Format(time.RFC3339)}, &stdout, &stderr)
	assert.Equal(t, rotateExitAction, rc)
	assert.Contains(t, stdout.String(), "age_exceeded")
}

// TestRunRotateKeys_JSONOutput verifies structured JSON output format.
func TestRunRotateKeys_JSONOutput(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-5 * 24 * time.Hour).Format(time.RFC3339)

	entries := []stateFileEntry{
		{AgentName: "agent-001", KeyType: "openrouter", KeyHash: "sha256:abc", CreatedAt: recent, Status: "dead"},
	}
	path := writeStateFile(t, entries)

	var stdout, stderr strings.Builder
	rc := runRotateKeys([]string{"--state-file", path, "--json", "--at", now.Format(time.RFC3339)}, &stdout, &stderr)
	assert.Equal(t, rotateExitAction, rc)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout.String()), &result))
	assert.Equal(t, float64(1), result["action_count"])
	assert.Equal(t, float64(0), result["skipped"])
	actions, ok := result["actions"].([]any)
	require.True(t, ok)
	assert.Len(t, actions, 1)
}

// TestRunRotateKeys_Execute_PrintsCommands verifies that --execute outputs shell commands.
func TestRunRotateKeys_Execute_PrintsCommands(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-5 * 24 * time.Hour).Format(time.RFC3339)

	entries := []stateFileEntry{
		{AgentName: "agent-001", KeyType: "ssh", KeyHash: "sha256:abc", CreatedAt: recent, Status: "dead"},
		{AgentName: "agent-002", KeyType: "pat", KeyHash: "sha256:def", CreatedAt: recent, Status: "dead"},
		{AgentName: "agent-003", KeyType: "openrouter", KeyHash: "sha256:ghi", CreatedAt: recent, Status: "dead"},
	}
	path := writeStateFile(t, entries)

	var stdout, stderr strings.Builder
	rc := runRotateKeys([]string{"--state-file", path, "--execute", "--at", now.Format(time.RFC3339)}, &stdout, &stderr)
	assert.Equal(t, rotateExitOK, rc)
	output := stdout.String()
	assert.Contains(t, output, "Executable Rotation Commands")
	assert.Contains(t, output, "helix-identity keygen")
	assert.Contains(t, output, "helix-identity pat-create")
	assert.Contains(t, output, "openrouter key create")
}

// TestRunRotateKeys_Execute_NoActions verifies --execute with no rotations needed.
func TestRunRotateKeys_Execute_NoActions(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-5 * 24 * time.Hour).Format(time.RFC3339)

	entries := []stateFileEntry{
		{AgentName: "agent-001", KeyType: "ssh", KeyHash: "sha256:abc", CreatedAt: recent, Status: "active"},
	}
	path := writeStateFile(t, entries)

	var stdout, stderr strings.Builder
	rc := runRotateKeys([]string{"--state-file", path, "--execute", "--at", now.Format(time.RFC3339)}, &stdout, &stderr)
	assert.Equal(t, rotateExitOK, rc)
	assert.Contains(t, stdout.String(), "no rotations needed")
}

// TestRunRotateKeys_JSONExecute verifies JSON output with commands included.
func TestRunRotateKeys_JSONExecute(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-5 * 24 * time.Hour).Format(time.RFC3339)

	entries := []stateFileEntry{
		{AgentName: "agent-001", KeyType: "ssh", KeyHash: "sha256:abc", CreatedAt: recent, Status: "dead"},
	}
	path := writeStateFile(t, entries)

	var stdout, stderr strings.Builder
	rc := runRotateKeys([]string{"--state-file", path, "--json", "--execute", "--at", now.Format(time.RFC3339)}, &stdout, &stderr)
	assert.Equal(t, rotateExitOK, rc)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout.String()), &result))
	commands, ok := result["commands"].([]any)
	require.True(t, ok)
	assert.Len(t, commands, 1)
	cmd := commands[0].(map[string]any)
	assert.Contains(t, cmd["command"].(string), "helix-identity keygen")
}

// TestRunRotateKeys_UnknownKeyType_Skipped verifies that unknown key types are silently skipped.
func TestRunRotateKeys_UnknownKeyType_Skipped(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-5 * 24 * time.Hour).Format(time.RFC3339)

	entries := []stateFileEntry{
		{AgentName: "agent-001", KeyType: "totp", KeyHash: "sha256:abc", CreatedAt: recent, Status: "active"},
	}
	path := writeStateFile(t, entries)

	var stdout, stderr strings.Builder
	rc := runRotateKeys([]string{"--state-file", path, "--at", now.Format(time.RFC3339)}, &stdout, &stderr)
	assert.Equal(t, rotateExitOK, rc)
}

// TestRunRotateKeys_MalformedJSON_Error verifies error handling for invalid JSON.
func TestRunRotateKeys_MalformedJSON_Error(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(path, []byte("{not valid json}"), 0o644))

	var stdout, stderr strings.Builder
	rc := runRotateKeys([]string{"--state-file", path}, &stdout, &stderr)
	assert.Equal(t, rotateExitError, rc)
	assert.Contains(t, stderr.String(), "parsing state file")
}

// TestRunRotateKeys_InvalidTimestamp_Error verifies error handling for bad --at value.
func TestRunRotateKeys_InvalidTimestamp_Error(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-5 * 24 * time.Hour).Format(time.RFC3339)

	entries := []stateFileEntry{
		{AgentName: "agent-001", KeyType: "ssh", KeyHash: "sha256:abc", CreatedAt: recent, Status: "active"},
	}
	path := writeStateFile(t, entries)

	var stdout, stderr strings.Builder
	rc := runRotateKeys([]string{"--state-file", path, "--at", "not-a-timestamp"}, &stdout, &stderr)
	assert.Equal(t, rotateExitError, rc)
	assert.Contains(t, stderr.String(), "invalid --at timestamp")
}

// TestRunRotateKeys_MultipleAgents verifies that multiple agents are all evaluated.
func TestRunRotateKeys_MultipleAgents(t *testing.T) {
	now := time.Now().UTC()
	veryOld := now.Add(-120 * 24 * time.Hour).Format(time.RFC3339)

	entries := []stateFileEntry{
		{AgentName: "agent-001", KeyType: "ssh", KeyHash: "sha256:abc", CreatedAt: veryOld, Status: "active"},
		{AgentName: "agent-002", KeyType: "ssh", KeyHash: "sha256:def", CreatedAt: veryOld, Status: "active"},
		{AgentName: "agent-003", KeyType: "openrouter", KeyHash: "sha256:ghi", CreatedAt: veryOld, Status: "active"},
	}
	path := writeStateFile(t, entries)

	var stdout, stderr strings.Builder
	rc := runRotateKeys([]string{"--state-file", path, "--at", now.Format(time.RFC3339)}, &stdout, &stderr)
	assert.Equal(t, rotateExitAction, rc)
	output := stdout.String()
	assert.Contains(t, output, "agent-001")
	assert.Contains(t, output, "agent-002")
	assert.Contains(t, output, "agent-003")
}

// TestRunRotateKeys_DryRunFlag verifies that --dry-run is accepted.
func TestRunRotateKeys_DryRunFlag(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-5 * 24 * time.Hour).Format(time.RFC3339)

	entries := []stateFileEntry{
		{AgentName: "agent-001", KeyType: "ssh", KeyHash: "sha256:abc", CreatedAt: recent, Status: "active"},
	}
	path := writeStateFile(t, entries)

	var stdout, stderr strings.Builder
	rc := runRotateKeys([]string{"--state-file", path, "--dry-run", "--at", now.Format(time.RFC3339)}, &stdout, &stderr)
	assert.Equal(t, rotateExitOK, rc)
}

// TestRunRotateKeys_UnknownFlag_Error verifies that unknown flags are rejected.
func TestRunRotateKeys_UnknownFlag_Error(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-5 * 24 * time.Hour).Format(time.RFC3339)

	entries := []stateFileEntry{
		{AgentName: "agent-001", KeyType: "ssh", KeyHash: "sha256:abc", CreatedAt: recent, Status: "active"},
	}
	path := writeStateFile(t, entries)

	var stdout, stderr strings.Builder
	rc := runRotateKeys([]string{"--state-file", path, "--bogus"}, &stdout, &stderr)
	assert.Equal(t, rotateExitError, rc)
}

// TestRunRotateKeys_PositionalArg_Error verifies that positional args are rejected.
func TestRunRotateKeys_PositionalArg_Error(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-5 * 24 * time.Hour).Format(time.RFC3339)

	entries := []stateFileEntry{
		{AgentName: "agent-001", KeyType: "ssh", KeyHash: "sha256:abc", CreatedAt: recent, Status: "active"},
	}
	path := writeStateFile(t, entries)

	var stdout, stderr strings.Builder
	rc := runRotateKeys([]string{"--state-file", path, "bogus-arg", "--at", now.Format(time.RFC3339)}, &stdout, &stderr)
	assert.Equal(t, rotateExitError, rc)
}

// TestRunRotateKeysWithDryRun_GlobalDryRun verifies that global dry-run adds the flag.
func TestRunRotateKeysWithDryRun_GlobalDryRun(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-5 * 24 * time.Hour).Format(time.RFC3339)

	entries := []stateFileEntry{
		{AgentName: "agent-001", KeyType: "ssh", KeyHash: "sha256:abc", CreatedAt: recent, Status: "active"},
	}
	path := writeStateFile(t, entries)

	var stdout, stderr strings.Builder
	err := runRotateKeysWithDryRun([]string{"--state-file", path, "--at", now.Format(time.RFC3339)}, &stdout, &stderr, true)
	assert.NoError(t, err)
}

// TestRunRotateKeysWithDryRun_RotationNeeded verifies that rotation needed is not treated as error.
func TestRunRotateKeysWithDryRun_RotationNeeded(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-5 * 24 * time.Hour).Format(time.RFC3339)

	entries := []stateFileEntry{
		{AgentName: "agent-001", KeyType: "ssh", KeyHash: "sha256:abc", CreatedAt: recent, Status: "dead"},
	}
	path := writeStateFile(t, entries)

	var stdout, stderr strings.Builder
	err := runRotateKeysWithDryRun([]string{"--state-file", path, "--at", now.Format(time.RFC3339)}, &stdout, &stderr, false)
	assert.NoError(t, err)
}

// TestRunRotateKeysWithDryRun_ErrorPropagated verifies that real errors propagate.
func TestRunRotateKeysWithDryRun_ErrorPropagated(t *testing.T) {
	var stdout, stderr strings.Builder
	err := runRotateKeysWithDryRun([]string{}, &stdout, &stderr, false)
	assert.Error(t, err)
}

// TestGenerateRotationCommand verifies the command generation for all key types.
func TestGenerateRotationCommand(t *testing.T) {
	tests := []struct {
		name     string
		action   identity.RotationAction
		contains string
	}{
		{
			name:     "ssh",
			action:   identity.RotationAction{AgentName: "agent-001", KeyType: identity.KeyTypeSSH},
			contains: "helix-identity keygen",
		},
		{
			name:     "pat",
			action:   identity.RotationAction{AgentName: "agent-002", KeyType: identity.KeyTypePAT},
			contains: "helix-identity pat-create",
		},
		{
			name:     "openrouter",
			action:   identity.RotationAction{AgentName: "agent-003", KeyType: identity.KeyTypeOpenRouter},
			contains: "openrouter key create",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := generateRotationCommand(tt.action)
			assert.Contains(t, cmd, tt.contains)
			assert.Contains(t, cmd, tt.action.AgentName)
		})
	}
}

// TestParseKeyType verifies key type string parsing.
func TestParseKeyType(t *testing.T) {
	valid := map[string]string{
		"ssh":        "ssh",
		"SSH":        "ssh",
		"pat":        "pat",
		"openrouter": "openrouter",
		"OpenRouter": "openrouter",
	}
	for input, expected := range valid {
		kt, ok := parseKeyType(input)
		assert.True(t, ok, "expected %q to be valid", input)
		assert.Equal(t, expected, string(kt))
	}

	_, ok := parseKeyType("totp")
	assert.False(t, ok)
	_, ok = parseKeyType("")
	assert.False(t, ok)
}

// TestParseRotateKeysFlags_StateFileLongValue verifies --state-file consumes the next arg.
func TestParseRotateKeysFlags_StateFileLongValue(t *testing.T) {
	flags, _, rc := parseRotateKeysFlags([]string{"--state-file", "/path/to/keys.json", "--json"})
	require.Equal(t, rotateExitOK, rc)
	assert.Equal(t, "/path/to/keys.json", flags.stateFile)
	assert.True(t, flags.jsonOut)
}

// TestParseRotateKeysFlags_Execute verifies the --execute flag.
func TestParseRotateKeysFlags_Execute(t *testing.T) {
	flags, _, rc := parseRotateKeysFlags([]string{"--execute"})
	require.Equal(t, rotateExitOK, rc)
	assert.True(t, flags.execute)
}

// TestParseRotateKeysFlags_MissingStateFileValue verifies error when --state-file has no value.
func TestParseRotateKeysFlags_MissingStateFileValue(t *testing.T) {
	_, _, rc := parseRotateKeysFlags([]string{"--state-file"})
	assert.Equal(t, rotateExitError, rc)
}

// TestParseRotateKeysFlags_MissingAtValue verifies error when --at has no value.
func TestParseRotateKeysFlags_MissingAtValue(t *testing.T) {
	_, _, rc := parseRotateKeysFlags([]string{"--at"})
	assert.Equal(t, rotateExitError, rc)
}

// TestParseRotateKeysFlags_AtFlag verifies the --at flag stores the timestamp string.
func TestParseRotateKeysFlags_AtFlag(t *testing.T) {
	flags, _, rc := parseRotateKeysFlags([]string{"--at", "2026-07-06T12:00:00Z"})
	require.Equal(t, rotateExitOK, rc)
	assert.Equal(t, "2026-07-06T12:00:00Z", flags.nowOverride)
}

// TestParseRotateKeysFlags_Help verifies --help returns helpWanted=true.
func TestParseRotateKeysFlags_Help(t *testing.T) {
	_, helpWanted, rc := parseRotateKeysFlags([]string{"--help"})
	require.Equal(t, rotateExitOK, rc)
	assert.True(t, helpWanted)
}
