package integration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// CircuitBreaker tests
// ---------------------------------------------------------------------------

func TestNewCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker(5, 60*time.Second)
	assert.Equal(t, 5, cb.MaxFailures)
	assert.Equal(t, 60*time.Second, cb.ResetTimeout)
	assert.Equal(t, "closed", cb.State())
	assert.Equal(t, 0, cb.Failures())
}

func TestCircuitBreaker_ClosedState(t *testing.T) {
	cb := NewCircuitBreaker(3, 1*time.Second)
	assert.True(t, cb.Allow())
	assert.True(t, cb.Allow())
	assert.True(t, cb.Allow())
	assert.Equal(t, "closed", cb.State())
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker(2, 1*time.Hour)
	cb.RecordFailure()
	assert.Equal(t, 1, cb.Failures())
	assert.Equal(t, "closed", cb.State())
	assert.True(t, cb.Allow())

	cb.RecordFailure()
	assert.Equal(t, 2, cb.Failures())
	assert.Equal(t, "open", cb.State())
	assert.False(t, cb.Allow())
}

func TestCircuitBreaker_OpensAtExactThreshold(t *testing.T) {
	cb := NewCircuitBreaker(3, 1*time.Hour)
	cb.RecordFailure() // 1
	cb.RecordFailure() // 2
	cb.RecordFailure() // 3 — should open
	assert.Equal(t, "open", cb.State())
	assert.False(t, cb.Allow())
}

func TestCircuitBreaker_OpensAboveThreshold(t *testing.T) {
	cb := NewCircuitBreaker(1, 1*time.Hour)
	cb.RecordFailure() // 1 — opens
	cb.RecordFailure() // 2 — stays open
	assert.Equal(t, "open", cb.State())
	assert.Equal(t, 2, cb.Failures())
}

func TestCircuitBreaker_HalfOpenProbe(t *testing.T) {
	cb := NewCircuitBreaker(1, 10*time.Millisecond)
	cb.RecordFailure()
	assert.Equal(t, "open", cb.State())
	assert.False(t, cb.Allow())

	// Wait for reset timeout
	time.Sleep(15 * time.Millisecond)
	assert.True(t, cb.Allow())       // transitions to half-open
	assert.Equal(t, "half-open", cb.State())
	assert.True(t, cb.Allow())       // still half-open (probe in progress)
	assert.Equal(t, "half-open", cb.State())
}

func TestCircuitBreaker_RecordSuccess(t *testing.T) {
	cb := NewCircuitBreaker(1, 10*time.Millisecond)
	cb.RecordFailure()
	assert.Equal(t, "open", cb.State())

	time.Sleep(15 * time.Millisecond)
	assert.True(t, cb.Allow())        // half-open
	cb.RecordSuccess()                // should close
	assert.Equal(t, "closed", cb.State())
	assert.Equal(t, 0, cb.Failures())
	assert.True(t, cb.Allow())
}

func TestCircuitBreaker_ResetTimeoutNotElapsed(t *testing.T) {
	cb := NewCircuitBreaker(1, 1*time.Hour)
	cb.RecordFailure()
	assert.False(t, cb.Allow()) // still open, timeout hasn't elapsed
	assert.Equal(t, "open", cb.State()) // stays open
}

func TestCircuitBreaker_MultipleFailuresAboveThreshold(t *testing.T) {
	cb := NewCircuitBreaker(1, 1*time.Hour)
	cb.RecordFailure() // opens
	cb.RecordFailure() // stays open
	cb.RecordFailure() // stays open
	assert.Equal(t, 3, cb.Failures())
	assert.Equal(t, "open", cb.State())
}

// ---------------------------------------------------------------------------
// Error types tests
// ---------------------------------------------------------------------------

func TestCircuitOpenError(t *testing.T) {
	err := &CircuitOpenError{Service: "forgejo", Since: 5 * time.Minute}
	assert.Contains(t, err.Error(), "circuit open for forgejo")
	assert.Contains(t, err.Error(), "5m")
}

func TestBudgetExhaustedError(t *testing.T) {
	err := &BudgetExhaustedError{Agent: "wojons", Cost: 2.50, Remaining: 0.10}
	assert.Contains(t, err.Error(), "wojons")
	assert.Contains(t, err.Error(), "$2.50")
	assert.Contains(t, err.Error(), "$0.10")
}

func TestServiceUnavailableError(t *testing.T) {
	inner := assert.AnError
	err := &ServiceUnavailableError{Service: "chimera", Err: inner}
	assert.Contains(t, err.Error(), "chimera unavailable")
	assert.Equal(t, inner, err.Unwrap())
}

// ---------------------------------------------------------------------------
// ServiceEndpoint tests
// ---------------------------------------------------------------------------

func TestDefaultServiceEndpoints(t *testing.T) {
	eps := DefaultServiceEndpoints()

	t.Run("forgejo", func(t *testing.T) {
		ep, ok := eps["forgejo"]
		assert.True(t, ok)
		assert.Equal(t, "http://forgejo-helix:3000", ep.InternalURL)
		assert.Equal(t, "http://localhost:3030", ep.ExternalURL)
		assert.Equal(t, "basic", ep.AuthType)
		assert.Equal(t, "/api/v1/version", ep.HealthPath)
	})

	t.Run("chimera", func(t *testing.T) {
		ep, ok := eps["chimera"]
		assert.True(t, ok)
		assert.Equal(t, "http://chimera-helix:8765", ep.InternalURL)
		assert.Equal(t, "/v1/health", ep.HealthPath)
	})

	t.Run("langfuse", func(t *testing.T) {
		ep, ok := eps["langfuse"]
		assert.True(t, ok)
		assert.Equal(t, "http://localhost:3001", ep.ExternalURL)
	})

	t.Run("muster", func(t *testing.T) {
		ep, ok := eps["muster"]
		assert.True(t, ok)
		assert.Equal(t, "http://localhost:9090", ep.ExternalURL)
	})

	t.Run("hivemind", func(t *testing.T) {
		ep, ok := eps["hivemind"]
		assert.True(t, ok)
		assert.Equal(t, "http://localhost:8081", ep.ExternalURL)
	})

	t.Run("count", func(t *testing.T) {
		assert.Len(t, eps, 5)
	})
}
