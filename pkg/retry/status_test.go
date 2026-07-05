package retry

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// ============================================================================
// PolicyStats tests
// ============================================================================

func TestPolicyStats_RecordSuccess(t *testing.T) {
	s := newPolicyStats("test", DefaultConfig())
	s.RecordResult(nil)
	if s.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", s.Attempts)
	}
	if s.Successes != 1 {
		t.Errorf("Successes = %d, want 1", s.Successes)
	}
	if s.Failures != 0 {
		t.Errorf("Failures = %d, want 0", s.Failures)
	}
	if s.LastError != "" {
		t.Errorf("LastError = %q, want empty", s.LastError)
	}
}

func TestPolicyStats_RecordFailure(t *testing.T) {
	s := newPolicyStats("test", DefaultConfig())
	err := errors.New("connection refused")
	s.RecordResult(err)
	if s.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", s.Attempts)
	}
	if s.Failures != 1 {
		t.Errorf("Failures = %d, want 1", s.Failures)
	}
	if s.Successes != 0 {
		t.Errorf("Successes = %d, want 0", s.Successes)
	}
	if s.LastError != "connection refused" {
		t.Errorf("LastError = %q, want %q", s.LastError, "connection refused")
	}
}

func TestPolicyStats_CircuitOpensOnThreshold(t *testing.T) {
	s := newPolicyStats("test", DefaultConfig())
	s.MaxFailures = 3
	for i := 0; i < 3; i++ {
		s.RecordResult(errors.New("fail"))
	}
	if s.CircuitState != CircuitOpen {
		t.Errorf("CircuitState = %q, want %q", s.CircuitState, CircuitOpen)
	}
}

func TestPolicyStats_CircuitStaysClosedBelowThreshold(t *testing.T) {
	s := newPolicyStats("test", DefaultConfig())
	s.MaxFailures = 5
	for i := 0; i < 4; i++ {
		s.RecordResult(errors.New("fail"))
	}
	if s.CircuitState != CircuitClosed {
		t.Errorf("CircuitState = %q, want %q", s.CircuitState, CircuitClosed)
	}
}

func TestPolicyStats_CircuitRecovery(t *testing.T) {
	s := newPolicyStats("test", DefaultConfig())
	s.MaxFailures = 2
	s.Config.MaxBackoff = 10 * time.Millisecond // short recovery for test

	// Trip the circuit
	s.RecordResult(errors.New("fail"))
	s.RecordResult(errors.New("fail"))
	if s.CircuitState != CircuitOpen {
		t.Fatalf("expected open, got %q", s.CircuitState)
	}

	// Wait for recovery timeout
	time.Sleep(15 * time.Millisecond)

	// CanAttempt should transition to half-open
	if !s.CanAttempt() {
		t.Error("CanAttempt should return true after recovery timeout")
	}
	if s.CircuitState != CircuitHalfOpen {
		t.Errorf("expected half-open, got %q", s.CircuitState)
	}

	// Two successes in half-open should close the circuit
	s.RecordResult(nil)
	s.RecordResult(nil)
	if s.CircuitState != CircuitClosed {
		t.Errorf("expected closed after recovery, got %q", s.CircuitState)
	}
}

func TestPolicyStats_CircuitOpenBlocksRequests(t *testing.T) {
	s := newPolicyStats("test", DefaultConfig())
	s.MaxFailures = 1
	s.RecordResult(errors.New("fail"))
	if s.CircuitState != CircuitOpen {
		t.Fatalf("expected open, got %q", s.CircuitState)
	}
	// No time has passed, so CanAttempt should return false
	if s.CanAttempt() {
		t.Error("CanAttempt should return false when circuit is open and no time has passed")
	}
}

func TestPolicyStats_FailureWindowPruning(t *testing.T) {
	s := newPolicyStats("test", DefaultConfig())
	s.MaxFailures = 10
	s.WindowDuration = 50 * time.Millisecond

	s.RecordResult(errors.New("fail"))
	time.Sleep(60 * time.Millisecond)
	s.RecordResult(errors.New("fail"))

	// The first failure should have been pruned
	if len(s.FailureWindow) != 1 {
		t.Errorf("FailureWindow len = %d, want 1 (pruned)", len(s.FailureWindow))
	}
}

func TestPolicyStats_Reset(t *testing.T) {
	s := newPolicyStats("test", DefaultConfig())
	s.RecordResult(errors.New("fail"))
	s.RecordResult(nil)
	s.Reset()
	if s.Attempts != 0 || s.Successes != 0 || s.Failures != 0 {
		t.Errorf("Reset didn't clear stats: %+v", s)
	}
	if s.CircuitState != CircuitClosed {
		t.Errorf("Reset should set circuit to closed, got %q", s.CircuitState)
	}
	if len(s.FailureWindow) != 0 {
		t.Errorf("Reset should clear failure window, got %d", len(s.FailureWindow))
	}
}

func TestPolicyStats_Snapshot(t *testing.T) {
	s := newPolicyStats("test", DefaultConfig())
	s.RecordResult(errors.New("fail"))
	snap := s.Snapshot()
	if snap.Name != "test" {
		t.Errorf("snapshot Name = %q, want %q", snap.Name, "test")
	}
	if snap.Attempts != 1 {
		t.Errorf("snapshot Attempts = %d, want 1", snap.Attempts)
	}
	if snap.Failures != 1 {
		t.Errorf("snapshot Failures = %d, want 1", snap.Failures)
	}
}

func TestPolicyStats_SuccessRate(t *testing.T) {
	s := newPolicyStats("test", DefaultConfig())
	s.RecordResult(nil)
	s.RecordResult(nil)
	s.RecordResult(errors.New("fail"))
	rate := s.SuccessRate()
	if rate < 0.65 || rate > 0.69 {
		t.Errorf("SuccessRate = %.2f, want ~0.667", rate)
	}
}

func TestPolicyStats_SuccessRateNoAttempts(t *testing.T) {
	s := newPolicyStats("test", DefaultConfig())
	if s.SuccessRate() != 0 {
		t.Errorf("SuccessRate with no attempts should be 0, got %f", s.SuccessRate())
	}
}

func TestPolicyStats_Concurrent(t *testing.T) {
	s := newPolicyStats("test", DefaultConfig())
	s.MaxFailures = 1000 // avoid tripping during concurrent test
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.RecordResult(nil)
		}()
	}
	wg.Wait()
	if s.Attempts != 100 {
		t.Errorf("Attempts = %d, want 100", s.Attempts)
	}
	if s.Successes != 100 {
		t.Errorf("Successes = %d, want 100", s.Successes)
	}
}

// ============================================================================
// Registry tests
// ============================================================================

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()
	s := r.Register("forgejo", DefaultConfig())
	if s.Name != "forgejo" {
		t.Errorf("Name = %q, want %q", s.Name, "forgejo")
	}
	if r.Count() != 1 {
		t.Errorf("Count = %d, want 1", r.Count())
	}
}

func TestRegistry_RegisterIdempotent(t *testing.T) {
	r := NewRegistry()
	s1 := r.Register("forgejo", DefaultConfig())
	s2 := r.Register("forgejo", DefaultConfig())
	if s1 != s2 {
		t.Error("Register should return same pointer for existing policy")
	}
	if r.Count() != 1 {
		t.Errorf("Count = %d, want 1", r.Count())
	}
}

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry()
	r.Register("forgejo", DefaultConfig())
	s := r.Get("forgejo")
	if s == nil {
		t.Fatal("Get returned nil for existing policy")
	}
	if s.Name != "forgejo" {
		t.Errorf("Name = %q, want %q", s.Name, "forgejo")
	}
}

func TestRegistry_GetMissing(t *testing.T) {
	r := NewRegistry()
	if r.Get("nonexistent") != nil {
		t.Error("Get should return nil for missing policy")
	}
}

func TestRegistry_Status(t *testing.T) {
	r := NewRegistry()
	r.Register("forgejo", DefaultConfig())
	r.Register("chimera", DefaultConfig())
	r.Register("axiom", DefaultConfig())
	status := r.Status()
	if len(status) != 3 {
		t.Fatalf("len = %d, want 3", len(status))
	}
	// Should be sorted by name
	if status[0].Name != "axiom" {
		t.Errorf("status[0].Name = %q, want %q", status[0].Name, "axiom")
	}
	if status[1].Name != "chimera" {
		t.Errorf("status[1].Name = %q, want %q", status[1].Name, "chimera")
	}
	if status[2].Name != "forgejo" {
		t.Errorf("status[2].Name = %q, want %q", status[2].Name, "forgejo")
	}
}

func TestRegistry_RecordResult(t *testing.T) {
	r := NewRegistry()
	r.RecordResult("forgejo", nil)
	r.RecordResult("forgejo", errors.New("fail"))
	s := r.Get("forgejo")
	if s.Attempts != 2 {
		t.Errorf("Attempts = %d, want 2", s.Attempts)
	}
	if s.Successes != 1 {
		t.Errorf("Successes = %d, want 1", s.Successes)
	}
	if s.Failures != 1 {
		t.Errorf("Failures = %d, want 1", s.Failures)
	}
}

func TestRegistry_RecordResultAutoRegister(t *testing.T) {
	r := NewRegistry()
	r.RecordResult("new-policy", nil)
	if r.Get("new-policy") == nil {
		t.Error("RecordResult should auto-register unknown policies")
	}
}

func TestRegistry_Reset(t *testing.T) {
	r := NewRegistry()
	r.RecordResult("forgejo", errors.New("fail"))
	r.Reset("forgejo")
	s := r.Get("forgejo")
	if s.Attempts != 0 {
		t.Errorf("Attempts after Reset = %d, want 0", s.Attempts)
	}
}

func TestRegistry_ResetAll(t *testing.T) {
	r := NewRegistry()
	r.RecordResult("forgejo", errors.New("fail"))
	r.RecordResult("chimera", errors.New("fail"))
	r.Reset("")
	for _, s := range r.Status() {
		if s.Attempts != 0 {
			t.Errorf("%s: Attempts = %d, want 0", s.Name, s.Attempts)
		}
	}
}

func TestRegistry_CircuitBreakers(t *testing.T) {
	r := NewRegistry()
	s := r.Register("forgejo", DefaultConfig())
	s.MaxFailures = 1
	s.RecordResult(errors.New("fail"))
	breakers := r.CircuitBreakers()
	if breakers["forgejo"] != CircuitOpen {
		t.Errorf("CircuitBreakers[forgejo] = %q, want %q", breakers["forgejo"], CircuitOpen)
	}
}

func TestRegistry_Names(t *testing.T) {
	r := NewRegistry()
	r.Register("chimera", DefaultConfig())
	r.Register("forgejo", DefaultConfig())
	r.Register("axiom", DefaultConfig())
	names := r.Names()
	if len(names) != 3 {
		t.Fatalf("len = %d, want 3", len(names))
	}
	if names[0] != "axiom" || names[1] != "chimera" || names[2] != "forgejo" {
		t.Errorf("Names = %v, want [axiom chimera forgejo]", names)
	}
}

func TestDefaultRegistry(t *testing.T) {
	r1 := DefaultRegistry()
	r2 := DefaultRegistry()
	if r1 != r2 {
		t.Error("DefaultRegistry should return same instance")
	}
}

// ============================================================================
// ChaosInjector tests
// ============================================================================

func TestChaosInjector_NotEnabledWithoutEnvVar(t *testing.T) {
	oldGetenv := osGetenv
	osGetenv = func(string) string { return "" }
	defer func() { osGetenv = oldGetenv }()

	c := NewChaosInjector(0.5, 60*time.Second)
	if c.Enabled {
		t.Error("ChaosInjector should not be enabled without env var")
	}
	if c.IsActive() {
		t.Error("IsActive should return false when not enabled")
	}
}

func TestChaosInjector_EnabledWithEnvVar(t *testing.T) {
	oldGetenv := osGetenv
	osGetenv = func(key string) string {
		if key == "HELIX_CHAOS_ENABLED" {
			return "1"
		}
		return ""
	}
	defer func() { osGetenv = oldGetenv }()

	c := NewChaosInjector(0.5, 60*time.Second)
	if !c.Enabled {
		t.Error("ChaosInjector should be enabled with env var=1")
	}
	if !c.IsActive() {
		t.Error("IsActive should return true when enabled and within duration")
	}
}

func TestChaosInjector_ZeroFailureRate(t *testing.T) {
	oldGetenv := osGetenv
	osGetenv = func(key string) string {
		if key == "HELIX_CHAOS_ENABLED" {
			return "1"
		}
		return ""
	}
	defer func() { osGetenv = oldGetenv }()

	c := NewChaosInjector(0.0, 60*time.Second)
	if c.IsActive() {
		t.Error("IsActive should return false with 0 failure rate")
	}
}

func TestChaosInjector_ExpiredDuration(t *testing.T) {
	oldGetenv := osGetenv
	osGetenv = func(key string) string {
		if key == "HELIX_CHAOS_ENABLED" {
			return "1"
		}
		return ""
	}
	defer func() { osGetenv = oldGetenv }()

	c := NewChaosInjector(0.5, 1*time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	if c.IsActive() {
		t.Error("IsActive should return false after duration expired")
	}
}

func TestChaosInjector_MaybeFail(t *testing.T) {
	oldGetenv := osGetenv
	osGetenv = func(key string) string {
		if key == "HELIX_CHAOS_ENABLED" {
			return "1"
		}
		return ""
	}
	defer func() { osGetenv = oldGetenv }()

	c := NewChaosInjector(1.0, 60*time.Second) // 100% failure rate
	err := c.MaybeFail()
	if err == nil {
		t.Error("MaybeFail with 100% rate should return error")
	}
	if err != nil && !strings.Contains(err.Error(), "chaos") {
		t.Errorf("MaybeFail error = %q, should contain 'chaos'", err.Error())
	}
}

func TestChaosInjector_MaybeFailZeroRate(t *testing.T) {
	oldGetenv := osGetenv
	osGetenv = func(key string) string {
		if key == "HELIX_CHAOS_ENABLED" {
			return "1"
		}
		return ""
	}
	defer func() { osGetenv = oldGetenv }()

	c := NewChaosInjector(0.0, 60*time.Second)
	if err := c.MaybeFail(); err != nil {
		t.Errorf("MaybeFail with 0%% rate should return nil, got %v", err)
	}
}

func TestChaosInjector_NotActiveMaybeFail(t *testing.T) {
	oldGetenv := osGetenv
	osGetenv = func(string) string { return "" }
	defer func() { osGetenv = oldGetenv }()

	c := NewChaosInjector(1.0, 60*time.Second)
	if err := c.MaybeFail(); err != nil {
		t.Errorf("MaybeFail when not active should return nil, got %v", err)
	}
}

func TestWrapWithChaos(t *testing.T) {
	oldGetenv := osGetenv
	osGetenv = func(key string) string {
		if key == "HELIX_CHAOS_ENABLED" {
			return "1"
		}
		return ""
	}
	defer func() { osGetenv = oldGetenv }()

	c := NewChaosInjector(1.0, 60*time.Second) // always fail
	fn := func(ctx context.Context) (string, error) {
		return "success", nil
	}
	wrapped := WrapWithChaos(fn, c)
	_, err := wrapped(context.Background())
	if err == nil {
		t.Error("wrapped function should fail with 100% chaos")
	}
}

func TestWrapWithChaos_NotEnabled(t *testing.T) {
	oldGetenv := osGetenv
	osGetenv = func(string) string { return "" }
	defer func() { osGetenv = oldGetenv }()

	c := NewChaosInjector(1.0, 60*time.Second) // not enabled
	fn := func(ctx context.Context) (string, error) {
		return "success", nil
	}
	wrapped := WrapWithChaos(fn, c)
	result, err := wrapped(context.Background())
	if err != nil {
		t.Errorf("wrapped should pass through when chaos not enabled: %v", err)
	}
	if result != "success" {
		t.Errorf("result = %q, want %q", result, "success")
	}
}

// ============================================================================
// StatusReport + Formatting tests
// ============================================================================

func TestBuildStatusReport(t *testing.T) {
	r := NewRegistry()
	r.Register("forgejo", DefaultConfig())
	r.Register("chimera", DefaultConfig())
	r.RecordResult("forgejo", nil)
	r.RecordResult("forgejo", errors.New("fail"))

	report := BuildStatusReport(r)
	if report.TotalPolicies != 2 {
		t.Errorf("TotalPolicies = %d, want 2", report.TotalPolicies)
	}
	if len(report.Policies) != 2 {
		t.Errorf("Policies len = %d, want 2", len(report.Policies))
	}
	if len(report.CircuitBreakers) != 2 {
		t.Errorf("CircuitBreakers len = %d, want 2", len(report.CircuitBreakers))
	}
}

func TestBuildStatusReport_OpenCircuits(t *testing.T) {
	r := NewRegistry()
	s := r.Register("forgejo", DefaultConfig())
	s.MaxFailures = 1
	s.RecordResult(errors.New("fail"))

	report := BuildStatusReport(r)
	if report.OpenCircuits != 1 {
		t.Errorf("OpenCircuits = %d, want 1", report.OpenCircuits)
	}
}

func TestFormatStatusTable(t *testing.T) {
	r := NewRegistry()
	r.Register("forgejo", DefaultConfig())
	r.RecordResult("forgejo", nil)
	r.RecordResult("forgejo", errors.New("timeout"))

	report := BuildStatusReport(r)
	output := FormatStatusTable(report)
	if !strings.Contains(output, "forgejo") {
		t.Errorf("output missing policy name: %s", output)
	}
	if !strings.Contains(output, "closed") {
		t.Errorf("output missing circuit state: %s", output)
	}
	if !strings.Contains(output, "POLICY") {
		t.Errorf("output missing header: %s", output)
	}
}

func TestFormatStatusTable_Empty(t *testing.T) {
	r := NewRegistry()
	report := BuildStatusReport(r)
	output := FormatStatusTable(report)
	if !strings.Contains(output, "0 policies") {
		t.Errorf("output should show 0 policies: %s", output)
	}
}

func TestFormatStatusTable_LongError(t *testing.T) {
	r := NewRegistry()
	r.RecordResult("forgejo", errors.New("this is a very long error message that should be truncated in the table output"))
	report := BuildStatusReport(r)
	output := FormatStatusTable(report)
	if !strings.Contains(output, "...") {
		t.Errorf("output should truncate long error: %s", output)
	}
}

// ============================================================================
// Integration test
// ============================================================================

func TestRegistry_FullLifecycle(t *testing.T) {
	r := NewRegistry()
	r.Register("forgejo", Config{MaxAttempts: 4, InitialBackoff: 1 * time.Millisecond, MaxBackoff: 10 * time.Millisecond})

	// Record some failures
	for i := 0; i < 5; i++ {
		r.RecordResult("forgejo", errors.New("conn refused"))
	}

	// Circuit should be open
	breakers := r.CircuitBreakers()
	if breakers["forgejo"] != CircuitOpen {
		t.Fatalf("expected circuit open, got %q", breakers["forgejo"])
	}

	// Reset
	r.Reset("forgejo")
	breakers = r.CircuitBreakers()
	if breakers["forgejo"] != CircuitClosed {
		t.Fatalf("expected circuit closed after reset, got %q", breakers["forgejo"])
	}
}
