package marketplace

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Metrics collector (spec §14 — Observability)
// ---------------------------------------------------------------------------

// MetricsCollector computes Prometheus-style metrics from the marketplace
// registry state. Gauges are derived from agent manifests; counters are
// accumulated from operational events (searches, ratings, assignments).
//
// The collector is thread-safe. Callers should call Collect() periodically
// (e.g., every 15 seconds for a Prometheus scrape endpoint) to emit the
// current state as Prometheus text exposition format.
type MetricsCollector struct {
	mu sync.RWMutex

	// Counters (monotonically increasing per spec §14).
	queryCount   map[string]int64 // key = filter type (capability, trust, cost)
	ratingsTotal int64
	assignments  map[string]int64 // key = agent name

	// Snapshot timestamp of last Collect() call.
	lastCollect time.Time
}

// NewMetricsCollector returns an initialized collector ready to track events.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		queryCount:  make(map[string]int64),
		assignments: make(map[string]int64),
	}
}

// RecordQuery increments the query counter for the given filter type.
// Valid filter types per spec §14: "capability", "trust", "cost".
func (m *MetricsCollector) RecordQuery(filter string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queryCount[filter]++
}

// RecordRating increments the total ratings counter.
func (m *MetricsCollector) RecordRating() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ratingsTotal++
}

// RecordAssignment increments the assignment counter for an agent.
func (m *MetricsCollector) RecordAssignment(agentName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.assignments[agentName]++
}

// QueryCount returns the current query count for a filter type.
func (m *MetricsCollector) QueryCount(filter string) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.queryCount[filter]
}

// RatingsTotal returns the current total ratings counter.
func (m *MetricsCollector) RatingsTotal() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ratingsTotal
}

// AssignmentCount returns the assignment count for an agent.
func (m *MetricsCollector) AssignmentCount(agentName string) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.assignments[agentName]
}

// ---------------------------------------------------------------------------
// Gauge computation from registry state
// ---------------------------------------------------------------------------

// AgentsByStatus counts agents grouped by lifecycle status (spec §14:
// helix_marketplace_agents_total{status="active|deprecated|retired"}).
func AgentsByStatus(r *Registry) map[AgentStatus]int {
	counts := map[AgentStatus]int{
		StatusActive:     0,
		StatusDeprecated: 0,
		StatusRetired:    0,
	}
	agents, err := r.List(nil)
	if err != nil {
		return counts
	}
	for _, a := range agents {
		counts[a.Status]++
	}
	return counts
}

// TrustScoreGauges returns {agent_name: trust_score} for all non-retired agents
// (spec §14: helix_marketplace_trust_score{agent="..."}).
func TrustScoreGauges(r *Registry) map[string]int {
	scores := make(map[string]int)
	agents, err := r.List(func(a *Agent) bool {
		return a.Status != StatusRetired
	})
	if err != nil {
		return scores
	}
	for _, a := range agents {
		scores[a.Name] = a.TrustScore
	}
	return scores
}

// ---------------------------------------------------------------------------
// Prometheus text exposition format
// ---------------------------------------------------------------------------

// MetricLine represents a single Prometheus exposition line.
type MetricLine struct {
	Name   string
	Labels map[string]string
	Value  float64
	Help   string
	Type   string // "gauge" or "counter"
}

// Collect produces the full set of Prometheus metrics from the registry state
// and accumulated counters. The output is Prometheus text exposition format
// suitable for a /metrics scrape endpoint.
func (m *MetricsCollector) Collect(r *Registry) string {
	m.mu.Lock()
	m.lastCollect = time.Now()
	m.mu.Unlock()

	var b strings.Builder
	headers := make(map[string]bool)

	// --- Gauges from registry state ---

	// helix_marketplace_agents_total{status="..."}
	statusCounts := AgentsByStatus(r)
	statuses := []AgentStatus{StatusActive, StatusDeprecated, StatusRetired}
	for _, s := range statuses {
		writeMetric(&b, headers, MetricLine{
			Name:   "helix_marketplace_agents_total",
			Help:   "Total agents by lifecycle status",
			Type:   "gauge",
			Labels: map[string]string{"status": string(s)},
			Value:  float64(statusCounts[s]),
		})
	}

	// helix_marketplace_trust_score{agent="..."}
	trustScores := TrustScoreGauges(r)
	// Sort agent names for deterministic output.
	agentNames := make([]string, 0, len(trustScores))
	for name := range trustScores {
		agentNames = append(agentNames, name)
	}
	sort.Strings(agentNames)
	for _, name := range agentNames {
		writeMetric(&b, headers, MetricLine{
			Name:   "helix_marketplace_trust_score",
			Help:   "Current trust score per agent",
			Type:   "gauge",
			Labels: map[string]string{"agent": name},
			Value:  float64(trustScores[name]),
		})
	}

	// --- Counters from accumulated events ---

	m.mu.RLock()
	defer m.mu.RUnlock()

	// helix_marketplace_queries_total{filter="..."}
	filters := make([]string, 0, len(m.queryCount))
	for f := range m.queryCount {
		filters = append(filters, f)
	}
	sort.Strings(filters)
	for _, f := range filters {
		writeMetric(&b, headers, MetricLine{
			Name:   "helix_marketplace_queries_total",
			Help:   "Total marketplace queries by filter type",
			Type:   "counter",
			Labels: map[string]string{"filter": f},
			Value:  float64(m.queryCount[f]),
		})
	}

	// helix_marketplace_ratings_total
	writeMetric(&b, headers, MetricLine{
		Name:  "helix_marketplace_ratings_total",
		Help:  "Total human ratings submitted",
		Type:  "counter",
		Value: float64(m.ratingsTotal),
	})

	// helix_marketplace_assignments_total{agent="..."}
	assignedAgents := make([]string, 0, len(m.assignments))
	for a := range m.assignments {
		assignedAgents = append(assignedAgents, a)
	}
	sort.Strings(assignedAgents)
	for _, a := range assignedAgents {
		writeMetric(&b, headers, MetricLine{
			Name:   "helix_marketplace_assignments_total",
			Help:   "Total task assignments per agent",
			Type:   "counter",
			Labels: map[string]string{"agent": a},
			Value:  float64(m.assignments[a]),
		})
	}

	return b.String()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// writeMetric writes one HELP + TYPE header + one data line.
// HELP and TYPE are written on first occurrence within a single Collect()
// call; writtenHeaders is passed by the caller to track deduplication.
func writeMetric(b *strings.Builder, writtenHeaders map[string]bool, ml MetricLine) {
	// Write HELP and TYPE on first occurrence of this metric name.
	if !writtenHeaders[ml.Name] {
		b.WriteString(fmt.Sprintf("# HELP %s %s\n", ml.Name, ml.Help))
		b.WriteString(fmt.Sprintf("# TYPE %s %s\n", ml.Name, ml.Type))
		writtenHeaders[ml.Name] = true
	}

	// Build label string.
	if len(ml.Labels) == 0 {
		b.WriteString(fmt.Sprintf("%s %g\n", ml.Name, ml.Value))
		return
	}

	labels := make([]string, 0, len(ml.Labels))
	for k := range ml.Labels {
		labels = append(labels, k)
	}
	sort.Strings(labels)

	parts := make([]string, len(labels))
	for i, k := range labels {
		parts[i] = fmt.Sprintf(`%s=%q`, k, ml.Labels[k])
	}
	b.WriteString(fmt.Sprintf("%s{%s} %g\n", ml.Name, strings.Join(parts, ","), ml.Value))
}

// Reset clears all accumulated counters. Gauges are always derived from the
// registry state at Collect() time, so they are not affected by Reset().
func (m *MetricsCollector) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queryCount = make(map[string]int64)
	m.ratingsTotal = 0
	m.assignments = make(map[string]int64)
}

// LastCollect returns the timestamp of the most recent Collect() call.
func (m *MetricsCollector) LastCollect() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastCollect
}
