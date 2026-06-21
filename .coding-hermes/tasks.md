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

## [ ] Write Go tests for pkg/marketplace/discovery.go
- **Priority:** medium
- **Model:** MiniMax-M3
- **Files:** pkg/marketplace/discovery_test.go (NEW)
- **AC:** `go test ./pkg/marketplace/... -count=1 -cover` passes with >80% coverage on discovery.go
- **Logic:** FindAgents, LoadBalance, matchPercent, budgetUtilization
