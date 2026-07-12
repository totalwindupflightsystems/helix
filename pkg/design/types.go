// Package design implements automated design review via adversarial agents
// (Phase 2 §2.3). A DesignReviewDispatcher dispatches prosecutor agents
// (@assumption-buster, @redteam, @cost-auditor, @chaos-engineer,
// @consistency-checker) against a spec + ADR design surface and returns a
// Change Management View with risk, threat map, cost projection, and
// PASS/WARN/FAIL consensus.
package design

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/estimate"
	"github.com/totalwindupflightsystems/helix/pkg/review"
)

// DesignAspect classifies a design finding for the Change Management View.
type DesignAspect string

const (
	// AspectAssumption — implicit assumption challenged by @assumption-buster.
	AspectAssumption DesignAspect = "assumption"
	// AspectThreat — attack vector / threat-surface item from @redteam.
	AspectThreat DesignAspect = "threat"
	// AspectCost — budget / token-cost issue from @cost-auditor.
	AspectCost DesignAspect = "cost"
	// AspectCompleteness — missing design element.
	AspectCompleteness DesignAspect = "completeness"
	// AspectConsistency — spec↔ADR↔contract contradiction from @consistency-checker.
	AspectConsistency DesignAspect = "consistency"
)

// Consensus verdict constants for the Change Management View.
const (
	VerdictPASS = "PASS"
	VerdictWARN = "WARN"
	VerdictFAIL = "FAIL"
)

// DesignContext carries capacity / budget constraints used during review.
type DesignContext struct {
	TeamCapacity    float64 `json:"team_capacity,omitempty"`    // person-weeks available
	BudgetRemaining float64 `json:"budget_remaining,omitempty"` // USD remaining
	TimelineDays    int     `json:"timeline_days,omitempty"`    // calendar days to ship
	Notes           string  `json:"notes,omitempty"`
}

// DesignReviewRequest is the input for design review.
// SpecText / ADRTexts are populated by the CLI (or API) after loading stores;
// agents consume the combined design document built from these fields.
type DesignReviewRequest struct {
	SpecRef        string          `json:"spec_ref"`
	ADRRefs        []string        `json:"adr_refs,omitempty"`
	ContractSchema json.RawMessage `json:"contract_schema,omitempty"`
	Context        DesignContext   `json:"context"`

	// SpecText is the full rendered spec body used by offline agents.
	SpecText string `json:"-"`
	// SpecTitle is the human-readable title (optional).
	SpecTitle string `json:"-"`
	// ADRTexts are rendered ADR bodies aligned with ADRRefs.
	ADRTexts []string `json:"-"`
	// ChangedHints are optional path-like hints that drive agent triggers
	// (e.g. "auth", "crypto", "retry"). When empty they are inferred from text.
	ChangedHints []string `json:"-"`
	// Category overrides automatic category inference when non-empty.
	Category review.ChangeCategory `json:"-"`
}

// DesignFinding extends review.Finding with design-specific fields.
type DesignFinding struct {
	review.Finding
	// ID uniquely identifies the finding for --fix targeting.
	ID string `json:"id,omitempty"`
	// DesignAspect classifies the finding for Change Management sections.
	DesignAspect DesignAspect `json:"design_aspect"`
	// AffectedComponent is the service / module implicated.
	AffectedComponent string `json:"affected_component,omitempty"`
	// DesignLine is a line reference into the design document (spec/ADR).
	DesignLine int `json:"design_line,omitempty"`
	// AgentType is the prosecutor that produced the finding.
	AgentType review.AgentType `json:"agent_type,omitempty"`
	// RiskLevel is low|medium|high (for assumption ranking).
	RiskLevel string `json:"risk_level,omitempty"`
}

// ThreatService is a service node in the threat map.
type ThreatService struct {
	Name       string   `json:"name"`
	Components []string `json:"components,omitempty"`
	RiskScore  float64  `json:"risk_score"`
}

// DataFlow is a data movement edge between services.
type DataFlow struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Description string `json:"description,omitempty"`
	Sensitive   bool   `json:"sensitive,omitempty"`
}

// TrustBoundary is a trust domain boundary in the design.
type TrustBoundary struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Services    []string `json:"services,omitempty"`
}

// AttackVector is a concrete threat mapped by @redteam.
type AttackVector struct {
	Description     string `json:"description"`
	Severity        string `json:"severity"`
	AffectedService string `json:"affected_service,omitempty"`
	Mitigation      string `json:"mitigation,omitempty"`
}

// ThreatMap visualizes services, data flows, trust boundaries, and attacks.
type ThreatMap struct {
	Services        []ThreatService `json:"services,omitempty"`
	DataFlows       []DataFlow      `json:"data_flows,omitempty"`
	TrustBoundaries []TrustBoundary `json:"trust_boundaries,omitempty"`
	AttackVectors   []AttackVector  `json:"attack_vectors,omitempty"`
}

// ConsensusResult is the multi-agent consensus for a design review.
// (review.Consensus uses approved/blocked resolution; design uses PASS/WARN/FAIL.)
type ConsensusResult struct {
	Verdict    string  `json:"verdict"` // PASS | WARN | FAIL
	Score      float64 `json:"score"`   // 0–1 agreement / cleanliness
	AgentsRun  int     `json:"agents_run"`
	CleanBill  bool    `json:"clean_bill"`
	Summary    string  `json:"summary"`
	Resolution string  `json:"resolution,omitempty"` // optional link to review.Consensus
}

// DesignReviewReport is the aggregate Change Management View.
type DesignReviewReport struct {
	SpecRef         string                `json:"spec_ref"`
	ADRRefs         []string              `json:"adr_refs,omitempty"`
	Findings        []DesignFinding       `json:"findings"`
	RiskScore       float64               `json:"risk_score"` // 0–100
	ThreatSurface   ThreatMap             `json:"threat_surface"`
	CostProjection  estimate.CostEstimate `json:"cost_projection"`
	Consensus       ConsensusResult       `json:"consensus"`
	AgentsRun       []string              `json:"agents_run,omitempty"`
	DispatchSummary string                `json:"dispatch_summary,omitempty"`
	ReviewedAt      time.Time             `json:"reviewed_at"`
}

// FindRiskLevel classifies a severity string into low|medium|high for ranking.
func FindRiskLevel(severity string) string {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical", "high", "error":
		return "high"
	case "medium", "warning", "warn":
		return "medium"
	case "low", "info", "note":
		return "low"
	case "":
		return "low"
	default:
		return "medium"
	}
}

// ValidDesignAspect reports whether a is a known design aspect.
func ValidDesignAspect(a DesignAspect) bool {
	switch a {
	case AspectAssumption, AspectThreat, AspectCost, AspectCompleteness, AspectConsistency:
		return true
	default:
		return false
	}
}
