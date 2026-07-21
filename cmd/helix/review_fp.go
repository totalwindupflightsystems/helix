package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/totalwindupflightsystems/helix/pkg/review"
)

// -----------------------------------------------------------------------------
// fp-stats / fp-record
// -----------------------------------------------------------------------------

func runReviewFPStats(flags revFlags, stdout, stderr io.Writer) int {
	tracker, err := loadFPTracker(flags.statePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: load tracker: %v\n", err)
		return revExitError
	}

	if flags.jsonOut {
		models := tracker.FlaggedModels()
		sort.Strings(models)
		type modelEntry struct {
			Model      string `json:"model"`
			Dismissals int    `json:"dismissals"`
			Flagged    bool   `json:"flagged"`
			Removed    bool   `json:"removed"`
		}
		var entries []modelEntry
		// Walk FlaggedModels + RemovedModels union (no API to enumerate all known).
		known := map[string]bool{}
		for _, m := range models {
			known[m] = true
		}
		for _, m := range tracker.RemovedModels() {
			known[m] = true
		}
		for m := range known {
			entries = append(entries, modelEntry{
				Model:      m,
				Dismissals: tracker.DismissalCount(m),
				Flagged:    tracker.IsFlagged(m),
				Removed:    tracker.IsRemoved(m),
			})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Model < entries[j].Model })
		b, _ := json.MarshalIndent(map[string]any{
			"models":     entries,
			"summary":    tracker.Summary(),
			"state_path": flags.statePath,
		}, "", "  ")
		fmt.Fprintln(stdout, string(b))
		return revExitOK
	}

	fmt.Fprintln(stdout, tracker.Summary())
	if len(tracker.FlaggedModels()) > 0 || len(tracker.RemovedModels()) > 0 {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Flagged models:")
		for _, m := range tracker.FlaggedModels() {
			fmt.Fprintf(stdout, "  - %s (dismissals=%d, flagged=true)\n", m, tracker.DismissalCount(m))
		}
		fmt.Fprintln(stdout, "Removed models:")
		for _, m := range tracker.RemovedModels() {
			fmt.Fprintf(stdout, "  - %s (removed=true)\n", m)
		}
	}
	return revExitOK
}

func runReviewFPRecord(flags revFlags, stdout, stderr io.Writer) int {
	if flags.modelID == "" {
		fmt.Fprintln(stderr, "error: --model is required for fp-record")
		return revExitError
	}
	tracker, err := loadFPTracker(flags.statePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: load tracker: %v\n", err)
		return revExitError
	}

	for i := 0; i < flags.dismissals; i++ {
		tracker.RecordDismissal(flags.modelID)
	}
	if flags.totalEvals > 0 {
		tracker.EvaluateFPRate(flags.modelID, flags.totalEvals)
	}

	if flags.statePath != "" {
		if err := saveFPTracker(flags.statePath, tracker); err != nil {
			fmt.Fprintf(stderr, "error: persist tracker: %v\n", err)
			return revExitError
		}
	}

	out := map[string]any{
		"model":      flags.modelID,
		"dismissals": tracker.DismissalCount(flags.modelID),
		"flagged":    tracker.IsFlagged(flags.modelID),
		"removed":    tracker.IsRemoved(flags.modelID),
		"state_path": flags.statePath,
	}
	if flags.jsonOut {
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(stdout, string(b))
	} else {
		fmt.Fprintf(stdout, "Recorded %d dismissal(s) for model %q\n", flags.dismissals, flags.modelID)
		fmt.Fprintf(stdout, "  dismissals=%d flagged=%v removed=%v\n",
			out["dismissals"], out["flagged"], out["removed"])
		if flags.statePath != "" {
			fmt.Fprintf(stdout, "  state persisted to %s\n", flags.statePath)
		}
	}
	if tracker.IsRemoved(flags.modelID) || tracker.IsFlagged(flags.modelID) {
		return revExitBlock
	}
	return revExitOK
}

// -----------------------------------------------------------------------------
// FP / helper functions
// -----------------------------------------------------------------------------

// signatureCount returns how many of the 3 signer roles (primary, adversarial,
// audit) have a non-empty signature on the bundle. The Signatures struct is
// embedded on EvidenceBundle so we read its 3 string fields directly.
func signatureCount(b review.EvidenceBundle) int {
	n := 0
	if b.Signatures.Primary != "" {
		n++
	}
	if b.Signatures.Adversarial != "" {
		n++
	}
	if b.Signatures.Audit != "" {
		n++
	}
	return n
}

// isValidSignerRole reports whether role matches one of EvidenceBundle's 3
// signing positions.
func isValidSignerRole(role string) bool {
	switch role {
	case "primary", "adversarial", "audit":
		return true
	}
	return false
}

// loadFPTracker loads a tracker from a JSON file (or returns a fresh one).
func loadFPTracker(path string) (*review.FPTracker, error) {
	if path == "" {
		return review.NewFPTracker(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return review.NewFPTracker(), nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return review.NewFPTracker(), nil
	}
	// FPTracker has no JSON marshaling defined — we wrap the persisted state
	// in a struct of dismissals + flags and rehydrate manually via RecordDismissal.
	var persisted struct {
		Dismissals map[string]int `json:"dismissals"`
	}
	if err := json.Unmarshal(data, &persisted); err != nil {
		return nil, fmt.Errorf("parse tracker state: %w", err)
	}
	tracker := review.NewFPTracker()
	for model, n := range persisted.Dismissals {
		for i := 0; i < n; i++ {
			tracker.RecordDismissal(model)
		}
	}
	return tracker, nil
}

// saveFPTracker writes tracker dismissals to a JSON file. We persist the
// observable state (dismissals map) so subsequent runs see the same counts.
func saveFPTracker(path string, tracker *review.FPTracker) error {
	// Walk flagged + removed + any model we can enumerate (we don't have a
	// "list all" API, so we walk all three known sets).
	state := struct {
		Dismissals map[string]int `json:"dismissals"`
	}{Dismissals: map[string]int{}}
	known := map[string]bool{}
	for _, m := range tracker.FlaggedModels() {
		known[m] = true
	}
	for _, m := range tracker.RemovedModels() {
		known[m] = true
	}
	for m := range known {
		state.Dismissals[m] = tracker.DismissalCount(m)
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// parseIntInRange parses s as an int in [lo, hi].
func parseIntInRange(s string, lo, hi int) (int, error) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, fmt.Errorf("not an integer: %q", s)
	}
	if n < lo || n > hi {
		return 0, fmt.Errorf("%d out of range [%d, %d]", n, lo, hi)
	}
	return n, nil
}

// tsSuffix returns a short timestamp suffix for ad-hoc bundle IDs.
func tsSuffix() string {
	// Use os.Stat-friendly format without time import to keep this file lean.
	return fmt.Sprintf("cli-%d", os.Getpid())
}
