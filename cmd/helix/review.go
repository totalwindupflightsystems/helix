// Command helix — review.go
//
// `helix review` is the operator surface for the pkg/review adversarial review
// pipeline. It exposes the BiasStripper (commit message de-biasing),
// FalsePositiveTracker (model rotation triggers), and EvidenceBundle
// (custody/signature) subsystems as cobra-style subcommands.
//
// Subcommands:
//
//	strip-bias    Strip evaluative language + confidence assertions from a
//	              commit message or text file. Reads from --input or stdin.
//	fp-stats      Print false-positive tracker stats — which models are
//	              flagged / removed, dismissal counts, FP rates.
//	fp-record     Record a false-positive dismissal for a model (mutates
//	              the in-process tracker; see "state" flag for persisted).
//	evidence      Sign / verify an evidence bundle JSON file. With --sign:
//	              append role+key flags and write signed bundle to --output.
//	              With --verify: read existing bundle and check signatures.
//	custody       Print a custody summary for an evidence bundle (events +
//	              tamper checks).
//	dashboard     Human change management view for a PR: blast radius, risk
//	              score, architectural fit (ADR lineage), trust context.
//	help          Show this help.
//
// Flags:
//
//	--input PATH       Read input from a file (commit message / bundle JSON).
//	--output PATH      Write output to a file (strip result / signed bundle).
//	--json             Structured JSON output instead of human-readable text.
//	--model NAME       Model name (fp-stats / fp-record subcommands).
//	--dismissals N    Increment dismissals by N (default 1) for fp-record.
//	--total-evals N    Total evaluations count for EvaluateFPRate.
//	--key-role ROLE    Signer role for evidence sign (reviewer|arbitrator).
//	--key-path PATH    Path to a 64-byte raw ed25519 private key.
//	--state PATH       Persist FPTracker across runs (JSON file).
//	--pr URL|N         PR identifier (dashboard / run).
//	--files LIST       Comma-separated changed files (dashboard).
//	--files-from PATH  File listing changed paths, one per line (dashboard).
//	--repo PATH        Repo root for import-graph blast radius (dashboard).
//	--agent NAME       Authoring agent ID (dashboard trust context).
//	--ledger PATH      Trust ledger JSONL path (dashboard).
//	--adr-dir PATH     Directory of ADR markdown files (dashboard).
//	--tier TIER        Trust tier override: provisional|observed|trusted|veteran.
//	--category CAT     Change category: contract|behavioral|resilience|cosmetic.
//	--incidents PATH   JSON array of related incidents for risk correlation.
//
// All subcommands are local-only — they never hit Forgejo, Chimera, or any
// networked service. The pipeline is exercised end-to-end through the CLI
// with file-based inputs so operators can dry-run de-biasing and bundle
// workflows before plugging them into a live PR.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
)

const (
	revExitOK    = 0
	revExitBlock = 1 // verify failed — signature mismatch
	revExitError = 2 // invocation / IO error
)

// -----------------------------------------------------------------------------
// Flags
// -----------------------------------------------------------------------------

type revFlags struct {
	subcommand  string // strip-bias, fp-stats, fp-record, evidence, custody, run, dashboard, dismiss, help
	inputPath   string // --input PATH
	outputPath  string // --output PATH
	jsonOut     bool   // --json
	modelID     string // --model NAME
	dismissals  int    // --dismissals N (default 1)
	totalEvals  int    // --total-evals N
	keyRole     string // --key-role ROLE
	keyPath     string // --key-path PATH
	statePath   string // --state PATH
	evidenceCmd string // sign | verify (positional within evidence subcommand)
	prURL       string // --pr URL (for review run / dashboard)
	filesCSV    string // --files a,b,c (dashboard)
	filesFrom   string // --files-from PATH (dashboard)
	repoRoot    string // --repo PATH (dashboard)
	agentID     string // --agent NAME (dashboard)
	ledgerPath  string // --ledger PATH (dashboard)
	adrDir      string // --adr-dir PATH (dashboard)
	tier        string // --tier TIER (dashboard)
	category    string // --category CAT (dashboard)
	incidents   string // --incidents PATH (dashboard JSON)
	status      bool   // --status (queue subcommand)
	// dismiss subcommand flags
	findingID       string // --finding-id
	dismissReason   string // --reason
	dismissNote     string // --note (dismiss)
	dismissHumanID  string // --human-id
	dismissPRNumber int    // --pr-number
}

func parseReviewFlags(args []string) (revFlags, bool, int) {
	var f revFlags
	f.dismissals = 1
	helpWanted := false

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			helpWanted = true
		case arg == "--json":
			f.jsonOut = true
		case arg == "--input":
			if i+1 >= len(args) {
				return f, false, revExitError
			}
			f.inputPath = args[i+1]
			i++
		case arg == "--output":
			if i+1 >= len(args) {
				return f, false, revExitError
			}
			f.outputPath = args[i+1]
			i++
		case arg == "--model":
			if i+1 >= len(args) {
				return f, false, revExitError
			}
			f.modelID = args[i+1]
			i++
		case arg == "--dismissals":
			if i+1 >= len(args) {
				return f, false, revExitError
			}
			n, err := parseIntInRange(args[i+1], 1, 1_000_000)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: invalid --dismissals: %v\n", err)
				return f, false, revExitError
			}
			f.dismissals = n
			i++
		case arg == "--total-evals":
			if i+1 >= len(args) {
				return f, false, revExitError
			}
			n, err := parseIntInRange(args[i+1], 0, 1_000_000_000)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: invalid --total-evals: %v\n", err)
				return f, false, revExitError
			}
			f.totalEvals = n
			i++
		case arg == "--key-role":
			if i+1 >= len(args) {
				return f, false, revExitError
			}
			f.keyRole = args[i+1]
			i++
		case arg == "--key-path":
			if i+1 >= len(args) {
				return f, false, revExitError
			}
			f.keyPath = args[i+1]
			i++
		case arg == "--state":
			if i+1 >= len(args) {
				return f, false, revExitError
			}
			f.statePath = args[i+1]
			i++
		case arg == "--pr":
			if i+1 >= len(args) {
				return f, false, revExitError
			}
			f.prURL = args[i+1]
			i++
		case arg == "--files":
			if i+1 >= len(args) {
				return f, false, revExitError
			}
			f.filesCSV = args[i+1]
			i++
		case arg == "--files-from":
			if i+1 >= len(args) {
				return f, false, revExitError
			}
			f.filesFrom = args[i+1]
			i++
		case arg == "--repo":
			if i+1 >= len(args) {
				return f, false, revExitError
			}
			f.repoRoot = args[i+1]
			i++
		case arg == "--agent":
			if i+1 >= len(args) {
				return f, false, revExitError
			}
			f.agentID = args[i+1]
			i++
		case arg == "--ledger":
			if i+1 >= len(args) {
				return f, false, revExitError
			}
			f.ledgerPath = args[i+1]
			i++
		case arg == "--adr-dir":
			if i+1 >= len(args) {
				return f, false, revExitError
			}
			f.adrDir = args[i+1]
			i++
		case arg == "--tier":
			if i+1 >= len(args) {
				return f, false, revExitError
			}
			f.tier = args[i+1]
			i++
		case arg == "--category":
			if i+1 >= len(args) {
				return f, false, revExitError
			}
			f.category = args[i+1]
			i++
		case arg == "--incidents":
			if i+1 >= len(args) {
				return f, false, revExitError
			}
			f.incidents = args[i+1]
			i++
		case arg == "--status":
			f.status = true
		case arg == "--finding-id":
			if i+1 >= len(args) {
				return f, false, revExitError
			}
			f.findingID = args[i+1]
			i++
		case arg == "--reason":
			if i+1 >= len(args) {
				return f, false, revExitError
			}
			f.dismissReason = args[i+1]
			i++
		case arg == "--note":
			if i+1 >= len(args) {
				return f, false, revExitError
			}
			f.dismissNote = args[i+1]
			i++
		case arg == "--human-id":
			if i+1 >= len(args) {
				return f, false, revExitError
			}
			f.dismissHumanID = args[i+1]
			i++
		case arg == "--pr-number":
			if i+1 >= len(args) {
				return f, false, revExitError
			}
			pr, err := strconv.Atoi(args[i+1])
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: --pr-number requires an integer, got %q\n", args[i+1])
				return f, false, revExitError
			}
			f.dismissPRNumber = pr
			i++
		case len(arg) > 2 && arg[0] == '-' && arg[1] == '-':
			fmt.Fprintf(os.Stderr, "error: unknown flag %q\n", arg)
			return f, false, revExitError
		default:
			if f.subcommand == "" {
				f.subcommand = arg
			} else if f.subcommand == "evidence" && f.evidenceCmd == "" {
				// Position argument inside the evidence subcommand: sign|verify
				switch arg {
				case "sign", "verify":
					f.evidenceCmd = arg
				default:
					fmt.Fprintf(os.Stderr, "error: unknown evidence subcommand %q\n", arg)
					return f, false, revExitError
				}
			} else {
				fmt.Fprintf(os.Stderr, "error: unexpected positional arg %q\n", arg)
				return f, false, revExitError
			}
		}
		i++
	}

	if f.subcommand == "" {
		f.subcommand = "help"
	}
	return f, helpWanted, revExitOK
}

func printReviewHelp(w io.Writer) {
	fmt.Fprintln(w, `helix review — adversarial review pipeline operator CLI

Usage:
  helix review strip-bias  --input PATH [--output PATH] [--json]
  helix review fp-stats    [--state PATH]                            [--json]
  helix review fp-record   --model NAME [--dismissals N] [--state PATH] [--json]
  helix review evidence sign   --input PATH --key-role ROLE --key-path PATH [--output PATH]
  helix review evidence verify --input PATH --key-role ROLE --key-path PATH [--json]
  helix review custody     --input PATH                              [--json]
  helix review run         --pr URL                                  [--json]
  helix review dashboard   --pr N --files a,b [--repo PATH] [--agent NAME]
                           [--ledger PATH] [--adr-dir PATH] [--tier TIER]
                           [--category CAT] [--incidents PATH] [--json]
  helix review queue       --status
  helix review assign      --pr URL
  helix review dismiss     --finding-id ID --reason REASON [--note TEXT]
                           [--human-id ID] [--agent NAME] [--pr-number N] [--state PATH] [--json]
  helix review help

Subcommands:
  strip-bias    Strip evaluative language + confidence assertions from a commit
                message or text file. Reads --input (or stdin if --input is "-").
  fp-stats      Print false-positive tracker statistics — flagged/removed models
                with dismissal counts and FP rates.
  fp-record     Record a false-positive dismissal for a model, optionally
                persisting the tracker to --state.
  evidence      Sign or verify an EvidenceBundle JSON file using ed25519 keys.
  custody       Print a custody summary for an evidence bundle.
  run           Run multi-model adversarial review against a PR (--pr URL).
  dashboard     Human change management view: blast radius, risk score,
                architectural fit (ADR lineage), trust context. Terminal + JSON.
  queue         Show review queue status: pending/in-progress items sorted
                by priority (risk × staleness). Use --status flag.
  assign        Trigger automatic reviewer assignment for a PR (--pr URL).
                Prints assigned models, human-needed decision, panel size.
  dismiss       Record a structured dismissal of an agent review finding.
                Feeds the false positive tracker and tracks human override weight.
  help          Show this help.

Flags:
  --input PATH         Input file (use "-" for stdin on strip-bias).
  --output PATH        Output file (default: stdout on strip-bias; required on
                       evidence sign if not piping).
  --model NAME         Model name for fp-stats / fp-record.
  --dismissals N       Increment count for fp-record (default: 1, range: 1..1e6).
  --total-evals N      Total evaluations for FP rate computation.
  --key-role ROLE      Signer / verifier role (e.g. reviewer, arbitrator).
  --key-path PATH      Path to a 64-byte raw ed25519 private key (binary file).
  --state PATH         Persist / load FPTracker state as JSON.
  --pr URL|N           PR URL or number (run / dashboard / assign).
  --files LIST         Comma-separated changed file paths (dashboard).
  --files-from PATH    File with one changed path per line (dashboard).
  --repo PATH          Repo root for import-graph blast radius (dashboard).
  --agent NAME         Authoring agent ID (dashboard trust context).
  --ledger PATH        Trust ledger JSONL (dashboard).
  --adr-dir PATH       ADR markdown directory (dashboard).
  --tier TIER          Trust tier override (provisional|observed|trusted|veteran).
  --category CAT       Change category (contract|behavioral|resilience|cosmetic).
  --incidents PATH     JSON array of related incidents for risk correlation.
  --status             Show queue status (queue subcommand).
  --json               Structured JSON output.
  --help, -h           Show this help.

Exit codes:
  0  Success (strip / stats / sign completed; verify passed)
  1  Verify failed (signature mismatch) / model flagged / removed
  2  Invocation / IO error`)
}

// -----------------------------------------------------------------------------
// Entry point
// -----------------------------------------------------------------------------

// runReviewWithDryRun wraps runReview with the global --dry-run flag.
func runReviewWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
	if len(args) > 0 && args[0] == "models" {
		rc := runModelsWithDryRun(args, stdout, stderr, globalDryRun)
		if rc != 0 && rc != revExitBlock {
			return errExit{code: rc}
		}
		return nil
	}
	rc := runReview(args, stdout, stderr)
	if rc != 0 && rc != revExitBlock {
		return errExit{code: rc}
	}
	return nil
}

func runReview(args []string, stdout, stderr io.Writer) int {
	flags, helpWanted, rc := parseReviewFlags(args)
	if rc != revExitOK {
		return rc
	}
	if helpWanted {
		printReviewHelp(stdout)
		return revExitOK
	}

	switch flags.subcommand {
	case "help":
		printReviewHelp(stdout)
		return revExitOK
	case "strip-bias":
		return runReviewStripBias(flags, stdout, stderr)
	case "fp-stats":
		return runReviewFPStats(flags, stdout, stderr)
	case "fp-record":
		return runReviewFPRecord(flags, stdout, stderr)
	case "evidence":
		return runReviewEvidence(flags, stdout, stderr)
	case "custody":
		return runReviewCustody(flags, stdout, stderr)
	case "run":
		return runReviewRun(flags, stdout, stderr)
	case "dashboard":
		return runReviewDashboard(flags, stdout, stderr)
	case "queue":
		return runReviewQueue(flags, stdout, stderr)
	case "assign":
		return runReviewAssign(flags, stdout, stderr)
	case "dismiss":
		return runReviewDismiss(flags, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n\n", flags.subcommand)
		printReviewHelp(stderr)
		return revExitError
	}
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// readReviewInput reads from --input path, or stdin if inputPath is "-" or "".
func readReviewInput(path string, stderr io.Writer) (string, error) {
	if path == "" || path == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(b), nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(b), nil
}

// readInputFile reads bytes from a path.
func readInputFile(path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("empty input path")
	}
	return os.ReadFile(path)
}

// writeReviewOutput writes body to path or stdout if path is empty.
func writeReviewOutput(path, body string, stdout io.Writer) {
	if path == "" {
		fmt.Fprint(stdout, body)
		return
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "warning: write %s failed: %v\n", path, err)
	}
}
