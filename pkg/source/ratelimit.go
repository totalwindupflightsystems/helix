// Package source provides configuration parsing, tool generation, capability
// gating, and per-source rate limiting for multi-source integrations in
// Helix (SPEC-025).
//
// This file implements the per-source rate limiter (SRC-004, SPEC-025 §6):
// parsing rate_limit values from .helix/sources.yaml ("N/s" and "N/m") and
// enforcing them with one token bucket per source. See ParseRateLimit,
// SourceRateLimiter, and RateLimitManager.
package source

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"time"

	"golang.org/x/time/rate"
)

// ---------------------------------------------------------------------------
// RateLimitSpec and parsing
// ---------------------------------------------------------------------------

// rateLimitPattern matches the supported rate_limit formats: "N/s" (requests
// per second) and "N/m" (requests per minute), where N is a positive integer.
var rateLimitPattern = regexp.MustCompile(`^(\d+)/([sm])$`)

// RateLimitSpec is a parsed rate_limit value from .helix/sources.yaml.
//
// The zero value (Requests == 0, Period == 0) means "no limit": the source
// is never throttled.
type RateLimitSpec struct {
	// Requests is the number of requests permitted per Period.
	Requests int

	// Period is the window the requests are spread over: time.Second for
	// "N/s" and time.Minute for "N/m".
	Period time.Duration
}

// Limited reports whether the spec imposes a rate limit.
func (s RateLimitSpec) Limited() bool {
	return s.Requests > 0 && s.Period > 0
}

// PerSecond returns the configured rate in requests per second. A zero spec
// (no limit) yields 0.
func (s RateLimitSpec) PerSecond() float64 {
	if !s.Limited() {
		return 0
	}
	return float64(s.Requests) / s.Period.Seconds()
}

// ParseRateLimit parses a rate_limit value in the "N/s" (requests per
// second) or "N/m" (requests per minute) format, where N is a positive
// integer.
//
// An empty string parses to the zero RateLimitSpec (no limit). Any other
// invalid format returns a descriptive error so that misconfiguration is
// surfaced rather than silently ignored.
func ParseRateLimit(spec string) (RateLimitSpec, error) {
	if spec == "" {
		return RateLimitSpec{}, nil
	}
	m := rateLimitPattern.FindStringSubmatch(spec)
	if m == nil {
		return RateLimitSpec{}, fmt.Errorf("invalid rate_limit %q: want \"N/s\" (per second) or \"N/m\" (per minute), where N is a positive integer", spec)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return RateLimitSpec{}, fmt.Errorf("invalid rate_limit %q: N must be a positive integer", spec)
	}
	period := time.Second
	if m[2] == "m" {
		period = time.Minute
	}
	return RateLimitSpec{Requests: n, Period: period}, nil
}

// ---------------------------------------------------------------------------
// SourceRateLimiter
// ---------------------------------------------------------------------------

// SourceRateLimiter controls the rate of outbound calls to a single source.
// Implementations must be safe for concurrent use.
type SourceRateLimiter interface {
	// Wait blocks until a token is available or the context is cancelled.
	Wait(ctx context.Context) error
}

// NoopSourceLimiter is a SourceRateLimiter that never blocks. It is used for
// sources that declare no rate_limit.
type NoopSourceLimiter struct{}

// Wait implements SourceRateLimiter.
func (NoopSourceLimiter) Wait(_ context.Context) error { return nil }

// TokenBucketLimiter implements SourceRateLimiter using
// golang.org/x/time/rate, mirroring pkg/forgejo.TokenBucket (SPEC-025 §6).
type TokenBucketLimiter struct {
	limiter *rate.Limiter // underlying token bucket
}

// NewTokenBucketLimiter creates a token-bucket limiter.
//
//	requests — tokens granted per Period (must be >= 1)
//	burst    — max burst size (must be >= 1)
//	period   — window the requests are spread over (e.g. time.Second,
//	           time.Minute)
func NewTokenBucketLimiter(requests, burst int, period time.Duration) *TokenBucketLimiter {
	return &TokenBucketLimiter{
		limiter: rate.NewLimiter(rate.Limit(float64(requests)/period.Seconds()), burst),
	}
}

// Wait blocks until a token is available or the context is cancelled.
func (tb *TokenBucketLimiter) Wait(ctx context.Context) error {
	return tb.limiter.Wait(ctx)
}

// ---------------------------------------------------------------------------
// RateLimitManager
// ---------------------------------------------------------------------------

// RateLimitManager builds and serves one SourceRateLimiter per source name
// (SPEC-025 §6). The limiter map is constructed once and is read-only
// afterwards, so the manager is safe for concurrent Wait calls.
type RateLimitManager struct {
	// limiters maps source names (as used in .helix/sources.yaml) to their
	// rate limiter.
	limiters map[string]SourceRateLimiter
}

// NewRateLimitManager builds a manager from source configuration, parsing
// each source's RateLimit field. Sources with an empty rate_limit get a
// NoopSourceLimiter; an invalid rate_limit is an error naming the offending
// source, so misconfiguration is surfaced at construction time.
//
// Burst sizing: each limiter's burst is the configured window's request
// count N (e.g. burst 10 for "10/s", burst 5 for "5/m"). A full window's
// worth of calls may be issued immediately after an idle period, while
// steady-state throughput stays at N per window. The bucket is deliberately
// not double-sized for the per-minute form: the configured limit is the
// limit.
func NewRateLimitManager(sources map[string]Source) (*RateLimitManager, error) {
	m := &RateLimitManager{limiters: make(map[string]SourceRateLimiter, len(sources))}

	// Iterate in sorted order so a config with several invalid rate limits
	// always fails on the same source first (matches ParseSourcesYAML).
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		spec, err := ParseRateLimit(sources[name].RateLimit)
		if err != nil {
			return nil, fmt.Errorf("source %q: %w", name, err)
		}
		m.limiters[name] = newLimiter(spec)
	}
	return m, nil
}

// newLimiter builds the limiter for a parsed spec: a no-op for "no limit",
// a token bucket otherwise. Burst equals the window's request count — see
// NewRateLimitManager for the rationale.
func newLimiter(spec RateLimitSpec) SourceRateLimiter {
	if !spec.Limited() {
		return NoopSourceLimiter{}
	}
	return NewTokenBucketLimiter(spec.Requests, spec.Requests, spec.Period)
}

// Wait blocks until a token is available for the named source or ctx is
// cancelled.
//
// An unknown source name is an error so callers catch misconfiguration (a
// typo, or a source dropped from .helix/sources.yaml). A source without a
// rate_limit never blocks: Wait returns nil immediately.
func (m *RateLimitManager) Wait(ctx context.Context, sourceName string) error {
	limiter, ok := m.limiters[sourceName]
	if !ok {
		return fmt.Errorf("source %q: unknown source", sourceName)
	}
	return limiter.Wait(ctx)
}
