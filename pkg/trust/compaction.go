package trust

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

// ---------------------------------------------------------------------------
// Ledger Compaction — spec §The Trust Ledger ("replay the ledger to verify any
// agent's current score"). The ledger grows unbounded without compaction.
// Old events are summarized into a compact snapshot, preserving score integrity.
// ---------------------------------------------------------------------------

// CompactionSummary is a single entry that replaces all old events for an agent.
// It captures the score state at the end of the compacted period so that
// ReplayToScore produces the same result after compaction.
type CompactionSummary struct {
	AgentID     string    `json:"agent_id"`
	ScoreBefore float64   `json:"score_before"` // agent's score at start of compacted period
	ScoreAfter  float64   `json:"score_after"`  // agent's score at end of compacted period
	EventCount  int       `json:"event_count"`  // number of events summarized
	PeriodStart time.Time `json:"period_start"` // timestamp of first compacted event
	PeriodEnd   time.Time `json:"period_end"`   // timestamp of last compacted event
	CompactedAt time.Time `json:"compacted_at"` // when the compaction was performed
}

// EventType for compaction summary entries in the ledger.
const EventCompactionSummary = "compaction_summary"

// CompactionConfig controls ledger compaction behavior.
type CompactionConfig struct {
	// MaxAge is the age threshold. Events older than this are candidates
	// for compaction. Default: 90 days.
	MaxAge time.Duration

	// MinEventsToCompact is the minimum number of old events per agent
	// required before compaction is worthwhile. Agents with fewer old
	// events keep them verbatim. Default: 10.
	MinEventsToCompact int
}

// DefaultCompactionConfig returns the default compaction configuration.
func DefaultCompactionConfig() CompactionConfig {
	return CompactionConfig{
		MaxAge:             90 * 24 * time.Hour,
		MinEventsToCompact: 10,
	}
}

// LedgerCompactor reduces JSONL trust ledger size by summarizing old events.
type LedgerCompactor struct {
	config CompactionConfig
}

// NewLedgerCompactor creates a LedgerCompactor with the given config.
// Pass DefaultCompactionConfig() for defaults.
func NewLedgerCompactor(config CompactionConfig) *LedgerCompactor {
	return &LedgerCompactor{config: config}
}

// CompactionResult describes the outcome of a compaction run.
type CompactionResult struct {
	OriginalEventCount  int                 `json:"original_event_count"`
	CompactedEventCount int                 `json:"compacted_event_count"` // events remaining after compaction
	SummariesCreated    int                 `json:"summaries_created"`
	SpaceSavedPercent   float64             `json:"space_saved_percent"`
	Summaries           []CompactionSummary `json:"summaries"`
	OutputPath          string              `json:"output_path"`
}

// Compact reads the ledger, partitions events by age, and writes a new ledger
// with a summary entry per agent prefixing their recent events.
//
// Events older than MaxAge (the "cutoff") are summarized. The summary captures
// the agent's score state so ReplayToScore produces the same result.
//
// The compacted ledger is written to outputPath. If outputPath is empty, the
// original path is used (in-place compaction — the original is renamed to
// path + ".bak" first).
func (c *LedgerCompactor) Compact(inputPath, outputPath string) (*CompactionResult, error) {
	events, err := Replay(inputPath)
	if err != nil {
		return nil, fmt.Errorf("read ledger for compaction: %w", err)
	}

	if len(events) == 0 {
		return &CompactionResult{
			OriginalEventCount:  0,
			CompactedEventCount: 0,
			SummariesCreated:    0,
			OutputPath:          outputPath,
		}, nil
	}

	cutoff := time.Now().UTC().Add(-c.config.MaxAge)

	// Partition events: old (before cutoff) are candidates for summarization.
	// Recent events are preserved verbatim by the rebuild loop below.
	var oldEvents []TrustEvent
	for _, evt := range events {
		if evt.Timestamp.Before(cutoff) {
			oldEvents = append(oldEvents, evt)
		}
	}

	// Group old events by agent.
	oldByAgent := make(map[string][]TrustEvent)
	for _, evt := range oldEvents {
		oldByAgent[evt.AgentID] = append(oldByAgent[evt.AgentID], evt)
	}

	// Create summaries for agents with enough old events.
	var summaries []CompactionSummary
	summarizedAgents := make(map[string]bool)

	for agentID, agentEvents := range oldByAgent {
		if len(agentEvents) < c.config.MinEventsToCompact {
			// Not enough events to justify compaction; keep them verbatim.
			continue
		}

		// Sort by timestamp ascending.
		sort.Slice(agentEvents, func(i, j int) bool {
			return agentEvents[i].Timestamp.Before(agentEvents[j].Timestamp)
		})

		summary := c.createSummary(agentID, agentEvents)
		summaries = append(summaries, summary)
		summarizedAgents[agentID] = true
	}

	// Build the new ledger: summaries first (sorted by agent), then all
	// recent events + non-summarized old events.
	var newEvents []TrustEvent

	// Add summaries as special events.
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].AgentID < summaries[j].AgentID
	})
	for _, s := range summaries {
		newEvents = append(newEvents, summaryToEvent(s))
	}

	// Add events that were NOT summarized (either recent or below threshold).
	for _, evt := range events {
		// Skip old events that were summarized.
		if evt.Timestamp.Before(cutoff) && summarizedAgents[evt.AgentID] {
			continue
		}
		// Skip any pre-existing compaction summaries (they'll be replaced).
		if evt.EventType == EventCompactionSummary {
			continue
		}
		newEvents = append(newEvents, evt)
	}

	// Resolve output path.
	if outputPath == "" {
		outputPath = inputPath
		// Back up original.
		bakPath := inputPath + ".bak"
		if err := os.Rename(inputPath, bakPath); err != nil {
			return nil, fmt.Errorf("backup original ledger: %w", err)
		}
	}

	// Write the compacted ledger.
	if err := c.writeLedger(outputPath, newEvents); err != nil {
		return nil, fmt.Errorf("write compacted ledger: %w", err)
	}

	origCount := len(events)
	newCount := len(newEvents)
	spaceSaved := 0.0
	if origCount > 0 {
		spaceSaved = float64(origCount-newCount) / float64(origCount) * 100.0
	}

	return &CompactionResult{
		OriginalEventCount:  origCount,
		CompactedEventCount: newCount,
		SummariesCreated:    len(summaries),
		SpaceSavedPercent:   spaceSaved,
		Summaries:           summaries,
		OutputPath:          outputPath,
	}, nil
}

// createSummary builds a CompactionSummary from a slice of old events for one
// agent. The events should be sorted by timestamp ascending.
func (c *LedgerCompactor) createSummary(agentID string, events []TrustEvent) CompactionSummary {
	scoreBefore := 0.5 // neutral starting score
	scoreAfter := 0.5

	for _, evt := range events {
		if evt.EventType == EventCompactionSummary {
			scoreBefore = evt.Data.ScoreAfter
			continue
		}
		scoreBefore = evt.Data.ScoreBefore
		scoreAfter = evt.Data.ScoreAfter
		if scoreAfter == 0 && scoreBefore != 0 {
			// Fall back to ScoreBefore if ScoreAfter isn't set.
			scoreAfter = scoreBefore
		}
	}

	return CompactionSummary{
		AgentID:     agentID,
		ScoreBefore: scoreBefore,
		ScoreAfter:  scoreAfter,
		EventCount:  len(events),
		PeriodStart: events[0].Timestamp,
		PeriodEnd:   events[len(events)-1].Timestamp,
		CompactedAt: time.Now().UTC(),
	}
}

// summaryToEvent converts a CompactionSummary into a TrustEvent that can be
// stored in the ledger. The EventType is EventCompactionSummary.
func summaryToEvent(s CompactionSummary) TrustEvent {
	return TrustEvent{
		AgentID:   s.AgentID,
		EventType: EventCompactionSummary,
		Timestamp: s.PeriodEnd,
		Data: EventData{
			ScoreBefore: s.ScoreBefore,
			ScoreAfter:  s.ScoreAfter,
			Delta:       s.ScoreAfter - s.ScoreBefore,
		},
	}
}

// writeLedger writes events to a JSONL file, one event per line.
func (c *LedgerCompactor) writeLedger(path string, events []TrustEvent) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, evt := range events {
		if err := enc.Encode(evt); err != nil {
			return fmt.Errorf("encode event: %w", err)
		}
	}
	return nil
}

// VerifyCompaction replays both the original and compacted ledgers and verifies
// that every agent's score is identical in both. Returns nil if all scores
// match, or an error listing the mismatches.
func VerifyCompaction(originalPath, compactedPath string) error {
	originalEvents, err := Replay(originalPath)
	if err != nil {
		return fmt.Errorf("read original: %w", err)
	}

	compactedEvents, err := Replay(compactedPath)
	if err != nil {
		return fmt.Errorf("read compacted: %w", err)
	}

	// Collect all agent IDs from both ledgers.
	agents := make(map[string]bool)
	for _, evt := range originalEvents {
		agents[evt.AgentID] = true
	}
	for _, evt := range compactedEvents {
		agents[evt.AgentID] = true
	}

	var mismatches []string
	for agentID := range agents {
		origScore, err := replayToScoreFromEvents(originalEvents, agentID)
		if err != nil {
			return fmt.Errorf("replay original for %s: %w", agentID, err)
		}

		compactedScore, err := replayToScoreFromEvents(compactedEvents, agentID)
		if err != nil {
			return fmt.Errorf("replay compacted for %s: %w", agentID, err)
		}

		// Allow small floating-point tolerance.
		diff := origScore - compactedScore
		if diff < 0 {
			diff = -diff
		}
		if diff > 0.001 {
			mismatches = append(mismatches, fmt.Sprintf(
				"agent %s: original=%.6f compacted=%.6f (diff=%.6f)",
				agentID, origScore, compactedScore, diff))
		}
	}

	if len(mismatches) > 0 {
		return fmt.Errorf("compaction verification failed:\n  %s",
			joinStrings(mismatches, "\n  "))
	}

	return nil
}

// replayToScoreFromEvents is like ReplayToScore but operates on an in-memory
// event slice instead of reading from a file. This avoids re-reading files
// during verification.
func replayToScoreFromEvents(events []TrustEvent, agentID string) (TrustScore, error) {
	var score TrustScore = 0.5 // neutral starting score
	var lastActive time.Time

	for _, evt := range events {
		if evt.AgentID != agentID {
			continue
		}

		switch evt.EventType {
		case EventMergeSuccess, EventReviewConsensus, EventHumanRating:
			score = TrustScore(evt.Data.ScoreAfter)
		case EventIncidentPenalty:
			score = ApplyIncidentPenalty(TrustScore(evt.Data.ScoreBefore), evt.Data.AttributionWt)
		case EventDecayApplied:
			score = ApplyInactivityDecay(TrustScore(evt.Data.ScoreBefore), evt.Data.DecayWeeks*DaysPerWeek)
		case EventTierChange, EventDemotion:
			score = TrustScore(evt.Data.ScoreAfter)
		case EventIncidentAttrib:
			score = TrustScore(evt.Data.ScoreAfter)
		case EventCompactionSummary:
			// The summary carries the authoritative score at compaction time.
			score = TrustScore(evt.Data.ScoreAfter)
		}
		lastActive = evt.Timestamp
	}

	// Apply inactivity decay since the last event.
	if !lastActive.IsZero() {
		daysSince := time.Since(lastActive).Hours() / 24
		if daysSince > InactivityGraceDays {
			score = ApplyInactivityDecay(score, int(daysSince))
		}
	}

	return score, nil
}

// joinStrings joins strings with a separator. Avoids importing strings for
// a single call.
func joinStrings(ss []string, sep string) string {
	if len(ss) == 0 {
		return ""
	}
	result := ss[0]
	for _, s := range ss[1:] {
		result += sep + s
	}
	return result
}

// NeedsCompaction checks if the ledger would benefit from compaction.
// Returns true if the ledger has more than threshold events and at least
// minOldPercent of them are older than the MaxAge cutoff.
func NeedsCompaction(path string, config CompactionConfig) (bool, error) {
	events, err := Replay(path)
	if err != nil {
		return false, err
	}

	if len(events) < config.MinEventsToCompact*2 {
		return false, nil
	}

	cutoff := time.Now().UTC().Add(-config.MaxAge)
	oldCount := 0
	for _, evt := range events {
		if evt.Timestamp.Before(cutoff) {
			oldCount++
		}
	}

	if oldCount == 0 {
		return false, nil
	}

	oldPercent := float64(oldCount) / float64(len(events)) * 100.0
	return oldPercent > 30.0, nil // compact if >30% are old
}

// Stats returns basic statistics about the ledger for compaction decisions.
type LedgerStats struct {
	TotalEvents   int            `json:"total_events"`
	UniqueAgents  int            `json:"unique_agents"`
	OldestEvent   time.Time      `json:"oldest_event"`
	NewestEvent   time.Time      `json:"newest_event"`
	EventsByType  map[string]int `json:"events_by_type"`
	EstimatedSize int64          `json:"estimated_size_bytes"`
}

// GetStats reads the ledger and returns summary statistics.
func GetStats(path string) (*LedgerStats, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	events, err := Replay(path)
	if err != nil {
		return nil, err
	}

	stats := &LedgerStats{
		TotalEvents:   len(events),
		EstimatedSize: info.Size(),
		EventsByType:  make(map[string]int),
	}

	agents := make(map[string]bool)
	for _, evt := range events {
		agents[evt.AgentID] = true
		stats.EventsByType[evt.EventType]++

		if stats.OldestEvent.IsZero() || evt.Timestamp.Before(stats.OldestEvent) {
			stats.OldestEvent = evt.Timestamp
		}
		if evt.Timestamp.After(stats.NewestEvent) {
			stats.NewestEvent = evt.Timestamp
		}
	}
	stats.UniqueAgents = len(agents)

	return stats, nil
}
