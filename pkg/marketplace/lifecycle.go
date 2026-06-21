package marketplace

import (
	"fmt"
)

// Agent lifecycle management (spec §10).
//
// Agents progress through three states: active → deprecated → retired.
// Auto-deprecation rules identify agents that should be phased out based on
// trust, activity, and budget criteria. Reactivation restores a deprecated
// agent to active status.

// AutoDeprecationRules scans all active agents and auto-deprecates those
// meeting the criteria in spec §10.2:
//
//   - Trust score < 20 (stub: without the "30 consecutive days" window — the
//     real implementation tracks when trust first dropped below threshold)
//   - No completed tasks in 90 days (stub: proxied by zero tasks AND trust < 30)
//   - Budget exhausted for 14+ consecutive days (stub: proxied by
//     average_task_cost >= weekly_limit, meaning each task exceeds the budget)
//
// Returns the list of agent names that were auto-deprecated.
func (r *Registry) AutoDeprecationRules() ([]string, error) {
	var deprecated []string
	for _, a := range r.agents {
		if a.Status != StatusActive {
			continue
		}

		shouldDeprecate := false

		// Rule 1 (spec §10.2): trust < 20.
		if a.TrustScore < 20 {
			shouldDeprecate = true
		}

		// Rule 2 (spec §10.2): no completed tasks + low trust.
		// Stub proxy — the real check needs task timestamps (90-day window).
		if a.Performance.TasksCompleted == 0 && a.TrustScore < 30 {
			shouldDeprecate = true
		}

		// Rule 3 (spec §10.2): budget exhausted.
		// Stub proxy — average cost exceeds weekly limit means no headroom.
		if a.Budget.WeeklyLimit > 0 && a.Budget.AverageTaskCost >= a.Budget.WeeklyLimit {
			shouldDeprecate = true
		}

		if shouldDeprecate {
			a.Status = StatusDeprecated
			a.DeprecatedAt = nowISO()
			a.UpdatedAt = nowISO()
			deprecated = append(deprecated, a.Name)
		}
	}
	return deprecated, nil
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
	a.UpdatedAt = nowISO()
	return nil
}
