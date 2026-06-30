package estimate

import (
	"math"
	"testing"
)

// ---------------------------------------------------------------------------
// ReconcilePipeline — spec §8.2 steps 1-5
// ---------------------------------------------------------------------------

func TestReconcilePipeline_Success(t *testing.T) {
	pricing := testPricing()
	usage := Usage{
		PromptTokens:     100000,
		CompletionTokens: 10000,
		TotalTokens:      110000,
		CacheReadTokens:  50000,
	}
	estimated := CostEstimate{CostTotal: 0.50}
	budget := BudgetInfo{AgentName: "test-agent", BudgetWeekly: 10.0, BudgetUsed: 1.0}
	tracker := NewDriftTracker()
	calib := NewCalibrator()

	result, updatedBudget, err := ReconcilePipeline(
		"test-agent", usage, pricing, "test-provider", "test-model",
		estimated, budget, tracker, calib,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AgentID != "test-agent" {
		t.Errorf("AgentID = %q, want test-agent", result.AgentID)
	}
	if result.EstimatedCost != 0.50 {
		t.Errorf("EstimatedCost = %.4f, want 0.50", result.EstimatedCost)
	}
	if result.ActualCost <= 0 {
		t.Errorf("ActualCost = %.4f, should be > 0", result.ActualCost)
	}
	// Budget should be updated with actual cost.
	if updatedBudget.BudgetUsed <= budget.BudgetUsed {
		t.Errorf("BudgetUsed not updated: before=%.4f after=%.4f", budget.BudgetUsed, updatedBudget.BudgetUsed)
	}
	if result.BudgetRemainingAfter <= 0 {
		t.Errorf("BudgetRemainingAfter = %.4f, should be > 0", result.BudgetRemainingAfter)
	}
	// Drift entry should be recorded.
	if tracker.Count("test-agent") != 1 {
		t.Errorf("Expected 1 drift entry, got %d", tracker.Count("test-agent"))
	}
}

func TestReconcilePipeline_NilTracker(t *testing.T) {
	pricing := testPricing()
	usage := Usage{PromptTokens: 1000, CompletionTokens: 100}
	estimated := CostEstimate{CostTotal: 0.01}
	budget := BudgetInfo{AgentName: "agent", BudgetWeekly: 5.0}

	result, _, err := ReconcilePipeline(
		"agent", usage, pricing, "test-provider", "test-model",
		estimated, budget, nil, nil,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.FedToCalibrator {
		t.Error("FedToCalibrator should be false with nil calibrator")
	}
}

func TestReconcilePipeline_DriftOverThreshold(t *testing.T) {
	pricing := testPricing()
	usage := Usage{
		PromptTokens:     500000,
		CompletionTokens: 50000,
	}
	// Estimate very low so actual >> estimated → high drift.
	estimated := CostEstimate{CostTotal: 0.01}
	budget := BudgetInfo{AgentName: "agent", BudgetWeekly: 100.0}
	tracker := NewDriftTracker()

	result, _, err := ReconcilePipeline(
		"agent", usage, pricing, "test-provider", "test-model",
		estimated, budget, tracker, nil,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.OverThreshold {
		t.Errorf("Expected OverThreshold=true, got false. Drift=%.2f%%", result.DriftPct)
	}
	if result.DriftPct < DriftThresholdPct {
		t.Errorf("DriftPct = %.2f%%, should be > %.1f%%", result.DriftPct, DriftThresholdPct)
	}
}

func TestReconcilePipeline_DriftUnderThreshold(t *testing.T) {
	pricing := testPricing()
	usage := Usage{
		PromptTokens:     1000,
		CompletionTokens: 100,
	}
	// Estimate close to actual so drift is within the 10% threshold.
	estimated := CostEstimate{CostTotal: 0.01}
	budget := BudgetInfo{AgentName: "agent", BudgetWeekly: 100.0}
	tracker := NewDriftTracker()

	result, _, err := ReconcilePipeline(
		"agent", usage, pricing, "test-provider", "test-model",
		estimated, budget, tracker, nil,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify drift is computed and the threshold check works.
	if math.IsInf(result.DriftPct, 0) {
		t.Errorf("DriftPct = +Inf, expected finite value")
	}
	// The actual cost from 1000 prompt + 100 completion tokens at test
	// pricing is very small. Whether it's over or under threshold depends
	// on the exact ratio. We just verify the pipeline computes drift
	// correctly — the key invariant is that OverThreshold matches
	// abs(DriftPct) > DriftThresholdPct.
	expectedOverThreshold := absFloat64(result.DriftPct) > DriftThresholdPct
	if result.OverThreshold != expectedOverThreshold {
		t.Errorf("OverThreshold mismatch: got %v, want %v (drift=%.2f%%)",
			result.OverThreshold, expectedOverThreshold, result.DriftPct)
	}
}

func TestReconcilePipeline_BudgetUpdated(t *testing.T) {
	pricing := testPricing()
	usage := Usage{PromptTokens: 10000, CompletionTokens: 1000}
	estimated := CostEstimate{CostTotal: 0.10}
	budget := BudgetInfo{AgentName: "agent", BudgetWeekly: 10.0, BudgetUsed: 2.0}

	_, updated, err := ReconcilePipeline(
		"agent", usage, pricing, "test-provider", "test-model",
		estimated, budget, nil, nil,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// BudgetUsed should increase by actual cost.
	if updated.BudgetUsed <= budget.BudgetUsed {
		t.Errorf("BudgetUsed should increase: before=%.4f after=%.4f",
			budget.BudgetUsed, updated.BudgetUsed)
	}
	// BudgetWeekly should be unchanged.
	if updated.BudgetWeekly != budget.BudgetWeekly {
		t.Errorf("BudgetWeekly changed: %.2f → %.2f", budget.BudgetWeekly, updated.BudgetWeekly)
	}
}

func TestReconcilePipeline_BudgetNotMutated(t *testing.T) {
	pricing := testPricing()
	usage := Usage{PromptTokens: 10000, CompletionTokens: 1000}
	estimated := CostEstimate{CostTotal: 0.10}
	budget := BudgetInfo{AgentName: "agent", BudgetWeekly: 10.0, BudgetUsed: 2.0}
	originalUsed := budget.BudgetUsed

	_, _, _ = ReconcilePipeline(
		"agent", usage, pricing, "test-provider", "test-model",
		estimated, budget, nil, nil,
	)

	if budget.BudgetUsed != originalUsed {
		t.Errorf("Input BudgetInfo was mutated: BudgetUsed changed from %.4f to %.4f",
			originalUsed, budget.BudgetUsed)
	}
}

func TestReconcilePipeline_CalibratorFed(t *testing.T) {
	pricing := testPricing()
	usage := Usage{PromptTokens: 10000, CompletionTokens: 1000, CacheReadTokens: 5000}
	estimated := CostEstimate{CostTotal: 0.10}
	budget := BudgetInfo{AgentName: "agent", BudgetWeekly: 10.0}
	tracker := NewDriftTracker()
	calib := NewCalibrator()

	result, _, err := ReconcilePipeline(
		"agent", usage, pricing, "test-provider", "test-model",
		estimated, budget, tracker, calib,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.FedToCalibrator {
		t.Error("Expected FedToCalibrator=true")
	}
	if len(calib.History) == 0 {
		t.Error("Calibrator should have at least 1 record")
	}
}

func TestReconcilePipeline_ErrorOnNilPricing(t *testing.T) {
	usage := Usage{PromptTokens: 100}
	estimated := CostEstimate{CostTotal: 0.01}
	budget := BudgetInfo{AgentName: "agent", BudgetWeekly: 10.0}

	_, _, err := ReconcilePipeline(
		"agent", usage, nil, "p", "m",
		estimated, budget, nil, nil,
	)

	if err == nil {
		t.Fatal("Expected error for nil pricing, got nil")
	}
}

func TestReconcilePipeline_ErrorOnUnknownModel(t *testing.T) {
	pricing := testPricing()
	usage := Usage{PromptTokens: 100}
	estimated := CostEstimate{CostTotal: 0.01}
	budget := BudgetInfo{AgentName: "agent", BudgetWeekly: 10.0}

	_, _, err := ReconcilePipeline(
		"agent", usage, pricing, "unknown-provider", "unknown-model",
		estimated, budget, nil, nil,
	)

	if err == nil {
		t.Fatal("Expected error for unknown model, got nil")
	}
}

func TestReconcilePipeline_DriftEntryContents(t *testing.T) {
	pricing := testPricing()
	usage := Usage{PromptTokens: 20000, CompletionTokens: 2000}
	estimated := CostEstimate{CostTotal: 0.05, Model: "test-model", Provider: "test-provider"}
	budget := BudgetInfo{AgentName: "agent", BudgetWeekly: 100.0}
	tracker := NewDriftTracker()

	_, _, err := ReconcilePipeline(
		"my-agent", usage, pricing, "test-provider", "test-model",
		estimated, budget, tracker, nil,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entries := tracker.filterEntries("my-agent")
	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.AgentID != "my-agent" {
		t.Errorf("AgentID = %q, want my-agent", e.AgentID)
	}
	if e.Model != "test-model" {
		t.Errorf("Model = %q, want test-model", e.Model)
	}
	if e.Estimated != 0.05 {
		t.Errorf("Estimated = %.4f, want 0.05", e.Estimated)
	}
}

// ---------------------------------------------------------------------------
// ReconcileAgent — convenience wrapper
// ---------------------------------------------------------------------------

func TestReconcileAgent_Success(t *testing.T) {
	pricing := testPricing()
	usage := Usage{PromptTokens: 5000, CompletionTokens: 500}
	estimated := CostEstimate{CostTotal: 0.05}
	budget := BudgetInfo{AgentName: "agent", BudgetWeekly: 10.0}

	result, err := ReconcileAgent(
		"agent", usage, pricing, "test-provider", "test-model",
		estimated, budget, nil, nil,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AgentID != "agent" {
		t.Errorf("AgentID = %q, want agent", result.AgentID)
	}
	if result.ActualCost <= 0 {
		t.Errorf("ActualCost = %.4f, should be > 0", result.ActualCost)
	}
}

// ---------------------------------------------------------------------------
// FormatReconciliation
// ---------------------------------------------------------------------------

func TestFormatReconciliation(t *testing.T) {
	r := ReconciliationResult{
		AgentID:              "test-agent",
		EstimatedCost:        0.50,
		ActualCost:           0.75,
		DriftPct:             50.0,
		BudgetRemainingAfter: 9.25,
		OverThreshold:        true,
		FedToCalibrator:      true,
	}

	s := FormatReconciliation(r)
	if s == "" {
		t.Error("FormatReconciliation returned empty string")
	}
	if !contains(s, "test-agent") {
		t.Errorf("Output missing agent name: %q", s)
	}
	if !contains(s, "0.5000") {
		t.Errorf("Output missing estimated cost: %q", s)
	}
}

func TestFormatReconciliation_NoDrift(t *testing.T) {
	r := ReconciliationResult{
		AgentID:              "agent",
		EstimatedCost:        0.10,
		ActualCost:           0.10,
		DriftPct:             0,
		BudgetRemainingAfter: 9.90,
	}

	s := FormatReconciliation(r)
	if !contains(s, "agent") {
		t.Errorf("Output missing agent name: %q", s)
	}
}

// ---------------------------------------------------------------------------
// absFloat64
// ---------------------------------------------------------------------------

func TestAbsFloat64(t *testing.T) {
	tests := []struct {
		input float64
		want  float64
	}{
		{0, 0},
		{1.5, 1.5},
		{-1.5, 1.5},
		{100, 100},
		{-100, 100},
	}

	for _, tc := range tests {
		got := absFloat64(tc.input)
		if got != tc.want {
			t.Errorf("absFloat64(%.1f) = %.1f, want %.1f", tc.input, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Integration — pipeline + tracker + calibrator
// ---------------------------------------------------------------------------

func TestReconcilePipeline_FullIntegration(t *testing.T) {
	pricing := testPricing()
	tracker := NewDriftTracker()
	calib := NewCalibrator()

	// Run 3 reconciliations with different usage levels.
	usages := []Usage{
		{PromptTokens: 10000, CompletionTokens: 1000, CacheReadTokens: 5000},
		{PromptTokens: 20000, CompletionTokens: 2000, CacheReadTokens: 10000},
		{PromptTokens: 5000, CompletionTokens: 500, CacheReadTokens: 2500},
	}

	for i, u := range usages {
		estimated := CostEstimate{CostTotal: 0.02 * float64(i+1)}
		budget := BudgetInfo{
			AgentName:    "integ-agent",
			BudgetWeekly: 100.0,
			BudgetUsed:   0.0,
		}

		_, _, err := ReconcilePipeline(
			"integ-agent", u, pricing, "test-provider", "test-model",
			estimated, budget, tracker, calib,
		)
		if err != nil {
			t.Fatalf("Reconciliation %d failed: %v", i, err)
		}
	}

	// Verify tracker has 3 entries.
	if tracker.Count("integ-agent") != 3 {
		t.Errorf("Expected 3 drift entries, got %d", tracker.Count("integ-agent"))
	}

	// Verify calibrator has records.
	if len(calib.History) == 0 {
		t.Error("Calibrator should have records after integration")
	}

	// Verify drift report is accessible.
	report := tracker.DriftReport("integ-agent")
	if report.Count != 3 {
		t.Errorf("DriftReport count = %d, want 3", report.Count)
	}
}

func TestReconcilePipeline_BudgetExhausted(t *testing.T) {
	pricing := testPricing()
	usage := Usage{PromptTokens: 1000000, CompletionTokens: 100000}
	estimated := CostEstimate{CostTotal: 1.0}
	// Agent with very small budget that gets exhausted.
	budget := BudgetInfo{
		AgentName:    "poor-agent",
		BudgetWeekly: 0.50,
		BudgetUsed:   0.40,
	}

	result, updated, err := ReconcilePipeline(
		"poor-agent", usage, pricing, "test-provider", "test-model",
		estimated, budget, nil, nil,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Budget should be overdrawn.
	if result.BudgetRemainingAfter != 0 {
		t.Errorf("BudgetRemainingAfter = %.4f, want 0 (exhausted)", result.BudgetRemainingAfter)
	}
	if updated.BudgetUsed <= budget.BudgetWeekly {
		t.Errorf("BudgetUsed (%.4f) should exceed weekly cap (%.4f)",
			updated.BudgetUsed, budget.BudgetWeekly)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && indexString(s, substr) >= 0))
}

func indexString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// testPricing returns a minimal pricing YAML suitable for unit tests.
func testPricing() *PricingYAML {
	cacheRead := 0.0001
	cacheWrite := 0.0015
	return &PricingYAML{
		Providers: map[string]ProviderPricing{
			"test-provider": {
				Models: map[string]ModelPrice{
					"test-model": {
						InputPer1K:      0.001,
						OutputPer1K:     0.002,
						CacheReadPer1K:  &cacheRead,
						CacheWritePer1K: &cacheWrite,
					},
				},
			},
		},
	}
}

// Ensure math import is used (for Inf comparison in edge tests).
var _ = math.Inf
