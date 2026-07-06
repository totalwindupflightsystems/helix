package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Flag parsing tests
// ============================================================================

func TestParseLifecycleFlags_DefaultsToHelp(t *testing.T) {
	flags, helpWanted, rc := parseLifecycleFlags([]string{})
	assert.Equal(t, lcExitOK, rc)
	assert.False(t, helpWanted)
	assert.Equal(t, "help", flags.subcommand)
}

func TestParseLifecycleFlags_ExplicitHelp(t *testing.T) {
	flags, helpWanted, rc := parseLifecycleFlags([]string{"--help"})
	assert.Equal(t, lcExitOK, rc)
	assert.True(t, helpWanted)
	assert.Equal(t, "help", flags.subcommand)
}

func TestParseLifecycleFlags_RunSubcommand(t *testing.T) {
	flags, _, rc := parseLifecycleFlags([]string{"run", "--repo", "org/name", "--pr", "42"})
	require.Equal(t, lcExitOK, rc)
	assert.Equal(t, "run", flags.subcommand)
	assert.Equal(t, "org/name", flags.repo)
	assert.Equal(t, 42, flags.pr)
}

func TestParseLifecycleFlags_PRURL(t *testing.T) {
	flags, _, rc := parseLifecycleFlags([]string{"run", "--pr-url", "http://forgejo:3030/org/repo/pulls/99"})
	require.Equal(t, lcExitOK, rc)
	assert.Equal(t, "http://forgejo:3030/org/repo/pulls/99", flags.prURL)
}

func TestParseLifecycleFlags_AllStageFlags(t *testing.T) {
	flags, _, rc := parseLifecycleFlags([]string{
		"run", "--repo", "r", "--pr", "1",
		"--agent", "agent-x", "--trust", "trusted",
		"--stages", "cost,review,mergegate", "--dry-run", "--json",
	})
	require.Equal(t, lcExitOK, rc)
	assert.Equal(t, "agent-x", flags.agent)
	assert.Equal(t, "trusted", flags.trustTier)
	assert.Equal(t, "cost,review,mergegate", flags.stages)
	assert.True(t, flags.dryRun)
	assert.True(t, flags.jsonOut)
}

func TestParseLifecycleFlags_MissingFlagValue(t *testing.T) {
	for _, arg := range []string{"--repo", "--pr", "--pr-url", "--agent", "--trust", "--stages"} {
		_, _, rc := parseLifecycleFlags([]string{arg})
		assert.Equal(t, lcExitError, rc, "missing value for %s should error", arg)
	}
}

func TestParseLifecycleFlags_UnknownLongFlag(t *testing.T) {
	_, _, rc := parseLifecycleFlags([]string{"run", "--bogus"})
	assert.Equal(t, lcExitError, rc)
}

func TestParseLifecycleFlags_ScanErrorFlag(t *testing.T) {
	flags, _, rc := parseLifecycleFlags([]string{"run", "--repo", "r", "--pr", "not-a-number"})
	require.Equal(t, lcExitOK, rc)
	assert.Equal(t, 0, flags.pr) // Sscanf fails silently → 0
}

// ============================================================================
// Stage list parsing tests
// ============================================================================

func TestParseStageList_All(t *testing.T) {
	stages := parseStageList("cost,review,negotiation,mergegate,shadow")
	assert.Len(t, stages, 5)
}

func TestParseStageList_Subset(t *testing.T) {
	stages := parseStageList("cost,review")
	assert.Len(t, stages, 2)
}

func TestParseStageList_Empty(t *testing.T) {
	stages := parseStageList("")
	assert.Empty(t, stages)
}

func TestParseStageList_Whitespace(t *testing.T) {
	stages := parseStageList(" cost , review , mergegate ")
	assert.Len(t, stages, 3)
}

func TestParseStageList_UnknownStage(t *testing.T) {
	stages := parseStageList("cost,nonexistent,review")
	// Unknown stages are silently skipped.
	assert.Len(t, stages, 2)
}

func TestStageIDFromName_All(t *testing.T) {
	assert.NotEmpty(t, stageIDFromName("cost"))
	assert.NotEmpty(t, stageIDFromName("review"))
	assert.NotEmpty(t, stageIDFromName("negotiation"))
	assert.NotEmpty(t, stageIDFromName("mergegate"))
	assert.NotEmpty(t, stageIDFromName("shadow"))
	assert.NotEmpty(t, stageIDFromName("surveillance"))
	assert.Empty(t, stageIDFromName("nonexistent"))
}

// ============================================================================
// Subcommand dispatch tests
// ============================================================================

func TestRunLifecycle_Help(t *testing.T) {
	var out bytes.Buffer
	rc := runLifecycle([]string{"help"}, &out, &out)
	assert.Equal(t, lcExitOK, rc)
	assert.Contains(t, out.String(), "helix lifecycle")
}

func TestRunLifecycle_HelpFlag(t *testing.T) {
	var out bytes.Buffer
	rc := runLifecycle([]string{"--help"}, &out, &out)
	assert.Equal(t, lcExitOK, rc)
	assert.Contains(t, out.String(), "helix lifecycle")
}

func TestRunLifecycle_UnknownSubcommand(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runLifecycle([]string{"bogus"}, &out, &errBuf)
	assert.Equal(t, lcExitError, rc)
	assert.Contains(t, errBuf.String(), "unknown subcommand")
}

func TestRunLifecycle_RunMissingRepo(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runLifecycle([]string{"run"}, &out, &errBuf)
	assert.Equal(t, lcExitError, rc)
	assert.Contains(t, errBuf.String(), "--repo")
}

func TestRunLifecycle_RunInvalidTier(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runLifecycle([]string{"run", "--repo", "r", "--pr", "1", "--trust", "nonsense"}, &out, &errBuf)
	assert.Equal(t, lcExitError, rc)
	assert.Contains(t, errBuf.String(), "invalid trust tier")
}

func TestRunLifecycle_RunDryRunJSON(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runLifecycle([]string{"run", "--repo", "org/name", "--pr", "5", "--dry-run", "--json"}, &out, &errBuf)
	assert.Equal(t, lcExitOK, rc)
	assert.Contains(t, out.String(), "\"dry_run\": true")
	assert.Contains(t, out.String(), "\"stages\"")
}

func TestRunLifecycle_RunDryRunText(t *testing.T) {
	var out bytes.Buffer
	rc := runLifecycle([]string{"run", "--repo", "org/name", "--pr", "5", "--dry-run"}, &out, &out)
	assert.Equal(t, lcExitOK, rc)
	assert.Contains(t, out.String(), "[DRY RUN]")
	assert.Contains(t, out.String(), "cost")
}

func TestRunLifecycle_RunDryRunSubset(t *testing.T) {
	var out bytes.Buffer
	rc := runLifecycle([]string{"run", "--pr-url", "http://x", "--dry-run", "--stages", "review,mergegate"}, &out, &out)
	assert.Equal(t, lcExitOK, rc)
	assert.Contains(t, out.String(), "Adversarial Review")
	assert.Contains(t, out.String(), "Merge Gate")
	assert.NotContains(t, out.String(), "Cost Estimate")
}

func TestRunLifecycle_StagesJSON(t *testing.T) {
	var out bytes.Buffer
	rc := runLifecycle([]string{"stages", "--json"}, &out, &out)
	assert.Equal(t, lcExitOK, rc)

	var items []map[string]any
	err := json.Unmarshal(out.Bytes(), &items)
	require.NoError(t, err)
	assert.Len(t, items, len(lcAllStages))
	assert.NotEmpty(t, items)
}

func TestRunLifecycle_StagesText(t *testing.T) {
	var out bytes.Buffer
	rc := runLifecycle([]string{"stages"}, &out, &out)
	assert.Equal(t, lcExitOK, rc)
	assert.Contains(t, out.String(), "Lifecycle Stages")
	assert.Contains(t, out.String(), "Cost Estimate")
}

// ============================================================================
// Help text
// ============================================================================

func TestPrintLifecycleHelp_Content(t *testing.T) {
	var buf bytes.Buffer
	printLifecycleHelp(&buf)
	s := buf.String()
	for _, want := range []string{"helix lifecycle", "--repo", "--pr", "--stages", "--dry-run", "Exit codes:"} {
		assert.Contains(t, s, want)
	}
}

// ============================================================================
// Wrapper
// ============================================================================

func TestRunLifecycleWithDryRun_OK(t *testing.T) {
	var out, errBuf bytes.Buffer
	err := runLifecycleWithDryRun([]string{"help"}, &out, &errBuf, false)
	assert.NoError(t, err)
}

func TestRunLifecycleWithDryRun_PropagatesError(t *testing.T) {
	var out, errBuf bytes.Buffer
	err := runLifecycleWithDryRun([]string{"bogus"}, &out, &errBuf, false)
	assert.Error(t, err)
	assert.Equal(t, lcExitError, extractExitCode(t, err))
}

func TestRunLifecycleWithDryRun_GlobalDryRunInjected(t *testing.T) {
	// When globalDryRun=true and the user did not pass --dry-run, the wrapper
	// injects --dry-run so the subcommand's own handler activates. Verify by
	// running "stages" with globalDryRun=true and checking for [DRY RUN] text.
	// Note: "stages" doesn't read --dry-run; "run" does. Use "run --stages cost".
	var out bytes.Buffer
	err := runLifecycleWithDryRun([]string{"run", "--pr-url", "http://x", "--stages", "cost"}, &out, &out, true)
	assert.NoError(t, err)
	assert.Contains(t, out.String(), "[DRY RUN]")
}

func TestRunLifecycleWithDryRun_UserDryRunNotDuplicated(t *testing.T) {
	// User explicitly passed --dry-run; wrapper should not inject a second one.
	var out bytes.Buffer
	err := runLifecycleWithDryRun([]string{"run", "--pr-url", "http://x", "--stages", "cost", "--dry-run"}, &out, &out, true)
	assert.NoError(t, err)
	// Single [DRY RUN] header, not duplicated.
	assert.Equal(t, 1, strings.Count(out.String(), "[DRY RUN]"))
}

func TestHasLifecycleDryRun(t *testing.T) {
	assert.True(t, hasLifecycleDryRun([]string{"--dry-run"}))
	assert.True(t, hasLifecycleDryRun([]string{"run", "--dry-run", "--repo", "r"}))
	assert.False(t, hasLifecycleDryRun([]string{"run", "--repo", "r"}))
	assert.False(t, hasLifecycleDryRun(nil))
	assert.False(t, hasLifecycleDryRun([]string{}))
}

// extractExitCode returns the code embedded in errExit.
func extractExitCode(t *testing.T, err error) int {
	t.Helper()
	if e, ok := err.(errExit); ok {
		return e.code
	}
	t.Fatalf("unexpected error type: %T", err)
	return -1
}

// ============================================================================
// Sanity: parseStageList never panics on garbage
// ============================================================================

func TestParseStageList_GarbageInput(t *testing.T) {
	for _, in := range []string{",,,", " ", strings.Repeat("x", 1000)} {
		assert.NotPanics(t, func() {
			_ = parseStageList(in)
		})
	}
}
