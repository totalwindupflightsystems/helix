package mergegate

import (
	"context"
	"testing"
	"time"
)

func TestGateOrder(t *testing.T) {
	order := GateOrder()
	if len(order) != 6 {
		t.Fatalf("GateOrder returned %d gates, want 6", len(order))
	}

	expected := []GateName{
		GateGitReinsTier1,
		GateGitReinsTier2,
		GateChimeraReview,
		GateConscientiousness,
		GatePromptFoo,
		GateCoApproval,
	}

	for i, g := range order {
		if g != expected[i] {
			t.Errorf("GateOrder[%d] = %s, want %s", i, g, expected[i])
		}
	}
}

func TestGatePipeline_AllPass(t *testing.T) {
	pipeline := NewDefaultPipeline()

	report := pipeline.Run(context.Background(), GateInput{
		PRURL:     "https://forgejo/repo/pulls/1",
		CommitSHA: "abc123",
		AgentID:   "agent-7",
	})

	if !report.AllPassed() {
		t.Errorf("expected all gates to pass, got decision %s", report.Decision)
	}
	if report.Decision != DecisionAllowed {
		t.Errorf("Decision = %s, want ALLOWED", report.Decision)
	}
	if report.GateReached != 5 {
		t.Errorf("GateReached = %d, want 5", report.GateReached)
	}
	if report.FailedGate() != nil {
		t.Error("expected no failed gate")
	}
}

func TestGatePipeline_FirstFailureStops(t *testing.T) {
	gates := []Gate{
		NewPassingStub(GateGitReinsTier1),
		NewFailingStub(GateGitReinsTier2, "evaluator found logic error in diff"),
		NewPassingStub(GateChimeraReview),
		NewPassingStub(GateConscientiousness),
		NewPassingStub(GatePromptFoo),
		NewPassingStub(GateCoApproval),
	}

	pipeline := NewGatePipeline(DefaultPipelineConfig(), gates...)
	report := pipeline.Run(context.Background(), GateInput{})

	if report.Decision != DecisionBlocked {
		t.Errorf("Decision = %s, want BLOCKED", report.Decision)
	}

	// Should have stopped after tier 2 failure
	if len(report.Gates) != 2 {
		t.Fatalf("expected 2 gate results (tier1 pass + tier2 fail), got %d", len(report.Gates))
	}

	if report.Gates[1].Status != CheckFail {
		t.Errorf("Gate[1] status = %s, want FAIL", report.Gates[1].Status)
	}
	if report.Gates[1].Evidence != "evaluator found logic error in diff" {
		t.Errorf("Gate[1] evidence = %q", report.Gates[1].Evidence)
	}

	failed := report.FailedGate()
	if failed == nil {
		t.Fatal("expected a failed gate")
	}
	if failed.Name != GateGitReinsTier2 {
		t.Errorf("FailedGate name = %s, want %s", failed.Name, GateGitReinsTier2)
	}
}

func TestGatePipeline_RunAllOnFailure(t *testing.T) {
	gates := []Gate{
		NewPassingStub(GateGitReinsTier1),
		NewFailingStub(GateGitReinsTier2, "tier2 fail"),
		NewPassingStub(GateChimeraReview),
		NewFailingStub(GateConscientiousness, "adversarial fail"),
	}

	config := DefaultPipelineConfig()
	config.StopOnFirstFail = false

	pipeline := NewGatePipeline(config, gates...)
	report := pipeline.Run(context.Background(), GateInput{})

	if report.Decision != DecisionBlocked {
		t.Errorf("Decision = %s, want BLOCKED", report.Decision)
	}
	if len(report.Gates) != 4 {
		t.Fatalf("expected 4 gate results (run-all mode), got %d", len(report.Gates))
	}
}

func TestGatePipeline_GlobalSkip(t *testing.T) {
	gates := []Gate{
		NewPassingStub(GateGitReinsTier1),
		NewPassingStub(GateGitReinsTier2),
		NewPassingStub(GateChimeraReview),
		NewFailingStub(GateConscientiousness, "should not run"),
		NewPassingStub(GatePromptFoo),
		NewPassingStub(GateCoApproval),
	}

	config := DefaultPipelineConfig()
	config.SkipGates = map[GateName]bool{GateConscientiousness: true}

	pipeline := NewGatePipeline(config, gates...)
	report := pipeline.Run(context.Background(), GateInput{})

	if report.Decision != DecisionAllowed {
		t.Errorf("Decision = %s, want ALLOWED", report.Decision)
	}

	// Find the conscientiousness gate result
	for _, g := range report.Gates {
		if g.Name == GateConscientiousness {
			if g.Status != CheckSkipped {
				t.Errorf("Conscientiousness status = %s, want SKIPPED", g.Status)
			}
			if g.SkippedReason != "globally skipped by pipeline config" {
				t.Errorf("SkippedReason = %q", g.SkippedReason)
			}
		}
	}
}

func TestGatePipeline_ConditionalSkip(t *testing.T) {
	gates := []Gate{
		NewPassingStub(GateGitReinsTier1),
		NewPassingStub(GateGitReinsTier2),
		NewPassingStub(GateChimeraReview),
		NewFailingStub(GateConscientiousness, "should not run"),
		NewPassingStub(GatePromptFoo),
		NewPassingStub(GateCoApproval),
	}

	pipeline := NewGatePipeline(DefaultPipelineConfig(), gates...)
	report := pipeline.Run(context.Background(), GateInput{
		SkippableGates: map[GateName]bool{GateConscientiousness: true},
	})

	if report.Decision != DecisionAllowed {
		t.Errorf("Decision = %s, want ALLOWED", report.Decision)
	}

	for _, g := range report.Gates {
		if g.Name == GateConscientiousness {
			if g.Status != CheckSkipped {
				t.Errorf("Conscientiousness status = %s, want SKIPPED", g.Status)
			}
			if g.SkippedReason != "conditionally skipped for this change type" {
				t.Errorf("SkippedReason = %q", g.SkippedReason)
			}
		}
	}
}

func TestGatePipeline_Empty(t *testing.T) {
	pipeline := NewGatePipeline(DefaultPipelineConfig())
	report := pipeline.Run(context.Background(), GateInput{})

	if report.Decision != DecisionAllowed {
		t.Errorf("Decision = %s, want ALLOWED for empty pipeline", report.Decision)
	}
	if len(report.Gates) != 0 {
		t.Errorf("expected 0 gates, got %d", len(report.Gates))
	}
	if report.GateReached != -1 {
		t.Errorf("GateReached = %d, want -1", report.GateReached)
	}
}

func TestGatePipeline_Timeout(t *testing.T) {
	// Gate that sleeps beyond the per-gate timeout
	slowGate := &slowGate{
		GateName: GateChimeraReview,
		delay:    200 * time.Millisecond,
	}

	gates := []Gate{
		NewPassingStub(GateGitReinsTier1),
		slowGate,
	}

	config := DefaultPipelineConfig()
	config.TimeoutPerGate = 50 * time.Millisecond

	pipeline := NewGatePipeline(config, gates...)
	report := pipeline.Run(context.Background(), GateInput{})

	// The slow gate should have been cancelled
	if report.Decision != DecisionBlocked {
		t.Errorf("Decision = %s, want BLOCKED", report.Decision)
	}
}

func TestGateResult_Passed(t *testing.T) {
	tests := []struct {
		result GateResult
		want   bool
	}{
		{GateResult{Status: CheckPass}, true},
		{GateResult{Status: CheckSkipped}, true},
		{GateResult{Status: CheckFail}, false},
		{GateResult{Status: CheckWarning}, false},
	}

	for _, tt := range tests {
		got := tt.result.Passed()
		if got != tt.want {
			t.Errorf("GateResult{Status: %s}.Passed() = %v, want %v", tt.result.Status, got, tt.want)
		}
	}
}

func TestPipelineReport_AllPassed(t *testing.T) {
	tests := []struct {
		name   string
		report *PipelineReport
		want   bool
	}{
		{
			name: "all pass",
			report: &PipelineReport{
				Gates: []GateResult{
					{Status: CheckPass},
					{Status: CheckPass},
					{Status: CheckSkipped},
				},
			},
			want: true,
		},
		{
			name: "one fail",
			report: &PipelineReport{
				Gates: []GateResult{
					{Status: CheckPass},
					{Status: CheckFail},
				},
			},
			want: false,
		},
		{
			name:   "empty",
			report: &PipelineReport{Gates: []GateResult{}},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.report.AllPassed() != tt.want {
				t.Errorf("AllPassed() = %v, want %v", tt.report.AllPassed(), tt.want)
			}
		})
	}
}

func TestPipelineReport_FailedGate(t *testing.T) {
	report := &PipelineReport{
		Gates: []GateResult{
			{Name: GateGitReinsTier1, Status: CheckPass},
			{Name: GateGitReinsTier2, Status: CheckFail, Evidence: "logic error"},
			{Name: GateChimeraReview, Status: CheckPass},
		},
	}

	failed := report.FailedGate()
	if failed == nil {
		t.Fatal("expected a failed gate")
	}
	if failed.Name != GateGitReinsTier2 {
		t.Errorf("FailedGate name = %s, want %s", failed.Name, GateGitReinsTier2)
	}

	report2 := &PipelineReport{
		Gates: []GateResult{
			{Name: GateGitReinsTier1, Status: CheckPass},
		},
	}
	if report2.FailedGate() != nil {
		t.Error("expected nil for all-passing report")
	}
}

func TestGatePipeline_GateNames(t *testing.T) {
	pipeline := NewGatePipeline(
		DefaultPipelineConfig(),
		NewPassingStub(GateGitReinsTier1),
		NewPassingStub(GateChimeraReview),
	)

	names := pipeline.GateNames()
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}
	if names[0] != GateGitReinsTier1 {
		t.Errorf("names[0] = %s", names[0])
	}
	if names[1] != GateChimeraReview {
		t.Errorf("names[1] = %s", names[1])
	}

	if pipeline.GateCount() != 2 {
		t.Errorf("GateCount = %d, want 2", pipeline.GateCount())
	}
}

func TestPipelineSummary(t *testing.T) {
	report := &PipelineReport{
		Decision: DecisionAllowed,
		Reason:   "all gates passed",
		Gates: []GateResult{
			{Name: GateGitReinsTier1, Status: CheckPass, Evidence: "clean"},
			{Name: GateGitReinsTier2, Status: CheckSkipped, SkippedReason: "cosmetic change"},
		},
	}

	summary := PipelineSummary(report)
	if summary == "" {
		t.Fatal("expected non-empty summary")
	}

	// Check it contains key elements
	if !containsStr(summary, "ALLOWED") {
		t.Error("summary should contain decision")
	}
	if !containsStr(summary, "gitreins_tier1") {
		t.Error("summary should contain gate names")
	}
	if !containsStr(summary, "✓") {
		t.Error("summary should contain pass icon")
	}
	if !containsStr(summary, "⊘") {
		t.Error("summary should contain skip icon")
	}
}

func TestPipelineSummary_Nil(t *testing.T) {
	s := PipelineSummary(nil)
	if s != "no report" {
		t.Errorf("PipelineSummary(nil) = %q, want 'no report'", s)
	}
}

func TestNewFailingStub(t *testing.T) {
	gate := NewFailingStub(GatePromptFoo, "test failure")
	if gate.Name() != GatePromptFoo {
		t.Errorf("Name = %s", gate.Name())
	}
	result := gate.Execute(context.Background(), GateInput{})
	if result.Status != CheckFail {
		t.Errorf("Status = %s, want FAIL", result.Status)
	}
	if result.Evidence != "test failure" {
		t.Errorf("Evidence = %q", result.Evidence)
	}
}

func TestDefaultPipelineConfig(t *testing.T) {
	config := DefaultPipelineConfig()
	if !config.StopOnFirstFail {
		t.Error("StopOnFirstFail should be true by default")
	}
	if config.TimeoutPerGate != 10*time.Minute {
		t.Errorf("TimeoutPerGate = %v, want 10m", config.TimeoutPerGate)
	}
	if config.SkipGates == nil {
		t.Error("SkipGates should be non-nil")
	}
}

// slowGate is a test gate that sleeps before returning.
type slowGate struct {
	GateName GateName
	delay    time.Duration
}

func (s *slowGate) Name() GateName { return s.GateName }
func (s *slowGate) Execute(ctx context.Context, _ GateInput) GateResult {
	select {
	case <-time.After(s.delay):
		return GateResult{
			Name:     s.GateName,
			Status:   CheckPass,
			Evidence: "completed after delay",
		}
	case <-ctx.Done():
		return GateResult{
			Name:     s.GateName,
			Status:   CheckFail,
			Evidence: "gate timed out",
			Error:    ctx.Err().Error(),
		}
	}
}
