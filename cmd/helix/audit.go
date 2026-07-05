package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/audit"
)

// ============================================================================
// helix audit CLI — 12-step audit chain trace (spec §6.5)
// ============================================================================

const (
	auditExitOK       = 0
	auditExitFail     = 1 // audit found issues
	auditExitError    = 2 // invocation error
	auditExitNotFound = 3 // file not found
)

// auditFlags holds parsed CLI flags for the audit subcommand.
type auditFlags struct {
	subcommand   string // trace, steps, validate, help
	evidenceFile string
	prIndex      int
	jsonOut      bool
	dryRun       bool
}

// parseAuditFlags parses the args for `helix audit`.
func parseAuditFlags(args []string) (auditFlags, int) {
	var f auditFlags
	f.subcommand = "" // no default — requires explicit subcommand
	f.prIndex = 0

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			printAuditHelp(os.Stdout)
			return f, auditExitOK
		case arg == "--json":
			f.jsonOut = true
		case arg == "--dry-run":
			f.dryRun = true
		case arg == "--evidence-file":
			if i+1 < len(args) {
				f.evidenceFile = args[i+1]
				i++
			} else {
				return f, auditExitError
			}
		case arg == "--pr-index":
			if i+1 < len(args) {
				_, err := fmt.Sscanf(args[i+1], "%d", &f.prIndex)
				if err != nil {
					return f, auditExitError
				}
				i++
			} else {
				return f, auditExitError
			}
		case strings.HasPrefix(arg, "--"):
			return f, auditExitError
		default:
			if f.subcommand == "" && arg != "" && !strings.HasPrefix(arg, "-") {
				f.subcommand = arg
			} else {
				return f, auditExitError
			}
		}
		i++
	}

	if f.subcommand == "" {
		f.subcommand = "help"
	}

	return f, auditExitOK
}

// printAuditHelp prints the help text for the audit subcommand.
func printAuditHelp(w io.Writer) {
	fmt.Fprintln(w, `helix audit — 12-step audit chain trace (spec §6.5)

Usage:
  helix audit trace --evidence-file <path> [--pr-index N] [--json]
  helix audit validate --evidence-file <path> [--json]
  helix audit steps [--json]
  helix audit help

Subcommands:
  trace      Run the 12-step audit checker against evidence file
  validate   Check evidence completeness (structural, no per-step checks)
  steps      List all 12 audit steps with descriptions
  help       Show this help

Flags:
  --evidence-file <path>  Path to JSON file containing AuditEvidence
  --pr-index <N>          PR number for the audit report (default: 0)
  --json                  Output structured JSON report
  --dry-run               No effect (audit is read-only)

Exit codes:
  0  Success (audit passed or steps listed)
  1  Audit found issues (trace subcommand only)
  2  Invocation error
  3  Evidence file not found`)
}

// runAudit is the entry point for `helix audit`.
func runAudit(args []string, stdout, stderr io.Writer) int {
	flags, rc := parseAuditFlags(args)
	if rc != auditExitOK {
		return rc
	}

	switch flags.subcommand {
	case "help":
		printAuditHelp(stdout)
		return auditExitOK
	case "steps":
		return runAuditSteps(flags, stdout, stderr)
	case "trace":
		return runAuditTrace(flags, stdout, stderr)
	case "validate":
		return runAuditValidate(flags, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n", flags.subcommand)
		return auditExitError
	}
}

// runAuditSteps lists all 12 audit steps with descriptions.
func runAuditSteps(flags auditFlags, stdout, stderr io.Writer) int {
	steps := audit.AllSteps()

	if flags.jsonOut {
		type stepInfo struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		var list []stepInfo
		for _, s := range steps {
			list = append(list, stepInfo{
				ID:          int(s),
				Name:        audit.StepName(s),
				Description: audit.StepDescription(s),
			})
		}
		data, err := json.MarshalIndent(list, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return auditExitError
		}
		fmt.Fprintln(stdout, string(data))
		return auditExitOK
	}

	fmt.Fprintf(stdout, "12-Step Audit Chain (spec §6.5)\n\n")
	for _, s := range steps {
		fmt.Fprintf(stdout, "  Step %2d: %-24s  %s\n", s, audit.StepName(s), audit.StepDescription(s))
	}
	return auditExitOK
}

// runAuditTrace reads an evidence file and runs the full audit checker.
func runAuditTrace(flags auditFlags, stdout, stderr io.Writer) int {
	if flags.evidenceFile == "" {
		fmt.Fprintf(stderr, "error: trace requires --evidence-file <path>\n")
		return auditExitError
	}

	data, err := os.ReadFile(flags.evidenceFile)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(stderr, "error: evidence file not found: %s\n", flags.evidenceFile)
			return auditExitNotFound
		}
		fmt.Fprintf(stderr, "error: reading evidence file: %v\n", err)
		return auditExitError
	}

	ev, err := audit.UnmarshalEvidence(data)
	if err != nil {
		fmt.Fprintf(stderr, "error: parsing evidence file: %v\n", err)
		return auditExitError
	}

	checker := audit.NewChecker()
	report := checker.Check(flags.prIndex, ev)

	if flags.jsonOut {
		jsonData, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return auditExitError
		}
		fmt.Fprintln(stdout, string(jsonData))
	} else {
		fmt.Fprint(stdout, report.FormatReport())
	}

	if report.AllPassed {
		return auditExitOK
	}
	return auditExitFail
}

// runAuditValidate checks evidence completeness without running per-step checks.
func runAuditValidate(flags auditFlags, stdout, stderr io.Writer) int {
	if flags.evidenceFile == "" {
		fmt.Fprintf(stderr, "error: validate requires --evidence-file <path>\n")
		return auditExitError
	}

	data, err := os.ReadFile(flags.evidenceFile)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(stderr, "error: evidence file not found: %s\n", flags.evidenceFile)
			return auditExitNotFound
		}
		fmt.Fprintf(stderr, "error: reading evidence file: %v\n", err)
		return auditExitError
	}

	ev, err := audit.UnmarshalEvidence(data)
	if err != nil {
		fmt.Fprintf(stderr, "error: parsing evidence file: %v\n", err)
		return auditExitError
	}

	completed := ev.CompletedSteps()
	complete := ev.IsComplete()
	missingSteps := []string{}
	allSteps := audit.AllSteps()
	completedMap := make(map[audit.StepID]bool, len(completed))
	for _, s := range completed {
		completedMap[s] = true
	}
	for _, s := range allSteps {
		if !completedMap[s] {
			missingSteps = append(missingSteps, audit.StepName(s))
		}
	}

	if flags.jsonOut {
		type validateReport struct {
			Complete      bool     `json:"complete"`
			CompletedN    int      `json:"completed_steps"`
			TotalSteps    int      `json:"total_steps"`
			MissingSteps  []string `json:"missing_steps"`
			CompletedList []string `json:"completed_step_names"`
		}
		var completedNames []string
		for _, s := range completed {
			completedNames = append(completedNames, audit.StepName(s))
		}
		if missingSteps == nil {
			missingSteps = []string{}
		}
		report := validateReport{
			Complete:      complete,
			CompletedN:    len(completed),
			TotalSteps:    len(allSteps),
			MissingSteps:  missingSteps,
			CompletedList: completedNames,
		}
		out, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return auditExitError
		}
		fmt.Fprintln(stdout, string(out))
	} else {
		fmt.Fprintf(stdout, "Evidence Validation — %d/%d steps present\n", len(completed), len(allSteps))
		if complete {
			fmt.Fprintf(stdout, "Status: COMPLETE — all 12 steps have evidence\n")
		} else {
			fmt.Fprintf(stdout, "Status: INCOMPLETE — %d step(s) missing:\n", len(missingSteps))
			for _, m := range missingSteps {
				fmt.Fprintf(stdout, "  ✗ %s\n", m)
			}
		}
	}

	if complete {
		return auditExitOK
	}
	return auditExitFail
}

// runAuditWithDryRun wraps runAudit with the global --dry-run flag.
func runAuditWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
	if globalDryRun {
		hasDryRun := false
		for _, a := range args {
			if a == "--dry-run" {
				hasDryRun = true
				break
			}
		}
		if !hasDryRun {
			args = append([]string{"--dry-run"}, args...)
		}
	}
	rc := runAudit(args, stdout, stderr)
	if rc != 0 {
		return errExit{code: rc}
	}
	return nil
}
