// Package forgejo — pull_request.go
//
// PR primitives used by the dispatcher Ralph Loop.
// Mirrors the Forgejo REST API for creating a pull request from an
// existing feature branch back into a base branch.
//
// Endpoints:
//
//	POST /api/v1/repos/{owner}/{repo}/pulls
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

// CreatePRRequest is the body for POST /api/v1/repos/{owner}/{repo}/pulls.
// Forgejo returns a 422 (Unprocessable) when the head==base or when either
// branch doesn't exist — those are expected validation errors that the
// dispatcher surfaces distinctly from 5xx transient failures.
type CreatePRRequest struct {
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	Head      string  `json:"head"` // local branch name on the same repo (e.g. "feature/wi-001")
	Base      string  `json:"base"` // local branch name (default branch for the repo, e.g. "main")
	Assignee  string  `json:"assignee,omitempty"`
	Milestone int64   `json:"milestone,omitempty"`
	Labels    []int64 `json:"labels,omitempty"`
}

// CreatePRResponse is the parsed PR payload returned by Forgejo. We
// surface the dispatcher-critical fields (Number, HTMLURL, Head, Base,
// State) and drop the rest onto the raw JSON.
type CreatePRResponse struct {
	ID        int64  `json:"id"`
	Number    int64  `json:"number"`
	State     string `json:"state"` // "open" | "closed" | "merged"
	Title     string `json:"title"`
	Body      string `json:"body"`
	HeadRef   string `json:"head_ref"` // local branch name
	BaseRef   string `json:"base_ref"`
	URL       string `json:"url"`      // API URL
	HTMLURL   string `json:"html_url"` // Browser URL — this is what the CLI returns to the user
	CreatedAt string `json:"created_at"`
}

// ---------------------------------------------------------------------------
// CreatePR
// ---------------------------------------------------------------------------

// CreatePR opens a new pull request from head → base on the given repo.
//
//   - owner / repo identify the target repo.
//   - head is the new feature branch (local branch name).
//   - base is the target branch on the same repo (e.g. "main" or "master").
//   - title and body become the PR title and description.
//
// Returns the parsed PR payload on success, including HTMLURL for the
// CLI to print. Errors:
//   - 422 if head==base or one of the branches doesn't exist (APIError).
//   - 409 if the PR already exists in open state (APIError).
//   - 5xx flow through standard doRequest handling.
func (c *Client) CreatePR(ctx context.Context, owner, repo, head, base, title, body string) (*CreatePRResponse, error) {
	if owner == "" || repo == "" || head == "" || base == "" {
		return nil, fmt.Errorf("forgejo: CreatePR requires owner, repo, head, and base")
	}

	path := fmt.Sprintf("/api/v1/repos/%s/%s/pulls",
		url.PathEscape(owner), url.PathEscape(repo))

	req := CreatePRRequest{
		Title: title,
		Body:  body,
		Head:  head,
		Base:  base,
	}

	var pr CreatePRResponse
	if err := c.doRequest(ctx, http.MethodPost, path, req, &pr); err != nil {
		return nil, err
	}

	if pr.State == "" {
		pr.State = "open"
	}

	return &pr, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// IsAlreadyExists reports whether an APIError represents a "branch/PR
// already exists" condition (HTTP 409 Conflict or 422 with conflict-like
// message). The dispatcher uses this to make CreateBranch / CreatePR
// idempotent: when run twice against the same state, the second call
// returns a result instead of failing the lock commit.
//
// The check is intentionally strict — only HTTP 409 maps cleanly;
// HTTP 422 covers a broad range of validation errors that we DON'T want
// to treat as "already exists" since the dispatcher should surface them.
func IsAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		return false
	}
	return apiErr.StatusCode == http.StatusConflict
}
