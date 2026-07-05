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

func TestLangFuseClient_IngestTrace_Spec82_FullTrace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}

		// Verify spec §8.2 fields
		if body["userId"] != "agent-sandbox-7@helix" {
			t.Errorf("expected userId agent-sandbox-7@helix, got %v", body["userId"])
		}
		if body["sessionId"] != "pr-1842" {
			t.Errorf("expected sessionId pr-1842, got %v", body["sessionId"])
		}

		tags, ok := body["tags"].([]interface{})
		if !ok || len(tags) != 2 {
			t.Fatalf("expected 2 tags, got %v", body["tags"])
		}
		if tags[0] != "implementation" || tags[1] != "go" {
			t.Errorf("expected [implementation, go], got %v", tags)
		}

		gens, ok := body["generations"].([]interface{})
		if !ok || len(gens) != 1 {
			t.Fatalf("expected 1 generation, got %v", body["generations"])
		}
		gen := gens[0].(map[string]interface{})
		if gen["name"] != "llm-call-1" {
			t.Errorf("expected generation name llm-call-1, got %v", gen["name"])
		}
		if gen["model"] != "deepseek-v4-pro" {
			t.Errorf("expected model deepseek-v4-pro, got %v", gen["model"])
		}
		genUsage := gen["usage"].(map[string]interface{})
		if genUsage["promptTokens"] != float64(45000) {
			t.Errorf("expected promptTokens 45000, got %v", genUsage["promptTokens"])
		}
		if gen["cost"] != 0.1064 {
			t.Errorf("expected cost 0.1064, got %v", gen["cost"])
		}
		if gen["duration_ms"] != float64(34200) {
			t.Errorf("expected duration_ms 34200, got %v", gen["duration_ms"])
		}

		obs, ok := body["observations"].([]interface{})
		if !ok || len(obs) != 1 {
			t.Fatalf("expected 1 observation, got %v", body["observations"])
		}
		ob := obs[0].(map[string]interface{})
		if ob["name"] != "file-write" {
			t.Errorf("expected observation name file-write, got %v", ob["name"])
		}
		if ob["type"] != "SPAN" {
			t.Errorf("expected type SPAN, got %v", ob["type"])
		}
		if ob["duration_ms"] != float64(120) {
			t.Errorf("expected duration_ms 120, got %v", ob["duration_ms"])
		}

		if err := json.NewEncoder(w).Encode(map[string]string{
			"id":     "trace-spec82",
			"status": "accepted",
		}); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	client := NewLangFuseClient(server.URL, "pk-test", "sk-test")
	result, err := client.IngestTrace(LangFuseTrace{
		ID:        "trace-spec82",
		Name:      "agent-implement",
		Project:   "helix",
		UserID:    "agent-sandbox-7@helix",
		SessionID: "pr-1842",
		Model:     "deepseek-v4-pro",
		Tags:      []string{"implementation", "go"},
		Generations: []LangFuseGeneration{
			{
				Name:       "llm-call-1",
				Model:      "deepseek-v4-pro",
				Input:      "...",
				Output:     "...",
				Usage:      LangFuseUsage{InputTokens: 45000, OutputTokens: 8200, TotalTokens: 53200},
				Cost:       0.1064,
				DurationMs: 34200,
			},
		},
		Observations: []LangFuseObservation{
			{
				Name:       "file-write",
				Type:       "SPAN",
				Input:      "pkg/identity/provisioner.go",
				Output:     "182 lines written",
				DurationMs: 120,
			},
		},
		Metadata: map[string]string{
			"repo":           "totalwindupflightsystems/helix",
			"pr":             "1842",
			"prompt_version": "agent-identity/v3",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "accepted" {
		t.Errorf("expected status accepted, got %s", result.Status)
	}
}

func TestParseLangFuseTrace_Spec82_FullTrace(t *testing.T) {
	raw := map[string]interface{}{
		"id":        "helix-pr-1842-agent-7",
		"name":      "agent-implement",
		"userId":    "agent-sandbox-7@helix",
		"sessionId": "pr-1842",
		"model":     "deepseek-v4-pro",
		"tags":      []interface{}{"implementation", "go", "agent-identity"},
		"generations": []interface{}{
			map[string]interface{}{
				"name":  "llm-call-1",
				"model": "deepseek-v4-pro",
				"input": "long prompt...",
				"usage": map[string]interface{}{
					"promptTokens":     float64(45000),
					"completionTokens": float64(8200),
					"totalTokens":      float64(53200),
				},
				"cost":        float64(0.1064),
				"duration_ms": float64(34200),
			},
		},
		"observations": []interface{}{
			map[string]interface{}{
				"name":        "file-write",
				"type":        "SPAN",
				"input":       "pkg/identity/provisioner.go",
				"output":      "182 lines written",
				"duration_ms": float64(120),
			},
		},
		"metadata": map[string]interface{}{
			"repo":           "totalwindupflightsystems/helix",
			"pr":             "1842",
			"prompt_version": "agent-identity/v3",
			"context_window": "131072",
		},
	}

	trace := parseLangFuseTrace(raw)

	if trace.UserID != "agent-sandbox-7@helix" {
		t.Errorf("expected UserID agent-sandbox-7@helix, got %s", trace.UserID)
	}
	if trace.SessionID != "pr-1842" {
		t.Errorf("expected SessionID pr-1842, got %s", trace.SessionID)
	}
	if len(trace.Tags) != 3 {
		t.Fatalf("expected 3 tags, got %d", len(trace.Tags))
	}
	if trace.Tags[0] != "implementation" {
		t.Errorf("expected first tag implementation, got %s", trace.Tags[0])
	}
	if len(trace.Generations) != 1 {
		t.Fatalf("expected 1 generation, got %d", len(trace.Generations))
	}
	gen := trace.Generations[0]
	if gen.Name != "llm-call-1" {
		t.Errorf("expected gen name llm-call-1, got %s", gen.Name)
	}
	if gen.Usage.InputTokens != 45000 {
		t.Errorf("expected promptTokens 45000, got %d", gen.Usage.InputTokens)
	}
	if gen.Usage.OutputTokens != 8200 {
		t.Errorf("expected completionTokens 8200, got %d", gen.Usage.OutputTokens)
	}
	if gen.Cost != 0.1064 {
		t.Errorf("expected cost 0.1064, got %f", gen.Cost)
	}
	if gen.DurationMs != 34200 {
		t.Errorf("expected duration_ms 34200, got %d", gen.DurationMs)
	}
	if len(trace.Observations) != 1 {
		t.Fatalf("expected 1 observation, got %d", len(trace.Observations))
	}
	ob := trace.Observations[0]
	if ob.Name != "file-write" {
		t.Errorf("expected obs name file-write, got %s", ob.Name)
	}
	if ob.Type != "SPAN" {
		t.Errorf("expected type SPAN, got %s", ob.Type)
	}
	if ob.DurationMs != 120 {
		t.Errorf("expected duration_ms 120, got %d", ob.DurationMs)
	}
	if trace.Metadata["context_window"] != "131072" {
		t.Errorf("expected context_window 131072, got %s", trace.Metadata["context_window"])
	}
}

func TestParseLangFuseTrace_Spec82_EmptyArrays(t *testing.T) {
	raw := map[string]interface{}{
		"id":           "trace-empty",
		"tags":         []interface{}{},
		"generations":  []interface{}{},
		"observations": []interface{}{},
	}
	trace := parseLangFuseTrace(raw)
	if len(trace.Tags) != 0 {
		t.Errorf("expected 0 tags, got %d", len(trace.Tags))
	}
	if len(trace.Generations) != 0 {
		t.Errorf("expected 0 generations, got %d", len(trace.Generations))
	}
	if len(trace.Observations) != 0 {
		t.Errorf("expected 0 observations, got %d", len(trace.Observations))
	}
}

func TestLangFuseGeneration_Observation_Types(t *testing.T) {
	gen := LangFuseGeneration{
		Name:       "gen-1",
		Model:      "test-model",
		Usage:      LangFuseUsage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
		Cost:       0.01,
		DurationMs: 5000,
	}
	if gen.Name != "gen-1" {
		t.Error("generation name mismatch")
	}
	if gen.Usage.TotalTokens != 150 {
		t.Error("usage mismatch")
	}

	ob := LangFuseObservation{
		Name:       "file-read",
		Type:       "EVENT",
		Input:      "main.go",
		Output:     "200 lines",
		DurationMs: 15,
	}
	if ob.Type != "EVENT" {
		t.Error("observation type mismatch")
	}
	if ob.DurationMs != 15 {
		t.Error("duration mismatch")
	}
}
