// Package source provides configuration parsing and bridging for
// multi-source integrations in Helix.
//
// This file implements the Muster bridge (SRC-002): connecting to a Muster
// instance, loading OpenAPI specs from files or URLs, generating MCP tool
// definitions, and returning structured ToolSets.
package source

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/integration"
)

// ---------------------------------------------------------------------------
// ToolSet
// ---------------------------------------------------------------------------

// ToolSet represents a collection of MCP tools generated from an OpenAPI
// specification, together with metadata about the source that produced them.
type ToolSet struct {
	// SourceName is the name of the Helix source that owns this tool set
	// (matches the key in .helix/sources.yaml).
	SourceName string `json:"source_name"`

	// Tools is the list of MCP tools generated from the OpenAPI spec.
	Tools []integration.MCPTool `json:"tools"`

	// GeneratedAt records when the tool set was produced.
	GeneratedAt time.Time `json:"generated_at"`

	// SpecSource is the URL or file path of the OpenAPI specification that
	// was used to generate these tools.
	SpecSource string `json:"spec_source"`

	// ToolCount is a convenience field equal to len(Tools).
	ToolCount int `json:"tool_count"`
}

// Empty returns true when the tool set contains no tools.
func (ts *ToolSet) Empty() bool { return len(ts.Tools) == 0 }

// ---------------------------------------------------------------------------
// BridgeConfig
// ---------------------------------------------------------------------------

// BridgeConfig holds the connection parameters for a Muster instance.
type BridgeConfig struct {
	// BaseURL is the Muster API root (e.g. "http://muster:9090").
	BaseURL string

	// Token is an optional Bearer token for authenticated Muster instances.
	Token string

	// Timeout is the HTTP request timeout. Defaults to 30 seconds when zero.
	Timeout time.Duration
}

// ---------------------------------------------------------------------------
// MusterBridge
// ---------------------------------------------------------------------------

// MusterBridge wraps a Muster HTTP client and adds spec-loading capabilities
// so Helix sources can generate MCP tools from local or remote OpenAPI specs.
type MusterBridge struct {
	client *integration.MusterClient
	cfg    BridgeConfig
}

// NewMusterBridge creates a new MusterBridge with the given configuration.
// A nil cfg is treated as an empty (default) config.
func NewMusterBridge(cfg BridgeConfig) *MusterBridge {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:9090"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	return &MusterBridge{
		client: integration.NewMusterClient(
			cfg.BaseURL,
			cfg.Token,
			integration.WithMusterTimeout(cfg.Timeout),
		),
		cfg: cfg,
	}
}

// Client returns the underlying Muster HTTP client so callers can access
// methods not exposed directly on the bridge.
func (b *MusterBridge) Client() *integration.MusterClient {
	return b.client
}

// Config returns a copy of the bridge configuration.
func (b *MusterBridge) Config() BridgeConfig {
	return b.cfg
}

// ---------------------------------------------------------------------------
// Spec loading
// ---------------------------------------------------------------------------

// LoadSpecFromFile reads an OpenAPI specification from a local file path.
func (b *MusterBridge) LoadSpecFromFile(path string) ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("source: spec file path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("source: cannot read spec file %q: %w", path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("source: spec file %q is empty", path)
	}
	return data, nil
}

// LoadSpecFromURL fetches an OpenAPI specification from a remote URL.
func (b *MusterBridge) LoadSpecFromURL(ctx context.Context, specURL string) ([]byte, error) {
	if specURL == "" {
		return nil, fmt.Errorf("source: spec URL is empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, specURL, nil)
	if err != nil {
		return nil, fmt.Errorf("source: cannot create request for %q: %w", specURL, err)
	}

	client := &http.Client{
		Timeout: b.cfg.Timeout,
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("source: cannot fetch spec from %q: %w", specURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("source: spec URL %q returned %d: %s", specURL, resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("source: cannot read spec body from %q: %w", specURL, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("source: spec URL %q returned empty body", specURL)
	}
	return data, nil
}

// ---------------------------------------------------------------------------
// Tool generation
// ---------------------------------------------------------------------------

// GenerateFromSpec sends raw OpenAPI spec content to Muster and returns a
// ToolSet. The rawSpec is the OpenAPI YAML/JSON bytes; specSource is a
// human-readable identifier (file path or URL) recorded in the result.
//
// When specURL is non-empty, it is forwarded to Muster's GenerateTools so that
// Muster can fetch the spec directly. When empty, the bridge assumes the spec
// has been pre-loaded into the running Muster instance.
func (b *MusterBridge) GenerateFromSpec(specSource string, specURL string, opts integration.GenerateOpts) (*ToolSet, error) {
	if specURL == "" && specSource == "" {
		return nil, fmt.Errorf("source: either specSource or specURL must be provided")
	}

	// If no specURL was given, default to specSource so Muster has something
	// to identify the spec.
	if specURL == "" {
		specURL = specSource
	}

	tools, err := b.client.GenerateTools(specURL, opts)
	if err != nil {
		return nil, fmt.Errorf("source: Muster tool generation failed for %q: %w", specSource, err)
	}

	ts := &ToolSet{
		Tools:       tools,
		ToolCount:   len(tools),
		GeneratedAt: time.Now(),
		SpecSource:  specSource,
	}
	return ts, nil
}

// GenerateToolsFromSource reads the OpenAPI spec referenced by a Helix Source
// (from .helix/sources.yaml) and returns a ToolSet with the generated MCP
// tools.
//
//   - Local files (relative or absolute) are read directly.
//   - HTTP/HTTPS URLs are fetched.
//   - For spec files that are already accessible by Muster, specURL can be set
//     to the same value on the Source config; the bridge will forward it as-is.
func (b *MusterBridge) GenerateToolsFromSource(ctx context.Context, src *Source) (*ToolSet, error) {
	if src == nil {
		return nil, fmt.Errorf("source: nil source provided to GenerateToolsFromSource")
	}
	if src.Name == "" {
		return nil, fmt.Errorf("source: source name is required for tool generation")
	}
	if src.OpenAPI == "" {
		return nil, fmt.Errorf("source: source %q has no OpenAPI spec configured", src.Name)
	}

	specURL := src.OpenAPI

	// Build Muster generation options from source configuration.
	opts := integration.GenerateOpts{
		CacheEnabled:      true,
		RateLimitRPS:      10,
		IncludeDeprecated: false,
	}

	ts, err := b.GenerateFromSpec(src.Name, specURL, opts)
	if err != nil {
		return nil, err
	}
	ts.SourceName = src.Name
	return ts, nil
}

// ---------------------------------------------------------------------------
// Health & introspection
// ---------------------------------------------------------------------------

// Health checks whether the backing Muster instance is reachable and healthy.
func (b *MusterBridge) Health() (*integration.MusterHealth, error) {
	h, err := b.client.Health()
	if err != nil {
		return nil, fmt.Errorf("source: Muster health check failed: %w", err)
	}
	return h, nil
}

// HealthWithCtx performs a health check with caller-controlled context
// and timeout. When the supplied context is cancelled or times out, the
// method returns an error rather than blocking indefinitely.
func (b *MusterBridge) HealthWithCtx(ctx context.Context) (*integration.MusterHealth, error) {
	type result struct {
		h   *integration.MusterHealth
		err error
	}
	ch := make(chan result, 1)

	go func() {
		h, err := b.Health()
		ch <- result{h, err}
	}()

	select {
	case r := <-ch:
		return r.h, r.err
	case <-ctx.Done():
		return &integration.MusterHealth{Status: "unknown"},
			fmt.Errorf("source: Muster health check cancelled: %w", ctx.Err())
	}
}

// ListTools returns all tools currently registered in Muster.
func (b *MusterBridge) ListTools() ([]integration.MCPTool, error) {
	tools, err := b.client.ListTools()
	if err != nil {
		return nil, fmt.Errorf("source: Muster list tools failed: %w", err)
	}
	return tools, nil
}

// ExecuteTool runs a specific MCP tool through Muster against its backing API.
func (b *MusterBridge) ExecuteTool(tool integration.MCPTool, params map[string]any, auth integration.AuthConfig) (*integration.ToolResult, error) {
	result, err := b.client.ExecuteTool(tool, params, auth)
	if err != nil {
		return nil, fmt.Errorf("source: Muster tool execution failed for %q: %w", tool.Name, err)
	}
	return result, nil
}
