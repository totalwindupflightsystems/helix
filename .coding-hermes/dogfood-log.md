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

