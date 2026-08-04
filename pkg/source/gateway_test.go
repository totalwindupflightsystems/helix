package source

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/totalwindupflightsystems/helix/pkg/identity"
	"github.com/totalwindupflightsystems/helix/pkg/integration"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// tool builds an MCPTool with the given name and HTTP method.
func tool(name, method string) integration.MCPTool {
	return integration.MCPTool{Name: name, Method: method}
}

// scopedTool builds an MCPTool carrying capability scopes.
func scopedTool(name, method string, scopes ...string) integration.MCPTool {
	return integration.MCPTool{Name: name, Method: method, Scopes: scopes}
}

// describedTool builds an MCPTool with a description.
func describedTool(name, method, description string) integration.MCPTool {
	return integration.MCPTool{Name: name, Method: method, Description: description}
}

// set builds a ToolSet for the named source.
func set(sourceName string, tools ...integration.MCPTool) ToolSet {
	return ToolSet{SourceName: sourceName, Tools: tools, ToolCount: len(tools)}
}

// claim builds a capability claim.
func claim(domain string, strength int) identity.CapabilityClaim {
	return identity.CapabilityClaim{Domain: domain, Strength: strength}
}

// methodNames extracts the HTTP methods of a ToolSet's tools, in order.
func methodNames(ts ToolSet) []string {
	names := make([]string, 0, len(ts.Tools))
	for _, t := range ts.Tools {
		names = append(names, t.Method)
	}
	return names
}

// ---------------------------------------------------------------------------
// Capability gating
// ---------------------------------------------------------------------------

func TestGateway_Filter_NoMatchingCapability(t *testing.T) {
	t.Parallel()
	g := NewGateway(map[string]Source{
		"database": {Name: "database", Type: SourceTypePostgres},
	})
	toolSets := []ToolSet{
		set("database", tool("database_query", "GET"), tool("database_execute", "POST")),
	}

	// Unrelated claim domain: agent gets zero tools from the source.
	got := g.Filter("agent-1", []identity.CapabilityClaim{claim("filesystem", 9)}, toolSets)
	assert.Empty(t, got)

	// No claims at all: zero tools.
	assert.Empty(t, g.Filter("agent-1", nil, toolSets))
	assert.Empty(t, g.Filter("agent-1", []identity.CapabilityClaim{}, toolSets))
}

func TestGateway_Filter_StrengthTiers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		strength int
		want     []string // expected methods in order; nil = ToolSet dropped
	}{
		{name: "below range", strength: 0, want: nil},
		{name: "negative", strength: -3, want: nil},
		{name: "lowest read-only", strength: MinReadOnlyStrength, want: []string{"GET", "HEAD", "OPTIONS"}},
		{name: "mid read-only", strength: 4, want: []string{"GET", "HEAD", "OPTIONS"}},
		{name: "max read-only", strength: MaxReadOnlyStrength, want: []string{"GET", "HEAD", "OPTIONS"}},
		{name: "min read-write", strength: MinReadWriteStrength, want: []string{"GET", "POST", "PUT", "HEAD", "OPTIONS", "DELETE", "PATCH"}},
		{name: "max read-write", strength: 10, want: []string{"GET", "POST", "PUT", "HEAD", "OPTIONS", "DELETE", "PATCH"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewGateway(map[string]Source{
				"database": {Name: "database", Type: SourceTypePostgres},
			})
			toolSets := []ToolSet{
				set("database",
					tool("db_get", "GET"),
					tool("db_post", "POST"),
					tool("db_put", "PUT"),
					tool("db_head", "HEAD"),
					tool("db_options", "OPTIONS"),
					tool("db_delete", "DELETE"),
					tool("db_patch", "PATCH"),
				),
			}
			got := g.Filter("agent-1", []identity.CapabilityClaim{claim("database", tt.strength)}, toolSets)
			if tt.want == nil {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, 1)
			assert.Equal(t, tt.want, methodNames(got[0]))
			assert.Equal(t, len(tt.want), got[0].ToolCount)
		})
	}
}

func TestGateway_Filter_ReadOnlyStripsUnknownAndEmptyMethods(t *testing.T) {
	t.Parallel()
	g := NewGateway(map[string]Source{
		"database": {Name: "database", Type: SourceTypePostgres},
	})
	got := g.Filter("agent-1", []identity.CapabilityClaim{claim("database", 3)},
		[]ToolSet{set("database",
			tool("lowercase_get", "get"), // case-insensitive read method kept
			tool("no_method", ""),
			tool("weird_method", "BREW"),
			tool("real_get", "GET"),
		)})
	require.Len(t, got, 1)
	assert.Equal(t, []string{"get", "GET"}, methodNames(got[0]))
	assert.Equal(t, 2, got[0].ToolCount)
}

func TestGateway_Filter_DropsSourceWhenNoToolsRemain(t *testing.T) {
	t.Parallel()
	g := NewGateway(map[string]Source{
		"database": {Name: "database", Type: SourceTypePostgres},
	})
	// Low-strength agent and only write tools available: ToolSet dropped.
	got := g.Filter("agent-1", []identity.CapabilityClaim{claim("database", 2)},
		[]ToolSet{set("database", tool("db_exec", "POST"), tool("db_del", "DELETE"))})
	assert.Empty(t, got)

	// High-strength agent, but the source is read-only: same outcome.
	g2 := NewGateway(map[string]Source{
		"database": {Name: "database", Type: SourceTypePostgres, ReadOnly: true},
	})
	got = g2.Filter("agent-1", []identity.CapabilityClaim{claim("database", 9)},
		[]ToolSet{set("database", tool("db_exec", "POST"))})
	assert.Empty(t, got)
}

// ---------------------------------------------------------------------------
// Source policy: ReadOnly and AllowedAgents
// ---------------------------------------------------------------------------

func TestGateway_Filter_ReadOnlySourceForcesReadOnly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		strength int
	}{
		{name: "min read-write strength", strength: MinReadWriteStrength},
		{name: "max strength", strength: 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewGateway(map[string]Source{
				"database": {Name: "database", Type: SourceTypePostgres, ReadOnly: true},
			})
			got := g.Filter("agent-1", []identity.CapabilityClaim{claim("database", tt.strength)},
				[]ToolSet{set("database",
					tool("db_get", "GET"), tool("db_exec", "POST"), tool("db_head", "HEAD"))})
			require.Len(t, got, 1)
			assert.Equal(t, []string{"GET", "HEAD"}, methodNames(got[0]))
			assert.Equal(t, 2, got[0].ToolCount)
		})
	}
}

func TestGateway_Filter_AllowedAgents(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		allowed []string
		agentID string
		want    int // expected number of ToolSets
	}{
		{name: "allowed agent", allowed: []string{"agent-1", "agent-2"}, agentID: "agent-2", want: 1},
		{name: "denied agent", allowed: []string{"agent-1"}, agentID: "agent-2", want: 0},
		{name: "exact match is case-sensitive", allowed: []string{"Agent-1"}, agentID: "agent-1", want: 0},
		{name: "empty allow-list is unrestricted", allowed: nil, agentID: "anyone", want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewGateway(map[string]Source{
				"database": {Name: "database", Type: SourceTypePostgres, AllowedAgents: tt.allowed},
			})
			got := g.Filter(tt.agentID, []identity.CapabilityClaim{claim("database", 9)},
				[]ToolSet{set("database", tool("db_get", "GET"))})
			assert.Len(t, got, tt.want)
		})
	}
}

// ---------------------------------------------------------------------------
// Domain matching
// ---------------------------------------------------------------------------

func TestGateway_Filter_DomainMatching(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		claim   identity.CapabilityClaim
		toolSet ToolSet
		want    []string // expected methods; nil = ToolSet dropped
	}{
		{
			name:    "source name match is case-insensitive",
			claim:   claim("DATABASE", 8),
			toolSet: set("database", tool("q", "GET"), tool("w", "POST")),
			want:    []string{"GET", "POST"},
		},
		{
			name:    "tool name mentions domain",
			claim:   claim("database", 8),
			toolSet: set("main", tool("source_database_query", "GET"), tool("source_database_execute", "POST")),
			want:    []string{"GET", "POST"},
		},
		{
			name:    "tool description mentions domain",
			claim:   claim("database", 4),
			toolSet: set("main", describedTool("query", "GET", "Run a database query"), describedTool("update", "POST", "Update the database")),
			want:    []string{"GET"},
		},
		{
			name:    "tool scope mentions domain",
			claim:   claim("database", 4),
			toolSet: set("main", scopedTool("query", "GET", "database:read"), scopedTool("update", "POST", "database:write")),
			want:    []string{"GET"},
		},
		{
			name:    "no mention anywhere",
			claim:   claim("database", 9),
			toolSet: set("main", tool("ping", "GET")),
			want:    nil,
		},
		{
			name:    "empty domain never matches",
			claim:   claim("", 9),
			toolSet: set("database", tool("q", "GET")),
			want:    nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewGateway(map[string]Source{})
			got := g.Filter("agent-1", []identity.CapabilityClaim{tt.claim}, []ToolSet{tt.toolSet})
			if tt.want == nil {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, 1)
			assert.Equal(t, tt.want, methodNames(got[0]))
		})
	}
}

func TestGateway_Filter_StrongestClaimWins(t *testing.T) {
	t.Parallel()
	g := NewGateway(map[string]Source{
		"database": {Name: "database", Type: SourceTypePostgres},
	})
	claims := []identity.CapabilityClaim{
		claim("database", 3),
		claim("filesystem", 9),
		claim("DATABASE", 9), // case-insensitive duplicate: strongest governs
	}
	got := g.Filter("agent-1", claims,
		[]ToolSet{set("database", tool("q", "GET"), tool("w", "POST"))})
	require.Len(t, got, 1)
	assert.Equal(t, []string{"GET", "POST"}, methodNames(got[0]))
}

// ---------------------------------------------------------------------------
// Multi-source and edge cases
// ---------------------------------------------------------------------------

func TestGateway_Filter_MixedMultiSource(t *testing.T) {
	t.Parallel()
	g := NewGateway(map[string]Source{
		"database": {Name: "database", Type: SourceTypePostgres},
		"crm":      {Name: "crm", Type: SourceTypeREST, ReadOnly: true},
		"files":    {Name: "files", Type: SourceTypeLocal},
		"vault":    {Name: "vault", Type: SourceTypeREST, AllowedAgents: []string{"other-agent"}},
	})
	toolSets := []ToolSet{
		set("database", tool("db_select", "GET"), tool("db_insert", "POST")),
		set("crm", tool("crm_list", "GET"), tool("crm_create", "POST")),
		set("files", tool("file_read", "GET"), tool("file_write", "PUT")),
		set("vault", tool("vault_read", "GET")),
		set("metrics", tool("metrics_get", "GET")), // no source config, no claim
	}
	claims := []identity.CapabilityClaim{
		claim("database", 9), // read-write
		claim("crm", 3),      // read-only (source also forces it)
		// "files" and "metrics": no matching claim → dropped
	}
	got := g.Filter("agent-1", claims, toolSets)

	require.Len(t, got, 2)
	// Input ToolSet order preserved.
	assert.Equal(t, "database", got[0].SourceName)
	assert.Equal(t, []string{"GET", "POST"}, methodNames(got[0]))
	assert.Equal(t, "crm", got[1].SourceName)
	assert.Equal(t, []string{"GET"}, methodNames(got[1]))
	assert.Equal(t, 1, got[1].ToolCount)
}

func TestGateway_Filter_UnknownSourceGetsClaimGatingOnly(t *testing.T) {
	t.Parallel()
	g := NewGateway(map[string]Source{
		"database": {Name: "database", Type: SourceTypePostgres, ReadOnly: true},
	})
	// "analytics" is not configured in the gateway: no ReadOnly forcing and
	// no AllowedAgents restriction — the high-strength claim governs alone.
	got := g.Filter("agent-1", []identity.CapabilityClaim{claim("analytics", 9)},
		[]ToolSet{set("analytics", tool("a_get", "GET"), tool("a_post", "POST"))})
	require.Len(t, got, 1)
	assert.Equal(t, []string{"GET", "POST"}, methodNames(got[0]))
}

func TestGateway_Filter_EmptyInputs(t *testing.T) {
	t.Parallel()
	g := NewGateway(map[string]Source{})
	claims := []identity.CapabilityClaim{claim("database", 9)}
	toolSets := []ToolSet{set("database", tool("q", "GET"))}

	assert.Empty(t, g.Filter("agent-1", nil, toolSets))
	assert.Empty(t, g.Filter("agent-1", []identity.CapabilityClaim{}, toolSets))
	assert.Empty(t, g.Filter("agent-1", claims, nil))
	assert.Empty(t, g.Filter("agent-1", claims, []ToolSet{}))
	assert.Empty(t, g.Filter("agent-1", nil, nil))

	// Nil gateway never panics; nil source map means no policy anywhere, so
	// the matching claim governs and the tools are granted.
	var nilGateway *Gateway
	assert.Empty(t, nilGateway.Filter("agent-1", claims, toolSets))

	gNoPolicy := NewGateway(nil)
	got := gNoPolicy.Filter("agent-1", claims, toolSets)
	require.Len(t, got, 1)
	assert.Equal(t, "database", got[0].SourceName)
	assert.Equal(t, []string{"GET"}, methodNames(got[0]))
}
