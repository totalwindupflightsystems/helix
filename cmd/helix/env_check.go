// Command helix — env_check.go
//
// `helix config env-check` wires `pkg/config.EnvInventory` (spec §9.6) to a
// CLI subcommand. It validates that every documented platform env var is
// present and non-empty, supporting multiple source-resolution strategies:
//
//	helix config env-check                       (use process env + ~/.helix/.env fallback)
//	helix config env-check --env-file PATH       (explicit .env file)
//	helix config env-check --source forgejo      (filter to one source group)
//	helix config env-check --json                (structured JSON output)
//	helix config env-check --strict              (default; empty = missing)
//
// Exit codes:
//
//	0 — all required vars present (non-empty in strict mode)
//	1 — one or more required vars missing
//	2 — invalid invocation (bad env-file path, bad source, parse error)
//
// Source precedence (highest wins):
//
//  1. process env (os.Getenv) — always consulted
//  2. explicit --env-file, then HELIX_ENV_FILE
//  3. /opt/helix/.env, then $HOME/.helix/.env (defaults)
//
// Values that look like secrets are auto-redacted in output via
// `pkg/config.redactIfSecret`.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/config"
)

// envCheckFlags holds the parsed flags for `helix config env-check`.
type envCheckFlags struct {
	help    bool
	jsonOut bool
	strict  bool // default true: empty values count as missing
	envFile string
	source  string // optional filter — only check vars with matching service
}

// defaultEnvFileCandidates is the fallback order when --env-file is unset.
// Matches the convention in pkg/config/loader.go and deploy/.
func defaultEnvFileCandidates() []string {
	out := []string{}
	if v := os.Getenv("HELIX_ENV_FILE"); v != "" {
		out = append(out, v)
	}
	if h, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(h, ".helix", ".env"))
	}
	out = append(out, "/opt/helix/.env")
	return out
}

// parseEnvCheckFlags parses the `helix config env-check` subcommand flags.
// The first non-flag arg is treated as a sub-subcommand ("help" only).
func parseEnvCheckFlags(args []string, stdout, stderr io.Writer) (envCheckFlags, error) {
	fs := flag.NewFlagSet("env-check", flag.ContinueOnError)
	fs.SetOutput(stderr)

	f := envCheckFlags{strict: true}
	fs.BoolVar(&f.help, "help", false, "Print usage")
	fs.BoolVar(&f.jsonOut, "json", false, "Emit structured JSON output")
	fs.BoolVar(&f.strict, "strict", true, "Treat empty values as missing")
	fs.StringVar(&f.envFile, "env-file", "", "Explicit .env file path (overrides HELIX_ENV_FILE and defaults)")
	fs.StringVar(&f.source, "source", "", "Filter to a single service group (e.g. forgejo, langfuse)")

	if err := fs.Parse(args); err != nil {
		return f, err
	}
	// Treat "help" as the only recognised positional arg.
	if rest := fs.Args(); len(rest) > 0 {
		if rest[0] == "help" {
			f.help = true
		} else {
			return f, fmt.Errorf("unknown positional arg %q (only 'help' is recognised)", rest[0])
		}
	}
	return f, nil
}

// resolveEnvFile returns the .env file path to use, honouring
// --env-file > HELIX_ENV_FILE > ~/.helix/.env > /opt/helix/.env.
// Returns "" if no candidate exists (process env only).
func resolveEnvFile(explicit string) string {
	if explicit != "" {
		return explicit
	}
	for _, p := range defaultEnvFileCandidates() {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// buildLoader returns the EnvLoader to use. When envFile is non-empty,
// wraps config.DotEnvLoader around it; otherwise returns nil (process env only).
func buildLoader(envFile string) config.EnvLoader {
	if envFile == "" {
		return nil
	}
	return config.DotEnvLoader{Path: envFile}
}

// filterByService returns the subset of vars matching the requested service.
// Unknown services yield an empty slice and a non-empty error.
func filterByService(vars []config.EnvVar, source string) ([]config.EnvVar, error) {
	if source == "" {
		return vars, nil
	}
	known := map[string]bool{}
	for _, v := range vars {
		known[v.Service] = true
	}
	if !known[source] {
		keys := make([]string, 0, len(known))
		for k := range known {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("unknown source %q (known: %s)", source, strings.Join(keys, ", "))
	}
	out := make([]config.EnvVar, 0, len(vars))
	for _, v := range vars {
		if v.Service == source {
			out = append(out, v)
		}
	}
	return out, nil
}

// envCheckReport is the structured JSON output shape for --json.
type envCheckReport struct {
	Total      int               `json:"total"`
	Present    int               `json:"present"`
	Missing    []envCheckMissing `json:"missing"`
	MissingCnt int               `json:"missing_count"`
	EnvFile    string            `json:"env_file"`
	Source     string            `json:"source,omitempty"`
	Strict     bool              `json:"strict"`
	HasMissing bool              `json:"has_missing"`
}

type envCheckMissing struct {
	Name        string `json:"name"`
	Service     string `json:"service"`
	Description string `json:"description"`
}

// buildJSONReport renders the structured --json payload from an inventory
// result.
func buildJSONReport(rpt config.InventoryReport, envFile, source string, strict bool) envCheckReport {
	out := envCheckReport{
		Total:      rpt.Total,
		Present:    rpt.Present,
		Missing:    make([]envCheckMissing, 0, len(rpt.Missing)),
		EnvFile:    envFile,
		Source:     source,
		Strict:     strict,
		HasMissing: rpt.HasMissing,
	}
	for _, m := range rpt.Missing {
		out.Missing = append(out.Missing, envCheckMissing{
			Name:        m.Var.Name,
			Service:     m.Var.Service,
			Description: m.Var.Description,
		})
	}
	out.MissingCnt = len(out.Missing)
	return out
}

// formatTable renders the human-readable report.
func formatTable(envvars []config.EnvVar, rpt config.InventoryReport, envFile, source string, strict bool) string {
	groups := config.GroupByService(envvars)
	var b strings.Builder

	fmt.Fprintf(&b, "Helix Environment Variable Inventory\n")
	fmt.Fprintf(&b, "===================================\n\n")
	if envFile != "" {
		fmt.Fprintf(&b, "Source file : %s\n", envFile)
	} else {
		fmt.Fprintf(&b, "Source file : (process env only — no .env fallback found)\n")
	}
	if source != "" {
		fmt.Fprintf(&b, "Filter      : service=%s\n", source)
	}
	fmt.Fprintf(&b, "Strict mode : %v (empty values count as missing)\n\n", strict)

	for _, g := range groups {
		fmt.Fprintf(&b, "[%s]\n", g.Service)
		for _, v := range g.Vars {
			rr := v.HasValue(nil, buildLoader(envFile))
			// Also honour process env (HasValue does, but our local builder
			// may not — re-run through the engine's combined path).
			combined := validateSingle(v, envFile)
			_ = rr
			status := "✓"
			detail := ""
			if combined.Present {
				detail = combined.Value
				if combined.IsDefault {
					detail += " (default)"
				}
			} else if v.Required {
				status = "✗"
				detail = "MISSING"
			} else {
				status = "·"
				detail = "not set (optional)"
			}
			fmt.Fprintf(&b, "  %s %-32s %s\n", status, v.Name, detail)
		}
		fmt.Fprintln(&b)
	}

	fmt.Fprintf(&b, "Summary: %d/%d present", rpt.Present, rpt.Total)
	if rpt.HasMissing {
		fmt.Fprintf(&b, ", %d missing (required)", len(rpt.Missing))
	}
	fmt.Fprintln(&b)

	if rpt.HasMissing {
		fmt.Fprintln(&b, "\nMissing required variables:")
		names := make([]string, 0, len(rpt.Missing))
		for _, m := range rpt.Missing {
			names = append(names, m.Var.Name)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintf(&b, "  - %s\n", n)
		}
	}
	return b.String()
}

// validateSingle checks a single EnvVar through the engine's combined
// (process env + loader) pipeline. The package's EnvVar.HasValue consults
// the supplied `env` map first then the loader; process env is read by
// DotEnvLoader's callers via separate path. Easiest: build a process-env
// map once and pass it.
func validateSingle(v config.EnvVar, envFile string) config.EnvVarReport {
	env := processEnvMap()
	loader := buildLoader(envFile)
	return v.HasValue(env, loader)
}

// processEnvMap reads os.Environ() into a map. We do this once per run.
func processEnvMap() map[string]string {
	out := make(map[string]string, 64)
	for _, kv := range os.Environ() {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		out[kv[:eq]] = kv[eq+1:]
	}
	return out
}

// runConfig is the cmd/helix config entry point. Dispatches to the
// appropriate subcommand (currently only `env-check`).
// Returns 0 on success, non-zero on error.
func runConfig(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "helix config: missing subcommand")
		printConfigUsage(stderr)
		return 2
	}
	switch args[0] {
	case "env-check":
		return runEnvCheck(args[1:], stdout, stderr)
	case "help", "--help", "-h":
		printConfigUsage(stdout)
		return 0
	}
	fmt.Fprintf(stderr, "helix config: unknown subcommand %q\n", args[0])
	printConfigUsage(stderr)
	return 2
}

// printConfigUsage lists the available `helix config <subcommand>` entries.
func printConfigUsage(w io.Writer) {
	fmt.Fprintf(w, `helix config — configuration validation utilities

Usage:
  helix config <subcommand> [flags]

Subcommands:
  env-check    Validate platform env vars against spec §9.6 inventory
  help         Print this help

Examples:
  helix config env-check
  helix config env-check --env-file /opt/helix/.env --json
`)
}

// runEnvCheck is the cmd/helix config env-check entry point.
// Returns 0 on success (all required present), 1 if any missing, 2 on bad invocation.
func runEnvCheck(args []string, stdout, stderr io.Writer) int {
	flags, err := parseEnvCheckFlags(args, stdout, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(stderr, "helix config env-check: parse:", err)
		printEnvCheckUsage(stderr)
		return 2
	}
	if flags.help {
		printEnvCheckUsage(stdout)
		return 0
	}

	envFile := resolveEnvFile(flags.envFile)
	// If user explicitly passed --env-file but it doesn't exist, that's
	// a usage error (exit 2). Otherwise the silent fallback is fine.
	if flags.envFile != "" {
		if _, err := os.Stat(flags.envFile); err != nil {
			fmt.Fprintf(stderr, "helix config env-check: --env-file %s not found: %v\n", flags.envFile, err)
			return 2
		}
	}

	loader := buildLoader(envFile)
	env := processEnvMap()
	// Empty-string handling: in strict mode, scrub empty values from the
	// env map so HasValue treats them as absent.
	if flags.strict {
		for k, v := range env {
			if v == "" {
				delete(env, k)
			}
		}
	}

	vars, err := filterByService(config.DefaultEnvVars(), flags.source)
	if err != nil {
		fmt.Fprintln(stderr, "helix config env-check:", err)
		return 2
	}

	rpt := config.ValidateEnvVars(vars, env, loader)

	if flags.jsonOut {
		out := buildJSONReport(rpt, envFile, flags.source, flags.strict)
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintln(stderr, "helix config env-check: json:", err)
			return 2
		}
	} else {
		fmt.Fprint(stdout, formatTable(vars, rpt, envFile, flags.source, flags.strict))
	}

	if rpt.HasMissing {
		return 1
	}
	return 0
}

// printEnvCheckUsage emits the subcommand help text.
func printEnvCheckUsage(w io.Writer) {
	fmt.Fprintf(w, `helix config env-check — validate platform environment variables (spec §9.6)

Usage:
  helix config env-check [flags]

Flags:
  --env-file PATH   Explicit .env file (overrides HELIX_ENV_FILE + defaults)
  --source NAME     Filter to one service group (forgejo, langfuse, ...)
  --strict          Treat empty values as missing (default: on)
  --json            Emit structured JSON instead of a table
  --help            Show this help

Source precedence (highest wins):
  1. process env
  2. --env-file, then HELIX_ENV_FILE, then ~/.helix/.env, then /opt/helix/.env

Exit codes:
  0  All required vars present
  1  One or more required vars missing
  2  Invalid invocation (bad env-file path, unknown source, parse error)

Examples:
  helix config env-check
  helix config env-check --env-file /opt/helix/.env
  helix config env-check --source forgejo
  helix config env-check --json | jq '.missing[]'
`)
}
