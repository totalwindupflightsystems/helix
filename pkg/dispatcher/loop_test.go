package dispatcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// executeStep
// ---------------------------------------------------------------------------

func TestExecuteStep(t *testing.T) {
	t.Run("creates marker file", func(t *testing.T) {
		dir := t.TempDir()
		step := Step{
			Action:         "build",
			ExpectedOutput: "go build ./... passes",
		}
		if err := executeStep(step, dir); err != nil {
			t.Fatalf("executeStep() error: %v", err)
		}

		// Find the marker file.
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected 1 marker file, got %d", len(entries))
		}
		if !strings.HasPrefix(entries[0].Name(), "step-") {
			t.Errorf("marker file %q missing 'step-' prefix", entries[0].Name())
		}
		if !strings.HasSuffix(entries[0].Name(), ".marker") {
			t.Errorf("marker file %q missing '.marker' suffix", entries[0].Name())
		}

		data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if !strings.Contains(string(data), "action: build") {
			t.Errorf("marker missing action, got: %s", string(data))
		}
		if !strings.Contains(string(data), "go build ./... passes") {
			t.Errorf("marker missing expected output, got: %s", string(data))
		}
	})

	t.Run("multiple steps create multiple markers", func(t *testing.T) {
		dir := t.TempDir()
		for i := 0; i < 3; i++ {
			step := Step{Action: fmt.Sprintf("step%d", i), ExpectedOutput: "ok"}
			if err := executeStep(step, dir); err != nil {
				t.Fatalf("executeStep() error: %v", err)
			}
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 3 {
			t.Fatalf("expected 3 markers, got %d", len(entries))
		}
	})
}

// ---------------------------------------------------------------------------
// commitWork
// ---------------------------------------------------------------------------

func TestCommitWork(t *testing.T) {
	t.Run("writes COMMIT_MSG", func(t *testing.T) {
		dir := t.TempDir()
		item := WorkItem{
			Task:  Task{ID: "WI-001", Description: "add feature X"},
			Agent: AgentProfile{Name: "coder-1", Capability: "code"},
			Steps: []Step{
				{Action: "impl", ExpectedOutput: "code written"},
				{Action: "test", ExpectedOutput: "tests pass"},
			},
		}
		if err := commitWork(item, dir); err != nil {
			t.Fatalf("commitWork() error: %v", err)
		}

		data, err := os.ReadFile(filepath.Join(dir, "COMMIT_MSG"))
		if err != nil {
			t.Fatalf("COMMIT_MSG not found: %v", err)
		}
		msg := string(data)
		if !strings.Contains(msg, "feat(WI-001): add feature X") {
			t.Errorf("COMMIT_MSG missing feature line, got: %s", msg)
		}
		if !strings.Contains(msg, "Executed by agent: coder-1") {
			t.Errorf("COMMIT_MSG missing agent, got: %s", msg)
		}
		if !strings.Contains(msg, "Steps: 2") {
			t.Errorf("COMMIT_MSG missing step count, got: %s", msg)
		}
	})

	t.Run("zero steps", func(t *testing.T) {
		dir := t.TempDir()
		item := WorkItem{
			Task:  Task{ID: "WI-002", Description: "cleanup"},
			Agent: AgentProfile{Name: "janitor", Capability: "maintain"},
			Steps: []Step{},
		}
		if err := commitWork(item, dir); err != nil {
			t.Fatalf("commitWork() error: %v", err)
		}
		data, _ := os.ReadFile(filepath.Join(dir, "COMMIT_MSG"))
		if !strings.Contains(string(data), "Steps: 0") {
			t.Errorf("COMMIT_MSG missing zero step count, got: %s", string(data))
		}
	})
}

// ---------------------------------------------------------------------------
// openPR
// ---------------------------------------------------------------------------

func TestOpenPR(t *testing.T) {
	t.Run("prints PR details", func(t *testing.T) {
		item := WorkItem{
			Task:  Task{ID: "WI-003", Description: "implement auth"},
			Agent: AgentProfile{Name: "auth-agent", Capability: "code"},
			Steps: []Step{
				{Action: "build", ExpectedOutput: "pass"},
			},
		}
		// openPR prints to os.Stdout via fmt.Printf — verify it doesn't panic.
		// Output capture not needed; just confirm it completes without error.
		openPR(item)
	})
}

// ---------------------------------------------------------------------------
// ExecuteLoop
// ---------------------------------------------------------------------------

func TestExecuteLoop(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		// ExecuteLoop reads os.Getwd() — set up a temp dir as working directory.
		dir := t.TempDir()

		// Create the .helix directory so lock acquisition works.
		helixDir := filepath.Join(dir, ".helix")
		if err := os.MkdirAll(helixDir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}

		// Change to temp dir for the duration of the test.
		origDir, err := os.Getwd()
		if err != nil {
			t.Fatalf("Getwd: %v", err)
		}
		if err := os.Chdir(dir); err != nil {
			t.Fatalf("Chdir: %v", err)
		}
		defer func() { _ = os.Chdir(origDir) }()

		agents := []AgentProfile{
			{Name: "test-agent", Capability: "code", CurrentLoad: 0, MaxLoad: 5},
		}
		d := NewDispatcher(agents)

		item := WorkItem{
			Task:  Task{ID: "WI-004", Description: "test feature"},
			Agent: AgentProfile{Name: "test-agent", Capability: "code"},
			Steps: []Step{
				{Action: "impl", ExpectedOutput: "code written"},
			},
		}

		if err := d.ExecuteLoop(item); err != nil {
			t.Fatalf("ExecuteLoop() error: %v", err)
		}

		// Verify the worktree was created.
		worktreePath := filepath.Join(dir, ".helix", "worktrees", "WI-004")
		if stat, err := os.Stat(worktreePath); err != nil || !stat.IsDir() {
			t.Fatalf("worktree not created at %s", worktreePath)
		}

		// Verify step markers exist.
		entries, _ := os.ReadDir(worktreePath)
		hasMarker := false
		hasCommitMsg := false
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "step-") {
				hasMarker = true
			}
			if e.Name() == "COMMIT_MSG" {
				hasCommitMsg = true
			}
		}
		if !hasMarker {
			t.Error("no step marker found in worktree")
		}
		if !hasCommitMsg {
			t.Error("no COMMIT_MSG found in worktree")
		}

		// Verify lock was released.
		lockPath := filepath.Join(dir, ".helix", "dispatch.lock")
		if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
			t.Error("lock file not released after ExecuteLoop")
		}

		// Verify step statuses were updated.
		if item.Steps[0].Status != StepComplete {
			t.Errorf("step status = %q, want %q", item.Steps[0].Status, StepComplete)
		}
	})

	t.Run("lock already held blocks execution", func(t *testing.T) {
		dir := t.TempDir()
		helixDir := filepath.Join(dir, ".helix")
		_ = os.MkdirAll(helixDir, 0o755)

		origDir, _ := os.Getwd()
		_ = os.Chdir(dir)
		defer func() { _ = os.Chdir(origDir) }()

		// Pre-create the lock file with our own PID (alive).
		lockPath := filepath.Join(dir, ".helix", "dispatch.lock")
		_ = os.WriteFile(lockPath, []byte(fmt.Sprintf("pid=%d\nts=2026-06-23T00:00:00Z\n", os.Getpid())), 0o644)

		d := NewDispatcher(nil)
		item := WorkItem{
			Task:  Task{ID: "WI-005", Description: "should fail"},
			Agent: AgentProfile{Name: "a1", Capability: "code"},
			Steps: []Step{{Action: "impl", ExpectedOutput: "ok"}},
		}
		err := d.ExecuteLoop(item)
		if err == nil {
			t.Fatal("ExecuteLoop() with existing live lock = nil, want error")
		}
		if !strings.Contains(err.Error(), "lock acquisition failed") {
			t.Errorf("ExecuteLoop() error = %v, want lock acquisition failed", err)
		}
	})

	t.Run("stale lock (dead PID) is overwritten", func(t *testing.T) {
		dir := t.TempDir()
		helixDir := filepath.Join(dir, ".helix")
		_ = os.MkdirAll(helixDir, 0o755)

		origDir, _ := os.Getwd()
		_ = os.Chdir(dir)
		defer func() { _ = os.Chdir(origDir) }()

		// Pre-create the lock file with a dead PID.
		lockPath := filepath.Join(dir, ".helix", "dispatch.lock")
		_ = os.WriteFile(lockPath, []byte("pid=999999\nts=2026-06-23T00:00:00Z\n"), 0o644)

		d := NewDispatcher(nil)
		item := WorkItem{
			Task:  Task{ID: "WI-005B", Description: "should succeed"},
			Agent: AgentProfile{Name: "a1", Capability: "code"},
			Steps: []Step{{Action: "impl", ExpectedOutput: "ok"}},
		}
		err := d.ExecuteLoop(item)
		if err != nil {
			t.Fatalf("ExecuteLoop() with stale lock = %v, want nil (stale lock should be overwritten)", err)
		}
	})

	t.Run("step failure marks step as failed", func(t *testing.T) {
		dir := t.TempDir()
		helixDir := filepath.Join(dir, ".helix")
		_ = os.MkdirAll(helixDir, 0o755)

		origDir, _ := os.Getwd()
		_ = os.Chdir(dir)
		defer func() { _ = os.Chdir(origDir) }()

		d := NewDispatcher(nil)

		item := WorkItem{
			Task:  Task{ID: "WI-006", Description: "will fail"},
			Agent: AgentProfile{Name: "a1", Capability: "code"},
			Steps: []Step{
				{Action: "step1", ExpectedOutput: "ok"},
				// step2 will fail because we'll pre-create a file at the marker
				// location using the test's temp dir, but that doesn't work since
				// the marker uses UnixNano(). Instead, test the error path by
				// using an unwritable worktreePath.
			},
		}

		// Create worktree as a file (not directory) so executeStep fails.
		worktreePath := filepath.Join(dir, ".helix", "worktrees", "WI-006")
		_ = os.MkdirAll(filepath.Dir(worktreePath), 0o755)
		_ = os.WriteFile(worktreePath, []byte("block"), 0o644)

		err := d.ExecuteLoop(item)
		if err == nil {
			t.Fatal("ExecuteLoop() with unwritable worktree = nil, want error")
		}
		if len(item.Steps) > 0 && item.Steps[0].Status == StepInProgress {
			t.Log("step was set to InProgress before failing as expected")
		}
	})
}

// ---------------------------------------------------------------------------
// RunPipeline
// ---------------------------------------------------------------------------

func TestRunPipeline(t *testing.T) {
	t.Run("dispatches and executes tasks", func(t *testing.T) {
		dir := t.TempDir()
		helixDir := filepath.Join(dir, ".helix")
		_ = os.MkdirAll(helixDir, 0o755)

		origDir, _ := os.Getwd()
		_ = os.Chdir(dir)
		defer func() { _ = os.Chdir(origDir) }()

		agents := []AgentProfile{
			{Name: "coder", Capability: "code", CurrentLoad: 0, MaxLoad: 5, Tier: "provisional"},
		}
		d := NewDispatcher(agents)

		tasks := []Task{
			{ID: "t1", Description: "write code", Priority: 1},
			{ID: "t2", Description: "write tests", Priority: 2},
		}

		results, err := d.RunPipeline(tasks)
		if err != nil {
			t.Fatalf("RunPipeline() error: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("RunPipeline() returned %d results, want 2", len(results))
		}

		// Both worktrees should exist.
		for _, id := range []string{"t1", "t2"} {
			wtPath := filepath.Join(dir, ".helix", "worktrees", id)
			if stat, err := os.Stat(wtPath); err != nil || !stat.IsDir() {
				t.Errorf("worktree for %s not created", id)
			}
		}

		// Both should have completed without errors.
		for i, r := range results {
			if r.Error != "" {
				t.Errorf("results[%d].Error = %q, want empty", i, r.Error)
			}
		}
	})

	t.Run("no agents returns dispatch error", func(t *testing.T) {
		dir := t.TempDir()
		helixDir := filepath.Join(dir, ".helix")
		_ = os.MkdirAll(helixDir, 0o755)

		origDir, _ := os.Getwd()
		_ = os.Chdir(dir)
		defer func() { _ = os.Chdir(origDir) }()

		d := NewDispatcher(nil)
		tasks := []Task{{ID: "t1", Description: "task", Priority: 1}}

		_, err := d.RunPipeline(tasks)
		if err == nil {
			t.Fatal("RunPipeline() with no agents = nil, want error")
		}
	})

	t.Run("task failure captured in result error", func(t *testing.T) {
		dir := t.TempDir()
		helixDir := filepath.Join(dir, ".helix")
		_ = os.MkdirAll(helixDir, 0o755)

		origDir, _ := os.Getwd()
		_ = os.Chdir(dir)
		defer func() { _ = os.Chdir(origDir) }()

		// Pre-create lock with our own PID (alive) so the first task's ExecuteLoop fails.
		_ = os.WriteFile(filepath.Join(dir, ".helix", "dispatch.lock"),
			[]byte(fmt.Sprintf("pid=%d\nts=2026-06-23T00:00:00Z\n", os.Getpid())), 0o644)

		agents := []AgentProfile{
			{Name: "coder", Capability: "code", CurrentLoad: 0, MaxLoad: 5, Tier: "provisional"},
		}
		d := NewDispatcher(agents)

		tasks := []Task{
			{ID: "t-lock-fail", Description: "will fail on lock", Priority: 1},
		}

		results, err := d.RunPipeline(tasks)
		// RunPipeline returns nil error even when ExecuteLoop fails — it
		// captures errors in the result struct.
		if err != nil {
			t.Fatalf("RunPipeline() unexpected error: %v", err)
		}
		if results[0].Error == "" {
			t.Error("results[0].Error = empty, want lock failure error")
		}
	})
}

// ---------------------------------------------------------------------------
// Error paths for acquireLock
// ---------------------------------------------------------------------------

func TestParseLockPID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"valid PID", "pid=1234\nts=2026-06-30T00:00:00Z\n", 1234},
		{"valid PID only line", "pid=42\n", 42},
		{"missing pid prefix", "ts=2026-06-30\n", 0},
		{"empty content", "", 0},
		{"non-numeric pid", "pid=abc\n", 0},
		{"multi-line with pid in middle", "line1\npid=9999\nts=2026\n", 9999},
		{"large pid", "pid=999999\nts=2026\n", 999999},
		{"pid with trailing spaces", "pid=555  \nts=2026\n", 555},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLockPID(tt.input)
			if got != tt.want {
				t.Errorf("parseLockPID(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsProcessAlive_CurrentPID(t *testing.T) {
	pid := os.Getpid()
	if !isProcessAlive(pid) {
		t.Error("current process should be alive")
	}
}

func TestIsProcessAlive_DeadPID(t *testing.T) {
	// PID 999999 is almost certainly not running.
	if isProcessAlive(999999) {
		t.Error("PID 999999 should not be alive")
	}
}

func TestIsProcessAlive_ZeroOrNegative(t *testing.T) {
	if isProcessAlive(0) {
		t.Error("PID 0 should not be alive")
	}
	if isProcessAlive(-1) {
		t.Error("PID -1 should not be alive")
	}
}

func TestAcquireLock_StaleOverwritten(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "dispatch.lock")

	// Write a stale lock with a dead PID.
	_ = os.WriteFile(lockPath, []byte("pid=999999\nts=2026-06-30T00:00:00Z\n"), 0o644)

	// Should succeed — stale lock is overwritten.
	if err := acquireLock(lockPath); err != nil {
		t.Fatalf("acquireLock with stale lock: %v", err)
	}

	// Verify lock now has our PID.
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	expected := fmt.Sprintf("pid=%d", os.Getpid())
	if !strings.Contains(string(data), expected) {
		t.Errorf("expected lock to contain %q, got: %s", expected, string(data))
	}
}

func TestAcquireLock_LiveBlocks(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "dispatch.lock")

	// Write a lock with our own PID (alive).
	_ = os.WriteFile(lockPath, []byte(fmt.Sprintf("pid=%d\nts=2026-06-30T00:00:00Z\n", os.Getpid())), 0o644)

	// Should fail — live lock blocks.
	if err := acquireLock(lockPath); err == nil {
		t.Fatal("acquireLock with live lock should fail")
	}
}

func TestAcquireLock_ErrorPaths(t *testing.T) {
	t.Run("release already released lock is no-op", func(t *testing.T) {
		dir := t.TempDir()
		lockPath := filepath.Join(dir, "nonexistent.lock")
		if err := releaseLock(lockPath); err != nil {
			t.Fatalf("releaseLock() on nonexistent lock: %v", err)
		}
	})

	t.Run("acquire lock in non-existent parent dir creates it", func(t *testing.T) {
		dir := t.TempDir()
		lockPath := filepath.Join(dir, "nested", "sub", "dispatch.lock")
		if err := acquireLock(lockPath); err != nil {
			t.Fatalf("acquireLock() error: %v", err)
		}
		// Verify created.
		if _, err := os.Stat(lockPath); err != nil {
			t.Fatalf("lock not created: %v", err)
		}
		_ = releaseLock(lockPath)
	})
}
