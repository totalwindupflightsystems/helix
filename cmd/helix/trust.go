package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

// ============================================================================
// helix trust CLI — trust snapshot queries (spec trust-model.md)
// ============================================================================

const (
	trustExitOK       = 0
	trustExitFail     = 1 // no data found
	trustExitError    = 2 // invocation error
	trustExitNotFound = 3 // ledger file not found
)

// trustFlags holds parsed CLI flags.
type trustFlags struct {
	subcommand string // show, history, list
	ledger     string
	agent      string
	jsonOut    bool
	days       int // for recent events window
}

// parseTrustFlags parses the args for `helix trust`.
func parseTrustFlags(args []string) (trustFlags, bool, int) {
	var f trustFlags
	f.days = 30
	helpWanted := false

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			helpWanted = true
		case arg == "--json":
			f.jsonOut = true
		case arg == "--ledger":
			if i+1 < len(args) {
				f.ledger = args[i+1]
				i++
			} else {
				return f, false, trustExitError
			}
		case arg == "--agent":
			if i+1 < len(args) {
				f.agent = args[i+1]
				i++
			} else {
				return f, false, trustExitError
			}
		case arg == "--days":
			if i+1 < len(args) {
				if _, err := fmt.Sscanf(args[i+1], "%d", &f.days); err != nil {
					return f, false, trustExitError
				}
				i++
			} else {
				return f, false, trustExitError
			}
		case strings.HasPrefix(arg, "--"):
			return f, false, trustExitError
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

	return f, helpWanted, trustExitOK
}

// printTrustHelp prints the help text.
func printTrustHelp(w io.Writer) {
	fmt.Fprintln(w, `helix trust — trust snapshot queries (spec trust-model.md)

Usage:
  helix trust show --ledger <path> --agent <name> [--json]
  helix trust history --ledger <path> --agent <name> [--json]
  helix trust list --ledger <path> [--json]
  helix trust help

Subcommands:
  show     Show current trust score, tier, and breakdown for an agent
  history  Show tier transition history for an agent
  list     List all agents in the ledger with current scores
  help     Show this help

Flags:
  --ledger <path>   Path to JSONL trust ledger file
  --agent <name>    Agent ID to query (required for show/history)
  --days <N>        Recent events window (default: 30)
  --json            Structured JSON output
  --help, -h        Show this help

Exit codes:
  0  Success
  1  No data found for agent
  2  Invocation error
  3  Ledger file not found`)
}

// runTrust is the entry point for `helix trust`.
func runTrust(args []string, stdout, stderr io.Writer) int {
	flags, helpWanted, rc := parseTrustFlags(args)
	if rc != trustExitOK {
		return rc
	}
	if helpWanted {
		printTrustHelp(stdout)
		return trustExitOK
	}

	switch flags.subcommand {
	case "help":
		printTrustHelp(stdout)
		return trustExitOK
	case "show":
		return runTrustShow(flags, stdout, stderr)
	case "history":
		return runTrustHistory(flags, stdout, stderr)
	case "list":
		return runTrustList(flags, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n", flags.subcommand)
		return trustExitError
	}
}

// checkLedger verifies the ledger file exists.
func checkLedger(flags trustFlags, stderr io.Writer) int {
	if flags.ledger == "" {
		fmt.Fprintf(stderr, "error: --ledger <path> is required\n")
		return trustExitError
	}
	if _, err := os.Stat(flags.ledger); os.IsNotExist(err) {
		fmt.Fprintf(stderr, "error: ledger file not found: %s\n", flags.ledger)
		return trustExitNotFound
	}
	return trustExitOK
}

// runTrustShow shows the current trust snapshot for an agent.
func runTrustShow(flags trustFlags, stdout, stderr io.Writer) int {
	if rc := checkLedger(flags, stderr); rc != trustExitOK {
		return rc
	}
	if flags.agent == "" {
		fmt.Fprintf(stderr, "error: --agent <name> is required\n")
		return trustExitError
	}

	snapshot, err := trust.GetSnapshot(flags.ledger, flags.agent)
	if err != nil {
		fmt.Fprintf(stderr, "error: reading ledger: %v\n", err)
		return trustExitError
	}
	if snapshot == nil || snapshot.TotalEvents == 0 {
		fmt.Fprintf(stderr, "no data found for agent %q\n", flags.agent)
		return trustExitFail
	}

	if flags.jsonOut {
		data, err := json.MarshalIndent(snapshot, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return trustExitError
		}
		fmt.Fprintln(stdout, string(data))
		return trustExitOK
	}

	printTrustSnapshotText(stdout, snapshot)
	return trustExitOK
}

// printTrustSnapshotText renders the snapshot in human-readable format.
func printTrustSnapshotText(w io.Writer, s *trust.TrustSnapshot) {
	fmt.Fprintf(w, "Trust Snapshot — Agent: %s\n", s.AgentID)
	fmt.Fprintf(w, "============================================\n")
	fmt.Fprintf(w, "Score:    %.2f / 1.00\n", float64(s.Score))
	fmt.Fprintf(w, "Tier:     %s\n", tierName(s.Tier))
	fmt.Fprintf(w, "Events:   %d total\n", s.TotalEvents)
	if !s.LastActive.IsZero() {
		fmt.Fprintf(w, "Last Active: %s\n", s.LastActive.Format("2006-01-02 15:04:05"))
	}
	fmt.Fprintf(w, "\nScore Breakdown:\n")
	printDimension(w, "Merge Success", s.ScoreBreakdown.MergeSuccessRate)
	printDimension(w, "Incident Track", s.ScoreBreakdown.IncidentTrack)
	printDimension(w, "Review Consensus", s.ScoreBreakdown.ReviewConsensus)
	printDimension(w, "Prompt Integrity", s.ScoreBreakdown.PromptIntegrity)
	printDimension(w, "Human Feedback", s.ScoreBreakdown.HumanFeedback)
	printDimension(w, "Tenure", s.ScoreBreakdown.Tenure)

	if s.ScoreTrend.DataPoints > 0 {
		fmt.Fprintf(w, "\nScore Trend (30d):\n")
		fmt.Fprintf(w, "  Direction: %s\n", s.ScoreTrend.Direction)
		fmt.Fprintf(w, "  Delta:     %+.2f\n", s.ScoreTrend.Delta)
	}

	if len(s.TierHistory) > 0 {
		fmt.Fprintf(w, "\nTier History (%d transitions):\n", len(s.TierHistory))
		for _, t := range s.TierHistory {
			arrow := "→"
			label := "demotion"
			if t.IsPromote {
				label = "promotion"
			}
			fmt.Fprintf(w, "  %s %s %s (%s) %s\n",
				tierName(t.From), arrow, tierName(t.To), label,
				t.Timestamp.Format("2006-01-02"))
		}
	}
}

// printDimension prints a single score dimension.
func printDimension(w io.Writer, name string, dc trust.DimensionContribution) {
	fmt.Fprintf(w, "  %-20s weight=%.2f  score=%.2f  contribution=%.2f\n",
		name, dc.Weight, dc.EstScore, dc.Contribution)
}

// tierName returns a human-readable tier name.
func tierName(tier trust.TrustTier) string {
	switch tier {
	case trust.TierProvisional:
		return "Provisional"
	case trust.TierObserved:
		return "Observed"
	case trust.TierTrusted:
		return "Trusted"
	case trust.TierVeteran:
		return "Veteran"
	default:
		return string(tier)
	}
}

// runTrustHistory shows tier transitions for an agent.
func runTrustHistory(flags trustFlags, stdout, stderr io.Writer) int {
	if rc := checkLedger(flags, stderr); rc != trustExitOK {
		return rc
	}
	if flags.agent == "" {
		fmt.Fprintf(stderr, "error: --agent <name> is required\n")
		return trustExitError
	}

	history, err := trust.GetTierHistory(flags.ledger, flags.agent)
	if err != nil {
		fmt.Fprintf(stderr, "error: reading ledger: %v\n", err)
		return trustExitError
	}

	if flags.jsonOut {
		if history == nil {
			history = []trust.TierTransition{}
		}
		data, err := json.MarshalIndent(history, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return trustExitError
		}
		fmt.Fprintln(stdout, string(data))
		return trustExitOK
	}

	if len(history) == 0 {
		fmt.Fprintf(stdout, "No tier transitions recorded for agent %q\n", flags.agent)
		return trustExitOK
	}

	fmt.Fprintf(stdout, "Tier History — Agent: %s (%d transitions)\n\n", flags.agent, len(history))
	for _, t := range history {
		label := "DEMOTION"
		if t.IsPromote {
			label = "PROMOTION"
		}
		fmt.Fprintf(stdout, "  %s: %s → %s  %s",
			t.Timestamp.Format("2006-01-02"),
			tierName(t.From), tierName(t.To), label)
		if t.Reason != "" {
			fmt.Fprintf(stdout, "  (%s)", t.Reason)
		}
		fmt.Fprintln(stdout)
	}

	return trustExitOK
}

// runTrustList lists all agents in the ledger.
func runTrustList(flags trustFlags, stdout, stderr io.Writer) int {
	if rc := checkLedger(flags, stderr); rc != trustExitOK {
		return rc
	}

	events, err := trust.Replay(flags.ledger)
	if err != nil {
		fmt.Fprintf(stderr, "error: reading ledger: %v\n", err)
		return trustExitError
	}

	// Collect unique agent IDs
	agentSet := make(map[string]bool)
	for _, evt := range events {
		agentSet[evt.AgentID] = true
	}

	type agentScore struct {
		AgentID string  `json:"agent_id"`
		Score   float64 `json:"score"`
		Tier    string  `json:"tier"`
		Events  int     `json:"events"`
	}

	var agents []agentScore
	for agentID := range agentSet {
		snapshot, err := trust.GetSnapshot(flags.ledger, agentID)
		if err != nil || snapshot == nil {
			continue
		}
		agents = append(agents, agentScore{
			AgentID: agentID,
			Score:   float64(snapshot.Score),
			Tier:    tierName(snapshot.Tier),
			Events:  snapshot.TotalEvents,
		})
	}

	if flags.jsonOut {
		data, err := json.MarshalIndent(agents, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return trustExitError
		}
		fmt.Fprintln(stdout, string(data))
		return trustExitOK
	}

	if len(agents) == 0 {
		fmt.Fprintln(stdout, "No agents found in ledger")
		return trustExitOK
	}

	fmt.Fprintf(stdout, "Agents in Trust Ledger (%d)\n\n", len(agents))
	fmt.Fprintf(stdout, "%-30s %6s  %-25s  %6s\n", "AGENT", "SCORE", "TIER", "EVENTS")
	for _, a := range agents {
		fmt.Fprintf(stdout, "%-30s %6.2f  %-25s  %6d\n", a.AgentID, a.Score, a.Tier, a.Events)
	}

	return trustExitOK
}

// runTrustWithDryRun wraps runTrust with the global --dry-run flag.
func runTrustWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
	rc := runTrust(args, stdout, stderr)
	if rc != 0 && rc != trustExitFail {
		return errExit{code: rc}
	}
	return nil
}
