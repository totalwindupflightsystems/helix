package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// writeSpec writes a tiny spec file to t.TempDir() and returns its path.
// The spec must contain at least one "## PHASE" or "## FEATURE" heading
// for pkg/dispatcher.DecomposeSpec to extract a task.
func writeSpec(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.md")
	body := `# Sample Helix Spec

## PHASE 1: Wire dispatcher to Forgejo

The dispatcher must:
  - Acquire lock
  - Execute steps
  - Open branch + PR
  - Release lock

## PHASE 2: Add dry-run mode

Add --dry-run to skip network calls.
`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

// dispatchReturnsZero runs runDispatch and asserts exit code 0 — used by the
// dry-run and live-mock tests where the run is expected to succeed.
func dispatchReturnsZero(t *testing.T, args []string, env map[string]string) (string, string) {
	t.Helper()
	// Push env, run, pop.
	prev := os.Environ()
	defer func() {
		os.Clearenv()
		for _, kv := range prev {
			if k, v, ok := strings.Cut(kv, "="); ok {
				_ = os.Setenv(k, v)
			}
		}
	}()
	os.Clearenv()
	for k, v := range env {
		_ = os.Setenv(k, v)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runDispatch(args, stdout, stderr)
	require.Equalf(t, 0, rc,
		"dispatch failed (rc=%d)\nstdout=%s\nstderr=%s",
		rc, stdout.String(), stderr.String())
	return stdout.String(), stderr.String()
}

// dispatchReturnsCode runs runDispatch and returns the raw rc and buffers —
// used by validation tests where we expect non-zero.
func dispatchReturnsCode(args []string) (int, string, string) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runDispatch(args, stdout, stderr)
	return rc, stdout.String(), stderr.String()
}

// ---------------------------------------------------------------------------
// Dry-run mode (no network)
// ---------------------------------------------------------------------------

func TestRunDispatch_DryRun(t *testing.T) {
	specPath := writeSpec(t)
	stdout, _ := dispatchReturnsZero(t,
		[]string{
			"--spec", specPath,
			"--agent", "test-agent",
			"--dry-run",
			"--forgejo-url", "http://forgejo.test",
		},
		nil,
	)

	var outcome map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &outcome),
		"dry-run output must be JSON: %s", stdout)
	assert.Equal(t, "test-agent", outcome["agent"])
	assert.Equal(t, "dry-run", outcome["mode"])
	assert.Equal(t, true, outcome["dry_run"])
	assert.Contains(t, outcome["branch_name"], "feature/test-agent-")
	assert.Contains(t, outcome["pr_url"], "http://forgejo.test/")
	assert.Contains(t, outcome["pr_url"], "compare/main...")
	assert.Equal(t, "main", outcome["base_branch"])
}

func TestRunDispatch_DryRun_ViaEnv(t *testing.T) {
	specPath := writeSpec(t)
	stdout, _ := dispatchReturnsZero(t, []string{"--dry-run"},
		map[string]string{
			"HELIX_DISPATCH_SPEC":  specPath,
			"HELIX_DISPATCH_AGENT": "env-agent",
			"HELIX_DISPATCH_REPO":  "helix",
		})
	var outcome map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &outcome))
	assert.Equal(t, "env-agent", outcome["agent"])
	assert.Equal(t, specPath, outcome["spec_path"])
}

// ---------------------------------------------------------------------------
// Validation (rc=2)
// ---------------------------------------------------------------------------

func TestRunDispatch_MissingSpec(t *testing.T) {
	rc, _, stderr := dispatchReturnsCode([]string{"--agent", "x"})
	assert.Equal(t, 2, rc, "missing --spec should exit 2")
	assert.Contains(t, stderr, "--spec is required")
}

func TestRunDispatch_MissingAgent(t *testing.T) {
	rc, _, stderr := dispatchReturnsCode([]string{"--spec", "/tmp/x.md"})
	assert.Equal(t, 2, rc, "missing --agent should exit 2")
	assert.Contains(t, stderr, "--agent is required")
}

func TestRunDispatch_LiveMode_MissingRepo(t *testing.T) {
	specPath := writeSpec(t)
	rc, _, stderr := dispatchReturnsCode([]string{
		"--spec", specPath,
		"--agent", "test-agent",
		// no --repo, no --dry-run
	})
	assert.Equal(t, 2, rc, "live mode without --repo should exit 2")
	assert.Contains(t, stderr, "--repo is required")
}

func TestRunDispatch_LiveMode_MissingPassword(t *testing.T) {
	specPath := writeSpec(t)
	rc, _, stderr := dispatchReturnsCode([]string{
		"--spec", specPath,
		"--agent", "test-agent",
		"--repo", "helix",
		"--forgejo-url", "http://localhost:3030",
		"--admin-user", "admin",
		// --admin-password omitted, env empty
	})
	assert.Equal(t, 2, rc, "live mode without password should exit 2")
	assert.Contains(t, stderr, "admin-password")
}

func TestRunDispatch_NoFlags_ShowsHelp(t *testing.T) {
	// When no args are supplied, parseDispatchFlags returns showHelp=true
	// for `--help`, but `runDispatch` itself treats missing --spec/--agent
	// as user errors (rc=2). Calling with --help exercises the help path
	// and exits 0. The usage is rendered to stdout (matches standard
	// Cobra/flag convention for help text).
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runDispatch([]string{"--help"}, stdout, stderr)
	assert.Equal(t, 0, rc, "--help should print usage and exit 0")
	assert.Contains(t, stdout.String(), "Usage: helix dispatch")
}

func TestRunDispatch_NoFlags_MissingRequired_Exit2(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runDispatch([]string{}, stdout, stderr)
	assert.Equal(t, 2, rc, "no flags should exit 2 (missing --spec)")
	assert.Contains(t, stderr.String(), "--spec is required")
}

// ---------------------------------------------------------------------------
// Live mode (httptest mock)
// ---------------------------------------------------------------------------

func TestRunDispatch_Live_HappyPath(t *testing.T) {
	specPath := writeSpec(t)

	// Start a fake Forgejo that records branch + PR calls.
	var branchCalls, prCalls int
	// Pre-declare so the closure (referenced inside NewServer) can see it.
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _, _ := r.BasicAuth()
		if user != "admin" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/branches") && r.Method == http.MethodPost:
			branchCalls++
			writeJT(w, http.StatusCreated, map[string]any{
				"name":     "feature/test-agent-task-001",
				"ref":      "refs/heads/feature/test-agent-task-001",
				"html_url": server.URL + "/branch",
			})
		case strings.HasSuffix(r.URL.Path, "/pulls") && r.Method == http.MethodPost:
			prCalls++
			writeJT(w, http.StatusCreated, map[string]any{
				"id": 1, "number": 7, "state": "open",
				"head_ref": "feature/test-agent-task-001",
				"base_ref": "main",
				"title":    "task-001 wire dispatcher",
				"html_url": server.URL + "/pulls/7",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	stdout, _ := dispatchReturnsZero(t,
		[]string{
			"--spec", specPath,
			"--agent", "test-agent",
			"--repo", "helix",
			"--forgejo-url", server.URL,
			"--admin-user", "admin",
			"--admin-password", "secret",
			"--owner", "helix-org",
		},
		nil,
	)

	var outcome map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &outcome))
	assert.Equal(t, "live", outcome["mode"])
	assert.Equal(t, false, outcome["dry_run"])
	assert.Equal(t, float64(7), outcome["pr_number"])
	assert.Contains(t, outcome["pr_url"], "/pulls/7")
	assert.Equal(t, 1, branchCalls, "exactly one branch POST")
	assert.Equal(t, 1, prCalls, "exactly one PR POST")
}

// ---------------------------------------------------------------------------
// Live mode error path
// ---------------------------------------------------------------------------

func TestRunDispatch_Live_ForgejoError(t *testing.T) {
	specPath := writeSpec(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"forgejo down"}`))
	}))
	defer server.Close()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runDispatch([]string{
		"--spec", specPath,
		"--agent", "test-agent",
		"--repo", "helix",
		"--forgejo-url", server.URL,
		"--admin-user", "admin",
		"--admin-password", "secret",
		"--owner", "helix-org",
	}, stdout, stderr)

	assert.Equal(t, 1, rc, "5xx from Forgejo should propagate as exit 1")
	assert.Contains(t, stderr.String(), "CreateBranch failed")
	// Partial outcome should be present in stdout for debugging.
	out := stdout.String()
	assert.NotEmpty(t, out, "should emit partial JSON even on failure")
}

func TestRunDispatch_Live_IdempotentBranch(t *testing.T) {
	specPath := writeSpec(t)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _, _ := r.BasicAuth()
		if user != "admin" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/branches") && r.Method == http.MethodPost:
			// Already-exists → 409 — should be treated as success
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"branch exists"}`))
		case strings.HasSuffix(r.URL.Path, "/pulls") && r.Method == http.MethodPost:
			writeJT(w, http.StatusCreated, map[string]any{
				"number": 99, "state": "open",
				"head_ref": "feature/test-agent-task-001",
				"base_ref": "main",
				"html_url": server.URL + "/pulls/99",
			})
		}
	}))
	defer server.Close()

	stdout, _ := dispatchReturnsZero(t,
		[]string{
			"--spec", specPath,
			"--agent", "test-agent",
			"--repo", "helix",
			"--forgejo-url", server.URL,
			"--admin-user", "admin",
			"--admin-password", "secret",
		},
		nil,
	)

	var outcome map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &outcome))
	assert.Equal(t, float64(99), outcome["pr_number"])
	assert.Contains(t, outcome["pr_url"], "/pulls/99")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeJT(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
