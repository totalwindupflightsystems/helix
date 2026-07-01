package trust

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// LedgerCompactor tests
// ---------------------------------------------------------------------------

func writeTestLedgerPath(t *testing.T, path string, events []TrustEvent) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create test ledger: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, evt := range events {
		if err := enc.Encode(evt); err != nil {
			t.Fatalf("encode event: %v", err)
		}
	}
}

func makeOldEvent(agentID, eventType string, ts time.Time, scoreBefore, scoreAfter float64) TrustEvent {
	return TrustEvent{
		AgentID:   agentID,
		EventType: eventType,
		Timestamp: ts,
		Data: EventData{
			ScoreBefore: scoreBefore,
			ScoreAfter:  scoreAfter,
			Delta:       scoreAfter - scoreBefore,
		},
	}
}

func TestCompaction_SummarizesOldEvents(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "ledger.jsonl")
	outputPath := filepath.Join(dir, "compacted.jsonl")

	now := time.Now().UTC()
	// 15 old events (older than 90 days) for agent-1
	var events []TrustEvent
	for i := 0; i < 15; i++ {
		ts := now.AddDate(0, 0, -(100 + i)) // 100-114 days ago
		events = append(events, makeOldEvent("agent-1", EventMergeSuccess, ts,
			0.5+float64(i)*0.01, 0.5+float64(i+1)*0.01))
	}
	// 5 recent events for agent-1
	for i := 0; i < 5; i++ {
		ts := now.AddDate(0, 0, -(10 + i)) // 10-14 days ago
		events = append(events, makeOldEvent("agent-1", EventMergeSuccess, ts,
			0.65+float64(i)*0.01, 0.65+float64(i+1)*0.01))
	}

	writeTestLedgerPath(t, inputPath, events)

	config := DefaultCompactionConfig()
	compactor := NewLedgerCompactor(config)
	result, err := compactor.Compact(inputPath, outputPath)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}

	if result.OriginalEventCount != 20 {
		t.Errorf("expected 20 original events, got %d", result.OriginalEventCount)
	}
	if result.SummariesCreated != 1 {
		t.Errorf("expected 1 summary, got %d", result.SummariesCreated)
	}
	if len(result.Summaries) != 1 {
		t.Fatalf("expected 1 summary in result, got %d", len(result.Summaries))
	}

	s := result.Summaries[0]
	if s.AgentID != "agent-1" {
		t.Errorf("expected agent-1, got %s", s.AgentID)
	}
	if s.EventCount != 15 {
		t.Errorf("expected 15 events summarized, got %d", s.EventCount)
	}

	// Compacted ledger should have: 1 summary + 5 recent = 6 events
	if result.CompactedEventCount != 6 {
		t.Errorf("expected 6 compacted events, got %d", result.CompactedEventCount)
	}
}

func TestCompaction_PreservesScoreIntegrity(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "ledger.jsonl")
	outputPath := filepath.Join(dir, "compacted.jsonl")

	now := time.Now().UTC()
	// 12 old events with increasing scores
	var events []TrustEvent
	for i := 0; i < 12; i++ {
		ts := now.AddDate(0, 0, -(100 + i))
		events = append(events, makeOldEvent("agent-x", EventMergeSuccess, ts,
			0.5+float64(i)*0.02, 0.5+float64(i+1)*0.02))
	}
	// 3 recent events
	for i := 0; i < 3; i++ {
		ts := now.AddDate(0, 0, -(5 + i))
		events = append(events, makeOldEvent("agent-x", EventMergeSuccess, ts,
			0.74+float64(i)*0.02, 0.74+float64(i+1)*0.02))
	}

	writeTestLedgerPath(t, inputPath, events)

	config := DefaultCompactionConfig()
	compactor := NewLedgerCompactor(config)
	_, err := compactor.Compact(inputPath, outputPath)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}

	// Verify scores match
	err = VerifyCompaction(inputPath, outputPath)
	if err != nil {
		t.Errorf("score integrity check failed: %v", err)
	}
}

func TestCompaction_MultipleAgents(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "ledger.jsonl")
	outputPath := filepath.Join(dir, "compacted.jsonl")

	now := time.Now().UTC()
	var events []TrustEvent

	// Agent A: 15 old events
	for i := 0; i < 15; i++ {
		ts := now.AddDate(0, 0, -(100 + i))
		events = append(events, makeOldEvent("agent-a", EventMergeSuccess, ts, 0.5, 0.55))
	}
	// Agent B: 15 old events
	for i := 0; i < 15; i++ {
		ts := now.AddDate(0, 0, -(100 + i))
		events = append(events, makeOldEvent("agent-b", EventReviewConsensus, ts, 0.6, 0.65))
	}
	// Agent C: 3 old events (below threshold, should NOT be compacted)
	for i := 0; i < 3; i++ {
		ts := now.AddDate(0, 0, -(100 + i))
		events = append(events, makeOldEvent("agent-c", EventHumanRating, ts, 0.7, 0.72))
	}
	// Recent events for all
	for _, agent := range []string{"agent-a", "agent-b", "agent-c"} {
		events = append(events, makeOldEvent(agent, EventMergeSuccess, now.AddDate(0, 0, -5), 0.7, 0.75))
	}

	writeTestLedgerPath(t, inputPath, events)

	config := DefaultCompactionConfig()
	compactor := NewLedgerCompactor(config)
	result, err := compactor.Compact(inputPath, outputPath)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}

	if result.SummariesCreated != 2 {
		t.Errorf("expected 2 summaries (agent-a and agent-b), got %d", result.SummariesCreated)
	}

	// Agent C's old events should be preserved verbatim (below threshold)
	compactedEvents, _ := Replay(outputPath)
	agentCOldCount := 0
	for _, evt := range compactedEvents {
		if evt.AgentID == "agent-c" && evt.Timestamp.Before(now.AddDate(0, 0, -90)) {
			agentCOldCount++
		}
	}
	if agentCOldCount != 3 {
		t.Errorf("expected 3 preserved old events for agent-c, got %d", agentCOldCount)
	}
}

func TestCompaction_EmptyLedger(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "empty.jsonl")
	outputPath := filepath.Join(dir, "out.jsonl")

	// Create empty file
	if err := os.WriteFile(inputPath, []byte{}, 0644); err != nil {
		t.Fatalf("create empty ledger: %v", err)
	}

	compactor := NewLedgerCompactor(DefaultCompactionConfig())
	result, err := compactor.Compact(inputPath, outputPath)
	if err != nil {
		t.Fatalf("compact empty ledger: %v", err)
	}

	if result.OriginalEventCount != 0 {
		t.Errorf("expected 0 events, got %d", result.OriginalEventCount)
	}
	if result.SummariesCreated != 0 {
		t.Errorf("expected 0 summaries, got %d", result.SummariesCreated)
	}
}

func TestCompaction_AllRecentEvents(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "ledger.jsonl")
	outputPath := filepath.Join(dir, "compacted.jsonl")

	now := time.Now().UTC()
	var events []TrustEvent
	for i := 0; i < 20; i++ {
		ts := now.AddDate(0, 0, -(i + 1)) // 1-20 days ago, all recent
		events = append(events, makeOldEvent("agent-1", EventMergeSuccess, ts, 0.5, 0.6))
	}

	writeTestLedgerPath(t, inputPath, events)

	compactor := NewLedgerCompactor(DefaultCompactionConfig())
	result, err := compactor.Compact(inputPath, outputPath)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}

	if result.SummariesCreated != 0 {
		t.Errorf("expected 0 summaries for all-recent events, got %d", result.SummariesCreated)
	}
	if result.CompactedEventCount != 20 {
		t.Errorf("expected 20 events preserved, got %d", result.CompactedEventCount)
	}
}

func TestCompaction_InPlace(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "ledger.jsonl")

	now := time.Now().UTC()
	var events []TrustEvent
	for i := 0; i < 15; i++ {
		ts := now.AddDate(0, 0, -(100 + i))
		events = append(events, makeOldEvent("agent-1", EventMergeSuccess, ts, 0.5, 0.55))
	}
	// Add recent events
	for i := 0; i < 5; i++ {
		ts := now.AddDate(0, 0, -(5 + i))
		events = append(events, makeOldEvent("agent-1", EventMergeSuccess, ts, 0.6, 0.65))
	}

	writeTestLedgerPath(t, inputPath, events)

	// In-place compaction (outputPath = "")
	compactor := NewLedgerCompactor(DefaultCompactionConfig())
	result, err := compactor.Compact(inputPath, "")
	if err != nil {
		t.Fatalf("in-place compact: %v", err)
	}

	// Backup should exist
	bakPath := inputPath + ".bak"
	if _, err := os.Stat(bakPath); os.IsNotExist(err) {
		t.Error("backup file should exist after in-place compaction")
	}

	// Compacted ledger should be at inputPath
	if result.OutputPath != inputPath {
		t.Errorf("expected output path %s, got %s", inputPath, result.OutputPath)
	}
}

func TestCompaction_BelowMinEventsThreshold(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "ledger.jsonl")
	outputPath := filepath.Join(dir, "compacted.jsonl")

	now := time.Now().UTC()
	var events []TrustEvent
	// Only 5 old events (below default MinEventsToCompact=10)
	for i := 0; i < 5; i++ {
		ts := now.AddDate(0, 0, -(100 + i))
		events = append(events, makeOldEvent("agent-1", EventMergeSuccess, ts, 0.5, 0.55))
	}

	writeTestLedgerPath(t, inputPath, events)

	compactor := NewLedgerCompactor(DefaultCompactionConfig())
	result, err := compactor.Compact(inputPath, outputPath)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}

	if result.SummariesCreated != 0 {
		t.Errorf("expected 0 summaries (below threshold), got %d", result.SummariesCreated)
	}
	// All events should be preserved
	if result.CompactedEventCount != 5 {
		t.Errorf("expected 5 events preserved, got %d", result.CompactedEventCount)
	}
}

func TestCompaction_CustomConfig(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "ledger.jsonl")
	outputPath := filepath.Join(dir, "compacted.jsonl")

	now := time.Now().UTC()
	var events []TrustEvent
	// 5 old events (below default 10 threshold, but custom config sets 3)
	for i := 0; i < 5; i++ {
		ts := now.AddDate(0, 0, -(100 + i))
		events = append(events, makeOldEvent("agent-1", EventMergeSuccess, ts, 0.5, 0.55))
	}
	for i := 0; i < 3; i++ {
		ts := now.AddDate(0, 0, -(5 + i))
		events = append(events, makeOldEvent("agent-1", EventMergeSuccess, ts, 0.6, 0.65))
	}

	writeTestLedgerPath(t, inputPath, events)

	config := CompactionConfig{
		MaxAge:             90 * 24 * time.Hour,
		MinEventsToCompact: 3, // lower threshold
	}
	compactor := NewLedgerCompactor(config)
	result, err := compactor.Compact(inputPath, outputPath)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}

	if result.SummariesCreated != 1 {
		t.Errorf("expected 1 summary with custom config (threshold=3), got %d", result.SummariesCreated)
	}
}

func TestCompaction_SpaceSavedPercent(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "ledger.jsonl")
	outputPath := filepath.Join(dir, "compacted.jsonl")

	now := time.Now().UTC()
	var events []TrustEvent
	// 20 old + 5 recent
	for i := 0; i < 20; i++ {
		ts := now.AddDate(0, 0, -(100 + i))
		events = append(events, makeOldEvent("agent-1", EventMergeSuccess, ts, 0.5, 0.55))
	}
	for i := 0; i < 5; i++ {
		ts := now.AddDate(0, 0, -(5 + i))
		events = append(events, makeOldEvent("agent-1", EventMergeSuccess, ts, 0.6, 0.65))
	}

	writeTestLedgerPath(t, inputPath, events)

	compactor := NewLedgerCompactor(DefaultCompactionConfig())
	result, err := compactor.Compact(inputPath, outputPath)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}

	// 25 original, 6 compacted (1 summary + 5 recent) → ~76% saved
	if result.SpaceSavedPercent < 50.0 {
		t.Errorf("expected >50%% space saved, got %.1f%%", result.SpaceSavedPercent)
	}
}

func TestVerifyCompaction_NoMismatch(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "ledger.jsonl")
	outputPath := filepath.Join(dir, "compacted.jsonl")

	now := time.Now().UTC()
	var events []TrustEvent
	for i := 0; i < 15; i++ {
		ts := now.AddDate(0, 0, -(100 + i))
		events = append(events, makeOldEvent("agent-1", EventMergeSuccess, ts, 0.5, 0.55))
	}
	for i := 0; i < 5; i++ {
		ts := now.AddDate(0, 0, -(5 + i))
		events = append(events, makeOldEvent("agent-1", EventMergeSuccess, ts, 0.6, 0.65))
	}

	writeTestLedgerPath(t, inputPath, events)

	compactor := NewLedgerCompactor(DefaultCompactionConfig())
	_, err := compactor.Compact(inputPath, outputPath)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}

	err = VerifyCompaction(inputPath, outputPath)
	if err != nil {
		t.Errorf("expected no verification errors, got: %v", err)
	}
}

func TestVerifyCompaction_MultipleAgentsNoMismatch(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "ledger.jsonl")
	outputPath := filepath.Join(dir, "compacted.jsonl")

	now := time.Now().UTC()
	var events []TrustEvent
	for _, agent := range []string{"a", "b", "c"} {
		for i := 0; i < 15; i++ {
			ts := now.AddDate(0, 0, -(100 + i))
			events = append(events, makeOldEvent(agent, EventMergeSuccess, ts,
				0.5+float64(i)*0.01, 0.5+float64(i+1)*0.01))
		}
		for i := 0; i < 5; i++ {
			ts := now.AddDate(0, 0, -(5 + i))
			events = append(events, makeOldEvent(agent, EventMergeSuccess, ts,
				0.65+float64(i)*0.01, 0.65+float64(i+1)*0.01))
		}
	}

	writeTestLedgerPath(t, inputPath, events)

	compactor := NewLedgerCompactor(DefaultCompactionConfig())
	_, err := compactor.Compact(inputPath, outputPath)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}

	err = VerifyCompaction(inputPath, outputPath)
	if err != nil {
		t.Errorf("expected no verification errors, got: %v", err)
	}
}

func TestNeedsCompaction_True(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.jsonl")

	now := time.Now().UTC()
	var events []TrustEvent
	// 20 old events (>30% threshold)
	for i := 0; i < 20; i++ {
		ts := now.AddDate(0, 0, -(100 + i))
		events = append(events, makeOldEvent("agent-1", EventMergeSuccess, ts, 0.5, 0.55))
	}
	// 5 recent
	for i := 0; i < 5; i++ {
		ts := now.AddDate(0, 0, -(5 + i))
		events = append(events, makeOldEvent("agent-1", EventMergeSuccess, ts, 0.6, 0.65))
	}

	writeTestLedgerPath(t, path, events)

	needs, err := NeedsCompaction(path, DefaultCompactionConfig())
	if err != nil {
		t.Fatalf("NeedsCompaction: %v", err)
	}
	if !needs {
		t.Error("expected NeedsCompaction=true for >30% old events")
	}
}

func TestNeedsCompaction_False(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.jsonl")

	now := time.Now().UTC()
	var events []TrustEvent
	// All recent events
	for i := 0; i < 20; i++ {
		ts := now.AddDate(0, 0, -(i + 1))
		events = append(events, makeOldEvent("agent-1", EventMergeSuccess, ts, 0.5, 0.55))
	}

	writeTestLedgerPath(t, path, events)

	needs, err := NeedsCompaction(path, DefaultCompactionConfig())
	if err != nil {
		t.Fatalf("NeedsCompaction: %v", err)
	}
	if needs {
		t.Error("expected NeedsCompaction=false for all-recent events")
	}
}

func TestNeedsCompaction_TooFewEvents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.jsonl")

	now := time.Now().UTC()
	// Only 5 events (below threshold)
	events := []TrustEvent{
		makeOldEvent("a", EventMergeSuccess, now.AddDate(0, 0, -200), 0.5, 0.55),
		makeOldEvent("a", EventMergeSuccess, now.AddDate(0, 0, -100), 0.5, 0.55),
		makeOldEvent("a", EventMergeSuccess, now.AddDate(0, 0, -5), 0.5, 0.55),
	}

	writeTestLedgerPath(t, path, events)

	needs, err := NeedsCompaction(path, DefaultCompactionConfig())
	if err != nil {
		t.Fatalf("NeedsCompaction: %v", err)
	}
	if needs {
		t.Error("expected NeedsCompaction=false for too few events")
	}
}

func TestGetStats(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.jsonl")

	now := time.Now().UTC()
	events := []TrustEvent{
		makeOldEvent("a", EventMergeSuccess, now.AddDate(0, 0, -30), 0.5, 0.55),
		makeOldEvent("a", EventReviewConsensus, now.AddDate(0, 0, -20), 0.55, 0.60),
		makeOldEvent("b", EventMergeSuccess, now.AddDate(0, 0, -10), 0.5, 0.65),
		makeOldEvent("b", EventHumanRating, now.AddDate(0, 0, -5), 0.65, 0.70),
	}

	writeTestLedgerPath(t, path, events)

	stats, err := GetStats(path)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}

	if stats.TotalEvents != 4 {
		t.Errorf("expected 4 total events, got %d", stats.TotalEvents)
	}
	if stats.UniqueAgents != 2 {
		t.Errorf("expected 2 unique agents, got %d", stats.UniqueAgents)
	}
	if stats.EventsByType[EventMergeSuccess] != 2 {
		t.Errorf("expected 2 merge_success events, got %d", stats.EventsByType[EventMergeSuccess])
	}
	if stats.EstimatedSize <= 0 {
		t.Error("expected positive file size")
	}
}

func TestGetStats_EmptyLedger(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatalf("create empty ledger: %v", err)
	}

	stats, err := GetStats(path)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}

	if stats.TotalEvents != 0 {
		t.Errorf("expected 0 events, got %d", stats.TotalEvents)
	}
}

func TestCompaction_ReplacesPreviousSummary(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "ledger.jsonl")
	outputPath := filepath.Join(dir, "compacted.jsonl")

	now := time.Now().UTC()
	// First, a pre-existing compaction summary in the ledger
	prevSummary := TrustEvent{
		AgentID:   "agent-1",
		EventType: EventCompactionSummary,
		Timestamp: now.AddDate(0, 0, -95),
		Data: EventData{
			ScoreBefore: 0.4,
			ScoreAfter:  0.5,
		},
	}
	var events []TrustEvent
	events = append(events, prevSummary)
	// 15 old events (after the pre-existing summary)
	for i := 0; i < 15; i++ {
		ts := now.AddDate(0, 0, -(100 + i))
		events = append(events, makeOldEvent("agent-1", EventMergeSuccess, ts, 0.5, 0.55))
	}
	// 5 recent events
	for i := 0; i < 5; i++ {
		ts := now.AddDate(0, 0, -(5 + i))
		events = append(events, makeOldEvent("agent-1", EventMergeSuccess, ts, 0.6, 0.65))
	}

	writeTestLedgerPath(t, inputPath, events)

	compactor := NewLedgerCompactor(DefaultCompactionConfig())
	_, err := compactor.Compact(inputPath, outputPath)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}

	// Should have only 1 summary for agent-1 (the old one is replaced)
	summaryCount := 0
	compactedEvents, _ := Replay(outputPath)
	for _, evt := range compactedEvents {
		if evt.EventType == EventCompactionSummary && evt.AgentID == "agent-1" {
			summaryCount++
		}
	}
	if summaryCount != 1 {
		t.Errorf("expected exactly 1 summary for agent-1, got %d", summaryCount)
	}
}

func TestCompaction_CompactionSummaryInReplayToScore(t *testing.T) {
	// Verify that replayToScoreFromEvents correctly handles compaction summaries.
	events := []TrustEvent{
		{
			AgentID:   "test-agent",
			EventType: EventCompactionSummary,
			Timestamp: time.Now().AddDate(0, 0, -95),
			Data: EventData{
				ScoreBefore: 0.3,
				ScoreAfter:  0.8,
			},
		},
		{
			AgentID:   "test-agent",
			EventType: EventMergeSuccess,
			Timestamp: time.Now().AddDate(0, 0, -5),
			Data: EventData{
				ScoreBefore: 0.8,
				ScoreAfter:  0.85,
			},
		},
	}

	score, err := replayToScoreFromEvents(events, "test-agent")
	if err != nil {
		t.Fatalf("replay: %v", err)
	}

	// The summary sets score to 0.8, then the merge event sets it to 0.85.
	if score != 0.85 {
		t.Errorf("expected score 0.85, got %.4f", float64(score))
	}
}

func TestCompactionResult_SpaceSavedCalculations(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "ledger.jsonl")
	outputPath := filepath.Join(dir, "compacted.jsonl")

	now := time.Now().UTC()
	// 50 old + 5 recent = 55 total → 1 summary + 5 recent = 6 compacted
	var events []TrustEvent
	for i := 0; i < 50; i++ {
		ts := now.AddDate(0, 0, -(100 + i))
		events = append(events, makeOldEvent("agent-1", EventMergeSuccess, ts, 0.5, 0.55))
	}
	for i := 0; i < 5; i++ {
		ts := now.AddDate(0, 0, -(5 + i))
		events = append(events, makeOldEvent("agent-1", EventMergeSuccess, ts, 0.6, 0.65))
	}

	writeTestLedgerPath(t, inputPath, events)

	compactor := NewLedgerCompactor(DefaultCompactionConfig())
	result, err := compactor.Compact(inputPath, outputPath)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}

	// (55 - 6) / 55 = 89.09%
	if result.SpaceSavedPercent < 85.0 {
		t.Errorf("expected >85%% space saved, got %.1f%%", result.SpaceSavedPercent)
	}
}
