// Package dispatcher — forgejo_loop.go
//
// Wires the Ralph Loop (ExecuteLoop in loop.go) to a live Forgejo instance
// via pkg/forgejo. The default ExecuteLoop opens "PRs" by writing a marker
// file and printing the details — fine for tests, useless for an actual
// spawn pipeline. ForgejoLoop replaces the commitWork + openPR stubs with
// real Forgejo calls.
//
// Lifecycle:
//
//  1. CreateBranch(owner, repo, "<branch-name>", baseRef)
//     - 409 = branch already exists → idempotent re-run, treat as success
//  2. CommitWork writes the per-step markers + commit msg to a worktree
//     (this is the default commitWork from loop.go; we don't change it —
//     in production the caller can replace it via WorkItemExecutor).
//  3. CreatePR(owner, repo, head, base, title, body)
//     - 409 = PR already exists → idempotent re-run, surface HTML_URL
//     - 422 = head==base or invalid branch names → fail loudly
//  4. Release the lock file.
//
// DryRun mode stops after CreateBranch planning and prints the would-be
// PR title/body — never touches the network. The DispatchOutcome in dry-run
// mode has PRURL == "" and BranchName populated.
//
// Standalone usage without provisioning:
//
//	The provisioner arg is optional. When nil, ForgejoLoop uses a static
//	agent profile constructed from the agentName argument, so the CLI can
//	drive ForgejoLoop without first calling identity.Provisioner. This
//	keeps the dispatch CLI runnable in dry-run or against a test mock
//	without requiring a live Forgejo user-record.
package dispatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/forgejo"
)

// ForgejoLoop wires a Forgejo client into the Ralph Loop.
// One ForgejoLoop is created per dispatch invocation and is NOT
// goroutine-safe — instantiate one per `helix dispatch` run.
type ForgejoLoop struct {
	// Client is the Forgejo REST client. Required for live mode.
	// May be nil when DryRun is true.
	Client *forgejo.Client

	// Owner is the GitHub-style owner namespace (org or user). Required.
	Owner string

	// Repo is the target repository on the Forgejo instance. Required.
	Repo string

	// BaseBranch is the target branch for PRs (default branch typically).
	// When empty, defaults to "main".
	BaseBranch string

	// Agent is the agent profile to attach to WorkItems. Required.
	Agent AgentProfile

	// DryRun skips all network I/O. The DispatchOutcome is populated with
	// the planned BranchName and a sentinel PRURL, but no actual
	// branch/PR is created.
	DryRun bool

	// WorkDir is the directory for the worktree + lock file. When empty,
	// defaults to the current working directory.
	WorkDir string

	// ForgejoURL is the human-readable URL to the Forgejo instance
	// (e.g. "http://localhost:3030"). Used only to construct placeholder
	// URLs in dry-run mode. Optional.
	ForgejoURL string
}

// DispatchOutcome is the JSON-serialisable result of a single dispatch run.
// Distinct from `DispatchResult` (the existing types.go struct used by the
// in-process Dispatcher) because this one captures the Forgejo-loop
// specifics: branch name, PR URL, dry-run flag, lock state.
type DispatchOutcome struct {
	SpecPath     string `json:"spec_path"`
	Agent        string `json:"agent"`
	TaskID       string `json:"task_id"`
	TaskDesc     string `json:"task_description"`
	Steps        []Step `json:"steps"`
	BranchName   string `json:"branch_name"`
	BaseBranch   string `json:"base_branch"`
	PRNumber     int64  `json:"pr_number,omitempty"`
	PRURL        string `json:"pr_url,omitempty"`
	DryRun       bool   `json:"dry_run"`
	Mode         string `json:"mode"` // "live" | "dry-run"
	StartedAt    string `json:"started_at"`
	CompletedAt  string `json:"completed_at"`
	LockPath     string `json:"lock_path"`
	WorktreePath string `json:"worktree_path"`
	ForgejoURL   string `json:"forgejo_url,omitempty"`
}

// BranchName returns the canonical feature branch name for a (agent, taskID)
// pair. Format: feature/<agent>-<taskID>. The agent name is sanitised to
// remove path separators so it can't escape the refs/heads namespace.
func (f *ForgejoLoop) BranchName(agent, taskID string) string {
	safeAgent := sanitizeBranchComponent(agent)
	safeTask := sanitizeBranchComponent(taskID)
	if safeAgent == "" {
		safeAgent = "agent"
	}
	if safeTask == "" {
		safeTask = "task"
	}
	return fmt.Sprintf("feature/%s-%s", safeAgent, safeTask)
}

// ---------------------------------------------------------------------------
// Spec → WorkItem
// ---------------------------------------------------------------------------

// Plan loads a spec markdown file and creates a single-work-item dispatch
// plan for `agent` against the first task the decomposer extracts. This is
// the path used by `helix dispatch --spec <path> --agent <name>`.
//
// If the spec decomposes to multiple tasks, only the first is dispatched —
// the dispatch CLI drives one task per invocation. Use the Dispatcher
// directly for batch.
func (f *ForgejoLoop) Plan(specPath, agent string) (Task, []Step, error) {
	tasks, err := DecomposeSpec(specPath)
	if err != nil {
		return Task{}, nil, fmt.Errorf("forgejo_loop: decompose spec %s: %w", specPath, err)
	}
	if len(tasks) == 0 {
		return Task{}, nil, fmt.Errorf("%w: spec %s produced no tasks", ErrDecomposeFailed, specPath)
	}
	task := tasks[0]
	task.AssignedAgent = agent
	steps, err := DecomposeTask(task.Description)
	if err != nil {
		return Task{}, nil, fmt.Errorf("forgejo_loop: decompose task %s: %w", task.ID, err)
	}
	return task, steps, nil
}

// ---------------------------------------------------------------------------
// Run
// ---------------------------------------------------------------------------

// Run executes the full pipeline: spec → lock → worktree → commit
// (stub) → CreateBranch → CreatePR → unlock. On dry-run it stops at the
// "would-be PR" stage and returns a DispatchOutcome with PRURL containing
// the placeholder URL — never touches the network.
//
// Returns the populated DispatchOutcome and any error. A non-error return
// value with PRURL set means a PR was successfully opened (or, in
// dry-run, the planned URL). An error from a Forgejo call bubbles up
// unchanged so the CLI can render it.
func (f *ForgejoLoop) Run(ctx context.Context, specPath, agent string) (*DispatchOutcome, error) {
	if f == nil {
		return nil, fmt.Errorf("forgejo_loop: nil receiver")
	}
	if f.Client == nil && !f.DryRun {
		return nil, fmt.Errorf("forgejo_loop: Client is required for non-dry-run mode")
	}
	if f.Owner == "" {
		return nil, fmt.Errorf("forgejo_loop: Owner is required")
	}
	if !f.DryRun && f.Repo == "" {
		return nil, fmt.Errorf("forgejo_loop: Repo is required")
	}

	workDir := f.WorkDir
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("forgejo_loop: cannot determine workdir: %w", err)
		}
	}
	baseBranch := f.BaseBranch
	if baseBranch == "" {
		baseBranch = "main"
	}

	// 1. Plan
	task, steps, err := f.Plan(specPath, agent)
	if err != nil {
		return nil, err
	}

	outcome := &DispatchOutcome{
		SpecPath:     specPath,
		Agent:        agent,
		TaskID:       task.ID,
		TaskDesc:     task.Description,
		Steps:        steps,
		BranchName:   f.BranchName(agent, task.ID),
		BaseBranch:   baseBranch,
		DryRun:       f.DryRun,
		StartedAt:    time.Now().UTC().Format(time.RFC3339),
		WorktreePath: filepath.Join(workDir, ".helix", "worktrees", task.ID),
		LockPath:     filepath.Join(workDir, ".helix", "dispatch.lock"),
		ForgejoURL:   f.ForgejoURL,
	}

	if f.DryRun {
		outcome.Mode = "dry-run"
		outcome.PRURL = f.dryRunURL(task)
		outcome.CompletedAt = time.Now().UTC().Format(time.RFC3339)
		return outcome, nil
	}

	outcome.Mode = "live"

	// 2. Acquire lock
	if err := acquireLock(outcome.LockPath); err != nil {
		return nil, fmt.Errorf("forgejo_loop: lock acquisition failed: %w", err)
	}
	defer func() { _ = releaseLock(outcome.LockPath) }()

	// 3. Create worktree directory
	if err := os.MkdirAll(outcome.WorktreePath, 0o755); err != nil {
		return nil, fmt.Errorf("forgejo_loop: cannot create worktree: %w", err)
	}

	// 4. Execute steps (stub markers, same as the default ExecuteLoop).
	for i := range steps {
		steps[i].Status = StepInProgress
		if err := executeStep(steps[i], outcome.WorktreePath); err != nil {
			steps[i].Status = StepFailed
			outcome.Steps = steps
			return outcome, fmt.Errorf("forgejo_loop: step %d failed: %w", i, err)
		}
		steps[i].Status = StepComplete
	}
	outcome.Steps = steps

	// 5. Commit stub.
	wi := WorkItem{Task: task, Agent: AgentProfile{Name: agent}, Steps: steps}
	if err := commitWork(wi, outcome.WorktreePath); err != nil {
		return outcome, fmt.Errorf("forgejo_loop: commit failed: %w", err)
	}

	// 6. Create branch — idempotent on 409.
	branch, err := f.Client.CreateBranch(ctx, f.Owner, f.Repo, outcome.BranchName, baseBranch)
	if err != nil && !forgejo.IsAlreadyExists(err) {
		return outcome, fmt.Errorf("forgejo_loop: CreateBranch failed: %w", err)
	}
	if branch != nil {
		outcome.PRURL = branch.HTMLURL
	}

	// 7. Open PR — idempotent on 409.
	prTitle := fmt.Sprintf("[%s] %s", task.ID, task.Description)
	prBody := fmt.Sprintf("Automated PR for %s by agent %s.\n\nSpec: %s\nBranch: %s\nBase: %s\n",
		task.ID, agent, specPath, outcome.BranchName, baseBranch)
	pr, prErr := f.Client.CreatePR(ctx, f.Owner, f.Repo,
		outcome.BranchName, baseBranch, prTitle, prBody)
	if prErr != nil && !forgejo.IsAlreadyExists(prErr) {
		return outcome, fmt.Errorf("forgejo_loop: CreatePR failed: %w", prErr)
	}
	if pr != nil {
		outcome.PRNumber = pr.Number
		outcome.PRURL = pr.HTMLURL
	}

	outcome.CompletedAt = time.Now().UTC().Format(time.RFC3339)
	return outcome, nil
}

// Marshal returns a stable JSON encoding for CLI output.
func (r *DispatchOutcome) Marshal() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (f *ForgejoLoop) dryRunURL(task Task) string {
	base := f.ForgejoURL
	if base == "" {
		base = "http://forgejo.example"
	}
	return fmt.Sprintf("%s/%s/%s/compare/%s...%s",
		base, f.Owner, f.Repo, f.BaseBranch, f.BranchName(f.Agent.Name, task.ID))
}

// sanitizeBranchComponent removes characters that are illegal in
// refs/heads/* names per git's check-ref-format rules. We restrict to
// alphanumeric + dash + underscore + dot, which is safe for any
// Forgejo installation.
func sanitizeBranchComponent(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}
