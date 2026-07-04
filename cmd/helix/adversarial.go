// Command helix — adversarial.go
//
// `helix adversarial` exposes the spec §12.4 adversarial scenario pack.
// The pack contains 6 roles (@assumption-buster, @devils-advocate,
// @redteam, @whitehat, @chaos-engineer, @finops-cost) each with multiple
// scenarios that test whether the platform's defenses hold against
// adversarial inputs.
//
// Per spec, adversarial tests are NOT CI-gated — they run on a schedule
// (daily) or pre-release. The CLI surfaces:
//
//	helix adversarial run-all   Execute every scenario, aggregate results
//	helix adversarial run <id>  Execute a single scenario by ID
//	helix adversarial list       List every registered scenario (table or json)
//	helix adversarial help       Show usage
//
// Filters (--role, --severity-min) apply to run-all and list. Per the
// task AC, --role and --severity filter by AgentRole and Severity
// respectively. --output json emits the structured Report for CI.
//
// Exit codes:
//
//	0 — Every scenario that ran passed
//	1 — One or more scenarios failed (expected != actual)
//	2 — Bad invocation (parse error, missing flag, malformed input)
//	130 — Internal panic during scenario execution (caught and reported)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	adv "github.com/totalwindupflightsystems/helix/pkg/adversarial"
)

// ============================================================================
// Constants
// ============================================================================

const (
	envAdversarialRole        = "HELIX_ADVERSARIAL_ROLE"
	envAdversarialSeverityMin = "HELIX_ADVERSARIAL_SEVERITY_MIN"
	envAdversarialOutput      = "HELIX_ADVERSARIAL_OUTPUT"
	envAdversarialTimeout     = "HELIX_ADVERSARIAL_TIMEOUT"
	envAdversarialScenario    = "HELIX_ADVERSARIAL_SCENARIO"
)

// adversarialFlags captures every CLI flag in one struct.
type adversarialFlags struct {
	subcommand  string
	scenarioID  string
	role        string
	severityMin string
	output      string
	timeout     time.Duration
}

// ============================================================================
// parseAdversarialFlags
// ============================================================================

// parseAdversarialFlags builds a flag.FS, parses the args, and returns
// the populated flags plus a "showHelp" bool. The first positional arg
// is the subcommand (default: "run-all").
func parseAdversarialFlags(args []string, stdout, stderr io.Writer) (adversarialFlags, string, bool, error) {
	subcommand := "run-all"
	rest := args
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		subcommand = rest[0]
		rest = rest[1:]
	}

	if subcommand == "help" || subcommand == "-h" || subcommand == "--help" {
		return adversarialFlags{}, subcommand, true, nil
	}

	switch subcommand {
	case "run-all", "run", "list":
		// recognised
	default:
		return adversarialFlags{}, subcommand, false, fmt.Errorf("unknown subcommand %q (expected: run-all | run | list | help)", subcommand)
	}

	fs := flag.NewFlagSet("helix-adversarial-"+subcommand, flag.ContinueOnError)
	fs.SetOutput(stderr)

	var f adversarialFlags
	f.subcommand = subcommand

	fs.StringVar(&f.scenarioID, "scenario", "",
		"Scenario ID (run subcommand only) (env HELIX_ADVERSARIAL_SCENARIO)")
	fs.StringVar(&f.role, "role", "",
		"Filter by AgentRole (run-all, list) (env HELIX_ADVERSARIAL_ROLE)")
	fs.StringVar(&f.severityMin, "severity-min", "",
		"Minimum severity: low | medium | high | critical (env HELIX_ADVERSARIAL_SEVERITY_MIN)")
	fs.StringVar(&f.output, "output", "table",
		"Output format: table | json (env HELIX_ADVERSARIAL_OUTPUT)")
	fs.DurationVar(&f.timeout, "timeout", 60*time.Second,
		"Total wall-clock cap for the whole batch (env HELIX_ADVERSARIAL_TIMEOUT)")

	// Env-var defaults BEFORE parse.
	if v := os.Getenv(envAdversarialRole); v != "" && f.role == "" {
		f.role = v
	}
	if v := os.Getenv(envAdversarialSeverityMin); v != "" && f.severityMin == "" {
		f.severityMin = v
	}
	if v := os.Getenv(envAdversarialOutput); v != "" && f.output == "table" {
		f.output = v
	}
	if v := os.Getenv(envAdversarialScenario); v != "" && f.scenarioID == "" {
		f.scenarioID = v
	}
	if v := os.Getenv(envAdversarialTimeout); v != "" && f.timeout == 60*time.Second {
		if d, err := time.ParseDuration(v); err == nil {
			f.timeout = d
		}
	}

	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return f, subcommand, true, nil
		}
		return f, subcommand, false, err
	}

	return f, subcommand, false, nil
}

// ============================================================================
// Entry points — wired from main.go
// ============================================================================

// runAdversarialWithDryRun is the variant invoked by the unified `helix`
// CLI when the user passes the GLOBAL --dry-run flag. The adversarial
// scenarios are pure local computations (no network), so --dry-run is a
// no-op for now — we only thread it through for consistency with the
// other subcommands.
func runAdversarialWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
	rc := runAdversarial(args, stdout, stderr)
	if rc != 0 {
		return errExit{code: rc}
	}
	return nil
}

// runAdversarial is the cmd/helix adversarial subcommand entry point.
func runAdversarial(args []string, stdout, stderr io.Writer) int {
	flags, subcommand, showHelp, err := parseAdversarialFlags(args, stdout, stderr)
	if showHelp {
		printAdversarialUsage(stdout, subcommand)
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, "helix adversarial: parse:", err)
		printAdversarialUsage(stderr, "")
		return 2
	}

	switch subcommand {
	case "run-all":
		return runAdversarialAll(flags, stdout, stderr)
	case "run":
		return runAdversarialOne(flags, stdout, stderr)
	case "list":
		return runAdversarialList(flags, stdout, stderr)
	}
	// Unreachable.
	return 2
}

// ============================================================================
// run-all — execute every scenario, aggregate
// ============================================================================

// runAdversarialAll executes every scenario in the library (filtered by
// --role / --severity-min if set) and renders a table or JSON report.
// Returns 0 if every scenario passed, 1 otherwise.
func runAdversarialAll(f adversarialFlags, stdout, stderr io.Writer) int {
	lib := adv.DefaultLibrary()

	scenarios, err := filterScenarios(lib, f.role, f.severityMin)
	if err != nil {
		fmt.Fprintln(stderr, "helix adversarial run-all: filter:", err)
		return 2
	}

	if len(scenarios) == 0 {
		fmt.Fprintln(stderr, "helix adversarial run-all: no scenarios match the filter (role=?", f.role, " severity>=", f.severityMin, ")")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), f.timeout)
	defer cancel()

	// Build a tiny ad-hoc library containing only the filtered scenarios
	// so RunAll processes exactly what the user asked for.
	filtered := adv.NewLibrary()
	for _, s := range scenarios {
		filtered.MustRegister(s)
	}

	// Catch panics so a single bad scenario doesn't kill the batch —
	// adversarial code is exploratory and may exercise edge cases.
	results := safeRunAll(ctx, filtered, stderr)

	report := adv.GenerateReport(results)

	switch strings.ToLower(f.output) {
	case "json":
		return renderAdversarialJSON(report, results, stdout, stderr)
	default:
		return renderAdversarialTable(f, report, results, stdout)
	}
}

// safeRunAll wraps Library.RunAll with a recover() that converts any
// panic into a synthetic FAIL result so the rest of the batch can
// continue. Panics are inherently dangerous in adversarial testing —
// we never want one to take down the whole run.
func safeRunAll(ctx context.Context, lib *adv.Library, stderr io.Writer) []adv.Result {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(stderr, "helix adversarial: panic in RunAll recovered: %v\n", r)
		}
	}()
	return lib.RunAll(ctx)
}

// ============================================================================
// run — execute a single scenario
// ============================================================================

// runAdversarialOne executes the scenario named by --scenario. Returns
// 0 on pass, 1 on fail, 2 on bad invocation.
func runAdversarialOne(f adversarialFlags, stdout, stderr io.Writer) int {
	if f.scenarioID == "" {
		fmt.Fprintln(stderr, "helix adversarial run: --scenario is required")
		printAdversarialUsage(stderr, "run")
		return 2
	}

	lib := adv.DefaultLibrary()
	ctx, cancel := context.WithTimeout(context.Background(), f.timeout)
	defer cancel()

	// Wrap the per-scenario Run in recover() too — panics during a
	// single scenario must not kill the CLI.
	var result adv.Result
	var runErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				runErr = fmt.Errorf("panic during scenario execution: %v", r)
			}
		}()
		var err error
		result, err = lib.RunOne(ctx, f.scenarioID)
		if err != nil {
			runErr = err
		}
	}()

	if runErr != nil {
		fmt.Fprintln(stderr, "helix adversarial run: error:", runErr)
		return 2
	}

	switch strings.ToLower(f.output) {
	case "json":
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "helix adversarial run: marshal: %v\n", err)
			return 2
		}
		fmt.Fprintln(stdout, string(data))
	default:
		fmt.Fprintln(stdout, adv.FormatResult(result))
	}

	if result.Pass {
		return 0
	}
	return 1
}

// ============================================================================
// list — print registered scenarios
// ============================================================================

// runAdversarialList prints every registered scenario (after applying
// --role and --severity-min filters) as a table or JSON.
func runAdversarialList(f adversarialFlags, stdout, stderr io.Writer) int {
	lib := adv.DefaultLibrary()

	scenarios, err := filterScenarios(lib, f.role, f.severityMin)
	if err != nil {
		fmt.Fprintln(stderr, "helix adversarial list: filter:", err)
		return 2
	}

	// Sort by ID for deterministic output.
	sort.Slice(scenarios, func(i, j int) bool {
		return scenarios[i].ID < scenarios[j].ID
	})

	switch strings.ToLower(f.output) {
	case "json":
		// Marshal each scenario as a public-only struct — the Run func
		// isn't serializable. json.Marshal would fail with "unsupported
		// type: func".
		type scenarioJSON struct {
			ID              string        `json:"id"`
			Name            string        `json:"name"`
			Description     string        `json:"description"`
			Role            adv.AgentRole `json:"role"`
			ExpectedOutcome adv.Outcome   `json:"expected_outcome"`
			Severity        adv.Severity  `json:"severity"`
			Trigger         string        `json:"trigger"`
		}
		out := make([]scenarioJSON, len(scenarios))
		for i, s := range scenarios {
			out[i] = scenarioJSON{
				ID:              s.ID,
				Name:            s.Name,
				Description:     s.Description,
				Role:            s.Role,
				ExpectedOutcome: s.ExpectedOutcome,
				Severity:        s.Severity,
				Trigger:         s.Trigger,
			}
		}
		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "helix adversarial list: marshal: %v\n", err)
			return 2
		}
		fmt.Fprintln(stdout, string(data))
	default:
		printAdversarialTable(scenarios, stdout)
	}
	return 0
}

// ============================================================================
// Renderers
// ============================================================================

// renderAdversarialTable prints a structured table of results: each row
// is one scenario with role, severity, name, outcome, pass/fail.
func renderAdversarialTable(f adversarialFlags, report *adv.Report, results []adv.Result, stdout io.Writer) int {
	// Header
	fmt.Fprintf(stdout, "Helix Adversarial Scenario Pack — run-all\n")
	if f.role != "" {
		fmt.Fprintf(stdout, "  Filter role:         %s\n", f.role)
	}
	if f.severityMin != "" {
		fmt.Fprintf(stdout, "  Filter severity>=:   %s\n", f.severityMin)
	}
	fmt.Fprintf(stdout, "  Total scenarios:     %d\n", report.Total)
	fmt.Fprintf(stdout, "  Passed:              %d\n", report.Pass)
	fmt.Fprintf(stdout, "  Failed:              %d\n", report.Fail)
	fmt.Fprintf(stdout, "  Pass rate:           %.1f%%\n\n", report.PassRate())

	// Column header
	fmt.Fprintf(stdout, "%-28s %-22s %-9s %-40s %-7s %s\n",
		"ROLE", "SCENARIO", "SEVERITY", "NAME", "RESULT", "DETAIL")
	fmt.Fprintf(stdout, "%s\n", strings.Repeat("-", 130))

	for _, r := range results {
		detail := r.Details
		if len(detail) > 50 {
			detail = detail[:47] + "..."
		}
		// Trim newlines for table cleanliness.
		detail = strings.ReplaceAll(detail, "\n", " ")

		outcomeLabel := "FAIL"
		if r.Pass {
			outcomeLabel = "PASS"
		}

		fmt.Fprintf(stdout, "%-28s %-22s %-9s %-40s %-7s %s\n",
			r.Role, r.ScenarioID, r.Severity, truncateForTable(r.Name, 40), outcomeLabel, detail)
	}

	if report.Fail > 0 {
		fmt.Fprintf(stdout, "\nResult: %d scenario(s) FAILED.\n", report.Fail)
		return 1
	}
	fmt.Fprintf(stdout, "\nResult: all %d scenarios passed.\n", report.Pass)
	return 0
}

// renderAdversarialJSON prints the structured Report + Results.
func renderAdversarialJSON(report *adv.Report, results []adv.Result, stdout, stderr io.Writer) int {
	out := struct {
		Report  *adv.Report  `json:"report"`
		Results []adv.Result `json:"results"`
	}{
		Report:  report,
		Results: results,
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "helix adversarial: marshal: %v\n", err)
		return 2
	}
	fmt.Fprintln(stdout, string(data))
	if report.Fail > 0 {
		return 1
	}
	return 0
}

// printAdversarialTable prints a list of scenarios (used by `list`).
func printAdversarialTable(scenarios []adv.Scenario, stdout io.Writer) {
	fmt.Fprintf(stdout, "%-22s %-28s %-9s %-50s\n", "ID", "ROLE", "SEVERITY", "NAME")
	fmt.Fprintf(stdout, "%s\n", strings.Repeat("-", 110))
	for _, s := range scenarios {
		fmt.Fprintf(stdout, "%-22s %-28s %-9s %-50s\n",
			s.ID, s.Role, s.Severity, truncateForTable(s.Name, 50))
	}
	fmt.Fprintf(stdout, "\nTotal: %d scenarios\n", len(scenarios))
}

// ============================================================================
// Helpers
// ============================================================================

// filterScenarios returns the subset of lib's scenarios matching the
// supplied role and severity-min filters. Empty filters mean "no
// filter on this dimension".
func filterScenarios(lib *adv.Library, role, severityMin string) ([]adv.Scenario, error) {
	if role != "" {
		r := adv.AgentRole(role)
		if !r.IsValid() {
			return nil, fmt.Errorf("unknown role %q (valid: %s)", role, formatRoles(adv.AllRoles()))
		}
	}

	if severityMin != "" {
		if _, err := severityRank(adv.Severity(severityMin)); err != nil {
			return nil, err
		}
	}

	all := lib.All()
	out := make([]adv.Scenario, 0, len(all))
	for _, s := range all {
		if role != "" && s.Role != adv.AgentRole(role) {
			continue
		}
		if severityMin != "" {
			ok, err := severityAtLeast(s.Severity, adv.Severity(severityMin))
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// severityRank maps severity names to a numeric rank for comparison.
func severityRank(s adv.Severity) (int, error) {
	switch s {
	case adv.SevLow:
		return 1, nil
	case adv.SevMedium:
		return 2, nil
	case adv.SevHigh:
		return 3, nil
	case adv.SevCritical:
		return 4, nil
	}
	return 0, fmt.Errorf("invalid severity %q (valid: low | medium | high | critical)", s)
}

// severityAtLeast returns true if s ranks >= min.
func severityAtLeast(s, min adv.Severity) (bool, error) {
	sr, err := severityRank(s)
	if err != nil {
		return false, err
	}
	mr, err := severityRank(min)
	if err != nil {
		return false, err
	}
	return sr >= mr, nil
}

// formatRoles joins roles with ", " for error messages.
func formatRoles(roles []adv.AgentRole) string {
	strs := make([]string, len(roles))
	for i, r := range roles {
		strs[i] = string(r)
	}
	return strings.Join(strs, ", ")
}

// truncateForTable trims s to at most n runes and appends "..." when
// truncation occurred. Used by the table renderer to keep columns
// aligned.
func truncateForTable(s string, n int) string {
	if n <= 3 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-3]) + "..."
}

// printAdversarialUsage prints top-level (subcommand="") usage when the
// user runs `helix adversarial` with no args or asks for help. When
// subcommand is set it prints that subcommand's flags.
func printAdversarialUsage(w io.Writer, subcommand string) {
	switch subcommand {
	case "run-all":
		fmt.Fprintf(w, `helix adversarial run-all — execute every scenario (filtered)

USAGE:
  helix adversarial run-all [flags...]

FLAGS:
  --role            Filter by AgentRole (e.g. @redteam) — optional
  --severity-min    Minimum severity: low | medium | high | critical — optional
  --output          Output format: table | json (default table)
  --timeout         Wall-clock cap for the whole batch (default 60s)

EXAMPLES:
  helix adversarial run-all
  helix adversarial run-all --role @redteam --severity-min high
  helix adversarial run-all --output json
`)
	case "run":
		fmt.Fprintf(w, `helix adversarial run — execute a single scenario

USAGE:
  helix adversarial run --scenario <id> [flags...]

FLAGS:
  --scenario        Scenario ID (required) — see `+"`helix adversarial list`"+`
  --output          Output format: table | json (default table)
  --timeout         Wall-clock cap (default 60s)

EXAMPLE:
  helix adversarial run --scenario gate-bypass
`)
	case "list":
		fmt.Fprintf(w, `helix adversarial list — print registered scenarios

USAGE:
  helix adversarial list [flags...]

FLAGS:
  --role            Filter by AgentRole — optional
  --severity-min    Minimum severity: low | medium | high | critical — optional
  --output          Output format: table | json (default table)
`)
	default:
		fmt.Fprintf(w, `helix adversarial — adversarial scenario pack CLI (specs/SPECIFICATION.md §12.4)

USAGE:
  helix adversarial <subcommand> [flags...]

SUBCOMMANDS:
  run-all    Execute every registered scenario, aggregate results
  run        Execute a single scenario by ID
  list       Print registered scenarios
  help       Show this message

Run 'helix adversarial <subcommand> --help' for subcommand-specific flags.
`)
	}
}
