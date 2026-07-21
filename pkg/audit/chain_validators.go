package audit

import "fmt"

// =============================================================================
// Step Validators
// =============================================================================

// StepValidator checks evidence for a single step and returns issues.
type StepValidator func(evidence interface{}) []string

// defaultValidators maps each step to its validation function.
func defaultValidators() map[StepID]StepValidator {
	return map[StepID]StepValidator{
		StepForgejoIssue:      validateForgejoIssue,
		StepAxiomWorkItem:     validateAxiomWorkItem,
		StepRalphLoop:         validateRalphLoop,
		StepOpenCodeSession:   validateOpenCodeSession,
		StepGitCommit:         validateGitCommit,
		StepGitReinsVerdict:   validateGitReinsVerdict,
		StepPRMetadata:        validatePRMetadata,
		StepChimeraReview:     validateChimeraReview,
		StepConscientiousness: validateConscientiousness,
		StepPromptFooCI:       validatePromptFooCI,
		StepCoApprovals:       validateCoApprovals,
		StepMerge:             validateMerge,
	}
}

func validateForgejoIssue(e interface{}) []string {
	v, ok := e.(*ForgejoIssueEvidence)
	if !ok || v == nil {
		return []string{"missing Forgejo issue evidence"}
	}
	var issues []string
	if v.IssueURL == "" {
		issues = append(issues, "issue URL is empty")
	}
	if v.Creator == "" {
		issues = append(issues, "creator is empty")
	}
	if v.Timestamp.IsZero() {
		issues = append(issues, "timestamp is zero")
	}
	if v.Title == "" {
		issues = append(issues, "title is empty")
	}
	return issues
}

func validateAxiomWorkItem(e interface{}) []string {
	v, ok := e.(*AxiomWorkItemEvidence)
	if !ok || v == nil {
		return []string{"missing Axiom work item evidence"}
	}
	var issues []string
	if v.PlanYAMLRef == "" {
		issues = append(issues, "plan.yaml reference is empty")
	}
	if len(v.AgentIDs) == 0 {
		issues = append(issues, "no agents assigned")
	}
	if v.RunID == "" {
		issues = append(issues, "run ID is empty")
	}
	if v.WorkItemID == "" {
		issues = append(issues, "work item ID is empty")
	}
	return issues
}

func validateRalphLoop(e interface{}) []string {
	v, ok := e.(*RalphLoopEvidence)
	if !ok || v == nil {
		return []string{"missing Ralph Loop evidence"}
	}
	var issues []string
	if v.LockID == "" {
		issues = append(issues, "lock ID is empty")
	}
	if v.WorktreePath == "" {
		issues = append(issues, "worktree path is empty")
	}
	if v.LockAcquiredAt.IsZero() {
		issues = append(issues, "lock acquired timestamp is zero")
	}
	return issues
}

func validateOpenCodeSession(e interface{}) []string {
	v, ok := e.(*OpenCodeSessionEvidence)
	if !ok || v == nil {
		return []string{"missing OpenCode session evidence"}
	}
	var issues []string
	if v.SessionID == "" {
		issues = append(issues, "session ID is empty")
	}
	if v.Model == "" {
		issues = append(issues, "model is empty")
	}
	if v.TokensInput == 0 && v.TokensOutput == 0 {
		issues = append(issues, "no tokens recorded")
	}
	if v.LangFuseTraceID == "" {
		issues = append(issues, "LangFuse trace ID is empty")
	}
	return issues
}

func validateGitCommit(e interface{}) []string {
	v, ok := e.(*GitCommitEvidence)
	if !ok || v == nil {
		return []string{"missing Git commit evidence"}
	}
	var issues []string
	if v.SHA == "" {
		issues = append(issues, "commit SHA is empty")
	}
	if !v.AttestationFound {
		issues = append(issues, "Helix-Attestation trailer not found")
	}
	if v.PromptHash == "" {
		issues = append(issues, "prompt hash is empty")
	}
	if v.Model == "" {
		issues = append(issues, "model is empty")
	}
	if v.AgentID == "" {
		issues = append(issues, "agent ID is empty")
	}
	if v.Confidence < 0 || v.Confidence > 100 {
		issues = append(issues, fmt.Sprintf("confidence %d out of range [0-100]", v.Confidence))
	}
	return issues
}

func validateGitReinsVerdict(e interface{}) []string {
	v, ok := e.(*GitReinsVerdictEvidence)
	if !ok || v == nil {
		return []string{"missing GitReins verdict evidence"}
	}
	var issues []string
	if !v.Tier1Passed {
		issues = append(issues, "Tier 1 guards did not pass")
	}
	if len(v.Tier1Checks) == 0 {
		issues = append(issues, "no Tier 1 checks recorded")
	}
	if v.Tier2Verdict == "" {
		issues = append(issues, "Tier 2 verdict is empty")
	}
	if v.Tier2Verdict != "" && v.Tier2Verdict != "COMPLETE" && v.Tier2Verdict != "INCOMPLETE" {
		issues = append(issues, fmt.Sprintf("invalid Tier 2 verdict %q", v.Tier2Verdict))
	}
	if v.VerdictTime.IsZero() {
		issues = append(issues, "verdict timestamp is zero")
	}
	return issues
}

func validatePRMetadata(e interface{}) []string {
	v, ok := e.(*PRMetadataEvidence)
	if !ok || v == nil {
		return []string{"missing PR metadata evidence"}
	}
	var issues []string
	if v.PRIndex == 0 {
		issues = append(issues, "PR index is zero")
	}
	if v.LinkedIssueURL == "" {
		issues = append(issues, "linked issue URL is empty")
	}
	if v.SpecRef == "" {
		issues = append(issues, "spec reference is empty")
	}
	if v.EvidenceBundleID == "" {
		issues = append(issues, "evidence bundle ID is empty")
	}
	return issues
}

func validateChimeraReview(e interface{}) []string {
	v, ok := e.(*ChimeraReviewEvidence)
	if !ok || v == nil {
		return []string{"missing Chimera review evidence"}
	}
	var issues []string
	if v.TraceID == "" {
		issues = append(issues, "trace ID is empty")
	}
	if v.Formation == "" {
		issues = append(issues, "formation is empty")
	}
	if len(v.WorkerModels) == 0 {
		issues = append(issues, "no worker models recorded")
	}
	if len(v.WorkerModels) < 3 {
		issues = append(issues, fmt.Sprintf("only %d worker models (spec requires 3+)", len(v.WorkerModels)))
	}
	if v.Verdict == "" {
		issues = append(issues, "verdict is empty")
	}
	if v.Score < 0 || v.Score > 1 {
		issues = append(issues, fmt.Sprintf("score %.2f out of range [0-1]", v.Score))
	}
	return issues
}

func validateConscientiousness(e interface{}) []string {
	v, ok := e.(*ConscientiousnessEvidence)
	if !ok || v == nil {
		return []string{"missing Conscientiousness report evidence"}
	}
	var issues []string
	if v.ReportID == "" {
		issues = append(issues, "report ID is empty")
	}
	if len(v.AttackVectors) == 0 {
		issues = append(issues, "no attack vectors recorded")
	}
	if v.Verdict == "" {
		issues = append(issues, "verdict is empty")
	}
	if v.Verdict != "" && v.Verdict != "DEFENSIBLE" && v.Verdict != "VULNERABLE" {
		issues = append(issues, fmt.Sprintf("invalid verdict %q", v.Verdict))
	}
	return issues
}

func validatePromptFooCI(e interface{}) []string {
	v, ok := e.(*PromptFooCIEvidence)
	if !ok || v == nil {
		return []string{"missing PromptFoo CI evidence"}
	}
	var issues []string
	if v.TotalTests == 0 {
		issues = append(issues, "no PromptFoo tests recorded")
	}
	if v.PassedTests+v.FailedTests != v.TotalTests {
		issues = append(issues, fmt.Sprintf("test count mismatch: %d passed + %d failed != %d total", v.PassedTests, v.FailedTests, v.TotalTests))
	}
	if v.ActionsRunID == "" {
		issues = append(issues, "Forgejo Actions run ID is empty")
	}
	if v.FailedTests > 0 {
		issues = append(issues, fmt.Sprintf("%d PromptFoo tests failed", v.FailedTests))
	}
	return issues
}

func validateCoApprovals(e interface{}) []string {
	v, ok := e.(*CoApprovalEvidence)
	if !ok || v == nil {
		return []string{"missing co-approval evidence"}
	}
	var issues []string
	if v.HumanApproval == nil {
		issues = append(issues, "human approval missing")
	} else {
		if v.HumanApproval.Reviewer == "" {
			issues = append(issues, "human approval reviewer is empty")
		}
		if v.HumanApproval.Timestamp.IsZero() {
			issues = append(issues, "human approval timestamp is zero")
		}
	}
	if v.AgentApproval == nil {
		issues = append(issues, "agent approval missing")
	} else {
		if v.AgentApproval.Reviewer == "" {
			issues = append(issues, "agent approval reviewer is empty")
		}
		if v.AgentApproval.Timestamp.IsZero() {
			issues = append(issues, "agent approval timestamp is zero")
		}
	}
	return issues
}

func validateMerge(e interface{}) []string {
	v, ok := e.(*MergeEvidence)
	if !ok || v == nil {
		return []string{"missing merge evidence"}
	}
	var issues []string
	if v.MergeSHA == "" {
		issues = append(issues, "merge SHA is empty")
	}
	if v.Strategy == "" {
		issues = append(issues, "merge strategy is empty")
	}
	if v.Timestamp.IsZero() {
		issues = append(issues, "merge timestamp is zero")
	}
	if v.LangFuseTraceID == "" {
		issues = append(issues, "LangFuse trace ID is empty")
	}
	return issues
}
