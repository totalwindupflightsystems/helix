package design

import (
	"fmt"
	"sort"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/estimate"
	"github.com/totalwindupflightsystems/helix/pkg/review"
)

// ---------------------------------------------------------------------------
// Aggregation
// ---------------------------------------------------------------------------

func mergeFindings(d *review.DispatchReport) []DesignFinding {
	if d == nil {
		return nil
	}
	var out []DesignFinding
	n := 0
	for _, res := range d.Results {
		aspectDefault := aspectForAgent(res.AgentType)
		// Map structured agent outputs into design findings when Findings empty.
		if len(res.Findings) == 0 {
			for _, a := range res.AssumptionsFound {
				n++
				out = append(out, DesignFinding{
					Finding: review.Finding{
						Model:       string(res.AgentType),
						Severity:    a.RiskLevel,
						Type:        string(AspectAssumption),
						File:        a.File,
						Line:        a.Line,
						Description: a.Description,
						Evidence:    a.Challenge,
					},
					ID:           fmt.Sprintf("DF-%03d", n),
					DesignAspect: AspectAssumption,
					DesignLine:   a.Line,
					AgentType:    res.AgentType,
					RiskLevel:    FindRiskLevel(a.RiskLevel),
				})
			}
			for _, e := range res.ExploitPaths {
				n++
				out = append(out, DesignFinding{
					Finding: review.Finding{
						Model:       string(res.AgentType),
						Severity:    e.Severity,
						Type:        string(AspectThreat),
						File:        e.File,
						Line:        e.Line,
						Description: e.Description,
						Evidence:    e.Evidence,
					},
					ID:                fmt.Sprintf("DF-%03d", n),
					DesignAspect:      AspectThreat,
					AffectedComponent: e.File,
					DesignLine:        e.Line,
					AgentType:         res.AgentType,
					RiskLevel:         FindRiskLevel(e.Severity),
				})
			}
			continue
		}
		for _, f := range res.Findings {
			n++
			aspect := DesignAspect(f.Type)
			if !ValidDesignAspect(aspect) {
				aspect = aspectDefault
			}
			out = append(out, DesignFinding{
				Finding:           f,
				ID:                fmt.Sprintf("DF-%03d", n),
				DesignAspect:      aspect,
				AffectedComponent: f.File,
				DesignLine:        f.Line,
				AgentType:         res.AgentType,
				RiskLevel:         FindRiskLevel(f.Severity),
			})
		}
	}
	// Stable order: high risk first, then by ID.
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := riskRank(out[i].RiskLevel), riskRank(out[j].RiskLevel)
		if ri != rj {
			return ri > rj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func aspectForAgent(at review.AgentType) DesignAspect {
	switch at {
	case review.AgentAssumptionBuster:
		return AspectAssumption
	case review.AgentRedTeam:
		return AspectThreat
	case review.AgentCostAuditor:
		return AspectCost
	case review.AgentChaosEngineer:
		return AspectCompleteness
	case AgentConsistencyChecker:
		return AspectConsistency
	default:
		return AspectCompleteness
	}
}

func riskRank(level string) int {
	switch FindRiskLevel(level) {
	case "high":
		return 3
	case "medium":
		return 2
	default:
		return 1
	}
}

func buildThreatMap(d *review.DispatchReport, doc string) ThreatMap {
	tm := ThreatMap{}
	svcSeen := map[string]bool{}
	addSvc := func(name string, risk float64) {
		if name == "" {
			return
		}
		if !svcSeen[name] {
			svcSeen[name] = true
			tm.Services = append(tm.Services, ThreatService{Name: name, RiskScore: risk})
		}
	}

	// Infer services from path-like tokens in exploits / findings.
	if d != nil {
		for _, res := range d.Results {
			for _, e := range res.ExploitPaths {
				svc := serviceFromPath(e.File)
				addSvc(svc, severityToScore(e.Severity))
				mit := firstNonEmpty(findingMitigation(d, e.Description), e.Evidence, "see redteam findings")
				tm.AttackVectors = append(tm.AttackVectors, AttackVector{
					Description:     e.Description,
					Severity:        e.Severity,
					AffectedService: svc,
					Mitigation:      mit,
				})
			}
			for _, f := range res.Findings {
				if f.Type == string(AspectThreat) || res.AgentType == review.AgentRedTeam {
					svc := serviceFromPath(f.File)
					addSvc(svc, severityToScore(f.Severity))
					// Avoid duplicate attack vectors when exploit paths already added.
					if len(res.ExploitPaths) == 0 {
						tm.AttackVectors = append(tm.AttackVectors, AttackVector{
							Description:     f.Description,
							Severity:        f.Severity,
							AffectedService: svc,
							Mitigation:      f.Mitigation,
						})
					}
				}
			}
		}
	}

	lower := strings.ToLower(doc)
	if strings.Contains(lower, "api") {
		addSvc("api-gateway", 40)
		tm.DataFlows = append(tm.DataFlows, DataFlow{
			From: "client", To: "api-gateway", Description: "HTTPS requests", Sensitive: true,
		})
		tm.TrustBoundaries = append(tm.TrustBoundaries, TrustBoundary{
			Name: "public-edge", Description: "Internet → API gateway", Services: []string{"api-gateway"},
		})
	}
	if strings.Contains(lower, "auth") {
		addSvc("auth-service", 55)
		tm.DataFlows = append(tm.DataFlows, DataFlow{
			From: "api-gateway", To: "auth-service", Description: "token validation", Sensitive: true,
		})
		tm.TrustBoundaries = append(tm.TrustBoundaries, TrustBoundary{
			Name: "auth-boundary", Description: "Authenticated vs anonymous", Services: []string{"auth-service"},
		})
	}
	if len(tm.Services) == 0 {
		addSvc("design-surface", 20)
	}
	return tm
}

func serviceFromPath(path string) string {
	if path == "" || path == "design" {
		return "design-surface"
	}
	parts := strings.Split(path, "/")
	for _, p := range parts {
		if p == "pkg" || p == "internal" || p == "cmd" {
			continue
		}
		if p != "" {
			return p + "-service"
		}
	}
	return path
}

func severityToScore(sev string) float64 {
	switch FindRiskLevel(sev) {
	case "high":
		return 80
	case "medium":
		return 50
	default:
		return 20
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// findingMitigation looks up a matching finding mitigation for an exploit description.
func findingMitigation(d *review.DispatchReport, desc string) string {
	if d == nil || desc == "" {
		return ""
	}
	for _, res := range d.Results {
		for _, f := range res.Findings {
			if f.Description == desc && strings.TrimSpace(f.Mitigation) != "" {
				return f.Mitigation
			}
		}
	}
	return ""
}

func extractCostProjection(d *review.DispatchReport, req *DesignReviewRequest) estimate.CostEstimate {
	var ce estimate.CostEstimate
	if d != nil {
		for _, res := range d.Results {
			if res.CostEstimate != nil {
				c := res.CostEstimate
				ce = estimate.CostEstimate{
					CostInput:  c.EstimatedCostUSD * 0.6,
					CostOutput: c.EstimatedCostUSD * 0.4,
					CostTotal:  c.EstimatedCostUSD,
					Tokens: estimate.TokenEstimate{
						TotalInput: c.EstimatedTokens * 2 / 3,
						Output:     c.EstimatedTokens / 3,
					},
					Model:    "design-heuristic",
					Provider: "offline",
				}
				break
			}
		}
	}
	if ce.CostTotal == 0 {
		// Fallback minimal projection.
		ce = estimate.CostEstimate{
			CostTotal: 0.01,
			Model:     "design-heuristic",
			Provider:  "offline",
		}
	}
	_ = req
	return ce
}

func agentsRunNames(d *review.DispatchReport) []string {
	if d == nil {
		return nil
	}
	names := make([]string, 0, len(d.Results))
	for _, res := range d.Results {
		names = append(names, "@"+string(res.AgentType))
	}
	sort.Strings(names)
	return names
}

func computeDesignConsensus(d *review.DispatchReport) ConsensusResult {
	if d == nil {
		return ConsensusResult{Verdict: VerdictFAIL, Summary: "no dispatch report"}
	}
	clean := 0
	suspicious := 0
	exploited := 0
	for _, res := range d.Results {
		switch res.Verdict {
		case "clean":
			clean++
		case "exploited":
			exploited++
		default:
			suspicious++
		}
	}
	total := len(d.Results)
	if total == 0 {
		return ConsensusResult{Verdict: VerdictFAIL, Summary: "no agents ran", AgentsRun: 0}
	}
	score := float64(clean) / float64(total)
	verdict := VerdictPASS
	if exploited > 0 {
		verdict = VerdictFAIL
	} else if suspicious > 0 || !d.CleanBill {
		verdict = VerdictWARN
	}
	return ConsensusResult{
		Verdict:   verdict,
		Score:     score,
		AgentsRun: d.AgentsRun,
		CleanBill: d.CleanBill,
		Summary:   fmt.Sprintf("clean=%d suspicious=%d exploited=%d score=%.2f", clean, suspicious, exploited, score),
	}
}

func refineConsensus(c ConsensusResult, risk float64, findings []DesignFinding) ConsensusResult {
	hasCritical := false
	for _, f := range findings {
		if strings.EqualFold(f.Severity, "critical") {
			hasCritical = true
			break
		}
	}
	if hasCritical || risk >= 75 {
		c.Verdict = VerdictFAIL
		c.CleanBill = false
		c.Summary += "; escalated by risk/critical findings"
	} else if c.Verdict == VerdictPASS && risk >= 40 {
		c.Verdict = VerdictWARN
		c.Summary += "; elevated risk score"
	}
	return c
}

func computeDesignRiskScore(findings []DesignFinding) float64 {
	score := 15.0
	for _, f := range findings {
		switch FindRiskLevel(f.Severity) {
		case "high":
			if strings.EqualFold(f.Severity, "critical") {
				score += 35
			} else {
				score += 22
			}
		case "medium":
			score += 12
		default:
			score += 4
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

// FilterFindingsByID returns findings matching the given ID (for --fix).
func FilterFindingsByID(findings []DesignFinding, id string) []DesignFinding {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	var out []DesignFinding
	for _, f := range findings {
		if f.ID == id {
			out = append(out, f)
		}
	}
	return out
}

// AssumptionsByRisk returns assumption findings ranked high→low.
func AssumptionsByRisk(findings []DesignFinding) []DesignFinding {
	var out []DesignFinding
	for _, f := range findings {
		if f.DesignAspect == AspectAssumption {
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return riskRank(out[i].RiskLevel) > riskRank(out[j].RiskLevel)
	})
	return out
}

// FindingsByAspect filters by DesignAspect.
func FindingsByAspect(findings []DesignFinding, aspect DesignAspect) []DesignFinding {
	var out []DesignFinding
	for _, f := range findings {
		if f.DesignAspect == aspect {
			out = append(out, f)
		}
	}
	return out
}
