package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/forgejo"
	"github.com/totalwindupflightsystems/helix/pkg/review"
	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

// -----------------------------------------------------------------------------
// review run — multi-model adversarial review
// -----------------------------------------------------------------------------

// parsePRURL extracts owner, repo, and PR index from a Forgejo PR URL of
// the form http(s)://host/<owner>/<repo>/pulls/<index>. The host is
// ignored — only the path matters (DF-018).
func parsePRURL(prURL string) (owner, repo string, index int64, err error) {
	u, err := url.Parse(prURL)
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid PR URL %q: %w", prURL, err)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 4 || parts[2] != "pulls" {
		return "", "", 0, fmt.Errorf("PR URL %q must look like http://host/<owner>/<repo>/pulls/<index>", prURL)
	}
	owner, repo = parts[0], parts[1]
	if owner == "" || repo == "" {
		return "", "", 0, fmt.Errorf("PR URL %q has empty owner or repo", prURL)
	}
	index, err = strconv.ParseInt(parts[3], 10, 64)
	if err != nil || index <= 0 {
		return "", "", 0, fmt.Errorf("PR URL %q has invalid pull index %q", prURL, parts[3])
	}
	return owner, repo, index, nil
}

// reviewDegradationReasons reports which panel members did NOT actually
// deliberate: a leg that errored, or a leg that returned no verdict at
// all. An empty slice means every panel member produced a verdict and
// the review is trustworthy. DF-018: the CLI must fail loud (non-zero
// exit + warning) when a stub run would otherwise masquerade as a clean
// "Review complete".
func reviewDegradationReasons(result *review.ReviewResult) []string {
	var reasons []string
	for _, o := range result.Outcomes {
		switch {
		case o.Err != "":
			reasons = append(reasons, fmt.Sprintf("%s (%s) failed to deliberate: %s", o.Role, o.Model, o.Err))
		case o.Verdict == "":
			reasons = append(reasons, fmt.Sprintf("%s (%s) returned no verdict", o.Role, o.Model))
		}
	}
	return reasons
}

// runReviewRun dispatches a multi-model adversarial review against a PR.
// It fetches the REAL PR diff from Forgejo, creates model clients for
// Chimera and DeepSeek, builds a review panel, and runs the orchestrator.
// Outputs the consensus verdict as JSON. If any panel member failed to
// deliberate, the run prints a hard degradation warning and exits
// non-zero instead of reporting a clean "Review complete" (DF-018).
func runReviewRun(flags revFlags, stdout, stderr io.Writer) int {
	if flags.prURL == "" {
		fmt.Fprintln(stderr, "error: --pr URL is required for review run")
		return revExitError
	}

	// Parse the PR URL and fetch the real diff from Forgejo.
	owner, repo, prIndex, err := parsePRURL(flags.prURL)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return revExitError
	}

	forgejoBaseURL := os.Getenv("FORGEJO_URL")
	if forgejoBaseURL == "" {
		forgejoBaseURL = "http://localhost:3030"
	}
	fjUser := os.Getenv("FORGEJO_ADMIN_USER")
	fjPass := os.Getenv("FORGEJO_ADMIN_PASSWORD")
	if token := os.Getenv("FORGEJO_ADMIN_TOKEN"); token != "" {
		// Forgejo accepts a PAT as the BasicAuth password.
		fjUser, fjPass = "", token
	}
	fjClient := forgejo.NewClient(forgejoBaseURL, fjUser, fjPass)

	fetchCtx, cancelFetch := context.WithTimeout(context.Background(), 30*time.Second)
	diff, err := fjClient.GetPullRequestDiff(fetchCtx, owner, repo, prIndex)
	cancelFetch()
	if err != nil {
		fmt.Fprintf(stderr, "error: fetch diff for PR %s: %v\n", flags.prURL, err)
		return revExitError
	}
	if strings.TrimSpace(diff) == "" {
		fmt.Fprintf(stderr, "error: PR %s has an empty diff — nothing to review\n", flags.prURL)
		return revExitError
	}

	// Create model clients.
	// In production these would read API keys from environment.
	chimeraClient := review.NewChimeraClient(review.ModelClientConfig{
		BaseURL: "http://localhost:8765",
		Model:   "chimera-default",
	})
	deepseekClient := review.NewDeepSeekClient(review.ModelClientConfig{
		BaseURL: "https://api.deepseek.com/v1",
		APIKey:  os.Getenv("DEEPSEEK_API_KEY"),
		Model:   "deepseek-v4-flash",
	})

	orchestrator := review.NewReviewOrchestrator()

	// Build a 2-model panel (primary + adversarial) for behavioral review.
	panel := &review.ReviewPanel{
		Primary:     deepseekClient,
		Adversarial: chimeraClient,
	}

	commitMsg := "review run via helix CLI"

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	result, err := orchestrator.Review(ctx, panel, diff, commitMsg,
		review.CategoryBehavioral, flags.prURL)
	if err != nil {
		fmt.Fprintf(stderr, "error: review failed: %v\n", err)
		return revExitError
	}

	// DF-018: a panel member that did not deliberate (error or empty
	// verdict) makes the run DEGRADED — never report clean success.
	reasons := reviewDegradationReasons(result)

	if flags.jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			fmt.Fprintf(stderr, "error: marshal result: %v\n", err)
			return revExitError
		}
		if len(reasons) > 0 {
			fmt.Fprintf(stderr, "helix review run: REVIEW DEGRADED — %s\n", strings.Join(reasons, "; "))
			fmt.Fprintf(stderr, "helix review run: review incomplete; do not treat as a clean verdict\n")
			return revExitError
		}
		return revExitOK
	}

	fmt.Fprintf(stdout, "Review complete for PR %s\n", flags.prURL)
	fmt.Fprintf(stdout, "Consensus: %s (%d/%d models agree)\n",
		result.ConsensusLevel, result.ModelsAgree, result.TotalModels)
	fmt.Fprintf(stdout, "Diversity score: %d\n", result.DiversityScore)
	fmt.Fprintf(stdout, "Findings: %d\n", len(result.Bundle.Findings))
	for _, f := range result.Bundle.Findings {
		fmt.Fprintf(stdout, "  - [%s] %s: %s\n", f.Severity, f.File, f.Description)
	}

	if len(reasons) > 0 {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "⚠ REVIEW DEGRADED:")
		for _, r := range reasons {
			fmt.Fprintf(stdout, "  - %s\n", r)
		}
		fmt.Fprintln(stdout, "Review incomplete — do not treat as a clean verdict.")
		return revExitError
	}
	return revExitOK
}

// -----------------------------------------------------------------------------
// queue
// -----------------------------------------------------------------------------

func runReviewQueue(flags revFlags, stdout, stderr io.Writer) int {
	q := review.NewReviewQueue()

	// Try loading from default path
	defaultPath := review.DefaultQueuePath()
	if err := q.Load(defaultPath); err != nil {
		fmt.Fprintf(stderr, "warning: could not load queue from %s: %v\n", defaultPath, err)
	}

	items := q.ListPendingSorted()

	if flags.jsonOut {
		type queueEntry struct {
			ID             string   `json:"id"`
			PRURL          string   `json:"pr_url"`
			PriorityScore  float64  `json:"priority_score"`
			RiskScore      float64  `json:"risk_score"`
			Category       string   `json:"category"`
			Tier           string   `json:"trust_tier"`
			Status         string   `json:"status"`
			AssignedHuman  string   `json:"assigned_human,omitempty"`
			AssignedModels []string `json:"assigned_models,omitempty"`
		}
		var entries []queueEntry
		for _, item := range items {
			entries = append(entries, queueEntry{
				ID:             item.ID,
				PRURL:          item.PRURL,
				PriorityScore:  item.PriorityScore,
				RiskScore:      item.RiskScore,
				Category:       string(item.Category),
				Tier:           string(item.Tier),
				Status:         string(item.Status),
				AssignedHuman:  item.AssignedHuman,
				AssignedModels: item.AssignedModels,
			})
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(map[string]any{"items": entries, "count": len(entries)}); err != nil {
			fmt.Fprintf(stderr, "error: marshal queue: %v\n", err)
			return revExitError
		}
		return revExitOK
	}

	if len(items) == 0 {
		fmt.Fprintln(stdout, "No reviews in queue.")
		return revExitOK
	}

	fmt.Fprintf(stdout, "Review Queue — %d items\n\n", len(items))
	fmt.Fprintln(stdout, "ID\tPriority\tRisk\tCategory\tTier\tStatus")
	for _, item := range items {
		fmt.Fprintf(stdout, "%s\t%.0f\t%.0f\t%s\t%s\t%s\n",
			item.ID, item.PriorityScore, item.RiskScore,
			item.Category, item.Tier, item.Status)
	}
	return revExitOK
}

// -----------------------------------------------------------------------------
// assign
// -----------------------------------------------------------------------------

func runReviewAssign(flags revFlags, stdout, stderr io.Writer) int {
	if flags.prURL == "" {
		fmt.Fprintln(stderr, "error: --pr is required for assign")
		return revExitError
	}

	ra := review.NewReviewAssigner()

	item := review.ReviewQueueItem{
		ID:            "cli-assign",
		PRURL:         flags.prURL,
		AuthorAgentID: "unknown-agent",
		Category:      review.CategoryBehavioral,
		RiskScore:     50,
		SubmittedAt:   time.Now(),
		Tier:          trust.TierObserved,
	}

	// Build a representative pool
	pool := []review.ModelPoolEntry{
		{Model: review.ModelInfo{Model: "model-a", Provider: "openai"}, Provider: "openai", RLHF: "helpful"},
		{Model: review.ModelInfo{Model: "model-b", Provider: "deepseek"}, Provider: "deepseek", RLHF: "constitutional"},
		{Model: review.ModelInfo{Model: "model-c", Provider: "anthropic"}, Provider: "anthropic", RLHF: "dpo"},
	}

	result, err := ra.AssignReviewers(item, pool)
	if err != nil {
		fmt.Fprintf(stderr, "error: assign: %v\n", err)
		return revExitError
	}

	if flags.jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			fmt.Fprintf(stderr, "error: marshal result: %v\n", err)
			return revExitError
		}
		return revExitOK
	}

	fmt.Fprintf(stdout, "Review Assignment for PR: %s\n\n", flags.prURL)
	if result.AutoMerge {
		fmt.Fprintln(stdout, "✓ Auto-merge: gates passed, no review needed")
		return revExitOK
	}
	fmt.Fprintf(stdout, "Panel size:       %d\n", result.PanelSize)
	fmt.Fprintf(stdout, "Consensus needed: %d\n", result.ConsensusNeeded)
	fmt.Fprintf(stdout, "Human needed:     %v\n", result.HumanNeeded)
	if result.HumanReason != "" {
		fmt.Fprintf(stdout, "Human reason:     %s\n", result.HumanReason)
	}
	fmt.Fprintf(stdout, "Assigned models:  %v\n", result.AssignedModels)
	return revExitOK
}

// -----------------------------------------------------------------------------
// dismiss
// -----------------------------------------------------------------------------

func runReviewDismiss(flags revFlags, stdout, stderr io.Writer) int {
	if flags.findingID == "" {
		fmt.Fprintln(stderr, "error: --finding-id is required")
		return revExitError
	}
	if flags.dismissReason == "" {
		fmt.Fprintln(stderr, "error: --reason is required (false_positive, already_handled, out_of_scope, architectural_decision)")
		return revExitError
	}

	reason := review.DismissalReason(flags.dismissReason)
	if !reason.Valid() {
		fmt.Fprintf(stderr, "error: invalid reason %q (must be one of: false_positive, already_handled, out_of_scope, architectural_decision)\n", flags.dismissReason)
		return revExitError
	}

	storePath := flags.statePath
	if storePath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(stderr, "error: cannot determine home directory: %v\n", err)
			return revExitError
		}
		storePath = filepath.Join(home, ".helix", "dismissals.jsonl")
	}

	store, err := review.NewDismissalStore(storePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: open dismissals store: %v\n", err)
		return revExitError
	}

	tracker, _ := loadFPTracker("") // shared in-process tracker, no persistence needed for one-shot

	handler := review.NewDismissalHandler(store, tracker)

	d := review.Dismissal{
		FindingID: flags.findingID,
		Reason:    reason,
		Note:      flags.dismissNote,
		HumanID:   flags.dismissHumanID,
		AgentID:   flags.agentID,
		PRNumber:  flags.dismissPRNumber,
	}

	count, err := handler.ProcessDismissal(d)
	if err != nil {
		fmt.Fprintf(stderr, "error: process dismissal: %v\n", err)
		return revExitError
	}

	if flags.jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		out := map[string]any{
			"finding_id":      d.FindingID,
			"reason":          string(d.Reason),
			"human_id":        d.HumanID,
			"agent_id":        d.AgentID,
			"pr_number":       d.PRNumber,
			"override_count":  count,
			"override_weight": store.OverrideWeight(d.HumanID),
			"tracker_summary": tracker.Summary(),
		}
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(stderr, "error: marshal: %v\n", err)
			return revExitError
		}
		return revExitOK
	}

	fmt.Fprintf(stdout, "✓ Dismissal recorded\n")
	fmt.Fprintf(stdout, "  Finding:  %s\n", d.FindingID)
	fmt.Fprintf(stdout, "  Reason:   %s\n", d.Reason)
	if d.Note != "" {
		fmt.Fprintf(stdout, "  Note:     %s\n", d.Note)
	}
	fmt.Fprintf(stdout, "  Human:    %s (override weight: %.2f)\n", d.HumanID, store.OverrideWeight(d.HumanID))
	fmt.Fprintf(stdout, "  Agent:    %s\n", d.AgentID)
	fmt.Fprintf(stdout, "  Store:    %s\n", storePath)
	if d.Reason == review.DismissalFalsePositive {
		newCount := tracker.DismissalCount(d.AgentID)
		fmt.Fprintf(stdout, "  FP count: %d (flagged: %v)\n", newCount, tracker.IsFlagged(d.AgentID))
	}
	return revExitOK
}
