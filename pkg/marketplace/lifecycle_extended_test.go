package marketplace

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// TestShouldAutoDeprecate — spec §10.2 per-agent evaluation
// ---------------------------------------------------------------------------

func TestShouldAutoDeprecate(t *testing.T) {
	now := parseTimestamp("2026-06-30T12:00:00Z")
	daysAgo := func(days int) string {
		return now.AddDate(0, 0, -days).Format("2006-01-02T15:04:05Z")
	}

	t.Run("nil_agent", func(t *testing.T) {
		d := ShouldAutoDeprecate(nil, now)
		if d.ShouldDeprecate {
			t.Error("nil agent should not deprecate")
		}
	})

	t.Run("non_active_agent", func(t *testing.T) {
		a := &Agent{Name: "a", Status: StatusDeprecated, TrustScore: 5, History: AgentHistory{TrustDroppedAt: daysAgo(60)}}
		d := ShouldAutoDeprecate(a, now)
		if d.ShouldDeprecate {
			t.Error("non-active agent should not deprecate")
		}
	})

	t.Run("rule1_trust_low_30d_triggers", func(t *testing.T) {
		a := &Agent{
			Name:        "a",
			Status:      StatusActive,
			TrustScore:  15,
			Performance: Performance{TasksCompleted: 1},
			History:     AgentHistory{TrustDroppedAt: daysAgo(30)},
		}
		d := ShouldAutoDeprecate(a, now)
		if !d.ShouldDeprecate {
			t.Error("trust < 20 for 30d should deprecate")
		}
		if d.Reason != ReasonTrustLowWindow {
			t.Errorf("Reason = %s, want %s", d.Reason, ReasonTrustLowWindow)
		}
	})

	t.Run("rule1_trust_low_only_29d_not_triggers", func(t *testing.T) {
		a := &Agent{
			Name:        "a",
			Status:      StatusActive,
			TrustScore:  15,
			Performance: Performance{TasksCompleted: 1},
			History:     AgentHistory{TrustDroppedAt: daysAgo(29)},
		}
		d := ShouldAutoDeprecate(a, now)
		if d.ShouldDeprecate {
			t.Error("trust < 20 for only 29d should NOT deprecate")
		}
	})

	t.Run("rule1_trust_low_no_timestamp_not_triggers", func(t *testing.T) {
		a := &Agent{
			Name:        "a",
			Status:      StatusActive,
			TrustScore:  15,
			Performance: Performance{TasksCompleted: 1},
		}
		d := ShouldAutoDeprecate(a, now)
		if d.ShouldDeprecate {
			t.Error("trust < 20 without TrustDroppedAt should NOT deprecate")
		}
	})

	t.Run("rule1_trust_at_20_not_triggers", func(t *testing.T) {
		a := &Agent{
			Name:        "a",
			Status:      StatusActive,
			TrustScore:  20,
			Performance: Performance{TasksCompleted: 1},
			History:     AgentHistory{TrustDroppedAt: daysAgo(60)},
		}
		d := ShouldAutoDeprecate(a, now)
		if d.ShouldDeprecate {
			t.Error("trust = 20 should NOT trigger rule 1 (threshold is < 20)")
		}
	})

	t.Run("rule2_no_tasks_90d_triggers", func(t *testing.T) {
		a := &Agent{
			Name:        "a",
			Status:      StatusActive,
			TrustScore:  50,
			Performance: Performance{TasksCompleted: 0},
			CreatedAt:   daysAgo(90),
		}
		d := ShouldAutoDeprecate(a, now)
		if !d.ShouldDeprecate {
			t.Error("0 tasks for 90d should deprecate")
		}
		if d.Reason != ReasonTaskInactivity {
			t.Errorf("Reason = %s, want %s", d.Reason, ReasonTaskInactivity)
		}
	})

	t.Run("rule2_no_tasks_89d_not_triggers", func(t *testing.T) {
		a := &Agent{
			Name:        "a",
			Status:      StatusActive,
			TrustScore:  50,
			Performance: Performance{TasksCompleted: 0},
			CreatedAt:   daysAgo(89),
		}
		d := ShouldAutoDeprecate(a, now)
		if d.ShouldDeprecate {
			t.Error("0 tasks for only 89d should NOT deprecate")
		}
	})

	t.Run("rule2_last_task_90d_ago_triggers", func(t *testing.T) {
		a := &Agent{
			Name:        "a",
			Status:      StatusActive,
			TrustScore:  50,
			Performance: Performance{TasksCompleted: 10},
			History:     AgentHistory{LastTaskCompletedAt: daysAgo(90)},
		}
		d := ShouldAutoDeprecate(a, now)
		if !d.ShouldDeprecate {
			t.Error("last task 90d ago should deprecate")
		}
	})

	t.Run("rule2_last_task_89d_ago_not_triggers", func(t *testing.T) {
		a := &Agent{
			Name:        "a",
			Status:      StatusActive,
			TrustScore:  50,
			Performance: Performance{TasksCompleted: 10},
			History:     AgentHistory{LastTaskCompletedAt: daysAgo(89)},
		}
		d := ShouldAutoDeprecate(a, now)
		if d.ShouldDeprecate {
			t.Error("last task 89d ago should NOT deprecate")
		}
	})

	t.Run("rule2_zero_tasks_no_createdAt_not_triggers", func(t *testing.T) {
		a := &Agent{
			Name:        "a",
			Status:      StatusActive,
			TrustScore:  50,
			Performance: Performance{TasksCompleted: 0},
		}
		d := ShouldAutoDeprecate(a, now)
		if d.ShouldDeprecate {
			t.Error("0 tasks without CreatedAt should NOT deprecate")
		}
	})

	t.Run("rule3_budget_exhausted_14d_triggers", func(t *testing.T) {
		a := &Agent{
			Name:       "a",
			Status:     StatusActive,
			TrustScore: 50,
			Budget:     Budget{WeeklyLimit: 10, AverageTaskCost: 12},
			History:    AgentHistory{BudgetExhaustedAt: daysAgo(14)},
		}
		d := ShouldAutoDeprecate(a, now)
		if !d.ShouldDeprecate {
			t.Error("budget exhausted for 14d should deprecate")
		}
		if d.Reason != ReasonBudgetExhausted {
			t.Errorf("Reason = %s, want %s", d.Reason, ReasonBudgetExhausted)
		}
	})

	t.Run("rule3_budget_exhausted_13d_not_triggers", func(t *testing.T) {
		a := &Agent{
			Name:       "a",
			Status:     StatusActive,
			TrustScore: 50,
			Budget:     Budget{WeeklyLimit: 10, AverageTaskCost: 12},
			History:    AgentHistory{BudgetExhaustedAt: daysAgo(13)},
		}
		d := ShouldAutoDeprecate(a, now)
		if d.ShouldDeprecate {
			t.Error("budget exhausted for only 13d should NOT deprecate")
		}
	})

	t.Run("rule3_budget_exhausted_no_timestamp_not_triggers", func(t *testing.T) {
		a := &Agent{
			Name:       "a",
			Status:     StatusActive,
			TrustScore: 50,
			Budget:     Budget{WeeklyLimit: 10, AverageTaskCost: 12},
		}
		d := ShouldAutoDeprecate(a, now)
		if d.ShouldDeprecate {
			t.Error("budget exhausted without timestamp should NOT deprecate")
		}
	})

	t.Run("rule3_budget_not_exhausted_not_triggers", func(t *testing.T) {
		a := &Agent{
			Name:       "a",
			Status:     StatusActive,
			TrustScore: 50,
			Budget:     Budget{WeeklyLimit: 100, AverageTaskCost: 10},
			History:    AgentHistory{BudgetExhaustedAt: daysAgo(30)},
		}
		d := ShouldAutoDeprecate(a, now)
		if d.ShouldDeprecate {
			t.Error("budget not exhausted should NOT deprecate even with old timestamp")
		}
	})

	t.Run("all_healthy_not_deprecated", func(t *testing.T) {
		a := &Agent{
			Name:        "a",
			Status:      StatusActive,
			TrustScore:  85,
			Performance: Performance{TasksCompleted: 100},
			Budget:      Budget{WeeklyLimit: 100, AverageTaskCost: 5},
			History:     AgentHistory{LastTaskCompletedAt: daysAgo(1)},
			CreatedAt:   daysAgo(365),
		}
		d := ShouldAutoDeprecate(a, now)
		if d.ShouldDeprecate {
			t.Error("healthy agent should NOT deprecate")
		}
		if d.Reason != ReasonNone {
			t.Errorf("Reason = %s, want empty", d.Reason)
		}
	})
}

// ---------------------------------------------------------------------------
// TestShouldReactivate — spec §10.3 auto-reactivation
// ---------------------------------------------------------------------------

func TestShouldReactivate(t *testing.T) {
	now := parseTimestamp("2026-06-30T12:00:00Z")
	daysAgo := func(days int) string {
		return now.AddDate(0, 0, -days).Format("2006-01-02T15:04:05Z")
	}

	t.Run("nil_agent", func(t *testing.T) {
		if ShouldReactivate(nil, now) {
			t.Error("nil agent should not reactivate")
		}
	})

	t.Run("active_agent_not_reactivated", func(t *testing.T) {
		a := &Agent{Name: "a", Status: StatusActive, TrustScore: 85}
		if ShouldReactivate(a, now) {
			t.Error("active agent should not be reactivated")
		}
	})

	t.Run("deprecated_trust_recovered_7d", func(t *testing.T) {
		a := &Agent{
			Name:       "a",
			Status:     StatusDeprecated,
			TrustScore: 50,
			History:    AgentHistory{TrustRecoveredAt: daysAgo(7)},
		}
		if !ShouldReactivate(a, now) {
			t.Error("deprecated agent with trust > 20 for 7d should be reactivated")
		}
	})

	t.Run("deprecated_trust_recovered_only_6d", func(t *testing.T) {
		a := &Agent{
			Name:       "a",
			Status:     StatusDeprecated,
			TrustScore: 50,
			History:    AgentHistory{TrustRecoveredAt: daysAgo(6)},
		}
		if ShouldReactivate(a, now) {
			t.Error("deprecated agent with trust > 20 for only 6d should NOT be reactivated")
		}
	})

	t.Run("deprecated_trust_still_low_not_reactivated", func(t *testing.T) {
		a := &Agent{
			Name:       "a",
			Status:     StatusDeprecated,
			TrustScore: 15,
			History:    AgentHistory{TrustRecoveredAt: daysAgo(30)},
		}
		if ShouldReactivate(a, now) {
			t.Error("deprecated agent with trust still low should NOT be reactivated")
		}
	})

	t.Run("deprecated_budget_replenished", func(t *testing.T) {
		a := &Agent{
			Name:       "a",
			Status:     StatusDeprecated,
			TrustScore: 15,
			Budget:     Budget{WeeklyLimit: 100, AverageTaskCost: 5},
			History:    AgentHistory{BudgetExhaustedAt: daysAgo(14)},
		}
		if !ShouldReactivate(a, now) {
			t.Error("deprecated agent with budget replenished should be reactivated")
		}
	})

	t.Run("deprecated_no_recovery_signal", func(t *testing.T) {
		a := &Agent{
			Name:       "a",
			Status:     StatusDeprecated,
			TrustScore: 15,
			Budget:     Budget{WeeklyLimit: 10, AverageTaskCost: 12},
			History:    AgentHistory{BudgetExhaustedAt: daysAgo(30)},
		}
		if ShouldReactivate(a, now) {
			t.Error("deprecated agent with no recovery signal should NOT be reactivated")
		}
	})
}

// ---------------------------------------------------------------------------
// TestAutoReactivationRules — batch reactivation
// ---------------------------------------------------------------------------

func TestAutoReactivationRules(t *testing.T) {
	now := parseTimestamp("2026-06-30T12:00:00Z")
	daysAgo := func(days int) string {
		return now.AddDate(0, 0, -days).Format("2006-01-02T15:04:05Z")
	}

	agents := map[string]*Agent{
		"recovered": {
			Name:       "recovered",
			Status:     StatusDeprecated,
			TrustScore: 50,
			History:    AgentHistory{TrustRecoveredAt: daysAgo(10)},
		},
		"still-low": {
			Name:       "still-low",
			Status:     StatusDeprecated,
			TrustScore: 15,
		},
		"budget-fixed": {
			Name:       "budget-fixed",
			Status:     StatusDeprecated,
			TrustScore: 15,
			Budget:     Budget{WeeklyLimit: 100, AverageTaskCost: 5},
			History:    AgentHistory{BudgetExhaustedAt: daysAgo(14)},
		},
		"not-deprecated": {
			Name:       "not-deprecated",
			Status:     StatusActive,
			TrustScore: 85,
		},
	}

	r := &Registry{agents: agents}
	reactivated, err := r.AutoReactivationRulesAt(now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(reactivated) != 2 {
		t.Fatalf("expected 2 reactivated, got %d: %v", len(reactivated), reactivated)
	}

	// Verify recovered and budget-fixed are reactivated.
	gotSet := make(map[string]bool)
	for _, name := range reactivated {
		gotSet[name] = true
	}
	if !gotSet["recovered"] {
		t.Error("expected 'recovered' to be reactivated")
	}
	if !gotSet["budget-fixed"] {
		t.Error("expected 'budget-fixed' to be reactivated")
	}

	// Verify statuses are updated.
	if agents["recovered"].Status != StatusActive {
		t.Error("'recovered' should be active after reactivation")
	}
	if agents["recovered"].DeprecatedAt != "" {
		t.Error("'recovered' DeprecatedAt should be cleared")
	}
	if agents["recovered"].History.TrustRecoveredAt != "" {
		t.Error("'recovered' TrustRecoveredAt should be cleared after reactivation")
	}
	if agents["budget-fixed"].History.BudgetExhaustedAt != "" {
		t.Error("'budget-fixed' BudgetExhaustedAt should be cleared after reactivation")
	}
}

// ---------------------------------------------------------------------------
// TestUpdateTrustHistory — cron calls
// ---------------------------------------------------------------------------

func TestUpdateTrustHistory(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)

	t.Run("sets_trust_dropped_at_when_below_threshold", func(t *testing.T) {
		a := &Agent{Name: "a", Status: StatusActive, TrustScore: 50}
		UpdateTrustHistory(a, 15, now)
		if a.History.TrustDroppedAt == "" {
			t.Error("TrustDroppedAt should be set")
		}
	})

	t.Run("does_not_overwrite_trust_dropped_at", func(t *testing.T) {
		original := "2026-05-01T00:00:00Z"
		a := &Agent{
			Name:       "a",
			Status:     StatusActive,
			TrustScore: 50,
			History:    AgentHistory{TrustDroppedAt: original},
		}
		UpdateTrustHistory(a, 10, now)
		if a.History.TrustDroppedAt != original {
			t.Error("TrustDroppedAt should not be overwritten if already set")
		}
	})

	t.Run("clears_trust_dropped_at_when_recovered", func(t *testing.T) {
		a := &Agent{
			Name:       "a",
			Status:     StatusActive,
			TrustScore: 10,
			History:    AgentHistory{TrustDroppedAt: "2026-05-01T00:00:00Z"},
		}
		UpdateTrustHistory(a, 50, now)
		if a.History.TrustDroppedAt != "" {
			t.Error("TrustDroppedAt should be cleared when trust recovers")
		}
	})

	t.Run("sets_trust_recovered_at_for_deprecated_agent", func(t *testing.T) {
		a := &Agent{
			Name:       "a",
			Status:     StatusDeprecated,
			TrustScore: 10,
		}
		UpdateTrustHistory(a, 50, now)
		if a.History.TrustRecoveredAt == "" {
			t.Error("TrustRecoveredAt should be set for deprecated agent with recovered trust")
		}
	})

	t.Run("clears_trust_recovered_at_when_trust_drops_again", func(t *testing.T) {
		a := &Agent{
			Name:       "a",
			Status:     StatusDeprecated,
			TrustScore: 50,
			History:    AgentHistory{TrustRecoveredAt: "2026-06-20T00:00:00Z"},
		}
		UpdateTrustHistory(a, 10, now)
		if a.History.TrustRecoveredAt != "" {
			t.Error("TrustRecoveredAt should be cleared when trust drops again")
		}
	})

	t.Run("nil_agent_no_panic", func(t *testing.T) {
		UpdateTrustHistory(nil, 10, now)
	})
}

// ---------------------------------------------------------------------------
// TestMarkTaskCompleted
// ---------------------------------------------------------------------------

func TestMarkTaskCompleted(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)

	t.Run("sets_last_task_completed_at", func(t *testing.T) {
		a := &Agent{Name: "a", Status: StatusActive}
		MarkTaskCompleted(a, now)
		if a.History.LastTaskCompletedAt == "" {
			t.Error("LastTaskCompletedAt should be set")
		}
	})

	t.Run("increments_tasks_completed", func(t *testing.T) {
		a := &Agent{Name: "a", Status: StatusActive, Performance: Performance{TasksCompleted: 5}}
		MarkTaskCompleted(a, now)
		if a.Performance.TasksCompleted != 6 {
			t.Errorf("TasksCompleted = %d, want 6", a.Performance.TasksCompleted)
		}
	})

	t.Run("nil_agent_no_panic", func(t *testing.T) {
		MarkTaskCompleted(nil, now)
	})
}

// ---------------------------------------------------------------------------
// TestUpdateBudgetStatus
// ---------------------------------------------------------------------------

func TestUpdateBudgetStatus(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)

	t.Run("sets_budget_exhausted_at_when_exhausted", func(t *testing.T) {
		a := &Agent{
			Name:   "a",
			Status: StatusActive,
			Budget: Budget{WeeklyLimit: 10, AverageTaskCost: 15},
		}
		UpdateBudgetStatus(a, now)
		if a.History.BudgetExhaustedAt == "" {
			t.Error("BudgetExhaustedAt should be set when budget is exhausted")
		}
	})

	t.Run("clears_budget_exhausted_at_when_replenished", func(t *testing.T) {
		a := &Agent{
			Name:    "a",
			Status:  StatusActive,
			Budget:  Budget{WeeklyLimit: 100, AverageTaskCost: 5},
			History: AgentHistory{BudgetExhaustedAt: "2026-06-01T00:00:00Z"},
		}
		UpdateBudgetStatus(a, now)
		if a.History.BudgetExhaustedAt != "" {
			t.Error("BudgetExhaustedAt should be cleared when budget is replenished")
		}
	})

	t.Run("does_not_set_budget_exhausted_when_zero_limit", func(t *testing.T) {
		a := &Agent{
			Name:   "a",
			Status: StatusActive,
			Budget: Budget{WeeklyLimit: 0, AverageTaskCost: 999},
		}
		UpdateBudgetStatus(a, now)
		if a.History.BudgetExhaustedAt != "" {
			t.Error("BudgetExhaustedAt should NOT be set when WeeklyLimit is 0")
		}
	})

	t.Run("nil_agent_no_panic", func(t *testing.T) {
		UpdateBudgetStatus(nil, now)
	})
}

// ---------------------------------------------------------------------------
// TestParseTimestamp + TestDaysSince
// ---------------------------------------------------------------------------

func TestParseTimestamp(t *testing.T) {
	t.Run("empty string returns zero", func(t *testing.T) {
		if !parseTimestamp("").IsZero() {
			t.Error("empty string should return zero time")
		}
	})

	t.Run("invalid string returns zero", func(t *testing.T) {
		if !parseTimestamp("not-a-timestamp").IsZero() {
			t.Error("invalid string should return zero time")
		}
	})

	t.Run("valid RFC3339", func(t *testing.T) {
		ts := parseTimestamp("2026-06-30T12:00:00Z")
		if ts.IsZero() {
			t.Error("valid RFC3339 should not return zero")
		}
		if ts.Year() != 2026 || ts.Month() != time.June || ts.Day() != 30 {
			t.Errorf("parsed wrong date: %v", ts)
		}
	})
}

func TestDaysSince(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)

	t.Run("zero_time_returns_zero", func(t *testing.T) {
		if daysSince(time.Time{}, now) != 0 {
			t.Error("zero time should return 0")
		}
	})

	t.Run("future_time_returns_zero", func(t *testing.T) {
		future := now.Add(24 * time.Hour)
		if daysSince(future, now) != 0 {
			t.Error("future time should return 0")
		}
	})

	t.Run("exact_days", func(t *testing.T) {
		past := now.AddDate(0, 0, -30)
		if daysSince(past, now) != 30 {
			t.Errorf("expected 30 days, got %d", daysSince(past, now))
		}
	})

	t.Run("partial_day_truncates", func(t *testing.T) {
		past := now.AddDate(0, 0, -1).Add(-1 * time.Hour) // 25 hours ago
		if daysSince(past, now) != 1 {
			t.Errorf("expected 1 day (truncated), got %d", daysSince(past, now))
		}
	})
}

// ---------------------------------------------------------------------------
// TestIsBudgetExhausted
// ---------------------------------------------------------------------------

func TestIsBudgetExhausted(t *testing.T) {
	t.Run("exhausted_when_avg_cost_exceeds_limit", func(t *testing.T) {
		a := &Agent{Budget: Budget{WeeklyLimit: 10, AverageTaskCost: 15}}
		if !isBudgetExhausted(a) {
			t.Error("should be exhausted when avg cost > limit")
		}
	})

	t.Run("exhausted_when_equal", func(t *testing.T) {
		a := &Agent{Budget: Budget{WeeklyLimit: 10, AverageTaskCost: 10}}
		if !isBudgetExhausted(a) {
			t.Error("should be exhausted when avg cost = limit")
		}
	})

	t.Run("not_exhausted", func(t *testing.T) {
		a := &Agent{Budget: Budget{WeeklyLimit: 100, AverageTaskCost: 5}}
		if isBudgetExhausted(a) {
			t.Error("should NOT be exhausted when headroom exists")
		}
	})

	t.Run("not_exhausted_when_zero_limit", func(t *testing.T) {
		a := &Agent{Budget: Budget{WeeklyLimit: 0, AverageTaskCost: 999}}
		if isBudgetExhausted(a) {
			t.Error("should NOT be exhausted when WeeklyLimit is 0")
		}
	})

	t.Run("nil_agent_not_exhausted", func(t *testing.T) {
		if isBudgetExhausted(nil) {
			t.Error("nil agent should not be exhausted")
		}
	})
}
