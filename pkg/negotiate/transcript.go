package negotiate

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Transcript replay and verdict file writer — specs/pr-negotiation.md §13
// ---------------------------------------------------------------------------

// TranscriptSummary is the result of replaying a JSONL debate transcript.
// It reconstructs the negotiation state from disk for audit, debugging,
// and post-hoc analysis.
type TranscriptSummary struct {
	PRNumber     int           `json:"pr_number"`
	Agents       []string      `json:"agents"`
	Rounds       int           `json:"rounds"`
	Events       []DebateEvent `json:"events"`
	StartedAt    time.Time     `json:"started_at"`
	EndedAt      time.Time     `json:"ended_at,omitempty"`
	Outcome      string        `json:"outcome"`
	HadChimera   bool          `json:"had_chimera"`
	HadDeadlock  bool          `json:"had_deadlock"`
	WasEscalated bool          `json:"was_escalated"`
}

// ReplayTranscript reads a JSONL debate transcript file and returns a
// structured summary. This is the read counterpart to AuditLogger.LogEvent.
// Each line is expected to be a valid DebateEvent JSON object.
func ReplayTranscript(path string) (*TranscriptSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read transcript %q: %w", path, err)
	}

	summary := &TranscriptSummary{}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024) // 1MB max line
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event DebateEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return nil, fmt.Errorf("parse transcript line %d: %w", lineNum, err)
		}

		summary.Events = append(summary.Events, event)
		applyEventToSummary(summary, &event)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan transcript: %w", err)
	}

	// Derive endedAt from the last event.
	if len(summary.Events) > 0 {
		summary.EndedAt = summary.Events[len(summary.Events)-1].Timestamp
	}

	return summary, nil
}

// applyEventToSummary mutates the summary based on a single event.
func applyEventToSummary(s *TranscriptSummary, e *DebateEvent) {
	// Track the earliest timestamp as startedAt.
	if s.StartedAt.IsZero() || e.Timestamp.Before(s.StartedAt) {
		s.StartedAt = e.Timestamp
	}

	// Track max round.
	if e.Round > s.Rounds {
		s.Rounds = e.Round
	}

	switch e.Type {
	case "conflict_detected":
		if e.Agent != "" && !containsStr(s.Agents, e.Agent) {
			s.Agents = append(s.Agents, e.Agent)
		}
	case "argument":
		if e.Agent != "" && !containsStr(s.Agents, e.Agent) {
			s.Agents = append(s.Agents, e.Agent)
		}
	case "deadlock":
		s.HadDeadlock = true
	case "chimera_tiebreak":
		s.HadChimera = true
		if e.Verdict != "" {
			s.Outcome = e.Verdict
		}
	case "resolved":
		if e.Outcome != "" {
			s.Outcome = e.Outcome
		} else if e.Verdict != "" {
			s.Outcome = e.Verdict
		}
	case "escalated":
		s.WasEscalated = true
		s.Outcome = "escalated"
	case "concession":
		if e.Outcome != "" {
			s.Outcome = e.Outcome
		}
	}
}

// containsStr checks if a string is in a slice (stdlib-only, avoids collision
// with test-file contains helper).
func containsStr(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Verdict file writer — spec §13 Filesystem Layout
// ---------------------------------------------------------------------------

// VerdictFile is the markdown summary of a negotiation resolution.
// Per spec §13, the file is written to ~/.helix/negotiations/<pr>-<ts>-verdict.md
type VerdictFile struct {
	PRNumber        int
	Outcome         string
	Timestamp       time.Time
	Agents          []Agent
	Winner          string
	Loser           string
	Reasoning       string
	WinningEvidence []string
	TieBreaker      string
	Rounds          int
	HadChimera      bool
	Cost            float64
}

// WriteVerdictFile writes a markdown verdict summary to the given directory.
// The filename follows the spec §13 convention: <pr-number>-<timestamp>-verdict.md
// Returns the full path to the written file.
func WriteVerdictFile(dir string, v *VerdictFile) (string, error) {
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create verdict dir: %w", err)
	}

	tsStr := v.Timestamp.UTC().Format("20060102T150405Z")
	filename := fmt.Sprintf("%d-%s-verdict.md", v.PRNumber, tsStr)
	path := filepath.Join(dir, filename)

	content := FormatVerdictMarkdown(v)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write verdict file: %w", err)
	}

	return path, nil
}

// FormatVerdictMarkdown renders a VerdictFile as a markdown document.
func FormatVerdictMarkdown(v *VerdictFile) string {
	var b strings.Builder

	b.WriteString("# Negotiation Verdict\n\n")
	b.WriteString(fmt.Sprintf("**PR:** #%d\n", v.PRNumber))
	b.WriteString(fmt.Sprintf("**Outcome:** %s\n", v.Outcome))
	b.WriteString(fmt.Sprintf("**Timestamp:** %s\n\n", v.Timestamp.UTC().Format(time.RFC3339)))

	b.WriteString("## Agents\n\n")
	for _, a := range v.Agents {
		role := ""
		if a.Name == v.Winner {
			role = " (winner)"
		} else if a.Name == v.Loser {
			role = " (conceded)"
		}
		b.WriteString(fmt.Sprintf("- %s (trust=%d, tier=%s)%s\n", a.Name, a.TrustLevel, a.Tier, role))
	}

	b.WriteString("\n## Result\n\n")
	b.WriteString(fmt.Sprintf("- **Rounds:** %d\n", v.Rounds))
	if v.HadChimera {
		b.WriteString(fmt.Sprintf("- **Tie-breaker:** Chimera arbiter (verdict: %s, cost: $%.4f)\n", v.TieBreaker, v.Cost))
	} else {
		b.WriteString("- **Tie-breaker:** None (resolved by concession)\n")
	}

	if v.Reasoning != "" {
		b.WriteString(fmt.Sprintf("- **Reasoning:** %s\n", v.Reasoning))
	}

	if len(v.WinningEvidence) > 0 {
		b.WriteString("\n## Winning Evidence\n\n")
		for _, e := range v.WinningEvidence {
			b.WriteString(fmt.Sprintf("- %s\n", e))
		}
	}

	return b.String()
}

// WriteStateFile writes the active negotiation state to state.json (spec §13).
// This file tracks active negotiations and pending trust deltas for recovery.
type StateFile struct {
	Negotiations []ActiveNegotiation `json:"negotiations"`
	Updated      time.Time           `json:"updated"`
}

// ActiveNegotiation represents a negotiation that may need recovery.
type ActiveNegotiation struct {
	PRNumber   int       `json:"pr_number"`
	State      State     `json:"state"`
	Round      int       `json:"round"`
	Transcript string    `json:"transcript_path"`
	StartedAt  time.Time `json:"started_at"`
}

// WriteStateFile writes the state.json file to the given directory.
func WriteStateFile(dir string, state *StateFile) (string, error) {
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create state dir: %w", err)
	}

	path := filepath.Join(dir, "state.json")
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal state: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write state file: %w", err)
	}

	return path, nil
}

// LoadStateFile reads the state.json from the given directory.
func LoadStateFile(dir string) (*StateFile, error) {
	path := filepath.Join(dir, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read state file: %w", err)
	}

	var state StateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}

	return &state, nil
}
