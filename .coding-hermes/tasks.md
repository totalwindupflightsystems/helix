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

**Assumptions:** Go 1.26+. 58/58 packages pass. golangci-lint clean (0 issues). go vet clean on helix code (xds/envoyproxy deps show fork resource exhaustion — host-level, not project regression). CI all green (last 3 runs). 0 panics in non-test code. 4 benchmark files. Hilo: 3,334 edges, 550 files (stable). DuckBrain: 26 keys.

**Routing Notes:** All INT tasks blocked on Forgejo instance availability. Project is feature-complete and stable — idle tick #7 (tick #36), cooldown at 12h (43200s, confirmed via scheduler API). Host fork resource contention on xds/envoy transitive deps observed in go vet but helix code vet clean. E2E-001 requires delegate_task (browser worker) — foreman cron can't dispatch.

**Execution Order:** INT-001 first (unblocks all other INTs) → INT-001b → INT-002 → NEVER-DONE.

**Escalation Conditions:** Forgejo unavailable → all INT tasks blocked indefinitely. Escalating: idle tick #7 reached — all INT tasks blocked on Forgejo instance. E2E-001 requires manual browser worker dispatch.

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
