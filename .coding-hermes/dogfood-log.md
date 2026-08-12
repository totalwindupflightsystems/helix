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
