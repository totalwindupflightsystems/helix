// Package review — client_chimera.go
//
// ChimeraModelClient implements ModelClient via Chimera's multi-model
// deliberation API. It dispatches the review to Chimera's formation of
// models and returns the merged consensus result.
//
// API: POST http://localhost:8765/v1/deliberate
// Docs: specs/chimera-api.md
//
// NOTE: the route is /v1/deliberate — chimera does NOT serve
// /api/v1/deliberate (that path returns 404; DF-018).

package review

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
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
		client: &http.Client{
			Timeout: cfg.timeout(),
			Transport: otelhttp.NewTransport(
				http.DefaultTransport,
				otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string {
					return "chimera." + r.Method + " " + r.URL.Path
				}),
			),
		},
	}
}

func (c *ChimeraModelClient) Info() ModelInfo { return c.info }

// Review submits the code for multi-model deliberation via Chimera.
func (c *ChimeraModelClient) Review(ctx context.Context, req ReviewRequest) (*ModelReviewResult, error) {
	start := time.Now()

	prompt := buildReviewPrompt(req, c.info)
	formation := formationForCategory(req.Context.Category)

	// Chimera deliberation API request (DeliberateRequest in chimera's
	// OpenAPI): only `prompt` is required; the rest are optional model-routing
	// overrides. The previously-sent `stage_models` field is accepted by the
	// server but unused here — replaced by the documented override fields.
	type deliberateReq struct {
		Prompt           string   `json:"prompt"`
		Formation        string   `json:"formation"`
		AllowedModels    []string `json:"allowed_models,omitempty"`
		DisallowedModels []string `json:"disallowed_models,omitempty"`
		DispatcherModel  string   `json:"dispatcher_model,omitempty"`
		AggregatorModel  string   `json:"aggregator_model,omitempty"`
		WorkerModel      string   `json:"worker_model,omitempty"`
	}
	payload := deliberateReq{
		Prompt:    prompt,
		Formation: formation,
	}

	respBody, err := doJSONPost(ctx, c.client, c.cfg.BaseURL+"/v1/deliberate", c.cfg.APIKey, payload)
	if err != nil {
		return nil, fmt.Errorf("chimera: %w", err)
	}

	// Chimera returns DeliberateResponse: {answer, trace, request_id} — all
	// three required. The merged deliberation text lives in "answer" (there
	// is NO "result" field); "trace" is the per-stage execution-graph object,
	// so decoding it as a map also enforces the object shape.
	var chimeraResp struct {
		Answer    string                 `json:"answer"`
		Trace     map[string]interface{} `json:"trace"`
		RequestID string                 `json:"request_id"`
	}
	if err := json.Unmarshal(respBody, &chimeraResp); err != nil {
		return nil, fmt.Errorf("chimera: parse deliberation response: %w", err)
	}

	result, err := parseReviewResponse([]byte(chimeraResp.Answer), "chimera:"+c.cfg.Model)
	if err != nil {
		return nil, fmt.Errorf("chimera: %w", err)
	}
	result.Latency = time.Since(start)
	return result, nil
}

// formationForCategory maps change category to a Chimera formation preset.
//
// Only formations that exist on the live Chimera server are used
// (/v1/formations at chimera 0.2.0: audit, auto, debate, simple,
// spec-writer, speed — DF-021: "rigorous"/"balanced"/"fast" were never
// valid and made every deliberation 422):
//   - CategoryContract -> "audit": full three-stage deliberation with an
//     audit pass, preserving the original "rigorous" intent.
//   - CategoryBehavioral -> "debate": best_of_n merge across two aggregator
//     runs, preserving the consensus intent of the old "balanced".
//   - CategoryResilience -> "speed": fast 2-worker preset, preserving the
//     old "fast" intent.
func formationForCategory(cat ChangeCategory) string {
	switch cat {
	case CategoryContract:
		return "audit"
	case CategoryBehavioral:
		return "debate"
	case CategoryResilience:
		return "speed"
	default:
		return "auto"
	}
}
