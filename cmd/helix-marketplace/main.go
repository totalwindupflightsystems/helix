// Command helix-marketplace is the Helix Agent Marketplace CLI.
//
// It provides four subcommands for browsing, searching, inspecting, and rating
// agents in the local marketplace registry (spec §3.3):
//
//	helix-marketplace list       list agents with filters
//	helix-marketplace show <nm>  show detailed agent profile
//	helix-marketplace search     search by capability and trust
//	helix-marketplace rate <nm>  submit a human rating
//
// Import constraint: stdlib + github.com/spf13/cobra + gopkg.in/yaml.v3 only.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/totalwindupflightsystems/helix/pkg/marketplace"
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
		Use:   "helix-marketplace",
		Short: "Helix Agent Marketplace",
		Long: `helix-marketplace — Helix Agent Marketplace

The discoverable registry where agents are listed, searched, rated, and selected
for work items. Axiom queries the marketplace to pick the right agents for each
work item.

Subcommands:
  list                List agents with filters
  show <agent-name>   Show detailed agent profile
  search              Search by capability and trust threshold
  rate <agent-name>   Submit a human rating (1-5 stars)
  review <agent-name> Submit a human review (1-5 stars, alias for rate)

Run "helix-marketplace <subcommand> --help" for per-command flags.`,
	}
	root.AddCommand(newListCmd(), newShowCmd(), newSearchCmd(), newRateCmd(), newReviewCmd())
	return root
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// defaultMarketplacePath returns the production marketplace location, falling
// back to the testdata fixture when ~/.helix/marketplace is absent.
func defaultMarketplacePath() string {
	home, err := os.UserHomeDir()
	if err == nil {
		p := home + "/.helix/marketplace"
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "pkg/marketplace/testdata"
}

// loadRegistry creates a Registry from the given marketplace path, exiting on
// error.
func loadRegistry(path string) *marketplace.Registry {
	r, err := marketplace.NewRegistry(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "CONFIG_ERROR: %s\n", err)
		os.Exit(marketplace.ExitManifestInvalid)
	}
	return r
}

// exitOnError maps a marketplace.ExitError to the correct exit code. Non-Exit
// errors are treated as generic failures (exit 1).
func exitOnError(err error) {
	if err == nil {
		return
	}
	if ee, ok := err.(*marketplace.ExitError); ok {
		fmt.Fprintln(os.Stderr, ee.Message)
		os.Exit(ee.Code)
	}
	fmt.Fprintln(os.Stderr, "Error:", err)
	os.Exit(1)
}

// ratingStars renders a star string for a numeric rating (e.g. 4.7 → "★★★★½").
func ratingStars(rating float64) string {
	full := int(rating)
	half := ""
	remainder := rating - float64(full)
	if remainder >= 0.25 && remainder < 0.75 {
		half = "½"
	} else if remainder >= 0.75 {
		full++
	}
	empty := 5 - full
	if half != "" {
		empty--
	}
	if empty < 0 {
		empty = 0
	}
	return strings.Repeat("★", full) + half + strings.Repeat("☆", empty)
}

// ---------------------------------------------------------------------------
// list subcommand
// ---------------------------------------------------------------------------

type listOptions struct {
	capabilities []string
	minTrust     int
	tier         string
	costProfile  string
	status       string
	format       string
	sortBy       string
	dryRun       bool
	verbose      bool
	marketplace  string
}

func newListCmd() *cobra.Command {
	opts := &listOptions{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List agents in the marketplace",
		Long: `List agents with optional filters. By default only active agents are shown.
Use --status to include deprecated or retired agents.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(opts)
		},
	}
	cmd.Flags().StringArrayVar(&opts.capabilities, "capability", nil,
		"filter by capability tag (repeatable)")
	cmd.Flags().IntVar(&opts.minTrust, "min-trust", 0, "minimum trust score")
	cmd.Flags().StringVar(&opts.tier, "tier", "", "filter by tier (pro|flash)")
	cmd.Flags().StringVar(&opts.costProfile, "cost-profile", "", "filter by cost profile (low|medium|high)")
	cmd.Flags().StringVar(&opts.status, "status", "active", "filter by status (active|deprecated|retired|all)")
	cmd.Flags().StringVar(&opts.format, "format", "table", "output format: json|table")
	cmd.Flags().StringVar(&opts.sortBy, "sort-by", "trust", "sort by: trust|cost|tasks|rating")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "resolve filters without printing results")
	cmd.Flags().BoolVar(&opts.verbose, "verbose", false, "log operations to stderr")
	cmd.Flags().StringVar(&opts.marketplace, "marketplace", defaultMarketplacePath(),
		"path to marketplace directory")
	return cmd
}

func runList(opts *listOptions) error {
	r := loadRegistry(opts.marketplace)
	caps := parseCapabilities(opts.capabilities)

	agents, err := r.List(func(a *marketplace.Agent) bool {
		// Status filter (spec §10.1: retired agents excluded by default).
		if opts.status != "all" && opts.status != "" {
			if string(a.Status) != opts.status {
				return false
			}
		}
		if opts.tier != "" && string(a.Tier) != opts.tier {
			return false
		}
		if opts.costProfile != "" && string(a.Budget.CostProfile) != opts.costProfile {
			return false
		}
		if opts.minTrust > 0 && a.TrustScore < opts.minTrust {
			return false
		}
		for _, c := range caps {
			if !agentHasCapability(a, c) {
				return false
			}
		}
		return true
	})
	if err != nil {
		exitOnError(err)
	}

	sortAgents(agents, opts.sortBy)

	if opts.verbose {
		fmt.Fprintf(os.Stderr, "operation=LIST status=%s results=%d\n", opts.status, len(agents))
	}

	if opts.dryRun {
		fmt.Printf("DRY_RUN: would list %d agents\n", len(agents))
		os.Exit(marketplace.ExitDryRun)
	}

	renderList(os.Stdout, agents, opts.format)
	return nil
}

// ---------------------------------------------------------------------------
// show subcommand
// ---------------------------------------------------------------------------

type showOptions struct {
	full        bool
	format      string
	dryRun      bool
	verbose     bool
	marketplace string
}

func newShowCmd() *cobra.Command {
	opts := &showOptions{}
	cmd := &cobra.Command{
		Use:   "show <agent-name>",
		Short: "Show detailed agent profile",
		Long: `Display the full profile for a named agent, including trust score,
capabilities, budget, performance metrics, and (with --full) all reviews.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShow(opts, args[0])
		},
	}
	cmd.Flags().BoolVar(&opts.full, "full", false, "show all fields including reviews")
	cmd.Flags().StringVar(&opts.format, "format", "table", "output format: json|yaml|table")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "resolve without printing")
	cmd.Flags().BoolVar(&opts.verbose, "verbose", false, "log operations to stderr")
	cmd.Flags().StringVar(&opts.marketplace, "marketplace", defaultMarketplacePath(),
		"path to marketplace directory")
	return cmd
}

func runShow(opts *showOptions, name string) error {
	r := loadRegistry(opts.marketplace)
	a, err := r.Get(name)
	exitOnError(err)

	if opts.verbose {
		fmt.Fprintf(os.Stderr, "operation=SHOW agent=%s trust=%d\n", name, a.TrustScore)
	}
	if opts.dryRun {
		fmt.Printf("DRY_RUN: would show agent %s\n", name)
		os.Exit(marketplace.ExitDryRun)
	}

	renderShow(os.Stdout, a, opts.full, opts.format)
	return nil
}

// ---------------------------------------------------------------------------
// search subcommand
// ---------------------------------------------------------------------------

type searchOptions struct {
	capabilities []string
	minTrust     int
	maxCost      float64
	limit        int
	dryRun       bool
	verbose      bool
	marketplace  string
}

func newSearchCmd() *cobra.Command {
	opts := &searchOptions{}
	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search agents by capability and trust",
		Long: `Search the marketplace for agents matching the given capabilities and trust
threshold. Capability filtering is AND semantics: agents must have ALL specified
capabilities. Results are ranked by trust score (descending).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(opts)
		},
	}
	cmd.Flags().StringArrayVar(&opts.capabilities, "capability", nil,
		"required capability tag (repeatable)")
	cmd.Flags().IntVar(&opts.minTrust, "min-trust", 0, "minimum trust score")
	cmd.Flags().Float64Var(&opts.maxCost, "max-cost", 0, "maximum average task cost")
	cmd.Flags().IntVar(&opts.limit, "limit", 10, "max results")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "resolve without printing")
	cmd.Flags().BoolVar(&opts.verbose, "verbose", false, "log operations to stderr")
	cmd.Flags().StringVar(&opts.marketplace, "marketplace", defaultMarketplacePath(),
		"path to marketplace directory")
	return cmd
}

func runSearch(opts *searchOptions) error {
	r := loadRegistry(opts.marketplace)
	q := marketplace.SearchQuery{
		Capabilities: parseCapabilities(opts.capabilities),
		MinTrust:     opts.minTrust,
		MaxCost:      opts.maxCost,
		Limit:        opts.limit,
	}
	agents, err := r.Search(q)
	if err != nil {
		exitOnError(err)
	}

	if opts.verbose {
		caps := make([]string, len(opts.capabilities))
		copy(caps, opts.capabilities)
		fmt.Fprintf(os.Stderr, "operation=SEARCH capabilities=[%s] min_trust=%d results=%d\n",
			strings.Join(caps, ","), opts.minTrust, len(agents))
	}

	if opts.dryRun {
		fmt.Printf("DRY_RUN: would search (%d matches)\n", len(agents))
		os.Exit(marketplace.ExitDryRun)
	}

	renderSearch(os.Stdout, agents)
	return nil
}

// ---------------------------------------------------------------------------
// rate subcommand
// ---------------------------------------------------------------------------

type rateOptions struct {
	comment     string
	author      string
	dryRun      bool
	verbose     bool
	marketplace string
}

func newRateCmd() *cobra.Command {
	opts := &rateOptions{}
	cmd := &cobra.Command{
		Use:   "rate <agent-name> <1-5>",
		Short: "Submit a human rating for an agent",
		Long: `Submit a 1-5 star rating for a named agent. Only verified humans can rate
agents. One rating per human per agent — re-rating replaces the previous review.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRate(opts, args[0], args[1])
		},
	}
	cmd.Flags().StringVar(&opts.comment, "comment", "", "review comment")
	cmd.Flags().StringVar(&opts.author, "author", "", "reviewer name (default: current user)")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "validate without submitting")
	cmd.Flags().BoolVar(&opts.verbose, "verbose", false, "log operations to stderr")
	cmd.Flags().StringVar(&opts.marketplace, "marketplace", defaultMarketplacePath(),
		"path to marketplace directory")
	return cmd
}

func runRate(opts *rateOptions, name, ratingStr string) error {
	rating, err := strconv.Atoi(ratingStr)
	if err != nil || rating < 1 || rating > 5 {
		fmt.Fprintf(os.Stderr, "INVALID_RATING: must be 1-5, got %s\n", ratingStr)
		os.Exit(marketplace.ExitInvalidRating)
	}

	author := opts.author
	if author == "" {
		author = currentUser()
	}

	r := loadRegistry(opts.marketplace)

	// Validate before dry-run so we get the right exit code.
	a, err := r.Get(name)
	exitOnError(err)
	if !marketplace.VerifyHuman(author) {
		fmt.Fprintln(os.Stderr, "UNAUTHORIZED: only humans can rate agents")
		os.Exit(marketplace.ExitUnauthorized)
	}

	if opts.verbose {
		fmt.Fprintf(os.Stderr, "operation=RATE agent=%s author=%s rating=%d\n", name, author, rating)
	}

	if opts.dryRun {
		fmt.Printf("DRY_RUN: would rate %s %d/5 (%s)\n", name, rating, author)
		os.Exit(marketplace.ExitDryRun)
	}

	oldAvg := a.Ratings.Average
	err = r.Rate(name, author, rating, opts.comment)
	exitOnError(err)

	renderRate(os.Stdout, name, author, rating, opts.comment, oldAvg, a.Ratings.Average, a.Ratings.Count)
	return nil
}

// ---------------------------------------------------------------------------
// review subcommand (alias for rate)
// ---------------------------------------------------------------------------

func newReviewCmd() *cobra.Command {
	opts := &rateOptions{}
	cmd := &cobra.Command{
		Use:   "review <agent-name> <1-5>",
		Short: "Submit a human review for an agent",
		Long: `Submit a 1-5 star review for a named agent. Only verified humans can review
agents. One review per human per agent — re-rating replaces the previous review.

This is an alias for the "rate" subcommand.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRate(opts, args[0], args[1])
		},
	}
	cmd.Flags().StringVar(&opts.comment, "comment", "", "review comment")
	cmd.Flags().StringVar(&opts.author, "author", "", "reviewer name (default: current user)")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "validate without submitting")
	cmd.Flags().BoolVar(&opts.verbose, "verbose", false, "log operations to stderr")
	cmd.Flags().StringVar(&opts.marketplace, "marketplace", defaultMarketplacePath(),
		"path to marketplace directory")
	return cmd
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

func renderList(w *os.File, agents []*marketplace.Agent, format string) {
	switch format {
	case "json":
		b, _ := json.MarshalIndent(agents, "", "  ")
		fmt.Fprintln(w, string(b))
	default:
		fmt.Fprintf(w, "%-14s %-7s %-7s %-9s %-7s %-10s %s\n",
			"NAME", "TIER", "TRUST", "RATING", "TASKS", "COST/AVG", "CAPABILITIES")
		for _, a := range agents {
			fmt.Fprintf(w, "%-14s %-7s %-7d ★%-8.1f %-7d $%-9.2f %s\n",
				a.Name, a.Tier, a.TrustScore, a.Ratings.Average,
				a.Performance.TasksCompleted, a.Budget.AverageTaskCost,
				capabilitiesString(a.Capabilities))
		}
		fmt.Fprintf(w, "\n%d agents listed.\n", len(agents))
	}
}

func renderShow(w *os.File, a *marketplace.Agent, full bool, format string) {
	switch format {
	case "json":
		b, _ := json.MarshalIndent(a, "", "  ")
		fmt.Fprintln(w, string(b))
	case "yaml":
		b, _ := yaml.Marshal(a)
		fmt.Fprintln(w, string(b))
	default:
		fmt.Fprintf(w, "AGENT: %s (%s tier)\n", a.Name, a.Tier)
		label := marketplace.TrustLabel(a.TrustScore)
		if a.TrustScore < 30 {
			fmt.Fprintf(w, "TRUST: %d (%s) ⚠️\n", a.TrustScore, label)
		} else {
			fmt.Fprintf(w, "TRUST: %d (%s)\n", a.TrustScore, label)
		}
		fmt.Fprintf(w, "RATING: %s (%.1f/5.0, %d reviews)\n",
			ratingStars(a.Ratings.Average), a.Ratings.Average, a.Ratings.Count)
		fmt.Fprintf(w, "STATUS: %s\n", a.Status)
		fmt.Fprintf(w, "CAPABILITIES: %s\n", capabilitiesString(a.Capabilities))
		fmt.Fprintf(w, "COST: $%.2f/task avg (%s profile)\n",
			a.Budget.AverageTaskCost, a.Budget.CostProfile)
		fmt.Fprintf(w, "TASKS: %d completed | Acceptance: %.0f%% | Adherence: %.0f%%\n",
			a.Performance.TasksCompleted,
			a.Performance.PrAcceptanceRate*100,
			a.Performance.BudgetAdherence*100)
		if full && len(a.Reviews) > 0 {
			fmt.Fprintln(w, "\nRECENT REVIEWS:")
			for _, rv := range a.Reviews {
				fmt.Fprintf(w, "  %s %s (%s): %q\n",
					ratingStars(float64(rv.Rating)), rv.Author, rv.Date, rv.Comment)
			}
		}
	}
}

func renderSearch(w *os.File, agents []*marketplace.Agent) {
	if len(agents) == 0 {
		fmt.Fprintln(w, "No agents found.")
		return
	}
	for _, a := range agents {
		fmt.Fprintf(w, "\nAGENT: %s (%s, trust=%d)\n", a.Name, a.Tier, a.TrustScore)
		fmt.Fprintf(w, "  Capabilities: %s\n", capabilitiesString(a.Capabilities))
		fmt.Fprintf(w, "  Cost profile: %s ($%.2f/task avg)\n",
			a.Budget.CostProfile, a.Budget.AverageTaskCost)
		fmt.Fprintf(w, "  Tasks completed: %d | Acceptance rate: %.0f%%\n",
			a.Performance.TasksCompleted, a.Performance.PrAcceptanceRate*100)
		fmt.Fprintf(w, "  Rating: %s (%.1f/5.0, %d reviews)\n",
			ratingStars(a.Ratings.Average), a.Ratings.Average, a.Ratings.Count)
	}
	fmt.Fprintf(w, "\n%d agent(s) found.\n", len(agents))
}

func renderRate(w *os.File, name, author string, rating int, comment string, oldAvg, newAvg float64, count int) {
	fmt.Fprintln(w, "RATING SUBMITTED:")
	fmt.Fprintf(w, "  Agent:  %s\n", name)
	fmt.Fprintf(w, "  Rating: %s (%d/5)\n", ratingStars(float64(rating)), rating)
	humanTag := "human ✅"
	if !marketplace.VerifyHuman(author) {
		humanTag = "non-human ❌"
	}
	fmt.Fprintf(w, "  Author: %s (%s)\n", author, humanTag)
	if comment != "" {
		fmt.Fprintf(w, "  Comment: %q\n", comment)
	}
	fmt.Fprintf(w, "\nAgent %s new average: %.1f → %.1f (%d reviews)\n", name, oldAvg, newAvg, count)
}

// ---------------------------------------------------------------------------
// Utility functions
// ---------------------------------------------------------------------------

// parseCapabilities converts string capability flags to typed Capability values.
// Invalid capabilities are silently dropped (validation happens at Register time).
func parseCapabilities(strs []string) []marketplace.Capability {
	var caps []marketplace.Capability
	for _, s := range strs {
		if c, ok := marketplace.ValidCapability(s); ok {
			caps = append(caps, c)
		}
	}
	return caps
}

// agentHasCapability checks if an agent has a capability (local helper to
// avoid importing the unexported hasCapability from the package).
func agentHasCapability(a *marketplace.Agent, c marketplace.Capability) bool {
	for _, cap := range a.Capabilities {
		if cap == c {
			return true
		}
	}
	return false
}

// sortAgents sorts by the specified key.
func sortAgents(agents []*marketplace.Agent, sortBy string) {
	switch sortBy {
	case "cost":
		sort.Slice(agents, func(i, j int) bool {
			return agents[i].Budget.AverageTaskCost < agents[j].Budget.AverageTaskCost
		})
	case "tasks":
		sort.Slice(agents, func(i, j int) bool {
			return agents[i].Performance.TasksCompleted > agents[j].Performance.TasksCompleted
		})
	case "rating":
		sort.Slice(agents, func(i, j int) bool {
			return agents[i].Ratings.Average > agents[j].Ratings.Average
		})
	default: // "trust"
		sort.Slice(agents, func(i, j int) bool {
			return agents[i].TrustScore > agents[j].TrustScore
		})
	}
}

// currentUser returns the OS username for the default --author flag.
func currentUser() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u := os.Getenv("USERNAME"); u != "" {
		return u
	}
	return "unknown"
}

// capabilitiesString joins capabilities into a display string.
func capabilitiesString(caps []marketplace.Capability) string {
	parts := make([]string, len(caps))
	for i, c := range caps {
		parts[i] = string(c)
	}
	return strings.Join(parts, ", ")
}
