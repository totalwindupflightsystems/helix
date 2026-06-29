package verify

import (
	"testing"
	"time"
)

// --- PromotionResult tests ---

func TestPromotionResult_AllPassed(t *testing.T) {
	result := PromotionResult{
		Checks: []PromotionCheck{
			{Name: CheckContractPassed, Passed: true},
			{Name: CheckDriftOK, Passed: true},
			{Name: CheckSuccessRate, Passed: true},
			{Name: CheckNoNewErrors, Passed: true},
			{Name: CheckObsWindow, Passed: true},
		},
	}
	if !result.AllPassed() {
		t.Error("AllPassed should be true when all checks pass")
	}
}

func TestPromotionResult_AllPassed_WithSkipped(t *testing.T) {
	result := PromotionResult{
		Checks: []PromotionCheck{
			{Name: CheckContractPassed, Passed: true},
			{Name: CheckDriftOK, Passed: false, Skipped: true},
			{Name: CheckSuccessRate, Passed: true},
		},
	}
	if !result.AllPassed() {
		t.Error("AllPassed should be true when skipped checks exist but no failures")
	}
}

func TestPromotionResult_AllPassed_WithFailure(t *testing.T) {
	result := PromotionResult{
		Checks: []PromotionCheck{
			{Name: CheckContractPassed, Passed: true},
			{Name: CheckSuccessRate, Passed: false},
			{Name: CheckDriftOK, Passed: false, Skipped: true},
		},
	}
	if result.AllPassed() {
		t.Error("AllPassed should be false when a non-skipped check fails")
	}
}

func TestPromotionResult_HasFailures(t *testing.T) {
	tests := []struct {
		name   string
		result PromotionResult
		want   bool
	}{
		{
			"all pass",
			PromotionResult{Checks: []PromotionCheck{{Passed: true}, {Passed: true}}},
			false,
		},
		{
			"one fails",
			PromotionResult{Checks: []PromotionCheck{{Passed: true}, {Passed: false}}},
			true,
		},
		{
			"only skipped",
			PromotionResult{Checks: []PromotionCheck{{Passed: false, Skipped: true}, {Passed: true}}},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.result.HasFailures() != tt.want {
				t.Errorf("HasFailures() = %v, want %v", tt.result.HasFailures(), tt.want)
			}
		})
	}
}

func TestPromotionResult_HasPendingData(t *testing.T) {
	result := PromotionResult{
		Checks: []PromotionCheck{
			{Name: CheckObsWindow, Passed: false},
		},
	}
	if !result.HasPendingData() {
		t.Error("HasPendingData should be true when observation window check fails")
	}

	result2 := PromotionResult{
		Checks: []PromotionCheck{
			{Name: CheckObsWindow, Passed: true},
		},
	}
	if result2.HasPendingData() {
		t.Error("HasPendingData should be false when observation window check passes")
	}
}

// --- CanaryPromoter.EvaluatePromotion tests ---

func TestEvaluatePromotion_AllChecksPass(t *testing.T) {
	promoter := NewCanaryPromoter()

	report := &DifferentialReport{
		Production: MetricsSnapshot{SuccessRate: 0.9997},
		Shadow:     MetricsSnapshot{SuccessRate: 0.9997, NewErrorTypes: 0},
		AllPassed:  true,
	}
	contractResults := []CheckResult{
		{Passed: true},
		{Passed: true},
	}
	drift := &DriftAssessment{
		CriticalCount: 0,
		MetricReports: []MetricDriftReport{
			{Exceeds: false, Severity: SeverityNone},
		},
	}

	result := promoter.EvaluatePromotion(report, contractResults, drift, 25*time.Hour, 24*time.Hour)

	if result.Decision != PromotionReady {
		t.Errorf("Decision = %s, want READY", result.Decision)
	}
	if !result.AllPassed() {
		t.Error("AllPassed should be true")
	}
}

func TestEvaluatePromotion_ContractFails(t *testing.T) {
	promoter := NewCanaryPromoter()

	report := &DifferentialReport{
		Production: MetricsSnapshot{SuccessRate: 0.9997},
		Shadow:     MetricsSnapshot{SuccessRate: 0.9997, NewErrorTypes: 0},
	}
	contractResults := []CheckResult{
		{Passed: true},
		{Passed: false, Reason: "rate limit missing"},
	}
	drift := &DriftAssessment{CriticalCount: 0}

	result := promoter.EvaluatePromotion(report, contractResults, drift, 25*time.Hour, 24*time.Hour)

	if result.Decision != PromotionNotReady {
		t.Errorf("Decision = %s, want NOT_READY (contract failed)", result.Decision)
	}
}

func TestEvaluatePromotion_DriftCritical(t *testing.T) {
	promoter := NewCanaryPromoter()

	report := &DifferentialReport{
		Production: MetricsSnapshot{SuccessRate: 0.9997},
		Shadow:     MetricsSnapshot{SuccessRate: 0.9997, NewErrorTypes: 0},
	}
	contractResults := []CheckResult{{Passed: true}}
	drift := &DriftAssessment{
		CriticalCount: 1,
		MetricReports: []MetricDriftReport{
			{Metric: "p99_latency", Exceeds: true, Severity: SeverityCritical},
		},
	}

	result := promoter.EvaluatePromotion(report, contractResults, drift, 25*time.Hour, 24*time.Hour)

	if result.Decision != PromotionNotReady {
		t.Errorf("Decision = %s, want NOT_READY (critical drift)", result.Decision)
	}
}

func TestEvaluatePromotion_SuccessRateDegraded(t *testing.T) {
	promoter := NewCanaryPromoter()

	report := &DifferentialReport{
		Production: MetricsSnapshot{SuccessRate: 0.9997},
		Shadow:     MetricsSnapshot{SuccessRate: 0.9950, NewErrorTypes: 0}, // -0.447%, exceeds 0.1%
	}
	contractResults := []CheckResult{{Passed: true}}
	drift := &DriftAssessment{CriticalCount: 0}

	result := promoter.EvaluatePromotion(report, contractResults, drift, 25*time.Hour, 24*time.Hour)

	if result.Decision != PromotionNotReady {
		t.Errorf("Decision = %s, want NOT_READY (success rate degraded)", result.Decision)
	}
}

func TestEvaluatePromotion_NewErrorTypes(t *testing.T) {
	promoter := NewCanaryPromoter()

	report := &DifferentialReport{
		Production: MetricsSnapshot{SuccessRate: 0.9997},
		Shadow:     MetricsSnapshot{SuccessRate: 0.9997, NewErrorTypes: 1},
	}
	contractResults := []CheckResult{{Passed: true}}
	drift := &DriftAssessment{CriticalCount: 0}

	result := promoter.EvaluatePromotion(report, contractResults, drift, 25*time.Hour, 24*time.Hour)

	if result.Decision != PromotionNotReady {
		t.Errorf("Decision = %s, want NOT_READY (new error types)", result.Decision)
	}
}

func TestEvaluatePromotion_ObservationWindowNotElapsed(t *testing.T) {
	promoter := NewCanaryPromoter()

	report := &DifferentialReport{
		Production: MetricsSnapshot{SuccessRate: 0.9997},
		Shadow:     MetricsSnapshot{SuccessRate: 0.9997, NewErrorTypes: 0},
	}
	contractResults := []CheckResult{{Passed: true}}
	drift := &DriftAssessment{CriticalCount: 0}

	result := promoter.EvaluatePromotion(report, contractResults, drift, 10*time.Hour, 24*time.Hour)

	if result.Decision != PromotionNotReady {
		t.Errorf("Decision = %s, want NOT_READY (observation window not elapsed)", result.Decision)
	}
}

func TestEvaluatePromotion_NilInputs(t *testing.T) {
	promoter := NewCanaryPromoter()

	result := promoter.EvaluatePromotion(nil, nil, nil, 25*time.Hour, 24*time.Hour)

	if result.Decision != PromotionNeedsMoreData {
		t.Errorf("Decision = %s, want NEEDS_MORE_DATA (nil inputs)", result.Decision)
	}
	for _, c := range result.Checks {
		if c.Name != CheckObsWindow && !c.Skipped {
			t.Errorf("Check %s should be skipped when input is nil", c.Name)
		}
	}
}

func TestEvaluatePromotion_NilInputsAndWindowNotElapsed(t *testing.T) {
	promoter := NewCanaryPromoter()

	result := promoter.EvaluatePromotion(nil, nil, nil, 5*time.Hour, 24*time.Hour)

	// With nil inputs AND window not elapsed → still NOT_READY (window is a real failure)
	if result.Decision != PromotionNotReady {
		t.Errorf("Decision = %s, want NOT_READY (window not elapsed)", result.Decision)
	}
}

func TestEvaluatePromotion_EmptyContractResults(t *testing.T) {
	promoter := NewCanaryPromoter()

	report := &DifferentialReport{
		Production: MetricsSnapshot{SuccessRate: 0.9997},
		Shadow:     MetricsSnapshot{SuccessRate: 0.9997, NewErrorTypes: 0},
	}
	drift := &DriftAssessment{CriticalCount: 0}

	result := promoter.EvaluatePromotion(report, []CheckResult{}, drift, 25*time.Hour, 24*time.Hour)

	// Empty contract results: AllPassed([]CheckResult{}) = true (no items to fail)
	if result.Decision != PromotionReady {
		t.Errorf("Decision = %s, want READY (empty contract = vacuously true)", result.Decision)
	}
}

func TestEvaluatePromotion_DriftWarningOnly(t *testing.T) {
	promoter := NewCanaryPromoter()

	report := &DifferentialReport{
		Production: MetricsSnapshot{SuccessRate: 0.9997},
		Shadow:     MetricsSnapshot{SuccessRate: 0.9997, NewErrorTypes: 0},
	}
	contractResults := []CheckResult{{Passed: true}}
	drift := &DriftAssessment{
		CriticalCount: 0,
		WarningCount:  1,
		MetricReports: []MetricDriftReport{
			{Metric: "p50_latency", Exceeds: true, Severity: SeverityWarning},
		},
	}

	result := promoter.EvaluatePromotion(report, contractResults, drift, 25*time.Hour, 24*time.Hour)

	// Warnings alone don't block promotion
	if result.Decision != PromotionReady {
		t.Errorf("Decision = %s, want READY (warning-only drift)", result.Decision)
	}
}

func TestEvaluatePromotion_FullCheckCount(t *testing.T) {
	promoter := NewCanaryPromoter()
	result := promoter.EvaluatePromotion(nil, nil, nil, 0, 1*time.Hour)

	if len(result.Checks) != 5 {
		t.Errorf("Expected 5 checks, got %d", len(result.Checks))
	}
}

func TestEvaluatePromotion_SuccessRateAtThreshold(t *testing.T) {
	promoter := NewCanaryPromoter()

	// Delta well within tolerance
	report := &DifferentialReport{
		Production: MetricsSnapshot{SuccessRate: 0.9999},
		Shadow:     MetricsSnapshot{SuccessRate: 0.9998, NewErrorTypes: 0}, // delta = 0.0001
	}
	contractResults := []CheckResult{{Passed: true}}
	drift := &DriftAssessment{CriticalCount: 0}

	result := promoter.EvaluatePromotion(report, contractResults, drift, 25*time.Hour, 24*time.Hour)

	if result.Decision != PromotionReady {
		t.Errorf("Decision = %s, want READY (within threshold)", result.Decision)
	}
}

// --- ComputeCanaryPercentage tests ---

func TestComputeCanaryPercentage(t *testing.T) {
	tests := []struct {
		tier string
		want float64
	}{
		{"provisional", 1.0},
		{"observed", 5.0},
		{"trusted", 10.0},
		{"veteran", 25.0},
		{"unknown", 1.0}, // conservative default
		{"", 1.0},
	}
	for _, tt := range tests {
		t.Run(tt.tier, func(t *testing.T) {
			got := ComputeCanaryPercentage(tt.tier)
			if got != tt.want {
				t.Errorf("ComputeCanaryPercentage(%q) = %.1f, want %.1f", tt.tier, got, tt.want)
			}
		})
	}
}

// --- AutoRampSchedule tests ---

func TestAutoRampSchedule_Provisional(t *testing.T) {
	steps := AutoRampSchedule("provisional")

	if len(steps) != 6 {
		t.Fatalf("Provisional should have 6 ramp steps, got %d", len(steps))
	}
	if steps[0].TrafficPct != 1 {
		t.Errorf("First step traffic = %.1f, want 1", steps[0].TrafficPct)
	}
	if steps[len(steps)-1].TrafficPct != 100 {
		t.Errorf("Last step traffic = %.1f, want 100", steps[len(steps)-1].TrafficPct)
	}
}

func TestAutoRampSchedule_Veteran(t *testing.T) {
	steps := AutoRampSchedule("veteran")

	if len(steps) != 2 {
		t.Fatalf("Veteran should have 2 ramp steps, got %d", len(steps))
	}
	if steps[0].TrafficPct != 10 {
		t.Errorf("First step traffic = %.1f, want 10", steps[0].TrafficPct)
	}
}

func TestAutoRampSchedule_AllTiers(t *testing.T) {
	tiers := []struct {
		tier      string
		wantSteps int
	}{
		{"provisional", 6},
		{"observed", 4},
		{"trusted", 3},
		{"veteran", 2},
	}
	for _, tt := range tiers {
		t.Run(tt.tier, func(t *testing.T) {
			steps := AutoRampSchedule(tt.tier)
			if len(steps) != tt.wantSteps {
				t.Errorf("AutoRampSchedule(%q) steps = %d, want %d", tt.tier, len(steps), tt.wantSteps)
			}
			// Last step should always be 100%
			if steps[len(steps)-1].TrafficPct != 100 {
				t.Errorf("Last step traffic = %.1f, want 100", steps[len(steps)-1].TrafficPct)
			}
			// Each step should have a non-zero observation duration
			for _, s := range steps {
				if s.ObservationDuration <= 0 {
					t.Errorf("Step %d has zero observation duration", s.Step)
				}
			}
		})
	}
}

func TestAutoRampSchedule_UnknownTier(t *testing.T) {
	steps := AutoRampSchedule("nonexistent")

	// Should fall back to default schedule
	if len(steps) < 2 {
		t.Errorf("Unknown tier should fall back to at least 2 steps, got %d", len(steps))
	}
	if steps[len(steps)-1].TrafficPct != 100 {
		t.Errorf("Last step traffic = %.1f, want 100", steps[len(steps)-1].TrafficPct)
	}
}

func TestAutoRampSchedule_RampIsMonotonic(t *testing.T) {
	for _, tier := range []string{"provisional", "observed", "trusted", "veteran"} {
		steps := AutoRampSchedule(tier)
		for i := 1; i < len(steps); i++ {
			if steps[i].TrafficPct < steps[i-1].TrafficPct {
				t.Errorf("Tier %s: step %d traffic (%.1f) < step %d (%.1f) — ramp must be monotonic",
					tier, i, steps[i].TrafficPct, i-1, steps[i-1].TrafficPct)
			}
		}
	}
}

// --- DriftAssessment helper tests ---

func TestDriftAssessment_HasCriticalBreach(t *testing.T) {
	da := &DriftAssessment{CriticalCount: 1}
	if !da.HasCriticalBreach() {
		t.Error("HasCriticalBreach should be true with CriticalCount=1")
	}

	da2 := &DriftAssessment{CriticalCount: 0}
	if da2.HasCriticalBreach() {
		t.Error("HasCriticalBreach should be false with CriticalCount=0")
	}

	var nilDA *DriftAssessment
	if nilDA.HasCriticalBreach() {
		t.Error("HasCriticalBreach should be false for nil receiver")
	}
}

func TestDriftAssessment_DriftCount(t *testing.T) {
	da := &DriftAssessment{
		MetricReports: []MetricDriftReport{
			{Exceeds: true},
			{Exceeds: false},
			{Exceeds: true},
		},
	}
	if da.DriftCount() != 2 {
		t.Errorf("DriftCount = %d, want 2", da.DriftCount())
	}
}

func TestDriftAssessment_DriftCount_Empty(t *testing.T) {
	da := &DriftAssessment{}
	if da.DriftCount() != 0 {
		t.Errorf("DriftCount = %d, want 0 for empty", da.DriftCount())
	}
}

func TestDriftAssessment_DriftCount_Nil(t *testing.T) {
	var da *DriftAssessment
	if da.DriftCount() != 0 {
		t.Errorf("DriftCount = %d, want 0 for nil", da.DriftCount())
	}
}

// --- computeDecision tests ---

func TestComputeDecision_AllPassed(t *testing.T) {
	result := &PromotionResult{
		Checks: []PromotionCheck{
			{Name: CheckContractPassed, Passed: true},
			{Name: CheckDriftOK, Passed: true},
			{Name: CheckSuccessRate, Passed: true},
			{Name: CheckNoNewErrors, Passed: true},
			{Name: CheckObsWindow, Passed: true},
		},
	}
	if computeDecision(result) != PromotionReady {
		t.Error("Should be READY when all pass")
	}
}

func TestComputeDecision_WithSkipped(t *testing.T) {
	result := &PromotionResult{
		Checks: []PromotionCheck{
			{Name: CheckContractPassed, Passed: true},
			{Name: CheckDriftOK, Passed: false, Skipped: true},
		},
	}
	if computeDecision(result) != PromotionNeedsMoreData {
		t.Error("Should be NEEDS_MORE_DATA when checks are skipped")
	}
}

func TestComputeDecision_WithFailure(t *testing.T) {
	result := &PromotionResult{
		Checks: []PromotionCheck{
			{Name: CheckContractPassed, Passed: true},
			{Name: CheckSuccessRate, Passed: false},
		},
	}
	if computeDecision(result) != PromotionNotReady {
		t.Error("Should be NOT_READY when a check fails")
	}
}

// --- Integration test ---

func TestEvaluatePromotion_RealisticScenario_Pass(t *testing.T) {
	promoter := NewCanaryPromoter()

	report := &DifferentialReport{
		Production: MetricsSnapshot{
			SuccessRate:   0.9997,
			P99LatencyMs:  87,
			P50LatencyMs:  12,
			NewErrorTypes: 0,
		},
		Shadow: MetricsSnapshot{
			SuccessRate:   0.9996,
			P99LatencyMs:  90,
			P50LatencyMs:  13,
			NewErrorTypes: 0,
		},
		AllPassed: true,
	}
	contractResults := []CheckResult{
		{Passed: true, Assertion: Assertion{Metric: "auth_success_rate", Op: "gte"}},
		{Passed: true, Assertion: Assertion{Metric: "auth_p99_latency_ms", Op: "lte"}},
	}
	drift := &DriftAssessment{
		CriticalCount: 0,
		WarningCount:  0,
		MetricReports: []MetricDriftReport{
			{Metric: "p99_latency", Exceeds: false, Severity: SeverityNone},
		},
	}

	result := promoter.EvaluatePromotion(report, contractResults, drift, 25*time.Hour, 24*time.Hour)

	if result.Decision != PromotionReady {
		t.Errorf("Realistic pass scenario: Decision = %s, want READY", result.Decision)
	}
}

func TestEvaluatePromotion_RealisticScenario_Fail(t *testing.T) {
	promoter := NewCanaryPromoter()

	report := &DifferentialReport{
		Production: MetricsSnapshot{
			SuccessRate:   0.9997,
			P99LatencyMs:  87,
			NewErrorTypes: 0,
		},
		Shadow: MetricsSnapshot{
			SuccessRate:   0.9900, // -0.97%, way over 0.1% tolerance
			P99LatencyMs:  150,    // +72%
			NewErrorTypes: 2,
		},
		AllPassed:   false,
		BlockReason: "success rate and latency breach",
	}
	contractResults := []CheckResult{
		{Passed: true},
		{Passed: false, Reason: "auth_p99_latency_ms measured 150 > threshold 200"},
	}
	drift := &DriftAssessment{
		CriticalCount: 2,
		WarningCount:  1,
		MetricReports: []MetricDriftReport{
			{Metric: "success_rate", Exceeds: true, Severity: SeverityCritical},
			{Metric: "p99_latency", Exceeds: true, Severity: SeverityCritical},
		},
	}

	result := promoter.EvaluatePromotion(report, contractResults, drift, 25*time.Hour, 24*time.Hour)

	if result.Decision != PromotionNotReady {
		t.Errorf("Realistic fail scenario: Decision = %s, want NOT_READY", result.Decision)
	}

	// Should have multiple failed checks
	failCount := 0
	for _, c := range result.Checks {
		if !c.Passed && !c.Skipped {
			failCount++
		}
	}
	if failCount < 3 {
		t.Errorf("Expected at least 3 failed checks, got %d", failCount)
	}
}
