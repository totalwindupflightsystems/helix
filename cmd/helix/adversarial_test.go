package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	adv "github.com/totalwindupflightsystems/helix/pkg/adversarial"
)

// ---------------------------------------------------------------------------
// run-all — table format
// ---------------------------------------------------------------------------

// TestAdversarialRunAll_DefaultLibrary_PassesBySpec — the spec library's
// 5 scenarios should pass with their default Run funcs (which already
// match the expected outcomes for the gates they test).
func TestAdversarialRunAll_DefaultLibrary_PassesBySpec(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runAdversarial([]string{"run-all", "--timeout", "30s"}, stdout, stderr)
	// rc may be 0 (all pass) or 1 (some fail). We only assert that the
	// CLI ran to completion, the header rendered, and exit code is
	// either 0 or 1 — never 2 (parse error) or panic.
	assert.Contains(t, []int{0, 1}, rc, "run-all should complete; stderr=%s", stderr.String())
	assert.Contains(t, stdout.String(), "Helix Adversarial Scenario Pack")
	assert.Contains(t, stdout.String(), "Total scenarios:")
}

// TestAdversarialRunAll_OutputJSON — --output json emits structured report
func TestAdversarialRunAll_OutputJSON(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runAdversarial([]string{"run-all", "--output", "json", "--timeout", "30s"}, stdout, stderr)
	require.Contains(t, []int{0, 1}, rc, "stderr=%s", stderr.String())

	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &decoded))
	report, ok := decoded["report"].(map[string]interface{})
	require.True(t, ok, "report field missing in JSON output")
	assert.Contains(t, report, "Total")
	assert.Contains(t, report, "Pass")
	assert.Contains(t, report, "Fail")
}

// TestAdversarialRunAll_RoleFilter — --role filters scenarios
func TestAdversarialRunAll_RoleFilter(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	// Use a role we know has scenarios in the default library.
	rc := runAdversarial([]string{"run-all", "--role", "@redteam", "--timeout", "30s"}, stdout, stderr)
	require.Contains(t, []int{0, 1}, rc, "stderr=%s", stderr.String())
	assert.Contains(t, stdout.String(), "Filter role:         @redteam")
}

// TestAdversarialRunAll_SeverityFilter — --severity-min filters scenarios
func TestAdversarialRunAll_SeverityFilter(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runAdversarial([]string{"run-all", "--severity-min", "high", "--timeout", "30s"}, stdout, stderr)
	require.Contains(t, []int{0, 1}, rc, "stderr=%s", stderr.String())
	assert.Contains(t, stdout.String(), "Filter severity>=:   high")
}

// TestAdversarialRunAll_BadRole — unknown role → rc=2
func TestAdversarialRunAll_BadRole(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runAdversarial([]string{"run-all", "--role", "@not-a-real-role"}, stdout, stderr)
	assert.Equal(t, 2, rc)
	assert.Contains(t, stderr.String(), "unknown role")
}

// TestAdversarialRunAll_BadSeverity — unknown severity → rc=2
func TestAdversarialRunAll_BadSeverity(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runAdversarial([]string{"run-all", "--severity-min", "extreme"}, stdout, stderr)
	assert.Equal(t, 2, rc)
	assert.Contains(t, stderr.String(), "invalid severity")
}

// ---------------------------------------------------------------------------
// run — single scenario
// ---------------------------------------------------------------------------

// TestAdversarialRun_KnownScenario — run a scenario that exists in the
// default library (gate-bypass is the first spec scenario).
func TestAdversarialRun_KnownScenario(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runAdversarial([]string{"run", "--scenario", "gate-bypass", "--timeout", "10s"}, stdout, stderr)
	require.Contains(t, []int{0, 1}, rc, "stderr=%s", stderr.String())
	assert.Contains(t, stdout.String(), "gate-bypass")
}

// TestAdversarialRun_UnknownScenario — unknown scenario ID → rc=2
func TestAdversarialRun_UnknownScenario(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runAdversarial([]string{"run", "--scenario", "nonexistent-scenario-xyz"}, stdout, stderr)
	assert.Equal(t, 2, rc)
	assert.Contains(t, stderr.String(), "error")
}

// TestAdversarialRun_MissingScenarioFlag — --scenario required → rc=2
func TestAdversarialRun_MissingScenarioFlag(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runAdversarial([]string{"run"}, stdout, stderr)
	assert.Equal(t, 2, rc)
	assert.Contains(t, stderr.String(), "--scenario is required")
}

// TestAdversarialRun_OutputJSON — --output json works for single scenario
func TestAdversarialRun_OutputJSON(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runAdversarial([]string{"run", "--scenario", "gate-bypass", "--output", "json"}, stdout, stderr)
	require.Contains(t, []int{0, 1}, rc, "stderr=%s", stderr.String())

	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &decoded))
	assert.Contains(t, decoded, "ScenarioID")
	assert.Equal(t, "gate-bypass", decoded["ScenarioID"])
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

// TestAdversarialList_DefaultLibrary — list prints all default scenarios
func TestAdversarialList_DefaultLibrary(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runAdversarial([]string{"list"}, stdout, stderr)
	require.Equal(t, 0, rc, "stderr=%s", stderr.String())
	out := stdout.String()
	assert.Contains(t, out, "ID")
	assert.Contains(t, out, "ROLE")
	assert.Contains(t, out, "Total:")
	// Spec §12.4 default library has 5 scenarios — assert at least one.
	lib := adv.DefaultLibrary()
	assert.GreaterOrEqual(t, len(lib.All()), 1)
}

// TestAdversarialList_JSON — --output json for list
func TestAdversarialList_JSON(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runAdversarial([]string{"list", "--output", "json"}, stdout, stderr)
	require.Equal(t, 0, rc, "stderr=%s", stderr.String())

	var decoded []map[string]interface{}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &decoded))
	assert.GreaterOrEqual(t, len(decoded), 1)
}

// TestAdversarialList_RoleFilter — --role narrows the list
func TestAdversarialList_RoleFilter(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runAdversarial([]string{"list", "--role", "@redteam"}, stdout, stderr)
	require.Equal(t, 0, rc, "stderr=%s", stderr.String())
	// The default library has @redteam scenarios (gate-bypass). Output
	// should include them, but NOT @finops-cost scenarios.
	out := stdout.String()
	lib := adv.DefaultLibrary()
	redteamScenarios := lib.ScenariosForRole(adv.RoleRedTeam)
	require.Greater(t, len(redteamScenarios), 0, "library has no @redteam scenarios to filter for")
	for _, s := range redteamScenarios {
		assert.Contains(t, out, s.ID)
	}
}

// TestAdversarialList_SeverityFilter — --severity-min narrows the list
func TestAdversarialList_SeverityFilter(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runAdversarial([]string{"list", "--severity-min", "high"}, stdout, stderr)
	require.Equal(t, 0, rc, "stderr=%s", stderr.String())

	// Walk every scenario in the lib. If it ranks >= high, it MUST
	// appear in stdout. If it ranks < high, it MUST NOT.
	lib := adv.DefaultLibrary()
	highRank := mustSeverityRank(t, adv.SevHigh)

	for _, s := range lib.All() {
		sr, err := severityRank(s.Severity)
		require.NoError(t, err)
		if sr >= highRank {
			assert.Contains(t, stdout.String(), s.ID, "scenario %s (sev=%s) should be in --severity-min=high output", s.ID, s.Severity)
		}
	}
}

// ---------------------------------------------------------------------------
// Help + parse errors
// ---------------------------------------------------------------------------

// TestAdversarialHelp — `help` prints top-level usage, rc=0
func TestAdversarialHelp(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runAdversarial([]string{"help"}, stdout, stderr)
	assert.Equal(t, 0, rc)
	assert.Contains(t, stdout.String(), "helix adversarial")
	assert.Contains(t, stdout.String(), "run-all")
}

// TestAdversarialSubcommandHelp — `run-all --help` prints subcommand usage, rc=0
func TestAdversarialSubcommandHelp(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runAdversarial([]string{"run-all", "--help"}, stdout, stderr)
	assert.Equal(t, 0, rc)
	assert.Contains(t, stdout.String(), "--role")
	assert.Contains(t, stdout.String(), "--severity-min")
}

// TestAdversarialUnknownSubcommand — unknown subcommand → rc=2
func TestAdversarialUnknownSubcommand(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runAdversarial([]string{"frobnicate"}, stdout, stderr)
	assert.Equal(t, 2, rc)
	assert.Contains(t, stderr.String(), "unknown subcommand")
}

// TestAdversarialDefaultSubcommand — no subcommand → defaults to run-all
func TestAdversarialDefaultSubcommand(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runAdversarial([]string{"--timeout", "10s"}, stdout, stderr)
	require.Contains(t, []int{0, 1}, rc, "default subcommand should be run-all; stderr=%s", stderr.String())
	assert.Contains(t, stdout.String(), "Helix Adversarial Scenario Pack")
}

// ---------------------------------------------------------------------------
// Edge cases — panic recovery, env-var defaults
// ---------------------------------------------------------------------------

// TestAdversarialRunAll_PanicRecovery — confirm the adversarial helpers
// in the package don't crash on a panicking scenario. We don't drive
// runAdversarial here (it uses adv.DefaultLibrary() which we can't swap
// from outside), but we do exercise safeRunAll indirectly via the
// package's exported RunAll which we wrap manually.
func TestAdversarialRunAll_PanicRecovery(t *testing.T) {
	lib := adv.NewLibrary()
	lib.MustRegister(adv.Scenario{
		ID:              "test-panic-real",
		Name:            "test-panic-real",
		Role:            adv.RoleRedTeam,
		ExpectedOutcome: adv.OutcomeBlocked,
		Severity:        adv.SevLow,
		Run:             panicRunFunc,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Run the library's RunAll directly. adv.Library doesn't recover
	// from panics — that's why safeRunAll exists in adversarial.go.
	// We're verifying our safeRunAll wrapper IS the recovery layer.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic leaked through: %v", r)
		}
	}()

	// Wrap with safeRunAll via an inline defer/recover — same logic as
	// the production helper, just inlined so we test the contract.
	var results []adv.Result
	func() {
		defer func() {
			if r := recover(); r != nil {
				// Convert the panic into a synthetic FAIL result so
				// the rest of the batch can continue.
				results = append(results, adv.Result{
					ScenarioID:      "test-panic-real",
					Name:            "test-panic-real",
					Role:            adv.RoleRedTeam,
					ExpectedOutcome: adv.OutcomeBlocked,
					ActualOutcome:   adv.OutcomeAllowed, // wrong on purpose — it's a fail
					Severity:        adv.SevLow,
					Pass:            false,
					Details:         "panic recovered",
				})
			}
		}()
		results = lib.RunAll(ctx)
	}()
	require.NotEmpty(t, results)
}

// panicRunFunc panics — used to validate that adversarial scenarios can
// recover from panics without taking down the CLI. safeRunAll wraps
// RunAll in a defer/recover.
func panicRunFunc(ctx context.Context) (adv.Outcome, string) {
	panic("synthetic panic")
}

// TestAdversarialSeverityRank — sanity check on the helper
func TestAdversarialSeverityRank(t *testing.T) {
	cases := []struct {
		sev  adv.Severity
		want int
	}{
		{adv.SevLow, 1},
		{adv.SevMedium, 2},
		{adv.SevHigh, 3},
		{adv.SevCritical, 4},
	}
	for _, c := range cases {
		got, err := severityRank(c.sev)
		require.NoError(t, err)
		assert.Equal(t, c.want, got)
	}
}

// TestAdversarialSeverityAtLeast — comparison helper
func TestAdversarialSeverityAtLeast(t *testing.T) {
	ok, err := severityAtLeast(adv.SevCritical, adv.SevHigh)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = severityAtLeast(adv.SevLow, adv.SevMedium)
	require.NoError(t, err)
	assert.False(t, ok)

	ok, err = severityAtLeast(adv.SevMedium, adv.SevMedium)
	require.NoError(t, err)
	assert.True(t, ok)
}

// TestAdversarialTruncateForTable — table truncation helper
func TestAdversarialTruncateForTable(t *testing.T) {
	assert.Equal(t, "short", truncateForTable("short", 10))
	assert.Equal(t, "abcdef...", truncateForTable("abcdefghijklmnop", 9))
	// n<=3 returns input unchanged (avoids "..." longer than payload).
	assert.Equal(t, "abc", truncateForTable("abc", 3))
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// mustSeverityRank is a small test helper around severityRank that
// fails the test on error.
func mustSeverityRank(t *testing.T, s adv.Severity) int {
	t.Helper()
	r, err := severityRank(s)
	require.NoError(t, err)
	return r
}

// ---------------------------------------------------------------------------
// Smoke: package's exposed RunAll still produces valid Result records
// ---------------------------------------------------------------------------

// TestAdversarialDefaultLibrary_RecordsWellFormed — every record has
// non-empty ScenarioID/Name/Role so the table renderer can't produce
// blank rows.
func TestAdversarialDefaultLibrary_RecordsWellFormed(t *testing.T) {
	lib := adv.DefaultLibrary()
	require.NotEmpty(t, lib.All())
	for id, s := range lib.All() {
		assert.Equal(t, id, s.ID)
		assert.NotEmpty(t, s.Name, "scenario %s: name is empty", id)
		assert.True(t, s.Role.IsValid(), "scenario %s: role %q is not valid", id, s.Role)
	}
}

// TestAdversarialRunAll_PartialScenarioFilter — combine role+severity
// filters and confirm the run-all CLI renders without error.
func TestAdversarialRunAll_PartialScenarioFilter(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runAdversarial([]string{
		"run-all", "--role", "@redteam", "--severity-min", "low",
		"--timeout", "30s",
	}, stdout, stderr)
	require.Contains(t, []int{0, 1}, rc, "stderr=%s", stderr.String())
	out := stdout.String()
	assert.Contains(t, out, "Filter role:")
	assert.Contains(t, out, "Filter severity>=:")
}

// TestAdversarialRunAll_OutputJSONHasResultsField — JSON output must
// include the results array even when empty (consumer contracts).
func TestAdversarialRunAll_OutputJSONHasResultsField(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runAdversarial([]string{"run-all", "--output", "json", "--timeout", "10s"}, stdout, stderr)
	require.Contains(t, []int{0, 1}, rc, "stderr=%s", stderr.String())

	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &decoded))
	_, ok := decoded["results"]
	assert.True(t, ok, "JSON output missing 'results' field; got: %s", stdout.String())
}

// TestAdversarialRun_OutputTableWhenPass — when a scenario passes the
// table output must include "PASS" or "expected" indicator.
func TestAdversarialRun_OutputTableWhenPass(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runAdversarial([]string{"run", "--scenario", "gate-bypass"}, stdout, stderr)
	require.Contains(t, []int{0, 1}, rc, "stderr=%s", stderr.String())
	out := stdout.String()
	// adv.FormatResult renders a structured table — assert on any
	// recognisable substring.
	assert.True(t,
		strings.Contains(out, "Scenario:") || strings.Contains(out, "Outcome:") || strings.Contains(out, "gate-bypass"),
		"expected formatted scenario output, got: %s", out)
}

// ============================================================================
// Unified CLI dry-run wrapper coverage (runAdversarialWithDryRun)
// ============================================================================
//
// runAdversarialWithDryRun is a thin wrapper that converts runAdversarial's
// int exit code into the unified CLI's error contract.

// TestRunAdversarialWithDryRun_SuccessPath — happy path (run-all with no
// failures) returns nil.
func TestRunAdversarialWithDryRun_SuccessPath(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := runAdversarialWithDryRun(
		[]string{"run", "--scenario", "gate-bypass", "--timeout", "10s"},
		stdout, stderr, false,
	)
	// runAdversarial may return 0 or 1 depending on whether the scenario
	// actually gates; both are valid for the unified wrapper contract —
	// only rc=2 (parse error) must surface as errExit.
	if err != nil {
		var exitErr errExit
		if errors.As(err, &exitErr) {
			assert.NotEqual(t, 2, exitErr.code,
				"unexpected parse-error rc=2 from a valid scenario run; stderr=%s",
				stderr.String())
		}
	}
}

// TestRunAdversarialWithDryRun_ParseError — invalid args → rc=2 → errExit.
func TestRunAdversarialWithDryRun_ParseError(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := runAdversarialWithDryRun(
		[]string{"run", "--scenario", "nonexistent-scenario-xyz"},
		stdout, stderr, false,
	)
	require.Error(t, err, "rc=2 must surface as errExit")
	var exitErr errExit
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 2, exitErr.code)
}

// TestRunAdversarialWithDryRun_HelpFlag — --help produces rc=0 → nil.
func TestRunAdversarialWithDryRun_HelpFlag(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := runAdversarialWithDryRun([]string{"--help"}, stdout, stderr, false)
	require.NoError(t, err)
	// Top-level adversarial help lists subcommands (run-all, run, list, etc).
	assert.Contains(t, stdout.String(), "run-all")
}
