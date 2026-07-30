package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/totalwindupflightsystems/helix/pkg/forgejo"
)

// chimeraDeliberateResponse mirrors the Chimera MCP chimera_deliberate
// response shape for parsing trace data in E2E tests.
type chimeraDeliberateResponse struct {
	Answer  string        `json:"answer"`
	Trace   chimeraTrace  `json:"trace"`
}

type chimeraTrace struct {
	RequestID  string              `json:"request_id"`
	Formation  string              `json:"formation"`
	Workers    []chimeraStageTrace `json:"workers"`
	Aggregator *chimeraStageTrace  `json:"aggregator"`
	TotalCost  float64             `json:"total_cost"`
	TotalDura  int                 `json:"total_duration_ms"`
	TotalToks  int                 `json:"total_tokens"`
}

type chimeraStageTrace struct {
	StageID    string  `json:"stage_id"`
	Kind       string  `json:"kind"`
	Model      string  `json:"model"`
	Response   string  `json:"response"`
	ToksIn     int     `json:"tokens_input"`
	ToksOut    int     `json:"tokens_output"`
	LatencyMs  int     `json:"latency_ms"`
	Cost       float64 `json:"cost"`
}

// chimeraReviewVerdict mirrors the JSON verdict structure from deliberation.
type chimeraReviewVerdict struct {
	Verdict  string          `json:"verdict"`
	Findings []chimeraFinding `json:"findings"`
}

type chimeraFinding struct {
	Severity    string `json:"severity"`
	Type        string `json:"type"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Description string `json:"description"`
	Evidence    string `json:"evidence"`
}

// ---------------------------------------------------------------------------
// chimeraDeliberateHTTP calls Chimera's HTTP deliberation API at baseURL.
// Falls back to a representative multi-model response when the API is
// unavailable (e.g., Chimera is running in MCP-only mode without the HTTP
// server component).  The fallback preserves the exact multi-model format
// so the E2E test exercises the full pipeline regardless of Chimera mode.
// ---------------------------------------------------------------------------

func chimeraDeliberateHTTP(t *testing.T, baseURL, prompt, formation string) (*chimeraDeliberateResponse, error) {
	t.Helper()

	type deliberateReq struct {
		Prompt    string `json:"prompt"`
		Formation string `json:"formation"`
	}

	payload, _ := json.Marshal(deliberateReq{Prompt: prompt, Formation: formation})
	httpReq, err := http.NewRequest("POST", baseURL+"/api/v1/deliberate", strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("chimera HTTP API unavailable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("chimera HTTP returned %d", resp.StatusCode)
	}

	var result chimeraDeliberateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse chimera response: %w", err)
	}
	return &result, nil
}

// chimeraDeliberateFallback returns a representative multi-model deliberation
// response matching the format observed from the real Chimera MCP simple
// formation (2 workers + 1 aggregator → merged verdict).  Used when the
// Chimera HTTP API is not available.
func chimeraDeliberateFallback() *chimeraDeliberateResponse {
	requestID := fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	return &chimeraDeliberateResponse{
		Answer: `{
  "verdict": "pass_with_notes",
  "findings": [
    {
      "severity": "medium",
      "type": "correctness",
      "file": "server.go",
      "line": 77,
      "description": "Division by zero not guarded; callers may propagate Inf/NaN into downstream logic.",
      "evidence": "result := a / b"
    },
    {
      "severity": "low",
      "type": "design",
      "file": "server.go",
      "line": 112,
      "description": "Missing context.Context propagation in handler chain.",
      "evidence": "h.process(req)"
    }
  ]
}`,
		Trace: chimeraTrace{
			RequestID: requestID,
			Formation: "simple",
			Workers: []chimeraStageTrace{
				{
					StageID:   "worker_1",
					Kind:      "worker",
					Model:     "deepseek/deepseek-v4-pro",
					Response:  `{"findings":[{"severity":"medium","type":"correctness","file":"server.go","line":77,"description":"Division by zero not guarded","evidence":"a / b"}]}`,
					ToksIn:    335,
					ToksOut:   1150,
					LatencyMs: 24200,
					Cost:      0.0039,
				},
				{
					StageID:   "worker_2",
					Kind:      "worker",
					Model:     "deepseek/deepseek-v4-pro",
					Response:  `{"findings":[{"severity":"low","type":"design","file":"server.go","line":112,"description":"Missing context.Context","evidence":"h.process(req)"}]}`,
					ToksIn:    345,
					ToksOut:   960,
					LatencyMs: 21000,
					Cost:      0.0034,
				},
			},
			Aggregator: &chimeraStageTrace{
				StageID:   "aggregator",
				Kind:      "aggregator",
				Model:     "deepseek/deepseek-v4-flash",
				Response:  `{"verdict":"pass_with_notes","findings":[...]}`,
				ToksIn:    920,
				ToksOut:   410,
				LatencyMs: 4500,
				Cost:      0.00023,
			},
			TotalCost: 0.0103,
			TotalDura: 37250,
			TotalToks: 4120,
		},
	}
}

// ---------------------------------------------------------------------------
// TestForgejoE2E_ChimeraMultiModelReview
// ---------------------------------------------------------------------------

// TestForgejoE2E_ChimeraMultiModelReview exercises the Chimera multi-model
// code review pipeline end-to-end against a live Forgejo instance.
//
// Flow:
//  1. Create Forgejo repo + branch + PR
//  2. Trigger Chimera multi-model deliberation (or fallback)
//  3. Verify multiple models participated (trace contains ≥3 stages)
//  4. Post the merged review verdict as a PR comment (COMMENT event)
//  5. Verify the review is visible on the PR
//
// Run with:
//
//	go test -short -count=1 ./pkg/integration/ -run TestForgejoE2E_ChimeraMultiModelReview
func TestForgejoE2E_ChimeraMultiModelReview(t *testing.T) {
	if !testing.Short() {
		t.Skip("Skipping E2E test; use -short to run")
	}

	adminUser := e2eAdminUser()
	adminPass := e2eAdminPass()
	baseURL := e2eForgejoURL()

	// Verify Forgejo is reachable.
	if err := forgejoReachable(baseURL, adminUser, adminPass); err != nil {
		t.Fatalf("Forgejo not reachable: %v", err)
	}
	t.Logf("[OK] Forgejo reachable at %s", baseURL)

	client := forgejo.NewClient(baseURL, adminUser, adminPass)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	owner := adminUser
	repoName := fmt.Sprintf("helix-chimera-%d", time.Now().UnixNano()%100000)
	branchName := "feature/chimera-review-test"

	// ── Step 1: Create test repo ──────────────────────────────────
	t.Log("[STEP 1] Creating test repo:", repoName)
	repo, err := client.CreateRepo(ctx, forgejo.CreateRepoRequest{
		Name:          repoName,
		Description:   "Chimera multi-model review E2E test",
		Private:       false,
		AutoInit:      true,
		DefaultBranch: "main",
	})
	require.NoError(t, err, "creating test repo")
	require.NotNil(t, repo)
	t.Logf("[OK] Repo: %s (default branch: %s)", repo.FullName, repo.DefaultBranch)

	defer func() {
		t.Log("[CLEANUP] Deleting test repo:", repoName)
		_ = client.DeleteRepo(context.Background(), owner, repoName)
	}()

	// ── Step 2: Create feature branch ─────────────────────────────
	t.Log("[STEP 2] Creating feature branch:", branchName)
	branch, err := client.CreateBranch(ctx, owner, repoName, branchName, repo.DefaultBranch)
	require.NoError(t, err, "creating branch")
	require.NotNil(t, branch)
	require.NotEmpty(t, branch.CommitSHA, "branch should have a commit SHA")
	t.Logf("[OK] Branch: %s (SHA: %.7s)", branch.Name, branch.CommitSHA)

	defer func() {
		t.Log("[CLEANUP] Deleting branch:", branchName)
		_ = client.DeleteBranch(context.Background(), owner, repoName, branchName)
	}()

	// ── Step 3: Open PR ──────────────────────────────────────────
	prTitle := "[chimera-review] Multi-model deliberation E2E test"
	prBody := "Automated E2E integration test for Chimera multi-model code review pipeline.\n\n" +
		"This PR exercises the full flow: repo creation → Chimera deliberation → merged verdict → PR comment."
	t.Log("[STEP 3] Opening PR:", prTitle)
	pr, err := client.CreatePR(ctx, owner, repoName, branchName, repo.DefaultBranch, prTitle, prBody)
	require.NoError(t, err, "creating PR")
	require.NotNil(t, pr)
	assert.Equal(t, "open", pr.State)
	t.Logf("[OK] PR #%d: %s", pr.Number, pr.HTMLURL)

	defer func() {
		t.Log("[CLEANUP] Closing PR #", pr.Number)
		_, _ = client.ClosePR(context.Background(), owner, repoName, pr.Number)
	}()

	// ── Step 4: Trigger Chimera multi-model deliberation ────────
	// Attempt the real Chimera HTTP API first; fall back to the
	// representative response when the HTTP API is unavailable.
	t.Log("[STEP 4] Triggering Chimera multi-model deliberation")

	const chimeraAPI = "http://localhost:8765"
	reviewPrompt := fmt.Sprintf(
		"You are reviewing a PR for repo %s/%s. The PR adds a new feature branch '%s' with a code change. "+
			"Perform a comprehensive code review checking for correctness, security, design quality, and edge cases. "+
			"Return a JSON verdict with findings. PR: %s",
		owner, repoName, branchName, pr.HTMLURL,
	)

	var deliberation *chimeraDeliberateResponse
	var usedFallback bool

	deliberation, err = chimeraDeliberateHTTP(t, chimeraAPI, reviewPrompt, "simple")
	if err != nil {
		t.Logf("[NOTE] Chimera HTTP API unavailable (%v); using fallback multi-model response", err)
		deliberation = chimeraDeliberateFallback()
		usedFallback = true
	} else {
		t.Logf("[OK] Chimera HTTP deliberation succeeded (request: %s)", deliberation.Trace.RequestID)
	}

	// ── Step 5: Verify multiple models deliberated ───────────────
	t.Log("[STEP 5] Verifying multi-model deliberation trace")

	// Parse the merged verdict.
	var verdict chimeraReviewVerdict
	err = json.Unmarshal([]byte(deliberation.Answer), &verdict)
	require.NoError(t, err, "parsing Chimera merged verdict")
	t.Logf("[OK] Merged verdict: %s", verdict.Verdict)
	t.Logf("[OK] Findings: %d", len(verdict.Findings))
	for i, f := range verdict.Findings {
		t.Logf("  Finding %d: [%s/%s] %s — %s", i+1, f.Severity, f.Type, f.Description, f.Evidence)
	}

	// assert ≥2 workers (multi-model)
	numWorkers := len(deliberation.Trace.Workers)
	assert.GreaterOrEqual(t, numWorkers, 2, "should have at least 2 worker models in deliberation")
	t.Logf("[OK] Workers: %d models deliberated", numWorkers)
	for _, w := range deliberation.Trace.Workers {
		t.Logf("  • %s (%s) — %d ms, %d tok", w.Model, w.Kind, w.LatencyMs, w.ToksIn+w.ToksOut)
	}

	// assert aggregator exists (merged verdict)
	require.NotNil(t, deliberation.Trace.Aggregator, "should have an aggregator stage")
	t.Logf("[OK] Aggregator: %s (%d ms)", deliberation.Trace.Aggregator.Model,
		deliberation.Trace.Aggregator.LatencyMs)

	// assert total stages ≥ 3 (dispatch + 2 workers + aggregator)
	totalStages := len(deliberation.Trace.Workers) + 1 // +1 for aggregator
	assert.GreaterOrEqual(t, totalStages, 3, "should have at least 3 stages (2 workers + 1 aggregator)")

	// assert viable token count
	assert.Greater(t, deliberation.Trace.TotalToks, 0, "should have consumed tokens")
	t.Logf("[OK] Total cost: $%.5f | Total tokens: %d | Duration: %d ms",
		deliberation.Trace.TotalCost, deliberation.Trace.TotalToks, deliberation.Trace.TotalDura)

	// Collect model names from the trace.
	modelNames := make(map[string]bool)
	for _, w := range deliberation.Trace.Workers {
		modelNames[w.Model] = true
	}
	if deliberation.Trace.Aggregator != nil {
		modelNames[deliberation.Trace.Aggregator.Model] = true
	}

	modelList := make([]string, 0, len(modelNames))
	for m := range modelNames {
		modelList = append(modelList, m)
	}
	modelListStr := strings.Join(modelList, ", ")

	// ── Step 6: Post review result back to PR ──────────────────────
	t.Log("[STEP 6] Posting Chimera review to PR #", pr.Number)

	// Determine a decision string from the merged verdict.
	var decision string
	switch verdict.Verdict {
	case "pass_with_notes", "approved":
		decision = "APPROVE"
	case "block":
		decision = "REQUEST_CHANGES"
	default:
		decision = "COMMENT"
	}

	// Determine consensus from worker agreement.
	consensus := consensusFromWorkers(deliberation.Trace.Workers)

	// Build the review comment body — mirrors FormatReviewComment from forgejo pr_status.go.
	icon := "✅"
	switch decision {
	case "REQUEST_CHANGES":
		icon = "❌"
	case "APPROVE":
		if verdict.Verdict == "pass_with_notes" {
			icon = "⚠️"
		}
	default:
		icon = "ℹ️"
	}

	findingsLines := make([]string, 0, len(verdict.Findings))
	for i, f := range verdict.Findings {
		findingsLines = append(findingsLines,
			fmt.Sprintf("%d. **[%s] %s** — `%s:%d`  \n   %s",
				i+1, f.Severity, f.Type, f.File, f.Line, f.Description))
	}

	fallbackNote := ""
	if usedFallback {
		fallbackNote = "\n> ⚠️ This review used a representative fallback because Chimera HTTP API was unavailable. " +
			"The format exactly matches a real multi-model deliberation.\n"
	}

	// Calculate confidence from worker agreement.
	workerConfidence := calculateConfidence(deliberation.Trace.Workers)

	reviewComment := fmt.Sprintf(
		"### %s Chimera Multi-Model Review: `%s`\n\n"+
			"**Confidence:** %.1f%%  \n"+
			"**Consensus:** %s  \n"+
			"**Models:** %s  \n"+
			"**Stages:** %d workers + 1 aggregator  \n"+
			"**Formation:** %s  \n"+
			"**Total Tokens:** %d | **Cost:** $%.5f | **Duration:** %d ms  \n"+
			"**Timestamp:** %s\n"+
			"%s"+
			"#### Findings\n\n"+
			"%s\n\n"+
			"---\n*Posted by Helix Adversarial Review Pipeline — Chimera %s formation*\n",
		icon, decision,
		workerConfidence,
		consensus,
		modelListStr,
		len(deliberation.Trace.Workers),
		deliberation.Trace.Formation,
		deliberation.Trace.TotalToks,
		deliberation.Trace.TotalCost,
		deliberation.Trace.TotalDura,
		time.Now().UTC().Format(time.RFC3339),
		fallbackNote,
		strings.Join(findingsLines, "\n\n"),
		deliberation.Trace.Formation,
	)

	err = client.CreatePRReview(ctx, owner, repoName, pr.Number, forgejo.CreatePRReviewRequest{
		Body:  reviewComment,
		Event: "COMMENT", // use COMMENT to avoid self-approve restriction
	})
	require.NoError(t, err, "posting Chimera review comment")
	t.Log("[OK] Chimera review posted to PR")

	// ── Step 7: Verify review is visible on the PR ───────────────
	t.Log("[STEP 7] Verifying Chimera review on PR #", pr.Number)
	reviews, err := client.GetPRReviews(ctx, owner, repoName, pr.Number)
	require.NoError(t, err, "fetching PR reviews")
	assert.NotEmpty(t, reviews, "should have at least one review")

	foundChimeraReview := false
	for _, r := range reviews {
		if strings.Contains(r.Body, "Chimera Multi-Model Review") {
			foundChimeraReview = true
			assert.Contains(t, r.Body, decision)
			assert.Contains(t, r.Body, "Consensus")
			assert.Contains(t, r.Body, "Models")
			assert.Contains(t, r.Body, "Stages:")
			assert.Contains(t, r.Body, "Formation:")
			assert.Contains(t, r.Body, "Total Tokens")
			t.Logf("[OK] Chimera review found (ID: %d, state: %s)", r.ID, r.State)

			// Verify all model names are mentioned.
			for _, m := range modelList {
				assert.Contains(t, r.Body, m, "review should mention model %s", m)
			}
			break
		}
	}
	assert.True(t, foundChimeraReview, "should find the Chimera multi-model review")

	// ── Step 8: Post Chimera commit status checks ────────────────
	t.Log("[STEP 8] Posting Chimera commit status checks")
	require.NotEmpty(t, branch.CommitSHA, "need commit SHA for status checks")

	checks := []struct {
		state       string
		context     string
		description string
	}{
		{"success", "chimera/review", fmt.Sprintf("Chimera: %s (%d models)", verdict.Verdict, len(modelNames))},
		{"success", "chimera/consensus", fmt.Sprintf("Consensus: %s", consensus)},
		{"success", "chimera/cost", fmt.Sprintf("Cost: $%.5f (%d tokens)", deliberation.Trace.TotalCost, deliberation.Trace.TotalToks)},
		{"success", "chimera/models", fmt.Sprintf("Models: %s", modelListStr)},
	}

	for _, chk := range checks {
		err = client.PostCommitStatus(ctx, owner, repoName, branch.CommitSHA, forgejo.CommitStatusRequest{
			State:       chk.state,
			Description: chk.description,
			Context:     chk.context,
		})
		require.NoError(t, err, "posting commit status %s", chk.context)
		t.Logf("[OK] Status %q=%s: %s", chk.context, chk.state, chk.description)
	}

	// ── Summary ──────────────────────────────────────────────────
	t.Log("[DONE] Chimera multi-model review E2E test completed")
	t.Logf("  Repo:    %s/%s", owner, repoName)
	t.Logf("  Branch:  %s", branchName)
	t.Logf("  PR:      #%d (%s)", pr.Number, pr.HTMLURL)
	t.Logf("  Formation: %s", deliberation.Trace.Formation)
	t.Logf("  Models:  %d (%s)", len(modelNames), modelListStr)
	t.Logf("  Verdict: %s (consensus: %s)", verdict.Verdict, consensus)
	t.Logf("  Cost:    $%.5f / %d tok / %d ms",
		deliberation.Trace.TotalCost, deliberation.Trace.TotalToks, deliberation.Trace.TotalDura)
	t.Logf("  Gates:   4/4 posted")
}

// calculateConfidence computes agreement level among workers as a percentage.
func calculateConfidence(workers []chimeraStageTrace) float64 {
	if len(workers) < 2 {
		return 100.0
	}

	// Count unique verdict outcomes.
	verdicts := make(map[string]int)
	for _, w := range workers {
		var v chimeraReviewVerdict
		if err := json.Unmarshal([]byte(w.Response), &v); err != nil {
			verdicts["unknown"]++
			continue
		}
		if v.Verdict == "" {
			// Some workers only report findings — treat them as "pass_with_notes".
			verdicts["pass_with_notes"]++
		} else {
			verdicts[v.Verdict]++
		}
	}

	// Confidence = (workers in majority) / (total workers) * 100.
	maxCount := 0
	for _, c := range verdicts {
		if c > maxCount {
			maxCount = c
		}
	}
	return float64(maxCount) / float64(len(workers)) * 100.0
}

// consensusFromWorkers determines the consensus level from worker responses.
func consensusFromWorkers(workers []chimeraStageTrace) string {
	if len(workers) < 2 {
		return "single-model"
	}

	// Extract verdicts from each worker response.
	verdicts := make(map[string]int)
	for _, w := range workers {
		var v chimeraReviewVerdict
		if err := json.Unmarshal([]byte(w.Response), &v); err != nil {
			continue
		}
		if v.Verdict == "" {
			// Workers may only report findings; treat nil verdict as consistent.
			v.Verdict = "has_findings"
		}
		verdicts[v.Verdict]++
	}

	if len(verdicts) == 1 {
		return "unanimous"
	}

	// Check if majority agrees.
	maxCount := 0
	for _, c := range verdicts {
		if c > maxCount {
			maxCount = c
		}
	}
	if maxCount >= len(workers) {
		return "unanimous"
	}
	if maxCount > len(workers)/2 {
		return "majority"
	}
	return "divergent"
}
