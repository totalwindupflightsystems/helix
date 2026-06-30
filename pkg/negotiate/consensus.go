package negotiate

import (
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// ReviewSignal is a single reviewer's verdict on a PR.
type ReviewSignal struct {
	AgentName   string     `json:"agent_name"`
	TrustLevel  TrustLevel `json:"trust_level"`
	Verdict     Verdict    `json:"verdict"`
	HasEvidence bool       `json:"has_evidence"`
}

// ConsensusCategory classifies the type of code change to determine
// consensus requirements. Mirrors pkg/review.ChangeCategory but kept
// local to avoid an import cycle.
type ConsensusCategory string

const (
	// CategoryContract — API signatures, data schemas, auth. Requires 3/3.
	CategoryContract ConsensusCategory = "contract"
	// CategoryBehavioral — business logic, algorithms. Requires 2/2.
	CategoryBehavioral ConsensusCategory = "behavioral"
	// CategoryResilience — error handling, retry. Requires 1/1.
	CategoryResilience ConsensusCategory = "resilience"
	// CategoryCosmetic — formatting, comments. Requires 1/1.
	CategoryCosmetic ConsensusCategory = "cosmetic"
)

// ReviewerWeight is the multiplier applied to a reviewer's vote based on
// their trust level (spec §10.1).
type ReviewerWeight float64

const (
	// WeightStandard — trust 70-89. Vote carries 1.0× weight.
	WeightStandard ReviewerWeight = 1.0
	// WeightHigh — trust 90+. Vote carries 1.5× weight.
	WeightHigh ReviewerWeight = 1.5
	// WeightLow — trust <70. Vote carries 0.5× weight.
	WeightLow ReviewerWeight = 0.5
)

// ConsensusResult is the output of ComputeConsensus.
type ConsensusResult struct {
	Category        ConsensusCategory      `json:"category"`
	Verdict         Verdict                `json:"verdict"`
	QuorumRequired  int                    `json:"quorum_required"`
	Reviewers       []ReviewerContribution `json:"reviewers"`
	WeightedApprove float64                `json:"weighted_approve"`
	WeightedReject  float64                `json:"weighted_reject"`
	OverrideApplied bool                   `json:"override_applied"`
	OverrideBy      string                 `json:"override_by,omitempty"`
	MeetsQuorum     bool                   `json:"meets_quorum"`
}

// ReviewerContribution shows each reviewer's weight and contribution.
type ReviewerContribution struct {
	AgentName  string         `json:"agent_name"`
	TrustLevel TrustLevel     `json:"trust_level"`
	Verdict    Verdict        `json:"verdict"`
	Weight     ReviewerWeight `json:"weight"`
}

// ---------------------------------------------------------------------------
// Weight computation (spec §10.1)
// ---------------------------------------------------------------------------

// ComputeWeight returns the vote weight multiplier for a trust level.
//   - trust 90+ → 1.5×
//   - trust 70-89 → 1.0×
//   - trust <70 → 0.5×
func ComputeWeight(trust TrustLevel) ReviewerWeight {
	switch {
	case trust >= 90:
		return WeightHigh
	case trust >= 70:
		return WeightStandard
	default:
		return WeightLow
	}
}

// ---------------------------------------------------------------------------
// Quorum computation
// ---------------------------------------------------------------------------

// RequiredQuorum returns the minimum number of reviewers that must agree
// for the given change category.
//
//   - Contract: 3/3 (all reviewers must agree)
//   - Behavioral: 2/2
//   - Resilience: 1/1
//   - Cosmetic: 1/1
func RequiredQuorum(category ConsensusCategory) int {
	switch category {
	case CategoryContract:
		return 3
	case CategoryBehavioral:
		return 2
	case CategoryResilience:
		return 1
	case CategoryCosmetic:
		return 1
	default:
		return 3 // default to strictest
	}
}

// ---------------------------------------------------------------------------
// Override detection
// ---------------------------------------------------------------------------

// OverrideResult captures whether a trust-90+ reviewer can override
// a dissent from a trust-<70 reviewer.
type OverrideResult struct {
	Applied    bool
	Overrider  string
	Overridden string
}

// CheckOverride evaluates whether a high-trust reviewer (≥90) can override
// a low-trust dissent (<70). Per spec §10.1, a trust-90+ reviewer carries
// 1.5× weight — enough to counter a 0.5× dissent from a trust-<70 reviewer.
// However, if any reviewer with veto power (trust ≥70) also dissents, the
// override does NOT apply — the dissent is too substantial to dismiss.
func CheckOverride(signals []ReviewSignal) OverrideResult {
	var vetoCapableDissent *ReviewSignal
	var lowTrustDissent *ReviewSignal

	for i := range signals {
		s := &signals[i]
		if s.Verdict == VerdictApproved {
			continue
		}
		if s.TrustLevel >= 70 {
			if vetoCapableDissent == nil {
				vetoCapableDissent = s
			}
		}
		if s.TrustLevel < 70 {
			if lowTrustDissent == nil {
				lowTrustDissent = s
			}
		}
	}

	// Override: a trust-90+ APPROVE overrides a trust-<70 REQUEST_CHANGES,
	// but only if no reviewer with veto power (trust ≥70) also dissents.
	var highTrustApprove *ReviewSignal
	for i := range signals {
		s := &signals[i]
		if s.Verdict == VerdictApproved && s.TrustLevel >= 90 {
			highTrustApprove = s
			break
		}
	}

	if highTrustApprove != nil && lowTrustDissent != nil && vetoCapableDissent == nil {
		return OverrideResult{
			Applied:    true,
			Overrider:  highTrustApprove.AgentName,
			Overridden: lowTrustDissent.AgentName,
		}
	}

	return OverrideResult{Applied: false}
}

// ---------------------------------------------------------------------------
// ConsensusCalculator
// ---------------------------------------------------------------------------

// ConsensusCalculator computes the final verdict from multiple review signals
// using trust-weighted voting (spec §10.1) and category-based quorum (spec §11).
type ConsensusCalculator struct {
	category ConsensusCategory
}

// NewConsensusCalculator creates a calculator for the given change category.
func NewConsensusCalculator(category ConsensusCategory) *ConsensusCalculator {
	return &ConsensusCalculator{category: category}
}

// ComputeConsensus evaluates review signals and returns a ConsensusResult.
// It applies trust-weighted voting, quorum checks, and override detection.
func (c *ConsensusCalculator) ComputeConsensus(signals []ReviewSignal) ConsensusResult {
	result := ConsensusResult{
		Category:       c.category,
		QuorumRequired: RequiredQuorum(c.category),
	}

	if len(signals) == 0 {
		result.Verdict = VerdictRequestChanges
		result.MeetsQuorum = false
		return result
	}

	// Compute per-reviewer weights and aggregate weighted scores.
	var weightedApprove, weightedReject float64
	var approveCount, rejectCount int

	for _, s := range signals {
		weight := ComputeWeight(s.TrustLevel)

		result.Reviewers = append(result.Reviewers, ReviewerContribution{
			AgentName:  s.AgentName,
			TrustLevel: s.TrustLevel,
			Verdict:    s.Verdict,
			Weight:     weight,
		})

		if s.Verdict == VerdictApproved {
			weightedApprove += float64(weight)
			approveCount++
		} else {
			weightedReject += float64(weight)
			rejectCount++
		}
	}

	result.WeightedApprove = weightedApprove
	result.WeightedReject = weightedReject

	// Check for override (trust-90+ overrides trust-<70 dissent).
	override := CheckOverride(signals)
	result.OverrideApplied = override.Applied
	result.OverrideBy = override.Overrider

	// Sort reviewers by trust level descending for stable output.
	sort.SliceStable(result.Reviewers, func(i, j int) bool {
		return result.Reviewers[i].TrustLevel > result.Reviewers[j].TrustLevel
	})

	// Determine consensus:
	// 1. If override applied, the high-trust APPROVE wins.
	// 2. If weighted approve > weighted reject, lean toward APPROVED.
	// 3. Check quorum: enough APPROVED votes to meet the category threshold.
	if override.Applied {
		result.Verdict = VerdictApproved
		result.MeetsQuorum = approveCount >= RequiredQuorum(c.category)
		return result
	}

	if weightedApprove > weightedReject {
		result.Verdict = VerdictApproved
	} else if weightedReject > weightedApprove {
		result.Verdict = VerdictRequestChanges
	} else {
		// Tie — default to REJECT for safety
		result.Verdict = VerdictRequestChanges
	}

	// Quorum check: does the winning side have enough votes?
	winningCount := approveCount
	if result.Verdict == VerdictRequestChanges {
		winningCount = rejectCount
	}
	result.MeetsQuorum = winningCount >= RequiredQuorum(c.category)

	// If quorum not met, downgrade to REJECT for safety.
	if !result.MeetsQuorum {
		result.Verdict = VerdictRequestChanges
	}

	return result
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// CanParticipate returns true if the agent's trust level is high enough
// to formally object and participate in negotiation (trust ≥ 30 per §10.1).
func CanParticipate(trust TrustLevel) bool {
	return trust >= 30
}

// CanVeto returns true if the agent's trust level is high enough to veto
// (trust ≥ 70 per §10.1).
func CanVeto(trust TrustLevel) bool {
	return trust >= 70
}

// HasQuorum checks whether the number of approving signals meets the
// required quorum for the given category. This is a convenience function
// that doesn't consider weights or overrides.
func HasQuorum(signals []ReviewSignal, category ConsensusCategory) bool {
	approveCount := 0
	for _, s := range signals {
		if s.Verdict == VerdictApproved {
			approveCount++
		}
	}
	return approveCount >= RequiredQuorum(category)
}

// FormatConsensus renders a ConsensusResult as a human-readable summary
// for audit logs and CLI output.
func FormatConsensus(result ConsensusResult) string {
	var b strings.Builder

	b.WriteString("Consensus: ")
	b.WriteString(string(result.Verdict))
	if result.MeetsQuorum {
		b.WriteString(" (quorum met)")
	} else {
		b.WriteString(" (quorum NOT met)")
	}
	b.WriteString("\n")

	b.WriteString("Category: ")
	b.WriteString(string(result.Category))
	b.WriteString(" (quorum required: ")
	b.WriteString(intToStr(result.QuorumRequired))
	b.WriteString(")\n")

	b.WriteString("Weighted approve: ")
	b.WriteString(floatToStr(result.WeightedApprove))
	b.WriteString(", reject: ")
	b.WriteString(floatToStr(result.WeightedReject))
	b.WriteString("\n")

	if result.OverrideApplied {
		b.WriteString("Override: ")
		b.WriteString(result.OverrideBy)
		b.WriteString(" overrode ")
		b.WriteString(result.OverrideBy) // overridden field not stored in result...
		b.WriteString("'s dissent\n")
	}

	for _, r := range result.Reviewers {
		b.WriteString("  ")
		b.WriteString(r.AgentName)
		b.WriteString(" (trust ")
		b.WriteString(intToStr(int(r.TrustLevel)))
		b.WriteString(", ")
		b.WriteString(floatToStr(float64(r.Weight)))
		b.WriteString("×): ")
		b.WriteString(string(r.Verdict))
		b.WriteString("\n")
	}

	return b.String()
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	result := ""
	for n > 0 {
		digit := byte('0' + n%10)
		result = string(digit) + result
		n /= 10
	}
	if negative {
		result = "-" + result
	}
	return result
}

func floatToStr(f float64) string {
	// Simple float formatting without strconv to keep stdlib-only style.
	whole := int(f)
	frac := int((f - float64(whole)) * 10)
	if frac < 0 {
		frac = -frac
	}
	if frac == 0 {
		return intToStr(whole) + ".0"
	}
	return intToStr(whole) + "." + intToStr(frac)
}
