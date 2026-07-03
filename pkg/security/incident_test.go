package security

import (
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Severity Tests
// =============================================================================

func TestAllSeverities_Count(t *testing.T) {
	sevs := AllSeverities()
	if len(sevs) != 4 {
		t.Errorf("AllSeverities() returned %d, want 4", len(sevs))
	}
}

func TestAllSeverities_Order(t *testing.T) {
	sevs := AllSeverities()
	expected := []Severity{SeveritySEV0, SeveritySEV1, SeveritySEV2, SeveritySEV3}
	for i, s := range sevs {
		if s.Level != expected[i] {
			t.Errorf("AllSeverities()[%d] = %q, want %q", i, s.Level, expected[i])
		}
	}
}

func TestSeverityOrder(t *testing.T) {
	tests := []struct {
		sev  Severity
		want int
	}{
		{SeveritySEV0, 0},
		{SeveritySEV1, 1},
		{SeveritySEV2, 2},
		{SeveritySEV3, 3},
		{Severity("SEV-9"), 99},
	}
	for _, tt := range tests {
		if got := SeverityOrder(tt.sev); got != tt.want {
			t.Errorf("SeverityOrder(%q) = %d, want %d", tt.sev, got, tt.want)
		}
	}
}

func TestSeverityInfo_NotEmpty(t *testing.T) {
	for _, s := range AllSeverities() {
		if s.Definition == "" {
			t.Errorf("severity %q has empty Definition", s.Level)
		}
		if s.ResponseTime == "" {
			t.Errorf("severity %q has empty ResponseTime", s.Level)
		}
		if s.Example == "" {
			t.Errorf("severity %q has empty Example", s.Level)
		}
	}
}

// =============================================================================
// Default Procedures Tests
// =============================================================================

func TestDefaultProcedures_AllSeverities(t *testing.T) {
	procs := DefaultProcedures()
	if len(procs) != 4 {
		t.Fatalf("DefaultProcedures() returned %d, want 4", len(procs))
	}
	seen := make(map[Severity]bool)
	for _, p := range procs {
		seen[p.Severity] = true
	}
	for _, s := range []Severity{SeveritySEV0, SeveritySEV1, SeveritySEV2, SeveritySEV3} {
		if !seen[s] {
			t.Errorf("missing procedure for %q", s)
		}
	}
}

func TestDefaultProcedures_SEV0_Steps(t *testing.T) {
	procs := DefaultProcedures()
	var sev0 *ResponseProcedure
	for i := range procs {
		if procs[i].Severity == SeveritySEV0 {
			sev0 = &procs[i]
			break
		}
	}
	if sev0 == nil {
		t.Fatal("SEV-0 procedure not found")
	}
	if len(sev0.Steps) != 6 {
		t.Errorf("SEV-0 has %d steps, want 6 (per spec)", len(sev0.Steps))
	}
	// Verify step ordering
	for i, step := range sev0.Steps {
		if step.Order != i+1 {
			t.Errorf("SEV-0 step %d has Order %d, want %d", i, step.Order, i+1)
		}
		if step.Action == "" {
			t.Errorf("SEV-0 step %d has empty Action", i+1)
		}
	}
}

func TestDefaultProcedures_SEV1_Steps(t *testing.T) {
	procs := DefaultProcedures()
	var sev1 *ResponseProcedure
	for i := range procs {
		if procs[i].Severity == SeveritySEV1 {
			sev1 = &procs[i]
			break
		}
	}
	if sev1 == nil {
		t.Fatal("SEV-1 procedure not found")
	}
	if len(sev1.Steps) != 5 {
		t.Errorf("SEV-1 has %d steps, want 5 (per spec)", len(sev1.Steps))
	}
}

func TestDefaultProcedures_SEV2_Steps(t *testing.T) {
	procs := DefaultProcedures()
	var sev2 *ResponseProcedure
	for i := range procs {
		if procs[i].Severity == SeveritySEV2 {
			sev2 = &procs[i]
			break
		}
	}
	if sev2 == nil {
		t.Fatal("SEV-2 procedure not found")
	}
	if len(sev2.Steps) != 3 {
		t.Errorf("SEV-2 has %d steps, want 3", len(sev2.Steps))
	}
}

func TestDefaultProcedures_SEV3_Steps(t *testing.T) {
	procs := DefaultProcedures()
	var sev3 *ResponseProcedure
	for i := range procs {
		if procs[i].Severity == SeveritySEV3 {
			sev3 = &procs[i]
			break
		}
	}
	if sev3 == nil {
		t.Fatal("SEV-3 procedure not found")
	}
	if len(sev3.Steps) != 2 {
		t.Errorf("SEV-3 has %d steps, want 2", len(sev3.Steps))
	}
}

func TestDefaultProcedures_AllHaveTriggers(t *testing.T) {
	for _, p := range DefaultProcedures() {
		if p.Trigger == "" {
			t.Errorf("procedure %q has empty Trigger", p.Severity)
		}
	}
}

// =============================================================================
// Incident Response Engine Tests
// =============================================================================

func TestNewIncidentResponseEngine(t *testing.T) {
	e := NewIncidentResponseEngine()
	if e.IncidentCount() != 0 {
		t.Errorf("IncidentCount() = %d, want 0 (new engine)", e.IncidentCount())
	}
}

func TestEngine_GetProcedure(t *testing.T) {
	e := NewIncidentResponseEngine()
	proc, ok := e.GetProcedure(SeveritySEV0)
	if !ok {
		t.Fatal("GetProcedure(SEV-0) returned ok=false")
	}
	if len(proc.Steps) != 6 {
		t.Errorf("SEV-0 procedure has %d steps, want 6", len(proc.Steps))
	}
}

func TestEngine_GetProcedure_NotFound(t *testing.T) {
	e := NewIncidentResponseEngine()
	_, ok := e.GetProcedure(Severity("SEV-9"))
	if ok {
		t.Error("GetProcedure(SEV-9) returned ok=true, want false")
	}
}

func TestEngine_RegisterIncident(t *testing.T) {
	e := NewIncidentResponseEngine()
	e.RegisterIncident(IncidentRecord{
		ID:       "INC-001",
		Severity: SeveritySEV1,
		Title:    "Agent pushing secrets",
	})
	if e.IncidentCount() != 1 {
		t.Errorf("IncidentCount() = %d, want 1", e.IncidentCount())
	}
}

func TestEngine_RegisterIncident_Defaults(t *testing.T) {
	e := NewIncidentResponseEngine()
	e.RegisterIncident(IncidentRecord{
		ID:       "INC-001",
		Severity: SeveritySEV1,
		Title:    "Test",
	})
	inc := e.ActiveIncidents()[0]
	if inc.Status != IncidentOpen {
		t.Errorf("Status = %q, want %q (default)", inc.Status, IncidentOpen)
	}
	if inc.CreatedAt.IsZero() {
		t.Error("CreatedAt should be auto-set")
	}
}

func TestEngine_ActiveIncidents(t *testing.T) {
	e := NewIncidentResponseEngine()
	e.RegisterIncident(IncidentRecord{
		ID: "INC-001", Severity: SeveritySEV1, Title: "Active",
	})
	e.RegisterIncident(IncidentRecord{
		ID: "INC-002", Severity: SeveritySEV2, Title: "Resolved",
		Status: IncidentResolved, ResolvedAt: time.Now().UTC(),
	})
	active := e.ActiveIncidents()
	if len(active) != 1 {
		t.Errorf("ActiveIncidents() = %d, want 1", len(active))
	}
	if active[0].ID != "INC-001" {
		t.Errorf("Active incident ID = %q, want INC-001", active[0].ID)
	}
}

func TestEngine_ResolveIncident(t *testing.T) {
	e := NewIncidentResponseEngine()
	e.RegisterIncident(IncidentRecord{
		ID: "INC-001", Severity: SeveritySEV1, Title: "Test",
	})
	if !e.ResolveIncident("INC-001") {
		t.Fatal("ResolveIncident returned false")
	}
	if e.IncidentCount() != 1 {
		t.Errorf("IncidentCount() = %d, want 1 (resolve doesn't remove)", e.IncidentCount())
	}
	active := e.ActiveIncidents()
	if len(active) != 0 {
		t.Errorf("ActiveIncidents() = %d, want 0 (resolved)", len(active))
	}
}

func TestEngine_ResolveIncident_NotFound(t *testing.T) {
	e := NewIncidentResponseEngine()
	if e.ResolveIncident("nonexistent") {
		t.Error("ResolveIncident returned true for non-existent ID")
	}
}

func TestEngine_EscalateIncident(t *testing.T) {
	e := NewIncidentResponseEngine()
	e.RegisterIncident(IncidentRecord{
		ID: "INC-001", Severity: SeveritySEV3, Title: "Low priority",
	})
	if !e.EscalateIncident("INC-001") {
		t.Fatal("EscalateIncident returned false")
	}
	inc := e.incidents[0]
	if inc.Severity != SeveritySEV2 {
		t.Errorf("Severity after escalation = %q, want SEV-2", inc.Severity)
	}
	if inc.Status != IncidentEscalated {
		t.Errorf("Status = %q, want %q", inc.Status, IncidentEscalated)
	}
}

func TestEngine_EscalateIncident_SEV0(t *testing.T) {
	e := NewIncidentResponseEngine()
	e.RegisterIncident(IncidentRecord{
		ID: "INC-001", Severity: SeveritySEV0, Title: "Already critical",
	})
	e.EscalateIncident("INC-001")
	// SEV-0 cannot be escalated further
	inc := e.incidents[0]
	if inc.Severity != SeveritySEV0 {
		t.Errorf("Severity = %q, want SEV-0 (already highest)", inc.Severity)
	}
}

func TestEngine_EscalateIncident_NotFound(t *testing.T) {
	e := NewIncidentResponseEngine()
	if e.EscalateIncident("nonexistent") {
		t.Error("EscalateIncident returned true for non-existent ID")
	}
}

func TestEngine_CompleteStep(t *testing.T) {
	e := NewIncidentResponseEngine()
	e.RegisterIncident(IncidentRecord{
		ID: "INC-001", Severity: SeveritySEV1, Title: "Test",
	})
	if !e.CompleteStep("INC-001", 1) {
		t.Fatal("CompleteStep returned false")
	}
	inc := e.incidents[0]
	if len(inc.StepsCompleted) != 1 || inc.StepsCompleted[0] != 1 {
		t.Errorf("StepsCompleted = %v, want [1]", inc.StepsCompleted)
	}
}

func TestEngine_CompleteStep_NotFound(t *testing.T) {
	e := NewIncidentResponseEngine()
	if e.CompleteStep("nonexistent", 1) {
		t.Error("CompleteStep returned true for non-existent ID")
	}
}

func TestEngine_IncidentsBySeverity(t *testing.T) {
	e := NewIncidentResponseEngine()
	e.RegisterIncident(IncidentRecord{ID: "INC-1", Severity: SeveritySEV0})
	e.RegisterIncident(IncidentRecord{ID: "INC-2", Severity: SeveritySEV1})
	e.RegisterIncident(IncidentRecord{ID: "INC-3", Severity: SeveritySEV1})
	e.RegisterIncident(IncidentRecord{ID: "INC-4", Severity: SeveritySEV3})
	bySev := e.IncidentsBySeverity()
	if len(bySev[SeveritySEV0]) != 1 {
		t.Errorf("SEV-0 count = %d, want 1", len(bySev[SeveritySEV0]))
	}
	if len(bySev[SeveritySEV1]) != 2 {
		t.Errorf("SEV-1 count = %d, want 2", len(bySev[SeveritySEV1]))
	}
	if len(bySev[SeveritySEV3]) != 1 {
		t.Errorf("SEV-3 count = %d, want 1", len(bySev[SeveritySEV3]))
	}
}

// =============================================================================
// IncidentRecord Tests
// =============================================================================

func TestIncidentRecord_Duration_Resolved(t *testing.T) {
	start := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 19, 12, 30, 0, 0, time.UTC)
	inc := &IncidentRecord{
		CreatedAt:  start,
		ResolvedAt: end,
		Status:     IncidentResolved,
	}
	d := inc.Duration()
	if d != 30*time.Minute {
		t.Errorf("Duration() = %s, want 30m", d)
	}
}

func TestIncidentRecord_Duration_Ongoing(t *testing.T) {
	inc := &IncidentRecord{
		CreatedAt: time.Now().Add(-5 * time.Minute),
	}
	d := inc.Duration()
	if d < 4*time.Minute || d > 6*time.Minute {
		t.Errorf("Duration() = %s, want ~5m", d)
	}
}

func TestIncidentRecord_IsResolved(t *testing.T) {
	tests := []struct {
		status IncidentStatus
		want   bool
	}{
		{IncidentResolved, true},
		{IncidentOpen, false},
		{IncidentInProgress, false},
		{IncidentEscalated, false},
	}
	for _, tt := range tests {
		inc := &IncidentRecord{Status: tt.status}
		if got := inc.IsResolved(); got != tt.want {
			t.Errorf("IsResolved() for %q = %v, want %v", tt.status, got, tt.want)
		}
	}
}

// =============================================================================
// Alert Classification Tests
// =============================================================================

func TestClassifyFromAlert_CriticalPlatform(t *testing.T) {
	alert := AlertSignal{
		AlertName: "AgentDown",
		Severity:  "critical",
		Message:   "Platform compromise detected",
	}
	if got := ClassifyFromAlert(alert); got != SeveritySEV0 {
		t.Errorf("ClassifyFromAlert() = %q, want SEV-0", got)
	}
}

func TestClassifyFromAlert_CriticalAgent(t *testing.T) {
	alert := AlertSignal{
		AlertName: "HighCostAgent",
		Severity:  "critical",
		Message:   "agent spending exceeding budget",
	}
	if got := ClassifyFromAlert(alert); got != SeveritySEV1 {
		t.Errorf("ClassifyFromAlert() = %q, want SEV-1", got)
	}
}

func TestClassifyFromAlert_CriticalOther(t *testing.T) {
	alert := AlertSignal{
		AlertName: "GateFailureSpike",
		Severity:  "critical",
		Message:   "gate pass rate below threshold",
	}
	if got := ClassifyFromAlert(alert); got != SeveritySEV2 {
		t.Errorf("ClassifyFromAlert() = %q, want SEV-2", got)
	}
}

func TestClassifyFromAlert_Warning(t *testing.T) {
	alert := AlertSignal{
		AlertName: "PRStuck",
		Severity:  "warning",
		Message:   "PR cycle > 2h",
	}
	if got := ClassifyFromAlert(alert); got != SeveritySEV3 {
		t.Errorf("ClassifyFromAlert() = %q, want SEV-3", got)
	}
}

func TestEngine_CreateFromAlert(t *testing.T) {
	e := NewIncidentResponseEngine()
	alert := AlertSignal{
		AlertName: "AgentDown",
		Severity:  "critical",
		Message:   "agent container not responding",
		Labels:    map[string]string{"agent": "agent-sandbox-7"},
	}
	record := e.CreateFromAlert(alert)
	if record == nil {
		t.Fatal("CreateFromAlert returned nil")
	}
	if record.Severity != SeveritySEV1 {
		t.Errorf("Severity = %q, want SEV-1 (agent-related)", record.Severity)
	}
	if record.AgentID != "agent-sandbox-7" {
		t.Errorf("AgentID = %q, want agent-sandbox-7", record.AgentID)
	}
	if record.Status != IncidentOpen {
		t.Errorf("Status = %q, want open", record.Status)
	}
	if e.IncidentCount() != 1 {
		t.Errorf("IncidentCount() = %d, want 1", e.IncidentCount())
	}
}

// =============================================================================
// Statistics Tests
// =============================================================================

func TestComputeStats_Empty(t *testing.T) {
	e := NewIncidentResponseEngine()
	stats := e.ComputeStats()
	if stats.Total != 0 {
		t.Errorf("Total = %d, want 0", stats.Total)
	}
}

func TestComputeStats_WithIncidents(t *testing.T) {
	e := NewIncidentResponseEngine()
	e.RegisterIncident(IncidentRecord{ID: "1", Severity: SeveritySEV0})
	e.RegisterIncident(IncidentRecord{ID: "2", Severity: SeveritySEV1})
	e.RegisterIncident(IncidentRecord{ID: "3", Severity: SeveritySEV1, Status: IncidentResolved,
		CreatedAt: time.Now().Add(-30 * time.Minute), ResolvedAt: time.Now()})
	stats := e.ComputeStats()
	if stats.Total != 3 {
		t.Errorf("Total = %d, want 3", stats.Total)
	}
	if stats.Active != 2 {
		t.Errorf("Active = %d, want 2", stats.Active)
	}
	if stats.Resolved != 1 {
		t.Errorf("Resolved = %d, want 1", stats.Resolved)
	}
	if stats.BySeverity[SeveritySEV0] != 1 {
		t.Errorf("SEV-0 count = %d, want 1", stats.BySeverity[SeveritySEV0])
	}
	if stats.BySeverity[SeveritySEV1] != 2 {
		t.Errorf("SEV-1 count = %d, want 2", stats.BySeverity[SeveritySEV1])
	}
	if stats.MeanResolveTime == 0 {
		t.Error("MeanResolveTime = 0, want non-zero (1 resolved)")
	}
}

func TestFormatStats(t *testing.T) {
	e := NewIncidentResponseEngine()
	e.RegisterIncident(IncidentRecord{ID: "1", Severity: SeveritySEV0, Title: "Test"})
	stats := e.ComputeStats()
	output := FormatStats(stats)
	if !strings.Contains(output, "Total: 1") {
		t.Errorf("FormatStats missing total: %s", output)
	}
	if !strings.Contains(output, "SEV-0: 1") {
		t.Errorf("FormatStats missing SEV-0: %s", output)
	}
}

// =============================================================================
// Format Functions Tests
// =============================================================================

func TestFormatIncident(t *testing.T) {
	inc := &IncidentRecord{
		ID:       "INC-001",
		Severity: SeveritySEV1,
		Title:    "Agent gone rogue",
		Status:   IncidentOpen,
		AgentID:  "agent-sandbox-7",
	}
	output := FormatIncident(inc)
	if !strings.Contains(output, "INC-001") {
		t.Errorf("FormatIncident missing ID: %s", output)
	}
	if !strings.Contains(output, "SEV-1") {
		t.Errorf("FormatIncident missing severity: %s", output)
	}
	if !strings.Contains(output, "Agent gone rogue") {
		t.Errorf("FormatIncident missing title: %s", output)
	}
	if !strings.Contains(output, "agent-sandbox-7") {
		t.Errorf("FormatIncident missing agent: %s", output)
	}
}

func TestFormatIncident_Resolved(t *testing.T) {
	start := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 19, 12, 15, 0, 0, time.UTC)
	inc := &IncidentRecord{
		ID:         "INC-001",
		Severity:   SeveritySEV2,
		Title:      "Chimera down",
		Status:     IncidentResolved,
		CreatedAt:  start,
		ResolvedAt: end,
	}
	output := FormatIncident(inc)
	if !strings.Contains(output, "Resolved") {
		t.Errorf("FormatIncident missing Resolved: %s", output)
	}
	if !strings.Contains(output, "15m") || !strings.Contains(output, "Duration") {
		t.Errorf("FormatIncident missing duration: %s", output)
	}
}

func TestFormatProcedure(t *testing.T) {
	procs := DefaultProcedures()
	var sev0 *ResponseProcedure
	for i := range procs {
		if procs[i].Severity == SeveritySEV0 {
			sev0 = &procs[i]
			break
		}
	}
	output := FormatProcedure(sev0)
	if !strings.Contains(output, "SEV-0") {
		t.Errorf("FormatProcedure missing severity: %s", output)
	}
	if !strings.Contains(output, "Steps:") {
		t.Errorf("FormatProcedure missing Steps: %s", output)
	}
	if !strings.Contains(output, "Kill all agent containers") {
		t.Errorf("FormatProcedure missing first action: %s", output)
	}
}

// =============================================================================
// SortedIncidents Tests
// =============================================================================

func TestSortedIncidents_BySeverity(t *testing.T) {
	e := NewIncidentResponseEngine()
	e.RegisterIncident(IncidentRecord{ID: "3", Severity: SeveritySEV3, Title: "Low"})
	e.RegisterIncident(IncidentRecord{ID: "1", Severity: SeveritySEV0, Title: "Critical"})
	e.RegisterIncident(IncidentRecord{ID: "2", Severity: SeveritySEV1, Title: "High"})
	sorted := e.SortedIncidents()
	if sorted[0].ID != "1" {
		t.Errorf("First should be SEV-0, got %s", sorted[0].ID)
	}
	if sorted[1].ID != "2" {
		t.Errorf("Second should be SEV-1, got %s", sorted[1].ID)
	}
	if sorted[2].ID != "3" {
		t.Errorf("Third should be SEV-3, got %s", sorted[2].ID)
	}
}

func TestSortedIncidents_Empty(t *testing.T) {
	e := NewIncidentResponseEngine()
	sorted := e.SortedIncidents()
	if len(sorted) != 0 {
		t.Errorf("SortedIncidents() = %d, want 0", len(sorted))
	}
}
