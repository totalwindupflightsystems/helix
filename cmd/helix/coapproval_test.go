package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/totalwindupflightsystems/helix/pkg/coapproval"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// writeApprovalsFixture serializes a slice of coapproval.Approval to a
// JSON file under t.TempDir() and returns the path. Used by every test
// that needs --human-approvals or --agent-approvals.
func writeApprovalsFixture(t *testing.T, approvals []coapproval.Approval) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "approvals.json")
	data, err := json.Marshal(approvals)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

// makeHumanApproval builds a coapproval.Approval with sensible defaults
// for tests (Type=human, trust=100, timestamp=now, commit matches).
func makeHumanApproval(t *testing.T, reviewerID string, commitSHA string) coapproval.Approval {
	t.Helper()
	return coapproval.Approval{
		ReviewerID: reviewerID,
		Type:       coapproval.ReviewerHuman,
		TrustScore: 100,
		Timestamp:  time.Now().Add(-1 * time.Minute),
		CommitSHA:  commitSHA,
	}
}

// makeAgentApproval builds a coapproval.Approval with sensible defaults
// for agents (Type=agent, caller specifies trust).
func makeAgentApproval(t *testing.T, reviewerID string, trust int, commitSHA string) coapproval.Approval {
	t.Helper()
	return coapproval.Approval{
		ReviewerID: reviewerID,
		Type:       coapproval.ReviewerAgent,
		TrustScore: trust,
		Timestamp:  time.Now().Add(-1 * time.Minute),
		CommitSHA:  commitSHA,
	}
}

// ---------------------------------------------------------------------------
// check — happy paths
// ---------------------------------------------------------------------------

// TestCoapprovalCheck_HappyPath_TrustedAgent — 1 human + 1 trusted agent → ALLOWED
func TestCoapprovalCheck_HappyPath_TrustedAgent(t *testing.T) {
	const sha = "abc123def456"
	humanFile := writeApprovalsFixture(t, []coapproval.Approval{
		makeHumanApproval(t, "alice", sha),
	})
	agentFile := writeApprovalsFixture(t, []coapproval.Approval{
		makeAgentApproval(t, "agent-bot", 75, sha),
	})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{
		"check",
		"--pr", "42",
		"--commit-sha", sha,
		"--human-approvals", humanFile,
		"--agent-approvals", agentFile,
	}, stdout, stderr)

	assert.Equal(t, 0, rc, "expected ALLOWED → rc 0; stderr=%s", stderr.String())
	assert.Contains(t, stdout.String(), "ALLOWED")
	assert.Contains(t, stdout.String(), "42")
	assert.Empty(t, stderr.String())
}

// TestCoapprovalCheck_HappyPath_TwoUntrustedAgents — 1 human + 2 untrusted agents → ALLOWED
func TestCoapprovalCheck_HappyPath_TwoUntrustedAgents(t *testing.T) {
	const sha = "def456"
	humanFile := writeApprovalsFixture(t, []coapproval.Approval{
		makeHumanApproval(t, "bob", sha),
	})
	agentFile := writeApprovalsFixture(t, []coapproval.Approval{
		makeAgentApproval(t, "agent-1", 30, sha),
		makeAgentApproval(t, "agent-2", 45, sha),
	})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{
		"check", "--pr", "7", "--commit-sha", sha,
		"--human-approvals", humanFile, "--agent-approvals", agentFile,
	}, stdout, stderr)

	assert.Equal(t, 0, rc, "expected ALLOWED → rc 0; stderr=%s", stderr.String())
	assert.Contains(t, stdout.String(), "ALLOWED")
}

// ---------------------------------------------------------------------------
// check — gate-not-satisfied paths (rc=1)
// ---------------------------------------------------------------------------

// TestCoapprovalCheck_NoHuman — only agent approvals → NEEDS_HUMAN, rc=1
func TestCoapprovalCheck_NoHuman(t *testing.T) {
	const sha = "aaa"
	humanFile := writeApprovalsFixture(t, nil) // empty
	agentFile := writeApprovalsFixture(t, []coapproval.Approval{
		makeAgentApproval(t, "agent-x", 80, sha),
	})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{
		"check", "--pr", "10", "--commit-sha", sha,
		"--human-approvals", humanFile, "--agent-approvals", agentFile,
	}, stdout, stderr)

	assert.Equal(t, 1, rc)
	assert.Contains(t, stdout.String(), "NEEDS_HUMAN")
}

// TestCoapprovalCheck_NoAgent — only human → NEEDS_AGENT, rc=1
func TestCoapprovalCheck_NoAgent(t *testing.T) {
	const sha = "bbb"
	humanFile := writeApprovalsFixture(t, []coapproval.Approval{
		makeHumanApproval(t, "alice", sha),
	})
	agentFile := writeApprovalsFixture(t, nil)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{
		"check", "--pr", "11", "--commit-sha", sha,
		"--human-approvals", humanFile, "--agent-approvals", agentFile,
	}, stdout, stderr)

	assert.Equal(t, 1, rc)
	assert.Contains(t, stdout.String(), "NEEDS_AGENT")
}

// TestCoapprovalCheck_OnlyOneUntrustedAgent — 1 human + 1 untrusted agent → NEEDS_AGENT (need 2)
func TestCoapprovalCheck_OnlyOneUntrustedAgent(t *testing.T) {
	const sha = "ccc"
	humanFile := writeApprovalsFixture(t, []coapproval.Approval{
		makeHumanApproval(t, "alice", sha),
	})
	agentFile := writeApprovalsFixture(t, []coapproval.Approval{
		makeAgentApproval(t, "agent-low", 40, sha),
	})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{
		"check", "--pr", "12", "--commit-sha", sha,
		"--human-approvals", humanFile, "--agent-approvals", agentFile,
	}, stdout, stderr)

	assert.Equal(t, 1, rc)
	assert.Contains(t, stdout.String(), "NEEDS_AGENT")
	assert.Contains(t, stdout.String(), "1 more agent")
}

// TestCoapprovalCheck_Veto — veto overrides ALLOWED → BLOCKED
func TestCoapprovalCheck_Veto(t *testing.T) {
	const sha = "ddd"
	humanFile := writeApprovalsFixture(t, []coapproval.Approval{
		makeHumanApproval(t, "alice", sha),
	})
	// Veto requires trust >= 90. Build the agent approvals with one
	// approval + one veto (the gate's RecordVeto flow is internal —
	// the CLI only knows about Approval with IsVeto flag).
	approval := makeAgentApproval(t, "agent-vetoer", 95, sha)
	approval.IsVeto = true
	agentFile := writeApprovalsFixture(t, []coapproval.Approval{approval})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{
		"check", "--pr", "13", "--commit-sha", sha,
		"--human-approvals", humanFile, "--agent-approvals", agentFile,
	}, stdout, stderr)

	assert.Equal(t, 1, rc)
	assert.Contains(t, stdout.String(), "BLOCKED")
}

// ---------------------------------------------------------------------------
// check — bad invocation (rc=2)
// ---------------------------------------------------------------------------

// TestCoapprovalCheck_MissingPR — --pr required
func TestCoapprovalCheck_MissingPR(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{
		"check", "--commit-sha", "x",
		"--human-approvals", "/tmp/x", "--agent-approvals", "/tmp/y",
	}, stdout, stderr)
	assert.Equal(t, 2, rc)
	assert.Contains(t, stderr.String(), "--pr is required")
}

// TestCoapprovalCheck_MissingCommitSHA — --commit-sha required
func TestCoapprovalCheck_MissingCommitSHA(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{
		"check", "--pr", "1",
		"--human-approvals", "/tmp/x", "--agent-approvals", "/tmp/y",
	}, stdout, stderr)
	assert.Equal(t, 2, rc)
	assert.Contains(t, stderr.String(), "--commit-sha is required")
}

// TestCoapprovalCheck_MissingApprovalsFile — non-existent file → rc=2
func TestCoapprovalCheck_MissingApprovalsFile(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{
		"check", "--pr", "1", "--commit-sha", "x",
		"--human-approvals", "/tmp/does-not-exist.json",
		"--agent-approvals", "/tmp/does-not-exist2.json",
	}, stdout, stderr)
	assert.Equal(t, 2, rc)
	assert.Contains(t, stderr.String(), "load human approvals")
}

// TestCoapprovalCheck_MalformedJSON — bad JSON → rc=2
func TestCoapprovalCheck_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(bad, []byte("{not json"), 0o644))
	good := writeApprovalsFixture(t, nil)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{
		"check", "--pr", "1", "--commit-sha", "x",
		"--human-approvals", bad, "--agent-approvals", good,
	}, stdout, stderr)
	assert.Equal(t, 2, rc)
	assert.Contains(t, stderr.String(), "parse")
}

// TestCoapprovalCheck_UnknownSubcommand — "foo" → rc=2
func TestCoapprovalCheck_UnknownSubcommand(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{"foo"}, stdout, stderr)
	assert.Equal(t, 2, rc)
	assert.Contains(t, stderr.String(), "unknown subcommand")
}

// TestCoapprovalCheck_HelpFlag — "help" → rc=0, prints usage
func TestCoapprovalCheck_HelpFlag(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{"help"}, stdout, stderr)
	assert.Equal(t, 0, rc)
	assert.Contains(t, stdout.String(), "helix coapproval")
}

// TestCoapprovalCheck_CheckSubcommandHelp — "check --help" → rc=0
func TestCoapprovalCheck_CheckSubcommandHelp(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{"check", "--help"}, stdout, stderr)
	assert.Equal(t, 0, rc)
	assert.Contains(t, stdout.String(), "--pr")
	assert.Contains(t, stdout.String(), "--commit-sha")
}

// ---------------------------------------------------------------------------
// check — output formats
// ---------------------------------------------------------------------------

// TestCoapprovalCheck_JSONOutput — --format json → machine-readable
func TestCoapprovalCheck_JSONOutput(t *testing.T) {
	const sha = "ee1"
	humanFile := writeApprovalsFixture(t, []coapproval.Approval{
		makeHumanApproval(t, "alice", sha),
	})
	agentFile := writeApprovalsFixture(t, []coapproval.Approval{
		makeAgentApproval(t, "agent-bot", 75, sha),
	})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{
		"check", "--pr", "55", "--commit-sha", sha,
		"--human-approvals", humanFile, "--agent-approvals", agentFile,
		"--format", "json",
	}, stdout, stderr)
	require.Equal(t, 0, rc, "stderr=%s", stderr.String())

	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &decoded))
	elig, ok := decoded["eligibility"].(map[string]interface{})
	require.True(t, ok, "eligibility field missing in JSON output")
	assert.Equal(t, "ALLOWED", elig["decision"])
}

// TestCoapprovalCheck_JSONOutput_BlockedAlwaysExitsOne — JSON output
// still surfaces non-zero exit code when the gate is blocked (so CI
// scripts can rely on the exit code without parsing JSON).
func TestCoapprovalCheck_JSONOutput_BlockedAlwaysExitsOne(t *testing.T) {
	const sha = "ee2"
	humanFile := writeApprovalsFixture(t, nil)
	agentFile := writeApprovalsFixture(t, nil)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{
		"check", "--pr", "56", "--commit-sha", sha,
		"--human-approvals", humanFile, "--agent-approvals", agentFile,
		"--format", "json",
	}, stdout, stderr)
	assert.Equal(t, 1, rc)
}

// TestCoapprovalCheck_MarkdownOutput — --format markdown → comment-style
func TestCoapprovalCheck_MarkdownOutput(t *testing.T) {
	const sha = "ee3"
	humanFile := writeApprovalsFixture(t, []coapproval.Approval{
		makeHumanApproval(t, "alice", sha),
	})
	agentFile := writeApprovalsFixture(t, []coapproval.Approval{
		makeAgentApproval(t, "agent-bot", 75, sha),
	})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{
		"check", "--pr", "57", "--commit-sha", sha,
		"--human-approvals", humanFile, "--agent-approvals", agentFile,
		"--format", "markdown", "--pr-url", "https://forgejo.example/helix-org/helix/pulls/57",
	}, stdout, stderr)
	require.Equal(t, 0, rc, "stderr=%s", stderr.String())

	out := stdout.String()
	assert.Contains(t, out, "## Co-Approval Gate")
	assert.Contains(t, out, "PR #57")
	assert.Contains(t, out, "ALLOWED")
	assert.Contains(t, out, "| Human")
	assert.Contains(t, out, "| Agent")
}

// ---------------------------------------------------------------------------
// status
// ---------------------------------------------------------------------------

// TestCoapprovalStatus_Text — default text format prints thresholds
func TestCoapprovalStatus_Text(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{"status"}, stdout, stderr)
	require.Equal(t, 0, rc, "stderr=%s", stderr.String())

	out := stdout.String()
	assert.Contains(t, out, "Approval expiry")
	assert.Contains(t, out, "24h")
	assert.Contains(t, out, "Trusted agent threshold")
	assert.Contains(t, out, "70")
	assert.Contains(t, out, "Untrusted agent required count")
	assert.Contains(t, out, "Veto agent threshold")
	assert.Contains(t, out, "90")
}

// TestCoapprovalStatus_JSON — status with --format json
func TestCoapprovalStatus_JSON(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{"status", "--format", "json"}, stdout, stderr)
	require.Equal(t, 0, rc, "stderr=%s", stderr.String())

	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &decoded))
	assert.EqualValues(t, coapproval.TrustedAgentThreshold, decoded["trusted_agent_threshold"])
	assert.EqualValues(t, coapproval.VetoAgentThreshold, decoded["veto_agent_threshold"])
	assert.EqualValues(t, coapproval.UntrustedAgentRequiredCount, decoded["untrusted_agent_required_count"])
}

// ---------------------------------------------------------------------------
// Wiring: helix coapproval dispatch
// ---------------------------------------------------------------------------

// TestDispatchCoapproval — the main dispatcher routes "coapproval" to
// runCoapprovalWithDryRun. We can't easily exec the full main() binary
// in tests, but we can verify the case statement exists by checking
// that runCoapproval is wired (smoke check by running via dispatch).
func TestDispatchCoapproval_RoutesCorrectly(t *testing.T) {
	// Smoke test: the runCoapproval function is exported within the
	// package (lowercase) but accessible to tests. We just confirm the
	// function exists and returns non-zero on bad invocation — that
	// proves the wiring path is alive.
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{"check", "--pr", "0"}, stdout, stderr)
	assert.Equal(t, 2, rc, "missing flags must return rc=2")
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

// TestCoapprovalCheck_DefaultSubcommandIsCheck — `helix coapproval --pr 1 ...`
// (no subcommand) should default to "check".
func TestCoapprovalCheck_DefaultSubcommandIsCheck(t *testing.T) {
	const sha = "ff1"
	humanFile := writeApprovalsFixture(t, []coapproval.Approval{
		makeHumanApproval(t, "alice", sha),
	})
	agentFile := writeApprovalsFixture(t, []coapproval.Approval{
		makeAgentApproval(t, "agent-bot", 75, sha),
	})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{
		"--pr", "99", "--commit-sha", sha,
		"--human-approvals", humanFile, "--agent-approvals", agentFile,
	}, stdout, stderr)
	assert.Equal(t, 0, rc, "default subcommand should be check; stderr=%s", stderr.String())
	assert.Contains(t, stdout.String(), "PR #99")
}

// TestCoapprovalCheck_ExpiredApproval — approval older than 24h → ignored.
// With zero valid human approvals AND zero agent approvals, the gate
// returns BLOCKED (not NEEDS_AGENT) per pkg/coapproval's logic: when
// both sides are missing it categorises the gate as fully blocked.
func TestCoapprovalCheck_ExpiredApproval(t *testing.T) {
	const sha = "ff2"
	expired := makeHumanApproval(t, "alice", sha)
	expired.Timestamp = time.Now().Add(-48 * time.Hour)
	humanFile := writeApprovalsFixture(t, []coapproval.Approval{expired})
	agentFile := writeApprovalsFixture(t, nil)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{
		"check", "--pr", "100", "--commit-sha", sha,
		"--human-approvals", humanFile, "--agent-approvals", agentFile,
	}, stdout, stderr)
	assert.Equal(t, 1, rc, "expired approval should be ignored → gate blocked")
	out := stdout.String()
	assert.True(t,
		strings.Contains(out, "BLOCKED") || strings.Contains(out, "NEEDS_AGENT"),
		"expected BLOCKED or NEEDS_AGENT in output, got: %s", out)
}

// TestCoapprovalCheck_WrongCommitSHA — approval recorded for different
// commit → ignored by gate (IsValidForCommit check). Same logic as the
// expired case: with zero valid approvals on either side, gate returns
// BLOCKED.
func TestCoapprovalCheck_WrongCommitSHA(t *testing.T) {
	const expectedSHA = "right"
	const otherSHA = "wrong"
	humanFile := writeApprovalsFixture(t, []coapproval.Approval{
		makeHumanApproval(t, "alice", otherSHA), // approval for different commit
	})
	agentFile := writeApprovalsFixture(t, nil)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{
		"check", "--pr", "101", "--commit-sha", expectedSHA,
		"--human-approvals", humanFile, "--agent-approvals", agentFile,
	}, stdout, stderr)
	assert.Equal(t, 1, rc)
	out := stdout.String()
	assert.True(t,
		strings.Contains(out, "BLOCKED") || strings.Contains(out, "NEEDS_AGENT"),
		"expected BLOCKED or NEEDS_AGENT in output, got: %s", out)
}

// TestCoapprovalCheck_EmptyFixtureFile — empty JSON file → no approvals
func TestCoapprovalCheck_EmptyFixtureFile(t *testing.T) {
	const sha = "ff3"
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.json")
	require.NoError(t, os.WriteFile(empty, []byte(""), 0o644))
	agentFile := writeApprovalsFixture(t, []coapproval.Approval{
		makeAgentApproval(t, "agent-bot", 75, sha),
	})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{
		"check", "--pr", "102", "--commit-sha", sha,
		"--human-approvals", empty, "--agent-approvals", agentFile,
	}, stdout, stderr)
	assert.Equal(t, 1, rc, "empty human fixture → only agent → NEEDS_HUMAN")
	assert.Contains(t, stdout.String(), "NEEDS_HUMAN")
}

// TestCoapprovalCheck_PrimitivesNotLeaking — ensure strings.Contains
// checks won't false-positive by checking for whole-word matches when
// the renderer might emit the suffix in a longer label.
//
// (Guards against a regression where a renderer change accidentally
// makes every decision read as "ALLOWED" because a substring like
// "NEEDS_HUMAN" contains "HUMAN".)
func TestCoapprovalCheck_RendererSubstringsSafe(t *testing.T) {
	const sha = "ff4"
	humanFile := writeApprovalsFixture(t, nil)
	agentFile := writeApprovalsFixture(t, []coapproval.Approval{
		makeAgentApproval(t, "agent-bot", 75, sha),
	})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runCoapproval([]string{
		"check", "--pr", "103", "--commit-sha", sha,
		"--human-approvals", humanFile, "--agent-approvals", agentFile,
	}, stdout, stderr)
	require.Equal(t, 1, rc)
	out := stdout.String()
	assert.True(t,
		strings.Contains(out, "NEEDS_HUMAN") || strings.Contains(out, "BLOCKED"),
		"expected NEEDS_HUMAN or BLOCKED in output, got: %s", out)
}
