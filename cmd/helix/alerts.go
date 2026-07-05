package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/health"
)

// ============================================================================
// helix alerts CLI — wires pkg/health.AlertEngine + Notifier (spec §8.4)
// ============================================================================

const (
	alertsExitOK       = 0
	alertsExitCritical = 1
	alertsExitError    = 2
)

// alertsFlags holds parsed CLI flags for the alerts subcommand.
type alertsFlags struct {
	metricsFile string
	dryRun      bool
	jsonOut     bool
	quiet       bool
	notifiers   []string // stdout, file, telegram
	filePath    string   // override default file path
	subcommand  string   // notify, list-rules
	timeout     time.Duration
}

// parseAlertsFlags parses the args for `helix alerts`.
func parseAlertsFlags(args []string) (alertsFlags, int) {
	var f alertsFlags
	f.subcommand = "notify"          // default
	f.notifiers = []string{"stdout"} // default to stdout
	f.timeout = 30 * time.Second

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			printAlertsHelp(os.Stdout)
			return f, alertsExitOK
		case arg == "--dry-run":
			f.dryRun = true
		case arg == "--json":
			f.jsonOut = true
		case arg == "--quiet" || arg == "-q":
			f.quiet = true
		case arg == "--metrics-file":
			i++
			if i >= len(args) {
				return f, alertsExitError
			}
			f.metricsFile = args[i]
		case arg == "--notifier":
			i++
			if i >= len(args) {
				return f, alertsExitError
			}
			f.notifiers = append(f.notifiers, args[i])
		case arg == "--file-path":
			i++
			if i >= len(args) {
				return f, alertsExitError
			}
			f.filePath = args[i]
		case arg == "--timeout":
			i++
			if i >= len(args) {
				return f, alertsExitError
			}
			d, err := time.ParseDuration(args[i])
			if err != nil {
				return f, alertsExitError
			}
			f.timeout = d
		case strings.HasPrefix(arg, "--"):
			return f, alertsExitError
		default:
			if f.subcommand == "notify" && arg != "" && !strings.HasPrefix(arg, "-") {
				f.subcommand = arg
			} else {
				return f, alertsExitError
			}
		}
		i++
	}

	return f, alertsExitOK
}

// printAlertsHelp prints the help text for the alerts subcommand.
func printAlertsHelp(w io.Writer) {
	fmt.Fprintln(w, `helix alerts — alert notification system (spec §8.4)

Usage:
  helix alerts notify [--metrics-file PATH] [--notifier NAME] [--dry-run] [--json]
  helix alerts list-rules
  helix alerts help

Subcommands:
  notify       Evaluate alerts from a metrics snapshot and dispatch to notifiers
  list-rules   List all configured alert rules
  help         Show this help

Flags:
  --metrics-file PATH   JSON file containing a MetricsSnapshot (required for notify)
  --notifier NAME       Add a notifier: stdout (default), file, telegram
  --file-path PATH      Override default file path (~/.helix/alerts.jsonl)
  --dry-run             Evaluate but don't send notifications
  --json                Output structured JSON report
  --quiet, -q           Suppress human-readable output
  --timeout DURATION    Notification timeout (default 30s)

Exit codes:
  0  No critical alerts firing
  1  At least one critical alert firing
  2  Error in invocation`)
}

// runAlerts is the entry point for `helix alerts`.
func runAlerts(args []string, stdout, stderr io.Writer) int {
	flags, rc := parseAlertsFlags(args)
	if rc != alertsExitOK {
		return rc
	}

	switch flags.subcommand {
	case "help":
		printAlertsHelp(stdout)
		return alertsExitOK
	case "list-rules":
		return runAlertsListRules(stdout)
	case "notify":
		return runAlertsNotify(flags, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n", flags.subcommand)
		return alertsExitError
	}
}

// runAlertsListRules prints all configured alert rules.
func runAlertsListRules(stdout io.Writer) int {
	engine := health.NewAlertEngine()
	cfg := engine.Config()

	rules := []struct {
		Name       string
		Severity   string
		Expression string
	}{
		{"HighCostAgent", string(health.AlertWarning), fmt.Sprintf("rate(helix_agent_cost_total[1h]) > %g", cfg.HighCostAgentThreshold)},
		{"GateFailureSpike", string(health.AlertCritical), fmt.Sprintf("rate(helix_gate_pass_rate{gate=%q}[15m]) < %g", cfg.GateFailureSpikeGate, cfg.GateFailureSpikeThreshold)},
		{"PRStuck", string(health.AlertWarning), fmt.Sprintf("helix_pr_cycle_time_seconds > %g", cfg.PRStuckThreshold)},
		{"AgentDown", string(health.AlertCritical), "helix_agent_sandbox_uptime_seconds == 0"},
		{"CostAnomaly", string(health.AlertWarning), fmt.Sprintf("helix_cost_per_pr > (avg * %g)", cfg.CostAnomalyMultiplier)},
	}

	fmt.Fprintf(stdout, "%-20s %-10s %s\n", "RULE", "SEVERITY", "EXPRESSION")
	fmt.Fprintln(stdout, strings.Repeat("-", 80))
	for _, r := range rules {
		fmt.Fprintf(stdout, "%-20s %-10s %s\n", r.Name, r.Severity, r.Expression)
	}
	fmt.Fprintf(stdout, "\n%d alert rules configured\n", len(rules))
	return alertsExitOK
}

// runAlertsNotify evaluates alerts and dispatches notifications.
func runAlertsNotify(flags alertsFlags, stdout, stderr io.Writer) int {
	if flags.metricsFile == "" {
		fmt.Fprintln(stderr, "error: --metrics-file is required for notify")
		return alertsExitError
	}

	snapshot, err := health.LoadMetricsSnapshotFromJSON(flags.metricsFile)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return alertsExitError
	}

	// Build notifiers
	var notifiers []health.Notifier
	for _, name := range flags.notifiers {
		switch name {
		case "stdout":
			notifiers = append(notifiers, health.NewStdoutNotifier(stderr))
		case "file":
			fn, err := health.NewFileNotifier(flags.filePath)
			if err != nil {
				fmt.Fprintf(stderr, "error: file notifier: %v\n", err)
				return alertsExitError
			}
			notifiers = append(notifiers, fn)
		case "telegram":
			tn := health.NewTelegramNotifier()
			if tn == nil {
				fmt.Fprintln(stderr, "warning: telegram notifier not configured (TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID required) — skipping")
				continue
			}
			notifiers = append(notifiers, tn)
		default:
			fmt.Fprintf(stderr, "error: unknown notifier %q (available: stdout, file, telegram)\n", name)
			return alertsExitError
		}
	}

	if len(notifiers) == 0 {
		fmt.Fprintln(stderr, "error: no notifiers configured")
		return alertsExitError
	}

	var notifier health.Notifier
	if len(notifiers) == 1 {
		notifier = notifiers[0]
	} else {
		notifier = health.NewMultiNotifier(notifiers...)
	}

	engine := health.NewAlertEngine()
	ne := health.NewNotifyEngine(engine, notifier).
		WithDryRun(flags.dryRun)

	ctx, cancel := context.WithTimeout(context.Background(), flags.timeout)
	defer cancel()

	report := ne.EvaluateAndNotify(ctx, snapshot)

	if flags.jsonOut {
		jsonStr, err := health.NotifyReportToJSON(report)
		if err != nil {
			fmt.Fprintf(stderr, "error: marshal report: %v\n", err)
			return alertsExitError
		}
		fmt.Fprintln(stdout, jsonStr)
	} else if !flags.quiet {
		fmt.Fprint(stdout, health.FormatNotifyReport(report))
	}

	// Exit code: 0 if no critical firing, 1 if any critical firing (and not dry-run)
	if report.Summary.HasCritical() && !flags.dryRun {
		return alertsExitCritical
	}
	return alertsExitOK
}

// runAlertsWithDryRun wraps runAlerts with the global --dry-run flag.
func runAlertsWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
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
	rc := runAlerts(args, stdout, stderr)
	if rc != 0 {
		return errExit{code: rc}
	}
	return nil
}

// ============================================================================
// Test helpers (used by alerts_test.go)
// ============================================================================

// runAlertsCapture runs alerts and captures stdout/stderr as strings (for tests).
func runAlertsCapture(args []string) (stdout, stderr string, rc int) {
	var out, errBuf strings.Builder
	rc = runAlerts(args, &out, &errBuf)
	return out.String(), errBuf.String(), rc
}

// encodeAlertsSnapshot writes a MetricsSnapshot as JSON to a temp file for testing.
func encodeAlertsSnapshot(t *testing.T, snap health.MetricsSnapshot) string {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/metrics.json"
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
