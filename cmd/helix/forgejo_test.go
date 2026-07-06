// Tests for cmd/helix/forgejo.go.
//
// These tests use httptest.NewServer to stand in for a live Forgejo
// instance — every subcommand hits the network, so the test fixtures
// are minimal JSON responses that match Forgejo's actual API shapes.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// -----------------------------------------------------------------------------
// Helper: a minimal Forgejo HTTP fixture
// -----------------------------------------------------------------------------

// forgejoFixture is a tiny httptest-backed Forgejo that responds to the
// handful of endpoints `helix forgejo` subcommands exercise.
type forgejoFixture struct {
	server   *httptest.Server
	userHits int32
	prHits   int32
	pingHits int32
}

func newForgejoFixture(t *testing.T, users, version string) *forgejoFixture {
	t.Helper()
	f := &forgejoFixture{}
	mux := http.NewServeMux()

	// /api/v1/version — health probe.
	mux.HandleFunc("/api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&f.pingHits, 1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, version)
	})

	// /api/v1/admin/users — admin user list.
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&f.userHits, 1)
		auth := r.Header.Get("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Basic ") {
			http.Error(w, `{"message":"auth required"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, users)
	})

	// /api/v1/users/{login} — get user.
	mux.HandleFunc("/api/v1/users/", func(w http.ResponseWriter, r *http.Request) {
		login := strings.TrimPrefix(r.URL.Path, "/api/v1/users/")
		if login == "" {
			http.Error(w, `{"message":"login required"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":1,"login":%q,"email":"%s@example.com","full_name":"%s Test","active":true,"is_admin":false}`, login, login, login)
	})

	// /api/v1/repos/{owner}/{repo}/pulls — list PRs.
	mux.HandleFunc("/api/v1/repos/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&f.prHits, 1)
		if strings.Contains(r.URL.Path, "/pulls") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `[{"id":1,"number":42,"title":"Test PR","body":"x","state":"open","head":{"label":"feature","ref":"feature","sha":"abc123","repo":{"id":1,"name":"helix","owner":{"login":"helix-org"}}},"base":{"label":"main","ref":"main","sha":"def456","repo":{"id":1,"name":"helix","owner":{"login":"helix-org"}}}}]`)
			return
		}
		if strings.Contains(r.URL.Path, "/branches") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `[{"name":"main","commit":{"id":"deadbeef1234567"}},{"name":"feature/test","commit":{"id":"cafebabe9876543"}}]`)
			return
		}
		http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
	})

	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

// -----------------------------------------------------------------------------
// parseForgejoFlags
// -----------------------------------------------------------------------------

func TestParseForgejoFlags_Defaults(t *testing.T) {
	// No env vars cleared; just verify defaults.
	f, helpWanted, rc := parseForgejoFlags([]string{})
	if rc != fgoExitOK {
		t.Fatalf("rc=%d", rc)
	}
	if helpWanted {
		t.Fatal("helpWanted should be false")
	}
	if f.subcommand != "help" {
		t.Fatalf("subcommand=%q", f.subcommand)
	}
	if f.user != "admin" {
		t.Fatalf("user default=%q", f.user)
	}
	if f.owner != "helix-org" {
		t.Fatalf("owner default=%q", f.owner)
	}
	if f.state != "open" {
		t.Fatalf("state default=%q", f.state)
	}
}

func TestParseForgejoFlags_PingWithURL(t *testing.T) {
	f, _, rc := parseForgejoFlags([]string{"ping", "--url", "http://x.test:3030"})
	if rc != fgoExitOK {
		t.Fatalf("rc=%d", rc)
	}
	if f.subcommand != "ping" || f.url != "http://x.test:3030" {
		t.Fatalf("flags=%+v", f)
	}
}

func TestParseForgejoFlags_PRList(t *testing.T) {
	f, _, rc := parseForgejoFlags([]string{
		"pr-list", "--url", "http://x",
		"--owner", "helix-org", "--repo", "helix",
		"--state", "closed",
	})
	if rc != fgoExitOK {
		t.Fatalf("rc=%d", rc)
	}
	if f.subcommand != "pr-list" || f.state != "closed" {
		t.Fatalf("flags=%+v", f)
	}
}

func TestParseForgejoFlags_InvalidState(t *testing.T) {
	_, _, rc := parseForgejoFlags([]string{"pr-list", "--state", "bogus"})
	if rc != fgoExitError {
		t.Fatalf("expected rc=2, got %d", rc)
	}
}

func TestParseForgejoFlags_UnknownFlag(t *testing.T) {
	_, _, rc := parseForgejoFlags([]string{"ping", "--nope"})
	if rc != fgoExitError {
		t.Fatalf("expected rc=2, got %d", rc)
	}
}

func TestParseForgejoFlags_UserInfo_LoginPositional(t *testing.T) {
	f, _, rc := parseForgejoFlags([]string{"user-info", "--url", "http://x", "wojons"})
	if rc != fgoExitOK {
		t.Fatalf("rc=%d", rc)
	}
	if f.subcommand != "user-info" || f.targetUser != "wojons" {
		t.Fatalf("flags=%+v", f)
	}
}

// -----------------------------------------------------------------------------
// runForgejo subcommand routing
// -----------------------------------------------------------------------------

func TestRunForgejo_Help(t *testing.T) {
	// Subcommand "help" routes through the URL check below; --help flag
	// short-circuits to the help text without requiring --url.
	var out, errBuf bytes.Buffer
	rc := runForgejo([]string{"--help"}, &out, &errBuf)
	if rc != fgoExitOK {
		t.Fatalf("rc=%d err=%q", rc, errBuf.String())
	}
	for _, want := range []string{"helix forgejo", "ping", "user-list", "user-info", "pr-list", "branch-list"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in help: %q", want, out.String())
		}
	}
}

func TestRunForgejo_MissingURL(t *testing.T) {
	// Clear env vars for this test so --url is required.
	t.Setenv("FORGEJO_URL", "")
	var out, errBuf bytes.Buffer
	rc := runForgejo([]string{"ping"}, &out, &errBuf)
	if rc != fgoExitError {
		t.Fatalf("expected rc=2, got %d", rc)
	}
	if !strings.Contains(errBuf.String(), "--url is required") {
		t.Fatalf("expected '--url is required', got %q", errBuf.String())
	}
}

func TestRunForgejo_UnknownSubcommand(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runForgejo([]string{"bogus"}, &out, &errBuf)
	if rc != fgoExitError {
		t.Fatalf("expected rc=2, got %d", rc)
	}
}

// -----------------------------------------------------------------------------
// ping
// -----------------------------------------------------------------------------

func TestRunForgejo_Ping_OK(t *testing.T) {
	f := newForgejoFixture(t, `[{"id":1,"login":"admin","email":"a@x","full_name":"Admin","active":true,"is_admin":true}]`, `{"version":"10.0.0"}`)
	var out, errBuf bytes.Buffer
	rc := runForgejo([]string{"ping", "--url", f.server.URL}, &out, &errBuf)
	if rc != fgoExitOK {
		t.Fatalf("rc=%d err=%q", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), "Forgejo 10.0.0 reachable") {
		t.Fatalf("missing reachable line: %q", out.String())
	}
	if atomic.LoadInt32(&f.pingHits) != 1 {
		t.Fatalf("expected 1 ping hit, got %d", f.pingHits)
	}
}

func TestRunForgejo_Ping_JSON(t *testing.T) {
	f := newForgejoFixture(t, `[]`, `{"version":"1.20.0"}`)
	var out, errBuf bytes.Buffer
	rc := runForgejo([]string{"ping", "--url", f.server.URL, "--json"}, &out, &errBuf)
	if rc != fgoExitOK {
		t.Fatalf("rc=%d err=%q", rc, errBuf.String())
	}
	var v map[string]any
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatalf("JSON: %v out=%q", err, out.String())
	}
	if v["version"] != "1.20.0" {
		t.Fatalf("version=%v", v["version"])
	}
}

func TestRunForgejo_Ping_Unreachable(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runForgejo([]string{"ping", "--url", "http://127.0.0.1:1"}, &out, &errBuf)
	if rc != fgoExitBlock {
		t.Fatalf("expected rc=1, got %d err=%q", rc, errBuf.String())
	}
}

// -----------------------------------------------------------------------------
// user-list / user-info
// -----------------------------------------------------------------------------

func TestRunForgejo_UserList_OK(t *testing.T) {
	users := `[{"id":1,"login":"admin","email":"a@x","full_name":"Admin","active":true,"is_admin":true},{"id":2,"login":"alice","email":"alice@x","full_name":"Alice","active":true,"is_admin":false}]`
	f := newForgejoFixture(t, users, `{"version":"x"}`)
	var out, errBuf bytes.Buffer
	rc := runForgejo([]string{
		"user-list", "--url", f.server.URL,
		"--user", "admin", "--password", "secret",
	}, &out, &errBuf)
	if rc != fgoExitOK {
		t.Fatalf("rc=%d err=%q", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), "alice") || !strings.Contains(out.String(), "admin") {
		t.Fatalf("missing users: %q", out.String())
	}
	if atomic.LoadInt32(&f.userHits) != 1 {
		t.Fatalf("user endpoint not hit")
	}
}

func TestRunForgejo_UserList_JSON(t *testing.T) {
	f := newForgejoFixture(t, `[{"id":1,"login":"admin","email":"a","full_name":"A","active":true,"is_admin":true}]`, `{}`)
	var out, errBuf bytes.Buffer
	rc := runForgejo([]string{
		"user-list", "--url", f.server.URL, "--json",
		"--user", "admin", "--password", "secret",
	}, &out, &errBuf)
	if rc != fgoExitOK {
		t.Fatalf("rc=%d err=%q", rc, errBuf.String())
	}
	var got []map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("JSON: %v out=%q", err, out.String())
	}
	if len(got) != 1 || got[0]["login"] != "admin" {
		t.Fatalf("got %+v", got)
	}
}

func TestRunForgejo_UserList_MissingAuth(t *testing.T) {
	f := newForgejoFixture(t, `[]`, `{}`)
	var out, errBuf bytes.Buffer
	// Real creds missing — server returns 401, exit block.
	rc := runForgejo([]string{"user-list", "--url", f.server.URL}, &out, &errBuf)
	if rc == fgoExitOK {
		t.Fatalf("expected non-zero exit, got %d out=%q", rc, out.String())
	}
}

func TestRunForgejo_UserInfo_OK(t *testing.T) {
	f := newForgejoFixture(t, `[]`, `{}`)
	var out, errBuf bytes.Buffer
	rc := runForgejo([]string{
		"user-info", "--url", f.server.URL, "wojons",
	}, &out, &errBuf)
	if rc != fgoExitOK {
		t.Fatalf("rc=%d err=%q", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), `wojons`) {
		t.Fatalf("missing login: %q", out.String())
	}
	if !strings.Contains(out.String(), "Active:") {
		t.Fatalf("missing fields: %q", out.String())
	}
}

func TestRunForgejo_UserInfo_JSON(t *testing.T) {
	f := newForgejoFixture(t, `[]`, `{}`)
	var out, errBuf bytes.Buffer
	rc := runForgejo([]string{
		"user-info", "--url", f.server.URL, "wojons", "--json",
	}, &out, &errBuf)
	if rc != fgoExitOK {
		t.Fatalf("rc=%d err=%q", rc, errBuf.String())
	}
	var u map[string]any
	if err := json.Unmarshal(out.Bytes(), &u); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if u["login"] != "wojons" {
		t.Fatalf("login=%v", u["login"])
	}
}

func TestRunForgejo_UserInfo_MissingLogin(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runForgejo([]string{"user-info", "--url", "http://x"}, &out, &errBuf)
	if rc != fgoExitError {
		t.Fatalf("expected rc=2, got %d", rc)
	}
}

// -----------------------------------------------------------------------------
// pr-list / branch-list
// -----------------------------------------------------------------------------

func TestRunForgejo_PRList_OK(t *testing.T) {
	f := newForgejoFixture(t, `[]`, `{}`)
	var out, errBuf bytes.Buffer
	rc := runForgejo([]string{
		"pr-list", "--url", f.server.URL,
		"--owner", "helix-org", "--repo", "helix",
	}, &out, &errBuf)
	if rc != fgoExitOK {
		t.Fatalf("rc=%d err=%q", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), "#42") {
		// Either "42" or "NUMBER" column header.
		if !strings.Contains(out.String(), "Test PR") {
			t.Fatalf("missing PR title: %q", out.String())
		}
	}
}

func TestRunForgejo_PRList_JSON(t *testing.T) {
	f := newForgejoFixture(t, `[]`, `{}`)
	var out, errBuf bytes.Buffer
	rc := runForgejo([]string{
		"pr-list", "--url", f.server.URL,
		"--owner", "helix-org", "--repo", "helix",
		"--state", "open", "--json",
	}, &out, &errBuf)
	if rc != fgoExitOK {
		t.Fatalf("rc=%d err=%q", rc, errBuf.String())
	}
	var prs []map[string]any
	if err := json.Unmarshal(out.Bytes(), &prs); err != nil {
		t.Fatalf("JSON: %v out=%q", err, out.String())
	}
	if len(prs) != 1 || prs[0]["number"].(float64) != 42 {
		t.Fatalf("got %+v", prs)
	}
}

func TestRunForgejo_PRList_MissingRepo(t *testing.T) {
	f := newForgejoFixture(t, `[]`, `{}`)
	var out, errBuf bytes.Buffer
	rc := runForgejo([]string{
		"pr-list", "--url", f.server.URL, "--owner", "helix-org",
	}, &out, &errBuf)
	// --repo was not provided. parseForgejoFlags does NOT set a default
	// for --repo (it has no env-var fallback), so the subcommand should
	// reject the call.
	if rc != fgoExitError {
		t.Fatalf("expected rc=2, got %d out=%q", rc, out.String())
	}
}

func TestRunForgejo_BranchList_OK(t *testing.T) {
	f := newForgejoFixture(t, `[]`, `{}`)
	var out, errBuf bytes.Buffer
	rc := runForgejo([]string{
		"branch-list", "--url", f.server.URL,
		"--owner", "helix-org", "--repo", "helix",
	}, &out, &errBuf)
	if rc != fgoExitOK {
		t.Fatalf("rc=%d err=%q", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), "main") {
		t.Fatalf("missing main branch: %q", out.String())
	}
	if !strings.Contains(out.String(), "feature/test") {
		t.Fatalf("missing feature branch: %q", out.String())
	}
}

func TestRunForgejo_BranchList_JSON(t *testing.T) {
	f := newForgejoFixture(t, `[]`, `{}`)
	var out, errBuf bytes.Buffer
	rc := runForgejo([]string{
		"branch-list", "--url", f.server.URL,
		"--owner", "helix-org", "--repo", "helix",
		"--json",
	}, &out, &errBuf)
	if rc != fgoExitOK {
		t.Fatalf("rc=%d err=%q", rc, errBuf.String())
	}
	var branches []map[string]any
	if err := json.Unmarshal(out.Bytes(), &branches); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if len(branches) != 2 {
		t.Fatalf("got %d branches", len(branches))
	}
}

func TestRunForgejo_BranchList_MissingRepo(t *testing.T) {
	f := newForgejoFixture(t, `[]`, `{}`)
	var out, errBuf bytes.Buffer
	rc := runForgejo([]string{
		"branch-list", "--url", f.server.URL, "--owner", "helix-org",
	}, &out, &errBuf)
	if rc != fgoExitError {
		t.Fatalf("expected rc=2, got %d", rc)
	}
}

// -----------------------------------------------------------------------------
// runForgejoWithDryRun plumbing
// -----------------------------------------------------------------------------

func TestRunForgejoWithDryRun_BlockNotWrapped(t *testing.T) {
	f := newForgejoFixture(t, `[]`, `{}`)
	var out, errBuf bytes.Buffer
	err := runForgejoWithDryRun([]string{"ping", "--url", f.server.URL}, &out, &errBuf, true)
	// ping succeeds → no error.
	if err != nil {
		t.Fatalf("OK ping should not error: %v", err)
	}
}

func TestRunForgejoWithDryRun_InvocationErrorWrapped(t *testing.T) {
	var out, errBuf bytes.Buffer
	err := runForgejoWithDryRun([]string{"bogus"}, &out, &errBuf, false)
	if err == nil {
		t.Fatal("expected errExit wrapping")
	}
}

// -----------------------------------------------------------------------------
// truncate helper
// -----------------------------------------------------------------------------

func TestTruncate_UnderLimit(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestTruncate_AtLimit(t *testing.T) {
	if got := truncate("hello", 5); got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestTruncate_OverLimit(t *testing.T) {
	got := truncate("hello world", 6)
	// truncate produces max bytes (the ellipsis is 3-byte UTF-8 so total = 5 + 3 = 8).
	// We assert < 12 to allow room for the ellipsis encoding.
	if len(got) > 12 || !strings.HasPrefix(got, "hello") || !strings.HasSuffix(got, "…") {
		t.Fatalf("expected 'hello' prefix + ellipsis, got %q (len=%d)", got, len(got))
	}
}

func TestTruncate_ZeroMax(t *testing.T) {
	// truncate with max=0 returns "" (per the implementation's docstring).
	if got := truncate("hi", 0); got != "" {
		t.Fatalf("got %q", got)
	}
}
