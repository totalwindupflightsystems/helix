// Package mergegate implements the pre-merge validation gate that composes
// all Helix quality checks into a single decision point.
//
// Per specs:
//   - adversarial-review.md §Integration Points: "GitReins pre-commit: Blocks
//     merges without valid evidence bundles"
//   - production-verification.md §Integration Points: "GitReins merge gate:
//     Verifies behavior contract exists and is valid before merge"
//   - trust-model.md §Integration Points: "GitReins pre-commit: Block merges
//     from agents below required trust tier for changed file categories"
//
// The MergeGate validates five preconditions before allowing a merge:
//  1. Evidence bundle exists and signatures are valid
//  2. Behavior contract exists and assertions are well-formed
//  3. Trust tier meets minimum requirement for changed file categories
//  4. Consensus threshold was met (from adversarial review)
//  5. Cost guard was approved (within tier budget)
package mergegate

import (
	"crypto/ed25519"
	"fmt"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/dispatcher"
	"github.com/totalwindupflightsystems/helix/pkg/review"
	"github.com/totalwindupflightsystems/helix/pkg/trust"
	"github.com/totalwindupflightsystems/helix/pkg/verify"
)

// ============================================================================
// MergeDecision
// ============================================================================

// MergeDecision is the overall outcome of running all merge gate checks.
type MergeDecision string

const (
	DecisionAllowed   MergeDecision = "ALLOWED"
	DecisionBlocked   MergeDecision = "BLOCKED"
	DecisionEscalated MergeDecision = "ESCALATED"
)

// ============================================================================
// CheckStatus
// ============================================================================

// CheckStatus is the pass/fail status of a single gate check.
type CheckStatus string

const (
	CheckPass    CheckStatus = "PASS"
	CheckFail    CheckStatus = "FAIL"
	CheckSkipped CheckStatus = "SKIPPED"
	CheckWarning CheckStatus = "WARN"
)

// ============================================================================
// CheckResult
// ============================================================================

// CheckResult is the outcome of a single gate check.
type CheckResult struct {
	Name    string      `json:"name"`
	Status  CheckStatus `json:"status"`
	Reason  string      `json:"reason"`
	Details string      `json:"details,omitempty"`
}

// IsPassing returns true if the check passed or was skipped.
func (c CheckResult) IsPassing() bool {
	return c.Status == CheckPass || c.Status == CheckSkipped
}

// ============================================================================
// MergeRequest — input to the gate
// ============================================================================

// MergeRequest is the input to the merge gate. It carries all the artifacts
// that the gate needs to validate.
type MergeRequest struct {
	AgentID      string          `json:"agent_id"`
	AgentTier    trust.TrustTier `json:"agent_tier"`
	ChangedFiles []string        `json:"changed_files"`

	// Evidence bundle from the adversarial review pipeline.
	Bundle *review.EvidenceBundle `json:"bundle,omitempty"`

	// Behavior contract committed with the code.
	Contract *verify.BehaviorContract `json:"contract,omitempty"`

	// Cost guard result from pre-dispatch check.
	CostResult *dispatcher.CostGuardResult `json:"cost_result,omitempty"`

	// Signing keys for evidence bundle verification (role → public key).
	// If empty, signature verification is skipped (development mode).
	SignKeys map[string]ed25519.PublicKey `json:"-"`
}

// ============================================================================
// GateReport — output of the gate
// ============================================================================

// GateReport is the full output of running all merge gate checks.
type GateReport struct {
	Decision  MergeDecision   `json:"decision"`
	AgentID   string          `json:"agent_id"`
	AgentTier trust.TrustTier `json:"agent_tier"`
	Checks    []CheckResult   `json:"checks"`
	Blockers  []string        `json:"blockers,omitempty"`
}

// IsAllowed returns true if the overall decision allows the merge.
func (r GateReport) IsAllowed() bool {
	return r.Decision == DecisionAllowed
}

// IsBlocked returns true if the merge is blocked by one or more failed checks.
func (r GateReport) IsBlocked() bool {
	return r.Decision == DecisionBlocked
}

// Summary returns a human-readable summary of the gate result.
func (r GateReport) Summary() string {
	status := map[CheckStatus]int{CheckPass: 0, CheckFail: 0, CheckSkipped: 0, CheckWarning: 0}
	for _, c := range r.Checks {
		status[c.Status]++
	}
	return fmt.Sprintf("decision=%s agent=%s tier=%s checks: %d pass, %d fail, %d skip, %d warn",
		r.Decision, r.AgentID, r.AgentTier,
		status[CheckPass], status[CheckFail], status[CheckSkipped], status[CheckWarning])
}

// ============================================================================
// MergeGate
// ============================================================================

// MergeGate validates all preconditions before allowing a merge.
type MergeGate struct {
	// skipContractCheck disables the behavior contract check entirely.
	// Use when the project has no production verification contracts yet.
	skipContract bool

	// skipCostCheck disables the cost guard check.
	// Use when cost tracking is not yet wired.
	skipCost bool

	// requireSignedBundle requires all evidence bundle signatures to be present and valid.
	// If false, unsigned bundles are allowed (development mode).
	requireSignedBundle bool
}

// GateOption configures a MergeGate.
type GateOption func(*MergeGate)

// WithContractSkipped disables the behavior contract check.
func WithContractSkipped() GateOption {
	return func(g *MergeGate) { g.skipContract = true }
}

// WithCostSkipped disables the cost guard check.
func WithCostSkipped() GateOption {
	return func(g *MergeGate) { g.skipCost = true }
}

// WithSignedBundleRequired requires evidence bundles to have valid signatures.
func WithSignedBundleRequired() GateOption {
	return func(g *MergeGate) { g.requireSignedBundle = true }
}

// NewMergeGate creates a MergeGate with the given options.
func NewMergeGate(opts ...GateOption) *MergeGate {
	g := &MergeGate{}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// Evaluate runs all gate checks and returns the final merge decision.
//
// The decision logic:
//   - Any FAIL → BLOCKED
//   - No FAILs, but any WARN → ESCALATED
//   - All PASS/SKIP → ALLOWED
func (g *MergeGate) Evaluate(req MergeRequest) GateReport {
	report := GateReport{
		AgentID:   req.AgentID,
		AgentTier: req.AgentTier,
	}

	// Run all checks.
	report.Checks = append(report.Checks,
		g.checkEvidenceBundle(req),
		g.checkConsensus(req),
	)

	if !g.skipContract {
		report.Checks = append(report.Checks, g.checkBehaviorContract(req))
	}

	report.Checks = append(report.Checks,
		g.checkTrustTier(req),
	)

	if !g.skipCost {
		report.Checks = append(report.Checks, g.checkCostGuard(req))
	}

	// Determine overall decision.
	hasFail := false
	hasWarn := false
	for _, c := range report.Checks {
		if c.Status == CheckFail {
			hasFail = true
			report.Blockers = append(report.Blockers, c.Reason)
		}
		if c.Status == CheckWarning {
			hasWarn = true
		}
	}

	switch {
	case hasFail:
		report.Decision = DecisionBlocked
	case hasWarn:
		report.Decision = DecisionEscalated
	default:
		report.Decision = DecisionAllowed
	}

	return report
}

// ============================================================================
// Individual checks
// ============================================================================

// checkEvidenceBundle validates that an evidence bundle exists and (if required)
// its signatures are valid.
//
// Spec: adversarial-review.md §Evidence Bundles — "Any merge without a valid
// evidence bundle is auto-reverted."
func (g *MergeGate) checkEvidenceBundle(req MergeRequest) CheckResult {
	const name = "evidence_bundle"

	if req.Bundle == nil {
		return CheckResult{
			Name:   name,
			Status: CheckFail,
			Reason: "no evidence bundle attached to merge request",
		}
	}

	b := req.Bundle

	// Basic completeness checks.
	if b.ReviewID == "" {
		return CheckResult{
			Name:   name,
			Status: CheckFail,
			Reason: "evidence bundle has no review_id",
		}
	}
	if b.PRURL == "" {
		return CheckResult{
			Name:   name,
			Status: CheckFail,
			Reason: "evidence bundle has no pr_url",
		}
	}

	// Signature verification (if required and keys provided).
	if g.requireSignedBundle && len(req.SignKeys) > 0 {
		results, err := b.VerifyAllSignatures(req.SignKeys)
		if err != nil {
			return CheckResult{
				Name:   name,
				Status: CheckFail,
				Reason: fmt.Sprintf("signature verification error: %v", err),
			}
		}
		allValid := true
		var invalidRoles []string
		for role, valid := range results {
			if !valid {
				allValid = false
				invalidRoles = append(invalidRoles, role)
			}
		}
		if !allValid {
			return CheckResult{
				Name:    name,
				Status:  CheckFail,
				Reason:  fmt.Sprintf("invalid signatures for roles: %s", strings.Join(invalidRoles, ", ")),
				Details: fmt.Sprintf("bundle_id=%s", b.BundleID()),
			}
		}
	}

	return CheckResult{
		Name:    name,
		Status:  CheckPass,
		Reason:  "evidence bundle present and valid",
		Details: fmt.Sprintf("review_id=%s, bundle_id=%s, findings=%d", b.ReviewID, b.BundleID(), len(b.Findings)),
	}
}

// checkConsensus validates that the review consensus allows the merge.
//
// Spec: adversarial-review.md §Review Criteria by Change Category —
// consensus thresholds vary by change category (3/3 for contract, 2/2 for behavioral, etc.)
func (g *MergeGate) checkConsensus(req MergeRequest) CheckResult {
	const name = "consensus"

	if req.Bundle == nil {
		return CheckResult{
			Name:   name,
			Status: CheckFail,
			Reason: "cannot check consensus without evidence bundle",
		}
	}

	consensus := req.Bundle.Consensus

	if consensus.IsBlocked() {
		return CheckResult{
			Name:   name,
			Status: CheckFail,
			Reason: fmt.Sprintf("review consensus resolution is 'blocked' (primary=%s, adversarial=%s, audit=%s)",
				consensus.PrimaryVerdict, consensus.AdversarialVerdict, consensus.AuditVerdict),
		}
	}

	if consensus.NeedsTieBreaker() {
		return CheckResult{
			Name:   name,
			Status: CheckWarning,
			Reason: "review consensus requires tie-breaker — escalate to Chimera arbiter",
		}
	}

	if consensus.IsApproved() {
		return CheckResult{
			Name:   name,
			Status: CheckPass,
			Reason: fmt.Sprintf("review consensus approved (primary=%s, adversarial=%s, audit=%s)",
				consensus.PrimaryVerdict, consensus.AdversarialVerdict, consensus.AuditVerdict),
		}
	}

	// No consensus set — default to fail.
	return CheckResult{
		Name:   name,
		Status: CheckFail,
		Reason: "review consensus has no resolution — evidence bundle may be incomplete",
	}
}

// checkBehaviorContract validates that a behavior contract exists and its
// assertions are well-formed.
//
// Spec: production-verification.md §Behavior Contracts — "GitReins merge gate:
// Verifies behavior contract exists and is valid before merge."
func (g *MergeGate) checkBehaviorContract(req MergeRequest) CheckResult {
	const name = "behavior_contract"

	if req.Contract == nil {
		return CheckResult{
			Name:   name,
			Status: CheckFail,
			Reason: "no behavior contract committed with the code",
		}
	}

	if err := req.Contract.Validate(); err != nil {
		return CheckResult{
			Name:   name,
			Status: CheckFail,
			Reason: fmt.Sprintf("behavior contract validation failed: %v", err),
		}
	}

	assertions := len(req.Contract.Contract.Assertions)
	breach := req.Contract.Contract.BreachAction
	if breach == "" {
		breach = "notify_only"
	}

	return CheckResult{
		Name:   name,
		Status: CheckPass,
		Reason: "behavior contract present and valid",
		Details: fmt.Sprintf("name=%s, agent=%s, assertions=%d, breach_action=%s",
			req.Contract.Contract.Name, req.Contract.Contract.Agent, assertions, breach),
	}
}

// checkTrustTier validates that the agent's trust tier is sufficient for the
// changed file categories.
//
// Spec: trust-model.md §Integration Points — "Block merges from agents below
// required trust tier for changed file categories."
func (g *MergeGate) checkTrustTier(req MergeRequest) CheckResult {
	const name = "trust_tier"

	if len(req.ChangedFiles) == 0 {
		return CheckResult{
			Name:   name,
			Status: CheckPass,
			Reason: "no changed files to check — tier requirement trivially met",
		}
	}

	agentRank := tierRank(req.AgentTier)
	categories := make(map[string]bool)
	var violations []string

	for _, file := range req.ChangedFiles {
		cat := classifyFile(file)
		categories[string(cat)] = true
		minTier := minTierForCategory(cat)
		if agentRank < tierRank(minTier) {
			violations = append(violations,
				fmt.Sprintf("%s requires %s+ (agent is %s)", file, minTier, req.AgentTier))
		}
	}

	if len(violations) > 0 {
		return CheckResult{
			Name:    name,
			Status:  CheckFail,
			Reason:  fmt.Sprintf("trust tier insufficient for %d file(s): %s", len(violations), strings.Join(violations, "; ")),
			Details: fmt.Sprintf("categories=%v", mapKeys(categories)),
		}
	}

	return CheckResult{
		Name:    name,
		Status:  CheckPass,
		Reason:  fmt.Sprintf("agent tier %s meets all file category requirements", req.AgentTier),
		Details: fmt.Sprintf("categories=%v", mapKeys(categories)),
	}
}

// checkCostGuard validates that the cost guard approved the task.
//
// Spec: trust-model.md §Integration Points — "Estimator: Cost caps enforced
// at job dispatch based on current tier."
func (g *MergeGate) checkCostGuard(req MergeRequest) CheckResult {
	const name = "cost_guard"

	if req.CostResult == nil {
		return CheckResult{
			Name:   name,
			Status: CheckSkipped,
			Reason: "no cost guard result attached — skipping cost check",
		}
	}

	cg := req.CostResult

	if cg.IsBlocked() {
		return CheckResult{
			Name:    name,
			Status:  CheckFail,
			Reason:  cg.Reason,
			Details: fmt.Sprintf("estimated=$%.2f, cap=$%.2f", cg.EstimatedCost, cg.CostCapPerJob),
		}
	}

	if cg.IsEscalated() {
		return CheckResult{
			Name:    name,
			Status:  CheckWarning,
			Reason:  cg.Reason,
			Details: fmt.Sprintf("estimated=$%.2f, cap=$%.2f", cg.EstimatedCost, cg.CostCapPerJob),
		}
	}

	return CheckResult{
		Name:    name,
		Status:  CheckPass,
		Reason:  cg.Reason,
		Details: fmt.Sprintf("estimated=$%.2f, cap=$%.2f", cg.EstimatedCost, cg.CostCapPerJob),
	}
}

// ============================================================================
// File category classification (mirrors scripts/check-trust-tier.sh)
// ============================================================================

// FileCategory represents the type of file being changed.
type FileCategory string

const (
	CatIaC    FileCategory = "iac"
	CatCICD   FileCategory = "cicd"
	CatAuth   FileCategory = "auth"
	CatSchema FileCategory = "schema"
	CatDocs   FileCategory = "docs"
	CatTests  FileCategory = "tests"
	CatCode   FileCategory = "code"
)

// classifyFile determines the file category from a file path.
// This mirrors the logic in scripts/check-trust-tier.sh.
func classifyFile(path string) FileCategory {
	lower := strings.ToLower(path)

	// IaC files
	if strings.HasSuffix(lower, ".tf") || strings.HasSuffix(lower, ".tfvars") ||
		strings.Contains(lower, "terraform/") || strings.HasSuffix(lower, ".tfstate") {
		return CatIaC
	}

	// Docker/compose
	if strings.Contains(lower, "dockerfile") || strings.Contains(lower, "docker-compose") {
		return CatCICD
	}

	// YAML/CI-CD
	if strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml") {
		if strings.Contains(lower, "ci") || strings.Contains(lower, "cd") ||
			strings.Contains(lower, "pipeline") || strings.Contains(lower, "deploy") ||
			strings.Contains(lower, "action") || strings.Contains(lower, "workflow") {
			return CatCICD
		}
		return CatCode
	}

	// Auth/security
	if strings.Contains(lower, "auth") || strings.Contains(lower, "security") ||
		strings.Contains(lower, "secret") || strings.Contains(lower, "credential") ||
		strings.Contains(lower, "session") || strings.Contains(lower, "token") ||
		strings.Contains(lower, "oauth") {
		return CatAuth
	}

	// Source code extensions
	if strings.HasSuffix(lower, ".go") || strings.HasSuffix(lower, ".rs") ||
		strings.HasSuffix(lower, ".py") || strings.HasSuffix(lower, ".ts") ||
		strings.HasSuffix(lower, ".js") || strings.HasSuffix(lower, ".java") ||
		strings.HasSuffix(lower, ".rb") || strings.HasSuffix(lower, ".swift") {
		return CatCode
	}

	// Documentation
	if strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".rst") ||
		strings.HasSuffix(lower, ".txt") || strings.HasPrefix(lower, "docs/") {
		return CatDocs
	}

	// Tests
	if strings.Contains(lower, "_test.go") || strings.Contains(lower, "_test.py") ||
		strings.Contains(lower, "_test.rs") || strings.Contains(lower, "_test.ts") ||
		strings.Contains(lower, "_test.js") || strings.Contains(lower, "spec") ||
		strings.Contains(lower, "test") {
		return CatTests
	}

	// Schemas
	if strings.HasSuffix(lower, ".sql") || strings.HasSuffix(lower, ".proto") ||
		strings.Contains(lower, "openapi") || strings.Contains(lower, "swagger") {
		return CatSchema
	}

	return CatCode
}

// minTierForCategory returns the minimum trust tier required for a file category.
// This matches the mapping in scripts/check-trust-tier.sh.
func minTierForCategory(cat FileCategory) trust.TrustTier {
	switch cat {
	case CatIaC:
		return trust.TierObserved
	case CatCICD:
		return trust.TierVeteran
	case CatAuth:
		return trust.TierTrusted
	case CatSchema:
		return trust.TierObserved
	case CatDocs, CatTests, CatCode:
		return trust.TierProvisional
	default:
		return trust.TierProvisional
	}
}

// ============================================================================
// Helpers
// ============================================================================

// tierRank returns a numeric rank for comparison (higher = more trusted).
func tierRank(t trust.TrustTier) int {
	switch t {
	case trust.TierProvisional:
		return 0
	case trust.TierObserved:
		return 1
	case trust.TierTrusted:
		return 2
	case trust.TierVeteran:
		return 3
	default:
		return -1
	}
}

// mapKeys extracts keys from a map as a slice for display.
func mapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
