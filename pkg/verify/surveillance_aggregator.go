package verify

import (
	"fmt"
	"sync"
	"time"
)

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
