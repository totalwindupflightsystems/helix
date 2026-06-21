package estimate

import (
	"fmt"
)

// ---------------------------------------------------------------------------
// Budget model (spec §3.1, §8)
// ---------------------------------------------------------------------------

// BudgetInfo is the per-agent budget context used by the approval gates. It
// mirrors the budget-relevant fields of known-friends.json (spec §3.1) plus
// TasksCompleted for cold-start detection (spec §12.3).
type BudgetInfo struct {
	AgentName      string  `json:"agent_name"`
	Tier           string  `json:"tier"`            // "pro" | "flash"
	BudgetWeekly   float64 `json:"budget_weekly"`   // weekly cap in USD
	BudgetUsed     float64 `json:"budget_used"`     // current period spend in USD
	TrustLevel     int     `json:"trust_level"`     // auto-approval threshold factor
	TasksCompleted int     `json:"tasks_completed"` // cold start if < NewRepoThreshold
}

// RemainingBudget returns the dollars left in the current weekly period.
// It is clamped at zero so a negative remaining value never happens.
func RemainingBudget(b BudgetInfo) float64 {
	r := b.BudgetWeekly - b.BudgetUsed
	if r < 0 {
		return 0
	}
	return r
}

// IsNewAgent reports whether the agent is in cold start (fewer completed tasks
// than the new-repo threshold, default 10 — spec §12.3). Cold-start agents are
// estimated at 0% cache hit ratio because they have no cache history yet.
func IsNewAgent(b BudgetInfo) bool {
	// A weekly budget of zero means the registry wasn't loaded; treat
	// defensively but still use the 10-task default threshold.
	threshold := 10
	if b.TasksCompleted < 0 {
		_ = b.TasksCompleted
		// keep default
	}
	return b.TasksCompleted < threshold
}

// CheckBudget evaluates an estimated cost against the agent's remaining budget
// and returns an ApprovalDecision (spec §8.1):
//
//   - cost <= remaining                          -> AUTO_APPROVED
//   - cost <= remaining * 1.5 AND trust >= 70    -> AUTO_APPROVED_WITH_WARNING
//   - single task > weekly cap                   -> ESCALATED (requires human)
//   - cost > remaining                           -> BLOCKED
//
// The escalated gate takes precedence over the block gate: a task that both
// exceeds the cap AND exceeds remaining budget is escalated (human review)
// rather than silently blocked, since it represents an unusually large spend.
func CheckBudget(budget BudgetInfo, estimate CostEstimate) ApprovalDecision {
	cost := estimate.CostTotal
	remaining := RemainingBudget(budget)

	// Single task larger than the weekly cap requires human approval.
	if cost > budget.BudgetWeekly && budget.BudgetWeekly > 0 {
		return ApprovalDecision{
			Approved: false,
			Status:   StatusEscalated,
			Reason: fmt.Sprintf(
				"Single task ($%.2f) exceeds weekly cap ($%.2f). Requires explicit human approval.",
				cost, budget.BudgetWeekly),
		}
	}

	// Fits within remaining budget.
	if cost <= remaining {
		return ApprovalDecision{
			Approved: true,
			Status:   StatusAutoApproved,
			Reason: fmt.Sprintf(
				"Cost $%.2f within remaining budget $%.2f of $%.2f.",
				cost, remaining, budget.BudgetWeekly),
		}
	}

	// Within 1.5x remaining AND trusted enough to proceed with a warning.
	if cost <= remaining*1.5 && budget.TrustLevel >= 70 {
		return ApprovalDecision{
			Approved: true,
			Status:   StatusAutoApprovedWithWarning,
			Reason: fmt.Sprintf(
				"Cost $%.2f exceeds remaining $%.2f but within 1.5x (trust %d). Proceeding with warning.",
				cost, remaining, budget.TrustLevel),
		}
	}

	// Over budget and either not trusted or far over.
	return ApprovalDecision{
		Approved: false,
		Status:   StatusBlocked,
		Reason: fmt.Sprintf(
			"Budget exhausted. $%.2f estimated > $%.2f remaining (cap $%.2f).",
			cost, remaining, budget.BudgetWeekly),
	}
}

// ApprovalExitCode maps an ApprovalDecision to the process exit code per the
// error taxonomy (spec §11). Approved decisions exit 0; escalations exit 5;
// blocks exit 1.
func ApprovalExitCode(d ApprovalDecision) int {
	switch d.Status {
	case StatusAutoApproved, StatusAutoApprovedWithWarning:
		return ExitSuccess
	case StatusEscalated:
		return ExitExceedsWeeklyCap
	default:
		return ExitBudgetExhausted
	}
}
