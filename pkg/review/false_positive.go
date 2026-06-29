package review

import (
	"fmt"
	"sync"
)

// =============================================================================
// False Positive Tracking
//
// Per spec §False Positive Feedback Loop:
//   1. Finding tagged human_dismissed with reason
//   2. Model receives trust penalty
//   3. After 10 dismissed findings → flag for re-evaluation
//   4. Re-evaluation runs against curated test suite
//   5. Models with >15% FP rate removed from rotation
// =============================================================================

// FPTracker tracks false positive findings per model and triggers
// re-evaluation or rotation when thresholds are crossed.
type FPTracker struct {
	mu sync.RWMutex

	// dismissed[modelID] = count of human-dismissed findings
	dismissed map[string]int

	// fpFlagged[modelID] = true if model has been flagged for re-evaluation
	fpFlagged map[string]bool

	// removed[modelID] = true if model has been removed from rotation
	removed map[string]bool

	// The threshold for flagging (default 10).
	FlagThreshold int

	// The maximum FP rate before removal (default 15.0 = 15%).
	MaxFPRate float64
}

// NewFPTracker creates a false positive tracker with default thresholds.
func NewFPTracker() *FPTracker {
	return &FPTracker{
		dismissed:     make(map[string]int),
		fpFlagged:     make(map[string]bool),
		removed:       make(map[string]bool),
		FlagThreshold: 10,
		MaxFPRate:     15.0,
	}
}

// RecordDismissal records a human-dismissed finding for a model.
// Returns the new dismissal count for that model.
// If the count reaches FlagThreshold, the model is flagged.
func (t *FPTracker) RecordDismissal(modelID string) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.dismissed[modelID]++
	count := t.dismissed[modelID]

	if count >= t.FlagThreshold {
		t.fpFlagged[modelID] = true
	}

	return count
}

// DismissalCount returns the number of dismissed findings for a model.
func (t *FPTracker) DismissalCount(modelID string) int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.dismissed[modelID]
}

// IsFlagged returns true if the model has been flagged for re-evaluation.
func (t *FPTracker) IsFlagged(modelID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.fpFlagged[modelID]
}

// IsRemoved returns true if the model has been removed from rotation.
func (t *FPTracker) IsRemoved(modelID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.removed[modelID]
}

// FlaggedModels returns the list of models currently flagged for re-evaluation.
func (t *FPTracker) FlaggedModels() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var models []string
	for m, flagged := range t.fpFlagged {
		if flagged {
			models = append(models, m)
		}
	}
	return models
}

// RemovedModels returns the list of models removed from rotation.
func (t *FPTracker) RemovedModels() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var models []string
	for m, removed := range t.removed {
		if removed {
			models = append(models, m)
		}
	}
	return models
}

// EvaluateFPRate checks whether a model's false positive rate exceeds MaxFPRate.
// If totalEvaluations is 0, returns false (no data yet).
// Returns true if the rate exceeds threshold (removing the model).
func (t *FPTracker) EvaluateFPRate(modelID string, totalEvaluations int) bool {
	if totalEvaluations <= 0 {
		return false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	dismissals := t.dismissed[modelID]
	fpRate := float64(dismissals) / float64(totalEvaluations) * 100.0

	if fpRate > t.MaxFPRate {
		t.removed[modelID] = true
		return true
	}

	return false
}

// Reset clears all tracking data for a model (e.g., after re-evaluation passes).
func (t *FPTracker) Reset(modelID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.dismissed, modelID)
	delete(t.fpFlagged, modelID)
	// Do NOT clear removed — that's permanent until explicitly un-removed.
}

// Summary returns a human-readable summary of the tracker state.
func (t *FPTracker) Summary() string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return fmt.Sprintf("FPTracker: %d models with dismissals, %d flagged, %d removed",
		len(t.dismissed), len(t.fpFlagged), len(t.removed))
}
