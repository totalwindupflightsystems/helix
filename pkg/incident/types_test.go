package incident

import (
	"testing"
	"time"
)

func TestNewStore(t *testing.T) {
	s := NewStore()
	if s == nil {
		t.Fatal("expected non-nil store")
	}
	if s.Count() != 0 {
		t.Errorf("expected empty store, got %d", s.Count())
	}
}

func TestAddAndGet(t *testing.T) {
	s := NewStore()
	inc := &Incident{
		ID:          "inc-001",
		AgentID:     "agent-alpha",
		PRURL:       "https://forgejo/pr/42",
		Severity:    SeverityHigh,
		CausalChain: []string{"deployed untested code path", "missed edge case in token refresh"},
		Timestamp:   time.Now().UTC(),
		Description: "Token refresh deadlock under concurrent load",
		Evidence:    []string{"test_run_id: abc123", "chimera-verdict: def456"},
	}

	err := s.Add(inc)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	got := s.Get("inc-001")
	if got == nil {
		t.Fatal("expected incident")
	}
	if got.ID != "inc-001" {
		t.Errorf("expected ID 'inc-001', got %q", got.ID)
	}
	if got.AgentID != "agent-alpha" {
		t.Errorf("expected agent 'agent-alpha', got %q", got.AgentID)
	}
	if got.Severity != SeverityHigh {
		t.Errorf("expected severity 'high', got %q", got.Severity)
	}
}

func TestAdd_DuplicateID(t *testing.T) {
	s := NewStore()
	_ = s.Add(&Incident{ID: "inc-001", AgentID: "agent-a", Severity: SeverityLow, Timestamp: time.Now()})
	err := s.Add(&Incident{ID: "inc-001", AgentID: "agent-b", Severity: SeverityLow, Timestamp: time.Now()})
	if err == nil {
		t.Error("expected error for duplicate ID")
	}
}

func TestAdd_MissingID(t *testing.T) {
	s := NewStore()
	err := s.Add(&Incident{AgentID: "agent-a", Severity: SeverityLow, Timestamp: time.Now()})
	if err == nil {
		t.Error("expected error for missing ID")
	}
}

func TestGet_NotFound(t *testing.T) {
	s := NewStore()
	if got := s.Get("non-existent"); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestListByAgent(t *testing.T) {
	s := NewStore()
	now := time.Now()

	_ = s.Add(&Incident{ID: "inc-001", AgentID: "agent-a", Severity: SeverityLow, Timestamp: now})
	_ = s.Add(&Incident{ID: "inc-002", AgentID: "agent-a", Severity: SeverityMedium, Timestamp: now})
	_ = s.Add(&Incident{ID: "inc-003", AgentID: "agent-b", Severity: SeverityHigh, Timestamp: now})

	incidents := s.ListByAgent("agent-a")
	if len(incidents) != 2 {
		t.Errorf("expected 2 incidents for agent-a, got %d", len(incidents))
	}

	incidents = s.ListByAgent("agent-b")
	if len(incidents) != 1 {
		t.Errorf("expected 1 incident for agent-b, got %d", len(incidents))
	}

	incidents = s.ListByAgent("agent-unknown")
	if len(incidents) != 0 {
		t.Errorf("expected 0 incidents for unknown agent, got %d", len(incidents))
	}
}

func TestListBySeverity(t *testing.T) {
	s := NewStore()
	now := time.Now()

	_ = s.Add(&Incident{ID: "inc-001", AgentID: "agent-a", Severity: SeverityLow, Timestamp: now})
	_ = s.Add(&Incident{ID: "inc-002", AgentID: "agent-b", Severity: SeverityMedium, Timestamp: now})
	_ = s.Add(&Incident{ID: "inc-003", AgentID: "agent-c", Severity: SeverityHigh, Timestamp: now})
	_ = s.Add(&Incident{ID: "inc-004", AgentID: "agent-d", Severity: SeverityLow, Timestamp: now})

	low := s.ListBySeverity(SeverityLow)
	if len(low) != 2 {
		t.Errorf("expected 2 low-severity incidents, got %d", len(low))
	}

	high := s.ListBySeverity(SeverityHigh)
	if len(high) != 1 {
		t.Errorf("expected 1 high-severity incident, got %d", len(high))
	}

	critical := s.ListBySeverity(SeverityCritical)
	if len(critical) != 0 {
		t.Errorf("expected 0 critical, got %d", len(critical))
	}
}

func TestCountInWindow(t *testing.T) {
	s := NewStore()
	now := time.Now()

	_ = s.Add(&Incident{ID: "inc-old", AgentID: "agent-a", Severity: SeverityLow, Timestamp: now.Add(-48 * time.Hour)})
	_ = s.Add(&Incident{ID: "inc-new", AgentID: "agent-a", Severity: SeverityMedium, Timestamp: now.Add(-2 * time.Hour)})

	count := s.CountInWindow("agent-a", 24*time.Hour)
	if count != 1 {
		t.Errorf("expected 1 incident in 24h window, got %d", count)
	}

	count = s.CountInWindow("agent-a", 72*time.Hour)
	if count != 2 {
		t.Errorf("expected 2 incidents in 72h window, got %d", count)
	}

	count = s.CountInWindow("agent-unknown", 24*time.Hour)
	if count != 0 {
		t.Errorf("expected 0 for unknown agent, got %d", count)
	}
}

func TestAll(t *testing.T) {
	s := NewStore()
	now := time.Now()
	_ = s.Add(&Incident{ID: "inc-001", AgentID: "agent-a", Severity: SeverityLow, Timestamp: now})
	_ = s.Add(&Incident{ID: "inc-002", AgentID: "agent-b", Severity: SeverityMedium, Timestamp: now})

	all := s.All()
	if len(all) != 2 {
		t.Errorf("expected 2 incidents, got %d", len(all))
	}
}

func TestCount(t *testing.T) {
	s := NewStore()
	now := time.Now()
	_ = s.Add(&Incident{ID: "inc-001", AgentID: "agent-a", Severity: SeverityLow, Timestamp: now})
	_ = s.Add(&Incident{ID: "inc-002", AgentID: "agent-b", Severity: SeverityMedium, Timestamp: now})
	_ = s.Add(&Incident{ID: "inc-003", AgentID: "agent-c", Severity: SeverityHigh, Timestamp: now})

	if s.Count() != 3 {
		t.Errorf("expected 3, got %d", s.Count())
	}
}

func TestResolvedAt(t *testing.T) {
	s := NewStore()
	now := time.Now()
	resolved := now.Add(2 * time.Hour)

	inc := &Incident{
		ID:         "inc-resolved",
		AgentID:    "agent-a",
		Severity:   SeverityMedium,
		Timestamp:  now,
		ResolvedAt: &resolved,
	}
	_ = s.Add(inc)

	got := s.Get("inc-resolved")
	if got.ResolvedAt == nil {
		t.Fatal("expected ResolvedAt")
	}
	if !got.ResolvedAt.Equal(resolved) {
		t.Errorf("expected resolution time %v, got %v", resolved, got.ResolvedAt)
	}
}
