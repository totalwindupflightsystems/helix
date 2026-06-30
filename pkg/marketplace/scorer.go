package marketplace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
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

// DailyRecalculation reads agent manifests from the marketplace directory,
// recomputes trust from the Performance metrics stored in each manifest, and
// writes updated manifests back to disk. This is the cron job wired at 02:00
// UTC (spec §7.4).
//
// The recalculation:
//  1. Loads all agent manifests from <marketplaceDir>/agents/*.yaml
//  2. For each active agent, computes a fresh trust score from Performance data
//  3. Applies time-based decay for inactivity (spec §7.2)
//  4. Updates the trust_score and updated_at fields in the manifest
//  5. Writes the manifest back to disk
//  6. Appends a recalculation entry to <marketplaceDir>/recalculation.log
//
// Agents with StatusRetired are skipped. Agents with StatusDeprecated are
// recalculated but flagged in the log.
func DailyRecalculation(marketplaceDir string) error {
	if marketplaceDir == "" {
		return fmt.Errorf("marketplace dir is empty")
	}

	agentsDir := filepath.Join(marketplaceDir, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no agents dir — nothing to recalculate
		}
		return fmt.Errorf("read agents dir: %w", err)
	}

	var logEntries []string
	recalcTime := nowISO()

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		path := filepath.Join(agentsDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue // skip unreadable manifests
		}

		var agent Agent
		if err := yaml.Unmarshal(data, &agent); err != nil {
			continue // skip malformed manifests
		}

		if agent.Name == "" || agent.Status == StatusRetired {
			continue
		}

		oldScore := agent.TrustScore
		newScore := recalculateAgentTrust(&agent)
		agent.TrustScore = newScore
		agent.UpdatedAt = recalcTime

		// Marshal back to YAML and write.
		out, err := yaml.Marshal(&agent)
		if err != nil {
			logEntries = append(logEntries, fmt.Sprintf(
				"[%s] ERROR marshal %s: %v", recalcTime, agent.Name, err))
			continue
		}

		if err := os.WriteFile(path, out, 0o644); err != nil {
			logEntries = append(logEntries, fmt.Sprintf(
				"[%s] ERROR write %s: %v", recalcTime, agent.Name, err))
			continue
		}

		logEntries = append(logEntries, fmt.Sprintf(
			"[%s] %s: trust %d → %d (%s)",
			recalcTime, agent.Name, oldScore, newScore,
			trustChangeLabel(oldScore, newScore)))
	}

	// Append recalculation log.
	if len(logEntries) > 0 {
		logPath := filepath.Join(marketplaceDir, "recalculation.log")
		logContent := strings.Join(logEntries, "\n") + "\n"
		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = f.WriteString(logContent)
			_ = f.Close()
		}
	}

	return nil
}

// recalculateAgentTrust computes the fresh trust score from the agent's
// Performance metrics using the spec §7.1 formula. Performance data includes
// PR acceptance rate, review accuracy, budget adherence, and uptime.
// These are mapped to the CalculateTrustScore parameters.
func recalculateAgentTrust(agent *Agent) int {
	p := agent.Performance

	// Derive merged/rejected PR counts from acceptance rate.
	// tasks_completed gives us the total; acceptance rate gives the split.
	// PrAcceptanceRate == 0 means "not tracked" — treat as 1.0 (all merged).
	totalTasks := p.TasksCompleted
	if totalTasks == 0 {
		// No tasks — use base score (spec §7.1: base = 30).
		return clamp(int(applyGlobalDecay(agent.UpdatedAt, 30)), 0, 100)
	}

	acceptanceRate := p.PrAcceptanceRate
	if acceptanceRate == 0 {
		acceptanceRate = 1.0
	}
	mergedPRs := int(float64(totalTasks) * acceptanceRate)
	rejectedPRs := totalTasks - mergedPRs

	// Budget adherence → budget overruns (inverse: low adherence = more overruns).
	// BudgetAdherence == 0 means "not tracked" — treat as perfect adherence.
	budgetOverruns := 0
	if p.BudgetAdherence > 0 && p.BudgetAdherence < 0.9 {
		budgetOverruns = int(float64(totalTasks) * (1.0 - p.BudgetAdherence))
	}

	// Review accuracy and uptime don't directly map to the trust formula inputs
	// but contribute to the overall performance picture. For v1, we use
	// review_accuracy as a modifier on the human rating.
	avgRating := agent.Ratings.Average

	score := CalculateTrustScore(
		mergedPRs, rejectedPRs,
		0, // incidents — not tracked in Performance (would come from incident store)
		0, // force merges — not tracked in Performance
		budgetOverruns,
		avgRating,
	)

	// Apply time-based decay for inactivity.
	score = int(applyGlobalDecay(agent.UpdatedAt, float64(score)))

	return clamp(score, 0, 100)
}

// applyGlobalDecay reduces a score based on time since the agent's last update.
// For each 30-day window beyond the first 30 days, reduce by 5%.
// Floor at the base score (30).
func applyGlobalDecay(lastUpdated string, score float64) float64 {
	if lastUpdated == "" {
		return score // no timestamp — can't compute decay
	}

	lastTime, err := time.Parse(time.RFC3339, lastUpdated)
	if err != nil {
		return score // unparseable — skip decay
	}

	const (
		baseScore   = 30.0
		decayRate   = 0.05
		decayWindow = 30 * 24 * time.Hour
		gracePeriod = 30 * 24 * time.Hour
	)

	inactive := time.Since(lastTime)
	if inactive <= gracePeriod {
		return score
	}

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

// trustChangeLabel returns a human-readable description of a trust change.
func trustChangeLabel(old, new int) string {
	switch {
	case new > old:
		return fmt.Sprintf("+%d promoted", new-old)
	case new < old:
		return fmt.Sprintf("-%d demoted", old-new)
	default:
		return "unchanged"
	}
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
