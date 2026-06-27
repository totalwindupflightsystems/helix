package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/helix/pkg/negotiate"
)

// captureStdout runs fn and returns captured stdout.
func captureStdout2(t *testing.T, fn func(w *os.File)) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	fn(w)
	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

// ---------------------------------------------------------------------------
// newRootCmd
// ---------------------------------------------------------------------------

func TestNewRootCmd(t *testing.T) {
	root := newRootCmd()
	if root.Use != "helix-negotiate" {
		t.Errorf("Use = %q, want helix-negotiate", root.Use)
	}
	children := root.Commands()
	if len(children) != 2 {
		t.Errorf("got %d subcommands, want 2 (debate, resolve)", len(children))
	}
	names := make(map[string]bool)
	for _, c := range children {
		names[c.Name()] = true
	}
	for _, want := range []string{"debate", "resolve"} {
		if !names[want] {
			t.Errorf("missing subcommand %q", want)
		}
	}
	// Persistent flags
	configPath, _ := root.PersistentFlags().GetString("config")
	if configPath == "" {
		t.Error("config flag should have default")
	}
}

// ---------------------------------------------------------------------------
// newDebateCmd
// ---------------------------------------------------------------------------

func TestNewDebateCmd(t *testing.T) {
	gOpts := &globalOptions{}
	cmd := newDebateCmd(gOpts)
	if cmd.Use != "debate <pr-url>" {
		t.Errorf("Use = %q", cmd.Use)
	}
	ff := cmd.Flags()
	maxRounds, _ := ff.GetInt("max-rounds")
	if maxRounds != 3 {
		t.Errorf("max-rounds default = %d, want 3", maxRounds)
	}
	dryRun, _ := ff.GetBool("dry-run")
	if dryRun {
		t.Error("dry-run default should be false")
	}
	chimeraURL, _ := ff.GetString("chimera-url")
	if chimeraURL != "http://localhost:8765" {
		t.Errorf("chimera-url default = %q", chimeraURL)
	}
	verdictA, _ := ff.GetString("verdict-a")
	if verdictA != "APPROVED" {
		t.Errorf("verdict-a default = %q", verdictA)
	}
	verdictB, _ := ff.GetString("verdict-b")
	if verdictB != "REQUEST_CHANGES" {
		t.Errorf("verdict-b default = %q", verdictB)
	}
}

// ---------------------------------------------------------------------------
// newResolveCmd
// ---------------------------------------------------------------------------

func TestNewResolveCmd(t *testing.T) {
	gOpts := &globalOptions{}
	cmd := newResolveCmd(gOpts)
	if cmd.Use != "resolve [pr-url]" {
		t.Errorf("Use = %q", cmd.Use)
	}
	ff := cmd.Flags()
	forceChimera, _ := ff.GetBool("force-chimera")
	if forceChimera {
		t.Error("force-chimera default should be false")
	}
	pr, _ := ff.GetInt("pr")
	if pr != 0 {
		t.Errorf("pr default = %d, want 0", pr)
	}
}

// ---------------------------------------------------------------------------
// defaultConfigPath
// ---------------------------------------------------------------------------

func TestDefaultConfigPath(t *testing.T) {
	path := defaultConfigPath()
	if path == "" {
		t.Error("defaultConfigPath should return non-empty path")
	}
	// Should end with known-friends.json
	if !strings.HasSuffix(path, "known-friends.json") {
		t.Errorf("expected known-friends.json suffix, got %s", path)
	}
}

// ---------------------------------------------------------------------------
// lookupAgent
// ---------------------------------------------------------------------------

func TestLookupAgent(t *testing.T) {
	agents := map[string]friendAgent{
		"alfa": {Name: "alfa", TrustLevel: 90, ForgejoUser: "alfa-bot", Tier: "pro"},
		"beta": {Name: "beta", TrustLevel: 50, ForgejoUser: "beta-bot", Tier: "flash"},
	}

	t.Run("found", func(t *testing.T) {
		a := lookupAgent(agents, "alfa")
		if a.Name != "alfa" {
			t.Errorf("Name = %q", a.Name)
		}
		if a.TrustLevel != 90 {
			t.Errorf("TrustLevel = %d", a.TrustLevel)
		}
		if a.ForgejoUser != "alfa-bot" {
			t.Errorf("ForgejoUser = %q", a.ForgejoUser)
		}
		if a.Tier != "pro" {
			t.Errorf("Tier = %q", a.Tier)
		}
	})

	t.Run("not_found_defaults", func(t *testing.T) {
		a := lookupAgent(agents, "unknown")
		if a.Name != "unknown" {
			t.Errorf("Name = %q, want unknown", a.Name)
		}
		if a.Tier != "pro" {
			t.Errorf("Tier = %q, want pro for default", a.Tier)
		}
	})

	t.Run("empty_map", func(t *testing.T) {
		a := lookupAgent(nil, "someone")
		if a.Name != "someone" {
			t.Errorf("Name = %q", a.Name)
		}
		if a.Tier != "pro" {
			t.Errorf("Tier = %q", a.Tier)
		}
	})
}

// ---------------------------------------------------------------------------
// verdictEmoji
// ---------------------------------------------------------------------------

func TestVerdictEmoji(t *testing.T) {
	t.Run("approved", func(t *testing.T) {
		if got := verdictEmoji(negotiate.VerdictApproved); got != "OK" {
			t.Errorf("got %q, want OK", got)
		}
	})
	t.Run("request_changes", func(t *testing.T) {
		if got := verdictEmoji(negotiate.VerdictRequestChanges); got != "CHANGES" {
			t.Errorf("got %q, want CHANGES", got)
		}
	})
	t.Run("unknown", func(t *testing.T) {
		if got := verdictEmoji(negotiate.Verdict("UNKNOWN")); got != "?" {
			t.Errorf("got %q, want ?", got)
		}
	})
	t.Run("empty", func(t *testing.T) {
		if got := verdictEmoji(negotiate.Verdict("")); got != "?" {
			t.Errorf("got %q, want ?", got)
		}
	})
}

// ---------------------------------------------------------------------------
// parsePRNumber
// ---------------------------------------------------------------------------

func TestParsePRNumber(t *testing.T) {
	tests := []struct {
		name    string
		prURL   string
		wantNum int
		wantErr bool
	}{
		{"standard_forgejo_url", "https://forgejo.helix.local/helix/core/pulls/42", 42, false},
		{"bare_number", "42", 42, false},
		{"multiple_numbers", "https://example.com/pr/5/files/12", 12, false},
		{"no_number", "https://example.com/pr/", 0, true},
		{"empty_string", "", 0, true},
		{"text_only", "no-numbers-here", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePRNumber(tt.prURL)
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if got != tt.wantNum {
				t.Errorf("got %d, want %d", got, tt.wantNum)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// auditLogPath
// ---------------------------------------------------------------------------

func TestAuditLogPath(t *testing.T) {
	path := auditLogPath(42)
	if path == "" {
		t.Error("auditLogPath should return non-empty path")
	}
	if !strings.Contains(path, "42") {
		t.Errorf("auditLogPath should contain PR number 42, got %s", path)
	}
	if !strings.HasSuffix(path, ".jsonl") {
		t.Errorf("auditLogPath should end with .jsonl, got %s", path)
	}
}

// ---------------------------------------------------------------------------
// renderNegotiationResult
// ---------------------------------------------------------------------------

func TestRenderNegotiationResult(t *testing.T) {
	t.Run("resolved_with_chimera", func(t *testing.T) {
		neg := &negotiate.Negotiator{
			Neg: &negotiate.Negotiation{
				PRNumber: 42,
				State:    negotiate.StateResolved,
				Round:    3,
			},
			ChimeraResult: &negotiate.ChimeraVerdict{
				Verdict:    "APPROVE",
				Confidence: 0.92,
				Cost:       0.015,
			},
		}
		out := captureStdout2(t, func(w *os.File) {
			renderNegotiationResult(w, neg, "/tmp/test.jsonl")
		})
		if !strings.Contains(out, "PR #42") {
			t.Errorf("missing PR number: %s", out)
		}
		if !strings.Contains(out, "resolved") {
			t.Errorf("missing state: %s", out)
		}
		if !strings.Contains(out, "Chimera verdict") {
			t.Errorf("missing chimera verdict: %s", out)
		}
		if !strings.Contains(out, "/tmp/test.jsonl") {
			t.Errorf("missing audit path: %s", out)
		}
	})

	t.Run("without_chimera_result", func(t *testing.T) {
		neg := &negotiate.Negotiator{
			Neg: &negotiate.Negotiation{
				PRNumber: 7,
				State:    negotiate.StateEscalated,
				Round:    2,
			},
		}
		out := captureStdout2(t, func(w *os.File) {
			renderNegotiationResult(w, neg, "/tmp/test2.jsonl")
		})
		if !strings.Contains(out, "PR #7") {
			t.Errorf("missing PR number: %s", out)
		}
		if !strings.Contains(out, "escalated") {
			t.Errorf("missing state: %s", out)
		}
		if strings.Contains(out, "Chimera verdict") {
			t.Errorf("should not have chimera verdict: %s", out)
		}
	})
}

// ---------------------------------------------------------------------------
// Command tree (smoke)
// ---------------------------------------------------------------------------

func TestCommandTree(t *testing.T) {
	// Redirect stderr to avoid polluting test output from cobra error messages
	saveStderr := os.Stderr
	os.Stderr, _ = os.Open(os.DevNull)
	defer func() { os.Stderr = saveStderr }()

	t.Run("debate_help", func(t *testing.T) {
		root := newRootCmd()
		root.SetArgs([]string{"debate", "--help"})
		err := root.Execute()
		if err != nil {
			t.Logf("Cobra --help may return error: %v", err)
		}
	})

	t.Run("resolve_help", func(t *testing.T) {
		root := newRootCmd()
		root.SetArgs([]string{"resolve", "--help"})
		err := root.Execute()
		if err != nil {
			t.Logf("Cobra --help may return error: %v", err)
		}
	})

	t.Run("debate_missing_args", func(t *testing.T) {
		root := newRootCmd()
		root.SetArgs([]string{"debate"})
		err := root.Execute()
		if err == nil {
			t.Log("Cobra ContinueOnError returns nil for missing required args")
		}
	})
}

// ---------------------------------------------------------------------------
// loadAgents
// ---------------------------------------------------------------------------

func TestLoadAgents(t *testing.T) {
	t.Run("wrapped_shape", func(t *testing.T) {
		dir := t.TempDir()
		path := dir + "/friends.json"
		writeFile(t, path, `{"version": 1, "agents": {"alfa": {"forgejo_username": "alfa-user", "tier": "pro", "trust_level": 80}}}`)
		agents, err := loadAgents(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(agents) != 1 {
			t.Fatalf("expected 1 agent, got %d", len(agents))
		}
		if agents["alfa"].ForgejoUser != "alfa-user" {
			t.Errorf("expected alfa-user, got %s", agents["alfa"].ForgejoUser)
		}
		if agents["alfa"].Tier != "pro" {
			t.Errorf("expected pro tier, got %s", agents["alfa"].Tier)
		}
	})

	t.Run("bare_shape", func(t *testing.T) {
		dir := t.TempDir()
		path := dir + "/friends.json"
		writeFile(t, path, `{"bravo": {"forgejo_username": "bravo-user", "tier": "flash", "trust_level": 60}}`)
		agents, err := loadAgents(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(agents) != 1 {
			t.Fatalf("expected 1 agent, got %d", len(agents))
		}
		if agents["bravo"].ForgejoUser != "bravo-user" {
			t.Errorf("expected bravo-user, got %s", agents["bravo"].ForgejoUser)
		}
	})

	t.Run("file_not_found", func(t *testing.T) {
		_, err := loadAgents("/nonexistent/known-friends.json")
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("invalid_json", func(t *testing.T) {
		dir := t.TempDir()
		path := dir + "/friends.json"
		writeFile(t, path, `not json`)
		_, err := loadAgents(path)
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("wrapped_empty_agents_falls_through_to_bare_error", func(t *testing.T) {
		dir := t.TempDir()
		path := dir + "/friends.json"
		// Wrapped shape with empty agents: len(Agents)==0, so falls through
		// to bare unmarshal, which fails because "version" is not a friendAgent.
		writeFile(t, path, `{"version": 1, "agents": {}}`)
		_, err := loadAgents(path)
		if err == nil {
			t.Fatal("expected error: empty wrapped agents falls to bare parse which rejects 'version' key")
		}
	})

	t.Run("wrapped_with_no_agents_key_falls_back_to_bare", func(t *testing.T) {
		dir := t.TempDir()
		path := dir + "/friends.json"
		writeFile(t, path, `{"charlie": {"forgejo_username": "charlie-user", "tier": "pro"}}`)
		agents, err := loadAgents(path)
		if err != nil {
			t.Fatal(err)
		}
		if agents["charlie"].ForgejoUser != "charlie-user" {
			t.Errorf("expected charlie-user, got %s", agents["charlie"].ForgejoUser)
		}
	})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
