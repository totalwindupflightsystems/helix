// Package health — agent_metrics.go
//
// Per-agent Prometheus metrics collector implementing all 6 agent metrics
// from specs/SPECIFICATION.md §8.4 (Agent metrics):
//
//	helix_agent_tasks_total{agent, repo, status="completed|failed|blocked"}
//	helix_agent_llm_calls_total{agent, model}
//	helix_agent_tokens_used{agent, model, type="prompt|completion"}
//	helix_agent_cost_total{agent, repo}
//	helix_agent_sandbox_uptime_seconds{agent}
//	helix_agent_worktree_count{agent}
//
// The collector tracks metrics per agent, per repo, and per model.
// Prometheus text exposition format with HELP/TYPE headers.
// Thread-safe via sync.RWMutex.
package health

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ============================================================================
// AgentTaskStatus
// ============================================================================

// AgentTaskStatus represents the outcome of an agent task.
type AgentTaskStatus string

const (
	TaskCompleted AgentTaskStatus = "completed"
	TaskFailed    AgentTaskStatus = "failed"
	TaskBlocked   AgentTaskStatus = "blocked"
)

// ============================================================================
// TokenType
// ============================================================================

// TokenType represents prompt vs completion tokens.
type TokenType string

const (
	TokenPrompt     TokenType = "prompt"
	TokenCompletion TokenType = "completion"
)

// ============================================================================
// AgentMetricsCollector
// ============================================================================

// AgentMetricsCollector tracks per-agent metrics for Prometheus export.
//
// All 6 spec §8.4 metrics:
//  1. helix_agent_tasks_total{agent, repo, status}
//  2. helix_agent_llm_calls_total{agent, model}
//  3. helix_agent_tokens_used{agent, model, type}
//  4. helix_agent_cost_total{agent, repo}
//  5. helix_agent_sandbox_uptime_seconds{agent}
//  6. helix_agent_worktree_count{agent}
type AgentMetricsCollector struct {
	mu sync.RWMutex

	// tasks maps agent→repo→status→count
	tasks map[string]map[string]map[AgentTaskStatus]int64

	// llmCalls maps agent→model→count
	llmCalls map[string]map[string]int64

	// tokens maps agent→model→type→count
	tokens map[string]map[string]map[TokenType]int64

	// costs maps agent→repo→total_cost_usd
	costs map[string]map[string]float64

	// uptime maps agent→uptime_seconds
	uptime map[string]float64

	// worktrees maps agent→count
	worktrees map[string]int64
}

// NewAgentMetricsCollector creates a new collector.
func NewAgentMetricsCollector() *AgentMetricsCollector {
	return &AgentMetricsCollector{
		tasks:     make(map[string]map[string]map[AgentTaskStatus]int64),
		llmCalls:  make(map[string]map[string]int64),
		tokens:    make(map[string]map[string]map[TokenType]int64),
		costs:     make(map[string]map[string]float64),
		uptime:    make(map[string]float64),
		worktrees: make(map[string]int64),
	}
}

// ============================================================================
// Recording Methods
// ============================================================================

// RecordTask records a task completion/failure/block for an agent.
func (c *AgentMetricsCollector) RecordTask(agent, repo string, status AgentTaskStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.tasks[agent] == nil {
		c.tasks[agent] = make(map[string]map[AgentTaskStatus]int64)
	}
	if c.tasks[agent][repo] == nil {
		c.tasks[agent][repo] = make(map[AgentTaskStatus]int64)
	}
	c.tasks[agent][repo][status]++
}

// RecordLLMCall records an LLM API call by an agent using a specific model.
func (c *AgentMetricsCollector) RecordLLMCall(agent, model string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.llmCalls[agent] == nil {
		c.llmCalls[agent] = make(map[string]int64)
	}
	c.llmCalls[agent][model]++
}

// RecordTokens records token usage by an agent for a specific model.
func (c *AgentMetricsCollector) RecordTokens(agent, model string, tokenType TokenType, count int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.tokens[agent] == nil {
		c.tokens[agent] = make(map[string]map[TokenType]int64)
	}
	if c.tokens[agent][model] == nil {
		c.tokens[agent][model] = make(map[TokenType]int64)
	}
	c.tokens[agent][model][tokenType] += count
}

// RecordCost records a cost (in USD) for an agent on a specific repo.
func (c *AgentMetricsCollector) RecordCost(agent, repo string, costUSD float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.costs[agent] == nil {
		c.costs[agent] = make(map[string]float64)
	}
	c.costs[agent][repo] += costUSD
}

// SetSandboxUptime sets the current sandbox uptime for an agent.
func (c *AgentMetricsCollector) SetSandboxUptime(agent string, seconds float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.uptime[agent] = seconds
}

// SetWorktreeCount sets the number of active worktrees for an agent.
func (c *AgentMetricsCollector) SetWorktreeCount(agent string, count int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.worktrees[agent] = count
}

// ============================================================================
// Query Methods
// ============================================================================

// GetTaskCount returns the total task count for an agent, repo, and status.
func (c *AgentMetricsCollector) GetTaskCount(agent, repo string, status AgentTaskStatus) int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.tasks[agent] == nil {
		return 0
	}
	if c.tasks[agent][repo] == nil {
		return 0
	}
	return c.tasks[agent][repo][status]
}

// GetLLMCallCount returns the total LLM call count for an agent and model.
func (c *AgentMetricsCollector) GetLLMCallCount(agent, model string) int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.llmCalls[agent] == nil {
		return 0
	}
	return c.llmCalls[agent][model]
}

// GetTokenCount returns the token count for an agent, model, and type.
func (c *AgentMetricsCollector) GetTokenCount(agent, model string, tokenType TokenType) int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.tokens[agent] == nil {
		return 0
	}
	if c.tokens[agent][model] == nil {
		return 0
	}
	return c.tokens[agent][model][tokenType]
}

// GetCost returns the total cost for an agent on a repo.
func (c *AgentMetricsCollector) GetCost(agent, repo string) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.costs[agent] == nil {
		return 0
	}
	return c.costs[agent][repo]
}

// GetSandboxUptime returns the current sandbox uptime for an agent.
func (c *AgentMetricsCollector) GetSandboxUptime(agent string) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.uptime[agent]
}

// GetWorktreeCount returns the worktree count for an agent.
func (c *AgentMetricsCollector) GetWorktreeCount(agent string) int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.worktrees[agent]
}

// ============================================================================
// Collect — Prometheus Text Exposition Format
// ============================================================================

// Collect returns all metrics in Prometheus text exposition format.
//
// Metric ordering (deterministic):
//  1. helix_agent_tasks_total (sorted by agent, repo, status)
//  2. helix_agent_llm_calls_total (sorted by agent, model)
//  3. helix_agent_tokens_used (sorted by agent, model, type)
//  4. helix_agent_cost_total (sorted by agent, repo)
//  5. helix_agent_sandbox_uptime_seconds (sorted by agent)
//  6. helix_agent_worktree_count (sorted by agent)
func (c *AgentMetricsCollector) Collect() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var sb strings.Builder

	// 1. helix_agent_tasks_total
	sb.WriteString("# HELP helix_agent_tasks_total Total tasks by agent, repo, and status.\n")
	sb.WriteString("# TYPE helix_agent_tasks_total counter\n")
	for _, agent := range sortedAgentKeys(c.tasks) {
		for _, repo := range sortedAgentKeys(c.tasks[agent]) {
			for _, status := range sortedStatusKeys(c.tasks[agent][repo]) {
				sb.WriteString(fmt.Sprintf("helix_agent_tasks_total{agent=%q,repo=%q,status=%q} %d\n",
					agent, repo, string(status), c.tasks[agent][repo][status]))
			}
		}
	}

	// 2. helix_agent_llm_calls_total
	sb.WriteString("# HELP helix_agent_llm_calls_total Total LLM API calls by agent and model.\n")
	sb.WriteString("# TYPE helix_agent_llm_calls_total counter\n")
	for _, agent := range sortedAgentKeys(c.llmCalls) {
		for _, model := range sortedAgentKeys(c.llmCalls[agent]) {
			sb.WriteString(fmt.Sprintf("helix_agent_llm_calls_total{agent=%q,model=%q} %d\n",
				agent, model, c.llmCalls[agent][model]))
		}
	}

	// 3. helix_agent_tokens_used
	sb.WriteString("# HELP helix_agent_tokens_used Token usage by agent, model, and type.\n")
	sb.WriteString("# TYPE helix_agent_tokens_used counter\n")
	for _, agent := range sortedAgentKeys(c.tokens) {
		for _, model := range sortedAgentKeys(c.tokens[agent]) {
			for _, tt := range sortedTokenKeys(c.tokens[agent][model]) {
				sb.WriteString(fmt.Sprintf("helix_agent_tokens_used{agent=%q,model=%q,type=%q} %d\n",
					agent, model, string(tt), c.tokens[agent][model][tt]))
			}
		}
	}

	// 4. helix_agent_cost_total
	sb.WriteString("# HELP helix_agent_cost_total Total cost in USD by agent and repo.\n")
	sb.WriteString("# TYPE helix_agent_cost_total counter\n")
	for _, agent := range sortedAgentKeys(c.costs) {
		for _, repo := range sortedAgentKeys(c.costs[agent]) {
			sb.WriteString(fmt.Sprintf("helix_agent_cost_total{agent=%q,repo=%q} %s\n",
				agent, repo, formatFloat(c.costs[agent][repo])))
		}
	}

	// 5. helix_agent_sandbox_uptime_seconds
	sb.WriteString("# HELP helix_agent_sandbox_uptime_seconds Current sandbox uptime in seconds.\n")
	sb.WriteString("# TYPE helix_agent_sandbox_uptime_seconds gauge\n")
	for _, agent := range sortedAgentKeys(c.uptime) {
		sb.WriteString(fmt.Sprintf("helix_agent_sandbox_uptime_seconds{agent=%q} %s\n",
			agent, formatFloat(c.uptime[agent])))
	}

	// 6. helix_agent_worktree_count
	sb.WriteString("# HELP helix_agent_worktree_count Number of active worktrees per agent.\n")
	sb.WriteString("# TYPE helix_agent_worktree_count gauge\n")
	for _, agent := range sortedAgentKeys(c.worktrees) {
		sb.WriteString(fmt.Sprintf("helix_agent_worktree_count{agent=%q} %d\n",
			agent, c.worktrees[agent]))
	}

	return sb.String()
}

// ============================================================================
// MetricsSource Interface (for integration with PlatformMetricsCollector)
// ============================================================================

// Metrics returns the collected metrics in Prometheus text format.
// Implements the MetricsSource interface.
func (c *AgentMetricsCollector) Metrics() string {
	return c.Collect()
}

// ============================================================================
// Summary
// ============================================================================

// AgentMetricsSummary provides aggregate counts for quick reporting.
type AgentMetricsSummary struct {
	TotalTasks    int64
	TotalLLMCalls int64
	TotalTokens   int64
	TotalCostUSD  float64
	AgentCount    int
	Agents        []string
}

// GetSummary returns aggregate statistics across all agents.
func (c *AgentMetricsCollector) GetSummary() AgentMetricsSummary {
	c.mu.RLock()
	defer c.mu.RUnlock()

	summary := AgentMetricsSummary{}

	// Collect unique agents
	agentSet := make(map[string]bool)
	for agent := range c.tasks {
		agentSet[agent] = true
	}
	for agent := range c.llmCalls {
		agentSet[agent] = true
	}
	for agent := range c.tokens {
		agentSet[agent] = true
	}
	for agent := range c.costs {
		agentSet[agent] = true
	}
	for agent := range c.uptime {
		agentSet[agent] = true
	}
	for agent := range c.worktrees {
		agentSet[agent] = true
	}

	for agent := range agentSet {
		summary.Agents = append(summary.Agents, agent)
	}
	sort.Strings(summary.Agents)
	summary.AgentCount = len(summary.Agents)

	// Sum tasks
	for _, repos := range c.tasks {
		for _, statuses := range repos {
			for _, count := range statuses {
				summary.TotalTasks += count
			}
		}
	}

	// Sum LLM calls
	for _, models := range c.llmCalls {
		for _, count := range models {
			summary.TotalLLMCalls += count
		}
	}

	// Sum tokens
	for _, models := range c.tokens {
		for _, types := range models {
			for _, count := range types {
				summary.TotalTokens += count
			}
		}
	}

	// Sum costs
	for _, repos := range c.costs {
		for _, cost := range repos {
			summary.TotalCostUSD += cost
		}
	}

	return summary
}

// ============================================================================
// Helpers
// ============================================================================

func sortedAgentKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedStatusKeys(m map[AgentTaskStatus]int64) []AgentTaskStatus {
	keys := make([]AgentTaskStatus, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return string(keys[i]) < string(keys[j])
	})
	return keys
}

func sortedTokenKeys(m map[TokenType]int64) []TokenType {
	keys := make([]TokenType, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return string(keys[i]) < string(keys[j])
	})
	return keys
}

func formatFloat(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%.4f", f)
}
