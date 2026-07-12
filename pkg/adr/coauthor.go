package adr

import (
	"fmt"
	"strings"
	"time"
)

// ADRCoAuthor proposes Architecture Decision Records from a spec reference
// and architecture context. Analysis is deterministic (rule-based) so offline
// CI and agent dry-runs work without LLM network calls. When marketplace
// patterns are present in context, they are cited as evidence.
type ADRCoAuthor struct {
	// DefaultAuthor is added to ADR.Authors when non-empty.
	DefaultAuthor string
}

// NewADRCoAuthor returns a co-author instance.
func NewADRCoAuthor() *ADRCoAuthor {
	return &ADRCoAuthor{DefaultAuthor: "adr-coauthor"}
}

// CoAuthor proposes an ADR for the given spec reference and architecture
// context. Every decision is evidence-linked to the spec and, when
// detectable, marketplace agent patterns.
//
// Signature matches Phase 2 §2.2: CoAuthor(specRef, architectureContext).
func (c *ADRCoAuthor) CoAuthor(specRef string, architectureContext string) (*ADR, error) {
	specRef = strings.TrimSpace(specRef)
	architectureContext = strings.TrimSpace(architectureContext)
	if specRef == "" && architectureContext == "" {
		return nil, fmt.Errorf("adr: specRef or architectureContext is required")
	}

	title := deriveTitle(specRef, architectureContext)
	decision := deriveDecision(title, architectureContext)
	context := deriveContext(specRef, architectureContext)
	alts := deriveAlternatives(title, architectureContext)
	consequences := deriveConsequences(title, alts)
	evidence := deriveEvidence(specRef, architectureContext)
	authors := []string{}
	if c != nil && c.DefaultAuthor != "" {
		authors = append(authors, c.DefaultAuthor)
	}
	authors = append(authors, "human")

	now := time.Now().UTC()
	a := &ADR{
		ID:            NewADRID(),
		Title:         title,
		Slug:          Slugify(title),
		Status:        StatusProposed,
		Context:       context,
		Decision:      decision,
		Alternatives:  alts,
		Consequences:  consequences,
		EvidenceLinks: evidence,
		Authors:       authors,
		RiskScore:     estimateRisk(title, architectureContext, alts),
		BlastRadius:   estimateBlastRadius(title, architectureContext),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	return a, nil
}

// CoAuthorFromDraft enriches a partially filled ADR (e.g. human-created
// title/context/decision) with alternatives, consequences, and evidence when
// those fields are empty.
func (c *ADRCoAuthor) CoAuthorFromDraft(draft *ADR, specRef string) (*ADR, error) {
	if draft == nil {
		return nil, fmt.Errorf("adr: draft is nil")
	}
	if strings.TrimSpace(draft.Title) == "" {
		return nil, fmt.Errorf("adr: title is required")
	}
	ctx := draft.Context
	if ctx == "" {
		ctx = draft.Decision
	}
	proposed, err := c.CoAuthor(specRef, strings.TrimSpace(draft.Title+"\n"+ctx+"\n"+draft.Decision))
	if err != nil {
		return nil, err
	}
	// Preserve human-authored fields.
	if draft.ID != "" {
		proposed.ID = draft.ID
	}
	proposed.Title = draft.Title
	proposed.Slug = Slugify(draft.Title)
	if draft.Context != "" {
		proposed.Context = draft.Context
	}
	if draft.Decision != "" {
		proposed.Decision = draft.Decision
	}
	if draft.Consequences != "" {
		proposed.Consequences = draft.Consequences
	}
	if len(draft.Alternatives) > 0 {
		proposed.Alternatives = draft.Alternatives
	}
	if len(draft.EvidenceLinks) > 0 {
		// Merge — keep draft evidence plus co-author evidence.
		proposed.EvidenceLinks = mergeEvidence(draft.EvidenceLinks, proposed.EvidenceLinks)
	}
	if len(draft.Authors) > 0 {
		proposed.Authors = draft.Authors
	} else if c != nil && c.DefaultAuthor != "" {
		proposed.Authors = []string{c.DefaultAuthor, "human"}
	}
	if draft.Status != "" {
		proposed.Status = draft.Status
	}
	if draft.Number > 0 {
		proposed.Number = draft.Number
	}
	return proposed, nil
}

func deriveTitle(specRef, architectureContext string) string {
	// Prefer first non-empty line of context that looks like a decision title.
	for _, line := range strings.Split(architectureContext, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "use ") || strings.HasPrefix(lower, "adopt ") ||
			strings.HasPrefix(lower, "prefer ") || strings.HasPrefix(lower, "choose ") {
			return truncateRunes(line, 120)
		}
		// First substantial line as title.
		if len(line) >= 8 {
			return truncateRunes(line, 120)
		}
	}
	if specRef != "" {
		return fmt.Sprintf("Architecture decision for %s", specRef)
	}
	return "Architecture decision"
}

func deriveDecision(title, architectureContext string) string {
	lower := strings.ToLower(title + " " + architectureContext)
	switch {
	case containsAny(lower, "event sourc", "event-sourc", "audit log"):
		return "We will use event sourcing for the audit log so every state change is an immutable, append-only event. Projections rebuild read models; the event store is the system of record for auditability."
	case containsAny(lower, "postgres", "postgresql", "relational"):
		return "We will use PostgreSQL as the primary system of record for transactional data, with schema migrations under version control."
	case containsAny(lower, "redis", "cache"):
		return "We will introduce Redis as a cache/session store with explicit TTLs and a cache-aside pattern; the source of truth remains the primary database."
	case containsAny(lower, "kafka", "message queue", "pub/sub", "event bus"):
		return "We will use an asynchronous message bus for cross-service events, with at-least-once delivery and idempotent consumers."
	case containsAny(lower, "monolith"):
		return "We will keep a modular monolith for this domain boundary until clear scaling or team ownership pressure requires extraction."
	case containsAny(lower, "microservice", "service mesh"):
		return "We will extract this capability into a dedicated service with a versioned API contract and independent deployability."
	case containsAny(lower, "auth", "oauth", "oidc", "sso"):
		return "We will centralize authentication via OIDC-compatible identity, with short-lived access tokens and auditable authorization decisions."
	default:
		if architectureContext != "" {
			return fmt.Sprintf("We will adopt the architecture described for %q: %s",
				title, firstSentence(architectureContext))
		}
		return fmt.Sprintf("We will proceed with %q as the selected architectural approach.", title)
	}
}

func deriveContext(specRef, architectureContext string) string {
	var parts []string
	parts = append(parts, "This ADR records a significant architectural choice for Helix.")
	if specRef != "" {
		parts = append(parts, fmt.Sprintf("It is driven by specification reference: %s.", specRef))
	}
	if architectureContext != "" {
		parts = append(parts, "Architecture context provided:")
		parts = append(parts, architectureContext)
	} else {
		parts = append(parts, "No additional architecture context was supplied; decision is derived from title and domain heuristics.")
	}
	return strings.Join(parts, "\n\n")
}

func deriveAlternatives(title, architectureContext string) []Alternative {
	lower := strings.ToLower(title + " " + architectureContext)

	// Domain-specific alternatives with tradeoff analysis.
	if containsAny(lower, "event sourc", "audit log") {
		return []Alternative{
			{
				Description:     "Event sourcing with immutable event log + projections",
				Tradeoffs:       "Strong audit trail and temporal queries; higher storage and operational complexity; eventual consistency for projections",
				RejectedBecause: "", // selected
			},
			{
				Description:     "CRUD audit table (before/after row snapshots)",
				Tradeoffs:       "Simple to implement and query; weak for multi-aggregate causality; harder to reconstruct domain intent",
				RejectedBecause: "Does not preserve causal event stream or support reliable rebuild of derived state",
			},
			{
				Description:     "Application-level logging only (structured logs)",
				Tradeoffs:       "Lowest implementation cost; not a durable system of record; log retention and integrity weaker",
				RejectedBecause: "Logs are not a transactional source of truth and fail compliance-grade audit requirements",
			},
		}
	}
	if containsAny(lower, "postgres", "postgresql", "sqlite", "mysql") {
		return []Alternative{
			{
				Description: "PostgreSQL as primary store",
				Tradeoffs:   "Mature ACID, rich indexing, ops familiarity; vertical scaling limits without sharding",
			},
			{
				Description:     "Document store (e.g. MongoDB)",
				Tradeoffs:       "Flexible schemas; weaker multi-document transactions and ad-hoc analytics",
				RejectedBecause: "Transactional integrity and relational reporting needs dominate this domain",
			},
			{
				Description:     "Embedded SQLite",
				Tradeoffs:       "Zero ops, great for single-node; limited concurrent write and HA story",
				RejectedBecause: "Multi-writer / multi-host deployment requires a networked database",
			},
		}
	}
	if containsAny(lower, "auth", "oauth", "oidc") {
		return []Alternative{
			{
				Description: "OIDC / OAuth2 centralized identity",
				Tradeoffs:   "Standards-based, SSO-friendly; dependency on IdP availability",
			},
			{
				Description:     "Custom session cookies + password store",
				Tradeoffs:       "Full control; high security maintenance burden",
				RejectedBecause: "Reimplements solved security surface and blocks marketplace agent SSO",
			},
			{
				Description:     "API keys only",
				Tradeoffs:       "Simple for machine clients; poor for human UX and rotation",
				RejectedBecause: "Does not support human+agent dual identity model in Helix",
			},
		}
	}

	// Generic three-way alternative set.
	return []Alternative{
		{
			Description: fmt.Sprintf("Adopt: %s", title),
			Tradeoffs:   "Aligns with stated architecture context; introduces change risk proportional to blast radius",
		},
		{
			Description:     "Status quo (no architectural change)",
			Tradeoffs:       "Zero migration cost; retains existing constraints and tech debt",
			RejectedBecause: "Does not address the drivers described in context",
		},
		{
			Description:     "Defer decision / spike further",
			Tradeoffs:       "Reduces premature commitment; delays delivery and may block dependent work",
			RejectedBecause: "Enough context exists to decide; deferral would stall dependent specs",
		},
	}
}

func deriveConsequences(title string, alts []Alternative) string {
	var b strings.Builder
	b.WriteString("Positive:\n")
	b.WriteString(fmt.Sprintf("- Decision %q is explicit and reviewable via multi-model ADR review.\n", title))
	b.WriteString("- Alternatives and tradeoffs are recorded for future supersession.\n")
	b.WriteString("- Evidence links keep the decision traceable to specs/patterns.\n\n")
	b.WriteString("Negative / risks:\n")
	b.WriteString("- Implementation cost and migration effort must be tracked in the linked spec.\n")
	b.WriteString("- Teams must update runbooks and blast-radius maps after acceptance.\n")
	if len(alts) > 1 {
		b.WriteString(fmt.Sprintf("- Rejected alternatives (%d) may re-surface if constraints change — supersede rather than silently reverse.\n", countRejected(alts)))
	}
	return b.String()
}

func deriveEvidence(specRef, architectureContext string) []EvidenceLink {
	var out []EvidenceLink
	if specRef != "" {
		out = append(out, EvidenceLink{
			Type:        EvidenceSpecRef,
			SpecRef:     specRef,
			Description: "Primary specification driving this ADR",
		})
	} else {
		// Always evidence-link something — marketplace pattern fallback.
		out = append(out, EvidenceLink{
			Type:               EvidenceMarketplacePattern,
			MarketplacePattern: "architecture-decision-record",
			Description:        "Default marketplace pattern for structured ADRs when no spec ref supplied",
		})
	}

	lower := strings.ToLower(architectureContext)
	if containsAny(lower, "auth", "oauth", "oidc", "security") {
		out = append(out, EvidenceLink{
			Type:               EvidenceMarketplacePattern,
			MarketplacePattern: "auth-pattern",
			Description:        "Marketplace agent pattern for authentication architecture",
		})
	}
	if containsAny(lower, "event sourc", "audit", "cqrs") {
		out = append(out, EvidenceLink{
			Type:               EvidenceMarketplacePattern,
			MarketplacePattern: "event-sourcing-audit-pattern",
			Description:        "Marketplace pattern for event-sourced audit logs",
		})
	}
	if containsAny(lower, "incident", "outage", "postmortem") {
		out = append(out, EvidenceLink{
			Type:        EvidenceIncidentRef,
			IncidentRef: "pattern:architecture-incident",
			Description: "Linked incident pattern referenced in architecture context",
		})
	}
	return out
}

func estimateRisk(title, architectureContext string, alts []Alternative) float64 {
	risk := 35.0
	lower := strings.ToLower(title + " " + architectureContext)
	if containsAny(lower, "auth", "security", "crypto", "secret", "payment") {
		risk += 25
	}
	if containsAny(lower, "migration", "rewrite", "breaking") {
		risk += 15
	}
	if containsAny(lower, "event sourc", "distributed", "multi-region") {
		risk += 10
	}
	if len(alts) < 2 {
		risk += 10 // insufficient alternative analysis
	}
	if risk > 100 {
		risk = 100
	}
	return risk
}

// estimateBlastRadius maps decision keywords to containment-style layers
// (aligned with pkg/security/blast and review blast-radius concepts).
func estimateBlastRadius(title, architectureContext string) string {
	lower := strings.ToLower(title + " " + architectureContext)
	layers := []string{"decision-local"}
	if containsAny(lower, "api", "contract", "schema", "proto", "openapi") {
		layers = append(layers, "api-contract")
	}
	if containsAny(lower, "auth", "identity", "token", "rbac") {
		layers = append(layers, "trust-boundary")
	}
	if containsAny(lower, "database", "store", "event sourc", "migration") {
		layers = append(layers, "data-plane")
	}
	if containsAny(lower, "deploy", "infra", "k8s", "network") {
		layers = append(layers, "infrastructure")
	}
	if containsAny(lower, "platform", "all services", "global") {
		layers = append(layers, "platform-wide")
	}
	return strings.Join(layers, " > ")
}

func mergeEvidence(primary, secondary []EvidenceLink) []EvidenceLink {
	seen := map[string]bool{}
	var out []EvidenceLink
	key := func(e EvidenceLink) string {
		return e.Type + "|" + e.SpecRef + "|" + e.IncidentRef + "|" + e.MarketplacePattern
	}
	for _, e := range primary {
		k := key(e)
		if !seen[k] {
			seen[k] = true
			out = append(out, e)
		}
	}
	for _, e := range secondary {
		k := key(e)
		if !seen[k] {
			seen[k] = true
			out = append(out, e)
		}
	}
	return out
}

func countRejected(alts []Alternative) int {
	n := 0
	for _, a := range alts {
		if strings.TrimSpace(a.RejectedBecause) != "" {
			n++
		}
	}
	return n
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	for _, sep := range []string{". ", ".\n", "\n"} {
		if i := strings.Index(s, sep); i > 0 {
			return strings.TrimSpace(s[:i+1])
		}
	}
	return truncateRunes(s, 240)
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}
