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
