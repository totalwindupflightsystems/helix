package health

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Platform Metrics Aggregator — specs/SPECIFICATION.md §8 (Observability)
//
// Aggregates Prometheus metrics from all Helix subsystems into a single
// /metrics endpoint. Each subsystem provides a MetricsSource that emits its
// own Prometheus text exposition format; the aggregator concatenates them
// with consistent HELP/TYPE headers and deterministic ordering.
// ---------------------------------------------------------------------------

// MetricLine represents a single Prometheus metric line.
type MetricLine struct {
	Name   string
	Help   string
	Type   string // "gauge", "counter", "histogram"
	Labels map[string]string
	Value  float64
}

// MetricsSource is implemented by any subsystem that emits Prometheus metrics.
type MetricsSource interface {
	// MetricsName returns the subsystem identifier (e.g., "trust", "review").
	MetricsName() string
	// CollectMetrics returns all metric lines for this subsystem.
	CollectMetrics() []MetricLine
}

// PlatformMetricsCollector aggregates metrics from multiple subsystems.
type PlatformMetricsCollector struct {
	mu      sync.RWMutex
	sources []MetricsSource
	// Counters for subsystems that don't implement MetricsSource but provide
	// summary data (incremented by the collector's RecordXxx methods).
	counters map[string]float64
	// Last collection timestamp.
	lastCollect time.Time
}

// NewPlatformMetricsCollector creates a new aggregator with no sources.
func NewPlatformMetricsCollector() *PlatformMetricsCollector {
	return &PlatformMetricsCollector{
		counters: make(map[string]float64),
	}
}

// RegisterSource adds a subsystem metrics source.
func (p *PlatformMetricsCollector) RegisterSource(source MetricsSource) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sources = append(p.sources, source)
}

// RecordCounter increments a named counter metric.
func (p *PlatformMetricsCollector) RecordCounter(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.counters[name]++
}

// RecordCounterValue adds a specific value to a named counter.
func (p *PlatformMetricsCollector) RecordCounterValue(name string, value float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.counters[name] += value
}

// Collect produces the full Prometheus text exposition format combining
// all registered sources and internal counters. Output is deterministic:
// metrics are sorted by name, then by labels.
func (p *PlatformMetricsCollector) Collect() string {
	p.mu.Lock()
	p.lastCollect = time.Now()
	sources := make([]MetricsSource, len(p.sources))
	copy(sources, p.sources)
	counters := make(map[string]float64, len(p.counters))
	for k, v := range p.counters {
		counters[k] = v
	}
	p.mu.Unlock()

	// Collect all metric lines from sources.
	var allLines []MetricLine
	emittedHeaders := make(map[string]bool)

	for _, source := range sources {
		lines := source.CollectMetrics()
		allLines = append(allLines, lines...)
	}

	// Add internal counters.
	for name, value := range counters {
		allLines = append(allLines, MetricLine{
			Name:  name,
			Type:  "counter",
			Value: value,
		})
	}

	// Sort by metric name, then by label values for determinism.
	sort.Slice(allLines, func(i, j int) bool {
		if allLines[i].Name != allLines[j].Name {
			return allLines[i].Name < allLines[j].Name
		}
		return labelKey(allLines[i].Labels) < labelKey(allLines[j].Labels)
	})

	var sb strings.Builder
	for _, line := range allLines {
		// Write HELP/TYPE headers (once per metric name).
		headerKey := line.Name + "/" + line.Type
		if !emittedHeaders[headerKey] && line.Help != "" {
			sb.WriteString(fmt.Sprintf("# HELP %s %s\n", line.Name, line.Help))
			sb.WriteString(fmt.Sprintf("# TYPE %s %s\n", line.Name, line.Type))
			emittedHeaders[headerKey] = true
		}
		sb.WriteString(formatMetricLine(line))
		sb.WriteString("\n")
	}

	return sb.String()
}

// SourceCount returns the number of registered metrics sources.
func (p *PlatformMetricsCollector) SourceCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.sources)
}

// LastCollectTime returns the timestamp of the last Collect() call.
func (p *PlatformMetricsCollector) LastCollectTime() time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastCollect
}

// Summary returns a human-readable summary of all metrics.
func (p *PlatformMetricsCollector) Summary() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var sb strings.Builder
	sb.WriteString("Platform Metrics Summary\n")
	sb.WriteString(fmt.Sprintf("  Sources registered: %d\n", len(p.sources)))
	sb.WriteString(fmt.Sprintf("  Internal counters: %d\n", len(p.counters)))
	for name, value := range p.counters {
		sb.WriteString(fmt.Sprintf("    %s: %.0f\n", name, value))
	}
	return sb.String()
}

// formatMetricLine renders a single metric line in Prometheus text format.
func formatMetricLine(line MetricLine) string {
	if len(line.Labels) == 0 {
		return fmt.Sprintf("%s %s", line.Name, formatValue(line.Value))
	}
	// Sort label keys for deterministic output.
	keys := make([]string, 0, len(line.Labels))
	for k := range line.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", k, line.Labels[k]))
	}
	return fmt.Sprintf("%s{%s} %s", line.Name, strings.Join(parts, ","), formatValue(line.Value))
}

// formatValue renders a float64 for Prometheus text format.
func formatValue(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%g", v)
}

// labelKey produces a sortable string from a label map.
func labelKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, k+"="+labels[k])
	}
	return strings.Join(parts, ",")
}

// ----- Stub MetricsSource implementations for testing -----

// StubMetricsSource is a simple MetricsSource for testing.
type StubMetricsSource struct {
	name  string
	lines []MetricLine
}

// NewStubMetricsSource creates a test metrics source.
func NewStubMetricsSource(name string, lines []MetricLine) *StubMetricsSource {
	return &StubMetricsSource{name: name, lines: lines}
}

func (s *StubMetricsSource) MetricsName() string          { return s.name }
func (s *StubMetricsSource) CollectMetrics() []MetricLine { return s.lines }
