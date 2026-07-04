// Package blast implements the blast radius containment verifier per
// specs/SPECIFICATION.md §6.4 (Blast Radius Containment).
//
// The verifier encodes the 8 containment layers, 6 damage types, and 5
// failure scenarios defined in the spec, and exposes a fluent API for
// inspecting the current containment posture of a Helix deployment:
//
//   - ContainmentLayer checks whether a single mitigation is in place.
//   - DamageType records the maximum-impact bounds for a damage category.
//   - BlastRadiusReport aggregates all layers + damage types and produces a
//     pass/fail verdict.
//   - ContainmentFailureScenario captures a known-failure mode + its
//     required mitigation, so operators can verify each scenario's
//     mitigation is actually deployed.
//
// The package is intentionally side-effect-free: it does not call out to
// Docker, Forgejo, or the OpenRouter API. Callers populate the report from
// their own probe results and pass the populated struct into the verifier.
package blast

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// =============================================================================
// Layer & DamageType Constants
// =============================================================================

// ContainmentLayer is one of the 8 layers from spec §6.4. The constant
// values are the strings used in CLI output and report files.
type ContainmentLayer string

const (
	LayerContainerIsolation   ContainmentLayer = "container-isolation"
	LayerBudgetEnforcement    ContainmentLayer = "budget-enforcement"
	LayerGuardrailEnforcement ContainmentLayer = "guardrail-enforcement"
	LayerBranchIsolation      ContainmentLayer = "branch-isolation"
	LayerLockIsolation        ContainmentLayer = "lock-isolation"
	LayerRepositoryIsolation  ContainmentLayer = "repository-isolation"
	LayerReviewIsolation      ContainmentLayer = "review-isolation"
	LayerKeyIsolation         ContainmentLayer = "key-isolation"
)

// AllLayers returns the canonical ordering of containment layers. The order
// matches the spec table for stable CLI output.
func AllLayers() []ContainmentLayer {
	return []ContainmentLayer{
		LayerContainerIsolation,
		LayerBudgetEnforcement,
		LayerGuardrailEnforcement,
		LayerBranchIsolation,
		LayerLockIsolation,
		LayerRepositoryIsolation,
		LayerReviewIsolation,
		LayerKeyIsolation,
	}
}

// DamageType is one of the 6 damage categories from spec §6.4.
type DamageType string

const (
	DamageFinancial         DamageType = "financial"
	DamageCodeQuality       DamageType = "code-quality"
	DamageDataExfiltration  DamageType = "data-exfiltration"
	DamageCredentialTheft   DamageType = "credential-theft"
	DamageServiceDisruption DamageType = "service-disruption"
	DamageReputation        DamageType = "reputation"
)

// AllDamageTypes returns the canonical ordering of damage types.
func AllDamageTypes() []DamageType {
	return []DamageType{
		DamageFinancial,
		DamageCodeQuality,
		DamageDataExfiltration,
		DamageCredentialTheft,
		DamageServiceDisruption,
		DamageReputation,
	}
}

// =============================================================================
// Layer Check
// =============================================================================

// LayerCheck records whether one containment layer is currently in place.
// Present=true means the mitigation is active. Evidence is a free-form
// human-readable string describing how the presence was verified (e.g.
// "OpenRouter key limit=$5, reset=weekly"). Evidence may be empty when
// Present is false.
type LayerCheck struct {
	Layer    ContainmentLayer
	Present  bool
	Evidence string
}

// String renders the check in a stable "PRESENT|ABSENT layer (evidence)"
// format suitable for CLI output.
func (c LayerCheck) String() string {
	state := "PRESENT"
	if !c.Present {
		state = "ABSENT"
	}
	if c.Evidence != "" {
		return fmt.Sprintf("%s %s (%s)", state, c.Layer, c.Evidence)
	}
	return fmt.Sprintf("%s %s", state, c.Layer)
}

// =============================================================================
// Damage Type
// =============================================================================

// DamageBound records the maximum-impact bound for one damage category
// per spec §6.4. Bounds are intentionally free-form strings rather than
// structured values because each category uses different units ($ vs
// "one PR" vs "agent workspace volume").
type DamageBound struct {
	Type      DamageType
	MaxImpact string
	Why       string
}

// String renders the bound in a stable format.
func (b DamageBound) String() string {
	return fmt.Sprintf("%s: %s (%s)", b.Type, b.MaxImpact, b.Why)
}

// =============================================================================
// Failure Scenario
// =============================================================================

// FailureScenario captures one of the 5 documented containment failure
// modes from spec §6.4 and the required mitigation. Operators use
// RequiredMitigation to verify the deployment covers each scenario.
type FailureScenario struct {
	ID                 string
	Scenario           string
	Impact             string
	RequiredMitigation string
}

// AllScenarios returns the canonical set of 5 failure scenarios.
// The order matches the spec table.
func AllScenarios() []FailureScenario {
	return []FailureScenario{
		{
			ID:                 "host-root-escape",
			Scenario:           "Agent gains host root",
			Impact:             "Full platform compromise",
			RequiredMitigation: "Docker user namespaces, no --privileged containers, host kernel hardening",
		},
		{
			ID:                 "management-key-compromise",
			Scenario:           "Management key compromised",
			Impact:             "All friend keys can be created/revoked",
			RequiredMitigation: "Rotate immediately. Audit all key creation events. Store management key offline.",
		},
		{
			ID:                 "forgejo-admin-compromise",
			Scenario:           "Forgejo admin compromised",
			Impact:             "All repos, all agent accounts",
			RequiredMitigation: "Rotate admin password. Audit all user creation. Re-provision all agent accounts.",
		},
		{
			ID:                 "supply-chain-attack",
			Scenario:           "Supply chain attack on dependency",
			Impact:             "Malicious code in agent-generated PR",
			RequiredMitigation: "Dependency pinning, vulnerability scanning, human review catches unexpected deps",
		},
		{
			ID:                 "cross-agent-network",
			Scenario:           "Cross-agent network reach",
			Impact:             "Agent reaches peer agent's workspace or credentials",
			RequiredMitigation: "Per-agent Docker network, no inter-agent routing, egress firewall",
		},
	}
}

// ScenarioByID looks up a scenario by its stable ID. Returns false if
// the ID is unknown.
func ScenarioByID(id string) (FailureScenario, bool) {
	for _, s := range AllScenarios() {
		if s.ID == id {
			return s, true
		}
	}
	return FailureScenario{}, false
}

// =============================================================================
// Report
// =============================================================================

// BlastReport is the aggregated posture of a Helix deployment. Callers
// populate Checks (per-layer status), Bounds (per-damage-type limits),
// and optionally MitigationCoverage (per-scenario mitigation status).
//
// The report itself does not impose pass/fail rules; call
// BlastReport.Validate to run the standard checks. Verifiers may also
// inspect individual fields (e.g. for custom rules).
type BlastReport struct {
	// GeneratedAt is the timestamp the report was assembled.
	GeneratedAt time.Time

	// Checks maps each ContainmentLayer to its current status. All 8
	// layers MUST be present in a complete report — missing keys are
	// treated as absent by Validate.
	Checks map[ContainmentLayer]LayerCheck

	// Bounds maps each DamageType to its declared maximum-impact bound.
	// All 6 damage types MUST be present.
	Bounds map[DamageType]DamageBound

	// MitigationCoverage maps each FailureScenario.ID to whether the
	// required mitigation is in place. Missing keys are treated as
	// uncovered by Validate.
	MitigationCoverage map[string]bool
}

// NewReport returns an empty BlastReport with all 8 layers + 6 damage
// types + 5 scenarios pre-registered as absent/uncovered. Callers fill
// in the checks/bounds/coverage as they probe the deployment.
func NewReport() *BlastReport {
	r := &BlastReport{
		GeneratedAt:        time.Now().UTC(),
		Checks:             make(map[ContainmentLayer]LayerCheck, 8),
		Bounds:             make(map[DamageType]DamageBound, 6),
		MitigationCoverage: make(map[string]bool, 5),
	}
	for _, layer := range AllLayers() {
		r.Checks[layer] = LayerCheck{Layer: layer, Present: false}
	}
	for _, dt := range AllDamageTypes() {
		r.Bounds[dt] = DamageBound{Type: dt}
	}
	for _, s := range AllScenarios() {
		r.MitigationCoverage[s.ID] = false
	}
	return r
}

// =============================================================================
// Validation
// =============================================================================

// ValidationResult is the outcome of running BlastReport.Validate.
type ValidationResult struct {
	// Passed is true if every layer is present, every damage type has a
	// declared bound, and every scenario's mitigation is in place.
	Passed bool

	// MissingLayers lists layers marked absent.
	MissingLayers []ContainmentLayer

	// MissingBounds lists damage types with no declared MaxImpact.
	MissingBounds []DamageType

	// UncoveredScenarios lists scenario IDs whose mitigation is not in place.
	UncoveredScenarios []string

	// LayerCount / DamageCount / ScenarioCount are convenience counts.
	LayerCount    int
	DamageCount   int
	ScenarioCount int
}

// Validate runs the standard containment checks against the report.
// The pass criteria are deliberately strict — every layer must be
// present, every damage type must have a non-empty MaxImpact, and
// every scenario must have its mitigation marked in place. Looser
// validation can be done by callers inspecting individual fields.
func (r *BlastReport) Validate() ValidationResult {
	res := ValidationResult{
		LayerCount:    len(r.Checks),
		DamageCount:   len(r.Bounds),
		ScenarioCount: len(r.MitigationCoverage),
	}

	for _, layer := range AllLayers() {
		check, ok := r.Checks[layer]
		if !ok || !check.Present {
			res.MissingLayers = append(res.MissingLayers, layer)
		}
	}

	for _, dt := range AllDamageTypes() {
		bound, ok := r.Bounds[dt]
		if !ok || strings.TrimSpace(bound.MaxImpact) == "" {
			res.MissingBounds = append(res.MissingBounds, dt)
		}
	}

	for _, s := range AllScenarios() {
		if !r.MitigationCoverage[s.ID] {
			res.UncoveredScenarios = append(res.UncoveredScenarios, s.ID)
		}
	}

	res.Passed = len(res.MissingLayers) == 0 &&
		len(res.MissingBounds) == 0 &&
		len(res.UncoveredScenarios) == 0

	return res
}

// =============================================================================
// Formatting
// =============================================================================

// FormatReport renders a BlastReport in the canonical CLI format:
// section per category, stable layer/damage ordering, deterministic
// scenario order.
func FormatReport(r *BlastReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Blast Radius Report — %s\n", r.GeneratedAt.Format(time.RFC3339))

	fmt.Fprintln(&b, "\nContainment Layers:")
	for _, layer := range AllLayers() {
		check, ok := r.Checks[layer]
		if !ok {
			fmt.Fprintf(&b, "  ABSENT  %s (not reported)\n", layer)
			continue
		}
		fmt.Fprintf(&b, "  %s\n", check.String())
	}

	fmt.Fprintln(&b, "\nDamage Bounds:")
	for _, dt := range AllDamageTypes() {
		bound, ok := r.Bounds[dt]
		if !ok {
			fmt.Fprintf(&b, "  %s: <no bound declared>\n", dt)
			continue
		}
		fmt.Fprintf(&b, "  %s\n", bound.String())
	}

	fmt.Fprintln(&b, "\nFailure Scenario Coverage:")
	for _, s := range AllScenarios() {
		covered, ok := r.MitigationCoverage[s.ID]
		if !ok {
			fmt.Fprintf(&b, "  ?       %s — %s\n", s.ID, s.Scenario)
			continue
		}
		state := "UNCOVERED"
		if covered {
			state = "COVERED  "
		}
		fmt.Fprintf(&b, "  %s %s — %s\n", state, s.ID, s.Scenario)
	}

	res := r.Validate()
	fmt.Fprintf(&b, "\nValidation: %s\n", validationStatus(res))
	if !res.Passed {
		if len(res.MissingLayers) > 0 {
			fmt.Fprintf(&b, "  Missing layers: %s\n", joinLayers(res.MissingLayers))
		}
		if len(res.MissingBounds) > 0 {
			fmt.Fprintf(&b, "  Missing damage bounds: %s\n", joinDamageTypes(res.MissingBounds))
		}
		if len(res.UncoveredScenarios) > 0 {
			sort.Strings(res.UncoveredScenarios)
			fmt.Fprintf(&b, "  Uncovered scenarios: %s\n", strings.Join(res.UncoveredScenarios, ", "))
		}
	}
	return b.String()
}

func validationStatus(r ValidationResult) string {
	if r.Passed {
		return "PASS"
	}
	return "FAIL"
}

func joinLayers(ls []ContainmentLayer) string {
	parts := make([]string, len(ls))
	for i, l := range ls {
		parts[i] = string(l)
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func joinDamageTypes(ds []DamageType) string {
	parts := make([]string, len(ds))
	for i, d := range ds {
		parts[i] = string(d)
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// =============================================================================
// Convenience Constructors
// =============================================================================

// DefaultBounds returns the spec §6.4 "Maximum blast radius" table as a
// map suitable for assigning to BlastReport.Bounds. This is the canonical
// bound set every Helix deployment is expected to enforce.
func DefaultBounds() map[DamageType]DamageBound {
	return map[DamageType]DamageBound{
		DamageFinancial: {
			Type:      DamageFinancial,
			MaxImpact: "budget_usd_weekly (typically $2-10)",
			Why:       "OpenRouter hard limit on key",
		},
		DamageCodeQuality: {
			Type:      DamageCodeQuality,
			MaxImpact: "One bad PR (blocked by review)",
			Why:       "Multi-model review + human co-approval",
		},
		DamageDataExfiltration: {
			Type:      DamageDataExfiltration,
			MaxImpact: "Agent's workspace volume only",
			Why:       "No host FS access, no inter-agent network",
		},
		DamageCredentialTheft: {
			Type:      DamageCredentialTheft,
			MaxImpact: "Agent's own OpenRouter key",
			Why:       "Per-agent keys, no shared credentials",
		},
		DamageServiceDisruption: {
			Type:      DamageServiceDisruption,
			MaxImpact: "Agent's own container",
			Why:       "Container isolation, no shared services",
		},
		DamageReputation: {
			Type:      DamageReputation,
			MaxImpact: "Agent's own identity",
			Why:       "Forgejo user is per-agent, audit trail attributable",
		},
	}
}

// MarkLayer is a small convenience helper for fluent report construction.
// Returns the report to allow chaining.
func (r *BlastReport) MarkLayer(layer ContainmentLayer, present bool, evidence string) *BlastReport {
	r.Checks[layer] = LayerCheck{
		Layer:    layer,
		Present:  present,
		Evidence: evidence,
	}
	return r
}

// MarkCoverage records whether a scenario's mitigation is in place.
func (r *BlastReport) MarkCoverage(scenarioID string, covered bool) *BlastReport {
	r.MitigationCoverage[scenarioID] = covered
	return r
}
