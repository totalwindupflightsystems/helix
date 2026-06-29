// Package retry provides exponential backoff retry logic for cross-service calls.
// Used by Forgejo client, Chimera adapter, and all HTTP-dependent components.
//
// Based on specs/cross-component-wiring.md §7 (Error Propagation).
package retry

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

// ErrMaxAttemptsExceeded is returned when all retry attempts are exhausted.
var ErrMaxAttemptsExceeded = errors.New("max retry attempts exceeded")

// RetryableFunc is a function that can be retried. It should return the result
// and an error. If the error is retryable (per IsRetryable), the function will
// be called again after a backoff delay.
type RetryableFunc[T any] func(ctx context.Context) (T, error)

// Config controls retry behavior.
type Config struct {
	MaxAttempts    int           // Total attempts including the first (default 4)
	InitialBackoff time.Duration // Backoff for first retry (default 1s)
	MaxBackoff     time.Duration // Cap on backoff growth (default 30s)
	Jitter         bool          // Add random jitter to prevent thundering herd (default true)
}

// DefaultConfig returns sensible defaults for cross-service calls.
func DefaultConfig() Config {
	return Config{
		MaxAttempts:    4,
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     30 * time.Second,
		Jitter:         true,
	}
}

// WithBackoff retries the given function with exponential backoff.
// The function is retried if it returns an error that IsRetryable returns true for.
// On context cancellation, returns ctx.Err() immediately.
func WithBackoff[T any](ctx context.Context, cfg Config, fn RetryableFunc[T]) (T, error) {
	var zero T

	if cfg.MaxAttempts < 1 {
		cfg.MaxAttempts = 1
	}
	if cfg.InitialBackoff <= 0 {
		cfg.InitialBackoff = 1 * time.Second
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 30 * time.Second
	}

	backoff := cfg.InitialBackoff
	var lastErr error

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		// Check context before each attempt
		if err := ctx.Err(); err != nil {
			return zero, err
		}

		result, err := fn(ctx)
		if err == nil {
			return result, nil
		}

		lastErr = err

		// Don't retry if not retryable
		if !IsRetryable(err) {
			return zero, err
		}

		// Don't wait after the last attempt
		if attempt == cfg.MaxAttempts {
			break
		}

		// Wait with backoff
		waitDuration := backoff
		if cfg.Jitter {
			// Add up to 50% jitter
			jitter := time.Duration(rand.Int63n(int64(backoff) / 2))
			waitDuration += jitter
		}

		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(waitDuration):
		}

		// Exponential growth
		backoff *= 2
		if backoff > cfg.MaxBackoff {
			backoff = cfg.MaxBackoff
		}
	}

	return zero, errors.Join(ErrMaxAttemptsExceeded, lastErr)
}

// IsRetryable determines if an error warrants a retry.
// Retryable: connection refused, timeout, 5xx HTTP errors.
// Not retryable: 4xx HTTP errors (except 429), context cancelled.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Context cancellation is never retryable
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	errStr := err.Error()

	// Network-level retryable errors
	retryablePatterns := []string{
		"connection refused",
		"connection reset",
		"timeout",
		"EOF",
		"no such host",
		"i/o timeout",
		"network is unreachable",
		"broken pipe",
	}

	for _, pattern := range retryablePatterns {
		if strings.Contains(strings.ToLower(errStr), strings.ToLower(pattern)) {
			return true
		}
	}

	// HTTP status code patterns
	if strings.Contains(errStr, "HTTP 5") {
		return true
	}

	// 429 Too Many Requests — retryable with backoff
	if strings.Contains(errStr, "HTTP 429") {
		return true
	}

	return false
}

// IsHTTPRetryable checks if an HTTP status code is retryable.
func IsHTTPRetryable(statusCode int) bool {
	if statusCode >= 500 && statusCode < 600 {
		return true
	}
	if statusCode == 429 {
		return true
	}
	return false
}

// DoHTTP is a convenience function for retrying HTTP requests.
// It retries on 5xx and 429 responses.
func DoHTTP(ctx context.Context, cfg Config, client *http.Client, req *http.Request) (*http.Response, error) {
	return WithBackoff(ctx, cfg, func(ctx context.Context) (*http.Response, error) {
		// Clone the request to allow body re-reading on retry
		clonedReq := req.Clone(ctx)
		resp, err := client.Do(clonedReq)
		if err != nil {
			return nil, err
		}

		if IsHTTPRetryable(resp.StatusCode) {
			resp.Body.Close()
			return nil, &httpError{StatusCode: resp.StatusCode}
		}

		return resp, nil
	})
}

// httpError wraps an HTTP status code as an error for retry detection.
type httpError struct {
	StatusCode int
}

func (e *httpError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, http.StatusText(e.StatusCode))
}
