// Command helix — adr_test.go
//
// Tests for `helix adr` (create/list/show/review/supersede) —
// Architecture Decision Record co-authoring CLI (Phase 2 §2.2). Covers CLI
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

// runAdrCLI drives runAdr with the given args and returns stdout/stderr.
func runAdrCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr strings.Builder
	rc := runAdr(args, &stdout, &stderr)
	return rc, stdout.String(), stderr.String()
}

// createAdrViaCLI creates an ADR through the CLI and returns its id.
func createAdrViaCLI(t *testing.T, store, title string, extra ...string) string {
	t.Helper()
	args := append([]string{"create", "--title", title, "--store", store}, extra...)
	rc, stdout, stderr := runAdrCLI(t, args...)
	require.Equal(t, adrExitOK, rc, "create ADR: stderr: %s", stderr)
	m := regexp.MustCompile(`id=(\S+)`).FindStringSubmatch(stdout)
	require.Len(t, m, 2, "stdout: %s", stdout)
	return m[1]
}

// ---------------------------------------------------------------------------
// parseAdrFlags
// ---------------------------------------------------------------------------

func TestParseAdrFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantSub  string
		wantID   string
		wantNew  string
		wantRC   int
		wantHelp bool
	}{
		{
			name:    "create with flags",
			args:    []string{"create", "--title", "Use Kafka", "--context", "C", "--decision", "D"},
			wantSub: "create", wantRC: adrExitOK,
		},
		{
			name:    "help flag",
			args:    []string{"--help"},
			wantSub: "help", wantHelp: true, wantRC: adrExitOK,
		},
		{
			name:   "missing title value",
			args:   []string{"create", "--title"},
			wantRC: adrExitError,
		},
		{
			name:   "unknown flag",
			args:   []string{"create", "--bogus"},
			wantRC: adrExitError,
		},
		{
			name:   "too many positionals",
			args:   []string{"supersede", "a", "b", "c"},
			wantRC: adrExitError,
		},
		{
			name:    "empty defaults to help",
			args:    []string{},
			wantSub: "help", wantRC: adrExitOK,
		},
		{
			name:    "supersede positional new id",
			args:    []string{"supersede", "old-1", "new-1"},
			wantSub: "supersede", wantID: "old-1", wantNew: "new-1",
			wantRC: adrExitOK,
		},
		{
			name:   "bad threshold",
			args:   []string{"review", "adr-1", "--threshold", "abc"},
			wantRC: adrExitError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, help, rc := parseAdrFlags(tt.args)
			assert.Equal(t, tt.wantRC, rc)
			if tt.wantRC != adrExitOK {
				return // error returns carry partial struct state
			}
			assert.Equal(t, tt.wantHelp, help)
			assert.Equal(t, tt.wantSub, f.subcommand)
			assert.Equal(t, tt.wantID, f.id)
			assert.Equal(t, tt.wantNew, f.newID)
		})
	}
}

func TestParseAdrFlags_Values(t *testing.T) {
	f, _, rc := parseAdrFlags([]string{
		"show", "adr-9", "--risk", "--tradeoffs", "--json",
		"--author", "alexis", "--models", "arch,security",
		"--threshold", "0.5", "--spec", "SPEC-1", "--status", "accepted",
		"--consequences", "X", "--store", "/tmp/adrs",
	})
	require.Equal(t, adrExitOK, rc)
	assert.Equal(t, "show", f.subcommand)
	assert.Equal(t, "adr-9", f.id)
	assert.True(t, f.risk)
	assert.True(t, f.tradeoffs)
	assert.True(t, f.jsonOut)
	assert.Equal(t, "alexis", f.author)
	assert.Equal(t, "arch,security", f.models)
	assert.Equal(t, 0.5, f.threshold)
	assert.Equal(t, "SPEC-1", f.specRef)
	assert.Equal(t, "accepted", f.status)
	assert.Equal(t, "X", f.consequences)
	assert.Equal(t, "/tmp/adrs", f.storePath)

	// Default threshold is the package consensus threshold.
	f, _, rc = parseAdrFlags([]string{"review", "adr-1"})
	require.Equal(t, adrExitOK, rc)
	assert.Equal(t, 0.66, f.threshold)
}

// ---------------------------------------------------------------------------
// printAdrHelp / resolveADRStorePath / openADRStore
// ---------------------------------------------------------------------------

func TestPrintAdrHelp(t *testing.T) {
	var w strings.Builder
	printAdrHelp(&w)
	out := w.String()
	assert.Contains(t, out, "helix adr")
	assert.Contains(t, out, "supersede")
	assert.Contains(t, out, "--threshold")
	assert.Contains(t, out, "Exit codes:")
}

func TestResolveADRStorePath(t *testing.T) {
	assert.Equal(t, "/explicit", resolveADRStorePath("/explicit"))

	t.Setenv(envADRStore, "/from/env")
	assert.Equal(t, "/from/env", resolveADRStorePath(""))

	t.Setenv(envADRStore, "")
	assert.Equal(t, "", resolveADRStorePath(""))
}

func TestOpenADRStore(t *testing.T) {
	dir := t.TempDir()
	s, err := openADRStore(dir)
	require.NoError(t, err)
	require.NotNil(t, s)

	// Error path: env points at a path under a regular file.
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))
	t.Setenv(envADRStore, filepath.Join(blocker, "sub"))
	_, err = openADRStore("")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// runAdr dispatch
// ---------------------------------------------------------------------------

func TestRunAdr_HelpSubcommand(t *testing.T) {
	rc, stdout, stderr := runAdrCLI(t, "help")
	assert.Equal(t, adrExitOK, rc)
	assert.Contains(t, stdout, "helix adr")
	assert.Empty(t, stderr)

	rc, stdout, _ = runAdrCLI(t, "create", "--help")
	assert.Equal(t, adrExitOK, rc)
	assert.Contains(t, stdout, "Usage:")
}

func TestRunAdr_InvalidArgs(t *testing.T) {
	rc, _, stderr := runAdrCLI(t, "--title")
	assert.Equal(t, adrExitError, rc)
	assert.Contains(t, stderr, "error: invalid arguments")
}

func TestRunAdr_UnknownSubcommand(t *testing.T) {
	rc, _, stderr := runAdrCLI(t, "frobnicate")
	assert.Equal(t, adrExitError, rc)
	assert.Contains(t, stderr, `unknown subcommand "frobnicate"`)
}

func TestRunAdrWithDryRun(t *testing.T) {
	var stdout, stderr strings.Builder
	err := runAdrWithDryRun([]string{"create", "--title", "T"}, &stdout, &stderr, true)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "[DRY RUN] would create ADR id=")

	// Error propagation: unknown subcommand surfaces as errExit{code:2}.
	stdout.Reset()
	err = runAdrWithDryRun([]string{"frobnicate"}, &stdout, &stderr, false)
	var ee errExit
	require.True(t, errors.As(err, &ee), "err = %v", err)
	assert.Equal(t, adrExitError, ee.code)

	// Non-dry-run success passes through cleanly.
	stdout.Reset()
	err = runAdrWithDryRun([]string{"list", "--store", t.TempDir()}, &stdout, &stderr, false)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "0 ADRs")
}

// ---------------------------------------------------------------------------
// runAdrCreate
// ---------------------------------------------------------------------------

func TestRunAdrCreate_UsageErrors(t *testing.T) {
	rc, _, stderr := runAdrCLI(t, "create", "--store", t.TempDir())
	assert.Equal(t, adrExitError, rc)
	assert.Contains(t, stderr, "error: --title is required")
}

func TestRunAdrCreate_HappyPath(t *testing.T) {
	store := t.TempDir()
	rc, stdout, stderr := runAdrCLI(t, "create",
		"--title", "Use event sourcing for ledger",
		"--context", "Ledger needs auditability",
		"--decision", "Adopt event sourcing",
		"--consequences", "Replay complexity",
		"--spec", "SPEC-001",
		"--author", "alexis",
		"--store", store)
	require.Equal(t, adrExitOK, rc, "stderr: %s", stderr)
	assert.Contains(t, stdout, "created ADR")
	assert.Contains(t, stdout, "status=proposed")
	assert.Contains(t, stdout, "title:        Use event sourcing for ledger")
	assert.Contains(t, stdout, "evidence:")
	assert.Contains(t, stdout, "decision:")

	// Persisted: exactly one .md file in the store.
	entries, err := os.ReadDir(store)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.True(t, strings.HasSuffix(entries[0].Name(), ".md"))
}

func TestRunAdrCreate_PositionalTitle(t *testing.T) {
	store := t.TempDir()
	rc, stdout, stderr := runAdrCLI(t, "create", "Use positional title", "--store", store)
	require.Equal(t, adrExitOK, rc, "stderr: %s", stderr)
	assert.Contains(t, stdout, "title:        Use positional title")
}

func TestRunAdrCreate_JSONOutput(t *testing.T) {
	store := t.TempDir()
	rc, stdout, stderr := runAdrCLI(t, "create", "--title", "JSON ADR", "--store", store, "--json")
	require.Equal(t, adrExitOK, rc, "stderr: %s", stderr)
	var got struct {
		ID      string   `json:"id"`
		Title   string   `json:"title"`
		Status  string   `json:"status"`
		Authors []string `json:"authors"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	assert.Equal(t, "JSON ADR", got.Title)
	assert.Equal(t, "proposed", got.Status)
	assert.NotEmpty(t, got.Authors)
}

func TestRunAdrCreate_DryRunWritesNothing(t *testing.T) {
	store := t.TempDir()
	rc, stdout, _ := runAdrCLI(t, "create", "--title", "Dry ADR", "--store", store, "--dry-run")
	require.Equal(t, adrExitOK, rc)
	assert.Contains(t, stdout, "[DRY RUN] would create ADR id=")
	entries, err := os.ReadDir(store)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// ---------------------------------------------------------------------------
// runAdrList
// ---------------------------------------------------------------------------

func TestRunAdrList_EmptyStore(t *testing.T) {
	rc, stdout, stderr := runAdrCLI(t, "list", "--store", t.TempDir())
	require.Equal(t, adrExitOK, rc, "stderr: %s", stderr)
	assert.Contains(t, stdout, "NUM")
	assert.Contains(t, stdout, "0 ADRs")
}

func TestRunAdrList_WithADRs(t *testing.T) {
	store := t.TempDir()
	createAdrViaCLI(t, store, "First ADR")
	createAdrViaCLI(t, store, "Second ADR")

	rc, stdout, stderr := runAdrCLI(t, "list", "--store", store)
	require.Equal(t, adrExitOK, rc, "stderr: %s", stderr)
	assert.Contains(t, stdout, "First ADR")
	assert.Contains(t, stdout, "Second ADR")
	assert.Contains(t, stdout, "2 ADRs")

	rc, stdout, _ = runAdrCLI(t, "list", "--store", store, "--json")
	require.Equal(t, adrExitOK, rc)
	var got []struct {
		Title string `json:"title"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	assert.Len(t, got, 2)
}

// ---------------------------------------------------------------------------
// runAdrShow
// ---------------------------------------------------------------------------

func TestRunAdrShow_UsageErrors(t *testing.T) {
	rc, _, stderr := runAdrCLI(t, "show", "--store", t.TempDir())
	assert.Equal(t, adrExitError, rc)
	assert.Contains(t, stderr, "error: adr id required")

	rc, _, _ = runAdrCLI(t, "show", "adr-nope", "--store", t.TempDir())
	assert.Equal(t, adrExitError, rc)
}

func TestRunAdrShow_HappyPath(t *testing.T) {
	store := t.TempDir()
	id := createAdrViaCLI(t, store, "Showable ADR", "--context", "ctx", "--decision", "dec")

	rc, stdout, stderr := runAdrCLI(t, "show", id, "--store", store)
	require.Equal(t, adrExitOK, rc, "stderr: %s", stderr)
	assert.Contains(t, stdout, "ADR 0001 — Showable ADR")
	assert.Contains(t, stdout, "ID:       "+id)
	assert.Contains(t, stdout, "Status:   proposed")
	assert.Contains(t, stdout, "## Context")
	assert.Contains(t, stdout, "ctx")
	assert.Contains(t, stdout, "## Decision")
	assert.Contains(t, stdout, "## Consequences")
	assert.Contains(t, stdout, "## Evidence")
}

func TestRunAdrShow_RiskAndTradeoffs(t *testing.T) {
	store := t.TempDir()
	id := createAdrViaCLI(t, store, "Risky ADR")

	rc, stdout, stderr := runAdrCLI(t, "show", id, "--store", store, "--risk", "--tradeoffs")
	require.Equal(t, adrExitOK, rc, "stderr: %s", stderr)
	assert.Contains(t, stdout, "## Risk / Blast Radius")
	assert.Contains(t, stdout, "Risk score:")
	assert.Contains(t, stdout, "Blast radius:")
	assert.Contains(t, stdout, "## Alternatives / Tradeoffs")
	assert.Contains(t, stdout, "(tradeoffs view: rejected alternatives shown with rationale)")
}

func TestRunAdrShow_JSONOutput(t *testing.T) {
	store := t.TempDir()
	id := createAdrViaCLI(t, store, "JSON ADR Show")

	rc, stdout, stderr := runAdrCLI(t, "show", id, "--store", store, "--json")
	require.Equal(t, adrExitOK, rc, "stderr: %s", stderr)
	var got struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	assert.Equal(t, id, got.ID)
	assert.Equal(t, "JSON ADR Show", got.Title)
}

// ---------------------------------------------------------------------------
// runAdrReview
// ---------------------------------------------------------------------------

func TestRunAdrReview_UsageErrors(t *testing.T) {
	rc, _, stderr := runAdrCLI(t, "review", "--store", t.TempDir())
	assert.Equal(t, adrExitError, rc)
	assert.Contains(t, stderr, "error: adr id required")

	rc, _, _ = runAdrCLI(t, "review", "adr-nope", "--store", t.TempDir())
	assert.Equal(t, adrExitError, rc)
}

func TestRunAdrReview_HappyPath(t *testing.T) {
	store := t.TempDir()
	id := createAdrViaCLI(t, store, "Reviewable ADR")

	rc, stdout, stderr := runAdrCLI(t, "review", id, "--store", store)
	require.Equal(t, adrExitOK, rc, "stderr: %s", stderr)
	assert.Contains(t, stdout, "ADR Review for 0001")
	assert.Contains(t, stdout, "Consensus:")
	assert.Contains(t, stdout, "Model verdicts:")
	assert.Contains(t, stdout, "passed=")
}

func TestRunAdrReview_CustomModelsAndThreshold(t *testing.T) {
	store := t.TempDir()
	id := createAdrViaCLI(t, store, "Multi-model ADR")

	rc, stdout, stderr := runAdrCLI(t, "review", id, "--store", store,
		"--models", "arch-eval,security-audit", "--threshold", "0.5")
	require.Equal(t, adrExitOK, rc, "stderr: %s", stderr)
	assert.Contains(t, stdout, "(threshold 0.50)")

	// --json form emits the review result.
	rc, stdout, _ = runAdrCLI(t, "review", id, "--store", store, "--json")
	require.Equal(t, adrExitOK, rc)
	var got struct {
		ConsensusScore float64 `json:"consensus_score"`
		Passed         bool    `json:"passed"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	assert.GreaterOrEqual(t, got.ConsensusScore, 0.0)
}

func TestRunAdrReview_DryRunSkipsSave(t *testing.T) {
	store := t.TempDir()
	id := createAdrViaCLI(t, store, "Dry Review ADR")

	rc, stdout, _ := runAdrCLI(t, "review", id, "--store", store, "--dry-run")
	require.Equal(t, adrExitOK, rc)
	assert.Contains(t, stdout, "ADR Review for")
}

// ---------------------------------------------------------------------------
// runAdrSupersede
// ---------------------------------------------------------------------------

func TestRunAdrSupersede_UsageErrors(t *testing.T) {
	rc, _, stderr := runAdrCLI(t, "supersede", "--store", t.TempDir())
	assert.Equal(t, adrExitError, rc)
	assert.Contains(t, stderr, "error: old adr id required")

	rc, _, stderr = runAdrCLI(t, "supersede", "adr-nope", "--store", t.TempDir())
	assert.Equal(t, adrExitError, rc)
	assert.Contains(t, stderr, "error: load old:")

	store := t.TempDir()
	old := createAdrViaCLI(t, store, "Old ADR")
	rc, _, stderr = runAdrCLI(t, "supersede", old, "--store", store)
	assert.Equal(t, adrExitError, rc)
	assert.Contains(t, stderr, "error: --title or new adr id required for supersede")
}

func TestRunAdrSupersede_WithTitle(t *testing.T) {
	store := t.TempDir()
	old := createAdrViaCLI(t, store, "Old ADR")

	rc, stdout, stderr := runAdrCLI(t, "supersede", old, "--title", "New Direction", "--store", store)
	require.Equal(t, adrExitOK, rc, "stderr: %s", stderr)
	assert.Contains(t, stdout, "superseded ADR 0001")
	assert.Contains(t, stdout, "new ADR")
	assert.Contains(t, stdout, "title: New Direction")
	assert.Contains(t, stdout, "lineage: "+old)

	// Old ADR now shows as superseded.
	rc, stdout, _ = runAdrCLI(t, "show", old, "--store", store)
	require.Equal(t, adrExitOK, rc)
	assert.Contains(t, stdout, "SupersededBy:")
}

func TestRunAdrSupersede_ExistingNewID(t *testing.T) {
	store := t.TempDir()
	old := createAdrViaCLI(t, store, "Old ADR")
	replacement := createAdrViaCLI(t, store, "Replacement ADR")

	rc, stdout, stderr := runAdrCLI(t, "supersede", old, replacement, "--store", store)
	require.Equal(t, adrExitOK, rc, "stderr: %s", stderr)
	assert.Contains(t, stdout, "superseded ADR")
	assert.Contains(t, stdout, "lineage: "+old+" → "+replacement)
}

func TestRunAdrSupersede_DryRunAndJSON(t *testing.T) {
	store := t.TempDir()
	old := createAdrViaCLI(t, store, "Old ADR")

	rc, stdout, _ := runAdrCLI(t, "supersede", old, "--title", "Dry New", "--store", store, "--dry-run")
	require.Equal(t, adrExitOK, rc)
	assert.Contains(t, stdout, "[DRY RUN] would supersede ADR")

	rc, stdout, stderr := runAdrCLI(t, "supersede", old, "--title", "JSON New", "--store", store, "--json")
	require.Equal(t, adrExitOK, rc, "stderr: %s", stderr)
	var got struct {
		OldID string `json:"old_id"`
		New   struct {
			Title string `json:"title"`
		} `json:"new"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	assert.Equal(t, old, got.OldID)
	assert.Equal(t, "JSON New", got.New.Title)
}

// ---------------------------------------------------------------------------
// writeAdrJSON / truncateDisplay
// ---------------------------------------------------------------------------

func TestWriteAdrJSON_HappyPath(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := writeAdrJSON(&stdout, &stderr, map[string]string{"a": "b"})
	assert.Equal(t, adrExitOK, rc)
	assert.Contains(t, stdout.String(), `"a": "b"`)
}

func TestWriteAdrJSON_MarshalError(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := writeAdrJSON(&stdout, &stderr, func() {}) // funcs are not JSON-marshalable
	assert.Equal(t, adrExitError, rc)
	assert.Contains(t, stderr.String(), "error: json:")
}

func TestTruncateDisplay(t *testing.T) {
	assert.Equal(t, "", truncateDisplay("", 10))
	assert.Equal(t, "short", truncateDisplay("short", 10))
	assert.Equal(t, "short", truncateDisplay("  short  ", 10))
	assert.Equal(t, "a b", truncateDisplay("a\nb", 10))
	assert.Equal(t, "abcdefg...", truncateDisplay("abcdefghijklmnop", 10))
	assert.Equal(t, "abc", truncateDisplay("abcdef", 3))
	assert.Equal(t, "ab", truncateDisplay("abcdef", 2))
	assert.Equal(t, "", truncateDisplay("abcdef", 0))
}
