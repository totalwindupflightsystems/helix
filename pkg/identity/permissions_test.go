package identity

import (
	"testing"

	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

func TestNewPermissionExpansion(t *testing.T) {
	pe := NewPermissionExpansion()
	if pe == nil {
		t.Fatal("PermissionExpansion is nil")
	}
}

func TestPermissionsForTier_Provisional(t *testing.T) {
	pe := NewPermissionExpansion()
	perms, err := pe.PermissionsForTier(trust.TierProvisional)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Spec §Tier 0: read-only, full sandbox, $5/job
	if !perms.CanReadRepos {
		t.Error("provisional should be able to read repos")
	}
	if perms.CanCreateBranches {
		t.Error("provisional should NOT create branches")
	}
	if perms.CanMergeOwnPRs {
		t.Error("provisional should NOT merge own PRs")
	}
	if perms.CostCapPerJob != 5.0 {
		t.Errorf("cost cap = %.0f, want 5.0", perms.CostCapPerJob)
	}
	if perms.SandboxLevel != "full" {
		t.Errorf("sandbox level = %s, want 'full'", perms.SandboxLevel)
	}
}

func TestPermissionsForTier_Observed(t *testing.T) {
	pe := NewPermissionExpansion()
	perms, err := pe.PermissionsForTier(trust.TierObserved)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Spec §Tier 1: create branches, modify non-sec IaC, $25/job
	if !perms.CanReadRepos {
		t.Error("observed should be able to read repos")
	}
	if !perms.CanCreateBranches {
		t.Error("observed should create branches")
	}
	if !perms.CanModifyNonSecIaC {
		t.Error("observed should modify non-sec IaC")
	}
	if perms.CanMergeOwnPRs {
		t.Error("observed should NOT merge own PRs")
	}
	if perms.CostCapPerJob != 25.0 {
		t.Errorf("cost cap = %.0f, want 25.0", perms.CostCapPerJob)
	}
	if perms.SandboxLevel != "limited" {
		t.Errorf("sandbox level = %s, want 'limited'", perms.SandboxLevel)
	}
}

func TestPermissionsForTier_Trusted(t *testing.T) {
	pe := NewPermissionExpansion()
	perms, err := pe.PermissionsForTier(trust.TierTrusted)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Spec §Tier 2: merge own PRs, review others (advisory), negotiate, $100/job
	if !perms.CanMergeOwnPRs {
		t.Error("trusted should merge own PRs")
	}
	if !perms.CanReviewOthersPRs {
		t.Error("trusted should review others' PRs")
	}
	if !perms.CanInitiateNegotiation {
		t.Error("trusted should initiate negotiation")
	}
	if !perms.CanAccessIntegrationEnv {
		t.Error("trusted should access integration env")
	}
	if perms.CanBindingCertify {
		t.Error("trusted should NOT have binding certification")
	}
	if perms.CostCapPerJob != 100.0 {
		t.Errorf("cost cap = %.0f, want 100.0", perms.CostCapPerJob)
	}
}

func TestPermissionsForTier_Veteran(t *testing.T) {
	pe := NewPermissionExpansion()
	perms, err := pe.PermissionsForTier(trust.TierVeteran)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Spec §Tier 3: binding certify, create/delete repos, admin, staging, no cost cap
	if !perms.CanBindingCertify {
		t.Error("veteran should have binding certification")
	}
	if !perms.CanCreateRepos {
		t.Error("veteran should create repos")
	}
	if !perms.CanDeleteRepos {
		t.Error("veteran should delete repos")
	}
	if !perms.HasAdminAccess {
		t.Error("veteran should have admin access")
	}
	if !perms.CanModifyCICD {
		t.Error("veteran should modify CI/CD")
	}
	if !perms.CanModifySecurityPolicy {
		t.Error("veteran should modify security policy")
	}
	if !perms.CanAccessStagingEnv {
		t.Error("veteran should access staging env")
	}
	if perms.CostCapPerJob != -1 {
		t.Errorf("cost cap = %.0f, want -1 (unlimited)", perms.CostCapPerJob)
	}
	if perms.SandboxLevel != "none" {
		t.Errorf("sandbox level = %s, want 'none'", perms.SandboxLevel)
	}
}

func TestPermissionsForTier_Unknown(t *testing.T) {
	pe := NewPermissionExpansion()
	_, err := pe.PermissionsForTier(trust.TrustTier("unknown"))
	if err == nil {
		t.Error("expected error for unknown tier")
	}
}

func TestPermissionsForTier_MonotonicExpansion(t *testing.T) {
	pe := NewPermissionExpansion()
	tiers := []trust.TrustTier{trust.TierProvisional, trust.TierObserved, trust.TierTrusted, trust.TierVeteran}
	permSets := make([]PermissionSet, len(tiers))

	for i, tier := range tiers {
		perms, err := pe.PermissionsForTier(tier)
		if err != nil {
			t.Fatalf("PermissionsForTier(%s): %v", tier, err)
		}
		permSets[i] = perms
	}

	// Each higher tier should have at least as many permissions as the one below.
	// Count true boolean fields.
	countTrue := func(p PermissionSet) int {
		bools := []bool{
			p.CanReadRepos, p.CanCreateBranches, p.CanMergeOwnPRs,
			p.CanCreateRepos, p.CanDeleteRepos, p.HasAdminAccess,
			p.CanReviewOthersPRs, p.CanBindingCertify, p.CanInitiateNegotiation,
			p.CanModifyNonSecIaC, p.CanModifySecIaC, p.CanModifyCICD,
			p.CanModifySecurityPolicy, p.CanAccessIntegrationEnv,
			p.CanAccessStagingEnv, p.CanAccessProductionEnv,
		}
		c := 0
		for _, b := range bools {
			if b {
				c++
			}
		}
		return c
	}

	for i := 1; i < len(permSets); i++ {
		if countTrue(permSets[i]) < countTrue(permSets[i-1]) {
			t.Errorf("tier %s has fewer permissions than %s (monotonic expansion violated)",
				tiers[i], tiers[i-1])
		}
	}
}

func TestTierTransition_IsPromotion(t *testing.T) {
	tt := TierTransition{
		AgentID: "agent-a",
		OldTier: trust.TierProvisional,
		NewTier: trust.TierObserved,
	}
	if !tt.IsPromotion() {
		t.Error("provisional→observed should be a promotion")
	}
	if tt.IsDemotion() {
		t.Error("provisional→observed should NOT be a demotion")
	}
}

func TestTierTransition_IsDemotion(t *testing.T) {
	tt := TierTransition{
		AgentID: "agent-a",
		OldTier: trust.TierTrusted,
		NewTier: trust.TierObserved,
	}
	if !tt.IsDemotion() {
		t.Error("trusted→observed should be a demotion")
	}
	if tt.IsPromotion() {
		t.Error("trusted→observed should NOT be a promotion")
	}
}

func TestTierTransition_SameTier(t *testing.T) {
	tt := TierTransition{
		AgentID: "agent-a",
		OldTier: trust.TierObserved,
		NewTier: trust.TierObserved,
	}
	if tt.IsPromotion() {
		t.Error("same tier should NOT be a promotion")
	}
	if tt.IsDemotion() {
		t.Error("same tier should NOT be a demotion")
	}
}

func TestComputeDelta_Promotion(t *testing.T) {
	pe := NewPermissionExpansion()
	delta, err := pe.ComputeDelta(TierTransition{
		AgentID: "agent-a",
		OldTier: trust.TierProvisional,
		NewTier: trust.TierObserved,
	})
	if err != nil {
		t.Fatalf("ComputeDelta: %v", err)
	}

	if len(delta.Granted) == 0 {
		t.Error("promotion should grant new permissions")
	}
	if len(delta.Revoked) != 0 {
		t.Errorf("promotion should not revoke permissions, got %v", delta.Revoked)
	}

	// Should grant create_branches and modify_non_sec_iac
	grantedMap := make(map[string]bool)
	for _, g := range delta.Granted {
		grantedMap[g] = true
	}
	if !grantedMap["create_branches"] {
		t.Error("provisional→observed should grant create_branches")
	}
	if !grantedMap["modify_non_sec_iac"] {
		t.Error("provisional→observed should grant modify_non_sec_iac")
	}
}

func TestComputeDelta_Demotion(t *testing.T) {
	pe := NewPermissionExpansion()
	delta, err := pe.ComputeDelta(TierTransition{
		AgentID: "agent-a",
		OldTier: trust.TierTrusted,
		NewTier: trust.TierProvisional,
	})
	if err != nil {
		t.Fatalf("ComputeDelta: %v", err)
	}

	if len(delta.Revoked) == 0 {
		t.Error("demotion should revoke permissions")
	}
	if len(delta.Granted) != 0 {
		t.Errorf("demotion should not grant permissions, got %v", delta.Granted)
	}

	revokedMap := make(map[string]bool)
	for _, r := range delta.Revoked {
		revokedMap[r] = true
	}
	if !revokedMap["merge_own_prs"] {
		t.Error("trusted→provisional should revoke merge_own_prs")
	}
}

func TestComputeDelta_SameTier(t *testing.T) {
	pe := NewPermissionExpansion()
	delta, err := pe.ComputeDelta(TierTransition{
		AgentID: "agent-a",
		OldTier: trust.TierObserved,
		NewTier: trust.TierObserved,
	})
	if err != nil {
		t.Fatalf("ComputeDelta: %v", err)
	}

	if len(delta.Granted) != 0 || len(delta.Revoked) != 0 {
		t.Errorf("same tier transition should have no delta, granted=%v revoked=%v",
			delta.Granted, delta.Revoked)
	}
}

func TestComputeDelta_InvalidTier(t *testing.T) {
	pe := NewPermissionExpansion()
	_, err := pe.ComputeDelta(TierTransition{
		AgentID: "agent-a",
		OldTier: trust.TrustTier("unknown"),
		NewTier: trust.TierObserved,
	})
	if err == nil {
		t.Error("expected error for invalid tier")
	}
}

func TestHandleTransition(t *testing.T) {
	pe := NewPermissionExpansion()
	delta, err := pe.HandleTransition(TierTransition{
		AgentID: "agent-a",
		OldTier: trust.TierProvisional,
		NewTier: trust.TierVeteran,
		Reason:  "1000 successful merges, 0 incidents",
	})
	if err != nil {
		t.Fatalf("HandleTransition: %v", err)
	}

	if len(delta.Granted) == 0 {
		t.Error("provisional→veteran should grant many permissions")
	}
}

func TestCostCapForTier(t *testing.T) {
	pe := NewPermissionExpansion()

	tests := []struct {
		tier trust.TrustTier
		want float64
	}{
		{trust.TierProvisional, 5.0},
		{trust.TierObserved, 25.0},
		{trust.TierTrusted, 100.0},
		{trust.TierVeteran, -1.0}, // unlimited
	}

	for _, tc := range tests {
		got, err := pe.CostCapForTier(tc.tier)
		if err != nil {
			t.Fatalf("CostCapForTier(%s): %v", tc.tier, err)
		}
		if got != tc.want {
			t.Errorf("CostCapForTier(%s) = %.0f, want %.0f", tc.tier, got, tc.want)
		}
	}
}

func TestCostCapForTier_Invalid(t *testing.T) {
	pe := NewPermissionExpansion()
	_, err := pe.CostCapForTier(trust.TrustTier("bogus"))
	if err == nil {
		t.Error("expected error for invalid tier")
	}
}

func TestCanPerformAction(t *testing.T) {
	pe := NewPermissionExpansion()

	tests := []struct {
		tier   trust.TrustTier
		action string
		want   bool
	}{
		{trust.TierProvisional, "read_repos", true},
		{trust.TierProvisional, "create_branches", false},
		{trust.TierProvisional, "merge_own_prs", false},
		{trust.TierObserved, "create_branches", true},
		{trust.TierObserved, "modify_iac", true},
		{trust.TierObserved, "merge_own_prs", false},
		{trust.TierTrusted, "merge_own_prs", true},
		{trust.TierTrusted, "review_others_prs", true},
		{trust.TierTrusted, "binding_certify", false},
		{trust.TierVeteran, "binding_certify", true},
		{trust.TierVeteran, "admin", true},
		{trust.TierVeteran, "create_repos", true},
		{trust.TierVeteran, "delete_repos", true},
		{trust.TierVeteran, "cicd", true},
		{trust.TierVeteran, "security_policy", true},
		{trust.TierVeteran, "staging_env", true},
	}

	for _, tc := range tests {
		got, err := pe.CanPerformAction(tc.tier, tc.action)
		if err != nil {
			t.Fatalf("CanPerformAction(%s, %s): %v", tc.tier, tc.action, err)
		}
		if got != tc.want {
			t.Errorf("CanPerformAction(%s, %s) = %v, want %v", tc.tier, tc.action, got, tc.want)
		}
	}
}

func TestCanPerformAction_UnknownAction(t *testing.T) {
	pe := NewPermissionExpansion()
	got, err := pe.CanPerformAction(trust.TierProvisional, "bogus_action")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("unknown action should return false")
	}
}

func TestPermissionSet_Can(t *testing.T) {
	perms := PermissionSet{
		CanReadRepos:       true,
		CanCreateBranches:  false,
		CanMergeOwnPRs:     true,
		HasAdminAccess:     true,
	}

	if !perms.Can("read_repos") {
		t.Error("should allow read_repos")
	}
	if perms.Can("create_branches") {
		t.Error("should NOT allow create_branches")
	}
	if !perms.Can("merge_own_prs") {
		t.Error("should allow merge_own_prs")
	}
	if !perms.Can("admin") {
		t.Error("should allow admin")
	}
	if perms.Can("unknown") {
		t.Error("unknown action should be false")
	}
}

func TestPermissionSet_Can_CaseInsensitive(t *testing.T) {
	perms := PermissionSet{CanReadRepos: true}
	if !perms.Can("READ_REPOS") {
		t.Error("Can should be case-insensitive")
	}
	if !perms.Can("Read") {
		t.Error("Can should handle shorthand names")
	}
}

func TestPermissionSet_Summary(t *testing.T) {
	perms, _ := NewPermissionExpansion().PermissionsForTier(trust.TierVeteran)
	s := perms.Summary()
	if s == "" {
		t.Error("Summary should not be empty")
	}
	// Check it includes key fields
	if !contains(s, "tier=veteran") {
		t.Errorf("Summary should include tier: %s", s)
	}
	if !contains(s, "cost=unlimited") {
		t.Errorf("Summary should include cost: %s", s)
	}
}

func TestPermissionSet_Summary_Provisional(t *testing.T) {
	perms, _ := NewPermissionExpansion().PermissionsForTier(trust.TierProvisional)
	s := perms.Summary()
	if !contains(s, "tier=provisional") {
		t.Errorf("Summary should include tier: %s", s)
	}
	if !contains(s, "sandbox=full") {
		t.Errorf("Summary should include sandbox level: %s", s)
	}
	if !contains(s, "$5") {
		t.Errorf("Summary should include cost cap: %s", s)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > 0 && (startsWith(s, substr) ||
			containsFrom(s, substr, 1))))
}

func startsWith(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}

func containsFrom(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
