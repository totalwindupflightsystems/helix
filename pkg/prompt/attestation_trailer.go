package prompt

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Helix-Attestation trailer (spec §2.2 Step 5 data contract)
// ---------------------------------------------------------------------------

// TokenUsage records the input/output token counts for an attestation.
type TokenUsage struct {
	Input  int `json:"input"`
	Output int `json:"output"`
}

// HelixAttestation is the structured JSON trailer embedded in commit messages
// per spec §2.2 Step 5. The format is:
//
//	Helix-Attestation: {
//	  "task_id": "uuid-v4",
//	  "prompt_hash": "sha256:hex",
//	  "model": "provider/model-name",
//	  "context_hash": "sha256:hex",
//	  "cost_usd": 0.0023,
//	  "tokens": {"input": 12000, "output": 3400},
//	  "langfuse_trace_id": "string",
//	  "agent": "tower-axiom",
//	  "confidence": 72
//	}
type HelixAttestation struct {
	TaskID          string     `json:"task_id"`
	PromptHash      string     `json:"prompt_hash"`
	Model           string     `json:"model"`
	ContextHash     string     `json:"context_hash"`
	CostUSD         float64    `json:"cost_usd"`
	Tokens          TokenUsage `json:"tokens"`
	LangfuseTraceID string     `json:"langfuse_trace_id"`
	Agent           string     `json:"agent"`
	Confidence      int        `json:"confidence"`
}

// reHelixAttestationStart matches the Helix-Attestation trailer line prefix.
// We capture only the start, then manually find the matching closing brace to
// handle nested JSON objects (e.g. tokens: {"input": N}).
var reHelixAttestationStart = regexp.MustCompile(
	`(?m)^Helix-Attestation:\s*(\{)`,
)

// ParseHelixAttestation extracts the Helix-Attestation JSON trailer from a
// commit message body. Returns the parsed struct, or an error if the trailer
// is absent or malformed.
func ParseHelixAttestation(commitMsg string) (*HelixAttestation, error) {
	loc := reHelixAttestationStart.FindStringSubmatchIndex(commitMsg)
	if loc == nil {
		return nil, fmt.Errorf("Helix-Attestation trailer not found in commit message")
	}

	// loc[2] is the start of group 1 (the opening brace)
	braceStart := loc[2]
	raw := extractBalancedJSON(commitMsg, braceStart)
	if raw == "" {
		return nil, fmt.Errorf("malformed Helix-Attestation JSON: could not find matching closing brace")
	}

	var ha HelixAttestation
	if err := json.Unmarshal([]byte(raw), &ha); err != nil {
		return nil, fmt.Errorf("malformed Helix-Attestation JSON: %w", err)
	}

	return &ha, nil
}

// extractBalancedJSON extracts a JSON object string starting at position
// `start` in `s`, accounting for nested braces. Returns "" if no matching
// closing brace is found.
func extractBalancedJSON(s string, start int) string {
	if start >= len(s) || s[start] != '{' {
		return ""
	}

	depth := 0
	inString := false
	escaped := false

	for i := start; i < len(s); i++ {
		ch := s[i]

		if escaped {
			escaped = false
			continue
		}

		if ch == '\\' {
			escaped = true
			continue
		}

		if ch == '"' {
			inString = !inString
			continue
		}

		if inString {
			continue
		}

		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}

	return ""
}

// FormatHelixAttestation renders a HelixAttestation as the commit-message
// trailer string. The JSON is compact (single line) for reliable parsing.
func FormatHelixAttestation(ha *HelixAttestation) (string, error) {
	if ha == nil {
		return "", fmt.Errorf("nil HelixAttestation")
	}
	if ha.PromptHash == "" {
		return "", fmt.Errorf("PromptHash is required")
	}

	data, err := json.Marshal(ha)
	if err != nil {
		return "", fmt.Errorf("marshal HelixAttestation: %w", err)
	}

	return fmt.Sprintf("Helix-Attestation: %s", string(data)), nil
}

// AppendHelixAttestation appends the trailer to a commit message body,
// inserting a blank line separator if needed.
func AppendHelixAttestation(commitMsg string, ha *HelixAttestation) (string, error) {
	trailer, err := FormatHelixAttestation(ha)
	if err != nil {
		return "", err
	}

	msg := strings.TrimRight(commitMsg, "\n")
	if !strings.HasSuffix(msg, "\n\n") {
		msg += "\n"
	}
	return msg + trailer + "\n", nil
}

// HasHelixAttestation returns true if the commit message contains a
// Helix-Attestation trailer.
func HasHelixAttestation(commitMsg string) bool {
	return reHelixAttestationStart.MatchString(commitMsg)
}

// ValidateHelixAttestation checks structural validity of a parsed attestation.
// Returns nil if valid, or an error listing all issues found.
func ValidateHelixAttestation(ha *HelixAttestation) error {
	if ha == nil {
		return fmt.Errorf("nil attestation")
	}

	var issues []string

	if ha.PromptHash == "" {
		issues = append(issues, "prompt_hash is required")
	} else if !strings.HasPrefix(ha.PromptHash, "sha256:") {
		issues = append(issues, fmt.Sprintf("prompt_hash must start with 'sha256:', got %q", ha.PromptHash[:min(len(ha.PromptHash), 20)]))
	}

	if ha.Model == "" {
		issues = append(issues, "model is required")
	}

	if ha.Agent == "" {
		issues = append(issues, "agent is required")
	}

	if ha.ContextHash != "" && !strings.HasPrefix(ha.ContextHash, "sha256:") {
		issues = append(issues, fmt.Sprintf("context_hash must start with 'sha256:' if present, got prefix %q", ha.ContextHash[:min(len(ha.ContextHash), 20)]))
	}

	if ha.Confidence < 0 || ha.Confidence > 100 {
		issues = append(issues, fmt.Sprintf("confidence must be 0-100, got %d", ha.Confidence))
	}

	if ha.CostUSD < 0 {
		issues = append(issues, fmt.Sprintf("cost_usd must be non-negative, got %f", ha.CostUSD))
	}

	if len(issues) > 0 {
		return fmt.Errorf("attestation validation failed: %s", strings.Join(issues, "; "))
	}
	return nil
}

// HelixAttestationFromLegacy converts the legacy Attestation struct (from the
// simple `Prompt:` line format) into a HelixAttestation. Fields not present in
// the legacy format are zero-valued.
func HelixAttestationFromLegacy(att *Attestation) *HelixAttestation {
	if att == nil {
		return nil
	}
	return &HelixAttestation{
		PromptHash: att.Hash,
		Model:      att.Model,
		CostUSD:    att.EstimatedCostUSD,
		Agent:      att.AgentAuthor,
	}
}

// HelixAttestationToLegacy converts a HelixAttestation into the legacy
// Attestation struct (for compatibility with existing validation code).
func HelixAttestationToLegacy(ha *HelixAttestation) *Attestation {
	if ha == nil {
		return nil
	}
	return &Attestation{
		Hash:             ha.PromptHash,
		Model:            ha.Model,
		EstimatedCostUSD: ha.CostUSD,
		AgentAuthor:      ha.Agent,
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
