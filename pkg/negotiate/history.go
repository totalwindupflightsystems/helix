package negotiate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Negotiation history query — spec §13 (Filesystem Layout) + audit trail
// ---------------------------------------------------------------------------

// HistoryQuery filters past negotiations stored in the filesystem.
type HistoryQuery struct {
	Agent    string    // filter by agent name (either agent_a or agent_b)
	PRNumber int       // filter by PR number (0 = no filter)
	Outcome  string    // filter by outcome (APPROVED, REJECT, escalated)
	Since    time.Time // only negotiations after this time
	Until    time.Time // only negotiations before this time
	Limit    int       // max results (0 = unlimited)
}

// HistoryEntry is a summary of one past negotiation on disk.
type HistoryEntry struct {
	PRNumber     int       `json:"pr_number"`
	Agents       []string  `json:"agents"`
	Rounds       int       `json:"rounds"`
	Outcome      string    `json:"outcome"`
	HadChimera   bool      `json:"had_chimera"`
	HadDeadlock  bool      `json:"had_deadlock"`
	WasEscalated bool      `json:"was_escalated"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at"`
	FilePath     string    `json:"file_path"`
}

// HistoryResult is the result of querying past negotiations.
type HistoryResult struct {
	Entries []HistoryEntry `json:"entries"`
	Total   int            `json:"total"`
}

// QueryHistory scans the negotiations directory for past JSONL debate
// transcripts, replays each one, and returns matching entries based on the
// query filters. The directory is expected to follow the spec §13 layout:
//
//	dir/
//	  <pr-number>-<timestamp>.jsonl          Debate transcripts
//	  <pr-number>-<timestamp>-verdict.md     Verdict files
//	  state.json                             Active negotiations
//
// Only .jsonl files are scanned (debate transcripts). state.json and
// verdict files are skipped.
func QueryHistory(dir string, q HistoryQuery) (*HistoryResult, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return &HistoryResult{Entries: []HistoryEntry{}}, nil
		}
		return nil, fmt.Errorf("read negotiations dir %q: %w", dir, err)
	}

	var results []HistoryEntry

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()

		// Only scan .jsonl files, skip state.json and verdict files.
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}

		path := filepath.Join(dir, name)
		summary, err := ReplayTranscript(path)
		if err != nil {
			continue // skip malformed transcripts
		}

		h := HistoryEntry{
			PRNumber:     summary.PRNumber,
			Agents:       summary.Agents,
			Rounds:       summary.Rounds,
			Outcome:      summary.Outcome,
			HadChimera:   summary.HadChimera,
			HadDeadlock:  summary.HadDeadlock,
			WasEscalated: summary.WasEscalated,
			StartedAt:    summary.StartedAt,
			EndedAt:      summary.EndedAt,
			FilePath:     path,
		}

		if !matchesQuery(h, q) {
			continue
		}

		results = append(results, h)
	}

	// Sort by StartedAt descending (most recent first).
	sort.Slice(results, func(i, j int) bool {
		return results[i].StartedAt.After(results[j].StartedAt)
	})

	// Apply limit.
	if q.Limit > 0 && len(results) > q.Limit {
		results = results[:q.Limit]
	}

	total := len(results)

	return &HistoryResult{
		Entries: results,
		Total:   total,
	}, nil
}

// matchesQuery checks whether a history entry passes all non-zero filters.
func matchesQuery(h HistoryEntry, q HistoryQuery) bool {
	// Agent filter: entry must include the agent name.
	if q.Agent != "" {
		found := false
		for _, a := range h.Agents {
			if a == q.Agent {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// PR number filter.
	if q.PRNumber > 0 && h.PRNumber != q.PRNumber {
		return false
	}

	// Outcome filter.
	if q.Outcome != "" && h.Outcome != q.Outcome {
		return false
	}

	// Time range filters.
	if !q.Since.IsZero() && h.StartedAt.Before(q.Since) {
		return false
	}
	if !q.Until.IsZero() && h.StartedAt.After(q.Until) {
		return false
	}

	return true
}

// FormatHistory renders a HistoryResult as a human-readable table for CLI output.
func FormatHistory(r *HistoryResult) string {
	if r == nil || len(r.Entries) == 0 {
		return "No negotiations found."
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d negotiation(s):\n\n", r.Total))
	b.WriteString("PR     Agents                    Rounds  Outcome     Chimera  Escalated  Started\n")
	b.WriteString("-----  ------------------------  ------  ----------  -------  ---------  --------\n")

	for _, h := range r.Entries {
		agents := strings.Join(h.Agents, ", ")
		if len(agents) > 24 {
			agents = agents[:21] + "..."
		}
		chimera := "no"
		if h.HadChimera {
			chimera = "yes"
		}
		escalated := "no"
		if h.WasEscalated {
			escalated = "yes"
		}
		outcome := h.Outcome
		if outcome == "" {
			outcome = "—"
		}

		b.WriteString(fmt.Sprintf("%-5d  %-24s  %-6d  %-10s  %-7s  %-9s  %s\n",
			h.PRNumber,
			agents,
			h.Rounds,
			outcome,
			chimera,
			escalated,
			h.StartedAt.Format("2006-01-02 15:04"),
		))
	}

	return b.String()
}
