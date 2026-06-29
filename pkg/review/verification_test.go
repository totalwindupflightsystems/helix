package review

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Test helpers
// =============================================================================

// mockTestRunner returns preset results for test IDs.
type mockTestRunner struct {
	results map[string]bool // testID → passed
	errs    map[string]error
	mu      sync.Mutex
	calls   int
}

func newMockTestRunner() *mockTestRunner {
	return &mockTestRunner{
		results: make(map[string]bool),
		errs:    make(map[string]error),
	}
}

func (m *mockTestRunner) RunTest(_ context.Context, testID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if err, ok := m.errs[testID]; ok {
		return false, err
	}
	return m.results[testID], nil
}

func sampleBundle() *EvidenceBundle {
	return &EvidenceBundle{
		ReviewID:  "rev-001",
		PRURL:     "https://forgejo/pr/1",
		Timestamp: time.Now().UTC(),
		Formation: Formation{
			Primary:     ModelInfo{Model: "gpt-5.5", Provider: "openai"},
			Adversarial: ModelInfo{Model: "deepseek-v4", Provider: "deepseek"},
		},
		Findings: []Finding{},
	}
}

func finding(model, severity, evidence string) Finding {
	return Finding{
		Model:    model,
		Severity: severity,
		Type:     "bug",
		File:     "main.go",
		Line:     42,
		Evidence: evidence,
	}
}

// =============================================================================
// extractTestID
// =============================================================================

func TestExtractTestID_TestRunID(t *testing.T) {
	id := extractTestID("test_run_id: abc-123, output: 'FAIL'")
	assert.Equal(t, "abc-123", id)
}

func TestExtractTestID_TestPrefix(t *testing.T) {
	id := extractTestID("test:TestFoo")
	assert.Equal(t, "TestFoo", id)
}

func TestExtractTestID_FAILPrefix(t *testing.T) {
	id := extractTestID("FAIL: TestConcurrentAccess")
	assert.Equal(t, "TestConcurrentAccess", id)
}

func TestExtractTestID_Empty(t *testing.T) {
	id := extractTestID("")
	assert.Equal(t, "", id)
}

func TestExtractTestID_NoMatch(t *testing.T) {
	id := extractTestID("some random evidence without test id")
	assert.Equal(t, "", id)
}

func TestExtractTestID_MultipleFields(t *testing.T) {
	id := extractTestID("test_run_id: uuid-42, output: 'panic: nil pointer'")
	assert.Equal(t, "uuid-42", id)
}

// =============================================================================
// classifyFinding
// =============================================================================

func TestClassifyFinding_Testable(t *testing.T) {
	f := finding("adversarial", "high", "test_run_id: t-001")
	class, testID := classifyFinding(f)
	assert.Equal(t, ClassTestable, class)
	assert.Equal(t, "t-001", testID)
}

func TestClassifyFinding_Mitigation(t *testing.T) {
	f := Finding{Evidence: "", Mitigation: "Add nil check before dereferencing the pointer"}
	class, _ := classifyFinding(f)
	assert.Equal(t, ClassMitigation, class)
}

func TestClassifyFinding_Unverifiable(t *testing.T) {
	f := finding("adversarial", "low", "seems risky")
	class, _ := classifyFinding(f)
	assert.Equal(t, ClassUnverifiable, class)
}

func TestClassifyFinding_TestableTakesPriority(t *testing.T) {
	f := Finding{
		Evidence:   "test_run_id: t-002",
		Mitigation: "Fix the race condition",
	}
	class, testID := classifyFinding(f)
	assert.Equal(t, ClassTestable, class)
	assert.Equal(t, "t-002", testID)
}

// =============================================================================
// VerifyFindings — core scenarios
// =============================================================================

func TestVerifyFindings_NilBundle(t *testing.T) {
	v := NewEvidenceVerifier()
	_, err := v.VerifyFindings(context.Background(), nil)
	assert.Error(t, err)
}

func TestVerifyFindings_EmptyBundle(t *testing.T) {
	v := NewEvidenceVerifier()
	bundle := sampleBundle()
	report, err := v.VerifyFindings(context.Background(), bundle)
	require.NoError(t, err)
	assert.True(t, report.AllVerified)
	assert.Equal(t, 0, report.TotalFindings)
}

func TestVerifyFindings_TestVerified(t *testing.T) {
	runner := newMockTestRunner()
	runner.results["t-001"] = false // test fails → finding confirmed

	v := NewEvidenceVerifier(WithTestRunner(runner))
	bundle := sampleBundle()
	bundle.Findings = []Finding{
		finding("adversarial", "high", "test_run_id: t-001"),
	}

	report, err := v.VerifyFindings(context.Background(), bundle)
	require.NoError(t, err)
	assert.Equal(t, 1, report.Verified)
	assert.Equal(t, 0, report.FalsePositives)
	assert.True(t, report.AllVerified)
}

func TestVerifyFindings_FalsePositive(t *testing.T) {
	runner := newMockTestRunner()
	runner.results["t-001"] = true // test passes → false positive

	v := NewEvidenceVerifier(WithTestRunner(runner))
	bundle := sampleBundle()
	bundle.Findings = []Finding{
		finding("adversarial", "high", "test_run_id: t-001"),
	}

	report, err := v.VerifyFindings(context.Background(), bundle)
	require.NoError(t, err)
	assert.Equal(t, 0, report.Verified)
	assert.Equal(t, 1, report.FalsePositives)
	assert.False(t, report.AllVerified)
}

func TestVerifyFindings_TestError(t *testing.T) {
	runner := newMockTestRunner()
	runner.errs["t-001"] = fmt.Errorf("compile error")

	v := NewEvidenceVerifier(WithTestRunner(runner))
	bundle := sampleBundle()
	bundle.Findings = []Finding{
		finding("adversarial", "high", "test_run_id: t-001"),
	}

	report, err := v.VerifyFindings(context.Background(), bundle)
	require.NoError(t, err)
	assert.Equal(t, 1, report.Unverifiable)
	assert.True(t, report.AllVerified, "unverifiable doesn't block AllVerified")
}

func TestVerifyFindings_UnverifiableNoEvidence(t *testing.T) {
	v := NewEvidenceVerifier()
	bundle := sampleBundle()
	bundle.Findings = []Finding{
		finding("adversarial", "low", "looks suspicious"),
	}

	report, err := v.VerifyFindings(context.Background(), bundle)
	require.NoError(t, err)
	assert.Equal(t, 1, report.Unverifiable)
	assert.True(t, report.AllVerified)
}

func TestVerifyFindings_MitigationVerified(t *testing.T) {
	v := NewEvidenceVerifier()
	bundle := sampleBundle()
	bundle.Findings = []Finding{
		{
			Model:      "primary",
			Severity:   "medium",
			Evidence:   "",
			Mitigation: "Add input validation in the handler to reject empty payloads",
		},
	}

	report, err := v.VerifyFindings(context.Background(), bundle)
	require.NoError(t, err)
	assert.Equal(t, 1, report.Verified)
}

func TestVerifyFindings_MitigationTooShort(t *testing.T) {
	v := NewEvidenceVerifier()
	bundle := sampleBundle()
	bundle.Findings = []Finding{
		{Mitigation: "fix it"},
	}

	report, err := v.VerifyFindings(context.Background(), bundle)
	require.NoError(t, err)
	assert.Equal(t, 1, report.Unverifiable)
}

func TestVerifyFindings_MixedResults(t *testing.T) {
	runner := newMockTestRunner()
	runner.results["t-pass"] = true   // false positive
	runner.results["t-fail"] = false  // verified

	v := NewEvidenceVerifier(WithTestRunner(runner))
	bundle := sampleBundle()
	bundle.Findings = []Finding{
		finding("adv", "high", "test_run_id: t-fail"),    // verified
		finding("adv", "low", "test_run_id: t-pass"),     // false positive
		finding("adv", "medium", "no test"),              // unverifiable
		{Mitigation: "Add proper error handling here"},   // verified (mitigation)
	}

	report, err := v.VerifyFindings(context.Background(), bundle)
	require.NoError(t, err)
	assert.Equal(t, 2, report.Verified)
	assert.Equal(t, 1, report.FalsePositives)
	assert.Equal(t, 1, report.Unverifiable)
	assert.False(t, report.AllVerified, "false positive should block AllVerified")
}

// =============================================================================
// Concurrent verification
// =============================================================================

func TestVerifyFindings_Concurrent(t *testing.T) {
	runner := newMockTestRunner()
	for i := 0; i < 10; i++ {
		runner.results[fmt.Sprintf("t-%d", i)] = false // all verified
	}

	v := NewEvidenceVerifier(WithTestRunner(runner))
	bundle := sampleBundle()
	for i := 0; i < 10; i++ {
		bundle.Findings = append(bundle.Findings,
			finding("adv", "high", fmt.Sprintf("test_run_id: t-%d", i)))
	}

	report, err := v.VerifyFindings(context.Background(), bundle)
	require.NoError(t, err)
	assert.Equal(t, 10, report.Verified)
	assert.Equal(t, 10, runner.calls)
}

// =============================================================================
// FP Tracker integration
// =============================================================================

func TestVerifyFindings_FPTrackerIntegration(t *testing.T) {
	runner := newMockTestRunner()
	runner.results["t-001"] = true // false positive

	fp := NewFPTracker()
	v := NewEvidenceVerifier(WithTestRunner(runner), WithVerifyFPTracker(fp))
	bundle := sampleBundle()
	bundle.Findings = []Finding{
		finding("deepseek-v4", "high", "test_run_id: t-001"),
	}

	_, err := v.VerifyFindings(context.Background(), bundle)
	require.NoError(t, err)

	// The model should have a dismissal recorded
	count := fp.DismissalCount("deepseek-v4")
	assert.Equal(t, 1, count)
}

// =============================================================================
// Report helpers
// =============================================================================

func TestPassRate_AllVerified(t *testing.T) {
	runner := newMockTestRunner()
	runner.results["t-1"] = false
	runner.results["t-2"] = false

	v := NewEvidenceVerifier(WithTestRunner(runner))
	bundle := sampleBundle()
	bundle.Findings = []Finding{
		finding("adv", "high", "test_run_id: t-1"),
		finding("adv", "high", "test_run_id: t-2"),
	}

	report, _ := v.VerifyFindings(context.Background(), bundle)
	assert.InDelta(t, 1.0, report.PassRate(), 0.001)
}

func TestPassRate_Mixed(t *testing.T) {
	runner := newMockTestRunner()
	runner.results["t-1"] = false // verified
	runner.results["t-2"] = true  // false positive

	v := NewEvidenceVerifier(WithTestRunner(runner))
	bundle := sampleBundle()
	bundle.Findings = []Finding{
		finding("adv", "high", "test_run_id: t-1"),
		finding("adv", "high", "test_run_id: t-2"),
	}

	report, _ := v.VerifyFindings(context.Background(), bundle)
	assert.InDelta(t, 0.5, report.PassRate(), 0.001)
}

func TestPassRate_NothingTestable(t *testing.T) {
	v := NewEvidenceVerifier()
	bundle := sampleBundle()
	bundle.Findings = []Finding{
		finding("adv", "low", "no test id"),
	}
	report, _ := v.VerifyFindings(context.Background(), bundle)
	assert.InDelta(t, 1.0, report.PassRate(), 0.001)
}

func TestFalsePositiveRate(t *testing.T) {
	runner := newMockTestRunner()
	runner.results["t-1"] = false // verified
	runner.results["t-2"] = true  // fp
	runner.results["t-3"] = true  // fp

	v := NewEvidenceVerifier(WithTestRunner(runner))
	bundle := sampleBundle()
	bundle.Findings = []Finding{
		finding("adv", "high", "test_run_id: t-1"),
		finding("adv", "high", "test_run_id: t-2"),
		finding("adv", "high", "test_run_id: t-3"),
	}

	report, _ := v.VerifyFindings(context.Background(), bundle)
	assert.InDelta(t, 2.0/3.0, report.FalsePositiveRate(), 0.01)
}

func TestVerifiedBySeverity(t *testing.T) {
	runner := newMockTestRunner()
	runner.results["t-1"] = false
	runner.results["t-2"] = false

	v := NewEvidenceVerifier(WithTestRunner(runner))
	bundle := sampleBundle()
	bundle.Findings = []Finding{
		finding("adv", "critical", "test_run_id: t-1"),
		finding("adv", "high", "test_run_id: t-2"),
	}

	report, _ := v.VerifyFindings(context.Background(), bundle)
	bySev := report.VerifiedBySeverity(bundle.Findings)
	assert.Equal(t, 1, bySev["critical"])
	assert.Equal(t, 1, bySev["high"])
}

func TestCriticalVerified(t *testing.T) {
	runner := newMockTestRunner()
	runner.results["t-1"] = false
	runner.results["t-2"] = false

	v := NewEvidenceVerifier(WithTestRunner(runner))
	bundle := sampleBundle()
	bundle.Findings = []Finding{
		finding("adv", "critical", "test_run_id: t-1"),
		finding("adv", "high", "test_run_id: t-2"),
		finding("adv", "critical", "test_run_id: t-3"),
	}
	runner.results["t-3"] = false

	report, _ := v.VerifyFindings(context.Background(), bundle)
	assert.Equal(t, 2, report.CriticalVerified(bundle.Findings))
}

func TestVerificationReport_Summary(t *testing.T) {
	runner := newMockTestRunner()
	runner.results["t-1"] = false
	runner.results["t-2"] = true

	v := NewEvidenceVerifier(WithTestRunner(runner))
	bundle := sampleBundle()
	bundle.Findings = []Finding{
		finding("adv", "high", "test_run_id: t-1"),
		finding("adv", "high", "test_run_id: t-2"),
		finding("adv", "low", "no test"),
	}

	report, _ := v.VerifyFindings(context.Background(), bundle)
	s := report.Summary()
	assert.Contains(t, s, "verified")
	assert.Contains(t, s, "false_positive")
}

// =============================================================================
// NoopTestRunner
// =============================================================================

func TestNoopTestRunner_ReturnsError(t *testing.T) {
	runner := &NoopTestRunner{}
	_, err := runner.RunTest(context.Background(), "any-test")
	assert.Error(t, err)
}

// =============================================================================
// Timeout handling
// =============================================================================

func TestVerifyFindings_ContextTimeout(t *testing.T) {
	slowRunner := &slowTestRunner{delay: 200 * time.Millisecond}
	v := NewEvidenceVerifier(WithTestRunner(slowRunner), WithVerifyTimeout(50*time.Millisecond))
	bundle := sampleBundle()
	bundle.Findings = []Finding{
		finding("adv", "high", "test_run_id: t-slow"),
	}

	report, err := v.VerifyFindings(context.Background(), bundle)
	require.NoError(t, err)
	// Should be unverifiable due to timeout
	assert.Equal(t, 1, report.Unverifiable)
}

type slowTestRunner struct {
	delay time.Duration
}

func (s *slowTestRunner) RunTest(ctx context.Context, _ string) (bool, error) {
	select {
	case <-time.After(s.delay):
		return false, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// =============================================================================
// AllVerified edge cases
// =============================================================================

func TestAllVerified_WithFalsePositive(t *testing.T) {
	runner := newMockTestRunner()
	runner.results["t-1"] = true // false positive

	v := NewEvidenceVerifier(WithTestRunner(runner))
	bundle := sampleBundle()
	bundle.Findings = []Finding{
		finding("adv", "high", "test_run_id: t-1"),
	}

	report, _ := v.VerifyFindings(context.Background(), bundle)
	assert.False(t, report.AllVerified)
}

func TestAllVerified_WithUnverifiable(t *testing.T) {
	v := NewEvidenceVerifier()
	bundle := sampleBundle()
	bundle.Findings = []Finding{
		finding("adv", "low", "no evidence"),
	}

	report, _ := v.VerifyFindings(context.Background(), bundle)
	assert.True(t, report.AllVerified, "unverifiable should not block AllVerified")
}

func TestAllVerified_AllVerified(t *testing.T) {
	runner := newMockTestRunner()
	runner.results["t-1"] = false

	v := NewEvidenceVerifier(WithTestRunner(runner))
	bundle := sampleBundle()
	bundle.Findings = []Finding{
		finding("adv", "high", "test_run_id: t-1"),
	}

	report, _ := v.VerifyFindings(context.Background(), bundle)
	assert.True(t, report.AllVerified)
}
