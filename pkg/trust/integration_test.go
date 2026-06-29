package trust

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/totalwindupflightsystems/helix/pkg/incident"
)

// =============================================================================
// Test helpers
// =============================================================================

func newTestBridge(t *testing.T) (*IncidentBridge, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.jsonl")
	ledger, err := NewLedger(path)
	require.NoError(t, err)
	t.Cleanup(func() { ledger.Close() })
	return NewIncidentBridge(ledger), path
}

func samplePaths() []incident.ChangePath {
	return []incident.ChangePath{
		{
			FilePath:    "internal/auth/handler.go",
			MergeSHA:    "abc123",
			AuthorID:    "agent-001",
			ReviewerIDs: []string{"agent-002"},
			ApproverID:  "human-001",
			CommitTime:  time.Now().Add(-2 * 24 * time.Hour),
		},
	}
}

func multiAgentPaths() []incident.ChangePath {
	return []incident.ChangePath{
		{
			FilePath:    "internal/auth/handler.go",
			MergeSHA:    "abc123",
			AuthorID:    "agent-001",
			ReviewerIDs: []string{"agent-002", "agent-003"},
			ApproverID:  "human-001",
			CommitTime:  time.Now().Add(-2 * 24 * time.Hour),
		},
		{
			FilePath:    "internal/auth/middleware.go",
			MergeSHA:    "def456",
			AuthorID:    "agent-004",
			ReviewerIDs: []string{"agent-001"},
			ApproverID:  "human-001",
			CommitTime:  time.Now().Add(-1 * 24 * time.Hour),
		},
	}
}

// =============================================================================
// NewIncidentBridge / NewIncidentBridgeWithFile
// =============================================================================

func TestNewIncidentBridge(t *testing.T) {
	bridge, _ := newTestBridge(t)
	assert.NotNil(t, bridge)
	assert.NotNil(t, bridge.scores)
	assert.Equal(t, 0, len(bridge.scores))
}

func TestNewIncidentBridgeWithFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.jsonl")

	// Create bridge with file — starts empty
	bridge, err := NewIncidentBridgeWithFile(path)
	require.NoError(t, err)
	t.Cleanup(func() { bridge.Close() })
	assert.NotNil(t, bridge)
}

func TestNewIncidentBridgeWithFile_BadPath(t *testing.T) {
	_, err := NewIncidentBridgeWithFile("/nonexistent/deep/path/trust.jsonl")
	assert.Error(t, err)
}

// =============================================================================
// Score management
// =============================================================================

func TestGetScore_UnknownAgent(t *testing.T) {
	bridge, _ := newTestBridge(t)
	score := bridge.GetScore("unknown-agent")
	assert.Equal(t, TrustScore(0.5), score)
}

func TestGetScore_KnownAgent(t *testing.T) {
	bridge, _ := newTestBridge(t)
	bridge.SetScore("agent-001", 0.8)
	score := bridge.GetScore("agent-001")
	assert.Equal(t, TrustScore(0.8), score)
}

func TestSetScore_ClampsHigh(t *testing.T) {
	bridge, _ := newTestBridge(t)
	bridge.SetScore("agent-001", 1.5)
	assert.Equal(t, TrustScore(1.0), bridge.GetScore("agent-001"))
}

func TestSetScore_ClampsLow(t *testing.T) {
	bridge, _ := newTestBridge(t)
	bridge.SetScore("agent-001", -0.5)
	assert.Equal(t, TrustScore(0.0), bridge.GetScore("agent-001"))
}

func TestAllScores(t *testing.T) {
	bridge, _ := newTestBridge(t)
	bridge.SetScore("a", 0.7)
	bridge.SetScore("b", 0.3)
	scores := bridge.AllScores()
	assert.Len(t, scores, 2)
	assert.Equal(t, TrustScore(0.7), scores["a"])
	assert.Equal(t, TrustScore(0.3), scores["b"])
}

func TestAllScores_ReturnsCopy(t *testing.T) {
	bridge, _ := newTestBridge(t)
	bridge.SetScore("a", 0.7)
	scores := bridge.AllScores()
	scores["a"] = 0.99 // mutate the copy
	assert.Equal(t, TrustScore(0.7), bridge.GetScore("a"), "original should be unchanged")
}

// =============================================================================
// LoadScoresFromLedger
// =============================================================================

func TestLoadScoresFromLedger_EmptyFile(t *testing.T) {
	bridge, path := newTestBridge(t)
	err := bridge.LoadScoresFromLedger(path)
	require.NoError(t, err)
	assert.Equal(t, 0, len(bridge.scores))
}

func TestLoadScoresFromLedger_NonexistentFile(t *testing.T) {
	bridge, _ := newTestBridge(t)
	err := bridge.LoadScoresFromLedger("/nonexistent/trust.jsonl")
	require.NoError(t, err) // nonexistent → nil events → no error
	assert.Equal(t, 0, len(bridge.scores))
}

func TestLoadScoresFromLedger_WithData(t *testing.T) {
	bridge, path := newTestBridge(t)

	// Seed a score and process an incident to create ledger events
	bridge.SetScore("agent-001", 0.8)
	engine := incident.NewAttributionEngine(nil)
	result, err := engine.Attribute("INC-001", samplePaths(), []string{"evidence-1"})
	require.NoError(t, err)
	_, err = bridge.ProcessResult(result, incident.SeverityMedium)
	require.NoError(t, err)

	// Create a fresh bridge and load from the file
	bridge2, path2 := newTestBridge(t)
	_ = path2
	err = bridge2.LoadScoresFromLedger(path)
	require.NoError(t, err)
	// agent-001 should have a score < 0.8 after penalty
	assert.Less(t, float64(bridge2.GetScore("agent-001")), 0.8)
	assert.Greater(t, float64(bridge2.GetScore("agent-001")), 0.0)
}

// =============================================================================
// ProcessResult
// =============================================================================

func TestProcessResult_NilResult(t *testing.T) {
	bridge, _ := newTestBridge(t)
	_, err := bridge.ProcessResult(nil, incident.SeverityMedium)
	assert.Error(t, err)
}

func TestProcessResult_SingleAgent(t *testing.T) {
	bridge, _ := newTestBridge(t)
	bridge.SetScore("agent-001", 0.8)

	engine := incident.NewAttributionEngine(nil)
	result, err := engine.Attribute("INC-001", samplePaths(), []string{"evidence-1"})
	require.NoError(t, err)

	summary, err := bridge.ProcessResult(result, incident.SeverityHigh)
	require.NoError(t, err)

	assert.Equal(t, "INC-001", summary.IncidentID)
	assert.Equal(t, incident.SeverityHigh, summary.Severity)
	assert.NotEmpty(t, summary.Agents)

	// agent-001 has the highest attribution (author: 70%)
	var agentPenalty *AgentPenalty
	for i := range summary.Agents {
		if summary.Agents[i].AgentID == "agent-001" {
			agentPenalty = &summary.Agents[i]
		}
	}
	require.NotNil(t, agentPenalty)
	assert.Greater(t, agentPenalty.AttributionShare, 0.0)
	assert.Less(t, agentPenalty.ScoreAfter, agentPenalty.ScoreBefore)
	assert.Equal(t, TrustScore(0.8), agentPenalty.ScoreBefore)

	// Score in cache should be updated
	cachedScore := bridge.GetScore("agent-001")
	assert.Equal(t, agentPenalty.ScoreAfter, cachedScore)
}

func TestProcessResult_MultiAgent(t *testing.T) {
	bridge, _ := newTestBridge(t)
	bridge.SetScore("agent-001", 0.8)
	bridge.SetScore("agent-002", 0.6)
	bridge.SetScore("agent-003", 0.5)
	bridge.SetScore("agent-004", 0.7)

	engine := incident.NewAttributionEngine(nil)
	result, err := engine.Attribute("INC-002", multiAgentPaths(), []string{"evidence-1", "evidence-2"})
	require.NoError(t, err)

	summary, err := bridge.ProcessResult(result, incident.SeverityCritical)
	require.NoError(t, err)

	// 4 agents should be penalized
	agentIDs := make(map[string]bool)
	for _, a := range summary.Agents {
		agentIDs[a.AgentID] = true
		assert.Less(t, a.ScoreAfter, a.ScoreBefore, "score should decrease for %s", a.AgentID)
	}
	assert.Contains(t, agentIDs, "agent-001")
	assert.Contains(t, agentIDs, "agent-002")
	assert.Contains(t, agentIDs, "agent-003")
	assert.Contains(t, agentIDs, "agent-004")
}

func TestProcessResult_AllSeverities(t *testing.T) {
	severities := []string{
		incident.SeverityLow,
		incident.SeverityMedium,
		incident.SeverityHigh,
		incident.SeverityCritical,
	}

	for _, sev := range severities {
		t.Run(sev, func(t *testing.T) {
			bridge, _ := newTestBridge(t)
			bridge.SetScore("agent-001", 0.8)

			engine := incident.NewAttributionEngine(nil)
			result, err := engine.Attribute("INC-"+sev, samplePaths(), nil)
			require.NoError(t, err)

			summary, err := bridge.ProcessResult(result, sev)
			require.NoError(t, err)
			require.NotEmpty(t, summary.Agents)

			// Higher severity → larger penalty
			penalty := summary.Agents[0]
			assert.Less(t, float64(penalty.ScoreAfter), 0.8, "score should decrease")

			// SeverityPenalty from incident package
			assert.Greater(t, penalty.SeverityPenalty, 0.0)
		})
	}
}

func TestProcessResult_ScoreAlreadyZero(t *testing.T) {
	bridge, _ := newTestBridge(t)
	bridge.SetScore("agent-001", 0.0)

	engine := incident.NewAttributionEngine(nil)
	result, err := engine.Attribute("INC-001", samplePaths(), nil)
	require.NoError(t, err)

	summary, err := bridge.ProcessResult(result, incident.SeverityCritical)
	require.NoError(t, err)
	for _, a := range summary.Agents {
		if a.AgentID == "agent-001" {
			assert.Equal(t, TrustScore(0.0), a.ScoreAfter, "score should stay at 0")
		}
	}
}

func TestProcessResult_WritesToLedger(t *testing.T) {
	bridge, path := newTestBridge(t)
	bridge.SetScore("agent-001", 0.8)

	engine := incident.NewAttributionEngine(nil)
	result, err := engine.Attribute("INC-001", samplePaths(), []string{"ev-1"})
	require.NoError(t, err)

	_, err = bridge.ProcessResult(result, incident.SeverityMedium)
	require.NoError(t, err)

	// Close ledger to flush
	bridge.Close()

	// Read the file and verify events were written
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, EventIncidentAttrib)
	assert.Contains(t, content, EventIncidentPenalty)
	assert.Contains(t, content, "agent-001")
	assert.Contains(t, content, "INC-001")
	assert.Contains(t, content, "ev-1")
}

func TestProcessResult_NoNilLedger(t *testing.T) {
	bridge := &IncidentBridge{scores: make(map[string]TrustScore)} // nil ledger
	engine := incident.NewAttributionEngine(nil)
	result, err := engine.Attribute("INC-001", samplePaths(), nil)
	require.NoError(t, err)
	_, err = bridge.ProcessResult(result, incident.SeverityLow)
	assert.Error(t, err)
}

// =============================================================================
// ProcessIncident (convenience method)
// =============================================================================

func TestProcessIncident_HappyPath(t *testing.T) {
	bridge, _ := newTestBridge(t)
	bridge.SetScore("agent-001", 0.7)

	store := incident.NewStore()
	engine := incident.NewAttributionEngine(store)

	summary, err := bridge.ProcessIncident(engine, "INC-005", samplePaths(), []string{"ev"}, incident.SeverityHigh)
	require.NoError(t, err)
	assert.Equal(t, "INC-005", summary.IncidentID)
	assert.NotEmpty(t, summary.Agents)
}

func TestProcessIncident_NilEngine(t *testing.T) {
	bridge, _ := newTestBridge(t)
	_, err := bridge.ProcessIncident(nil, "INC-005", samplePaths(), nil, incident.SeverityHigh)
	assert.Error(t, err)
}

func TestProcessIncident_AttributionError(t *testing.T) {
	bridge, _ := newTestBridge(t)
	engine := incident.NewAttributionEngine(nil)
	// Empty incident ID should cause attribution error
	_, err := bridge.ProcessIncident(engine, "", samplePaths(), nil, incident.SeverityHigh)
	assert.Error(t, err)
}

// =============================================================================
// ProcessBatch
// =============================================================================

func TestProcessBatch_MultipleIncidents(t *testing.T) {
	bridge, _ := newTestBridge(t)
	bridge.SetScore("agent-001", 0.9)
	bridge.SetScore("agent-004", 0.8)

	engine := incident.NewAttributionEngine(nil)
	batch := []BatchIncident{
		{
			IncidentID: "INC-001",
			Paths:      samplePaths(),
			Evidence:   []string{"ev-1"},
			Severity:   incident.SeverityMedium,
		},
		{
			IncidentID: "INC-002",
			Paths:      multiAgentPaths(),
			Evidence:   []string{"ev-2"},
			Severity:   incident.SeverityHigh,
		},
	}

	summaries, err := bridge.ProcessBatch(engine, batch)
	require.NoError(t, err)
	assert.Len(t, summaries, 2)
	assert.Equal(t, "INC-001", summaries[0].IncidentID)
	assert.Equal(t, "INC-002", summaries[1].IncidentID)

	// agent-001 is involved in both incidents — score should decrease further
	// after the second incident
	scoreAfterFirst := summaries[0].Agents
	scoreAfterSecond := bridge.GetScore("agent-001")

	// Find agent-001 in first summary
	var firstScore TrustScore
	for _, a := range scoreAfterFirst {
		if a.AgentID == "agent-001" {
			firstScore = a.ScoreAfter
		}
	}
	assert.Less(t, float64(scoreAfterSecond), float64(firstScore),
		"agent-001 score should be lower after second incident")
}

func TestProcessBatch_Empty(t *testing.T) {
	bridge, _ := newTestBridge(t)
	engine := incident.NewAttributionEngine(nil)
	summaries, err := bridge.ProcessBatch(engine, []BatchIncident{})
	require.NoError(t, err)
	assert.Empty(t, summaries)
}

func TestProcessBatch_PartialError(t *testing.T) {
	bridge, _ := newTestBridge(t)
	engine := incident.NewAttributionEngine(nil)
	batch := []BatchIncident{
		{IncidentID: "INC-001", Paths: samplePaths(), Severity: incident.SeverityLow},
		{IncidentID: "", Paths: samplePaths(), Severity: incident.SeverityLow}, // error
	}
	summaries, err := bridge.ProcessBatch(engine, batch)
	assert.Error(t, err)
	// Should have processed the first incident before erroring on the second
	assert.Len(t, summaries, 1)
}

// =============================================================================
// Replay integration — verify ledger is deterministic
// =============================================================================

func TestProcessResult_LedgerReplayDeterministic(t *testing.T) {
	bridge, path := newTestBridge(t)
	bridge.SetScore("agent-001", 0.8)

	engine := incident.NewAttributionEngine(nil)
	result, err := engine.Attribute("INC-001", samplePaths(), []string{"ev"})
	require.NoError(t, err)

	_, err = bridge.ProcessResult(result, incident.SeverityHigh)
	require.NoError(t, err)

	bridge.Close()

	// Replay the ledger and verify the score is consistent
	score, err := ReplayToScore(path, "agent-001")
	require.NoError(t, err)

	// The replayed score should be less than the initial 0.8
	assert.Less(t, float64(score), 0.8, "replayed score should reflect the incident penalty")
	assert.Greater(t, float64(score), 0.0, "score should be positive")
}

func TestProcessResult_MultiplePenaltiesAccumulate(t *testing.T) {
	bridge, path := newTestBridge(t)
	bridge.SetScore("agent-001", 0.9)

	engine := incident.NewAttributionEngine(nil)

	// First incident
	r1, _ := engine.Attribute("INC-001", samplePaths(), nil)
	_, err := bridge.ProcessResult(r1, incident.SeverityMedium)
	require.NoError(t, err)

	scoreAfterFirst := bridge.GetScore("agent-001")

	// Second incident — same agent
	r2, _ := engine.Attribute("INC-002", samplePaths(), nil)
	_, err = bridge.ProcessResult(r2, incident.SeverityMedium)
	require.NoError(t, err)

	scoreAfterSecond := bridge.GetScore("agent-001")

	assert.Less(t, float64(scoreAfterSecond), float64(scoreAfterFirst),
		"second incident should further reduce score")

	bridge.Close()

	// Verify replay matches
	replayed, err := ReplayToScore(path, "agent-001")
	require.NoError(t, err)
	assert.InDelta(t, float64(scoreAfterSecond), float64(replayed), 0.15,
		"replayed score should approximately match the cached score")
}

// =============================================================================
// ProcessSummary helpers
// =============================================================================

func TestTotalScoreReduction(t *testing.T) {
	bridge, _ := newTestBridge(t)
	bridge.SetScore("agent-001", 0.8)

	engine := incident.NewAttributionEngine(nil)
	result, err := engine.Attribute("INC-001", samplePaths(), nil)
	require.NoError(t, err)

	summary, err := bridge.ProcessResult(result, incident.SeverityHigh)
	require.NoError(t, err)

	reduction := summary.TotalScoreReduction()
	assert.Greater(t, reduction, 0.0, "total reduction should be positive")
}

func TestMostAffectedAgent(t *testing.T) {
	bridge, _ := newTestBridge(t)
	bridge.SetScore("agent-001", 0.8)
	bridge.SetScore("agent-004", 0.8)

	engine := incident.NewAttributionEngine(nil)
	result, err := engine.Attribute("INC-002", multiAgentPaths(), nil)
	require.NoError(t, err)

	summary, err := bridge.ProcessResult(result, incident.SeverityHigh)
	require.NoError(t, err)

	most := summary.MostAffectedAgent()
	assert.NotEmpty(t, most, "should return the most affected agent")
}

func TestVerifyDecrease_AllDecreased(t *testing.T) {
	bridge, _ := newTestBridge(t)
	bridge.SetScore("agent-001", 0.8)

	engine := incident.NewAttributionEngine(nil)
	result, _ := engine.Attribute("INC-001", samplePaths(), nil)
	summary, _ := bridge.ProcessResult(result, incident.SeverityMedium)

	err := summary.VerifyDecrease()
	assert.NoError(t, err)
}

func TestVerifyDecrease_ScoreAtZero(t *testing.T) {
	bridge, _ := newTestBridge(t)
	bridge.SetScore("agent-001", 0.0)

	engine := incident.NewAttributionEngine(nil)
	result, _ := engine.Attribute("INC-001", samplePaths(), nil)
	summary, _ := bridge.ProcessResult(result, incident.SeverityMedium)

	// Score was already 0, so after - before = 0, but before is also 0
	// The condition `before > 0` guards this case
	err := summary.VerifyDecrease()
	assert.NoError(t, err, "no error when score was already 0")
}

func TestVerifyDecrease_FailsWhenScoreIncreases(t *testing.T) {
	// Craft a summary where score increased (shouldn't happen normally)
	summary := &ProcessSummary{
		IncidentID: "INC-FAKE",
		Severity:   incident.SeverityHigh,
		Agents: []AgentPenalty{
			{
				AgentID:     "agent-x",
				ScoreBefore: 0.5,
				ScoreAfter:  0.6, // increased!
			},
		},
	}
	err := summary.VerifyDecrease()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "did not decrease")
}

// =============================================================================
// Evidence links in events
// =============================================================================

func TestProcessResult_EvidenceInEvents(t *testing.T) {
	bridge, path := newTestBridge(t)
	bridge.SetScore("agent-001", 0.8)

	engine := incident.NewAttributionEngine(nil)
	evidence := []string{"https://forgejo/pr/1", "runbook-id-42", "monitoring-alert-7"}
	result, err := engine.Attribute("INC-001", samplePaths(), evidence)
	require.NoError(t, err)

	_, err = bridge.ProcessResult(result, incident.SeverityCritical)
	require.NoError(t, err)
	bridge.Close()

	events, err := Replay(path)
	require.NoError(t, err)

	for _, evt := range events {
		if evt.AgentID == "agent-001" {
			assert.Equal(t, evidence, evt.Data.Evidence,
				"evidence links should be in event data")
		}
	}
}

// =============================================================================
// Close
// =============================================================================

func TestClose_NilLedger(t *testing.T) {
	bridge := &IncidentBridge{scores: make(map[string]TrustScore)}
	err := bridge.Close()
	assert.NoError(t, err)
}

func TestClose_RealLedger(t *testing.T) {
	bridge, _ := newTestBridge(t)
	err := bridge.Close()
	assert.NoError(t, err)
}
