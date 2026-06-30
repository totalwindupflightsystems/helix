package negotiate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Tests for QueryHistory + FormatHistory — spec §13 (audit trail)
// ---------------------------------------------------------------------------

func writeTestTranscript(t *testing.T, dir, filename string, events []DebateEvent) {
	t.Helper()
	path := filepath.Join(dir, filename)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	for _, e := range events {
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			t.Fatalf("write event: %v", err)
		}
	}
}

func TestQueryHistory_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	r, err := QueryHistory(dir, HistoryQuery{})
	if err != nil {
		t.Fatalf("QueryHistory on empty dir: %v", err)
	}
	if len(r.Entries) != 0 {
		t.Errorf("empty dir should return 0 entries, got %d", len(r.Entries))
	}
}

func TestQueryHistory_NonexistentDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	r, err := QueryHistory(dir, HistoryQuery{})
	if err != nil {
		t.Fatalf("QueryHistory on nonexistent dir: %v", err)
	}
	if len(r.Entries) != 0 {
		t.Errorf("nonexistent dir should return 0 entries, got %d", len(r.Entries))
	}
}

func TestQueryHistory_SingleTranscript(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)

	writeTestTranscript(t, dir, "42-20260630T100000Z.jsonl", []DebateEvent{
		{Round: 0, Type: "conflict_detected", Agent: "agent-a", VerdictA: "APPROVED", Timestamp: ts},
		{Round: 0, Type: "conflict_detected", Agent: "agent-b", VerdictB: "REQUEST_CHANGES", Timestamp: ts.Add(1 * time.Second)},
		{Round: 1, Type: "argument", Agent: "agent-a", Body: "passing tests", EvidenceCount: 3, Timestamp: ts.Add(2 * time.Second)},
		{Round: 1, Type: "argument", Agent: "agent-b", Body: "security concern", EvidenceCount: 2, Timestamp: ts.Add(3 * time.Second)},
		{Type: "resolved", Outcome: "APPROVED", Timestamp: ts.Add(4 * time.Second)},
	})

	r, err := QueryHistory(dir, HistoryQuery{})
	if err != nil {
		t.Fatalf("QueryHistory: %v", err)
	}
	if len(r.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(r.Entries))
	}
	e := r.Entries[0]
	if len(e.Agents) != 2 {
		t.Errorf("expected 2 agents, got %d: %v", len(e.Agents), e.Agents)
	}
	if e.Outcome != "APPROVED" {
		t.Errorf("expected outcome APPROVED, got %q", e.Outcome)
	}
}

func TestQueryHistory_FilterByAgent(t *testing.T) {
	dir := t.TempDir()
	ts := time.Now().UTC()

	writeTestTranscript(t, dir, "1-t1.jsonl", []DebateEvent{
		{Type: "conflict_detected", Agent: "alice", Timestamp: ts},
		{Type: "argument", Agent: "bob", Timestamp: ts.Add(time.Second)},
	})
	writeTestTranscript(t, dir, "2-t2.jsonl", []DebateEvent{
		{Type: "conflict_detected", Agent: "charlie", Timestamp: ts},
		{Type: "argument", Agent: "dave", Timestamp: ts.Add(time.Second)},
	})

	r, err := QueryHistory(dir, HistoryQuery{Agent: "alice"})
	if err != nil {
		t.Fatalf("QueryHistory: %v", err)
	}
	if len(r.Entries) != 1 {
		t.Fatalf("filter alice should return 1 entry, got %d", len(r.Entries))
	}
	found := false
	for _, a := range r.Entries[0].Agents {
		if a == "alice" {
			found = true
		}
	}
	if !found {
		t.Errorf("returned entry doesn't contain alice: %v", r.Entries[0].Agents)
	}
}

func TestQueryHistory_FilterByPRNumber(t *testing.T) {
	dir := t.TempDir()
	ts := time.Now().UTC()

	writeTestTranscript(t, dir, "1.jsonl", []DebateEvent{
		{Type: "conflict_detected", Agent: "x", Timestamp: ts},
	})
	writeTestTranscript(t, dir, "2.jsonl", []DebateEvent{
		{Type: "conflict_detected", Agent: "y", Timestamp: ts},
	})

	// Query with PRNumber=999 → no match.
	r, err := QueryHistory(dir, HistoryQuery{PRNumber: 999})
	if err != nil {
		t.Fatalf("QueryHistory: %v", err)
	}
	if len(r.Entries) != 0 {
		t.Errorf("PRNumber=999 should return 0, got %d", len(r.Entries))
	}

	// Query with PRNumber=0 → matches all (0 = no filter).
	r2, _ := QueryHistory(dir, HistoryQuery{PRNumber: 0})
	if len(r2.Entries) != 2 {
		t.Errorf("PRNumber=0 should match all (no filter), got %d", len(r2.Entries))
	}
}

func TestQueryHistory_FilterByOutcome(t *testing.T) {
	dir := t.TempDir()
	ts := time.Now().UTC()

	writeTestTranscript(t, dir, "approved.jsonl", []DebateEvent{
		{Type: "resolved", Outcome: "APPROVED", Timestamp: ts},
	})
	writeTestTranscript(t, dir, "escalated.jsonl", []DebateEvent{
		{Type: "escalated", Timestamp: ts},
	})

	r, err := QueryHistory(dir, HistoryQuery{Outcome: "APPROVED"})
	if err != nil {
		t.Fatalf("QueryHistory: %v", err)
	}
	if len(r.Entries) != 1 {
		t.Fatalf("filter APPROVED should return 1, got %d", len(r.Entries))
	}
	if r.Entries[0].Outcome != "APPROVED" {
		t.Errorf("entry outcome = %q, want APPROVED", r.Entries[0].Outcome)
	}
}

func TestQueryHistory_FilterByTimeRange(t *testing.T) {
	dir := t.TempDir()

	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	new := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)

	writeTestTranscript(t, dir, "old.jsonl", []DebateEvent{
		{Type: "resolved", Outcome: "APPROVED", Timestamp: old},
	})
	writeTestTranscript(t, dir, "new.jsonl", []DebateEvent{
		{Type: "resolved", Outcome: "APPROVED", Timestamp: new},
	})

	// Only recent negotiations.
	cutoff := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	r, err := QueryHistory(dir, HistoryQuery{Since: cutoff})
	if err != nil {
		t.Fatalf("QueryHistory: %v", err)
	}
	if len(r.Entries) != 1 {
		t.Fatalf("Since filter should return 1, got %d", len(r.Entries))
	}
	if !r.Entries[0].StartedAt.Equal(new) {
		t.Errorf("expected the new entry, got started at %v", r.Entries[0].StartedAt)
	}
}

func TestQueryHistory_SortedByStartedAtDescending(t *testing.T) {
	dir := t.TempDir()

	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)

	writeTestTranscript(t, dir, "oldest.jsonl", []DebateEvent{{Type: "resolved", Timestamp: t1}})
	writeTestTranscript(t, dir, "newest.jsonl", []DebateEvent{{Type: "resolved", Timestamp: t3}})
	writeTestTranscript(t, dir, "middle.jsonl", []DebateEvent{{Type: "resolved", Timestamp: t2}})

	r, err := QueryHistory(dir, HistoryQuery{})
	if err != nil {
		t.Fatalf("QueryHistory: %v", err)
	}
	if len(r.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(r.Entries))
	}

	// Verify descending order.
	if !r.Entries[0].StartedAt.After(r.Entries[1].StartedAt) {
		t.Errorf("entries not sorted descending: [0]=%v should be after [1]=%v",
			r.Entries[0].StartedAt, r.Entries[1].StartedAt)
	}
	if !r.Entries[1].StartedAt.After(r.Entries[2].StartedAt) {
		t.Errorf("entries not sorted descending: [1]=%v should be after [2]=%v",
			r.Entries[1].StartedAt, r.Entries[2].StartedAt)
	}
}

func TestQueryHistory_Limit(t *testing.T) {
	dir := t.TempDir()

	for i := 0; i < 5; i++ {
		ts := time.Date(2026, 6, 1, i, 0, 0, 0, time.UTC)
		filename := string(rune('a'+i)) + ".jsonl"
		writeTestTranscript(t, dir, filename, []DebateEvent{{Type: "resolved", Timestamp: ts}})
	}

	r, err := QueryHistory(dir, HistoryQuery{Limit: 2})
	if err != nil {
		t.Fatalf("QueryHistory: %v", err)
	}
	if len(r.Entries) != 2 {
		t.Errorf("Limit=2 should return 2, got %d", len(r.Entries))
	}
}

func TestQueryHistory_SkipsNonJSONL(t *testing.T) {
	dir := t.TempDir()
	ts := time.Now().UTC()

	writeTestTranscript(t, dir, "valid.jsonl", []DebateEvent{
		{Type: "resolved", Timestamp: ts},
	})
	// Write a verdict file and state.json — should be skipped.
	_ = os.WriteFile(filepath.Join(dir, "1-verdict.md"), []byte("# Verdict"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "state.json"), []byte("{}"), 0644)

	r, err := QueryHistory(dir, HistoryQuery{})
	if err != nil {
		t.Fatalf("QueryHistory: %v", err)
	}
	if len(r.Entries) != 1 {
		t.Errorf("should only count .jsonl files, got %d", len(r.Entries))
	}
}

func TestQueryHistory_MalformedTranscriptSkipped(t *testing.T) {
	dir := t.TempDir()

	writeTestTranscript(t, dir, "good.jsonl", []DebateEvent{
		{Type: "resolved", Timestamp: time.Now()},
	})
	// Write a malformed JSONL file.
	_ = os.WriteFile(filepath.Join(dir, "bad.jsonl"), []byte("{not valid json\n"), 0644)

	r, err := QueryHistory(dir, HistoryQuery{})
	if err != nil {
		t.Fatalf("QueryHistory: %v", err)
	}
	if len(r.Entries) != 1 {
		t.Errorf("should skip malformed, got %d entries", len(r.Entries))
	}
}

func TestQueryHistory_ChimeraFlag(t *testing.T) {
	dir := t.TempDir()
	ts := time.Now().UTC()

	writeTestTranscript(t, dir, "with-chimera.jsonl", []DebateEvent{
		{Type: "deadlock", Timestamp: ts},
		{Type: "chimera_tiebreak", Verdict: "APPROVE", Timestamp: ts.Add(time.Second)},
		{Type: "resolved", Outcome: "APPROVE", Timestamp: ts.Add(2 * time.Second)},
	})

	r, err := QueryHistory(dir, HistoryQuery{})
	if err != nil {
		t.Fatalf("QueryHistory: %v", err)
	}
	if len(r.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(r.Entries))
	}
	if !r.Entries[0].HadChimera {
		t.Errorf("expected HadChimera=true")
	}
	if !r.Entries[0].HadDeadlock {
		t.Errorf("expected HadDeadlock=true")
	}
}

func TestQueryHistory_EscalatedFlag(t *testing.T) {
	dir := t.TempDir()
	ts := time.Now().UTC()

	writeTestTranscript(t, dir, "escalated.jsonl", []DebateEvent{
		{Type: "escalated", Timestamp: ts},
	})

	r, err := QueryHistory(dir, HistoryQuery{})
	if err != nil {
		t.Fatalf("QueryHistory: %v", err)
	}
	if len(r.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(r.Entries))
	}
	if !r.Entries[0].WasEscalated {
		t.Errorf("expected WasEscalated=true")
	}
}

func TestFormatHistory_Empty(t *testing.T) {
	r := &HistoryResult{Entries: []HistoryEntry{}}
	out := FormatHistory(r)
	if out != "No negotiations found." {
		t.Errorf("empty result should say 'No negotiations found.', got %q", out)
	}
}

func TestFormatHistory_Nil(t *testing.T) {
	out := FormatHistory(nil)
	if out != "No negotiations found." {
		t.Errorf("nil result should say 'No negotiations found.', got %q", out)
	}
}

func TestFormatHistory_WithEntries(t *testing.T) {
	r := &HistoryResult{
		Entries: []HistoryEntry{
			{
				PRNumber:  42,
				Agents:    []string{"alice", "bob"},
				Rounds:    3,
				Outcome:   "APPROVED",
				StartedAt: time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC),
			},
		},
		Total: 1,
	}
	out := FormatHistory(r)
	if out == "No negotiations found." {
		t.Errorf("should not be empty for 1 entry")
	}
	if !strings.Contains(out, "Found 1") {
		t.Errorf("header should contain 'Found 1'")
	}
}

func TestFormatHistory_LongAgentNames(t *testing.T) {
	r := &HistoryResult{
		Entries: []HistoryEntry{
			{
				PRNumber:  1,
				Agents:    []string{"very-long-agent-name-one", "very-long-agent-name-two"},
				Rounds:    2,
				StartedAt: time.Now(),
			},
		},
		Total: 1,
	}
	out := FormatHistory(r)
	if out == "" {
		t.Errorf("format should not be empty")
	}
}
