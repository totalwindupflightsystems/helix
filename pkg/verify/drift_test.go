package verify

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Test helpers
// =============================================================================

func goodBaseline() MetricsSnapshot {
	return MetricsSnapshot{
		SuccessRate:     0.99,
		P99LatencyMs:    100,
		P50LatencyMs:    50,
		ErrorCount:      10,
		NewErrorTypes:   0,
		MemoryGrowthPct: 2.0,
		RequestCount:    10000,
		Timestamp:       time.Now().Add(-10 * time.Minute).UTC(),
	}
}

func degradedSnapshot() MetricsSnapshot {
	return MetricsSnapshot{
		SuccessRate:     0.90, // 9% drop → exceeds 2% threshold
		P99LatencyMs:    150,  // 50% increase → exceeds 10% threshold
		P50LatencyMs:    70,   // 40% increase → exceeds 15% threshold
		ErrorCount:      30,   // 200% increase → exceeds 50% threshold
		NewErrorTypes:   3,    // any new type triggers
		MemoryGrowthPct: 15.0, // exceeds 10% threshold
		RequestCount:    10000,
		Timestamp:       time.Now().UTC(),
	}
}

func healthySnapshot() MetricsSnapshot {
	return MetricsSnapshot{
		SuccessRate:     0.995, // slight improvement
		P99LatencyMs:    98,    // slight improvement
		P50LatencyMs:    49,    // slight improvement
		ErrorCount:      9,     // slight improvement
		NewErrorTypes:   0,
		MemoryGrowthPct: 2.0,   // same as baseline
		RequestCount:    10500,
		Timestamp:       time.Now().UTC(),
	}
}

// =============================================================================
// DefaultSensitivity
// =============================================================================

func TestDefaultSensitivity(t *testing.T) {
	s := DefaultSensitivity()
	assert.Equal(t, 2.0, s.SuccessRateDropPct)
	assert.Equal(t, 10.0, s.P99LatencyIncPct)
	assert.Equal(t, 15.0, s.P50LatencyIncPct)
	assert.Equal(t, 50.0, s.ErrorCountIncPct)
	assert.Equal(t, 10.0, s.MemoryGrowthPct)
	assert.Equal(t, 0, s.NewErrorTypesMax)
}

// =============================================================================
// NewDriftDetector
// =============================================================================

func TestNewDriftDetector_Defaults(t *testing.T) {
	d := NewDriftDetector(goodBaseline())
	assert.Equal(t, 5*time.Minute, d.windowSize)
	assert.Equal(t, 60, d.maxSamples)
	assert.Equal(t, 2.0, d.sensitivity.SuccessRateDropPct)
}

func TestNewDriftDetector_CustomSensitivity(t *testing.T) {
	custom := DriftSensitivity{SuccessRateDropPct: 5.0, P99LatencyIncPct: 20.0}
	d := NewDriftDetector(goodBaseline(), WithSensitivity(custom))
	assert.Equal(t, 5.0, d.sensitivity.SuccessRateDropPct)
	assert.Equal(t, 20.0, d.sensitivity.P99LatencyIncPct)
}

func TestNewDriftDetector_CustomWindow(t *testing.T) {
	d := NewDriftDetector(goodBaseline(), WithWindowSize(10*time.Minute))
	assert.Equal(t, 10*time.Minute, d.windowSize)
}

func TestNewDriftDetector_CustomMaxSamples(t *testing.T) {
	d := NewDriftDetector(goodBaseline(), WithMaxSamples(100))
	assert.Equal(t, 100, d.maxSamples)
}

// =============================================================================
// RecordSample
// =============================================================================

func TestRecordSample_AddsToWindow(t *testing.T) {
	d := NewDriftDetector(goodBaseline())
	d.RecordSample(healthySnapshot())
	assert.Equal(t, 1, d.SampleCount())
}

func TestRecordSample_MultipleSamples(t *testing.T) {
	d := NewDriftDetector(goodBaseline())
	for i := 0; i < 5; i++ {
		d.RecordSample(healthySnapshot())
	}
	assert.Equal(t, 5, d.SampleCount())
}

func TestRecordSample_EvictsOldSamples(t *testing.T) {
	d := NewDriftDetector(goodBaseline(), WithWindowSize(1*time.Second))

	// Add old sample
	old := healthySnapshot()
	d.RecordSampleAt(old, time.Now().Add(-2*time.Second))

	// Add new sample
	d.RecordSample(healthySnapshot())

	// Old sample should be evicted
	assert.Equal(t, 1, d.SampleCount())
}

func TestRecordSample_MaxSamplesCap(t *testing.T) {
	d := NewDriftDetector(goodBaseline(), WithMaxSamples(3))
	for i := 0; i < 5; i++ {
		d.RecordSample(healthySnapshot())
	}
	assert.Equal(t, 3, d.SampleCount(), "should be capped at maxSamples")
}

// =============================================================================
// AverageInWindow
// =============================================================================

func TestAverageInWindow_NoSamples(t *testing.T) {
	base := goodBaseline()
	d := NewDriftDetector(base)
	avg := d.AverageInWindow()
	// Should return baseline when no samples
	assert.Equal(t, base.SuccessRate, avg.SuccessRate)
}

func TestAverageInWindow_SingleSample(t *testing.T) {
	d := NewDriftDetector(goodBaseline())
	d.RecordSample(healthySnapshot())
	avg := d.AverageInWindow()
	assert.InDelta(t, 0.995, avg.SuccessRate, 0.001)
}

func TestAverageInWindow_MultipleSamples(t *testing.T) {
	d := NewDriftDetector(goodBaseline())
	d.RecordSample(MetricsSnapshot{SuccessRate: 0.98, P99LatencyMs: 110})
	d.RecordSample(MetricsSnapshot{SuccessRate: 0.99, P99LatencyMs: 100})
	d.RecordSample(MetricsSnapshot{SuccessRate: 1.00, P99LatencyMs: 90})

	avg := d.AverageInWindow()
	assert.InDelta(t, 0.99, avg.SuccessRate, 0.001)
	assert.InDelta(t, 100.0, avg.P99LatencyMs, 0.1)
}

// =============================================================================
// Assess — degraded metrics
// =============================================================================

func TestAssess_NoBreach(t *testing.T) {
	d := NewDriftDetector(goodBaseline())
	d.RecordSample(healthySnapshot())
	assessment := d.Assess()
	assert.False(t, assessment.AnyBreach)
	assert.Equal(t, 0, assessment.CriticalCount)
}

func TestAssess_DegradedAllBreach(t *testing.T) {
	d := NewDriftDetector(goodBaseline())
	d.RecordSample(degradedSnapshot())
	assessment := d.Assess()

	assert.True(t, assessment.AnyBreach)
	assert.Greater(t, assessment.CriticalCount, 0)
	assert.Greater(t, len(assessment.MetricReports), 0)
}

func TestAssess_SuccessRateDrop(t *testing.T) {
	d := NewDriftDetector(goodBaseline())
	d.RecordSample(MetricsSnapshot{
		SuccessRate: 0.95, // ~4% drop from 0.99 → exceeds 2% threshold
		P99LatencyMs: 100,
		P50LatencyMs: 50,
		ErrorCount:   10,
		Timestamp:    time.Now().UTC(),
	})
	assessment := d.Assess()

	var srReport *MetricDriftReport
	for i := range assessment.MetricReports {
		if assessment.MetricReports[i].Metric == "success_rate" {
			srReport = &assessment.MetricReports[i]
		}
	}
	require.NotNil(t, srReport)
	assert.True(t, srReport.Exceeds)
	assert.Equal(t, TrendDegrading, srReport.Direction)
}

func TestAssess_LatencyIncrease(t *testing.T) {
	d := NewDriftDetector(goodBaseline())
	d.RecordSample(MetricsSnapshot{
		SuccessRate:  0.99,
		P99LatencyMs: 130, // 30% increase from 100 → exceeds 10%
		P50LatencyMs: 50,
		ErrorCount:   10,
		Timestamp:    time.Now().UTC(),
	})
	assessment := d.Assess()

	var latReport *MetricDriftReport
	for i := range assessment.MetricReports {
		if assessment.MetricReports[i].Metric == "p99_latency_ms" {
			latReport = &assessment.MetricReports[i]
		}
	}
	require.NotNil(t, latReport)
	assert.True(t, latReport.Exceeds)
	assert.Equal(t, TrendDegrading, latReport.Direction)
}

func TestAssess_ErrorCountIncrease(t *testing.T) {
	d := NewDriftDetector(goodBaseline())
	d.RecordSample(MetricsSnapshot{
		SuccessRate:  0.99,
		P99LatencyMs: 100,
		P50LatencyMs: 50,
		ErrorCount:   25, // 150% increase from 10 → exceeds 50%
		Timestamp:    time.Now().UTC(),
	})
	assessment := d.Assess()

	var errReport *MetricDriftReport
	for i := range assessment.MetricReports {
		if assessment.MetricReports[i].Metric == "error_count" {
			errReport = &assessment.MetricReports[i]
		}
	}
	require.NotNil(t, errReport)
	assert.True(t, errReport.Exceeds)
}

func TestAssess_NewErrorTypes(t *testing.T) {
	d := NewDriftDetector(goodBaseline())
	d.RecordSample(MetricsSnapshot{
		SuccessRate:   0.99,
		P99LatencyMs:  100,
		P50LatencyMs:  50,
		ErrorCount:    10,
		NewErrorTypes: 2, // any new type is bad
		Timestamp:     time.Now().UTC(),
	})
	assessment := d.Assess()

	var typesReport *MetricDriftReport
	for i := range assessment.MetricReports {
		if assessment.MetricReports[i].Metric == "new_error_types" {
			typesReport = &assessment.MetricReports[i]
		}
	}
	require.NotNil(t, typesReport)
	assert.True(t, typesReport.Exceeds)
}

func TestAssess_MemoryGrowth(t *testing.T) {
	d := NewDriftDetector(goodBaseline())
	d.RecordSample(MetricsSnapshot{
		SuccessRate:     0.99,
		P99LatencyMs:    100,
		P50LatencyMs:    50,
		ErrorCount:      10,
		MemoryGrowthPct: 15.0, // exceeds 10% threshold
		Timestamp:       time.Now().UTC(),
	})
	assessment := d.Assess()

	var memReport *MetricDriftReport
	for i := range assessment.MetricReports {
		if assessment.MetricReports[i].Metric == "memory_growth_pct" {
			memReport = &assessment.MetricReports[i]
		}
	}
	require.NotNil(t, memReport)
	assert.True(t, memReport.Exceeds)
}

// =============================================================================
// Severity classification
// =============================================================================

func TestSeverity_CriticalForLargeDrift(t *testing.T) {
	d := NewDriftDetector(goodBaseline())
	d.RecordSample(MetricsSnapshot{
		SuccessRate:  0.80, // ~19% drop → way beyond 2% threshold
		P99LatencyMs: 100,
		P50LatencyMs: 50,
		ErrorCount:   10,
		Timestamp:    time.Now().UTC(),
	})
	assessment := d.Assess()
	assert.Greater(t, assessment.CriticalCount, 0)
}

func TestSeverity_WarningForSmallDrift(t *testing.T) {
	d := NewDriftDetector(goodBaseline())
	d.RecordSample(MetricsSnapshot{
		SuccessRate:  0.97, // ~2% drop → just beyond threshold
		P99LatencyMs: 100,
		P50LatencyMs: 50,
		ErrorCount:   10,
		Timestamp:    time.Now().UTC(),
	})
	assessment := d.Assess()
	assert.GreaterOrEqual(t, assessment.WarningCount, 1)
}

// =============================================================================
// ShouldRollback
// =============================================================================

func TestShouldRollback_CriticalBreach(t *testing.T) {
	d := NewDriftDetector(goodBaseline())
	d.RecordSample(degradedSnapshot())
	assessment := d.Assess()
	assert.True(t, assessment.ShouldRollback())
}

func TestShouldRollback_NoBreach(t *testing.T) {
	d := NewDriftDetector(goodBaseline())
	d.RecordSample(healthySnapshot())
	assessment := d.Assess()
	assert.False(t, assessment.ShouldRollback())
}

func TestShouldRollback_OnlyWarnings(t *testing.T) {
	d := NewDriftDetector(goodBaseline())
	d.RecordSample(MetricsSnapshot{
		SuccessRate:  0.97, // ~2% drop → warning
		P99LatencyMs: 105,  // 5% increase → within threshold
		P50LatencyMs: 50,
		ErrorCount:   10,
		Timestamp:    time.Now().UTC(),
	})
	assessment := d.Assess()
	// Only warnings → no rollback
	assert.False(t, assessment.ShouldRollback())
}

// =============================================================================
// AssessLatest
// =============================================================================

func TestAssessLatest(t *testing.T) {
	d := NewDriftDetector(goodBaseline())
	d.RecordSample(healthySnapshot())
	d.RecordSample(degradedSnapshot())

	assessment := d.AssessLatest()
	assert.True(t, assessment.AnyBreach)
}

func TestAssessLatest_NoSamples(t *testing.T) {
	d := NewDriftDetector(goodBaseline())
	assessment := d.AssessLatest()
	assert.False(t, assessment.AnyBreach)
}

// =============================================================================
// DriftAssessment helpers
// =============================================================================

func TestCriticalBreaches(t *testing.T) {
	d := NewDriftDetector(goodBaseline())
	d.RecordSample(degradedSnapshot())
	assessment := d.Assess()
	critical := assessment.CriticalBreaches()
	assert.NotEmpty(t, critical)
}

func TestAssessmentSummary(t *testing.T) {
	d := NewDriftDetector(goodBaseline())
	d.RecordSample(degradedSnapshot())
	assessment := d.Assess()
	s := assessment.Summary()
	assert.Contains(t, s, "critical")
}

// =============================================================================
// AssessDeployment
// =============================================================================

func TestAssessDeployment_NilDeployment(t *testing.T) {
	assessment := AssessDeployment(nil, DefaultSensitivity())
	assert.False(t, assessment.AnyBreach)
}

func TestAssessDeployment_Healthy(t *testing.T) {
	dep := &ShadowDeployment{
		AgentID:  "agent-001",
		Baseline: goodBaseline(),
		Shadow:   healthySnapshot(),
	}
	assessment := AssessDeployment(dep, DefaultSensitivity())
	assert.False(t, assessment.AnyBreach)
}

func TestAssessDeployment_Degraded(t *testing.T) {
	dep := &ShadowDeployment{
		AgentID:  "agent-001",
		Baseline: goodBaseline(),
		Shadow:   degradedSnapshot(),
	}
	assessment := AssessDeployment(dep, DefaultSensitivity())
	assert.True(t, assessment.AnyBreach)
	assert.True(t, assessment.ShouldRollback())
}

// =============================================================================
// Helpers
// =============================================================================

func TestPctChange(t *testing.T) {
	assert.InDelta(t, 50.0, pctChange(100, 150), 0.01)
	assert.InDelta(t, -50.0, pctChange(100, 50), 0.01)
	assert.InDelta(t, 0.0, pctChange(100, 100), 0.01)
}

func TestPctChange_ZeroBase(t *testing.T) {
	assert.Equal(t, 100.0, pctChange(0, 50))
	assert.Equal(t, 0.0, pctChange(0, 0))
}

func TestClassifyTrend_Stable(t *testing.T) {
	assert.Equal(t, TrendStable, classifyTrend(0, "higher_is_better"))
	assert.Equal(t, TrendStable, classifyTrend(0.00001, "higher_is_better"))
}

func TestClassifyTrend_Improving(t *testing.T) {
	assert.Equal(t, TrendImproving, classifyTrend(0.1, "higher_is_better"))
	assert.Equal(t, TrendImproving, classifyTrend(-0.1, "lower_is_better"))
}

func TestClassifyTrend_Degrading(t *testing.T) {
	assert.Equal(t, TrendDegrading, classifyTrend(-0.1, "higher_is_better"))
	assert.Equal(t, TrendDegrading, classifyTrend(0.1, "lower_is_better"))
}

func TestClassifySeverity_NoneWhenNotExceeding(t *testing.T) {
	assert.Equal(t, SeverityNone, classifySeverity(false, 5.0, 10.0, "lower_is_better"))
}

func TestClassifySeverity_Warning(t *testing.T) {
	// driftPct 15, threshold 10 → overshoot = 5, less than threshold → warning
	assert.Equal(t, SeverityWarning, classifySeverity(true, 15.0, 10.0, "lower_is_better"))
}

func TestClassifySeverity_Critical(t *testing.T) {
	// driftPct 30, threshold 10 → overshoot = 20, more than threshold → critical
	assert.Equal(t, SeverityCritical, classifySeverity(true, 30.0, 10.0, "lower_is_better"))
}
