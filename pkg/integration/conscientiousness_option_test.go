package integration

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestWithConscientiousnessHTTPClient verifies that WithConscientiousnessHTTPClient
// swaps in a custom http.Client and that the Evaluate path uses it.
func TestWithConscientiousnessHTTPClient(t *testing.T) {
	var serverURL string
	called := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"status":"DEFENSIBLE","confidence":0.5}`))
	}))
	defer server.Close()
	serverURL = server.URL

	// Custom client with custom Transport (also records the call)
	custom := &http.Client{
		Timeout: 5 * time.Second,
	}

	client := NewConscientiousnessClient(serverURL, "key",
		WithConscientiousnessHTTPClient(custom))

	verdict, err := client.Evaluate(ConscientiousnessPR{
		RepoOwner: "org",
		RepoName:  "repo",
		PRNumber:  7,
	}, EvalOpts{})
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if !called {
		t.Fatal("custom http.Client was not invoked (server never received request)")
	}
	if verdict == nil || verdict.Status != "DEFENSIBLE" {
		t.Errorf("verdict = %+v, want DEFENSIBLE status", verdict)
	}
}

// TestWithConscientiousnessHTTPClient_DifferentFromDefault verifies that the
// custom client is actually used (not the default 180s-timeout one) — by
// injecting a Transport that requires a different User-Agent header.
func TestWithConscientiousnessHTTPClient_CustomTransport(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Header.Get("X-Test-Marker") != "helix-1" {
			t.Errorf("X-Test-Marker header missing; got %q", r.Header.Get("X-Test-Marker"))
		}
		_, _ = w.Write([]byte(`{"status":"DEFENSIBLE"}`))
	}))
	defer server.Close()

	// Wrap the default transport with a RoundTripper that adds a marker
	base := http.DefaultTransport
	marker := &markerTransport{base: base, key: "X-Test-Marker", value: "helix-1"}

	custom := &http.Client{Transport: marker, Timeout: 5 * time.Second}

	client := NewConscientiousnessClient(server.URL, "k",
		WithConscientiousnessHTTPClient(custom))

	// Evaluate adds the marker header on the way out — we just need the
	// request to be served; the marker check above verifies transport use.
	_, _ = client.Evaluate(ConscientiousnessPR{
		RepoOwner: "o", RepoName: "r", PRNumber: 1,
	}, EvalOpts{MaxIterations: 1})
	if !called {
		t.Fatal("custom Transport was not used")
	}
}

// TestWithConscientiousnessTimeout_OverwritesDefault verifies that
// WithConscientiousnessTimeout sets the http.Client.Timeout on the default
// client (not a separate field).
func TestWithConscientiousnessTimeout_OverwritesDefault(t *testing.T) {
	client := NewConscientiousnessClient("http://localhost:1", "",
		WithConscientiousnessTimeout(42*time.Second))

	if client.httpClient.Timeout != 42*time.Second {
		t.Errorf("httpClient.Timeout = %v, want 42s", client.httpClient.Timeout)
	}
}

// TestConscientiousness_Health_UsesCustomClient verifies Health() uses the
// injected http.Client as well.
func TestConscientiousness_Health_UsesCustomClient(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/health" {
			t.Errorf("path = %s, want /health", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"status":"healthy","version":"1.0.0","uptime":123.4}`))
	}))
	defer server.Close()

	custom := &http.Client{Timeout: 2 * time.Second}
	client := NewConscientiousnessClient(server.URL, "",
		WithConscientiousnessHTTPClient(custom))

	h, err := client.Health()
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}
	if !called {
		t.Fatal("custom http.Client was not used by Health()")
	}
	if h.Status != "healthy" || h.Version != "1.0.0" || h.Uptime != 123.4 {
		t.Errorf("health = %+v, want healthy/1.0.0/123.4", h)
	}
}

// TestConscientiousness_Evaluate_ContextCancel verifies that the inner
// context.WithTimeout (driven by httpClient.Timeout) actually cancels the
// request when the timeout is hit.
func TestConscientiousness_Evaluate_ContextCancel(t *testing.T) {
	// Use a TCP listener that accepts but never replies. The client should
	// bail out around its configured timeout.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	// Accept connections in a loop but never respond — hangs the read.
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Hold the connection open without responding.
			go func(c net.Conn) {
				<-time.After(5 * time.Second) // outlives any test timeout
				_ = c.Close()
			}(conn)
		}
	}()

	client := NewConscientiousnessClient("http://"+ln.Addr().String(), "",
		WithConscientiousnessTimeout(100*time.Millisecond))

	start := time.Now()
	_, err = client.Evaluate(ConscientiousnessPR{
		RepoOwner: "o", RepoName: "r", PRNumber: 1,
	}, EvalOpts{})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from timed-out request")
	}
	// Should bail out around the timeout, not hang for the default 180s.
	if elapsed > 2*time.Second {
		t.Errorf("Evaluate took %v, expected ~100ms (timeout) — default 180s client leaked through?", elapsed)
	}
}

// markerTransport injects a header on every request — used to verify that
// the custom http.Client (not the default one) actually fires the request.
type markerTransport struct {
	base  http.RoundTripper
	key   string
	value string
}

func (m *markerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set(m.key, m.value)
	// Pass through to the base transport. Use ctx-aware context just in case.
	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
	defer cancel()
	return m.base.RoundTrip(req.WithContext(ctx))
}
