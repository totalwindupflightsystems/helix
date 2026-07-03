package estimate

import (
	"testing"
)

func TestCostAttributionRecordAndQuery(t *testing.T) {
	m := NewCostAttributionModel()

	entry := CostEntry{
		Attribute: CostAttribute{
			Namespace:     "owner/repo-a",
			Agent:         "agent-1",
			Task:          "PR-42",
			PromptVersion: "sha256:abc123",
			Model:         "deepseek-v4-flash",
		},
		CostUSD:   0.50,
		TokensIn:  10000,
		TokensOut: 2000,
		Timestamp: "2026-07-03T12:00:00Z",
	}

	m.RecordCost(entry)

	if m.AgentCost("agent-1") != 0.50 {
		t.Errorf("AgentCost = %v, want 0.50", m.AgentCost("agent-1"))
	}
	if m.RepoCost("owner/repo-a") != 0.50 {
		t.Errorf("RepoCost = %v, want 0.50", m.RepoCost("owner/repo-a"))
	}
	if m.PlatformCost() != 0.50 {
		t.Errorf("PlatformCost = %v, want 0.50", m.PlatformCost())
	}
}

func TestCostAttributionMultipleEntries(t *testing.T) {
	m := NewCostAttributionModel()

	m.RecordCost(CostEntry{
		Attribute: CostAttribute{Namespace: "owner/repo-a", Agent: "agent-1", Model: "model-a"},
		CostUSD:   0.30,
	})
	m.RecordCost(CostEntry{
		Attribute: CostAttribute{Namespace: "owner/repo-a", Agent: "agent-1", Model: "model-b"},
		CostUSD:   0.70,
	})
	m.RecordCost(CostEntry{
		Attribute: CostAttribute{Namespace: "owner/repo-b", Agent: "agent-2", Model: "model-a"},
		CostUSD:   1.20,
	})

	if m.AgentCost("agent-1") != 1.00 {
		t.Errorf("agent-1 cost = %v, want 1.00", m.AgentCost("agent-1"))
	}
	if m.AgentCost("agent-2") != 1.20 {
		t.Errorf("agent-2 cost = %v, want 1.20", m.AgentCost("agent-2"))
	}
	if m.RepoCost("owner/repo-a") != 1.00 {
		t.Errorf("repo-a cost = %v, want 1.00", m.RepoCost("owner/repo-a"))
	}
	if m.RepoCost("owner/repo-b") != 1.20 {
		t.Errorf("repo-b cost = %v, want 1.20", m.RepoCost("owner/repo-b"))
	}
	if m.PlatformCost() != 2.20 {
		t.Errorf("platform cost = %v, want 2.20", m.PlatformCost())
	}
}

func TestCostAttributionSprintTracking(t *testing.T) {
	m := NewCostAttributionModel()

	m.RecordCostWithSprint(CostEntry{
		Attribute: CostAttribute{Namespace: "owner/repo", Agent: "agent-1"},
		CostUSD:   0.50,
	}, "sprint-1")

	m.RecordCostWithSprint(CostEntry{
		Attribute: CostAttribute{Namespace: "owner/repo", Agent: "agent-2"},
		CostUSD:   1.50,
	}, "sprint-1")

	if m.SprintCost("sprint-1") != 2.00 {
		t.Errorf("sprint cost = %v, want 2.00", m.SprintCost("sprint-1"))
	}
}

func TestCostAttributionBudgetLimits(t *testing.T) {
	m := NewCostAttributionModel()
	m.SetAgentBudget("agent-1", 10.00)
	m.SetRepoBudget("owner/repo", 50.00)
	m.SetPlatformBudget(100.00)

	m.RecordCost(CostEntry{
		Attribute: CostAttribute{Namespace: "owner/repo", Agent: "agent-1"},
		CostUSD:   3.00,
	})

	if rem := m.AgentRemaining("agent-1"); rem != 7.00 {
		t.Errorf("agent remaining = %v, want 7.00", rem)
	}
	if rem := m.RepoRemaining("owner/repo"); rem != 47.00 {
		t.Errorf("repo remaining = %v, want 47.00", rem)
	}
}

func TestCostAttributionNoBudgetSet(t *testing.T) {
	m := NewCostAttributionModel()
	if rem := m.AgentRemaining("unknown"); rem != -1 {
		t.Errorf("agent remaining = %v, want -1 (no limit)", rem)
	}
	if rem := m.RepoRemaining("unknown"); rem != -1 {
		t.Errorf("repo remaining = %v, want -1 (no limit)", rem)
	}
}

func TestCostAttributionCheckExhaustionNone(t *testing.T) {
	m := NewCostAttributionModel()
	m.SetAgentBudget("agent-1", 10.00)
	m.SetRepoBudget("owner/repo", 50.00)

	m.RecordCost(CostEntry{
		Attribute: CostAttribute{Namespace: "owner/repo", Agent: "agent-1"},
		CostUSD:   3.00,
	})

	if level := m.CheckExhaustion("agent-1", "owner/repo"); level != ExhaustionNone {
		t.Errorf("CheckExhaustion = %v, want ExhaustionNone", level)
	}
}

func TestCostAttributionCheckExhaustionAgent(t *testing.T) {
	m := NewCostAttributionModel()
	m.SetAgentBudget("agent-1", 5.00)
	m.SetRepoBudget("owner/repo", 50.00)

	m.RecordCost(CostEntry{
		Attribute: CostAttribute{Namespace: "owner/repo", Agent: "agent-1"},
		CostUSD:   5.00,
	})

	if level := m.CheckExhaustion("agent-1", "owner/repo"); level != ExhaustionAgent {
		t.Errorf("CheckExhaustion = %v, want ExhaustionAgent", level)
	}
}

func TestCostAttributionCheckExhaustionRepo(t *testing.T) {
	m := NewCostAttributionModel()
	m.SetAgentBudget("agent-1", 100.00)
	m.SetRepoBudget("owner/repo", 5.00)

	m.RecordCost(CostEntry{
		Attribute: CostAttribute{Namespace: "owner/repo", Agent: "agent-1"},
		CostUSD:   5.00,
	})

	if level := m.CheckExhaustion("agent-1", "owner/repo"); level != ExhaustionRepo {
		t.Errorf("CheckExhaustion = %v, want ExhaustionRepo", level)
	}
}

func TestCostAttributionCheckExhaustionPlatform(t *testing.T) {
	m := NewCostAttributionModel()
	m.SetAgentBudget("agent-1", 100.00)
	m.SetRepoBudget("owner/repo", 100.00)
	m.SetPlatformBudget(10.00)

	m.RecordCost(CostEntry{
		Attribute: CostAttribute{Namespace: "owner/repo", Agent: "agent-1"},
		CostUSD:   10.00,
	})

	if level := m.CheckExhaustion("agent-1", "owner/repo"); level != ExhaustionPlatform {
		t.Errorf("CheckExhaustion = %v, want ExhaustionPlatform", level)
	}
}

func TestExhaustionAction(t *testing.T) {
	tests := []struct {
		level BudgetExhaustionLevel
		want  string
	}{
		{ExhaustionAgent, "403 on next API call → agent halts → Hivemind notifies repo owner"},
		{ExhaustionRepo, "all agents for that repo paused → Axiom notifies all repo collaborators"},
		{ExhaustionPlatform, "H4F pauses all agents → Telegram alert to platform admin"},
		{ExhaustionNone, ""},
	}
	for _, tt := range tests {
		got := ExhaustionAction(tt.level)
		if got != tt.want {
			t.Errorf("ExhaustionAction(%v) = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestEntriesByAgent(t *testing.T) {
	m := NewCostAttributionModel()

	m.RecordCost(CostEntry{Attribute: CostAttribute{Agent: "a1", Namespace: "r1"}, CostUSD: 0.5})
	m.RecordCost(CostEntry{Attribute: CostAttribute{Agent: "a2", Namespace: "r1"}, CostUSD: 0.3})
	m.RecordCost(CostEntry{Attribute: CostAttribute{Agent: "a1", Namespace: "r2"}, CostUSD: 0.7})

	entries := m.EntriesByAgent("a1")
	if len(entries) != 2 {
		t.Fatalf("EntriesByAgent(a1) = %d, want 2", len(entries))
	}
	totalCost := 0.0
	for _, e := range entries {
		totalCost += e.CostUSD
	}
	if totalCost != 1.2 {
		t.Errorf("total cost for a1 = %v, want 1.2", totalCost)
	}
}

func TestEntriesByRepo(t *testing.T) {
	m := NewCostAttributionModel()

	m.RecordCost(CostEntry{Attribute: CostAttribute{Agent: "a1", Namespace: "r1"}, CostUSD: 0.5})
	m.RecordCost(CostEntry{Attribute: CostAttribute{Agent: "a2", Namespace: "r1"}, CostUSD: 0.3})
	m.RecordCost(CostEntry{Attribute: CostAttribute{Agent: "a1", Namespace: "r2"}, CostUSD: 0.7})

	entries := m.EntriesByRepo("r1")
	if len(entries) != 2 {
		t.Fatalf("EntriesByRepo(r1) = %d, want 2", len(entries))
	}
}

func TestEntriesByModel(t *testing.T) {
	m := NewCostAttributionModel()

	m.RecordCost(CostEntry{Attribute: CostAttribute{Agent: "a1", Model: "deepseek"}, CostUSD: 0.5})
	m.RecordCost(CostEntry{Attribute: CostAttribute{Agent: "a2", Model: "glm"}, CostUSD: 0.3})
	m.RecordCost(CostEntry{Attribute: CostAttribute{Agent: "a1", Model: "deepseek"}, CostUSD: 0.7})

	entries := m.EntriesByModel("deepseek")
	if len(entries) != 2 {
		t.Fatalf("EntriesByModel(deepseek) = %d, want 2", len(entries))
	}
}

func TestCostReport(t *testing.T) {
	m := NewCostAttributionModel()
	m.RecordCost(CostEntry{Attribute: CostAttribute{Agent: "a1", Namespace: "r1"}, CostUSD: 0.5})
	m.RecordCost(CostEntry{Attribute: CostAttribute{Agent: "a2", Namespace: "r1"}, CostUSD: 0.3})

	r := m.Report()
	if r.EntryCount != 2 {
		t.Errorf("EntryCount = %d, want 2", r.EntryCount)
	}
	if r.PlatformTotal != 0.8 {
		t.Errorf("PlatformTotal = %v, want 0.8", r.PlatformTotal)
	}
	if r.AgentCosts["a1"] != 0.5 {
		t.Errorf("AgentCosts[a1] = %v, want 0.5", r.AgentCosts["a1"])
	}
	if r.RepoCosts["r1"] != 0.8 {
		t.Errorf("RepoCosts[r1] = %v, want 0.8", r.RepoCosts["r1"])
	}
}

func TestFormatCostReport(t *testing.T) {
	m := NewCostAttributionModel()
	m.RecordCost(CostEntry{Attribute: CostAttribute{Agent: "a1", Namespace: "r1"}, CostUSD: 0.5})
	r := m.Report()
	s := FormatCostReport(r)
	if s == "" {
		t.Error("FormatCostReport returned empty string")
	}
}

func TestCostAttributionReset(t *testing.T) {
	m := NewCostAttributionModel()
	m.SetAgentBudget("a1", 10.00)
	m.RecordCost(CostEntry{Attribute: CostAttribute{Agent: "a1", Namespace: "r1"}, CostUSD: 5.0})

	m.Reset()

	if m.AgentCost("a1") != 0 {
		t.Errorf("AgentCost after Reset = %v, want 0", m.AgentCost("a1"))
	}
	if m.PlatformCost() != 0 {
		t.Errorf("PlatformCost after Reset = %v, want 0", m.PlatformCost())
	}
	// Budget limits should persist
	if rem := m.AgentRemaining("a1"); rem != 10.00 {
		t.Errorf("AgentRemaining after Reset = %v, want 10.00 (limits persist)", rem)
	}
}

func TestCostAttributionConcurrent(t *testing.T) {
	m := NewCostAttributionModel()
	done := make(chan struct{})

	for i := 0; i < 10; i++ {
		go func(n int) {
			for j := 0; j < 10; j++ {
				m.RecordCost(CostEntry{
					Attribute: CostAttribute{Agent: "a1", Namespace: "r1"},
					CostUSD:   0.01,
				})
			}
			done <- struct{}{}
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Use tolerance for float comparison due to concurrent accumulation
	got := m.AgentCost("a1")
	if got < 0.999 || got > 1.001 {
		t.Errorf("concurrent AgentCost = %v, want ~1.00", got)
	}
}
