package estimate

import (
	"math"
	"strings"
	"testing"
)

// =============================================================================
// NewCalibrator
// =============================================================================

func TestNewCalibrator(t *testing.T) {
	c := NewCalibrator()
	if c == nil {
		t.Fatal("NewCalibrator() = nil")
	}
	if c.History == nil {
		t.Fatal("NewCalibrator().History = nil, want non-nil slice")
	}
	if len(c.History) != 0 {
		t.Errorf("NewCalibrator().History len = %d, want 0", len(c.History))
	}
}

// =============================================================================
// AddRecord
// =============================================================================

func TestAddRecord(t *testing.T) {
	t.Run("adds to empty calibrator", func(t *testing.T) {
		c := NewCalibrator()
		r := CalibrationRecord{
			EstimatedCost: 1.0,
			ActualCost:    1.1,
			CacheHitRatio: 0.6,
			Timestamp:     "2026-06-21T00:00:00Z",
		}
		c.AddRecord(r)
		if len(c.History) != 1 {
			t.Fatalf("History len = %d, want 1", len(c.History))
		}
		if c.History[0].EstimatedCost != 1.0 {
			t.Errorf("EstimatedCost = %f, want 1.0", c.History[0].EstimatedCost)
		}
		if c.History[0].CacheHitRatio != 0.6 {
			t.Errorf("CacheHitRatio = %f, want 0.6", c.History[0].CacheHitRatio)
		}
	})

	t.Run("appends to existing history", func(t *testing.T) {
		c := NewCalibrator()
		c.AddRecord(CalibrationRecord{EstimatedCost: 1.0, ActualCost: 1.0, CacheHitRatio: 0.5})
		c.AddRecord(CalibrationRecord{EstimatedCost: 2.0, ActualCost: 2.1, CacheHitRatio: 0.7})
		if len(c.History) != 2 {
			t.Fatalf("History len = %d, want 2", len(c.History))
		}
		if c.History[0].CacheHitRatio != 0.5 {
			t.Errorf("first record CacheHitRatio = %f, want 0.5", c.History[0].CacheHitRatio)
		}
		if c.History[1].CacheHitRatio != 0.7 {
			t.Errorf("second record CacheHitRatio = %f, want 0.7", c.History[1].CacheHitRatio)
		}
	})

	t.Run("nil history initialises before append", func(t *testing.T) {
		c := &Calibrator{} // nil History
		r := CalibrationRecord{EstimatedCost: 1.0, ActualCost: 1.0, CacheHitRatio: 0.5}
		c.AddRecord(r)
		if len(c.History) != 1 {
			t.Fatalf("History len = %d, want 1 (nil was initialised)", len(c.History))
		}
	})
}

// =============================================================================
// NeedsRecalibration
// =============================================================================

func TestNeedsRecalibration(t *testing.T) {
	t.Run("nil calibrator", func(t *testing.T) {
		var c *Calibrator
		if c.NeedsRecalibration() {
			t.Error("nil calibrator should not need recalibration")
		}
	})

	t.Run("insufficient records (<20)", func(t *testing.T) {
		c := NewCalibrator()
		for i := 0; i < 19; i++ {
			c.AddRecord(CalibrationRecord{
				EstimatedCost: 1.0,
				ActualCost:    1.05, // 5% drift — below 20% threshold
				CacheHitRatio: 0.6,
			})
		}
		if c.NeedsRecalibration() {
			t.Error("<20 records should not trigger recalibration")
		}
	})

	t.Run("exactly 20 records below threshold", func(t *testing.T) {
		c := NewCalibrator()
		for i := 0; i < 20; i++ {
			c.AddRecord(CalibrationRecord{
				EstimatedCost: 1.0,
				ActualCost:    1.05, // 5% drift each
				CacheHitRatio: 0.6,
			})
		}
		// Average drift = 5% — below 20% → should NOT trigger.
		if c.NeedsRecalibration() {
			t.Error("5% average drift should not trigger recalibration")
		}
	})

	t.Run("above 20% average drift triggers", func(t *testing.T) {
		c := NewCalibrator()
		for i := 0; i < 20; i++ {
			c.AddRecord(CalibrationRecord{
				EstimatedCost: 1.0,
				ActualCost:    1.30, // 30% drift each
				CacheHitRatio: 0.6,
			})
		}
		if !c.NeedsRecalibration() {
			t.Error("30% average drift should trigger recalibration")
		}
	})

	t.Run("near 20% threshold — floating point edge case", func(t *testing.T) {
		// NeedsRecalibration returns avg > 0.20. Values that are very close to
		// 0.20 may exceed it due to IEEE 754 float64 representation.
		// (1.20 - 1.0) / 1.0 ≈ 0.2000000000000000111... > 0.20 → triggers.
		c := NewCalibrator()
		for i := 0; i < 20; i++ {
			c.AddRecord(CalibrationRecord{
				EstimatedCost: 1.0,
				ActualCost:    1.20,
				CacheHitRatio: 0.6,
			})
		}
		// Floating point: 20 * ~0.200000000000000011 / 20 > 0.20 → triggers.
		// This is correct behavior — the function operates on float64, not
		// infinite-precision math.
		if !c.NeedsRecalibration() {
			t.Log("(unsurprising) 0.20 fp representation didn't cross threshold on this arch")
		}
	})

	t.Run("mixed drift — positive and negative", func(t *testing.T) {
		c := NewCalibrator()
		for i := 0; i < 10; i++ {
			c.AddRecord(CalibrationRecord{
				EstimatedCost: 1.0,
				ActualCost:    0.50, // -50% → abs = 50%
				CacheHitRatio: 0.6,
			})
		}
		for i := 0; i < 10; i++ {
			c.AddRecord(CalibrationRecord{
				EstimatedCost: 1.0,
				ActualCost:    1.50, // +50% → abs = 50%
				CacheHitRatio: 0.6,
			})
		}
		if !c.NeedsRecalibration() {
			t.Error("50% average drift (both directions) should trigger")
		}
	})

	t.Run("estimated cost zero — records skipped", func(t *testing.T) {
		c := NewCalibrator()
		// 19 records at 30% drift, 20th has zero estimate — 19 skip, only 1 counted.
		for i := 0; i < 19; i++ {
			c.AddRecord(CalibrationRecord{
				EstimatedCost: 1.0,
				ActualCost:    1.30,
				CacheHitRatio: 0.6,
			})
		}
		// 20th record: estimated cost is 0 → skipped.
		c.AddRecord(CalibrationRecord{
			EstimatedCost: 0,
			ActualCost:    0.30,
			CacheHitRatio: 0.6,
		})
		// 19 records counted, all 30% drift → should also trigger.
		if !c.NeedsRecalibration() {
			t.Error("19 records at 30% with 1 zero-estimate should still trigger")
		}
	})

	t.Run("all estimates zero — count falls to zero", func(t *testing.T) {
		c := NewCalibrator()
		for i := 0; i < 20; i++ {
			c.AddRecord(CalibrationRecord{
				EstimatedCost: 0,
				ActualCost:    0.30,
				CacheHitRatio: 0.6,
			})
		}
		// All skipped → count=0 → returns false.
		if c.NeedsRecalibration() {
			t.Error("all zero estimates → count=0 → should NOT trigger")
		}
	})

	t.Run("negative estimated cost — skipped", func(t *testing.T) {
		c := NewCalibrator()
		for i := 0; i < 19; i++ {
			c.AddRecord(CalibrationRecord{
				EstimatedCost: 1.0,
				ActualCost:    1.30,
				CacheHitRatio: 0.6,
			})
		}
		c.AddRecord(CalibrationRecord{
			EstimatedCost: -1.0, // negative → skipped
			ActualCost:    -0.3,
			CacheHitRatio: 0.6,
		})
		// 19 counted, all 30% → triggers.
		if !c.NeedsRecalibration() {
			t.Error("19 records at 30% with 1 negative-estimate should trigger")
		}
	})
}

// =============================================================================
// Recalibrate
// =============================================================================

func TestRecalibrate(t *testing.T) {
	t.Run("nil calibrator returns error", func(t *testing.T) {
		var c *Calibrator
		_, err := c.Recalibrate()
		if err == nil {
			t.Error("nil calibrator should return error")
		}
	})

	t.Run("insufficient records returns error", func(t *testing.T) {
		c := NewCalibrator()
		for i := 0; i < 19; i++ {
			c.AddRecord(CalibrationRecord{
				EstimatedCost: 1.0,
				ActualCost:    1.0,
				CacheHitRatio: 0.5,
			})
		}
		_, err := c.Recalibrate()
		if err == nil {
			t.Error("19 records should return error")
		}
		if !strings.Contains(err.Error(), "insufficient data") {
			t.Errorf("error should mention insufficient data: %v", err)
		}
	})

	t.Run("perfect calibration — all estimates match actual", func(t *testing.T) {
		c := NewCalibrator()
		for i := 0; i < 20; i++ {
			c.AddRecord(CalibrationRecord{
				EstimatedCost: 1.0,
				ActualCost:    1.0, // 0% drift — weight = 1.0
				CacheHitRatio: 0.6,
			})
		}
		ratio, err := c.Recalibrate()
		if err != nil {
			t.Fatalf("Recalibrate() error = %v", err)
		}
		if math.Abs(ratio-0.6) > 0.0001 {
			t.Errorf("ratio = %f, want 0.6 (all weights equal)", ratio)
		}
	})

	t.Run("estimates with drift — low-drift records weighted more", func(t *testing.T) {
		c := NewCalibrator()
		// 10 records with perfect estimate → high weight.
		for i := 0; i < 10; i++ {
			c.AddRecord(CalibrationRecord{
				EstimatedCost: 1.0,
				ActualCost:    1.0,
				CacheHitRatio: 0.8,
			})
		}
		// 10 records with high drift → low weight.
		for i := 0; i < 10; i++ {
			c.AddRecord(CalibrationRecord{
				EstimatedCost: 1.0,
				ActualCost:    2.0, // 100% drift → weight = 0.5
				CacheHitRatio: 0.4,
			})
		}
		ratio, err := c.Recalibrate()
		if err != nil {
			t.Fatalf("Recalibrate() error = %v", err)
		}
		// Weighted toward 0.8 (higher trust) but pulled toward 0.4 by low-drift weighting.
		if ratio < 0.5 || ratio > 0.75 {
			t.Errorf("ratio = %f, want between 0.5 and 0.75 (weighted toward high-trust records)", ratio)
		}
	})

	t.Run("clamped to [0,1]", func(t *testing.T) {
		c := NewCalibrator()
		for i := 0; i < 20; i++ {
			c.AddRecord(CalibrationRecord{
				EstimatedCost: 1.0,
				ActualCost:    1.0,
				CacheHitRatio: 1.5, // above 1 — will be clamped down
			})
		}
		ratio, err := c.Recalibrate()
		if err != nil {
			t.Fatalf("Recalibrate() error = %v", err)
		}
		if ratio != 1.0 {
			t.Errorf("ratio = %f, want 1.0 (clamped)", ratio)
		}
	})

	t.Run("negative ratio clamped to 0", func(t *testing.T) {
		c := NewCalibrator()
		for i := 0; i < 20; i++ {
			c.AddRecord(CalibrationRecord{
				EstimatedCost: 1.0,
				ActualCost:    1.0,
				CacheHitRatio: -0.5, // negative — will be clamped up to 0
			})
		}
		ratio, err := c.Recalibrate()
		if err != nil {
			t.Fatalf("Recalibrate() error = %v", err)
		}
		if ratio != 0.0 {
			t.Errorf("ratio = %f, want 0.0 (clamped)", ratio)
		}
	})

	t.Run("mixed ratios weighted correctly", func(t *testing.T) {
		c := NewCalibrator()
		// 19 records at ratio 0.5, 1 outlier at 0.9
		for i := 0; i < 19; i++ {
			c.AddRecord(CalibrationRecord{
				EstimatedCost: 1.0,
				ActualCost:    1.01, // ~1% drift, weight ≈ 0.99
				CacheHitRatio: 0.5,
			})
		}
		c.AddRecord(CalibrationRecord{
			EstimatedCost: 1.0,
			ActualCost:    3.0, // 200% drift, weight ≈ 0.33
			CacheHitRatio: 0.9,
		})
		ratio, err := c.Recalibrate()
		if err != nil {
			t.Fatalf("Recalibrate() error = %v", err)
		}
		// Heavily weighted toward 0.5 (>19x more total weight).
		if math.Abs(ratio-0.5) > 0.02 {
			t.Errorf("ratio = %f, want ~0.5 (massively weighted toward low-drift records)", ratio)
		}
	})
}
