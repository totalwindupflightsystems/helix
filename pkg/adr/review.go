package adr

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/review"
)

// Default consensus threshold when the request does not specify one.
const DefaultConsensusThreshold = 0.66

// Default offline model personas used when req.Models is empty and no
// ModelClients are injected. Named to mirror multi-model review panels.
var defaultADRModels = []string{
	"architecture-consistency",
	"security-surface",
	"operational-performance",
}

// ADRModelClient reviews an ADR (not a code diff). Implementations may wrap
// LLM calls; the default offline clients are deterministic rule engines.
type ADRModelClient interface {
	// Name returns the model identity used in verdicts.
	Name() string
	// ReviewADR assesses the ADR and returns a verdict.
	ReviewADR(ctx context.Context, a *ADR) (*ModelVerdict, error)
}

// ADRReviewer dispatches multi-model review of an ADR and merges consensus.
// When no clients are configured it falls back to offline deterministic
// personas. Optional review.ModelClient values can be adapted via
// AdaptReviewModelClient for live multi-model panels.
type ADRReviewer struct {
	clients []ADRModelClient
}

// NewADRReviewer constructs a reviewer. Pass zero clients to use offline
// default personas; pass injected clients to override.
func NewADRReviewer(clients ...ADRModelClient) *ADRReviewer {
	return &ADRReviewer{clients: append([]ADRModelClient(nil), clients...)}
}

// WithReviewModelClients adapts pkg/review.ModelClient instances so ADR
// content is reviewed through the existing multi-model client surface.
func (r *ADRReviewer) WithReviewModelClients(clients ...review.ModelClient) *ADRReviewer {
	for _, c := range clients {
		if c == nil {
			continue
		}
		r.clients = append(r.clients, AdaptReviewModelClient(c))
	}
	return r
}

// Review dispatches the ADR to configured (or default) models, merges
// independent verdicts, and surfaces conflicting assessments.
func (r *ADRReviewer) Review(ctx context.Context, req *ADRReviewRequest) (*ADRReviewResult, error) {
	if req == nil {
		return nil, fmt.Errorf("adr: review request is nil")
	}
	if req.ADR.ID == "" {
		return nil, fmt.Errorf("adr: adr id is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	threshold := req.ConsensusThreshold
	if threshold <= 0 {
		threshold = DefaultConsensusThreshold
	}

	clients := r.resolveClients(req.Models)
	if len(clients) == 0 {
		return nil, fmt.Errorf("adr: no review models available")
	}

	type result struct {
		v   *ModelVerdict
		err error
	}
	results := make([]result, len(clients))
	var wg sync.WaitGroup
	for i, c := range clients {
		wg.Add(1)
		go func(i int, c ADRModelClient) {
			defer wg.Done()
			// Copy ADR to avoid concurrent mutation surprises.
			adrCopy := req.ADR
			v, err := c.ReviewADR(ctx, &adrCopy)
			results[i] = result{v: v, err: err}
		}(i, c)
	}
	wg.Wait()

	var verdicts []ModelVerdict
	var errs []string
	for i, res := range results {
		if res.err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", clients[i].Name(), res.err))
			continue
		}
		if res.v == nil {
			errs = append(errs, fmt.Sprintf("%s: nil verdict", clients[i].Name()))
			continue
		}
		verdicts = append(verdicts, *res.v)
	}
	if len(verdicts) == 0 {
		return nil, fmt.Errorf("adr: all models failed: %s", strings.Join(errs, "; "))
	}

	consensus := computeConsensus(verdicts)
	conflicts := detectConflicts(verdicts)
	suggestions := collectSuggestions(verdicts)

	passed := consensus >= threshold
	summary := fmt.Sprintf(
		"consensus=%.2f threshold=%.2f models=%d passed=%v",
		consensus, threshold, len(verdicts), passed,
	)
	if len(conflicts) > 0 {
		summary += fmt.Sprintf(" conflicts=%d", len(conflicts))
	}

	return &ADRReviewResult{
		ADRID:                  req.ADR.ID,
		ModelVerdicts:          verdicts,
		ConsensusScore:         consensus,
		ConsensusThreshold:     threshold,
		Passed:                 passed,
		ConflictingAssessments: conflicts,
		SuggestedAlternatives:  suggestions,
		Summary:                summary,
		ReviewedAt:             time.Now().UTC(),
	}, nil
}

func (r *ADRReviewer) resolveClients(models []string) []ADRModelClient {
	if r != nil && len(r.clients) > 0 {
		if len(models) == 0 {
			return r.clients
		}
		// Filter injected clients by requested model name.
		want := map[string]bool{}
		for _, m := range models {
			want[strings.ToLower(m)] = true
		}
		var out []ADRModelClient
		for _, c := range r.clients {
			if want[strings.ToLower(c.Name())] {
				out = append(out, c)
			}
		}
		if len(out) > 0 {
			return out
		}
		// Fall through to offline named models when filter misses.
	}

	names := models
	if len(names) == 0 {
		names = defaultADRModels
	}
	out := make([]ADRModelClient, 0, len(names))
	for _, name := range names {
		out = append(out, newOfflineADRClient(name))
	}
	return out
}

// ---------------------------------------------------------------------------
// Offline deterministic model personas
// ---------------------------------------------------------------------------

type offlineADRClient struct {
	name string
}

func newOfflineADRClient(name string) ADRModelClient {
	return &offlineADRClient{name: name}
}

func (c *offlineADRClient) Name() string { return c.name }

func (c *offlineADRClient) ReviewADR(_ context.Context, a *ADR) (*ModelVerdict, error) {
	if a == nil {
		return nil, fmt.Errorf("adr is nil")
	}
	switch {
	case strings.Contains(strings.ToLower(c.name), "security"):
		return reviewSecurity(c.name, a), nil
	case strings.Contains(strings.ToLower(c.name), "performance"),
		strings.Contains(strings.ToLower(c.name), "operational"):
		return reviewPerformance(c.name, a), nil
	default:
		return reviewArchitecture(c.name, a), nil
	}
}

func reviewArchitecture(model string, a *ADR) *ModelVerdict {
	var concerns []string
	var suggestions []string
	score := 0.85

	if strings.TrimSpace(a.Decision) == "" {
		concerns = append(concerns, "Decision section is empty")
		score -= 0.4
	}
	if strings.TrimSpace(a.Context) == "" {
		concerns = append(concerns, "Context section is empty — drivers unclear")
		score -= 0.15
	}
	if len(a.Alternatives) < 2 {
		concerns = append(concerns, "Fewer than 2 alternatives considered")
		suggestions = append(suggestions, "Document at least one rejected alternative with tradeoffs")
		score -= 0.2
	}
	rejected := 0
	for _, alt := range a.Alternatives {
		if strings.TrimSpace(alt.RejectedBecause) != "" {
			rejected++
		}
	}
	if len(a.Alternatives) >= 2 && rejected == 0 {
		suggestions = append(suggestions, "Mark rejected alternatives with rationale")
		score -= 0.05
	}
	if !a.HasEvidence() {
		concerns = append(concerns, "No evidence links — decision is untraceable")
		score -= 0.25
	}
	if strings.TrimSpace(a.Consequences) == "" {
		concerns = append(concerns, "Consequences not documented")
		score -= 0.1
	}

	return verdictFromScore(model, score, concerns, suggestions,
		"Architecture consistency review of decision structure and alternatives")
}

func reviewSecurity(model string, a *ADR) *ModelVerdict {
	var concerns []string
	var suggestions []string
	score := 0.8
	blob := strings.ToLower(a.Title + " " + a.Context + " " + a.Decision + " " + a.Consequences)

	sensitive := containsAny(blob, "auth", "token", "secret", "password", "crypto", "pii", "credential", "oauth")
	if sensitive {
		if !containsAny(blob, "threat", "rotate", "least privilege", "audit", "encrypt", "tls", "rbac", "scope") {
			concerns = append(concerns, "Security-sensitive decision lacks explicit threat/controls language")
			suggestions = append(suggestions, "Add threat model notes and control requirements to Consequences")
			score -= 0.25
		}
	}
	if containsAny(blob, "public", "internet", "external api") && !containsAny(blob, "rate limit", "auth", "waf") {
		concerns = append(concerns, "Externally exposed surface without rate-limit/auth mention")
		score -= 0.15
	}
	if !a.HasEvidence() {
		concerns = append(concerns, "Missing evidence links for security-relevant decision")
		score -= 0.1
	}
	if len(a.Alternatives) == 0 {
		concerns = append(concerns, "No alternatives — security tradeoffs unexamined")
		score -= 0.2
	}

	return verdictFromScore(model, score, concerns, suggestions,
		"Security surface review of trust boundaries and controls")
}

func reviewPerformance(model string, a *ADR) *ModelVerdict {
	var concerns []string
	var suggestions []string
	score := 0.82
	blob := strings.ToLower(a.Title + " " + a.Context + " " + a.Decision + " " + a.Consequences)

	if containsAny(blob, "event sourc", "kafka", "stream", "queue") {
		if !containsAny(blob, "lag", "throughput", "retention", "backpressure", "consumer") {
			concerns = append(concerns, "Async/event architecture without operational SLOs (lag/throughput/retention)")
			suggestions = append(suggestions, "Document retention, consumer lag budgets, and backpressure strategy")
			score -= 0.2
		}
	}
	if containsAny(blob, "cache", "redis") && !containsAny(blob, "ttl", "invalidat", "stampede") {
		concerns = append(concerns, "Caching decision missing TTL/invalidation strategy")
		score -= 0.15
	}
	if containsAny(blob, "migration", "rewrite") && !containsAny(blob, "rollback", "dual-write", "feature flag") {
		concerns = append(concerns, "Migration path lacks rollback/dual-write/feature-flag strategy")
		suggestions = append(suggestions, "Add operational migration and rollback plan to Consequences")
		score -= 0.15
	}
	if strings.TrimSpace(a.Consequences) == "" {
		concerns = append(concerns, "No operational consequences documented")
		score -= 0.1
	}

	return verdictFromScore(model, score, concerns, suggestions,
		"Operational/performance review of scalability and operability")
}

func verdictFromScore(model string, score float64, concerns, suggestions []string, rationale string) *ModelVerdict {
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	verdict := "approve"
	switch {
	case score < 0.5:
		verdict = "reject"
	case score < 0.75:
		verdict = "warn"
	}
	if rationale == "" {
		rationale = "Offline structural review"
	}
	if len(concerns) > 0 {
		rationale = rationale + "; concerns: " + strings.Join(concerns, "; ")
	}
	return &ModelVerdict{
		Model:       model,
		Verdict:     verdict,
		Score:       score,
		Rationale:   rationale,
		Concerns:    concerns,
		Suggestions: suggestions,
	}
}

func computeConsensus(verdicts []ModelVerdict) float64 {
	if len(verdicts) == 0 {
		return 0
	}
	// Average model scores, weighted lightly by verdict polarity.
	sum := 0.0
	for _, v := range verdicts {
		s := v.Score
		switch strings.ToLower(v.Verdict) {
		case "approve", "approved", "pass":
			if s < 0.75 {
				s = 0.75
			}
		case "warn", "pass_with_notes":
			if s > 0.74 {
				s = 0.7
			}
			if s < 0.5 {
				s = 0.55
			}
		case "reject", "block":
			if s > 0.49 {
				s = 0.4
			}
		}
		sum += s
	}
	return sum / float64(len(verdicts))
}

func detectConflicts(verdicts []ModelVerdict) []ConflictingAssessment {
	if len(verdicts) < 2 {
		return nil
	}
	// Group by coarse verdict class.
	positions := map[string]string{}
	classes := map[string]int{}
	for _, v := range verdicts {
		cls := normalizeVerdictClass(v.Verdict)
		positions[v.Model] = cls + ": " + truncateRunes(v.Rationale, 120)
		classes[cls]++
	}
	if len(classes) <= 1 {
		return nil
	}
	return []ConflictingAssessment{{
		Topic:     "overall-verdict",
		Positions: positions,
		Rationale: "Models disagree on whether the ADR should be accepted as written",
	}}
}

func normalizeVerdictClass(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "approve", "approved", "pass":
		return "approve"
	case "warn", "pass_with_notes":
		return "warn"
	case "reject", "block":
		return "reject"
	default:
		return strings.ToLower(v)
	}
}

func collectSuggestions(verdicts []ModelVerdict) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range verdicts {
		for _, s := range v.Suggestions {
			s = strings.TrimSpace(s)
			if s == "" || seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Adapter: pkg/review.ModelClient → ADRModelClient
// ---------------------------------------------------------------------------

// reviewModelAdapter wraps a code-review ModelClient so ADR text is submitted
// as the "diff" payload with an architecture change category.
type reviewModelAdapter struct {
	client review.ModelClient
}

// AdaptReviewModelClient bridges pkg/review.ModelClient into ADR review.
func AdaptReviewModelClient(c review.ModelClient) ADRModelClient {
	return &reviewModelAdapter{client: c}
}

func (a *reviewModelAdapter) Name() string {
	info := a.client.Info()
	if info.Model != "" {
		return info.Model
	}
	if info.Provider != "" {
		return info.Provider
	}
	return "review-model"
}

func (a *reviewModelAdapter) ReviewADR(ctx context.Context, adr *ADR) (*ModelVerdict, error) {
	payload := formatADRAsReviewPayload(adr)
	req := review.ReviewRequest{
		Diff:             payload,
		NeutralCommitMsg: "ADR review: " + adr.Title,
		Role:             review.RolePrimary,
		Context: review.ReviewContext{
			Category: review.CategoryContract,
			ReviewID: "adr-" + adr.ID,
		},
	}
	res, err := a.client.Review(ctx, req)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, fmt.Errorf("nil model result")
	}
	score := 0.8
	verdict := "approve"
	switch strings.ToLower(res.Verdict) {
	case "approved", "approve", "pass":
		verdict = "approve"
		score = 0.9
	case "pass_with_notes", "warn":
		verdict = "warn"
		score = 0.65
	case "block", "reject":
		verdict = "reject"
		score = 0.3
	}
	var concerns []string
	for _, f := range res.Findings {
		concerns = append(concerns, f.Description)
		if f.Severity == "critical" || f.Severity == "high" {
			score -= 0.1
		}
	}
	if score < 0 {
		score = 0
	}
	return &ModelVerdict{
		Model:     a.Name(),
		Verdict:   verdict,
		Score:     score,
		Rationale: fmt.Sprintf("Delegated to review.ModelClient verdict=%s findings=%d", res.Verdict, len(res.Findings)),
		Concerns:  concerns,
	}, nil
}

func formatADRAsReviewPayload(a *ADR) string {
	var b strings.Builder
	b.WriteString("ADR Title: ")
	b.WriteString(a.Title)
	b.WriteString("\nStatus: ")
	b.WriteString(a.Status)
	b.WriteString("\n\n## Context\n")
	b.WriteString(a.Context)
	b.WriteString("\n\n## Decision\n")
	b.WriteString(a.Decision)
	b.WriteString("\n\n## Alternatives\n")
	for i, alt := range a.Alternatives {
		b.WriteString(fmt.Sprintf("%d. %s\n   Tradeoffs: %s\n   Rejected: %s\n",
			i+1, alt.Description, alt.Tradeoffs, alt.RejectedBecause))
	}
	b.WriteString("\n## Consequences\n")
	b.WriteString(a.Consequences)
	return b.String()
}
