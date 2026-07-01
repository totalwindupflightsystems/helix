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
// GitReins HTTP Client — concrete implementation of GitReinsAdapter
//
// Per spec cross-component-wiring.md §1:
// "GitReins is the quality gate. Every commit passes through Tier 1 guards
// (secrets, lint, tests, build) and Tier 2 agentic LLM evaluation."
//
// This client wraps GitReins' REST API into Go calls implementing the
// GitReinsAdapter interface. The Cost() method computes dollar cost from
// LLMUsage using a pricing table (same approach as pkg/estimate).
// ---------------------------------------------------------------------------

// Pricing constants for common providers (per 1M tokens, USD).
// Used by Cost() to compute CostBreakdown from LLMUsage.
var (
	gitreinsDefaultPricing = providerPricing{
		inputPerMillion:      1.00,
		outputPerMillion:     2.00,
		cacheReadPerMillion:  0.10,
		cacheWritePerMillion: 1.25,
	}
)

type providerPricing struct {
	inputPerMillion      float64
	outputPerMillion     float64
	cacheReadPerMillion  float64
	cacheWritePerMillion float64
}

// Sentinel errors for GitReins API failures.
var (
	ErrGitReinsAuthFailed   = fmt.Errorf("gitreins: authentication failed (401)")
	ErrGitReinsRateLimited  = fmt.Errorf("gitreins: rate limited (429)")
	ErrGitReinsServerDown   = fmt.Errorf("gitreins: server unreachable")
	ErrGitReinsEvalTimeout  = fmt.Errorf("gitreins: evaluation timed out")
	ErrGitReinsInvalidGuard = fmt.Errorf("gitreins: invalid guard response")
)

// GitReinsAdapterClient is the HTTP implementation of GitReinsAdapter.
// (Named AdapterClient to avoid collision with the GitReinsAdapter interface.)
type GitReinsAdapterClient struct {
	baseURL    string
	apiKey     string // API key for auth
	httpClient *http.Client
	pricing    providerPricing
}

// GitReinsClientOption configures a GitReinsAdapterClient.
type GitReinsClientOption func(*GitReinsAdapterClient)

// WithGitReinsHTTPClient sets a custom HTTP client (for testing).
func WithGitReinsHTTPClient(c *http.Client) GitReinsClientOption {
	return func(gc *GitReinsAdapterClient) { gc.httpClient = c }
}

// WithGitReinsTimeout sets the HTTP timeout.
func WithGitReinsTimeout(d time.Duration) GitReinsClientOption {
	return func(gc *GitReinsAdapterClient) { gc.httpClient.Timeout = d }
}

// WithGitReinsPricing sets custom per-1M-token pricing for Cost() calculations.
func WithGitReinsPricing(input, output, cacheRead, cacheWrite float64) GitReinsClientOption {
	return func(gc *GitReinsAdapterClient) {
		gc.pricing = providerPricing{
			inputPerMillion:      input,
			outputPerMillion:     output,
			cacheReadPerMillion:  cacheRead,
			cacheWritePerMillion: cacheWrite,
		}
	}
}

// NewGitReinsAdapterClient creates a new GitReins HTTP client.
// baseURL is the GitReins API root (e.g., http://localhost:8080).
// apiKey is the optional API key for authentication.
func NewGitReinsAdapterClient(baseURL, apiKey string, opts ...GitReinsClientOption) *GitReinsAdapterClient {
	c := &GitReinsAdapterClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 120 * time.Second, // evaluations can be slow
		},
		pricing: gitreinsDefaultPricing,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Guard runs Tier 1 checks against staged changes.
func (c *GitReinsAdapterClient) Guard(workdir string, opts GuardOpts) (*GuardResult, error) {
	body := map[string]interface{}{
		"workdir":      workdir,
		"skip_secrets": opts.SkipSecrets,
		"skip_lint":    opts.SkipLint,
		"skip_tests":   opts.SkipTests,
		"skip_build":   opts.SkipBuild,
	}
	if opts.Timeout > 0 {
		body["timeout"] = opts.Timeout
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal guard request: %w", err)
	}

	url := c.baseURL + "/api/v1/guard"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGitReinsServerDown, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrGitReinsAuthFailed
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, ErrGitReinsRateLimited
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gitreins returned %d: %s", resp.StatusCode, string(respBody))
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("%w: decode error: %v", ErrGitReinsInvalidGuard, err)
	}

	return parseGuardResult(raw), nil
}

// Evaluate runs Tier 2 agentic evaluation against the diff.
func (c *GitReinsAdapterClient) Evaluate(workdir string, diff string, opts EvalOpts) (*EvalResult, error) {
	body := map[string]interface{}{
		"workdir":  workdir,
		"diff":     diff,
		"criteria": opts.Criteria,
	}
	if opts.MaxIterations > 0 {
		body["max_iterations"] = opts.MaxIterations
	}
	if opts.MaxTime != "" {
		body["max_time"] = opts.MaxTime
	}
	if opts.MaxInputTokens != "" {
		body["max_input_tokens"] = opts.MaxInputTokens
	}
	if opts.MaxOutputTokens != "" {
		body["max_output_tokens"] = opts.MaxOutputTokens
	}
	if opts.ToolCallWeight > 0 {
		body["tool_call_weight"] = opts.ToolCallWeight
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal eval request: %w", err)
	}

	url := c.baseURL + "/api/v1/evaluate"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	// Use a longer context timeout for evaluation since it can take minutes
	ctx, cancel := context.WithTimeout(context.Background(), c.httpClient.Timeout)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGitReinsServerDown, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrGitReinsAuthFailed
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, ErrGitReinsRateLimited
	}
	if resp.StatusCode == http.StatusGatewayTimeout {
		return nil, ErrGitReinsEvalTimeout
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gitreins returned %d: %s", resp.StatusCode, string(respBody))
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode eval response: %w", err)
	}

	return parseEvalResult(raw), nil
}

// Cost computes a CostBreakdown from the LLMUsage in an EvalResult.
// Uses the client's pricing table (overridable via WithGitReinsPricing).
func (c *GitReinsAdapterClient) Cost(evalResult *EvalResult) CostBreakdown {
	if evalResult == nil || evalResult.Usage.TotalTokens == 0 {
		return CostBreakdown{}
	}
	p := c.pricing
	usage := evalResult.Usage

	fresh := float64(usage.PromptTokens-usage.CacheReadTokens) / 1e6 * p.inputPerMillion
	cacheHit := float64(usage.CacheReadTokens) / 1e6 * p.cacheReadPerMillion
	cacheWrite := float64(usage.CacheWriteTokens) / 1e6 * p.cacheWritePerMillion
	output := float64(usage.CompletionTokens) / 1e6 * p.outputPerMillion

	return CostBreakdown{
		FreshInputCost: fresh,
		CacheHitCost:   cacheHit,
		CacheWriteCost: cacheWrite,
		OutputCost:     output,
		TotalCost:      fresh + cacheHit + cacheWrite + output,
	}
}

// Health checks the GitReins service health endpoint.
func (c *GitReinsAdapterClient) Health() error {
	url := c.baseURL + "/api/v1/health"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrGitReinsServerDown, err)
	}
	resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gitreins health returned %d", resp.StatusCode)
	}
	return nil
}

// parseGuardResult converts a raw JSON map into a typed GuardResult.
func parseGuardResult(raw map[string]interface{}) *GuardResult {
	result := &GuardResult{
		Checks: make(map[string]CheckResult),
	}
	if v, ok := raw["passed"].(bool); ok {
		result.Passed = v
	}
	if v, ok := raw["duration"].(float64); ok {
		result.Duration = v
	}
	if checks, ok := raw["checks"].(map[string]interface{}); ok {
		for name, cv := range checks {
			if cm, ok := cv.(map[string]interface{}); ok {
				cr := CheckResult{}
				if v, ok := cm["passed"].(bool); ok {
					cr.Passed = v
				}
				if v, ok := cm["output"].(string); ok {
					cr.Output = v
				}
				if v, ok := cm["duration"].(float64); ok {
					cr.Duration = v
				}
				result.Checks[name] = cr
			}
		}
	}
	return result
}

// parseEvalResult converts a raw JSON map into a typed EvalResult.
func parseEvalResult(raw map[string]interface{}) *EvalResult {
	result := &EvalResult{
		Verdicts: make(map[string]Verdict),
	}
	if v, ok := raw["passed"].(bool); ok {
		result.Passed = v
	}
	if v, ok := raw["duration"].(float64); ok {
		result.Duration = v
	}
	if verdicts, ok := raw["verdicts"].(map[string]interface{}); ok {
		for criterion, vv := range verdicts {
			if vm, ok := vv.(map[string]interface{}); ok {
				v := Verdict{}
				if s, ok := vm["status"].(string); ok {
					v.Status = s
				}
				if s, ok := vm["reason"].(string); ok {
					v.Reason = s
				}
				if f, ok := vm["score"].(float64); ok {
					v.Score = f
				}
				result.Verdicts[criterion] = v
			}
		}
	}
	if evidence, ok := raw["evidence"].([]interface{}); ok {
		for _, ei := range evidence {
			if em, ok := ei.(map[string]interface{}); ok {
				e := Evidence{}
				if s, ok := em["type"].(string); ok {
					e.Type = s
				}
				if s, ok := em["content"].(string); ok {
					e.Content = s
				}
				if s, ok := em["source"].(string); ok {
					e.Source = s
				}
				result.Evidence = append(result.Evidence, e)
			}
		}
	}
	if usage, ok := raw["usage"].(map[string]interface{}); ok {
		if v, ok := usage["prompt_tokens"].(float64); ok {
			result.Usage.PromptTokens = int(v)
		}
		if v, ok := usage["completion_tokens"].(float64); ok {
			result.Usage.CompletionTokens = int(v)
		}
		if v, ok := usage["total_tokens"].(float64); ok {
			result.Usage.TotalTokens = int(v)
		}
		if v, ok := usage["cache_read_tokens"].(float64); ok {
			result.Usage.CacheReadTokens = int(v)
		}
		if v, ok := usage["cache_write_tokens"].(float64); ok {
			result.Usage.CacheWriteTokens = int(v)
		}
	}
	return result
}
