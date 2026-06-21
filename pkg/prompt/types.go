// Package prompt implements the Helix Prompt Registry — prompt storage,
// versioning, content-addressed hashing, lifecycle state machine, commit
// attestation, PromptFoo CI integration, and provenance chain verification.
//
// See specs/prompt-registry.md for the full design.
package prompt

import "time"

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// LifecycleStatus is a prompt lifecycle state per spec §7.
type LifecycleStatus string

const (
	StatusDraft      LifecycleStatus = "draft"
	StatusProposed   LifecycleStatus = "proposed"
	StatusReviewed   LifecycleStatus = "reviewed"
	StatusAttested   LifecycleStatus = "attested"
	StatusActive     LifecycleStatus = "active"
	StatusDeprecated LifecycleStatus = "deprecated"
	StatusRetired    LifecycleStatus = "retired"
)

// ---------------------------------------------------------------------------
// Metadata (spec §3.2)
// ---------------------------------------------------------------------------

// Cost holds estimated token and dollar costs for a prompt.
type Cost struct {
	EstimatedInputTokens  int     `yaml:"estimated_input_tokens" json:"estimated_input_tokens"`
	EstimatedOutputTokens int     `yaml:"estimated_output_tokens" json:"estimated_output_tokens"`
	EstimatedCostUSD      float64 `yaml:"estimated_cost_usd" json:"estimated_cost_usd"`
}

// CommitRef links a prompt version to a specific commit.
type CommitRef struct {
	SHA          string `yaml:"sha" json:"sha"`
	Repo         string `yaml:"repo" json:"repo"`
	FilesChanged int    `yaml:"files_changed" json:"files_changed"`
	Merged       bool   `yaml:"merged" json:"merged"`
}

// PromptfooResult records the last PromptFoo CI run for this prompt.
type PromptfooResult struct {
	TestSuite string    `yaml:"test_suite" json:"test_suite"`
	LastRun   time.Time `yaml:"last_run" json:"last_run"`
	Status    string    `yaml:"status" json:"status"`
}

// Metadata is the full metadata record stored in metadata.yaml (spec §3.2).
type Metadata struct {
	Version         string          `yaml:"version" json:"version"`
	Component       string          `yaml:"component" json:"component"`
	Hash            string          `yaml:"hash" json:"hash"`
	Model           string          `yaml:"model" json:"model"`
	Provider        string          `yaml:"provider" json:"provider"`
	Author          string          `yaml:"author" json:"author"`
	AuthorTrust     int             `yaml:"author_trust" json:"author_trust"`
	SpecRef         string          `yaml:"spec_ref" json:"spec_ref"`
	SpecVersion     string          `yaml:"spec_version" json:"spec_version"`
	WorkItem        string          `yaml:"work_item" json:"work_item"`
	CreatedAt       time.Time       `yaml:"created_at" json:"created_at"`
	AttestedAt      time.Time       `yaml:"attested_at,omitempty" json:"attested_at,omitempty"`
	Status          LifecycleStatus `yaml:"status" json:"status"`
	PreviousVersion string          `yaml:"previous_version,omitempty" json:"previous_version,omitempty"`
	Changes         string          `yaml:"changes,omitempty" json:"changes,omitempty"`
	Cost            Cost            `yaml:"cost,omitempty" json:"cost,omitempty"`
	Commits         []CommitRef     `yaml:"commits,omitempty" json:"commits,omitempty"`
	Promptfoo       PromptfooResult `yaml:"promptfoo,omitempty" json:"promptfoo,omitempty"`
}

// ---------------------------------------------------------------------------
// Registry Index (spec §3.1)
// ---------------------------------------------------------------------------

// Index is the registry index mapping component → version → entry.
type Index map[string]map[string]*PromptEntry

// PromptEntry is the lightweight index entry for a registered prompt version.
type PromptEntry struct {
	Hash     string          `yaml:"hash" json:"hash"`
	Status   LifecycleStatus `yaml:"status" json:"status"`
	Model    string          `yaml:"model" json:"model"`
	Provider string          `yaml:"provider" json:"provider"`
}

// PromptVersion is the resolved view of a registered prompt, returned by
// Lookup, List, and Register.
type PromptVersion struct {
	Version      string          `yaml:"version" json:"version"`
	Component    string          `yaml:"component" json:"component"`
	Hash         string          `yaml:"hash" json:"hash"`
	Status       LifecycleStatus `yaml:"status" json:"status"`
	Model        string          `yaml:"model,omitempty" json:"model,omitempty"`
	Provider     string          `yaml:"provider,omitempty" json:"provider,omitempty"`
	PromptPath   string          `yaml:"prompt_path" json:"prompt_path"`
	MetadataPath string          `yaml:"metadata_path" json:"metadata_path"`
}

// ---------------------------------------------------------------------------
// Lifecycle Transition
// ---------------------------------------------------------------------------

// Transition describes a rule in the lifecycle state machine.
type Transition struct {
	From    LifecycleStatus
	To      LifecycleStatus
	Allowed bool
	Reason  string
}

// ---------------------------------------------------------------------------
// Exit Codes (spec §13)
// ---------------------------------------------------------------------------

const (
	ExitOK                 = 0
	ExitAttestationMissing = 1
	ExitPromptNotFound     = 2
	ExitTamperDetected     = 3
	ExitLifecycleViolation = 4
	ExitPromptfooFailed    = 5
	ExitMetadataInvalid    = 6
	ExitDryRun             = 10
)

// ---------------------------------------------------------------------------
// Prompt (spec §3.1) — the core prompt artifact
// ---------------------------------------------------------------------------

// Prompt is the full prompt artifact stored in the registry.
type Prompt struct {
	Content   string `json:"content" yaml:"content"`
	Hash      string `json:"hash" yaml:"hash"`
	Component string `json:"component" yaml:"component"`
	Version   string `json:"version" yaml:"version"`
	Model     string `json:"model" yaml:"model"`
	Provider  string `json:"provider" yaml:"provider"`
}

// Version is a lightweight version identifier returned by ListVersions.
type Version struct {
	Version   string          `json:"version" yaml:"version"`
	Hash      string          `json:"hash" yaml:"hash"`
	Status    LifecycleStatus `json:"status" yaml:"status"`
	Model     string          `json:"model" yaml:"model"`
	Provider  string          `json:"provider" yaml:"provider"`
	CreatedAt string          `json:"created_at" yaml:"created_at"`
}

// PromptDiff holds the result of comparing two prompt versions (spec §18).
type PromptDiff struct {
	Component    string `json:"component" yaml:"component"`
	FromVersion  string `json:"from_version" yaml:"from_version"`
	ToVersion    string `json:"to_version" yaml:"to_version"`
	FromHash     string `json:"from_hash" yaml:"from_hash"`
	ToHash       string `json:"to_hash" yaml:"to_hash"`
	ContentDiff  string `json:"content_diff" yaml:"content_diff"`   // unified-diff text
	MetadataDiff string `json:"metadata_diff" yaml:"metadata_diff"` // key-level changes
}

// EvalResults holds parsed PromptFoo evaluation results (spec §10).
type EvalResults struct {
	TotalTests  int              `json:"total_tests" yaml:"total_tests"`
	PassedTests int              `json:"passed_tests" yaml:"passed_tests"`
	FailedTests int              `json:"failed_tests" yaml:"failed_tests"`
	Results     []EvalTestResult `json:"results" yaml:"results"`
}

// EvalTestResult is a single PromptFoo test result.
type EvalTestResult struct {
	Description string `json:"description" yaml:"description"`
	Passed      bool   `json:"passed" yaml:"passed"`
	Error       string `json:"error,omitempty" yaml:"error,omitempty"`
}
