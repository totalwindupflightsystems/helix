package estimate

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Estimation drift tracking (spec §8.2 step 4, §9.2)
// ---------------------------------------------------------------------------

// DriftThresholdPct is the percentage above which average drift triggers a
// warning. Per spec §9.2 step 3: "If Helix projection differs from OpenRouter
// by >10%, log warning."
const DriftThresholdPct = 10.0

// RecentEntryLimit is the maximum number of recent entries returned in a
// DriftReport. This keeps reports compact for CLI/dashboard display.
const RecentEntryLimit = 20

// DriftEntry is a single recorded estimation-vs-actual data point.
type DriftEntry struct {
	AgentID     string    `json:"agent_id"`
	Estimated   float64   `json:"estimated"`
	Actual      float64   `json:"actual"`
	DriftPct    float64   `json:"drift_pct"`
	Timestamp   time.Time `json:"timestamp"`
	TaskType    string    `json:"task_type,omitempty"`
	Model       string    `json:"model,omitempty"`
	Description string    `json:"description,omitempty"`
}

// DriftReport summarizes drift data for a single agent (or all agents).
type DriftReport struct {
	AgentID       string        `json:"agent_id"`
	Count         int           `json:"count"`
	AvgDriftPct   float64       `json:"avg_drift_pct"`
	MaxDrift      float64       `json:"max_drift"`
	MinDrift      float64       `json:"min_drift"`
	OverThreshold bool          `json:"over_threshold"`
	RecentEntries []DriftEntry  `json:"recent_entries"`
	Period        time.Duration `json:"period"`
}

// DriftTracker accumulates estimation drift entries per spec §8.2 step 4
// ("If actual > estimated: difference is logged as ESTIMATION_DRIFT") and
// provides reporting + threshold checks per spec §9.2 step 3.
//
// The tracker is safe for concurrent use. It feeds the existing Calibrator
// (spec §12.2 — weekly recalibration from drift data) via FeedCalibrator.
type DriftTracker struct {
	mu      sync.RWMutex
	entries []DriftEntry
}

// NewDriftTracker returns an empty tracker.
func NewDriftTracker() *DriftTracker {
	return &DriftTracker{
		entries: make([]DriftEntry, 0),
	}
}

// computeDriftPct returns the percentage drift: (actual - estimated) / estimated * 100.
// Positive → estimate was too low. Negative → estimate was too high.
// Zero estimated with non-zero actual → +Inf. Zero/zero → 0.
func computeDriftPct(estimated, actual float64) float64 {
	if estimated == 0 {
		if actual == 0 {
			return 0
		}
		return math.Inf(1)
	}
	return (actual - estimated) / estimated * 100
}

// RecordDrift logs a new drift entry for the given agent. The drift percentage
// is computed internally from the estimated and actual costs. Returns the
// created entry for convenience.
func (dt *DriftTracker) RecordDrift(agentID string, estimated, actual float64) DriftEntry {
	return dt.RecordDriftEntry(DriftEntry{
		AgentID:   agentID,
		Estimated: estimated,
		Actual:    actual,
		Timestamp: time.Now().UTC(),
	})
}

// RecordDriftEntry logs a fully-specified drift entry (with pre-filled metadata
// like task type, model, description, and timestamp). This is preferred when
// the caller already has contextual data. The DriftPct field is recomputed
// from Estimated/Actual to ensure consistency.
func (dt *DriftTracker) RecordDriftEntry(entry DriftEntry) DriftEntry {
	entry.DriftPct = computeDriftPct(entry.Estimated, entry.Actual)
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	dt.mu.Lock()
	dt.entries = append(dt.entries, entry)
	dt.mu.Unlock()
	return entry
}

// getEntries returns a copy of all entries under read lock.
func (dt *DriftTracker) getEntries() []DriftEntry {
	dt.mu.RLock()
	defer dt.mu.RUnlock()
	result := make([]DriftEntry, len(dt.entries))
	copy(result, dt.entries)
	return result
}

// filterEntries returns entries matching the agentID filter. If agentID is
// empty, all entries are returned.
func (dt *DriftTracker) filterEntries(agentID string) []DriftEntry {
	all := dt.getEntries()
	if agentID == "" {
		return all
	}
	var filtered []DriftEntry
	for _, e := range all {
		if e.AgentID == agentID {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// DriftReport generates a summary for the given agent. If agentID is empty,
// the report covers ALL agents. The period field indicates the time span from
// the oldest to newest entry.
func (dt *DriftTracker) DriftReport(agentID string) DriftReport {
	entries := dt.filterEntries(agentID)

	report := DriftReport{
		AgentID:       agentID,
		Count:         len(entries),
		RecentEntries: make([]DriftEntry, 0),
	}
	if len(entries) == 0 {
		return report
	}

	var sumDrift, maxDrift, minDrift float64
	var oldest, newest time.Time
	maxDrift = math.Inf(-1)
	minDrift = math.Inf(1)

	for _, e := range entries {
		if !math.IsInf(e.DriftPct, 0) {
			sumDrift += e.DriftPct
		} else if math.IsInf(e.DriftPct, 1) {
			sumDrift += 1000 // cap +Inf contribution
		}
		if e.DriftPct > maxDrift {
			maxDrift = e.DriftPct
		}
		if e.DriftPct < minDrift {
			minDrift = e.DriftPct
		}
		if oldest.IsZero() || e.Timestamp.Before(oldest) {
			oldest = e.Timestamp
		}
		if newest.IsZero() || e.Timestamp.After(newest) {
			newest = e.Timestamp
		}
	}

	report.AvgDriftPct = sumDrift / float64(len(entries))
	report.MaxDrift = maxDrift
	report.MinDrift = minDrift
	report.OverThreshold = math.Abs(report.AvgDriftPct) > DriftThresholdPct
	report.Period = newest.Sub(oldest)

	// Sort by timestamp descending for recent entries.
	sorted := make([]DriftEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.After(sorted[j].Timestamp)
	})
	limit := RecentEntryLimit
	if len(sorted) < limit {
		limit = len(sorted)
	}
	report.RecentEntries = sorted[:limit]

	return report
}

// DriftReportAll generates a report covering all agents.
func (dt *DriftTracker) DriftReportAll() DriftReport {
	return dt.DriftReport("")
}

// IsOverThreshold returns true when the average absolute drift exceeds the
// 10% threshold per spec §9.2 step 3.
func (dt *DriftTracker) IsOverThreshold(agentID string) bool {
	if dt.Count(agentID) == 0 {
		return false
	}
	report := dt.DriftReport(agentID)
	return report.OverThreshold
}

// Count returns the number of drift entries for the given agent (or all if empty).
func (dt *DriftTracker) Count(agentID string) int {
	return len(dt.filterEntries(agentID))
}

// Clear removes all entries for the given agent. If agentID is empty, all
// entries are cleared.
func (dt *DriftTracker) Clear(agentID string) int {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	if agentID == "" {
		n := len(dt.entries)
		dt.entries = make([]DriftEntry, 0)
		return n
	}

	var kept []DriftEntry
	cleared := 0
	for _, e := range dt.entries {
		if e.AgentID == agentID {
			cleared++
		} else {
			kept = append(kept, e)
		}
	}
	dt.entries = kept
	return cleared
}

// ExportDriftLog writes all drift entries as JSONL to the given writer/path.
// Entries are sorted by timestamp ascending for chronological replay.
func (dt *DriftTracker) ExportDriftLog(path string) error {
	entries := dt.getEntries()
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp.Before(entries[j].Timestamp)
	})

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create drift log: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return fmt.Errorf("encode drift entry: %w", err)
		}
	}
	return nil
}

// ImportDriftLog reads a JSONL drift log file and loads entries into the tracker.
// Existing entries are preserved.
func (dt *DriftTracker) ImportDriftLog(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read drift log: %w", err)
	}

	lines := splitJSONLines(data)
	dt.mu.Lock()
	defer dt.mu.Unlock()

	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var entry DriftEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue // skip malformed lines
		}
		dt.entries = append(dt.entries, entry)
	}
	return nil
}

// splitJSONLines splits raw JSONL data into individual line byte slices,
// trimming whitespace and skipping empty lines.
func splitJSONLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			line := trimSpace(data[start:i])
			if len(line) > 0 {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	// Last line (no trailing newline)
	if start < len(data) {
		line := trimSpace(data[start:])
		if len(line) > 0 {
			lines = append(lines, line)
		}
	}
	return lines
}

func trimSpace(b []byte) []byte {
	start := 0
	end := len(b)
	for start < end && (b[start] == ' ' || b[start] == '\t' || b[start] == '\r') {
		start++
	}
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\r') {
		end--
	}
	return b[start:end]
}

// FeedCalibrator transfers drift entries into a Calibrator as CalibrationRecords.
// This implements the spec §12.2 integration: "drift_detector.go → compares
// estimate vs actual → recalibrates ratios" and §8.2 step 5: "Estimation ratios
// are recalibrated weekly from drift data."
//
// Only entries with valid (non-zero, non-infinite) estimated cost are fed,
// since the Calibrator divides by estimated cost. Returns the number of
// records added.
func (dt *DriftTracker) FeedCalibrator(c *Calibrator, agentID string) int {
	entries := dt.filterEntries(agentID)
	count := 0
	for _, e := range entries {
		if e.Estimated <= 0 || math.IsInf(e.DriftPct, 0) {
			continue
		}
		// Estimate the cache hit ratio: if actual < estimated, the estimate
		// was likely too pessimistic about cache effectiveness. Use the
		// drift as a correction signal. The exact ratio isn't recoverable
		// from cost alone, but the Calibrator uses inverse-drift weighting,
		// so we provide a reasonable approximation: the ratio that would
		// have produced the actual cost.
		ratio := 0.5 // default if we can't infer
		if e.Estimated > 0 && e.Actual > 0 && e.Actual <= e.Estimated {
			// Actual was cheaper → better cache than estimated.
			// Approximate: ratio scales with the cost reduction.
			ratio = 0.5 + (1.0-e.Actual/e.Estimated)*0.5
			if ratio > 1.0 {
				ratio = 1.0
			}
		} else if e.Estimated > 0 && e.Actual > e.Estimated {
			// Actual was more expensive → worse cache than estimated.
			ratio = 0.5 - (e.Actual/e.Estimated-1.0)*0.5
			if ratio < 0 {
				ratio = 0
			}
		}

		c.AddRecord(CalibrationRecord{
			EstimatedCost: e.Estimated,
			ActualCost:    e.Actual,
			CacheHitRatio: ratio,
			Timestamp:     e.Timestamp.Format(time.RFC3339),
		})
		count++
	}
	return count
}

// AgentsWithDrift returns a sorted list of agent IDs that have drift entries.
func (dt *DriftTracker) AgentsWithDrift() []string {
	entries := dt.getEntries()
	seen := make(map[string]bool)
	for _, e := range entries {
		seen[e.AgentID] = true
	}
	result := make([]string, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

// FormatDriftReport renders a DriftReport as a human-readable string for CLI output.
func FormatDriftReport(r DriftReport) string {
	agentLabel := r.AgentID
	if agentLabel == "" {
		agentLabel = "ALL"
	}
	thresholdMark := ""
	if r.OverThreshold {
		thresholdMark = " ⚠️"
	}

	out := fmt.Sprintf(
		"Agent: %s | Entries: %d | Avg Drift: %.2f%%%s | Max: %.2f%% | Min: %.2f%%",
		agentLabel, r.Count, r.AvgDriftPct, thresholdMark, r.MaxDrift, r.MinDrift,
	)

	if r.Count > 0 && len(r.RecentEntries) > 0 {
		out += "\n\nRecent entries:"
		for _, e := range r.RecentEntries {
			out += fmt.Sprintf(
				"\n  %s  %s  est=$%.4f  actual=$%.4f  drift=%.2f%%",
				e.Timestamp.Format("2006-01-02 15:04"),
				e.AgentID,
				e.Estimated,
				e.Actual,
				e.DriftPct,
			)
		}
	}

	return out
}
