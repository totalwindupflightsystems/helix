package estimate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// GetKeyRemaining tests
// ---------------------------------------------------------------------------

func TestOpenRouterClient_GetKeyRemaining(t *testing.T) {
	srv := newMockOpenRouterServer(t, http.StatusOK, `{
		"data": {
			"limit": 10.00,
			"usage": 3.42,
			"limit_remaining": 6.58
		}
	}`)
	defer srv.Close()

	c := NewOpenRouterClient(srv.URL)
	v, err := c.GetKeyRemaining(context.Background(), "sk-test")
	if err != nil {
		t.Fatalf("GetKeyRemaining() error = %v", err)
	}
	if v != 6.58 {
		t.Errorf("GetKeyRemaining() = %v, want 6.58", v)
	}
}

// ---------------------------------------------------------------------------
// GetKeyInfo tests
// ---------------------------------------------------------------------------

func TestOpenRouterClient_GetKeyInfo(t *testing.T) {
	srv := newMockOpenRouterServer(t, http.StatusOK, `{
		"data": {
			"label": "agent-wojons",
			"limit": 25.00,
			"usage": 12.50,
			"limit_remaining": 12.50,
			"is_free_tier": false
		}
	}`)
	defer srv.Close()

	c := NewOpenRouterClient(srv.URL)
	info, err := c.GetKeyInfo(context.Background(), "sk-test")
	if err != nil {
		t.Fatalf("GetKeyInfo() error = %v", err)
	}
	if info.Label != "agent-wojons" {
		t.Errorf("Label = %q, want %q", info.Label, "agent-wojons")
	}
	if info.Limit != 25.00 {
		t.Errorf("Limit = %v, want 25.00", info.Limit)
	}
	if info.Usage != 12.50 {
		t.Errorf("Usage = %v, want 12.50", info.Usage)
	}
	if info.LimitRemaining != 12.50 {
		t.Errorf("LimitRemaining = %v, want 12.50", info.LimitRemaining)
	}
	if info.IsFreeTier {
		t.Errorf("IsFreeTier = true, want false")
	}
}

func TestOpenRouterClient_KeyInfo_BudgetFractions(t *testing.T) {
	info := &KeyInfo{
		Limit:          100.0,
		Usage:          30.0,
		LimitRemaining: 70.0,
	}
	if info.BudgetRemaining() != 0.7 {
		t.Errorf("BudgetRemaining() = %v, want 0.7", info.BudgetRemaining())
	}
	if info.BudgetUsed() != 0.3 {
		t.Errorf("BudgetUsed() = %v, want 0.3", info.BudgetUsed())
	}
}

func TestOpenRouterClient_KeyInfo_ZeroLimit(t *testing.T) {
	info := &KeyInfo{
		Limit: 0,
		Usage: 10.0,
	}
	if info.BudgetRemaining() != 0 {
		t.Errorf("BudgetRemaining() with zero limit = %v, want 0", info.BudgetRemaining())
	}
	if info.BudgetUsed() != 0 {
		t.Errorf("BudgetUsed() with zero limit = %v, want 0", info.BudgetUsed())
	}
}

// ---------------------------------------------------------------------------
// Error handling tests
// ---------------------------------------------------------------------------

func TestOpenRouterClient_401_Unauthorized(t *testing.T) {
	srv := newMockOpenRouterServer(t, http.StatusUnauthorized, `{"error": "invalid key"}`)
	defer srv.Close()

	c := NewOpenRouterClient(srv.URL)
	_, err := c.GetKeyUsage(context.Background(), "sk-dead-key")
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if err != ErrAuthFailed {
		t.Errorf("expected ErrAuthFailed, got %v", err)
	}
}

func TestOpenRouterClient_429_RateLimited(t *testing.T) {
	srv := newMockOpenRouterServer(t, http.StatusTooManyRequests, `{"error": "rate limited"}`)
	defer srv.Close()

	c := NewOpenRouterClient(srv.URL)
	_, err := c.GetKeyUsage(context.Background(), "sk-test")
	if err == nil {
		t.Fatal("expected error for 429")
	}
	if err != ErrRateLimited {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
}

func TestOpenRouterClient_500_ServerError(t *testing.T) {
	srv := newMockOpenRouterServer(t, http.StatusInternalServerError, `{"error": "internal"}`)
	defer srv.Close()

	c := NewOpenRouterClient(srv.URL)
	_, err := c.GetKeyUsage(context.Background(), "sk-test")
	if err == nil {
		t.Fatal("expected error for 500")
	}
}

func TestOpenRouterClient_EmptyAPIKey(t *testing.T) {
	c := NewOpenRouterClient("")
	_, err := c.GetKeyUsage(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty API key")
	}
}

func TestOpenRouterClient_MalformedJSON(t *testing.T) {
	srv := newMockOpenRouterServer(t, http.StatusOK, `{not valid json}`)
	defer srv.Close()

	c := NewOpenRouterClient(srv.URL)
	_, err := c.GetKeyUsage(context.Background(), "sk-test")
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

// ---------------------------------------------------------------------------
// Context cancellation test
// ---------------------------------------------------------------------------

func TestOpenRouterClient_ContextCancelled(t *testing.T) {
	srv := newMockOpenRouterServer(t, http.StatusOK, `{"data": {"usage": 5.0}}`)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before calling

	c := NewOpenRouterClient(srv.URL)
	_, err := c.GetKeyUsage(ctx, "sk-test")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// ---------------------------------------------------------------------------
// Request verification test
// ---------------------------------------------------------------------------

func TestOpenRouterClient_AuthHeaderSent(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data": {"usage": 1.0}}`))
	}))
	defer srv.Close()

	c := NewOpenRouterClient(srv.URL)
	_, err := c.GetKeyUsage(context.Background(), "sk-my-secret-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedAuth != "Bearer sk-my-secret-key" {
		t.Errorf("Authorization header = %q, want %q", receivedAuth, "Bearer sk-my-secret-key")
	}
}

// ---------------------------------------------------------------------------
// Response body verification
// ---------------------------------------------------------------------------

func TestOpenRouterClient_FullResponseParsed(t *testing.T) {
	// Verify all fields from spec §9.1 are correctly parsed.
	responseData := map[string]any{
		"data": map[string]any{
			"label":           "test-agent",
			"limit":           50.00,
			"usage":           15.75,
			"limit_remaining": 34.25,
			"is_free_tier":    true,
		},
	}
	body, _ := json.Marshal(responseData)

	srv := newMockOpenRouterServer(t, http.StatusOK, string(body))
	defer srv.Close()

	c := NewOpenRouterClient(srv.URL)
	info, err := c.GetKeyInfo(context.Background(), "sk-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Label != "test-agent" {
		t.Errorf("Label = %q", info.Label)
	}
	if info.Limit != 50.00 {
		t.Errorf("Limit = %v", info.Limit)
	}
	if info.Usage != 15.75 {
		t.Errorf("Usage = %v", info.Usage)
	}
	if info.LimitRemaining != 34.25 {
		t.Errorf("LimitRemaining = %v", info.LimitRemaining)
	}
	if !info.IsFreeTier {
		t.Errorf("IsFreeTier = false, want true")
	}
}
