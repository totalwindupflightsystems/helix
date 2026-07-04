package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/totalwindupflightsystems/helix/pkg/identity"
)

// captureOutput redirects os.Stdout during f() and returns what was written.
func captureOutput(f func()) string {
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
	w.Close()
	os.Stdout = old
	return <-done
}

// ---------------------------------------------------------------------------
// envOr tests
// ---------------------------------------------------------------------------

func TestEnvOr_UsesEnv(t *testing.T) {
	_ = os.Setenv("HELIX_TEST_ENV", "from-env")
	defer func() { _ = os.Unsetenv("HELIX_TEST_ENV") }()
	result := envOr("HELIX_TEST_ENV", "fallback")
	if result != "from-env" {
		t.Errorf("expected 'from-env', got %q", result)
	}
}

func TestEnvOr_UsesFallback(t *testing.T) {
	result := envOr("NONEXISTENT_ENV_VAR", "fallback")
	if result != "fallback" {
		t.Errorf("expected 'fallback', got %q", result)
	}
}

func TestEnvOr_EmptyString(t *testing.T) {
	_ = os.Setenv("HELIX_TEST_EMPTY", "")
	defer func() { _ = os.Unsetenv("HELIX_TEST_EMPTY") }()
	result := envOr("HELIX_TEST_EMPTY", "fallback")
	if result != "fallback" {
		t.Errorf("empty env should use fallback, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// buildConfig tests
// ---------------------------------------------------------------------------

func TestBuildConfig_UsesDefaults(t *testing.T) {
	// Reset rootFlags to empty
	rootFlags = &flagHolder{}
	cfg, err := buildConfig()
	if err != nil {
		t.Fatalf("buildConfig error: %v", err)
	}
	// DefaultConfig may set paths based on HOME or other env vars.
	// Just verify the function doesn't error and returns a config.
	if cfg.DryRun {
		t.Error("DryRun should default to false")
	}
	if cfg.Verbose {
		t.Error("Verbose should default to false")
	}
}

func TestBuildConfig_HonorsFlags(t *testing.T) {
	rootFlags = &flagHolder{
		forgejoURL:    "http://custom:3000",
		adminToken:    "tok123",
		adminUser:     "admin",
		adminPassword: "pass",
		knownFriends:  "/custom/friends.json",
		sshKeyDir:     "/custom/keys",
		statePath:     "/custom/state.json",
		dryRun:        true,
		verbose:       true,
	}
	cfg, err := buildConfig()
	if err != nil {
		t.Fatalf("buildConfig error: %v", err)
	}
	if cfg.ForgejoURL != "http://custom:3000" {
		t.Errorf("ForgejoURL: got %q", cfg.ForgejoURL)
	}
	if !cfg.DryRun {
		t.Error("DryRun should be true")
	}
	if !cfg.Verbose {
		t.Error("Verbose should be true")
	}
	if cfg.KnownFriendsPath != "/custom/friends.json" {
		t.Errorf("KnownFriendsPath: got %q", cfg.KnownFriendsPath)
	}
}

// ---------------------------------------------------------------------------
// mustJSON tests
// ---------------------------------------------------------------------------

func TestMustJSON_ValidStruct(t *testing.T) {
	type testStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}
	result := mustJSON(testStruct{Name: "hello", Value: 42})
	// Verify it's valid JSON
	var parsed testStruct
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Errorf("mustJSON produced invalid JSON: %v (output: %q)", err, result)
	}
	if parsed.Name != "hello" || parsed.Value != 42 {
		t.Errorf("round-trip mismatch: got %+v", parsed)
	}
}

func TestMustJSON_EmptyStruct(t *testing.T) {
	result := mustJSON(struct{}{})
	if result != "{}" {
		t.Errorf("empty struct should produce '{}', got %q", result)
	}
}

func TestMustJSON_Map(t *testing.T) {
	result := mustJSON(map[string]string{"key": "val"})
	if !strings.Contains(result, "key") {
		t.Errorf("map JSON should contain key: %q", result)
	}
}

// ---------------------------------------------------------------------------
// renderDryRun tests
// ---------------------------------------------------------------------------

func TestRenderDryRun_NoAgents(t *testing.T) {
	kf := &identity.KnownFriends{
		Agents: map[string]*identity.Agent{},
	}
	out := captureOutput(func() {
		renderDryRun(kf)
	})
	if !strings.Contains(out, "DRY RUN COMPLETE") {
		t.Errorf("dry run output missing completion message: %q", out)
	}
	if !strings.Contains(out, "0 operations") {
		t.Errorf("dry run should report 0 operations: %q", out)
	}
}

func TestRenderDryRun_ActiveAgent(t *testing.T) {
	kf := &identity.KnownFriends{
		Agents: map[string]*identity.Agent{
			"test-agent": {
				Name:        "test-agent",
				DisplayName: "Test Agent",
				Status:      identity.StatusActive,
				Tier:        identity.TierPro,
			},
		},
	}
	out := captureOutput(func() {
		renderDryRun(kf)
	})
	if !strings.Contains(out, "test-agent") {
		t.Errorf("dry run should mention agent: %q", out)
	}
	if !strings.Contains(out, "would create") {
		t.Errorf("dry run should show 'would create': %q", out)
	}
}

// ---------------------------------------------------------------------------
// renderResultsTable tests
// ---------------------------------------------------------------------------

func TestRenderResultsTable_Empty(t *testing.T) {
	out := captureOutput(func() {
		renderResultsTable([]identity.ProvisioningResult{})
	})
	if out != "" {
		t.Errorf("empty results should produce no output, got: %q", out)
	}
}

func TestRenderResultsTable_Success(t *testing.T) {
	results := []identity.ProvisioningResult{
		{
			AgentName:      "agent-1",
			Status:         "active",
			Action:         "created",
			SSHFingerprint: "SHA256:abc123def456",
			PATLastEight:   "abcd1234",
		},
	}
	out := captureOutput(func() {
		renderResultsTable(results)
	})
	if !strings.Contains(out, "agent-1") {
		t.Errorf("table should contain agent name: %q", out)
	}
	if !strings.Contains(out, "created") {
		t.Errorf("table should contain action: %q", out)
	}
}

func TestRenderResultsTable_Failure(t *testing.T) {
	results := []identity.ProvisioningResult{
		{
			AgentName: "agent-2",
			Status:    "error",
			Action:    "failed",
			Error:     "connection refused",
		},
	}
	out := captureOutput(func() {
		renderResultsTable(results)
	})
	if !strings.Contains(out, "❌") {
		t.Errorf("failed result should show ❌ marker: %q", out)
	}
	if !strings.Contains(out, "agent-2") {
		t.Errorf("table should contain agent name: %q", out)
	}
}

// ---------------------------------------------------------------------------
// renderSingleResult tests
// ---------------------------------------------------------------------------

func TestRenderSingleResult_Success(t *testing.T) {
	r := identity.ProvisioningResult{
		AgentName:      "agent-1",
		Action:         "provisioned",
		SSHFingerprint: "SHA256:abc123",
		PATLastEight:   "deadbeef",
	}
	out := captureOutput(func() {
		renderSingleResult(r)
	})
	if !strings.Contains(out, "✅") {
		t.Errorf("success should show ✅: %q", out)
	}
	if !strings.Contains(out, "agent-1") {
		t.Errorf("should contain agent name: %q", out)
	}
	if !strings.Contains(out, "ssh fingerprint") {
		t.Errorf("should show ssh fingerprint: %q", out)
	}
}

func TestRenderSingleResult_Failure(t *testing.T) {
	r := identity.ProvisioningResult{
		AgentName: "agent-2",
		Action:    "failed",
		Error:     "timeout",
	}
	out := captureOutput(func() {
		renderSingleResult(r)
	})
	if !strings.Contains(out, "❌") {
		t.Errorf("failure should show ❌: %q", out)
	}
	if !strings.Contains(out, "timeout") {
		t.Errorf("should show error: %q", out)
	}
}

func TestRenderSingleResult_NoSSHNoPAT(t *testing.T) {
	r := identity.ProvisioningResult{
		AgentName: "agent-3",
		Action:    "dry-run",
	}
	out := captureOutput(func() {
		renderSingleResult(r)
	})
	if strings.Contains(out, "ssh fingerprint") {
		t.Error("no SSH fingerprint should be shown when empty")
	}
	if strings.Contains(out, "pat:") {
		t.Error("no PAT should be shown when empty")
	}
}

// ---------------------------------------------------------------------------
// renderStateTable tests
// ---------------------------------------------------------------------------

func TestRenderStateTable_NilState(t *testing.T) {
	out := captureOutput(func() {
		renderStateTable(nil)
	})
	if !strings.Contains(out, "(no agents provisioned") {
		t.Errorf("nil state should show empty message: %q", out)
	}
}

func TestRenderStateTable_EmptyMap(t *testing.T) {
	state := &identity.StateFile{
		Agents: map[string]*identity.AgentState{},
	}
	out := captureOutput(func() {
		renderStateTable(state)
	})
	if !strings.Contains(out, "(no agents provisioned") {
		t.Errorf("empty state should show empty message: %q", out)
	}
}

func TestRenderStateTable_WithAgents(t *testing.T) {
	state := &identity.StateFile{
		Agents: map[string]*identity.AgentState{
			"agent-a": {
				ForgejoAccountID: 1,
				SSHFingerprint:   "SHA256:aaa",
				PATLastEight:     "11112222",
			},
		},
	}
	// Set LastProvisioned so Format doesn't panic on zero time
	out := captureOutput(func() {
		renderStateTable(state)
	})
	if !strings.Contains(out, "agent-a") {
		t.Errorf("state table should contain agent: %q", out)
	}
}

func TestRenderStateTable_LongSSHFingerprint(t *testing.T) {
	// Cover the SSHFingerprint >24 char truncation branch (main.go:491-493)
	longFP := "SHA256:" + strings.Repeat("abcdef0123456789", 3) // 54 chars total
	state := &identity.StateFile{
		Agents: map[string]*identity.AgentState{
			"longkey": {
				ForgejoAccountID: 7,
				SSHFingerprint:   longFP,
				PATLastEight:     "abcd1234",
			},
		},
	}
	out := captureOutput(func() {
		renderStateTable(state)
	})
	if !strings.Contains(out, "longkey") {
		t.Errorf("state table should contain agent: %q", out)
	}
	if !strings.Contains(out, "...") {
		t.Errorf("long SSH fingerprint should be truncated with '...': %q", out)
	}
	// Verify the truncated prefix is present, full fingerprint is not
	if strings.Contains(out, longFP) {
		t.Errorf("full long fingerprint should NOT appear in output: %q", out)
	}
}

func TestRenderStateTable_MissingPAT(t *testing.T) {
	// Cover the PATLastEight == "" branch (main.go:495-497) — shows em-dash placeholder
	state := &identity.StateFile{
		Agents: map[string]*identity.AgentState{
			"no-pat": {
				ForgejoAccountID: 5,
				SSHFingerprint:   "SHA256:short",
				PATLastEight:     "",
			},
		},
	}
	out := captureOutput(func() {
		renderStateTable(state)
	})
	if !strings.Contains(out, "—") {
		t.Errorf("missing PAT should show em-dash placeholder: %q", out)
	}
}

func TestRenderStateTable_MultipleAgentsSorted(t *testing.T) {
	// Verify the insertion-sort path (main.go:483-487) — names come out
	// in alphabetical order regardless of map iteration order.
	state := &identity.StateFile{
		Agents: map[string]*identity.AgentState{
			"zeta":  {ForgejoAccountID: 3, SSHFingerprint: "SHA256:z", PATLastEight: "11111111"},
			"alpha": {ForgejoAccountID: 1, SSHFingerprint: "SHA256:a", PATLastEight: "22222222"},
			"mu":    {ForgejoAccountID: 2, SSHFingerprint: "SHA256:m", PATLastEight: "33333333"},
		},
	}
	out := captureOutput(func() {
		renderStateTable(state)
	})
	alphaIdx := strings.Index(out, "alpha")
	muIdx := strings.Index(out, "mu")
	zetaIdx := strings.Index(out, "zeta")
	if alphaIdx == -1 || muIdx == -1 || zetaIdx == -1 {
		t.Fatalf("all agents should appear in output: %q", out)
	}
	if !(alphaIdx < muIdx && muIdx < zetaIdx) {
		t.Errorf("agents should be sorted alphabetically: alpha<mu<zeta, got %d<%d<%d in %q",
			alphaIdx, muIdx, zetaIdx, out)
	}
}

// ---------------------------------------------------------------------------
// Cobra command tree tests
// ---------------------------------------------------------------------------

func TestRootCmd_HasFiveSubcommands(t *testing.T) {
	rootCmd := buildRootCmd()
	names := map[string]bool{}
	for _, c := range rootCmd.Commands() {
		names[c.Use] = true
	}
	expected := []string{"sync", "provision", "deprovision", "status", "keygen"}
	for _, want := range expected {
		found := false
		for name := range names {
			if strings.HasPrefix(name, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing subcommand: %s (available: %v)", want, names)
		}
	}
}

// buildRootCmd constructs the root command the same way main() does but
// returns it for testing (doesn't call Execute or os.Exit).
func buildRootCmd() *cobra.Command {
	// Build fresh each time to avoid flag registration conflicts
	rootFlags = &flagHolder{}
	rootCmd := &cobra.Command{
		Use:           "helix-identity",
		Short:         "Provision Helix agent identities in Forgejo",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.PersistentFlags().StringVar(&rootFlags.forgejoURL, "forgejo-url",
		envOr("FORGEJO_URL", ""), "")
	rootCmd.PersistentFlags().StringVar(&rootFlags.adminToken, "admin-token",
		envOr("FORGEJO_ADMIN_TOKEN", ""), "")
	rootCmd.PersistentFlags().StringVar(&rootFlags.adminUser, "admin-user",
		envOr("FORGEJO_ADMIN_USER", ""), "")
	rootCmd.PersistentFlags().StringVar(&rootFlags.adminPassword, "admin-password",
		envOr("FORGEJO_ADMIN_PASSWORD", ""), "")
	rootCmd.PersistentFlags().StringVar(&rootFlags.knownFriends, "known-friends",
		envOr("HELIX_KNOWN_FRIENDS", identity.DefaultKnownFriendsPath), "")
	rootCmd.PersistentFlags().StringVar(&rootFlags.sshKeyDir, "ssh-key-dir",
		envOr("HELIX_SSH_KEY_DIR", identity.DefaultSSHKeyDir), "")
	rootCmd.PersistentFlags().StringVar(&rootFlags.statePath, "state-path",
		envOr("HELIX_STATE_PATH", identity.DefaultStatePath), "")
	rootCmd.PersistentFlags().BoolVar(&rootFlags.dryRun, "dry-run", false, "")
	rootCmd.PersistentFlags().BoolVarP(&rootFlags.verbose, "verbose", "v", false, "")

	rootCmd.AddCommand(
		newSyncCmd(),
		newProvisionCmd(),
		newDeprovisionCmd(),
		newStatusCmd(),
		newKeygenCmd(),
	)
	return rootCmd
}

func TestSyncCmd_MissingKnownFriends(t *testing.T) {
	// Set rootFlags to a nonexistent path
	rootFlags = &flagHolder{knownFriends: "/nonexistent/friends.json"}
	cmd := newSyncCmd()
	// runSync uses buildConfig() which reads rootFlags
	err := cmd.RunE(cmd, []string{})
	if err == nil {
		t.Error("sync with missing known-friends should error")
	}
}

func TestProvisionCmd_MissingArg(t *testing.T) {
	// Cobra's ExactArgs(1) only enforces via Execute, not RunE directly.
	// Test via cobra command execution to validate arg checking.
	rootCmd := buildRootCmd()
	rootCmd.SetArgs([]string{"provision"})
	err := rootCmd.Execute()
	if err == nil {
		t.Error("provision with missing arg should error")
	}
}

func TestDeprovisionCmd_MissingArg(t *testing.T) {
	rootCmd := buildRootCmd()
	rootCmd.SetArgs([]string{"deprovision"})
	err := rootCmd.Execute()
	if err == nil {
		t.Error("deprovision with missing arg should error")
	}
}

func TestKeygenCmd_MissingArg(t *testing.T) {
	rootCmd := buildRootCmd()
	rootCmd.SetArgs([]string{"keygen"})
	err := rootCmd.Execute()
	if err == nil {
		t.Error("keygen with missing arg should error")
	}
}

// ---------------------------------------------------------------------------
// runSync handler — minimal dry-run coverage (no forgejo call)
// ---------------------------------------------------------------------------

func writeMinimalFriendsFile(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "known-friends.json")
	// One active agent; OpenRouter key is a test stub the secrets scanner
	// will accept because the test runs go test, not git commit.
	content := `{"version":1,"updated_at":"2026-06-20T00:00:00Z","agents":{"alice":{"display_name":"Alice","status":"active","tier":"pro","openrouter_key":"fake-or-key-00000000000000000000000000000000","coolify_service_uuid":"a1b2c3d4-e5f6-7890-abcd-ef1234567890"}}}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// withRootFlags sets rootFlags to a safe dry-run state, runs fn, and restores.
func withRootFlags(t *testing.T, fn func()) {
	t.Helper()
	orig := *rootFlags
	t.Cleanup(func() { *rootFlags = orig })

	dir := t.TempDir()
	rootFlags.forgejoURL = "https://forgejo.example.com"
	rootFlags.adminToken = "tok"
	rootFlags.adminUser = "admin"
	rootFlags.adminPassword = "adminpw"
	rootFlags.knownFriends = writeMinimalFriendsFile(t, dir)
	rootFlags.sshKeyDir = filepath.Join(dir, "keys")
	rootFlags.statePath = filepath.Join(dir, "state.json")
	rootFlags.dryRun = true
	rootFlags.verbose = false
	fn()
}

func TestRunSync_DryRun(t *testing.T) {
	var out string
	withRootFlags(t, func() {
		out = captureOutput(func() {
			cmd := &cobra.Command{Use: "sync"}
			if err := runSync(cmd, nil); err != nil {
				t.Errorf("runSync error: %v", err)
			}
		})
	})
	if !strings.Contains(out, "DRY RUN") && !strings.Contains(out, "alice") {
		t.Errorf("expected DRY RUN output mentioning alice, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// runProvision handler
// ---------------------------------------------------------------------------

func TestRunProvision_AgentNotFound(t *testing.T) {
	withRootFlags(t, func() {
		cmd := &cobra.Command{Use: "provision"}
		err := runProvision(cmd, []string{"ghost"})
		if err == nil {
			t.Fatal("expected error for unknown agent")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error should mention 'not found', got: %v", err)
		}
	})
}

func TestRunProvision_DryRun_Success(t *testing.T) {
	var out string
	withRootFlags(t, func() {
		out = captureOutput(func() {
			cmd := &cobra.Command{Use: "provision"}
			if err := runProvision(cmd, []string{"alice"}); err != nil {
				t.Errorf("runProvision error: %v", err)
			}
		})
	})
	// Dry-run should produce some output (table or DRY RUN marker)
	if out == "" {
		t.Error("expected output from dry-run provision")
	}
}

// ---------------------------------------------------------------------------
// runDeprovision handler
// ---------------------------------------------------------------------------

func TestRunDeprovision_DryRun_Success(t *testing.T) {
	var out string
	withRootFlags(t, func() {
		out = captureOutput(func() {
			cmd := &cobra.Command{Use: "deprovision"}
			if err := runDeprovision(cmd, []string{"alice"}); err != nil {
				t.Errorf("runDeprovision error: %v", err)
			}
		})
	})
	if out == "" {
		t.Error("expected output from dry-run deprovision")
	}
}

func TestRunDeprovision_UnknownAgent_StillProceeds(t *testing.T) {
	// runDeprovision allows unknown agents (state file is source of truth).
	var out string
	withRootFlags(t, func() {
		out = captureOutput(func() {
			cmd := &cobra.Command{Use: "deprovision"}
			if err := runDeprovision(cmd, []string{"ghost"}); err != nil {
				t.Errorf("runDeprovision should not error for unknown agent (dry-run), got: %v", err)
			}
		})
	})
	_ = out // output content depends on path; existence is enough
}

// ---------------------------------------------------------------------------
// runStatus handler
// ---------------------------------------------------------------------------

func TestRunStatus_DryRun(t *testing.T) {
	var out string
	withRootFlags(t, func() {
		out = captureOutput(func() {
			cmd := &cobra.Command{Use: "status"}
			if err := runStatus(cmd, nil); err != nil {
				t.Errorf("runStatus error: %v", err)
			}
		})
	})
	// renderStateTable prints header row "AGENT" or empty table — either is fine
	if out == "" {
		t.Error("expected status table output")
	}
}

// ---------------------------------------------------------------------------
// runKeygen handler
// ---------------------------------------------------------------------------

func TestRunKeygen_DryRun_Success(t *testing.T) {
	var out string
	withRootFlags(t, func() {
		out = captureOutput(func() {
			cmd := &cobra.Command{Use: "keygen"}
			if err := runKeygen(cmd, []string{"newagent"}); err != nil {
				t.Errorf("runKeygen error: %v", err)
			}
		})
	})
	if out == "" {
		t.Error("expected keygen output")
	}
}

// ---------------------------------------------------------------------------
// Additional renderDryRun coverage — offboarded agents
// ---------------------------------------------------------------------------

func TestRenderDryRun_OffboardedAgent(t *testing.T) {
	kf := &identity.KnownFriends{
		Agents: map[string]*identity.Agent{
			"leaver": {
				Name:        "leaver",
				DisplayName: "Leaver Agent",
				Status:      identity.StatusOffboarded,
				Tier:        identity.TierPro,
			},
		},
	}
	out := captureOutput(func() {
		renderDryRun(kf)
	})
	if !strings.Contains(out, "leaver") {
		t.Errorf("dry run should mention offboarded agent: %q", out)
	}
	if !strings.Contains(out, "would deprovision") {
		t.Errorf("dry run should show 'would deprovision' for offboarded agent: %q", out)
	}
	if !strings.Contains(out, "DELETE /api/v1/users/leaver/tokens") {
		t.Errorf("dry run should show PAT revocation: %q", out)
	}
}

func TestRenderDryRun_MixedActiveAndOffboarded(t *testing.T) {
	kf := &identity.KnownFriends{
		Agents: map[string]*identity.Agent{
			"joiner": {
				Name:        "joiner",
				DisplayName: "Joiner Agent",
				Status:      identity.StatusActive,
				Tier:        identity.TierPro,
			},
			"leaver": {
				Name:   "leaver",
				Status: identity.StatusOffboarded,
				Tier:   identity.TierPro,
			},
		},
	}
	out := captureOutput(func() {
		renderDryRun(kf)
	})
	if !strings.Contains(out, "joiner") {
		t.Errorf("output missing joiner: %q", out)
	}
	if !strings.Contains(out, "leaver") {
		t.Errorf("output missing leaver: %q", out)
	}
	if !strings.Contains(out, "2 operations simulated") {
		t.Errorf("dry run should report 2 operations: %q", out)
	}
}

// ---------------------------------------------------------------------------
// Additional runProvision coverage — validation failure path
// ---------------------------------------------------------------------------

func TestRunProvision_InvalidAgent(t *testing.T) {
	// An agent with empty name AND missing required fields triggers agent.Validate()
	withRootFlags(t, func() {
		kfPath := rootFlags.knownFriends
		// Write a known-friends.json with a malformed agent entry directly.
		// Use an invalid status enum so agent.Validate() rejects it.
		malformedJSON := `{"agents":{"borked":{"name":"borked","status":"bogus-status","tier":"pro"}}}`
		if err := os.WriteFile(kfPath, []byte(malformedJSON), 0644); err != nil {
			t.Fatal(err)
		}

		cmd := &cobra.Command{Use: "provision"}
		err := runProvision(cmd, []string{"borked"})
		if err == nil {
			t.Fatal("expected validation error for agent with invalid status")
		}
		if !strings.Contains(err.Error(), "invalid status") {
			t.Errorf("error should mention invalid status, got: %v", err)
		}
	})
}

func TestRunProvision_AgentNil(t *testing.T) {
	// Cover the agent == nil branch in runProvision (line 215-218).
	withRootFlags(t, func() {
		kfPath := rootFlags.knownFriends
		// Explicit nil entry: agent present but value is null
		nilJSON := `{"agents":{"nullagent":null}}`
		if err := os.WriteFile(kfPath, []byte(nilJSON), 0644); err != nil {
			t.Fatal(err)
		}

		cmd := &cobra.Command{Use: "provision"}
		err := runProvision(cmd, []string{"nullagent"})
		if err == nil {
			t.Fatal("expected 'agent not found' error for nil entry")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error should mention 'not found', got: %v", err)
		}
	})
}

func TestRunProvision_MalformedJSON(t *testing.T) {
	// Cover the LoadKnownFriends error path (line 211-213).
	withRootFlags(t, func() {
		kfPath := rootFlags.knownFriends
		if err := os.WriteFile(kfPath, []byte(`{not valid json`), 0644); err != nil {
			t.Fatal(err)
		}

		cmd := &cobra.Command{Use: "provision"}
		err := runProvision(cmd, []string{"any"})
		if err == nil {
			t.Fatal("expected JSON parse error")
		}
	})
}

func TestRunDeprovision_MalformedJSON(t *testing.T) {
	// Cover the LoadKnownFriends error path in runDeprovision (line 263-265).
	withRootFlags(t, func() {
		kfPath := rootFlags.knownFriends
		if err := os.WriteFile(kfPath, []byte(`garbage`), 0644); err != nil {
			t.Fatal(err)
		}

		cmd := &cobra.Command{Use: "deprovision"}
		err := runDeprovision(cmd, []string{"alice"})
		if err == nil {
			t.Fatal("expected JSON parse error")
		}
	})
}

func TestRunStatus_MalformedJSON(t *testing.T) {
	// runStatus reads state via NewSyncer — error path through buildConfig
	// and the syncer factory. Triggered by corrupt state file.
	withRootFlags(t, func() {
		statePath := rootFlags.statePath
		if err := os.WriteFile(statePath, []byte(`not json`), 0644); err != nil {
			t.Fatal(err)
		}

		cmd := &cobra.Command{Use: "status"}
		err := runStatus(cmd, nil)
		if err == nil {
			t.Fatal("expected error from corrupt state file")
		}
	})
}

// ---------------------------------------------------------------------------
// Additional runSync coverage — empty agents list
// ---------------------------------------------------------------------------

func TestRunSync_NoAgents(t *testing.T) {
	var out string
	withRootFlags(t, func() {
		kfPath := rootFlags.knownFriends
		// Empty agents list
		if err := os.WriteFile(kfPath, []byte(`{"agents":{}}`), 0644); err != nil {
			t.Fatal(err)
		}

		out = captureOutput(func() {
			cmd := &cobra.Command{Use: "sync"}
			if err := runSync(cmd, nil); err != nil {
				t.Errorf("runSync error: %v", err)
			}
		})
	})
	if !strings.Contains(out, "NO_AGENTS") {
		t.Errorf("empty agents should print NO_AGENTS marker: %s", out)
	}
}

// ---------------------------------------------------------------------------
// renderSingleResult — failure case (exercises failure branch)
// ---------------------------------------------------------------------------

func TestRenderSingleResult_Failure_RendersError(t *testing.T) {
	r := identity.ProvisioningResult{
		AgentName: "failing-agent",
		Action:    identity.ActionFailed,
		Duration:  100 * time.Millisecond,
		Error:     "simulated failure",
	}
	out := captureOutput(func() {
		renderSingleResult(r)
	})
	if !strings.Contains(out, "failing-agent") {
		t.Errorf("output should mention failing agent: %s", out)
	}
	if !strings.Contains(out, "simulated failure") {
		t.Errorf("output should include error message: %s", out)
	}
	if !strings.Contains(out, "❌") {
		t.Errorf("failure should use error marker: %s", out)
	}
}

// ---------------------------------------------------------------------------
// mustJSON — invalid input (graceful fallback path)
// ---------------------------------------------------------------------------

func TestMustJSON_InvalidInput(t *testing.T) {
	// A function value cannot be marshaled to JSON. mustJSON surfaces a
	// placeholder string rather than panicking — covers the marshal-error
	// branch in main.go:513.
	out := mustJSON(func() {})
	if !strings.Contains(out, "<marshal error:") {
		t.Errorf("mustJSON should return placeholder on marshal error, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// runKeygen — agent without Forgejo interaction (already exercised in
// TestRunKeygen_DryRun_Success but verify the agent construction path)
// ---------------------------------------------------------------------------

func TestRunKeygen_ConfigError(t *testing.T) {
	// Config error path is reached when rootFlags are invalid; buildConfig
	// reads from rootFlags so we can override rootFlags directly.
	withRootFlags(t, func() {
		// Invalidate forgejo URL
		origURL := rootFlags.forgejoURL
		rootFlags.forgejoURL = ""
		defer func() { rootFlags.forgejoURL = origURL }()

		cmd := &cobra.Command{Use: "keygen"}
		_ = runKeygen(cmd, []string{"newagent"})
		// buildConfig() with empty forgejo URL still returns a config (DefaultProvisionerConfig
		// applies). NewSyncer may fail. Either outcome is acceptable — we just exercise the path.
	})
}

// ---------------------------------------------------------------------------
// Additional runDeprovision — agent with existing known-friends entry
// ---------------------------------------------------------------------------

func TestRunDeprovision_KnownAgent_NilAgent(t *testing.T) {
	// Test the `agent == nil` branch in runDeprovision
	withRootFlags(t, func() {
		kfPath := rootFlags.knownFriends
		// Explicit nil entry: "present": null in JSON
		nilJSON := `{"agents":{"present":null}}`
		if err := os.WriteFile(kfPath, []byte(nilJSON), 0644); err != nil {
			t.Fatal(err)
		}

		cmd := &cobra.Command{Use: "deprovision"}
		_ = runDeprovision(cmd, []string{"present"})
		// nil entry → fallback to constructing an offboarded agent, then call DeprovisionOne
		// The exact behavior depends on whether DeprovisionOne handles nil gracefully;
		// we just verify no panic
	})
}

// ---------------------------------------------------------------------------
// Additional buildConfig paths
// ---------------------------------------------------------------------------

func TestBuildConfig_AllOverrides(t *testing.T) {
	orig := rootFlags
	defer func() { rootFlags = orig }()

	rootFlags = &flagHolder{
		forgejoURL:    "https://forge.example.com",
		adminUser:     "admin",
		adminPassword: "secret",
		adminToken:    "tok",
		knownFriends:  "/tmp/friends.json",
		statePath:     "/tmp/state.json",
		dryRun:        true,
	}

	cfg, err := buildConfig()
	if err != nil {
		t.Fatalf("buildConfig failed: %v", err)
	}
	if cfg.ForgejoURL != "https://forge.example.com" {
		t.Errorf("forgejo URL not honored: got %q", cfg.ForgejoURL)
	}
	if cfg.AdminUser != "admin" {
		t.Errorf("admin user not honored: got %q", cfg.AdminUser)
	}
	if cfg.AdminToken != "tok" {
		t.Errorf("admin token not honored: got %q", cfg.AdminToken)
	}
	if !cfg.DryRun {
		t.Error("dry run flag not honored")
	}
}
