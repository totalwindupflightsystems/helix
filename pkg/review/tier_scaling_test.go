package review

import (
	"testing"

	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

func TestNewTierScaling(t *testing.T) {
	ts := NewTierScaling()
	if ts == nil {
		t.Fatal("TierScaling is nil")
	}
}

func TestPolicyForTier_Provisional(t *testing.T) {
	ts := NewTierScaling()
	policy, err := ts.PolicyForTier(trust.TierProvisional)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Spec §Tier 0: full 3-model adversarial + all prosecutor agents + 100% verification
	if policy.PanelSize != 3 {
		t.Errorf("panel size = %d, want 3", policy.PanelSize)
	}
	if policy.ConsensusThreshold != 2 {
		t.Errorf("consensus threshold = %d, want 2 (2/3rds)", policy.ConsensusThreshold)
	}
	if !policy.RequireProsecutorAgents {
		t.Error("provisional should require prosecutor agents")
	}
	if policy.EvidenceVerificationPct != 100 {
		t.Errorf("evidence verification = %d%%, want 100%%", policy.EvidenceVerificationPct)
	}
	if policy.RequireProviderDiversity != 2 {
		t.Errorf("provider diversity = %d, want 2", policy.RequireProviderDiversity)
	}
}

func TestPolicyForTier_Observed(t *testing.T) {
	ts := NewTierScaling()
	policy, err := ts.PolicyForTier(trust.TierObserved)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Spec §Tier 1: 2-model + prosecutor agents for contract changes
	if policy.PanelSize != 2 {
		t.Errorf("panel size = %d, want 2", policy.PanelSize)
	}
	if policy.ConsensusThreshold != 2 {
		t.Errorf("consensus threshold = %d, want 2 (simple majority)", policy.ConsensusThreshold)
	}
	if !policy.RequireProsecutorAgents {
		t.Error("observed should require prosecutor agents")
	}
}

func TestPolicyForTier_Trusted(t *testing.T) {
	ts := NewTierScaling()
	policy, err := ts.PolicyForTier(trust.TierTrusted)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Spec §Tier 2: single-model + spot-check verification
	if policy.PanelSize != 1 {
		t.Errorf("panel size = %d, want 1", policy.PanelSize)
	}
	if policy.ConsensusThreshold != 1 {
		t.Errorf("consensus threshold = %d, want 1", policy.ConsensusThreshold)
	}
	if policy.RequireProsecutorAgents {
		t.Error("trusted should NOT require prosecutor agents (except contract)")
	}
	if policy.EvidenceVerificationPct != 25 {
		t.Errorf("evidence verification = %d%%, want 25%% (spot-check)", policy.EvidenceVerificationPct)
	}
}

func TestPolicyForTier_Veteran(t *testing.T) {
	ts := NewTierScaling()
	policy, err := ts.PolicyForTier(trust.TierVeteran)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Spec §Tier 3: single-model review
	if policy.PanelSize != 1 {
		t.Errorf("panel size = %d, want 1", policy.PanelSize)
	}
	if policy.ConsensusThreshold != 1 {
		t.Errorf("consensus threshold = %d, want 1", policy.ConsensusThreshold)
	}
	if policy.RequireProsecutorAgents {
		t.Error("veteran should NOT require prosecutor agents")
	}
	if policy.EvidenceVerificationPct != 0 {
		t.Errorf("evidence verification = %d%%, want 0%%", policy.EvidenceVerificationPct)
	}
}

func TestPolicyForTier_Unknown(t *testing.T) {
	ts := NewTierScaling()
	_, err := ts.PolicyForTier(trust.TrustTier("bogus"))
	if err == nil {
		t.Error("expected error for unknown tier")
	}
}

func TestPolicyForTier_MonotonicReduction(t *testing.T) {
	ts := NewTierScaling()
	tiers := []trust.TrustTier{trust.TierProvisional, trust.TierObserved, trust.TierTrusted, trust.TierVeteran}
	policies := make([]TierReviewPolicy, len(tiers))

	for i, tier := range tiers {
		p, err := ts.PolicyForTier(tier)
		if err != nil {
			t.Fatalf("PolicyForTier(%s): %v", tier, err)
		}
		policies[i] = p
	}

	// Panel size should be non-increasing across tiers.
	for i := 1; i < len(policies); i++ {
		if policies[i].PanelSize > policies[i-1].PanelSize {
			t.Errorf("tier %d panel size %d > tier %d panel size %d (should be non-increasing)",
				i, policies[i].PanelSize, i-1, policies[i-1].PanelSize)
		}
	}

	// Evidence verification should be non-increasing.
	for i := 1; i < len(policies); i++ {
		if policies[i].EvidenceVerificationPct > policies[i-1].EvidenceVerificationPct {
			t.Errorf("tier %d evidence %d%% > tier %d evidence %d%% (should be non-increasing)",
				i, policies[i].EvidenceVerificationPct, i-1, policies[i-1].EvidenceVerificationPct)
		}
	}
}

func TestAdjustFormation_ContractProvisional(t *testing.T) {
	ts := NewTierScaling()
	size, err := ts.AdjustFormation(CategoryContract, trust.TierProvisional)
	if err != nil {
		t.Fatalf("AdjustFormation: %v", err)
	}
	// Contract = 3, Provisional = 3 → 3
	if size != 3 {
		t.Errorf("formation = %d, want 3", size)
	}
}

func TestAdjustFormation_ContractTrusted(t *testing.T) {
	ts := NewTierScaling()
	size, err := ts.AdjustFormation(CategoryContract, trust.TierTrusted)
	if err != nil {
		t.Fatalf("AdjustFormation: %v", err)
	}
	// Contract = 3, Trusted = 1 → 1 (tier reduces)
	if size != 1 {
		t.Errorf("formation = %d, want 1 (tier reduces)", size)
	}
}

func TestAdjustFormation_CosmeticProvisional(t *testing.T) {
	ts := NewTierScaling()
	size, err := ts.AdjustFormation(CategoryCosmetic, trust.TierProvisional)
	if err != nil {
		t.Fatalf("AdjustFormation: %v", err)
	}
	// Cosmetic = 1, Provisional = 3 → 1 (category wins)
	if size != 1 {
		t.Errorf("formation = %d, want 1 (category wins)", size)
	}
}

func TestAdjustFormation_BehavioralObserved(t *testing.T) {
	ts := NewTierScaling()
	size, err := ts.AdjustFormation(CategoryBehavioral, trust.TierObserved)
	if err != nil {
		t.Fatalf("AdjustFormation: %v", err)
	}
	// Behavioral = 2, Observed = 2 → 2
	if size != 2 {
		t.Errorf("formation = %d, want 2", size)
	}
}

func TestAdjustFormation_InvalidTier(t *testing.T) {
	ts := NewTierScaling()
	_, err := ts.AdjustFormation(CategoryContract, trust.TrustTier("bogus"))
	if err == nil {
		t.Error("expected error for invalid tier")
	}
}

func TestAdjustConsensusThreshold(t *testing.T) {
	ts := NewTierScaling()

	tests := []struct {
		cat  ChangeCategory
		tier trust.TrustTier
		want int
	}{
		{CategoryContract, trust.TierProvisional, 2},
		{CategoryContract, trust.TierObserved, 2},
		{CategoryContract, trust.TierTrusted, 1},
		{CategoryContract, trust.TierVeteran, 1},
		{CategoryBehavioral, trust.TierProvisional, 2},
		{CategoryBehavioral, trust.TierTrusted, 1},
		{CategoryResilience, trust.TierProvisional, 1},
		{CategoryCosmetic, trust.TierProvisional, 1},
	}

	for _, tc := range tests {
		got, err := ts.AdjustConsensusThreshold(tc.cat, tc.tier)
		if err != nil {
			t.Fatalf("AdjustConsensusThreshold(%s, %s): %v", tc.cat, tc.tier, err)
		}
		if got != tc.want {
			t.Errorf("AdjustConsensusThreshold(%s, %s) = %d, want %d", tc.cat, tc.tier, got, tc.want)
		}
	}
}

func TestShouldVerifyEvidence(t *testing.T) {
	ts := NewTierScaling()

	tests := []struct {
		tier trust.TrustTier
		want bool
	}{
		{trust.TierProvisional, true},
		{trust.TierObserved, true},
		{trust.TierTrusted, true}, // spot-check (25%)
		{trust.TierVeteran, false},
	}

	for _, tc := range tests {
		got, err := ts.ShouldVerifyEvidence(tc.tier)
		if err != nil {
			t.Fatalf("ShouldVerifyEvidence(%s): %v", tc.tier, err)
		}
		if got != tc.want {
			t.Errorf("ShouldVerifyEvidence(%s) = %v, want %v", tc.tier, got, tc.want)
		}
	}
}

func TestShouldDispatchProsecutors(t *testing.T) {
	ts := NewTierScaling()

	tests := []struct {
		cat  ChangeCategory
		tier trust.TrustTier
		want bool
	}{
		// Provisional: prosecutors for all non-cosmetic
		{CategoryContract, trust.TierProvisional, true},
		{CategoryBehavioral, trust.TierProvisional, true},
		{CategoryResilience, trust.TierProvisional, true},
		{CategoryCosmetic, trust.TierProvisional, false},

		// Observed: prosecutors for all non-cosmetic
		{CategoryContract, trust.TierObserved, true},
		{CategoryBehavioral, trust.TierObserved, true},
		{CategoryCosmetic, trust.TierObserved, false},

		// Trusted: prosecutors only for contract
		{CategoryContract, trust.TierTrusted, true},
		{CategoryBehavioral, trust.TierTrusted, false},
		{CategoryCosmetic, trust.TierTrusted, false},

		// Veteran: prosecutors only for contract
		{CategoryContract, trust.TierVeteran, true},
		{CategoryBehavioral, trust.TierVeteran, false},
		{CategoryCosmetic, trust.TierVeteran, false},
	}

	for _, tc := range tests {
		got, err := ts.ShouldDispatchProsecutors(tc.cat, tc.tier)
		if err != nil {
			t.Fatalf("ShouldDispatchProsecutors(%s, %s): %v", tc.cat, tc.tier, err)
		}
		if got != tc.want {
			t.Errorf("ShouldDispatchProsecutors(%s, %s) = %v, want %v", tc.cat, tc.tier, got, tc.want)
		}
	}
}

func TestReviewDepthSummary(t *testing.T) {
	ts := NewTierScaling()

	tests := []trust.TrustTier{
		trust.TierProvisional,
		trust.TierObserved,
		trust.TierTrusted,
		trust.TierVeteran,
	}

	for _, tier := range tests {
		s, err := ts.ReviewDepthSummary(tier)
		if err != nil {
			t.Fatalf("ReviewDepthSummary(%s): %v", tier, err)
		}
		if s == "" {
			t.Errorf("ReviewDepthSummary(%s) should not be empty", tier)
		}
	}
}

func TestReviewDepthSummary_InvalidTier(t *testing.T) {
	ts := NewTierScaling()
	_, err := ts.ReviewDepthSummary(trust.TrustTier("bogus"))
	if err == nil {
		t.Error("expected error for invalid tier")
	}
}
