# Helix Diagnostics Trail — 2026-08-02

This is the "how it's built, why, what broke, and the right way" record for
Helix, written after a full dogfood run. It is **not** raw logs — it explains
the system so the next agent can answer "does this work?" from the repo
records.

## How Helix is built

- **Language/stack:** Go 1.25/1.26. Two CLI layers:
  1. Standalone cobra CLIs (`cmd/helix-identity`, `cmd/helix-estimate`,
     `cmd/helix-prompt`, `cmd/helix-marketplace`, `cmd/helix-negotiate`,
     `cmd/sandbox`, …).
  2. A hand-rolled unified dispatcher (`cmd/helix/main.go`, no cobra) that
     wires ~40 subcommands. Many subcommands are native (status, doctor,
     idea, ci, deploy, mergegate, config, security, vuln, recovery, backup,
     incident, audit, trust); several **shell out to the standalone binaries**
     (estimate, identity, prompt, negotiate, marketplace, sandbox) via
     `exec.LookPath`-style resolution.
- **Why two layers:** each component was built as a standalone tool first,
  then unified. The cost: the "unified" binary's behavior depends on sibling
  binaries being discoverable (cwd/PATH) — a real footgun for users (DF-006).
- **Board:** `.coding-hermes/board/tasks.jsonl` (JSONL canonical) is the task
  board; `board.jsonl` carries the header, `events.jsonl` the event stream,
  `fixtures.jsonl` the recurring fixtures, and `schema.sql` documents the
  DuckDB v2.1 schema. The live DuckDB cache and the `tasks.parquet`/
  `events.parquet` exports are untracked and regenerated each tick.
- **Quality:** GitReins hooks (commit-msg requires `Co-authored-by:` +
  `Prompt: prompts/<name>/v<N>.md`; pre-commit runs secrets/lint/tests/build
  in diff mode). Tests: 60/60 unit tests green; CI 4/4 (Forgejo Actions);
  Hilo graph stats maintained by the foreman.
- **External services in this environment:** Chimera on :8765 (FastAPI-style,
  real OpenAPI at `/openapi.json`, health at `/v1/health`), a **node stub on
  :3000** (answers `/health` 200, everything else `ROUTE_NOT_FOUND` JSON —
  *not* a real Forgejo), LangFuse on :3001, scheduler on :9090 (fleet
  foreman).

## Errors encountered and what they meant

| Error | Root cause | Right way |
|---|---|---|
| `go: error obtaining buildID … resource temporarily unavailable` | Host fork EAGAIN (thread/process pressure — host issue, comes in waves) | Retry with `go build -p 2`; not a repo defect |
| `helix estimate --task` → `unknown flag: --task` | Docs/help example uses a flag that doesn't exist | Use `helix estimate estimate "<task>" --model <m> --provider <p>` |
| `helix-estimate check --spec` → `unknown flag: --spec` | README example wrong; flag is `--spec-file` | See integration report §3.2 |
| `CONFIG_ERROR: read pricing file "pkg/estimate/testdata/pricing.yaml"` | Default `--pricing` is CWD-relative; CLI run outside repo root | Run from repo root or pass absolute `--pricing` (DF-004) |
| `helix prompt register coding-hermes v1` → `cannot read prompt file prompts/coding-hermes/v1/prompt.md` | Layout mismatch: tool wants `prompts/<c>/<v>/prompt.md`; repo stores flat `prompts/<c>/v<N>.md` for most prompts | Use nested layout or fix DF-005 |
| `subcommand "helix-estimate" not found (helix-estimate)` | Unified CLI shells out; binary not on PATH/cwd | Build it (`go build ./cmd/helix-estimate`) and put repo root on PATH |
| `helix status`: chimera down / forgejo degraded | Probe hits `http://localhost:8765/v1/health` which **hangs** (server accepts but never responds — health handler checks providers); :3000 is a node stub whose routes don't match the Forgejo probe path | Short per-service timeouts; classify route-mismatch vs down (DF-007) |
| `mergegate hook` prints `✗ REJECTED` but **exits 0** | The hook evaluator never maps rejection to a non-zero exit; wrapper `exec`s the binary so it inherits 0 | **P0 (DF-001):** exit 1 on any rejected ref |
| `mergegate hook` reports `allowed:true` when `git diff-tree` fails | Fail-open fallback: `"could not collect changed files … (allowing — likely a new branch)"` | **P0-adjacent (DF-009):** fail closed on unexpected errors |
| `helix audit`/`security` SIGABRT (`pthread_create failed`) | Host thread exhaustion; cgo crash instead of clean error | Host-side; CLI should degrade gracefully (minor) |

## The right way (verified paths)

1. **Diagnose first:** `helix doctor` — fast, precise, per-check errors.
   `helix config env-check` — secret-masked env validation.
2. **Design loop:** `helix idea capture → validate → prioritize` works fully
   offline and is instant — it's the most polished flow in the platform.
3. **CI:** `helix ci render` produces a complete Forgejo Actions workflow;
   `helix ci validate --path <file>` checks structure, coverage gate, and
   Forgejo service. Real artifact, real validation.
4. **Deploy:** `helix deploy render --kind systemd|agent|caddy --json` gives
   usable artifacts; `deploy tiers` lists trust tiers.
5. **Gate (until DF-001 is fixed):** treat `mergegate hook` output as
   advisory; enforce via its JSON `allowed` field, not exit code.
6. **Sandbox:** `go build -o sandbox ./cmd/sandbox` + PATH, then
   `helix sandbox run -- <cmd>` (bubblewrap; ~20 ms overhead).

## Why the verdict is PROMISING-BUT-ROUGH

- **Value is real:** the idea pipeline, doctor, env-check, CI gen/validate,
  deploy render, sandbox, vuln scan all work and answer real needs. This is a
  seriously engineered platform (60 tests, CI, spec suite, foreman loop).
- **Usability is the blocker:** broken README examples, `--help` that lies,
  CWD-relative defaults, external-binary coupling — a new user burns 10+
  friction points before a working estimate.
- **One P0 breaks the core promise:** the pre-receive merge gate cannot block
  a push (exit 0 on reject + fail-open on error), so "gates that gate" — the
  platform's reason for existing — is not yet true. That, not test colors, is
  what the board's DF-001/DF-009 tasks track.
