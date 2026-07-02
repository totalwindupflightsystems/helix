package mergegate

import (
	"context"
	"fmt"
	"time"
)

// ============================================================================
// Gate Pipeline (spec §7.2)
// ============================================================================

// The full gate pipeline per spec §7.2:
//
//	GitReins Tier 1 (static, <5s)
//	  → GitReins Tier 2 (agentic, 30-90s)
//	    → Chimera Formation Review (multi-model, 2-5m)
//	      → Conscientiousness Adversarial Loop (iterative, 3-10m)
//	        → PromptFoo Regression (CI, 30-120s)
//	          → Co-Approval Gate (human + agent, async)
//
// Each gate must return a structured PASS or FAIL with evidence.
// SOFT_FAIL is not a valid state — all gates are hard gates.

// GateName identifies a specific gate in the pipeline.
type GateName string

const (
	GateGitReinsTier1     GateName = "gitreins_tier1"
	GateGitReinsTier2     GateName = "gitreins_tier2"
	GateChimeraReview     GateName = "chimera_review"
	GateConscientiousness GateName = "conscientiousness_adversarial"
	GatePromptFoo         GateName = "promptfoo_regression"
	GateCoApproval        GateName = "co_approval"
)

// GateOrder returns the canonical execution order for all gates per spec §7.2.
func GateOrder() []GateName {
	return []GateName{
		GateGitReinsTier1,
		GateGitReinsTier2,
		GateChimeraReview,
		GateConscientiousness,
		GatePromptFoo,
		GateCoApproval,
	}
}

// Gate is a single executable gate in the quality pipeline.
type Gate interface {
	Name() GateName
	Execute(ctx context.Context, input GateInput) GateResult
}

// GateInput carries the data needed by gates to execute.
type GateInput struct {
	PRURL      string
	CommitSHA  string
	AgentID    string
	Repo       string
	Diff       string
	SpecRef    string
	PromptHash string
	// AllowSkip marks gates that can be skipped (e.g., cosmetic changes skip
	// adversarial review). The pipeline executor checks this set before each
	// gate.
	SkippableGates map[GateName]bool
}

// GateResult is the outcome of executing a single gate.
type GateResult struct {
	Name          GateName      `json:"name"`
	Status        CheckStatus   `json:"status"`
	Evidence      string        `json:"evidence"`
	Duration      time.Duration `json:"duration_ms"`
	Error         string        `json:"error,omitempty"`
	SkippedReason string        `json:"skipped_reason,omitempty"`
}

// Passed returns true if the gate passed (PASS or SKIPPED).
func (r GateResult) Passed() bool {
	return r.Status == CheckPass || r.Status == CheckSkipped
}

// ============================================================================
// GatePipeline — sequential executor
// ============================================================================

// GatePipeline executes the full gate sequence per spec §7.2. Gates run
// sequentially — a failure at any gate halts the pipeline and returns the
// failure to the caller.
type GatePipeline struct {
	gates  []Gate
	config PipelineConfig
}

// PipelineConfig controls pipeline behavior.
type PipelineConfig struct {
	// StopOnFirstFail controls whether the pipeline stops at the first failing
	// gate (true, default) or runs all gates regardless (false, for
	// diagnostic/debugging purposes).
	StopOnFirstFail bool
	// TimeoutPerGate is the maximum duration for any single gate.
	TimeoutPerGate time.Duration
	// SkipGates is a set of gate names to skip entirely (not just
	// conditionally skip via GateInput.SkippableGates).
	SkipGates map[GateName]bool
}

// DefaultPipelineConfig returns the recommended configuration.
func DefaultPipelineConfig() PipelineConfig {
	return PipelineConfig{
		StopOnFirstFail: true,
		TimeoutPerGate:  10 * time.Minute,
		SkipGates:       map[GateName]bool{},
	}
}

// NewGatePipeline creates a pipeline with the given gates and configuration.
func NewGatePipeline(config PipelineConfig, gates ...Gate) *GatePipeline {
	return &GatePipeline{
		gates:  gates,
		config: config,
	}
}

// PipelineReport is the full outcome of running the gate pipeline.
type PipelineReport struct {
	Gates    []GateResult  `json:"gates"`
	Decision MergeDecision `json:"decision"`
	Reason   string        `json:"reason"`
	// GateReached is the index of the last gate that executed (not skipped).
	GateReached int `json:"gate_reached"`
}

// AllPassed returns true if every executed gate passed.
func (r *PipelineReport) AllPassed() bool {
	for _, g := range r.Gates {
		if !g.Passed() {
			return false
		}
	}
	return true
}

// FailedGate returns the first failing gate result, or nil if all passed.
func (r *PipelineReport) FailedGate() *GateResult {
	for i := range r.Gates {
		if !r.Gates[i].Passed() {
			return &r.Gates[i]
		}
	}
	return nil
}

// Run executes the pipeline sequentially per spec §7.2.
func (p *GatePipeline) Run(ctx context.Context, input GateInput) *PipelineReport {
	report := &PipelineReport{
		Gates:       make([]GateResult, 0, len(p.gates)),
		Decision:    DecisionAllowed,
		GateReached: -1,
	}

	for _, gate := range p.gates {
		name := gate.Name()

		// Check if this gate is globally skipped
		if p.config.SkipGates[name] {
			report.Gates = append(report.Gates, GateResult{
				Name:          name,
				Status:        CheckSkipped,
				SkippedReason: "globally skipped by pipeline config",
			})
			continue
		}

		// Check if this gate is conditionally skipped for this input
		if input.SkippableGates != nil && input.SkippableGates[name] {
			report.Gates = append(report.Gates, GateResult{
				Name:          name,
				Status:        CheckSkipped,
				SkippedReason: "conditionally skipped for this change type",
			})
			continue
		}

		// Execute the gate with timeout
		gateCtx := ctx
		if p.config.TimeoutPerGate > 0 {
			var cancel context.CancelFunc
			gateCtx, cancel = context.WithTimeout(ctx, p.config.TimeoutPerGate)
			defer cancel()
		}

		start := time.Now()
		result := gate.Execute(gateCtx, input)
		result.Duration = time.Since(start)
		report.Gates = append(report.Gates, result)
		report.GateReached = len(report.Gates) - 1

		if !result.Passed() {
			report.Decision = DecisionBlocked
			report.Reason = fmt.Sprintf("gate %s failed: %s", name, result.Evidence)

			if p.config.StopOnFirstFail {
				return report
			}
		}
	}

	if report.AllPassed() {
		report.Decision = DecisionAllowed
		report.Reason = "all gates passed"
	} else {
		report.Decision = DecisionBlocked
		failed := report.FailedGate()
		if failed != nil {
			report.Reason = fmt.Sprintf("gate %s failed: %s", failed.Name, failed.Evidence)
		}
	}

	return report
}

// ============================================================================
// Stub Gate — for testing and as a base for real implementations
// ============================================================================

// StubGate is a gate that always returns a fixed result. Used for testing
// and as a placeholder for gates that haven't been wired to live services yet.
type StubGate struct {
	GateName GateName
	Result   GateResult
}

func (s *StubGate) Name() GateName { return s.GateName }
func (s *StubGate) Execute(_ context.Context, _ GateInput) GateResult {
	return s.Result
}

// NewPassingStub creates a StubGate that always passes.
func NewPassingStub(name GateName) *StubGate {
	return &StubGate{
		GateName: name,
		Result: GateResult{
			Name:     name,
			Status:   CheckPass,
			Evidence: "stub: pass",
		},
	}
}

// NewFailingStub creates a StubGate that always fails with the given evidence.
func NewFailingStub(name GateName, evidence string) *StubGate {
	return &StubGate{
		GateName: name,
		Result: GateResult{
			Name:     name,
			Status:   CheckFail,
			Evidence: evidence,
		},
	}
}

// ============================================================================
// Default Pipeline Factory
// ============================================================================

// NewDefaultPipeline creates the full 6-gate pipeline with stub gates.
// In production, each stub is replaced with a real implementation that calls
// the corresponding service (GitReins, Chimera, Conscientiousness, etc.).
func NewDefaultPipeline() *GatePipeline {
	return NewGatePipeline(
		DefaultPipelineConfig(),
		NewPassingStub(GateGitReinsTier1),
		NewPassingStub(GateGitReinsTier2),
		NewPassingStub(GateChimeraReview),
		NewPassingStub(GateConscientiousness),
		NewPassingStub(GatePromptFoo),
		NewPassingStub(GateCoApproval),
	)
}

// ============================================================================
// Helpers
// ============================================================================

// GateNames returns the names of all gates registered in the pipeline.
func (p *GatePipeline) GateNames() []GateName {
	names := make([]GateName, len(p.gates))
	for i, g := range p.gates {
		names[i] = g.Name()
	}
	return names
}

// GateCount returns the number of gates in the pipeline.
func (p *GatePipeline) GateCount() int {
	return len(p.gates)
}

// PipelineSummary renders a human-readable summary of the pipeline report.
func PipelineSummary(report *PipelineReport) string {
	if report == nil {
		return "no report"
	}

	var sb []byte
	sb = append(sb, fmt.Sprintf("Pipeline Decision: %s\n", report.Decision)...)
	sb = append(sb, fmt.Sprintf("Reason: %s\n\n", report.Reason)...)
	sb = append(sb, "Gate Results:\n"...)

	for _, g := range report.Gates {
		icon := "✓"
		if !g.Passed() {
			icon = "✗"
		}
		if g.Status == CheckSkipped {
			icon = "⊘"
		}
		sb = append(sb, fmt.Sprintf("  %s %s (%v) — %s\n", icon, g.Name, g.Duration, g.Evidence)...)
	}

	return string(sb)
}
