package retry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Config tests
// ---------------------------------------------------------------------------

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, 4, cfg.MaxAttempts)
	assert.Equal(t, 1*time.Second, cfg.InitialBackoff)
	assert.Equal(t, 30*time.Second, cfg.MaxBackoff)
	assert.True(t, cfg.Jitter)
}

// ---------------------------------------------------------------------------
// IsRetryable
// ---------------------------------------------------------------------------

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"connection refused", errors.New("dial tcp: connection refused"), true},
		{"connection reset", errors.New("read: connection reset by peer"), true},
		{"timeout", errors.New("i/o timeout"), true},
		{"EOF", errors.New("unexpected EOF"), true},
		{"no such host", errors.New("dial tcp: lookup foo: no such host"), true},
		{"HTTP 500", errors.New("HTTP 500: internal server error"), true},
		{"HTTP 502", errors.New("HTTP 502: bad gateway"), true},
		{"HTTP 503", errors.New("HTTP 503: service unavailable"), true},
		{"HTTP 429", errors.New("HTTP 429: too many requests"), true},
		{"HTTP 404", errors.New("HTTP 404: not found"), false},
		{"HTTP 400", errors.New("HTTP 400: bad request"), false},
		{"HTTP 401", errors.New("HTTP 401: unauthorized"), false},
		{"context cancelled", context.Canceled, false},
		{"context deadline", context.DeadlineExceeded, false},
		{"generic error", errors.New("something went wrong"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsRetryable(tt.err))
		})
	}
}

func TestIsHTTPRetryable(t *testing.T) {
	tests := []struct {
		code     int
		expected bool
	}{
		{200, false},
		{301, false},
		{400, false},
		{401, false},
		{403, false},
		{404, false},
		{429, true},
		{500, true},
		{502, true},
		{503, true},
		{504, true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("HTTP %d", tt.code), func(t *testing.T) {
			assert.Equal(t, tt.expected, IsHTTPRetryable(tt.code))
		})
	}
}

// ---------------------------------------------------------------------------
// WithBackoff — success cases
// ---------------------------------------------------------------------------

func TestWithBackoff_SuccessOnFirstAttempt(t *testing.T) {
	cfg := Config{
		MaxAttempts:    3,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
		Jitter:         false,
	}

	var callCount int32
	result, err := WithBackoff(context.Background(), cfg, func(ctx context.Context) (string, error) {
		atomic.AddInt32(&callCount, 1)
		return "success", nil
	})

	require.NoError(t, err)
	assert.Equal(t, "success", result)
	assert.Equal(t, int32(1), atomic.LoadInt32(&callCount))
}

func TestWithBackoff_SuccessAfterRetries(t *testing.T) {
	cfg := Config{
		MaxAttempts:    4,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
		Jitter:         false,
	}

	var callCount int32
	result, err := WithBackoff(context.Background(), cfg, func(ctx context.Context) (string, error) {
		count := atomic.AddInt32(&callCount, 1)
		if count < 3 {
			return "", errors.New("connection refused")
		}
		return "success", nil
	})

	require.NoError(t, err)
	assert.Equal(t, "success", result)
	assert.Equal(t, int32(3), atomic.LoadInt32(&callCount))
}

func TestWithBackoff_GenericType(t *testing.T) {
	cfg := Config{
		MaxAttempts:    2,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
		Jitter:         false,
	}

	type custom struct{ Value int }
	result, err := WithBackoff(context.Background(), cfg, func(ctx context.Context) (custom, error) {
		return custom{Value: 42}, nil
	})

	require.NoError(t, err)
	assert.Equal(t, 42, result.Value)
}

// ---------------------------------------------------------------------------
// WithBackoff — failure cases
// ---------------------------------------------------------------------------

func TestWithBackoff_NonRetryableError(t *testing.T) {
	cfg := Config{
		MaxAttempts:    5,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
		Jitter:         false,
	}

	var callCount int32
	_, err := WithBackoff(context.Background(), cfg, func(ctx context.Context) (string, error) {
		atomic.AddInt32(&callCount, 1)
		return "", errors.New("HTTP 404: not found")
	})

	require.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&callCount), "should not retry non-retryable error")
	assert.Contains(t, err.Error(), "404")
}

func TestWithBackoff_MaxAttemptsExceeded(t *testing.T) {
	cfg := Config{
		MaxAttempts:    3,
		InitialBackoff: 5 * time.Millisecond,
		MaxBackoff:     20 * time.Millisecond,
		Jitter:         false,
	}

	var callCount int32
	_, err := WithBackoff(context.Background(), cfg, func(ctx context.Context) (string, error) {
		atomic.AddInt32(&callCount, 1)
		return "", errors.New("connection refused")
	})

	require.Error(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&callCount))
	assert.True(t, errors.Is(err, ErrMaxAttemptsExceeded))
}

func TestWithBackoff_ContextCancelled(t *testing.T) {
	cfg := Config{
		MaxAttempts:    5,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
		Jitter:         false,
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	var callCount int32
	_, err := WithBackoff(ctx, cfg, func(ctx context.Context) (string, error) {
		atomic.AddInt32(&callCount, 1)
		return "", errors.New("connection refused")
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestWithBackoff_ContextDeadlineDuringBackoff(t *testing.T) {
	cfg := Config{
		MaxAttempts:    10,
		InitialBackoff: 500 * time.Millisecond,
		MaxBackoff:     2 * time.Second,
		Jitter:         false,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	var callCount int32
	_, err := WithBackoff(ctx, cfg, func(ctx context.Context) (string, error) {
		atomic.AddInt32(&callCount, 1)
		return "", errors.New("connection refused")
	})

	require.Error(t, err)
	// Should stop early because context deadline hits during backoff wait
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

// ---------------------------------------------------------------------------
// WithBackoff — config edge cases
// ---------------------------------------------------------------------------

func TestWithBackoff_ZeroAttempts(t *testing.T) {
	cfg := Config{MaxAttempts: 0}

	var callCount int32
	_, err := WithBackoff(context.Background(), cfg, func(ctx context.Context) (string, error) {
		atomic.AddInt32(&callCount, 1)
		return "", nil
	})

	// Should still work with at least 1 attempt
	assert.Equal(t, int32(1), atomic.LoadInt32(&callCount))
	assert.NoError(t, err)
}

func TestWithBackoff_SingleAttempt(t *testing.T) {
	cfg := Config{
		MaxAttempts:    1,
		InitialBackoff: 1 * time.Millisecond,
		Jitter:         false,
	}

	var callCount int32
	_, err := WithBackoff(context.Background(), cfg, func(ctx context.Context) (string, error) {
		atomic.AddInt32(&callCount, 1)
		return "", errors.New("connection refused")
	})

	require.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&callCount))
}

// ---------------------------------------------------------------------------
// WithBackoff — jitter
// ---------------------------------------------------------------------------

func TestWithBackoff_WithJitter(t *testing.T) {
	cfg := Config{
		MaxAttempts:    3,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
		Jitter:         true,
	}

	start := time.Now()
	_, err := WithBackoff(context.Background(), cfg, func(ctx context.Context) (string, error) {
		return "", errors.New("connection refused")
	})
	elapsed := time.Since(start)

	require.Error(t, err)
	// With jitter, should still take roughly the same time as without
	// Two backoffs: 10ms + jitter and 20ms + jitter → at least 10ms total
	assert.Greater(t, elapsed, 10*time.Millisecond)
}

// ---------------------------------------------------------------------------
// DoHTTP
// ---------------------------------------------------------------------------

func TestDoHTTP_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := Config{
		MaxAttempts:    3,
		InitialBackoff: 5 * time.Millisecond,
		Jitter:         false,
	}

	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := DoHTTP(context.Background(), cfg, http.DefaultClient, req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}

func TestDoHTTP_RetriesOn500(t *testing.T) {
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := Config{
		MaxAttempts:    4,
		InitialBackoff: 5 * time.Millisecond,
		Jitter:         false,
	}

	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := DoHTTP(context.Background(), cfg, http.DefaultClient, req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(3), atomic.LoadInt32(&callCount))
	resp.Body.Close()
}

func TestDoHTTP_DoesNotRetryOn404(t *testing.T) {
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := Config{
		MaxAttempts:    5,
		InitialBackoff: 5 * time.Millisecond,
		Jitter:         false,
	}

	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := DoHTTP(context.Background(), cfg, http.DefaultClient, req)
	// 404 is returned as-is (not retried)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, int32(1), atomic.LoadInt32(&callCount))
	resp.Body.Close()
}

func TestDoHTTP_ExhaustsOn500(t *testing.T) {
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cfg := Config{
		MaxAttempts:    3,
		InitialBackoff: 5 * time.Millisecond,
		Jitter:         false,
	}

	req, _ := http.NewRequest("GET", srv.URL, nil)
	_, err := DoHTTP(context.Background(), cfg, http.DefaultClient, req)
	require.Error(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&callCount))
}

func TestDoHTTP_RetriesOn429(t *testing.T) {
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		if count < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := Config{
		MaxAttempts:    3,
		InitialBackoff: 5 * time.Millisecond,
		Jitter:         false,
	}

	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := DoHTTP(context.Background(), cfg, http.DefaultClient, req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(2), atomic.LoadInt32(&callCount))
	resp.Body.Close()
}

// ---------------------------------------------------------------------------
// httpError
// ---------------------------------------------------------------------------

func TestHTTPError(t *testing.T) {
	err := &httpError{StatusCode: http.StatusInternalServerError}
	assert.Equal(t, "HTTP 500: Internal Server Error", err.Error())
}

func TestHTTPError_NotFound(t *testing.T) {
	err := &httpError{StatusCode: http.StatusNotFound}
	assert.Equal(t, "HTTP 404: Not Found", err.Error())
}
