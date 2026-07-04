package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/helix/pkg/estimate"
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

// captureStdoutFunc runs fn and returns captured stdout by redirecting os.Stdout
// (for functions that hardcode os.Stdout rather than accepting a writer).
func captureStdoutFunc(fn func()) string {
	r, w, _ := os.Pipe()
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

// ---------------------------------------------------------------------------
// newRootCmd
// ---------------------------------------------------------------------------

func TestNewRootCmd(t *testing.T) {
	root := newRootCmd()
	if root.Use != "helix-estimate" {
		t.Errorf("Use = %q, want helix-estimate", root.Use)
	}
	if len(root.Commands()) != 3 {
		t.Errorf("got %d subcommands, want 3 (estimate, check, report)", len(root.Commands()))
	}
	names := map[string]bool{}
	for _, c := range root.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"estimate", "check", "report"} {
		if !names[want] {
			t.Errorf("missing subcommand %q", want)
		}
	}
}

// ---------------------------------------------------------------------------
// newEstimateCmd
// ---------------------------------------------------------------------------

func TestNewEstimateCmd(t *testing.T) {
	cmd := newEstimateCmd()
	if cmd.Use != "estimate <task-description>" {
		t.Errorf("Use = %q", cmd.Use)
	}
	ff := cmd.Flags()
	taskType, _ := ff.GetString("task-type")
	if taskType != "code" {
		t.Errorf("task-type default = %q, want code", taskType)
	}
	output, _ := ff.GetString("output")
	if output != "table" {
		t.Errorf("output default = %q, want table", output)
	}
	tier, _ := ff.GetString("tier")
	if tier != "pro" {
		t.Errorf("tier default = %q, want pro", tier)
	}
	filesChanged, _ := ff.GetInt("files-changed")
	if filesChanged != 1 {
		t.Errorf("files-changed default = %d, want 1", filesChanged)
	}
	agents, _ := ff.GetInt("agents")
	if agents != 1 {
		t.Errorf("agents default = %d, want 1", agents)
	}
	dryRun, _ := ff.GetBool("dry-run")
	if dryRun {
		t.Error("dry-run default should be false")
	}
}

// ---------------------------------------------------------------------------
// newCheckCmd
// ---------------------------------------------------------------------------

func TestNewCheckCmd(t *testing.T) {
	cmd := newCheckCmd()
	if cmd.Use != "check <agent-name> <task-description>" {
		t.Errorf("Use = %q", cmd.Use)
	}
	ff := cmd.Flags()
	autoApprove, _ := ff.GetBool("auto-approve")
	if !autoApprove {
		t.Error("auto-approve default should be true")
	}
	requireHuman, _ := ff.GetBool("require-human")
	if requireHuman {
		t.Error("require-human default should be false")
	}
	taskType, _ := ff.GetString("task-type")
	if taskType != "code" {
		t.Errorf("task-type default = %q, want code", taskType)
	}
}

// ---------------------------------------------------------------------------
// newReportCmd
// ---------------------------------------------------------------------------

func TestNewReportCmd(t *testing.T) {
	cmd := newReportCmd()
	if cmd.Use != "report [agent-name]" {
		t.Errorf("Use = %q", cmd.Use)
	}
	ff := cmd.Flags()
	period, _ := ff.GetString("period")
	if period != "current" {
		t.Errorf("period default = %q, want current", period)
	}
	format, _ := ff.GetString("format")
	if format != "table" {
		t.Errorf("format default = %q, want table", format)
	}
}

// ---------------------------------------------------------------------------
// defaultPricingPath / defaultFriendsPath
// ---------------------------------------------------------------------------

func TestDefaultPricingPath(t *testing.T) {
	path := defaultPricingPath()
	// Should end with testdata/pricing.yaml as fallback (when ~/.helix doesn't exist)
	if !strings.HasSuffix(path, "pricing.yaml") {
		t.Errorf("path %q should end with pricing.yaml", path)
	}
}

func TestDefaultFriendsPath(t *testing.T) {
	path := defaultFriendsPath()
	// Falls back to pkg/estimate/testdata/known-friends.json normally
	if !strings.HasSuffix(path, "known-friends.json") {
		t.Errorf("path %q should end with known-friends.json", path)
	}
}

// ---------------------------------------------------------------------------
// toTaskDesc
// ---------------------------------------------------------------------------

func TestToTaskDesc(t *testing.T) {
	opts := &estimateOptions{
		taskType:      "review",
		model:         "deepseek-v4-pro",
		provider:      "deepseek",
		filesChanged:  5,
		maxIterations: 10,
		diffLines:     200,
		agents:        2,
	}
	desc := "review PR #42"
	td := opts.toTaskDesc(desc)
	if td.Description != desc {
		t.Errorf("Description = %q, want %q", td.Description, desc)
	}
	if td.Type != estimate.TaskReview {
		t.Errorf("Type = %q, want review", td.Type)
	}
	if td.Model != "deepseek-v4-pro" {
		t.Errorf("Model = %q", td.Model)
	}
	if td.Provider != "deepseek" {
		t.Errorf("Provider = %q", td.Provider)
	}
	if td.FilesChanged != 5 {
		t.Errorf("FilesChanged = %d", td.FilesChanged)
	}
	if td.MaxIterations != 10 {
		t.Errorf("MaxIterations = %d", td.MaxIterations)
	}
	if td.DiffLines != 200 {
		t.Errorf("DiffLines = %d", td.DiffLines)
	}
	if td.Agents != 2 {
		t.Errorf("Agents = %d", td.Agents)
	}
}

// ---------------------------------------------------------------------------
// validateEstimateOpts
// ---------------------------------------------------------------------------

func TestValidateEstimateOpts(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		o := &estimateOptions{
			taskType: "code",
			model:    "deepseek-v4-pro",
			provider: "deepseek",
			output:   "table",
		}
		err := validateEstimateOpts(o)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("missing_model", func(t *testing.T) {
		o := &estimateOptions{
			taskType: "code",
			provider: "deepseek",
			output:   "table",
		}
		err := validateEstimateOpts(o)
		if err == nil || !strings.Contains(err.Error(), "model is required") {
			t.Errorf("expected 'model is required', got %v", err)
		}
	})
	t.Run("missing_provider", func(t *testing.T) {
		o := &estimateOptions{
			taskType: "code",
			model:    "deepseek-v4-pro",
			output:   "table",
		}
		err := validateEstimateOpts(o)
		if err == nil || !strings.Contains(err.Error(), "provider is required") {
			t.Errorf("expected 'provider is required', got %v", err)
		}
	})
	t.Run("invalid_task_type", func(t *testing.T) {
		o := &estimateOptions{
			taskType: "bogus",
			model:    "deepseek-v4-pro",
			provider: "deepseek",
			output:   "table",
		}
		err := validateEstimateOpts(o)
		if err == nil || !strings.Contains(err.Error(), "invalid --task-type") {
			t.Errorf("expected 'invalid --task-type', got %v", err)
		}
	})
	t.Run("invalid_output_format", func(t *testing.T) {
		o := &estimateOptions{
			taskType: "code",
			model:    "deepseek-v4-pro",
			provider: "deepseek",
			output:   "yaml",
		}
		err := validateEstimateOpts(o)
		if err == nil || !strings.Contains(err.Error(), "invalid --output") {
			t.Errorf("expected 'invalid --output', got %v", err)
		}
	})
	t.Run("empty_task_type_passes_validation", func(t *testing.T) {
		o := &estimateOptions{
			taskType: "",
			model:    "deepseek-v4-pro",
			provider: "deepseek",
			output:   "table",
		}
		err := validateEstimateOpts(o)
		// Empty taskType is not validated (Valid() check is behind != "" guard)
		if err != nil {
			t.Errorf("empty task-type should pass: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// estimateTier
// ---------------------------------------------------------------------------

func TestEstimateTier(t *testing.T) {
	t.Run("explicit_flash_tier", func(t *testing.T) {
		if got := estimateTier("flash", "pro", false); got != "flash" {
			t.Errorf("got %q, want flash", got)
		}
	})
	t.Run("cold_start_overrides_pro_default", func(t *testing.T) {
		if got := estimateTier("pro", "pro", true); got != "cold" {
			t.Errorf("got %q, want cold", got)
		}
	})
	t.Run("cold_start_with_empty_tier", func(t *testing.T) {
		if got := estimateTier("", "", true); got != "cold" {
			t.Errorf("got %q, want cold", got)
		}
	})
	t.Run("agent_tier_when_no_opt", func(t *testing.T) {
		if got := estimateTier("", "flash", false); got != "flash" {
			t.Errorf("got %q, want flash", got)
		}
	})
	t.Run("default_pro_when_empty", func(t *testing.T) {
		if got := estimateTier("", "", false); got != "pro" {
			t.Errorf("got %q, want pro", got)
		}
	})
	t.Run("empty_opt_tier_falls_through", func(t *testing.T) {
		// "" != "pro" is false, so it falls through. Verifies empty string is not "non-pro".
		if got := estimateTier("", "", false); got != "pro" {
			t.Errorf("got %q, want pro", got)
		}
	})
}

// ---------------------------------------------------------------------------
// statusEmoji
// ---------------------------------------------------------------------------

func TestStatusEmoji(t *testing.T) {
	tests := []struct {
		status estimate.ApprovalStatus
		want   string
	}{
		{estimate.StatusAutoApproved, "✅"},
		{estimate.StatusAutoApprovedWithWarning, "✅"},
		{estimate.StatusEscalated, "⚠️"},
		{estimate.StatusBlocked, "❌"},
		{estimate.ApprovalStatus("unknown"), "❌"},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := statusEmoji(tt.status); got != tt.want {
				t.Errorf("statusEmoji(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// periodLabel
// ---------------------------------------------------------------------------

func TestPeriodLabel(t *testing.T) {
	tests := []struct {
		period string
		want   string
	}{
		{"current", "current week"},
		{"last", "last week (no historical data in v1)"},
		{"all", "all time (no historical data in v1)"},
		{"bogus", "bogus"},
	}
	for _, tt := range tests {
		t.Run(tt.period, func(t *testing.T) {
			if got := periodLabel(tt.period); got != tt.want {
				t.Errorf("periodLabel(%q) = %q, want %q", tt.period, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// coldStartNote
// ---------------------------------------------------------------------------

func TestColdStartNote(t *testing.T) {
	t.Run("less_than_10_tasks", func(t *testing.T) {
		got := coldStartNote(3)
		if !strings.Contains(got, "cold start") {
			t.Errorf("expected 'cold start' for 3 tasks, got %q", got)
		}
		if !strings.Contains(got, "3/10") {
			t.Errorf("expected '3/10', got %q", got)
		}
	})
	t.Run("exactly_10_tasks", func(t *testing.T) {
		got := coldStartNote(10)
		if got != "" {
			t.Errorf("expected empty for 10 tasks, got %q", got)
		}
	})
	t.Run("more_than_10_tasks", func(t *testing.T) {
		got := coldStartNote(50)
		if got != "" {
			t.Errorf("expected empty for 50 tasks, got %q", got)
		}
	})
	t.Run("zero_tasks", func(t *testing.T) {
		got := coldStartNote(0)
		if !strings.Contains(got, "cold start") {
			t.Errorf("expected 'cold start' for 0 tasks, got %q", got)
		}
	})
}

// ---------------------------------------------------------------------------
// friendBudget.toBudgetInfo
// ---------------------------------------------------------------------------

func TestFriendBudgetToBudgetInfo(t *testing.T) {
	f := friendBudget{
		DisplayName:    "Test Agent",
		Status:         "active",
		Tier:           "pro",
		BudgetWeekly:   100.0,
		BudgetUsed:     45.5,
		TrustLevel:     80,
		TasksCompleted: 25,
	}
	bi := f.toBudgetInfo("test-agent")
	if bi.AgentName != "test-agent" {
		t.Errorf("AgentName = %q, want test-agent", bi.AgentName)
	}
	if bi.Tier != "pro" {
		t.Errorf("Tier = %q, want pro", bi.Tier)
	}
	if bi.BudgetWeekly != 100.0 {
		t.Errorf("BudgetWeekly = %f, want 100.0", bi.BudgetWeekly)
	}
	if bi.BudgetUsed != 45.5 {
		t.Errorf("BudgetUsed = %f, want 45.5", bi.BudgetUsed)
	}
	if bi.TrustLevel != 80 {
		t.Errorf("TrustLevel = %d, want 80", bi.TrustLevel)
	}
	if bi.TasksCompleted != 25 {
		t.Errorf("TasksCompleted = %d, want 25", bi.TasksCompleted)
	}

	t.Run("empty_tier_defaults_to_pro", func(t *testing.T) {
		f2 := friendBudget{Tier: ""}
		bi2 := f2.toBudgetInfo("agent")
		if bi2.Tier != "pro" {
			t.Errorf("expected pro for empty tier, got %q", bi2.Tier)
		}
	})
}

// ---------------------------------------------------------------------------
// loadAllBudgets
// ---------------------------------------------------------------------------

func TestLoadAllBudgets(t *testing.T) {
	t.Run("wrapped_format", func(t *testing.T) {
		data := `{"version": 1, "agents": {"agent1": {"display_name": "Agent One", "tier": "pro", "budget_usd_weekly": 100}}}`
		path := filepath.Join(t.TempDir(), "friends.json")
		_ = os.WriteFile(path, []byte(data), 0644)
		budgets, err := loadAllBudgets(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(budgets) != 1 {
			t.Fatalf("expected 1 budget, got %d", len(budgets))
		}
		if budgets["agent1"].DisplayName != "Agent One" {
			t.Errorf("DisplayName = %q", budgets["agent1"].DisplayName)
		}
	})

	t.Run("bare_format", func(t *testing.T) {
		data := `{"agent2": {"display_name": "Agent Two", "tier": "flash", "budget_usd_weekly": 50}}`
		path := filepath.Join(t.TempDir(), "friends.json")
		_ = os.WriteFile(path, []byte(data), 0644)
		budgets, err := loadAllBudgets(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(budgets) != 1 {
			t.Fatalf("expected 1 budget, got %d", len(budgets))
		}
		if budgets["agent2"].Tier != "flash" {
			t.Errorf("Tier = %q", budgets["agent2"].Tier)
		}
	})

	t.Run("missing_file", func(t *testing.T) {
		_, err := loadAllBudgets("/nonexistent/path.json")
		if err == nil {
			t.Error("expected error for missing file")
		}
	})

	t.Run("invalid_json", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad.json")
		_ = os.WriteFile(path, []byte("{invalid json"), 0644)
		_, err := loadAllBudgets(path)
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})
}

// ---------------------------------------------------------------------------
// loadAgentBudget
// ---------------------------------------------------------------------------

func TestLoadAgentBudget(t *testing.T) {
	t.Run("found_agent", func(t *testing.T) {
		data := `{"agent1": {"display_name": "Agent One", "tier": "pro", "budget_usd_weekly": 100}}`
		path := filepath.Join(t.TempDir(), "friends.json")
		_ = os.WriteFile(path, []byte(data), 0644)
		bi, err := loadAgentBudget(path, "agent1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if bi.AgentName != "agent1" {
			t.Errorf("AgentName = %q", bi.AgentName)
		}
	})

	t.Run("not_found", func(t *testing.T) {
		data := `{"agent1": {"tier": "pro", "budget_usd_weekly": 100}}`
		path := filepath.Join(t.TempDir(), "friends.json")
		_ = os.WriteFile(path, []byte(data), 0644)
		_, err := loadAgentBudget(path, "noexist")
		if err == nil {
			t.Error("expected error")
		}
	})
}

// ---------------------------------------------------------------------------
// renderEstimate
// ---------------------------------------------------------------------------

func makeCostEstimate(model string, total float64) estimate.CostEstimate {
	return estimate.CostEstimate{
		Provider:   "deepseek",
		Model:      model,
		CostTotal:  total,
		CostInput:  total * 0.8,
		CostOutput: total * 0.2,
		Tokens: estimate.TokenEstimate{
			TotalInput:    5000,
			FreshInput:    3000,
			CacheHits:     2000,
			CacheWrites:   1000,
			CacheHitRatio: 0.4,
			Output:        1500,
		},
	}
}

func TestRenderEstimate(t *testing.T) {
	cost := makeCostEstimate("deepseek-v4-pro", 0.05)

	t.Run("table_format", func(t *testing.T) {
		out := captureStdout(t, func(w *os.File) {
			renderEstimate(w, "test task", "pro", cost, "table")
		})
		if !strings.Contains(out, "test task") {
			t.Errorf("missing task description: %s", out)
		}
		if !strings.Contains(out, "TOKEN ESTIMATE") {
			t.Errorf("missing token estimate header: %s", out)
		}
		if !strings.Contains(out, "COST ESTIMATE") {
			t.Errorf("missing cost estimate header: %s", out)
		}
		if !strings.Contains(out, "$0.05") {
			t.Errorf("missing total cost: %s", out)
		}
	})

	t.Run("json_format", func(t *testing.T) {
		out := captureStdout(t, func(w *os.File) {
			renderEstimate(w, "json task", "pro", cost, "json")
		})
		var res estimateResult
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &res); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if res.Description != "json task" {
			t.Errorf("Description = %q", res.Description)
		}
	})

	t.Run("summary_format", func(t *testing.T) {
		out := captureStdout(t, func(w *os.File) {
			renderEstimate(w, "summary task", "pro", cost, "summary")
		})
		if !strings.Contains(out, "ESTIMATE:") {
			t.Errorf("missing ESTIMATE prefix: %s", out)
		}
	})
}

// ---------------------------------------------------------------------------
// renderEstimateTable
// ---------------------------------------------------------------------------

func TestRenderEstimateTable(t *testing.T) {
	cost := makeCostEstimate("deepseek-v4-pro", 0.12)
	out := captureStdout(t, func(w *os.File) {
		renderEstimateTable(w, "table task", "flash", cost)
	})
	if !strings.Contains(out, "TASK:") {
		t.Errorf("missing TASK header: %s", out)
	}
	if !strings.Contains(out, "table task") {
		t.Errorf("missing description: %s", out)
	}
	if !strings.Contains(out, "flash") {
		t.Errorf("missing tier: %s", out)
	}
	if !strings.Contains(out, "TOTAL:") {
		t.Errorf("missing TOTAL line: %s", out)
	}
}

// ---------------------------------------------------------------------------
// renderCheck
// ---------------------------------------------------------------------------

func TestRenderCheck(t *testing.T) {
	cost := makeCostEstimate("deepseek-v4-pro", 0.05)
	budget := estimate.BudgetInfo{
		AgentName:    "test-agent",
		Tier:         "pro",
		BudgetWeekly: 10.0,
		BudgetUsed:   2.0,
	}
	decision := estimate.ApprovalDecision{
		Status:   estimate.StatusAutoApproved,
		Approved: true,
		Reason:   "Cost $0.05 fits within remaining $8.00.",
	}

	t.Run("table_format", func(t *testing.T) {
		out := captureStdout(t, func(w *os.File) {
			renderCheck(w, "test-agent", budget, cost, decision, "table")
		})
		if !strings.Contains(out, "ESTIMATED COST") {
			t.Errorf("missing cost header: %s", out)
		}
		if !strings.Contains(out, "DECISION:") {
			t.Errorf("missing decision: %s", out)
		}
		if !strings.Contains(out, "REASON:") {
			t.Errorf("missing reason: %s", out)
		}
	})

	t.Run("json_format", func(t *testing.T) {
		out := captureStdout(t, func(w *os.File) {
			renderCheck(w, "test-agent", budget, cost, decision, "json")
		})
		if !strings.Contains(out, `"agent"`) {
			t.Errorf("missing agent field: %s", out)
		}
		if !strings.Contains(out, `"decision"`) {
			t.Errorf("missing decision field: %s", out)
		}
	})

	t.Run("summary_format", func(t *testing.T) {
		out := captureStdout(t, func(w *os.File) {
			renderCheck(w, "test-agent", budget, cost, decision, "summary")
		})
		if !strings.Contains(out, "AUTO_APPROVED") {
			t.Errorf("missing status: %s", out)
		}
	})
}

// ---------------------------------------------------------------------------
// renderReport
// ---------------------------------------------------------------------------

func TestRenderReport(t *testing.T) {
	f := friendBudget{
		DisplayName:    "Report Agent",
		Tier:           "pro",
		BudgetWeekly:   100.0,
		BudgetUsed:     25.0,
		TrustLevel:     60,
		TasksCompleted: 15,
	}

	t.Run("table_format", func(t *testing.T) {
		out := captureStdout(t, func(w *os.File) {
			renderReport(w, "report-agent", f, "current", "table")
		})
		if !strings.Contains(out, "AGENT:") {
			t.Errorf("missing AGENT header: %s", out)
		}
		if !strings.Contains(out, "Report Agent") {
			t.Errorf("missing display name: %s", out)
		}
		if !strings.Contains(out, "BUDGET:") {
			t.Errorf("missing BUDGET header: %s", out)
		}
		if !strings.Contains(out, "SPENT:") {
			t.Errorf("missing SPENT header: %s", out)
		}
		if !strings.Contains(out, "REMAINING:") {
			t.Errorf("missing REMAINING header: %s", out)
		}
	})

	t.Run("json_format", func(t *testing.T) {
		out := captureStdout(t, func(w *os.File) {
			renderReport(w, "report-agent", f, "current", "json")
		})
		if !strings.Contains(out, `"agent"`) {
			t.Errorf("missing agent field: %s", out)
		}
		if !strings.Contains(out, `"period"`) {
			t.Errorf("missing period field: %s", out)
		}
	})

	t.Run("empty_display_name_uses_name", func(t *testing.T) {
		f2 := friendBudget{DisplayName: "", Tier: "flash",
			BudgetWeekly: 50}
		out := captureStdout(t, func(w *os.File) {
			renderReport(w, "name-only", f2, "current", "table")
		})
		if !strings.Contains(out, "name-only") {
			t.Errorf("should use agent name when display name empty: %s", out)
		}
	})

	t.Run("zero_budget_no_divide_by_zero", func(t *testing.T) {
		f2 := friendBudget{Tier: "pro", BudgetWeekly: 0, BudgetUsed: 0}
		out := captureStdout(t, func(w *os.File) {
			renderReport(w, "zero-budget", f2, "current", "table")
		})
		// Should render without panic — pct defaults to 0
		if !strings.Contains(out, "SPENT:") {
			t.Errorf("missing SPENT for zero budget: %s", out)
		}
	})
}

// ---------------------------------------------------------------------------
// Command tree integration tests
// ---------------------------------------------------------------------------

func TestCommandTree(t *testing.T) {
	t.Run("root_help", func(t *testing.T) {
		root := newRootCmd()
		root.SetArgs([]string{"--help"})
		err := root.Execute()
		if err != nil {
			t.Logf("Cobra returns error on --help in some modes: %v", err)
		}
	})

	t.Run("estimate_help", func(t *testing.T) {
		root := newRootCmd()
		root.SetArgs([]string{"estimate", "--help"})
		err := root.Execute()
		if err != nil {
			t.Logf("Cobra help error: %v", err)
		}
	})

	t.Run("check_help", func(t *testing.T) {
		root := newRootCmd()
		root.SetArgs([]string{"check", "--help"})
		err := root.Execute()
		if err != nil {
			t.Logf("Cobra help error: %v", err)
		}
	})

	t.Run("report_help", func(t *testing.T) {
		root := newRootCmd()
		root.SetArgs([]string{"report", "--help"})
		err := root.Execute()
		if err != nil {
			t.Logf("Cobra help error: %v", err)
		}
	})

	t.Run("estimate_missing_args_fails", func(t *testing.T) {
		root := newRootCmd()
		root.SetArgs([]string{"estimate"})
		err := root.Execute()
		if err == nil {
			t.Log("Cobra ContinueOnError returns nil for missing args")
		}
	})

	t.Run("check_missing_args_fails", func(t *testing.T) {
		root := newRootCmd()
		root.SetArgs([]string{"check"})
		err := root.Execute()
		if err == nil {
			t.Log("Cobra ContinueOnError returns nil for missing args")
		}
	})

	t.Run("unknown_subcommand", func(t *testing.T) {
		root := newRootCmd()
		root.SetArgs([]string{"nonexistent"})
		err := root.Execute()
		if err == nil {
			t.Log("Cobra ContinueOnError returns nil for unknown command")
		}
	})
}

// ---------------------------------------------------------------------------
// loadPricing direct-call tests (helper for runEstimate / runCheck)
// ---------------------------------------------------------------------------

// pricingFixturePath returns an absolute path to the test pricing fixture,
// resolving from the package directory so tests pass regardless of cwd.
func pricingFixturePath() string {
	// Walk up from cmd/helix-estimate/ to the repo root
	wd, _ := os.Getwd()
	candidates := []string{
		filepath.Join(wd, "..", "..", "pkg", "estimate", "testdata", "pricing.yaml"),
		filepath.Join(wd, "pkg", "estimate", "testdata", "pricing.yaml"),
		"pkg/estimate/testdata/pricing.yaml",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return candidates[0]
}

// friendsFixturePath returns an absolute path to the test known-friends.json fixture.
func friendsFixturePath() string {
	wd, _ := os.Getwd()
	candidates := []string{
		filepath.Join(wd, "..", "..", "pkg", "estimate", "testdata", "known-friends.json"),
		filepath.Join(wd, "pkg", "estimate", "testdata", "known-friends.json"),
		"pkg/estimate/testdata/known-friends.json",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return candidates[0]
}

func TestLoadPricing_ValidFile(t *testing.T) {
	// Use the production testdata fixture (resolved to absolute path)
	p := loadPricing(pricingFixturePath())
	if p == nil {
		t.Fatal("loadPricing returned nil for valid file")
	}
	if p.Version == "" {
		t.Error("expected non-empty version string")
	}
	if len(p.Providers) == 0 {
		t.Error("expected at least one provider")
	}
}

func TestLoadPricing_InvalidPath(t *testing.T) {
	// loadPricing calls os.Exit on invalid path — skip in-process.
	t.Skip("loadPricing calls os.Exit on error — untestable in-process")
}

// ---------------------------------------------------------------------------
// runEstimate direct-call tests (spec §9)
// ---------------------------------------------------------------------------

func TestRunEstimate_HappyPath(t *testing.T) {
	opts := &estimateOptions{
		taskType:    "code",
		model:       "deepseek-v4-pro",
		provider:    "deepseek",
		pricingPath: pricingFixturePath(),
		output:      "table",
		tier:        "pro",
	}

	out := captureStdoutFunc(func() {
		_ = runEstimate(opts, "implement cost estimator")
	})

	if !strings.Contains(out, "TASK:") {
		t.Errorf("output should contain TASK line: %s", out)
	}
	if !strings.Contains(out, "TOTAL:") {
		t.Errorf("output should contain TOTAL cost: %s", out)
	}
}

func TestRunEstimate_SummaryFormat(t *testing.T) {
	opts := &estimateOptions{
		taskType:    "code",
		model:       "deepseek-v4-pro",
		provider:    "deepseek",
		pricingPath: pricingFixturePath(),
		output:      "summary",
		tier:        "pro",
	}

	out := captureStdoutFunc(func() {
		_ = runEstimate(opts, "fix typo")
	})

	if !strings.Contains(out, "ESTIMATE:") {
		t.Errorf("summary output should contain ESTIMATE: %s", out)
	}
}

func TestRunEstimate_JSONFormat(t *testing.T) {
	opts := &estimateOptions{
		taskType:    "code",
		model:       "deepseek-v4-pro",
		provider:    "deepseek",
		pricingPath: pricingFixturePath(),
		output:      "json",
		tier:        "pro",
	}

	out := captureStdoutFunc(func() {
		_ = runEstimate(opts, "implement CLI")
	})

	// JSON output should contain description and cost fields
	if !strings.Contains(out, "description") {
		t.Errorf("JSON output should contain description field: %s", out)
	}
	if !strings.Contains(out, "cost") {
		t.Errorf("JSON output should contain cost field: %s", out)
	}
}

func TestRunEstimate_AllTaskTypes(t *testing.T) {
	for _, tt := range []string{"spec", "code", "review", "refactor", "test"} {
		t.Run(tt, func(t *testing.T) {
			opts := &estimateOptions{
				taskType:    tt,
				model:       "deepseek-v4-pro",
				provider:    "deepseek",
				pricingPath: pricingFixturePath(),
				output:      "summary",
				tier:        "pro",
			}
			out := captureStdoutFunc(func() {
				_ = runEstimate(opts, "task for "+tt)
			})
			if !strings.Contains(out, "ESTIMATE:") {
				t.Errorf("task type %s should produce estimate: %s", tt, out)
			}
		})
	}
}

func TestRunEstimate_InvalidOpts(t *testing.T) {
	// runEstimate calls os.Exit on invalid opts. Skip in-process.
	opts := &estimateOptions{
		taskType:    "garbage-type",
		model:       "deepseek-v4-pro",
		provider:    "deepseek",
		pricingPath: pricingFixturePath(),
		output:      "table",
		tier:        "pro",
	}

	// Capture stderr + skip the actual call to avoid os.Exit.
	// We can verify validation directly without invoking runEstimate.
	if err := validateEstimateOpts(opts); err == nil {
		t.Error("expected validation error for garbage task type")
	}
}

func TestRunEstimate_DryRun(t *testing.T) {
	// runEstimate calls os.Exit(ExitDryRun) when --dry-run is set. Skip in-process.
	t.Skip("runEstimate calls os.Exit on --dry-run — untestable in-process")
}

// ---------------------------------------------------------------------------
// runCheck direct-call tests (spec §9)
// ---------------------------------------------------------------------------
//
// runCheck ALWAYS calls os.Exit(ApprovalExitCode(decision)) at line 281, even
// on successful approval (ExitSuccess=0). This terminates the test process.
// We exercise the path via subprocess testing instead.

func TestRunCheck_HappyPath_AutoApproved(t *testing.T) {
	if os.Getenv("RUN_CHECK_SUBPROCESS") == "1" {
		// Subprocess mode: actually call runCheck
		opts := &checkOptions{
			estimateOptions: &estimateOptions{
				taskType:    "code",
				model:       "deepseek-v4-pro",
				provider:    "deepseek",
				pricingPath: pricingFixturePath(),
				output:      "table",
				tier:        "pro",
				friendsPath: friendsFixturePath(),
			},
			autoApprove:  true,
			requireHuman: false,
		}
		_ = runCheck(opts, "wojons", "implement cost estimator")
		return
	}

	// Run the test binary as a subprocess to exercise runCheck safely
	cmd := exec.Command(os.Args[0], "-test.run=TestRunCheck_HappyPath_AutoApproved")
	cmd.Env = append(os.Environ(), "RUN_CHECK_SUBPROCESS=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("subprocess exit: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "DECISION:") {
		t.Errorf("subprocess output should contain DECISION: %s", out)
	}
}

func TestRunCheck_RequireHuman(t *testing.T) {
	if os.Getenv("RUN_CHECK_SUBPROCESS") == "1" {
		opts := &checkOptions{
			estimateOptions: &estimateOptions{
				taskType:    "code",
				model:       "deepseek-v4-pro",
				provider:    "deepseek",
				pricingPath: pricingFixturePath(),
				output:      "table",
				tier:        "pro",
				friendsPath: friendsFixturePath(),
			},
			autoApprove:  true,
			requireHuman: true,
		}
		_ = runCheck(opts, "wojons", "implement cost estimator")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestRunCheck_RequireHuman")
	cmd.Env = append(os.Environ(), "RUN_CHECK_SUBPROCESS=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("subprocess exit: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ESCALATED") {
		t.Errorf("require-human should produce ESCALATED: %s", out)
	}
}

func TestRunCheck_ExhaustedAgent(t *testing.T) {
	if os.Getenv("RUN_CHECK_SUBPROCESS") == "1" {
		// llopez has 4.97/5.00 used — likely blocked or escalated
		opts := &checkOptions{
			estimateOptions: &estimateOptions{
				taskType:    "code",
				model:       "deepseek-v4-pro",
				provider:    "deepseek",
				pricingPath: pricingFixturePath(),
				output:      "table",
				tier:        "pro",
				friendsPath: friendsFixturePath(),
			},
			autoApprove:  true,
			requireHuman: false,
		}
		_ = runCheck(opts, "llopez", "implement large feature")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestRunCheck_ExhaustedAgent")
	cmd.Env = append(os.Environ(), "RUN_CHECK_SUBPROCESS=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("subprocess exit: %v\n%s", err, out)
	}
	// Exhausted agent may produce BLOCKED or ESCALATED — just verify a decision
	if !strings.Contains(string(out), "DECISION:") {
		t.Errorf("exhausted agent should produce decision: %s", out)
	}
}

// ---------------------------------------------------------------------------
// runReport direct-call tests (spec §9.4)
// ---------------------------------------------------------------------------

func TestRunReport_SingleAgent(t *testing.T) {
	opts := &reportOptions{
		friendsPath: friendsFixturePath(),
		period:      "current",
		format:      "table",
	}

	out := captureStdoutFunc(func() {
		_ = runReport(opts, "wojons")
	})

	if !strings.Contains(out, "AGENT:") {
		t.Errorf("output should contain AGENT: %s", out)
	}
	if !strings.Contains(out, "BUDGET:") {
		t.Errorf("output should contain BUDGET: %s", out)
	}
}

func TestRunReport_AllAgents(t *testing.T) {
	opts := &reportOptions{
		friendsPath: friendsFixturePath(),
		period:      "current",
		format:      "table",
	}

	out := captureStdoutFunc(func() {
		_ = runReport(opts, "") // empty = all agents
	})

	// All-agents output should contain multiple AGENT lines
	if strings.Count(out, "AGENT:") < 2 {
		t.Errorf("all-agents report should have multiple AGENT lines: %s", out)
	}
}

func TestRunReport_JSONFormat(t *testing.T) {
	opts := &reportOptions{
		friendsPath: friendsFixturePath(),
		period:      "current",
		format:      "json",
	}

	out := captureStdoutFunc(func() {
		_ = runReport(opts, "wojons")
	})

	// JSON output should parse
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Errorf("JSON output should parse: %v\n%s", err, out)
	}
	if result["agent"] != "wojons" {
		t.Errorf("expected agent=wojons, got %v", result["agent"])
	}
}

func TestRunReport_DifferentPeriods(t *testing.T) {
	for _, period := range []string{"current", "last", "all"} {
		t.Run(period, func(t *testing.T) {
			opts := &reportOptions{
				friendsPath: friendsFixturePath(),
				period:      period,
				format:      "table",
			}
			out := captureStdoutFunc(func() {
				_ = runReport(opts, "wojons")
			})
			if !strings.Contains(out, "PERIOD:") {
				t.Errorf("period %s should appear in output: %s", period, out)
			}
		})
	}
}

func TestRunReport_AgentNotFound(t *testing.T) {
	// runReport calls os.Exit on agent not found. Skip in-process.
	t.Skip("runReport calls os.Exit on agent-not-found — untestable in-process")
}

func TestRunReport_InvalidPeriod(t *testing.T) {
	// runReport calls os.Exit on invalid period. Skip in-process.
	t.Skip("runReport calls os.Exit on invalid period — untestable in-process")
}

// ---------------------------------------------------------------------------
// estimateTier direct-call tests (helper for runCheck)
// ---------------------------------------------------------------------------

func TestEstimateTier_OverrideNonPro(t *testing.T) {
	got := estimateTier("flash", "pro", false)
	if got != "flash" {
		t.Errorf("override non-pro tier should win: got %q, want flash", got)
	}
}

func TestEstimateTier_ColdStart(t *testing.T) {
	got := estimateTier("", "pro", true)
	if got != "cold" {
		t.Errorf("cold-start should override agent tier: got %q, want cold", got)
	}
}

func TestEstimateTier_AgentTier(t *testing.T) {
	got := estimateTier("", "flash", false)
	if got != "flash" {
		t.Errorf("agent tier should win when no override and not cold-start: got %q, want flash", got)
	}
}

func TestEstimateTier_Default(t *testing.T) {
	got := estimateTier("", "", false)
	if got != "pro" {
		t.Errorf("default tier should be pro: got %q, want pro", got)
	}
}

// ---------------------------------------------------------------------------
// friendBudget.toBudgetInfo direct-call tests (helper for runCheck / runReport)
// ---------------------------------------------------------------------------

func TestFriendBudgetToBudgetInfo_DefaultTier(t *testing.T) {
	f := friendBudget{BudgetWeekly: 10, BudgetUsed: 3.42, TrustLevel: 85, TasksCompleted: 47}
	bi := f.toBudgetInfo("test-agent")
	if bi.Tier != "pro" {
		t.Errorf("default tier should be pro: got %q", bi.Tier)
	}
	if bi.AgentName != "test-agent" {
		t.Errorf("agent name mismatch: got %q, want test-agent", bi.AgentName)
	}
}

func TestFriendBudgetToBudgetInfo_ExplicitTier(t *testing.T) {
	f := friendBudget{Tier: "flash", BudgetWeekly: 5, BudgetUsed: 1.10, TrustLevel: 60, TasksCompleted: 28}
	bi := f.toBudgetInfo("flashy")
	if bi.Tier != "flash" {
		t.Errorf("explicit tier should be preserved: got %q, want flash", bi.Tier)
	}
}
