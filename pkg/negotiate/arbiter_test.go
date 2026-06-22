package negotiate

import (
	"math"
	"testing"
)

// =============================================================================
// SplitCost tests
// =============================================================================

func TestSplitCost_EvenSplit(t *testing.T) {
	a, b := SplitCost(100.0)
	if a != 50.0 {
		t.Errorf("agentA share = %f, want 50.0", a)
	}
	if b != 50.0 {
		t.Errorf("agentB share = %f, want 50.0", b)
	}
	if a+b != 100.0 {
		t.Errorf("sum of shares = %f, want 100.0", a+b)
	}
}

func TestSplitCost_OddNumber(t *testing.T) {
	// 33 split evenly gives 16.5 to each.
	a, b := SplitCost(33.0)
	if a != 16.5 {
		t.Errorf("agentA share = %f, want 16.5", a)
	}
	if b != 16.5 {
		t.Errorf("agentB share = %f, want 16.5", b)
	}
	if a+b != 33.0 {
		t.Errorf("sum of shares = %f, want 33.0", a+b)
	}
}

func TestSplitCost_ZeroCost(t *testing.T) {
	a, b := SplitCost(0.0)
	if a != 0.0 {
		t.Errorf("agentA share = %f, want 0.0", a)
	}
	if b != 0.0 {
		t.Errorf("agentB share = %f, want 0.0", b)
	}
}

func TestSplitCost_SmallCost(t *testing.T) {
	a, b := SplitCost(0.01)
	if math.Abs(a-0.005) > 1e-10 {
		t.Errorf("agentA share = %f, want 0.005", a)
	}
	if math.Abs(b-0.005) > 1e-10 {
		t.Errorf("agentB share = %f, want 0.005", b)
	}
}

func TestSplitCost_LargeCost(t *testing.T) {
	a, b := SplitCost(1_000_000.0)
	if a != 500_000.0 {
		t.Errorf("agentA share = %f, want 500000.0", a)
	}
	if b != 500_000.0 {
		t.Errorf("agentB share = %f, want 500000.0", b)
	}
}

func TestSplitCost_FloatingPointPrecision(t *testing.T) {
	// 0.1 / 2 = 0.05 — floating point should be exact.
	a, b := SplitCost(0.1)
	if a != 0.05 {
		t.Errorf("agentA share = %f, want 0.05", a)
	}
	if b != 0.05 {
		t.Errorf("agentB share = %f, want 0.05", b)
	}
}

func TestSplitCost_SharesAlwaysEqual(t *testing.T) {
	tests := []float64{0.0, 1.0, 3.0, 7.77, 100.0, 999.99}
	for _, cost := range tests {
		a, b := SplitCost(cost)
		if a != b {
			t.Errorf("SplitCost(%f): shares not equal: a=%f, b=%f", cost, a, b)
		}
	}
}

func TestSplitCost_SumEqualsCost(t *testing.T) {
	tests := []float64{0.0, 1.0, 10.5, 99.99, 1000.0, 0.03}
	for _, cost := range tests {
		a, b := SplitCost(cost)
		sum := a + b
		if math.Abs(sum-cost) > 1e-9 {
			t.Errorf("SplitCost(%f): sum=%f, want %f", cost, sum, cost)
		}
	}
}

// =============================================================================
// estimateArbiterCost tests
// =============================================================================

func TestEstimateArbiterCost_ZeroTokens(t *testing.T) {
	cost := estimateArbiterCost(0)
	if cost != 0.0 {
		t.Errorf("cost = %f, want 0.0", cost)
	}
}

func TestEstimateArbiterCost_MillionTokens(t *testing.T) {
	// 1M tokens × $0.32/M = $0.32
	cost := estimateArbiterCost(1_000_000)
	if math.Abs(cost-0.32) > 1e-9 {
		t.Errorf("cost = %f, want 0.32", cost)
	}
}

func TestEstimateArbiterCost_SmallTokenCount(t *testing.T) {
	cost := estimateArbiterCost(100)
	expected := float64(100) * 0.00000032
	if math.Abs(cost-expected) > 1e-12 {
		t.Errorf("cost = %f, want %f", cost, expected)
	}
}

func TestEstimateArbiterCost_LargeTokenCount(t *testing.T) {
	cost := estimateArbiterCost(10_000_000)
	expected := float64(10_000_000) * 0.00000032
	if math.Abs(cost-expected) > 1e-9 {
		t.Errorf("cost = %f, want %f", cost, expected)
	}
}

// =============================================================================
// NewArbiterClient tests
// =============================================================================

func TestNewArbiterClient_SetsBaseURL(t *testing.T) {
	client := NewArbiterClient("http://localhost:8765")
	if client == nil {
		t.Fatal("expected non-nil ArbiterClient")
	}
	if client.BaseURL != "http://localhost:8765" {
		t.Errorf("BaseURL = %q, want %q", client.BaseURL, "http://localhost:8765")
	}
	if client.Client == nil {
		t.Error("expected non-nil http.Client")
	}
	if client.Client.Timeout == 0 {
		t.Error("expected non-zero timeout on http.Client")
	}
}

func TestNewArbiterClient_CustomURL(t *testing.T) {
	client := NewArbiterClient("https://chimera.example.com:443")
	if client.BaseURL != "https://chimera.example.com:443" {
		t.Errorf("BaseURL = %q, want %q", client.BaseURL, "https://chimera.example.com:443")
	}
}
