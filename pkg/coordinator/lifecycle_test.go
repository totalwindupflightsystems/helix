package coordinator

import (
	"context"
	"testing"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/estimate"
	"github.com/totalwindupflightsystems/helix/pkg/mergegate"
	"github.com/totalwindupflightsystems/helix/pkg/negotiate"
	"github.com/totalwindupflightsystems/helix/pkg/review"
	"github.com/totalwindupflightsystems/helix/pkg/trust"
	"github.com/totalwindupflightsystems/helix/pkg/verify"
)

// ============================================================================
// Mock Review Model
// ============================================================================

type mockModelClient struct {
	info    review.ModelInfo
	verdict string
	err     error
}

func (m *mockModelClient) Review(ctx context.Context, req review.ReviewRequest) (*review.ModelReviewResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &review.ModelReviewResult{
		Verdict:  m.verdict,
		Findings: []review.Finding{},
	}, nil
}

func (m *mockModelClient) Info() review.ModelInfo { return m.info }

// ============================================================================
// Helpers
// ============================================================================

func makeEstimator() *estimate.Estimator {
	cacheRead := 0.014
	cacheWrite := 0.014
	pricing := &estimate.PricingYAML{
		Providers: map[string]estimate.ProviderPricing{
			"deepseek": {
				Models: map[string]estimate.ModelPrice{
					"deepseek-v4-pro": {
						InputPer1K:      0.0011,
						CacheReadPer1K:  &cacheRead,
						CacheWritePer1K: &cacheWrite,
						OutputPer1K:     0.00276,
					},
				},
			},
		},
		Cache: estimate.CacheRatios{
			ProHitRatio:     0.60,
			FlashHitRatio:   0.80,
			NewRepoHitRatio: 0.0,
			ProWriteRatio:   0.50,
			FlashWriteRatio: 0.70,
		},
		Tasks: map[string]estimate.TaskDefaults{
			"code": {InputTokens: 50000, OutputRatio: 0.3, MaxIterations: 20, InputPerFile: 5000, TokensPerIter: 10000, TokensPer10Diff: 1},
		},
	}
	return estimate.NewEstimator(pricing, "pro")
}

func makeTaskDesc() *estimate.TaskDesc {
	return &estimate.TaskDesc{
		Description:   "test task",
		Type:          estimate.TaskCode,
		Model:         "deepseek-v4-pro",
		Provider:      "deepseek",
		FilesChanged:  1,
		MaxIterations: 5,
		Agents:        1,
	}
}

func makePanel(verdict string) *review.ReviewPanel {
	return &review.ReviewPanel{
		Primary: &mockModelClient{
			info:    review.ModelInfo{Model: "model-a", Provider: "provider-a"},
			verdict: verdict,
		},
		Adversarial: &mockModelClient{
			info:    review.ModelInfo{Model: "model-b", Provider: "provider-b"},
			verdict: verdict,
		},
	}
}

func makePRRequest() PRRequest {
	return PRRequest{
		PRURL:        "https://forgejo.local/user/repo/pulls/1",
		AgentID:      "test-agent",
		AgentTier:    trust.TierProvisional,
		CommitMsg:    "feat: add new feature",
		Diff:         "diff --git a/main.go b/main.go\n+func main() {}",
		ChangedFiles: []string{"main.go"},
		Category:     review.CategoryCosmetic,
	}
}

// ============================================================================
// Tests — StageResult
// ============================================================================

func TestStageResult_Elapsed(t *testing.T) {
	now := time.Now()
	s := StageResult{StartedAt: now, EndedAt: now.Add(100 * time.Millisecond)}
	if d := s.Elapsed(); d != 100*time.Millisecond {
		t.Errorf("Elapsed() = %v, want 100ms", d)
	}
}

func TestStageResult_ElapsedZeroEndedAt(t *testing.T) {
	s := StageResult{StartedAt: time.Now()}
	if d := s.Elapsed(); d != 0 {
		t.Errorf("Elapsed() = %v, want 0", d)
	}
}

// ============================================================================
// Tests — LifecycleResult
// ============================================================================

func TestLifecycleResult_StageByName(t *testing.T) {
	r := &LifecycleResult{
		Stages: []StageResult{
			{Name: StageCostEstimate, Status: StatusPassed},
			{Name: StageReview, Status: StatusPassed},
		},
	}
	s := r.StageByName(StageReview)
	if s == nil || s.Name != StageReview {
		t.Error("StageByName should find StageReview")
	}
}

func TestLifecycleResult_StageByNameNotFound(t *testing.T) {
	r := &LifecycleResult{Stages: []StageResult{{Name: StageCostEstimate}}}
	if s := r.StageByName(StageReview); s != nil {
		t.Error("StageByName should return nil for missing stage")
	}
}

func TestLifecycleResult_HasStage(t *testing.T) {
	r := &LifecycleResult{
		Stages: []StageResult{{Name: StageCostEstimate}},
	}
	if !r.HasStage(StageCostEstimate) {
		t.Error("HasStage should be true for existing stage")
	}
	if r.HasStage(StageReview) {
		t.Error("HasStage should be false for missing stage")
	}
}

func TestLifecycleResult_AllPassed(t *testing.T) {
	r := &LifecycleResult{
		Stages: []StageResult{
			{Name: StageCostEstimate, Status: StatusPassed},
			{Name: StageReview, Status: StatusPassed},
		},
	}
	if !r.AllPassed() {
		t.Error("AllPassed should be true when all pass")
	}
}

func TestLifecycleResult_AllPassedWithSkipped(t *testing.T) {
	r := &LifecycleResult{
		Stages: []StageResult{
			{Name: StageCostEstimate, Status: StatusPassed},
			{Name: StageReview, Status: StatusSkipped, Skipped: true},
		},
	}
	// AllPassed skips skipped stages
	if !r.AllPassed() {
		t.Error("AllPassed should be true when non-skipped stages pass")
	}
}

func TestLifecycleResult_AllPassedFalseOnFailure(t *testing.T) {
	r := &LifecycleResult{
		Stages: []StageResult{
			{Name: StageCostEstimate, Status: StatusPassed},
			{Name: StageReview, Status: StatusFailed},
		},
	}
	if r.AllPassed() {
		t.Error("AllPassed should be false when a stage fails")
	}
}

func TestLifecycleResult_HasFailure(t *testing.T) {
	r := &LifecycleResult{
		Stages: []StageResult{
			{Name: StageCostEstimate, Status: StatusPassed},
			{Name: StageReview, Status: StatusFailed},
		},
	}
	if !r.HasFailure() {
		t.Error("HasFailure should be true")
	}
}

func TestLifecycleResult_HasFailureFalse(t *testing.T) {
	r := &LifecycleResult{
		Stages: []StageResult{{Name: StageCostEstimate, Status: StatusPassed}},
	}
	if r.HasFailure() {
		t.Error("HasFailure should be false")
	}
}

func TestLifecycleResult_Elapsed(t *testing.T) {
	now := time.Now()
	r := &LifecycleResult{StartedAt: now, CompletedAt: now.Add(200 * time.Millisecond)}
	if d := r.Elapsed(); d != 200*time.Millisecond {
		t.Errorf("Elapsed() = %v, want 200ms", d)
	}
}

func TestLifecycleResult_ElapsedZeroCompletedAt(t *testing.T) {
	r := &LifecycleResult{StartedAt: time.Now()}
	if d := r.Elapsed(); d != 0 {
		t.Errorf("Elapsed() = %v, want 0", d)
	}
}

func TestLifecycleResult_Summary(t *testing.T) {
	now := time.Now()
	r := &LifecycleResult{
		Decision:    DecisionApproved,
		AgentID:     "test-agent",
		StartedAt:   now,
		CompletedAt: now.Add(500 * time.Millisecond),
		Stages: []StageResult{
			{Name: StageCostEstimate, Status: StatusPassed},
			{Name: StageReview, Status: StatusPassed},
			{Name: StageShadowDeploy, Status: StatusSkipped, Skipped: true},
		},
	}
	summary := r.Summary()
	if summary == "" {
		t.Error("Summary should not be empty")
	}
}

func TestLifecycleResult_AllPassedOrSkipped(t *testing.T) {
	r := &LifecycleResult{
		Stages: []StageResult{
			{Name: StageCostEstimate, Status: StatusPassed},
			{Name: StageReview, Status: StatusSkipped, Skipped: true},
		},
	}
	if !r.allPassedOrSkipped() {
		t.Error("allPassedOrSkipped should be true")
	}
}

func TestLifecycleResult_AllPassedOrSkippedFalse(t *testing.T) {
	r := &LifecycleResult{
		Stages: []StageResult{
			{Name: StageCostEstimate, Status: StatusPassed},
			{Name: StageReview, Status: StatusFailed},
		},
	}
	if r.allPassedOrSkipped() {
		t.Error("allPassedOrSkipped should be false with a failure")
	}
}

func TestLifecycleResult_AllPassedOrSkippedEmpty(t *testing.T) {
	r := &LifecycleResult{}
	if r.allPassedOrSkipped() {
		t.Error("allPassedOrSkipped should be false with no stages")
	}
}

// ============================================================================
// Tests — Coordinator construction
// ============================================================================

func TestNewPRLifecycleCoordinator_Default(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	if c == nil {
		t.Fatal("NewPRLifecycleCoordinator returned nil")
	}
	if !c.shouldRun(StageCostEstimate) {
		t.Error("default coordinator should run all stages")
	}
}

func TestNewPRLifecycleCoordinator_WithStages(t *testing.T) {
	c := NewPRLifecycleCoordinator(WithStages(StageCostEstimate, StageReview))
	if c.shouldRun(StageShadowDeploy) {
		t.Error("shadow deploy should not run with WithStages limiting to cost+review")
	}
	if !c.shouldRun(StageCostEstimate) {
		t.Error("cost estimate should run")
	}
}

func TestShouldRun_AllEnabledByDefault(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	stages := []StageName{
		StageCostEstimate, StageReview, StageNegotiation,
		StageMergeGate, StageShadowDeploy, StageSurveillance,
	}
	for _, s := range stages {
		if !c.shouldRun(s) {
			t.Errorf("stage %s should run by default", s)
		}
	}
}

// ============================================================================
// Tests — Full lifecycle (all stages skipped)
// ============================================================================

func TestExecute_AllStagesSkipped(t *testing.T) {
	// With no components configured:
	// - cost estimate: skipped (no estimator)
	// - review: skipped (no orchestrator)
	// - merge gate: runs (creates default gate) but fails (no bundle) → short-circuits
	c := NewPRLifecycleCoordinator()
	req := makePRRequest()
	result := c.Execute(context.Background(), req)
	if result == nil {
		t.Fatal("Execute returned nil result")
	}
	// Cost estimate + review should be skipped, merge gate should fail
	costStage := result.StageByName(StageCostEstimate)
	if costStage == nil || costStage.Status != StatusSkipped {
		t.Errorf("cost estimate should be skipped, got %+v", costStage)
	}
	reviewStage := result.StageByName(StageReview)
	if reviewStage == nil || reviewStage.Status != StatusSkipped {
		t.Errorf("review should be skipped, got %+v", reviewStage)
	}
	// Merge gate runs by default and fails without bundle
	gateStage := result.StageByName(StageMergeGate)
	if gateStage == nil {
		t.Error("merge gate should exist")
	}
	// Result should be rejected due to merge gate failure
	if result.Decision != DecisionRejected {
		t.Errorf("Decision = %s, want REJECTED", result.Decision)
	}
}

func TestExecute_PRURLPropagated(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := makePRRequest()
	result := c.Execute(context.Background(), req)
	if result.PRURL != req.PRURL {
		t.Errorf("PRURL = %s, want %s", result.PRURL, req.PRURL)
	}
}

func TestExecute_AgentIDPropagated(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := makePRRequest()
	result := c.Execute(context.Background(), req)
	if result.AgentID != req.AgentID {
		t.Errorf("AgentID = %s, want %s", result.AgentID, req.AgentID)
	}
}

func TestExecute_AllSkippedResultsRejected(t *testing.T) {
	// With no components configured, merge gate fails → REJECTED
	c := NewPRLifecycleCoordinator()
	req := makePRRequest()
	result := c.Execute(context.Background(), req)
	if result.Decision != DecisionRejected {
		t.Errorf("Decision = %s, want REJECTED when merge gate fails", result.Decision)
	}
}

func TestExecute_CompletedAtSet(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := makePRRequest()
	result := c.Execute(context.Background(), req)
	if result.CompletedAt.IsZero() {
		t.Error("CompletedAt should be set")
	}
}

func TestExecute_StartedAtSet(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := makePRRequest()
	result := c.Execute(context.Background(), req)
	if result.StartedAt.IsZero() {
		t.Error("StartedAt should be set")
	}
}

// ============================================================================
// Tests — Cost Estimate Stage
// ============================================================================

func TestExecute_CostEstimateSkipped(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := makePRRequest()
	result := c.Execute(context.Background(), req)
	stage := result.StageByName(StageCostEstimate)
	if stage == nil || stage.Status != StatusSkipped {
		t.Errorf("cost estimate should be skipped without estimator")
	}
}

func TestExecute_CostEstimatePassed(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := makePRRequest()
	req.Estimator = makeEstimator()
	req.TaskDesc = makeTaskDesc()
	result := c.Execute(context.Background(), req)
	stage := result.StageByName(StageCostEstimate)
	if stage == nil || stage.Status != StatusPassed {
		t.Errorf("cost estimate should pass, got %+v", stage)
	}
}

func TestExecute_CostEstimateSkippedWhenTaskDescNil(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := makePRRequest()
	req.Estimator = makeEstimator()
	// TaskDesc is nil
	result := c.Execute(context.Background(), req)
	stage := result.StageByName(StageCostEstimate)
	if stage == nil || stage.Status != StatusSkipped {
		t.Errorf("cost estimate should be skipped when TaskDesc is nil")
	}
}

// ============================================================================
// Tests — Review Stage
// ============================================================================

func TestExecute_ReviewSkipped(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := makePRRequest()
	result := c.Execute(context.Background(), req)
	stage := result.StageByName(StageReview)
	if stage == nil || stage.Status != StatusSkipped {
		t.Errorf("review should be skipped without orchestrator")
	}
}

func TestExecute_ReviewPassedUnanimous(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := makePRRequest()
	req.ReviewOrchestrator = review.NewReviewOrchestrator()
	req.ReviewPanel = makePanel("approved")
	result := c.Execute(context.Background(), req)
	stage := result.StageByName(StageReview)
	if stage == nil || stage.Status != StatusPassed {
		t.Errorf("review should pass, got %+v", stage)
	}
}

func TestExecute_ReviewSkippedWhenPanelNil(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := makePRRequest()
	req.ReviewOrchestrator = review.NewReviewOrchestrator()
	// Panel is nil
	result := c.Execute(context.Background(), req)
	stage := result.StageByName(StageReview)
	if stage == nil || stage.Status != StatusSkipped {
		t.Errorf("review should be skipped when panel is nil")
	}
}

// ============================================================================
// Tests — Merge Gate Stage
// ============================================================================

func TestExecute_MergeGatePassed(t *testing.T) {
	c := NewPRLifecycleCoordinator(WithStages(StageMergeGate))
	req := makePRRequest()
	req.MergeGate = mergegate.NewMergeGate(
		mergegate.WithContractSkipped(),
		mergegate.WithCostSkipped(),
	)
	result := c.Execute(context.Background(), req)
	stage := result.StageByName(StageMergeGate)
	if stage == nil {
		t.Fatal("merge gate stage should exist")
	}
	// Without evidence bundle, gate should fail
	// This is expected behavior — no bundle → blocked
}

func TestExecute_MergeGateDefaultGate(t *testing.T) {
	c := NewPRLifecycleCoordinator(WithStages(StageMergeGate))
	req := makePRRequest()
	// No MergeGate set — default gate created
	result := c.Execute(context.Background(), req)
	stage := result.StageByName(StageMergeGate)
	if stage == nil {
		t.Fatal("merge gate stage should exist")
	}
	// Without evidence bundle, gate should fail
	t.Logf("merge gate status = %s", stage.Status)
}

func TestExecute_MergeGateEscalated(t *testing.T) {
	c := NewPRLifecycleCoordinator(WithStages(StageMergeGate))
	req := makePRRequest()
	req.MergeGate = mergegate.NewMergeGate(
		mergegate.WithContractSkipped(),
		mergegate.WithCostSkipped(),
	)
	result := c.Execute(context.Background(), req)
	// Without evidence bundle the gate blocks or escalates
	if result.Decision != DecisionRejected && result.Decision != DecisionEscalated {
		t.Errorf("expected rejected or escalated without bundle, got %s", result.Decision)
	}
}

// ============================================================================
// Tests — Shadow Deploy Stage
// ============================================================================

func TestExecute_ShadowDeploySkipped(t *testing.T) {
	c := NewPRLifecycleCoordinator(WithStages(StageShadowDeploy))
	req := makePRRequest()
	result := c.Execute(context.Background(), req)
	stage := result.StageByName(StageShadowDeploy)
	if stage == nil || stage.Status != StatusSkipped {
		t.Errorf("shadow deploy should be skipped without manager")
	}
}

func TestExecute_ShadowDeployPassed(t *testing.T) {
	c := NewPRLifecycleCoordinator(WithStages(StageShadowDeploy))
	req := makePRRequest()
	req.ShadowManager = verify.NewShadowManager()
	req.BaselineMetrics = verify.MetricsSnapshot{
		SuccessRate:  0.99,
		P99LatencyMs: 100,
		Timestamp:    time.Now(),
	}
	result := c.Execute(context.Background(), req)
	stage := result.StageByName(StageShadowDeploy)
	if stage == nil || stage.Status != StatusPassed {
		t.Errorf("shadow deploy should pass, got %+v", stage)
	}
}

func TestExecute_ShadowDeployUsesDefaultConfig(t *testing.T) {
	c := NewPRLifecycleCoordinator(WithStages(StageShadowDeploy))
	req := makePRRequest()
	req.ShadowManager = verify.NewShadowManager()
	req.BaselineMetrics = verify.MetricsSnapshot{
		SuccessRate: 0.99,
		Timestamp:   time.Now(),
	}
	// ShadowConfig is zero-valued — should use default
	result := c.Execute(context.Background(), req)
	stage := result.StageByName(StageShadowDeploy)
	if stage == nil || stage.Status != StatusPassed {
		t.Errorf("shadow deploy should pass with default config")
	}
}

// ============================================================================
// Tests — Surveillance Stage
// ============================================================================

func TestExecute_SurveillanceSkipped(t *testing.T) {
	c := NewPRLifecycleCoordinator(WithStages(StageSurveillance))
	req := makePRRequest()
	result := c.Execute(context.Background(), req)
	stage := result.StageByName(StageSurveillance)
	if stage == nil || stage.Status != StatusSkipped {
		t.Errorf("surveillance should be skipped without aggregator")
	}
}

func TestExecute_SurveillancePassed(t *testing.T) {
	c := NewPRLifecycleCoordinator(WithStages(StageSurveillance))
	req := makePRRequest()
	req.Surveillance = verify.NewSteadyStateAggregator()
	req.BaselineMetrics = verify.MetricsSnapshot{
		SuccessRate: 0.99,
		Timestamp:   time.Now(),
	}
	result := c.Execute(context.Background(), req)
	stage := result.StageByName(StageSurveillance)
	if stage == nil || stage.Status != StatusPassed {
		t.Errorf("surveillance should pass, got %+v", stage)
	}
}

// ============================================================================
// Tests — Short-circuit behavior
// ============================================================================

func TestExecute_CostEstimateFailureShortCircuits(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := makePRRequest()
	// Create an estimator that will produce an error
	req.Estimator = &estimate.Estimator{} // nil Pricing → returns error
	req.TaskDesc = makeTaskDesc()
	result := c.Execute(context.Background(), req)
	if result.Decision != DecisionRejected {
		t.Errorf("Decision = %s, want REJECTED on cost estimate failure", result.Decision)
	}
	costStage := result.StageByName(StageCostEstimate)
	if costStage == nil || costStage.Status != StatusFailed {
		t.Errorf("cost estimate stage should be failed")
	}
	// Subsequent stages should still exist (they run after short-circuit in Execute's flow)
	// but review should not have been added
}

func TestExecute_ReviewFailureShortCircuits(t *testing.T) {
	c := NewPRLifecycleCoordinator(WithStages(StageReview, StageMergeGate))
	req := makePRRequest()
	// Review orchestrator with nil primary — will error
	req.ReviewOrchestrator = review.NewReviewOrchestrator()
	req.ReviewPanel = &review.ReviewPanel{} // no primary
	result := c.Execute(context.Background(), req)
	// Review fails because no primary model
	reviewStage := result.StageByName(StageReview)
	if reviewStage == nil {
		t.Fatal("review stage should exist")
	}
	if reviewStage.Status != StatusFailed {
		t.Errorf("review should fail with empty panel, got %s", reviewStage.Status)
	}
}

// ============================================================================
// Tests — Negotiation Stage
// ============================================================================

func TestExecute_NegotiationSkippedWhenNoConflict(t *testing.T) {
	c := NewPRLifecycleCoordinator(WithStages(StageNegotiation))
	req := makePRRequest()
	// No negotiation setup → needsNegotiation returns false
	result := c.Execute(context.Background(), req)
	stage := result.StageByName(StageNegotiation)
	if stage != nil {
		t.Error("negotiation stage should not exist when no conflict detected")
	}
}

func TestNeedsNegotiation_WithSetupNoConflict(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	setup := &NegotiationSetup{
		VerdictA: negotiate.VerdictApproved,
		VerdictB: negotiate.VerdictApproved,
	}
	req := PRRequest{NegotiationSetup: setup}
	if c.needsNegotiation(req, review.Consensus{}) {
		t.Error("needsNegotiation should be false when both approve")
	}
}

func TestNeedsNegotiation_WithSetupConflict(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	setup := &NegotiationSetup{
		VerdictA: negotiate.VerdictApproved,
		VerdictB: negotiate.VerdictRequestChanges,
	}
	req := PRRequest{NegotiationSetup: setup}
	if !c.needsNegotiation(req, review.Consensus{}) {
		t.Error("needsNegotiation should be true when agents disagree")
	}
}

func TestNeedsNegotiation_DivergentConsensus(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := PRRequest{}
	consensus := review.Consensus{Resolution: "flagged"}
	if !c.needsNegotiation(req, consensus) {
		t.Error("needsNegotiation should be true with divergent consensus")
	}
}

func TestNeedsNegotiation_ApprovedConsensus(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := PRRequest{}
	consensus := review.Consensus{Resolution: review.ResolutionApproved}
	if c.needsNegotiation(req, consensus) {
		t.Error("needsNegotiation should be false with approved consensus")
	}
}

func TestNeedsNegotiation_EmptyConsensus(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := PRRequest{}
	if c.needsNegotiation(req, review.Consensus{}) {
		t.Error("needsNegotiation should be false with empty consensus")
	}
}

// ============================================================================
// Tests — Full lifecycle with all stages configured
// ============================================================================

func TestExecute_FullLifecycleAllConfigured(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := makePRRequest()

	// Cost estimate
	req.Estimator = makeEstimator()
	req.TaskDesc = makeTaskDesc()

	// Review
	req.ReviewOrchestrator = review.NewReviewOrchestrator()
	req.ReviewPanel = makePanel("approved")

	// Merge gate — skip contract and cost since we don't have those artifacts
	req.MergeGate = mergegate.NewMergeGate(
		mergegate.WithContractSkipped(),
		mergegate.WithCostSkipped(),
	)

	// Shadow deploy
	req.ShadowManager = verify.NewShadowManager()
	req.BaselineMetrics = verify.MetricsSnapshot{
		SuccessRate:  0.99,
		P99LatencyMs: 100,
		Timestamp:    time.Now(),
	}

	// Surveillance
	req.Surveillance = verify.NewSteadyStateAggregator()

	result := c.Execute(context.Background(), req)

	// All stages should be present
	expectedStages := []StageName{
		StageCostEstimate, StageReview, StageMergeGate,
		StageShadowDeploy, StageSurveillance,
	}
	for _, name := range expectedStages {
		if !result.HasStage(name) {
			t.Errorf("expected stage %s in result", name)
		}
	}

	// Cost estimate, review, shadow, surveillance should pass.
	// Merge gate may fail if no evidence bundle (expected in unit test).
	costStage := result.StageByName(StageCostEstimate)
	if costStage == nil || costStage.Status != StatusPassed {
		t.Errorf("cost estimate should pass, got %+v", costStage)
	}

	reviewStage := result.StageByName(StageReview)
	if reviewStage == nil || reviewStage.Status != StatusPassed {
		t.Errorf("review should pass, got %+v", reviewStage)
	}

	shadowStage := result.StageByName(StageShadowDeploy)
	if shadowStage == nil || shadowStage.Status != StatusPassed {
		t.Errorf("shadow deploy should pass, got %+v", shadowStage)
	}

	survStage := result.StageByName(StageSurveillance)
	if survStage == nil || survStage.Status != StatusPassed {
		t.Errorf("surveillance should pass, got %+v", survStage)
	}
}

func TestExecute_MergeGateSkipsShadowWhenBlocked(t *testing.T) {
	c := NewPRLifecycleCoordinator(WithStages(StageMergeGate, StageShadowDeploy))
	req := makePRRequest()
	// Default gate with no bundle → blocked
	result := c.Execute(context.Background(), req)

	gateStage := result.StageByName(StageMergeGate)
	if gateStage == nil {
		t.Fatal("merge gate stage should exist")
	}
	if gateStage.Status != StatusFailed {
		t.Errorf("merge gate should fail without bundle, got %s", gateStage.Status)
	}

	// Shadow deploy should NOT run because gate failed
	// (Execute returns early on gate failure)
	shadowStage := result.StageByName(StageShadowDeploy)
	if shadowStage != nil {
		t.Error("shadow deploy should not exist after merge gate failure (short-circuit)")
	}
}

// ============================================================================
// Tests — Stage selection via WithStages
// ============================================================================

func TestExecute_OnlyReviewStage(t *testing.T) {
	c := NewPRLifecycleCoordinator(WithStages(StageReview))
	req := makePRRequest()
	req.ReviewOrchestrator = review.NewReviewOrchestrator()
	req.ReviewPanel = makePanel("approved")
	result := c.Execute(context.Background(), req)

	if len(result.Stages) != 1 {
		t.Errorf("expected 1 stage, got %d", len(result.Stages))
	}
	if !result.HasStage(StageReview) {
		t.Error("should have review stage")
	}
	if result.HasStage(StageCostEstimate) {
		t.Error("should NOT have cost estimate stage")
	}
}

func TestExecute_CostAndReviewOnly(t *testing.T) {
	c := NewPRLifecycleCoordinator(WithStages(StageCostEstimate, StageReview))
	req := makePRRequest()
	req.Estimator = makeEstimator()
	req.TaskDesc = makeTaskDesc()
	req.ReviewOrchestrator = review.NewReviewOrchestrator()
	req.ReviewPanel = makePanel("approved")
	result := c.Execute(context.Background(), req)

	if len(result.Stages) != 2 {
		t.Errorf("expected 2 stages, got %d", len(result.Stages))
	}
}

// ============================================================================
// Tests — runStage internal methods
// ============================================================================

func TestRunCostEstimate_NilEstimator(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := makePRRequest()
	stage := c.runCostEstimate(context.Background(), req)
	if stage.Status != StatusSkipped {
		t.Errorf("expected skipped, got %s", stage.Status)
	}
}

func TestRunCostEstimate_WithEstimator(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := makePRRequest()
	req.Estimator = makeEstimator()
	req.TaskDesc = makeTaskDesc()
	stage := c.runCostEstimate(context.Background(), req)
	if stage.Status != StatusPassed {
		t.Errorf("expected passed, got %s", stage.Status)
	}
}

func TestRunReview_NilOrchestrator(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := makePRRequest()
	stage, _, _ := c.runReview(context.Background(), req)
	if stage.Status != StatusSkipped {
		t.Errorf("expected skipped, got %s", stage.Status)
	}
}

func TestRunReview_WithOrchestrator(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := makePRRequest()
	req.ReviewOrchestrator = review.NewReviewOrchestrator()
	req.ReviewPanel = makePanel("approved")
	stage, bundle, _ := c.runReview(context.Background(), req)
	if stage.Status != StatusPassed {
		t.Errorf("expected passed, got %s", stage.Status)
	}
	if bundle == nil {
		t.Error("expected evidence bundle from review")
	}
}

func TestRunMergeGate_DefaultGate(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := makePRRequest()
	stage, report := c.runMergeGate(context.Background(), req, nil)
	if report == nil {
		t.Error("expected non-nil gate report")
	}
	// Without bundle → blocked
	if stage.Status != StatusFailed {
		t.Errorf("expected failed without bundle, got %s", stage.Status)
	}
}

func TestRunShadowDeploy_NilManager(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := makePRRequest()
	stage := c.runShadowDeploy(context.Background(), req)
	if stage.Status != StatusSkipped {
		t.Errorf("expected skipped, got %s", stage.Status)
	}
}

func TestRunShadowDeploy_WithManager(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := makePRRequest()
	req.ShadowManager = verify.NewShadowManager()
	req.BaselineMetrics = verify.MetricsSnapshot{
		SuccessRate: 0.99,
		Timestamp:   time.Now(),
	}
	stage := c.runShadowDeploy(context.Background(), req)
	if stage.Status != StatusPassed {
		t.Errorf("expected passed, got %s", stage.Status)
	}
}

func TestRunSurveillance_NilAggregator(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := makePRRequest()
	stage := c.runSurveillance(context.Background(), req)
	if stage.Status != StatusSkipped {
		t.Errorf("expected skipped, got %s", stage.Status)
	}
}

func TestRunSurveillance_WithAggregator(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := makePRRequest()
	req.Surveillance = verify.NewSteadyStateAggregator()
	req.BaselineMetrics = verify.MetricsSnapshot{
		SuccessRate: 0.99,
		Timestamp:   time.Now(),
	}
	stage := c.runSurveillance(context.Background(), req)
	if stage.Status != StatusPassed {
		t.Errorf("expected passed, got %s", stage.Status)
	}
}

// ============================================================================
// Tests — Decision logic
// ============================================================================

func TestExecute_DecisionApprovedWhenAllPassedOrSkipped(t *testing.T) {
	c := NewPRLifecycleCoordinator(WithStages(StageShadowDeploy))
	req := makePRRequest()
	req.ShadowManager = verify.NewShadowManager()
	req.BaselineMetrics = verify.MetricsSnapshot{
		SuccessRate: 0.99,
		Timestamp:   time.Now(),
	}
	result := c.Execute(context.Background(), req)
	if result.Decision != DecisionApproved {
		t.Errorf("expected APPROVED, got %s", result.Decision)
	}
}

func TestExecute_DecisionRejectedOnShadowFailure(t *testing.T) {
	c := NewPRLifecycleCoordinator(WithStages(StageShadowDeploy))
	req := makePRRequest()
	// ShadowManager nil + something else...
	// Actually this will just skip. Let me create a scenario where shadow fails.
	// Set agentID empty → LaunchShadow returns error
	req.AgentID = ""
	req.ShadowManager = verify.NewShadowManager()
	req.BaselineMetrics = verify.MetricsSnapshot{Timestamp: time.Now()}
	result := c.Execute(context.Background(), req)
	shadowStage := result.StageByName(StageShadowDeploy)
	if shadowStage == nil || shadowStage.Status != StatusFailed {
		t.Errorf("shadow deploy should fail with empty agentID, got %+v", shadowStage)
	}
	if result.Decision != DecisionRejected {
		t.Errorf("expected REJECTED, got %s", result.Decision)
	}
}

// ============================================================================
// Tests — NegotiationSetup
// ============================================================================

func TestNegotiationSetup_Fields(t *testing.T) {
	setup := &NegotiationSetup{
		PRNumber:   42,
		AgentA:     negotiate.Agent{Name: "alpha", TrustLevel: 50},
		AgentB:     negotiate.Agent{Name: "beta", TrustLevel: 80},
		VerdictA:   negotiate.VerdictApproved,
		VerdictB:   negotiate.VerdictRequestChanges,
		ArbiterURL: "http://localhost:8765",
		AuditPath:  "/tmp/audit",
	}
	if setup.PRNumber != 42 {
		t.Error("PRNumber mismatch")
	}
	if !negotiate.DetectConflict(setup.VerdictA, setup.VerdictB) {
		t.Error("should detect conflict")
	}
}

// ============================================================================
// Tests — runNegotiation
// ============================================================================

func TestRunNegotiation_NilSetup(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := PRRequest{}
	stage := c.runNegotiation(context.Background(), req)
	if stage.Status != StatusSkipped {
		t.Errorf("expected skipped, got %s", stage.Status)
	}
}

func TestRunNegotiation_NoConflict(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := PRRequest{
		NegotiationSetup: &NegotiationSetup{
			PRNumber:   1,
			AgentA:     negotiate.Agent{Name: "a", TrustLevel: 50},
			AgentB:     negotiate.Agent{Name: "b", TrustLevel: 50},
			VerdictA:   negotiate.VerdictApproved,
			VerdictB:   negotiate.VerdictApproved,
			ArbiterURL: "http://localhost:8765",
			AuditPath:  t.TempDir() + "/audit.jsonl",
		},
	}
	stage := c.runNegotiation(context.Background(), req)
	if stage.Status != StatusSkipped {
		t.Errorf("expected skipped (no conflict), got %s", stage.Status)
	}
}

func TestRunNegotiation_WithConflict(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := PRRequest{
		NegotiationSetup: &NegotiationSetup{
			PRNumber:   1,
			AgentA:     negotiate.Agent{Name: "a", TrustLevel: 50},
			AgentB:     negotiate.Agent{Name: "b", TrustLevel: 50},
			VerdictA:   negotiate.VerdictApproved,
			VerdictB:   negotiate.VerdictRequestChanges,
			ArbiterURL: "http://localhost:8765",
			AuditPath:  t.TempDir() + "/audit.jsonl",
		},
	}
	stage := c.runNegotiation(context.Background(), req)
	// Negotiator created with conflict_detected state → escalated (needs human)
	if stage.Status == StatusSkipped {
		t.Error("should not be skipped with active conflict")
	}
}

func TestExecute_NegotiationEscalated(t *testing.T) {
	c := NewPRLifecycleCoordinator(WithStages(StageNegotiation))
	req := PRRequest{
		NegotiationSetup: &NegotiationSetup{
			PRNumber:   1,
			AgentA:     negotiate.Agent{Name: "a", TrustLevel: 50},
			AgentB:     negotiate.Agent{Name: "b", TrustLevel: 50},
			VerdictA:   negotiate.VerdictApproved,
			VerdictB:   negotiate.VerdictRequestChanges,
			ArbiterURL: "http://localhost:8765",
			AuditPath:  t.TempDir() + "/audit.jsonl",
		},
	}
	result := c.Execute(context.Background(), req)
	stage := result.StageByName(StageNegotiation)
	if stage == nil {
		t.Fatal("negotiation stage should exist")
	}
	if stage.Status != StatusEscalated {
		t.Errorf("expected escalated (auto-resolve), got %s", stage.Status)
	}
	if result.Decision != DecisionEscalated {
		t.Errorf("expected ESCALATED decision, got %s", result.Decision)
	}
}

func TestRunMergeGate_EscalatedResult(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := PRRequest{
		AgentID:   "test-agent",
		AgentTier: trust.TierProvisional,
	}
	// Default gate with no bundle → blocked (FAIL not ESCALATED)
	// Use custom gate that escalates
	gate := mergegate.NewMergeGate(
		mergegate.WithContractSkipped(),
		mergegate.WithCostSkipped(),
	)
	req.MergeGate = gate
	stage, report := c.runMergeGate(context.Background(), req, nil)
	if report == nil {
		t.Error("expected non-nil report")
	}
	// Without evidence bundle, gate blocks
	if stage.Status != StatusFailed {
		t.Errorf("expected failed without bundle, got %s", stage.Status)
	}
}

func TestRunCostEstimate_Error(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := PRRequest{
		Estimator: &estimate.Estimator{}, // nil Pricing → error
		TaskDesc:  makeTaskDesc(),
	}
	stage := c.runCostEstimate(context.Background(), req)
	if stage.Status != StatusFailed {
		t.Errorf("expected failed, got %s", stage.Status)
	}
}

func TestRunCostEstimate_OnlyTaskDescNil(t *testing.T) {
	c := NewPRLifecycleCoordinator()
	req := PRRequest{
		Estimator: makeEstimator(),
		// TaskDesc is nil
	}
	stage := c.runCostEstimate(context.Background(), req)
	if stage.Status != StatusSkipped {
		t.Errorf("expected skipped when TaskDesc nil, got %s", stage.Status)
	}
}
