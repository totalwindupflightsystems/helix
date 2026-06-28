package integration

// ---------------------------------------------------------------------------
// Chimera Adapter — specs/integrations.md §2
// ---------------------------------------------------------------------------
//
// Chimera provides multi-model PR review for Helix. The formation engine
// designs custom DAGs, dispatches models by domain strength, judges merges
// with scoring, and independently audits verdicts. Called by Forgejo Actions
// on PR open/update, and by helix-negotiate for tie-break deliberation.

// ChimeraAdapter defines the contract for Chimera multi-model deliberation.
type ChimeraAdapter interface {
	// Review dispatches a full multi-model deliberation on a PR.
	Review(pr ChimeraPR, opts ReviewOpts) (*ChimeraVerdict, error)

	// Formations returns available deliberation formations.
	Formations() ([]Formation, error)

	// Models returns available models with category weights.
	Models() ([]ChimeraModel, error)

	// Health returns service health and version.
	Health() (*ChimeraHealth, error)
}

// ChimeraPR holds the information needed for a PR review deliberation.
type ChimeraPR struct {
	RepoOwner    string
	RepoName     string
	PRNumber     int
	Title        string
	Description  string
	Diff         string        // git diff of PR
	SpecFiles    []string      // paths to relevant spec files
	AgentReviews []AgentReview // existing agent reviews on the PR
}

// AgentReview represents a single agent's review on a PR.
type AgentReview struct {
	AgentName  string
	Verdict    string   // "APPROVED", "REQUEST_CHANGES", "COMMENT"
	Body       string
	Evidence   []string
	TrustLevel int
}

// ReviewOpts configures a Chimera review deliberation.
type ReviewOpts struct {
	Formation      string            // Formation preset name (default: "standard")
	MaxBudget      float64           // Max USD for this review
	StageModels    map[string]string // Override models per stage
	AllowCustomDAG bool              // Allow custom DAG (default: false)
}

// ChimeraVerdict is the outcome of a multi-model deliberation.
type ChimeraVerdict struct {
	Status     string  // "APPROVE", "REJECT", "NEEDS_MORE_EVIDENCE"
	Confidence float64 // 0.0-1.0
	Summary    string
	Findings   []Finding
	Trace      ChimeraTrace
	Cost       float64
}

// Finding is a single issue discovered during review.
type Finding struct {
	Severity    string // "CRITICAL", "HIGH", "MEDIUM", "LOW"
	Category    string // "security", "performance", "style", "logic", "spec_violation"
	File        string
	Line        int
	Description string
	Suggestion  string
}

// ChimeraTrace records the deliberation execution path.
type ChimeraTrace struct {
	Source      string       // "full" (multi-model) or "fallback" (single-model)
	Stages      []StageResult
	Duration    float64
	TotalTokens int
}

// StageResult records a single deliberation stage.
type StageResult struct {
	Stage    string
	Model    string
	Output   string
	Tokens   int
	Duration float64
}

// Formation is a named deliberation configuration preset.
type Formation struct {
	Name        string
	Description string
	Stages      int
}

// ChimeraModel represents an available LLM with its category and weight.
type ChimeraModel struct {
	Name     string
	Category string
	Weight   float64
}

// ChimeraHealth reports Chimera's operational status.
type ChimeraHealth struct {
	Status  string // "healthy", "degraded", "down"
	Version string
	Uptime  float64
	Models  int
}
