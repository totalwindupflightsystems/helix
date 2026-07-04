package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewChimeraAdapterClient(t *testing.T) {
	c := NewChimeraAdapterClient("http://localhost:8765", "tok123")
	if c.baseURL != "http://localhost:8765" {
		t.Errorf("baseURL = %q, want http://localhost:8765", c.baseURL)
	}
	if c.token != "tok123" {
		t.Errorf("token = %q, want tok123", c.token)
	}
	if c.httpClient.Timeout != 30*time.Second {
		t.Errorf("timeout = %v, want 30s", c.httpClient.Timeout)
	}
}

func TestNewChimeraAdapterClient_Options(t *testing.T) {
	customClient := &http.Client{Timeout: 5 * time.Second}
	c := NewChimeraAdapterClient("http://localhost:8765", "",
		WithChimeraHTTPClient(customClient),
		WithChimeraTimeout(10*time.Second),
	)
	if c.httpClient != customClient {
		t.Fatal("WithChimeraHTTPClient did not set custom client")
	}
	if c.httpClient.Timeout != 10*time.Second {
		t.Errorf("timeout after WithChimeraTimeout = %v, want 10s", c.httpClient.Timeout)
	}
}

func TestChimeraClient_Review_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/deliberate" {
			t.Errorf("path = %q, want /api/v1/deliberate", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			t.Errorf("Authorization = %q, want Bearer test-token", auth)
		}

		// Verify request body contains expected fields
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["repo_name"] != "helix" {
			t.Errorf("repo_name = %v", body["repo_name"])
		}
		if body["pr_number"] != float64(42) {
			t.Errorf("pr_number = %v", body["pr_number"])
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "APPROVE",
			"confidence": 0.92,
			"summary":    "All checks passed",
			"cost":       0.15,
			"findings": []map[string]interface{}{
				{
					"severity":    "LOW",
					"category":    "style",
					"file":        "main.go",
					"line":        10,
					"description": "Minor style suggestion",
					"suggestion":  "Use camelCase",
				},
			},
			"trace": map[string]interface{}{
				"source":       "full",
				"duration":     5.3,
				"total_tokens": 15000,
				"stages": []map[string]interface{}{
					{
						"stage":    "review",
						"model":    "deepseek-v4-flash",
						"output":   "LGTM",
						"tokens":   5000,
						"duration": 2.1,
					},
				},
			},
		})
	}))
	defer srv.Close()

	c := NewChimeraAdapterClient(srv.URL, "test-token")
	pr := ChimeraPR{
		RepoName:  "helix",
		PRNumber:  42,
		Title:     "feat: add X",
		Diff:      "diff content",
		SpecFiles: []string{"specs/trust-model.md"},
	}
	verdict, err := c.Review(pr, ReviewOpts{Formation: "standard"})
	if err != nil {
		t.Fatalf("Review() error: %v", err)
	}
	if verdict.Status != "APPROVE" {
		t.Errorf("Status = %q, want APPROVE", verdict.Status)
	}
	if verdict.Confidence != 0.92 {
		t.Errorf("Confidence = %f, want 0.92", verdict.Confidence)
	}
	if verdict.Cost != 0.15 {
		t.Errorf("Cost = %f, want 0.15", verdict.Cost)
	}
	if len(verdict.Findings) != 1 {
		t.Fatalf("Findings len = %d, want 1", len(verdict.Findings))
	}
	if verdict.Findings[0].Severity != "LOW" {
		t.Errorf("Finding severity = %q", verdict.Findings[0].Severity)
	}
	if verdict.Findings[0].Line != 10 {
		t.Errorf("Finding line = %d, want 10", verdict.Findings[0].Line)
	}
	if verdict.Trace.Source != "full" {
		t.Errorf("Trace source = %q", verdict.Trace.Source)
	}
	if verdict.Trace.TotalTokens != 15000 {
		t.Errorf("Trace tokens = %d, want 15000", verdict.Trace.TotalTokens)
	}
	if len(verdict.Trace.Stages) != 1 {
		t.Fatalf("Trace stages len = %d, want 1", len(verdict.Trace.Stages))
	}
	if verdict.Trace.Stages[0].Model != "deepseek-v4-flash" {
		t.Errorf("Stage model = %q", verdict.Trace.Stages[0].Model)
	}
}

func TestChimeraClient_Review_AuthFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewChimeraAdapterClient(srv.URL, "bad-token")
	_, err := c.Review(ChimeraPR{}, ReviewOpts{})
	if err != ErrChimeraAuthFailed {
		t.Errorf("err = %v, want ErrChimeraAuthFailed", err)
	}
}

func TestChimeraClient_Review_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewChimeraAdapterClient(srv.URL, "")
	_, err := c.Review(ChimeraPR{}, ReviewOpts{})
	if err != ErrChimeraRateLimited {
		t.Errorf("err = %v, want ErrChimeraRateLimited", err)
	}
}

func TestChimeraClient_Review_BudgetExceed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer srv.Close()

	c := NewChimeraAdapterClient(srv.URL, "")
	_, err := c.Review(ChimeraPR{}, ReviewOpts{})
	if err != ErrChimeraBudgetExceed {
		t.Errorf("err = %v, want ErrChimeraBudgetExceed", err)
	}
}

func TestChimeraClient_Review_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	c := NewChimeraAdapterClient(srv.URL, "")
	_, err := c.Review(ChimeraPR{}, ReviewOpts{})
	if err == nil {
		t.Fatal("err = nil, want error")
	}
}

func TestChimeraClient_Review_ConnectionError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // immediately close to force connection error

	c := NewChimeraAdapterClient(srv.URL, "")
	_, err := c.Review(ChimeraPR{}, ReviewOpts{})
	if err == nil {
		t.Fatal("err = nil, want error")
	}
}

func TestChimeraClient_Review_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c := NewChimeraAdapterClient(srv.URL, "")
	_, err := c.Review(ChimeraPR{}, ReviewOpts{})
	if err == nil {
		t.Fatal("err = nil, want error for malformed JSON")
	}
}

func TestChimeraClient_Formations_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/formations" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{"name": "standard", "description": "3-model review", "stages": float64(3)},
			{"name": "fast", "description": "1-model review", "stages": float64(1)},
		})
	}))
	defer srv.Close()

	c := NewChimeraAdapterClient(srv.URL, "tok")
	formations, err := c.Formations()
	if err != nil {
		t.Fatalf("Formations() error: %v", err)
	}
	if len(formations) != 2 {
		t.Fatalf("len = %d, want 2", len(formations))
	}
	if formations[0].Name != "standard" {
		t.Errorf("name[0] = %q", formations[0].Name)
	}
	if formations[0].Stages != 3 {
		t.Errorf("stages[0] = %d, want 3", formations[0].Stages)
	}
	if formations[1].Name != "fast" {
		t.Errorf("name[1] = %q", formations[1].Name)
	}
}

func TestChimeraClient_Formations_AuthFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewChimeraAdapterClient(srv.URL, "")
	_, err := c.Formations()
	if err != ErrChimeraAuthFailed {
		t.Errorf("err = %v, want ErrChimeraAuthFailed", err)
	}
}

func TestChimeraClient_Formations_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{})
	}))
	defer srv.Close()

	c := NewChimeraAdapterClient(srv.URL, "")
	formations, err := c.Formations()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(formations) != 0 {
		t.Errorf("len = %d, want 0", len(formations))
	}
}

func TestChimeraClient_Formations_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewChimeraAdapterClient(srv.URL, "")
	_, err := c.Formations()
	if err == nil {
		t.Fatal("err = nil, want error")
	}
}

func TestChimeraClient_Formations_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewChimeraAdapterClient(srv.URL, "")
	_, err := c.Formations()
	if err != ErrChimeraRateLimited {
		t.Errorf("err = %v, want ErrChimeraRateLimited", err)
	}
}

func TestChimeraClient_Formations_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{not valid json`))
	}))
	defer srv.Close()

	c := NewChimeraAdapterClient(srv.URL, "")
	_, err := c.Formations()
	if err == nil {
		t.Fatal("err = nil, want malformed-JSON error")
	}
}

func TestChimeraClient_Models_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewChimeraAdapterClient(srv.URL, "")
	_, err := c.Models()
	if err != ErrChimeraRateLimited {
		t.Errorf("err = %v, want ErrChimeraRateLimited", err)
	}
}

func TestChimeraClient_Models_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`garbage payload`))
	}))
	defer srv.Close()

	c := NewChimeraAdapterClient(srv.URL, "")
	_, err := c.Models()
	if err == nil {
		t.Fatal("err = nil, want malformed-JSON error")
	}
}

func TestChimeraClient_Models_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{"name": "deepseek-v4-flash", "category": "reasoning", "weight": 0.9},
			{"name": "minimax-m3", "category": "coding", "weight": 0.85},
		})
	}))
	defer srv.Close()

	c := NewChimeraAdapterClient(srv.URL, "tok")
	models, err := c.Models()
	if err != nil {
		t.Fatalf("Models() error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("len = %d, want 2", len(models))
	}
	if models[0].Name != "deepseek-v4-flash" {
		t.Errorf("name[0] = %q", models[0].Name)
	}
	if models[0].Category != "reasoning" {
		t.Errorf("category[0] = %q", models[0].Category)
	}
	if models[0].Weight != 0.9 {
		t.Errorf("weight[0] = %f", models[0].Weight)
	}
	if models[1].Name != "minimax-m3" {
		t.Errorf("name[1] = %q", models[1].Name)
	}
}

func TestChimeraClient_Models_AuthFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewChimeraAdapterClient(srv.URL, "")
	_, err := c.Models()
	if err != ErrChimeraAuthFailed {
		t.Errorf("err = %v, want ErrChimeraAuthFailed", err)
	}
}

func TestChimeraClient_Models_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{})
	}))
	defer srv.Close()

	c := NewChimeraAdapterClient(srv.URL, "")
	models, err := c.Models()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(models) != 0 {
		t.Errorf("len = %d, want 0", len(models))
	}
}

func TestChimeraClient_Models_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewChimeraAdapterClient(srv.URL, "")
	_, err := c.Models()
	if err == nil {
		t.Fatal("err = nil, want error")
	}
}

func TestChimeraClient_Health_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/health" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "healthy",
			"version": "1.2.0",
			"uptime":  3600.5,
			"models":  float64(35),
		})
	}))
	defer srv.Close()

	c := NewChimeraAdapterClient(srv.URL, "")
	h, err := c.Health()
	if err != nil {
		t.Fatalf("Health() error: %v", err)
	}
	if h.Status != "healthy" {
		t.Errorf("Status = %q", h.Status)
	}
	if h.Version != "1.2.0" {
		t.Errorf("Version = %q", h.Version)
	}
	if h.Uptime != 3600.5 {
		t.Errorf("Uptime = %f", h.Uptime)
	}
	if h.Models != 35 {
		t.Errorf("Models = %d, want 35", h.Models)
	}
}

func TestChimeraClient_Health_Down(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewChimeraAdapterClient(srv.URL, "")
	h, err := c.Health()
	if err != nil {
		t.Fatalf("Health() error: %v", err)
	}
	if h.Status != "down" {
		t.Errorf("Status = %q, want down", h.Status)
	}
}

func TestChimeraClient_Health_ConnectionError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	c := NewChimeraAdapterClient(srv.URL, "")
	_, err := c.Health()
	if err == nil {
		t.Fatal("err = nil, want error")
	}
}

func TestChimeraClient_Health_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := NewChimeraAdapterClient(srv.URL, "")
	h, err := c.Health()
	if err != nil {
		t.Fatalf("Health() error: %v", err)
	}
	// Malformed JSON still returns healthy since 2xx status code
	if h.Status != "healthy" {
		t.Errorf("Status = %q, want healthy", h.Status)
	}
}

func TestParseChimeraVerdict_Empty(t *testing.T) {
	v := parseChimeraVerdict(map[string]interface{}{})
	if v.Status != "" {
		t.Errorf("Status = %q, want empty", v.Status)
	}
	if len(v.Findings) != 0 {
		t.Errorf("Findings len = %d, want 0", len(v.Findings))
	}
}

func TestParseChimeraVerdict_NilFindings(t *testing.T) {
	v := parseChimeraVerdict(map[string]interface{}{
		"status":   "REJECT",
		"findings": nil,
	})
	if v.Status != "REJECT" {
		t.Errorf("Status = %q", v.Status)
	}
	if len(v.Findings) != 0 {
		t.Errorf("Findings len = %d, want 0", len(v.Findings))
	}
}

func TestParseChimeraVerdict_MultipleFindings(t *testing.T) {
	raw := map[string]interface{}{
		"status": "REJECT",
		"findings": []interface{}{
			map[string]interface{}{
				"severity":    "CRITICAL",
				"category":    "security",
				"file":        "auth.go",
				"line":        float64(42),
				"description": "SQL injection vulnerability",
			},
			map[string]interface{}{
				"severity":    "HIGH",
				"category":    "logic",
				"file":        "handler.go",
				"line":        float64(100),
				"description": "Missing nil check",
			},
		},
	}
	v := parseChimeraVerdict(raw)
	if len(v.Findings) != 2 {
		t.Fatalf("Findings len = %d, want 2", len(v.Findings))
	}
	if v.Findings[0].Severity != "CRITICAL" {
		t.Errorf("Finding[0] severity = %q", v.Findings[0].Severity)
	}
	if v.Findings[1].Line != 100 {
		t.Errorf("Finding[1] line = %d", v.Findings[1].Line)
	}
}

func TestSerializeAgentReviews(t *testing.T) {
	reviews := []AgentReview{
		{AgentName: "agent-a", Verdict: "APPROVED", Body: "LGTM", Evidence: []string{"test1"}, TrustLevel: 85},
		{AgentName: "agent-b", Verdict: "REQUEST_CHANGES", Body: "Needs work", TrustLevel: 70},
	}
	result := serializeAgentReviews(reviews)
	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	if result[0]["agent_name"] != "agent-a" {
		t.Errorf("agent_name[0] = %v", result[0]["agent_name"])
	}
	if result[0]["trust_level"] != 85 {
		t.Errorf("trust_level[0] = %v", result[0]["trust_level"])
	}
	if result[1]["verdict"] != "REQUEST_CHANGES" {
		t.Errorf("verdict[1] = %v", result[1]["verdict"])
	}
}

func TestSerializeAgentReviews_Empty(t *testing.T) {
	result := serializeAgentReviews(nil)
	if len(result) != 0 {
		t.Errorf("len = %d, want 0", len(result))
	}
}

func TestChimeraClient_Review_WithAgentReviews(t *testing.T) {
	var receivedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "APPROVE",
			"confidence": 0.9,
		})
	}))
	defer srv.Close()

	c := NewChimeraAdapterClient(srv.URL, "")
	pr := ChimeraPR{
		RepoName: "helix",
		PRNumber: 1,
		AgentReviews: []AgentReview{
			{AgentName: "reviewer-1", Verdict: "APPROVED", TrustLevel: 90},
		},
	}
	_, err := c.Review(pr, ReviewOpts{Formation: "fast", MaxBudget: 5.0, AllowCustomDAG: true})
	if err != nil {
		t.Fatalf("Review() error: %v", err)
	}
	if receivedBody["formation"] != "fast" {
		t.Errorf("formation = %v", receivedBody["formation"])
	}
	if receivedBody["max_budget"] != float64(5.0) {
		t.Errorf("max_budget = %v", receivedBody["max_budget"])
	}
	if receivedBody["allow_custom_dag"] != true {
		t.Errorf("allow_custom_dag = %v", receivedBody["allow_custom_dag"])
	}
	reviews, ok := receivedBody["agent_reviews"].([]interface{})
	if !ok || len(reviews) != 1 {
		t.Fatalf("agent_reviews = %v", receivedBody["agent_reviews"])
	}
}
