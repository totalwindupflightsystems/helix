package verify

import (
	"fmt"
	"sync"
	"time"
)

// =============================================================================
// Shadow Deployment Manager (spec §Phase 1 — Shadow Verification)
//
// The shadow manager orchestrates the three-phase post-merge deployment
// pipeline: dark launch → shadow observation → canary promotion → auto-rollback.
// It consumes the CanarySchedule from contract.go and the breach/drift
// detection from monitor.go to provide a single lifecycle API.
//
// Lifecycle:
//
//	Idle → LaunchShadow → Shadowing → (observation window) →
//	  → Evaluate → ShadowPassed → PromoteToCanary → Canaried →
//	  OR Evaluate → ShadowFailed → RolledBack
// =============================================================================

// ShadowState enumerates the lifecycle stage of a shadow deployment.
type ShadowState string

const (
	// StateIdle: no shadow deployment exists for this agent.
	StateIdle ShadowState = "idle"
	// StateShadowing: dark launch active — traffic mirrored, responses discarded.
	StateShadowing ShadowState = "shadowing"
	// StateShadowPassed: shadow observation completed, no anomalies detected.
	StateShadowPassed ShadowState = "shadow_passed"
	// StateShadowFailed: shadow detected anomalies, rollback triggered.
	StateShadowFailed ShadowState = "shadow_failed"
	// StateCanaried: promoted to canary — real traffic being routed.
	StateCanaried ShadowState = "canaried"
	// StateRolledBack: deployment reverted due to canary contract breach.
	StateRolledBack ShadowState = "rolled_back"
	// StatePromoted: canary completed 100% traffic ramp, fully promoted.
	StatePromoted ShadowState = "promoted"
)

// Valid returns true if s is a recognized shadow state.
func (s ShadowState) Valid() bool {
	switch s {
	case StateIdle, StateShadowing, StateShadowPassed, StateShadowFailed,
		StateCanaried, StateRolledBack, StatePromoted:
		return true
	}
	return false
}

// IsTerminal reports whether the state is terminal (no further transitions).
func (s ShadowState) IsTerminal() bool {
	return s == StateRolledBack || s == StatePromoted
}

// =============================================================================
// ShadowConfig
// =============================================================================

// ShadowConfig holds configurable thresholds and timing for shadow verification.
// Defaults match the spec's auto-rollback trigger thresholds (§Auto-Rollback Triggers).
type ShadowConfig struct {
	// ObservationWindow is how long shadow traffic runs before evaluation.
	// Default: matches the trust tier's shadow duration from CanarySchedule.
	ObservationWindow time.Duration `json:"observation_window"`

	// MaxErrorRateDelta: shadow success rate must be within this fraction of prod.
	// Spec: >0.1% = 0.001.
	MaxErrorRateDelta float64 `json:"max_error_rate_delta"`

	// MaxLatencyOverheadPct: shadow P99 must not exceed prod P99 by this fraction.
	// Spec: >20%.
	MaxLatencyOverheadPct float64 `json:"max_latency_overhead_pct"`

	// MaxMemoryGrowthPct: shadow memory must not grow more than this %.
	// Spec: >10%.
	MaxMemoryGrowthPct float64 `json:"max_memory_growth_pct"`

	// MaxNewErrorTypes: number of new error categories tolerated.
	// Spec: any new error type blocks.
	MaxNewErrorTypes int `json:"max_new_error_types"`

	// CanaryTrafficStartPct: initial traffic % when promoting to canary.
	// Spec: 1% (Provisional/Observed) or 10% (Veteran). Uses CanarySchedule.
	CanaryTrafficStartPct float64 `json:"canary_traffic_start_pct"`
}

// DefaultShadowConfig returns spec-compliant thresholds.
func DefaultShadowConfig() ShadowConfig {
	return ShadowConfig{
		ObservationWindow:     0, // 0 = use CanarySchedule tier default
		MaxErrorRateDelta:     0.001,
		MaxLatencyOverheadPct: 20.0,
		MaxMemoryGrowthPct:    10.0,
		MaxNewErrorTypes:      0,
		CanaryTrafficStartPct: 1.0,
	}
}

// effectiveObservationWindow resolves the observation window, falling back
// to the trust tier's default shadow duration when not explicitly configured.
func (c ShadowConfig) effectiveObservationWindow(tier string) time.Duration {
	if c.ObservationWindow > 0 {
		return c.ObservationWindow
	}
	dur, _ := CanarySchedule(tier)
	return dur
}

// =============================================================================
// MetricsSnapshot
// =============================================================================

// MetricsSnapshot captures a point-in-time observation of production or shadow
// behavior. All fields are raw measured values — thresholds are applied
// during evaluation.
type MetricsSnapshot struct {
	SuccessRate     float64   `json:"success_rate"`      // 0..1
	P99LatencyMs    float64   `json:"p99_latency_ms"`    // milliseconds
	P50LatencyMs    float64   `json:"p50_latency_ms"`    // milliseconds
	ErrorCount      int64     `json:"error_count"`       // raw count
	NewErrorTypes   int       `json:"new_error_types"`   // categories not seen in baseline
	MemoryGrowthPct float64   `json:"memory_growth_pct"` // percentage
	RequestCount    int64     `json:"request_count"`     // total requests observed
	Timestamp       time.Time `json:"timestamp"`
}

// DifferentialReport compares production baseline vs shadow metrics and
// reports per-metric deltas with pass/fail status.
type DifferentialReport struct {
	Production  MetricsSnapshot `json:"production"`
	Shadow      MetricsSnapshot `json:"shadow"`
	Deltas      []MetricDelta   `json:"deltas"`
	AllPassed   bool            `json:"all_passed"`
	BlockReason string          `json:"block_reason,omitempty"`
}

// MetricDelta is a single metric comparison.
type MetricDelta struct {
	Metric string  `json:"metric"`
	Prod   float64 `json:"prod"`
	Shadow float64 `json:"shadow"`
	Delta  float64 `json:"delta"`
	Passed bool    `json:"passed"`
	Reason string  `json:"reason,omitempty"`
}

// =============================================================================
// ShadowDeployment
// =============================================================================

// ShadowDeployment tracks the full lifecycle of a single shadow+canary deployment.
type ShadowDeployment struct {
	mu sync.RWMutex

	AgentID        string              `json:"agent_id"`
	Tier           string              `json:"tier"` // trust tier: provisional|observed|trusted|veteran
	State          ShadowState         `json:"state"`
	Config         ShadowConfig        `json:"config"`
	Baseline       MetricsSnapshot     `json:"baseline"`
	Shadow         MetricsSnapshot     `json:"shadow_metrics"`
	LaunchedAt     time.Time           `json:"launched_at"`
	EvaluatedAt    time.Time           `json:"evaluated_at,omitempty"`
	PromotedAt     time.Time           `json:"promoted_at,omitempty"`
	RolledBackAt   time.Time           `json:"rolled_back_at,omitempty"`
	RollbackReason string              `json:"rollback_reason,omitempty"`
	CanaryStepIdx  int                 `json:"canary_step_idx,omitempty"` // 0-based index into schedule
	Report         *DifferentialReport `json:"report,omitempty"`
}

// State returns the current deployment state (thread-safe).
func (d *ShadowDeployment) GetState() ShadowState {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.State
}

// =============================================================================
// ShadowManager
// =============================================================================

// ShadowManager manages shadow deployments across multiple agents. It provides
// the lifecycle API: launch → evaluate → promote/rollback.
type ShadowManager struct {
	mu          sync.RWMutex
	deployments map[string]*ShadowDeployment // agentID → deployment
}

// NewShadowManager creates a manager with no active deployments.
func NewShadowManager() *ShadowManager {
	return &ShadowManager{
		deployments: make(map[string]*ShadowDeployment),
	}
}

// LaunchShadow starts a shadow deployment for the given agent. The agent's code
// runs in dark mode — 0% production traffic — while metrics are collected.
// If a deployment already exists for the agent, it returns the existing one.
//
// Spec §Dark Launch:
//   - Production traffic mirrored to shadow instance
//   - Shadow responses discarded
//   - Observation window = tier-specific default (CanarySchedule)
func (m *ShadowManager) LaunchShadow(agentID, tier string, baseline MetricsSnapshot, config ShadowConfig) (*ShadowDeployment, error) {
	if agentID == "" {
		return nil, fmt.Errorf("agentID is required")
	}
	if tier == "" {
		tier = "provisional"
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Idempotent: if a deployment exists and is not terminal, return it.
	if existing, ok := m.deployments[agentID]; ok {
		if !existing.GetState().IsTerminal() {
			return existing, nil
		}
	}

	d := &ShadowDeployment{
		AgentID:    agentID,
		Tier:       tier,
		State:      StateShadowing,
		Config:     config,
		Baseline:   baseline,
		LaunchedAt: time.Now().UTC(),
	}
	m.deployments[agentID] = d
	return d, nil
}

// GetDeployment returns the shadow deployment for an agent, or nil if none.
func (m *ShadowManager) GetDeployment(agentID string) *ShadowDeployment {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.deployments[agentID]
}

// ListDeployments returns all deployment agent IDs.
func (m *ShadowManager) ListDeployments() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var ids []string
	for id := range m.deployments {
		ids = append(ids, id)
	}
	return ids
}

// RecordShadowMetrics updates the shadow metrics snapshot for a deployment.
func (m *ShadowManager) RecordShadowMetrics(agentID string, snapshot MetricsSnapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	d, ok := m.deployments[agentID]
	if !ok {
		return fmt.Errorf("no shadow deployment for agent %q", agentID)
	}
	d.mu.Lock()
	d.Shadow = snapshot
	d.mu.Unlock()
	return nil
}

// EvaluateShadow compares the current shadow metrics against the production
// baseline using the config's thresholds. If all checks pass, the deployment
// transitions to ShadowPassed. If any fail, it transitions to ShadowFailed
// and rollback is triggered automatically (if config allows).
//
// Returns the differential report and any breach detected.
func (m *ShadowManager) EvaluateShadow(agentID string) (*DifferentialReport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	d, ok := m.deployments[agentID]
	if !ok {
		return nil, fmt.Errorf("no shadow deployment for agent %q", agentID)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.State != StateShadowing {
		return nil, fmt.Errorf("agent %q is in state %s, cannot evaluate (expected %s)", agentID, d.State, StateShadowing)
	}

	report := evaluateDifferential(d.Baseline, d.Shadow, d.Config)
	d.Report = report
	d.EvaluatedAt = time.Now().UTC()

	if report.AllPassed {
		d.State = StateShadowPassed
	} else {
		d.State = StateShadowFailed
		d.State = StateRolledBack
		d.RolledBackAt = time.Now().UTC()
		d.RollbackReason = report.BlockReason
	}

	return report, nil
}

// PromoteToCanary transitions a shadow-passed deployment to canary mode,
// routing the first step of the trust-tier-specific canary schedule.
//
// Spec §Phase 2 — Canary Verification:
//   - Provisional: 1% traffic start, 6 steps to 100%
//   - Trusted: 1% start, 3 steps
//   - Veteran: 10% start, 2 steps
func (m *ShadowManager) PromoteToCanary(agentID string) (*ShadowDeployment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	d, ok := m.deployments[agentID]
	if !ok {
		return nil, fmt.Errorf("no shadow deployment for agent %q", agentID)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.State != StateShadowPassed {
		return nil, fmt.Errorf("agent %q must pass shadow before canary (current state: %s)", agentID, d.State)
	}

	d.State = StateCanaried
	d.PromotedAt = time.Now().UTC()
	d.CanaryStepIdx = 0

	return d, nil
}

// AdvanceCanary moves the deployment to the next canary step.
// Returns the current canary step and whether this was the final step.
// If the final step is reached, the deployment transitions to Promoted.
func (m *ShadowManager) AdvanceCanary(agentID string) (CanaryStep, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	d, ok := m.deployments[agentID]
	if !ok {
		return CanaryStep{}, false, fmt.Errorf("no deployment for agent %q", agentID)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.State != StateCanaried {
		return CanaryStep{}, false, fmt.Errorf("agent %q is not in canary (state: %s)", agentID, d.State)
	}

	_, steps := CanarySchedule(d.Tier)
	nextIdx := d.CanaryStepIdx + 1

	// When the next step IS the final step (100% traffic), the deployment
	// transitions to Promoted after running it.
	if nextIdx >= len(steps)-1 {
		d.CanaryStepIdx = len(steps) - 1
		d.State = StatePromoted
		return steps[d.CanaryStepIdx], true, nil
	}

	d.CanaryStepIdx = nextIdx
	return steps[nextIdx], false, nil
}

// CurrentCanaryStep returns the active canary step for a deployment.
func (m *ShadowManager) CurrentCanaryStep(agentID string) (CanaryStep, error) {
	m.mu.RLock()
	d, ok := m.deployments[agentID]
	m.mu.RUnlock()
	if !ok {
		return CanaryStep{}, fmt.Errorf("no deployment for agent %q", agentID)
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.State != StateCanaried && d.State != StatePromoted {
		return CanaryStep{}, fmt.Errorf("agent %q is not in canary (state: %s)", agentID, d.State)
	}

	_, steps := CanarySchedule(d.Tier)
	if d.CanaryStepIdx >= len(steps) {
		return CanaryStep{}, fmt.Errorf("canary step index %d out of range (schedule has %d steps)", d.CanaryStepIdx, len(steps))
	}
	return steps[d.CanaryStepIdx], nil
}

// AutoRollback forcibly reverts a deployment to the rolled-back state,
// regardless of current phase. Used when external monitoring detects a
// breach during canary (Phase 2) or steady-state (Phase 3).
//
// Spec §Auto-Rollback Triggers:
//   - Error rate exceeds baseline by >0.1%
//   - P99 latency exceeds baseline by >20%
//   - New error types appear
//   - Memory growth >10%
//   - Security-relevant path produces different result
func (m *ShadowManager) AutoRollback(agentID, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	d, ok := m.deployments[agentID]
	if !ok {
		return fmt.Errorf("no deployment for agent %q", agentID)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.State.IsTerminal() {
		return fmt.Errorf("agent %q deployment is already terminal (%s)", agentID, d.State)
	}

	d.State = StateRolledBack
	d.RolledBackAt = time.Now().UTC()
	d.RollbackReason = reason
	return nil
}

// ObservationWindowRemaining returns how long until the observation window
// expires for a shadowing deployment. Returns 0 if the window has elapsed
// or the deployment is not shadowing.
func (m *ShadowManager) ObservationWindowRemaining(agentID string) time.Duration {
	m.mu.RLock()
	d, ok := m.deployments[agentID]
	m.mu.RUnlock()
	if !ok {
		return 0
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.State != StateShadowing {
		return 0
	}

	window := d.Config.effectiveObservationWindow(d.Tier)
	elapsed := time.Since(d.LaunchedAt)
	remaining := window - elapsed
	if remaining < 0 {
		return 0
	}
	return remaining
}

// =============================================================================
// Internal: differential evaluation
// =============================================================================

// evaluateDifferential compares shadow metrics against production baseline
// using the config thresholds. It produces a report with per-metric pass/fail.
func evaluateDifferential(prod, shadow MetricsSnapshot, cfg ShadowConfig) *DifferentialReport {
	report := &DifferentialReport{
		Production: prod,
		Shadow:     shadow,
		AllPassed:  true,
	}

	// 1. Error rate delta (spec: shadow success < prod success - 0.1%)
	successDelta := prod.SuccessRate - shadow.SuccessRate
	successPassed := successDelta <= cfg.MaxErrorRateDelta
	report.Deltas = append(report.Deltas, MetricDelta{
		Metric: "success_rate",
		Prod:   prod.SuccessRate,
		Shadow: shadow.SuccessRate,
		Delta:  successDelta,
		Passed: successPassed,
		Reason: passFailReason(successPassed, fmt.Sprintf("delta %.4f vs max %.4f", successDelta, cfg.MaxErrorRateDelta)),
	})

	// 2. P99 latency overhead (spec: shadow P99 > prod P99 * 1.2)
	latencyOverheadPct := 0.0
	if prod.P99LatencyMs > 0 {
		latencyOverheadPct = ((shadow.P99LatencyMs - prod.P99LatencyMs) / prod.P99LatencyMs) * 100
	}
	latencyPassed := latencyOverheadPct <= cfg.MaxLatencyOverheadPct
	report.Deltas = append(report.Deltas, MetricDelta{
		Metric: "p99_latency_ms",
		Prod:   prod.P99LatencyMs,
		Shadow: shadow.P99LatencyMs,
		Delta:  latencyOverheadPct,
		Passed: latencyPassed,
		Reason: passFailReason(latencyPassed, fmt.Sprintf("overhead %.1f%% vs max %.1f%%", latencyOverheadPct, cfg.MaxLatencyOverheadPct)),
	})

	// 3. New error types (spec: any new error type blocks)
	newErrPassed := shadow.NewErrorTypes <= cfg.MaxNewErrorTypes
	report.Deltas = append(report.Deltas, MetricDelta{
		Metric: "new_error_types",
		Prod:   float64(prod.NewErrorTypes),
		Shadow: float64(shadow.NewErrorTypes),
		Delta:  float64(shadow.NewErrorTypes),
		Passed: newErrPassed,
		Reason: passFailReason(newErrPassed, fmt.Sprintf("%d new types vs max %d", shadow.NewErrorTypes, cfg.MaxNewErrorTypes)),
	})

	// 4. Memory growth (spec: >10% growth blocks)
	memPassed := shadow.MemoryGrowthPct <= cfg.MaxMemoryGrowthPct
	report.Deltas = append(report.Deltas, MetricDelta{
		Metric: "memory_growth_pct",
		Prod:   0,
		Shadow: shadow.MemoryGrowthPct,
		Delta:  shadow.MemoryGrowthPct,
		Passed: memPassed,
		Reason: passFailReason(memPassed, fmt.Sprintf("%.1f%% vs max %.1f%%", shadow.MemoryGrowthPct, cfg.MaxMemoryGrowthPct)),
	})

	// Aggregate
	for _, delta := range report.Deltas {
		if !delta.Passed {
			report.AllPassed = false
			if report.BlockReason == "" {
				report.BlockReason = fmt.Sprintf("%s: %s", delta.Metric, delta.Reason)
			}
		}
	}

	return report
}

// passFailReason returns a human-readable pass/fail note.
func passFailReason(passed bool, detail string) string {
	if passed {
		return "PASS: " + detail
	}
	return "FAIL: " + detail
}
