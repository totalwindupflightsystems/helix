package trust

import (
	"fmt"
	"time"
)

// =============================================================================
// RecoveryTracker — tracks agents recovering from incidents and tier drops.
//
// Spec §Anti-Patterns: "Immutable reputation — An agent that ships 2,000 merges
// then causes 3 incidents drops. Permanence is the enemy of accuracy."
//
// This tracker ensures trust is earnable back: it monitors agents who have
// received incident penalties or tier demotions and tracks whether they are
// on a positive recovery trajectory (clean merges, improving score trend).
// =============================================================================

// RecoverySnapshot captures the recovery state for one agent at a point in time.
type RecoverySnapshot struct {
	AgentID                string     `json:"agent_id"`
	IsRecovering           bool       `json:"is_recovering"`
	RecoveryProgress       float64    `json:"recovery_progress"`  // 0–100
	PreIncidentScore       TrustScore `json:"pre_incident_score"` // score before last incident/demotion
	CurrentScore           TrustScore `json:"current_score"`
	ConsecutiveCleanMerges int        `json:"consecutive_clean_merges"` // merges since last incident
	DaysSinceLastIncident  int        `json:"days_since_last_incident"`
	IncidentCount          int        `json:"total_incidents"`
	Trend                  string     `json:"trend"`                     // up, down, stable
	TargetScore            TrustScore `json:"target_score"`              // pre-incident score to recover to
	EstimatedDaysToRecover int        `json:"estimated_days_to_recover"` // 0 if already recovered
}

// RecoveryConfig holds tunable thresholds for recovery tracking.
type RecoveryConfig struct {
	// CleanMergeRecoveryRate: how much each clean merge contributes to recovery.
	// E.g., 2.0 means each clean merge recovers 2% of the gap.
	CleanMergeRecoveryRate float64
	// DaysDecayRate: how much time without incident contributes to recovery per day.
	// E.g., 0.5 means each clean day recovers 0.5% of the gap.
	DaysDecayRate float64
	// TrendImprovementThreshold: minimum positive delta to count as "improving".
	TrendImprovementThreshold float64
	// MinCleanMergesForRecovering: minimum clean merges since last incident to be recovering.
	MinCleanMergesForRecovering int
}

// DefaultRecoveryConfig provides sensible spec-aligned defaults.
func DefaultRecoveryConfig() RecoveryConfig {
	return RecoveryConfig{
		CleanMergeRecoveryRate:      2.0,  // each clean merge → 2% of gap recovered
		DaysDecayRate:               0.5,  // each clean day → 0.5% of gap recovered
		TrendImprovementThreshold:   0.01, // 1% improvement needed to count as "up"
		MinCleanMergesForRecovering: 1,    // at least 1 clean merge post-incident
	}
}

// GetRecoverySnapshot replays the ledger and computes the recovery state for an agent.
// If the agent has never had an incident, IsRecovering is false and progress is 100.
func GetRecoverySnapshot(ledgerPath, agentID string) (*RecoverySnapshot, error) {
	return GetRecoverySnapshotWithConfig(ledgerPath, agentID, DefaultRecoveryConfig())
}

// GetRecoverySnapshotWithConfig is the configurable variant for testing.
func GetRecoverySnapshotWithConfig(ledgerPath, agentID string, cfg RecoveryConfig) (*RecoverySnapshot, error) {
	events, err := Replay(ledgerPath)
	if err != nil {
		return nil, fmt.Errorf("replay ledger: %w", err)
	}
	agentEvents := filterAgentEvents(events, agentID)
	return computeRecovery(agentEvents, agentID, cfg), nil
}

// GetRecoveryBatch computes recovery snapshots for multiple agents in one ledger replay.
func GetRecoveryBatch(ledgerPath string, agentIDs []string) (map[string]*RecoverySnapshot, error) {
	events, err := Replay(ledgerPath)
	if err != nil {
		return nil, fmt.Errorf("replay ledger: %w", err)
	}
	cfg := DefaultRecoveryConfig()
	result := make(map[string]*RecoverySnapshot)
	for _, id := range agentIDs {
		agentEvents := filterAgentEvents(events, id)
		result[id] = computeRecovery(agentEvents, id, cfg)
	}
	return result, nil
}

// =============================================================================
// Core computation
// =============================================================================

func computeRecovery(agentEvents []TrustEvent, agentID string, cfg RecoveryConfig) *RecoverySnapshot {
	snap := &RecoverySnapshot{
		AgentID:      agentID,
		IsRecovering: false,
		CurrentScore: 0.5,
	}

	if len(agentEvents) == 0 {
		snap.RecoveryProgress = 100.0
		snap.Trend = TrendStable
		return snap
	}

	// Find the last incident or demotion event.
	lastIncidentIdx := findLastIncidentOrDemotion(agentEvents)
	incidentCount := countIncidents(agentEvents)

	// Compute current score.
	currentScore, _ := computeScoreFromEvents(agentEvents, agentID)
	snap.CurrentScore = currentScore
	snap.IncidentCount = incidentCount

	// No incidents → not recovering, fully healthy.
	if lastIncidentIdx < 0 {
		snap.RecoveryProgress = 100.0
		snap.PreIncidentScore = currentScore
		snap.TargetScore = currentScore
		return snap
	}

	// Pre-incident score: the score BEFORE the last incident was applied.
	preIncidentScore := TrustScore(agentEvents[lastIncidentIdx].Data.ScoreBefore)
	if preIncidentScore <= 0 {
		// Fall back to a neutral baseline if the event didn't record it.
		preIncidentScore = 0.5
	}
	snap.PreIncidentScore = preIncidentScore
	snap.TargetScore = preIncidentScore

	// Post-incident metrics.
	postIncidentEvents := agentEvents[lastIncidentIdx+1:]
	consecutiveCleanMerges := countEventsByType(postIncidentEvents, EventMergeSuccess)
	snap.ConsecutiveCleanMerges = consecutiveCleanMerges

	lastIncidentTime := agentEvents[lastIncidentIdx].Timestamp
	daysSince := int(time.Since(lastIncidentTime).Hours() / 24)
	if daysSince < 0 {
		daysSince = 0
	}
	snap.DaysSinceLastIncident = daysSince

	// Compute trend from POST-incident events only — the incident drop itself
	// should not count as a downward trend.
	postTrend := computeScoreTrend(postIncidentEvents, SnapshotWindow30Days)
	snap.Trend = postTrend.Direction

	// Compute recovery progress percentage.
	// Progress = how close current score is to the pre-incident score,
	// boosted by clean merges and clean days.
	gap := float64(preIncidentScore) - float64(currentScore)
	if gap <= 0 {
		// Score already at or above pre-incident → fully recovered.
		snap.RecoveryProgress = 100.0
		snap.IsRecovering = false
		snap.EstimatedDaysToRecover = 0
		return snap
	}

	// Score-based progress: (current - post-incident floor) / (target - post-incident floor)
	postIncidentFloor := TrustScore(agentEvents[lastIncidentIdx].Data.ScoreAfter)
	scoreGap := float64(preIncidentScore) - float64(postIncidentFloor)
	var scoreProgress float64
	if scoreGap > 0 {
		scoreProgress = (float64(currentScore) - float64(postIncidentFloor)) / scoreGap
		if scoreProgress < 0 {
			scoreProgress = 0
		}
		if scoreProgress > 1 {
			scoreProgress = 1
		}
	} else {
		scoreProgress = 1.0
	}

	// Bonus from clean activity.
	mergeBonus := float64(consecutiveCleanMerges) * cfg.CleanMergeRecoveryRate / 100.0
	daysBonus := float64(daysSince) * cfg.DaysDecayRate / 100.0

	totalProgress := scoreProgress + mergeBonus + daysBonus
	if totalProgress > 1.0 {
		totalProgress = 1.0
	}
	if totalProgress < 0 {
		totalProgress = 0
	}
	snap.RecoveryProgress = totalProgress * 100.0

	// Determine if the agent is actively recovering.
	// Conditions: has had incidents, is trending up (or stable with clean merges),
	// and has at least the minimum clean merges since last incident.
	isUpward := postTrend.Direction == TrendUp ||
		(postTrend.Direction == TrendStable && consecutiveCleanMerges >= cfg.MinCleanMergesForRecovering)
	snap.IsRecovering = isUpward && snap.RecoveryProgress < 100.0

	// Estimate days to full recovery.
	if snap.IsRecovering && cfg.DaysDecayRate > 0 {
		remaining := 1.0 - totalProgress
		daysNeeded := int(remaining*100.0/cfg.DaysDecayRate) + 1
		snap.EstimatedDaysToRecover = daysNeeded
	} else if snap.RecoveryProgress >= 100.0 {
		snap.EstimatedDaysToRecover = 0
	}

	return snap
}

// =============================================================================
// Helpers
// =============================================================================

// findLastIncidentOrDemotion returns the index of the last incident or demotion
// event in the event slice, or -1 if none exist.
func findLastIncidentOrDemotion(events []TrustEvent) int {
	for i := len(events) - 1; i >= 0; i-- {
		switch events[i].EventType {
		case EventIncidentAttrib, EventIncidentPenalty, EventDemotion:
			return i
		}
	}
	return -1
}

// countIncidents counts all incident attribution events.
func countIncidents(events []TrustEvent) int {
	return countEventsByType(events, EventIncidentAttrib) +
		countEventsByType(events, EventIncidentPenalty)
}

// IsFullyRecovered returns true when an agent's recovery progress is 100%
// and they are no longer in an active recovery state.
func (s *RecoverySnapshot) IsFullyRecovered() bool {
	return s.RecoveryProgress >= 100.0 && !s.IsRecovering
}

// RecoveryHealthLabel returns a human-readable label for the recovery state.
func (s *RecoverySnapshot) RecoveryHealthLabel() string {
	if s.IncidentCount == 0 {
		return "healthy"
	}
	if s.IsFullyRecovered() {
		return "recovered"
	}
	if s.IsRecovering {
		switch {
		case s.RecoveryProgress >= 75:
			return "recovering-strong"
		case s.RecoveryProgress >= 50:
			return "recovering"
		case s.RecoveryProgress >= 25:
			return "recovering-slow"
		default:
			return "recovering-early"
		}
	}
	return "at-risk"
}
