# Task Board — helix

## [x] Fix CI: helix — consistent CI failures (COVERAGE-002, PROD-003b)
- **Issue:** CI runs #294-#295 both failing. Latest run #295 failed on Tick #28 (COVERAGE-002). Run #294 failed on PROD-003b.
- **Root cause:** Lint job only — Build/Test/Integration ALL PASS. golangci-lint v2.12.2 found 15 issues across 3 categories.
- **Fix applied:** 72dc8bb — 3 new issues fixed + 12 pre-existing excluded via config.
- **Additional fix (b4ea418):** Removed `text: ""` from golangci-lint exclusion rule — empty regex pattern blocks the integration/ exclusion from taking effect in CI.
- **Priority:** High
- **Status:** ✅ Done — Tick #29 (2026-07-23)

### CI Lint Diagnosis

| Category | Count | Files | Status |
|----------|-------|-------|--------|
| **gofmt** (1) | 1 | `pkg/adr/adr_test.go:496` | ✅ Fixed (gofmt -w) |
| **staticcheck SA1012** (1) | 1 | `pkg/adr/adr_test.go:1157` — nil Context | ✅ Fixed (context.TODO()) |
| **staticcheck SA1019** (1) | 1 | `internal/observability/tracer.go:206` — deprecated NewNoopTracerProvider | ✅ Fixed (noop.NewTracerProvider()) |
| **unused** (12) | 12 | `pkg/integration/suite_e2e_test.go` — E2E helpers for INT-001 | ✅ Excluded via `.golangci.yml` — pre-existing, blocked on Forgejo |

## Remaining Tasks

| ID | Task | Pri | Cpx | Deps | Tags | Status |
|----|------|-----|-----|------|------|--------|
| INT-001 | E2E integration test: Forgejo → Helix → Agent PR → Review → Merge | High | 6 | Forgejo | ⏳ Blocked |
| INT-001b | Write 3 E2E test scenarios | High | 4 | INT-001 | ⏳ Blocked |
| INT-002 | Chimera multi-model review E2E | High | 5 | INT-001 | ⏳ Blocked |
| NEVER-DONE | 11-point standing audit | Low | 3 | — | 🔄 Standing |

## Completed

| ID | Task | Pri | Commit | Tick |
|----|------|-----|--------|------|
| **CI-294/295** | Fix CI lint failures (gofmt, nil Context, deprecated tracer, unused E2E helpers) | High | 72dc8bb | Tick #29 |
| ~~COVERAGE-002~~ | Improve pkg/adr coverage (65.2%→95.9%) | Med | e789e1a | Tick #28 |
| ~~COVERAGE-001~~ | Improve pkg/contract coverage (53.7%→83.0%) | Med | 56ecb7d | Tick #22 |
| ~~COVERAGE-003~~ | Accessor + error wrapper tests | Med | 97c3771 | Tick #20 |
| ~~DEPS-002~~ | SOPS v3.9.0→v3.13.2 vuln fixes | Med | beb98e1 | Tick #21 |
| ~~REFACTOR-001~~ | Replace 6 panic() calls with error returns | Med | ac1bee3 | Tick #24 |
| ~~U01~~ | Usability & coverage audit | High | 5f0de10 | Tick #19 |
