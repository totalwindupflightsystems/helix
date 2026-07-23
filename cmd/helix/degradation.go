package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/degradation"
)

// ============================================================================
// helix degradation CLI — wraps pkg/degradation.Registry (spec §14.2)
// ============================================================================

const (
	degradationExitOK    = 0
	degradationExitError = 2
)

// degradationFlags holds parsed CLI flags for the degradation subcommand.
type degradationFlags struct {
	subcommand string // list, check, help
	jsonOut    bool
	dryRun     bool
	service    string
	state      string
}

// parseDegradationFlags parses the args for `helix degradation`.
func parseDegradationFlags(args []string) (degradationFlags, int) {
	var f degradationFlags
	f.subcommand = "list" // default

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			printDegradationHelp(os.Stdout)
			return f, degradationExitOK
		case arg == "--json":
			f.jsonOut = true
		case arg == "--dry-run":
			f.dryRun = true
		case strings.HasPrefix(arg, "--"):
			return f, degradationExitError
		default:
			if f.subcommand == "list" && arg != "" && !strings.HasPrefix(arg, "-") {
				f.subcommand = arg
			} else if f.subcommand == "check" && f.service == "" && arg != "" && !strings.HasPrefix(arg, "-") {
				f.service = arg
			} else if f.subcommand == "check" && f.state == "" && arg != "" && !strings.HasPrefix(arg, "-") {
				f.state = arg
			} else {
				return f, degradationExitError
			}
		}
		i++
	}

	return f, degradationExitOK
}

// printDegradationHelp prints the help text for the degradation subcommand.
func printDegradationHelp(w io.Writer) {
	fmt.Fprintln(w, `helix degradation — graceful degradation policy observability (spec §14.2)

Usage:
  helix degradation list [--json]
  helix degradation check <service> <state> [--json]
  helix degradation help

Subcommands:
  list                   Show all degradation policies by service
  check <service> <state>  Show the applicable policy for a service+state
  help                   Show this help

Arguments:
  service  Platform service name (forgejo, chimera, conscientiousness,
           hivemind, langfuse, prometheus, caddy, duckbrain, muster)
  state    Health state (healthy, degraded, down, unknown)

Flags:
  --json     Output structured JSON report
  --dry-run  No effect (degradation is read-only)

Exit codes:
  0  Success
  2  Error in invocation`)
}

// runDegradation is the entry point for `helix degradation`.
func runDegradation(args []string, stdout, stderr io.Writer) int {
	flags, rc := parseDegradationFlags(args)
	if rc != degradationExitOK {
		return rc
	}

	switch flags.subcommand {
	case "help":
		printDegradationHelp(stdout)
		return degradationExitOK
	case "list":
		return runDegradationList(flags, stdout, stderr)
	case "check":
		return runDegradationCheck(flags, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n", flags.subcommand)
		return degradationExitError
	}
}

// runDegradationList prints all degradation policies.
func runDegradationList(flags degradationFlags, stdout, stderr io.Writer) int {
	reg, err := degradation.DefaultRegistry()
	if err != nil {
		fmt.Fprintf(stderr, "error: initialize degradation registry: %v\n", err)
		return degradationExitError
	}

	if flags.jsonOut {
		report := reg.GenerateReport()
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return degradationExitError
		}
		fmt.Fprintln(stdout, string(data))
		return degradationExitOK
	}

	// Human-readable output
	fmt.Fprint(stdout, reg.FormatReport())
	return degradationExitOK
}

// runDegradationCheck shows the applicable policy for a service+state.
func runDegradationCheck(flags degradationFlags, stdout, stderr io.Writer) int {
	if flags.service == "" || flags.state == "" {
		fmt.Fprintf(stderr, "error: check requires <service> <state>\n")
		return degradationExitError
	}

	svc := degradation.Service(flags.service)
	state := degradation.HealthState(flags.state)

	reg, err := degradation.DefaultRegistry()
	if err != nil {
		fmt.Fprintf(stderr, "error: initialize degradation registry: %v\n", err)
		return degradationExitError
	}
	policy, ok := reg.Lookup(svc, state)
	if !ok {
		fmt.Fprintf(stderr, "no policy found for service=%s state=%s\n", flags.service, flags.state)
		return degradationExitError
	}

	result := reg.ApplyPolicy(svc, state)

	if flags.jsonOut {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return degradationExitError
		}
		fmt.Fprintln(stdout, string(data))
		return degradationExitOK
	}

	fmt.Fprintln(stdout, degradation.FormatApplyResult(result))
	if policy.Fallback != "" {
		fmt.Fprintf(stdout, "  fallback: %s\n", policy.Fallback)
	}
	return degradationExitOK
}

// runDegradationWithDryRun wraps runDegradation with the global --dry-run flag.
func runDegradationWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
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
	rc := runDegradation(args, stdout, stderr)
	if rc != 0 {
		return errExit{code: rc}
	}
	return nil
}
