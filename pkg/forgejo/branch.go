// Package forgejo — branch.go
//
// Branch/PR primitives used by the dispatcher Ralph Loop.
// These methods extend the Client with the lifecycle operations needed to
// open a PR as part of a work item: create a feature branch from a base
// ref, then open the PR targeting that base.
//
// All methods use the same doRequest core as the rest of the package —
// they inherit BasicAuth, circuit-breaker, and retryable-error handling.
package forgejo

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// ---------------------------------------------------------------------------
// Request / Response types
// ---------------------------------------------------------------------------

// CreateBranchRequest is the body for POST /api/v1/repos/{owner}/{repo}/branches.
// Forgejo expects `new_branch_ref` (the new branch name) and optionally
// `old_ref` (a ref/branch/SHA to fork from). When old_ref is empty, the new
// branch is created from the repository's default branch.
type CreateBranchRequest struct {
	NewBranchRef string `json:"new_branch_ref"`
	OldRef       string `json:"old_ref,omitempty"`
}

// CreateBranchResponse mirrors the Forgejo branch payload returned on success.
// The HTTP body also includes commit/url/etc. — we expose the fields the
// dispatcher actually consumes (Name, SHA, URL) and keep the rest accessible
// through the raw JSON.
type CreateBranchResponse struct {
	Name      string `json:"name"` // e.g. "feature/test-agent-001"
	Ref       string `json:"ref"`  // refs/heads/<name>
	CommitSHA string `json:"commit_sha,omitempty"`
	URL       string `json:"url"` // API URL to this branch object
	HTMLURL   string `json:"html_url,omitempty"`
}

// ---------------------------------------------------------------------------
// CreateBranch
// ---------------------------------------------------------------------------

// CreateBranch creates a new branch on the given repository.
//
//   - owner / repo identify the target repo.
//   - newBranchName is the new branch's local name (e.g. "feature/wi-001-test-agent").
//   - fromRef optionally specifies an existing branch/tag/SHA. When empty,
//     Forgejo uses the repository's default branch.
//
// On success it returns the parsed branch payload. Errors:
//   - 404 if the repo or fromRef doesn't exist (returned as APIError).
//   - 409 if the new branch name already exists (returned as APIError).
//   - 5xx etc. flow through the standard doRequest handling.
func (c *Client) CreateBranch(ctx context.Context, owner, repo, newBranchName, fromRef string) (*CreateBranchResponse, error) {
	if owner == "" || repo == "" || newBranchName == "" {
		return nil, fmt.Errorf("forgejo: CreateBranch requires owner, repo, and newBranchName")
	}

	path := fmt.Sprintf("/api/v1/repos/%s/%s/branches",
		url.PathEscape(owner), url.PathEscape(repo))

	req := CreateBranchRequest{
		NewBranchRef: newBranchName,
		OldRef:       fromRef,
	}

	var branch CreateBranchResponse
	if err := c.doRequest(ctx, http.MethodPost, path, req, &branch); err != nil {
		return nil, err
	}

	// Some Forgejo versions omit commit_sha; backfill from ref if so.
	if branch.CommitSHA == "" && branch.Ref != "" {
		branch.CommitSHA = branch.Ref
	}

	return &branch, nil
}

// BranchRef returns the canonical refs/heads/<name> for a local branch name.
// Convenience helper for code that needs to compare refs without string
// formatting in the dispatcher.
func BranchRef(name string) string {
	return "refs/heads/" + name
}
