package source

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/totalwindupflightsystems/helix/pkg/integration"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// writeTestOpenAPISpec creates a minimal valid OpenAPI 3.x spec in a temp file.
func writeTestOpenAPISpec(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "api.openapi.yaml")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

// mustMusterServer creates an httptest.Server that mimics a Muster API with
// the given response function. The cleanup closure is returned for defer at
// the caller's call site.
func mustMusterServer(t *testing.T, handler http.HandlerFunc) (string, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	return srv.URL, srv.Close
}

// validOpenAPISpec is a minimal OpenAPI 3.0 spec used across tests.
const validOpenAPISpec = `openapi: "3.0.3"
info:
  title: Test API
  version: "1.0.0"
paths:
  /users:
    get:
      operationId: listUsers
      summary: List all users
      parameters:
        - name: limit
          in: query
          schema:
            type: integer
      responses:
        "200":
          description: OK
  /users/{id}:
    get:
      operationId: getUser
      summary: Get a user by ID
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: OK
`

// ---------------------------------------------------------------------------
// NewMusterBridge — constructor tests
// ---------------------------------------------------------------------------

func TestNewMusterBridge_DefaultConfig(t *testing.T) {
	t.Parallel()
	b := NewMusterBridge(BridgeConfig{})
	require.NotNil(t, b)
	cfg := b.Config()
	assert.Equal(t, "http://localhost:9090", cfg.BaseURL)
	assert.Equal(t, 30*time.Second, cfg.Timeout)
	assert.Equal(t, "", cfg.Token)
	assert.NotNil(t, b.Client())
}

func TestNewMusterBridge_CustomBaseURL(t *testing.T) {
	t.Parallel()
	b := NewMusterBridge(BridgeConfig{BaseURL: "http://muster.internal:8080"})
	require.NotNil(t, b)
	cfg := b.Config()
	assert.Equal(t, "http://muster.internal:8080", cfg.BaseURL)
}

func TestNewMusterBridge_WithToken(t *testing.T) {
	t.Parallel()
	b := NewMusterBridge(BridgeConfig{
		BaseURL: "http://muster:9090",
		Token:   "secret-token",
	})
	require.NotNil(t, b)
	cfg := b.Config()
	assert.Equal(t, "secret-token", cfg.Token)
}

func TestNewMusterBridge_CustomTimeout(t *testing.T) {
	t.Parallel()
	b := NewMusterBridge(BridgeConfig{
		BaseURL: "http://muster:9090",
		Timeout: 5 * time.Second,
	})
	require.NotNil(t, b)
	cfg := b.Config()
	assert.Equal(t, 5*time.Second, cfg.Timeout)
}

func TestNewMusterBridge_ZeroTimeoutDefaultsTo30s(t *testing.T) {
	t.Parallel()
	b := NewMusterBridge(BridgeConfig{
		BaseURL: "http://muster:9090",
		Timeout: 0,
	})
	require.NotNil(t, b)
	cfg := b.Config()
	assert.Equal(t, 30*time.Second, cfg.Timeout)
}

// ---------------------------------------------------------------------------
// LoadSpecFromFile — success cases
// ---------------------------------------------------------------------------

func TestLoadSpecFromFile_Success(t *testing.T) {
	t.Parallel()
	p := writeTestOpenAPISpec(t, validOpenAPISpec)
	b := NewMusterBridge(BridgeConfig{})

	data, err := b.LoadSpecFromFile(p)
	require.NoError(t, err)
	assert.Contains(t, string(data), "Test API")
	assert.Contains(t, string(data), "listUsers")
}

func TestLoadSpecFromFile_JSONSpec(t *testing.T) {
	t.Parallel()
	jsonSpec := `{"openapi":"3.0.0","info":{"title":"JSON API"},"paths":{}}`
	dir := t.TempDir()
	p := filepath.Join(dir, "api.json")
	require.NoError(t, os.WriteFile(p, []byte(jsonSpec), 0o644))

	b := NewMusterBridge(BridgeConfig{})
	data, err := b.LoadSpecFromFile(p)
	require.NoError(t, err)
	assert.Contains(t, string(data), "JSON API")
}

// ---------------------------------------------------------------------------
// LoadSpecFromFile — error cases
// ---------------------------------------------------------------------------

func TestLoadSpecFromFile_EmptyPath(t *testing.T) {
	t.Parallel()
	b := NewMusterBridge(BridgeConfig{})
	_, err := b.LoadSpecFromFile("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestLoadSpecFromFile_NonExistentFile(t *testing.T) {
	t.Parallel()
	b := NewMusterBridge(BridgeConfig{})
	_, err := b.LoadSpecFromFile("/nonexistent/path/spec.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot read spec file")
}

func TestLoadSpecFromFile_EmptyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "empty.yaml")
	require.NoError(t, os.WriteFile(p, []byte{}, 0o644))

	b := NewMusterBridge(BridgeConfig{})
	_, err := b.LoadSpecFromFile(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestLoadSpecFromFile_WhitespaceOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "blank.yaml")
	require.NoError(t, os.WriteFile(p, []byte("\n\n  \n"), 0o644))

	b := NewMusterBridge(BridgeConfig{})
	_, err := b.LoadSpecFromFile(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

// ---------------------------------------------------------------------------
// LoadSpecFromURL — success cases
// ---------------------------------------------------------------------------

func TestLoadSpecFromURL_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-yaml")
		_, _ = w.Write([]byte(validOpenAPISpec))
	}))
	defer srv.Close()

	b := NewMusterBridge(BridgeConfig{Timeout: 5 * time.Second})
	data, err := b.LoadSpecFromURL(context.Background(), srv.URL)
	require.NoError(t, err)
	assert.Contains(t, string(data), "Test API")
}

// ---------------------------------------------------------------------------
// LoadSpecFromURL — error cases
// ---------------------------------------------------------------------------

func TestLoadSpecFromURL_EmptyURL(t *testing.T) {
	t.Parallel()
	b := NewMusterBridge(BridgeConfig{})
	_, err := b.LoadSpecFromURL(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestLoadSpecFromURL_ServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	b := NewMusterBridge(BridgeConfig{Timeout: 5 * time.Second})
	_, err := b.LoadSpecFromURL(context.Background(), srv.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestLoadSpecFromURL_NotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	b := NewMusterBridge(BridgeConfig{Timeout: 5 * time.Second})
	_, err := b.LoadSpecFromURL(context.Background(), srv.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestLoadSpecFromURL_EmptyBody(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	b := NewMusterBridge(BridgeConfig{Timeout: 5 * time.Second})
	_, err := b.LoadSpecFromURL(context.Background(), srv.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty body")
}

func TestLoadSpecFromURL_Unreachable(t *testing.T) {
	t.Parallel()
	b := NewMusterBridge(BridgeConfig{Timeout: 100 * time.Millisecond})
	_, err := b.LoadSpecFromURL(context.Background(), "http://127.0.0.1:0/spec.yaml")
	require.Error(t, err)
}

func TestLoadSpecFromURL_ContextCancelled(t *testing.T) {
	t.Parallel()
	// Slow server that waits; we cancel the context immediately.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()

	b := NewMusterBridge(BridgeConfig{Timeout: 10 * time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := b.LoadSpecFromURL(ctx, srv.URL)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// GenerateFromSpec — success cases
// ---------------------------------------------------------------------------

func TestGenerateFromSpec_Success(t *testing.T) {
	t.Parallel()
	srvURL, cleanup := mustMusterServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/tools/generate", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)

		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "http://example.com/api.yaml", body["spec_url"])

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"name":          "listUsers",
				"description":   "List all users",
				"method":        "GET",
				"path":          "/users",
				"auth_required": false,
			},
			{
				"name":          "getUser",
				"description":   "Get a user",
				"method":        "GET",
				"path":          "/users/{id}",
				"auth_required": true,
			},
		})
	})
	defer cleanup()

	b := NewMusterBridge(BridgeConfig{BaseURL: srvURL, Timeout: 5 * time.Second})
	ts, err := b.GenerateFromSpec("test-source", "http://example.com/api.yaml", integration.GenerateOpts{
		CacheEnabled: true,
		RateLimitRPS: 10,
	})
	require.NoError(t, err)
	require.NotNil(t, ts)
	assert.Equal(t, "test-source", ts.SpecSource)
	assert.Equal(t, 2, ts.ToolCount)
	assert.False(t, ts.Empty())
	assert.False(t, ts.GeneratedAt.IsZero())

	require.Len(t, ts.Tools, 2)
	assert.Equal(t, "listUsers", ts.Tools[0].Name)
	assert.Equal(t, "GET", ts.Tools[0].Method)
	assert.Equal(t, "getUser", ts.Tools[1].Name)
	assert.True(t, ts.Tools[1].AuthRequired)
}

func TestGenerateFromSpec_EmptyTools(t *testing.T) {
	t.Parallel()
	srvURL, cleanup := mustMusterServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	})
	defer cleanup()

	b := NewMusterBridge(BridgeConfig{BaseURL: srvURL, Timeout: 5 * time.Second})
	ts, err := b.GenerateFromSpec("empty-api", "http://example.com/empty.yaml", integration.GenerateOpts{})
	require.NoError(t, err)
	require.NotNil(t, ts)
	assert.True(t, ts.Empty())
	assert.Equal(t, 0, ts.ToolCount)
}

func TestGenerateFromSpec_SpecURLEmptyDefaultsToSpecSource(t *testing.T) {
	t.Parallel()
	var capturedSpecURL string
	srvURL, cleanup := mustMusterServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		if v, ok := body["spec_url"].(string); ok {
			capturedSpecURL = v
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	})
	defer cleanup()

	b := NewMusterBridge(BridgeConfig{BaseURL: srvURL, Timeout: 5 * time.Second})
	_, err := b.GenerateFromSpec("my-source", "", integration.GenerateOpts{})
	require.NoError(t, err)
	assert.Equal(t, "my-source", capturedSpecURL,
		"specURL should default to specSource when empty")
}

// ---------------------------------------------------------------------------
// GenerateFromSpec — error cases
// ---------------------------------------------------------------------------

func TestGenerateFromSpec_BothEmpty(t *testing.T) {
	t.Parallel()
	b := NewMusterBridge(BridgeConfig{})
	_, err := b.GenerateFromSpec("", "", integration.GenerateOpts{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "either specSource or specURL")
}

func TestGenerateFromSpec_MusterUnavailable(t *testing.T) {
	t.Parallel()
	// Use a port that nothing listens on.
	b := NewMusterBridge(BridgeConfig{
		BaseURL: "http://127.0.0.1:0",
		Timeout: 100 * time.Millisecond,
	})
	_, err := b.GenerateFromSpec("test", "http://example.com/spec.yaml", integration.GenerateOpts{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Muster tool generation failed")
}

func TestGenerateFromSpec_AuthFailed(t *testing.T) {
	t.Parallel()
	srvURL, cleanup := mustMusterServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	defer cleanup()

	b := NewMusterBridge(BridgeConfig{BaseURL: srvURL, Token: "bad-token", Timeout: 5 * time.Second})
	_, err := b.GenerateFromSpec("test", "http://example.com/spec.yaml", integration.GenerateOpts{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Muster tool generation failed")
	// The underlying error should be ErrMusterAuthFailed.
}

func TestGenerateFromSpec_MalformedJSON(t *testing.T) {
	t.Parallel()
	srvURL, cleanup := mustMusterServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{not-json"))
	})
	defer cleanup()

	b := NewMusterBridge(BridgeConfig{BaseURL: srvURL, Timeout: 5 * time.Second})
	_, err := b.GenerateFromSpec("test", "http://example.com/spec.yaml", integration.GenerateOpts{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Muster tool generation failed")
}

func TestGenerateFromSpec_RateLimited(t *testing.T) {
	t.Parallel()
	srvURL, cleanup := mustMusterServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	defer cleanup()

	b := NewMusterBridge(BridgeConfig{BaseURL: srvURL, Timeout: 5 * time.Second})
	_, err := b.GenerateFromSpec("test", "http://example.com/spec.yaml", integration.GenerateOpts{})
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// GenerateToolsFromSource
// ---------------------------------------------------------------------------

func TestGenerateToolsFromSource_Success(t *testing.T) {
	t.Parallel()
	srvURL, cleanup := mustMusterServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/tools/generate", r.URL.Path)

		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"name":        "listRepos",
				"description": "List repos",
				"method":      "GET",
				"path":        "/repos",
			},
		})
	})
	defer cleanup()

	b := NewMusterBridge(BridgeConfig{BaseURL: srvURL, Timeout: 5 * time.Second})
	src := &Source{
		Name:    "forgejo",
		Type:    SourceTypeREST,
		BaseURL: "http://forgejo:3000",
		OpenAPI: "http://forgejo:3000/swagger.v1.json",
	}

	ts, err := b.GenerateToolsFromSource(context.Background(), src)
	require.NoError(t, err)
	require.NotNil(t, ts)
	assert.Equal(t, "forgejo", ts.SourceName)
	assert.Equal(t, "forgejo", ts.SpecSource)
	assert.Equal(t, 1, ts.ToolCount)
}

func TestGenerateToolsFromSource_NilSource(t *testing.T) {
	t.Parallel()
	b := NewMusterBridge(BridgeConfig{})
	_, err := b.GenerateToolsFromSource(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil source")
}

func TestGenerateToolsFromSource_EmptyName(t *testing.T) {
	t.Parallel()
	b := NewMusterBridge(BridgeConfig{})
	src := &Source{
		Name:    "",
		OpenAPI: "./spec.yaml",
	}
	_, err := b.GenerateToolsFromSource(context.Background(), src)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source name is required")
}

func TestGenerateToolsFromSource_MissingOpenAPI(t *testing.T) {
	t.Parallel()
	b := NewMusterBridge(BridgeConfig{})
	src := &Source{
		Name: "my-source",
	}
	_, err := b.GenerateToolsFromSource(context.Background(), src)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no OpenAPI spec configured")
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

func TestMusterBridge_Health_Healthy(t *testing.T) {
	t.Parallel()
	srvURL, cleanup := mustMusterServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/health", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":         "healthy",
			"tools_loaded":   5,
			"cache_hit_rate": 0.92,
		})
	})
	defer cleanup()

	b := NewMusterBridge(BridgeConfig{BaseURL: srvURL, Timeout: 5 * time.Second})
	h, err := b.Health()
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.Equal(t, "healthy", h.Status)
	assert.Equal(t, 5, h.ToolsLoaded)
	assert.InDelta(t, 0.92, h.CacheHitRate, 0.001)
}

func TestMusterBridge_Health_Down(t *testing.T) {
	t.Parallel()
	b := NewMusterBridge(BridgeConfig{
		BaseURL: "http://127.0.0.1:0",
		Timeout: 100 * time.Millisecond,
	})
	_, err := b.Health()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Muster health check failed")
}

func TestMusterBridge_HealthWithCtx_Cancelled(t *testing.T) {
	t.Parallel()
	// Slow server.
	srvURL, cleanup := mustMusterServer(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
	})
	defer cleanup()

	b := NewMusterBridge(BridgeConfig{BaseURL: srvURL, Timeout: 10 * time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	h, err := b.HealthWithCtx(ctx)
	require.Error(t, err)
	assert.NotNil(t, h)
	assert.Equal(t, "unknown", h.Status)
}

func TestMusterBridge_HealthWithCtx_Success(t *testing.T) {
	t.Parallel()
	srvURL, cleanup := mustMusterServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "healthy",
			"tools_loaded": 3,
		})
	})
	defer cleanup()

	b := NewMusterBridge(BridgeConfig{BaseURL: srvURL, Timeout: 5 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	h, err := b.HealthWithCtx(ctx)
	require.NoError(t, err)
	assert.Equal(t, "healthy", h.Status)
}

// ---------------------------------------------------------------------------
// ListTools
// ---------------------------------------------------------------------------

func TestMusterBridge_ListTools(t *testing.T) {
	t.Parallel()
	srvURL, cleanup := mustMusterServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/tools", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"name":          "listUsers",
				"description":   "List users",
				"method":        "GET",
				"path":          "/users",
				"auth_required": false,
			},
		})
	})
	defer cleanup()

	b := NewMusterBridge(BridgeConfig{BaseURL: srvURL, Timeout: 5 * time.Second})
	tools, err := b.ListTools()
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "listUsers", tools[0].Name)
}

func TestMusterBridge_ListTools_ServerError(t *testing.T) {
	t.Parallel()
	srvURL, cleanup := mustMusterServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer cleanup()

	b := NewMusterBridge(BridgeConfig{BaseURL: srvURL, Timeout: 5 * time.Second})
	_, err := b.ListTools()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Muster list tools failed")
}

// ---------------------------------------------------------------------------
// ExecuteTool
// ---------------------------------------------------------------------------

func TestMusterBridge_ExecuteTool_Success(t *testing.T) {
	t.Parallel()
	srvURL, cleanup := mustMusterServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/tools/execute", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)

		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		toolMap, ok := body["tool"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "listUsers", toolMap["name"])

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status_code": 200,
			"body":        `[{"name":"Alice"}]`,
			"duration":    0.042,
		})
	})
	defer cleanup()

	b := NewMusterBridge(BridgeConfig{BaseURL: srvURL, Timeout: 5 * time.Second})
	tool := integration.MCPTool{Name: "listUsers", Method: "GET", Path: "/users"}
	result, err := b.ExecuteTool(tool, map[string]any{"limit": 10}, integration.AuthConfig{})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 200, result.StatusCode)
	assert.Contains(t, result.Body, "Alice")
}

func TestMusterBridge_ExecuteTool_AuthFailed(t *testing.T) {
	t.Parallel()
	srvURL, cleanup := mustMusterServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	defer cleanup()

	b := NewMusterBridge(BridgeConfig{BaseURL: srvURL, Timeout: 5 * time.Second})
	_, err := b.ExecuteTool(integration.MCPTool{Name: "x"}, nil, integration.AuthConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Muster tool execution failed")
}

// ---------------------------------------------------------------------------
// ToolSet
// ---------------------------------------------------------------------------

func TestToolSet_Empty(t *testing.T) {
	t.Parallel()
	ts := &ToolSet{}
	assert.True(t, ts.Empty())

	ts.Tools = []integration.MCPTool{{Name: "test"}}
	ts.ToolCount = 1
	assert.False(t, ts.Empty())
}

func TestToolSet_GeneratedAt(t *testing.T) {
	t.Parallel()
	ts := &ToolSet{
		SourceName:  "test",
		Tools:       nil,
		ToolCount:   0,
		GeneratedAt: time.Now(),
		SpecSource:  "http://example.com/spec.yaml",
	}
	assert.True(t, ts.Empty())
	assert.Equal(t, "test", ts.SourceName)
	assert.Equal(t, "http://example.com/spec.yaml", ts.SpecSource)
}

// ---------------------------------------------------------------------------
// Integration: bridge used with file-based spec
// ---------------------------------------------------------------------------

func TestBridge_Integration_FileSpecToTools(t *testing.T) {
	t.Parallel()
	// Write a real spec file.
	specPath := writeTestOpenAPISpec(t, validOpenAPISpec)

	// Start a mock Muster server.
	srvURL, cleanup := mustMusterServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Verify Muster received a request with our spec URL.
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		// Muster should receive the file path as spec_url.
		assert.Contains(t, body["spec_url"].(string), "api.openapi.yaml")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"name":        "listUsers",
				"description": "List users",
				"method":      "GET",
				"path":        "/users",
				"parameters": []map[string]interface{}{
					{
						"name":        "limit",
						"type":        "integer",
						"required":    false,
						"description": "Page size",
					},
				},
			},
		})
	})
	defer cleanup()

	b := NewMusterBridge(BridgeConfig{BaseURL: srvURL, Timeout: 5 * time.Second})

	// Verify file can be loaded.
	specData, err := b.LoadSpecFromFile(specPath)
	require.NoError(t, err)
	assert.NotEmpty(t, specData)

	// Generate tools using the file path as spec URL.
	ts, err := b.GenerateFromSpec("local-api", specPath, integration.GenerateOpts{})
	require.NoError(t, err)
	assert.Equal(t, 1, ts.ToolCount)
	assert.Equal(t, "listUsers", ts.Tools[0].Name)
	assert.Len(t, ts.Tools[0].Parameters, 1)
	assert.Equal(t, "limit", ts.Tools[0].Parameters[0].Name)
}
