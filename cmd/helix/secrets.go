// Command helix — secrets.go
//
// `helix secrets scan` wires the pkg/security/secrets.Scanner behind a CLI
// interface per specs/SPECIFICATION.md §13.2 (Secrets Scanning). It walks a
// file or directory, runs the scanner over every text file, and renders
// findings to stdout (table or JSON). Exit code is 0 if clean, 1 if any
// findings, 2 for invocation errors (mirrors `helix doctor` / `helix
// adversarial`).
//
// Subcommands:
//
//	helix secrets scan <path>    Scan a file or directory
//	helix secrets list-rules     Print every detector rule with its severity
//	helix secrets help           Print usage
//
// Flags for `scan`:
//
//	--exclude <glob>     Skip files matching this glob (repeatable)
//	--min-severity LVL   Filter out findings below this severity
//	                     (low | med | high | critical, default = low)
//	--format FORMAT      "table" (default) or "json"
//	--quiet              Emit NO findings lines; just the summary header.
//	                     Useful for CI when only the exit code is wanted.
//
// `list-rules` always emits JSON; there is no table form.
//
// The scanner itself already walks directories, suppresses allowlisted
// matches, and skips `.git/`, `.venv/`, `node_modules/`. The CLI adds the
// user-facing knobs above and the file-glob exclusion filter so ops can
// quickly drop noisy directories.
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

	"github.com/totalwindupflightsystems/helix/pkg/security/secrets"
)

// =============================================================================
// Severity
// =============================================================================

// severityLevel matches specs/SPECIFICATION.md §6.2 severity banding.
// Lower numbers = more severe. We keep numeric ordering so callers can
// write `if f.severityRank() >= min.rank()` without string compares.
type severityLevel string

const (
	sevLow      severityLevel = "low"
	sevMed      severityLevel = "med"
	sevHigh     severityLevel = "high"
	sevCritical severityLevel = "critical"
)

// rank returns 0 (low) through 3 (critical). Unknown severity defaults to
// high — better to over-report than to miss a real key.
func (s severityLevel) rank() int {
	switch s {
	case sevLow:
		return 0
	case sevMed:
		return 1
	case sevHigh:
		return 2
	case sevCritical:
		return 3
	}
	return 2
}

func parseSeverity(s string) (severityLevel, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "low":
		return sevLow, nil
	case "med", "medium":
		return sevMed, nil
	case "high":
		return sevHigh, nil
	case "critical", "crit":
		return sevCritical, nil
	}
	return "", fmt.Errorf("invalid severity %q (expected: low | med | high | critical)", s)
}

// severityForRule maps each NamedPattern name to a severity. The catalog is
// stable — see pkg/security/secrets.AllPatterns — so a switch here is the
// authoritative place to tune severity. Anything unknown gets high (see
// rank() default).
func severityForRule(rule string) severityLevel {
	switch rule {
	case "openrouter-key":
		return sevHigh
	case "github-pat":
		return sevHigh
	case "env-assignment":
		return sevMed
	case "private-key":
		return sevCritical
	}
	return sevHigh
}

// =============================================================================
// Flags + config
// =============================================================================

// secretsFlags groups every CLI flag. flag.FS holds the parsed values.
type secretsFlags struct {
	subcommand  string
	path        string
	excludes    []string
	minSeverity severityLevel
	format      string
	quiet       bool
	help        bool
}

// Env-var names are kept in package scope so parseSecretFlags can reference
// them without reaching into test code.
const (
	envSecretsExclude     = "HELIX_SECRETS_EXCLUDE"
	envSecretsMinSeverity = "HELIX_SECRETS_MIN_SEVERITY"
	envSecretsFormat      = "HELIX_SECRETS_FORMAT"
	envSecretsQuiet       = "HELIX_SECRETS_QUIET"
)

// parseSecretFlags builds a flag.FS, applies env-var defaults, parses
// args. The first positional arg is the subcommand (default: "help"); the
// second positional arg, for "scan", is the path.
//
// All value-clipping/validation happens in runSecretScan — parseSecretFlags
// only worries about flag plumbing.
func parseSecretFlags(args []string, stdout, stderr io.Writer) (secretsFlags, error) {
	subcommand := "help"
	rest := args
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		subcommand = rest[0]
		rest = rest[1:]
	}

	switch subcommand {
	case "scan", "list-rules", "help", "-h", "--help":
		// recognised scan-only subcommands handled below
	case "set", "get", "delete", "list", "rotate", "init":
		// CRUD subcommands are parsed + dispatched by secrets_crud.go.
		// parseSecretFlags returns the raw subcommand so runSecrets
		// can hand the original args off to parseCrudFlags.
		return secretsFlags{subcommand: subcommand}, nil
	default:
		return secretsFlags{}, fmt.Errorf("unknown subcommand %q (expected: scan | list-rules | help | set | get | delete | list | rotate | init)", subcommand)
	}

	if subcommand == "help" || subcommand == "-h" || subcommand == "--help" {
		return secretsFlags{subcommand: "help", help: true}, nil
	}

	fs := flag.NewFlagSet("helix-secrets-"+subcommand, flag.ContinueOnError)
	fs.SetOutput(stderr)

	var f secretsFlags
	f.subcommand = subcommand

	// Vars so we can reference each flag multiple times (for repeatable
	// `--exclude` and env-var merging).
	var excludeVals stringSliceFlag
	var minSevRaw string

	fs.Var(&excludeVals, "exclude",
		"Glob to exclude (repeatable) (env HELIX_SECRETS_EXCLUDE: comma-separated)")
	fs.StringVar(&minSevRaw, "min-severity", "low",
		"Filter: low | med | high | critical (env HELIX_SECRETS_MIN_SEVERITY)")
	fs.StringVar(&f.format, "format", "table",
		"Output format: table | json (env HELIX_SECRETS_FORMAT)")
	fs.BoolVar(&f.quiet, "quiet", false,
		"Suppress per-finding lines; print only summary (env HELIX_SECRETS_QUIET=1)")

	// Apply env-var defaults before parse.
	if v := os.Getenv(envSecretsExclude); v != "" && !excludeVals.set {
		for _, g := range strings.Split(v, ",") {
			g = strings.TrimSpace(g)
			if g != "" {
				excludeVals.values = append(excludeVals.values, g)
			}
		}
		excludeVals.set = true
	}
	if v := os.Getenv(envSecretsMinSeverity); v != "" && minSevRaw == "low" {
		minSevRaw = v
	}
	if v := os.Getenv(envSecretsFormat); v != "" && f.format == "table" {
		f.format = v
	}
	if v := os.Getenv(envSecretsQuiet); v != "" && !f.quiet {
		f.quiet = v == "1" || strings.EqualFold(v, "true")
	}

	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return secretsFlags{subcommand: subcommand, help: true}, nil
		}
		return secretsFlags{}, err
	}

	// Validate exclude globs eagerly so bad patterns fail at parse time
	// rather than at scan time. This also lets parseSecretFlags return
	// errors for CLI input validation, not just structural parse errors.
	if _, err := newGlobExcluder(excludeVals.values); err != nil {
		return secretsFlags{}, err
	}

	// Path is the first positional after flags.
	if subcommand == "scan" {
		pos := fs.Args()
		if len(pos) < 1 {
			return secretsFlags{}, fmt.Errorf("helix secrets scan: missing required PATH argument")
		}
		f.path = pos[0]
		if extra := pos[1:]; len(extra) > 0 {
			return secretsFlags{}, fmt.Errorf("helix secrets scan: unexpected extra arguments: %v", extra)
		}
	}

	minSev, err := parseSeverity(minSevRaw)
	if err != nil {
		return secretsFlags{}, err
	}
	f.minSeverity = minSev

	// Normalise format.
	switch strings.ToLower(strings.TrimSpace(f.format)) {
	case "table", "":
		f.format = "table"
	case "json":
		f.format = "json"
	default:
		return secretsFlags{}, fmt.Errorf("invalid --format %q (expected: table | json)", f.format)
	}

	f.excludes = excludeVals.values
	return f, nil
}

// stringSliceFlag implements flag.Value for repeatable string slices.
// We also expose a `set` flag so env-var defaults don't get clobbered
// when the user also passes --exclude.
type stringSliceFlag struct {
	values []string
	set    bool
}

func (s *stringSliceFlag) String() string {
	return strings.Join(s.values, ",")
}

func (s *stringSliceFlag) Set(v string) error {
	s.values = append(s.values, v)
	s.set = true
	return nil
}

// =============================================================================
// Entry points — wired from main.go
// =============================================================================

// runSecretsWithDryRun mirrors the pattern used by every other
// subcommand: thread the global --dry-run flag in, translate the inner
// int exit code into the errExit wrapper so main.go can use it as a Go
// error. `helix secrets scan` has no remote side-effects so --dry-run is
// effectively a no-op — we accept it for parity with the other
// subcommands.
func runSecretsWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
	rc := runSecrets(args, stdout, stderr)
	if rc != 0 {
		return errExit{code: rc}
	}
	return nil
}

// runSecrets is the cmd/helix secrets subcommand entry point. Returns 0
// on success, 1 if any findings (after filter), 2 on invocation errors.
func runSecrets(args []string, stdout, stderr io.Writer) int {
	flags, err := parseSecretFlags(args, stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "helix secrets: parse:", err)
		printSecretsUsage(stderr)
		return 2
	}
	if flags.help {
		printSecretsUsage(stdout)
		return 0
	}

	switch flags.subcommand {
	case "scan":
		return runSecretsScan(flags, stdout, stderr)
	case "list-rules":
		return runSecretsListRules(stdout)
	case "set", "get", "delete", "list", "rotate", "init":
		// CRUD subcommands parse their own flags + positionals via
		// secrets_crud.go. We hand the original args (subcommand + rest)
		// off to parseCrudFlags so it can re-derive the subcommand.
		cf, err := parseCrudFlags(args, stdout, stderr)
		if err != nil {
			fmt.Fprintln(stderr, "helix secrets: parse:", err)
			printCrudUsage(stderr)
			return 2
		}
		return runCrud(cf, stdout, stderr)
	}
	return 2
}

// runSecretsScan walks the path (or single file), applies glob
// exclusions, scans via pkg/security/secrets.ScanPath (or .ScanFile for
// single files), filters by --min-severity, and renders the result.
//
// Exit codes:
//
//	0 — no findings (clean)
//	1 — at least one finding above the severity threshold
//	2 — invocation / IO error
func runSecretsScan(f secretsFlags, stdout, stderr io.Writer) int {
	if f.path == "" {
		fmt.Fprintln(stderr, "helix secrets scan: missing PATH")
		return 2
	}

	excluder, err := newGlobExcluder(f.excludes)
	if err != nil {
		fmt.Fprintln(stderr, "helix secrets scan: bad --exclude:", err)
		return 2
	}

	findings, scanErr := scanWithExcludes(f.path, excluder)
	if scanErr != nil {
		// Walk error that produced no findings isn't fatal — but a
		// stat error on a missing file is.
		fmt.Fprintln(stderr, "helix secrets scan:", scanErr)
		if len(findings) == 0 {
			return 2
		}
	}

	minRank := f.minSeverity.rank()
	filtered := make([]secrets.Finding, 0, len(findings))
	for _, fnd := range findings {
		if severityForRule(fnd.Rule).rank() >= minRank {
			filtered = append(filtered, fnd)
		}
	}

	files := countFiles(f.path)
	report := secrets.Report{Path: f.path, Findings: filtered, Files: files}

	switch strings.ToLower(f.format) {
	case "json":
		return renderSecretsJSON(report, stdout, stderr)
	default:
		return renderSecretsTable(report, f.quiet, stdout, stderr)
	}
}

// runSecretsListRules emits the catalog as a JSON document. Kept simple —
// this is a debugging aid for the operator who wants to know which rules
// exist at what severity without writing Go code.
func runSecretsListRules(stdout io.Writer) int {
	type ruleRow struct {
		Rule     string `json:"rule"`
		Severity string `json:"severity"`
	}
	rules := secrets.AllPatterns()
	rows := make([]ruleRow, 0, len(rules))
	for _, r := range rules {
		rows = append(rows, ruleRow{Rule: r.Name, Severity: string(severityForRule(r.Name))})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Rule < rows[j].Rule })
	out, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		fmt.Fprintln(io.Discard, "marshal:", err) // unreachable
		return 2
	}
	if _, err := stdout.Write(out); err != nil {
		fmt.Fprintln(io.Discard, "write:", err) // unreachable in tests
		return 2
	}
	if _, err := fmt.Fprintln(stdout); err != nil {
		return 2
	}
	return 0
}

// =============================================================================
// Glob exclusion
// =============================================================================

// globExcluder matches file paths against a fixed set of user-supplied
// glob patterns. Patterns use filepath.Match semantics, anchored against
// the path's full slash-normalised form. An empty set is a no-op.
type globExcluder struct {
	patterns []string
}

func newGlobExcluder(patterns []string) (*globExcluder, error) {
	out := make([]string, 0, len(patterns))
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Validate the pattern eagerly so we fail fast with a clear
		// error rather than emitting cryptic Match errors per-file
		// in scanWithExcludes.
		if _, err := filepath.Match(p, ""); err != nil {
			return nil, fmt.Errorf("invalid glob %q: %w", p, err)
		}
		out = append(out, p)
	}
	return &globExcluder{patterns: out}, nil
}

// excludes reports whether `path` matches any registered glob. Both
// basename (`*.bak`) and the full forward-slash-normalised path are
// tried — operators sometimes pass one or the other depending on shell.
func (g *globExcluder) excludes(path string) bool {
	if g == nil || len(g.patterns) == 0 {
		return false
	}
	clean := filepath.ToSlash(path)
	base := filepath.Base(clean)
	for _, p := range g.patterns {
		if ok, _ := filepath.Match(p, base); ok {
			return true
		}
		if ok, _ := filepath.Match(p, clean); ok {
			return true
		}
	}
	return false
}

// scanWithExcludes is a thin wrapper over secrets.ScanPath / ScanFile
// that consults the globExcluder. For a single file, exclusions are not
// applied (the user named it explicitly). For directories we walk via
// filepath.WalkDir ourselves so we can drop the file before invoking the
// scanner.
func scanWithExcludes(path string, excluder *globExcluder) ([]secrets.Finding, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.IsDir() {
		return secrets.ScanFile(path)
	}

	var out []secrets.Finding
	walkErr := filepath.WalkDir(path, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == ".venv" || name == "node_modules" {
				return filepath.SkipDir
			}
			// Skip hidden directories (mirror pkg/security/secrets).
			if strings.HasPrefix(name, ".") && p != path {
				return filepath.SkipDir
			}
			if excluder.excludes(p) {
				return filepath.SkipDir
			}
			return nil
		}
		if excluder.excludes(p) {
			return nil
		}
		findings, ferr := secrets.ScanFile(p)
		if ferr != nil {
			// Permission/symlink etc. — keep walking.
			return nil
		}
		out = append(out, findings...)
		return nil
	})
	if walkErr != nil {
		return out, fmt.Errorf("walk %s: %w", path, walkErr)
	}
	return out, nil
}

// countFiles reports the number of *files* under the path (including the
// path itself if it is a file). Used as a hint in the report header.
// Implementation note: we use os.Stat (not WalkDir) so the count avoids
// the excluder — the user wants to see total files scanned, not "files
// minus excluded". We DO honour the walker's hidden-dir skip for parity
// with pkg/security/secrets.
func countFiles(path string) int {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	if !info.IsDir() {
		return 1
	}
	count := 0
	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == ".venv" || name == "node_modules" {
				return filepath.SkipDir
			}
			if strings.HasPrefix(name, ".") && p != path {
				return filepath.SkipDir
			}
			return nil
		}
		count++
		return nil
	})
	return count
}

// =============================================================================
// Renderers
// =============================================================================

// renderSecretsTable prints the canonical "file:line:col: rule snippet"
// format with a header line. When --quiet is set we emit the header and
// the count but skip the per-finding rows — useful for CI scripts that
// only care about the exit code.
func renderSecretsTable(report secrets.Report, quiet bool, stdout, stderr io.Writer) int {
	fmt.Fprintf(stdout, "Scanned: %s (%d files)\n", report.Path, report.Files)
	fmt.Fprintf(stdout, "Findings: %d\n", len(report.Findings))
	if quiet || len(report.Findings) == 0 {
		if len(report.Findings) == 0 {
			fmt.Fprintln(stdout, "  (no secrets detected)")
		}
		if len(report.Findings) == 0 {
			return 0
		}
		return 1
	}
	for _, f := range report.Findings {
		sev := severityForRule(f.Rule)
		fmt.Fprintf(stdout, "  [%s] %s\n", sev, f.String())
	}
	return 1
}

// renderSecretsJSON emits a JSON document shaped to be machine-readable.
// Stable field order helps downstream tooling.
func renderSecretsJSON(report secrets.Report, stdout, stderr io.Writer) int {
	type findingJSON struct {
		Rule     string `json:"rule"`
		Severity string `json:"severity"`
		File     string `json:"file"`
		Line     int    `json:"line"`
		Column   int    `json:"column"`
		Snippet  string `json:"snippet"`
	}
	type outJSON struct {
		Path     string        `json:"path"`
		Files    int           `json:"files"`
		Findings []findingJSON `json:"findings"`
	}
	o := outJSON{Path: report.Path, Files: report.Files, Findings: make([]findingJSON, 0, len(report.Findings))}
	for _, f := range report.Findings {
		o.Findings = append(o.Findings, findingJSON{
			Rule: f.Rule, Severity: string(severityForRule(f.Rule)),
			File: f.File, Line: f.Line, Column: f.Column, Snippet: f.Snippet,
		})
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(o); err != nil {
		return 2
	}
	if len(report.Findings) == 0 {
		return 0
	}
	return 1
}

// =============================================================================
// Usage
// =============================================================================

func printSecretsUsage(w io.Writer) {
	fmt.Fprint(w, `helix secrets — scan for secret patterns in files/directories

Usage:
  helix secrets scan <path> [--exclude GLOB]... [--min-severity LVL] [--format FMT] [--quiet]
  helix secrets list-rules
  helix secrets help

Subcommands:
  scan <path>       Walk the file or directory and report findings
  list-rules        Print every registered rule with its severity (JSON)
  help              Print this help

Flags for `+"`scan`"+`:
  --exclude GLOB        Glob to exclude (repeatable). Tries both basename
                        (e.g. "*.bak") and full path match.
  --min-severity LVL    Filter: low | med | high | critical (default: low).
                        Findings below this level are dropped.
  --format FMT          table (default) | json.
  --quiet               Suppress per-finding lines; emit only the header.

Environment overrides (applied if flag unset):
  HELIX_SECRETS_EXCLUDE        comma-separated list of glob exclusions
  HELIX_SECRETS_MIN_SEVERITY   default severity threshold
  HELIX_SECRETS_FORMAT         default format
  HELIX_SECRETS_QUIET          1 / true to enable --quiet by default

Exit codes:
  0 — clean (no findings or all filtered out)
  1 — at least one finding above the severity threshold
  2 — invocation error (bad flag, missing file, unparseable glob)
`)
	fmt.Fprintln(w) // trailing newline
}
