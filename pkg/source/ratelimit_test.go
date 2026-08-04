package source

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ParseRateLimit
// ---------------------------------------------------------------------------

func TestParseRateLimit(t *testing.T) {
	tests := []struct {
		name         string
		spec         string
		wantRequests int
		wantPeriod   time.Duration
		wantLimited  bool
		wantErr      bool
	}{
		{name: "per second", spec: "10/s", wantRequests: 10, wantPeriod: time.Second, wantLimited: true},
		{name: "per minute", spec: "5/m", wantRequests: 5, wantPeriod: time.Minute, wantLimited: true},
		{name: "single", spec: "1/s", wantRequests: 1, wantPeriod: time.Second, wantLimited: true},
		{name: "empty means no limit", spec: "", wantRequests: 0, wantPeriod: 0, wantLimited: false},
		{name: "garbage", spec: "abc", wantErr: true},
		{name: "missing unit", spec: "10", wantErr: true},
		{name: "double unit", spec: "10/s/s", wantErr: true},
		{name: "negative", spec: "-1/s", wantErr: true},
		{name: "zero per second", spec: "0/s", wantErr: true},
		{name: "zero per minute", spec: "0/m", wantErr: true},
		{name: "uppercase unit", spec: "10/S", wantErr: true},
		{name: "unknown unit", spec: "10/h", wantErr: true},
		{name: "fractional", spec: "10.5/s", wantErr: true},
		{name: "leading whitespace", spec: " 10/s", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRateLimit(tt.spec)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "rate_limit")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantRequests, got.Requests)
			assert.Equal(t, tt.wantPeriod, got.Period)
			assert.Equal(t, tt.wantLimited, got.Limited())
		})
	}
}

func TestRateLimitSpec_PerSecond(t *testing.T) {
	tests := []struct {
		name string
		spec string
		want float64
	}{
		{name: "per second", spec: "10/s", want: 10},
		{name: "per minute", spec: "5/m", want: 5.0 / 60.0},
		{name: "no limit", spec: "", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := ParseRateLimit(tt.spec)
			require.NoError(t, err)
			assert.InDelta(t, tt.want, spec.PerSecond(), 1e-9)
		})
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// limitedManager builds a manager whose sources carry the given rate limits
// (map of source name to rate_limit value).
func limitedManager(t *testing.T, limits map[string]string) *RateLimitManager {
	t.Helper()
	sources := make(map[string]Source, len(limits))
	for name, limit := range limits {
		sources[name] = Source{Name: name, RateLimit: limit}
	}
	m, err := NewRateLimitManager(sources)
	require.NoError(t, err)
	return m
}

// ---------------------------------------------------------------------------
// RateLimitManager
// ---------------------------------------------------------------------------

func TestRateLimitManager_NoLimitSourceNeverBlocks(t *testing.T) {
	m := limitedManager(t, map[string]string{"fs": ""})

	start := time.Now()
	err := m.Wait(context.Background(), "fs")
	assert.NoError(t, err)
	assert.Less(t, time.Since(start), 50*time.Millisecond)
}

func TestRateLimitManager_UnknownSource(t *testing.T) {
	m := limitedManager(t, map[string]string{"db": "10/s"})

	err := m.Wait(context.Background(), "nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"nope"`)

	// A manager built from a nil map has no known sources at all.
	empty, err := NewRateLimitManager(nil)
	require.NoError(t, err)
	assert.Error(t, empty.Wait(context.Background(), "db"))
}

func TestRateLimitManager_InvalidRateLimitSurfacesAtConstruction(t *testing.T) {
	sources := map[string]Source{
		"db": {Name: "db", RateLimit: "abc"},
	}
	_, err := NewRateLimitManager(sources)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db")
	assert.Contains(t, err.Error(), "abc")
}

func TestRateLimitManager_BlocksLimitedSource(t *testing.T) {
	// 1/s with burst 1: the first Wait consumes the only token; the second
	// must wait for the bucket to refill (~1s).
	m := limitedManager(t, map[string]string{"db": "1/s"})
	require.NoError(t, m.Wait(context.Background(), "db"))

	start := time.Now()
	require.NoError(t, m.Wait(context.Background(), "db"))
	elapsed := time.Since(start)
	assert.GreaterOrEqual(t, elapsed, 900*time.Millisecond)
	assert.Less(t, elapsed, 3*time.Second)
}

func TestRateLimitManager_BurstMatchesWindowSize(t *testing.T) {
	// 10/s with burst 10: a full window's worth of calls passes immediately,
	// the eleventh must wait for a refill (~100ms).
	m := limitedManager(t, map[string]string{"db": "10/s"})

	start := time.Now()
	for i := 0; i < 10; i++ {
		require.NoError(t, m.Wait(context.Background(), "db"))
	}
	assert.Less(t, time.Since(start), 500*time.Millisecond)

	start = time.Now()
	require.NoError(t, m.Wait(context.Background(), "db"))
	assert.GreaterOrEqual(t, time.Since(start), 80*time.Millisecond)
}

func TestRateLimitManager_PreCancelledContext(t *testing.T) {
	m := limitedManager(t, map[string]string{"db": "1/s"})
	require.NoError(t, m.Wait(context.Background(), "db")) // consume the token

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	err := m.Wait(ctx, "db")
	assert.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(start), 100*time.Millisecond)
}

func TestRateLimitManager_WaitReturnsOnContextCancel(t *testing.T) {
	m := limitedManager(t, map[string]string{"db": "1/s"})
	require.NoError(t, m.Wait(context.Background(), "db")) // consume the token

	ctx, cancel := context.WithCancel(context.Background())
	start := time.Now()
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	err := m.Wait(ctx, "db")
	assert.ErrorIs(t, err, context.Canceled)
	elapsed := time.Since(start)
	assert.GreaterOrEqual(t, elapsed, 80*time.Millisecond)
	assert.Less(t, elapsed, 500*time.Millisecond)
}

func TestRateLimitManager_ShortDeadline(t *testing.T) {
	m := limitedManager(t, map[string]string{"db": "1/s"})
	require.NoError(t, m.Wait(context.Background(), "db")) // consume the token

	// The token needs ~1s but the context allows 100ms: Wait must fail
	// promptly (x/time/rate reports the missed deadline as a rate error).
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := m.Wait(ctx, "db")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deadline")
	assert.Less(t, time.Since(start), 200*time.Millisecond)
}

func TestRateLimitManager_ConcurrentUse(t *testing.T) {
	// Concurrent Wait calls across a limited and an unlimited source: the
	// manager must be safe for concurrent use (exercised under -race) and
	// never error on known sources.
	m := limitedManager(t, map[string]string{"api": "100/s", "fs": ""})

	const goroutines = 8
	const iterations = 20
	errs := make(chan error, goroutines*iterations*2)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				errs <- m.Wait(context.Background(), "api")
				errs <- m.Wait(context.Background(), "fs")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		assert.NoError(t, err)
	}
}

// ---------------------------------------------------------------------------
// TokenBucketLimiter
// ---------------------------------------------------------------------------

func TestTokenBucketLimiter_ConcurrentWait(t *testing.T) {
	// 8 goroutines against a 100/s bucket with burst 1: the first token is
	// immediate and the rest are spaced ~10ms apart, so the whole batch must
	// take at least ~70ms and every Wait must succeed.
	limiter := NewTokenBucketLimiter(100, 1, time.Second)

	const goroutines = 8
	errs := make(chan error, goroutines)
	var wg sync.WaitGroup
	start := time.Now()
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- limiter.Wait(context.Background())
		}()
	}
	wg.Wait()
	close(errs)
	elapsed := time.Since(start)

	for err := range errs {
		assert.NoError(t, err)
	}
	// Seven tokens must be refilled at ~10ms each: the floor is ~70ms.
	assert.GreaterOrEqual(t, elapsed, 50*time.Millisecond)
	assert.Less(t, elapsed, 3*time.Second)
}
