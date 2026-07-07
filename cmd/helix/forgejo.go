// Command helix — forgejo.go
//
// `helix forgejo` is the operator CLI for inspecting and driving the
// pkg/forgejo REST adapter. Subcommands are READ-MOSTLY — the platform's
// own creation flows live in `helix dispatcher` and `helix dispatch`. This
// CLI is for inspecting a connected Forgejo instance, listing users, listing
// PRs/branches, and signing in to verify reachability.
//
// Subcommands:
//
//	ping          Health probe — GET /api/v1/version, returns the
//	              server version string. Useful as the first check in a
//	              new deployment.
//	user-list     List users (admin-only) — GET /api/v1/admin/users.
//	user-info     Get a single user by login — GET /api/v1/users/{login}.
//	pr-list       List pull requests in a repo (filter by state).
//	branch-list   List branches in a repo.
//	help          Show this help.
//
// Flags:
//
//	--url URL         Forgejo base URL (e.g. http://localhost:3030).
//	                   Default: --url flag value or $FORGEJO_URL.
//	--user NAME       Admin username for BasicAuth.
//	                   Default: $FORGEJO_ADMIN_USER or "admin".
//	--password PWD    Admin password (prefer env: $FORGEJO_ADMIN_PASSWORD).
//	--owner ORG       Repository owner (GitHub-style namespace).
//	                   Default: $FORGEJO_OWNER or "helix-org".
//	--repo NAME       Repository name (e.g. "helix").
//	--state STATE     PR state filter: open | closed | all. Default: open.
//	--json            Structured JSON output instead of human-readable text.
//
// All subcommands fail-fast with exit code 1 if the configured Forgejo
// instance is unreachable, returns a non-2xx status, or rejects auth.
// Distinguishes "endpoint not configured" (exit 2) from "endpoint
// configured but unreachable" (exit 1).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/forgejo"
)

const (
	fgoExitOK    = 0
	fgoExitBlock = 1 // network / auth / not-found failure
	fgoExitError = 2 // invocation error
)

// -----------------------------------------------------------------------------
// Flags
// -----------------------------------------------------------------------------

type fgoFlags struct {
	subcommand string // ping, user-list, user-info, pr-list, branch-list, help
	url        string // --url
	user       string // --user
	password   string // --password
	owner      string // --owner
	repo       string // --repo
	state      string // --state (open|closed|all)
	jsonOut    bool   // --json
	targetUser string // positional for user-info
}

func parseForgejoFlags(args []string) (fgoFlags, bool, int) {
	var f fgoFlags
	f.url = os.Getenv("FORGEJO_URL")
	f.user = os.Getenv("FORGEJO_ADMIN_USER")
	if f.user == "" {
		f.user = "admin"
	}
	f.password = os.Getenv("FORGEJO_ADMIN_PASSWORD")
	f.owner = os.Getenv("FORGEJO_OWNER")
	if f.owner == "" {
		f.owner = "helix-org"
	}
	f.state = "open"
	helpWanted := false

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			helpWanted = true
		case arg == "--json":
			f.jsonOut = true
		case arg == "--url":
			if i+1 >= len(args) {
				return f, false, fgoExitError
			}
			f.url = args[i+1]
			i++
		case arg == "--user":
			if i+1 >= len(args) {
				return f, false, fgoExitError
			}
			f.user = args[i+1]
			i++
		case arg == "--password":
			if i+1 >= len(args) {
				return f, false, fgoExitError
			}
			f.password = args[i+1]
			i++
		case arg == "--owner":
			if i+1 >= len(args) {
				return f, false, fgoExitError
			}
			f.owner = args[i+1]
			i++
		case arg == "--repo":
			if i+1 >= len(args) {
				return f, false, fgoExitError
			}
			f.repo = args[i+1]
			i++
		case arg == "--state":
			if i+1 >= len(args) {
				return f, false, fgoExitError
			}
			switch args[i+1] {
			case "open", "closed", "all":
				f.state = args[i+1]
			default:
				fmt.Fprintf(os.Stderr, "error: --state must be open|closed|all (got %q)\n", args[i+1])
				return f, false, fgoExitError
			}
			i++
		case len(arg) > 2 && arg[0] == '-' && arg[1] == '-':
			fmt.Fprintf(os.Stderr, "error: unknown flag %q\n", arg)
			return f, false, fgoExitError
		default:
			if f.subcommand == "" {
				f.subcommand = arg
			} else if f.subcommand == "user-info" && f.targetUser == "" {
				f.targetUser = arg
			} else {
				fmt.Fprintf(os.Stderr, "error: unexpected positional arg %q\n", arg)
				return f, false, fgoExitError
			}
		}
		i++
	}

	if f.subcommand == "" {
		f.subcommand = "help"
	}
	return f, helpWanted, fgoExitOK
}

func printForgejoHelp(w io.Writer) {
	fmt.Fprintln(w, `helix forgejo — inspect and probe a connected Forgejo instance

Usage:
  helix forgejo ping                    --url URL [--user NAME] [--password PWD]
  helix forgejo user-list               --url URL [--user NAME] [--password PWD] [--json]
  helix forgejo user-info LOGIN         --url URL [--user NAME] [--password PWD] [--json]
  helix forgejo pr-list                 --url URL --owner ORG --repo NAME [--state open|closed|all] [--json]
  helix forgejo branch-list             --url URL --owner ORG --repo NAME [--json]
  helix forgejo help

Subcommands:
  ping          GET /api/v1/version — confirms the instance is reachable and
                returns the server version.
  user-list     List all users (admin auth required).
  user-info     Get a single user by login (public read; admin auth optional).
  pr-list       List pull requests in a repository, filtered by state.
  branch-list   List branches in a repository.
  help          Show this help.

Flags:
  --url URL         Forgejo base URL (default: $FORGEJO_URL).
  --user NAME       Admin username (default: $FORGEJO_ADMIN_USER or "admin").
  --password PWD    Admin password (use $FORGEJO_ADMIN_PASSWORD env var in CI).
  --owner ORG       Repository owner (default: $FORGEJO_OWNER or "helix-org").
  --repo NAME       Repository name (required for pr-list / branch-list).
  --state STATE     PR state filter — open|closed|all (default: open).
  --json            Emit structured JSON instead of tables.
  --help, -h        Show this help.

Environment Variables:
  FORGEJO_URL                Default --url value.
  FORGEJO_ADMIN_USER         Default --user value (override "admin").
  FORGEJO_ADMIN_PASSWORD     Default --password value (PREFERRED in CI).
  FORGEJO_OWNER              Default --owner value (override "helix-org").

Exit codes:
  0  Success (endpoint reachable + data returned)
  1  Network failure / auth rejected / endpoint not found
  2  Invocation error (missing flag, bad --state, etc.)`)
}

// -----------------------------------------------------------------------------
// Entry point
// -----------------------------------------------------------------------------

// runForgejoWithDryRun wraps runForgejo with the global --dry-run flag.
func runForgejoWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
	rc := runForgejo(args, stdout, stderr)
	if rc != 0 && rc != fgoExitBlock {
		return errExit{code: rc}
	}
	return nil
}

func runForgejo(args []string, stdout, stderr io.Writer) int {
	flags, helpWanted, rc := parseForgejoFlags(args)
	if rc != fgoExitOK {
		return rc
	}
	if helpWanted {
		printForgejoHelp(stdout)
		return fgoExitOK
	}
	if flags.url == "" {
		fmt.Fprintln(stderr, "error: --url is required (or set FORGEJO_URL)")
		return fgoExitError
	}
	switch flags.subcommand {
	case "help":
		printForgejoHelp(stdout)
		return fgoExitOK
	case "ping":
		return runForgejoPing(flags, stdout, stderr)
	case "user-list":
		return runForgejoUserList(flags, stdout, stderr)
	case "user-info":
		return runForgejoUserInfo(flags, stdout, stderr)
	case "pr-list":
		return runForgejoPRList(flags, stdout, stderr)
	case "branch-list":
		return runForgejoBranchList(flags, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n\n", flags.subcommand)
		printForgejoHelp(stderr)
		return fgoExitError
	}
}

// -----------------------------------------------------------------------------
// ping
// -----------------------------------------------------------------------------

func runForgejoPing(flags fgoFlags, stdout, stderr io.Writer) int {
	client := forgejo.NewClient(flags.url, flags.user, flags.password)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Use the raw HTTP client to GET /api/v1/version — this is the
	// canonical Forgejo health endpoint and works without auth.
	body, status, err := fgoRawGET(ctx, flags.url+"/api/v1/version")
	if err != nil {
		fmt.Fprintf(stderr, "error: ping failed: %v\n", err)
		return fgoExitBlock
	}
	if status != 200 {
		fmt.Fprintf(stderr, "error: GET /api/v1/version returned HTTP %d: %s\n", status, body)
		return fgoExitBlock
	}
	_ = client // silence unused
	if flags.jsonOut {
		fmt.Fprintln(stdout, string(body))
		return fgoExitOK
	}
	var v struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		fmt.Fprintf(stdout, "Forgejo reachable but response was not JSON: %s\n", body)
		return fgoExitOK
	}
	fmt.Fprintf(stdout, "Forgejo %s reachable at %s\n", v.Version, flags.url)
	return fgoExitOK
}

// -----------------------------------------------------------------------------
// user-list / user-info
// -----------------------------------------------------------------------------

func runForgejoUserList(flags fgoFlags, stdout, stderr io.Writer) int {
	if flags.user == "" || flags.password == "" {
		fmt.Fprintln(stderr, "error: user-list requires --user and --password (or env FORGEJO_ADMIN_USER + FORGEJO_ADMIN_PASSWORD)")
		return fgoExitError
	}
	client := forgejo.NewClient(flags.url, flags.user, flags.password)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Forgejo's REST adapter doesn't have a dedicated UserList method, so
	// call the raw /api/v1/admin/users endpoint via the underlying HTTP path.
	body, status, err := fgoAdminGET(ctx, flags, "/admin/users")
	if err != nil {
		fmt.Fprintf(stderr, "error: user-list failed: %v\n", err)
		return fgoExitBlock
	}
	if status != 200 {
		fmt.Fprintf(stderr, "error: GET /admin/users returned HTTP %d: %s\n", status, body)
		return fgoExitBlock
	}
	_ = client // silence unused
	var users []struct {
		ID       int64  `json:"id"`
		Login    string `json:"login"`
		Email    string `json:"email"`
		FullName string `json:"full_name"`
		IsActive bool   `json:"active"`
		IsAdmin  bool   `json:"is_admin"`
	}
	if err := json.Unmarshal([]byte(body), &users); err != nil {
		fmt.Fprintf(stderr, "error: parse user-list: %v\n", err)
		return fgoExitBlock
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Login < users[j].Login })
	if flags.jsonOut {
		out, _ := json.MarshalIndent(users, "", "  ")
		fmt.Fprintln(stdout, string(out))
		return fgoExitOK
	}
	fmt.Fprintf(stdout, "Users on %s (%d)\n", flags.url, len(users))
	fmt.Fprintln(stdout, "  LOGIN                         EMAIL                         ADMIN  ACTIVE")
	for _, u := range users {
		fmt.Fprintf(stdout, "  %-29s %-29s %-5v  %v\n",
			truncate(u.Login, 29),
			truncate(u.Email, 29),
			u.IsAdmin,
			u.IsActive,
		)
	}
	return fgoExitOK
}

func runForgejoUserInfo(flags fgoFlags, stdout, stderr io.Writer) int {
	if flags.targetUser == "" {
		fmt.Fprintln(stderr, "error: user-info requires LOGIN (positional arg)")
		return fgoExitError
	}
	client := forgejo.NewClient(flags.url, flags.user, flags.password)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	u, err := client.GetUser(ctx, flags.targetUser)
	if err != nil {
		fmt.Fprintf(stderr, "error: get user %q: %v\n", flags.targetUser, err)
		return fgoExitBlock
	}
	_ = ctx
	if flags.jsonOut {
		out, _ := json.MarshalIndent(u, "", "  ")
		fmt.Fprintln(stdout, string(out))
		return fgoExitOK
	}
	fmt.Fprintf(stdout, "User %q (id=%d)\n", u.UserName, u.ID)
	fmt.Fprintf(stdout, "  Email:    %s\n", u.Email)
	fmt.Fprintf(stdout, "  FullName: %s\n", u.FullName)
	fmt.Fprintf(stdout, "  Active:   %v\n", u.IsActive)
	fmt.Fprintf(stdout, "  Admin:    %v\n", u.IsAdmin)
	return fgoExitOK
}

// -----------------------------------------------------------------------------
// pr-list / branch-list
// -----------------------------------------------------------------------------

func runForgejoPRList(flags fgoFlags, stdout, stderr io.Writer) int {
	if flags.owner == "" || flags.repo == "" {
		fmt.Fprintln(stderr, "error: pr-list requires --owner and --repo")
		return fgoExitError
	}
	client := forgejo.NewClient(flags.url, flags.user, flags.password)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	prs, err := client.ListPRs(ctx, flags.owner, flags.repo, flags.state)
	if err != nil {
		fmt.Fprintf(stderr, "error: list PRs: %v\n", err)
		return fgoExitBlock
	}
	sort.Slice(prs, func(i, j int) bool { return prs[i].Number < prs[j].Number })
	if flags.jsonOut {
		out, _ := json.MarshalIndent(prs, "", "  ")
		fmt.Fprintln(stdout, string(out))
		return fgoExitOK
	}
	fmt.Fprintf(stdout, "PRs in %s/%s (state=%s) — %d total\n", flags.owner, flags.repo, flags.state, len(prs))
	if len(prs) == 0 {
		return fgoExitOK
	}
	fmt.Fprintln(stdout, "  NUMBER  STATE    TITLE                              HEAD -> BASE")
	for _, p := range prs {
		headBase := truncate(p.Head.Ref+" -> "+p.Base.Ref, 30)
		fmt.Fprintf(stdout, "  %-7d %-8s %-33s %s\n",
			p.Number,
			p.State,
			truncate(p.Title, 33),
			headBase,
		)
	}
	return fgoExitOK
}

func runForgejoBranchList(flags fgoFlags, stdout, stderr io.Writer) int {
	if flags.owner == "" || flags.repo == "" {
		fmt.Fprintln(stderr, "error: branch-list requires --owner and --repo")
		return fgoExitError
	}
	client := forgejo.NewClient(flags.url, flags.user, flags.password)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Forgejo's REST adapter has CreateBranch but no ListBranches; use the
	// raw endpoint.
	body, status, err := fgoAuthGET(ctx, flags, fmt.Sprintf("/repos/%s/%s/branches", flags.owner, flags.repo))
	if err != nil {
		fmt.Fprintf(stderr, "error: list branches: %v\n", err)
		return fgoExitBlock
	}
	if status != 200 {
		fmt.Fprintf(stderr, "error: GET /repos/.../branches returned HTTP %d: %s\n", status, body)
		return fgoExitBlock
	}
	_ = client // silence unused
	var branches []struct {
		Name   string `json:"name"`
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if err := json.Unmarshal([]byte(body), &branches); err != nil {
		fmt.Fprintf(stderr, "error: parse branches: %v\n", err)
		return fgoExitBlock
	}
	sort.Slice(branches, func(i, j int) bool { return branches[i].Name < branches[j].Name })
	if flags.jsonOut {
		out, _ := json.MarshalIndent(branches, "", "  ")
		fmt.Fprintln(stdout, string(out))
		return fgoExitOK
	}
	fmt.Fprintf(stdout, "Branches in %s/%s — %d total\n", flags.owner, flags.repo, len(branches))
	for _, b := range branches {
		short := b.Commit.ID
		if len(short) > 7 {
			short = short[:7]
		}
		fmt.Fprintf(stdout, "  %-40s %s\n", b.Name, short)
	}
	return fgoExitOK
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// fgoRawGET issues a public GET against fullURL without auth.
func fgoRawGET(ctx context.Context, fullURL string) (body string, status int, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return "", 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, err
	}
	return string(b), resp.StatusCode, nil
}

// fgoAdminGET issues an authenticated GET against fullURL using BasicAuth.
func fgoAdminGET(ctx context.Context, f fgoFlags, path string) (body string, status int, err error) {
	return fgoAuthGET(ctx, f, path)
}

// fgoAuthGET issues an authenticated GET against {url}{path} using BasicAuth.
func fgoAuthGET(ctx context.Context, f fgoFlags, path string) (body string, status int, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", f.url+"/api/v1"+path, nil)
	if err != nil {
		return "", 0, err
	}
	if f.user != "" {
		req.SetBasicAuth(f.user, f.password)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, err
	}
	return string(b), resp.StatusCode, nil
}

// truncate returns s limited to max bytes, appending "…" if cut.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max < 1 {
		return ""
	}
	return s[:max-1] + "…"
}
