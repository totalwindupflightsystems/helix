package prompt

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// PromptFoo YAML generation (spec §10)
// ---------------------------------------------------------------------------

// PromptFooYAML represents the .promptfoo.yaml configuration structure.
type PromptFooYAML struct {
	Prompts   []PromptFooPrompt `yaml:"prompts"`
	Providers []PromptFooProvider `yaml:"providers"`
	Tests     []PromptFooTest   `yaml:"tests"`
}

// PromptFooPrompt is a prompt entry in the PromptFoo config.
type PromptFooPrompt struct {
	File string `yaml:"file,omitempty"`
	ID   string `yaml:"id,omitempty"`
	Text string `yaml:"text,omitempty"`
}

// PromptFooProvider is a provider entry in the PromptFoo config.
type PromptFooProvider struct {
	ID   string            `yaml:"id"`
	Env  map[string]string `yaml:"env,omitempty"`
	Name string            `yaml:"name,omitempty"`
}

// PromptFooTest is a test case in the PromptFoo config.
type PromptFooTest struct {
	Description string              `yaml:"description"`
	Vars        map[string]string   `yaml:"vars,omitempty"`
	Assert      []PromptFooAssert   `yaml:"assert"`
}

// PromptFooAssert is a single assertion in a PromptFoo test.
type PromptFooAssert struct {
	Type  string `yaml:"type"`
	Value string `yaml:"value"`
}

// GeneratePromptFooYAML generates a .promptfoo.yaml configuration from a list
// of registered prompts. Each prompt becomes a prompt entry (file:// reference)
// and a default test suite is generated (spec §10.2).
func GeneratePromptFooYAML(prompts []Prompt) ([]byte, error) {
	config := PromptFooYAML{
		Prompts: make([]PromptFooPrompt, 0, len(prompts)),
		Providers: []PromptFooProvider{
			{ID: "deepseek:deepseek-v4-flash"},
			{ID: "deepseek:deepseek-v4-pro"},
			{ID: "zai-glm:glm-5.2"},
		},
		Tests: make([]PromptFooTest, 0, len(prompts)),
	}

	for _, p := range prompts {
		// Add prompt entry
		config.Prompts = append(config.Prompts, PromptFooPrompt{
			File: fmt.Sprintf("prompts/%s/%s/prompt.md", p.Component, p.Version),
		})

		// Generate default test: prompt must not contain TODO or FIXME
		config.Tests = append(config.Tests, PromptFooTest{
			Description: fmt.Sprintf("%s/%s: no TODO stubs", p.Component, p.Version),
			Assert: []PromptFooAssert{
				{Type: "not-contains", Value: "TODO"},
				{Type: "not-contains", Value: "FIXME"},
			},
		})

		// Generate model-specific test
		config.Tests = append(config.Tests, PromptFooTest{
			Description: fmt.Sprintf("%s/%s: model is %s", p.Component, p.Version, p.Model),
			Assert: []PromptFooAssert{
				{Type: "contains", Value: p.Model},
			},
		})
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal promptfoo yaml: %w", err)
	}
	return data, nil
}

// ---------------------------------------------------------------------------
// PromptFoo results parsing (spec §10)
// ---------------------------------------------------------------------------

// promptFooRawResult is the JSON structure returned by `promptfoo eval`.
type promptFooRawResult struct {
	Results []struct {
		Prompt struct {
			Raw string `json:"raw"`
			ID  string `json:"id"`
		} `json:"prompt"`
		Vars  map[string]interface{} `json:"vars"`
		Grader struct {
			Pass   bool   `json:"pass"`
			Reason string `json:"reason"`
			Score  float64 `json:"score"`
		} `json:"grader"`
	} `json:"results"`
	Stats struct {
		Successes int `json:"successes"`
		Failures  int `json:"failures"`
		Total     int `json:"total"`
	} `json:"stats"`
}

// ParsePromptFooResults parses the JSON output of `promptfoo eval` into
// EvalResults. The input is the raw bytes from promptfoo-results.json
// (spec §10.1).
func ParsePromptFooResults(results []byte) (*EvalResults, error) {
	var raw promptFooRawResult
	if err := json.Unmarshal(results, &raw); err != nil {
		return nil, fmt.Errorf("parse promptfoo results: %w", err)
	}

	evalResults := &EvalResults{
		TotalTests:  raw.Stats.Total,
		PassedTests: raw.Stats.Successes,
		FailedTests: raw.Stats.Failures,
		Results:     make([]EvalTestResult, 0, len(raw.Results)),
	}

	for _, r := range raw.Results {
		desc := r.Prompt.ID
		if desc == "" {
			desc = truncate(r.Prompt.Raw, 80)
		}
		evalResults.Results = append(evalResults.Results, EvalTestResult{
			Description: desc,
			Passed:      r.Grader.Pass,
			Error:       errorFromGrader(r.Grader.Pass, r.Grader.Reason),
		})
	}

	return evalResults, nil
}

// errorFromGrader returns an empty string on pass, or the grader reason on fail.
func errorFromGrader(pass bool, reason string) string {
	if pass {
		return ""
	}
	if reason == "" {
		return "test failed (no reason given)"
	}
	return reason
}

// truncate shortens a string to maxLen characters, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return strings.TrimSpace(s[:maxLen]) + "..."
}
