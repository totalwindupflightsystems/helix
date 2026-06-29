package verify

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// =============================================================================
// DriftDetector — Time-Windowed Metric Drift Detection
//
// Per spec §Shadow Verification + §Behavior Contracts:
//
//   Detect metric drift (success rate drop >2%, latency increase >10%,
//   error type distribution shift). Configurable sensitivity thresholds
//   per metric. Time-windowed comparison (rolling 5-min windows).
//
// The DriftDetector maintains rolling time windows of MetricsSnapshots,
// compares current metrics against the baseline, and produces detailed
// DriftReports with trend direction and breach severity. It integrates
// with the existing ShadowDeployment lifecycle.
// =============================================================================

// DriftSensitivity defines per-metric drift thresholds.
// Each value is the maximum allowed percentage change before drift is flagged.
type DriftSensitivity struct {
	SuccessRateDropPct float64 // default 2.0 — success rate drops are critical
	P99LatencyIncPct   float64 // default 10.0
	P50LatencyIncPct   float64 // default 15.0
	ErrorCountIncPct   float64 // default 50.0 — some error fluctuation is normal
	MemoryGrowthPct    float64 // default 10.0 — memory leaks are insidious
	NewErrorTypesMax   int     // default 0 — any new error type is a red flag
}

// DefaultSensitivity returns spec-compliant drift thresholds.
func DefaultSensitivity() DriftSensitivity {
	return DriftSensitivity{
		SuccessRateDropPct: 2.0,
		P99LatencyIncPct:   10.0,
		P50LatencyIncPct:   15.0,
		ErrorCountIncPct:   50.0,
		MemoryGrowthPct:    10.0,
		NewErrorTypesMax:   0,
	}
}

// TrendDirection indicates whether a metric is improving or degrading.
type TrendDirection string

const (
	TrendStable    TrendDirection = "stable"
	TrendImproving TrendDirection = "improving"
	TrendDegrading TrendDirection = "degrading"
)

// BreachSeverity classifies the severity of a drift breach.
type BreachSeverity string

const (
	SeverityNone     BreachSeverity = "none"
	SeverityWarning  BreachSeverity = "warning"
	SeverityCritical BreachSeverity = "critical"
)

// MetricDriftReport is a per-metric drift assessment with trend and severity.
type MetricDriftReport struct {
	Metric       string         `json:"metric"`
	Baseline     float64        `json:"baseline"`
	Current      float64        `json:"current"`
	Delta        float64        `json:"delta"`
	DriftPct     float64        `json:"drift_pct"`
	ThresholdPct float64        `json:"threshold_pct"`
	Exceeds      bool           `json:"exceeds"`
	Direction    TrendDirection `json:"direction"`
	Severity     BreachSeverity `json:"severity"`
}

// DriftAssessment aggregates per-metric drift reports for a single comparison.
type DriftAssessment struct {
	Timestamp     time.Time           `json:"timestamp"`
	WindowStart   time.Time           `json:"window_start"`
	WindowEnd     time.Time           `json:"window_end"`
	MetricReports []MetricDriftReport `json:"metric_reports"`
	AnyBreach     bool                `json:"any_breach"`
	CriticalCount int                 `json:"critical_count"`
	WarningCount  int                 `json:"warning_count"`
}

// Summary returns a one-line assessment summary.
func (a *DriftAssessment) Summary() string {
	return fmt.Sprintf("%d metrics assessed, %d critical, %d warning, breach=%v",
		len(a.MetricReports), a.CriticalCount, a.WarningCount, a.AnyBreach)
}

// CriticalBreaches returns only the critical-severity drift reports.
func (a *DriftAssessment) CriticalBreaches() []MetricDriftReport {
	var critical []MetricDriftReport
	for _, r := range a.MetricReports {
		if r.Severity == SeverityCritical {
			critical = append(critical, r)
		}
	}
	return critical
}

// ShouldRollback returns true if any critical breach is detected.
// Warnings alone don't trigger rollback — they're for human review.
func (a *DriftAssessment) ShouldRollback() bool {
	return a.CriticalCount > 0
}

// =============================================================================
// DriftDetector
// =============================================================================

// DriftDetector maintains rolling time windows of metrics and detects drift
// against a baseline. It's designed to be called periodically (e.g., every
// 5 minutes) with fresh MetricsSnapshots from the shadow deployment.
type DriftDetector struct {
	mu          sync.RWMutex
	baseline    MetricsSnapshot
	sensitivity DriftSensitivity
	windowSize  time.Duration
	samples     []timestampedSample
	maxSamples  int // max samples to retain in the rolling window
}

// timestampedSample pairs a MetricsSnapshot with its collection time.
type timestampedSample struct {
	snapshot MetricsSnapshot
	time     time.Time
}

// DriftDetectorOption configures a DriftDetector.
type DriftDetectorOption func(*DriftDetector)

// WithSensitivity sets custom drift thresholds.
func WithSensitivity(s DriftSensitivity) DriftDetectorOption {
	return func(d *DriftDetector) { d.sensitivity = s }
}

// WithWindowSize sets the rolling window duration (default 5m).
func WithWindowSize(d time.Duration) DriftDetectorOption {
	return func(det *DriftDetector) { det.windowSize = d }
}

// WithMaxSamples sets the maximum number of samples to retain.
func WithMaxSamples(n int) DriftDetectorOption {
	return func(d *DriftDetector) { d.maxSamples = n }
}

// NewDriftDetector creates a detector with the given baseline and defaults.
func NewDriftDetector(baseline MetricsSnapshot, opts ...DriftDetectorOption) *DriftDetector {
	d := &DriftDetector{
		baseline:    baseline,
		sensitivity: DefaultSensitivity(),
		windowSize:  5 * time.Minute,
		samples:     make([]timestampedSample, 0, 60),
		maxSamples:  60,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// RecordSample adds a metrics snapshot to the rolling window.
// Samples older than the window size are automatically evicted.
func (d *DriftDetector) RecordSample(snapshot MetricsSnapshot) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now().UTC()
	d.samples = append(d.samples, timestampedSample{
		snapshot: snapshot,
		time:     now,
	})

	// Evict old samples beyond the window.
	cutoff := now.Add(-d.windowSize)
	idx := 0
	for idx < len(d.samples) && d.samples[idx].time.Before(cutoff) {
		idx++
	}
	if idx > 0 {
		d.samples = d.samples[idx:]
	}

	// Enforce maxSamples cap.
	if len(d.samples) > d.maxSamples {
		d.samples = d.samples[len(d.samples)-d.maxSamples:]
	}
}

// RecordSampleAt adds a sample with a specific timestamp (for testing).
func (d *DriftDetector) RecordSampleAt(snapshot MetricsSnapshot, at time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.samples = append(d.samples, timestampedSample{
		snapshot: snapshot,
		time:     at,
	})
	if len(d.samples) > d.maxSamples {
		d.samples = d.samples[len(d.samples)-d.maxSamples:]
	}
}

// SampleCount returns the number of samples currently in the window.
func (d *DriftDetector) SampleCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.samples)
}

// AverageInWindow computes the average of each metric across samples
// in the current rolling window. Returns a MetricsSnapshot with averaged values.
func (d *DriftDetector) AverageInWindow() MetricsSnapshot {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.samples) == 0 {
		return d.baseline
	}

	n := float64(len(d.samples))
	var avg MetricsSnapshot
	avg.Timestamp = time.Now().UTC()

	for _, s := range d.samples {
		snap := s.snapshot
		avg.SuccessRate += snap.SuccessRate
		avg.P99LatencyMs += snap.P99LatencyMs
		avg.P50LatencyMs += snap.P50LatencyMs
		avg.ErrorCount += snap.ErrorCount
		avg.NewErrorTypes += snap.NewErrorTypes
		avg.MemoryGrowthPct += snap.MemoryGrowthPct
		avg.RequestCount += snap.RequestCount
	}

	avg.SuccessRate /= n
	avg.P99LatencyMs /= n
	avg.P50LatencyMs /= n
	// ErrorCount and RequestCount are summed (total), not averaged
	// But for drift comparison we want average rate
	avg.ErrorCount = int64(float64(avg.ErrorCount) / n)
	avg.RequestCount = int64(float64(avg.RequestCount) / n)
	avg.NewErrorTypes = int(math.Round(float64(avg.NewErrorTypes) / n))
	avg.MemoryGrowthPct /= n

	return avg
}

// Assess compares current metrics against the baseline using configured
// sensitivity thresholds. Returns a detailed DriftAssessment.
func (d *DriftDetector) Assess() *DriftAssessment {
	current := d.AverageInWindow()
	return d.assessAgainst(current)
}

// AssessLatest compares the most recent sample against the baseline.
// This is useful for immediate alerting without waiting for window averaging.
func (d *DriftDetector) AssessLatest() *DriftAssessment {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.samples) == 0 {
		return &DriftAssessment{
			MetricReports: []MetricDriftReport{},
		}
	}
	latest := d.samples[len(d.samples)-1].snapshot
	// We need to call assessAgainst which itself locks, so unlock first
	// Actually we can compute inline since we're already locked.
	return d.assessAgainstLocked(latest)
}

// assessAgainst is the unlock-safe version.
func (d *DriftDetector) assessAgainst(current MetricsSnapshot) *DriftAssessment {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.assessAgainstLocked(current)
}

// assessAgainstLocked assumes the read lock is already held.
func (d *DriftDetector) assessAgainstLocked(current MetricsSnapshot) *DriftAssessment {
	base := d.baseline
	s := d.sensitivity

	assessment := &DriftAssessment{
		Timestamp:     time.Now().UTC(),
		WindowStart:   base.Timestamp,
		WindowEnd:     current.Timestamp,
		MetricReports: []MetricDriftReport{},
	}

	// Success rate — drops are bad (inverse direction)
	if base.SuccessRate > 0 {
		delta := current.SuccessRate - base.SuccessRate
		driftPct := pctChange(base.SuccessRate, current.SuccessRate)
		exceeds := driftPct < -s.SuccessRateDropPct // negative drift = drop
		report := MetricDriftReport{
			Metric:       "success_rate",
			Baseline:     base.SuccessRate,
			Current:      current.SuccessRate,
			Delta:        delta,
			DriftPct:     driftPct,
			ThresholdPct: s.SuccessRateDropPct,
			Exceeds:      exceeds,
			Direction:    classifyTrend(delta, "higher_is_better"),
			Severity:     classifySeverity(exceeds, driftPct, s.SuccessRateDropPct, "higher_is_better"),
		}
		assessment.MetricReports = append(assessment.MetricReports, report)
	}

	// P99 latency — increases are bad
	if base.P99LatencyMs > 0 {
		delta := current.P99LatencyMs - base.P99LatencyMs
		driftPct := pctChange(base.P99LatencyMs, current.P99LatencyMs)
		exceeds := driftPct > s.P99LatencyIncPct
		report := MetricDriftReport{
			Metric:       "p99_latency_ms",
			Baseline:     base.P99LatencyMs,
			Current:      current.P99LatencyMs,
			Delta:        delta,
			DriftPct:     driftPct,
			ThresholdPct: s.P99LatencyIncPct,
			Exceeds:      exceeds,
			Direction:    classifyTrend(delta, "lower_is_better"),
			Severity:     classifySeverity(exceeds, driftPct, s.P99LatencyIncPct, "lower_is_better"),
		}
		assessment.MetricReports = append(assessment.MetricReports, report)
	}

	// P50 latency — increases are bad
	if base.P50LatencyMs > 0 {
		delta := current.P50LatencyMs - base.P50LatencyMs
		driftPct := pctChange(base.P50LatencyMs, current.P50LatencyMs)
		exceeds := driftPct > s.P50LatencyIncPct
		report := MetricDriftReport{
			Metric:       "p50_latency_ms",
			Baseline:     base.P50LatencyMs,
			Current:      current.P50LatencyMs,
			Delta:        delta,
			DriftPct:     driftPct,
			ThresholdPct: s.P50LatencyIncPct,
			Exceeds:      exceeds,
			Direction:    classifyTrend(delta, "lower_is_better"),
			Severity:     classifySeverity(exceeds, driftPct, s.P50LatencyIncPct, "lower_is_better"),
		}
		assessment.MetricReports = append(assessment.MetricReports, report)
	}

	// Error count — increases are bad
	if base.ErrorCount > 0 {
		delta := float64(current.ErrorCount - base.ErrorCount)
		driftPct := pctChange(float64(base.ErrorCount), float64(current.ErrorCount))
		exceeds := driftPct > s.ErrorCountIncPct
		report := MetricDriftReport{
			Metric:       "error_count",
			Baseline:     float64(base.ErrorCount),
			Current:      float64(current.ErrorCount),
			Delta:        delta,
			DriftPct:     driftPct,
			ThresholdPct: s.ErrorCountIncPct,
			Exceeds:      exceeds,
			Direction:    classifyTrend(delta, "lower_is_better"),
			Severity:     classifySeverity(exceeds, driftPct, s.ErrorCountIncPct, "lower_is_better"),
		}
		assessment.MetricReports = append(assessment.MetricReports, report)
	}

	// New error types — any new type is bad
	newTypes := current.NewErrorTypes
	newTypesExceeds := newTypes > s.NewErrorTypesMax
	if newTypes > 0 {
		report := MetricDriftReport{
			Metric:       "new_error_types",
			Baseline:     0,
			Current:      float64(newTypes),
			Delta:        float64(newTypes),
			DriftPct:     100.0, // any new type = 100% drift
			ThresholdPct: float64(s.NewErrorTypesMax),
			Exceeds:      newTypesExceeds,
			Direction:    TrendDegrading,
			Severity:     SeverityWarning,
		}
		if newTypes >= 3 {
			report.Severity = SeverityCritical
		}
		assessment.MetricReports = append(assessment.MetricReports, report)
	}

	// Memory growth
	if base.MemoryGrowthPct > 0 || current.MemoryGrowthPct > 0 {
		delta := current.MemoryGrowthPct - base.MemoryGrowthPct
		driftPct := pctChangeSafe(base.MemoryGrowthPct, current.MemoryGrowthPct)
		exceeds := current.MemoryGrowthPct > s.MemoryGrowthPct
		report := MetricDriftReport{
			Metric:       "memory_growth_pct",
			Baseline:     base.MemoryGrowthPct,
			Current:      current.MemoryGrowthPct,
			Delta:        delta,
			DriftPct:     driftPct,
			ThresholdPct: s.MemoryGrowthPct,
			Exceeds:      exceeds,
			Direction:    classifyTrend(delta, "lower_is_better"),
			Severity:     classifySeverity(exceeds, driftPct, s.MemoryGrowthPct, "lower_is_better"),
		}
		assessment.MetricReports = append(assessment.MetricReports, report)
	}

	// Aggregate
	for _, r := range assessment.MetricReports {
		if r.Exceeds {
			assessment.AnyBreach = true
		}
		switch r.Severity {
		case SeverityCritical:
			assessment.CriticalCount++
		case SeverityWarning:
			assessment.WarningCount++
		}
	}

	return assessment
}

// =============================================================================
// Integration with ShadowDeployment
// =============================================================================

// AssessDeployment evaluates a ShadowDeployment for drift.
// It uses the deployment's baseline and current shadow metrics.
func AssessDeployment(dep *ShadowDeployment, sensitivity DriftSensitivity) *DriftAssessment {
	if dep == nil {
		return &DriftAssessment{MetricReports: []MetricDriftReport{}}
	}

	dep.mu.RLock()
	baseline := dep.Baseline
	current := dep.Shadow
	dep.mu.RUnlock()

	detector := NewDriftDetector(baseline, WithSensitivity(sensitivity))
	return detector.assessAgainst(current)
}

// =============================================================================
// Helpers
// =============================================================================

// pctChange computes percentage change: ((current - base) / base) * 100
func pctChange(base, current float64) float64 {
	if base == 0 {
		if current == 0 {
			return 0
		}
		return 100.0
	}
	return ((current - base) / base) * 100.0
}

// pctChangeSafe handles zero-division for memory metrics where base can be 0.
func pctChangeSafe(base, current float64) float64 {
	if base == 0 {
		if current == 0 {
			return 0
		}
		return 100.0
	}
	return ((current - base) / base) * 100.0
}

// classifyTrend determines if a metric is improving or degrading.
// directionHint: "higher_is_better" or "lower_is_better".
func classifyTrend(delta float64, directionHint string) TrendDirection {
	const epsilon = 0.0001
	if math.Abs(delta) < epsilon {
		return TrendStable
	}
	if directionHint == "higher_is_better" {
		if delta > 0 {
			return TrendImproving
		}
		return TrendDegrading
	}
	// lower_is_better
	if delta > 0 {
		return TrendDegrading
	}
	return TrendImproving
}

// classifySeverity determines breach severity from the drift amount.
// For "higher_is_better" metrics (success rate), large negative drift is critical.
// For "lower_is_better" metrics (latency, errors), large positive drift is critical.
func classifySeverity(exceeds bool, driftPct, thresholdPct float64, directionHint string) BreachSeverity {
	if !exceeds {
		return SeverityNone
	}

	// Calculate how far beyond threshold
	var overshoot float64
	if directionHint == "higher_is_better" {
		// Negative drift beyond threshold: driftPct < -thresholdPct
		overshoot = math.Abs(driftPct) - thresholdPct
	} else {
		overshoot = driftPct - thresholdPct
	}

	// If drift exceeds 2x the threshold, it's critical
	if overshoot > thresholdPct {
		return SeverityCritical
	}
	return SeverityWarning
}
