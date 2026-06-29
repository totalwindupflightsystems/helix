package review

import (
	"strings"
	"testing"
)

// =============================================================================
// Basic tracking
// =============================================================================

func TestNewFPTracker(t *testing.T) {
	tracker := NewFPTracker()
	if tracker == nil {
		t.Fatal("expected non-nil tracker")
	}
	if tracker.FlagThreshold != 10 {
		t.Errorf("expected default threshold 10, got %d", tracker.FlagThreshold)
	}
	if tracker.MaxFPRate != 15.0 {
		t.Errorf("expected default max FP rate 15.0, got %.1f", tracker.MaxFPRate)
	}
}

func TestRecordDismissal_Increments(t *testing.T) {
	tracker := NewFPTracker()
	c1 := tracker.RecordDismissal("model-a")
	if c1 != 1 {
		t.Errorf("expected count 1 after first dismissal, got %d", c1)
	}

	c2 := tracker.RecordDismissal("model-a")
	if c2 != 2 {
		t.Errorf("expected count 2 after second dismissal, got %d", c2)
	}
}

func TestRecordDismissal_SeparateModels(t *testing.T) {
	tracker := NewFPTracker()
	tracker.RecordDismissal("model-a")
	tracker.RecordDismissal("model-b")

	if tracker.DismissalCount("model-a") != 1 {
		t.Errorf("model-a should have 1 dismissal")
	}
	if tracker.DismissalCount("model-b") != 1 {
		t.Errorf("model-b should have 1 dismissal")
	}
}

func TestDismissalCount_UnknownModel(t *testing.T) {
	tracker := NewFPTracker()
	if c := tracker.DismissalCount("unknown"); c != 0 {
		t.Errorf("expected 0 for unknown model, got %d", c)
	}
}

// =============================================================================
// Flag threshold
// =============================================================================

func TestFlagThreshold(t *testing.T) {
	tracker := NewFPTracker()
	tracker.FlagThreshold = 3 // lower for test

	if tracker.IsFlagged("model-x") {
		t.Error("expected not flagged initially")
	}

	tracker.RecordDismissal("model-x")
	if tracker.IsFlagged("model-x") {
		t.Error("expected not flagged at 1 dismissal")
	}

	tracker.RecordDismissal("model-x")
	tracker.RecordDismissal("model-x")

	if !tracker.IsFlagged("model-x") {
		t.Error("expected flagged at 3+ dismissals")
	}
}

func TestFlaggedModels(t *testing.T) {
	tracker := NewFPTracker()
	tracker.FlagThreshold = 2

	tracker.RecordDismissal("model-a")
	tracker.RecordDismissal("model-a")
	tracker.RecordDismissal("model-b")
	tracker.RecordDismissal("model-b")

	flagged := tracker.FlaggedModels()
	if len(flagged) != 2 {
		t.Errorf("expected 2 flagged models, got %d: %v", len(flagged), flagged)
	}

	// Verify both are flagged
	hasA, hasB := false, false
	for _, m := range flagged {
		if m == "model-a" {
			hasA = true
		}
		if m == "model-b" {
			hasB = true
		}
	}
	if !hasA {
		t.Error("model-a should be flagged")
	}
	if !hasB {
		t.Error("model-b should be flagged")
	}
}

// =============================================================================
// FP rate evaluation
// =============================================================================

func TestEvaluateFPRate_BelowThreshold(t *testing.T) {
	tracker := NewFPTracker()
	tracker.MaxFPRate = 15.0

	tracker.RecordDismissal("model-y") // 1 dismissal out of 20 evaluations = 5%
	removed := tracker.EvaluateFPRate("model-y", 20)
	if removed {
		t.Error("expected NOT removed at 5% FP rate")
	}
	if tracker.IsRemoved("model-y") {
		t.Error("expected NOT removed")
	}
}

func TestEvaluateFPRate_AboveThreshold(t *testing.T) {
	tracker := NewFPTracker()
	tracker.MaxFPRate = 15.0

	tracker.RecordDismissal("model-z") // 1
	tracker.RecordDismissal("model-z") // 2
	tracker.RecordDismissal("model-z") // 3
	// 3 dismissals out of 10 evaluations = 30% > 15%
	removed := tracker.EvaluateFPRate("model-z", 10)
	if !removed {
		t.Error("expected removed at 30% FP rate")
	}
	if !tracker.IsRemoved("model-z") {
		t.Error("expected IsRemoved=true")
	}
}

func TestEvaluateFPRate_ZeroEvaluations(t *testing.T) {
	tracker := NewFPTracker()
	removed := tracker.EvaluateFPRate("model-w", 0)
	if removed {
		t.Error("expected not removed when no evaluations")
	}
}

func TestEvaluateFPRate_ExactlyAtThreshold(t *testing.T) {
	tracker := NewFPTracker()
	tracker.MaxFPRate = 20.0

	tracker.RecordDismissal("model-v") // 1
	tracker.RecordDismissal("model-v") // 2
	// 2 dismissals out of 10 evaluations = 20% == threshold, NOT exceeded
	removed := tracker.EvaluateFPRate("model-v", 10)
	if removed {
		t.Error("expected not removed at exactly threshold (must exceed)")
	}
}

// =============================================================================
// Reset
// =============================================================================

func TestReset(t *testing.T) {
	tracker := NewFPTracker()
	tracker.FlagThreshold = 2

	tracker.RecordDismissal("model-r")
	tracker.RecordDismissal("model-r")

	if !tracker.IsFlagged("model-r") {
		t.Error("expected flagged before reset")
	}

	tracker.Reset("model-r")

	if tracker.DismissalCount("model-r") != 0 {
		t.Error("expected 0 dismissals after reset")
	}
	if tracker.IsFlagged("model-r") {
		t.Error("expected not flagged after reset")
	}
}

func TestReset_DoesNotClearRemoved(t *testing.T) {
	tracker := NewFPTracker()
	tracker.MaxFPRate = 5.0

	tracker.RecordDismissal("model-s")
	tracker.EvaluateFPRate("model-s", 10) // 10% > 5% → removed

	if !tracker.IsRemoved("model-s") {
		t.Error("expected removed")
	}

	tracker.Reset("model-s")

	if !tracker.IsRemoved("model-s") {
		t.Error("expected still removed after reset (permanent)")
	}
}

// =============================================================================
// RemovedModels
// =============================================================================

func TestRemovedModels(t *testing.T) {
	tracker := NewFPTracker()
	tracker.MaxFPRate = 5.0

	tracker.RecordDismissal("model-p")
	tracker.EvaluateFPRate("model-p", 10)

	tracker.RecordDismissal("model-q")
	tracker.EvaluateFPRate("model-q", 10)

	removed := tracker.RemovedModels()
	if len(removed) != 2 {
		t.Errorf("expected 2 removed models, got %d: %v", len(removed), removed)
	}
}

// =============================================================================
// IsFlagged / IsRemoved for unknown models
// =============================================================================

func TestIsFlagged_UnknownModel(t *testing.T) {
	tracker := NewFPTracker()
	if tracker.IsFlagged("unknown") {
		t.Error("expected false for unknown model")
	}
}

func TestIsRemoved_UnknownModel(t *testing.T) {
	tracker := NewFPTracker()
	if tracker.IsRemoved("unknown") {
		t.Error("expected false for unknown model")
	}
}

// =============================================================================
// Summary
// =============================================================================

func TestSummary(t *testing.T) {
	tracker := NewFPTracker()
	tracker.FlagThreshold = 2

	tracker.RecordDismissal("model-a")
	tracker.RecordDismissal("model-a")
	tracker.RecordDismissal("model-b")

	summary := tracker.Summary()
	if !strings.Contains(summary, "2 models with dismissals") {
		t.Errorf("unexpected summary: %s", summary)
	}
	if !strings.Contains(summary, "1 flagged") {
		t.Errorf("unexpected summary: %s", summary)
	}
}

// =============================================================================
// Concurrency safety (quick smoke test)
// =============================================================================

func TestFPTracker_ConcurrentAccess(t *testing.T) {
	tracker := NewFPTracker()
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			tracker.RecordDismissal("concurrent-model")
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			tracker.DismissalCount("concurrent-model")
			tracker.IsFlagged("concurrent-model")
			tracker.IsRemoved("concurrent-model")
		}
		done <- true
	}()

	// Evaluator goroutine
	go func() {
		for i := 0; i < 10; i++ {
			tracker.EvaluateFPRate("concurrent-model", 500)
		}
		done <- true
	}()

	// Wait for all goroutines
	<-done
	<-done
	<-done

	count := tracker.DismissalCount("concurrent-model")
	if count != 100 {
		t.Errorf("expected 100 dismissals, got %d", count)
	}
}
