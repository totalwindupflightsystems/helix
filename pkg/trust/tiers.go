// Package trust implements a graduated, multi-dimensional trust scoring system
// for AI agents per specs/trust-model.md.
//
// Trust is a 0.0–1.0 float64 calculated from six weighted dimensions. Every
// trust event is recorded in an append-only JSONL ledger whose replay is
// deterministic — any observer can independently verify an agent's score.
package trust

import (
	"math"
)

// =============================================================================
// TrustTier
// =============================================================================

// TrustTier represents an agent's graduated privilege tier.
type TrustTier string

const (
	TierProvisional TrustTier = "provisional"
	TierObserved    TrustTier = "observed"
	TierTrusted     TrustTier = "trusted"
	TierVeteran     TrustTier = "veteran"
)

// AllTiers returns all tiers in ascending order.
func AllTiers() []TrustTier {
	return []TrustTier{TierProvisional, TierObserved, TierTrusted, TierVeteran}
}

// TierThresholds defines the minimum score, merge count, and max incidents
// (rolling 180d) required for each tier.
type TierThresholds struct {
	MinScore          float64
	MinMerges         int
	MaxIncidents180d  int // -1 means unlimited
}

// Thresholds for each tier per spec §Tier Thresholds.
func thresholdsFor(tier TrustTier) TierThresholds {
	switch tier {
	case TierProvisional:
		return TierThresholds{MinScore: 0.0, MinMerges: 0, MaxIncidents180d: -1}
	case TierObserved:
		return TierThresholds{MinScore: 0.40, MinMerges: 100, MaxIncidents180d: 0}
	case TierTrusted:
		return TierThresholds{MinScore: 0.65, MinMerges: 500, MaxIncidents180d: 0}
	case TierVeteran:
		return TierThresholds{MinScore: 0.85, MinMerges: 2000, MaxIncidents180d: 1}
	default:
		return TierThresholds{MinScore: 0.0, MinMerges: 0, MaxIncidents180d: -1}
	}
}

// DetermineTier returns the highest tier an agent qualifies for given their
// current score, total merges, and incident count in the last 180 days.
// The check proceeds from Veteran down to Provisional — the first tier whose
// thresholds are all met wins.
func DetermineTier(score float64, totalMerges int, incidents180d int) TrustTier {
	tiers := []TrustTier{TierVeteran, TierTrusted, TierObserved, TierProvisional}
	for _, t := range tiers {
		th := thresholdsFor(t)
		if score >= th.MinScore && totalMerges >= th.MinMerges {
			if th.MaxIncidents180d == -1 || incidents180d <= th.MaxIncidents180d {
				return t
			}
		}
	}
	return TierProvisional
}

// =============================================================================
// DimensionWeights
// =============================================================================

// DimensionWeights holds the per-dimension weights from the spec.
// All weights sum to 1.0.
var DimensionWeights = map[string]float64{
	"merge_success_rate":  0.25,
	"incident_attribution": 0.30,
	"review_consensus":     0.15,
	"prompt_integrity":     0.10,
	"human_feedback":       0.10,
	"tenure":               0.10,
}

// =============================================================================
// TrustScore
// =============================================================================

// TrustScore is the agent's current trust score in [0.0, 1.0].
type TrustScore float64

// Clamp ensures the score stays within [0.0, 1.0].
func (s TrustScore) Clamp() TrustScore {
	if s < 0 {
		return 0
	}
	if s > 1.0 {
		return 1.0
	}
	return s
}

// DimensionScores holds the raw (pre-weight) scores for each dimension in [0,1].
type DimensionScores struct {
	MergeSuccessRate  float64 // Merged PRs / (merged + reverted)
	IncidentTrack     float64 // Transformed from merges-since-last-incident; 1.0 = clean, 0.0 = recent incident
	ReviewConsensus   float64 // Average review model agreement score
	PromptIntegrity   float64 // Ratio of commits with valid prompt attestation
	HumanFeedback     float64 // Normalized human ratings from marketplace
	Tenure            float64 // Log-scaled days since first contribution
}

// Calculate produces a weighted TrustScore from dimension scores.
func Calculate(dims DimensionScores) TrustScore {
	score := dims.MergeSuccessRate*DimensionWeights["merge_success_rate"] +
		dims.IncidentTrack*DimensionWeights["incident_attribution"] +
		dims.ReviewConsensus*DimensionWeights["review_consensus"] +
		dims.PromptIntegrity*DimensionWeights["prompt_integrity"] +
		dims.HumanFeedback*DimensionWeights["human_feedback"] +
		dims.Tenure*DimensionWeights["tenure"]
	return TrustScore(score).Clamp()
}

// =============================================================================
// Incident time-decay
// =============================================================================

// IncidentAttributionWeight returns the attribution weight for an incident
// that occurred d days ago per spec §Incident attribution rules:
//
//	0–7 days:   100%
//	8–30 days:   50%
//	31–90 days:  10%
//	>90 days:     0%
func IncidentAttributionWeight(daysSince float64) float64 {
	switch {
	case daysSince < 0:
		return 0
	case daysSince <= 7:
		return 1.0
	case daysSince <= 30:
		return 0.50
	case daysSince <= 90:
		return 0.10
	default:
		return 0
	}
}

// ApplyIncidentPenalty reduces a TrustScore by 0.3 × attributionWeight.
// The penalty is the spec-defined 0.3 multiplier for an attributable incident.
func ApplyIncidentPenalty(current TrustScore, attributionWeight float64) TrustScore {
	penalty := 0.3 * attributionWeight
	result := float64(current) - penalty
	return TrustScore(result).Clamp()
}

// =============================================================================
// Inactivity decay
// =============================================================================

// InactivityDecayRate is the weekly decay when inactive (per spec: 0.05/week).
const InactivityDecayRate = 0.05

// InactivityGraceDays is the grace period before decay begins (30 days).
const InactivityGraceDays = 30

// ApplyInactivityDecay reduces the score by 0.05 per full week of inactivity
// beyond the 30-day grace period. Score is frozen at 0.0 (cannot go negative).
func ApplyInactivityDecay(current TrustScore, daysInactive int) TrustScore {
	if daysInactive <= InactivityGraceDays {
		return current
	}
	excessDays := daysInactive - InactivityGraceDays
	weeks := float64(excessDays) / 7.0
	weeks = math.Floor(weeks) // only full weeks count
	decay := weeks * InactivityDecayRate
	result := float64(current) - decay
	if result < 0 {
		result = 0
	}
	return TrustScore(result)
}

// =============================================================================
// TenureScore
// =============================================================================

// TenureScore calculates the tenure dimension score from days since first
// contribution using log-scaling: log2(days+1) / log2(maxReference+1).
// The reference point is 730 days (2 years) which maps to ~1.0.
// Returns 1.0 for any tenure >= reference days.
func TenureScore(daysSinceFirstContribution int) float64 {
	if daysSinceFirstContribution <= 0 {
		return 0
	}
	const refDays = 730 // 2 years ≈ full score
	score := math.Log2(float64(daysSinceFirstContribution)+1) / math.Log2(float64(refDays)+1)
	if score > 1.0 {
		return 1.0
	}
	return score
}

// =============================================================================
// IncidentTrackScore
// =============================================================================

// IncidentTrackScore transforms merges-since-last-incident into a [0,1] score.
// An agent with many merges since their last incident approaches 1.0.
//   - Less than 10 merges: score grows linearly from 0.0 to 0.5
//   - 10–100 merges: score grows from 0.5 to 0.9
//   - 100+ merges: score saturates at 0.95 (never perfect — recency matters)
//   - Negative (incident more recent than last merge) → 0.0
func IncidentTrackScore(mergesSinceLastIncident int) float64 {
	if mergesSinceLastIncident < 0 {
		return 0
	}
	switch {
	case mergesSinceLastIncident == 0:
		return 0.5 // neutral starting point; no incidents yet
	case mergesSinceLastIncident < 10:
		return 0.5 + float64(mergesSinceLastIncident)*0.05 // 0.55 → 0.95
	case mergesSinceLastIncident < 100:
		return 0.9
	default:
		return 0.95
	}
}

// =============================================================================
// MergeSuccessScore
// =============================================================================

// MergeSuccessScore calculates the merge success rate dimension.
// Merged / (merged + reverted). If total is 0, returns 1.0 (neutral start).
func MergeSuccessScore(merged, reverted int) float64 {
	total := merged + reverted
	if total == 0 {
		return 1.0
	}
	return float64(merged) / float64(total)
}

// =============================================================================
// Demotion check
// =============================================================================

// ShouldDemote checks whether an agent's trust score has been below their
// current tier's minimum score for at least consecutiveDays. Per spec:
// "Trust score below tier threshold for 7 consecutive days → demotion."
func ShouldDemote(currentTier TrustTier, currentScore float64, consecutiveDaysBelow int) bool {
	th := thresholdsFor(currentTier)
	if currentScore >= th.MinScore {
		return false
	}
	return consecutiveDaysBelow >= 7
}

// DemoteTo returns the tier an agent should be demoted to when they fail
// their current tier's thresholds. Drops one level at a time.
func DemoteTo(currentTier TrustTier) TrustTier {
	switch currentTier {
	case TierVeteran:
		return TierTrusted
	case TierTrusted:
		return TierObserved
	case TierObserved:
		return TierProvisional
	default:
		return TierProvisional
	}
}

// Time constants used throughout the trust package.
const (
	DaysPerWeek = 7
)
