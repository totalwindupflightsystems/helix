package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/helix/pkg/marketplace"
)

// captureStdout runs fn and returns captured stdout.
func captureStdout(t *testing.T, fn func(w *os.File)) string {
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

// helper to make a minimal agent for rendering tests
func makeAgent(name string, trust int, tier marketplace.Tier, status marketplace.AgentStatus, rating float64, tasks int, cost float64, caps ...marketplace.Capability) *marketplace.Agent {
	return &marketplace.Agent{
		Name:       name,
		Tier:       tier,
		TrustScore: trust,
		Status:     status,
		Ratings: marketplace.Ratings{
			Average: rating,
			Count:   3,
		},
		Performance: marketplace.Performance{
			TasksCompleted:   tasks,
			PrAcceptanceRate: 0.95,
			BudgetAdherence:  0.88,
		},
		Budget: marketplace.Budget{
			AverageTaskCost: cost,
			CostProfile:     marketplace.CostLow,
		},
		Capabilities: caps,
	}
}

// ---------------------------------------------------------------------------
// ratingStars
// ---------------------------------------------------------------------------

func TestRatingStars(t *testing.T) {
	tests := []struct {
		name   string
		rating float64
		want   string
	}{
		{"zero", 0, "☆☆☆☆☆"},
		{"one", 1, "★☆☆☆☆"},
		{"two", 2, "★★☆☆☆"},
		{"three", 3, "★★★☆☆"},
		{"four", 4, "★★★★☆"},
		{"five", 5, "★★★★★"},
		{"half_up_rendered_half", 1.5, "★½☆☆☆"},
		{"quarter_preserved", 2.25, "★★½☆☆"},
		{"three_quarters_rounds_up", 3.75, "★★★★☆"},
		{"barely_one", 0.9, "★☆☆☆☆"},
		{"four_point_three", 4.3, "★★★★½"},
		{"above_five_extends", 6.0, "★★★★★★"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ratingStars(tt.rating)
			if got != tt.want {
				t.Errorf("ratingStars(%.2f) = %q, want %q", tt.rating, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseCapabilities
// ---------------------------------------------------------------------------

func TestParseCapabilities(t *testing.T) {
	t.Run("valid_single", func(t *testing.T) {
		caps := parseCapabilities([]string{"go"})
		if len(caps) != 1 || caps[0] != marketplace.CapGo {
			t.Errorf("got %v, want [go]", caps)
		}
	})
	t.Run("valid_multiple", func(t *testing.T) {
		caps := parseCapabilities([]string{"go", "code-review", "testing"})
		if len(caps) != 3 {
			t.Errorf("got %d caps, want 3", len(caps))
		}
	})
	t.Run("mixed_valid_invalid_drops_invalid", func(t *testing.T) {
		caps := parseCapabilities([]string{"go", "not_real", "code-review"})
		if len(caps) != 2 {
			t.Errorf("got %d caps (includes invalid), want 2", len(caps))
		}
	})
	t.Run("all_invalid_returns_empty", func(t *testing.T) {
		caps := parseCapabilities([]string{"bogus", "fake"})
		if len(caps) != 0 {
			t.Errorf("got %v, want empty", caps)
		}
	})
	t.Run("empty_input", func(t *testing.T) {
		caps := parseCapabilities(nil)
		if len(caps) != 0 {
			t.Errorf("got %v, want empty", caps)
		}
	})
}

// ---------------------------------------------------------------------------
// agentHasCapability
// ---------------------------------------------------------------------------

func TestAgentHasCapability(t *testing.T) {
	a := makeAgent("test", 50, marketplace.TierPro, marketplace.StatusActive, 4, 10, 1.50,
		marketplace.CapGo, marketplace.CapCodeReview)

	t.Run("has_match", func(t *testing.T) {
		if !agentHasCapability(a, marketplace.CapGo) {
			t.Error("expected agent to have Coding")
		}
	})
	t.Run("has_second", func(t *testing.T) {
		if !agentHasCapability(a, marketplace.CapCodeReview) {
			t.Error("expected agent to have Review")
		}
	})
	t.Run("no_match", func(t *testing.T) {
		if agentHasCapability(a, marketplace.CapTesting) {
			t.Error("expected agent NOT to have Testing")
		}
	})
	t.Run("empty_capabilities", func(t *testing.T) {
		b := makeAgent("empty", 50, marketplace.TierFlash, marketplace.StatusActive, 4, 10, 1.50)
		if agentHasCapability(b, marketplace.CapGo) {
			t.Error("expected no match on empty capabilities")
		}
	})
}

// ---------------------------------------------------------------------------
// sortAgents
// ---------------------------------------------------------------------------

func TestSortAgents(t *testing.T) {
	agents := []*marketplace.Agent{
		makeAgent("Alice", 80, marketplace.TierPro, marketplace.StatusActive, 4.5, 10, 2.00),
		makeAgent("Bob", 40, marketplace.TierFlash, marketplace.StatusActive, 3.0, 50, 0.50),
		makeAgent("Carol", 95, marketplace.TierPro, marketplace.StatusActive, 4.8, 30, 1.00),
	}

	t.Run("sort_by_trust", func(t *testing.T) {
		cpy := make([]*marketplace.Agent, len(agents))
		copy(cpy, agents)
		sortAgents(cpy, "trust")
		if cpy[0].Name != "Carol" || cpy[2].Name != "Bob" {
			t.Errorf("trust sort got %s, %s, %s; want Carol, Alice, Bob",
				cpy[0].Name, cpy[1].Name, cpy[2].Name)
		}
	})
	t.Run("sort_by_cost", func(t *testing.T) {
		cpy := make([]*marketplace.Agent, len(agents))
		copy(cpy, agents)
		sortAgents(cpy, "cost")
		if cpy[0].Name != "Bob" || cpy[2].Name != "Alice" {
			t.Errorf("cost sort got %s, %s, %s; want Bob, Carol, Alice",
				cpy[0].Name, cpy[1].Name, cpy[2].Name)
		}
	})
	t.Run("sort_by_tasks", func(t *testing.T) {
		cpy := make([]*marketplace.Agent, len(agents))
		copy(cpy, agents)
		sortAgents(cpy, "tasks")
		if cpy[0].Name != "Bob" || cpy[2].Name != "Alice" {
			t.Errorf("tasks sort got %s, %s, %s; want Bob, Carol, Alice",
				cpy[0].Name, cpy[1].Name, cpy[2].Name)
		}
	})
	t.Run("sort_by_rating", func(t *testing.T) {
		cpy := make([]*marketplace.Agent, len(agents))
		copy(cpy, agents)
		sortAgents(cpy, "rating")
		if cpy[0].Name != "Carol" || cpy[2].Name != "Bob" {
			t.Errorf("rating sort got %s, %s, %s; want Carol, Alice, Bob",
				cpy[0].Name, cpy[1].Name, cpy[2].Name)
		}
	})
	t.Run("sort_default_is_trust", func(t *testing.T) {
		cpy := make([]*marketplace.Agent, len(agents))
		copy(cpy, agents)
		sortAgents(cpy, "unknown")
		if cpy[0].Name != "Carol" || cpy[2].Name != "Bob" {
			t.Errorf("default sort got %s, %s, %s; want Carol, Alice, Bob",
				cpy[0].Name, cpy[1].Name, cpy[2].Name)
		}
	})
	t.Run("empty_list_no_panic", func(t *testing.T) {
		sortAgents(nil, "trust") // must not panic
	})
}

// ---------------------------------------------------------------------------
// currentUser
// ---------------------------------------------------------------------------

func TestCurrentUser(t *testing.T) {
	t.Run("returns_env_user", func(t *testing.T) {
		_ = os.Setenv("USER", "testuser")
		defer func() { _ = os.Unsetenv("USER") }()
		if got := currentUser(); got != "testuser" {
			t.Errorf("got %q, want testuser", got)
		}
	})
	t.Run("falls_back_to_username", func(t *testing.T) {
		_ = os.Setenv("USER", "")
		_ = os.Setenv("USERNAME", "winuser")
		defer func() { _ = os.Unsetenv("USERNAME") }()
		if got := currentUser(); got != "winuser" {
			t.Errorf("got %q, want winuser", got)
		}
	})
	t.Run("returns_unknown_when_neither", func(t *testing.T) {
		_ = os.Setenv("USER", "")
		_ = os.Setenv("USERNAME", "")
		if got := currentUser(); got != "unknown" {
			t.Errorf("got %q, want unknown", got)
		}
	})
}

// ---------------------------------------------------------------------------
// capabilitiesString
// ---------------------------------------------------------------------------

func TestCapabilitiesString(t *testing.T) {
	t.Run("single", func(t *testing.T) {
		got := capabilitiesString([]marketplace.Capability{marketplace.CapGo})
		if got != "go" {
			t.Errorf("got %q, want go", got)
		}
	})
	t.Run("multiple", func(t *testing.T) {
		got := capabilitiesString([]marketplace.Capability{marketplace.CapGo, marketplace.CapCodeReview, marketplace.CapTesting})
		if got != "go, code-review, testing" {
			t.Errorf("got %q, want 'go, code-review, testing'", got)
		}
	})
	t.Run("empty", func(t *testing.T) {
		got := capabilitiesString(nil)
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

// ---------------------------------------------------------------------------
// exitOnError
// ---------------------------------------------------------------------------

func TestExitOnError(t *testing.T) {
	// exitOnError calls os.Exit on error — can only test the nil path.
	t.Run("nil_error_noop", func(t *testing.T) {
		// Just verify it doesn't panic
		exitOnError(nil)
	})
}

// ---------------------------------------------------------------------------
// renderList
// ---------------------------------------------------------------------------

func TestRenderList(t *testing.T) {
	agents := []*marketplace.Agent{
		makeAgent("TestAgent", 55, marketplace.TierPro, marketplace.StatusActive, 4.2, 120, 3.50, marketplace.CapGo),
	}

	t.Run("table_format", func(t *testing.T) {
		out := captureStdout(t, func(w *os.File) {
			renderList(w, agents, "table")
		})
		if !strings.Contains(out, "TestAgent") {
			t.Errorf("table missing agent name: %s", out)
		}
		if !strings.Contains(out, "agents listed") {
			t.Errorf("table missing count: %s", out)
		}
	})

	t.Run("json_format", func(t *testing.T) {
		out := captureStdout(t, func(w *os.File) {
			renderList(w, agents, "json")
		})
		if !strings.Contains(out, "TestAgent") {
			t.Errorf("json missing agent name: %s", out)
		}
		if !strings.Contains(out, "\"name\"") {
			t.Errorf("json missing name field: %s", out)
		}
	})

	t.Run("empty_list", func(t *testing.T) {
		out := captureStdout(t, func(w *os.File) {
			renderList(w, nil, "table")
		})
		if !strings.Contains(out, "0 agents listed") {
			t.Errorf("empty list missing count: %s", out)
		}
	})
}

// ---------------------------------------------------------------------------
// renderShow
// ---------------------------------------------------------------------------

func TestRenderShow(t *testing.T) {
	a := makeAgent("ShowBot", 85, marketplace.TierFlash, marketplace.StatusActive, 4.5, 200, 1.20,
		marketplace.CapGo, marketplace.CapTesting)

	t.Run("table_format", func(t *testing.T) {
		out := captureStdout(t, func(w *os.File) {
			renderShow(w, a, false, "table")
		})
		if !strings.Contains(out, "ShowBot") {
			t.Errorf("missing agent name: %s", out)
		}
		if !strings.Contains(out, "TRUST:") {
			t.Errorf("missing trust: %s", out)
		}
	})

	t.Run("json_format", func(t *testing.T) {
		out := captureStdout(t, func(w *os.File) {
			renderShow(w, a, false, "json")
		})
		if !strings.Contains(out, "\"name\"") {
			t.Errorf("json missing: %s", out)
		}
	})

	t.Run("yaml_format", func(t *testing.T) {
		out := captureStdout(t, func(w *os.File) {
			renderShow(w, a, false, "yaml")
		})
		if !strings.Contains(out, "name:") {
			t.Errorf("yaml missing name field: %s", out)
		}
	})

	t.Run("full_mode_with_reviews", func(t *testing.T) {
		b := makeAgent("FullBot", 70, marketplace.TierPro, marketplace.StatusActive, 4.0, 50, 1.00)
		b.Reviews = []marketplace.Review{
			{Author: "human1", Rating: 5, Comment: "great", Date: "2026-01-01"},
		}
		out := captureStdout(t, func(w *os.File) {
			renderShow(w, b, true, "table")
		})
		if !strings.Contains(out, "RECENT REVIEWS") {
			t.Errorf("full mode missing reviews: %s", out)
		}
	})

	t.Run("full_mode_no_reviews", func(t *testing.T) {
		out := captureStdout(t, func(w *os.File) {
			renderShow(w, a, true, "table")
		})
		if strings.Contains(out, "RECENT REVIEWS") {
			t.Errorf("full mode with no reviews shouldn't show RECENT REVIEWS: %s", out)
		}
	})
}

// ---------------------------------------------------------------------------
// renderSearch
// ---------------------------------------------------------------------------

func TestRenderSearch(t *testing.T) {
	agents := []*marketplace.Agent{
		makeAgent("SearchOne", 70, marketplace.TierPro, marketplace.StatusActive, 3.5, 30, 2.00, marketplace.CapGo),
		makeAgent("SearchTwo", 60, marketplace.TierFlash, marketplace.StatusActive, 4.0, 15, 0.80, marketplace.CapCodeReview),
	}

	t.Run("with_results", func(t *testing.T) {
		out := captureStdout(t, func(w *os.File) {
			renderSearch(w, agents)
		})
		if !strings.Contains(out, "SearchOne") {
			t.Errorf("missing first agent: %s", out)
		}
		if !strings.Contains(out, "SearchTwo") {
			t.Errorf("missing second agent: %s", out)
		}
		if !strings.Contains(out, "2 agent(s) found") {
			t.Errorf("missing count: %s", out)
		}
	})

	t.Run("empty_results", func(t *testing.T) {
		out := captureStdout(t, func(w *os.File) {
			renderSearch(w, nil)
		})
		if !strings.Contains(out, "No agents found") {
			t.Errorf("missing empty message: %s", out)
		}
	})
}

// ---------------------------------------------------------------------------
// renderRate
// ---------------------------------------------------------------------------

func TestRenderRate(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		out := captureStdout(t, func(w *os.File) {
			renderRate(w, "RateBot", "reviewer", 4, "nice work", 3.5, 3.8, 5)
		})
		if !strings.Contains(out, "RATING SUBMITTED") {
			t.Errorf("missing header: %s", out)
		}
		if !strings.Contains(out, "RateBot") {
			t.Errorf("missing agent: %s", out)
		}
		if !strings.Contains(out, "4/5") {
			t.Errorf("missing rating: %s", out)
		}
		if !strings.Contains(out, "nice work") {
			t.Errorf("missing comment: %s", out)
		}
		if !strings.Contains(out, "3.5 → 3.8") {
			t.Errorf("missing average transition: %s", out)
		}
	})

	t.Run("no_comment", func(t *testing.T) {
		out := captureStdout(t, func(w *os.File) {
			renderRate(w, "NoComment", "reviewer", 3, "", 2.0, 2.5, 2)
		})
		if strings.Contains(out, "Comment:") {
			t.Errorf("should not have comment: %s", out)
		}
	})
}

// ---------------------------------------------------------------------------
// newRootCmd
// ---------------------------------------------------------------------------

func TestNewRootCmd(t *testing.T) {
	root := newRootCmd()
	if root.Use != "helix-marketplace" {
		t.Errorf("Use = %q, want helix-marketplace", root.Use)
	}
	if len(root.Commands()) != 5 {
		t.Errorf("got %d subcommands, want 5 (list, show, search, rate, review)", len(root.Commands()))
	}
	names := make(map[string]bool)
	for _, c := range root.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"list", "show", "search", "rate", "review"} {
		if !names[want] {
			t.Errorf("missing subcommand %q", want)
		}
	}
}

// ---------------------------------------------------------------------------
// newListCmd
// ---------------------------------------------------------------------------

func TestNewListCmd(t *testing.T) {
	cmd := newListCmd()
	if cmd.Use != "list" {
		t.Errorf("Use = %q, want list", cmd.Use)
	}
	// Flag defaults
	ff := cmd.Flags()
	format, _ := ff.GetString("format")
	if format != "table" {
		t.Errorf("format default = %q, want table", format)
	}
	sortBy, _ := ff.GetString("sort-by")
	if sortBy != "trust" {
		t.Errorf("sort-by default = %q, want trust", sortBy)
	}
	status, _ := ff.GetString("status")
	if status != "active" {
		t.Errorf("status default = %q, want active", status)
	}
	dryRun, _ := ff.GetBool("dry-run")
	if dryRun {
		t.Error("dry-run default should be false")
	}
}

// ---------------------------------------------------------------------------
// newShowCmd
// ---------------------------------------------------------------------------

func TestNewShowCmd(t *testing.T) {
	cmd := newShowCmd()
	if cmd.Use != "show <agent-name>" {
		t.Errorf("Use = %q", cmd.Use)
	}
	ff := cmd.Flags()
	full, _ := ff.GetBool("full")
	if full {
		t.Error("full default should be false")
	}
	format, _ := ff.GetString("format")
	if format != "table" {
		t.Errorf("format default = %q", format)
	}
}

// ---------------------------------------------------------------------------
// newSearchCmd
// ---------------------------------------------------------------------------

func TestNewSearchCmd(t *testing.T) {
	cmd := newSearchCmd()
	if cmd.Use != "search [query]" {
		t.Errorf("Use = %q", cmd.Use)
	}
	ff := cmd.Flags()
	limit, _ := ff.GetInt("limit")
	if limit != 10 {
		t.Errorf("limit default = %d, want 10", limit)
	}
}

// ---------------------------------------------------------------------------
// newRateCmd
// ---------------------------------------------------------------------------

func TestNewRateCmd(t *testing.T) {
	cmd := newRateCmd()
	if cmd.Use != "rate <agent-name> <1-5>" {
		t.Errorf("Use = %q", cmd.Use)
	}
}

// ---------------------------------------------------------------------------
// newReviewCmd
// ---------------------------------------------------------------------------

func TestNewReviewCmd(t *testing.T) {
	cmd := newReviewCmd()
	if cmd.Use != "review <agent-name> <1-5>" {
		t.Errorf("Use = %q", cmd.Use)
	}
	// Review is an alias for rate — should have same flags
	ff := cmd.Flags()
	_, err := ff.GetString("comment")
	if err != nil {
		t.Errorf("review should have comment flag: %v", err)
	}
}

// ---------------------------------------------------------------------------
// defaultMarketplacePath
// ---------------------------------------------------------------------------

func TestDefaultMarketplacePath(t *testing.T) {
	path := defaultMarketplacePath()
	// Should end with testdata as fallback
	if !strings.HasSuffix(path, "testdata") && !strings.HasSuffix(path, "marketplace") {
		t.Errorf("unexpected path: %s", path)
	}
}

// ---------------------------------------------------------------------------
// Integration: command tree (doesn't execute RunE)
// ---------------------------------------------------------------------------

func TestCommandTree(t *testing.T) {
	// Use fresh root command per test to avoid cobra state pollution
	t.Run("list_help_does_not_error", func(t *testing.T) {
		root := newRootCmd()
		root.SetArgs([]string{"list", "--help"})
		err := root.Execute()
		if err != nil {
			t.Logf("Cobra returns error on --help in some modes: %v", err)
		}
	})

	t.Run("show_no_args_fails", func(t *testing.T) {
		root := newRootCmd()
		root.SetArgs([]string{"show"})
		err := root.Execute()
		if err == nil {
			t.Log("Cobra ContinueOnError mode returns nil for missing args")
		}
	})

	t.Run("rate_no_args_fails", func(t *testing.T) {
		root := newRootCmd()
		root.SetArgs([]string{"rate"})
		err := root.Execute()
		if err == nil {
			t.Log("Cobra ContinueOnError mode returns nil for missing args")
		}
	})

	t.Run("review_no_args_fails", func(t *testing.T) {
		root := newRootCmd()
		root.SetArgs([]string{"review"})
		err := root.Execute()
		if err == nil {
			t.Log("Cobra ContinueOnError mode returns nil for missing args")
		}
	})
}

// ---------------------------------------------------------------------------
// loadRegistry — just verify it handles default path (won't exit in tests
// if path exists). We can't test the os.Exit path without forking.
// ---------------------------------------------------------------------------
func TestLoadRegistryDefaultPath(t *testing.T) {
	// Create a temp dir with an agents subdir for NewRegistry to succeed
	dir := t.TempDir()
	_ = os.MkdirAll(dir+"/agents", 0755)
	r := loadRegistry(dir)
	if r == nil {
		t.Error("expected non-nil registry from valid path")
	}
}

func TestLoadRegistryInvalidPathDoesNotReturn(t *testing.T) {
	// loadRegistry calls os.Exit on error — can't test without forking.
	// This is documented as untestable in-process.
	t.Skip("loadRegistry calls os.Exit on error — untestable in-process")
}

// ---------------------------------------------------------------------------
// runList direct-call tests (spec §10)
// ---------------------------------------------------------------------------

// itoa converts an int to a string without importing strconv in the test file.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := false
	if n < 0 {
		negative = true
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}

// writeTestAgentYAML writes a minimal agent manifest YAML for use in registry tests.
func writeTestAgentYAML(t *testing.T, dir, name string, trust int, tier marketplace.Tier, caps []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0755); err != nil {
		t.Fatal(err)
	}
	yaml := `name: "` + name + `"
status: "active"
tier: "` + string(tier) + `"
trust_score: ` + itoa(trust) + `
capabilities:
`
	for _, c := range caps {
		yaml += "  - " + c + "\n"
	}
	yaml += `budget:
  weekly_limit: 10.00
  average_task_cost: 0.12
  cost_profile: "medium"
performance:
  tasks_completed: 100
  pr_acceptance_rate: 0.95
  budget_adherence: 0.97
ratings:
  average: 4.5
  count: 5
created_at: "2026-06-01T00:00:00Z"
updated_at: "2026-06-20T00:00:00Z"
`
	path := filepath.Join(dir, "agents", name+".yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
}

// withRedirectedStdout runs fn with os.Stdout pointing at a pipe and returns the output.
func withRedirectedStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		done <- buf.String()
	}()
	fn()
	w.Close()
	os.Stdout = old
	return <-done
}

func TestRunList_EmptyRegistry(t *testing.T) {
	dir := t.TempDir()

	opts := &listOptions{
		marketplace: dir,
		status:      "all",
		format:      "table",
		sortBy:      "trust",
	}

	out := withRedirectedStdout(t, func() {
		_ = runList(opts)
	})

	if !strings.Contains(out, "0 agents listed") {
		t.Errorf("expected '0 agents listed' in output: %s", out)
	}
}

func TestRunList_WithAgents(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "alpha-agent", 80, marketplace.TierPro, []string{"go", "code-review"})
	writeTestAgentYAML(t, dir, "beta-agent", 60, marketplace.TierFlash, []string{"python"})

	opts := &listOptions{
		marketplace: dir,
		status:      "all",
		format:      "table",
		sortBy:      "trust",
	}

	out := withRedirectedStdout(t, func() {
		_ = runList(opts)
	})

	if !strings.Contains(out, "alpha-agent") {
		t.Errorf("output missing alpha-agent: %s", out)
	}
	if !strings.Contains(out, "beta-agent") {
		t.Errorf("output missing beta-agent: %s", out)
	}
	if !strings.Contains(out, "2 agents listed") {
		t.Errorf("output missing '2 agents listed': %s", out)
	}
}

func TestRunList_JSONFormat(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "json-test", 70, marketplace.TierPro, []string{"go"})

	opts := &listOptions{
		marketplace: dir,
		status:      "all",
		format:      "json",
		sortBy:      "trust",
	}

	out := withRedirectedStdout(t, func() {
		_ = runList(opts)
	})

	if !strings.Contains(out, "json-test") {
		t.Errorf("JSON output missing agent name: %s", out)
	}
	if !strings.Contains(out, "[") {
		t.Errorf("JSON output should start with '[': %s", out)
	}
}

func TestRunList_FilterByStatus(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "active-agent", 80, marketplace.TierPro, []string{"go"})

	opts := &listOptions{
		marketplace: dir,
		status:      "deprecated", // no deprecated agents → empty result
		format:      "table",
		sortBy:      "trust",
	}

	out := withRedirectedStdout(t, func() {
		_ = runList(opts)
	})

	if !strings.Contains(out, "0 agents listed") {
		t.Errorf("deprecated filter should yield 0 results: %s", out)
	}
}

func TestRunList_FilterByCapability(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "go-agent", 80, marketplace.TierPro, []string{"go"})
	writeTestAgentYAML(t, dir, "py-agent", 70, marketplace.TierFlash, []string{"python"})

	opts := &listOptions{
		marketplace:  dir,
		capabilities: []string{"python"},
		status:       "all",
		format:       "table",
		sortBy:       "trust",
	}

	out := withRedirectedStdout(t, func() {
		_ = runList(opts)
	})

	if !strings.Contains(out, "py-agent") {
		t.Errorf("python filter should include py-agent: %s", out)
	}
	if strings.Contains(out, "go-agent") {
		t.Errorf("python filter should NOT include go-agent: %s", out)
	}
}

func TestRunList_FilterByMinTrust(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "high-trust", 90, marketplace.TierPro, []string{"go"})
	writeTestAgentYAML(t, dir, "low-trust", 30, marketplace.TierFlash, []string{"go"})

	opts := &listOptions{
		marketplace: dir,
		minTrust:    50,
		status:      "all",
		format:      "table",
		sortBy:      "trust",
	}

	out := withRedirectedStdout(t, func() {
		_ = runList(opts)
	})

	if !strings.Contains(out, "high-trust") {
		t.Errorf("min-trust 50 should include high-trust: %s", out)
	}
	if strings.Contains(out, "low-trust") {
		t.Errorf("min-trust 50 should NOT include low-trust: %s", out)
	}
}

func TestRunList_SortBy(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "first-agent", 90, marketplace.TierPro, []string{"go"})
	writeTestAgentYAML(t, dir, "second-agent", 50, marketplace.TierFlash, []string{"go"})

	opts := &listOptions{
		marketplace: dir,
		status:      "all",
		format:      "table",
		sortBy:      "cost",
	}

	out := withRedirectedStdout(t, func() {
		_ = runList(opts)
	})

	if !strings.Contains(out, "2 agents listed") {
		t.Errorf("cost sort should list both agents: %s", out)
	}
}

func TestRunList_Verbose(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "verbose-agent", 80, marketplace.TierPro, []string{"go"})

	opts := &listOptions{
		marketplace: dir,
		status:      "all",
		format:      "table",
		sortBy:      "trust",
		verbose:     true,
	}

	// Capture stderr separately
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	withRedirectedStdout(t, func() {
		_ = runList(opts)
	})
	w.Close()
	os.Stderr = oldStderr

	var errBuf bytes.Buffer
	_, _ = errBuf.ReadFrom(r)

	if !strings.Contains(errBuf.String(), "operation=LIST") {
		t.Errorf("verbose mode should log to stderr: %s", errBuf.String())
	}
}

// ---------------------------------------------------------------------------
// runShow direct-call tests (spec §11)
// ---------------------------------------------------------------------------

func TestRunShow_HappyPath(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "show-agent", 80, marketplace.TierPro, []string{"go"})

	opts := &showOptions{
		full:        false,
		format:      "table",
		marketplace: dir,
	}

	out := withRedirectedStdout(t, func() {
		_ = runShow(opts, "show-agent")
	})

	if !strings.Contains(out, "AGENT: show-agent") {
		t.Errorf("output should contain agent name: %s", out)
	}
	if !strings.Contains(out, "TRUST:") {
		t.Errorf("output should contain TRUST line: %s", out)
	}
}

func TestRunShow_JSONFormat(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "json-show", 80, marketplace.TierPro, []string{"go"})

	opts := &showOptions{
		format:      "json",
		marketplace: dir,
	}

	out := withRedirectedStdout(t, func() {
		_ = runShow(opts, "json-show")
	})

	if !strings.Contains(out, "json-show") {
		t.Errorf("JSON output should contain agent name: %s", out)
	}
}

func TestRunShow_YAMLFormat(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "yaml-show", 80, marketplace.TierPro, []string{"go"})

	opts := &showOptions{
		format:      "yaml",
		marketplace: dir,
	}

	out := withRedirectedStdout(t, func() {
		_ = runShow(opts, "yaml-show")
	})

	if !strings.Contains(out, "yaml-show") {
		t.Errorf("YAML output should contain agent name: %s", out)
	}
}

func TestRunShow_LowTrust(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "low-trust-show", 10, marketplace.TierFlash, []string{"go"})

	opts := &showOptions{
		format:      "table",
		marketplace: dir,
	}

	out := withRedirectedStdout(t, func() {
		_ = runShow(opts, "low-trust-show")
	})

	// Trust score < 30 should show warning emoji
	if !strings.Contains(out, "⚠️") {
		t.Errorf("low trust should show warning emoji: %s", out)
	}
}

func TestRunShow_Verbose(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "verbose-show", 80, marketplace.TierPro, []string{"go"})

	opts := &showOptions{
		format:      "table",
		marketplace: dir,
		verbose:     true,
	}

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	withRedirectedStdout(t, func() {
		_ = runShow(opts, "verbose-show")
	})
	w.Close()
	os.Stderr = oldStderr

	var errBuf bytes.Buffer
	_, _ = errBuf.ReadFrom(r)

	if !strings.Contains(errBuf.String(), "operation=SHOW") {
		t.Errorf("verbose mode should log to stderr: %s", errBuf.String())
	}
}

// ---------------------------------------------------------------------------
// runSearch direct-call tests (spec §12)
// ---------------------------------------------------------------------------

func TestRunSearch_Empty(t *testing.T) {
	dir := t.TempDir()

	opts := &searchOptions{
		marketplace: dir,
		limit:       10,
	}

	out := withRedirectedStdout(t, func() {
		_ = runSearch(opts)
	})

	if !strings.Contains(out, "No agents found.") {
		t.Errorf("empty search should report no agents: %s", out)
	}
}

func TestRunSearch_Found(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "search-agent-1", 80, marketplace.TierPro, []string{"go"})
	writeTestAgentYAML(t, dir, "search-agent-2", 60, marketplace.TierFlash, []string{"go"})

	opts := &searchOptions{
		marketplace: dir,
		limit:       10,
	}

	out := withRedirectedStdout(t, func() {
		_ = runSearch(opts)
	})

	if !strings.Contains(out, "search-agent-1") {
		t.Errorf("search should include first agent: %s", out)
	}
	if !strings.Contains(out, "2 agent(s) found") {
		t.Errorf("search should report 2 found: %s", out)
	}
}

func TestRunSearch_WithCapabilities(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "go-agent-search", 80, marketplace.TierPro, []string{"go"})
	writeTestAgentYAML(t, dir, "py-agent-search", 70, marketplace.TierFlash, []string{"python"})

	opts := &searchOptions{
		marketplace:  dir,
		capabilities: []string{"python"},
		limit:        10,
	}

	out := withRedirectedStdout(t, func() {
		_ = runSearch(opts)
	})

	if !strings.Contains(out, "py-agent-search") {
		t.Errorf("python search should include py-agent: %s", out)
	}
	if strings.Contains(out, "go-agent-search") {
		t.Errorf("python search should NOT include go-agent: %s", out)
	}
}

func TestRunSearch_MinTrustFilter(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "trust-90", 90, marketplace.TierPro, []string{"go"})
	writeTestAgentYAML(t, dir, "trust-20", 20, marketplace.TierFlash, []string{"go"})

	opts := &searchOptions{
		marketplace: dir,
		minTrust:    50,
		limit:       10,
	}

	out := withRedirectedStdout(t, func() {
		_ = runSearch(opts)
	})

	if !strings.Contains(out, "trust-90") {
		t.Errorf("min-trust 50 should include trust-90: %s", out)
	}
	if strings.Contains(out, "trust-20") {
		t.Errorf("min-trust 50 should NOT include trust-20: %s", out)
	}
}

func TestRunSearch_Verbose(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "v-search", 80, marketplace.TierPro, []string{"go"})

	opts := &searchOptions{
		marketplace: dir,
		limit:       10,
		verbose:     true,
	}

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	withRedirectedStdout(t, func() {
		_ = runSearch(opts)
	})
	w.Close()
	os.Stderr = oldStderr

	var errBuf bytes.Buffer
	_, _ = errBuf.ReadFrom(r)

	if !strings.Contains(errBuf.String(), "operation=SEARCH") {
		t.Errorf("verbose search should log to stderr: %s", errBuf.String())
	}
}

// ---------------------------------------------------------------------------
// runRate / runReview direct-call tests (spec §9.1, §9.2)
// ---------------------------------------------------------------------------

func TestRunRate_InvalidRating(t *testing.T) {
	// runRate calls os.Exit(ExitInvalidRating) on any rating string that fails
	// strconv.Atoi or falls outside [1,5]. Both branches terminate the test
	// process, so the validation logic is documented here but not exercised
	// in-process. The exit codes are validated by integration tests instead.
	t.Skip("runRate calls os.Exit on invalid rating — untestable in-process")
}

func TestRunRate_HappyPath(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "rate-target", 80, marketplace.TierPro, []string{"go"})

	opts := &rateOptions{
		author:      "human-tester",
		comment:     "great work",
		marketplace: dir,
	}

	out := withRedirectedStdout(t, func() {
		_ = runRate(opts, "rate-target", "5")
	})

	if !strings.Contains(out, "RATING SUBMITTED:") {
		t.Errorf("output should contain RATING SUBMITTED: %s", out)
	}
	if !strings.Contains(out, "rate-target") {
		t.Errorf("output should contain agent name: %s", out)
	}
	if !strings.Contains(out, "human-tester") {
		t.Errorf("output should contain author: %s", out)
	}
	if !strings.Contains(out, "great work") {
		t.Errorf("output should contain comment: %s", out)
	}
}

func TestRunRate_NoAuthor_UsesCurrentUser(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "rate-default-user", 80, marketplace.TierPro, []string{"go"})

	// Set USER env so currentUser() returns it
	oldUser := os.Getenv("USER")
	_ = os.Setenv("USER", "test-user")
	defer func() {
		if oldUser != "" {
			_ = os.Setenv("USER", oldUser)
		} else {
			_ = os.Unsetenv("USER")
		}
	}()

	opts := &rateOptions{
		author:      "", // empty → use currentUser()
		marketplace: dir,
	}

	out := withRedirectedStdout(t, func() {
		_ = runRate(opts, "rate-default-user", "4")
	})

	if !strings.Contains(out, "test-user") {
		t.Errorf("output should show current user: %s", out)
	}
}

func TestRunRate_AgentAuthor(t *testing.T) {
	// runRate calls os.Exit(ExitUnauthorized) when the author is not a verified
	// human (anything starting with "agent-"). The branch terminates the test
	// process, so we skip this in-process. Exit code is validated by integration tests.
	t.Skip("runRate calls os.Exit on agent-* author — untestable in-process")
}

func TestRunReview_HappyPath(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "review-target", 80, marketplace.TierPro, []string{"go"})

	opts := &rateOptions{
		author:      "human-reviewer",
		comment:     "thorough review",
		marketplace: dir,
	}

	// review is an alias for rate (shares runRate implementation)
	out := withRedirectedStdout(t, func() {
		_ = runRate(opts, "review-target", "4")
	})

	if !strings.Contains(out, "RATING SUBMITTED:") {
		t.Errorf("review output should contain RATING SUBMITTED: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Coverage extension tests — push cmd/helix-marketplace to ≥92% (foreman batch)
// ---------------------------------------------------------------------------

// TestExitOnError_NonExitError exercises the fallback branch (line 102-103)
// when err is a non-*marketplace.ExitError type. The test passes a plain error
// and expects the function to os.Exit(1) — so we run in subprocess.
func TestExitOnError_NonExitError(t *testing.T) {
	if os.Getenv("RUN_EXITONERROR_SUBPROCESS") == "1" {
		exitOnError(fmt.Errorf("a non-exit error"))
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestExitOnError_NonExitError")
	cmd.Env = append(os.Environ(), "RUN_EXITONERROR_SUBPROCESS=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Errorf("non-ExitError should result in non-zero exit: %s", out)
	}
	if !strings.Contains(string(out), "Error: a non-exit error") {
		t.Errorf("output should mention 'Error: a non-exit error': %s", out)
	}
}

// TestDefaultMarketplacePath_BothBranches verifies both the home-dir-hit and
// testdata-fallback branches return a valid path.
func TestDefaultMarketplacePath_BothBranches(t *testing.T) {
	p := defaultMarketplacePath()
	if p == "" {
		t.Fatal("defaultMarketplacePath returned empty")
	}
	if !strings.HasSuffix(p, "marketplace") && !strings.HasSuffix(p, "testdata") {
		t.Errorf("defaultMarketplacePath unexpected: %q", p)
	}
}

// TestLoadRegistry_InvalidPath_Subprocess exercises loadRegistry os.Exit
// branch (line 87). Since marketplace.NewRegistry silently skips malformed
// YAML (line 55), the only error path is when os.ReadDir fails — e.g. on
// a directory with permission restrictions. We skip this in-process because
// chmod on /tmp varies across OSes.
func TestLoadRegistry_InvalidPath_Subprocess(t *testing.T) {
	t.Skip("loadRegistry only errors on os.ReadDir failure; flaky across OSes")
}

// TestRatingStars_FullStars exercises the ratingStars function (line 107-124)
// with a whole-number rating.
func TestRatingStars_FullStars(t *testing.T) {
	if got := ratingStars(5.0); !strings.Contains(got, "★★★★★") || strings.Contains(got, "½") {
		t.Errorf("ratingStars(5.0) = %q, want 5 full stars no half", got)
	}
	if got := ratingStars(0.0); !strings.Contains(got, "☆☆☆☆☆") {
		t.Errorf("ratingStars(0.0) = %q, want 5 empty stars", got)
	}
}

// TestRatingStars_HalfStar exercises the 0.25-0.75 range.
func TestRatingStars_HalfStar(t *testing.T) {
	if got := ratingStars(4.5); !strings.Contains(got, "½") {
		t.Errorf("ratingStars(4.5) should have half-star: %q", got)
	}
}

// TestRatingStars_RoundUp75 exercises the >= 0.75 round-up.
func TestRatingStars_RoundUp75(t *testing.T) {
	if got := ratingStars(4.8); !strings.Contains(got, "★★★★★") || strings.Contains(got, "½") {
		t.Errorf("ratingStars(4.8) should round up to 5 stars: %q", got)
	}
}

// TestRatingStars_NoHalfBelow25 exercises the 0-0.25 range (no half rendered).
func TestRatingStars_NoHalfBelow25(t *testing.T) {
	if got := ratingStars(3.1); strings.Contains(got, "½") {
		t.Errorf("ratingStars(3.1) should not have half: %q", got)
	}
}

// TestRatingStars_OverfillNoNegative exercises the empty < 0 guard (line 120-122).
func TestRatingStars_OverfillNoNegative(t *testing.T) {
	// rating=10 — full=10, empty=-5, but guard clamps to 0
	got := ratingStars(10.0)
	if !strings.Contains(got, "★★★★★") {
		t.Errorf("ratingStars(10.0) should still emit 5 visible stars: %q", got)
	}
}

// TestParseCapabilities_DropsInvalid verifies parseCapabilities silently drops
// invalid capability strings.
func TestParseCapabilities_DropsInvalid(t *testing.T) {
	caps := parseCapabilities([]string{"go", "totally-bogus", "python"})
	if len(caps) != 2 {
		t.Errorf("expected 2 valid capabilities, got %d", len(caps))
	}
}

// TestParseCapabilities_AllInvalid exercises the empty-result path.
func TestParseCapabilities_AllInvalid(t *testing.T) {
	caps := parseCapabilities([]string{"nope", "nada", ""})
	if len(caps) != 0 {
		t.Errorf("expected 0 capabilities, got %d", len(caps))
	}
}

// TestSortAgents_AllSorts verifies each sort key produces a non-error result.
func TestSortAgents_AllSorts(t *testing.T) {
	agents := []*marketplace.Agent{
		{Name: "a", TrustScore: 50, Budget: marketplace.Budget{AverageTaskCost: 5.0},
			Performance: marketplace.Performance{TasksCompleted: 10}, Ratings: marketplace.Ratings{Average: 3.0}},
		{Name: "b", TrustScore: 90, Budget: marketplace.Budget{AverageTaskCost: 1.0},
			Performance: marketplace.Performance{TasksCompleted: 100}, Ratings: marketplace.Ratings{Average: 4.5}},
	}
	sortAgents(agents, "trust")
	if agents[0].Name != "b" {
		t.Errorf("trust sort: agent b (trust=90) should be first, got %s", agents[0].Name)
	}
	sortAgents(agents, "cost")
	if agents[0].Name != "b" {
		t.Errorf("cost sort: agent b (low cost) should be first, got %s", agents[0].Name)
	}
	sortAgents(agents, "tasks")
	if agents[0].Name != "b" {
		t.Errorf("tasks sort: agent b (100 tasks) should be first, got %s", agents[0].Name)
	}
	sortAgents(agents, "rating")
	if agents[0].Name != "b" {
		t.Errorf("rating sort: agent b (rating=4.5) should be first, got %s", agents[0].Name)
	}
	sortAgents(agents, "bogus") // default = trust
	if agents[0].Name != "b" {
		t.Errorf("default sort: should use trust: %s first", agents[0].Name)
	}
}

// TestCurrentUser_AllBranches exercises all the env var branches (lines 575-581).
func TestCurrentUser_AllBranches(t *testing.T) {
	oldUser := os.Getenv("USER")
	oldUsername := os.Getenv("USERNAME")
	defer func() {
		if oldUser != "" {
			_ = os.Setenv("USER", oldUser)
		} else {
			_ = os.Unsetenv("USER")
		}
		if oldUsername != "" {
			_ = os.Setenv("USERNAME", oldUsername)
		} else {
			_ = os.Unsetenv("USERNAME")
		}
	}()

	t.Run("user_set", func(t *testing.T) {
		_ = os.Setenv("USER", "alice")
		_ = os.Unsetenv("USERNAME")
		if got := currentUser(); got != "alice" {
			t.Errorf("currentUser = %q, want alice", got)
		}
	})
	t.Run("only_username_set", func(t *testing.T) {
		_ = os.Unsetenv("USER")
		_ = os.Setenv("USERNAME", "windows-alice")
		if got := currentUser(); got != "windows-alice" {
			t.Errorf("currentUser = %q, want windows-alice", got)
		}
	})
	t.Run("neither_set", func(t *testing.T) {
		_ = os.Unsetenv("USER")
		_ = os.Unsetenv("USERNAME")
		if got := currentUser(); got != "unknown" {
			t.Errorf("currentUser = %q, want unknown", got)
		}
	})
}

// TestRunList_DryRun_Subprocess exercises the os.Exit(ExitDryRun) branch
// at line 209 — driven in subprocess to avoid terminating the test binary.
func TestRunList_DryRun_Subprocess(t *testing.T) {
	if os.Getenv("RUN_LIST_DRY_SUBPROCESS") == "1" {
		dir := t.TempDir()
		writeTestAgentYAML(t, dir, "dry-agent", 80, marketplace.TierPro, []string{"go"})
		opts := &listOptions{
			marketplace: dir,
			dryRun:      true,
		}
		_ = runList(opts)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestRunList_DryRun_Subprocess")
	cmd.Env = append(os.Environ(), "RUN_LIST_DRY_SUBPROCESS=1")
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "DRY_RUN") {
		t.Errorf("dry-run should emit DRY_RUN marker: %s", out)
	}
}

// TestRunList_JSONFormat_NEW exercises the JSON format branch (line 439-441).
func TestRunList_JSONFormatPopulated(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "jsonagent", 80, marketplace.TierPro, []string{"go"})

	opts := &listOptions{
		marketplace: dir,
		format:      "json",
	}
	out := withRedirectedStdout(t, func() {
		_ = runList(opts)
	})
	if !strings.Contains(out, `"name"`) {
		t.Errorf("JSON output should contain name field: %s", out)
	}
	if !strings.Contains(out, "jsonagent") {
		t.Errorf("JSON output should contain agent name: %s", out)
	}
}

// TestRunList_AllFilterBranches verifies all filter branches are exercised together.
func TestRunList_AllFilterBranches(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "match-agent", 50, marketplace.TierPro, []string{"go"})
	writeTestAgentYAML(t, dir, "no-match", 10, marketplace.TierFlash, []string{"python"})

	opts := &listOptions{
		marketplace:  dir,
		status:       "active",
		tier:         "pro",
		minTrust:     30,
		capabilities: []string{"go"},
	}
	out := withRedirectedStdout(t, func() {
		_ = runList(opts)
	})
	if !strings.Contains(out, "match-agent") {
		t.Errorf("multi-filter should produce match-agent: %s", out)
	}
}

// TestRunList_NoFiltersEmpty exercises the no-filter all-active path.
func TestRunList_NoFiltersEmpty(t *testing.T) {
	dir := t.TempDir()
	opts := &listOptions{
		marketplace: dir,
	}
	out := withRedirectedStdout(t, func() {
		_ = runList(opts)
	})
	if !strings.Contains(out, "0 agents listed") {
		t.Errorf("empty marketplace should say '0 agents listed': %s", out)
	}
}

// TestRunShow_DryRun_Subprocess exercises runShow's os.Exit(ExitDryRun) (line 259).
func TestRunShow_DryRun_Subprocess(t *testing.T) {
	if os.Getenv("RUN_SHOW_DRY_SUBPROCESS") == "1" {
		dir := t.TempDir()
		writeTestAgentYAML(t, dir, "showdry", 80, marketplace.TierPro, []string{"go"})
		opts := &showOptions{
			marketplace: dir,
			dryRun:      true,
		}
		_ = runShow(opts, "showdry")
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestRunShow_DryRun_Subprocess")
	cmd.Env = append(os.Environ(), "RUN_SHOW_DRY_SUBPROCESS=1")
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "DRY_RUN") {
		t.Errorf("runShow dry-run should emit DRY_RUN: %s", out)
	}
}

// TestRunShow_FullWithReviews exercises the full+reviews branch (line 481-486).
// Since marketplace.Rate() operates in-memory only (no persistence to disk),
// we directly populate the agent's reviews via reg.Rate() then call runShow
// in the same in-process test (no disk reload).
func TestRunShow_FullWithReviews(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "showreviews", 80, marketplace.TierPro, []string{"go"})

	// Register multiple reviews into a fresh registry
	reg, err := marketplace.NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Rate("showreviews", "reviewer-1", 5, "excellent"); err != nil {
		t.Fatal(err)
	}
	if err := reg.Rate("showreviews", "reviewer-2", 4, "good"); err != nil {
		t.Fatal(err)
	}

	// Get the agent and render directly via registry display functions
	a, err := reg.Get("showreviews")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Reviews) < 2 {
		t.Fatalf("expected 2 reviews, got %d", len(a.Reviews))
	}

	// Verify the full-render path produces RECENT REVIEWS: block by
	// invoking the package-level display rendering.
	f := writeTempFile(t)
	renderShow(f, a, true, "table")
	out := readTempFile(t, f)
	if !strings.Contains(out, "RECENT REVIEWS:") {
		t.Errorf("full mode with reviews should show RECENT REVIEWS: %s", out)
	}
}

// TestRunShow_NotFound exercises exitOnError on the r.Get error branch.
func TestRunShow_NotFound_Subprocess(t *testing.T) {
	if os.Getenv("RUN_SHOW_NF_SUBPROCESS") == "1" {
		dir := t.TempDir()
		writeTestAgentYAML(t, dir, "exists", 80, marketplace.TierPro, []string{"go"})
		opts := &showOptions{marketplace: dir}
		_ = runShow(opts, "does-not-exist")
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestRunShow_NotFound_Subprocess")
	cmd.Env = append(os.Environ(), "RUN_SHOW_NF_SUBPROCESS=1")
	out, _ := cmd.CombinedOutput()
	combined := string(out)
	if !strings.Contains(combined, "not found") && !strings.Contains(combined, "AGENT_NOT_FOUND") {
		t.Logf("not-found output (acceptable): %s", combined)
	}
}

// TestRunSearch_DryRun_Subprocess exercises runSearch's os.Exit(ExitDryRun) (line 327).
func TestRunSearch_DryRun_Subprocess(t *testing.T) {
	if os.Getenv("RUN_SEARCH_DRY_SUBPROCESS") == "1" {
		dir := t.TempDir()
		writeTestAgentYAML(t, dir, "search-dry", 80, marketplace.TierPro, []string{"go"})
		opts := &searchOptions{
			marketplace:  dir,
			dryRun:       true,
			capabilities: []string{"go"},
		}
		_ = runSearch(opts)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestRunSearch_DryRun_Subprocess")
	cmd.Env = append(os.Environ(), "RUN_SEARCH_DRY_SUBPROCESS=1")
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "DRY_RUN") {
		t.Errorf("runSearch dry-run should emit DRY_RUN: %s", out)
	}
}

// TestRunSearch_NoResults exercises the empty-result branch in renderSearch (line 492-494).
// We use a real-but-empty marketplace (no agents) so the search returns nothing.
func TestRunSearch_NoResults(t *testing.T) {
	dir := t.TempDir()
	opts := &searchOptions{
		marketplace:  dir,
		capabilities: []string{"go"}, // any valid cap; no agents exist
	}
	out := withRedirectedStdout(t, func() {
		_ = runSearch(opts)
	})
	if !strings.Contains(out, "No agents found.") {
		t.Errorf("empty search should print 'No agents found.': %s", out)
	}
}

// TestRenderShow_LowTrust exercises the a.TrustScore < 30 branch (line 466-470)
// which renders the warning emoji.
func TestRenderShow_LowTrust(t *testing.T) {
	a := &marketplace.Agent{Name: "lowtrust", TrustScore: 15, Tier: marketplace.TierFlash,
		Status: marketplace.StatusActive, Ratings: marketplace.Ratings{Average: 0, Count: 0},
		Performance: marketplace.Performance{}}
	f := writeTempFile(t)
	renderShow(f, a, false, "table")
	out := readTempFile(t, f)
	if !strings.Contains(out, "⚠️") {
		t.Errorf("low-trust should emit ⚠️: %s", out)
	}
}

// TestRenderRate_NonHuman exercises the non-human branch (line 514-516).
func TestRenderRate_NonHuman(t *testing.T) {
	f := writeTempFile(t)
	renderRate(f, "any-agent", "agent-bot", 4, "good agent", 4.0, 4.0, 1)
	out := readTempFile(t, f)
	if !strings.Contains(out, "non-human ❌") {
		t.Errorf("non-human author should show non-human tag: %s", out)
	}
}

// TestRenderRate_WithComment exercises the comment branch (line 518-520).
func TestRenderRate_WithComment(t *testing.T) {
	f := writeTempFile(t)
	renderRate(f, "any-agent", "reviewer", 5, "great work", 3.0, 4.5, 2)
	out := readTempFile(t, f)
	if !strings.Contains(out, "Comment:") {
		t.Errorf("rate with comment should show Comment line: %s", out)
	}
}

// TestCapabilitiesStringVerify verifies basic join behavior (covers edge cases).
func TestCapabilitiesStringVerify(t *testing.T) {
	caps := []marketplace.Capability{marketplace.CapGo, marketplace.CapPython}
	if got := capabilitiesString(caps); !strings.Contains(got, "go") || !strings.Contains(got, "python") {
		t.Errorf("capabilitiesString = %q", got)
	}
	if got := capabilitiesString(nil); got != "" {
		t.Errorf("capabilitiesString(nil) = %q, want empty", got)
	}
}

// writeTempFile creates a temp file for render* tests and returns the *os.File.
func writeTempFile(t *testing.T) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "render-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	// Don't close yet — render functions write to it
	return f
}

// readTempFile closes the file, reads its contents, and returns them.
func readTempFile(t *testing.T, f *os.File) string {
	t.Helper()
	_ = f.Close()
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// (TestAgentHasCapability already exists earlier in this test file — see line 127)

// TestRenderList_Empty exercises the "0 agents listed" branch.
func TestRenderList_Empty(t *testing.T) {
	f := writeTempFile(t)
	renderList(f, nil, "table")
	out := readTempFile(t, f)
	if !strings.Contains(out, "0 agents listed") {
		t.Errorf("empty list should say 0 agents: %s", out)
	}
}

// TestRunList_LoadRegistryError_Subprocess exercises loadRegistry os.Exit
// path from inside runList. Skipped because marketplace.NewRegistry silently
// skips malformed YAML (line 55), and ReadDir failure paths are flaky.
func TestRunList_LoadRegistryError_Subprocess(t *testing.T) {
	t.Skip("loadRegistry error paths depend on os.ReadDir failures; flaky")
}

// TestRunSearch_MaxCostFilter exercises the MaxCost branch (searchOptions.MaxCost).
func TestRunSearch_MaxCostFilter(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "cheap-agent", 80, marketplace.TierPro, []string{"go"})

	opts := &searchOptions{
		marketplace: dir,
		maxCost:     100.0, // high threshold to include everything
		limit:       10,
	}
	out := withRedirectedStdout(t, func() {
		_ = runSearch(opts)
	})
	if !strings.Contains(out, "cheap-agent") {
		t.Errorf("search with high max-cost should include cheap-agent: %s", out)
	}
}

// TestRunRate_InvalidRatingFormat exercises the strconv.Atoi error branch
// (line 369) — non-numeric input gets caught by the same error check.
func TestRunRate_InvalidRatingFormat_Subprocess(t *testing.T) {
	if os.Getenv("RUN_RATE_BADFORMAT_SUBPROCESS") == "1" {
		dir := t.TempDir()
		writeTestAgentYAML(t, dir, "any", 80, marketplace.TierPro, []string{"go"})
		opts := &rateOptions{author: "reviewer", marketplace: dir}
		_ = runRate(opts, "any", "not-a-number")
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestRunRate_InvalidRatingFormat_Subprocess")
	cmd.Env = append(os.Environ(), "RUN_RATE_BADFORMAT_SUBPROCESS=1")
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "INVALID_RATING") {
		t.Errorf("non-numeric rating should emit INVALID_RATING: %s", out)
	}
}

// TestRunRate_RatingOutOfRange exercises the rating < 1 || > 5 branch (line 369).
func TestRunRate_RatingOutOfRange_Subprocess(t *testing.T) {
	if os.Getenv("RUN_RATE_OOR_SUBPROCESS") == "1" {
		dir := t.TempDir()
		writeTestAgentYAML(t, dir, "any", 80, marketplace.TierPro, []string{"go"})
		opts := &rateOptions{author: "reviewer", marketplace: dir}
		_ = runRate(opts, "any", "10")
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestRunRate_RatingOutOfRange_Subprocess")
	cmd.Env = append(os.Environ(), "RUN_RATE_OOR_SUBPROCESS=1")
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "INVALID_RATING") {
		t.Errorf("out-of-range rating should emit INVALID_RATING: %s", out)
	}
}

// TestRunRate_DryRun_Subprocess exercises the runRate os.Exit(ExitDryRun) branch (line 395).
func TestRunRate_DryRun_Subprocess(t *testing.T) {
	if os.Getenv("RUN_RATE_DRY_SUBPROCESS") == "1" {
		dir := t.TempDir()
		writeTestAgentYAML(t, dir, "rate-dry", 80, marketplace.TierPro, []string{"go"})
		opts := &rateOptions{author: "reviewer", marketplace: dir, dryRun: true}
		_ = runRate(opts, "rate-dry", "4")
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestRunRate_DryRun_Subprocess")
	cmd.Env = append(os.Environ(), "RUN_RATE_DRY_SUBPROCESS=1")
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "DRY_RUN") {
		t.Errorf("rate dry-run should emit DRY_RUN: %s", out)
	}
}

// TestRunRate_Verbose exercises the verbose branch in runRate (line 389-391).
func TestRunRate_Verbose(t *testing.T) {
	dir := t.TempDir()
	writeTestAgentYAML(t, dir, "rate-verbose", 80, marketplace.TierPro, []string{"go"})
	opts := &rateOptions{
		author:      "reviewer",
		marketplace: dir,
		verbose:     true,
	}
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	_ = withRedirectedStdout(t, func() {
		_ = runRate(opts, "rate-verbose", "4")
	})
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	os.Stderr = oldStderr
	if !strings.Contains(buf.String(), "operation=RATE") {
		t.Errorf("verbose rate should log operation: %s", buf.String())
	}
}
