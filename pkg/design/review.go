package design

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/estimate"
	"github.com/totalwindupflightsystems/helix/pkg/review"
)

// AgentConsistencyChecker cross-references spec↔ADR for contradictions.
// Registered alongside the existing prosecutor agents in pkg/review.
const AgentConsistencyChecker review.AgentType = "consistency-checker"

// DesignReviewDispatcher wraps pkg/review.AdversarialAgentDispatcher with
// design-review agent variants and design-specific report aggregation.
type DesignReviewDispatcher struct {
	dispatcher *review.AdversarialAgentDispatcher
}

// NewDesignReviewDispatcher constructs a dispatcher with all design-review
// agents registered and design-oriented trigger rules.
func NewDesignReviewDispatcher() *DesignReviewDispatcher {
	d := review.NewAdversarialAgentDispatcher(
		review.WithDispatcherTriggers(designTriggers()),
	)
	// Always-on prosecutors.
	d.Register(newDesignAgent(review.AgentAssumptionBuster, prosecuteAssumptionBuster))
	d.Register(newDesignAgent(review.AgentCostAuditor, prosecuteCostAuditor))
	d.Register(newDesignAgent(AgentConsistencyChecker, prosecuteConsistencyChecker))
	// Conditional prosecutors (selected via triggers + file patterns).
	d.Register(newDesignAgent(review.AgentRedTeam, prosecuteRedTeam))
	d.Register(newDesignAgent(review.AgentChaosEngineer, prosecuteChaosEngineer))
	return &DesignReviewDispatcher{dispatcher: d}
}

// designTriggers extends DefaultTriggers with always-on design agents and
// the consistency-checker.
func designTriggers() []review.AgentTrigger {
	return []review.AgentTrigger{
		// Always for design review.
		{Agent: review.AgentAssumptionBuster, Required: true},
		{Agent: review.AgentCostAuditor, Required: true},
		{Agent: AgentConsistencyChecker, Required: true},
		// Red team when auth/crypto/secret surface is present.
		{Agent: review.AgentRedTeam, FilePattern: "auth", Required: true},
		{Agent: review.AgentRedTeam, FilePattern: "crypto", Required: true},
		{Agent: review.AgentRedTeam, FilePattern: "secret", Required: true},
		// Chaos when resilience category is selected.
		{Agent: review.AgentChaosEngineer, Category: review.CategoryResilience, Required: true},
	}
}

// Review dispatches applicable adversarial agents and builds a
// DesignReviewReport (Change Management View).
func (d *DesignReviewDispatcher) Review(ctx context.Context, req *DesignReviewRequest) (*DesignReviewReport, error) {
	if d == nil || d.dispatcher == nil {
		return nil, fmt.Errorf("design: dispatcher is nil")
	}
	if req == nil {
		return nil, fmt.Errorf("design: review request is nil")
	}
	if strings.TrimSpace(req.SpecRef) == "" {
		return nil, fmt.Errorf("design: spec_ref is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	doc := buildDesignDocument(req)
	if strings.TrimSpace(doc) == "" {
		return nil, fmt.Errorf("design: empty design document for %s", req.SpecRef)
	}

	cat, files := inferCategoryAndFiles(req, doc)
	agentReq := review.AgentRequest{
		Diff:           doc,
		CommitMsg:      fmt.Sprintf("design review of %s", req.SpecRef),
		ChangeCategory: cat,
		ChangedFiles:   files,
		PRURL:          "design://" + req.SpecRef,
	}

	dispatch, err := d.dispatcher.Dispatch(ctx, agentReq)
	if err != nil {
		return nil, fmt.Errorf("design: dispatch: %w", err)
	}

	report := &DesignReviewReport{
		SpecRef:         req.SpecRef,
		ADRRefs:         append([]string(nil), req.ADRRefs...),
		Findings:        mergeFindings(dispatch),
		ThreatSurface:   buildThreatMap(dispatch, doc),
		CostProjection:  extractCostProjection(dispatch, req),
		Consensus:       computeDesignConsensus(dispatch),
		AgentsRun:       agentsRunNames(dispatch),
		DispatchSummary: dispatch.Summary(),
		ReviewedAt:      time.Now().UTC(),
	}
	report.RiskScore = computeDesignRiskScore(report.Findings)
	// Re-evaluate consensus with risk so high risk can escalate to FAIL.
	report.Consensus = refineConsensus(report.Consensus, report.RiskScore, report.Findings)
	return report, nil
}

// Dispatcher exposes the underlying AdversarialAgentDispatcher (tests/hooks).
func (d *DesignReviewDispatcher) Dispatcher() *review.AdversarialAgentDispatcher {
	if d == nil {
		return nil
	}
	return d.dispatcher
}

// ---------------------------------------------------------------------------
// Document + trigger inference
// ---------------------------------------------------------------------------

func buildDesignDocument(req *DesignReviewRequest) string {
	var b strings.Builder
	b.WriteString("# Design Review Document\n")
	b.WriteString("SpecRef: ")
	b.WriteString(req.SpecRef)
	b.WriteString("\n")
	if req.SpecTitle != "" {
		b.WriteString("Title: ")
		b.WriteString(req.SpecTitle)
		b.WriteString("\n")
	}
	if len(req.ADRRefs) > 0 {
		b.WriteString("ADRRefs: ")
		b.WriteString(strings.Join(req.ADRRefs, ", "))
		b.WriteString("\n")
	}
	if req.Context.BudgetRemaining > 0 || req.Context.TeamCapacity > 0 || req.Context.TimelineDays > 0 {
		b.WriteString(fmt.Sprintf("Context: team_capacity=%.1f budget_remaining=%.2f timeline_days=%d\n",
			req.Context.TeamCapacity, req.Context.BudgetRemaining, req.Context.TimelineDays))
	}
	b.WriteString("\n## Spec\n")
	if strings.TrimSpace(req.SpecText) == "" {
		b.WriteString("(no spec body provided)\n")
	} else {
		b.WriteString(req.SpecText)
		b.WriteString("\n")
	}
	for i, adrText := range req.ADRTexts {
		ref := ""
		if i < len(req.ADRRefs) {
			ref = req.ADRRefs[i]
		} else {
			ref = fmt.Sprintf("adr-%d", i+1)
		}
		b.WriteString("\n## ADR ")
		b.WriteString(ref)
		b.WriteString("\n")
		b.WriteString(adrText)
		b.WriteString("\n")
	}
	if len(req.ContractSchema) > 0 {
		b.WriteString("\n## ContractSchema\n")
		b.Write(req.ContractSchema)
		b.WriteString("\n")
	}
	return b.String()
}

func inferCategoryAndFiles(req *DesignReviewRequest, doc string) (review.ChangeCategory, []string) {
	lower := strings.ToLower(doc)
	files := make([]string, 0, 8)
	seen := map[string]bool{}

	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		files = append(files, path)
	}

	for _, h := range req.ChangedHints {
		add(h)
	}

	// Synthetic path hints so review.extractFilePatterns matches keywords.
	keywordPaths := map[string]string{
		"auth":       "pkg/auth/handler.go",
		"oauth":      "pkg/auth/oauth.go",
		"crypto":     "pkg/crypto/keys.go",
		"secret":     "pkg/secrets/store.go",
		"password":   "pkg/auth/password.go",
		"token":      "pkg/auth/token.go",
		"session":    "pkg/auth/session.go",
		"permission": "pkg/auth/rbac.go",
		"rbac":       "pkg/auth/rbac.go",
		"retry":      "pkg/retry/backoff.go",
		"circuit":    "pkg/retry/circuit.go",
		"timeout":    "pkg/resilience/timeout.go",
		"failover":   "pkg/resilience/failover.go",
		"resilience": "pkg/resilience/policy.go",
	}
	for kw, path := range keywordPaths {
		if strings.Contains(lower, kw) {
			add(path)
		}
	}
	if len(files) == 0 {
		add("design/" + req.SpecRef + ".md")
	}

	if req.Category != "" {
		return req.Category, files
	}

	// Prefer resilience when recovery language dominates, else contract for auth/API, else behavioral.
	resilienceHits := countAny(lower, "retry", "circuit breaker", "timeout", "failover", "resilience", "chaos", "degrad")
	contractHits := countAny(lower, "auth", "oauth", "api contract", "openapi", "schema", "crypto", "secret")
	switch {
	case resilienceHits > 0 && resilienceHits >= contractHits:
		return review.CategoryResilience, files
	case contractHits > 0:
		return review.CategoryContract, files
	default:
		return review.CategoryBehavioral, files
	}
}

func countAny(lower string, words ...string) int {
	n := 0
	for _, w := range words {
		if strings.Contains(lower, w) {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Agent implementations (offline, deterministic prosecutors)
// ---------------------------------------------------------------------------

type designAgent struct {
	info      review.AgentInfo
	prosecute func(ctx context.Context, req review.AgentRequest) (*review.AgentResult, error)
}

func newDesignAgent(at review.AgentType, fn func(context.Context, review.AgentRequest) (*review.AgentResult, error)) *designAgent {
	return &designAgent{
		info:      designAgentInfo(at),
		prosecute: fn,
	}
}

func (a *designAgent) Identity() review.AgentInfo { return a.info }

func (a *designAgent) Prosecute(ctx context.Context, req review.AgentRequest) (*review.AgentResult, error) {
	return a.prosecute(ctx, req)
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
	sev := "info"
	verdict := "clean"
	if overBudget {
		sev = "high"
		verdict = "suspicious"
		findings = append(findings, review.Finding{
			Model:       "@cost-auditor",
			Severity:    sev,
			Type:        string(AspectCost),
			File:        "design",
			Description: fmt.Sprintf("Projected cost $%.2f exceeds remaining budget $%.2f", costUSD, budgetRemaining),
			Evidence:    notes,
			Mitigation:  "Reduce scope, switch models, or raise budget before implementation",
		})
	} else if costUSD >= 5.0 {
		sev = "medium"
		verdict = "suspicious"
		findings = append(findings, review.Finding{
			Model:       "@cost-auditor",
			Severity:    sev,
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

	// Contradictory language patterns across sections.
	if strings.Contains(lower, "must not") && strings.Contains(lower, "must ") {
		// Weak signal: presence of both strong modalities is fine; look for known pairs.
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

func contradictionPairs(text string) bool {
	// Extremely light heuristic: same token after "rejected" appears after "decision".
	lower := strings.ToLower(text)
	return strings.Contains(lower, "rejected because") &&
		(strings.Contains(lower, "we will use") || strings.Contains(lower, "decision:"))
}

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
