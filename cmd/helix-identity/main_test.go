package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

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
