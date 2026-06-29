package negotiate

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Veto Protocol — specs/pr-negotiation.md §8
// ---------------------------------------------------------------------------

// VetoValidation holds the result of validating a veto attempt.
type VetoValidation struct {
	Valid      bool     `json:"valid"`
	Errors     []string `json:"errors,omitempty"`
	SpecRef    string   `json:"spec_ref,omitempty"`
	TestCmd    string   `json:"test_cmd,omitempty"`
	ACEvidence string   `json:"ac_evidence,omitempty"`
	VetoBody   string   `json:"veto_body"`
	AgentName  string   `json:"agent_name"`
}

// VetoAttempt captures one veto occurrence for tracking purposes.
type VetoAttempt struct {
	AgentName string    `json:"agent_name"`
	PRNumber  int       `json:"pr_number"`
	SpecRef   string    `json:"spec_ref"`
	Frivolous bool      `json:"frivolous"`
	Timestamp time.Time `json:"timestamp"`
}

// FrivolousVetoThreshold is the number of frivolous vetoes within the window
// that triggers the trust cap (spec §8.3: "3 frivolous vetos in 90 days").
const FrivolousVetoThreshold = 3

// FrivolousVetoWindow is the look-back period for counting frivolous vetoes
// (spec §8.3: "3 frivolous vetos in 90 days").
const FrivolousVetoWindow = 90 * 24 * time.Hour

// TrustCapAfterFrivolousVetoes is the maximum trust level allowed after
// exceeding the frivolous veto threshold (spec §8.3: "trust_level capped at 69").
const TrustCapAfterFrivolousVetoes = 69

// VetoTracker tracks veto history per agent for frivolous veto detection
// (spec §8.3).
type VetoTracker struct {
	mu       sync.Mutex
	attempts map[string][]VetoAttempt // agent name → veto history
}

// NewVetoTracker creates an empty veto tracker.
func NewVetoTracker() *VetoTracker {
	return &VetoTracker{
		attempts: make(map[string][]VetoAttempt),
	}
}

// ValidateVeto checks all 4 conditions from spec §8.1:
//  1. Agent trust_level ≥ 70
//  2. Cites a SPECIFIC spec section that is violated
//  3. Provides REPRODUCIBLE evidence (test command that fails)
//  4. The spec violation maps to an acceptance criterion marked as PASS
//
// Returns a VetoValidation with detailed error messages if any condition fails.
func ValidateVeto(agent Agent, body string) VetoValidation {
	result := VetoValidation{
		VetoBody:  body,
		AgentName: agent.Name,
	}

	// Condition 1: trust_level >= 70
	if agent.TrustLevel < 70 {
		result.Errors = append(result.Errors,
			fmt.Sprintf("trust_level %d < 70 — agent lacks veto power (spec §8.1)", agent.TrustLevel))
	}

	// Parse veto body for structured content
	specRef := extractSpecRef(body)
	testCmd := extractTestCommand(body)
	acRef := extractACEvidence(body)

	result.SpecRef = specRef
	result.TestCmd = testCmd
	result.ACEvidence = acRef

	// Condition 2: cites a specific spec section
	if specRef == "" {
		result.Errors = append(result.Errors,
			"no spec section cited — veto must reference a specific spec section (spec §8.1)")
	}

	// Condition 3: provides reproducible evidence (test command)
	if testCmd == "" {
		result.Errors = append(result.Errors,
			"no reproducible evidence — veto must include a test command that fails (spec §8.1)")
	}

	// Condition 4: spec violation maps to an acceptance criterion
	if acRef == "" {
		result.Errors = append(result.Errors,
			"no acceptance criterion reference — veto must cite an AC marked PASS that is violated (spec §8.1)")
	}

	result.Valid = len(result.Errors) == 0
	return result
}

// RecordFrivolousVeto records a frivolous veto for an agent (spec §8.3).
// A veto is frivolous when Chimera's tie-break determines no spec violation exists.
func (vt *VetoTracker) RecordFrivolousVeto(agentName string, prNumber int, specRef string, at time.Time) {
	vt.mu.Lock()
	defer vt.mu.Unlock()
	vt.attempts[agentName] = append(vt.attempts[agentName], VetoAttempt{
		AgentName: agentName,
		PRNumber:  prNumber,
		SpecRef:   specRef,
		Frivolous: true,
		Timestamp: at,
	})
}

// RecordValidVeto records a non-frivolous veto for tracking purposes.
func (vt *VetoTracker) RecordValidVeto(agentName string, prNumber int, specRef string, at time.Time) {
	vt.mu.Lock()
	defer vt.mu.Unlock()
	vt.attempts[agentName] = append(vt.attempts[agentName], VetoAttempt{
		AgentName: agentName,
		PRNumber:  prNumber,
		SpecRef:   specRef,
		Frivolous: false,
		Timestamp: at,
	})
}

// FrivolousCount returns the number of frivolous vetoes within the look-back
// window (90 days per spec §8.3).
func (vt *VetoTracker) FrivolousCount(agentName string, at time.Time) int {
	vt.mu.Lock()
	defer vt.mu.Unlock()

	cutoff := at.Add(-FrivolousVetoWindow)
	count := 0
	for _, v := range vt.attempts[agentName] {
		if v.Frivolous && v.Timestamp.After(cutoff) {
			count++
		}
	}
	return count
}

// ShouldCapTrust returns true if the agent has exceeded the frivolous veto
// threshold and their trust should be capped at 69 (spec §8.3: "3 frivolous
// vetos in 90 days → trust_level capped at 69").
func (vt *VetoTracker) ShouldCapTrust(agentName string, at time.Time) bool {
	return vt.FrivolousCount(agentName, at) >= FrivolousVetoThreshold
}

// ApplyTrustCap returns the trust level after applying the frivolous veto cap.
// If the agent hasn't exceeded the threshold, the trust is returned unchanged.
// If they have, trust is capped at 69 (spec §8.3).
func (vt *VetoTracker) ApplyTrustCap(agentName string, currentTrust TrustLevel, at time.Time) TrustLevel {
	if vt.ShouldCapTrust(agentName, at) {
		if currentTrust > TrustCapAfterFrivolousVetoes {
			return TrustCapAfterFrivolousVetoes
		}
	}
	return currentTrust
}

// FrivolousPenalty returns the trust penalty for a frivolous veto (spec §8.3:
// "-5 trust"). This is applied per frivolous veto, not just at the threshold.
func FrivolousPenalty() int { return -5 }

// VetoWeight returns the weight multiplier for a veto in Chimera deliberation
// prompts (spec §10.1: trust 90+ → 1.5× weight).
func VetoWeight(agent Agent) float64 {
	if agent.TrustLevel >= 90 {
		return 1.5
	}
	return 1.0
}

// --- Body parsing helpers ---

// extractSpecRef extracts a spec reference from the veto body.
// Recognizes patterns like "spec §8.1", "§8.1", "specs/auth.md §3", or "specs/foo.md".
func extractSpecRef(body string) string {
	// Check for § marker — this is the strongest signal of a spec section reference
	sectionMark := "§"
	if idx := strings.Index(body, sectionMark); idx != -1 {
		// § is a multi-byte UTF-8 rune — start scanning after the full character
		start := idx + len(sectionMark)
		end := start
		for end < len(body) && (body[end] == '.' || (body[end] >= '0' && body[end] <= '9')) {
			end++
		}
		if end > start {
			return body[idx:end]
		}
	}
	// Check for spec file path (specs/foo.md or spec.md)
	lower := strings.ToLower(body)
	if idx := strings.Index(lower, "spec"); idx != -1 {
		rest := body[idx:]
		if strings.Contains(rest, ".md") || strings.Contains(rest, "/") {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// extractTestCommand looks for a reproducible test command in the veto body.
// Patterns: "go test ...", "pytest ...", "npm test ...", or generic "test command: ...".
func extractTestCommand(body string) string {
	lower := strings.ToLower(body)
	commands := []string{"go test", "pytest", "npm test", "cargo test", "test command:", "run:"}
	for _, cmd := range commands {
		if idx := strings.Index(lower, cmd); idx != -1 {
			// Extract the rest of the line
			lineEnd := strings.IndexByte(body[idx:], '\n')
			if lineEnd == -1 {
				lineEnd = len(body) - idx
			}
			return strings.TrimSpace(body[idx : idx+lineEnd])
		}
	}
	return ""
}

// extractACEvidence looks for an acceptance criterion reference in the veto body.
// Pattern: "AC-<number>" or "AC <number>".
func extractACEvidence(body string) string {
	lower := strings.ToLower(body)
	for _, prefix := range []string{"ac-", "ac "} {
		if idx := strings.Index(lower, prefix); idx != -1 {
			rest := body[idx+len(prefix):]
			end := 0
			for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
				end++
			}
			if end > 0 {
				return body[idx : idx+len(prefix)+end]
			}
		}
	}
	return ""
}
