package negotiate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ArbiterClient calls Chimera's arbiter formation for tie-break resolution (spec §9).
type ArbiterClient struct {
	BaseURL string // e.g., "http://localhost:8765"
	Client  *http.Client
}

// NewArbiterClient creates an ArbiterClient with the given Chimera base URL.
func NewArbiterClient(baseURL string) *ArbiterClient {
	return &ArbiterClient{
		BaseURL: baseURL,
		Client:  &http.Client{Timeout: 300 * time.Second},
	}
}

// chimeraResponse is the JSON shape returned by Chimera's /v1/deliberate
// endpoint (verified live 2026-09-01): {answer, trace, request_id}.
// The verdict text lives in "answer"; "trace" is an execution-graph object
// that does NOT reliably carry {source, duration, total_tokens}.
type chimeraResponse struct {
	Answer    string                 `json:"answer"`
	Trace     map[string]interface{} `json:"trace"`
	RequestID string                 `json:"request_id"`
}

// deliberationRequest is the JSON body sent to Chimera's /v1/deliberate endpoint.
type deliberationRequest struct {
	Prompt    string `json:"prompt"`
	Formation string `json:"formation"`
}

// Deliberate sends the negotiation context to Chimera's arbiter formation.
// The prompt includes PR context, agent reviews, and full debate transcript.
// Returns a ChimeraVerdict with the APPROVE/REJECT decision.
func (c *ArbiterClient) Deliberate(prompt string) (*ChimeraVerdict, error) {
	payload := deliberationRequest{
		Prompt:    prompt,
		Formation: "debate",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal deliberation request: %w", err)
	}

	// Live Chimera serves /v1/deliberate — NOT /deliberate (404) and NOT
	// /api/v1/deliberate (404). Verified live 2026-09-01 (DF-HELIX-1).
	url := strings.TrimRight(c.BaseURL, "/") + "/v1/deliberate"
	resp, err := c.Client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, NewChimeraUnavailableError(err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Include a body snippet so 404 (path) vs 422 (payload) vs 5xx
		// (down) are distinguishable.
		snippet := ""
		if b, rerr := io.ReadAll(io.LimitReader(resp.Body, 512)); rerr == nil {
			snippet = strings.TrimSpace(string(b))
		}
		detail := fmt.Sprintf("HTTP %d", resp.StatusCode)
		if snippet != "" {
			detail += ": " + snippet
		}
		return nil, NewChimeraUnavailableError(detail)
	}

	var cr chimeraResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return nil, fmt.Errorf("parse Chimera response: %w", err)
	}

	verdict, err := parseChimeraVerdict(cr.Answer)
	if err != nil {
		return nil, err
	}

	return &ChimeraVerdict{
		Verdict:    verdict,
		Confidence: 0, // live API does not return confidence — defensive zero
		Cost:       estimateArbiterCost(0),
		Trace:      cr.Answer,
	}, nil
}

// parseChimeraVerdict extracts an APPROVE/REJECT verdict from Chimera's
// free-text "answer" field. The live server returns the verdict as plain
// text (e.g. "REJECT"); tolerate surrounding prose and case variants.
// "APPROVED" normalizes to "APPROVE" (the ChimeraVerdict convention).
func parseChimeraVerdict(answer string) (string, error) {
	upper := strings.ToUpper(strings.TrimSpace(answer))
	switch {
	case strings.Contains(upper, "APPROVE"):
		return "APPROVE", nil
	case strings.Contains(upper, "REJECT"):
		return "REJECT", nil
	default:
		return "", fmt.Errorf("parse Chimera response: no APPROVE/REJECT verdict in answer %q", answer)
	}
}

// SplitCost divides the tie-break cost evenly between two agents (spec §9.3).
func SplitCost(cost float64) (agentAShare, agentBShare float64) {
	half := cost / 2.0
	return half, half
}

// estimateArbiterCost provides a rough cost estimate from token usage.
// Approximate rate: $0.32 per million tokens (cache-heavy arbiter formation).
func estimateArbiterCost(totalTokens int) float64 {
	return float64(totalTokens) * 0.00000032
}
