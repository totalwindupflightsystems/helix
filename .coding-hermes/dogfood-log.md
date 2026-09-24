# Helix Dogfood Log

## 2026-08-02 — 🟡 PROMISING-BUT-ROUGH

**Promise:** "A user can run the `helix` CLI (and 9 standalone binaries) to operate an agent-first dev platform — estimate costs, attest prompts, generate/validate CI workflows, render deployment artifacts, sandbox commands, and enforce a 5-check pre-merge gate — all documented in README/AGENTS.md/specs."

**Reality:** The offline CLI surface largely works (idea pipeline, ci render/validate, deploy render, sandbox, env-check, vuln scan, doctor, recovery, backup, incident — all verified live). But the flagship enforcement promise fails: the pre-receive merge gate prints REJECTED yet exits 0, so it **never blocks a push** (proven with a live bare-repo push), and it fails OPEN when diff-tree errors. Most README quickstart examples fail as written (`estimate --task`, `check --spec`), and subcommand `--help` shows the root menu everywhere.

**Time-to-first-success:** ~4 min (build + `helix version`/`status`). First documented-example task failed immediately (`estimate --task` → unknown flag).

**Top 3 findings:**
1. **DF-001 (P0)** — `helix mergegate hook` exits 0 on REJECT; official wrapper `exec`s the binary → gate can never block a push.
2. **DF-002 (P1)** — `helix <sub> --help` prints root help; flags undiscoverable without reading source.
3. **DF-003 (P1)** — README quickstart examples broken (`--task`/`--spec` flags don't exist); working form is positional + `--model`.

**Friction count:** ~10 project-relevant friction points over the session (13 total incl. 2-3 host-environment ones: intermittent fork EAGAIN on this host).

**Artifacts:** docs/dogfood/2026-08-02-integration.md · docs/dogfood/diagnostics.md · skills/helix-usage/SKILL.md · board tasks DF-001..DF-009.

## 2026-08-12 — 🟡 PROMISING-BUT-ROUGH (second run)

**Promise:** "A user can provision real agent identities into Forgejo
(account + SSH key + PAT), operate channels and sources, generate/validate
contracts, and trust that the merge gate actually blocks bad pushes."

**Reality:** The flagship now WORKS — a real agent was provisioned into live
Forgejo v1.21.11 (user id 5, SSH key, `helix-identity-pat`, state file) in
~1s; every 2026-08-02 blocker (DF-001/002/004/005, GAP-001/004/005/007) was
re-verified fixed in reality; channels, sources, contracts, identity CLI all
function. But the repair path lies: after a partial provision failure
(missing BasicAuth), retry reports `action=unchanged` + exit 0 while Forgejo
has 0 tokens (proven); deprovision leaves the SSH key registered server-side
and fails to archive the local key; `source test` false-fails Forgejo's own
API; contract ids gain a `-openapi` suffix that breaks validate/freeze/diff.

**Time-to-first-success:** ~3 min (build + `helix status` healthy in 3.4s).
First real task (identity create) succeeded immediately; provisioning took
2 friction cycles (known-friends schema, BasicAuth requirement).

**Top 3 findings:**
1. **DF-011 (P1)** — provision idempotency = user-existence check only; missing PAT/SSH key never repaired; retry after partial failure exits 0 claiming "unchanged".
2. **DF-012 (P2)** — deprovision leaves SSH key active in Forgejo + local key archive fails (missing MkdirAll) — deprovisioned agent can still SSH-auth.
3. **DF-013 (P2)** — `helix source test` false-fails valid REST sources whose base URL returns non-200 (live Forgejo /api/v1 → 404).

**Friction count:** 7 project-relevant friction points (identity verify
--name vs --hid, known-friends schema, BasicAuth surprise, contract id
suffix, contract empty generation, source test 404, silent unknown flags).

**Artifacts:** docs/dogfood/2026-08-12-integration.md · diagnostics.md addendum · skills/helix-usage/SKILL.md (identity/channel/source/contract sections) · board tasks DF-011..DF-016.

## 2026-08-22 — 🟡 PROMISING-BUT-ROUGH (third run)

**Promise:** "A user can operate the platform from the `helix` CLI — health
checks, cost estimates, idea→spec→contract→CI→deploy planning, prompt
provenance, identity, and multi-model adversarial PR review — with trustworthy
signals throughout."

**Reality:** The offline planning pipeline is in good shape: README quickstart
(`estimate check`, `marketplace search`) works verbatim; idea capture→validate→
prioritize→promote, spec create/review/gap-analysis/approve, contract create/
validate/freeze/diff, ci render/validate, deploy systemd, prompt register/list,
and read-only `identity status` all rc=0 and fast. But the two trust signals a
user depends on most are wrong: `helix status`/`doctor` report the platform
CRITICALLY DOWN on a healthy host (3s/5s probe timeout vs chimera `/v1/health`
~10s readiness latency; `--timeout 30s` proves all 8 subsystems healthy), and
`helix review run --pr` fabricates success (never fetches the diff, chimera
leg 404s on `/api/v1/deliberate` vs real `/v1/deliberate`, models_agree 0/2,
yet exit 0 with a "No diff was provided" verdict).

**Time-to-first-success:** ~2 min (version + quickstart). First failure: `helix
status` default (false down, DF-017).

**Top 3 findings:**
1. **DF-017 (P1)** — status/doctor false "DOWN": default probe timeout < chimera
   `/v1/health` latency; GAP-024's non-zero exit now gates on a false alarm;
   GAP-025's audit probe prescription inherits the bug.
2. **DF-018 (P1)** — `review run` false-success stub: placeholder diff, dead
   `/api/v1/deliberate` path, 0/2 models, exit 0.
3. **DF-019 (P2)** — `estimate check --json` unknown flag (`--output json` is
   real); testdata-relative pricing/known-friends defaults. **DF-020 (P2)** —
   spec edit loop undocumented (hand-edit `~/.helix/specs/<id>.md`; proven
   score 9.6→17.4).

**Friction count:** 7 project-relevant points.

**Artifacts:** docs/dogfood/2026-08-22-integration.md · diagnostics.md addendum
· skills/helix-usage/SKILL.md field notes · board tasks DF-017..DF-020.
**Foreman wake:** skipped — cooldown 21600 = fleet.toml operator pin (board
precedent GAP-024..026: never PUT below pin); push channel blocked
(INFRA-GH-001, human). Commits local-only.
2026-09-01 | PROMISING-BUT-ROUGH | 181s t2fs | friction 8 | 5 findings

2026-09-04 | PROMISING-BUT-ROUGH | 117s t2fs | friction 11 | 5 findings

## 2026-09-24 — 🟡 PROMISING-BUT-ROUGH (fifth run, angle: L3 documented-contract + fresh machine)

**Promise:** "A user can clone the documented origin, run the README Quickstart
(`make build`, `make test`), install the 9 CLIs, and drive the platform — every
component in the README table reachable at the CLI named there, and the flagship
`helix dispatch` turning a spec file into agent work."

**Angle:** previous runs (08-02, 08-12, 08-22 deep; 09-01, 09-04 short) swept the
offline CLI, identity/Forgejo, contracts, trust and verify. This run took the two
surfaces none of them touched: **the README-as-contract CLI table** and
**install-from-scratch on an ephemeral bunker box**.

**Reality:** The offline core is good — `make build` succeeds from a clean clone
in 214s, `make install PREFIX=$HOME/.local` works without sudo, and the quickstart
CLIs are correct and fast (13–18ms). But the two things a first-time user does
first both fail: `make test` is **deterministically red on the default branch**
(`FAIL pkg/prompt`), and the README component table names **three CLIs that do not
exist**. The flagship `helix dispatch` cannot decompose 15 of the repo's own 24
specs, including `specs/SPECIFICATION.md`. On a bare Debian box the documented
`make build` cannot even start — Go is not a documented prerequisite.

**Time-to-first-success:** ~4 min (Go 9s + `make build` 214s + smoke).
**Time-to-first-failure:** ~1 min — documented `make build` on a box without Go.
**Friction count:** 11 project-relevant points (+1 host-environment note: los-03
agents share `/tmp`, which produced one false reading — see diagnostics).

**Top 3 findings:**
1. **DF-HELIX-6 (P0)** — default branch red: `make test` fails on a clean clone.
   `TestVerify/head_commit_with_path_style_attestation` asserts on ambient `HEAD`;
   board-writer commits carry no `Prompt:` trailer → fails on most commits.
   Reproduced on the fresh clone + this host + a pristine HOME; CI run 34507859593
   Test job agrees; red on 4 of the last 8 master pushes.
2. **DF-HELIX-7 (P0)** — `helix dispatch` decomposes 9 of 24 own specs; the
   `currentDesc` body-capture is dead code, so task steps are title-only with empty
   `expected_output`.
3. **DF-HELIX-8 (P1)** — README table's `helix adversarial` / `helix coordinator` /
   `helix health` are dead names; the existing docs-consistency CI gate counts
   `pkg/...` strings and is structurally blind to CLI names.

**Also:** DF-HELIX-9 (Go prerequisite undocumented; quickstart omits FORGEJO_ADMIN_*
exports), DF-HELIX-10 (agent private key written to repo CWD, .gitignore-unprotected),
DF-HELIX-11 (`trust_tier` gate passes vacuously on an empty diff).

**Deliverables:** docs/dogfood/2026-09-24-integration.md · diagnostics.md addendum
(CI-gate behavior, the ambient-HEAD test, the decomposer, the key-in-cwd trap, the
U-GAP-054 foreign-AGENTS.md injection reproduced in this repo) ·
skills/helix-usage/SKILL.md field notes · board rows DF-HELIX-6..11 + task_created
events.

**Perf:** measured, nothing actionable — `helix version` 13.3ms, `estimate check`
17.0ms, `marketplace search` 18.0ms (hyperfine, 20 runs). No PERF rows filed.

**Install leg:** 2026-09-24 | PROMISING-BUT-ROUGH | install_seconds=223 (make build
214s after a 9s user-local Go install; first attempt rc=2, `make: go: No such file
or directory`) | bunker=las-bunker-03 agent=3bc183ac (destroyed) | smoke=partial
(version/estimate/marketplace ok; status rc=2 no stack; identity provision rc=3
missing documented env) | make test=FAIL (DF-HELIX-6)

