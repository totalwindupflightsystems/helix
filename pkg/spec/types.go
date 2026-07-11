// Package spec implements Helix spec co-authoring with adversarial
// annotation (Phase 2 §2.1). A SpecCoAuthor dispatches two deterministic
// rule-based agent personas — @spec-generator and @spec-challenger — that
// annotate a spec with edge cases, failure modes, consistency issues, and
// missing coverage. A SpecCompleteness scores the spec across 12 dimensions.
package spec

import "time"

// Spec status constants.
const (
	StatusDraft    = "draft"
	StatusInReview = "in_review"
	StatusApproved = "approved"
	StatusFrozen   = "frozen"
)

// Annotation type constants.
const (
	AnnEdgeCase       = "edge_case"
	AnnFailureMode    = "failure_mode"
	AnnConsistency    = "consistency"
	AnnIncompleteness = "incompleteness"
	AnnSecurity       = "security"
)

// Section approval status constants.
const (
	ApprovalPending  = "pending"
	ApprovalApproved = "approved"
	ApprovalRejected = "rejected"
)

// Annotation status constants.
const (
	AnnotationProposed = "proposed"
	AnnotationAccepted = "accepted"
	AnnotationRejected = "rejected"
)

// Severity constants.
const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

// Agent persona identifiers.
const (
	AgentSpecGenerator  = "spec-generator"
	AgentSpecChallenger = "spec-challenger"
)

// Spec stores a specification document.
type Spec struct {
	ID           string           `json:"id"`
	IdeaRef      string           `json:"idea_ref"`
	Title        string           `json:"title"`
	Sections     []SpecSection    `json:"sections"`
	Status       string           `json:"status"`
	Annotations  []SpecAnnotation `json:"annotations,omitempty"`
	ADRRefs      []string         `json:"adr_refs,omitempty"`
	ContractRefs []string         `json:"contract_refs,omitempty"`
	Hash         string           `json:"hash,omitempty"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
}

// SpecSection is a titled block within a spec.
type SpecSection struct {
	Title          string    `json:"title"`
	Content        string    `json:"content"`
	ApprovalStatus string    `json:"approval_status"`
	ApprovedBy     string    `json:"approved_by,omitempty"`
	ApprovedAt     time.Time `json:"approved_at,omitempty"`
}

// SpecAnnotation is a co-author annotation on a spec.
type SpecAnnotation struct {
	Line           int       `json:"line"`
	AgentType      string    `json:"agent_type"`
	AnnotationType string    `json:"annotation_type"`
	Content        string    `json:"content"`
	Severity       string    `json:"severity"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

// CompletenessReport scores a spec across 12 dimensions.
type CompletenessReport struct {
	SpecID      string            `json:"spec_id"`
	TotalScore  float64           `json:"total_score"`
	Dimensions  []DimensionScore  `json:"dimensions"`
	Gaps        []CompletenessGap `json:"gaps"`
	GeneratedAt time.Time         `json:"generated_at"`
}

// DimensionScore is one dimension in a CompletenessReport.
type DimensionScore struct {
	Name     string  `json:"name"`
	Score    float64 `json:"score"`
	MaxScore float64 `json:"max_score"`
	Note     string  `json:"note,omitempty"`
}

// CompletenessGap describes a missing or weak area in a dimension.
type CompletenessGap struct {
	Dimension string `json:"dimension"`
	Line      int    `json:"line,omitempty"`
	Detail    string `json:"detail"`
	Severity  string `json:"severity"`
}
