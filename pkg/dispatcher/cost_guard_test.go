package dispatcher

import (
	"testing"

	"github.com/totalwindupflightsystems/helix/pkg/estimate"
	"github.com/totalwindupflightsystems/helix/pkg/identity"
	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

func TestNewCostGuard(t *testing.T) {
	pe := identity.NewPermissionExpansion()
	cg := NewCostGuard(pe, nil)
	if cg == nil {
		t.Fatal("CostGuard is nil")
	}
}

func TestNewCostGuard_NilPermExp(t *testing.T) {
	cg := NewCostGuard(nil, nil)
	if cg == nil {
		t.Fatal("CostGuard should create default PermissionExpansion")
	}
}

func TestCheck_ProvisionalWithinCap(t *testing.T) {
	cg := NewCostGuard(nil, nil) // no estimator → rough heuristic
	task := estimate.TaskDesc{
		Type:         estimate.TaskCode,
		FilesChanged: 1,
		MaxIterations: 5,
		DiffLines:    50,
	}

	result, err := cg.Check("agent-a", trust.TierProvisional, task)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if result.Decision != CostGuardApproved {
		t.Errorf("decision = %s, want APPROVED", result.Decision)
	}
	if result.CostCapPerJob != 5.0 {
		t.Errorf("cap = %.0f, want 5.0", result.CostCapPerJob)
	}
}

func TestCheck_ProvisionalExceedsCap(t *testing.T) {
	cg := NewCostGuard(nil, nil)
	// Rough heuristic: filesChanged*500 + maxIter*200 + diffLines*10 tokens
	// $0.001 per 1K tokens → need > 5M tokens to exceed $5
	// 5000 files * 500 + 500 * 200 + 500000 * 10 = 2.5M + 100K + 5M = 7.6M tokens → $7.60
	task := estimate.TaskDesc{
		Type:          estimate.TaskCode,
		FilesChanged:  5000,
		MaxIterations: 500,
		DiffLines:     500000,
	}

	result, err := cg.Check("agent-a", trust.TierProvisional, task)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if result.Decision != CostGuardBlocked {
		t.Errorf("decision = %s, want BLOCKED", result.Decision)
	}
	if result.CostCapPerJob != 5.0 {
		t.Errorf("cap = %.0f, want 5.0", result.CostCapPerJob)
	}
}

func TestCheck_VeteranUnlimitedCap(t *testing.T) {
	cg := NewCostGuard(nil, nil)
	task := estimate.TaskDesc{
		Type:          estimate.TaskCode,
		FilesChanged:  1000,
		MaxIterations: 500,
		DiffLines:     50000,
	}

	result, err := cg.Check("agent-a", trust.TierVeteran, task)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if result.Decision != CostGuardApproved {
		t.Errorf("decision = %s, want APPROVED (veteran has unlimited cap)", result.Decision)
	}
	if result.CostCapPerJob != -1 {
		t.Errorf("cap = %.0f, want -1 (unlimited)", result.CostCapPerJob)
	}
}

func TestCheck_ObservedWithinCap(t *testing.T) {
	cg := NewCostGuard(nil, nil)
	task := estimate.TaskDesc{
		Type:          estimate.TaskCode,
		FilesChanged:  10,
		MaxIterations: 20,
		DiffLines:     500,
	}

	result, err := cg.Check("agent-a", trust.TierObserved, task)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if result.Decision != CostGuardApproved {
		t.Errorf("decision = %s, want APPROVED", result.Decision)
	}
	if result.CostCapPerJob != 25.0 {
		t.Errorf("cap = %.0f, want 25.0", result.CostCapPerJob)
	}
}

func TestCheck_TrustedWithinCap(t *testing.T) {
	cg := NewCostGuard(nil, nil)
	task := estimate.TaskDesc{
		Type:          estimate.TaskCode,
		FilesChanged:  30,
		MaxIterations: 50,
		DiffLines:     2000,
	}

	result, err := cg.Check("agent-a", trust.TierTrusted, task)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if result.Decision != CostGuardApproved {
		t.Errorf("decision = %s, want APPROVED", result.Decision)
	}
	if result.CostCapPerJob != 100.0 {
		t.Errorf("cap = %.0f, want 100.0", result.CostCapPerJob)
	}
}

func TestCheck_InvalidTier(t *testing.T) {
	cg := NewCostGuard(nil, nil)
	task := estimate.TaskDesc{Type: estimate.TaskCode}

	_, err := cg.Check("agent-a", trust.TrustTier("bogus"), task)
	if err == nil {
		t.Error("expected error for invalid tier")
	}
}

func TestCheckWithEstimate_ProvisionalBlocked(t *testing.T) {
	cg := NewCostGuard(nil, nil)
	est := estimate.CostEstimate{
		CostTotal: 10.0, // exceeds $5 cap
	}

	result, err := cg.CheckWithEstimate("agent-a", trust.TierProvisional, est)
	if err != nil {
		t.Fatalf("CheckWithEstimate: %v", err)
	}

	if result.Decision != CostGuardBlocked {
		t.Errorf("decision = %s, want BLOCKED", result.Decision)
	}
	if result.EstimatedCost != 10.0 {
		t.Errorf("estimated cost = %.2f, want 10.0", result.EstimatedCost)
	}
}

func TestCheckWithEstimate_VeteranApproved(t *testing.T) {
	cg := NewCostGuard(nil, nil)
	est := estimate.CostEstimate{
		CostTotal: 500.0, // would exceed any cap, but veteran is unlimited
	}

	result, err := cg.CheckWithEstimate("agent-a", trust.TierVeteran, est)
	if err != nil {
		t.Fatalf("CheckWithEstimate: %v", err)
	}

	if result.Decision != CostGuardApproved {
		t.Errorf("decision = %s, want APPROVED (unlimited)", result.Decision)
	}
}

func TestCheckWithEstimate_ApproachingLimit(t *testing.T) {
	cg := NewCostGuard(nil, nil)
	// Provisional cap is $5; $4.50 is 90% → within 80% threshold
	est := estimate.CostEstimate{CostTotal: 4.50}

	result, err := cg.CheckWithEstimate("agent-a", trust.TierProvisional, est)
	if err != nil {
		t.Fatalf("CheckWithEstimate: %v", err)
	}

	if result.Decision != CostGuardApproved {
		t.Errorf("decision = %s, want APPROVED", result.Decision)
	}
	if result.Reason == "" {
		t.Error("reason should mention approaching limit")
	}
}

func TestCheckWithEstimate_WellWithinCap(t *testing.T) {
	cg := NewCostGuard(nil, nil)
	est := estimate.CostEstimate{CostTotal: 2.0}

	result, err := cg.CheckWithEstimate("agent-a", trust.TierProvisional, est)
	if err != nil {
		t.Fatalf("CheckWithEstimate: %v", err)
	}

	if result.Decision != CostGuardApproved {
		t.Errorf("decision = %s, want APPROVED", result.Decision)
	}
}

func TestCheckWithEstimate_InvalidTier(t *testing.T) {
	cg := NewCostGuard(nil, nil)
	est := estimate.CostEstimate{CostTotal: 1.0}

	_, err := cg.CheckWithEstimate("agent-a", trust.TrustTier("bogus"), est)
	if err == nil {
		t.Error("expected error for invalid tier")
	}
}

func TestCostGuardResult_IsApproved(t *testing.T) {
	r := CostGuardResult{Decision: CostGuardApproved}
	if !r.IsApproved() {
		t.Error("APPROVED should be approved")
	}
	if r.IsBlocked() {
		t.Error("APPROVED should not be blocked")
	}
	if r.IsEscalated() {
		t.Error("APPROVED should not be escalated")
	}
}

func TestCostGuardResult_IsBlocked(t *testing.T) {
	r := CostGuardResult{Decision: CostGuardBlocked}
	if !r.IsBlocked() {
		t.Error("BLOCKED should be blocked")
	}
	if r.IsApproved() {
		t.Error("BLOCKED should not be approved")
	}
}

func TestCostGuardResult_IsEscalated(t *testing.T) {
	r := CostGuardResult{Decision: CostGuardEscalated}
	if !r.IsEscalated() {
		t.Error("ESCALATED should be escalated")
	}
	if r.IsApproved() {
		t.Error("ESCALATED should not be approved")
	}
}

func TestCheck_TierEscalationProgression(t *testing.T) {
	// Same task, different tiers — should be blocked for low tiers,
	// approved for high tiers.
	cg := NewCostGuard(nil, nil)
	task := estimate.TaskDesc{
		Type:          estimate.TaskCode,
		FilesChanged:  20,
		MaxIterations: 30,
		DiffLines:     1000,
	}

	tiers := []trust.TrustTier{trust.TierProvisional, trust.TierObserved, trust.TierTrusted, trust.TierVeteran}
	for _, tier := range tiers {
		result, err := cg.Check("agent-a", tier, task)
		if err != nil {
			t.Fatalf("Check(%s): %v", tier, err)
		}
		// Each tier has progressively higher caps, so at minimum the veteran should pass.
		if tier == trust.TierVeteran && !result.IsApproved() {
			t.Errorf("veteran should always be approved (unlimited cap), got %s", result.Decision)
		}
	}
}

func TestCheckWithEstimate_ExactCapBoundary(t *testing.T) {
	cg := NewCostGuard(nil, nil)

	// Observed cap is $25. Cost of exactly $25 should be approved.
	est := estimate.CostEstimate{CostTotal: 25.0}
	result, err := cg.CheckWithEstimate("agent-a", trust.TierObserved, est)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !result.IsApproved() {
		t.Errorf("cost at cap boundary should be approved, got %s", result.Decision)
	}

	// Cost of $25.01 should be blocked.
	est2 := estimate.CostEstimate{CostTotal: 25.01}
	result2, err := cg.CheckWithEstimate("agent-a", trust.TierObserved, est2)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !result2.IsBlocked() {
		t.Errorf("cost above cap should be blocked, got %s", result2.Decision)
	}
}

func TestCheckWithEstimate_EightyPercentThreshold(t *testing.T) {
	cg := NewCostGuard(nil, nil)

	// Observed cap is $25. 80% = $20. Cost of $20.01 should be in warn zone.
	est := estimate.CostEstimate{CostTotal: 20.01}
	result, err := cg.CheckWithEstimate("agent-a", trust.TierObserved, est)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !result.IsApproved() {
		t.Errorf("80%% zone should still be approved, got %s", result.Decision)
	}

	// Cost of $19.99 should be normal approved (below 80%).
	est2 := estimate.CostEstimate{CostTotal: 19.99}
	result2, _ := cg.CheckWithEstimate("agent-a", trust.TierObserved, est2)
	if !result2.IsApproved() {
		t.Errorf("below 80%% threshold should be approved, got %s", result2.Decision)
	}
}
