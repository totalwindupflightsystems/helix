# Helix Coding Tasks — Foreman Queue

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

## [ ] Write Go tests for pkg/prompt/lifecycle.go
- **Priority:** medium
- **Model:** MiniMax-M3
- **Files:** pkg/prompt/lifecycle_test.go (NEW)
- **AC:** `go test ./pkg/prompt/... -count=1 -cover` passes with >80% coverage on lifecycle.go
- **Logic:** AllowedForAttestation, ValidTransition, AllowedTransitions, DeprecationGrace, ValidateTransition

## [ ] Write Go tests for pkg/prompt/registry.go
- **Priority:** medium
- **Model:** MiniMax-M3
- **Files:** pkg/prompt/registry_test.go (NEW)
- **AC:** `go test ./pkg/prompt/... -count=1 -cover` passes with >80% coverage on registry.go
- **Logic:** Register, Lookup, LookupByComponent, List, UpdateStatus, TransitionStatus, loadIndex, saveIndex, writeMetadata, readMetadata, writePrompt, entryToPromptVersion
