package dispatcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/adr"
	"github.com/totalwindupflightsystems/helix/pkg/incident"
	"github.com/totalwindupflightsystems/helix/pkg/spec"
	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

// ---------------------------------------------------------------------------
// EstimateTokens
// ---------------------------------------------------------------------------

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 1},
		{"a", 1},
		{"abcd", 1},
		{"abcde", 2},
		{"abcdefgh", 2},
		{"hello world, this is a test of token estimation", 12},
	}
	for _, tt := range tests {
		got := EstimateTokens(tt.input)
		if got != tt.want {
			t.Errorf("EstimateTokens(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// ContextBudget
// ---------------------------------------------------------------------------

func TestContextBudget(t *testing.T) {
	b := NewContextBudget(10)
	if got := b.Remaining(); got != 10 {
		t.Fatalf("remaining = %d, want 10", got)
	}
	if got := b.Used(); got != 0 {
		t.Fatalf("used = %d, want 0", got)
	}

	if !b.Consume(5) {
		t.Fatal("consume 5 should succeed")
	}
	if got := b.Remaining(); got != 5 {
		t.Fatalf("remaining = %d, want 5", got)
	}

	if b.Consume(10) {
		t.Fatal("consume 10 should fail with only 5 remaining")
	}
	if got := b.Remaining(); got != 5 {
		t.Fatalf("remaining should still be 5 after failed consume, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func setupTempStores(t *testing.T) (specStore *spec.SpecStore, adrStore *adr.ADRStore, incStore *incident.Store, cleanup func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "helix-context-test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}

	specDir := filepath.Join(tmpDir, "specs")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("mkdir specs: %v", err)
	}
	specStore, err = spec.NewSpecStore(specDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("NewSpecStore: %v", err)
	}

	adrDir := filepath.Join(tmpDir, "adrs")
	if err := os.MkdirAll(adrDir, 0o755); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("mkdir adrs: %v", err)
	}
	adrStore, err = adr.NewADRStore(adrDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("NewADRStore: %v", err)
	}

	incStore = incident.NewStore()
	cleanup = func() { os.RemoveAll(tmpDir) }
	return
}

func seedSpec(t *testing.T, s *spec.SpecStore, id, title string, sections []spec.SpecSection) {
	t.Helper()
	sp := &spec.Spec{
		ID:       id,
		Title:    title,
		Sections: sections,
	}
	if err := s.Save(sp); err != nil {
		t.Fatalf("Save spec: %v", err)
	}
}

func seedADR(t *testing.T, a *adr.ADRStore, number int, title, decision, status string) {
	t.Helper()
	ad := &adr.ADR{
		ID:       fmt.Sprintf("adr-%04d", number),
		Number:   number,
		Title:    title,
		Decision: decision,
		Status:   status,
	}
	if err := a.Save(ad); err != nil {
		t.Fatalf("Save ADR: %v", err)
	}
}

func seedIncident(t *testing.T, s *incident.Store, id, agentID, severity, desc string) {
	t.Helper()
	inc := &incident.Incident{
		ID:          id,
		AgentID:     agentID,
		Severity:    severity,
		Description: desc,
		Timestamp:   time.Now(),
	}
	if err := s.Add(inc); err != nil {
		t.Fatalf("Add incident: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Normal assembly
// ---------------------------------------------------------------------------

func TestAssemble_Full(t *testing.T) {
	specStore, adrStore, incStore, cleanup := setupTempStores(t)
	defer cleanup()

	seedSpec(t, specStore, "spec-001", "Auth API", []spec.SpecSection{
		{Title: "Overview", Content: "This spec defines the authentication API."},
		{Title: "Endpoints", Content: "POST /auth/login, POST /auth/refresh."},
	})
	seedADR(t, adrStore, 1, "Use JWT for auth", "Adopt JWT with short-lived access tokens.", "accepted")
	seedADR(t, adrStore, 2, "Session storage", "Use Redis for session storage.", "proposed")
	seedIncident(t, incStore, "inc-001", "agent-7", "medium", "Auth token refresh failed")
	seedIncident(t, incStore, "inc-002", "agent-7", "high", "Session hijack via leaked JWT")

	ca := &ContextAssembler{
		SpecStore:     specStore,
		ADRStore:      adrStore,
		IncidentStore: incStore,
		Budget:        8192,
	}

	task := Task{ID: "task-001", SpecRef: "spec-001"}

	ctx, err := ca.Assemble(task)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	if len(ctx.SpecSections) == 0 {
		t.Error("expected spec sections, got none")
	}
	if len(ctx.ADRConstraints) == 0 {
		t.Error("expected ADR constraints, got none")
	}
	if len(ctx.IncidentHistory) == 0 {
		t.Error("expected incident history, got none")
	}
	if ctx.BudgetUsed <= 0 {
		t.Error("expected budget used > 0")
	}
	if ctx.BudgetTotal != 8192 {
		t.Errorf("BudgetTotal = %d, want 8192", ctx.BudgetTotal)
	}
}

// ---------------------------------------------------------------------------
// Empty / nil stores
// ---------------------------------------------------------------------------

func TestAssemble_NilStores(t *testing.T) {
	ca := &ContextAssembler{Budget: 1024}

	ctx, err := ca.Assemble(Task{ID: "t1"})
	if err != nil {
		t.Fatalf("Assemble with nil stores: %v", err)
	}
	if !ctx.IsEmpty() {
		t.Error("expected empty context with nil stores")
	}
	if ctx.BudgetTotal != 1024 {
		t.Errorf("BudgetTotal = %d, want 1024", ctx.BudgetTotal)
	}
}

func TestAssemble_EmptyStores(t *testing.T) {
	specStore, adrStore, incStore, cleanup := setupTempStores(t)
	defer cleanup()

	ca := &ContextAssembler{
		SpecStore:     specStore,
		ADRStore:      adrStore,
		IncidentStore: incStore,
		Budget:        1024,
	}

	ctx, err := ca.Assemble(Task{ID: "t1", SpecRef: "nonexistent"})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if !ctx.IsEmpty() {
		t.Error("expected empty context with empty stores and bad SpecRef")
	}
}

// ---------------------------------------------------------------------------
// Budget trimming
// ---------------------------------------------------------------------------

func TestAssemble_BudgetTrimming(t *testing.T) {
	specStore, adrStore, incStore, cleanup := setupTempStores(t)
	defer cleanup()

	longContent := strings.Repeat("very long content that should be trimmed ", 100)
	seedSpec(t, specStore, "spec-big", "Big Spec", []spec.SpecSection{
		{Title: "Section 1", Content: longContent},
		{Title: "Section 2", Content: longContent},
	})
	seedADR(t, adrStore, 1, "ADR Title", "Some decision here.", "accepted")
	seedIncident(t, incStore, "inc-1", "agent-1", "low", "Minor incident.")

	ca := &ContextAssembler{
		SpecStore:     specStore,
		ADRStore:      adrStore,
		IncidentStore: incStore,
		Budget:        256,
	}

	ctx, err := ca.Assemble(Task{ID: "t1", SpecRef: "spec-big"})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if ctx.BudgetUsed > 256 {
		t.Errorf("BudgetUsed = %d, should not exceed 256", ctx.BudgetUsed)
	}
}

func TestAssemble_ZeroBudget(t *testing.T) {
	specStore, adrStore, incStore, cleanup := setupTempStores(t)
	defer cleanup()

	seedSpec(t, specStore, "spec-z", "Z", []spec.SpecSection{
		{Title: "X", Content: "content"},
	})

	ca := &ContextAssembler{
		SpecStore:     specStore,
		ADRStore:      adrStore,
		IncidentStore: incStore,
		Budget:        0,
	}

	ctx, err := ca.Assemble(Task{ID: "t1", SpecRef: "spec-z"})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if ctx.BudgetTotal != DefaultBudget {
		t.Errorf("BudgetTotal = %d, want DefaultBudget %d", ctx.BudgetTotal, DefaultBudget)
	}
}

// ---------------------------------------------------------------------------
// Expand
// ---------------------------------------------------------------------------

func TestExpand_ValidSection(t *testing.T) {
	specStore, adrStore, incStore, cleanup := setupTempStores(t)
	defer cleanup()

	seedSpec(t, specStore, "spec-e", "Expand Test", []spec.SpecSection{
		{Title: "API", Content: "API definition here."},
	})
	seedADR(t, adrStore, 1, "ADR1", "Decision 1.", "accepted")

	ca := &ContextAssembler{
		SpecStore:     specStore,
		ADRStore:      adrStore,
		IncidentStore: incStore,
		Budget:        128,
	}

	ctx, err := ca.Assemble(Task{ID: "t1", SpecRef: "spec-e"})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	oldBudget := ctx.BudgetTotal

	if err := ca.Expand(ctx, SectionADRs, 512); err != nil {
		t.Fatalf("Expand(adrs): %v", err)
	}
	if ctx.BudgetTotal <= oldBudget {
		t.Errorf("BudgetTotal should increase after expand: %d -> %d", oldBudget, ctx.BudgetTotal)
	}
}

func TestExpand_UnknownSection(t *testing.T) {
	ca := &ContextAssembler{Budget: 256}
	ctx := &AssembledContext{BudgetTotal: 256}

	err := ca.Expand(ctx, "nonexistent", 128)
	if err == nil {
		t.Fatal("expected error for unknown section")
	}
	if !strings.Contains(err.Error(), "unknown section") {
		t.Errorf("error should mention unknown section, got: %v", err)
	}
}

func TestExpand_NilCtx(t *testing.T) {
	ca := &ContextAssembler{}
	err := ca.Expand(nil, SectionSpecs, 100)
	if err == nil {
		t.Fatal("expected error for nil ctx")
	}
}

func TestExpand_NonPositiveBudget(t *testing.T) {
	ca := &ContextAssembler{}
	ctx := &AssembledContext{}
	err := ca.Expand(ctx, SectionSpecs, 0)
	if err == nil {
		t.Fatal("expected error for non-positive extra budget")
	}
	err = ca.Expand(ctx, SectionADRs, -5)
	if err == nil {
		t.Fatal("expected error for negative extra budget")
	}
}

// ---------------------------------------------------------------------------
// Git PR assembly (no repo path)
// ---------------------------------------------------------------------------

func TestAssemble_NoRepoPath(t *testing.T) {
	ca := &ContextAssembler{
		RepoPath: "",
		Budget:   1024,
	}

	ctx, err := ca.Assemble(Task{ID: "t1"})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if len(ctx.RelatedPRs) != 0 {
		t.Errorf("expected no PRs without repo path, got %d", len(ctx.RelatedPRs))
	}
}

// ---------------------------------------------------------------------------
// AssembledContext.IsEmpty
// ---------------------------------------------------------------------------

func TestAssembledContext_IsEmpty(t *testing.T) {
	ac := &AssembledContext{}
	if !ac.IsEmpty() {
		t.Error("empty context should be empty")
	}

	ac.SpecSections = []ContextSection{{Title: "X", Content: "Y"}}
	if ac.IsEmpty() {
		t.Error("context with spec sections should not be empty")
	}
}

// ---------------------------------------------------------------------------
// Incident agent filtering + fallback
// ---------------------------------------------------------------------------

func TestAssemble_IncidentAgentFiltering(t *testing.T) {
	_, _, incStore, cleanup := setupTempStores(t)
	defer cleanup()

	seedIncident(t, incStore, "inc-3", "agent-bot", "low", "Bot incident")
	seedIncident(t, incStore, "inc-4", "agent-human", "high", "Human incident")

	ca := &ContextAssembler{
		IncidentStore: incStore,
		Budget:        4096,
	}

	task := Task{
		ID:            "t1",
		AssignedAgent: "agent-bot",
	}

	ctx, err := ca.Assemble(task)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	if len(ctx.IncidentHistory) == 0 {
		t.Error("expected incident history")
	}
	for _, h := range ctx.IncidentHistory {
		if !strings.Contains(h, "agent-bot") {
			t.Errorf("expected only agent-bot incidents, got: %s", h)
		}
	}
}

// ---------------------------------------------------------------------------
// Trust tier integration check
// ---------------------------------------------------------------------------

func TestCompareTiers_Integration(t *testing.T) {
	if trust.CompareTiers(trust.TierProvisional, trust.TierTrusted) >= 0 {
		t.Error("Provisional should be < Trusted")
	}
	if trust.CompareTiers(trust.TierVeteran, trust.TierObserved) <= 0 {
		t.Error("Veteran should be > Observed")
	}
	if trust.CompareTiers(trust.TierTrusted, trust.TierTrusted) != 0 {
		t.Error("Trusted should be == Trusted")
	}
}
