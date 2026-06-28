package integration

// ---------------------------------------------------------------------------
// Axiom Adapter — specs/integrations.md §6
// ---------------------------------------------------------------------------
//
// Axiom is the agent fleet management system. Decomposes human intent →
// spec extraction → meta-plan → work items → build loop → verification →
// adversarial review → PR. 60+ adversarial/quality agents with specs-as-
// contracts and evidence bundles.

// AxiomAdapter defines the contract for Axiom orchestration.
type AxiomAdapter interface {
	// Run executes the full Axiom pipeline on a work item.
	Run(intent string, repoPath string, opts RunOpts) (*AxiomResult, error)

	// Cmd executes a single Axiom command.
	Cmd(command string, repoPath string) (*CmdResult, error)

	// Status returns current Axiom pipeline status.
	Status(repoPath string) (*AxiomStatus, error)

	// ListWorkItems returns all work items for a repo.
	ListWorkItems(repoPath string) ([]WorkItem, error)
}

// RunOpts configures an Axiom pipeline run.
type RunOpts struct {
	InProcess      bool   // Run in-process (default: true)
	EntryCommand   string // Single phase only (e.g., "/axiom-step")
	NoBranch       bool   // Skip worktree branching
	Yes            bool   // Skip approval prompts
	OpenCodeURL    string // External OpenCode server URL
	SpinUpOpenCode bool   // Spin up fresh OpenCode server
}

// AxiomResult captures the outcome of a full Axiom pipeline run.
type AxiomResult struct {
	WorkItemID string
	Status     string // "complete", "failed", "blocked"
	Confidence float64
	Evidence   string // Path to evidence bundle
	PR         string // PR URL
	Cost       float64
	Duration   float64
}

// CmdResult captures the output of a single Axiom command.
type CmdResult struct {
	Status   string
	Output   string
	Duration float64
}

// AxiomStatus reports current pipeline state.
type AxiomStatus struct {
	ActiveRuns   int
	QueuedItems  int
	CurrentPhase string
	BlockedItems []string
}

// WorkItem represents a single Axiom-managed task.
type WorkItem struct {
	ID         string
	Title      string
	Status     string // "pending", "in_progress", "complete", "blocked"
	Priority   string
	Assignee   string // Agent name
	Confidence float64
}
