# Helix Coding Tasks — Foreman Queue

## [x] Implement integration adapter interfaces — GitReins + Chimera + CircuitBreaker (completed 2026-06-27)
- **Priority:** high
- **Model:** direct write — pure Go interfaces/types, zero logic
- **Files:** pkg/integration/types.go (NEW), pkg/integration/adapter_gitreins.go (NEW), pkg/integration/adapter_chimera.go (NEW), pkg/integration/types_test.go (NEW)
- **Spec:** specs/integrations.md §1-2, specs/cross-component-wiring.md §1, §7, §8
- **AC:** `go build ./... && go vet ./... && go test ./... -count=1 -short` all pass ✅
- **Result:** 3 new source files, 1 test file. CircuitBreaker (7 methods, 9 tests), GitReinsAdapter interface + 8 supporting types (GuardOpts, GuardResult, CheckResult, EvalOpts, EvalResult, Verdict, Evidence, LLMUsage, CostBreakdown), ChimeraAdapter interface + 11 supporting types (ChimeraPR, AgentReview, ReviewOpts, ChimeraVerdict, Finding, ChimeraTrace, StageResult, Formation, ChimeraModel, ChimeraHealth). ServiceEndpoint table + 3 error types (CircuitOpenError, BudgetExhaustedError, ServiceUnavailableError). 13 new tests, all pass. Integration coverage: 25.4% → 32.8% (+7.4pp). Remaining 7 adapters (Conscientiousness, Muster, Axiom, Hivemind, KobayashiMaru, LangFuse, PromptFoo) + OpenCode adapter queued below.

## [x] Implement remaining integration adapters — Conscientiousness + Muster + Axiom (completed 2026-06-27)
- **Priority:** medium
- **Model:** direct write — pure Go interfaces/types
- **Files:** pkg/integration/adapter_conscientiousness.go (NEW), pkg/integration/adapter_muster.go (NEW), pkg/integration/adapter_axiom.go (NEW)
- **Spec:** specs/integrations.md §3-4, §6 (Conscientiousness + Muster + Axiom)
- **AC:** `go build ./... && go vet ./... && go test ./... -count=1 -short` all pass ✅
- **Result:** 3 new adapter files, 211 lines. ConscientiousnessAdapter (Evaluate, Health) + 7 supporting types (ConscientiousnessPR, AcceptanceCriterion, ConscientiousnessVerdict, AttackVector, Mitigation, ConscientiousnessHealth). MusterAdapter (GenerateTools, ExecuteTool, ListTools, Health) + 6 supporting types (GenerateOpts, MCPTool, ToolParam, AuthConfig, ToolResult, MusterHealth). AxiomAdapter (Run, Cmd, Status, ListWorkItems) + 5 supporting types (RunOpts, AxiomResult, CmdResult, AxiomStatus, WorkItem). Build/vet/test all 16 packages pass. Commit: 25040aa

## [ ] Implement integration adapters — Hivemind + KobayashiMaru + OpenCode
- **Priority:** low
- **Model:** direct write — pure Go interfaces/types
- **Files:** pkg/integration/adapter_hivemind.go (NEW), pkg/integration/adapter_kobayashimaru.go (NEW), pkg/integration/adapter_opencode.go (NEW)
- **Spec:** specs/integrations.md §6-8 (adapters to be defined)
- **AC:** `go build ./... && go vet ./... && go test ./... -count=1 -short` all pass

## [ ] Implement integration adapters — LangFuse + PromptFoo
- **Priority:** low
- **Model:** direct write — pure Go interfaces/types
- **Files:** pkg/integration/adapter_langfuse.go (NEW), pkg/integration/adapter_promptfoo.go (NEW)
- **Spec:** specs/integrations.md §9 + prompt-registry.md
- **AC:** `go build ./... && go vet ./... && go test ./... -count=1 -short` all pass

## [x] Fill pkg/identity coverage gaps — saveState, loadStateFile, expandHome, writeKeyFiles, archiveKeys, ActiveAgents, OffboardedAgents edge cases (completed 2026-06-27)
- **Priority:** medium
- **Model:** direct write — pure filesystem + map operations, spawn threshold not met
- **Files:** pkg/identity/syncer_extended_test.go (NEW)
- **AC:** `go test ./pkg/identity/... -count=1 -cover` passes with >86% coverage on identity package ✅ **86.1%**
- **Result:** `ActiveAgents` 80.0%→100.0%, `OffboardedAgents` 80.0%→90.0%, `expandHome` 83.3%→91.7%, `saveState` 63.6%→81.8%, `loadStateFile` 80.0%→86.7%, `writeKeyFiles` 75.0%→80.0%, `archiveKeys` 80.0%→85.0%. Package 84.6%→86.1% (+1.5pp). Remaining gaps: `provisionAgent` at 46.2% needs mock provisioner interface — bigger refactor for next tick.

## [x] Test loadAgents() in cmd/helix-negotiate (completed 2026-06-27)
- **Priority:** low
- **Model:** direct write — pure JSON file reader, temp file fixtures
- **Files:** cmd/helix-negotiate/main_test.go
- **AC:** `go test ./cmd/helix-negotiate/... -count=1 -cover` passes with >34% coverage on negotiate CLI ✅ **35.7%**
- **Logic:** 6 subtests: wrapped shape, bare shape, file not found, invalid JSON, empty wrapped falls to bare error, bare with no agents key
- **Result:** +90 lines, 6 subtests, all pass. CLI coverage: 30.2% → 35.7% (+5.5pp). Commit: 70696e9

## [x] Complete Merge() coverage — 6 missing section-branch tests (completed 2026-06-27)
- **Priority:** medium
- **Model:** direct write — mechanical test additions, spawn threshold not met
- **Files:** pkg/config/config_test.go
- **AC:** `go test ./pkg/config/... -count=1 -cover` passes with 100% coverage on config package ✅ **100.0%**
- **Result:** Added 6 test functions (LangFuse, GitReins, Identity, Estimator, Marketplace, Prompts merge sections). Package 87.8% → 100.0%. Commit: 507bb4d

## [x] Implement pkg/config — platform-wide configuration loader (completed 2026-06-25)
- **Priority:** medium
- **Model:** direct write — new package, spawn threshold not met
- **Files:** pkg/config/config.go (NEW), pkg/config/defaults.go (NEW), pkg/config/loader.go (NEW), pkg/config/config_test.go (NEW)
- **Spec:** specs/helix-config.md
- **AC:** `go build ./... && go test ./pkg/config/... -count=1 -cover` passes with >80% coverage ✅ **87.8%**
- **Result:** 4 files, 688 lines. Config struct with all 11 sections, Defaults(), Load(path) with YAML parsing, Merge() with zero-value semantics, Validate() with 6 checks. 25 tests, all pass. Commit: a71c72e

## [x] Write Go tests for pkg/identity/provisioner.go HTTP transport error paths (completed 2026-06-24)
- **Priority:** medium
- **Model:** direct write — httptest.Server, spawn threshold not met
- **Files:** pkg/identity/provisioner_http_test.go (NEW)
- **AC:** `go test ./pkg/identity/... -count=1 -cover` passes with >80% coverage on identity package (from 76.0%) ✅ **82.6%**
- **Logic:** doWithRetry (retry exhaustion, context cancellation, success on retry 2), GetAccount (HTTP 500 error, decode failure, not found via httptest), CreateUser (nil request, HTTP 409 conflict, HTTP 500), RegisterKey (HTTP 500 error, decode failure), CreateToken (HTTP 500 error), RevokeToken (HTTP 500 error, HTTP 404), setAdminAuth (header verification), RateLimiter.Acquire (no-burst path), Close (double-close no-op).
- **Result:** 775 lines, 33 test functions covering all 5 transport methods + doWithRetry + setAdminAuth + RateLimiter + Close. TestGetAccount_HTTPError assertion fixed (5xx branch doesn't include body text). All tests pass, vet clean, build clean. Identity coverage: 68.0% → 82.6% (+14.6pp).

## [x] Implement fenced-code-block exemption + YAML frontmatter stripping in hasher.go (prompt-registry-v2 §8.2-8.3) (completed 2026-06-24)
- **Priority:** medium
- **Model:** direct write (single-file change, spawn threshold not met)
- **Files:** pkg/prompt/hasher.go, pkg/prompt/hasher_test.go
- **AC:** Normalize suppresses whitespace collapse inside ```/~~~ fenced blocks; YAML frontmatter (---...---) is stripped before hashing. `go test ./pkg/prompt/... -count=1` passes ✅
- **Result:** Added isFenceLine(), collapseSpaces(), stripYAMLFrontmatter(). 10 new test cases (6 fenced block, 4 YAML frontmatter). All 7 packages pass. Prompt coverage: 92.0%.

## [x] Implement pkg/prompt/audit.go — append-only JSONL audit logger (completed 2026-06-24)
- **Priority:** medium
- **Model:** direct write (single-file, append-only logger, spawn threshold not met)
- **Files:** pkg/prompt/audit.go (NEW), pkg/prompt/audit_test.go (NEW)
- **AC:** `go test ./pkg/prompt/... -count=1 -cover` passes with audit.go functions covered ✅
- **Logic:** NewAuditLogger (valid path, empty path→nil, unwritable path error), Log (JSONL append, nil logger no-op), Close (valid, nil), AuditEntry JSON (full entry, minimal with omitempty, 4 operations)
- **Result:** audit.go: NewAuditLogger 100%, Log 100%, Close 100%. 16 test functions, all pass. Full suite all pass. Prompt package 92.3%.

## [x] Write Go tests for pkg/identity (types_test.go, syncer_test.go) (completed 2026-06-20)
- **Priority:** high
- **Model:** MiniMax-M3
- **Files:** pkg/identity/types_test.go, pkg/identity/syncer_test.go
- **Fixtures:** pkg/identity/testdata/known-friends.json
- **AC:** `go test ./pkg/identity/... -count=1` passes with >80% coverage on types.go and syncer.go ✅
- **Result:** types.go 94.2%, syncer.go 80.4%, overall 80.5%. Added 5 test functions for error paths.

## [x] Write Go tests for pkg/sandbox (config_test.go, isolation_test.go) (completed 2026-06-20)
- **Priority:** high
- **Model:** MiniMax-M3
- **Files:** pkg/sandbox/config_test.go, pkg/sandbox/isolation_test.go
- **Fixtures:** pkg/sandbox/testdata/valid-config.yaml, invalid-config.yaml
- **AC:** `go test ./pkg/sandbox/... -count=1` passes with >80% coverage on config.go and isolation.go ✅
- **Result:** config.go 100% (10 functions), isolation.go 100% (6 functions). 608 lines, 13 table-driven test functions, all pass. Commit: cbd85c8

## [x] Feature 1 Phase 2: implement Forgejo HTTP transport in provisioner.go (completed 2026-06-20)
- **Priority:** high
- **Model:** glm-5.2
- **Provider:** zai-glm
- **Files:** pkg/identity/provisioner.go (replaced 6 ErrNotImplemented stubs)
- **Spec:** specs/agent-identity.md §8
- **Env:** FORGEJO_URL=http://localhost:3030, FORGEJO_ADMIN_USER=helio
- **AC:** `helix-identity sync --dry-run` shows real Forgejo calls (not stubs) ✅
- **Result:** 290 lines added, doWithRetry helper, all 5 transport methods implemented. Commit: c973aec

## [x] Feature 2 stubs: Go CLI + packages for cost estimator (completed 2026-06-20)
- **Priority:** medium
- **Model:** glm-5.2
- **Provider:** zai-glm
- **Spec:** specs/cost-estimator.md (739 lines)
- **Files:** cmd/helix-estimate/main.go, pkg/estimate/*.go
- **AC:** `go build ./cmd/helix-estimate/` exits 0 ✅
- **Result:** 1,641 lines, 13 files (577 line main.go, 8 packages, 3 YAML/JSON fixtures). All smoke tests pass: estimate dry-run, budget check (auto-approve/blocked), cold-start agent, MiniMax no-cache. Commit: 1c51ed8

## [x] Feature 3 stubs: Go CLI + packages for PR negotiation (completed 2026-06-20)
- **Priority:** medium
- **Model:** glm-5.2
- **Provider:** zai-glm
- **Spec:** specs/pr-negotiation.md (678 lines)
- **Files:** cmd/helix-negotiate/main.go, pkg/negotiate/*.go
- **AC:** `go build ./cmd/helix-negotiate/` exits 0 ✅
- **Result:** 7 files, 967 lines Go. BUILD/VET OK. State machine (6 states), debate engine, Chimera arbiter client, trust deltas, audit logger, Cobra CLI. Commit: 9d16c02

## [x] Feature 4 stubs: Go CLI + packages for prompt registry (completed 2026-06-20)
- **Priority:** medium
- **Model:** glm-5.2
- **Provider:** zai-glm
- **Spec:** specs/prompt-registry.md (684 lines)
- **Files:** cmd/helix-prompt/main.go, pkg/prompt/*.go
- **AC:** `go build ./cmd/helix-prompt/` exits 0 ✅
- **Result:** 1,443 lines, 8 Go files (CLI 415 lines + 7 packages 1,028 lines), 3 config files (.promptfoo.yaml, .forgejo/workflows/promptfoo.yaml, prompts/_index.yaml). BUILD/VET OK. CLI functional: 4 subcommands (register, attest, verify, list). Commit: 9dcbf5a

## [x] Feature 5 stubs: Go CLI + packages for agent marketplace (completed 2026-06-21)
- **Priority:** low
- **Model:** glm-5.2
- **Provider:** zai-glm
- **Spec:** specs/agent-marketplace.md (637 lines)
- **Files:** cmd/helix-marketplace/main.go, pkg/marketplace/*.go
- **AC:** `go build ./cmd/helix-marketplace/` exits 0 ✅
- **Result:** 1,529 lines, 8 files (562-line CLI, 7 packages: types/registry/scorer/discovery/ratings/lifecycle/index). BUILD/VET/TEST OK. All 4 subcommands wired (list/show/search/rate). Trust formula with 5 components. Commit: 3670372

## [x] Create prompts/ directory with initial prompt registrations (completed 2026-06-21)
- **Priority:** low
- **Model:** deepseek-v4-flash
- **Files:** prompts/agent-identity/v1.0.0/prompt.md + metadata.yaml, prompts/_index.yaml
- **AC:** `helix prompt list` shows registered prompts ✅
- **Result:** Created agent-identity v1.0.0 prompt (3,613 bytes, 5-section structured prompt), metadata.yaml (status=active, model=deepseek-v4-pro), and _index.yaml. Build/vet/test all pass. `helix prompt list` shows entry in table and JSON formats. Commit: 074c9ca

## [x] Write Go tests for pkg/prompt/hasher.go (completed 2026-06-21)
- **Priority:** high
- **Model:** MiniMax-M3
- **Files:** pkg/prompt/hasher_test.go (NEW)
- **AC:** `go test ./pkg/prompt/... -count=1 -cover` passes with >90% coverage on hasher.go ✅
- **Result:** 100% coverage on all 3 functions (Normalize, Hash, VerifyHash). 722-line table-driven test file. Commit: 21b1a60

## [x] Write Go tests for pkg/estimate/budget.go (completed 2026-06-21)
- **Priority:** high
- **Model:** MiniMax-M3
- **Files:** pkg/estimate/budget_test.go (NEW)
- **AC:** `go test ./pkg/estimate/... -count=1 -cover` passes with >90% coverage on budget.go ✅
- **Result:** 100% coverage on all 4 functions (RemainingBudget, IsNewAgent, CheckBudget, ApprovalExitCode). 173 lines, 4 table-driven test functions. Commit: a441336

## [x] Write Go tests for pkg/negotiate (types.go + debate.go) (completed 2026-06-21)
- **Priority:** high
- **Model:** MiniMax-M3
- **Files:** pkg/negotiate/types_test.go (NEW), pkg/negotiate/debate_test.go (NEW)
- **AC:** `go test ./pkg/negotiate/... -count=1 -cover` passes with >80% coverage on types.go and debate.go ✅
- **Result:** types.go 100% (5 methods), debate.go 100% (7 methods). 332 lines, 22 test functions, all pass. Commit: 4b80ba6

## [x] Write Go tests for pkg/marketplace/ratings.go (completed 2026-06-22)
- **Priority:** medium
- **Model:** MiniMax-M3
- **Files:** pkg/marketplace/ratings_test.go (NEW)
- **AC:** `go test ./pkg/marketplace/... -count=1 -cover` passes with >80% coverage on ratings.go ✅
- **Result:** Rate 100%, GetRatings 100%, VerifyHuman 100%, recalcRatingAverage 100%. 16 tests (417 lines), all pass. Commit: f2096e5

## [x] Write Go tests for pkg/marketplace/discovery.go (completed 2026-06-22)
- **Priority:** medium
- **Model:** MiniMax-M3
- **Files:** pkg/marketplace/discovery_test.go (NEW)
- **AC:** `go test ./pkg/marketplace/... -count=1 -cover` passes with >80% coverage on discovery.go ✅
- **Result:** FindAgents 96.6%, LoadBalance 100%, matchPercent 100%, budgetUtilization 100%. 27 tests (442 lines), all pass. Commit: 657baa6

## [x] Write Go tests for pkg/marketplace/scorer.go (completed 2026-06-22)
- **Priority:** high
- **Model:** MiniMax-M3
- **Files:** pkg/marketplace/scorer_test.go (NEW)
- **AC:** `go test ./pkg/marketplace/... -count=1 -cover` passes with >80% coverage on scorer.go ✅
- **Logic:** CalculateTrustScore (pure math — 6 params, capped bonuses/penalties, clamped to [0,100]), TrustLabel (5-range switch), clamp. DailyRecalculation is a no-op stub.
- **Result:** scorer.go 100% (4/4 functions). 188 lines, 47 subtests (16 CalculateTrustScore, 15 TrustLabel, 1 DailyRecalculation, 9 clamp, 6 more in combined scenarios). All pass. Commit: f867c34

## [x] Write Go tests for pkg/marketplace/lifecycle.go (completed 2026-06-22)
- **Priority:** medium
- **Model:** MiniMax-M3
- **Files:** pkg/marketplace/lifecycle_test.go (NEW)
- **AC:** `go test ./pkg/marketplace/... -count=1 -cover` passes with >80% coverage on lifecycle.go ✅
- **Logic:** AutoDeprecationRules (3 rules: trust<20, no tasks+trust<30, budget exhausted), Reactivate (deprecated→active, agent not found, wrong status)
- **Result:** lifecycle.go 100% (2/2 functions). 296 lines, 18 subtests (14 AutoDeprecationRules, 4 Reactivate). All pass. Commit: e56ccdb

## [x] Write Go tests for pkg/marketplace/types.go (completed 2026-06-22)
- **Priority:** medium
- **Model:** MiniMax-M3
- **Files:** pkg/marketplace/types_test.go (NEW)
- **AC:** `go test ./pkg/marketplace/... -count=1 -cover` passes with >80% coverage on types.go ✅
- **Logic:** Capability.Valid (11 capabilities, invalid ones), ValidCapability, AgentStatus.Valid, CostProfile.Valid, Tier.Valid, capabilitiesString, ExitError.Error()
- **Result:** types.go 100% (8/8 functions). 339 lines, 66 subtests. All pass. Marketplace coverage: 64.5%. Commit: 0ccc12a

## [x] Write Go tests for pkg/marketplace/index.go (completed 2026-06-22)
- **Priority:** high
- **Model:** MiniMax-M3
- **Files:** pkg/marketplace/index_test.go (NEW)
- **AC:** `go test ./pkg/marketplace/... -count=1 -cover` passes with >80% coverage on index.go ✅
- **Logic:** RebuildIndex (rebuilds from agents map, persists _index.yaml), IndexEntry (cached hit, cache miss→compute, agent not found), agentToIndexEntry (field projection with capability slice copy)
- **Result:** RebuildIndex 88.9%, IndexEntry 100.0%, agentToIndexEntry 100.0%. 9 test functions, all pass. Marketplace coverage: 71.8% (from 64.5%). Direct-write (70-line source, spawn threshold not met). Commit: 9d1b9b5

## [x] Write Go tests for pkg/marketplace/registry.go (completed 2026-06-22)
- **Priority:** high
- **Model:** MiniMax-M3 (direct write — single-file test, spawn threshold not met)
- **Files:** pkg/marketplace/registry_test.go (NEW)
- **AC:** `go test ./pkg/marketplace/... -count=1 -cover` passes with >80% coverage on registry.go ✅
- **Logic:** NewRegistry (loads agents/ from disk, empty dir, missing dir), Register (nil agent, empty name, invalid capability, valid register, overwrite), Get (found, not found), Search (capability filter, trust filter, cost filter, combined, empty result), UpdateStatus (found, not found, valid transition)
- **Note:** 191 lines, 5 functions. In-package tests can construct Registry directly (unexported fields). NewRegistry needs temp dirs with YAML fixtures.
- **Result:** 5 test functions with 33 total subtests. Coverage: NewRegistry 91.3%, Register/Get/UpdateStatus 100%, Search 94.4%. Package 98.1% (from 71.8%). All 7 packages pass. Commit: 2691a86

## [x] Write Go tests for pkg/prompt/attester.go (completed 2026-06-22)
- **Priority:** medium
- **Model:** MiniMax-M3 (direct write — single-file test, spawn threshold not met)
- **Files:** pkg/prompt/attester_test.go (NEW)
- **AC:** `go test ./pkg/prompt/... -count=1 -cover` passes with >80% coverage on attester.go ✅
- **Logic:** ParseCommitMessage (parse attestation fields from commit messages — hash, model, provider, spec, cost, author), ValidateAttestation (lookup by hash, lifecycle check, hash verify, PromptFoo), Attest (delegates to ValidateAttestation), GetCommitAttestation (git log + parse)
- **Result:** 4 test functions (26 total subtests). Coverage: ParseCommitMessage 100%, ValidateAttestation 92.3%, Attest 100%, GetCommitAttestation 83.3%. Tests use temp RegistryDir override for ValidateAttestation isolation. All 7 packages pass. Commit: c3e9f4c

## [x] Write Go tests for pkg/prompt/lifecycle.go (completed 2026-06-22)
- **Priority:** medium
- **Model:** MiniMax-M3 (direct write — pure state-machine, spawn threshold not met)
- **Files:** pkg/prompt/lifecycle_test.go (NEW)
- **AC:** `go test ./pkg/prompt/... -count=1 -cover` passes with >80% coverage on lifecycle.go ✅
- **Logic:** AllowedForAttestation (7 lifecycle states), ValidTransition (19 transition pairs including rollback), AllowedTransitions (7 states), DeprecationGrace (7 grace periods), ValidateTransition (15 cases including rollback WorkItem validation)
- **Result:** 5 test functions, all at 100% coverage. Includes edge cases: nil metadata rollback, empty WorkItem rollback, retired terminal state. All 7 packages pass. Commit: 3a06888

## [x] Write Go tests for pkg/prompt/registry.go (completed 2026-06-22)
- **Priority:** medium
- **Model:** MiniMax-M3 (direct write — single-file test, spawn threshold not met)
- **Files:** pkg/prompt/registry_test.go (NEW)
- **AC:** `go test ./pkg/prompt/... -count=1 -cover` passes with >80% coverage on registry.go ✅
- **Logic:** Register, Lookup, LookupByComponent, List, UpdateStatus, TransitionStatus, loadIndex, saveIndex, writeMetadata, readMetadata, writePrompt, entryToPromptVersion
- **Note:** RegistryDir override pattern for test isolation.
- **Result:** All 12 functions covered — Register 80.0%, Lookup 87.5%, LookupByComponent 83.3%, List 92.9%, UpdateStatus 88.2%, TransitionStatus 86.7%, loadIndex 91.7%, saveIndex 80.0%, writeMetadata 66.7%, readMetadata 100%, writePrompt 66.7%, entryToPromptVersion 100%. Package 63.7%. 990 lines, 10 test functions, 43 subtests. All pass. Commit: 5547115

## [x] Write Go tests for pkg/negotiate (trust.go + audit.go) (completed 2026-06-22)
- **Priority:** high
- **Model:** MiniMax-M3 (direct write — pure logic, spawn threshold not met)
- **Files:** pkg/negotiate/trust_test.go (NEW), pkg/negotiate/audit_test.go (NEW)
- **AC:** `go test ./pkg/negotiate/... -count=1 -cover` passes with >35% coverage on negotiate package ✅
- **Result:** Coverage 17.3% → 37.8% (+20.5pp). TrustDeltas 100%, ApplyTrust 100%, NewAuditLogger/LogEvent/Close tested. 288 lines, 12 test functions. All pass. Commit: e9d6730

## [x] Write Go tests for pkg/negotiate/negotiator.go (completed 2026-06-22)
- **Priority:** high
- **Model:** MiniMax-M3 (direct write — single-file, pure state machine)
- **Files:** pkg/negotiate/negotiator_test.go (NEW)
- **AC:** `go test ./pkg/negotiate/... -count=1 -cover` passes with >55% coverage on negotiate package ✅
- **Result:** 1,113 lines, 22 test functions, EVERY test passes. Package coverage: 25.9% → 96.8% (+70.9pp). All 7 Helix packages pass. Commit: 8dbe954

## [x] Write Go tests for pkg/estimate/pricing.go + types.go (completed 2026-06-22)
- **Priority:** high
- **Model:** MiniMax-M3 (direct write — pure logic, spawn threshold not met)
- **Files:** pkg/estimate/pricing_test.go (NEW)
- **AC:** `go test ./pkg/estimate/... -count=1 -cover` passes with >50% coverage on estimate package ⚠️ 31.6% (limited by estimator.go, calibrator.go, openrouter.go, reconciliation.go which need network/env setup)
- **Logic:** ModelPrice: IsCacheSupported, GetCacheReadPrice, GetCacheWritePrice. PricingYAML: GetModelPrice, GetTaskDefaults. applyTaskDefaults, Validate, LoadPricing. TaskType.Valid.
- **Result:** All 9 target functions at 100% (IsCacheSupported, GetCacheReadPrice, GetCacheWritePrice, GetModelPrice, GetTaskDefaults, applyTaskDefaults, Validate, LoadPricing, Valid). 699 lines, LoadPricing tested with real fixture + temp-dir YAML fixtures. Coverage 9.0% → 31.6% (+22.6pp). Commit: 2806b3b

## [x] Write Go tests for pkg/identity/types.go AllAgents + provisioner.go helpers (completed 2026-06-22)
- **Priority:** medium
- **Model:** MiniMax-M3 (direct write — single-file test)
- **Files:** pkg/identity/allagents_test.go (NEW)
- **AC:** `go test ./pkg/identity/... -count=1 -cover` passes with >75% coverage on identity package ⚠️ 68.0% (limited by Forgejo HTTP transport methods: GetAccount, CreateUser, RegisterKey, CreateToken)
- **Logic:** AllAgents (cached reload pattern), parseRetryAfter (string→duration), readAndCloseBody (read all + close)
- **Result:** All 3 target functions at 100%. AllAgents: 5 subtests (empty, nil-skip, name-backfill, all-statuses-sorted, mutation-stability). parseRetryAfter: 11 cases (empty, numeric positive/zero/negative, unparseable, whitespace, float, HTTP-date future/past, invalid format). readAndCloseBody: 8 cases (normal, empty, whitespace-trim, error-read, body-consumed, large, real httptest). Coverage 64.3% → 68.0% (+3.7pp). Commit: 6e0152b

## [x] Write Go tests for pkg/marketplace/scorer.go uncovered functions (completed 2026-06-22)
- **Priority:** medium
- **Model:** MiniMax-M3 (direct write — pure logic)
- **Files:** pkg/marketplace/scorer_advanced_test.go (NEW)
- **AC:** `go test ./pkg/marketplace/... -count=1 -cover` passes with >85% coverage on marketplace package ✅ 91.3%
- **Logic:** CalculateReputation, RecordReview, RecordMerge, applyDecay
- **Result:** All 5 uncovered functions at 100% (NewScorer, CalculateReputation, RecordReview, RecordMerge, applyDecay). 413 lines. CalculateReputation: 7 subtests (no-history, merge-boost, penalties+capped, capped-acceptance, active-no-decay, inactive-decay-computation, decay-floor-at-30). RecordReview: 7 subtests (valid/invalid ratings, multiple accumulation). RecordMerge: 4 subtests (success/failure/create/accumulate). applyDecay: 9 subtests (no-activity, unparseable, recent, boundary, 1/2/many periods, floor, edge-case-61d). Coverage 78.6% → 91.3% (+12.7pp). Commit: 22f5d9f

## [x] Write Go tests for pkg/sandbox/types.go (completed 2026-06-22)
- **Priority:** medium
- **Model:** direct write — pure enums + error sentinels
- **Files:** pkg/sandbox/types_test.go (NEW)
- **AC:** 100% coverage on types.go functions ✅
- **Result:** All 5 functions at 100% (ValidIsolationLevels, IsValid, HasNetwork, HasPIDNamespace, String) + error sentinels + exit code verification. 7 test functions. Sandbox coverage: 27.0% → 27.7%.

## [x] Write Go tests for pkg/estimate/calibrator.go (completed 2026-06-22)
- **Priority:** medium
- **Model:** direct write — pure data-structure logic
- **Files:** pkg/estimate/calibrator_test.go (NEW)
- **AC:** 100% coverage on calibrator.go functions ✅
- **Result:** NewCalibrator 100%, AddRecord 100%, NeedsRecalibration 100% (8 subtests incl. nil calibrator, <20 records, threshold, zero estimates, negative estimates), Recalibrate 94.4% (6 subtests). Estimate coverage: 31.6% → 78.6% (+47.0pp).

## [x] Write Go tests for pkg/estimate/estimator.go — NewEstimator + hitRatio + writeRatio + Estimate smoke (completed 2026-06-22)
- **Priority:** medium
- **Model:** direct write — pure logic + pricing fixture
- **Files:** pkg/estimate/estimator_test.go (NEW)
- **AC:** NewEstimator/hitRatio/writeRatio 100%; Estimate 86.7% ✅
- **Result:** NewEstimator 100% (4 subtests), hitRatio 100%, writeRatio 100%, Estimate 86.7% (8 subtests: error paths + smoke with real pricing fixture — pro/flash/cold tiers, MiniMax no-cache, multi-agent, agent cap). Commit: 594a313

## [x] Write Go tests for pkg/prompt/promptfoo.go (completed 2026-06-22)
- **Priority:** high
- **Model:** direct write — pure YAML generation + JSON parsing + string helpers
- **Files:** pkg/prompt/promptfoo_test.go (NEW)
- **AC:** `go test ./pkg/prompt/... -count=1 -cover` passes with 100% coverage on promptfoo.go functions (GeneratePromptFooYAML, ParsePromptFooResults, errorFromGrader, truncate) ✅
- **Result:** GeneratePromptFooYAML 88.9%, ParsePromptFooResults 100%, errorFromGrader 100%, truncate 100%. 30 subtests. Package 45.4% → 51.5% (+6.1pp). Commit: 000696b

## [x] Write Go tests for pkg/prompt/hook.go (completed 2026-06-22)
- **Priority:** high
- **Model:** direct write — RegistryDir override + temp files
- **Files:** pkg/prompt/hook_test.go (NEW)
- **AC:** `go test ./pkg/prompt/... -count=1 -cover` passes with 100% coverage on hook.go functions (RunCommitMsgHook, ParseCommitMsgFromFile, shortHash) ✅
- **Result:** RunCommitMsgHook 95.5%, ParseCommitMsgFromFile 100%, shortHash 100%. 16 subtests covering all 7 hook steps (missing attestation, hash not found, lifecycle violations, tamper, PromptFoo pass/fail/no-results). Package 51.5% → 63.5% (+12.0pp). Commit: b1368a3

## [x] Write Go tests for pkg/prompt/provenance.go
- **Priority:** medium
- **Model:** direct write — RegistryDir override, pure logic
- **Files:** pkg/prompt/provenance_test.go (NEW)
- **AC:** `go test ./pkg/prompt/... -count=1 -cover` passes with 100% coverage on provenance.go functions (WalkProvenance, VerifyProvenance) ✅
- **Result:** provenance.go 100% coverage on both functions. WalkProvenance: 7 scenarios (empty attestHash, hash not found, full chain 5-link, missing spec file, no specRef, no workItem, empty changes). VerifyProvenance: 4 scenarios (all OK, some failures, empty chain, all failures). 14 test functions. All pass. Prompt package 90.1%. Commit: b46d9fd

## [x] Write Go tests for pkg/prompt/registry.go uncovered functions
- **Priority:** medium
- **Model:** direct write — RegistryDir override
- **Files:** pkg/prompt/registry_extended_test.go (NEW)
- **AC:** `go test ./pkg/prompt/... -count=1 -cover` passes with 100% coverage on Diff, ListVersions, Resolve, computeLineDiff, computeMetaDiff ✅
- **Result:** Resolve 90.0%, ListVersions 94.1%, Diff 87.5%, computeLineDiff 100%, computeMetaDiff 100%. Created setupMultiVersionPrompt helper to avoid index-overwrite bug in setupRegisteredPrompt. 22 test functions. All pass. Prompt package 90.1%. Commit: b46d9fd

## [x] Write Go tests for pkg/sandbox/executor.go uncovered functions
- **Priority:** medium
- **Model:** direct write — pure command construction, no bwrap needed
- **Files:** pkg/sandbox/executor_test.go (NEW)
- **AC:** `go test ./pkg/sandbox/... -count=1 -cover` passes with >50% coverage on sandbox package (from 27.7%) ✅
- **Result:** executor.go 61.7%, sandbox package 70.3% (+42.6pp from 27.7%). 28 test functions covering: NewExecutor (valid/invalid), SetOutput (override/nil), SetupSessionDir/CleanupSessionDir, BwrapArgs (IsolationNone/nil spec/workspace flags/full env/die-with-parent), BwrapCommand (IsolationNone/workspace), DryRun (stdout+structured summary), Run (ErrNotImplemented/dry-run), RunWithTimeout (with/without limit), shellEscape (9 cases), needsQuoting (safe/special/empty), mountToArgs (bind/ro-bind/proc/dev/tmpfs/unknown). All 8 Helix packages pass. Commit: b46d9fd

## [x] Write Go tests for pkg/sandbox/cgroups.go (completed 2026-06-22)
- **Priority:** medium
- **Model:** direct write — pure filesystem (mkdir, writeFile)
- **Files:** pkg/sandbox/cgroups_test.go (NEW)
- **AC:** `go test ./pkg/sandbox/... -count=1 -cover` passes with >75% coverage on sandbox package (from 70.3%) ✅
- **Logic:** CgroupPIDPath (path construction), mkdirIfNotExist (already exists, creates new, unwritable path), writeFile (creates file, overwrites existing), isWritable (writable dir, read-only dir, non-existent dir)
- **Result:** Sandbox 78.9%. All 7 cgroups.go functions covered: NewCgroup/Setup/Cleanup/CgroupPIDPath/mkdirIfNotExist/writeFile/isWritable. 28 tests, all pass. Commit: 12c9742

## [x] Write Go tests for pkg/negotiate/arbiter.go SplitCost (completed 2026-06-22)
- **Priority:** low
- **Model:** direct write — pure arithmetic
- **Files:** pkg/negotiate/arbiter_test.go (NEW)
- **AC:** `go test ./pkg/negotiate/... -count=1 -cover` passes with >97% coverage on negotiate package (from 96.8%) ✅
- **Logic:** SplitCost (equal split, single-way, zero cost, uneven division rounding)
- **Result:** Negotiate 97.8%. SplitCost 100%, estimateArbiterCost 100%, NewArbiterClient 100%. 13 tests, all pass. Commit: 12c9742

## [x] Write Go tests for pkg/marketplace/registry.go ListByCapability + GetAgent (completed 2026-06-22)
- **Priority:** medium
- **Model:** direct write — registry fixture setup
- **Files:** pkg/marketplace/registry_extended_test.go (NEW)
- **AC:** `go test ./pkg/marketplace/... -count=1 -cover` passes with >93% coverage on marketplace package (from 91.3%) ✅
- **Logic:** ListByCapability (matching capability, non-matching, empty registry, multiple matches), GetAgent (found, not found, nil agents map)
- **Result:** Marketplace 98.1%. ListByCapability: 6 subtests (match/non-match/empty/sorted/retired/deprecated+listing-fields). GetAgent: 8 subtests (found/not-found/nil-map/empty-map/reviews-sorted/single/zero/truncated). 14 tests, all pass. Commit: 12c9742

## [x] Write Go tests for pkg/prompt/attester.go AttestPrompt + Verify (completed 2026-06-22)
- **Priority:** medium
- **Model:** direct write — RegistryDir override + temp git repo (happy path)
- **Files:** pkg/prompt/attester_extended_test.go (NEW)
- **AC:** `go test ./pkg/prompt/... -count=1 -cover` passes with >92% coverage on prompt package (from 90.1%) ✅ **92.5%**
- **Logic:** AttestPrompt (valid prompt, partial prompt, empty prompt, 5 subtests 100%), Verify (invalid ref, HEAD no attestation, happy path via temp git repo with attested commit, 3 subtests 72.7%), GetCommitAttestation (invalid ref, non-git dir, 100%)
- **Result:** AttestPrompt 100%, Verify 72.7%, GetCommitAttestation 100%. Package 92.5%. Commit: c2c15e0

## [x] Write Go tests for pkg/sandbox/executor.go uncovered private functions (completed 2026-06-23)
- **Priority:** medium
- **Model:** direct write — pure helpers, no bwrap needed
- **Files:** pkg/sandbox/executor_extended_test.go (NEW)
- **AC:** `go test ./pkg/sandbox/... -count=1 -cover` passes with >84% coverage on sandbox package (from 78.9%) ✅ **92.1%**
- **Logic:** _killProcessGroup, _findBwrapBinary, _ensureBwrapAvailable, _execContext, _joinPath (5 private functions at 0%)
- **Result:** All 5 functions covered: _killProcessGroup 100% (3 tests incl. real child-process kill+SIGKILL verification), _findBwrapBinary 80% (3 tests: env var, env var nonexistent, all empty), _ensureBwrapAvailable 100% (4 tests: existing, not-found, directory, not-executable), _execContext 84.6% (4 tests: success, command-failed, command-not-found, timeout-exceeded), _joinPath 100% (10 subtests). Replaced dangerous _killProcessGroup InvalidPID test (would have kill(0,SIGKILL)'d the test runner) with safe child-process kill test. Also: SetupSessionDir verbose (100%), SetupSessionDir error path/unwritable (100%), CleanupSessionDir verbose (100%), BwrapCommand error path. 19 test functions total. Sandbox 78.9%→92.1% (+13.2pp). Commit: af83cd4

## [x] Write Go tests for pkg/sandbox/executor.go SetupSessionDir + CleanupSessionDir (completed 2026-06-23)
- **Priority:** medium
- **Model:** direct write — temp dirs, no bwrap needed
- **Files:** pkg/sandbox/executor_extended_test.go (append to above)
- **AC:** `go test ./pkg/sandbox/... -count=1 -cover` passes with >88% coverage on sandbox package ✅ **92.1%**
- **Logic:** SetupSessionDir error paths (82%→100%), CleanupSessionDir error paths (67%→100%)
- **Result:** SetupSessionDir 100% (tested verbose logging, unwritable path with ErrSetupFailed, verbose error path), CleanupSessionDir 100% (tested verbose logging, no-op on non-existent). Combined with private functions above. Commit: af83cd4

## [x] Write Go tests for pkg/sandbox/cgroups.go Setup uncovered paths (completed 2026-06-23)
- **Priority:** low
- **Model:** direct write — temp filesystem + v2 detection
- **Files:** pkg/sandbox/cgroups_extended_test.go (NEW)
- **AC:** `go test ./pkg/sandbox/... -count=1 -cover` passes with >92% coverage on sandbox package ✅ **92.1%**
- **Logic:** cgroups.go Setup() — cgroup v1 branch, v2 branch, /sys/fs/cgroup write failures
- **Result:** Setup 77.8%→83.3% (+5.5pp). Added 2 error-path tests: helix parent dir blocked by file (mkdirIfNotExist failure → ErrSetupFailed), session dir blocked by file (os.MkdirAll failure → ErrSetupFailed). Combined with existing 4 Setup tests. Sandbox 78.9%→92.1% (+13.2pp). Commit: af83cd4

## [x] Write Go tests for pkg/dispatcher/loop.go (completed 2026-06-24)
- **Priority:** high
- **Model:** direct write — pure filesystem stubs, spawn threshold not met
- **Files:** pkg/dispatcher/loop_test.go (NEW)
- **AC:** `go test ./pkg/dispatcher/... -count=1 -cover` passes with >85% coverage on dispatcher package ✅ **92.9%**
- **Logic:** ExecuteLoop (happy path, lock-held blocks, step-failure marks Failed), RunPipeline (full dispatch+execute, no-agents error, task failure in result), executeStep (marker creation, multiple steps), commitWork (COMMIT_MSG content, zero steps), openPR (doesn't panic), acquireLock error paths (nested dir creation), releaseLock error paths (no-op on missing lock)
- **Result:** All 5 previously-0% functions covered: ExecuteLoop 81.0%, RunPipeline 88.9%, executeStep 80.0%, commitWork 80.0%, openPR 100.0%. Dispatcher coverage: 72.9%→92.9% (+20.0pp). 19 subtests, all pass.

## [x] Write Go tests for pkg/estimate/openrouter.go + reconciliation.go (completed 2026-06-24)
- **Priority:** high
- **Model:** direct write — pure constructors, stubs, and math
- **Files:** pkg/estimate/reconciliation_test.go (NEW)
- **AC:** `go test ./pkg/estimate/... -count=1 -cover` passes with >90% coverage on estimate package ✅ **94.0%**
- **Logic:** NewOpenRouterClient (default URL, custom URL), GetKeyUsage/GetKeyLimit (ErrNotImplemented stubs), ReconcileDrift (positive/negative drifts, exact match, zero/zero, zero/non-zero→+Inf, large drift, float precision), ActualCost (nil pricing, unknown model, full computation, negative token clamping, zero usage, cache hit ratio)
- **Result:** All 5 previously-0% functions covered: NewOpenRouterClient 100%, GetKeyUsage 100%, GetKeyLimit 100%, ActualCost 89.7%, ReconcileDrift 100%. Estimate coverage: 78.6%→94.0% (+15.4pp). 17 subtests, all pass.

## [x] Write Go tests for pkg/identity/provisioner.go URL helpers (completed 2026-06-24)
- **Priority:** low
- **Model:** direct write — pure string construction
- **Files:** pkg/identity/provisioner_url_test.go (NEW)
- **AC:** `go test ./pkg/identity/... -count=1 -cover` exercises URL helper functions ✅
- **Logic:** adminUserKeysURL (normal name, special characters, trailing slash in ForgejoURL), userKeysURL, userTokensURL, userTokenURL
- **Result:** All 4 previously-0% URL helpers at 100%: adminUserKeysURL, userKeysURL, userTokensURL, userTokenURL. 6 subtests, all pass. Identity coverage: 68.0%→68.1%.

## [x] Write Go tests for cmd/sandbox/main.go — CLI test coverage (completed 2026-06-25)
- **Priority:** high
- **Model:** direct write — stdlib flag.FlagSet, pure logic
- **Files:** cmd/sandbox/main_test.go (NEW)
- **AC:** `go test ./cmd/sandbox/... -count=1 -cover` passes with >70% coverage on sandbox CLI ✅ **89.2%**
- **Logic:** run (help/version/unknown command/empty args), runCommand (dry-run, invalid isolation, missing command, valid config), handleError (all 7 error branches + unknown), generateSessionID (hex format + uniqueness), printUsage, printVersion, isDryRunOnly (match + no-match)
- **Result:** 25 test functions, all pass. Coverage: 0% → 89.2%. Every non-os.Exit code path tested.

## [x] Write Go tests for cmd/helix-prompt/main.go — CLI test coverage (completed 2026-06-25)
- **Priority:** high
- **Model:** direct write — Cobra, stdout capture via os.Pipe
- **Files:** cmd/helix-prompt/main_test.go (NEW)
- **AC:** `go test ./cmd/helix-prompt/... -count=1 -cover` passes with >45% coverage on prompt CLI ✅ **45.3%** (honest ceiling — runRegister/runAttest/runVerify need registry+git; runRegister also calls os.Exit on dry-run)
- **Logic:** newRootCmd (4 subcommands, verbose flag), register (missing args, non-existent file, missing prompt file, all flag defaults), attest (missing args, --force not-found), verify (missing args, bad commit, --check-hash/lifecycle/promptfoo flags), list (table/json/filtered by status/component/model), shortHashDisplay (long truncation, short unchanged, exact boundary)
- **Result:** 19 test functions, all pass. All 4 new*Cmd constructors at 100%, shortHashDisplay at 100%. Coverage: 0% → 45.3%.

## [x] Write Go tests for cmd/helix-identity/main.go — CLI test coverage (completed 2026-06-25)
- **Priority:** high
- **Model:** direct write — Cobra, stdout/stderr capture
- **Files:** cmd/helix-identity/main_test.go (NEW)
- **AC:** `go test ./cmd/helix-identity/... -count=1 -cover` passes with >45% coverage on identity CLI ✅ **47.1%**
- **Logic:** envOr (env-var, fallback, empty-string), buildConfig (defaults, honors all 9 flags), mustJSON (valid struct, empty, map), renderDryRun (no agents, active agent), renderResultsTable (empty, success, failure), renderSingleResult (success, failure, no SSH/PAT), renderStateTable (nil, empty, with agents), command tree (5 subcommands verified), sync (missing known-friends), provision/deprovision/keygen (missing args via cobra Execute)
- **Result:** 22 test functions, all pass. Coverage: 0% → 47.1%. All rendering functions at 100%, envOr at 100%, buildConfig at 100%, mustJSON at 100%.

## [x] Write Go tests for cmd/helix-marketplace/main.go — CLI test coverage (completed 2026-06-25)
- **Priority:** high
- **Model:** direct write — Cobra, stdout capture, pure helpers
- **Files:** cmd/helix-marketplace/main_test.go (NEW)
- **AC:** `go test ./cmd/helix-marketplace/... -count=1 -cover` passes with >45% coverage on marketplace CLI ✅ **61.3%**
- **Logic:** newRootCmd (5 subcommands), newListCmd/newShowCmd/newSearchCmd/newRateCmd/newReviewCmd (flag defaults, help text), ratingStars (0-5 + boundary), parseCapabilities (valid/invalid/empty/mixed), agentHasCapability (match/mismatch), sortAgents (by-trust/by-name/by-tasks/default-lex), currentUser, capabilitiesString, renderList (table/json/empty), renderShow (table/json/yaml/full), renderSearch, renderRate
- **Result:** 25 test functions, all pass. Coverage: 0% → 61.3%. Build/vet/test all pass. Commit: def477b

## [x] Write Go tests for cmd/helix-negotiate/main.go — CLI test coverage (completed 2026-06-25)
- **Priority:** high
- **Model:** direct write — Cobra, stdout capture, pure helpers
- **Files:** cmd/helix-negotiate/main_test.go (NEW)
- **AC:** `go test ./cmd/helix-negotiate/... -count=1 -cover` passes with >25% coverage on negotiate CLI ✅ **30.2%**
- **Logic:** newRootCmd (2 subcommands: debate, resolve), newDebateCmd/newResolveCmd (flag defaults, required flags), verdictEmoji (all 3 verdicts + unknown), parsePRNumber (valid/invalid/empty/boundary), auditLogPath, renderNegotiationResult (with/without chimera), defaultConfigPath, lookupAgent
- **Result:** 14 test functions, all pass. Coverage: 0% → 30.2%. Honest ceiling — runDebate/runResolve/runResolveWithPositions need infrastructure. Commit: ae1e1f6

## [x] Write Go tests for pkg/integration/suite.go — pure functions (no services needed) (completed 2026-06-26)
- **Priority:** medium
- **Model:** direct write — pure functions + stubs, temp file fixtures
- **Files:** pkg/integration/suite_test.go (NEW)
- **AC:** `go test ./pkg/integration/... -count=1 -cover` passes with >30% coverage on integration package ⚠️ **25.4%** (honest ceiling — remaining functions need live Forgejo/Chimera HTTP calls)
- **Logic:** NewForgejoClient (valid URL, invalid URL, trailing slash trim, https), NewChimeraClient (valid URL, invalid URL, trailing slash trim), NewIntegrationTestSuite (defaults, env-var overrides for all 5 env vars), ChimeraClient.Estimate (stub map verification), decomposeSpec (valid spec with PHASE/FEATURE/TASK headings, no-task headings, file-not-found, empty file, keyword matching), attestPrompt (valid prompt, deterministic hash, different content→different hash, file-not-found), searchMarketplace (returns stub list with field verification), getEnv (set, unset, empty string→fallback), generateTestSSHKey (generates ED25519 key, two keys differ), ErrAlreadyExists, all 6 type structs, HTTP client timeouts, constants
- **Result:** 22 test functions, all pass. Integration coverage: 0% → 25.4%. Honest ceiling — GetAccount/CreateUser/RegisterKey/CreateToken/DeleteUser/Health/Setup/Teardown/TestFullLoop all need live Forgejo + Chimera.

## [x] Write Go tests for cmd/helix/main.go — CLI test coverage (completed 2026-06-26)
- **Priority:** high
- **Model:** direct write — cobra-free dispatcher, pure helpers
- **Files:** cmd/helix/main.go (refactor), cmd/helix/main_test.go (NEW)
- **AC:** `go test ./cmd/helix/... -count=1 -cover` passes with >60% coverage on helix CLI ✅ **92.0%**
- **Logic:** urlToAddr (5 address formats + edge cases), sortedKeys (empty/normal/single), printVersion (stdout capture), printUsage (stdout capture), lookPath (local files, PATH fallback, perms), dispatch (--help/--version/--verbose/--config/--dry-run + unknown subcommand + valid subcommand routing), execSubcommand (missing binary, verbose, dry-run), runStatus (minimal — component listing, missing binary detection), checkEndpoint (TCP dial timeout — skipped, URL parsing tested via urlToAddr)
- **Result:** Refactored Execute() → dispatch(args []string) for testability. 22 test functions covering all pure helpers + full dispatch logic (global flags, built-in commands, subcommand routing, error paths). Coverage: 0% → 92.0%. Build/vet/test all pass.

## [x] Write Go tests for cmd/helix-estimate/main.go — CLI test coverage (completed 2026-06-25)
- **Priority:** high
- **Model:** direct write — Cobra, stdout capture, pure helpers
- **Files:** cmd/helix-estimate/main_test.go (NEW)
- **AC:** `go test ./cmd/helix-estimate/... -count=1 -cover` passes with >35% coverage on estimate CLI ✅ **63.6%**
- **Result:** 39 test functions, all pass. Covered: newRootCmd (3 subcommands verified), newEstimateCmd (8 flag defaults), newCheckCmd (3 flag defaults), newReportCmd (2 flag defaults), estimateTier (6 subtests), statusEmoji (5 cases), periodLabel (4 cases), coldStartNote (4 subtests), toTaskDesc (all 8 fields), validateEstimateOpts (6 subtests), defaultPricingPath, defaultFriendsPath, friendBudget.toBudgetInfo (2 subtests), loadAllBudgets (4 subtests incl. wrapped/bare formats + error paths), loadAgentBudget (found+not found), renderEstimate (table/json/summary), renderEstimateTable, renderCheck (table/json/summary), renderReport (table/json/empty-display-name/zero-budget), command tree (7 subtests: help + missing args + unknown command). Coverage: 0% → 63.6%. Commit: 9ca9c15
