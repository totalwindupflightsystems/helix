package estimate

import "errors"

// ---------------------------------------------------------------------------
// OpenRouter integration (spec §9) — v1 stub
// ---------------------------------------------------------------------------

// ErrNotImplemented signals that a feature is deferred to a later version.
// In v1 all OpenRouter network calls are stubbed: budget data comes from
// known-friends.json fixtures rather than live API queries.
var ErrNotImplemented = errors.New("not implemented: OpenRouter integration (v2)")

// OpenRouterClient is a stub for the OpenRouter API client. The production
// integration (spec §9.1) queries GET https://openrouter.ai/api/v1/key to read
// real-time spend against an agent's key limit. In v1 this returns
// ErrNotImplemented so callers can wire the call site without a network
// dependency in tests.
type OpenRouterClient struct {
	BaseURL string
}

// NewOpenRouterClient returns a client pointed at the given base URL
// (default https://openrouter.ai).
func NewOpenRouterClient(baseURL string) *OpenRouterClient {
	if baseURL == "" {
		baseURL = "https://openrouter.ai"
	}
	return &OpenRouterClient{BaseURL: baseURL}
}

// GetKeyUsage would return the current USD usage for an agent's OpenRouter API
// key. v1: not implemented — returns ErrNotImplemented.
func (c *OpenRouterClient) GetKeyUsage(apiKey string) (float64, error) {
	return 0, ErrNotImplemented
}

// GetKeyLimit would return the USD limit set on an agent's key. v1: not
// implemented — returns ErrNotImplemented.
func (c *OpenRouterClient) GetKeyLimit(apiKey string) (float64, error) {
	return 0, ErrNotImplemented
}
