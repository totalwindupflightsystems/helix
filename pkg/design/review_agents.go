package design

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/review"
)

func prosecuteAssumptionBuster(_ context.Context, req review.AgentRequest) (*review.AgentResult, error) {
	text := req.Diff
	lower := strings.ToLower(text)
	var assumptions []review.Assumption
	var findings []review.Finding

	// Always surface baseline design assumptions for non-trivial docs.
	baseline := []struct {
		desc, challenge, risk string
	}{
		{"Operators will keep the design document current as implementation evolves", "Docs drift; agents may implement stale design", "medium"},
		{"Referenced ADRs remain accepted and non-superseded during implementation", "ADR supersession can invalidate downstream work", "medium"},
		{"Team capacity and budget in DesignContext are accurate", "Underestimated capacity causes schedule slip", "low"},
		{"External dependencies named in the design stay available and API-stable", "Upstream breaking changes force redesign", "medium"},
		{"Security review coverage is complete for listed trust boundaries", "Missed boundaries create unreviewed attack surface", "high"},
	}
	for _, b := range baseline {
		assumptions = append(assumptions, review.Assumption{
			Description: b.desc,
			File:        "design",
			RiskLevel:   b.risk,
			Challenge:   b.challenge,
		})
	}

	// Content-derived assumptions.
	if !strings.Contains(lower, "rollback") && !strings.Contains(lower, "rollback plan") {
		assumptions = append(assumptions, review.Assumption{
			Description: "No explicit rollback plan is required",
			File:        "design",
			RiskLevel:   "high",
			Challenge:   "Production failures without rollback amplify blast radius",
		})
	}
	if strings.Contains(lower, "simply") || strings.Contains(lower, "just ") || strings.Contains(lower, "trivial") {
		assumptions = append(assumptions, review.Assumption{
			Description: "Complexity is low because language uses 'simply/just/trivial'",
			File:        "design",
			RiskLevel:   "medium",
			Challenge:   "Hand-waving hides edge cases and multi-step failure modes",
		})
	}
	if !strings.Contains(lower, "auth") && !strings.Contains(lower, "authentication") && strings.Contains(lower, "api") {
		assumptions = append(assumptions, review.Assumption{
			Description: "API surface does not require authentication design",
			File:        "design",
			RiskLevel:   "high",
			Challenge:   "Unauthenticated APIs are a default exploit path",
		})
	}
	if !strings.Contains(lower, "rate limit") && !strings.Contains(lower, "throttl") {
		assumptions = append(assumptions, review.Assumption{
			Description: "No rate limiting is needed for the design's request surface",
			File:        "design",
			RiskLevel:   "medium",
			Challenge:   "Without throttling, cost and DoS exposure grow unbounded",
		})
	}
	if len(text) < 200 {
		assumptions = append(assumptions, review.Assumption{
			Description: "A short design document is sufficient for implementation",
			File:        "design",
			RiskLevel:   "high",
			Challenge:   "Sparse designs omit failure modes, contracts, and acceptance criteria",
		})
	}

	for i, a := range assumptions {
		sev := a.RiskLevel
		if sev == "high" {
			sev = "high"
		}
		findings = append(findings, review.Finding{
			Model:       "@assumption-buster",
			Severity:    sev,
			Type:        string(AspectAssumption),
			File:        a.File,
			Line:        i + 1,
			Description: a.Description,
			Evidence:    a.Challenge,
			Mitigation:  "Document validation criteria and owner for this assumption",
		})
	}

	verdict := "suspicious"
	if len(assumptions) == 0 {
		verdict = "clean"
	}
	// High-risk assumptions keep suspicious; critical language → exploited-like.
	for _, a := range assumptions {
		if a.RiskLevel == "high" && (strings.Contains(strings.ToLower(a.Description), "auth") || len(text) < 200) {
			verdict = "suspicious"
			break
		}
	}

	return &review.AgentResult{
		AgentType:        review.AgentAssumptionBuster,
		AssumptionsFound: assumptions,
		Findings:         findings,
		Verdict:          verdict,
	}, nil
}

func prosecuteRedTeam(_ context.Context, req review.AgentRequest) (*review.AgentResult, error) {
	lower := strings.ToLower(req.Diff)
	var exploits []review.ExploitPath
	var findings []review.Finding

	candidates := []struct {
		kw, desc, sev, file, mit string
		steps                    []string
	}{
		{"auth", "Privilege escalation via weak authentication boundary", "high", "pkg/auth/handler.go",
			"Enforce MFA + short-lived tokens + explicit trust boundaries",
			[]string{"Identify unauthenticated entrypoints", "Forge session or token", "Access privileged resource"}},
		{"token", "Token replay / stolen credential reuse", "high", "pkg/auth/token.go",
			"Bind tokens to device/session, rotate, revoke on anomaly",
			[]string{"Capture bearer token", "Replay from second client", "Operate as victim"}},
		{"secret", "Secret leakage through logs or config surfaces", "critical", "pkg/secrets/store.go",
			"Never log secrets; use sealed storage + rotation",
			[]string{"Trigger error path", "Inspect logs/config", "Exfiltrate credentials"}},
		{"crypto", "Weak cryptography or key management misuse", "critical", "pkg/crypto/keys.go",
			"Use vetted primitives; separate keys by purpose; rotate",
			[]string{"Identify crypto usage", "Attack key storage or nonce reuse", "Decrypt or forge"}},
		{"password", "Password brute-force / credential stuffing", "medium", "pkg/auth/password.go",
			"Rate limit + lockout + passwordless where possible",
			[]string{"Enumerate login endpoint", "Spray credentials", "Gain account access"}},
		{"admin", "Missing admin authorization checks", "high", "pkg/admin/api.go",
			"Default-deny RBAC on admin routes",
			[]string{"Find admin path", "Call without elevated role", "Mutate system state"}},
	}

	for _, c := range candidates {
		if !strings.Contains(lower, c.kw) {
			continue
		}
		exploits = append(exploits, review.ExploitPath{
			Description: c.desc,
			Severity:    c.sev,
			File:        c.file,
			Line:        1,
			Steps:       c.steps,
			Evidence:    fmt.Sprintf("design mentions %q", c.kw),
		})
		findings = append(findings, review.Finding{
			Model:       "@redteam",
			Severity:    c.sev,
			Type:        string(AspectThreat),
			File:        c.file,
			Line:        1,
			Description: c.desc,
			Evidence:    fmt.Sprintf("keyword %q present in design", c.kw),
			Mitigation:  c.mit,
		})
	}

	if len(exploits) == 0 {
		// Still map a generic external-input vector when any API surface is present.
		if strings.Contains(lower, "api") || strings.Contains(lower, "endpoint") {
			exploits = append(exploits, review.ExploitPath{
				Description: "Unvalidated input on public API surface",
				Severity:    "medium",
				File:        "pkg/api/handler.go",
				Steps:       []string{"Locate public endpoint", "Send malformed payload", "Trigger unexpected state"},
				Evidence:    "API surface without explicit input validation design",
			})
			findings = append(findings, review.Finding{
				Model:       "@redteam",
				Severity:    "medium",
				Type:        string(AspectThreat),
				File:        "pkg/api/handler.go",
				Description: "Unvalidated input on public API surface",
				Evidence:    "API surface without explicit input validation design",
				Mitigation:  "Define schema validation and rejection paths per endpoint",
			})
		}
	}

	verdict := "clean"
	if len(exploits) > 0 {
		verdict = "suspicious"
	}
	for _, e := range exploits {
		if e.Severity == "critical" {
			verdict = "exploited"
			break
		}
	}

	return &review.AgentResult{
		AgentType:    review.AgentRedTeam,
		ExploitPaths: exploits,
		Findings:     findings,
		Verdict:      verdict,
	}, nil
}

func prosecuteChaosEngineer(_ context.Context, req review.AgentRequest) (*review.AgentResult, error) {
	lower := strings.ToLower(req.Diff)
	var faults []review.FaultResult
	var findings []review.Finding

	scenarios := []struct {
		faultType, desc, outcome, sev string
		need                          string // if design lacks this recovery keyword → bad outcome
	}{
		{"network", "Network partition between services", "degraded", "high", "partition"},
		{"timeout", "Downstream timeout under load", "recovered", "medium", "timeout"},
		{"disk", "Disk full on stateful component", "crashed", "high", "disk"},
		{"rate-limit", "Upstream rate limit hit", "degraded", "medium", "rate limit"},
		{"dependency", "Critical dependency unavailable", "degraded", "high", "fallback"},
	}

	for _, s := range scenarios {
		outcome := s.outcome
		// If design mentions recovery language for this fault, mark recovered.
		if strings.Contains(lower, s.need) || strings.Contains(lower, "retry") || strings.Contains(lower, "circuit") {
			outcome = "recovered"
			if s.sev == "high" {
				s.sev = "medium"
			}
		}
		faults = append(faults, review.FaultResult{
			FaultType:   s.faultType,
			Description: s.desc,
			Outcome:     outcome,
			Severity:    s.sev,
		})
		if outcome != "recovered" {
			findings = append(findings, review.Finding{
				Model:       "@chaos-engineer",
				Severity:    s.sev,
				Type:        string(AspectCompleteness),
				File:        "design",
				Description: fmt.Sprintf("No clear recovery path for: %s", s.desc),
				Evidence:    fmt.Sprintf("fault=%s outcome=%s", s.faultType, outcome),
				Mitigation:  "Document recovery, fallback, and SLOs for this fault class",
			})
		}
	}

	verdict := "clean"
	for _, f := range faults {
		if f.Outcome == "crashed" {
			verdict = "exploited"
			break
		}
		if f.Outcome == "degraded" {
			verdict = "suspicious"
		}
	}

	return &review.AgentResult{
		AgentType:       review.AgentChaosEngineer,
		FaultInjections: faults,
		Findings:        findings,
		Verdict:         verdict,
	}, nil
}

func prosecuteCostAuditor(_ context.Context, req review.AgentRequest) (*review.AgentResult, error) {
	text := req.Diff
	// Heuristic token projection: ~1.3 tokens/word + section overhead.
	words := len(strings.Fields(text))
	sections := strings.Count(text, "##")
	baseTokens := int64(words)*13/10 + int64(sections)*500 + 8000
	if baseTokens < 10000 {
		baseTokens = 10000
	}
	// Implementation multiplier: design review estimates implementation burn.
	implTokens := baseTokens * 12
	// Rough USD: $0.50 / 1M tokens blended (offline default).
	costUSD := float64(implTokens) * 0.50 / 1_000_000
	// Round up to cent.
	costUSD = math.Ceil(costUSD*100) / 100
	if costUSD < 0.01 {
		costUSD = 0.01
	}

	budgetRemaining := 0.0
	// Parse DesignContext line if present.
	if m := regexp.MustCompile(`budget_remaining=([0-9.]+)`).FindStringSubmatch(text); len(m) == 2 {
		var v float64
		_, _ = fmt.Sscanf(m[1], "%f", &v)
		budgetRemaining = v
	}

	overBudget := budgetRemaining > 0 && costUSD > budgetRemaining
	notes := fmt.Sprintf("heuristic projection from design size (words=%d sections=%d)", words, sections)
	if overBudget {
		notes += "; OVER BUDGET relative to DesignContext.budget_remaining"
	}

	cost := &review.CostEstimate{
		EstimatedTokens:  implTokens,
		EstimatedCostUSD: costUSD,
		BudgetRemaining:  budgetRemaining,
		OverBudget:       overBudget,
		Notes:            notes,
	}

	var findings []review.Finding
	verdict := "clean"
	if overBudget {
		verdict = "suspicious"
		findings = append(findings, review.Finding{
			Model:       "@cost-auditor",
			Severity:    "high",
			Type:        string(AspectCost),
			File:        "design",
			Description: fmt.Sprintf("Projected cost $%.2f exceeds remaining budget $%.2f", costUSD, budgetRemaining),
			Evidence:    notes,
			Mitigation:  "Reduce scope, switch models, or raise budget before implementation",
		})
	} else if costUSD >= 5.0 {
		verdict = "suspicious"
		findings = append(findings, review.Finding{
			Model:       "@cost-auditor",
			Severity:    "medium",
			Type:        string(AspectCost),
			File:        "design",
			Description: fmt.Sprintf("Projected implementation cost is elevated ($%.2f)", costUSD),
			Evidence:    notes,
			Mitigation:  "Break work into smaller tasks and re-estimate per slice",
		})
	} else {
		findings = append(findings, review.Finding{
			Model:       "@cost-auditor",
			Severity:    "info",
			Type:        string(AspectCost),
			File:        "design",
			Description: fmt.Sprintf("Projected implementation cost $%.2f (~%d tokens)", costUSD, implTokens),
			Evidence:    notes,
		})
	}

	return &review.AgentResult{
		AgentType:    review.AgentCostAuditor,
		CostEstimate: cost,
		Findings:     findings,
		Verdict:      verdict,
	}, nil
}

func prosecuteConsistencyChecker(_ context.Context, req review.AgentRequest) (*review.AgentResult, error) {
	text := req.Diff
	lower := strings.ToLower(text)
	var findings []review.Finding

	hasSpec := strings.Contains(text, "## Spec")
	hasADR := strings.Contains(text, "## ADR")
	hasContract := strings.Contains(text, "## ContractSchema")

	if hasSpec && !hasADR && strings.Contains(lower, "architecture decision") {
		findings = append(findings, review.Finding{
			Model:       "@consistency-checker",
			Severity:    "medium",
			Type:        string(AspectConsistency),
			File:        "design",
			Description: "Spec references architecture decisions but no ADR body was attached",
			Evidence:    "missing ## ADR sections",
			Mitigation:  "Link and include accepted ADRs in the design review request",
		})
	}

	// Rejected alternative still described as chosen decision.
	if strings.Contains(lower, "rejected because") && strings.Contains(lower, "we will use") {
		// Check if same option appears as both rejected and chosen — crude line scan.
		if contradictionPairs(text) {
			findings = append(findings, review.Finding{
				Model:       "@consistency-checker",
				Severity:    "high",
				Type:        string(AspectConsistency),
				File:        "design",
				Description: "Possible contradiction: rejected alternative language coexists with affirmative decision",
				Evidence:    "rejected because + we will use",
				Mitigation:  "Reconcile ADR alternatives with the chosen decision text",
			})
		}
	}

	// Status mismatches.
	if strings.Contains(lower, "status: superseded") && strings.Contains(lower, "status: accepted") {
		findings = append(findings, review.Finding{
			Model:       "@consistency-checker",
			Severity:    "medium",
			Type:        string(AspectConsistency),
			File:        "design",
			Description: "Mixed ADR statuses (accepted + superseded) in one review package",
			Evidence:    "status: accepted and status: superseded both present",
			Mitigation:  "Review only the currently accepted ADR lineage",
		})
	}

	// Completeness gaps.
	required := []struct {
		kw, label string
	}{
		{"acceptance", "acceptance criteria"},
		{"error", "error handling"},
		{"test", "test strategy"},
	}
	for _, r := range required {
		if !strings.Contains(lower, r.kw) {
			findings = append(findings, review.Finding{
				Model:       "@consistency-checker",
				Severity:    "medium",
				Type:        string(AspectCompleteness),
				File:        "design",
				Description: fmt.Sprintf("Design appears to omit %s", r.label),
				Evidence:    fmt.Sprintf("keyword %q not found", r.kw),
				Mitigation:  fmt.Sprintf("Add a section covering %s", r.label),
			})
		}
	}

	if hasContract {
		// Schema present — check for required field mentions vs empty schema.
		if len(strings.TrimSpace(text[strings.Index(text, "## ContractSchema"):])) < 40 {
			findings = append(findings, review.Finding{
				Model:       "@consistency-checker",
				Severity:    "low",
				Type:        string(AspectConsistency),
				File:        "contract",
				Description: "Contract schema section is nearly empty",
				Evidence:    "ContractSchema block too short",
				Mitigation:  "Attach full OpenAPI/protobuf schema before freeze",
			})
		}
	}

	verdict := "clean"
	for _, f := range findings {
		if f.Severity == "high" || f.Severity == "critical" {
			verdict = "suspicious"
			break
		}
		if f.Severity == "medium" {
			verdict = "suspicious"
		}
	}
	if len(findings) == 0 {
		findings = append(findings, review.Finding{
			Model:       "@consistency-checker",
			Severity:    "info",
			Type:        string(AspectConsistency),
			File:        "design",
			Description: "No cross-document contradictions detected",
		})
	}

	return &review.AgentResult{
		AgentType: AgentConsistencyChecker,
		Findings:  findings,
		Verdict:   verdict,
	}, nil
}

func designAgentInfo(at review.AgentType) review.AgentInfo {
	switch at {
	case AgentConsistencyChecker:
		return review.AgentInfo{
			Type:    at,
			Name:    "@consistency-checker",
			Mission: "Cross-reference spec↔ADR↔contract for contradictions",
		}
	default:
		return review.DefaultAgentInfo(at)
	}
}

func contradictionPairs(text string) bool {
	// Extremely light heuristic: same token after "rejected" appears after "decision".
	lower := strings.ToLower(text)
	return strings.Contains(lower, "rejected because") &&
		(strings.Contains(lower, "we will use") || strings.Contains(lower, "decision:"))
}
