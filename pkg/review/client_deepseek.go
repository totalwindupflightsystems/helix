// Package review — client_deepseek.go
//
// DeepSeekModelClient implements ModelClient via the DeepSeek API
// (OpenAI-compatible chat completions). It constructs a review prompt,
// sends it to the model, and parses the structured JSON response.
//
// API: POST https://api.deepseek.com/v1/chat/completions

package review

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// DeepSeekModelClient implements ModelClient for a single-model DeepSeek review.
type DeepSeekModelClient struct {
	cfg    ModelClientConfig
	info   ModelInfo
	client *http.Client
}

// NewDeepSeekClient creates a DeepSeek model client.
func NewDeepSeekClient(cfg ModelClientConfig) *DeepSeekModelClient {
	if cfg.Model == "" {
		cfg.Model = "deepseek-v4-flash"
	}
	return &DeepSeekModelClient{
		cfg: cfg,
		info: ModelInfo{
			Model:    cfg.Model,
			Provider: "deepseek",
		},
		client: &http.Client{
			Timeout: cfg.timeout(),
			Transport: otelhttp.NewTransport(
				http.DefaultTransport,
				otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string {
					return "deepseek." + r.Method + " " + r.URL.Path
				}),
			),
		},
	}
}

func (c *DeepSeekModelClient) Info() ModelInfo { return c.info }

// Review submits the code for adversarial review via DeepSeek chat completions.
func (c *DeepSeekModelClient) Review(ctx context.Context, req ReviewRequest) (*ModelReviewResult, error) {
	start := time.Now()

	prompt := buildReviewPrompt(req, c.info)

	type chatMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type chatReq struct {
		Model       string    `json:"model"`
		Messages    []chatMsg `json:"messages"`
		Temperature float64   `json:"temperature"`
		MaxTokens   int       `json:"max_tokens"`
	}
	payload := chatReq{
		Model: c.cfg.Model,
		Messages: []chatMsg{
			{Role: "system", Content: "You are an expert adversarial code reviewer. Return ONLY valid JSON."},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.1,
		MaxTokens:   4096,
	}

	respBody, err := doJSONPost(ctx, c.client, c.cfg.BaseURL+"/chat/completions", c.cfg.APIKey, payload)
	if err != nil {
		return nil, fmt.Errorf("deepseek: %w", err)
	}

	// OpenAI-compatible response.
	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("deepseek: parse chat response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("deepseek: no choices in response")
	}

	content := chatResp.Choices[0].Message.Content
	if content == "" {
		return nil, fmt.Errorf("deepseek: empty content in response")
	}

	result, err := parseReviewResponse([]byte(content), "deepseek:"+c.cfg.Model)
	if err != nil {
		return nil, fmt.Errorf("deepseek: %w", err)
	}
	result.Latency = time.Since(start)
	return result, nil
}
