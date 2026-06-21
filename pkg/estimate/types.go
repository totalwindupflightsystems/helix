// Package estimate implements the Helix pre-flight cost estimator.
//
// It estimates the token cost of an agent task BEFORE execution begins, checks
// the estimate against the agent's remaining weekly budget, and either
// approves, denies, or escalates for human approval. Estimation is
// cache-aware: it distinguishes fresh input tokens (full price) from cache-hit
// tokens (10x cheaper) and cache-write tokens (future discount), using
// configurable per-tier cache hit ratios.
//
// Design constraints (specs/cost-estimator.md §4):
//   - Only stdlib + github.com/spf13/cobra + gopkg.in/yaml.v3 may be imported.
//   - No hardcoded prices; all pricing comes from ~/.helix/pricing.yaml.
//   - Cost estimates are always rounded UP to the nearest cent.
//
// This file defines the core data models shared across the estimator,
// pricing, budget, reconciliation, and calibration layers.
package estimate

// ---------------------------------------------------------------------------
// Task classification
// ---------------------------------------------------------------------------

// TaskType classifies the kind of agent work being estimated. Different task
// types have very different token profiles (spec §7.2): a refactor reads far
// more context than a test run.
type TaskType string

const (
	// TaskSpec is a specification-writing task. Large output (output ratio 2.0).
	TaskSpec TaskType = "spec"
	// TaskCode is the default coding task. Balanced input/output.
	TaskCode TaskType = "code"
	// TaskReview is a code review. Low output ratio (0.3).
	TaskReview TaskType = "review"
	// TaskRefactor is a large-scale refactor. Highest input budget (200K base).
	TaskRefactor TaskType = "refactor"
	// TaskTest is a test-writing task. Smallest input budget (30K base).
	TaskTest TaskType = "test"
)

// Valid reports whether t is one of the recognized task types.
func (t TaskType) Valid() bool {
	switch t {
	case TaskSpec, TaskCode, TaskReview, TaskRefactor, TaskTest:
		return true
	}
	return false
}

// TaskDesc is the input to the estimator: a human task description plus the
// complexity signals that drive the token projection (spec §3.4).
//
// Zero-value defaults (applied during estimation, spec §3.5):
//   - Type:          code
//   - FilesChanged:  1
//   - MaxIterations: 20 (overridden by the task type's default when unset)
//   - DiffLines:     0
//   - Agents:        1 (capped at 5, spec §3.4)
type TaskDesc struct {
	Description   string   `json:"description"`
	Type          TaskType `json:"task_type"`
	Model         string   `json:"model"`    // e.g. "deepseek-v4-pro"
	Provider      string   `json:"provider"` // e.g. "deepseek"
	FilesChanged  int      `json:"files_changed"`
	MaxIterations int      `json:"max_iterations"`
	DiffLines     int      `json:"diff_lines"`
	Agents        int      `json:"agents"`
}

// ---------------------------------------------------------------------------
// Token + cost projections
// ---------------------------------------------------------------------------

// TokenEstimate is the projected token breakdown for a task. TotalInput is the
// sum of fresh and cache-hit input; cache writes are a subset of fresh input
// that gets written to cache for future discount (spec §7.1).
type TokenEstimate struct {
	TotalInput    int64   `json:"total_input"`    // FreshInput + CacheHits
	FreshInput    int64   `json:"fresh_input"`    // input served at full price
	CacheHits     int64   `json:"cache_hits"`     // input served from cache (10x cheaper)
	CacheWrites   int64   `json:"cache_writes"`   // fresh input written to cache
	Output        int64   `json:"output"`         // generated output tokens
	CacheHitRatio float64 `json:"cache_hit_ratio"` // ratio actually applied (0..1)
}

// CostEstimate is the projected USD cost of a task, broken down by input and
// output components. All values are in USD. CostTotal is rounded UP to the
// nearest cent per the operating contract (overestimate, never underestimate).
type CostEstimate struct {
	CostInput  float64 `json:"cost_input"`  // USD, all agents, unrounded
	CostOutput float64 `json:"cost_output"` // USD, all agents, unrounded
	CostTotal  float64 `json:"cost_total"`  // USD, rounded UP to nearest cent
	Tokens     TokenEstimate `json:"tokens"`
	Model      string  `json:"model"`
	Provider   string  `json:"provider"`
}

// ---------------------------------------------------------------------------
// Budget decisions
// ---------------------------------------------------------------------------

// ApprovalStatus enumerates the possible outcomes of a budget check.
type ApprovalStatus string

const (
	// StatusAutoApproved: cost fits within remaining budget. Proceeds immediately.
	StatusAutoApproved ApprovalStatus = "AUTO_APPROVED"
	// StatusAutoApprovedWithWarning: cost exceeds remaining but is within 1.5x
	// and the agent's trust level is >= 70. Proceeds; warning logged.
	StatusAutoApprovedWithWarning ApprovalStatus = "AUTO_APPROVED_WITH_WARNING"
	// StatusBlocked: cost exceeds remaining budget. Task may not proceed.
	StatusBlocked ApprovalStatus = "BLOCKED"
	// StatusEscalated: single task exceeds the weekly cap. Requires human approval.
	StatusEscalated ApprovalStatus = "ESCALATED"
)

// ApprovalDecision is the result of CheckBudget: whether the task may proceed,
// under which status, and why.
type ApprovalDecision struct {
	Approved bool            `json:"approved"`
	Status   ApprovalStatus  `json:"status"`
	Reason   string          `json:"reason"`
}

// ---------------------------------------------------------------------------
// Exit codes (spec §11)
// ---------------------------------------------------------------------------

// Exit codes are machine-readable so cron jobs can branch without parsing
// stderr. See specs/cost-estimator.md §11 for the full taxonomy.
const (
	// ExitSuccess: estimate/check/report completed without blocking the task.
	ExitSuccess = 0
	// ExitBudgetExhausted: estimated cost exceeds remaining budget.
	ExitBudgetExhausted = 1
	// ExitEstimationFailed: bad input (unknown model, invalid task type).
	ExitEstimationFailed = 2
	// ExitConfigError: missing/bad pricing file or known-friends registry.
	ExitConfigError = 3
	// ExitOpenRouterDown: OpenRouter API unreachable (network error).
	ExitOpenRouterDown = 4
	// ExitExceedsWeeklyCap: single task exceeds the weekly cap (escalated).
	ExitExceedsWeeklyCap = 5
	// ExitDryRun: informational — estimate was produced in dry-run mode (no enforcement).
	ExitDryRun = 10
)
