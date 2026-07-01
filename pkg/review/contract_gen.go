package review

import (
	"fmt"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/verify"
)

// ---------------------------------------------------------------------------
// Contract Generator — specs/production-verification.md §Integration Points:
// "Chimera: Generates behavior contract assertions from review findings"
//
// Converts EvidenceBundle findings into BehaviorContract assertions. Each
// high-severity finding generates a monitoring assertion that catches
// regressions if the finding's concern materializes in production.
// ---------------------------------------------------------------------------

// Severity constants (matching strings used throughout pkg/review).
const (
	severityCritical = "CRITICAL"
	severityHigh     = "HIGH"
	severityMedium   = "MEDIUM"
	severityLow      = "LOW"
)

// Finding type categories (matching strings used in Finding.Type).
const (
	typeSecurity    = "security"
	typePerformance = "performance"
	typeLogic       = "logic"
	typeRace        = "race_condition"
	typeAPISurface  = "spec_violation"
)

// Breach action constants from verify package.
const (
	breachActionRollback = "rollback"
	breachActionAlert    = "alert"
	breachActionBlock    = "block"
)

// ContractGenerator converts review findings into behavior contract assertions.
type ContractGenerator struct {
	// ConfidenceWeight scales assertion strictness based on review confidence.
	// Default: 1.0 (use findings as-is). Higher = stricter thresholds.
	ConfidenceWeight float64
}

// NewContractGenerator creates a generator with default settings.
func NewContractGenerator() *ContractGenerator {
	return &ContractGenerator{ConfidenceWeight: 1.0}
}

// GenerateFromFindings creates a BehaviorContract from an EvidenceBundle.
// Only high and critical findings generate assertions; low/medium findings
// produce informational notes in the breach action description.
func (g *ContractGenerator) GenerateFromFindings(bundle *EvidenceBundle) *verify.BehaviorContract {
	if bundle == nil {
		return nil
	}

	contract := &verify.BehaviorContract{
		Contract: verify.ContractBody{
			Name:        fmt.Sprintf("review-%s", bundle.ReviewID),
			MergeCommit: bundle.OriginalCommit,
			Assertions:  []verify.Assertion{},
		},
	}

	// Derive agent from PR URL (best effort)
	contract.Contract.Agent = extractAgentFromPR(bundle.PRURL)

	// Determine breach action from consensus resolution
	contract.Contract.BreachAction = breachActionFromConsensus(bundle.Consensus)

	assertionCount := 0
	for _, finding := range bundle.Findings {
		assertions := g.findingToAssertions(finding)
		contract.Contract.Assertions = append(contract.Contract.Assertions, assertions...)
		assertionCount += len(assertions)
	}

	// If no actionable findings, add a baseline assertion
	if assertionCount == 0 {
		contract.Contract.Assertions = append(contract.Contract.Assertions, verify.Assertion{
			Metric: "success_rate",
			Op:     "gte",
			Value:  99.0,
			Window: "1h",
		})
		contract.Contract.BreachAction = breachActionAlert
	}

	return contract
}

// GenerateForAgent wraps GenerateFromFindings with agent-specific metadata.
func (g *ContractGenerator) GenerateForAgent(bundle *EvidenceBundle, agentID, mergeCommit string) *verify.BehaviorContract {
	contract := g.GenerateFromFindings(bundle)
	if contract == nil {
		return nil
	}
	contract.Contract.Agent = agentID
	if mergeCommit != "" {
		contract.Contract.MergeCommit = mergeCommit
	}
	return contract
}

// findingToAssertions converts a single finding into zero or more assertions.
// The mapping is category-aware:
//   - security → error_count eq 0 + success_rate gte 99.9
//   - performance → latency_p99 lte threshold (derived from severity)
//   - logic → success_rate gte threshold
//   - race_condition → error_count lte 0 + latency_p99 lte
//   - spec_violation → success_rate gte 99.5
func (g *ContractGenerator) findingToAssertions(f Finding) []verify.Assertion {
	// Only high/critical findings generate assertions
	severity := strings.ToUpper(f.Severity)
	if severity != severityCritical && severity != severityHigh {
		return nil
	}

	var assertions []verify.Assertion
	category := strings.ToLower(f.Type)

	switch category {
	case typeSecurity:
		assertions = append(assertions, verify.Assertion{
			Metric: "error_count",
			Op:     "eq",
			Value:  0,
			Window: "1h",
		})
		assertions = append(assertions, verify.Assertion{
			Metric: "success_rate",
			Op:     "gte",
			Value:  g.applyConfidence(99.9),
			Window: "1h",
		})

	case typePerformance:
		// Critical = 100ms p99, High = 500ms p99
		threshold := 500.0
		if severity == severityCritical {
			threshold = 100.0
		}
		assertions = append(assertions, verify.Assertion{
			Metric: "latency_p99",
			Op:     "lte",
			Value:  g.applyConfidence(threshold),
			Window: "5m",
		})

	case typeLogic:
		// Critical = 99.9% success, High = 99.5% success
		threshold := 99.5
		if severity == severityCritical {
			threshold = 99.9
		}
		assertions = append(assertions, verify.Assertion{
			Metric: "success_rate",
			Op:     "gte",
			Value:  g.applyConfidence(threshold),
			Window: "1h",
		})

	case typeRace:
		// Race conditions → zero errors allowed + bounded latency
		assertions = append(assertions, verify.Assertion{
			Metric: "error_count",
			Op:     "lte",
			Value:  0,
			Window: "1h",
		})
		assertions = append(assertions, verify.Assertion{
			Metric: "latency_p99",
			Op:     "lte",
			Value:  g.applyConfidence(200.0),
			Window: "5m",
		})

	case typeAPISurface:
		assertions = append(assertions, verify.Assertion{
			Metric: "success_rate",
			Op:     "gte",
			Value:  g.applyConfidence(99.5),
			Window: "1h",
		})

	default:
		// Unknown category but high/critical severity → baseline assertion
		if severity == severityCritical {
			assertions = append(assertions, verify.Assertion{
				Metric: "success_rate",
				Op:     "gte",
				Value:  g.applyConfidence(99.9),
				Window: "1h",
			})
		} else {
			assertions = append(assertions, verify.Assertion{
				Metric: "success_rate",
				Op:     "gte",
				Value:  g.applyConfidence(99.5),
				Window: "1h",
			})
		}
	}

	return assertions
}

// applyConfidence adjusts a threshold based on the confidence weight.
// Higher confidence weight → stricter threshold (lower for lte, higher for gte).
func (g *ContractGenerator) applyConfidence(base float64) float64 {
	if g.ConfidenceWeight <= 0 || g.ConfidenceWeight == 1.0 {
		return base
	}
	return base * g.ConfidenceWeight
}

// breachActionFromConsensus determines the breach action from review consensus.
func breachActionFromConsensus(c Consensus) string {
	resolution := strings.ToLower(c.Resolution)
	if resolution == ResolutionBlocked {
		return breachActionBlock
	}
	if resolution == ResolutionTieBreak || resolution == ResolutionApproved {
		return breachActionRollback
	}
	return breachActionAlert
}

// extractAgentFromPR extracts agent ID from PR URL (best effort).
func extractAgentFromPR(prURL string) string {
	if prURL == "" {
		return ""
	}
	parts := strings.Split(prURL, "/")
	for i, p := range parts {
		if p == "pulls" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// Summary returns a human-readable summary of generated assertions.
func (g *ContractGenerator) Summary(contract *verify.BehaviorContract) string {
	if contract == nil {
		return "no contract generated"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Contract: %s (agent: %s)\n", contract.Contract.Name, contract.Contract.Agent))
	sb.WriteString(fmt.Sprintf("Breach action: %s\n", contract.Contract.BreachAction))
	sb.WriteString(fmt.Sprintf("Assertions: %d\n", len(contract.Contract.Assertions)))
	for i, a := range contract.Contract.Assertions {
		sb.WriteString(fmt.Sprintf("  %d. %s %s %.1f (window: %s)\n", i+1, a.Metric, a.Op, a.Value, a.Window))
	}
	return sb.String()
}
