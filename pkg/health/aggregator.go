package health

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

// SubsystemHealth is the interface each Helix subsystem implements
// so the aggregator can query it without coupling to its internals.
type SubsystemHealth interface {
	// HealthCheck returns the subsystem's current health status.
	HealthCheck(ctx context.Context) SubsystemStatus
}

// SubsystemStatus captures the health of a single Helix subsystem.
type SubsystemStatus struct {
	Name      string            `json:"name"`
	State     HealthState       `json:"state"`
	Message   string            `json:"message,omitempty"`
	Metrics   map[string]string `json:"metrics,omitempty"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// HealthState is the coarse-grained health state.
type HealthState string

const (
	StateHealthy  HealthState = "healthy"
	StateDegraded HealthState = "degraded"
	StateDown     HealthState = "down"
	StateUnknown  HealthState = "unknown"
)

// Critical determines whether this subsystem failing should degrade the
// entire platform. Critical subsystems are core to the Helix loop
// (forgejo, chimera, trust, review). Non-critical ones (marketplace,
// estimate) affect specific features but not the core pipeline.
func (s SubsystemStatus) Critical() bool {
	return criticalSubsystems[s.Name]
}

// criticalSubsystems maps subsystem names that are critical to the platform.
var criticalSubsystems = map[string]bool{
	"forgejo":    true,
	"chimera":    true,
	"trust":      true,
	"review":     true,
	"negotiate":  true,
	"dispatcher": true,
	"sandbox":    true,
}

// DashboardReport is the unified health view produced by the aggregator.
type DashboardReport struct {
	Overall     HealthState       `json:"overall"`
	Subsystems  []SubsystemStatus `json:"subsystems"`
	Critical    []string          `json:"critical_failures,omitempty"`
	Degraded    []string          `json:"degraded_subsystems,omitempty"`
	GeneratedAt time.Time         `json:"generated_at"`
	CacheTTL    time.Duration     `json:"cache_ttl_seconds"`
	Latency     time.Duration     `json:"latency_ms"`
}

// AllHealthy returns true if every subsystem reports healthy.
func (d *DashboardReport) AllHealthy() bool {
	for _, s := range d.Subsystems {
		if s.State != StateHealthy {
			return false
		}
	}
	return true
}

// HasCriticalFailure returns true if any critical subsystem is down.
func (d *DashboardReport) HasCriticalFailure() bool {
	return len(d.Critical) > 0
}

// MarshalJSON returns a JSON-serialisable dashboard with latency in
// milliseconds and cache_ttl in seconds for human-friendly consumption.
func (d *DashboardReport) MarshalJSON() ([]byte, error) {
	type alias DashboardReport
	return json.Marshal(&struct {
		*alias
		LatencyMs       int64 `json:"latency_ms"`
		CacheTTLSeconds int64 `json:"cache_ttl_seconds"`
	}{
		alias:           (*alias)(d),
		LatencyMs:       d.Latency.Milliseconds(),
		CacheTTLSeconds: int64(d.CacheTTL.Seconds()),
	})
}

// PlatformHealthAggregator collects health from all Helix subsystems,
// caches results with a TTL, and produces a unified DashboardReport.
type PlatformHealthAggregator struct {
	mu         sync.RWMutex
	subsystems map[string]SubsystemHealth
	cache      *DashboardReport
	cacheTTL   time.Duration
}

// NewPlatformHealthAggregator creates an aggregator with the given cache TTL.
// A typical TTL is 15–30 seconds for a CLI `helix status` command.
func NewPlatformHealthAggregator(cacheTTL time.Duration) *PlatformHealthAggregator {
	if cacheTTL <= 0 {
		cacheTTL = 15 * time.Second
	}
	return &PlatformHealthAggregator{
		subsystems: make(map[string]SubsystemHealth),
		cacheTTL:   cacheTTL,
	}
}

// Register adds a subsystem health provider.
func (a *PlatformHealthAggregator) Register(name string, sh SubsystemHealth) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.subsystems[name] = sh
}

// Unregister removes a subsystem health provider.
func (a *PlatformHealthAggregator) Unregister(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.subsystems, name)
}

// SubsystemNames returns a sorted list of registered subsystem names.
func (a *PlatformHealthAggregator) SubsystemNames() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	names := make([]string, 0, len(a.subsystems))
	for name := range a.subsystems {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Aggregate collects health from all subsystems concurrently and returns
// a unified DashboardReport. Results are cached for the configured TTL.
// If the cache is fresh, the cached report is returned without calling
// subsystem health checks.
func (a *PlatformHealthAggregator) Aggregate(ctx context.Context) *DashboardReport {
	a.mu.RLock()
	if a.cache != nil && time.Since(a.cache.GeneratedAt) < a.cacheTTL {
		report := a.cache
		a.mu.RUnlock()
		return report
	}
	a.mu.RUnlock()

	return a.aggregateFresh(ctx)
}

// Invalidate clears the cache, forcing the next Aggregate call to re-check.
func (a *PlatformHealthAggregator) Invalidate() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cache = nil
}

// Cached returns the cached report without triggering a fresh check,
// or nil if the cache is empty or stale.
func (a *PlatformHealthAggregator) Cached() *DashboardReport {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.cache != nil && time.Since(a.cache.GeneratedAt) < a.cacheTTL {
		return a.cache
	}
	return nil
}

// aggregateFresh performs concurrent health checks on all subsystems.
func (a *PlatformHealthAggregator) aggregateFresh(ctx context.Context) *DashboardReport {
	a.mu.Lock()
	subsystems := make([]SubsystemHealth, 0, len(a.subsystems))
	names := make([]string, 0, len(a.subsystems))
	for name, sh := range a.subsystems {
		subsystems = append(subsystems, sh)
		names = append(names, name)
	}
	a.mu.Unlock()

	sort.Strings(names)

	start := time.Now()

	type result struct {
		name   string
		status SubsystemStatus
	}

	results := make([]result, len(subsystems))
	var wg sync.WaitGroup

	for i, sh := range subsystems {
		wg.Add(1)
		go func(idx int, name string, provider SubsystemHealth) {
			defer wg.Done()
			status := provider.HealthCheck(ctx)
			if status.Name == "" {
				status.Name = name
			}
			if status.UpdatedAt.IsZero() {
				status.UpdatedAt = time.Now()
			}
			results[idx] = result{name: name, status: status}
		}(i, names[i], sh)
	}

	wg.Wait()

	latency := time.Since(start)

	// Build the report — sorted by subsystem name for deterministic output.
	statuses := make([]SubsystemStatus, 0, len(results))
	for _, r := range results {
		statuses = append(statuses, r.status)
	}
	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].Name < statuses[j].Name
	})

	overall := computeOverallState(statuses)
	critical := collectByState(statuses, StateDown, true)
	degraded := collectByState(statuses, StateDegraded, false)

	report := &DashboardReport{
		Overall:     overall,
		Subsystems:  statuses,
		Critical:    critical,
		Degraded:    degraded,
		GeneratedAt: time.Now(),
		CacheTTL:    a.cacheTTL,
		Latency:     latency,
	}

	a.mu.Lock()
	a.cache = report
	a.mu.Unlock()

	return report
}

// computeOverallState determines the overall platform health from
// subsystem states.
//   - Any critical subsystem DOWN → overall DOWN
//   - Any critical subsystem DEGRADED → overall DEGRADED
//   - Any non-critical DOWN → overall DEGRADED
//   - All healthy → HEALTHY
func computeOverallState(statuses []SubsystemStatus) HealthState {
	hasCriticalDown := false
	hasDegraded := false
	hasNonCriticalDown := false

	for _, s := range statuses {
		switch s.State {
		case StateDown:
			if s.Critical() {
				hasCriticalDown = true
			} else {
				hasNonCriticalDown = true
			}
		case StateDegraded:
			hasDegraded = true
		}
	}

	if hasCriticalDown {
		return StateDown
	}
	if hasDegraded || hasNonCriticalDown {
		return StateDegraded
	}
	return StateHealthy
}

// collectByState returns subsystem names matching the given state.
// If criticalOnly is true, only critical subsystems are included.
func collectByState(statuses []SubsystemStatus, state HealthState, criticalOnly bool) []string {
	var matches []string
	for _, s := range statuses {
		if s.State != state {
			continue
		}
		if criticalOnly && !s.Critical() {
			continue
		}
		matches = append(matches, s.Name)
	}
	sort.Strings(matches)
	return matches
}

// StateEmoji returns a unicode emoji for a health state (for CLI display).
func StateEmoji(state HealthState) string {
	switch state {
	case StateHealthy:
		return "✅"
	case StateDegraded:
		return "⚠️"
	case StateDown:
		return "❌"
	default:
		return "❓"
	}
}

// FormatDashboard renders a human-readable summary of the dashboard
// suitable for CLI output (`helix status`).
func FormatDashboard(report *DashboardReport) string {
	if report == nil {
		return "No health data available."
	}

	var b []byte
	b = append(b, fmt.Sprintf("Helix Platform Health: %s %s\n", StateEmoji(report.Overall), report.Overall)...)
	b = append(b, fmt.Sprintf("Generated: %s (latency: %s)\n", report.GeneratedAt.Format(time.RFC3339), report.Latency.Round(time.Millisecond))...)
	b = append(b, fmt.Sprintf("Subsystem count: %d\n\n", len(report.Subsystems))...)

	if len(report.Critical) > 0 {
		b = append(b, "CRITICAL FAILURES:\n"...)
		for _, name := range report.Critical {
			b = append(b, fmt.Sprintf("  ❌ %s\n", name)...)
		}
		b = append(b, '\n')
	}

	b = append(b, "Subsystems:\n"...)
	for _, s := range report.Subsystems {
		b = append(b, fmt.Sprintf("  %s %-12s %s\n", StateEmoji(s.State), s.Name, s.Message)...)
		if len(s.Metrics) > 0 {
			keys := make([]string, 0, len(s.Metrics))
			for k := range s.Metrics {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				b = append(b, fmt.Sprintf("       %s: %s\n", k, s.Metrics[k])...)
			}
		}
	}

	return string(b)
}

// ServiceHealthAdapter wraps an existing health.ServiceCheck-based service
// so it can be registered with the PlatformHealthAggregator.
type ServiceHealthAdapter struct {
	Name     string
	Critical bool
	Check    func(ctx context.Context) (bool, string, error)
}

// HealthCheck implements SubsystemHealth by calling the wrapped check
// function and translating the result into a SubsystemStatus.
func (a *ServiceHealthAdapter) HealthCheck(ctx context.Context) SubsystemStatus {
	healthy, msg, err := a.Check(ctx)
	if err != nil {
		return SubsystemStatus{
			Name:    a.Name,
			State:   StateDown,
			Message: err.Error(),
		}
	}
	if !healthy {
		return SubsystemStatus{
			Name:    a.Name,
			State:   StateDown,
			Message: msg,
		}
	}
	return SubsystemStatus{
		Name:    a.Name,
		State:   StateHealthy,
		Message: msg,
	}
}
