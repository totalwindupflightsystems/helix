package forgejo

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ============================================================================
// Test Helpers
// ============================================================================

type recordingHandler struct {
	prOpened   int
	prUpdated  int
	prClosed   int
	push       int
	review     int
	lastResult WebhookResult
}

func (r *recordingHandler) OnPROpened(info PREventInfo) WebhookResult {
	r.prOpened++
	r.lastResult = WebhookResult{Action: ActionProcessed, Message: "pr opened processed"}
	return r.lastResult
}
func (r *recordingHandler) OnPRUpdated(info PREventInfo) WebhookResult {
	r.prUpdated++
	r.lastResult = WebhookResult{Action: ActionProcessed, Message: "pr updated processed"}
	return r.lastResult
}
func (r *recordingHandler) OnPRClosed(info PREventInfo) WebhookResult {
	r.prClosed++
	r.lastResult = WebhookResult{Action: ActionProcessed, Message: "pr closed processed"}
	return r.lastResult
}
func (r *recordingHandler) OnPush(info PushEventInfo) WebhookResult {
	r.push++
	r.lastResult = WebhookResult{Action: ActionProcessed, Message: "push processed"}
	return r.lastResult
}
func (r *recordingHandler) OnReviewSubmitted(info ReviewEventInfo) WebhookResult {
	r.review++
	r.lastResult = WebhookResult{Action: ActionProcessed, Message: "review processed"}
	return r.lastResult
}

func makeWebhookBody(action string, prNumber int, extraFields map[string]interface{}) []byte {
	m := map[string]interface{}{
		"action": action,
	}
	if prNumber > 0 {
		m["number"] = prNumber
	}
	for k, v := range extraFields {
		m[k] = v
	}
	body, _ := json.Marshal(m)
	return body
}

func signBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// ============================================================================
// Tests — MapEventType
// ============================================================================

func TestMapEventType_PullRequest(t *testing.T) {
	if et := MapEventType("pull_request"); et != EventPROpened {
		t.Errorf("expected EventPROpened, got %s", et)
	}
}

func TestMapEventType_Push(t *testing.T) {
	if et := MapEventType("push"); et != EventPush {
		t.Errorf("expected EventPush, got %s", et)
	}
}

func TestMapEventType_Unknown(t *testing.T) {
	if et := MapEventType("foobar"); et != EventUnknown {
		t.Errorf("expected EventUnknown, got %s", et)
	}
}

func TestMapEventType_Empty(t *testing.T) {
	if et := MapEventType(""); et != EventUnknown {
		t.Errorf("expected EventUnknown, got %s", et)
	}
}

func TestParseEventType_ForgejoHeader(t *testing.T) {
	header := http.Header{}
	header.Set("X-Forgejo-Event", "push")
	if et := ParseEventType(header); et != EventPush {
		t.Errorf("expected EventPush, got %s", et)
	}
}

func TestParseEventType_GiteaHeader(t *testing.T) {
	header := http.Header{}
	header.Set("X-Gitea-Event", "push")
	if et := ParseEventType(header); et != EventPush {
		t.Errorf("expected EventPush, got %s", et)
	}
}

func TestParseEventType_NoHeader(t *testing.T) {
	header := http.Header{}
	if et := ParseEventType(header); et != EventUnknown {
		t.Errorf("expected EventUnknown, got %s", et)
	}
}

// ============================================================================
// Tests — ParseWebhook
// ============================================================================

func TestParseWebhook_Valid(t *testing.T) {
	body := makeWebhookBody("opened", 42, nil)
	payload, err := ParseWebhook(body)
	if err != nil {
		t.Fatalf("ParseWebhook failed: %v", err)
	}
	if payload.Action != "opened" {
		t.Errorf("action = %s, want opened", payload.Action)
	}
	if payload.PRNumber != 42 {
		t.Errorf("number = %d, want 42", payload.PRNumber)
	}
}

func TestParseWebhook_InvalidJSON(t *testing.T) {
	_, err := ParseWebhook([]byte("{invalid"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ============================================================================
// Tests — ParsePRInfo
// ============================================================================

func TestParsePRInfo_WithPRData(t *testing.T) {
	prJSON, _ := json.Marshal(map[string]interface{}{
		"url":   "https://forgejo.local/repo/pulls/1",
		"title": "Add feature",
		"user":  map[string]string{"login": "agent-001"},
	})
	repoJSON, _ := json.Marshal(map[string]interface{}{
		"name":  "helix",
		"owner": map[string]string{"login": "kara"},
	})
	payload := &WebhookPayload{
		Action:      "opened",
		PRNumber:    1,
		PullRequest: prJSON,
		Repository:  repoJSON,
	}
	info, err := ParsePRInfo(payload)
	if err != nil {
		t.Fatalf("ParsePRInfo failed: %v", err)
	}
	if info.PRURL != "https://forgejo.local/repo/pulls/1" {
		t.Errorf("URL = %s", info.PRURL)
	}
	if info.Title != "Add feature" {
		t.Errorf("Title = %s", info.Title)
	}
	if info.Author != "agent-001" {
		t.Errorf("Author = %s", info.Author)
	}
	if info.RepoName != "helix" {
		t.Errorf("RepoName = %s", info.RepoName)
	}
	if info.RepoOwner != "kara" {
		t.Errorf("RepoOwner = %s", info.RepoOwner)
	}
}

func TestParsePRInfo_NoPRData(t *testing.T) {
	payload := &WebhookPayload{}
	_, err := ParsePRInfo(payload)
	if err == nil {
		t.Error("expected error for no PR data")
	}
}

func TestParsePRInfo_PRNumberOnly(t *testing.T) {
	payload := &WebhookPayload{PRNumber: 42}
	info, err := ParsePRInfo(payload)
	if err != nil {
		t.Fatalf("ParsePRInfo failed: %v", err)
	}
	if info.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42", info.PRNumber)
	}
}

// ============================================================================
// Tests — ParsePushInfo
// ============================================================================

func TestParsePushInfo_WithCommits(t *testing.T) {
	commitsJSON, _ := json.Marshal([]map[string]string{
		{"id": "abc123"},
		{"id": "def456"},
	})
	repoJSON, _ := json.Marshal(map[string]interface{}{
		"name":  "helix",
		"owner": map[string]string{"login": "kara"},
	})
	payload := &WebhookPayload{
		Commits:    commitsJSON,
		Repository: repoJSON,
	}
	info, err := ParsePushInfo(payload)
	if err != nil {
		t.Fatalf("ParsePushInfo failed: %v", err)
	}
	if len(info.Commits) != 2 {
		t.Errorf("expected 2 commits, got %d", len(info.Commits))
	}
	if info.RepoName != "helix" {
		t.Errorf("RepoName = %s", info.RepoName)
	}
}

func TestParsePushInfo_Empty(t *testing.T) {
	payload := &WebhookPayload{}
	info, err := ParsePushInfo(payload)
	if err != nil {
		t.Fatalf("ParsePushInfo failed: %v", err)
	}
	if info == nil {
		t.Error("expected non-nil info")
	}
}

// ============================================================================
// Tests — ParseReviewInfo
// ============================================================================

func TestParseReviewInfo_WithReview(t *testing.T) {
	reviewJSON, _ := json.Marshal(map[string]interface{}{
		"reviewer": map[string]string{"login": "agent-reviewer"},
		"type":     "approve",
		"body":     "LGTM",
		"state":    "approved",
	})
	payload := &WebhookPayload{
		PRNumber: 1,
		Review:   reviewJSON,
	}
	info, err := ParseReviewInfo(payload)
	if err != nil {
		t.Fatalf("ParseReviewInfo failed: %v", err)
	}
	if info.Reviewer != "agent-reviewer" {
		t.Errorf("Reviewer = %s", info.Reviewer)
	}
	if info.Body != "LGTM" {
		t.Errorf("Body = %s", info.Body)
	}
	if info.State != "approved" {
		t.Errorf("State = %s", info.State)
	}
}

// ============================================================================
// Tests — Signature Verification
// ============================================================================

func TestVerifySignature_NoSecret(t *testing.T) {
	h := NewWebhookHandler()
	header := http.Header{}
	body := []byte("test")
	if !h.VerifySignature(header, body) {
		t.Error("should pass with no secret configured")
	}
}

func TestVerifySignature_Valid(t *testing.T) {
	secret := "my-secret"
	body := []byte(`{"action":"opened","number":1}`)
	sig := signBody(secret, body)
	h := NewWebhookHandler(WithWebhookSecret(secret))
	header := http.Header{}
	header.Set("X-Forgejo-Signature", sig)
	if !h.VerifySignature(header, body) {
		t.Error("should pass with valid signature")
	}
}

func TestVerifySignature_Invalid(t *testing.T) {
	h := NewWebhookHandler(WithWebhookSecret("secret"))
	header := http.Header{}
	header.Set("X-Forgejo-Signature", "badsig")
	if h.VerifySignature(header, []byte("body")) {
		t.Error("should fail with invalid signature")
	}
}

func TestVerifySignature_NoSignatureHeader(t *testing.T) {
	h := NewWebhookHandler(WithWebhookSecret("secret"))
	header := http.Header{}
	if h.VerifySignature(header, []byte("body")) {
		t.Error("should fail with no signature header when secret is set")
	}
}

func TestVerifySignature_GiteaHeader(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"action":"push"}`)
	sig := signBody(secret, body)
	h := NewWebhookHandler(WithWebhookSecret(secret))
	header := http.Header{}
	header.Set("X-Gitea-Signature", sig)
	if !h.VerifySignature(header, body) {
		t.Error("should accept X-Gitea-Signature header")
	}
}

// ============================================================================
// Tests — HandleBody
// ============================================================================

func TestHandleBody_UnknownEvent(t *testing.T) {
	h := NewWebhookHandler(WithEventHandler(NoOpHandler{}))
	header := http.Header{}
	header.Set("X-Forgejo-Event", "something_random")
	result := h.HandleBody(header, []byte("{}"))
	if result.Action != ActionSkipped {
		t.Errorf("expected skipped, got %s", result.Action)
	}
}

func TestHandleBody_InvalidJSON(t *testing.T) {
	h := NewWebhookHandler(WithEventHandler(NoOpHandler{}))
	header := http.Header{}
	header.Set("X-Forgejo-Event", "push")
	result := h.HandleBody(header, []byte("{invalid"))
	if result.Action != ActionError {
		t.Errorf("expected error, got %s", result.Action)
	}
}

func TestHandleBody_NoHandler(t *testing.T) {
	h := NewWebhookHandler()
	header := http.Header{}
	header.Set("X-Forgejo-Event", "push")
	result := h.HandleBody(header, []byte("{}"))
	if result.Action != ActionSkipped {
		t.Errorf("expected skipped (no handler), got %s", result.Action)
	}
}

func TestHandleBody_SignatureFailure(t *testing.T) {
	h := NewWebhookHandler(WithWebhookSecret("secret"), WithEventHandler(NoOpHandler{}))
	header := http.Header{}
	header.Set("X-Forgejo-Event", "push")
	result := h.HandleBody(header, []byte("{}"))
	if result.Action != ActionError {
		t.Errorf("expected error (sig fail), got %s", result.Action)
	}
	if result.Message == "" {
		t.Error("expected error message")
	}
}

func TestHandleBody_PROpened(t *testing.T) {
	rh := &recordingHandler{}
	h := NewWebhookHandler(WithEventHandler(rh))
	header := http.Header{}
	header.Set("X-Forgejo-Event", "pull_request")
	body := makeWebhookBody("opened", 42, nil)
	result := h.HandleBody(header, body)
	if result.Action != ActionProcessed {
		t.Errorf("expected processed, got %s", result.Action)
	}
	if rh.prOpened != 1 {
		t.Errorf("expected prOpened=1, got %d", rh.prOpened)
	}
}

func TestHandleBody_PRUpdated(t *testing.T) {
	rh := &recordingHandler{}
	h := NewWebhookHandler(WithEventHandler(rh))
	header := http.Header{}
	header.Set("X-Forgejo-Event", "pull_request")
	body := makeWebhookBody("synchronized", 42, nil)
	result := h.HandleBody(header, body)
	if result.Action != ActionProcessed {
		t.Errorf("expected processed, got %s", result.Action)
	}
	if rh.prUpdated != 1 {
		t.Errorf("expected prUpdated=1, got %d", rh.prUpdated)
	}
}

func TestHandleBody_PRClosed(t *testing.T) {
	rh := &recordingHandler{}
	h := NewWebhookHandler(WithEventHandler(rh))
	header := http.Header{}
	header.Set("X-Forgejo-Event", "pull_request")
	body := makeWebhookBody("closed", 42, nil)
	result := h.HandleBody(header, body)
	if result.Action != ActionProcessed {
		t.Errorf("expected processed, got %s", result.Action)
	}
	if rh.prClosed != 1 {
		t.Errorf("expected prClosed=1, got %d", rh.prClosed)
	}
}

func TestHandleBody_Push(t *testing.T) {
	rh := &recordingHandler{}
	h := NewWebhookHandler(WithEventHandler(rh))
	header := http.Header{}
	header.Set("X-Forgejo-Event", "push")
	body := []byte(`{"commits":[{"id":"abc"},{"id":"def"}]}`)
	result := h.HandleBody(header, body)
	if result.Action != ActionProcessed {
		t.Errorf("expected processed, got %s", result.Action)
	}
	if rh.push != 1 {
		t.Errorf("expected push=1, got %d", rh.push)
	}
	if result.PushInfo == nil || len(result.PushInfo.Commits) != 2 {
		t.Errorf("expected 2 commits in push info")
	}
}

func TestHandleBody_Review(t *testing.T) {
	rh := &recordingHandler{}
	h := NewWebhookHandler(WithEventHandler(rh))
	header := http.Header{}
	header.Set("X-Forgejo-Event", "review")
	reviewJSON, _ := json.Marshal(map[string]interface{}{
		"reviewer": map[string]string{"login": "reviewer-1"},
		"body":     "approved",
		"state":    "APPROVED",
	})
	body, _ := json.Marshal(map[string]interface{}{
		"number": 5,
		"review": json.RawMessage(reviewJSON),
	})
	result := h.HandleBody(header, body)
	if result.Action != ActionProcessed {
		t.Errorf("expected processed, got %s", result.Action)
	}
	if rh.review != 1 {
		t.Errorf("expected review=1, got %d", rh.review)
	}
	if result.ReviewInfo == nil || result.ReviewInfo.Reviewer != "reviewer-1" {
		t.Errorf("expected reviewer info, got %+v", result.ReviewInfo)
	}
}

// ============================================================================
// Tests — HandleRequest (full HTTP)
// ============================================================================

func TestHandleRequest_PROpened(t *testing.T) {
	rh := &recordingHandler{}
	h := NewWebhookHandler(WithEventHandler(rh))

	body := makeWebhookBody("opened", 1, nil)
	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Forgejo-Event", "pull_request")
	req.Header.Set("Content-Type", "application/json")

	result := h.HandleRequest(req)
	if result.Action != ActionProcessed {
		t.Errorf("expected processed, got %s", result.Action)
	}
}

func TestHandleRequest_InvalidBody(t *testing.T) {
	h := NewWebhookHandler(WithEventHandler(NoOpHandler{}))
	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte("invalid")))
	req.Header.Set("X-Forgejo-Event", "push")
	result := h.HandleRequest(req)
	if result.Action != ActionError {
		t.Errorf("expected error, got %s", result.Action)
	}
}

// ============================================================================
// Tests — NoOpHandler
// ============================================================================

func TestNoOpHandler_AllMethods(t *testing.T) {
	n := NoOpHandler{}
	r1 := n.OnPROpened(PREventInfo{})
	r2 := n.OnPRUpdated(PREventInfo{})
	r3 := n.OnPRClosed(PREventInfo{})
	r4 := n.OnPush(PushEventInfo{})
	r5 := n.OnReviewSubmitted(ReviewEventInfo{})

	for i, r := range []WebhookResult{r1, r2, r3, r4, r5} {
		if r.Action != ActionSkipped {
			t.Errorf("handler %d: expected skipped, got %s", i, r.Action)
		}
	}
}

// ============================================================================
// Tests — WebhookResult helpers
// ============================================================================

func TestWebhookResult_WithPRInfo(t *testing.T) {
	result := WebhookResult{
		EventType: EventPROpened,
		Action:    ActionProcessed,
		PRInfo:    &PREventInfo{PRNumber: 42, Title: "Test"},
	}
	if result.PRInfo == nil || result.PRInfo.PRNumber != 42 {
		t.Error("PRInfo should be set")
	}
}

func TestWebhookResult_WithPushInfo(t *testing.T) {
	result := WebhookResult{
		EventType: EventPush,
		Action:    ActionProcessed,
		PushInfo:  &PushEventInfo{Commits: []string{"abc"}},
	}
	if result.PushInfo == nil || len(result.PushInfo.Commits) != 1 {
		t.Error("PushInfo should be set")
	}
}

// ============================================================================
// Tests — Handler construction
// ============================================================================

func TestNewWebhookHandler_Default(t *testing.T) {
	h := NewWebhookHandler()
	if h == nil {
		t.Fatal("NewWebhookHandler returned nil")
	}
	if h.secret != nil {
		t.Error("default secret should be nil")
	}
	if h.handler != nil {
		t.Error("default handler should be nil")
	}
}

func TestNewWebhookHandler_WithSecret(t *testing.T) {
	h := NewWebhookHandler(WithWebhookSecret("my-secret"))
	if string(h.secret) != "my-secret" {
		t.Errorf("secret mismatch")
	}
}

func TestNewWebhookHandler_WithHandler(t *testing.T) {
	rh := &recordingHandler{}
	h := NewWebhookHandler(WithEventHandler(rh))
	if h.handler == nil {
		t.Error("handler should be set")
	}
}

// ============================================================================
// Tests — Dispatch with valid signature
// ============================================================================

func TestHandleBody_WithSignature_PROpened(t *testing.T) {
	rh := &recordingHandler{}
	secret := "webhook-secret"
	h := NewWebhookHandler(WithWebhookSecret(secret), WithEventHandler(rh))

	body := makeWebhookBody("opened", 7, nil)
	sig := signBody(secret, body)

	header := http.Header{}
	header.Set("X-Forgejo-Event", "pull_request")
	header.Set("X-Forgejo-Signature", sig)

	result := h.HandleBody(header, body)
	if result.Action != ActionProcessed {
		t.Errorf("expected processed, got %s (msg: %s)", result.Action, result.Message)
	}
	if rh.prOpened != 1 {
		t.Errorf("expected prOpened=1, got %d", rh.prOpened)
	}
}

func TestHandleBody_PRReopened(t *testing.T) {
	rh := &recordingHandler{}
	h := NewWebhookHandler(WithEventHandler(rh))
	header := http.Header{}
	header.Set("X-Forgejo-Event", "pull_request")
	body := makeWebhookBody("reopened", 42, nil)
	result := h.HandleBody(header, body)
	if result.Action != ActionProcessed {
		t.Errorf("expected processed, got %s", result.Action)
	}
	if rh.prOpened != 1 {
		t.Errorf("reopened should call OnPROpened, got prOpened=%d", rh.prOpened)
	}
}
