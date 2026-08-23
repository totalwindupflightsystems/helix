// Package review — chimera_e2e_test.go
//
// INT-002: Chimera multi-model review E2E test.
//
// Exercises the Chimera deliberation pipeline end-to-end:
//   1. Submit a code review through ChimeraModelClient
//   2. Validate the merged answer contains expected review elements
//   3. Verify the trace/deliberation history is populated
//
// Uses httptest mocks when Chimera is not reachable; the live test
// is skipped by default to keep CI green.

package review

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock Chimera server helpers
// =============================================================================

// chimeraMockConfig configures the mock Chimera server behaviour.
type chimeraMockConfig struct {
	// deliberationResponse is the raw JSON body returned by POST /v1/deliberate.
	deliberationResponse string
	// statusCode overrides the default 200 status (for error scenarios).
	statusCode int
	// delay adds artificial latency before responding.
	delay time.Duration
}

// newChimeraMockServer starts an httptest server that emulates the Chimera
// deliberation API. Returns the server and the captured request body (populated
// after the handler runs).
func newChimeraMockServer(t *testing.T, cfg chimeraMockConfig) (*httptest.Server, *[]byte) {
	t.Helper()

	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read and capture request body for assertions.
		body := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			_, _ = r.Body.Read(body)
		}
		captured = body

		if cfg.delay > 0 {
			time.Sleep(cfg.delay)
		}

		if cfg.statusCode > 0 {
			w.WriteHeader(cfg.statusCode)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if cfg.deliberationResponse != "" {
			_, _ = w.Write([]byte(cfg.deliberationResponse))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &captured
}

// standardDeliberationResponse builds a Chimera /v1/deliberate response
// matching the real DeliberateResponse contract (OpenAPI 3.1, chimera 0.2.0):
// {"answer": string (REQUIRED), "trace": object (REQUIRED),
//
//	"request_id": string (REQUIRED)}. The merged deliberation text in
//
// "answer" carries the review-result JSON the model was asked to produce;
// "trace" is the per-stage execution-graph OBJECT (not an array) observed on
// the live server (DF-021).
func standardDeliberationResponse(verdict string, findings []map[string]interface{}) string {
	inner := map[string]interface{}{
		"verdict":  verdict,
		"findings": findings,
	}
	innerBytes, _ := json.Marshal(inner)

	outer := map[string]interface{}{
		"answer": string(innerBytes),
		"trace": map[string]interface{}{
			"request_id": "trace-req-001",
			"formation":  "debate",
			"stages": []map[string]interface{}{
				{
					"stage_id": "worker_1",
					"kind":     "worker",
					"model":    "deepseek/deepseek-v4-pro",
					"output":   verdict,
					"tokens":   1200,
					"duration": 1.5,
				},
				{
					"stage_id": "aggregator",
					"kind":     "aggregator",
					"model":    "deepseek/deepseek-v4-flash",
					"output":   verdict,
					"tokens":   800,
					"duration": 2.1,
				},
				{
					"stage_id": "merge",
					"kind":     "merge",
					"model":    "deepseek/deepseek-v4-flash",
					"output":   verdict,
					"tokens":   600,
					"duration": 0.9,
				},
			},
		},
		"request_id": "req-001",
	}
	outerBytes, _ := json.Marshal(outer)
	return string(outerBytes)
}

// =============================================================================
// TestChimeraMultiModelReview — E2E test via mock Chimera server
// =============================================================================

func TestChimeraMultiModelReview(t *testing.T) {
	// —— Setup: mock Chimera returning a full multi-model deliberation result ——
	reviewJSON := standardDeliberationResponse("approved", []map[string]interface{}{
		{
			"severity":    "low",
			"type":        "style",
			"file":        "main.go",
			"line":        12,
			"description": "Consider using camelCase",
			"evidence":    "var user_name string",
		},
		{
			"severity":    "medium",
			"type":        "performance",
			"file":        "handler.go",
			"line":        45,
			"description": "Allocate slice with known capacity",
			"evidence":    "var out []string; for ... { out = append(out, v) }",
		},
	})
	srv, captured := newChimeraMockServer(t, chimeraMockConfig{
		deliberationResponse: reviewJSON,
	})
	defer srv.Close()

	// —— Create ChimeraModelClient ——
	client := NewChimeraClient(ModelClientConfig{
		BaseURL: srv.URL,
		Model:   "chimera-default",
		Timeout: 10 * time.Second,
	})

	// —— Submit review ——
	req := ReviewRequest{
		Diff:             "diff --git a/main.go b/main.go\n+func main() { fmt.Println(\"hello\") }",
		NeutralCommitMsg: "add hello world function",
		Role:             RolePrimary,
		Context: ReviewContext{
			Category: CategoryBehavioral,
			PRURL:    "https://forgejo.example.com/helix/repo/pulls/1",
			ReviewID: "rev-001",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.Review(ctx, req)
	require.NoError(t, err, "Chimera deliberation should succeed")
	require.NotNil(t, result)

	// —— Verify: merged answer ——
	assert.Equal(t, "approved", result.Verdict, "verdict should be approved")
	assert.Len(t, result.Findings, 2, "should have 2 findings from multi-model deliberation")

	// Check first finding.
	f0 := result.Findings[0]
	assert.Equal(t, "low", f0.Severity)
	assert.Equal(t, "style", f0.Type)
	assert.Equal(t, "main.go", f0.File)
	assert.Equal(t, 12, f0.Line)
	assert.Equal(t, "chimera:chimera-default", f0.Model, "model prefix should be set")
	assert.Contains(t, f0.Description, "camelCase")

	// Check second finding.
	f1 := result.Findings[1]
	assert.Equal(t, "medium", f1.Severity)
	assert.Equal(t, "performance", f1.Type)
	assert.Equal(t, "handler.go", f1.File)
	assert.Equal(t, 45, f1.Line)
	assert.Contains(t, f1.Description, "Allocate")

	// —— Verify: latency was recorded ——
	assert.Greater(t, result.Latency.Microseconds(), int64(0), "latency should be non-zero")

	// —— Verify: the request body sent to Chimera ——
	var sent map[string]interface{}
	require.NoError(t, json.Unmarshal(*captured, &sent))
	assert.Contains(t, sent["prompt"].(string), "hello", "prompt should contain diff content")
	assert.Equal(t, "debate", sent["formation"], "formation should map from category (live-valid preset, DF-021)")
}

// =============================================================================
// TestChimeraMultiModelReview_FormationRouting — table-driven formation test
// =============================================================================

func TestChimeraMultiModelReview_FormationRouting(t *testing.T) {
	emptyReview := standardDeliberationResponse("approved", nil)

	tests := []struct {
		name          string
		category      ChangeCategory
		wantFormation string
	}{
		{name: "contract", category: CategoryContract, wantFormation: "audit"},
		{name: "behavioral", category: CategoryBehavioral, wantFormation: "debate"},
		{name: "resilience", category: CategoryResilience, wantFormation: "speed"},
		{name: "cosmetic", category: CategoryCosmetic, wantFormation: "auto"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, captured := newChimeraMockServer(t, chimeraMockConfig{
				deliberationResponse: emptyReview,
			})
			defer srv.Close()

			client := NewChimeraClient(ModelClientConfig{
				BaseURL: srv.URL,
				Model:   "chimera-default",
				Timeout: 5 * time.Second,
			})

			req := ReviewRequest{
				Diff:             "diff content",
				NeutralCommitMsg: "test",
				Role:             RolePrimary,
				Context: ReviewContext{
					Category: tt.category,
					PRURL:    "https://forgejo.example.com/pulls/1",
					ReviewID: "rev-002",
				},
			}

			_, err := client.Review(context.Background(), req)
			require.NoError(t, err)

			var sent map[string]interface{}
			require.NoError(t, json.Unmarshal(*captured, &sent))
			assert.Equal(t, tt.wantFormation, sent["formation"],
				"formation for category %q should be %q", tt.category, tt.wantFormation)
		})
	}
}

// =============================================================================
// TestChimeraMultiModelReview_TracePopulated — verifies trace is in response
// =============================================================================

func TestChimeraMultiModelReview_TracePopulated(t *testing.T) {
	// This test validates that the Chimera deliberation response includes a
	// trace with per-stage results (model, tokens, duration), even though
	// ChimeraModelClient.Review() only extracts the merged "answer" text for
	// review parsing. We validate at the HTTP level that the trace structure
	// is present.

	reviewWithFindings := standardDeliberationResponse("block", []map[string]interface{}{
		{
			"severity":    "critical",
			"type":        "security",
			"file":        "auth.go",
			"line":        42,
			"description": "SQL injection in raw query",
			"evidence":    "db.Exec(\"SELECT * FROM users WHERE id=\" + userID)",
		},
	})

	srv, _ := newChimeraMockServer(t, chimeraMockConfig{
		deliberationResponse: reviewWithFindings,
	})
	defer srv.Close()

	client := NewChimeraClient(ModelClientConfig{
		BaseURL: srv.URL,
		Model:   "chimera-default",
		Timeout: 5 * time.Second,
	})

	req := ReviewRequest{
		Diff:             "diff --git a/auth.go b/auth.go\n+db.Exec(\"SELECT * FROM users WHERE id=\" + userID)",
		NeutralCommitMsg: "add user query",
		Role:             RolePrimary,
		Context: ReviewContext{
			Category: CategoryContract,
			PRURL:    "https://forgejo.example.com/pulls/2",
			ReviewID: "rev-003",
		},
	}

	result, err := client.Review(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)

	// The client parses the merged "answer" text into ModelReviewResult.
	assert.Equal(t, "block", result.Verdict)
	assert.Len(t, result.Findings, 1)
	assert.Equal(t, "critical", result.Findings[0].Severity)
	assert.Equal(t, "security", result.Findings[0].Type)

	// Verify at the HTTP level that the raw response matches the real
	// DeliberateResponse contract: {"answer", "trace" (object),
	// "request_id"} — all required (DF-021: there is no "result" field).
	// We make a direct HTTP call to inspect the full response.
	resp, err := http.Post(srv.URL+"/v1/deliberate", "application/json",
		strings.NewReader(`{"prompt":"test","formation":"audit"}`))
	require.NoError(t, err)
	defer resp.Body.Close()

	var full map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&full))

	// Contract shape: answer + trace + request_id present.
	answerStr, ok := full["answer"].(string)
	require.True(t, ok, "answer field should be a string")
	require.NotEmpty(t, answerStr)
	assert.Contains(t, answerStr, `"verdict"`)
	assert.Contains(t, answerStr, `"findings"`)

	traceObj, ok := full["trace"].(map[string]interface{})
	require.True(t, ok, "response should contain trace as an OBJECT (per-stage execution graph), not an array")

	// The trace object carries per-stage entries; validate their fields.
	stages, ok := traceObj["stages"].([]interface{})
	require.True(t, ok, "trace should contain a stages array")
	require.Len(t, stages, 3)

	stageFields := []string{"stage_id", "model", "output", "tokens", "duration"}
	for i, stageRaw := range stages {
		stage, ok := stageRaw.(map[string]interface{})
		require.True(t, ok, "stage[%d] should be an object", i)
		for _, field := range stageFields {
			assert.Contains(t, stage, field, "stage[%d] should have field %q", i, field)
		}
	}

	// Verify stage models are distinct (multi-model).
	models := make(map[string]bool)
	for _, stageRaw := range stages {
		stage := stageRaw.(map[string]interface{})
		models[stage["model"].(string)] = true
	}
	assert.GreaterOrEqual(t, len(models), 2, "should use at least 2 distinct models")

	// request_id is required by the contract.
	requestID, ok := full["request_id"].(string)
	require.True(t, ok, "request_id field should be a string")
	assert.NotEmpty(t, requestID)
}

// =============================================================================
// TestChimeraMultiModelReview_ClientInfo — client metadata
// =============================================================================

func TestChimeraMultiModelReview_ClientInfo(t *testing.T) {
	client := NewChimeraClient(ModelClientConfig{
		BaseURL: "http://localhost:8765",
		Model:   "chimera-arbiter",
		Timeout: 30 * time.Second,
	})

	info := client.Info()
	assert.Equal(t, "chimera-arbiter", info.Model)
	assert.Equal(t, "chimera", info.Provider)
}

// =============================================================================
// TestChimeraMultiModelReview_Timeout — timeout handling
// =============================================================================

func TestChimeraMultiModelReview_Timeout(t *testing.T) {
	srv, _ := newChimeraMockServer(t, chimeraMockConfig{
		deliberationResponse: `{"answer": "{\"verdict\":\"approved\",\"findings\":[]}", "trace": {}, "request_id": "req-timeout-001"}`,
		delay:                2 * time.Second,
	})
	defer srv.Close()

	client := NewChimeraClient(ModelClientConfig{
		BaseURL: srv.URL,
		Model:   "chimera-default",
		Timeout: 100 * time.Millisecond,
	})

	req := ReviewRequest{
		Diff:             "diff",
		NeutralCommitMsg: "test",
		Role:             RolePrimary,
		Context: ReviewContext{
			Category: CategoryBehavioral,
			PRURL:    "https://forgejo.example.com/pulls/1",
			ReviewID: "rev-004",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := client.Review(ctx, req)
	require.Error(t, err, "should timeout when server is slow")
	assert.Contains(t, err.Error(), "chimera")
}

// =============================================================================
// TestChimeraMultiModelReview_ServerError — HTTP error handling
// =============================================================================

func TestChimeraMultiModelReview_ServerError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    string
	}{
		{name: "500", statusCode: http.StatusInternalServerError, wantErr: "500"},
		{name: "503", statusCode: http.StatusServiceUnavailable, wantErr: "503"},
		{name: "400", statusCode: http.StatusBadRequest, wantErr: "400"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := newChimeraMockServer(t, chimeraMockConfig{
				statusCode: tt.statusCode,
			})
			defer srv.Close()

			client := NewChimeraClient(ModelClientConfig{
				BaseURL: srv.URL,
				Model:   "chimera-default",
				Timeout: 5 * time.Second,
			})

			req := ReviewRequest{
				Diff:             "diff",
				NeutralCommitMsg: "test",
				Role:             RolePrimary,
				Context: ReviewContext{
					Category: CategoryBehavioral,
					PRURL:    "https://forgejo.example.com/pulls/1",
					ReviewID: "rev-005",
				},
			}

			_, err := client.Review(context.Background(), req)
			require.Error(t, err)
			assert.Contains(t, err.Error(), fmt.Sprintf("chimera: http %d", tt.statusCode))
		})
	}
}

// =============================================================================
// TestChimeraMultiModelReview_MalformedResult — invalid JSON in result field
// =============================================================================

func TestChimeraMultiModelReview_MalformedResult(t *testing.T) {
	srv, _ := newChimeraMockServer(t, chimeraMockConfig{
		deliberationResponse: `{"answer": "not valid json at all", "trace": {}, "request_id": "req-bad-001"}`,
	})
	defer srv.Close()

	client := NewChimeraClient(ModelClientConfig{
		BaseURL: srv.URL,
		Model:   "chimera-default",
		Timeout: 5 * time.Second,
	})

	req := ReviewRequest{
		Diff:             "diff",
		NeutralCommitMsg: "test",
		Role:             RolePrimary,
		Context: ReviewContext{
			Category: CategoryBehavioral,
			PRURL:    "https://forgejo.example.com/pulls/1",
			ReviewID: "rev-006",
		},
	}

	_, err := client.Review(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chimera")
	assert.Contains(t, err.Error(), "parse response")
}

// =============================================================================
// TestChimeraClient_DeliberateRoute — the client must POST to the real
// chimera route /v1/deliberate (NOT /api/v1/deliberate, which returns
// 404 — DF-018).
// =============================================================================

func TestChimeraClient_DeliberateRoute(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"answer": "{\"verdict\":\"approved\",\"findings\":[]}", "trace": {}, "request_id": "req-route-001"}`))
	}))
	defer srv.Close()

	client := NewChimeraClient(ModelClientConfig{
		BaseURL: srv.URL,
		Model:   "chimera-default",
		Timeout: 5 * time.Second,
	})

	req := ReviewRequest{
		Diff:             "diff",
		NeutralCommitMsg: "test",
		Role:             RoleAdversarial,
		Context: ReviewContext{
			Category: CategoryBehavioral,
			PRURL:    "http://localhost:3030/helio/repo/pulls/1",
			ReviewID: "rev-route-001",
		},
	}

	_, err := client.Review(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "/v1/deliberate", gotPath,
		"chimera client must POST to /v1/deliberate, not /api/v1/deliberate (DF-018)")
}

// =============================================================================
// TestChimeraMultiModelReview_Live — skipped integration test for real Chimera
// =============================================================================

func TestChimeraMultiModelReview_Live(t *testing.T) {
	// DF-021: run against live Chimera when CHIMERA_LIVE=1 (or when
	// CHIMERA_BASE_URL points somewhere); otherwise skip so CI stays green
	// without a running deliberation service.
	if lookupEnv("CHIMERA_LIVE") == "" && lookupEnv("CHIMERA_BASE_URL") == "" {
		t.Skip("live Chimera deliberation disabled; set CHIMERA_LIVE=1 (and " +
			"optionally CHIMERA_BASE_URL) to run against a running chimera server")
	}

	baseURL := "http://localhost:8765"
	if v := lookupEnv("CHIMERA_BASE_URL"); v != "" {
		baseURL = v
	}

	// Quick health check — if unreachable, skip with a clear message.
	// 5s budget: /v1/health probes every configured provider and can take
	// multiple seconds on a loaded server (observed ~2.7s live).
	hc := &http.Client{Timeout: 5 * time.Second}
	resp, err := hc.Get(baseURL + "/v1/health")
	if err != nil {
		t.Skipf("Chimera not reachable at %s: %v", baseURL, err)
	}
	resp.Body.Close()

	client := NewChimeraClient(ModelClientConfig{
		BaseURL: baseURL,
		Model:   "chimera-default",
		Timeout: 120 * time.Second,
	})

	req := ReviewRequest{
		Diff: "diff --git a/main.go b/main.go\n" +
			"--- a/main.go\n" +
			"+++ b/main.go\n" +
			"@@ -1,3 +1,5 @@\n" +
			" package main\n" +
			"+import \"fmt\"\n" +
			"+\n" +
			"+func Hello() string { return \"hello, world\" }",
		NeutralCommitMsg: "add Hello function",
		Role:             RolePrimary,
		Context: ReviewContext{
			Category: CategoryBehavioral,
			PRURL:    "https://forgejo.example.com/helix/repo/pulls/42",
			ReviewID: "rev-live-001",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	result, err := client.Review(ctx, req)
	require.NoError(t, err, "live Chimera deliberation should succeed")
	require.NotNil(t, result)

	// Validate merged answer.
	assert.NotEmpty(t, result.Verdict, "verdict should not be empty")
	t.Logf("Chimera verdict: %s", result.Verdict)
	t.Logf("Findings: %d", len(result.Findings))
	for _, f := range result.Findings {
		t.Logf("  [%s] %s:%d — %s", f.Severity, f.File, f.Line, f.Description)
	}
	assert.Greater(t, result.Latency.Microseconds(), int64(0), "latency should be recorded")

	t.Log("Live Chimera multi-model deliberation completed successfully")
}

// lookupEnv reads an environment variable. Kept trivially simple so the
// live test needs no extra dependencies; returns "" when unset.
func lookupEnv(key string) string {
	return os.Getenv(key)
}
