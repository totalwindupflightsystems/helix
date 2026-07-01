package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLangFuseClient_IngestTrace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/public/ingestion" {
			t.Errorf("expected /api/public/ingestion, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		user, pass, ok := r.BasicAuth()
		if !ok || user != "pk-test" || pass != "sk-test" {
			t.Errorf("auth failed: user=%s pass=%s", user, pass)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["name"] != "test-trace" {
			t.Errorf("expected name=test-trace, got %v", body["name"])
		}

		if err := json.NewEncoder(w).Encode(map[string]string{
			"id":     "trace-123",
			"status": "accepted",
		}); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	client := NewLangFuseClient(server.URL, "pk-test", "sk-test")
	result, err := client.IngestTrace(LangFuseTrace{
		ID:       "trace-123",
		Name:     "test-trace",
		Project:  "helix",
		Input:    "test input",
		Output:   "test output",
		Model:    "deepseek-v4-flash",
		Provider: "deepseek",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != "trace-123" {
		t.Errorf("expected ID trace-123, got %s", result.ID)
	}
	if result.Status != "accepted" {
		t.Errorf("expected status accepted, got %s", result.Status)
	}
}

func TestLangFuseClient_IngestTrace_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := NewLangFuseClient(server.URL, "pk", "sk")
	_, err := client.IngestTrace(LangFuseTrace{
		ID:   "trace-1",
		Name: "test",
	})
	if err == nil {
		t.Error("expected error on 500")
	}
}

func TestLangFuseClient_IngestTrace_AuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewLangFuseClient(server.URL, "bad", "creds")
	_, err := client.IngestTrace(LangFuseTrace{
		ID:   "trace-1",
		Name: "test",
	})
	if err == nil {
		t.Error("expected error on 401")
	}
}

func TestLangFuseClient_IngestTrace_ConnectionError(t *testing.T) {
	client := NewLangFuseClient("http://127.0.0.1:0", "pk", "sk")
	_, err := client.IngestTrace(LangFuseTrace{
		ID:   "trace-1",
		Name: "test",
	})
	if err == nil {
		t.Error("expected connection error")
	}
}

func TestLangFuseClient_GetTrace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/public/traces/trace-456" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"id":       "trace-456",
			"name":     "chimera-review",
			"project":  "helix",
			"input":    "review this PR",
			"output":   "APPROVE",
			"model":    "deepseek-v4-flash",
			"provider": "deepseek",
			"usage": map[string]int{
				"input":  1000,
				"output": 200,
			},
			"cost": 0.002,
		}); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	client := NewLangFuseClient(server.URL, "pk", "sk")
	trace, err := client.GetTrace("trace-456")
	if err != nil {
		t.Fatal(err)
	}
	if trace.ID != "trace-456" {
		t.Errorf("expected ID trace-456, got %s", trace.ID)
	}
	if trace.Name != "chimera-review" {
		t.Errorf("expected name chimera-review, got %s", trace.Name)
	}
	if trace.Usage.InputTokens != 1000 {
		t.Errorf("expected 1000 input tokens, got %d", trace.Usage.InputTokens)
	}
	if trace.Cost != 0.002 {
		t.Errorf("expected cost 0.002, got %f", trace.Cost)
	}
}

func TestLangFuseClient_GetTrace_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewLangFuseClient(server.URL, "pk", "sk")
	_, err := client.GetTrace("nonexistent")
	if err == nil {
		t.Error("expected error on 404")
	}
}

func TestLangFuseClient_ListTraces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("project") != "helix" {
			t.Errorf("expected project=helix, got %s", r.URL.Query().Get("project"))
		}

		if err := json.NewEncoder(w).Encode([]map[string]interface{}{
			{"id": "trace-1", "name": "review-1", "model": "gpt-5"},
			{"id": "trace-2", "name": "review-2", "model": "deepseek-v4"},
		}); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	client := NewLangFuseClient(server.URL, "pk", "sk")
	traces, err := client.ListTraces("helix", TraceFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(traces) != 2 {
		t.Fatalf("expected 2 traces, got %d", len(traces))
	}
	if traces[0].ID != "trace-1" {
		t.Errorf("expected first trace ID trace-1, got %s", traces[0].ID)
	}
}

func TestLangFuseClient_ListTraces_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode([]map[string]interface{}{}); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	client := NewLangFuseClient(server.URL, "pk", "sk")
	traces, err := client.ListTraces("helix", TraceFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(traces) != 0 {
		t.Errorf("expected 0 traces, got %d", len(traces))
	}
}

func TestLangFuseClient_Health(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/public/health" {
			t.Errorf("expected /api/public/health, got %s", r.URL.Path)
		}
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "healthy",
			"version": "2.0.0",
			"uptime":  3600.5,
		}); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	client := NewLangFuseClient(server.URL, "pk", "sk")
	health, err := client.Health()
	if err != nil {
		t.Fatal(err)
	}
	if health.Status != "healthy" {
		t.Errorf("expected status healthy, got %s", health.Status)
	}
	if health.Version != "2.0.0" {
		t.Errorf("expected version 2.0.0, got %s", health.Version)
	}
	if health.Uptime != 3600.5 {
		t.Errorf("expected uptime 3600.5, got %f", health.Uptime)
	}
}

func TestLangFuseClient_Health_Down(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewLangFuseClient(server.URL, "pk", "sk")
	health, err := client.Health()
	if err != nil {
		t.Fatal(err)
	}
	if health.Status != "down" {
		t.Errorf("expected status down, got %s", health.Status)
	}
}

func TestLangFuseClient_Health_ConnectionError(t *testing.T) {
	client := NewLangFuseClient("http://127.0.0.1:0", "pk", "sk")
	_, err := client.Health()
	if err == nil {
		t.Error("expected connection error")
	}
}

func TestLangFuseClient_WithTimeout(t *testing.T) {
	client := NewLangFuseClient("http://localhost:3000", "pk", "sk",
		WithLangFuseTimeout(5*time.Second),
	)
	if client.httpClient.Timeout != 5*time.Second {
		t.Errorf("expected 5s timeout, got %v", client.httpClient.Timeout)
	}
}

func TestLangFuseClient_WithCustomHTTPClient(t *testing.T) {
	customClient := &http.Client{Timeout: 42 * time.Second}
	client := NewLangFuseClient("http://localhost:3000", "pk", "sk",
		WithLangFuseHTTPClient(customClient),
	)
	if client.httpClient != customClient {
		t.Error("custom HTTP client not set")
	}
}

func TestParseLangFuseTrace(t *testing.T) {
	raw := map[string]interface{}{
		"id":        "test-id",
		"name":      "test-name",
		"project":   "test-project",
		"input":     "input text",
		"output":    "output text",
		"model":     "test-model",
		"provider":  "test-provider",
		"cost":      0.01,
		"timestamp": "2026-07-01T12:00:00Z",
		"usage": map[string]interface{}{
			"input":      float64(500),
			"output":     float64(100),
			"total":      float64(600),
			"cacheRead":  float64(50),
			"cacheWrite": float64(25),
		},
		"metadata": map[string]interface{}{
			"agent_id": "agent-123",
			"pr_url":   "https://example.com/pr/1",
		},
	}

	trace := parseLangFuseTrace(raw)
	if trace.ID != "test-id" {
		t.Errorf("expected ID test-id, got %s", trace.ID)
	}
	if trace.Usage.InputTokens != 500 {
		t.Errorf("expected 500 input tokens, got %d", trace.Usage.InputTokens)
	}
	if trace.Usage.TotalTokens != 600 {
		t.Errorf("expected 600 total tokens, got %d", trace.Usage.TotalTokens)
	}
	if trace.Usage.CacheReadTokens != 50 {
		t.Errorf("expected 50 cache read tokens, got %d", trace.Usage.CacheReadTokens)
	}
	if trace.Metadata["agent_id"] != "agent-123" {
		t.Errorf("expected agent_id agent-123, got %s", trace.Metadata["agent_id"])
	}
}

func TestParseLangFuseTrace_Empty(t *testing.T) {
	trace := parseLangFuseTrace(map[string]interface{}{})
	if trace.ID != "" {
		t.Error("empty map should produce empty trace")
	}
	if trace.Usage.InputTokens != 0 {
		t.Error("empty map should produce zero tokens")
	}
}
