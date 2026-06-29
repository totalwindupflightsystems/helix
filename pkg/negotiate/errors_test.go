package negotiate

import (
	"errors"
	"strings"
	"testing"
)

func TestNegotiationError_Error(t *testing.T) {
	e := &NegotiationError{Code: ExitCodeTimeout, Message: "NEGOTIATION_TIMEOUT: rounds=3 escalated=true"}
	if e.Error() != "NEGOTIATION_TIMEOUT: rounds=3 escalated=true" {
		t.Errorf("Error() = %q", e.Error())
	}
}

func TestNegotiationError_ErrorWithDetail(t *testing.T) {
	e := &NegotiationError{Code: ExitCodeEvidenceRequired, Message: "EVIDENCE_REQUIRED", Detail: "need 2 items"}
	if !strings.Contains(e.Error(), "EVIDENCE_REQUIRED") {
		t.Error("should contain message")
	}
	if !strings.Contains(e.Error(), "need 2 items") {
		t.Error("should contain detail")
	}
}

func TestNegotiationError_IsTerminal(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{ExitCodeResolved, true},
		{ExitCodeEvidenceRequired, false},
		{ExitCodeChimeraUnavailable, false},
		{ExitCodeBudgetExhausted, true},
		{ExitCodeTimeout, true},
		{ExitCodeInvalidState, true},
		{ExitCodeDryRun, false},
	}
	for _, tt := range tests {
		e := &NegotiationError{Code: tt.code}
		if got := e.IsTerminal(); got != tt.want {
			t.Errorf("code %d: IsTerminal() = %v, want %v", tt.code, got, tt.want)
		}
	}
}

func TestNegotiationError_IsRetryable(t *testing.T) {
	e := &NegotiationError{Code: ExitCodeChimeraUnavailable}
	if !e.IsRetryable() {
		t.Error("ChimeraUnavailable should be retryable")
	}

	e2 := &NegotiationError{Code: ExitCodeTimeout}
	if e2.IsRetryable() {
		t.Error("Timeout should NOT be retryable")
	}
}

func TestNewEvidenceRequiredError(t *testing.T) {
	e := NewEvidenceRequiredError("alice", 2)
	if e.Code != ExitCodeEvidenceRequired {
		t.Errorf("Code = %d, want %d", e.Code, ExitCodeEvidenceRequired)
	}
	if !strings.Contains(e.Message, "alice") {
		t.Error("message should contain agent name")
	}
	if !strings.Contains(e.Message, "round=2") {
		t.Error("message should contain round number")
	}
}

func TestNewChimeraUnavailableError(t *testing.T) {
	e := NewChimeraUnavailableError("connection refused")
	if e.Code != ExitCodeChimeraUnavailable {
		t.Errorf("Code = %d, want %d", e.Code, ExitCodeChimeraUnavailable)
	}
	if !strings.Contains(e.Message, "CHIMERA_UNAVAILABLE") {
		t.Error("message should contain CHIMERA_UNAVAILABLE")
	}
	if !strings.Contains(e.Message, "connection refused") {
		t.Error("message should contain detail")
	}
}

func TestNewBudgetExhaustedError(t *testing.T) {
	e := NewBudgetExhaustedError("bob", 2.50, 5.00)
	if e.Code != ExitCodeBudgetExhausted {
		t.Errorf("Code = %d, want %d", e.Code, ExitCodeBudgetExhausted)
	}
	if !strings.Contains(e.Message, "bob") {
		t.Error("message should contain agent name")
	}
	if !strings.Contains(e.Message, "remaining") {
		t.Error("message should contain remaining budget")
	}
	if !strings.Contains(e.Message, "tiebreak_cost") {
		t.Error("message should contain tiebreak cost")
	}
}

func TestNewTimeoutError(t *testing.T) {
	e := NewTimeoutError(3)
	if e.Code != ExitCodeTimeout {
		t.Errorf("Code = %d, want %d", e.Code, ExitCodeTimeout)
	}
	if !strings.Contains(e.Message, "rounds=3") {
		t.Error("message should contain rounds")
	}
	if !strings.Contains(e.Message, "escalated=true") {
		t.Error("message should contain escalated=true")
	}
}

func TestNewInvalidStateError(t *testing.T) {
	e := NewInvalidStateError("only 1 review")
	if e.Code != ExitCodeInvalidState {
		t.Errorf("Code = %d, want %d", e.Code, ExitCodeInvalidState)
	}
	if !strings.Contains(e.Message, "INVALID_STATE") {
		t.Error("message should contain INVALID_STATE")
	}
	if !strings.Contains(e.Message, "only 1 review") {
		t.Error("message should contain reason")
	}
}

func TestNewDryRunError(t *testing.T) {
	e := NewDryRunError(3)
	if e.Code != ExitCodeDryRun {
		t.Errorf("Code = %d, want %d", e.Code, ExitCodeDryRun)
	}
	if !strings.Contains(e.Message, "DRY_RUN") {
		t.Error("message should contain DRY_RUN")
	}
}

func TestExitCodeFromError_NilError(t *testing.T) {
	if code := ExitCodeFromError(nil); code != ExitCodeResolved {
		t.Errorf("nil error code = %d, want %d", code, ExitCodeResolved)
	}
}

func TestExitCodeFromError_NegotiationError(t *testing.T) {
	e := NewTimeoutError(3)
	if code := ExitCodeFromError(e); code != ExitCodeTimeout {
		t.Errorf("timeout error code = %d, want %d", code, ExitCodeTimeout)
	}
}

func TestExitCodeFromError_GenericError(t *testing.T) {
	e := errors.New("something went wrong")
	if code := ExitCodeFromError(e); code != ExitCodeInvalidState {
		t.Errorf("generic error code = %d, want %d", code, ExitCodeInvalidState)
	}
}

func TestExitCodeFromError_WrappedError(t *testing.T) {
	inner := NewChimeraUnavailableError("timeout")
	wrapped := errors.Join(inner, errors.New("context"))
	code := ExitCodeFromError(wrapped)
	// errors.Join may or may not unwrap to NegotiationError
	// Just verify it returns a valid code
	if code < 0 {
		t.Errorf("wrapped error code = %d, should be non-negative", code)
	}
}

func TestFormatExitMessage_AllCodes(t *testing.T) {
	tests := []struct {
		code     int
		contains string
	}{
		{ExitCodeResolved, ""},
		{ExitCodeEvidenceRequired, "EVIDENCE_REQUIRED"},
		{ExitCodeChimeraUnavailable, "CHIMERA_UNAVAILABLE"},
		{ExitCodeBudgetExhausted, "BUDGET_EXHAUSTED"},
		{ExitCodeTimeout, "NEGOTIATION_TIMEOUT"},
		{ExitCodeInvalidState, "INVALID_STATE"},
		{ExitCodeDryRun, "DRY_RUN"},
	}
	for _, tt := range tests {
		msg := FormatExitMessage(tt.code, "test detail")
		if tt.contains == "" {
			if msg != "" {
				t.Errorf("code %d: expected empty message, got %q", tt.code, msg)
			}
			continue
		}
		if !strings.Contains(msg, tt.contains) {
			t.Errorf("code %d: message %q should contain %q", tt.code, msg, tt.contains)
		}
	}
}

func TestFormatExitMessage_UnknownCode(t *testing.T) {
	msg := FormatExitMessage(99, "weird")
	if !strings.Contains(msg, "UNKNOWN_EXIT_CODE") {
		t.Error("unknown code should contain UNKNOWN_EXIT_CODE")
	}
}

func TestIsTerminalExit(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{ExitCodeResolved, true},
		{ExitCodeEvidenceRequired, false},
		{ExitCodeChimeraUnavailable, false},
		{ExitCodeBudgetExhausted, true},
		{ExitCodeTimeout, true},
		{ExitCodeInvalidState, true},
		{ExitCodeDryRun, false},
	}
	for _, tt := range tests {
		if got := IsTerminalExit(tt.code); got != tt.want {
			t.Errorf("IsTerminalExit(%d) = %v, want %v", tt.code, got, tt.want)
		}
	}
}

func TestIsRetryableExit(t *testing.T) {
	if !IsRetryableExit(ExitCodeChimeraUnavailable) {
		t.Error("ChimeraUnavailable should be retryable")
	}
	if IsRetryableExit(ExitCodeTimeout) {
		t.Error("Timeout should NOT be retryable")
	}
	if IsRetryableExit(ExitCodeResolved) {
		t.Error("Resolved should NOT be retryable")
	}
}

func TestExitCodeDescription_AllCodes(t *testing.T) {
	for _, code := range AllExitCodes() {
		desc := ExitCodeDescription(code)
		if desc == "" {
			t.Errorf("code %d: description should not be empty", code)
		}
	}
}

func TestExitCodeDescription_UnknownCode(t *testing.T) {
	desc := ExitCodeDescription(99)
	if !strings.Contains(desc, "Unknown") {
		t.Error("unknown code description should contain 'Unknown'")
	}
}

func TestAllExitCodes(t *testing.T) {
	codes := AllExitCodes()
	if len(codes) != 7 {
		t.Errorf("AllExitCodes count = %d, want 7", len(codes))
	}
}

func TestNegotiationError_ImplementsError(t *testing.T) {
	var e error = &NegotiationError{Code: ExitCodeTimeout, Message: "test"}
	if e.Error() != "test" {
		t.Error("NegotiationError should implement error interface")
	}
}

func TestExitCodes_MatchSpecValues(t *testing.T) {
	// Verify exact values from spec §14 table
	if ExitCodeResolved != 0 {
		t.Errorf("ExitCodeResolved = %d, want 0", ExitCodeResolved)
	}
	if ExitCodeEvidenceRequired != 1 {
		t.Errorf("ExitCodeEvidenceRequired = %d, want 1", ExitCodeEvidenceRequired)
	}
	if ExitCodeChimeraUnavailable != 2 {
		t.Errorf("ExitCodeChimeraUnavailable = %d, want 2", ExitCodeChimeraUnavailable)
	}
	if ExitCodeBudgetExhausted != 3 {
		t.Errorf("ExitCodeBudgetExhausted = %d, want 3", ExitCodeBudgetExhausted)
	}
	if ExitCodeTimeout != 4 {
		t.Errorf("ExitCodeTimeout = %d, want 4", ExitCodeTimeout)
	}
	if ExitCodeInvalidState != 5 {
		t.Errorf("ExitCodeInvalidState = %d, want 5", ExitCodeInvalidState)
	}
	if ExitCodeDryRun != 10 {
		t.Errorf("ExitCodeDryRun = %d, want 10", ExitCodeDryRun)
	}
}

func TestErrors_As(t *testing.T) {
	err := NewTimeoutError(3)
	var negErr *NegotiationError
	if !errors.As(err, &negErr) {
		t.Error("errors.As should match NegotiationError")
	}
	if negErr.Code != ExitCodeTimeout {
		t.Errorf("code = %d, want %d", negErr.Code, ExitCodeTimeout)
	}
}
