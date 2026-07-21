# Helix — Forgejo API Rate Limiting Specification

**Spec version:** 1.0
**Status:** Draft
**Last updated:** 2026-07-21
**Implements:** PROD-002
**Depends on:** forgejo client (`pkg/forgejo/client.go`), config spec (`helix-config.md`)

---

## 1. Overview

Helix's Forgejo REST API client (`pkg/forgejo/client.go`) currently has no rate-limiting logic. When Helix components (identity provisioning, marketplace polling, PR negotiation) make concurrent API calls, they can trigger Forgejo's HTTP 429 rate limits. This spec adds rate-limiting middleware to the shared HTTP client so that all callers respect a configurable per-second budget.

**Key goals:**
- Prevent 429 responses by proactively throttling outbound API calls
- Provide a token-bucket rate limiter with configurable rate and burst
- Integrate with the existing `Client` via a `WithRateLimiter` option (parallel to `WithCircuitBreaker`)
- Support per-component rate limits via distinct `Client` instances
- Log rate-limit state changes for observability

**Non-goals:**
- Global / cross-client rate limiting (each client instance has its own limiter)
- Distributed rate limiting (no Redis/shared state)
- Adaptive rate limiting based on 429 responses (future enhancement)

---

## 2. Dependencies

| Dependency | Version | Purpose |
|------------|---------|---------|
| Go standard library (`golang.org/x/time/rate`) | latest | Token-bucket rate limiter (golang.org/x/time/rate) |
| `pkg/forgejo` | existing | HTTP client to extend |
| `pkg/log` | existing | Structured logging for rate-limit state |

**Decision: `golang.org/x/time/rate` over custom implementation**
- Well-tested, zero-dependency token-bucket implementation from the Go team
- Already a common dependency in Go projects (likely already in the dependency tree)
- Supports configurable rate (tokens/sec) and burst (max accumulated tokens)
- Avoids reinventing the wheel; the Forgejo client just needs a thin wrapper

---

## 3. Interface

```go
// Package forgejo — RateLimiter interface and implementations.

// RateLimiter controls the rate of outbound API calls.
// Implementations must be safe for concurrent use.
type RateLimiter interface {
    // Wait blocks until a token is available or the context is cancelled.
    Wait(ctx context.Context) error
}

// TokenBucket implements RateLimiter using golang.org/x/time/rate.
type TokenBucket struct {
    limiter *rate.Limiter
}

// NewTokenBucket creates a token-bucket limiter.
//   rate  — tokens per second (e.g., 10 means 10 requests/sec)
//   burst — max burst size (e.g., 20 allows short bursts up to 20)
func NewTokenBucket(rps, burst int) *TokenBucket {
    return &TokenBucket{
        limiter: rate.NewLimiter(rate.Limit(rps), burst),
    }
}

func (tb *TokenBucket) Wait(ctx context.Context) error {
    return tb.limiter.Wait(ctx)
}

// NoopRateLimiter is a RateLimiter that never blocks.
type NoopRateLimiter struct{}

func (NoopRateLimiter) Wait(ctx context.Context) error { return nil }
```

### Client Changes

```go
// New fields on Client:
type Client struct {
    // ... existing fields ...
    rateLimiter RateLimiter
}

// NewClient uses NoopRateLimiter by default.
func NewClient(baseURL, username, password string) *Client {
    return &Client{
        // ... existing fields ...
        rateLimiter: NoopRateLimiter{},
    }
}

// WithRateLimiter attaches a rate limiter.
func (c *Client) WithRateLimiter(rl RateLimiter) *Client {
    c.rateLimiter = rl
    return c
}
```

### doRequest Changes

```go
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}, result interface{}) error {
    if !c.circuit.Allow() {
        return ErrCircuitOpen
    }

    // Rate-limit gate: wait for a token before sending.
    if err := c.rateLimiter.Wait(ctx); err != nil {
        return fmt.Errorf("rate limit: %w", err)
    }

    // ... rest of existing doRequest ...
}
```

---

## 4. Behavior

### Normal operation
1. Caller invokes `client.GetUser(ctx, "alice")`
2. `doRequest` calls `rateLimiter.Wait(ctx)` — blocks if no tokens available
3. On token acquisition, sends the HTTP request normally
4. Circuit breaker records success/failure as before

### Rate-limit exceeded
1. All tokens consumed (burst exhausted)
2. `rateLimiter.Wait(ctx)` blocks until a token refills (at the configured rate)
3. If context is cancelled while waiting, returns context error immediately
4. No 429 is ever sent to Forgejo — the client self-throttles

### Context cancellation
- If the caller's context is cancelled during `Wait`, the error propagates
- Callers must handle `context.Canceled` / `context.DeadlineExceeded` 

---

## 5. Data Model

No persistent storage needed. Rate limiter state is in-memory:

```go
type TokenBucket struct {
    limiter *rate.Limiter  // in-memory token bucket
}
```

Configuration (future, via config loader):

```yaml
# ~/.helix/config.yaml (future)
forgejo:
  rate_limit:
    rps: 10       # requests per second
    burst: 20     # max burst
```

---

## 6. States

| State | Condition | Behavior |
|-------|-----------|----------|
| Normal | Tokens available | Request proceeds immediately |
| Throttled | No tokens, burst consumed | `Wait` blocks until next token |
| Context cancelled | Context cancelled during Wait | Returns context error |
| Noop | No RateLimiter set (default) | No rate limiting applied |

---

## 7. Error Handling

| Error | Cause | Handling |
|-------|-------|----------|
| `context.Canceled` | Caller cancelled context during `Wait` | Propagates to caller — same as circuit-breaker open |
| `context.DeadlineExceeded` | Context timeout during `Wait` | Propagates to caller |
| No other errors | Token bucket never fails on its own | Always succeeds with sufficient wait |

---

## 8. Testing

| Test | Scenario | Verification |
|------|----------|-------------|
| TokenBucket allows normal rate | 5 calls at 100 RPS | All succeed without blocking |
| TokenBucket throttles excess | 100 calls at 10 RPS / burst 1 | Some calls wait (wall-clock time > theoretical min) |
| TokenBucket respects burst | 10 calls at 5 RPS / burst 10 | First 10 pass instantly, 11th blocks |
| Context cancellation during wait | Call with cancelled context | Returns context.Canceled immediately |
| NoopRateLimiter never blocks | 1000 concurrent calls | All return instantly |
| WithRateLimiter integration | Client configured with TokenBucket | RateLimiter.Wait called before every doRequest |
| WithRateLimiter default | Client with no rate limiter | doRequest works as before (NoopRateLimiter) |

---

## 9. Configuration

Not wired into config in this iteration. Rate limit is configured programmatically:

```go
client := forgejo.NewClient(url, user, pass).
    WithRateLimiter(forgejo.NewTokenBucket(10, 20))
```

---

## 10. Security & Performance

- **Security:** No security implications. Rate limiting is a performance safeguard, not a security boundary.
- **Performance:** Token bucket uses O(1) memory and O(1) time per operation.
- **Noise:** One `log.Debug` call per rate-limit wait >100ms for observability.

---

## Implementation Plan

1. Add `golang.org/x/time/rate` to `go.mod`
2. Implement `RateLimiter` interface + `TokenBucket` in `pkg/forgejo/rate.go`
3. Add `rateLimiter` field to `Client` struct
4. Implement `WithRateLimiter` option
5. Wire `rateLimiter.Wait()` into `doRequest` (before circuit breaker check)
6. Write tests in `pkg/forgejo/rate_test.go`
7. Update `pkg/forgejo/client_test.go` to verify NoopRateLimiter default behavior
