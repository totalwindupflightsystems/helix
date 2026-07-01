package estimate

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"time"
)

// ---------------------------------------------------------------------------
// Estimation Logger — spec §14 (Observability)
// ---------------------------------------------------------------------------

// EstimationLog records a single estimation event for observability and later
// reconciliation. Per spec §14: "Estimation records written to JSON files for
// later reconciliation. Drift metric logged as a gauge for each task."
type EstimationLog struct {
	Timestamp       time.Time      `json:"timestamp"`
	Level           string         `json:"level"`
	Agent           string         `json:"agent"`
	TaskType        string         `json:"task_type"`
	Model           string         `json:"model"`
	Provider        string         `json:"provider"`
	Estimated       float64        `json:"estimated"`
	CacheHitPct     float64        `json:"cache_hit_pct"`
	BudgetRemaining float64        `json:"budget_remaining"`
	Decision        ApprovalStatus `json:"decision"`
	CostInput       float64        `json:"cost_input,omitempty"`
	CostOutput      float64        `json:"cost_output,omitempty"`
	FreshInputTok   int            `json:"fresh_input_tokens,omitempty"`
	CacheHitTok     int            `json:"cache_hit_tokens,omitempty"`
	OutputTok       int            `json:"output_tokens,omitempty"`
	Extra           map[string]any `json:"extra,omitempty"`
}

// EstimationLogger writes structured estimation records per spec §14.
// Default output is stderr (via stdlib log). When Verbose is true, every
// estimation step is logged with the structured format from §14:
//
//	timestamp [level] agent=NAME task_type=CODE model=deepseek-v4-pro
//	estimated=$1.23 cache_hit=60% budget_remaining=$8.77 decision=AUTO_APPROVED
type EstimationLogger struct {
	Verbose bool
	Writer  io.Writer
	Logger  *log.Logger
}

// NewEstimationLogger creates a logger. If verbose is false, only errors are
// logged. Output goes to stderr by default.
func NewEstimationLogger(verbose bool) *EstimationLogger {
	w := io.Writer(os.Stderr)
	return &EstimationLogger{
		Verbose: verbose,
		Writer:  w,
		Logger:  log.New(w, "", 0), // no prefix — we format our own
	}
}

// NewEstimationLoggerWithWriter creates a logger with a custom writer (for testing).
func NewEstimationLoggerWithWriter(verbose bool, w io.Writer) *EstimationLogger {
	return &EstimationLogger{
		Verbose: verbose,
		Writer:  w,
		Logger:  log.New(w, "", 0),
	}
}

// LogEstimation writes a structured estimation record.
func (el *EstimationLogger) LogEstimation(entry EstimationLog) {
	if el == nil || el.Logger == nil {
		return
	}

	// Always write JSON record for machine-readable reconciliation
	if el.Verbose {
		jsonBytes, _ := json.Marshal(entry)
		el.Logger.Println(string(jsonBytes))
	}
}

// LogVerbose writes the human-readable verbose format from spec §14.
func (el *EstimationLogger) LogVerbose(agent, taskType, model string, estimated float64,
	cacheHitPct float64, budgetRemaining float64, decision ApprovalStatus) {
	if el == nil || el.Logger == nil || !el.Verbose {
		return
	}

	msg := fmt.Sprintf("%s [INFO] agent=%s task_type=%s model=%s estimated=$%.2f cache_hit=%.0f%% budget_remaining=$%.2f decision=%s",
		time.Now().UTC().Format(time.RFC3339),
		agent,
		taskType,
		model,
		estimated,
		cacheHitPct,
		budgetRemaining,
		decision,
	)
	el.Logger.Println(msg)
}

// LogError writes an error-level estimation record.
func (el *EstimationLogger) LogError(agent string, err error) {
	if el == nil || el.Logger == nil {
		return
	}

	msg := fmt.Sprintf("%s [ERROR] agent=%s error=%s",
		time.Now().UTC().Format(time.RFC3339),
		agent,
		err.Error(),
	)
	el.Logger.Println(msg)
}

// LogDrift logs a drift gauge for a completed task (spec §14: "Drift metric:
// (actual - estimated) / estimated logged as a gauge for each task.").
func (el *EstimationLogger) LogDrift(agent string, estimated, actual float64) {
	if el == nil || el.Logger == nil || !el.Verbose {
		return
	}

	driftPct := 0.0
	if estimated > 0 {
		driftPct = ((actual - estimated) / estimated) * 100
	}

	msg := fmt.Sprintf("%s [METRIC] agent=%s drift_pct=%.1f estimated=$%.4f actual=$%.4f",
		time.Now().UTC().Format(time.RFC3339),
		agent,
		driftPct,
		estimated,
		actual,
	)
	el.Logger.Println(msg)
}

// LogRecalibration logs a recalibration flag when persistent drift exceeds
// threshold (spec §14: "Persistent drift >20% over 20 tasks triggers a
// recalibration flag.").
func (el *EstimationLogger) LogRecalibration(agent string, avgDriftPct float64, taskCount int) {
	if el == nil || el.Logger == nil {
		return
	}

	msg := fmt.Sprintf("%s [WARN] agent=%s recalibration_flag=true avg_drift=%.1f%% over_%d_tasks",
		time.Now().UTC().Format(time.RFC3339),
		agent,
		avgDriftPct,
		taskCount,
	)
	el.Logger.Println(msg)
}

// WriteEstimationRecord writes an estimation record as a JSON file for later
// reconciliation (spec §14: "Estimation records written to JSON files for
// later reconciliation.").
func WriteEstimationRecord(path string, entry EstimationLog) error {
	jsonBytes, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal estimation record: %w", err)
	}
	jsonBytes = append(jsonBytes, '\n')

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open estimation log file %q: %w", path, err)
	}
	defer f.Close()

	if _, err := f.Write(jsonBytes); err != nil {
		return fmt.Errorf("write estimation record: %w", err)
	}

	return nil
}

// ReadEstimationRecords reads JSONL estimation records from a file.
func ReadEstimationRecords(path string) ([]EstimationLog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read estimation log file %q: %w", path, err)
	}

	var records []EstimationLog
	for _, line := range splitJSONL(data) {
		if len(line) == 0 {
			continue
		}
		var rec EstimationLog
		if err := json.Unmarshal(line, &rec); err != nil {
			continue // skip malformed lines
		}
		records = append(records, rec)
	}

	return records, nil
}

// splitJSONL splits JSONL data into individual JSON byte slices.
func splitJSONL(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

// CheckRecalibration evaluates whether the average drift over the last N
// estimation records exceeds the threshold (spec §14: 20% over 20 tasks).
// Returns true if recalibration is warranted.
func CheckRecalibration(records []EstimationLog, threshold float64, minTasks int) (bool, float64) {
	if len(records) < minTasks {
		return false, 0
	}

	// Take the last minTasks records
	start := len(records) - minTasks
	if start < 0 {
		start = 0
	}
	recent := records[start:]

	totalDrift := 0.0
	count := 0
	for _, rec := range recent {
		if rec.Estimated > 0 && rec.CostInput+rec.CostOutput > 0 {
			actual := rec.CostInput + rec.CostOutput
			driftPct := ((actual - rec.Estimated) / rec.Estimated) * 100
			totalDrift += driftPct
			count++
		}
	}

	if count == 0 {
		return false, 0
	}

	avgDrift := totalDrift / float64(count)
	return avgDrift > threshold, avgDrift
}
