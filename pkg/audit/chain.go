// Package audit implements the 12-step audit trail checker per spec §6.5.
//
// For any merged PR, an auditor MUST be able to trace evidence through all 12
// steps of the Helix pipeline. Missing evidence at any step is an audit
// failure — the merge is flagged for review.
//
// The checker is a pure Go composition layer: it takes structured evidence
// from each pipeline stage (already produced by other Helix packages) and
// verifies completeness. It does NOT make live API calls — callers supply
// the evidence, the checker validates it.
package audit

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// =============================================================================
// Step Identifiers
// =============================================================================

// StepID identifies a step in the 12-step audit chain.
type StepID int

const (
	StepForgejoIssue      StepID = 1
	StepAxiomWorkItem     StepID = 2
	StepRalphLoop         StepID = 3
	StepOpenCodeSession   StepID = 4
	StepGitCommit         StepID = 5
	StepGitReinsVerdict   StepID = 6
	StepPRMetadata        StepID = 7
	StepChimeraReview     StepID = 8
	StepConscientiousness StepID = 9
	StepPromptFooCI       StepID = 10
	StepCoApprovals       StepID = 11
	StepMerge             StepID = 12
)

// StepName returns the human-readable name for a step.
func StepName(id StepID) string {
	names := map[StepID]string{
		StepForgejoIssue:      "Forgejo Issue",
		StepAxiomWorkItem:     "Axiom Work Item",
		StepRalphLoop:         "Ralph Loop",
		StepOpenCodeSession:   "OpenCode Session",
		StepGitCommit:         "Git Commit",
		StepGitReinsVerdict:   "GitReins Verdict",
		StepPRMetadata:        "PR Metadata",
		StepChimeraReview:     "Chimera Review",
		StepConscientiousness: "Conscientiousness Report",
		StepPromptFooCI:       "PromptFoo CI",
		StepCoApprovals:       "Co-Approvals",
		StepMerge:             "Merge",
	}
	if name, ok := names[id]; ok {
		return name
	}
	return fmt.Sprintf("Step %d", id)
}

// AllSteps returns all 12 step IDs in order.
func AllSteps() []StepID {
	return []StepID{
		StepForgejoIssue,
		StepAxiomWorkItem,
		StepRalphLoop,
		StepOpenCodeSession,
		StepGitCommit,
		StepGitReinsVerdict,
		StepPRMetadata,
		StepChimeraReview,
		StepConscientiousness,
		StepPromptFooCI,
		StepCoApprovals,
		StepMerge,
	}
}

// StepDescription returns a brief description of what evidence a step requires,
// matching the spec §6.5 12-step audit chain.
func StepDescription(id StepID) string {
	descs := map[StepID]string{
		StepForgejoIssue:      "Forgejo issue URL + creator + timestamp",
		StepAxiomWorkItem:     "plan.yaml ref + agent assignments + run_id",
		StepRalphLoop:         "lock_id + worktree_path + lock_acquired_at",
		StepOpenCodeSession:   "session_id + model + tokens + cost (LangFuse trace)",
		StepGitCommit:         "SHA + attestation trailer (prompt-hash, model, context-hash)",
		StepGitReinsVerdict:   "Tier 1 guard results + Tier 2 verdict (COMPLETE/INCOMPLETE)",
		StepPRMetadata:        "pr_index + linked issue + spec ref + evidence bundle",
		StepChimeraReview:     "trace_id + formation + worker models + verdict + findings",
		StepConscientiousness: "report_id + attack vectors + verdict (DEFENSIBLE/VULNERABLE)",
		StepPromptFooCI:       "test results (pass/fail per test case) + Forgejo Actions run ID",
		StepCoApprovals:       "human approval (user + timestamp) + agent approval (agent + confidence)",
		StepMerge:             "merge SHA + strategy + timestamp + Pages URL + LangFuse trace ID",
	}
	if d, ok := descs[id]; ok {
		return d
	}
	return fmt.Sprintf("Step %d", id)
}

// =============================================================================
// Evidence Types — One Per Step
// =============================================================================

// ForgejoIssueEvidence corresponds to Step 1.
type ForgejoIssueEvidence struct {
	IssueURL  string    // URL to the Forgejo issue
	Creator   string    // Username of the issue creator
	Timestamp time.Time // When the issue was created
	Title     string    // Issue title
}

// AxiomWorkItemEvidence corresponds to Step 2.
type AxiomWorkItemEvidence struct {
	PlanYAMLRef string   // Reference to plan.yaml
	AgentIDs    []string // Assigned agent IDs
	RunID       string   // Axiom run ID
	WorkItemID  string   // Work item identifier
}

// RalphLoopEvidence corresponds to Step 3.
type RalphLoopEvidence struct {
	LockID         string    // Ralph Loop lock identifier
	WorktreePath   string    // Path to the worktree
	LockAcquiredAt time.Time // When the lock was acquired
	LockReleasedAt time.Time // When the lock was released (zero = still held)
}

// OpenCodeSessionEvidence corresponds to Step 4.
type OpenCodeSessionEvidence struct {
	SessionID       string  // OpenCode session ID
	Model           string  // Model used for the session
	TokensInput     int64   // Input tokens consumed
	TokensOutput    int64   // Output tokens consumed
	CostUSD         float64 // Dollar cost of the session
	LangFuseTraceID string  // LangFuse trace ID for the session
}

// GitCommitEvidence corresponds to Step 5.
type GitCommitEvidence struct {
	SHA              string  // Git commit SHA
	AttestationFound bool    // Whether Helix-Attestation trailer was present
	PromptHash       string  // sha256 hash of the prompt
	Model            string  // Model that generated the code
	ContextHash      string  // sha256 hash of the context
	AgentID          string  // Agent identity
	Confidence       int     // Confidence score (0-100)
	CostUSD          float64 // Cost of the commit
}

// GitReinsVerdictEvidence corresponds to Step 6.
type GitReinsVerdictEvidence struct {
	Tier1Passed   bool      // Whether Tier 1 guards passed
	Tier1Checks   []string  // List of Tier 1 check names
	Tier2Verdict  string    // COMPLETE or INCOMPLETE
	Tier2Findings int       // Number of findings from Tier 2
	VerdictTime   time.Time // When the verdict was issued
}

// PRMetadataEvidence corresponds to Step 7.
type PRMetadataEvidence struct {
	PRIndex          int    // PR number
	LinkedIssueURL   string // URL to the linked issue
	SpecRef          string // Reference to the spec file
	EvidenceBundleID string // ID of the evidence bundle
}

// ChimeraReviewEvidence corresponds to Step 8.
type ChimeraReviewEvidence struct {
	TraceID      string   // Chimera trace ID
	Formation    string   // Formation name used
	WorkerModels []string // List of worker model IDs
	Verdict      string   // APPROVE, REJECT, or ESCALATE
	Findings     int      // Number of findings
	Score        float64  // Consensus score (0-1)
}

// ConscientiousnessEvidence corresponds to Step 9.
type ConscientiousnessEvidence struct {
	ReportID      string   // Conscientiousness report ID
	AttackVectors []string // List of attack vectors tested
	Verdict       string   // DEFENSIBLE or VULNERABLE
	Mitigations   int      // Number of mitigations suggested
}

// PromptFooCIEvidence corresponds to Step 10.
type PromptFooCIEvidence struct {
	TotalTests   int               // Total PromptFoo test cases
	PassedTests  int               // Number of passing test cases
	FailedTests  int               // Number of failing test cases
	ActionsRunID string            // Forgejo Actions run ID
	Results      []PromptFooResult // Per-test results
}

// PromptFooResult is a single PromptFoo test case result.
type PromptFooResult struct {
	TestCase string  // Test case name
	Passed   bool    // Whether the test passed
	Model    string  // Model tested
	Variance float64 // Variance from baseline (0 = identical)
}

// CoApprovalEvidence corresponds to Step 11.
type CoApprovalEvidence struct {
	HumanApproval *ApprovalRecord // Human approval record
	AgentApproval *ApprovalRecord // Agent approval record
}

// ApprovalRecord is a single approval from human or agent.
type ApprovalRecord struct {
	Reviewer   string    // Username or agent ID
	TrustLevel int       // Trust level (0-100 for agents)
	Timestamp  time.Time // When the approval was given
}

// MergeEvidence corresponds to Step 12.
type MergeEvidence struct {
	MergeSHA        string    // Merge commit SHA
	Strategy        string    // Merge strategy (squash, merge, rebase)
	Timestamp       time.Time // When the merge occurred
	PagesURL        string    // Pages deployment URL
	LangFuseTraceID string    // Final LangFuse trace ID
}

// =============================================================================
// Audit Evidence Bundle
// =============================================================================

// AuditEvidence collects evidence for all 12 steps of a single PR.
type AuditEvidence struct {
	ForgejoIssue      *ForgejoIssueEvidence
	AxiomWorkItem     *AxiomWorkItemEvidence
	RalphLoop         *RalphLoopEvidence
	OpenCodeSession   *OpenCodeSessionEvidence
	GitCommit         *GitCommitEvidence
	GitReinsVerdict   *GitReinsVerdictEvidence
	PRMetadata        *PRMetadataEvidence
	ChimeraReview     *ChimeraReviewEvidence
	Conscientiousness *ConscientiousnessEvidence
	PromptFooCI       *PromptFooCIEvidence
	CoApprovals       *CoApprovalEvidence
	Merge             *MergeEvidence
}

// StepEvidence returns the evidence pointer for a given step ID.
// Returns nil (untyped) when the evidence is absent, so the caller
// can distinguish "not provided" from "provided but zero-valued".
func (a *AuditEvidence) StepEvidence(id StepID) interface{} {
	switch id {
	case StepForgejoIssue:
		if a.ForgejoIssue == nil {
			return nil
		}
		return a.ForgejoIssue
	case StepAxiomWorkItem:
		if a.AxiomWorkItem == nil {
			return nil
		}
		return a.AxiomWorkItem
	case StepRalphLoop:
		if a.RalphLoop == nil {
			return nil
		}
		return a.RalphLoop
	case StepOpenCodeSession:
		if a.OpenCodeSession == nil {
			return nil
		}
		return a.OpenCodeSession
	case StepGitCommit:
		if a.GitCommit == nil {
			return nil
		}
		return a.GitCommit
	case StepGitReinsVerdict:
		if a.GitReinsVerdict == nil {
			return nil
		}
		return a.GitReinsVerdict
	case StepPRMetadata:
		if a.PRMetadata == nil {
			return nil
		}
		return a.PRMetadata
	case StepChimeraReview:
		if a.ChimeraReview == nil {
			return nil
		}
		return a.ChimeraReview
	case StepConscientiousness:
		if a.Conscientiousness == nil {
			return nil
		}
		return a.Conscientiousness
	case StepPromptFooCI:
		if a.PromptFooCI == nil {
			return nil
		}
		return a.PromptFooCI
	case StepCoApprovals:
		if a.CoApprovals == nil {
			return nil
		}
		return a.CoApprovals
	case StepMerge:
		if a.Merge == nil {
			return nil
		}
		return a.Merge
	default:
		return nil
	}
}

// IsComplete returns true if all 12 steps have evidence present.
func (a *AuditEvidence) IsComplete() bool {
	return a.ForgejoIssue != nil &&
		a.AxiomWorkItem != nil &&
		a.RalphLoop != nil &&
		a.OpenCodeSession != nil &&
		a.GitCommit != nil &&
		a.GitReinsVerdict != nil &&
		a.PRMetadata != nil &&
		a.ChimeraReview != nil &&
		a.Conscientiousness != nil &&
		a.PromptFooCI != nil &&
		a.CoApprovals != nil &&
		a.Merge != nil
}

// CompletedSteps returns the IDs of steps that have evidence present.
func (a *AuditEvidence) CompletedSteps() []StepID {
	var steps []StepID
	if a.ForgejoIssue != nil {
		steps = append(steps, StepForgejoIssue)
	}
	if a.AxiomWorkItem != nil {
		steps = append(steps, StepAxiomWorkItem)
	}
	if a.RalphLoop != nil {
		steps = append(steps, StepRalphLoop)
	}
	if a.OpenCodeSession != nil {
		steps = append(steps, StepOpenCodeSession)
	}
	if a.GitCommit != nil {
		steps = append(steps, StepGitCommit)
	}
	if a.GitReinsVerdict != nil {
		steps = append(steps, StepGitReinsVerdict)
	}
	if a.PRMetadata != nil {
		steps = append(steps, StepPRMetadata)
	}
	if a.ChimeraReview != nil {
		steps = append(steps, StepChimeraReview)
	}
	if a.Conscientiousness != nil {
		steps = append(steps, StepConscientiousness)
	}
	if a.PromptFooCI != nil {
		steps = append(steps, StepPromptFooCI)
	}
	if a.CoApprovals != nil {
		steps = append(steps, StepCoApprovals)
	}
	if a.Merge != nil {
		steps = append(steps, StepMerge)
	}
	return steps
}

// =============================================================================
// Audit Report — Result of running the Checker
// =============================================================================

// StepResult is the audit result for a single step.
type StepResult struct {
	StepID   StepID
	StepName string
	Present  bool     // Whether evidence was provided at all
	Valid    bool     // Whether the evidence passed validation
	Issues   []string // Validation issues (empty if valid)
}

// AuditReport is the full 12-step audit result.
type AuditReport struct {
	PRIndex     int          // PR being audited
	Steps       []StepResult // One per step, in order
	AllPassed   bool         // Whether all 12 steps passed
	TotalIssues int          // Total validation issues across all steps
	AuditedAt   time.Time    // When the audit was performed
}

// FailedSteps returns the step IDs that failed audit.
func (r *AuditReport) FailedSteps() []StepID {
	var failed []StepID
	for _, s := range r.Steps {
		if !s.Valid {
			failed = append(failed, s.StepID)
		}
	}
	return failed
}

// MissingSteps returns the step IDs with no evidence at all.
func (r *AuditReport) MissingSteps() []StepID {
	var missing []StepID
	for _, s := range r.Steps {
		if !s.Present {
			missing = append(missing, s.StepID)
		}
	}
	return missing
}

// FormatReport renders the audit report as a human-readable string.
func (r *AuditReport) FormatReport() string {
	var b strings.Builder
	status := "PASS"
	if !r.AllPassed {
		status = "FAIL"
	}
	fmt.Fprintf(&b, "Audit Report — PR #%d — %s\n", r.PRIndex, status)
	fmt.Fprintf(&b, "Audited: %s\n\n", r.AuditedAt.Format(time.RFC3339))
	for _, s := range r.Steps {
		icon := "✓"
		if !s.Valid {
			icon = "✗"
		}
		fmt.Fprintf(&b, "  %s Step %d: %s", icon, s.StepID, s.StepName)
		if !s.Present {
			b.WriteString(" — MISSING\n")
		} else if s.Valid {
			b.WriteString(" — OK\n")
		} else {
			fmt.Fprintf(&b, " — %d issue(s)\n", len(s.Issues))
			for _, issue := range s.Issues {
				fmt.Fprintf(&b, "      • %s\n", issue)
			}
		}
	}
	if r.TotalIssues > 0 {
		fmt.Fprintf(&b, "\nTotal issues: %d\n", r.TotalIssues)
	}
	return b.String()
}

// Check validates all 12 steps of the audit evidence.
func (c *Checker) Check(prIndex int, evidence *AuditEvidence) *AuditReport {
	report := &AuditReport{
		PRIndex:   prIndex,
		AllPassed: true,
		AuditedAt: time.Now().UTC(),
	}

	for _, stepID := range AllSteps() {
		stepEvidence := evidence.StepEvidence(stepID)
		present := stepEvidence != nil

		var issues []string
		valid := true

		if present {
			if validator, ok := c.validators[stepID]; ok {
				issues = validator(stepEvidence)
				if len(issues) > 0 {
					valid = false
					report.AllPassed = false
				}
			}
		} else {
			issues = []string{"evidence not provided"}
			valid = false
			report.AllPassed = false
		}

		report.Steps = append(report.Steps, StepResult{
			StepID:   stepID,
			StepName: StepName(stepID),
			Present:  present,
			Valid:    valid,
			Issues:   issues,
		})
		report.TotalIssues += len(issues)
	}

	return report
}

// CheckStep validates a single step and returns its result.
func (c *Checker) CheckStep(stepID StepID, evidence interface{}) StepResult {
	present := evidence != nil
	var issues []string
	valid := true

	if present {
		if validator, ok := c.validators[stepID]; ok {
			issues = validator(evidence)
			if len(issues) > 0 {
				valid = false
			}
		}
	} else {
		issues = []string{"evidence not provided"}
		valid = false
	}

	return StepResult{
		StepID:   stepID,
		StepName: StepName(stepID),
		Present:  present,
		Valid:    valid,
		Issues:   issues,
	}
}

// =============================================================================
// Audit Ledger — Append-only log of audit results
// =============================================================================

// LedgerEntry is a single entry in the audit ledger.
type LedgerEntry struct {
	PRIndex     int       `json:"pr_index"`
	AuditResult string    `json:"audit_result"` // PASS or FAIL
	FailedSteps []StepID  `json:"failed_steps,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
	TotalIssues int       `json:"total_issues"`
}

// Ledger is an append-only log of audit results for traceability.
type Ledger struct {
	entries []LedgerEntry
}

// NewLedger creates a new empty audit ledger.
func NewLedger() *Ledger {
	return &Ledger{}
}

// Append adds an audit report to the ledger.
func (l *Ledger) Append(report *AuditReport) {
	entry := LedgerEntry{
		PRIndex:     report.PRIndex,
		AuditResult: "PASS",
		FailedSteps: report.FailedSteps(),
		Timestamp:   report.AuditedAt,
		TotalIssues: report.TotalIssues,
	}
	if !report.AllPassed {
		entry.AuditResult = "FAIL"
	}
	l.entries = append(l.entries, entry)
}

// Entries returns all ledger entries (sorted by timestamp).
func (l *Ledger) Entries() []LedgerEntry {
	sorted := make([]LedgerEntry, len(l.entries))
	copy(sorted, l.entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.Before(sorted[j].Timestamp)
	})
	return sorted
}

// PassRate returns the fraction of audits that passed (0-1).
func (l *Ledger) PassRate() float64 {
	if len(l.entries) == 0 {
		return 0
	}
	passed := 0
	for _, e := range l.entries {
		if e.AuditResult == "PASS" {
			passed++
		}
	}
	return float64(passed) / float64(len(l.entries))
}

// RecentFailures returns the N most recent failed audits.
func (l *Ledger) RecentFailures(n int) []LedgerEntry {
	var failures []LedgerEntry
	for i := len(l.entries) - 1; i >= 0 && len(failures) < n; i-- {
		if l.entries[i].AuditResult == "FAIL" {
			failures = append(failures, l.entries[i])
		}
	}
	return failures
}

// Len returns the number of entries in the ledger.
func (l *Ledger) Len() int {
	return len(l.entries)
}

// =============================================================================
// Audit Chain Builder — Constructs evidence from pipeline stages
// =============================================================================

// ChainBuilder helps assemble AuditEvidence from individual pipeline stages.
// Callers populate each step's evidence as it becomes available.
type ChainBuilder struct {
	evidence AuditEvidence
}

// NewChainBuilder creates a new ChainBuilder for a PR.
func NewChainBuilder() *ChainBuilder {
	return &ChainBuilder{}
}

// WithForgejoIssue sets Step 1 evidence.
func (b *ChainBuilder) WithForgejoIssue(e ForgejoIssueEvidence) *ChainBuilder {
	b.evidence.ForgejoIssue = &e
	return b
}

// WithAxiomWorkItem sets Step 2 evidence.
func (b *ChainBuilder) WithAxiomWorkItem(e AxiomWorkItemEvidence) *ChainBuilder {
	b.evidence.AxiomWorkItem = &e
	return b
}

// WithRalphLoop sets Step 3 evidence.
func (b *ChainBuilder) WithRalphLoop(e RalphLoopEvidence) *ChainBuilder {
	b.evidence.RalphLoop = &e
	return b
}

// WithOpenCodeSession sets Step 4 evidence.
func (b *ChainBuilder) WithOpenCodeSession(e OpenCodeSessionEvidence) *ChainBuilder {
	b.evidence.OpenCodeSession = &e
	return b
}

// WithGitCommit sets Step 5 evidence.
func (b *ChainBuilder) WithGitCommit(e GitCommitEvidence) *ChainBuilder {
	b.evidence.GitCommit = &e
	return b
}

// WithGitReinsVerdict sets Step 6 evidence.
func (b *ChainBuilder) WithGitReinsVerdict(e GitReinsVerdictEvidence) *ChainBuilder {
	b.evidence.GitReinsVerdict = &e
	return b
}

// WithPRMetadata sets Step 7 evidence.
func (b *ChainBuilder) WithPRMetadata(e PRMetadataEvidence) *ChainBuilder {
	b.evidence.PRMetadata = &e
	return b
}

// WithChimeraReview sets Step 8 evidence.
func (b *ChainBuilder) WithChimeraReview(e ChimeraReviewEvidence) *ChainBuilder {
	b.evidence.ChimeraReview = &e
	return b
}

// WithConscientiousness sets Step 9 evidence.
func (b *ChainBuilder) WithConscientiousness(e ConscientiousnessEvidence) *ChainBuilder {
	b.evidence.Conscientiousness = &e
	return b
}

// WithPromptFooCI sets Step 10 evidence.
func (b *ChainBuilder) WithPromptFooCI(e PromptFooCIEvidence) *ChainBuilder {
	b.evidence.PromptFooCI = &e
	return b
}

// WithCoApprovals sets Step 11 evidence.
func (b *ChainBuilder) WithCoApprovals(e CoApprovalEvidence) *ChainBuilder {
	b.evidence.CoApprovals = &e
	return b
}

// WithMerge sets Step 12 evidence.
func (b *ChainBuilder) WithMerge(e MergeEvidence) *ChainBuilder {
	b.evidence.Merge = &e
	return b
}

// Build returns the assembled audit evidence.
func (b *ChainBuilder) Build() *AuditEvidence {
	return &b.evidence
}

// IsComplete returns true if all 12 steps have evidence present.
func (b *ChainBuilder) IsComplete() bool {
	ev := &b.evidence
	return ev.ForgejoIssue != nil &&
		ev.AxiomWorkItem != nil &&
		ev.RalphLoop != nil &&
		ev.OpenCodeSession != nil &&
		ev.GitCommit != nil &&
		ev.GitReinsVerdict != nil &&
		ev.PRMetadata != nil &&
		ev.ChimeraReview != nil &&
		ev.Conscientiousness != nil &&
		ev.PromptFooCI != nil &&
		ev.CoApprovals != nil &&
		ev.Merge != nil
}

// CompletedSteps returns the IDs of steps that have evidence.
func (b *ChainBuilder) CompletedSteps() []StepID {
	ev := &b.evidence
	var steps []StepID
	if ev.ForgejoIssue != nil {
		steps = append(steps, StepForgejoIssue)
	}
	if ev.AxiomWorkItem != nil {
		steps = append(steps, StepAxiomWorkItem)
	}
	if ev.RalphLoop != nil {
		steps = append(steps, StepRalphLoop)
	}
	if ev.OpenCodeSession != nil {
		steps = append(steps, StepOpenCodeSession)
	}
	if ev.GitCommit != nil {
		steps = append(steps, StepGitCommit)
	}
	if ev.GitReinsVerdict != nil {
		steps = append(steps, StepGitReinsVerdict)
	}
	if ev.PRMetadata != nil {
		steps = append(steps, StepPRMetadata)
	}
	if ev.ChimeraReview != nil {
		steps = append(steps, StepChimeraReview)
	}
	if ev.Conscientiousness != nil {
		steps = append(steps, StepConscientiousness)
	}
	if ev.PromptFooCI != nil {
		steps = append(steps, StepPromptFooCI)
	}
	if ev.CoApprovals != nil {
		steps = append(steps, StepCoApprovals)
	}
	if ev.Merge != nil {
		steps = append(steps, StepMerge)
	}
	return steps
}
