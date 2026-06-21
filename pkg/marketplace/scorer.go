package marketplace

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

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
