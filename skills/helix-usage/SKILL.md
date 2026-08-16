---
name: helix-usage
description: "How to actually use the Helix agent-first dev platform CLI — entry points, working invocations, pitfalls. Load this before touching the helix repo."
metadata:
  version: "1.0.0"
  applies_to: "repo:/home/kara/helix"
---

# Helix Usage

Helix is an agent-first software development platform (Go). Humans and AI
agents are equal participants: agents get Forgejo accounts, open PRs, get
reviewed by other agents, and build trust. This skill is the field-tested
"right way" to use it, from the dogfood run of 2026-08-02.

## Entry points

- **Unified CLI:** `cmd/helix` → `./helix <subcommand>` (after `go build ./cmd/...`)
- **Standalone binaries:** `helix-identity`, `helix-estimate`, `helix-prompt`,
  `helix-marketplace`, `helix-negotiate`, `helix-sandbox`, `helix-verify`,
  `helix-release` — the unified CLI **shells out** to several of these
  (`estimate`, `identity`, `prompt`, `negotiate`, `marketplace`, `sandbox`),
  so they must be built and reachable (repo root cwd or PATH).
- **Forge:** Forgejo REST API (port 3030 in this env), Chimera LLM server
  (8765), scheduler API (9090, fleet foreman).

## Build & test

```bash
export TMPDIR=/home/kara/.cache/go-tmp   # host /tmp is a loaded tmpfs
go build ./cmd/...                       # 58 packages
go test -short -count=1 ./...            # unit tests (60/60 pass)
```

## Working invocations (verified 2026-08-02 + 2026-08-12)

| Task | Command |
|---|---|
| Diagnostics | `helix doctor` / `helix status` / `helix config env-check` |
| Idea pipeline | `helix idea capture --title T --body B` → `helix idea validate <id>` → `helix idea prioritize` |
| Cost estimate | `helix estimate estimate "<task>" --model <m> --provider <p>` (from repo root, or add `--pricing <abs-path>`) |
| Prompt registry | `helix prompt register <component> <version>` (file at `prompts/<c>/<v>/prompt.md`), `helix prompt list` |
| CI workflow | `helix ci render` → save → `helix ci validate --path <file.yml>` |
| Deploy artifacts | `helix deploy render --kind agent\|caddy\|systemd [--json]`, `helix deploy tiers` |
| Sandbox | build `cmd/sandbox`, put on PATH → `helix sandbox run -- <cmd>` |
| Merge gate | `helix mergegate checks`; `helix mergegate hook --trust <tier>` reads refs from stdin |
| Security/ops | `helix vuln scan`, `helix recovery`, `helix backup status`, `helix incident list` |
| Agent identity | `helix identity create --name A` → `verify --hid A.hid` → `export --hid A.hid [--key A.hid.key] --format json\|nostr` |
| Provision (live Forgejo) | `helix-identity provision A --forgejo-url http://localhost:3030 --admin-token $T --admin-user U --admin-password P --known-friends kf.json --state-path s.json` |
| Deprovision | `helix-identity deprovision A` (same creds) — revokes PAT only; SSH key stays (DF-012) |
| Channels | `helix channel create --name C --type task --members A` → `send --channel C --message M` → `history --name C` → `archive --name C` |
| Sources | `helix source add --name S --type rest --spec openapi.json --base-url URL [--read-only]`, `source list`, `source test --name S`, `source tools --name S` |
| Contracts | `helix contract create <spec-id>` → registered id is `<spec-id>-openapi`; `validate/freeze/diff <spec-id>-openapi`; `diff <new> <old>` arg order is <new> <old> |

## Identity provisioning — the right way (field-tested 2026-08-12)

The flagship flow works end-to-end against a live Forgejo v1.21+ (~1s):
user + SSH key + `helix-identity-pat` token, state recorded. Requirements
that are NOT in the README:

1. **known-friends.json `agents` is a MAP** (name → {display_name, status,
   active, tier, ...}), not an array — see `pkg/identity/testdata/known-friends.json`.
2. **PAT creation needs BasicAuth**: pass `--admin-user` + `--admin-password`
   IN ADDITION to `--admin-token`, or CreateToken fails *after* the user and
   key were already created (partial state, exit 4).
3. **Idempotency is user-existence-only**: if a provision partially failed,
   a retry reports `action=unchanged` and exits 0 WITHOUT repairing the
   missing PAT/key (DF-011). After any failure, verify server-side:
   `GET /api/v1/users/<a>/tokens` (BasicAuth) must show 1 token.
4. `--state-path` expects a **file** path, not a directory.

## Pitfalls (all hit in real use)

1. **`helix <sub> --help` prints the ROOT menu.** Run the bare subcommand
   (`helix ci`, `helix mergegate`, `helix idea`) to see its real usage.
2. **`helix estimate` needs a doubled subcommand (fixed in DF-003).** The
   unified CLI shells out to `helix-estimate`, so the working form is
   `helix estimate estimate "<task>" --model <m> --provider <p>` — the
   README quickstart now shows this; `helix estimate --task` never existed.
   For spec-based estimates use `--spec-file` (not `--spec`).
3. **Estimate's default pricing path is CWD-relative** — run from the repo
   root or pass `--pricing`.
4. **Prompt files: nested layout** `prompts/<c>/<v>/prompt.md` is what
   `register` reads; flat `prompts/<c>/v<N>.md` (AGENTS.md convention) is not.
5. **The merge gate hook exits 0 even when it REJECTS** (DF-001, P0). Do not
   trust its exit code for enforcement; check the JSON `allowed` field. It
   also fails OPEN if it can't collect changed files (DF-009).
6. **Unified CLI needs sibling binaries** — `helix estimate` from an arbitrary
   cwd says `subcommand "helix-estimate" not found` unless the repo root is on
   PATH.
7. **Health probes can hang** (`/v1/health` on Chimera) — give `helix status`
   time; `helix doctor` is the better first step. **Audits must probe chimera
   too** (not just forgejo): `helix status --timeout 2s` exits non-zero and
   lists per-subsystem state; a down chimera is a FINDING, never "NO findings"
   (GAP-025).
8. **Commit convention is enforced by GitReins hooks:** every commit needs
   `Co-authored-by: Name <email>` and `Prompt: prompts/<name>/v<N>.md` in the
   body, or the commit-msg hook blocks it.
9. **`helix identity verify/export` take `--hid <path>`** — `--name` does not
   exist there (only on create/provision/deprovision). Run the bare
   subcommand for accurate usage.
10. **Contract ids get a `-openapi` suffix on create** — `contract create
    agent-identity` registers `agent-identity-openapi`; validating with the
    bare id says "not found".
11. **`helix source test` false-fails REST sources whose base URL returns
    non-200** (live Forgejo `/api/v1` → 404 → test fails even though the
    source works). Treat base 404 as a warning and probe a spec path
    (DF-013).
12. **Unknown flags are silently dropped** by the hand-rolled parser (e.g.
    `contract validate --file x.yaml` treats x.yaml as the id). Typos become
    confusing lookup errors, not flag errors (DF-016).

## Right-way patterns

- **Getting unblocked:** when a subcommand's flags are unknown, read
  `cmd/helix/<sub>.go` for the `usage`/flag block — it's accurate where
  `--help` is not.
- **Board:** tasks live in `.coding-hermes/board/tasks.jsonl` (JSONL
  canonical, git-tracked) with audit events in `events.jsonl`. The DuckDB
  cache is untracked and rebuildable — never write to it directly. IDs
  follow `<AREA>-NNN` (e.g. DF-001).
