package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/totalwindupflightsystems/helix/pkg/verify"
)

// ---------------------------------------------------------------------------
// Output capture helpers (mirrors cmd/helix-prompt/main_test.go).
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

// newRootCmd is a test-local constructor that mirrors the root command built in
// main() but without calling observability.Init or os.Exit. This is what
// cmd/helix-prompt/main_test.go does with newRootCmd.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "helix-release",
		Short: "Release signoff — dual human+agent gate for production deployment",
		Long: `helix-release enforces the dual-signoff release gate:

  signoff — Display the release dashboard: agent technical gates
            (shadow, canary, contracts, trust tier) and human
            approval status. Both signatures required.
  approve — Record human approval of the change intent.
  status  — Show signoff status for a release.

The agent verifies technical gates automatically. The human
approves the change intent (risk, timing, blast radius).
Both signatures are mandatory — neither alone is sufficient.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		newSignoffCmd(),
		newApproveCmd(),
		newStatusCmd(),
	)
	return root
}

// resetFlags restores the package-level flag structs to zero values so that
// tests do not leak state into each other.
func resetFlags() {
	*soFlags = signoffFlags{}
	*appFlags = approveFlags{}
	*stFlags = statusFlags{}
}

// withTempHome redirects os.UserHomeDir() output to a temp directory for the
// duration of the test by overriding the package-level signoffStore. This
// prevents tests from writing to the real ~/.helix/releases directory.
func withTempHome(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	// Override signoffStore so Load/Save point to a temp dir.
	oldStore := signoffStore
	signoffStore = &releaseStore{dir: dir}
	// Override verifyManager so GetDeployment returns nil for unknown agents.
	oldMgr := verifyManager
	verifyManager = verify.NewShadowManager()
	return dir, func() {
		signoffStore = oldStore
		verifyManager = oldMgr
	}
}

// ---------------------------------------------------------------------------
// Root command structure
// ---------------------------------------------------------------------------

func TestRootCmd_HasExpectedSubcommands(t *testing.T) {
	root := newRootCmd()

	want := map[string]string{
		"signoff": "Display the dual-signoff release dashboard",
		"approve": "Record human approval of the change intent",
		"status":  "Show signoff status for a release",
	}

	got := make(map[string]string)
	for _, c := range root.Commands() {
		got[c.Name()] = c.Short
	}

	for name, short := range want {
		s, ok := got[name]
		if !ok {
			t.Errorf("missing subcommand %q (have: %v)", name, keys(got))
			continue
		}
		if !strings.Contains(s, strings.SplitN(short, " ", 2)[0]) {
			t.Errorf("subcommand %q short=%q, want to contain %q", name, s, short)
		}
	}
	if len(got) != 3 {
		t.Errorf("root has %d subcommands, want 3: %v", len(got), keys(got))
	}
}

func TestRootCmd_UseAndShort(t *testing.T) {
	root := newRootCmd()
	if root.Use != "helix-release" {
		t.Errorf("root.Use = %q, want %q", root.Use, "helix-release")
	}
	if !strings.Contains(root.Short, "Release signoff") {
		t.Errorf("root.Short = %q, want to contain 'Release signoff'", root.Short)
	}
	if root.SilenceUsage != true {
		t.Error("SilenceUsage should be true (errors shouldn't dump usage)")
	}
	if root.SilenceErrors != true {
		t.Error("SilenceErrors should be true (errors printed by executeRoot)")
	}
}

func TestRootCmd_HelpFlag(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"--help"})

	out := captureStdout(func() {
		_ = captureStderr(func() {
			_ = root.Execute()
		})
	})
	for _, want := range []string{"signoff", "approve", "status", "Available Commands"} {
		if !strings.Contains(out, want) {
			t.Errorf("root --help output missing %q\n---\n%s\n---", want, out)
		}
	}
}

func TestRootCmd_NoArgs(t *testing.T) {
	// With no args cobra prints root help to stdout (no error).
	root := newRootCmd()
	root.SetArgs([]string{})

	out := captureStdout(func() {
		_ = captureStderr(func() {
			_ = root.Execute()
		})
	})
	if !strings.Contains(out, "helix-release") {
		t.Errorf("root with no args should print help mentioning helix-release, got:\n%s", out)
	}
}

func TestRootCmd_UnknownSubcommand(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"nonexistent-subcommand-xyzzy"})

	errOut := captureStderr(func() {
		_ = captureStdout(func() {
			_ = root.Execute()
		})
	})

	// Cobra prints "Error: unknown command ..." to stderr on the command's
	// OutOrStderr. The test expects an error string (not necessarily in our
	// captured stderr, since cobra may use its own writer).
	if errOut == "" {
		t.Logf("no stderr captured for unknown subcommand (cobra routes via cmd.ErrOrStderr)")
	}
	// Most importantly, Execute() returns an error.
	root2 := newRootCmd()
	root2.SetArgs([]string{"nonexistent-subcommand-xyzzy"})
	if err := root2.Execute(); err == nil {
		t.Error("unknown subcommand should produce an error")
	}
}

// ---------------------------------------------------------------------------
// signoff subcommand
// ---------------------------------------------------------------------------

func TestSignoffCmd_Flags(t *testing.T) {
	cmd := newSignoffCmd()

	if cmd.Use != "signoff" {
		t.Errorf("signoff Use = %q, want %q", cmd.Use, "signoff")
	}
	if !strings.Contains(cmd.Short, "dual-signoff") {
		t.Errorf("signoff Short = %q, want to contain 'dual-signoff'", cmd.Short)
	}

	// All expected flags registered.
	wantFlags := []string{"version", "agent", "merge-commit", "tier", "ledger", "json"}
	for _, name := range wantFlags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("signoff missing --%s flag", name)
		}
	}

	// Required flags.
	for _, req := range []string{"version", "agent"} {
		f := cmd.Flags().Lookup(req)
		if f == nil {
			continue
		}
		// cobra stores "required" annotation on the flag; verify via Annotations.
		annotations := f.Annotations
		if annotations == nil {
			t.Errorf("signoff --%s missing annotations", req)
			continue
		}
		if _, ok := annotations["cobra_annotation_required"]; !ok && f.Value.String() != "" {
			t.Logf("signoff --%s may not be marked required (annotations=%v)", req, annotations)
		}
	}
}

func TestSignoffCmd_Help(t *testing.T) {
	cmd := newSignoffCmd()
	cmd.SetArgs([]string{"--help"})
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&outBuf)

	if err := cmd.Help(); err != nil {
		t.Fatalf("signoff --help error: %v", err)
	}
	s := outBuf.String()
	for _, want := range []string{"signoff", "Shadow verification", "Behavior contracts", "Both signatures"} {
		if !strings.Contains(s, want) {
			t.Errorf("signoff help missing %q\n---\n%s\n---", want, s)
		}
	}
	// Flag section should be present.
	if !strings.Contains(s, "--version") || !strings.Contains(s, "--agent") {
		t.Errorf("signoff help missing flag section\n---\n%s\n---", s)
	}
}

func TestSignoffCmd_MissingRequiredFlags(t *testing.T) {
	// Test that --version and --agent are required.
	cmd := newSignoffCmd()
	cmd.SetArgs([]string{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil {
		t.Error("signoff with no flags should error (missing --version and --agent)")
	}
}

func TestSignoffCmd_MissingAgent(t *testing.T) {
	defer resetFlags()
	resetFlags()

	cmd := newSignoffCmd()
	cmd.SetArgs([]string{"--version", "v1.2.3"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil {
		t.Error("signoff --version v1.2.3 (missing --agent) should error")
	}
}

func TestSignoffCmd_MissingVersion(t *testing.T) {
	defer resetFlags()
	resetFlags()

	cmd := newSignoffCmd()
	cmd.SetArgs([]string{"--agent", "agent-007"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil {
		t.Error("signoff --agent agent-007 (missing --version) should error")
	}
}

func TestSignoffCmd_InvalidFlag(t *testing.T) {
	defer resetFlags()
	resetFlags()

	cmd := newSignoffCmd()
	cmd.SetArgs([]string{"--version", "v1", "--agent", "a", "--no-such-flag"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil {
		t.Error("signoff with unknown flag should error")
	}
}

func TestSignoffCmd_HappyPath_Text(t *testing.T) {
	defer resetFlags()
	_, cleanup := withTempHome(t)
	defer cleanup()
	resetFlags()

	cmd := newSignoffCmd()
	cmd.SetArgs([]string{
		"--version", "v1.2.3",
		"--agent", "agent-test-001",
		"--tier", "observed",
		"--ledger", "/nonexistent/ledger.jsonl",
	})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	// printSignoffDashboard uses fmt.Println (os.Stdout), so capture os.Stdout.
	out := captureStdout(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("signoff execute error: %v\nstderr: %s", err, errBuf.String())
		}
	})
	combined := outBuf.String() + out + errBuf.String()

	for _, want := range []string{"RELEASE SIGNOFF DASHBOARD", "agent-test-001", "v1.2.3"} {
		if !strings.Contains(combined, want) {
			t.Errorf("signoff text output missing %q\n---\n%s\n---", want, combined)
		}
	}
}

func TestSignoffCmd_HappyPath_JSON(t *testing.T) {
	defer resetFlags()
	_, cleanup := withTempHome(t)
	defer cleanup()
	resetFlags()

	cmd := newSignoffCmd()
	cmd.SetArgs([]string{
		"--version", "v9.9.9",
		"--agent", "agent-json-test",
		"--json",
	})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	// JSON output is written via json.NewEncoder(os.Stdout) directly,
	// so we also wrap os.Stdout capture to catch it.
	out := captureStdout(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("signoff --json execute error: %v\nstderr: %s", err, errBuf.String())
		}
	})
	// Combine cobra buffers and captured stdout.
	combined := outBuf.String() + out + errBuf.String()

	if !strings.Contains(combined, "\"version\"") || !strings.Contains(combined, "v9.9.9") {
		t.Errorf("signoff --json output missing version field\n---\n%s\n---", combined)
	}
	if !strings.Contains(combined, "\"agent_id\"") {
		t.Errorf("signoff --json output missing agent_id field\n---\n%s\n---", combined)
	}
}

func TestSignoffCmd_PersistsRecord(t *testing.T) {
	defer resetFlags()
	dir, cleanup := withTempHome(t)
	defer cleanup()
	resetFlags()

	cmd := newSignoffCmd()
	cmd.SetArgs([]string{
		"--version", "v5.5.5",
		"--agent", "agent-persist-test",
	})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("signoff execute error: %v", err)
	}

	// The store should have created <dir>/v5.5.5.json
	expected := filepath.Join(dir, "v5.5.5.json")
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("signoff should persist record to %s: %v", expected, err)
	}
}

// ---------------------------------------------------------------------------
// approve subcommand
// ---------------------------------------------------------------------------

func TestApproveCmd_Flags(t *testing.T) {
	cmd := newApproveCmd()

	if cmd.Use != "approve" {
		t.Errorf("approve Use = %q, want %q", cmd.Use, "approve")
	}
	if !strings.Contains(cmd.Short, "human approval") {
		t.Errorf("approve Short = %q, want to contain 'human approval'", cmd.Short)
	}

	for _, name := range []string{"version", "human-id", "ledger"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("approve missing --%s flag", name)
		}
	}
}

func TestApproveCmd_Help(t *testing.T) {
	cmd := newApproveCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := cmd.Help(); err != nil {
		t.Fatalf("approve --help error: %v", err)
	}
	s := buf.String()
	for _, want := range []string{"approve", "intent", "Risk acceptance", "CLARIFICATION_NEEDED"} {
		if !strings.Contains(s, want) {
			t.Errorf("approve help missing %q\n---\n%s\n---", want, s)
		}
	}
}

func TestApproveCmd_MissingRequiredFlags(t *testing.T) {
	defer resetFlags()
	resetFlags()

	cmd := newApproveCmd()
	cmd.SetArgs([]string{})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	if err := cmd.Execute(); err == nil {
		t.Error("approve with no flags should error")
	}
}

func TestApproveCmd_NoRecord(t *testing.T) {
	defer resetFlags()
	_, cleanup := withTempHome(t)
	defer cleanup()
	resetFlags()

	cmd := newApproveCmd()
	cmd.SetArgs([]string{"--version", "v0.0.0-unknown", "--human-id", "alice"})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	err := cmd.Execute()
	if err == nil {
		t.Error("approve for missing record should error")
	}
	if !strings.Contains(err.Error(), "no signoff record") {
		t.Errorf("approve missing-record error = %q, want 'no signoff record'", err.Error())
	}
}

func TestApproveCmd_HappyPath(t *testing.T) {
	defer resetFlags()
	_, cleanup := withTempHome(t)
	defer cleanup()
	resetFlags()

	// 1. Create a signoff record.
	signoff := newSignoffCmd()
	signoff.SetArgs([]string{"--version", "v2.0.0", "--agent", "agent-approve-test"})
	var sOut, sErr bytes.Buffer
	signoff.SetOut(&sOut)
	signoff.SetErr(&sErr)
	if err := signoff.Execute(); err != nil {
		t.Fatalf("pre-approve signoff failed: %v", err)
	}

	// 2. Approve it. runApprove uses fmt.Printf to stdout, so capture os.Stdout.
	resetFlags()
	approve := newApproveCmd()
	approve.SetArgs([]string{"--version", "v2.0.0", "--human-id", "bob"})
	var aOut, aErr bytes.Buffer
	approve.SetOut(&aOut)
	approve.SetErr(&aErr)

	out := captureStdout(func() {
		if err := approve.Execute(); err != nil {
			t.Fatalf("approve execute error: %v\nstderr: %s", err, aErr.String())
		}
	})
	combined := aOut.String() + out + aErr.String()

	for _, want := range []string{"Human approval recorded", "bob", "v2.0.0"} {
		if !strings.Contains(combined, want) {
			t.Errorf("approve output missing %q\n---\n%s\n---", want, combined)
		}
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

	for _, name := range []string{"version", "json"} {
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
	for _, want := range []string{"status", "agent technical signoff", "helix-release signoff"} {
		if !strings.Contains(s, want) {
			t.Errorf("status help missing %q\n---\n%s\n---", want, s)
		}
	}
}

func TestStatusCmd_MissingRequiredFlags(t *testing.T) {
	defer resetFlags()
	resetFlags()

	cmd := newStatusCmd()
	cmd.SetArgs([]string{})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	if err := cmd.Execute(); err == nil {
		t.Error("status with no flags should error (missing --version)")
	}
}

func TestStatusCmd_NoRecord(t *testing.T) {
	defer resetFlags()
	_, cleanup := withTempHome(t)
	defer cleanup()
	resetFlags()

	cmd := newStatusCmd()
	cmd.SetArgs([]string{"--version", "v99.99.99-not-found"})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	err := cmd.Execute()
	if err == nil {
		t.Error("status for missing record should error")
	}
	if !strings.Contains(err.Error(), "no signoff record") {
		t.Errorf("status missing-record error = %q, want 'no signoff record'", err.Error())
	}
}

func TestStatusCmd_HappyPath_Text(t *testing.T) {
	defer resetFlags()
	dir, cleanup := withTempHome(t)
	defer cleanup()
	resetFlags()

	// Create a record via signoff first.
	signoff := newSignoffCmd()
	signoff.SetArgs([]string{"--version", "v3.3.3", "--agent", "agent-status-test"})
	var sOut, sErr bytes.Buffer
	signoff.SetOut(&sOut)
	signoff.SetErr(&sErr)
	if err := signoff.Execute(); err != nil {
		t.Fatalf("pre-status signoff failed: %v", err)
	}

	// Now query status. runStatus uses fmt.Printf to stdout, so capture os.Stdout.
	resetFlags()
	status := newStatusCmd()
	status.SetArgs([]string{"--version", "v3.3.3"})
	var stOut, stErr bytes.Buffer
	status.SetOut(&stOut)
	status.SetErr(&stErr)

	out := captureStdout(func() {
		if err := status.Execute(); err != nil {
			t.Fatalf("status execute error: %v\nstderr: %s", err, stErr.String())
		}
	})
	combined := stOut.String() + out + stErr.String()

	for _, want := range []string{"Release:", "v3.3.3", "agent-status-test", "Human approval:"} {
		if !strings.Contains(combined, want) {
			t.Errorf("status output missing %q\n---\n%s\n---", want, combined)
		}
	}

	// Ensure record exists on disk.
	if _, err := os.Stat(filepath.Join(dir, "v3.3.3.json")); err != nil {
		t.Errorf("record should be persisted: %v", err)
	}
}

func TestStatusCmd_HappyPath_JSON(t *testing.T) {
	defer resetFlags()
	_, cleanup := withTempHome(t)
	defer cleanup()
	resetFlags()

	// Create record.
	signoff := newSignoffCmd()
	signoff.SetArgs([]string{"--version", "v4.4.4", "--agent", "agent-json-status"})
	var sOut, sErr bytes.Buffer
	signoff.SetOut(&sOut)
	signoff.SetErr(&sErr)
	if err := signoff.Execute(); err != nil {
		t.Fatalf("pre-status signoff failed: %v", err)
	}

	// Query as JSON.
	resetFlags()
	status := newStatusCmd()
	status.SetArgs([]string{"--version", "v4.4.4", "--json"})
	var stOut, stErr bytes.Buffer
	status.SetOut(&stOut)
	status.SetErr(&stErr)

	// runStatus uses json.NewEncoder(os.Stdout) directly, so capture os.Stdout.
	out := captureStdout(func() {
		if err := status.Execute(); err != nil {
			t.Fatalf("status --json execute error: %v", err)
		}
	})
	combined := stOut.String() + out + stErr.String()
	if !strings.Contains(combined, "\"version\"") || !strings.Contains(combined, "v4.4.4") {
		t.Errorf("status --json output missing version\n---\n%s\n---", combined)
	}
}

// ---------------------------------------------------------------------------
// Pure-function helpers (sanitizeVersion, tierRank, etc.)
// ---------------------------------------------------------------------------

func TestSanitizeVersion(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"v1.2.3", "v1.2.3"},
		{"v1.2.3-rc.1", "v1.2.3-rc.1"},
		{"v 1 2 3", "v-1-2-3"},
		{"../etc/passwd", "..-etc-passwd"},
		{"agent@host:1.0", "agent-host-1.0"},
		{"", ""},
		{"a_b_c", "a-b-c"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := sanitizeVersion(tt.in)
			if got != tt.want {
				t.Errorf("sanitizeVersion(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestTierRank(t *testing.T) {
	tests := []struct {
		tier string
		want int
	}{
		{"provisional", 1},
		{"observed", 2},
		{"trusted", 3},
		{"veteran", 4},
		{"VETERAN", 4}, // case insensitive
		{"unknown", 0},
		{"", 0},
	}
	for _, tt := range tests {
		t.Run(tt.tier, func(t *testing.T) {
			got := tierRank(tt.tier)
			if got != tt.want {
				t.Errorf("tierRank(%q) = %d, want %d", tt.tier, got, tt.want)
			}
		})
	}
}

func TestCanaryScheduleSummaryFor(t *testing.T) {
	durations := make(map[string]time.Duration)
	for _, tier := range []string{"provisional", "observed", "trusted", "veteran"} {
		t.Run(tier, func(t *testing.T) {
			s := canaryScheduleSummaryFor(tier)
			if s == nil {
				t.Fatalf("canaryScheduleSummaryFor(%q) returned nil", tier)
			}
			if s.Tier != tier {
				t.Errorf("summary.Tier = %q, want %q", s.Tier, tier)
			}
			if s.TotalSteps <= 0 {
				t.Errorf("summary.TotalSteps = %d, want > 0", s.TotalSteps)
			}
			if s.TotalDuration == "" {
				t.Error("summary.TotalDuration should not be empty")
			}
			parsed, err := time.ParseDuration(s.TotalDuration)
			if err != nil {
				t.Errorf("TotalDuration %q unparseable: %v", s.TotalDuration, err)
				return
			}
			durations[tier] = parsed
		})
	}
	// Veteran should have the fastest ramp (smallest duration),
	// provisional the slowest.
	if durations["veteran"] >= durations["provisional"] {
		t.Errorf("veteran duration (%s) should be shorter than provisional (%s)",
			durations["veteran"], durations["provisional"])
	}
	if durations["trusted"] >= durations["provisional"] {
		t.Errorf("trusted duration (%s) should be shorter than provisional (%s)",
			durations["trusted"], durations["provisional"])
	}
}

func TestPrintGate_Output(t *testing.T) {
	// Capture stdout around printGate to confirm it writes expected markers.
	cases := []struct {
		name string
		gate GateStatus
		want []string // substrings expected in output
	}{
		{
			name: "passed and blocking",
			gate: GateStatus{Name: "shadow_verification", Passed: true, Detail: "ok", Blocked: true},
			want: []string{"✅", "shadow_verification", "[BLOCKS RELEASE]"},
		},
		{
			name: "failed and blocking",
			gate: GateStatus{Name: "trust_tier", Passed: false, Detail: "low score", Blocked: true},
			want: []string{"❌", "trust_tier", "[BLOCKS RELEASE]"},
		},
		{
			name: "passed and not blocking",
			gate: GateStatus{Name: "info_only", Passed: true, Detail: "informational", Blocked: false},
			want: []string{"✅", "info_only"},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			out := captureStdout(func() {
				printGate(tt.gate)
			})
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("printGate output missing %q\n---\n%s\n---", w, out)
				}
			}
		})
	}
}

func TestPrintSignoffDashboard_Blocked(t *testing.T) {
	rec := &SignoffRecord{
		ReleaseID: "rel-test-1",
		Version:   "v0.1.0",
		AgentID:   "agent-dash",
		AgentSignoff: AgentSignoff{
			AllGatesPassed:  false,
			BlockingReasons: []string{"shadow: failed"},
		},
	}
	out := captureStdout(func() {
		printSignoffDashboard(rec, nil, "/nonexistent/ledger")
	})
	for _, want := range []string{
		"RELEASE SIGNOFF DASHBOARD",
		"agent-dash",
		"v0.1.0",
		"TECHNICAL GATES BLOCKED",
		"PENDING",
		"BLOCKED",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dashboard output missing %q\n---\n%s\n---", want, out)
		}
	}
}

func TestPrintSignoffDashboard_BothSigned(t *testing.T) {
	rec := &SignoffRecord{
		ReleaseID:   "rel-test-2",
		Version:     "v0.2.0",
		AgentID:     "agent-dash-2",
		MergeCommit: "abc1234",
		AgentSignoff: AgentSignoff{
			AllGatesPassed: true,
		},
		HumanSignoff: HumanSignoff{
			HumanID:  "alice",
			Approved: true,
		},
		CanarySchedule: canaryScheduleSummaryFor("trusted"),
	}
	out := captureStdout(func() {
		printSignoffDashboard(rec, nil, "")
	})
	for _, want := range []string{
		"RELEASE SIGNOFF DASHBOARD",
		"abc1234",
		"ALL TECHNICAL GATES PASSED",
		"APPROVED by alice",
		"READY FOR DEPLOYMENT",
		"trusted",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dashboard output missing %q\n---\n%s\n---", want, out)
		}
	}
}

func TestCollectGates_NilDeployment(t *testing.T) {
	var gates []GateStatus
	collectGates(&gates, "agent-x", "provisional", nil, "")
	if len(gates) == 0 {
		t.Fatal("collectGates with nil dep should still produce gates")
	}

	// At minimum, the shadow_verification, canary_readiness, trust_tier,
	// and behavior_contracts gates should be present.
	seen := make(map[string]bool)
	for _, g := range gates {
		seen[g.Name] = true
	}
	for _, want := range []string{"shadow_verification", "canary_readiness", "trust_tier", "behavior_contracts"} {
		if !seen[want] {
			t.Errorf("collectGates missing %q (got: %v)", want, boolKeys(seen))
		}
	}
}

func TestComputeAgentSignoff_NilDep(t *testing.T) {
	signoff := computeAgentSignoff("agent-x", "provisional", nil, "")
	// With nil deployment and provisional tier, agent signoff should fail
	// because not all gates are passed.
	if signoff.AllGatesPassed {
		t.Error("computeAgentSignoff with nil dep should not pass all gates")
	}
	if len(signoff.BlockingReasons) == 0 {
		t.Error("expected at least one blocking reason")
	}
}

// ---------------------------------------------------------------------------
// SignoffStore unit tests
// ---------------------------------------------------------------------------

func TestReleaseStore_LoadMissing(t *testing.T) {
	dir := t.TempDir()
	s := &releaseStore{dir: dir}

	rec, err := s.Load("v999")
	if err != nil {
		t.Errorf("Load of missing version should return nil,nil, got err: %v", err)
	}
	if rec != nil {
		t.Errorf("Load of missing version should return nil record, got %+v", rec)
	}
}

func TestReleaseStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	s := &releaseStore{dir: dir}

	rec := &SignoffRecord{
		ReleaseID: "rel-x",
		Version:   "v1.0.0",
		AgentID:   "agent-store",
		AgentSignoff: AgentSignoff{
			AllGatesPassed: true,
		},
	}
	if err := s.Save(rec); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := s.Load("v1.0.0")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil record after Save")
	}
	if loaded.AgentID != "agent-store" {
		t.Errorf("loaded.AgentID = %q, want %q", loaded.AgentID, "agent-store")
	}
	if !loaded.AgentSignoff.AllGatesPassed {
		t.Error("loaded.AgentSignoff.AllGatesPassed should be true")
	}
}

func TestReleaseStore_SaveUsesSanitizedFilename(t *testing.T) {
	dir := t.TempDir()
	s := &releaseStore{dir: dir}

	rec := &SignoffRecord{Version: "v1/2/3", AgentID: "a"}
	if err := s.Save(rec); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Slashes should be sanitized to dashes.
	expected := filepath.Join(dir, "v1-2-3.json")
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("expected sanitized file at %s: %v", expected, err)
	}
}

func TestReleaseStore_PathUsesSanitized(t *testing.T) {
	s := &releaseStore{dir: t.TempDir()}
	got := s.path("v1@bad/name")
	want := filepath.Join(s.dir, "v1-bad-name.json")
	if got != want {
		t.Errorf("path() = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Short mode compatibility — all tests should be safe under -short.
// ---------------------------------------------------------------------------

func TestShortMode(t *testing.T) {
	if testing.Short() {
		t.Log("running in -short mode (skipping any external-service tests)")
	}
	// Smoke test the package compiles and pure helpers don't panic.
	_ = sanitizeVersion("v0.0.1")
	_ = tierRank("provisional")
	_ = canaryScheduleSummaryFor("trusted")
}

// ---------------------------------------------------------------------------
// Misc helpers
// ---------------------------------------------------------------------------

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func boolKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
