package estimate

import (
	"testing"
)

// =============================================================================
// NewEstimator
// =============================================================================

func TestNewEstimator(t *testing.T) {
	t.Run("default tier is pro", func(t *testing.T) {
		pricing := &PricingYAML{
			Cache: CacheRatios{
				ProHitRatio:   0.60,
				FlashHitRatio: 0.80,
			},
		}
		e := NewEstimator(pricing, "")
		if e == nil {
			t.Fatal("NewEstimator() = nil")
		}
		if e.Tier != "pro" {
			t.Errorf("Tier = %q, want %q", e.Tier, "pro")
		}
		if e.Pricing != pricing {
			t.Error("Pricing pointer not preserved")
		}
	})

	t.Run("explicit pro tier", func(t *testing.T) {
		pricing := &PricingYAML{}
		e := NewEstimator(pricing, "pro")
		if e.Tier != "pro" {
			t.Errorf("Tier = %q, want %q", e.Tier, "pro")
		}
	})

	t.Run("explicit flash tier", func(t *testing.T) {
		pricing := &PricingYAML{}
		e := NewEstimator(pricing, "flash")
		if e.Tier != "flash" {
			t.Errorf("Tier = %q, want %q", e.Tier, "flash")
		}
	})

	t.Run("explicit cold tier", func(t *testing.T) {
		pricing := &PricingYAML{}
		e := NewEstimator(pricing, "cold")
		if e.Tier != "cold" {
			t.Errorf("Tier = %q, want %q", e.Tier, "cold")
		}
	})
}

// =============================================================================
// hitRatio
// =============================================================================

func TestEstimator_hitRatio(t *testing.T) {
	pricing := &PricingYAML{
		Cache: CacheRatios{
			ProHitRatio:      0.60,
			FlashHitRatio:    0.80,
			NewRepoHitRatio:  0.0,
		},
	}

	tests := []struct {
		name string
		tier string
		want float64
	}{
		{"pro", "pro", 0.60},
		{"flash", "flash", 0.80},
		{"cold", "cold", 0.0},
		{"unrecognized falls back to pro", "unknown", 0.60},
		{"empty falls back to pro (shouldn't happen — NewEstimator sets pro)", "", 0.60},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Estimator{Pricing: pricing, Tier: tt.tier}
			if got := e.hitRatio(); got != tt.want {
				t.Errorf("hitRatio() = %f, want %f", got, tt.want)
			}
		})
	}
}

// =============================================================================
// writeRatio
// =============================================================================

func TestEstimator_writeRatio(t *testing.T) {
	pricing := &PricingYAML{
		Cache: CacheRatios{
			ProWriteRatio:      0.50,
			FlashWriteRatio:    0.70,
			NewRepoWriteRatio:  0.50,
		},
	}

	tests := []struct {
		name string
		tier string
		want float64
	}{
		{"pro", "pro", 0.50},
		{"flash", "flash", 0.70},
		{"cold", "cold", 0.50},
		{"unrecognized falls back to pro", "unknown", 0.50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Estimator{Pricing: pricing, Tier: tt.tier}
			if got := e.writeRatio(); got != tt.want {
				t.Errorf("writeRatio() = %f, want %f", got, tt.want)
			}
		})
	}
}

// =============================================================================
// Estimate — error paths (no pricing fixture needed)
// =============================================================================

func TestEstimate_ErrorPaths(t *testing.T) {
	t.Run("nil estimator", func(t *testing.T) {
		var e *Estimator
		_, err := e.Estimate(TaskDesc{Model: "deepseek-v4-pro", Provider: "deepseek"})
		if err == nil {
			t.Error("nil estimator should return error")
		}
	})

	t.Run("nil pricing", func(t *testing.T) {
		e := NewEstimator(nil, "pro")
		_, err := e.Estimate(TaskDesc{Model: "deepseek-v4-pro", Provider: "deepseek"})
		if err == nil {
			t.Error("nil pricing should return error")
		}
	})

	t.Run("invalid task type", func(t *testing.T) {
		pricing := &PricingYAML{
			Tasks: map[string]TaskDefaults{
				"code": {InputTokens: 120000, OutputRatio: 0.8, MaxIterations: 20},
			},
			Providers: map[string]ProviderPricing{
				"deepseek": {
					Models: map[string]ModelPrice{
						"deepseek-v4-pro": {
							InputPer1K:  0.00014,
							OutputPer1K: 0.00028,
						},
					},
				},
			},
			Cache: CacheRatios{ProHitRatio: 0.6, ProWriteRatio: 0.5},
		}
		e := NewEstimator(pricing, "pro")
		_, err := e.Estimate(TaskDesc{
			Type:     TaskType("nonexistent"),
			Model:    "deepseek-v4-pro",
			Provider: "deepseek",
		})
		if err == nil {
			t.Error("invalid task type should return error")
		}
	})

	t.Run("empty model", func(t *testing.T) {
		pricing := &PricingYAML{
			Cache: CacheRatios{ProHitRatio: 0.6, ProWriteRatio: 0.5},
		}
		e := NewEstimator(pricing, "pro")
		_, err := e.Estimate(TaskDesc{})
		if err == nil {
			t.Error("empty model should return error")
		}
	})
}

// =============================================================================
// Estimate — smoke test with real pricing fixture
// =============================================================================

func TestEstimate_Smoke(t *testing.T) {
	p, err := LoadPricing("testdata/pricing.yaml")
	if err != nil {
		t.Fatalf("LoadPricing: %v", err)
	}

	t.Run("pro tier deepseek", func(t *testing.T) {
		e := NewEstimator(p, "pro")
		est, err := e.Estimate(TaskDesc{
			Type:     TaskCode,
			Model:    "deepseek-v4-pro",
			Provider: "deepseek",
		})
		if err != nil {
			t.Fatalf("Estimate() error = %v", err)
		}
		if est.CostTotal <= 0 {
			t.Errorf("CostTotal = %f, want > 0", est.CostTotal)
		}
		if est.Model != "deepseek-v4-pro" {
			t.Errorf("Model = %q, want deepseek-v4-pro", est.Model)
		}
		if est.Tokens.TotalInput <= 0 {
			t.Errorf("TotalInput = %d, want > 0", est.Tokens.TotalInput)
		}
	})

	t.Run("flash tier deepseek", func(t *testing.T) {
		e := NewEstimator(p, "flash")
		est, err := e.Estimate(TaskDesc{
			Type:     TaskCode,
			Model:    "deepseek-v4-flash",
			Provider: "deepseek",
		})
		if err != nil {
			t.Fatalf("Estimate() error = %v", err)
		}
		if est.CostTotal <= 0 {
			t.Errorf("CostTotal = %f, want > 0", est.CostTotal)
		}
		// Flash should have a higher cache hit ratio → lower cost.
		e2 := NewEstimator(p, "pro")
		est2, _ := e2.Estimate(TaskDesc{
			Type:     TaskCode,
			Model:    "deepseek-v4-flash",
			Provider: "deepseek",
		})
		// Flash just has a higher hit ratio — same model, same pricing → lower cost.
		if est.CostTotal > est2.CostTotal {
			t.Errorf("flash CostTotal (%f) > pro (%f); flash should be cheaper via higher cache hit",
				est.CostTotal, est2.CostTotal)
		}
	})

	t.Run("cold tier — no cache history", func(t *testing.T) {
		e := NewEstimator(p, "cold")
		est, err := e.Estimate(TaskDesc{
			Type:     TaskCode,
			Model:    "deepseek-v4-pro",
			Provider: "deepseek",
		})
		if err != nil {
			t.Fatalf("Estimate() error = %v", err)
		}
		// Cold tier: NewRepoHitRatio = 0.0 → no cache discount → higher cost than pro.
		e2 := NewEstimator(p, "pro")
		est2, _ := e2.Estimate(TaskDesc{
			Type:     TaskCode,
			Model:    "deepseek-v4-pro",
			Provider: "deepseek",
		})
		// Cold has 0% cache hit → every input token is fresh → higher cost.
		if est.CostTotal < est2.CostTotal {
			t.Errorf("cold CostTotal (%f) < pro (%f); cold should be more expensive (no cache)",
				est.CostTotal, est2.CostTotal)
		}
	})

	t.Run("MiniMax no cache API", func(t *testing.T) {
		// MiniMax has cache_read_per_1k: null → IsCacheSupported = false.
		e := NewEstimator(p, "pro")
		est, err := e.Estimate(TaskDesc{
			Type:     TaskCode,
			Model:    "MiniMax-M3",
			Provider: "minimax",
		})
		if err != nil {
			t.Fatalf("Estimate() error = %v", err)
		}
		// Cache hits should be 0 for no-cache models.
		if est.Tokens.CacheHits != 0 {
			t.Errorf("MiniMax CacheHits = %d, want 0 (no cache API)", est.Tokens.CacheHits)
		}
	})

	t.Run("multiple agents", func(t *testing.T) {
		e := NewEstimator(p, "pro")
		single, _ := e.Estimate(TaskDesc{
			Type:     TaskCode,
			Model:    "deepseek-v4-pro",
			Provider: "deepseek",
			Agents:   1,
		})
		multi, _ := e.Estimate(TaskDesc{
			Type:     TaskCode,
			Model:    "deepseek-v4-pro",
			Provider: "deepseek",
			Agents:   3,
		})
		// 3 agents should be roughly 3x the single-agent cost.
		ratio := multi.CostTotal / single.CostTotal
		if ratio < 2.5 || ratio > 3.5 {
			t.Errorf("3-agent / 1-agent cost ratio = %f, want ~3.0", ratio)
		}
	})

	t.Run("agents capped at 5 (spec §3.4)", func(t *testing.T) {
		e := NewEstimator(p, "pro")
		est5, _ := e.Estimate(TaskDesc{
			Type:     TaskCode,
			Model:    "deepseek-v4-pro",
			Provider: "deepseek",
			Agents:   5,
		})
		est10, err := e.Estimate(TaskDesc{
			Type:     TaskCode,
			Model:    "deepseek-v4-pro",
			Provider: "deepseek",
			Agents:   10, // should be clamped to 5
		})
		if err != nil {
			t.Fatalf("Estimate() error = %v", err)
		}
		if est5.CostTotal != est10.CostTotal {
			t.Errorf("5-agent cost (%f) != 10-agent cost (%f); agents >5 should be capped",
				est5.CostTotal, est10.CostTotal)
		}
	})
}
