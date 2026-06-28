package integration

// ---------------------------------------------------------------------------
// PromptFoo Adapter — specs/prompt-registry.md §10
// ---------------------------------------------------------------------------
//
// PromptFoo is the prompt quality testing framework. Every Helix prompt
// goes through PromptFoo regression tests on commit. Prompt changes trigger
// Forgejo CI that runs promptfoo eval and blocks the commit on failure.
//
// The core PromptFoo logic (GeneratePromptFooYAML, ParsePromptFooResults)
// lives in pkg/prompt/promptfoo.go. This adapter defines the service-level
// contract: run the eval, generate configs, check results.

// PromptFooAdapter defines the contract for prompt quality testing.
type PromptFooAdapter interface {
	// GenerateConfig produces a .promptfoo.yaml configuration from
	// registered prompts. Each prompt becomes a test entry with
	// model-specific assertions (spec §10.2).
	GenerateConfig(prompts []PromptFooPromptDef) ([]byte, error)

	// RunEval executes promptfoo eval against the current config.
	RunEval(opts PromptFooRunOpts) (*PromptFooEvalResult, error)

	// GetResults returns the parsed results of the last eval run.
	GetResults() (*PromptFooEvalResult, error)

	// ValidateConfig checks that .promptfoo.yaml is syntactically valid.
	ValidateConfig(configPath string) error
}

// PromptFooPromptDef describes a prompt for PromptFoo configuration.
type PromptFooPromptDef struct {
	Component string
	Version   string
	FilePath  string
	Model     string
}

// PromptFooRunOpts configures a PromptFoo eval run.
type PromptFooRunOpts struct {
	ConfigPath     string   // Path to .promptfoo.yaml
	Prompts        []string // Filter specific prompts (empty = all)
	MaxConcurrency int      // Max parallel eval workers
	Repeat         int      // Repeat each test N times
}

// PromptFooEvalResult captures the outcome of a PromptFoo eval run.
type PromptFooEvalResult struct {
	TotalTests  int
	PassedTests int
	FailedTests int
	Results     []PromptFooTestResult
	Duration    float64
}

// PromptFooTestResult is a single test outcome.
type PromptFooTestResult struct {
	Description string
	Passed      bool
	Error       string
	Score       float64
}
