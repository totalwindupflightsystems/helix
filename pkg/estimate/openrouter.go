package estimate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ---------------------------------------------------------------------------
// OpenRouter integration (spec §9)
// ---------------------------------------------------------------------------

// OpenRouterClient queries the OpenRouter API for real-time key budget data.
// Per spec §9.1, it calls GET https://openrouter.ai/api/v1/key with the
// agent's API key as a Bearer token. The response contains usage (USD spent
// this period), limit (USD cap on the key), and limit_remaining.
type OpenRouterClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

// openrouterKeyResponse is the JSON structure returned by GET /api/v1/key.
type openrouterKeyResponse struct {
	Data struct {
		Label          string  `json:"label"`
		Limit          float64 `json:"limit"`
		Usage          float64 `json:"usage"`
		LimitRemaining float64 `json:"limit_remaining"`
		IsFreeTier     bool    `json:"is_free_tier"`
	} `json:"data"`
}

// NewOpenRouterClient returns a client pointed at the given base URL
// (default https://openrouter.ai).
func NewOpenRouterClient(baseURL string) *OpenRouterClient {
	if baseURL == "" {
		baseURL = "https://openrouter.ai"
	}
	return &OpenRouterClient{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// getKeyResponse fetches and parses the full key info response from the
// OpenRouter API. It is the shared implementation for GetKeyUsage,
// GetKeyLimit, and GetKeyRemaining.
func (c *OpenRouterClient) getKeyResponse(ctx context.Context, apiKey string) (*openrouterKeyResponse, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("openrouter: empty API key")
	}

	url := c.BaseURL + "/api/v1/key"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("openrouter: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB max
	if err != nil {
		return nil, fmt.Errorf("openrouter: read response: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, ErrAuthFailed
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, ErrRateLimited
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("openrouter: server error (HTTP %d)", resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("openrouter: unexpected status (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result openrouterKeyResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("openrouter: parse response: %w", err)
	}

	return &result, nil
}

// GetKeyUsage returns the current USD usage for an agent's OpenRouter API key.
// Per spec §9.2, this is the authoritative spend data.
func (c *OpenRouterClient) GetKeyUsage(ctx context.Context, apiKey string) (float64, error) {
	result, err := c.getKeyResponse(ctx, apiKey)
	if err != nil {
		return 0, err
	}
	return result.Data.Usage, nil
}

// GetKeyLimit returns the USD limit set on an agent's key.
func (c *OpenRouterClient) GetKeyLimit(ctx context.Context, apiKey string) (float64, error) {
	result, err := c.getKeyResponse(ctx, apiKey)
	if err != nil {
		return 0, err
	}
	return result.Data.Limit, nil
}

// GetKeyRemaining returns the USD remaining on an agent's key.
func (c *OpenRouterClient) GetKeyRemaining(ctx context.Context, apiKey string) (float64, error) {
	result, err := c.getKeyResponse(ctx, apiKey)
	if err != nil {
		return 0, err
	}
	return result.Data.LimitRemaining, nil
}

// GetKeyInfo returns the full key info (usage, limit, remaining, label, free_tier).
func (c *OpenRouterClient) GetKeyInfo(ctx context.Context, apiKey string) (*KeyInfo, error) {
	result, err := c.getKeyResponse(ctx, apiKey)
	if err != nil {
		return nil, err
	}
	return &KeyInfo{
		Label:          result.Data.Label,
		Limit:          result.Data.Limit,
		Usage:          result.Data.Usage,
		LimitRemaining: result.Data.LimitRemaining,
		IsFreeTier:     result.Data.IsFreeTier,
	}, nil
}

// KeyInfo holds the full budget state of an OpenRouter API key.
type KeyInfo struct {
	Label          string
	Limit          float64
	Usage          float64
	LimitRemaining float64
	IsFreeTier     bool
}

// BudgetRemaining returns the fraction of the budget remaining (0.0–1.0).
// Returns 0 if the limit is 0 (unlimited or not set).
func (k *KeyInfo) BudgetRemaining() float64 {
	if k.Limit == 0 {
		return 0
	}
	return k.LimitRemaining / k.Limit
}

// BudgetUsed returns the fraction of the budget used (0.0–1.0).
func (k *KeyInfo) BudgetUsed() float64 {
	if k.Limit == 0 {
		return 0
	}
	return k.Usage / k.Limit
}

// ---------------------------------------------------------------------------
// Error sentinels
// ---------------------------------------------------------------------------

// ErrAuthFailed indicates the API key is invalid, expired, or revoked.
var ErrAuthFailed = fmt.Errorf("openrouter: authentication failed (HTTP 401) — key may be dead")

// ErrRateLimited indicates the API rate limit was exceeded.
var ErrRateLimited = fmt.Errorf("openrouter: rate limited (HTTP 429)")
