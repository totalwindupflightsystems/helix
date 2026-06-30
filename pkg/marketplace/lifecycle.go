package marketplace

import (
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Agent lifecycle management (spec §10).
// ---------------------------------------------------------------------------

// Spec §10.2 auto-deprecation time windows.
const (
	// Rule 1: trust below threshold for this many consecutive days.
	TrustLowWindowDays = 30
	// TrustLowThreshold is the trust score below which Rule 1 activates.
	TrustLowThreshold = 20

	// Rule 2: no completed tasks for this many consecutive days.
	TaskInactivityWindowDays = 90

	// Rule 3: budget exhausted for this many consecutive days.
	BudgetExhaustedWindowDays = 14

	// ReactivationWindowDays: spec §10.3 — trust must stay above threshold for
	// this many consecutive days to qualify for auto-reactivation.
	ReactivationWindowDays = 7
)

// AgentHistory tracks lifecycle-relevant timestamps for spec §10.2 time-window
// checks. The marketplace cron job (daily recalculation) updates these fields
// when conditions change.
type AgentHistory struct {
	// TrustDroppedAt is when trust first fell below TrustLowThreshold.
	// Cleared when trust recovers above the threshold.
	TrustDroppedAt string `yaml:"trust_dropped_at,omitempty" json:"trust_dropped_at,omitempty"`

	// LastTaskCompletedAt is the timestamp of the most recently completed task.
	// Used for Rule 2 (90-day inactivity window).
	LastTaskCompletedAt string `yaml:"last_task_completed_at,omitempty" json:"last_task_completed_at,omitempty"`

	// BudgetExhaustedAt is when the budget was first observed as exhausted
	// (no headroom for any task). Cleared when budget is replenished.
	BudgetExhaustedAt string `yaml:"budget_exhausted_at,omitempty" json:"budget_exhausted_at,omitempty"`

	// TrustRecoveredAt is when trust first rose above TrustLowThreshold after
	// a deprecation. Used for spec §10.3 auto-reactivation (7-day window).
	TrustRecoveredAt string `yaml:"trust_recovered_at,omitempty" json:"trust_recovered_at,omitempty"`
}

// parseTimestamp parses an RFC3339 timestamp string. Returns zero time on
// empty or parse error.
func parseTimestamp(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// daysSince returns the integer days between ts and now. Returns 0 if ts is
// zero or in the future.
func daysSince(ts time.Time, now time.Time) int {
	if ts.IsZero() {
		return 0
	}
	d := now.Sub(ts)
	if d < 0 {
		return 0
	}
	return int(d.Hours() / 24)
}

// isBudgetExhausted checks whether the agent has no budget headroom (Rule 3
// condition: average task cost exceeds the remaining budget, meaning no task
// can be accepted).
func isBudgetExhausted(a *Agent) bool {
	if a == nil {
		return false
	}
	if a.Budget.WeeklyLimit <= 0 {
		return false
	}
	return a.Budget.AverageTaskCost >= a.Budget.WeeklyLimit
}

// DeprecationReason describes which spec §10.2 rule triggered deprecation.
type DeprecationReason string

const (
	ReasonNone            DeprecationReason = ""
	ReasonTrustLowWindow  DeprecationReason = "trust_below_20_for_30d"
	ReasonTaskInactivity  DeprecationReason = "no_tasks_for_90d"
	ReasonBudgetExhausted DeprecationReason = "budget_exhausted_for_14d"
)

// DeprecationDecision captures the result of ShouldAutoDeprecate.
type DeprecationDecision struct {
	ShouldDeprecate bool              `json:"should_deprecate"`
	Reason          DeprecationReason `json:"reason"`
	Detail          string            `json:"detail"`
}

// ShouldAutoDeprecate evaluates a single agent against all three spec §10.2
// rules with proper time windows. Returns the first matching rule as the
// reason, or an empty decision if no rule triggers.
//
// Rules (spec §10.2):
//  1. Trust score < 20 for 30 consecutive days
//  2. No completed tasks in 90 days
//  3. Budget exhausted for 14 consecutive days
func ShouldAutoDeprecate(a *Agent, now time.Time) DeprecationDecision {
	if a == nil {
		return DeprecationDecision{}
	}
	if a.Status != StatusActive {
		return DeprecationDecision{}
	}

	// Rule 1: trust < 20 for 30 consecutive days.
	if a.TrustScore < TrustLowThreshold {
		droppedAt := parseTimestamp(a.History.TrustDroppedAt)
		if !droppedAt.IsZero() {
			days := daysSince(droppedAt, now)
			if days >= TrustLowWindowDays {
				return DeprecationDecision{
					ShouldDeprecate: true,
					Reason:          ReasonTrustLowWindow,
					Detail:          fmt.Sprintf("trust %d < %d for %d days (>= %d)", a.TrustScore, TrustLowThreshold, days, TrustLowWindowDays),
				}
			}
		}
	}

	// Rule 2: no completed tasks in 90 days.
	// This applies if the agent has a LastTaskCompletedAt timestamp and it's
	// older than 90 days, OR if the agent was created >90 days ago and has
	// zero tasks ever completed.
	if a.Performance.TasksCompleted == 0 {
		createdAt := parseTimestamp(a.CreatedAt)
		if !createdAt.IsZero() {
			days := daysSince(createdAt, now)
			if days >= TaskInactivityWindowDays {
				return DeprecationDecision{
					ShouldDeprecate: true,
					Reason:          ReasonTaskInactivity,
					Detail:          fmt.Sprintf("0 tasks completed, created %d days ago (>= %d)", days, TaskInactivityWindowDays),
				}
			}
		}
	} else {
		// Has completed tasks — check if the last one was >90 days ago.
		lastTask := parseTimestamp(a.History.LastTaskCompletedAt)
		if !lastTask.IsZero() {
			days := daysSince(lastTask, now)
			if days >= TaskInactivityWindowDays {
				return DeprecationDecision{
					ShouldDeprecate: true,
					Reason:          ReasonTaskInactivity,
					Detail:          fmt.Sprintf("last task %d days ago (>= %d)", days, TaskInactivityWindowDays),
				}
			}
		}
	}

	// Rule 3: budget exhausted for 14 consecutive days.
	if isBudgetExhausted(a) {
		exhaustedAt := parseTimestamp(a.History.BudgetExhaustedAt)
		if !exhaustedAt.IsZero() {
			days := daysSince(exhaustedAt, now)
			if days >= BudgetExhaustedWindowDays {
				return DeprecationDecision{
					ShouldDeprecate: true,
					Reason:          ReasonBudgetExhausted,
					Detail:          fmt.Sprintf("budget exhausted for %d days (>= %d)", days, BudgetExhaustedWindowDays),
				}
			}
		}
	}

	return DeprecationDecision{}
}

// AutoDeprecationRules scans all active agents and auto-deprecates those
// meeting the criteria in spec §10.2 using proper time-window checks via
// ShouldAutoDeprecate.
//
// Returns the list of agent names that were auto-deprecated.
func (r *Registry) AutoDeprecationRules() ([]string, error) {
	return r.AutoDeprecationRulesAt(time.Now().UTC())
}

// AutoDeprecationRulesAt is the time-injectable version for testing.
func (r *Registry) AutoDeprecationRulesAt(now time.Time) ([]string, error) {
	var deprecated []string
	for _, a := range r.agents {
		decision := ShouldAutoDeprecate(a, now)
		if decision.ShouldDeprecate {
			a.Status = StatusDeprecated
			a.DeprecatedAt = nowISO()
			a.UpdatedAt = nowISO()
			deprecated = append(deprecated, a.Name)
		}
	}
	return deprecated, nil
}

// ShouldReactivate evaluates whether a deprecated agent qualifies for
// auto-reactivation per spec §10.3: "Trust score rises above 20
// (auto-reactivation after 7 days above threshold)."
//
// Also returns true if the budget has been replenished (spec §10.3: "Budget
// is replenished").
func ShouldReactivate(a *Agent, now time.Time) bool {
	if a == nil {
		return false
	}
	if a.Status != StatusDeprecated {
		return false
	}

	// Auto-reactivation: trust > 20 for 7 consecutive days.
	if a.TrustScore > TrustLowThreshold {
		recoveredAt := parseTimestamp(a.History.TrustRecoveredAt)
		if !recoveredAt.IsZero() {
			days := daysSince(recoveredAt, now)
			if days >= ReactivationWindowDays {
				return true
			}
		}
	}

	// Budget replenished — no longer exhausted.
	if !isBudgetExhausted(a) && a.History.BudgetExhaustedAt != "" {
		return true
	}

	return false
}

// AutoReactivationRules scans all deprecated agents and auto-reactivates those
// meeting spec §10.3 criteria via ShouldReactivate. Returns the list of
// reactivated agent names.
func (r *Registry) AutoReactivationRules() ([]string, error) {
	return r.AutoReactivationRulesAt(time.Now().UTC())
}

// AutoReactivationRulesAt is the time-injectable version for testing.
func (r *Registry) AutoReactivationRulesAt(now time.Time) ([]string, error) {
	var reactivated []string
	for _, a := range r.agents {
		if ShouldReactivate(a, now) {
			a.Status = StatusActive
			a.DeprecatedAt = ""
			a.History.TrustRecoveredAt = ""
			a.History.BudgetExhaustedAt = ""
			a.UpdatedAt = nowISO()
			reactivated = append(reactivated, a.Name)
		}
	}
	return reactivated, nil
}

// Reactivate changes an agent from deprecated back to active (spec §10.3).
// Only deprecated agents can be reactivated. Returns an error if the agent is
// not found or is not in the deprecated state.
func (r *Registry) Reactivate(name string) error {
	a, ok := r.agents[name]
	if !ok {
		return &ExitError{
			Code:    ExitAgentNotFound,
			Message: fmt.Sprintf("AGENT_NOT_FOUND: %s not in marketplace", name),
		}
	}
	if a.Status != StatusDeprecated {
		return fmt.Errorf("agent %s is %s — only deprecated agents can be reactivated", name, a.Status)
	}
	a.Status = StatusActive
	a.DeprecatedAt = ""
	a.History.TrustRecoveredAt = ""
	a.History.BudgetExhaustedAt = ""
	a.UpdatedAt = nowISO()
	return nil
}

// ---------------------------------------------------------------------------
// History tracking methods — called by the daily recalculation cron.
// ---------------------------------------------------------------------------

// UpdateTrustHistory records trust score changes for time-window tracking.
// If trust drops below TrustLowThreshold and TrustDroppedAt is empty, it sets
// the timestamp. If trust recovers above the threshold, it clears
// TrustDroppedAt and sets TrustRecoveredAt (for auto-reactivation window).
func UpdateTrustHistory(a *Agent, newTrust int, now time.Time) {
	if a == nil {
		return
	}

	if newTrust < TrustLowThreshold {
		if a.History.TrustDroppedAt == "" {
			a.History.TrustDroppedAt = now.Format(time.RFC3339)
		}
		// Clear recovery timestamp if trust drops again.
		a.History.TrustRecoveredAt = ""
	} else {
		a.History.TrustDroppedAt = ""
		if a.Status == StatusDeprecated && a.History.TrustRecoveredAt == "" {
			a.History.TrustRecoveredAt = now.Format(time.RFC3339)
		}
	}
}

// MarkTaskCompleted records that the agent completed a task at the given time.
// Updates LastTaskCompletedAt and increments TasksCompleted.
func MarkTaskCompleted(a *Agent, now time.Time) {
	if a == nil {
		return
	}
	a.History.LastTaskCompletedAt = now.Format(time.RFC3339)
	a.Performance.TasksCompleted++
}

// UpdateBudgetStatus tracks budget exhaustion state for time-window checks.
// If the budget is exhausted and BudgetExhaustedAt is empty, it sets the
// timestamp. If the budget is replenished, it clears the timestamp.
func UpdateBudgetStatus(a *Agent, now time.Time) {
	if a == nil {
		return
	}

	if isBudgetExhausted(a) {
		if a.History.BudgetExhaustedAt == "" {
			a.History.BudgetExhaustedAt = now.Format(time.RFC3339)
		}
	} else {
		a.History.BudgetExhaustedAt = ""
	}
}
