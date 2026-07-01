package estimate

import (
	"fmt"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Approval Gate Engine (spec §8.1)
// ---------------------------------------------------------------------------

// BlockedOption represents one of the three remediation paths available to an
// agent whose task was BLOCKED by the budget gate (spec §8.1 BLOCKED section).
type BlockedOption struct {
	Type         string  `json:"type"`                    // "wait" | "increase" | "cheaper_model"
	Description  string  `json:"description"`             // human-readable explanation
	CheaperModel string  `json:"cheaper_model,omitempty"` // model ID for "cheaper_model" option
	CheaperCost  float64 `json:"cheaper_cost,omitempty"`  // estimated cost with the cheaper model
}

// GateApprovalResult is the enriched output of ApprovalGate.Evaluate. It wraps
// the existing ApprovalDecision with post-execution budget projection, blocked
// options, and suggested cheaper models.
type GateApprovalResult struct {
	Decision            ApprovalDecision `json:"decision"`
	Status              ApprovalStatus   `json:"status"`
	Approved            bool             `json:"approved"`
	Reason              string           `json:"reason"`
	RemainingAfter      float64          `json:"remaining_after"`  // budget remaining after task cost
	RemainingBefore     float64          `json:"remaining_before"` // budget remaining before task cost
	WeeklyCap           float64          `json:"weekly_cap"`
	TaskCost            float64          `json:"task_cost"`
	BlockedOptions      []BlockedOption  `json:"blocked_options,omitempty"`
	CheaperAlternatives []CheaperModel   `json:"cheaper_alternatives,omitempty"`
}

// CheaperModel is a model that costs less than the originally estimated model
// for the same task. Used to populate BLOCKED options and suggestions.
type CheaperModel struct {
	Model    string  `json:"model"`
	Provider string  `json:"provider"`
	Cost     float64 `json:"cost"`
	Savings  float64 `json:"savings"` // savings vs original model
}

// ApprovalGate is the spec §8.1 approval gate engine. It evaluates estimated
// cost against remaining budget and returns a structured decision with
// remaining-after projection and remediation options.
//
// The gate can optionally hold a *PricingYAML reference to suggest cheaper
// model alternatives when a task is blocked. If pricing is nil, cheaper-model
// suggestions are omitted.
type ApprovalGate struct {
	pricing *PricingYAML
}

// NewApprovalGate creates an ApprovalGate. pricing may be nil — in that case
// cheaper-model suggestions are not generated.
func NewApprovalGate(pricing *PricingYAML) *ApprovalGate {
	return &ApprovalGate{pricing: pricing}
}

// Evaluate runs the full spec §8.1 approval gate logic against a budget and
// estimated cost, returning a rich GateApprovalResult.
//
// Logic order (spec §8.1):
//  1. cost > weekly cap → ESCALATED (human approval required)
//  2. cost ≤ remaining → AUTO_APPROVED
//  3. cost ≤ remaining × 1.5 AND trust ≥ 70 → AUTO_APPROVED_WITH_WARNING
//  4. cost > remaining → BLOCKED (with 3 options)
func (g *ApprovalGate) Evaluate(budget BudgetInfo, estimate CostEstimate) GateApprovalResult {
	cost := estimate.CostTotal
	remaining := RemainingBudget(budget)
	remainingAfter := remaining - cost
	if remainingAfter < 0 {
		remainingAfter = 0
	}

	base := GateApprovalResult{
		WeeklyCap:       budget.BudgetWeekly,
		TaskCost:        cost,
		RemainingBefore: remaining,
		RemainingAfter:  remainingAfter,
	}

	// Use existing CheckBudget for the core decision logic (DRY).
	dec := CheckBudget(budget, estimate)
	base.Decision = dec
	base.Status = dec.Status
	base.Approved = dec.Approved
	base.Reason = dec.Reason

	// Enrich BLOCKED results with remediation options.
	if dec.Status == StatusBlocked {
		base.BlockedOptions = g.generateBlockedOptions(budget, estimate, cost, remaining)
	}

	// For ESCALATED results, also suggest cheaper models if available.
	if dec.Status == StatusEscalated {
		cheaper := g.findCheaperModels(estimate, budget)
		if len(cheaper) > 0 {
			base.CheaperAlternatives = cheaper
		}
	}

	return base
}

// EvaluateWithTrust is a convenience wrapper that sets trust level before
// evaluating. Useful when trust comes from a separate lookup.
func (g *ApprovalGate) EvaluateWithTrust(budget BudgetInfo, estimate CostEstimate, trustLevel int) GateApprovalResult {
	budget.TrustLevel = trustLevel
	return g.Evaluate(budget, estimate)
}

// generateBlockedOptions creates the three spec §8.1 BLOCKED remediation paths.
func (g *ApprovalGate) generateBlockedOptions(budget BudgetInfo, estimate CostEstimate, cost, remaining float64) []BlockedOption {
	options := []BlockedOption{
		{
			Type:        "wait",
			Description: fmt.Sprintf("Wait for budget reset (Sunday 00:00 UTC). %.2f will become available.", budget.BudgetWeekly),
		},
		{
			Type:        "increase",
			Description: fmt.Sprintf("Request human budget increase. Current weekly cap: $%.2f.", budget.BudgetWeekly),
		},
	}

	// Add cheaper model option if pricing data is available and a cheaper model exists.
	if g.pricing != nil {
		cheaper := g.findCheaperModels(estimate, budget)
		if len(cheaper) > 0 {
			best := cheaper[0] // already sorted by cost ascending
			options = append(options, BlockedOption{
				Type:         "cheaper_model",
				Description:  fmt.Sprintf("Switch to %s (%s) — estimated cost $%.4f (saves $%.4f).", best.Model, best.Provider, best.Cost, best.Savings),
				CheaperModel: best.Model,
				CheaperCost:  best.Cost,
			})
		}
	}

	return options
}

// findCheaperModels scans the pricing data for models that would cost less than
// the originally estimated model. Returns up to 5 alternatives sorted by cost
// ascending. Returns empty if pricing is nil or no cheaper model is found.
func (g *ApprovalGate) findCheaperModels(estimate CostEstimate, budget BudgetInfo) []CheaperModel {
	if g.pricing == nil || g.pricing.Providers == nil {
		return nil
	}

	originalCost := estimate.CostTotal
	if originalCost <= 0 {
		return nil
	}

	var alternatives []CheaperModel

	for providerName, providerPricing := range g.pricing.Providers {
		for modelName, modelPrice := range providerPricing.Models {
			// Skip the same model that was already estimated.
			if modelName == estimate.Model && providerName == estimate.Provider {
				continue
			}

			cheaperCost := g.estimateCheaperCost(estimate, modelPrice, providerPricing, budget)
			if cheaperCost > 0 && cheaperCost < originalCost {
				alternatives = append(alternatives, CheaperModel{
					Model:    modelName,
					Provider: providerName,
					Cost:     cheaperCost,
					Savings:  originalCost - cheaperCost,
				})
			}
		}
	}

	// Sort by cost ascending (cheapest first).
	sort.Slice(alternatives, func(i, j int) bool {
		return alternatives[i].Cost < alternatives[j].Cost
	})

	// Limit to top 5.
	if len(alternatives) > 5 {
		alternatives = alternatives[:5]
	}

	return alternatives
}

// estimateCheaperCost computes a rough cost for the same task using a different
// model's pricing. It preserves the token count from the original estimate but
// recalculates cost with the new model's per-1K prices.
func (g *ApprovalGate) estimateCheaperCost(estimate CostEstimate, modelPrice ModelPrice, providerPricing ProviderPricing, budget BudgetInfo) float64 {
	tokens := estimate.Tokens

	// Determine per-token prices.
	inputPerToken := modelPrice.InputPer1K / 1000.0
	outputPerToken := modelPrice.OutputPer1K / 1000.0
	cacheReadPerToken := modelPrice.GetCacheReadPrice() / 1000.0
	cacheWritePerToken := modelPrice.GetCacheWritePrice() / 1000.0

	// Apply provider markup if present (e.g., OpenRouter 5%).
	markup := 1.0 + providerPricing.MarkupPercent/100.0

	freshInputCost := float64(tokens.FreshInput) * inputPerToken
	cacheHitCost := float64(tokens.CacheHits) * cacheReadPerToken
	cacheWriteCost := float64(tokens.CacheWrites) * cacheWritePerToken
	outputCost := float64(tokens.Output) * outputPerToken

	total := (freshInputCost + cacheHitCost + cacheWriteCost + outputCost) * markup

	return total
}

// FormatGateResult renders a GateApprovalResult as a human-readable string
// suitable for CLI output.
func FormatGateResult(r GateApprovalResult) string {
	var sb strings.Builder

	statusEmoji := map[ApprovalStatus]string{
		StatusAutoApproved:            "✓",
		StatusAutoApprovedWithWarning: "⚠",
		StatusBlocked:                 "✗",
		StatusEscalated:               "⚡",
	}

	emoji := statusEmoji[r.Status]
	if emoji == "" {
		emoji = "?"
	}

	sb.WriteString(fmt.Sprintf("%s %s\n", emoji, r.Status))
	sb.WriteString(fmt.Sprintf("  %s\n", r.Reason))
	sb.WriteString(fmt.Sprintf("  Budget: $%.2f remaining (of $%.2f weekly)\n", r.RemainingBefore, r.WeeklyCap))
	sb.WriteString(fmt.Sprintf("  Cost:   $%.4f\n", r.TaskCost))
	sb.WriteString(fmt.Sprintf("  After:  $%.2f remaining\n", r.RemainingAfter))

	if len(r.BlockedOptions) > 0 {
		sb.WriteString("\n  Options:\n")
		for _, opt := range r.BlockedOptions {
			sb.WriteString(fmt.Sprintf("    [%s] %s\n", opt.Type, opt.Description))
		}
	}

	if len(r.CheaperAlternatives) > 0 {
		sb.WriteString("\n  Cheaper alternatives:\n")
		for _, alt := range r.CheaperAlternatives {
			sb.WriteString(fmt.Sprintf("    %s (%s): $%.4f (saves $%.4f)\n", alt.Model, alt.Provider, alt.Cost, alt.Savings))
		}
	}

	return sb.String()
}

// BatchEvaluate runs the approval gate against multiple agents for the same
// task. Returns per-agent results keyed by agent name. Useful for multi-agent
// dispatch scenarios where each agent has their own budget.
func (g *ApprovalGate) BatchEvaluate(budgets []BudgetInfo, estimate CostEstimate) map[string]GateApprovalResult {
	results := make(map[string]GateApprovalResult, len(budgets))
	for _, b := range budgets {
		results[b.AgentName] = g.Evaluate(b, estimate)
	}
	return results
}

// AnyApproved returns true if at least one agent in the batch result was
// approved (AUTO_APPROVED or AUTO_APPROVED_WITH_WARNING).
func AnyApproved(results map[string]GateApprovalResult) bool {
	for _, r := range results {
		if r.Approved {
			return true
		}
	}
	return false
}

// AllBlocked returns true if every agent in the batch result was BLOCKED.
func AllBlocked(results map[string]GateApprovalResult) bool {
	if len(results) == 0 {
		return false
	}
	for _, r := range results {
		if r.Status != StatusBlocked {
			return false
		}
	}
	return true
}
