package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ---------------------------------------------------------------------------
// Axiom HTTP Client — concrete implementation of AxiomAdapter
//
// Per spec integrations.md §6:
// "Axiom is the agent fleet management system. Decomposes human intent →
// spec extraction → meta-plan → work items → build loop → verification →
// adversarial review → PR. 60+ adversarial/quality agents with specs-as-
// contracts and evidence bundles."
//
// This client wraps Axiom's REST API into Go calls implementing the
// AxiomAdapter interface.
// ---------------------------------------------------------------------------

// Sentinel errors for Axiom API failures.
var (
	ErrAxiomAuthFailed    = fmt.Errorf("axiom: authentication failed (401)")
	ErrAxiomRateLimited   = fmt.Errorf("axiom: rate limited (429)")
	ErrAxiomServerDown    = fmt.Errorf("axiom: server unreachable")
	ErrAxiomInvalidResult = fmt.Errorf("axiom: invalid response")
)

// AxiomClient is the HTTP implementation of AxiomAdapter.
type AxiomClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// AxiomClientOption configures an AxiomClient.
type AxiomClientOption func(*AxiomClient)

// WithAxiomHTTPClient sets a custom HTTP client (for testing).
func WithAxiomHTTPClient(c *http.Client) AxiomClientOption {
	return func(ac *AxiomClient) { ac.httpClient = c }
}

// WithAxiomTimeout sets the HTTP timeout.
func WithAxiomTimeout(d time.Duration) AxiomClientOption {
	return func(ac *AxiomClient) { ac.httpClient.Timeout = d }
}

// NewAxiomClient creates a new Axiom HTTP client.
// baseURL is the Axiom API root (e.g., http://axiom:8080).
// token is the optional Bearer token for authentication.
func NewAxiomClient(baseURL, token string, opts ...AxiomClientOption) *AxiomClient {
	c := &AxiomClient{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Run executes the full Axiom pipeline on a work item.
func (c *AxiomClient) Run(intent string, repoPath string, opts RunOpts) (*AxiomResult, error) {
	body := map[string]interface{}{
		"intent":           intent,
		"repo_path":        repoPath,
		"in_process":       opts.InProcess,
		"entry_command":    opts.EntryCommand,
		"no_branch":        opts.NoBranch,
		"yes":              opts.Yes,
		"opencode_url":     opts.OpenCodeURL,
		"spin_up_opencode": opts.SpinUpOpenCode,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal run request: %w", err)
	}

	url := c.baseURL + "/api/v1/run"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAxiomServerDown, err)
	}
	defer resp.Body.Close()

	if err := checkAxiomError(resp); err != nil {
		return nil, err
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("%w: decode error: %v", ErrAxiomInvalidResult, err)
	}

	return parseAxiomResult(raw), nil
}

// Cmd executes a single Axiom command.
func (c *AxiomClient) Cmd(command string, repoPath string) (*CmdResult, error) {
	body := map[string]interface{}{
		"command":   command,
		"repo_path": repoPath,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal cmd request: %w", err)
	}

	url := c.baseURL + "/api/v1/cmd"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAxiomServerDown, err)
	}
	defer resp.Body.Close()

	if err := checkAxiomError(resp); err != nil {
		return nil, err
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("%w: decode error: %v", ErrAxiomInvalidResult, err)
	}

	r := &CmdResult{}
	if v, ok := raw["status"].(string); ok {
		r.Status = v
	}
	if v, ok := raw["output"].(string); ok {
		r.Output = v
	}
	if v, ok := raw["duration"].(float64); ok {
		r.Duration = v
	}
	return r, nil
}

// Status returns current Axiom pipeline status.
func (c *AxiomClient) Status(repoPath string) (*AxiomStatus, error) {
	url := c.baseURL + "/api/v1/status?repo=" + repoPath
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAxiomServerDown, err)
	}
	defer resp.Body.Close()

	if err := checkAxiomError(resp); err != nil {
		return nil, err
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("%w: decode error: %v", ErrAxiomInvalidResult, err)
	}

	s := &AxiomStatus{}
	if v, ok := raw["active_runs"].(float64); ok {
		s.ActiveRuns = int(v)
	}
	if v, ok := raw["queued_items"].(float64); ok {
		s.QueuedItems = int(v)
	}
	if v, ok := raw["current_phase"].(string); ok {
		s.CurrentPhase = v
	}
	if blocked, ok := raw["blocked_items"].([]interface{}); ok {
		for _, bi := range blocked {
			if s2, ok := bi.(string); ok {
				s.BlockedItems = append(s.BlockedItems, s2)
			}
		}
	}
	return s, nil
}

// ListWorkItems returns all work items for a repo.
func (c *AxiomClient) ListWorkItems(repoPath string) ([]WorkItem, error) {
	url := c.baseURL + "/api/v1/work-items?repo=" + repoPath
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAxiomServerDown, err)
	}
	defer resp.Body.Close()

	if err := checkAxiomError(resp); err != nil {
		return nil, err
	}

	var rawList []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rawList); err != nil {
		return nil, fmt.Errorf("%w: decode error: %v", ErrAxiomInvalidResult, err)
	}

	items := make([]WorkItem, 0, len(rawList))
	for _, raw := range rawList {
		items = append(items, parseWorkItem(raw))
	}
	return items, nil
}

// Health checks the Axiom service health endpoint.
func (c *AxiomClient) Health() error {
	url := c.baseURL + "/api/v1/health"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	c.setAuth(req)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAxiomServerDown, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: status %d", ErrAxiomServerDown, resp.StatusCode)
	}
	return nil
}

// setAuth adds the Bearer token header if configured.
func (c *AxiomClient) setAuth(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

// checkAxiomError inspects the HTTP response status and returns a sentinel
// error for known failure codes.
func checkAxiomError(resp *http.Response) error {
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrAxiomAuthFailed
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return ErrAxiomRateLimited
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("axiom returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// parseAxiomResult converts a raw JSON map into a typed AxiomResult.
func parseAxiomResult(raw map[string]interface{}) *AxiomResult {
	r := &AxiomResult{}
	if v, ok := raw["work_item_id"].(string); ok {
		r.WorkItemID = v
	}
	if v, ok := raw["status"].(string); ok {
		r.Status = v
	}
	if v, ok := raw["confidence"].(float64); ok {
		r.Confidence = v
	}
	if v, ok := raw["evidence"].(string); ok {
		r.Evidence = v
	}
	if v, ok := raw["pr"].(string); ok {
		r.PR = v
	}
	if v, ok := raw["cost"].(float64); ok {
		r.Cost = v
	}
	if v, ok := raw["duration"].(float64); ok {
		r.Duration = v
	}
	return r
}

// parseWorkItem converts a raw JSON map into a typed WorkItem.
func parseWorkItem(raw map[string]interface{}) WorkItem {
	wi := WorkItem{}
	if v, ok := raw["id"].(string); ok {
		wi.ID = v
	}
	if v, ok := raw["title"].(string); ok {
		wi.Title = v
	}
	if v, ok := raw["status"].(string); ok {
		wi.Status = v
	}
	if v, ok := raw["priority"].(string); ok {
		wi.Priority = v
	}
	if v, ok := raw["assignee"].(string); ok {
		wi.Assignee = v
	}
	if v, ok := raw["confidence"].(float64); ok {
		wi.Confidence = v
	}
	return wi
}
