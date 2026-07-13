package dispatcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

// -----------------------------------------------------------------------------
// ContextPackage and related types (spec §3.3)
//
// An agent receives a finite token-budgeted snapshot of everything it needs
// to do the work: the spec section for its task, the acceptance criteria as
// pseudo-sentences from the description, prior decisions (ADRs), prior
// related PRs, historical incidents, and the most relevant code files. If
// the package would exceed the budget, the leftover resources are exposed
// as on-demand "expandable" items the agent may request.
// -----------------------------------------------------------------------------

// ContextPackage is the assembled context handed to an agent for one task.
type ContextPackage struct {
	TaskID             string                `json:"task_id"`
	AgentID            string                `json:"agent_id"`
	SpecSection        ContextResource       `json:"spec_section"`
	AcceptanceCriteria []ContextResource     `json:"acceptance_criteria"`
	ADRs               []ContextResource     `json:"adrs"`
	PriorPRs           []ContextResource     `json:"prior_prs"`
	Incidents          []ContextResource     `json:"incidents"`
	CodeFiles          []ContextResource     `json:"code_files"`
	TotalTokens        int64                 `json:"total_tokens_est"`
	TokenBudget        int64                 `json:"token_budget"`
	Expandable         []ExpandableResource  `json:"expandable"`
}

// ContextResource is a single bounded piece of context with an estimated
// token count and a stable identifier (SourceHash) for caching.
type ContextResource struct {
	Type       string `json:"type"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	TokenEst   int64  `json:"token_est"`
	SourceHash string `json:"source_hash"`
}

// ExpandableResource is a resource available on agent request. It is NOT
// included in the initial package; the agent can ask for it explicitly when
// it needs more context.
type ExpandableResource struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	TokenEst  int64   `json:"token_est"`
	TokenCost float64 `json:"token_cost_usd"`
}

// -----------------------------------------------------------------------------
// Tier-gated token budgets
// -----------------------------------------------------------------------------

// ContextBudget maps each trust tier to the maximum token count the initial
// context package may consume. Veteran agents receive the largest budget; the
// tiers ascend by 2x.
var contextBudget = map[trust.TrustTier]int64{
	trust.TierProvisional: 12000,
	trust.TierObserved:    24000,
	trust.TierTrusted:     48000,
	trust.TierVeteran:     96000,
}

// TokenBudgetForTier returns the token budget for tier. Unknown tiers fall
// back to Provisional so a newly-introduced tier never silently overloads an
// agent's window.
func TokenBudgetForTier(tier trust.TrustTier) int64 {
	if b, ok := contextBudget[tier]; ok {
		return b
	}
	return contextBudget[trust.TierProvisional]
}

// -----------------------------------------------------------------------------
// Public API
// -----------------------------------------------------------------------------

// ContextSources groups the repositories and documents AssembleContext reads
// to fill the package. A zero value is valid and produces a minimal package
// containing only the always-included spec section and pseudo-ACs.
type ContextSources struct {
	// RepoPath is the codebase root to search for code matches. Empty
	// means "do not include code files in the package".
	RepoPath string
	// IgnorePatterns are passed through to IndexRepo.
	IgnorePatterns []string
	// TopFiles limits how many matched code files we try to pull into
	// the package. Zero defaults to 5.
	TopFiles int
	// ADRGlob is the glob path/pattern for architecture decision records
	// (e.g. "docs/adr/*.md"). Empty means no ADRs.
	ADRGlob string
	// PRGlob is the glob path/pattern for prior PR summaries.
	PRGlob string
	// IncidentGlob is the glob path/pattern for historical incidents.
	IncidentGlob string
}

// AssembleContext builds a ContextPackage for the given task on the given
// agent within budgetTokens tokens. Pass 0 to use the agent tier's default.
//
// The algorithm (spec §3.3):
//  1. Always-included: the spec section referenced by task.SpecRef, plus
//     pseudo-acceptance-criteria derived from task.Description sentences.
//  2. Optional-included (skipped when over budget): ADRs, prior PRs,
//     incidents, and the top-N code files matching the task description by
//     coverage score.
//  3. Expandable (never in the initial package): the full codebase index, all
//     incidents, all prior PRs. The agent can opt in to them.
//
// Resources added to Expandable still carry their TokenEst so the agent can
// estimate the cost of pulling them in.
func AssembleContext(task Task, agent AgentProfile, budgetTokens int64, sources ContextSources) (*ContextPackage, error) {
	budget := budgetTokens
	if budget <= 0 {
		budget = TokenBudgetForTier(agent.Tier)
	}

	pkg := &ContextPackage{
		TaskID:      task.ID,
		AgentID:     agent.Name,
		TokenBudget: budget,
	}

	// 1. Always-included: spec section.
	spec, err := loadSpecSection(task.SpecRef)
	if err == nil && spec != nil {
		pkg.SpecSection = *spec
		pkg.TotalTokens += spec.TokenEst
	}
	// Missing spec is non-fatal; we still emit a placeholder so the
	// package stays usable for dry-runs.
	if pkg.SpecSection.Type == "" {
		pkg.SpecSection = ContextResource{
			Type:       "spec_section",
			Title:      trimTo(task.SpecRef, 80),
			Content:    task.Description,
			TokenEst:   estTokens(task.Description),
			SourceHash: hashString(task.SpecRef),
		}
		pkg.TotalTokens += pkg.SpecSection.TokenEst
	}

	// 2. Always-included pseudo-ACs from the task description.
	pkg.AcceptanceCriteria = acceptanceCriteriaFrom(task.Description)
	for _, ac := range pkg.AcceptanceCriteria {
		pkg.TotalTokens += ac.TokenEst
	}

	// Helper for budgeted include. Returns true if the resource was added
	// inline (within budget), false if it was demoted to Expandable.
	add := func(res ContextResource, expandTitle string) bool {
		if pkg.TotalTokens+res.TokenEst > budget {
			pkg.Expandable = append(pkg.Expandable, ExpandableResource{
				ID:        res.SourceHash,
				Title:     expandTitle,
				TokenEst:  res.TokenEst,
				TokenCost: estimateCost(res.TokenEst),
			})
			return false
		}
		return true
	}

	// 3. ADRs, prior PRs, incidents.
	if sources.ADRGlob != "" {
		for _, res := range loadMarkedFiles(sources.ADRGlob, "adr") {
			if add(res, "ADR: "+res.Title) {
				pkg.ADRs = append(pkg.ADRs, res)
				pkg.TotalTokens += res.TokenEst
			}
		}
	}
	if sources.PRGlob != "" {
		for _, res := range loadMarkedFiles(sources.PRGlob, "prior_pr") {
			if add(res, "Prior PR: "+res.Title) {
				pkg.PriorPRs = append(pkg.PriorPRs, res)
				pkg.TotalTokens += res.TokenEst
			}
		}
	}
	if sources.IncidentGlob != "" {
		for _, res := range loadMarkedFiles(sources.IncidentGlob, "incident") {
			if add(res, "Incident: "+res.Title) {
				pkg.Incidents = append(pkg.Incidents, res)
				pkg.TotalTokens += res.TokenEst
			}
		}
	}

	// 4. Code files matched by coverage from the codebase index.
	if sources.RepoPath != "" {
		idx, err := IndexRepo(sources.RepoPath, sources.IgnorePatterns)
		if err == nil && idx != nil {
			top := sources.TopFiles
			if top <= 0 {
				top = 5
			}
			matches := idx.Search(task.Description, top)
			for _, path := range matches {
				f := idx.Files[path]
				res := ContextResource{
					Type:       "code_file",
					Title:      path,
					Content:    strings.Join(f.Tokens, " "),
					TokenEst:   f.TokenCount,
					SourceHash: hashString(path),
				}
				if add(res, "Code file: "+res.Title) {
					pkg.CodeFiles = append(pkg.CodeFiles, res)
					pkg.TotalTokens += res.TokenEst
				}
			}
			// Always expose the full index as one expandable item so an
			// agent that wants exhaustive search can ask for it.
			pkg.Expandable = append(pkg.Expandable, ExpandableResource{
				ID:        hashString(sources.RepoPath + "::full-index"),
				Title:     "Full codebase index",
				TokenEst:  int64(idxTotalTokens(idx)),
				TokenCost: 0,
			})
		}
	}

	return pkg, nil
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// estTokens estimates tokens via the standard 4-chars-per-token heuristic.
func estTokens(content string) int64 {
	if content == "" {
		return 0
	}
	return int64(len(content) / 4)
}

// estimateCost returns a rough USD cost for a token count at $0.001/1k tokens.
// Used for ExpandableResource previews; the real billing path uses
// pkg/estimate.Estimator.
func estimateCost(tokens int64) float64 {
	if tokens <= 0 {
		return 0
	}
	return float64(tokens) / 1000.0 * 0.001
}

// loadSpecSection returns the spec section referenced by specRef. Currently
// reads the whole spec file as one section; spec-slice-by-heading support is
// a follow-up. A missing spec returns an error so the caller can fall back
// to a placeholder.
func loadSpecSection(specRef string) (*ContextResource, error) {
	if specRef == "" {
		return nil, fmt.Errorf("context: empty spec ref")
	}
	data, err := os.ReadFile(specRef)
	if err != nil {
		return nil, fmt.Errorf("context: read spec %s: %w", specRef, err)
	}
	content := string(data)
	return &ContextResource{
		Type:       "spec_section",
		Title:      trimTo(specRef, 80),
		Content:    content,
		TokenEst:   estTokens(content),
		SourceHash: hashString(content),
	}, nil
}

// acceptanceCriteriaFrom splits desc by sentence-like periods and emits one
// ContextResource per non-empty fragment. Title is the first 60 chars of each
// fragment.
func acceptanceCriteriaFrom(desc string) []ContextResource {
	if strings.TrimSpace(desc) == "" {
		return nil
	}
	// Normalize newlines to sentence separators.
	text := strings.ReplaceAll(desc, "\n", ". ")
	parts := strings.Split(text, ". ")
	var out []ContextResource
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		title := trimTo(p, 60)
		out = append(out, ContextResource{
			Type:       "acceptance_criterion",
			Title:      title,
			Content:    p,
			TokenEst:   estTokens(p),
			SourceHash: hashString(p),
		})
	}
	return out
}

// loadMarkedFiles reads every file matching glob and emits them as typed
// ContextResources. A glob that doesn't match anything yields no resources
// (and no error) — the caller can distinguish "no ADR docs yet" from "walk
// error" via ExpandableResource accumulation downstream.
func loadMarkedFiles(glob, typ string) []ContextResource {
	paths, err := filepath.Glob(glob)
	if err != nil {
		return nil
	}
	var out []ContextResource
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		content := string(data)
		out = append(out, ContextResource{
			Type:       typ,
			Title:      trimTo(p, 80),
			Content:    content,
			TokenEst:   estTokens(content),
			SourceHash: hashString(content),
		})
	}
	return out
}

// idxTotalTokens sums token counts across the entire index. Used only for
// the "Full codebase index" expandable preview.
func idxTotalTokens(idx *CodebaseIndex) int64 {
	if idx == nil {
		return 0
	}
	var total int64
	for _, f := range idx.Files {
		total += f.TokenCount
	}
	return total
}

// trimTo truncates s to n chars (with an ellipsis when exceeded). Used so
// titles stay readable in ExpandableResource listings.
func trimTo(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}

// hashString is a deterministic, allocation-light stable identifier. We avoid
// crypto hashes here; stability matters more than collision resistance for
// in-memory cache lookup.
func hashString(s string) string {
	var h uint64 = 1469598103934665603
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return fmt.Sprintf("%016x", h)
}
