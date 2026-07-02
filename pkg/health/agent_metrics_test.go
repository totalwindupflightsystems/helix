package health

import (
	"strings"
	"testing"
)

// ============================================================================
// Construction
// ============================================================================

func TestNewAgentMetricsCollector(t *testing.T) {
	c := NewAgentMetricsCollector()
	if c == nil {
		t.Fatal("expected non-nil collector")
	}
}

// ============================================================================
// RecordTask / GetTaskCount
// ============================================================================

func TestRecordTask_Single(t *testing.T) {
	c := NewAgentMetricsCollector()
	c.RecordTask("agent-1", "helix", TaskCompleted)

	if got := c.GetTaskCount("agent-1", "helix", TaskCompleted); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestRecordTask_Multiple(t *testing.T) {
	c := NewAgentMetricsCollector()
	c.RecordTask("agent-1", "helix", TaskCompleted)
	c.RecordTask("agent-1", "helix", TaskCompleted)
	c.RecordTask("agent-1", "helix", TaskFailed)

	if got := c.GetTaskCount("agent-1", "helix", TaskCompleted); got != 2 {
		t.Errorf("expected 2 completed, got %d", got)
	}
	if got := c.GetTaskCount("agent-1", "helix", TaskFailed); got != 1 {
		t.Errorf("expected 1 failed, got %d", got)
	}
}

func TestGetTaskCount_NotExists(t *testing.T) {
	c := NewAgentMetricsCollector()
	if got := c.GetTaskCount("nonexistent", "repo", TaskCompleted); got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
}

// ============================================================================
// RecordLLMCall / GetLLMCallCount
// ============================================================================

func TestRecordLLMCall(t *testing.T) {
	c := NewAgentMetricsCollector()
	c.RecordLLMCall("agent-1", "deepseek-v4-pro")
	c.RecordLLMCall("agent-1", "deepseek-v4-pro")
	c.RecordLLMCall("agent-1", "glm-5.2")

	if got := c.GetLLMCallCount("agent-1", "deepseek-v4-pro"); got != 2 {
		t.Errorf("expected 2, got %d", got)
	}
	if got := c.GetLLMCallCount("agent-1", "glm-5.2"); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestGetLLMCallCount_NotExists(t *testing.T) {
	c := NewAgentMetricsCollector()
	if got := c.GetLLMCallCount("nope", "model"); got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
}

// ============================================================================
// RecordTokens / GetTokenCount
// ============================================================================

func TestRecordTokens(t *testing.T) {
	c := NewAgentMetricsCollector()
	c.RecordTokens("agent-1", "deepseek-v4-pro", TokenPrompt, 50000)
	c.RecordTokens("agent-1", "deepseek-v4-pro", TokenCompletion, 8200)
	c.RecordTokens("agent-1", "deepseek-v4-pro", TokenPrompt, 10000)

	if got := c.GetTokenCount("agent-1", "deepseek-v4-pro", TokenPrompt); got != 60000 {
		t.Errorf("expected 60000 prompt tokens, got %d", got)
	}
	if got := c.GetTokenCount("agent-1", "deepseek-v4-pro", TokenCompletion); got != 8200 {
		t.Errorf("expected 8200 completion tokens, got %d", got)
	}
}

func TestGetTokenCount_NotExists(t *testing.T) {
	c := NewAgentMetricsCollector()
	if got := c.GetTokenCount("nope", "model", TokenPrompt); got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
}

// ============================================================================
// RecordCost / GetCost
// ============================================================================

func TestRecordCost(t *testing.T) {
	c := NewAgentMetricsCollector()
	c.RecordCost("agent-1", "helix", 1.50)
	c.RecordCost("agent-1", "helix", 2.25)

	if got := c.GetCost("agent-1", "helix"); got != 3.75 {
		t.Errorf("expected 3.75, got %f", got)
	}
}

func TestGetCost_NotExists(t *testing.T) {
	c := NewAgentMetricsCollector()
	if got := c.GetCost("nope", "repo"); got != 0 {
		t.Errorf("expected 0, got %f", got)
	}
}

// ============================================================================
// SetSandboxUptime / GetSandboxUptime
// ============================================================================

func TestSetSandboxUptime(t *testing.T) {
	c := NewAgentMetricsCollector()
	c.SetSandboxUptime("agent-1", 3600.5)

	if got := c.GetSandboxUptime("agent-1"); got != 3600.5 {
		t.Errorf("expected 3600.5, got %f", got)
	}
}

func TestGetSandboxUptime_NotExists(t *testing.T) {
	c := NewAgentMetricsCollector()
	if got := c.GetSandboxUptime("nope"); got != 0 {
		t.Errorf("expected 0, got %f", got)
	}
}

// ============================================================================
// SetWorktreeCount / GetWorktreeCount
// ============================================================================

func TestSetWorktreeCount(t *testing.T) {
	c := NewAgentMetricsCollector()
	c.SetWorktreeCount("agent-1", 3)

	if got := c.GetWorktreeCount("agent-1"); got != 3 {
		t.Errorf("expected 3, got %d", got)
	}
}

func TestGetWorktreeCount_NotExists(t *testing.T) {
	c := NewAgentMetricsCollector()
	if got := c.GetWorktreeCount("nope"); got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
}

// ============================================================================
// Collect — Prometheus Text Exposition Format
// ============================================================================

func TestCollect_Empty(t *testing.T) {
	c := NewAgentMetricsCollector()
	output := c.Collect()

	if !strings.Contains(output, "helix_agent_tasks_total") {
		t.Error("expected tasks metric in output")
	}
	if !strings.Contains(output, "helix_agent_llm_calls_total") {
		t.Error("expected llm_calls metric in output")
	}
	if !strings.Contains(output, "helix_agent_tokens_used") {
		t.Error("expected tokens_used metric in output")
	}
	if !strings.Contains(output, "helix_agent_cost_total") {
		t.Error("expected cost_total metric in output")
	}
	if !strings.Contains(output, "helix_agent_sandbox_uptime_seconds") {
		t.Error("expected uptime metric in output")
	}
	if !strings.Contains(output, "helix_agent_worktree_count") {
		t.Error("expected worktree metric in output")
	}
}

func TestCollect_WithData(t *testing.T) {
	c := NewAgentMetricsCollector()
	c.RecordTask("agent-1", "helix", TaskCompleted)
	c.RecordLLMCall("agent-1", "deepseek-v4-pro")
	c.RecordTokens("agent-1", "deepseek-v4-pro", TokenPrompt, 50000)
	c.RecordCost("agent-1", "helix", 1.50)
	c.SetSandboxUptime("agent-1", 3600)
	c.SetWorktreeCount("agent-1", 2)

	output := c.Collect()

	if !strings.Contains(output, "agent=\"agent-1\"") {
		t.Error("expected agent-1 label in output")
	}
	if !strings.Contains(output, "status=\"completed\"") {
		t.Error("expected status=completed in output")
	}
	if !strings.Contains(output, "model=\"deepseek-v4-pro\"") {
		t.Error("expected model label in output")
	}
	if !strings.Contains(output, "type=\"prompt\"") {
		t.Error("expected type=prompt in output")
	}
}

func TestCollect_HasHELPHeaders(t *testing.T) {
	c := NewAgentMetricsCollector()
	output := c.Collect()

	if !strings.Contains(output, "# HELP") {
		t.Error("expected HELP headers")
	}
}

func TestCollect_HasTYPEHeaders(t *testing.T) {
	c := NewAgentMetricsCollector()
	output := c.Collect()

	if !strings.Contains(output, "# TYPE") {
		t.Error("expected TYPE headers")
	}
}

func TestCollect_CounterType(t *testing.T) {
	c := NewAgentMetricsCollector()
	output := c.Collect()

	// tasks, llm_calls, tokens, cost should be counters
	if !strings.Contains(output, "# TYPE helix_agent_tasks_total counter") {
		t.Error("expected tasks as counter")
	}
	if !strings.Contains(output, "# TYPE helix_agent_cost_total counter") {
		t.Error("expected cost as counter")
	}
}

func TestCollect_GaugeType(t *testing.T) {
	c := NewAgentMetricsCollector()
	output := c.Collect()

	// uptime, worktree should be gauges
	if !strings.Contains(output, "# TYPE helix_agent_sandbox_uptime_seconds gauge") {
		t.Error("expected uptime as gauge")
	}
	if !strings.Contains(output, "# TYPE helix_agent_worktree_count gauge") {
		t.Error("expected worktree as gauge")
	}
}

func TestCollect_DeterministicOrdering(t *testing.T) {
	c := NewAgentMetricsCollector()
	c.RecordTask("agent-z", "repo-b", TaskCompleted)
	c.RecordTask("agent-a", "repo-a", TaskCompleted)

	output1 := c.Collect()
	output2 := c.Collect()

	if output1 != output2 {
		t.Error("expected deterministic output")
	}
}

func TestCollect_SortedAgents(t *testing.T) {
	c := NewAgentMetricsCollector()
	c.SetSandboxUptime("agent-z", 100)
	c.SetSandboxUptime("agent-a", 200)
	c.SetSandboxUptime("agent-m", 300)

	output := c.Collect()

	// agent-a should appear before agent-m, which should appear before agent-z
	idxA := strings.Index(output, "agent-a")
	idxM := strings.Index(output, "agent-m")
	idxZ := strings.Index(output, "agent-z")

	if idxA == -1 || idxM == -1 || idxZ == -1 {
		t.Fatal("expected all agents in output")
	}
	if !(idxA < idxM && idxM < idxZ) {
		t.Error("expected agents sorted alphabetically")
	}
}

// ============================================================================
// Metrics (interface method)
// ============================================================================

func TestMetrics_ReturnsPrometheusFormat(t *testing.T) {
	c := NewAgentMetricsCollector()
	c.RecordTask("agent-1", "repo", TaskCompleted)

	output := c.Metrics()
	if !strings.Contains(output, "helix_agent_tasks_total") {
		t.Error("expected Prometheus format from Metrics()")
	}
}

// ============================================================================
// GetSummary
// ============================================================================

func TestGetSummary_Empty(t *testing.T) {
	c := NewAgentMetricsCollector()
	s := c.GetSummary()

	if s.TotalTasks != 0 {
		t.Errorf("expected 0 tasks, got %d", s.TotalTasks)
	}
	if s.AgentCount != 0 {
		t.Errorf("expected 0 agents, got %d", s.AgentCount)
	}
}

func TestGetSummary_WithData(t *testing.T) {
	c := NewAgentMetricsCollector()
	c.RecordTask("agent-1", "helix", TaskCompleted)
	c.RecordTask("agent-1", "helix", TaskFailed)
	c.RecordLLMCall("agent-1", "model-a")
	c.RecordTokens("agent-1", "model-a", TokenPrompt, 1000)
	c.RecordCost("agent-1", "helix", 5.0)

	c.RecordTask("agent-2", "helix", TaskCompleted)
	c.RecordCost("agent-2", "helix", 3.0)

	s := c.GetSummary()

	if s.TotalTasks != 3 {
		t.Errorf("expected 3 total tasks, got %d", s.TotalTasks)
	}
	if s.TotalLLMCalls != 1 {
		t.Errorf("expected 1 LLM call, got %d", s.TotalLLMCalls)
	}
	if s.TotalTokens != 1000 {
		t.Errorf("expected 1000 tokens, got %d", s.TotalTokens)
	}
	if s.TotalCostUSD != 8.0 {
		t.Errorf("expected 8.0 cost, got %f", s.TotalCostUSD)
	}
	if s.AgentCount != 2 {
		t.Errorf("expected 2 agents, got %d", s.AgentCount)
	}
}

func TestGetSummary_AgentsSorted(t *testing.T) {
	c := NewAgentMetricsCollector()
	c.RecordTask("agent-z", "repo", TaskCompleted)
	c.RecordTask("agent-a", "repo", TaskCompleted)

	s := c.GetSummary()

	if len(s.Agents) < 2 {
		t.Fatal("expected at least 2 agents")
	}
	if s.Agents[0] != "agent-a" {
		t.Errorf("expected first agent 'agent-a', got %s", s.Agents[0])
	}
}

// ============================================================================
// Concurrent Access
// ============================================================================

func TestConcurrentRecordTask(t *testing.T) {
	c := NewAgentMetricsCollector()

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				c.RecordTask("agent-concurrent", "repo", TaskCompleted)
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	if got := c.GetTaskCount("agent-concurrent", "repo", TaskCompleted); got != 1000 {
		t.Errorf("expected 1000 tasks, got %d", got)
	}
}

func TestConcurrentCollect(t *testing.T) {
	c := NewAgentMetricsCollector()
	c.RecordTask("agent-1", "repo", TaskCompleted)

	done := make(chan bool, 2)

	go func() {
		for i := 0; i < 100; i++ {
			c.RecordTask("agent-1", "repo", TaskCompleted)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_ = c.Collect()
		}
		done <- true
	}()

	<-done
	<-done
}

// ============================================================================
// formatFloat
// ============================================================================

func TestFormatFloat_Integer(t *testing.T) {
	if got := formatFloat(100.0); got != "100" {
		t.Errorf("expected '100', got %s", got)
	}
}

func TestFormatFloat_Decimal(t *testing.T) {
	if got := formatFloat(3.5); got != "3.5000" {
		t.Errorf("expected '3.5000', got %s", got)
	}
}
