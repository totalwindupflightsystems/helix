package integration

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSuiteUnit_NewForgejoClient_ValidURL verifies construction with a valid URL.
func TestSuiteUnit_NewForgejoClient_ValidURL(t *testing.T) {
	c, err := NewForgejoClient("http://localhost:3030", "admin", "pass")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if c == nil {
		t.Fatal("client is nil")
	}
	if c.BaseURL != "http://localhost:3030" {
		t.Errorf("BaseURL = %q, want http://localhost:3030", c.BaseURL)
	}
	if c.AdminUser != "admin" || c.AdminPass != "pass" {
		t.Errorf("creds = %q/%q, want admin/pass", c.AdminUser, c.AdminPass)
	}
	if c.HTTPClient == nil {
		t.Error("HTTPClient is nil")
	}
	if c.HTTPClient.Timeout == 0 {
		t.Error("HTTPClient.Timeout not set")
	}
}

// TestSuiteUnit_NewForgejoClient_TrailingSlash verifies that trailing slashes
// are trimmed from the BaseURL.
func TestSuiteUnit_NewForgejoClient_TrailingSlash(t *testing.T) {
	c, err := NewForgejoClient("http://localhost:3030/", "u", "p")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if strings.HasSuffix(c.BaseURL, "/") {
		t.Errorf("BaseURL = %q, want trimmed trailing slash", c.BaseURL)
	}
}

// TestSuiteUnit_NewChimeraClient_ValidURL verifies construction with a valid URL.
func TestSuiteUnit_NewChimeraClient_ValidURL(t *testing.T) {
	c, err := NewChimeraClient("http://localhost:8765")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if c == nil {
		t.Fatal("client is nil")
	}
	if c.BaseURL != "http://localhost:8765" {
		t.Errorf("BaseURL = %q, want http://localhost:8765", c.BaseURL)
	}
	if c.HTTPClient == nil {
		t.Error("HTTPClient is nil")
	}
}

// TestSuiteUnit_ChimeraHealth_OK verifies Health() returns the right status.
func TestSuiteUnit_ChimeraHealth_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health" {
			t.Errorf("path = %s, want /v1/health", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy"}`))
	}))
	defer srv.Close()

	c, err := NewChimeraClient(srv.URL)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	status, err := c.Health(t)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
}

// TestSuiteUnit_ChimeraHealth_Down verifies Health() reports the right HTTP
// status when the server returns non-200.
func TestSuiteUnit_ChimeraHealth_Down(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c, _ := NewChimeraClient(srv.URL)
	status, _ := c.Health(t)
	if status != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", status)
	}
}

// TestSuiteUnit_ChimeraEstimate_StubReturn verifies that Estimate() returns
// the documented stub result without calling any HTTP endpoint.
func TestSuiteUnit_ChimeraEstimate_StubReturn(t *testing.T) {
	c, err := NewChimeraClient("http://localhost:9999")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	result, err := c.Estimate(t, "Write a Go hello-world")
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	if cost, ok := result["cost_total"].(float64); !ok || cost != 0.05 {
		t.Errorf("cost_total = %v (%T), want 0.05 (float64)", result["cost_total"], result["cost_total"])
	}
	if tok, ok := result["tokens_in"].(int); !ok || tok != 4000 {
		t.Errorf("tokens_in = %v (%T), want 4000 (int)", result["tokens_in"], result["tokens_in"])
	}
	if result["model"].(string) != "owl-alpha" {
		t.Errorf("model = %v, want owl-alpha", result["model"])
	}
	if result["description"].(string) != "Write a Go hello-world" {
		t.Errorf("description = %v, want input taskDesc", result["description"])
	}
}

// TestSuiteUnit_GenerateTestSSHKey_Format verifies the generated key starts
// with the OpenSSH ed25519 prefix.
func TestSuiteUnit_GenerateTestSSHKey_Format(t *testing.T) {
	key, err := generateTestSSHKey(t)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.HasPrefix(key, "ssh-ed25519 ") {
		t.Errorf("key does not start with ssh-ed25519 prefix: %q", key)
	}
	if !strings.Contains(key, "helix-integration-test") {
		t.Errorf("key missing comment: %q", key)
	}
}
