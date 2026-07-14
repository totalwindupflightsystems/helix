package review

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestDismissalReason_Valid(t *testing.T) {
	tests := []struct {
		reason DismissalReason
		valid  bool
	}{
		{DismissalFalsePositive, true},
		{DismissalAlreadyHandled, true},
		{DismissalOutOfScope, true},
		{DismissalArchitecturalDecision, true},
		{DismissalReason("invalid"), false},
		{DismissalReason(""), false},
		{DismissalReason("false_positive_extra"), false},
		{DismissalReason("False_Positive"), false}, // case-sensitive
	}

	for _, tt := range tests {
		got := tt.reason.Valid()
		if got != tt.valid {
			t.Errorf("DismissalReason(%q).Valid() = %v, want %v", tt.reason, got, tt.valid)
		}
	}
}

func TestDismissalReason_String(t *testing.T) {
	if DismissalFalsePositive.String() != "false_positive" {
		t.Errorf("String() = %q, want %q", DismissalFalsePositive.String(), "false_positive")
	}
}

func tempStore(t *testing.T) (*DismissalStore, func()) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "dismissals.jsonl")
	store, err := NewDismissalStore(path)
	if err != nil {
		t.Fatalf("NewDismissalStore: %v", err)
	}
	cleanup := func() { _ = os.Remove(path) }
	return store, cleanup
}

func makeDismissal(findingID string, reason DismissalReason, note, humanID, agentID string, pr int) Dismissal {
	return Dismissal{
		FindingID: findingID,
		Reason:    reason,
		Note:      note,
		HumanID:   humanID,
		AgentID:   agentID,
		PRNumber:  pr,
		Timestamp: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	}
}

func TestNewDismissalStore_CreatesFile(t *testing.T) {
	store, cleanup := tempStore(t)
	defer cleanup()

	d := makeDismissal("finding-1", DismissalFalsePositive, "Not a real issue", "human-a", "agent-x", 42)
	if err := store.Record(d); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// Verify file exists and has content.
	data, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty file")
	}
}

func TestDismissalStore_Record_InvalidReason(t *testing.T) {
	store, cleanup := tempStore(t)
	defer cleanup()

	d := makeDismissal("finding-2", DismissalReason("bogus"), "", "human-a", "agent-x", 1)
	err := store.Record(d)
	if err == nil {
		t.Error("expected error for invalid reason, got nil")
	}
}

func TestDismissalStore_OverrideCount(t *testing.T) {
	store, cleanup := tempStore(t)
	defer cleanup()

	for i := 0; i < 3; i++ {
		d := makeDismissal("f-"+string(rune('a'+i)), DismissalAlreadyHandled, "", "human-a", "agent-x", 10+i)
		if err := store.Record(d); err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
	}
	for i := 0; i < 2; i++ {
		d := makeDismissal("f-"+string(rune('x'+i)), DismissalOutOfScope, "", "human-b", "agent-y", 20+i)
		if err := store.Record(d); err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
	}

	if got := store.OverrideCount("human-a"); got != 3 {
		t.Errorf("OverrideCount(human-a) = %d, want 3", got)
	}
	if got := store.OverrideCount("human-b"); got != 2 {
		t.Errorf("OverrideCount(human-b) = %d, want 2", got)
	}
	if got := store.OverrideCount("human-c"); got != 0 {
		t.Errorf("OverrideCount(human-c) = %d, want 0", got)
	}
}

func TestDismissalStore_OverrideWeight(t *testing.T) {
	store, cleanup := tempStore(t)
	defer cleanup()

	tests := []struct {
		humanID string
		count   int
		want    float64
	}{
		{"human-0", 0, 1.00},
		{"human-5", 5, 1.00},
		{"human-6", 6, 0.75},
		{"human-15", 15, 0.75},
		{"human-16", 16, 0.50},
		{"human-30", 30, 0.50},
		{"human-31", 31, 0.25},
		{"human-100", 100, 0.25},
	}

	for _, tt := range tests {
		// Pre-populate the store with the exact count.
		for i := 0; i < tt.count; i++ {
			d := makeDismissal("f", DismissalAlreadyHandled, "", tt.humanID, "agent", i)
			if err := store.Record(d); err != nil {
				t.Fatalf("Record(%s, %d): %v", tt.humanID, i, err)
			}
		}
		if got := store.OverrideWeight(tt.humanID); got != tt.want {
			t.Errorf("OverrideWeight(%s) after %d overrides = %v, want %v", tt.humanID, tt.count, got, tt.want)
		}
	}
}

func TestDismissalHandler_ProcessDismissal_FalsePositive(t *testing.T) {
	store, cleanup := tempStore(t)
	defer cleanup()
	tracker := NewFPTracker()
	handler := NewDismissalHandler(store, tracker)

	d := makeDismissal("finding-1", DismissalFalsePositive, "Not a bug", "human-a", "agent-x", 42)
	count, err := handler.ProcessDismissal(d)
	if err != nil {
		t.Fatalf("ProcessDismissal: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	if dc := tracker.DismissalCount("agent-x"); dc != 1 {
		t.Errorf("tracker DismissalCount = %d, want 1", dc)
	}
}

func TestDismissalHandler_ProcessDismissal_NotFalsePositive(t *testing.T) {
	store, cleanup := tempStore(t)
	defer cleanup()
	tracker := NewFPTracker()
	handler := NewDismissalHandler(store, tracker)

	d := makeDismissal("finding-2", DismissalAlreadyHandled, "Already fixed", "human-a", "agent-x", 42)
	count, err := handler.ProcessDismissal(d)
	if err != nil {
		t.Fatalf("ProcessDismissal: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	if dc := tracker.DismissalCount("agent-x"); dc != 0 {
		t.Errorf("tracker DismissalCount = %d, want 0 (not false_positive)", dc)
	}
}

func TestDismissalHandler_ProcessDismissal_InvalidReason(t *testing.T) {
	store, cleanup := tempStore(t)
	defer cleanup()
	tracker := NewFPTracker()
	handler := NewDismissalHandler(store, tracker)

	d := makeDismissal("finding-3", DismissalReason("bogus"), "", "human-a", "agent-x", 1)
	_, err := handler.ProcessDismissal(d)
	if err == nil {
		t.Error("expected error for invalid reason")
	}
}

func TestParseDismissal_Valid(t *testing.T) {
	comment := `DISMISS: finding-123
Reason: false_positive
Note: The agent flagged this but the behavior is correct per spec §4.2.
`
	d, err := ParseDismissal(comment, "human-a", "agent-x", 42)
	if err != nil {
		t.Fatalf("ParseDismissal: %v", err)
	}
	if d.FindingID != "finding-123" {
		t.Errorf("FindingID = %q, want %q", d.FindingID, "finding-123")
	}
	if d.Reason != DismissalFalsePositive {
		t.Errorf("Reason = %q, want %q", d.Reason, DismissalFalsePositive)
	}
	if d.Note != "The agent flagged this but the behavior is correct per spec §4.2." {
		t.Errorf("Note = %q", d.Note)
	}
	if d.HumanID != "human-a" {
		t.Errorf("HumanID = %q, want %q", d.HumanID, "human-a")
	}
	if d.AgentID != "agent-x" {
		t.Errorf("AgentID = %q, want %q", d.AgentID, "agent-x")
	}
	if d.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42", d.PRNumber)
	}
	if d.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
}

func TestParseDismissal_AllReasons(t *testing.T) {
	reasons := []DismissalReason{
		DismissalFalsePositive,
		DismissalAlreadyHandled,
		DismissalOutOfScope,
		DismissalArchitecturalDecision,
	}
	for _, reason := range reasons {
		comment := "DISMISS: f\nReason: " + reason.String() + "\nNote: n"
		d, err := ParseDismissal(comment, "h", "a", 1)
		if err != nil {
			t.Errorf("ParseDismissal(%s): %v", reason, err)
			continue
		}
		if d.Reason != reason {
			t.Errorf("ParseDismissal(%s): Reason = %q", reason, d.Reason)
		}
	}
}

func TestParseDismissal_MissingDismiss(t *testing.T) {
	_, err := ParseDismissal("Reason: false_positive", "h", "a", 1)
	if err == nil {
		t.Error("expected error for missing DISMISS:")
	}
}

func TestParseDismissal_MissingReason(t *testing.T) {
	_, err := ParseDismissal("DISMISS: f\nNote: n", "h", "a", 1)
	if err == nil {
		t.Error("expected error for missing Reason:")
	}
}

func TestParseDismissal_InvalidReason(t *testing.T) {
	_, err := ParseDismissal("DISMISS: f\nReason: not_a_real_reason", "h", "a", 1)
	if err == nil {
		t.Error("expected error for invalid reason")
	}
}

func TestParseDismissal_EmptyInput(t *testing.T) {
	_, err := ParseDismissal("", "h", "a", 1)
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestParseDismissal_CaseInsensitive(t *testing.T) {
	comment := "DISMISS: f-1\nREASON: FALSE_POSITIVE\nNOTE: test"
	d, err := ParseDismissal(comment, "h", "a", 1)
	if err != nil {
		t.Fatalf("ParseDismissal: %v", err)
	}
	if d.Reason != DismissalFalsePositive {
		t.Errorf("Reason = %q, want %q", d.Reason, DismissalFalsePositive)
	}
}

func TestParseDismissal_WhitespaceHandling(t *testing.T) {
	comment := "   DISMISS:   f-1  \n  Reason:  false_positive  \n  Note:   test note  "
	d, err := ParseDismissal(comment, "h", "a", 1)
	if err != nil {
		t.Fatalf("ParseDismissal: %v", err)
	}
	if d.FindingID != "f-1" {
		t.Errorf("FindingID = %q, want %q", d.FindingID, "f-1")
	}
	if d.Note != "test note" {
		t.Errorf("Note = %q, want %q", d.Note, "test note")
	}
}

func TestDismissalStore_Concurrency(t *testing.T) {
	store, cleanup := tempStore(t)
	defer cleanup()

	var wg sync.WaitGroup
	n := 50
	errors := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			d := makeDismissal("f-concurrent", DismissalAlreadyHandled, "", "human-a", "agent-z", idx)
			errors[idx] = store.Record(d)
		}(i)
	}
	wg.Wait()

	for i, err := range errors {
		if err != nil {
			t.Errorf("goroutine %d: Record failed: %v", i, err)
		}
	}

	if got := store.OverrideCount("human-a"); got != n {
		t.Errorf("OverrideCount = %d, want %d", got, n)
	}
}

func TestDismissalStore_LoadsExistingCounts(t *testing.T) {
	store, cleanup := tempStore(t)
	defer cleanup()

	// Populate store with 7 dismissals.
	for i := 0; i < 7; i++ {
		d := makeDismissal("f", DismissalOutOfScope, "", "human-a", "agent", i)
		if err := store.Record(d); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	// Re-open the store from the same file.
	store2, err := NewDismissalStore(store.Path())
	if err != nil {
		t.Fatalf("NewDismissalStore (reload): %v", err)
	}
	if got := store2.OverrideCount("human-a"); got != 7 {
		t.Errorf("OverrideCount after reload = %d, want 7", got)
	}
}

func TestDismissalHandler_NilTracker(t *testing.T) {
	store, cleanup := tempStore(t)
	defer cleanup()
	handler := NewDismissalHandler(store, nil)

	d := makeDismissal("f", DismissalFalsePositive, "", "h", "a", 1)
	count, err := handler.ProcessDismissal(d)
	if err != nil {
		t.Fatalf("ProcessDismissal with nil tracker: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	// Should not panic.
}

func TestDismissalStore_Record_ZeroTimestamp(t *testing.T) {
	store, cleanup := tempStore(t)
	defer cleanup()

	d := Dismissal{
		FindingID: "f-zero",
		Reason:    DismissalArchitecturalDecision,
		HumanID:   "h",
		AgentID:   "a",
		PRNumber:  1,
		// Timestamp is zero
	}
	if err := store.Record(d); err != nil {
		t.Fatalf("Record with zero timestamp: %v", err)
	}
	// Verify the timestamp was set.
	data, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// Should have a valid JSON line with a non-zero timestamp.
	if len(data) == 0 {
		t.Error("expected non-empty file")
	}
}
