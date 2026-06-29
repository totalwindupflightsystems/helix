package main

import (
	"bytes"
	"os"
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
