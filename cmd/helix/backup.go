package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/backup"
)

// ============================================================================
// helix backup CLI — wraps pkg/backup.BackupManager (spec §10.1)
// ============================================================================

const (
	backupExitOK    = 0
	backupExitError = 2
)

// backupFlags holds parsed CLI flags for the backup subcommand.
type backupFlags struct {
	subcommand string // status, validate, help
	jsonOut    bool
	dryRun     bool
}

// parseBackupFlags parses the args for `helix backup`.
func parseBackupFlags(args []string) (backupFlags, int) {
	var f backupFlags
	f.subcommand = "status" // default

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			printBackupHelp(os.Stdout)
			return f, backupExitOK
		case arg == "--json":
			f.jsonOut = true
		case arg == "--dry-run":
			f.dryRun = true
		case strings.HasPrefix(arg, "--"):
			return f, backupExitError
		default:
			if f.subcommand == "status" && arg != "" && !strings.HasPrefix(arg, "-") {
				f.subcommand = arg
			} else {
				return f, backupExitError
			}
		}
		i++
	}

	return f, backupExitOK
}

// printBackupHelp prints the help text for the backup subcommand.
func printBackupHelp(w io.Writer) {
	fmt.Fprintln(w, `helix backup — backup strategy observability (spec §10.1)

Usage:
  helix backup status [--json]
  helix backup validate [--json]
  helix backup help

Subcommands:
  status    Show all backup targets and their configuration
  validate  Check backup compliance (path existence + freshness)
  help      Show this help

Flags:
  --json     Output structured JSON report
  --dry-run  No effect (backup is read-only)

Exit codes:
  0  Success
  2  Error in invocation`)
}

// runBackup is the entry point for `helix backup`.
func runBackup(args []string, stdout, stderr io.Writer) int {
	flags, rc := parseBackupFlags(args)
	if rc != backupExitOK {
		return rc
	}

	switch flags.subcommand {
	case "help":
		printBackupHelp(stdout)
		return backupExitOK
	case "status":
		return runBackupStatus(flags, stdout, stderr)
	case "validate":
		return runBackupValidate(flags, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n", flags.subcommand)
		return backupExitError
	}
}

// runBackupStatus prints all configured backup targets.
func runBackupStatus(flags backupFlags, stdout, stderr io.Writer) int {
	mgr := backup.NewBackupManager()
	targets := mgr.Targets()

	if flags.jsonOut {
		report := map[string]interface{}{
			"targets": targets,
			"count":   mgr.Count(),
		}
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return backupExitError
		}
		fmt.Fprintln(stdout, string(data))
		return backupExitOK
	}

	// Table output
	fmt.Fprintf(stdout, "%-35s %-20s %-10s %-12s\n", "PATH", "CONTENT", "FREQ", "RETENTION")
	fmt.Fprintln(stdout, strings.Repeat("-", 80))
	for _, t := range targets {
		fmt.Fprintf(stdout, "%-35s %-20s %-10s %-12s\n",
			truncateStr(t.Path, 35),
			truncateStr(t.Content, 20),
			t.Frequency,
			t.Retention,
		)
	}
	fmt.Fprintf(stdout, "\n%d backup targets configured (spec §10.1)\n", mgr.Count())
	return backupExitOK
}

// runBackupValidate checks backup compliance.
func runBackupValidate(flags backupFlags, stdout, stderr io.Writer) int {
	mgr := backup.NewBackupManager()

	// In dry-run mode, use empty lastBackups (just check path existence)
	lastBackups := make(map[string]time.Time)
	results := mgr.Validate(lastBackups)

	if flags.jsonOut {
		report := map[string]interface{}{
			"results": results,
			"healthy": countHealthy(results),
			"total":   len(results),
		}
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return backupExitError
		}
		fmt.Fprintln(stdout, string(data))
		return backupExitOK
	}

	// Table output
	fmt.Fprintf(stdout, "%-35s %-8s %-8s %-20s\n", "PATH", "EXISTS", "FRESH", "ISSUE")
	fmt.Fprintln(stdout, strings.Repeat("-", 75))
	healthy := 0
	for _, r := range results {
		exists := "no"
		if r.PathExists {
			exists = "yes"
		}
		fresh := "no"
		if r.Fresh {
			fresh = "yes"
		}
		issue := r.Issue
		if issue == "" && r.IsHealthy() {
			issue = "OK"
			healthy++
		}
		fmt.Fprintf(stdout, "%-35s %-8s %-8s %-20s\n",
			truncateStr(r.Target.Path, 35),
			exists,
			fresh,
			truncateStr(issue, 20),
		)
	}
	fmt.Fprintf(stdout, "\n%d/%d targets healthy\n", healthy, len(results))
	return backupExitOK
}

// runBackupWithDryRun wraps runBackup with the global --dry-run flag.
func runBackupWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
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
	rc := runBackup(args, stdout, stderr)
	if rc != 0 {
		return errExit{code: rc}
	}
	return nil
}

// countHealthy counts how many validation results are healthy.
func countHealthy(results []backup.ValidationResult) int {
	count := 0
	for _, r := range results {
		if r.IsHealthy() {
			count++
		}
	}
	return count
}

// truncateStr truncates a string to maxLen, adding "..." if truncated.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
