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
// Chimera HTTP Client — concrete implementation of ChimeraAdapter
//
// Per spec cross-component-wiring.md §3:
// "Chimera is the multi-model PR review engine. Every PR open triggers a
// deliberation. Tie-breaks in negotiation go to Chimera arbiter formation."
//
// This client wraps Chimera's REST API into Go calls implementing the
// ChimeraAdapter interface.
// ---------------------------------------------------------------------------

// Sentinel errors for Chimera API failures.
var (
	ErrChimeraAuthFailed    = fmt.Errorf("chimera: authentication failed (401)")
	ErrChimeraRateLimited   = fmt.Errorf("chimera: rate limited (429)")
	ErrChimeraServerDown    = fmt.Errorf("chimera: server unreachable")
	ErrChimeraBudgetExceed  = fmt.Errorf("chimera: deliberation budget exceeded")
	ErrChimeraInvalidResult = fmt.Errorf("chimera: invalid deliberation result")
)

// ChimeraAdapterClient is the HTTP implementation of ChimeraAdapter.
// (Named AdapterClient to avoid collision with the test ChimeraClient in suite.go.)
type ChimeraAdapterClient struct {
	baseURL    string
	token      string // Bearer token for auth
	httpClient *http.Client
}

// ChimeraClientOption configures a ChimeraAdapterClient.
type ChimeraClientOption func(*ChimeraAdapterClient)

// WithChimeraHTTPClient sets a custom HTTP client (for testing).
func WithChimeraHTTPClient(c *http.Client) ChimeraClientOption {
	return func(chc *ChimeraAdapterClient) { chc.httpClient = c }
}

// WithChimeraTimeout sets the HTTP timeout.
func WithChimeraTimeout(d time.Duration) ChimeraClientOption {
	return func(chc *ChimeraAdapterClient) { chc.httpClient.Timeout = d }
}

// NewChimeraAdapterClient creates a new Chimera HTTP client.
// baseURL is the Chimera API root (e.g., http://localhost:8765).
// token is the optional Bearer token for authentication.
func NewChimeraAdapterClient(baseURL, token string, opts ...ChimeraClientOption) *ChimeraAdapterClient {
	c := &ChimeraAdapterClient{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Review dispatches a full multi-model deliberation on a PR.
func (c *ChimeraAdapterClient) Review(pr ChimeraPR, opts ReviewOpts) (*ChimeraVerdict, error) {
	body := map[string]interface{}{
		"repo_owner":    pr.RepoOwner,
		"repo_name":     pr.RepoName,
		"pr_number":     pr.PRNumber,
		"title":         pr.Title,
		"description":   pr.Description,
		"diff":          pr.Diff,
		"spec_files":    pr.SpecFiles,
		"agent_reviews": serializeAgentReviews(pr.AgentReviews),
	}
	if opts.Formation != "" {
		body["formation"] = opts.Formation
	}
	if opts.MaxBudget > 0 {
		body["max_budget"] = opts.MaxBudget
	}
	if len(opts.StageModels) > 0 {
		body["stage_models"] = opts.StageModels
	}
	body["allow_custom_dag"] = opts.AllowCustomDAG

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal review request: %w", err)
	}

	url := c.baseURL + "/api/v1/deliberate"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrChimeraServerDown, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrChimeraAuthFailed
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, ErrChimeraRateLimited
	}
	if resp.StatusCode == http.StatusPaymentRequired {
		return nil, ErrChimeraBudgetExceed
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("chimera returned %d: %s", resp.StatusCode, string(respBody))
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("%w: decode error: %v", ErrChimeraInvalidResult, err)
	}

	return parseChimeraVerdict(raw), nil
}

// Formations returns available deliberation formations.
func (c *ChimeraAdapterClient) Formations() ([]Formation, error) {
	url := c.baseURL + "/api/v1/formations"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrChimeraServerDown, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrChimeraAuthFailed
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, ErrChimeraRateLimited
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("chimera returned %d", resp.StatusCode)
	}

	var rawList []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rawList); err != nil {
		return nil, fmt.Errorf("decode formations: %w", err)
	}

	formations := make([]Formation, 0, len(rawList))
	for _, raw := range rawList {
		f := Formation{}
		if v, ok := raw["name"].(string); ok {
			f.Name = v
		}
		if v, ok := raw["description"].(string); ok {
			f.Description = v
		}
		if v, ok := raw["stages"].(float64); ok {
			f.Stages = int(v)
		}
		formations = append(formations, f)
	}
	return formations, nil
}

// Models returns available models with category weights.
func (c *ChimeraAdapterClient) Models() ([]ChimeraModel, error) {
	url := c.baseURL + "/api/v1/models"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrChimeraServerDown, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrChimeraAuthFailed
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, ErrChimeraRateLimited
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("chimera returned %d", resp.StatusCode)
	}

	var rawList []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rawList); err != nil {
		return nil, fmt.Errorf("decode models: %w", err)
	}

	models := make([]ChimeraModel, 0, len(rawList))
	for _, raw := range rawList {
		m := ChimeraModel{}
		if v, ok := raw["name"].(string); ok {
			m.Name = v
		}
		if v, ok := raw["category"].(string); ok {
			m.Category = v
		}
		if v, ok := raw["weight"].(float64); ok {
			m.Weight = v
		}
		models = append(models, m)
	}
	return models, nil
}

// Health checks the Chimera service health endpoint.
func (c *ChimeraAdapterClient) Health() (*ChimeraHealth, error) {
	url := c.baseURL + "/api/v1/health"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrChimeraServerDown, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &ChimeraHealth{Status: "down"}, nil
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return &ChimeraHealth{Status: "healthy"}, nil
	}

	h := &ChimeraHealth{Status: "healthy"}
	if v, ok := raw["status"].(string); ok {
		h.Status = v
	}
	if v, ok := raw["version"].(string); ok {
		h.Version = v
	}
	if v, ok := raw["uptime"].(float64); ok {
		h.Uptime = v
	}
	if v, ok := raw["models"].(float64); ok {
		h.Models = int(v)
	}
	return h, nil
}

// serializeAgentReviews converts []AgentReview to []map[string]interface{} for JSON.
func serializeAgentReviews(reviews []AgentReview) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(reviews))
	for _, r := range reviews {
		result = append(result, map[string]interface{}{
			"agent_name":  r.AgentName,
			"verdict":     r.Verdict,
			"body":        r.Body,
			"evidence":    r.Evidence,
			"trust_level": r.TrustLevel,
		})
	}
	return result
}

// parseChimeraVerdict converts a raw JSON map into a typed ChimeraVerdict.
func parseChimeraVerdict(raw map[string]interface{}) *ChimeraVerdict {
	v := &ChimeraVerdict{}
	if s, ok := raw["status"].(string); ok {
		v.Status = s
	}
	if f, ok := raw["confidence"].(float64); ok {
		v.Confidence = f
	}
	if s, ok := raw["summary"].(string); ok {
		v.Summary = s
	}
	if f, ok := raw["cost"].(float64); ok {
		v.Cost = f
	}
	if findings, ok := raw["findings"].([]interface{}); ok {
		for _, fi := range findings {
			if fm, ok := fi.(map[string]interface{}); ok {
				f := Finding{}
				if s, ok := fm["severity"].(string); ok {
					f.Severity = s
				}
				if s, ok := fm["category"].(string); ok {
					f.Category = s
				}
				if s, ok := fm["file"].(string); ok {
					f.File = s
				}
				if n, ok := fm["line"].(float64); ok {
					f.Line = int(n)
				}
				if s, ok := fm["description"].(string); ok {
					f.Description = s
				}
				if s, ok := fm["suggestion"].(string); ok {
					f.Suggestion = s
				}
				v.Findings = append(v.Findings, f)
			}
		}
	}
	if trace, ok := raw["trace"].(map[string]interface{}); ok {
		if s, ok := trace["source"].(string); ok {
			v.Trace.Source = s
		}
		if f, ok := trace["duration"].(float64); ok {
			v.Trace.Duration = f
		}
		if n, ok := trace["total_tokens"].(float64); ok {
			v.Trace.TotalTokens = int(n)
		}
		if stages, ok := trace["stages"].([]interface{}); ok {
			for _, si := range stages {
				if sm, ok := si.(map[string]interface{}); ok {
					s := StageResult{}
					if v, ok := sm["stage"].(string); ok {
						s.Stage = v
					}
					if v, ok := sm["model"].(string); ok {
						s.Model = v
					}
					if v, ok := sm["output"].(string); ok {
						s.Output = v
					}
					if v, ok := sm["tokens"].(float64); ok {
						s.Tokens = int(v)
					}
					if v, ok := sm["duration"].(float64); ok {
						s.Duration = v
					}
					v.Trace.Stages = append(v.Trace.Stages, s)
				}
			}
		}
	}
	return v
}
