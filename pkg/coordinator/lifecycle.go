// Package coordinator orchestrates the full PR lifecycle by composing
// every Helix subsystem in the correct sequence.
//
// Per specs/cross-component-wiring.md, when a PR is opened the platform must:
//
//  1. Pre-flight cost estimate (pkg/estimate)
//  2. Multi-model adversarial review (pkg/review)
//  3. PR negotiation if agents disagree (pkg/negotiate)
//  4. Merge gate validation (pkg/mergegate)
//  5. Shadow deployment if merge approved (pkg/verify)
//  6. Steady-state surveillance begins (pkg/verify)
//
// The coordinator holds references to each subsystem and calls them
// in sequence. Each stage can fail independently without crashing
// the pipeline — the failure is recorded in the lifecycle result.
package coordinator

import (
	"context"
	"fmt"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/estimate"
	"github.com/totalwindupflightsystems/helix/pkg/mergegate"
	"github.com/totalwindupflightsystems/helix/pkg/negotiate"
	"github.com/totalwindupflightsystems/helix/pkg/review"
	"github.com/totalwindupflightsystems/helix/pkg/trust"
	"github.com/totalwindupflightsystems/helix/pkg/verify"
)

// ============================================================================
// Stage Status
// ============================================================================

// StageName identifies a lifecycle stage.
type StageName string

const (
	StageCostEstimate StageName = "cost_estimate"
	StageReview       StageName = "review"
	StageNegotiation  StageName = "negotiation"
	StageMergeGate    StageName = "merge_gate"
	StageShadowDeploy StageName = "shadow_deploy"
	StageSurveillance StageName = "surveillance"
)

// StageStatus is the execution status of a lifecycle stage.
type StageStatus string

const (
	StatusPending   StageStatus = "pending"
	StatusRunning   StageStatus = "running"
	StatusPassed    StageStatus = "passed"
	StatusFailed    StageStatus = "failed"
	StatusSkipped   StageStatus = "skipped"
	StatusEscalated StageStatus = "escalated"
)

// StageResult captures the outcome of one lifecycle stage.
type StageResult struct {
	Name      StageName   `json:"name"`
	Status    StageStatus `json:"status"`
	Duration  string      `json:"duration,omitempty"`
	Message   string      `json:"message,omitempty"`
	Error     string      `json:"error,omitempty"`
	Skipped   bool        `json:"skipped,omitempty"`
	StartedAt time.Time   `json:"started_at"`
	EndedAt   time.Time   `json:"ended_at,omitempty"`
}

// Elapsed returns the wall-clock duration of the stage.
func (s StageResult) Elapsed() time.Duration {
	if s.EndedAt.IsZero() {
		return 0
	}
	return s.EndedAt.Sub(s.StartedAt)
}

// ============================================================================
// Lifecycle Result
// ============================================================================

// LifecycleDecision is the overall PR lifecycle decision.
type LifecycleDecision string

const (
	DecisionApproved  LifecycleDecision = "APPROVED"
	DecisionRejected  LifecycleDecision = "REJECTED"
	DecisionEscalated LifecycleDecision = "ESCALATED"
	DecisionContested LifecycleDecision = "CONTESTED"
)

// LifecycleResult is the full outcome of a PR lifecycle run.
type LifecycleResult struct {
	PRURL       string            `json:"pr_url"`
	AgentID     string            `json:"agent_id"`
	Decision    LifecycleDecision `json:"decision"`
	Stages      []StageResult     `json:"stages"`
	StartedAt   time.Time         `json:"started_at"`
	CompletedAt time.Time         `json:"completed_at"`
}

// StageByName returns the stage result with the given name, or nil.
func (r *LifecycleResult) StageByName(name StageName) *StageResult {
	for i := range r.Stages {
		if r.Stages[i].Name == name {
			return &r.Stages[i]
		}
	}
	return nil
}

// HasStage returns true if a stage with the given name exists.
func (r *LifecycleResult) HasStage(name StageName) bool {
	return r.StageByName(name) != nil
}

// AllPassed returns true if every non-skipped stage passed.
func (r *LifecycleResult) AllPassed() bool {
	for i := range r.Stages {
		s := &r.Stages[i]
		if s.Skipped {
			continue
		}
		if s.Status != StatusPassed {
			return false
		}
	}
	return true
}

// HasFailure returns true if any stage failed.
func (r *LifecycleResult) HasFailure() bool {
	for i := range r.Stages {
		if r.Stages[i].Status == StatusFailed {
			return true
		}
	}
	return false
}

// Elapsed returns the total wall-clock duration of the lifecycle run.
func (r *LifecycleResult) Elapsed() time.Duration {
	if r.CompletedAt.IsZero() {
		return 0
	}
	return r.CompletedAt.Sub(r.StartedAt)
}

// Summary returns a human-readable one-line summary.
func (r *LifecycleResult) Summary() string {
	passed, failed, skipped := 0, 0, 0
	for i := range r.Stages {
		switch r.Stages[i].Status {
		case StatusPassed:
			passed++
		case StatusFailed:
			failed++
		case StatusSkipped:
			skipped++
		}
	}
	return fmt.Sprintf("decision=%s agent=%s stages: %d passed, %d failed, %d skipped (%.1fs)",
		r.Decision, r.AgentID, passed, failed, skipped, r.Elapsed().Seconds())
}

// ============================================================================
// PR Request — input to the coordinator
// ============================================================================

// PRRequest is the input describing a PR entering the lifecycle.
type PRRequest struct {
	PRURL        string          `json:"pr_url"`
	AgentID      string          `json:"agent_id"`
	AgentTier    trust.TrustTier `json:"agent_tier"`
	CommitMsg    string          `json:"commit_msg"`
	Diff         string          `json:"diff"`
	ChangedFiles []string        `json:"changed_files"`

	// Category classifies the change for review depth.
	Category review.ChangeCategory `json:"category"`

	// ReviewPanel is the set of models assigned to review the PR.
	// If nil, the review stage is skipped.
	ReviewPanel *review.ReviewPanel `json:"-"`

	// ReviewOrchestrator handles the adversarial review.
	// If nil, the review stage is skipped.
	ReviewOrchestrator *review.ReviewOrchestrator `json:"-"`

	// Estimator computes the pre-flight cost estimate.
	// If nil, the cost estimate stage is skipped.
	Estimator *estimate.Estimator `json:"-"`

	// TaskDesc describes the task for cost estimation.
	TaskDesc *estimate.TaskDesc `json:"-"`

	// NegotiationSetup configures negotiation if agents disagree.
	// If nil, contested PRs are escalated without negotiation.
	NegotiationSetup *NegotiationSetup `json:"-"`

	// MergeGate validates all preconditions.
	// If nil, a default gate is created.
	MergeGate *mergegate.MergeGate `json:"-"`

	// BehaviorContract attached to the PR (if any).
	BehaviorContract *verify.BehaviorContract `json:"-"`

	// ShadowManager handles shadow deployment.
	// If nil, the shadow deploy stage is skipped.
	ShadowManager *verify.ShadowManager `json:"-"`

	// BaselineMetrics for shadow comparison.
	BaselineMetrics verify.MetricsSnapshot `json:"-"`

	// ShadowConfig overrides default shadow thresholds.
	// If zero-valued, DefaultShadowConfig() is used.
	ShadowConfig verify.ShadowConfig `json:"-"`

	// Surveillance registers the agent for steady-state monitoring.
	// If nil, the surveillance stage is skipped.
	Surveillance *verify.SteadyStateAggregator `json:"-"`
}

// NegotiationSetup configures the negotiation stage.
type NegotiationSetup struct {
	PRNumber   int               `json:"pr_number"`
	AgentA     negotiate.Agent   `json:"agent_a"`
	AgentB     negotiate.Agent   `json:"agent_b"`
	VerdictA   negotiate.Verdict `json:"verdict_a"`
	VerdictB   negotiate.Verdict `json:"verdict_b"`
	ArbiterURL string            `json:"arbiter_url"`
	AuditPath  string            `json:"audit_path"`
}

// ============================================================================
// Coordinator
// ============================================================================

// PRLifecycleCoordinator orchestrates the full PR lifecycle.
type PRLifecycleCoordinator struct {
	// enabledStages controls which stages run. If empty, all stages run.
	enabledStages map[StageName]bool
}

// CoordinatorOption configures a PRLifecycleCoordinator.
type CoordinatorOption func(*PRLifecycleCoordinator)

// WithStages limits the coordinator to specific stages.
func WithStages(stages ...StageName) CoordinatorOption {
	return func(c *PRLifecycleCoordinator) {
		c.enabledStages = make(map[StageName]bool, len(stages))
		for _, s := range stages {
			c.enabledStages[s] = true
		}
	}
}

// NewPRLifecycleCoordinator creates a coordinator with default settings.
func NewPRLifecycleCoordinator(opts ...CoordinatorOption) *PRLifecycleCoordinator {
	c := &PRLifecycleCoordinator{}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// shouldRun checks whether a stage should run.
func (c *PRLifecycleCoordinator) shouldRun(stage StageName) bool {
	if len(c.enabledStages) == 0 {
		return true // all stages enabled
	}
	return c.enabledStages[stage]
}

// ============================================================================
// Execute — the main lifecycle entry point
// ============================================================================

// Execute runs the full PR lifecycle and returns the result.
// Each stage is called in sequence. A failed stage may cause
// subsequent stages to be skipped or the lifecycle to short-circuit.
func (c *PRLifecycleCoordinator) Execute(ctx context.Context, req PRRequest) *LifecycleResult {
	result := &LifecycleResult{
		PRURL:     req.PRURL,
		AgentID:   req.AgentID,
		StartedAt: time.Now(),
	}
	defer func() {
		result.CompletedAt = time.Now()
	}()

	// Stage 1: Cost estimate.
	if c.shouldRun(StageCostEstimate) {
		stage := c.runCostEstimate(ctx, req)
		result.Stages = append(result.Stages, stage)
		if stage.Status == StatusFailed {
			result.Decision = DecisionRejected
			return result
		}
	}

	// Stage 2: Adversarial review.
	var reviewBundle *review.EvidenceBundle
	var consensus review.Consensus
	if c.shouldRun(StageReview) {
		stage, bundle, cons := c.runReview(ctx, req)
		result.Stages = append(result.Stages, stage)
		reviewBundle = bundle
		consensus = cons
		if stage.Status == StatusFailed {
			result.Decision = DecisionRejected
			return result
		}
	}

	// Stage 3: Negotiation (only if contested).
	if c.shouldRun(StageNegotiation) && c.needsNegotiation(req, consensus) {
		stage := c.runNegotiation(ctx, req)
		result.Stages = append(result.Stages, stage)
		if stage.Status == StatusEscalated {
			result.Decision = DecisionEscalated
			return result
		}
		if stage.Status == StatusFailed {
			result.Decision = DecisionContested
			return result
		}
	}

	// Stage 4: Merge gate.
	if c.shouldRun(StageMergeGate) {
		stage, _ := c.runMergeGate(ctx, req, reviewBundle)
		result.Stages = append(result.Stages, stage)
		if stage.Status == StatusFailed {
			result.Decision = DecisionRejected
			return result
		}
		if stage.Status == StatusEscalated {
			result.Decision = DecisionEscalated
			return result
		}
	}

	// Stage 5: Shadow deployment.
	if c.shouldRun(StageShadowDeploy) {
		stage := c.runShadowDeploy(ctx, req)
		result.Stages = append(result.Stages, stage)
		if stage.Status == StatusFailed {
			result.Decision = DecisionRejected
			return result
		}
	}

	// Stage 6: Steady-state surveillance.
	if c.shouldRun(StageSurveillance) {
		stage := c.runSurveillance(ctx, req)
		result.Stages = append(result.Stages, stage)
	}

	// Determine final decision.
	if result.HasFailure() {
		result.Decision = DecisionRejected
	} else if result.AllPassed() || result.allPassedOrSkipped() {
		result.Decision = DecisionApproved
	} else {
		result.Decision = DecisionEscalated
	}

	return result
}

// allPassedOrSkipped returns true if all stages are passed or skipped.
func (r *LifecycleResult) allPassedOrSkipped() bool {
	if len(r.Stages) == 0 {
		return false
	}
	for i := range r.Stages {
		s := r.Stages[i].Status
		if s != StatusPassed && s != StatusSkipped {
			return false
		}
	}
	return true
}

// ============================================================================
// Stage implementations
// ============================================================================

func (c *PRLifecycleCoordinator) runCostEstimate(ctx context.Context, req PRRequest) StageResult {
	stage := StageResult{
		Name:      StageCostEstimate,
		StartedAt: time.Now(),
	}
	defer func() {
		stage.EndedAt = time.Now()
		stage.Duration = fmt.Sprintf("%.3fs", stage.Elapsed().Seconds())
	}()

	if req.Estimator == nil || req.TaskDesc == nil {
		stage.Status = StatusSkipped
		stage.Skipped = true
		stage.Message = "estimator or task descriptor not configured"
		return stage
	}

	stage.Status = StatusRunning

	costEst, err := req.Estimator.Estimate(*req.TaskDesc)
	if err != nil {
		stage.Status = StatusFailed
		stage.Error = fmt.Sprintf("cost estimation failed: %v", err)
		return stage
	}
	if costEst.CostTotal < 0 {
		stage.Status = StatusFailed
		stage.Error = "negative cost estimate"
		return stage
	}

	stage.Status = StatusPassed
	stage.Message = fmt.Sprintf("estimated cost: $%.4f", costEst.CostTotal)
	return stage
}

func (c *PRLifecycleCoordinator) runReview(ctx context.Context, req PRRequest) (StageResult, *review.EvidenceBundle, review.Consensus) {
	stage := StageResult{
		Name:      StageReview,
		StartedAt: time.Now(),
	}
	defer func() {
		stage.EndedAt = time.Now()
		stage.Duration = fmt.Sprintf("%.3fs", stage.Elapsed().Seconds())
	}()

	if req.ReviewOrchestrator == nil || req.ReviewPanel == nil {
		stage.Status = StatusSkipped
		stage.Skipped = true
		stage.Message = "review orchestrator or panel not configured"
		return stage, nil, review.Consensus{}
	}

	stage.Status = StatusRunning

	result, err := req.ReviewOrchestrator.Review(ctx, req.ReviewPanel,
		req.Diff, req.CommitMsg, req.Category, req.PRURL)
	if err != nil {
		stage.Status = StatusFailed
		stage.Error = fmt.Sprintf("review failed: %v", err)
		return stage, nil, review.Consensus{}
	}

	var bundle *review.EvidenceBundle
	var consensus review.Consensus
	if result != nil && result.Bundle != nil {
		bundle = result.Bundle
		consensus = bundle.Consensus
	}

	// Check consensus resolution.
	resolution := ""
	if result != nil {
		resolution = result.ConsensusLevel
	}
	if resolution == "divergent" {
		stage.Status = StatusEscalated
		stage.Message = "review consensus divergent — escalation required"
	} else if resolution == "majority" {
		stage.Status = StatusPassed
		stage.Message = fmt.Sprintf("review passed with majority consensus (%d/%d models agree)",
			result.ModelsAgree, result.TotalModels)
	} else {
		stage.Status = StatusPassed
		stage.Message = fmt.Sprintf("review passed unanimously (%d/%d models agree)",
			result.ModelsAgree, result.TotalModels)
	}

	return stage, bundle, consensus
}

func (c *PRLifecycleCoordinator) needsNegotiation(req PRRequest, consensus review.Consensus) bool {
	// If there's a negotiated setup, check if agents disagree.
	if req.NegotiationSetup != nil {
		return negotiate.DetectConflict(req.NegotiationSetup.VerdictA, req.NegotiationSetup.VerdictB)
	}
	// If review consensus is divergent, consider it contested.
	return consensus.Resolution != "" && consensus.Resolution != review.ResolutionApproved
}

func (c *PRLifecycleCoordinator) runNegotiation(ctx context.Context, req PRRequest) StageResult {
	stage := StageResult{
		Name:      StageNegotiation,
		StartedAt: time.Now(),
	}
	defer func() {
		stage.EndedAt = time.Now()
		stage.Duration = fmt.Sprintf("%.3fs", stage.Elapsed().Seconds())
	}()

	if req.NegotiationSetup == nil {
		stage.Status = StatusSkipped
		stage.Skipped = true
		stage.Message = "no negotiation setup provided"
		return stage
	}

	stage.Status = StatusRunning

	setup := req.NegotiationSetup
	neg, err := negotiate.NewNegotiator(
		setup.PRNumber, setup.AgentA, setup.AgentB,
		setup.VerdictA, setup.VerdictB,
		setup.ArbiterURL, setup.AuditPath,
	)
	if err != nil {
		// If no conflict, negotiation is unnecessary.
		stage.Status = StatusSkipped
		stage.Skipped = true
		stage.Message = fmt.Sprintf("no negotiation needed: %v", err)
		return stage
	}

	// Run the negotiation rounds.
	negState := neg.Neg.State
	switch negState {
	case negotiate.StateConflictDetected, negotiate.StateRound1,
		negotiate.StateRound2, negotiate.StateRound3:
		stage.Status = StatusEscalated
		stage.Message = "negotiation could not be auto-resolved — escalated to human"
	case negotiate.StateResolved:
		stage.Status = StatusPassed
		stage.Message = "negotiation resolved"
	case negotiate.StateEscalated:
		stage.Status = StatusEscalated
		stage.Message = "negotiation escalated to human review"
	case negotiate.StateDeadlock, negotiate.StateChimeraTiebreak:
		stage.Status = StatusFailed
		stage.Error = "negotiation deadlocked or awaiting Chimera tiebreak"
	default:
		stage.Status = StatusPassed
		stage.Message = fmt.Sprintf("negotiation state: %s", negState)
	}

	return stage
}

func (c *PRLifecycleCoordinator) runMergeGate(ctx context.Context, req PRRequest,
	bundle *review.EvidenceBundle) (StageResult, *mergegate.GateReport) {
	stage := StageResult{
		Name:      StageMergeGate,
		StartedAt: time.Now(),
	}
	defer func() {
		stage.EndedAt = time.Now()
		stage.Duration = fmt.Sprintf("%.3fs", stage.Elapsed().Seconds())
	}()

	stage.Status = StatusRunning

	gate := req.MergeGate
	if gate == nil {
		gate = mergegate.NewMergeGate()
	}

	mergeReq := mergegate.MergeRequest{
		AgentID:      req.AgentID,
		AgentTier:    req.AgentTier,
		ChangedFiles: req.ChangedFiles,
		Bundle:       bundle,
		Contract:     req.BehaviorContract,
	}

	report := gate.Evaluate(mergeReq)

	if report.IsBlocked() {
		stage.Status = StatusFailed
		stage.Error = fmt.Sprintf("merge blocked: %v", report.Blockers)
	} else if report.Decision == mergegate.DecisionEscalated {
		stage.Status = StatusEscalated
		stage.Message = report.Summary()
	} else {
		stage.Status = StatusPassed
		stage.Message = report.Summary()
	}

	return stage, &report
}

func (c *PRLifecycleCoordinator) runShadowDeploy(ctx context.Context, req PRRequest) StageResult {
	stage := StageResult{
		Name:      StageShadowDeploy,
		StartedAt: time.Now(),
	}
	defer func() {
		stage.EndedAt = time.Now()
		stage.Duration = fmt.Sprintf("%.3fs", stage.Elapsed().Seconds())
	}()

	if req.ShadowManager == nil {
		stage.Status = StatusSkipped
		stage.Skipped = true
		stage.Message = "shadow manager not configured"
		return stage
	}

	stage.Status = StatusRunning

	cfg := req.ShadowConfig
	if cfg.MaxErrorRateDelta == 0 && cfg.MaxLatencyOverheadPct == 0 {
		cfg = verify.DefaultShadowConfig()
	}

	_, err := req.ShadowManager.LaunchShadow(req.AgentID, string(req.AgentTier),
		req.BaselineMetrics, cfg)
	if err != nil {
		stage.Status = StatusFailed
		stage.Error = fmt.Sprintf("shadow launch failed: %v", err)
		return stage
	}

	stage.Status = StatusPassed
	stage.Message = fmt.Sprintf("shadow deployment launched for agent %s", req.AgentID)
	return stage
}

func (c *PRLifecycleCoordinator) runSurveillance(ctx context.Context, req PRRequest) StageResult {
	stage := StageResult{
		Name:      StageSurveillance,
		StartedAt: time.Now(),
	}
	defer func() {
		stage.EndedAt = time.Now()
		stage.Duration = fmt.Sprintf("%.3fs", stage.Elapsed().Seconds())
	}()

	if req.Surveillance == nil {
		stage.Status = StatusSkipped
		stage.Skipped = true
		stage.Message = "surveillance aggregator not configured"
		return stage
	}

	stage.Status = StatusRunning

	req.Surveillance.RegisterAgent(req.AgentID, req.BaselineMetrics)

	stage.Status = StatusPassed
	stage.Message = fmt.Sprintf("agent %s registered for steady-state surveillance", req.AgentID)
	return stage
}
