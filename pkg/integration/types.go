package integration

import (
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Shared integration types — used across all sub-project adapters
// ---------------------------------------------------------------------------

// CircuitBreaker implements the cross-component circuit breaker pattern
// (specs/cross-component-wiring.md §8). All cross-service HTTP calls use
// circuit breakers to prevent cascading failures.
type CircuitBreaker struct {
	MaxFailures  int           // Number of consecutive failures before opening
	ResetTimeout time.Duration // Time before attempting a half-open probe
	failures     int
	state        string // "closed", "open", "half-open"
	lastFailure  time.Time
}

// NewCircuitBreaker creates a circuit breaker with the given thresholds.
func NewCircuitBreaker(maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		MaxFailures:  maxFailures,
		ResetTimeout: resetTimeout,
		state:        "closed",
	}
}

// State returns the current circuit breaker state ("closed", "open", "half-open").
func (cb *CircuitBreaker) State() string { return cb.state }

// RecordFailure records a failure and opens the circuit if the threshold is exceeded.
func (cb *CircuitBreaker) RecordFailure() {
	cb.failures++
	cb.lastFailure = time.Now()
	if cb.failures >= cb.MaxFailures {
		cb.state = "open"
	}
}

// RecordSuccess resets the failure count and closes the circuit.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.failures = 0
	cb.state = "closed"
}

// Allow returns true if the circuit is closed, or half-open (probe allowed).
// A circuit transitions from open to half-open after ResetTimeout elapses.
func (cb *CircuitBreaker) Allow() bool {
	switch cb.state {
	case "closed":
		return true
	case "open":
		if time.Since(cb.lastFailure) >= cb.ResetTimeout {
			cb.state = "half-open"
			return true
		}
		return false
	case "half-open":
		return true
	default:
		return false
	}
}

// Failures returns the current failure count.
func (cb *CircuitBreaker) Failures() int { return cb.failures }

// ---------------------------------------------------------------------------
// Service address configuration (specs/cross-component-wiring.md §1)
// ---------------------------------------------------------------------------

// ServiceEndpoint holds the address and auth configuration for a service.
type ServiceEndpoint struct {
	InternalURL string // Docker network URL (e.g., "http://forgejo-helix:3000")
	ExternalURL string // Host-accessible URL (e.g., "http://localhost:3030")
	AuthType    string // "basic", "bearer", "none"
	HealthPath  string // Health check path (e.g., "/api/v1/version")
}

// DefaultServiceEndpoints returns the standard Helix service address table.
func DefaultServiceEndpoints() map[string]ServiceEndpoint {
	return map[string]ServiceEndpoint{
		"forgejo": {
			InternalURL: "http://forgejo-helix:3000",
			ExternalURL: "http://localhost:3030",
			AuthType:    "basic",
			HealthPath:  "/api/v1/version",
		},
		"chimera": {
			InternalURL: "http://chimera-helix:8765",
			ExternalURL: "http://localhost:8765",
			AuthType:    "none",
			HealthPath:  "/v1/health",
		},
		"langfuse": {
			InternalURL: "http://langfuse-helix:3000",
			ExternalURL: "http://localhost:3001",
			AuthType:    "bearer",
			HealthPath:  "/api/public/health",
		},
		"muster": {
			InternalURL: "http://muster-helix:9090",
			ExternalURL: "http://localhost:9090",
			AuthType:    "none",
			HealthPath:  "/health",
		},
		"hivemind": {
			InternalURL: "http://hivemind-helix:8081",
			ExternalURL: "http://localhost:8081",
			AuthType:    "bearer",
			HealthPath:  "/health",
		},
	}
}

// ---------------------------------------------------------------------------
// Error propagation (specs/cross-component-wiring.md §7)
// ---------------------------------------------------------------------------

// CircuitOpenError is returned when a circuit breaker is open.
type CircuitOpenError struct {
	Service string
	Since   time.Duration
}

func (e *CircuitOpenError) Error() string {
	return fmt.Sprintf("circuit open for %s (open for %v)", e.Service, e.Since.Round(time.Second))
}

// BudgetExhaustedError is returned when an agent's budget is exhausted.
type BudgetExhaustedError struct {
	Agent     string
	Cost      float64
	Remaining float64
}

func (e *BudgetExhaustedError) Error() string {
	return fmt.Sprintf("budget exhausted for %s: cost $%.2f > remaining $%.2f",
		e.Agent, e.Cost, e.Remaining)
}

// ServiceUnavailableError is returned when a downstream service is unreachable.
type ServiceUnavailableError struct {
	Service string
	Err     error
}

func (e *ServiceUnavailableError) Error() string {
	return fmt.Sprintf("%s unavailable: %v", e.Service, e.Err)
}

func (e *ServiceUnavailableError) Unwrap() error { return e.Err }
