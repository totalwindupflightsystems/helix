package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/deploy/agent"
	"github.com/totalwindupflightsystems/helix/pkg/deploy/caddy"
	"github.com/totalwindupflightsystems/helix/pkg/deploy/systemd"
)

// ============================================================================
// helix deploy CLI — deployment artifact rendering (spec §8 Deployment)
//
// The pkg/deploy family renders three deployment artifacts from
// declarative registries:
//   - agent: Docker Compose service specs (one per agent tier)
//   - caddy: Caddy vhost registry (one per public domain)
//   - systemd: systemd unit registry (one per platform service)
//
// Operators reach for `helix deploy` to inspect the rendered artifacts
// before applying them to a real environment. This CLI exposes:
//   - render  Render all three registries (or one, with --kind)
//   - list    List known artifacts in each registry
//   - tiers   List the canonical agent tiers
//   - help
// ============================================================================

const (
	depExitOK    = 0
	depExitFail  = 1
	depExitError = 2
)

// depFlags holds parsed CLI flags.
type depFlags struct {
	subcommand string // render, list, tiers, help
	kind       string // agent | caddy | systemd — render filter
	jsonOut    bool
}

// parseDeployFlags parses args for `helix deploy`.
func parseDeployFlags(args []string) (depFlags, bool, int) {
	var f depFlags
	helpWanted := false

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			helpWanted = true
		case arg == "--json":
			f.jsonOut = true
		case arg == "--kind":
			if i+1 < len(args) {
				f.kind = args[i+1]
				i++
			} else {
				return f, false, depExitError
			}
		case strings.HasPrefix(arg, "--"):
			return f, false, depExitError
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

	return f, helpWanted, depExitOK
}

// printDeployHelp prints the help text.
func printDeployHelp(w io.Writer) {
	fmt.Fprintln(w, `helix deploy — deployment artifact rendering (spec §8)

Renders three declarative registries of deployment artifacts:
  agent    Docker Compose service specs (one per agent tier)
  caddy    Caddy vhost registry (one per public domain)
  systemd  systemd unit registry (one per platform service)

Usage:
  helix deploy render [--kind agent|caddy|systemd] [--json]
  helix deploy list [--kind agent|caddy|systemd] [--json]
  helix deploy tiers
  helix deploy help

Subcommands:
  render  Print the rendered artifacts (compose YAML / Caddyfile / unit files).
          Default: render all three kinds.
  list    Print artifact names + summary, no full body.
  tiers   Print the canonical agent tiers with budget + resource defaults.
  help    Show this help.

Flags:
  --kind <agent|caddy|systemd>  Restrict output to one registry
  --json                        Structured JSON output (render + list)
  --help, -h                    Show this help

Exit codes:
  0  Success
  1  Render or validation failure
  2  Invocation error`)
}

// runDeploy is the entry point for `helix deploy`.
func runDeploy(args []string, stdout, stderr io.Writer) int {
	flags, helpWanted, rc := parseDeployFlags(args)
	if rc != depExitOK {
		return rc
	}
	if helpWanted {
		printDeployHelp(stdout)
		return depExitOK
	}

	switch flags.subcommand {
	case "help":
		printDeployHelp(stdout)
		return depExitOK
	case "render":
		return runDeployRender(flags, stdout, stderr)
	case "list":
		return runDeployList(flags, stdout, stderr)
	case "tiers":
		return runDeployTiers(stdout)
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n", flags.subcommand)
		return depExitError
	}
}

// depKindSet reports which registries to render given the optional --kind filter.
func depKindSet(kind string) (agent, caddyKind, systemdKind bool) {
	if kind == "" {
		return true, true, true
	}
	switch strings.ToLower(kind) {
	case "agent":
		return true, false, false
	case "caddy":
		return false, true, false
	case "systemd":
		return false, false, true
	}
	return false, false, false
}

// runDeployRender renders all (or one) registry.
func runDeployRender(flags depFlags, stdout, stderr io.Writer) int {
	wantsAgent, wantsCaddy, wantsSystemd := depKindSet(flags.kind)
	if flags.kind != "" && !wantsAgent && !wantsCaddy && !wantsSystemd {
		fmt.Fprintf(stderr, "error: invalid --kind %q (must be agent|caddy|systemd)\n", flags.kind)
		return depExitError
	}

	if flags.jsonOut {
		out := map[string]any{}
		if wantsAgent {
			reg := agent.NewRegistry()
			out["agent"] = reg.All()
		}
		if wantsCaddy {
			reg := caddy.NewRegistry()
			out["caddy"] = reg
		}
		if wantsSystemd {
			reg := systemd.DefaultRegistry()
			out["systemd"] = reg.All()
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return depExitOK
	}

	if wantsAgent {
		reg := agent.NewRegistry()
		rendered, err := renderAgentRegistry(reg)
		if err != nil {
			fmt.Fprintf(stderr, "error: render agent: %v\n", err)
			return depExitFail
		}
		fmt.Fprintln(stdout, rendered)
	}
	if wantsCaddy {
		reg := caddy.NewRegistry()
		body, err := caddy.Render(reg)
		if err != nil {
			fmt.Fprintf(stderr, "error: render caddy: %v\n", err)
			return depExitFail
		}
		fmt.Fprintln(stdout, body)
	}
	if wantsSystemd {
		reg := systemd.DefaultRegistry()
		body, err := systemd.FormatRegistry(reg)
		if err != nil {
			fmt.Fprintf(stderr, "error: render systemd: %v\n", err)
			return depExitFail
		}
		fmt.Fprintln(stdout, body)
	}
	return depExitOK
}

// renderAgentRegistry renders every registered agent spec as YAML.
func renderAgentRegistry(reg *agent.Registry) (string, error) {
	var b strings.Builder
	for _, name := range reg.List() {
		spec, ok := reg.Get(name)
		if !ok {
			continue
		}
		body, err := agent.FormatService(spec)
		if err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		b.WriteString(body)
		b.WriteString("\n")
	}
	return b.String(), nil
}

// runDeployList lists artifacts in the registry without rendering their full body.
func runDeployList(flags depFlags, stdout, stderr io.Writer) int {
	wantsAgent, wantsCaddy, wantsSystemd := depKindSet(flags.kind)
	if flags.kind != "" && !wantsAgent && !wantsCaddy && !wantsSystemd {
		fmt.Fprintf(stderr, "error: invalid --kind %q\n", flags.kind)
		return depExitError
	}

	type entry struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	}

	var entries []entry

	if wantsAgent {
		reg := agent.NewRegistry()
		for _, name := range reg.List() {
			entries = append(entries, entry{Kind: "agent", Name: name})
		}
	}
	if wantsCaddy {
		reg := caddy.NewRegistry()
		// Caddy registry exposes vhost names via Names().
		for _, name := range reg.Names() {
			entries = append(entries, entry{Kind: "caddy", Name: name})
		}
	}
	if wantsSystemd {
		reg := systemd.DefaultRegistry()
		for _, name := range reg.List() {
			entries = append(entries, entry{Kind: "systemd", Name: name})
		}
	}

	if flags.jsonOut {
		data, _ := json.MarshalIndent(entries, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return depExitOK
	}

	if len(entries) == 0 {
		fmt.Fprintln(stdout, "(no artifacts registered)")
		return depExitOK
	}
	fmt.Fprintf(stdout, "%-10s  %s\n", "KIND", "NAME")
	for _, e := range entries {
		fmt.Fprintf(stdout, "%-10s  %s\n", e.Kind, e.Name)
	}
	return depExitOK
}

// runDeployTiers prints the canonical agent tiers.
func runDeployTiers(stdout io.Writer) int {
	tiers := agent.AllTiers()
	for _, t := range tiers {
		fmt.Fprintf(stdout, "%-12s  valid=%v\n", t, t.IsValid())
	}
	return depExitOK
}

// runDeployWithDryRun wraps runDeploy with the global --dry-run flag.
func runDeployWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
	rc := runDeploy(args, stdout, stderr)
	if rc != 0 && rc != depExitFail {
		return errExit{code: rc}
	}
	return nil
}
