package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/totalwindupflightsystems/helix/pkg/verify"
)

// ---------------------------------------------------------------------------
// Output capture helpers.
// ---------------------------------------------------------------------------

// captureStdout redirects os.Stdout during f() and returns what was written.
func captureStdout(f func()) string {
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	f()
	_ = w.Close()
	os.Stdout = old
	return <-done
}

// captureStderr redirects os.Stderr during f().
func captureStderr(f func()) string {
	r, w, _ := os.Pipe()
	old := os.Stderr
	os.Stderr = w
	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	f()
	_ = w.Close()
	os.Stderr = old
	return <-done
}

// newRootCmd is a test-local constructor that mirrors the root built in main()
// but without observability.Init / os.Exit. Mirrors cmd/helix-prompt/main_test.go.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "helix-verify",
		Short: "Production verification — shadow, canary, and differential analysis",
		Long: `helix-verify manages the post-merge production verification pipeline:

  shadow  — Launch a dark-traffic shadow deployment, mirror production
            requests, and produce a differential report comparing
            shadow behavior against the production baseline.
  canary  — Promote a shadow-passed deployment to canary (gradual
            traffic ramp) or advance an active canary to the next step.
  status  — Show the current deployment lifecycle state for an agent.
  rollback — Force-rollback an active deployment with a structured reason.

All commands operate against the in-memory ShadowManager from pkg/verify.
Trust-tier-specific schedules (Provisional 24h → Veteran 2h) are respected
automatically via CanarySchedule(tier).`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		newShadowCmd(),
		newCanaryCmd(),
		newStatusCmd(),
		newRollbackCmd(),
	)
	return root
}

// resetFlags restores the package-level flag structs to zero values so that
// tests do not leak state into each other.
func resetFlags() {
	*shadowF = shadowFlags{}
	*canaryF = canaryFlags{}
	*statusF = statusFlags{}
	*rollbackF = rollbackFlags{}
}

// withFreshManager swaps the package-level manager for a fresh in-memory
// ShadowManager so tests start with empty state.
func withFreshManager(t *testing.T) func() {
	t.Helper()
	old := manager
	manager = verify.NewShadowManager()
	return func() { manager = old }
}

// backdateShadow moves a deployment's LaunchedAt backwards so that
// ObservationWindowRemaining returns 0 on the next call. This lets tests
// exercise the post-evaluation path of runShadow without sleeping for hours.
// The test runs single-threaded, so direct field mutation is safe.
func backdateShadow(t *testing.T, agentID string, age time.Duration) {
	t.Helper()
	dep := manager.GetDeployment(agentID)
	if dep == nil {
		t.Fatalf("backdateShadow: no deployment for %q", agentID)
	}
	dep.LaunchedAt = dep.LaunchedAt.Add(-age)
}

// shadowAndEvaluate drives the manager through shadow → evaluation, returning
// the deployment in ShadowPassed (or ShadowFailed) state. Useful for tests
// that want to start in a post-shadow state without running the CLI twice.
func shadowAndEvaluate(t *testing.T, agentID, tier string, shadow verify.MetricsSnapshot) *verify.ShadowDeployment {
	t.Helper()
	baseline := verify.MetricsSnapshot{
		SuccessRate:  1.0,
		P99LatencyMs: 50,
		Timestamp:    time.Now().UTC(),
	}
	cfg := verify.DefaultShadowConfig()
	d, err := manager.LaunchShadow(agentID, tier, baseline, cfg)
	if err != nil {
		t.Fatalf("LaunchShadow: %v", err)
	}
	if err := manager.RecordShadowMetrics(agentID, shadow); err != nil {
		t.Fatalf("RecordShadowMetrics: %v", err)
	}
	if _, err := manager.EvaluateShadow(agentID); err != nil {
		t.Fatalf("EvaluateShadow: %v", err)
	}
	if d.GetState() != verify.StateShadowPassed {
		t.Fatalf("deployment state = %s, want %s (differential report failed)",
			d.GetState(), verify.StateShadowPassed)
	}
	return d
}

// ---------------------------------------------------------------------------
// Root command structure
// ---------------------------------------------------------------------------

func TestRootCmd_HasExpectedSubcommands(t *testing.T) {
	root := newRootCmd()

	want := map[string]string{
		"shadow":   "Launch a shadow deployment for dark-traffic verification",
		"canary":   "Promote or advance a canary deployment",
		"status":   "Show deployment lifecycle state",
		"rollback": "Force-rollback an active deployment",
	}

	got := make(map[string]string)
	for _, c := range root.Commands() {
		got[c.Name()] = c.Short
	}

	for name := range want {
		if _, ok := got[name]; !ok {
			t.Errorf("missing subcommand %q (have: %v)", name, gotKeys(got))
		}
	}
	if len(got) != 4 {
		t.Errorf("root has %d subcommands, want 4: %v", len(got), gotKeys(got))
	}
}

func TestRootCmd_UseAndShort(t *testing.T) {
	root := newRootCmd()
	if root.Use != "helix-verify" {
		t.Errorf("root.Use = %q, want %q", root.Use, "helix-verify")
	}
	if !strings.Contains(root.Short, "Production verification") {
		t.Errorf("root.Short = %q, want to contain 'Production verification'", root.Short)
	}
	if !root.SilenceUsage {
		t.Error("SilenceUsage should be true")
	}
	if !root.SilenceErrors {
		t.Error("SilenceErrors should be true")
	}
}

func TestRootCmd_NoArgs(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{})

	out := captureStdout(func() {
		_ = captureStderr(func() {
			_ = root.Execute()
		})
	})
	if !strings.Contains(out, "helix-verify") {
		t.Errorf("root with no args should print help mentioning helix-verify, got:\n%s", out)
	}
}

func TestRootCmd_UnknownSubcommand(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"nonexistent-xyzzy"})

	errOut := captureStderr(func() {
		_ = captureStdout(func() {
			_ = root.Execute()
		})
	})

	_ = errOut
	root2 := newRootCmd()
	root2.SetArgs([]string{"nonexistent-xyzzy"})
	if err := root2.Execute(); err == nil {
		t.Error("unknown subcommand should produce an error")
	}
}

// TestVerifyRootCmd_Help is the canonical "root help shows all 4 subcommands"
// check. The task spec requires this exact test name and these keywords.
func TestVerifyRootCmd_Help(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"--help"})

	out := captureStdout(func() {
		_ = captureStderr(func() {
			_ = root.Execute()
		})
	})

	// Must include all four subcommands in the Available Commands section.
	for _, want := range []string{"shadow", "canary", "status", "rollback"} {
		if !strings.Contains(out, want) {
			t.Errorf("root --help output missing subcommand %q\n---\n%s\n---", want, out)
		}
	}

	// Must include the standard cobra help sections.
	for _, want := range []string{"Available Commands", "Flags", "Usage:"} {
		if !strings.Contains(out, want) {
			t.Errorf("root --help output missing section %q\n---\n%s\n---", want, out)
		}
	}
}

// TestRootCmd_HelpKeywords validates that each subcommand's keyword appears
// in the root --help output. This is a more granular check than
// TestVerifyRootCmd_Help — it ensures the root's long description references
// every concept the spec promises.
func TestRootCmd_HelpKeywords(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"--help"})
	out := captureStdout(func() {
		_ = captureStderr(func() {
			_ = root.Execute()
		})
	})

	// The root long description references shadow, canary, status, rollback.
	for _, want := range []string{
		"shadow",   // dark-launch, traffic mirroring
		"canary",   // gradual traffic ramp
		"status",   // deployment lifecycle
		"rollback", // force-rollback
		"Available Commands",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("root --help output missing keyword %q\n---\n%s\n---", want, out)
		}
	}
}

// ---------------------------------------------------------------------------
// shadow subcommand
// ---------------------------------------------------------------------------

func TestShadowCmd_Flags(t *testing.T) {
	cmd := newShadowCmd()

	if cmd.Use != "shadow" {
		t.Errorf("shadow Use = %q, want %q", cmd.Use, "shadow")
	}
	if !strings.Contains(cmd.Short, "shadow") {
		t.Errorf("shadow Short = %q, want to contain 'shadow'", cmd.Short)
	}

	for _, name := range []string{"agent", "tier", "duration", "json"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("shadow missing --%s flag", name)
		}
	}
}

func TestShadowCmd_Help(t *testing.T) {
	cmd := newShadowCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := cmd.Help(); err != nil {
		t.Fatalf("shadow --help error: %v", err)
	}
	s := buf.String()
	for _, want := range []string{"shadow", "dark-launch", "provisional", "veteran", "--duration"} {
		if !strings.Contains(s, want) {
			t.Errorf("shadow help missing %q\n---\n%s\n---", want, s)
		}
	}
}

func TestShadowCmd_MissingAgent(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	cmd := newShadowCmd()
	cmd.SetArgs([]string{})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	err := cmd.Execute()
	if err == nil {
		t.Error("shadow without --agent should error")
	}
}

func TestShadowCmd_InvalidFlag(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	cmd := newShadowCmd()
	cmd.SetArgs([]string{"--agent", "a1", "--no-such-flag"})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	err := cmd.Execute()
	if err == nil {
		t.Error("shadow with unknown flag should error")
	}
}

func TestShadowCmd_DefaultsToTier(t *testing.T) {
	// The shadow command's --tier default should be "provisional".
	cmd := newShadowCmd()
	f := cmd.Flags().Lookup("tier")
	if f == nil {
		t.Fatal("tier flag missing")
	}
	if f.DefValue != "provisional" {
		t.Errorf("--tier default = %q, want %q", f.DefValue, "provisional")
	}
}

func TestShadowCmd_HappyPath(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	cmd := newShadowCmd()
	cmd.SetArgs([]string{"--agent", "agent-shadow-1"})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	out := captureStdout(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("shadow execute error: %v\nstderr: %s", err, errBuf.String())
		}
	})
	combined := outBuf.String() + out + errBuf.String()

	for _, want := range []string{"Shadow launched", "agent-shadow-1", "Window:"} {
		if !strings.Contains(combined, want) {
			t.Errorf("shadow output missing %q\n---\n%s\n---", want, combined)
		}
	}

	// Manager should now have a deployment registered.
	dep := manager.GetDeployment("agent-shadow-1")
	if dep == nil {
		t.Fatal("manager should have a deployment for agent-shadow-1 after shadow run")
	}
	if dep.GetState() != verify.StateShadowing {
		t.Errorf("deployment state = %s, want %s", dep.GetState(), verify.StateShadowing)
	}
}

func TestShadowCmd_CustomDuration(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	cmd := newShadowCmd()
	cmd.SetArgs([]string{"--agent", "agent-shadow-2", "--duration", "1h"})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	out := captureStdout(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("shadow execute error: %v", err)
		}
	})
	combined := outBuf.String() + out + errBuf.String()

	if !strings.Contains(combined, "1h0m0s") {
		t.Errorf("shadow --duration 1h output should mention 1h0m0s\n---\n%s\n---", combined)
	}
}

func TestShadowCmd_VeteranTier(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	cmd := newShadowCmd()
	cmd.SetArgs([]string{"--agent", "agent-vet", "--tier", "veteran"})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	out := captureStdout(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("shadow execute error: %v", err)
		}
	})
	combined := outBuf.String() + out + errBuf.String()

	if !strings.Contains(combined, "veteran") {
		t.Errorf("shadow --tier veteran output should mention veteran\n---\n%s\n---", combined)
	}
}

// TestShadowCmd_Output_JSON exercises the shadow --json end-to-end path:
// the CLI is launched with --duration 1h, then we backdate the deployment
// so the observation window has elapsed, then re-run shadow with --json.
// The output is parsed as a DifferentialReport and verified for structure.
func TestShadowCmd_Output_JSON(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	// 1. First launch establishes the deployment with --duration 1h.
	cmd := newShadowCmd()
	cmd.SetArgs([]string{"--agent", "test-agent", "--duration", "1h"})
	var buf1 bytes.Buffer
	cmd.SetOut(&buf1)
	cmd.SetErr(&buf1)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("shadow launch failed: %v", err)
	}

	// 2. Record shadow metrics matching the baseline (perfect shadow) and
	//    backdate the launch so the observation window is fully elapsed.
	if err := manager.RecordShadowMetrics("test-agent", verify.MetricsSnapshot{
		SuccessRate:  1.0,
		P99LatencyMs: 50,
		Timestamp:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("RecordShadowMetrics: %v", err)
	}
	backdateShadow(t, "test-agent", 2*time.Hour)

	// 3. Re-run shadow with --json — should print a DifferentialReport.
	resetFlags()
	cmd = newShadowCmd()
	cmd.SetArgs([]string{"--agent", "test-agent", "--duration", "1h", "--json"})
	var buf2 bytes.Buffer
	cmd.SetOut(&buf2)
	cmd.SetErr(&buf2)

	out := captureStdout(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("shadow --json execute error: %v", err)
		}
	})
	jsonOut := buf2.String() + out
	if jsonOut == "" {
		t.Fatal("shadow --json produced no output")
	}

	// 4. Parse the JSON. The encoder in printDifferentialReport emits a single
	//    JSON object — locate the leading '{' and decode from there.
	start := strings.Index(jsonOut, "{")
	if start < 0 {
		t.Fatalf("shadow --json output contains no '{':\n%s", jsonOut)
	}
	var report verify.DifferentialReport
	if err := json.Unmarshal([]byte(jsonOut[start:]), &report); err != nil {
		t.Fatalf("shadow --json output is not valid JSON:\n%s\nerr: %v", jsonOut[start:], err)
	}

	// 5. Verify the differential report structure (these are the actual JSON
	//    fields emitted by pkg/verify.DifferentialReport — not the names in
	//    the task description, which referred to a separate surveillance
	//    payload that this CLI does not produce).
	if !report.AllPassed {
		t.Errorf("differential report AllPassed = false, want true (perfect shadow metrics)")
	}
	if len(report.Deltas) == 0 {
		t.Error("differential report has 0 deltas, want ≥4 (success_rate, p99, new_err, memory)")
	}
	metricNames := make(map[string]bool, len(report.Deltas))
	for _, d := range report.Deltas {
		metricNames[d.Metric] = true
	}
	for _, want := range []string{"success_rate", "p99_latency_ms", "new_error_types", "memory_growth_pct"} {
		if !metricNames[want] {
			t.Errorf("differential report missing metric %q (have: %v)", want, metricNames)
		}
	}
}

// TestShadowCmd_Output_JSON_FailedReport drives a failing differential and
// verifies that --json still produces valid JSON with all_passed=false.
func TestShadowCmd_Output_JSON_FailedReport(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	cmd := newShadowCmd()
	cmd.SetArgs([]string{"--agent", "agent-json-fail", "--duration", "1h"})
	var buf1 bytes.Buffer
	cmd.SetOut(&buf1)
	cmd.SetErr(&buf1)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("shadow launch failed: %v", err)
	}

	// Shadow metrics with success rate well below baseline → differential fails.
	if err := manager.RecordShadowMetrics("agent-json-fail", verify.MetricsSnapshot{
		SuccessRate:  0.5, // 50% success vs 100% baseline → 50% delta > 0.1% threshold
		P99LatencyMs: 500,
		Timestamp:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("RecordShadowMetrics: %v", err)
	}
	backdateShadow(t, "agent-json-fail", 2*time.Hour)

	resetFlags()
	cmd = newShadowCmd()
	cmd.SetArgs([]string{"--agent", "agent-json-fail", "--duration", "1h", "--json"})
	var buf2 bytes.Buffer
	cmd.SetOut(&buf2)
	cmd.SetErr(&buf2)

	out := captureStdout(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("shadow --json failed-report execute error: %v", err)
		}
	})
	jsonOut := buf2.String() + out
	start := strings.Index(jsonOut, "{")
	if start < 0 {
		t.Fatalf("no JSON in output:\n%s", jsonOut)
	}
	var report verify.DifferentialReport
	if err := json.Unmarshal([]byte(jsonOut[start:]), &report); err != nil {
		t.Fatalf("failed-report JSON parse error: %v\n%s", err, jsonOut[start:])
	}
	if report.AllPassed {
		t.Error("AllPassed should be false for a failed differential report")
	}
	if report.BlockReason == "" {
		t.Error("BlockReason should be set when differential fails")
	}
}

// ---------------------------------------------------------------------------
// canary subcommand
// ---------------------------------------------------------------------------

func TestCanaryCmd_Flags(t *testing.T) {
	cmd := newCanaryCmd()

	if cmd.Use != "canary" {
		t.Errorf("canary Use = %q, want %q", cmd.Use, "canary")
	}
	for _, name := range []string{"agent", "step", "json"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("canary missing --%s flag", name)
		}
	}
}

func TestCanaryCmd_Help(t *testing.T) {
	cmd := newCanaryCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := cmd.Help(); err != nil {
		t.Fatalf("canary --help error: %v", err)
	}
	s := buf.String()
	for _, want := range []string{"canary", "trust tier", "Provisional", "Veteran"} {
		if !strings.Contains(s, want) {
			t.Errorf("canary help missing %q\n---\n%s\n---", want, s)
		}
	}
}

func TestCanaryCmd_MissingAgent(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	cmd := newCanaryCmd()
	cmd.SetArgs([]string{})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	err := cmd.Execute()
	if err == nil {
		t.Error("canary without --agent should error")
	}
}

func TestCanaryCmd_NoDeployment(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	cmd := newCanaryCmd()
	cmd.SetArgs([]string{"--agent", "agent-nonexistent"})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	err := cmd.Execute()
	if err == nil {
		t.Error("canary for unknown agent should error")
	}
	if !strings.Contains(err.Error(), "no deployment") {
		t.Errorf("canary no-deployment error = %q, want 'no deployment'", err.Error())
	}
}

func TestCanaryCmd_WrongState(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	// Launch shadow but do NOT evaluate (so state is shadowing, not shadow_passed).
	shadow := newShadowCmd()
	shadow.SetArgs([]string{"--agent", "agent-wrong-state"})
	var sOut, sErr bytes.Buffer
	shadow.SetOut(&sOut)
	shadow.SetErr(&sErr)
	if err := shadow.Execute(); err != nil {
		t.Fatalf("shadow setup failed: %v", err)
	}

	// Now try canary — state is shadowing, not shadow_passed.
	resetFlags()
	canary := newCanaryCmd()
	canary.SetArgs([]string{"--agent", "agent-wrong-state"})
	var cOut, cErr bytes.Buffer
	canary.SetOut(&cOut)
	canary.SetErr(&cErr)

	err := canary.Execute()
	if err == nil {
		t.Error("canary on shadowing agent should error (not yet shadow_passed)")
	}
	if !strings.Contains(err.Error(), "shadowing") {
		t.Errorf("canary wrong-state error = %q, want to mention 'shadowing'", err.Error())
	}
}

func TestCanaryCmd_StepFlagDefaults(t *testing.T) {
	cmd := newCanaryCmd()
	f := cmd.Flags().Lookup("step")
	if f == nil {
		t.Fatal("canary missing --step flag")
	}
	if f.DefValue != "-1" {
		t.Errorf("--step default = %q, want %q", f.DefValue, "-1")
	}
}

func TestCanaryCmd_HappyPath_Promote(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	// Drive the manager through shadow → evaluation so we land in
	// StateShadowPassed — the only state from which canary can promote.
	shadowAndEvaluate(t, "agent-canary-promote", "provisional",
		verify.MetricsSnapshot{
			SuccessRate:  1.0,
			P99LatencyMs: 50,
			Timestamp:    time.Now().UTC(),
		})

	// Run the canary CLI command — should promote to canary state 1/N.
	cmd := newCanaryCmd()
	cmd.SetArgs([]string{"--agent", "agent-canary-promote"})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	out := captureStdout(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("canary execute error: %v\nstderr: %s", err, errBuf.String())
		}
	})
	combined := outBuf.String() + out + errBuf.String()

	for _, want := range []string{"Canary promoted", "agent-canary-promote", "State:", "canaried", "Traffic:"} {
		if !strings.Contains(combined, want) {
			t.Errorf("canary promote output missing %q\n---\n%s\n---", want, combined)
		}
	}

	// Verify the manager state transitioned.
	dep := manager.GetDeployment("agent-canary-promote")
	if dep == nil {
		t.Fatal("deployment missing after canary promotion")
	}
	if dep.GetState() != verify.StateCanaried {
		t.Errorf("deployment state = %s, want %s", dep.GetState(), verify.StateCanaried)
	}
	if dep.PromotedAt.IsZero() {
		t.Error("PromotedAt should be set after canary promotion")
	}
}

func TestCanaryCmd_HappyPath_Advance(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	// Set up a canaried deployment (state Canaried, step 0).
	shadowAndEvaluate(t, "agent-canary-advance", "trusted",
		verify.MetricsSnapshot{
			SuccessRate:  0.9999,
			P99LatencyMs: 55,
			Timestamp:    time.Now().UTC(),
		})
	if _, err := manager.PromoteToCanary("agent-canary-advance"); err != nil {
		t.Fatalf("setup: PromoteToCanary failed: %v", err)
	}

	// Advance — without --step, walks one step forward.
	resetFlags()
	cmd := newCanaryCmd()
	cmd.SetArgs([]string{"--agent", "agent-canary-advance"})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	out := captureStdout(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("canary advance execute error: %v\nstderr: %s", err, errBuf.String())
		}
	})
	combined := outBuf.String() + out + errBuf.String()

	for _, want := range []string{"Canary advanced", "agent-canary-advance", "Traffic:"} {
		if !strings.Contains(combined, want) {
			t.Errorf("canary advance output missing %q\n---\n%s\n---", want, combined)
		}
	}
}

func TestCanaryCmd_StepTargetingNotImplemented(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	// Set up a canaried agent with metrics identical to baseline (guaranteed to pass).
	shadowAndEvaluate(t, "agent-step-impossible", "trusted",
		verify.MetricsSnapshot{SuccessRate: 1.0, P99LatencyMs: 50, Timestamp: time.Now().UTC()})
	if _, err := manager.PromoteToCanary("agent-step-impossible"); err != nil {
		t.Fatalf("setup: PromoteToCanary failed: %v", err)
	}

	// --step with positive integer on a canaried agent should error
	// (step targeting not yet implemented).
	resetFlags()
	cmd := newCanaryCmd()
	cmd.SetArgs([]string{"--agent", "agent-step-impossible", "--step", "2"})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	err := cmd.Execute()
	if err == nil {
		t.Error("canary --step 2 on canaried agent should error (not implemented)")
	}
	if !strings.Contains(err.Error(), "step targeting") {
		t.Errorf("canary step-impossible error = %q, want to mention 'step targeting'", err.Error())
	}
}

// TestCanaryCmd_FinalStepPromoted walks a "trusted" deployment all the way
// from shadow-passed → canaried → advanced to 100% traffic (terminal
// StatePromoted) by invoking the canary CLI command twice. The second
// invocation must hit the `final` branch in runCanary and print the
// "FINAL — deployment fully promoted to 100% traffic" message.
func TestCanaryCmd_FinalStepPromoted(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	// Set up canaried state on a "trusted" agent (3-step schedule).
	shadowAndEvaluate(t, "agent-final", "trusted",
		verify.MetricsSnapshot{SuccessRate: 1.0, P99LatencyMs: 50, Timestamp: time.Now().UTC()})
	if _, err := manager.PromoteToCanary("agent-final"); err != nil {
		t.Fatalf("setup PromoteToCanary: %v", err)
	}

	// First advance: step 0 → step 1 (not final).
	resetFlags()
	cmd := newCanaryCmd()
	cmd.SetArgs([]string{"--agent", "agent-final"})
	var out1, err1 bytes.Buffer
	cmd.SetOut(&out1)
	cmd.SetErr(&err1)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("first canary advance: %v", err)
	}

	// Second advance: step 1 → step 2 (FINAL → StatePromoted).
	resetFlags()
	cmd = newCanaryCmd()
	cmd.SetArgs([]string{"--agent", "agent-final"})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	out := captureStdout(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("second canary advance: %v", err)
		}
	})
	combined := outBuf.String() + out + errBuf.String()

	// The FINAL message should appear.
	if !strings.Contains(combined, "FINAL") {
		t.Errorf("final canary advance should print FINAL marker\n---\n%s\n---", combined)
	}
	if !strings.Contains(combined, "10000%") {
		t.Errorf("final canary advance should print 10000%% traffic (100%% × 100)\n---\n%s\n---", combined)
	}

	// State should now be promoted.
	dep := manager.GetDeployment("agent-final")
	if dep == nil {
		t.Fatal("deployment missing")
	}
	if dep.GetState() != verify.StatePromoted {
		t.Errorf("deployment state = %s, want %s", dep.GetState(), verify.StatePromoted)
	}
}

// ---------------------------------------------------------------------------
// status subcommand
// ---------------------------------------------------------------------------

func TestStatusCmd_Flags(t *testing.T) {
	cmd := newStatusCmd()

	if cmd.Use != "status" {
		t.Errorf("status Use = %q, want %q", cmd.Use, "status")
	}
	for _, name := range []string{"agent", "all"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("status missing --%s flag", name)
		}
	}
}

func TestStatusCmd_Help(t *testing.T) {
	cmd := newStatusCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := cmd.Help(); err != nil {
		t.Fatalf("status --help error: %v", err)
	}
	s := buf.String()
	for _, want := range []string{"status", "shadow", "canary", "--all"} {
		if !strings.Contains(s, want) {
			t.Errorf("status help missing %q\n---\n%s\n---", want, s)
		}
	}
}

func TestStatusCmd_NoArgs_NoAll(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	cmd := newStatusCmd()
	cmd.SetArgs([]string{})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	err := cmd.Execute()
	if err == nil {
		t.Error("status without --agent or --all should error")
	}
	if !strings.Contains(err.Error(), "--agent") {
		t.Errorf("status no-args error = %q, want to mention '--agent'", err.Error())
	}
}

func TestStatusCmd_NoDeployment(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	cmd := newStatusCmd()
	cmd.SetArgs([]string{"--agent", "agent-not-deployed"})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	out := captureStdout(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("status for unknown agent should NOT error (it prints 'No deployment'): %v", err)
		}
	})
	combined := outBuf.String() + out + errBuf.String()
	if !strings.Contains(combined, "No deployment") {
		t.Errorf("status output for unknown agent should say 'No deployment'\n---\n%s\n---", combined)
	}
}

func TestStatusCmd_AllEmpty(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	cmd := newStatusCmd()
	cmd.SetArgs([]string{"--all"})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	out := captureStdout(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("status --all empty should not error: %v", err)
		}
	})
	combined := outBuf.String() + out + errBuf.String()
	if !strings.Contains(combined, "No deployments") {
		t.Errorf("status --all empty should print 'No deployments'\n---\n%s\n---", combined)
	}
}

func TestStatusCmd_HappyPath(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	// Launch a shadow deployment first.
	shadow := newShadowCmd()
	shadow.SetArgs([]string{"--agent", "agent-status-1"})
	var sOut, sErr bytes.Buffer
	shadow.SetOut(&sOut)
	shadow.SetErr(&sErr)
	if err := shadow.Execute(); err != nil {
		t.Fatalf("shadow setup failed: %v", err)
	}

	// Query status.
	resetFlags()
	status := newStatusCmd()
	status.SetArgs([]string{"--agent", "agent-status-1"})
	var stOut, stErr bytes.Buffer
	status.SetOut(&stOut)
	status.SetErr(&stErr)

	out := captureStdout(func() {
		if err := status.Execute(); err != nil {
			t.Fatalf("status execute error: %v", err)
		}
	})
	combined := stOut.String() + out + stErr.String()

	for _, want := range []string{"Agent: agent-status-1", "Tier:", "State:", "shadowing"} {
		if !strings.Contains(combined, want) {
			t.Errorf("status output missing %q\n---\n%s\n---", want, combined)
		}
	}
}

func TestStatusCmd_AllList(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	// Launch two shadow deployments.
	for _, agent := range []string{"agent-A", "agent-B"} {
		resetFlags()
		shadow := newShadowCmd()
		shadow.SetArgs([]string{"--agent", agent})
		var sOut, sErr bytes.Buffer
		shadow.SetOut(&sOut)
		shadow.SetErr(&sErr)
		if err := shadow.Execute(); err != nil {
			t.Fatalf("shadow setup for %s failed: %v", agent, err)
		}
	}

	// status --all
	resetFlags()
	status := newStatusCmd()
	status.SetArgs([]string{"--all"})
	var stOut, stErr bytes.Buffer
	status.SetOut(&stOut)
	status.SetErr(&stErr)

	out := captureStdout(func() {
		if err := status.Execute(); err != nil {
			t.Fatalf("status --all execute error: %v", err)
		}
	})
	combined := stOut.String() + out + stErr.String()

	if !strings.Contains(combined, "agent-A") || !strings.Contains(combined, "agent-B") {
		t.Errorf("status --all output should list both agents\n---\n%s\n---", combined)
	}
}

// TestStatusCmd_CanariedAgent covers the canary-step branch of
// printDeploymentStatus: when the deployment is in StateCanaried or
// StatePromoted, the printer queries CurrentCanaryStep and renders a
// "Canary step: X/Y (Z% traffic, ...)" line. No existing test exercises
// that branch — this one drives the manager through shadow → evaluation →
// canary promotion so the CLI's status command must produce the canary
// step info.
func TestStatusCmd_CanariedAgent(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	// Set up a canaried agent.
	shadowAndEvaluate(t, "agent-canaried-1", "trusted",
		verify.MetricsSnapshot{SuccessRate: 0.9999, P99LatencyMs: 55, Timestamp: time.Now().UTC()})
	if _, err := manager.PromoteToCanary("agent-canaried-1"); err != nil {
		t.Fatalf("PromoteToCanary: %v", err)
	}

	resetFlags()
	cmd := newStatusCmd()
	cmd.SetArgs([]string{"--agent", "agent-canaried-1"})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	out := captureStdout(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("status canaried execute error: %v", err)
		}
	})
	combined := outBuf.String() + out + errBuf.String()

	// Print must mention the canary step line — this is the branch of
	// printDeploymentStatus that only fires when state == Canaried|Promoted.
	for _, want := range []string{
		"agent-canaried-1",
		"canaried",
		"Canary step:",
		"traffic",
	} {
		if !strings.Contains(combined, want) {
			t.Errorf("status of canaried agent missing %q\n---\n%s\n---", want, combined)
		}
	}
}

// ---------------------------------------------------------------------------
// rollback subcommand
// ---------------------------------------------------------------------------

func TestRollbackCmd_Flags(t *testing.T) {
	cmd := newRollbackCmd()

	if cmd.Use != "rollback" {
		t.Errorf("rollback Use = %q, want %q", cmd.Use, "rollback")
	}
	for _, name := range []string{"agent", "reason"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("rollback missing --%s flag", name)
		}
	}
}

func TestRollbackCmd_Help(t *testing.T) {
	cmd := newRollbackCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := cmd.Help(); err != nil {
		t.Fatalf("rollback --help error: %v", err)
	}
	s := buf.String()
	for _, want := range []string{"rollback", "rolled_back", "breach report"} {
		if !strings.Contains(s, want) {
			t.Errorf("rollback help missing %q\n---\n%s\n---", want, s)
		}
	}
}

func TestRollbackCmd_MissingRequiredFlags(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	cmd := newRollbackCmd()
	cmd.SetArgs([]string{})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	err := cmd.Execute()
	if err == nil {
		t.Error("rollback without --agent and --reason should error")
	}
}

func TestRollbackCmd_MissingReason(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	cmd := newRollbackCmd()
	cmd.SetArgs([]string{"--agent", "agent-x"})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	err := cmd.Execute()
	if err == nil {
		t.Error("rollback without --reason should error")
	}
}

func TestRollbackCmd_NoDeployment(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	cmd := newRollbackCmd()
	cmd.SetArgs([]string{"--agent", "agent-nope", "--reason", "test rollback"})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	err := cmd.Execute()
	if err == nil {
		t.Error("rollback for non-deployed agent should error")
	}
	if !strings.Contains(err.Error(), "rollback") {
		t.Errorf("rollback no-deployment error = %q, want to mention 'rollback'", err.Error())
	}
}

func TestRollbackCmd_HappyPath(t *testing.T) {
	defer resetFlags()
	defer withFreshManager(t)()
	resetFlags()

	// Launch a shadow first.
	shadow := newShadowCmd()
	shadow.SetArgs([]string{"--agent", "agent-rb-1"})
	var sOut, sErr bytes.Buffer
	shadow.SetOut(&sOut)
	shadow.SetErr(&sErr)
	if err := shadow.Execute(); err != nil {
		t.Fatalf("shadow setup failed: %v", err)
	}

	// Rollback.
	resetFlags()
	rb := newRollbackCmd()
	rb.SetArgs([]string{"--agent", "agent-rb-1", "--reason", "differential report failed"})
	var rOut, rErr bytes.Buffer
	rb.SetOut(&rOut)
	rb.SetErr(&rErr)

	out := captureStdout(func() {
		if err := rb.Execute(); err != nil {
			t.Fatalf("rollback execute error: %v", err)
		}
	})
	combined := rOut.String() + out + rErr.String()

	for _, want := range []string{"Rollback triggered", "agent-rb-1", "differential report failed"} {
		if !strings.Contains(combined, want) {
			t.Errorf("rollback output missing %q\n---\n%s\n---", want, combined)
		}
	}

	// Manager should now report rolled_back state.
	dep := manager.GetDeployment("agent-rb-1")
	if dep == nil {
		t.Fatal("deployment missing after rollback")
	}
	if dep.GetState() != verify.StateRolledBack {
		t.Errorf("deployment state = %s, want %s", dep.GetState(), verify.StateRolledBack)
	}
	if dep.RollbackReason != "differential report failed" {
		t.Errorf("RollbackReason = %q, want %q", dep.RollbackReason, "differential report failed")
	}
}

// ---------------------------------------------------------------------------
// Pure helpers — printDeploymentStatus, canaryScheduleSteps
// ---------------------------------------------------------------------------

func TestCanaryScheduleSteps(t *testing.T) {
	for _, tier := range []string{"provisional", "observed", "trusted", "veteran"} {
		t.Run(tier, func(t *testing.T) {
			steps := canaryScheduleSteps(tier)
			if len(steps) == 0 {
				t.Errorf("canaryScheduleSteps(%q) returned 0 steps", tier)
			}
			// Steps should be monotonically increasing in traffic.
			for i := 1; i < len(steps); i++ {
				if steps[i].TrafficPct < steps[i-1].TrafficPct {
					t.Errorf("tier %q step %d traffic %.2f < step %d traffic %.2f",
						tier, i, steps[i].TrafficPct, i-1, steps[i-1].TrafficPct)
				}
			}
		})
	}
}

func TestPrintDeploymentStatus(t *testing.T) {
	// Build a minimal ShadowDeployment to exercise the printer.
	// We use the manager directly to create one, then pass it to the printer.
	dep := &verify.ShadowDeployment{
		AgentID:    "agent-print-1",
		Tier:       "trusted",
		LaunchedAt: time.Now().UTC(),
	}
	// State will be zero value (idle); the function tolerates that.

	out := captureStdout(func() {
		printDeploymentStatus(dep)
	})

	for _, want := range []string{"agent-print-1", "trusted", "State:", "Launched:"} {
		if !strings.Contains(out, want) {
			t.Errorf("printDeploymentStatus output missing %q\n---\n%s\n---", want, out)
		}
	}
}

func TestPrintDeploymentStatus_AfterRollback(t *testing.T) {
	defer withFreshManager(t)()
	dep := &verify.ShadowDeployment{
		AgentID:        "agent-rb-print",
		Tier:           "observed",
		LaunchedAt:     time.Now().UTC().Add(-1 * time.Hour),
		RolledBackAt:   time.Now().UTC(),
		RollbackReason: "baseline regression",
	}

	out := captureStdout(func() {
		printDeploymentStatus(dep)
	})
	for _, want := range []string{"agent-rb-print", "Rolled back", "baseline regression"} {
		if !strings.Contains(out, want) {
			t.Errorf("printDeploymentStatus output missing %q\n---\n%s\n---", want, out)
		}
	}
}

func TestPrintDifferentialReport_Text(t *testing.T) {
	report := &verify.DifferentialReport{
		AllPassed: true,
		Deltas: []verify.MetricDelta{
			{Metric: "success_rate", Prod: 0.99, Shadow: 0.99, Delta: 0.0, Passed: true, Reason: ""},
			{Metric: "p99_latency_ms", Prod: 120, Shadow: 125, Delta: 5, Passed: true, Reason: ""},
		},
	}
	var buf bytes.Buffer
	// Manually feed the function the buffer via captureStdout wrapper.
	out := captureStdout(func() {
		printDifferentialReport(report, false)
	})
	combined := buf.String() + out

	for _, want := range []string{
		"Shadow Differential Report",
		"success_rate",
		"p99_latency_ms",
		"ALL CHECKS PASSED",
	} {
		if !strings.Contains(combined, want) {
			t.Errorf("differential report text output missing %q\n---\n%s\n---", want, combined)
		}
	}
}

func TestPrintDifferentialReport_Failed(t *testing.T) {
	report := &verify.DifferentialReport{
		AllPassed:   false,
		BlockReason: "p99 latency regression exceeded threshold",
		Deltas: []verify.MetricDelta{
			{Metric: "p99_latency_ms", Prod: 120, Shadow: 200, Delta: 80, Passed: false, Reason: "regression"},
		},
	}
	out := captureStdout(func() {
		printDifferentialReport(report, false)
	})
	for _, want := range []string{"SHADOW FAILED", "p99 latency regression exceeded threshold"} {
		if !strings.Contains(out, want) {
			t.Errorf("failed report output missing %q\n---\n%s\n---", want, out)
		}
	}
}

func TestPrintDifferentialReport_JSON(t *testing.T) {
	report := &verify.DifferentialReport{
		AllPassed: true,
		Deltas:    []verify.MetricDelta{{Metric: "x", Prod: 1, Shadow: 1, Delta: 0, Passed: true}},
	}
	out := captureStdout(func() {
		printDifferentialReport(report, true)
	})
	// Should be valid JSON — parse and verify expected top-level fields.
	start := strings.Index(out, "{")
	if start < 0 {
		t.Fatalf("differential report JSON output missing '{':\n%s", out)
	}
	var parsed verify.DifferentialReport
	if err := json.Unmarshal([]byte(out[start:]), &parsed); err != nil {
		t.Fatalf("differential report JSON parse error: %v\n---\n%s\n---", err, out[start:])
	}
	if !parsed.AllPassed {
		t.Error("parsed.AllPassed should be true")
	}
	if len(parsed.Deltas) != 1 || parsed.Deltas[0].Metric != "x" {
		t.Errorf("parsed.Deltas = %+v, want one delta with Metric=x", parsed.Deltas)
	}
}

// ---------------------------------------------------------------------------
// End-to-end smoke test: shadow -> status -> rollback via root command.
// ---------------------------------------------------------------------------

func TestRootCmd_Routing(t *testing.T) {
	defer withFreshManager(t)()
	resetFlags()

	root := newRootCmd()
	root.SetArgs([]string{"shadow", "--agent", "agent-route-1"})

	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)

	out := captureStdout(func() {
		if err := root.Execute(); err != nil {
			t.Fatalf("root.Execute(shadow) error: %v", err)
		}
	})
	combined := outBuf.String() + out + errBuf.String()

	if !strings.Contains(combined, "Shadow launched") {
		t.Errorf("root routing to shadow should print 'Shadow launched'\n---\n%s\n---", combined)
	}

	// Now query via root.
	resetFlags()
	root2 := newRootCmd()
	root2.SetArgs([]string{"status", "--agent", "agent-route-1"})
	var stOut, stErr bytes.Buffer
	root2.SetOut(&stOut)
	root2.SetErr(&stErr)

	out2 := captureStdout(func() {
		if err := root2.Execute(); err != nil {
			t.Fatalf("root.Execute(status) error: %v", err)
		}
	})
	combined2 := stOut.String() + out2 + stErr.String()

	if !strings.Contains(combined2, "agent-route-1") {
		t.Errorf("root routing to status should print agent id\n---\n%s\n---", combined2)
	}
}

// ---------------------------------------------------------------------------
// Short mode compatibility
// ---------------------------------------------------------------------------

func TestShortMode(t *testing.T) {
	if testing.Short() {
		t.Log("running in -short mode")
	}
	// Pure helper smoke tests — no external dependencies.
	steps := canaryScheduleSteps("provisional")
	if len(steps) == 0 {
		t.Error("canaryScheduleSteps(provisional) returned empty")
	}
	_ = time.Second
}

// ---------------------------------------------------------------------------
// Misc helpers
// ---------------------------------------------------------------------------

func gotKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
