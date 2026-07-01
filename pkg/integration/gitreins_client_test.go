package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewGitReinsAdapterClient(t *testing.T) {
	c := NewGitReinsAdapterClient("http://localhost:8080", "key123")
	if c.baseURL != "http://localhost:8080" {
		t.Errorf("baseURL = %q", c.baseURL)
	}
	if c.apiKey != "key123" {
		t.Errorf("apiKey = %q", c.apiKey)
	}
	if c.httpClient.Timeout != 120*time.Second {
		t.Errorf("timeout = %v, want 120s", c.httpClient.Timeout)
	}
	// Default pricing
	if c.pricing.inputPerMillion != 1.00 {
		t.Errorf("input pricing = %f, want 1.00", c.pricing.inputPerMillion)
	}
}

func TestNewGitReinsAdapterClient_Options(t *testing.T) {
	customClient := &http.Client{Timeout: 5 * time.Second}
	c := NewGitReinsAdapterClient("http://localhost:8080", "",
		WithGitReinsHTTPClient(customClient),
		WithGitReinsTimeout(30*time.Second),
		WithGitReinsPricing(2.0, 6.0, 0.2, 2.5),
	)
	if c.httpClient != customClient {
		t.Fatal("WithGitReinsHTTPClient did not set custom client")
	}
	if c.httpClient.Timeout != 30*time.Second {
		t.Errorf("timeout = %v, want 30s", c.httpClient.Timeout)
	}
	if c.pricing.inputPerMillion != 2.0 {
		t.Errorf("input pricing = %f, want 2.0", c.pricing.inputPerMillion)
	}
	if c.pricing.outputPerMillion != 6.0 {
		t.Errorf("output pricing = %f, want 6.0", c.pricing.outputPerMillion)
	}
}

func TestGitReinsClient_Guard_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/guard" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("Authorization = %q", auth)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["workdir"] != "/repo/helix" {
			t.Errorf("workdir = %v", body["workdir"])
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"passed":   true,
			"duration": 1.5,
			"checks": map[string]interface{}{
				"secrets": map[string]interface{}{"passed": true, "output": "no leaks", "duration": 0.1},
				"lint":    map[string]interface{}{"passed": true, "output": "clean", "duration": 0.5},
				"tests":   map[string]interface{}{"passed": true, "output": "25/25 pass", "duration": 0.8},
				"build":   map[string]interface{}{"passed": true, "output": "ok", "duration": 0.1},
			},
		})
	}))
	defer srv.Close()

	c := NewGitReinsAdapterClient(srv.URL, "test-key")
	result, err := c.Guard("/repo/helix", GuardOpts{})
	if err != nil {
		t.Fatalf("Guard() error: %v", err)
	}
	if !result.Passed {
		t.Error("Passed = false, want true")
	}
	if result.Duration != 1.5 {
		t.Errorf("Duration = %f, want 1.5", result.Duration)
	}
	if len(result.Checks) != 4 {
		t.Fatalf("Checks len = %d, want 4", len(result.Checks))
	}
	if !result.Checks["secrets"].Passed {
		t.Error("secrets check failed")
	}
	if result.Checks["tests"].Output != "25/25 pass" {
		t.Errorf("tests output = %q", result.Checks["tests"].Output)
	}
}

func TestGitReinsClient_Guard_AuthFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewGitReinsAdapterClient(srv.URL, "bad")
	_, err := c.Guard("/repo", GuardOpts{})
	if err != ErrGitReinsAuthFailed {
		t.Errorf("err = %v, want ErrGitReinsAuthFailed", err)
	}
}

func TestGitReinsClient_Guard_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewGitReinsAdapterClient(srv.URL, "")
	_, err := c.Guard("/repo", GuardOpts{})
	if err != ErrGitReinsRateLimited {
		t.Errorf("err = %v, want ErrGitReinsRateLimited", err)
	}
}

func TestGitReinsClient_Guard_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	c := NewGitReinsAdapterClient(srv.URL, "")
	_, err := c.Guard("/repo", GuardOpts{})
	if err == nil {
		t.Fatal("err = nil, want error")
	}
}

func TestGitReinsClient_Guard_ConnectionError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	c := NewGitReinsAdapterClient(srv.URL, "")
	_, err := c.Guard("/repo", GuardOpts{})
	if err == nil {
		t.Fatal("err = nil, want error")
	}
}

func TestGitReinsClient_Guard_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c := NewGitReinsAdapterClient(srv.URL, "")
	_, err := c.Guard("/repo", GuardOpts{})
	if err == nil {
		t.Fatal("err = nil, want error")
	}
}

func TestGitReinsClient_Evaluate_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/evaluate" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q", r.Method)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["workdir"] != "/repo/helix" {
			t.Errorf("workdir = %v", body["workdir"])
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"passed":   true,
			"duration": 30.5,
			"verdicts": map[string]interface{}{
				"criterion-1": map[string]interface{}{"status": "PASS", "reason": "found at line 5", "score": 1.0},
				"criterion-2": map[string]interface{}{"status": "PASS", "reason": "all tests pass", "score": 0.95},
			},
			"evidence": []interface{}{
				map[string]interface{}{"type": "test_output", "content": "PASS", "source": "handler_test.go"},
				map[string]interface{}{"type": "file_content", "content": "func main()", "source": "main.go"},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":      float64(5000),
				"completion_tokens":  float64(1000),
				"total_tokens":       float64(6000),
				"cache_read_tokens":  float64(3000),
				"cache_write_tokens": float64(500),
			},
		})
	}))
	defer srv.Close()

	c := NewGitReinsAdapterClient(srv.URL, "key")
	result, err := c.Evaluate("/repo/helix", "diff content", EvalOpts{
		Criteria: []string{"criterion-1", "criterion-2"},
	})
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if !result.Passed {
		t.Error("Passed = false, want true")
	}
	if len(result.Verdicts) != 2 {
		t.Fatalf("Verdicts len = %d, want 2", len(result.Verdicts))
	}
	if result.Verdicts["criterion-1"].Status != "PASS" {
		t.Errorf("criterion-1 status = %q", result.Verdicts["criterion-1"].Status)
	}
	if len(result.Evidence) != 2 {
		t.Fatalf("Evidence len = %d, want 2", len(result.Evidence))
	}
	if result.Evidence[0].Type != "test_output" {
		t.Errorf("evidence[0] type = %q", result.Evidence[0].Type)
	}
	if result.Usage.PromptTokens != 5000 {
		t.Errorf("PromptTokens = %d, want 5000", result.Usage.PromptTokens)
	}
	if result.Usage.CacheReadTokens != 3000 {
		t.Errorf("CacheReadTokens = %d, want 3000", result.Usage.CacheReadTokens)
	}
}

func TestGitReinsClient_Evaluate_AuthFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewGitReinsAdapterClient(srv.URL, "bad")
	_, err := c.Evaluate("/repo", "diff", EvalOpts{})
	if err != ErrGitReinsAuthFailed {
		t.Errorf("err = %v, want ErrGitReinsAuthFailed", err)
	}
}

func TestGitReinsClient_Evaluate_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer srv.Close()

	c := NewGitReinsAdapterClient(srv.URL, "")
	_, err := c.Evaluate("/repo", "diff", EvalOpts{})
	if err != ErrGitReinsEvalTimeout {
		t.Errorf("err = %v, want ErrGitReinsEvalTimeout", err)
	}
}

func TestGitReinsClient_Evaluate_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewGitReinsAdapterClient(srv.URL, "")
	_, err := c.Evaluate("/repo", "diff", EvalOpts{})
	if err != ErrGitReinsRateLimited {
		t.Errorf("err = %v, want ErrGitReinsRateLimited", err)
	}
}

func TestGitReinsClient_Evaluate_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewGitReinsAdapterClient(srv.URL, "")
	_, err := c.Evaluate("/repo", "diff", EvalOpts{})
	if err == nil {
		t.Fatal("err = nil, want error")
	}
}

func TestGitReinsClient_Evaluate_ConnectionError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	c := NewGitReinsAdapterClient(srv.URL, "")
	_, err := c.Evaluate("/repo", "diff", EvalOpts{})
	if err == nil {
		t.Fatal("err = nil, want error")
	}
}

func TestGitReinsClient_Cost(t *testing.T) {
	c := NewGitReinsAdapterClient("http://localhost:8080", "key",
		WithGitReinsPricing(1.0, 2.0, 0.1, 1.25),
	)
	evalResult := &EvalResult{
		Usage: LLMUsage{
			PromptTokens:     5000,
			CompletionTokens: 1000,
			TotalTokens:      6000,
			CacheReadTokens:  3000,
			CacheWriteTokens: 500,
		},
	}
	breakdown := c.Cost(evalResult)

	// fresh_input = (5000 - 3000) / 1e6 * 1.0 = 0.002
	if breakdown.FreshInputCost < 0.0019 || breakdown.FreshInputCost > 0.0021 {
		t.Errorf("FreshInputCost = %f, want ~0.002", breakdown.FreshInputCost)
	}
	// cache_hit = 3000 / 1e6 * 0.1 = 0.0003
	if breakdown.CacheHitCost < 0.0002 || breakdown.CacheHitCost > 0.0004 {
		t.Errorf("CacheHitCost = %f, want ~0.0003", breakdown.CacheHitCost)
	}
	// cache_write = 500 / 1e6 * 1.25 = 0.000625
	if breakdown.CacheWriteCost < 0.0005 || breakdown.CacheWriteCost > 0.0008 {
		t.Errorf("CacheWriteCost = %f, want ~0.000625", breakdown.CacheWriteCost)
	}
	// output = 1000 / 1e6 * 2.0 = 0.002
	if breakdown.OutputCost < 0.0019 || breakdown.OutputCost > 0.0021 {
		t.Errorf("OutputCost = %f, want ~0.002", breakdown.OutputCost)
	}
	// total ≈ 0.002 + 0.0003 + 0.000625 + 0.002 ≈ 0.004925
	expectedTotal := breakdown.FreshInputCost + breakdown.CacheHitCost + breakdown.CacheWriteCost + breakdown.OutputCost
	if breakdown.TotalCost < expectedTotal-0.0001 || breakdown.TotalCost > expectedTotal+0.0001 {
		t.Errorf("TotalCost = %f, want ~%f", breakdown.TotalCost, expectedTotal)
	}
}

func TestGitReinsClient_Cost_NilEvalResult(t *testing.T) {
	c := NewGitReinsAdapterClient("http://localhost:8080", "")
	breakdown := c.Cost(nil)
	if breakdown.TotalCost != 0 {
		t.Errorf("TotalCost = %f, want 0", breakdown.TotalCost)
	}
}

func TestGitReinsClient_Cost_ZeroTokens(t *testing.T) {
	c := NewGitReinsAdapterClient("http://localhost:8080", "")
	breakdown := c.Cost(&EvalResult{Usage: LLMUsage{}})
	if breakdown.TotalCost != 0 {
		t.Errorf("TotalCost = %f, want 0", breakdown.TotalCost)
	}
}

func TestGitReinsClient_Health_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/health" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewGitReinsAdapterClient(srv.URL, "key")
	if err := c.Health(); err != nil {
		t.Errorf("Health() error: %v", err)
	}
}

func TestGitReinsClient_Health_ConnectionError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	c := NewGitReinsAdapterClient(srv.URL, "")
	if err := c.Health(); err == nil {
		t.Fatal("Health() err = nil, want error")
	}
}

func TestGitReinsClient_Health_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewGitReinsAdapterClient(srv.URL, "")
	if err := c.Health(); err == nil {
		t.Fatal("Health() err = nil, want error for 503")
	}
}

func TestParseGuardResult_Empty(t *testing.T) {
	result := parseGuardResult(map[string]interface{}{})
	if result.Passed {
		t.Error("Passed = true, want false")
	}
	if len(result.Checks) != 0 {
		t.Errorf("Checks len = %d, want 0", len(result.Checks))
	}
}

func TestParseEvalResult_Empty(t *testing.T) {
	result := parseEvalResult(map[string]interface{}{})
	if result.Passed {
		t.Error("Passed = true, want false")
	}
	if len(result.Verdicts) != 0 {
		t.Errorf("Verdicts len = %d, want 0", len(result.Verdicts))
	}
}

func TestParseEvalResult_NilVerdicts(t *testing.T) {
	result := parseEvalResult(map[string]interface{}{
		"passed":   true,
		"verdicts": nil,
	})
	if !result.Passed {
		t.Error("Passed = false, want true")
	}
	if len(result.Verdicts) != 0 {
		t.Errorf("Verdicts len = %d, want 0", len(result.Verdicts))
	}
}
