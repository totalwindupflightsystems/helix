// Package health — platform_metrics.go
//
// Platform-level Prometheus metrics recorder implementing all 7 platform
// metrics from specs/SPECIFICATION.md §8.4 (Prometheus Metrics — Platform metrics):
//
//	helix_pr_cycle_time_seconds{repo, quantile="0.5|0.95|0.99"}
//	helix_gate_pass_rate{gate="tier1|tier2|chimera|conscientiousness|promptfoo"}
//	helix_active_agents
//	helix_queued_tasks
//	helix_forgejo_api_latency_seconds{endpoint, quantile}
//	helix_cost_per_pr{repo}
//	helix_merge_rate{repo, period="hour|day|week"}
//
// This recorder is the producer side: it records raw events (PR cycle times,
// gate pass/fail, agent activity, task queue depth, API latencies, per-PR costs,
// merge counts) and exposes them in Prometheus text exposition format. The
// AlertEngine (alerts.go) consumes the same data via MetricsSnapshot.
//
// Thread-safe via sync.RWMutex.
package health

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
)

// ============================================================================
// GateName — identifies a quality gate in the pipeline
// ============================================================================

// GateName identifies a quality gate.
type GateName string

const (
	GateTier1             GateName = "tier1"
	GateTier2             GateName = "tier2"
	GateChimera           GateName = "chimera"
	GateConscientiousness GateName = "conscientiousness"
	GatePromptFoo         GateName = "promptfoo"
)

// AllGateNames returns all spec-defined gate names in order.
func AllGateNames() []GateName {
	return []GateName{GateTier1, GateTier2, GateChimera, GateConscientiousness, GatePromptFoo}
}

// ============================================================================
// MergePeriod — time aggregation for merge rate
// ============================================================================

// MergePeriod is the time window for merge rate reporting.
type MergePeriod string

const (
	MergeHour MergePeriod = "hour"
	MergeDay  MergePeriod = "day"
	MergeWeek MergePeriod = "week"
)

// ============================================================================
// Quantile helpers
// ============================================================================

// quantile computes the p-th percentile (0-100) from a sorted slice.
// Returns 0 for empty slices.
func quantile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	idx := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// ============================================================================
// PlatformMetricsRecorder
// ============================================================================

// PlatformMetricsRecorder tracks all 7 platform-level metrics from
// spec §8.4 and exposes them in Prometheus text exposition format.
//
// It serves as the data producer; the AlertEngine (alerts.go) consumes
// the same data through MetricsSnapshot via ToSnapshot().
type PlatformMetricsRecorder struct {
	mu sync.RWMutex

	// prCycleTimes maps repo → list of PR cycle time samples (seconds).
	prCycleTimes map[string][]float64

	// gateResults maps gate → {pass count, total count}.
	gateResults map[GateName]*gateCounters

	// activeAgents is the current count of active agent containers.
	activeAgents int64

	// queuedTasks is the current count of queued tasks.
	queuedTasks int64

	// apiLatencies maps endpoint → list of latency samples (seconds).
	apiLatencies map[string][]float64

	// costPerPR maps repo → list of per-PR costs (USD).
	costPerPR map[string][]float64

	// mergeCounts maps repo → period → count of merges in current window.
	mergeCounts map[string]map[MergePeriod]int64
}

type gateCounters struct {
	pass  int64
	total int64
}

// NewPlatformMetricsRecorder creates a new recorder with all counters
// initialized.
func NewPlatformMetricsRecorder() *PlatformMetricsRecorder {
	return &PlatformMetricsRecorder{
		prCycleTimes: make(map[string][]float64),
		gateResults:  make(map[GateName]*gateCounters),
		apiLatencies: make(map[string][]float64),
		costPerPR:    make(map[string][]float64),
		mergeCounts:  make(map[string]map[MergePeriod]int64),
	}
}

// ============================================================================
// Recording Methods
// ============================================================================

// RecordPRCycleTime records a PR cycle time sample for a repo (in seconds).
// The cycle time is measured from PR open to merge.
func (c *PlatformMetricsRecorder) RecordPRCycleTime(repo string, seconds float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if seconds < 0 {
		seconds = 0
	}
	c.prCycleTimes[repo] = append(c.prCycleTimes[repo], seconds)
}

// RecordGateResult records a pass or fail for a specific gate.
func (c *PlatformMetricsRecorder) RecordGateResult(gate GateName, passed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.gateResults[gate] == nil {
		c.gateResults[gate] = &gateCounters{}
	}
	c.gateResults[gate].total++
	if passed {
		c.gateResults[gate].pass++
	}
}

// SetActiveAgents sets the current count of active agent containers.
func (c *PlatformMetricsRecorder) SetActiveAgents(count int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if count < 0 {
		count = 0
	}
	c.activeAgents = count
}

// SetQueuedTasks sets the current count of queued tasks awaiting agent pickup.
func (c *PlatformMetricsRecorder) SetQueuedTasks(count int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if count < 0 {
		count = 0
	}
	c.queuedTasks = count
}

// RecordAPILatency records an API latency sample for a specific endpoint
// (in seconds).
func (c *PlatformMetricsRecorder) RecordAPILatency(endpoint string, seconds float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if seconds < 0 {
		seconds = 0
	}
	c.apiLatencies[endpoint] = append(c.apiLatencies[endpoint], seconds)
}

// RecordPRCost records the total cost for a single PR on a specific repo.
func (c *PlatformMetricsRecorder) RecordPRCost(repo string, costUSD float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if costUSD < 0 {
		costUSD = 0
	}
	c.costPerPR[repo] = append(c.costPerPR[repo], costUSD)
}

// RecordMerge records a merge event for a repo within a time period window.
func (c *PlatformMetricsRecorder) RecordMerge(repo string, period MergePeriod) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.mergeCounts[repo] == nil {
		c.mergeCounts[repo] = make(map[MergePeriod]int64)
	}
	c.mergeCounts[repo][period]++
}

// ============================================================================
// Query Methods
// ============================================================================

// GetActiveAgents returns the current active agent count.
func (c *PlatformMetricsRecorder) GetActiveAgents() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.activeAgents
}

// GetQueuedTasks returns the current queued task count.
func (c *PlatformMetricsRecorder) GetQueuedTasks() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.queuedTasks
}

// GetGatePassRate returns the pass rate (0.0–1.0) for a specific gate.
// Returns 0.0 if no results have been recorded for the gate.
func (c *PlatformMetricsRecorder) GetGatePassRate(gate GateName) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	gc := c.gateResults[gate]
	if gc == nil || gc.total == 0 {
		return 0
	}
	return float64(gc.pass) / float64(gc.total)
}

// GetMergeRate returns the merge count for a repo and period.
func (c *PlatformMetricsRecorder) GetMergeRate(repo string, period MergePeriod) int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.mergeCounts[repo] == nil {
		return 0
	}
	return c.mergeCounts[repo][period]
}

// GetAvgCostPerPR returns the average cost per PR for a repo.
func (c *PlatformMetricsRecorder) GetAvgCostPerPR(repo string) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	costs := c.costPerPR[repo]
	if len(costs) == 0 {
		return 0
	}
	var sum float64
	for _, c := range costs {
		sum += c
	}
	return sum / float64(len(costs))
}

// ============================================================================
// Collect — Prometheus Text Exposition Format
// ============================================================================

// Collect returns all 7 platform metrics in Prometheus text exposition format.
// Output is deterministic (sorted by metric name, then by labels).
func (c *PlatformMetricsRecorder) Collect() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var sb strings.Builder

	// 1. helix_pr_cycle_time_seconds
	c.collectPRCycleTime(&sb)

	// 2. helix_gate_pass_rate
	c.collectGatePassRate(&sb)

	// 3. helix_active_agents
	sb.WriteString("# HELP helix_active_agents Current number of active agent containers.\n")
	sb.WriteString("# TYPE helix_active_agents gauge\n")
	sb.WriteString(fmt.Sprintf("helix_active_agents %d\n", c.activeAgents))

	// 4. helix_queued_tasks
	sb.WriteString("# HELP helix_queued_tasks Current number of tasks queued for agent pickup.\n")
	sb.WriteString("# TYPE helix_queued_tasks gauge\n")
	sb.WriteString(fmt.Sprintf("helix_queued_tasks %d\n", c.queuedTasks))

	// 5. helix_forgejo_api_latency_seconds
	c.collectAPILatency(&sb)

	// 6. helix_cost_per_pr
	c.collectCostPerPR(&sb)

	// 7. helix_merge_rate
	c.collectMergeRate(&sb)

	return sb.String()
}

func (c *PlatformMetricsRecorder) collectPRCycleTime(sb *strings.Builder) {
	sb.WriteString("# HELP helix_pr_cycle_time_seconds PR cycle time (open→merge) in seconds, by repo and quantile.\n")
	sb.WriteString("# TYPE helix_pr_cycle_time_seconds summary\n")

	repos := sortedKeys(c.prCycleTimes)
	for _, repo := range repos {
		samples := make([]float64, len(c.prCycleTimes[repo]))
		copy(samples, c.prCycleTimes[repo])
		sort.Float64s(samples)

		p50 := quantile(samples, 50)
		p95 := quantile(samples, 95)
		p99 := quantile(samples, 99)

		sb.WriteString(fmt.Sprintf("helix_pr_cycle_time_seconds{repo=%q,quantile=\"0.5\"} %s\n", repo, formatPlatformFloat(p50)))
		sb.WriteString(fmt.Sprintf("helix_pr_cycle_time_seconds{repo=%q,quantile=\"0.95\"} %s\n", repo, formatPlatformFloat(p95)))
		sb.WriteString(fmt.Sprintf("helix_pr_cycle_time_seconds{repo=%q,quantile=\"0.99\"} %s\n", repo, formatPlatformFloat(p99)))
	}
}

func (c *PlatformMetricsRecorder) collectGatePassRate(sb *strings.Builder) {
	sb.WriteString("# HELP helix_gate_pass_rate Pass rate (0.0-1.0) for each quality gate.\n")
	sb.WriteString("# TYPE helix_gate_pass_rate gauge\n")

	for _, gate := range AllGateNames() {
		gc := c.gateResults[gate]
		if gc == nil || gc.total == 0 {
			continue
		}
		rate := float64(gc.pass) / float64(gc.total)
		sb.WriteString(fmt.Sprintf("helix_gate_pass_rate{gate=%q} %s\n", string(gate), formatPlatformFloat(rate)))
	}
}

func (c *PlatformMetricsRecorder) collectAPILatency(sb *strings.Builder) {
	sb.WriteString("# HELP helix_forgejo_api_latency_seconds API latency in seconds, by endpoint and quantile.\n")
	sb.WriteString("# TYPE helix_forgejo_api_latency_seconds summary\n")

	endpoints := sortedKeys(c.apiLatencies)
	for _, ep := range endpoints {
		samples := make([]float64, len(c.apiLatencies[ep]))
		copy(samples, c.apiLatencies[ep])
		sort.Float64s(samples)

		p50 := quantile(samples, 50)
		p95 := quantile(samples, 95)
		p99 := quantile(samples, 99)

		sb.WriteString(fmt.Sprintf("helix_forgejo_api_latency_seconds{endpoint=%q,quantile=\"0.5\"} %s\n", ep, formatPlatformFloat(p50)))
		sb.WriteString(fmt.Sprintf("helix_forgejo_api_latency_seconds{endpoint=%q,quantile=\"0.95\"} %s\n", ep, formatPlatformFloat(p95)))
		sb.WriteString(fmt.Sprintf("helix_forgejo_api_latency_seconds{endpoint=%q,quantile=\"0.99\"} %s\n", ep, formatPlatformFloat(p99)))
	}
}

func (c *PlatformMetricsRecorder) collectCostPerPR(sb *strings.Builder) {
	sb.WriteString("# HELP helix_cost_per_pr Average LLM cost per PR in USD, by repo.\n")
	sb.WriteString("# TYPE helix_cost_per_pr gauge\n")

	repos := sortedKeys(c.costPerPR)
	for _, repo := range repos {
		costs := c.costPerPR[repo]
		if len(costs) == 0 {
			continue
		}
		var sum float64
		for _, c := range costs {
			sum += c
		}
		avg := sum / float64(len(costs))
		sb.WriteString(fmt.Sprintf("helix_cost_per_pr{repo=%q} %s\n", repo, formatPlatformFloat(avg)))
	}
}

func (c *PlatformMetricsRecorder) collectMergeRate(sb *strings.Builder) {
	sb.WriteString("# HELP helix_merge_rate Number of merges per repo, by time period.\n")
	sb.WriteString("# TYPE helix_merge_rate counter\n")

	repos := sortedKeysMerge(c.mergeCounts)
	periodOrder := []MergePeriod{MergeHour, MergeDay, MergeWeek}

	for _, repo := range repos {
		for _, period := range periodOrder {
			count := c.mergeCounts[repo][period]
			if count == 0 {
				continue
			}
			sb.WriteString(fmt.Sprintf("helix_merge_rate{repo=%q,period=%q} %d\n",
				repo, string(period), count))
		}
	}
}

// ============================================================================
// MetricsSource Interface
// ============================================================================

// MetricsName returns the subsystem identifier for MetricsSource.
func (c *PlatformMetricsRecorder) MetricsName() string {
	return "platform"
}

// CollectMetrics returns the collected metrics as MetricLines for the
// PlatformMetricsCollector aggregator.
func (c *PlatformMetricsRecorder) CollectMetrics() []MetricLine {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var lines []MetricLine

	// helix_pr_cycle_time_seconds (quantile per repo)
	for repo, samples := range c.prCycleTimes {
		if len(samples) == 0 {
			continue
		}
		s := make([]float64, len(samples))
		copy(s, samples)
		sort.Float64s(s)
		for _, q := range []struct {
			label string
			pct   float64
		}{
			{"0.5", 50}, {"0.95", 95}, {"0.99", 99},
		} {
			lines = append(lines, MetricLine{
				Name:   "helix_pr_cycle_time_seconds",
				Help:   "PR cycle time (open→merge) in seconds, by repo and quantile.",
				Type:   "summary",
				Labels: map[string]string{"repo": repo, "quantile": q.label},
				Value:  quantile(s, q.pct),
			})
		}
	}

	// helix_gate_pass_rate
	for _, gate := range AllGateNames() {
		gc := c.gateResults[gate]
		if gc == nil || gc.total == 0 {
			continue
		}
		rate := float64(gc.pass) / float64(gc.total)
		lines = append(lines, MetricLine{
			Name:   "helix_gate_pass_rate",
			Help:   "Pass rate (0.0-1.0) for each quality gate.",
			Type:   "gauge",
			Labels: map[string]string{"gate": string(gate)},
			Value:  rate,
		})
	}

	// helix_active_agents
	lines = append(lines, MetricLine{
		Name:  "helix_active_agents",
		Help:  "Current number of active agent containers.",
		Type:  "gauge",
		Value: float64(c.activeAgents),
	})

	// helix_queued_tasks
	lines = append(lines, MetricLine{
		Name:  "helix_queued_tasks",
		Help:  "Current number of tasks queued for agent pickup.",
		Type:  "gauge",
		Value: float64(c.queuedTasks),
	})

	// helix_forgejo_api_latency_seconds
	for ep, samples := range c.apiLatencies {
		if len(samples) == 0 {
			continue
		}
		s := make([]float64, len(samples))
		copy(s, samples)
		sort.Float64s(s)
		for _, q := range []struct {
			label string
			pct   float64
		}{
			{"0.5", 50}, {"0.95", 95}, {"0.99", 99},
		} {
			lines = append(lines, MetricLine{
				Name:   "helix_forgejo_api_latency_seconds",
				Help:   "API latency in seconds, by endpoint and quantile.",
				Type:   "summary",
				Labels: map[string]string{"endpoint": ep, "quantile": q.label},
				Value:  quantile(s, q.pct),
			})
		}
	}

	// helix_cost_per_pr
	for repo, costs := range c.costPerPR {
		if len(costs) == 0 {
			continue
		}
		var sum float64
		for _, c := range costs {
			sum += c
		}
		avg := sum / float64(len(costs))
		lines = append(lines, MetricLine{
			Name:   "helix_cost_per_pr",
			Help:   "Average LLM cost per PR in USD, by repo.",
			Type:   "gauge",
			Labels: map[string]string{"repo": repo},
			Value:  avg,
		})
	}

	// helix_merge_rate
	for repo, periods := range c.mergeCounts {
		for _, period := range []MergePeriod{MergeHour, MergeDay, MergeWeek} {
			count := periods[period]
			if count == 0 {
				continue
			}
			lines = append(lines, MetricLine{
				Name:   "helix_merge_rate",
				Help:   "Number of merges per repo, by time period.",
				Type:   "counter",
				Labels: map[string]string{"repo": repo, "period": string(period)},
				Value:  float64(count),
			})
		}
	}

	return lines
}

// ============================================================================
// ToSnapshot — convert to MetricsSnapshot for the AlertEngine
// ============================================================================

// ToSnapshot converts the recorder's current state to a MetricsSnapshot
// for evaluation by the AlertEngine.
func (c *PlatformMetricsRecorder) ToSnapshot() MetricsSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	snap := NewMetricsSnapshot()

	// Gate pass rates
	for gate, gc := range c.gateResults {
		if gc.total > 0 {
			snap.GatePassRates[string(gate)] = float64(gc.pass) / float64(gc.total)
		}
	}

	// PR cycle times (use P50 per repo)
	for repo, samples := range c.prCycleTimes {
		if len(samples) == 0 {
			continue
		}
		s := make([]float64, len(samples))
		copy(s, samples)
		sort.Float64s(s)
		snap.PRCycleTimes[repo] = quantile(s, 50)
	}

	// Cost per PR (average per repo)
	for repo, costs := range c.costPerPR {
		if len(costs) == 0 {
			continue
		}
		var sum float64
		for _, c := range costs {
			sum += c
		}
		avg := sum / float64(len(costs))
		snap.CostPerPR[repo] = avg

		// Weekly avg: use same average (full history). In production, this would
		// be a 7-day rolling window from Prometheus.
		snap.WeeklyAvgCostPerPR[repo] = avg
	}

	return snap
}

// ============================================================================
// Reset — clear all recorded metrics (for testing / windowing)
// ============================================================================

// Reset clears all recorded metrics and counters.
func (c *PlatformMetricsRecorder) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.prCycleTimes = make(map[string][]float64)
	c.gateResults = make(map[GateName]*gateCounters)
	c.activeAgents = 0
	c.queuedTasks = 0
	c.apiLatencies = make(map[string][]float64)
	c.costPerPR = make(map[string][]float64)
	c.mergeCounts = make(map[string]map[MergePeriod]int64)
}

// ============================================================================
// PlatformMetricsSummary
// ============================================================================

// PlatformMetricsSummary provides aggregate statistics for quick reporting.
type PlatformMetricsSummary struct {
	TotalPRs         int
	TotalGateResults int
	ActiveAgents     int64
	QueuedTasks      int64
	EndpointsTracked int
	ReposTracked     int
}

// GetSummary returns aggregate statistics across all tracked metrics.
func (c *PlatformMetricsRecorder) GetSummary() PlatformMetricsSummary {
	c.mu.RLock()
	defer c.mu.RUnlock()

	summary := PlatformMetricsSummary{
		ActiveAgents: c.activeAgents,
		QueuedTasks:  c.queuedTasks,
	}

	// Count PRs
	for _, samples := range c.prCycleTimes {
		summary.TotalPRs += len(samples)
	}

	// Count gate results
	for _, gc := range c.gateResults {
		summary.TotalGateResults += int(gc.total)
	}

	// Count endpoints
	summary.EndpointsTracked = len(c.apiLatencies)

	// Count unique repos across all metrics
	repoSet := make(map[string]bool)
	for repo := range c.prCycleTimes {
		repoSet[repo] = true
	}
	for repo := range c.costPerPR {
		repoSet[repo] = true
	}
	for repo := range c.mergeCounts {
		repoSet[repo] = true
	}
	summary.ReposTracked = len(repoSet)

	return summary
}

// ============================================================================
// Helpers
// ============================================================================

func formatPlatformFloat(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%.4f", f)
}

// sortedKeys returns sorted string keys from a map of slices.
func sortedKeys(m map[string][]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedKeysMerge returns sorted string keys from the merge counts map.
func sortedKeysMerge(m map[string]map[MergePeriod]int64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
