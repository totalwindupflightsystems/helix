package negotiate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
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

// chimeraResponse is the JSON shape returned by Chimera's /deliberate endpoint.
// The testdata fixtures (chimera-arbiter-approve.json, chimera-arbiter-reject.json)
// use "status" for the verdict field, not "verdict".
type chimeraResponse struct {
	Status     string           `json:"status"`
	Confidence float64          `json:"confidence"`
	Summary    string           `json:"summary"`
	Findings   []chimeraFinding `json:"findings"`
	Trace      chimeraTrace     `json:"trace"`
}

type chimeraFinding struct {
	Severity    string `json:"severity"`
	Category    string `json:"category"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Description string `json:"description"`
}

type chimeraTrace struct {
	Source      string  `json:"source"`
	Duration    float64 `json:"duration"`
	TotalTokens int     `json:"total_tokens"`
}

// deliberationRequest is the JSON body sent to Chimera's /deliberate endpoint.
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
		Formation: "arbiter",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal deliberation request: %w", err)
	}

	url := c.BaseURL + "/deliberate"
	resp, err := c.Client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("CHIMERA_UNAVAILABLE: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CHIMERA_UNAVAILABLE: HTTP %d", resp.StatusCode)
	}

	var cr chimeraResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return nil, fmt.Errorf("parse Chimera response: %w", err)
	}

	return &ChimeraVerdict{
		Verdict:    cr.Status,
		Confidence: cr.Confidence,
		Cost:       estimateArbiterCost(cr.Trace.TotalTokens),
		Trace:      cr.Summary,
	}, nil
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
