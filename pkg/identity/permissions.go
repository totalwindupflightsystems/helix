package identity

import (
	"fmt"
	"sort"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

// PermissionSet describes the Forgejo permissions granted to an agent at a
// given trust tier. Permissions expand monotonically — each tier includes
// all permissions from lower tiers plus new ones.
//
// Spec: specs/trust-model.md §Trust Tiers + §Integration Points:
// "Forgejo permissions expand with trust tier."
type PermissionSet struct {
	Tier trust.TrustTier `json:"tier"`

	// Repository permissions
	CanReadRepos      bool `json:"can_read_repos"`
	CanCreateBranches bool `json:"can_create_branches"`
	CanMergeOwnPRs    bool `json:"can_merge_own_prs"`
	CanCreateRepos    bool `json:"can_create_repos"`
	CanDeleteRepos    bool `json:"can_delete_repos"`
	HasAdminAccess    bool `json:"has_admin_access"`

	// Code review permissions
	CanReviewOthersPRs     bool `json:"can_review_others_prs"`      // advisory
	CanBindingCertify      bool `json:"can_binding_certify"`        // binding, counts toward consensus
	CanInitiateNegotiation bool `json:"can_initiate_negotiation"`   // PR negotiation with other agents

	// Infrastructure permissions
	CanModifyNonSecIaC     bool `json:"can_modify_non_sec_iac"`     // under review
	CanModifySecIaC        bool `json:"can_modify_sec_iac"`         // under adversarial review
	CanModifyCICD          bool `json:"can_modify_cicd"`
	CanModifySecurityPolicy bool `json:"can_modify_security_policy"`

	// Environment access
	CanAccessIntegrationEnv bool `json:"can_access_integration_env"`
	CanAccessStagingEnv     bool `json:"can_access_staging_env"`
	CanAccessProductionEnv  bool `json:"can_access_production_env"`

	// Cost management
	CostCapPerJob float64 `json:"cost_cap_per_job"` // USD, -1 = unlimited

	// Sandbox
	SandboxLevel string `json:"sandbox_level"` // "full", "limited", "none"
}

// PermissionExpansion maps trust tiers to Forgejo permission sets.
// It is the authoritative mapping between an agent's earned trust and
// the permissions they hold in the platform.
type PermissionExpansion struct{}

// NewPermissionExpansion creates a PermissionExpansion.
func NewPermissionExpansion() *PermissionExpansion {
	return &PermissionExpansion{}
}

// PermissionsForTier returns the permission set for a given trust tier.
// Each tier includes all permissions from lower tiers plus new capabilities.
func (pe *PermissionExpansion) PermissionsForTier(tier trust.TrustTier) (PermissionSet, error) {
	switch tier {
	case trust.TierProvisional:
		return provisionalPermissions(), nil
	case trust.TierObserved:
		return observedPermissions(), nil
	case trust.TierTrusted:
		return trustedPermissions(), nil
	case trust.TierVeteran:
		return veteranPermissions(), nil
	default:
		return PermissionSet{}, fmt.Errorf("unknown trust tier: %s", tier)
	}
}

// provisionalPermissions returns the Tier 0 permission set.
// Spec §Tier 0: read-only + own branches, full sandbox, $5/job cap,
// cannot modify IaC/CI-CD/security.
func provisionalPermissions() PermissionSet {
	return PermissionSet{
		Tier:              trust.TierProvisional,
		CanReadRepos:      true,
		CanCreateBranches: false, // provisional agents work in assigned branches only
		CostCapPerJob:     5.0,
		SandboxLevel:      "full",
	}
}

// observedPermissions returns the Tier 1 permission set.
// Spec §Tier 1: create branches + PRs, modify non-security IaC under review,
// limited cache sharing, $25/job cap.
func observedPermissions() PermissionSet {
	p := provisionalPermissions()
	p.Tier = trust.TierObserved
	p.CanCreateBranches = true          // "can create and manage feature branches autonomously"
	p.CanModifyNonSecIaC = true         // "can modify non-security IaC under review"
	p.CanAccessIntegrationEnv = false   // not yet — that's Tier 2
	p.CostCapPerJob = 25.0
	p.SandboxLevel = "limited"
	return p
}

// trustedPermissions returns the Tier 2 permission set.
// Spec §Tier 2: merge own PRs, review others (advisory), initiate negotiation,
// access integration env, $100/job cap.
func trustedPermissions() PermissionSet {
	p := observedPermissions()
	p.Tier = trust.TierTrusted
	p.CanMergeOwnPRs = true
	p.CanReviewOthersPRs = true       // advisory
	p.CanInitiateNegotiation = true
	p.CanAccessIntegrationEnv = true
	p.CostCapPerJob = 100.0
	return p
}

// veteranPermissions returns the Tier 3 permission set.
// Spec §Tier 3: binding certification, create/delete repos, admin access,
// modify CI/CD + security policy under adversarial review, staging access,
// no cost cap, production access (monitoring only).
func veteranPermissions() PermissionSet {
	p := trustedPermissions()
	p.Tier = trust.TierVeteran
	p.CanBindingCertify = true
	p.CanCreateRepos = true
	p.CanDeleteRepos = true
	p.HasAdminAccess = true
	p.CanModifySecIaC = true
	p.CanModifyCICD = true
	p.CanModifySecurityPolicy = true
	p.CanAccessStagingEnv = true
	p.CostCapPerJob = -1 // unlimited (monitoring only)
	p.SandboxLevel = "none"
	return p
}

// TierTransition represents a change in an agent's trust tier.
// When an agent is promoted or demoted, their permissions must be updated
// to match the new tier.
type TierTransition struct {
	AgentID  string          `json:"agent_id"`
	OldTier  trust.TrustTier `json:"old_tier"`
	NewTier  trust.TrustTier `json:"new_tier"`
	Reason   string          `json:"reason"`
}

// IsPromotion returns true if the transition is a tier increase.
func (t TierTransition) IsPromotion() bool {
	return tierRank(t.NewTier) > tierRank(t.OldTier)
}

// IsDemotion returns true if the transition is a tier decrease.
func (t TierTransition) IsDemotion() bool {
	return tierRank(t.NewTier) < tierRank(t.OldTier)
}

// PermissionDelta describes the permission changes resulting from a
// tier transition. Granted permissions are those newly enabled;
// Revoked permissions are those newly disabled.
type PermissionDelta struct {
	Granted  []string `json:"granted"`
	Revoked  []string `json:"revoked"`
}

// ComputeDelta returns the permission delta for a tier transition.
func (pe *PermissionExpansion) ComputeDelta(t TierTransition) (PermissionDelta, error) {
	oldPerms, err := pe.PermissionsForTier(t.OldTier)
	if err != nil {
		return PermissionDelta{}, fmt.Errorf("old tier %s: %w", t.OldTier, err)
	}
	newPerms, err := pe.PermissionsForTier(t.NewTier)
	if err != nil {
		return PermissionDelta{}, fmt.Errorf("new tier %s: %w", t.NewTier, err)
	}

	return diffPermissionSets(oldPerms, newPerms), nil
}

// HandleTransition processes a tier transition and returns the resulting
// permission delta. In a real deployment, this would trigger Forgejo API
// calls to update the agent's permissions.
func (pe *PermissionExpansion) HandleTransition(t TierTransition) (PermissionDelta, error) {
	delta, err := pe.ComputeDelta(t)
	if err != nil {
		return PermissionDelta{}, err
	}
	// In production: call ForgejoClient.UpdateUserPermissions(agentID, newPerms)
	// For now, just return the computed delta.
	return delta, nil
}

// CostCapForTier returns the maximum cost per job for the given tier.
// Returns -1 for unlimited (Veteran tier).
func (pe *PermissionExpansion) CostCapForTier(tier trust.TrustTier) (float64, error) {
	perms, err := pe.PermissionsForTier(tier)
	if err != nil {
		return 0, err
	}
	return perms.CostCapPerJob, nil
}

// CanPerformAction checks whether an agent at the given tier can perform
// a specific action (identified by a permission field name).
func (pe *PermissionExpansion) CanPerformAction(tier trust.TrustTier, action string) (bool, error) {
	perms, err := pe.PermissionsForTier(tier)
	if err != nil {
		return false, err
	}
	return perms.Can(action), nil
}

// Can reports whether the permission set allows the named action.
func (p PermissionSet) Can(action string) bool {
	switch strings.ToLower(action) {
	case "read_repos", "read":
		return p.CanReadRepos
	case "create_branches", "branch":
		return p.CanCreateBranches
	case "merge_own_prs", "merge":
		return p.CanMergeOwnPRs
	case "create_repos":
		return p.CanCreateRepos
	case "delete_repos":
		return p.CanDeleteRepos
	case "admin":
		return p.HasAdminAccess
	case "review_others_prs", "review":
		return p.CanReviewOthersPRs
	case "binding_certify", "certify":
		return p.CanBindingCertify
	case "initiate_negotiation", "negotiate":
		return p.CanInitiateNegotiation
	case "modify_non_sec_iac", "modify_iac":
		return p.CanModifyNonSecIaC
	case "modify_sec_iac":
		return p.CanModifySecIaC
	case "modify_cicd", "cicd":
		return p.CanModifyCICD
	case "modify_security_policy", "security_policy":
		return p.CanModifySecurityPolicy
	case "access_integration_env", "integration_env":
		return p.CanAccessIntegrationEnv
	case "access_staging_env", "staging_env":
		return p.CanAccessStagingEnv
	case "access_production_env", "production_env":
		return p.CanAccessProductionEnv
	default:
		return false
	}
}

// Summary returns a human-readable summary of the permission set.
func (p PermissionSet) Summary() string {
	actions := []string{}
	if p.CanReadRepos {
		actions = append(actions, "read")
	}
	if p.CanCreateBranches {
		actions = append(actions, "create-branches")
	}
	if p.CanMergeOwnPRs {
		actions = append(actions, "merge-own-prs")
	}
	if p.CanCreateRepos {
		actions = append(actions, "create-repos")
	}
	if p.CanDeleteRepos {
		actions = append(actions, "delete-repos")
	}
	if p.HasAdminAccess {
		actions = append(actions, "admin")
	}
	if p.CanReviewOthersPRs {
		actions = append(actions, "review-others")
	}
	if p.CanBindingCertify {
		actions = append(actions, "binding-certify")
	}
	if p.CanInitiateNegotiation {
		actions = append(actions, "negotiate")
	}
	if p.CanModifyNonSecIaC {
		actions = append(actions, "modify-iac")
	}
	if p.CanModifyCICD {
		actions = append(actions, "cicd")
	}
	if p.CanModifySecurityPolicy {
		actions = append(actions, "security-policy")
	}
	if p.CanAccessIntegrationEnv {
		actions = append(actions, "integration-env")
	}
	if p.CanAccessStagingEnv {
		actions = append(actions, "staging-env")
	}

	cost := "unlimited"
	if p.CostCapPerJob > 0 {
		cost = fmt.Sprintf("$%.0f/job", p.CostCapPerJob)
	}

	return fmt.Sprintf("tier=%s sandbox=%s cost=%s actions=[%s]",
		p.Tier, p.SandboxLevel, cost, strings.Join(actions, ", "))
}

// tierRank returns a numeric rank for comparison (higher = more trusted).
func tierRank(t trust.TrustTier) int {
	switch t {
	case trust.TierProvisional:
		return 0
	case trust.TierObserved:
		return 1
	case trust.TierTrusted:
		return 2
	case trust.TierVeteran:
		return 3
	default:
		return -1
	}
}

// diffPermissionSets compares two PermissionSets and returns the delta.
func diffPermissionSets(old, new PermissionSet) PermissionDelta {
	delta := PermissionDelta{}

	// Compare each boolean field
	compare := func(name string, oldVal, newVal bool) {
		if !oldVal && newVal {
			delta.Granted = append(delta.Granted, name)
		} else if oldVal && !newVal {
			delta.Revoked = append(delta.Revoked, name)
		}
	}

	compare("read_repos", old.CanReadRepos, new.CanReadRepos)
	compare("create_branches", old.CanCreateBranches, new.CanCreateBranches)
	compare("merge_own_prs", old.CanMergeOwnPRs, new.CanMergeOwnPRs)
	compare("create_repos", old.CanCreateRepos, new.CanCreateRepos)
	compare("delete_repos", old.CanDeleteRepos, new.CanDeleteRepos)
	compare("admin_access", old.HasAdminAccess, new.HasAdminAccess)
	compare("review_others_prs", old.CanReviewOthersPRs, new.CanReviewOthersPRs)
	compare("binding_certify", old.CanBindingCertify, new.CanBindingCertify)
	compare("initiate_negotiation", old.CanInitiateNegotiation, new.CanInitiateNegotiation)
	compare("modify_non_sec_iac", old.CanModifyNonSecIaC, new.CanModifyNonSecIaC)
	compare("modify_sec_iac", old.CanModifySecIaC, new.CanModifySecIaC)
	compare("modify_cicd", old.CanModifyCICD, new.CanModifyCICD)
	compare("modify_security_policy", old.CanModifySecurityPolicy, new.CanModifySecurityPolicy)
	compare("access_integration_env", old.CanAccessIntegrationEnv, new.CanAccessIntegrationEnv)
	compare("access_staging_env", old.CanAccessStagingEnv, new.CanAccessStagingEnv)
	compare("access_production_env", old.CanAccessProductionEnv, new.CanAccessProductionEnv)

	// Sort for deterministic output
	sort.Strings(delta.Granted)
	sort.Strings(delta.Revoked)

	return delta
}
