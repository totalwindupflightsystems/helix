package dispatcher

import (
	"errors"
	"testing"

	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

// Helper to quickly build an agent.
func agent(name string, tier trust.TrustTier, capability string, load, maxLoad int) AgentProfile {
	return AgentProfile{
		Name:        name,
		Tier:        tier,
		Capability:  capability,
		CurrentLoad: load,
		MaxLoad:     maxLoad,
		TrustScore:  0.5,
	}
}

// Helper to quickly build a task.
func task(id string, desc string, tier trust.TrustTier) Task {
	return Task{
		ID:           id,
		Description:  desc,
		RequiredTier: tier,
		Status:       StatusPending,
	}
}

// ——— Tier-ordinal and comparison ———

func TestCompareTiers_Same(t *testing.T) {
	if trust.CompareTiers(trust.TierObserved, trust.TierObserved) != 0 {
		t.Error("same tier should return 0")
	}
}

func TestCompareTiers_Ascending(t *testing.T) {
	if trust.CompareTiers(trust.TierProvisional, trust.TierVeteran) != -1 {
		t.Error("Provisional < Veteran should return -1")
	}
	if trust.CompareTiers(trust.TierObserved, trust.TierTrusted) != -1 {
		t.Error("Observed < Trusted should return -1")
	}
}

func TestCompareTiers_Descending(t *testing.T) {
	if trust.CompareTiers(trust.TierVeteran, trust.TierProvisional) != 1 {
		t.Error("Veteran > Provisional should return 1")
	}
	if trust.CompareTiers(trust.TierTrusted, trust.TierObserved) != 1 {
		t.Error("Trusted > Observed should return 1")
	}
}

func TestTierOrdinal_AllTiers(t *testing.T) {
	want := map[trust.TrustTier]int{
		trust.TierProvisional: 0,
		trust.TierObserved:    1,
		trust.TierTrusted:     2,
		trust.TierVeteran:     3,
	}
	for tier, ord := range want {
		if trust.TierOrdinal[tier] != ord {
			t.Errorf("TierOrdinal[%s] = %d, want %d", tier, trust.TierOrdinal[tier], ord)
		}
	}
}

// ——— AC-3.2.1: Agent with tier below required_tier is never auto-assigned ———

func TestAssignAgent_TierGate_ProvisionalBlockedFromTrusted(t *testing.T) {
	agents := []AgentProfile{
		agent("dtoole", trust.TierProvisional, "web", 0, 3),
		agent("wojons", trust.TierTrusted, "web", 1, 3),
	}
	tsk := task("task-001", "create login form", trust.TierTrusted)

	result, err := AssignAgent(tsk, agents)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.WorkItem.Agent.Name != "wojons" {
		t.Errorf("expected wojons (Trusted), got %s", result.WorkItem.Agent.Name)
	}
}

func TestAssignAgent_TierGate_NoAgentMeetsTier(t *testing.T) {
	agents := []AgentProfile{
		agent("dtoole", trust.TierProvisional, "web", 0, 3),
	}
	tsk := task("task-002", "deploy to production", trust.TierVeteran)

	_, err := AssignAgent(tsk, agents)
	if err == nil {
		t.Fatal("expected error when no agent meets required tier")
	}
	if !errors.Is(err, ErrTierTooLow) {
		t.Errorf("expected ErrTierTooLow, got %v", err)
	}
}

// ——— AC-3.2.3: Agent with highest trust + lowest load wins on tie ———

func TestAssignAgent_TierTie_LowestLoadWins(t *testing.T) {
	agents := []AgentProfile{
		agent("a-highload", trust.TierTrusted, "web", 2, 3),
		agent("b-lowload", trust.TierTrusted, "web", 0, 3),
		agent("c-midload", trust.TierTrusted, "web", 1, 3),
	}
	tsk := task("task-003", "update landing page", trust.TierTrusted)

	result, err := AssignAgent(tsk, agents)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.WorkItem.Agent.Name != "b-lowload" {
		t.Errorf("expected lowest-load agent b-lowload, got %s", result.WorkItem.Agent.Name)
	}
}

func TestAssignAgent_HighestTierWins(t *testing.T) {
	agents := []AgentProfile{
		agent("obs", trust.TierObserved, "web", 0, 3),
		agent("vet", trust.TierVeteran, "web", 1, 3),
		agent("trust", trust.TierTrusted, "web", 0, 3),
	}
	tsk := task("task-004", "security patch", trust.TierTrusted)

	result, err := AssignAgent(tsk, agents)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both trust and vet meet tier. Trust has load 0, vet has load 1.
	// obs is blocked (Observed < Trusted). Winner: trust (lowest load among tier-ok).
	if result.WorkItem.Agent.Name != "trust" {
		t.Errorf("expected trust (Trusted, load 0), got %s", result.WorkItem.Agent.Name)
	}
}

// ——— AC-3.2.5: Overloaded agent is skipped ———

func TestAssignAgent_OverloadedAgentSkipped(t *testing.T) {
	agents := []AgentProfile{
		agent("full", trust.TierTrusted, "web", 3, 3),
		agent("ok", trust.TierTrusted, "web", 1, 3),
	}
	tsk := task("task-005", "fix typo", trust.TierTrusted)

	result, err := AssignAgent(tsk, agents)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.WorkItem.Agent.Name != "ok" {
		t.Errorf("expected ok (has capacity), got %s", result.WorkItem.Agent.Name)
	}
}

// ——— Default tier: empty RequiredTier → Provisional ———

func TestAssignAgent_DefaultTier(t *testing.T) {
	agents := []AgentProfile{
		agent("dtoole", trust.TierProvisional, "web", 0, 3),
	}
	tsk := Task{
		ID:          "task-006",
		Description: "add comment",
		Status:      StatusPending,
		// RequiredTier is empty string — should default to Provisional
	}

	result, err := AssignAgent(tsk, agents)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.WorkItem.Agent.Name != "dtoole" {
		t.Errorf("expected dtoole, got %s", result.WorkItem.Agent.Name)
	}
}

// ——— No agents ———

func TestAssignAgent_NoAgents(t *testing.T) {
	tsk := task("task-007", "do something", trust.TierProvisional)
	_, err := AssignAgent(tsk, nil)
	if err == nil {
		t.Fatal("expected error with no agents")
	}
}

// ——— ValidateTierAssignment ———

func TestValidateTierAssignment_Ok(t *testing.T) {
	a := agent("trust", trust.TierTrusted, "go", 0, 3)
	tsk := task("t", "do thing", trust.TierObserved)
	if err := ValidateTierAssignment(a, tsk); err != nil {
		t.Errorf("trusted agent should be allowed for observed task: %v", err)
	}
}

func TestValidateTierAssignment_Blocked(t *testing.T) {
	a := agent("prov", trust.TierProvisional, "go", 0, 3)
	tsk := task("t", "do thing", trust.TierTrusted)
	err := ValidateTierAssignment(a, tsk)
	if err == nil {
		t.Fatal("provisional agent should be blocked from trusted task")
	}
	if !stringsFoldContains(err.Error(), "cannot self-assign above tier") {
		t.Errorf("error message should mention self-assign restriction, got: %v", err)
	}
}

func TestCanSelfAssign(t *testing.T) {
	a := agent("prov", trust.TierProvisional, "go", 0, 3)

	if !CanSelfAssign(a, task("t1", "docs", trust.TierProvisional)) {
		t.Error("provisional should self-assign to provisional task")
	}
	if CanSelfAssign(a, task("t2", "auth", trust.TierTrusted)) {
		t.Error("provisional should NOT self-assign to trusted task")
	}
}

// ——— File category classification ———

func TestClassifyFileCategory(t *testing.T) {
	tests := []struct {
		path string
		want FileCategory
	}{
		{"Dockerfile", CatInfrastructure},
		{"docker-compose.yml", CatInfrastructure},
		{"main.tf", CatInfrastructure},
		{"Makefile", CatInfrastructure},
		{".github/workflows/ci.yml", CatCICD},
		{".gitlab-ci.yml", CatCICD},
		{"pkg/auth/login.go", CatAuth},
		{"internal/identity/provisioner.go", CatAuth},
		{"pkg/oauth/handler.go", CatAuth},
		{"README.md", CatDocs},
		{"pkg/dispatcher/assigner.go", CatGeneral},
		{"pkg/review/dashboard.go", CatGeneral},
	}

	for _, tt := range tests {
		got := ClassifyFileCategory(tt.path)
		if got != tt.want {
			t.Errorf("ClassifyFileCategory(%q) = %s, want %s", tt.path, got, tt.want)
		}
	}
}

func TestRequiredTierForFiles(t *testing.T) {
	// Auth (Trusted) > Infra (Observed) > General (Provisional)
	tier := RequiredTierForFiles([]string{"pkg/auth/login.go", "main.tf", "README.md"})
	if tier != trust.TierTrusted {
		t.Errorf("expected Trusted (highest), got %s", tier)
	}

	// Only docs → Provisional
	tier = RequiredTierForFiles([]string{"README.md", "CHANGELOG.md"})
	if tier != trust.TierProvisional {
		t.Errorf("expected Provisional for docs-only, got %s", tier)
	}

	// CI/CD → Veteran
	tier = RequiredTierForFiles([]string{".github/workflows/deploy.yml"})
	if tier != trust.TierVeteran {
		t.Errorf("expected Veteran for CI/CD, got %s", tier)
	}
}

// ——— Helpers ———

func stringsFoldContains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) &&
		containsSubstringFold(s, sub)
}
