package dispatcher

import (
	"fmt"
	"sort"
)

// AssignAgent matches a task to the best-fit agent based on capability and
// current load. The selection strategy:
//   1. Filter agents whose Capability matches the task description.
//   2. Among matches, pick the one with the lowest current load.
//   3. If no capability match, pick the least-loaded agent overall.
//
// Returns a DispatchResult with the assigned WorkItem or an error.
func AssignAgent(task Task, agents []AgentProfile) (*DispatchResult, error) {
	if len(agents) == 0 {
		return nil, fmt.Errorf("%w: cannot assign task %s", ErrNoAgents, task.ID)
	}

	// Filter to agents that can accept load.
	var available []AgentProfile
	for _, a := range agents {
		if a.CanAcceptLoad() {
			available = append(available, a)
		}
	}
	if len(available) == 0 {
		return nil, fmt.Errorf("%w: task %s", ErrAgentOverloaded, task.ID)
	}

	// Score: prefer capability match, then lowest load.
	taskLower := task.Description
	type scored struct {
		agent AgentProfile
		score int // lower is better: 0=cap match, 1=no match; tiebreak by load
	}
	var candidates []scored
	for _, a := range available {
		s := scored{agent: a, score: 1}
		// Simple capability matching: check if the agent's capability
		// keyword appears in the task description (case-insensitive).
		if containsFold(taskLower, a.Capability) {
			s.score = 0
		}
		candidates = append(candidates, s)
	}

	// Sort: capability match first, then by current load ascending.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score < candidates[j].score
		}
		return candidates[i].agent.CurrentLoad < candidates[j].agent.CurrentLoad
	})

	best := candidates[0].agent
	steps, _ := DecomposeTask(task.Description)

	result := &DispatchResult{
		WorkItem: WorkItem{
			Task:  task,
			Agent: best,
			Steps: steps,
		},
	}
	return result, nil
}

// Dispatch assigns all tasks to agents, respecting load limits. Tasks are
// processed in priority order (lowest Priority number first). Each task is
// assigned independently; if one assignment fails its error is captured in the
// DispatchResult rather than aborting the batch.
func (d *Dispatcher) Dispatch(tasks []Task, agents []AgentProfile) ([]DispatchResult, error) {
	if len(agents) == 0 {
		return nil, ErrNoAgents
	}

	// Sort tasks by priority (ascending: 1 is highest).
	sorted := make([]Task, len(tasks))
	copy(sorted, tasks)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

	// Track load changes as we assign.
	agentLoad := make(map[string]int)
	for _, a := range agents {
		agentLoad[a.Name] = a.CurrentLoad
	}

	results := make([]DispatchResult, 0, len(sorted))
	for _, task := range sorted {
		// Build a view of agents with updated loads.
		agentsView := make([]AgentProfile, len(agents))
		for i, a := range agents {
			agentsView[i] = a
			agentsView[i].CurrentLoad = agentLoad[a.Name]
		}

		result, err := AssignAgent(task, agentsView)
		if err != nil {
			results = append(results, DispatchResult{
				Error: err.Error(),
			})
			continue
		}

		// Update tracked load.
		agentLoad[result.WorkItem.Agent.Name]++
		results = append(results, *result)
	}

	return results, nil
}

// containsFold is a case-insensitive substring check (strings equivalent of
// strings.Contains(strings.ToLower(s), strings.ToLower(sub)) without allocating
// full lowercased copies).
func containsFold(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && containsSubstringFold(s, sub)
}

func containsSubstringFold(s, sub string) bool {
	subLower := toLower(sub)
	for i := 0; i+len(sub) <= len(s); i++ {
		if toLower(s[i:i+len(sub)]) == subLower {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c = c + 32
		}
		b[i] = c
	}
	return string(b)
}
