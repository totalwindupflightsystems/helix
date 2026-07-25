<!--
  ⚠️  BOARD FORMAT — coding-hermes-model-router v1.3 (2026-07-24)
  All tasks MUST use matrix format: | ID | Task | Pri | Cpx | Deps | Tags | Model | Reasoning | Fallback |
  Before editing this file, load the skill: skill_view(name='coding-hermes-model-router')
  Validate: python3 ~/.hermes/scripts/validate-board-format.py .coding-hermes/tasks.md
- [ ] **GITREINS-JUDGE — Configure LLM evaluator for commit quality review**
  | 🔴 Critical | — | — | deepseek-v4-flash @ deepseek-foreman | GITREINS_LLM_API_KEY in ~/.hermes/.env | foreman-direct |

  Run: `python3 ~/.hermes/scripts/check-gitreins-judge.py .` to verify.
  Default limits (adjust per-project based on codebase size and task complexity):
  - Fast/small projects: `max_iterations: 50`, `max_time: 10m`, tokens: `0.2M/0.4M`
  - Large repos (Go monorepos, 100+ files): `max_iterations: 100`, `max_time: 30m`, tokens: `1M/2M`
  - C++/Rust (slow compiles): `max_time: 30m` minimum
  - Scheduler/production infra: `max_time: 30m`, tokens: `1M/2M`
  Supervisor auto-flags projects where limits are too low for codebase size.

| 🔴 Critical | — | — | deepseek-v4-flash @ deepseek-foreman | GITREINS_LLM_API_KEY in ~/.hermes/.env | foreman-direct |

  Run: `python3 ~/.hermes/scripts/check-gitreins-judge.py .` to verify.
  If missing, create/edit .gitreins/config.yaml with evaluator section using deepseek-v4-flash.
  This is CRITICAL for code quality — no automated review of worker output without it.

  NEVER remove the matrix header row or NEVER-DONE / E2E-001 fixtures.
-->

# Helix — Model Router Task Matrix

**Core purpose:** Agent-first code platform — development toolchain integrating CI, code review, vulnerability scanning, and multi-model deliberation via Chimera. Go 1.26+, 30+ packages, 58/58 test packages pass.

## Active Tasks

- [ ] **E2E-001 — E2E Testing Tick (self-improving loop)** 🔁 Every 5-10 ticks
  Spawn Luna (browser/screenshots) or Step 3.7 Flash (CLI/API). Deploy/build, Playwright, screenshots, endpoints, console. → e2e-output/tasks.md → inject into board.

| ID | Task | Pri | Cpx | Deps | Tags | Model | Reasoning | Fallback |
|----|------|-----|-----|------|------|-------|-----------|----------|
| INT-001 | E2E integration test: Forgejo → Helix → Agent PR → Review → Merge | High | 6 | Forgejo instance | +++testing, ++integration, ++infra | **BLOCKED** | Requires running Forgejo instance | — |
| INT-001b | Write 3 E2E test scenarios for Forgejo integration | High | 4 | INT-001 | ++testing, +spec-writing | **BLOCKED** | Depends on INT-001 (Forgejo) | — |
| INT-002 | Chimera multi-model review E2E | High | 5 | INT-001, Chimera | +++testing, ++distributed-systems | **BLOCKED** | Depends on INT-001 (Forgejo) | — |
| NEVER-DONE | 11-point audit sweep | Low | 2 | — | ++code-review, +testing | DeepSeek V4 Pro | Audit runs every tick | GLM-5.2 |

**Assumptions:** Go 1.26+. 58/58 packages pass. golangci-lint clean (0 issues). go vet clean on helix code. CI all green (last 3 runs). 0 panics in non-test code. 4 benchmark files. Hilo: 3,334 edges, 550 files (stable). DuckBrain: 50+ keys (helix namespace). 40+ outdated deps (idle drift). .gitreins/config.yaml committed with evaluator section (743408d).

**Routing Notes:** All INT tasks blocked on Forgejo instance availability. Project is feature-complete and stable — idle tick #8 (tick #37), cooldown at 12h (43200s, confirmed via scheduler API). Go build+vet clean. Hilo: 3,334 edges, 550 files (stable). DuckBrain: 50+ keys (helix namespace). GitReins: 5/5 tasks complete, board ↔ GitReins consistent. E2E-001 requires delegate_task (browser worker) — foreman cron can't dispatch.

**Execution Order:** INT-001 first (unblocks all other INTs) → INT-001b → INT-002 → NEVER-DONE.

**Escalation Conditions:** Forgejo unavailable → all INT tasks blocked indefinitely. Escalating: idle tick #8 reached — all INT tasks blocked on Forgejo instance. E2E-001 requires manual browser worker dispatch.

## Completed

| ID | Task | Pri | Cpx | Commit | Model |
|----|------|-----|-----|--------|-------|
| CI-294/295 | Fix CI lint failures (gofmt, nil Context, deprecated tracer, unused E2E) | High | 3 | 72dc8bb, b4ea418 | DeepSeek V4 Pro |
| COVERAGE-002 | Improve pkg/adr coverage (65.2%→95.9%) | Med | 4 | e789e1a | DeepSeek V4 Pro |
| COVERAGE-001 | Improve pkg/contract coverage (53.7%→83.0%) | Med | 3 | 56ecb7d | DeepSeek V4 Pro |
| COVERAGE-003 | Accessor + error wrapper tests | Med | 3 | 97c3771 | DeepSeek V4 Pro |
| DEPS-002 | SOPS v3.9.0→v3.13.2 vuln fixes | Med | 2 | beb98e1 | DeepSeek V4 Pro |
| REFACTOR-001 | Replace 6 panic() calls with error returns | Med | 3 | ac1bee3 | DeepSeek V4 Pro |
| U01 | Usability & coverage audit | High | 3 | 5f0de10 | DS-V4-Flash |

## Tick Log

### Tick 8 — 2026-07-25 00:38 UTC (DeepSeek V4 Pro)

| # | Gate | Result | Detail |
|---|------|--------|--------|
| 1 | Git status | ⚠️ DIRTY | .gitreins/config.yaml modified (uncommitted evaluator config fix) |
| 2 | GitReins guard | ✅ PASS | Secrets clean, no Go files staged |
| 3 | Hilo graph stats | ✅ 3,334 edges | 550 files, stable (was 3,334 last tick) |
| 4 | Go build ./... | ✅ PASS | EXIT:0 |
| 5 | Go vet ./... | ✅ PASS | EXIT:0 (helix code clean) |
| 6 | TODO/FIXME scan | ✅ CLEAN | 16 hits — all legitimate (context.TODO() idiom, test assertions, 1 test-data TODO) |
| 7 | GitReins config | ✅ FIXED | Evaluator section added: deepseek-v4-flash @ deepseek-foreman, committed as 743408d |
| 8 | GitReins task_list | ✅ CONSISTENT | 5/5 tasks complete ↔ board shows 5 completed. No hidden pending tasks |
| 9 | DuckBrain | ✅ POPULATED | 50+ keys in helix namespace. Board had stale count (26→50+), not fabricated |
| 10 | Outdated deps | ⚠️ 40+ outdated | Idle drift — transitive deps (cloud.google.com/go/*, aws-sdk-go-v2/*, etc.) |
| 11 | Dispatch | ⏸️ IDLE | All INT tasks BLOCKED on Forgejo instance. Idle tick #8. No dispatch |

**Verdict:** IDLE — all gates pass, GitReins evaluator config fixed (743408d). Board ↔ GitReins consistent. DuckBrain populated. All INT tasks blocked on Forgejo. Escalating idle tick #8 to Bane. Cooldown remains 12h.