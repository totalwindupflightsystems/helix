package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

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
	_, _ = buf.ReadFrom(r)
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

// ---------------------------------------------------------------------------
// runDebate handler
// ---------------------------------------------------------------------------

func TestRunDebate_MissingAgents(t *testing.T) {
	// Capture stderr for the error case
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = oldStderr }()

	opts := &debateOptions{
		globalOptions: &globalOptions{},
		// agent-a and agent-b empty
	}
	err = runDebate(opts, "http://localhost:3030/helix/helix/pulls/1")
	w.Close()

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	if err == nil {
		t.Fatal("expected error for missing --agent-a/--agent-b")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("error %q should mention 'required'", err.Error())
	}
}

func TestRunDebate_BadPRURL(t *testing.T) {
	opts := &debateOptions{
		globalOptions: &globalOptions{},
		agentA:        "alice",
		agentB:        "bob",
	}
	err := runDebate(opts, "not-a-url")
	if err == nil {
		t.Fatal("expected error for malformed PR URL")
	}
}

func TestRunDebate_NoConflict(t *testing.T) {
	// Both verdicts APPROVED → no conflict → returns nil after printing
	out := captureStdout2(t, func(w *os.File) {
		old := os.Stdout
		os.Stdout = w
		defer func() { os.Stdout = old }()

		opts := &debateOptions{
			globalOptions: &globalOptions{configPath: defaultConfigPath()},
			agentA:        "alice",
			agentB:        "bob",
			verdictA:      "APPROVED",
			verdictB:      "APPROVED",
		}
		if err := runDebate(opts, "http://localhost:3030/helix/helix/pulls/42"); err != nil {
			fmt.Fprintf(old, "runDebate error: %v\n", err)
		}
	})
	if !strings.Contains(out, "CONFLICT CHECK") {
		t.Errorf("missing CONFLICT CHECK header in output:\n%s", out)
	}
	if !strings.Contains(out, "No conflict") {
		t.Errorf("expected 'No conflict' message, got:\n%s", out)
	}
}

func TestRunDebate_DryRunConflict(t *testing.T) {
	// Stub exitProcess to capture the would-be exit code.
	var exited int
	exitProcess = func(code int) { exited = code }
	defer func() { exitProcess = os.Exit }()

	out := captureStdout2(t, func(w *os.File) {
		old := os.Stdout
		os.Stdout = w
		defer func() { os.Stdout = old }()

		opts := &debateOptions{
			globalOptions: &globalOptions{
				configPath: defaultConfigPath(),
				verbose:    false,
			},
			agentA:    "alice",
			agentB:    "bob",
			verdictA:  "APPROVED",
			verdictB:  "REQUEST_CHANGES",
			dryRun:    true,
			maxRounds: 3,
			timeout:   30 * time.Minute,
		}
		if err := runDebate(opts, "http://localhost:3030/helix/helix/pulls/77"); err != nil {
			fmt.Fprintf(old, "runDebate error: %v\n", err)
		}
	})

	if exited != 10 {
		t.Errorf("expected exit code 10 from dry-run, got %d", exited)
	}
	if !strings.Contains(out, "DRY RUN") {
		t.Errorf("expected DRY RUN in output:\n%s", out)
	}
	if !strings.Contains(out, "Chimera URL:") {
		t.Errorf("expected Chimera URL in output:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// runResolve handler
// ---------------------------------------------------------------------------

func TestRunResolve_PreSetVerdict(t *testing.T) {
	out := captureStdout2(t, func(w *os.File) {
		old := os.Stdout
		os.Stdout = w
		defer func() { os.Stdout = old }()

		opts := &resolveOptions{
			globalOptions: &globalOptions{},
			verdict:       "approved",
			pr:            42,
		}
		if err := runResolve(opts, ""); err != nil {
			fmt.Fprintf(old, "runResolve error: %v\n", err)
		}
	})
	if !strings.Contains(out, "FORCE VERDICT") {
		t.Errorf("expected FORCE VERDICT in output:\n%s", out)
	}
	if !strings.Contains(out, "PR #42") {
		t.Errorf("expected 'PR #42' in output:\n%s", out)
	}
	if !strings.Contains(out, "APPROVED") {
		t.Errorf("expected verdict uppercased to APPROVED:\n%s", out)
	}
}

func TestRunResolve_PositionsFileMissing(t *testing.T) {
	opts := &resolveOptions{
		globalOptions: &globalOptions{},
		positionsFile: "/nonexistent/positions.json",
		pr:            42,
	}
	err := runResolve(opts, "")
	if err == nil {
		t.Fatal("expected error for missing positions file")
	}
	if !strings.Contains(err.Error(), "read positions file") {
		t.Errorf("error should mention 'read positions file', got: %v", err)
	}
}

func TestRunResolve_PositionsFileInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/positions.json"
	writeFile(t, path, `not valid json {`)

	opts := &resolveOptions{
		globalOptions: &globalOptions{},
		positionsFile: path,
		pr:            42,
	}
	err := runResolve(opts, "")
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "parse positions JSON") {
		t.Errorf("error should mention 'parse positions JSON', got: %v", err)
	}
}

func TestRunResolve_PositionsTooFew(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/positions.json"
	writeFile(t, path, `[{"agent":"alice","verdict":"APPROVED","evidence":"lgtm"}]`)

	opts := &resolveOptions{
		globalOptions: &globalOptions{},
		positionsFile: path,
		pr:            42,
	}
	err := runResolve(opts, "")
	if err == nil {
		t.Fatal("expected error for <2 positions")
	}
	if !strings.Contains(err.Error(), "at least 2") {
		t.Errorf("error should mention 'at least 2', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// runResolveWithPositions handler
// ---------------------------------------------------------------------------

func TestRunResolveWithPositions_HappyPath(t *testing.T) {
	// Stub exitProcess (defensive — should not be called on happy path)
	var exited int
	exitProcess = func(code int) { exited = code }
	defer func() { exitProcess = os.Exit }()

	// Redirect HOME to a temp dir so the audit log path resolves to a
	// directory that does NOT pre-exist. This reproduces the CI failure
	// (`/home/runner/.helix/negotiations/` doesn't exist in CI runners)
	// and exercises the auto-MkdirAll fix in runResolveWithPositions.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// Mock Chimera arbiter — returns an APPROVED verdict.
	// Response shape: {status, confidence, summary, trace:{source, duration, total_tokens}}
	chimera := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":     "APPROVED",
			"confidence": 0.85,
			"summary":    "alice's evidence was stronger (covers 4/5 spec requirements)",
			"trace": map[string]any{
				"source":       "arbiter-formation",
				"duration":     1.234,
				"total_tokens": 1234,
			},
		})
	}))
	defer chimera.Close()

	dir := t.TempDir()
	positionsPath := dir + "/positions.json"
	writeFile(t, positionsPath, `[
		{"agent":"alice","verdict":"APPROVED","evidence":"looks good"},
		{"agent":"bob","verdict":"REQUEST_CHANGES","evidence":"needs tests"}
	]`)

	out := captureStdout2(t, func(w *os.File) {
		old := os.Stdout
		os.Stdout = w
		defer func() { os.Stdout = old }()

		opts := &resolveOptions{
			globalOptions: &globalOptions{
				configPath: defaultConfigPath(),
			},
			chimeraURL:    chimera.URL,
			positionsFile: positionsPath,
		}
		if err := runResolveWithPositions(opts, 99); err != nil {
			fmt.Fprintf(old, "runResolveWithPositions error: %v\n", err)
		}
	})
	if exited != 0 {
		t.Errorf("expected no exit on happy path, got code %d", exited)
	}
	if !strings.Contains(out, "PR #99") {
		t.Errorf("expected 'PR #99' in output:\n%s", out)
	}
	if !strings.Contains(out, "alice") || !strings.Contains(out, "bob") {
		t.Errorf("expected both agents in output:\n%s", out)
	}
	if !strings.Contains(out, "NEGOTIATION RESULT") {
		t.Errorf("expected NEGOTIATION RESULT in output:\n%s", out)
	}
}
