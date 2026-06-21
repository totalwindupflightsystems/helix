package estimate

import (
	"testing"
)

// -----------------------------------------------------------------------------
// 1. RemainingBudget
// -----------------------------------------------------------------------------

func TestRemainingBudget(t *testing.T) {
	cases := []struct {
		name string
		b    BudgetInfo
		want float64
	}{
		{"weekly_100_used_30", BudgetInfo{BudgetWeekly: 100, BudgetUsed: 30}, 70},
		{"weekly_100_used_100", BudgetInfo{BudgetWeekly: 100, BudgetUsed: 100}, 0},
		{"weekly_100_used_150_clamped", BudgetInfo{BudgetWeekly: 100, BudgetUsed: 150}, 0},
		{"weekly_0_used_0", BudgetInfo{BudgetWeekly: 0, BudgetUsed: 0}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RemainingBudget(tc.b); got != tc.want {
				t.Errorf("RemainingBudget() = %v, want %v", got, tc.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 2. IsNewAgent
// -----------------------------------------------------------------------------

func TestIsNewAgent(t *testing.T) {
	cases := []struct {
		name  string
		tasks int
		want  bool
	}{
		{"zero_tasks", 0, true},
		{"five_tasks", 5, true},
		{"nine_tasks", 9, true},
		{"ten_tasks", 10, false},
		{"hundred_tasks", 100, false},
		{"negative_tasks", -1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := BudgetInfo{TasksCompleted: tc.tasks}
			if got := IsNewAgent(b); got != tc.want {
				t.Errorf("IsNewAgent() = %v, want %v", got, tc.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 3. CheckBudget
// -----------------------------------------------------------------------------

func TestCheckBudget(t *testing.T) {
	cases := []struct {
		name         string
		budget       BudgetInfo
		cost         float64
		wantApproved bool
		wantStatus   ApprovalStatus
	}{
		{
			name:         "AC-1_fits_within_remaining",
			budget:       BudgetInfo{AgentName: "test", BudgetWeekly: 100, BudgetUsed: 0, TrustLevel: 0},
			cost:         50,
			wantApproved: true,
			wantStatus:   StatusAutoApproved,
		},
		{
			name:         "AC-2_exactly_remaining",
			budget:       BudgetInfo{AgentName: "test", BudgetWeekly: 100, BudgetUsed: 50, TrustLevel: 0},
			cost:         50,
			wantApproved: true,
			wantStatus:   StatusAutoApproved,
		},
		{
			name:         "AC-3_within_1_5x_with_trust_70",
			budget:       BudgetInfo{AgentName: "test", BudgetWeekly: 100, BudgetUsed: 80, TrustLevel: 70},
			cost:         30,
			wantApproved: true,
			wantStatus:   StatusAutoApprovedWithWarning,
		},
		{
			name:         "AC-4_within_1_5x_but_trust_too_low",
			budget:       BudgetInfo{AgentName: "test", BudgetWeekly: 100, BudgetUsed: 80, TrustLevel: 50},
			cost:         30,
			wantApproved: false,
			wantStatus:   StatusBlocked,
		},
		{
			name:         "AC-5_within_1_5x_with_trust_80",
			budget:       BudgetInfo{AgentName: "test", BudgetWeekly: 100, BudgetUsed: 50, TrustLevel: 80},
			cost:         60,
			wantApproved: true,
			wantStatus:   StatusAutoApprovedWithWarning,
		},
		{
			name:         "AC-6_zero_weekly_cap_blocks",
			budget:       BudgetInfo{AgentName: "test", BudgetWeekly: 0, BudgetUsed: 0, TrustLevel: 0},
			cost:         50,
			wantApproved: false,
			wantStatus:   StatusBlocked,
		},
		{
			name:         "AC-7_exceeds_weekly_cap_escalates",
			budget:       BudgetInfo{AgentName: "test", BudgetWeekly: 100, BudgetUsed: 0, TrustLevel: 0},
			cost:         150,
			wantApproved: false,
			wantStatus:   StatusEscalated,
		},
		{
			name:         "AC-8_over_remaining_with_low_trust_blocks",
			budget:       BudgetInfo{AgentName: "test", BudgetWeekly: 100, BudgetUsed: 90, TrustLevel: 50},
			cost:         20,
			wantApproved: false,
			wantStatus:   StatusBlocked,
		},
		{
			name:         "AC-9_float_epsilon_over_remaining",
			budget:       BudgetInfo{AgentName: "test", BudgetWeekly: 100, BudgetUsed: 99.99, TrustLevel: 50},
			cost:         0.02,
			wantApproved: false,
			wantStatus:   StatusBlocked,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			est := CostEstimate{CostTotal: tc.cost}
			d := CheckBudget(tc.budget, est)
			if d.Approved != tc.wantApproved {
				t.Errorf("Approved = %v, want %v (status=%q, reason=%q)",
					d.Approved, tc.wantApproved, d.Status, d.Reason)
			}
			if d.Status != tc.wantStatus {
				t.Errorf("Status = %q, want %q (reason=%q)",
					d.Status, tc.wantStatus, d.Reason)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 4. ApprovalExitCode
// -----------------------------------------------------------------------------

func TestApprovalExitCode(t *testing.T) {
	cases := []struct {
		name string
		d    ApprovalDecision
		want int
	}{
		{"auto_approved", ApprovalDecision{Status: StatusAutoApproved}, ExitSuccess},
		{"auto_approved_with_warning", ApprovalDecision{Status: StatusAutoApprovedWithWarning}, ExitSuccess},
		{"escalated", ApprovalDecision{Status: StatusEscalated}, ExitExceedsWeeklyCap},
		{"blocked", ApprovalDecision{Status: StatusBlocked}, ExitBudgetExhausted},
		{"unknown_status_falls_through_default", ApprovalDecision{Status: ApprovalStatus("UNKNOWN")}, ExitBudgetExhausted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ApprovalExitCode(tc.d); got != tc.want {
				t.Errorf("ApprovalExitCode() = %d, want %d", got, tc.want)
			}
		})
	}
}
