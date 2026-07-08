// Package review — client_chimera.go
//
// ChimeraModelClient implements ModelClient via Chimera's multi-model
// deliberation API. It dispatches the review to Chimera's formation of
// models and returns the merged consensus result.
//
// API: POST http://localhost:8765/api/v1/deliberate
// Docs: specs/chimera-api.md

package review

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ChimeraModelClient implements ModelClient for Chimera multi-model deliberation.
type ChimeraModelClient struct {
	cfg    ModelClientConfig
	info   ModelInfo
	client *http.Client
}

// NewChimeraClient creates a Chimera model client.
func NewChimeraClient(cfg ModelClientConfig) *ChimeraModelClient {
	return &ChimeraModelClient{
		cfg: cfg,
		info: ModelInfo{
			Model:    cfg.Model,
			Provider: "chimera",
		},
		client: &http.Client{Timeout: cfg.timeout()},
	}
}

func (c *ChimeraModelClient) Info() ModelInfo { return c.info }

// Review submits the code for multi-model deliberation via Chimera.
func (c *ChimeraModelClient) Review(ctx context.Context, req ReviewRequest) (*ModelReviewResult, error) {
	start := time.Now()

	prompt := buildReviewPrompt(req, c.info)
	formation := formationForCategory(req.Context.Category)

	// Chimera deliberation API.
	type deliberateReq struct {
		Prompt    string            `json:"prompt"`
		Formation string            `json:"formation"`
		Models    map[string]string `json:"stage_models,omitempty"`
	}
	payload := deliberateReq{
		Prompt:    prompt,
		Formation: formation,
	}

	respBody, err := doJSONPost(ctx, c.client, c.cfg.BaseURL+"/api/v1/deliberate", c.cfg.APIKey, payload)
	if err != nil {
		return nil, fmt.Errorf("chimera: %w", err)
	}

	// Chimera returns: {"result": "...", "trace": [...]}
	var chimeraResp struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(respBody, &chimeraResp); err != nil {
		return nil, fmt.Errorf("chimera: parse deliberation response: %w", err)
	}

	result, err := parseReviewResponse([]byte(chimeraResp.Result), "chimera:"+c.cfg.Model)
	if err != nil {
		return nil, fmt.Errorf("chimera: %w", err)
	}
	result.Latency = time.Since(start)
	return result, nil
}

// formationForCategory maps change category to Chimera formation preset.
func formationForCategory(cat ChangeCategory) string {
	switch cat {
	case CategoryContract:
		return "rigorous" // full three-stage deliberation
	case CategoryBehavioral:
		return "balanced"
	case CategoryResilience:
		return "fast"
	default:
		return "auto"
	}
}
