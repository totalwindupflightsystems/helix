package negotiate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReplayTranscript_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	summary, err := ReplayTranscript(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summary.Events) != 0 {
		t.Errorf("expected 0 events, got %d", len(summary.Events))
	}
}

func TestReplayTranscript_FileNotFound(t *testing.T) {
	_, err := ReplayTranscript("/nonexistent/file.jsonl")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestReplayTranscript_FullDebate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "debate.jsonl")
	ts := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)

	events := []DebateEvent{
		{Round: 0, Type: "conflict_detected", VerdictA: "APPROVED", VerdictB: "REQUEST_CHANGES", Timestamp: ts},
		{Round: 1, Type: "argument", Agent: "agent-a", Body: "I approve", EvidenceCount: 3, Timestamp: ts.Add(30 * time.Second)},
		{Round: 1, Type: "argument", Agent: "agent-b", Body: "I disagree", EvidenceCount: 2, Timestamp: ts.Add(60 * time.Second)},
		{Round: 2, Type: "argument", Agent: "agent-a", Body: "Rebuttal", EvidenceCount: 2, Timestamp: ts.Add(90 * time.Second)},
		{Round: 2, Type: "argument", Agent: "agent-b", Body: "Counter", EvidenceCount: 2, Timestamp: ts.Add(120 * time.Second)},
		{Round: 3, Type: "argument", Agent: "agent-a", Body: "Final", EvidenceCount: 3, Timestamp: ts.Add(150 * time.Second)},
		{Round: 3, Type: "argument", Agent: "agent-b", Body: "Final counter", EvidenceCount: 2, Timestamp: ts.Add(180 * time.Second)},
		{Type: "deadlock", Timestamp: ts.Add(200 * time.Second)},
		{Type: "chimera_tiebreak", Verdict: "APPROVE", Timestamp: ts.Add(210 * time.Second)},
		{Type: "resolved", Outcome: "APPROVED", Timestamp: ts.Add(220 * time.Second)},
	}

	writeTranscriptFile(t, path, events)

	summary, err := ReplayTranscript(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(summary.Events) != 10 {
		t.Errorf("expected 10 events, got %d", len(summary.Events))
	}
	if summary.Rounds != 3 {
		t.Errorf("expected 3 rounds, got %d", summary.Rounds)
	}
	if !summary.HadDeadlock {
		t.Error("expected deadlock")
	}
	if !summary.HadChimera {
		t.Error("expected chimera tiebreak")
	}
	if summary.Outcome != "APPROVED" {
		t.Errorf("expected outcome APPROVED, got %s", summary.Outcome)
	}
	if summary.WasEscalated {
		t.Error("did not expect escalation")
	}
	// StartedAt should be from the first event
	if !summary.StartedAt.Equal(ts) {
		t.Errorf("expected started_at %v, got %v", ts, summary.StartedAt)
	}
	// EndedAt should be from the last event
	if !summary.EndedAt.Equal(ts.Add(220 * time.Second)) {
		t.Errorf("expected ended_at %v, got %v", ts.Add(220*time.Second), summary.EndedAt)
	}
}

func TestReplayTranscript_Concession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "concession.jsonl")
	ts := time.Now().UTC()

	events := []DebateEvent{
		{Round: 0, Type: "conflict_detected", Timestamp: ts},
		{Round: 1, Type: "argument", Agent: "agent-a", Timestamp: ts.Add(30 * time.Second)},
		{Round: 1, Type: "concession", Agent: "agent-b", Outcome: "APPROVED", Timestamp: ts.Add(60 * time.Second)},
		{Type: "resolved", Outcome: "APPROVED", Timestamp: ts.Add(70 * time.Second)},
	}

	writeTranscriptFile(t, path, events)

	summary, err := ReplayTranscript(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.HadDeadlock {
		t.Error("did not expect deadlock")
	}
	if summary.HadChimera {
		t.Error("did not expect chimera")
	}
	if summary.Outcome != "APPROVED" {
		t.Errorf("expected outcome APPROVED, got %s", summary.Outcome)
	}
}

func TestReplayTranscript_Escalated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "escalated.jsonl")
	ts := time.Now().UTC()

	events := []DebateEvent{
		{Round: 0, Type: "conflict_detected", Timestamp: ts},
		{Round: 1, Type: "argument", Agent: "agent-a", Timestamp: ts.Add(5 * time.Minute)},
		{Round: 1, Type: "argument", Agent: "agent-b", Timestamp: ts.Add(6 * time.Minute)},
		{Type: "escalated", Timestamp: ts.Add(31 * time.Minute)},
	}

	writeTranscriptFile(t, path, events)

	summary, err := ReplayTranscript(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !summary.WasEscalated {
		t.Error("expected escalation")
	}
	if summary.Outcome != "escalated" {
		t.Errorf("expected outcome escalated, got %s", summary.Outcome)
	}
}

func TestReplayTranscript_BlankLinesSkipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blanks.jsonl")
	ts := time.Now().UTC()

	content := `{"type":"conflict_detected","timestamp":"` + ts.Format(time.RFC3339) + `"}

{"type":"argument","agent":"a","round":1,"timestamp":"` + ts.Add(time.Second).Format(time.RFC3339) + `"}

`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	summary, err := ReplayTranscript(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summary.Events) != 2 {
		t.Errorf("expected 2 events, got %d", len(summary.Events))
	}
}

func TestReplayTranscript_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.jsonl")
	content := `{"type":"conflict_detected"}
not valid json
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ReplayTranscript(path)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestReplayTranscript_AgentsCollected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.jsonl")
	ts := time.Now().UTC()

	events := []DebateEvent{
		{Round: 1, Type: "argument", Agent: "wojons", Timestamp: ts},
		{Round: 1, Type: "argument", Agent: "llopez", Timestamp: ts.Add(time.Second)},
		{Round: 2, Type: "argument", Agent: "wojons", Timestamp: ts.Add(2 * time.Second)},
		{Round: 2, Type: "argument", Agent: "llopez", Timestamp: ts.Add(3 * time.Second)},
	}

	writeTranscriptFile(t, path, events)

	summary, err := ReplayTranscript(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(summary.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(summary.Agents))
	}
	found := map[string]bool{}
	for _, a := range summary.Agents {
		found[a] = true
	}
	if !found["wojons"] || !found["llopez"] {
		t.Errorf("expected wojons and llopez, got %v", summary.Agents)
	}
}

func TestWriteVerdictFile_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)

	v := &VerdictFile{
		PRNumber:  42,
		Outcome:   "APPROVED",
		Timestamp: ts,
		Agents: []Agent{
			{Name: "wojons", TrustLevel: 85, Tier: "veteran"},
			{Name: "llopez", TrustLevel: 72, Tier: "trusted"},
		},
		Winner:          "wojons",
		Loser:           "llopez",
		Reasoning:       "Agent B conceded in round 2.",
		WinningEvidence: []string{"Unit test coverage >80%", "CI passed"},
		Rounds:          3,
		HadChimera:      true,
		TieBreaker:      "APPROVE",
		Cost:            0.004,
	}

	path, err := WriteVerdictFile(dir, v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check filename convention.
	expectedName := "42-20260630T120000Z-verdict.md"
	if filepath.Base(path) != expectedName {
		t.Errorf("expected filename %s, got %s", expectedName, filepath.Base(path))
	}

	// Read and verify content.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read verdict file: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "# Negotiation Verdict") {
		t.Error("missing title")
	}
	if !strings.Contains(content, "#42") {
		t.Error("missing PR number")
	}
	if !strings.Contains(content, "wojons") {
		t.Error("missing winner agent")
	}
	if !strings.Contains(content, "(winner)") {
		t.Error("missing winner role")
	}
	if !strings.Contains(content, "(conceded)") {
		t.Error("missing loser role")
	}
	if !strings.Contains(content, "Chimera arbiter") {
		t.Error("missing tie-breaker info")
	}
	if !strings.Contains(content, "Unit test coverage") {
		t.Error("missing winning evidence")
	}
}

func TestWriteVerdictFile_NoChimera(t *testing.T) {
	dir := t.TempDir()
	ts := time.Now().UTC()

	v := &VerdictFile{
		PRNumber:   10,
		Outcome:    "APPROVED",
		Timestamp:  ts,
		Agents:     []Agent{{Name: "a", TrustLevel: 60, Tier: "observed"}},
		Winner:     "a",
		Rounds:     1,
		HadChimera: false,
	}

	path, err := WriteVerdictFile(dir, v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "None (resolved by concession)") {
		t.Error("expected concession text for no-chimera case")
	}
}

func TestWriteVerdictFile_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "subdir", "nested")
	ts := time.Now().UTC()

	v := &VerdictFile{
		PRNumber:  1,
		Outcome:   "APPROVED",
		Timestamp: ts,
	}

	path, err := WriteVerdictFile(dir, v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("verdict file not created in nested dir")
	}
}

func TestFormatVerdictMarkdown_AllFields(t *testing.T) {
	v := &VerdictFile{
		PRNumber:        99,
		Outcome:         "REJECTED",
		Timestamp:       time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC),
		Agents:          []Agent{{Name: "agent-x", TrustLevel: 90, Tier: "veteran"}},
		Winner:          "agent-x",
		Reasoning:       "Critical bug found.",
		WinningEvidence: []string{"Evidence A", "Evidence B"},
		Rounds:          3,
		HadChimera:      true,
		TieBreaker:      "REJECT",
		Cost:            0.008,
	}

	md := FormatVerdictMarkdown(v)

	if !strings.Contains(md, "REJECTED") {
		t.Error("missing outcome")
	}
	if !strings.Contains(md, "agent-x") {
		t.Error("missing agent")
	}
	if !strings.Contains(md, "Critical bug found") {
		t.Error("missing reasoning")
	}
	if !strings.Contains(md, "Evidence A") {
		t.Error("missing evidence")
	}
}

func TestWriteStateFile_CreatesJSON(t *testing.T) {
	dir := t.TempDir()

	state := &StateFile{
		Negotiations: []ActiveNegotiation{
			{
				PRNumber:   42,
				State:      StateRound1,
				Round:      1,
				Transcript: "/path/to/transcript.jsonl",
				StartedAt:  time.Now().UTC(),
			},
		},
		Updated: time.Now().UTC(),
	}

	path, err := WriteStateFile(dir, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if filepath.Base(path) != "state.json" {
		t.Errorf("expected state.json, got %s", filepath.Base(path))
	}

	// Read back and verify.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var read StateFile
	if err := json.Unmarshal(data, &read); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(read.Negotiations) != 1 {
		t.Fatalf("expected 1 negotiation, got %d", len(read.Negotiations))
	}
	if read.Negotiations[0].PRNumber != 42 {
		t.Errorf("expected PR 42, got %d", read.Negotiations[0].PRNumber)
	}
}

func TestLoadStateFile_ReadsJSON(t *testing.T) {
	dir := t.TempDir()

	state := &StateFile{
		Negotiations: []ActiveNegotiation{
			{PRNumber: 1, State: StateIdle, Round: 0},
		},
		Updated: time.Now().UTC(),
	}
	path, _ := WriteStateFile(dir, state)

	loaded, err := LoadStateFile(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(loaded.Negotiations) != 1 {
		t.Fatalf("expected 1 negotiation, got %d", len(loaded.Negotiations))
	}
	if loaded.Negotiations[0].PRNumber != 1 {
		t.Errorf("expected PR 1, got %d", loaded.Negotiations[0].PRNumber)
	}

	_ = path // path not asserted on
}

func TestLoadStateFile_NotFound(t *testing.T) {
	_, err := LoadStateFile("/nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent state file")
	}
}

func TestWriteStateFile_EmptyDir(t *testing.T) {
	state := &StateFile{
		Updated: time.Now().UTC(),
	}
	path, err := WriteStateFile("", state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = os.Remove(path) }()

	if filepath.Base(path) != "state.json" {
		t.Errorf("expected state.json, got %s", filepath.Base(path))
	}
}

func TestReplayTranscript_LargeLineBuffer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.jsonl")
	ts := time.Now().UTC()

	// Create a line with a very large body field (> 64KB, default scanner limit).
	largeBody := strings.Repeat("x", 100_000)
	events := []DebateEvent{
		{Type: "argument", Agent: "a", Body: largeBody, Round: 1, Timestamp: ts},
	}
	writeTranscriptFile(t, path, events)

	summary, err := ReplayTranscript(path)
	if err != nil {
		t.Fatalf("unexpected error on large line: %v", err)
	}
	if len(summary.Events) != 1 {
		t.Errorf("expected 1 event, got %d", len(summary.Events))
	}
}

// writeTranscriptFile writes a list of DebateEvents as JSONL.
func writeTranscriptFile(t *testing.T, path string, events []DebateEvent) {
	t.Helper()
	var buf strings.Builder
	for _, e := range events {
		data, err := json.Marshal(&e)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(buf.String()), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
}
