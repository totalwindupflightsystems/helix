package review

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// =============================================================================
// Structured Dismissal Protocol
//
// Per spec §7.2 Human-Agent Disagreement:
//   Human dismisses agent review finding with structured reason.
//   Dismissal feeds the false positive tracker.
//   Frequent incorrect dismissals reduce human override weight.
// =============================================================================

// DismissalReason enumerates valid reasons for dismissing an agent finding.
type DismissalReason string

const (
	DismissalFalsePositive         DismissalReason = "false_positive"
	DismissalAlreadyHandled        DismissalReason = "already_handled"
	DismissalOutOfScope            DismissalReason = "out_of_scope"
	DismissalArchitecturalDecision DismissalReason = "architectural_decision"
)

// String returns the string representation of the reason.
func (r DismissalReason) String() string { return string(r) }

// Valid returns true if the reason is one of the defined constants.
func (r DismissalReason) Valid() bool {
	switch r {
	case DismissalFalsePositive,
		DismissalAlreadyHandled,
		DismissalOutOfScope,
		DismissalArchitecturalDecision:
		return true
	default:
		return false
	}
}

// Dismissal represents a human dismissal of an agent review finding.
type Dismissal struct {
	FindingID string          `json:"finding_id"`
	Reason    DismissalReason `json:"reason"`
	Note      string          `json:"note"`
	HumanID   string          `json:"human_id"`
	AgentID   string          `json:"agent_id"`
	PRNumber  int             `json:"pr_number"`
	Timestamp time.Time       `json:"timestamp"`
}

// DismissalStore persists dismissals to a JSONL file and tracks override counts.
type DismissalStore struct {
	path           string
	mu             sync.RWMutex
	overrideCounts map[string]int // human_id → count
}

// NewDismissalStore creates a DismissalStore backed by the given path.
// The directory is created if it does not exist.
func NewDismissalStore(path string) (*DismissalStore, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("dismissal store: create directory %s: %w", dir, err)
	}
	ds := &DismissalStore{
		path:           path,
		overrideCounts: make(map[string]int),
	}
	// Load existing counts from file on startup.
	if err := ds.loadCounts(); err != nil {
		return nil, err
	}
	return ds, nil
}

// loadCounts reads the JSONL file and rebuilds the in-memory override counts.
func (s *DismissalStore) loadCounts() error {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // fresh store
		}
		return fmt.Errorf("dismissal store: open %s: %w", s.path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var d Dismissal
		if err := json.Unmarshal([]byte(line), &d); err != nil {
			continue // skip malformed lines
		}
		if d.HumanID != "" {
			s.overrideCounts[d.HumanID]++
		}
	}
	return scanner.Err()
}

// Record appends a dismissal to the JSONL file and increments the override count.
func (s *DismissalStore) Record(d Dismissal) error {
	if !d.Reason.Valid() {
		return fmt.Errorf("dismissal store: invalid reason %q", d.Reason)
	}
	if d.Timestamp.IsZero() {
		d.Timestamp = time.Now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("dismissal store: open %s for append: %w", s.path, err)
	}

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(d); err != nil {
		f.Close()
		return fmt.Errorf("dismissal store: encode dismissal: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("dismissal store: close after append: %w", err)
	}

	if d.HumanID != "" {
		s.overrideCounts[d.HumanID]++
	}
	return nil
}

// OverrideCount returns the number of overrides for a given human.
func (s *DismissalStore) OverrideCount(humanID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.overrideCounts[humanID]
}

// OverrideWeight returns the review weight multiplier for a human based on
// their override count in a 90-day sliding window.
//
//	0–5   overrides → 1.00 (no penalty)
//	6–15  overrides → 0.75
//	16–30 overrides → 0.50
//	31+   overrides → 0.25
func (s *DismissalStore) OverrideWeight(humanID string) float64 {
	count := s.OverrideCount(humanID)
	switch {
	case count <= 5:
		return 1.00
	case count <= 15:
		return 0.75
	case count <= 30:
		return 0.50
	default:
		return 0.25
	}
}

// Path returns the store's backing file path.
func (s *DismissalStore) Path() string { return s.path }

// DismissalHandler coordinates dismissal processing with the FP tracker.
type DismissalHandler struct {
	store   *DismissalStore
	tracker *FPTracker
}

// NewDismissalHandler creates a handler backed by a store and tracker.
func NewDismissalHandler(store *DismissalStore, tracker *FPTracker) *DismissalHandler {
	return &DismissalHandler{store: store, tracker: tracker}
}

// ProcessDismissal processes a dismissal:
//   - Validates the reason
//   - Records to the store
//   - If reason is false_positive, calls tracker.RecordDismissal(agentID)
//   - Returns the new dismissal count for the agent.
func (h *DismissalHandler) ProcessDismissal(d Dismissal) (int, error) {
	if !d.Reason.Valid() {
		return 0, fmt.Errorf("dismissal handler: invalid reason %q", d.Reason)
	}
	if err := h.store.Record(d); err != nil {
		return 0, fmt.Errorf("dismissal handler: record: %w", err)
	}

	if d.Reason == DismissalFalsePositive && h.tracker != nil {
		h.tracker.RecordDismissal(d.AgentID)
	}
	return h.store.OverrideCount(d.HumanID), nil
}

// ParseDismissal parses a DISMISS: block from a PR comment text.
//
// Expected format:
//
//	DISMISS: <finding-id>
//	Reason: <false_positive|already_handled|out_of_scope|architectural_decision>
//	Note: <free-text explanation>
func ParseDismissal(commentText, humanID, agentID string, prNumber int) (Dismissal, error) {
	lines := strings.Split(commentText, "\n")

	var findingID, reasonStr, note string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "DISMISS:") {
			findingID = strings.TrimSpace(line[len("DISMISS:"):])
		} else if strings.HasPrefix(upper, "REASON:") {
			reasonStr = strings.TrimSpace(line[len("REASON:"):])
		} else if strings.HasPrefix(upper, "NOTE:") {
			note = strings.TrimSpace(line[len("NOTE:"):])
		} else if findingID == "" && reasonStr == "" && strings.HasPrefix(upper, "DISMISS") {
			// Handle "DISMISS finding-id" format or malformed input.
			rest := strings.TrimSpace(line[len("DISMISS"):])
			if strings.HasPrefix(rest, ":") {
				findingID = strings.TrimSpace(rest[1:])
			} else {
				findingID = rest
			}
		}
	}

	if findingID == "" {
		return Dismissal{}, fmt.Errorf("parse dismissal: missing DISMISS: <finding-id>")
	}
	if reasonStr == "" {
		return Dismissal{}, fmt.Errorf("parse dismissal: missing Reason: <reason>")
	}

	reason := DismissalReason(strings.ToLower(reasonStr))
	if !reason.Valid() {
		return Dismissal{}, fmt.Errorf("parse dismissal: invalid reason %q (must be one of: false_positive, already_handled, out_of_scope, architectural_decision)", reasonStr)
	}

	return Dismissal{
		FindingID: findingID,
		Reason:    reason,
		Note:      note,
		HumanID:   humanID,
		AgentID:   agentID,
		PRNumber:  prNumber,
		Timestamp: time.Now().UTC(),
	}, nil
}
