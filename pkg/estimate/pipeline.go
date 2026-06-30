package estimate

import (
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Reconciliation pipeline (spec §8.2 steps 1-5)
// ---------------------------------------------------------------------------

// ReconciliationResult captures the full output of a single reconciliation run.
// It records the estimated vs actual costs, the drift percentage, the agent's
// remaining budget AFTER the actual cost is deducted, and whether the drift
// exceeded the 10% threshold (spec §9.2 step 3).
type ReconciliationResult struct {
	AgentID              string  `json:"agent_id"`
	EstimatedCost        float64 `json:"estimated_cost"`
	ActualCost           float64 `json:"actual_cost"`
	DriftPct             float64 `json:"drift_pct"`
	BudgetUsedAfter      float64 `json:"budget_used_after"`
	BudgetRemainingAfter float64 `json:"budget_remaining_after"`
	OverThreshold        bool    `json:"over_threshold"`
	FedToCalibrator      bool    `json:"fed_to_calibrator"`
	Timestamp            string  `json:"timestamp"`
}

// ReconcilePipeline chains the reconciliation steps per spec §8.2:
//  1. Receive GitReins LLMUsage from the evaluator.
//  2. Compute actual cost via ActualCost().
//  3. Update budget_used in the BudgetInfo (returns updated copy).
//  4. Log drift via DriftTracker.RecordDrift.
//  5. Feed DriftTracker into Calibrator for weekly recalibration.
//
// The pipeline does NOT mutate the input BudgetInfo — it returns an updated
// copy with BudgetUsed incremented by the actual cost. The caller is
// responsible for persisting the updated budget (e.g., writing back to
// known-friends.json).
//
// Parameters:
//   - agentID:    the agent whose task is being reconciled
//   - usage:      observed token counts from GitReins evaluator
//   - pricing:    pricing data for cost computation
//   - provider:   provider key (e.g. "ollama-cloud")
//   - model:      model name (e.g. "glm-5.2")
//   - estimated:  the pre-flight CostEstimate produced by the estimator
//   - budget:     the agent's current BudgetInfo (not mutated)
//   - tracker:    drift tracker (may be nil — step 4 is skipped)
//   - calibrator: calibrator (may be nil — step 5 is skipped)
func ReconcilePipeline(
	agentID string,
	usage Usage,
	pricing *PricingYAML,
	provider, model string,
	estimated CostEstimate,
	budget BudgetInfo,
	tracker *DriftTracker,
	calibrator *Calibrator,
) (ReconciliationResult, BudgetInfo, error) {
	result := ReconciliationResult{
		AgentID:       agentID,
		EstimatedCost: estimated.CostTotal,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}

	// Step 2: compute actual cost from observed usage.
	actual, err := ActualCost(usage, pricing, provider, model)
	if err != nil {
		return result, budget, fmt.Errorf("compute actual cost: %w", err)
	}
	result.ActualCost = actual.CostTotal

	// Step 3: update budget_used in a copy of the BudgetInfo.
	updatedBudget := budget
	updatedBudget.BudgetUsed += actual.CostTotal
	result.BudgetUsedAfter = updatedBudget.BudgetUsed
	result.BudgetRemainingAfter = RemainingBudget(updatedBudget)

	// Compute drift percentage.
	result.DriftPct = ReconcileDrift(estimated, actual)
	result.OverThreshold = absFloat64(result.DriftPct) > DriftThresholdPct

	// Step 4: log drift to the DriftTracker.
	if tracker != nil {
		tracker.RecordDriftEntry(DriftEntry{
			AgentID:     agentID,
			Estimated:   estimated.CostTotal,
			Actual:      actual.CostTotal,
			DriftPct:    result.DriftPct,
			Timestamp:   time.Now().UTC(),
			Model:       model,
			Description: fmt.Sprintf("reconciliation: est=$%.4f actual=$%.4f drift=%.2f%%", estimated.CostTotal, actual.CostTotal, result.DriftPct),
		})
	}

	// Step 5: feed drift data into the Calibrator.
	if tracker != nil && calibrator != nil {
		fed := tracker.FeedCalibrator(calibrator, agentID)
		result.FedToCalibrator = fed > 0
	}

	return result, updatedBudget, nil
}

// ReconcileAgent is a convenience method that wraps ReconcilePipeline with
// the most common parameters. It returns only the ReconciliationResult for
// callers that don't need the updated budget.
func ReconcileAgent(
	agentID string,
	usage Usage,
	pricing *PricingYAML,
	provider, model string,
	estimated CostEstimate,
	budget BudgetInfo,
	tracker *DriftTracker,
	calibrator *Calibrator,
) (ReconciliationResult, error) {
	result, _, err := ReconcilePipeline(
		agentID, usage, pricing, provider, model,
		estimated, budget, tracker, calibrator,
	)
	return result, err
}

// FormatReconciliation renders a ReconciliationResult as a human-readable
// string for CLI output.
func FormatReconciliation(r ReconciliationResult) string {
	thresholdMark := ""
	if r.OverThreshold {
		thresholdMark = " ⚠️"
	}
	calibMark := ""
	if r.FedToCalibrator {
		calibMark = " (calibrator updated)"
	}

	return fmt.Sprintf(
		"Agent: %s | Est: $%.4f | Actual: $%.4f | Drift: %.2f%%%s | Budget Remaining: $%.4f%s",
		r.AgentID, r.EstimatedCost, r.ActualCost,
		r.DriftPct, thresholdMark,
		r.BudgetRemainingAfter, calibMark,
	)
}

// absFloat64 returns the absolute value of a float64 without importing math
// (avoids an extra import for a trivial operation).
func absFloat64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
