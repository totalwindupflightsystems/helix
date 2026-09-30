# Helix Docs Walkthrough — 2026-09-30 (helix-docs lane, run 6)

**Angle:** prior runs swept the CLI surface (08-02, 08-12, 08-22, 09-01/04) and the
README-as-contract + fresh-machine install (09-24). Nobody had executed the docs
themselves as a user contract: this run walked `docs/GETTING-STARTED.md` §2→§8
top-down in a fresh clone, running every documented command verbatim, then
audited all 41 `docs/api/*.md` pages against source (symbol check + compile
check + `go doc` signature check). Plus the mandatory ephemeral-bunker install
leg. Board rows: DF-HELIX-12..16.

**Promise under test:** "This guide gets you from a fresh checkout to a running
platform in ~10 minutes" — with the Forgejo stack up, an agent provisioned, an
estimate checked, and prompts attested, following the doc top to bottom.

## What the walkthrough did (all commands verbatim from the doc)

| Step | Documented command | Result |
|------|--------------------|--------|
| §2 build | `make build` (fresh clone of HEAD 11c42e0) | ✅ rc=0 (~5 min warm deps) |
| §2 verify | `./helix --help` "lists all 50 subcommands" | ✅ 52 listed (claim ok) |
| §2 test | `make test` "unit tests (fast, no services required)" | ❌ **rc=2 — RED at HEAD** (new cause, see DF-HELIX-12); wall ~16 min |
| §3 stack | `./scripts/up.sh` | ❌ **hangs forever on a fresh volume** (DF-HELIX-13); manual `docker compose up -d forgejo` works but lands on the install wizard |
| §4 health | `./helix doctor`, `./helix status --json` | ✅ both work; status reports healthy (08-22 false-down confirmed fixed). ⚠️ doctor exits 1 on the documented "expected, not a failure" checks |
| §5 identity | create/provision/verify/list | ✅ create+provision+verify work (provision 0.64s). ❌ `helix identity list` needs `--forge` (doc omits flag) and lists OAuth apps, not provisioned agents |
| §5 repair claim | delete PAT server-side → re-provision | ✅ **action=updated + PAT re-created** — the 08-12 DF-011 idempotency lie is genuinely fixed |
| §6 estimate | `estimate estimate …` | ✅ works, 32ms |
| §6 estimate | `estimate check codex-alpha …` (doc's example agent) | ❌ `codex-alpha` not in the shipped roster; with `test-agent`: **always BLOCKED $0.10 > $0.00** — no budget field exists anywhere in docs/example (DF-HELIX-14) |
| §6 report | `estimate report codex-alpha` | ❌ same agent-name gap; `report test-agent` works |
| §7 prompts | register/list/test | ✅ all work (register→hash, test 1/1 pass) |
| §8 next-steps | `helix forgejo status` | ❌ unknown subcommand (it's `forgejo ping --url …`) |
| §8 next-steps | `helix channel create #agents` | ❌ invalid args (real form: `--name agents --type task`) |
| §8 next-steps | `helix marketplace search --capability go --min-trust 50` | ✅ works (README quickstart form) |

## docs/api audit (41 pages)

- **Package refs:** all 41 pages reference real `pkg/…` dirs. ✅
- **Symbol existence:** every `func`/`type`/`var` name drawn from doc ```go
  blocks exists in the package source. ✅ (59 method names flagged by a naive
  `go doc` diff are all real methods — `go doc pkg` doesn't list methods; the
  static grep against `pkg/**/*.go` confirms each.)
- **Compilability:** 0 of 28 extracted "examples" compile standalone — they are
  identifier fragments (`NewEstimator(…)` with undefined locals), not runnable
  examples. A docs page whose only Go example is a non-compiling sketch is a
  known rot vector: nothing can break *visibly*. DOC-HELIX note inside
  DF-HELIX-16.

## Install leg (bunker-las-03, ephemeral agent 7b900211, destroyed after)

- `git clone https://github.com/totalwindupflightsystems/helix.git` from the
  documented origin: ✅ (network fetch, HEAD 11c42e0).
- **Documented install fails immediately:** `make build` → `make: go: No such
  file or directory` rc=2 in 0s — Go is not a documented prerequisite anywhere
  (DF-HELIX-9 from 09-24, still open). README/GETTING-STARTED both assume it.
- With a user-local Go 1.25.8 install prepended: `make build` **rc=0 in 155s**
  (binary 68MB, 8 helix* binaries in repo root). Smoke: `helix version` 19ms ✅,
  `helix estimate estimate …` ✅.
- Same-day cross-reference: 09-24 run measured 214s on las-bunker-03 agent
  3bc183ac (also rc=2 first attempt). Today's run is consistent: build itself is
  fine; the doc gap is the missing Go prerequisite + no `go version` check in
  `make`.

## Verdict-relevant numbers

- Time-to-first-success (fresh clone → `helix version` answering): ~5 min
  (build dominates). On a Go-less box: never — the first documented command
  fails at 0s.
- Time-to-running-platform: **unbounded via the documented path** (up.sh never
  returns on a fresh volume); ~8 min via the undocumented manual path + web
  wizard + the 4 manual fixes in DF-HELIX-13.
- `make test`: ~16 min wall (59 pkgs ok, `pkg/prompt` alone 63s; `pkg/mergegate`
  53s) — "fast, no services required" is false on both counts.
- CLI latency: `helix version` 21.5ms ±3.0, `helix estimate estimate` 32.1ms
  ±3.1 (hyperfine 20 runs, warm) — nothing worth a PERF row.
