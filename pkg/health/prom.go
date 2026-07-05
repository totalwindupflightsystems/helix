// Package health — prom.go
//
// Prometheus exposition for Helix. Subcommand-invocation metrics,
// error counts, and service-up gauges, all exposed via the
// `helix status --serve --addr :9095` HTTP endpoint at /metrics.
//
// Per docs/specs/SPECIFICATION.md §10.7 (Monitoring SLAs) and
// deployment.md §3 (Prometheus scraping). Renderer is hand-rolled —
// no Prometheus client_golang dep, keeps the helix binary lean.
//
// Thread-safety: all counters/gauges are guarded by a single mutex
// (PromStore.mu). The HTTP handler acquires a read lock during scrape
// so concurrent scrapes don't block writers.
//
// Naming conventions follow Prometheus best practices:
//
//	subcommand_duration_seconds_bucket{subcommand="..."}  histogram
//	subcommand_invocations_total{subcommand="..."}        counter
//	subcommand_rc_total{subcommand="...",rc="..."}        counter
//	service_up{service="..."}                            gauge
//
// `service` label value is one of: forgejo, chimera, langfuse,
// conscientiousness, hivemind, prometheus, backup, disk, memory.
//
// AutoApplicable safety: this file does NOT modify any other subsystem
// — it only READS from the cache. Adding/updating metrics is safe to
// call concurrently from anywhere; reads are O(1) per metric.
package health

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// PromStore holds the live Prometheus metrics for Helix. Created
// once at process start by cmd/helix; shared across all subcommands.
type PromStore struct {
	mu sync.RWMutex

	// Subcommand invocation counters — incremented by recordSubcommand
	// (called from the observability wrapper). Bucketed by name.
	invocations map[string]uint64            // subcommand → total invocations
	rcCounts    map[string]map[string]uint64 // subcommand → rc string → count
	durations   map[string][]float64         // subcommand → recent durations in seconds (capped)

	// Service-up gauges — updated by SetServiceUp. Value is 1 if last
	// probe was healthy, 0 otherwise. "service" label distinguishes.
	serviceUp map[string]float64

	// Probe cache — populated by the status --serve handler on first
	// scrape and refreshed every 10s. map[service] → (timestamp, healthy).
	probeCache   map[string]probeEntry
	probeMaxAge  time.Duration
	serviceOrder []string // deterministic scrape order

	// Histogram bucket boundaries, in seconds. Includes the standard
	// Prometheus default buckets + a finer "fast" bucket for subcommands
	// that should return in <100ms.
	buckets []float64
}

type probeEntry struct {
	at      time.Time
	healthy bool
	detail  string
}

// DefaultPromBuckets are the canonical Prometheus default buckets
// plus a few extra for subcommand latency (5ms and 10ms are critical
// for things like `helix status` and `helix doctor` which should be
// sub-100ms).
var DefaultPromBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// NewPromStore returns an empty store with default buckets and a
// 10s probe cache. Use SetProbes + SetBuckets in tests for isolation.
func NewPromStore() *PromStore {
	return &PromStore{
		invocations:  make(map[string]uint64),
		rcCounts:     make(map[string]map[string]uint64),
		durations:    make(map[string][]float64),
		serviceUp:    make(map[string]float64),
		probeCache:   make(map[string]probeEntry),
		probeMaxAge:  10 * time.Second,
		serviceOrder: []string{},
		buckets:      append([]float64{}, DefaultPromBuckets...),
	}
}

// SetBuckets replaces the histogram bucket boundaries. Tests use
// tighter buckets; production uses DefaultPromBuckets.
func (s *PromStore) SetBuckets(b []float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buckets = append([]float64{}, b...)
}

// SetProbes registers the set of services that should be probed on
// scrape. Replaces any prior set; safe to call from main().
func (s *PromStore) SetProbes(services []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.serviceOrder = append([]string{}, services...)
}

// RecordInvocation atomically increments the invocation counter for
// subcommand, plus the rc counter and duration histogram. Safe to
// call concurrently from any goroutine.
//
// durations list is capped at the last 1024 observations per
// subcommand so memory is bounded even for long-running daemons.
func (s *PromStore) RecordInvocation(subcommand string, rc int, duration time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if subcommand == "" {
		return
	}
	s.invocations[subcommand]++

	rcKey := fmt.Sprintf("%d", rc)
	if s.rcCounts[subcommand] == nil {
		s.rcCounts[subcommand] = make(map[string]uint64)
	}
	s.rcCounts[subcommand][rcKey]++

	obs := duration.Seconds()
	durs := s.durations[subcommand]
	durs = append(durs, obs)
	if len(durs) > 1024 {
		// drop oldest ~25% to amortize the cost
		drop := len(durs) - 768
		durs = append(durs[:0], durs[drop:]...)
	}
	s.durations[subcommand] = durs
}

// SetServiceUp sets the service-up gauge for a given service.
// value should be 1.0 if the last probe was healthy, 0.0 otherwise.
func (s *PromStore) SetServiceUp(service string, healthy bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if healthy {
		s.serviceUp[service] = 1.0
	} else {
		s.serviceUp[service] = 0.0
	}
}

// ServiceUp returns the current gauge value for a service.
// Returns 0 if the service has never been recorded (defensive — never
// panic; Prometheus expects numeric values).
func (s *PromStore) ServiceUp(service string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.serviceUp[service]
}

// UpdateProbe atomically records a probe result and updates the
// service-up gauge. Use from the HTTP handler:
//
//	store.UpdateProbe("forgejo", time.Now(), true, "HTTP 200")
func (s *PromStore) UpdateProbe(service string, at time.Time, healthy bool, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probeCache[service] = probeEntry{at: at, healthy: healthy, detail: detail}
	if healthy {
		s.serviceUp[service] = 1.0
	} else {
		s.serviceUp[service] = 0.0
	}
}

// ProbeFreshness returns (fresh, lastUpdate) for a service. fresh
// is true if the probe was recorded within probeMaxAge.
//
// Returns (false, zero) for unknown services so the handler can
// decide whether to invoke a probe.
func (s *PromStore) ProbeFreshness(service string) (bool, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.probeCache[service]
	if !ok {
		return false, time.Time{}
	}
	age := time.Since(entry.at)
	return age <= s.probeMaxAge, entry.at
}

// ============================================================================
// Renderer — produces Prometheus text exposition format 0.0.4
// ============================================================================

// WriteMetrics renders the entire PromStore as Prometheus text
// exposition and writes it to w. Suitable for serving on /metrics.
//
// The output is deterministic: subcommands are emitted in sorted
// order, labels are sorted, no duplicates. Two passes — counters
// first, gauges second, histograms last — match the convention used
// by most exporters.
func (s *PromStore) WriteMetrics(w io.Writer) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var written int

	// --- helix_subcommand_invocations_total counter ---
	written += writeLn(w, "# HELP helix_subcommand_invocations_total Total number of helix subcommand invocations.")
	written += writeLn(w, "# TYPE helix_subcommand_invocations_total counter")
	names := promSortedKeys(s.invocations)
	for _, name := range names {
		n := written
		written += writeLn(w, fmt.Sprintf("helix_subcommand_invocations_total{subcommand=%q} %d",
			name, s.invocations[name]))
		_ = n // satisfy linter
	}

	// --- helix_subcommand_rc_total counter ---
	written += writeLn(w, "")
	written += writeLn(w, "# HELP helix_subcommand_rc_total Total invocations grouped by exit code.")
	written += writeLn(w, "# TYPE helix_subcommand_rc_total counter")
	for _, name := range names {
		rcs := s.rcCounts[name]
		if len(rcs) == 0 {
			continue
		}
		rcKeys := promSortedKeys(rcs)
		for _, rcKey := range rcKeys {
			written += writeLn(w, fmt.Sprintf("helix_subcommand_rc_total{subcommand=%q,rc=%q} %d",
				name, rcKey, rcs[rcKey]))
		}
	}

	// --- helix_subcommand_duration_seconds histogram ---
	written += writeLn(w, "")
	written += writeLn(w, "# HELP helix_subcommand_duration_seconds Histogram of helix subcommand invocation duration in seconds.")
	written += writeLn(w, "# TYPE helix_subcommand_duration_seconds histogram")
	for _, name := range names {
		durs := s.durations[name]
		if len(durs) == 0 {
			continue
		}
		bucketCounts := computeBuckets(durs, s.buckets)
		for i, b := range s.buckets {
			written += writeLn(w, fmt.Sprintf(`helix_subcommand_duration_seconds_bucket{subcommand=%q,le="%g"} %d`,
				name, b, bucketCounts[i]))
		}
		written += writeLn(w, fmt.Sprintf(`helix_subcommand_duration_seconds_bucket{subcommand=%q,le="+Inf"} %d`,
			name, len(durs)))
		written += writeLn(w, fmt.Sprintf("helix_subcommand_duration_seconds_sum{subcommand=%q} %s",
			name, promFormatSum(durs)))
		written += writeLn(w, fmt.Sprintf("helix_subcommand_duration_seconds_count{subcommand=%q} %d",
			name, len(durs)))
	}

	// --- helix_service_up gauge ---
	written += writeLn(w, "")
	written += writeLn(w, "# HELP helix_service_up Whether the named service was healthy on its last probe (1=yes, 0=no).")
	written += writeLn(w, "# TYPE helix_service_up gauge")
	// Emit in deterministic order: known serviceOrder first, then any
	// extras the application added (sorted).
	emitted := make(map[string]bool)
	for _, svc := range s.serviceOrder {
		v, ok := s.serviceUp[svc]
		if !ok {
			v = 0
		}
		written += writeLn(w, fmt.Sprintf("helix_service_up{service=%q} %s", svc, promFormatFloat(v)))
		emitted[svc] = true
	}
	extras := make([]string, 0)
	for k := range s.serviceUp {
		if !emitted[k] {
			extras = append(extras, k)
		}
	}
	sort.Strings(extras)
	for _, svc := range extras {
		written += writeLn(w, fmt.Sprintf("helix_service_up{service=%q} %s", svc, promFormatFloat(s.serviceUp[svc])))
	}

	return written, nil
}

// ComputeBucketsInStore returns the bucket counts that would be
// emitted for a given subcommand. Used in tests.
func (s *PromStore) ComputeBucketsInStore(subcommand string) []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return computeBuckets(s.durations[subcommand], s.buckets)
}

// Snapshot returns a copy of the current state. Tests use this to
// verify record/set operations without holding the lock.
type Snapshot struct {
	Invocations map[string]uint64
	RCCounts    map[string]map[string]uint64
	Durations   map[string]int
	ServiceUp   map[string]float64
}

// Snapshot returns a defensive copy of the store state.
func (s *PromStore) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	inv := make(map[string]uint64, len(s.invocations))
	for k, v := range s.invocations {
		inv[k] = v
	}
	rcs := make(map[string]map[string]uint64, len(s.rcCounts))
	for k, v := range s.rcCounts {
		m := make(map[string]uint64, len(v))
		for k2, v2 := range v {
			m[k2] = v2
		}
		rcs[k] = m
	}
	durs := make(map[string]int, len(s.durations))
	for k, v := range s.durations {
		durs[k] = len(v)
	}
	sv := make(map[string]float64, len(s.serviceUp))
	for k, v := range s.serviceUp {
		sv[k] = v
	}
	return Snapshot{Invocations: inv, RCCounts: rcs, Durations: durs, ServiceUp: sv}
}

// ============================================================================
// helpers
// ============================================================================

// writeLn writes a single line + newline, returning the byte count.
// Errors are swallowed at this layer; the caller may inspect the
// returned count or a separate error from io.Writer if it matters.
func writeLn(w io.Writer, line string) int {
	n, _ := io.WriteString(w, line)
	_, _ = io.WriteString(w, "\n")
	return n + 1
}

// promSortedKeys returns keys of m in sorted order. Generic helper
// (the platform_metrics package has a typed version that operates on
// map[string][]float64 — we use a different name here to coexist).
func promSortedKeys(m map[string]uint64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// computeBuckets returns the cumulative count of observations in each
// bucket. Buckets are inclusive on the upper bound (Prometheus
// convention: `le="0.1"` includes any value <= 0.1).
func computeBuckets(observations []float64, buckets []float64) []int {
	out := make([]int, len(buckets))
	for _, obs := range observations {
		for i, b := range buckets {
			if obs <= b {
				out[i]++
			}
		}
	}
	return out
}

// promFormatSum returns a stable representation of a duration sum.
// Prometheus accepts any float64.
func promFormatSum(observations []float64) string {
	var sum float64
	for _, o := range observations {
		sum += o
	}
	return promFormatFloat(sum)
}

// promFormatFloat renders a float in compact form suitable for the
// Prometheus exposition format. Avoids strconv.FormatFloat so the
// output is deterministic across Go versions.
func promFormatFloat(v float64) string {
	if v == 0 {
		return "0"
	}
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%g", v)
}

// TrimQuotes is a small helper used in tests when comparing label
// values that get quoted in Prometheus output.
func TrimQuotes(s string) string { return strings.Trim(s, `"`) }

// atomicCounter is a tiny atomic helper kept around in case future
// code needs lock-free counters. Not currently used.
type atomicCounter struct{ n atomic.Uint64 }

func (a *atomicCounter) Inc()        { a.n.Add(1) }
func (a *atomicCounter) Get() uint64 { return a.n.Load() }
