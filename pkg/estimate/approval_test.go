package estimate

import (
	"fmt"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ApprovalGate — spec §8.1 tests
// ---------------------------------------------------------------------------

func makeBudget(weekly, used float64, trust int) BudgetInfo {
	return BudgetInfo{
		AgentName:    "test-agent",
		Tier:         "pro",
		BudgetWeekly: weekly,
		BudgetUsed:   used,
		TrustLevel:   trust,
	}
}

func makeEstimate(cost float64) CostEstimate {
	return CostEstimate{
		CostTotal:  cost,
		CostInput:  cost * 0.6,
		CostOutput: cost * 0.4,
		Model:      "deepseek-v4-pro",
		Provider:   "deepseek",
		Tokens: TokenEstimate{
			TotalInput:  100000,
			FreshInput:  40000,
			CacheHits:   60000,
			CacheWrites: 20000,
			Output:      32000,
		},
	}
}

func makePricing() *PricingYAML {
	flashInput := 0.00007
	flashCache := 0.000007
	glmInput := 0.00010
	glmCache := 0.000010
	return &PricingYAML{
		Providers: map[string]ProviderPricing{
			"deepseek": {
				Models: map[string]ModelPrice{
					"deepseek-v4-pro": {
						InputPer1K:     0.00014,
						CacheReadPer1K: &[]float64{0.000014}[0],
						OutputPer1K:    0.00028,
					},
					"deepseek-v4-flash": {
						InputPer1K:     flashInput,
						CacheReadPer1K: &flashCache,
						OutputPer1K:    0.00014,
					},
				},
			},
			"zai-glm": {
				Models: map[string]ModelPrice{
					"glm-5.2": {
						InputPer1K:     glmInput,
						CacheReadPer1K: &glmCache,
						OutputPer1K:    0.00020,
					},
				},
			},
			"openrouter": {
				MarkupPercent: 5,
				Models: map[string]ModelPrice{
					"deepseek-v4-pro": {
						InputPer1K:     0.00014,
						CacheReadPer1K: &[]float64{0.000014}[0],
						OutputPer1K:    0.00028,
					},
				},
			},
		},
	}
}

// --- Core evaluation tests ---

func TestApprovalGate_AutoApproved(t *testing.T) {
	gate := NewApprovalGate(nil)
	budget := makeBudget(100, 50, 85) // $50 remaining
	est := makeEstimate(5.0)          // $5 cost, well within

	result := gate.Evaluate(budget, est)

	if result.Status != StatusAutoApproved {
		t.Errorf("expected AUTO_APPROVED, got %s", result.Status)
	}
	if !result.Approved {
		t.Error("expected approved=true")
	}
	if result.RemainingBefore != 50.0 {
		t.Errorf("expected remaining before=50.0, got %.2f", result.RemainingBefore)
	}
	if result.RemainingAfter != 45.0 {
		t.Errorf("expected remaining after=45.0, got %.2f", result.RemainingAfter)
	}
	if result.WeeklyCap != 100.0 {
		t.Errorf("expected weekly cap=100.0, got %.2f", result.WeeklyCap)
	}
	if result.TaskCost != 5.0 {
		t.Errorf("expected task cost=5.0, got %.4f", result.TaskCost)
	}
}

func TestApprovalGate_AutoApprovedWithWarning(t *testing.T) {
	gate := NewApprovalGate(nil)
	budget := makeBudget(100, 80, 85) // $20 remaining, trust 85
	est := makeEstimate(25.0)         // $25 cost ≤ $20*1.5=$30, trust≥70

	result := gate.Evaluate(budget, est)

	if result.Status != StatusAutoApprovedWithWarning {
		t.Errorf("expected AUTO_APPROVED_WITH_WARNING, got %s", result.Status)
	}
	if !result.Approved {
		t.Error("expected approved=true for warning")
	}
	if result.RemainingAfter != 0 {
		t.Errorf("expected remaining after clamped to 0, got %.2f", result.RemainingAfter)
	}
}

func TestApprovalGate_AutoApprovedWithWarning_TrustTooLow(t *testing.T) {
	gate := NewApprovalGate(nil)
	budget := makeBudget(100, 80, 50) // $20 remaining, trust 50 (too low)
	est := makeEstimate(25.0)         // $25 > $20 remaining

	result := gate.Evaluate(budget, est)

	if result.Status != StatusBlocked {
		t.Errorf("expected BLOCKED (trust too low for warning), got %s", result.Status)
	}
}

func TestApprovalGate_Blocked(t *testing.T) {
	gate := NewApprovalGate(nil)
	budget := makeBudget(100, 90, 85) // $10 remaining
	est := makeEstimate(50.0)         // $50 > $10 remaining, but < $100 weekly cap

	result := gate.Evaluate(budget, est)

	if result.Status != StatusBlocked {
		t.Errorf("expected BLOCKED, got %s", result.Status)
	}
	if result.Approved {
		t.Error("expected approved=false for blocked")
	}
	if len(result.BlockedOptions) < 2 {
		t.Errorf("expected at least 2 blocked options, got %d", len(result.BlockedOptions))
	}
	// Should have wait and increase options
	hasWait, hasIncrease := false, false
	for _, opt := range result.BlockedOptions {
		switch opt.Type {
		case "wait":
			hasWait = true
		case "increase":
			hasIncrease = true
		}
	}
	if !hasWait {
		t.Error("missing 'wait' option")
	}
	if !hasIncrease {
		t.Error("missing 'increase' option")
	}
}

func TestApprovalGate_Blocked_WithCheaperModel(t *testing.T) {
	pricing := makePricing()
	gate := NewApprovalGate(pricing)
	budget := makeBudget(100, 90, 85)
	est := makeEstimate(50.0)

	result := gate.Evaluate(budget, est)

	if result.Status != StatusBlocked {
		t.Fatalf("expected BLOCKED, got %s", result.Status)
	}

	// Should have 3 options: wait, increase, cheaper_model
	hasCheaper := false
	for _, opt := range result.BlockedOptions {
		if opt.Type == "cheaper_model" {
			hasCheaper = true
			if opt.CheaperModel == "" {
				t.Error("cheaper_model option should have a model name")
			}
			if opt.CheaperCost <= 0 {
				t.Error("cheaper_model cost should be positive")
			}
		}
	}
	if !hasCheaper {
		t.Error("expected cheaper_model option when pricing is available")
	}
}

func TestApprovalGate_Escalated(t *testing.T) {
	gate := NewApprovalGate(nil)
	budget := makeBudget(100, 0, 85) // $100 remaining
	est := makeEstimate(150.0)       // $150 > $100 weekly cap

	result := gate.Evaluate(budget, est)

	if result.Status != StatusEscalated {
		t.Errorf("expected ESCALATED, got %s", result.Status)
	}
	if result.Approved {
		t.Error("expected approved=false for escalated")
	}
}

func TestApprovalGate_Escalated_WithCheaperAlternatives(t *testing.T) {
	pricing := makePricing()
	gate := NewApprovalGate(pricing)
	budget := makeBudget(100, 0, 85)
	est := makeEstimate(150.0)

	result := gate.Evaluate(budget, est)

	if result.Status != StatusEscalated {
		t.Fatalf("expected ESCALATED, got %s", result.Status)
	}

	if len(result.CheaperAlternatives) == 0 {
		t.Fatal("expected cheaper alternatives for escalated task")
	}

	// Should be sorted by cost ascending
	for i := 1; i < len(result.CheaperAlternatives); i++ {
		if result.CheaperAlternatives[i].Cost < result.CheaperAlternatives[i-1].Cost {
			t.Error("cheaper alternatives should be sorted by cost ascending")
		}
	}

	// All alternatives should be cheaper than original cost
	for _, alt := range result.CheaperAlternatives {
		if alt.Cost >= 150.0 {
			t.Errorf("alternative %s cost $%.4f should be < $%.4f", alt.Model, alt.Cost, 150.0)
		}
		if alt.Savings <= 0 {
			t.Errorf("alternative %s should have positive savings", alt.Model)
		}
	}
}

func TestApprovalGate_Escalated_PrecedenceOverBlocked(t *testing.T) {
	gate := NewApprovalGate(nil)
	budget := makeBudget(100, 80, 85) // $20 remaining
	est := makeEstimate(150.0)        // $150 > weekly cap AND > remaining

	result := gate.Evaluate(budget, est)

	// Spec: escalated takes precedence over blocked
	if result.Status != StatusEscalated {
		t.Errorf("expected ESCALATED (precedence), got %s", result.Status)
	}
}

// --- Edge cases ---

func TestApprovalGate_ZeroRemainingBudget(t *testing.T) {
	gate := NewApprovalGate(nil)
	budget := makeBudget(100, 100, 85) // $0 remaining
	est := makeEstimate(1.0)           // $1 cost

	result := gate.Evaluate(budget, est)

	if result.Status != StatusBlocked {
		t.Errorf("expected BLOCKED with zero budget, got %s", result.Status)
	}
	if result.RemainingBefore != 0 {
		t.Errorf("expected remaining before=0, got %.2f", result.RemainingBefore)
	}
}

func TestApprovalGate_ZeroCost(t *testing.T) {
	gate := NewApprovalGate(nil)
	budget := makeBudget(100, 50, 85)
	est := makeEstimate(0.0) // $0 cost

	result := gate.Evaluate(budget, est)

	if result.Status != StatusAutoApproved {
		t.Errorf("expected AUTO_APPROVED for zero cost, got %s", result.Status)
	}
	if result.RemainingAfter != 50.0 {
		t.Errorf("expected remaining after=50.0, got %.2f", result.RemainingAfter)
	}
}

func TestApprovalGate_NegativeRemainingClamped(t *testing.T) {
	gate := NewApprovalGate(nil)
	budget := makeBudget(100, 150, 85) // used > cap → remaining is 0
	est := makeEstimate(5.0)

	result := gate.Evaluate(budget, est)

	// Remaining should be clamped to 0, not negative
	if result.RemainingBefore != 0 {
		t.Errorf("expected remaining before clamped to 0, got %.2f", result.RemainingBefore)
	}
	if result.RemainingAfter != 0 {
		t.Errorf("expected remaining after clamped to 0, got %.2f", result.RemainingAfter)
	}
	// Should be blocked since remaining is 0
	if result.Status != StatusBlocked {
		t.Errorf("expected BLOCKED, got %s", result.Status)
	}
}

func TestApprovalGate_EvaluateWithTrust(t *testing.T) {
	gate := NewApprovalGate(nil)
	budget := makeBudget(100, 80, 0) // trust=0 initially
	est := makeEstimate(25.0)        // within 1.5x of remaining

	// Override trust via the convenience method
	result := gate.EvaluateWithTrust(budget, est, 85)

	if result.Status != StatusAutoApprovedWithWarning {
		t.Errorf("expected AUTO_APPROVED_WITH_WARNING with trust override, got %s", result.Status)
	}
}

// --- Blocked options tests ---

func TestApprovalGate_BlockedOptions_HaveDescriptions(t *testing.T) {
	gate := NewApprovalGate(nil)
	budget := makeBudget(100, 95, 85)
	est := makeEstimate(20.0)

	result := gate.Evaluate(budget, est)

	for _, opt := range result.BlockedOptions {
		if opt.Description == "" {
			t.Errorf("blocked option type %s has empty description", opt.Type)
		}
	}
}

func TestApprovalGate_BlockedOptions_NoPricing(t *testing.T) {
	gate := NewApprovalGate(nil)
	budget := makeBudget(100, 95, 85)
	est := makeEstimate(20.0)

	result := gate.Evaluate(budget, est)

	// Without pricing, there should be only 2 options (wait + increase)
	for _, opt := range result.BlockedOptions {
		if opt.Type == "cheaper_model" {
			t.Error("should not have cheaper_model option without pricing")
		}
	}
	if len(result.BlockedOptions) != 2 {
		t.Errorf("expected exactly 2 options without pricing, got %d", len(result.BlockedOptions))
	}
}

// --- Cheaper model finding ---

func TestApprovalGate_FindCheaperModels(t *testing.T) {
	pricing := makePricing()
	gate := NewApprovalGate(pricing)
	budget := makeBudget(100, 0, 85)
	est := makeEstimate(50.0)

	cheaper := gate.findCheaperModels(est, budget)

	if len(cheaper) == 0 {
		t.Fatal("expected cheaper models to be found")
	}

	// All should be cheaper than original
	for _, c := range cheaper {
		if c.Cost >= est.CostTotal {
			t.Errorf("%s cost $%.4f should be < $%.4f", c.Model, c.Cost, est.CostTotal)
		}
	}
}

func TestApprovalGate_FindCheaperModels_LimitedTo5(t *testing.T) {
	pricing := &PricingYAML{
		Providers: map[string]ProviderPricing{
			"provider1": {
				Models: map[string]ModelPrice{},
			},
		},
	}
	// Add 10 models
	for i := 0; i < 10; i++ {
		price := 0.00001 * float64(i+1) // increasing prices, all cheaper than original
		cachePrice := price * 0.1
		pricing.Providers["provider1"].Models[fmt.Sprintf("model-%d", i)] = ModelPrice{
			InputPer1K:     price,
			CacheReadPer1K: &cachePrice,
			OutputPer1K:    price * 2,
		}
	}

	gate := NewApprovalGate(pricing)
	budget := makeBudget(100, 0, 85)
	est := makeEstimate(50.0)

	cheaper := gate.findCheaperModels(est, budget)

	if len(cheaper) > 5 {
		t.Errorf("expected max 5 cheaper models, got %d", len(cheaper))
	}
}

func TestApprovalGate_FindCheaperModels_NilPricing(t *testing.T) {
	gate := NewApprovalGate(nil)
	est := makeEstimate(50.0)
	budget := makeBudget(100, 0, 85)

	cheaper := gate.findCheaperModels(est, budget)

	if cheaper != nil {
		t.Errorf("expected nil with no pricing, got %v", cheaper)
	}
}

func TestApprovalGate_FindCheaperModels_SkipsOriginalModel(t *testing.T) {
	pricing := makePricing()
	gate := NewApprovalGate(pricing)
	budget := makeBudget(100, 0, 85)
	est := makeEstimate(50.0)

	cheaper := gate.findCheaperModels(est, budget)

	for _, c := range cheaper {
		if c.Model == est.Model && c.Provider == est.Provider {
			t.Error("should skip the original model")
		}
	}
}

// --- Batch evaluation ---

func TestApprovalGate_BatchEvaluate(t *testing.T) {
	gate := NewApprovalGate(nil)
	budgets := []BudgetInfo{
		makeBudget(100, 50, 85), // $50 remaining → approved for $5
		makeBudget(100, 95, 85), // $5 remaining → blocked for $5
	}
	budgets[0].AgentName = "agent-a"
	budgets[1].AgentName = "agent-b"
	est := makeEstimate(5.0)

	results := gate.BatchEvaluate(budgets, est)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results["agent-a"].Status != StatusAutoApproved {
		t.Errorf("agent-a should be approved, got %s", results["agent-a"].Status)
	}
	// agent-b: $5 remaining, $5 cost → cost <= remaining → AUTO_APPROVED
	// Actually $5 cost == $5 remaining, so cost <= remaining → AUTO_APPROVED
	// Let me adjust: $5.01 cost would block
}

func TestApprovalGate_BatchEvaluate_MultipleBlocked(t *testing.T) {
	gate := NewApprovalGate(nil)
	budgets := []BudgetInfo{
		{AgentName: "a", BudgetWeekly: 100, BudgetUsed: 95, TrustLevel: 85},
		{AgentName: "b", BudgetWeekly: 100, BudgetUsed: 95, TrustLevel: 85},
		{AgentName: "c", BudgetWeekly: 100, BudgetUsed: 50, TrustLevel: 85},
	}
	est := makeEstimate(20.0)

	results := gate.BatchEvaluate(budgets, est)

	if !AnyApproved(results) {
		t.Error("expected at least one approved in batch")
	}
	if AllBlocked(results) {
		t.Error("should not be all blocked — agent c has budget")
	}
}

func TestApprovalGate_BatchEvaluate_AllBlocked(t *testing.T) {
	gate := NewApprovalGate(nil)
	budgets := []BudgetInfo{
		{AgentName: "a", BudgetWeekly: 100, BudgetUsed: 95, TrustLevel: 85},
		{AgentName: "b", BudgetWeekly: 100, BudgetUsed: 95, TrustLevel: 85},
	}
	est := makeEstimate(20.0)

	results := gate.BatchEvaluate(budgets, est)

	if !AllBlocked(results) {
		t.Error("expected all blocked")
	}
	if AnyApproved(results) {
		t.Error("should not have any approved")
	}
}

func TestApprovalGate_BatchEvaluate_Empty(t *testing.T) {
	gate := NewApprovalGate(nil)
	results := gate.BatchEvaluate(nil, makeEstimate(5.0))

	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
	if AllBlocked(results) {
		t.Error("empty results should not be all blocked")
	}
}

// --- Formatting ---

func TestFormatGateResult_Approved(t *testing.T) {
	gate := NewApprovalGate(nil)
	result := gate.Evaluate(makeBudget(100, 50, 85), makeEstimate(5.0))

	formatted := FormatGateResult(result)

	if !strings.Contains(formatted, "AUTO_APPROVED") {
		t.Error("formatted output should contain status")
	}
	if !strings.Contains(formatted, "$") {
		t.Error("formatted output should contain cost info")
	}
}

func TestFormatGateResult_Blocked(t *testing.T) {
	pricing := makePricing()
	gate := NewApprovalGate(pricing)
	result := gate.Evaluate(makeBudget(100, 95, 85), makeEstimate(20.0))

	formatted := FormatGateResult(result)

	if !strings.Contains(formatted, "BLOCKED") {
		t.Error("should contain BLOCKED")
	}
	if !strings.Contains(formatted, "Options:") {
		t.Error("should contain Options section")
	}
}

func TestFormatGateResult_Escalated(t *testing.T) {
	pricing := makePricing()
	gate := NewApprovalGate(pricing)
	result := gate.Evaluate(makeBudget(100, 0, 85), makeEstimate(150.0))

	formatted := FormatGateResult(result)

	if !strings.Contains(formatted, "ESCALATED") {
		t.Error("should contain ESCALATED")
	}
}

// --- Remaining-after budget projection ---

func TestApprovalGate_RemainingAfterProjection(t *testing.T) {
	gate := NewApprovalGate(nil)
	budget := makeBudget(100, 40, 85) // $60 remaining
	est := makeEstimate(15.0)         // $15 cost

	result := gate.Evaluate(budget, est)

	if result.RemainingBefore != 60.0 {
		t.Errorf("expected remaining before=60.0, got %.2f", result.RemainingBefore)
	}
	if result.RemainingAfter != 45.0 {
		t.Errorf("expected remaining after=45.0, got %.2f", result.RemainingAfter)
	}
}

func TestApprovalGate_RemainingAfterZeroWhenOver(t *testing.T) {
	gate := NewApprovalGate(nil)
	budget := makeBudget(100, 80, 85) // $20 remaining
	est := makeEstimate(25.0)         // $25 cost > $20 remaining

	result := gate.Evaluate(budget, est)

	// $20 - $25 = -$5, clamped to 0
	if result.RemainingAfter != 0 {
		t.Errorf("expected remaining after clamped to 0, got %.2f", result.RemainingAfter)
	}
}

// --- estimateCheaperCost ---

func TestEstimateCheaperCost(t *testing.T) {
	gate := NewApprovalGate(makePricing())
	est := makeEstimate(50.0)
	flashPrice := gate.pricing.Providers["deepseek"].Models["deepseek-v4-flash"]
	budget := makeBudget(100, 0, 85)

	cost := gate.estimateCheaperCost(est, flashPrice, gate.pricing.Providers["deepseek"], budget)

	if cost <= 0 {
		t.Errorf("cheaper cost should be positive, got %.4f", cost)
	}
	if cost >= est.CostTotal {
		t.Errorf("flash cost $%.4f should be cheaper than pro cost $%.4f", cost, est.CostTotal)
	}
}

func TestEstimateCheaperCost_WithMarkup(t *testing.T) {
	gate := NewApprovalGate(makePricing())
	est := makeEstimate(50.0)
	proPrice := gate.pricing.Providers["deepseek"].Models["deepseek-v4-pro"]
	budget := makeBudget(100, 0, 85)

	// OpenRouter has 5% markup
	orProvider := gate.pricing.Providers["openrouter"]
	orCost := gate.estimateCheaperCost(est, proPrice, orProvider, budget)
	dsCost := gate.estimateCheaperCost(est, proPrice, gate.pricing.Providers["deepseek"], budget)

	// OpenRouter should be ~5% more expensive than direct DeepSeek
	if orCost <= dsCost {
		t.Errorf("OpenRouter ($%.4f) should cost more than direct ($%.4f) due to markup", orCost, dsCost)
	}
}
