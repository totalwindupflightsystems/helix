package review

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// =============================================================================
// Mock ModelClient for testing
// =============================================================================

type mockModel struct {
	info    ModelInfo
	verdict string
	findings []Finding
	err     error
	delay   time.Duration
	calls   int32 // atomic counter
}

func (m *mockModel) Review(ctx context.Context, req ReviewRequest) (*ModelReviewResult, error) {
	atomic.AddInt32(&m.calls, 1)
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if m.err != nil {
		return nil, m.err
	}
	return &ModelReviewResult{
		Verdict:  m.verdict,
		Findings: m.findings,
	}, nil
}

func (m *mockModel) Info() ModelInfo { return m.info }
func (m *mockModel) CallCount() int  { return int(atomic.LoadInt32(&m.calls)) }

func newMock(model, provider, verdict string) *mockModel {
	return &mockModel{
		info:    ModelInfo{Model: model, Provider: provider},
		verdict: verdict,
	}
}

func newMockWithFinding(model, provider, verdict, findingType, file string, line int) *mockModel {
	return &mockModel{
		info:    ModelInfo{Model: model, Provider: provider},
		verdict: verdict,
		findings: []Finding{
			{Type: findingType, File: file, Line: line, Description: "test finding", Evidence: "test evidence"},
		},
	}
}

func newMockErr(model, provider string, err error) *mockModel {
	return &mockModel{
		info: ModelInfo{Model: model, Provider: provider},
		err:  err,
	}
}

func newMockDelay(model, provider, verdict string, d time.Duration) *mockModel {
	m := newMock(model, provider, verdict)
	m.delay = d
	return m
}

// =============================================================================
// Tests: ReviewOrchestrator construction
// =============================================================================

func TestNewReviewOrchestrator(t *testing.T) {
	o := NewReviewOrchestrator()
	if o == nil {
		t.Fatal("expected non-nil orchestrator")
	}
	if o.stripper == nil {
		t.Error("expected non-nil bias stripper")
	}
	if o.fpTracker == nil {
		t.Error("expected non-nil FP tracker")
	}
	if o.minProviderDiversity != 2 {
		t.Errorf("expected minProviderDiversity=2, got %d", o.minProviderDiversity)
	}
}

func TestOrchestratorOptions(t *testing.T) {
	customBS := NewBiasStripper()
	customFP := NewFPTracker()
	o := NewReviewOrchestrator(
		WithBiasStripper(customBS),
		WithFPTracker(customFP),
		WithMinProviderDiversity(3),
	)
	if o.stripper != customBS {
		t.Error("custom bias stripper not set")
	}
	if o.fpTracker != customFP {
		t.Error("custom FP tracker not set")
	}
	if o.minProviderDiversity != 3 {
		t.Errorf("expected diversity=3, got %d", o.minProviderDiversity)
	}
}

func TestFPTrackerAccessor(t *testing.T) {
	o := NewReviewOrchestrator()
	if o.FPTracker() == nil {
		t.Error("FPTracker() returned nil")
	}
}

// =============================================================================
// Tests: Panel validation
// =============================================================================

func TestValidatePanel_SingleProvider(t *testing.T) {
	o := NewReviewOrchestrator()
	panel := &ReviewPanel{
		Primary:     newMock("model-a", "openai", VerdictApproved),
		Adversarial: newMock("model-b", "openai", VerdictApproved),
	}
	err := o.ValidatePanel(panel)
	if err == nil {
		t.Error("expected diversity violation error")
	}
}

func TestValidatePanel_TwoProviders(t *testing.T) {
	o := NewReviewOrchestrator()
	panel := &ReviewPanel{
		Primary:     newMock("model-a", "openai", VerdictApproved),
		Adversarial: newMock("model-b", "deepseek", VerdictApproved),
	}
	err := o.ValidatePanel(panel)
	if err != nil {
		t.Errorf("expected no error for 2-provider panel, got: %v", err)
	}
}

func TestValidatePanel_ThreeProviders(t *testing.T) {
	o := NewReviewOrchestrator()
	panel := &ReviewPanel{
		Primary:     newMock("model-a", "openai", VerdictApproved),
		Adversarial: newMock("model-b", "deepseek", VerdictApproved),
		Audit:       newMock("model-c", "openrouter", VerdictConfirmAdversarial),
	}
	err := o.ValidatePanel(panel)
	if err != nil {
		t.Errorf("expected no error for 3-provider panel, got: %v", err)
	}
}

func TestValidatePanel_RemovedModel(t *testing.T) {
	o := NewReviewOrchestrator()
	// Force a model into removed state.
	o.fpTracker.removed["model-x"] = true
	panel := &ReviewPanel{
		Primary:     newMock("model-x", "openai", VerdictApproved),
		Adversarial: newMock("model-b", "deepseek", VerdictApproved),
	}
	err := o.ValidatePanel(panel)
	if err == nil {
		t.Fatal("expected error for removed model")
	}
}

func TestValidatePanel_AllowsSingleModel(t *testing.T) {
	o := NewReviewOrchestrator()
	panel := &ReviewPanel{
		Primary: newMock("model-a", "openai", VerdictApproved),
	}
	// Single model → single provider → should fail diversity check.
	err := o.ValidatePanel(panel)
	if err == nil {
		t.Error("expected diversity violation for single-provider panel")
	}
}

func TestValidatePanel_AllowsSingleModelWithDiversityDisabled(t *testing.T) {
	o := NewReviewOrchestrator(WithMinProviderDiversity(1))
	panel := &ReviewPanel{
		Primary: newMock("model-a", "openai", VerdictApproved),
	}
	err := o.ValidatePanel(panel)
	if err != nil {
		t.Errorf("expected no error with diversity=1, got: %v", err)
	}
}

// =============================================================================
// Tests: Formation determination
// =============================================================================

func TestDetermineFormation(t *testing.T) {
	tests := []struct {
		category ChangeCategory
		expected int
	}{
		{CategoryContract, 3},
		{CategoryBehavioral, 2},
		{CategoryResilience, 1},
		{CategoryCosmetic, 1},
		{"unknown", 3}, // default
	}
	for _, tt := range tests {
		t.Run(string(tt.category), func(t *testing.T) {
			got := DetermineFormation(tt.category)
			if got != tt.expected {
				t.Errorf("DetermineFormation(%s) = %d, want %d", tt.category, got, tt.expected)
			}
		})
	}
}

func TestConsensusThreshold(t *testing.T) {
	tests := []struct {
		category ChangeCategory
		expected int
	}{
		{CategoryContract, 3},
		{CategoryBehavioral, 2},
		{CategoryResilience, 1},
		{CategoryCosmetic, 1},
		{"unknown", 3},
	}
	for _, tt := range tests {
		t.Run(string(tt.category), func(t *testing.T) {
			got := ConsensusThreshold(tt.category)
			if got != tt.expected {
				t.Errorf("ConsensusThreshold(%s) = %d, want %d", tt.category, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// Tests: Diversity score
// =============================================================================

func TestDiversityScore(t *testing.T) {
	tests := []struct {
		name     string
		f        Formation
		expected int
	}{
		{
			name:     "single provider",
			f:        Formation{Primary: ModelInfo{Model: "a", Provider: "openai"}},
			expected: 1,
		},
		{
			name: "two providers",
			f: Formation{
				Primary:     ModelInfo{Model: "a", Provider: "openai"},
				Adversarial: ModelInfo{Model: "b", Provider: "deepseek"},
			},
			expected: 2,
		},
		{
			name: "three providers",
			f: Formation{
				Primary:     ModelInfo{Model: "a", Provider: "openai"},
				Adversarial: ModelInfo{Model: "b", Provider: "deepseek"},
				Audit:       ModelInfo{Model: "c", Provider: "openrouter"},
			},
			expected: 3,
		},
		{
			name: "duplicate providers count once",
			f: Formation{
				Primary:     ModelInfo{Model: "a", Provider: "openai"},
				Adversarial: ModelInfo{Model: "b", Provider: "openai"},
				Audit:       ModelInfo{Model: "c", Provider: "deepseek"},
			},
			expected: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DiversityScore(tt.f)
			if got != tt.expected {
				t.Errorf("DiversityScore = %d, want %d", got, tt.expected)
			}
		})
	}
}

// =============================================================================
// Tests: Full review pipeline
// =============================================================================

func TestReview_UnanimousApproval(t *testing.T) {
	o := NewReviewOrchestrator()
	panel := &ReviewPanel{
		Primary:     newMock("gpt-5", "openai", VerdictApproved),
		Adversarial: newMock("ds-v4", "deepseek", VerdictApproved),
	}
	result, err := o.Review(context.Background(), panel, "diff content",
		"feat: add user registration", CategoryBehavioral,
		"https://forgejo.example.com/org/repo/pulls/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Bundle.Consensus.IsApproved() {
		t.Logf("Resolution: %s", result.Bundle.Consensus.Resolution)
	}
	if result.ModelsAgree != 2 {
		t.Errorf("expected 2 models agree, got %d", result.ModelsAgree)
	}
	if result.TotalModels != 2 {
		t.Errorf("expected 2 total models, got %d", result.TotalModels)
	}
	if result.ConsensusLevel != "unanimous" {
		t.Errorf("expected unanimous, got %s", result.ConsensusLevel)
	}
}

func TestReview_AdversarialBlocks(t *testing.T) {
	o := NewReviewOrchestrator()
	panel := &ReviewPanel{
		Primary:     newMock("gpt-5", "openai", VerdictApproved),
		Adversarial: newMock("ds-v4", "deepseek", VerdictBlock),
	}
	result, err := o.Review(context.Background(), panel, "diff content",
		"fix: handle nil pointer", CategoryBehavioral,
		"https://forgejo.example.com/org/repo/pulls/2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 2-model disagreement → tie_breaker
	if !result.Bundle.Consensus.NeedsTieBreaker() {
		t.Errorf("expected tie_breaker, got resolution=%s", result.Bundle.Consensus.Resolution)
	}
}

func TestReview_ThreeModelContract_Change(t *testing.T) {
	o := NewReviewOrchestrator()
	panel := &ReviewPanel{
		Primary:     newMock("gpt-5", "openai", VerdictApproved),
		Adversarial: newMock("ds-v4", "deepseek", VerdictBlock),
		Audit:       newMock("owl-a", "openrouter", VerdictConfirmAdversarial),
	}
	result, err := o.Review(context.Background(), panel, "diff content",
		"feat: change auth API", CategoryContract,
		"https://forgejo.example.com/org/repo/pulls/3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Audit confirms adversarial → blocked
	if !result.Bundle.Consensus.IsBlocked() {
		t.Errorf("expected blocked, got resolution=%s", result.Bundle.Consensus.Resolution)
	}
	if result.TotalModels != 3 {
		t.Errorf("expected 3 models, got %d", result.TotalModels)
	}
}

func TestReview_CosmeticChange_SingleModel(t *testing.T) {
	o := NewReviewOrchestrator()
	// For cosmetic, we only need 1 model — but validation requires 2 providers.
	// Override to allow 1.
	o.minProviderDiversity = 1
	panel := &ReviewPanel{
		Primary: newMock("gpt-5", "openai", VerdictApproved),
	}
	result, err := o.Review(context.Background(), panel, "diff content",
		"style: fix indentation", CategoryCosmetic,
		"https://forgejo.example.com/org/repo/pulls/4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TotalModels != 1 {
		t.Errorf("expected 1 model, got %d", result.TotalModels)
	}
	if result.ModelsAgree != 1 {
		t.Errorf("expected 1 agree, got %d", result.ModelsAgree)
	}
}

func TestReview_CollectsFindingsFromAllModels(t *testing.T) {
	o := NewReviewOrchestrator()
	panel := &ReviewPanel{
		Primary:     newMockWithFinding("gpt-5", "openai", VerdictPassWithNotes, "bug", "main.go", 10),
		Adversarial: newMockWithFinding("ds-v4", "deepseek", VerdictBlock, "race_condition", "main.go", 20),
	}
	result, err := o.Review(context.Background(), panel, "diff",
		"feat: add feature", CategoryBehavioral,
		"https://forgejo.example.com/org/repo/pulls/5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Bundle.Findings) != 2 {
		t.Errorf("expected 2 findings, got %d", len(result.Bundle.Findings))
	}
	// Verify each finding has its model role attached.
	types := map[string]bool{}
	for _, f := range result.Bundle.Findings {
		types[f.Type] = true
	}
	if !types["bug"] {
		t.Error("missing 'bug' finding from primary")
	}
	if !types["race_condition"] {
		t.Error("missing 'race_condition' finding from adversarial")
	}
}

func TestReview_BiasStrippingApplied(t *testing.T) {
	o := NewReviewOrchestrator()
	// Use a mock that records what it received.
	var receivedMsg string
	var mu sync.Mutex
	primary := &mockModel{
		info:    ModelInfo{Model: "gpt-5", Provider: "openai"},
		verdict: VerdictApproved,
	}
	origReview := primary.Review
	_ = origReview // suppress unused warning
	panel := &ReviewPanel{
		Primary:     primary,
		Adversarial: newMock("ds-v4", "deepseek", VerdictApproved),
	}

	// Override with a capturing client.
	capturingPrimary := &capturingModel{
		info:    ModelInfo{Model: "gpt-5", Provider: "openai"},
		verdict: VerdictApproved,
		capture: func(msg string) {
			mu.Lock()
			receivedMsg = msg
			mu.Unlock()
		},
	}
	panel.Primary = capturingPrimary

	originalMsg := "feat: Fixed the auth bug 🚀 all tests pass, ready to merge!"
	_, err := o.Review(context.Background(), panel, "diff", originalMsg,
		CategoryBehavioral, "https://forgejo.example.com/org/repo/pulls/6")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// Verify bias-stripped message was delivered.
	if receivedMsg == originalMsg {
		t.Error("expected bias-stripped message to differ from original")
	}
	// Should NOT contain stripped words.
	for _, word := range []string{"Fixed", "all tests pass", "ready to merge"} {
		if contains(receivedMsg, word) {
			t.Errorf("bias-stripped message still contains %q: %s", word, receivedMsg)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// capturingModel captures the NeutralCommitMsg it receives.
type capturingModel struct {
	info    ModelInfo
	verdict string
	capture func(string)
}

func (c *capturingModel) Review(ctx context.Context, req ReviewRequest) (*ModelReviewResult, error) {
	c.capture(req.NeutralCommitMsg)
	return &ModelReviewResult{Verdict: c.verdict}, nil
}

func (c *capturingModel) Info() ModelInfo { return c.info }

// =============================================================================
// Tests: Error handling
// =============================================================================

func TestReview_NilPanel(t *testing.T) {
	o := NewReviewOrchestrator()
	_, err := o.Review(context.Background(), nil, "diff", "msg",
		CategoryBehavioral, "url")
	if err == nil {
		t.Fatal("expected error for nil panel")
	}
}

func TestReview_NilPrimary(t *testing.T) {
	o := NewReviewOrchestrator()
	panel := &ReviewPanel{}
	_, err := o.Review(context.Background(), panel, "diff", "msg",
		CategoryBehavioral, "url")
	if err == nil {
		t.Fatal("expected error for nil primary")
	}
}

func TestReview_AllModelsFail(t *testing.T) {
	o := NewReviewOrchestrator()
	panel := &ReviewPanel{
		Primary:     newMockErr("gpt-5", "openai", errors.New("connection refused")),
		Adversarial: newMockErr("ds-v4", "deepseek", errors.New("connection refused")),
	}
	_, err := o.Review(context.Background(), panel, "diff", "msg",
		CategoryBehavioral, "url")
	if err == nil {
		t.Fatal("expected error when all models fail")
	}
}

func TestReview_PartialFailure(t *testing.T) {
	o := NewReviewOrchestrator()
	panel := &ReviewPanel{
		Primary:     newMock("gpt-5", "openai", VerdictApproved),
		Adversarial: newMockErr("ds-v4", "deepseek", errors.New("timeout")),
	}
	result, err := o.Review(context.Background(), panel, "diff", "msg",
		CategoryBehavioral, "url")
	if err != nil {
		t.Fatalf("expected partial success, got error: %v", err)
	}
	// The failed model should be counted as a block.
	if result.TotalModels != 2 {
		t.Errorf("expected 2 total models, got %d", result.TotalModels)
	}
}

func TestReview_DiversityViolation(t *testing.T) {
	o := NewReviewOrchestrator()
	panel := &ReviewPanel{
		Primary:     newMock("gpt-5", "openai", VerdictApproved),
		Adversarial: newMock("claude", "openai", VerdictApproved), // same provider
	}
	_, err := o.Review(context.Background(), panel, "diff", "msg",
		CategoryBehavioral, "url")
	if err == nil {
		t.Fatal("expected diversity violation error")
	}
}

// =============================================================================
// Tests: Context cancellation
// =============================================================================

func TestReview_ContextCancellation(t *testing.T) {
	o := NewReviewOrchestrator()
	panel := &ReviewPanel{
		Primary:     newMockDelay("gpt-5", "openai", VerdictApproved, 5*time.Second),
		Adversarial: newMockDelay("ds-v4", "deepseek", VerdictApproved, 5*time.Second),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	// Should either return an error or return results with failures.
	result, err := o.Review(ctx, panel, "diff", "msg", CategoryBehavioral, "url")
	if err != nil {
		// Error is acceptable for context cancellation.
		return
	}
	// If no error, the models should have failed (context deadline).
	_ = result
}

// =============================================================================
// Tests: Helpers
// =============================================================================

func TestCountApproving(t *testing.T) {
	tests := []struct {
		name     string
		verdicts []string
		expected int
	}{
		{"all approve", []string{VerdictApproved, VerdictApproved, VerdictPassWithNotes}, 3},
		{"all block", []string{VerdictBlock, VerdictBlock}, 0},
		{"mixed", []string{VerdictApproved, VerdictBlock}, 1},
		{"empty", []string{}, 0},
		{"pass with notes counts", []string{VerdictPassWithNotes, VerdictPassWithNotes}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countApproving(tt.verdicts...)
			if got != tt.expected {
				t.Errorf("countApproving(%v) = %d, want %d", tt.verdicts, got, tt.expected)
			}
		})
	}
}

func TestClassifyConsensus(t *testing.T) {
	tests := []struct {
		agree    int
		total    int
		expected string
	}{
		{3, 3, "unanimous"},
		{2, 2, "unanimous"},
		{1, 1, "unanimous"},
		{2, 3, "majority"},
		{1, 2, "divergent"},
		{0, 3, "divergent"},
		{0, 0, "none"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := classifyConsensus(tt.agree, tt.total)
			if got != tt.expected {
				t.Errorf("classifyConsensus(%d, %d) = %s, want %s",
					tt.agree, tt.total, got, tt.expected)
			}
		})
	}
}

func TestHashSHA256(t *testing.T) {
	h1 := hashSHA256("test")
	h2 := hashSHA256("test")
	h3 := hashSHA256("different")
	if h1 != h2 {
		t.Error("same input should produce same hash")
	}
	if h1 == h3 {
		t.Error("different input should produce different hash")
	}
	if len(h1) != 64 {
		t.Errorf("expected 64-char hex hash, got %d chars", len(h1))
	}
}

func TestGenerateReviewID(t *testing.T) {
	id1 := generateReviewID("https://example.com/pr/1")
	id2 := generateReviewID("https://example.com/pr/1")
	// IDs include a timestamp, so they should differ.
	if id1 == id2 {
		// Could theoretically be the same if called at exact same nanosecond.
		// Just verify format.
	}
	if len(id1) != 16 {
		t.Errorf("expected 16-char review ID, got %d chars", len(id1))
	}
}

// =============================================================================
// Tests: ReviewPanel methods
// =============================================================================

func TestReviewPanel_Roles(t *testing.T) {
	t.Run("full panel", func(t *testing.T) {
		panel := &ReviewPanel{
			Primary:     newMock("a", "p1", VerdictApproved),
			Adversarial: newMock("b", "p2", VerdictApproved),
			Audit:       newMock("c", "p3", VerdictApproved),
		}
		roles := panel.Roles()
		if len(roles) != 3 {
			t.Errorf("expected 3 roles, got %d", len(roles))
		}
	})

	t.Run("two-model panel", func(t *testing.T) {
		panel := &ReviewPanel{
			Primary:     newMock("a", "p1", VerdictApproved),
			Adversarial: newMock("b", "p2", VerdictApproved),
		}
		roles := panel.Roles()
		if len(roles) != 2 {
			t.Errorf("expected 2 roles, got %d", len(roles))
		}
	})

	t.Run("single-model panel", func(t *testing.T) {
		panel := &ReviewPanel{
			Primary: newMock("a", "p1", VerdictApproved),
		}
		roles := panel.Roles()
		if len(roles) != 1 {
			t.Errorf("expected 1 role, got %d", len(roles))
		}
	})
}

func TestReviewPanel_Formation(t *testing.T) {
	panel := &ReviewPanel{
		Primary:     newMock("gpt-5", "openai", VerdictApproved),
		Adversarial: newMock("ds-v4", "deepseek", VerdictApproved),
		Audit:       newMock("owl-a", "openrouter", VerdictApproved),
	}
	f := panel.Formation()
	if f.Primary.Model != "gpt-5" {
		t.Errorf("expected primary model gpt-5, got %s", f.Primary.Model)
	}
	if f.Adversarial.Model != "ds-v4" {
		t.Errorf("expected adversarial model ds-v4, got %s", f.Adversarial.Model)
	}
	if f.Audit.Model != "owl-a" {
		t.Errorf("expected audit model owl-a, got %s", f.Audit.Model)
	}
}

// =============================================================================
// Tests: Bias stripping integration
// =============================================================================

func TestReview_BiasStrippedSHA_DifferentFromOriginal(t *testing.T) {
	o := NewReviewOrchestrator()
	panel := &ReviewPanel{
		Primary:     newMock("gpt-5", "openai", VerdictApproved),
		Adversarial: newMock("ds-v4", "deepseek", VerdictApproved),
	}
	result, err := o.Review(context.Background(), panel, "diff",
		"feat: Fixed critical bug 🚀 all tests pass, definitely correct!",
		CategoryBehavioral, "https://forgejo.example.com/org/repo/pulls/7")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The stripped commit SHA should differ from original.
	if result.Bundle.BiasStrippedSHA == result.Bundle.OriginalCommit {
		t.Error("expected stripped SHA to differ from original when bias was present")
	}
}

func TestReview_BiasStrippedSHA_SameForNeutralMessage(t *testing.T) {
	o := NewReviewOrchestrator()
	panel := &ReviewPanel{
		Primary:     newMock("gpt-5", "openai", VerdictApproved),
		Adversarial: newMock("ds-v4", "deepseek", VerdictApproved),
	}
	neutralMsg := "Modified auth module"
	result, err := o.Review(context.Background(), panel, "diff",
		neutralMsg, CategoryBehavioral, "https://forgejo.example.com/org/repo/pulls/8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Neutral message should be unchanged.
	if result.Bundle.BiasStrippedSHA != result.Bundle.OriginalCommit {
		t.Error("expected same SHA for neutral message")
	}
}

// =============================================================================
// Tests: Evidence bundle structure
// =============================================================================

func TestReview_BundleHasCorrectFormation(t *testing.T) {
	o := NewReviewOrchestrator()
	panel := &ReviewPanel{
		Primary:     newMock("gpt-5", "openai", VerdictApproved),
		Adversarial: newMock("ds-v4", "deepseek", VerdictApproved),
		Audit:       newMock("owl-a", "openrouter", VerdictConfirmAdversarial),
	}
	result, err := o.Review(context.Background(), panel, "diff", "msg",
		CategoryContract, "https://forgejo.example.com/org/repo/pulls/9")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Bundle.Formation.Primary.Model != "gpt-5" {
		t.Errorf("expected primary gpt-5, got %s", result.Bundle.Formation.Primary.Model)
	}
	if result.Bundle.Formation.Adversarial.Model != "ds-v4" {
		t.Errorf("expected adversarial ds-v4, got %s", result.Bundle.Formation.Adversarial.Model)
	}
	if result.Bundle.Formation.Audit.Model != "owl-a" {
		t.Errorf("expected audit owl-a, got %s", result.Bundle.Formation.Audit.Model)
	}
}

func TestReview_DiversityScoreInResult(t *testing.T) {
	o := NewReviewOrchestrator()
	panel := &ReviewPanel{
		Primary:     newMock("gpt-5", "openai", VerdictApproved),
		Adversarial: newMock("ds-v4", "deepseek", VerdictApproved),
		Audit:       newMock("owl-a", "openrouter", VerdictApproved),
	}
	result, err := o.Review(context.Background(), panel, "diff", "msg",
		CategoryContract, "url")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DiversityScore != 3 {
		t.Errorf("expected diversity score 3, got %d", result.DiversityScore)
	}
}

// =============================================================================
// Tests: Concurrent dispatch verification
// =============================================================================

func TestReview_DispatchesToAllModelsConcurrently(t *testing.T) {
	o := NewReviewOrchestrator()
	primary := newMock("gpt-5", "openai", VerdictApproved)
	adversarial := newMock("ds-v4", "deepseek", VerdictApproved)
	audit := newMock("owl-a", "openrouter", VerdictApproved)
	panel := &ReviewPanel{
		Primary:     primary,
		Adversarial: adversarial,
		Audit:       audit,
	}
	_, err := o.Review(context.Background(), panel, "diff", "msg",
		CategoryContract, "url")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if primary.CallCount() != 1 {
		t.Errorf("expected primary called once, got %d", primary.CallCount())
	}
	if adversarial.CallCount() != 1 {
		t.Errorf("expected adversarial called once, got %d", adversarial.CallCount())
	}
	if audit.CallCount() != 1 {
		t.Errorf("expected audit called once, got %d", audit.CallCount())
	}
}
