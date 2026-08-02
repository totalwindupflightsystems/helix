# Helix Integration Report — 2026-08-02

Dogfood run: **what actually happens when a real user picks up the Helix CLI
and tries to use it.** Every command below was executed against the repo at
`/home/kara/helix` (Go 1.26 toolchain, Linux). Prebuilt binaries existed in
the repo root; a fresh `go build ./cmd/...` (58 packages) also succeeds.

Verdict: 🟡 **PROMISING-BUT-ROUGH** — the value is real, usability is the blocker,
and one P0 bug breaks the core enforcement promise.

---

## 1. What works (verified live, with the exact commands)

| Capability | Command that worked | Notes |
|---|---|---|
| Build | `go build ./cmd/...` | 58 packages, exit 0 |
| Version | `helix version` | `helix 0.1.0-dev` |
| Platform diagnostics | `helix doctor` | Excellent output: 1 passed, 6 failed, 2 warnings, precise per-check errors |
| Config validation | `helix config env-check` | Great: masks secrets (`sk-ab…b5`), lists missing vars, exit 1 |
| Idea pipeline | `helix idea capture --title T --body B` → `validate <id>` → `prioritize` | Full offline loop, instant; named agent findings (`@assumption-buster`, `@architecture-fit`) |
| Cost estimation | `helix estimate estimate "<task>" --model deepseek-v4-pro --provider deepseek` | Sensible cache-aware estimate ($0.08 for a Go HTTP server task) |
| Prompt registry | `helix prompt list` / `register <c> <v>` | Works with **nested** layout `prompts/<c>/<v>/prompt.md` |
| CI workflow gen | `helix ci render` → `helix ci validate --path workflow.yml` | Generated a full Forgejo Actions workflow (unit + integration with Forgejo service); validator confirmed structure/coverage/forgejo-service |
| Deploy artifacts | `helix deploy render --kind systemd --json`, `deploy tiers` | systemd units + agent tier registry |
| Sandbox | `helix sandbox run -- echo hi` | Bubblewrap isolation executes (19 ms) — once the `sandbox` binary is built and on PATH |
| Merge gate checks | `helix mergegate checks`, `hook` | 5-check evaluation works — but see P0 below |
| Vuln scan | `helix vuln scan` | `go: 0 findings` on the repo |
| Ops surfaces | `helix recovery`, `helix backup status`, `helix incident list`, `helix trust` (usage) | Recovery matrix, backup strategy table, empty incident DB |

## 2. The P0: the merge gate does not gate

**Claim (README step 9/13):** GitReins pre-receive hook / merge gate blocks
pushes that fail quality checks.

**Evidence chain (all reproduced live):**

1. Direct: `echo "<old> <new> refs/heads/main" | helix mergegate hook --trust veteran`
   → prints `helix-pre-receive: ✗ REJECTED` (commit-attestation: commits lack
   `Co-authored-by:`/`Helix-Agent:` trailers) — but the process **exits 0**
   (`rc=0 subcommand=mergegate`). Reproduced with the fresh build too.
2. Official wrapper `scripts/helix-pre-receive.sh` ends with
   `exec "$HELIX_BIN" mergegate hook ...` → the wrapper's exit code *is* the
   binary's = 0. Its header documents "1 = Push BLOCKED" — that never happens.
3. Live push: bare repo with the hook installed, push an unattested commit →
   hook JSON says `"allowed": false` but **the ref still moves** (push exit 0).
4. Fail-open: when the hook's internal `git diff-tree` call failed under host
   fork pressure, it reported `"allowed": true` with reason
   `could not collect changed files: … resource temporarily unavailable
   (allowing — likely a new branch)` — an *error* became an *approval* on a
   protected branch.

**Fix direction (board task DF-001/DF-009):** exit non-zero when any ref is
rejected; fail closed on unexpected errors (only genuine new-branch detection
may skip); add a push-to-bare-repo integration test.

## 3. Frictions that cost real time (in order hit)

1. **`--help` lies (DF-002).** `helix estimate --help`, `ci render --help`,
   `mergegate check --help` all print the *root* menu, exit 0. Flag discovery
   requires running the bare subcommand (`helix ci` shows usage) or reading
   `cmd/helix/*.go`. I had to grep source for `ci validate`'s `--path` flag.
2. **README examples broken (DF-003).** `helix-estimate check --spec` →
   `unknown flag: --spec` (it's `--spec-file`); unified help's
   `helix estimate --task "…"` → `unknown flag: --task` (the estimate command
   takes a positional description + requires `--model`).
3. **CWD-dependent defaults (DF-004).** Running `helix estimate` from outside
   the repo root → `CONFIG_ERROR: read pricing file
   "pkg/estimate/testdata/pricing.yaml"` (relative default). A CLI that only
   works from its checkout root.
4. **Prompt layout mismatch (DF-005).** `helix prompt register coding-hermes v1`
   fails: it wants `prompts/coding-hermes/v1/prompt.md`, the repo stores
   `prompts/coding-hermes/v1.md` (AGENTS.md's documented commit-rule layout).
5. **Unified CLI isn't unified (DF-006).** `helix estimate` / `sandbox` etc.
   shell out to sibling binaries; from another cwd you get
   `subcommand "helix-estimate" not found` (helpful install hint, but README
   never says to install them or put them on PATH).
6. **Health probes misdiagnose (DF-007).** `helix status` takes 5s+ —
   sequential probes each hang on `http://localhost:8765/v1/health` (the
   Chimera server 404s fast on other routes but *hangs* on the health route);
   Forgejo shows "degraded HTTP 404" though the service on :3000 answers
   `/health` 200 (it's a node stub — probe path ≠ reality).
7. **PromptFoo config drift (DF-008).** `.promptfoo.yaml` references
   `prompts/agent-identity/v1.1.0/prompt.md`; only `v1.0.0` exists →
   `prompt test` fails, CI would too.
8. **Crash under pressure (minor).** `helix audit`/`security` SIGABRT
   (`pthread_create failed`) when the host hit fork EAGAIN — host-caused, but
   a CLI should fail with a clean error.

## 4. The "aha" — how to use Helix for real (right way)

- **Discover flags:** run the bare subcommand (`helix ci`, `helix mergegate`,
  `helix idea`, `helix deploy`) — the per-command usage blocks are good.
  Ignore `--help` at subcommand level.
- **Estimate:** `helix estimate estimate "<task>" --model <m> --provider <p>`
  from the repo root (or pass `--pricing /abs/path/pricing.yaml`).
- **Prompts:** register with nested layout: `helix prompt register <c> <v>`
  where the file lives at `prompts/<c>/<v>/prompt.md`.
- **CI:** `helix ci render > .forgejo/workflows/test.yml`, then
  `helix ci validate --path .forgejo/workflows/test.yml`.
- **Gate:** don't rely on the hook's exit code yet (DF-001) — check the JSON
  `allowed` field in the hook output, or fix DF-001 first.
- **Sandbox:** build `cmd/sandbox` and put it on PATH; then
  `helix sandbox run -- <cmd>`.
