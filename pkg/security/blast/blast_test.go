package blast

import (
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Layer & DamageType Catalogs
// =============================================================================

func TestAllLayers_ContainsAllEightSpecLayers(t *testing.T) {
	layers := AllLayers()
	if len(layers) != 8 {
		t.Fatalf("expected 8 layers, got %d", len(layers))
	}
	want := []ContainmentLayer{
		LayerContainerIsolation, LayerBudgetEnforcement, LayerGuardrailEnforcement,
		LayerBranchIsolation, LayerLockIsolation, LayerRepositoryIsolation,
		LayerReviewIsolation, LayerKeyIsolation,
	}
	for i, w := range want {
		if layers[i] != w {
			t.Errorf("AllLayers()[%d] = %q, want %q", i, layers[i], w)
		}
	}
}

func TestAllDamageTypes_ContainsAllSixSpecDamageTypes(t *testing.T) {
	dts := AllDamageTypes()
	if len(dts) != 6 {
		t.Fatalf("expected 6 damage types, got %d", len(dts))
	}
	want := []DamageType{
		DamageFinancial, DamageCodeQuality, DamageDataExfiltration,
		DamageCredentialTheft, DamageServiceDisruption, DamageReputation,
	}
	for i, w := range want {
		if dts[i] != w {
			t.Errorf("AllDamageTypes()[%d] = %q, want %q", i, dts[i], w)
		}
	}
}

func TestLayerConstants_StableStringValues(t *testing.T) {
	// These strings are part of the report format. Changing them is a
	// breaking change for downstream CLIs and audit consumers.
	cases := map[ContainmentLayer]string{
		LayerContainerIsolation:   "container-isolation",
		LayerBudgetEnforcement:    "budget-enforcement",
		LayerGuardrailEnforcement: "guardrail-enforcement",
		LayerBranchIsolation:      "branch-isolation",
		LayerLockIsolation:        "lock-isolation",
		LayerRepositoryIsolation:  "repository-isolation",
		LayerReviewIsolation:      "review-isolation",
		LayerKeyIsolation:         "key-isolation",
	}
	for layer, want := range cases {
		if string(layer) != want {
			t.Errorf("layer %q mismatch: got %q", want, string(layer))
		}
	}
}

// =============================================================================
// LayerCheck.String
// =============================================================================

func TestLayerCheck_String_PresentWithEvidence(t *testing.T) {
	c := LayerCheck{
		Layer:    LayerBudgetEnforcement,
		Present:  true,
		Evidence: "OpenRouter limit=$5, reset=weekly",
	}
	got := c.String()
	want := "PRESENT budget-enforcement (OpenRouter limit=$5, reset=weekly)"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestLayerCheck_String_PresentWithoutEvidence(t *testing.T) {
	c := LayerCheck{Layer: LayerKeyIsolation, Present: true}
	if got, want := c.String(), "PRESENT key-isolation"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestLayerCheck_String_Absent(t *testing.T) {
	c := LayerCheck{Layer: LayerContainerIsolation, Present: false}
	if got, want := c.String(), "ABSENT container-isolation"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// =============================================================================
// DamageBound.String
// =============================================================================

func TestDamageBound_String(t *testing.T) {
	b := DamageBound{
		Type:      DamageFinancial,
		MaxImpact: "$5/week",
		Why:       "OpenRouter hard limit",
	}
	got := b.String()
	want := `financial: $5/week (OpenRouter hard limit)`
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// =============================================================================
// Failure Scenarios
// =============================================================================

func TestAllScenarios_FiveCanonicalScenarios(t *testing.T) {
	scenarios := AllScenarios()
	if len(scenarios) != 5 {
		t.Fatalf("expected 5 scenarios, got %d", len(scenarios))
	}
	// Every scenario must have non-empty required mitigation.
	for i, s := range scenarios {
		if s.ID == "" {
			t.Errorf("scenario[%d] missing ID", i)
		}
		if s.Scenario == "" {
			t.Errorf("scenario[%d] missing Scenario", i)
		}
		if s.Impact == "" {
			t.Errorf("scenario[%d] missing Impact", i)
		}
		if strings.TrimSpace(s.RequiredMitigation) == "" {
			t.Errorf("scenario[%d] %q missing RequiredMitigation", i, s.ID)
		}
	}
}

func TestAllScenarios_StableIDs(t *testing.T) {
	// The IDs are part of the public API (lookups via ScenarioByID).
	want := []string{
		"host-root-escape",
		"management-key-compromise",
		"forgejo-admin-compromise",
		"supply-chain-attack",
		"cross-agent-network",
	}
	scenarios := AllScenarios()
	for i, w := range want {
		if scenarios[i].ID != w {
			t.Errorf("scenarios[%d].ID = %q, want %q", i, scenarios[i].ID, w)
		}
	}
}

func TestScenarioByID_Found(t *testing.T) {
	s, ok := ScenarioByID("host-root-escape")
	if !ok {
		t.Fatal("expected to find host-root-escape")
	}
	if s.Scenario != "Agent gains host root" {
		t.Errorf("Scenario = %q", s.Scenario)
	}
}

func TestScenarioByID_NotFound(t *testing.T) {
	_, ok := ScenarioByID("nope-not-real")
	if ok {
		t.Error("expected ok=false for unknown ID")
	}
}

// =============================================================================
// NewReport
// =============================================================================

func TestNewReport_PreRegistersAllLayersAndDamage(t *testing.T) {
	r := NewReport()
	if r.GeneratedAt.IsZero() {
		t.Error("GeneratedAt should be set to time.Now()")
	}
	if len(r.Checks) != 8 {
		t.Errorf("Checks size = %d, want 8", len(r.Checks))
	}
	if len(r.Bounds) != 6 {
		t.Errorf("Bounds size = %d, want 6", len(r.Bounds))
	}
	if len(r.MitigationCoverage) != 5 {
		t.Errorf("MitigationCoverage size = %d, want 5", len(r.MitigationCoverage))
	}
	// All pre-registered values should be zero/false.
	for layer, check := range r.Checks {
		if check.Present {
			t.Errorf("layer %q pre-registered as Present=true", layer)
		}
	}
	for dt, bound := range r.Bounds {
		if bound.MaxImpact != "" {
			t.Errorf("damage type %q pre-registered with non-empty MaxImpact: %q", dt, bound.MaxImpact)
		}
	}
	for id, covered := range r.MitigationCoverage {
		if covered {
			t.Errorf("scenario %q pre-registered as covered", id)
		}
	}
}

// =============================================================================
// Validation
// =============================================================================

func TestValidate_EmptyReport_Fails(t *testing.T) {
	r := NewReport()
	res := r.Validate()
	if res.Passed {
		t.Error("empty report should not pass")
	}
	if len(res.MissingLayers) != 8 {
		t.Errorf("MissingLayers size = %d, want 8", len(res.MissingLayers))
	}
	if len(res.MissingBounds) != 6 {
		t.Errorf("MissingBounds size = %d, want 6", len(res.MissingBounds))
	}
	if len(res.UncoveredScenarios) != 5 {
		t.Errorf("UncoveredScenarios size = %d, want 5", len(res.UncoveredScenarios))
	}
}

func TestValidate_AllLayersPresent_BoundsDeclared_ScenariosCovered_Passes(t *testing.T) {
	r := NewReport()
	for _, layer := range AllLayers() {
		r.MarkLayer(layer, true, "verified")
	}
	for dt, b := range DefaultBounds() {
		r.Bounds[dt] = b
	}
	for _, s := range AllScenarios() {
		r.MarkCoverage(s.ID, true)
	}

	res := r.Validate()
	if !res.Passed {
		t.Errorf("expected pass, got fail: missing=%v bounds=%v scenarios=%v",
			res.MissingLayers, res.MissingBounds, res.UncoveredScenarios)
	}
}

func TestValidate_PartialLayers_Fails(t *testing.T) {
	r := NewReport()
	// Mark only 4 of 8 layers as present.
	for i, layer := range AllLayers() {
		if i < 4 {
			r.MarkLayer(layer, true, "ok")
		}
	}
	for dt, b := range DefaultBounds() {
		r.Bounds[dt] = b
	}
	for _, s := range AllScenarios() {
		r.MarkCoverage(s.ID, true)
	}

	res := r.Validate()
	if res.Passed {
		t.Error("expected fail when only 4/8 layers present")
	}
	if len(res.MissingLayers) != 4 {
		t.Errorf("MissingLayers size = %d, want 4", len(res.MissingLayers))
	}
}

func TestValidate_EmptyMaxImpact_Fails(t *testing.T) {
	r := NewReport()
	for _, layer := range AllLayers() {
		r.MarkLayer(layer, true, "ok")
	}
	// Bounds have empty MaxImpact → fail.
	for _, s := range AllScenarios() {
		r.MarkCoverage(s.ID, true)
	}
	res := r.Validate()
	if res.Passed {
		t.Error("expected fail with empty MaxImpact")
	}
	if len(res.MissingBounds) != 6 {
		t.Errorf("MissingBounds size = %d, want 6", len(res.MissingBounds))
	}
}

func TestValidate_WhitespaceOnlyMaxImpact_Fails(t *testing.T) {
	r := NewReport()
	for _, layer := range AllLayers() {
		r.MarkLayer(layer, true, "ok")
	}
	r.Bounds[DamageFinancial] = DamageBound{Type: DamageFinancial, MaxImpact: "   ", Why: "x"}
	res := r.Validate()
	if res.Passed {
		t.Error("expected fail with whitespace-only MaxImpact")
	}
}

func TestValidate_MissingScenario_Fails(t *testing.T) {
	r := NewReport()
	for _, layer := range AllLayers() {
		r.MarkLayer(layer, true, "ok")
	}
	for dt, b := range DefaultBounds() {
		r.Bounds[dt] = b
	}
	// Mark only 3 of 5 scenarios.
	for i, s := range AllScenarios() {
		if i < 3 {
			r.MarkCoverage(s.ID, true)
		}
	}
	res := r.Validate()
	if res.Passed {
		t.Error("expected fail with uncovered scenarios")
	}
	if len(res.UncoveredScenarios) != 2 {
		t.Errorf("UncoveredScenarios size = %d, want 2", len(res.UncoveredScenarios))
	}
}

// =============================================================================
// MarkLayer / MarkCoverage (fluent API)
// =============================================================================

func TestMarkLayer_FluentReturnsReport(t *testing.T) {
	r := NewReport()
	got := r.MarkLayer(LayerKeyIsolation, true, "per-agent OpenRouter keys")
	if got != r {
		t.Error("MarkLayer should return the report for chaining")
	}
	check := r.Checks[LayerKeyIsolation]
	if !check.Present || check.Evidence != "per-agent OpenRouter keys" {
		t.Errorf("MarkLayer did not record: %+v", check)
	}
}

func TestMarkCoverage_RecordsCoverage(t *testing.T) {
	r := NewReport()
	r.MarkCoverage("host-root-escape", true)
	if !r.MitigationCoverage["host-root-escape"] {
		t.Error("expected coverage to be true")
	}
	r.MarkCoverage("host-root-escape", false)
	if r.MitigationCoverage["host-root-escape"] {
		t.Error("expected coverage to be false after re-mark")
	}
}

// =============================================================================
// DefaultBounds
// =============================================================================

func TestDefaultBounds_CoversAllSixDamageTypes(t *testing.T) {
	bounds := DefaultBounds()
	if len(bounds) != 6 {
		t.Fatalf("expected 6 bounds, got %d", len(bounds))
	}
	for _, dt := range AllDamageTypes() {
		b, ok := bounds[dt]
		if !ok {
			t.Errorf("missing bound for %q", dt)
			continue
		}
		if strings.TrimSpace(b.MaxImpact) == "" {
			t.Errorf("bound for %q has empty MaxImpact", dt)
		}
		if b.Type != dt {
			t.Errorf("bound[%q].Type = %q", dt, b.Type)
		}
	}
}

func TestDefaultBounds_FinancialMatchesSpec(t *testing.T) {
	// Spec says: "Financial: budget_usd_weekly (typically $2-10)".
	b := DefaultBounds()[DamageFinancial]
	if !strings.Contains(b.MaxImpact, "budget_usd_weekly") {
		t.Errorf("Financial MaxImpact missing spec phrase: %q", b.MaxImpact)
	}
}

// =============================================================================
// FormatReport
// =============================================================================

func TestFormatReport_ContainsAllSections(t *testing.T) {
	r := NewReport()
	r.GeneratedAt = time.Date(2026, 7, 3, 21, 30, 0, 0, time.UTC)
	for _, layer := range AllLayers() {
		r.MarkLayer(layer, true, "ok")
	}
	for dt, b := range DefaultBounds() {
		r.Bounds[dt] = b
	}
	for _, s := range AllScenarios() {
		r.MarkCoverage(s.ID, true)
	}
	got := FormatReport(r)
	for _, section := range []string{
		"Blast Radius Report",
		"Containment Layers:",
		"Damage Bounds:",
		"Failure Scenario Coverage:",
		"Validation: PASS",
		"container-isolation",
		"key-isolation",
		"host-root-escape",
	} {
		if !strings.Contains(got, section) {
			t.Errorf("FormatReport missing %q", section)
		}
	}
}

func TestFormatReport_FailingReport_ShowsMissingItems(t *testing.T) {
	r := NewReport()
	r.GeneratedAt = time.Date(2026, 7, 3, 21, 30, 0, 0, time.UTC)
	// Half layers absent, half damage bounds missing, half scenarios uncovered.
	for i, layer := range AllLayers() {
		if i >= 4 {
			r.MarkLayer(layer, true, "ok")
		}
	}
	for i, dt := range AllDamageTypes() {
		if i >= 3 {
			r.Bounds[dt] = DefaultBounds()[dt]
		}
	}
	for i, s := range AllScenarios() {
		if i >= 2 {
			r.MarkCoverage(s.ID, true)
		}
	}
	got := FormatReport(r)
	for _, expected := range []string{
		"Validation: FAIL",
		"Missing layers:",
		"Missing damage bounds:",
		"Uncovered scenarios:",
		"container-isolation",
		"code-quality",
		"data-exfiltration",
		"host-root-escape",
		"management-key-compromise",
	} {
		if !strings.Contains(got, expected) {
			t.Errorf("FormatReport(fail) missing %q", expected)
		}
	}
}

func TestFormatReport_AbsentLayerRendering(t *testing.T) {
	r := NewReport()
	r.GeneratedAt = time.Date(2026, 7, 3, 21, 30, 0, 0, time.UTC)
	got := FormatReport(r)
	// All layers should be rendered as ABSENT.
	if strings.Count(got, "ABSENT") != 8 {
		t.Errorf("expected 8 ABSENT markers, got %d in:\n%s", strings.Count(got, "ABSENT"), got)
	}
}

func TestFormatReport_PresentLayerRendering(t *testing.T) {
	r := NewReport()
	r.GeneratedAt = time.Date(2026, 7, 3, 21, 30, 0, 0, time.UTC)
	for _, layer := range AllLayers() {
		r.MarkLayer(layer, true, "evidence")
	}
	got := FormatReport(r)
	if strings.Count(got, "PRESENT") != 8 {
		t.Errorf("expected 8 PRESENT markers, got %d", strings.Count(got, "PRESENT"))
	}
}

func TestFormatReport_ScenarioCoverageRendering(t *testing.T) {
	r := NewReport()
	r.GeneratedAt = time.Date(2026, 7, 3, 21, 30, 0, 0, time.UTC)
	for _, s := range AllScenarios() {
		r.MarkCoverage(s.ID, true)
	}
	got := FormatReport(r)
	if strings.Count(got, "COVERED") != 5 {
		t.Errorf("expected 5 COVERED markers, got %d", strings.Count(got, "COVERED"))
	}
}

// =============================================================================
// Spec Compliance
// =============================================================================

func TestSpecCompliance_AllScenariosHaveMitigation(t *testing.T) {
	// Spec §6.4 lists 4 failure scenarios. We added cross-agent-network as
	// the 5th — verify each has a non-empty mitigation.
	for _, s := range AllScenarios() {
		if strings.TrimSpace(s.RequiredMitigation) == "" {
			t.Errorf("scenario %q missing mitigation", s.ID)
		}
	}
}

func TestSpecCompliance_LayerOrderMatchesSpecTable(t *testing.T) {
	// Spec §6.4 table order: container, budget, guardrail, branch, lock,
	// repository, review, key. AllLayers() must match.
	want := []ContainmentLayer{
		"container-isolation", "budget-enforcement", "guardrail-enforcement",
		"branch-isolation", "lock-isolation", "repository-isolation",
		"review-isolation", "key-isolation",
	}
	got := AllLayers()
	for i, w := range want {
		if got[i] != w {
			t.Errorf("AllLayers()[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestSpecCompliance_DamageTypeOrderMatchesSpecTable(t *testing.T) {
	want := []DamageType{
		"financial", "code-quality", "data-exfiltration",
		"credential-theft", "service-disruption", "reputation",
	}
	got := AllDamageTypes()
	for i, w := range want {
		if got[i] != w {
			t.Errorf("AllDamageTypes()[%d] = %q, want %q", i, got[i], w)
		}
	}
}
