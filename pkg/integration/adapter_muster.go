package integration

// ---------------------------------------------------------------------------
// Muster Adapter — specs/integrations.md §4
// ---------------------------------------------------------------------------
//
// Muster auto-generates MCP tools from OpenAPI specs. Parses any REST API
// → CLI commands, MCP tools, shell completions, Starlark DSL. The universal
// adapter for external services (Forgejo, LangFuse, OpenRouter).

// MusterAdapter defines the contract for Muster API glue.
type MusterAdapter interface {
	// GenerateTools parses an OpenAPI spec and returns MCP tool definitions.
	GenerateTools(specURL string, opts GenerateOpts) ([]MCPTool, error)

	// ExecuteTool calls a specific API endpoint defined by an MCP tool.
	ExecuteTool(tool MCPTool, params map[string]any, auth AuthConfig) (*ToolResult, error)

	// ListTools returns all currently loaded tools.
	ListTools() ([]MCPTool, error)

	// Health returns service health.
	Health() (*MusterHealth, error)
}

// GenerateOpts configures tool generation from OpenAPI specs.
type GenerateOpts struct {
	CacheEnabled      bool // Use multi-tier cache (default: true)
	RateLimitRPS      int  // Max requests/second to source API (default: 10)
	IncludeDeprecated bool // Include deprecated endpoints (default: false)
}

// MCPTool represents a single MCP tool generated from an API endpoint.
type MCPTool struct {
	Name         string
	Description  string
	Method       string
	Path         string
	Parameters   []ToolParam
	AuthRequired bool
	Scopes       []string
}

// ToolParam describes a single parameter for an MCP tool.
type ToolParam struct {
	Name        string
	Type        string
	Required    bool
	Description string
	Default     any
}

// AuthConfig holds authentication for API calls.
type AuthConfig struct {
	Type   string // "bearer", "basic", "api_key"
	Token  string
	Header string // Header name for api_key type
}

// ToolResult captures the result of executing an MCP tool.
type ToolResult struct {
	StatusCode int
	Body       string
	Headers    map[string]string
	Duration   float64
}

// MusterHealth reports Muster's operational status.
type MusterHealth struct {
	Status       string
	ToolsLoaded  int
	CacheHitRate float64
}
