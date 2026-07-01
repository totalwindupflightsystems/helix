package prompt

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// Spec §19 — Observability: Prometheus metrics for the prompt registry
//
// Metrics:
//   helix_prompts_total{status}                    — gauge: prompts by lifecycle status
//   helix_prompt_attestations_total                — counter: total attestations
//   helix_prompt_attestation_failures_total{reason} — counter: failures by reason
//   helix_prompt_versions_total{component}         — gauge: version count per component
//   helix_prompt_overrides_total                   — counter: --no-verify overrides (spike trigger)
// ---------------------------------------------------------------------------

// MetricsCollector tracks prompt registry metrics for Prometheus exposition.
// Thread-safe with sync.RWMutex.
type MetricsCollector struct {
	mu                    sync.RWMutex
	attestationTotal      int
	attestationFailures   map[string]int // reason → count
	overridesTotal        int
	statusCounts          map[LifecycleStatus]int
	componentVersionCount map[string]int // component → version count
}

// NewMetricsCollector creates an initialized MetricsCollector.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		attestationFailures:   make(map[string]int),
		statusCounts:          make(map[LifecycleStatus]int),
		componentVersionCount: make(map[string]int),
	}
}

// RecordAttestation increments the attestation counter.
func (m *MetricsCollector) RecordAttestation() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attestationTotal++
}

// RecordAttestationFailure increments the failure counter for the given reason.
func (m *MetricsCollector) RecordAttestationFailure(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attestationFailures[reason]++
}

// RecordOverride increments the override counter (--no-verify bypass).
func (m *MetricsCollector) RecordOverride() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.overridesTotal++
}

// SetStatusCounts sets the prompt counts by lifecycle status.
func (m *MetricsCollector) SetStatusCounts(counts map[LifecycleStatus]int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusCounts = make(map[LifecycleStatus]int, len(counts))
	for k, v := range counts {
		m.statusCounts[k] = v
	}
}

// SetComponentVersions sets the version count per component.
func (m *MetricsCollector) SetComponentVersions(counts map[string]int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.componentVersionCount = make(map[string]int, len(counts))
	for k, v := range counts {
		m.componentVersionCount[k] = v
	}
}

// UpdateFromIndex populates status and component version counts from a registry index.
func (m *MetricsCollector) UpdateFromIndex(idx Index) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.statusCounts = make(map[LifecycleStatus]int)
	m.componentVersionCount = make(map[string]int)

	for component, versions := range idx {
		m.componentVersionCount[component] = len(versions)
		for _, entry := range versions {
			if entry != nil {
				m.statusCounts[entry.Status]++
			}
		}
	}
}

// AttestationTotal returns the total attestation count.
func (m *MetricsCollector) AttestationTotal() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.attestationTotal
}

// OverridesTotal returns the total override count.
func (m *MetricsCollector) OverridesTotal() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.overridesTotal
}

// AttestationFailures returns a copy of the failure counts by reason.
func (m *MetricsCollector) AttestationFailures() map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]int, len(m.attestationFailures))
	for k, v := range m.attestationFailures {
		result[k] = v
	}
	return result
}

// StatusCounts returns a copy of the prompt counts by lifecycle status.
func (m *MetricsCollector) StatusCounts() map[LifecycleStatus]int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[LifecycleStatus]int, len(m.statusCounts))
	for k, v := range m.statusCounts {
		result[k] = v
	}
	return result
}

// ComponentVersionCounts returns a copy of the version counts per component.
func (m *MetricsCollector) ComponentVersionCounts() map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]int, len(m.componentVersionCount))
	for k, v := range m.componentVersionCount {
		result[k] = v
	}
	return result
}

// Collect emits all metrics in Prometheus text exposition format.
// Output is deterministic (sorted by metric name then label value).
func (m *MetricsCollector) Collect() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var sb strings.Builder

	// helix_prompts_total{status}
	sb.WriteString("# HELP helix_prompts_total Number of prompts by lifecycle status\n")
	sb.WriteString("# TYPE helix_prompts_total gauge\n")
	statuses := make([]string, 0, len(m.statusCounts))
	for s := range m.statusCounts {
		statuses = append(statuses, string(s))
	}
	sort.Strings(statuses)
	for _, s := range statuses {
		sb.WriteString(fmt.Sprintf("helix_prompts_total{status=%q} %d\n", s, m.statusCounts[LifecycleStatus(s)]))
	}

	// helix_prompt_attestations_total
	sb.WriteString("# HELP helix_prompt_attestations_total Total number of prompt attestations\n")
	sb.WriteString("# TYPE helix_prompt_attestations_total counter\n")
	sb.WriteString(fmt.Sprintf("helix_prompt_attestations_total %d\n", m.attestationTotal))

	// helix_prompt_attestation_failures_total{reason}
	sb.WriteString("# HELP helix_prompt_attestation_failures_total Attestation failures by reason\n")
	sb.WriteString("# TYPE helix_prompt_attestation_failures_total counter\n")
	reasons := make([]string, 0, len(m.attestationFailures))
	for r := range m.attestationFailures {
		reasons = append(reasons, r)
	}
	sort.Strings(reasons)
	for _, r := range reasons {
		sb.WriteString(fmt.Sprintf("helix_prompt_attestation_failures_total{reason=%q} %d\n",
			r, m.attestationFailures[r]))
	}

	// helix_prompt_versions_total{component}
	sb.WriteString("# HELP helix_prompt_versions_total Number of versions per component\n")
	sb.WriteString("# TYPE helix_prompt_versions_total gauge\n")
	components := make([]string, 0, len(m.componentVersionCount))
	for c := range m.componentVersionCount {
		components = append(components, c)
	}
	sort.Strings(components)
	for _, c := range components {
		sb.WriteString(fmt.Sprintf("helix_prompt_versions_total{component=%q} %d\n",
			c, m.componentVersionCount[c]))
	}

	// helix_prompt_overrides_total
	sb.WriteString("# HELP helix_prompt_overrides_total Number of --no-verify overrides (spike triggers review)\n")
	sb.WriteString("# TYPE helix_prompt_overrides_total counter\n")
	sb.WriteString(fmt.Sprintf("helix_prompt_overrides_total %d\n", m.overridesTotal))

	return sb.String()
}

// Reset zeros all counters and gauges.
func (m *MetricsCollector) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attestationTotal = 0
	m.attestationFailures = make(map[string]int)
	m.overridesTotal = 0
	m.statusCounts = make(map[LifecycleStatus]int)
	m.componentVersionCount = make(map[string]int)
}
