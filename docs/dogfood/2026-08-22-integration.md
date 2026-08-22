# Dogfood Run 2026-08-22 — Real-Use Integration Report

**Project:** helix (Agent-First Code Platform, Go) · **Mode:** full dogfood loop
**Environment:** Forgejo :3030 live (HTTP 200) · Chimera :8765 live (36 models)
**Verdict:** 🟡 PROMISING-BUT-ROUGH (third run; pattern persists: local CLI surface
works, flagship trust/health paths still lie)

---

## 1. Promise (null hypothesis)

> "A user can operate an agent-first dev platform from the `helix` CLI: check
> platform health, estimate costs, capture ideas → co-author specs → generate
> contracts, render CI/deploy artifacts, manage prompt provenance, provision
> Forgejo agent identities, and run multi-model adversarial PR review — with
> trustworthy signals throughout."

## 2. What was actually done (real user workflow, not tests)

A realistic "plan a feature and prepare it for delivery" session, plus the
README quickstart verbatim:

| # | Step | Command | Result |
|---|------|---------|--------|
| 1 | Quickstart | `helix estimate check wojons "Write a Go HTTP server" --model deepseek-v4-pro --provider deepseek` | ✅ $0.08, AUTO_APPROVED, rc=0 (0.045s) |
| 2 | Quickstart | `helix marketplace search --capability go --min-trust 50` | ✅ 2 agents (wojons trust=85, llopez trust=52) |
| 3 | Health | `helix status` (no flags) | 🔴 **Overall: down**, 7 subsystems "timed out after 3s", rc=2 — platform actually healthy |
| 4 | Health | `helix doctor` | 🔴 Chimera "unreachable: timed out after 5s" — actually alive (see §3.1) |
| 5 | Identity | `helix identity status` (read-only) | ✅ rc=0, "(no agents provisioned)" — GAP-022 fix holds |
| 6 | Ideas | `idea capture` → `validate` → `prioritize` → `promote --to spec` | ✅ full pipeline (validate: needs_clarification with agent findings) |
| 7 | Spec | `spec create <idea>` → `review` → `gap-analysis` | ✅ works; ⚠ skeleton sections, score 9.6/100, no edit command (see §3.4) |
| 8 | Spec loop | hand-edit `~/.helix/specs/<id>.md` → `gap-analysis` again | ✅ score 9.6 → 17.4 (rate_limiting 35→70) — loop works, undocumented |
| 9 | Contract | `contract create` → `validate` → `freeze` → `diff` | ✅ create (scaffold, no endpoints), validate (⚠ no endpoints), freeze (sha256), diff "no breaking changes" |
| 10 | CI | `ci render` → `ci validate --path` | ✅ VALID (unit/integration/coverage/forgejo-service detected) |
| 11 | Deploy | `deploy render --kind agent\|caddy\|systemd`, `deploy tiers`, `deploy list` | ⚠ agent/caddy registries empty (no known-friends.json → silent empty `{"agent":{}}`); systemd renders 3 units; tiers ok |
| 12 | Prompt | `prompt list` / `prompt register coding-hermes v1` | ✅ hashes, status draft |
| 13 | Review | `helix review run --pr <url> --json` | 🔴 false success: 0/2 models, "No diff was provided…", exit 0 (see §3.2) |

**Time-to-first-success:** ~2 min (`helix version` + quickstart commands).
**Friction count:** 7 project-relevant friction points (listed §4).

## 3. Findings (each also filed on the board)

### 3.1 DF-017 (P1) — Health signal lies: "platform DOWN" on a healthy platform

- `curl http://localhost:8765/health` → 200 in **0.001s**; `/v1/health/live` → 200 in 0.001s.
- `curl http://localhost:8765/v1/health` (what `helix status`/`doctor` probe) → 200 but only after **~10.0s** (measured twice; it's the 36-model readiness check).
- `helix status` default probe timeout = **3s** → guaranteed false "unreachable" for 7 subsystems, "Overall: down", **rc=2**.
- `helix status --timeout 30s` on the same host → all 8 subsystems **healthy**, latency 10075ms.
- The GAP-025 audit procedure (in `docs/dogfood/diagnostics.md`) prescribes `curl --max-time 3 http://localhost:8765/v1/health` — same false-down, so the fleet's own audit now flags a healthy platform.
- Compounding: GAP-024 made `status` exit non-zero on down — automation gating on `helix status` now blocks on a healthy platform.

**Fix direction:** probe `/health` or `/v1/health/live` (1ms) in `cmd/helix/status.go:200-206` and `pkg/health/checker.go:108`; or raise default timeout ≥15s; update `pkg/health/remediation.go:222` + the audit note.

### 3.2 DF-018 (P1) — `helix review run` is a false-success demo stub

Live run (`review run --pr http://localhost:3030/helix/dogfood/pulls/1 --json`):
- Output: `models_agree: 0/2`, `consensus_level: divergent`, ONE finding from "primary":
  > "No diff was provided in the review request. The PR cannot be evaluated for correctness, security, or spec compliance." (evidence: "placeholder text '(Full diff would be fetched from Forgejo API)'")
- ...yet prints "Review complete" and **exits 0** (took 21.3s).
- Root causes (read source only after the live failure):
  1. `cmd/helix/review_ops.go:49-51` reviews a placeholder string — the PR diff is **never fetched** from Forgejo.
  2. `pkg/review/client_chimera.go:69` POSTs `/api/v1/deliberate` → **404**. Chimera's real route is `/v1/deliberate` (verified via chimera `/openapi.json` route inventory + live POST that answered "Unknown formation: budget" — a real response, not 404).
- The repo's own recovery runbook (`pkg/recovery/runbook.go:392`) already documents the correct `/v1/deliberate` path — the client drifted from it.

**Fix direction:** fetch the real diff via `pkg/forgejo`; fix the client path; when a panel member can't deliberate, degrade loudly (non-zero exit / explicit notice) instead of fabricating success.

### 3.3 DF-019 (P2) — `--json` flag surface inconsistency

- `helix estimate check ... --json` → `Error: unknown flag: --json`; the real flag is `--output json` — while `idea`, `spec`, `deploy`, `review` all accept `--json`. Hit live; muscle memory breaks.
- `estimate check` defaults `--pricing` and `--known-friends` to **repo-relative testdata paths** (`pkg/estimate/testdata/...`) — a CLI installed outside the repo needs explicit flags every call.

### 3.4 DF-020 (P2) — Spec co-authoring edit loop undiscoverable

- `spec create` makes 5 placeholder sections ("_Replace with intent and context._"); the CLI has **no edit command** (only create/review/gap-analysis/approve/show/list).
- Working loop proven: hand-edit `~/.helix/specs/<id>.md` → re-run `spec gap-analysis` → score 9.6 → 17.4. Discoverable only by reading source; `spec help` never mentions the store file.

### 3.5 Verified-still-open (already filed, no new tasks)

- **GAP-039** — README:84 "41 packages" vs `go list ./...` = **60** (50 pkg + 9 cmd + root). Still drifting.
- **GAP-040** — `helix --help` still lists both `review` and `adversarial` with no deprecation marker.
- **GAP-041** — `helix --help` still lists both `dispatch` ("distinct from dispatcher") and `dispatcher`.

### 3.6 Positives (fixes from prior runs held)

- README quickstart `estimate check` + `marketplace search` work **verbatim** (DF-003 era fixes held).
- `identity status` read-only without creds (GAP-022 held). `status` rc=2 on down (GAP-024 held — but see DF-017 for the false-down).
- `pipeline` canonical with `lifecycle` deprecated marker (GAP-035 held). Contract `-openapi` suffix now visible in `contract list` (DF-014 held). `ci render/validate`, `deploy systemd`, idea pipeline all clean.

## 4. Friction log (7 points)

1. `helix status` default → false CRITICAL (DF-017) — 3s wasted + wrong signal.
2. `helix doctor` chimera false-fail (DF-017) — 5s wasted.
3. `helix review run` fabricated success (DF-018) — worst kind of friction: none reported.
4. `review run` 21.3s stall before the stub verdict (DF-018).
5. `estimate check --json` unknown flag (DF-019).
6. `spec create` skeleton + no edit command (DF-020) — had to read the store file to proceed.
7. `deploy render --kind agent --json` → `{"agent": {}}` with no hint why (no known-friends.json → empty registry; only systemd kind renders content).

## 5. Verdict

🟡 **PROMISING-BUT-ROUGH.** The offline planning pipeline (idea → spec → contract → ci → deploy), quickstart, prompt registry, and identity read-only flow all work and feel fast. But the two signals a user must trust most — **health** and **multi-model review** — are both wrong in real use: one cries wolf, the other cries success. Consistent with the 2026-08-02/08-12 verdicts: value is real, trust is the blocker.
