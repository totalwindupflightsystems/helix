package verify

// surveillance.go implements Phase 3 steady-state surveillance per
// specs/production-verification.md §Phase 3 — Steady-State Surveillance (72h+).
//
// After shadow verification (Phase 1) and canary verification (Phase 2), the
// surveillance system runs continuously:
//
//  1. Aggregates metrics from multiple sources into time-windowed views
//  2. Evaluates behavior contracts periodically
//  3. Detects gradual degradation over 7-day windows
//  4. Escalates alerts when sustained drift persists beyond thresholds
//
// This integrates with the existing DriftDetector (immediate drift),
// BehaviorContract/Monitor (breach detection), and NotificationDispatcher
// (multi-channel alerts).

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// ============================================================================
// Surveillance Status and Events
// ============================================================================

// SurveillanceStatus classifies the health of an agent under surveillance.
type SurveillanceStatus string

const (
	StatusHealthy  SurveillanceStatus = "healthy"  // all contracts pass, no drift
	StatusWarning  SurveillanceStatus = "warning"  // minor drift detected, no breach
	StatusDegraded SurveillanceStatus = "degraded" // gradual degradation over long window
	StatusBreach   SurveillanceStatus = "breach"   // behavior contract breach detected
)

// SurveillanceEvent is emitted on every periodic surveillance check.
// Callers consume events from the Events() channel or the Events slice.
type SurveillanceEvent struct {
	Timestamp       time.Time          `json:"timestamp"`
	ContractName    string             `json:"contract_name"`
	AgentID         string             `json:"agent_id"`
	Status          SurveillanceStatus `json:"status"`
	Metrics         MetricsSnapshot    `json:"metrics"`
	Breaches        []Breach           `json:"breaches,omitempty"`
	DriftAssessment *DriftAssessment   `json:"drift_assessment,omitempty"`
	Degradation     *DegradationReport `json:"degradation,omitempty"`
	EscalationLevel EscalationLevel    `json:"escalation_level"`
	Message         string             `json:"message"`
}

// IsActionable returns true if the event requires intervention.
func (e *SurveillanceEvent) IsActionable() bool {
	return e.Status == StatusBreach || e.Status == StatusDegraded ||
		e.EscalationLevel == EscalationInvestigate ||
		e.EscalationLevel == EscalationRollback
}

// Summary returns a one-line human-readable summary.
func (e *SurveillanceEvent) Summary() string {
	breachCount := len(e.Breaches)
	driftBreach := false
	if e.DriftAssessment != nil {
		driftBreach = e.DriftAssessment.AnyBreach
	}
	return fmt.Sprintf("[%s] agent=%s contract=%s breaches=%d drift_breach=%v escalation=%s",
		e.Status, e.AgentID, e.ContractName, breachCount, driftBreach, e.EscalationLevel)
}

// ============================================================================
// Daily Summary — long-window aggregation
// ============================================================================

// DailySummary is an aggregated snapshot of metrics for a single calendar day.
// The LongRunningMonitor uses these to detect gradual degradation trends.
type DailySummary struct {
	Date           time.Time `json:"date"`
	AvgSuccessRate float64   `json:"avg_success_rate"`
	AvgP99Latency  float64   `json:"avg_p99_latency_ms"`
	AvgP50Latency  float64   `json:"avg_p50_latency_ms"`
	TotalErrors    int64     `json:"total_errors"`
	TotalRequests  int64     `json:"total_requests"`
	BreachCount    int       `json:"breach_count"`
	SampleCount    int       `json:"sample_count"`
}

// ============================================================================
// Degradation Report — output of LongRunningMonitor
// ============================================================================

// DegradationDirection indicates the trend direction of a metric over the
// long observation window.
type DegradationDirection string

const (
	DegradationNone      DegradationDirection = "none"
	DegradationWorsening DegradationDirection = "worsening"
	DegradationImproving DegradationDirection = "improving"
)

// MetricDegradation tracks a single metric's gradual degradation.
type MetricDegradation struct {
	Metric       string               `json:"metric"`
	FirstDay     float64              `json:"first_day"`
	LastDay      float64              `json:"last_day"`
	ChangePct    float64              `json:"change_pct"`
	ThresholdPct float64              `json:"threshold_pct"`
	Exceeds      bool                 `json:"exceeds"`
	Direction    DegradationDirection `json:"direction"`
}

// DegradationReport aggregates per-metric degradation analysis for an agent
// over the long observation window (default 7 days).
type DegradationReport struct {
	AgentID         string              `json:"agent_id"`
	WindowStart     time.Time           `json:"window_start"`
	WindowEnd       time.Time           `json:"window_end"`
	WindowDays      int                 `json:"window_days"`
	MetricTrends    []MetricDegradation `json:"metric_trends"`
	IsDegrading     bool                `json:"is_degrading"`
	Severity        BreachSeverity      `json:"severity"`
	AffectedMetrics int                 `json:"affected_metrics"`
}

// Summary returns a human-readable degradation summary.
func (d *DegradationReport) Summary() string {
	return fmt.Sprintf("agent %s over %dd: %d/%d metrics degrading, severity=%s",
		d.AgentID, d.WindowDays, d.AffectedMetrics, len(d.MetricTrends), d.Severity)
}

// ============================================================================
// Escalation
// ============================================================================

// EscalationLevel tracks how far an alert has been escalated.
type EscalationLevel string

const (
	EscalationNone        EscalationLevel = "none"
	EscalationNotify      EscalationLevel = "notify"
	EscalationInvestigate EscalationLevel = "investigate"
	EscalationRollback    EscalationLevel = "rollback"
)

// IsActionable returns true if the escalation level requires intervention.
func (l EscalationLevel) IsActionable() bool {
	return l == EscalationInvestigate || l == EscalationRollback
}

// ============================================================================
// Configuration Types
// ============================================================================

// SurveillanceConfig holds all configurable thresholds for steady-state
// surveillance.
type SurveillanceConfig struct {
	// CheckInterval is how often contracts are evaluated against fresh metrics.
	// Default: 5 minutes.
	CheckInterval time.Duration `json:"check_interval"`

	// LongWindowDays is the number of days used for gradual degradation analysis.
	// Spec: 7-day windows. Default: 7.
	LongWindowDays int `json:"long_window_days"`

	// Sensitivity controls per-metric drift thresholds for short-window drift.
	Sensitivity DriftSensitivity `json:"sensitivity"`

	// LongRunningThresholds controls gradual degradation detection.
	LongRunningThresholds LongRunningThresholds `json:"long_running_thresholds"`

	// SustainedDriftDuration is how long drift must persist before escalation.
	// Default: 30 minutes.
	SustainedDriftDuration time.Duration `json:"sustained_drift_duration"`

	// EscalationRollbackDuration is how long sustained drift must persist before
	// auto-rollback escalation. Default: 2 hours.
	EscalationRollbackDuration time.Duration `json:"escalation_rollback_duration"`

	// MaxDailySummaries limits how many daily summaries are retained.
	// Default: 14 (2 weeks).
	MaxDailySummaries int `json:"max_daily_summaries"`

	// MaxRecentSamples limits the short-window sample buffer.
	// Default: 288 (24h at 5-minute intervals).
	MaxRecentSamples int `json:"max_recent_samples"`

	// MaxEvents limits the stored event log.
	// Default: 1000.
	MaxEvents int `json:"max_events"`
}

// DefaultSurveillanceConfig returns spec-compliant surveillance settings.
func DefaultSurveillanceConfig() SurveillanceConfig {
	return SurveillanceConfig{
		CheckInterval:              5 * time.Minute,
		LongWindowDays:             7,
		Sensitivity:                DefaultSensitivity(),
		LongRunningThresholds:      DefaultLongRunningThresholds(),
		SustainedDriftDuration:     30 * time.Minute,
		EscalationRollbackDuration: 2 * time.Hour,
		MaxDailySummaries:          14,
		MaxRecentSamples:           288,
		MaxEvents:                  1000,
	}
}

// LongRunningThresholds defines what constitutes gradual degradation over the
// long observation window (spec: 7-day windows).
type LongRunningThresholds struct {
	// SuccessRateDeclinePct: if success rate declined by this % or more over
	// the window, it's degraded. Spec implies gradual degradation detection.
	// Default: 5.0.
	SuccessRateDeclinePct float64 `json:"success_rate_decline_pct"`

	// LatencyIncreasePct: if P99 latency increased by this % or more.
	// Default: 20.0.
	LatencyIncreasePct float64 `json:"latency_increase_pct"`

	// ErrorRateIncreasePct: if error rate increased by this % or more.
	// Default: 50.0.
	ErrorRateIncreasePct float64 `json:"error_rate_increase_pct"`

	// MemoryGrowthPct: if memory grew by this % or more.
	// Default: 15.0.
	MemoryGrowthPct float64 `json:"memory_growth_pct"`
}

// DefaultLongRunningThresholds returns spec-compliant degradation thresholds.
func DefaultLongRunningThresholds() LongRunningThresholds {
	return LongRunningThresholds{
		SuccessRateDeclinePct: 5.0,
		LatencyIncreasePct:    20.0,
		ErrorRateIncreasePct:  50.0,
		MemoryGrowthPct:       15.0,
	}
}

// ============================================================================
// agentWindow — per-agent rolling metrics storage
// ============================================================================

// agentWindow holds rolling metrics for a single agent across time scales.
type agentWindow struct {
	agentID  string
	baseline MetricsSnapshot
	// Recent samples for short-window drift analysis.
	recentSamples []timestampedSample
	// Daily summaries for long-window degradation analysis.
	dailySummaries []DailySummary
	// Per-agent drift detector.
	drift *DriftDetector
}

// addSample adds a metric sample to the agent's window.
func (w *agentWindow) addSample(snapshot MetricsSnapshot, maxSamples int) {
	sample := timestampedSample{snapshot: snapshot, time: snapshot.Timestamp}
	if sample.time.IsZero() {
		sample.time = time.Now().UTC()
	}
	w.recentSamples = append(w.recentSamples, sample)
	if len(w.recentSamples) > maxSamples {
		w.recentSamples = w.recentSamples[len(w.recentSamples)-maxSamples:]
	}
	if w.drift != nil {
		w.drift.RecordSampleAt(snapshot, sample.time)
	}
}

// summarizeDay aggregates samples from a given day into a DailySummary.
func (w *agentWindow) summarizeDay(day time.Time) DailySummary {
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	dayEnd := dayStart.Add(24 * time.Hour)

	var sum DailySummary
	sum.Date = dayStart
	var totalSuccess, totalP99, totalP50 float64
	var count int

	for _, s := range w.recentSamples {
		if !s.time.Before(dayStart) && s.time.Before(dayEnd) {
			totalSuccess += s.snapshot.SuccessRate
			totalP99 += s.snapshot.P99LatencyMs
			totalP50 += s.snapshot.P50LatencyMs
			sum.TotalErrors += s.snapshot.ErrorCount
			sum.TotalRequests += s.snapshot.RequestCount
			count++
		}
	}

	if count > 0 {
		sum.AvgSuccessRate = totalSuccess / float64(count)
		sum.AvgP99Latency = totalP99 / float64(count)
		sum.AvgP50Latency = totalP50 / float64(count)
	}
	sum.SampleCount = count
	return sum
}

// rollupDailySummaries aggregates recent samples into daily summaries,
// only creating summaries for days not already summarized.
func (w *agentWindow) rollupDailySummaries(maxSummaries int) {
	if len(w.recentSamples) == 0 {
		return
	}

	// Find the set of days we have samples for.
	seen := make(map[string]bool)
	for _, s := range w.recentSamples {
		day := s.time.Format("2006-01-02")
		seen[day] = true
	}

	// Check which days we've already summarized.
	summarizedDays := make(map[string]bool)
	for _, ds := range w.dailySummaries {
		day := ds.Date.Format("2006-01-02")
		summarizedDays[day] = true
	}

	// Create new daily summaries for unsummarized days.
	for day := range seen {
		if summarizedDays[day] {
			continue
		}
		dayTime, err := time.Parse("2006-01-02", day)
		if err != nil {
			continue
		}
		w.dailySummaries = append(w.dailySummaries, w.summarizeDay(dayTime))
	}

	// Sort and trim.
	if len(w.dailySummaries) > maxSummaries {
		w.dailySummaries = w.dailySummaries[len(w.dailySummaries)-maxSummaries:]
	}
}

// recentBreaches returns how many breaches occurred in the last 24h.
func (w *agentWindow) recentBreaches() int {
	count := 0
	for _, ds := range w.dailySummaries {
		if time.Since(ds.Date) < 24*time.Hour {
			count += ds.BreachCount
		}
	}
	return count
}

// ============================================================================
// SteadyStateAggregator
// ============================================================================

// SteadyStateAggregator runs continuous behavior contract surveillance on
// deployed agents. It aggregates metrics across multiple time windows,
// evaluates contracts periodically, detects gradual degradation over long
// windows, and escalates alerts when sustained drift persists.
type SteadyStateAggregator struct {
	mu         sync.RWMutex
	config     SurveillanceConfig
	monitor    *Monitor
	dispatcher *NotificationDispatcher

	// Per-agent windowed metric storage.
	windows map[string]*agentWindow

	// Per-agent surveillance start time.
	started map[string]time.Time

	// Per-agent escalation tracking (delegated to AlertEscalation).
	escalation *AlertEscalation

	// Long-running degradation monitor.
	longRunning *LongRunningMonitor

	// Event log.
	events    []SurveillanceEvent
	maxEvents int
}

// SurveillanceOption configures a SteadyStateAggregator.
type SurveillanceOption func(*SteadyStateAggregator)

// WithSurveillanceConfig overrides the default config.
func WithSurveillanceConfig(c SurveillanceConfig) SurveillanceOption {
	return func(s *SteadyStateAggregator) { s.config = c }
}

// WithSurveillanceMonitor injects a pre-configured Monitor.
func WithSurveillanceMonitor(m *Monitor) SurveillanceOption {
	return func(s *SteadyStateAggregator) { s.monitor = m }
}

// WithSurveillanceDispatcher injects a pre-configured NotificationDispatcher.
func WithSurveillanceDispatcher(d *NotificationDispatcher) SurveillanceOption {
	return func(s *SteadyStateAggregator) { s.dispatcher = d }
}

// NewSteadyStateAggregator creates a surveillance aggregator with the given
// baseline metrics per agent and default configuration.
func NewSteadyStateAggregator(opts ...SurveillanceOption) *SteadyStateAggregator {
	cfg := DefaultSurveillanceConfig()
	s := &SteadyStateAggregator{
		config:      cfg,
		monitor:     NewMonitor(),
		windows:     make(map[string]*agentWindow),
		started:     make(map[string]time.Time),
		escalation:  NewAlertEscalation(cfg.SustainedDriftDuration, cfg.EscalationRollbackDuration),
		longRunning: NewLongRunningMonitor(cfg.LongWindowDays, cfg.LongRunningThresholds),
		events:      make([]SurveillanceEvent, 0, cfg.MaxEvents),
		maxEvents:   cfg.MaxEvents,
	}
	for _, opt := range opts {
		opt(s)
	}
	// Re-sync maxEvents in case config was overridden.
	s.maxEvents = s.config.MaxEvents
	if s.escalation == nil {
		s.escalation = NewAlertEscalation(s.config.SustainedDriftDuration, s.config.EscalationRollbackDuration)
	}
	if s.longRunning == nil {
		s.longRunning = NewLongRunningMonitor(s.config.LongWindowDays, s.config.LongRunningThresholds)
	}
	return s
}

// RegisterAgent begins surveillance for an agent with the given baseline metrics.
func (s *SteadyStateAggregator) RegisterAgent(agentID string, baseline MetricsSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.windows[agentID] = &agentWindow{
		agentID:       agentID,
		baseline:      baseline,
		recentSamples: make([]timestampedSample, 0, s.config.MaxRecentSamples),
		drift:         NewDriftDetector(baseline, WithSensitivity(s.config.Sensitivity), WithMaxSamples(s.config.MaxRecentSamples)),
	}
	s.started[agentID] = time.Now().UTC()
}

// UnregisterAgent stops surveillance for an agent.
func (s *SteadyStateAggregator) UnregisterAgent(agentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.windows, agentID)
	delete(s.started, agentID)
	s.escalation.Reset(agentID)
}

// RegisteredAgents returns the list of agents under surveillance.
func (s *SteadyStateAggregator) RegisteredAgents() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	agents := make([]string, 0, len(s.windows))
	for id := range s.windows {
		agents = append(agents, id)
	}
	return agents
}

// RegisterContract adds a behavior contract to the monitor.
func (s *SteadyStateAggregator) RegisterContract(c *BehaviorContract) {
	s.monitor.RegisterContract(c)
}

// UnregisterContract removes a contract from the monitor.
func (s *SteadyStateAggregator) UnregisterContract(name string) {
	s.monitor.UnregisterContract(name)
}

// RecordMetrics submits fresh metrics for an agent. This is called by the
// surveillance loop (or manually for testing) to feed new data into the
// aggregator.
func (s *SteadyStateAggregator) RecordMetrics(agentID string, snapshot MetricsSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()

	w, ok := s.windows[agentID]
	if !ok {
		return
	}
	w.addSample(snapshot, s.config.MaxRecentSamples)
}

// ============================================================================
// Surveillance Check — the core evaluation cycle
// ============================================================================

// CheckAgent runs a full surveillance evaluation cycle for a single agent.
// It evaluates contracts, checks drift, detects gradual degradation, and
// updates escalation level. Returns the emitted SurveillanceEvent.
func (s *SteadyStateAggregator) CheckAgent(agentID string, metrics map[string]float64) *SurveillanceEvent {
	s.mu.Lock()
	w, ok := s.windows[agentID]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	baseline := w.baseline

	// Roll up daily summaries.
	w.rollupDailySummaries(s.config.MaxDailySummaries)

	// Get daily summaries for long-running analysis.
	dailySummaries := make([]DailySummary, len(w.dailySummaries))
	copy(dailySummaries, w.dailySummaries)
	s.mu.Unlock()

	now := time.Now().UTC()
	event := &SurveillanceEvent{
		Timestamp:       now,
		AgentID:         agentID,
		EscalationLevel: EscalationNone,
		Status:          StatusHealthy,
	}

	// 1. Evaluate behavior contracts → breaches.
	breaches := s.monitor.Evaluate(metrics)
	if len(breaches) > 0 {
		event.Breaches = breaches
		event.Status = StatusBreach

		// Increment breach count on today's daily summary.
		s.mu.Lock()
		if w, ok := s.windows[agentID]; ok {
			today := time.Now().UTC()
			dayKey := today.Format("2006-01-02")
			for i := range w.dailySummaries {
				if w.dailySummaries[i].Date.Format("2006-01-02") == dayKey {
					w.dailySummaries[i].BreachCount += len(breaches)
					break
				}
			}
		}
		s.mu.Unlock()

		// Dispatch notifications if configured.
		if s.dispatcher != nil {
			for _, b := range breaches {
				s.dispatcher.NotifyFromBreach(&b, metrics, nil)
			}
		}
	}

	// 2. Check short-window drift.
	snapshot := metricsFromMap(metrics, baseline)
	if w.drift != nil {
		s.mu.RLock()
		drift := w.drift
		s.mu.RUnlock()
		drift.RecordSample(snapshot)
		assessment := drift.AssessLatest()
		if assessment != nil {
			event.DriftAssessment = assessment
			if assessment.AnyBreach && event.Status == StatusHealthy {
				event.Status = StatusWarning
			}
		}
	}

	// 3. Check gradual degradation over long window.
	if len(dailySummaries) >= 2 {
		degradation := s.longRunning.Analyze(agentID, dailySummaries)
		if degradation != nil && degradation.IsDegrading {
			event.Degradation = degradation
			if event.Status == StatusHealthy || event.Status == StatusWarning {
				event.Status = StatusDegraded
			}
		}
	}

	// 4. Update escalation based on sustained drift / breach.
	escalation := s.escalation.Evaluate(agentID, event.Status, event.DriftAssessment)
	event.EscalationLevel = escalation

	// Determine message.
	event.Metrics = snapshot
	event.Message = s.eventMessage(event)

	// Record event.
	s.recordEvent(*event)

	return event
}

// CheckAll runs a surveillance check for all registered agents.
// metricsPerAgent maps agentID → metrics map for the current check cycle.
func (s *SteadyStateAggregator) CheckAll(metricsPerAgent map[string]map[string]float64) []*SurveillanceEvent {
	s.mu.RLock()
	agents := make([]string, 0, len(s.windows))
	for id := range s.windows {
		agents = append(agents, id)
	}
	s.mu.RUnlock()

	events := make([]*SurveillanceEvent, 0, len(agents))
	for _, agentID := range agents {
		m, ok := metricsPerAgent[agentID]
		if !ok {
			continue
		}
		ev := s.CheckAgent(agentID, m)
		if ev != nil {
			events = append(events, ev)
		}
	}
	return events
}

// ============================================================================
// Event Log
// ============================================================================

// recordEvent appends to the event log, trimming if over capacity.
func (s *SteadyStateAggregator) recordEvent(e SurveillanceEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
	if len(s.events) > s.maxEvents {
		s.events = s.events[len(s.events)-s.maxEvents:]
	}
}

// Events returns a copy of the event log.
func (s *SteadyStateAggregator) Events() []SurveillanceEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]SurveillanceEvent, len(s.events))
	copy(result, s.events)
	return result
}

// RecentEvents returns events from the last N minutes.
func (s *SteadyStateAggregator) RecentEvents(window time.Duration) []SurveillanceEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cutoff := time.Now().UTC().Add(-window)
	var result []SurveillanceEvent
	for _, e := range s.events {
		if e.Timestamp.After(cutoff) {
			result = append(result, e)
		}
	}
	return result
}

// EventsForAgent returns all events for a specific agent.
func (s *SteadyStateAggregator) EventsForAgent(agentID string) []SurveillanceEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []SurveillanceEvent
	for _, e := range s.events {
		if e.AgentID == agentID {
			result = append(result, e)
		}
	}
	return result
}

// EventCount returns the total number of recorded events.
func (s *SteadyStateAggregator) EventCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.events)
}

// ClearEvents empties the event log.
func (s *SteadyStateAggregator) ClearEvents() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = s.events[:0]
}

// ============================================================================
// Agent Status Queries
// ============================================================================

// AgentStatus returns the current surveillance status for an agent.
type AgentStatus struct {
	AgentID           string             `json:"agent_id"`
	Status            SurveillanceStatus `json:"status"`
	EscalationLevel   EscalationLevel    `json:"escalation_level"`
	LastEvent         *SurveillanceEvent `json:"last_event,omitempty"`
	SurveillanceStart time.Time          `json:"surveillance_start"`
	DailySummaryCount int                `json:"daily_summary_count"`
	RecentSampleCount int                `json:"recent_sample_count"`
}

// GetAgentStatus returns the current status of an agent under surveillance.
func (s *SteadyStateAggregator) GetAgentStatus(agentID string) *AgentStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	w, ok := s.windows[agentID]
	if !ok {
		return nil
	}

	status := &AgentStatus{
		AgentID:           agentID,
		Status:            StatusHealthy,
		EscalationLevel:   s.escalation.GetLevel(agentID),
		SurveillanceStart: s.started[agentID],
		DailySummaryCount: len(w.dailySummaries),
		RecentSampleCount: len(w.recentSamples),
	}

	// Find last event for this agent.
	for i := len(s.events) - 1; i >= 0; i-- {
		if s.events[i].AgentID == agentID {
			ev := s.events[i]
			status.LastEvent = &ev
			status.Status = ev.Status
			break
		}
	}

	return status
}

// DailySummaries returns all daily summaries for an agent.
func (s *SteadyStateAggregator) DailySummaries(agentID string) []DailySummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	w, ok := s.windows[agentID]
	if !ok {
		return nil
	}
	result := make([]DailySummary, len(w.dailySummaries))
	copy(result, w.dailySummaries)
	return result
}

// ============================================================================
// Helpers
// ============================================================================

// eventMessage generates a human-readable message for a surveillance event.
func (s *SteadyStateAggregator) eventMessage(e *SurveillanceEvent) string {
	switch e.Status {
	case StatusBreach:
		return fmt.Sprintf("behavior contract breach: %d breach(es) for agent %s",
			len(e.Breaches), e.AgentID)
	case StatusDegraded:
		if e.Degradation != nil {
			return e.Degradation.Summary()
		}
		return fmt.Sprintf("gradual degradation detected for agent %s", e.AgentID)
	case StatusWarning:
		if e.DriftAssessment != nil {
			return fmt.Sprintf("drift detected: %s", e.DriftAssessment.Summary())
		}
		return fmt.Sprintf("minor drift for agent %s", e.AgentID)
	default:
		return fmt.Sprintf("agent %s healthy — all contracts passing", e.AgentID)
	}
}

// metricsFromMap constructs a MetricsSnapshot from a metrics map and baseline.
func metricsFromMap(m map[string]float64, baseline MetricsSnapshot) MetricsSnapshot {
	snap := MetricsSnapshot{
		Timestamp: time.Now().UTC(),
	}
	// Copy baseline for fields not in the map.
	snap.SuccessRate = baseline.SuccessRate
	snap.P99LatencyMs = baseline.P99LatencyMs
	snap.P50LatencyMs = baseline.P50LatencyMs
	snap.MemoryGrowthPct = baseline.MemoryGrowthPct

	// Override with provided values.
	if v, ok := m["success_rate"]; ok {
		snap.SuccessRate = v
	}
	if v, ok := m["p99_latency_ms"]; ok {
		snap.P99LatencyMs = v
	}
	if v, ok := m["p50_latency_ms"]; ok {
		snap.P50LatencyMs = v
	}
	if v, ok := m["error_count"]; ok {
		snap.ErrorCount = int64(v)
	}
	if v, ok := m["new_error_types"]; ok {
		snap.NewErrorTypes = int(v)
	}
	if v, ok := m["memory_growth_pct"]; ok {
		snap.MemoryGrowthPct = v
	}
	if v, ok := m["request_count"]; ok {
		snap.RequestCount = int64(v)
	}
	return snap
}

// ============================================================================
// LongRunningMonitor — Gradual Degradation Detection
// ============================================================================

// LongRunningMonitor detects gradual degradation over 7-day windows by
// analyzing daily summaries. It compares the first and last day in the window
// and flags metrics that have degraded beyond configured thresholds.
type LongRunningMonitor struct {
	mu         sync.RWMutex
	windowDays int
	thresholds LongRunningThresholds
	// Per-agent degradation reports (most recent).
	reports map[string]*DegradationReport
}

// NewLongRunningMonitor creates a monitor with the given window size and thresholds.
func NewLongRunningMonitor(windowDays int, thresholds LongRunningThresholds) *LongRunningMonitor {
	if windowDays <= 0 {
		windowDays = 7
	}
	return &LongRunningMonitor{
		windowDays: windowDays,
		thresholds: thresholds,
		reports:    make(map[string]*DegradationReport),
	}
}

// Analyze evaluates daily summaries for gradual degradation.
// Returns a DegradationReport if degradation is detected, or nil if the
// agent is stable or there's insufficient data (fewer than 2 daily summaries).
func (m *LongRunningMonitor) Analyze(agentID string, summaries []DailySummary) *DegradationReport {
	if len(summaries) < 2 {
		return nil
	}

	// Use the most recent N daily summaries (up to windowDays).
	start := 0
	if len(summaries) > m.windowDays {
		start = len(summaries) - m.windowDays
	}
	window := summaries[start:]

	first := window[0]
	last := window[len(window)-1]

	report := &DegradationReport{
		AgentID:     agentID,
		WindowStart: first.Date,
		WindowEnd:   last.Date,
		WindowDays:  len(window),
		Severity:    SeverityNone,
	}

	t := m.thresholds

	// Success rate decline.
	if first.AvgSuccessRate > 0 {
		change := pctChange(first.AvgSuccessRate, last.AvgSuccessRate)
		exceeds := change < -t.SuccessRateDeclinePct // negative change = decline
		report.MetricTrends = append(report.MetricTrends, MetricDegradation{
			Metric:       "success_rate",
			FirstDay:     first.AvgSuccessRate,
			LastDay:      last.AvgSuccessRate,
			ChangePct:    change,
			ThresholdPct: t.SuccessRateDeclinePct,
			Exceeds:      exceeds,
			Direction:    degradationDirection(change, "higher_is_better"),
		})
	}

	// P99 latency increase.
	if first.AvgP99Latency > 0 {
		change := pctChange(first.AvgP99Latency, last.AvgP99Latency)
		exceeds := change > t.LatencyIncreasePct
		report.MetricTrends = append(report.MetricTrends, MetricDegradation{
			Metric:       "p99_latency_ms",
			FirstDay:     first.AvgP99Latency,
			LastDay:      last.AvgP99Latency,
			ChangePct:    change,
			ThresholdPct: t.LatencyIncreasePct,
			Exceeds:      exceeds,
			Direction:    degradationDirection(change, "lower_is_better"),
		})
	}

	// Error rate increase.
	firstErrRate := errorRate(first)
	lastErrRate := errorRate(last)
	if firstErrRate > 0 {
		change := pctChange(firstErrRate, lastErrRate)
		exceeds := change > t.ErrorRateIncreasePct
		report.MetricTrends = append(report.MetricTrends, MetricDegradation{
			Metric:       "error_rate",
			FirstDay:     firstErrRate,
			LastDay:      lastErrRate,
			ChangePct:    change,
			ThresholdPct: t.ErrorRateIncreasePct,
			Exceeds:      exceeds,
			Direction:    degradationDirection(change, "lower_is_better"),
		})
	} else if lastErrRate > 0 {
		// Went from 0 errors to some errors.
		report.MetricTrends = append(report.MetricTrends, MetricDegradation{
			Metric:       "error_rate",
			FirstDay:     0,
			LastDay:      lastErrRate,
			ChangePct:    100.0,
			ThresholdPct: t.ErrorRateIncreasePct,
			Exceeds:      true,
			Direction:    DegradationWorsening,
		})
	}

	// Aggregate severity.
	for _, trend := range report.MetricTrends {
		if trend.Exceeds {
			report.IsDegrading = true
			report.AffectedMetrics++
		}
	}

	// Severity based on number of affected metrics.
	if report.AffectedMetrics >= 3 {
		report.Severity = SeverityCritical
	} else if report.AffectedMetrics >= 2 {
		report.Severity = SeverityWarning
	} else if report.AffectedMetrics >= 1 {
		report.Severity = SeverityWarning
	}

	// Store report.
	m.mu.Lock()
	m.reports[agentID] = report
	m.mu.Unlock()

	return report
}

// GetLastReport returns the most recent degradation report for an agent.
func (m *LongRunningMonitor) GetLastReport(agentID string) *DegradationReport {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.reports[agentID]
}

// errorRate computes the error rate from a daily summary.
func errorRate(s DailySummary) float64 {
	if s.TotalRequests == 0 {
		return 0
	}
	return float64(s.TotalErrors) / float64(s.TotalRequests)
}

// degradationDirection classifies a change as worsening or improving.
func degradationDirection(changePct float64, directionHint string) DegradationDirection {
	const epsilon = 0.001
	if math.Abs(changePct) < epsilon {
		return DegradationNone
	}
	if directionHint == "higher_is_better" {
		if changePct > 0 {
			return DegradationImproving
		}
		return DegradationWorsening
	}
	// lower_is_better
	if changePct > 0 {
		return DegradationWorsening
	}
	return DegradationImproving
}

// ============================================================================
// AlertEscalation — Sustained Drift Escalation
// ============================================================================

// AlertEscalation tracks sustained drift and breach conditions, escalating
// alert levels when conditions persist beyond configured durations.
type AlertEscalation struct {
	mu                 sync.Mutex
	sustainedThreshold time.Duration // how long drift must persist before notify
	rollbackThreshold  time.Duration // how long before rollback escalation
	driftStart         map[string]time.Time
	breachStart        map[string]time.Time
	currentLevels      map[string]EscalationLevel
}

// NewAlertEscalation creates an escalation tracker.
func NewAlertEscalation(sustainedThreshold, rollbackThreshold time.Duration) *AlertEscalation {
	return &AlertEscalation{
		sustainedThreshold: sustainedThreshold,
		rollbackThreshold:  rollbackThreshold,
		driftStart:         make(map[string]time.Time),
		breachStart:        make(map[string]time.Time),
		currentLevels:      make(map[string]EscalationLevel),
	}
}

// Evaluate checks the current status and drift assessment for an agent and
// returns the appropriate escalation level. It tracks how long drift/breach
// conditions have persisted and escalates accordingly.
func (a *AlertEscalation) Evaluate(agentID string, status SurveillanceStatus, drift *DriftAssessment) EscalationLevel {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now().UTC()
	hasDrift := drift != nil && drift.AnyBreach
	hasCriticalDrift := drift != nil && drift.CriticalCount > 0
	hasBreach := status == StatusBreach
	hasDegradation := status == StatusDegraded

	// Track drift start time.
	if hasDrift {
		if _, ok := a.driftStart[agentID]; !ok {
			a.driftStart[agentID] = now
		}
	} else {
		delete(a.driftStart, agentID)
	}

	// Track breach start time.
	if hasBreach {
		if _, ok := a.breachStart[agentID]; !ok {
			a.breachStart[agentID] = now
		}
	} else {
		delete(a.breachStart, agentID)
	}

	// Determine escalation level based on persistence.
	level := EscalationNone

	if hasBreach {
		level = EscalationRollback
	} else if hasDegradation {
		level = EscalationInvestigate
	} else if hasDrift {
		driftDuration := now.Sub(a.driftStart[agentID])
		if hasCriticalDrift {
			// Critical drift → escalate faster.
			if driftDuration >= a.sustainedThreshold/2 {
				level = EscalationInvestigate
			} else if driftDuration >= a.sustainedThreshold/4 {
				level = EscalationNotify
			}
		} else if driftDuration >= a.rollbackThreshold {
			level = EscalationRollback
		} else if driftDuration >= a.sustainedThreshold {
			level = EscalationInvestigate
		} else if driftDuration >= a.sustainedThreshold/2 {
			level = EscalationNotify
		}
	}

	a.currentLevels[agentID] = level
	return level
}

// GetLevel returns the current escalation level for an agent.
func (a *AlertEscalation) GetLevel(agentID string) EscalationLevel {
	a.mu.Lock()
	defer a.mu.Unlock()
	if level, ok := a.currentLevels[agentID]; ok {
		return level
	}
	return EscalationNone
}

// Reset clears escalation tracking for an agent (e.g., after recovery).
func (a *AlertEscalation) Reset(agentID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.driftStart, agentID)
	delete(a.breachStart, agentID)
	delete(a.currentLevels, agentID)
}

// DriftDuration returns how long sustained drift has been occurring.
func (a *AlertEscalation) DriftDuration(agentID string) time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	if start, ok := a.driftStart[agentID]; ok {
		return time.Since(start)
	}
	return 0
}

// AllLevels returns the current escalation level for all tracked agents.
func (a *AlertEscalation) AllLevels() map[string]EscalationLevel {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make(map[string]EscalationLevel, len(a.currentLevels))
	for k, v := range a.currentLevels {
		result[k] = v
	}
	return result
}
