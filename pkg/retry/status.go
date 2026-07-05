// Package retry — status.go
//
// Registry for tracking named retry policies and their circuit-breaker state.
// Provides observability for the retry layer per spec §14.1 (Component Failure
// Matrix — circuit breakers + retry policies) and §14.3 (Retry Policies —
// 4-attempt exponential, 5-failure-in-60s circuit breaker).
package retry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

// ============================================================================
// Circuit Breaker State
// ============================================================================

// CircuitState represents the state of a circuit breaker.
type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"    // normal operation, requests flow
	CircuitOpen     CircuitState = "open"      // tripped, requests blocked
	CircuitHalfOpen CircuitState = "half-open" // probing, limited requests allowed
)

// ============================================================================
// PolicyStats — internal type with mutex
// ============================================================================

// PolicyStats tracks runtime statistics for a named retry policy.
// It is not safe to copy (contains sync.Mutex); use Snapshot() for read-only access.
type PolicyStats struct {
	mu                sync.Mutex
	Name              string
	Config            Config
	Attempts          int64
	Successes         int64
	Failures          int64
	LastError         string
	LastAttemptAt     time.Time
	CircuitState      CircuitState
	FailureWindow     []time.Time // rolling window of failure timestamps
	MaxFailures       int         // threshold to open circuit
	WindowDuration    time.Duration
	HalfOpenProbes    int
	HalfOpenSuccesses int
	LastStateChange   time.Time
}

// PolicyStatsSnapshot is a read-only copy of PolicyStats without the mutex.
// Used for returning from Snapshot() and Status() to avoid lock copying.
type PolicyStatsSnapshot struct {
	Name               string        `json:"name"`
	Config             Config        `json:"config"`
	Attempts           int64         `json:"attempts"`
	Successes          int64         `json:"successes"`
	Failures           int64         `json:"failures"`
	LastError          string        `json:"last_error,omitempty"`
	LastAttemptAt      time.Time     `json:"last_attempt_at,omitempty"`
	CircuitState       CircuitState  `json:"circuit_state"`
	FailureWindowCount int           `json:"failure_window_count"`
	MaxFailures        int           `json:"max_failures"`
	WindowDuration     time.Duration `json:"window_duration"`
	HalfOpenProbes     int           `json:"half_open_probes,omitempty"`
	HalfOpenSuccesses  int           `json:"half_open_successes,omitempty"`
	LastStateChange    time.Time     `json:"last_state_change,omitempty"`
}

// newPolicyStats creates a PolicyStats with default circuit breaker config.
func newPolicyStats(name string, cfg Config) *PolicyStats {
	return &PolicyStats{
		Name:           name,
		Config:         cfg,
		CircuitState:   CircuitClosed,
		MaxFailures:    5,
		WindowDuration: 60 * time.Second,
	}
}

// RecordResult updates stats based on a function call result.
func (s *PolicyStats) RecordResult(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Attempts++
	s.LastAttemptAt = time.Now()

	if err == nil {
		s.Successes++
		s.LastError = ""
		if s.CircuitState == CircuitHalfOpen {
			s.HalfOpenSuccesses++
			if s.HalfOpenSuccesses >= 2 {
				s.transitionTo(CircuitClosed)
			}
		}
		return
	}

	s.Failures++
	s.LastError = err.Error()
	s.FailureWindow = append(s.FailureWindow, time.Now())

	cutoff := time.Now().Add(-s.WindowDuration)
	pruned := s.FailureWindow[:0]
	for _, t := range s.FailureWindow {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}
	s.FailureWindow = pruned

	if s.CircuitState == CircuitClosed && len(s.FailureWindow) >= s.MaxFailures {
		s.transitionTo(CircuitOpen)
	}
}

func (s *PolicyStats) transitionTo(state CircuitState) {
	s.CircuitState = state
	s.LastStateChange = time.Now()
	s.HalfOpenProbes = 0
	s.HalfOpenSuccesses = 0
}

// CanAttempt returns true if the circuit allows a request through.
func (s *PolicyStats) CanAttempt() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch s.CircuitState {
	case CircuitClosed:
		return true
	case CircuitOpen:
		recoveryTimeout := s.Config.MaxBackoff
		if recoveryTimeout <= 0 {
			recoveryTimeout = 30 * time.Second
		}
		if time.Since(s.LastStateChange) >= recoveryTimeout {
			s.transitionTo(CircuitHalfOpen)
			return true
		}
		return false
	case CircuitHalfOpen:
		s.HalfOpenProbes++
		return s.HalfOpenProbes <= 3
	default:
		return true
	}
}

// Reset clears all accumulated stats and resets the circuit to closed.
func (s *PolicyStats) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Attempts = 0
	s.Successes = 0
	s.Failures = 0
	s.LastError = ""
	s.FailureWindow = nil
	s.HalfOpenProbes = 0
	s.HalfOpenSuccesses = 0
	s.CircuitState = CircuitClosed
	s.LastStateChange = time.Time{}
	s.LastAttemptAt = time.Time{}
}

// Snapshot returns a read-only copy of the current stats.
func (s *PolicyStats) Snapshot() PolicyStatsSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return PolicyStatsSnapshot{
		Name:               s.Name,
		Config:             s.Config,
		Attempts:           s.Attempts,
		Successes:          s.Successes,
		Failures:           s.Failures,
		LastError:          s.LastError,
		LastAttemptAt:      s.LastAttemptAt,
		CircuitState:       s.CircuitState,
		FailureWindowCount: len(s.FailureWindow),
		MaxFailures:        s.MaxFailures,
		WindowDuration:     s.WindowDuration,
		HalfOpenProbes:     s.HalfOpenProbes,
		HalfOpenSuccesses:  s.HalfOpenSuccesses,
		LastStateChange:    s.LastStateChange,
	}
}

// SuccessRate returns the success rate as a fraction (0.0 to 1.0).
func (s *PolicyStats) SuccessRate() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Attempts == 0 {
		return 0
	}
	return float64(s.Successes) / float64(s.Attempts)
}

// ============================================================================
// Registry
// ============================================================================

type Registry struct {
	mu       sync.RWMutex
	policies map[string]*PolicyStats
}

func NewRegistry() *Registry {
	return &Registry{policies: make(map[string]*PolicyStats)}
}

func (r *Registry) Register(name string, cfg Config) *PolicyStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.policies[name]; ok {
		return existing
	}
	stats := newPolicyStats(name, cfg)
	r.policies[name] = stats
	return stats
}

func (r *Registry) Get(name string) *PolicyStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.policies[name]
}

// Status returns snapshots of all registered policies, sorted by name.
func (r *Registry) Status() []PolicyStatsSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []PolicyStatsSnapshot
	for _, s := range r.policies {
		result = append(result, s.Snapshot())
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func (r *Registry) RecordResult(name string, err error) {
	r.mu.RLock()
	stats, ok := r.policies[name]
	r.mu.RUnlock()
	if !ok {
		stats = r.Register(name, DefaultConfig())
	}
	stats.RecordResult(err)
}

func (r *Registry) Reset(name string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if name == "" {
		for _, s := range r.policies {
			s.Reset()
		}
		return
	}
	if s, ok := r.policies[name]; ok {
		s.Reset()
	}
}

func (r *Registry) CircuitBreakers() map[string]CircuitState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]CircuitState, len(r.policies))
	for name, s := range r.policies {
		s.mu.Lock()
		result[name] = s.CircuitState
		s.mu.Unlock()
	}
	return result
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.policies))
	for name := range r.policies {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.policies)
}

// ============================================================================
// DefaultRegistry — global singleton for CLI access
// ============================================================================

var (
	defaultRegistry     *Registry
	defaultRegistryOnce sync.Once
)

func DefaultRegistry() *Registry {
	defaultRegistryOnce.Do(func() {
		defaultRegistry = NewRegistry()
	})
	return defaultRegistry
}

// ============================================================================
// ChaosInjector
// ============================================================================

type ChaosInjector struct {
	FailureRate  float64
	Duration     time.Duration
	StartTime    time.Time
	Enabled      bool
	FailureError error
}

func NewChaosInjector(failureRate float64, duration time.Duration) *ChaosInjector {
	return &ChaosInjector{
		FailureRate:  failureRate,
		Duration:     duration,
		StartTime:    time.Now(),
		Enabled:      chaosEnabled(),
		FailureError: errors.New("chaos: injected failure"),
	}
}

func chaosEnabled() bool {
	return osGetenv("HELIX_CHAOS_ENABLED") == "1"
}

var osGetenv = os.Getenv

func (c *ChaosInjector) IsActive() bool {
	if !c.Enabled || c.FailureRate <= 0 {
		return false
	}
	if time.Since(c.StartTime) > c.Duration {
		return false
	}
	return true
}

func (c *ChaosInjector) ShouldFail() bool {
	if !c.IsActive() {
		return false
	}
	ns := time.Now().UnixNano()
	threshold := int64(c.FailureRate * 100)
	return (ns % 100) < threshold
}

func (c *ChaosInjector) MaybeFail() error {
	if c.ShouldFail() {
		return c.FailureError
	}
	return nil
}

func WrapWithChaos[T any](fn RetryableFunc[T], chaos *ChaosInjector) RetryableFunc[T] {
	return func(ctx context.Context) (T, error) {
		if err := chaos.MaybeFail(); err != nil {
			var zero T
			return zero, err
		}
		return fn(ctx)
	}
}

// ============================================================================
// Status Report Formatting
// ============================================================================

type StatusReport struct {
	Policies         []PolicyStatsSnapshot `json:"policies"`
	CircuitBreakers  map[string]string     `json:"circuit_breakers"`
	TotalPolicies    int                   `json:"total_policies"`
	OpenCircuits     int                   `json:"open_circuits"`
	HalfOpenCircuits int                   `json:"half_open_circuits"`
}

func BuildStatusReport(r *Registry) StatusReport {
	policies := r.Status()
	breakers := r.CircuitBreakers()

	report := StatusReport{
		Policies:        policies,
		CircuitBreakers: make(map[string]string, len(breakers)),
		TotalPolicies:   len(policies),
	}
	for name, state := range breakers {
		report.CircuitBreakers[name] = string(state)
		switch state {
		case CircuitOpen:
			report.OpenCircuits++
		case CircuitHalfOpen:
			report.HalfOpenCircuits++
		}
	}
	return report
}

func FormatStatusTable(report StatusReport) string {
	var b []byte
	b = append(b, []byte(fmt.Sprintf("%-20s %-12s %-8s %-8s %-8s %-10s %s\n",
		"POLICY", "CIRCUIT", "ATTEMPTS", "SUCC", "FAIL", "SUCCESS%", "LAST_ERROR"))...)
	b = append(b, []byte(fmt.Sprintf("%s\n", repeat("-", 90)))...)

	for _, s := range report.Policies {
		successPct := 0.0
		if s.Attempts > 0 {
			successPct = float64(s.Successes) / float64(s.Attempts) * 100
		}
		lastErr := s.LastError
		if len(lastErr) > 30 {
			lastErr = lastErr[:27] + "..."
		}
		b = append(b, []byte(fmt.Sprintf("%-20s %-12s %-8d %-8d %-8d %-10.1f %s\n",
			s.Name, s.CircuitState, s.Attempts, s.Successes, s.Failures, successPct, lastErr))...)
	}

	b = append(b, []byte(fmt.Sprintf("\n%d policies | %d open circuits | %d half-open\n",
		report.TotalPolicies, report.OpenCircuits, report.HalfOpenCircuits))...)
	return string(b)
}

func repeat(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}
