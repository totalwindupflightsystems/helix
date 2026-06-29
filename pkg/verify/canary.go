package verify

import (
	"fmt"
	"time"
)

// =============================================================================
// Canary Promotion Decision Engine (spec §Phase 2 — Canary Promotion)
//
// After shadow verification passes, the CanaryPromoter evaluates whether the
// deployment is ready for canary promotion. It checks:
//   - Behavior contract passed all assertions
//   - Drift detector shows no degradation
//   - Success rate within threshold of baseline
//   - No new error types introduced
//   - Minimum observation window elapsed
//
// Returns PromotionDecision (READY/NOT_READY/NEEDS_MORE_DATA) with per-check
// results. ComputeCanaryPercentage decides the initial traffic ramp by trust
// tier. AutoRampSchedule generates a gradual ramp-up with observation gaps.
// =============================================================================

// PromotionDecision is the outcome of evaluating canary readiness.
type PromotionDecision string

const (
	// PromotionReady: all checks passed — safe to promote to canary.
	PromotionReady PromotionDecision = "READY"
	// PromotionNotReady: at least one check failed definitively.
	PromotionNotReady PromotionDecision = "NOT_READY"
	// PromotionNeedsMoreData: observation window not yet elapsed or data
	// insufficient — re-check after more data is collected.
	PromotionNeedsMoreData PromotionDecision = "NEEDS_MORE_DATA"
)

// CheckName identifies which promotion check produced a result.
type CheckName string

const (
	CheckContractPassed CheckName = "behavior_contract"
	CheckDriftOK        CheckName = "drift_detection"
	CheckSuccessRate    CheckName = "success_rate_threshold"
	CheckNoNewErrors    CheckName = "no_new_error_types"
	CheckObsWindow      CheckName = "observation_window_elapsed"
)

// PromotionCheck is the result of a single readiness check.
type PromotionCheck struct {
	Name    CheckName `json:"name"`
	Passed  bool      `json:"passed"`
	Detail  string    `json:"detail"`
	Skipped bool      `json:"skipped,omitempty"` // nil inputs → skipped, not failed
}

// PromotionResult aggregates all readiness checks into a single decision.
type PromotionResult struct {
	Decision PromotionDecision `json:"decision"`
	Checks   []PromotionCheck  `json:"checks"`
	Reason   string            `json:"reason,omitempty"`
}

// AllPassed returns true if every non-skipped check passed.
func (r PromotionResult) AllPassed() bool {
	for _, c := range r.Checks {
		if c.Skipped {
			continue
		}
		if !c.Passed {
			return false
		}
	}
	return true
}

// HasFailures returns true if any non-skipped check explicitly failed.
func (r PromotionResult) HasFailures() bool {
	for _, c := range r.Checks {
		if c.Skipped {
			continue
		}
		if !c.Passed {
			return true
		}
	}
	return false
}

// HasPendingData returns true if any check needs more data.
func (r PromotionResult) HasPendingData() bool {
	for _, c := range r.Checks {
		if c.Name == CheckObsWindow && !c.Passed && !c.Skipped {
			return true
		}
	}
	return false
}

// =============================================================================
// CanaryPromoter
// =============================================================================

// SuccessRateTolerance is the maximum allowed success rate degradation
// before promotion is blocked. Spec: success rate must be within 0.1% of
// production baseline.
const SuccessRateTolerance = 0.001

// MinObservationFactor is the minimum fraction of the observation window
// that must have elapsed before a definitive promotion decision can be made.
// Set to 1.0 — the full observation window must complete.
const MinObservationFactor = 1.0

// CanaryPromoter evaluates whether a shadow deployment is ready for canary
// promotion by checking all spec requirements.
type CanaryPromoter struct {
	// DriftSensitivity used for drift detection checks. Falls back to
	// DefaultSensitivity() if zero-valued.
	Sensitivity DriftSensitivity
}

// NewCanaryPromoter creates a promoter with default sensitivity.
func NewCanaryPromoter() *CanaryPromoter {
	return &CanaryPromoter{
		Sensitivity: DefaultSensitivity(),
	}
}

// EvaluatePromotion runs all 5 promotion checks against the shadow deployment
// result and metrics. If any check is skipped (nil input), it's marked skipped
// but doesn't fail the overall decision — it produces NEEDS_MORE_DATA.
//
// Parameters:
//   - report: the differential report from shadow evaluation (may be nil)
//   - contractResults: behavior contract check results (may be nil/empty)
//   - driftAssessment: drift detector output (may be nil)
//   - elapsed: time since shadow launch
//   - requiredWindow: the tier-specific observation window from CanarySchedule
func (cp *CanaryPromoter) EvaluatePromotion(
	report *DifferentialReport,
	contractResults []CheckResult,
	driftAssessment *DriftAssessment,
	elapsed time.Duration,
	requiredWindow time.Duration,
) PromotionResult {
	result := PromotionResult{}

	// 1. Behavior contract passed all assertions
	if contractResults == nil {
		result.Checks = append(result.Checks, PromotionCheck{
			Name: CheckContractPassed, Passed: false, Skipped: true,
			Detail: "no behavior contract results provided",
		})
	} else {
		passed := AllPassed(contractResults)
		detail := fmt.Sprintf("%d/%d assertions passed", countPassed(contractResults), len(contractResults))
		result.Checks = append(result.Checks, PromotionCheck{
			Name:   CheckContractPassed,
			Passed: passed,
			Detail: detail,
		})
	}

	// 2. Drift detector shows no degradation
	if driftAssessment == nil {
		result.Checks = append(result.Checks, PromotionCheck{
			Name: CheckDriftOK, Passed: false, Skipped: true,
			Detail: "no drift assessment provided",
		})
	} else {
		passed := !driftAssessment.HasCriticalBreach()
		detail := fmt.Sprintf("%d metric drifts, %d critical",
			driftAssessment.DriftCount(), driftAssessment.CriticalCount)
		result.Checks = append(result.Checks, PromotionCheck{
			Name:   CheckDriftOK,
			Passed: passed,
			Detail: detail,
		})
	}

	// 3. Success rate within threshold of baseline
	if report == nil {
		result.Checks = append(result.Checks, PromotionCheck{
			Name: CheckSuccessRate, Passed: false, Skipped: true,
			Detail: "no differential report provided",
		})
	} else {
		delta := report.Production.SuccessRate - report.Shadow.SuccessRate
		passed := delta <= SuccessRateTolerance
		detail := fmt.Sprintf("success rate delta %.4f (max %.4f)", delta, SuccessRateTolerance)
		result.Checks = append(result.Checks, PromotionCheck{
			Name:   CheckSuccessRate,
			Passed: passed,
			Detail: detail,
		})
	}

	// 4. No new error types introduced
	if report == nil {
		result.Checks = append(result.Checks, PromotionCheck{
			Name: CheckNoNewErrors, Passed: false, Skipped: true,
			Detail: "no differential report provided",
		})
	} else {
		newTypes := report.Shadow.NewErrorTypes
		passed := newTypes == 0
		detail := fmt.Sprintf("%d new error types detected", newTypes)
		result.Checks = append(result.Checks, PromotionCheck{
			Name:   CheckNoNewErrors,
			Passed: passed,
			Detail: detail,
		})
	}

	// 5. Minimum observation window elapsed
	windowPassed := elapsed >= requiredWindow
	detail := fmt.Sprintf("elapsed %s / required %s", elapsed.Round(time.Second), requiredWindow)
	result.Checks = append(result.Checks, PromotionCheck{
		Name:   CheckObsWindow,
		Passed: windowPassed,
		Detail: detail,
	})

	// Determine overall decision
	result.Decision = computeDecision(&result)
	return result
}

// computeDecision aggregates per-check results into a final decision.
// If any check has an explicit failure (non-skipped, passed=false) → NOT_READY.
// If all non-skipped checks pass but some are skipped → NEEDS_MORE_DATA.
// If all checks pass → READY.
func computeDecision(result *PromotionResult) PromotionDecision {
	if result.HasFailures() {
		return PromotionNotReady
	}
	for _, c := range result.Checks {
		if c.Skipped {
			return PromotionNeedsMoreData
		}
	}
	if result.AllPassed() {
		return PromotionReady
	}
	return PromotionNotReady
}

// countPassed returns how many check results passed.
func countPassed(results []CheckResult) int {
	count := 0
	for _, r := range results {
		if r.Passed {
			count++
		}
	}
	return count
}

// =============================================================================
// ComputeCanaryPercentage — initial traffic ramp by trust tier
// =============================================================================

// ComputeCanaryPercentage returns the initial canary traffic percentage
// for a given trust tier. Higher-trust agents get larger initial allocations
// because their code has demonstrated reliability.
//
// Spec §Agent-Specific Canary Rules:
//   - Provisional: 1% (most cautious)
//   - Observed: 5%
//   - Trusted: 10%
//   - Veteran: 25% (most aggressive — fast deployment)
func ComputeCanaryPercentage(tier string) float64 {
	switch tier {
	case "provisional":
		return 1.0
	case "observed":
		return 5.0
	case "trusted":
		return 10.0
	case "veteran":
		return 25.0
	default:
		return 1.0 // conservative default
	}
}

// =============================================================================
// AutoRampSchedule — gradual canary ramp-up with observation gaps
// =============================================================================

// RampStep is a single step in the auto-ramp schedule.
type RampStep struct {
	Step                int           `json:"step"`
	TrafficPct          float64       `json:"traffic_pct"`
	ObservationDuration time.Duration `json:"observation_duration"` // wait this long before next step
}

// AutoRampSchedule generates a gradual ramp-up schedule for canary promotion.
// It creates incremental traffic steps from the initial percentage to 100%,
// with observation gaps between each increment for monitoring.
//
// The schedule respects the trust tier's canary step count from CanarySchedule:
//   - Provisional: 6 steps (most gradual)
//   - Observed: 4 steps
//   - Trusted: 3 steps
//   - Veteran: 2 steps (fastest)
func AutoRampSchedule(tier string) []RampStep {
	_, canarySteps := CanarySchedule(tier)
	if len(canarySteps) == 0 {
		// Fallback for unknown tiers
		canarySteps = []CanaryStep{
			{Step: 1, TrafficPct: 1, Duration: 12 * time.Hour},
			{Step: 2, TrafficPct: 100, Duration: 12 * time.Hour},
		}
	}

	rampSteps := make([]RampStep, len(canarySteps))
	for i, cs := range canarySteps {
		rampSteps[i] = RampStep{
			Step:                cs.Step,
			TrafficPct:          cs.TrafficPct,
			ObservationDuration: cs.Duration,
		}
	}
	return rampSteps
}

// =============================================================================
// DriftAssessment helpers (needed for EvaluatePromotion)
// =============================================================================

// HasCriticalBreach returns true if any drift metric has critical severity.
func (da *DriftAssessment) HasCriticalBreach() bool {
	if da == nil {
		return false
	}
	return da.CriticalCount > 0
}

// DriftCount returns the total number of metrics that exceed their threshold.
func (da *DriftAssessment) DriftCount() int {
	if da == nil {
		return 0
	}
	count := 0
	for _, r := range da.MetricReports {
		if r.Exceeds {
			count++
		}
	}
	return count
}
