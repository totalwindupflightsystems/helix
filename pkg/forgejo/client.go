// Package forgejo provides a Go client wrapper for the Forgejo REST API.
// Used by helix-identity, helix-negotiate, and helix-marketplace.
//
// Based on specs/cross-component-wiring.md §2 and specs/agent-identity.md.
package forgejo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

// Client wraps the Forgejo REST API with BasicAuth and circuit-breaker support.
type Client struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
	circuit    CircuitBreaker
}

// CircuitBreaker is the minimal interface the client needs.
// This allows using integration.CircuitBreaker or a custom implementation.
type CircuitBreaker interface {
	Allow() bool
	RecordSuccess()
	RecordFailure()
}

// NoopCircuitBreaker is a circuit breaker that always allows calls.
type NoopCircuitBreaker struct{}

func (NoopCircuitBreaker) Allow() bool      { return true }
func (NoopCircuitBreaker) RecordSuccess()   {}
func (NoopCircuitBreaker) RecordFailure()   {}

// NewClient creates a Forgejo API client.
func NewClient(baseURL, username, password string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		username:   username,
		password:   password,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		circuit:    NoopCircuitBreaker{},
	}
}

// WithHTTPClient sets a custom HTTP client.
func (c *Client) WithHTTPClient(client *http.Client) *Client {
	c.httpClient = client
	return c
}

// WithCircuitBreaker attaches a circuit breaker.
func (c *Client) WithCircuitBreaker(cb CircuitBreaker) *Client {
	c.circuit = cb
	return c
}

// ---------------------------------------------------------------------------
// API types
// ---------------------------------------------------------------------------

// User represents a Forgejo user.
type User struct {
	ID       int64  `json:"id"`
	UserName string `json:"login"`
	Email    string `json:"email"`
	FullName string `json:"full_name"`
	IsActive bool   `json:"active"`
	IsAdmin  bool   `json:"is_admin"`
}

// CreateUserRequest is the body for POST /admin/users.
type CreateUserRequest struct {
	UserName string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	FullName string `json:"full_name,omitempty"`
	SourceID  int64 `json:"source_id,omitempty"`
}

// SSHKey represents a public SSH key registered with Forgejo.
type SSHKey struct {
	ID          int64  `json:"id"`
	Key         string `json:"key"`
	Title       string `json:"title"`
	Fingerprint string `json:"fingerprint"`
	URL         string `json:"url,omitempty"`
}

// PAT (Personal Access Token) represents an API token.
type PAT struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	SHA1 string `json:"sha1"`
}

// PRReview represents a review on a pull request.
type PRReview struct {
	ID       int64  `json:"id"`
	User     User   `json:"user"`
	Body     string `json:"body"`
	State    string `json:"state"` // "APPROVED", "REQUEST_CHANGES", "COMMENT"
}

// PullRequest represents a Forgejo pull request.
type PullRequest struct {
	ID     int64  `json:"id"`
	Number int64  `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
	Head   Branch `json:"head"`
	Base   Branch `json:"base"`
}

// Branch represents a git branch in a PR.
type Branch struct {
	Ref  string `json:"ref"`
	Name string `json:"name"`
}

// CreatePRReviewRequest is the body for posting a review.
type CreatePRReviewRequest struct {
	Body  string `json:"body"`
	Event string `json:"event"` // "APPROVED", "REQUEST_CHANGES", "COMMENT"
}

// ---------------------------------------------------------------------------
// Error types
// ---------------------------------------------------------------------------

// APIError is returned for non-2xx Forgejo API responses.
type APIError struct {
	StatusCode int
	Message    string
	URL        string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("forgejo API error: %d %s (url: %s)", e.StatusCode, e.Message, e.URL)
}

// ErrCircuitOpen is returned when the circuit breaker is open.
var ErrCircuitOpen = fmt.Errorf("circuit breaker open — service unavailable")

// ---------------------------------------------------------------------------
// Core HTTP methods
// ---------------------------------------------------------------------------

// doRequest executes an authenticated HTTP request and decodes the JSON response.
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	if !c.circuit.Allow() {
		return ErrCircuitOpen
	}

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	reqURL := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.SetBasicAuth(c.username, c.password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.circuit.RecordFailure()
		return fmt.Errorf("request to %s: %w", reqURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		c.circuit.RecordFailure()
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    strings.TrimSpace(string(respBody)),
			URL:        reqURL,
		}
	}

	c.circuit.RecordSuccess()

	if result != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// User operations
// ---------------------------------------------------------------------------

// GetUser retrieves a user by username.
func (c *Client) GetUser(ctx context.Context, username string) (*User, error) {
	var user User
	if err := c.doRequest(ctx, http.MethodGet, "/api/v1/users/"+url.PathEscape(username), nil, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// CreateUser creates a new user. Requires admin privileges.
func (c *Client) CreateUser(ctx context.Context, req CreateUserRequest) (*User, error) {
	var user User
	if err := c.doRequest(ctx, http.MethodPost, "/api/v1/admin/users", req, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// DeleteUser removes a user. Requires admin privileges.
func (c *Client) DeleteUser(ctx context.Context, username string) error {
	return c.doRequest(ctx, http.MethodDelete, "/api/v1/admin/users/"+url.PathEscape(username), nil, nil)
}

// ---------------------------------------------------------------------------
// SSH Key operations
// ---------------------------------------------------------------------------

// CreateSSHKey registers a public SSH key for the authenticated user.
func (c *Client) CreateSSHKey(ctx context.Context, title, publicKey string) (*SSHKey, error) {
	body := map[string]string{
		"title": title,
		"key":   publicKey,
	}
	var key SSHKey
	if err := c.doRequest(ctx, http.MethodPost, "/api/v1/user/keys", body, &key); err != nil {
		return nil, err
	}
	return &key, nil
}

// ListSSHKeys lists SSH keys for the authenticated user.
func (c *Client) ListSSHKeys(ctx context.Context) ([]SSHKey, error) {
	var keys []SSHKey
	if err := c.doRequest(ctx, http.MethodGet, "/api/v1/user/keys", nil, &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

// ---------------------------------------------------------------------------
// PAT (Personal Access Token) operations
// ---------------------------------------------------------------------------

// CreatePAT creates a personal access token for a user. Requires admin privileges.
func (c *Client) CreatePAT(ctx context.Context, username, tokenName string) (*PAT, error) {
	body := map[string]string{
		"name": tokenName,
	}
	var pat PAT
	if err := c.doRequest(ctx, http.MethodPost,
		fmt.Sprintf("/api/v1/users/%s/tokens", url.PathEscape(username)), body, &pat); err != nil {
		return nil, err
	}
	return &pat, nil
}

// ---------------------------------------------------------------------------
// Pull Request operations
// ---------------------------------------------------------------------------

// ListPRs lists pull requests for a repository.
func (c *Client) ListPRs(ctx context.Context, owner, repo, state string) ([]PullRequest, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/pulls?state=%s",
		url.PathEscape(owner), url.PathEscape(repo), url.QueryEscape(state))
	var prs []PullRequest
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &prs); err != nil {
		return nil, err
	}
	return prs, nil
}

// GetPRReviews retrieves all reviews for a specific pull request.
func (c *Client) GetPRReviews(ctx context.Context, owner, repo string, prNumber int64) ([]PRReview, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%d/reviews",
		url.PathEscape(owner), url.PathEscape(repo), prNumber)
	var reviews []PRReview
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &reviews); err != nil {
		return nil, err
	}
	return reviews, nil
}

// CreatePRReview posts a review comment on a pull request.
func (c *Client) CreatePRReview(ctx context.Context, owner, repo string, prNumber int64, req CreatePRReviewRequest) error {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%d/reviews",
		url.PathEscape(owner), url.PathEscape(repo), prNumber)
	return c.doRequest(ctx, http.MethodPost, path, req, nil)
}
