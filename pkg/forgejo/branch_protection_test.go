package forgejo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ============================================================================
// TierProtectionConfig
// ============================================================================

func TestDefaultTierProtectionConfig(t *testing.T) {
	c := DefaultTierProtectionConfig()

	tests := []struct {
		tier     TrustTier
		expected int
	}{
		{TierProvisional, 2},
		{TierObserved, 2},
		{TierTrusted, 1},
		{TierVeteran, 1},
	}

	for _, tt := range tests {
		if got := c.RequiredApprovalsForTier(tt.tier); got != tt.expected {
			t.Errorf("tier %s: RequiredApprovalsForTier = %d, want %d", tt.tier, got, tt.expected)
		}
	}
}

func TestDefaultTierProtectionConfig_CanMergeOwnPR(t *testing.T) {
	c := DefaultTierProtectionConfig()

	if c.CanMergeOwnPRForTier(TierProvisional) {
		t.Error("Provisional should NOT merge own PRs")
	}
	if c.CanMergeOwnPRForTier(TierVeteran) {
		// Veteran CAN merge own PRs
	} else {
		t.Error("Veteran should merge own PRs")
	}
}

func TestRequiredApprovalsForTier_UnknownTier(t *testing.T) {
	c := DefaultTierProtectionConfig()
	if got := c.RequiredApprovalsForTier(TrustTier("unknown")); got != 2 {
		t.Errorf("expected default 2 for unknown tier, got %d", got)
	}
}

func TestCanMergeOwnPRForTier_UnknownTier(t *testing.T) {
	c := DefaultTierProtectionConfig()
	if c.CanMergeOwnPRForTier(TrustTier("unknown")) {
		t.Error("expected false for unknown tier")
	}
}

// ============================================================================
// BranchProtectionEnforcer — AgentPushAllowed
// ============================================================================

func TestAgentPushAllowed_FeatureBranch(t *testing.T) {
	e := NewBranchProtectionEnforcerWithDefaults(nil)

	tests := []struct {
		branch string
		tier   TrustTier
		allow  bool
	}{
		{"feat/abc", TierProvisional, true},
		{"feat/my-feature", TierVeteran, true},
		{"feature/long-name", TierObserved, true},
		{"feat/", TierTrusted, true},
	}

	for _, tt := range tests {
		if got := e.AgentPushAllowed("agent-1", tt.branch, tt.tier); got != tt.allow {
			t.Errorf("branch %s tier %s: got %v, want %v", tt.branch, tt.tier, got, tt.allow)
		}
	}
}

func TestAgentPushAllowed_MainBranch(t *testing.T) {
	e := NewBranchProtectionEnforcerWithDefaults(nil)

	if e.AgentPushAllowed("agent-1", "main", TierVeteran) {
		t.Error("Veteran should NOT push to main")
	}
	if e.AgentPushAllowed("agent-1", "master", TierVeteran) {
		t.Error("Veteran should NOT push to master")
	}
}

func TestAgentPushAllowed_ReleaseBranch(t *testing.T) {
	e := NewBranchProtectionEnforcerWithDefaults(nil)

	if !e.AgentPushAllowed("agent-1", "release/v1.0", TierTrusted) {
		t.Error("Trusted should push to release/*")
	}
	if !e.AgentPushAllowed("agent-1", "release/v2.0", TierVeteran) {
		t.Error("Veteran should push to release/*")
	}
	if e.AgentPushAllowed("agent-1", "release/v1.0", TierObserved) {
		t.Error("Observed should NOT push to release/*")
	}
	if e.AgentPushAllowed("agent-1", "release/v1.0", TierProvisional) {
		t.Error("Provisional should NOT push to release/*")
	}
}

func TestAgentPushAllowed_OtherBranch(t *testing.T) {
	e := NewBranchProtectionEnforcerWithDefaults(nil)

	if !e.AgentPushAllowed("agent-1", "develop", TierProvisional) {
		t.Error("Provisional should push to develop")
	}
	if !e.AgentPushAllowed("agent-1", "hotfix/bug", TierObserved) {
		t.Error("Observed should push to hotfix/*")
	}
}

// ============================================================================
// BranchProtectionEnforcer — AgentMergeAllowed
// ============================================================================

func TestAgentMergeAllowed_OwnPR_Veteran(t *testing.T) {
	e := NewBranchProtectionEnforcerWithDefaults(nil)
	if !e.AgentMergeAllowed("agent-1", TierVeteran, true) {
		t.Error("Veteran should merge own PRs")
	}
}

func TestAgentMergeAllowed_OwnPR_Provisional(t *testing.T) {
	e := NewBranchProtectionEnforcerWithDefaults(nil)
	if e.AgentMergeAllowed("agent-1", TierProvisional, true) {
		t.Error("Provisional should NOT merge own PRs")
	}
}

func TestAgentMergeAllowed_NonOwnPR(t *testing.T) {
	e := NewBranchProtectionEnforcerWithDefaults(nil)
	// All agents can merge non-own PRs if approvals are met
	if !e.AgentMergeAllowed("agent-1", TierProvisional, false) {
		t.Error("Provisional should be eligible to merge non-own PRs")
	}
}

func TestRequiredApprovalsForTier(t *testing.T) {
	e := NewBranchProtectionEnforcerWithDefaults(nil)

	tests := []struct {
		tier     TrustTier
		expected int
	}{
		{TierProvisional, 2},
		{TierObserved, 2},
		{TierTrusted, 1},
		{TierVeteran, 1},
	}

	for _, tt := range tests {
		if got := e.RequiredApprovalsForTier(tt.tier); got != tt.expected {
			t.Errorf("tier %s: got %d, want %d", tt.tier, got, tt.expected)
		}
	}
}

// ============================================================================
// BranchProtectionEnforcer — ConfigureBranch (HTTP)
// ============================================================================

func TestConfigureBranch_Success(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/branch_protections") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)

		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := NewClient(server.URL, "admin", "pass")
	enforcer := NewBranchProtectionEnforcerWithDefaults(client)

	rule := BranchProtectionRule{
		BranchName:        "main",
		RequiredApprovals: 2,
		EnablePush:        false,
	}

	err := enforcer.ConfigureBranch(context.Background(), "owner", "repo", rule)
	if err != nil {
		t.Fatalf("ConfigureBranch failed: %v", err)
	}

	if receivedBody["branch_name"] != "main" {
		t.Errorf("expected branch_name=main, got %v", receivedBody["branch_name"])
	}
}

func TestConfigureBranch_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL, "admin", "pass")
	enforcer := NewBranchProtectionEnforcerWithDefaults(client)

	err := enforcer.ConfigureBranch(context.Background(), "owner", "repo", BranchProtectionRule{
		BranchName: "main",
	})
	if err == nil {
		t.Error("expected error on 500 response")
	}
}

// ============================================================================
// BranchProtectionEnforcer — ApplyTierProtection
// ============================================================================

func TestApplyTierProtection_Success(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := NewClient(server.URL, "admin", "pass")
	enforcer := NewBranchProtectionEnforcerWithDefaults(client)

	err := enforcer.ApplyTierProtection(context.Background(), "owner", "repo", "agent-1", TierTrusted)
	if err != nil {
		t.Fatalf("ApplyTierProtection failed: %v", err)
	}

	// Trusted tier should require 1 approval
	if receivedBody["required_approvals"].(float64) != 1 {
		t.Errorf("expected 1 approval for Trusted, got %v", receivedBody["required_approvals"])
	}
	// main should not allow direct push
	if receivedBody["enable_push"].(bool) {
		t.Error("expected enable_push=false for main")
	}
}

func TestApplyTierProtection_Provisional(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := NewClient(server.URL, "admin", "pass")
	enforcer := NewBranchProtectionEnforcerWithDefaults(client)

	err := enforcer.ApplyTierProtection(context.Background(), "owner", "repo", "agent-1", TierProvisional)
	if err != nil {
		t.Fatalf("ApplyTierProtection failed: %v", err)
	}

	// Provisional should require 2 approvals
	if receivedBody["required_approvals"].(float64) != 2 {
		t.Errorf("expected 2 approvals for Provisional, got %v", receivedBody["required_approvals"])
	}
}

// ============================================================================
// BranchProtectionEnforcer — CreateFeatureBranchRule
// ============================================================================

func TestCreateFeatureBranchRule_Success(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := NewClient(server.URL, "admin", "pass")
	enforcer := NewBranchProtectionEnforcerWithDefaults(client)

	err := enforcer.CreateFeatureBranchRule(context.Background(), "owner", "repo", "feat/auth", "agent-7", TierObserved)
	if err != nil {
		t.Fatalf("CreateFeatureBranchRule failed: %v", err)
	}

	if receivedBody["branch_name"] != "feat/auth" {
		t.Errorf("expected branch_name=feat/auth, got %v", receivedBody["branch_name"])
	}
	// Feature branches allow push
	if !receivedBody["enable_push"].(bool) {
		t.Error("expected enable_push=true for feature branch")
	}
}

// ============================================================================
// BranchProtectionEnforcer — DeleteBranchProtection
// ============================================================================

func TestDeleteBranchProtection_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL, "admin", "pass")
	enforcer := NewBranchProtectionEnforcerWithDefaults(client)

	err := enforcer.DeleteBranchProtection(context.Background(), "owner", "repo", "main")
	if err != nil {
		t.Fatalf("DeleteBranchProtection failed: %v", err)
	}
}

func TestDeleteBranchProtection_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.URL, "admin", "pass")
	enforcer := NewBranchProtectionEnforcerWithDefaults(client)

	err := enforcer.DeleteBranchProtection(context.Background(), "owner", "repo", "nonexistent")
	if err == nil {
		t.Error("expected error on 404")
	}
}

// ============================================================================
// Custom Config
// ============================================================================

func TestCustomTierProtectionConfig(t *testing.T) {
	customConfig := TierProtectionConfig{
		RequiredApprovals: map[TrustTier]int{
			TierProvisional: 3,
			TierObserved:    2,
			TierTrusted:     1,
			TierVeteran:     0,
		},
		RequiredStatusChecks: []string{"custom-check"},
		CanMergeOwnPR: map[TrustTier]bool{
			TierProvisional: false,
			TierObserved:    false,
			TierTrusted:     true,
			TierVeteran:     true,
		},
	}

	e := NewBranchProtectionEnforcer(nil, customConfig)

	if e.RequiredApprovalsForTier(TierProvisional) != 3 {
		t.Error("expected 3 for custom Provisional")
	}
	if e.RequiredApprovalsForTier(TierVeteran) != 0 {
		t.Error("expected 0 for custom Veteran")
	}
	if !e.config.CanMergeOwnPRForTier(TierTrusted) {
		t.Error("expected Trusted can merge own PRs in custom config")
	}
}

// ============================================================================
// Config getter
// ============================================================================

func TestBranchProtectionEnforcer_Config(t *testing.T) {
	config := DefaultTierProtectionConfig()
	config.RequiredApprovals[TierProvisional] = 5

	e := NewBranchProtectionEnforcer(nil, config)
	c := e.Config()

	if c.RequiredApprovals[TierProvisional] != 5 {
		t.Error("Config() should return the config it was created with")
	}
}

// ============================================================================
// Helpers
// ============================================================================

func TestIsFeatureBranch(t *testing.T) {
	tests := []struct {
		branch string
		expect bool
	}{
		{"feat/abc", true},
		{"feature/long", true},
		{"feat/", true},
		{"main", false},
		{"develop", false},
		{"release/v1", false},
		{"fe", false},
	}

	for _, tt := range tests {
		if got := isFeatureBranch(tt.branch); got != tt.expect {
			t.Errorf("isFeatureBranch(%s) = %v, want %v", tt.branch, got, tt.expect)
		}
	}
}

func TestIsReleaseBranch(t *testing.T) {
	tests := []struct {
		branch string
		expect bool
	}{
		{"release/v1.0", true},
		{"release/v2", true},
		{"feat/abc", false},
		{"main", false},
		{"releas", false},
	}

	for _, tt := range tests {
		if got := isReleaseBranch(tt.branch); got != tt.expect {
			t.Errorf("isReleaseBranch(%s) = %v, want %v", tt.branch, got, tt.expect)
		}
	}
}

// ============================================================================
// NewBranchProtectionEnforcerWithDefaults
// ============================================================================

func TestNewBranchProtectionEnforcerWithDefaults(t *testing.T) {
	e := NewBranchProtectionEnforcerWithDefaults(nil)
	if e == nil {
		t.Fatal("expected non-nil enforcer")
	}
	if e.RequiredApprovalsForTier(TierProvisional) != 2 {
		t.Error("expected default Provisional approvals = 2")
	}
}
