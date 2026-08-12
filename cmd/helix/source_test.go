// Command helix — source_test.go
//
// Tests for `helix source` (add/list/test/tools) — SPEC-025 §7.
package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/totalwindupflightsystems/helix/pkg/source"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// setSourcesFileEnv points HELIX_SOURCES_FILE at path under a fresh temp
// dir and returns the path. The env is restored automatically by t.Setenv.
func setSourcesFileEnv(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sources.yaml")
	t.Setenv(envSourceFile, p)
	return p
}

// writeSourcesFile writes the given sources.yaml content to path and
// returns the path.
func writeSourcesFile(t *testing.T, path, content string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// ---------------------------------------------------------------------------
// parseSourceFlags
// ---------------------------------------------------------------------------

func TestParseSourceFlags(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		want       sourceFlags
		wantHelp   bool
		wantExitOK bool
	}{
		{
			name: "add all flags",
			args: []string{"add", "--name", "db", "--type", "postgres",
				"--spec", "spec.yaml", "--connection", "postgres://h:5432/db",
				"--rate-limit", "10/s", "--token-env", "TOKEN",
				"--read-only", "--allowed-agents", "a, b", "--dry-run"},
			want: sourceFlags{subcommand: "add", name: "db", typ: "postgres",
				specPath: "spec.yaml", connection: "postgres://h:5432/db",
				rateLimit: "10/s", tokenEnv: "TOKEN", readOnly: true,
				allowedAgents: "a, b", dryRun: true},
			wantExitOK: true,
		},
		{
			name: "add rest with base-url and root",
			args: []string{"add", "--name", "api", "--type", "rest",
				"--base-url", "https://api.example.com", "--spec", "api.yaml",
				"--root", "/data"},
			want: sourceFlags{subcommand: "add", name: "api", typ: "rest",
				baseURL: "https://api.example.com", specPath: "api.yaml", root: "/data"},
			wantExitOK: true,
		},
		{
			name:       "list with enabled",
			args:       []string{"list", "--enabled"},
			want:       sourceFlags{subcommand: "list", enabled: true},
			wantExitOK: true,
		},
		{
			name:       "test with name",
			args:       []string{"test", "--name", "db"},
			want:       sourceFlags{subcommand: "test", name: "db"},
			wantExitOK: true,
		},
		{
			name:       "test with probe path",
			args:       []string{"test", "--name", "api", "--probe-path", "/health"},
			want:       sourceFlags{subcommand: "test", name: "api", probePath: "/health"},
			wantExitOK: true,
		},
		{
			name:       "tools with name",
			args:       []string{"tools", "--name", "db"},
			want:       sourceFlags{subcommand: "tools", name: "db"},
			wantExitOK: true,
		},
		{
			name:       "help",
			args:       []string{"--help"},
			wantHelp:   true,
			wantExitOK: true,
		},
		{
			name:       "missing flag value",
			args:       []string{"add", "--name"},
			wantExitOK: false,
		},
		{
			name:       "unknown flag",
			args:       []string{"list", "--bogus"},
			wantExitOK: false,
		},
		{
			name:       "extra positional",
			args:       []string{"add", "extra", "--name", "x"},
			wantExitOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, help, exitCode := parseSourceFlags(tt.args)
			assert.Equal(t, tt.wantHelp, help)
			if tt.wantExitOK {
				assert.Equal(t, sourceExitOK, exitCode)
				assert.Equal(t, tt.want, got)
			} else {
				assert.Equal(t, sourceExitError, exitCode)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// runSource — help / dispatch
// ---------------------------------------------------------------------------

func TestRunSource_Help(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runSource([]string{"--help"}, &stdout, &stderr)
	assert.Equal(t, sourceExitOK, rc)
	assert.Contains(t, stdout.String(), "helix source")
	assert.Contains(t, stdout.String(), "add")
	assert.Contains(t, stdout.String(), "list")
	assert.Contains(t, stdout.String(), "test")
	assert.Contains(t, stdout.String(), "tools")
}

func TestRunSource_NoArgsShowsHelp(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runSource([]string{}, &stdout, &stderr)
	assert.Equal(t, sourceExitOK, rc)
	assert.Contains(t, stdout.String(), "helix source")
}

func TestRunSource_UnknownSubcommand(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runSource([]string{"bogus"}, &stdout, &stderr)
	assert.Equal(t, sourceExitError, rc)
	assert.Contains(t, stderr.String(), "unknown subcommand")
}

// ---------------------------------------------------------------------------
// runSourceAdd
// ---------------------------------------------------------------------------

func TestRunSourceAdd_MissingName(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runSource([]string{"add", "--type", "postgres", "--spec", "x"}, &stdout, &stderr)
	assert.Equal(t, sourceExitError, rc)
	assert.Contains(t, stderr.String(), "--name is required")
}

func TestRunSourceAdd_MissingType(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runSource([]string{"add", "--name", "db", "--spec", "x"}, &stdout, &stderr)
	assert.Equal(t, sourceExitError, rc)
	assert.Contains(t, stderr.String(), "--type is required")
}

func TestRunSourceAdd_MissingSpecForPostgres(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runSource([]string{"add", "--name", "db", "--type", "postgres",
		"--connection", "postgres://h/db"}, &stdout, &stderr)
	assert.Equal(t, sourceExitError, rc)
	assert.Contains(t, stderr.String(), "openapi is required")
}

func TestRunSourceAdd_ValidationFailureNoWrite(t *testing.T) {
	path := setSourcesFileEnv(t)
	var stdout, stderr strings.Builder
	rc := runSource([]string{"add", "--name", "test", "--type", "bogus", "--spec", "x"}, &stdout, &stderr)
	assert.Equal(t, sourceExitError, rc)
	assert.Contains(t, stderr.String(), "unknown type")
	// The file must NOT have been created.
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "sources file must not be written on validation failure")
}

func TestRunSourceAdd_DryRunNoWrite(t *testing.T) {
	path := setSourcesFileEnv(t)
	var stdout, stderr strings.Builder
	rc := runSource([]string{"add", "--name", "db", "--type", "postgres",
		"--connection", "postgres://u:p@localhost:5432/db",
		"--spec", "./specs/multi-source-integration.md", "--dry-run"}, &stdout, &stderr)
	assert.Equal(t, sourceExitOK, rc)
	assert.Contains(t, stdout.String(), "[DRY-RUN]")
	assert.Contains(t, stdout.String(), "db")
	assert.Contains(t, stdout.String(), path)
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "dry-run must not write the sources file")
}

func TestRunSourceAdd_CreatesAndUpserts(t *testing.T) {
	path := setSourcesFileEnv(t)
	var stdout, stderr strings.Builder

	// First add: postgres source.
	rc := runSource([]string{"add", "--name", "db", "--type", "postgres",
		"--connection", "postgres://u:p@localhost:5432/db",
		"--spec", "./specs/multi-source-integration.md"}, &stdout, &stderr)
	assert.Equal(t, sourceExitOK, rc)
	assert.Contains(t, stdout.String(), "added")

	// Second add: a different source.
	stdout.Reset()
	rc = runSource([]string{"add", "--name", "files", "--type", "local",
		"--root", "/tmp/data", "--read-only"}, &stdout, &stderr)
	assert.Equal(t, sourceExitOK, rc)

	// Third add: upsert db with a rate limit — must not duplicate.
	stdout.Reset()
	rc = runSource([]string{"add", "--name", "db", "--type", "postgres",
		"--connection", "postgres://u:p@localhost:5432/db",
		"--spec", "./specs/multi-source-integration.md",
		"--rate-limit", "10/s"}, &stdout, &stderr)
	assert.Equal(t, sourceExitOK, rc)

	file, err := source.ParseSourcesYAML(path)
	require.NoError(t, err)
	require.Len(t, file.Sources, 2, "upsert must not duplicate the source")

	db, ok := file.Sources["db"]
	require.True(t, ok)
	assert.Equal(t, source.SourceTypePostgres, db.Type)
	assert.Equal(t, "10/s", db.RateLimit, "upsert must update existing source fields")

	files, ok := file.Sources["files"]
	require.True(t, ok)
	assert.Equal(t, source.SourceTypeLocal, files.Type)
	assert.Equal(t, "/tmp/data", files.Root)
	assert.True(t, files.ReadOnly)
}

// ---------------------------------------------------------------------------
// runSourceList
// ---------------------------------------------------------------------------

func TestRunSourceList_NoSources(t *testing.T) {
	path := setSourcesFileEnv(t)
	writeSourcesFile(t, path, "sources: {}\n")
	var stdout, stderr strings.Builder
	rc := runSource([]string{"list"}, &stdout, &stderr)
	assert.Equal(t, sourceExitOK, rc)
	assert.Contains(t, stdout.String(), "no sources configured")
}

func TestRunSourceList_MissingFileIsEmpty(t *testing.T) {
	setSourcesFileEnv(t) // file does not exist yet
	var stdout, stderr strings.Builder
	rc := runSource([]string{"list"}, &stdout, &stderr)
	assert.Equal(t, sourceExitOK, rc)
	assert.Contains(t, stdout.String(), "no sources configured")
}

func TestRunSourceList_SortedByName(t *testing.T) {
	path := setSourcesFileEnv(t)
	writeSourcesFile(t, path, `sources:
  zebra:
    type: local
    root: /tmp/z
  alpha:
    type: rest
    base_url: https://a.example.com
    openapi: ./a.yaml
  mike:
    type: postgres
    connection: postgres://h/db
    openapi: ./m.yaml
    rate_limit: 5/s
`)
	var stdout, stderr strings.Builder
	rc := runSource([]string{"list"}, &stdout, &stderr)
	assert.Equal(t, sourceExitOK, rc)
	out := stdout.String()
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "TYPE")
	assert.Contains(t, out, "READ_ONLY")
	assert.Contains(t, out, "RATE_LIMIT")
	assert.Contains(t, out, "ALLOWED_AGENTS")
	aIdx := strings.Index(out, "alpha")
	mIdx := strings.Index(out, "mike")
	zIdx := strings.Index(out, "zebra")
	assert.True(t, aIdx >= 0 && mIdx > aIdx && zIdx > mIdx, "rows must be sorted by name")
	assert.Contains(t, out, "5/s")
}

func TestRunSourceList_EnabledFilter(t *testing.T) {
	path := setSourcesFileEnv(t)
	writeSourcesFile(t, path, `sources:
  on:
    type: local
    root: /tmp/a
  off:
    type: local
    root: /tmp/b
    enabled: false
`)
	var stdout, stderr strings.Builder

	// Without --enabled both sources appear.
	rc := runSource([]string{"list"}, &stdout, &stderr)
	assert.Equal(t, sourceExitOK, rc)
	out := stdout.String()
	assert.Contains(t, out, "on")
	assert.Contains(t, out, "off")

	// With --enabled only the enabled one appears.
	stdout.Reset()
	rc = runSource([]string{"list", "--enabled"}, &stdout, &stderr)
	assert.Equal(t, sourceExitOK, rc)
	out = stdout.String()
	assert.Contains(t, out, "on")
	assert.NotContains(t, out, "off")
}

func TestRunSourceList_AllDisabled(t *testing.T) {
	path := setSourcesFileEnv(t)
	writeSourcesFile(t, path, `sources:
  a:
    type: local
    root: /tmp/a
    enabled: false
`)
	var stdout, stderr strings.Builder
	rc := runSource([]string{"list", "--enabled"}, &stdout, &stderr)
	assert.Equal(t, sourceExitOK, rc)
	assert.Contains(t, stdout.String(), "no enabled sources configured")
}

// ---------------------------------------------------------------------------
// runSourceTest
// ---------------------------------------------------------------------------

func TestRunSourceTest_MissingName(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runSource([]string{"test"}, &stdout, &stderr)
	assert.Equal(t, sourceExitError, rc)
	assert.Contains(t, stderr.String(), "--name is required")
}

func TestRunSourceTest_SourceNotFound(t *testing.T) {
	path := setSourcesFileEnv(t)
	writeSourcesFile(t, path, "sources: {}\n")
	var stdout, stderr strings.Builder
	rc := runSource([]string{"test", "--name", "nope"}, &stdout, &stderr)
	assert.Equal(t, sourceExitError, rc)
	assert.Contains(t, stderr.String(), "not found")
}

func TestRunSourceTest_LocalRootExists(t *testing.T) {
	path := setSourcesFileEnv(t)
	root := t.TempDir()
	writeSourcesFile(t, path, "sources:\n  fs:\n    type: local\n    root: "+root+"\n")
	var stdout, stderr strings.Builder
	rc := runSource([]string{"test", "--name", "fs"}, &stdout, &stderr)
	assert.Equal(t, sourceExitOK, rc)
	out := stdout.String()
	assert.Contains(t, out, "✓ local root")
	assert.Contains(t, out, "✓ source \"fs\" checks passed")
	// Muster may or may not be running on this host; either outcome is
	// acceptable and must not fail the command.
	assert.True(t,
		strings.Contains(out, "✓ muster reachable") || strings.Contains(out, "⚠ muster unreachable"),
		"muster health must be reported as reachable or warned, got: %s", out)
}

func TestRunSourceTest_LocalRootMissing(t *testing.T) {
	path := setSourcesFileEnv(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	writeSourcesFile(t, path, "sources:\n  fs:\n    type: local\n    root: "+missing+"\n")
	var stdout, stderr strings.Builder
	rc := runSource([]string{"test", "--name", "fs"}, &stdout, &stderr)
	assert.Equal(t, sourceExitError, rc)
	assert.Contains(t, stdout.String(), "✗ local root")
}

func TestRunSourceTest_LocalRootIsFile(t *testing.T) {
	path := setSourcesFileEnv(t)
	f := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0o644))
	writeSourcesFile(t, path, "sources:\n  fs:\n    type: local\n    root: "+f+"\n")
	var stdout, stderr strings.Builder
	rc := runSource([]string{"test", "--name", "fs"}, &stdout, &stderr)
	assert.Equal(t, sourceExitError, rc)
	assert.Contains(t, stdout.String(), "not a directory")
}

func TestRunSourceTest_MissingOpenAPISpec(t *testing.T) {
	path := setSourcesFileEnv(t)
	root := t.TempDir()
	writeSourcesFile(t, path, "sources:\n  fs:\n    type: local\n    root: "+root+
		"\n    openapi: ./missing-spec.yaml\n")
	var stdout, stderr strings.Builder
	rc := runSource([]string{"test", "--name", "fs"}, &stdout, &stderr)
	assert.Equal(t, sourceExitError, rc)
	assert.Contains(t, stdout.String(), "✗ openapi spec")
}

func TestRunSourceTest_UnparseablePostgresConnectionIsWarning(t *testing.T) {
	path := setSourcesFileEnv(t)
	writeSourcesFile(t, path, `sources:
  db:
    type: postgres
    connection: "not a url"
    openapi: ./db.yaml
`)
	var stdout, stderr strings.Builder
	rc := runSource([]string{"test", "--name", "db"}, &stdout, &stderr)
	// Unparseable connection is a WARNING, not a failure — but the missing
	// spec file below it IS a failure, so the command exits 2.
	assert.Equal(t, sourceExitError, rc)
	assert.Contains(t, stdout.String(), "⚠ postgres connection not parseable")
	assert.Contains(t, stdout.String(), "✗ openapi spec")
}

// ---------------------------------------------------------------------------
// runSourceTools
// ---------------------------------------------------------------------------

func TestRunSourceTools_MissingName(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runSource([]string{"tools"}, &stdout, &stderr)
	assert.Equal(t, sourceExitError, rc)
	assert.Contains(t, stderr.String(), "--name is required")
}

func TestRunSourceTools_SourceNotFound(t *testing.T) {
	path := setSourcesFileEnv(t)
	writeSourcesFile(t, path, "sources: {}\n")
	var stdout, stderr strings.Builder
	rc := runSource([]string{"tools", "--name", "nope"}, &stdout, &stderr)
	assert.Equal(t, sourceExitError, rc)
	assert.Contains(t, stderr.String(), "not found")
}

func TestRunSourceTools_MusterUnreachable(t *testing.T) {
	path := setSourcesFileEnv(t)
	writeSourcesFile(t, path, `sources:
  api:
    type: rest
    base_url: https://api.example.com
    openapi: ./api.yaml
`)
	t.Setenv(envMusterURL, "http://127.0.0.1:1") // closed port
	var stdout, stderr strings.Builder
	rc := runSource([]string{"tools", "--name", "api"}, &stdout, &stderr)
	assert.Equal(t, sourceExitError, rc)
	assert.Contains(t, stderr.String(), "source tools:")
}

// ---------------------------------------------------------------------------
// runSourceWithDryRun — errExit contract
// ---------------------------------------------------------------------------

func TestRunSourceWithDryRun_ErrExitOnFailure(t *testing.T) {
	path := setSourcesFileEnv(t)
	writeSourcesFile(t, path, "sources: {}\n")
	var stdout, stderr strings.Builder
	err := runSourceWithDryRun([]string{"test", "--name", "nope"}, &stdout, &stderr, false)
	require.Error(t, err)
	var ee errExit
	assert.True(t, errors.As(err, &ee), "error must wrap errExit")
	assert.Equal(t, sourceExitError, ee.code)
}

func TestRunSourceWithDryRun_NilOnSuccess(t *testing.T) {
	setSourcesFileEnv(t)
	var stdout, stderr strings.Builder
	err := runSourceWithDryRun([]string{"list"}, &stdout, &stderr, false)
	assert.NoError(t, err)
	assert.Contains(t, stdout.String(), "no sources configured")
}

func TestRunSourceWithDryRun_ThreadsGlobalDryRun(t *testing.T) {
	path := setSourcesFileEnv(t)
	var stdout, stderr strings.Builder
	err := runSourceWithDryRun([]string{"add", "--name", "db", "--type", "postgres",
		"--connection", "postgres://u:p@localhost:5432/db",
		"--spec", "./specs/multi-source-integration.md"}, &stdout, &stderr, true)
	assert.NoError(t, err)
	assert.Contains(t, stdout.String(), "[DRY-RUN]")
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "global dry-run must not write the sources file")
}

// ---------------------------------------------------------------------------
// probeREST — base-URL probe with spec-path fallback (DF-013)
// ---------------------------------------------------------------------------

// restTestServer returns an httptest server whose root answers with
// rootStatus (Forgejo-style 404 when no root route exists) and whose
// non-root paths answer with pathStatus.
func restTestServer(t *testing.T, rootStatus, pathStatus int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/api/v1" {
			w.WriteHeader(rootStatus)
			return
		}
		w.WriteHeader(pathStatus)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestProbeREST_BaseURLReachable(t *testing.T) {
	srv := restTestServer(t, http.StatusOK, http.StatusOK)
	warning, err := probeREST(srv.URL+"/api/v1", "")
	assert.NoError(t, err)
	assert.Empty(t, warning)
}

func TestProbeREST_AuthWrappedReachable(t *testing.T) {
	srv := restTestServer(t, http.StatusUnauthorized, http.StatusUnauthorized)
	warning, err := probeREST(srv.URL, "")
	assert.NoError(t, err)
	assert.Empty(t, warning, "401 on the base URL counts as reachable")
}

func TestProbeREST_Base404WithoutProbePathFails(t *testing.T) {
	srv := restTestServer(t, http.StatusNotFound, http.StatusNotFound)
	warning, err := probeREST(srv.URL, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 404")
	assert.Empty(t, warning)
}

func TestProbeREST_Base404FallsBackToProbePath(t *testing.T) {
	srv := restTestServer(t, http.StatusNotFound, http.StatusOK)
	warning, err := probeREST(srv.URL+"/api/v1", "/version")
	assert.NoError(t, err)
	require.NotEmpty(t, warning, "degraded probe must be reported as a warning")
	assert.Contains(t, warning, "HTTP 404")
	assert.Contains(t, warning, "/version")
}

func TestProbeREST_FallbackAcceptsAnyStatus(t *testing.T) {
	// The fallback probe must treat ANY HTTP response — even 404 — as
	// reachable: the server answered, which is all the probe checks.
	srv := restTestServer(t, http.StatusNotFound, http.StatusNotFound)
	warning, err := probeREST(srv.URL, "/anything")
	assert.NoError(t, err)
	assert.Contains(t, warning, "/anything")
}

func TestProbeREST_NetworkErrorFails(t *testing.T) {
	// A closed port must fail the source test even when a probe path is
	// offered — the fallback probes the same host.
	warning, err := probeREST("http://127.0.0.1:1/api/v1", "/version")
	assert.Error(t, err)
	assert.Empty(t, warning)
}

// ---------------------------------------------------------------------------
// firstGETPath / restProbePath — spec-path derivation
// ---------------------------------------------------------------------------

func TestFirstGETPath_JSONSpecPrefersNonTemplatedSorted(t *testing.T) {
	spec := filepath.Join(t.TempDir(), "spec.json")
	writeSourcesFile(t, spec, `{
  "swagger": "2.0",
  "paths": {
    "/version": {"get": {}},
    "/repos/{owner}/{repo}": {"get": {}},
    "/admin/users": {"get": {}}
  }
}`)
	p, err := firstGETPath(spec)
	require.NoError(t, err)
	assert.Equal(t, "/admin/users", p, "first non-templated GET path, sorted")
}

func TestFirstGETPath_YAMLSpec(t *testing.T) {
	spec := filepath.Join(t.TempDir(), "spec.yaml")
	writeSourcesFile(t, spec, `openapi: 3.0.0
paths:
  /users:
    get:
      responses: {}
  /health:
    get:
      responses: {}
`)
	p, err := firstGETPath(spec)
	require.NoError(t, err)
	assert.Equal(t, "/health", p)
}

func TestFirstGETPath_TemplatedOnly(t *testing.T) {
	spec := filepath.Join(t.TempDir(), "spec.json")
	writeSourcesFile(t, spec, `{"paths": {"/repos/{owner}": {"get": {}}}}`)
	p, err := firstGETPath(spec)
	require.NoError(t, err)
	assert.Equal(t, "/repos/{owner}", p, "templated paths are a last resort")
}

func TestFirstGETPath_NoGETPaths(t *testing.T) {
	spec := filepath.Join(t.TempDir(), "spec.json")
	writeSourcesFile(t, spec, `{"paths": {"/users": {"post": {}}}}`)
	_, err := firstGETPath(spec)
	assert.Error(t, err)
}

func TestFirstGETPath_MissingFile(t *testing.T) {
	_, err := firstGETPath(filepath.Join(t.TempDir(), "nope.json"))
	assert.Error(t, err)
}

func TestRestProbePath_ExplicitFlagWins(t *testing.T) {
	got := restProbePath("/health", source.Source{OpenAPI: "./spec.json"})
	assert.Equal(t, "/health", got)
}

func TestRestProbePath_FromSpec(t *testing.T) {
	spec := filepath.Join(t.TempDir(), "spec.json")
	writeSourcesFile(t, spec, `{"paths": {"/version": {"get": {}}}}`)
	got := restProbePath("", source.Source{OpenAPI: spec})
	assert.Equal(t, "/version", got)
}

func TestRestProbePath_RemoteSpecNoFallback(t *testing.T) {
	got := restProbePath("", source.Source{OpenAPI: "https://example.com/swagger.json"})
	assert.Empty(t, got)
}

func TestRestProbePath_UnparseableSpecNoFallback(t *testing.T) {
	spec := filepath.Join(t.TempDir(), "spec.json")
	writeSourcesFile(t, spec, "not: [valid")
	got := restProbePath("", source.Source{OpenAPI: spec})
	assert.Empty(t, got)
}

// ---------------------------------------------------------------------------
// runSourceTest — REST fallback end-to-end (DF-013)
// ---------------------------------------------------------------------------

func TestRunSourceTest_RESTBase404FallsBackToSpecPath(t *testing.T) {
	path := setSourcesFileEnv(t)
	spec := filepath.Join(t.TempDir(), "spec.json")
	writeSourcesFile(t, spec, `{"paths": {"/version": {"get": {}}}}`)
	srv := restTestServer(t, http.StatusNotFound, http.StatusOK)
	writeSourcesFile(t, path, fmt.Sprintf("sources:\n  api:\n    type: rest\n    base_url: %s/api/v1\n    openapi: %s\n", srv.URL, spec))
	t.Setenv(envMusterURL, "http://127.0.0.1:1") // closed port — warning only
	var stdout, stderr strings.Builder
	rc := runSource([]string{"test", "--name", "api"}, &stdout, &stderr)
	assert.Equal(t, sourceExitOK, rc)
	out := stdout.String()
	assert.Contains(t, out, "⚠ rest base_url")
	assert.Contains(t, out, "HTTP 404")
	assert.Contains(t, out, "/version")
	assert.Contains(t, out, "✓ rest source \"api\" reachable (fallback probe)")
	assert.Contains(t, out, "✓ source \"api\" checks passed")
}

func TestRunSourceTest_RESTBrokenBaseFails(t *testing.T) {
	path := setSourcesFileEnv(t)
	spec := filepath.Join(t.TempDir(), "spec.json")
	writeSourcesFile(t, spec, `{"paths": {"/version": {"get": {}}}}`)
	writeSourcesFile(t, path, fmt.Sprintf("sources:\n  api:\n    type: rest\n    base_url: http://127.0.0.1:1/api/v1\n    openapi: %s\n", spec))
	t.Setenv(envMusterURL, "http://127.0.0.1:1")
	var stdout, stderr strings.Builder
	rc := runSource([]string{"test", "--name", "api"}, &stdout, &stderr)
	assert.Equal(t, sourceExitError, rc)
	assert.Contains(t, stdout.String(), "✗ rest base_url")
}

func TestRunSourceTest_RESTProbePathFlag(t *testing.T) {
	// Explicit --probe-path covers servers whose spec has no usable GET
	// path (e.g. POST-only specs).
	path := setSourcesFileEnv(t)
	spec := filepath.Join(t.TempDir(), "spec.json")
	writeSourcesFile(t, spec, `{"paths": {"/users": {"post": {}}}}`)
	srv := restTestServer(t, http.StatusNotFound, http.StatusOK)
	writeSourcesFile(t, path, fmt.Sprintf("sources:\n  api:\n    type: rest\n    base_url: %s\n    openapi: %s\n", srv.URL, spec))
	t.Setenv(envMusterURL, "http://127.0.0.1:1")
	var stdout, stderr strings.Builder
	rc := runSource([]string{"test", "--name", "api", "--probe-path", "/health"}, &stdout, &stderr)
	assert.Equal(t, sourceExitOK, rc)
	out := stdout.String()
	assert.Contains(t, out, "⚠ rest base_url")
	assert.Contains(t, out, "/health")
	assert.Contains(t, out, "✓ source \"api\" checks passed")
}
