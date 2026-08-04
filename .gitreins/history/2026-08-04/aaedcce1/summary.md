# Verdict: src-005

**Task:** Dispatcher wiring: inject capability-gated source tools at task dispatch (SPEC-025 §6)
**Evaluated:** 2026-08-04T09:03:31.727736
**Result:** ✓ PASS

## Pipeline Stages

- ✓ **tier1**
  -   ✓ lsp: 
  ✓ trust_tier: ✓ Trust tier: no changed files to check — PASS

  ✓ build: 
  ✓ lint: 
  ✓ tests: 
  ✓ secrets: 
- ✓ **tier2**
  - COMPLETE
  ✓ pkg/dispatcher/source_tools.go exists with SourceToolInjector, ToolsProvider, NewSourceToolInjector (file or map constructor), Inject(ctx, agent) ([]source.ToolSet, error), Wait(ctx, sourceName) error, WaitForTool(ctx, toolName, sets) error: pkg/dispatcher/source_tools.go (226 lines) defines type SourceToolInjector, type ToolsProvider func(ctx,*source.Source)(*source.ToolSet,error), NewSourceToolInjector(sources,opts...) plus NewSourceToolInjectorFromFile, Inject(ctx,AgentProfile)([]source.ToolSet,error) (line ~130), Wait(ctx,sourceName) error, and WaitForTool(ctx,toolName,sets) error (line ~215)
  ✓ AgentProfile (pkg/dispatcher/types.go) carries Capabilities []identity.CapabilityClaim; WorkItem carries SourceTools []source.ToolSet and SourceToolsError string (json omitempty): types.go: AgentProfile.Capabilities []identity.CapabilityClaim `json:"capabilities,omitempty"`; WorkItem.SourceTools []source.ToolSet `json:"source_tools,omitempty"` and SourceToolsError string `json:"source_tools_error,omitempty"`
  ✓ Dispatch path attaches capability-gated tools: agent claim strength 7-10 on matching domain receives full tool set; strength 1-6 receives read-only methods only (GET/HEAD/OPTIONS preserved, write methods stripped); no matching claim yields zero source tools (SPEC-025 §5): assigner.go DispatchContext: when d.SourceTools!=nil, sets,injErr := d.SourceTools.Inject(ctx, WorkItem.Agent); sets attached and injErr recorded to SourceToolsError. Gating delegated to source.Gateway.Filter (gateway.go: MinReadOnlyStrength=1, MaxReadOnlyStrength=6, MinReadWriteStrength=7, keepReadOnly=GET/HEAD/OPTIONS). Tests confirm each tier: TestSourceToolInjector_Inject_ReadWriteStrength (strength 9→all 4 tools incl POST/DELETE), Inject_ReadOnlyStrength (strength 4→2 read-only methods), NoMatchingClaim/NoClaims→0 sets, and end-to-end TestDispatcher_Dispatch_AttachesGatedTools and ReadOnlyStrengthEndToEnd
  ✓ Rate limiting wired: WaitForTool resolves the owning source by tool name within attached sets and waits; unknown source name returns an error: WaitForTool uses SourceForTool (iterates sets' Tools, matches tool.Name→ts.SourceName) then calls Wait(ctx,srcName) which delegates to source.RateLimitManager.Wait; unknown source returns error. Tests: TestSourceToolInjector_WaitForTool_GatesExecution (2/s burst: first 2 waits pass, 3rd blocked with deadline error), Wait_UnknownSource, WaitForTool_UnknownTool/UnknownSource all expect errors
  ✓ Muster unreachable fails cleanly: per-source generation errors are joined and returned, never a panic or hang; injector works with a static ToolsProvider (no live Muster required in tests): Inject collects per-source errors and returns errors.Join(genErrs...) alongside any filtered sets that succeeded (partial success preserved); nil injector/provider/empty sources all return errors, never panic. WithToolsProvider static seam + WithMusterBridge; all tests use static providers and never contact Muster (Inject_ProviderError, Inject_PartialFailure, Inject_NoProvider, NilReceiver, WithMusterBridge on empty sources)
  ✓ pkg/source and cmd/helix unchanged by this task (git diff HEAD~1 --stat shows no pkg/source or cmd/helix or go.mod/go.sum files): git diff HEAD~1 --stat -- pkg/source cmd/helix go.mod go.sum produced empty output (exit 0); changed files are only pkg/dispatcher/{assigner.go,source_tools.go,source_tools_test.go,types.go}, .gitreins/tasks.yaml, .vfs/graph/edges.jsonl, prompts/src-005/v1.md
  ✓ go build ./... and go vet ./... clean; golangci-lint run ./... zero issues; pkg/dispatcher tests pass (148 tests, incl. 27 new in source_tools_test.go): go build ./... exit 0; go vet ./... exit 0; golangci-lint run ./... reported '0 issues.'; go test -v ./pkg/dispatcher/ shows 148 PASS / 0 FAIL; source_tools_test.go contains exactly 27 func Test functions
  ✓ All pre-existing pkg/dispatcher tests unchanged and passing: git diff HEAD~1 --name-only -- pkg/dispatcher touches only assigner.go, source_tools.go, source_tools_test.go, types.go — no pre-existing *_test.go modified; full 148-test suite passes on fresh run (go test -count=1: ok, 0.110s)
SRC-005 is complete: source_tools.go implements the injector/provider/constructor API with capability-gated Inject, per-source Wait/WaitForTool rate limiting, and clean joined-error failure; types, dispatch wiring, and 27 new tests (148 total passing) all verified, with build/vet/lint clean and pkg/source, cmd/helix, and go.mod untouched.

## Summary

Judge Result: src-005

Stage tier1: PASS
    ✓ lsp: 
  ✓ trust_tier: ✓ Trust tier: no changed files to check — PASS

  ✓ build: 
  ✓ lint: 
  ✓ tests: 
  ✓ secrets: 

Stage tier2: PASS
  COMPLETE
  ✓ pkg/dispatcher/source_tools.go exists with SourceToolInjector, ToolsProvider, NewSourceToolInjector (file or map constructor), Inject(ctx, agent) ([]source.ToolSet, error), Wait(ctx, sourceName) error, WaitForTool(ctx, toolName, sets) error: pkg/dispatcher/source_tools.go (226 lines) defines type SourceToolInjector, type ToolsProvider func(ctx,*source.Source)(*source.ToolSet,error), NewSourceToolInjector(sources,opts...) plus NewSourceToolInjectorFromFile, Inject(ctx,AgentProfile)([]source.ToolSet,error) (line ~130), Wait(ctx,sourceName) error, and WaitForTool(ctx,toolName,sets) error (line ~215)
  ✓ AgentProfile (pkg/dispatcher/types.go) carries Capabilities []identity.CapabilityClaim; WorkItem carries SourceTools []source.ToolSet and SourceToolsError string (json omitempty): types.go: AgentProfile.Capabilities []identity.CapabilityClaim `json:"capabilities,omitempty"`; WorkItem.SourceTools []source.ToolSet `json:"source_tools,omitempty"` and SourceToolsError string `json:"source_tools_error,omitempty"`
  ✓ Dispatch path attaches capability-gated tools: agent claim strength 7-10 on matching domain receives full tool set; strength 1-6 receives read-only methods only (GET/HEAD/OPTIONS preserved, write methods stripped); no matching claim yields zero source tools (SPEC-025 §5): assigner.go DispatchContext: when d.SourceTools!=nil, sets,injErr := d.SourceTools.Inject(ctx, WorkItem.Agent); sets attached and injErr recorded to SourceToolsError. Gating delegated to source.Gateway.Filter (gateway.go: MinReadOnlyStrength=1, MaxReadOnlyStrength=6, MinReadWriteStrength=7, keepReadOnly=GET/HEAD/OPTIONS). Tests confirm each tier: TestSourceToolInjector_Inject_ReadWriteStrength (strength 9→all 4 tools incl POST/DELETE), Inject_ReadOnlyStrength (strength 4→2 read-only methods), NoMatchingClaim/NoClaims→0 sets, and end-to-end TestDispatcher_Dispatch_AttachesGatedTools and ReadOnlyStrengthEndToEnd
  ✓ Rate limiting wired: WaitForTool resolves the owning source by tool name within attached sets and waits; unknown source name returns an error: WaitForTool uses SourceForTool (iterates sets' Tools, matches tool.Name→ts.SourceName) then calls Wait(ctx,srcName) which delegates to source.RateLimitManager.Wait; unknown source returns error. Tests: TestSourceToolInjector_WaitForTool_GatesExecution (2/s burst: first 2 waits pass, 3rd blocked with deadline error), Wait_UnknownSource, WaitForTool_UnknownTool/UnknownSource all expect errors
  ✓ Muster unreachable fails cleanly: per-source generation errors are joined and returned, never a panic or hang; injector works with a static ToolsProvider (no live Muster required in tests): Inject collects per-source errors and returns errors.Join(genErrs...) alongside any filtered sets that succeeded (partial success preserved); nil injector/provider/empty sources all return errors, never panic. WithToolsProvider static seam + WithMusterBridge; all tests use static providers and never contact Muster (Inject_ProviderError, Inject_PartialFailure, Inject_NoProvider, NilReceiver, WithMusterBridge on empty sources)
  ✓ pkg/source and cmd/helix unchanged by this task (git diff HEAD~1 --stat shows no pkg/source or cmd/helix or go.mod/go.sum files): git diff HEAD~1 --stat -- pkg/source cmd/helix go.mod go.sum produced empty output (exit 0); changed files are only pkg/dispatcher/{assigner.go,source_tools.go,source_tools_test.go,types.go}, .gitreins/tasks.yaml, .vfs/graph/edges.jsonl, prompts/src-005/v1.md
  ✓ go build ./... and go vet ./... clean; golangci-lint run ./... zero issues; pkg/dispatcher tests pass (148 tests, incl. 27 new in source_tools_test.go): go build ./... exit 0; go vet ./... exit 0; golangci-lint run ./... reported '0 issues.'; go test -v ./pkg/dispatcher/ shows 148 PASS / 0 FAIL; source_tools_test.go contains exactly 27 func Test functions
  ✓ All pre-existing pkg/dispatcher tests unchanged and passing: git diff HEAD~1 --name-only -- pkg/dispatcher touches only assigner.go, source_tools.go, source_tools_test.go, types.go — no pre-existing *_test.go modified; full 148-test suite passes on fresh run (go test -count=1: ok, 0.110s)
SRC-005 is complete: source_tools.go implements the injector/provider/constructor API with capability-gated Inject, per-source Wait/WaitForTool rate limiting, and clean joined-error failure; types, dispatch wiring, and 27 new tests (148 total passing) all verified, with build/vet/lint clean and pkg/source, cmd/helix, and go.mod untouched.

Overall: PASS ✓
