// Command helix-negotiate is the Helix agent-to-agent PR negotiation CLI.
//
// It implements the negotiation protocol from specs/pr-negotiation.md: when two
// agents post conflicting PR reviews (one APPROVED, one REQUEST_CHANGES), the
// protocol structures their debate across 3 evidence-bound rounds. If they
// deadlock, Chimera's arbiter formation breaks the tie.
//
// Two subcommands:
//
//	helix-negotiate debate <pr-url>   start or debug a negotiation
//	helix-negotiate resolve <pr-url>  force Chimera tie-break resolution
//
// Import constraint: stdlib + github.com/spf13/cobra + gopkg.in/yaml.v3 only.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/totalwindupflightsystems/helix/pkg/negotiate"
)

// exitProcess is the process-exit hook used by the CLI's os.Exit calls.
// Tests override it to a no-op so dry-run paths can be exercised without
// killing the test binary.
var exitProcess = os.Exit

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// Root command
// ---------------------------------------------------------------------------

// globalOptions holds persistent flags shared by all subcommands.
type globalOptions struct {
	configPath string
	verbose    bool
}

func newRootCmd() *cobra.Command {
	gOpts := &globalOptions{}
	root := &cobra.Command{
		Use:   "helix-negotiate",
		Short: "Helix agent-to-agent PR negotiation",
		Long: `helix-negotiate — Agent-to-Agent PR Negotiation

When two agents post conflicting PR reviews (one APPROVED, one REQUEST_CHANGES),
the negotiation protocol structures their debate across 3 evidence-bound rounds.
If they deadlock, Chimera's arbiter formation breaks the tie.

Subcommands:
  debate <pr-url>   Start or debug a negotiation between two agents
  resolve <pr-url>  Force Chimera tie-break resolution

Run "helix-negotiate <subcommand> --help" for per-command flags.`,
	}
	root.PersistentFlags().StringVar(&gOpts.configPath, "config", defaultConfigPath(),
		"path to known-friends.json")
	root.PersistentFlags().BoolVarP(&gOpts.verbose, "verbose", "v", false,
		"verbose output (log every state transition)")
	root.AddCommand(newDebateCmd(gOpts), newResolveCmd(gOpts))
	return root
}

// ---------------------------------------------------------------------------
// debate subcommand
// ---------------------------------------------------------------------------

type debateOptions struct {
	*globalOptions
	agentA     string
	agentB     string
	maxRounds  int
	timeout    time.Duration
	dryRun     bool
	chimeraURL string
	verdictA   string
	verdictB   string
}

func newDebateCmd(gOpts *globalOptions) *cobra.Command {
	opts := &debateOptions{globalOptions: gOpts}
	cmd := &cobra.Command{
		Use:   "debate <pr-url>",
		Short: "Start or debug a negotiation between two agents",
		Long: `Start a structured negotiation between two agents who posted conflicting
PR reviews. The protocol runs 3 evidence-bound debate rounds; if agents deadlock,
Chimera's arbiter formation breaks the tie.

In v1, Forgejo review fetching is not implemented. Use --verdict-a and --verdict-b
to simulate agent verdicts for testing and dry-run previews.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDebate(opts, args[0])
		},
	}
	cmd.Flags().StringVar(&opts.agentA, "agent-a", "", "first agent name (required)")
	cmd.Flags().StringVar(&opts.agentB, "agent-b", "", "second agent name (required)")
	cmd.Flags().IntVar(&opts.maxRounds, "max-rounds", 3, "max debate rounds")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", 30*time.Minute, "max negotiation time")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "preview debate without posting")
	cmd.Flags().StringVar(&opts.chimeraURL, "chimera-url", "http://localhost:8765",
		"Chimera base URL")
	cmd.Flags().StringVar(&opts.verdictA, "verdict-a", "APPROVED",
		"simulated verdict for agent A (v1: Forgejo fetch not implemented)")
	cmd.Flags().StringVar(&opts.verdictB, "verdict-b", "REQUEST_CHANGES",
		"simulated verdict for agent B (v1: Forgejo fetch not implemented)")
	return cmd
}

func runDebate(opts *debateOptions, prURL string) error {
	if opts.agentA == "" || opts.agentB == "" {
		return fmt.Errorf("--agent-a and --agent-b are required")
	}

	prNumber, err := parsePRNumber(prURL)
	if err != nil {
		return err
	}

	agents, _ := loadAgents(opts.configPath) // ignore error — use defaults
	agentA := lookupAgent(agents, opts.agentA)
	agentB := lookupAgent(agents, opts.agentB)
	verdictA := negotiate.Verdict(opts.verdictA)
	verdictB := negotiate.Verdict(opts.verdictB)

	// Conflict check
	fmt.Fprintln(os.Stdout, "CONFLICT CHECK:")
	fmt.Fprintf(os.Stdout, "  Agent %s: %s %s\n", agentA.Name, verdictA, verdictEmoji(verdictA))
	fmt.Fprintf(os.Stdout, "  Agent %s: %s %s\n", agentB.Name, verdictB, verdictEmoji(verdictB))

	if !negotiate.DetectConflict(verdictA, verdictB) {
		fmt.Fprintln(os.Stdout, "\nRESULT: No conflict. Both agents agree. Proceeding to merge.")
		return nil
	}

	fmt.Fprintln(os.Stdout, "\nCONFLICT DETECTED:")
	fmt.Fprintf(os.Stdout, "  Agent %s (trust=%d): %s\n", agentA.Name, agentA.TrustLevel, verdictA)
	fmt.Fprintf(os.Stdout, "  Agent %s (trust=%d): %s\n", agentB.Name, agentB.TrustLevel, verdictB)

	if opts.dryRun {
		fmt.Fprintf(os.Stdout, "\nDRY RUN: would negotiate up to %d rounds (timeout %s)\n",
			opts.maxRounds, opts.timeout)
		fmt.Fprintf(os.Stdout, "Chimera URL: %s\n", opts.chimeraURL)
		fmt.Fprintf(os.Stdout, "Audit log would be written to: %s\n", auditLogPath(prNumber))
		exitProcess(10)
		return nil
	}

	// Set up audit log
	auditPath := auditLogPath(prNumber)
	if err := os.MkdirAll(filepath.Dir(auditPath), 0755); err != nil {
		return fmt.Errorf("create audit directory: %w", err)
	}

	neg, err := negotiate.NewNegotiator(prNumber, agentA, agentB, verdictA, verdictB,
		opts.chimeraURL, auditPath)
	if err != nil {
		return err
	}
	defer neg.Audit.Close()

	// Run the state machine until terminal state
	for {
		prevState := neg.Neg.State
		if prevState == negotiate.StateResolved || prevState == negotiate.StateEscalated {
			break
		}
		newState, advErr := neg.Advance()
		if opts.verbose {
			fmt.Fprintf(os.Stdout, "[transition] %s -> %s (round=%d)\n",
				prevState, newState, neg.Neg.Round)
		}
		if advErr != nil {
			fmt.Fprintf(os.Stderr, "negotiation step error: %v\n", advErr)
			break
		}
		if newState == prevState {
			break // no state change — avoid infinite loop
		}
	}

	renderNegotiationResult(os.Stdout, neg, auditPath)
	return nil
}

// ---------------------------------------------------------------------------
// resolve subcommand
// ---------------------------------------------------------------------------

type resolveOptions struct {
	*globalOptions
	forceChimera  bool
	verdict       string
	chimeraURL    string
	pr            int    //nolint:unused
	positionsFile string //nolint:unused
}

func newResolveCmd(gOpts *globalOptions) *cobra.Command {
	opts := &resolveOptions{globalOptions: gOpts}
	cmd := &cobra.Command{
		Use:   "resolve [pr-url]",
		Short: "Force Chimera tie-break resolution",
		Long: `Force-resolve a negotiation by invoking Chimera's arbiter formation directly,
bypassing the debate rounds.

Use --pr to specify a PR number directly, or pass a Forgejo PR URL as a
positional argument.

Use --positions-file to provide agent positions as JSON (requires --pr).
  Format: [{"agent":"alfa","verdict":"APPROVED","evidence":"..."}, ...]

Use --verdict to pre-set a verdict for testing (no Chimera call is made).
Use --force-chimera to explicitly skip debate and go straight to the tie-break.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.pr == 0 && len(args) == 0 {
				return fmt.Errorf("either --pr or a positional <pr-url> is required")
			}
			prURL := ""
			if len(args) > 0 {
				prURL = args[0]
			}
			return runResolve(opts, prURL)
		},
	}
	cmd.Flags().BoolVar(&opts.forceChimera, "force-chimera", false,
		"skip debate, go straight to Chimera tie-break")
	cmd.Flags().StringVar(&opts.verdict, "verdict", "",
		"pre-set verdict (APPROVE|REJECT) for testing — no Chimera call")
	cmd.Flags().StringVar(&opts.chimeraURL, "chimera-url", "http://localhost:8765",
		"Chimera base URL")
	cmd.Flags().IntVar(&opts.pr, "pr", 0,
		"PR number (alternative to positional pr-url)")
	cmd.Flags().StringVar(&opts.positionsFile, "positions-file", "",
		"JSON file with agent positions (requires --pr)")
	return cmd
}

func runResolve(opts *resolveOptions, prURL string) error {
	var prNumber int
	var err error

	if opts.pr > 0 {
		prNumber = opts.pr
	} else {
		prNumber, err = parsePRNumber(prURL)
		if err != nil {
			return err
		}
	}

	// Pre-set verdict for testing — no Chimera call
	if opts.verdict != "" {
		v := strings.ToUpper(opts.verdict)
		fmt.Fprintf(os.Stdout, "FORCE VERDICT: PR #%d -> %s\n", prNumber, v)
		fmt.Fprintf(os.Stdout, "  (pre-set via --verdict, no Chimera call)\n")
		return nil
	}

	// If --positions-file is provided, use the Negotiate API
	if opts.positionsFile != "" {
		return runResolveWithPositions(opts, prNumber)
	}

	// Call Chimera directly
	arbiter := negotiate.NewArbiterClient(opts.chimeraURL)
	prompt := fmt.Sprintf("Resolve PR #%d conflict. Force Chimera tie-break requested.", prNumber)

	fmt.Fprintf(os.Stdout, "Calling Chimera arbiter at %s...\n", opts.chimeraURL)
	verdict, err := arbiter.Deliberate(prompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "CHIMERA_UNAVAILABLE: %v\n", err)
		exitProcess(2)
		return err
	}

	fmt.Fprintln(os.Stdout, "\nCHIMERA TIE-BREAK RESULT:")
	fmt.Fprintf(os.Stdout, "  Verdict:     %s\n", verdict.Verdict)
	fmt.Fprintf(os.Stdout, "  Confidence:  %.2f\n", verdict.Confidence)
	fmt.Fprintf(os.Stdout, "  Cost:        $%.4f\n", verdict.Cost)
	aShare, bShare := negotiate.SplitCost(verdict.Cost)
	fmt.Fprintf(os.Stdout, "  Split:       $%.4f / $%.4f (evenly per spec §9.3)\n", aShare, bShare)
	fmt.Fprintf(os.Stdout, "  Reasoning:   %s\n", verdict.Trace)
	return nil
}

// runResolveWithPositions reads agent positions from a JSON file and runs
// the full negotiation protocol via the Negotiate API.
func runResolveWithPositions(opts *resolveOptions, prNumber int) error {
	data, err := os.ReadFile(opts.positionsFile)
	if err != nil {
		return fmt.Errorf("read positions file: %w", err)
	}

	var positions []negotiate.Position
	if err := json.Unmarshal(data, &positions); err != nil {
		return fmt.Errorf("parse positions JSON: %w", err)
	}

	if len(positions) < 2 {
		return fmt.Errorf("positions file must contain at least 2 agent positions, got %d", len(positions))
	}

	prCtx := negotiate.PRContext{
		PRNumber:       prNumber,
		AgentPositions: positions,
	}

	cfg := negotiate.NegotiationConfig{
		ChimeraURL: opts.chimeraURL,
		MaxRounds:  3,
		Timeout:    30 * time.Minute,
		AuditPath:  auditLogPath(prNumber),
		Verbose:    opts.verbose,
	}

	neg, err := negotiate.NewNegotiatorFromConfig(cfg, prCtx)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "Negotiating PR #%d with %d agent positions...\n", prNumber, len(positions))
	for _, p := range positions {
		fmt.Fprintf(os.Stdout, "  Agent %s: %s\n", p.Agent, p.Verdict)
	}

	ctx := context.Background()
	resolution, err := neg.Negotiate(ctx, prCtx)
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, "\nNEGOTIATION RESULT:")
	fmt.Fprintf(os.Stdout, "  Verdict:    %s\n", resolution.Verdict)
	fmt.Fprintf(os.Stdout, "  Reasoning:  %s\n", resolution.Reasoning)
	if resolution.TieBreaker != "" {
		fmt.Fprintf(os.Stdout, "  Tie-Breaker: %s\n", resolution.TieBreaker)
	}
	if len(resolution.WinningEvidence) > 0 {
		fmt.Fprintln(os.Stdout, "  Winning Evidence:")
		for _, e := range resolution.WinningEvidence {
			fmt.Fprintf(os.Stdout, "    - %s\n", e)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// defaultConfigPath returns the known-friends.json location, falling back to the
// testdata fixture when production paths are absent.
func defaultConfigPath() string {
	if home, err := os.UserHomeDir(); err == nil {
		p := home + "/.helix/known-friends.json"
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	const prod = "/opt/hermes-demo/.hermes/h4f/known-friends.json"
	if _, err := os.Stat(prod); err == nil {
		return prod
	}
	return "pkg/estimate/testdata/known-friends.json"
}

// friendAgent is the negotiation-relevant slice of an agent entry in
// known-friends.json (spec §3.1).
type friendAgent struct {
	Name        string `json:"name"`
	TrustLevel  int    `json:"trust_level"`
	ForgejoUser string `json:"forgejo_username"`
	Tier        string `json:"tier"`
}

type friendsFile struct {
	Version int                    `json:"version"`
	Agents  map[string]friendAgent `json:"agents"`
}

// loadAgents reads known-friends.json. Supports both the wrapped
// {"agents": {...}} shape and a bare {name: {...}} map.
func loadAgents(path string) (map[string]friendAgent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ff friendsFile
	if err := json.Unmarshal(data, &ff); err == nil && len(ff.Agents) > 0 {
		return ff.Agents, nil
	}
	bare := map[string]friendAgent{}
	if err := json.Unmarshal(data, &bare); err != nil {
		return nil, err
	}
	return bare, nil
}

// lookupAgent resolves an agent name to a negotiate.Agent, applying sensible
// defaults when the agent is not found in known-friends.json.
func lookupAgent(agents map[string]friendAgent, name string) negotiate.Agent {
	if a, ok := agents[name]; ok {
		return negotiate.Agent{
			Name:        name,
			TrustLevel:  negotiate.TrustLevel(a.TrustLevel),
			ForgejoUser: a.ForgejoUser,
			Tier:        a.Tier,
		}
	}
	return negotiate.Agent{
		Name: name,
		Tier: "pro",
	}
}

// verdictEmoji returns a display emoji for a verdict.
func verdictEmoji(v negotiate.Verdict) string {
	switch v {
	case negotiate.VerdictApproved:
		return "OK"
	case negotiate.VerdictRequestChanges:
		return "CHANGES"
	default:
		return "?"
	}
}

// parsePRNumber extracts the PR number from a Forgejo PR URL.
// e.g., https://forgejo.helix.local/helix/core/pulls/42 -> 42
func parsePRNumber(prURL string) (int, error) {
	parts := strings.Split(prURL, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if n, err := strconv.Atoi(parts[i]); err == nil {
			return n, nil
		}
	}
	return 0, fmt.Errorf("could not extract PR number from URL: %s", prURL)
}

// auditLogPath returns the JSONL audit log path for a PR (spec §13).
func auditLogPath(prNumber int) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	ts := time.Now().Format("20060102-150405")
	return fmt.Sprintf("%s/.helix/negotiations/%d-%s.jsonl", home, prNumber, ts)
}

// renderNegotiationResult prints the final negotiation outcome.
func renderNegotiationResult(w *os.File, neg *negotiate.Negotiator, auditPath string) {
	fmt.Fprintf(w, "\nRESULT: PR #%d — %s\n", neg.Neg.PRNumber, neg.Neg.State)
	if neg.ChimeraResult != nil {
		cv := neg.ChimeraResult
		fmt.Fprintf(w, "  Chimera verdict: %s (confidence %.2f, cost $%.4f)\n",
			cv.Verdict, cv.Confidence, cv.Cost)
		aShare, bShare := negotiate.SplitCost(cv.Cost)
		fmt.Fprintf(w, "  Cost split:      $%.4f / $%.4f\n", aShare, bShare)
	}
	fmt.Fprintf(w, "  Rounds:          %d\n", neg.Neg.Round)
	fmt.Fprintf(w, "  Audit log:       %s\n", auditPath)
}
