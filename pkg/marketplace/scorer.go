package marketplace

import (
	"fmt"
	"time"
)

// Trust score calculation (spec §7).
//
// The trust score is computed from five components:
//
//   base_score          = 30  (starting trust for provisioned agents)
//   acceptance_bonus    = MIN(40, merged_prs × 2)
//   rejection_penalty   = MIN(20, rejected_prs × 3)
//   incident_penalty    = MIN(30, incidents × 10 + force_merges × 5 + budget_overruns × 3)
//   human_rating_bonus  = MIN(10, avg_rating × 2)  [only if avg_rating ≥ 3.0]
//
// The result is clamped to [0, 100].

// CalculateTrustScore computes trust from the formula in spec §7.1.
//
// Parameters:
//   - mergedPRs:      merged PRs in the last 90 days (Forgejo)
//   - rejectedPRs:    rejected PRs in the last 90 days (Forgejo)
//   - incidents:      security incidents in the last 90 days (Chimera)
//   - forceMerges:    force-merged PRs in the last 90 days
//   - budgetOverruns: tasks that exceeded budget in the last 90 days (H4F)
//   - avgRating:      average human rating (1.0–5.0) from marketplace ratings
//
// The human rating bonus only applies when avgRating ≥ 3.0 (spec §9.2: "Below
// 3.0 → no bonus").
func CalculateTrustScore(mergedPRs, rejectedPRs, incidents, forceMerges, budgetOverruns int, avgRating float64) int {
	const (
		base             = 30
		acceptanceCap    = 40
		rejectionCap     = 20
		incidentCap      = 30
		humanBonusCap    = 10
		humanBonusCutoff = 3.0
	)

	acceptanceBonus := min(acceptanceCap, mergedPRs*2)
	rejectionPenalty := min(rejectionCap, rejectedPRs*3)
	incidentPenalty := min(incidentCap, incidents*10+forceMerges*5+budgetOverruns*3)

	humanBonus := 0
	if avgRating >= humanBonusCutoff {
		humanBonus = min(humanBonusCap, int(avgRating*2))
	}

	score := base + acceptanceBonus - rejectionPenalty - incidentPenalty + humanBonus
	return clamp(score, 0, 100)
}

// TrustLabel returns the human-readable label for a trust range (spec §7.3):
//
//	0–29   → New          (no review/approval permissions)
//	30–49  → Established  (can review, can't approve)
//	50–69  → Trusted      (can close issues, review PRs)
//	70–89  → Senior       (can approve, veto with evidence)
//	90–100 → Elder        (maximum permissions, weighted veto)
func TrustLabel(score int) string {
	switch {
	case score < 30:
		return "New"
	case score < 50:
		return "Established"
	case score < 70:
		return "Trusted"
	case score < 90:
		return "Senior"
	default:
		return "Elder"
	}
}

// DailyRecalculation reads agent manifests, recomputes trust from objective
// data sources (GitReins, Chimera, H4F, Forgejo), and writes updated manifests
// back to disk. This is the cron job wired at 02:00 UTC (spec §7.4).
//
// In this stub build it is a no-op placeholder — the data source integration
// is deferred to the implementation phase. It always returns nil.
func DailyRecalculation(marketplaceDir string) error {
	// TODO: wire to GitReins (task completion, PR stats), Chimera (review
	// accuracy), H4F (budget adherence, uptime), Forgejo (PR merge/reject
	// counts). For each agent, recompute trust and persist the updated
	// manifest. Log to recalculation.log (spec §14).
	_ = marketplaceDir // suppress unused-parameter warning in linters
	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// Scorer computes and tracks agent reputation over time (spec §7, §9).
// It wraps the trust formula with state for merge/review tracking and
// applies time-based decay for inactive agents.
type Scorer struct {
	// mergeHistory tracks merge outcomes per agent.
	mergeHistory map[string]*MergeStats
	// reviewCount tracks the number of reviews per agent.
	reviewCount map[string]int
	// lastActivity tracks the last ISO timestamp an agent had activity.
	lastActivity map[string]string
}

// MergeStats tracks merge outcomes for reputation calculation.
type MergeStats struct {
	MergedPRs      int
	RejectedPRs    int
	Incidents      int
	ForceMerges    int
	BudgetOverruns int
}

// NewScorer creates a fresh Scorer with empty tracking state.
func NewScorer() *Scorer {
	return &Scorer{
		mergeHistory: make(map[string]*MergeStats),
		reviewCount:  make(map[string]int),
		lastActivity: make(map[string]string),
	}
}

// CalculateReputation computes the reputation score for an agent using the
// same formula as CalculateTrustScore but incorporating the Scorer's tracked
// state (spec §7.1). Reputation decays over time if the agent is inactive
// (spec §7.2: "Reputation decays over time if inactive").
//
// The decay formula: for each 30-day period of inactivity beyond the first
// 30 days, the score is reduced by 5%, floored at the base score (30).
func (s *Scorer) CalculateReputation(agent string) (float64, error) {
	stats, ok := s.mergeHistory[agent]
	if !ok {
		stats = &MergeStats{}
	}

	// Get the agent's average rating from the review count.
	// In a full implementation this would query the marketplace ratings.
	// For the stub, we use a default of 0 (no bonus).
	avgRating := 0.0

	score := CalculateTrustScore(
		stats.MergedPRs,
		stats.RejectedPRs,
		stats.Incidents,
		stats.ForceMerges,
		stats.BudgetOverruns,
		avgRating,
	)

	// Apply decay for inactivity.
	decayed := s.applyDecay(agent, float64(score))
	return decayed, nil
}

// RecordReview records a human review for an agent (spec §9). Updates the
// review count and last activity timestamp.
func (s *Scorer) RecordReview(agent string, review Review) error {
	if review.Rating < 1 || review.Rating > 5 {
		return &ExitError{
			Code:    ExitInvalidRating,
			Message: fmt.Sprintf("INVALID_RATING: must be 1-5, got %d", review.Rating),
		}
	}
	s.reviewCount[agent]++
	s.lastActivity[agent] = nowISO()
	return nil
}

// RecordMerge records a merge outcome for an agent (spec §7). Updates the
// merge stats and last activity timestamp.
func (s *Scorer) RecordMerge(agent string, success bool) error {
	stats, ok := s.mergeHistory[agent]
	if !ok {
		stats = &MergeStats{}
		s.mergeHistory[agent] = stats
	}
	if success {
		stats.MergedPRs++
	} else {
		stats.RejectedPRs++
	}
	s.lastActivity[agent] = nowISO()
	return nil
}

// applyDecay reduces the score based on inactivity periods.
// For each 30-day window beyond the first 30 days, the score is reduced by 5%.
// The score never decays below the base score (30).
func (s *Scorer) applyDecay(agent string, score float64) float64 {
	const (
		baseScore   = 30.0
		decayRate   = 0.05                // 5% per 30-day period
		decayWindow = 30 * 24 * time.Hour // 30 days
		gracePeriod = 30 * 24 * time.Hour // first 30 days: no decay
	)

	last, ok := s.lastActivity[agent]
	if !ok {
		// No activity recorded — apply maximum decay.
		// Without data, return the score as-is (conservative).
		return score
	}

	lastTime, err := time.Parse(time.RFC3339, last)
	if err != nil {
		return score // unparseable timestamp — skip decay
	}

	inactive := time.Since(lastTime)
	if inactive <= gracePeriod {
		return score // within grace period — no decay
	}

	// Number of 30-day periods beyond the grace window.
	periods := int((inactive - gracePeriod) / decayWindow)
	if periods <= 0 {
		return score
	}

	decayed := score
	for i := 0; i < periods; i++ {
		decayed *= (1.0 - decayRate)
	}
	if decayed < baseScore {
		decayed = baseScore
	}
	return decayed
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
