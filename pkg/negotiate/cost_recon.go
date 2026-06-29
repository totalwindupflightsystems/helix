package negotiate

import (
	"fmt"
	"sort"
)

// ---------------------------------------------------------------------------
// Cost reconciliation (spec §9.3)
//
// The CostReconciler tracks debate costs across rounds, splits tie-break costs
// evenly between the two disagreeing agents, checks each agent's remaining
// weekly budget, and flags overruns. When either agent cannot absorb the
// tie-break share, the negotiation is escalated to human review (spec §9.3,
// spec §14 exit code 3).
// ---------------------------------------------------------------------------

// AgentBudget holds the per-agent budget context for cost reconciliation.
// It mirrors the budget-relevant fields from pkg/estimate.BudgetInfo without
// creating a cross-package dependency.
type AgentBudget struct {
	AgentName    string  `json:"agent_name"`
	BudgetWeekly float64 `json:"budget_weekly"` // weekly cap in USD
	BudgetUsed   float64 `json:"budget_used"`   // current period spend in USD
}

// RemainingBudget returns the dollars left in the current weekly period.
// Clamped at zero so a negative remaining never happens.
func (b AgentBudget) RemainingBudget() float64 {
	r := b.BudgetWeekly - b.BudgetUsed
	if r < 0 {
		return 0
	}
	return r
}

// RoundCost tracks the cost contribution of a single debate round for one
// agent. Debate round costs are the LLM API cost for generating the agent's
// argument in that round.
type RoundCost struct {
	Round  int     `json:"round"`
	Agent  string  `json:"agent"`
	Cost   float64 `json:"cost"`
	Reason string  `json:"reason"`
}

// AgentCostBreakdown is the per-agent view of accumulated costs.
type AgentCostBreakdown struct {
	Agent         string  `json:"agent"`
	DebateTotal   float64 `json:"debate_total"`
	TieBreakShare float64 `json:"tie_break_share"`
	Total         float64 `json:"total"`
	BudgetWeekly  float64 `json:"budget_weekly"`
	BudgetUsed    float64 `json:"budget_used"`
	Remaining     float64 `json:"remaining"`
	Overrun       bool    `json:"overrun"`
}

// CostReport is the final per-agent cost breakdown after a negotiation
// completes (or escalates).
type CostReport struct {
	AgentShares   map[string]float64     `json:"agent_shares"`
	TieBreakCost  float64                `json:"tie_break_cost"`
	DebateCosts   []RoundCost            `json:"debate_costs"`
	TotalCost     float64                `json:"total_cost"`
	Breakdowns    map[string]AgentCostBreakdown `json:"breakdowns"`
	OverrunAgents []string               `json:"overrun_agents"`
	Escalated     bool                   `json:"escalated"`
	Reason        string                 `json:"reason,omitempty"`
}

// CostReconciler tracks debate costs across rounds, splits tie-break costs
// between disagreeing agents, checks against weekly budgets, and flags cost
// overruns per spec §9.3.
//
// Usage:
//
//	cr := NewCostReconciler([]AgentBudget{...})
//	cr.RecordRoundCost(1, "agent-a", 0.002, "round 1 argument")
//	cr.RecordRoundCost(1, "agent-b", 0.0015, "round 1 rebuttal")
//	// ... more rounds ...
//	report := cr.ApplyTieBreakCost(chimeraCost, "agent-a", "agent-b")
type CostReconciler struct {
	budgets     map[string]AgentBudget
	roundCosts  []RoundCost
	agentTotals map[string]float64 // debate costs only (no tie-break yet)
	tieBreakCost float64
}

// NewCostReconciler creates a CostReconciler from a list of agent budgets.
func NewCostReconciler(budgets []AgentBudget) *CostReconciler {
	bm := make(map[string]AgentBudget, len(budgets))
	for _, b := range budgets {
		bm[b.AgentName] = b
	}
	return &CostReconciler{
		budgets:     bm,
		agentTotals: make(map[string]float64),
	}
}

// RecordRoundCost adds the cost of one agent's contribution to a debate round.
func (cr *CostReconciler) RecordRoundCost(round int, agent string, cost float64, reason string) {
	rc := RoundCost{
		Round:  round,
		Agent:  agent,
		Cost:   cost,
		Reason: reason,
	}
	cr.roundCosts = append(cr.roundCosts, rc)
	cr.agentTotals[agent] += cost
}

// RecordTieBreakCost stores the total Chimera tie-break cost without
// distributing it. Use ApplyTieBreakCost to distribute and finalize.
func (cr *CostReconciler) RecordTieBreakCost(cost float64) {
	cr.tieBreakCost = cost
}

// TieBreakShares splits the tie-break cost evenly between two agents per
// spec §9.3. This is functionally identical to SplitCost but operates on the
// stored tie-break cost.
func (cr *CostReconciler) TieBreakShares(agentA, agentB string) (float64, float64) {
	return SplitCost(cr.tieBreakCost)
}

// CheckBudget verifies that both agents have sufficient remaining budget to
// absorb their tie-break share. Returns the list of agents with insufficient
// budget (empty if all OK).
func (cr *CostReconciler) CheckBudget(agentA, agentB string) []string {
	shareA, shareB := cr.TieBreakShares(agentA, agentB)

	var overruns []string

	if budget, ok := cr.budgets[agentA]; ok {
		totalA := cr.agentTotals[agentA] + shareA
		if budget.BudgetWeekly > 0 && totalA > budget.RemainingBudget() {
			overruns = append(overruns, agentA)
		}
	}

	if budget, ok := cr.budgets[agentB]; ok {
		totalB := cr.agentTotals[agentB] + shareB
		if budget.BudgetWeekly > 0 && totalB > budget.RemainingBudget() {
			overruns = append(overruns, agentB)
		}
	}

	return overruns
}

// ApplyTieBreakCost splits the given tie-break cost between two agents,
// checks budgets, and produces a final CostReport. If either agent has
// insufficient budget, the report is flagged as escalated (spec §9.3, spec
// §14 exit code 3).
func (cr *CostReconciler) ApplyTieBreakCost(cost float64, agentA, agentB string) CostReport {
	cr.tieBreakCost = cost
	shareA, shareB := SplitCost(cost)

	overruns := cr.CheckBudget(agentA, agentB)
	escalated := len(overruns) > 0

	breakdowns := make(map[string]AgentCostBreakdown)
	agentShares := make(map[string]float64)

	for _, agent := range []string{agentA, agentB} {
		debateTotal := cr.agentTotals[agent]
		var share float64
		if agent == agentA {
			share = shareA
		} else {
			share = shareB
		}
		total := debateTotal + share

		budget, hasBudget := cr.budgets[agent]
		overrun := false
		if hasBudget && budget.BudgetWeekly > 0 && total > budget.RemainingBudget() {
			overrun = true
		}

		bd := AgentCostBreakdown{
			Agent:         agent,
			DebateTotal:   roundTo2(debateTotal),
			TieBreakShare: roundTo2(share),
			Total:         roundTo2(total),
			Overrun:       overrun,
		}
		if hasBudget {
			bd.BudgetWeekly = budget.BudgetWeekly
			bd.BudgetUsed = budget.BudgetUsed
			bd.Remaining = budget.RemainingBudget()
		}
		breakdowns[agent] = bd
		agentShares[agent] = roundTo2(total)
	}

	totalCost := cr.totalDebateCost() + cost

	report := CostReport{
		AgentShares:   agentShares,
		TieBreakCost:  roundTo2(cost),
		DebateCosts:   cr.roundCosts,
		TotalCost:     roundTo2(totalCost),
		Breakdowns:    breakdowns,
		OverrunAgents: overruns,
		Escalated:     escalated,
	}

	if escalated {
		sort.Strings(overruns)
		report.Reason = fmt.Sprintf(
			"BUDGET_EXHAUSTED: agents=%s tiebreak_cost=$%.4f debate_cost=$%.4f",
			joinStrings(overruns, ", "),
			cost,
			cr.totalDebateCost(),
		)
	}

	return report
}

// Finalize produces a CostReport without applying a tie-break cost (e.g.,
// negotiation resolved via concession without Chimera). Only debate round
// costs are included.
func (cr *CostReconciler) Finalize() CostReport {
	breakdowns := make(map[string]AgentCostBreakdown)
	agentShares := make(map[string]float64)

	for agent, debateTotal := range cr.agentTotals {
		budget, hasBudget := cr.budgets[agent]
		bd := AgentCostBreakdown{
			Agent:       agent,
			DebateTotal: roundTo2(debateTotal),
			TieBreakShare: 0,
			Total:       roundTo2(debateTotal),
		}
		if hasBudget {
			bd.BudgetWeekly = budget.BudgetWeekly
			bd.BudgetUsed = budget.BudgetUsed
			bd.Remaining = budget.RemainingBudget()
			if budget.BudgetWeekly > 0 && debateTotal > budget.RemainingBudget() {
				bd.Overrun = true
			}
		}
		breakdowns[agent] = bd
		agentShares[agent] = roundTo2(debateTotal)
	}

	return CostReport{
		AgentShares:  agentShares,
		TieBreakCost: 0,
		DebateCosts:  cr.roundCosts,
		TotalCost:    roundTo2(cr.totalDebateCost()),
		Breakdowns:   breakdowns,
	}
}

// DebateCostForAgent returns the total debate cost accumulated for one agent.
func (cr *CostReconciler) DebateCostForAgent(agent string) float64 {
	return cr.agentTotals[agent]
}

// TotalDebateCost returns the sum of all recorded round costs.
func (cr *CostReconciler) TotalDebateCost() float64 {
	return cr.totalDebateCost()
}

// --- internal helpers ---

func (cr *CostReconciler) totalDebateCost() float64 {
	var total float64
	for _, rc := range cr.roundCosts {
		total += rc.Cost
	}
	return total
}

// roundTo2 rounds a float64 to 2 decimal places for currency display.
func roundTo2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

// joinStrings joins a slice of strings with a separator.
func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += sep + p
	}
	return result
}
