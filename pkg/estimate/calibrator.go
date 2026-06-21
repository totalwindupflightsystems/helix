package estimate

import (
	"fmt"
	"math"
)

// ---------------------------------------------------------------------------
// Cache-ratio calibration (spec §12) — v1 skeleton
// ---------------------------------------------------------------------------

// CalibrationRecord is a single historical estimate-vs-actual data point used
// to detect estimation drift and recompute cache hit ratios.
type CalibrationRecord struct {
	EstimatedCost float64 `json:"estimated_cost"`
	ActualCost    float64 `json:"actual_cost"`
	CacheHitRatio float64 `json:"cache_hit_ratio"` // ratio applied at estimate time
	Timestamp     string  `json:"timestamp"`
}

// Calibrator accumulates historical records and decides when the cache hit
// ratios need recomputing. In v1 it provides the structural skeleton; the
// weekly recalibration cron (spec §12.2) consumes this in a follow-up.
type Calibrator struct {
	History []CalibrationRecord
}

// NewCalibrator returns an empty calibrator ready to accumulate records.
func NewCalibrator() *Calibrator {
	return &Calibrator{History: []CalibrationRecord{}}
}

// AddRecord appends a calibration data point.
func (c *Calibrator) AddRecord(r CalibrationRecord) {
	if c.History == nil {
		c.History = []CalibrationRecord{}
	}
	c.History = append(c.History, r)
}

// NeedsRecalibration reports whether the average drift exceeds 20% (in either
// direction) over at least 20 records (spec §14). Fewer than 20 records always
// returns false — not enough signal to trust a recompute.
func (c *Calibrator) NeedsRecalibration() bool {
	if c == nil || len(c.History) < 20 {
		return false
	}
	var sumDrift float64
	var count int
	for _, r := range c.History {
		if r.EstimatedCost <= 0 {
			continue
		}
		sumDrift += math.Abs((r.ActualCost - r.EstimatedCost) / r.EstimatedCost)
		count++
	}
	if count == 0 {
		return false
	}
	avg := sumDrift / float64(count)
	return avg > 0.20
}

// Recalibrate computes a new cache hit ratio from the historical data. It
// weights each record's applied ratio by how close its estimate was to actual
// (records with low drift contribute more), which nudges the ratio toward
// values that produced accurate estimates. Returns an error if there are fewer
// than 20 records (insufficient signal per spec §14).
func (c *Calibrator) Recalibrate() (float64, error) {
	if c == nil {
		return 0, fmt.Errorf("calibrator is nil")
	}
	if len(c.History) < 20 {
		return 0, fmt.Errorf("insufficient data: have %d records, need >= 20", len(c.History))
	}

	var weightedSum, weightSum float64
	for _, r := range c.History {
		// Inverse-drift weight: estimates that were nearly correct dominate.
		drift := math.Abs((r.ActualCost - r.EstimatedCost) / r.EstimatedCost)
		weight := 1.0 / (1.0 + drift)
		weightedSum += r.CacheHitRatio * weight
		weightSum += weight
	}
	if weightSum == 0 {
		return 0, fmt.Errorf("calibration produced zero weight (all estimates zero)")
	}
	ratio := weightedSum / weightSum
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	return ratio, nil
}
