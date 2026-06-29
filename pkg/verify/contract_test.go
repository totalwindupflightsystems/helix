package verify

import (
	"math"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Contract parsing
// =============================================================================

var validContractYAML = []byte(`
contract:
  name: auth-session-v2
  agent: agent-uuid-1234
  merge_commit: abc123def456
  assertions:
    - metric: auth_token_refresh_success_rate
      operator: gte
      value: 0.999
      window: 1h
    - metric: auth_token_refresh_p99_latency_ms
      operator: lte
      value: 200
      window: 1h
    - metric: concurrent_session_limit_errors
      operator: eq
      value: 0
      window: 24h
  breach_action: rollback_and_notify
`)

func TestParseContract_Valid(t *testing.T) {
	c, err := ParseContract(validContractYAML)
	if err != nil {
		t.Fatalf("ParseContract: %v", err)
	}
	if c.Contract.Name != "auth-session-v2" {
		t.Errorf("expected name 'auth-session-v2', got %q", c.Contract.Name)
	}
	if c.Contract.Agent != "agent-uuid-1234" {
		t.Errorf("expected agent 'agent-uuid-1234', got %q", c.Contract.Agent)
	}
	if len(c.Contract.Assertions) != 3 {
		t.Errorf("expected 3 assertions, got %d", len(c.Contract.Assertions))
	}
	if c.Contract.BreachAction != "rollback_and_notify" {
		t.Errorf("expected breach_action 'rollback_and_notify', got %q", c.Contract.BreachAction)
	}
}

func TestParseContract_MissingName(t *testing.T) {
	_, err := ParseContract([]byte(`contract:
  agent: agent-1
  merge_commit: abc123
  assertions:
    - metric: m1
      operator: gte
      value: 1
      window: 1h
`))
	if err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Errorf("expected 'name is required' error, got %v", err)
	}
}

func TestParseContract_MissingAgent(t *testing.T) {
	_, err := ParseContract([]byte(`contract:
  name: test-contract
  merge_commit: abc123
  assertions:
    - metric: m1
      operator: gte
      value: 1
      window: 1h
`))
	if err == nil || !strings.Contains(err.Error(), "agent is required") {
		t.Errorf("expected 'agent is required' error, got %v", err)
	}
}

func TestParseContract_MissingMergeCommit(t *testing.T) {
	_, err := ParseContract([]byte(`contract:
  name: test-contract
  agent: agent-1
  assertions:
    - metric: m1
      operator: gte
      value: 1
      window: 1h
`))
	if err == nil || !strings.Contains(err.Error(), "merge_commit") {
		t.Errorf("expected 'merge_commit is required' error, got %v", err)
	}
}

func TestParseContract_NoAssertions(t *testing.T) {
	_, err := ParseContract([]byte(`contract:
  name: test-contract
  agent: agent-1
  merge_commit: abc123
  assertions: []
`))
	if err == nil || !strings.Contains(err.Error(), "assertion is required") {
		t.Errorf("expected 'assertion is required' error, got %v", err)
	}
}

func TestParseContract_InvalidOperator(t *testing.T) {
	_, err := ParseContract([]byte(`contract:
  name: test-contract
  agent: agent-1
  merge_commit: abc123
  assertions:
    - metric: m1
      operator: invalid_op
      value: 1
      window: 1h
`))
	if err == nil || !strings.Contains(err.Error(), "unknown operator") {
		t.Errorf("expected 'unknown operator' error, got %v", err)
	}
}

func TestParseContract_InvalidBreachAction(t *testing.T) {
	_, err := ParseContract([]byte(`contract:
  name: test-contract
  agent: agent-1
  merge_commit: abc123
  assertions:
    - metric: m1
      operator: gte
      value: 1
      window: 1h
  breach_action: self_destruct
`))
	if err == nil || !strings.Contains(err.Error(), "unknown breach_action") {
		t.Errorf("expected 'unknown breach_action' error, got %v", err)
	}
}

func TestParseContract_EmptyBreachActionDefaults(t *testing.T) {
	c, err := ParseContract([]byte(`contract:
  name: test-contract
  agent: agent-1
  merge_commit: abc123
  assertions:
    - metric: m1
      operator: gte
      value: 1
      window: 1h
`))
	if err != nil {
		t.Fatalf("ParseContract: %v", err)
	}
	if c.Contract.BreachAction != "" {
		t.Errorf("expected empty breach_action, got %q", c.Contract.BreachAction)
	}
	if c.ShouldRollback() {
		t.Error("expected ShouldRollback=false for empty breach_action")
	}
	if c.ShouldNotify() {
		t.Error("expected ShouldNotify=false for empty breach_action")
	}
}

// =============================================================================
// ShouldRollback / ShouldNotify
// =============================================================================

func TestBehaviorContract_ShouldRollback(t *testing.T) {
	c := &BehaviorContract{Contract: ContractBody{BreachAction: "rollback"}}
	if !c.ShouldRollback() {
		t.Error("expected rollback=true for 'rollback'")
	}
	c2 := &BehaviorContract{Contract: ContractBody{BreachAction: "rollback_and_notify"}}
	if !c2.ShouldRollback() {
		t.Error("expected rollback=true for 'rollback_and_notify'")
	}
	c3 := &BehaviorContract{Contract: ContractBody{BreachAction: "notify_only"}}
	if c3.ShouldRollback() {
		t.Error("expected rollback=false for 'notify_only'")
	}
}

func TestBehaviorContract_ShouldNotify(t *testing.T) {
	c := &BehaviorContract{Contract: ContractBody{BreachAction: "notify_only"}}
	if !c.ShouldNotify() {
		t.Error("expected notify=true for 'notify_only'")
	}
	c2 := &BehaviorContract{Contract: ContractBody{BreachAction: "rollback_and_notify"}}
	if !c2.ShouldNotify() {
		t.Error("expected notify=true for 'rollback_and_notify'")
	}
	c3 := &BehaviorContract{Contract: ContractBody{BreachAction: "rollback"}}
	if c3.ShouldNotify() {
		t.Error("expected notify=false for 'rollback'")
	}
}

// =============================================================================
// Operator resolution
// =============================================================================

func TestOperator_Valid(t *testing.T) {
	tests := []struct {
		s    string
		want AssertionOperator
	}{
		{"gte", OpGte},
		{"lte", OpLte},
		{"eq", OpEq},
	}
	for _, tc := range tests {
		op, err := Operator(tc.s)
		if err != nil {
			t.Errorf("Operator(%q): %v", tc.s, err)
		}
		if op != tc.want {
			t.Errorf("Operator(%q) = %d, want %d", tc.s, op, tc.want)
		}
	}
}

func TestOperator_Invalid(t *testing.T) {
	if _, err := Operator("gt"); err == nil {
		t.Error("expected error for 'gt'")
	}
	if _, err := Operator(""); err == nil {
		t.Error("expected error for empty string")
	}
}

// =============================================================================
// Checker
// =============================================================================

func TestChecker_Check_Gte_Pass(t *testing.T) {
	ch := NewChecker()
	r := ch.Check(Assertion{Metric: "m1", Op: "gte", Value: 100}, 150)
	if !r.Passed {
		t.Errorf("expected pass (150 >= 100): %s", r.Reason)
	}
}

func TestChecker_Check_Gte_Boundary(t *testing.T) {
	ch := NewChecker()
	r := ch.Check(Assertion{Metric: "m1", Op: "gte", Value: 100}, 100)
	if !r.Passed {
		t.Errorf("expected pass (100 >= 100): %s", r.Reason)
	}
}

func TestChecker_Check_Gte_Fail(t *testing.T) {
	ch := NewChecker()
	r := ch.Check(Assertion{Metric: "m1", Op: "gte", Value: 100}, 99)
	if r.Passed {
		t.Error("expected fail (99 < 100)")
	}
}

func TestChecker_Check_Lte_Pass(t *testing.T) {
	ch := NewChecker()
	r := ch.Check(Assertion{Metric: "m1", Op: "lte", Value: 200}, 150)
	if !r.Passed {
		t.Errorf("expected pass (150 <= 200): %s", r.Reason)
	}
}

func TestChecker_Check_Lte_Boundary(t *testing.T) {
	ch := NewChecker()
	r := ch.Check(Assertion{Metric: "m1", Op: "lte", Value: 200}, 200)
	if !r.Passed {
		t.Errorf("expected pass (200 <= 200): %s", r.Reason)
	}
}

func TestChecker_Check_Lte_Fail(t *testing.T) {
	ch := NewChecker()
	r := ch.Check(Assertion{Metric: "m1", Op: "lte", Value: 200}, 250)
	if r.Passed {
		t.Error("expected fail (250 > 200)")
	}
}

func TestChecker_Check_Eq_Pass(t *testing.T) {
	ch := NewChecker()
	r := ch.Check(Assertion{Metric: "m1", Op: "eq", Value: 42}, 42)
	if !r.Passed {
		t.Errorf("expected pass (42 == 42): %s", r.Reason)
	}
}

func TestChecker_Check_Eq_Fail(t *testing.T) {
	ch := NewChecker()
	r := ch.Check(Assertion{Metric: "m1", Op: "eq", Value: 42}, 43)
	if r.Passed {
		t.Error("expected fail (43 != 42)")
	}
}

func TestChecker_Check_InvalidOperator(t *testing.T) {
	ch := NewChecker()
	r := ch.Check(Assertion{Metric: "m1", Op: "unknown"}, 100)
	if r.Passed {
		t.Error("expected fail for unknown operator")
	}
}

// =============================================================================
// CheckAll & AllPassed
// =============================================================================

func TestChecker_CheckAll_AllPass(t *testing.T) {
	c, _ := ParseContract(validContractYAML)
	ch := NewChecker()
	metrics := map[string]float64{
		"auth_token_refresh_success_rate":     0.9995,
		"auth_token_refresh_p99_latency_ms":   150,
		"concurrent_session_limit_errors":     0,
	}
	results := ch.CheckAll(c, metrics)
	if !AllPassed(results) {
		for _, r := range results {
			t.Logf("  %s: %v", r.Assertion.Metric, r.Reason)
		}
		t.Fatal("expected all assertions to pass")
	}
}

func TestChecker_CheckAll_SomeFail(t *testing.T) {
	c, _ := ParseContract(validContractYAML)
	ch := NewChecker()
	metrics := map[string]float64{
		"auth_token_refresh_success_rate":     0.99,
		"auth_token_refresh_p99_latency_ms":   300,
		"concurrent_session_limit_errors":     1,
	}
	results := ch.CheckAll(c, metrics)
	if AllPassed(results) {
		t.Fatal("expected some assertions to fail")
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

func TestChecker_CheckAll_MissingMetric(t *testing.T) {
	c, _ := ParseContract(validContractYAML)
	ch := NewChecker()
	metrics := map[string]float64{
		"auth_token_refresh_success_rate": 0.999,
	}
	results := ch.CheckAll(c, metrics)
	if AllPassed(results) {
		t.Fatal("expected failures from missing metrics")
	}
	// At least 2 should fail (missing metrics)
	failCount := 0
	for _, r := range results {
		if !r.Passed {
			failCount++
		}
	}
	if failCount < 2 {
		t.Errorf("expected at least 2 failures from missing metrics, got %d", failCount)
	}
}

func TestAllPassed_AllPass(t *testing.T) {
	results := []CheckResult{
		{Passed: true},
		{Passed: true},
	}
	if !AllPassed(results) {
		t.Error("expected all passed")
	}
}

func TestAllPassed_SomeFail(t *testing.T) {
	results := []CheckResult{
		{Passed: true},
		{Passed: false, Reason: "fail"},
	}
	if AllPassed(results) {
		t.Error("expected not all passed")
	}
}

func TestAllPassed_Empty(t *testing.T) {
	if !AllPassed(nil) {
		t.Error("expected empty to pass")
	}
}

// =============================================================================
// Canary schedule
// =============================================================================

func TestCanarySchedule_Provisional(t *testing.T) {
	shadow, steps := CanarySchedule("provisional")
	if shadow != 24*time.Hour {
		t.Errorf("expected 24h shadow, got %v", shadow)
	}
	if len(steps) != 6 {
		t.Errorf("expected 6 steps, got %d", len(steps))
	}
	// Verify total is 96h
	total := TotalCanaryDuration(steps)
	if total != 96*time.Hour {
		t.Errorf("expected 96h total, got %v", total)
	}
}

func TestCanarySchedule_Observed(t *testing.T) {
	shadow, steps := CanarySchedule("observed")
	if shadow != 12*time.Hour {
		t.Errorf("expected 12h shadow, got %v", shadow)
	}
	if len(steps) != 4 {
		t.Errorf("expected 4 steps, got %d", len(steps))
	}
	total := TotalCanaryDuration(steps)
	if total != 60*time.Hour {
		t.Errorf("expected 60h total, got %v", total)
	}
}

func TestCanarySchedule_Trusted(t *testing.T) {
	shadow, steps := CanarySchedule("trusted")
	if shadow != 6*time.Hour {
		t.Errorf("expected 6h shadow, got %v", shadow)
	}
	if len(steps) != 3 {
		t.Errorf("expected 3 steps, got %d", len(steps))
	}
	total := TotalCanaryDuration(steps)
	if total != 36*time.Hour {
		t.Errorf("expected 36h total, got %v", total)
	}
}

func TestCanarySchedule_Veteran(t *testing.T) {
	shadow, steps := CanarySchedule("veteran")
	if shadow != 2*time.Hour {
		t.Errorf("expected 2h shadow, got %v", shadow)
	}
	if len(steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(steps))
	}
	total := TotalCanaryDuration(steps)
	if total != 12*time.Hour {
		t.Errorf("expected 12h total, got %v", total)
	}
}

func TestCanarySchedule_UnknownTierDefaults(t *testing.T) {
	shadow, steps := CanarySchedule("unknown")
	if shadow != 24*time.Hour {
		t.Errorf("expected 24h shadow for unknown tier, got %v", shadow)
	}
	if len(steps) != 2 {
		t.Errorf("expected 2 default steps, got %d", len(steps))
	}
}

func TestCanarySchedule_TrafficRampMonotonic(t *testing.T) {
	_, steps := CanarySchedule("provisional")
	for i := 1; i < len(steps); i++ {
		if steps[i].TrafficPct <= steps[i-1].TrafficPct {
			t.Errorf("traffic not monotonic at step %d: %.1f -> %.1f",
				i, steps[i-1].TrafficPct, steps[i].TrafficPct)
		}
	}
}

// =============================================================================
// Monitor
// =============================================================================

func TestMonitor_RegisterAndEvaluate(t *testing.T) {
	c, _ := ParseContract(validContractYAML)
	m := NewMonitor()
	m.RegisterContract(c)

	contracts := m.Contracts()
	if len(contracts) != 1 {
		t.Errorf("expected 1 registered contract, got %d", len(contracts))
	}
	if contracts[0] != "auth-session-v2" {
		t.Errorf("expected 'auth-session-v2', got %q", contracts[0])
	}

	// All metrics within bounds → no breaches
	metrics := map[string]float64{
		"auth_token_refresh_success_rate":     0.9995,
		"auth_token_refresh_p99_latency_ms":   150,
		"concurrent_session_limit_errors":     0,
	}
	breaches := m.Evaluate(metrics)
	if len(breaches) != 0 {
		t.Errorf("expected 0 breaches (all within bounds), got %d", len(breaches))
	}
}

func TestMonitor_Evaluate_BreachDetected(t *testing.T) {
	c, _ := ParseContract(validContractYAML)
	m := NewMonitor()
	m.RegisterContract(c)

	metrics := map[string]float64{
		"auth_token_refresh_success_rate":     0.99,
		"auth_token_refresh_p99_latency_ms":   150,
		"concurrent_session_limit_errors":     0,
	}
	breaches := m.Evaluate(metrics)
	if len(breaches) != 1 {
		t.Fatalf("expected 1 breach (success_rate 0.99 < 0.999), got %d", len(breaches))
	}
	b := breaches[0]
	if b.ContractName != "auth-session-v2" {
		t.Errorf("expected contract 'auth-session-v2', got %q", b.ContractName)
	}
	if len(b.FailedChecks) != 1 {
		t.Errorf("expected 1 failed check, got %d", len(b.FailedChecks))
	}
	if !b.ShouldRollback {
		t.Error("expected ShouldRollback=true")
	}
	if !b.ShouldNotify {
		t.Error("expected ShouldNotify=true")
	}
}

func TestMonitor_EvaluateOne_Registered(t *testing.T) {
	c, _ := ParseContract(validContractYAML)
	m := NewMonitor()
	m.RegisterContract(c)

	breach := m.EvaluateOne("auth-session-v2", map[string]float64{
		"auth_token_refresh_success_rate": 0.99,
		"auth_token_refresh_p99_latency_ms": 150,
		"concurrent_session_limit_errors": 0,
	})
	if breach == nil {
		t.Fatal("expected a breach")
	}
}

func TestMonitor_EvaluateOne_NotRegistered(t *testing.T) {
	m := NewMonitor()
	breach := m.EvaluateOne("non-existent", nil)
	if breach != nil {
		t.Error("expected nil for unregistered contract")
	}
}

func TestMonitor_EvaluateOne_AllPass(t *testing.T) {
	c, _ := ParseContract(validContractYAML)
	m := NewMonitor()
	m.RegisterContract(c)

	breach := m.EvaluateOne("auth-session-v2", map[string]float64{
		"auth_token_refresh_success_rate":     0.9995,
		"auth_token_refresh_p99_latency_ms":   150,
		"concurrent_session_limit_errors":     0,
	})
	if breach != nil {
		t.Errorf("expected nil (all pass), got breach: %s", breach.Error())
	}
}

func TestMonitor_Unregister(t *testing.T) {
	c, _ := ParseContract(validContractYAML)
	m := NewMonitor()
	m.RegisterContract(c)
	m.UnregisterContract("auth-session-v2")
	if len(m.Contracts()) != 0 {
		t.Error("expected no contracts after unregister")
	}
}

// =============================================================================
// Breach.Error
// =============================================================================

func TestBreachError(t *testing.T) {
	b := Breach{
		ContractName: "test-contract",
		FailedChecks: []CheckResult{{Passed: false}},
		ShouldRollback: true,
	}
	err := b.Error()
	if !strings.Contains(err, "test-contract") {
		t.Errorf("expected contract name in error: %s", err)
	}
	if !strings.Contains(err, "rollback=true") {
		t.Errorf("expected rollback info in error: %s", err)
	}
}

// =============================================================================
// Drift detection
// =============================================================================

func TestDetectDrift_NoDrift(t *testing.T) {
	baseline := map[string]float64{"latency_p99": 100, "success_rate": 0.999}
	current := map[string]float64{"latency_p99": 100, "success_rate": 0.999}
	reports := DetectDrift(baseline, current, 50.0)
	for _, r := range reports {
		if r.Exceeds {
			t.Errorf("unexpected drift for %s: %.1f%%", r.Metric, r.DriftPct)
		}
	}
}

func TestDetectDrift_ExceedsThreshold(t *testing.T) {
	baseline := map[string]float64{"latency_p99": 100}
	current := map[string]float64{"latency_p99": 200}
	reports := DetectDrift(baseline, current, 50.0)
	if len(reports) != 1 {
		t.Fatalf("expected 1 report, got %d", len(reports))
	}
	if !reports[0].Exceeds {
		t.Error("expected drift to exceed 50% threshold (100→200 = +100%)")
	}
	if math.Abs(reports[0].DriftPct-100.0) > 0.01 {
		t.Errorf("expected 100%% drift, got %.1f%%", reports[0].DriftPct)
	}
}

func TestDetectDrift_WithinThreshold(t *testing.T) {
	baseline := map[string]float64{"latency_p99": 100}
	current := map[string]float64{"latency_p99": 120}
	reports := DetectDrift(baseline, current, 50.0)
	if len(reports) != 1 {
		t.Fatalf("expected 1 report, got %d", len(reports))
	}
	if reports[0].Exceeds {
		t.Errorf("expected no exceed (120/100 = +20%% < 50%% threshold)")
	}
}

func TestDetectDrift_NegativeDrift(t *testing.T) {
	baseline := map[string]float64{"latency_p99": 100}
	current := map[string]float64{"latency_p99": 40}
	reports := DetectDrift(baseline, current, 50.0)
	if len(reports) != 1 {
		t.Fatalf("expected 1 report, got %d", len(reports))
	}
	if !reports[0].Exceeds {
		t.Errorf("expected exceed (100→40 = -60%%, threshold 50%%)")
	}
}

func TestDetectDrift_FromZero(t *testing.T) {
	baseline := map[string]float64{"new_errors": 0}
	current := map[string]float64{"new_errors": 10}
	reports := DetectDrift(baseline, current, 50.0)
	if len(reports) != 1 {
		t.Fatalf("expected 1 report, got %d", len(reports))
	}
	if !reports[0].Exceeds {
		t.Error("expected exceed (0→10 = infinite drift)")
	}
}

func TestDetectDrift_SkipsMissingMetrics(t *testing.T) {
	baseline := map[string]float64{"m1": 100, "m2": 200}
	current := map[string]float64{"m1": 150}
	reports := DetectDrift(baseline, current, 50.0)
	if len(reports) != 1 {
		t.Errorf("expected only 1 report (m2 missing from current), got %d", len(reports))
	}
}

// =============================================================================
// Shadow auto-rollback triggers
// =============================================================================

func TestShadowAutoRollback_ErrorRateExceeded(t *testing.T) {
	reason := ShadowAutoRollbackTriggers(0.9999, 0.9985, 100, 100, 0, 0)
	if reason == "" {
		t.Error("expected rollback trigger (error rate delta >0.1%)")
	}
	if !strings.Contains(reason, "error rate") {
		t.Errorf("expected 'error rate' in reason: %s", reason)
	}
}

func TestShadowAutoRollback_P99LatencyExceeded(t *testing.T) {
	reason := ShadowAutoRollbackTriggers(0.999, 0.999, 100, 130, 0, 0)
	if reason == "" {
		t.Error("expected rollback trigger (P99 130 > 120 = 20%%)")
	}
	if !strings.Contains(reason, "P99 latency") {
		t.Errorf("expected 'P99 latency' in reason: %s", reason)
	}
}

func TestShadowAutoRollback_NewErrors(t *testing.T) {
	reason := ShadowAutoRollbackTriggers(0.999, 0.999, 100, 100, 3, 0)
	if reason == "" {
		t.Error("expected rollback trigger (new error types)")
	}
	if !strings.Contains(reason, "new error") {
		t.Errorf("expected 'new error' in reason: %s", reason)
	}
}

func TestShadowAutoRollback_MemoryGrowth(t *testing.T) {
	reason := ShadowAutoRollbackTriggers(0.999, 0.999, 100, 100, 0, 15.0)
	if reason == "" {
		t.Error("expected rollback trigger (15% memory growth > 10%)")
	}
	if !strings.Contains(reason, "memory") {
		t.Errorf("expected 'memory' in reason: %s", reason)
	}
}

func TestShadowAutoRollback_AllPass(t *testing.T) {
	reason := ShadowAutoRollbackTriggers(0.999, 0.999, 100, 105, 0, 3.0)
	if reason != "" {
		t.Errorf("expected no trigger (all within bounds), got: %s", reason)
	}
}

// =============================================================================
// YAML spec example
// =============================================================================

func TestParseContract_SpecExample(t *testing.T) {
	// Directly from spec/production-verification.md
	yaml := []byte(`
contract:
  name: auth-session-v2
  agent: agent-uuid
  merge_commit: abc123
  assertions:
    - metric: auth_token_refresh_success_rate
      operator: gte
      value: 0.999
      window: 1h
    - metric: auth_token_refresh_p99_latency_ms
      operator: lte
      value: 200
      window: 1h
    - metric: concurrent_session_limit_errors
      operator: eq
      value: 0
      window: 24h
  breach_action: rollback_and_notify
`)
	c, err := ParseContract(yaml)
	if err != nil {
		t.Fatalf("ParseContract(spec example): %v", err)
	}
	if c.Contract.Name != "auth-session-v2" {
		t.Errorf("expected name 'auth-session-v2', got %q", c.Contract.Name)
	}
	if len(c.Contract.Assertions) != 3 {
		t.Errorf("expected 3 assertions, got %d", len(c.Contract.Assertions))
	}
}

// =============================================================================
// Breach.TotalChecks
// =============================================================================

func TestBreach_TotalChecks(t *testing.T) {
	b := Breach{FailedChecks: []CheckResult{{}, {}, {}}}
	if b.TotalChecks() != 3 {
		t.Errorf("expected 3, got %d", b.TotalChecks())
	}
}
