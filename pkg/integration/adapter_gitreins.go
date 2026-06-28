package integration

// ---------------------------------------------------------------------------
// GitReins Adapter — specs/integrations.md §1
// ---------------------------------------------------------------------------
//
// GitReins provides the two-tier quality gate for Helix: Tier 1 static
// guards (secrets, lint, tests, build) and Tier 2 agentic LLM evaluation.
// The adapter wraps GitReins CLI calls into a Go interface.

// GitReinsAdapter defines the contract for GitReins integration.
type GitReinsAdapter interface {
	// Guard runs Tier 1 checks against staged changes. Returns PASS/FAIL
	// with per-check results. Called by pre-commit hook.
	Guard(workdir string, opts GuardOpts) (*GuardResult, error)

	// Evaluate runs Tier 2 agentic evaluation against the diff.
	// Returns structured verdict with evidence. Called post-commit.
	Evaluate(workdir string, diff string, opts EvalOpts) (*EvalResult, error)

	// Cost returns the token cost of the last Evaluate call (from LLMUsage).
	// Required for Feature 2 (Cost Estimator) reconciliation.
	Cost(evalResult *EvalResult) CostBreakdown
}

// GuardOpts configures which Tier 1 checks to run.
type GuardOpts struct {
	SkipSecrets bool // Skip secret scanning
	SkipLint    bool // Skip lint checks
	SkipTests   bool // Skip test execution
	SkipBuild   bool // Skip build verification
	Timeout     int  // Seconds (default: 60)
}

// GuardResult holds the outcome of a Tier 1 guard run.
type GuardResult struct {
	Passed   bool
	Checks   map[string]CheckResult // key: "secrets", "lint", "tests", "build"
	Duration float64                // seconds
}

// CheckResult holds the result of a single guard check.
type CheckResult struct {
	Passed   bool
	Output   string // stdout/stderr summary
	Duration float64
}

// EvalOpts configures Tier 2 agentic evaluation.
type EvalOpts struct {
	MaxIterations   int      // Max LLM reasoning turns (default: 10)
	MaxTime         string   // Wall-clock cap: "30s", "5m" (default: "2m")
	MaxInputTokens  string   // Input token budget (default: "200k")
	MaxOutputTokens string   // Output token budget (default: "50k")
	ToolCallWeight  float64  // Fraction of an iteration per tool call (default: 0.1)
	Criteria        []string // Evaluation criteria
}

// EvalResult holds the outcome of a Tier 2 evaluation.
type EvalResult struct {
	Passed   bool
	Verdicts map[string]Verdict // keyed by criteria
	Evidence []Evidence
	Usage    LLMUsage
	Duration float64
}

// Verdict represents a single criterion evaluation.
type Verdict struct {
	Status string // "PASS", "FAIL", "INCONCLUSIVE"
	Reason string
	Score  float64 // 0.0-1.0
}

// Evidence is a piece of evaluation evidence with source attribution.
type Evidence struct {
	Type    string // "test_output", "file_content", "search_result", "tool_call"
	Content string
	Source  string // file path or tool name
}

// LLMUsage tracks token consumption for cost reconciliation.
type LLMUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CacheReadTokens  int // tokens served from cache
	CacheWriteTokens int // tokens written to cache
}

// CostBreakdown decomposes the cost of an evaluation.
type CostBreakdown struct {
	FreshInputCost float64
	CacheHitCost   float64
	CacheWriteCost float64
	OutputCost     float64
	TotalCost      float64
}
