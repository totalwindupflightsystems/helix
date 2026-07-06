package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/coordinator"
	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

// ============================================================================
// helix lifecycle CLI — PR lifecycle coordinator (spec cross-component-
// wiring.md §1-§5 + SPECIFICATION.md §2 Data Flow)
// ============================================================================

const (
	lcExitOK    = 0
	lcExitFail  = 1 // lifecycle rejected or escalated
	lcExitError = 2 // invocation / IO error
)

// lcStage describes a lifecycle stage for the `stages` listing.
type lcStage struct {
	ID          string
	Name        string
	Description string
}

var lcAllStages = []lcStage{
	{ID: "cost", Name: "Cost Estimate", Description: "Pre-flight token burn estimation against agent budget (spec cost-estimator.md)"},
	{ID: "review", Name: "Adversarial Review", Description: "Multi-model adversarial review with bias stripping and evidence bundles (spec adversarial-review.md)"},
	{ID: "negotiation", Name: "PR Negotiation", Description: "Agent debate protocol with Chimera tie-break if review consensus is contested (spec pr-negotiation.md)"},
	{ID: "mergegate", Name: "Merge Gate", Description: "Pre-merge validation gate composing all 5 quality checks (spec adversarial-review.md §Integration)"},
	{ID: "shadow", Name: "Shadow Deployment", Description: "Dark-launch shadow verification before canary promotion (spec production-verification.md)"},
}

// lcFlags holds parsed CLI flags.
type lcFlags struct {
	subcommand string // run, stages, help
	repo       string
	pr         int
	prURL      string
	agent      string
	trustTier  string
	stages     string // comma-separated list
	dryRun     bool
	jsonOut    bool
}

// parseLifecycleFlags parses args for `helix lifecycle`.
func parseLifecycleFlags(args []string) (lcFlags, bool, int) {
	var f lcFlags
	helpWanted := false

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			helpWanted = true
		case arg == "--json":
			f.jsonOut = true
		case arg == "--dry-run":
			f.dryRun = true
		case arg == "--repo":
			if i+1 < len(args) {
				f.repo = args[i+1]
				i++
			} else {
				return f, false, lcExitError
			}
		case arg == "--pr":
			if i+1 < len(args) {
				_, _ = fmt.Sscanf(args[i+1], "%d", &f.pr)
				i++
			} else {
				return f, false, lcExitError
			}
		case arg == "--pr-url":
			if i+1 < len(args) {
				f.prURL = args[i+1]
				i++
			} else {
				return f, false, lcExitError
			}
		case arg == "--agent":
			if i+1 < len(args) {
				f.agent = args[i+1]
				i++
			} else {
				return f, false, lcExitError
			}
		case arg == "--trust":
			if i+1 < len(args) {
				f.trustTier = args[i+1]
				i++
			} else {
				return f, false, lcExitError
			}
		case arg == "--stages":
			if i+1 < len(args) {
				f.stages = args[i+1]
				i++
			} else {
				return f, false, lcExitError
			}
		case arg == "--":
			// rest are positional
		case len(arg) > 2 && arg[0] == '-' && arg[1] == '-':
			return f, false, lcExitError
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

	return f, helpWanted, lcExitOK
}

// printLifecycleHelp prints the help text.
func printLifecycleHelp(w io.Writer) {
	fmt.Fprintln(w, `helix lifecycle — PR lifecycle coordinator

Runs the multi-stage PR lifecycle pipeline: cost estimate → adversarial review →
negotiation (if contested) → merge gate → shadow deployment. Each stage runs
independently and failures are surfaced without crashing the pipeline.

Usage:
  helix lifecycle run --repo <r> --pr <N> [--stages cost,review,negotiation,mergegate,shadow]
  helix lifecycle stages
  helix lifecycle help

Subcommands:
  run       Execute the lifecycle pipeline for a PR
  stages    List all available lifecycle stages
  help      Show this help

Flags:
  --repo <r>           Repository path (owner/name)
  --pr <N>             PR number
  --pr-url <url>       Full PR URL (alternative to --repo + --pr)
  --agent <name>       Agent ID
  --trust <tier>       Agent trust tier: provisional|observed|trusted|veteran
  --stages <list>      Comma-separated stages to run (default: all)
  --dry-run            Show planned stages without executing
  --json               Structured JSON output
  --help, -h           Show this help

Exit codes:
  0  Lifecycle completed (APPROVED)
  1  Lifecycle rejected or escalated
  2  Invocation error`)
}

// runLifecycle is the entry point for `helix lifecycle`.
func runLifecycle(args []string, stdout, stderr io.Writer) int {
	flags, helpWanted, rc := parseLifecycleFlags(args)
	if rc != lcExitOK {
		return rc
	}
	if helpWanted {
		printLifecycleHelp(stdout)
		return lcExitOK
	}

	switch flags.subcommand {
	case "help":
		printLifecycleHelp(stdout)
		return lcExitOK
	case "run":
		return runLifecycleRun(flags, stdout, stderr)
	case "stages":
		return runLifecycleStages(flags, stdout)
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n", flags.subcommand)
		return lcExitError
	}
}

// parseStageList converts a comma-separated stage list to StageName values.
func parseStageList(s string) []coordinator.StageName {
	stageMap := map[string]coordinator.StageName{
		"cost":         coordinator.StageCostEstimate,
		"review":       coordinator.StageReview,
		"negotiation":  coordinator.StageNegotiation,
		"mergegate":    coordinator.StageMergeGate,
		"shadow":       coordinator.StageShadowDeploy,
		"surveillance": coordinator.StageSurveillance,
	}
	var stages []coordinator.StageName
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if stage, ok := stageMap[part]; ok {
			stages = append(stages, stage)
		}
	}
	return stages
}

// runLifecycleRun executes the lifecycle pipeline.
func runLifecycleRun(flags lcFlags, stdout, stderr io.Writer) int {
	// Validate required flags.
	if flags.repo == "" && flags.prURL == "" {
		fmt.Fprintf(stderr, "error: --repo <owner/name> or --pr-url <url> is required\n")
		return lcExitError
	}

	// Determine trust tier.
	tier := trust.TierProvisional
	switch flags.trustTier {
	case "provisional", "observed", "trusted", "veteran", "":
		if flags.trustTier != "" {
			tier = trust.TrustTier(flags.trustTier)
		}
	default:
		fmt.Fprintf(stderr, "error: invalid trust tier %q\n", flags.trustTier)
		return lcExitError
	}

	prURL := flags.prURL
	if prURL == "" {
		prURL = fmt.Sprintf("http://localhost:3030/%s/pulls/%d", flags.repo, flags.pr)
	}

	// Build the coordinator.
	var opts []coordinator.CoordinatorOption
	if flags.stages != "" {
		stages := parseStageList(flags.stages)
		if len(stages) > 0 {
			opts = append(opts, coordinator.WithStages(stages...))
		}
	}

	coord := coordinator.NewPRLifecycleCoordinator(opts...)

	req := coordinator.PRRequest{
		PRURL:     prURL,
		AgentID:   flags.agent,
		AgentTier: tier,
	}

	if flags.dryRun {
		return runLifecycleDryRun(flags, prURL, stdout)
	}

	// Execute with a timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	result := coord.Execute(ctx, req)

	// Output.
	if flags.jsonOut {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "error: encoding result: %v\n", err)
			return lcExitError
		}
		fmt.Fprintln(stdout, string(data))
	} else {
		printLifecycleResult(stdout, result)
	}

	if result.Decision == coordinator.DecisionApproved {
		return lcExitOK
	}
	return lcExitFail
}

// runLifecycleDryRun shows planned stages without executing.
func runLifecycleDryRun(flags lcFlags, prURL string, stdout io.Writer) int {
	stages := lcAllStages
	if flags.stages != "" {
		stageList := parseStageList(flags.stages)
		if len(stageList) > 0 {
			stageIDs := make(map[string]bool, len(stageList))
			for _, s := range stageList {
				stageIDs[string(s)] = true
			}
			var filtered []lcStage
			for _, s := range lcAllStages {
				if stageIDs[stageIDFromName(s.ID)] {
					filtered = append(filtered, s)
				}
			}
			stages = filtered
		}
	}

	if flags.jsonOut {
		type planItem struct {
			Stage       string `json:"stage"`
			Description string `json:"description"`
		}
		items := make([]planItem, len(stages))
		for i, s := range stages {
			items[i] = planItem{Stage: s.ID, Description: s.Description}
		}
		data, _ := json.MarshalIndent(map[string]any{
			"pr_url":     prURL,
			"agent":      flags.agent,
			"trust_tier": flags.trustTier,
			"dry_run":    true,
			"stages":     items,
		}, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return lcExitOK
	}

	fmt.Fprintf(stdout, "\n[DRY RUN] PR Lifecycle Plan\n")
	fmt.Fprintf(stdout, "   PR:      %s\n", prURL)
	fmt.Fprintf(stdout, "   Agent:   %s\n", flags.agent)
	fmt.Fprintf(stdout, "   Tier:    %s\n\n", flags.trustTier)

	fmt.Fprintf(stdout, "   Planned stages:\n")
	for i, s := range stages {
		fmt.Fprintf(stdout, "     %d. %-12s  %s\n", i+1, s.ID, s.Name)
	}
	fmt.Fprintf(stdout, "\n")
	return lcExitOK
}

// stageIDFromName maps a short stage ID to its coordinator constant string.
func stageIDFromName(shortID string) string {
	switch shortID {
	case "cost":
		return string(coordinator.StageCostEstimate)
	case "review":
		return string(coordinator.StageReview)
	case "negotiation":
		return string(coordinator.StageNegotiation)
	case "mergegate":
		return string(coordinator.StageMergeGate)
	case "shadow":
		return string(coordinator.StageShadowDeploy)
	case "surveillance":
		return string(coordinator.StageSurveillance)
	}
	return ""
}

// printLifecycleResult prints a human-readable lifecycle result.
func printLifecycleResult(w io.Writer, result *coordinator.LifecycleResult) {
	icon := "✗"
	if result.AllPassed() {
		icon = "✓"
	}

	fmt.Fprintf(w, "\n%s  PR LIFECYCLE: %s\n", icon, result.Decision)
	fmt.Fprintf(w, "   PR: %s  Agent: %s\n\n", result.PRURL, result.AgentID)

	fmt.Fprintf(w, "   %-18s  %-10s  %-12s  %s\n", "STAGE", "STATUS", "DURATION", "MESSAGE")
	fmt.Fprintf(w, "   %-18s  %-10s  %-12s  %s\n", "------------------", "----------", "------------", "-------")

	for _, s := range result.Stages {
		dur := s.Elapsed().Round(time.Millisecond).String()
		if dur == "0s" {
			dur = "-"
		}
		fmt.Fprintf(w, "   %-18s  %-10s  %-12s  %s\n", s.Name, s.Status, dur, s.Message)
	}

	fmt.Fprintf(w, "\n%s\n", result.Summary())
}

// runLifecycleStages lists all available stages.
func runLifecycleStages(flags lcFlags, stdout io.Writer) int {
	if flags.jsonOut {
		type stageItem struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		items := make([]stageItem, len(lcAllStages))
		for i, s := range lcAllStages {
			items[i] = stageItem(s)
		}
		data, _ := json.MarshalIndent(items, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return lcExitOK
	}

	fmt.Fprintf(stdout, "\nLifecycle Stages (%d)\n\n", len(lcAllStages))
	for _, s := range lcAllStages {
		fmt.Fprintf(stdout, "  %-12s  %s\n", s.ID, s.Name)
		fmt.Fprintf(stdout, "  %-12s  %s\n\n", "", s.Description)
	}
	return lcExitOK
}

// runLifecycleWithDryRun wraps runLifecycle with the global --dry-run flag.
func runLifecycleWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
	// If the global --dry-run flag was set but the user didn't already pass
	// --dry-run to the lifecycle subcommand itself, inject it so the
	// subcommand's own dry-run handler picks it up.
	if globalDryRun && !hasLifecycleDryRun(args) {
		args = append([]string{"--dry-run"}, args...)
	}
	rc := runLifecycle(args, stdout, stderr)
	if rc != 0 && rc != lcExitFail {
		return errExit{code: rc}
	}
	return nil
}

// hasLifecycleDryRun reports whether --dry-run appears in the args slice.
func hasLifecycleDryRun(args []string) bool {
	for _, a := range args {
		if a == "--dry-run" {
			return true
		}
	}
	return false
}
