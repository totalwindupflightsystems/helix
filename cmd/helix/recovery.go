// Package main — helix recovery CLI: wraps pkg/recovery runbook registry
// and DR scenario catalog (spec §14.1, §10.3).
//
// `helix recovery <subcommand>` lets operators look up the recovery
// runbook for a failing component, browse the component failure matrix,
// inspect DR scenarios, view the key-rotation procedure, and query the
// scaling model for capacity planning.
package main

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/recovery"
)

const (
	recoveryExitOK    = 0
	recoveryExitError = 2
)

type recoveryFlags struct {
	subcommand string // matrix | lookup | components | scenarios | key-rotation | scaling | severity
	component  string
	id         string
	mode       string
	severity   string
	cores      int
	coresPer   float64
	jsonOut    bool
}

// recoverySubcommands lists the supported recovery subcommands.
var recoverySubcommands = []string{
	"matrix", "lookup", "components", "scenarios", "key-rotation", "scaling", "severity",
}

// parseRecoveryFlags parses args for `helix recovery <sub> [flags]`.
func parseRecoveryFlags(args []string, stdout, stderr io.Writer) (recoveryFlags, int) {
	var f recoveryFlags
	f.subcommand = "matrix" // default

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			printRecoveryHelp(stdout)
			return f, recoveryExitOK
		case arg == "--json":
			f.jsonOut = true
		case arg == "--component":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "recovery: --component requires a value")
				return f, recoveryExitError
			}
			i++
			f.component = args[i]
		case arg == "--id":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "recovery: --id requires a value")
				return f, recoveryExitError
			}
			i++
			f.id = args[i]
		case arg == "--mode":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "recovery: --mode requires a value")
				return f, recoveryExitError
			}
			i++
			f.mode = args[i]
		case arg == "--severity":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "recovery: --severity requires a value")
				return f, recoveryExitError
			}
			i++
			f.severity = args[i]
		case arg == "--cores":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "recovery: --cores requires a value")
				return f, recoveryExitError
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil {
				fmt.Fprintf(stderr, "recovery: --cores invalid: %v\n", err)
				return f, recoveryExitError
			}
			f.cores = n
		case arg == "--cores-per-agent":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "recovery: --cores-per-agent requires a value")
				return f, recoveryExitError
			}
			i++
			v, err := strconv.ParseFloat(args[i], 64)
			if err != nil {
				fmt.Fprintf(stderr, "recovery: --cores-per-agent invalid: %v\n", err)
				return f, recoveryExitError
			}
			f.coresPer = v
		case strings.HasPrefix(arg, "--component="):
			f.component = strings.TrimPrefix(arg, "--component=")
		case strings.HasPrefix(arg, "--id="):
			f.id = strings.TrimPrefix(arg, "--id=")
		case strings.HasPrefix(arg, "--mode="):
			f.mode = strings.TrimPrefix(arg, "--mode=")
		case strings.HasPrefix(arg, "--severity="):
			f.severity = strings.TrimPrefix(arg, "--severity=")
		case strings.HasPrefix(arg, "--cores="):
			n, err := strconv.Atoi(strings.TrimPrefix(arg, "--cores="))
			if err != nil {
				fmt.Fprintf(stderr, "recovery: --cores invalid: %v\n", err)
				return f, recoveryExitError
			}
			f.cores = n
		case strings.HasPrefix(arg, "--cores-per-agent="):
			v, err := strconv.ParseFloat(strings.TrimPrefix(arg, "--cores-per-agent="), 64)
			if err != nil {
				fmt.Fprintf(stderr, "recovery: --cores-per-agent invalid: %v\n", err)
				return f, recoveryExitError
			}
			f.coresPer = v
		case strings.HasPrefix(arg, "--"):
			fmt.Fprintf(stderr, "recovery: unknown flag %q\n", arg)
			return f, recoveryExitError
		default:
			if isRecoverySubcommand(arg) {
				f.subcommand = arg
			} else {
				fmt.Fprintf(stderr, "recovery: unknown subcommand %q (available: %s)\n",
					arg, strings.Join(recoverySubcommands, ", "))
				return f, recoveryExitError
			}
		}
		i++
	}

	return f, recoveryExitOK
}

func isRecoverySubcommand(s string) bool {
	for _, c := range recoverySubcommands {
		if c == s {
			return true
		}
	}
	return false
}

func printRecoveryHelp(w io.Writer) {
	fmt.Fprintf(w, `helix recovery — Error recovery runbook + DR scenario catalog (spec §14.1, §10.3)

Usage:
  helix recovery matrix              Print the full component-failure matrix
  helix recovery lookup              Look up a single entry (--id <FJ-NNN> or --component <name>)
  helix recovery components          List all components in the registry
  helix recovery scenarios           List all DR scenarios (spec §10.3)
  helix recovery key-rotation        Print the spec §10.3 security incident key rotation steps
  helix recovery scaling             Print the spec §10.4 scaling model + capacity calc
  helix recovery severity            Filter runbook by severity (--severity SEV-1|SEV-2|SEV-3)

Flags:
  --component <name>       Filter by component name (lookup/matrix)
  --id <FJ-NNN>            Look up a specific runbook entry
  --mode <substring>       Filter by failure mode substring
  --severity <SEV-N>       Filter by severity (SEV-1, SEV-2, SEV-3)
  --cores <N>              Host core count (scaling)
  --cores-per-agent <N>    Cores per agent (scaling, default from spec §10.4)
  --json                   JSON output
  --help                   Show this help
`)
}

// runRecovery dispatches to the appropriate subcommand handler.
func runRecovery(args []string, stdout, stderr io.Writer) int {
	flags, rc := parseRecoveryFlags(args, stdout, stderr)
	if rc != recoveryExitOK {
		return rc
	}

	switch flags.subcommand {
	case "matrix":
		return runRecoveryMatrix(flags, stdout, stderr)
	case "lookup":
		return runRecoveryLookup(flags, stdout, stderr)
	case "components":
		return runRecoveryComponents(flags, stdout, stderr)
	case "scenarios":
		return runRecoveryScenarios(flags, stdout, stderr)
	case "key-rotation":
		return runRecoveryKeyRotation(flags, stdout, stderr)
	case "scaling":
		return runRecoveryScaling(flags, stdout, stderr)
	case "severity":
		return runRecoverySeverity(flags, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "recovery: unknown subcommand %q\n", flags.subcommand)
		return recoveryExitError
	}
}

// runRecoveryMatrix prints the full component-failure matrix.
func runRecoveryMatrix(flags recoveryFlags, stdout, stderr io.Writer) int {
	r := recovery.NewRecoveryRegistry()
	if flags.jsonOut {
		matrix := r.RecoveryMatrix()
		fmt.Fprintf(stdout, `{"entry_count":%d,"components":[`, r.Count())
		first := true
		comps := r.Components()
		for _, c := range comps {
			if !first {
				fmt.Fprint(stdout, ",")
			}
			first = false
			fmt.Fprintf(stdout, `%q`, c)
		}
		fmt.Fprint(stdout, "]}\n")
		_ = matrix
		return recoveryExitOK
	}
	fmt.Fprintf(stdout, "%s\n", recovery.FormatMatrix(r))
	return recoveryExitOK
}

// runRecoveryLookup fetches a single entry by ID or component name.
func runRecoveryLookup(flags recoveryFlags, stdout, stderr io.Writer) int {
	r := recovery.NewRecoveryRegistry()

	var entries []recovery.FailureEntry

	switch {
	case flags.id != "":
		e := r.LookupByID(flags.id)
		if e == nil {
			fmt.Fprintf(stderr, "recovery: no entry with id %q\n", flags.id)
			return recoveryExitError
		}
		entries = []recovery.FailureEntry{*e}
	case flags.component != "":
		entries = r.LookupByComponent(flags.component)
		if len(entries) == 0 {
			fmt.Fprintf(stderr, "recovery: no entries for component %q\n", flags.component)
			return recoveryExitError
		}
	case flags.mode != "":
		entries = r.LookupByFailureMode(flags.mode)
		if len(entries) == 0 {
			fmt.Fprintf(stderr, "recovery: no entries matching mode %q\n", flags.mode)
			return recoveryExitError
		}
	default:
		fmt.Fprintln(stderr, "recovery: lookup requires --id, --component, or --mode")
		return recoveryExitError
	}

	for _, e := range entries {
		fmt.Fprintf(stdout, "%s\n", recovery.FormatRunbook(e))
		fmt.Fprintln(stdout)
	}
	return recoveryExitOK
}

// runRecoveryComponents lists all component names in the registry.
func runRecoveryComponents(flags recoveryFlags, stdout, stderr io.Writer) int {
	r := recovery.NewRecoveryRegistry()
	comps := r.Components()
	sort.Strings(comps)

	if flags.jsonOut {
		fmt.Fprint(stdout, `{"components":[`)
		for i, c := range comps {
			if i > 0 {
				fmt.Fprint(stdout, ",")
			}
			fmt.Fprintf(stdout, "%q", c)
		}
		fmt.Fprintf(stdout, "]}\n")
		return recoveryExitOK
	}

	fmt.Fprintf(stdout, "%d components in registry:\n", len(comps))
	for _, c := range comps {
		fmt.Fprintf(stdout, "  %s\n", c)
	}
	return recoveryExitOK
}

// runRecoveryScenarios lists all DR scenarios from spec §10.3.
func runRecoveryScenarios(flags recoveryFlags, stdout, stderr io.Writer) int {
	r := recovery.NewDRRegistry()
	scenarios := r.All()
	sort.Slice(scenarios, func(i, j int) bool { return scenarios[i].ID < scenarios[j].ID })

	if flags.jsonOut {
		fmt.Fprintf(stdout, `{"scenario_count":%d,"scenarios":[`, len(scenarios))
		for i, s := range scenarios {
			if i > 0 {
				fmt.Fprint(stdout, ",")
			}
			fmt.Fprintf(stdout, `{"id":%q,"scenario":%q,"rto":%q,"rpo":%q,"severity":%q}`,
				s.ID, s.Scenario, s.RTO, s.RPO, string(s.Severity))
		}
		fmt.Fprint(stdout, "]}\n")
		return recoveryExitOK
	}

	fmt.Fprintf(stdout, "%d DR scenarios (spec §10.3):\n", len(scenarios))
	for _, s := range scenarios {
		fmt.Fprintf(stdout, "%s\n", recovery.FormatDRScenario(s))
		fmt.Fprintln(stdout)
	}
	return recoveryExitOK
}

// runRecoveryKeyRotation prints the spec §10.3 key rotation procedure.
func runRecoveryKeyRotation(flags recoveryFlags, stdout, stderr io.Writer) int {
	steps := recovery.KeyRotationSteps()

	if flags.jsonOut {
		fmt.Fprint(stdout, `{"steps":[`)
		for i, s := range steps {
			if i > 0 {
				fmt.Fprint(stdout, ",")
			}
			fmt.Fprintf(stdout, "%q", s)
		}
		fmt.Fprint(stdout, "]}\n")
		return recoveryExitOK
	}

	fmt.Fprintln(stdout, "Spec §10.3 key rotation procedure:")
	for i, s := range steps {
		fmt.Fprintf(stdout, "  %d. %s\n", i+1, s)
	}
	return recoveryExitOK
}

// runRecoveryScaling prints the spec §10.4 scaling model + capacity calculation.
func runRecoveryScaling(flags recoveryFlags, stdout, stderr io.Writer) int {
	sm := recovery.DefaultScalingModel()

	if flags.cores > 0 && flags.coresPer > 0 {
		maxAgents := recovery.MaxAgentsForCores(flags.cores, flags.coresPer)
		if flags.jsonOut {
			fmt.Fprintf(stdout, `{"cores":%d,"cores_per_agent":%v,"max_concurrent_agents":%d}`+"\n",
				flags.cores, flags.coresPer, maxAgents)
			return recoveryExitOK
		}
		fmt.Fprintf(stdout, "Custom scaling calculation:\n")
		fmt.Fprintf(stdout, "  cores:                %d\n", flags.cores)
		fmt.Fprintf(stdout, "  cores_per_agent:      %.2f\n", flags.coresPer)
		fmt.Fprintf(stdout, "  max_concurrent_agents: %d\n", maxAgents)
		return recoveryExitOK
	}

	if flags.jsonOut {
		fmt.Fprintf(stdout, `{"max_concurrent_agents":%d,"cores_per_agent":%v,"host_cores":%d,"git_clone_latency_threshold_ns":%d,"prometheus_storage_limit_gb":%v}`+"\n",
			sm.MaxConcurrentAgents, sm.CoresPerAgent, sm.HostCores,
			sm.GitCloneLatencyThreshold.Nanoseconds(), sm.PrometheusStorageLimit)
		return recoveryExitOK
	}

	fmt.Fprintln(stdout, "Spec §10.4 scaling model:")
	fmt.Fprintf(stdout, "  max_concurrent_agents:       %d\n", sm.MaxConcurrentAgents)
	fmt.Fprintf(stdout, "  cores_per_agent:             %.2f\n", sm.CoresPerAgent)
	fmt.Fprintf(stdout, "  host_cores:                  %d\n", sm.HostCores)
	fmt.Fprintf(stdout, "  git_clone_latency_threshold: %v (add host above this)\n", sm.GitCloneLatencyThreshold)
	fmt.Fprintf(stdout, "  prometheus_storage_limit_gb: %.0f (add host above this)\n", sm.PrometheusStorageLimit)
	fmt.Fprintln(stdout, "\nProvide --cores and --cores-per-agent to compute max concurrent agents for a host.")
	return recoveryExitOK
}

// runRecoverySeverity filters runbook entries by severity level.
func runRecoverySeverity(flags recoveryFlags, stdout, stderr io.Writer) int {
	if flags.severity == "" {
		fmt.Fprintln(stderr, "recovery: severity requires --severity SEV-1|SEV-2|SEV-3")
		return recoveryExitError
	}

	sev := recovery.Severity(strings.ToUpper(flags.severity))
	if sev != recovery.SEV1 && sev != recovery.SEV2 && sev != recovery.SEV3 {
		fmt.Fprintf(stderr, "recovery: invalid severity %q (SEV-1, SEV-2, SEV-3)\n", flags.severity)
		return recoveryExitError
	}

	r := recovery.NewRecoveryRegistry()
	entries := r.LookupBySeverity(sev)

	if flags.jsonOut {
		fmt.Fprintf(stdout, `{"severity":%q,"description":%q,"count":%d}`+"\n",
			string(sev), recovery.SeverityDescription(sev), len(entries))
		return recoveryExitOK
	}

	fmt.Fprintf(stdout, "%s — %s\n", sev, recovery.SeverityDescription(sev))
	fmt.Fprintf(stdout, "%d entries:\n", len(entries))
	for _, e := range entries {
		fmt.Fprintf(stdout, "  [%s] %s/%s — %s\n", e.ErrorID, e.Component, e.FailureMode, e.Impact)
	}
	return recoveryExitOK
}

// runRecoveryWithDryRun wires the global --dry-run flag through. Recovery
// subcommands are read-only inspectors — the flag has no separate effect.
func runRecoveryWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
	_ = globalDryRun // no destructive ops
	rc := runRecovery(args, stdout, stderr)
	if rc != 0 {
		return errExit{code: rc}
	}
	return nil
}
