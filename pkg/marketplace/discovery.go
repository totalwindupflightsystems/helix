package marketplace

import "sort"

// Discovery and load balancing for Axiom swarm assembly (spec §8).
//
// FindAgents is the Axiom integration interface: when Axiom decomposes a work
// item, it queries the marketplace to find agents matching the required
// capabilities, trust threshold, cost ceiling, and tier. Results are ranked
// for optimal assignment.

// FindAgents returns agents matching the Axiom search requirements, ranked by
// capability match percentage → trust score → cost (lower is better) per
// spec §8.2.
//
// Only active agents are returned. Deprecated and retired agents are excluded
// (deprecated agents are visible in the marketplace but not assignable — spec
// §10.1).
func (r *Registry) FindAgents(req SearchRequirements) ([]*Agent, error) {
	candidates, err := r.List(func(a *Agent) bool {
		// Only active agents are assignable (spec §10.1).
		if a.Status != StatusActive {
			return false
		}
		// Exclude list — agents already assigned to this work item.
		for _, ex := range req.ExcludeAgents {
			if a.Name == ex {
				return false
			}
		}
		// Required capabilities — agent MUST have ALL of them.
		for _, c := range req.RequiredCapabilities {
			if !hasCapability(a, c) {
				return false
			}
		}
		// Trust threshold.
		if req.MinTrust > 0 && a.TrustScore < req.MinTrust {
			return false
		}
		// Cost ceiling.
		if req.MaxCost > 0 && a.Budget.AverageTaskCost > req.MaxCost {
			return false
		}
		// Tier filter (empty = any tier).
		if req.Tier != "" && a.Tier != req.Tier {
			return false
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	// Rank: capability match % → trust (desc) → cost (asc).
	sort.SliceStable(candidates, func(i, j int) bool {
		pi := matchPercent(candidates[i], req)
		pj := matchPercent(candidates[j], req)
		if pi != pj {
			return pi > pj
		}
		if candidates[i].TrustScore != candidates[j].TrustScore {
			return candidates[i].TrustScore > candidates[j].TrustScore
		}
		return candidates[i].Budget.AverageTaskCost < candidates[j].Budget.AverageTaskCost
	})

	if req.Limit > 0 && len(candidates) > req.Limit {
		candidates = candidates[:req.Limit]
	}
	return candidates, nil
}

// LoadBalance reorders agents for assignment: fewer active tasks preferred,
// lower budget utilization preferred, ties preserved in input order (spec §8.3).
//
// The round-robin for ties is achieved by preserving the input order of
// equally-qualified agents (the caller can rotate the slice between rounds).
func LoadBalance(agents []*Agent, activeTaskCounts map[string]int) []*Agent {
	result := make([]*Agent, len(agents))
	copy(result, agents)
	sort.SliceStable(result, func(i, j int) bool {
		ci := activeTaskCounts[result[i].Name]
		cj := activeTaskCounts[result[j].Name]
		if ci != cj {
			return ci < cj // fewer active tasks first
		}
		return budgetUtilization(result[i]) < budgetUtilization(result[j])
	})
	return result
}

// matchPercent computes the percentage of required+preferred capabilities the
// agent possesses (spec §8.2 ranking criterion).
func matchPercent(a *Agent, req SearchRequirements) int {
	total := len(req.RequiredCapabilities) + len(req.PreferredCapabilities)
	if total == 0 {
		return 100
	}
	matched := 0
	for _, c := range req.RequiredCapabilities {
		if hasCapability(a, c) {
			matched++
		}
	}
	for _, c := range req.PreferredCapabilities {
		if hasCapability(a, c) {
			matched++
		}
	}
	return matched * 100 / total
}

// budgetUtilization returns a proxy for how much of the agent's weekly budget
// is consumed. Lower values mean more headroom (preferred for load balancing).
func budgetUtilization(a *Agent) float64 {
	if a.Budget.WeeklyLimit <= 0 {
		return 1.0 // exhausted — least preferred
	}
	ratio := a.Budget.AverageTaskCost / a.Budget.WeeklyLimit
	if ratio > 1.0 {
		return 1.0
	}
	return ratio
}
