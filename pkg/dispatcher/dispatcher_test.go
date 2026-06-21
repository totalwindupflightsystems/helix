package dispatcher

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// TaskStatus
// ---------------------------------------------------------------------------

func TestTaskStatus_IsValid(t *testing.T) {
	cases := []struct {
		name  string
		status TaskStatus
		want  bool
	}{
		{"pending", StatusPending, true},
		{"assigned", StatusAssigned, true},
		{"in_progress", StatusInProgress, true},
		{"complete", StatusComplete, true},
		{"failed", StatusFailed, true},
		{"empty", TaskStatus(""), false},
		{"bogus", TaskStatus("bogus"), false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.status.IsValid(); got != tc.want {
				t.Fatalf("TaskStatus(%q).IsValid() = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// StepStatus
// ---------------------------------------------------------------------------

func TestStepStatus_IsValid(t *testing.T) {
	cases := []struct {
		name  string
		status StepStatus
		want  bool
	}{
		{"pending", StepPending, true},
		{"in_progress", StepInProgress, true},
		{"complete", StepComplete, true},
		{"failed", StepFailed, true},
		{"empty", StepStatus(""), false},
		{"bogus", StepStatus("bogus"), false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.status.IsValid(); got != tc.want {
				t.Fatalf("StepStatus(%q).IsValid() = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// AgentProfile.CanAcceptLoad
// ---------------------------------------------------------------------------

func TestAgentProfile_CanAcceptLoad(t *testing.T) {
	cases := []struct {
		name  string
		agent AgentProfile
		want  bool
	}{
		{"has capacity", AgentProfile{Name: "a1", CurrentLoad: 2, MaxLoad: 5}, true},
		{"at max", AgentProfile{Name: "a2", CurrentLoad: 5, MaxLoad: 5}, false},
		{"over max", AgentProfile{Name: "a3", CurrentLoad: 6, MaxLoad: 5}, false},
		{"zero load", AgentProfile{Name: "a4", CurrentLoad: 0, MaxLoad: 3}, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.agent.CanAcceptLoad(); got != tc.want {
				t.Fatalf("CanAcceptLoad() = %v, want %v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// DecomposeSpec
// ---------------------------------------------------------------------------

func TestDecomposeSpec(t *testing.T) {
	t.Run("reads phase sections", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "test-spec.md")
		content := `# Test Spec

## PHASE 1: Setup
Create the project structure.

## PHASE 2: Implementation
Write the core logic.

## PHASE 3: Testing
Add unit tests.
`
		if err := os.WriteFile(specPath, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		tasks, err := DecomposeSpec(specPath)
		if err != nil {
			t.Fatalf("DecomposeSpec() error: %v", err)
		}
		if len(tasks) != 3 {
			t.Fatalf("DecomposeSpec() returned %d tasks, want 3", len(tasks))
		}
		if tasks[0].Priority != 1 {
			t.Errorf("task[0].Priority = %d, want 1", tasks[0].Priority)
		}
		if tasks[0].Status != StatusPending {
			t.Errorf("task[0].Status = %q, want %q", tasks[0].Status, StatusPending)
		}
		if tasks[0].SpecRef != specPath {
			t.Errorf("task[0].SpecRef = %q, want %q", tasks[0].SpecRef, specPath)
		}
	})

	t.Run("reads feature sections", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "feat-spec.md")
		content := `# Feature Spec

## Feature: Authentication
Implement login.

## Feature: Authorization
Add RBAC.
`
		if err := os.WriteFile(specPath, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		tasks, err := DecomposeSpec(specPath)
		if err != nil {
			t.Fatalf("DecomposeSpec() error: %v", err)
		}
		if len(tasks) != 2 {
			t.Fatalf("DecomposeSpec() returned %d tasks, want 2", len(tasks))
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := DecomposeSpec("/nonexistent/spec.md")
		if err == nil {
			t.Fatal("DecomposeSpec() = nil, want error")
		}
		if !errors.Is(err, ErrSpecNotFound) {
			t.Fatalf("DecomposeSpec() error = %v, want ErrSpecNotFound", err)
		}
	})

	t.Run("no phase or feature sections", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "empty.md")
		content := `# Empty Spec

## Overview
Nothing here.
`
		if err := os.WriteFile(specPath, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		_, err := DecomposeSpec(specPath)
		if err == nil {
			t.Fatal("DecomposeSpec() = nil, want error")
		}
		if !errors.Is(err, ErrDecomposeFailed) {
			t.Fatalf("DecomposeSpec() error = %v, want ErrDecomposeFailed", err)
		}
	})
}

// ---------------------------------------------------------------------------
// DecomposeTask
// ---------------------------------------------------------------------------

func TestDecomposeTask(t *testing.T) {
	t.Run("extracts action steps", func(t *testing.T) {
		desc := "Create the types file. Write the implementation. Add tests."
		steps, err := DecomposeTask(desc)
		if err != nil {
			t.Fatalf("DecomposeTask() error: %v", err)
		}
		if len(steps) != 3 {
			t.Fatalf("DecomposeTask() returned %d steps, want 3", len(steps))
		}
		for i, s := range steps {
			if s.Status != StepPending {
				t.Errorf("step[%d].Status = %q, want %q", i, s.Status, StepPending)
			}
		}
	})

	t.Run("empty description", func(t *testing.T) {
		_, err := DecomposeTask("")
		if err == nil {
			t.Fatal("DecomposeTask() = nil, want error")
		}
		if !errors.Is(err, ErrDecomposeFailed) {
			t.Fatalf("DecomposeTask() error = %v, want ErrDecomposeFailed", err)
		}
	})

	t.Run("no action verbs becomes single step", func(t *testing.T) {
		desc := "This is a description with no action verbs at all."
		steps, err := DecomposeTask(desc)
		if err != nil {
			t.Fatalf("DecomposeTask() error: %v", err)
		}
		if len(steps) != 1 {
			t.Fatalf("DecomposeTask() returned %d steps, want 1", len(steps))
		}
	})
}

// ---------------------------------------------------------------------------
// AssignAgent
// ---------------------------------------------------------------------------

func TestAssignAgent(t *testing.T) {
	agents := []AgentProfile{
		{Name: "coder", Capability: "code", CurrentLoad: 1, MaxLoad: 5},
		{Name: "reviewer", Capability: "review", CurrentLoad: 0, MaxLoad: 3},
		{Name: "tester", Capability: "test", CurrentLoad: 4, MaxLoad: 5},
	}

	t.Run("capability match", func(t *testing.T) {
		task := Task{ID: "t1", Description: "Write code for the new feature"}
		result, err := AssignAgent(task, agents)
		if err != nil {
			t.Fatalf("AssignAgent() error: %v", err)
		}
		if result.WorkItem.Agent.Name != "coder" {
			t.Errorf("AssignAgent() agent = %q, want %q", result.WorkItem.Agent.Name, "coder")
		}
	})

	t.Run("no agents", func(t *testing.T) {
		task := Task{ID: "t2", Description: "Some task"}
		_, err := AssignAgent(task, nil)
		if err == nil {
			t.Fatal("AssignAgent() = nil, want error")
		}
		if !errors.Is(err, ErrNoAgents) {
			t.Fatalf("AssignAgent() error = %v, want ErrNoAgents", err)
		}
	})

	t.Run("all agents overloaded", func(t *testing.T) {
		overloaded := []AgentProfile{
			{Name: "busy", Capability: "code", CurrentLoad: 5, MaxLoad: 5},
		}
		task := Task{ID: "t3", Description: "Write code"}
		_, err := AssignAgent(task, overloaded)
		if err == nil {
			t.Fatal("AssignAgent() = nil, want error")
		}
		if !errors.Is(err, ErrAgentOverloaded) {
			t.Fatalf("AssignAgent() error = %v, want ErrAgentOverloaded", err)
		}
	})

	t.Run("fallback to least loaded when no capability match", func(t *testing.T) {
		task := Task{ID: "t4", Description: "Deploy the infrastructure"}
		result, err := AssignAgent(task, agents)
		if err != nil {
			t.Fatalf("AssignAgent() error: %v", err)
		}
		// reviewer has load 0, which is lowest.
		if result.WorkItem.Agent.Name != "reviewer" {
			t.Errorf("AssignAgent() agent = %q, want %q (least loaded)", result.WorkItem.Agent.Name, "reviewer")
		}
	})
}

// ---------------------------------------------------------------------------
// Dispatcher.Dispatch
// ---------------------------------------------------------------------------

func TestDispatcher_Dispatch(t *testing.T) {
	t.Run("dispatches all tasks in priority order", func(t *testing.T) {
		agents := []AgentProfile{
			{Name: "a1", Capability: "code", CurrentLoad: 0, MaxLoad: 5},
		}
		d := NewDispatcher(agents)

		tasks := []Task{
			{ID: "t3", Description: "Third", Priority: 3},
			{ID: "t1", Description: "First", Priority: 1},
			{ID: "t2", Description: "Second", Priority: 2},
		}

		results, err := d.Dispatch(tasks, agents)
		if err != nil {
			t.Fatalf("Dispatch() error: %v", err)
		}
		if len(results) != 3 {
			t.Fatalf("Dispatch() returned %d results, want 3", len(results))
		}
		// Verify priority ordering: t1 first, t2 second, t3 third.
		if results[0].WorkItem.Task.ID != "t1" {
			t.Errorf("results[0].Task.ID = %q, want %q", results[0].WorkItem.Task.ID, "t1")
		}
		if results[2].WorkItem.Task.ID != "t3" {
			t.Errorf("results[2].Task.ID = %q, want %q", results[2].WorkItem.Task.ID, "t3")
		}
	})

	t.Run("no agents returns error", func(t *testing.T) {
		d := NewDispatcher(nil)
		tasks := []Task{{ID: "t1", Description: "Task", Priority: 1}}
		_, err := d.Dispatch(tasks, nil)
		if !errors.Is(err, ErrNoAgents) {
			t.Fatalf("Dispatch() error = %v, want ErrNoAgents", err)
		}
	})

	t.Run("respects load limits", func(t *testing.T) {
		agents := []AgentProfile{
			{Name: "a1", Capability: "code", CurrentLoad: 0, MaxLoad: 1},
		}
		d := NewDispatcher(agents)

		tasks := []Task{
			{ID: "t1", Description: "First task", Priority: 1},
			{ID: "t2", Description: "Second task", Priority: 2},
		}

		results, err := d.Dispatch(tasks, agents)
		if err != nil {
			t.Fatalf("Dispatch() error: %v", err)
		}
		// First task should succeed, second should fail (agent at max load).
		if results[0].Error != "" {
			t.Errorf("results[0] unexpected error: %s", results[0].Error)
		}
		if results[1].Error == "" {
			t.Error("results[1].Error = empty, want error (agent overloaded)")
		}
	})
}

// ---------------------------------------------------------------------------
// ExecuteLoop (lock acquisition)
// ---------------------------------------------------------------------------

func TestExecuteLoop_Lock(t *testing.T) {
	t.Run("acquire and release lock", func(t *testing.T) {
		dir := t.TempDir()
		lockPath := filepath.Join(dir, ".helix", "dispatch.lock")

		if err := acquireLock(lockPath); err != nil {
			t.Fatalf("acquireLock() error: %v", err)
		}

		// Lock file should exist.
		if _, err := os.Stat(lockPath); err != nil {
			t.Fatalf("stat lock file: %v", err)
		}

		// Second acquire should fail.
		if err := acquireLock(lockPath); err == nil {
			t.Fatal("second acquireLock() = nil, want error")
		}

		// Release.
		if err := releaseLock(lockPath); err != nil {
			t.Fatalf("releaseLock() error: %v", err)
		}

		// Lock file should be gone.
		if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
			t.Fatalf("lock file still exists after release")
		}
	})
}

// ---------------------------------------------------------------------------
// NewDispatcher
// ---------------------------------------------------------------------------

func TestNewDispatcher(t *testing.T) {
	agents := []AgentProfile{
		{Name: "a1", Capability: "code", CurrentLoad: 0, MaxLoad: 5},
	}
	d := NewDispatcher(agents)
	if d == nil {
		t.Fatal("NewDispatcher() = nil")
	}
	if len(d.Agents) != 1 {
		t.Fatalf("NewDispatcher() agents = %d, want 1", len(d.Agents))
	}
}
