package spec

import (
	"fmt"
	"strings"
	"time"
)

// completenessDimensions defines the 12 scoring dimensions and the keyword
// sets used to score each. A dimension scores higher when more of its keywords
// appear in the spec text.
var completenessDimensions = []struct {
	Name        string
	DedicatedKW []string // keywords that, if a section is named after them, auto-score 80+
	Keywords    []string // keyword hits; each unique keyword contributes score
}{
	{
		Name:        "requirements_coverage",
		DedicatedKW: []string{"requirements", "acceptance criteria"},
		Keywords:    []string{"must", "shall", "should", "requirement", "acceptance", "criteria", "specification", "functional"},
	},
	{
		Name:        "error_states",
		DedicatedKW: []string{"error", "errors", "failure modes"},
		Keywords:    []string{"error", "failure", "fallback", "retry", "recover", "exception", "timeout", "invalid"},
	},
	{
		Name:        "security",
		DedicatedKW: []string{"security", "threat model"},
		Keywords:    []string{"security", "encrypt", "auth", "secret", "vulnerability", "sanitize", "threat", "privilege", "rbac"},
	},
	{
		Name:        "authentication",
		DedicatedKW: []string{"authentication", "auth"},
		Keywords:    []string{"auth", "login", "token", "session", "oauth", "jwt", "credential", "password", "mfa"},
	},
	{
		Name:        "rate_limiting",
		DedicatedKW: []string{"rate limit", "rate limiting", "throttling"},
		Keywords:    []string{"rate", "throttle", "quota", "429", "limit", "backoff", "burst"},
	},
	{
		Name:        "data_validation",
		DedicatedKW: []string{"data validation", "validation", "input validation"},
		Keywords:    []string{"validation", "sanitize", "schema", "type check", "parse", "input", "boundary", "reject"},
	},
	{
		Name:        "observability",
		DedicatedKW: []string{"observability", "logging", "telemetry"},
		Keywords:    []string{"log", "metric", "trace", "observability", "telemetry", "structured log", "audit"},
	},
	{
		Name:        "testing",
		DedicatedKW: []string{"testing", "test strategy"},
		Keywords:    []string{"test", "unit test", "integration test", "coverage", "tdd", "fixture", "mock", "e2e"},
	},
	{
		Name:        "deployment",
		DedicatedKW: []string{"deployment", "deploy", "release"},
		Keywords:    []string{"deploy", "release", "ci/cd", "pipeline", "docker", "kubernetes", "helm", "canary", "blue-green"},
	},
	{
		Name:        "rollback",
		DedicatedKW: []string{"rollback", "roll back"},
		Keywords:    []string{"rollback", "revert", "undo", "previous version", "restore", "migrate back"},
	},
	{
		Name:        "monitoring",
		DedicatedKW: []string{"monitoring", "alerts", "alerting"},
		Keywords:    []string{"alert", "monitor", "dashboard", "health check", "slo", "sla", "pagerduty", "grafana", "prometheus"},
	},
	{
		Name:        "documentation",
		DedicatedKW: []string{"documentation", "docs", "readme"},
		Keywords:    []string{"readme", "documentation", "api doc", "comment", "example", "guide", "reference"},
	},
}

// SpecCompleteness scores a spec across 12 dimensions.
type SpecCompleteness struct{}

// NewSpecCompleteness returns a completeness checker.
func NewSpecCompleteness() *SpecCompleteness { return &SpecCompleteness{} }

// CheckCompleteness scores the spec across all 12 dimensions.
func (c *SpecCompleteness) CheckCompleteness(spec *Spec) (*CompletenessReport, error) {
	if spec == nil {
		return nil, fmt.Errorf("spec: spec is nil")
	}
	if spec.ID == "" {
		return nil, fmt.Errorf("spec: id is required")
	}

	fullText := concatSectionText(spec)
	fullLower := strings.ToLower(fullText)

	sectionTitlesLower := make([]string, 0, len(spec.Sections))
	for _, sec := range spec.Sections {
		sectionTitlesLower = append(sectionTitlesLower, strings.ToLower(sec.Title))
	}

	var dimensions []DimensionScore
	var gaps []CompletenessGap

	for _, dim := range completenessDimensions {
		score, note := scoreDimension(dim, fullLower, sectionTitlesLower)
		dimensions = append(dimensions, DimensionScore{
			Name:     dim.Name,
			Score:    score,
			MaxScore: 100.0,
			Note:     note,
		})
		if score < 50 {
			gaps = append(gaps, CompletenessGap{
				Dimension: dim.Name,
				Detail:    fmt.Sprintf("%s score is %.0f/100 — below the 50-point threshold", dim.Name, score),
				Severity:  severityForScore(score),
			})
		}
	}

	totalScore := 0.0
	for _, d := range dimensions {
		totalScore += d.Score
	}
	if len(dimensions) > 0 {
		totalScore /= float64(len(dimensions))
	}

	return &CompletenessReport{
		SpecID:      spec.ID,
		TotalScore:  roundTo(totalScore, 1),
		Dimensions:  dimensions,
		Gaps:        gaps,
		GeneratedAt: time.Now().UTC(),
	}, nil
}

func scoreDimension(dim struct {
	Name        string
	DedicatedKW []string
	Keywords    []string
}, fullLower string, sectionTitlesLower []string) (float64, string) {
	// Check if a dedicated section exists.
	for _, title := range sectionTitlesLower {
		for _, kw := range dim.DedicatedKW {
			if strings.Contains(title, kw) {
				return 80.0, fmt.Sprintf("dedicated section detected (%.0f baseline)", 80.0)
			}
		}
	}

	// Otherwise score by keyword hit density.
	hits := 0
	for _, kw := range dim.Keywords {
		if strings.Contains(fullLower, kw) {
			hits++
		}
	}

	maxKW := len(dim.Keywords)
	if maxKW == 0 {
		return 0, "no keywords defined for dimension"
	}

	// Score = min(hits/ceil(maxKW*0.4) * 70, 70) — partial coverage maxes at 70
	// without a dedicated section.
	threshold := maxKW * 4 / 10
	if threshold < 1 {
		threshold = 1
	}
	score := float64(hits) / float64(threshold) * 70.0
	if score > 70 {
		score = 70
	}
	note := fmt.Sprintf("%d/%d keywords matched", hits, maxKW)
	return roundTo(score, 1), note
}

func severityForScore(score float64) string {
	if score < 20 {
		return SeverityCritical
	}
	if score < 40 {
		return SeverityWarning
	}
	return SeverityInfo
}

func concatSectionText(spec *Spec) string {
	var b strings.Builder
	for _, sec := range spec.Sections {
		b.WriteString(sec.Title)
		b.WriteString("\n")
		b.WriteString(sec.Content)
		b.WriteString("\n")
	}
	return b.String()
}

func roundTo(v float64, places int) float64 {
	mult := 1.0
	for i := 0; i < places; i++ {
		mult *= 10
	}
	return float64(int(v*mult+0.5)) / mult
}
