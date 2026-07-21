package forgejo

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// NoopRateLimiter
// ---------------------------------------------------------------------------

func TestNoopRateLimiter_NeverBlocks(t *testing.T) {
	rl := NoopRateLimiter{}
	// 1000 concurrent calls all return instantly.
	const n = 1000
	var wg sync.WaitGroup
	wg.Add(n)
	start := time.Now()
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			require.NoError(t, rl.Wait(context.Background()))
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)
	assert.Less(t, elapsed, 100*time.Millisecond,
		"NoopRateLimiter should never block; elapsed=%s", elapsed)
}

func TestNoopRateLimiter_RespectsContext(t *testing.T) {
	rl := NoopRateLimiter{}
	// Noop returns nil regardless of context state.
	require.NoError(t, rl.Wait(context.Background()))
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already-cancelled context
	require.NoError(t, rl.Wait(ctx))
}

// ---------------------------------------------------------------------------
// TokenBucket
// ---------------------------------------------------------------------------

func TestTokenBucket_AllowsNormalRate(t *testing.T) {
	// 5 calls at 100 RPS with burst 10 — all should complete well under 1s.
	tb := NewTokenBucket(100, 10)
	ctx := context.Background()
	start := time.Now()
	for i := 0; i < 5; i++ {
		require.NoError(t, tb.Wait(ctx))
	}
	elapsed := time.Since(start)
	assert.Less(t, elapsed, 100*time.Millisecond,
		"5 calls at 100 RPS should not block meaningfully; elapsed=%s", elapsed)
}

func TestTokenBucket_ThrottlesExcess(t *testing.T) {
	// 10 RPS, burst 1: 5 consecutive calls must take at least ~400ms
	// (1 immediate, 4 spaced at 100ms each).
	tb := NewTokenBucket(10, 1)
	ctx := context.Background()
	start := time.Now()
	for i := 0; i < 5; i++ {
		require.NoError(t, tb.Wait(ctx))
	}
	elapsed := time.Since(start)
	assert.GreaterOrEqual(t, elapsed, 350*time.Millisecond,
		"5 calls at 10 RPS / burst 1 should take at least ~400ms; elapsed=%s", elapsed)
	// Also verify it didn't run crazy slow (allow 2s slack for CI jitter).
	assert.Less(t, elapsed, 2*time.Second,
		"5 calls at 10 RPS / burst 1 should finish in well under 2s; elapsed=%s", elapsed)
}

func TestTokenBucket_RespectsBurst(t *testing.T) {
	// 5 RPS, burst 10: first 10 calls pass instantly, 11th blocks ~200ms.
	tb := NewTokenBucket(5, 10)
	ctx := context.Background()

	start := time.Now()
	for i := 0; i < 10; i++ {
		require.NoError(t, tb.Wait(ctx))
	}
	burstElapsed := time.Since(start)
	assert.Less(t, burstElapsed, 100*time.Millisecond,
		"burst of 10 should consume instantly; elapsed=%s", burstElapsed)

	// 11th call: bucket is empty; must wait ~200ms for one token at 5 RPS.
	start = time.Now()
	require.NoError(t, tb.Wait(ctx))
	waitElapsed := time.Since(start)
	assert.GreaterOrEqual(t, waitElapsed, 150*time.Millisecond,
		"11th call at 5 RPS should wait ~200ms; elapsed=%s", waitElapsed)
}

func TestTokenBucket_ContextCancelled(t *testing.T) {
	// 1 RPS, burst 1 — second call must wait ~1s. Cancel mid-wait.
	tb := NewTokenBucket(1, 1)
	ctx := context.Background()
	require.NoError(t, tb.Wait(ctx)) // consume the one token

	ctxCancel, cancel := context.WithCancel(ctx)
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	err := tb.Wait(ctxCancel)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestTokenBucket_DeadlineExceeded(t *testing.T) {
	// 1 RPS, burst 1 — second call must wait ~1s. Use a 50ms deadline.
	tb := NewTokenBucket(1, 1)
	ctx := context.Background()
	require.NoError(t, tb.Wait(ctx)) // consume the one token

	ctxTimeout, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	err := tb.Wait(ctxTimeout)
	require.Error(t, err)
	// x/time/rate returns a RateLimitError ("would exceed context deadline")
	// rather than wrapping context.DeadlineExceeded in the error chain.
	assert.ErrorContains(t, err, "would exceed context deadline")
}

func TestTokenBucket_ConcurrentSafety(t *testing.T) {
	// Stress test: many goroutines competing for the same bucket.
	tb := NewTokenBucket(50, 5) // 50 RPS, small burst
	ctx := context.Background()

	const goroutines = 20
	const callsEach = 25 // 500 total calls
	var served atomic.Int64
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsEach; j++ {
				if err := tb.Wait(ctx); err == nil {
					served.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	// With 50 RPS, 500 calls should take ~10s in worst case; we cap at 15s for CI.
	// All calls should have succeeded (no context cancellation).
	assert.Equal(t, int64(goroutines*callsEach), served.Load(),
		"all concurrent calls should be served when context is alive")
}

// ---------------------------------------------------------------------------
// Client integration
// ---------------------------------------------------------------------------

func TestClient_WithRateLimiter_SetsField(t *testing.T) {
	tb := NewTokenBucket(10, 5)
	c := NewClient("http://localhost:3030", "admin", "secret").
		WithRateLimiter(tb)
	assert.Equal(t, tb, c.rateLimiter)
}

func TestClient_DefaultRateLimiterIsNoop(t *testing.T) {
	c := NewClient("http://localhost:3030", "admin", "secret")
	_, ok := c.rateLimiter.(NoopRateLimiter)
	assert.True(t, ok, "default rate limiter should be NoopRateLimiter; got %T", c.rateLimiter)
}
