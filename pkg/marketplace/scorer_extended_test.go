package marketplace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// DailyRecalculation tests
// ---------------------------------------------------------------------------

// TestDailyRecalculation_EmptyDir tests that DailyRecalculation returns nil
// when the marketplace directory doesn't exist or has no agents.
func TestDailyRecalculation_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	err := DailyRecalculation(tmpDir)
	if err != nil {
		t.Errorf("expected nil error for empty dir, got: %v", err)
	}
}

// TestDailyRecalculation_NonexistentDir tests that a missing agents/
// subdirectory is handled gracefully (returns nil, not error).
func TestDailyRecalculation_NonexistentDir(t *testing.T) {
	err := DailyRecalculation("/tmp/nonexistent-marketplace-xyz-12345")
	if err != nil {
		t.Errorf("expected nil error for nonexistent dir, got: %v", err)
	}
}

// TestDailyRecalculation_EmptyString tests that an empty path returns an error.
func TestDailyRecalculation_EmptyString(t *testing.T) {
	err := DailyRecalculation("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

// TestDailyRecalculation_SingleAgent tests that a single agent manifest is
// correctly recalculated, with trust score and updated_at changed.
func TestDailyRecalculation_SingleAgent(t *testing.T) {
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}

	agent := Agent{
		Name:       "test-agent",
		Status:     StatusActive,
		TrustScore: 30, // base score
		Performance: Performance{
			TasksCompleted:   10,
			PrAcceptanceRate: 0.8, // 8 merged, 2 rejected
			BudgetAdherence:  0.95,
			ReviewAccuracy:   0.9,
		},
		Ratings: Ratings{
			Average: 4.0,
		},
		UpdatedAt: nowISO(),
	}

	writeAgentManifest(t, agentsDir, &agent)

	err := DailyRecalculation(tmpDir)
	if err != nil {
		t.Fatalf("DailyRecalculation: %v", err)
	}

	// Verify the manifest was updated.
	updated := readAgentManifest(t, agentsDir, "test-agent")
	if updated.TrustScore <= 30 {
		t.Errorf("trust score %d should be > 30 base with 8 merged PRs", updated.TrustScore)
	}
	if updated.UpdatedAt == "" {
		t.Error("updated_at should not be empty after recalculation")
	}

	// Expected: base(30) + min(40, 8*2)=16 - min(20, 2*3)=6 - 0 incidents
	//         + min(10, 4*2)=8 = 30+16-6+8 = 48
	if updated.TrustScore != 48 {
		t.Errorf("trust score = %d, want 48", updated.TrustScore)
	}
}

// TestDailyRecalculation_RetiredSkipped tests that retired agents are skipped.
func TestDailyRecalculation_RetiredSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}

	agent := Agent{
		Name:       "retired-agent",
		Status:     StatusRetired,
		TrustScore: 50,
		UpdatedAt:  nowISO(),
	}
	writeAgentManifest(t, agentsDir, &agent)

	err := DailyRecalculation(tmpDir)
	if err != nil {
		t.Fatalf("DailyRecalculation: %v", err)
	}

	updated := readAgentManifest(t, agentsDir, "retired-agent")
	if updated.TrustScore != 50 {
		t.Errorf("retired agent trust should be unchanged (50), got %d", updated.TrustScore)
	}
}

// TestDailyRecalculation_MultipleAgents tests recalculating multiple agents.
func TestDailyRecalculation_MultipleAgents(t *testing.T) {
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}

	// Agent 1: high performance
	agent1 := Agent{
		Name:       "high-performer",
		Status:     StatusActive,
		TrustScore: 30,
		Performance: Performance{
			TasksCompleted:   100,
			PrAcceptanceRate: 0.95,
			BudgetAdherence:  0.99,
		},
		UpdatedAt: nowISO(),
	}
	writeAgentManifest(t, agentsDir, &agent1)

	// Agent 2: low performance
	agent2 := Agent{
		Name:       "low-performer",
		Status:     StatusActive,
		TrustScore: 30,
		Performance: Performance{
			TasksCompleted:   20,
			PrAcceptanceRate: 0.3, // 6 merged, 14 rejected
			BudgetAdherence:  0.5, // lots of overruns
		},
		UpdatedAt: nowISO(),
	}
	writeAgentManifest(t, agentsDir, &agent2)

	err := DailyRecalculation(tmpDir)
	if err != nil {
		t.Fatalf("DailyRecalculation: %v", err)
	}

	high := readAgentManifest(t, agentsDir, "high-performer")
	low := readAgentManifest(t, agentsDir, "low-performer")

	if high.TrustScore <= low.TrustScore {
		t.Errorf("high performer (%d) should have higher trust than low performer (%d)",
			high.TrustScore, low.TrustScore)
	}
}

// TestDailyRecalculation_LogWritten tests that recalculation entries are
// appended to recalculation.log.
func TestDailyRecalculation_LogWritten(t *testing.T) {
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}

	agent := Agent{
		Name:       "logged-agent",
		Status:     StatusActive,
		TrustScore: 30,
		Performance: Performance{
			TasksCompleted:   5,
			PrAcceptanceRate: 1.0,
		},
		UpdatedAt: nowISO(),
	}
	writeAgentManifest(t, agentsDir, &agent)

	err := DailyRecalculation(tmpDir)
	if err != nil {
		t.Fatalf("DailyRecalculation: %v", err)
	}

	logPath := filepath.Join(tmpDir, "recalculation.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected recalculation.log to exist: %v", err)
	}
	if !strings.Contains(string(data), "logged-agent") {
		t.Errorf("log should contain agent name, got: %s", string(data))
	}
}

// TestDailyRecalculation_NoTasksBaseScore tests that an agent with no tasks
// gets the base score (30) minus decay.
func TestDailyRecalculation_NoTasksBaseScore(t *testing.T) {
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}

	agent := Agent{
		Name:       "new-agent",
		Status:     StatusActive,
		TrustScore: 50, // artificially high
		Performance: Performance{
			TasksCompleted: 0,
		},
		UpdatedAt: nowISO(),
	}
	writeAgentManifest(t, agentsDir, &agent)

	err := DailyRecalculation(tmpDir)
	if err != nil {
		t.Fatalf("DailyRecalculation: %v", err)
	}

	updated := readAgentManifest(t, agentsDir, "new-agent")
	// With 0 tasks and recent update, should get base score 30.
	if updated.TrustScore != 30 {
		t.Errorf("agent with no tasks should have base trust 30, got %d", updated.TrustScore)
	}
}

// TestDailyRecalculation_BudgetOverruns tests that budget overruns reduce trust.
func TestDailyRecalculation_BudgetOverruns(t *testing.T) {
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}

	// Agent with poor budget adherence.
	agent := Agent{
		Name:       "budget-buster",
		Status:     StatusActive,
		TrustScore: 30,
		Performance: Performance{
			TasksCompleted:   10,
			PrAcceptanceRate: 1.0,
			BudgetAdherence:  0.5, // 50% adherence → 5 overruns
		},
		UpdatedAt: nowISO(),
	}
	writeAgentManifest(t, agentsDir, &agent)

	err := DailyRecalculation(tmpDir)
	if err != nil {
		t.Fatalf("DailyRecalculation: %v", err)
	}

	updated := readAgentManifest(t, agentsDir, "budget-buster")
	// Expected: base(30) + min(40,10*2)=20 - 0 - min(30, 5*3)=15 + 0 = 35
	// (vs 50 without budget overruns)
	if updated.TrustScore >= 50 {
		t.Errorf("budget overruns should reduce trust below 50, got %d", updated.TrustScore)
	}
}

// TestDailyRecalculation_HumanRatingBonus tests that good ratings boost trust.
func TestDailyRecalculation_HumanRatingBonus(t *testing.T) {
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}

	agent := Agent{
		Name:       "rated-agent",
		Status:     StatusActive,
		TrustScore: 30,
		Performance: Performance{
			TasksCompleted:   10,
			PrAcceptanceRate: 1.0,
		},
		Ratings: Ratings{
			Average: 5.0,
		},
		UpdatedAt: nowISO(),
	}
	writeAgentManifest(t, agentsDir, &agent)

	err := DailyRecalculation(tmpDir)
	if err != nil {
		t.Fatalf("DailyRecalculation: %v", err)
	}

	updated := readAgentManifest(t, agentsDir, "rated-agent")
	// Expected: base(30) + min(40,20)=20 + min(10,5*2)=10 = 60
	if updated.TrustScore != 60 {
		t.Errorf("trust with 5-star rating = %d, want 60", updated.TrustScore)
	}
}

// TestDailyRecalculation_MalformedManifest tests that malformed YAML manifests
// are silently skipped.
func TestDailyRecalculation_MalformedManifest(t *testing.T) {
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}

	// Write a malformed YAML file.
	malformed := filepath.Join(agentsDir, "broken.yaml")
	if err := os.WriteFile(malformed, []byte("{{not yaml"), 0o644); err != nil {
		t.Fatalf("write malformed: %v", err)
	}

	// Also write a valid one.
	valid := Agent{
		Name:       "valid-agent",
		Status:     StatusActive,
		TrustScore: 30,
		Performance: Performance{
			TasksCompleted:   5,
			PrAcceptanceRate: 1.0,
		},
		UpdatedAt: nowISO(),
	}
	writeAgentManifest(t, agentsDir, &valid)

	err := DailyRecalculation(tmpDir)
	if err != nil {
		t.Fatalf("DailyRecalculation should not error on malformed manifest: %v", err)
	}

	// The valid agent should have been recalculated.
	updated := readAgentManifest(t, agentsDir, "valid-agent")
	if updated.TrustScore <= 30 {
		t.Errorf("valid agent trust should be > 30, got %d", updated.TrustScore)
	}
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

func writeAgentManifest(t *testing.T, dir string, agent *Agent) {
	t.Helper()
	data, err := yamlMarshal(agent)
	if err != nil {
		t.Fatalf("marshal agent: %v", err)
	}
	path := filepath.Join(dir, agent.Name+".yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func readAgentManifest(t *testing.T, dir, name string) *Agent {
	t.Helper()
	path := filepath.Join(dir, name+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var a Agent
	if err := yamlUnmarshal(data, &a); err != nil {
		t.Fatalf("unmarshal agent: %v", err)
	}
	return &a
}

// yamlMarshal wraps yaml.Marshal to avoid importing yaml in test.
func yamlMarshal(v interface{}) ([]byte, error) {
	return yaml.Marshal(v)
}

func yamlUnmarshal(data []byte, v interface{}) error {
	return yaml.Unmarshal(data, v)
}

// ---------------------------------------------------------------------------
// trustChangeLabel tests
// ---------------------------------------------------------------------------

func TestTrustChangeLabel(t *testing.T) {
	tests := []struct {
		old, new int
		want     string
	}{
		{50, 60, "+10 promoted"},
		{60, 50, "-10 demoted"},
		{50, 50, "unchanged"},
	}
	for _, tc := range tests {
		got := trustChangeLabel(tc.old, tc.new)
		if got != tc.want {
			t.Errorf("trustChangeLabel(%d, %d) = %q, want %q", tc.old, tc.new, got, tc.want)
		}
	}
}
