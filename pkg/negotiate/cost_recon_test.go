package negotiate

import (
	"math"
	"testing"
)

// --- AgentBudget tests ---

func TestAgentBudget_RemainingBudget(t *testing.T) {
	tests := []struct {
		name     string
		b        AgentBudget
		expected float64
	}{
		{"normal", AgentBudget{BudgetWeekly: 100, BudgetUsed: 30}, 70},
		{"exactly at cap", AgentBudget{BudgetWeekly: 100, BudgetUsed: 100}, 0},
		{"over budget clamps to zero", AgentBudget{BudgetWeekly: 100, BudgetUsed: 150}, 0},
		{"zero budget", AgentBudget{BudgetWeekly: 0, BudgetUsed: 0}, 0},
		{"negative used", AgentBudget{BudgetWeekly: 50, BudgetUsed: -10}, 60},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.b.RemainingBudget()
			if got != tc.expected {
				t.Errorf("RemainingBudget() = %.2f, want %.2f", got, tc.expected)
			}
		})
	}
}

// --- CostReconciler RecordRoundCost tests ---

func TestRecordRoundCost(t *testing.T) {
	cr := NewCostReconciler(nil)
	cr.RecordRoundCost(1, "agent-a", 0.002, "round 1 arg")
	cr.RecordRoundCost(1, "agent-b", 0.0015, "round 1 rebuttal")
	cr.RecordRoundCost(2, "agent-a", 0.003, "round 2 arg")

	if cr.DebateCostForAgent("agent-a") != 0.005 {
		t.Errorf("agent-a debate cost = %.6f, want 0.005", cr.DebateCostForAgent("agent-a"))
	}
	if cr.DebateCostForAgent("agent-b") != 0.0015 {
		t.Errorf("agent-b debate cost = %.6f, want 0.0015", cr.DebateCostForAgent("agent-b"))
	}
	total := cr.TotalDebateCost()
	if math.Abs(total-0.0065) > 0.0000001 {
		t.Errorf("total debate cost = %.6f, want 0.0065", total)
	}
}

func TestRecordRoundCost_MultipleRoundsAccumulate(t *testing.T) {
	cr := NewCostReconciler(nil)
	for i := 1; i <= 3; i++ {
		cr.RecordRoundCost(i, "agent-x", 0.001, "round")
	}
	if cr.DebateCostForAgent("agent-x") != 0.003 {
		t.Errorf("expected 0.003, got %.6f", cr.DebateCostForAgent("agent-x"))
	}
}

func TestRecordRoundCost_UnknownAgent(t *testing.T) {
	cr := NewCostReconciler(nil)
	cr.RecordRoundCost(1, "ghost-agent", 0.001, "test")
	if cr.DebateCostForAgent("ghost-agent") != 0.001 {
		t.Errorf("expected 0.001, got %.6f", cr.DebateCostForAgent("ghost-agent"))
	}
	if cr.DebateCostForAgent("missing-agent") != 0 {
		t.Errorf("expected 0 for unknown agent, got %.6f", cr.DebateCostForAgent("missing-agent"))
	}
}

// --- TieBreakShares tests ---

func TestTieBreakShares_EvenSplit(t *testing.T) {
	cr := NewCostReconciler(nil)
	cr.RecordTieBreakCost(0.10)
	a, b := cr.TieBreakShares("agent-a", "agent-b")
	if a != 0.05 || b != 0.05 {
		t.Errorf("shares = (%.4f, %.4f), want (0.05, 0.05)", a, b)
	}
}

func TestTieBreakShares_OddNumber(t *testing.T) {
	cr := NewCostReconciler(nil)
	cr.RecordTieBreakCost(0.07)
	a, b := cr.TieBreakShares("agent-a", "agent-b")
	// Both should be 0.035
	if math.Abs(a-0.035) > 0.00001 || math.Abs(b-0.035) > 0.00001 {
		t.Errorf("shares = (%.6f, %.6f), want (~0.035, ~0.035)", a, b)
	}
}

func TestTieBreakShares_ZeroCost(t *testing.T) {
	cr := NewCostReconciler(nil)
	a, b := cr.TieBreakShares("agent-a", "agent-b")
	if a != 0 || b != 0 {
		t.Errorf("zero cost shares = (%.4f, %.4f), want (0, 0)", a, b)
	}
}

// --- CheckBudget tests ---

func TestCheckBudget_BothAgentsOK(t *testing.T) {
	budgets := []AgentBudget{
		{AgentName: "a", BudgetWeekly: 100, BudgetUsed: 10},
		{AgentName: "b", BudgetWeekly: 100, BudgetUsed: 20},
	}
	cr := NewCostReconciler(budgets)
	cr.RecordRoundCost(1, "a", 5, "round 1")
	cr.RecordRoundCost(1, "b", 5, "round 1")
	cr.RecordTieBreakCost(20)

	overruns := cr.CheckBudget("a", "b")
	if len(overruns) != 0 {
		t.Errorf("expected no overruns, got %v", overruns)
	}
}

func TestCheckBudget_AgentAOverrun(t *testing.T) {
	budgets := []AgentBudget{
		{AgentName: "a", BudgetWeekly: 10, BudgetUsed: 8},
		{AgentName: "b", BudgetWeekly: 100, BudgetUsed: 10},
	}
	cr := NewCostReconciler(budgets)
	cr.RecordRoundCost(1, "a", 2, "round 1")
	cr.RecordTieBreakCost(10)

	overruns := cr.CheckBudget("a", "b")
	if len(overruns) != 1 || overruns[0] != "a" {
		t.Errorf("expected [a], got %v", overruns)
	}
}

func TestCheckBudget_BothOverrun(t *testing.T) {
	budgets := []AgentBudget{
		{AgentName: "a", BudgetWeekly: 5, BudgetUsed: 4},
		{AgentName: "b", BudgetWeekly: 5, BudgetUsed: 4},
	}
	cr := NewCostReconciler(budgets)
	cr.RecordRoundCost(1, "a", 1, "round 1")
	cr.RecordRoundCost(1, "b", 1, "round 1")
	cr.RecordTieBreakCost(10)

	overruns := cr.CheckBudget("a", "b")
	if len(overruns) != 2 {
		t.Errorf("expected 2 overruns, got %d: %v", len(overruns), overruns)
	}
}

func TestCheckBudget_ZeroBudgetSkipsCheck(t *testing.T) {
	// Agents with BudgetWeekly=0 are treated as unlimited (no cap)
	budgets := []AgentBudget{
		{AgentName: "a", BudgetWeekly: 0, BudgetUsed: 0},
		{AgentName: "b", BudgetWeekly: 0, BudgetUsed: 0},
	}
	cr := NewCostReconciler(budgets)
	cr.RecordTieBreakCost(1000)

	overruns := cr.CheckBudget("a", "b")
	if len(overruns) != 0 {
		t.Errorf("expected no overruns for unlimited budget, got %v", overruns)
	}
}

func TestCheckBudget_UnknownAgentSkipped(t *testing.T) {
	budgets := []AgentBudget{
		{AgentName: "a", BudgetWeekly: 100, BudgetUsed: 0},
	}
	cr := NewCostReconciler(budgets)
	cr.RecordTieBreakCost(10)

	// Agent "b" has no budget entry — should not cause overrun
	overruns := cr.CheckBudget("a", "b")
	if len(overruns) != 0 {
		t.Errorf("expected no overruns, got %v", overruns)
	}
}

// --- ApplyTieBreakCost tests ---

func TestApplyTieBreakCost_NormalCase(t *testing.T) {
	budgets := []AgentBudget{
		{AgentName: "a", BudgetWeekly: 100, BudgetUsed: 10},
		{AgentName: "b", BudgetWeekly: 100, BudgetUsed: 20},
	}
	cr := NewCostReconciler(budgets)
	cr.RecordRoundCost(1, "a", 1.0, "round 1")
	cr.RecordRoundCost(1, "b", 1.5, "round 1")
	cr.RecordRoundCost(2, "a", 0.5, "round 2")

	report := cr.ApplyTieBreakCost(10.0, "a", "b")

	if report.Escalated {
		t.Errorf("should not be escalated for normal case")
	}
	if report.TieBreakCost != 10.0 {
		t.Errorf("tie break cost = %.2f, want 10.0", report.TieBreakCost)
	}

	bdA := report.Breakdowns["a"]
	if bdA.DebateTotal != 1.5 {
		t.Errorf("agent-a debate total = %.2f, want 1.5", bdA.DebateTotal)
	}
	if bdA.TieBreakShare != 5.0 {
		t.Errorf("agent-a tie break share = %.2f, want 5.0", bdA.TieBreakShare)
	}
	if bdA.Total != 6.5 {
		t.Errorf("agent-a total = %.2f, want 6.5", bdA.Total)
	}

	bdB := report.Breakdowns["b"]
	if bdB.DebateTotal != 1.5 {
		t.Errorf("agent-b debate total = %.2f, want 1.5", bdB.DebateTotal)
	}
	if bdB.TieBreakShare != 5.0 {
		t.Errorf("agent-b tie break share = %.2f, want 5.0", bdB.TieBreakShare)
	}
	if bdB.Total != 6.5 {
		t.Errorf("agent-b total = %.2f, want 6.5", bdB.Total)
	}

	// Total cost = debate (3.0) + tie-break (10.0) = 13.0
	if report.TotalCost != 13.0 {
		t.Errorf("total cost = %.2f, want 13.0", report.TotalCost)
	}
}

func TestApplyTieBreakCost_OverrunEscalated(t *testing.T) {
	budgets := []AgentBudget{
		{AgentName: "a", BudgetWeekly: 5, BudgetUsed: 4},
		{AgentName: "b", BudgetWeekly: 100, BudgetUsed: 10},
	}
	cr := NewCostReconciler(budgets)
	cr.RecordRoundCost(1, "a", 1.0, "round 1")

	report := cr.ApplyTieBreakCost(10.0, "a", "b")

	if !report.Escalated {
		t.Error("should be escalated when agent-a budget insufficient")
	}
	if len(report.OverrunAgents) != 1 || report.OverrunAgents[0] != "a" {
		t.Errorf("expected [a] overrun, got %v", report.OverrunAgents)
	}
	if report.Reason == "" {
		t.Error("escalation reason should not be empty")
	}
}

func TestApplyTieBreakCost_BothOverrunEscalated(t *testing.T) {
	budgets := []AgentBudget{
		{AgentName: "a", BudgetWeekly: 1, BudgetUsed: 0},
		{AgentName: "b", BudgetWeekly: 1, BudgetUsed: 0},
	}
	cr := NewCostReconciler(budgets)
	cr.RecordRoundCost(1, "a", 0.5, "round 1")
	cr.RecordRoundCost(1, "b", 0.5, "round 1")

	report := cr.ApplyTieBreakCost(10.0, "a", "b")

	if !report.Escalated {
		t.Error("should be escalated when both agents over budget")
	}
	if len(report.OverrunAgents) != 2 {
		t.Errorf("expected 2 overrun agents, got %v", report.OverrunAgents)
	}
}

func TestApplyTieBreakCost_ZeroTieBreakCost(t *testing.T) {
	budgets := []AgentBudget{
		{AgentName: "a", BudgetWeekly: 100, BudgetUsed: 10},
		{AgentName: "b", BudgetWeekly: 100, BudgetUsed: 10},
	}
	cr := NewCostReconciler(budgets)
	cr.RecordRoundCost(1, "a", 0.5, "round 1")
	cr.RecordRoundCost(1, "b", 0.5, "round 1")

	report := cr.ApplyTieBreakCost(0, "a", "b")

	if report.Escalated {
		t.Error("should not escalate with zero tie-break cost")
	}
	if report.TieBreakCost != 0 {
		t.Errorf("tie break cost = %.2f, want 0", report.TieBreakCost)
	}
	if report.TotalCost != 1.0 {
		t.Errorf("total = %.2f, want 1.0 (debate only)", report.TotalCost)
	}
}

func TestApplyTieBreakCost_FloatPrecision(t *testing.T) {
	budgets := []AgentBudget{
		{AgentName: "a", BudgetWeekly: 1000, BudgetUsed: 0},
		{AgentName: "b", BudgetWeekly: 1000, BudgetUsed: 0},
	}
	cr := NewCostReconciler(budgets)
	// Cost that produces non-trivial halves: 0.03 / 2 = 0.015
	report := cr.ApplyTieBreakCost(0.03, "a", "b")
	if report.TieBreakCost != 0.03 {
		t.Errorf("tie break cost = %.4f, want 0.03", report.TieBreakCost)
	}
	bdA := report.Breakdowns["a"]
	// 0.015 rounded to 2 dp = 0.02
	if bdA.TieBreakShare != 0.02 {
		t.Errorf("agent-a share = %.4f, want 0.02 (rounded up)", bdA.TieBreakShare)
	}
}

func TestApplyTieBreakCost_AgentShares(t *testing.T) {
	budgets := []AgentBudget{
		{AgentName: "a", BudgetWeekly: 1000, BudgetUsed: 0},
		{AgentName: "b", BudgetWeekly: 1000, BudgetUsed: 0},
	}
	cr := NewCostReconciler(budgets)
	cr.RecordRoundCost(1, "a", 3.0, "round 1")
	cr.RecordRoundCost(1, "b", 2.0, "round 1")

	report := cr.ApplyTieBreakCost(10.0, "a", "b")

	// agent-a: 3.0 debate + 5.0 tie-break = 8.0
	if report.AgentShares["a"] != 8.0 {
		t.Errorf("agent-a share = %.2f, want 8.0", report.AgentShares["a"])
	}
	// agent-b: 2.0 debate + 5.0 tie-break = 7.0
	if report.AgentShares["b"] != 7.0 {
		t.Errorf("agent-b share = %.2f, want 7.0", report.AgentShares["b"])
	}
}

func TestApplyTieBreakCost_DebateCostsPreserved(t *testing.T) {
	cr := NewCostReconciler(nil)
	cr.RecordRoundCost(1, "a", 0.1, "round 1 arg")
	cr.RecordRoundCost(2, "a", 0.2, "round 2 arg")
	cr.RecordRoundCost(3, "a", 0.3, "round 3 arg")

	report := cr.ApplyTieBreakCost(1.0, "a", "b")

	if len(report.DebateCosts) != 3 {
		t.Errorf("expected 3 debate cost entries, got %d", len(report.DebateCosts))
	}
	// Check order is preserved
	if report.DebateCosts[0].Round != 1 || report.DebateCosts[2].Round != 3 {
		t.Error("debate cost order not preserved")
	}
}

// --- Finalize tests ---

func TestFinalize_NoTieBreak(t *testing.T) {
	budgets := []AgentBudget{
		{AgentName: "a", BudgetWeekly: 100, BudgetUsed: 10},
		{AgentName: "b", BudgetWeekly: 100, BudgetUsed: 20},
	}
	cr := NewCostReconciler(budgets)
	cr.RecordRoundCost(1, "a", 0.5, "round 1")
	cr.RecordRoundCost(1, "b", 0.3, "round 1")
	cr.RecordRoundCost(2, "a", 0.2, "round 2")

	report := cr.Finalize()

	if report.TieBreakCost != 0 {
		t.Errorf("tie break cost = %.2f, want 0 (no tie-break)", report.TieBreakCost)
	}
	if report.TotalCost != 1.0 {
		t.Errorf("total = %.2f, want 1.0 (debate only)", report.TotalCost)
	}
	if report.Escalated {
		t.Error("should not escalate without tie-break")
	}
	bdA := report.Breakdowns["a"]
	if bdA.Total != 0.7 {
		t.Errorf("agent-a total = %.2f, want 0.7", bdA.Total)
	}
	bdB := report.Breakdowns["b"]
	if bdB.Total != 0.3 {
		t.Errorf("agent-b total = %.2f, want 0.3", bdB.Total)
	}
}

func TestFinalize_WithBudgetOverrun(t *testing.T) {
	budgets := []AgentBudget{
		{AgentName: "a", BudgetWeekly: 5, BudgetUsed: 4.5},
	}
	cr := NewCostReconciler(budgets)
	cr.RecordRoundCost(1, "a", 1.0, "round 1")

	report := cr.Finalize()

	bdA := report.Breakdowns["a"]
	if !bdA.Overrun {
		t.Error("agent-a should be flagged as overrun")
	}
}

// --- Integration with existing SplitCost ---

func TestCostReconciler_SplitCostConsistency(t *testing.T) {
	cr := NewCostReconciler(nil)
	cr.RecordTieBreakCost(100.0)
	a, b := cr.TieBreakShares("x", "y")
	sa, sb := SplitCost(100.0)

	if a != sa || b != sb {
		t.Errorf("TieBreakShares (%.4f, %.4f) != SplitCost (%.4f, %.4f)", a, b, sa, sb)
	}
}

// --- Full negotiation cost flow ---

func TestFullNegotiationCostFlow_WithTieBreak(t *testing.T) {
	budgets := []AgentBudget{
		{AgentName: "alpha", BudgetWeekly: 50, BudgetUsed: 5},
		{AgentName: "beta", BudgetWeekly: 50, BudgetUsed: 10},
	}
	cr := NewCostReconciler(budgets)

	// 3 rounds of debate
	cr.RecordRoundCost(1, "alpha", 0.50, "round 1")
	cr.RecordRoundCost(1, "beta", 0.45, "round 1")
	cr.RecordRoundCost(2, "alpha", 0.30, "round 2")
	cr.RecordRoundCost(2, "beta", 0.35, "round 2")
	cr.RecordRoundCost(3, "alpha", 0.40, "round 3")
	cr.RecordRoundCost(3, "beta", 0.38, "round 3")

	// Tie-break
	report := cr.ApplyTieBreakCost(5.00, "alpha", "beta")

	if report.Escalated {
		t.Error("should not escalate — both agents have sufficient budget")
	}

	// alpha: debate (1.20) + tie-break (2.50) = 3.70
	bdAlpha := report.Breakdowns["alpha"]
	if bdAlpha.DebateTotal != 1.20 {
		t.Errorf("alpha debate = %.2f, want 1.20", bdAlpha.DebateTotal)
	}
	if bdAlpha.TieBreakShare != 2.50 {
		t.Errorf("alpha tie break = %.2f, want 2.50", bdAlpha.TieBreakShare)
	}
	if bdAlpha.Total != 3.70 {
		t.Errorf("alpha total = %.2f, want 3.70", bdAlpha.Total)
	}

	// beta: debate (1.18) + tie-break (2.50) = 3.68
	bdBeta := report.Breakdowns["beta"]
	if bdBeta.DebateTotal != 1.18 {
		t.Errorf("beta debate = %.2f, want 1.18", bdBeta.DebateTotal)
	}
	if bdBeta.TieBreakShare != 2.50 {
		t.Errorf("beta tie break = %.2f, want 2.50", bdBeta.TieBreakShare)
	}
	if bdBeta.Total != 3.68 {
		t.Errorf("beta total = %.2f, want 3.68", bdBeta.Total)
	}

	// Total: debate (2.38) + tie-break (5.00) = 7.38
	if report.TotalCost != 7.38 {
		t.Errorf("total cost = %.2f, want 7.38", report.TotalCost)
	}
}

func TestFullNegotiationCostFlow_Concession(t *testing.T) {
	budgets := []AgentBudget{
		{AgentName: "alpha", BudgetWeekly: 50, BudgetUsed: 5},
		{AgentName: "beta", BudgetWeekly: 50, BudgetUsed: 10},
	}
	cr := NewCostReconciler(budgets)

	// Agent concedes in round 2 — no tie-break needed
	cr.RecordRoundCost(1, "alpha", 0.50, "round 1")
	cr.RecordRoundCost(1, "beta", 0.45, "round 1")
	cr.RecordRoundCost(2, "alpha", 0.30, "round 2")
	cr.RecordRoundCost(2, "beta", 0.35, "round 2 (concede)")

	report := cr.Finalize()

	if report.TieBreakCost != 0 {
		t.Errorf("tie break cost should be 0 for concession, got %.2f", report.TieBreakCost)
	}
	if report.Escalated {
		t.Error("concession should not escalate")
	}
	// Total: debate costs only
	expectedTotal := 0.50 + 0.45 + 0.30 + 0.35
	if report.TotalCost != expectedTotal {
		t.Errorf("total = %.2f, want %.2f", report.TotalCost, expectedTotal)
	}
}

// --- Edge cases ---

func TestRoundTo2(t *testing.T) {
	tests := []struct {
		input    float64
		expected float64
	}{
		{0.005, 0.01}, // rounds up
		{0.004, 0.0},  // rounds down
		{1.235, 1.24}, // rounds up
		{1.234, 1.23}, // rounds down
		{0, 0},
		{100.999, 101.0},
	}
	for _, tc := range tests {
		got := roundTo2(tc.input)
		if got != tc.expected {
			t.Errorf("roundTo2(%.4f) = %.4f, want %.4f", tc.input, got, tc.expected)
		}
	}
}

func TestJoinStrings(t *testing.T) {
	tests := []struct {
		parts    []string
		sep      string
		expected string
	}{
		{[]string{"a"}, ", ", "a"},
		{[]string{"a", "b"}, ", ", "a, b"},
		{[]string{"a", "b", "c"}, ", ", "a, b, c"},
		{[]string{}, ", ", ""},
		{[]string{"x", "y"}, "|", "x|y"},
	}
	for _, tc := range tests {
		got := joinStrings(tc.parts, tc.sep)
		if got != tc.expected {
			t.Errorf("joinStrings(%v, %q) = %q, want %q", tc.parts, tc.sep, got, tc.expected)
		}
	}
}

func TestCostReport_OverrunAgentsSorted(t *testing.T) {
	budgets := []AgentBudget{
		{AgentName: "zeta", BudgetWeekly: 1, BudgetUsed: 0},
		{AgentName: "alpha", BudgetWeekly: 1, BudgetUsed: 0},
	}
	cr := NewCostReconciler(budgets)
	cr.RecordRoundCost(1, "zeta", 0.5, "round 1")
	cr.RecordRoundCost(1, "alpha", 0.5, "round 1")

	report := cr.ApplyTieBreakCost(10.0, "zeta", "alpha")

	if len(report.OverrunAgents) != 2 {
		t.Fatalf("expected 2 overrun agents, got %d", len(report.OverrunAgents))
	}
	// Should be sorted alphabetically
	if report.OverrunAgents[0] != "alpha" || report.OverrunAgents[1] != "zeta" {
		t.Errorf("expected [alpha, zeta], got %v", report.OverrunAgents)
	}
}

func TestApplyTieBreakCost_ReasonContainsInfo(t *testing.T) {
	budgets := []AgentBudget{
		{AgentName: "a", BudgetWeekly: 1, BudgetUsed: 0},
		{AgentName: "b", BudgetWeekly: 100, BudgetUsed: 0},
	}
	cr := NewCostReconciler(budgets)
	report := cr.ApplyTieBreakCost(5.0, "a", "b")

	if !report.Escalated {
		t.Fatal("expected escalation")
	}
	if report.Reason == "" {
		t.Error("reason should not be empty on escalation")
	}
	// Reason should mention agent name
	if report.Reason == "" || !contains(report.Reason, "a") {
		t.Error("reason should mention the overrun agent")
	}
}
