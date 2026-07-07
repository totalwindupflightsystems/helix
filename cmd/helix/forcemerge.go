package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/forcemerge"
)

// ============================================================================
// helix forcemerge CLI — force-merge audit operations
// (spec SPECIFICATION.md §5.4 + §6.6)
//
// `force-merge` is the operator override that lets a human merge a PR
// without the co-approval gate. Every use MUST be recorded in the audit
// log, and a Conscientiousness review verdict MUST follow.
//
// This CLI exposes:
//   - record  — append a new audit entry to the JSONL log
//   - review  — append a Conscientiousness post-merge review verdict
//   - report  — build the monthly review report from the JSONL log
//   - path    — print the canonical audit log path (for cron wiring)
//   - help    — usage
// ============================================================================

const (
	fmExitOK    = 0
	fmExitFail  = 1 // validation or write failure
	fmExitError = 2 // invocation error
)

// fmFlags holds parsed CLI flags.
type fmFlags struct {
	subcommand string // record, review, report, path, help
	auditPath  string
	prURL      string
	mergeSHA   string
	human      string
	just       string
	reviewer   string
	verdict    string // PASSED / FAILED / PENDING
	reason     string
	confidence int
	month      string // YYYY-MM for report filter
	jsonOut    bool
}

// parseForceMergeFlags parses args for `helix forcemerge`.
func parseForceMergeFlags(args []string) (fmFlags, bool, int) {
	var f fmFlags
	f.auditPath = forcemerge.DefaultAuditPath
	helpWanted := false

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			helpWanted = true
		case arg == "--json":
			f.jsonOut = true
		case arg == "--path":
			if i+1 < len(args) {
				f.auditPath = args[i+1]
				i++
			} else {
				return f, false, fmExitError
			}
		case arg == "--pr-url":
			if i+1 < len(args) {
				f.prURL = args[i+1]
				i++
			} else {
				return f, false, fmExitError
			}
		case arg == "--merge-sha":
			if i+1 < len(args) {
				f.mergeSHA = args[i+1]
				i++
			} else {
				return f, false, fmExitError
			}
		case arg == "--human":
			if i+1 < len(args) {
				f.human = args[i+1]
				i++
			} else {
				return f, false, fmExitError
			}
		case arg == "--justification":
			if i+1 < len(args) {
				f.just = args[i+1]
				i++
			} else {
				return f, false, fmExitError
			}
		case arg == "--reviewer":
			if i+1 < len(args) {
				f.reviewer = args[i+1]
				i++
			} else {
				return f, false, fmExitError
			}
		case arg == "--verdict":
			if i+1 < len(args) {
				f.verdict = args[i+1]
				i++
			} else {
				return f, false, fmExitError
			}
		case arg == "--reason":
			if i+1 < len(args) {
				f.reason = args[i+1]
				i++
			} else {
				return f, false, fmExitError
			}
		case arg == "--confidence":
			if i+1 < len(args) {
				if _, err := fmt.Sscanf(args[i+1], "%d", &f.confidence); err != nil {
					return f, false, fmExitError
				}
				i++
			} else {
				return f, false, fmExitError
			}
		case arg == "--month":
			if i+1 < len(args) {
				f.month = args[i+1]
				i++
			} else {
				return f, false, fmExitError
			}
		case strings.HasPrefix(arg, "--"):
			return f, false, fmExitError
		default:
			if f.subcommand == "" {
				f.subcommand = arg
			}
		}
		i++
	}

	if f.subcommand == "" {
		f.subcommand = "help"
	}

	return f, helpWanted, fmExitOK
}

// printForceMergeHelp prints the help text.
func printForceMergeHelp(w io.Writer) {
	fmt.Fprintln(w, `helix forcemerge — force-merge audit operations

Records and inspects every Helix PR merge that used the force-merge override
(spec SPECIFICATION.md §5.4 + §6.6).

Usage:
  helix forcemerge record --pr-url <URL> --merge-sha <SHA> \
                          --human <id> --justification <text>
  helix forcemerge review --pr-url <URL> --merge-sha <SHA> \
                          --reviewer <model> --verdict <PASSED|FAILED|PENDING> \
                          --reason <text> [--confidence <0-100>]
  helix forcemerge report [--month YYYY-MM] [--json]
  helix forcemerge path
  helix forcemerge help

Subcommands:
  record   Append a new force-merge audit entry. Justification MUST be
           ≥20 chars (spec §5.4 spirit — "Use sparingly").
  review   Append a Conscientiousness post-merge verdict for a prior merge.
  report   Build the monthly review report (spec §6.6) from the JSONL log.
  path     Print the canonical audit log path (defaults to
           ~/.helix/forcemerge-audit.jsonl). Useful for cron wiring.
  help     Show this help.

Flags:
  --path <file>         Override the audit log path (default ~/.helix/forcemerge-audit.jsonl)
  --pr-url <url>        Canonical Forgejo PR URL
  --merge-sha <sha>     Commit SHA that landed on main
  --human <id>          Human identity who applied force-merge label
  --justification <t>   Free-text explanation (≥20 chars, ≤2000)
  --reviewer <model>    Conscientiousness model identifier
  --verdict <status>    PASSED | FAILED | PENDING
  --reason <text>       Free-text review explanation
  --confidence <n>      Model confidence 0-100 (default 0)
  --month YYYY-MM       Filter report to a single month (default: all months)
  --json                Structured JSON output (report subcommand)
  --help, -h            Show this help

Exit codes:
  0  Success
  1  Validation error or write failure
  2  Invocation error (missing/invalid flags)`)
}

// runForceMerge is the entry point for `helix forcemerge`.
func runForceMerge(args []string, stdout, stderr io.Writer) int {
	flags, helpWanted, rc := parseForceMergeFlags(args)
	if rc != fmExitOK {
		return rc
	}
	if helpWanted {
		printForceMergeHelp(stdout)
		return fmExitOK
	}

	switch flags.subcommand {
	case "help":
		printForceMergeHelp(stdout)
		return fmExitOK
	case "record":
		return runFMRecord(flags, stdout, stderr)
	case "review":
		return runFMReview(flags, stdout, stderr)
	case "report":
		return runFMReport(flags, stdout, stderr)
	case "path":
		expanded, err := forcemerge.ExpandPath(flags.auditPath)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return fmExitFail
		}
		fmt.Fprintln(stdout, expanded)
		return fmExitOK
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n", flags.subcommand)
		return fmExitError
	}
}

// openFMStore opens the audit store at flags.auditPath. Returns the
// store, its expanded path, and an exit code suitable for the caller.
func openFMStore(flags fmFlags, stderr io.Writer) (*forcemerge.AuditStore, string, int) {
	store, err := forcemerge.NewFileStore(flags.auditPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: open audit log: %v\n", err)
		return nil, "", fmExitFail
	}
	return store, store.Path(), fmExitOK
}

// runFMRecord appends a new audit entry.
func runFMRecord(flags fmFlags, stdout, stderr io.Writer) int {
	if flags.prURL == "" || flags.mergeSHA == "" || flags.human == "" || flags.just == "" {
		fmt.Fprintln(stderr, "error: --pr-url, --merge-sha, --human, --justification are required")
		return fmExitError
	}

	entry := forcemerge.AuditEntry{
		PRURL:         flags.prURL,
		HumanIdentity: flags.human,
		Justification: flags.just,
		MergeSHA:      flags.mergeSHA,
		Timestamp:     time.Now().UTC().Format(time.RFC3339Nano),
	}

	store, path, rc := openFMStore(flags, stderr)
	if rc != fmExitOK {
		return rc
	}
	defer func() { _ = store.Close() }()

	if err := store.RecordAudit(entry); err != nil {
		fmt.Fprintf(stderr, "error: record audit: %v\n", err)
		return fmExitFail
	}

	fmt.Fprintf(stdout, "recorded force-merge: pr=%s sha=%s human=%s path=%s\n",
		flags.prURL, flags.mergeSHA, flags.human, path)
	return fmExitOK
}

// runFMReview appends a Conscientiousness post-merge verdict.
func runFMReview(flags fmFlags, stdout, stderr io.Writer) int {
	if flags.prURL == "" || flags.mergeSHA == "" || flags.reviewer == "" || flags.verdict == "" {
		fmt.Fprintln(stderr, "error: --pr-url, --merge-sha, --reviewer, --verdict are required")
		return fmExitError
	}

	status := forcemerge.ReviewStatus(strings.ToUpper(flags.verdict))
	if !status.IsValid() {
		fmt.Fprintf(stderr, "error: invalid --verdict %q (must be PASSED, FAILED, or PENDING)\n", flags.verdict)
		return fmExitError
	}

	entry := forcemerge.ReviewEntry{
		PRURL:      flags.prURL,
		MergeSHA:   flags.mergeSHA,
		Reviewer:   flags.reviewer,
		Status:     status,
		Reason:     flags.reason,
		Confidence: flags.confidence,
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
	}

	store, path, rc := openFMStore(flags, stderr)
	if rc != fmExitOK {
		return rc
	}
	defer func() { _ = store.Close() }()

	if err := store.RecordReview(entry); err != nil {
		fmt.Fprintf(stderr, "error: record review: %v\n", err)
		return fmExitFail
	}

	fmt.Fprintf(stdout, "recorded review: pr=%s sha=%s verdict=%s reviewer=%s path=%s\n",
		flags.prURL, flags.mergeSHA, status, flags.reviewer, path)
	return fmExitOK
}

// runFMReport builds the monthly review report.
func runFMReport(flags fmFlags, stdout, stderr io.Writer) int {
	expanded, err := forcemerge.ExpandPath(flags.auditPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return fmExitFail
	}

	f, err := os.Open(expanded)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(stderr, "error: audit log not found at %s\n", expanded)
			return fmExitFail
		}
		fmt.Fprintf(stderr, "error: open audit log: %v\n", err)
		return fmExitFail
	}
	defer func() { _ = f.Close() }()

	rep, err := forcemerge.BuildAuditReport(f, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(stderr, "error: build report: %v\n", err)
		return fmExitFail
	}

	if flags.month != "" {
		filtered := forcemerge.AuditReport{
			ByMonth:       map[string]forcemerge.MonthlyStats{},
			HumansByMonth: map[string][]forcemerge.HumanUsage{},
		}
		if ms, ok := rep.ByMonth[flags.month]; ok {
			filtered.ByMonth[flags.month] = ms
			if usages, ok := rep.HumansByMonth[flags.month]; ok {
				filtered.HumansByMonth[flags.month] = usages
			}
		}
		rep = filtered
	}

	if flags.jsonOut {
		out, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Fprintln(stdout, string(out))
	} else {
		fmt.Fprintln(stdout, forcemerge.FormatReport(rep))
	}

	// Non-zero exit if there are pending or failed reviews — these
	// are the items that need operator attention.
	if rep.PendingReviewCount > 0 || rep.FailedReviewCount > 0 {
		return fmExitFail
	}
	return fmExitOK
}

// runForceMergeWithDryRun wraps runForceMerge with the global --dry-run flag.
func runForceMergeWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
	rc := runForceMerge(args, stdout, stderr)
	if rc != 0 && rc != fmExitFail {
		return errExit{code: rc}
	}
	return nil
}
