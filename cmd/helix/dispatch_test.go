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

// ============================================================================
// Unified CLI dry-run wrapper coverage (runDispatchWithDryRun, runDispatchDryRun)
// ============================================================================
//
// These wrappers convert the dispatch subcommand's exit code into the unified
// CLI's error contract: rc=0 → nil, rc≠0 → errExit{code: rc}. They also let
// main.go's global --dry-run flag override dispatch's own --dry-run.
//
// The 0%-coverage gap existed because no test exercised the wrapper layer
// directly — every existing test called runDispatch instead.

// TestRunDispatchDryRun_DryRunOverridesGlobal — globalDryRun=true forces dry-run
// even when the dispatch subcommand doesn't get --dry-run.
func TestRunDispatchDryRun_DryRunOverridesGlobal(t *testing.T) {
	specPath := writeSpec(t)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runDispatchDryRun(
		[]string{
			"--spec", specPath,
			"--agent", "test-agent",
			"--forgejo-url", "http://forgejo.test",
		},
		stdout, stderr, true, // globalDryRun=true
	)
	require.Equalf(t, 0, rc, "global --dry-run must enable dry-run mode; stderr=%s", stderr.String())

	var outcome map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &outcome))
	assert.Equal(t, "test-agent", outcome["agent"])
}

// TestRunDispatchDryRun_GlobalFalse_PropagatesToDispatch — globalDryRun=false
// delegates to runDispatch which will try the live path and exit with parse
// error (rc=2) since no --forgejo-url was passed; that means --spec but no
// agent and no --dry-run.
func TestRunDispatchDryRun_GlobalFalse_PropagatesToDispatch(t *testing.T) {
	specPath := writeSpec(t)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	// Missing --agent → runDispatch parses, hits --agent required check, returns 2.
	rc := runDispatchDryRun(
		[]string{
			"--spec", specPath,
			"--forgejo-url", "http://forgejo.test",
		},
		stdout, stderr, false,
	)
	assert.Equal(t, 2, rc, "missing --agent must surface as rc=2; stderr=%s", stderr.String())
	assert.Contains(t, stderr.String(), "--agent")
}

// TestRunDispatchDryRun_ParseError — bad flag returns rc=2 (exit 2 from
// runDispatch → bubbles up unchanged through runDispatchDryRun).
func TestRunDispatchDryRun_ParseError(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runDispatchDryRun([]string{"--no-such-flag"}, stdout, stderr, false)
	assert.Equal(t, 2, rc, "unknown flag must rc=2; stderr=%s", stderr.String())
}

// TestRunDispatchDryRun_HelpFlag — --help is detected early by parseDispatchFlags
// and returns rc=0 before any pipeline work.
func TestRunDispatchDryRun_HelpFlag(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runDispatchDryRun([]string{"--help"}, stdout, stderr, false)
	assert.Equal(t, 0, rc)
	assert.Contains(t, stdout.String(), "--spec")
}

// TestRunDispatchWithDryRun_SuccessPath — wraps runDispatchDryRun's rc=0 into
// nil (no errExit). Exercised via the unified CLI's happy path.
func TestRunDispatchWithDryRun_SuccessPath(t *testing.T) {
	specPath := writeSpec(t)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := runDispatchWithDryRun(
		[]string{
			"--spec", specPath,
			"--agent", "test-agent",
			"--dry-run",
			"--forgejo-url", "http://forgejo.test",
		},
		stdout, stderr, true,
	)
	require.NoError(t, err, "dry-run success must return nil; stderr=%s", stderr.String())
}

// TestRunDispatchWithDryRun_ParseFailure — wrap rc=2 → errExit{code:2}.
func TestRunDispatchWithDryRun_ParseFailure(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := runDispatchWithDryRun([]string{"--no-such-flag"}, stdout, stderr, false)
	require.Error(t, err, "rc=2 must surface as errExit")
	var exitErr errExit
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 2, exitErr.code)
}

// TestErrExit_Error — the errExit type's Error() method is part of the
// error contract; every error.Is/As path depends on it producing a stable
// string with the exit code.
func TestErrExit_Error(t *testing.T) {
	e := errExit{code: 7}
	assert.Equal(t, "dispatch exit 7", e.Error())
}

// ============================================================================
// runDispatch entry-point coverage — fill remaining branches
// ============================================================================

// TestRunDispatch_VerboseFlag — verbose=true adds a stderr log line.
func TestRunDispatch_VerboseFlag(t *testing.T) {
	specPath := writeSpec(t)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runDispatch(
		[]string{
			"--spec", specPath,
			"--agent", "verbose-agent",
			"--dry-run",
			"--forgejo-url", "http://forgejo.test",
			"--verbose",
		},
		stdout, stderr,
	)
	require.Equalf(t, 0, rc, "dry-run verbose must succeed; stderr=%s", stderr.String())
	assert.Contains(t, stderr.String(), "[dispatch]")
}

// TestRunDispatch_UnexpectedPositionalArgs — passing a positional arg after
// all flags triggers parseDispatchFlags' "unexpected positional arguments"
// branch.
func TestRunDispatch_UnexpectedPositionalArgs(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runDispatch([]string{"extra-positional-arg"}, stdout, stderr)
	assert.Equal(t, 2, rc, "unexpected positional must rc=2; stderr=%s", stderr.String())
	assert.Contains(t, stderr.String(), "positional")
}

// TestRunDispatch_LiveMode_MissingPassword — live mode requires admin password.
// (Existing test TestRunDispatch_LiveMode_MissingPassword covers this; this
// is a redundant smoke check that uses runDispatchWithDryRun for symmetry.)
func TestRunDispatch_LiveMode_MissingPassword_DryRunWrapper(t *testing.T) {
	specPath := writeSpec(t)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rc := runDispatchDryRun(
		[]string{
			"--spec", specPath,
			"--agent", "live-agent",
			"--repo", "helix",
			// No --dry-run; will try live mode.
		},
		stdout, stderr, false,
	)
	// rc=2 because --admin-password is required in live mode.
	assert.Equal(t, 2, rc, "live mode without password must rc=2; stderr=%s", stderr.String())
}
