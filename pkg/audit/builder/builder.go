// Package builder implements the producer-side of the 12-step audit
// trail per specs/SPECIFICATION.md §6.5 (Audit Trail Requirements) and
// §2.2 (Step-by-Step State Transitions).
//
// pkg/audit/chain.go is the consumer side — given a fully-populated
// AuditEvidence, it validates each of the 12 steps. This package is the
// producer side — given partial evidence collected during a Helix run,
// it assembles an AuditEvidence incrementally and lets callers inspect
// what's still missing at any point.
//
// Usage pattern (assembling a chain as a run progresses):
//
//	b := builder.New("42")                                  // PR #42
//	b.Issue("https://forgejo/...", "alice", time.Now(), "Fix bug")
//	b.AxiomWorkItem("agent-1", "wi-007", "run-9", "/path")
//	b.Lock("lock-3", "/worktree", time.Now())               // still held
//	// ... continue collecting evidence ...
//	ev := b.Build()
//	missing := b.MissingSteps()                              // what's left
//	status := ev.Validate()                                  // or via chain.go
//
// The builder is intentionally side-effect-free: it does not call out
// to any service. Callers populate fields from their own probes, the
// builder packages them into a complete AuditEvidence ready for
// persistence or validation.
package builder

import (
	"fmt"
	"sort"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/audit"
)

// =============================================================================
// Builder
// =============================================================================

// Builder is a fluent assembler for audit.AuditEvidence. The zero value
// is NOT usable — always construct via New. The builder is safe for
// sequential use within a single goroutine; concurrent calls require
// external synchronization.
type Builder struct {
	prRef string
	ev    *audit.AuditEvidence
}

// New returns a Builder pre-populated with the supplied PR reference.
// prRef is used only as a label in MissingSteps output; it does not
// appear inside the produced AuditEvidence (that field is set per-step).
func New(prRef string) *Builder {
	return &Builder{
		prRef: prRef,
		ev:    &audit.AuditEvidence{},
	}
}

// Build returns the underlying AuditEvidence. The returned pointer
// aliases the builder's internal state; subsequent calls to any setter
// mutate the same struct. Callers that need a deep copy should clone
// the result.
func (b *Builder) Build() *audit.AuditEvidence {
	return b.ev
}

// =============================================================================
// Per-step setters (fluent)
// =============================================================================
//
// Each setter returns the builder so calls can be chained. Setters are
// no-ops when given zero values (empty strings, zero times, nil slices)
// to make it safe to wire up from uninitialized upstream signals.

// Issue records Step 1 (Forgejo Issue) evidence.
func (b *Builder) Issue(url, creator string, ts time.Time, title string) *Builder {
	if url == "" && creator == "" && title == "" && ts.IsZero() {
		return b
	}
	b.ev.ForgejoIssue = &audit.ForgejoIssueEvidence{
		IssueURL:  url,
		Creator:   creator,
		Timestamp: ts,
		Title:     title,
	}
	return b
}

// AxiomWorkItem records Step 2 (Axiom Work Item) evidence. Multiple
// agents can be assigned; this setter replaces the previous list.
func (b *Builder) AxiomWorkItem(workItemID, runID, planYAMLRef string, agentIDs ...string) *Builder {
	if workItemID == "" && runID == "" && planYAMLRef == "" && len(agentIDs) == 0 {
		return b
	}
	b.ev.AxiomWorkItem = &audit.AxiomWorkItemEvidence{
		PlanYAMLRef: planYAMLRef,
		AgentIDs:    append([]string{}, agentIDs...),
		RunID:       runID,
		WorkItemID:  workItemID,
	}
	return b
}

// Lock records Step 3 (Ralph Loop) lock acquisition. LockReleasedAt
// defaults to zero ("still held"). Use Release() to record release.
func (b *Builder) Lock(lockID, worktreePath string, acquiredAt time.Time) *Builder {
	if lockID == "" && worktreePath == "" && acquiredAt.IsZero() {
		return b
	}
	b.ev.RalphLoop = &audit.RalphLoopEvidence{
		LockID:         lockID,
		WorktreePath:   worktreePath,
		LockAcquiredAt: acquiredAt,
	}
	return b
}

// Release records the Ralph Loop lock release timestamp.
func (b *Builder) Release(releasedAt time.Time) *Builder {
	if b.ev.RalphLoop == nil {
		return b
	}
	b.ev.RalphLoop.LockReleasedAt = releasedAt
	return b
}

// Session records Step 4 (OpenCode session) evidence.
func (b *Builder) Session(sessionID, model, langfuseTraceID string, tokensIn, tokensOut int64, costUSD float64) *Builder {
	if sessionID == "" && model == "" && langfuseTraceID == "" && tokensIn == 0 && tokensOut == 0 && costUSD == 0 {
		return b
	}
	b.ev.OpenCodeSession = &audit.OpenCodeSessionEvidence{
		SessionID:       sessionID,
		Model:           model,
		TokensInput:     tokensIn,
		TokensOutput:    tokensOut,
		CostUSD:         costUSD,
		LangFuseTraceID: langfuseTraceID,
	}
	return b
}

// Commit records Step 5 (Git commit) attestation evidence.
// attestationFound indicates whether the Helix-Attestation trailer was
// present; the hashes + agent metadata are recorded separately.
func (b *Builder) Commit(sha, promptHash, model, contextHash, agentID string, confidence int, costUSD float64, attestationFound bool) *Builder {
	if sha == "" && promptHash == "" && model == "" && agentID == "" {
		return b
	}
	b.ev.GitCommit = &audit.GitCommitEvidence{
		SHA:              sha,
		AttestationFound: attestationFound,
		PromptHash:       promptHash,
		Model:            model,
		ContextHash:      contextHash,
		AgentID:          agentID,
		Confidence:       confidence,
		CostUSD:          costUSD,
	}
	return b
}

// Verdict records Step 6 (GitReins verdict) evidence. tier2Verdict
// should be "COMPLETE" or "INCOMPLETE".
func (b *Builder) Verdict(tier1Passed bool, tier1Checks []string, tier2Verdict string, tier2Findings int, verdictTime time.Time) *Builder {
	if !verdictTime.IsZero() || tier2Verdict != "" || len(tier1Checks) > 0 {
		b.ev.GitReinsVerdict = &audit.GitReinsVerdictEvidence{
			Tier1Passed:   tier1Passed,
			Tier1Checks:   append([]string{}, tier1Checks...),
			Tier2Verdict:  tier2Verdict,
			Tier2Findings: tier2Findings,
			VerdictTime:   verdictTime,
		}
	}
	return b
}

// PR records Step 7 (PR metadata) evidence.
func (b *Builder) PR(prIndex int, linkedIssueURL, specRef, evidenceBundleID string) *Builder {
	if prIndex == 0 && linkedIssueURL == "" && specRef == "" && evidenceBundleID == "" {
		return b
	}
	b.ev.PRMetadata = &audit.PRMetadataEvidence{
		PRIndex:          prIndex,
		LinkedIssueURL:   linkedIssueURL,
		SpecRef:          specRef,
		EvidenceBundleID: evidenceBundleID,
	}
	return b
}

// ChimeraReview records Step 8 (Chimera review) evidence.
func (b *Builder) ChimeraReview(traceID, formation, verdict string, workerModels []string, findings int, score float64) *Builder {
	if traceID == "" && formation == "" && verdict == "" && len(workerModels) == 0 {
		return b
	}
	b.ev.ChimeraReview = &audit.ChimeraReviewEvidence{
		TraceID:      traceID,
		Formation:    formation,
		WorkerModels: append([]string{}, workerModels...),
		Verdict:      verdict,
		Findings:     findings,
		Score:        score,
	}
	return b
}

// Conscientiousness records Step 9 (Conscientiousness report) evidence.
func (b *Builder) Conscientiousness(reportID, verdict string, attackVectors []string, mitigations int) *Builder {
	if reportID == "" && verdict == "" && len(attackVectors) == 0 {
		return b
	}
	b.ev.Conscientiousness = &audit.ConscientiousnessEvidence{
		ReportID:      reportID,
		AttackVectors: append([]string{}, attackVectors...),
		Verdict:       verdict,
		Mitigations:   mitigations,
	}
	return b
}

// PromptFoo records Step 10 (PromptFoo CI) evidence.
func (b *Builder) PromptFoo(total, passed, failed int, actionsRunID string, results []audit.PromptFooResult) *Builder {
	if total == 0 && passed == 0 && failed == 0 && actionsRunID == "" && len(results) == 0 {
		return b
	}
	b.ev.PromptFooCI = &audit.PromptFooCIEvidence{
		TotalTests:   total,
		PassedTests:  passed,
		FailedTests:  failed,
		ActionsRunID: actionsRunID,
		Results:      append([]audit.PromptFooResult{}, results...),
	}
	return b
}

// CoApprovals records Step 11 (Co-approvals) evidence. Both human and
// agent approvals are recorded separately via pointers so an absent
// approval is distinguishable from a zero-valued one.
func (b *Builder) CoApprovals(human, agent *audit.ApprovalRecord) *Builder {
	if human == nil && agent == nil {
		return b
	}
	b.ev.CoApprovals = &audit.CoApprovalEvidence{
		HumanApproval: human,
		AgentApproval: agent,
	}
	return b
}

// Merge records Step 12 (final merge) evidence.
func (b *Builder) Merge(mergeSHA, strategy, pagesURL, langfuseTraceID string, ts time.Time) *Builder {
	if mergeSHA == "" && strategy == "" && pagesURL == "" && langfuseTraceID == "" && ts.IsZero() {
		return b
	}
	b.ev.Merge = &audit.MergeEvidence{
		MergeSHA:        mergeSHA,
		Strategy:        strategy,
		Timestamp:       ts,
		PagesURL:        pagesURL,
		LangFuseTraceID: langfuseTraceID,
	}
	return b
}

// =============================================================================
// Inspection
// =============================================================================

// MissingSteps returns the audit.StepID values for which the builder
// has no evidence. The list is sorted in step order (1, 2, ..., 12)
// for stable CLI output.
func (b *Builder) MissingSteps() []audit.StepID {
	var missing []audit.StepID
	for _, id := range audit.AllSteps() {
		if b.ev.StepEvidence(id) == nil {
			missing = append(missing, id)
		}
	}
	return missing
}

// PresentSteps returns the audit.StepID values for which the builder
// has evidence, in step order.
func (b *Builder) PresentSteps() []audit.StepID {
	var present []audit.StepID
	for _, id := range audit.AllSteps() {
		if b.ev.StepEvidence(id) != nil {
			present = append(present, id)
		}
	}
	return present
}

// Completion returns the number of populated steps out of 12 (e.g. 7/12).
// Convenience for callers that want to display progress.
func (b *Builder) Completion() (present, total int) {
	return len(b.PresentSteps()), len(audit.AllSteps())
}

// IsComplete reports whether every step has evidence.
func (b *Builder) IsComplete() bool {
	return len(b.MissingSteps()) == 0
}

// =============================================================================
// Formatting
// =============================================================================

// FormatProgress renders a human-readable summary of the builder's
// current state, suitable for `helix audit chain <pr>` CLI output.
func (b *Builder) FormatProgress() string {
	present := b.PresentSteps()
	missing := b.MissingSteps()
	var s string
	s += fmt.Sprintf("Audit chain progress (PR %s): %d/%d complete\n",
		b.prRef, len(present), len(audit.AllSteps()))
	s += "\nPresent:\n"
	if len(present) == 0 {
		s += "  (none)\n"
	} else {
		for _, id := range present {
			s += fmt.Sprintf("  ✓ Step %d: %s\n", id, audit.StepName(id))
		}
	}
	s += "\nMissing:\n"
	if len(missing) == 0 {
		s += "  (all steps present)\n"
	} else {
		// Sort for stable output.
		sort.Slice(missing, func(i, j int) bool { return missing[i] < missing[j] })
		for _, id := range missing {
			s += fmt.Sprintf("  ✗ Step %d: %s\n", id, audit.StepName(id))
		}
	}
	return s
}
