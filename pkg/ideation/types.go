// Package ideation implements Helix offline idea capture, validation,
// prioritization, and promotion (Phase 1 §1.1–1.3).
package ideation

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// Idea source constants.
const (
	SourceHuman   = "human"
	SourceAgent   = "agent"
	SourceChimera = "chimera"
)

// Idea status constants.
const (
	StatusDraft       = "draft"
	StatusValidated   = "validated"
	StatusPrioritized = "prioritized"
	StatusPromoted    = "promoted"
	StatusClosed      = "closed"
)

// Evidence type constants.
const (
	EvidenceIncident    = "incident"
	EvidenceCodePattern = "code_pattern"
	EvidenceMarketTrend = "market_trend"
	EvidenceFile        = "file"
)

// EvidenceRef links supporting material to an idea.
type EvidenceRef struct {
	Type        string `json:"type"`
	Ref         string `json:"ref"`
	Description string `json:"description,omitempty"`
}

// Idea is a captured product/engineering idea.
type Idea struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Body         string        `json:"body"`
	Tags         []string      `json:"tags,omitempty"`
	Source       string        `json:"source"` // IdeaSource*
	SourceAgent  string        `json:"source_agent,omitempty"`
	SourceModel  string        `json:"source_model,omitempty"`
	Evidence     []EvidenceRef `json:"evidence,omitempty"`
	Status       string        `json:"status"`
	RiskScore    float64       `json:"risk_score,omitempty"` // 0-100 after validate
	CostTotal    float64       `json:"cost_total,omitempty"` // USD estimate after prioritize
	Score        float64       `json:"score,omitempty"`      // composite priority score
	PromotedTo   string        `json:"promoted_to,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
	ClosedReason string        `json:"closed_reason,omitempty"`
}

// NewIdeaID returns a random hex ID (8–16 bytes → 16–32 hex chars).
func NewIdeaID() string {
	// 12 bytes → 24 hex chars (within 8–16 byte range).
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		// Extremely unlikely; fall back to timestamp-based ID.
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// ValidSource reports whether s is a known idea source.
func ValidSource(s string) bool {
	switch s {
	case SourceHuman, SourceAgent, SourceChimera:
		return true
	default:
		return false
	}
}

// ValidStatus reports whether s is a known idea status.
func ValidStatus(s string) bool {
	switch s {
	case StatusDraft, StatusValidated, StatusPrioritized, StatusPromoted, StatusClosed:
		return true
	default:
		return false
	}
}
