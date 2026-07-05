package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/retry"
)

// ============================================================================
// helix retry CLI — wraps pkg/retry with self-check + chaos-mode (spec §14.1/§14.3)
// ============================================================================

const (
	retryExitOK    = 0
	retryExitError = 2
)

// retryFlags holds parsed CLI flags for the retry subcommand.
type retryFlags struct {
	subcommand  string // status, chaos, reset
	jsonOut     bool
	policyName  string
	failureRate float64
	duration    time.Duration
	dryRun      bool
}

// parseRetryFlags parses the args for `helix retry`.
func parseRetryFlags(args []string) (retryFlags, int) {
	var f retryFlags
	f.subcommand = "status" // default

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			printRetryHelp(os.Stdout)
			return f, retryExitOK
		case arg == "--json":
			f.jsonOut = true
		case arg == "--dry-run":
			f.dryRun = true
		case arg == "--policy":
			i++
			if i >= len(args) {
				return f, retryExitError
			}
			f.policyName = args[i]
		case arg == "--failure-rate":
			i++
			if i >= len(args) {
				return f, retryExitError
			}
			rate, err := parseFloat(args[i])
			if err != nil || rate < 0 || rate > 1 {
				fmt.Fprintf(os.Stderr, "error: invalid failure-rate %q (must be 0.0-1.0)\n", args[i])
				return f, retryExitError
			}
			f.failureRate = rate
		case arg == "--duration":
			i++
			if i >= len(args) {
				return f, retryExitError
			}
			d, err := time.ParseDuration(args[i])
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: invalid duration %q: %v\n", args[i], err)
				return f, retryExitError
			}
			f.duration = d
		case strings.HasPrefix(arg, "--"):
			return f, retryExitError
		default:
			if f.subcommand == "status" && arg != "" && !strings.HasPrefix(arg, "-") {
				f.subcommand = arg
			} else {
				return f, retryExitError
			}
		}
		i++
	}

	// Set defaults for chaos subcommand
	if f.subcommand == "chaos" {
		if f.failureRate == 0 {
			f.failureRate = 0.3 // default 30%
		}
		if f.duration == 0 {
			f.duration = 60 * time.Second // default 60s
		}
	}

	return f, retryExitOK
}

// printRetryHelp prints the help text for the retry subcommand.
func printRetryHelp(w io.Writer) {
	fmt.Fprintln(w, `helix retry — retry policy observability (spec §14.1/§14.3)

Usage:
  helix retry status [--json]
  helix retry chaos --policy NAME [--failure-rate 0.3] [--duration 60s]
  helix retry reset [--policy NAME]
  helix retry help

Subcommands:
  status    Show all retry policies and circuit-breaker state
  chaos     Inject synthetic failures (requires HELIX_CHAOS_ENABLED=1)
  reset     Clear accumulated stats (all or single policy via --policy)
  help      Show this help

Flags:
  --json                Output structured JSON report
  --policy NAME         Target a specific policy
  --failure-rate RATE   Chaos failure probability (0.0-1.0, default 0.3)
  --duration DURATION   Chaos duration (default 60s)
  --dry-run             No effect (retry is read-only except chaos)

Exit codes:
  0  Success
  2  Error in invocation`)
}

// runRetry is the entry point for `helix retry`.
func runRetry(args []string, stdout, stderr io.Writer) int {
	flags, rc := parseRetryFlags(args)
	if rc != retryExitOK {
		return rc
	}

	switch flags.subcommand {
	case "help":
		printRetryHelp(stdout)
		return retryExitOK
	case "status":
		return runRetryStatus(flags, stdout, stderr)
	case "chaos":
		return runRetryChaos(flags, stdout, stderr)
	case "reset":
		return runRetryReset(flags, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n", flags.subcommand)
		return retryExitError
	}
}

// runRetryStatus prints the status of all retry policies.
func runRetryStatus(flags retryFlags, stdout, stderr io.Writer) int {
	reg := retry.DefaultRegistry()
	report := retry.BuildStatusReport(reg)

	if flags.jsonOut {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "error: marshal report: %v\n", err)
			return retryExitError
		}
		fmt.Fprintln(stdout, string(data))
	} else {
		fmt.Fprint(stdout, retry.FormatStatusTable(report))
	}
	return retryExitOK
}

// runRetryChaos simulates failures for a policy.
func runRetryChaos(flags retryFlags, stdout, stderr io.Writer) int {
	if flags.policyName == "" {
		fmt.Fprintln(stderr, "error: --policy is required for chaos")
		return retryExitError
	}

	if os.Getenv("HELIX_CHAOS_ENABLED") != "1" {
		fmt.Fprintln(stderr, "error: chaos mode requires HELIX_CHAOS_ENABLED=1 env var")
		fmt.Fprintln(stderr, "Set HELIX_CHAOS_ENABLED=1 to enable synthetic failure injection.")
		return retryExitError
	}

	reg := retry.DefaultRegistry()
	stats := reg.Get(flags.policyName)
	if stats == nil {
		fmt.Fprintf(stderr, "error: policy %q not found\n", flags.policyName)
		return retryExitError
	}

	chaos := retry.NewChaosInjector(flags.failureRate, flags.duration)
	fmt.Fprintf(stdout, "Chaos injection started for policy %q\n", flags.policyName)
	fmt.Fprintf(stdout, "  Failure rate: %.0f%%\n", flags.failureRate*100)
	fmt.Fprintf(stdout, "  Duration: %s\n", flags.duration)
	fmt.Fprintf(stdout, "  Active: %v\n", chaos.IsActive())

	// Simulate a few calls
	ctx, cancel := context.WithTimeout(context.Background(), flags.duration)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	count := 0
	failures := 0
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintf(stdout, "\nChaos injection complete: %d calls, %d injected failures\n", count, failures)
			return retryExitOK
		case <-ticker.C:
			count++
			if err := chaos.MaybeFail(); err != nil {
				failures++
				reg.RecordResult(flags.policyName, err)
			} else {
				reg.RecordResult(flags.policyName, nil)
			}
		}
	}
}

// runRetryReset clears accumulated stats.
func runRetryReset(flags retryFlags, stdout, stderr io.Writer) int {
	reg := retry.DefaultRegistry()
	if flags.policyName != "" {
		stats := reg.Get(flags.policyName)
		if stats == nil {
			fmt.Fprintf(stderr, "error: policy %q not found\n", flags.policyName)
			return retryExitError
		}
		reg.Reset(flags.policyName)
		fmt.Fprintf(stdout, "Reset stats for policy %q\n", flags.policyName)
	} else {
		reg.Reset("")
		fmt.Fprintln(stdout, "Reset stats for all policies")
	}
	return retryExitOK
}

// runRetryWithDryRun wraps runRetry with the global --dry-run flag.
func runRetryWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
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
	rc := runRetry(args, stdout, stderr)
	if rc != 0 {
		return errExit{code: rc}
	}
	return nil
}

// runRetryCapture runs retry and captures stdout/stderr as strings (for tests).
func runRetryCapture(args []string) (stdout, stderr string, rc int) {
	var out, errBuf strings.Builder
	rc = runRetry(args, &out, &errBuf)
	return out.String(), errBuf.String(), rc
}

// parseFloat parses a float64 from a string (avoiding strconv import for simplicity).
func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}
