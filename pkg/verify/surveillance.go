package verify

// surveillance.go implements Phase 3 steady-state surveillance per
// specs/production-verification.md §Phase 3 — Steady-State Surveillance (72h+).
//
// After shadow verification (Phase 1) and canary verification (Phase 2), the
// surveillance system runs continuously:
//
//  1. Aggregates metrics from multiple sources into time-windowed views
//  2. Evaluates behavior contracts periodically
//  3. Detects gradual degradation over 7-day windows
//  4. Escalates alerts when sustained drift persists beyond thresholds
//
// This integrates with the existing DriftDetector (immediate drift),
// BehaviorContract/Monitor (breach detection), and NotificationDispatcher
// (multi-channel alerts).

import (
	"fmt"
	"time"
)

// ============================================================================
// Surveillance Status and Events
// ============================================================================

// SurveillanceStatus classifies the health of an agent under surveillance.
type SurveillanceStatus string

const (
	StatusHealthy  SurveillanceStatus = "healthy"  // all contracts pass, no drift
	StatusWarning  SurveillanceStatus = "warning"  // minor drift detected, no breach
	StatusDegraded SurveillanceStatus = "degraded" // gradual degradation over long window
	StatusBreach   SurveillanceStatus = "breach"   // behavior contract breach detected
)

// SurveillanceEvent is emitted on every periodic surveillance check.
// Callers consume events from the Events() channel or the Events slice.
type SurveillanceEvent struct {
	Timestamp       time.Time          `json:"timestamp"`
	ContractName    string             `json:"contract_name"`
	AgentID         string             `json:"agent_id"`
	Status          SurveillanceStatus `json:"status"`
	Metrics         MetricsSnapshot    `json:"metrics"`
	Breaches        []Breach           `json:"breaches,omitempty"`
	DriftAssessment *DriftAssessment   `json:"drift_assessment,omitempty"`
	Degradation     *DegradationReport `json:"degradation,omitempty"`
	EscalationLevel EscalationLevel    `json:"escalation_level"`
	Message         string             `json:"message"`
}

// IsActionable returns true if the event requires intervention.
func (e *SurveillanceEvent) IsActionable() bool {
	return e.Status == StatusBreach || e.Status == StatusDegraded ||
		e.EscalationLevel == EscalationInvestigate ||
		e.EscalationLevel == EscalationRollback
}

// Summary returns a one-line human-readable summary.
func (e *SurveillanceEvent) Summary() string {
	breachCount := len(e.Breaches)
	driftBreach := false
	if e.DriftAssessment != nil {
		driftBreach = e.DriftAssessment.AnyBreach
	}
	return fmt.Sprintf("[%s] agent=%s contract=%s breaches=%d drift_breach=%v escalation=%s",
		e.Status, e.AgentID, e.ContractName, breachCount, driftBreach, e.EscalationLevel)
}

// ============================================================================
// Daily Summary — long-window aggregation
// ============================================================================

// DailySummary is an aggregated snapshot of metrics for a single calendar day.
// The LongRunningMonitor uses these to detect gradual degradation trends.
type DailySummary struct {
	Date           time.Time `json:"date"`
	AvgSuccessRate float64   `json:"avg_success_rate"`
	AvgP99Latency  float64   `json:"avg_p99_latency_ms"`
	AvgP50Latency  float64   `json:"avg_p50_latency_ms"`
	TotalErrors    int64     `json:"total_errors"`
	TotalRequests  int64     `json:"total_requests"`
	BreachCount    int       `json:"breach_count"`
	SampleCount    int       `json:"sample_count"`
}

// ============================================================================
// Degradation Report — output of LongRunningMonitor
// ============================================================================

// DegradationDirection indicates the trend direction of a metric over the
// long observation window.
type DegradationDirection string

const (
	DegradationNone      DegradationDirection = "none"
	DegradationWorsening DegradationDirection = "worsening"
	DegradationImproving DegradationDirection = "improving"
)

// MetricDegradation tracks a single metric's gradual degradation.
type MetricDegradation struct {
	Metric       string               `json:"metric"`
	FirstDay     float64              `json:"first_day"`
	LastDay      float64              `json:"last_day"`
	ChangePct    float64              `json:"change_pct"`
	ThresholdPct float64              `json:"threshold_pct"`
	Exceeds      bool                 `json:"exceeds"`
	Direction    DegradationDirection `json:"direction"`
}

// DegradationReport aggregates per-metric degradation analysis for an agent
// over the long observation window (default 7 days).
type DegradationReport struct {
	AgentID         string              `json:"agent_id"`
	WindowStart     time.Time           `json:"window_start"`
	WindowEnd       time.Time           `json:"window_end"`
	WindowDays      int                 `json:"window_days"`
	MetricTrends    []MetricDegradation `json:"metric_trends"`
	IsDegrading     bool                `json:"is_degrading"`
	Severity        BreachSeverity      `json:"severity"`
	AffectedMetrics int                 `json:"affected_metrics"`
}

// Summary returns a human-readable degradation summary.
func (d *DegradationReport) Summary() string {
	return fmt.Sprintf("agent %s over %dd: %d/%d metrics degrading, severity=%s",
		d.AgentID, d.WindowDays, d.AffectedMetrics, len(d.MetricTrends), d.Severity)
}

// ============================================================================
// Escalation
// ============================================================================

// EscalationLevel tracks how far an alert has been escalated.
type EscalationLevel string

const (
	EscalationNone        EscalationLevel = "none"
	EscalationNotify      EscalationLevel = "notify"
	EscalationInvestigate EscalationLevel = "investigate"
	EscalationRollback    EscalationLevel = "rollback"
)

// IsActionable returns true if the escalation level requires intervention.
func (l EscalationLevel) IsActionable() bool {
	return l == EscalationInvestigate || l == EscalationRollback
}

// ============================================================================
// Configuration Types
// ============================================================================

// SurveillanceConfig holds all configurable thresholds for steady-state
// surveillance.
type SurveillanceConfig struct {
	// CheckInterval is how often contracts are evaluated against fresh metrics.
	// Default: 5 minutes.
	CheckInterval time.Duration `json:"check_interval"`

	// LongWindowDays is the number of days used for gradual degradation analysis.
	// Spec: 7-day windows. Default: 7.
	LongWindowDays int `json:"long_window_days"`

	// Sensitivity controls per-metric drift thresholds for short-window drift.
	Sensitivity DriftSensitivity `json:"sensitivity"`

	// LongRunningThresholds controls gradual degradation detection.
	LongRunningThresholds LongRunningThresholds `json:"long_running_thresholds"`

	// SustainedDriftDuration is how long drift must persist before escalation.
	// Default: 30 minutes.
	SustainedDriftDuration time.Duration `json:"sustained_drift_duration"`

	// EscalationRollbackDuration is how long sustained drift must persist before
	// auto-rollback escalation. Default: 2 hours.
	EscalationRollbackDuration time.Duration `json:"escalation_rollback_duration"`

	// MaxDailySummaries limits how many daily summaries are retained.
	// Default: 14 (2 weeks).
	MaxDailySummaries int `json:"max_daily_summaries"`

	// MaxRecentSamples limits the short-window sample buffer.
	// Default: 288 (24h at 5-minute intervals).
	MaxRecentSamples int `json:"max_recent_samples"`

	// MaxEvents limits the stored event log.
	// Default: 1000.
	MaxEvents int `json:"max_events"`
}

// DefaultSurveillanceConfig returns spec-compliant surveillance settings.
func DefaultSurveillanceConfig() SurveillanceConfig {
	return SurveillanceConfig{
		CheckInterval:              5 * time.Minute,
		LongWindowDays:             7,
		Sensitivity:                DefaultSensitivity(),
		LongRunningThresholds:      DefaultLongRunningThresholds(),
		SustainedDriftDuration:     30 * time.Minute,
		EscalationRollbackDuration: 2 * time.Hour,
		MaxDailySummaries:          14,
		MaxRecentSamples:           288,
		MaxEvents:                  1000,
	}
}

// LongRunningThresholds defines what constitutes gradual degradation over the
// long observation window (spec: 7-day windows).
type LongRunningThresholds struct {
	// SuccessRateDeclinePct: if success rate declined by this % or more over
	// the window, it's degraded. Spec implies gradual degradation detection.
	// Default: 5.0.
	SuccessRateDeclinePct float64 `json:"success_rate_decline_pct"`

	// LatencyIncreasePct: if P99 latency increased by this % or more.
	// Default: 20.0.
	LatencyIncreasePct float64 `json:"latency_increase_pct"`

	// ErrorRateIncreasePct: if error rate increased by this % or more.
	// Default: 50.0.
	ErrorRateIncreasePct float64 `json:"error_rate_increase_pct"`

	// MemoryGrowthPct: if memory grew by this % or more.
	// Default: 15.0.
	MemoryGrowthPct float64 `json:"memory_growth_pct"`
}

// DefaultLongRunningThresholds returns spec-compliant degradation thresholds.
func DefaultLongRunningThresholds() LongRunningThresholds {
	return LongRunningThresholds{
		SuccessRateDeclinePct: 5.0,
		LatencyIncreasePct:    20.0,
		ErrorRateIncreasePct:  50.0,
		MemoryGrowthPct:       15.0,
	}
}

// ============================================================================
// agentWindow — per-agent rolling metrics storage
// ============================================================================

// agentWindow holds rolling metrics for a single agent across time scales.
type agentWindow struct {
	agentID  string
	baseline MetricsSnapshot
	// Recent samples for short-window drift analysis.
	recentSamples []timestampedSample
	// Daily summaries for long-window degradation analysis.
	dailySummaries []DailySummary
	// Per-agent drift detector.
	drift *DriftDetector
}

// addSample adds a metric sample to the agent's window.
func (w *agentWindow) addSample(snapshot MetricsSnapshot, maxSamples int) {
	sample := timestampedSample{snapshot: snapshot, time: snapshot.Timestamp}
	if sample.time.IsZero() {
		sample.time = time.Now().UTC()
	}
	w.recentSamples = append(w.recentSamples, sample)
	if len(w.recentSamples) > maxSamples {
		w.recentSamples = w.recentSamples[len(w.recentSamples)-maxSamples:]
	}
	if w.drift != nil {
		w.drift.RecordSampleAt(snapshot, sample.time)
	}
}

// summarizeDay aggregates samples from a given day into a DailySummary.
func (w *agentWindow) summarizeDay(day time.Time) DailySummary {
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	dayEnd := dayStart.Add(24 * time.Hour)

	var sum DailySummary
	sum.Date = dayStart
	var totalSuccess, totalP99, totalP50 float64
	var count int

	for _, s := range w.recentSamples {
		if !s.time.Before(dayStart) && s.time.Before(dayEnd) {
			totalSuccess += s.snapshot.SuccessRate
			totalP99 += s.snapshot.P99LatencyMs
			totalP50 += s.snapshot.P50LatencyMs
			sum.TotalErrors += s.snapshot.ErrorCount
			sum.TotalRequests += s.snapshot.RequestCount
			count++
		}
	}

	if count > 0 {
		sum.AvgSuccessRate = totalSuccess / float64(count)
		sum.AvgP99Latency = totalP99 / float64(count)
		sum.AvgP50Latency = totalP50 / float64(count)
	}
	sum.SampleCount = count
	return sum
}

// rollupDailySummaries aggregates recent samples into daily summaries,
// only creating summaries for days not already summarized.
func (w *agentWindow) rollupDailySummaries(maxSummaries int) {
	if len(w.recentSamples) == 0 {
		return
	}

	// Find the set of days we have samples for.
	seen := make(map[string]bool)
	for _, s := range w.recentSamples {
		day := s.time.Format("2006-01-02")
		seen[day] = true
	}

	// Check which days we've already summarized.
	summarizedDays := make(map[string]bool)
	for _, ds := range w.dailySummaries {
		day := ds.Date.Format("2006-01-02")
		summarizedDays[day] = true
	}

	// Create new daily summaries for unsummarized days.
	for day := range seen {
		if summarizedDays[day] {
			continue
		}
		dayTime, err := time.Parse("2006-01-02", day)
		if err != nil {
			continue
		}
		w.dailySummaries = append(w.dailySummaries, w.summarizeDay(dayTime))
	}

	// Sort and trim.
	if len(w.dailySummaries) > maxSummaries {
		w.dailySummaries = w.dailySummaries[len(w.dailySummaries)-maxSummaries:]
	}
}

// AgentStatus returns the current surveillance status for an agent.
type AgentStatus struct {
	AgentID           string             `json:"agent_id"`
	Status            SurveillanceStatus `json:"status"`
	EscalationLevel   EscalationLevel    `json:"escalation_level"`
	LastEvent         *SurveillanceEvent `json:"last_event,omitempty"`
	SurveillanceStart time.Time          `json:"surveillance_start"`
	DailySummaryCount int                `json:"daily_summary_count"`
	RecentSampleCount int                `json:"recent_sample_count"`
}
