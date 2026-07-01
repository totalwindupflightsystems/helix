package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewHivemindClient(t *testing.T) {
	c := NewHivemindClient("http://localhost:3000", "tok123")
	if c.baseURL != "http://localhost:3000" {
		t.Errorf("baseURL = %q, want http://localhost:3000", c.baseURL)
	}
	if c.token != "tok123" {
		t.Errorf("token = %q, want tok123", c.token)
	}
	if c.httpClient.Timeout != 30*time.Second {
		t.Errorf("timeout = %v, want 30s", c.httpClient.Timeout)
	}
}

func TestNewHivemindClient_Options(t *testing.T) {
	customClient := &http.Client{Timeout: 5 * time.Second}
	c := NewHivemindClient("http://localhost:3000", "",
		WithHivemindHTTPClient(customClient),
		WithHivemindTimeout(10*time.Second),
	)
	if c.httpClient != customClient {
		t.Fatal("WithHivemindHTTPClient did not set custom client")
	}
	if c.httpClient.Timeout != 10*time.Second {
		t.Errorf("timeout = %v, want 10s", c.httpClient.Timeout)
	}
}

func TestHivemindClient_ScheduleTask_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks" {
			t.Errorf("path = %q, want /api/v1/tasks", r.URL.Path)
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
		if body["title"] != "review PR" {
			t.Errorf("title = %v", body["title"])
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":           "task-001",
			"title":        "review PR",
			"description":  "Review PR #5",
			"priority":     "high",
			"status":       "queued",
			"assigned_to":  "",
			"created_at":   "2026-07-01T00:00:00Z",
			"deadline":     "2026-07-01T12:00:00Z",
			"dependencies": []string{"task-000"},
		})
	}))
	defer srv.Close()

	c := NewHivemindClient(srv.URL, "test-token")
	task, err := c.ScheduleTask(HiveTask{Title: "review PR", Priority: "high"})
	if err != nil {
		t.Fatalf("ScheduleTask() error: %v", err)
	}
	if task.ID != "task-001" {
		t.Errorf("ID = %q", task.ID)
	}
	if task.Status != "queued" {
		t.Errorf("Status = %q", task.Status)
	}
	if len(task.Dependencies) != 1 {
		t.Errorf("Dependencies len = %d", len(task.Dependencies))
	}
}

func TestHivemindClient_ScheduleTask_AuthFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewHivemindClient(srv.URL, "")
	_, err := c.ScheduleTask(HiveTask{Title: "x"})
	if err != ErrHivemindAuthFailed {
		t.Errorf("error = %v, want ErrHivemindAuthFailed", err)
	}
}

func TestHivemindClient_ScheduleTask_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewHivemindClient(srv.URL, "")
	_, err := c.ScheduleTask(HiveTask{Title: "x"})
	if err != ErrHivemindRateLimited {
		t.Errorf("error = %v, want ErrHivemindRateLimited", err)
	}
}

func TestHivemindClient_ScheduleTask_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewHivemindClient(srv.URL, "")
	_, err := c.ScheduleTask(HiveTask{Title: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHivemindClient_ScheduleTask_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{bad"))
	}))
	defer srv.Close()

	c := NewHivemindClient(srv.URL, "")
	_, err := c.ScheduleTask(HiveTask{Title: "x"})
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestHivemindClient_ClaimTask_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/claim" {
			t.Errorf("path = %q, want /api/v1/tasks/claim", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if agent := r.URL.Query().Get("agent"); agent != "agent-1" {
			t.Errorf("agent param = %q, want agent-1", agent)
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":          "task-005",
			"title":       "write tests",
			"status":      "claimed",
			"assigned_to": "agent-1",
		})
	}))
	defer srv.Close()

	c := NewHivemindClient(srv.URL, "tok")
	task, err := c.ClaimTask("agent-1")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}
	if task.ID != "task-005" {
		t.Errorf("ID = %q", task.ID)
	}
	if task.AssignedTo != "agent-1" {
		t.Errorf("AssignedTo = %q", task.AssignedTo)
	}
}

func TestHivemindClient_ClaimTask_AuthFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewHivemindClient(srv.URL, "")
	_, err := c.ClaimTask("agent-1")
	if err != ErrHivemindAuthFailed {
		t.Errorf("error = %v, want ErrHivemindAuthFailed", err)
	}
}

func TestHivemindClient_CompleteTask_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/task-005/complete" {
			t.Errorf("path = %q, want /api/v1/tasks/task-005/complete", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["success"] != true {
			t.Errorf("success = %v", body["success"])
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewHivemindClient(srv.URL, "tok")
	err := c.CompleteTask("task-005", TaskResult{Success: true, Output: "done"})
	if err != nil {
		t.Fatalf("CompleteTask() error: %v", err)
	}
}

func TestHivemindClient_CompleteTask_AuthFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewHivemindClient(srv.URL, "")
	err := c.CompleteTask("task-005", TaskResult{})
	if err != ErrHivemindAuthFailed {
		t.Errorf("error = %v, want ErrHivemindAuthFailed", err)
	}
}

func TestHivemindClient_CompleteTask_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewHivemindClient(srv.URL, "")
	err := c.CompleteTask("task-005", TaskResult{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHivemindClient_ReadMemory_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/memory/projects/helix" {
			t.Errorf("path = %q, want /api/v1/memory/projects/helix", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"key":        "projects/helix",
			"content":    "Agent-first code platform",
			"domain":     "concept",
			"updated_at": "2026-07-01T00:00:00Z",
			"version":    3,
		})
	}))
	defer srv.Close()

	c := NewHivemindClient(srv.URL, "tok")
	entry, err := c.ReadMemory("projects/helix")
	if err != nil {
		t.Fatalf("ReadMemory() error: %v", err)
	}
	if entry.Key != "projects/helix" {
		t.Errorf("Key = %q", entry.Key)
	}
	if entry.Content != "Agent-first code platform" {
		t.Errorf("Content = %q", entry.Content)
	}
	if entry.Version != 3 {
		t.Errorf("Version = %d", entry.Version)
	}
}

func TestHivemindClient_ReadMemory_AuthFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewHivemindClient(srv.URL, "")
	_, err := c.ReadMemory("key")
	if err != ErrHivemindAuthFailed {
		t.Errorf("error = %v, want ErrHivemindAuthFailed", err)
	}
}

func TestHivemindClient_ReadMemory_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewHivemindClient(srv.URL, "")
	_, err := c.ReadMemory("key")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHivemindClient_WriteMemory_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/memory" {
			t.Errorf("path = %q, want /api/v1/memory", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["key"] != "projects/helix/status" {
			t.Errorf("key = %v", body["key"])
		}
		if body["domain"] != "concept" {
			t.Errorf("domain = %v", body["domain"])
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewHivemindClient(srv.URL, "tok")
	err := c.WriteMemory("projects/helix/status", "active", "concept")
	if err != nil {
		t.Fatalf("WriteMemory() error: %v", err)
	}
}

func TestHivemindClient_WriteMemory_AuthFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewHivemindClient(srv.URL, "")
	err := c.WriteMemory("key", "content", "domain")
	if err != ErrHivemindAuthFailed {
		t.Errorf("error = %v, want ErrHivemindAuthFailed", err)
	}
}

func TestHivemindClient_WriteMemory_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewHivemindClient(srv.URL, "")
	err := c.WriteMemory("key", "content", "domain")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHivemindClient_Health_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/health" {
			t.Errorf("path = %q, want /api/v1/health", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "healthy",
			"tasks_queued": 15,
			"tasks_active": 3,
			"memory_size":  1048576,
			"uptime":       86400.5,
		})
	}))
	defer srv.Close()

	c := NewHivemindClient(srv.URL, "")
	h, err := c.Health()
	if err != nil {
		t.Fatalf("Health() error: %v", err)
	}
	if h.Status != "healthy" {
		t.Errorf("Status = %q", h.Status)
	}
	if h.TasksQueued != 15 {
		t.Errorf("TasksQueued = %d", h.TasksQueued)
	}
	if h.TasksActive != 3 {
		t.Errorf("TasksActive = %d", h.TasksActive)
	}
	if h.MemorySize != 1048576 {
		t.Errorf("MemorySize = %d", h.MemorySize)
	}
	if h.Uptime != 86400.5 {
		t.Errorf("Uptime = %f", h.Uptime)
	}
}

func TestHivemindClient_Health_Down(t *testing.T) {
	c := NewHivemindClient("http://127.0.0.1:0", "")
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

func TestHivemindClient_Health_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewHivemindClient(srv.URL, "")
	h, err := c.Health()
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if h.Status != "down" {
		t.Errorf("Status = %q, want down", h.Status)
	}
}

func TestHivemindClient_Health_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{bad"))
	}))
	defer srv.Close()

	c := NewHivemindClient(srv.URL, "")
	h, err := c.Health()
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if h.Status != "healthy" {
		t.Errorf("Status = %q, want healthy (fallback)", h.Status)
	}
}

func TestParseHiveTask_Empty(t *testing.T) {
	t2 := parseHiveTask(map[string]interface{}{})
	if t2.ID != "" {
		t.Errorf("ID = %q, want empty", t2.ID)
	}
}

func TestParseMemoryEntry_Empty(t *testing.T) {
	m := parseMemoryEntry(map[string]interface{}{})
	if m.Key != "" {
		t.Errorf("Key = %q, want empty", m.Key)
	}
}
