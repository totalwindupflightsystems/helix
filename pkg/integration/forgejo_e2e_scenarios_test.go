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

// ---------------------------------------------------------------------------
// Shared helpers for scenario tests
// ---------------------------------------------------------------------------

// fileCreateResponse mirrors the Forgejo v1.21 response for
// POST /api/v1/repos/{owner}/{repo}/contents/{path}.
// Bug #46–#47: the commit SHA is in commit.sha (nested), NOT content.sha.
type fileCreateResponse struct {
	Content struct {
		SHA string `json:"sha"`
	} `json:"content"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

// commitStatusEntry mirrors an individual status entry from
// GET /api/v1/repos/{owner}/{repo}/commits/{sha}/statuses.
type commitStatusEntry struct {
	Status      string `json:"status"`
	Context     string `json:"context"`
	Description string `json:"description"`
}

// combinedStatus mirrors the combined status from
// GET /api/v1/repos/{owner}/{repo}/commits/{sha}/status.
type combinedStatus struct {
	State      string              `json:"state"`
	TotalCount int                 `json:"total_count"`
	Statuses   []commitStatusEntry `json:"statuses"`
}

// createFileOnBranch creates a file on the given branch and returns the
// commit SHA from the response. This avoids the Forgejo v1.21 bug where
// GET ?ref=branch returns 404 on fresh branches (bug #46/#47).
func createFileOnBranch(t *testing.T, baseURL, adminUser, adminPass, owner, repo, branch, filePath, content, message string) string {
	t.Helper()

	payload := map[string]string{
		"content": content,
		"message": message,
		"branch":  branch,
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err, "marshaling file create request")

	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/contents/%s",
		baseURL, owner, repo, filePath)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, strings.NewReader(string(body)))
	require.NoError(t, err, "building file create request")
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(adminUser, adminPass)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err, "creating file on branch")
	defer resp.Body.Close()

	require.Equal(t, http.StatusCreated, resp.StatusCode,
		"file create should return 201, got %d", resp.StatusCode)

	var result fileCreateResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err, "decoding file create response")

	// Bug #46/#47: use commit.sha (nested), NOT content.sha
	require.NotEmpty(t, result.Commit.SHA, "commit SHA must not be empty")
	return result.Commit.SHA
}

// getCommitStatuses fetches all status checks for a commit.
func getCommitStatuses(t *testing.T, baseURL, adminUser, adminPass, owner, repo, sha string) []commitStatusEntry {
	t.Helper()

	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/commits/%s/statuses",
		baseURL, owner, repo, sha)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err, "building statuses request")
	req.SetBasicAuth(adminUser, adminPass)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err, "fetching commit statuses")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode,
		"statuses request should return 200, got %d", resp.StatusCode)

	var statuses []commitStatusEntry
	err = json.NewDecoder(resp.Body).Decode(&statuses)
	require.NoError(t, err, "decoding statuses response")
	return statuses
}

// getCombinedStatus fetches the combined commit status.
func getCombinedStatus(t *testing.T, baseURL, adminUser, adminPass, owner, repo, sha string) combinedStatus {
	t.Helper()

	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/commits/%s/status",
		baseURL, owner, repo, sha)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err, "building combined status request")
	req.SetBasicAuth(adminUser, adminPass)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err, "fetching combined status")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode,
		"combined status request should return 200, got %d", resp.StatusCode)

	var cs combinedStatus
	err = json.NewDecoder(resp.Body).Decode(&cs)
	require.NoError(t, err, "decoding combined status response")
	return cs
}

// ---------------------------------------------------------------------------
// Scenario 1: Multi-agent review pipeline
// ---------------------------------------------------------------------------

// TestForgejoE2E_MultiAgentReview simulates multiple AI agents reviewing
// the same PR independently. Alpha approves, Beta rejects — both use
// COMMENT events (bug #46: Forgejo v1.21 rejects self-approve/reject).
// We then verify both reviews appear and merge gates reflect the reviews.
func TestForgejoE2E_MultiAgentReview(t *testing.T) {
	adminUser := e2eAdminUser()
	adminPass := e2eAdminPass()
	baseURL := e2eForgejoURL()
	// Verify Forgejo is reachable.
	if err := forgejoReachable(baseURL, adminUser, adminPass); err != nil {
		t.Skipf("Forgejo not reachable, skipping E2E: %v", err)
	}
	t.Logf("[OK] Forgejo reachable at %s", baseURL)

	client := forgejo.NewClient(baseURL, adminUser, adminPass)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	owner := adminUser
	repoName := fmt.Sprintf("helix-multi-review-%d", time.Now().UnixNano()%100000)
	branchName := "feature/multi-agent-review"

	// ── Step 1: Create repo ──────────────────────────────────────
	t.Log("[STEP 1] Creating test repo:", repoName)
	repo, err := client.CreateRepo(ctx, forgejo.CreateRepoRequest{
		Name:          repoName,
		Description:   "Multi-agent review E2E test",
		Private:       false,
		AutoInit:      true,
		DefaultBranch: "main",
	})
	require.NoError(t, err, "creating test repo")
	defer func() {
		_ = client.DeleteRepo(context.Background(), owner, repoName)
	}()
	t.Logf("[OK] Repo: %s", repo.FullName)

	// ── Step 2: Create branch ────────────────────────────────────
	t.Log("[STEP 2] Creating branch:", branchName)
	branch, err := client.CreateBranch(ctx, owner, repoName, branchName, repo.DefaultBranch)
	require.NoError(t, err, "creating branch")
	defer func() {
		_ = client.DeleteBranch(context.Background(), owner, repoName, branchName)
	}()
	t.Logf("[OK] Branch: %s (SHA: %.7s)", branch.Name, branch.CommitSHA)

	// ── Step 3: Open PR ──────────────────────────────────────────
	prTitle := "[task-002] Multi-agent review test"
	prBody := "Testing multi-agent review pipeline."
	t.Log("[STEP 3] Opening PR:", prTitle)
	pr, err := client.CreatePR(ctx, owner, repoName, branchName, repo.DefaultBranch, prTitle, prBody)
	require.NoError(t, err, "creating PR")
	defer func() {
		_, _ = client.ClosePR(context.Background(), owner, repoName, pr.Number)
	}()
	assert.Equal(t, "open", pr.State)
	t.Logf("[OK] PR #%d: %s", pr.Number, pr.HTMLURL)

	// ── Step 4: Alpha agent reviews (COMMENT — approval in body) ─
	t.Log("[STEP 4] Alpha agent review (APPROVE via COMMENT)")
	alphaBody := fmt.Sprintf(
		"### ✅ Alpha Review: `APPROVE`\n\n"+
			"**Confidence:** 92.0%%\n"+
			"**Model:** deepseek-v4-pro\n"+
			"**Timestamp:** %s\n\n"+
			"LGTM. Code is clean and follows conventions.\n",
		time.Now().UTC().Format(time.RFC3339),
	)
	// Bug #46: ALWAYS use COMMENT event to avoid self-review rejection
	err = client.CreatePRReview(ctx, owner, repoName, pr.Number, forgejo.CreatePRReviewRequest{
		Body:  alphaBody,
		Event: "COMMENT",
	})
	require.NoError(t, err, "posting alpha review")
	t.Log("[OK] Alpha review posted")

	// ── Step 5: Beta agent reviews (COMMENT — rejection in body) ─
	t.Log("[STEP 5] Beta agent review (REJECT via COMMENT)")
	betaBody := fmt.Sprintf(
		"### ❌ Beta Review: `REJECT`\n\n"+
			"**Confidence:** 88.0%%\n"+
			"**Model:** claude-opus-4-5\n"+
			"**Timestamp:** %s\n\n"+
			"Needs more test coverage on edge cases.\n",
		time.Now().UTC().Format(time.RFC3339),
	)
	err = client.CreatePRReview(ctx, owner, repoName, pr.Number, forgejo.CreatePRReviewRequest{
		Body:  betaBody,
		Event: "COMMENT",
	})
	require.NoError(t, err, "posting beta review")
	t.Log("[OK] Beta review posted")

	// ── Step 6: Verify both reviews appear ───────────────────────
	t.Log("[STEP 6] Verifying both reviews on PR #", pr.Number)
	reviews, err := client.GetPRReviews(ctx, owner, repoName, pr.Number)
	require.NoError(t, err, "fetching reviews")
	assert.GreaterOrEqual(t, len(reviews), 2, "should have at least 2 reviews")

	foundAlpha, foundBeta := false, false
	for _, r := range reviews {
		if strings.Contains(r.Body, "Alpha Review") {
			foundAlpha = true
			assert.Contains(t, r.Body, "APPROVE")
			assert.Contains(t, r.Body, "92.0%")
			t.Logf("[OK] Alpha review found (ID: %d)", r.ID)
		}
		if strings.Contains(r.Body, "Beta Review") {
			foundBeta = true
			assert.Contains(t, r.Body, "REJECT")
			assert.Contains(t, r.Body, "88.0%")
			t.Logf("[OK] Beta review found (ID: %d)", r.ID)
		}
	}
	assert.True(t, foundAlpha, "should find Alpha's review")
	assert.True(t, foundBeta, "should find Beta's review")

	// ── Step 7: Merge gates reflect reviews ──────────────────────
	t.Log("[STEP 7] Posting merge gate status checks")
	require.NotEmpty(t, branch.CommitSHA, "need commit SHA for status checks")

	checks := []struct {
		state       string
		context     string
		description string
	}{
		{"success", "helix/review-alpha", "Alpha: APPROVE (92% confidence)"},
		{"failure", "helix/review-beta", "Beta: REJECT (88% confidence)"},
		{"success", "helix/trust", "Trust tier: high — gate passed"},
		{"warning", "helix/consensus", "Consensus: split (Alpha approves, Beta rejects)"},
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

	// Verify statuses were posted
	statuses := getCommitStatuses(t, baseURL, adminUser, adminPass, owner, repoName, branch.CommitSHA)
	assert.GreaterOrEqual(t, len(statuses), 4, "should have at least 4 status checks")
	t.Logf("[OK] %d status checks on commit", len(statuses))

	// Verify combined status reflects the split
	cs := getCombinedStatus(t, baseURL, adminUser, adminPass, owner, repoName, branch.CommitSHA)
	t.Logf("[OK] Combined status: %s (total: %d checks)", cs.State, cs.TotalCount)

	// ── Summary ──────────────────────────────────────────────────
	t.Log("[DONE] Multi-agent review pipeline completed")
	t.Logf("  Repo:   %s/%s", owner, repoName)
	t.Logf("  PR:     #%d", pr.Number)
	t.Logf("  Alpha:  APPROVE (COMMENT)")
	t.Logf("  Beta:   REJECT (COMMENT)")
	t.Logf("  Gates:  4 status checks posted")
}

// ---------------------------------------------------------------------------
// Scenario 2: Commit status pipeline
// ---------------------------------------------------------------------------

// TestForgejoE2E_CommitStatusPipeline exercises the commit status workflow:
// push a file to a branch, post a commit status, and verify it appears in
// the PR status checks.
//
// Uses the nested commit SHA pattern (bug #46/#47): Forgejo v1.21 returns
// commit info as {"commit": {"sha": "..."}}, not a flat commit_sha.
func TestForgejoE2E_CommitStatusPipeline(t *testing.T) {
	adminUser := e2eAdminUser()
	adminPass := e2eAdminPass()
	baseURL := e2eForgejoURL()
	// Verify Forgejo is reachable.
	if err := forgejoReachable(baseURL, adminUser, adminPass); err != nil {
		t.Skipf("Forgejo not reachable, skipping E2E: %v", err)
	}
	t.Logf("[OK] Forgejo reachable at %s", baseURL)

	client := forgejo.NewClient(baseURL, adminUser, adminPass)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	owner := adminUser
	repoName := fmt.Sprintf("helix-status-pipe-%d", time.Now().UnixNano()%100000)
	branchName := "feature/commit-status-test"

	// ── Step 1: Create repo ──────────────────────────────────────
	t.Log("[STEP 1] Creating test repo:", repoName)
	repo, err := client.CreateRepo(ctx, forgejo.CreateRepoRequest{
		Name:          repoName,
		Description:   "Commit status pipeline E2E test",
		Private:       false,
		AutoInit:      true,
		DefaultBranch: "main",
	})
	require.NoError(t, err, "creating test repo")
	defer func() {
		_ = client.DeleteRepo(context.Background(), owner, repoName)
	}()
	t.Logf("[OK] Repo: %s", repo.FullName)

	// ── Step 2: Create branch ────────────────────────────────────
	t.Log("[STEP 2] Creating branch:", branchName)
	branch, err := client.CreateBranch(ctx, owner, repoName, branchName, repo.DefaultBranch)
	require.NoError(t, err, "creating branch")
	defer func() {
		_ = client.DeleteBranch(context.Background(), owner, repoName, branchName)
	}()
	t.Logf("[OK] Branch: %s (SHA: %.7s)", branch.Name, branch.CommitSHA)

	// ── Step 3: Push file to branch ──────────────────────────────
	// Bug #46/#47: capture commit SHA from file create response
	t.Log("[STEP 3] Pushing file to branch:", branchName)
	// base64-encoded "package main\n\nfunc main() {\n\tprintln(\"helix ci/cd\")\n}\n"
	fileContent := "cGFja2FnZSBtYWluCgpmdW5jIG1haW4oKSB7CglwcmludGxuKCJoZWxpeCBjaS9jZCIpCn0K"
	commitSHA := createFileOnBranch(t, baseURL, adminUser, adminPass,
		owner, repoName, branchName, "main.go", fileContent, "feat: add main.go")
	t.Logf("[OK] File pushed — commit SHA: %.7s", commitSHA)

	// ── Step 4: Open PR ──────────────────────────────────────────
	prTitle := "[task-003] Commit status test"
	prBody := "Testing commit status pipeline."
	t.Log("[STEP 4] Opening PR:", prTitle)
	pr, err := client.CreatePR(ctx, owner, repoName, branchName, repo.DefaultBranch, prTitle, prBody)
	require.NoError(t, err, "creating PR")
	defer func() {
		_, _ = client.ClosePR(context.Background(), owner, repoName, pr.Number)
	}()
	t.Logf("[OK] PR #%d: %s", pr.Number, pr.HTMLURL)

	// ── Step 5: Post commit status (success) ─────────────────────
	// Bug #46/#47: use the commit SHA from file create, NOT content.sha
	t.Log("[STEP 5] Posting commit status on commit:", commitSHA[:7])
	err = client.PostCommitStatus(ctx, owner, repoName, commitSHA, forgejo.CommitStatusRequest{
		State:       "success",
		Context:     "ci/build",
		Description: "Build: passed (go build ./...)",
	})
	require.NoError(t, err, "posting commit status")
	t.Log("[OK] Commit status posted: ci/build=success")

	// Post additional status checks
	extraChecks := []struct {
		state       string
		context     string
		description string
	}{
		{"success", "ci/test", "Tests: 42 passed, 0 failed"},
		{"success", "ci/lint", "Lint: no issues found"},
	}

	for _, chk := range extraChecks {
		err = client.PostCommitStatus(ctx, owner, repoName, commitSHA, forgejo.CommitStatusRequest{
			State:       chk.state,
			Context:     chk.context,
			Description: chk.description,
		})
		require.NoError(t, err, "posting %s status", chk.context)
		t.Logf("[OK] Status %q=%s", chk.context, chk.state)
	}

	// ── Step 6: Verify statuses appear ───────────────────────────
	t.Log("[STEP 6] Verifying commit statuses")
	statuses := getCommitStatuses(t, baseURL, adminUser, adminPass, owner, repoName, commitSHA)
	assert.GreaterOrEqual(t, len(statuses), 3, "should have at least 3 status checks")

	foundBuild, foundTest, foundLint := false, false, false
	for _, s := range statuses {
		switch s.Context {
		case "ci/build":
			foundBuild = true
			assert.Equal(t, "success", s.Status)
		case "ci/test":
			foundTest = true
			assert.Equal(t, "success", s.Status)
		case "ci/lint":
			foundLint = true
			assert.Equal(t, "success", s.Status)
		}
	}
	assert.True(t, foundBuild, "should find ci/build status")
	assert.True(t, foundTest, "should find ci/test status")
	assert.True(t, foundLint, "should find ci/lint status")
	t.Logf("[OK] All 3 status checks verified: build ✓, test ✓, lint ✓")

	// Verify combined status
	cs := getCombinedStatus(t, baseURL, adminUser, adminPass, owner, repoName, commitSHA)
	assert.Equal(t, "success", cs.State, "combined status should be success")
	assert.GreaterOrEqual(t, cs.TotalCount, 3, "combined should have at least 3 statuses")
	t.Logf("[OK] Combined status: %s (%d checks)", cs.State, cs.TotalCount)

	// ── Summary ──────────────────────────────────────────────────
	t.Log("[DONE] Commit status pipeline completed")
	t.Logf("  Repo:    %s/%s", owner, repoName)
	t.Logf("  Branch:  %s", branchName)
	t.Logf("  Commit:  %.7s", commitSHA)
	t.Logf("  PR:      #%d", pr.Number)
	t.Logf("  Checks:  3/3 passed")
}

// ---------------------------------------------------------------------------
// Scenario 3: Full CI/CD simulation
// ---------------------------------------------------------------------------

// TestForgejoE2E_FullCICDSimulation exercises the complete CI/CD loop:
// repo create → branch → push code → open PR → agent review → commit
// status → verify merge gates → merge → cleanup.
func TestForgejoE2E_FullCICDSimulation(t *testing.T) {
	adminUser := e2eAdminUser()
	adminPass := e2eAdminPass()
	baseURL := e2eForgejoURL()
	// Verify Forgejo is reachable.
	if err := forgejoReachable(baseURL, adminUser, adminPass); err != nil {
		t.Skipf("Forgejo not reachable, skipping E2E: %v", err)
	}
	t.Logf("[OK] Forgejo reachable at %s", baseURL)

	client := forgejo.NewClient(baseURL, adminUser, adminPass)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	owner := adminUser
	repoName := fmt.Sprintf("helix-cicd-%d", time.Now().UnixNano()%100000)
	branchName := "feature/full-cicd-sim"

	// ── Step 1: Create repo ──────────────────────────────────────
	t.Log("[STEP 1] Creating test repo:", repoName)
	repo, err := client.CreateRepo(ctx, forgejo.CreateRepoRequest{
		Name:          repoName,
		Description:   "Full CI/CD simulation E2E test",
		Private:       false,
		AutoInit:      true,
		DefaultBranch: "main",
	})
	require.NoError(t, err, "creating test repo")
	defer func() {
		t.Log("[CLEANUP] Deleting repo:", repoName)
		_ = client.DeleteRepo(context.Background(), owner, repoName)
	}()
	t.Logf("[OK] Repo: %s", repo.FullName)

	// ── Step 2: Create branch ────────────────────────────────────
	t.Log("[STEP 2] Creating branch:", branchName)
	branch, err := client.CreateBranch(ctx, owner, repoName, branchName, repo.DefaultBranch)
	require.NoError(t, err, "creating branch")
	defer func() {
		_ = client.DeleteBranch(context.Background(), owner, repoName, branchName)
	}()
	t.Logf("[OK] Branch: %s (SHA: %.7s)", branch.Name, branch.CommitSHA)

	// ── Step 3: Push code to branch ──────────────────────────────
	t.Log("[STEP 3] Pushing code to branch:", branchName)
	// base64-encoded "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"helix cicd pipeline v1.0\")\n}\n"
	fileContent := "cGFja2FnZSBtYWluCgppbXBvcnQgImZtdCIKCmZ1bmMgbWFpbigpIHsKCWZtdC5QcmludGxuKCJoZWxpeCBjaWNkIHBpcGVsaW5lIHYxLjAiKQp9Cg=="
	commitSHA := createFileOnBranch(t, baseURL, adminUser, adminPass,
		owner, repoName, branchName, "main.go", fileContent, "feat: add main.go with fmt import")
	t.Logf("[OK] Code pushed — commit: %.7s", commitSHA)

	// ── Step 4: Open PR ──────────────────────────────────────────
	prTitle := "[task-004] Full CI/CD simulation"
	prBody := "End-to-end CI/CD pipeline simulation.\n\n- Build check\n- Test suite\n- Agent review\n- Merge gates"
	t.Log("[STEP 4] Opening PR:", prTitle)
	pr, err := client.CreatePR(ctx, owner, repoName, branchName, repo.DefaultBranch, prTitle, prBody)
	require.NoError(t, err, "creating PR")
	t.Logf("[OK] PR #%d: %s", pr.Number, pr.HTMLURL)

	// ── Step 5: Agent review (COMMENT — approval in body) ────────
	t.Log("[STEP 5] Agent posting review on PR #", pr.Number)
	reviewBody := fmt.Sprintf(
		"### ✅ Helix Review: `APPROVE`\n\n"+
			"**Confidence:** 97.0%%  \n"+
			"**Consensus:** unanimous  \n"+
			"**Models:** deepseek-v4-pro, claude-opus-4-5  \n"+
			"**Bias Stripping:** ✅ Applied  \n"+
			"**Timestamp:** %s\n\n"+
			"#### Findings\n\n"+
			"1. Code follows Go conventions (gofmt clean)\n"+
			"2. No security issues detected\n"+
			"3. Appropriate use of standard library\n\n"+
			"---\n*Posted by Helix Adversarial Review Pipeline*\n",
		time.Now().UTC().Format(time.RFC3339),
	)
	// Bug #46: use COMMENT event for agent reviews
	err = client.CreatePRReview(ctx, owner, repoName, pr.Number, forgejo.CreatePRReviewRequest{
		Body:  reviewBody,
		Event: "COMMENT",
	})
	require.NoError(t, err, "posting agent review")
	t.Log("[OK] Agent review posted (COMMENT)")

	// ── Step 6: Verify review appears ────────────────────────────
	t.Log("[STEP 6] Verifying review")
	reviews, err := client.GetPRReviews(ctx, owner, repoName, pr.Number)
	require.NoError(t, err, "fetching reviews")
	foundReview := false
	for _, r := range reviews {
		if strings.Contains(r.Body, "Helix Review") {
			foundReview = true
			assert.Contains(t, r.Body, "APPROVE")
			assert.Contains(t, r.Body, "97.0%")
			t.Logf("[OK] Helix review found (ID: %d)", r.ID)
			break
		}
	}
	assert.True(t, foundReview, "should find the Helix review")

	// ── Step 7: Post commit status checks ────────────────────────
	t.Log("[STEP 7] Posting CI/CD status checks on commit:", commitSHA[:7])

	checks := []struct {
		state       string
		context     string
		description string
	}{
		{"success", "ci/build", "Build: go build ./... passed"},
		{"success", "ci/test", "Tests: 128 passed, 0 failed, 0 skipped"},
		{"success", "ci/lint", "Lint: go vet clean, golangci-lint passed"},
		{"success", "helix/review", "Review: APPROVE (97% confidence)"},
		{"success", "helix/trust", "Trust tier: high — gate passed"},
		{"success", "helix/cost", "Cost guard: $0.03 within budget"},
		{"success", "helix/contract", "Behavior contract valid"},
	}

	for _, chk := range checks {
		err = client.PostCommitStatus(ctx, owner, repoName, commitSHA, forgejo.CommitStatusRequest{
			State:       chk.state,
			Context:     chk.context,
			Description: chk.description,
		})
		require.NoError(t, err, "posting %s status", chk.context)
	}

	// ── Step 8: Verify merge gates ───────────────────────────────
	t.Log("[STEP 8] Verifying merge gate status checks")
	statuses := getCommitStatuses(t, baseURL, adminUser, adminPass, owner, repoName, commitSHA)
	assert.GreaterOrEqual(t, len(statuses), 7, "should have at least 7 status checks")

	allSuccess := true
	for _, s := range statuses {
		if s.Status != "success" {
			allSuccess = false
			t.Logf("[WARN] Status %q = %s (expected success)", s.Context, s.Status)
		}
	}
	assert.True(t, allSuccess, "all status checks should be success")

	cs := getCombinedStatus(t, baseURL, adminUser, adminPass, owner, repoName, commitSHA)
	assert.Equal(t, "success", cs.State, "combined status should be success")
	t.Logf("[OK] All %d merge gate checks passed (combined: %s)", len(statuses), cs.State)

	// ── Step 9: Merge PR ─────────────────────────────────────────
	// Bug: Forgejo expects "Do":"merge" (capital D), not "do":"merge".
	// The forgejo.Client.MergePR sends lowercase "do" which Forgejo
	// rejects with 405. Use a direct HTTP call with correct casing.
	t.Log("[STEP 9] Merging PR #", pr.Number)
	mergeBody, _ := json.Marshal(map[string]string{"Do": "merge"})
	mergeReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/api/v1/repos/%s/%s/pulls/%d/merge",
			baseURL, owner, repoName, pr.Number),
		strings.NewReader(string(mergeBody)))
	require.NoError(t, err, "building merge request")
	mergeReq.Header.Set("Content-Type", "application/json")
	mergeReq.SetBasicAuth(adminUser, adminPass)
	mergeResp, err := (&http.Client{Timeout: 10 * time.Second}).Do(mergeReq)
	require.NoError(t, err, "sending merge request")
	mergeResp.Body.Close()
	require.Equal(t, http.StatusOK, mergeResp.StatusCode,
		"merge should return 200, got %d", mergeResp.StatusCode)
	t.Logf("[OK] PR #%d merged successfully", pr.Number)

	// Verify PR is merged
	prs, err := client.ListPRs(ctx, owner, repoName, "closed")
	require.NoError(t, err, "listing closed PRs")
	foundMerged := false
	for _, p := range prs {
		if p.Number == pr.Number {
			foundMerged = true
			t.Logf("[OK] PR #%d is in closed state", p.Number)
			break
		}
	}
	assert.True(t, foundMerged, "merged PR should appear in closed PRs list")

	// ── Summary ──────────────────────────────────────────────────
	t.Log("[DONE] Full CI/CD simulation completed")
	t.Logf("  Repo:    %s/%s", owner, repoName)
	t.Logf("  Branch:  %s", branchName)
	t.Logf("  Commit:  %.7s", commitSHA)
	t.Logf("  PR:      #%d", pr.Number)
	t.Logf("  Review:  APPROVED (97%%)")
	t.Logf("  Gates:   7/7 passed")
	t.Logf("  Merge:   ✅ merged")
}
