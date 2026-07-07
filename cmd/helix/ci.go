// Package main — helix ci CLI: wraps pkg/ci workflow generation/validation.
//
// `helix ci <subcommand>` exposes the Forgejo Actions test workflow
// generator and validator (spec §12.5). The CLI lets operators render the
// canonical workflow YAML, validate an existing workflow, and inspect the
// spec-derived defaults.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/ci"
)

const (
	ciExitOK    = 0
	ciExitError = 2
)

type ciFlags struct {
	subcommand string // render | validate | defaults
	path       string // path to workflow file (validate only)
	jsonOut    bool
}

// ciSubcommands lists the supported ci subcommands.
var ciSubcommands = []string{"render", "validate", "defaults"}

// parseCIFlags parses args for `helix ci <sub> [flags]`.
func parseCIFlags(args []string, stdout, stderr io.Writer) (ciFlags, int) {
	var f ciFlags
	f.subcommand = "render" // default

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			printCIHelp(stdout)
			return f, ciExitOK
		case arg == "--json":
			f.jsonOut = true
		case arg == "--path":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "ci: --path requires a value")
				return f, ciExitError
			}
			i++
			f.path = args[i]
		case strings.HasPrefix(arg, "--path="):
			f.path = strings.TrimPrefix(arg, "--path=")
		case strings.HasPrefix(arg, "--"):
			fmt.Fprintf(stderr, "ci: unknown flag %q\n", arg)
			return f, ciExitError
		default:
			if isCISubcommand(arg) {
				f.subcommand = arg
			} else {
				fmt.Fprintf(stderr, "ci: unknown subcommand %q (available: %s)\n",
					arg, strings.Join(ciSubcommands, ", "))
				return f, ciExitError
			}
		}
		i++
	}

	return f, ciExitOK
}

func isCISubcommand(s string) bool {
	for _, c := range ciSubcommands {
		if c == s {
			return true
		}
	}
	return false
}

func printCIHelp(w io.Writer) {
	fmt.Fprintf(w, `helix ci — Forgejo Actions test workflow generator/validator

Usage:
  helix ci render       Render the canonical spec §12.5 workflow as YAML
  helix ci validate     Validate an existing workflow file (default: .forgejo/workflows/test.yml)
  helix ci defaults     Show spec-derived default values (runner image, Go version, etc.)

Flags:
  --path <file>   Path to workflow YAML (validate only)
  --json          JSON output
  --help          Show this help
`)
}

// runCI dispatches to the appropriate subcommand handler.
func runCI(args []string, stdout, stderr io.Writer) int {
	flags, rc := parseCIFlags(args, stdout, stderr)
	if rc != ciExitOK {
		return rc
	}

	switch flags.subcommand {
	case "render":
		return runCIRender(flags, stdout, stderr)
	case "validate":
		return runCIValidate(flags, stdout, stderr)
	case "defaults":
		return runCIDefaults(flags, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "ci: unknown subcommand %q\n", flags.subcommand)
		return ciExitError
	}
}

// runCIRender outputs the canonical spec §12.5 workflow as YAML.
func runCIRender(flags ciFlags, stdout, stderr io.Writer) int {
	wf := ci.DefaultTestWorkflow()
	data, err := wf.Marshal()
	if err != nil {
		fmt.Fprintf(stderr, "ci: render failed: %v\n", err)
		return ciExitError
	}
	if flags.jsonOut {
		// Provide a small wrapper with metadata instead of raw YAML for JSON callers.
		fmt.Fprintf(stdout, `{"name":%q,"unit_job":%q,"integration_job":%q,"coverage_gate":%v,"forgejo_service":%v,"bytes":%d}`+"\n",
			wf.Name, ci.UnitJobName, ci.IntegrationJobName,
			wf.HasCoverageGate(), wf.HasForgejoService(), len(data))
		return ciExitOK
	}
	_, _ = stdout.Write(data)
	return ciExitOK
}

// runCIValidate reads a workflow file and reports whether it satisfies
// spec §12.5 validation rules.
func runCIValidate(flags ciFlags, stdout, stderr io.Writer) int {
	path := flags.path
	if path == "" {
		path = ".forgejo/workflows/test.yml"
	}

	data, err := os.ReadFile(path) //nolint:gosec // intentional read of operator-supplied path
	if err != nil {
		fmt.Fprintf(stderr, "ci: cannot read %q: %v\n", path, err)
		return ciExitError
	}

	wf, err := ci.Parse(data)
	if err != nil {
		fmt.Fprintf(stderr, "ci: parse %q failed: %v\n", path, err)
		return ciExitError
	}

	if err := wf.Validate(); err != nil {
		fmt.Fprintf(stdout, "INVALID %s — %v\n", path, err)
		return ciExitError
	}

	fmt.Fprintf(stdout, "VALID %s\n", path)
	fmt.Fprintf(stdout, "  unit_job:        %s\n", ci.UnitJobName)
	fmt.Fprintf(stdout, "  integration_job: %s (present=%v)\n", ci.IntegrationJobName, wf.HasIntegrationJob())
	fmt.Fprintf(stdout, "  coverage_gate:   %v\n", wf.HasCoverageGate())
	fmt.Fprintf(stdout, "  forgejo_service: %v\n", wf.HasForgejoService())
	return ciExitOK
}

// runCIDefaults prints the spec §12.5 derived default values for inspection.
func runCIDefaults(flags ciFlags, stdout, stderr io.Writer) int {
	wf := ci.DefaultTestWorkflow()
	if flags.jsonOut {
		fmt.Fprintf(stdout, `{"name":%q,"runner":%q,"go_version":%q,"forgejo_image":%q,"coverage_threshold":%v,"integration_timeout":%q,"branches":%q,"pr_types":%q}`+"\n",
			ci.DefaultWorkflowName, ci.DefaultRunnerImage, ci.DefaultGoVersion,
			ci.DefaultForgejoImage, ci.DefaultCoverageThreshold, ci.DefaultIntegrationTimeout,
			strings.Join(ci.DefaultBranches(), ","), strings.Join(ci.DefaultPRTypes(), ","))
		return ciExitOK
	}

	fmt.Fprintln(stdout, "Spec §12.5 defaults:")
	fmt.Fprintf(stdout, "  workflow_name:        %s\n", ci.DefaultWorkflowName)
	fmt.Fprintf(stdout, "  runner_image:         %s\n", ci.DefaultRunnerImage)
	fmt.Fprintf(stdout, "  go_version:           %s\n", ci.DefaultGoVersion)
	fmt.Fprintf(stdout, "  forgejo_image:        %s\n", ci.DefaultForgejoImage)
	fmt.Fprintf(stdout, "  coverage_threshold:   %.1f%%\n", ci.DefaultCoverageThreshold)
	fmt.Fprintf(stdout, "  integration_timeout:  %s\n", ci.DefaultIntegrationTimeout)
	fmt.Fprintf(stdout, "  trigger_branches:     %s\n", strings.Join(ci.DefaultBranches(), ", "))
	fmt.Fprintf(stdout, "  trigger_pr_types:     %s\n", strings.Join(ci.DefaultPRTypes(), ", "))
	fmt.Fprintf(stdout, "  workflow_jobs:        %d (%s, %s)\n", len(wf.Jobs), ci.UnitJobName, ci.IntegrationJobName)
	return ciExitOK
}

// runCIWithDryRun wires the global --dry-run flag through. CI subcommands are
// already read-only (render/validate/defaults), so the global flag has no
// separate effect — dry-run semantics apply implicitly.
func runCIWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
	_ = globalDryRun // no destructive ops in ci subcommands
	rc := runCI(args, stdout, stderr)
	if rc != 0 {
		return errExit{code: rc}
	}
	return nil
}
