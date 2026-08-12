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

---

# Addendum — 2026-08-12 dogfood (second run)

## What changed since 2026-08-02

The 2026-08-02 verdict's blockers are **fixed in reality** (re-verified live,
not just via tests): `mergegate hook` exits 1 on reject (DF-001), `--help`
shows subcommand usage (DF-002), CLI works from /tmp (GAP-005), `status`
completes in 3.4s (GAP-001), prompt attestation accepts both layouts
(GAP-004), dispatcher decomposes Helix specs (GAP-007). New surface landed:
identity lifecycle (ID-004), channels (CH-005), sources (SRC-006), contracts,
spec/adr workflow.

## How the flagship flow is built (identity provisioning)

`helix-identity provision` (cobra, pkg/identity/provisioner.go) does:
create Forgejo user via admin API → register Ed25519 SSH key → create scoped
PAT via **admin BasicAuth** (not the admin token) → write idempotency state
(file: version/last_sync/agents[name]{forgejo_account_id, ssh_key_id, pat_id,
last_provisioned}). The happy path is ~1s and verified live (user id 5,
key "Helix Agent — <a> (flash)", PAT `helix-identity-pat`).

**Why the idempotency check lies (DF-011):** the "unchanged" decision comes
from an admin-list user-existence probe, not from reconciling the state file
against live resources. After a partial failure (user+key created, PAT step
failed), state has no agent record, Forgejo has no PAT, but the user exists
→ every retry reports `unchanged` + exit 0. The platform's own trust theme
applies to itself: it does not detect that a provisioned agent is
half-provisioned. Right way: on "unchanged", verify pat_id (and key) still
exist server-side; repair or report.

**Why deprovision leaves the key (DF-012):** deprovision revokes the PAT
(delete token by pat_id) and tries to archive the local key via `os.Rename`
into `archive/<date>/` — but never creates that directory (no MkdirAll), so
the rename fails with a WARN and the key stays live both locally and in
Forgejo (server-side key deletion is not implemented). Right way: MkdirAll
before rename; delete the Forgejo key (by ssh_key_id) as part of deprovision.

**Why `source test` false-fails Forgejo (DF-013):** the REST probe does
`GET <base-url>` and requires 200. Forgejo's `/api/v1` root is 404 by
design — the spec (from `/swagger.v1.json`) is valid and every real endpoint
works. Right way: probe a GET path taken from the spec, or make the base
probe informational when spec paths respond.

**Contract naming (DF-014):** `contract create <spec-id>` appends the format
to the id (`agent-identity-openapi`) but create output doesn't echo the full
id, and validate/freeze/diff with the bare id fail "not found". The generated
OpenAPI is also an empty scaffold (0 paths) — generation from prose specs
doesn't extract endpoints yet.

## Environment notes (2026-08-12)

- Forgejo :3030 live (v1.21.11, admin helio/helio123 per scripts/bootstrap.sh —
  dev instance; never commit these). :3000 node stub gone (404). Chimera :8765
  responds, providers degraded (missing creds). SSH port 2222 NOT exposed on
  this host — SSH-as-agent verification impossible here.
- Host: 82% disk, `go build` fine with `TMPDIR=/home/kara/.cache/go-tmp`.
