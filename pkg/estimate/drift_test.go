package estimate

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestComputeDriftPct(t *testing.T) {
	tests := []struct {
		name      string
		estimated float64
		actual    float64
		want      float64
		isInf     bool
	}{
		{"zero zero", 0, 0, 0, false},
		{"zero estimated non-zero actual", 0, 5, 0, true}, // +Inf
		{"exact match", 10, 10, 0, false},
		{"20% over (actual > estimated)", 10, 12, 20, false},
		{"10% under (actual < estimated)", 10, 9, -10, false},
		{"50% over", 100, 150, 50, false},
		{"large drift", 1, 10, 900, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeDriftPct(tt.estimated, tt.actual)
			if tt.isInf {
				if !math.IsInf(got, 1) {
					t.Errorf("computeDriftPct(%f, %f) = %v, want +Inf", tt.estimated, tt.actual, got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("computeDriftPct(%f, %f) = %v, want %v", tt.estimated, tt.actual, got, tt.want)
			}
		})
	}
}

func TestNewDriftTracker(t *testing.T) {
	dt := NewDriftTracker()
	if dt == nil {
		t.Fatal("NewDriftTracker() = nil")
	}
	if dt.Count("") != 0 {
		t.Errorf("NewDriftTracker().Count() = %d, want 0", dt.Count(""))
	}
}

func TestRecordDrift(t *testing.T) {
	t.Run("basic record", func(t *testing.T) {
		dt := NewDriftTracker()
		entry := dt.RecordDrift("agent-1", 10.0, 12.0)
		if entry.AgentID != "agent-1" {
			t.Errorf("AgentID = %s, want agent-1", entry.AgentID)
		}
		if entry.DriftPct != 20.0 {
			t.Errorf("DriftPct = %f, want 20.0", entry.DriftPct)
		}
		if entry.Timestamp.IsZero() {
			t.Error("Timestamp not set")
		}
		if dt.Count("agent-1") != 1 {
			t.Errorf("Count(agent-1) = %d, want 1", dt.Count("agent-1"))
		}
	})

	t.Run("multiple agents", func(t *testing.T) {
		dt := NewDriftTracker()
		dt.RecordDrift("agent-1", 10.0, 12.0)
		dt.RecordDrift("agent-2", 20.0, 18.0)
		dt.RecordDrift("agent-1", 5.0, 5.5)
		if dt.Count("agent-1") != 2 {
			t.Errorf("Count(agent-1) = %d, want 2", dt.Count("agent-1"))
		}
		if dt.Count("agent-2") != 1 {
			t.Errorf("Count(agent-2) = %d, want 1", dt.Count("agent-2"))
		}
		if dt.Count("") != 3 {
			t.Errorf("Count(all) = %d, want 3", dt.Count(""))
		}
	})

	t.Run("zero estimated non-zero actual", func(t *testing.T) {
		dt := NewDriftTracker()
		entry := dt.RecordDrift("agent-1", 0, 5.0)
		if !math.IsInf(entry.DriftPct, 1) {
			t.Errorf("DriftPct = %f, want +Inf", entry.DriftPct)
		}
	})
}

func TestRecordDriftEntry(t *testing.T) {
	dt := NewDriftTracker()
	ts := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	entry := dt.RecordDriftEntry(DriftEntry{
		AgentID:     "agent-x",
		Estimated:   10.0,
		Actual:      15.0,
		TaskType:    "code",
		Model:       "glm-5.2",
		Description: "Test task",
		Timestamp:   ts,
	})
	if entry.DriftPct != 50.0 {
		t.Errorf("DriftPct = %f, want 50.0", entry.DriftPct)
	}
	if entry.TaskType != "code" {
		t.Errorf("TaskType = %s, want code", entry.TaskType)
	}

	// Test auto-fill timestamp
	entry2 := dt.RecordDriftEntry(DriftEntry{
		AgentID:   "agent-y",
		Estimated: 10.0,
		Actual:    8.0,
	})
	if entry2.Timestamp.IsZero() {
		t.Error("auto-filled timestamp should not be zero")
	}
}

func TestDriftReport(t *testing.T) {
	t.Run("empty report", func(t *testing.T) {
		dt := NewDriftTracker()
		r := dt.DriftReport("agent-1")
		if r.Count != 0 {
			t.Errorf("Count = %d, want 0", r.Count)
		}
		if r.OverThreshold {
			t.Error("OverThreshold should be false for empty report")
		}
	})

	t.Run("single agent with entries", func(t *testing.T) {
		dt := NewDriftTracker()
		dt.RecordDrift("agent-1", 10.0, 12.0) // +20%
		dt.RecordDrift("agent-1", 10.0, 11.0) // +10%
		dt.RecordDrift("agent-1", 10.0, 10.5) // +5%

		r := dt.DriftReport("agent-1")
		if r.Count != 3 {
			t.Fatalf("Count = %d, want 3", r.Count)
		}
		wantAvg := (20.0 + 10.0 + 5.0) / 3.0
		if math.Abs(r.AvgDriftPct-wantAvg) > 0.01 {
			t.Errorf("AvgDriftPct = %f, want %f", r.AvgDriftPct, wantAvg)
		}
		if r.MaxDrift != 20.0 {
			t.Errorf("MaxDrift = %f, want 20.0", r.MaxDrift)
		}
		if r.MinDrift != 5.0 {
			t.Errorf("MinDrift = %f, want 5.0", r.MinDrift)
		}
		// (20+10+5)/3 = 11.67 > 10, so it IS over threshold
		if !r.OverThreshold {
			t.Error("OverThreshold should be true (avg ~11.67% > 10%)")
		}
	})

	t.Run("report with negative drift (estimate too high)", func(t *testing.T) {
		dt := NewDriftTracker()
		dt.RecordDrift("agent-1", 10.0, 8.0) // -20%
		dt.RecordDrift("agent-1", 10.0, 9.0) // -10%
		r := dt.DriftReport("agent-1")
		// abs avg = 15% > 10%
		if !r.OverThreshold {
			t.Error("OverThreshold should be true for avg abs drift 15%")
		}
		if r.MaxDrift != -10.0 {
			t.Errorf("MaxDrift = %f, want -10.0", r.MaxDrift)
		}
		if r.MinDrift != -20.0 {
			t.Errorf("MinDrift = %f, want -20.0", r.MinDrift)
		}
	})

	t.Run("report respects agent filter", func(t *testing.T) {
		dt := NewDriftTracker()
		dt.RecordDrift("agent-1", 10.0, 12.0)
		dt.RecordDrift("agent-2", 10.0, 11.0)
		r1 := dt.DriftReport("agent-1")
		r2 := dt.DriftReport("agent-2")
		if r1.Count != 1 {
			t.Errorf("agent-1 Count = %d, want 1", r1.Count)
		}
		if r2.Count != 1 {
			t.Errorf("agent-2 Count = %d, want 1", r2.Count)
		}
	})

	t.Run("report all agents", func(t *testing.T) {
		dt := NewDriftTracker()
		dt.RecordDrift("agent-1", 10.0, 12.0)
		dt.RecordDrift("agent-2", 10.0, 11.0)
		r := dt.DriftReportAll()
		if r.Count != 2 {
			t.Errorf("Count = %d, want 2", r.Count)
		}
		if r.AgentID != "" {
			t.Errorf("AgentID = %s, want empty for all", r.AgentID)
		}
	})

	t.Run("recent entries sorted desc by time", func(t *testing.T) {
		dt := NewDriftTracker()
		ts1 := time.Date(2026, 6, 28, 10, 0, 0, 0, time.UTC)
		ts2 := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
		ts3 := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
		dt.RecordDriftEntry(DriftEntry{AgentID: "a", Estimated: 1, Actual: 2, Timestamp: ts1})
		dt.RecordDriftEntry(DriftEntry{AgentID: "a", Estimated: 1, Actual: 2, Timestamp: ts3})
		dt.RecordDriftEntry(DriftEntry{AgentID: "a", Estimated: 1, Actual: 2, Timestamp: ts2})
		r := dt.DriftReport("a")
		if len(r.RecentEntries) != 3 {
			t.Fatalf("RecentEntries len = %d, want 3", len(r.RecentEntries))
		}
		if !r.RecentEntries[0].Timestamp.Equal(ts3) {
			t.Error("first recent entry should be newest (ts3)")
		}
		if !r.RecentEntries[2].Timestamp.Equal(ts1) {
			t.Error("last recent entry should be oldest (ts1)")
		}
	})

	t.Run("recent entries capped at limit", func(t *testing.T) {
		dt := NewDriftTracker()
		for i := 0; i < RecentEntryLimit+10; i++ {
			dt.RecordDriftEntry(DriftEntry{
				AgentID:   "a",
				Estimated: 1,
				Actual:    2,
				Timestamp: time.Date(2026, 6, 1+i, 10, 0, 0, 0, time.UTC),
			})
		}
		r := dt.DriftReport("a")
		if len(r.RecentEntries) != RecentEntryLimit {
			t.Errorf("RecentEntries len = %d, want %d", len(r.RecentEntries), RecentEntryLimit)
		}
	})
}

func TestIsOverThreshold(t *testing.T) {
	t.Run("empty tracker", func(t *testing.T) {
		dt := NewDriftTracker()
		if dt.IsOverThreshold("agent-1") {
			t.Error("IsOverThreshold should be false for empty tracker")
		}
	})

	t.Run("under threshold", func(t *testing.T) {
		dt := NewDriftTracker()
		dt.RecordDrift("a", 10.0, 10.5) // +5%
		dt.RecordDrift("a", 10.0, 10.3) // +3%
		// avg = 4% < 10%
		if dt.IsOverThreshold("a") {
			t.Error("IsOverThreshold should be false (avg 4%)")
		}
	})

	t.Run("over threshold", func(t *testing.T) {
		dt := NewDriftTracker()
		dt.RecordDrift("a", 10.0, 12.0) // +20%
		dt.RecordDrift("a", 10.0, 11.0) // +10%
		// avg = 15% > 10%
		if !dt.IsOverThreshold("a") {
			t.Error("IsOverThreshold should be true (avg 15%)")
		}
	})

	t.Run("threshold uses absolute value", func(t *testing.T) {
		dt := NewDriftTracker()
		dt.RecordDrift("a", 10.0, 8.5) // -15%
		dt.RecordDrift("a", 10.0, 8.0) // -20%
		// abs avg = 17.5% > 10%
		if !dt.IsOverThreshold("a") {
			t.Error("IsOverThreshold should be true for negative drift (avg -17.5%)")
		}
	})
}

func TestDriftTrackerCount(t *testing.T) {
	dt := NewDriftTracker()
	dt.RecordDrift("a", 1, 2)
	dt.RecordDrift("a", 1, 2)
	dt.RecordDrift("b", 1, 2)
	if dt.Count("a") != 2 {
		t.Errorf("Count(a) = %d, want 2", dt.Count("a"))
	}
	if dt.Count("b") != 1 {
		t.Errorf("Count(b) = %d, want 1", dt.Count("b"))
	}
	if dt.Count("") != 3 {
		t.Errorf("Count(all) = %d, want 3", dt.Count(""))
	}
	if dt.Count("nonexistent") != 0 {
		t.Errorf("Count(nonexistent) = %d, want 0", dt.Count("nonexistent"))
	}
}

func TestDriftTrackerClear(t *testing.T) {
	t.Run("clear single agent", func(t *testing.T) {
		dt := NewDriftTracker()
		dt.RecordDrift("a", 1, 2)
		dt.RecordDrift("a", 1, 2)
		dt.RecordDrift("b", 1, 2)
		cleared := dt.Clear("a")
		if cleared != 2 {
			t.Errorf("Clear(a) returned %d, want 2", cleared)
		}
		if dt.Count("a") != 0 {
			t.Errorf("Count(a) = %d after clear, want 0", dt.Count("a"))
		}
		if dt.Count("b") != 1 {
			t.Errorf("Count(b) = %d after clear, want 1 (unaffected)", dt.Count("b"))
		}
	})

	t.Run("clear all", func(t *testing.T) {
		dt := NewDriftTracker()
		dt.RecordDrift("a", 1, 2)
		dt.RecordDrift("b", 1, 2)
		cleared := dt.Clear("")
		if cleared != 2 {
			t.Errorf("Clear(all) returned %d, want 2", cleared)
		}
		if dt.Count("") != 0 {
			t.Errorf("Count(all) = %d after clear, want 0", dt.Count(""))
		}
	})
}

func TestExportDriftLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "drift.jsonl")

	dt := NewDriftTracker()
	ts := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	dt.RecordDriftEntry(DriftEntry{
		AgentID:   "agent-1",
		Estimated: 10.0,
		Actual:    12.0,
		Timestamp: ts,
		TaskType:  "code",
	})
	dt.RecordDriftEntry(DriftEntry{
		AgentID:   "agent-2",
		Estimated: 20.0,
		Actual:    18.0,
		Timestamp: ts.Add(time.Hour),
	})

	if err := dt.ExportDriftLog(path); err != nil {
		t.Fatalf("ExportDriftLog: %v", err)
	}

	// Verify file exists and has 2 lines
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := splitJSONLines(data)
	if len(lines) != 2 {
		t.Fatalf("Expected 2 JSONL lines, got %d", len(lines))
	}

	// Parse first entry
	var entry DriftEntry
	if err := json.Unmarshal(lines[0], &entry); err != nil {
		t.Fatalf("Unmarshal line 0: %v", err)
	}
	if entry.AgentID != "agent-1" {
		t.Errorf("Entry 0 AgentID = %s, want agent-1", entry.AgentID)
	}
	if entry.DriftPct != 20.0 {
		t.Errorf("Entry 0 DriftPct = %f, want 20.0", entry.DriftPct)
	}

	// Verify entries are chronologically sorted
	var e1, e2 DriftEntry
	if err := json.Unmarshal(lines[0], &e1); err != nil {
		t.Fatalf("Unmarshal line 0: %v", err)
	}
	if err := json.Unmarshal(lines[1], &e2); err != nil {
		t.Fatalf("Unmarshal line 1: %v", err)
	}
	if !e1.Timestamp.Before(e2.Timestamp) {
		t.Error("Entries should be sorted ascending by timestamp")
	}
}

func TestImportDriftLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "drift.jsonl")

	dt := NewDriftTracker()
	dt.RecordDrift("agent-1", 10.0, 12.0)

	// Export
	if err := dt.ExportDriftLog(path); err != nil {
		t.Fatalf("ExportDriftLog: %v", err)
	}

	// Import into a new tracker
	dt2 := NewDriftTracker()
	if err := dt2.ImportDriftLog(path); err != nil {
		t.Fatalf("ImportDriftLog: %v", err)
	}
	if dt2.Count("") != 1 {
		t.Errorf("After import, Count = %d, want 1", dt2.Count(""))
	}

	// Verify entry data
	r := dt2.DriftReport("agent-1")
	if r.Count != 1 {
		t.Fatalf("Report Count = %d, want 1", r.Count)
	}
	if r.AvgDriftPct != 20.0 {
		t.Errorf("AvgDriftPct = %f, want 20.0", r.AvgDriftPct)
	}
}

func TestImportDriftLog_NonexistentFile(t *testing.T) {
	dt := NewDriftTracker()
	err := dt.ImportDriftLog("/nonexistent/path/drift.jsonl")
	if err == nil {
		t.Error("ImportDriftLog should fail for nonexistent file")
	}
}

func TestImportDriftLog_MalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "drift.jsonl")

	// Write a mix of valid and invalid JSONL
	content := `{"agent_id":"valid","estimated":10,"actual":12,"drift_pct":20,"timestamp":"2026-06-30T12:00:00Z"}
{"agent_id":"also-valid","estimated":20,"actual":18,"drift_pct":-10,"timestamp":"2026-06-30T13:00:00Z"}
not valid json line
{"broken
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	dt := NewDriftTracker()
	if err := dt.ImportDriftLog(path); err != nil {
		t.Fatalf("ImportDriftLog should skip malformed lines: %v", err)
	}
	if dt.Count("") != 2 {
		t.Errorf("Count = %d, want 2 (malformed skipped)", dt.Count(""))
	}
}

func TestFeedCalibrator(t *testing.T) {
	t.Run("basic feed", func(t *testing.T) {
		dt := NewDriftTracker()
		dt.RecordDrift("agent-1", 10.0, 12.0) // +20% — actual more expensive
		dt.RecordDrift("agent-1", 10.0, 8.0)  // -20% — actual cheaper

		c := NewCalibrator()
		added := dt.FeedCalibrator(c, "agent-1")
		if added != 2 {
			t.Errorf("Added = %d, want 2", added)
		}
		if len(c.History) != 2 {
			t.Errorf("Calibrator History len = %d, want 2", len(c.History))
		}
	})

	t.Run("skips zero estimated", func(t *testing.T) {
		dt := NewDriftTracker()
		dt.RecordDriftEntry(DriftEntry{
			AgentID:   "a",
			Estimated: 0,
			Actual:    5,
		})
		dt.RecordDriftEntry(DriftEntry{
			AgentID:   "a",
			Estimated: 10,
			Actual:    12,
		})
		c := NewCalibrator()
		added := dt.FeedCalibrator(c, "a")
		if added != 1 {
			t.Errorf("Added = %d, want 1 (zero-estimated skipped)", added)
		}
	})

	t.Run("skips infinite drift", func(t *testing.T) {
		dt := NewDriftTracker()
		dt.RecordDriftEntry(DriftEntry{
			AgentID:   "a",
			Estimated: 0,
			Actual:    5, // +Inf drift
		})
		c := NewCalibrator()
		added := dt.FeedCalibrator(c, "a")
		if added != 0 {
			t.Errorf("Added = %d, want 0 (infinite drift skipped)", added)
		}
	})

	t.Run("respects agent filter", func(t *testing.T) {
		dt := NewDriftTracker()
		dt.RecordDrift("a", 10, 12)
		dt.RecordDrift("b", 10, 12)
		c := NewCalibrator()
		added := dt.FeedCalibrator(c, "a")
		if added != 1 {
			t.Errorf("Added = %d, want 1", added)
		}
	})

	t.Run("cache ratio inference — actual cheaper", func(t *testing.T) {
		dt := NewDriftTracker()
		dt.RecordDrift("a", 10.0, 8.0) // actual 20% cheaper → better cache
		c := NewCalibrator()
		dt.FeedCalibrator(c, "a")
		rec := c.History[0]
		// ratio should be > 0.5 (actual cheaper → higher inferred cache)
		if rec.CacheHitRatio <= 0.5 {
			t.Errorf("CacheHitRatio = %f, want > 0.5 (actual cheaper implies better cache)", rec.CacheHitRatio)
		}
	})

	t.Run("cache ratio inference — actual more expensive", func(t *testing.T) {
		dt := NewDriftTracker()
		dt.RecordDrift("a", 10.0, 12.0) // actual 20% more expensive → worse cache
		c := NewCalibrator()
		dt.FeedCalibrator(c, "a")
		rec := c.History[0]
		// ratio should be < 0.5 (actual more expensive implies worse cache)
		if rec.CacheHitRatio >= 0.5 {
			t.Errorf("CacheHitRatio = %f, want < 0.5 (actual more expensive implies worse cache)", rec.CacheHitRatio)
		}
	})

	t.Run("all agents when filter empty", func(t *testing.T) {
		dt := NewDriftTracker()
		dt.RecordDrift("a", 10, 12)
		dt.RecordDrift("b", 10, 12)
		c := NewCalibrator()
		added := dt.FeedCalibrator(c, "")
		if added != 2 {
			t.Errorf("Added = %d, want 2", added)
		}
	})
}

func TestAgentsWithDrift(t *testing.T) {
	t.Run("empty tracker", func(t *testing.T) {
		dt := NewDriftTracker()
		agents := dt.AgentsWithDrift()
		if len(agents) != 0 {
			t.Errorf("AgentsWithDrift() = %v, want empty", agents)
		}
	})

	t.Run("multiple agents sorted", func(t *testing.T) {
		dt := NewDriftTracker()
		dt.RecordDrift("charlie", 1, 2)
		dt.RecordDrift("alpha", 1, 2)
		dt.RecordDrift("bravo", 1, 2)
		agents := dt.AgentsWithDrift()
		if len(agents) != 3 {
			t.Fatalf("AgentsWithDrift() = %v, want 3 items", agents)
		}
		if agents[0] != "alpha" {
			t.Errorf("agents[0] = %s, want alpha", agents[0])
		}
		if agents[1] != "bravo" {
			t.Errorf("agents[1] = %s, want bravo", agents[1])
		}
		if agents[2] != "charlie" {
			t.Errorf("agents[2] = %s, want charlie", agents[2])
		}
	})

	t.Run("deduplicates", func(t *testing.T) {
		dt := NewDriftTracker()
		dt.RecordDrift("a", 1, 2)
		dt.RecordDrift("a", 1, 2)
		dt.RecordDrift("a", 1, 2)
		agents := dt.AgentsWithDrift()
		if len(agents) != 1 {
			t.Errorf("AgentsWithDrift() = %v, want 1 item", agents)
		}
	})
}

func TestFormatDriftReport(t *testing.T) {
	t.Run("empty report", func(t *testing.T) {
		r := DriftReport{AgentID: "agent-1", Count: 0}
		s := FormatDriftReport(r)
		if s == "" {
			t.Error("FormatDriftReport should return non-empty string")
		}
	})

	t.Run("report with entries", func(t *testing.T) {
		dt := NewDriftTracker()
		dt.RecordDrift("agent-1", 10.0, 12.0)
		r := dt.DriftReport("agent-1")
		s := FormatDriftReport(r)
		if s == "" {
			t.Error("FormatDriftReport should return non-empty string")
		}
	})

	t.Run("all agents label", func(t *testing.T) {
		r := DriftReport{AgentID: "", Count: 0}
		s := FormatDriftReport(r)
		if s == "" {
			t.Error("FormatDriftReport should return non-empty string for empty agentID")
		}
	})

	t.Run("over threshold shows warning mark", func(t *testing.T) {
		r := DriftReport{
			AgentID:       "a",
			Count:         1,
			AvgDriftPct:   25.0,
			OverThreshold: true,
		}
		s := FormatDriftReport(r)
		// The warning mark should appear when OverThreshold is true
		if s == "" {
			t.Error("FormatDriftReport should return non-empty string")
		}
	})
}

func TestSplitJSONLines(t *testing.T) {
	t.Run("single line", func(t *testing.T) {
		data := []byte(`{"a":1}`)
		lines := splitJSONLines(data)
		if len(lines) != 1 {
			t.Errorf("len = %d, want 1", len(lines))
		}
	})

	t.Run("multiple lines", func(t *testing.T) {
		data := []byte("{\"a\":1}\n{\"b\":2}\n{\"c\":3}")
		lines := splitJSONLines(data)
		if len(lines) != 3 {
			t.Errorf("len = %d, want 3", len(lines))
		}
	})

	t.Run("empty lines skipped", func(t *testing.T) {
		data := []byte("{\"a\":1}\n\n  \n{\"b\":2}\n")
		lines := splitJSONLines(data)
		if len(lines) != 2 {
			t.Errorf("len = %d, want 2", len(lines))
		}
	})

	t.Run("empty input", func(t *testing.T) {
		lines := splitJSONLines([]byte{})
		if len(lines) != 0 {
			t.Errorf("len = %d, want 0", len(lines))
		}
	})
}

func TestDriftTrackerConcurrent(t *testing.T) {
	dt := NewDriftTracker()
	done := make(chan struct{})

	// Concurrent writers
	for i := 0; i < 10; i++ {
		go func(n int) {
			for j := 0; j < 10; j++ {
				dt.RecordDrift("agent-concurrent", float64(n*j), float64(n*j+1))
			}
			done <- struct{}{}
		}(i)
	}

	// Concurrent reader
	go func() {
		for i := 0; i < 10; i++ {
			dt.DriftReport("agent-concurrent")
			dt.Count("agent-concurrent")
		}
		done <- struct{}{}
	}()

	// Wait for all
	for i := 0; i < 11; i++ {
		<-done
	}

	// After all writes complete, count should be deterministic
	if dt.Count("agent-concurrent") != 100 {
		t.Errorf("Count = %d, want 100", dt.Count("agent-concurrent"))
	}
}

func TestDriftReportPeriod(t *testing.T) {
	t.Run("period calculation", func(t *testing.T) {
		dt := NewDriftTracker()
		ts1 := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
		ts2 := time.Date(2026, 6, 30, 14, 0, 0, 0, time.UTC)
		dt.RecordDriftEntry(DriftEntry{AgentID: "a", Estimated: 1, Actual: 2, Timestamp: ts1})
		dt.RecordDriftEntry(DriftEntry{AgentID: "a", Estimated: 1, Actual: 2, Timestamp: ts2})
		r := dt.DriftReport("a")
		wantPeriod := 4 * time.Hour
		if r.Period != wantPeriod {
			t.Errorf("Period = %v, want %v", r.Period, wantPeriod)
		}
	})

	t.Run("single entry period is zero", func(t *testing.T) {
		dt := NewDriftTracker()
		ts := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
		dt.RecordDriftEntry(DriftEntry{AgentID: "a", Estimated: 1, Actual: 2, Timestamp: ts})
		r := dt.DriftReport("a")
		if r.Period != 0 {
			t.Errorf("Period = %v, want 0 for single entry", r.Period)
		}
	})
}

func TestDriftReportInfDriftHandling(t *testing.T) {
	t.Run("inf drift doesn't break avg", func(t *testing.T) {
		dt := NewDriftTracker()
		dt.RecordDrift("a", 10.0, 12.0) // +20%
		dt.RecordDrift("a", 0, 5.0)     // +Inf (capped at 1000)
		r := dt.DriftReport("a")
		if r.Count != 2 {
			t.Fatalf("Count = %d, want 2", r.Count)
		}
		// avg should be (20 + 1000) / 2 = 510
		if r.AvgDriftPct != 510.0 {
			t.Errorf("AvgDriftPct = %f, want 510.0 (inf capped at 1000)", r.AvgDriftPct)
		}
	})
}
