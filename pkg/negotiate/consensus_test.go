package negotiate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ComputeWeight tests (spec §10.1)
// ---------------------------------------------------------------------------

func TestComputeWeight(t *testing.T) {
	tests := []struct {
		name     string
		trust    TrustLevel
		expected ReviewerWeight
	}{
		{"trust 0", 0, WeightLow},
		{"trust 29", 29, WeightLow},
		{"trust 50", 50, WeightLow},
		{"trust 69", 69, WeightLow},
		{"trust 70", 70, WeightStandard},
		{"trust 80", 80, WeightStandard},
		{"trust 89", 89, WeightStandard},
		{"trust 90", 90, WeightHigh},
		{"trust 95", 95, WeightHigh},
		{"trust 100", 100, WeightHigh},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ComputeWeight(tt.trust))
		})
	}
}

// ---------------------------------------------------------------------------
// RequiredQuorum tests
// ---------------------------------------------------------------------------

func TestRequiredQuorum(t *testing.T) {
	tests := []struct {
		category ConsensusCategory
		expected int
	}{
		{CategoryContract, 3},
		{CategoryBehavioral, 2},
		{CategoryResilience, 1},
		{CategoryCosmetic, 1},
		{ConsensusCategory("unknown"), 3}, // defaults to strictest
	}

	for _, tt := range tests {
		t.Run(string(tt.category), func(t *testing.T) {
			assert.Equal(t, tt.expected, RequiredQuorum(tt.category))
		})
	}
}

// ---------------------------------------------------------------------------
// CheckOverride tests
// ---------------------------------------------------------------------------

func TestCheckOverride_Applied(t *testing.T) {
	signals := []ReviewSignal{
		{AgentName: "trusted", TrustLevel: 95, Verdict: VerdictApproved},
		{AgentName: "junior", TrustLevel: 40, Verdict: VerdictRequestChanges},
	}

	result := CheckOverride(signals)
	assert.True(t, result.Applied)
	assert.Equal(t, "trusted", result.Overrider)
	assert.Equal(t, "junior", result.Overridden)
}

func TestCheckOverride_NotApplied_BothLowTrust(t *testing.T) {
	signals := []ReviewSignal{
		{AgentName: "a", TrustLevel: 50, Verdict: VerdictApproved},
		{AgentName: "b", TrustLevel: 40, Verdict: VerdictRequestChanges},
	}

	result := CheckOverride(signals)
	assert.False(t, result.Applied)
}

func TestCheckOverride_NotApplied_HighTrustDissent(t *testing.T) {
	// If a high-trust reviewer also dissents, no override.
	signals := []ReviewSignal{
		{AgentName: "trusted", TrustLevel: 95, Verdict: VerdictApproved},
		{AgentName: "senior", TrustLevel: 85, Verdict: VerdictRequestChanges},
		{AgentName: "junior", TrustLevel: 40, Verdict: VerdictRequestChanges},
	}

	result := CheckOverride(signals)
	assert.False(t, result.Applied)
}

func TestCheckOverride_NotApplied_NoLowTrustDissent(t *testing.T) {
	signals := []ReviewSignal{
		{AgentName: "trusted", TrustLevel: 95, Verdict: VerdictApproved},
		{AgentName: "senior", TrustLevel: 85, Verdict: VerdictRequestChanges},
	}

	result := CheckOverride(signals)
	assert.False(t, result.Applied)
}

func TestCheckOverride_NotApplied_NoHighTrustApprove(t *testing.T) {
	signals := []ReviewSignal{
		{AgentName: "mid", TrustLevel: 75, Verdict: VerdictApproved},
		{AgentName: "junior", TrustLevel: 40, Verdict: VerdictRequestChanges},
	}

	result := CheckOverride(signals)
	assert.False(t, result.Applied)
}

func TestCheckOverride_EmptySignals(t *testing.T) {
	result := CheckOverride([]ReviewSignal{})
	assert.False(t, result.Applied)
}

// ---------------------------------------------------------------------------
// ConsensusCalculator — ComputeConsensus tests
// ---------------------------------------------------------------------------

func TestComputeConsensus_UnanimousApprove(t *testing.T) {
	calc := NewConsensusCalculator(CategoryBehavioral)
	signals := []ReviewSignal{
		{AgentName: "a", TrustLevel: 80, Verdict: VerdictApproved},
		{AgentName: "b", TrustLevel: 75, Verdict: VerdictApproved},
	}

	result := calc.ComputeConsensus(signals)

	assert.Equal(t, VerdictApproved, result.Verdict)
	assert.True(t, result.MeetsQuorum)
	assert.Equal(t, 2, result.QuorumRequired)
	assert.False(t, result.OverrideApplied)
	assert.Greater(t, result.WeightedApprove, result.WeightedReject)
}

func TestComputeConsensus_UnanimousReject(t *testing.T) {
	calc := NewConsensusCalculator(CategoryBehavioral)
	signals := []ReviewSignal{
		{AgentName: "a", TrustLevel: 80, Verdict: VerdictRequestChanges},
		{AgentName: "b", TrustLevel: 75, Verdict: VerdictRequestChanges},
	}

	result := calc.ComputeConsensus(signals)

	assert.Equal(t, VerdictRequestChanges, result.Verdict)
	assert.True(t, result.MeetsQuorum)
	assert.Greater(t, result.WeightedReject, result.WeightedApprove)
}

func TestComputeConsensus_SplitDecision_WeightedApprove(t *testing.T) {
	// High-trust approve vs low-trust reject: approve wins
	calc := NewConsensusCalculator(CategoryBehavioral)
	signals := []ReviewSignal{
		{AgentName: "senior", TrustLevel: 90, Verdict: VerdictApproved},
		{AgentName: "junior", TrustLevel: 40, Verdict: VerdictRequestChanges},
	}

	result := calc.ComputeConsensus(signals)

	// Weighted: 1.5 (approve) vs 0.5 (reject) → approve
	assert.Equal(t, VerdictApproved, result.Verdict)
	assert.True(t, result.OverrideApplied)
	assert.Equal(t, "senior", result.OverrideBy)
}

func TestComputeConsensus_SplitDecision_WeightedReject(t *testing.T) {
	// High-trust reject vs low-trust approve: reject wins
	calc := NewConsensusCalculator(CategoryBehavioral)
	signals := []ReviewSignal{
		{AgentName: "senior", TrustLevel: 90, Verdict: VerdictRequestChanges},
		{AgentName: "junior", TrustLevel: 40, Verdict: VerdictApproved},
	}

	result := calc.ComputeConsensus(signals)

	// Weighted: 0.5 (approve) vs 1.5 (reject) → reject
	assert.Equal(t, VerdictRequestChanges, result.Verdict)
}

func TestComputeConsensus_Tie_DefaultsToReject(t *testing.T) {
	calc := NewConsensusCalculator(CategoryBehavioral)
	signals := []ReviewSignal{
		{AgentName: "a", TrustLevel: 80, Verdict: VerdictApproved},
		{AgentName: "b", TrustLevel: 80, Verdict: VerdictRequestChanges},
	}

	result := calc.ComputeConsensus(signals)

	// Weighted: 1.0 (approve) vs 1.0 (reject) → tie → reject for safety
	assert.Equal(t, VerdictRequestChanges, result.Verdict)
}

func TestComputeConsensus_QuorumNotMet(t *testing.T) {
	// Contract category requires 3/3. Only 2 approve → not enough.
	calc := NewConsensusCalculator(CategoryContract)
	signals := []ReviewSignal{
		{AgentName: "a", TrustLevel: 80, Verdict: VerdictApproved},
		{AgentName: "b", TrustLevel: 80, Verdict: VerdictApproved},
	}

	result := calc.ComputeConsensus(signals)

	// 2 approve but quorum is 3 → not met → reject
	assert.Equal(t, VerdictRequestChanges, result.Verdict)
	assert.False(t, result.MeetsQuorum)
}

func TestComputeConsensus_QuorumMet_Contract(t *testing.T) {
	calc := NewConsensusCalculator(CategoryContract)
	signals := []ReviewSignal{
		{AgentName: "a", TrustLevel: 80, Verdict: VerdictApproved},
		{AgentName: "b", TrustLevel: 80, Verdict: VerdictApproved},
		{AgentName: "c", TrustLevel: 80, Verdict: VerdictApproved},
	}

	result := calc.ComputeConsensus(signals)

	assert.Equal(t, VerdictApproved, result.Verdict)
	assert.True(t, result.MeetsQuorum)
	assert.Equal(t, 3, result.QuorumRequired)
}

func TestComputeConsensus_EmptySignals(t *testing.T) {
	calc := NewConsensusCalculator(CategoryContract)
	result := calc.ComputeConsensus([]ReviewSignal{})

	assert.Equal(t, VerdictRequestChanges, result.Verdict)
	assert.False(t, result.MeetsQuorum)
}

func TestComputeConsensus_SingleReviewer_Cosmetic(t *testing.T) {
	calc := NewConsensusCalculator(CategoryCosmetic)
	signals := []ReviewSignal{
		{AgentName: "a", TrustLevel: 60, Verdict: VerdictApproved},
	}

	result := calc.ComputeConsensus(signals)

	assert.Equal(t, VerdictApproved, result.Verdict)
	assert.True(t, result.MeetsQuorum)
	assert.Equal(t, 1, result.QuorumRequired)
}

func TestComputeConsensus_SingleReviewer_Resilience(t *testing.T) {
	calc := NewConsensusCalculator(CategoryResilience)
	signals := []ReviewSignal{
		{AgentName: "a", TrustLevel: 70, Verdict: VerdictApproved},
	}

	result := calc.ComputeConsensus(signals)

	assert.Equal(t, VerdictApproved, result.Verdict)
	assert.True(t, result.MeetsQuorum)
}

func TestComputeConsensus_ReviewersSortedByTrust(t *testing.T) {
	calc := NewConsensusCalculator(CategoryContract)
	signals := []ReviewSignal{
		{AgentName: "low", TrustLevel: 40, Verdict: VerdictApproved},
		{AgentName: "high", TrustLevel: 95, Verdict: VerdictApproved},
		{AgentName: "mid", TrustLevel: 75, Verdict: VerdictApproved},
	}

	result := calc.ComputeConsensus(signals)

	require.Len(t, result.Reviewers, 3)
	assert.Equal(t, TrustLevel(95), result.Reviewers[0].TrustLevel)
	assert.Equal(t, TrustLevel(75), result.Reviewers[1].TrustLevel)
	assert.Equal(t, TrustLevel(40), result.Reviewers[2].TrustLevel)
}

func TestComputeConsensus_OverrideWinsOverQuorum(t *testing.T) {
	// Contract needs 3/3. Only 2 approve, but one is trust-90+ overriding
	// a trust-<70 dissent. Override should apply.
	calc := NewConsensusCalculator(CategoryContract)
	signals := []ReviewSignal{
		{AgentName: "senior", TrustLevel: 95, Verdict: VerdictApproved},
		{AgentName: "mid", TrustLevel: 75, Verdict: VerdictApproved},
		{AgentName: "junior", TrustLevel: 40, Verdict: VerdictRequestChanges},
	}

	result := calc.ComputeConsensus(signals)

	assert.True(t, result.OverrideApplied)
	assert.Equal(t, VerdictApproved, result.Verdict)
	// Quorum for contract is 3, only 2 approve — but override wins
	// Quorum is not met in the traditional sense
}

func TestComputeConsensus_WeightedScoresAccurate(t *testing.T) {
	calc := NewConsensusCalculator(CategoryBehavioral)
	signals := []ReviewSignal{
		{AgentName: "a", TrustLevel: 95, Verdict: VerdictApproved},       // 1.5
		{AgentName: "b", TrustLevel: 50, Verdict: VerdictRequestChanges}, // 0.5
	}

	result := calc.ComputeConsensus(signals)

	assert.InDelta(t, 1.5, result.WeightedApprove, 0.001)
	assert.InDelta(t, 0.5, result.WeightedReject, 0.001)
}

func TestComputeConsensus_AllReject(t *testing.T) {
	calc := NewConsensusCalculator(CategoryContract)
	signals := []ReviewSignal{
		{AgentName: "a", TrustLevel: 90, Verdict: VerdictRequestChanges},
		{AgentName: "b", TrustLevel: 80, Verdict: VerdictRequestChanges},
		{AgentName: "c", TrustLevel: 70, Verdict: VerdictRequestChanges},
	}

	result := calc.ComputeConsensus(signals)

	assert.Equal(t, VerdictRequestChanges, result.Verdict)
	assert.True(t, result.MeetsQuorum)
	assert.InDelta(t, 0.0, result.WeightedApprove, 0.001)
}

// ---------------------------------------------------------------------------
// Helper function tests
// ---------------------------------------------------------------------------

func TestCanParticipate(t *testing.T) {
	assert.True(t, CanParticipate(30))
	assert.True(t, CanParticipate(50))
	assert.True(t, CanParticipate(100))
	assert.False(t, CanParticipate(29))
	assert.False(t, CanParticipate(0))
}

func TestCanVeto(t *testing.T) {
	assert.True(t, CanVeto(70))
	assert.True(t, CanVeto(90))
	assert.True(t, CanVeto(100))
	assert.False(t, CanVeto(69))
	assert.False(t, CanVeto(50))
}

func TestHasQuorum(t *testing.T) {
	t.Run("meets quorum", func(t *testing.T) {
		signals := []ReviewSignal{
			{Verdict: VerdictApproved},
			{Verdict: VerdictApproved},
			{Verdict: VerdictRequestChanges},
		}
		assert.True(t, HasQuorum(signals, CategoryBehavioral))
		assert.False(t, HasQuorum(signals, CategoryContract))
	})

	t.Run("all approve", func(t *testing.T) {
		signals := []ReviewSignal{
			{Verdict: VerdictApproved},
			{Verdict: VerdictApproved},
			{Verdict: VerdictApproved},
		}
		assert.True(t, HasQuorum(signals, CategoryContract))
	})

	t.Run("none approve", func(t *testing.T) {
		signals := []ReviewSignal{
			{Verdict: VerdictRequestChanges},
			{Verdict: VerdictRequestChanges},
		}
		assert.False(t, HasQuorum(signals, CategoryCosmetic))
	})

	t.Run("empty signals", func(t *testing.T) {
		assert.False(t, HasQuorum([]ReviewSignal{}, CategoryCosmetic))
	})
}

// ---------------------------------------------------------------------------
// FormatConsensus tests
// ---------------------------------------------------------------------------

func TestFormatConsensus(t *testing.T) {
	result := ConsensusResult{
		Category:        CategoryBehavioral,
		Verdict:         VerdictApproved,
		QuorumRequired:  2,
		WeightedApprove: 2.0,
		WeightedReject:  0.0,
		MeetsQuorum:     true,
		Reviewers: []ReviewerContribution{
			{AgentName: "a", TrustLevel: 80, Verdict: VerdictApproved, Weight: WeightStandard},
			{AgentName: "b", TrustLevel: 80, Verdict: VerdictApproved, Weight: WeightStandard},
		},
	}

	output := FormatConsensus(result)
	assert.Contains(t, output, "APPROVED")
	assert.Contains(t, output, "quorum met")
	assert.Contains(t, output, "behavioral")
	assert.Contains(t, output, "a (trust")
	assert.Contains(t, output, "b (trust")
}

func TestFormatConsensus_WithOverride(t *testing.T) {
	result := ConsensusResult{
		Category:        CategoryContract,
		Verdict:         VerdictApproved,
		OverrideApplied: true,
		OverrideBy:      "senior",
		MeetsQuorum:     true,
	}

	output := FormatConsensus(result)
	assert.Contains(t, output, "Override:")
	assert.Contains(t, output, "senior")
}

// ---------------------------------------------------------------------------
// intToStr / floatToStr helpers
// ---------------------------------------------------------------------------

func TestIntToStr(t *testing.T) {
	assert.Equal(t, "0", intToStr(0))
	assert.Equal(t, "1", intToStr(1))
	assert.Equal(t, "42", intToStr(42))
	assert.Equal(t, "100", intToStr(100))
	assert.Equal(t, "-5", intToStr(-5))
}

func TestFloatToStr(t *testing.T) {
	assert.Contains(t, floatToStr(1.5), "1.")
	assert.Contains(t, floatToStr(0.5), "0.")
	assert.Contains(t, floatToStr(2.0), "2.")
}
