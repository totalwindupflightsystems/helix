package forgejo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Test helpers
// ============================================================================

func makeMockClient(t *testing.T, handler http.HandlerFunc) *Client {
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewClient(server.URL, "admin", "password").
		WithHTTPClient(server.Client())
}

func now() time.Time {
	return time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
}

func sampleVerdict(decision string) *ReviewVerdict {
	return &ReviewVerdict{
		Decision:     decision,
		Confidence:   0.95,
		ModelsUsed:   []string{"glm-5.2", "minimax-m3", "deepseek-v4-pro"},
		Findings:     []string{"Edge case in nil pointer handling", "Missing timeout on HTTP call"},
		Consensus:    "unanimous",
		BiasStripped: true,
		Timestamp:    now(),
	}
}

func sampleDeployment() *DeploymentStatus {
	return &DeploymentStatus{
		Phase:        "canary",
		TrafficPct:   10,
		AgentID:      "agent-001",
		ContractName: "auth-session-v2",
		StartTime:    now(),
		Progress:     0.45,
		Breaches:     0,
	}
}

// ============================================================================
// FormatReviewComment
// ============================================================================

func TestFormatReviewComment_Approve(t *testing.T) {
	v := sampleVerdict("APPROVE")
	body := FormatReviewComment(v)

	if !strings.Contains(body, "✅") {
		t.Error("expected ✅ emoji for APPROVE")
	}
	if !strings.Contains(body, "APPROVE") {
		t.Error("expected APPROVE in body")
	}
	if !strings.Contains(body, "95.0%") {
		t.Error("expected confidence percentage")
	}
	if !strings.Contains(body, "glm-5.2") {
		t.Error("expected model name")
	}
	if !strings.Contains(body, "Bias Stripping") {
		t.Error("expected bias stripping info")
	}
}

func TestFormatReviewComment_Reject(t *testing.T) {
	v := sampleVerdict("REJECT")
	body := FormatReviewComment(v)

	if !strings.Contains(body, "❌") {
		t.Error("expected ❌ emoji for REJECT")
	}
}

func TestFormatReviewComment_RequestChanges(t *testing.T) {
	v := sampleVerdict("REQUEST_CHANGES")
	body := FormatReviewComment(v)

	if !strings.Contains(body, "⚠️") {
		t.Error("expected ⚠️ emoji for REQUEST_CHANGES")
	}
}

func TestFormatReviewComment_Findings(t *testing.T) {
	v := sampleVerdict("APPROVE")
	v.Findings = []string{"Issue A", "Issue B", "Issue C"}
	body := FormatReviewComment(v)

	if !strings.Contains(body, "1. Issue A") {
		t.Error("expected numbered finding")
	}
	if !strings.Contains(body, "3. Issue C") {
		t.Error("expected third numbered finding")
	}
}

func TestFormatReviewComment_NoFindings(t *testing.T) {
	v := sampleVerdict("APPROVE")
	v.Findings = nil
	body := FormatReviewComment(v)

	// Should not contain findings section header when empty.
	if strings.Contains(body, "#### Findings") {
		t.Error("should not contain findings section when empty")
	}
}

func TestFormatReviewComment_NoBiasStripping(t *testing.T) {
	v := sampleVerdict("APPROVE")
	v.BiasStripped = false
	body := FormatReviewComment(v)

	if strings.Contains(body, "Bias Stripping") {
		t.Error("should not contain bias stripping info when false")
	}
}

func TestFormatReviewComment_Timestamp(t *testing.T) {
	v := sampleVerdict("APPROVE")
	v.Timestamp = time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	body := FormatReviewComment(v)

	if !strings.Contains(body, "2026-06-29T12:00:00Z") {
		t.Error("expected RFC3339 timestamp")
	}
}

func TestFormatReviewComment_HelixFooter(t *testing.T) {
	v := sampleVerdict("APPROVE")
	body := FormatReviewComment(v)

	if !strings.Contains(body, "Helix Adversarial Review Pipeline") {
		t.Error("expected Helix footer")
	}
}

// ============================================================================
// PostReviewComment
// ============================================================================

func TestPostReviewComment_Approve(t *testing.T) {
	var capturedBody string
	var capturedPath string
	client := makeMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	})

	mgr := NewPRStatusManager(client)
	err := mgr.PostReviewComment(context.Background(), "owner", "repo", 42, sampleVerdict("APPROVE"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(capturedPath, "pulls/42/reviews") {
		t.Errorf("unexpected path: %s", capturedPath)
	}

	var req CreatePRReviewRequest
	if err := json.Unmarshal([]byte(capturedBody), &req); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if req.Event != "APPROVED" {
		t.Errorf("expected event APPROVED, got %s", req.Event)
	}
	if !strings.Contains(req.Body, "APPROVE") {
		t.Error("expected review body to contain APPROVE")
	}
}

func TestPostReviewComment_Reject(t *testing.T) {
	client := makeMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mgr := NewPRStatusManager(client)
	err := mgr.PostReviewComment(context.Background(), "owner", "repo", 42, sampleVerdict("REJECT"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPostReviewComment_RequestChanges(t *testing.T) {
	client := makeMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mgr := NewPRStatusManager(client)
	err := mgr.PostReviewComment(context.Background(), "owner", "repo", 42, sampleVerdict("REQUEST_CHANGES"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPostReviewComment_APIError(t *testing.T) {
	client := makeMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	})

	mgr := NewPRStatusManager(client)
	err := mgr.PostReviewComment(context.Background(), "owner", "repo", 42, sampleVerdict("APPROVE"))
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// ============================================================================
// PostCommitStatus
// ============================================================================

func TestPostCommitStatus(t *testing.T) {
	var capturedPath string
	var capturedBody string
	client := makeMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.WriteHeader(http.StatusCreated)
	})

	mgr := NewPRStatusManager(client)
	err := mgr.PostCommitStatus(context.Background(), "owner", "repo", "abc123", CommitStatus{
		State:       StatusStateSuccess,
		Description: "All checks passed",
		Context:     "helix/test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(capturedPath, "statuses/abc123") {
		t.Errorf("unexpected path: %s", capturedPath)
	}

	var status CommitStatus
	if err := json.Unmarshal([]byte(capturedBody), &status); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if status.State != StatusStateSuccess {
		t.Errorf("expected success state, got %s", status.State)
	}
}

func TestPostReviewStatus_Approve(t *testing.T) {
	var capturedBody string
	client := makeMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.WriteHeader(http.StatusCreated)
	})

	mgr := NewPRStatusManager(client)
	err := mgr.PostReviewStatus(context.Background(), "owner", "repo", "abc", sampleVerdict("APPROVE"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var status CommitStatus
	if err := json.Unmarshal([]byte(capturedBody), &status); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if status.State != StatusStateSuccess {
		t.Errorf("expected success for APPROVE, got %s", status.State)
	}
}

func TestPostReviewStatus_Reject(t *testing.T) {
	var capturedBody string
	client := makeMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.WriteHeader(http.StatusCreated)
	})

	mgr := NewPRStatusManager(client)
	err := mgr.PostReviewStatus(context.Background(), "owner", "repo", "abc", sampleVerdict("REJECT"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var status CommitStatus
	if err := json.Unmarshal([]byte(capturedBody), &status); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if status.State != StatusStateFailure {
		t.Errorf("expected failure for REJECT, got %s", status.State)
	}
}

func TestPostReviewStatus_RequestChanges(t *testing.T) {
	var capturedBody string
	client := makeMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.WriteHeader(http.StatusCreated)
	})

	mgr := NewPRStatusManager(client)
	err := mgr.PostReviewStatus(context.Background(), "owner", "repo", "abc", sampleVerdict("REQUEST_CHANGES"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var status CommitStatus
	if err := json.Unmarshal([]byte(capturedBody), &status); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if status.State != StatusStateWarning {
		t.Errorf("expected warning for REQUEST_CHANGES, got %s", status.State)
	}
}

// ============================================================================
// PostDeploymentStatus
// ============================================================================

func TestPostDeploymentStatus_Canary(t *testing.T) {
	var capturedBody string
	client := makeMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.WriteHeader(http.StatusCreated)
	})

	mgr := NewPRStatusManager(client)
	dep := sampleDeployment()
	err := mgr.PostDeploymentStatus(context.Background(), "owner", "repo", "abc", dep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var status CommitStatus
	if err := json.Unmarshal([]byte(capturedBody), &status); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if status.State != StatusStatePending {
		t.Errorf("expected pending for canary, got %s", status.State)
	}
	if !strings.Contains(status.Description, "canary") {
		t.Errorf("expected canary in description, got %s", status.Description)
	}
}

func TestPostDeploymentStatus_Promoted(t *testing.T) {
	var capturedBody string
	client := makeMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.WriteHeader(http.StatusCreated)
	})

	mgr := NewPRStatusManager(client)
	dep := sampleDeployment()
	dep.Phase = "promoted"
	err := mgr.PostDeploymentStatus(context.Background(), "owner", "repo", "abc", dep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var status CommitStatus
	if err := json.Unmarshal([]byte(capturedBody), &status); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if status.State != StatusStateSuccess {
		t.Errorf("expected success for promoted, got %s", status.State)
	}
}

func TestPostDeploymentStatus_RolledBack(t *testing.T) {
	var capturedBody string
	client := makeMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.WriteHeader(http.StatusCreated)
	})

	mgr := NewPRStatusManager(client)
	dep := sampleDeployment()
	dep.Phase = "rolled_back"
	dep.Breaches = 3
	err := mgr.PostDeploymentStatus(context.Background(), "owner", "repo", "abc", dep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var status CommitStatus
	if err := json.Unmarshal([]byte(capturedBody), &status); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if status.State != StatusStateError {
		t.Errorf("expected error for rolled_back, got %s", status.State)
	}
	if !strings.Contains(status.Description, "rolled back") {
		t.Errorf("expected 'rolled back' in description, got %s", status.Description)
	}
}

func TestPostDeploymentStatus_CanaryWithBreaches(t *testing.T) {
	var capturedBody string
	client := makeMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.WriteHeader(http.StatusCreated)
	})

	mgr := NewPRStatusManager(client)
	dep := sampleDeployment()
	dep.Breaches = 2
	err := mgr.PostDeploymentStatus(context.Background(), "owner", "repo", "abc", dep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var status CommitStatus
	if err := json.Unmarshal([]byte(capturedBody), &status); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if status.State != StatusStateWarning {
		t.Errorf("expected warning for canary with breaches, got %s", status.State)
	}
}

// ============================================================================
// ParsePRReviews
// ============================================================================

func TestParsePRReviews_HelixReviews(t *testing.T) {
	reviews := []PRReview{
		{Body: "### ✅ Helix Review: `APPROVE`\n\n**Confidence:** 95.0%\n\n**Consensus:** unanimous"},
		{Body: "### ❌ Helix Review: `REJECT`\n\n**Confidence:** 88.0%"},
		{Body: "LGTM, looks good to me!"},
	}

	parsed := ParsePRReviews(reviews)
	if len(parsed) != 2 {
		t.Fatalf("expected 2 Helix reviews, got %d", len(parsed))
	}

	if parsed[0].Decision != "APPROVE" {
		t.Errorf("expected APPROVE, got %s", parsed[0].Decision)
	}
	if parsed[0].Confidence < 0.94 || parsed[0].Confidence > 0.96 {
		t.Errorf("expected ~0.95 confidence, got %f", parsed[0].Confidence)
	}
	if !parsed[0].IsHelix {
		t.Error("expected IsHelix to be true")
	}

	if parsed[1].Decision != "REJECT" {
		t.Errorf("expected REJECT, got %s", parsed[1].Decision)
	}
}

func TestParsePRReviews_NonHelixReviews(t *testing.T) {
	reviews := []PRReview{
		{Body: "Looks good!"},
		{Body: "Need to fix the tests"},
	}

	parsed := ParsePRReviews(reviews)
	if len(parsed) != 0 {
		t.Errorf("expected 0 parsed reviews (non-Helix), got %d", len(parsed))
	}
}

func TestParsePRReviews_Empty(t *testing.T) {
	parsed := ParsePRReviews(nil)
	if len(parsed) != 0 {
		t.Errorf("expected 0 for empty input")
	}
}

func TestParseReviewBody_ExtractConfidence(t *testing.T) {
	body := "### ✅ Helix Review: `APPROVE`\n\n**Confidence:** 87.5%\n\nSome text"
	p := ParseReviewBody(body)
	if p == nil {
		t.Fatal("expected non-nil")
	}
	if p.Confidence < 0.87 || p.Confidence > 0.88 {
		t.Errorf("expected ~0.875 confidence, got %f", p.Confidence)
	}
}

func TestParseReviewBody_NoConfidence(t *testing.T) {
	body := "### ✅ Helix Review: `APPROVE`\n\nNo confidence data"
	p := ParseReviewBody(body)
	if p == nil {
		t.Fatal("expected non-nil")
	}
	if p.Confidence != 0 {
		t.Errorf("expected 0 confidence when not found, got %f", p.Confidence)
	}
}

// ============================================================================
// FormatDeploymentComment
// ============================================================================

func TestFormatDeploymentComment_Canary(t *testing.T) {
	dep := sampleDeployment()
	body := FormatDeploymentComment(dep)

	if !strings.Contains(body, "🐤") {
		t.Error("expected canary emoji")
	}
	if !strings.Contains(body, "canary") {
		t.Error("expected phase name")
	}
	if !strings.Contains(body, "agent-001") {
		t.Error("expected agent ID")
	}
	if !strings.Contains(body, "auth-session-v2") {
		t.Error("expected contract name")
	}
	if !strings.Contains(body, "10%") {
		t.Error("expected traffic percentage")
	}
}

func TestFormatDeploymentComment_Shadow(t *testing.T) {
	dep := sampleDeployment()
	dep.Phase = "shadow"
	dep.TrafficPct = 0
	body := FormatDeploymentComment(dep)

	if !strings.Contains(body, "🌑") {
		t.Error("expected shadow emoji")
	}
}

func TestFormatDeploymentComment_Promoted(t *testing.T) {
	dep := sampleDeployment()
	dep.Phase = "promoted"
	dep.Progress = 1.0
	body := FormatDeploymentComment(dep)

	if !strings.Contains(body, "✅") {
		t.Error("expected promoted emoji")
	}
}

func TestFormatDeploymentComment_RolledBack(t *testing.T) {
	dep := sampleDeployment()
	dep.Phase = "rolled_back"
	dep.Breaches = 5
	dep.LastError = "behavior contract breach: success_rate < 0.999"
	body := FormatDeploymentComment(dep)

	if !strings.Contains(body, "⏪") {
		t.Error("expected rollback emoji")
	}
	if !strings.Contains(body, "5") {
		t.Error("expected breach count")
	}
	if !strings.Contains(body, "behavior contract") {
		t.Error("expected error message")
	}
}

func TestFormatDeploymentComment_ProgressBar(t *testing.T) {
	dep := sampleDeployment()
	dep.Progress = 0.5
	body := FormatDeploymentComment(dep)

	if !strings.Contains(body, "█") {
		t.Error("expected progress bar filled chars")
	}
	if !strings.Contains(body, "░") {
		t.Error("expected progress bar empty chars")
	}
}

func TestPostDeploymentComment(t *testing.T) {
	var capturedBody string
	client := makeMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.WriteHeader(http.StatusOK)
	})

	mgr := NewPRStatusManager(client)
	err := mgr.PostDeploymentComment(context.Background(), "owner", "repo", 42, sampleDeployment())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var req CreatePRReviewRequest
	if err := json.Unmarshal([]byte(capturedBody), &req); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if req.Event != "COMMENT" {
		t.Errorf("expected COMMENT event, got %s", req.Event)
	}
	if !strings.Contains(req.Body, "canary") {
		t.Error("expected deployment comment body")
	}
}

// ============================================================================
// Helpers
// ============================================================================

func TestVerdictIcon(t *testing.T) {
	if verdictIcon("APPROVE") != "✅" {
		t.Error("expected ✅")
	}
	if verdictIcon("REJECT") != "❌" {
		t.Error("expected ❌")
	}
	if verdictIcon("REQUEST_CHANGES") != "⚠️" {
		t.Error("expected ⚠️")
	}
	if verdictIcon("UNKNOWN") != "ℹ️" {
		t.Error("expected ℹ️")
	}
}

func TestVerdictToReviewEvent(t *testing.T) {
	if verdictToReviewEvent("APPROVE") != "APPROVED" {
		t.Error("expected APPROVED")
	}
	if verdictToReviewEvent("REJECT") != "REQUEST_CHANGES" {
		t.Error("expected REQUEST_CHANGES")
	}
	if verdictToReviewEvent("COMMENT") != "COMMENT" {
		t.Error("expected COMMENT")
	}
}

func TestRenderProgressBar(t *testing.T) {
	bar := renderProgressBar(0.5)
	if !strings.Contains(bar, "█") || !strings.Contains(bar, "░") {
		t.Error("expected both filled and empty chars")
	}
	// 50% → 5 filled, 5 empty.
	filledCount := strings.Count(bar, "█")
	if filledCount != 5 {
		t.Errorf("expected 5 filled, got %d", filledCount)
	}
}

func TestRenderProgressBar_Full(t *testing.T) {
	bar := renderProgressBar(1.0)
	filledCount := strings.Count(bar, "█")
	if filledCount != 10 {
		t.Errorf("expected 10 filled, got %d", filledCount)
	}
}

func TestRenderProgressBar_Empty(t *testing.T) {
	bar := renderProgressBar(0)
	filledCount := strings.Count(bar, "█")
	if filledCount != 0 {
		t.Errorf("expected 0 filled, got %d", filledCount)
	}
}

func TestRenderProgressBar_Clamped(t *testing.T) {
	// Negative should clamp to 0.
	bar := renderProgressBar(-0.5)
	if strings.Count(bar, "█") != 0 {
		t.Error("expected 0 filled for negative progress")
	}
	// >1 should clamp to 1.
	bar = renderProgressBar(1.5)
	if strings.Count(bar, "█") != 10 {
		t.Error("expected 10 filled for >1 progress")
	}
}

func TestEscapePath(t *testing.T) {
	if escapePath("owner/repo") != "owner%2Frepo" {
		t.Error("expected / to be escaped")
	}
	if escapePath("hello world") != "hello%20world" {
		t.Error("expected space to be escaped")
	}
}

func TestDeploymentPhaseIcon(t *testing.T) {
	if deploymentPhaseIcon("shadow") != "🌑" {
		t.Error("expected shadow emoji")
	}
	if deploymentPhaseIcon("canary") != "🐤" {
		t.Error("expected canary emoji")
	}
	if deploymentPhaseIcon("promoted") != "✅" {
		t.Error("expected promoted emoji")
	}
	if deploymentPhaseIcon("rolled_back") != "⏪" {
		t.Error("expected rollback emoji")
	}
}

// ============================================================================
// NewPRStatusManager
// ============================================================================

func TestNewPRStatusManager(t *testing.T) {
	client := NewClient("http://localhost:3030", "admin", "pass")
	mgr := NewPRStatusManager(client)
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
	if mgr.client == nil {
		t.Error("expected non-nil client")
	}
}

// ============================================================================
// Integration: full comment posting round-trip
// ============================================================================

func TestPostReviewComment_RoundTrip(t *testing.T) {
	var postedBody string
	client := makeMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		postedBody = string(body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	})

	mgr := NewPRStatusManager(client)
	v := sampleVerdict("APPROVE")
	v.Findings = []string{"Finding 1", "Finding 2"}

	err := mgr.PostReviewComment(context.Background(), "owner", "repo", 42, v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the posted body contains all expected content.
	var req CreatePRReviewRequest
	if err := json.Unmarshal([]byte(postedBody), &req); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if req.Event != "APPROVED" {
		t.Errorf("expected APPROVED event, got %s", req.Event)
	}
	if !strings.Contains(req.Body, "APPROVE") {
		t.Error("body should contain APPROVE")
	}
	if !strings.Contains(req.Body, "Finding 1") {
		t.Error("body should contain findings")
	}
	if !strings.Contains(req.Body, "Helix") {
		t.Error("body should contain Helix branding")
	}
}

func TestPostReviewStatus_Description(t *testing.T) {
	var capturedBody string
	client := makeMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.WriteHeader(http.StatusCreated)
	})

	mgr := NewPRStatusManager(client)
	v := sampleVerdict("APPROVE")
	err := mgr.PostReviewStatus(context.Background(), "owner", "repo", "abc", v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var status CommitStatus
	if err := json.Unmarshal([]byte(capturedBody), &status); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if !strings.Contains(status.Description, "APPROVE") {
		t.Errorf("expected APPROVE in description, got %s", status.Description)
	}
	if !strings.Contains(status.Description, "95") {
		t.Errorf("expected confidence in description, got %s", status.Description)
	}
}

func TestPostCommitStatus_APIError(t *testing.T) {
	client := makeMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	})

	mgr := NewPRStatusManager(client)
	err := mgr.PostCommitStatus(context.Background(), "owner", "repo", "abc", CommitStatus{
		State:   StatusStateSuccess,
		Context: "helix/test",
	})
	if err == nil {
		t.Fatal("expected error for 403")
	}
}

// ============================================================================
// CommitStatus types
// ============================================================================

func TestCommitStatusState_String(t *testing.T) {
	states := []CommitStatusState{StatusStatePending, StatusStateSuccess, StatusStateFailure, StatusStateError, StatusStateWarning}
	for _, s := range states {
		if string(s) == "" {
			t.Errorf("expected non-empty string for state")
		}
	}
}

// ============================================================================
// Edge cases
// ============================================================================

func TestFormatReviewComment_EmptyModels(t *testing.T) {
	v := sampleVerdict("APPROVE")
	v.ModelsUsed = nil
	body := FormatReviewComment(v)

	// Should not crash and should still contain review info.
	if !strings.Contains(body, "APPROVE") {
		t.Error("expected APPROVE in body")
	}
}

func TestFormatReviewComment_LowConfidence(t *testing.T) {
	v := sampleVerdict("REQUEST_CHANGES")
	v.Confidence = 0.12
	body := FormatReviewComment(v)

	if !strings.Contains(body, "12.0%") {
		t.Error("expected 12.0% confidence")
	}
}

func TestParseReviewBody_HelixWithoutDecision(t *testing.T) {
	body := "Some comment mentioning Helix but no structured data"
	p := ParseReviewBody(body)
	if p == nil {
		t.Fatal("expected non-nil (mentions Helix)")
	}
	if !p.IsHelix {
		t.Error("expected IsHelix")
	}
	if p.Decision != "" {
		t.Errorf("expected empty decision, got %s", p.Decision)
	}
}

func TestParseReviewBody_Empty(t *testing.T) {
	p := ParseReviewBody("")
	if p != nil {
		t.Error("expected nil for empty body")
	}
}

func TestParseReviewBody_CompletelyUnrelated(t *testing.T) {
	p := ParseReviewBody("This is a random comment with no structure.")
	if p != nil {
		t.Error("expected nil for unstructured non-Helix comment")
	}
}

// ============================================================================
// Multiple reviews with mixed content
// ============================================================================

func TestParsePRReviews_MixedReviews(t *testing.T) {
	reviews := []PRReview{
		{Body: fmt.Sprintf("### ✅ Helix Review: `%s`\n\n**Confidence:** %s%%", "APPROVE", "95.0")},
		{Body: "Human review: LGTM"},
		{Body: fmt.Sprintf("### ❌ Helix Review: `%s`\n\n**Confidence:** %s%%", "REJECT", "72.0")},
		{Body: "Another human comment"},
		{Body: fmt.Sprintf("### ⚠️ Helix Review: `%s`\n\n**Confidence:** %s%%", "REQUEST_CHANGES", "85.0")},
	}

	parsed := ParsePRReviews(reviews)
	if len(parsed) != 3 {
		t.Fatalf("expected 3 Helix reviews, got %d", len(parsed))
	}
	decisions := []string{"APPROVE", "REJECT", "REQUEST_CHANGES"}
	for i, expected := range decisions {
		if parsed[i].Decision != expected {
			t.Errorf("review %d: expected %s, got %s", i, expected, parsed[i].Decision)
		}
	}
}
