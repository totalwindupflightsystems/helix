// Package health provides startup service-health validation for the Helix platform.
// All Helix CLI tools call CheckServices at startup to fail-fast when required
// services are unreachable.
//
// Based on specs/cross-component-wiring.md §8 and specs/helix-config.md §7.
package health

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// ServiceCheck defines a single service to health-check.
type ServiceCheck struct {
	Name    string // Human-readable service name (e.g., "forgejo")
	URL     string // Full health-check URL (e.g., "http://localhost:3030/api/v1/version")
	Timeout time.Duration
	Required bool // If true, failure causes overall Check to fail. If false, failure is a warning.
}

// ServiceResult captures the outcome of a single service health check.
type ServiceResult struct {
	Name     string
	Healthy  bool
	Latency  time.Duration
	Err      error
	Required bool
}

// IsRequired returns whether this service is required for overall health.
func (r *ServiceResult) IsRequired() bool { return r.Required }

// HealthReport aggregates results from all checked services.
type HealthReport struct {
	Results  []ServiceResult
	Healthy  bool // true if all required services are healthy
	Took     time.Duration
}

// HasFailures returns true if any service failed (required or optional).
func (r *HealthReport) HasFailures() bool {
	for _, res := range r.Results {
		if !res.Healthy {
			return true
		}
	}
	return false
}

// HasRequiredFailures returns true if any required service failed.
func (r *HealthReport) HasRequiredFailures() bool {
	for _, res := range r.Results {
		if !res.Healthy && res.Required {
			return true
		}
	}
	return false
}

// Get returns the result for a named service, or nil if not found.
func (r *HealthReport) Get(name string) *ServiceResult {
	for i := range r.Results {
		if r.Results[i].Name == name {
			return &r.Results[i]
		}
	}
	return nil
}

// Checker probes service health endpoints concurrently.
type Checker struct {
	services []ServiceCheck
	client   *http.Client
}

// NewChecker creates a health checker for the given services.
func NewChecker(services []ServiceCheck) *Checker {
	return &Checker{
		services: services,
		client:   &http.Client{},
	}
}

// WithClient sets a custom HTTP client (useful for testing or custom transport).
func (c *Checker) WithClient(client *http.Client) *Checker {
	c.client = client
	return c
}

// DefaultTimeout is the default per-service health check timeout.
const DefaultTimeout = 5 * time.Second

// DefaultServices returns the standard Helix service health checks
// based on specs/cross-component-wiring.md §1.
func DefaultServices() []ServiceCheck {
	return []ServiceCheck{
		{
			Name:     "forgejo",
			URL:      "http://localhost:3030/api/v1/version",
			Timeout:  DefaultTimeout,
			Required: true,
		},
		{
			Name:     "chimera",
			URL:      "http://localhost:8765/v1/health",
			Timeout:  DefaultTimeout,
			Required: true,
		},
		{
			Name:     "langfuse",
			URL:      "http://localhost:3001/api/public/health",
			Timeout:  DefaultTimeout,
			Required: false,
		},
	}
}

// Check probes all configured services concurrently and returns an aggregated report.
func (c *Checker) Check(ctx context.Context) *HealthReport {
	start := time.Now()

	var wg sync.WaitGroup
	results := make([]ServiceResult, len(c.services))

	for i, svc := range c.services {
		wg.Add(1)
		go func(idx int, check ServiceCheck) {
			defer wg.Done()
			results[idx] = c.checkOne(ctx, check)
		}(i, svc)
	}

	wg.Wait()

	overall := true
	for _, r := range results {
		if !r.Healthy && r.Required {
			overall = false
			break
		}
	}

	return &HealthReport{
		Results: results,
		Healthy: overall,
		Took:    time.Since(start),
	}
}

// checkOne probes a single service endpoint.
func (c *Checker) checkOne(ctx context.Context, check ServiceCheck) ServiceResult {
	timeout := check.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, check.URL, nil)
	if err != nil {
		return ServiceResult{
			Name:     check.Name,
			Healthy:  false,
			Err:      fmt.Errorf("invalid URL %s: %w", check.URL, err),
			Required: check.Required,
		}
	}

	start := time.Now()
	resp, err := c.client.Do(req)
	latency := time.Since(start)

	if err != nil {
		return ServiceResult{
			Name:     check.Name,
			Healthy:  false,
			Latency:  latency,
			Err:      err,
			Required: check.Required,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ServiceResult{
			Name:     check.Name,
			Healthy:  false,
			Latency:  latency,
			Err:      fmt.Errorf("HTTP %d from %s", resp.StatusCode, check.URL),
			Required: check.Required,
		}
	}

	return ServiceResult{
		Name:     check.Name,
		Healthy:  true,
		Latency:  latency,
		Required: check.Required,
	}
}

// CheckServices is a convenience function that checks the default Helix
// services and returns nil if all required services are healthy, or an
// error listing the failed services.
func CheckServices() error {
	return CheckServicesWithConfig(DefaultServices())
}

// CheckServicesWithConfig checks the given services and returns an error
// if any required service is unhealthy.
func CheckServicesWithConfig(services []ServiceCheck) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	checker := NewChecker(services)
	report := checker.Check(ctx)

	if !report.HasRequiredFailures() {
		return nil
	}

	var failed []string
	for _, r := range report.Results {
		if !r.Healthy && r.Required {
			failed = append(failed, fmt.Sprintf("%s: %v", r.Name, r.Err))
		}
	}

	return fmt.Errorf("required services unhealthy: %v", failed)
}
