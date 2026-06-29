package review

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// DefaultAgentInfo
// =============================================================================

func TestDefaultAgentInfo_AllTypes(t *testing.T) {
	types := []AgentType{AgentAssumptionBuster, AgentRedTeam, AgentChaosEngineer, AgentCostAuditor}
	for _, at := range types {
		info := DefaultAgentInfo(at)
		assert.Equal(t, at, info.Type)
		assert.NotEmpty(t, info.Name)
		assert.NotEmpty(t, info.Mission)
	}
}

func TestDefaultAgentInfo_Unknown(t *testing.T) {
	info := DefaultAgentInfo(AgentType("unknown"))
	assert.Equal(t, "unknown", info.Mission)
}

// =============================================================================
// extractFilePatterns
// =============================================================================

func TestExtractFilePatterns_Auth(t *testing.T) {
	patterns := extractFilePatterns([]string{
		"internal/auth/handler.go",
		"internal/auth/middleware.go",
	})
	assert.True(t, patterns["auth"])
	assert.False(t, patterns["crypto"])
}

func TestExtractFilePatterns_Crypto(t *testing.T) {
	patterns := extractFilePatterns([]string{
		"pkg/crypto/cipher.go",
	})
	assert.True(t, patterns["crypto"])
}

func TestExtractFilePatterns_Secret(t *testing.T) {
	patterns := extractFilePatterns([]string{
		"config/secrets.yaml",
	})
	assert.True(t, patterns["secret"])
}

func TestExtractFilePatterns_Multiple(t *testing.T) {
	patterns := extractFilePatterns([]string{
		"internal/auth/token.go",
		"pkg/crypto/aes.go",
		"config/secret_keys.yaml",
	})
	assert.True(t, patterns["auth"])
	assert.True(t, patterns["crypto"])
	assert.True(t, patterns["secret"])
}

func TestExtractFilePatterns_None(t *testing.T) {
	patterns := extractFilePatterns([]string{
		"cmd/server/main.go",
		"README.md",
	})
	assert.Empty(t, patterns)
}

func TestExtractFilePatterns_Empty(t *testing.T) {
	patterns := extractFilePatterns(nil)
	assert.Empty(t, patterns)
}

func TestExtractFilePatterns_CaseInsensitive(t *testing.T) {
	patterns := extractFilePatterns([]string{
		"internal/AUTH/handler.go",
		"pkg/Crypto.go",
	})
	assert.True(t, patterns["auth"])
	assert.True(t, patterns["crypto"])
}

// =============================================================================
// SelectAgents
// =============================================================================

func TestSelectAgents_BehavioralChange(t *testing.T) {
	d := NewAdversarialAgentDispatcher()
	// Register all agents so selection can succeed
	for _, at := range []AgentType{AgentAssumptionBuster, AgentRedTeam, AgentChaosEngineer, AgentCostAuditor} {
		d.Register(NewStubAgent(DefaultAgentInfo(at), &AgentResult{Verdict: "clean"}))
	}

	selected, err := d.SelectAgents(CategoryBehavioral, []string{"internal/logic/handler.go"})
	require.NoError(t, err)
	assert.Contains(t, selected, AgentAssumptionBuster)
	assert.Contains(t, selected, AgentCostAuditor)
	// Red team and chaos shouldn't be triggered for behavioral without auth/crypto
	assert.NotContains(t, selected, AgentRedTeam)
	assert.NotContains(t, selected, AgentChaosEngineer)
}

func TestSelectAgents_AuthChange(t *testing.T) {
	d := NewAdversarialAgentDispatcher()
	for _, at := range []AgentType{AgentAssumptionBuster, AgentRedTeam, AgentChaosEngineer, AgentCostAuditor} {
		d.Register(NewStubAgent(DefaultAgentInfo(at), &AgentResult{Verdict: "clean"}))
	}

	selected, err := d.SelectAgents(CategoryContract, []string{"internal/auth/handler.go"})
	require.NoError(t, err)
	assert.Contains(t, selected, AgentRedTeam)
	assert.Contains(t, selected, AgentCostAuditor)
	assert.Contains(t, selected, AgentAssumptionBuster) // contract category triggers assumption-buster
}

func TestSelectAgents_ResilienceChange(t *testing.T) {
	d := NewAdversarialAgentDispatcher()
	for _, at := range []AgentType{AgentAssumptionBuster, AgentRedTeam, AgentChaosEngineer, AgentCostAuditor} {
		d.Register(NewStubAgent(DefaultAgentInfo(at), &AgentResult{Verdict: "clean"}))
	}

	selected, err := d.SelectAgents(CategoryResilience, []string{"pkg/retry/retry.go"})
	require.NoError(t, err)
	assert.Contains(t, selected, AgentChaosEngineer)
	assert.Contains(t, selected, AgentCostAuditor)
}

func TestSelectAgents_CostAuditorAlwaysSelected(t *testing.T) {
	d := NewAdversarialAgentDispatcher()
	for _, at := range []AgentType{AgentAssumptionBuster, AgentRedTeam, AgentChaosEngineer, AgentCostAuditor} {
		d.Register(NewStubAgent(DefaultAgentInfo(at), &AgentResult{Verdict: "clean"}))
	}

	for _, cat := range []ChangeCategory{CategoryContract, CategoryBehavioral, CategoryResilience, CategoryCosmetic} {
		selected, err := d.SelectAgents(cat, []string{"any/file.go"})
		require.NoError(t, err)
		assert.Contains(t, selected, AgentCostAuditor, "cost-auditor should be selected for %s", cat)
	}
}

func TestSelectAgents_MissingRequiredAgent(t *testing.T) {
	d := NewAdversarialAgentDispatcher()
	// Only register cost-auditor, not assumption-buster
	d.Register(NewStubAgent(DefaultAgentInfo(AgentCostAuditor), &AgentResult{Verdict: "clean"}))

	_, err := d.SelectAgents(CategoryBehavioral, []string{"file.go"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "assumption-buster")
}

func TestSelectAgents_NoDuplicateAgents(t *testing.T) {
	d := NewAdversarialAgentDispatcher()
	for _, at := range []AgentType{AgentAssumptionBuster, AgentRedTeam, AgentChaosEngineer, AgentCostAuditor} {
		d.Register(NewStubAgent(DefaultAgentInfo(at), &AgentResult{Verdict: "clean"}))
	}

	// Contract + auth should trigger multiple rules, but agent should appear once
	selected, err := d.SelectAgents(CategoryContract, []string{"internal/auth/auth.go"})
	require.NoError(t, err)

	counts := make(map[AgentType]int)
	for _, at := range selected {
		counts[at]++
	}
	for at, count := range counts {
		assert.Equal(t, 1, count, "agent %s appeared %d times", at, count)
	}
}

// =============================================================================
// Dispatch
// =============================================================================

func TestDispatch_HappyPath(t *testing.T) {
	d := NewAdversarialAgentDispatcher()
	for _, at := range []AgentType{AgentAssumptionBuster, AgentRedTeam, AgentChaosEngineer, AgentCostAuditor} {
		d.Register(NewStubAgent(DefaultAgentInfo(at), &AgentResult{Verdict: "clean"}))
	}

	req := AgentRequest{
		Diff:           "some diff",
		ChangeCategory: CategoryBehavioral,
		ChangedFiles:   []string{"internal/logic/handler.go"},
		PRURL:          "https://forgejo/pr/1",
	}

	report, err := d.Dispatch(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, report.CleanBill)
	assert.GreaterOrEqual(t, report.AgentsRun, 2) // assumption-buster + cost-auditor minimum
}

func TestDispatch_ExploitFound(t *testing.T) {
	d := NewAdversarialAgentDispatcher()
	d.Register(NewStubAgent(DefaultAgentInfo(AgentAssumptionBuster), &AgentResult{Verdict: "clean"}))
	d.Register(NewStubAgent(DefaultAgentInfo(AgentRedTeam), &AgentResult{
		Verdict: "exploited",
		ExploitPaths: []ExploitPath{
			{Description: "SQL injection", Severity: "critical", File: "auth.go", Line: 42},
		},
	}))
	d.Register(NewStubAgent(DefaultAgentInfo(AgentCostAuditor), &AgentResult{Verdict: "clean"}))

	req := AgentRequest{
		Diff:           "diff",
		ChangeCategory: CategoryContract,
		ChangedFiles:   []string{"internal/auth/handler.go"},
	}

	report, err := d.Dispatch(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, report.CleanBill)
	assert.Equal(t, 1, report.TotalExploits())
}

func TestDispatch_AllClean(t *testing.T) {
	d := NewAdversarialAgentDispatcher()
	for _, at := range []AgentType{AgentAssumptionBuster, AgentCostAuditor} {
		d.Register(NewStubAgent(DefaultAgentInfo(at), &AgentResult{Verdict: "clean"}))
	}

	req := AgentRequest{
		Diff:           "diff",
		ChangeCategory: CategoryBehavioral,
		ChangedFiles:   []string{"logic.go"},
	}

	report, err := d.Dispatch(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, report.CleanBill)
}

func TestDispatch_AgentError(t *testing.T) {
	d := NewAdversarialAgentDispatcher()
	d.Register(NewErrorAgent(DefaultAgentInfo(AgentAssumptionBuster), errors.New("agent crashed")))
	d.Register(NewStubAgent(DefaultAgentInfo(AgentCostAuditor), &AgentResult{Verdict: "clean"}))

	req := AgentRequest{
		Diff:           "diff",
		ChangeCategory: CategoryBehavioral,
		ChangedFiles:   []string{"logic.go"},
	}

	report, err := d.Dispatch(context.Background(), req)
	require.NoError(t, err) // partial failure, not full failure
	assert.NotEmpty(t, report.Errors)
	assert.Equal(t, 1, report.AgentsRun) // cost-auditor succeeded
}

func TestDispatch_AllAgentsFail(t *testing.T) {
	d := NewAdversarialAgentDispatcher()
	d.Register(NewErrorAgent(DefaultAgentInfo(AgentAssumptionBuster), errors.New("crash 1")))
	d.Register(NewErrorAgent(DefaultAgentInfo(AgentCostAuditor), errors.New("crash 2")))

	req := AgentRequest{
		Diff:           "diff",
		ChangeCategory: CategoryBehavioral,
		ChangedFiles:   []string{"logic.go"},
	}

	_, err := d.Dispatch(context.Background(), req)
	assert.Error(t, err)
}

func TestDispatch_EmptyRequest(t *testing.T) {
	d := NewAdversarialAgentDispatcher()
	_, err := d.Dispatch(context.Background(), AgentRequest{})
	assert.Error(t, err)
}

func TestDispatch_NoAgentsSelected(t *testing.T) {
	d := NewAdversarialAgentDispatcher()
	// No agents registered at all, but cosmetic change only triggers cost-auditor
	// which is required → error

	_, err := d.SelectAgents(CategoryCosmetic, []string{"format.go"})
	assert.Error(t, err)
}

// =============================================================================
// DispatchReport helpers
// =============================================================================

func TestTotalExploits(t *testing.T) {
	report := &DispatchReport{
		Results: []AgentResult{
			{ExploitPaths: []ExploitPath{{Severity: "high"}, {Severity: "low"}}},
			{ExploitPaths: []ExploitPath{{Severity: "critical"}}},
			{},
		},
	}
	assert.Equal(t, 3, report.TotalExploits())
}

func TestTotalAssumptions(t *testing.T) {
	report := &DispatchReport{
		Results: []AgentResult{
			{AssumptionsFound: []Assumption{{}, {}}},
			{AssumptionsFound: []Assumption{{}}},
		},
	}
	assert.Equal(t, 3, report.TotalAssumptions())
}

func TestCriticalExploits(t *testing.T) {
	report := &DispatchReport{
		Results: []AgentResult{
			{
				ExploitPaths: []ExploitPath{
					{Severity: "critical", Description: "RCE"},
					{Severity: "high", Description: "XSS"},
				},
			},
			{
				ExploitPaths: []ExploitPath{
					{Severity: "critical", Description: "SSRF"},
				},
			},
		},
	}
	critical := report.CriticalExploits()
	assert.Len(t, critical, 2)
}

func TestAllFindings(t *testing.T) {
	report := &DispatchReport{
		Results: []AgentResult{
			{Findings: []Finding{{Type: "bug"}, {Type: "vuln"}}},
			{Findings: []Finding{{Type: "perf"}}},
		},
	}
	all := report.AllFindings()
	assert.Len(t, all, 3)
}

func TestDispatchReport_Summary(t *testing.T) {
	report := &DispatchReport{
		AgentsRun: 3,
		CleanBill: false,
		Results: []AgentResult{
			{ExploitPaths: []ExploitPath{{}}, AssumptionsFound: []Assumption{{}, {}}},
		},
	}
	s := report.Summary()
	assert.Contains(t, s, "agents ran")
	assert.Contains(t, s, "exploits")
}

// =============================================================================
// StubAgent
// =============================================================================

func TestStubAgent_Identity(t *testing.T) {
	info := DefaultAgentInfo(AgentRedTeam)
	agent := NewStubAgent(info, &AgentResult{Verdict: "clean"})
	assert.Equal(t, info, agent.Identity())
}

func TestStubAgent_Prosecute(t *testing.T) {
	result := &AgentResult{Verdict: "suspicious"}
	agent := NewStubAgent(DefaultAgentInfo(AgentAssumptionBuster), result)

	res, err := agent.Prosecute(context.Background(), AgentRequest{})
	require.NoError(t, err)
	assert.Equal(t, "suspicious", res.Verdict)
}

func TestStubAgent_Error(t *testing.T) {
	agent := NewErrorAgent(DefaultAgentInfo(AgentRedTeam), errors.New("fail"))
	_, err := agent.Prosecute(context.Background(), AgentRequest{})
	assert.Error(t, err)
}

// =============================================================================
// Registry
// =============================================================================

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := newAgentRegistry()
	agent := NewStubAgent(DefaultAgentInfo(AgentRedTeam), &AgentResult{Verdict: "clean"})
	r.Register(agent)

	got, ok := r.Get(AgentRedTeam)
	assert.True(t, ok)
	assert.NotNil(t, got)
}

func TestRegistry_Get_NotFound(t *testing.T) {
	r := newAgentRegistry()
	_, ok := r.Get(AgentRedTeam)
	assert.False(t, ok)
}

func TestRegistry_All(t *testing.T) {
	r := newAgentRegistry()
	r.Register(NewStubAgent(DefaultAgentInfo(AgentRedTeam), &AgentResult{}))
	r.Register(NewStubAgent(DefaultAgentInfo(AgentCostAuditor), &AgentResult{}))
	agents := r.All()
	assert.Len(t, agents, 2)
}

// =============================================================================
// AgentResult types
// =============================================================================

func TestCostEstimate_OverBudget(t *testing.T) {
	est := &CostEstimate{
		EstimatedCostUSD: 150.0,
		BudgetRemaining:  -50.0,
		OverBudget:       true,
	}
	assert.True(t, est.OverBudget)
}

func TestExploitPath_Fields(t *testing.T) {
	ep := ExploitPath{
		Description: "Buffer overflow",
		Severity:    "critical",
		File:        "handler.go",
		Line:        100,
		Steps:       []string{"step1", "step2"},
	}
	assert.Equal(t, "critical", ep.Severity)
	assert.Len(t, ep.Steps, 2)
}

func TestAssumption_Fields(t *testing.T) {
	a := Assumption{
		Description: "Assumes input is always UTF-8",
		RiskLevel:   "high",
		Challenge:   "Binary input may cause panics",
	}
	assert.Equal(t, "high", a.RiskLevel)
}

func TestFaultResult_Fields(t *testing.T) {
	fr := FaultResult{
		FaultType:   "network",
		Description: "Simulated network partition",
		Outcome:     "degraded",
		Severity:    "medium",
	}
	assert.Equal(t, "degraded", fr.Outcome)
}
