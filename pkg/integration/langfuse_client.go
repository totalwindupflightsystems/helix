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
// LangFuse HTTP Client — concrete implementation of LangFuseAdapter
//
// Per spec cross-component-wiring.md §3.1:
// "Every Chimera deliberation → POST langfuse-helix:3000/api/public/ingestion"
//
// LangFuse ingests LLM interaction traces for cost tracking, debugging, and
// cross-component observability. This client implements the adapter interface
// with real HTTP calls to the LangFuse public API.
// ---------------------------------------------------------------------------

// LangFuseClient is the HTTP implementation of LangFuseAdapter.
type LangFuseClient struct {
	baseURL    string
	publicKey  string
	secretKey  string
	httpClient *http.Client
}

// LangFuseClientOption configures a LangFuseClient.
type LangFuseClientOption func(*LangFuseClient)

// WithLangFuseHTTPClient sets a custom HTTP client (for testing).
func WithLangFuseHTTPClient(c *http.Client) LangFuseClientOption {
	return func(lfc *LangFuseClient) { lfc.httpClient = c }
}

// WithLangFuseTimeout sets the HTTP timeout.
func WithLangFuseTimeout(d time.Duration) LangFuseClientOption {
	return func(lfc *LangFuseClient) { lfc.httpClient.Timeout = d }
}

// NewLangFuseClient creates a new LangFuse HTTP client.
// baseURL is the LangFuse API root (e.g., http://langfuse-helix:3000).
// publicKey and secretKey are the LangFuse API credentials.
func NewLangFuseClient(baseURL, publicKey, secretKey string, opts ...LangFuseClientOption) *LangFuseClient {
	c := &LangFuseClient{
		baseURL:   baseURL,
		publicKey: publicKey,
		secretKey: secretKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// IngestTrace posts a trace to the LangFuse ingestion endpoint.
func (c *LangFuseClient) IngestTrace(trace LangFuseTrace) (*LangFuseIngestResult, error) {
	body := map[string]interface{}{
		"id":       trace.ID,
		"name":     trace.Name,
		"project":  trace.Project,
		"input":    trace.Input,
		"output":   trace.Output,
		"model":    trace.Model,
		"provider": trace.Provider,
		"usage": map[string]int{
			"input":      trace.Usage.InputTokens,
			"output":     trace.Usage.OutputTokens,
			"total":      trace.Usage.TotalTokens,
			"cacheRead":  trace.Usage.CacheReadTokens,
			"cacheWrite": trace.Usage.CacheWriteTokens,
		},
		"cost":      trace.Cost,
		"metadata":  trace.Metadata,
		"timestamp": trace.Timestamp,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal trace: %w", err)
	}

	url := c.baseURL + "/api/public/ingestion"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.publicKey, c.secretKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("langfuse request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("langfuse returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		// Trace was likely accepted (2xx) but response body wasn't JSON
		return &LangFuseIngestResult{
			ID:     trace.ID,
			Status: "accepted",
		}, nil
	}
	return &LangFuseIngestResult{
		ID:     result.ID,
		Status: result.Status,
	}, nil
}

// GetTrace retrieves a trace by ID from LangFuse.
func (c *LangFuseClient) GetTrace(traceID string) (*LangFuseTrace, error) {
	url := fmt.Sprintf("%s/api/public/traces/%s", c.baseURL, traceID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.SetBasicAuth(c.publicKey, c.secretKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("langfuse request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("trace %s not found", traceID)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("langfuse returned %d", resp.StatusCode)
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return parseLangFuseTrace(raw), nil
}

// ListTraces returns traces filtered by project and optional time range.
func (c *LangFuseClient) ListTraces(project string, opts TraceFilter) ([]LangFuseTrace, error) {
	url := fmt.Sprintf("%s/api/public/traces?project=%s", c.baseURL, project)
	if opts.From != "" {
		url += "&from=" + opts.From
	}
	if opts.To != "" {
		url += "&to=" + opts.To
	}
	if opts.Limit > 0 {
		url += fmt.Sprintf("&limit=%d", opts.Limit)
	}
	if opts.Offset > 0 {
		url += fmt.Sprintf("&offset=%d", opts.Offset)
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.SetBasicAuth(c.publicKey, c.secretKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("langfuse request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("langfuse returned %d", resp.StatusCode)
	}

	var rawList []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rawList); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	traces := make([]LangFuseTrace, 0, len(rawList))
	for _, raw := range rawList {
		traces = append(traces, *parseLangFuseTrace(raw))
	}
	return traces, nil
}

// Health checks the LangFuse service health endpoint.
func (c *LangFuseClient) Health() (*LangFuseHealth, error) {
	url := c.baseURL + "/api/public/health"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.SetBasicAuth(c.publicKey, c.secretKey)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("langfuse health check: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &LangFuseHealth{Status: "down"}, nil
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return &LangFuseHealth{Status: "healthy"}, nil
	}

	h := &LangFuseHealth{Status: "healthy"}
	if v, ok := raw["version"].(string); ok {
		h.Version = v
	}
	if v, ok := raw["uptime"].(float64); ok {
		h.Uptime = v
	}
	return h, nil
}

// parseLangFuseTrace converts a raw map into a LangFuseTrace.
func parseLangFuseTrace(raw map[string]interface{}) *LangFuseTrace {
	trace := &LangFuseTrace{}
	if v, ok := raw["id"].(string); ok {
		trace.ID = v
	}
	if v, ok := raw["name"].(string); ok {
		trace.Name = v
	}
	if v, ok := raw["project"].(string); ok {
		trace.Project = v
	}
	if v, ok := raw["input"].(string); ok {
		trace.Input = v
	}
	if v, ok := raw["output"].(string); ok {
		trace.Output = v
	}
	if v, ok := raw["model"].(string); ok {
		trace.Model = v
	}
	if v, ok := raw["provider"].(string); ok {
		trace.Provider = v
	}
	if v, ok := raw["cost"].(float64); ok {
		trace.Cost = v
	}
	if v, ok := raw["timestamp"].(string); ok {
		trace.Timestamp = v
	}
	if usage, ok := raw["usage"].(map[string]interface{}); ok {
		if v, ok := usage["input"].(float64); ok {
			trace.Usage.InputTokens = int(v)
		}
		if v, ok := usage["output"].(float64); ok {
			trace.Usage.OutputTokens = int(v)
		}
		if v, ok := usage["total"].(float64); ok {
			trace.Usage.TotalTokens = int(v)
		}
		if v, ok := usage["cacheRead"].(float64); ok {
			trace.Usage.CacheReadTokens = int(v)
		}
		if v, ok := usage["cacheWrite"].(float64); ok {
			trace.Usage.CacheWriteTokens = int(v)
		}
	}
	if meta, ok := raw["metadata"].(map[string]interface{}); ok {
		trace.Metadata = make(map[string]string)
		for k, v := range meta {
			if s, ok := v.(string); ok {
				trace.Metadata[k] = s
			}
		}
	}
	return trace
}
