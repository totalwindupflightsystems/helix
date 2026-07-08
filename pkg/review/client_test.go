// Package review — client_test.go
//
// Tests for the model client infrastructure: prompt construction,
// JSON response parsing, and client constructors.

package review

import (
	"strings"
	"testing"
)

// =============================================================================
// buildReviewPrompt tests
// =============================================================================

func TestBuildReviewPrompt_IncludesDiff(t *testing.T) {
	req := ReviewRequest{
		Role:             RolePrimary,
		Diff:             "diff --git a/main.go b/main.go\n+fmt.Println(\"hello\")",
		NeutralCommitMsg: "add greeting",
		Context:          ReviewContext{Category: CategoryBehavioral},
	}
	info := ModelInfo{Model: "deepseek-v4-flash", Provider: "deepseek"}

	prompt := buildReviewPrompt(req, info)

	if !strings.Contains(prompt, "PRIMARY") {
		t.Error("prompt should mention PRIMARY role")
	}
	if !strings.Contains(prompt, "add greeting") {
		t.Error("prompt should include commit message")
	}
	if !strings.Contains(prompt, "fmt.Println") {
		t.Error("prompt should include diff content")
	}
	if !strings.Contains(prompt, `"verdict"`) {
		t.Error("prompt should include JSON output instructions")
	}
}

func TestBuildReviewPrompt_RoleVariants(t *testing.T) {
	diff := "test diff"
	msg := "test msg"
	ctx := ReviewContext{Category: CategoryBehavioral}
	info := ModelInfo{Model: "test", Provider: "test"}

	tests := []struct {
		role     ReviewRole
		expected string
	}{
		{RolePrimary, "PRIMARY reviewer"},
		{RoleAdversarial, "ADVERSARIAL reviewer"},
		{RoleAudit, "AUDIT reviewer"},
	}

	for _, tt := range tests {
		prompt := buildReviewPrompt(ReviewRequest{
			Role:             tt.role,
			Diff:             diff,
			NeutralCommitMsg: msg,
			Context:          ctx,
		}, info)
		if !strings.Contains(prompt, tt.expected) {
			t.Errorf("role %s: expected %q in prompt", tt.role, tt.expected)
		}
	}
}

// =============================================================================
// truncateDiff tests
// =============================================================================

func TestTruncateDiff_Short(t *testing.T) {
	diff := "short diff"
	result := truncateDiff(diff)
	if result != diff {
		t.Errorf("short diff should not be truncated, got %q", result)
	}
}

func TestTruncateDiff_Long(t *testing.T) {
	diff := strings.Repeat("x", 60000)
	result := truncateDiff(diff)
	if len(result) > 50000+30 {
		t.Errorf("truncated diff too long: %d chars", len(result))
	}
	if !strings.Contains(result, "[diff truncated]") {
		t.Error("truncated diff should include truncation marker")
	}
}

// =============================================================================
// parseReviewResponse tests
// =============================================================================

func TestParseReviewResponse_CleanJSON(t *testing.T) {
	body := []byte(`{"verdict":"approved","findings":[]}`)
	result, err := parseReviewResponse(body, "test-model")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result.Verdict != "approved" {
		t.Errorf("verdict = %q, want %q", result.Verdict, "approved")
	}
}

func TestParseReviewResponse_WithMarkdownFences(t *testing.T) {
	body := []byte("```json\n{\"verdict\":\"block\",\"findings\":[{\"severity\":\"high\",\"type\":\"security\",\"file\":\"auth.go\",\"line\":42,\"description\":\"SQL injection\",\"evidence\":\"unparameterized query\"}]}\n```")
	result, err := parseReviewResponse(body, "test-model")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result.Verdict != "block" {
		t.Errorf("verdict = %q, want %q", result.Verdict, "block")
	}
	if len(result.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(result.Findings))
	}
	if result.Findings[0].Severity != "high" {
		t.Errorf("severity = %q, want %q", result.Findings[0].Severity, "high")
	}
	// Model name should be set.
	if result.Findings[0].Model != "test-model" {
		t.Errorf("model = %q, want %q", result.Findings[0].Model, "test-model")
	}
}

func TestParseReviewResponse_JSONInText(t *testing.T) {
	body := []byte("Here is my review:\n\n{\"verdict\":\"pass_with_notes\",\"findings\":[]}\n\nHope this helps!")
	result, err := parseReviewResponse(body, "test-model")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result.Verdict != "pass_with_notes" {
		t.Errorf("verdict = %q, want %q", result.Verdict, "pass_with_notes")
	}
}

func TestParseReviewResponse_InvalidJSON(t *testing.T) {
	body := []byte("not json at all")
	_, err := parseReviewResponse(body, "test")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// =============================================================================
// roleInstruction tests
// =============================================================================

func TestRoleInstruction_Primary(t *testing.T) {
	got := roleInstruction(RolePrimary, CategoryContract)
	if !strings.Contains(got, "PRIMARY") {
		t.Error("primary instruction should mention PRIMARY")
	}
	if !strings.Contains(got, "contract") {
		t.Error("primary instruction should include category")
	}
}

func TestRoleInstruction_Adversarial(t *testing.T) {
	got := roleInstruction(RoleAdversarial, CategoryBehavioral)
	if !strings.Contains(got, "BREAK") {
		t.Error("adversarial instruction should say BREAK")
	}
}

// =============================================================================
// Client constructor tests
// =============================================================================

func TestNewDeepSeekClient_Defaults(t *testing.T) {
	cfg := ModelClientConfig{
		BaseURL: "https://api.deepseek.com/v1",
		APIKey:  "sk-test",
	}
	client := NewDeepSeekClient(cfg)
	if client.Info().Model != "deepseek-v4-flash" {
		t.Errorf("default model = %q, want %q", client.Info().Model, "deepseek-v4-flash")
	}
	if client.Info().Provider != "deepseek" {
		t.Errorf("provider = %q, want %q", client.Info().Provider, "deepseek")
	}
}

func TestNewDeepSeekClient_CustomModel(t *testing.T) {
	cfg := ModelClientConfig{
		BaseURL: "https://api.deepseek.com/v1",
		APIKey:  "sk-test",
		Model:   "deepseek-v4-pro",
	}
	client := NewDeepSeekClient(cfg)
	if client.Info().Model != "deepseek-v4-pro" {
		t.Errorf("model = %q, want %q", client.Info().Model, "deepseek-v4-pro")
	}
}

func TestNewChimeraClient(t *testing.T) {
	cfg := ModelClientConfig{
		BaseURL: "http://localhost:8765",
		Model:   "chimera-default",
	}
	client := NewChimeraClient(cfg)
	if client.Info().Provider != "chimera" {
		t.Errorf("provider = %q, want %q", client.Info().Provider, "chimera")
	}
	if client.Info().Model != "chimera-default" {
		t.Errorf("model = %q, want %q", client.Info().Model, "chimera-default")
	}
}

// =============================================================================
// formationForCategory tests
// =============================================================================

func TestFormationForCategory(t *testing.T) {
	tests := []struct {
		cat      ChangeCategory
		expected string
	}{
		{CategoryContract, "rigorous"},
		{CategoryBehavioral, "balanced"},
		{CategoryResilience, "fast"},
		{CategoryCosmetic, "auto"},
	}

	for _, tt := range tests {
		got := formationForCategory(tt.cat)
		if got != tt.expected {
			t.Errorf("formationForCategory(%s) = %q, want %q", tt.cat, got, tt.expected)
		}
	}
}
