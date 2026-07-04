package dispatcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/totalwindupflightsystems/helix/pkg/forgejo"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// mockSpec writes a tiny spec to a temp file and returns the path.
// DecomposeSpec looks for "PHASE" or "FEATURE" in ## headings.
func mockSpec(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.md")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

const sampleSpec = `# Sample Spec

## PHASE 1: Implement wire-dispatcher

The dispatcher must call Forgejo to open a PR.

## PHASE 2: Add dry-run

Add a --dry-run flag.

## PHASE 3: Verify tests

Add httptest coverage for the wiring.
`

// mockForgejoServer is a Forgejo mock that records every call and lets the
// test register pre-canned responses.
type mockForgejoServer struct {
	*httptest.Server
	branchCalls int
	prCalls     int
	lastBranch  string
	lastPRTitle string
}

func newMockForgejoServer(t *testing.T) *mockForgejoServer {
	t.Helper()
	mf := &mockForgejoServer{}
	mf.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Auth check (matches newMockForgejo in pkg/forgejo/client_test.go).
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/branches") && r.Method == http.MethodPost:
			mf.branchCalls++
			mf.lastBranch = r.URL.Path
			writeJSONHelper(w, http.StatusCreated, map[string]any{
				"name":       "feature/test-agent-task-001",
				"ref":        "refs/heads/feature/test-agent-task-001",
				"commit_sha": "deadbeef",
				"html_url":   mf.Server.URL + "/org/repo/branches/feature/test-agent-task-001",
			})
		case strings.HasSuffix(r.URL.Path, "/pulls") && r.Method == http.MethodPost:
			mf.prCalls++
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			mf.lastPRTitle = body["title"]
			writeJSONHelper(w, http.StatusCreated, map[string]any{
				"id":       1,
				"number":   42,
				"state":    "open",
				"head_ref": body["head"],
				"base_ref": body["base"],
				"title":    body["title"],
				"html_url": mf.Server.URL + "/org/repo/pulls/42",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	return mf
}

func writeJSONHelper(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ---------------------------------------------------------------------------
// BranchName
// ---------------------------------------------------------------------------

func TestForgejoLoop_BranchName_Simple(t *testing.T) {
	f := &ForgejoLoop{}
	got := f.BranchName("test-agent", "task-001")
	assert.Equal(t, "feature/test-agent-task-001", got)
}

func TestForgejoLoop_BranchName_Sanitises(t *testing.T) {
	f := &ForgejoLoop{}
	// Slashes, dots, spaces get squashed to dashes.
	got := f.BranchName("with/slashes and spaces", "task/with.dot")
	// Slashes in component → dashes; spaces → dashes; dots inside branch
	// name can be ambiguous in some Forgejo versions but git-allowed.
	assert.NotContains(t, got, "//", "no double slashes")
	assert.True(t, strings.HasPrefix(got, "feature/"))
	assert.NotContains(t, got, " ", "no spaces in branch name")
}

func TestForgejoLoop_BranchName_Empty(t *testing.T) {
	f := &ForgejoLoop{}
	got := f.BranchName("", "")
	assert.Equal(t, "feature/agent-task", got, "empty components fall back to defaults")
}

// ---------------------------------------------------------------------------
// Plan
// ---------------------------------------------------------------------------

func TestForgejoLoop_Plan_FirstTask(t *testing.T) {
	specPath := mockSpec(t, sampleSpec)
	f := &ForgejoLoop{}
	task, steps, err := f.Plan(specPath, "test-agent")
	require.NoError(t, err)
	assert.Equal(t, "test-agent", task.AssignedAgent)
	assert.NotEmpty(t, task.ID)
	assert.NotEmpty(t, steps)
}

func TestForgejoLoop_Plan_NoSpecFile(t *testing.T) {
	f := &ForgejoLoop{}
	_, _, err := f.Plan("/nonexistent/file.md", "test-agent")
	require.Error(t, err)
}

func TestForgejoLoop_Plan_NoTasks(t *testing.T) {
	// Spec file with no Phase/Feature headings.
	specPath := mockSpec(t, "# Just a title\n\nNo phases here.\n")
	f := &ForgejoLoop{}
	_, _, err := f.Plan(specPath, "test-agent")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDecomposeFailed)
}

// ---------------------------------------------------------------------------
// Dry-run mode
// ---------------------------------------------------------------------------

func TestForgejoLoop_Run_DryRun(t *testing.T) {
	specPath := mockSpec(t, sampleSpec)
	workDir := t.TempDir()

	f := &ForgejoLoop{
		Owner:      "helix-org",
		Repo:       "helix",
		BaseBranch: "main",
		Agent:      AgentProfile{Name: "test-agent", Capability: "go", MaxLoad: 1},
		DryRun:     true,
		WorkDir:    workDir,
		ForgejoURL: "http://localhost:3030",
	}

	outcome, err := f.Run(context.Background(), specPath, "test-agent")
	require.NoError(t, err)
	require.NotNil(t, outcome)

	assert.True(t, outcome.DryRun)
	assert.Equal(t, "dry-run", outcome.Mode)
	assert.Equal(t, specPath, outcome.SpecPath)
	assert.Equal(t, "test-agent", outcome.Agent)
	assert.Equal(t, "main", outcome.BaseBranch)
	assert.NotEmpty(t, outcome.BranchName)
	assert.True(t, strings.HasPrefix(outcome.BranchName, "feature/test-agent-"))
	assert.Contains(t, outcome.PRURL, "http://localhost:3030")
	assert.Contains(t, outcome.PRURL, "compare/main...")
	assert.NotEmpty(t, outcome.StartedAt)
	assert.NotEmpty(t, outcome.CompletedAt)
	assert.NotEmpty(t, outcome.LockPath)
}

// ---------------------------------------------------------------------------
// Live-mode (httptest mock)
// ---------------------------------------------------------------------------

func TestForgejoLoop_Run_Live_HappyPath(t *testing.T) {
	specPath := mockSpec(t, sampleSpec)
	workDir := t.TempDir()

	mf := newMockForgejoServer(t)
	defer mf.Close()

	c := forgejo.NewClient(mf.URL, "admin", "secret")
	f := &ForgejoLoop{
		Client:     c,
		Owner:      "helix-org",
		Repo:       "helix",
		BaseBranch: "main",
		Agent:      AgentProfile{Name: "test-agent", Capability: "go", MaxLoad: 1},
		DryRun:     false,
		WorkDir:    workDir,
	}

	outcome, err := f.Run(context.Background(), specPath, "test-agent")
	require.NoError(t, err)
	require.NotNil(t, outcome)

	assert.Equal(t, "live", outcome.Mode)
	assert.False(t, outcome.DryRun)
	assert.Equal(t, int64(42), outcome.PRNumber)
	assert.Contains(t, outcome.PRURL, "/pulls/42")
	assert.Equal(t, 1, mf.branchCalls, "exactly one branch POST")
	assert.Equal(t, 1, mf.prCalls, "exactly one PR POST")
	assert.Contains(t, mf.lastPRTitle, "task-001") // from sampleSpec PHASE 1
	assert.True(t, strings.HasSuffix(outcome.LockPath, "dispatch.lock"))
}

// ---------------------------------------------------------------------------
// Idempotency — branch already exists
// ---------------------------------------------------------------------------

func TestForgejoLoop_Run_BranchAlreadyExists(t *testing.T) {
	specPath := mockSpec(t, sampleSpec)
	workDir := t.TempDir()

	mf := newMockForgejoServer(t)
	defer mf.Close()

	// Replace branch handler with a 409 conflict.
	mf.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _, _ := r.BasicAuth()
		if user != "admin" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/branches") && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"branch already exists"}`))
		case strings.HasSuffix(r.URL.Path, "/pulls") && r.Method == http.MethodPost:
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			writeJSONHelper(w, http.StatusCreated, map[string]any{
				"id": 2, "number": 100, "state": "open",
				"head_ref": body["head"], "base_ref": body["base"],
				"title":    body["title"],
				"html_url": mf.URL + "/org/repo/pulls/100",
			})
		default:
			http.NotFound(w, r)
		}
	})

	c := forgejo.NewClient(mf.URL, "admin", "secret")
	f := &ForgejoLoop{
		Client: c, Owner: "helix-org", Repo: "helix", BaseBranch: "main",
		Agent: AgentProfile{Name: "test-agent"}, DryRun: false, WorkDir: workDir,
	}

	outcome, err := f.Run(context.Background(), specPath, "test-agent")
	require.NoError(t, err, "409 on branch should be treated as idempotent success")
	assert.Equal(t, int64(100), outcome.PRNumber)
	assert.Contains(t, outcome.PRURL, "/pulls/100")
}

// ---------------------------------------------------------------------------
// Idempotency — PR already exists
// ---------------------------------------------------------------------------

func TestForgejoLoop_Run_PRAlreadyExists(t *testing.T) {
	specPath := mockSpec(t, sampleSpec)
	workDir := t.TempDir()

	mf := newMockForgejoServer(t)
	defer mf.Close()

	mf.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _, _ := r.BasicAuth()
		if user != "admin" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/branches") && r.Method == http.MethodPost:
			writeJSONHelper(w, http.StatusCreated, map[string]any{
				"name": "feature/test-agent-task-001", "ref": "refs/heads/feature/test-agent-task-001",
				"html_url": mf.URL + "/branch",
			})
		case strings.HasSuffix(r.URL.Path, "/pulls") && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"pull request already exists"}`))
		default:
			http.NotFound(w, r)
		}
	})

	c := forgejo.NewClient(mf.URL, "admin", "secret")
	f := &ForgejoLoop{
		Client: c, Owner: "helix-org", Repo: "helix", BaseBranch: "main",
		Agent: AgentProfile{Name: "test-agent"}, DryRun: false, WorkDir: workDir,
	}

	outcome, err := f.Run(context.Background(), specPath, "test-agent")
	require.NoError(t, err, "409 on PR should be treated as idempotent success")
	assert.NotEmpty(t, outcome.BranchName)
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func TestForgejoLoop_Run_MissingClient_LiveMode(t *testing.T) {
	specPath := mockSpec(t, sampleSpec)
	f := &ForgejoLoop{
		Owner: "helix-org", Repo: "helix", BaseBranch: "main",
		Agent: AgentProfile{Name: "x"}, DryRun: false, WorkDir: t.TempDir(),
	}
	_, err := f.Run(context.Background(), specPath, "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Client is required")
}

func TestForgejoLoop_Run_MissingOwner(t *testing.T) {
	specPath := mockSpec(t, sampleSpec)
	f := &ForgejoLoop{
		Client: forgejo.NewClient("http://x", "u", "p"), Repo: "helix",
		BaseBranch: "main", Agent: AgentProfile{Name: "x"},
		DryRun: true, WorkDir: t.TempDir(),
	}
	_, err := f.Run(context.Background(), specPath, "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Owner is required")
}

func TestForgejoLoop_Run_MissingRepo_LiveMode(t *testing.T) {
	// Live mode requires Repo even when other validation passes.
	specPath := mockSpec(t, sampleSpec)
	f := &ForgejoLoop{
		Client: forgejo.NewClient("http://x", "u", "p"), Owner: "o",
		BaseBranch: "main", Agent: AgentProfile{Name: "x"},
		// Repo omitted, DryRun false → should error
		DryRun: false, WorkDir: t.TempDir(),
	}
	_, err := f.Run(context.Background(), specPath, "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Repo is required")
}

func TestForgejoLoop_Run_MissingRepo_DryRunOK(t *testing.T) {
	// Dry-run doesn't need Repo — verification is local.
	specPath := mockSpec(t, sampleSpec)
	f := &ForgejoLoop{
		Owner: "o", BaseBranch: "main",
		Agent: AgentProfile{Name: "x"}, DryRun: true, WorkDir: t.TempDir(),
		// Repo omitted, Client nil — should still plan
	}
	outcome, err := f.Run(context.Background(), specPath, "x")
	require.NoError(t, err)
	assert.NotNil(t, outcome)
	assert.True(t, outcome.DryRun)
}

func TestForgejoLoop_Run_DefaultsBaseBranch(t *testing.T) {
	specPath := mockSpec(t, sampleSpec)
	f := &ForgejoLoop{
		Client: forgejo.NewClient("http://x", "u", "p"), Owner: "o", Repo: "r",
		Agent: AgentProfile{Name: "x"}, DryRun: true, WorkDir: t.TempDir(),
		// BaseBranch intentionally empty
	}
	outcome, err := f.Run(context.Background(), specPath, "x")
	require.NoError(t, err)
	assert.Equal(t, "main", outcome.BaseBranch)
}

// ---------------------------------------------------------------------------
// Marshal
// ---------------------------------------------------------------------------

func TestDispatchOutcome_Marshal(t *testing.T) {
	o := &DispatchOutcome{
		SpecPath:   "specs/agent-identity.md",
		Agent:      "test-agent",
		TaskID:     "task-001",
		TaskDesc:   "Implement wire-dispatcher",
		BranchName: "feature/test-agent-task-001",
		BaseBranch: "main",
		DryRun:     true,
		Mode:       "dry-run",
	}
	data, err := o.Marshal()
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "test-agent", parsed["agent"])
	assert.Equal(t, "feature/test-agent-task-001", parsed["branch_name"])
	assert.Equal(t, "dry-run", parsed["mode"])
}

// ---------------------------------------------------------------------------
// Lock release on error path
// ---------------------------------------------------------------------------

func TestForgejoLoop_Run_LockReleasedOnCreateBranchError(t *testing.T) {
	// When CreateBranch fails with a non-409 error, the defer
	// should release the lock and return the error to the caller.
	specPath := mockSpec(t, sampleSpec)
	workDir := t.TempDir()

	mf := newMockForgejoServer(t)
	defer mf.Close()
	mf.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"forgejo down"}`))
	})

	c := forgejo.NewClient(mf.URL, "admin", "secret")
	f := &ForgejoLoop{
		Client: c, Owner: "helix-org", Repo: "helix", BaseBranch: "main",
		Agent: AgentProfile{Name: "test-agent"}, DryRun: false, WorkDir: workDir,
	}

	_, err := f.Run(context.Background(), specPath, "test-agent")
	require.Error(t, err, "5xx should bubble up as a real failure")

	// Lock file should be released (defer-fired).
	lockPath := filepath.Join(workDir, ".helix", "dispatch.lock")
	_, statErr := os.Stat(lockPath)
	assert.True(t, os.IsNotExist(statErr),
		"lock file should have been released after Run failed; got %v", statErr)
}
