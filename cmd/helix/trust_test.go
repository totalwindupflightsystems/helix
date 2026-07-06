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

	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

// writeTrustLedger writes a small JSONL trust ledger to a temp file.
func writeTrustLedger(t *testing.T, events []trust.TrustEvent) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.jsonl")

	var lines []string
	for _, evt := range events {
		raw, err := json.Marshal(evt)
		require.NoError(t, err)
		lines = append(lines, string(raw))
	}

	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644))
	return path
}

// makeMergeEvent creates a merge_success trust event.
func makeMergeEvent(agentID string, score float64, ts time.Time) trust.TrustEvent {
	return trust.TrustEvent{
		AgentID:   agentID,
		EventType: "merge_success",
		Timestamp: ts,
		Data: trust.EventData{
			PRURL:       "https://example.com/pr/1",
			ScoreBefore: score - 0.01,
			ScoreAfter:  score,
			Delta:       0.01,
		},
	}
}

// TestRunTrust_Help verifies --help shows help text.
func TestRunTrust_Help(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runTrust([]string{"--help"}, &stdout, &stderr)
	assert.Equal(t, trustExitOK, rc)
	assert.Contains(t, stdout.String(), "helix trust")
	assert.Contains(t, stdout.String(), "show")
	assert.Contains(t, stdout.String(), "history")
}

// TestRunTrust_HelpSubcommand verifies help subcommand.
func TestRunTrust_HelpSubcommand(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runTrust([]string{"help"}, &stdout, &stderr)
	assert.Equal(t, trustExitOK, rc)
	assert.Contains(t, stdout.String(), "helix trust")
}

// TestRunTrust_DefaultToHelp verifies no subcommand defaults to help.
func TestRunTrust_DefaultToHelp(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runTrust([]string{}, &stdout, &stderr)
	assert.Equal(t, trustExitOK, rc)
	assert.Contains(t, stdout.String(), "helix trust")
}

// TestRunTrust_UnknownSubcommand verifies error.
func TestRunTrust_UnknownSubcommand(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runTrust([]string{"bogus"}, &stdout, &stderr)
	assert.Equal(t, trustExitError, rc)
}

// TestRunTrustShow_NoLedger verifies error when --ledger missing.
func TestRunTrustShow_NoLedger(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runTrust([]string{"show", "--agent", "a"}, &stdout, &stderr)
	assert.Equal(t, trustExitError, rc)
	assert.Contains(t, stderr.String(), "--ledger")
}

// TestRunTrustShow_LedgerNotFound verifies exit code 3.
func TestRunTrustShow_LedgerNotFound(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runTrust([]string{"show", "--ledger", "/nonexistent/path.jsonl", "--agent", "a"}, &stdout, &stderr)
	assert.Equal(t, trustExitNotFound, rc)
}

// TestRunTrustShow_NoAgent verifies error when --agent missing.
func TestRunTrustShow_NoAgent(t *testing.T) {
	path := writeTrustLedger(t, []trust.TrustEvent{makeMergeEvent("a", 0.5, time.Now())})
	var stdout, stderr strings.Builder
	rc := runTrust([]string{"show", "--ledger", path}, &stdout, &stderr)
	assert.Equal(t, trustExitError, rc)
	assert.Contains(t, stderr.String(), "--agent")
}

// TestRunTrustShow_WithEvents verifies show produces output for an agent.
func TestRunTrustShow_WithEvents(t *testing.T) {
	now := time.Now().UTC()
	events := []trust.TrustEvent{
		makeMergeEvent("agent-001", 0.5, now.Add(-5*24*time.Hour)),
		makeMergeEvent("agent-001", 0.6, now.Add(-3*24*time.Hour)),
		makeMergeEvent("agent-001", 0.7, now.Add(-1*24*time.Hour)),
	}
	path := writeTrustLedger(t, events)

	var stdout, stderr strings.Builder
	rc := runTrust([]string{"show", "--ledger", path, "--agent", "agent-001"}, &stdout, &stderr)
	assert.Equal(t, trustExitOK, rc)
	assert.Contains(t, stdout.String(), "agent-001")
	assert.Contains(t, stdout.String(), "Score:")
	assert.Contains(t, stdout.String(), "Tier:")
}

// TestRunTrustShow_JSON verifies JSON output.
func TestRunTrustShow_JSON(t *testing.T) {
	now := time.Now().UTC()
	events := []trust.TrustEvent{
		makeMergeEvent("agent-001", 0.6, now.Add(-1*24*time.Hour)),
	}
	path := writeTrustLedger(t, events)

	var stdout, stderr strings.Builder
	rc := runTrust([]string{"show", "--ledger", path, "--agent", "agent-001", "--json"}, &stdout, &stderr)
	assert.Equal(t, trustExitOK, rc)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout.String()), &result))
	assert.Equal(t, "agent-001", result["agent_id"])
}

// TestRunTrustShow_NoData verifies exit code 1 when agent has no events.
func TestRunTrustShow_NoData(t *testing.T) {
	events := []trust.TrustEvent{makeMergeEvent("agent-001", 0.5, time.Now())}
	path := writeTrustLedger(t, events)

	var stdout, stderr strings.Builder
	rc := runTrust([]string{"show", "--ledger", path, "--agent", "nonexistent"}, &stdout, &stderr)
	assert.Equal(t, trustExitFail, rc)
}

// TestRunTrustHistory_NoTransitions verifies history with no tier changes.
func TestRunTrustHistory_NoTransitions(t *testing.T) {
	events := []trust.TrustEvent{makeMergeEvent("agent-001", 0.5, time.Now())}
	path := writeTrustLedger(t, events)

	var stdout, stderr strings.Builder
	rc := runTrust([]string{"history", "--ledger", path, "--agent", "agent-001"}, &stdout, &stderr)
	assert.Equal(t, trustExitOK, rc)
	assert.Contains(t, stdout.String(), "No tier transitions")
}

// TestRunTrustHistory_JSON verifies JSON output for history.
func TestRunTrustHistory_JSON(t *testing.T) {
	events := []trust.TrustEvent{makeMergeEvent("agent-001", 0.5, time.Now())}
	path := writeTrustLedger(t, events)

	var stdout, stderr strings.Builder
	rc := runTrust([]string{"history", "--ledger", path, "--agent", "agent-001", "--json"}, &stdout, &stderr)
	assert.Equal(t, trustExitOK, rc)
	assert.Contains(t, stdout.String(), "[")
}

// TestRunTrustList_MultipleAgents verifies listing all agents.
func TestRunTrustList_MultipleAgents(t *testing.T) {
	now := time.Now().UTC()
	events := []trust.TrustEvent{
		makeMergeEvent("agent-001", 0.5, now.Add(-1*24*time.Hour)),
		makeMergeEvent("agent-002", 0.7, now.Add(-1*24*time.Hour)),
		makeMergeEvent("agent-003", 0.3, now.Add(-1*24*time.Hour)),
	}
	path := writeTrustLedger(t, events)

	var stdout, stderr strings.Builder
	rc := runTrust([]string{"list", "--ledger", path}, &stdout, &stderr)
	assert.Equal(t, trustExitOK, rc)
	output := stdout.String()
	assert.Contains(t, output, "agent-001")
	assert.Contains(t, output, "agent-002")
	assert.Contains(t, output, "agent-003")
}

// TestRunTrustList_JSON verifies JSON listing.
func TestRunTrustList_JSON(t *testing.T) {
	now := time.Now().UTC()
	events := []trust.TrustEvent{
		makeMergeEvent("agent-001", 0.5, now.Add(-1*24*time.Hour)),
	}
	path := writeTrustLedger(t, events)

	var stdout, stderr strings.Builder
	rc := runTrust([]string{"list", "--ledger", path, "--json"}, &stdout, &stderr)
	assert.Equal(t, trustExitOK, rc)

	var result []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout.String()), &result))
	assert.GreaterOrEqual(t, len(result), 1)
}

// TestRunTrustList_EmptyLedger verifies empty ledger handling.
func TestRunTrustList_EmptyLedger(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(""), 0o644))

	var stdout, stderr strings.Builder
	rc := runTrust([]string{"list", "--ledger", path}, &stdout, &stderr)
	assert.Equal(t, trustExitOK, rc)
	assert.Contains(t, stdout.String(), "No agents found")
}

// TestRunTrustList_NoLedger verifies error.
func TestRunTrustList_NoLedger(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runTrust([]string{"list"}, &stdout, &stderr)
	assert.Equal(t, trustExitError, rc)
}

// TestTierName verifies tier name conversion.
func TestTierName(t *testing.T) {
	assert.Equal(t, "Provisional", tierName(trust.TierProvisional))
	assert.Equal(t, "Observed", tierName(trust.TierObserved))
	assert.Equal(t, "Trusted", tierName(trust.TierTrusted))
	assert.Equal(t, "Veteran", tierName(trust.TierVeteran))
	assert.Equal(t, "custom", tierName(trust.TrustTier("custom")))
}

// TestRunTrustWithDryRun_Success verifies wrapper on success.
func TestRunTrustWithDryRun_Success(t *testing.T) {
	var stdout, stderr strings.Builder
	err := runTrustWithDryRun([]string{"help"}, &stdout, &stderr, false)
	assert.NoError(t, err)
}

// TestRunTrustWithDryRun_ErrorPropagated verifies errors propagate.
func TestRunTrustWithDryRun_ErrorPropagated(t *testing.T) {
	var stdout, stderr strings.Builder
	err := runTrustWithDryRun([]string{"bogus"}, &stdout, &stderr, false)
	assert.Error(t, err)
}

// TestRunTrustWithDryRun_NoDataNotError verifies exit 1 is not treated as error.
func TestRunTrustWithDryRun_NoDataNotError(t *testing.T) {
	events := []trust.TrustEvent{makeMergeEvent("a", 0.5, time.Now())}
	path := writeTrustLedger(t, events)

	var stdout, stderr strings.Builder
	err := runTrustWithDryRun([]string{"show", "--ledger", path, "--agent", "nonexistent"}, &stdout, &stderr, false)
	assert.NoError(t, err)
}

// TestParseTrustFlags_Ledger verifies --ledger flag.
func TestParseTrustFlags_Ledger(t *testing.T) {
	flags, _, rc := parseTrustFlags([]string{"show", "--ledger", "/path/to/ledger.jsonl", "--agent", "a"})
	require.Equal(t, trustExitOK, rc)
	assert.Equal(t, "/path/to/ledger.jsonl", flags.ledger)
	assert.Equal(t, "a", flags.agent)
	assert.Equal(t, "show", flags.subcommand)
}

// TestParseTrustFlags_Days verifies --days flag.
func TestParseTrustFlags_Days(t *testing.T) {
	flags, _, rc := parseTrustFlags([]string{"show", "--days", "60"})
	require.Equal(t, trustExitOK, rc)
	assert.Equal(t, 60, flags.days)
}

// TestParseTrustFlags_Help verifies --help.
func TestParseTrustFlags_Help(t *testing.T) {
	_, helpWanted, rc := parseTrustFlags([]string{"--help"})
	require.Equal(t, trustExitOK, rc)
	assert.True(t, helpWanted)
}

// TestParseTrustFlags_MissingLedgerValue verifies error.
func TestParseTrustFlags_MissingLedgerValue(t *testing.T) {
	_, _, rc := parseTrustFlags([]string{"show", "--ledger"})
	assert.Equal(t, trustExitError, rc)
}

// TestParseTrustFlags_UnknownFlag verifies error.
func TestParseTrustFlags_UnknownFlag(t *testing.T) {
	_, _, rc := parseTrustFlags([]string{"show", "--bogus"})
	assert.Equal(t, trustExitError, rc)
}
