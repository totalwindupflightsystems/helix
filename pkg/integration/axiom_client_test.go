package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewAxiomClient(t *testing.T) {
	c := NewAxiomClient("http://localhost:8080", "tok123")
	if c.baseURL != "http://localhost:8080" {
		t.Errorf("baseURL = %q, want http://localhost:8080", c.baseURL)
	}
	if c.token != "tok123" {
		t.Errorf("token = %q, want tok123", c.token)
	}
	if c.httpClient.Timeout != 60*time.Second {
		t.Errorf("timeout = %v, want 60s", c.httpClient.Timeout)
	}
}

func TestNewAxiomClient_Options(t *testing.T) {
	customClient := &http.Client{Timeout: 5 * time.Second}
	c := NewAxiomClient("http://localhost:8080", "",
		WithAxiomHTTPClient(customClient),
		WithAxiomTimeout(10*time.Second),
	)
	if c.httpClient != customClient {
		t.Fatal("WithAxiomHTTPClient did not set custom client")
	}
	if c.httpClient.Timeout != 10*time.Second {
		t.Errorf("timeout = %v, want 10s", c.httpClient.Timeout)
	}
}

func TestAxiomClient_Run_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/run" {
			t.Errorf("path = %q, want /api/v1/run", r.URL.Path)
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
		if body["intent"] != "add login feature" {
			t.Errorf("intent = %v", body["intent"])
		}
		if body["repo_path"] != "/workspace/helix" {
			t.Errorf("repo_path = %v", body["repo_path"])
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"work_item_id": "wi-001",
			"status":       "complete",
			"confidence":   0.85,
			"evidence":     "/workspace/evidence/wi-001.json",
			"pr":           "http://forgejo:3000/helix/helix/pulls/5",
			"cost":         2.50,
			"duration":     120.5,
		})
	}))
	defer srv.Close()

	c := NewAxiomClient(srv.URL, "test-token")
	result, err := c.Run("add login feature", "/workspace/helix", RunOpts{InProcess: true, Yes: true})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result.WorkItemID != "wi-001" {
		t.Errorf("WorkItemID = %q", result.WorkItemID)
	}
	if result.Status != "complete" {
		t.Errorf("Status = %q", result.Status)
	}
	if result.Confidence != 0.85 {
		t.Errorf("Confidence = %f", result.Confidence)
	}
	if result.PR != "http://forgejo:3000/helix/helix/pulls/5" {
		t.Errorf("PR = %q", result.PR)
	}
	if result.Cost != 2.50 {
		t.Errorf("Cost = %f", result.Cost)
	}
}

func TestAxiomClient_Run_AuthFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewAxiomClient(srv.URL, "")
	_, err := c.Run("intent", "/repo", RunOpts{})
	if err != ErrAxiomAuthFailed {
		t.Errorf("error = %v, want ErrAxiomAuthFailed", err)
	}
}

func TestAxiomClient_Run_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewAxiomClient(srv.URL, "")
	_, err := c.Run("intent", "/repo", RunOpts{})
	if err != ErrAxiomRateLimited {
		t.Errorf("error = %v, want ErrAxiomRateLimited", err)
	}
}

func TestAxiomClient_Run_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	c := NewAxiomClient(srv.URL, "")
	_, err := c.Run("intent", "/repo", RunOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAxiomClient_Run_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{bad"))
	}))
	defer srv.Close()

	c := NewAxiomClient(srv.URL, "")
	_, err := c.Run("intent", "/repo", RunOpts{})
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestAxiomClient_Cmd_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/cmd" {
			t.Errorf("path = %q, want /api/v1/cmd", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer tok" {
			t.Errorf("Authorization = %q, want Bearer tok", auth)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["command"] != "/axiom-step" {
			t.Errorf("command = %v", body["command"])
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "ok",
			"output":   "step completed",
			"duration": 5.2,
		})
	}))
	defer srv.Close()

	c := NewAxiomClient(srv.URL, "tok")
	result, err := c.Cmd("/axiom-step", "/repo")
	if err != nil {
		t.Fatalf("Cmd() error: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("Status = %q", result.Status)
	}
	if result.Output != "step completed" {
		t.Errorf("Output = %q", result.Output)
	}
	if result.Duration != 5.2 {
		t.Errorf("Duration = %f", result.Duration)
	}
}

func TestAxiomClient_Cmd_AuthFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewAxiomClient(srv.URL, "")
	_, err := c.Cmd("cmd", "/repo")
	if err != ErrAxiomAuthFailed {
		t.Errorf("error = %v, want ErrAxiomAuthFailed", err)
	}
}

func TestAxiomClient_Cmd_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewAxiomClient(srv.URL, "")
	_, err := c.Cmd("cmd", "/repo")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAxiomClient_Status_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/status" {
			t.Errorf("path = %q, want /api/v1/status", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"active_runs":   2,
			"queued_items":  5,
			"current_phase": "review",
			"blocked_items": []string{"wi-003", "wi-007"},
		})
	}))
	defer srv.Close()

	c := NewAxiomClient(srv.URL, "")
	status, err := c.Status("/repo")
	if err != nil {
		t.Fatalf("Status() error: %v", err)
	}
	if status.ActiveRuns != 2 {
		t.Errorf("ActiveRuns = %d", status.ActiveRuns)
	}
	if status.QueuedItems != 5 {
		t.Errorf("QueuedItems = %d", status.QueuedItems)
	}
	if status.CurrentPhase != "review" {
		t.Errorf("CurrentPhase = %q", status.CurrentPhase)
	}
	if len(status.BlockedItems) != 2 {
		t.Errorf("BlockedItems len = %d", len(status.BlockedItems))
	}
}

func TestAxiomClient_Status_AuthFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewAxiomClient(srv.URL, "")
	_, err := c.Status("/repo")
	if err != ErrAxiomAuthFailed {
		t.Errorf("error = %v, want ErrAxiomAuthFailed", err)
	}
}

func TestAxiomClient_Status_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewAxiomClient(srv.URL, "")
	_, err := c.Status("/repo")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAxiomClient_ListWorkItems_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/work-items" {
			t.Errorf("path = %q, want /api/v1/work-items", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}

		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"id":         "wi-001",
				"title":      "Add auth",
				"status":     "complete",
				"priority":   "high",
				"assignee":   "agent-1",
				"confidence": 0.92,
			},
			{
				"id":     "wi-002",
				"title":  "Fix tests",
				"status": "pending",
			},
		})
	}))
	defer srv.Close()

	c := NewAxiomClient(srv.URL, "")
	items, err := c.ListWorkItems("/repo")
	if err != nil {
		t.Fatalf("ListWorkItems() error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items len = %d, want 2", len(items))
	}
	if items[0].ID != "wi-001" {
		t.Errorf("items[0].ID = %q", items[0].ID)
	}
	if items[0].Confidence != 0.92 {
		t.Errorf("items[0].Confidence = %f", items[0].Confidence)
	}
	if items[1].Status != "pending" {
		t.Errorf("items[1].Status = %q", items[1].Status)
	}
}

func TestAxiomClient_ListWorkItems_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	c := NewAxiomClient(srv.URL, "")
	items, err := c.ListWorkItems("/repo")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("items len = %d, want 0", len(items))
	}
}

func TestAxiomClient_ListWorkItems_AuthFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewAxiomClient(srv.URL, "")
	_, err := c.ListWorkItems("/repo")
	if err != ErrAxiomAuthFailed {
		t.Errorf("error = %v, want ErrAxiomAuthFailed", err)
	}
}

func TestAxiomClient_Health_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/health" {
			t.Errorf("path = %q, want /api/v1/health", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewAxiomClient(srv.URL, "")
	if err := c.Health(); err != nil {
		t.Fatalf("Health() error: %v", err)
	}
}

func TestAxiomClient_Health_Down(t *testing.T) {
	c := NewAxiomClient("http://127.0.0.1:0", "")
	if err := c.Health(); err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

func TestAxiomClient_Health_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewAxiomClient(srv.URL, "")
	if err := c.Health(); err == nil {
		t.Fatal("expected error for 503")
	}
}

func TestParseAxiomResult_Empty(t *testing.T) {
	r := parseAxiomResult(map[string]interface{}{})
	if r.WorkItemID != "" {
		t.Errorf("WorkItemID = %q, want empty", r.WorkItemID)
	}
}

func TestParseWorkItem_Empty(t *testing.T) {
	wi := parseWorkItem(map[string]interface{}{})
	if wi.ID != "" {
		t.Errorf("ID = %q, want empty", wi.ID)
	}
}
