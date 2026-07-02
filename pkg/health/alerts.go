// Package health — alert_rules.go
//
// Platform alert rules engine implementing all 5 alert rules from
// specs/SPECIFICATION.md §8.4 (Prometheus Metrics — Alert thresholds):
//
//  1. HighCostAgent  — rate(helix_agent_cost_total[1h]) > 5
//  2. GateFailureSpike — rate(helix_gate_pass_rate{gate="tier1"}[15m]) < 0.7
//  3. PRStuck        — helix_pr_cycle_time_seconds > 7200
//  4. AgentDown      — helix_agent_sandbox_uptime_seconds == 0
//  5. CostAnomaly    — helix_cost_per_pr > (avg_over_time(helix_cost_per_pr[7d]) * 3)
//
// The engine evaluates a MetricsSnapshot against configurable thresholds
// and returns AlertResults with firing/resolved state.
package health

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// ============================================================================
// Alert Severity
// ============================================================================

// AlertSeverity represents the severity level of an alert.
type AlertSeverity string

const (
	AlertCritical AlertSeverity = "critical"
	AlertWarning  AlertSeverity = "warning"
)

// ============================================================================
// Alert State
// ============================================================================

// AlertState represents whether an alert is currently firing or resolved.
type AlertState string

const (
	AlertFiring   AlertState = "firing"
	AlertResolved AlertState = "resolved"
)

// ============================================================================
// AlertRule
// ============================================================================

// AlertRule defines a single alert rule with its evaluation function.
type AlertRule struct {
	Name       string            `json:"name"`
	Expression string            `json:"expr"`
	Severity   AlertSeverity     `json:"severity"`
	Annotation string            `json:"annotation"`
	Labels     map[string]string `json:"labels,omitempty"`
}

// ============================================================================
// AlertResult
// ============================================================================

// AlertResult is the outcome of evaluating an alert rule against a metrics snapshot.
type AlertResult struct {
	Rule      AlertRule         `json:"rule"`
	State     AlertState        `json:"state"`
	Value     float64           `json:"value"`
	Threshold float64           `json:"threshold"`
	FiredAt   time.Time         `json:"fired_at,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// IsFiring returns true if the alert is currently firing.
func (r *AlertResult) IsFiring() bool {
	return r.State == AlertFiring
}

// ============================================================================
// MetricsSnapshot
// ============================================================================

// MetricsSnapshot is a point-in-time collection of platform metrics
// used for alert evaluation.
type MetricsSnapshot struct {
	// Timestamp is when the snapshot was taken.
	Timestamp time.Time `json:"timestamp"`

	// AgentCosts maps agent_id → cost in USD per hour.
	AgentCosts map[string]float64 `json:"agent_costs"`

	// GatePassRates maps gate name (tier1, tier2, chimera) → pass rate (0.0-1.0).
	GatePassRates map[string]float64 `json:"gate_pass_rates"`

	// PRCycleTimes maps repo → PR cycle time in seconds.
	PRCycleTimes map[string]float64 `json:"pr_cycle_times"`

	// AgentUptimes maps agent_id → sandbox uptime in seconds (0 = down).
	AgentUptimes map[string]float64 `json:"agent_uptimes"`

	// CostPerPR maps repo → average cost per PR in USD.
	CostPerPR map[string]float64 `json:"cost_per_pr"`

	// WeeklyAvgCostPerPR maps repo → 7-day average cost per PR in USD.
	WeeklyAvgCostPerPR map[string]float64 `json:"weekly_avg_cost_per_pr"`
}

// ============================================================================
// AlertConfig
// ============================================================================

// AlertConfig holds configurable thresholds for all alert rules.
type AlertConfig struct {
	// HighCostAgentThreshold is the cost per hour in USD that triggers HighCostAgent.
	// Default: 5.0
	HighCostAgentThreshold float64 `json:"high_cost_agent_threshold"`

	// GateFailureSpikeThreshold is the minimum gate pass rate (below which triggers).
	// Default: 0.7
	GateFailureSpikeThreshold float64 `json:"gate_failure_spike_threshold"`

	// GateFailureSpikeGate is which gate to monitor for failures.
	// Default: "tier1"
	GateFailureSpikeGate string `json:"gate_failure_spike_gate"`

	// PRStuckThreshold is the PR cycle time in seconds that triggers PRStuck.
	// Default: 7200 (2 hours)
	PRStuckThreshold float64 `json:"pr_stuck_threshold"`

	// CostAnomalyMultiplier triggers when a PR cost exceeds weekly average by this factor.
	// Default: 3.0
	CostAnomalyMultiplier float64 `json:"cost_anomaly_multiplier"`
}

// DefaultAlertConfig returns the spec-defined default thresholds.
func DefaultAlertConfig() AlertConfig {
	return AlertConfig{
		HighCostAgentThreshold:    5.0,
		GateFailureSpikeThreshold: 0.7,
		GateFailureSpikeGate:      "tier1",
		PRStuckThreshold:          7200,
		CostAnomalyMultiplier:     3.0,
	}
}

// ============================================================================
// AlertEngine
// ============================================================================

// AlertEngine evaluates alert rules against metrics snapshots.
type AlertEngine struct {
	mu             sync.RWMutex
	config         AlertConfig
	previousFiring map[string]bool // rule name → was firing last evaluation
}

// NewAlertEngine creates a new AlertEngine with default config.
func NewAlertEngine() *AlertEngine {
	return NewAlertEngineWithConfig(DefaultAlertConfig())
}

// NewAlertEngineWithConfig creates an AlertEngine with custom thresholds.
func NewAlertEngineWithConfig(config AlertConfig) *AlertEngine {
	return &AlertEngine{
		config:         config,
		previousFiring: make(map[string]bool),
	}
}

// Config returns the current alert configuration.
func (e *AlertEngine) Config() AlertConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.config
}

// SetConfig updates the alert configuration.
func (e *AlertEngine) SetConfig(config AlertConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config = config
}

// EvaluateRules evaluates all alert rules against a metrics snapshot.
func (e *AlertEngine) EvaluateRules(snapshot MetricsSnapshot) []AlertResult {
	e.mu.Lock()
	defer e.mu.Unlock()

	var results []AlertResult

	// 1. HighCostAgent
	for agent, cost := range snapshot.AgentCosts {
		results = append(results, e.evalHighCostAgent(agent, cost, snapshot.Timestamp))
	}

	// 2. GateFailureSpike
	if rate, ok := snapshot.GatePassRates[e.config.GateFailureSpikeGate]; ok {
		results = append(results, e.evalGateFailureSpike(rate, snapshot.Timestamp))
	}

	// 3. PRStuck
	for repo, cycleTime := range snapshot.PRCycleTimes {
		results = append(results, e.evalPRStuck(repo, cycleTime, snapshot.Timestamp))
	}

	// 4. AgentDown
	for agent, uptime := range snapshot.AgentUptimes {
		results = append(results, e.evalAgentDown(agent, uptime, snapshot.Timestamp))
	}

	// 5. CostAnomaly
	for repo, cost := range snapshot.CostPerPR {
		if avg, ok := snapshot.WeeklyAvgCostPerPR[repo]; ok && avg > 0 {
			results = append(results, e.evalCostAnomaly(repo, cost, avg, snapshot.Timestamp))
		}
	}

	// Sort results by rule name for deterministic output
	sort.Slice(results, func(i, j int) bool {
		return results[i].Rule.Name < results[j].Rule.Name
	})

	return results
}

// ============================================================================
// Individual Rule Evaluators
// ============================================================================

func (e *AlertEngine) evalHighCostAgent(agent string, costPerHour float64, ts time.Time) AlertResult {
	rule := AlertRule{
		Name:       "HighCostAgent",
		Expression: fmt.Sprintf("rate(helix_agent_cost_total{agent=%q}[1h]) > %g", agent, e.config.HighCostAgentThreshold),
		Severity:   AlertWarning,
		Annotation: fmt.Sprintf("Agent %s spending >$%g/hr", agent, e.config.HighCostAgentThreshold),
		Labels:     map[string]string{"agent": agent},
	}

	result := AlertResult{
		Rule:      rule,
		Value:     costPerHour,
		Threshold: e.config.HighCostAgentThreshold,
		Labels:    map[string]string{"agent": agent},
	}

	if costPerHour > e.config.HighCostAgentThreshold {
		result.State = AlertFiring
		result.FiredAt = ts
	} else {
		result.State = AlertResolved
	}

	return result
}

func (e *AlertEngine) evalGateFailureSpike(passRate float64, ts time.Time) AlertResult {
	rule := AlertRule{
		Name:       "GateFailureSpike",
		Expression: fmt.Sprintf("rate(helix_gate_pass_rate{gate=%q}[15m]) < %g", e.config.GateFailureSpikeGate, e.config.GateFailureSpikeThreshold),
		Severity:   AlertCritical,
		Annotation: fmt.Sprintf("%s pass rate dropped below %.0f%%", e.config.GateFailureSpikeGate, e.config.GateFailureSpikeThreshold*100),
		Labels:     map[string]string{"gate": e.config.GateFailureSpikeGate},
	}

	result := AlertResult{
		Rule:      rule,
		Value:     passRate,
		Threshold: e.config.GateFailureSpikeThreshold,
		Labels:    map[string]string{"gate": e.config.GateFailureSpikeGate},
	}

	if passRate < e.config.GateFailureSpikeThreshold {
		result.State = AlertFiring
		result.FiredAt = ts
	} else {
		result.State = AlertResolved
	}

	return result
}

func (e *AlertEngine) evalPRStuck(repo string, cycleTimeSec float64, ts time.Time) AlertResult {
	rule := AlertRule{
		Name:       "PRStuck",
		Expression: fmt.Sprintf("helix_pr_cycle_time_seconds > %g", e.config.PRStuckThreshold),
		Severity:   AlertWarning,
		Annotation: fmt.Sprintf("PR %s in review >%s", repo, formatDuration(time.Duration(e.config.PRStuckThreshold)*time.Second)),
		Labels:     map[string]string{"repo": repo},
	}

	result := AlertResult{
		Rule:      rule,
		Value:     cycleTimeSec,
		Threshold: e.config.PRStuckThreshold,
		Labels:    map[string]string{"repo": repo},
	}

	if cycleTimeSec > e.config.PRStuckThreshold {
		result.State = AlertFiring
		result.FiredAt = ts
	} else {
		result.State = AlertResolved
	}

	return result
}

func (e *AlertEngine) evalAgentDown(agent string, uptimeSec float64, ts time.Time) AlertResult {
	rule := AlertRule{
		Name:       "AgentDown",
		Expression: fmt.Sprintf("helix_agent_sandbox_uptime_seconds{agent=%q} == 0", agent),
		Severity:   AlertCritical,
		Annotation: fmt.Sprintf("Agent %s container not running", agent),
		Labels:     map[string]string{"agent": agent},
	}

	result := AlertResult{
		Rule:      rule,
		Value:     uptimeSec,
		Threshold: 0,
		Labels:    map[string]string{"agent": agent},
	}

	if uptimeSec == 0 {
		result.State = AlertFiring
		result.FiredAt = ts
	} else {
		result.State = AlertResolved
	}

	return result
}

func (e *AlertEngine) evalCostAnomaly(repo string, costPerPR, weeklyAvg float64, ts time.Time) AlertResult {
	threshold := weeklyAvg * e.config.CostAnomalyMultiplier
	rule := AlertRule{
		Name:       "CostAnomaly",
		Expression: fmt.Sprintf("helix_cost_per_pr{repo=%q} > (%g * %g)", repo, weeklyAvg, e.config.CostAnomalyMultiplier),
		Severity:   AlertWarning,
		Annotation: fmt.Sprintf("PR cost %s 3x above weekly average", repo),
		Labels:     map[string]string{"repo": repo},
	}

	result := AlertResult{
		Rule:      rule,
		Value:     costPerPR,
		Threshold: threshold,
		Labels:    map[string]string{"repo": repo},
	}

	if costPerPR > threshold {
		result.State = AlertFiring
		result.FiredAt = ts
	} else {
		result.State = AlertResolved
	}

	return result
}

// ============================================================================
// AlertSummary
// ============================================================================

// AlertSummary aggregates alert results for quick reporting.
type AlertSummary struct {
	Total    int           `json:"total"`
	Firing   int           `json:"firing"`
	Resolved int           `json:"resolved"`
	Critical int           `json:"critical_firing"`
	Warning  int           `json:"warning_firing"`
	Results  []AlertResult `json:"results"`
}

// SummarizeResults creates a summary from a slice of alert results.
func SummarizeResults(results []AlertResult) AlertSummary {
	s := AlertSummary{Results: results, Total: len(results)}
	for _, r := range results {
		if r.IsFiring() {
			s.Firing++
			switch r.Rule.Severity {
			case AlertCritical:
				s.Critical++
			case AlertWarning:
				s.Warning++
			}
		} else {
			s.Resolved++
		}
	}
	return s
}

// HasFiring returns true if any alert is firing.
func (s AlertSummary) HasFiring() bool {
	return s.Firing > 0
}

// HasCritical returns true if any critical alert is firing.
func (s AlertSummary) HasCritical() bool {
	return s.Critical > 0
}

// FormatSummary renders a human-readable summary string.
func (s AlertSummary) FormatSummary() string {
	if s.HasCritical() {
		return fmt.Sprintf("⚠️ %d critical, %d warning alerts firing (%d total alerts)", s.Critical, s.Warning, s.Total)
	}
	if s.HasFiring() {
		return fmt.Sprintf("⚠️ %d warning alerts firing (%d total alerts)", s.Warning, s.Total)
	}
	return fmt.Sprintf("✓ All %d alerts resolved", s.Total)
}

// ============================================================================
// Helpers
// ============================================================================

// formatDuration converts a duration to a human-readable string.
func formatDuration(d time.Duration) string {
	if d >= time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	if d >= time.Minute {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

// NewMetricsSnapshot creates an empty metrics snapshot with initialized maps.
func NewMetricsSnapshot() MetricsSnapshot {
	return MetricsSnapshot{
		Timestamp:          time.Now(),
		AgentCosts:         make(map[string]float64),
		GatePassRates:      make(map[string]float64),
		PRCycleTimes:       make(map[string]float64),
		AgentUptimes:       make(map[string]float64),
		CostPerPR:          make(map[string]float64),
		WeeklyAvgCostPerPR: make(map[string]float64),
	}
}
