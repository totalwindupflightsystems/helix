// Command helix — pipeline.go
//
// `helix pipeline` is the CLI entry point to pkg/coordinator.PRLifecycleCoordinator.
// It exposes three subcommands:
//
//	helix pipeline run     --spec <file>  Execute the full 6-stage lifecycle
//	helix pipeline show     <state-file>  Render a saved LifecycleResult JSON
//	helix pipeline validate --spec <file> Dry-run the state machine — no side effects
//
// The PRLifecycleCoordinator composes pkg/estimate, pkg/review, pkg/negotiate,
// pkg/mergegate, and pkg/verify into a single pass. When those subsystems are
// not wired (typical for a CI invocation that supplies only spec metadata),
// the corresponding stages are reported as Skipped — the lifecycle still runs.
//
// Output formats:
//
//	Default: human-readable table of per-stage results + summary line.
//	--json:  structured JSON LifecycleResult on stdout.
//
// Exit codes:
//
//	0  lifecycle completed (decision APPROVED or all stages PASSED/SKIPPED)
//	1  lifecycle rejected / failed
//	2  invalid arguments
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/coordinator"
	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

// =============================================================================
// Spec parsing
// =============================================================================

// pipelineSpec is the minimal structured input required by the coordinator.
// In real deployments it would be produced by spec-decomposition; for the CLI
// we accept a small JSON/YAML-shaped object so callers can drive the
// coordinator without standing up the full spec pipeline.
type pipelineSpec struct {
	PRURL        string   `json:"pr_url"`
	AgentID      string   `json:"agent_id"`
	AgentTier    string   `json:"agent_tier"` // trust.TierProvisional | Observed | Trusted | Veteran
	CommitMsg    string   `json:"commit_msg"`
	Diff         string   `json:"diff"`
	ChangedFiles []string `json:"changed_files"`
}

// parsePipelineSpecFile reads a JSON file from disk and decodes it into a
// pipelineSpec. Any I/O or decode error is returned to the caller verbatim.
func parsePipelineSpecFile(path string) (pipelineSpec, error) {
	var spec pipelineSpec
	data, err := os.ReadFile(path)
	if err != nil {
		return spec, fmt.Errorf("read spec: %w", err)
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		return spec, fmt.Errorf("decode spec: %w", err)
	}
	return spec, nil
}

// trustTierFromString maps the spec string into a trust.TrustTier enum.
// Unknown values fall back to TierProvisional (the safest default — every
// new agent starts there) so a typo in the spec doesn't accidentally
// grant a higher tier.
func trustTierFromString(s string) trust.TrustTier {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "provisional":
		return trust.TierProvisional
	case "observed":
		return trust.TierObserved
	case "trusted":
		return trust.TierTrusted
	case "veteran":
		return trust.TierVeteran
	default:
		return trust.TierProvisional
	}
}

// =============================================================================
// run / show / validate dispatch
// =============================================================================

// dryRunStages are the lifecycle stages exercised by `helix pipeline validate`.
// Each of these stages reports as Skipped when its downstream subsystem is
// nil (the typical case for a CLI-only validation), so the validate path
// produces a LifecycleResult with no stage failures and a clean exit.
//
// MergeGate, ShadowDeploy, and Surveillance are intentionally excluded —
// MergeGate fails (not skips) on a missing evidence bundle; ShadowDeploy
// requires a real ShadowManager; Surveillance mutates aggregator state.
// All three are real production stages that should only run when wired.
var dryRunStages = []coordinator.StageName{
	coordinator.StageCostEstimate,
	coordinator.StageReview,
	coordinator.StageNegotiation,
}

// runPipeline is the cmd/helix pipeline subcommand entry point. It takes the
// args slice (already stripped of the "pipeline" prefix by main.go), parses
// the nested subcommand, and routes to the right helper.
func runPipeline(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printPipelineUsage(stdout)
		return 0
	}
	switch args[0] {
	case "run":
		return runPipelineRun(args[1:], stdout, stderr)
	case "show":
		return runPipelineShow(args[1:], stdout, stderr)
	case "validate":
		return runPipelineValidate(args[1:], stdout, stderr)
	case "--help", "-h", "help":
		printPipelineUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "helix pipeline: unknown subcommand %q\n\n", args[0])
		printPipelineUsage(stderr)
		return 2
	}
}

// -----------------------------------------------------------------------
// pipeline run
// -----------------------------------------------------------------------

type pipelineRunFlags struct {
	specPath string
	asJSON   bool
	verbose  bool
}

func parsePipelineRunFlags(args []string, stdout, stderr io.Writer) (pipelineRunFlags, bool, error) {
	fs := flag.NewFlagSet("helix-pipeline-run", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var f pipelineRunFlags
	fs.StringVar(&f.specPath, "spec", "", "Path to pipeline spec JSON file (required)")
	fs.BoolVar(&f.asJSON, "json", false, "Emit the LifecycleResult as JSON")
	fs.BoolVar(&f.verbose, "verbose", false, "Verbose stderr logging")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printPipelineUsage(stdout)
			return f, true, nil
		}
		return f, false, err
	}
	if rest := fs.Args(); len(rest) > 0 {
		return f, false, fmt.Errorf("unexpected positional arguments: %v", rest)
	}
	return f, false, nil
}

func runPipelineRun(args []string, stdout, stderr io.Writer) int {
	flags, showHelp, err := parsePipelineRunFlags(args, stdout, stderr)
	if showHelp {
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, "helix pipeline run: parse:", err)
		return 2
	}
	if flags.specPath == "" {
		fmt.Fprintln(stderr, "helix pipeline run: --spec is required")
		return 2
	}
	if flags.verbose {
		fmt.Fprintf(stderr, "[pipeline run] spec=%s json=%v\n", flags.specPath, flags.asJSON)
	}

	spec, err := parsePipelineSpecFile(flags.specPath)
	if err != nil {
		fmt.Fprintln(stderr, "helix pipeline run:", err)
		return 1
	}

	// Build a minimal PRRequest — every subsystem is nil, so each stage
	// reports as Skipped. We restrict the coordinator to dry-run stages
	// only (cost_estimate, review, negotiation) because MergeGate would
	// otherwise fail when no evidence bundle is attached, and
	// ShadowDeploy / Surveillance require live subsystems.
	req := coordinator.PRRequest{
		PRURL:        spec.PRURL,
		AgentID:      spec.AgentID,
		AgentTier:    trustTierFromString(spec.AgentTier),
		CommitMsg:    spec.CommitMsg,
		Diff:         spec.Diff,
		ChangedFiles: spec.ChangedFiles,
	}

	coord := coordinator.NewPRLifecycleCoordinator(coordinator.WithStages(dryRunStages...))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result := coord.Execute(ctx, req)

	if flags.asJSON {
		return emitPipelineJSON(result, stdout, stderr)
	}
	return emitPipelineTable(result, stdout)
}

// -----------------------------------------------------------------------
// pipeline show
// -----------------------------------------------------------------------

func runPipelineShow(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "helix pipeline show: state file path is required")
		return 2
	}
	path := args[0]
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(stderr, "helix pipeline show:", err)
		return 1
	}
	var result coordinator.LifecycleResult
	if err := json.Unmarshal(data, &result); err != nil {
		fmt.Fprintln(stderr, "helix pipeline show: decode:", err)
		return 1
	}
	return emitPipelineTable(&result, stdout)
}

// -----------------------------------------------------------------------
// pipeline validate
// -----------------------------------------------------------------------

type pipelineValidateFlags struct {
	specPath string
	asJSON   bool
	verbose  bool
}

func parsePipelineValidateFlags(args []string, stdout, stderr io.Writer) (pipelineValidateFlags, bool, error) {
	fs := flag.NewFlagSet("helix-pipeline-validate", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var f pipelineValidateFlags
	fs.StringVar(&f.specPath, "spec", "", "Path to pipeline spec JSON file (required)")
	fs.BoolVar(&f.asJSON, "json", false, "Emit the LifecycleResult as JSON")
	fs.BoolVar(&f.verbose, "verbose", false, "Verbose stderr logging")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printPipelineUsage(stdout)
			return f, true, nil
		}
		return f, false, err
	}
	if rest := fs.Args(); len(rest) > 0 {
		return f, false, fmt.Errorf("unexpected positional arguments: %v", rest)
	}
	return f, false, nil
}

// runPipelineValidate is the dry-run sibling of runPipelineRun. It exercises
// the coordinator with stub-only PRRequest fields (no subsystems wired) and
// returns the LifecycleResult so the operator can confirm the state machine
// would accept the spec.
//
// The key difference from `run`: validate does NOT touch any downstream
// service. Every stage that requires a live subsystem reports as Skipped.
// The decision is computed normally from stage outcomes — if anything
// fails validation (currently unreachable without wired subsystems), the
// exit code is 1.
func runPipelineValidate(args []string, stdout, stderr io.Writer) int {
	flags, showHelp, err := parsePipelineValidateFlags(args, stdout, stderr)
	if showHelp {
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, "helix pipeline validate: parse:", err)
		return 2
	}
	if flags.specPath == "" {
		fmt.Fprintln(stderr, "helix pipeline validate: --spec is required")
		return 2
	}
	if flags.verbose {
		fmt.Fprintf(stderr, "[pipeline validate] spec=%s json=%v\n", flags.specPath, flags.asJSON)
	}

	spec, err := parsePipelineSpecFile(flags.specPath)
	if err != nil {
		fmt.Fprintln(stderr, "helix pipeline validate:", err)
		return 1
	}

	req := coordinator.PRRequest{
		PRURL:        spec.PRURL,
		AgentID:      spec.AgentID,
		AgentTier:    trustTierFromString(spec.AgentTier),
		CommitMsg:    spec.CommitMsg,
		Diff:         spec.Diff,
		ChangedFiles: spec.ChangedFiles,
	}

	coord := coordinator.NewPRLifecycleCoordinator(coordinator.WithStages(dryRunStages...))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := coord.Execute(ctx, req)

	if flags.asJSON {
		return emitPipelineJSON(result, stdout, stderr)
	}
	return emitPipelineTable(result, stdout)
}

// =============================================================================
// Output formatters
// =============================================================================

// emitPipelineJSON marshals the LifecycleResult to stdout. Any marshal error
// is reported to stderr and the function returns 1; otherwise 0.
//
// Note: LifecycleResult.MarshalJSON is provided by the json tags on the
// struct itself, so this round-trip is straightforward.
func emitPipelineJSON(result *coordinator.LifecycleResult, stdout, stderr io.Writer) int {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, "helix pipeline: marshal:", err)
		return 1
	}
	fmt.Fprintln(stdout, string(data))
	return pipelineExitCode(result)
}

// emitPipelineTable renders a human-readable table of per-stage outcomes plus
// a summary line. Suitable for interactive use.
func emitPipelineTable(result *coordinator.LifecycleResult, stdout io.Writer) int {
	fmt.Fprintf(stdout, "Pipeline lifecycle result (PR: %s, agent: %s)\n", result.PRURL, result.AgentID)
	fmt.Fprintf(stdout, "Decision: %s\n", result.Decision)
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "%-18s %-10s %-9s %s\n", "STAGE", "STATUS", "ELAPSED", "DETAIL")
	fmt.Fprintln(stdout, strings.Repeat("-", 70))
	for _, stage := range result.Stages {
		fmt.Fprintf(stdout, "%-18s %-10s %-9s %s\n",
			stage.Name, stage.Status, stage.Duration,
			pipelineStageDetail(stage))
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, result.Summary())
	return pipelineExitCode(result)
}

// pipelineStageDetail returns the most informative single-line summary of a
// stage: error wins, then message, then "(no detail)".
func pipelineStageDetail(s coordinator.StageResult) string {
	if s.Error != "" {
		return s.Error
	}
	if s.Message != "" {
		return s.Message
	}
	if s.Skipped {
		return "skipped"
	}
	return "-"
}

// pipelineExitCode maps the LifecycleResult decision to a process exit code.
// APPROVED + REJECTED-with-no-failures (escalated but not failed) return 0;
// anything that reported a stage failure returns 1.
func pipelineExitCode(r *coordinator.LifecycleResult) int {
	if r.HasFailure() {
		return 1
	}
	return 0
}

// =============================================================================
// Usage
// =============================================================================

func printPipelineUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage: helix pipeline <subcommand> [flags]

Drive the PR lifecycle coordinator end-to-end.

Subcommands:
  run        Execute the full lifecycle against a spec (uses stub subsystems)
  show       Render a saved LifecycleResult JSON file
  validate   Dry-run the state machine — no downstream side effects

Run / validate flags:
  --spec <path>    Path to a JSON pipeline spec (see schema below)
  --json           Emit the LifecycleResult as JSON instead of a table
  --verbose        Verbose stderr logging

Spec schema (JSON):
  {
    "pr_url":        "https://forgejo/.../pulls/42",
    "agent_id":      "agent-7",
    "agent_tier":    "provisional|observed|trusted|veteran",
    "commit_msg":    "feat: ...",
    "diff":          "diff --git a/...",
    "changed_files": ["pkg/foo/foo.go"]
  }

Exit codes:
  0  lifecycle completed without stage failures
  1  lifecycle reported one or more stage failures
  2  invalid arguments

Examples:
  helix pipeline validate --spec /tmp/spec.json
  helix pipeline run --spec /tmp/spec.json --json
  helix pipeline show /tmp/lifecycle-result.json`)
}
