package health

import (
	"context"
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
// ServiceResult tests
// ---------------------------------------------------------------------------

func TestServiceResult_IsRequired(t *testing.T) {
	r := ServiceResult{Name: "forgejo", Required: true}
	assert.True(t, r.IsRequired())

	r2 := ServiceResult{Name: "langfuse", Required: false}
	assert.False(t, r2.IsRequired())
}

// ---------------------------------------------------------------------------
// HealthReport tests
// ---------------------------------------------------------------------------

func TestHealthReport_HasFailures(t *testing.T) {
	tests := []struct {
		name     string
		results  []ServiceResult
		expected bool
	}{
		{
			name:     "empty results",
			results:  []ServiceResult{},
			expected: false,
		},
		{
			name: "all healthy",
			results: []ServiceResult{
				{Name: "a", Healthy: true},
				{Name: "b", Healthy: true},
			},
			expected: false,
		},
		{
			name: "one unhealthy required",
			results: []ServiceResult{
				{Name: "a", Healthy: true},
				{Name: "b", Healthy: false, Required: true},
			},
			expected: true,
		},
		{
			name: "one unhealthy optional",
			results: []ServiceResult{
				{Name: "a", Healthy: true},
				{Name: "b", Healthy: false, Required: false},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &HealthReport{Results: tt.results}
			assert.Equal(t, tt.expected, r.HasFailures())
		})
	}
}

func TestHealthReport_HasRequiredFailures(t *testing.T) {
	tests := []struct {
		name     string
		results  []ServiceResult
		expected bool
	}{
		{
			name:     "empty results",
			results:  []ServiceResult{},
			expected: false,
		},
		{
			name: "all healthy",
			results: []ServiceResult{
				{Name: "a", Healthy: true, Required: true},
				{Name: "b", Healthy: true, Required: false},
			},
			expected: false,
		},
		{
			name: "optional failure only",
			results: []ServiceResult{
				{Name: "a", Healthy: true, Required: true},
				{Name: "b", Healthy: false, Required: false},
			},
			expected: false,
		},
		{
			name: "required failure",
			results: []ServiceResult{
				{Name: "a", Healthy: false, Required: true},
				{Name: "b", Healthy: true, Required: false},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &HealthReport{Results: tt.results}
			assert.Equal(t, tt.expected, r.HasRequiredFailures())
		})
	}
}

func TestHealthReport_Get(t *testing.T) {
	r := &HealthReport{
		Results: []ServiceResult{
			{Name: "forgejo", Healthy: true},
			{Name: "chimera", Healthy: false},
		},
	}

	found := r.Get("forgejo")
	require.NotNil(t, found)
	assert.True(t, found.Healthy)

	found = r.Get("chimera")
	require.NotNil(t, found)
	assert.False(t, found.Healthy)

	missing := r.Get("nonexistent")
	assert.Nil(t, missing)
}

// ---------------------------------------------------------------------------
// Checker tests with httptest
// ---------------------------------------------------------------------------

func TestChecker_AllHealthy(t *testing.T) {
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"version":"1.21"}`)
	}))
	defer srv1.Close()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer srv2.Close()

	checker := NewChecker([]ServiceCheck{
		{Name: "svc1", URL: srv1.URL, Timeout: 2 * time.Second, Required: true},
		{Name: "svc2", URL: srv2.URL, Timeout: 2 * time.Second, Required: true},
	})

	report := checker.Check(context.Background())

	assert.True(t, report.Healthy)
	assert.False(t, report.HasFailures())
	assert.Len(t, report.Results, 2)

	for _, r := range report.Results {
		assert.True(t, r.Healthy, "service %s should be healthy", r.Name)
		assert.NoError(t, r.Err)
		assert.Greater(t, r.Latency, time.Duration(0))
	}
}

func TestChecker_RequiredServiceDown(t *testing.T) {
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv1.Close()

	checker := NewChecker([]ServiceCheck{
		{Name: "healthy", URL: srv1.URL, Timeout: 2 * time.Second, Required: true},
		{Name: "down", URL: "http://127.0.0.1:59999", Timeout: 1 * time.Second, Required: true},
	})

	report := checker.Check(context.Background())

	assert.False(t, report.Healthy)
	assert.True(t, report.HasRequiredFailures())

	downResult := report.Get("down")
	require.NotNil(t, downResult)
	assert.False(t, downResult.Healthy)
	assert.Error(t, downResult.Err)
	assert.True(t, downResult.Required)
}

func TestChecker_OptionalServiceDown(t *testing.T) {
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv1.Close()

	checker := NewChecker([]ServiceCheck{
		{Name: "required-ok", URL: srv1.URL, Timeout: 2 * time.Second, Required: true},
		{Name: "optional-down", URL: "http://127.0.0.1:59998", Timeout: 1 * time.Second, Required: false},
	})

	report := checker.Check(context.Background())

	// Overall healthy because only optional service is down
	assert.True(t, report.Healthy)
	assert.False(t, report.HasRequiredFailures())
	assert.True(t, report.HasFailures()) // there IS a failure, just optional

	optResult := report.Get("optional-down")
	require.NotNil(t, optResult)
	assert.False(t, optResult.Healthy)
	assert.False(t, optResult.Required)
}

func TestChecker_HTTP500Status(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	checker := NewChecker([]ServiceCheck{
		{Name: "error-svc", URL: srv.URL, Timeout: 2 * time.Second, Required: true},
	})

	report := checker.Check(context.Background())

	assert.False(t, report.Healthy)
	result := report.Get("error-svc")
	require.NotNil(t, result)
	assert.False(t, result.Healthy)
	assert.Contains(t, result.Err.Error(), "HTTP 500")
}

func TestChecker_404Status(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	checker := NewChecker([]ServiceCheck{
		{Name: "notfound", URL: srv.URL, Timeout: 2 * time.Second, Required: true},
	})

	report := checker.Check(context.Background())

	result := report.Get("notfound")
	require.NotNil(t, result)
	assert.False(t, result.Healthy)
	assert.Contains(t, result.Err.Error(), "HTTP 404")
}

func TestChecker_TimeoutExceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	checker := NewChecker([]ServiceCheck{
		{Name: "slow", URL: srv.URL, Timeout: 50 * time.Millisecond, Required: true},
	})

	report := checker.Check(context.Background())

	result := report.Get("slow")
	require.NotNil(t, result)
	assert.False(t, result.Healthy)
	assert.Error(t, result.Err)
}

func TestChecker_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	checker := NewChecker([]ServiceCheck{
		{Name: "slow", URL: srv.URL, Timeout: 5 * time.Second, Required: true},
	})

	report := checker.Check(ctx)

	result := report.Get("slow")
	require.NotNil(t, result)
	assert.False(t, result.Healthy)
}

func TestChecker_EmptyServices(t *testing.T) {
	checker := NewChecker([]ServiceCheck{})
	report := checker.Check(context.Background())

	assert.True(t, report.Healthy)
	assert.Len(t, report.Results, 0)
}

func TestChecker_ConcurrentRequests(t *testing.T) {
	var callCount int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	services := make([]ServiceCheck, 10)
	for i := range services {
		services[i] = ServiceCheck{
			Name:     fmt.Sprintf("svc-%d", i),
			URL:      srv.URL,
			Timeout:  2 * time.Second,
			Required: true,
		}
	}

	checker := NewChecker(services)
	report := checker.Check(context.Background())

	assert.True(t, report.Healthy)
	assert.Equal(t, int32(10), atomic.LoadInt32(&callCount))
}

func TestChecker_InvalidURL(t *testing.T) {
	checker := NewChecker([]ServiceCheck{
		{Name: "bad", URL: "http://[::1]:namedport", Timeout: 1 * time.Second, Required: true},
	})

	report := checker.Check(context.Background())

	result := report.Get("bad")
	require.NotNil(t, result)
	assert.False(t, result.Healthy)
	assert.Error(t, result.Err)
}

// ---------------------------------------------------------------------------
// Default services + convenience functions
// ---------------------------------------------------------------------------

func TestDefaultServices(t *testing.T) {
	services := DefaultServices()
	assert.Len(t, services, 3)

	names := make(map[string]bool)
	for _, s := range services {
		names[s.Name] = true
		assert.NotEmpty(t, s.URL)
		assert.True(t, s.Timeout > 0)
	}

	assert.True(t, names["forgejo"])
	assert.True(t, names["chimera"])
	assert.True(t, names["langfuse"])

	// forgejo and chimera are required, langfuse is optional
	forgejo := findService(services, "forgejo")
	assert.True(t, forgejo.Required)

	chimera := findService(services, "chimera")
	assert.True(t, chimera.Required)

	langfuse := findService(services, "langfuse")
	assert.False(t, langfuse.Required)
}

func TestCheckServices_WithMockServices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Replace with mock servers — all healthy
	services := []ServiceCheck{
		{Name: "mock1", URL: srv.URL, Timeout: 2 * time.Second, Required: true},
		{Name: "mock2", URL: srv.URL, Timeout: 2 * time.Second, Required: true},
	}

	err := CheckServicesWithConfig(services)
	assert.NoError(t, err)
}

func TestCheckServices_RequiredFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	services := []ServiceCheck{
		{Name: "healthy", URL: srv.URL, Timeout: 2 * time.Second, Required: true},
		{Name: "unreachable", URL: "http://127.0.0.1:59997", Timeout: 500 * time.Millisecond, Required: true},
	}

	err := CheckServicesWithConfig(services)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "required services unhealthy")
	assert.Contains(t, err.Error(), "unreachable")
}

func TestCheckServices_OptionalFailureNoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	services := []ServiceCheck{
		{Name: "healthy", URL: srv.URL, Timeout: 2 * time.Second, Required: true},
		{Name: "optional-down", URL: "http://127.0.0.1:59996", Timeout: 500 * time.Millisecond, Required: false},
	}

	err := CheckServicesWithConfig(services)
	assert.NoError(t, err) // optional failure doesn't cause error
}

// ---------------------------------------------------------------------------
// WithClient
// ---------------------------------------------------------------------------

func TestChecker_WithClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	customClient := &http.Client{Timeout: 10 * time.Second}
	checker := NewChecker([]ServiceCheck{
		{Name: "test", URL: srv.URL, Timeout: 2 * time.Second, Required: true},
	}).WithClient(customClient)

	report := checker.Check(context.Background())
	assert.True(t, report.Healthy)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func findService(services []ServiceCheck, name string) ServiceCheck {
	for _, s := range services {
		if s.Name == name {
			return s
		}
	}
	return ServiceCheck{}
}
