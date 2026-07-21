# Helix — Model Router Task Matrix

> **Core purpose:** Agent-First Code Platform — humans and AI agents as equal participants in the SDLC. Forgejo integration, sandboxed execution, adversarial review, trust-tiered task assignment.

> **Foreman:** deepseek-v4-pro @ deepseek | **DuckBrain:** helix (13 entries — populated)
> **Last tick:** 2026-07-21 22:15 UTC | **Tick #11** | **Build:** PASS | **Commit:** `708f1c1` + `d8f70a8`

```
ID | Task | Priority | Complexity | Deps | Tags | Model | Reasoning | Fallback
```

## Remaining Tasks

| ID | Task | Pri | Cpx | Deps | Tags | Model | Lvl | Fallback |
|----|------|-----|-----|------|------|-------|-----|----------|
| INT-001 | E2E integration test: Forgejo → Helix → Agent PR → Review → Merge — helpers + API methods done in c6355c7 | High | 6 | — | ++testing, ++integration, ++multi-step-reasoning, ++distributed-systems | DeepSeek V4 Pro | High | GPT-5.6 Sol |
| INT-001b | Write 3 E2E test scenarios (happy path, 409 idempotent, error path) using helpers from c6355c7 | High | 4 | INT-001 | ++testing, ++integration | DeepSeek V4 Pro | High | GPT-5.6 Sol |
| INT-002 | Chimera multi-model review E2E: real LLM calls, not stubs | High | 5 | INT-001 | ++testing, ++api-use, ++multi-step-reasoning | GLM-5.2 | High | DeepSeek V4 Pro |
|| PROD-001 | **SPEC DONE — Impl Pending.** SOPS-backed SecretStore: interface, sops.go, CLI CRUD, config integration (spec committed 94ac4b7) | Medium | 5 | — | ++security, ++architecture, ++distributed-systems | GPT-5.6 Sol | High | DeepSeek V4 Pro |
|| PROD-001a | ✅ Implement `pkg/security/store` — `SecretStore` interface, SOPS-backed store, 23 unit tests | Medium | 3 | PROD-001 | ++backend, ++go-coding, ++terminal | GLM-5.2 (foreman-direct) | High | MiniMax-M3 |
| PROD-002 | Rate limiting on Forgejo API calls | Medium | 3 | — | ++backend, ++performance | DeepSeek V4 Flash | Medium | GLM-5.2 |
| PROD-003 | Metrics + tracing (OpenTelemetry) | Low | 4 | — | ++backend, ++infra | DeepSeek V4 Pro | Medium | GLM-5.2 |

## Never-Done Audit (Standing)

| ID | Task | Pri | Cpx | Deps | Tags | Model | Lvl | Fallback |
|----|------|-----|-----|------|------|-------|-----|----------|
| NEVER-DONE | 11-point audit across all 55+ packages | Low | 3 | — | ++terminal, ++code-review, ++file-editing | DeepSeek V4 Pro | Medium | GLM-5.2 |

## Tick #12 — 2026-07-21 17:34 UTC — NEVER-DONE Audit + PROD-001 Spec Committed

**11-point audit:** All 11 checks PASS. No spec gaps, test gaps, outdated deps, stubs, or wiring issues found.

**Pre-existing uncommitted work discovered:** PROD-001 spec (293 lines, 10 sections) was sitting untracked on disk with SOPS/age deps in go.mod. Spec is complete with exact Go interfaces, CLI surface, error catalog, and build order. Committed as `94ac4b7`.

| # | File | Status | Notes |
|---|------|--------|-------|
| 1 | `specs/secret-management.md` | ✅ Committed (94ac4b7) | Complete 10-section PROD-001 spec |
| 2 | `go.mod` + `go.sum` | ✅ Committed | SOPS v3.9.0 + filippo.io/age v1.1.1 |

**CI:** Build ✅ Test ✅ Integration ✅ | Lint ❌ (pre-existing unused E2E helpers)

**Build:** `go build ./...` ✅ | **Vet:** `go vet ./...` ✅ | **Tests:** All packages `ok` ✅

**Next:** Spawn worker for PROD-001a — implement `pkg/security/store` (interface + SOPS store + tests)

**Found uncommitted worker output in dirty workdir.** Prior tick's worker had extracted 5 files >1,000 lines but never committed. 3 of 5 extractions were partial (original code not pruned). Foreman completed the pruning and committed all 5.

| # | File | Before | After | Status |
|---|------|--------|-------|--------|
| 1 | `cmd/helix/review.go` | 1,441 lines | 467 lines | ✅ Extracted to 6 files (review_bias, review_crypto, review_dashboard, review_evidence, review_fp, review_ops) |
| 2 | `cmd/helix/incident.go` | 1,183 lines | 244 lines | ✅ Extracted to 4 files (incident_attr, incident_crud, incident_patterns, incident_stats) |
| 3 | `pkg/audit/chain.go` | 1,064 lines | 750 lines | ✅ Extracted to 2 files (chain_validators, chain_checker) |
| 4 | `pkg/design/review.go` | 1,138 lines | 257 lines | ✅ Extracted to 2 files (review_agents, review_findings) |
| 5 | `pkg/verify/surveillance.go` | 1,081 lines | 347 lines | ✅ Extracted to 3 files (surveillance_aggregator, surveillance_alert, surveillance_monitor) |

**Build:** `go build ./...` ✅ | **Vet:** `go vet ./...` ✅ | **Tests:** All 30+ packages `ok` ✅ | **Guard:** PASS ✅

**Commits:**
- `708f1c1` — feat: split 5 files >1,000 lines (21 files, +4013/−2904)
- `d8f70a8` — fix: complete incident.go extraction (−973 lines)

**CI:** Running (2 builds in_progress). Previous CI failure on `13e101a` was pre-existing golangci-lint on unused E2E helpers in `pkg/integration/suite_e2e_test.go` (expected — helpers for INT-001 which isn't implemented yet).

**DuckBrain:** 2 entries written (event + status). Namespace now has 13 entries total.

**What's left:** INT-001/001b/002 blocked by Forgejo availability. PROD tasks actionable. Cooldown at max (43200s = 12h).

## Tick #13 — 2026-07-21 17:44 UTC — PROD-001a Complete (Store Package)

**PROD-001a — SecretStore interface + SOPS-backed implementation + 23 tests.**

Prior worker produced partial output (interface + errors, 218 lines). Foreman completed sops.go (529 lines, full CRUD + Rotate + atomic file writes) and wrote sops_test.go (570 lines, 23 tests including error catalog, helpers, auto-init, CRUD lifecycle, rotate, concurrency, corruption handling). All 23 tests pass.

| # | File | Lines | Status |
|---|------|-------|--------|
| 1 | `pkg/security/store/store.go` | 77 | ✅ SecretStore interface + SecretMeta + Provider constants |
| 2 | `pkg/security/store/errors.go` | 142 | ✅ Error catalog (7 sentinels + SecretError interface + Wrap helpers + AsSecretError) |
| 3 | `pkg/security/store/sops.go` | 529 | ✅ SOPSSecretStore: Get, Set, Delete, List, Rotate + SOPS integration + atomic writes |
| 4 | `pkg/security/store/sops_test.go` | 570 | ✅ 23 tests: error catalog, helpers, auto-init, CRUD, rotate, concurrency, edge cases |

**Build:** `go build ./...` ✅ | **Vet:** `go vet ./...` ✅ | **Tests:** 30+ packages all `ok` ✅ | **Guard:** PASS ✅

**Commit:** `418a91b` — feat: PROD-001a — implement pkg/security/store (SOPSSecretStore + tests)

**Next:** PROD-002 (rate limiting) is the highest-priority actionable task. INT-001/001b/002 remain blocked on Forgejo.

## Assumptions (UPDATED Tick #13)
- Build/Tests GREEN — host exhaustion cleared
- CI may fail on pre-existing golangci-lint lint in suite_e2e_test.go (unused E2E helpers)
- Forgejo instance NOT locally available (blocks INT-001/001b/002)
- PROD-001/002/003 actionable and could be worked next
- Cooldown at 43200s (max idle — next tick will be NEVER-DONE audit)

## Completed
**Tick #11 (2026-07-21 22:15 UTC):** QUALITY-001 — split 5 files >1,000 lines (review.go, incident.go, chain.go, design/review.go, surveillance.go). 21 new files, 17 modified. All build/test/guard pass. Pushed 2 commits.

**Tick #10 (2026-07-21 16:44 UTC):** DUCKBRAIN-001 — Helix DuckBrain namespace populated with 11 entries. Host thread exhaustion cleared.

**Phase 1-12:** 30+ tasks completed (see prior board state).
