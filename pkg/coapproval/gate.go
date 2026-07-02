// Package coapproval implements the co-approval gate — the final merge
// gate that requires both 1 human AND 1 trusted agent to approve a PR
// before it can be merged.
//
// Per specs/SPECIFICATION.md §7.2 (Gate Ordering — Co-Approval Gate):
//
//	"Co-Approval Gate (human + agent, async)"
//
// Per specs/SPECIFICATION.md §13.3 (Phase 3 success criteria):
//
//	"PR blocked until 1 human + 1 agent approve"
//
// Trust-based agent approval (per trust-model.md integration points):
//   - Agent with trust >= 70 satisfies the agent side alone
//   - Agent with trust < 70 requires 2 agents to satisfy
//   - Agent with veto power (trust >= 90) can override a single dissent
//
// Approval expiry: approvals expire after 24 hours. If the PR changes
// after an approval (new push), the approval is automatically invalidated.
package coapproval

import (
	"fmt"
	"sync"
	"time"
)

// ============================================================================
// Constants
// ============================================================================

const (
	// ApprovalExpiry is how long an approval remains valid.
	ApprovalExpiry = 24 * time.Hour

	// TrustedAgentThreshold is the minimum trust score for an agent to
	// single-handedly satisfy the agent approval requirement.
	TrustedAgentThreshold = 70

	// UntrustedAgentRequiredCount is how many agents with trust < 70
	// are needed to collectively satisfy the agent approval requirement.
	UntrustedAgentRequiredCount = 2

	// VetoAgentThreshold is the trust score needed for veto power.
	VetoAgentThreshold = 90
)

// ============================================================================
// ReviewerType
// ============================================================================

// ReviewerType identifies whether an approval comes from a human or an agent.
type ReviewerType string

const (
	ReviewerHuman ReviewerType = "human"
	ReviewerAgent ReviewerType = "agent"
)

// ============================================================================
// Approval
// ============================================================================

// Approval represents a single reviewer's approval of a PR.
type Approval struct {
	// ReviewerID is the unique identifier for the reviewer (username or agent UUID).
	ReviewerID string `json:"reviewer_id"`

	// Type is human or agent.
	Type ReviewerType `json:"type"`

	// TrustScore is the reviewer's current trust score (0-100).
	// Humans are assumed to have trust 100.
	TrustScore int `json:"trust_score"`

	// Timestamp is when the approval was recorded.
	Timestamp time.Time `json:"timestamp"`

	// CommitSHA is the commit SHA that was approved.
	// If a new push happens, approvals for a different SHA are invalidated.
	CommitSHA string `json:"commit_sha"`

	// IsVeto marks this as a blocking veto instead of an approval.
	IsVeto bool `json:"is_veto"`
}

// IsExpired returns true if the approval is older than ApprovalExpiry.
func (a *Approval) IsExpired(now time.Time) bool {
	return now.Sub(a.Timestamp) > ApprovalExpiry
}

// IsValidForCommit returns true if the approval is for the given commit SHA.
func (a *Approval) IsValidForCommit(commitSHA string) bool {
	return a.CommitSHA == commitSHA
}

// IsTrustedAgent returns true if this is an agent with trust >= TrustedAgentThreshold.
func (a *Approval) IsTrustedAgent() bool {
	return a.Type == ReviewerAgent && a.TrustScore >= TrustedAgentThreshold
}

// HasVetoPower returns true if the reviewer can veto (trust >= VetoAgentThreshold).
func (a *Approval) HasVetoPower() bool {
	return a.TrustScore >= VetoAgentThreshold
}

// ============================================================================
// MergeEligibility
// ============================================================================

// MergeEligibility is the outcome of checking whether all co-approval
// requirements are met.
type MergeEligibility string

const (
	EligibilityAllowed    MergeEligibility = "ALLOWED"
	EligibilityBlocked    MergeEligibility = "BLOCKED"
	EligibilityNeedsHuman MergeEligibility = "NEEDS_HUMAN"
	EligibilityNeedsAgent MergeEligibility = "NEEDS_AGENT"
)

// ============================================================================
// EligibilityResult
// ============================================================================

// EligibilityResult is the full result of checking co-approval status.
type EligibilityResult struct {
	Decision   MergeEligibility `json:"decision"`
	Reason     string           `json:"reason"`
	HumanOK    bool             `json:"human_satisfied"`
	AgentOK    bool             `json:"agent_satisfied"`
	HumanCount int              `json:"human_count"`
	AgentCount int              `json:"agent_count"`
	Vetoed     bool             `json:"vetoed"`
	VetoBy     string           `json:"veto_by,omitempty"`
	Approvals  []Approval       `json:"approvals"`
}

// IsMergeable returns true if the decision is ALLOWED.
func (r *EligibilityResult) IsMergeable() bool {
	return r.Decision == EligibilityAllowed
}

// IsBlocked returns true if the decision is BLOCKED (vetoed).
func (r *EligibilityResult) IsBlocked() bool {
	return r.Decision == EligibilityBlocked
}

// Summary returns a human-readable summary.
func (r *EligibilityResult) Summary() string {
	if r.Decision == EligibilityAllowed {
		return fmt.Sprintf("Co-approval satisfied: %d human(s), %d agent(s)", r.HumanCount, r.AgentCount)
	}
	if r.Vetoed {
		return fmt.Sprintf("Co-approval blocked: vetoed by %s", r.VetoBy)
	}
	return fmt.Sprintf("Co-approval incomplete: %s (human=%d, agent=%d)", r.Reason, r.HumanCount, r.AgentCount)
}

// ============================================================================
// CoApprovalGate
// ============================================================================

// CoApprovalGate tracks approvals for a PR and determines whether the
// co-approval requirement is satisfied.
//
// Thread-safe via sync.RWMutex.
type CoApprovalGate struct {
	mu         sync.RWMutex
	prURL      string
	commitSHA  string
	approvals  map[string]*Approval // key = reviewer_id
	lastPushAt time.Time
	now        func() time.Time
}

// NewCoApprovalGate creates a new gate for the given PR URL and commit SHA.
func NewCoApprovalGate(prURL, commitSHA string) *CoApprovalGate {
	return &CoApprovalGate{
		prURL:      prURL,
		commitSHA:  commitSHA,
		approvals:  make(map[string]*Approval),
		lastPushAt: time.Now(),
		now:        time.Now,
	}
}

// NewCoApprovalGateWithClock creates a gate with a custom clock (for testing).
func NewCoApprovalGateWithClock(prURL, commitSHA string, clock func() time.Time) *CoApprovalGate {
	g := NewCoApprovalGate(prURL, commitSHA)
	g.now = clock
	return g
}

// PRURL returns the PR URL this gate tracks.
func (g *CoApprovalGate) PRURL() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.prURL
}

// CommitSHA returns the current commit SHA.
func (g *CoApprovalGate) CommitSHA() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.commitSHA
}

// UpdateCommit records a new push to the PR, invalidating approvals
// for the previous commit SHA. This enforces the spec requirement that
// approvals must be re-obtained if the PR changes.
func (g *CoApprovalGate) UpdateCommit(newSHA string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.commitSHA = newSHA
	g.lastPushAt = g.now()
}

// RecordApproval adds or updates an approval for the given reviewer.
// If the approval is a veto, it overrides all previous approvals.
// Returns an error if the reviewer has already vetoed (cannot un-veto).
func (g *CoApprovalGate) RecordApproval(approval Approval) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if approval.ReviewerID == "" {
		return fmt.Errorf("reviewer_id is required")
	}

	// Check for existing veto
	if existing, ok := g.approvals[approval.ReviewerID]; ok && existing.IsVeto && !approval.IsVeto {
		return fmt.Errorf("reviewer %s has already vetoed and cannot un-veto", approval.ReviewerID)
	}

	// Set timestamp if zero
	if approval.Timestamp.IsZero() {
		approval.Timestamp = g.now()
	}

	g.approvals[approval.ReviewerID] = &approval
	return nil
}

// RecordVeto records a blocking veto from a reviewer with veto power.
// Returns an error if the reviewer doesn't have veto power (trust < 90).
func (g *CoApprovalGate) RecordVeto(reviewerID string, reviewerType ReviewerType, trustScore int, commitSHA string) error {
	if trustScore < VetoAgentThreshold {
		return fmt.Errorf("reviewer %s (trust %d) does not have veto power (requires trust >= %d)",
			reviewerID, trustScore, VetoAgentThreshold)
	}

	return g.RecordApproval(Approval{
		ReviewerID: reviewerID,
		Type:       reviewerType,
		TrustScore: trustScore,
		CommitSHA:  commitSHA,
		IsVeto:     true,
	})
}

// RemoveApproval removes an approval (e.g., when a reviewer changes their mind
// and withdraws). Returns true if the approval was found and removed.
func (g *CoApprovalGate) RemoveApproval(reviewerID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if _, ok := g.approvals[reviewerID]; ok {
		delete(g.approvals, reviewerID)
		return true
	}
	return false
}

// GetApproval returns the approval from a specific reviewer, or nil.
func (g *CoApprovalGate) GetApproval(reviewerID string) *Approval {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if a, ok := g.approvals[reviewerID]; ok {
		cp := *a
		return &cp
	}
	return nil
}

// AllApprovals returns a snapshot of all current approvals (not vetoes).
func (g *CoApprovalGate) AllApprovals() []Approval {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var result []Approval
	for _, a := range g.approvals {
		if !a.IsVeto {
			result = append(result, *a)
		}
	}
	return result
}

// AllVetoes returns a snapshot of all current vetoes.
func (g *CoApprovalGate) AllVetoes() []Approval {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var result []Approval
	for _, a := range g.approvals {
		if a.IsVeto {
			result = append(result, *a)
		}
	}
	return result
}

// CheckEligibility evaluates whether all co-approval requirements are met.
//
// Logic:
//  1. If any veto exists → BLOCKED
//  2. Count valid (non-expired, matching commit) human approvals
//  3. Count valid agent approvals, classifying trusted vs untrusted
//  4. Human side satisfied if >= 1 human approval
//  5. Agent side satisfied if >= 1 trusted agent OR >= 2 untrusted agents
//  6. Both satisfied → ALLOWED; else → NEEDS_HUMAN or NEEDS_AGENT
func (g *CoApprovalGate) CheckEligibility() *EligibilityResult {
	g.mu.RLock()
	defer g.mu.RUnlock()

	now := g.now()
	result := &EligibilityResult{
		Decision: EligibilityBlocked,
	}

	// Collect valid approvals (non-expired, matching commit)
	var validApprovals []Approval
	var vetoes []Approval

	for _, a := range g.approvals {
		if a.IsExpired(now) {
			continue
		}
		if !a.IsValidForCommit(g.commitSHA) {
			continue
		}
		if a.IsVeto {
			vetoes = append(vetoes, *a)
		} else {
			validApprovals = append(validApprovals, *a)
		}
	}

	result.Approvals = validApprovals

	// Check for veto
	if len(vetoes) > 0 {
		result.Vetoed = true
		result.VetoBy = vetoes[0].ReviewerID
		result.Decision = EligibilityBlocked
		result.Reason = fmt.Sprintf("PR vetoed by %s", vetoes[0].ReviewerID)
		return result
	}

	// Count human and agent approvals
	humanCount := 0
	trustedAgentCount := 0
	untrustedAgentCount := 0

	for _, a := range validApprovals {
		switch a.Type {
		case ReviewerHuman:
			humanCount++
		case ReviewerAgent:
			if a.IsTrustedAgent() {
				trustedAgentCount++
			} else {
				untrustedAgentCount++
			}
		}
	}

	result.HumanCount = humanCount
	result.AgentCount = trustedAgentCount + untrustedAgentCount

	// Check human side
	result.HumanOK = humanCount >= 1

	// Check agent side: 1 trusted OR 2 untrusted
	agentSatisfied := trustedAgentCount >= 1 || untrustedAgentCount >= UntrustedAgentRequiredCount
	result.AgentOK = agentSatisfied

	// Determine decision
	switch {
	case result.HumanOK && result.AgentOK:
		result.Decision = EligibilityAllowed
		result.Reason = "Both human and agent approvals satisfied"
	case !result.HumanOK && !result.AgentOK:
		result.Decision = EligibilityBlocked
		result.Reason = "Neither human nor agent approval provided"
	case !result.HumanOK:
		result.Decision = EligibilityNeedsHuman
		result.Reason = "Waiting for human approval"
	case !result.AgentOK:
		result.Decision = EligibilityNeedsAgent
		if trustedAgentCount == 0 && untrustedAgentCount > 0 {
			remaining := UntrustedAgentRequiredCount - untrustedAgentCount
			result.Reason = fmt.Sprintf("Waiting for %d more agent approval(s) (current agents below trust threshold %d)",
				remaining, TrustedAgentThreshold)
		} else {
			result.Reason = "Waiting for agent approval"
		}
	}

	return result
}

// IsSatisfied is a convenience method that returns true if the co-approval
// requirement is fully satisfied (Decision == ALLOWED and not vetoed).
func (g *CoApprovalGate) IsSatisfied() bool {
	r := g.CheckEligibility()
	return r.IsMergeable()
}

// Reset clears all approvals and vetoes (e.g., when starting a new review cycle).
func (g *CoApprovalGate) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.approvals = make(map[string]*Approval)
}

// ApprovalCount returns the total number of non-veto approvals.
func (g *CoApprovalGate) ApprovalCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()

	count := 0
	for _, a := range g.approvals {
		if !a.IsVeto {
			count++
		}
	}
	return count
}

// VetoCount returns the number of active vetoes.
func (g *CoApprovalGate) VetoCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()

	count := 0
	for _, a := range g.approvals {
		if a.IsVeto {
			count++
		}
	}
	return count
}
