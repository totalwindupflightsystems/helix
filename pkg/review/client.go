// Package review — client.go
//
// Common HTTP client infrastructure for LLM-based review model clients.
// Provides config, prompt construction, and shared response parsing for
// Chimera and DeepSeek (and future) model integrations.

package review

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ModelClientConfig holds the configuration for any HTTP-based review model
// client. API key is read from environment if not set explicitly.
type ModelClientConfig struct {
	// BaseURL is the API endpoint base (e.g. "http://localhost:8765" or
	// "https://api.deepseek.com/v1").
	BaseURL string
	// APIKey is the auth token for the service.
	APIKey string
	// Model is the model identifier to use.
	Model string
	// Timeout is the HTTP client timeout. Default 120s.
	Timeout time.Duration
}

func (c *ModelClientConfig) timeout() time.Duration {
	if c.Timeout <= 0 {
		return 120 * time.Second
	}
	return c.Timeout
}

// =============================================================================
// Review prompt construction
// =============================================================================

// buildReviewPrompt constructs the prompt sent to an LLM for adversarial review.
// The prompt instructs the model to take a specific adversarial posture and
// return structured JSON findings.
func buildReviewPrompt(req ReviewRequest, modelInfo ModelInfo) string {
	rolePrompt := roleInstruction(req.Role, req.Context.Category)

	return fmt.Sprintf(`You are an adversarial code reviewer acting in role: %s.

%s

## Commit Message (bias-stripped)
%s

## Code Diff
%s

## Instructions
1. Analyze the diff for bugs, security issues, design flaws, and edge cases.
2. Return ONLY a JSON object with this exact structure (no markdown, no explanation):
{
  "verdict": "approved" | "block" | "pass_with_notes",
  "findings": [
    {
      "severity": "critical" | "high" | "medium" | "low",
      "type": "security" | "correctness" | "design" | "performance" | "style",
      "file": "filename",
      "line": 0,
      "description": "what the issue is",
      "evidence": "code snippet or reasoning"
    }
  ]
}

Verdict meanings:
- "approved": no issues found
- "block": critical issue that must be fixed before merge
- "pass_with_notes": minor issues found, not blocking

Return JSON only.`, req.Role, rolePrompt, req.NeutralCommitMsg, truncateDiff(req.Diff))
}

func roleInstruction(role ReviewRole, cat ChangeCategory) string {
	switch role {
	case RolePrimary:
		return fmt.Sprintf(`Your role is PRIMARY reviewer. Focus on structural correctness, spec compliance, and architectural fit. Change category: %s. Verify that the code fulfills its stated purpose correctly.`, cat)
	case RoleAdversarial:
		return fmt.Sprintf(`Your role is ADVERSARIAL reviewer. Your job is to BREAK the code. Find what the primary reviewer missed. Challenge every assumption. Look for edge cases, race conditions, injection vectors, and unintended side effects. Change category: %s. If you can't break it, the code passes.`, cat)
	case RoleAudit:
		return fmt.Sprintf(`Your role is AUDIT reviewer. Verify the adversarial reviewer's findings. For each finding the adversarial reviewer flagged: is it a real issue, or a false positive? Change category: %s. Provide evidence for your conclusions.`, cat)
	default:
		return fmt.Sprintf("Review the code change (category: %s).", cat)
	}
}

// truncateDiff limits diff size to avoid exceeding context windows.
func truncateDiff(diff string) string {
	const maxLen = 50000
	if len(diff) <= maxLen {
		return diff
	}
	return diff[:maxLen] + "\n... [diff truncated]"
}

// =============================================================================
// JSON response parsing
// =============================================================================

// parseReviewResponse decodes the LLM JSON response into a ModelReviewResult.
// It handles common LLM formatting artifacts (markdown fences, extra whitespace).
func parseReviewResponse(body []byte, modelName string) (*ModelReviewResult, error) {
	jsonStr := string(body)

	// Strip markdown fences if present.
	if idx := strings.Index(jsonStr, "```json"); idx >= 0 {
		jsonStr = jsonStr[idx+7:]
		if endIdx := strings.LastIndex(jsonStr, "```"); endIdx >= 0 {
			jsonStr = jsonStr[:endIdx]
		}
	} else if idx := strings.Index(jsonStr, "```"); idx >= 0 {
		jsonStr = jsonStr[idx+3:]
		if endIdx := strings.LastIndex(jsonStr, "```"); endIdx >= 0 {
			jsonStr = jsonStr[:endIdx]
		}
	}

	// Find the outermost { } to extract JSON object.
	if start := strings.Index(jsonStr, "{"); start >= 0 {
		if end := strings.LastIndex(jsonStr, "}"); end > start {
			jsonStr = jsonStr[start : end+1]
		}
	}

	jsonStr = strings.TrimSpace(jsonStr)

	var result ModelReviewResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("parse response: %w (raw: %.200s)", err, jsonStr)
	}

	// Attach model name to findings.
	for i := range result.Findings {
		result.Findings[i].Model = modelName
	}

	return &result, nil
}

// =============================================================================
// HTTP helpers
// =============================================================================

// doJSONPost sends a JSON POST request and returns the response body.
func doJSONPost(ctx context.Context, httpClient *http.Client, url, apiKey string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB max
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http %d: %.500s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}
