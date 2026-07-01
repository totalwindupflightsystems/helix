package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewMusterClient(t *testing.T) {
	c := NewMusterClient("http://localhost:9090", "tok123")
	if c.baseURL != "http://localhost:9090" {
		t.Errorf("baseURL = %q, want http://localhost:9090", c.baseURL)
	}
	if c.token != "tok123" {
		t.Errorf("token = %q, want tok123", c.token)
	}
	if c.httpClient.Timeout != 30*time.Second {
		t.Errorf("timeout = %v, want 30s", c.httpClient.Timeout)
	}
}

func TestNewMusterClient_Options(t *testing.T) {
	customClient := &http.Client{Timeout: 5 * time.Second}
	c := NewMusterClient("http://localhost:9090", "",
		WithMusterHTTPClient(customClient),
		WithMusterTimeout(10*time.Second),
	)
	if c.httpClient != customClient {
		t.Fatal("WithMusterHTTPClient did not set custom client")
	}
	if c.httpClient.Timeout != 10*time.Second {
		t.Errorf("timeout after WithMusterTimeout = %v, want 10s", c.httpClient.Timeout)
	}
}

func TestMusterClient_GenerateTools_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tools/generate" {
			t.Errorf("path = %q, want /api/v1/tools/generate", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			t.Errorf("Authorization = %q, want Bearer test-token", auth)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["spec_url"] != "http://forgejo:3000/swagger.v1.json" {
			t.Errorf("spec_url = %v", body["spec_url"])
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"name":          "listRepos",
				"description":   "List all repositories",
				"method":        "GET",
				"path":          "/api/v1/repos",
				"auth_required": true,
				"parameters": []map[string]interface{}{
					{
						"name":        "limit",
						"type":        "integer",
						"required":    false,
						"description": "Page size",
						"default":     30,
					},
				},
				"scopes": []string{"repo", "user"},
			},
			{
				"name":          "createIssue",
				"description":   "Create an issue",
				"method":        "POST",
				"path":          "/api/v1/repos/{owner}/{repo}/issues",
				"auth_required": true,
			},
		})
	}))
	defer srv.Close()

	c := NewMusterClient(srv.URL, "test-token")
	tools, err := c.GenerateTools("http://forgejo:3000/swagger.v1.json", GenerateOpts{
		CacheEnabled: true,
		RateLimitRPS: 10,
	})
	if err != nil {
		t.Fatalf("GenerateTools() error: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("tools len = %d, want 2", len(tools))
	}
	if tools[0].Name != "listRepos" {
		t.Errorf("tools[0].Name = %q", tools[0].Name)
	}
	if tools[0].Method != "GET" {
		t.Errorf("tools[0].Method = %q", tools[0].Method)
	}
	if !tools[0].AuthRequired {
		t.Errorf("tools[0].AuthRequired = false")
	}
	if len(tools[0].Parameters) != 1 {
		t.Fatalf("tools[0].Parameters len = %d", len(tools[0].Parameters))
	}
	if tools[0].Parameters[0].Name != "limit" {
		t.Errorf("params[0].Name = %q", tools[0].Parameters[0].Name)
	}
	if len(tools[0].Scopes) != 2 {
		t.Errorf("tools[0].Scopes len = %d", len(tools[0].Scopes))
	}
	if tools[1].Name != "createIssue" {
		t.Errorf("tools[1].Name = %q", tools[1].Name)
	}
}

func TestMusterClient_GenerateTools_AuthFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewMusterClient(srv.URL, "bad-token")
	_, err := c.GenerateTools("http://example.com/spec.json", GenerateOpts{})
	if err != ErrMusterAuthFailed {
		t.Errorf("error = %v, want ErrMusterAuthFailed", err)
	}
}

func TestMusterClient_GenerateTools_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewMusterClient(srv.URL, "")
	_, err := c.GenerateTools("http://example.com/spec.json", GenerateOpts{})
	if err != ErrMusterRateLimited {
		t.Errorf("error = %v, want ErrMusterRateLimited", err)
	}
}

func TestMusterClient_GenerateTools_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	c := NewMusterClient(srv.URL, "")
	_, err := c.GenerateTools("http://example.com/spec.json", GenerateOpts{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestMusterClient_GenerateTools_EmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	c := NewMusterClient(srv.URL, "")
	tools, err := c.GenerateTools("http://example.com/spec.json", GenerateOpts{})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("tools len = %d, want 0", len(tools))
	}
}

func TestMusterClient_GenerateTools_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{not-json"))
	}))
	defer srv.Close()

	c := NewMusterClient(srv.URL, "")
	_, err := c.GenerateTools("http://example.com/spec.json", GenerateOpts{})
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestMusterClient_ExecuteTool_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tools/execute" {
			t.Errorf("path = %q, want /api/v1/tools/execute", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			t.Errorf("Authorization = %q, want Bearer test-token", auth)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		toolMap, ok := body["tool"].(map[string]interface{})
		if !ok {
			t.Fatal("tool field missing or not an object")
		}
		if toolMap["name"] != "listRepos" {
			t.Errorf("tool.name = %v", toolMap["name"])
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status_code": 200,
			"body":        `[{"name":"helix"}]`,
			"duration":    0.05,
			"headers": map[string]interface{}{
				"Content-Type": "application/json",
			},
		})
	}))
	defer srv.Close()

	c := NewMusterClient(srv.URL, "test-token")
	tool := MCPTool{Name: "listRepos", Method: "GET", Path: "/api/v1/repos"}
	result, err := c.ExecuteTool(tool, map[string]any{"limit": 10}, AuthConfig{Type: "bearer", Token: "forgejo-tok"})
	if err != nil {
		t.Fatalf("ExecuteTool() error: %v", err)
	}
	if result.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", result.StatusCode)
	}
	if result.Body != `[{"name":"helix"}]` {
		t.Errorf("Body = %q", result.Body)
	}
	if result.Duration != 0.05 {
		t.Errorf("Duration = %f", result.Duration)
	}
	if result.Headers["Content-Type"] != "application/json" {
		t.Errorf("Headers[Content-Type] = %q", result.Headers["Content-Type"])
	}
}

func TestMusterClient_ExecuteTool_AuthFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewMusterClient(srv.URL, "")
	_, err := c.ExecuteTool(MCPTool{Name: "x"}, nil, AuthConfig{})
	if err != ErrMusterAuthFailed {
		t.Errorf("error = %v, want ErrMusterAuthFailed", err)
	}
}

func TestMusterClient_ExecuteTool_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c := NewMusterClient(srv.URL, "")
	_, err := c.ExecuteTool(MCPTool{Name: "x"}, nil, AuthConfig{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMusterClient_ExecuteTool_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{bad"))
	}))
	defer srv.Close()

	c := NewMusterClient(srv.URL, "")
	_, err := c.ExecuteTool(MCPTool{Name: "x"}, nil, AuthConfig{})
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestMusterClient_ListTools_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tools" {
			t.Errorf("path = %q, want /api/v1/tools", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"name":          "getRepo",
				"description":   "Get a repository",
				"method":        "GET",
				"path":          "/api/v1/repos/{owner}/{repo}",
				"auth_required": false,
			},
		})
	}))
	defer srv.Close()

	c := NewMusterClient(srv.URL, "tok")
	tools, err := c.ListTools()
	if err != nil {
		t.Fatalf("ListTools() error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	if tools[0].Name != "getRepo" {
		t.Errorf("tools[0].Name = %q", tools[0].Name)
	}
}

func TestMusterClient_ListTools_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	c := NewMusterClient(srv.URL, "")
	tools, err := c.ListTools()
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("tools len = %d, want 0", len(tools))
	}
}

func TestMusterClient_ListTools_AuthFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewMusterClient(srv.URL, "")
	_, err := c.ListTools()
	if err != ErrMusterAuthFailed {
		t.Errorf("error = %v, want ErrMusterAuthFailed", err)
	}
}

func TestMusterClient_ListTools_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewMusterClient(srv.URL, "")
	_, err := c.ListTools()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMusterClient_Health_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/health" {
			t.Errorf("path = %q, want /api/v1/health", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":         "healthy",
			"tools_loaded":   42,
			"cache_hit_rate": 0.87,
		})
	}))
	defer srv.Close()

	c := NewMusterClient(srv.URL, "")
	h, err := c.Health()
	if err != nil {
		t.Fatalf("Health() error: %v", err)
	}
	if h.Status != "healthy" {
		t.Errorf("Status = %q", h.Status)
	}
	if h.ToolsLoaded != 42 {
		t.Errorf("ToolsLoaded = %d", h.ToolsLoaded)
	}
	if h.CacheHitRate != 0.87 {
		t.Errorf("CacheHitRate = %f", h.CacheHitRate)
	}
}

func TestMusterClient_Health_Down(t *testing.T) {
	c := NewMusterClient("http://127.0.0.1:0", "")
	h, err := c.Health()
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
	if h == nil {
		t.Fatal("expected non-nil health even on error")
	}
	if h.Status != "down" {
		t.Errorf("Status = %q, want down", h.Status)
	}
}

func TestMusterClient_Health_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewMusterClient(srv.URL, "")
	h, err := c.Health()
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if h.Status != "down" {
		t.Errorf("Status = %q, want down", h.Status)
	}
}

func TestMusterClient_Health_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{bad"))
	}))
	defer srv.Close()

	c := NewMusterClient(srv.URL, "")
	h, err := c.Health()
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if h.Status != "healthy" {
		t.Errorf("Status = %q, want healthy (fallback on decode error)", h.Status)
	}
}

func TestParseMCPTool_Empty(t *testing.T) {
	t2 := parseMCPTool(map[string]interface{}{})
	if t2.Name != "" {
		t.Errorf("Name = %q, want empty", t2.Name)
	}
}

func TestParseToolResult_Empty(t *testing.T) {
	r := parseToolResult(map[string]interface{}{})
	if r.StatusCode != 0 {
		t.Errorf("StatusCode = %d, want 0", r.StatusCode)
	}
}
