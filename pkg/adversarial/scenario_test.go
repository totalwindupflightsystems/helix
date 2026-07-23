package adversarial

import (
	"context"
	"strings"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// AgentRole
// -----------------------------------------------------------------------------

func TestAgentRole_IsValid(t *testing.T) {
	tests := []struct {
		r    AgentRole
		want bool
	}{
		{RoleAssumptionBuster, true},
		{RoleDevilsAdvocate, true},
		{RoleRedTeam, true},
		{RoleWhiteHat, true},
		{RoleChaosEngineer, true},
		{RoleFinOpsCost, true},
		{AgentRole("@unknown"), false},
		{AgentRole(""), false},
	}
	for _, tt := range tests {
		if got := tt.r.IsValid(); got != tt.want {
			t.Errorf("AgentRole(%q).IsValid() = %v, want %v", tt.r, got, tt.want)
		}
	}
}

func TestAllRoles(t *testing.T) {
	got := AllRoles()
	if len(got) != 6 {
		t.Errorf("AllRoles() = %d, want 6", len(got))
	}
	for i := 1; i < len(got); i++ {
		if string(got[i-1]) >= string(got[i]) {
			t.Errorf("AllRoles() not sorted at %d", i)
		}
	}
}

func TestAgentRole_Description(t *testing.T) {
	tests := []struct {
		r    AgentRole
		want string
	}{
		{RoleAssumptionBuster, "Surfaces undocumented prerequisites, ambiguous specs"},
		{RoleRedTeam, "Adversarial falsification, attack matrix, exploitable paths"},
		{RoleFinOpsCost, "Cost-risk detection, cardinality guardrails"},
	}
	for _, tt := range tests {
		if got := tt.r.Description(); got != tt.want {
			t.Errorf("AgentRole(%q).Description() = %q, want %q", tt.r, got, tt.want)
		}
	}
	if got := AgentRole("@unknown").Description(); got != "" {
		t.Errorf("unknown role Description() = %q, want \"\"", got)
	}
}

// -----------------------------------------------------------------------------
// Scenario.IsValid
// -----------------------------------------------------------------------------

func validScenario() Scenario {
	return Scenario{
		ID:              "test",
		Name:            "Test scenario",
		Description:     "x",
		Role:            RoleRedTeam,
		ExpectedOutcome: OutcomeBlocked,
		Severity:        SevHigh,
		Trigger:         "every PR",
	}
}

func TestScenario_IsValid_OK(t *testing.T) {
	if err := validScenario().IsValid(); err != nil {
		t.Errorf("expected valid: %v", err)
	}
}

func TestScenario_IsValid_MissingID(t *testing.T) {
	s := validScenario()
	s.ID = ""
	if err := s.IsValid(); err == nil {
		t.Error("expected error for missing ID")
	}
}

func TestScenario_IsValid_MissingName(t *testing.T) {
	s := validScenario()
	s.Name = ""
	if err := s.IsValid(); err == nil {
		t.Error("expected error for missing Name")
	}
}

func TestScenario_IsValid_BadRole(t *testing.T) {
	s := validScenario()
	s.Role = AgentRole("@nope")
	if err := s.IsValid(); err == nil {
		t.Error("expected error for invalid Role")
	}
}

func TestScenario_IsValid_BadOutcome(t *testing.T) {
	s := validScenario()
	s.ExpectedOutcome = Outcome("invalid")
	if err := s.IsValid(); err == nil {
		t.Error("expected error for invalid ExpectedOutcome")
	}
}

func TestScenario_IsValid_BadSeverity(t *testing.T) {
	s := validScenario()
	s.Severity = Severity("extreme")
	if err := s.IsValid(); err == nil {
		t.Error("expected error for invalid Severity")
	}
}

func TestScenario_IsValid_NilRunAllowed(t *testing.T) {
	// Run may be nil for unwired scenarios.
	s := validScenario()
	s.Run = nil
	if err := s.IsValid(); err != nil {
		t.Errorf("nil Run should be allowed: %v", err)
	}
}

// -----------------------------------------------------------------------------
// Library.Register/Get/List/ScenariosForRole/All
// -----------------------------------------------------------------------------

func TestLibrary_Register_Get(t *testing.T) {
	lib := NewLibrary()
	if err := lib.Register(validScenario()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, ok := lib.Get("test")
	if !ok {
		t.Fatal("Get(test) not found")
	}
	if got.Name != "Test scenario" {
		t.Errorf("got Name=%q", got.Name)
	}
	if _, ok := lib.Get("nonexistent"); ok {
		t.Error("Get should miss on nonexistent")
	}
}

func TestLibrary_Register_Duplicate(t *testing.T) {
	lib := NewLibrary()
	lib.MustRegister(validScenario())
	err := lib.Register(validScenario())
	if err == nil {
		t.Error("expected duplicate-registration error")
	}
}

func TestLibrary_Register_Invalid(t *testing.T) {
	lib := NewLibrary()
	s := validScenario()
	s.ID = ""
	if err := lib.Register(s); err == nil {
		t.Error("expected validation error for empty ID")
	}
}

func TestLibrary_List_Sorted(t *testing.T) {
	lib := NewLibrary()
	lib.MustRegister(Scenario{ID: "z", Name: "z", Role: RoleRedTeam, ExpectedOutcome: OutcomeBlocked, Severity: SevLow})
	lib.MustRegister(Scenario{ID: "a", Name: "a", Role: RoleRedTeam, ExpectedOutcome: OutcomeBlocked, Severity: SevLow})
	got := lib.List()
	if got[0] != "a" || got[1] != "z" {
		t.Errorf("List() = %v, want [a z]", got)
	}
}

func TestLibrary_ScenariosForRole(t *testing.T) {
	lib := NewLibrary()
	lib.MustRegister(Scenario{ID: "a", Name: "a", Role: RoleRedTeam, ExpectedOutcome: OutcomeBlocked, Severity: SevHigh})
	lib.MustRegister(Scenario{ID: "b", Name: "b", Role: RoleRedTeam, ExpectedOutcome: OutcomeBlocked, Severity: SevHigh})
	lib.MustRegister(Scenario{ID: "c", Name: "c", Role: RoleWhiteHat, ExpectedOutcome: OutcomeBlocked, Severity: SevHigh})
	got := lib.ScenariosForRole(RoleRedTeam)
	if len(got) != 2 {
		t.Errorf("ScenariosForRole(redteam) = %d, want 2", len(got))
	}
}

func TestLibrary_All(t *testing.T) {
	lib := NewLibrary()
	lib.MustRegister(validScenario())
	all := lib.All()
	if len(all) != 1 {
		t.Errorf("All() = %d, want 1", len(all))
	}
}

// -----------------------------------------------------------------------------
// SpecScenarios — 5 canonical scenarios from spec §12.4
// -----------------------------------------------------------------------------

func TestSpecScenarios_Count(t *testing.T) {
	scenarios := SpecScenarios()
	if len(scenarios) != 5 {
		t.Errorf("SpecScenarios() = %d, want 5", len(scenarios))
	}
}

func TestSpecScenarios_AllValid(t *testing.T) {
	for _, s := range SpecScenarios() {
		if err := s.IsValid(); err != nil {
			t.Errorf("scenario %q: %v", s.ID, err)
		}
	}
}

func TestSpecScenarios_AllHaveRun(t *testing.T) {
	for _, s := range SpecScenarios() {
		if s.Run == nil {
			t.Errorf("scenario %q: Run is nil", s.ID)
		}
	}
}

func TestSpecScenarios_HasGateBypass(t *testing.T) {
	scenarios := SpecScenarios()
	var found *Scenario
	for i := range scenarios {
		if scenarios[i].ID == "gate-bypass" {
			found = &scenarios[i]
			break
		}
	}
	if found == nil {
		t.Fatal("missing gate-bypass scenario")
	}
	if found.ExpectedOutcome != OutcomeBlocked {
		t.Errorf("gate-bypass ExpectedOutcome=%q, want blocked", found.ExpectedOutcome)
	}
	if found.Severity != SevCritical {
		t.Errorf("gate-bypass Severity=%q, want critical", found.Severity)
	}
	if found.Role != RoleRedTeam {
		t.Errorf("gate-bypass Role=%q, want redteam", found.Role)
	}
}

func TestSpecScenarios_HasBudgetExhaustion(t *testing.T) {
	scenarios := SpecScenarios()
	var found *Scenario
	for i := range scenarios {
		if scenarios[i].ID == "budget-exhaustion" {
			found = &scenarios[i]
			break
		}
	}
	if found == nil {
		t.Fatal("missing budget-exhaustion scenario")
	}
	if found.ExpectedOutcome != OutcomeIsolatedOnly {
		t.Errorf("budget-exhaustion ExpectedOutcome=%q, want isolated_only", found.ExpectedOutcome)
	}
	if found.Role != RoleFinOpsCost {
		t.Errorf("budget-exhaustion Role=%q, want finops", found.Role)
	}
}

func TestSpecScenarios_HasKeyLeak(t *testing.T) {
	scenarios := SpecScenarios()
	var found *Scenario
	for i := range scenarios {
		if scenarios[i].ID == "key-leak" {
			found = &scenarios[i]
			break
		}
	}
	if found == nil {
		t.Fatal("missing key-leak scenario")
	}
	if found.ExpectedOutcome != OutcomeBlocked {
		t.Errorf("key-leak ExpectedOutcome=%q, want blocked", found.ExpectedOutcome)
	}
	if found.Role != RoleWhiteHat {
		t.Errorf("key-leak Role=%q, want whitehat", found.Role)
	}
}

func TestSpecScenarios_HasNetworkIsolation(t *testing.T) {
	scenarios := SpecScenarios()
	var found *Scenario
	for i := range scenarios {
		if scenarios[i].ID == "network-isolation-bypass" {
			found = &scenarios[i]
			break
		}
	}
	if found == nil {
		t.Fatal("missing network-isolation-bypass scenario")
	}
	if found.Role != RoleChaosEngineer {
		t.Errorf("network-isolation-bypass Role=%q, want chaos-engineer", found.Role)
	}
}

func TestSpecScenarios_HasLockRace(t *testing.T) {
	scenarios := SpecScenarios()
	var found *Scenario
	for i := range scenarios {
		if scenarios[i].ID == "ralph-lock-race" {
			found = &scenarios[i]
			break
		}
	}
	if found == nil {
		t.Fatal("missing ralph-lock-race scenario")
	}
	if found.ExpectedOutcome != OutcomeIsolatedOnly {
		t.Errorf("ralph-lock-race ExpectedOutcome=%q, want isolated_only", found.ExpectedOutcome)
	}
}

// -----------------------------------------------------------------------------
// DefaultLibrary
// -----------------------------------------------------------------------------

func TestDefaultLibrary_HasAllSpecScenarios(t *testing.T) {
	lib, err := DefaultLibrary()
	if err != nil {
		t.Fatalf("DefaultLibrary: %v", err)
	}
	if len(lib.List()) != 5 {
		t.Errorf("DefaultLibrary has %d scenarios, want 5", len(lib.List()))
	}
	for _, id := range []string{
		"gate-bypass", "budget-exhaustion", "key-leak",
		"network-isolation-bypass", "ralph-lock-race",
	} {
		if _, ok := lib.Get(id); !ok {
			t.Errorf("DefaultLibrary missing %s", id)
		}
	}
}

// -----------------------------------------------------------------------------
// RunOne / RunAll
// -----------------------------------------------------------------------------

func TestLibrary_RunOne_NotFound(t *testing.T) {
	lib := NewLibrary()
	_, err := lib.RunOne(context.Background(), "nope")
	if err == nil {
		t.Error("expected error for unknown scenario")
	}
}

func TestLibrary_RunOne_OK(t *testing.T) {
	lib, err := DefaultLibrary()
	if err != nil {
		t.Fatalf("DefaultLibrary: %v", err)
	}
	res, err := lib.RunOne(context.Background(), "gate-bypass")
	if err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	if !res.Pass {
		t.Errorf("gate-bypass should pass with stub Run: %+v", res)
	}
	if res.ActualOutcome != OutcomeBlocked {
		t.Errorf("gate-bypass ActualOutcome=%q, want blocked", res.ActualOutcome)
	}
}

func TestLibrary_RunAll_AllPass(t *testing.T) {
	lib, err := DefaultLibrary()
	if err != nil {
		t.Fatalf("DefaultLibrary: %v", err)
	}
	results := lib.RunAll(context.Background())
	if len(results) != 5 {
		t.Errorf("RunAll returned %d results, want 5", len(results))
	}
	for _, r := range results {
		if !r.Pass {
			t.Errorf("scenario %s failed: expected=%s actual=%s", r.ScenarioID, r.ExpectedOutcome, r.ActualOutcome)
		}
	}
}

func TestLibrary_RunAll_ContextCancel(t *testing.T) {
	lib := NewLibrary()
	lib.MustRegister(Scenario{
		ID: "slow", Name: "slow", Role: RoleRedTeam,
		ExpectedOutcome: OutcomeBlocked, Severity: SevLow,
		Run: func(ctx context.Context) (Outcome, string) {
			select {
			case <-time.After(10 * time.Second):
				return OutcomeBlocked, "completed"
			case <-ctx.Done():
				return OutcomeBlocked, "cancelled"
			}
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	results := lib.RunAll(ctx)
	if len(results) != 0 {
		t.Errorf("cancelled context should return 0 results, got %d", len(results))
	}
}

func TestLibrary_RunOne_NilRun(t *testing.T) {
	lib := NewLibrary()
	lib.MustRegister(Scenario{
		ID: "nilrun", Name: "nil run", Role: RoleRedTeam,
		ExpectedOutcome: OutcomeBlocked, Severity: SevLow,
		// Run is nil
	})
	res, err := lib.RunOne(context.Background(), "nilrun")
	if err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	if res.Pass {
		t.Error("scenario with nil Run should not pass")
	}
	if res.Details == "" {
		t.Error("expected non-empty Details explaining nil Run")
	}
}

// -----------------------------------------------------------------------------
// Report
// -----------------------------------------------------------------------------

func TestGenerateReport_AllPass(t *testing.T) {
	lib, err := DefaultLibrary()
	if err != nil {
		t.Fatalf("DefaultLibrary: %v", err)
	}
	results := lib.RunAll(context.Background())
	rep := GenerateReport(results)
	if rep.Total != 5 {
		t.Errorf("Total=%d, want 5", rep.Total)
	}
	if rep.Pass != 5 {
		t.Errorf("Pass=%d, want 5", rep.Pass)
	}
	if rep.Fail != 0 {
		t.Errorf("Fail=%d, want 0", rep.Fail)
	}
	if rep.PassRate() != 1.0 {
		t.Errorf("PassRate=%v, want 1.0", rep.PassRate())
	}
}

func TestGenerateReport_WithFailure(t *testing.T) {
	lib := NewLibrary()
	lib.MustRegister(Scenario{
		ID: "failcase", Name: "fail case", Role: RoleRedTeam,
		ExpectedOutcome: OutcomeBlocked, Severity: SevHigh,
		Run: func(ctx context.Context) (Outcome, string) {
			return OutcomeAllowed, "unexpectedly allowed"
		},
	})
	results := lib.RunAll(context.Background())
	rep := GenerateReport(results)
	if rep.Pass != 0 {
		t.Errorf("Pass=%d, want 0", rep.Pass)
	}
	if rep.Fail != 1 {
		t.Errorf("Fail=%d, want 1", rep.Fail)
	}
	if len(rep.FailureIDs) != 1 || rep.FailureIDs[0] != "failcase" {
		t.Errorf("FailureIDs=%v, want [failcase]", rep.FailureIDs)
	}
}

func TestGenerateReport_Empty(t *testing.T) {
	rep := GenerateReport(nil)
	if rep.Total != 0 || rep.PassRate() != 0 {
		t.Errorf("empty report should be 0/0: %+v", rep)
	}
}

func TestGenerateReport_ByRoleBySeverity(t *testing.T) {
	lib := NewLibrary()
	lib.MustRegister(Scenario{
		ID: "x", Name: "x", Role: RoleRedTeam,
		ExpectedOutcome: OutcomeBlocked, Severity: SevCritical,
		Run: func(ctx context.Context) (Outcome, string) { return OutcomeBlocked, "ok" },
	})
	results := lib.RunAll(context.Background())
	rep := GenerateReport(results)
	if rep.ByRole[RoleRedTeam] != 1 {
		t.Errorf("ByRole[redteam]=%d, want 1", rep.ByRole[RoleRedTeam])
	}
	if rep.BySeverity[SevCritical] != 1 {
		t.Errorf("BySeverity[critical]=%d, want 1", rep.BySeverity[SevCritical])
	}
}

// -----------------------------------------------------------------------------
// Formatting
// -----------------------------------------------------------------------------

func TestFormatReport(t *testing.T) {
	lib, err := DefaultLibrary()
	if err != nil {
		t.Fatalf("DefaultLibrary: %v", err)
	}
	results := lib.RunAll(context.Background())
	rep := GenerateReport(results)
	got := rep.FormatReport()
	wants := []string{"Helix Adversarial Test Report", "Total: 5", "Pass: 5", "Fail: 0"}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in report", w)
		}
	}
}

func TestFormatResults(t *testing.T) {
	lib, err := DefaultLibrary()
	if err != nil {
		t.Fatalf("DefaultLibrary: %v", err)
	}
	results := lib.RunAll(context.Background())
	got := FormatResults(results)
	if !strings.Contains(got, "Scenario") {
		t.Errorf("missing header in results table: %s", got[:50])
	}
}

func TestFormatResult(t *testing.T) {
	r := Result{
		ScenarioID:      "test",
		Name:            "Test scenario",
		Role:            RoleRedTeam,
		ExpectedOutcome: OutcomeBlocked,
		ActualOutcome:   OutcomeBlocked,
		Pass:            true,
		Severity:        SevHigh,
		Details:         "all good",
		Duration:        10 * time.Millisecond,
	}
	got := FormatResult(r)
	wants := []string{"[PASS]", "Test scenario", "test", "redteam", "blocked"}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in result", w)
		}
	}
}

// -----------------------------------------------------------------------------
// Truncate helper
// -----------------------------------------------------------------------------

func TestTruncate(t *testing.T) {
	tests := []struct {
		s    string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "he..."},
		{"hi", 5, "hi"},
		{"abc", 0, ""},
	}
	for _, tt := range tests {
		if got := truncate(tt.s, tt.n); got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
		}
	}
}

// -----------------------------------------------------------------------------
// Spec verbatim coverage
// -----------------------------------------------------------------------------

func TestSpecCoverage_AllRoles(t *testing.T) {
	// Spec §12.4 roster table lists 6 roles but only 5 example scenarios.
	// DefaultLibrary must have at least one scenario for at least 5 of 6 roles
	// (the 6th role, @devils-advocate, is documented in the roster but has no
	// concrete scenario in §12.4).
	lib, err := DefaultLibrary()
	if err != nil {
		t.Fatalf("DefaultLibrary: %v", err)
	}
	covered := 0
	for _, role := range AllRoles() {
		if len(lib.ScenariosForRole(role)) > 0 {
			covered++
		}
	}
	if covered < 5 {
		t.Errorf("DefaultLibrary covers %d roles, want >= 5", covered)
	}
}

func TestSpecCoverage_AllSeverities(t *testing.T) {
	lib, err := DefaultLibrary()
	if err != nil {
		t.Fatalf("DefaultLibrary: %v", err)
	}
	all := lib.All()
	hasMedium, hasHigh, hasCritical := false, false, false
	for _, s := range all {
		switch s.Severity {
		case SevLow:
			// hasLow not tracked — SevLow scenarios are valid but not required
			_ = s
		case SevMedium:
			hasMedium = true
		case SevHigh:
			hasHigh = true
		case SevCritical:
			hasCritical = true
		}
	}
	if !hasHigh {
		t.Error("spec coverage: missing High severity")
	}
	if !hasCritical {
		t.Error("spec coverage: missing Critical severity")
	}
	if !hasMedium {
		t.Error("spec coverage: missing Medium severity")
	}
}
