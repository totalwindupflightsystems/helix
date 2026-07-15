// Command helix-verify provides the production verification CLI for shadow
// deployment, canary promotion, and differential behavior analysis.
//
// Subcommands:
//
//	helix-verify shadow   Launch shadow deployment with traffic mirroring
//	helix-verify canary   Promote shadow to canary or advance canary step
//	helix-verify status   Show current deployment state for an agent
//	helix-verify rollback Force rollback of an active deployment
//
// See specs/plans/phase-9-10-deploy-monitor.md §9.1 for shadow verification
// and §9.2 for canary deployment by trust tier.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/totalwindupflightsystems/helix/internal/observability"
	"github.com/totalwindupflightsystems/helix/pkg/verify"
)

func main() {
	if _, err := observability.Init(observability.Options{App: "helix-verify"}); err != nil {
		fmt.Fprintf(os.Stderr, "helix-verify: failed to initialise logger: %v\n", err)
		os.Exit(1)
	}

	rootCmd := &cobra.Command{
		Use:   "helix-verify",
		Short: "Production verification — shadow, canary, and differential analysis",
		Long: `helix-verify manages the post-merge production verification pipeline:

  shadow  — Launch a dark-traffic shadow deployment, mirror production
            requests, and produce a differential report comparing
            shadow behavior against the production baseline.
  canary  — Promote a shadow-passed deployment to canary (gradual
            traffic ramp) or advance an active canary to the next step.
  status  — Show the current deployment lifecycle state for an agent.
  rollback — Force-rollback an active deployment with a structured reason.

All commands operate against the in-memory ShadowManager from pkg/verify.
Trust-tier-specific schedules (Provisional 24h → Veteran 2h) are respected
automatically via CanarySchedule(tier).`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.AddCommand(
		newShadowCmd(),
		newCanaryCmd(),
		newStatusCmd(),
		newRollbackCmd(),
	)

	rc := executeRoot(rootCmd)
	os.Exit(rc)
}

func executeRoot(rootCmd *cobra.Command) int {
	sub := "helix-verify"
	if args := rootCmd.Flags().Args(); len(args) > 0 {
		sub = "helix-verify:" + args[0]
	}
	err := observability.Run(sub, func() error {
		return rootCmd.Execute()
	})
	if err == nil {
		return 0
	}
	fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
	return 1
}

// ---------------------------------------------------------------------------
// Shared state — the ShadowManager lives for the duration of the process.
// ---------------------------------------------------------------------------

var manager = verify.NewShadowManager()

// ---------------------------------------------------------------------------
// shadow
// ---------------------------------------------------------------------------

type shadowFlags struct {
	agent    string
	tier     string
	duration time.Duration
	json     bool
}

var shadowF = &shadowFlags{}

func newShadowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shadow",
		Short: "Launch a shadow deployment for dark-traffic verification",
		Long: `Launches a shadow (dark-launch) deployment for the specified agent.
Production traffic is mirrored to the shadow instance; responses are
discarded. After the observation window elapses, metrics are evaluated
against the production baseline and a differential report is produced.

Observation window defaults to the trust tier's CanarySchedule duration:
  provisional: 24h   observed: 12h   trusted: 6h   veteran: 2h

The --agent flag is required. Use --duration to override the default
observation window (e.g., --duration 1h for quick verification).`,
		RunE: runShadow,
	}
	cmd.Flags().StringVar(&shadowF.agent, "agent", "", "Agent ID (required)")
	cmd.Flags().StringVar(&shadowF.tier, "tier", "provisional", "Trust tier: provisional|observed|trusted|veteran")
	cmd.Flags().DurationVar(&shadowF.duration, "duration", 0, "Override observation window (e.g., 24h, 1h30m)")
	cmd.Flags().BoolVar(&shadowF.json, "json", false, "Output differential report as JSON")
	_ = cmd.MarkFlagRequired("agent")
	return cmd
}

func runShadow(cmd *cobra.Command, args []string) error {
	cfg := verify.DefaultShadowConfig()
	if shadowF.duration > 0 {
		cfg.ObservationWindow = shadowF.duration
	}

	// Capture a (simulated) production baseline. In production this
	// would come from live metrics; for the CLI we accept zero-valued
	// baseline as "no prior metrics — shadow must pass on absolute
	// thresholds."
	baseline := verify.MetricsSnapshot{
		SuccessRate: 1.0,
		Timestamp:   time.Now().UTC(),
	}

	d, err := manager.LaunchShadow(shadowF.agent, shadowF.tier, baseline, cfg)
	if err != nil {
		return fmt.Errorf("launch shadow: %w", err)
	}

	fmt.Printf("Shadow launched for agent %q (tier: %s)\n", d.AgentID, d.Tier)
	fmt.Printf("  State:      %s\n", d.GetState())
	fmt.Printf("  Launched:   %s\n", d.LaunchedAt.Format(time.RFC3339))
	window, _ := verify.CanarySchedule(shadowF.tier)
	if cfg.ObservationWindow > 0 {
		window = cfg.ObservationWindow
	}
	fmt.Printf("  Window:     %s\n", window.Round(time.Second))
	fmt.Printf("  Evaluate at: %s\n", d.LaunchedAt.Add(window).Format(time.RFC3339))

	remaining := manager.ObservationWindowRemaining(shadowF.agent)
	if remaining > 0 {
		fmt.Printf("\nObservation window remaining: %s\n", remaining.Round(time.Second))
		fmt.Println("Run 'helix-verify status --agent " + shadowF.agent + "' to check, then re-run shadow to evaluate.")
		return nil
	}

	// Window elapsed — evaluate.
	report, evalErr := manager.EvaluateShadow(shadowF.agent)
	if evalErr != nil {
		return fmt.Errorf("evaluate shadow: %w", evalErr)
	}

	fmt.Println()
	printDifferentialReport(report, shadowF.json)
	return nil
}

// ---------------------------------------------------------------------------
// canary
// ---------------------------------------------------------------------------

type canaryFlags struct {
	agent string
	step  int
	json  bool
}

var canaryF = &canaryFlags{}

func newCanaryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "canary",
		Short: "Promote or advance a canary deployment",
		Long: `Promotes a shadow-passed deployment to canary, or advances an
active canary to the next traffic step.

Without --step: promotes shadow→canary (initial traffic allocation per
trust tier — 1% for Provisional/Observed, 10% for Trusted, 25% for Veteran).

With --step N: advances to canary step N. The step is validated against
the tier's CanarySchedule. Step 0 is the initial canary promotion.

Use 'helix-verify status --agent <id>' to see the current canary step.`,
		RunE: runCanary,
	}
	cmd.Flags().StringVar(&canaryF.agent, "agent", "", "Agent ID (required)")
	cmd.Flags().IntVar(&canaryF.step, "step", -1, "Canary step to advance to (-1 = promote to canary)")
	cmd.Flags().BoolVar(&canaryF.json, "json", false, "Output step details as JSON")
	_ = cmd.MarkFlagRequired("agent")
	return cmd
}

func runCanary(cmd *cobra.Command, args []string) error {
	d := manager.GetDeployment(canaryF.agent)
	if d == nil {
		return fmt.Errorf("no deployment for agent %q — run 'helix-verify shadow --agent %s' first", canaryF.agent, canaryF.agent)
	}

	state := d.GetState()

	switch {
	case state == verify.StateShadowPassed && canaryF.step < 0:
		// Promote to canary (initial step).
		promoted, err := manager.PromoteToCanary(canaryF.agent)
		if err != nil {
			return fmt.Errorf("promote to canary: %w", err)
		}
		step, _ := manager.CurrentCanaryStep(canaryF.agent)
		fmt.Printf("Canary promoted for agent %q\n", canaryF.agent)
		fmt.Printf("  State:      %s\n", promoted.GetState())
		fmt.Printf("  Step:       %d/%d\n", step.Step, len(canaryScheduleSteps(d.Tier)))
		fmt.Printf("  Traffic:    %.0f%%\n", step.TrafficPct*100)
		fmt.Printf("  Duration:   %s\n", step.Duration.Round(time.Second))
		return nil

	case state == verify.StateCanaried:
		// Advance canary step.
		if canaryF.step < 0 {
			// Advance one step.
			step, final, err := manager.AdvanceCanary(canaryF.agent)
			if err != nil {
				return fmt.Errorf("advance canary: %w", err)
			}
			fmt.Printf("Canary advanced for agent %q\n", canaryF.agent)
			fmt.Printf("  Traffic:   %.0f%%\n", step.TrafficPct*100)
			if final {
				fmt.Println("  FINAL — deployment fully promoted to 100% traffic")
			}
			return nil
		}
		return fmt.Errorf("step targeting not yet implemented — use without --step to advance one step")

	default:
		return fmt.Errorf("agent %q is in state %s — must be %s (shadow passed) or %s (canary active)", canaryF.agent, state, verify.StateShadowPassed, verify.StateCanaried)
	}
}

// ---------------------------------------------------------------------------
// status
// ---------------------------------------------------------------------------

type statusFlags struct {
	agent string
	all   bool
}

var statusF = &statusFlags{}

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show deployment lifecycle state",
		Long: `Shows the current state of an agent's deployment through the
shadow→canary→promoted→rolled_back lifecycle.

With --all: lists all known deployments.`,
		RunE: runStatus,
	}
	cmd.Flags().StringVar(&statusF.agent, "agent", "", "Agent ID to check")
	cmd.Flags().BoolVar(&statusF.all, "all", false, "List all deployments")
	return cmd
}

func runStatus(cmd *cobra.Command, args []string) error {
	if statusF.all {
		ids := manager.ListDeployments()
		if len(ids) == 0 {
			fmt.Println("No deployments.")
			return nil
		}
		for _, id := range ids {
			d := manager.GetDeployment(id)
			if d == nil {
				continue
			}
			printDeploymentStatus(d)
		}
		return nil
	}

	if statusF.agent == "" {
		return fmt.Errorf("--agent is required (or use --all)")
	}

	d := manager.GetDeployment(statusF.agent)
	if d == nil {
		fmt.Printf("No deployment for agent %q.\n", statusF.agent)
		return nil
	}
	printDeploymentStatus(d)
	return nil
}

// ---------------------------------------------------------------------------
// rollback
// ---------------------------------------------------------------------------

type rollbackFlags struct {
	agent  string
	reason string
}

var rollbackF = &rollbackFlags{}

func newRollbackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback",
		Short: "Force-rollback an active deployment",
		Long: `Immediately rolls back an active deployment (shadowing or canaried)
with the specified reason.

This simulates an auto-rollback trigger — the deployment transitions to
rolled_back and the reason is recorded for the breach report.`,
		RunE: runRollback,
	}
	cmd.Flags().StringVar(&rollbackF.agent, "agent", "", "Agent ID (required)")
	cmd.Flags().StringVar(&rollbackF.reason, "reason", "", "Rollback reason (required)")
	_ = cmd.MarkFlagRequired("agent")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}

func runRollback(cmd *cobra.Command, args []string) error {
	if err := manager.AutoRollback(rollbackF.agent, rollbackF.reason); err != nil {
		return fmt.Errorf("rollback: %w", err)
	}
	fmt.Printf("Rollback triggered for agent %q\n", rollbackF.agent)
	fmt.Printf("  Reason: %s\n", rollbackF.reason)
	return nil
}

// ---------------------------------------------------------------------------
// Output helpers
// ---------------------------------------------------------------------------

func printDifferentialReport(report *verify.DifferentialReport, asJSON bool) {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
		return
	}

	fmt.Println("═══ Shadow Differential Report ═══")
	fmt.Printf("Production: success=%.4f  P99=%.1fms  P50=%.1fms  errors=%d\n",
		report.Production.SuccessRate, report.Production.P99LatencyMs,
		report.Production.P50LatencyMs, report.Production.ErrorCount)
	fmt.Printf("Shadow:     success=%.4f  P99=%.1fms  P50=%.1fms  errors=%d\n",
		report.Shadow.SuccessRate, report.Shadow.P99LatencyMs,
		report.Shadow.P50LatencyMs, report.Shadow.ErrorCount)
	fmt.Println()

	for _, delta := range report.Deltas {
		marker := "✅"
		if !delta.Passed {
			marker = "❌"
		}
		fmt.Printf("  %s %-20s  prod=%-8.4f  shadow=%-8.4f  delta=%-8.4f  %s\n",
			marker, delta.Metric, delta.Prod, delta.Shadow, delta.Delta, delta.Reason)
	}

	fmt.Println()
	if report.AllPassed {
		fmt.Println("Result: ✅ ALL CHECKS PASSED — shadow is ready for canary promotion.")
	} else {
		fmt.Println("Result: ❌ SHADOW FAILED — auto-rollback triggered.")
		if report.BlockReason != "" {
			fmt.Printf("  Block reason: %s\n", report.BlockReason)
		}
	}
}

func printDeploymentStatus(d *verify.ShadowDeployment) {
	state := d.GetState()
	fmt.Printf("Agent: %s\n", d.AgentID)
	fmt.Printf("  Tier:       %s\n", d.Tier)
	fmt.Printf("  State:      %s\n", state)
	fmt.Printf("  Launched:   %s\n", d.LaunchedAt.Format(time.RFC3339))
	if !d.EvaluatedAt.IsZero() {
		fmt.Printf("  Evaluated:  %s\n", d.EvaluatedAt.Format(time.RFC3339))
	}
	if !d.PromotedAt.IsZero() {
		fmt.Printf("  Promoted:   %s\n", d.PromotedAt.Format(time.RFC3339))
	}
	if !d.RolledBackAt.IsZero() {
		fmt.Printf("  Rolled back: %s\n", d.RolledBackAt.Format(time.RFC3339))
		fmt.Printf("  Reason:      %s\n", d.RollbackReason)
	}
	if state == verify.StateCanaried || state == verify.StatePromoted {
		step, err := manager.CurrentCanaryStep(d.AgentID)
		if err == nil {
			_, steps := verify.CanarySchedule(d.Tier)
			fmt.Printf("  Canary step: %d/%d (%.0f%% traffic, %s)\n",
				step.Step, len(steps), step.TrafficPct*100, step.Duration.Round(time.Second))
		}
	}
	remaining := manager.ObservationWindowRemaining(d.AgentID)
	if remaining > 0 {
		fmt.Printf("  Obs window remaining: %s\n", remaining.Round(time.Second))
	}
	fmt.Println()
}

func canaryScheduleSteps(tier string) []verify.CanaryStep {
	_, steps := verify.CanarySchedule(tier)
	return steps
}
