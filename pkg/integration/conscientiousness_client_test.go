package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestConscientiousness_Evaluate_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/evaluate" {
			t.Errorf("path = %s, want /api/v1/evaluate", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}

		// Verify the request body
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["repo_owner"] != "test-org" {
			t.Errorf("repo_owner = %v", body["repo_owner"])
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "DEFENSIBLE",
			"confidence": 0.92,
			"cost":       0.15,
			"attack_vectors": []map[string]interface{}{
				{
					"description":    "No input validation on user field",
					"severity":       "medium",
					"exploitability": "difficult",
				},
			},
			"mitigations": []map[string]interface{}{
				{
					"attack_vector": "No input validation on user field",
					"mitigation":    "Add length check",
					"sufficient":    true,
				},
			},
		})
	}))
	defer server.Close()

	client := NewConscientiousnessClient(server.URL, "test-key",
		WithConscientiousnessTimeout(5*time.Second))

	pr := ConscientiousnessPR{
		RepoOwner: "test-org",
		RepoName:  "test-repo",
		PRNumber:  42,
		Diff:      "@@ diff @@",
		ChimeraVerdict: &ChimeraVerdict{
			Status:     "APPROVE",
			Confidence: 0.85,
		},
		ACs: []AcceptanceCriterion{
			{ID: "ac-1", Text: "Must handle errors", Status: "pass"},
		},
	}

	verdict, err := client.Evaluate(pr, EvalOpts{MaxIterations: 10})
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if verdict.Status != "DEFENSIBLE" {
		t.Errorf("status = %s, want DEFENSIBLE", verdict.Status)
	}
	if verdict.Confidence != 0.92 {
		t.Errorf("confidence = %.2f, want 0.92", verdict.Confidence)
	}
	if verdict.Cost != 0.15 {
		t.Errorf("cost = %.2f, want 0.15", verdict.Cost)
	}
	if len(verdict.AttackVectors) != 1 {
		t.Fatalf("expected 1 attack vector, got %d", len(verdict.AttackVectors))
	}
	av := verdict.AttackVectors[0]
	if av.Description != "No input validation on user field" {
		t.Errorf("attack vector description = %s", av.Description)
	}
	if av.Severity != "medium" {
		t.Errorf("severity = %s, want medium", av.Severity)
	}
	if av.Exploitability != "difficult" {
		t.Errorf("exploitability = %s, want difficult", av.Exploitability)
	}
	if len(verdict.Mitigations) != 1 {
		t.Fatalf("expected 1 mitigation, got %d", len(verdict.Mitigations))
	}
	if !verdict.Mitigations[0].Sufficient {
		t.Error("mitigation should be sufficient")
	}
}

func TestConscientiousness_Evaluate_AuthFailed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewConscientiousnessClient(server.URL, "bad-key")
	_, err := client.Evaluate(ConscientiousnessPR{}, EvalOpts{})
	if err == nil {
		t.Fatal("expected error for 401")
	}
}

func TestConscientiousness_Evaluate_RateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewConscientiousnessClient(server.URL, "")
	_, err := client.Evaluate(ConscientiousnessPR{}, EvalOpts{})
	if err == nil {
		t.Fatal("expected error for 429")
	}
}

func TestConscientiousness_Evaluate_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewConscientiousnessClient(server.URL, "")
	_, err := client.Evaluate(ConscientiousnessPR{}, EvalOpts{})
	if err == nil {
		t.Fatal("expected error for 500")
	}
}

func TestConscientiousness_Evaluate_ConnectionError(t *testing.T) {
	client := NewConscientiousnessClient("http://127.0.0.1:0", "")
	_, err := client.Evaluate(ConscientiousnessPR{}, EvalOpts{})
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestConscientiousness_Evaluate_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	client := NewConscientiousnessClient(server.URL, "")
	_, err := client.Evaluate(ConscientiousnessPR{}, EvalOpts{})
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestConscientiousness_Evaluate_AuthHeader(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "DEFENSIBLE",
		})
	}))
	defer server.Close()

	client := NewConscientiousnessClient(server.URL, "secret-token")
	_, _ = client.Evaluate(ConscientiousnessPR{}, EvalOpts{})

	if authHeader != "Bearer secret-token" {
		t.Errorf("auth header = %s, want 'Bearer secret-token'", authHeader)
	}
}

func TestConscientiousness_Evaluate_NoAuthWhenEmpty(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "DEFENSIBLE",
		})
	}))
	defer server.Close()

	client := NewConscientiousnessClient(server.URL, "")
	_, _ = client.Evaluate(ConscientiousnessPR{}, EvalOpts{})

	if authHeader != "" {
		t.Errorf("auth header = %s, should be empty", authHeader)
	}
}

func TestConscientiousness_Evaluate_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer server.Close()

	client := NewConscientiousnessClient(server.URL, "")
	verdict, err := client.Evaluate(ConscientiousnessPR{}, EvalOpts{})
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if verdict.Status != "" {
		t.Errorf("status should be empty, got %s", verdict.Status)
	}
	if len(verdict.AttackVectors) != 0 {
		t.Errorf("expected 0 attack vectors, got %d", len(verdict.AttackVectors))
	}
}

func TestConscientiousness_Health_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("path = %s, want /health", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "healthy",
			"version": "1.2.0",
			"uptime":  3600.0,
		})
	}))
	defer server.Close()

	client := NewConscientiousnessClient(server.URL, "")
	h, err := client.Health()
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}
	if h.Status != "healthy" {
		t.Errorf("status = %s, want healthy", h.Status)
	}
	if h.Version != "1.2.0" {
		t.Errorf("version = %s", h.Version)
	}
	if h.Uptime != 3600.0 {
		t.Errorf("uptime = %.0f", h.Uptime)
	}
}

func TestConscientiousness_Health_Down(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewConscientiousnessClient(server.URL, "")
	h, err := client.Health()
	if err != nil {
		t.Fatalf("Health should not return error for 503: %v", err)
	}
	if h.Status != "down" {
		t.Errorf("status = %s, want down", h.Status)
	}
}

func TestConscientiousness_Health_ConnectionError(t *testing.T) {
	client := NewConscientiousnessClient("http://127.0.0.1:0", "")
	_, err := client.Health()
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestConscientiousness_Health_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	client := NewConscientiousnessClient(server.URL, "")
	h, err := client.Health()
	if err != nil {
		t.Fatalf("Health should not fail on malformed JSON: %v", err)
	}
	if h.Status != "healthy" {
		t.Errorf("status = %s, want healthy (default)", h.Status)
	}
}

func TestParseConscientiousnessVerdict_Nil(t *testing.T) {
	v := parseConscientiousnessVerdict(nil)
	if v == nil {
		t.Fatal("parseConscientiousnessVerdict returned nil")
	}
	if v.Status != "" {
		t.Errorf("status should be empty")
	}
}

func TestParseConscientiousnessVerdict_MultipleFindings(t *testing.T) {
	raw := map[string]interface{}{
		"status":     "VULNERABLE",
		"confidence": 0.45,
		"cost":       0.30,
		"attack_vectors": []interface{}{
			map[string]interface{}{
				"description":    "SQL injection in /api/users",
				"severity":       "critical",
				"exploitability": "trivial",
			},
			map[string]interface{}{
				"description":    "XSS in comment field",
				"severity":       "high",
				"exploitability": "moderate",
			},
		},
		"mitigations": []interface{}{
			map[string]interface{}{
				"attack_vector": "SQL injection in /api/users",
				"mitigation":    "Parameterized queries",
				"sufficient":    true,
			},
			map[string]interface{}{
				"attack_vector": "XSS in comment field",
				"mitigation":    "",
				"sufficient":    false,
			},
		},
	}

	v := parseConscientiousnessVerdict(raw)
	if v.Status != "VULNERABLE" {
		t.Errorf("status = %s", v.Status)
	}
	if len(v.AttackVectors) != 2 {
		t.Fatalf("expected 2 attack vectors, got %d", len(v.AttackVectors))
	}
	if v.AttackVectors[0].Severity != "critical" {
		t.Errorf("first attack vector severity = %s", v.AttackVectors[0].Severity)
	}
	if len(v.Mitigations) != 2 {
		t.Fatalf("expected 2 mitigations, got %d", len(v.Mitigations))
	}
	if v.Mitigations[1].Sufficient {
		t.Error("second mitigation should not be sufficient")
	}
}
