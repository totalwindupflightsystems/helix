package integration

// errors.go implements the centralized error propagation layer per
// specs/cross-component-wiring.md §7 (Error Propagation).
//
// Each cross-service failure maps to a specific ServiceError with a code,
// human-readable message, retryable flag, and retry hint.  The ClassifyError
// function encodes the spec's 5-row table; ClassifyHTTP maps raw HTTP status
// codes to error types for generic use.

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// --- Error codes -----------------------------------------------------------

const (
	// CodeChimeraUnavailable — Forgejo Action → Chimera unreachable.
	CodeChimeraUnavailable = "CHIMERA_UNAVAILABLE"

	// CodeBudgetExhausted — negotiate → Chimera tie-break cost exceeded.
	CodeBudgetExhausted = "BUDGET_EXHAUSTED"

	// CodeConnectionRefused — identity → Forgejo 503.
	CodeConnectionRefused = "CONNECTION_REFUSED"

	// CodeAuthFailed — estimate → OpenRouter 401.
	CodeAuthFailed = "AUTH_FAILED"

	// CodeBranchConflict — Axiom → Forgejo 409.
	CodeBranchConflict = "BRANCH_CONFLICT"

	// CodeServiceUnavailable — generic downstream 5xx / network.
	CodeServiceUnavailable = "SERVICE_UNAVAILABLE"

	// CodeRateLimited — HTTP 429.
	CodeRateLimited = "RATE_LIMITED"

	// CodeTimeout — request exceeded deadline.
	CodeTimeout = "TIMEOUT"

	// CodeCircuitOpen — circuit breaker prevented the call.
	CodeCircuitOpen = "CIRCUIT_OPEN"

	// CodeNotFound — HTTP 404.
	CodeNotFound = "NOT_FOUND"

	// CodeForbidden — HTTP 403 (distinct from 401 auth failure).
	CodeForbidden = "FORBIDDEN"
)

// --- ServiceError ---------------------------------------------------------

// ServiceError is the canonical error type for all cross-service failures.
// Every caller that receives a downstream error wraps it into a ServiceError
// before propagating up.
type ServiceError struct {
	Code       string        // Machine-readable error code (CodeXxx constants)
	Message    string        // Human-readable propagated message per spec §7
	Service    string        // Downstream service name (e.g., "chimera", "forgejo")
	StatusCode int           // HTTP status (0 for non-HTTP errors)
	Retryable  bool          // Whether the caller should retry
	RetryAfter time.Duration // Suggested backoff before retry (0 = immediate)
	Cause      error         // Original error (for Unwrap)
}

// Error implements the error interface.
func (e *ServiceError) Error() string { return e.Message }

// Unwrap allows errors.Is / errors.As to reach the original cause.
func (e *ServiceError) Unwrap() error { return e.Cause }

// --- Spec §7 error formatters ----------------------------------------------

// The five propagated-error rows in spec §7, each materialised as a
// constructor that produces the exact spec-formatted message.

// NewChimeraUnavailableError implements spec §7 row 1:
// "Chimera unavailable — manual review required".
func NewChimeraUnavailableError(cause error) *ServiceError {
	return &ServiceError{
		Code:       CodeChimeraUnavailable,
		Message:    "Chimera unavailable — manual review required",
		Service:    "chimera",
		StatusCode: 0,
		Retryable:  false,
		RetryAfter: 0,
		Cause:      cause,
	}
}

// NewBudgetExhaustedError implements spec §7 row 2:
// "BUDGET_EXHAUSTED: tie-break cost $X.XX > remaining $Y.YY".
func NewBudgetExhaustedError(cost, remaining float64) *ServiceError {
	return &ServiceError{
		Code:       CodeBudgetExhausted,
		Message:    fmt.Sprintf("BUDGET_EXHAUSTED: tie-break cost $%.2f > remaining $%.2f", cost, remaining),
		Service:    "chimera",
		StatusCode: 0,
		Retryable:  false,
		RetryAfter: 0,
	}
}

// NewConnectionRefusedError implements spec §7 row 3:
// "CONNECTION_REFUSED: retry in Ns (attempt M/N)".
func NewConnectionRefusedError(service string, attempt, maxAttempts int, retryAfter time.Duration, cause error) *ServiceError {
	return &ServiceError{
		Code:       CodeConnectionRefused,
		Message:    fmt.Sprintf("CONNECTION_REFUSED: retry in %ds (attempt %d/%d)", int(retryAfter.Seconds()), attempt, maxAttempts),
		Service:    service,
		StatusCode: http.StatusServiceUnavailable,
		Retryable:  true,
		RetryAfter: retryAfter,
		Cause:      cause,
	}
}

// NewAuthFailedError implements spec §7 row 4:
// "AUTH_FAILED: agent key is dead — trigger key rotation".
func NewAuthFailedError(service string, cause error) *ServiceError {
	return &ServiceError{
		Code:       CodeAuthFailed,
		Message:    "AUTH_FAILED: agent key is dead — trigger key rotation",
		Service:    service,
		StatusCode: http.StatusUnauthorized,
		Retryable:  false,
		RetryAfter: 0,
		Cause:      cause,
	}
}

// NewBranchConflictError implements spec §7 row 5:
// "BRANCH_CONFLICT: feat/X exists — use --force-branch".
func NewBranchConflictError(branch string) *ServiceError {
	return &ServiceError{
		Code:       CodeBranchConflict,
		Message:    fmt.Sprintf("BRANCH_CONFLICT: %s exists — use --force-branch", branch),
		Service:    "forgejo",
		StatusCode: http.StatusConflict,
		Retryable:  false,
		RetryAfter: 0,
	}
}

// --- ClassifyHTTP: map HTTP status → ServiceError -------------------------

// ClassifyHTTP maps a raw HTTP status code to a ServiceError with sensible
// defaults for retryability and retry hints.  This is used by HTTP clients
// that do not match a specific spec §7 row.
func ClassifyHTTP(service string, statusCode int, body string) *ServiceError {
	base := &ServiceError{
		Service:    service,
		StatusCode: statusCode,
	}
	switch statusCode {
	case http.StatusUnauthorized: // 401
		base.Code = CodeAuthFailed
		base.Message = fmt.Sprintf("AUTH_FAILED: %s rejected credentials — %s", service, truncate(body, 200))
		base.Retryable = false
	case http.StatusForbidden: // 403
		base.Code = CodeForbidden
		base.Message = fmt.Sprintf("FORBIDDEN: %s denied access — %s", service, truncate(body, 200))
		base.Retryable = false
	case http.StatusNotFound: // 404
		base.Code = CodeNotFound
		base.Message = fmt.Sprintf("NOT_FOUND: %s returned 404 — %s", service, truncate(body, 200))
		base.Retryable = false
	case http.StatusConflict: // 409
		base.Code = CodeBranchConflict
		base.Message = fmt.Sprintf("CONFLICT: %s returned 409 — %s", service, truncate(body, 200))
		base.Retryable = false
	case http.StatusTooManyRequests: // 429
		base.Code = CodeRateLimited
		base.Message = fmt.Sprintf("RATE_LIMITED: %s returned 429 — %s", service, truncate(body, 200))
		base.Retryable = true
		base.RetryAfter = 60 * time.Second
	case http.StatusInternalServerError: // 500
		base.Code = CodeServiceUnavailable
		base.Message = fmt.Sprintf("SERVICE_UNAVAILABLE: %s returned 500 — %s", service, truncate(body, 200))
		base.Retryable = true
		base.RetryAfter = 5 * time.Second
	case http.StatusBadGateway, http.StatusServiceUnavailable: // 502, 503
		base.Code = CodeConnectionRefused
		base.Message = fmt.Sprintf("CONNECTION_REFUSED: %s returned %d — %s", service, statusCode, truncate(body, 200))
		base.Retryable = true
		base.RetryAfter = 30 * time.Second
	case http.StatusGatewayTimeout: // 504
		base.Code = CodeTimeout
		base.Message = fmt.Sprintf("TIMEOUT: %s returned 504 — %s", service, truncate(body, 200))
		base.Retryable = true
		base.RetryAfter = 15 * time.Second
	default:
		if statusCode >= 500 {
			base.Code = CodeServiceUnavailable
			base.Message = fmt.Sprintf("SERVICE_UNAVAILABLE: %s returned %d", service, statusCode)
			base.Retryable = true
			base.RetryAfter = 5 * time.Second
		} else if statusCode >= 400 {
			base.Code = "CLIENT_ERROR"
			base.Message = fmt.Sprintf("CLIENT_ERROR: %s returned %d — %s", service, statusCode, truncate(body, 200))
			base.Retryable = false
		} else {
			base.Code = "UNKNOWN"
			base.Message = fmt.Sprintf("UNKNOWN: %s returned %d", service, statusCode)
			base.Retryable = false
		}
	}
	return base
}

// --- ClassifyError: spec §7 caller→callee table ----------------------------

// CallerCallee identifies a service-pair from the spec §7 table.
type CallerCallee struct {
	Caller string // e.g., "forgejo", "negotiate", "identity", "estimate", "axiom"
	Callee string // e.g., "chimera", "forgejo", "openrouter"
}

// ClassifyError implements the full spec §7 propagation table.  Given the
// caller, callee, HTTP status (0 for non-HTTP), and context, it returns the
// spec-mandated ServiceError with the exact propagated message.
func ClassifyError(caller, callee string, statusCode int, ctx ErrorContext) *ServiceError {
	switch {
	case caller == "forgejo" && callee == "chimera" && (statusCode == 0 || statusCode >= 500):
		return NewChimeraUnavailableError(ctx.Err)

	case caller == "negotiate" && callee == "chimera" && ctx.BudgetExhausted:
		return NewBudgetExhaustedError(ctx.Cost, ctx.Remaining)

	case caller == "identity" && callee == "forgejo" && statusCode == http.StatusServiceUnavailable:
		return NewConnectionRefusedError(callee, ctx.Attempt, ctx.MaxAttempts, ctx.RetryAfter, ctx.Err)

	case caller == "estimate" && callee == "openrouter" && statusCode == http.StatusUnauthorized:
		return NewAuthFailedError(callee, ctx.Err)

	case caller == "axiom" && callee == "forgejo" && statusCode == http.StatusConflict:
		return NewBranchConflictError(ctx.Branch)

	default:
		// Fall back to generic HTTP classification.
		return ClassifyHTTP(callee, statusCode, ctx.Body)
	}
}

// ErrorContext carries the contextual information needed to build spec-formatted
// error messages (cost amounts, branch names, retry counts).
type ErrorContext struct {
	Err             error         // Original underlying error
	Body            string        // HTTP response body (for generic classification)
	BudgetExhausted bool          // True when this is a budget exhaustion
	Cost            float64       // Tie-break cost (for BUDGET_EXHAUSTED)
	Remaining       float64       // Remaining budget (for BUDGET_EXHAUSTED)
	Attempt         int           // Retry attempt number (for CONNECTION_REFUSED)
	MaxAttempts     int           // Max retry attempts (for CONNECTION_REFUSED)
	RetryAfter      time.Duration // Suggested backoff
	Branch          string        // Branch name (for BRANCH_CONFLICT)
}

// --- Helpers ---------------------------------------------------------------

// IsRetryable returns true if err is a ServiceError marked retryable.
func IsRetryable(err error) bool {
	var se *ServiceError
	if errors.As(err, &se) {
		return se.Retryable
	}
	return false
}

// IsCode returns true if err is a ServiceError with the given code.
func IsCode(err error, code string) bool {
	var se *ServiceError
	if errors.As(err, &se) {
		return se.Code == code
	}
	return false
}

// GetRetryAfter extracts the RetryAfter duration from a ServiceError.
// Returns 0 if the error is not a ServiceError or has no retry hint.
func GetRetryAfter(err error) time.Duration {
	var se *ServiceError
	if errors.As(err, &se) {
		return se.RetryAfter
	}
	return 0
}

// truncate limits s to n characters, appending "…" if truncated.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
