// Command helix — dispatch.go
//
// `helix dispatch` is the top-level orchestration entry point. It parses a
// specification markdown, plans a work item for the named agent, and runs
// the Ralph Loop via pkg/dispatcher.ForgejoLoop: acquire lock → create
// worktree → execute steps → commit → open branch in Forgejo → open PR →
// release lock. Returns a structured DispatchOutcome as JSON.
//
// Modes:
//
//	--dry-run   Plan only. Never touches Forgejo. Prints the would-be
//	            branch name and a compare URL for visual verification.
//	(live)      Connects to Forgejo, opens the branch + PR, prints the
//	            PR HTML URL.
//
// Required flags:
//
//	--spec          Path to a spec markdown file (decomposed into tasks)
//	--agent         Agent name to attribute the work item to
//	--repo          target Forgejo repo (e.g. "helix")
//
// Common flags:
//
//	--forgejo-url     (default http://localhost:3030)
//	--admin-user      (default admin; reads FORGEJO_ADMIN_USER)
//	--admin-password  (reads FORGEJO_ADMIN_PASSWORD — prefer env var)
//	--owner           (default "helix-org"; the GitHub-style owner ns)
//	--base-branch     (default "main")
//	--workdir         (default current dir; controls lock + worktree path)
//	--verbose         enable verbose stderr logging
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	disp "github.com/totalwindupflightsystems/helix/pkg/dispatcher"
	"github.com/totalwindupflightsystems/helix/pkg/forgejo"
)

// envVar list for the dispatch flags.
const (
	envDispatchSpec       = "HELIX_DISPATCH_SPEC"
	envDispatchAgent      = "HELIX_DISPATCH_AGENT"
	envDispatchRepo       = "HELIX_DISPATCH_REPO"
	envDispatchOwner      = "HELIX_DISPATCH_OWNER"
	envDispatchForgejoURL = "FORGEJO_URL"
	envDispatchAdminUser  = "FORGEJO_ADMIN_USER"
	envDispatchAdminPass  = "FORGEJO_ADMIN_PASSWORD"
	envDispatchBaseBranch = "HELIX_DISPATCH_BASE_BRANCH"
	envDispatchWorkDir    = "HELIX_DISPATCH_WORKDIR"
)

// dispatchFlags captures every CLI flag in one struct so the Run function
// can stay a pure reader + caller.
type dispatchFlags struct {
	spec          string
	agent         string
	repo          string
	owner         string
	forgejoURL    string
	adminUser     string
	adminPassword string
	baseBranch    string
	workDir       string
	dryRun        bool
	verbose       bool
}

// parseDispatchFlags builds a flag.FS rooted at a fake arg-set, parses the
// args (which may be empty to print help), and returns a populated
// dispatchFlags. The flag.FS is bound to a usage func that prints the
// dispatch subcommand's help text.
//
// Returns the flags, a "showHelp" bool, and an error. showHelp=true means
// the user passed --help or no positional args — the caller should print
// the help and exit 0.
//
// stdout/stderr are the writers used when --help is detected; flag.Parse
// itself writes help to its configured Output (we set it to stderr so any
// flag-parse errors from wrong flag types are visible — the cli parses
// fixed-position flags so this rarely happens).
func parseDispatchFlags(args []string, stdout, stderr io.Writer) (dispatchFlags, bool, error) {
	fs := flag.NewFlagSet("helix-dispatch", flag.ContinueOnError)
	fs.SetOutput(stderr) // surface flag-library errors to stderr

	var f dispatchFlags

	fs.StringVar(&f.spec, "spec", envOr(envDispatchSpec, ""),
		"Path to spec markdown file (env HELIX_DISPATCH_SPEC)")
	fs.StringVar(&f.agent, "agent", envOr(envDispatchAgent, ""),
		"Agent name to attribute this work item to (env HELIX_DISPATCH_AGENT)")
	fs.StringVar(&f.repo, "repo", envOr(envDispatchRepo, ""),
		"Target Forgejo repo (env HELIX_DISPATCH_REPO)")
	fs.StringVar(&f.owner, "owner", envOr(envDispatchOwner, "helix-org"),
		"Owner namespace (org or user) in Forgejo (env HELIX_DISPATCH_OWNER)")
	fs.StringVar(&f.forgejoURL, "forgejo-url", envOr(envDispatchForgejoURL, "http://localhost:3030"),
		"Forgejo base URL (env FORGEJO_URL)")
	fs.StringVar(&f.adminUser, "admin-user", envOr(envDispatchAdminUser, "admin"),
		"Forgejo admin username (env FORGEJO_ADMIN_USER)")
	fs.StringVar(&f.adminPassword, "admin-password", envOr(envDispatchAdminPass, ""),
		"Forgejo admin password (env FORGEJO_ADMIN_PASSWORD — preferred)")
	fs.StringVar(&f.baseBranch, "base-branch", envOr(envDispatchBaseBranch, "main"),
		"Base branch to target with the PR (env HELIX_DISPATCH_BASE_BRANCH)")
	fs.StringVar(&f.workDir, "workdir", envOr(envDispatchWorkDir, ""),
		"Working directory for lock + worktree (env HELIX_DISPATCH_WORKDIR; default cwd)")
	fs.BoolVar(&f.dryRun, "dry-run", false,
		"Plan only — never touch the network; prints a placeholder URL")
	fs.BoolVar(&f.verbose, "verbose", false, "Verbose stderr logging")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printDispatchUsage(stdout)
			return f, true, nil
		}
		return f, false, err
	}

	// Positional-args after flags: none accepted.
	if rest := fs.Args(); len(rest) > 0 {
		return f, false, fmt.Errorf("unexpected positional arguments: %v", rest)
	}

	return f, false, nil
}

// runDispatchWithIO wraps runDispatch and returns an error for non-zero
// exit codes. main.go's dispatcher route uses this; tests use runDispatch
// directly with captured I/O.
func runDispatchWithIO(args []string, stdout, stderr io.Writer) error {
	rc := runDispatch(args, stdout, stderr)
	if rc != 0 {
		return errExit{code: rc}
	}
	return nil
}

// errExit lets us return a non-zero exit code from runDispatchWithIO
// without using os.Exit directly.
type errExit struct{ code int }

func (e errExit) Error() string { return fmt.Sprintf("dispatch exit %d", e.code) }

// runDispatch is the cmd/helix dispatch subcommand entry point.
//
// Flow:
//  1. Parse flags + env.
//  2. Build a ForgejoLoop with either a real *forgejo.Client (live) or
//     nil (dry-run).
//  3. Run the loop with a 60s timeout (the Forgejo API is fast — any
//     call that takes longer is a sign of network problems we want to
//     surface, not paper over).
//  4. Marshal the DispatchOutcome to JSON and write to stdout.
//  5. Exit non-zero on error.
//
// Exposed for unit tests in dispatch_test.go.
func runDispatch(args []string, stdout, stderr io.Writer) int {
	flags, showHelp, err := parseDispatchFlags(args, stdout, stderr)
	if showHelp {
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, "helix dispatch: parse:", err)
		printDispatchUsage(stderr)
		return 2
	}

	if flags.spec == "" {
		fmt.Fprintln(stderr, "helix dispatch: --spec is required")
		printDispatchUsage(stderr)
		return 2
	}
	if flags.agent == "" {
		fmt.Fprintln(stderr, "helix dispatch: --agent is required")
		printDispatchUsage(stderr)
		return 2
	}
	if !flags.dryRun && flags.repo == "" {
		fmt.Fprintln(stderr, "helix dispatch: --repo is required in live mode (or pass --dry-run)")
		printDispatchUsage(stderr)
		return 2
	}

	if flags.verbose {
		fmt.Fprintf(stderr, "[dispatch] spec=%s agent=%s repo=%s dry-run=%v\n",
			flags.spec, flags.agent, flags.repo, flags.dryRun)
	}

	fl := &disp.ForgejoLoop{
		Owner:      flags.owner,
		Repo:       flags.repo,
		BaseBranch: flags.baseBranch,
		Agent:      disp.AgentProfile{Name: flags.agent, Capability: "go", MaxLoad: 1},
		DryRun:     flags.dryRun,
		WorkDir:    flags.workDir,
		ForgejoURL: flags.forgejoURL,
	}

	// Live mode requires a real client.
	if !flags.dryRun {
		if flags.adminPassword == "" {
			fmt.Fprintln(stderr, "helix dispatch: --admin-password (or FORGEJO_ADMIN_PASSWORD) is required in live mode")
			return 2
		}
		fl.Client = forgejo.NewClient(flags.forgejoURL, flags.adminUser, flags.adminPassword)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	outcome, err := fl.Run(ctx, flags.spec, flags.agent)
	if err != nil {
		// Partial outcome — log the structured fields so the user can
		// see what was reached before the failure.
		if outcome != nil {
			if data, mErr := json.Marshal(outcome); mErr == nil {
				fmt.Fprintln(stdout, string(data))
			}
		}
		fmt.Fprintln(stderr, "helix dispatch: error:", err)
		return 1
	}

	data, err := outcome.Marshal()
	if err != nil {
		fmt.Fprintln(stderr, "helix dispatch: marshal outcome:", err)
		return 1
	}
	fmt.Fprintln(stdout, string(data))
	return 0
}

// printDispatchUsage renders the dispatch subcommand's help text.
func printDispatchUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage: helix dispatch [flags]

Drive a spec -> agent -> Forgejo branch/PR workflow (Ralph Loop).

Required flags:
  --spec <path>      Spec markdown file to decompose into a work item
  --agent <name>     Agent name to attribute the work item to
  --repo <name>      Target Forgejo repository (omit for --dry-run)

Common flags:
  --owner <ns>           Owner namespace (default: helix-org)
  --base-branch <name>   Target base branch (default: main)
  --forgejo-url <url>    Forgejo base URL (default: http://localhost:3030)
  --admin-user <name>    Forgejo admin user (default: admin)
  --admin-password <pw>  Forgejo admin password — prefer env FORGEJO_ADMIN_PASSWORD
  --workdir <path>       Working directory (default: cwd)
  --dry-run              Plan only — never touch the network
  --verbose              Verbose stderr logging

Environment:
  HELIX_DISPATCH_SPEC, HELIX_DISPATCH_AGENT, HELIX_DISPATCH_REPO,
  HELIX_DISPATCH_OWNER, HELIX_DISPATCH_BASE_BRANCH, HELIX_DISPATCH_WORKDIR,
  FORGEJO_URL, FORGEJO_ADMIN_USER, FORGEJO_ADMIN_PASSWORD

Exit codes:
  0  dispatch succeeded (dry-run or live)
  1  dispatch failed during execution
  2  invalid arguments / missing required flags

Examples:
  helix dispatch --spec specs/agent-identity.md --agent test-agent --dry-run
  helix dispatch --spec specs/agent-identity.md --agent test-agent \\
                --repo helix --admin-password "$FORGEJO_ADMIN_PASSWORD"`)
}

// envOr returns the value of the named env var, or fallback if unset/empty.
func envOr(name, fallback string) string {
	if v, ok := os.LookupEnv(name); ok && v != "" {
		return v
	}
	return fallback
}
