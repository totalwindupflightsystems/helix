// Command helix — spec_test.go
//
// Tests for `helix spec` (create/review/gap-analysis/approve/show/list) —
// spec co-authoring CLI driven by pkg/spec (Phase 2 §2.1). Covers CLI
// happy paths AND usage-error paths (COV-003).
package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// runSpecCLI drives runSpec with the given args and returns stdout/stderr.
func runSpecCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr strings.Builder
	rc := runSpec(args, &stdout, &stderr)
	return rc, stdout.String(), stderr.String()
}

// createSpecViaCLI creates a spec through the CLI and returns its id.
func createSpecViaCLI(t *testing.T, store, ideaID, title string) string {
	t.Helper()
	rc, stdout, stderr := runSpecCLI(t, "create", ideaID, "--title", title, "--store", store)
	require.Equal(t, specExitOK, rc, "create spec: stderr: %s", stderr)
	m := regexp.MustCompile(`created spec (\S+)`).FindStringSubmatch(stdout)
	require.Len(t, m, 2, "stdout: %s", stdout)
	return m[1]
}

// ---------------------------------------------------------------------------
// parseSpecFlags
// ---------------------------------------------------------------------------

func TestParseSpecFlags(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantSub    string
		wantID     string
		wantTitle  string
		wantHelp   bool
		wantJSON   bool
		wantDryRun bool
		wantStore  string
		wantRC     int
	}{
		{
			name:    "create with title",
			args:    []string{"create", "idea-42", "--title", "Event Sourcing"},
			wantSub: "create", wantID: "idea-42", wantTitle: "Event Sourcing",
			wantRC: specExitOK,
		},
		{
			name:    "idea override flag",
			args:    []string{"create", "--idea", "idea-7", "--title", "T"},
			wantSub: "create", wantTitle: "T",
			wantRC: specExitOK,
		},
		{
			name:    "help flag short circuit",
			args:    []string{"--help"},
			wantSub: "help", wantHelp: true, wantRC: specExitOK,
		},
		{
			name:    "json and dry-run",
			args:    []string{"list", "--json", "--dry-run"},
			wantSub: "list", wantJSON: true, wantDryRun: true,
			wantRC: specExitOK,
		},
		{
			name:    "store flag",
			args:    []string{"list", "--store", "/tmp/specs"},
			wantSub: "list", wantStore: "/tmp/specs",
			wantRC: specExitOK,
		},
		{
			name:    "section and approver",
			args:    []string{"approve", "spec-1", "--section", "Requirements", "--by", "alexis"},
			wantSub: "approve", wantID: "spec-1",
			wantRC: specExitOK,
		},
		{
			name:   "missing title value",
			args:   []string{"create", "idea-1", "--title"},
			wantRC: specExitError,
		},
		{
			name:   "unknown flag",
			args:   []string{"create", "--bogus"},
			wantRC: specExitError,
		},
		{
			name:   "too many positionals",
			args:   []string{"create", "a", "b", "c"},
			wantRC: specExitError,
		},
		{
			name:    "empty defaults to help",
			args:    []string{},
			wantSub: "help", wantRC: specExitOK,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, help, rc := parseSpecFlags(tt.args)
			assert.Equal(t, tt.wantRC, rc)
			if tt.wantRC != specExitOK {
				return // error returns carry partial struct state
			}
			assert.Equal(t, tt.wantSub, f.subcommand)
			assert.Equal(t, tt.wantID, f.id)
			assert.Equal(t, tt.wantTitle, f.title)
			assert.Equal(t, tt.wantHelp, help)
			assert.Equal(t, tt.wantJSON, f.jsonOut)
			assert.Equal(t, tt.wantDryRun, f.dryRun)
			assert.Equal(t, tt.wantStore, f.storePath)
		})
	}
}

func TestParseSpecFlags_FlagValues(t *testing.T) {
	f, _, rc := parseSpecFlags([]string{"approve", "spec-x", "--section", "Overview", "--by", "chief"})
	require.Equal(t, specExitOK, rc)
	assert.Equal(t, "Overview", f.section)
	assert.Equal(t, "chief", f.approvedBy)

	f, _, rc = parseSpecFlags([]string{"review", "spec-y", "--idea", "idea-9"})
	require.Equal(t, specExitOK, rc)
	assert.Equal(t, "idea-9", f.ideaID)
}

// ---------------------------------------------------------------------------
// printSpecHelp / resolveSpecStorePath / openSpecStore
// ---------------------------------------------------------------------------

func TestPrintSpecHelp(t *testing.T) {
	var w strings.Builder
	printSpecHelp(&w)
	out := w.String()
	assert.Contains(t, out, "helix spec")
	assert.Contains(t, out, "gap-analysis")
	assert.Contains(t, out, "--dry-run")
	assert.Contains(t, out, "Exit codes:")
}

func TestResolveSpecStorePath(t *testing.T) {
	assert.Equal(t, "/explicit", resolveSpecStorePath("/explicit"))
	assert.Equal(t, "/explicit", resolveSpecStorePath("/explicit")) // env must not win

	t.Setenv(envSpecStore, "/from/env")
	assert.Equal(t, "/from/env", resolveSpecStorePath(""))

	t.Setenv(envSpecStore, "")
	assert.Equal(t, "", resolveSpecStorePath(""))
}

func TestOpenSpecStore(t *testing.T) {
	dir := t.TempDir()
	s, err := openSpecStore(dir)
	require.NoError(t, err)
	require.NotNil(t, s)

	// Error path: env points at a path under a regular file.
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))
	t.Setenv(envSpecStore, filepath.Join(blocker, "sub"))
	_, err = openSpecStore("")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// runSpec dispatch
// ---------------------------------------------------------------------------

func TestRunSpec_HelpSubcommand(t *testing.T) {
	rc, stdout, stderr := runSpecCLI(t, "help")
	assert.Equal(t, specExitOK, rc)
	assert.Contains(t, stdout, "helix spec")
	assert.Empty(t, stderr)

	rc, stdout, _ = runSpecCLI(t, "list", "--help")
	assert.Equal(t, specExitOK, rc)
	assert.Contains(t, stdout, "Usage:")
}

func TestRunSpec_InvalidArgs(t *testing.T) {
	rc, _, stderr := runSpecCLI(t, "--title")
	assert.Equal(t, specExitError, rc)
	assert.Contains(t, stderr, "error: invalid arguments")
	assert.Contains(t, stderr, "Usage:")
}

func TestRunSpec_UnknownSubcommand(t *testing.T) {
	rc, _, stderr := runSpecCLI(t, "frobnicate")
	assert.Equal(t, specExitError, rc)
	assert.Contains(t, stderr, `unknown subcommand "frobnicate"`)
	assert.Contains(t, stderr, "Usage:")
}

func TestRunSpecWithDryRun(t *testing.T) {
	// Threaded --dry-run: create prints intent without a store.
	var stdout, stderr strings.Builder
	err := runSpecWithDryRun([]string{"create", "idea-1", "--title", "T"}, &stdout, &stderr, true)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "[DRY RUN] would create spec id=")

	// Error propagation: unknown subcommand surfaces as errExit{code:2}.
	stdout.Reset()
	err = runSpecWithDryRun([]string{"frobnicate"}, &stdout, &stderr, false)
	var ee errExit
	require.True(t, errors.As(err, &ee), "err = %v", err)
	assert.Equal(t, specExitError, ee.code)

	// Non-dry-run success passes through cleanly.
	stdout.Reset()
	err = runSpecWithDryRun([]string{"list", "--store", t.TempDir()}, &stdout, &stderr, false)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "0 specs")
}

// ---------------------------------------------------------------------------
// runSpecCreate
// ---------------------------------------------------------------------------

func TestRunSpecCreate_UsageErrors(t *testing.T) {
	rc, _, stderr := runSpecCLI(t, "create", "--title", "T", "--store", t.TempDir())
	assert.Equal(t, specExitError, rc)
	assert.Contains(t, stderr, "error: idea id required")

	rc, _, stderr = runSpecCLI(t, "create", "idea-1", "--store", t.TempDir())
	assert.Equal(t, specExitError, rc)
	assert.Contains(t, stderr, "error: --title is required")
}

func TestRunSpecCreate_HappyPath(t *testing.T) {
	store := t.TempDir()
	rc, stdout, stderr := runSpecCLI(t, "create", "idea-77", "--title", "AuthN v2", "--store", store)
	require.Equal(t, specExitOK, rc, "stderr: %s", stderr)
	assert.Contains(t, stdout, "created spec ")
	assert.Contains(t, stdout, "status=draft")
	assert.Contains(t, stdout, "title: AuthN v2")
	assert.Contains(t, stdout, "idea:  idea-77")
	assert.Contains(t, stdout, "sections: 5")

	// Persisted: exactly one .md file in the store.
	entries, err := os.ReadDir(store)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.True(t, strings.HasSuffix(entries[0].Name(), ".md"))
}

func TestRunSpecCreate_JSONOutput(t *testing.T) {
	store := t.TempDir()
	rc, stdout, stderr := runSpecCLI(t, "create", "idea-3", "--title", "JSON Spec", "--store", store, "--json")
	require.Equal(t, specExitOK, rc, "stderr: %s", stderr)

	var got struct {
		ID       string            `json:"id"`
		Title    string            `json:"title"`
		Status   string            `json:"status"`
		Sections []json.RawMessage `json:"sections"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	assert.Equal(t, "JSON Spec", got.Title)
	assert.Equal(t, "draft", got.Status)
	assert.Len(t, got.Sections, 5)
}

func TestRunSpecCreate_DryRunWritesNothing(t *testing.T) {
	store := t.TempDir()
	rc, stdout, _ := runSpecCLI(t, "create", "idea-1", "--title", "T", "--store", store, "--dry-run")
	require.Equal(t, specExitOK, rc)
	assert.Contains(t, stdout, "[DRY RUN] would create spec id=")
	entries, err := os.ReadDir(store)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// ---------------------------------------------------------------------------
// runSpecReview
// ---------------------------------------------------------------------------

func TestRunSpecReview_UsageErrors(t *testing.T) {
	rc, _, stderr := runSpecCLI(t, "review", "--store", t.TempDir())
	assert.Equal(t, specExitError, rc)
	assert.Contains(t, stderr, "error: spec id required")

	rc, _, stderr = runSpecCLI(t, "review", "spec-nope", "--store", t.TempDir())
	assert.Equal(t, specExitError, rc)
	assert.Contains(t, stderr, "error: ")
}

func TestRunSpecReview_HappyPath(t *testing.T) {
	store := t.TempDir()
	id := createSpecViaCLI(t, store, "idea-5", "Review Me")

	rc, stdout, stderr := runSpecCLI(t, "review", id, "--store", store)
	require.Equal(t, specExitOK, rc, "stderr: %s", stderr)
	assert.Contains(t, stdout, "Spec Review for "+id)
	assert.Contains(t, stdout, "Title:  Review Me")
	assert.Contains(t, stdout, "Annotations:")
}

func TestRunSpecReview_JSONOutput(t *testing.T) {
	store := t.TempDir()
	id := createSpecViaCLI(t, store, "idea-5", "Review JSON")

	rc, stdout, stderr := runSpecCLI(t, "review", id, "--store", store, "--json")
	require.Equal(t, specExitOK, rc, "stderr: %s", stderr)
	var got struct {
		ID          string `json:"id"`
		Annotations []struct {
			Severity string `json:"severity"`
		} `json:"annotations"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	assert.Equal(t, id, got.ID)
	assert.NotEmpty(t, got.Annotations)
}

// ---------------------------------------------------------------------------
// runSpecGapAnalysis
// ---------------------------------------------------------------------------

func TestRunSpecGapAnalysis_UsageErrors(t *testing.T) {
	rc, _, stderr := runSpecCLI(t, "gap-analysis", "--store", t.TempDir())
	assert.Equal(t, specExitError, rc)
	assert.Contains(t, stderr, "error: spec id required")

	rc, _, _ = runSpecCLI(t, "gap-analysis", "spec-nope", "--store", t.TempDir())
	assert.Equal(t, specExitError, rc)
}

func TestRunSpecGapAnalysis_HappyPath(t *testing.T) {
	store := t.TempDir()
	id := createSpecViaCLI(t, store, "idea-9", "Gap Me")

	rc, stdout, stderr := runSpecCLI(t, "gap-analysis", id, "--store", store)
	require.Equal(t, specExitOK, rc, "stderr: %s", stderr)
	assert.Contains(t, stdout, "Gap Analysis for "+id)
	assert.Contains(t, stdout, "Total Score:")
	assert.Contains(t, stdout, "DIMENSION")

	// --json form
	rc, stdout, stderr = runSpecCLI(t, "gap-analysis", id, "--store", store, "--json")
	require.Equal(t, specExitOK, rc, "stderr: %s", stderr)
	var report struct {
		SpecID     string  `json:"spec_id"`
		TotalScore float64 `json:"total_score"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &report))
	assert.Equal(t, id, report.SpecID)
	assert.GreaterOrEqual(t, report.TotalScore, 0.0)
}

// ---------------------------------------------------------------------------
// runSpecApprove
// ---------------------------------------------------------------------------

func TestRunSpecApprove_UsageErrors(t *testing.T) {
	store := t.TempDir()
	rc, _, stderr := runSpecCLI(t, "approve", "--section", "X", "--store", store)
	assert.Equal(t, specExitError, rc)
	assert.Contains(t, stderr, "error: spec id required")

	rc, _, stderr = runSpecCLI(t, "approve", "spec-1", "--store", store)
	assert.Equal(t, specExitError, rc)
	assert.Contains(t, stderr, "error: --section is required")
}

func TestRunSpecApprove_SectionNotFound(t *testing.T) {
	store := t.TempDir()
	id := createSpecViaCLI(t, store, "idea-1", "Approve Me")
	rc, _, stderr := runSpecCLI(t, "approve", id, "--section", "Nope", "--store", store)
	assert.Equal(t, specExitError, rc)
	assert.Contains(t, stderr, `section "Nope" not found`)
}

func TestRunSpecApprove_HappyPath(t *testing.T) {
	store := t.TempDir()
	id := createSpecViaCLI(t, store, "idea-1", "Approve Me")

	rc, stdout, stderr := runSpecCLI(t, "approve", id, "--section", "Requirements", "--by", "alexis", "--store", store)
	require.Equal(t, specExitOK, rc, "stderr: %s", stderr)
	assert.Contains(t, stdout, `approved section "Requirements" in spec `+id)
	assert.Contains(t, stdout, "(by alexis)")
	assert.NotContains(t, stdout, "all sections approved")
}

func TestRunSpecApprove_AllSectionsPromotesStatus(t *testing.T) {
	store := t.TempDir()
	id := createSpecViaCLI(t, store, "idea-1", "Approve All")

	sections := []string{"Overview", "Requirements", "Non-Goals", "Constraints", "Acceptance Criteria"}
	var lastStdout string
	for i, sec := range sections {
		rc, stdout, stderr := runSpecCLI(t, "approve", id, "--section", sec, "--store", store)
		require.Equal(t, specExitOK, rc, "approve %s: stderr: %s", sec, stderr)
		lastStdout = stdout
		if i == len(sections)-1 {
			assert.Contains(t, stdout, `all sections approved — spec status is now "approved"`)
		}
	}
	assert.Contains(t, lastStdout, `approved section "Acceptance Criteria"`)

	// Status persisted: show reports approved.
	rc, stdout, _ := runSpecCLI(t, "show", id, "--store", store)
	require.Equal(t, specExitOK, rc)
	assert.Contains(t, stdout, "Status: approved")
}

func TestRunSpecApprove_DryRun(t *testing.T) {
	store := t.TempDir()
	id := createSpecViaCLI(t, store, "idea-1", "Approve Dry")
	rc, stdout, _ := runSpecCLI(t, "approve", id, "--section", "Overview", "--store", store, "--dry-run")
	require.Equal(t, specExitOK, rc)
	assert.Contains(t, stdout, "[DRY RUN] would approve section")
}

// ---------------------------------------------------------------------------
// runSpecShow
// ---------------------------------------------------------------------------

func TestRunSpecShow_UsageErrors(t *testing.T) {
	rc, _, stderr := runSpecCLI(t, "show", "--store", t.TempDir())
	assert.Equal(t, specExitError, rc)
	assert.Contains(t, stderr, "error: spec id required")

	rc, _, _ = runSpecCLI(t, "show", "spec-nope", "--store", t.TempDir())
	assert.Equal(t, specExitError, rc)
}

func TestRunSpecShow_HappyPath(t *testing.T) {
	store := t.TempDir()
	id := createSpecViaCLI(t, store, "idea-11", "Show Me")

	rc, stdout, stderr := runSpecCLI(t, "show", id, "--store", store)
	require.Equal(t, specExitOK, rc, "stderr: %s", stderr)
	assert.Contains(t, stdout, "ID:     "+id)
	assert.Contains(t, stdout, "Title:  Show Me")
	assert.Contains(t, stdout, "Status: draft")
	assert.Contains(t, stdout, "Idea:   idea-11")
	assert.Contains(t, stdout, "Sections:")
	assert.Contains(t, stdout, "## Overview  [pending]")

	// --json form
	rc, stdout, _ = runSpecCLI(t, "show", id, "--store", store, "--json")
	require.Equal(t, specExitOK, rc)
	var got struct {
		ID       string            `json:"id"`
		Sections []json.RawMessage `json:"sections"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	assert.Equal(t, id, got.ID)
	assert.Len(t, got.Sections, 5)
}

// ---------------------------------------------------------------------------
// runSpecList
// ---------------------------------------------------------------------------

func TestRunSpecList_EmptyStore(t *testing.T) {
	rc, stdout, stderr := runSpecCLI(t, "list", "--store", t.TempDir())
	require.Equal(t, specExitOK, rc, "stderr: %s", stderr)
	assert.Contains(t, stdout, "ID")
	assert.Contains(t, stdout, "0 specs")
}

func TestRunSpecList_WithSpecs(t *testing.T) {
	store := t.TempDir()
	createSpecViaCLI(t, store, "idea-1", "First Spec")
	createSpecViaCLI(t, store, "idea-2", "Second Spec")

	rc, stdout, stderr := runSpecCLI(t, "list", "--store", store)
	require.Equal(t, specExitOK, rc, "stderr: %s", stderr)
	assert.Contains(t, stdout, "First Spec")
	assert.Contains(t, stdout, "Second Spec")
	assert.Contains(t, stdout, "2 specs")

	// --json form returns an array.
	rc, stdout, _ = runSpecCLI(t, "list", "--store", store, "--json")
	require.Equal(t, specExitOK, rc)
	var got []struct {
		Title string `json:"title"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	assert.Len(t, got, 2)
}

// ---------------------------------------------------------------------------
// writeSpecJSON
// ---------------------------------------------------------------------------

func TestWriteSpecJSON_HappyPath(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := writeSpecJSON(&stdout, &stderr, map[string]string{"a": "b"})
	assert.Equal(t, specExitOK, rc)
	assert.Contains(t, stdout.String(), `"a": "b"`)
}

func TestWriteSpecJSON_MarshalError(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := writeSpecJSON(&stdout, &stderr, func() {}) // funcs are not JSON-marshalable
	assert.Equal(t, specExitError, rc)
	assert.Contains(t, stderr.String(), "error: json:")
}
