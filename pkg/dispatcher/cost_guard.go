package dispatcher

import (
	"fmt"

	"github.com/totalwindupflightsystems/helix/pkg/estimate"
	"github.com/totalwindupflightsystems/helix/pkg/identity"
	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

// CostGuardDecision enumerates the possible outcomes of a pre-dispatch cost check.
type CostGuardDecision string

const (
	CostGuardApproved  CostGuardDecision = "APPROVED"
	CostGuardBlocked   CostGuardDecision = "BLOCKED"
	CostGuardEscalated CostGuardDecision = "ESCALATED"
)

// CostGuard runs before dispatching a work item. It queries the agent's trust
// tier, looks up the tier-specific cost cap, pre-flights the token cost, and
// blocks/escalates based on the result.
//
// Spec: specs/trust-model.md §Integration Points:
// "Estimator: Cost caps enforced at job dispatch based on current tier"
//
// Tier-specific cost caps (from specs/trust-model.md §Trust Tiers):
//   - Provisional: $5/job
//   - Observed: $25/job
//   - Trusted: $100/job
//   - Veteran: unlimited (monitoring only)
type CostGuard struct {
	permExp   *identity.PermissionExpansion
	estimator *estimate.Estimator
}

// NewCostGuard creates a CostGuard.
func NewCostGuard(permExp *identity.PermissionExpansion, est *estimate.Estimator) *CostGuard {
	if permExp == nil {
		permExp = identity.NewPermissionExpansion()
	}
	return &CostGuard{
		permExp:   permExp,
		estimator: est,
	}
}

// CostGuardResult is the outcome of a pre-dispatch cost check.
type CostGuardResult struct {
	Decision      CostGuardDecision `json:"decision"`
	AgentID       string            `json:"agent_id"`
	Tier          trust.TrustTier   `json:"tier"`
	CostCapPerJob float64           `json:"cost_cap_per_job"`
	EstimatedCost float64           `json:"estimated_cost"`
	Reason        string            `json:"reason"`
}

// Check runs the cost guard for a given agent and task description.
// It returns APPROVED if the estimated cost is within the tier cap,
// BLOCKED if it exceeds the cap, or ESCALATED for special cases.
func (cg *CostGuard) Check(agentID string, tier trust.TrustTier, task estimate.TaskDesc) (CostGuardResult, error) {
	// Get the tier-specific cost cap.
	cap, err := cg.permExp.CostCapForTier(tier)
	if err != nil {
		return CostGuardResult{}, fmt.Errorf("get cost cap for tier %s: %w", tier, err)
	}

	result := CostGuardResult{
		AgentID:       agentID,
		Tier:          tier,
		CostCapPerJob: cap,
	}

	// Estimate cost if an estimator is available.
	if cg.estimator != nil {
		est, estErr := cg.estimator.Estimate(task)
		if estErr != nil {
			// Estimation failed — escalate rather than block.
			result.Decision = CostGuardEscalated
			result.Reason = fmt.Sprintf("estimation failed: %v", estErr)
			return result, nil
		}
		result.EstimatedCost = est.CostTotal

		return cg.evaluateCap(result, cap, est.CostTotal, tier), nil
	}

	// No estimator — use rough heuristic from task desc fields.
	totalTokens := int64(task.FilesChanged*500 + task.MaxIterations*200 + task.DiffLines*10)
	if cap < 0 {
		result.Decision = CostGuardApproved
		result.Reason = fmt.Sprintf("tier %s unlimited cap, ~%d tokens estimated", tier, totalTokens)
		return result, nil
	}

	roughCost := float64(totalTokens) / 1000.0 * 0.001
	result.EstimatedCost = roughCost

	return cg.evaluateCap(result, cap, roughCost, tier), nil
}

// CheckWithEstimate runs the cost guard using a pre-computed CostEstimate.
func (cg *CostGuard) CheckWithEstimate(agentID string, tier trust.TrustTier, est estimate.CostEstimate) (CostGuardResult, error) {
	cap, err := cg.permExp.CostCapForTier(tier)
	if err != nil {
		return CostGuardResult{}, fmt.Errorf("get cost cap for tier %s: %w", tier, err)
	}

	result := CostGuardResult{
		AgentID:       agentID,
		Tier:          tier,
		CostCapPerJob: cap,
		EstimatedCost: est.CostTotal,
	}

	return cg.evaluateCap(result, cap, est.CostTotal, tier), nil
}

// evaluateCap checks the estimated cost against the tier cap and sets the
// decision + reason on the result.
func (cg *CostGuard) evaluateCap(result CostGuardResult, cap, cost float64, tier trust.TrustTier) CostGuardResult {
	// Unlimited cap (Veteran tier = -1).
	if cap < 0 {
		result.Decision = CostGuardApproved
		result.Reason = fmt.Sprintf("tier %s has unlimited cost cap (monitoring only)", tier)
		return result
	}

	if cost > cap {
		result.Decision = CostGuardBlocked
		result.Reason = fmt.Sprintf("estimated cost $%.2f exceeds tier %s cap $%.2f",
			cost, tier, cap)
		return result
	}

	// Warn zone: within 80% of cap.
	if cost > cap*0.8 {
		result.Decision = CostGuardApproved
		result.Reason = fmt.Sprintf("cost $%.2f within 80%% of cap $%.2f (approaching limit)",
			cost, cap)
		return result
	}

	result.Decision = CostGuardApproved
	result.Reason = fmt.Sprintf("cost $%.2f within tier %s cap $%.2f",
		cost, tier, cap)
	return result
}

// IsApproved returns true if the decision allows the task to proceed.
func (r CostGuardResult) IsApproved() bool {
	return r.Decision == CostGuardApproved
}

// IsBlocked returns true if the task is blocked by the cost guard.
func (r CostGuardResult) IsBlocked() bool {
	return r.Decision == CostGuardBlocked
}

// IsEscalated returns true if the task requires human escalation.
func (r CostGuardResult) IsEscalated() bool {
	return r.Decision == CostGuardEscalated
}
