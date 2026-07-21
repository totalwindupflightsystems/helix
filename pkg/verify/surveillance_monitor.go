package verify

import (
	"math"
	"sync"
)

// ============================================================================
// LongRunningMonitor — Gradual Degradation Detection
// ============================================================================

// LongRunningMonitor detects gradual degradation over 7-day windows by
// analyzing daily summaries. It compares the first and last day in the window
// and flags metrics that have degraded beyond configured thresholds.
type LongRunningMonitor struct {
	mu         sync.RWMutex
	windowDays int
	thresholds LongRunningThresholds
	// Per-agent degradation reports (most recent).
	reports map[string]*DegradationReport
}

// NewLongRunningMonitor creates a monitor with the given window size and thresholds.
func NewLongRunningMonitor(windowDays int, thresholds LongRunningThresholds) *LongRunningMonitor {
	if windowDays <= 0 {
		windowDays = 7
	}
	return &LongRunningMonitor{
		windowDays: windowDays,
		thresholds: thresholds,
		reports:    make(map[string]*DegradationReport),
	}
}

// Analyze evaluates daily summaries for gradual degradation.
// Returns a DegradationReport if degradation is detected, or nil if the
// agent is stable or there's insufficient data (fewer than 2 daily summaries).
func (m *LongRunningMonitor) Analyze(agentID string, summaries []DailySummary) *DegradationReport {
	if len(summaries) < 2 {
		return nil
	}

	// Use the most recent N daily summaries (up to windowDays).
	start := 0
	if len(summaries) > m.windowDays {
		start = len(summaries) - m.windowDays
	}
	window := summaries[start:]

	first := window[0]
	last := window[len(window)-1]

	report := &DegradationReport{
		AgentID:     agentID,
		WindowStart: first.Date,
		WindowEnd:   last.Date,
		WindowDays:  len(window),
		Severity:    SeverityNone,
	}

	t := m.thresholds

	// Success rate decline.
	if first.AvgSuccessRate > 0 {
		change := pctChange(first.AvgSuccessRate, last.AvgSuccessRate)
		exceeds := change < -t.SuccessRateDeclinePct // negative change = decline
		report.MetricTrends = append(report.MetricTrends, MetricDegradation{
			Metric:       "success_rate",
			FirstDay:     first.AvgSuccessRate,
			LastDay:      last.AvgSuccessRate,
			ChangePct:    change,
			ThresholdPct: t.SuccessRateDeclinePct,
			Exceeds:      exceeds,
			Direction:    degradationDirection(change, "higher_is_better"),
		})
	}

	// P99 latency increase.
	if first.AvgP99Latency > 0 {
		change := pctChange(first.AvgP99Latency, last.AvgP99Latency)
		exceeds := change > t.LatencyIncreasePct
		report.MetricTrends = append(report.MetricTrends, MetricDegradation{
			Metric:       "p99_latency_ms",
			FirstDay:     first.AvgP99Latency,
			LastDay:      last.AvgP99Latency,
			ChangePct:    change,
			ThresholdPct: t.LatencyIncreasePct,
			Exceeds:      exceeds,
			Direction:    degradationDirection(change, "lower_is_better"),
		})
	}

	// Error rate increase.
	firstErrRate := errorRate(first)
	lastErrRate := errorRate(last)
	if firstErrRate > 0 {
		change := pctChange(firstErrRate, lastErrRate)
		exceeds := change > t.ErrorRateIncreasePct
		report.MetricTrends = append(report.MetricTrends, MetricDegradation{
			Metric:       "error_rate",
			FirstDay:     firstErrRate,
			LastDay:      lastErrRate,
			ChangePct:    change,
			ThresholdPct: t.ErrorRateIncreasePct,
			Exceeds:      exceeds,
			Direction:    degradationDirection(change, "lower_is_better"),
		})
	} else if lastErrRate > 0 {
		// Went from 0 errors to some errors.
		report.MetricTrends = append(report.MetricTrends, MetricDegradation{
			Metric:       "error_rate",
			FirstDay:     0,
			LastDay:      lastErrRate,
			ChangePct:    100.0,
			ThresholdPct: t.ErrorRateIncreasePct,
			Exceeds:      true,
			Direction:    DegradationWorsening,
		})
	}

	// Aggregate severity.
	for _, trend := range report.MetricTrends {
		if trend.Exceeds {
			report.IsDegrading = true
			report.AffectedMetrics++
		}
	}

	// Severity based on number of affected metrics.
	if report.AffectedMetrics >= 3 {
		report.Severity = SeverityCritical
	} else if report.AffectedMetrics >= 2 {
		report.Severity = SeverityWarning
	} else if report.AffectedMetrics >= 1 {
		report.Severity = SeverityWarning
	}

	// Store report.
	m.mu.Lock()
	m.reports[agentID] = report
	m.mu.Unlock()

	return report
}

// GetLastReport returns the most recent degradation report for an agent.
func (m *LongRunningMonitor) GetLastReport(agentID string) *DegradationReport {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.reports[agentID]
}

// errorRate computes the error rate from a daily summary.
func errorRate(s DailySummary) float64 {
	if s.TotalRequests == 0 {
		return 0
	}
	return float64(s.TotalErrors) / float64(s.TotalRequests)
}

// degradationDirection classifies a change as worsening or improving.
func degradationDirection(changePct float64, directionHint string) DegradationDirection {
	const epsilon = 0.001
	if math.Abs(changePct) < epsilon {
		return DegradationNone
	}
	if directionHint == "higher_is_better" {
		if changePct > 0 {
			return DegradationImproving
		}
		return DegradationWorsening
	}
	// lower_is_better
	if changePct > 0 {
		return DegradationWorsening
	}
	return DegradationImproving
}
