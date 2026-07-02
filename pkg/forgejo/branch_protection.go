// Package forgejo — branch_protection.go
//
// BranchProtectionEnforcer configures Forgejo branch protection rules
// per trust tier, ensuring agents can only push to feat/* branches and
// main is protected with appropriate review requirements.
//
// Per specs/SPECIFICATION.md §13.2 (Day 9-10: scoped permissions):
//
//	"Wire scoped permissions: feat/* push, main block, PR open"
//
// Per specs/SPECIFICATION.md §5 (IAM):
//
//	Agents push to branches, not main. Branch protection enforces this.
package forgejo

import (
	"context"
	"fmt"
	"strings"
)

// ============================================================================
// Trust Tier Constants
// ============================================================================

// TrustTier represents an agent's trust tier.
type TrustTier string

const (
	TierProvisional TrustTier = "provisional"
	TierObserved    TrustTier = "observed"
	TierTrusted     TrustTier = "trusted"
	TierVeteran     TrustTier = "veteran"
)

// ============================================================================
// Branch Protection Types
// ============================================================================

// BranchProtectionRule defines the protection rules for a branch.
type BranchProtectionRule struct {
	// BranchName is the branch to protect (e.g., "main").
	BranchName string `json:"branch_name"`

	// RequiredApprovals is the minimum number of approving reviews.
	RequiredApprovals int `json:"required_approvals"`

	// RequiredStatusChecks lists CI checks that must pass before merge.
	RequiredStatusChecks []string `json:"required_status_checks"`

	// EnablePush controls whether direct push is allowed.
	// false = nobody can push directly (PR required).
	EnablePush bool `json:"enable_push"`

	// EnablePushWhitelist allows specific users/teams to push directly.
	EnablePushWhitelist bool     `json:"enable_push_whitelist"`
	PushWhitelistUsers  []string `json:"push_whitelist_users,omitempty"`

	// EnableMergeWhitelist restricts who can merge.
	EnableMergeWhitelist bool     `json:"enable_merge_whitelist"`
	MergeWhitelistUsers  []string `json:"merge_whitelist_users,omitempty"`

	// BlockOnRejectedReviews blocks merge if any review is "REQUEST_CHANGES".
	BlockOnRejectedReviews bool `json:"block_on_rejected_reviews"`

	// BlockOnOutdatedBranch blocks merge if behind main.
	BlockOnOutdatedBranch bool `json:"block_on_outdated_branch"`
}

// ============================================================================
// Tier Protection Configuration
// ============================================================================

// TierProtectionConfig maps trust tiers to their required review counts.
type TierProtectionConfig struct {
	// RequiredApprovalsByTier: Provisional=2, Observed=2, Trusted=1, Veteran=1
	RequiredApprovals map[TrustTier]int

	// RequiredStatusChecks are the CI checks that must pass.
	RequiredStatusChecks []string

	// CanMergeOwnPR: Veteran agents can merge their own PRs.
	CanMergeOwnPR map[TrustTier]bool
}

// DefaultTierProtectionConfig returns the spec-defined protection rules.
func DefaultTierProtectionConfig() TierProtectionConfig {
	return TierProtectionConfig{
		RequiredApprovals: map[TrustTier]int{
			TierProvisional: 2,
			TierObserved:    2,
			TierTrusted:     1,
			TierVeteran:     1,
		},
		RequiredStatusChecks: []string{"tier1", "tier2", "chimera"},
		CanMergeOwnPR: map[TrustTier]bool{
			TierProvisional: false,
			TierObserved:    false,
			TierTrusted:     false,
			TierVeteran:     true,
		},
	}
}

// RequiredApprovalsForTier returns the approval count for a given tier.
func (c TierProtectionConfig) RequiredApprovalsForTier(tier TrustTier) int {
	if n, ok := c.RequiredApprovals[tier]; ok {
		return n
	}
	return 2 // default to safest
}

// CanMergeOwnPRForTier returns whether agents of this tier can merge their own PRs.
func (c TierProtectionConfig) CanMergeOwnPRForTier(tier TrustTier) bool {
	if can, ok := c.CanMergeOwnPR[tier]; ok {
		return can
	}
	return false
}

// ============================================================================
// BranchProtectionEnforcer
// ============================================================================

// BranchProtectionEnforcer manages Forgejo branch protection rules
// based on agent trust tiers.
type BranchProtectionEnforcer struct {
	client *Client
	config TierProtectionConfig
}

// NewBranchProtectionEnforcer creates a new enforcer.
func NewBranchProtectionEnforcer(client *Client, config TierProtectionConfig) *BranchProtectionEnforcer {
	return &BranchProtectionEnforcer{
		client: client,
		config: config,
	}
}

// NewBranchProtectionEnforcerWithDefaults creates an enforcer with default config.
func NewBranchProtectionEnforcerWithDefaults(client *Client) *BranchProtectionEnforcer {
	return NewBranchProtectionEnforcer(client, DefaultTierProtectionConfig())
}

// Config returns the current protection configuration.
func (e *BranchProtectionEnforcer) Config() TierProtectionConfig {
	return e.config
}

// ============================================================================
// Branch Protection API Operations
// ============================================================================

// ConfigureBranch applies protection rules to a branch.
// Uses the Forgejo API: POST /repos/{owner}/{repo}/branch_protections
func (e *BranchProtectionEnforcer) ConfigureBranch(ctx context.Context, owner, repo string, rule BranchProtectionRule) error {
	path := fmt.Sprintf("/repos/%s/%s/branch_protections", owner, repo)
	body := map[string]interface{}{
		"branch_name":               rule.BranchName,
		"required_approvals":        rule.RequiredApprovals,
		"enable_push":               rule.EnablePush,
		"enable_push_whitelist":     rule.EnablePushWhitelist,
		"enable_merge_whitelist":    rule.EnableMergeWhitelist,
		"block_on_rejected_reviews": rule.BlockOnRejectedReviews,
		"block_on_outdated_branch":  rule.BlockOnOutdatedBranch,
		"required_status_checks":    rule.RequiredStatusChecks,
	}
	if len(rule.PushWhitelistUsers) > 0 {
		body["push_whitelist_user_teams"] = rule.PushWhitelistUsers
	}
	if len(rule.MergeWhitelistUsers) > 0 {
		body["merge_whitelist_user_teams"] = rule.MergeWhitelistUsers
	}

	return e.client.doRequest(ctx, "POST", path, body, nil)
}

// DeleteBranchProtection removes all protection from a branch.
func (e *BranchProtectionEnforcer) DeleteBranchProtection(ctx context.Context, owner, repo, branch string) error {
	path := fmt.Sprintf("/repos/%s/%s/branch_protections/%s", owner, repo, branch)
	return e.client.doRequest(ctx, "DELETE", path, nil, nil)
}

// ============================================================================
// Agent Permission Rules
// ============================================================================

// AgentPushAllowed checks whether an agent with the given tier can push
// to the specified branch.
func (e *BranchProtectionEnforcer) AgentPushAllowed(agentID, branch string, tier TrustTier) bool {
	// Agents can always push to feat/* branches
	if isFeatureBranch(branch) {
		return true
	}
	// No agent can push directly to main
	if branch == "main" || branch == "master" {
		return false
	}
	// Release branches require at least Trusted
	if isReleaseBranch(branch) {
		return tier == TierTrusted || tier == TierVeteran
	}
	// Other branches: allow for all agents
	return true
}

// AgentMergeAllowed checks whether an agent can merge a PR.
func (e *BranchProtectionEnforcer) AgentMergeAllowed(agentID string, tier TrustTier, isOwnPR bool) bool {
	// For own PRs: only Veteran can merge their own PRs
	if isOwnPR {
		return e.config.CanMergeOwnPRForTier(tier)
	}
	// Non-own PRs: all agents can merge if approvals are met (the branch
	// protection rule enforces the count; the enforcer just decides eligibility)
	return true
}

// RequiredApprovalsForTier returns the number of approvals needed.
func (e *BranchProtectionEnforcer) RequiredApprovalsForTier(tier TrustTier) int {
	return e.config.RequiredApprovalsForTier(tier)
}

// ============================================================================
// ApplyTierProtection — main entry point
// ============================================================================

// ApplyTierProtection creates or updates the branch protection rules
// for a repo based on the agent tier context.
//
// This is called when an agent's tier changes or when a new repo is created.
// It ensures the protection rules match the spec for the given tier context.
func (e *BranchProtectionEnforcer) ApplyTierProtection(ctx context.Context, owner, repo, agentID string, tier TrustTier) error {
	rule := BranchProtectionRule{
		BranchName:             "main",
		RequiredApprovals:      e.config.RequiredApprovalsForTier(tier),
		RequiredStatusChecks:   e.config.RequiredStatusChecks,
		EnablePush:             false, // No direct push to main
		EnablePushWhitelist:    true,
		PushWhitelistUsers:     []string{}, // Only humans can push to main
		BlockOnRejectedReviews: true,
		BlockOnOutdatedBranch:  true,
	}

	return e.ConfigureBranch(ctx, owner, repo, rule)
}

// CreateFeatureBranchRule creates protection for a feature branch.
// Feature branches allow the owning agent to push but require a PR for merge.
func (e *BranchProtectionEnforcer) CreateFeatureBranchRule(ctx context.Context, owner, repo, branchName, agentID string, tier TrustTier) error {
	rule := BranchProtectionRule{
		BranchName:             branchName,
		RequiredApprovals:      e.config.RequiredApprovalsForTier(tier),
		RequiredStatusChecks:   e.config.RequiredStatusChecks,
		EnablePush:             true,
		EnablePushWhitelist:    true,
		PushWhitelistUsers:     []string{agentID},
		BlockOnRejectedReviews: true,
	}

	return e.ConfigureBranch(ctx, owner, repo, rule)
}

// ============================================================================
// Helpers
// ============================================================================

// isFeatureBranch returns true for feat/* or feature/* branches.
func isFeatureBranch(branch string) bool {
	return strings.HasPrefix(branch, "feat/") || strings.HasPrefix(branch, "feature/")
}

// isReleaseBranch returns true for release/* branches.
func isReleaseBranch(branch string) bool {
	return len(branch) > 8 && branch[:8] == "release/"
}
