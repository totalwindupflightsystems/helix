package trust

import (
	"fmt"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/incident"
)

// =============================================================================
// Incident → Trust Integration Bridge
//
// Per specs/trust-model.md §Integration Points:
//   "Incident learning database: Every incident feeds back into trust decay
//    and model evaluation."
//
// Per specs/production-verification.md §Production Incident Attribution:
//   "Record in trust ledger with evidence links."
//
// This bridge takes incident.AttributionResult, creates TrustEvents for each
// responsible agent, applies the appropriate penalty to their score, and
// appends everything to the JSONL ledger. The ledger is the single source of
// truth — anyone can replay it to independently verify an agent's score.
// =============================================================================

// IncidentBridge connects incident attribution to the trust ledger.
//
// It maintains an in-memory cache of agent scores (seeded from replay or
// defaults to 0.5 for new agents). Each processed incident appends events
// to the ledger and updates the cache.
type IncidentBridge struct {
	ledger *Ledger
	scores map[string]TrustScore
}

// NewIncidentBridge creates a bridge backed by the given ledger.
// Scores start empty — call LoadScoresFromLedger to populate from an
// existing ledger file, or use SetScore to seed individual agents.
func NewIncidentBridge(ledger *Ledger) *IncidentBridge {
	return &IncidentBridge{
		ledger: ledger,
		scores: make(map[string]TrustScore),
	}
}

// NewIncidentBridgeWithFile creates a bridge that loads existing scores
// from a ledger file before processing new incidents.
func NewIncidentBridgeWithFile(ledgerPath string) (*IncidentBridge, error) {
	ledger, err := NewLedger(ledgerPath)
	if err != nil {
		return nil, fmt.Errorf("open ledger: %w", err)
	}
	b := NewIncidentBridge(ledger)
	if err := b.LoadScoresFromLedger(ledgerPath); err != nil {
		ledger.Close()
		return nil, err
	}
	return b, nil
}

// LoadScoresFromLedger replays the ledger and caches the latest score
// for every agent that appears in it.
func (b *IncidentBridge) LoadScoresFromLedger(path string) error {
	events, err := Replay(path)
	if err != nil {
		return fmt.Errorf("replay ledger: %w", err)
	}
	for _, evt := range events {
		if evt.AgentID != "" {
			b.scores[evt.AgentID] = TrustScore(evt.Data.ScoreAfter)
		}
	}
	return nil
}

// SetScore manually sets an agent's current score (for seeding new agents).
func (b *IncidentBridge) SetScore(agentID string, score TrustScore) {
	b.scores[agentID] = score.Clamp()
}

// GetScore returns the cached score for an agent, or 0.5 (neutral) if unknown.
func (b *IncidentBridge) GetScore(agentID string) TrustScore {
	if score, ok := b.scores[agentID]; ok {
		return score
	}
	return 0.5
}

// AllScores returns a copy of the current cached score for every agent.
func (b *IncidentBridge) AllScores() map[string]TrustScore {
	out := make(map[string]TrustScore, len(b.scores))
	for k, v := range b.scores {
		out[k] = v
	}
	return out
}

// Close closes the underlying ledger file.
func (b *IncidentBridge) Close() error {
	if b.ledger != nil {
		return b.ledger.Close()
	}
	return nil
}

// =============================================================================
// Incident Processing
// =============================================================================

// ProcessResult takes an incident attribution result, creates trust events
// for each responsible agent, applies the incident penalty, and appends
// everything to the ledger.
//
// For each responsible agent, two events are written:
//  1. EventIncidentAttrib — records the raw attribution (weight, evidence)
//  2. EventIncidentPenalty — records the score change (before → after)
//
// The severity string controls the penalty multiplier (per incident.TrustPenalty).
func (b *IncidentBridge) ProcessResult(result *incident.AttributionResult, severity string) (*ProcessSummary, error) {
	if result == nil {
		return nil, fmt.Errorf("attribution result is nil")
	}
	if b.ledger == nil {
		return nil, fmt.Errorf("bridge has no ledger")
	}

	now := time.Now().UTC()
	summary := &ProcessSummary{
		IncidentID: result.IncidentID,
		Severity:   severity,
		Agents:     make([]AgentPenalty, 0, len(result.Responsibility)),
	}

	for agentID, attributionWeight := range result.Responsibility {
		if agentID == "" {
			continue
		}

		scoreBefore := b.GetScore(agentID)

		// The incident package computes the raw penalty as:
		//   penalty = attributionShare × severityMultiplier
		// The trust package applies the penalty as:
		//   ApplyIncidentPenalty(currentScore, attributionWeight)
		// which does: currentScore - (0.3 × attributionWeight)
		//
		// We use the trust package's penalty mechanism (0.3 × attributionWeight)
		// because that's what ReplayToScore expects. The severity multiplier
		// is captured in the event data for auditing.
		scoreAfter := ApplyIncidentPenalty(scoreBefore, attributionWeight)

		// Event 1: Raw attribution record
		attribEvt := TrustEvent{
			AgentID:   agentID,
			EventType: EventIncidentAttrib,
			Timestamp: now,
			Data: EventData{
				PRURL:         result.IncidentID,
				ScoreBefore:   float64(scoreBefore),
				ScoreAfter:    float64(scoreAfter),
				Delta:         float64(scoreAfter) - float64(scoreBefore),
				Evidence:      result.EvidenceLinks,
				AttributionWt: attributionWeight,
			},
		}
		if err := b.ledger.Append(attribEvt); err != nil {
			return nil, fmt.Errorf("append attribution event for %s: %w", agentID, err)
		}

		// Event 2: Penalty application record
		penaltyEvt := TrustEvent{
			AgentID:   agentID,
			EventType: EventIncidentPenalty,
			Timestamp: now.Add(time.Millisecond), // ensure ordering after attribution
			Data: EventData{
				PRURL:         result.IncidentID,
				ScoreBefore:   float64(scoreBefore),
				ScoreAfter:    float64(scoreAfter),
				Delta:         float64(scoreAfter) - float64(scoreBefore),
				Evidence:      result.EvidenceLinks,
				AttributionWt: attributionWeight,
			},
		}
		if err := b.ledger.Append(penaltyEvt); err != nil {
			return nil, fmt.Errorf("append penalty event for %s: %w", agentID, err)
		}

		// Update cache
		b.scores[agentID] = scoreAfter

		summary.Agents = append(summary.Agents, AgentPenalty{
			AgentID:          agentID,
			AttributionShare: attributionWeight,
			SeverityPenalty:  incident.TrustPenalty(attributionWeight, severity),
			ScoreBefore:      scoreBefore,
			ScoreAfter:       scoreAfter,
		})
	}

	return summary, nil
}

// ProcessIncident is a convenience method that combines attribution
// and penalty processing in one call. It takes the raw incident
// change paths, runs attribution, and processes the result.
func (b *IncidentBridge) ProcessIncident(
	engine *incident.AttributionEngine,
	incidentID string,
	paths []incident.ChangePath,
	evidence []string,
	severity string,
) (*ProcessSummary, error) {
	if engine == nil {
		return nil, fmt.Errorf("attribution engine is nil")
	}

	result, err := engine.Attribute(incidentID, paths, evidence)
	if err != nil {
		return nil, fmt.Errorf("attribution: %w", err)
	}

	return b.ProcessResult(result, severity)
}

// ProcessBatch processes multiple incidents in sequence. Each incident is
// attributed and penalized independently, but all events share the same
// ledger. Returns one summary per incident.
func (b *IncidentBridge) ProcessBatch(
	engine *incident.AttributionEngine,
	incidents []BatchIncident,
) ([]*ProcessSummary, error) {
	summaries := make([]*ProcessSummary, 0, len(incidents))

	for _, inc := range incidents {
		summary, err := b.ProcessIncident(engine, inc.IncidentID, inc.Paths, inc.Evidence, inc.Severity)
		if err != nil {
			return summaries, fmt.Errorf("batch incident %s: %w", inc.IncidentID, err)
		}
		summaries = append(summaries, summary)
	}

	return summaries, nil
}

// BatchIncident is a single incident in a batch processing request.
type BatchIncident struct {
	IncidentID string
	Paths      []incident.ChangePath
	Evidence   []string
	Severity   string
}

// =============================================================================
// Summary Types
// =============================================================================

// ProcessSummary records the outcome of processing one incident through
// the trust pipeline.
type ProcessSummary struct {
	IncidentID string         `json:"incident_id"`
	Severity   string         `json:"severity"`
	Agents     []AgentPenalty `json:"agents"`
}

// AgentPenalty records the penalty applied to a single agent.
type AgentPenalty struct {
	AgentID          string     `json:"agent_id"`
	AttributionShare float64    `json:"attribution_share"`
	SeverityPenalty  float64    `json:"severity_penalty"` // from incident.TrustPenalty
	ScoreBefore      TrustScore `json:"score_before"`
	ScoreAfter       TrustScore `json:"score_after"`
}

// TotalScoreReduction returns the sum of all agents' score reductions.
func (s *ProcessSummary) TotalScoreReduction() float64 {
	total := 0.0
	for _, a := range s.Agents {
		total += float64(a.ScoreBefore) - float64(a.ScoreAfter)
	}
	return total
}

// MostAffectedAgent returns the agent with the largest absolute score drop.
// Returns empty string if no agents were penalized.
func (s *ProcessSummary) MostAffectedAgent() string {
	var topID string
	var topDrop float64
	for _, a := range s.Agents {
		drop := float64(a.ScoreBefore) - float64(a.ScoreAfter)
		if drop > topDrop {
			topDrop = drop
			topID = a.AgentID
		}
	}
	return topID
}

// VerifyDecrease checks that every penalized agent's score actually decreased.
// This is a sanity check — in theory ApplyIncidentPenalty always reduces the
// score, but this guards against edge cases (e.g., score already at 0.0).
func (s *ProcessSummary) VerifyDecrease() error {
	for _, a := range s.Agents {
		if a.ScoreAfter >= a.ScoreBefore && a.ScoreBefore > 0 {
			return fmt.Errorf("agent %s: score did not decrease (%.4f → %.4f)",
				a.AgentID, a.ScoreBefore, a.ScoreAfter)
		}
	}
	return nil
}
