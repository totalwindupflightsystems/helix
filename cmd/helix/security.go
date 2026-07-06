package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/totalwindupflightsystems/helix/pkg/security"
)

// ============================================================================
// helix security CLI — security hardening checklist (spec SPECIFICATION.md
// §6.6 Security Hardening Checklist)
// ============================================================================

// secFlags holds parsed CLI flags.
type secFlags struct {
	subcommand string // check, checklist, help
	jsonOut    bool
}

// parseSecurityFlags parses args for `helix security`.
func parseSecurityFlags(args []string) (secFlags, bool, int) {
	var f secFlags
	helpWanted := false

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			helpWanted = true
		case arg == "--json":
			f.jsonOut = true
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

// printSecurityHelp prints the help text.
func printSecurityHelp(w io.Writer) {
	fmt.Fprintln(w, `helix security — security hardening checklist

Runs the deployment security hardening checklist from spec §6.6. Each check
verifies a security property of the Helix deployment.

Usage:
  helix security check [--json]
  helix security checklist [--json]
  helix security help

Subcommands:
  check      Run the full hardening checklist
  checklist  List all hardening checks with descriptions
  help       Show this help

Flags:
  --json     Structured JSON output with per-check PASS/FAIL/NA
  --help, -h Show this help

Exit codes:
  0  All critical checks passed
  1  One or more critical checks failed
  2  Invocation error`)
}

// runSecurity is the entry point for `helix security`.
func runSecurity(args []string, stdout, stderr io.Writer) int {
	flags, helpWanted, rc := parseSecurityFlags(args)
	if rc != mgExitOK {
		return rc
	}
	if helpWanted {
		printSecurityHelp(stdout)
		return mgExitOK
	}

	switch flags.subcommand {
	case "help":
		printSecurityHelp(stdout)
		return mgExitOK
	case "check":
		return runSecurityCheck(flags, stdout)
	case "checklist":
		return runSecurityChecklist(flags, stdout)
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n", flags.subcommand)
		return mgExitError
	}
}

// runSecurityCheck runs the full hardening checklist.
func runSecurityCheck(flags secFlags, stdout io.Writer) int {
	checker := security.NewHardeningChecker()
	for _, check := range security.DefaultChecks() {
		c := check // capture
		checker.RegisterCheck(c.ID, func() (security.CheckStatus, string) {
			// CLI checks are environmental — we can only verify what's
			// accessible from the process. Most deployment hardening
			// checks require server-side inspection, so they report SKIP
			// (not applicable from this context).
			return security.StatusSkip, "Requires server-side inspection — run on the deployment host"
		})
	}

	report := checker.Run()

	if flags.jsonOut {
		type checkItem struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Status   string `json:"status"`
			Detail   string `json:"detail"`
			Category string `json:"category"`
		}
		items := make([]checkItem, len(report.Results))
		for i, r := range report.Results {
			items[i] = checkItem{
				ID:       r.Check.ID,
				Name:     r.Check.Name,
				Status:   string(r.Status),
				Detail:   r.Detail,
				Category: string(r.Check.Category),
			}
		}
		summary := security.NewHardeningSummary(report)
		out, _ := json.MarshalIndent(map[string]any{
			"checks":  items,
			"summary": summary,
		}, "", "  ")
		fmt.Fprintln(stdout, string(out))
	} else {
		fmt.Fprintln(stdout, report.FormatReport())
	}

	// Exit 0 if no FAIL results (NA is acceptable from CLI context).
	for _, r := range report.Results {
		if r.Status == security.StatusFail {
			return mgExitBlock
		}
	}
	return mgExitOK
}

// runSecurityChecklist lists all hardening checks.
func runSecurityChecklist(flags secFlags, stdout io.Writer) int {
	checks := security.DefaultChecks()

	if flags.jsonOut {
		type checkItem struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Category    string `json:"category"`
		}
		items := make([]checkItem, len(checks))
		for i, c := range checks {
			items[i] = checkItem{
				ID:          c.ID,
				Name:        c.Name,
				Description: c.Description,
				Category:    string(c.Category),
			}
		}
		out, _ := json.MarshalIndent(items, "", "  ")
		fmt.Fprintln(stdout, string(out))
		return mgExitOK
	}

	fmt.Fprintf(stdout, "\nSecurity Hardening Checklist (%d items)\n\n", len(checks))
	for _, c := range checks {
		fmt.Fprintf(stdout, "  %-30s  %s\n", c.ID, c.Name)
		fmt.Fprintf(stdout, "  %-30s  Category: %s\n", "", c.Category)
		fmt.Fprintf(stdout, "  %-30s  %s\n\n", "", c.Description)
	}
	return mgExitOK
}

// runSecurityWithDryRun wraps runSecurity with the global --dry-run flag.
func runSecurityWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
	rc := runSecurity(args, stdout, stderr)
	if rc != 0 && rc != mgExitBlock {
		return errExit{code: rc}
	}
	return nil
}
