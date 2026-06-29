package trust

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// =============================================================================
// Trust Event Types
// =============================================================================

const (
	EventMergeSuccess     = "merge_success"
	EventIncidentAttrib   = "incident_attributed"
	EventReviewConsensus  = "review_consensus"
	EventHumanRating      = "human_rating"
	EventTierChange       = "tier_change"
	EventDecayApplied     = "decay_applied"
	EventIncidentPenalty  = "incident_penalty"
	EventDemotion         = "demotion"
)

// TrustEvent is one entry in the trust ledger (appendix-only JSONL).
type TrustEvent struct {
	AgentID   string    `json:"agent_id"`
	EventType string    `json:"event_type"`
	Timestamp time.Time `json:"timestamp"`
	Data      EventData `json:"data"`
}

// EventData holds the numeric and evidence fields for a trust event.
type EventData struct {
	PRURL          string   `json:"pr_url,omitempty"`
	ScoreBefore    float64  `json:"trust_score_before"`
	ScoreAfter     float64  `json:"trust_score_after"`
	Delta          float64  `json:"delta"`
	Evidence       []string `json:"evidence,omitempty"`
	OldTier        string   `json:"old_tier,omitempty"`
	NewTier        string   `json:"new_tier,omitempty"`
	AttributionWt  float64  `json:"attribution_weight,omitempty"`
	DecayWeeks     int      `json:"decay_weeks,omitempty"`
	ConsecutiveBad int      `json:"consecutive_days_below,omitempty"`
}

// =============================================================================
// Ledger — JSONL append + replay
// =============================================================================

// Ledger manages an append-only JSONL trust ledger file.
type Ledger struct {
	path string
	file *os.File
}

// NewLedger opens (or creates) a JSONL ledger at path for appending.
func NewLedger(path string) (*Ledger, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("open ledger: %w", err)
	}
	return &Ledger{path: path, file: f}, nil
}

// Close closes the ledger file.
func (l *Ledger) Close() error {
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// Append writes one trust event to the ledger.
func (l *Ledger) Append(evt TrustEvent) error {
	if l.file == nil {
		return fmt.Errorf("ledger not open")
	}
	line, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	_, err = fmt.Fprintln(l.file, string(line))
	if err != nil {
		return fmt.Errorf("write to ledger: %w", err)
	}
	return nil
}

// Replay reads the entire ledger from disk and returns all events in order.
// This is the deterministic replay path — anyone can verify an agent's score
// by replaying the full ledger.
func Replay(path string) ([]TrustEvent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // empty ledger is valid
		}
		return nil, fmt.Errorf("read ledger: %w", err)
	}

	var events []TrustEvent
	for _, line := range splitLines(string(data)) {
		if line == "" {
			continue
		}
		var evt TrustEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			return nil, fmt.Errorf("unmarshal event: %w", err)
		}
		events = append(events, evt)
	}
	return events, nil
}

// ReplayToScore replays the full ledger and computes the final trust score
// for a given agent. This is the canonical verification method — anyone who
// has the ledger file can independently reproduce an agent's score.
func ReplayToScore(path, agentID string) (TrustScore, error) {
	events, err := Replay(path)
	if err != nil {
		return 0, err
	}

	var score TrustScore = 0.5 // neutral starting score
	var lastActive time.Time

	for _, evt := range events {
		if evt.AgentID != agentID {
			continue
		}

		// Only track score from explicit score-change events.
		// We use the stored score_after as authoritative when available;
		// otherwise we simulate the delta.
		switch evt.EventType {
		case EventMergeSuccess, EventReviewConsensus, EventHumanRating:
			score = TrustScore(evt.Data.ScoreAfter)
		case EventIncidentPenalty:
			score = ApplyIncidentPenalty(TrustScore(evt.Data.ScoreBefore), evt.Data.AttributionWt)
		case EventDecayApplied:
			score = ApplyInactivityDecay(TrustScore(evt.Data.ScoreBefore), evt.Data.DecayWeeks*DaysPerWeek)
		case EventTierChange, EventDemotion:
			// Tier changes don't modify the score, just the tier label.
			score = TrustScore(evt.Data.ScoreAfter)
		case EventIncidentAttrib:
			// The raw attribution event; penalty is applied as a separate EventIncidentPenalty.
			score = TrustScore(evt.Data.ScoreAfter)
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

	return score, nil
}

// splitLines splits a string by newlines, handling both \n and \r\n.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
