package prompt

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// ============================================================================
// GeneratePromptFooYAML
// ============================================================================

func TestGeneratePromptFooYAML(t *testing.T) {
	t.Run("empty prompts", func(t *testing.T) {
		data, err := GeneratePromptFooYAML(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var cfg PromptFooYAML
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			t.Fatalf("output is not valid YAML: %v", err)
		}
		if len(cfg.Prompts) != 0 {
			t.Errorf("expected 0 prompts, got %d", len(cfg.Prompts))
		}
		if len(cfg.Providers) != 3 {
			t.Errorf("expected 3 default providers, got %d", len(cfg.Providers))
		}
		if len(cfg.Tests) != 0 {
			t.Errorf("expected 0 tests, got %d", len(cfg.Tests))
		}
	})

	t.Run("single prompt", func(t *testing.T) {
		prompts := []Prompt{
			{Component: "agent-identity", Version: "v1.0.0", Model: "deepseek-v4-pro", Provider: "deepseek"},
		}
		data, err := GeneratePromptFooYAML(prompts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var cfg PromptFooYAML
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			t.Fatalf("output is not valid YAML: %v", err)
		}
		if len(cfg.Prompts) != 1 {
			t.Fatalf("expected 1 prompt, got %d", len(cfg.Prompts))
		}
		if cfg.Prompts[0].File != "prompts/agent-identity/v1.0.0/prompt.md" {
			t.Errorf("unexpected prompt file: %s", cfg.Prompts[0].File)
		}
		// Each prompt generates 2 tests: no-TODO and model-specific
		if len(cfg.Tests) != 2 {
			t.Fatalf("expected 2 tests, got %d", len(cfg.Tests))
		}
		if cfg.Tests[0].Description != "agent-identity/v1.0.0: no TODO stubs" {
			t.Errorf("unexpected test desc: %s", cfg.Tests[0].Description)
		}
		if cfg.Tests[1].Description != "agent-identity/v1.0.0: model is deepseek-v4-pro" {
			t.Errorf("unexpected test desc: %s", cfg.Tests[1].Description)
		}
		// Verify providers unchanged
		if len(cfg.Providers) != 3 {
			t.Errorf("expected 3 providers, got %d", len(cfg.Providers))
		}
	})

	t.Run("multiple prompts", func(t *testing.T) {
		prompts := []Prompt{
			{Component: "agent-identity", Version: "v1.0.0", Model: "deepseek-v4-pro"},
			{Component: "cost-estimator", Version: "v2.0.0", Model: "glm-5.2"},
		}
		data, err := GeneratePromptFooYAML(prompts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var cfg PromptFooYAML
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			t.Fatalf("output is not valid YAML: %v", err)
		}
		if len(cfg.Prompts) != 2 {
			t.Fatalf("expected 2 prompts, got %d", len(cfg.Prompts))
		}
		// 2 prompts × 2 tests each = 4 tests
		if len(cfg.Tests) != 4 {
			t.Fatalf("expected 4 tests, got %d", len(cfg.Tests))
		}
	})

	t.Run("providers include expected IDs", func(t *testing.T) {
		data, err := GeneratePromptFooYAML([]Prompt{{Component: "x", Version: "v1", Model: "m"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var cfg PromptFooYAML
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			t.Fatalf("output is not valid YAML: %v", err)
		}
		ids := []string{cfg.Providers[0].ID, cfg.Providers[1].ID, cfg.Providers[2].ID}
		expected := []string{"deepseek:deepseek-v4-flash", "deepseek:deepseek-v4-pro", "zai-glm:glm-5.2"}
		for i, id := range ids {
			if id != expected[i] {
				t.Errorf("provider[%d] = %q, want %q", i, id, expected[i])
			}
		}
	})
}

// ============================================================================
// ParsePromptFooResults
// ============================================================================

func TestParsePromptFooResults(t *testing.T) {
	t.Run("passing results", func(t *testing.T) {
		input := `{"results":[{"prompt":{"raw":"hello","id":"test1"},"grader":{"pass":true,"reason":"","score":1.0}}],"stats":{"successes":1,"failures":0,"total":1}}`
		results, err := ParsePromptFooResults([]byte(input))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if results.TotalTests != 1 {
			t.Errorf("TotalTests = %d, want 1", results.TotalTests)
		}
		if results.PassedTests != 1 {
			t.Errorf("PassedTests = %d, want 1", results.PassedTests)
		}
		if results.FailedTests != 0 {
			t.Errorf("FailedTests = %d, want 0", results.FailedTests)
		}
		if len(results.Results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results.Results))
		}
		if !results.Results[0].Passed {
			t.Error("expected passed=true")
		}
		if results.Results[0].Error != "" {
			t.Errorf("expected no error, got %q", results.Results[0].Error)
		}
		if results.Results[0].Description != "test1" {
			t.Errorf("desc = %q, want %q", results.Results[0].Description, "test1")
		}
	})

	t.Run("failing results with reason", func(t *testing.T) {
		input := `{"results":[{"prompt":{"raw":"hello","id":"fail1"},"grader":{"pass":false,"reason":"expected X got Y","score":0.0}}],"stats":{"successes":0,"failures":1,"total":1}}`
		results, err := ParsePromptFooResults([]byte(input))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if results.TotalTests != 1 {
			t.Errorf("TotalTests = %d, want 1", results.TotalTests)
		}
		if results.FailedTests != 1 {
			t.Errorf("FailedTests = %d, want 1", results.FailedTests)
		}
		if len(results.Results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results.Results))
		}
		if results.Results[0].Passed {
			t.Error("expected passed=false")
		}
		if results.Results[0].Error != "expected X got Y" {
			t.Errorf("error = %q, want %q", results.Results[0].Error, "expected X got Y")
		}
	})

	t.Run("failing results without reason", func(t *testing.T) {
		input := `{"results":[{"prompt":{"raw":"hello","id":"fail2"},"grader":{"pass":false,"reason":"","score":0.0}}],"stats":{"successes":0,"failures":1,"total":1}}`
		results, err := ParsePromptFooResults([]byte(input))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if results.Results[0].Error != "test failed (no reason given)" {
			t.Errorf("error = %q, want default", results.Results[0].Error)
		}
	})

	t.Run("mixed pass/fail results", func(t *testing.T) {
		input := `{"results":[{"prompt":{"raw":"","id":"p1"},"grader":{"pass":true,"reason":"","score":1.0}},{"prompt":{"raw":"","id":"p2"},"grader":{"pass":false,"reason":"bad","score":0.0}}],"stats":{"successes":1,"failures":1,"total":2}}`
		results, err := ParsePromptFooResults([]byte(input))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if results.TotalTests != 2 {
			t.Errorf("TotalTests = %d, want 2", results.TotalTests)
		}
		if results.PassedTests != 1 {
			t.Errorf("PassedTests = %d, want 1", results.PassedTests)
		}
		if results.FailedTests != 1 {
			t.Errorf("FailedTests = %d, want 1", results.FailedTests)
		}
		if len(results.Results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results.Results))
		}
	})

	t.Run("empty prompt ID falls back to truncated raw", func(t *testing.T) {
		input := `{"results":[{"prompt":{"raw":"a longer prompt that should be truncated at eighty characters for the description field in test results","id":""},"grader":{"pass":true,"reason":"","score":1.0}}],"stats":{"successes":1,"failures":0,"total":1}}`
		results, err := ParsePromptFooResults([]byte(input))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		desc := results.Results[0].Description
		if len(desc) > 83 {
			// 80 chars + "..."
			t.Errorf("desc too long: %d chars, desc=%q", len(desc), desc)
		}
		if desc[len(desc)-3:] != "..." {
			t.Error("expected truncation '...' suffix")
		}
	})

	t.Run("empty prompt ID with short raw", func(t *testing.T) {
		input := `{"results":[{"prompt":{"raw":"short","id":""},"grader":{"pass":true,"reason":"","score":1.0}}],"stats":{"successes":1,"failures":0,"total":1}}`
		results, err := ParsePromptFooResults([]byte(input))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if results.Results[0].Description != "short" {
			t.Errorf("desc = %q, want %q", results.Results[0].Description, "short")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		_, err := ParsePromptFooResults([]byte(`{not valid json`))
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})

	t.Run("empty input", func(t *testing.T) {
		_, err := ParsePromptFooResults([]byte{})
		if err == nil {
			t.Error("expected error for empty input")
		}
	})

	t.Run("valid JSON but missing stats", func(t *testing.T) {
		input := `{"results":[],"stats":{"successes":0,"failures":0,"total":0}}`
		results, err := ParsePromptFooResults([]byte(input))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if results.TotalTests != 0 {
			t.Errorf("TotalTests = %d, want 0", results.TotalTests)
		}
	})

	t.Run("multiple results with same prompt ID", func(t *testing.T) {
		input := `{"results":[{"prompt":{"raw":"","id":"same"},"grader":{"pass":true,"reason":"","score":1.0}},{"prompt":{"raw":"","id":"same"},"grader":{"pass":false,"reason":"fail2","score":0.0}}],"stats":{"successes":1,"failures":1,"total":2}}`
		results, err := ParsePromptFooResults([]byte(input))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results.Results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results.Results))
		}
	})
}

// ============================================================================
// errorFromGrader
// ============================================================================

func TestErrorFromGrader(t *testing.T) {
	tests := []struct {
		name   string
		pass   bool
		reason string
		want   string
	}{
		{"pass with reason", true, "something", ""},
		{"pass with empty reason", true, "", ""},
		{"fail with reason", false, "expected X got Y", "expected X got Y"},
		{"fail with empty reason", false, "", "test failed (no reason given)"},
		{"fail with whitespace reason", false, "  ", "  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errorFromGrader(tt.pass, tt.reason)
			if got != tt.want {
				t.Errorf("errorFromGrader(%v, %q) = %q, want %q", tt.pass, tt.reason, got, tt.want)
			}
		})
	}
}

// ============================================================================
// truncate
// ============================================================================

func TestTruncate(t *testing.T) {
	t.Run("short string unchanged", func(t *testing.T) {
		got := truncate("hello", 10)
		if got != "hello" {
			t.Errorf("truncate = %q, want %q", got, "hello")
		}
	})

	t.Run("exactly maxLen unchanged", func(t *testing.T) {
		s := "1234567890"
		got := truncate(s, 10)
		if got != s {
			t.Errorf("truncate = %q, want %q", got, s)
		}
	})

	t.Run("longer string truncated", func(t *testing.T) {
		s := "this is a very long string that exceeds the limit"
		got := truncate(s, 20)
		if len(got) > 23 { // 20 + "..."
			t.Errorf("truncated too long: %d chars, got %q", len(got), got)
		}
		if got[len(got)-3:] != "..." {
			t.Errorf("expected '...' suffix, got %q", got)
		}
	})

	t.Run("whitespace trimmed at boundary", func(t *testing.T) {
		s := "hello     world extra"
		got := truncate(s, 10)
		// Truncates at 10 then trims trailing whitespace
		// "hello     " → "hello"
		if got != "hello..." {
			t.Errorf("truncate = %q, want %q", got, "hello...")
		}
	})

	t.Run("empty string", func(t *testing.T) {
		got := truncate("", 5)
		if got != "" {
			t.Errorf("truncate = %q, want %q", got, "")
		}
	})

	t.Run("zero maxLen", func(t *testing.T) {
		s := "hello"
		got := truncate(s, 0)
		// Truncate to 0 chars → trim all whitespace → empty → "..."
		if got != "..." {
			t.Errorf("truncate = %q, want %q", got, "...")
		}
	})

	t.Run("single char truncated", func(t *testing.T) {
		got := truncate("hello", 1)
		if got != "h..." {
			t.Errorf("truncate = %q, want %q", got, "h...")
		}
	})

	t.Run("boundary at space", func(t *testing.T) {
		s := "hello world extra"
		got := truncate(s, 11)
		// 11 chars of "hello world extra" = "hello world" + TrimSpace → "hello world" + "..."
		if got != "hello world..." {
			t.Errorf("truncate = %q, want %q", got, "hello world...")
		}
	})

	t.Run("boundary at space with trim", func(t *testing.T) {
		s := "hello world extra"
		got := truncate(s, 12)
		// 12 chars of "hello world extra" = "hello world " + TrimSpace → "hello world" + "..."
		if got != "hello world..." {
			t.Errorf("truncate = %q, want %q", got, "hello world...")
		}
	})
}

// Verify that real PromptFoo JSON with all edge cases parses correctly.
func TestPromptFooResultJSONRoundtrip(t *testing.T) {
	// Build realistic PromptFoo output JSON and verify parsing.
	raw := `{
		"results": [
			{"prompt": {"raw": "full prompt text here", "id": "eval-001"}, "grader": {"pass": true, "reason": "", "score": 1.0}},
			{"prompt": {"raw": "another prompt", "id": "eval-002"}, "grader": {"pass": false, "reason": "expected X got Y", "score": 0.0}}
		],
		"stats": {"successes": 1, "failures": 1, "total": 2}
	}`
	parsed, err := ParsePromptFooResults([]byte(raw))
	if err != nil {
		t.Fatalf("ParsePromptFooResults: %v", err)
	}
	if parsed.TotalTests != 2 {
		t.Errorf("TotalTests = %d, want 2", parsed.TotalTests)
	}
	if parsed.PassedTests != 1 {
		t.Errorf("PassedTests = %d, want 1", parsed.PassedTests)
	}
	if parsed.FailedTests != 1 {
		t.Errorf("FailedTests = %d, want 1", parsed.FailedTests)
	}
	if len(parsed.Results) != 2 {
		t.Fatalf("Results len = %d, want 2", len(parsed.Results))
	}
	if parsed.Results[0].Description != "eval-001" {
		t.Errorf("Results[0].Description = %q, want %q", parsed.Results[0].Description, "eval-001")
	}
	if !parsed.Results[0].Passed {
		t.Error("Results[0].Passed should be true")
	}
	if parsed.Results[1].Description != "eval-002" {
		t.Errorf("Results[1].Description = %q, want %q", parsed.Results[1].Description, "eval-002")
	}
	if parsed.Results[1].Passed {
		t.Error("Results[1].Passed should be false")
	}
	if parsed.Results[1].Error != "expected X got Y" {
		t.Errorf("Results[1].Error = %q, want %q", parsed.Results[1].Error, "expected X got Y")
	}
}
