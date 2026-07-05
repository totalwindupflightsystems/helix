// Tests for pkg/webhook/forgejo.go — covers event payload parsing,
// HTTP handler semantics, error paths, and the 5 event-type variants.
package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// -----------------------------------------------------------------------------
// Test helpers
// -----------------------------------------------------------------------------

func signedRequest(payload []byte, eventType string, secret []byte) *http.Request {
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(payload))
	req.Header.Set(SignatureHeader, sig)
	if eventType != "" {
		req.Header.Set("X-Forgejo-Event", eventType)
	}
	return req
}

// -----------------------------------------------------------------------------
// ParsePullRequestEvent — 5 event types
// -----------------------------------------------------------------------------

func TestParsePullRequestEvent_Opened(t *testing.T) {
	payload := []byte(`{
		"action": "opened",
		"number": 42,
		"pull_request": {
			"title": "Fix bug",
			"html_url": "https://forgejo/owner/repo/pulls/42",
			"state": "open",
			"body": "Fixes #41",
			"user": {"login": "alice"},
			"repository": {"name": "repo", "owner": {"login": "owner"}}
		}
	}`)
	got, err := ParsePullRequestEvent(payload, "pull_request_opened")
	if err != nil {
		t.Fatalf("ParsePullRequestEvent: %v", err)
	}
	if got.EventType != EventPROpened {
		t.Errorf("EventType = %v, want %v", got.EventType, EventPROpened)
	}
	if got.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42", got.PRNumber)
	}
	if got.Title != "Fix bug" {
		t.Errorf("Title = %q, want 'Fix bug'", got.Title)
	}
	if got.Author != "alice" {
		t.Errorf("Author = %q, want 'alice'", got.Author)
	}
	if got.RepoOwner != "owner" || got.RepoName != "repo" {
		t.Errorf("Repo = %s/%s, want owner/repo", got.RepoOwner, got.RepoName)
	}
}

func TestParsePullRequestEvent_Updated(t *testing.T) {
	payload := []byte(`{"action":"updated","number":7,"pull_request":{"title":"x","state":"open"}}`)
	got, err := ParsePullRequestEvent(payload, "pull_request_updated")
	if err != nil {
		t.Fatalf("ParsePullRequestEvent: %v", err)
	}
	if got.EventType != EventPRUpdated {
		t.Errorf("EventType = %v, want %v", got.EventType, EventPRUpdated)
	}
}

func TestParsePullRequestEvent_Closed(t *testing.T) {
	payload := []byte(`{"action":"closed","number":1,"pull_request":{"title":"x","state":"closed"}}`)
	got, err := ParsePullRequestEvent(payload, "pull_request_closed")
	if err != nil {
		t.Fatalf("ParsePullRequestEvent: %v", err)
	}
	if got.EventType != EventPRClosed {
		t.Errorf("EventType = %v, want %v", got.EventType, EventPRClosed)
	}
}

func TestParsePullRequestEvent_Reviewed(t *testing.T) {
	payload := []byte(`{"action":"reviewed","number":3,"pull_request":{"title":"x","state":"open"}}`)
	got, err := ParsePullRequestEvent(payload, "pull_request_reviewed")
	if err != nil {
		t.Fatalf("ParsePullRequestEvent: %v", err)
	}
	if got.EventType != EventPRReviewed {
		t.Errorf("EventType = %v, want %v", got.EventType, EventPRReviewed)
	}
}

func TestParsePullRequestEvent_Labeled(t *testing.T) {
	payload := []byte(`{"action":"labeled","number":3,"pull_request":{"title":"x","labels":[{"name":"bug"}]}}`)
	got, err := ParsePullRequestEvent(payload, "pull_request_labeled")
	if err != nil {
		t.Fatalf("ParsePullRequestEvent: %v", err)
	}
	if got.EventType != EventPRLabeled {
		t.Errorf("EventType = %v, want %v", got.EventType, EventPRLabeled)
	}
	if len(got.Labels) != 1 || got.Labels[0] != "bug" {
		t.Errorf("Labels = %v, want [bug]", got.Labels)
	}
}

func TestParsePullRequestEvent_Unknown(t *testing.T) {
	payload := []byte(`{"action":"x","number":3,"pull_request":{"title":"x"}}`)
	got, err := ParsePullRequestEvent(payload, "totally_unknown")
	if err != nil {
		t.Fatalf("ParsePullRequestEvent: %v", err)
	}
	if got.EventType != EventUnknown {
		t.Errorf("EventType = %v, want %v", got.EventType, EventUnknown)
	}
}

func TestParsePullRequestEvent_FlatPayload(t *testing.T) {
	// Some Forgejo versions send a flattened payload (no pull_request wrapper).
	payload := []byte(`{"action":"opened","number":99,"title":"Flat","state":"open"}`)
	got, err := ParsePullRequestEvent(payload, "opened")
	if err != nil {
		t.Fatalf("ParsePullRequestEvent: %v", err)
	}
	if got.PRNumber != 99 {
		t.Errorf("PRNumber = %d, want 99", got.PRNumber)
	}
	if got.Title != "Flat" {
		t.Errorf("Title = %q, want 'Flat'", got.Title)
	}
}

func TestParsePullRequestEvent_FullNameFallback(t *testing.T) {
	// Some payloads have FullName="owner/repo" but no Owner sub-object.
	payload := []byte(`{"action":"opened","number":3,"pull_request":{"title":"x","repository":{"full_name":"owner/repo","name":"repo"}}}`)
	got, err := ParsePullRequestEvent(payload, "opened")
	if err != nil {
		t.Fatalf("ParsePullRequestEvent: %v", err)
	}
	if got.RepoOwner != "owner" || got.RepoName != "repo" {
		t.Errorf("Repo = %s/%s, want owner/repo", got.RepoOwner, got.RepoName)
	}
}

func TestParsePullRequestEvent_EmptyPayload(t *testing.T) {
	_, err := ParsePullRequestEvent(nil, "opened")
	if err == nil {
		t.Fatal("expected error for empty payload")
	}
}

func TestParsePullRequestEvent_MalformedJSON(t *testing.T) {
	_, err := ParsePullRequestEvent([]byte("{not json"), "opened")
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

// -----------------------------------------------------------------------------
// mapEventType — wire-format to enum
// -----------------------------------------------------------------------------

func TestMapEventType(t *testing.T) {
	tests := []struct {
		in   string
		want EventType
	}{
		{"pull_request_opened", EventPROpened},
		{"opened", EventPROpened},
		{"pull_request_updated", EventPRUpdated},
		{"updated", EventPRUpdated},
		{"synchronize", EventPRUpdated},
		{"pull_request_closed", EventPRClosed},
		{"closed", EventPRClosed},
		{"pull_request_reviewed", EventPRReviewed},
		{"reviewed", EventPRReviewed},
		{"review_submitted", EventPRReviewed},
		{"pull_request_labeled", EventPRLabeled},
		{"labeled", EventPRLabeled},
		{"something_else", EventUnknown},
		{"", EventUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := mapEventType(tc.in); got != tc.want {
				t.Errorf("mapEventType(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// splitFullName / firstNonEmpty — small helpers
// -----------------------------------------------------------------------------

func TestSplitFullName(t *testing.T) {
	tests := []struct {
		in        string
		wantOwner string
		wantName  string
	}{
		{"owner/repo", "owner", "repo"},
		{"a/b/c", "a", "b/c"}, // splits on first slash only
		{"", "", ""},
		{"single", "", "single"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			owner, name := splitFullName(tc.in)
			if owner != tc.wantOwner || name != tc.wantName {
				t.Errorf("splitFullName(%q) = (%q, %q), want (%q, %q)",
					tc.in, owner, name, tc.wantOwner, tc.wantName)
			}
		})
	}
}

func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		in   []string
		want string
	}{
		{[]string{"a", "b"}, "a"},
		{[]string{"", "b"}, "b"},
		{[]string{"", ""}, ""},
		{nil, ""},
		{[]string{"", "x", "y"}, "x"},
	}
	for _, tc := range tests {
		got := firstNonEmpty(tc.in...)
		if got != tc.want {
			t.Errorf("firstNonEmpty(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// -----------------------------------------------------------------------------
// Handler.ServeHTTP — happy path
// -----------------------------------------------------------------------------

func TestHandler_ValidRequest(t *testing.T) {
	var output bytes.Buffer
	h := &Handler{
		Secret: StaticSecret([]byte("test-secret")),
		Output: &output,
		Path:   "/",
	}

	payload := []byte(`{"action":"opened","number":7,"pull_request":{"title":"x"}}`)
	req := signedRequest(payload, "pull_request_opened", []byte("test-secret"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	// Output should contain a JSONL-encoded PullRequestEvent.
	var parsed PullRequestEvent
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &parsed); err != nil {
		t.Fatalf("output is not valid PullRequestEvent JSON: %v\nraw: %s", err, output.String())
	}
	if parsed.PRNumber != 7 {
		t.Errorf("PRNumber = %d, want 7", parsed.PRNumber)
	}
	if !bytes.HasSuffix(output.Bytes(), []byte("\n")) {
		t.Errorf("output missing trailing newline")
	}
}

func TestHandler_NoPrefixInSignature(t *testing.T) {
	var output bytes.Buffer
	h := &Handler{
		Secret: StaticSecret([]byte("test-secret")),
		Output: &output,
	}

	payload := []byte(`{"action":"opened","number":7}`)
	mac := hmac.New(sha256.New, []byte("test-secret"))
	mac.Write(payload)
	sig := hex.EncodeToString(mac.Sum(nil)) // no prefix
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(payload))
	req.Header.Set(SignatureHeader, sig)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// -----------------------------------------------------------------------------
// Handler.ServeHTTP — error paths
// -----------------------------------------------------------------------------

func TestHandler_MethodNotAllowed(t *testing.T) {
	h := NewHandler([]byte("test-secret"))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestHandler_MissingSignature(t *testing.T) {
	h := NewHandler([]byte("test-secret"))
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("{}")))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestHandler_SignatureMismatch(t *testing.T) {
	h := NewHandler([]byte("test-secret"))
	req := signedRequest([]byte("{}"), "opened", []byte("wrong-secret"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestHandler_MalformedPayload(t *testing.T) {
	h := NewHandler([]byte("test-secret"))
	req := signedRequest([]byte("{not json"), "opened", []byte("test-secret"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandler_EmptyPayload(t *testing.T) {
	h := NewHandler([]byte("test-secret"))
	req := signedRequest(nil, "opened", []byte("test-secret"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// -----------------------------------------------------------------------------
// Handler — default Output falls through to ResponseWriter
// -----------------------------------------------------------------------------

func TestHandler_DefaultOutputIsResponseWriter(t *testing.T) {
	h := NewHandler([]byte("test-secret"))
	payload := []byte(`{"action":"opened","number":7,"pull_request":{"title":"x"}}`)
	req := signedRequest(payload, "opened", []byte("test-secret"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	// The body should contain JSON with PRNumber=7.
	if !strings.Contains(rec.Body.String(), `"pr_number":7`) {
		t.Errorf("response body missing pr_number: %s", rec.Body.String())
	}
}

// -----------------------------------------------------------------------------
// Handler — Output write failure returns 500
// -----------------------------------------------------------------------------

type failingWriter struct{}

func (f *failingWriter) Write(p []byte) (int, error) {
	return 0, io.ErrShortWrite
}

func TestHandler_OutputWriteFailure(t *testing.T) {
	h := &Handler{
		Secret: StaticSecret([]byte("test-secret")),
		Output: &failingWriter{},
	}
	payload := []byte(`{"action":"opened","number":7,"pull_request":{"title":"x"}}`)
	req := signedRequest(payload, "opened", []byte("test-secret"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// -----------------------------------------------------------------------------
// NewHandler defaults
// -----------------------------------------------------------------------------

func TestNewHandler_Defaults(t *testing.T) {
	h := NewHandler([]byte("secret"))
	if h.Path != "/" {
		t.Errorf("Path = %q, want '/'", h.Path)
	}
	if h.Secret == nil {
		t.Error("Secret should be set")
	}
}
