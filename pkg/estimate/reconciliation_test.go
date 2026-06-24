package estimate

import (
	"errors"
	"math"
	"testing"
)

// ---------------------------------------------------------------------------
// NewOpenRouterClient
// ---------------------------------------------------------------------------

func TestNewOpenRouterClient(t *testing.T) {
	t.Run("default base URL when empty", func(t *testing.T) {
		c := NewOpenRouterClient("")
		if c == nil {
			t.Fatal("NewOpenRouterClient() = nil")
		}
		if c.BaseURL != "https://openrouter.ai" {
			t.Errorf("BaseURL = %q, want %q", c.BaseURL, "https://openrouter.ai")
		}
	})

	t.Run("custom base URL", func(t *testing.T) {
		c := NewOpenRouterClient("https://custom-gateway.example.com")
		if c.BaseURL != "https://custom-gateway.example.com" {
			t.Errorf("BaseURL = %q, want %q", c.BaseURL, "https://custom-gateway.example.com")
		}
	})
}

// ---------------------------------------------------------------------------
// GetKeyUsage / GetKeyLimit (ErrNotImplemented stubs)
// ---------------------------------------------------------------------------

func TestOpenRouterClient_GetKeyUsage(t *testing.T) {
	c := NewOpenRouterClient("")
	v, err := c.GetKeyUsage("sk-test")
	if v != 0 {
		t.Errorf("GetKeyUsage() value = %v, want 0", v)
	}
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("GetKeyUsage() error = %v, want ErrNotImplemented", err)
	}
}

func TestOpenRouterClient_GetKeyLimit(t *testing.T) {
	c := NewOpenRouterClient("")
	v, err := c.GetKeyLimit("sk-test")
	if v != 0 {
		t.Errorf("GetKeyLimit() value = %v, want 0", v)
	}
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("GetKeyLimit() error = %v, want ErrNotImplemented", err)
	}
}

// ---------------------------------------------------------------------------
// ReconcileDrift
// ---------------------------------------------------------------------------

func TestReconcileDrift(t *testing.T) {
	t.Run("positive drift (actual > estimated)", func(t *testing.T) {
		estimated := CostEstimate{CostTotal: 1.00}
		actual := CostEstimate{CostTotal: 1.25}
		drift := ReconcileDrift(estimated, actual)
		if drift != 25.0 {
			t.Errorf("ReconcileDrift() = %v, want 25.0", drift)
		}
	})

	t.Run("negative drift (actual < estimated)", func(t *testing.T) {
		estimated := CostEstimate{CostTotal: 2.00}
		actual := CostEstimate{CostTotal: 1.80}
		drift := ReconcileDrift(estimated, actual)
		// Floating point: (1.80 - 2.00) / 2.00 * 100 ≈ -10.0
		const epsilon = 1e-9
		if drift > -10.0+epsilon || drift < -10.0-epsilon {
			t.Errorf("ReconcileDrift() = %v, want -10.0", drift)
		}
	})

	t.Run("exact match", func(t *testing.T) {
		estimated := CostEstimate{CostTotal: 0.50}
		actual := CostEstimate{CostTotal: 0.50}
		drift := ReconcileDrift(estimated, actual)
		if drift != 0 {
			t.Errorf("ReconcileDrift() = %v, want 0", drift)
		}
	})

	t.Run("zero estimated, zero actual", func(t *testing.T) {
		estimated := CostEstimate{CostTotal: 0}
		actual := CostEstimate{CostTotal: 0}
		drift := ReconcileDrift(estimated, actual)
		if drift != 0 {
			t.Errorf("ReconcileDrift() = %v, want 0", drift)
		}
	})

	t.Run("zero estimated, non-zero actual returns +Inf", func(t *testing.T) {
		estimated := CostEstimate{CostTotal: 0}
		actual := CostEstimate{CostTotal: 0.05}
		drift := ReconcileDrift(estimated, actual)
		if !math.IsInf(drift, 1) {
			t.Errorf("ReconcileDrift() = %v, want +Inf", drift)
		}
	})

	t.Run("large drift", func(t *testing.T) {
		estimated := CostEstimate{CostTotal: 0.10}
		actual := CostEstimate{CostTotal: 1.00}
		drift := ReconcileDrift(estimated, actual)
		if drift != 900.0 {
			t.Errorf("ReconcileDrift() = %v, want 900.0", drift)
		}
	})
}

// ---------------------------------------------------------------------------
// ActualCost
// ---------------------------------------------------------------------------

func TestActualCost(t *testing.T) {
	// Load the real pricing fixture.
	pricing, err := LoadPricing("testdata/pricing.yaml")
	if err != nil {
		t.Fatalf("LoadPricing failed: %v", err)
	}

	t.Run("nil pricing returns error", func(t *testing.T) {
		_, err := ActualCost(Usage{}, nil, "deepseek", "deepseek-v4-pro")
		if err == nil {
			t.Fatal("ActualCost() with nil pricing = nil, want error")
		}
	})

	t.Run("unknown model returns error", func(t *testing.T) {
		_, err := ActualCost(Usage{}, pricing, "unknown", "no-such-model")
		if err == nil {
			t.Fatal("ActualCost() with unknown model = nil, want error")
		}
	})

	t.Run("computes cost from usage", func(t *testing.T) {
		usage := Usage{
			PromptTokens:     10000,
			CompletionTokens: 5000,
			CacheReadTokens:  2000,
			CacheWriteTokens: 1000,
			TotalTokens:      15000,
		}
		cost, err := ActualCost(usage, pricing, "deepseek", "deepseek-v4-pro")
		if err != nil {
			t.Fatalf("ActualCost() error: %v", err)
		}
		// Verify it's non-zero and all fields populated.
		if cost.CostTotal <= 0 {
			t.Errorf("CostTotal = %v, want > 0", cost.CostTotal)
		}
		if cost.Tokens.FreshInput != 8000 {
			t.Errorf("FreshInput = %d, want 8000 (10000 - 2000)", cost.Tokens.FreshInput)
		}
		if cost.Tokens.CacheHits != 2000 {
			t.Errorf("CacheHits = %d, want 2000", cost.Tokens.CacheHits)
		}
		if cost.Tokens.CacheWrites != 1000 {
			t.Errorf("CacheWrites = %d, want 1000", cost.Tokens.CacheWrites)
		}
		if cost.Tokens.Output != 5000 {
			t.Errorf("Output = %d, want 5000", cost.Tokens.Output)
		}
		if cost.Model != "deepseek-v4-pro" {
			t.Errorf("Model = %q, want %q", cost.Model, "deepseek-v4-pro")
		}
		if cost.Provider != "deepseek" {
			t.Errorf("Provider = %q, want %q", cost.Provider, "deepseek")
		}
	})

	t.Run("negative tokens clamped to zero", func(t *testing.T) {
		usage := Usage{
			PromptTokens:     100,
			CompletionTokens: 50,
			CacheReadTokens:  200, // more cache reads than prompts
		}
		cost, err := ActualCost(usage, pricing, "deepseek", "deepseek-v4-flash")
		if err != nil {
			t.Fatalf("ActualCost() error: %v", err)
		}
		if cost.Tokens.FreshInput != 0 {
			t.Errorf("FreshInput = %d, want 0 (clamped from negative)", cost.Tokens.FreshInput)
		}
	})

	t.Run("zero usage returns zero cost", func(t *testing.T) {
		cost, err := ActualCost(Usage{}, pricing, "deepseek", "deepseek-v4-pro")
		if err != nil {
			t.Fatalf("ActualCost() error: %v", err)
		}
		if cost.CostTotal != 0 {
			t.Errorf("CostTotal = %v, want 0", cost.CostTotal)
		}
		if cost.CostInput != 0 {
			t.Errorf("CostInput = %v, want 0", cost.CostInput)
		}
		if cost.CostOutput != 0 {
			t.Errorf("CostOutput = %v, want 0", cost.CostOutput)
		}
	})

	t.Run("cache hit ratio computed", func(t *testing.T) {
		usage := Usage{
			PromptTokens:    10000,
			CacheReadTokens: 5000,
		}
		cost, err := ActualCost(usage, pricing, "deepseek", "deepseek-v4-pro")
		if err != nil {
			t.Fatalf("ActualCost() error: %v", err)
		}
		if cost.Tokens.CacheHitRatio != 0.5 {
			t.Errorf("CacheHitRatio = %v, want 0.5", cost.Tokens.CacheHitRatio)
		}
	})
}
