package trust

import (
	"fmt"
	"math"
	"time"
)

// =============================================================================
// TrustSnapshot — point-in-time view of an agent's trust state.
// Spec §The Trust Ledger: "replay the ledger to verify any agent's current score"
// =============================================================================

// TrustSnapshot is the full point-in-time trust state for one agent.
type TrustSnapshot struct {
	AgentID        string           `json:"agent_id"`
	Score          TrustScore       `json:"score"`
	Tier           TrustTier        `json:"tier"`
	TotalEvents    int              `json:"total_events"`
	ScoreBreakdown ScoreBreakdown   `json:"score_breakdown"`
	RecentEvents   []TrustEvent     `json:"recent_events"`
	TierHistory    []TierTransition `json:"tier_history"`
	ScoreTrend     ScoreTrend       `json:"score_trend"`
	LastActive     time.Time        `json:"last_active"`
	SnapshotTime   time.Time        `json:"snapshot_time"`
}

// ScoreBreakdown shows the per-dimension weight contributions to the score.
type ScoreBreakdown struct {
	MergeSuccessRate DimensionContribution `json:"merge_success_rate"`
	IncidentTrack    DimensionContribution `json:"incident_attribution"`
	ReviewConsensus  DimensionContribution `json:"review_consensus"`
	PromptIntegrity  DimensionContribution `json:"prompt_integrity"`
	HumanFeedback    DimensionContribution `json:"human_feedback"`
	Tenure           DimensionContribution `json:"tenure"`
}

// DimensionContribution shows the weight and estimated score for one dimension.
type DimensionContribution struct {
	Weight       float64 `json:"weight"`
	EstScore     float64 `json:"estimated_score"`
	Contribution float64 `json:"contribution"` // weight × est_score
}

// TierTransition records a promotion or demotion event.
type TierTransition struct {
	From      TrustTier `json:"from"`
	To        TrustTier `json:"to"`
	IsPromote bool      `json:"is_promotion"`
	Timestamp time.Time `json:"timestamp"`
	Reason    string    `json:"reason,omitempty"`
}

// ScoreTrend summarizes score changes over a time window.
type ScoreTrend struct {
	Window     time.Duration `json:"window"`
	StartScore TrustScore    `json:"start_score"`
	EndScore   TrustScore    `json:"end_score"`
	Delta      float64       `json:"delta"`
	Direction  string        `json:"direction"` // "up", "down", "stable"
	DataPoints int           `json:"data_points"`
}

const (
	TrendUp     = "up"
	TrendDown   = "down"
	TrendStable = "stable"
)

// SnapshotWindow30Days is the default lookback window for recent events.
const SnapshotWindow30Days = 30 * 24 * time.Hour

// GetSnapshot replays the ledger and returns the full trust state for an agent.
// It is the primary query API: any observer with the ledger file can call this
// to independently verify an agent's trust state.
func GetSnapshot(ledgerPath, agentID string) (*TrustSnapshot, error) {
	events, err := Replay(ledgerPath)
	if err != nil {
		return nil, fmt.Errorf("replay ledger: %w", err)
	}

	score, lastActive := computeScoreFromEvents(events, agentID)
	agentEvents := filterAgentEvents(events, agentID)

	tier := computeTierFromEvents(agentEvents, score)

	snapshot := &TrustSnapshot{
		AgentID:      agentID,
		Score:        score,
		Tier:         tier,
		TotalEvents:  len(agentEvents),
		LastActive:   lastActive,
		SnapshotTime: time.Now().UTC(),
	}

	snapshot.ScoreBreakdown = computeScoreBreakdown(agentEvents)
	snapshot.RecentEvents = filterRecentEvents(agentEvents, SnapshotWindow30Days)
	snapshot.TierHistory = extractTierHistory(agentEvents)
	snapshot.ScoreTrend = computeScoreTrend(agentEvents, SnapshotWindow30Days)

	return snapshot, nil
}

// GetScoreBreakdown returns the per-dimension contribution breakdown.
func GetScoreBreakdown(ledgerPath, agentID string) (ScoreBreakdown, error) {
	events, err := Replay(ledgerPath)
	if err != nil {
		return ScoreBreakdown{}, fmt.Errorf("replay ledger: %w", err)
	}
	agentEvents := filterAgentEvents(events, agentID)
	return computeScoreBreakdown(agentEvents), nil
}

// GetTierHistory returns all promotion/demotion events for an agent.
func GetTierHistory(ledgerPath, agentID string) ([]TierTransition, error) {
	events, err := Replay(ledgerPath)
	if err != nil {
		return nil, fmt.Errorf("replay ledger: %w", err)
	}
	agentEvents := filterAgentEvents(events, agentID)
	return extractTierHistory(agentEvents), nil
}

// ScoreTrendOver returns the score change over a given time window.
func ScoreTrendOver(ledgerPath, agentID string, window time.Duration) (ScoreTrend, error) {
	events, err := Replay(ledgerPath)
	if err != nil {
		return ScoreTrend{}, fmt.Errorf("replay ledger: %w", err)
	}
	agentEvents := filterAgentEvents(events, agentID)
	return computeScoreTrend(agentEvents, window), nil
}

// GetRecentEvents returns events within the last N days for an agent.
func GetRecentEvents(ledgerPath, agentID string, days int) ([]TrustEvent, error) {
	events, err := Replay(ledgerPath)
	if err != nil {
		return nil, fmt.Errorf("replay ledger: %w", err)
	}
	agentEvents := filterAgentEvents(events, agentID)
	window := time.Duration(days) * 24 * time.Hour
	return filterRecentEvents(agentEvents, window), nil
}

// =============================================================================
// Internal helpers
// =============================================================================

func filterAgentEvents(events []TrustEvent, agentID string) []TrustEvent {
	var result []TrustEvent
	for _, e := range events {
		if e.AgentID == agentID {
			result = append(result, e)
		}
	}
	return result
}

func computeScoreFromEvents(events []TrustEvent, agentID string) (TrustScore, time.Time) {
	var score TrustScore = 0.5 // neutral starting
	var lastActive time.Time

	for _, evt := range events {
		if evt.AgentID != agentID {
			continue
		}
		switch evt.EventType {
		case EventMergeSuccess, EventReviewConsensus, EventHumanRating,
			EventTierChange, EventDemotion, EventIncidentAttrib:
			score = TrustScore(evt.Data.ScoreAfter)
		case EventIncidentPenalty:
			score = ApplyIncidentPenalty(TrustScore(evt.Data.ScoreBefore), evt.Data.AttributionWt)
		case EventDecayApplied:
			score = ApplyInactivityDecay(TrustScore(evt.Data.ScoreBefore), evt.Data.DecayWeeks*DaysPerWeek)
		}
		lastActive = evt.Timestamp
	}

	// Apply inactivity decay since the last event
	if !lastActive.IsZero() {
		daysSince := time.Since(lastActive).Hours() / 24
		if daysSince > InactivityGraceDays {
			score = ApplyInactivityDecay(score, int(daysSince))
		}
	}

	return score, lastActive
}

func computeTierFromEvents(agentEvents []TrustEvent, score TrustScore) TrustTier {
	totalMerges := countEventsByType(agentEvents, EventMergeSuccess)
	incidents180d := countRecentIncidents(agentEvents, 180)
	return DetermineTier(float64(score), totalMerges, incidents180d)
}

func countEventsByType(events []TrustEvent, eventType string) int {
	count := 0
	for _, e := range events {
		if e.EventType == eventType {
			count++
		}
	}
	return count
}

func countRecentIncidents(events []TrustEvent, days int) int {
	cutoff := time.Now().AddDate(0, 0, -days)
	count := 0
	for _, e := range events {
		if e.EventType == EventIncidentAttrib && e.Timestamp.After(cutoff) {
			count++
		}
	}
	return count
}

func computeScoreBreakdown(agentEvents []TrustEvent) ScoreBreakdown {
	// Estimate per-dimension scores from event history.
	// We derive best-effort dimension scores from the event stream.
	mergeRate := estimateMergeSuccessRate(agentEvents)
	incidentTrack := estimateIncidentTrack(agentEvents)
	reviewScore := estimateReviewConsensus(agentEvents)
	promptScore := estimatePromptIntegrity(agentEvents)
	humanScore := estimateHumanFeedback(agentEvents)
	tenureScore := estimateTenure(agentEvents)

	return ScoreBreakdown{
		MergeSuccessRate: makeContribution("merge_success_rate", mergeRate),
		IncidentTrack:    makeContribution("incident_attribution", incidentTrack),
		ReviewConsensus:  makeContribution("review_consensus", reviewScore),
		PromptIntegrity:  makeContribution("prompt_integrity", promptScore),
		HumanFeedback:    makeContribution("human_feedback", humanScore),
		Tenure:           makeContribution("tenure", tenureScore),
	}
}

func makeContribution(dimKey string, estScore float64) DimensionContribution {
	weight := DimensionWeights[dimKey]
	return DimensionContribution{
		Weight:       weight,
		EstScore:     estScore,
		Contribution: weight * estScore,
	}
}

// estimateMergeSuccessRate derives merge success from merge events vs incidents.
func estimateMergeSuccessRate(events []TrustEvent) float64 {
	merges := 0
	reverts := 0
	for _, e := range events {
		if e.EventType == EventMergeSuccess {
			merges++
		}
		if e.EventType == EventIncidentAttrib {
			// Each incident implies a potential revert
			reverts++
		}
	}
	if merges+reverts == 0 {
		return 0.5 // neutral
	}
	return float64(merges) / float64(merges+reverts)
}

// estimateIncidentTrack: 1.0 = clean, 0.0 = recent incident.
func estimateIncidentTrack(events []TrustEvent) float64 {
	cutoff30d := time.Now().AddDate(0, 0, -30)
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].EventType == EventIncidentAttrib && events[i].Timestamp.After(cutoff30d) {
			return 0.1 // recent incident → very low
		}
	}
	cutoff90d := time.Now().AddDate(0, 0, -90)
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].EventType == EventIncidentAttrib && events[i].Timestamp.After(cutoff90d) {
			return 0.5 // 31-90 days → moderate
		}
	}
	return 1.0 // clean
}

// estimateReviewConsensus from review events.
func estimateReviewConsensus(events []TrustEvent) float64 {
	reviews := 0
	for _, e := range events {
		if e.EventType == EventReviewConsensus {
			reviews++
		}
	}
	if reviews == 0 {
		return 0.5
	}
	// More reviews → more data → higher confidence score
	score := 0.5 + float64(reviews)*0.05
	if score > 1.0 {
		score = 1.0
	}
	return score
}

// estimatePromptIntegrity: ratio of events with evidence to total.
func estimatePromptIntegrity(events []TrustEvent) float64 {
	if len(events) == 0 {
		return 0.5
	}
	withEvidence := 0
	for _, e := range events {
		if len(e.Data.Evidence) > 0 {
			withEvidence++
		}
	}
	return float64(withEvidence) / float64(len(events))
}

// estimateHumanFeedback from human_rating events.
func estimateHumanFeedback(events []TrustEvent) float64 {
	var sum float64
	count := 0
	for _, e := range events {
		if e.EventType == EventHumanRating {
			sum += e.Data.ScoreAfter
			count++
		}
	}
	if count == 0 {
		return 0.5
	}
	return sum / float64(count)
}

// estimateTenure: log-scaled by days active.
func estimateTenure(events []TrustEvent) float64 {
	if len(events) == 0 {
		return 0.0
	}
	first := events[0].Timestamp
	days := time.Since(first).Hours() / 24
	// Log scale: 1 day → ~0.15, 30 days → ~0.50, 90 days → ~0.65, 365 days → ~0.85
	if days <= 0 {
		return 0.0
	}
	score := math.Log(days+1) / math.Log(366)
	if score > 1.0 {
		score = 1.0
	}
	return score
}

func filterRecentEvents(events []TrustEvent, window time.Duration) []TrustEvent {
	cutoff := time.Now().Add(-window)
	var result []TrustEvent
	for _, e := range events {
		if e.Timestamp.After(cutoff) {
			result = append(result, e)
		}
	}
	// Cap at 100 events to keep snapshot manageable
	if len(result) > 100 {
		result = result[len(result)-100:]
	}
	return result
}

func extractTierHistory(events []TrustEvent) []TierTransition {
	var history []TierTransition
	for _, e := range events {
		if e.EventType == EventTierChange || e.EventType == EventDemotion {
			from := TrustTier(e.Data.OldTier)
			to := TrustTier(e.Data.NewTier)
			transition := TierTransition{
				From:      from,
				To:        to,
				IsPromote: TierRank(to) > TierRank(from),
				Timestamp: e.Timestamp,
			}
			history = append(history, transition)
		}
	}
	return history
}

func computeScoreTrend(events []TrustEvent, window time.Duration) ScoreTrend {
	cutoff := time.Now().Add(-window)

	var windowEvents []TrustEvent
	for _, e := range events {
		if e.Timestamp.After(cutoff) && e.Data.ScoreAfter != 0 {
			windowEvents = append(windowEvents, e)
		}
	}

	if len(windowEvents) == 0 {
		return ScoreTrend{
			Window:     window,
			Direction:  TrendStable,
			DataPoints: 0,
		}
	}

	startScore := TrustScore(windowEvents[0].Data.ScoreBefore)
	if startScore == 0 {
		startScore = 0.5
	}
	endScore := TrustScore(windowEvents[len(windowEvents)-1].Data.ScoreAfter)
	delta := float64(endScore) - float64(startScore)

	direction := TrendStable
	if delta > 0.01 {
		direction = TrendUp
	} else if delta < -0.01 {
		direction = TrendDown
	}

	return ScoreTrend{
		Window:     window,
		StartScore: startScore,
		EndScore:   endScore,
		Delta:      delta,
		Direction:  direction,
		DataPoints: len(windowEvents),
	}
}
