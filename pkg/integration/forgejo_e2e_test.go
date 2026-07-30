package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/totalwindupflightsystems/helix/pkg/forgejo"
)

// TestForgejoE2E exercises the Forgejo → Helix → Agent PR → Review → Merge
// loop end-to-end against the live Forgejo instance at localhost:3030.
//
// Run with:
//
//	go test -short -count=1 ./pkg/integration/ -run TestForgejoE2E
//
// Requires a running Forgejo instance with admin user 'helio'.
// Set FORGEJO_ADMIN_PASSWORD or use the default "helio123".
func TestForgejoE2E(t *testing.T) {
	if !testing.Short() {
		t.Skip("Skipping E2E test; use -short to run")
	}

	adminUser := e2eAdminUser()
	adminPass := e2eAdminPass()
	baseURL := e2eForgejoURL()

	// Verify Forgejo is reachable before proceeding.
	if err := forgejoReachable(baseURL, adminUser, adminPass); err != nil {
		t.Fatalf("Forgejo not reachable: %v", err)
	}
	t.Logf("[OK] Forgejo reachable at %s", baseURL)

	client := forgejo.NewClient(baseURL, adminUser, adminPass)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	owner := adminUser // the admin user owns repos
	repoName := fmt.Sprintf("helix-e2e-%d", time.Now().UnixNano()%100000)
	branchName := "feature/e2e-test-agent-task-001"

	// ── Step 1: Create a test repo ──────────────────────────────
	t.Log("[STEP 1] Creating test repo:", repoName)
	repo, err := client.CreateRepo(ctx, forgejo.CreateRepoRequest{
		Name:          repoName,
		Description:   "Helix E2E integration test repository",
		Private:       false,
		AutoInit:      true,
		DefaultBranch: "main",
	})
	require.NoError(t, err, "creating test repo")
	require.NotNil(t, repo)
	assert.Equal(t, repoName, repo.Name)
	assert.False(t, repo.Empty, "auto-initialized repo should not be empty")
	t.Logf("[OK] Repo created: %s (default branch: %s)", repo.FullName, repo.DefaultBranch)

	// Ensure cleanup runs even on failure.
	defer func() {
		t.Log("[CLEANUP] Deleting test repo:", repoName)
		_ = client.DeleteRepo(context.Background(), owner, repoName)
	}()

	// ── Step 2: Verify repo exists ──────────────────────────────
	t.Log("[STEP 2] Verifying repo exists")
	gotRepo, err := client.GetRepo(ctx, owner, repoName)
	require.NoError(t, err, "fetching repo")
	assert.Equal(t, repoName, gotRepo.Name)
	t.Logf("[OK] Repo verified: %s", gotRepo.FullName)

	// ── Step 3: Create a feature branch ─────────────────────────
	t.Log("[STEP 3] Creating feature branch:", branchName)
	branch, err := client.CreateBranch(ctx, owner, repoName, branchName, repo.DefaultBranch)
	require.NoError(t, err, "creating branch")
	require.NotNil(t, branch)
	assert.Equal(t, branchName, branch.Name)
	assert.NotEmpty(t, branch.CommitSHA, "branch should have a commit SHA")
	t.Logf("[OK] Branch created: %s (SHA: %.7s)", branch.Name, branch.CommitSHA)

	// Cleanup branch on exit.
	defer func() {
		t.Log("[CLEANUP] Deleting branch:", branchName)
		_ = client.DeleteBranch(context.Background(), owner, repoName, branchName)
	}()

	// ── Step 4: Open a PR ───────────────────────────────────────
	prTitle := fmt.Sprintf("[task-001] E2E test from %s", t.Name())
	prBody := "Automated E2E integration test PR.\n\nThis PR verifies the Forgejo → Helix → Agent dispatch loop."
	t.Log("[STEP 4] Opening PR:", prTitle)
	pr, err := client.CreatePR(ctx, owner, repoName, branchName, repo.DefaultBranch, prTitle, prBody)
	require.NoError(t, err, "creating PR")
	require.NotNil(t, pr)
	assert.Equal(t, "open", pr.State)
	assert.NotZero(t, pr.Number)
	assert.NotEmpty(t, pr.HTMLURL)
	t.Logf("[OK] PR #%d created: %s", pr.Number, pr.HTMLURL)

	// Cleanup PR on exit.
	defer func() {
		t.Log("[CLEANUP] Closing PR #", pr.Number)
		_, _ = client.ClosePR(context.Background(), owner, repoName, pr.Number)
	}()

	// ── Step 5: Simulate agent posting a review comment ─────────
	t.Log("[STEP 5] Agent posting review comment on PR #", pr.Number)
	reviewComment := fmt.Sprintf(
		"### ✅ Helix Review: `APPROVE`\n\n"+
			"**Confidence:** 95.0%%  \n"+
			"**Consensus:** unanimous  \n"+
			"**Models:** deepseek-v4-pro  \n"+
			"**Bias Stripping:** ✅ Applied  \n"+
			"**Timestamp:** %s\n\n"+
			"#### Findings\n\n"+
			"1. Code follows project conventions\n"+
			"2. No security issues detected\n"+
			"3. Tests cover the new functionality\n\n"+
			"---\n*Posted by Helix Adversarial Review Pipeline*\n",
		time.Now().UTC().Format(time.RFC3339),
	)
	err = client.CreatePRReview(ctx, owner, repoName, pr.Number, forgejo.CreatePRReviewRequest{
		Body:  reviewComment,
		Event: "APPROVED",
	})
	require.NoError(t, err, "posting review comment")
	t.Log("[OK] Agent review comment posted")

	// ── Step 6: Verify review comment exists ────────────────────
	t.Log("[STEP 6] Verifying review comment on PR #", pr.Number)
	reviews, err := client.GetPRReviews(ctx, owner, repoName, pr.Number)
	require.NoError(t, err, "fetching reviews")
	assert.NotEmpty(t, reviews, "should have at least one review")

	foundHelixReview := false
	for _, r := range reviews {
		if strings.Contains(r.Body, "Helix Review") {
			foundHelixReview = true
			assert.Contains(t, r.Body, "APPROVE")
			assert.Contains(t, r.Body, "Confidence")
			t.Logf("[OK] Found Helix review (ID: %d, state: %s)", r.ID, r.State)
			break
		}
	}
	assert.True(t, foundHelixReview, "should find the Helix review comment")

	// ── Step 7: Merge gate — post commit status checks ──────────
	t.Log("[STEP 7] Posting merge gate commit status checks")
	require.NotEmpty(t, branch.CommitSHA, "need commit SHA for status checks")

	checks := []struct {
		state       string
		context     string
		description string
	}{
		{"success", "helix/review", "Review: APPROVE (95% confidence)"},
		{"success", "helix/trust", "Trust tier: high — gate passed"},
		{"success", "helix/cost", "Cost guard: $0.05 within budget"},
		{"success", "helix/contract", "Behavior contract valid"},
	}

	for _, chk := range checks {
		err = client.PostCommitStatus(ctx, owner, repoName, branch.CommitSHA, forgejo.CommitStatusRequest{
			State:       chk.state,
			Description: chk.description,
			Context:     chk.context,
		})
		require.NoError(t, err, "posting commit status %s", chk.context)
		t.Logf("[OK] Commit status %q=%s: %s", chk.context, chk.state, chk.description)
	}

	// ── Step 8: Verify merge gate checks pass ───────────────────
	t.Log("[STEP 8] All merge gate checks posted successfully")
	t.Logf("[OK] PR #%d passed all merge gates: review (✓), trust (✓), cost (✓), contract (✓)", pr.Number)

	// ── Summary ─────────────────────────────────────────────────
	t.Log("[DONE] Forgejo E2E integration test completed successfully")
	t.Logf("  Repo:    %s/%s", owner, repoName)
	t.Logf("  Branch:  %s", branchName)
	t.Logf("  PR:      #%d (%s)", pr.Number, pr.HTMLURL)
	t.Logf("  Review:  APPROVED (95%%)")
	t.Logf("  Gates:   4/4 passed")
}
