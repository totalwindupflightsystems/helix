package design

import (
	"context"
	"fmt"
	"strings"
	"time"

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
