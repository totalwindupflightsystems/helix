package integration

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// --- Constructor tests: verify exact spec §7 messages ---------------------

func TestNewChimeraUnavailableError(t *testing.T) {
	cause := errors.New("dial tcp: connection refused")
	e := NewChimeraUnavailableError(cause)
	if e.Code != CodeChimeraUnavailable {
		t.Errorf("Code = %q, want %q", e.Code, CodeChimeraUnavailable)
	}
	if e.Message != "Chimera unavailable — manual review required" {
		t.Errorf("Message = %q", e.Message)
	}
	if e.Service != "chimera" {
		t.Errorf("Service = %q, want chimera", e.Service)
	}
	if e.Retryable {
		t.Error("should not be retryable")
	}
	if !errors.Is(e, cause) {
		t.Error("Unwrap should return cause")
	}
}

func TestNewBudgetExhaustedError(t *testing.T) {
	e := NewBudgetExhaustedError(3.50, 2.00)
	if e.Code != CodeBudgetExhausted {
		t.Errorf("Code = %q, want %q", e.Code, CodeBudgetExhausted)
	}
	want := "BUDGET_EXHAUSTED: tie-break cost $3.50 > remaining $2.00"
	if e.Message != want {
		t.Errorf("Message = %q, want %q", e.Message, want)
	}
	if e.Service != "chimera" {
		t.Errorf("Service = %q", e.Service)
	}
	if e.Retryable {
		t.Error("budget exhaustion should not be retryable")
	}
}

func TestNewConnectionRefusedError(t *testing.T) {
	cause := fmt.Errorf("503 Service Unavailable")
	e := NewConnectionRefusedError("forgejo", 1, 4, 30*time.Second, cause)
	if e.Code != CodeConnectionRefused {
		t.Errorf("Code = %q", e.Code)
	}
	want := "CONNECTION_REFUSED: retry in 30s (attempt 1/4)"
	if e.Message != want {
		t.Errorf("Message = %q, want %q", e.Message, want)
	}
	if !e.Retryable {
		t.Error("connection refused should be retryable")
	}
	if e.RetryAfter != 30*time.Second {
		t.Errorf("RetryAfter = %v", e.RetryAfter)
	}
	if e.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("StatusCode = %d, want %d", e.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestNewAuthFailedError(t *testing.T) {
	cause := errors.New("invalid api key")
	e := NewAuthFailedError("openrouter", cause)
	if e.Code != CodeAuthFailed {
		t.Errorf("Code = %q", e.Code)
	}
	if e.Message != "AUTH_FAILED: agent key is dead — trigger key rotation" {
		t.Errorf("Message = %q", e.Message)
	}
	if e.Retryable {
		t.Error("auth failure should not be retryable")
	}
	if e.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want %d", e.StatusCode, http.StatusUnauthorized)
	}
}

func TestNewBranchConflictError(t *testing.T) {
	e := NewBranchConflictError("feat/WI-001")
	if e.Code != CodeBranchConflict {
		t.Errorf("Code = %q", e.Code)
	}
	if e.Message != "BRANCH_CONFLICT: feat/WI-001 exists — use --force-branch" {
		t.Errorf("Message = %q", e.Message)
	}
	if e.Service != "forgejo" {
		t.Errorf("Service = %q", e.Service)
	}
	if e.StatusCode != http.StatusConflict {
		t.Errorf("StatusCode = %d, want %d", e.StatusCode, http.StatusConflict)
	}
	if e.Retryable {
		t.Error("branch conflict should not be retryable")
	}
}

// --- ClassifyError: spec §7 table coverage --------------------------------

func TestClassifyError_ForgejoChimera_Unreachable(t *testing.T) {
	e := ClassifyError("forgejo", "chimera", 0, ErrorContext{Err: errors.New("net err")})
	if e.Code != CodeChimeraUnavailable {
		t.Errorf("Code = %q, want %q", e.Code, CodeChimeraUnavailable)
	}
	if e.Retryable {
		t.Error("should not be retryable")
	}
}

func TestClassifyError_NegotiateChimera_Budget(t *testing.T) {
	e := ClassifyError("negotiate", "chimera", 0, ErrorContext{
		BudgetExhausted: true,
		Cost:            5.00,
		Remaining:       1.50,
	})
	if e.Code != CodeBudgetExhausted {
		t.Fatalf("Code = %q", e.Code)
	}
}

func TestClassifyError_IdentityForgejo_503(t *testing.T) {
	e := ClassifyError("identity", "forgejo", http.StatusServiceUnavailable, ErrorContext{
		Attempt:     2,
		MaxAttempts: 4,
		RetryAfter:  30 * time.Second,
		Err:         errors.New("503"),
	})
	if e.Code != CodeConnectionRefused {
		t.Fatalf("Code = %q", e.Code)
	}
	if !e.Retryable {
		t.Error("should be retryable")
	}
}

func TestClassifyError_EstimateOpenRouter_401(t *testing.T) {
	e := ClassifyError("estimate", "openrouter", http.StatusUnauthorized, ErrorContext{
		Err: errors.New("invalid key"),
	})
	if e.Code != CodeAuthFailed {
		t.Fatalf("Code = %q", e.Code)
	}
}

func TestClassifyError_AxiomForgejo_409(t *testing.T) {
	e := ClassifyError("axiom", "forgejo", http.StatusConflict, ErrorContext{
		Branch: "feat/WI-001",
	})
	if e.Code != CodeBranchConflict {
		t.Fatalf("Code = %q", e.Code)
	}
}

func TestClassifyError_FallbackToHTTP(t *testing.T) {
	// Unknown caller/callee → falls back to ClassifyHTTP.
	e := ClassifyError("unknown", "forgejo", http.StatusNotFound, ErrorContext{Body: "not found"})
	if e.Code != CodeNotFound {
		t.Errorf("Code = %q, want %q", e.Code, CodeNotFound)
	}
}

// --- ClassifyHTTP tests ---------------------------------------------------

func TestClassifyHTTP_401(t *testing.T) {
	e := ClassifyHTTP("openrouter", http.StatusUnauthorized, "bad key")
	if e.Code != CodeAuthFailed {
		t.Errorf("Code = %q", e.Code)
	}
	if e.Retryable {
		t.Error("401 should not be retryable")
	}
}

func TestClassifyHTTP_403(t *testing.T) {
	e := ClassifyHTTP("forgejo", http.StatusForbidden, "denied")
	if e.Code != CodeForbidden {
		t.Errorf("Code = %q", e.Code)
	}
}

func TestClassifyHTTP_404(t *testing.T) {
	e := ClassifyHTTP("forgejo", http.StatusNotFound, "missing")
	if e.Code != CodeNotFound {
		t.Errorf("Code = %q", e.Code)
	}
}

func TestClassifyHTTP_409(t *testing.T) {
	e := ClassifyHTTP("forgejo", http.StatusConflict, "exists")
	if e.Code != CodeBranchConflict {
		t.Errorf("Code = %q", e.Code)
	}
}

func TestClassifyHTTP_429(t *testing.T) {
	e := ClassifyHTTP("chimera", http.StatusTooManyRequests, "slow down")
	if e.Code != CodeRateLimited {
		t.Errorf("Code = %q", e.Code)
	}
	if !e.Retryable {
		t.Error("429 should be retryable")
	}
	if e.RetryAfter != 60*time.Second {
		t.Errorf("RetryAfter = %v", e.RetryAfter)
	}
}

func TestClassifyHTTP_500(t *testing.T) {
	e := ClassifyHTTP("forgejo", 500, "oops")
	if e.Code != CodeServiceUnavailable {
		t.Errorf("Code = %q", e.Code)
	}
	if !e.Retryable {
		t.Error("500 should be retryable")
	}
}

func TestClassifyHTTP_502(t *testing.T) {
	e := ClassifyHTTP("forgejo", http.StatusBadGateway, "bad gateway")
	if e.Code != CodeConnectionRefused {
		t.Errorf("Code = %q", e.Code)
	}
	if !e.Retryable {
		t.Error("502 should be retryable")
	}
}

func TestClassifyHTTP_503(t *testing.T) {
	e := ClassifyHTTP("forgejo", http.StatusServiceUnavailable, "down")
	if e.Code != CodeConnectionRefused {
		t.Errorf("Code = %q", e.Code)
	}
	if e.RetryAfter != 30*time.Second {
		t.Errorf("RetryAfter = %v", e.RetryAfter)
	}
}

func TestClassifyHTTP_504(t *testing.T) {
	e := ClassifyHTTP("forgejo", http.StatusGatewayTimeout, "timed out")
	if e.Code != CodeTimeout {
		t.Errorf("Code = %q", e.Code)
	}
	if !e.Retryable {
		t.Error("504 should be retryable")
	}
}

func TestClassifyHTTP_Generic5xx(t *testing.T) {
	e := ClassifyHTTP("forgejo", 508, "loop detected")
	if e.Code != CodeServiceUnavailable {
		t.Errorf("Code = %q", e.Code)
	}
	if !e.Retryable {
		t.Error("5xx should be retryable")
	}
}

func TestClassifyHTTP_Generic4xx(t *testing.T) {
	e := ClassifyHTTP("forgejo", 422, "unprocessable")
	if e.Code != "CLIENT_ERROR" {
		t.Errorf("Code = %q", e.Code)
	}
	if e.Retryable {
		t.Error("4xx should not be retryable")
	}
}

func TestClassifyHTTP_Success(t *testing.T) {
	e := ClassifyHTTP("forgejo", 200, "")
	if e.Code != "UNKNOWN" {
		t.Errorf("Code = %q", e.Code)
	}
}

func TestClassifyHTTP_TruncatesLongBody(t *testing.T) {
	longBody := string(make([]byte, 500))
	e := ClassifyHTTP("forgejo", http.StatusNotFound, longBody)
	if len(e.Message) > 300 {
		t.Errorf("Message too long: %d chars", len(e.Message))
	}
}

// --- Helper function tests ------------------------------------------------

func TestIsRetryable_ServiceError(t *testing.T) {
	retryable := NewConnectionRefusedError("forgejo", 1, 4, 30*time.Second, nil)
	if !IsRetryable(retryable) {
		t.Error("connection refused should be retryable")
	}

	nonRetryable := NewAuthFailedError("openrouter", nil)
	if IsRetryable(nonRetryable) {
		t.Error("auth failure should not be retryable")
	}
}

func TestIsRetryable_NonServiceError(t *testing.T) {
	plainErr := errors.New("plain error")
	if IsRetryable(plainErr) {
		t.Error("plain error should not be retryable")
	}
}

func TestIsCode(t *testing.T) {
	authErr := NewAuthFailedError("openrouter", nil)
	if !IsCode(authErr, CodeAuthFailed) {
		t.Error("should match CodeAuthFailed")
	}
	if IsCode(authErr, CodeNotFound) {
		t.Error("should not match CodeNotFound")
	}
}

func TestIsCode_NonServiceError(t *testing.T) {
	plainErr := errors.New("plain")
	if IsCode(plainErr, CodeAuthFailed) {
		t.Error("should return false for non-ServiceError")
	}
}

func TestGetRetryAfter(t *testing.T) {
	e := NewConnectionRefusedError("forgejo", 1, 4, 45*time.Second, nil)
	if got := GetRetryAfter(e); got != 45*time.Second {
		t.Errorf("RetryAfter = %v, want 45s", got)
	}
}

func TestGetRetryAfter_NonServiceError(t *testing.T) {
	if got := GetRetryAfter(errors.New("plain")); got != 0 {
		t.Errorf("GetRetryAfter on plain error = %v, want 0", got)
	}
}

func TestGetRetryAfter_NoHint(t *testing.T) {
	e := NewAuthFailedError("openrouter", nil)
	if got := GetRetryAfter(e); got != 0 {
		t.Errorf("RetryAfter = %v, want 0", got)
	}
}

// --- ServiceError interface compliance ------------------------------------

func TestServiceError_ErrorString(t *testing.T) {
	e := NewBudgetExhaustedError(10.00, 5.00)
	if e.Error() != "BUDGET_EXHAUSTED: tie-break cost $10.00 > remaining $5.00" {
		t.Errorf("Error() = %q", e.Error())
	}
}

func TestServiceError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	e := NewChimeraUnavailableError(cause)
	if !errors.Is(e, cause) {
		t.Error("errors.Is should find cause via Unwrap")
	}
}

func TestServiceError_UnwrapNil(t *testing.T) {
	e := NewBudgetExhaustedError(1, 0)
	if e.Unwrap() != nil {
		t.Error("Unwrap should return nil when no cause")
	}
}

// --- Wrapped error chain tests --------------------------------------------

func TestErrorsAs_ServiceError(t *testing.T) {
	plainErr := errors.New("inner")
	se := NewAuthFailedError("openrouter", plainErr)
	// Wrapping in another error.
	wrapped := fmt.Errorf("outer: %w", se)

	var target *ServiceError
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As should find ServiceError in chain")
	}
	if target.Code != CodeAuthFailed {
		t.Errorf("Code = %q", target.Code)
	}
}

// --- Budget formatting edge cases -----------------------------------------

func TestNewBudgetExhaustedError_Zero(t *testing.T) {
	e := NewBudgetExhaustedError(0, 0)
	if e.Message != "BUDGET_EXHAUSTED: tie-break cost $0.00 > remaining $0.00" {
		t.Errorf("Message = %q", e.Message)
	}
}

func TestNewBudgetExhaustedError_LargeAmounts(t *testing.T) {
	e := NewBudgetExhaustedError(99999.99, 1.00)
	if e.Message != "BUDGET_EXHAUSTED: tie-break cost $99999.99 > remaining $1.00" {
		t.Errorf("Message = %q", e.Message)
	}
}

// --- truncate helper ------------------------------------------------------

func TestTruncate_ShortString(t *testing.T) {
	got := truncate("hello", 200)
	if got != "hello" {
		t.Errorf("truncate = %q", got)
	}
}

func TestTruncate_LongString(t *testing.T) {
	input := string(make([]byte, 300))
	got := truncate(input, 200)
	// 200 bytes of the original + "…" (3 bytes in UTF-8) = 203.
	if len(got) != 203 {
		t.Errorf("len = %d, want 203", len(got))
	}
}

func TestTruncate_ExactLimit(t *testing.T) {
	input := "exact"
	got := truncate(input, 5)
	if got != "exact" {
		t.Errorf("truncate at exact limit = %q", got)
	}
}

// --- Service pair specific ClassifyError with HTTP status ------------------

func TestClassifyError_ForgejoChimera_503(t *testing.T) {
	e := ClassifyError("forgejo", "chimera", 503, ErrorContext{Err: errors.New("503")})
	if e.Code != CodeChimeraUnavailable {
		t.Errorf("Code = %q, want %q", e.Code, CodeChimeraUnavailable)
	}
}

func TestClassifyError_IdentityForgejo_200(t *testing.T) {
	// Non-503 from identity→forgejo → should fall to ClassifyHTTP.
	e := ClassifyError("identity", "forgejo", 200, ErrorContext{})
	// 200 is success — classifies as UNKNOWN.
	if e.Code != "UNKNOWN" {
		t.Errorf("Code = %q, want UNKNOWN", e.Code)
	}
}

func TestClassifyError_EstimateOpenRouter_500(t *testing.T) {
	// Non-401 from estimate→openrouter → should fall to ClassifyHTTP.
	e := ClassifyError("estimate", "openrouter", 500, ErrorContext{})
	if e.Code != CodeServiceUnavailable {
		t.Errorf("Code = %q", e.Code)
	}
}

// --- Retryable classification table ---------------------------------------

func TestRetryableMatrix(t *testing.T) {
	tests := []struct {
		name     string
		err      *ServiceError
		retryExp bool
	}{
		{"chimera unavailable", NewChimeraUnavailableError(nil), false},
		{"budget exhausted", NewBudgetExhaustedError(1, 0), false},
		{"connection refused", NewConnectionRefusedError("forgejo", 1, 4, 30*time.Second, nil), true},
		{"auth failed", NewAuthFailedError("openrouter", nil), false},
		{"branch conflict", NewBranchConflictError("feat/x"), false},
		{"429 rate limited", ClassifyHTTP("s", 429, ""), true},
		{"500 server error", ClassifyHTTP("s", 500, ""), true},
		{"502 bad gateway", ClassifyHTTP("s", 502, ""), true},
		{"503 unavailable", ClassifyHTTP("s", 503, ""), true},
		{"504 timeout", ClassifyHTTP("s", 504, ""), true},
		{"401 auth failed", ClassifyHTTP("s", 401, ""), false},
		{"404 not found", ClassifyHTTP("s", 404, ""), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Retryable != tc.retryExp {
				t.Errorf("Retryable = %v, want %v", tc.err.Retryable, tc.retryExp)
			}
		})
	}
}
