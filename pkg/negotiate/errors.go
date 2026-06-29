package negotiate

// errors.go implements the negotiation error taxonomy per
// specs/pr-negotiation.md §14 (Error Taxonomy and Exit Codes).
//
// Exit codes:
//   0  — Negotiation resolved (agreement or tie-break)
//   1  — Insufficient evidence in debate comment
//   2  — Chimera unavailable
//   3  — Budget exhausted (either agent)
//   4  — Timeout (30 min)
//   5  — Invalid state (e.g., only 1 review)
//   10 — Dry-run mode

import (
	"errors"
	"fmt"
)

// Exit codes from spec §14.
const (
	ExitCodeResolved           = 0
	ExitCodeEvidenceRequired   = 1
	ExitCodeChimeraUnavailable = 2
	ExitCodeBudgetExhausted    = 3
	ExitCodeTimeout            = 4
	ExitCodeInvalidState       = 5
)

// NegotiationError is a typed error with an exit code from spec §14.
type NegotiationError struct {
	Code    int
	Message string
	Detail  string
}

// Error implements the error interface.
func (e *NegotiationError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%s: %s", e.Message, e.Detail)
	}
	return e.Message
}

// Unwrap allows errors.Is / errors.As to work with wrapped errors.
func (e *NegotiationError) Unwrap() error {
	return errors.New(e.Message)
}

// IsTerminal returns true if this exit code means the negotiation is done
// and cannot continue (resolved, timeout, budget exhausted, invalid state).
func (e *NegotiationError) IsTerminal() bool {
	switch e.Code {
	case ExitCodeResolved, ExitCodeTimeout, ExitCodeBudgetExhausted, ExitCodeInvalidState:
		return true
	default:
		return false
	}
}

// IsRetryable returns true if the error might succeed on retry
// (Chimera unavailable — might come back).
func (e *NegotiationError) IsRetryable() bool {
	return e.Code == ExitCodeChimeraUnavailable
}

// --- Error constructors (spec §14 message formats) ---

// NewEvidenceRequiredError creates exit code 1: insufficient evidence.
func NewEvidenceRequiredError(agent string, round int) *NegotiationError {
	return &NegotiationError{
		Code:    ExitCodeEvidenceRequired,
		Message: fmt.Sprintf("EVIDENCE_REQUIRED: agent=%s round=%d", agent, round),
		Detail:  "minimum 2 evidence items required per spec §7.2",
	}
}

// NewChimeraUnavailableError creates exit code 2: Chimera service unavailable.
func NewChimeraUnavailableError(detail string) *NegotiationError {
	return &NegotiationError{
		Code:    ExitCodeChimeraUnavailable,
		Message: fmt.Sprintf("CHIMERA_UNAVAILABLE: %s", detail),
	}
}

// NewBudgetExhaustedError creates exit code 3: budget exhausted.
func NewBudgetExhaustedError(agent string, remaining, tiebreakCost float64) *NegotiationError {
	return &NegotiationError{
		Code: ExitCodeBudgetExhausted,
		Message: fmt.Sprintf("BUDGET_EXHAUSTED: agent=%s remaining=$%.2f tiebreak_cost=$%.2f",
			agent, remaining, tiebreakCost),
	}
}

// NewTimeoutError creates exit code 4: negotiation exceeded global timeout.
func NewTimeoutError(rounds int) *NegotiationError {
	return &NegotiationError{
		Code:    ExitCodeTimeout,
		Message: fmt.Sprintf("NEGOTIATION_TIMEOUT: rounds=%d escalated=true", rounds),
	}
}

// NewInvalidStateError creates exit code 5: invalid negotiation state.
func NewInvalidStateError(reason string) *NegotiationError {
	return &NegotiationError{
		Code:    ExitCodeInvalidState,
		Message: fmt.Sprintf("INVALID_STATE: %s", reason),
	}
}

// NewDryRunError creates exit code 10: dry-run mode (not an error per se,
// but uses the exit code system per spec §14).
func NewDryRunError(rounds int) *NegotiationError {
	return &NegotiationError{
		Code:    ExitCodeDryRun,
		Message: fmt.Sprintf("DRY_RUN: would negotiate %d rounds", rounds),
	}
}

// --- Utility functions ---

// ExitCodeFromError extracts the exit code from a NegotiationError.
// Returns 5 (INVALID_STATE) for non-NegotiationError errors.
func ExitCodeFromError(err error) int {
	if err == nil {
		return ExitCodeResolved
	}
	var negErr *NegotiationError
	if errors.As(err, &negErr) {
		return negErr.Code
	}
	return ExitCodeInvalidState
}

// FormatExitMessage renders the spec §14 message format for an exit code.
func FormatExitMessage(code int, detail string) string {
	switch code {
	case ExitCodeResolved:
		return ""
	case ExitCodeEvidenceRequired:
		return fmt.Sprintf("EVIDENCE_REQUIRED: %s", detail)
	case ExitCodeChimeraUnavailable:
		return fmt.Sprintf("CHIMERA_UNAVAILABLE: %s", detail)
	case ExitCodeBudgetExhausted:
		return fmt.Sprintf("BUDGET_EXHAUSTED: %s", detail)
	case ExitCodeTimeout:
		return fmt.Sprintf("NEGOTIATION_TIMEOUT: %s escalated=true", detail)
	case ExitCodeInvalidState:
		return fmt.Sprintf("INVALID_STATE: %s", detail)
	case ExitCodeDryRun:
		return fmt.Sprintf("DRY_RUN: %s", detail)
	default:
		return fmt.Sprintf("UNKNOWN_EXIT_CODE(%d): %s", code, detail)
	}
}

// IsTerminalExit checks if the exit code means negotiation is permanently done.
func IsTerminalExit(code int) bool {
	switch code {
	case ExitCodeResolved, ExitCodeTimeout, ExitCodeBudgetExhausted, ExitCodeInvalidState:
		return true
	default:
		return false
	}
}

// IsRetryableExit checks if the exit code might succeed on retry.
func IsRetryableExit(code int) bool {
	return code == ExitCodeChimeraUnavailable
}

// ExitCodeDescription returns a human-readable description of an exit code.
func ExitCodeDescription(code int) string {
	switch code {
	case ExitCodeResolved:
		return "Negotiation resolved (agreement or tie-break)"
	case ExitCodeEvidenceRequired:
		return "Insufficient evidence in debate comment"
	case ExitCodeChimeraUnavailable:
		return "Chimera unavailable"
	case ExitCodeBudgetExhausted:
		return "Budget exhausted (either agent)"
	case ExitCodeTimeout:
		return "Timeout (30 min)"
	case ExitCodeInvalidState:
		return "Invalid state (e.g., only 1 review)"
	case ExitCodeDryRun:
		return "Dry-run mode"
	default:
		return fmt.Sprintf("Unknown exit code %d", code)
	}
}

// AllExitCodes returns all valid exit codes for iteration.
func AllExitCodes() []int {
	return []int{
		ExitCodeResolved,
		ExitCodeEvidenceRequired,
		ExitCodeChimeraUnavailable,
		ExitCodeBudgetExhausted,
		ExitCodeTimeout,
		ExitCodeInvalidState,
		ExitCodeDryRun,
	}
}
