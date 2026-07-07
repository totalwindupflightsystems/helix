package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/vuln"
)

// ============================================================================
// helix vuln CLI — dependency vulnerability scanning (spec §6.6)
//
// Wraps govulncheck (Go), npm audit (Node), pip-audit (Python) behind a
// unified operator surface. Auto-detects language from project markers
// (go.mod, package.json, requirements.txt). Exit code follows the
// severity band: critical/high → 1, medium → 2, low → 0 (matching the
// pkg/vuln Report.ExitCode contract).
//
// Subcommands:
//   - scan    Run the language-appropriate scanner on a project dir
//   - parse   Parse canned scanner JSON (for testing/inspection)
//   - list    List the canonical severities and their weights
//   - help
// ============================================================================

const (
	vulnExitOK    = 0
	vulnExitFail  = 1
	vulnExitError = 2
)

// vulnFlags holds parsed CLI flags.
type vulnFlags struct {
	subcommand string // scan, parse, list, help
	dir        string
	language   string // go|js|python — overrides auto-detect
	jsonOut    bool
	timeout    int // seconds; 0 = scanner default
}

// parseVulnFlags parses args for `helix vuln`.
func parseVulnFlags(args []string) (vulnFlags, bool, int) {
	var f vulnFlags
	f.dir = "."
	helpWanted := false

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			helpWanted = true
		case arg == "--json":
			f.jsonOut = true
		case arg == "--dir":
			if i+1 < len(args) {
				f.dir = args[i+1]
				i++
			} else {
				return f, false, vulnExitError
			}
		case arg == "--language":
			if i+1 < len(args) {
				f.language = args[i+1]
				i++
			} else {
				return f, false, vulnExitError
			}
		case arg == "--timeout":
			if i+1 < len(args) {
				if _, err := fmt.Sscanf(args[i+1], "%d", &f.timeout); err != nil {
					return f, false, vulnExitError
				}
				i++
			} else {
				return f, false, vulnExitError
			}
		case strings.HasPrefix(arg, "--"):
			return f, false, vulnExitError
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

	return f, helpWanted, vulnExitOK
}

// printVulnHelp prints the help text.
func printVulnHelp(w io.Writer) {
	fmt.Fprintln(w, `helix vuln — dependency vulnerability scanner (spec §6.6)

Wraps govulncheck (Go), npm audit (Node.js), and pip-audit (Python) behind a
unified operator surface. Auto-detects language from project markers.

Usage:
  helix vuln scan [--dir <path>] [--language go|js|python] [--json]
                  [--timeout <seconds>]
  helix vuln parse <scanner> [--json]   # read JSON from stdin
  helix vuln list
  helix vuln help

Subcommands:
  scan   Run the language-appropriate scanner on the project directory.
         Auto-detects from go.mod / package.json / requirements.txt unless
         --language is set.
  parse  Parse canned scanner JSON from stdin (scanner=go|js|python). Useful
         for offline analysis or testing the normalisation layer.
  list   Print the canonical severity ordering and weights.
  help   Show this help.

Flags:
  --dir <path>       Project directory (default: cwd)
  --language <lang>  Override auto-detection (go|js|python)
  --timeout <secs>   Per-scanner timeout (default: scanner internal)
  --json             Structured JSON output
  --help, -h         Show this help

Exit codes (scan):
  0  No vulnerabilities found (or only low)
  1  Critical or high severity findings (CI-block per spec §6.6)
  2  Medium severity findings
  3  Invocation / IO error`)
}

// runVuln is the entry point for `helix vuln`.
func runVuln(args []string, stdout, stderr io.Writer) int {
	flags, helpWanted, rc := parseVulnFlags(args)
	if rc != vulnExitOK {
		return rc
	}
	if helpWanted {
		printVulnHelp(stdout)
		return vulnExitOK
	}

	switch flags.subcommand {
	case "help":
		printVulnHelp(stdout)
		return vulnExitOK
	case "scan":
		return runVulnScan(flags, stdout, stderr)
	case "parse":
		return runVulnParse(args, stdout, stderr)
	case "list":
		return runVulnList(stdout)
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n", flags.subcommand)
		return vulnExitError
	}
}

// runVulnScan runs the configured scanner against the project directory.
func runVulnScan(flags vulnFlags, stdout, stderr io.Writer) int {
	// Resolve language.
	lang := vuln.Language(flags.language)
	if flags.language != "" {
		switch lang {
		case vuln.LangGo, vuln.LangJS, vuln.LangPython:
			// ok
		default:
			fmt.Fprintf(stderr, "error: invalid --language %q (must be go|js|python)\n", flags.language)
			return vulnExitError
		}
	} else {
		detected, err := vuln.DetectLanguage(flags.dir)
		if err != nil {
			fmt.Fprintf(stderr, "error: detect language in %s: %v\n", flags.dir, err)
			return vulnExitError
		}
		lang = detected
	}

	ctx := context.Background()
	if flags.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(flags.timeout)*time.Second)
		defer cancel()
	}

	scanner := vuln.NewScanner()
	// The default Executor shells out to the binary on PATH. In a
	// clean repo without scanners installed, the runner reports
	// ScannerUnavailable for each finding — see pkg/vuln docs.

	var (
		report *vuln.Report
		err    error
	)
	switch lang {
	case vuln.LangGo:
		report, err = scanner.RunGo(ctx, flags.dir)
	case vuln.LangJS:
		report, err = scanner.RunJS(ctx, flags.dir)
	case vuln.LangPython:
		report, err = scanner.RunPython(ctx, flags.dir)
	default:
		fmt.Fprintf(stderr, "error: unsupported language %q\n", lang)
		return vulnExitError
	}

	if err != nil {
		fmt.Fprintf(stderr, "error: scan %s: %v\n", flags.dir, err)
		return vulnExitError
	}

	if flags.jsonOut {
		out, _ := json.MarshalIndent(report, "", "  ")
		fmt.Fprintln(stdout, string(out))
	} else {
		fmt.Fprintln(stdout, report.FormatSummary())
	}

	return report.ExitCode()
}

// runVulnParse reads JSON from stdin and normalises it through the
// language-specific parser. Useful for offline analysis.
func runVulnParse(args []string, stdout, stderr io.Writer) int {
	// First positional after `parse` is the scanner name.
	if len(args) < 2 {
		fmt.Fprintln(stderr, "error: parse requires a scanner (go|js|python)")
		return vulnExitError
	}
	scanner := strings.ToLower(strings.TrimSpace(args[1]))

	// Check for --json flag.
	jsonOut := false
	for _, a := range args[2:] {
		if a == "--json" {
			jsonOut = true
		}
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(stderr, "error: read stdin: %v\n", err)
		return vulnExitError
	}

	var vulns []vuln.Vulnerability
	switch scanner {
	case "go":
		vulns, err = vuln.ParseGoVulnCheck(string(data))
	case "js", "npm":
		vulns, err = vuln.ParseNPMAudit(string(data))
	case "python", "pip":
		vulns, err = vuln.ParsePipAudit(string(data))
	default:
		fmt.Fprintf(stderr, "error: unknown scanner %q (must be go|js|python)\n", scanner)
		return vulnExitError
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: parse: %v\n", err)
		return vulnExitError
	}

	rep := &vuln.Report{Findings: vulns}
	if jsonOut {
		out, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Fprintln(stdout, string(out))
	} else {
		fmt.Fprintln(stdout, rep.FormatSummary())
	}
	return rep.ExitCode()
}

// runVulnList prints severity ordering.
func runVulnList(stdout io.Writer) int {
	for _, sev := range vuln.AllSeverities() {
		fmt.Fprintf(stdout, "%-10s  weight=%d\n", sev, sev.Weight())
	}
	return vulnExitOK
}

// runVulnWithDryRun wraps runVuln with the global --dry-run flag.
func runVulnWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
	rc := runVuln(args, stdout, stderr)
	if rc != 0 && rc != vulnExitFail {
		return errExit{code: rc}
	}
	return nil
}
