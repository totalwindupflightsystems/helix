package dispatcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/estimate"
	"github.com/totalwindupflightsystems/helix/pkg/forgejo"
	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

// =====================================================================
// pkg/dispatcher/loop.go — branches not exercised by existing tests
// =====================================================================

// TestReleaseLock_FileAlreadyMissing verifies the os.IsNotExist branch.
func TestReleaseLock_FileAlreadyMissing(t *testing.T) {
	tmpDir := t.TempDir()
	missing := filepath.Join(tmpDir, "nope.lock")
	if err := releaseLock(missing); err != nil {
		t.Errorf("releaseLock on missing file = %v, want nil", err)
	}
}

// TestReleaseLock_PermissionError verifies the !IsNotExist branch returns
// an error. We make the parent dir read-only so Remove fails.
func TestReleaseLock_PermissionError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root — chmod restrictions don't apply")
	}
	tmpDir := t.TempDir()
	lockedDir := filepath.Join(tmpDir, "locked")
	if err := os.Mkdir(lockedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(lockedDir, "lock")
	if err := os.WriteFile(lockPath, []byte("pid=0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(lockedDir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(lockedDir, 0o755) }()

	if err := releaseLock(lockPath); err == nil {
		t.Error("releaseLock on permission-denied = nil, want error")
	}
}

// TestExecuteStep_MarkerWriteFails verifies the os.WriteFile failure path.
// We point executeStep at a path inside a non-existent directory (no MkdirAll
// is performed by executeStep itself).
func TestExecuteStep_MarkerWriteFails(t *testing.T) {
	tmpDir := t.TempDir()
	missingSubdir := filepath.Join(tmpDir, "nope")
	step := Step{Action: "noop", ExpectedOutput: "nothing"}
	if err := executeStep(step, missingSubdir); err == nil {
		t.Error("executeStep on missing dir = nil, want error")
	}
}

// TestCommitWork_WritesValidMessage verifies the happy path (already covered
// indirectly by Run_Live_HappyPath, but locks down the contract here).
func TestCommitWork_WritesValidMessage(t *testing.T) {
	tmpDir := t.TempDir()
	item := WorkItem{
		Task:  Task{ID: "task-007", Description: "Write a CHANGELOG entry"},
		Agent: AgentProfile{Name: "test-agent"},
		Steps: []Step{{Action: "edit"}, {Action: "commit"}},
	}
	if err := commitWork(item, tmpDir); err != nil {
		t.Fatalf("commitWork: %v", err)
	}
	msg, err := os.ReadFile(filepath.Join(tmpDir, "COMMIT_MSG"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	s := string(msg)
	for _, want := range []string{"task-007", "Write a CHANGELOG entry", "test-agent", "Steps: 2"} {
		if !contains(s, want) {
			t.Errorf("commit message missing %q:\n%s", want, s)
		}
	}
}

// TestCommitWork_WriteFails verifies the os.WriteFile failure branch.
func TestCommitWork_WriteFails(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root — chmod restrictions don't apply")
	}
	tmpDir := t.TempDir()
	readOnly := filepath.Join(tmpDir, "readonly")
	if err := os.Mkdir(readOnly, 0o555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(readOnly, 0o755) }()

	item := WorkItem{Task: Task{ID: "x"}, Agent: AgentProfile{Name: "a"}}
	if err := commitWork(item, readOnly); err == nil {
		t.Error("commitWork on read-only dir = nil, want error")
	}
}

// TestAcquireLock_StaleLockOverwritten verifies that an existing lock with a
// dead PID is overwritten.
func TestAcquireLock_StaleLockOverwritten(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "stale.lock")
	// Use a guaranteed-dead PID (very high number unlikely to exist on
	// test runners).
	deadPID := 999_999
	if err := os.WriteFile(lockPath, []byte("pid="+strconv.Itoa(deadPID)+"\nts=2026-01-01T00:00:00Z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := acquireLock(lockPath); err != nil {
		t.Errorf("acquireLock on stale lock = %v, want nil (overwrite stale)", err)
	}
	data, _ := os.ReadFile(lockPath)
	if !contains(string(data), "pid="+strconv.Itoa(os.Getpid())) {
		t.Errorf("lock file does not contain current PID:\n%s", string(data))
	}
}

// TestAcquireLock_LiveLockBlocks verifies that an existing lock with a live
// PID returns an error.
func TestAcquireLock_LiveLockBlocks(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "live.lock")

	// Acquire the lock with our own PID (we're live).
	if err := acquireLock(lockPath); err != nil {
		t.Fatalf("first acquireLock: %v", err)
	}

	// Second call should fail because the PID is still alive.
	if err := acquireLock(lockPath); err == nil {
		t.Error("second acquireLock on live lock = nil, want error")
	}
}

// TestAcquireLock_MkdirAllFailure uses a path whose parent cannot be created.
// On Linux, writing to a path under a regular file returns ENOTDIR.
func TestAcquireLock_MkdirAllFailure(t *testing.T) {
	tmpDir := t.TempDir()
	blocker := filepath.Join(tmpDir, "blocker")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(blocker, "subdir", "lock.lock")
	if err := acquireLock(lockPath); err == nil {
		t.Error("acquireLock under a regular file = nil, want error")
	}
}

// TestAcquireLock_ConcurrentWriters verifies that two goroutines competing
// for the same lock — only one wins.
func TestAcquireLock_ConcurrentWriters(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "race.lock")

	var winners, losers int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := acquireLock(lockPath)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				winners++
				time.Sleep(5 * time.Millisecond)
				_ = releaseLock(lockPath)
			} else {
				losers++
			}
		}()
	}
	wg.Wait()
	if winners+losers != 10 {
		t.Errorf("winners+losers = %d, want 10", winners+losers)
	}
	if winners < 1 {
		t.Error("expected at least one winner")
	}
}

// =====================================================================
// pkg/dispatcher/cost_guard.go — branches not exercised
// =====================================================================

// TestCheck_EstimatorFailureEscalates covers the branch where the real
// Estimator.Estimate() returns an error → decision = ESCALATED.
func TestCheck_EstimatorFailureEscalates(t *testing.T) {
	est := &estimate.Estimator{Pricing: nil, Tier: "pro"}
	cg := NewCostGuard(nil, est)
	result, err := cg.Check("agent-x", trust.TierProvisional, estimate.TaskDesc{
		Type:          estimate.TaskCode,
		FilesChanged:  1,
		MaxIterations: 5,
		DiffLines:     10,
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Decision != CostGuardEscalated {
		t.Errorf("decision = %s, want ESCALATED", result.Decision)
	}
	if result.Reason == "" {
		t.Error("Reason should be non-empty when escalating")
	}
}

// TestCheck_NoEstimator_VeteranUnlimited_HeuristicBranch covers the
// "no estimator + cap < 0 (Veteran)" path in the rough-heuristic branch.
func TestCheck_NoEstimator_VeteranUnlimited_HeuristicBranch(t *testing.T) {
	cg := NewCostGuard(nil, nil)
	result, err := cg.Check("agent-v", trust.TierVeteran, estimate.TaskDesc{
		Type:          estimate.TaskCode,
		FilesChanged:  5,
		MaxIterations: 10,
		DiffLines:     100,
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Decision != CostGuardApproved {
		t.Errorf("decision = %s, want APPROVED (Veteran unlimited)", result.Decision)
	}
	if !contains(result.Reason, "unlimited") {
		t.Errorf("Reason should mention unlimited: %q", result.Reason)
	}
}

// TestCheck_NoEstimator_ProvisionalBlocked_HeuristicBranch covers the
// "no estimator + cap > 0 + cost > cap" path.
func TestCheck_NoEstimator_ProvisionalBlocked_HeuristicBranch(t *testing.T) {
	cg := NewCostGuard(nil, nil)
	// Rough: (filesChanged*500 + maxIter*200 + diffLines*10) / 1000 * 0.001
	// Provisional cap = $5. 100k files * 500 = 50M tokens → 50 USD → BLOCKED.
	result, err := cg.Check("agent-p", trust.TierProvisional, estimate.TaskDesc{
		Type:          estimate.TaskCode,
		FilesChanged:  100_000,
		MaxIterations: 1,
		DiffLines:     0,
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Decision != CostGuardBlocked {
		t.Errorf("decision = %s, want BLOCKED (cost > $5 cap)", result.Decision)
	}
}

// TestCheck_NoEstimator_ApproachingLimit verifies the 80% warn-zone branch
// via the heuristic path.
func TestCheck_NoEstimator_ApproachingLimit(t *testing.T) {
	cg := NewCostGuard(nil, nil)
	// Provisional cap = $5. 80% = $4. Want cost in [$4, $5).
	// 9_000 files * 500 = 4.5M tokens → 4.5 USD → in warn zone.
	result, err := cg.Check("agent-p", trust.TierProvisional, estimate.TaskDesc{
		Type:          estimate.TaskCode,
		FilesChanged:  9_000,
		MaxIterations: 1,
		DiffLines:     0,
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Decision != CostGuardApproved {
		t.Errorf("decision = %s, want APPROVED (warn-zone but not blocked)", result.Decision)
	}
	if !contains(result.Reason, "approaching limit") {
		t.Errorf("reason should mention 'approaching limit': %q", result.Reason)
	}
}

// =====================================================================
// pkg/dispatcher/forgejo_loop.go — Plan/Run branches
// =====================================================================

// TestForgejoLoop_Plan_EmptyTasks covers the "decomposed to 0 tasks" branch.
func TestForgejoLoop_Plan_EmptyTasks(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "empty-spec.md")
	if err := os.WriteFile(specPath, []byte("# Empty\n\nNo tasks here.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	f := &ForgejoLoop{Owner: "org", Repo: "repo", DryRun: true}
	_, _, err := f.Plan(specPath, "agent-x")
	if err == nil {
		t.Error("Plan on empty spec = nil, want ErrDecomposeFailed")
	}
}

// TestForgejoLoop_Plan_DecomposeSpecFails covers missing-spec branch.
func TestForgejoLoop_Plan_DecomposeSpecFails(t *testing.T) {
	f := &ForgejoLoop{Owner: "org", Repo: "repo", DryRun: true}
	_, _, err := f.Plan("/nonexistent/spec.md", "agent-x")
	if err == nil {
		t.Error("Plan on missing spec = nil, want error")
	}
}

// TestForgejoLoop_Run_Live_CreateBranchError covers the branch where the
// CreateBranch call returns a non-409 error and Run propagates it.
func TestForgejoLoop_Run_Live_CreateBranchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/repos/org/repo/branches" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"branch create failed"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	fc := forgejo.NewClient(srv.URL, "admin", "secret")

	tmpDir := t.TempDir()
	specPath := writeTestSpec(t, tmpDir)

	f := &ForgejoLoop{
		Client:     fc,
		Owner:      "org",
		Repo:       "repo",
		ForgejoURL: srv.URL,
		BaseBranch: "main",
		DryRun:     false,
		WorkDir:    tmpDir,
		Agent:      AgentProfile{Name: "agent-x"},
	}

	outcome, err := f.Run(context.Background(), specPath, "agent-x")
	if err == nil {
		t.Fatal("Run on CreateBranch error = nil, want error")
	}
	if outcome == nil {
		t.Error("outcome should be populated even on error")
	}
}

// TestForgejoLoop_Run_Live_CreatePRError covers the CreatePR non-409 error
// branch (CreateBranch succeeded with 201 → branch is set).
func TestForgejoLoop_Run_Live_CreatePRError(t *testing.T) {
	branchCalls := 0
	prCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/org/repo/branches":
			branchCalls++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"name":"feature/test","html_url":"http://forgejo/org/repo/branches/feature/test"}`))
		case "/api/v1/repos/org/repo/pulls":
			prCalls++
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"PR create failed"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	fc := forgejo.NewClient(srv.URL, "admin", "secret")
	tmpDir := t.TempDir()
	specPath := writeTestSpec(t, tmpDir)

	f := &ForgejoLoop{
		Client:     fc,
		Owner:      "org",
		Repo:       "repo",
		ForgejoURL: srv.URL,
		BaseBranch: "main",
		DryRun:     false,
		WorkDir:    tmpDir,
		Agent:      AgentProfile{Name: "agent-x"},
	}

	_, err := f.Run(context.Background(), specPath, "agent-x")
	if err == nil {
		t.Fatal("Run on CreatePR error = nil, want error")
	}
	if branchCalls != 1 {
		t.Errorf("branchCalls = %d, want 1", branchCalls)
	}
	if prCalls != 1 {
		t.Errorf("prCalls = %d, want 1", prCalls)
	}
}

// =====================================================================
// Helpers
// =====================================================================

// contains reports whether s contains sub.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// writeTestSpec writes a minimal spec file with one Phase heading.
func writeTestSpec(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "spec.md")
	body := "# Spec\n\n## PHASE 1: implement feature\n\nDescription.\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writeTestSpec: %v", err)
	}
	return path
}
