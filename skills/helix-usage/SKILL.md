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
- **Forge:** Forgejo REST API (port 3000 in this env), Chimera LLM server
  (8765), scheduler API (9090, fleet foreman).

## Build & test

```bash
export TMPDIR=/home/kara/.cache/go-tmp   # host /tmp is a loaded tmpfs
go build ./cmd/...                       # 58 packages
go test -short -count=1 ./...            # unit tests (60/60 pass)
```

## Working invocations (verified 2026-08-02)

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

## Pitfalls (all hit in real use)

1. **`helix <sub> --help` prints the ROOT menu.** Run the bare subcommand
   (`helix ci`, `helix mergegate`, `helix idea`) to see its real usage.
2. **README quickstart examples are stale.** `helix estimate --task` and
   `helix-estimate check --spec` fail (`--spec-file` is the flag; the
   estimate command takes a positional task + requires `--model`).
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
   time; `helix doctor` is the better first step.
8. **Commit convention is enforced by GitReins hooks:** every commit needs
   `Co-authored-by: Name <email>` and `Prompt: prompts/<name>/v<N>.md` in the
   body, or the commit-msg hook blocks it.

## Right-way patterns

- **Getting unblocked:** when a subcommand's flags are unknown, read
  `cmd/helix/<sub>.go` for the `usage`/flag block — it's accurate where
  `--help` is not.
- **Board:** tasks live in `.coding-hermes/board/board.db` (DuckDB) with
  git-tracked `tasks.parquet`/`events.parquet` exports. IDs follow
  `<AREA>-NNN` (e.g. DF-001). The board has no PK constraints — use plain
  INSERTs, and refresh parquet with `COPY tasks TO '…' (FORMAT PARQUET)`.
