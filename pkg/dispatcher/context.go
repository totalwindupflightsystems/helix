// Package dispatcher provides the Helix orchestration layer that replaces
// Axiom. It decomposes specifications into tasks, assigns tasks to capable
// agents, and drives the Ralph Loop execution pipeline.
package dispatcher

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/adr"
	"github.com/totalwindupflightsystems/helix/pkg/incident"
	"github.com/totalwindupflightsystems/helix/pkg/spec"
)

// ---------------------------------------------------------------------------
// Context budget
// ---------------------------------------------------------------------------

// ContextBudget tracks token consumption against a total limit.
type ContextBudget struct {
	total     int
	remaining int
}

// NewContextBudget creates a budget with the given token limit.
func NewContextBudget(tokens int) *ContextBudget {
	return &ContextBudget{total: tokens, remaining: tokens}
}

// Consume deducts tokens from the budget. Returns false if insufficient.
func (b *ContextBudget) Consume(tokens int) bool {
	if tokens > b.remaining {
		return false
	}
	b.remaining -= tokens
	return true
}

// Remaining returns the unused token budget.
func (b *ContextBudget) Remaining() int {
	return b.remaining
}

// Used returns how many tokens have been consumed.
func (b *ContextBudget) Used() int {
	return b.total - b.remaining
}

// EstimateTokens returns a rough token count for a string: len/4 rounded up,
// minimum 1.
func EstimateTokens(s string) int {
	n := (len(s) + 3) / 4
	if n < 1 {
		n = 1
	}
	return n
}

// ---------------------------------------------------------------------------
// Assembled context
// ---------------------------------------------------------------------------

// ContextSection holds a single piece of contextual information.
type ContextSection struct {
	Title   string
	Content string
}

// AssembledContext is the context package delivered to an agent on assignment.
type AssembledContext struct {
	SpecSections    []ContextSection
	RelatedPRs      []string
	ADRConstraints  []string
	IncidentHistory []string
	BudgetUsed      int
	BudgetTotal     int
}

// IsEmpty reports whether the context contains no sections at all.
func (ac *AssembledContext) IsEmpty() bool {
	return len(ac.SpecSections) == 0 &&
		len(ac.RelatedPRs) == 0 &&
		len(ac.ADRConstraints) == 0 &&
		len(ac.IncidentHistory) == 0
}

// SectionNames returns the ordered list of section names supported by Expand.
const (
	SectionSpecs     = "specs"
	SectionPRs       = "prs"
	SectionADRs      = "adrs"
	SectionIncidents = "incidents"
)

// ---------------------------------------------------------------------------
// Context assembler
// ---------------------------------------------------------------------------

// ContextAssembler builds context packages for agent task assignments.
type ContextAssembler struct {
	SpecStore     *spec.SpecStore
	ADRStore      *adr.ADRStore
	IncidentStore *incident.Store
	RepoPath      string
	Budget        int
}

// DefaultBudget is the token budget used when Budget <= 0.
const DefaultBudget = 4096

// Assemble builds an AssembledContext for the given task, respecting the
// configured token budget. When stores are nil, the corresponding section is
// empty (no error).
func (ca *ContextAssembler) Assemble(task Task) (*AssembledContext, error) {
	budget := ca.Budget
	if budget <= 0 {
		budget = DefaultBudget
	}
	ctx := &AssembledContext{
		BudgetTotal: budget,
	}
	cb := NewContextBudget(budget)

	// 1. Spec sections (allocate 40% of budget).
	specBudget := budget * 40 / 100
	ca.assembleSpecs(ctx, cb, task, specBudget)

	// 2. Related PRs (20%).
	prBudget := budget * 20 / 100
	ca.assemblePRs(ctx, cb, prBudget)

	// 3. ADR constraints (20%).
	adrBudget := budget * 20 / 100
	ca.assembleADRs(ctx, cb, adrBudget)

	// 4. Incident history (20%).
	incBudget := budget * 20 / 100
	ca.assembleIncidents(ctx, cb, task, incBudget)

	ctx.BudgetUsed = cb.Used()
	return ctx, nil
}

// Expand increases the budget for a specific section and re-fetches its
// content. section must be one of SectionSpecs, SectionPRs, SectionADRs,
// SectionIncidents.
func (ca *ContextAssembler) Expand(ctx *AssembledContext, section string, extraBudget int) error {
	if ctx == nil {
		return fmt.Errorf("context: ctx is nil")
	}
	if extraBudget <= 0 {
		return fmt.Errorf("context: extra budget must be positive, got %d", extraBudget)
	}
	ctx.BudgetTotal += extraBudget
	switch section {
	case SectionSpecs:
		// Re-assemble specs with the extra budget only.
		ca.assembleSpecs(ctx, NewContextBudget(ctx.BudgetTotal), Task{}, extraBudget)
	case SectionPRs:
		ca.assemblePRs(ctx, NewContextBudget(ctx.BudgetTotal), extraBudget)
	case SectionADRs:
		ca.assembleADRs(ctx, NewContextBudget(ctx.BudgetTotal), extraBudget)
	case SectionIncidents:
		ca.assembleIncidents(ctx, NewContextBudget(ctx.BudgetTotal), Task{}, extraBudget)
	default:
		return fmt.Errorf("context: unknown section %q (valid: %s, %s, %s, %s)",
			section, SectionSpecs, SectionPRs, SectionADRs, SectionIncidents)
	}
	ctx.BudgetUsed = ctx.BudgetTotal // expansion consumes the extended budget
	return nil
}

// ---------------------------------------------------------------------------
// Private helpers
// ---------------------------------------------------------------------------

func (ca *ContextAssembler) assembleSpecs(ctx *AssembledContext, cb *ContextBudget, task Task, subBudget int) {
	if ca.SpecStore == nil || task.SpecRef == "" {
		return
	}
	sp, err := ca.SpecStore.Load(task.SpecRef)
	if err != nil {
		return
	}
	scb := NewContextBudget(subBudget)
	for _, sec := range sp.Sections {
		title := trimToBudget(sec.Title, scb, 0)
		body := trimToBudget(sec.Content, scb, 0)
		if title == "" && body == "" {
			break
		}
		tokens := EstimateTokens(title) + EstimateTokens(body)
		if !cb.Consume(tokens) {
			return
		}
		ctx.SpecSections = append(ctx.SpecSections, ContextSection{
			Title:   title,
			Content: body,
		})
	}
}

func (ca *ContextAssembler) assemblePRs(ctx *AssembledContext, cb *ContextBudget, subBudget int) {
	if ca.RepoPath == "" {
		return
	}
	out, err := exec.Command("git", "-C", ca.RepoPath, "log", "--oneline", "-10").Output()
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	scb := NewContextBudget(subBudget)
	for _, line := range lines {
		trimmed := trimToBudget(line, scb, 0)
		if trimmed == "" {
			break
		}
		tokens := EstimateTokens(trimmed)
		if !cb.Consume(tokens) {
			return
		}
		ctx.RelatedPRs = append(ctx.RelatedPRs, trimmed)
	}
}

func (ca *ContextAssembler) assembleADRs(ctx *AssembledContext, cb *ContextBudget, subBudget int) {
	if ca.ADRStore == nil {
		return
	}
	adrs, err := ca.ADRStore.List()
	if err != nil {
		return
	}
	// Sort by most recent: Accepted/Proposed first.
	sort.Slice(adrs, func(i, j int) bool {
		ri := statusRank(adrs[i].Status)
		rj := statusRank(adrs[j].Status)
		if ri != rj {
			return ri < rj // lower rank = more relevant
		}
		return adrs[i].Number > adrs[j].Number
	})
	scb := NewContextBudget(subBudget)
	for _, a := range adrs {
		sum := fmt.Sprintf("ADR-%04d %s: %s", a.Number, a.Title, a.Decision)
		trimmed := trimToBudget(sum, scb, 0)
		if trimmed == "" {
			break
		}
		tokens := EstimateTokens(trimmed)
		if !cb.Consume(tokens) {
			return
		}
		ctx.ADRConstraints = append(ctx.ADRConstraints, trimmed)
	}
}

func (ca *ContextAssembler) assembleIncidents(ctx *AssembledContext, cb *ContextBudget, task Task, subBudget int) {
	if ca.IncidentStore == nil {
		return
	}
	all := ca.IncidentStore.All()
	if len(all) == 0 {
		return
	}
	// Sort by recency (newest first).
	sort.Slice(all, func(i, j int) bool {
		return all[i].Timestamp.After(all[j].Timestamp)
	})
	scb := NewContextBudget(subBudget)
	for _, inc := range all {
		// If task has an assigned agent, prefer incidents from that agent.
		if task.AssignedAgent != "" && inc.AgentID != task.AssignedAgent {
			continue
		}
		sum := fmt.Sprintf("[%s] %s: %s", inc.Severity, inc.AgentID, inc.Description)
		trimmed := trimToBudget(sum, scb, 0)
		if trimmed == "" {
			break
		}
		tokens := EstimateTokens(trimmed)
		if !cb.Consume(tokens) {
			return
		}
		ctx.IncidentHistory = append(ctx.IncidentHistory, trimmed)
	}
	// If we filtered by agent and got nothing, include all incidents (agent-agnostic fallback).
	if len(ctx.IncidentHistory) == 0 && task.AssignedAgent != "" {
		for _, inc := range all {
			sum := fmt.Sprintf("[%s] %s: %s", inc.Severity, inc.AgentID, inc.Description)
			trimmed := trimToBudget(sum, scb, 0)
			if trimmed == "" {
				break
			}
			tokens := EstimateTokens(trimmed)
			if !cb.Consume(tokens) {
				return
			}
			ctx.IncidentHistory = append(ctx.IncidentHistory, trimmed)
		}
	}
}

// trimToBudget truncates s to fit within the given token budget.
// When minTokens is 0, a single token is the minimum.
func trimToBudget(s string, budget *ContextBudget, _ int) string {
	if s == "" {
		return ""
	}
	// How many tokens can we afford?
	avail := budget.Remaining()
	if avail <= 0 {
		return ""
	}
	// Each token ≈ 4 chars; compute max chars before we overrun.
	maxChars := avail * 4
	if maxChars > len(s) {
		maxChars = len(s)
	}
	// Try to break at a word boundary.
	for maxChars > 0 && maxChars < len(s) && s[maxChars] != ' ' {
		maxChars--
	}
	if maxChars == 0 {
		return ""
	}
	result := s[:maxChars]
	tokens := EstimateTokens(result)
	if tokens > avail {
		tokens = avail
		result = s[:tokens*4]
	}
	budget.Consume(tokens)
	return result
}

func statusRank(s string) int {
	switch strings.ToLower(s) {
	case "accepted":
		return 0
	case "proposed":
		return 1
	case "superseded":
		return 2
	case "deprecated":
		return 3
	default:
		return 4
	}
}
