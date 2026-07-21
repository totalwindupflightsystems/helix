package forgejo

import (
	"context"

	"golang.org/x/time/rate"
)

// ---------------------------------------------------------------------------
// RateLimiter
// ---------------------------------------------------------------------------

// RateLimiter controls the rate of outbound API calls.
// Implementations must be safe for concurrent use.
type RateLimiter interface {
	// Wait blocks until a token is available or the context is cancelled.
	Wait(ctx context.Context) error
}

// NoopRateLimiter is a RateLimiter that never blocks.
type NoopRateLimiter struct{}

func (NoopRateLimiter) Wait(_ context.Context) error { return nil }

// ---------------------------------------------------------------------------
// TokenBucket
// ---------------------------------------------------------------------------

// TokenBucket implements RateLimiter using golang.org/x/time/rate.
type TokenBucket struct {
	limiter *rate.Limiter // underlying token bucket
}

// NewTokenBucket creates a token-bucket limiter.
//
//	rps   — tokens per second (e.g., 10 means 10 requests/sec)
//	burst — max burst size (e.g., 20 allows short bursts up to 20)
func NewTokenBucket(rps, burst int) *TokenBucket {
	return &TokenBucket{
		limiter: rate.NewLimiter(rate.Limit(rps), burst),
	}
}

// Wait blocks until a token is available or the context is cancelled.
func (tb *TokenBucket) Wait(ctx context.Context) error {
	return tb.limiter.Wait(ctx)
}
