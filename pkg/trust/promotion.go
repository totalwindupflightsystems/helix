package trust

import (
	"fmt"
)

// =============================================================================
// Trust Tier Promotion Engine
//
// Per spec §Trust Tiers + §Tier Thresholds, an agent qualifies for promotion
// when ALL entry criteria are met for the target tier:
//
//   - Trust score ≥ tier minimum (0.40 / 0.65 / 0.85)
//   - Total merges ≥ tier minimum (100 / 500 / 2000)
//   - Attributable incidents in 180d ≤ tier maximum (0 / 0 / 1)
//   - Days active ≥ tier minimum (30 / 90 / 180)
//   - For Veteran: reviewed ≥ 50 other agents' PRs
//
// Promotion is the complement of demotion (ShouldDemote/DemoteTo). Together
// they form the complete tier lifecycle: agents earn promotion through
// demonstrated outcomes, and lose tier through incidents and inactivity.
// =============================================================================

// AgentMetrics captures the observable signals used for tier evaluation.
type AgentMetrics struct {
	TrustScore         float64 `json:"trust_score"`
	TotalMerges        int     `json:"total_merges"`
	Incidents180d      int     `json:"incidents_180d"`
	DaysActive         int     `json:"days_active"`
	PRsReviewed        int     `json:"prs_reviewed"`        // Veteran requirement
	CurrentTier        TrustTier `json:"current_tier"`
}

// CriterionResult is the pass/fail status of a single promotion criterion.
type CriterionResult struct {
	Name     string  `json:"name"`
	Required float64 `json:"required"`
	Actual   float64 `json:"actual"`
	Passed   bool    `json:"passed"`
}

// PromotionResult is the full evaluation of whether an agent qualifies for
// promotion to a target tier.
type PromotionResult struct {
	TargetTier    TrustTier         `json:"target_tier"`
	Qualified     bool              `json:"qualified"`
	Criteria      []CriterionResult `json:"criteria"`
	BlockingCount int               `json:"blocking_count"`
	Reason        string            `json:"reason"`
}

// tierEntryRequirements defines ALL entry requirements per spec §Trust Tiers.
type tierEntryRequirements struct {
	MinScore    float64
	MinMerges   int
	MaxIncidents int
	MinDays     int
	MinReviews  int // only Veteran requires this (>0); others are 0
}

// entryRequirementsFor returns the full entry requirements for a tier.
func entryRequirementsFor(tier TrustTier) tierEntryRequirements {
	switch tier {
	case TierProvisional:
		return tierEntryRequirements{MinScore: 0.0, MinMerges: 0, MaxIncidents: -1, MinDays: 0, MinReviews: 0}
	case TierObserved:
		return tierEntryRequirements{MinScore: 0.40, MinMerges: 100, MaxIncidents: 0, MinDays: 30, MinReviews: 0}
	case TierTrusted:
		return tierEntryRequirements{MinScore: 0.65, MinMerges: 500, MaxIncidents: 0, MinDays: 90, MinReviews: 0}
	case TierVeteran:
		return tierEntryRequirements{MinScore: 0.85, MinMerges: 2000, MaxIncidents: 1, MinDays: 180, MinReviews: 50}
	default:
		return tierEntryRequirements{MinScore: 0.0, MinMerges: 0, MaxIncidents: -1, MinDays: 0, MinReviews: 0}
	}
}

// EvaluatePromotion checks ALL entry criteria for a target tier and returns
// a detailed PromotionResult with per-criterion pass/fail.
func EvaluatePromotion(metrics AgentMetrics, targetTier TrustTier) *PromotionResult {
	req := entryRequirementsFor(targetTier)

	var criteria []CriterionResult
	blockCount := 0

	// Criterion 1: Trust score
	scorePassed := metrics.TrustScore >= req.MinScore
	criteria = append(criteria, CriterionResult{
		Name:     "trust_score",
		Required: req.MinScore,
		Actual:   metrics.TrustScore,
		Passed:   scorePassed,
	})
	if !scorePassed {
		blockCount++
	}

	// Criterion 2: Total merges
	mergesPassed := metrics.TotalMerges >= req.MinMerges
	criteria = append(criteria, CriterionResult{
		Name:     "total_merges",
		Required: float64(req.MinMerges),
		Actual:   float64(metrics.TotalMerges),
		Passed:   mergesPassed,
	})
	if !mergesPassed {
		blockCount++
	}

	// Criterion 3: Attributable incidents (max allowed)
	incidentsPassed := req.MaxIncidents == -1 || metrics.Incidents180d <= req.MaxIncidents
	criteria = append(criteria, CriterionResult{
		Name:     "max_incidents_180d",
		Required: float64(req.MaxIncidents),
		Actual:   float64(metrics.Incidents180d),
		Passed:   incidentsPassed,
	})
	if !incidentsPassed {
		blockCount++
	}

	// Criterion 4: Days active
	daysPassed := metrics.DaysActive >= req.MinDays
	criteria = append(criteria, CriterionResult{
		Name:     "min_days_active",
		Required: float64(req.MinDays),
		Actual:   float64(metrics.DaysActive),
		Passed:   daysPassed,
	})
	if !daysPassed {
		blockCount++
	}

	// Criterion 5: PR reviews (Veteran only)
	if req.MinReviews > 0 {
		reviewsPassed := metrics.PRsReviewed >= req.MinReviews
		criteria = append(criteria, CriterionResult{
			Name:     "min_pr_reviews",
			Required: float64(req.MinReviews),
			Actual:   float64(metrics.PRsReviewed),
			Passed:   reviewsPassed,
		})
		if !reviewsPassed {
			blockCount++
		}
	}

	qualified := blockCount == 0

	reason := ""
	if !qualified {
		reason = fmt.Sprintf("Agent does not meet %d of %d criteria for %s tier",
			blockCount, len(criteria), targetTier)
	} else {
		reason = fmt.Sprintf("Agent meets all %d criteria for %s tier", len(criteria), targetTier)
	}

	return &PromotionResult{
		TargetTier:    targetTier,
		Qualified:     qualified,
		Criteria:      criteria,
		BlockingCount: blockCount,
		Reason:        reason,
	}
}

// ShouldPromote returns true if an agent meets ALL entry criteria for the
// next tier above their current one.
func ShouldPromote(metrics AgentMetrics) bool {
	target := nextTierUp(metrics.CurrentTier)
	if target == metrics.CurrentTier {
		return false // already at Veteran (highest)
	}
	result := EvaluatePromotion(metrics, target)
	return result.Qualified
}

// PromoteTo returns the next tier above the current one if the agent qualifies.
// Returns the current tier if no promotion is possible (already Veteran or
// criteria not met).
func PromoteTo(metrics AgentMetrics) TrustTier {
	target := nextTierUp(metrics.CurrentTier)
	if target == metrics.CurrentTier {
		return metrics.CurrentTier
	}
	result := EvaluatePromotion(metrics, target)
	if result.Qualified {
		return target
	}
	return metrics.CurrentTier
}

// nextTierUp returns the tier immediately above the given one.
// Veteran has no tier above — returns Veteran.
func nextTierUp(tier TrustTier) TrustTier {
	switch tier {
	case TierProvisional:
		return TierObserved
	case TierObserved:
		return TierTrusted
	case TierTrusted:
		return TierVeteran
	default:
		return TierVeteran
	}
}

// EvaluateFullTierCycle runs a complete tier evaluation: check for promotion
// first, then demotion. Returns the recommended tier and the evaluation details.
//
// This is the canonical entry point for tier lifecycle management — call this
// after any trust event (merge, incident, inactivity decay) to determine if
// the agent's tier should change.
func EvaluateFullTierCycle(metrics AgentMetrics) (TrustTier, *PromotionResult, bool) {
	// Check promotion first
	target := nextTierUp(metrics.CurrentTier)
	if target != metrics.CurrentTier {
		promoResult := EvaluatePromotion(metrics, target)
		if promoResult.Qualified {
			return target, promoResult, true
		}
	}

	// Check demotion (score below current tier threshold)
	th := thresholdsFor(metrics.CurrentTier)
	if metrics.TrustScore < th.MinScore {
		// ShouldDemote requires consecutive days below threshold — but the caller
		// may not have that information. We return the current tier and let the
		// caller check ShouldDemote separately with the days-below count.
		// However, if we're checking the full cycle, we flag the demotion risk.
		return metrics.CurrentTier, nil, false
	}

	// No change
	return metrics.CurrentTier, nil, false
}

// TierRank returns the numeric rank of a tier (0=Provisional, 3=Veteran).
// Useful for comparisons.
func TierRank(tier TrustTier) int {
	switch tier {
	case TierProvisional:
		return 0
	case TierObserved:
		return 1
	case TierTrusted:
		return 2
	case TierVeteran:
		return 3
	default:
		return 0
	}
}

// IsPromotion returns true if the tier change is an upward promotion.
func IsPromotion(from, to TrustTier) bool {
	return TierRank(to) > TierRank(from)
}

// IsDemotion returns true if the tier change is a downward demotion.
func IsDemotion(from, to TrustTier) bool {
	return TierRank(to) < TierRank(from)
}
