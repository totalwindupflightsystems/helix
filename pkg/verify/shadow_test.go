package verify

import (
	"testing"
	"time"
)

// =============================================================================
// ShadowState tests
// =============================================================================

func TestShadowState_Valid(t *testing.T) {
	valid := []ShadowState{
		StateIdle, StateShadowing, StateShadowPassed, StateShadowFailed,
		StateCanaried, StateRolledBack, StatePromoted,
	}
	for _, s := range valid {
		if !s.Valid() {
			t.Errorf("state %q should be valid", s)
		}
	}
	invalid := []ShadowState{"", "unknown", "IDLE", "pending"}
	for _, s := range invalid {
		if s.Valid() {
			t.Errorf("state %q should be invalid", s)
		}
	}
}

func TestShadowState_IsTerminal(t *testing.T) {
	terminal := []ShadowState{StateRolledBack, StatePromoted}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("state %q should be terminal", s)
		}
	}
	nonTerminal := []ShadowState{
		StateIdle, StateShadowing, StateShadowPassed, StateShadowFailed, StateCanaried,
	}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Errorf("state %q should not be terminal", s)
		}
	}
}

// =============================================================================
// ShadowConfig tests
// =============================================================================

func TestDefaultShadowConfig(t *testing.T) {
	cfg := DefaultShadowConfig()
	if cfg.MaxErrorRateDelta != 0.001 {
		t.Errorf("MaxErrorRateDelta = %v, want 0.001", cfg.MaxErrorRateDelta)
	}
	if cfg.MaxLatencyOverheadPct != 20.0 {
		t.Errorf("MaxLatencyOverheadPct = %v, want 20.0", cfg.MaxLatencyOverheadPct)
	}
	if cfg.MaxMemoryGrowthPct != 10.0 {
		t.Errorf("MaxMemoryGrowthPct = %v, want 10.0", cfg.MaxMemoryGrowthPct)
	}
	if cfg.MaxNewErrorTypes != 0 {
		t.Errorf("MaxNewErrorTypes = %v, want 0", cfg.MaxNewErrorTypes)
	}
}

func TestShadowConfig_effectiveObservationWindow(t *testing.T) {
	// Explicit override
	cfg := ShadowConfig{ObservationWindow: 5 * time.Hour}
	if got := cfg.effectiveObservationWindow("provisional"); got != 5*time.Hour {
		t.Errorf("explicit window = %v, want 5h", got)
	}

	// Falls back to tier default
	cfg = ShadowConfig{}
	if got := cfg.effectiveObservationWindow("veteran"); got != 2*time.Hour {
		t.Errorf("veteran default = %v, want 2h", got)
	}
	if got := cfg.effectiveObservationWindow("provisional"); got != 24*time.Hour {
		t.Errorf("provisional default = %v, want 24h", got)
	}
}

// =============================================================================
// ShadowManager — LaunchShadow
// =============================================================================

func TestLaunchShadow_Success(t *testing.T) {
	m := NewShadowManager()
	baseline := MetricsSnapshot{
		SuccessRate:  0.9997,
		P99LatencyMs: 87,
		Timestamp:    time.Now(),
	}
	d, err := m.LaunchShadow("agent-1", "trusted", baseline, DefaultShadowConfig())
	if err != nil {
		t.Fatalf("LaunchShadow failed: %v", err)
	}
	if d.AgentID != "agent-1" {
		t.Errorf("AgentID = %q, want agent-1", d.AgentID)
	}
	if d.Tier != "trusted" {
		t.Errorf("Tier = %q, want trusted", d.Tier)
	}
	if d.State != StateShadowing {
		t.Errorf("State = %q, want %q", d.State, StateShadowing)
	}
	if d.Baseline.SuccessRate != 0.9997 {
		t.Errorf("Baseline.SuccessRate = %v, want 0.9997", d.Baseline.SuccessRate)
	}
}

func TestLaunchShadow_EmptyAgentID(t *testing.T) {
	m := NewShadowManager()
	_, err := m.LaunchShadow("", "trusted", MetricsSnapshot{}, DefaultShadowConfig())
	if err == nil {
		t.Error("expected error for empty agentID")
	}
}

func TestLaunchShadow_DefaultTier(t *testing.T) {
	m := NewShadowManager()
	d, _ := m.LaunchShadow("agent-1", "", MetricsSnapshot{}, DefaultShadowConfig())
	if d.Tier != "provisional" {
		t.Errorf("Tier = %q, want provisional (default)", d.Tier)
	}
}

func TestLaunchShadow_Idempotent_NonTerminal(t *testing.T) {
	m := NewShadowManager()
	baseline := MetricsSnapshot{SuccessRate: 0.99}
	d1, _ := m.LaunchShadow("agent-1", "trusted", baseline, DefaultShadowConfig())
	d2, _ := m.LaunchShadow("agent-1", "trusted", baseline, DefaultShadowConfig())
	if d1 != d2 {
		t.Error("second LaunchShadow should return the same deployment for non-terminal state")
	}
}

func TestLaunchShadow_Replaces_Terminal(t *testing.T) {
	m := NewShadowManager()
	d1, _ := m.LaunchShadow("agent-1", "trusted", MetricsSnapshot{}, DefaultShadowConfig())
	d1.mu.Lock()
	d1.State = StateRolledBack // terminal
	d1.mu.Unlock()

	d2, _ := m.LaunchShadow("agent-1", "trusted", MetricsSnapshot{SuccessRate: 0.98}, DefaultShadowConfig())
	if d2 == d1 {
		t.Error("should create a new deployment when previous is terminal")
	}
	if d2.State != StateShadowing {
		t.Errorf("new deployment state = %q, want %q", d2.State, StateShadowing)
	}
}

// =============================================================================
// ShadowManager — GetDeployment / ListDeployments
// =============================================================================

func TestGetDeployment_NotFound(t *testing.T) {
	m := NewShadowManager()
	if d := m.GetDeployment("nope"); d != nil {
		t.Error("expected nil for unknown agent")
	}
}

func TestListDeployments(t *testing.T) {
	m := NewShadowManager()
	_, _ = m.LaunchShadow("agent-a", "trusted", MetricsSnapshot{}, DefaultShadowConfig())
	_, _ = m.LaunchShadow("agent-b", "veteran", MetricsSnapshot{}, DefaultShadowConfig())

	ids := m.ListDeployments()
	if len(ids) != 2 {
		t.Fatalf("len(ListDeployments) = %d, want 2", len(ids))
	}
	found := map[string]bool{}
	for _, id := range ids {
		found[id] = true
	}
	if !found["agent-a"] || !found["agent-b"] {
		t.Errorf("missing agents in list: %v", ids)
	}
}

// =============================================================================
// ShadowManager — RecordShadowMetrics
// =============================================================================

func TestRecordShadowMetrics_Success(t *testing.T) {
	m := NewShadowManager()
	_, _ = m.LaunchShadow("agent-1", "trusted", MetricsSnapshot{}, DefaultShadowConfig())
	snap := MetricsSnapshot{SuccessRate: 0.999, P99LatencyMs: 90, RequestCount: 1000}
	if err := m.RecordShadowMetrics("agent-1", snap); err != nil {
		t.Fatalf("RecordShadowMetrics failed: %v", err)
	}
	d := m.GetDeployment("agent-1")
	if d.Shadow.SuccessRate != 0.999 {
		t.Errorf("Shadow.SuccessRate = %v, want 0.999", d.Shadow.SuccessRate)
	}
}

func TestRecordShadowMetrics_NoDeployment(t *testing.T) {
	m := NewShadowManager()
	err := m.RecordShadowMetrics("nope", MetricsSnapshot{})
	if err == nil {
		t.Error("expected error for unknown agent")
	}
}

// =============================================================================
// ShadowManager — EvaluateShadow (all pass)
// =============================================================================

func TestEvaluateShadow_AllPassed(t *testing.T) {
	m := NewShadowManager()
	baseline := MetricsSnapshot{
		SuccessRate:  0.9997,
		P99LatencyMs: 87,
		Timestamp:    time.Now(),
	}
	_, _ = m.LaunchShadow("agent-1", "trusted", baseline, DefaultShadowConfig())

	shadow := MetricsSnapshot{
		SuccessRate:     0.9996, // -0.01% → within 0.1% threshold
		P99LatencyMs:    94,     // +8% → within 20%
		NewErrorTypes:   0,
		MemoryGrowthPct: 5.0, // within 10%
		RequestCount:    5000,
		Timestamp:       time.Now(),
	}
	_ = m.RecordShadowMetrics("agent-1", shadow)

	report, err := m.EvaluateShadow("agent-1")
	if err != nil {
		t.Fatalf("EvaluateShadow failed: %v", err)
	}
	if !report.AllPassed {
		t.Errorf("expected all passed, got block reason: %s", report.BlockReason)
	}
	d := m.GetDeployment("agent-1")
	if d.State != StateShadowPassed {
		t.Errorf("state = %q, want %q", d.State, StateShadowPassed)
	}
}

// =============================================================================
// ShadowManager — EvaluateShadow (failure → rollback)
// =============================================================================

func TestEvaluateShadow_ErrorRateExceeded(t *testing.T) {
	m := NewShadowManager()
	baseline := MetricsSnapshot{SuccessRate: 0.9997, P99LatencyMs: 87}
	_, _ = m.LaunchShadow("agent-1", "trusted", baseline, DefaultShadowConfig())

	shadow := MetricsSnapshot{
		SuccessRate:  0.9950, // -0.47% → exceeds 0.1% threshold
		P99LatencyMs: 90,
	}
	_ = m.RecordShadowMetrics("agent-1", shadow)

	report, err := m.EvaluateShadow("agent-1")
	if err != nil {
		t.Fatalf("EvaluateShadow failed: %v", err)
	}
	if report.AllPassed {
		t.Error("expected failure due to error rate")
	}
	d := m.GetDeployment("agent-1")
	if d.State != StateRolledBack {
		t.Errorf("state = %q, want %q", d.State, StateRolledBack)
	}
	if d.RollbackReason == "" {
		t.Error("expected non-empty rollback reason")
	}
}

func TestEvaluateShadow_LatencyExceeded(t *testing.T) {
	m := NewShadowManager()
	baseline := MetricsSnapshot{SuccessRate: 0.9999, P99LatencyMs: 100}
	_, _ = m.LaunchShadow("agent-1", "trusted", baseline, DefaultShadowConfig())

	shadow := MetricsSnapshot{
		SuccessRate:  0.9998, // fine
		P99LatencyMs: 150,    // +50% → exceeds 20%
	}
	_ = m.RecordShadowMetrics("agent-1", shadow)

	report, _ := m.EvaluateShadow("agent-1")
	if report.AllPassed {
		t.Error("expected failure due to latency")
	}
	d := m.GetDeployment("agent-1")
	if d.State != StateRolledBack {
		t.Errorf("state = %q, want %q", d.State, StateRolledBack)
	}
}

func TestEvaluateShadow_NewErrorTypes(t *testing.T) {
	m := NewShadowManager()
	baseline := MetricsSnapshot{SuccessRate: 0.9999, P99LatencyMs: 50}
	_, _ = m.LaunchShadow("agent-1", "trusted", baseline, DefaultShadowConfig())

	shadow := MetricsSnapshot{
		SuccessRate:   0.9998,
		P99LatencyMs:  55,
		NewErrorTypes: 1, // any new error type blocks
	}
	_ = m.RecordShadowMetrics("agent-1", shadow)

	report, _ := m.EvaluateShadow("agent-1")
	if report.AllPassed {
		t.Error("expected failure due to new error types")
	}
}

func TestEvaluateShadow_MemoryGrowth(t *testing.T) {
	m := NewShadowManager()
	baseline := MetricsSnapshot{SuccessRate: 0.9999, P99LatencyMs: 50}
	_, _ = m.LaunchShadow("agent-1", "trusted", baseline, DefaultShadowConfig())

	shadow := MetricsSnapshot{
		SuccessRate:     0.9998,
		P99LatencyMs:    55,
		MemoryGrowthPct: 15.0, // exceeds 10%
	}
	_ = m.RecordShadowMetrics("agent-1", shadow)

	report, _ := m.EvaluateShadow("agent-1")
	if report.AllPassed {
		t.Error("expected failure due to memory growth")
	}
}

func TestEvaluateShadow_NotShadowing(t *testing.T) {
	m := NewShadowManager()
	d, _ := m.LaunchShadow("agent-1", "trusted", MetricsSnapshot{}, DefaultShadowConfig())
	d.mu.Lock()
	d.State = StateShadowPassed
	d.mu.Unlock()

	_, err := m.EvaluateShadow("agent-1")
	if err == nil {
		t.Error("expected error when evaluating non-shadowing deployment")
	}
}

func TestEvaluateShadow_NoDeployment(t *testing.T) {
	m := NewShadowManager()
	_, err := m.EvaluateShadow("nope")
	if err == nil {
		t.Error("expected error for unknown agent")
	}
}

// =============================================================================
// ShadowManager — PromoteToCanary
// =============================================================================

func TestPromoteToCanary_Success(t *testing.T) {
	m := NewShadowManager()
	_, _ = m.LaunchShadow("agent-1", "trusted", MetricsSnapshot{SuccessRate: 0.9999, P99LatencyMs: 50}, DefaultShadowConfig())
	_ = m.RecordShadowMetrics("agent-1", MetricsSnapshot{SuccessRate: 0.9998, P99LatencyMs: 55})
	_, _ = m.EvaluateShadow("agent-1") // → ShadowPassed

	d, err := m.PromoteToCanary("agent-1")
	if err != nil {
		t.Fatalf("PromoteToCanary failed: %v", err)
	}
	if d.State != StateCanaried {
		t.Errorf("state = %q, want %q", d.State, StateCanaried)
	}
	if d.CanaryStepIdx != 0 {
		t.Errorf("CanaryStepIdx = %d, want 0", d.CanaryStepIdx)
	}
}

func TestPromoteToCanary_NotPassed(t *testing.T) {
	m := NewShadowManager()
	_, _ = m.LaunchShadow("agent-1", "trusted", MetricsSnapshot{}, DefaultShadowConfig())
	// Still shadowing, not passed
	_, err := m.PromoteToCanary("agent-1")
	if err == nil {
		t.Error("expected error promoting non-passed shadow")
	}
}

func TestPromoteToCanary_NoDeployment(t *testing.T) {
	m := NewShadowManager()
	_, err := m.PromoteToCanary("nope")
	if err == nil {
		t.Error("expected error for unknown agent")
	}
}

// =============================================================================
// ShadowManager — AdvanceCanary
// =============================================================================

func TestAdvanceCanary_Trusted(t *testing.T) {
	m := NewShadowManager()
	_, _ = m.LaunchShadow("agent-1", "trusted", MetricsSnapshot{SuccessRate: 0.9999, P99LatencyMs: 50}, DefaultShadowConfig())
	_ = m.RecordShadowMetrics("agent-1", MetricsSnapshot{SuccessRate: 0.9998, P99LatencyMs: 55})
	_, _ = m.EvaluateShadow("agent-1")
	_, _ = m.PromoteToCanary("agent-1")

	// Trusted: 3 steps (1%, 10%, 100%)
	_, steps := CanarySchedule("trusted")
	totalSteps := len(steps)

	// Advance through all steps
	for i := 1; i < totalSteps; i++ {
		step, isFinal, err := m.AdvanceCanary("agent-1")
		if err != nil {
			t.Fatalf("AdvanceCanary step %d failed: %v", i, err)
		}
		if i == totalSteps-1 {
			if !isFinal {
				t.Errorf("step %d: expected final=true", i)
			}
		} else {
			if isFinal {
				t.Errorf("step %d: expected final=false", i)
			}
		}
		_ = step
	}

	d := m.GetDeployment("agent-1")
	if d.State != StatePromoted {
		t.Errorf("state = %q, want %q", d.State, StatePromoted)
	}
}

func TestAdvanceCanary_Veteran(t *testing.T) {
	m := NewShadowManager()
	_, _ = m.LaunchShadow("agent-1", "veteran", MetricsSnapshot{SuccessRate: 0.9999, P99LatencyMs: 50}, DefaultShadowConfig())
	_ = m.RecordShadowMetrics("agent-1", MetricsSnapshot{SuccessRate: 0.9998, P99LatencyMs: 55})
	_, _ = m.EvaluateShadow("agent-1")
	_, _ = m.PromoteToCanary("agent-1")

	// Veteran: 2 steps (10%, 100%)
	_, steps := CanarySchedule("veteran")
	totalSteps := len(steps)

	for i := 1; i < totalSteps; i++ {
		_, _, err := m.AdvanceCanary("agent-1")
		if err != nil {
			t.Fatalf("AdvanceCanary step %d failed: %v", i, err)
		}
	}

	d := m.GetDeployment("agent-1")
	if d.State != StatePromoted {
		t.Errorf("state = %q, want %q", d.State, StatePromoted)
	}
}

func TestAdvanceCanary_NotCanaried(t *testing.T) {
	m := NewShadowManager()
	_, _ = m.LaunchShadow("agent-1", "trusted", MetricsSnapshot{}, DefaultShadowConfig())
	_, _, err := m.AdvanceCanary("agent-1")
	if err == nil {
		t.Error("expected error advancing non-canary deployment")
	}
}

// =============================================================================
// ShadowManager — CurrentCanaryStep
// =============================================================================

func TestCurrentCanaryStep(t *testing.T) {
	m := NewShadowManager()
	_, _ = m.LaunchShadow("agent-1", "trusted", MetricsSnapshot{SuccessRate: 0.9999, P99LatencyMs: 50}, DefaultShadowConfig())
	_ = m.RecordShadowMetrics("agent-1", MetricsSnapshot{SuccessRate: 0.9998, P99LatencyMs: 55})
	_, _ = m.EvaluateShadow("agent-1")
	_, _ = m.PromoteToCanary("agent-1")

	step, err := m.CurrentCanaryStep("agent-1")
	if err != nil {
		t.Fatalf("CurrentCanaryStep failed: %v", err)
	}
	// Trusted step 0: 1% traffic
	if step.TrafficPct != 1 {
		t.Errorf("step 0 traffic = %v, want 1", step.TrafficPct)
	}
}

func TestCurrentCanaryStep_NotCanary(t *testing.T) {
	m := NewShadowManager()
	_, _ = m.LaunchShadow("agent-1", "trusted", MetricsSnapshot{}, DefaultShadowConfig())
	_, err := m.CurrentCanaryStep("agent-1")
	if err == nil {
		t.Error("expected error for non-canary deployment")
	}
}

// =============================================================================
// ShadowManager — AutoRollback
// =============================================================================

func TestAutoRollback_FromCanary(t *testing.T) {
	m := NewShadowManager()
	_, _ = m.LaunchShadow("agent-1", "trusted", MetricsSnapshot{SuccessRate: 0.9999, P99LatencyMs: 50}, DefaultShadowConfig())
	_ = m.RecordShadowMetrics("agent-1", MetricsSnapshot{SuccessRate: 0.9998, P99LatencyMs: 55})
	_, _ = m.EvaluateShadow("agent-1")
	_, _ = m.PromoteToCanary("agent-1")

	err := m.AutoRollback("agent-1", "canary breach: error rate spike at 5% traffic")
	if err != nil {
		t.Fatalf("AutoRollback failed: %v", err)
	}
	d := m.GetDeployment("agent-1")
	if d.State != StateRolledBack {
		t.Errorf("state = %q, want %q", d.State, StateRolledBack)
	}
	if d.RollbackReason == "" {
		t.Error("expected non-empty rollback reason")
	}
}

func TestAutoRollback_FromShadowing(t *testing.T) {
	m := NewShadowManager()
	_, _ = m.LaunchShadow("agent-1", "trusted", MetricsSnapshot{}, DefaultShadowConfig())
	err := m.AutoRollback("agent-1", "shadow crash")
	if err != nil {
		t.Fatalf("AutoRollback failed: %v", err)
	}
	d := m.GetDeployment("agent-1")
	if d.State != StateRolledBack {
		t.Errorf("state = %q, want %q", d.State, StateRolledBack)
	}
}

func TestAutoRollback_AlreadyTerminal(t *testing.T) {
	m := NewShadowManager()
	_, _ = m.LaunchShadow("agent-1", "trusted", MetricsSnapshot{}, DefaultShadowConfig())
	_ = m.AutoRollback("agent-1", "first rollback")

	// Second rollback should fail
	err := m.AutoRollback("agent-1", "second rollback")
	if err == nil {
		t.Error("expected error rolling back an already-terminal deployment")
	}
}

func TestAutoRollback_NoDeployment(t *testing.T) {
	m := NewShadowManager()
	err := m.AutoRollback("nope", "reason")
	if err == nil {
		t.Error("expected error for unknown agent")
	}
}

// =============================================================================
// ShadowManager — ObservationWindowRemaining
// =============================================================================

func TestObservationWindowRemaining_Active(t *testing.T) {
	m := NewShadowManager()
	cfg := DefaultShadowConfig()
	cfg.ObservationWindow = 10 * time.Hour
	baseline := MetricsSnapshot{SuccessRate: 0.99}
	_, _ = m.LaunchShadow("agent-1", "trusted", baseline, cfg)

	remaining := m.ObservationWindowRemaining("agent-1")
	if remaining <= 0 || remaining > 10*time.Hour {
		t.Errorf("remaining = %v, want (0, 10h]", remaining)
	}
}

func TestObservationWindowRemaining_TierDefault(t *testing.T) {
	m := NewShadowManager()
	_, _ = m.LaunchShadow("agent-1", "veteran", MetricsSnapshot{}, DefaultShadowConfig())

	remaining := m.ObservationWindowRemaining("agent-1")
	// Veteran: 2h shadow
	if remaining <= 0 || remaining > 2*time.Hour {
		t.Errorf("remaining = %v, want (0, 2h]", remaining)
	}
}

func TestObservationWindowRemaining_NotShadowing(t *testing.T) {
	m := NewShadowManager()
	_, _ = m.LaunchShadow("agent-1", "trusted", MetricsSnapshot{SuccessRate: 0.9999, P99LatencyMs: 50}, DefaultShadowConfig())
	_ = m.RecordShadowMetrics("agent-1", MetricsSnapshot{SuccessRate: 0.9998, P99LatencyMs: 55})
	_, _ = m.EvaluateShadow("agent-1")

	remaining := m.ObservationWindowRemaining("agent-1")
	if remaining != 0 {
		t.Errorf("remaining = %v, want 0 (not shadowing)", remaining)
	}
}

func TestObservationWindowRemaining_NoDeployment(t *testing.T) {
	m := NewShadowManager()
	if r := m.ObservationWindowRemaining("nope"); r != 0 {
		t.Errorf("remaining = %v, want 0", r)
	}
}

// =============================================================================
// evaluateDifferential — edge cases
// =============================================================================

func TestEvaluateDifferential_AllWithinThreshold(t *testing.T) {
	prod := MetricsSnapshot{SuccessRate: 0.9999, P99LatencyMs: 100}
	shadow := MetricsSnapshot{
		SuccessRate:     0.9998,
		P99LatencyMs:    119, // +19% → within 20%
		MemoryGrowthPct: 9.0, // within 10%
		NewErrorTypes:   0,
	}
	cfg := DefaultShadowConfig()
	report := evaluateDifferential(prod, shadow, cfg)
	if !report.AllPassed {
		t.Errorf("expected all passed, got: %s", report.BlockReason)
	}
}

func TestEvaluateDifferential_BoundaryLatency(t *testing.T) {
	prod := MetricsSnapshot{SuccessRate: 0.9999, P99LatencyMs: 100}
	shadow := MetricsSnapshot{
		SuccessRate:  0.9999,
		P99LatencyMs: 120, // exactly 20% → should pass (<=)
	}
	cfg := DefaultShadowConfig()
	report := evaluateDifferential(prod, shadow, cfg)
	if !report.AllPassed {
		t.Errorf("expected boundary to pass, got: %s", report.BlockReason)
	}
}

func TestEvaluateDifferential_ZeroProdLatency(t *testing.T) {
	prod := MetricsSnapshot{SuccessRate: 0.9999, P99LatencyMs: 0}
	shadow := MetricsSnapshot{SuccessRate: 0.9999, P99LatencyMs: 500}
	cfg := DefaultShadowConfig()
	report := evaluateDifferential(prod, shadow, cfg)
	// When prod latency is 0, overhead % should not cause a failure (no baseline to compare)
	if !report.AllPassed {
		// Acceptable with prod=0: the delta should be 0% overhead
		// The only allowed failure is p99_latency_ms with zero baseline
		for _, d := range report.Deltas {
			if d.Metric == "p99_latency_ms" && !d.Passed {
				t.Logf("p99 latency delta failed as expected with zero baseline: %v", d)
			} else if !d.Passed {
				t.Errorf("unexpected delta failure: %v", d)
			}
		}
	}
}

// =============================================================================
// Full lifecycle integration test
// =============================================================================

func TestFullLifecycle_Trusted_PassAndPromote(t *testing.T) {
	m := NewShadowManager()
	baseline := MetricsSnapshot{
		SuccessRate:  0.9997,
		P99LatencyMs: 87,
		Timestamp:    time.Now(),
	}
	cfg := DefaultShadowConfig()

	// 1. Launch shadow
	d, err := m.LaunchShadow("agent-lifecycle", "trusted", baseline, cfg)
	if err != nil {
		t.Fatalf("LaunchShadow: %v", err)
	}
	if d.State != StateShadowing {
		t.Fatalf("expected shadowing, got %s", d.State)
	}

	// 2. Record shadow metrics (within thresholds)
	_ = m.RecordShadowMetrics("agent-lifecycle", MetricsSnapshot{
		SuccessRate:  0.9996,
		P99LatencyMs: 90,
	})

	// 3. Evaluate → should pass
	report, err := m.EvaluateShadow("agent-lifecycle")
	if err != nil {
		t.Fatalf("EvaluateShadow: %v", err)
	}
	if !report.AllPassed {
		t.Fatalf("shadow evaluation failed: %s", report.BlockReason)
	}

	// 4. Promote to canary
	d, err = m.PromoteToCanary("agent-lifecycle")
	if err != nil {
		t.Fatalf("PromoteToCanary: %v", err)
	}
	if d.State != StateCanaried {
		t.Fatalf("expected canaried, got %s", d.State)
	}

	// 5. Advance through all canary steps → promoted
	_, steps := CanarySchedule("trusted")
	for i := 1; i < len(steps); i++ {
		_, _, err := m.AdvanceCanary("agent-lifecycle")
		if err != nil {
			t.Fatalf("AdvanceCanary step %d: %v", i, err)
		}
	}

	d = m.GetDeployment("agent-lifecycle")
	if d.State != StatePromoted {
		t.Errorf("expected promoted, got %s", d.State)
	}
}

func TestFullLifecycle_RollbackDuringShadow(t *testing.T) {
	m := NewShadowManager()
	baseline := MetricsSnapshot{SuccessRate: 0.9999, P99LatencyMs: 50}
	_, _ = m.LaunchShadow("agent-rb", "provisional", baseline, DefaultShadowConfig())

	// Shadow with bad metrics
	_ = m.RecordShadowMetrics("agent-rb", MetricsSnapshot{
		SuccessRate:  0.9800, // -1.99% → way over 0.1%
		P99LatencyMs: 52,
	})

	report, err := m.EvaluateShadow("agent-rb")
	if err != nil {
		t.Fatalf("EvaluateShadow: %v", err)
	}
	if report.AllPassed {
		t.Fatal("expected shadow to fail")
	}

	d := m.GetDeployment("agent-rb")
	if d.State != StateRolledBack {
		t.Errorf("expected rolled_back, got %s", d.State)
	}

	// Cannot promote a rolled-back deployment
	_, err = m.PromoteToCanary("agent-rb")
	if err == nil {
		t.Error("expected error promoting rolled-back deployment")
	}
}

func TestFullLifecycle_RollbackDuringCanary(t *testing.T) {
	m := NewShadowManager()
	baseline := MetricsSnapshot{SuccessRate: 0.9999, P99LatencyMs: 50}
	_, _ = m.LaunchShadow("agent-cr", "observed", baseline, DefaultShadowConfig())
	_ = m.RecordShadowMetrics("agent-cr", MetricsSnapshot{SuccessRate: 0.9998, P99LatencyMs: 55})
	_, _ = m.EvaluateShadow("agent-cr")
	_, _ = m.PromoteToCanary("agent-cr")

	// External monitoring detects breach during canary
	err := m.AutoRollback("agent-cr", "canary: error rate spike at 10% traffic")
	if err != nil {
		t.Fatalf("AutoRollback: %v", err)
	}

	d := m.GetDeployment("agent-cr")
	if d.State != StateRolledBack {
		t.Errorf("expected rolled_back, got %s", d.State)
	}

	// Cannot advance a rolled-back deployment
	_, _, err = m.AdvanceCanary("agent-cr")
	if err == nil {
		t.Error("expected error advancing rolled-back deployment")
	}
}
