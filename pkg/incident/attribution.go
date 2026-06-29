package incident

import (
	"fmt"
	"time"
)

// =============================================================================
// Incident Attribution Engine
//
// Per spec §Production Incident Attribution:
//   1. Identify changed code paths in incident window
//   2. For each changed path, find merge commit and responsible agents
//   3. Apply attribution weights:
//      - Author agent:    70% responsibility
//      - Reviewer agents: 20% (shared equally)
//      - Approving human: 10%
//   4. Record in trust ledger with evidence links
//   5. Trigger trust score recalculation
//
// Shared responsibility is intentional — an agent that rubber-stamps reviews
// shares blame when the code fails. This incentivizes thorough review.
// =============================================================================

// AttributionWeights defines the responsibility split for an incident.
// Defaults per spec: author 70%, reviewers 20% (shared), approver 10%.
type AttributionWeights struct {
	Author    float64 `json:"author"`
	Reviewers float64 `json:"reviewers"` // total, split among all reviewers
	Approver  float64 `json:"approver"`
}

// DefaultAttributionWeights returns the spec-compliant attribution weights.
func DefaultAttributionWeights() AttributionWeights {
	return AttributionWeights{
		Author:    0.70,
		Reviewers: 0.20,
		Approver:  0.10,
	}
}

// ChangePath represents a changed code path involved in an incident.
type ChangePath struct {
	FilePath    string   `json:"file_path"`
	MergeSHA    string   `json:"merge_sha"`
	AuthorID    string   `json:"author_id"`
	ReviewerIDs []string `json:"reviewer_ids"`
	ApproverID  string   `json:"approver_id"`
	CommitTime  time.Time `json:"commit_time"`
}

// AttributionResult holds the computed responsibility distribution.
type AttributionResult struct {
	IncidentID    string                 `json:"incident_id"`
	Responsibility map[string]float64    `json:"responsibility"` // agentID → weight (0.0–1.0)
	ChangePaths   []ChangePath           `json:"change_paths"`
	EvidenceLinks []string               `json:"evidence_links"`
	Timestamp     time.Time              `json:"timestamp"`
}

// AttributionEngine computes incident responsibility distribution.
type AttributionEngine struct {
	weights AttributionWeights
	store   *Store
}

// NewAttributionEngine creates an engine with default weights and the given store.
func NewAttributionEngine(store *Store) *AttributionEngine {
	return &AttributionEngine{
		weights: DefaultAttributionWeights(),
		store:   store,
	}
}

// WithWeights sets custom attribution weights.
func (e *AttributionEngine) WithWeights(w AttributionWeights) *AttributionEngine {
	e.weights = w
	return e
}

// Attribute traces a causal chain from incident → changed code → responsible agents
// and computes the responsibility distribution.
//
// If multiple change paths exist, responsibility is accumulated across paths,
// then normalized so the total sums to 1.0.
func (e *AttributionEngine) Attribute(incidentID string, paths []ChangePath, evidence []string) (*AttributionResult, error) {
	if incidentID == "" {
		return nil, fmt.Errorf("incident ID is required")
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("at least one change path is required")
	}

	responsibility := make(map[string]float64)

	for _, path := range paths {
		// Author gets the full author weight for this path.
		if path.AuthorID != "" {
			responsibility[path.AuthorID] += e.weights.Author
		}

		// Reviewer weight is split equally among all reviewers.
		if len(path.ReviewerIDs) > 0 {
			reviewerShare := e.weights.Reviewers / float64(len(path.ReviewerIDs))
			for _, rid := range path.ReviewerIDs {
				if rid != "" {
					responsibility[rid] += reviewerShare
				}
			}
		}

		// Approver gets the approver weight.
		if path.ApproverID != "" {
			responsibility[path.ApproverID] += e.weights.Approver
		}
	}

	// Normalize so total sums to 1.0.
	total := 0.0
	for _, w := range responsibility {
		total += w
	}
	if total > 0 {
		for k := range responsibility {
			responsibility[k] /= total
		}
	}

	result := &AttributionResult{
		IncidentID:     incidentID,
		Responsibility: responsibility,
		ChangePaths:    paths,
		EvidenceLinks:  evidence,
		Timestamp:      time.Now().UTC(),
	}

	return result, nil
}

// TrustPenalty computes the trust score penalty for an agent based on their
// attribution share and the incident severity.
//
// Severity multipliers:
//   low=0.05, medium=0.10, high=0.20, critical=0.40
//
// The penalty = attributionShare × severityMultiplier.
func TrustPenalty(attributionShare float64, severity string) float64 {
	severityMult := severityMultiplier(severity)
	return attributionShare * severityMult
}

// severityMultiplier maps incident severity to a trust penalty multiplier.
func severityMultiplier(severity string) float64 {
	switch severity {
	case SeverityLow:
		return 0.05
	case SeverityMedium:
		return 0.10
	case SeverityHigh:
		return 0.20
	case SeverityCritical:
		return 0.40
	default:
		return 0.10 // default to medium
	}
}

// AttributionSummary is a human-readable summary of responsibility.
type AttributionSummary struct {
	AgentID      string  `json:"agent_id"`
	Responsibility float64 `json:"responsibility"`
	TrustPenalty float64 `json:"trust_penalty"`
}

// Summarize produces per-agent summaries from an attribution result.
func (r *AttributionResult) Summarize(severity string) []AttributionSummary {
	var summaries []AttributionSummary
	for agentID, share := range r.Responsibility {
		summaries = append(summaries, AttributionSummary{
			AgentID:        agentID,
			Responsibility: share,
			TrustPenalty:   TrustPenalty(share, severity),
		})
	}
	return summaries
}

// PrimaryResponsible returns the agent with the highest responsibility share.
// In case of a tie, returns the first one encountered (map iteration order).
func (r *AttributionResult) PrimaryResponsible() string {
	var topAgent string
	var topShare float64
	for agentID, share := range r.Responsibility {
		if share > topShare {
			topShare = share
			topAgent = agentID
		}
	}
	return topAgent
}

// TotalResponsibility returns the sum of all responsibility shares.
// For a properly normalized result, this should be ~1.0.
func (r *AttributionResult) TotalResponsibility() float64 {
	total := 0.0
	for _, share := range r.Responsibility {
		total += share
	}
	return total
}

// TrustPenaltyCallback is called with the computed trust penalty for each agent.
// This is the integration point with pkg/trust — the callback feeds the penalty
// into the trust scoring engine.
type TrustPenaltyCallback func(agentID string, penalty float64, evidence []string) error

// ApplyTrustPenalties calls the callback for each responsible agent.
func (e *AttributionEngine) ApplyTrustPenalties(result *AttributionResult, severity string, cb TrustPenaltyCallback) error {
	for agentID, share := range result.Responsibility {
		penalty := TrustPenalty(share, severity)
		if err := cb(agentID, penalty, result.EvidenceLinks); err != nil {
			return fmt.Errorf("trust penalty for agent %s: %w", agentID, err)
		}
	}
	return nil
}

// FindResponsiblePaths filters change paths to those matching the incident's
// causal chain. A path is considered "responsible" if its file appears in the
// causal chain.
func FindResponsiblePaths(paths []ChangePath, causalChain []string) []ChangePath {
	causalSet := make(map[string]bool)
	for _, f := range causalChain {
		causalSet[f] = true
	}

	var matched []ChangePath
	for _, p := range paths {
		if causalSet[p.FilePath] {
			matched = append(matched, p)
		}
	}
	return matched
}

// MergeAttribution combines results from multiple incidents into a single
// aggregate responsibility map. This is used when an agent is responsible
// for multiple incidents — their penalties accumulate.
func MergeAttribution(results []*AttributionResult) map[string]float64 {
	merged := make(map[string]float64)
	for _, r := range results {
		for agentID, share := range r.Responsibility {
			merged[agentID] += share
		}
	}
	return merged
}
