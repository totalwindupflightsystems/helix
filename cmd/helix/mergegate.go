package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/mergegate"
	"github.com/totalwindupflightsystems/helix/pkg/review"
	"github.com/totalwindupflightsystems/helix/pkg/trust"
	"github.com/totalwindupflightsystems/helix/pkg/verify"
)

// ============================================================================
// helix mergegate CLI — pre-merge validation gate (spec adversarial-review.md,
// production-verification.md, trust-model.md)
// ============================================================================

const (
	mgExitOK    = 0
	mgExitBlock = 1 // merge blocked by a failed check
	mgExitError = 2 // invocation / IO error
)

// mgCheck describes a single gate check for the `checks` listing.
type mgCheck struct {
	ID          string
	Name        string
	Description string
}

var mgAllChecks = []mgCheck{
	{ID: "evidence", Name: "Evidence Bundle", Description: "Validates that an evidence bundle exists from the adversarial review pipeline and its signatures are valid (spec adversarial-review.md §Evidence Bundles)"},
	{ID: "consensus", Name: "Consensus Threshold", Description: "Checks that the review consensus resolution was met — APPROVE requires all models to agree or a valid tie-break decision (spec adversarial-review.md §Consensus)"},
	{ID: "contract", Name: "Behavior Contract", Description: "Validates that a behavior contract YAML exists and its assertions are well-formed (spec production-verification.md §Behavior Contracts)"},
	{ID: "trust", Name: "Trust Tier Gate", Description: "Checks the agent's trust tier meets the minimum requirement for the changed file categories (spec trust-model.md §Integration Points)"},
	{ID: "cost", Name: "Cost Guard", Description: "Validates the pre-dispatch cost guard was approved and the estimated cost is within the agent's tier budget (spec trust-model.md §Cost Caps)"},
}

// mgFlags holds parsed CLI flags.
type mgFlags struct {
	subcommand   string // check, checks, hook
	evidence     string // path to evidence bundle JSON (check)
	evidencePath string // path to evidence directory (hook)
	contract     string // path to behavior contract YAML
	trustTier    string // agent trust tier
	agent        string // agent ID
	jsonOut      bool
	skipContract bool
	skipCost     bool
	dryRun       bool
	protected    string // comma-separated protected branch patterns (hook)
}

// parseMergeGateFlags parses args for `helix mergegate`.
func parseMergeGateFlags(args []string) (mgFlags, bool, int) {
	var f mgFlags
	helpWanted := false

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			helpWanted = true
		case arg == "--json":
			f.jsonOut = true
		case arg == "--evidence":
			if i+1 < len(args) {
				f.evidence = args[i+1]
				i++
			} else {
				return f, false, mgExitError
			}
		case arg == "--contract":
			if i+1 < len(args) {
				f.contract = args[i+1]
				i++
			} else {
				return f, false, mgExitError
			}
		case arg == "--trust":
			if i+1 < len(args) {
				f.trustTier = args[i+1]
				i++
			} else {
				return f, false, mgExitError
			}
		case arg == "--agent":
			if i+1 < len(args) {
				f.agent = args[i+1]
				i++
			} else {
				return f, false, mgExitError
			}
		case arg == "--skip-contract":
			f.skipContract = true
		case arg == "--skip-cost":
			f.skipCost = true
		case arg == "--dry-run":
			f.dryRun = true
		case arg == "--evidence-path":
			if i+1 < len(args) {
				f.evidencePath = args[i+1]
				i++
			} else {
				return f, false, mgExitError
			}
		case arg == "--protected":
			if i+1 < len(args) {
				f.protected = args[i+1]
				i++
			} else {
				return f, false, mgExitError
			}
		case arg == "--":
			// rest are positional
		case len(arg) > 2 && arg[0] == '-' && arg[1] == '-':
			return f, false, mgExitError
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

	return f, helpWanted, mgExitOK
}

// printMergeGateHelp prints the help text.
func printMergeGateHelp(w io.Writer) {
	fmt.Fprintln(w, `helix mergegate — pre-merge validation gate

Runs all 5 Helix quality checks before allowing a merge:
  1. Evidence bundle valid (adversarial-review.md)
  2. Consensus threshold met (adversarial-review.md)
  3. Behavior contract well-formed (production-verification.md)
  4. Trust tier meets minimum for file categories (trust-model.md)
  5. Cost guard approved within tier budget (trust-model.md)

Usage:
  helix mergegate check --evidence <path> --contract <path> --trust <tier> [--agent <name>] [--json]
  helix mergegate checks
  helix mergegate hook [--trust <tier>] [--evidence-path <dir>] [--protected main,master] [--dry-run]
  helix mergegate help

Subcommands:
  check    Run the merge gate with the given artifacts
  checks   List all 5 gate checks with descriptions
  hook     Run as a pre-receive hook (reads git refs from stdin)
  help     Show this help

Flags:
  --evidence <path>      Path to evidence bundle JSON file (check)
  --evidence-path <dir>  Path to evidence directory (hook)
  --contract <path>      Path to behavior contract YAML file
  --trust <tier>         Agent trust tier: provisional|observed|trusted|veteran
  --agent <name>         Agent ID (optional, for display)
  --protected <list>     Comma-separated protected branch patterns (hook)
  --skip-contract        Skip the behavior contract check
  --skip-cost            Skip the cost guard check
  --dry-run              Dry-run mode: do not reject pushes (hook)
  --json                 Structured JSON output
  --help, -h             Show this help

Exit codes:
  0  Merge ALLOWED
  1  Merge BLOCKED or ESCALATED
  2  Invocation error`)
}

// runMergeGate is the entry point for `helix mergegate`.
func runMergeGate(args []string, stdout, stderr io.Writer) int {
	flags, helpWanted, rc := parseMergeGateFlags(args)
	if rc != mgExitOK {
		return rc
	}
	if helpWanted {
		printMergeGateHelp(stdout)
		return mgExitOK
	}

	switch flags.subcommand {
	case "help":
		printMergeGateHelp(stdout)
		return mgExitOK
	case "check":
		return runMergeGateCheck(flags, stdout, stderr)
	case "checks":
		return runMergeGateChecks(flags, stdout)
	case "hook":
		return runMergeGateHook(flags, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n", flags.subcommand)
		return mgExitError
	}
}

// runMergeGateCheck runs the merge gate evaluation.
func runMergeGateCheck(flags mgFlags, stdout, stderr io.Writer) int {
	// Parse trust tier.
	var tier trust.TrustTier
	switch flags.trustTier {
	case "provisional", "observed", "trusted", "veteran":
		tier = trust.TrustTier(flags.trustTier)
	case "":
		fmt.Fprintf(stderr, "error: --trust <tier> is required (provisional|observed|trusted|veteran)\n")
		return mgExitError
	default:
		fmt.Fprintf(stderr, "error: invalid trust tier %q (use: provisional|observed|trusted|veteran)\n", flags.trustTier)
		return mgExitError
	}

	// Load evidence bundle.
	var bundle *review.EvidenceBundle
	if flags.evidence != "" {
		data, err := os.ReadFile(flags.evidence)
		if err != nil {
			fmt.Fprintf(stderr, "error: reading evidence bundle: %v\n", err)
			return mgExitError
		}
		bundle = &review.EvidenceBundle{}
		if err := json.Unmarshal(data, bundle); err != nil {
			fmt.Fprintf(stderr, "error: parsing evidence bundle JSON: %v\n", err)
			return mgExitError
		}
	}

	// Load behavior contract.
	var contract *verify.BehaviorContract
	if flags.contract != "" {
		data, err := os.ReadFile(flags.contract)
		if err != nil {
			fmt.Fprintf(stderr, "error: reading behavior contract: %v\n", err)
			return mgExitError
		}
		contract, err = verify.ParseContract(data)
		if err != nil {
			fmt.Fprintf(stderr, "error: parsing behavior contract: %v\n", err)
			return mgExitError
		}
	}

	// Build merge gate options.
	var opts []mergegate.GateOption
	if flags.skipContract {
		opts = append(opts, mergegate.WithContractSkipped())
	}
	if flags.skipCost {
		opts = append(opts, mergegate.WithCostSkipped())
	}

	gate := mergegate.NewMergeGate(opts...)

	// Build the merge request.
	req := mergegate.MergeRequest{
		AgentID:   flags.agent,
		AgentTier: tier,
		Bundle:    bundle,
		Contract:  contract,
	}

	report := gate.Evaluate(req)

	// Output.
	if flags.jsonOut {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "error: encoding report: %v\n", err)
			return mgExitError
		}
		fmt.Fprintln(stdout, string(data))
	} else {
		printMergeGateReport(stdout, report)
	}

	if report.IsAllowed() {
		return mgExitOK
	}
	// BLOCKED or ESCALATED → exit 1
	return mgExitBlock
}

// printMergeGateReport prints a human-readable merge gate report.
func printMergeGateReport(w io.Writer, report mergegate.GateReport) {
	icon := "✗"
	if report.IsAllowed() {
		icon = "✓"
	}

	fmt.Fprintf(w, "\n%s  MERGE GATE: %s\n", icon, report.Decision)
	fmt.Fprintf(w, "   Agent: %s  (Tier: %s)\n\n", report.AgentID, report.AgentTier)

	fmt.Fprintf(w, "   %-20s  %-8s  %s\n", "CHECK", "STATUS", "REASON")
	fmt.Fprintf(w, "   %-20s  %-8s  %s\n", "--------------------", "--------", "--------")

	for _, c := range report.Checks {
		statusIcon := statusEmoji(c.Status)
		fmt.Fprintf(w, "   %-20s  %s %-6s  %s\n", c.Name, statusIcon, c.Status, c.Reason)
		if c.Details != "" {
			fmt.Fprintf(w, "   %-20s  %-8s  ↳ %s\n", "", "", c.Details)
		}
	}

	if len(report.Blockers) > 0 {
		fmt.Fprintf(w, "\n   Blockers:\n")
		for _, b := range report.Blockers {
			fmt.Fprintf(w, "     • %s\n", b)
		}
	}

	fmt.Fprintf(w, "\n%s\n", report.Summary())
}

// statusEmoji returns an emoji for a check status.
func statusEmoji(s mergegate.CheckStatus) string {
	switch s {
	case mergegate.CheckPass:
		return "✅"
	case mergegate.CheckFail:
		return "❌"
	case mergegate.CheckSkipped:
		return "⏭️"
	case mergegate.CheckWarning:
		return "⚠️"
	default:
		return "?"
	}
}

// runMergeGateChecks lists all gate checks.
func runMergeGateChecks(flags mgFlags, stdout io.Writer) int {
	if flags.jsonOut {
		type checkItem struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		items := make([]checkItem, len(mgAllChecks))
		for i, c := range mgAllChecks {
			items[i] = checkItem(c)
		}
		data, err := json.MarshalIndent(items, "", "  ")
		if err != nil {
			return mgExitError
		}
		fmt.Fprintln(stdout, string(data))
		return mgExitOK
	}

	fmt.Fprintf(stdout, "\nMerge Gate Checks (%d)\n\n", len(mgAllChecks))
	for _, c := range mgAllChecks {
		fmt.Fprintf(stdout, "  %-12s  %s\n", c.ID, c.Name)
		fmt.Fprintf(stdout, "  %-12s  %s\n\n", "", c.Description)
	}
	return mgExitOK
}

// runMergeGateWithDryRun wraps runMergeGate with the global --dry-run flag.
func runMergeGateWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
	rc := runMergeGate(args, stdout, stderr)
	if rc != 0 && rc != mgExitBlock {
		return errExit{code: rc}
	}
	return nil
}

// runMergeGateHook runs the pre-receive hook evaluation.
// Reads git refs from stdin (standard pre-receive protocol).
func runMergeGateHook(flags mgFlags, stdout, stderr io.Writer) int {
	cfg := mergegate.DefaultHookConfig()

	// Override with CLI flags.
	cfg.TrustTier = flags.trustTier
	cfg.AgentID = flags.agent
	cfg.EvidencePath = flags.evidencePath
	cfg.DryRun = flags.dryRun

	// Parse protected branches from comma-separated list.
	if flags.protected != "" {
		cfg.ProtectedBranches = strings.Split(flags.protected, ",")
		for i, b := range cfg.ProtectedBranches {
			cfg.ProtectedBranches[i] = strings.TrimSpace(b)
		}
	}

	// Allow env override for bypass.
	if os.Getenv("HELIX_SKIP_GATE") == "1" {
		fmt.Fprintln(stderr, "helix-pre-receive: HELIX_SKIP_GATE=1 set, allowing push")
		return mgExitOK
	}

	if err := mergegate.EvaluateHook(cfg, os.Stdin, stdout, stderr); err != nil {
		return mgExitBlock
	}
	return mgExitOK
}
