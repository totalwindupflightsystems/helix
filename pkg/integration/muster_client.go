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
// Muster HTTP Client — concrete implementation of MusterAdapter
//
// Per spec integrations.md §4:
// "Muster auto-generates MCP tools from OpenAPI specs. Parses any REST API
// → CLI commands, MCP tools, shell completions, Starlark DSL. The universal
// adapter for external services (Forgejo, LangFuse, OpenRouter)."
//
// This client wraps Muster's REST API into Go calls implementing the
// MusterAdapter interface.
// ---------------------------------------------------------------------------

// Sentinel errors for Muster API failures.
var (
	ErrMusterAuthFailed    = fmt.Errorf("muster: authentication failed (401)")
	ErrMusterRateLimited   = fmt.Errorf("muster: rate limited (429)")
	ErrMusterServerDown    = fmt.Errorf("muster: server unreachable")
	ErrMusterInvalidResult = fmt.Errorf("muster: invalid response")
)

// MusterClient is the HTTP implementation of MusterAdapter.
type MusterClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// MusterClientOption configures a MusterClient.
type MusterClientOption func(*MusterClient)

// WithMusterHTTPClient sets a custom HTTP client (for testing).
func WithMusterHTTPClient(c *http.Client) MusterClientOption {
	return func(mc *MusterClient) { mc.httpClient = c }
}

// WithMusterTimeout sets the HTTP timeout.
func WithMusterTimeout(d time.Duration) MusterClientOption {
	return func(mc *MusterClient) { mc.httpClient.Timeout = d }
}

// NewMusterClient creates a new Muster HTTP client.
// baseURL is the Muster API root (e.g., http://muster:9090).
// token is the optional Bearer token for authentication.
func NewMusterClient(baseURL, token string, opts ...MusterClientOption) *MusterClient {
	c := &MusterClient{
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

// GenerateTools parses an OpenAPI spec and returns MCP tool definitions.
func (c *MusterClient) GenerateTools(specURL string, opts GenerateOpts) ([]MCPTool, error) {
	body := map[string]interface{}{
		"spec_url":           specURL,
		"cache_enabled":      opts.CacheEnabled,
		"rate_limit_rps":     opts.RateLimitRPS,
		"include_deprecated": opts.IncludeDeprecated,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal generate request: %w", err)
	}

	url := c.baseURL + "/api/v1/tools/generate"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMusterServerDown, err)
	}
	defer resp.Body.Close()

	if err := checkMusterError(resp); err != nil {
		return nil, err
	}

	var rawList []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rawList); err != nil {
		return nil, fmt.Errorf("%w: decode error: %v", ErrMusterInvalidResult, err)
	}

	tools := make([]MCPTool, 0, len(rawList))
	for _, raw := range rawList {
		tools = append(tools, parseMCPTool(raw))
	}
	return tools, nil
}

// ExecuteTool calls a specific API endpoint defined by an MCP tool.
func (c *MusterClient) ExecuteTool(tool MCPTool, params map[string]any, auth AuthConfig) (*ToolResult, error) {
	body := map[string]interface{}{
		"tool": map[string]interface{}{
			"name":          tool.Name,
			"description":   tool.Description,
			"method":        tool.Method,
			"path":          tool.Path,
			"auth_required": tool.AuthRequired,
		},
		"params": params,
		"auth": map[string]interface{}{
			"type":   auth.Type,
			"token":  auth.Token,
			"header": auth.Header,
		},
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal execute request: %w", err)
	}

	url := c.baseURL + "/api/v1/tools/execute"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMusterServerDown, err)
	}
	defer resp.Body.Close()

	if err := checkMusterError(resp); err != nil {
		return nil, err
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("%w: decode error: %v", ErrMusterInvalidResult, err)
	}

	return parseToolResult(raw), nil
}

// ListTools returns all currently loaded tools.
func (c *MusterClient) ListTools() ([]MCPTool, error) {
	url := c.baseURL + "/api/v1/tools"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMusterServerDown, err)
	}
	defer resp.Body.Close()

	if err := checkMusterError(resp); err != nil {
		return nil, err
	}

	var rawList []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rawList); err != nil {
		return nil, fmt.Errorf("%w: decode error: %v", ErrMusterInvalidResult, err)
	}

	tools := make([]MCPTool, 0, len(rawList))
	for _, raw := range rawList {
		tools = append(tools, parseMCPTool(raw))
	}
	return tools, nil
}

// Health returns service health.
func (c *MusterClient) Health() (*MusterHealth, error) {
	url := c.baseURL + "/api/v1/health"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	c.setAuth(req)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &MusterHealth{Status: "down"}, fmt.Errorf("%w: %v", ErrMusterServerDown, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &MusterHealth{Status: "down"}, nil
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return &MusterHealth{Status: "healthy"}, nil
	}

	h := &MusterHealth{Status: "healthy"}
	if v, ok := raw["status"].(string); ok {
		h.Status = v
	}
	if v, ok := raw["tools_loaded"].(float64); ok {
		h.ToolsLoaded = int(v)
	}
	if v, ok := raw["cache_hit_rate"].(float64); ok {
		h.CacheHitRate = v
	}
	return h, nil
}

// setAuth adds the Bearer token header if configured.
func (c *MusterClient) setAuth(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

// checkMusterError inspects the HTTP response status and returns a sentinel
// error for known failure codes.
func checkMusterError(resp *http.Response) error {
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrMusterAuthFailed
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return ErrMusterRateLimited
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("muster returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// parseMCPTool converts a raw JSON map into a typed MCPTool.
func parseMCPTool(raw map[string]interface{}) MCPTool {
	t := MCPTool{}
	if v, ok := raw["name"].(string); ok {
		t.Name = v
	}
	if v, ok := raw["description"].(string); ok {
		t.Description = v
	}
	if v, ok := raw["method"].(string); ok {
		t.Method = v
	}
	if v, ok := raw["path"].(string); ok {
		t.Path = v
	}
	if v, ok := raw["auth_required"].(bool); ok {
		t.AuthRequired = v
	}
	if params, ok := raw["parameters"].([]interface{}); ok {
		for _, pi := range params {
			if pm, ok := pi.(map[string]interface{}); ok {
				p := ToolParam{}
				if v, ok := pm["name"].(string); ok {
					p.Name = v
				}
				if v, ok := pm["type"].(string); ok {
					p.Type = v
				}
				if v, ok := pm["required"].(bool); ok {
					p.Required = v
				}
				if v, ok := pm["description"].(string); ok {
					p.Description = v
				}
				p.Default = pm["default"]
				t.Parameters = append(t.Parameters, p)
			}
		}
	}
	if scopes, ok := raw["scopes"].([]interface{}); ok {
		for _, si := range scopes {
			if s, ok := si.(string); ok {
				t.Scopes = append(t.Scopes, s)
			}
		}
	}
	return t
}

// parseToolResult converts a raw JSON map into a typed ToolResult.
func parseToolResult(raw map[string]interface{}) *ToolResult {
	r := &ToolResult{}
	if v, ok := raw["status_code"].(float64); ok {
		r.StatusCode = int(v)
	}
	if v, ok := raw["body"].(string); ok {
		r.Body = v
	}
	if v, ok := raw["duration"].(float64); ok {
		r.Duration = v
	}
	if headers, ok := raw["headers"].(map[string]interface{}); ok {
		r.Headers = make(map[string]string, len(headers))
		for k, v := range headers {
			if s, ok := v.(string); ok {
				r.Headers[k] = s
			}
		}
	}
	return r
}
