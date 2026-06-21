package dispatcher

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ExecuteLoop drives a single work item through the Ralph Loop pipeline:
//   1. Acquire a lock file to prevent concurrent pipeline runs.
//   2. Create a worktree / workspace directory for the work item.
//   3. Execute each step in the work item.
//   4. Commit the results.
//   5. Open a PR (stub — prints the PR details).
//   6. Release the lock.
//
// The lock file is placed at <workdir>/.helix/dispatch.lock. The worktree is
// created at <workdir>/.helix/worktrees/<task-id>/.
func (d *Dispatcher) ExecuteLoop(item WorkItem) error {
	workdir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("dispatcher: cannot determine working directory: %w", err)
	}

	lockPath := filepath.Join(workdir, ".helix", "dispatch.lock")
	worktreePath := filepath.Join(workdir, ".helix", "worktrees", item.Task.ID)

	// 1. Acquire lock
	if err := acquireLock(lockPath); err != nil {
		return fmt.Errorf("dispatcher: lock acquisition failed: %w", err)
	}
	defer func() { _ = releaseLock(lockPath) }()

	// 2. Create worktree directory
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		return fmt.Errorf("dispatcher: cannot create worktree: %w", err)
	}

	// 3. Execute steps
	for i, step := range item.Steps {
		item.Steps[i].Status = StepInProgress
		if err := executeStep(step, worktreePath); err != nil {
			item.Steps[i].Status = StepFailed
			return fmt.Errorf("dispatcher: step %d failed: %w", i, err)
		}
		item.Steps[i].Status = StepComplete
	}

	// 4. Commit (stub — in production this would call git commit)
	if err := commitWork(item, worktreePath); err != nil {
		return fmt.Errorf("dispatcher: commit failed: %w", err)
	}

	// 5. Open PR (stub — prints PR details)
	openPR(item)

	return nil
}

// RunPipeline dispatches all tasks and then executes each work item through
// the Ralph Loop sequentially. For parallel execution, call Dispatch and
// ExecuteLoop independently.
func (d *Dispatcher) RunPipeline(tasks []Task) ([]DispatchResult, error) {
	results, err := d.Dispatch(tasks, d.Agents)
	if err != nil {
		return nil, fmt.Errorf("dispatcher: dispatch failed: %w", err)
	}

	for i, result := range results {
		if result.Error != "" {
			continue
		}
		if err := d.ExecuteLoop(result.WorkItem); err != nil {
			results[i].Error = err.Error()
		}
	}

	return results, nil
}

// ---------------------------------------------------------------------------
// Internal helpers (stubs — wired to real implementation in later phases)
// ---------------------------------------------------------------------------

// acquireLock creates a lock file with the current PID and timestamp.
// If the lock already exists and is held by a live process, it returns
// an error.
func acquireLock(lockPath string) error {
	dir := filepath.Dir(lockPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create lock directory: %w", err)
	}

	// Check for existing lock.
	if data, err := os.ReadFile(lockPath); err == nil {
		// Lock file exists — in a real implementation we would check if
		// the PID is still alive. For now, we fail fast.
		return fmt.Errorf("lock already held: %s", string(data))
	}

	// Write lock file with PID and timestamp.
	pid := os.Getpid()
	ts := time.Now().UTC().Format(time.RFC3339)
	content := fmt.Sprintf("pid=%d\nts=%s\n", pid, ts)
	if err := os.WriteFile(lockPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("cannot write lock file: %w", err)
	}
	return nil
}

// releaseLock removes the lock file.
func releaseLock(lockPath string) error {
	if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot remove lock file: %w", err)
	}
	return nil
}

// executeStep runs a single step in the worktree. This is a stub that
// creates a marker file for each step.
func executeStep(step Step, worktreePath string) error {
	marker := filepath.Join(worktreePath, fmt.Sprintf("step-%d.marker", time.Now().UnixNano()))
	content := fmt.Sprintf("action: %s\nexpected: %s\n", step.Action, step.ExpectedOutput)
	if err := os.WriteFile(marker, []byte(content), 0o644); err != nil {
		return fmt.Errorf("cannot write step marker: %w", err)
	}
	return nil
}

// commitWork creates a commit for the work item. Stub — writes a commit
// message file.
func commitWork(item WorkItem, worktreePath string) error {
	msgPath := filepath.Join(worktreePath, "COMMIT_MSG")
	msg := fmt.Sprintf("feat(%s): %s\n\nExecuted by agent: %s\nSteps: %d\n",
		item.Task.ID, item.Task.Description, item.Agent.Name, len(item.Steps))
	if err := os.WriteFile(msgPath, []byte(msg), 0o644); err != nil {
		return fmt.Errorf("cannot write commit message: %w", err)
	}
	return nil
}

// openPR prints PR details for the work item. Stub — in production this
// would call the GitHub API.
func openPR(item WorkItem) {
	fmt.Printf("[PR] %s: %s (agent: %s, steps: %d)\n",
		item.Task.ID, item.Task.Description, item.Agent.Name, len(item.Steps))
}
