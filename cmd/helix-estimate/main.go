// Command helix-estimate is the Helix pre-flight cost estimator CLI.
//
// It estimates the token cost of an agent task before execution, checks the
// estimate against an agent's remaining weekly budget, and either approves,
// denies, or escalates for human approval. See specs/cost-estimator.md for the
// full design.
//
// Three subcommands:
//
//	helix-estimate estimate <task-description>   project a task's cost
//	helix-estimate check <agent> <task>          estimate + budget gate
//	helix-estimate report [agent]                show budget report
//
// Import constraint: stdlib + github.com/spf13/cobra + gopkg.in/yaml.v3 only.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"
	"github.com/totalwindupflightsystems/helix/pkg/estimate"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		// Errors that should map to specific exit codes are handled inside the
		// command RunE via os.Exit; anything reaching here is a generic failure.
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// Root command
// ---------------------------------------------------------------------------

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "helix-estimate",
		Short: "Helix pre-flight cost estimator",
		Long: `helix-estimate — Helix Pre-Flight Cost Estimator

Estimates the token cost of an agent task BEFORE execution begins and checks it
against the agent's remaining weekly budget. Estimation is cache-aware: fresh
input is charged at full price while cache-hit tokens are 10x cheaper.

Subcommands:
  estimate <task>       Project a task's cost (no budget enforcement by default)
  check <agent> <task>  Estimate + enforce the agent's budget gate
  report [agent]        Show an agent's budget report for a period

Run "helix-estimate <subcommand> --help" for per-command flags.`,
	}
	root.AddCommand(newEstimateCmd(), newCheckCmd(), newReportCmd())
	return root
}

// ---------------------------------------------------------------------------
// Shared options + helpers
// ---------------------------------------------------------------------------

// estimateOptions holds the flags shared by the estimate and check subcommands.
type estimateOptions struct {
	taskType      string
	model         string
	provider      string
	filesChanged  int
	specFile      string
	agents        int
	maxIterations int
	diffLines     int
	dryRun        bool
	output        string
	tier          string
	pricingPath   string
	friendsPath   string
}

// addFlags registers the shared estimation flags on a cobra command.
func (o *estimateOptions) addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.taskType, "task-type", "code",
		"task type: spec|code|review|refactor|test")
	cmd.Flags().StringVar(&o.model, "model", "", "model name (e.g. deepseek-v4-pro)")
	cmd.Flags().StringVar(&o.provider, "provider", "", "provider name (e.g. deepseek)")
	cmd.Flags().IntVar(&o.filesChanged, "files-changed", 1, "estimated files to modify")
	cmd.Flags().StringVar(&o.specFile, "spec-file", "", "path to spec file (for spec tasks)")
	cmd.Flags().IntVar(&o.agents, "agents", 1, "number of agents (capped at 5)")
	cmd.Flags().IntVar(&o.maxIterations, "max-iterations", 0,
		"max reasoning iterations (0 = use task-type default)")
	cmd.Flags().IntVar(&o.diffLines, "diff-lines", 0, "estimated diff lines")
	cmd.Flags().BoolVar(&o.dryRun, "dry-run", false, "estimate without checking budget")
	cmd.Flags().StringVar(&o.output, "output", "table", "output format: json|table|summary")
	cmd.Flags().StringVar(&o.tier, "tier", "pro", "agent tier: pro|flash")
	cmd.Flags().StringVar(&o.pricingPath, "pricing", defaultPricingPath(),
		"path to pricing.yaml")
}

// defaultPricingPath returns the production pricing location, falling back to
// the testdata fixture when ~/.helix/pricing.yaml is absent.
func defaultPricingPath() string {
	home, err := os.UserHomeDir()
	if err == nil {
		p := home + "/.helix/pricing.yaml"
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "pkg/estimate/testdata/pricing.yaml"
}

// defaultFriendsPath returns the known-friends.json budget registry location.
func defaultFriendsPath() string {
	const prod = "/opt/hermes-demo/.hermes/h4f/known-friends.json"
	if _, err := os.Stat(prod); err == nil {
		return prod
	}
	return "pkg/estimate/testdata/known-friends.json"
}

// toTaskDesc converts the CLI options + description into an estimate.TaskDesc.
func (o *estimateOptions) toTaskDesc(desc string) estimate.TaskDesc {
	return estimate.TaskDesc{
		Description:   desc,
		Type:          estimate.TaskType(o.taskType),
		Model:         o.model,
		Provider:      o.provider,
		FilesChanged:  o.filesChanged,
		MaxIterations: o.maxIterations,
		DiffLines:     o.diffLines,
		Agents:        o.agents,
	}
}

// loadPricing wraps estimate.LoadPricing with a config-error exit on failure.
func loadPricing(path string) *estimate.PricingYAML {
	p, err := estimate.LoadPricing(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "CONFIG_ERROR: %s\n", err)
		os.Exit(estimate.ExitConfigError)
	}
	return p
}

// estimateTier resolves the estimator tier to use. When --tier is set it wins;
// otherwise the agent's tier (possibly "cold" for new agents) is used.
func estimateTier(optTier, agentTier string, coldStart bool) string {
	if optTier != "" && optTier != "pro" {
		return optTier // explicit non-pro tier (e.g. --tier flash)
	}
	if coldStart {
		return "cold"
	}
	if agentTier != "" {
		return agentTier
	}
	return "pro"
}

// ---------------------------------------------------------------------------
// estimate subcommand
// ---------------------------------------------------------------------------

func newEstimateCmd() *cobra.Command {
	opts := &estimateOptions{}
	cmd := &cobra.Command{
		Use:   "estimate <task-description>",
		Short: "Project the token cost of a task",
		Long: `Estimate the pre-flight token cost of a task using cache-aware pricing.

By default this only projects the cost — it does NOT enforce a budget. Use the
check subcommand to gate a task against an agent's weekly budget. With --dry-run
the command explicitly skips any budget consideration and exits with code 10.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEstimate(opts, args[0])
		},
	}
	opts.addFlags(cmd)
	return cmd
}

func runEstimate(opts *estimateOptions, desc string) error {
	if err := validateEstimateOpts(opts); err != nil {
		fmt.Fprintf(os.Stderr, "ESTIMATION_FAILED: %s\n", err)
		os.Exit(estimate.ExitEstimationFailed)
	}
	pricing := loadPricing(opts.pricingPath)

	tier := opts.tier
	if tier == "" {
		tier = "pro"
	}
	est := estimate.NewEstimator(pricing, tier)
	cost, err := est.Estimate(opts.toTaskDesc(desc))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ESTIMATION_FAILED: %s\n", err)
		os.Exit(estimate.ExitEstimationFailed)
	}

	renderEstimate(os.Stdout, desc, tier, cost, opts.output)

	if opts.dryRun {
		fmt.Fprintln(os.Stdout, "\nDRY RUN — no budget check performed.")
		os.Exit(estimate.ExitDryRun)
	}
	// Non-dry-run estimate exits 0 (no enforcement in the estimate command).
	return nil
}

// ---------------------------------------------------------------------------
// check subcommand
// ---------------------------------------------------------------------------

type checkOptions struct {
	*estimateOptions
	autoApprove  bool
	requireHuman bool
}

func newCheckCmd() *cobra.Command {
	opts := &checkOptions{estimateOptions: &estimateOptions{}}
	cmd := &cobra.Command{
		Use:   "check <agent-name> <task-description>",
		Short: "Estimate a task and enforce the agent's budget gate",
		Long: `Estimate a task's cost and check it against the named agent's remaining
weekly budget. The decision (AUTO_APPROVED, AUTO_APPROVED_WITH_WARNING, BLOCKED,
or ESCALATED) is printed and the process exits with the matching code from the
error taxonomy (spec §11).

With --require-human the decision is always treated as needing explicit human
approval regardless of the budget arithmetic.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheck(opts, args[0], args[1])
		},
	}
	opts.addFlags(cmd)
	cmd.Flags().BoolVar(&opts.autoApprove, "auto-approve", true,
		"auto-approve tasks within budget")
	cmd.Flags().BoolVar(&opts.requireHuman, "require-human", false,
		"require explicit human approval even if within budget")
	cmd.Flags().StringVar(&opts.friendsPath, "known-friends", defaultFriendsPath(),
		"path to known-friends.json budget registry")
	return cmd
}

func runCheck(opts *checkOptions, agentName, desc string) error {
	if err := validateEstimateOpts(opts.estimateOptions); err != nil {
		fmt.Fprintf(os.Stderr, "ESTIMATION_FAILED: %s\n", err)
		os.Exit(estimate.ExitEstimationFailed)
	}
	pricing := loadPricing(opts.pricingPath)

	budget, err := loadAgentBudget(opts.friendsPath, agentName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "CONFIG_ERROR: %s\n", err)
		os.Exit(estimate.ExitConfigError)
	}

	coldStart := estimate.IsNewAgent(budget)
	tier := estimateTier(opts.tier, budget.Tier, coldStart)
	est := estimate.NewEstimator(pricing, tier)
	cost, err := est.Estimate(opts.toTaskDesc(desc))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ESTIMATION_FAILED: %s\n", err)
		os.Exit(estimate.ExitEstimationFailed)
	}

	decision := estimate.CheckBudget(budget, cost)
	if opts.requireHuman {
		decision.Approved = false
		decision.Status = estimate.StatusEscalated
		decision.Reason = "Human approval required (--require-human). " + decision.Reason
	}

	renderCheck(os.Stdout, agentName, budget, cost, decision, opts.output)
	os.Exit(estimate.ApprovalExitCode(decision))
	return nil
}

// ---------------------------------------------------------------------------
// report subcommand
// ---------------------------------------------------------------------------

type reportOptions struct {
	friendsPath string
	period      string
	format      string
	pricingPath string
}

func newReportCmd() *cobra.Command {
	opts := &reportOptions{}
	cmd := &cobra.Command{
		Use:   "report [agent-name]",
		Short: "Show an agent's budget report",
		Long: `Show the weekly budget report for one agent (or all agents when no name is
given). In v1 the report reflects current-period budget data from
known-friends.json; per-task history and drift metrics land with GitReins
reconciliation in a follow-up.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agent := ""
			if len(args) == 1 {
				agent = args[0]
			}
			return runReport(opts, agent)
		},
	}
	cmd.Flags().StringVar(&opts.friendsPath, "known-friends", defaultFriendsPath(),
		"path to known-friends.json budget registry")
	cmd.Flags().StringVar(&opts.period, "period", "current",
		"budget period: current|last|all")
	cmd.Flags().StringVar(&opts.format, "format", "table", "output format: json|table")
	cmd.Flags().StringVar(&opts.pricingPath, "pricing", defaultPricingPath(),
		"path to pricing.yaml")
	return cmd
}

func runReport(opts *reportOptions, agent string) error {
	friends, err := loadAllBudgets(opts.friendsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "CONFIG_ERROR: %s\n", err)
		os.Exit(estimate.ExitConfigError)
	}
	if opts.period != "current" && opts.period != "last" && opts.period != "all" {
		fmt.Fprintf(os.Stderr, "ESTIMATION_FAILED: invalid --period %q (want current|last|all)\n", opts.period)
		os.Exit(estimate.ExitEstimationFailed)
	}

	if agent != "" {
		b, ok := friends[agent]
		if !ok {
			fmt.Fprintf(os.Stderr, "CONFIG_ERROR: agent %q not found in %s\n", agent, opts.friendsPath)
			os.Exit(estimate.ExitConfigError)
		}
		renderReport(os.Stdout, agent, b, opts.period, opts.format)
		return nil
	}
	// No agent: report all, sorted by name.
	names := make([]string, 0, len(friends))
	for n := range friends {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		renderReport(os.Stdout, n, friends[n], opts.period, opts.format)
		fmt.Fprintln(os.Stdout)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func validateEstimateOpts(o *estimateOptions) error {
	tt := estimate.TaskType(o.taskType)
	if o.taskType != "" && !tt.Valid() {
		return fmt.Errorf("invalid --task-type %q", o.taskType)
	}
	if o.model == "" {
		return fmt.Errorf("--model is required")
	}
	if o.provider == "" {
		return fmt.Errorf("--provider is required")
	}
	if o.output != "json" && o.output != "table" && o.output != "summary" {
		return fmt.Errorf("invalid --output %q (want json|table|summary)", o.output)
	}
	return nil
}

// ---------------------------------------------------------------------------
// known-friends.json budget loading
// ---------------------------------------------------------------------------

// friendBudget is the cost-relevant slice of an agent entry in
// known-friends.json (spec §3.1). It deliberately ignores identity-only fields.
type friendBudget struct {
	DisplayName    string  `json:"display_name"`
	Status         string  `json:"status"`
	Tier           string  `json:"tier"`
	BudgetWeekly   float64 `json:"budget_usd_weekly"`
	BudgetUsed     float64 `json:"budget_used_usd"`
	TrustLevel     int     `json:"trust_level"`
	TasksCompleted int     `json:"tasks_completed"`
}

// toBudgetInfo projects a friendBudget into the estimate.BudgetInfo used by the
// approval gates.
func (f friendBudget) toBudgetInfo(name string) estimate.BudgetInfo {
	tier := f.Tier
	if tier == "" {
		tier = "pro"
	}
	return estimate.BudgetInfo{
		AgentName:      name,
		Tier:           tier,
		BudgetWeekly:   f.BudgetWeekly,
		BudgetUsed:     f.BudgetUsed,
		TrustLevel:     f.TrustLevel,
		TasksCompleted: f.TasksCompleted,
	}
}

type friendsFile struct {
	Version int                     `json:"version"`
	Agents  map[string]friendBudget `json:"agents"`
}

func loadAllBudgets(path string) (map[string]friendBudget, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read known-friends %q: %w", path, err)
	}
	var ff friendsFile
	// Support both the wrapped {"agents": {...}} shape and a bare {name: {...}} map.
	if err := json.Unmarshal(data, &ff); err == nil && len(ff.Agents) > 0 {
		return ff.Agents, nil
	}
	bare := map[string]friendBudget{}
	if err := json.Unmarshal(data, &bare); err != nil {
		return nil, fmt.Errorf("parse known-friends %q: %w", path, err)
	}
	return bare, nil
}

func loadAgentBudget(path, name string) (estimate.BudgetInfo, error) {
	all, err := loadAllBudgets(path)
	if err != nil {
		return estimate.BudgetInfo{}, err
	}
	f, ok := all[name]
	if !ok {
		return estimate.BudgetInfo{}, fmt.Errorf("agent %q not found in %s", name, path)
	}
	return f.toBudgetInfo(name), nil
}

// ---------------------------------------------------------------------------
// Output rendering
// ---------------------------------------------------------------------------

// estimateResult is the JSON shape for the estimate/check commands.
type estimateResult struct {
	Description string               `json:"description"`
	Tier        string               `json:"tier"`
	Cost        estimate.CostEstimate `json:"cost"`
}

func renderEstimate(w *os.File, desc, tier string, cost estimate.CostEstimate, format string) {
	switch format {
	case "json":
		b, _ := json.MarshalIndent(estimateResult{Description: desc, Tier: tier, Cost: cost}, "", "  ")
		fmt.Fprintln(w, string(b))
	case "summary":
		fmt.Fprintf(w, "ESTIMATE: $%.2f (%s/%s, %d input tokens, %.0f%% cache hit)\n",
			cost.CostTotal, cost.Provider, cost.Model, cost.Tokens.TotalInput, cost.Tokens.CacheHitRatio*100)
	default:
		renderEstimateTable(w, desc, tier, cost)
	}
}

func renderEstimateTable(w *os.File, desc, tier string, cost estimate.CostEstimate) {
	t := cost.Tokens
	fmt.Fprintf(w, "TASK:           %s\n", desc)
	fmt.Fprintf(w, "TYPE/MODEL:     %s (%s)\n", cost.Model, cost.Provider)
	fmt.Fprintf(w, "TIER:           %s (%.0f%% cache hit)\n\n", tier, t.CacheHitRatio*100)
	fmt.Fprintln(w, "TOKEN ESTIMATE:")
	fmt.Fprintf(w, "  Fresh input:   %10d\n", t.FreshInput)
	fmt.Fprintf(w, "  Cache hits:    %10d\n", t.CacheHits)
	fmt.Fprintf(w, "  Cache writes:  %10d\n", t.CacheWrites)
	fmt.Fprintf(w, "  Output:        %10d\n\n", t.Output)
	fmt.Fprintln(w, "COST ESTIMATE:")
	fmt.Fprintf(w, "  Input cost:    $%.6f\n", cost.CostInput)
	fmt.Fprintf(w, "  Output cost:   $%.6f\n", cost.CostOutput)
	fmt.Fprintf(w, "  ----------------------------\n")
	fmt.Fprintf(w, "  TOTAL:         $%.2f\n", cost.CostTotal)
}

// checkResult is the JSON shape for the check command.
type checkResult struct {
	Agent     string                  `json:"agent"`
	Cost      estimate.CostEstimate   `json:"cost"`
	Budget    estimate.BudgetInfo     `json:"budget"`
	Decision  estimate.ApprovalDecision `json:"decision"`
}

func renderCheck(w *os.File, agent string, budget estimate.BudgetInfo,
	cost estimate.CostEstimate, decision estimate.ApprovalDecision, format string) {
	switch format {
	case "json":
		b, _ := json.MarshalIndent(checkResult{
			Agent: agent, Cost: cost, Budget: budget, Decision: decision,
		}, "", "  ")
		fmt.Fprintln(w, string(b))
	case "summary":
		emoji := statusEmoji(decision.Status)
		remaining := estimate.RemainingBudget(budget)
		fmt.Fprintf(w, "%s %s: $%.2f (remaining $%.2f) — %s\n",
			emoji, decision.Status, cost.CostTotal, remaining, decision.Reason)
	default:
		remaining := estimate.RemainingBudget(budget)
		fmt.Fprintf(w, "ESTIMATED COST:   $%.2f\n", cost.CostTotal)
		fmt.Fprintf(w, "BUDGET REMAINING: $%.2f of $%.2f (weekly)\n", remaining, budget.BudgetWeekly)
		fmt.Fprintf(w, "DECISION:         %s %s\n", decision.Status, statusEmoji(decision.Status))
		fmt.Fprintf(w, "REASON:           %s\n", decision.Reason)
	}
}

func statusEmoji(s estimate.ApprovalStatus) string {
	switch s {
	case estimate.StatusAutoApproved, estimate.StatusAutoApprovedWithWarning:
		return "✅"
	case estimate.StatusEscalated:
		return "⚠️"
	default:
		return "❌"
	}
}

// reportResult is the JSON shape for the report command.
type reportResult struct {
	Agent   string                `json:"agent"`
	Budget  estimate.BudgetInfo   `json:"budget"`
	Period  string                `json:"period"`
}

func renderReport(w *os.File, name string, f friendBudget, period, format string) {
	bi := f.toBudgetInfo(name)
	remaining := estimate.RemainingBudget(bi)
	pct := 0.0
	if bi.BudgetWeekly > 0 {
		pct = bi.BudgetUsed / bi.BudgetWeekly * 100
	}
	switch format {
	case "json":
		b, _ := json.MarshalIndent(reportResult{Agent: name, Budget: bi, Period: period}, "", "  ")
		fmt.Fprintln(w, string(b))
	default:
		display := f.DisplayName
		if display == "" {
			display = name
		}
		fmt.Fprintf(w, "AGENT:       %s (%s tier)\n", display, bi.Tier)
		fmt.Fprintf(w, "PERIOD:      %s\n", periodLabel(period))
		fmt.Fprintf(w, "BUDGET:      $%.2f/week\n", bi.BudgetWeekly)
		fmt.Fprintf(w, "SPENT:       $%.2f (%.1f%%)\n", bi.BudgetUsed, pct)
		fmt.Fprintf(w, "REMAINING:   $%.2f\n", remaining)
		fmt.Fprintf(w, "TASKS:       %d completed%s\n", bi.TasksCompleted,
			coldStartNote(bi.TasksCompleted))
	}
}

func periodLabel(period string) string {
	switch period {
	case "current":
		return "current week"
	case "last":
		return "last week (no historical data in v1)"
	case "all":
		return "all time (no historical data in v1)"
	default:
		return period
	}
}

func coldStartNote(tasks int) string {
	if tasks < 10 {
		return fmt.Sprintf(" [cold start — %d/10, 0%% cache hit]", tasks)
	}
	return ""
}
