package estimate

import (
	"fmt"
	"sync"
)

// Cost attribution model per spec §8.3.
// Every token burned in Helix is attributed to namespace, agent, task, prompt_version, model.

// CostAttribute identifies the source of an LLM cost.
type CostAttribute struct {
	Namespace     string // <repo-owner>/<repo-name>
	Agent         string // <agent-uuid>
	Task          string // <pr-number or task-id>
	PromptVersion string // <prompt-file sha256>
	Model         string // <model-name>
}

// CostHierarchyTier enumerates the 4-level cost hierarchy from spec §8.3.
type CostHierarchyTier int

const (
	// TierAgent is per-agent budget (H4F enforces).
	TierAgent CostHierarchyTier = iota
	// TierRepo is per-repo budget (Hivemind enforces).
	TierRepo
	// TierSprint is per-sprint budget (Axiom enforces).
	TierSprint
	// TierPlatform is platform-wide cap (H4F global limit).
	TierPlatform
)

// BudgetExhaustionLevel represents spec §8.3 budget exhaustion behavior.
type BudgetExhaustionLevel int

const (
	// ExhaustionNone — no budget exhausted.
	ExhaustionNone BudgetExhaustionLevel = iota
	// ExhaustionAgent — agent hit per-agent limit.
	ExhaustionAgent
	// ExhaustionRepo — repo hit per-repo limit.
	ExhaustionRepo
	// ExhaustionPlatform — platform cap hit.
	ExhaustionPlatform
)

// CostEntry is a single attributed cost record.
type CostEntry struct {
	Attribute CostAttribute
	CostUSD   float64
	TokensIn  int64
	TokensOut int64
	Timestamp string // ISO 8601
}

// CostAttributionModel tracks costs across the 4-level hierarchy per spec §8.3.
type CostAttributionModel struct {
	mu sync.RWMutex
	// Per-level cost accumulators
	agentCosts    map[string]float64 // key: agent UUID
	repoCosts     map[string]float64 // key: namespace
	sprintCosts   map[string]float64 // key: sprint ID
	platformTotal float64
	// Budget limits
	agentLimits   map[string]float64
	repoLimits    map[string]float64
	sprintLimits  map[string]float64
	platformLimit float64
	// Entries log
	entries []CostEntry
}

// NewCostAttributionModel creates a new empty cost attribution model.
func NewCostAttributionModel() *CostAttributionModel {
	return &CostAttributionModel{
		agentCosts:   make(map[string]float64),
		repoCosts:    make(map[string]float64),
		sprintCosts:  make(map[string]float64),
		agentLimits:  make(map[string]float64),
		repoLimits:   make(map[string]float64),
		sprintLimits: make(map[string]float64),
	}
}

// SetAgentBudget sets the per-agent budget limit.
func (m *CostAttributionModel) SetAgentBudget(agent string, limit float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.agentLimits[agent] = limit
}

// SetRepoBudget sets the per-repo (namespace) budget limit.
func (m *CostAttributionModel) SetRepoBudget(namespace string, limit float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.repoLimits[namespace] = limit
}

// SetSprintBudget sets the per-sprint budget limit.
func (m *CostAttributionModel) SetSprintBudget(sprintID string, limit float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sprintLimits[sprintID] = limit
}

// SetPlatformBudget sets the platform-wide budget limit.
func (m *CostAttributionModel) SetPlatformBudget(limit float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.platformLimit = limit
}

// RecordCost records a cost entry and updates all hierarchy levels.
func (m *CostAttributionModel) RecordCost(entry CostEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, entry)
	m.agentCosts[entry.Attribute.Agent] += entry.CostUSD
	m.repoCosts[entry.Attribute.Namespace] += entry.CostUSD
	m.platformTotal += entry.CostUSD
}

// RecordCostWithSprint records a cost entry with sprint attribution.
func (m *CostAttributionModel) RecordCostWithSprint(entry CostEntry, sprintID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, entry)
	m.agentCosts[entry.Attribute.Agent] += entry.CostUSD
	m.repoCosts[entry.Attribute.Namespace] += entry.CostUSD
	m.sprintCosts[sprintID] += entry.CostUSD
	m.platformTotal += entry.CostUSD
}

// AgentCost returns the total cost attributed to an agent.
func (m *CostAttributionModel) AgentCost(agent string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.agentCosts[agent]
}

// RepoCost returns the total cost attributed to a namespace.
func (m *CostAttributionModel) RepoCost(namespace string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.repoCosts[namespace]
}

// SprintCost returns the total cost attributed to a sprint.
func (m *CostAttributionModel) SprintCost(sprintID string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sprintCosts[sprintID]
}

// PlatformCost returns the total platform-wide cost.
func (m *CostAttributionModel) PlatformCost() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.platformTotal
}

// AgentRemaining returns the remaining budget for an agent.
func (m *CostAttributionModel) AgentRemaining(agent string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	limit := m.agentLimits[agent]
	if limit == 0 {
		return -1 // no limit set
	}
	used := m.agentCosts[agent]
	return limit - used
}

// RepoRemaining returns the remaining budget for a namespace.
func (m *CostAttributionModel) RepoRemaining(namespace string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	limit := m.repoLimits[namespace]
	if limit == 0 {
		return -1
	}
	used := m.repoCosts[namespace]
	return limit - used
}

// CheckExhaustion returns the highest budget exhaustion level per spec §8.3.
// Priority: Platform > Repo > Agent (highest tier wins).
func (m *CostAttributionModel) CheckExhaustion(agent, namespace string) BudgetExhaustionLevel {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Platform cap
	if m.platformLimit > 0 && m.platformTotal >= m.platformLimit {
		return ExhaustionPlatform
	}
	// Repo limit
	if limit, ok := m.repoLimits[namespace]; ok && limit > 0 {
		if m.repoCosts[namespace] >= limit {
			return ExhaustionRepo
		}
	}
	// Agent limit
	if limit, ok := m.agentLimits[agent]; ok && limit > 0 {
		if m.agentCosts[agent] >= limit {
			return ExhaustionAgent
		}
	}
	return ExhaustionNone
}

// ExhaustionAction returns the spec §8.3 response for a budget exhaustion level.
func ExhaustionAction(level BudgetExhaustionLevel) string {
	switch level {
	case ExhaustionAgent:
		return "403 on next API call → agent halts → Hivemind notifies repo owner"
	case ExhaustionRepo:
		return "all agents for that repo paused → Axiom notifies all repo collaborators"
	case ExhaustionPlatform:
		return "H4F pauses all agents → Telegram alert to platform admin"
	default:
		return ""
	}
}

// CostReport summarizes costs across all hierarchy levels.
type CostReport struct {
	AgentCosts    map[string]float64
	RepoCosts     map[string]float64
	SprintCosts   map[string]float64
	PlatformTotal float64
	EntryCount    int
}

// Report generates a full cost report.
func (m *CostAttributionModel) Report() CostReport {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return CostReport{
		AgentCosts:    copyFloatMap(m.agentCosts),
		RepoCosts:     copyFloatMap(m.repoCosts),
		SprintCosts:   copyFloatMap(m.sprintCosts),
		PlatformTotal: m.platformTotal,
		EntryCount:    len(m.entries),
	}
}

// EntriesByAgent returns all cost entries for a specific agent.
func (m *CostAttributionModel) EntriesByAgent(agent string) []CostEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []CostEntry
	for _, e := range m.entries {
		if e.Attribute.Agent == agent {
			out = append(out, e)
		}
	}
	return out
}

// EntriesByRepo returns all cost entries for a specific namespace.
func (m *CostAttributionModel) EntriesByRepo(namespace string) []CostEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []CostEntry
	for _, e := range m.entries {
		if e.Attribute.Namespace == namespace {
			out = append(out, e)
		}
	}
	return out
}

// EntriesByModel returns all cost entries for a specific model.
func (m *CostAttributionModel) EntriesByModel(model string) []CostEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []CostEntry
	for _, e := range m.entries {
		if e.Attribute.Model == model {
			out = append(out, e)
		}
	}
	return out
}

// FormatCostReport renders a human-readable cost report.
func FormatCostReport(r CostReport) string {
	s := fmt.Sprintf("Cost Report — %d entries, platform total: $%.2f\n", r.EntryCount, r.PlatformTotal)
	s += "\nPer-Agent:\n"
	for agent, cost := range r.AgentCosts {
		s += fmt.Sprintf("  %s: $%.2f\n", agent, cost)
	}
	s += "\nPer-Repo:\n"
	for repo, cost := range r.RepoCosts {
		s += fmt.Sprintf("  %s: $%.2f\n", repo, cost)
	}
	if len(r.SprintCosts) > 0 {
		s += "\nPer-Sprint:\n"
		for sprint, cost := range r.SprintCosts {
			s += fmt.Sprintf("  %s: $%.2f\n", sprint, cost)
		}
	}
	return s
}

// Reset clears all accumulated costs and entries (keeps budget limits).
func (m *CostAttributionModel) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.agentCosts = make(map[string]float64)
	m.repoCosts = make(map[string]float64)
	m.sprintCosts = make(map[string]float64)
	m.platformTotal = 0
	m.entries = nil
}

func copyFloatMap(in map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
