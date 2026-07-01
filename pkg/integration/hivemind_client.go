package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ---------------------------------------------------------------------------
// Hivemind HTTP Client — concrete implementation of HivemindAdapter
//
// Per spec integrations.md §7:
// "Hivemind provides persistent agent memory + task scheduling. IAM/auth, git
// operations, Ralph Loop engine, SQLite + YAML memory bank, inbox/compiled
// pattern, hierarchical rate limiting. The shared blackboard that agents
// read and write."
//
// This client wraps Hivemind's REST API into Go calls implementing the
// HivemindAdapter interface.
// ---------------------------------------------------------------------------

// Sentinel errors for Hivemind API failures.
var (
	ErrHivemindAuthFailed    = fmt.Errorf("hivemind: authentication failed (401)")
	ErrHivemindRateLimited   = fmt.Errorf("hivemind: rate limited (429)")
	ErrHivemindServerDown    = fmt.Errorf("hivemind: server unreachable")
	ErrHivemindInvalidResult = fmt.Errorf("hivemind: invalid response")
)

// HivemindClient is the HTTP implementation of HivemindAdapter.
type HivemindClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// HivemindClientOption configures a HivemindClient.
type HivemindClientOption func(*HivemindClient)

// WithHivemindHTTPClient sets a custom HTTP client (for testing).
func WithHivemindHTTPClient(c *http.Client) HivemindClientOption {
	return func(hc *HivemindClient) { hc.httpClient = c }
}

// WithHivemindTimeout sets the HTTP timeout.
func WithHivemindTimeout(d time.Duration) HivemindClientOption {
	return func(hc *HivemindClient) { hc.httpClient.Timeout = d }
}

// NewHivemindClient creates a new Hivemind HTTP client.
// baseURL is the Hivemind API root (e.g., http://hivemind:3000).
// token is the optional Bearer token for authentication.
func NewHivemindClient(baseURL, token string, opts ...HivemindClientOption) *HivemindClient {
	c := &HivemindClient{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ScheduleTask queues a task for agent execution.
func (c *HivemindClient) ScheduleTask(task HiveTask) (*HiveTask, error) {
	body := map[string]interface{}{
		"id":           task.ID,
		"title":        task.Title,
		"description":  task.Description,
		"priority":     task.Priority,
		"status":       task.Status,
		"assigned_to":  task.AssignedTo,
		"created_at":   task.CreatedAt,
		"deadline":     task.Deadline,
		"dependencies": task.Dependencies,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal task: %w", err)
	}

	url := c.baseURL + "/api/v1/tasks"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrHivemindServerDown, err)
	}
	defer resp.Body.Close()

	if err := checkHivemindError(resp); err != nil {
		return nil, err
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("%w: decode error: %v", ErrHivemindInvalidResult, err)
	}

	result := parseHiveTask(raw)
	return &result, nil
}

// ClaimTask acquires the next available task for an agent.
func (c *HivemindClient) ClaimTask(agentName string) (*HiveTask, error) {
	url := c.baseURL + "/api/v1/tasks/claim?agent=" + agentName
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrHivemindServerDown, err)
	}
	defer resp.Body.Close()

	if err := checkHivemindError(resp); err != nil {
		return nil, err
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("%w: decode error: %v", ErrHivemindInvalidResult, err)
	}

	result := parseHiveTask(raw)
	return &result, nil
}

// CompleteTask marks a task as done with results.
func (c *HivemindClient) CompleteTask(taskID string, result TaskResult) error {
	body := map[string]interface{}{
		"success":  result.Success,
		"output":   result.Output,
		"evidence": result.Evidence,
		"cost":     result.Cost,
		"duration": result.Duration,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	url := c.baseURL + "/api/v1/tasks/" + taskID + "/complete"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrHivemindServerDown, err)
	}
	defer resp.Body.Close()

	return checkHivemindError(resp)
}

// ReadMemory reads from the shared memory bank.
func (c *HivemindClient) ReadMemory(key string) (*MemoryEntry, error) {
	url := c.baseURL + "/api/v1/memory/" + key
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrHivemindServerDown, err)
	}
	defer resp.Body.Close()

	if err := checkHivemindError(resp); err != nil {
		return nil, err
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("%w: decode error: %v", ErrHivemindInvalidResult, err)
	}

	return parseMemoryEntry(raw), nil
}

// WriteMemory writes to the shared memory bank.
func (c *HivemindClient) WriteMemory(key string, content string, domain string) error {
	body := map[string]interface{}{
		"key":     key,
		"content": content,
		"domain":  domain,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}

	url := c.baseURL + "/api/v1/memory"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrHivemindServerDown, err)
	}
	defer resp.Body.Close()

	return checkHivemindError(resp)
}

// Health returns service health.
func (c *HivemindClient) Health() (*HivemindHealth, error) {
	url := c.baseURL + "/api/v1/health"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	c.setAuth(req)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &HivemindHealth{Status: "down"}, fmt.Errorf("%w: %v", ErrHivemindServerDown, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HivemindHealth{Status: "down"}, nil
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return &HivemindHealth{Status: "healthy"}, nil
	}

	h := &HivemindHealth{Status: "healthy"}
	if v, ok := raw["status"].(string); ok {
		h.Status = v
	}
	if v, ok := raw["tasks_queued"].(float64); ok {
		h.TasksQueued = int(v)
	}
	if v, ok := raw["tasks_active"].(float64); ok {
		h.TasksActive = int(v)
	}
	if v, ok := raw["memory_size"].(float64); ok {
		h.MemorySize = int64(v)
	}
	if v, ok := raw["uptime"].(float64); ok {
		h.Uptime = v
	}
	return h, nil
}

// setAuth adds the Bearer token header if configured.
func (c *HivemindClient) setAuth(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

// checkHivemindError inspects the HTTP response status and returns a sentinel
// error for known failure codes.
func checkHivemindError(resp *http.Response) error {
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrHivemindAuthFailed
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return ErrHivemindRateLimited
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("hivemind returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// parseHiveTask converts a raw JSON map into a typed HiveTask.
func parseHiveTask(raw map[string]interface{}) HiveTask {
	t := HiveTask{}
	if v, ok := raw["id"].(string); ok {
		t.ID = v
	}
	if v, ok := raw["title"].(string); ok {
		t.Title = v
	}
	if v, ok := raw["description"].(string); ok {
		t.Description = v
	}
	if v, ok := raw["priority"].(string); ok {
		t.Priority = v
	}
	if v, ok := raw["status"].(string); ok {
		t.Status = v
	}
	if v, ok := raw["assigned_to"].(string); ok {
		t.AssignedTo = v
	}
	if v, ok := raw["created_at"].(string); ok {
		t.CreatedAt = v
	}
	if v, ok := raw["deadline"].(string); ok {
		t.Deadline = v
	}
	if deps, ok := raw["dependencies"].([]interface{}); ok {
		for _, di := range deps {
			if s, ok := di.(string); ok {
				t.Dependencies = append(t.Dependencies, s)
			}
		}
	}
	return t
}

// parseMemoryEntry converts a raw JSON map into a typed MemoryEntry.
func parseMemoryEntry(raw map[string]interface{}) *MemoryEntry {
	m := &MemoryEntry{}
	if v, ok := raw["key"].(string); ok {
		m.Key = v
	}
	if v, ok := raw["content"].(string); ok {
		m.Content = v
	}
	if v, ok := raw["domain"].(string); ok {
		m.Domain = v
	}
	if v, ok := raw["updated_at"].(string); ok {
		m.UpdatedAt = v
	}
	if v, ok := raw["version"].(float64); ok {
		m.Version = int(v)
	}
	return m
}
