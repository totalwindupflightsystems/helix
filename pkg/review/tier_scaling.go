package review

import (
	"fmt"

	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

// TierReviewPolicy maps trust tiers to review formation requirements.
// Higher trust tiers get lighter review; lower tiers get full adversarial
// treatment.
//
// Spec: specs/trust-model.md §Integration Points:
// "Chimera multi-model review: Review depth and model count scale inversely
// with trust tier"
//
// Spec: specs/trust-model.md §Trust Tiers:
//   - Provisional: full 3-model adversarial + all prosecutor agents + 100%
//     evidence verification
//   - Observed: 2-model + prosecutor agents
//   - Trusted: single-model + spot-check verification
//   - Veteran: single-model review
type TierReviewPolicy struct {
	Tier trust.TrustTier

	// PanelSize is the number of models in the review panel.
	PanelSize int

	// ConsensusThreshold is the minimum models that must agree for approval.
	ConsensusThreshold int

	// RequireProsecutorAgents controls whether adversarial agents
	// (@assumption-buster, @redteam, @chaos-engineer, @cost-auditor)
	// are dispatched.
	RequireProsecutorAgents bool

	// EvidenceVerificationPct is the percentage of findings that must be
	// independently verified (0-100).
	EvidenceVerificationPct int

	// RequireProviderDiversity is the minimum number of distinct providers.
	RequireProviderDiversity int
}

// TierScaling provides tier-to-review-policy mapping.
type TierScaling struct{}

// NewTierScaling creates a TierScaling.
func NewTierScaling() *TierScaling {
	return &TierScaling{}
}

// PolicyForTier returns the review policy for a given trust tier.
func (ts *TierScaling) PolicyForTier(tier trust.TrustTier) (TierReviewPolicy, error) {
	switch tier {
	case trust.TierProvisional:
		return provisionalReviewPolicy(), nil
	case trust.TierObserved:
		return observedReviewPolicy(), nil
	case trust.TierTrusted:
		return trustedReviewPolicy(), nil
	case trust.TierVeteran:
		return veteranReviewPolicy(), nil
	default:
		return TierReviewPolicy{}, fmt.Errorf("unknown trust tier: %s", tier)
	}
}

// provisionalReviewPolicy: full adversarial, 3 models, 2/3 consensus,
// all prosecutor agents, 100% evidence verification.
func provisionalReviewPolicy() TierReviewPolicy {
	return TierReviewPolicy{
		Tier:                     trust.TierProvisional,
		PanelSize:                3,
		ConsensusThreshold:       2, // spec: "Merge requires 2/3rds consensus"
		RequireProsecutorAgents:  true,
		EvidenceVerificationPct:  100,
		RequireProviderDiversity: 2,
	}
}

// observedReviewPolicy: 2 models for contract changes, simple majority,
// prosecutor agents for contract changes, standard evidence verification.
func observedReviewPolicy() TierReviewPolicy {
	return TierReviewPolicy{
		Tier:                     trust.TierObserved,
		PanelSize:                2,
		ConsensusThreshold:       2, // spec: "simple majority from review models"
		RequireProsecutorAgents:  true, // still required for contract changes
		EvidenceVerificationPct:  80,
		RequireProviderDiversity: 2,
	}
}

// trustedReviewPolicy: single-model for most changes, spot-check verification.
// Prosecutor agents only for contract changes.
func trustedReviewPolicy() TierReviewPolicy {
	return TierReviewPolicy{
		Tier:                     trust.TierTrusted,
		PanelSize:                1, // single-model review
		ConsensusThreshold:       1, // single reviewer signoff (human or Tier 3)
		RequireProsecutorAgents:  false,
		EvidenceVerificationPct:  25, // spot-check only
		RequireProviderDiversity: 1,
	}
}

// veteranReviewPolicy: single-model review, no prosecutor agents,
// minimal verification. Veteran agents have demonstrated reliability.
func veteranReviewPolicy() TierReviewPolicy {
	return TierReviewPolicy{
		Tier:                     trust.TierVeteran,
		PanelSize:                1,
		ConsensusThreshold:       1,
		RequireProsecutorAgents:  false,
		EvidenceVerificationPct:  0, // no evidence verification for veterans
		RequireProviderDiversity: 1,
	}
}

// AdjustFormation combines the change category formation with the tier policy.
// The tier policy can reduce the panel size but never increase it beyond what
// the change category requires.
//
// Example: Contract change for a Provisional agent → 3 models (both agree).
// Contract change for a Trusted agent → 1 model (tier reduces from 3 to 1).
// Cosmetic change for a Provisional agent → 1 model (category wins, not tier).
func (ts *TierScaling) AdjustFormation(cat ChangeCategory, tier trust.TrustTier) (int, error) {
	categoryFormation := DetermineFormation(cat)
	tierPolicy, err := ts.PolicyForTier(tier)
	if err != nil {
		return 0, err
	}

	// Take the minimum of category formation and tier panel size.
	// Rationale: the change category defines the MAXIMUM review depth needed;
	// the tier can reduce it (trusted agents get lighter review) but the tier
	// never forces MORE review than the category calls for.
	if tierPolicy.PanelSize < categoryFormation {
		return tierPolicy.PanelSize, nil
	}
	return categoryFormation, nil
}

// AdjustConsensusThreshold combines the category consensus threshold with the
// tier policy, returning the effective threshold for a given panel size.
func (ts *TierScaling) AdjustConsensusThreshold(cat ChangeCategory, tier trust.TrustTier) (int, error) {
	tierPolicy, err := ts.PolicyForTier(tier)
	if err != nil {
		return 0, err
	}

	categoryThreshold := ConsensusThreshold(cat)

	// The effective threshold is the lower of the two (tier or category),
	// but never less than 1.
	effective := tierPolicy.ConsensusThreshold
	if categoryThreshold < effective {
		effective = categoryThreshold
	}
	if effective < 1 {
		effective = 1
	}
	return effective, nil
}

// ShouldVerifyEvidence returns true if evidence verification should be
// performed for an agent at the given tier.
func (ts *TierScaling) ShouldVerifyEvidence(tier trust.TrustTier) (bool, error) {
	policy, err := ts.PolicyForTier(tier)
	if err != nil {
		return false, err
	}
	return policy.EvidenceVerificationPct > 0, nil
}

// ShouldDispatchProsecutors returns true if adversarial prosecutor agents
// should be dispatched for an agent at the given tier with the given change
// category.
func (ts *TierScaling) ShouldDispatchProsecutors(cat ChangeCategory, tier trust.TrustTier) (bool, error) {
	policy, err := ts.PolicyForTier(tier)
	if err != nil {
		return false, err
	}

	// Prosecutors are always skipped for cosmetic changes.
	if cat == CategoryCosmetic {
		return false, nil
	}

	// For trusted+ tiers, prosecutors only apply to contract changes.
	if tier == trust.TierTrusted || tier == trust.TierVeteran {
		return cat == CategoryContract, nil
	}

	// For provisional and observed tiers, prosecutors are always on
	// (unless cosmetic, handled above).
	return policy.RequireProsecutorAgents, nil
}

// ReviewDepthSummary returns a human-readable description of the review
// requirements for the given tier.
func (ts *TierScaling) ReviewDepthSummary(tier trust.TrustTier) (string, error) {
	policy, err := ts.PolicyForTier(tier)
	if err != nil {
		return "", err
	}

	prosecutors := "no prosecutors"
	if policy.RequireProsecutorAgents {
		prosecutors = "prosecutors active"
	}

	verification := "no verification"
	if policy.EvidenceVerificationPct > 0 {
		verification = fmt.Sprintf("%d%% evidence verification", policy.EvidenceVerificationPct)
	}

	return fmt.Sprintf("tier=%s panel=%d consensus=%d/%d %s %s",
		tier, policy.PanelSize, policy.ConsensusThreshold, policy.PanelSize,
		prosecutors, verification), nil
}
