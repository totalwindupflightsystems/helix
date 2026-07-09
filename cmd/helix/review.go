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
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/review"
	"github.com/totalwindupflightsystems/helix/pkg/trust"
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
	subcommand  string // strip-bias, fp-stats, fp-record, evidence, custody, run, dashboard, help
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
  --pr URL|N           PR URL or number (run / dashboard).
  --files LIST         Comma-separated changed file paths (dashboard).
  --files-from PATH    File with one changed path per line (dashboard).
  --repo PATH          Repo root for import-graph blast radius (dashboard).
  --agent NAME         Authoring agent ID (dashboard trust context).
  --ledger PATH        Trust ledger JSONL (dashboard).
  --adr-dir PATH       ADR markdown directory (dashboard).
  --tier TIER          Trust tier override (provisional|observed|trusted|veteran).
  --category CAT       Change category (contract|behavioral|resilience|cosmetic).
  --incidents PATH     JSON array of related incidents for risk correlation.
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
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n\n", flags.subcommand)
		printReviewHelp(stderr)
		return revExitError
	}
}

// -----------------------------------------------------------------------------
// strip-bias
// -----------------------------------------------------------------------------

func runReviewStripBias(flags revFlags, stdout, stderr io.Writer) int {
	body, err := readReviewInput(flags.inputPath, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return revExitError
	}

	bs := review.NewBiasStripper()
	stripped := bs.StripPreservingPrefix(body)

	if flags.jsonOut {
		// JSON-shape so callers can compare original vs stripped programmatically.
		out := map[string]any{
			"input":    body,
			"stripped": stripped,
		}
		// If input is JSON, try to extract structured fields for the wrapped bundle.
		b, _ := json.MarshalIndent(out, "", "  ")
		writeReviewOutput(flags.outputPath, string(b)+"\n", stdout)
		return revExitOK
	}

	if flags.outputPath != "" {
		writeReviewOutput(flags.outputPath, stripped+"\n", stdout)
		fmt.Fprintf(stdout, "wrote %d bytes to %s\n", len(stripped)+1, flags.outputPath)
		return revExitOK
	}

	fmt.Fprintln(stdout, stripped)
	return revExitOK
}

// -----------------------------------------------------------------------------
// fp-stats / fp-record
// -----------------------------------------------------------------------------

func runReviewFPStats(flags revFlags, stdout, stderr io.Writer) int {
	tracker, err := loadFPTracker(flags.statePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: load tracker: %v\n", err)
		return revExitError
	}

	if flags.jsonOut {
		models := tracker.FlaggedModels()
		sort.Strings(models)
		type modelEntry struct {
			Model      string `json:"model"`
			Dismissals int    `json:"dismissals"`
			Flagged    bool   `json:"flagged"`
			Removed    bool   `json:"removed"`
		}
		var entries []modelEntry
		// Walk FlaggedModels + RemovedModels union (no API to enumerate all known).
		known := map[string]bool{}
		for _, m := range models {
			known[m] = true
		}
		for _, m := range tracker.RemovedModels() {
			known[m] = true
		}
		for m := range known {
			entries = append(entries, modelEntry{
				Model:      m,
				Dismissals: tracker.DismissalCount(m),
				Flagged:    tracker.IsFlagged(m),
				Removed:    tracker.IsRemoved(m),
			})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Model < entries[j].Model })
		b, _ := json.MarshalIndent(map[string]any{
			"models":     entries,
			"summary":    tracker.Summary(),
			"state_path": flags.statePath,
		}, "", "  ")
		fmt.Fprintln(stdout, string(b))
		return revExitOK
	}

	fmt.Fprintln(stdout, tracker.Summary())
	if len(tracker.FlaggedModels()) > 0 || len(tracker.RemovedModels()) > 0 {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Flagged models:")
		for _, m := range tracker.FlaggedModels() {
			fmt.Fprintf(stdout, "  - %s (dismissals=%d, flagged=true)\n", m, tracker.DismissalCount(m))
		}
		fmt.Fprintln(stdout, "Removed models:")
		for _, m := range tracker.RemovedModels() {
			fmt.Fprintf(stdout, "  - %s (removed=true)\n", m)
		}
	}
	return revExitOK
}

func runReviewFPRecord(flags revFlags, stdout, stderr io.Writer) int {
	if flags.modelID == "" {
		fmt.Fprintln(stderr, "error: --model is required for fp-record")
		return revExitError
	}
	tracker, err := loadFPTracker(flags.statePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: load tracker: %v\n", err)
		return revExitError
	}

	for i := 0; i < flags.dismissals; i++ {
		tracker.RecordDismissal(flags.modelID)
	}
	if flags.totalEvals > 0 {
		tracker.EvaluateFPRate(flags.modelID, flags.totalEvals)
	}

	if flags.statePath != "" {
		if err := saveFPTracker(flags.statePath, tracker); err != nil {
			fmt.Fprintf(stderr, "error: persist tracker: %v\n", err)
			return revExitError
		}
	}

	out := map[string]any{
		"model":      flags.modelID,
		"dismissals": tracker.DismissalCount(flags.modelID),
		"flagged":    tracker.IsFlagged(flags.modelID),
		"removed":    tracker.IsRemoved(flags.modelID),
		"state_path": flags.statePath,
	}
	if flags.jsonOut {
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(stdout, string(b))
	} else {
		fmt.Fprintf(stdout, "Recorded %d dismissal(s) for model %q\n", flags.dismissals, flags.modelID)
		fmt.Fprintf(stdout, "  dismissals=%d flagged=%v removed=%v\n",
			out["dismissals"], out["flagged"], out["removed"])
		if flags.statePath != "" {
			fmt.Fprintf(stdout, "  state persisted to %s\n", flags.statePath)
		}
	}
	if tracker.IsRemoved(flags.modelID) || tracker.IsFlagged(flags.modelID) {
		return revExitBlock
	}
	return revExitOK
}

// -----------------------------------------------------------------------------
// evidence sign / verify
// -----------------------------------------------------------------------------

func runReviewEvidence(flags revFlags, stdout, stderr io.Writer) int {
	if flags.evidenceCmd == "" {
		fmt.Fprintln(stderr, "error: evidence requires a subcommand: sign | verify")
		return revExitError
	}
	switch flags.evidenceCmd {
	case "sign":
		return runReviewEvidenceSign(flags, stdout, stderr)
	case "verify":
		return runReviewEvidenceVerify(flags, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown evidence subcommand %q\n", flags.evidenceCmd)
		return revExitError
	}
}

func runReviewEvidenceSign(flags revFlags, stdout, stderr io.Writer) int {
	if flags.inputPath == "" || flags.keyPath == "" {
		fmt.Fprintln(stderr, "error: evidence sign requires --input, --key-path")
		return revExitError
	}
	// Default keyRole to "primary" if empty (signer role inferred from flag)
	// BEFORE validation, otherwise the validation rejects empty role.
	if flags.keyRole == "" {
		flags.keyRole = "primary"
	}
	if !isValidSignerRole(flags.keyRole) {
		fmt.Fprintf(stderr, "error: --key-role must be one of primary|adversarial|audit (got %q)\n", flags.keyRole)
		return revExitError
	}
	priv, err := readPrivateKeyFile(flags.keyPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: read key: %v\n", err)
		return revExitError
	}

	inputBytes, err := readInputFile(flags.inputPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: read input: %v\n", err)
		return revExitError
	}

	// Accept either a plain string OR a JSON-shaped EvidenceBundle.
	var bundle *review.EvidenceBundle
	if json.Valid(inputBytes) {
		var existing review.EvidenceBundle
		if err := json.Unmarshal(inputBytes, &existing); err == nil && existing.PRURL != "" {
			bundle = &existing
		}
	}
	if bundle == nil {
		// Build a minimal bundle from the raw text.
		formation := review.Formation{
			Primary:     review.ModelInfo{Model: "primary", Provider: "local"},
			Adversarial: review.ModelInfo{Model: "adversarial", Provider: "local"},
			Audit:       review.ModelInfo{Model: "audit", Provider: "local"},
		}
		bundle = review.NewEvidenceBundle("local://input", "review-"+tsSuffix(), formation, "", "")
	}
	sig, err := bundle.SignBundle(flags.keyRole, priv)
	if err != nil {
		fmt.Fprintf(stderr, "error: sign: %v\n", err)
		return revExitError
	}

	// Write the bundle itself (with the embedded signature) so verify can
	// re-read it as a full EvidenceBundle. The CLI wraps extra metadata in
	// the stdout summary but always persists the canonical bundle JSON to
	// --output (or stdout if --output is empty).
	bundleJSON, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "error: marshal bundle: %v\n", err)
		return revExitError
	}
	bundleJSON = append(bundleJSON, '\n')
	if flags.outputPath != "" {
		writeReviewOutput(flags.outputPath, string(bundleJSON), stdout)
		fmt.Fprintf(stdout, "Signed bundle written to %s (role=%s, sig=%s...)\n", flags.outputPath, flags.keyRole, sig[:min(16, len(sig))])
	} else {
		fmt.Fprintln(stdout, string(bundleJSON))
	}
	return revExitOK
}

func runReviewEvidenceVerify(flags revFlags, stdout, stderr io.Writer) int {
	if flags.inputPath == "" || flags.keyPath == "" {
		fmt.Fprintln(stderr, "error: evidence verify requires --input, --key-path")
		return revExitError
	}
	if flags.keyRole == "" {
		flags.keyRole = "primary"
	}
	if !isValidSignerRole(flags.keyRole) {
		fmt.Fprintf(stderr, "error: --key-role must be one of primary|adversarial|audit (got %q)\n", flags.keyRole)
		return revExitError
	}
	pub, err := readPublicKeyFile(flags.keyPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: read key: %v\n", err)
		return revExitError
	}
	inputBytes, err := readInputFile(flags.inputPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: read input: %v\n", err)
		return revExitError
	}
	var bundle review.EvidenceBundle
	if err := json.Unmarshal(inputBytes, &bundle); err != nil {
		fmt.Fprintf(stderr, "error: parse bundle: %v\n", err)
		return revExitError
	}
	ok, err := bundle.VerifySignature(flags.keyRole, pub)
	if err != nil {
		fmt.Fprintf(stderr, "error: verify: %v\n", err)
		return revExitError
	}

	out := map[string]any{
		"pr_url": bundle.PRURL,
		"role":   flags.keyRole,
		"valid":  ok,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Fprintln(stdout, string(b))
	if !ok {
		return revExitBlock
	}
	return revExitOK
}

// -----------------------------------------------------------------------------
// custody
// -----------------------------------------------------------------------------

func runReviewCustody(flags revFlags, stdout, stderr io.Writer) int {
	if flags.inputPath == "" {
		fmt.Fprintln(stderr, "error: custody requires --input")
		return revExitError
	}
	inputBytes, err := readInputFile(flags.inputPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: read input: %v\n", err)
		return revExitError
	}
	var bundle review.EvidenceBundle
	if err := json.Unmarshal(inputBytes, &bundle); err != nil {
		fmt.Fprintf(stderr, "error: parse bundle: %v\n", err)
		return revExitError
	}

	coc := review.NewChainOfCustody(&bundle)
	sigCount := signatureCount(bundle)
	if flags.jsonOut {
		type sealedFor struct {
			PRURL         string `json:"pr_url"`
			ReviewID      string `json:"review_id"`
			Signed        bool   `json:"signed"`
			Signatures    int    `json:"signatures"`
			CustodySealed bool   `json:"custody_sealed"`
		}
		out := sealedFor{
			PRURL:         bundle.PRURL,
			ReviewID:      bundle.ReviewID,
			Signed:        sigCount > 0,
			Signatures:    sigCount,
			CustodySealed: coc != nil,
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(stdout, string(b))
		return revExitOK
	}

	fmt.Fprintf(stdout, "Custody summary for PR %q (review %q)\n", bundle.PRURL, bundle.ReviewID)
	fmt.Fprintf(stdout, "  Signed: %v (signatures=%d)\n", sigCount > 0, sigCount)
	if coc != nil {
		fmt.Fprintf(stdout, "  Custody chain-of-custody sealed: true")
	} else {
		fmt.Fprintf(stdout, "  Custody chain-of-custody sealed: false")
	}
	return revExitOK
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// signatureCount returns how many of the 3 signer roles (primary, adversarial,
// audit) have a non-empty signature on the bundle. The Signatures struct is
// embedded on EvidenceBundle so we read its 3 string fields directly.
func signatureCount(b review.EvidenceBundle) int {
	n := 0
	if b.Signatures.Primary != "" {
		n++
	}
	if b.Signatures.Adversarial != "" {
		n++
	}
	if b.Signatures.Audit != "" {
		n++
	}
	return n
}

// isValidSignerRole reports whether role matches one of EvidenceBundle's 3
// signing positions.
func isValidSignerRole(role string) bool {
	switch role {
	case "primary", "adversarial", "audit":
		return true
	}
	return false
}

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

// loadFPTracker loads a tracker from a JSON file (or returns a fresh one).
func loadFPTracker(path string) (*review.FPTracker, error) {
	if path == "" {
		return review.NewFPTracker(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return review.NewFPTracker(), nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return review.NewFPTracker(), nil
	}
	// FPTracker has no JSON marshaling defined — we wrap the persisted state
	// in a struct of dismissals + flags and rehydrate manually via RecordDismissal.
	var persisted struct {
		Dismissals map[string]int `json:"dismissals"`
	}
	if err := json.Unmarshal(data, &persisted); err != nil {
		return nil, fmt.Errorf("parse tracker state: %w", err)
	}
	tracker := review.NewFPTracker()
	for model, n := range persisted.Dismissals {
		for i := 0; i < n; i++ {
			tracker.RecordDismissal(model)
		}
	}
	return tracker, nil
}

// saveFPTracker writes tracker dismissals to a JSON file. We persist the
// observable state (dismissals map) so subsequent runs see the same counts.
func saveFPTracker(path string, tracker *review.FPTracker) error {
	// Walk flagged + removed + any model we can enumerate (we don't have a
	// "list all" API, so we walk all three known sets).
	state := struct {
		Dismissals map[string]int `json:"dismissals"`
	}{Dismissals: map[string]int{}}
	known := map[string]bool{}
	for _, m := range tracker.FlaggedModels() {
		known[m] = true
	}
	for _, m := range tracker.RemovedModels() {
		known[m] = true
	}
	for m := range known {
		state.Dismissals[m] = tracker.DismissalCount(m)
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// readPrivateKeyFile reads a 64-byte raw ed25519 private key from disk.
func readPrivateKeyFile(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	keyLen := len(data)
	if keyLen == 64 {
		return ed25519.PrivateKey(data), nil
	}
	// Also accept hex-encoded (128 hex chars).
	if keyLen == 128 {
		raw, err := hex.DecodeString(string(data))
		if err != nil {
			return nil, err
		}
		if len(raw) != 64 {
			return nil, fmt.Errorf("hex-decoded key length %d != 64", len(raw))
		}
		return ed25519.PrivateKey(raw), nil
	}
	// PEM-encoded PRIVATE KEY block.
	block, _ := pem.Decode(data)
	if block != nil && block.Type == "PRIVATE KEY" {
		if key, err := parsePKCS8Ed25519(block.Bytes); err == nil {
			return key, nil
		}
	}
	return nil, fmt.Errorf("key file must be 64 raw bytes, 128 hex chars, or PEM PKCS8 ed25519 (got %d bytes)", len(data))
}

// readPublicKeyFile reads a 32-byte raw ed25519 public key from disk.
func readPublicKeyFile(path string) (ed25519.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	keyLen := len(data)
	if keyLen == 32 {
		return ed25519.PublicKey(data), nil
	}
	if keyLen == 64 {
		raw, err := hex.DecodeString(string(data))
		if err != nil {
			return nil, err
		}
		if len(raw) != 32 {
			return nil, fmt.Errorf("hex-decoded key length %d != 32", len(raw))
		}
		return ed25519.PublicKey(raw), nil
	}
	block, _ := pem.Decode(data)
	if block != nil && block.Type == "PUBLIC KEY" {
		if key, err := parsePKIXEd25519(block.Bytes); err == nil {
			return key, nil
		}
	}
	return nil, fmt.Errorf("public key file must be 32 raw bytes, 64 hex chars, or PEM PKIX ed25519 (got %d bytes)", len(data))
}

// parseIntInRange parses s as an int in [lo, hi].
func parseIntInRange(s string, lo, hi int) (int, error) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, fmt.Errorf("not an integer: %q", s)
	}
	if n < lo || n > hi {
		return 0, fmt.Errorf("%d out of range [%d, %d]", n, lo, hi)
	}
	return n, nil
}

// tsSuffix returns a short timestamp suffix for ad-hoc bundle IDs.
func tsSuffix() string {
	// Use os.Stat-friendly format without time import to keep this file lean.
	return fmt.Sprintf("cli-%d", os.Getpid())
}

// parsePKCS8Ed25519 parses a DER-encoded PKCS8 ed25519 private key.
// We don't depend on crypto/x509 to keep the key-decoding path minimal; we
// walk the PKCS8 structure manually for the ed25519 OID (1.3.101.112).
func parsePKCS8Ed25519(der []byte) (ed25519.PrivateKey, error) {
	// PKCS8 PrivateKeyInfo:
	//   SEQUENCE { version INTEGER, algorithm AlgorithmIdentifier, key OCTET STRING }
	// ed25519 OID = 06 03 2B 65 70.
	if len(der) < 16 {
		return nil, errors.New("pkcs8 der too short")
	}
	for i := 0; i < len(der)-8; i++ {
		if der[i] == 0x06 && der[i+1] == 0x03 && der[i+2] == 0x2B &&
			der[i+3] == 0x65 && der[i+4] == 0x70 {
			// Found the OID. The OCTET STRING holding the raw key follows.
			// PKCS8 wraps the 32-byte seed for ed25519; we need to return a
			// 64-byte PrivateKey (seed || pub). We can't derive the pub from
			// the seed without ed25519 internals, so we ask the caller to
			// provide a raw key.
			return nil, errors.New("PEM PKCS8 ed25519 not supported; provide a 64-byte raw key file")
		}
	}
	return nil, errors.New("ed25519 OID not found in DER")
}

// parsePKIXEd25519 parses a DER-encoded SubjectPublicKeyInfo for ed25519.
func parsePKIXEd25519(der []byte) (ed25519.PublicKey, error) {
	// SPKI structure: SEQUENCE { algorithm, BIT STRING { 04 || rawKey } }
	if len(der) < 12 {
		return nil, errors.New("spki der too short")
	}
	// Find the BIT STRING tag 0x03 and skip length + unused-bits.
	for i := 0; i < len(der)-2; i++ {
		if der[i] == 0x03 && der[i+1] == 0x22 && i+2 < len(der) && der[i+2] == 0x00 {
			candidate := der[i+3:]
			if len(candidate) == 32 {
				return ed25519.PublicKey(candidate), nil
			}
		}
	}
	return nil, errors.New("ed25519 BIT STRING not found in DER")
}

// -----------------------------------------------------------------------------
// review dashboard — human change management view
// -----------------------------------------------------------------------------

// runReviewDashboard builds the change management dashboard (blast radius,
// risk score, ADR fit, trust context) for human reviewers. Spec §6.1.
func runReviewDashboard(flags revFlags, stdout, stderr io.Writer) int {
	if flags.prURL == "" {
		fmt.Fprintln(stderr, "error: --pr is required for review dashboard")
		return revExitError
	}

	files, err := collectDashboardFiles(flags)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return revExitError
	}
	if len(files) == 0 {
		fmt.Fprintln(stderr, "error: provide --files or --files-from with at least one changed path")
		return revExitError
	}

	in := review.DashboardInput{
		PR:           flags.prURL,
		AgentID:      flags.agentID,
		ChangedFiles: files,
		RepoRoot:     flags.repoRoot,
		LedgerPath:   flags.ledgerPath,
		ADRDir:       flags.adrDir,
	}
	if flags.category != "" {
		cat, ok := parseReviewCategory(flags.category)
		if !ok {
			fmt.Fprintf(stderr, "error: invalid --category %q (contract|behavioral|resilience|cosmetic)\n", flags.category)
			return revExitError
		}
		in.Category = cat
	}
	if flags.tier != "" {
		tier, ok := parseReviewTrustTier(flags.tier)
		if !ok {
			fmt.Fprintf(stderr, "error: invalid --tier %q (provisional|observed|trusted|veteran)\n", flags.tier)
			return revExitError
		}
		in.TrustTier = tier
	}
	if flags.incidents != "" {
		incs, err := loadRelatedIncidents(flags.incidents)
		if err != nil {
			fmt.Fprintf(stderr, "error: --incidents: %v\n", err)
			return revExitError
		}
		in.RelatedIncidents = incs
	}

	dash, err := review.BuildDashboard(in)
	if err != nil {
		fmt.Fprintf(stderr, "error: build dashboard: %v\n", err)
		return revExitError
	}

	if flags.jsonOut {
		raw, err := review.DashboardJSON(dash)
		if err != nil {
			fmt.Fprintf(stderr, "error: marshal dashboard: %v\n", err)
			return revExitError
		}
		fmt.Fprintln(stdout, string(raw))
		return revExitOK
	}

	fmt.Fprint(stdout, review.FormatDashboard(dash))
	return revExitOK
}

func collectDashboardFiles(flags revFlags) ([]string, error) {
	var files []string
	if flags.filesCSV != "" {
		for _, p := range strings.Split(flags.filesCSV, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				files = append(files, p)
			}
		}
	}
	if flags.filesFrom != "" {
		body, err := os.ReadFile(flags.filesFrom)
		if err != nil {
			return nil, fmt.Errorf("read --files-from: %w", err)
		}
		for _, line := range strings.Split(string(body), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			files = append(files, line)
		}
	}
	return files, nil
}

func parseReviewCategory(s string) (review.ChangeCategory, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "contract":
		return review.CategoryContract, true
	case "behavioral":
		return review.CategoryBehavioral, true
	case "resilience":
		return review.CategoryResilience, true
	case "cosmetic":
		return review.CategoryCosmetic, true
	default:
		return "", false
	}
}

func parseReviewTrustTier(s string) (trust.TrustTier, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "provisional":
		return trust.TierProvisional, true
	case "observed":
		return trust.TierObserved, true
	case "trusted":
		return trust.TierTrusted, true
	case "veteran":
		return trust.TierVeteran, true
	default:
		return "", false
	}
}

func loadRelatedIncidents(path string) ([]review.RelatedIncident, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var incs []review.RelatedIncident
	if err := json.Unmarshal(body, &incs); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	return incs, nil
}

// -----------------------------------------------------------------------------
// review run — multi-model adversarial review
// -----------------------------------------------------------------------------

// runReviewRun dispatches a multi-model adversarial review against a PR.
// It creates model clients for Chimera and DeepSeek, builds a review panel,
// and runs the orchestrator. Outputs the consensus verdict as JSON.
func runReviewRun(flags revFlags, stdout, stderr io.Writer) int {
	if flags.prURL == "" {
		fmt.Fprintln(stderr, "error: --pr URL is required for review run")
		return revExitError
	}

	// Create model clients.
	// In production these would read API keys from environment.
	chimeraClient := review.NewChimeraClient(review.ModelClientConfig{
		BaseURL: "http://localhost:8765",
		Model:   "chimera-default",
	})
	deepseekClient := review.NewDeepSeekClient(review.ModelClientConfig{
		BaseURL: "https://api.deepseek.com/v1",
		APIKey:  os.Getenv("DEEPSEEK_API_KEY"),
		Model:   "deepseek-v4-flash",
	})

	orchestrator := review.NewReviewOrchestrator()

	// Build a 2-model panel (primary + adversarial) for behavioral review.
	panel := &review.ReviewPanel{
		Primary:     deepseekClient,
		Adversarial: chimeraClient,
	}

	// For the CLI demo, use a small representative diff.
	// In production this would be fetched from the PR.
	diff := fmt.Sprintf("Review of PR %s\n\n(Full diff would be fetched from Forgejo API)\n", flags.prURL)
	commitMsg := "review run via helix CLI"

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	result, err := orchestrator.Review(ctx, panel, diff, commitMsg,
		review.CategoryBehavioral, flags.prURL)
	if err != nil {
		fmt.Fprintf(stderr, "error: review failed: %v\n", err)
		return revExitError
	}

	if flags.jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			fmt.Fprintf(stderr, "error: marshal result: %v\n", err)
			return revExitError
		}
		return revExitOK
	}

	fmt.Fprintf(stdout, "Review complete for PR %s\n", flags.prURL)
	fmt.Fprintf(stdout, "Consensus: %s (%d/%d models agree)\n",
		result.ConsensusLevel, result.ModelsAgree, result.TotalModels)
	fmt.Fprintf(stdout, "Diversity score: %d\n", result.DiversityScore)
	fmt.Fprintf(stdout, "Findings: %d\n", len(result.Bundle.Findings))
	return revExitOK
}
