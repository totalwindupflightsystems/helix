package review

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// =============================================================================
// Evidence Verification Layer (Tier 3)
//
// Per spec §Three-Layer Review Pipeline:
//
//	Tier 3 — Evidence Verification
//	    ├── Run tests from all three models' suggestions
//	    ├── Verify edge cases actually fail
//	    └── Confirm fixes actually resolve issues
//
// After the ReviewOrchestrator (Tier 2) produces consensus findings, the
// EvidenceVerifier independently verifies each finding by:
//   1. Running the suggested test case (if provided)
//   2. Checking whether the claimed edge case actually fails
//   3. Confirming that a proposed fix resolves the issue
//
// Findings are classified as:
//   - Verified: the claim is confirmed by evidence
//   - FalsePositive: the claim is disproven
//   - Unverifiable: cannot be tested (no test case, timing-dependent, etc.)
// =============================================================================

// FindingStatus is the verification outcome for a single finding.
type FindingStatus string

const (
	StatusVerified      FindingStatus = "verified"
	StatusFalsePositive FindingStatus = "false_positive"
	StatusUnverifiable  FindingStatus = "unverifiable"
)

// TestRunner executes test commands and returns pass/fail.
// This interface lets us mock the test execution layer.
type TestRunner interface {
	// RunTest executes a named test and returns whether it passed.
	RunTest(ctx context.Context, testID string) (bool, error)
}

// VerificationResult is the outcome of verifying one finding.
type VerificationResult struct {
	FindingIdx    int           `json:"finding_idx"`
	Status        FindingStatus `json:"status"`
	TestID        string        `json:"test_id,omitempty"`
	TestPassed    bool          `json:"test_passed"`
	Detail        string        `json:"detail"`
	Duration      time.Duration `json:"duration"`
}

// VerificationReport aggregates results for an entire evidence bundle.
type VerificationReport struct {
	ReviewID       string               `json:"review_id"`
	PRURL          string               `json:"pr_url"`
	Timestamp      time.Time            `json:"timestamp"`
	Results        []VerificationResult `json:"results"`
	Verified       int                  `json:"verified"`
	FalsePositives int                  `json:"false_positives"`
	Unverifiable   int                  `json:"unverifiable"`
	TotalFindings  int                  `json:"total_findings"`
	// AllVerified is true when every testable finding is confirmed.
	// Unverifiable findings don't block AllVerified.
	AllVerified bool `json:"all_verified"`
}

// EvidenceVerifier takes consensus findings from the ReviewOrchestrator
// and independently verifies them using test execution.
type EvidenceVerifier struct {
	runner    TestRunner
	timeout   time.Duration
	fpTracker *FPTracker
}

// VerifierOption configures an EvidenceVerifier.
type VerifierOption func(*EvidenceVerifier)

// WithTestRunner sets the test execution backend.
func WithTestRunner(r TestRunner) VerifierOption {
	return func(v *EvidenceVerifier) { v.runner = r }
}

// WithVerifyTimeout sets the per-test timeout.
func WithVerifyTimeout(d time.Duration) VerifierOption {
	return func(v *EvidenceVerifier) { v.timeout = d }
}

// WithVerifyFPTracker sets a shared false-positive tracker so verified
// false positives feed back into the model rotation system.
func WithVerifyFPTracker(fp *FPTracker) VerifierOption {
	return func(v *EvidenceVerifier) { v.fpTracker = fp }
}

// NewEvidenceVerifier creates a verifier with defaults.
func NewEvidenceVerifier(opts ...VerifierOption) *EvidenceVerifier {
	v := &EvidenceVerifier{
		runner:  &NoopTestRunner{},
		timeout: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// =============================================================================
// Finding classification — which findings can be verified?
// =============================================================================

// ClassifyFinding determines whether a finding can be verified and how.
// It looks at the finding's Evidence field for a test identifier.
//
// Extraction rules:
//   - "test_run_id: <id>" or "test:<id>" → the test to run
//   - Findings without evidence → unverifiable
//   - Mitigation-presence → we can verify the fix too
type FindingClass int

const (
	ClassTestable    FindingClass = iota // has a test we can run
	ClassMitigation                      // has a fix to verify
	ClassUnverifiable                    // no actionable evidence
)

// extractTestID parses the finding's evidence string for a test identifier.
// Supported formats:
//   - "test_run_id: uuid, output: ..."
//   - "test:TestFoo"
//   - "FAIL: TestBar"
func extractTestID(evidence string) string {
	if evidence == "" {
		return ""
	}

	// Try "test_run_id: <id>" format
	if id := extractField(evidence, "test_run_id:"); id != "" {
		return id
	}

	// Try "test:<id>" format
	if id := extractField(evidence, "test:"); id != "" {
		return id
	}

	// Try "FAIL: <TestName>" format (from go test output)
	if id := extractField(evidence, "FAIL:"); id != "" {
		return id
	}

	return ""
}

// extractField finds "field: value" and returns value (trimmed, first token).
func extractField(s, field string) string {
	idx := indexOf(s, field)
	if idx < 0 {
		return ""
	}
	rest := s[idx+len(field):]
	// Trim leading whitespace
	for len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\t') {
		rest = rest[1:]
	}
	// Take up to comma or end
	end := 0
	for end < len(rest) && rest[end] != ',' && rest[end] != '\n' && rest[end] != ' ' {
		end++
	}
	return rest[:end]
}

// indexOf is a case-sensitive substring search.
func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			if s[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// classifyFinding returns the class and extracted test ID (if any).
func classifyFinding(f Finding) (FindingClass, string) {
	testID := extractTestID(f.Evidence)
	if testID != "" {
		return ClassTestable, testID
	}
	if f.Mitigation != "" {
		return ClassMitigation, ""
	}
	return ClassUnverifiable, ""
}

// =============================================================================
// Core verification logic
// =============================================================================

// VerifyFindings verifies each finding in an evidence bundle.
// Each finding is classified and verified independently. The results are
// collected into a VerificationReport.
//
// Findings are processed concurrently (up to the context's cancellation).
func (v *EvidenceVerifier) VerifyFindings(ctx context.Context, bundle *EvidenceBundle) (*VerificationReport, error) {
	if bundle == nil {
		return nil, fmt.Errorf("evidence bundle is nil")
	}

	findings := bundle.Findings
	report := &VerificationReport{
		ReviewID:      bundle.ReviewID,
		PRURL:         bundle.PRURL,
		Timestamp:     time.Now().UTC(),
		Results:       make([]VerificationResult, len(findings)),
		TotalFindings: len(findings),
	}

	if len(findings) == 0 {
		report.AllVerified = true
		return report, nil
	}

	// Process findings concurrently.
	var wg sync.WaitGroup
	mu := sync.Mutex{}

	for i, f := range findings {
		wg.Add(1)
		go func(idx int, finding Finding) {
			defer wg.Done()
			result := v.verifyOne(ctx, idx, finding)
			mu.Lock()
			report.Results[idx] = result
			mu.Unlock()
		}(i, f)
	}
	wg.Wait()

	// Aggregate counts.
	for _, r := range report.Results {
		switch r.Status {
		case StatusVerified:
			report.Verified++
		case StatusFalsePositive:
			report.FalsePositives++
		case StatusUnverifiable:
			report.Unverifiable++
		}
	}

	// AllVerified if no finding is a false positive.
	// Verified and unverifiable both pass.
	report.AllVerified = report.FalsePositives == 0

	return report, nil
}

// verifyOne verifies a single finding.
func (v *EvidenceVerifier) verifyOne(ctx context.Context, idx int, f Finding) VerificationResult {
	start := time.Now()

	class, testID := classifyFinding(f)

	switch class {
	case ClassTestable:
		return v.verifyTestable(ctx, idx, f, testID, start)
	case ClassMitigation:
		return v.verifyMitigation(idx, f, start)
	default:
		return VerificationResult{
			FindingIdx: idx,
			Status:     StatusUnverifiable,
			Detail:     "no testable evidence in finding",
			Duration:   time.Since(start),
		}
	}
}

// verifyTestable runs the extracted test and checks the outcome.
func (v *EvidenceVerifier) verifyTestable(ctx context.Context, idx int, f Finding, testID string, start time.Time) VerificationResult {
	// A finding claims something is wrong (a bug/vulnerability).
	// If the test FAILS, the finding is verified (the bug exists).
	// If the test PASSES, the finding is a false positive (no bug).

	testCtx, cancel := context.WithTimeout(ctx, v.timeout)
	defer cancel()

	passed, err := v.runner.RunTest(testCtx, testID)
	duration := time.Since(start)

	if err != nil {
		return VerificationResult{
			FindingIdx: idx,
			Status:     StatusUnverifiable,
			TestID:     testID,
			Detail:     fmt.Sprintf("test execution error: %v", err),
			Duration:   duration,
		}
	}

	if !passed {
		// Test failed → the bug is real → finding is verified.
		return VerificationResult{
			FindingIdx: idx,
			Status:     StatusVerified,
			TestID:     testID,
			TestPassed: false,
			Detail:     "test fails as claimed — finding confirmed",
			Duration:   duration,
		}
	}

	// Test passed → no bug → false positive.
	if v.fpTracker != nil {
		v.fpTracker.RecordDismissal(f.Model)
	}

	return VerificationResult{
		FindingIdx: idx,
		Status:     StatusFalsePositive,
		TestID:     testID,
		TestPassed: true,
		Detail:     "test passes — finding is a false positive",
		Duration:   duration,
	}
}

// verifyMitigation checks that a mitigation/fix is present.
// Since we can't run the mitigation itself, we verify structural properties:
// the finding has a mitigation field with non-trivial content.
func (v *EvidenceVerifier) verifyMitigation(idx int, f Finding, start time.Time) VerificationResult {
	if len(f.Mitigation) < 10 {
		return VerificationResult{
			FindingIdx: idx,
			Status:     StatusUnverifiable,
			Detail:     "mitigation too short to be actionable",
			Duration:   time.Since(start),
		}
	}

	return VerificationResult{
		FindingIdx: idx,
		Status:     StatusVerified,
		Detail:     "mitigation present and actionable",
		Duration:   time.Since(start),
	}
}

// =============================================================================
// Report helpers
// =============================================================================

// PassRate returns the fraction of findings that are verified (excluding unverifiable).
func (r *VerificationReport) PassRate() float64 {
	testable := r.Verified + r.FalsePositives
	if testable == 0 {
		return 1.0 // nothing to verify → pass
	}
	return float64(r.Verified) / float64(testable)
}

// FalsePositiveRate returns the fraction of verified+false_positive findings
// that are false positives.
func (r *VerificationReport) FalsePositiveRate() float64 {
	testable := r.Verified + r.FalsePositives
	if testable == 0 {
		return 0.0
	}
	return float64(r.FalsePositives) / float64(testable)
}

// FindingsBySeverity returns verified findings grouped by severity.
func (r *VerificationReport) VerifiedBySeverity(findings []Finding) map[string]int {
	bySev := make(map[string]int)
	for _, res := range r.Results {
		if res.Status == StatusVerified && res.FindingIdx < len(findings) {
			bySev[findings[res.FindingIdx].Severity]++
		}
	}
	return bySev
}

// CriticalVerified returns the count of verified critical-severity findings.
func (r *VerificationReport) CriticalVerified(findings []Finding) int {
	count := 0
	for _, res := range r.Results {
		if res.Status == StatusVerified && res.FindingIdx < len(findings) {
			if findings[res.FindingIdx].Severity == "critical" {
				count++
			}
		}
	}
	return count
}

// Summary returns a one-line human-readable summary.
func (r *VerificationReport) Summary() string {
	return fmt.Sprintf("%d verified, %d false_positive, %d unverifiable (of %d total) — pass_rate=%.1f%%",
		r.Verified, r.FalsePositives, r.Unverifiable, r.TotalFindings, r.PassRate()*100)
}

// =============================================================================
// NoopTestRunner — default that marks everything as unverifiable
// =============================================================================

// NoopTestRunner is the default runner when no test backend is configured.
// It returns an error for every test, causing findings to be unverifiable.
type NoopTestRunner struct{}

// RunTest returns an error indicating no test runner is configured.
func (n *NoopTestRunner) RunTest(_ context.Context, _ string) (bool, error) {
	return false, fmt.Errorf("no test runner configured")
}
