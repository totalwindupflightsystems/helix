# Verdict: context-auto-assembly

**Task:** Context auto-assembly and codebase indexer for agent tasks
**Evaluated:** 2026-07-13T03:55:07.167310
**Result:** ✗ FAIL

## Pipeline Stages

- ✓ **tier1**
  -   ✓ lsp: 
  ✓ trust_tier: ✓ Trust tier: no changed files to check — PASS

  ✓ lint: 
  ✓ tests: 
  ✓ build: 
  ✓ secrets: 
- ✗ **tier2**
  - INCOMPLETE
  ✗ ContextPackage struct exists in pkg/dispatcher/context.go with all required fields: TaskID, AgentID, SpecSection, AcceptanceCriteria, ADRs, PriorPRs, Incidents, CodeFiles, TotalTokens, TokenBudget, Expandable: ContextPackage struct does not exist. AssembledContext at context.go:56 has fields SpecSections, RelatedPRs, ADRConstraints, IncidentHistory, BudgetUsed, BudgetTotal — none of the required fields are present.
  ✗ AssembleContext always includes SpecSection and AcceptanceCriteria in the context package (AC-3.3.1) — verify context.go implements this: No AssembleContext function. ContextAssembler.Assemble() (context.go:117) includes spec sections via assembleSpecs() but has zero acceptance criteria logic. No acceptanceCriteriaFrom() equivalent exists anywhere in pkg/dispatcher/.
  ✓ TotalTokens never exceeds TokenBudget for any assembled package (AC-3.3.2) — verify budget enforcement code in context.go: ContextBudget.Consume() at context.go:37-40 checks if tokens > b.remaining before deducting. Assemble() and all helper methods check Consume() return values. Test TestAssemble_BudgetTrimming at context_test.go:239 confirms BudgetUsed ≤ BudgetTotal.
  ✗ Expandable resources are populated when resources exceed budget (AC-3.3.3) — verify with tiny budget test: Expand method (context.go:154) only supports post-assembly section expansion, not automatic demotion of over-budget resources. The old Expandable field and add() helper were removed. No tiny-budget test for expandable population exists.
  ✓ CodebaseIndex.Search finds relevant files by task description (AC-3.3.4) — verify in indexer.go and context_test.go: CodebaseIndex.Search at indexer.go:183 tokenizes query, looks up in inverted index, scores files by token coverage (hits/unique-tokens), and returns top-N ranked paths by relevance.
  ✓ IndexRepo ignores build artifacts: node_modules/, .git/, vendor/, dist/, *.min.js, *.generated.*, *.pb.go, _test.go (AC-3.3.5): IndexRepo at indexer.go:95 uses defaultIgnoreDirs=["node_modules",".git","vendor","dist"] (line 66-69) and defaultIgnoreFileSuffixes=[".min.js",".generated.",".pb.go","_test.go"] (line 75-78). shouldIgnoreSegment (line 283) and shouldIgnoreFile (line 300) implement filtering.
  ✗ Token budget is tier-gated: provisional=12000, observed=24000, trusted=48000, veteran=96000 (AC-3.3.6) — verify in context.go: No tier-gated budgets exist. The old contextBudget map with trust tier values was removed. Only DefaultBudget=4096 at context.go:112. No reference to 12000, 24000, 48000, 96000, or any trust.Tier constants in context.go.
  ✓ All 53+ packages build and test green: go test ./... -count=1 -short exits 0: go test ./... -count=1 -short exits 0 with all 54 packages (verified via go list ./... | wc -l) building and testing green.
4 of 8 criteria pass: budget enforcement, CodebaseIndex.Search, IndexRepo ignore patterns, and all tests pass. 4 fail: ContextPackage struct missing, no acceptance criteria, no expandable-on-exceed mechanism, and no tier-gated token budgets.

## Summary

Judge Result: context-auto-assembly

Stage tier1: PASS
    ✓ lsp: 
  ✓ trust_tier: ✓ Trust tier: no changed files to check — PASS

  ✓ lint: 
  ✓ tests: 
  ✓ build: 
  ✓ secrets: 

Stage tier2: FAIL
  INCOMPLETE
  ✗ ContextPackage struct exists in pkg/dispatcher/context.go with all required fields: TaskID, AgentID, SpecSection, AcceptanceCriteria, ADRs, PriorPRs, Incidents, CodeFiles, TotalTokens, TokenBudget, Expandable: ContextPackage struct does not exist. AssembledContext at context.go:56 has fields SpecSections, RelatedPRs, ADRConstraints, IncidentHistory, BudgetUsed, BudgetTotal — none of the required fields are present.
  ✗ AssembleContext always includes SpecSection and AcceptanceCriteria in the context package (AC-3.3.1) — verify context.go implements this: No AssembleContext function. ContextAssembler.Assemble() (context.go:117) includes spec sections via assembleSpecs() but has zero acceptance criteria logic. No acceptanceCriteriaFrom() equivalent exists anywhere in pkg/dispatcher/.
  ✓ TotalTokens never exceeds TokenBudget for any assembled package (AC-3.3.2) — verify budget enforcement code in context.go: ContextBudget.Consume() at context.go:37-40 checks if tokens > b.remaining before deducting. Assemble() and all helper methods check Consume() return values. Test TestAssemble_BudgetTrimming at context_test.go:239 confirms BudgetUsed ≤ BudgetTotal.
  ✗ Expandable resources are populated when resources exceed budget (AC-3.3.3) — verify with tiny budget test: Expand method (context.go:154) only supports post-assembly section expansion, not automatic demotion of over-budget resources. The old Expandable field and add() helper were removed. No tiny-budget test for expandable population exists.
  ✓ CodebaseIndex.Search finds relevant files by task description (AC-3.3.4) — verify in indexer.go and context_test.go: CodebaseIndex.Search at indexer.go:183 tokenizes query, looks up in inverted index, scores files by token coverage (hits/unique-tokens), and returns top-N ranked paths by relevance.
  ✓ IndexRepo ignores build artifacts: node_modules/, .git/, vendor/, dist/, *.min.js, *.generated.*, *.pb.go, _test.go (AC-3.3.5): IndexRepo at indexer.go:95 uses defaultIgnoreDirs=["node_modules",".git","vendor","dist"] (line 66-69) and defaultIgnoreFileSuffixes=[".min.js",".generated.",".pb.go","_test.go"] (line 75-78). shouldIgnoreSegment (line 283) and shouldIgnoreFile (line 300) implement filtering.
  ✗ Token budget is tier-gated: provisional=12000, observed=24000, trusted=48000, veteran=96000 (AC-3.3.6) — verify in context.go: No tier-gated budgets exist. The old contextBudget map with trust tier values was removed. Only DefaultBudget=4096 at context.go:112. No reference to 12000, 24000, 48000, 96000, or any trust.Tier constants in context.go.
  ✓ All 53+ packages build and test green: go test ./... -count=1 -short exits 0: go test ./... -count=1 -short exits 0 with all 54 packages (verified via go list ./... | wc -l) building and testing green.
4 of 8 criteria pass: budget enforcement, CodebaseIndex.Search, IndexRepo ignore patterns, and all tests pass. 4 fail: ContextPackage struct missing, no acceptance criteria, no expandable-on-exceed mechanism, and no tier-gated token budgets.

Overall: FAIL ✗
