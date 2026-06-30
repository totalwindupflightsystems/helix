package health

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// StubSubsystemHealth — test double for SubsystemHealth interface
// ---------------------------------------------------------------------------

type stubSubsystem struct {
	name    string
	state   HealthState
	message string
	metrics map[string]string
	delay   time.Duration
	calls   int32
}

func (s *stubSubsystem) HealthCheck(ctx context.Context) SubsystemStatus {
	atomic.AddInt32(&s.calls, 1)
	if s.delay > 0 {
		select {
		case <-ctx.Done():
			return SubsystemStatus{
				Name:    s.name,
				State:   StateDown,
				Message: ctx.Err().Error(),
			}
		case <-time.After(s.delay):
		}
	}
	return SubsystemStatus{
		Name:    s.name,
		State:   s.state,
		Message: s.message,
		Metrics: s.metrics,
	}
}

// ---------------------------------------------------------------------------
// SubsystemStatus tests
// ---------------------------------------------------------------------------

func TestSubsystemStatus_Critical(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"forgejo", true},
		{"chimera", true},
		{"trust", true},
		{"review", true},
		{"negotiate", true},
		{"dispatcher", true},
		{"sandbox", true},
		{"marketplace", false},
		{"estimate", false},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := SubsystemStatus{Name: tt.name}
			assert.Equal(t, tt.expected, s.Critical())
		})
	}
}

// ---------------------------------------------------------------------------
// DashboardReport tests
// ---------------------------------------------------------------------------

func TestDashboardReport_AllHealthy(t *testing.T) {
	t.Run("all healthy", func(t *testing.T) {
		r := &DashboardReport{
			Subsystems: []SubsystemStatus{
				{Name: "a", State: StateHealthy},
				{Name: "b", State: StateHealthy},
			},
		}
		assert.True(t, r.AllHealthy())
	})

	t.Run("one degraded", func(t *testing.T) {
		r := &DashboardReport{
			Subsystems: []SubsystemStatus{
				{Name: "a", State: StateHealthy},
				{Name: "b", State: StateDegraded},
			},
		}
		assert.False(t, r.AllHealthy())
	})

	t.Run("one down", func(t *testing.T) {
		r := &DashboardReport{
			Subsystems: []SubsystemStatus{
				{Name: "a", State: StateDown},
			},
		}
		assert.False(t, r.AllHealthy())
	})

	t.Run("empty", func(t *testing.T) {
		r := &DashboardReport{}
		assert.True(t, r.AllHealthy())
	})
}

func TestDashboardReport_HasCriticalFailure(t *testing.T) {
	t.Run("with critical failures", func(t *testing.T) {
		r := &DashboardReport{
			Critical: []string{"forgejo"},
		}
		assert.True(t, r.HasCriticalFailure())
	})

	t.Run("no critical failures", func(t *testing.T) {
		r := &DashboardReport{
			Critical: []string{},
		}
		assert.False(t, r.HasCriticalFailure())
	})
}

func TestDashboardReport_MarshalJSON(t *testing.T) {
	r := &DashboardReport{
		Overall: StateHealthy,
		Subsystems: []SubsystemStatus{
			{Name: "forgejo", State: StateHealthy},
		},
		GeneratedAt: time.Now(),
		CacheTTL:    15 * time.Second,
		Latency:     42 * time.Millisecond,
	}

	data, err := r.MarshalJSON()
	require.NoError(t, err)
	assert.Contains(t, string(data), `"latency_ms":42`)
	assert.Contains(t, string(data), `"cache_ttl_seconds":15`)
	assert.Contains(t, string(data), `"overall":"healthy"`)
}

// ---------------------------------------------------------------------------
// PlatformHealthAggregator — registration
// ---------------------------------------------------------------------------

func TestAggregator_Register(t *testing.T) {
	a := NewPlatformHealthAggregator(5 * time.Second)

	s1 := &stubSubsystem{name: "forgejo", state: StateHealthy}
	s2 := &stubSubsystem{name: "chimera", state: StateHealthy}

	a.Register("forgejo", s1)
	a.Register("chimera", s2)

	names := a.SubsystemNames()
	assert.Equal(t, []string{"chimera", "forgejo"}, names)
}

func TestAggregator_Unregister(t *testing.T) {
	a := NewPlatformHealthAggregator(5 * time.Second)

	s1 := &stubSubsystem{name: "forgejo", state: StateHealthy}
	a.Register("forgejo", s1)
	assert.Len(t, a.SubsystemNames(), 1)

	a.Unregister("forgejo")
	assert.Len(t, a.SubsystemNames(), 0)
}

func TestAggregator_SubsystemNames_Empty(t *testing.T) {
	a := NewPlatformHealthAggregator(5 * time.Second)
	assert.Empty(t, a.SubsystemNames())
}

func TestAggregator_SubsystemNames_Sorted(t *testing.T) {
	a := NewPlatformHealthAggregator(5 * time.Second)
	a.Register("zeta", &stubSubsystem{name: "zeta"})
	a.Register("alpha", &stubSubsystem{name: "alpha"})
	a.Register("mid", &stubSubsystem{name: "mid"})

	names := a.SubsystemNames()
	assert.Equal(t, []string{"alpha", "mid", "zeta"}, names)
}

func TestNewPlatformHealthAggregator_DefaultTTL(t *testing.T) {
	a := NewPlatformHealthAggregator(0)
	assert.Equal(t, 15*time.Second, a.cacheTTL)

	a2 := NewPlatformHealthAggregator(-1)
	assert.Equal(t, 15*time.Second, a2.cacheTTL)
}

// ---------------------------------------------------------------------------
// PlatformHealthAggregator — Aggregate (core)
// ---------------------------------------------------------------------------

func TestAggregate_AllHealthy(t *testing.T) {
	a := NewPlatformHealthAggregator(5 * time.Second)
	a.Register("forgejo", &stubSubsystem{name: "forgejo", state: StateHealthy})
	a.Register("chimera", &stubSubsystem{name: "chimera", state: StateHealthy})

	report := a.Aggregate(context.Background())

	assert.Equal(t, StateHealthy, report.Overall)
	assert.True(t, report.AllHealthy())
	assert.False(t, report.HasCriticalFailure())
	assert.Empty(t, report.Critical)
	assert.Empty(t, report.Degraded)
	assert.Len(t, report.Subsystems, 2)
	assert.Greater(t, report.Latency, time.Duration(0))
	assert.False(t, report.GeneratedAt.IsZero())
}

func TestAggregate_CriticalDown_MakesOverallDown(t *testing.T) {
	a := NewPlatformHealthAggregator(5 * time.Second)
	a.Register("forgejo", &stubSubsystem{name: "forgejo", state: StateDown, message: "connection refused"})
	a.Register("chimera", &stubSubsystem{name: "chimera", state: StateHealthy})

	report := a.Aggregate(context.Background())

	assert.Equal(t, StateDown, report.Overall)
	assert.True(t, report.HasCriticalFailure())
	assert.Contains(t, report.Critical, "forgejo")
	assert.NotContains(t, report.Critical, "chimera")
}

func TestAggregate_CriticalDegraded_MakesOverallDegraded(t *testing.T) {
	a := NewPlatformHealthAggregator(5 * time.Second)
	a.Register("forgejo", &stubSubsystem{name: "forgejo", state: StateDegraded, message: "slow responses"})
	a.Register("chimera", &stubSubsystem{name: "chimera", state: StateHealthy})

	report := a.Aggregate(context.Background())

	assert.Equal(t, StateDegraded, report.Overall)
	assert.Contains(t, report.Degraded, "forgejo")
}

func TestAggregate_NonCriticalDown_MakesOverallDegraded(t *testing.T) {
	a := NewPlatformHealthAggregator(5 * time.Second)
	a.Register("forgejo", &stubSubsystem{name: "forgejo", state: StateHealthy})
	a.Register("marketplace", &stubSubsystem{name: "marketplace", state: StateDown, message: "no agents registered"})

	report := a.Aggregate(context.Background())

	// marketplace is non-critical — its failure degrades, not downs
	assert.Equal(t, StateDegraded, report.Overall)
	assert.False(t, report.HasCriticalFailure())
	assert.NotContains(t, report.Critical, "marketplace")
}

func TestAggregate_MixedStates(t *testing.T) {
	a := NewPlatformHealthAggregator(5 * time.Second)
	a.Register("forgejo", &stubSubsystem{name: "forgejo", state: StateHealthy})
	a.Register("chimera", &stubSubsystem{name: "chimera", state: StateDegraded})
	a.Register("marketplace", &stubSubsystem{name: "marketplace", state: StateDown})
	a.Register("estimate", &stubSubsystem{name: "estimate", state: StateHealthy})

	report := a.Aggregate(context.Background())

	// chimera (critical) degraded → overall degraded
	assert.Equal(t, StateDegraded, report.Overall)
	assert.Contains(t, report.Degraded, "chimera")
	assert.NotContains(t, report.Degraded, "marketplace")
}

func TestAggregate_StateUnknown_NotHealthy(t *testing.T) {
	a := NewPlatformHealthAggregator(5 * time.Second)
	a.Register("forgejo", &stubSubsystem{name: "forgejo", state: StateUnknown})

	report := a.Aggregate(context.Background())

	assert.False(t, report.AllHealthy())
	// unknown is not down, not degraded — overall should be healthy-ish
	// but not explicitly healthy since there's an unknown state
}

func TestAggregate_SubsystemsSortedByName(t *testing.T) {
	a := NewPlatformHealthAggregator(5 * time.Second)
	a.Register("zeta", &stubSubsystem{name: "zeta", state: StateHealthy})
	a.Register("alpha", &stubSubsystem{name: "alpha", state: StateHealthy})
	a.Register("mid", &stubSubsystem{name: "mid", state: StateHealthy})

	report := a.Aggregate(context.Background())

	require.Len(t, report.Subsystems, 3)
	assert.Equal(t, "alpha", report.Subsystems[0].Name)
	assert.Equal(t, "mid", report.Subsystems[1].Name)
	assert.Equal(t, "zeta", report.Subsystems[2].Name)
}

func TestAggregate_SubsystemStatusUpdatedAt(t *testing.T) {
	a := NewPlatformHealthAggregator(5 * time.Second)
	a.Register("forgejo", &stubSubsystem{name: "forgejo", state: StateHealthy})

	report := a.Aggregate(context.Background())

	for _, s := range report.Subsystems {
		assert.False(t, s.UpdatedAt.IsZero(), "UpdatedAt should be set for %s", s.Name)
	}
}

func TestAggregate_PreservesMetricsAndMessage(t *testing.T) {
	a := NewPlatformHealthAggregator(5 * time.Second)
	a.Register("trust", &stubSubsystem{
		name:    "trust",
		state:   StateDegraded,
		message: "ledger replay slow",
		metrics: map[string]string{
			"agents_tracked": "42",
			"avg_score":      "0.73",
		},
	})

	report := a.Aggregate(context.Background())

	s := report.Subsystems[0]
	assert.Equal(t, StateDegraded, s.State)
	assert.Equal(t, "ledger replay slow", s.Message)
	assert.Len(t, s.Metrics, 2)
	assert.Equal(t, "42", s.Metrics["agents_tracked"])
}

// ---------------------------------------------------------------------------
// Cache tests
// ---------------------------------------------------------------------------

func TestAggregate_Cache_HitsWithinTTL(t *testing.T) {
	a := NewPlatformHealthAggregator(1 * time.Second)
	s := &stubSubsystem{name: "forgejo", state: StateHealthy}
	a.Register("forgejo", s)

	// First call — cache miss
	report1 := a.Aggregate(context.Background())
	assert.Equal(t, int32(1), atomic.LoadInt32(&s.calls))

	// Second call within TTL — cache hit
	report2 := a.Aggregate(context.Background())
	assert.Equal(t, int32(1), atomic.LoadInt32(&s.calls), "subsystem should not be re-checked within TTL")

	// Same cached pointer
	assert.Equal(t, report1, report2)
}

func TestAggregate_Cache_ExpiresAfterTTL(t *testing.T) {
	a := NewPlatformHealthAggregator(50 * time.Millisecond)
	s := &stubSubsystem{name: "forgejo", state: StateHealthy}
	a.Register("forgejo", s)

	a.Aggregate(context.Background())
	assert.Equal(t, int32(1), atomic.LoadInt32(&s.calls))

	time.Sleep(80 * time.Millisecond)

	a.Aggregate(context.Background())
	assert.Equal(t, int32(2), atomic.LoadInt32(&s.calls), "subsystem should be re-checked after TTL expiry")
}

func TestAggregate_Invalidate(t *testing.T) {
	a := NewPlatformHealthAggregator(10 * time.Second)
	s := &stubSubsystem{name: "forgejo", state: StateHealthy}
	a.Register("forgejo", s)

	a.Aggregate(context.Background())
	assert.Equal(t, int32(1), atomic.LoadInt32(&s.calls))

	a.Invalidate()

	a.Aggregate(context.Background())
	assert.Equal(t, int32(2), atomic.LoadInt32(&s.calls), "subsystem should be re-checked after invalidation")
}

func TestAggregate_Cached_FreshCache(t *testing.T) {
	a := NewPlatformHealthAggregator(5 * time.Second)
	a.Register("forgejo", &stubSubsystem{name: "forgejo", state: StateHealthy})

	// No cache yet
	assert.Nil(t, a.Cached())

	a.Aggregate(context.Background())

	// Now should have a cache
	cached := a.Cached()
	require.NotNil(t, cached)
	assert.Equal(t, StateHealthy, cached.Overall)
}

func TestAggregate_Cached_StaleReturnsNil(t *testing.T) {
	a := NewPlatformHealthAggregator(50 * time.Millisecond)
	a.Register("forgejo", &stubSubsystem{name: "forgejo", state: StateHealthy})

	a.Aggregate(context.Background())
	require.NotNil(t, a.Cached())

	time.Sleep(80 * time.Millisecond)
	assert.Nil(t, a.Cached(), "stale cache should return nil")
}

// ---------------------------------------------------------------------------
// Concurrent health checks
// ---------------------------------------------------------------------------

func TestAggregate_ConcurrentChecks(t *testing.T) {
	a := NewPlatformHealthAggregator(5 * time.Second)

	for i := 0; i < 10; i++ {
		a.Register(fmt.Sprintf("svc-%d", i), &stubSubsystem{
			name:  fmt.Sprintf("svc-%d", i),
			state: StateHealthy,
		})
	}

	report := a.Aggregate(context.Background())

	assert.True(t, report.AllHealthy())
	assert.Len(t, report.Subsystems, 10)
}

func TestAggregate_ContextCancellation(t *testing.T) {
	a := NewPlatformHealthAggregator(5 * time.Second)
	a.Register("slow", &stubSubsystem{
		name:  "slow",
		state: StateHealthy,
		delay: 2 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	report := a.Aggregate(ctx)

	s := report.Subsystems[0]
	assert.Equal(t, StateDown, s.State)
	assert.Contains(t, s.Message, "context deadline exceeded")
}

// ---------------------------------------------------------------------------
// computeOverallState tests
// ---------------------------------------------------------------------------

func TestComputeOverallState(t *testing.T) {
	tests := []struct {
		name     string
		statuses []SubsystemStatus
		expected HealthState
	}{
		{
			name:     "empty",
			statuses: []SubsystemStatus{},
			expected: StateHealthy,
		},
		{
			name: "all healthy",
			statuses: []SubsystemStatus{
				{Name: "forgejo", State: StateHealthy},
				{Name: "chimera", State: StateHealthy},
			},
			expected: StateHealthy,
		},
		{
			name: "critical down",
			statuses: []SubsystemStatus{
				{Name: "forgejo", State: StateDown},
				{Name: "chimera", State: StateHealthy},
			},
			expected: StateDown,
		},
		{
			name: "critical degraded",
			statuses: []SubsystemStatus{
				{Name: "forgejo", State: StateDegraded},
				{Name: "chimera", State: StateHealthy},
			},
			expected: StateDegraded,
		},
		{
			name: "non-critical down only",
			statuses: []SubsystemStatus{
				{Name: "marketplace", State: StateDown},
				{Name: "forgejo", State: StateHealthy},
			},
			expected: StateDegraded,
		},
		{
			name: "both critical and non-critical down",
			statuses: []SubsystemStatus{
				{Name: "forgejo", State: StateDown},
				{Name: "marketplace", State: StateDown},
			},
			expected: StateDown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := computeOverallState(tt.statuses)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ---------------------------------------------------------------------------
// collectByState tests
// ---------------------------------------------------------------------------

func TestCollectByState(t *testing.T) {
	statuses := []SubsystemStatus{
		{Name: "forgejo", State: StateDown},
		{Name: "chimera", State: StateDegraded},
		{Name: "marketplace", State: StateDown},
		{Name: "trust", State: StateHealthy},
		{Name: "review", State: StateDegraded},
	}

	t.Run("collect down critical only", func(t *testing.T) {
		result := collectByState(statuses, StateDown, true)
		assert.Equal(t, []string{"forgejo"}, result)
	})

	t.Run("collect down all", func(t *testing.T) {
		result := collectByState(statuses, StateDown, false)
		assert.Equal(t, []string{"forgejo", "marketplace"}, result)
	})

	t.Run("collect degraded all", func(t *testing.T) {
		result := collectByState(statuses, StateDegraded, false)
		assert.Equal(t, []string{"chimera", "review"}, result)
	})

	t.Run("collect healthy — empty", func(t *testing.T) {
		result := collectByState(statuses, StateHealthy, false)
		assert.Equal(t, []string{"trust"}, result)
	})
}

// ---------------------------------------------------------------------------
// StateEmoji + FormatDashboard tests
// ---------------------------------------------------------------------------

func TestStateEmoji(t *testing.T) {
	assert.Equal(t, "✅", StateEmoji(StateHealthy))
	assert.Equal(t, "⚠️", StateEmoji(StateDegraded))
	assert.Equal(t, "❌", StateEmoji(StateDown))
	assert.Equal(t, "❓", StateEmoji(StateUnknown))
	assert.Equal(t, "❓", StateEmoji(HealthState("weird")))
}

func TestFormatDashboard_AllHealthy(t *testing.T) {
	report := &DashboardReport{
		Overall: StateHealthy,
		Subsystems: []SubsystemStatus{
			{Name: "forgejo", State: StateHealthy, Message: "ok"},
			{Name: "chimera", State: StateHealthy, Message: "ok"},
		},
		GeneratedAt: time.Now(),
		Latency:     15 * time.Millisecond,
	}

	output := FormatDashboard(report)
	assert.Contains(t, output, "✅ healthy")
	assert.Contains(t, output, "forgejo")
	assert.Contains(t, output, "chimera")
}

func TestFormatDashboard_WithCriticalFailures(t *testing.T) {
	report := &DashboardReport{
		Overall:  StateDown,
		Critical: []string{"forgejo"},
		Subsystems: []SubsystemStatus{
			{Name: "forgejo", State: StateDown, Message: "connection refused"},
		},
		GeneratedAt: time.Now(),
	}

	output := FormatDashboard(report)
	assert.Contains(t, output, "❌ down")
	assert.Contains(t, output, "CRITICAL FAILURES")
	assert.Contains(t, output, "forgejo")
}

func TestFormatDashboard_WithMetrics(t *testing.T) {
	report := &DashboardReport{
		Overall: StateDegraded,
		Subsystems: []SubsystemStatus{
			{
				Name:    "trust",
				State:   StateDegraded,
				Message: "ledger slow",
				Metrics: map[string]string{
					"agents": "42",
					"score":  "0.73",
				},
			},
		},
		GeneratedAt: time.Now(),
	}

	output := FormatDashboard(report)
	assert.Contains(t, output, "agents: 42")
	assert.Contains(t, output, "score: 0.73")
}

func TestFormatDashboard_NilReport(t *testing.T) {
	assert.Equal(t, "No health data available.", FormatDashboard(nil))
}

// ---------------------------------------------------------------------------
// ServiceHealthAdapter tests
// ---------------------------------------------------------------------------

func TestServiceHealthAdapter_Healthy(t *testing.T) {
	adapter := &ServiceHealthAdapter{
		Name:     "test-svc",
		Critical: true,
		Check: func(ctx context.Context) (bool, string, error) {
			return true, "ok", nil
		},
	}

	status := adapter.HealthCheck(context.Background())
	assert.Equal(t, "test-svc", status.Name)
	assert.Equal(t, StateHealthy, status.State)
	assert.Equal(t, "ok", status.Message)
}

func TestServiceHealthAdapter_Unhealthy(t *testing.T) {
	adapter := &ServiceHealthAdapter{
		Name:     "test-svc",
		Critical: true,
		Check: func(ctx context.Context) (bool, string, error) {
			return false, "service unavailable", nil
		},
	}

	status := adapter.HealthCheck(context.Background())
	assert.Equal(t, StateDown, status.State)
	assert.Equal(t, "service unavailable", status.Message)
}

func TestServiceHealthAdapter_Error(t *testing.T) {
	adapter := &ServiceHealthAdapter{
		Name:     "test-svc",
		Critical: true,
		Check: func(ctx context.Context) (bool, string, error) {
			return false, "", errors.New("connection refused")
		},
	}

	status := adapter.HealthCheck(context.Background())
	assert.Equal(t, StateDown, status.State)
	assert.Contains(t, status.Message, "connection refused")
}

func TestServiceHealthAdapter_IntegrationWithAggregator(t *testing.T) {
	a := NewPlatformHealthAggregator(5 * time.Second)

	a.Register("forgejo", &ServiceHealthAdapter{
		Name: "forgejo",
		Check: func(ctx context.Context) (bool, string, error) {
			return true, "v1.21", nil
		},
	})
	a.Register("chimera", &ServiceHealthAdapter{
		Name: "chimera",
		Check: func(ctx context.Context) (bool, string, error) {
			return false, "timeout", nil
		},
	})

	report := a.Aggregate(context.Background())

	// chimera is critical and down → overall down
	assert.Equal(t, StateDown, report.Overall)
	assert.True(t, report.HasCriticalFailure())
	assert.Contains(t, report.Critical, "chimera")

	// forgejo should be healthy
	for _, s := range report.Subsystems {
		if s.Name == "forgejo" {
			assert.Equal(t, StateHealthy, s.State)
		}
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestAggregate_EmptyAggregator(t *testing.T) {
	a := NewPlatformHealthAggregator(5 * time.Second)
	report := a.Aggregate(context.Background())

	assert.Equal(t, StateHealthy, report.Overall)
	assert.True(t, report.AllHealthy())
	assert.Empty(t, report.Subsystems)
	assert.False(t, report.HasCriticalFailure())
}

func TestAggregate_ReregisterOverwrites(t *testing.T) {
	a := NewPlatformHealthAggregator(5 * time.Second)
	s1 := &stubSubsystem{name: "forgejo", state: StateHealthy}
	a.Register("forgejo", s1)

	s2 := &stubSubsystem{name: "forgejo", state: StateDown}
	a.Register("forgejo", s2)

	report := a.Aggregate(context.Background())
	assert.Equal(t, StateDown, report.Overall)
}

func TestAggregate_NameDefaultsToRegistrationName(t *testing.T) {
	a := NewPlatformHealthAggregator(5 * time.Second)
	// Register with a stub that doesn't set its name
	a.Register("my-service", &stubSubsystem{state: StateHealthy})

	report := a.Aggregate(context.Background())
	require.Len(t, report.Subsystems, 1)
	assert.Equal(t, "my-service", report.Subsystems[0].Name)
}
