package estimate

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestEstimationLogger_LogVerbose(t *testing.T) {
	var buf bytes.Buffer
	el := NewEstimationLoggerWithWriter(true, &buf)

	el.LogVerbose("test-agent", "code", "deepseek-v4-pro", 1.23, 60, 8.77, StatusAutoApproved)

	out := buf.String()
	if !strings.Contains(out, "agent=test-agent") {
		t.Error("missing agent field")
	}
	if !strings.Contains(out, "task_type=code") {
		t.Error("missing task_type field")
	}
	if !strings.Contains(out, "model=deepseek-v4-pro") {
		t.Error("missing model field")
	}
	if !strings.Contains(out, "estimated=$1.23") {
		t.Error("missing estimated field")
	}
	if !strings.Contains(out, "cache_hit=60%") {
		t.Error("missing cache_hit field")
	}
	if !strings.Contains(out, "budget_remaining=$8.77") {
		t.Error("missing budget_remaining field")
	}
	if !strings.Contains(out, "decision=AUTO_APPROVED") {
		t.Error("missing decision field")
	}
}

func TestEstimationLogger_LogVerbose_DisabledWhenNotVerbose(t *testing.T) {
	var buf bytes.Buffer
	el := NewEstimationLoggerWithWriter(false, &buf)

	el.LogVerbose("test-agent", "code", "model", 1.0, 50, 10.0, StatusAutoApproved)

	if buf.Len() > 0 {
		t.Error("non-verbose logger should not output")
	}
}

func TestEstimationLogger_LogError(t *testing.T) {
	var buf bytes.Buffer
	el := NewEstimationLoggerWithWriter(true, &buf)

	el.LogError("test-agent", errFake("connection refused"))

	out := buf.String()
	if !strings.Contains(out, "[ERROR]") {
		t.Error("missing ERROR level")
	}
	if !strings.Contains(out, "agent=test-agent") {
		t.Error("missing agent")
	}
	if !strings.Contains(out, "connection refused") {
		t.Error("missing error message")
	}
}

func TestEstimationLogger_LogDrift(t *testing.T) {
	var buf bytes.Buffer
	el := NewEstimationLoggerWithWriter(true, &buf)

	el.LogDrift("test-agent", 1.00, 1.23)

	out := buf.String()
	if !strings.Contains(out, "[METRIC]") {
		t.Error("missing METRIC level")
	}
	if !strings.Contains(out, "drift_pct=") {
		t.Error("missing drift_pct field")
	}
	if !strings.Contains(out, "agent=test-agent") {
		t.Error("missing agent")
	}
}

func TestEstimationLogger_LogRecalibration(t *testing.T) {
	var buf bytes.Buffer
	el := NewEstimationLoggerWithWriter(true, &buf)

	el.LogRecalibration("test-agent", 25.5, 20)

	out := buf.String()
	if !strings.Contains(out, "[WARN]") {
		t.Error("missing WARN level")
	}
	if !strings.Contains(out, "recalibration_flag=true") {
		t.Error("missing recalibration_flag")
	}
	if !strings.Contains(out, "avg_drift=25.5%") {
		t.Error("missing avg_drift")
	}
	if !strings.Contains(out, "over_20_tasks") {
		t.Error("missing task count")
	}
}

func TestEstimationLogger_LogEstimation_JSON(t *testing.T) {
	var buf bytes.Buffer
	el := NewEstimationLoggerWithWriter(true, &buf)

	entry := EstimationLog{
		Timestamp:       time.Now().UTC(),
		Level:           "INFO",
		Agent:           "test-agent",
		TaskType:        "code",
		Model:           "deepseek-v4-pro",
		Provider:        "deepseek",
		Estimated:       1.23,
		CacheHitPct:     60,
		BudgetRemaining: 8.77,
		Decision:        StatusAutoApproved,
	}

	el.LogEstimation(entry)

	out := buf.String()
	if !strings.Contains(out, "\"agent\":\"test-agent\"") {
		t.Error("JSON output missing agent field")
	}
	if !strings.Contains(out, "\"estimated\":1.23") {
		t.Error("JSON output missing estimated field")
	}
}

func TestWriteAndReadEstimationRecords(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/estimation.jsonl"

	entries := []EstimationLog{
		{
			Timestamp:       time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
			Agent:           "agent-1",
			TaskType:        "code",
			Model:           "deepseek-v4-pro",
			Estimated:       1.00,
			CostInput:       0.80,
			CostOutput:      0.30,
			Decision:        StatusAutoApproved,
			BudgetRemaining: 9.00,
		},
		{
			Timestamp:       time.Date(2026, 7, 1, 12, 5, 0, 0, time.UTC),
			Agent:           "agent-1",
			TaskType:        "review",
			Model:           "deepseek-v4-flash",
			Estimated:       0.50,
			CostInput:       0.35,
			CostOutput:      0.20,
			Decision:        StatusAutoApproved,
			BudgetRemaining: 8.45,
		},
	}

	for _, e := range entries {
		if err := WriteEstimationRecord(path, e); err != nil {
			t.Fatalf("WriteEstimationRecord: %v", err)
		}
	}

	records, err := ReadEstimationRecords(path)
	if err != nil {
		t.Fatalf("ReadEstimationRecords: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("read %d records, want 2", len(records))
	}

	if records[0].Agent != "agent-1" {
		t.Errorf("record[0] Agent = %q, want agent-1", records[0].Agent)
	}
	if records[1].TaskType != "review" {
		t.Errorf("record[1] TaskType = %q, want review", records[1].TaskType)
	}
}

func TestCheckRecalibration_Triggered(t *testing.T) {
	// 20 records all with 25% drift
	records := make([]EstimationLog, 20)
	for i := range records {
		records[i] = EstimationLog{
			Estimated:  1.00,
			CostInput:  1.00,
			CostOutput: 0.25,
		}
	}

	flagged, avgDrift := CheckRecalibration(records, 20.0, 20)

	if !flagged {
		t.Error("should trigger recalibration with 25% avg drift")
	}
	if avgDrift < 24.0 || avgDrift > 26.0 {
		t.Errorf("avgDrift = %.1f, want ~25%%", avgDrift)
	}
}

func TestCheckRecalibration_NotTriggered_LowDrift(t *testing.T) {
	records := make([]EstimationLog, 20)
	for i := range records {
		records[i] = EstimationLog{
			Estimated:  1.00,
			CostInput:  0.90,
			CostOutput: 0.05, // actual = 0.95, drift = -5%
		}
	}

	flagged, _ := CheckRecalibration(records, 20.0, 20)

	if flagged {
		t.Error("should NOT trigger recalibration with -5% drift")
	}
}

func TestCheckRecalibration_NotTriggered_TooFewTasks(t *testing.T) {
	records := make([]EstimationLog, 5) // only 5, need 20
	for i := range records {
		records[i] = EstimationLog{
			Estimated:  1.00,
			CostInput:  1.50,
			CostOutput: 0.50, // 100% drift
		}
	}

	flagged, _ := CheckRecalibration(records, 20.0, 20)

	if flagged {
		t.Error("should NOT trigger with fewer than minTasks")
	}
}

func TestEstimationLogger_NilSafe(t *testing.T) {
	var el *EstimationLogger

	// All methods should be nil-safe
	el.LogVerbose("a", "b", "c", 1, 50, 10, StatusAutoApproved)
	el.LogError("a", errFake("x"))
	el.LogDrift("a", 1, 2)
	el.LogRecalibration("a", 10, 5)
	el.LogEstimation(EstimationLog{})
}

func TestSplitJSONL(t *testing.T) {
	data := []byte(`{"a":1}
{"b":2}
{"c":3}`)

	lines := splitJSONL(data)

	if len(lines) != 3 {
		t.Fatalf("split into %d lines, want 3", len(lines))
	}
	if string(lines[0]) != `{"a":1}` {
		t.Errorf("line[0] = %q", lines[0])
	}
	if string(lines[2]) != `{"c":3}` {
		t.Errorf("line[2] = %q", lines[2])
	}
}

func TestReadEstimationRecords_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/empty.jsonl"

	records, err := ReadEstimationRecords(path)
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
	if records != nil {
		t.Errorf("expected nil records, got %d", len(records))
	}
}

// errFake is a simple test error type.
type errFake string

func (e errFake) Error() string { return string(e) }
