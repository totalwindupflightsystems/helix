package ideation

import (
	"fmt"
	"strings"
)

// ValidationFinding is one offline agent finding.
type ValidationFinding struct {
	AgentType      string        `json:"agent_type"`
	Severity       string        `json:"severity"` // info|low|medium|high|critical
	Description    string        `json:"description"`
	Evidence       []EvidenceRef `json:"evidence,omitempty"`
	Recommendation string        `json:"recommendation,omitempty"`
}

// ValidationReport is the aggregate offline validation result.
type ValidationReport struct {
	IdeaID    string              `json:"idea_id"`
	Verdict   string              `json:"verdict"`    // pass|fail|needs_clarification
	RiskScore float64             `json:"risk_score"` // 0-100
	Findings  []ValidationFinding `json:"findings"`
	AgentsRun []string            `json:"agents_run"`
}

// Verdict constants.
const (
	VerdictPass               = "pass"
	VerdictFail               = "fail"
	VerdictNeedsClarification = "needs_clarification"
)

// Severity constants.
const (
	SeverityInfo     = "info"
	SeverityLow      = "low"
	SeverityMedium   = "medium"
	SeverityHigh     = "high"
	SeverityCritical = "critical"
)

// Agent type names (offline concept agents).
const (
	AgentAssumptionBuster = "@assumption-buster"
	AgentArchitectureFit  = "@architecture-fit"
)

// IdeaValidator runs offline deterministic concept agents.
type IdeaValidator struct{}

// NewIdeaValidator returns an offline idea validator.
func NewIdeaValidator() *IdeaValidator {
	return &IdeaValidator{}
}

// Validate runs offline agents and produces a risk score + verdict.
// No network calls — fully deterministic for a given idea.
func (v *IdeaValidator) Validate(idea *Idea) (*ValidationReport, error) {
	if idea == nil {
		return nil, fmt.Errorf("ideation: idea is nil")
	}
	if idea.ID == "" {
		return nil, fmt.Errorf("ideation: idea id is required")
	}

	var findings []ValidationFinding
	agentsRun := []string{AgentAssumptionBuster, AgentArchitectureFit}

	findings = append(findings, runAssumptionBuster(idea)...)
	findings = append(findings, runArchitectureFit(idea)...)

	risk := computeRiskScore(findings)
	verdict := verdictFromRisk(risk, findings)

	return &ValidationReport{
		IdeaID:    idea.ID,
		Verdict:   verdict,
		RiskScore: risk,
		Findings:  findings,
		AgentsRun: agentsRun,
	}, nil
}

func runAssumptionBuster(idea *Idea) []ValidationFinding {
	var out []ValidationFinding
	body := strings.TrimSpace(idea.Body)
	title := strings.TrimSpace(idea.Title)

	if len(body) < 20 {
		out = append(out, ValidationFinding{
			AgentType:      AgentAssumptionBuster,
			Severity:       SeverityMedium,
			Description:    "Body is too short (<20 chars); idea lacks actionable detail",
			Recommendation: "Expand the body with problem statement, approach, and success criteria",
		})
	}
	if len(title) < 5 {
		out = append(out, ValidationFinding{
			AgentType:      AgentAssumptionBuster,
			Severity:       SeverityLow,
			Description:    "Title is vague or too short",
			Recommendation: "Use a specific, outcome-oriented title",
		})
	}
	if len(idea.Tags) == 0 {
		out = append(out, ValidationFinding{
			AgentType:      AgentAssumptionBuster,
			Severity:       SeverityLow,
			Description:    "Missing tags; harder to route and prioritize",
			Recommendation: "Add domain tags (e.g. auth, performance, ux)",
		})
	}
	if len(idea.Evidence) == 0 {
		out = append(out, ValidationFinding{
			AgentType:      AgentAssumptionBuster,
			Severity:       SeverityMedium,
			Description:    "Empty evidence; claims are unbacked",
			Recommendation: "Attach incident, code_pattern, market_trend, or file evidence",
		})
	}

	buzzwords := []string{"simply", "just", "easily", "trivial", "obviously"}
	lowerBody := strings.ToLower(body + " " + title)
	for _, bw := range buzzwords {
		if strings.Contains(lowerBody, bw) {
			out = append(out, ValidationFinding{
				AgentType:      AgentAssumptionBuster,
				Severity:       SeverityInfo,
				Description:    fmt.Sprintf("Buzzword %q masks complexity assumptions", bw),
				Recommendation: "Replace hand-waving with concrete steps and constraints",
			})
			// One buzzword finding is enough signal.
			break
		}
	}

	if len(out) == 0 {
		out = append(out, ValidationFinding{
			AgentType:   AgentAssumptionBuster,
			Severity:    SeverityInfo,
			Description: "No major assumption issues detected",
		})
	}
	return out
}

func runArchitectureFit(idea *Idea) []ValidationFinding {
	var out []ValidationFinding
	tagsLower := make([]string, 0, len(idea.Tags))
	for _, t := range idea.Tags {
		tagsLower = append(tagsLower, strings.ToLower(t))
	}
	sensitive := []string{"auth", "security", "crypto", "iac", "payment"}
	for _, tag := range tagsLower {
		for _, s := range sensitive {
			if tag == s || strings.Contains(tag, s) {
				out = append(out, ValidationFinding{
					AgentType:      AgentArchitectureFit,
					Severity:       SeverityMedium,
					Description:    fmt.Sprintf("Tag %q indicates elevated architectural risk domain", tag),
					Recommendation: "Require security review, threat model, and staged rollout plan",
				})
				goto afterSensitive
			}
		}
	}
afterSensitive:

	lowerBody := strings.ToLower(idea.Body + " " + idea.Title)
	if strings.Contains(lowerBody, "rewrite everything") || strings.Contains(lowerBody, "big bang") {
		out = append(out, ValidationFinding{
			AgentType:      AgentArchitectureFit,
			Severity:       SeverityHigh,
			Description:    "Body mentions rewrite-everything / big-bang migration language",
			Recommendation: "Prefer incremental strangler-fig migration with measurable checkpoints",
		})
	}

	if len(out) == 0 {
		out = append(out, ValidationFinding{
			AgentType:   AgentArchitectureFit,
			Severity:    SeverityInfo,
			Description: "No elevated architecture-fit risks detected",
		})
	}
	return out
}

func computeRiskScore(findings []ValidationFinding) float64 {
	// base 20 + points for findings, clamp 0–100
	score := 20.0
	for _, f := range findings {
		switch strings.ToLower(f.Severity) {
		case SeverityInfo:
			score += 5
		case SeverityLow:
			score += 10
		case SeverityMedium:
			score += 15
		case SeverityHigh:
			score += 25
		case SeverityCritical:
			score += 40
		}
	}
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score
}

func verdictFromRisk(risk float64, findings []ValidationFinding) string {
	hasCritical := false
	hasHigh := false
	for _, f := range findings {
		switch strings.ToLower(f.Severity) {
		case SeverityCritical:
			hasCritical = true
		case SeverityHigh:
			hasHigh = true
		}
	}
	// fail if RiskScore>=70 or any critical
	if risk >= 70 || hasCritical {
		return VerdictFail
	}
	// needs_clarification if RiskScore>=40 or any high
	if risk >= 40 || hasHigh {
		return VerdictNeedsClarification
	}
	return VerdictPass
}
