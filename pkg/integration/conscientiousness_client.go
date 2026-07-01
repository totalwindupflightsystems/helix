package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ---------------------------------------------------------------------------
// Conscientiousness HTTP Client — concrete implementation of
// ConscientiousnessAdapter
//
// Per spec integrations.md §3:
// "Agentic self-evaluation. Runs after Chimera review. Asks 'are you sure?'"
//
// This client implements the adapter interface with real HTTP calls to the
// Conscientiousness service.
// ---------------------------------------------------------------------------

// ConscientiousnessClient is the HTTP implementation of ConscientiousnessAdapter.
type ConscientiousnessClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// ConscientiousnessClientOption configures a ConscientiousnessClient.
type ConscientiousnessClientOption func(*ConscientiousnessClient)

// WithConscientiousnessHTTPClient sets a custom HTTP client (for testing).
func WithConscientiousnessHTTPClient(c *http.Client) ConscientiousnessClientOption {
	return func(cc *ConscientiousnessClient) { cc.httpClient = c }
}

// WithConscientiousnessTimeout sets the HTTP timeout.
func WithConscientiousnessTimeout(d time.Duration) ConscientiousnessClientOption {
	return func(cc *ConscientiousnessClient) { cc.httpClient.Timeout = d }
}

// NewConscientiousnessClient creates a new Conscientiousness HTTP client.
// baseURL is the service root (e.g., http://conscientiousness:8080).
func NewConscientiousnessClient(baseURL, apiKey string, opts ...ConscientiousnessClientOption) *ConscientiousnessClient {
	c := &ConscientiousnessClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 180 * time.Second, // spec §3.2 default timeout
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Evaluate runs adversarial self-evaluation on a completed PR.
// POSTs to /api/v1/evaluate with the PR context.
func (c *ConscientiousnessClient) Evaluate(pr ConscientiousnessPR, opts EvalOpts) (*ConscientiousnessVerdict, error) {
	payload, err := c.serializeEvaluateRequest(pr, opts)
	if err != nil {
		return nil, err
	}

	url := c.baseURL + "/api/v1/evaluate"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.httpClient.Timeout)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("conscientiousness evaluate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("conscientiousness evaluate: auth failed (401)")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("conscientiousness evaluate: rate limited (429)")
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("conscientiousness evaluate: server error (%d)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("conscientiousness evaluate: unexpected status %d", resp.StatusCode)
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("conscientiousness evaluate: decode response: %w", err)
	}

	return parseConscientiousnessVerdict(raw), nil
}

// serializeEvaluateRequest converts the PR and opts to a JSON payload.
func (c *ConscientiousnessClient) serializeEvaluateRequest(pr ConscientiousnessPR, opts EvalOpts) ([]byte, error) {
	req := map[string]interface{}{
		"repo_owner": pr.RepoOwner,
		"repo_name":  pr.RepoName,
		"pr_number":  pr.PRNumber,
		"diff":       pr.Diff,
	}

	// Include Chimera verdict if available
	if pr.ChimeraVerdict != nil {
		req["chimera_verdict"] = map[string]interface{}{
			"status":     pr.ChimeraVerdict.Status,
			"confidence": pr.ChimeraVerdict.Confidence,
		}
	}

	// Include GitReins eval if available
	if pr.GitReinsEval != nil {
		req["gitreins_eval"] = map[string]interface{}{
			"passed":   pr.GitReinsEval.Passed,
			"duration": pr.GitReinsEval.Duration,
		}
	}

	if pr.EvidenceBundle != "" {
		req["evidence_bundle"] = pr.EvidenceBundle
	}

	// Serialize acceptance criteria
	if len(pr.ACs) > 0 {
		acs := make([]map[string]string, 0, len(pr.ACs))
		for _, ac := range pr.ACs {
			acs = append(acs, map[string]string{
				"id":     ac.ID,
				"text":   ac.Text,
				"status": ac.Status,
			})
		}
		req["acs"] = acs
	}

	// Include eval options
	req["max_iterations"] = opts.MaxIterations

	return json.Marshal(req)
}

// Health checks the Conscientiousness service health endpoint.
func (c *ConscientiousnessClient) Health() (*ConscientiousnessHealth, error) {
	url := c.baseURL + "/health"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("conscientiousness health check: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &ConscientiousnessHealth{Status: "down"}, nil
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return &ConscientiousnessHealth{Status: "healthy"}, nil
	}

	h := &ConscientiousnessHealth{Status: "healthy"}
	if v, ok := raw["status"].(string); ok {
		h.Status = v
	}
	if v, ok := raw["version"].(string); ok {
		h.Version = v
	}
	if v, ok := raw["uptime"].(float64); ok {
		h.Uptime = v
	}
	return h, nil
}

// parseConscientiousnessVerdict converts a raw map into a typed verdict.
func parseConscientiousnessVerdict(raw map[string]interface{}) *ConscientiousnessVerdict {
	v := &ConscientiousnessVerdict{}

	if s, ok := raw["status"].(string); ok {
		v.Status = s
	}
	if c, ok := raw["confidence"].(float64); ok {
		v.Confidence = c
	}
	if cost, ok := raw["cost"].(float64); ok {
		v.Cost = cost
	}

	if av, ok := raw["attack_vectors"].([]interface{}); ok {
		for _, item := range av {
			if m, ok := item.(map[string]interface{}); ok {
				av2 := AttackVector{}
				if d, ok := m["description"].(string); ok {
					av2.Description = d
				}
				if s, ok := m["severity"].(string); ok {
					av2.Severity = s
				}
				if e, ok := m["exploitability"].(string); ok {
					av2.Exploitability = e
				}
				v.AttackVectors = append(v.AttackVectors, av2)
			}
		}
	}

	if ms, ok := raw["mitigations"].([]interface{}); ok {
		for _, item := range ms {
			if m, ok := item.(map[string]interface{}); ok {
				mit := Mitigation{}
				if av, ok := m["attack_vector"].(string); ok {
					mit.AttackVector = av
				}
				if mt, ok := m["mitigation"].(string); ok {
					mit.Mitigation = mt
				}
				if sf, ok := m["sufficient"].(bool); ok {
					mit.Sufficient = sf
				}
				v.Mitigations = append(v.Mitigations, mit)
			}
		}
	}

	return v
}
