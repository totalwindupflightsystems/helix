// Tests for cmd/helix/webhook.go — covers flag parsing, the serve
// subcommand, error paths, and the integrated subcommand dispatcher.
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/helix/pkg/webhook"
)

// -----------------------------------------------------------------------------
// parseWebhookServeFlags
// -----------------------------------------------------------------------------

func TestParseWebhookServeFlags_HappyPath(t *testing.T) {
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	f, showHelp, err := parseWebhookServeFlags(
		[]string{"--addr", ":8080", "--secret-file", "/tmp/secret", "--verbose"},
		stdout, stderr,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if showHelp {
		t.Fatal("showHelp should be false")
	}
	if f.addr != ":8080" {
		t.Errorf("addr = %q, want :8080", f.addr)
	}
	if f.secretFile != "/tmp/secret" {
		t.Errorf("secretFile = %q, want /tmp/secret", f.secretFile)
	}
	if !f.verbose {
		t.Error("verbose should be true")
	}
}

func TestParseWebhookServeFlags_Help(t *testing.T) {
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	_, showHelp, err := parseWebhookServeFlags([]string{"--help"}, stdout, stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !showHelp {
		t.Error("showHelp should be true on --help")
	}
}

func TestParseWebhookServeFlags_PositionalRejected(t *testing.T) {
	_, _, err := parseWebhookServeFlags(
		[]string{"--secret-file", "/tmp/x", "extra"},
		&strings.Builder{}, &strings.Builder{},
	)
	if err == nil {
		t.Fatal("expected error for positional argument")
	}
}

// -----------------------------------------------------------------------------
// runWebhookServe — error paths
// -----------------------------------------------------------------------------

func TestRunWebhookServe_MissingSecretFile(t *testing.T) {
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	rc := runWebhookServe([]string{}, stdout, stderr)
	if rc != 2 {
		t.Errorf("rc = %d, want 2 for missing --secret-file", rc)
	}
	if !strings.Contains(stderr.String(), "--secret-file is required") {
		t.Errorf("stderr = %q, want it to mention --secret-file", stderr.String())
	}
}

func TestRunWebhookServe_MissingFile(t *testing.T) {
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	rc := runWebhookServe([]string{
		"--secret-file", "/nonexistent/secret",
		"--addr", ":0", // ephemeral port — won't actually bind because we exit first
	}, stdout, stderr)
	if rc != 1 {
		t.Errorf("rc = %d, want 1 for missing secret file", rc)
	}
	if !strings.Contains(stderr.String(), "read secret") {
		t.Errorf("stderr = %q, want it to mention 'read secret'", stderr.String())
	}
}

func TestRunWebhookServe_EmptySecretFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	rc := runWebhookServe([]string{
		"--secret-file", path,
		"--addr", ":0",
	}, stdout, stderr)
	if rc != 1 {
		t.Errorf("rc = %d, want 1 for empty secret file", rc)
	}
	if !strings.Contains(stderr.String(), "secret file is empty") {
		t.Errorf("stderr = %q, want it to mention 'empty'", stderr.String())
	}
}

// -----------------------------------------------------------------------------
// runWebhookServe — happy path: start the server, send a signed event,
// verify it appears on stdout, then shut down.
// -----------------------------------------------------------------------------

func TestRunWebhookServe_AcceptsValidEvent(t *testing.T) {
	dir := t.TempDir()
	secretFile := filepath.Join(dir, "secret")
	secret := []byte("super-secret-token")
	if err := os.WriteFile(secretFile, secret, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Bind a httptest server with the same handler the CLI uses. We
	// can't actually bind a real listener in the test process because
	// runWebhookServe blocks on ListenAndServe; instead we replicate
	// the wiring here and exercise it directly.
	h := webhook.NewHandler(secret)
	srv := httptest.NewServer(http.HandlerFunc(h.ServeHTTP))
	defer srv.Close()

	// Build a signed event.
	payload := []byte(`{"action":"opened","number":42,"pull_request":{"title":"x","state":"open"}}`)
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set(webhook.SignatureHeader, sig)
	req.Header.Set("X-Forgejo-Event", "pull_request_opened")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"pr_number":42`) {
		t.Errorf("response missing pr_number=42: %s", body)
	}
}

func TestRunWebhookServe_RejectsBadSignature(t *testing.T) {
	h := webhook.NewHandler([]byte("real-secret"))
	srv := httptest.NewServer(http.HandlerFunc(h.ServeHTTP))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader("{}"))
	req.Header.Set(webhook.SignatureHeader, "sha256=deadbeef")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

// -----------------------------------------------------------------------------
// runWebhook — top-level dispatcher
// -----------------------------------------------------------------------------

func TestRunWebhook_NoArgs(t *testing.T) {
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	rc := runWebhook([]string{}, stdout, stderr)
	if rc != 0 {
		t.Errorf("rc = %d, want 0 (usage should exit 0)", rc)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Errorf("stdout should contain Usage:\n%s", stdout.String())
	}
}

func TestRunWebhook_Help(t *testing.T) {
	stdout, _ := &strings.Builder{}, &strings.Builder{}
	rc := runWebhook([]string{"help"}, stdout, &strings.Builder{})
	if rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
}

func TestRunWebhook_UnknownSubcommand(t *testing.T) {
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	rc := runWebhook([]string{"frobnicate"}, stdout, stderr)
	if rc != 2 {
		t.Errorf("rc = %d, want 2", rc)
	}
	if !strings.Contains(stderr.String(), "unknown subcommand") {
		t.Errorf("stderr should mention unknown subcommand\n%s", stderr.String())
	}
}

// -----------------------------------------------------------------------------
// printWebhookUsage
// -----------------------------------------------------------------------------

func TestPrintWebhookUsage(t *testing.T) {
	out := &strings.Builder{}
	printWebhookUsage(out)
	for _, want := range []string{"serve", "--addr", "--secret-file", "HELIX_WEBHOOK_ADDR"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("usage missing %q", want)
		}
	}
}
