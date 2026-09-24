# Helix Dogfood — Integration Report (2026-09-24)

**Verdict:** 🟡 **PROMISING-BUT-ROUGH**
**Run angle:** *L3 — "does the documented contract survive contact with a fresh
machine?"* Previous runs (08-02, 08-12, 08-22 deep; 09-01, 09-04 short) swept the
offline CLI, Forgejo/identity, contracts, trust and verify. This run took the two
surfaces none of them touched: **the README-derived CLI contract** and **a
from-scratch install on a clean box**.

---

## 1. Promise (null hypothesis)

> "A user can clone `github.com/totalwindupflightsystems/helix`, run the README
> Quickstart (`make build`, `make test`), install the 9 CLIs, and then drive the
> platform from the `helix` dispatcher — every component in the README table is
> reachable at the CLI named in that table, and the flagship `helix dispatch`
> turns a spec file into agent work."

Evaluated in three places: this host (warm checkout), a **fresh clone on an
ephemeral bunker box** (las-bunker-03, agent `3bc183ac`, Debian, non-root), and
**CI** (GitHub Actions on the pushed HEAD).

## 2. What actually happened

| Leg | Command (documented) | Result |
|---|---|---|
| Fresh clone | `git clone https://github.com/totalwindupflightsystems/helix.git` | ✅ 4.2s, HEAD `acf2c90` |
| Install | `make build` (README quickstart) | ❌ **rc=2 — `make: go: No such file or directory`** (no Go on a bare box; docs never say Go is required) |
| Install (after installing Go 1.25.8) | `make build` | ✅ **rc=0, 214s** → 9 binaries |
| Smoke | `./helix version` | ✅ `helix 0.1.0-dev (linux/amd64)`, 13ms |
| Smoke | `./helix status` | ⚠️ rc=2 *CRITICAL* on a box with no stack — correct locally, but see GAP-HELIX-6 for the doc gap |
| Smoke | `./helix estimate check …` | ✅ rc=0, 17ms |
| Smoke | `./helix marketplace search …` | ✅ rc=0, 18ms |
| Smoke | `./helix identity provision test-agent` | ❌ rc=3 `FORGEJO_ADMIN_TOKEN or FORGEJO_ADMIN_USER+FORGEJO_ADMIN_PASSWORD must be set` — **the README's own quickstart omits the `export`s** |
| Tests | `make test` (README quickstart) | ❌ **rc=2 — `FAIL pkg/prompt`, on a clean HOME and on the fresh clone** |
| Install | `make install PREFIX=$HOME/.local` | ✅ rc=0, all 9 binaries installed |

**Time-to-first-success:** ~4 min (Go install 9s + `make build` 214s + smoke).
**Time-to-first-*failure*:** ~1 min — the documented `make build` cannot run on a
machine without a Go toolchain and the README never says so.

**Friction count:** 11 project-relevant points (below), plus 1 host-environment
note (bunker agents share `/tmp` — see §6).

## 3. The headline blocker: the default branch is red, and has been since 2026-09-10

A fresh clone gets `acf2c90`. On that commit:

```
--- FAIL: TestVerify (0.01s)
    --- FAIL: TestVerify/head_commit_with_path_style_attestation (0.00s)
        attester_extended_test.go:145: unexpected error:
            TAMPER_DETECTED: stored hash != computed hash
        attester_extended_test.go:148: expected non-nil attestation
FAIL  github.com/totalwindupflightsystems/helix/pkg/prompt
```

Equally true on this host *and* on the ephemeral box, and **with a pristine
`HOME`** — it is not accumulated state:

```
$ HOME=/tmp/df-freshhome go test -short -count=1 -run TestVerify ./pkg/prompt/
--- FAIL: TestVerify/head_commit_with_path_style_attestation
FAIL  github.com/totalwindupflightsystems/helix/pkg/prompt  0.704s
```

CI agrees on the pushed commit — `Test` job, step *"Run unit tests"* → `failure`
(run [34507859513](https://github.com/totalwindupflightsystems/helix/actions/runs/34507859513));
the `Build`, `Lint` and `Docs Consistency` jobs are green. CI has been red on 4 of
the last 8 pushes.

### Root cause (proven by worktree replay, not inference)

`TestVerify` calls `Verify("HEAD")`, which reads **whatever commit is checked
out** and requires a `Prompt:` trailer plus a passing hash check:

- At `34e473d` (the fix for GAP-004) the HEAD commit carried
  `Prompt: prompts/gap-004/v1.md`, but that file **does not exist in the tree** →
  `TAMPER_DETECTED`.
- On every commit since, the HEAD commit (`board: …`, `foreman tick [ci skip]`,
  `qa-cron: …`) carries **no `Prompt:` trailer at all** →
  `ATTESTATION_MISSING`.

So the test is green **only** on those rare commits whose own commit message
happens to satisfy the guard, and red on all other commits — including the board
writer's, which is the commit a fresh clone lands on.

**Missing gate:** CI runs the test suite against a checkout whose `HEAD` is
irrelevant to the code under test. A unit test that asserts on ambient git state
must be made hermetic (build a fixture commit in a temp repo — which the same
file already does elsewhere), or the suite must run against a controlled ref.

## 4. Contract drift: the README table names three CLIs that do not exist

The README's "Adversarial Review" table is asserted by a real CI gate —
`.github/workflows/ci.yml` step *"README package counts agree"* — which checks
`headline == component table == docs/api/README.md` and passes (41 == 41 == 41).
It does **not** check a single CLI name. Sweeping all 36 CLI claims in the
component tables against the binary's real subcommand list:

| README claims | Reality |
|---|---|
| `helix adversarial` (Adversarial Scenarios, line 109) | unknown subcommand — the real one is `helix review` (`helix adversarial` survives only as a **deprecated shim**, and the print claims "Pass rate: 1.0%" for 5/5 — `%.1f` applied to a 0–1 rate; the same file's `PassRate()` already returns percent, hence the 100× unit mismatch) |
| `helix coordinator` (PR Coordinator, line 126) | unknown subcommand — renamed to `helix pipeline` |
| `helix health` (Health Metrics, line 154) | unknown subcommand — the real surface is `helix doctor` / `helix status` |

Verified live: `./helix coordinator` → `Error: unknown subcommand "coordinator"`.

## 5. The flagship promise: `helix dispatch` decomposes one task, drops the body, and half the specs are unreadable

`helix dispatch` is the top row of "The Helix Loop" (README steps 2–5). Run
`--dry-run` against **every spec in the repo's own `specs/`**:

- **15 of 24 specs fail to decompose** with
  `dispatcher: spec decomposition failed: no Phase or Feature sections found`,
  including the repo's own `specs/SPECIFICATION.md`
  (`# Helix Platform — Master Implementation Specification`).
- `DecomposeSpec` (`pkg/dispatcher/decomposer.go:20-21`) **documents** that it
  matches `^# Helix Feature` for `specs/*.md`. That covers exactly **one** spec;
  the other 23 do not carry that H1. The documented mitigation does not cover the
  documented corpus.
- `currentDesc` is declared (`:29`) and `Reset()` (`:46`), but **never written to**
  — verified by grep. `Task.Description` is set only from the heading, so the
  package doc's promise "the body until the next heading is captured as context"
  is dead code. The plan output shows it: the single step's `action` repeats the
  task title and `expected_output` is `""`:
  ```
  "task_id": "task-001",
  "task_description": "Helix Feature 1 — Agent Identity in Forgejo",
  "steps": [ { "action": "Helix Feature 1 — Agent Identity in Forgejo",
               "expected_output": "", "status": "pending" } ]
  ```
- In `--dry-run` with **no `--repo`**, `pr_url` renders
  `http://localhost:3030/helix-org//compare/main...feature/…` — a doubled slash
  with an empty repo segment. A user cannot tell "expected in dry-run" from
  "misspelled input"; the placeholder should be explicit.
- The dispatch guarantee is also weaker than the framing: only `tasks[0]` is ever
  dispatched (`pkg/dispatcher/forgejo_loop.go:125-133`). That limitation is
  honestly documented in the comment but nowhere user-visible.

## 6. Non-code findings worth the maintainer's time

- **`make install` and `helix <sub>` disagree about location.** README §Install
  says the dispatcher resolves siblings via cwd → `cmd/<name>/<name>` → `PATH`.
  It does not mention the repo-root binaries, yet §Install justifies the
  resolution order with "`make build` compiles all binaries into the repository
  root". Inconsistent, and it sent me on an extra probe.
- **A private key lands in the repo working tree with zero ignore protection.**
  `helix identity create --name test-agent` (the GETTING-STARTED step) writes
  `test-agent.hid` and `test-agent.hid.key` **into cwd** at mode 0600. Neither is
  covered by `.gitignore` (which lists only two hardcoded names,
  `test-gap-hunter.hid{,.key}`), and `git add -A` in the repo root stages both
  (`git add -An` confirms). `.coding-hermes/`-style board writers and any
  `git add -A` in a CI step would sweep an agent private key into history. The
  leak counter-measure is `.gitreins` secrets scanning — which will not fire,
  because a PEM/base64 key pair does not match a token pattern.
- **Three different gate lists for one gate.** `pkg/mergegate` actually evaluates
  `evidence_bundle, consensus, behavior_contract, trust_tier, cost_guard`;
  README's Primitives section says GitReins is "6 blocking gates (secrets, lint,
  tests, build, attestation, prompt link)"; AGENTS.md §GitReins lists a third
  variant ("5-check" in the README table, "6 checks" in AGENTS.md). Running
  `helix mergegate check --trust trusted` on a clean tree yields
  1 pass / 3 fail / 1 skip — but `trust_tier` passes with the reason *"no changed
  files to check — tier requirement trivially met"*, i.e. the gate that is
  supposed to govern trust **passes vacuously** on exactly the input a first-time
  user tries. That is the same class of defect as the "green check on an adjacent
  credential path" pitfall: a passing check that can prove nothing.
- **`docs/GETTING-STARTED.md` uses a CLI form that does not exist.**
  `helix identity list` → `unknown subcommand "list"`; the real one is
  `helix-identity status`. The same doc could not be followed on the fresh box
  because of the missing admin exports.

## 7. Performance (measured, not felt)

`hyperfine`, this host, warm (20 runs), commands taken from the README quickstart:

| Operation | mean ± σ | min…max |
|---|---|---|
| `helix version` | 13.3 ms ± 1.9 | 10.2 … 17.6 ms |
| `helix estimate check …` | 17.0 ms ± 2.0 | 14.5 … 22.5 ms |
| `helix marketplace search …` | 18.0 ms ± 2.3 | 15.4 … 23.5 ms |

**Nothing here is slow enough to be worth a `PERF-` row.** The CLI is
process-per-invocation and sub-30ms; `status`/`doctor` added a fixed ~108ms
startup probe cost, still imperceptible. The one number a user *does* feel is the
**214s cold `make build`** (dependency download dominates) and the 9s Go
toolchain install the docs never mention — both reported above as findings, not
perf rows, because they are correctness/documentation gaps.

## 8. Install-leg summary (ephemeral bunker)

| Field | Value |
|---|---|
| Server | `bunker-las-03` (las-bunker-03), tailnet 100.69.3.13 |
| Agent | `3bc183ac` (destroyed after the run — `bunker list` → "No agents found") |
| Box | Debian, **non-root** (`sudo` needs a password), gcc 14.2, git 2.47.3, make 4.4.1, docker 26.1.5; **no Go, no docker-compose** |
| Clone | documented origin, `acf2c90`, 4.2s — existing access only, no visibility/permission change |
| First `make build` | **rc=2**, `make: go: No such file or directory` |
| Go install (user-local, 1.25.8 per `go.mod`) | 9s |
| Second `make build` | **rc=0, 214s** |
| Smoke | `version` ok · `estimate` ok · `marketplace` ok · `status` rc=2 (no stack) · `identity provision` rc=3 (missing env the README omits) |
| `make test` | **rc=2** — `FAIL pkg/prompt` (same defect as this host) |

> Host note (not a Helix defect): bunker agents on las-03 share `/tmp`. My first
> build log was written to `/tmp/build.log`, the redirect was denied, and I then
> read a **sibling fleet worker's** log from three days earlier — it showed a
> Rust `hilo` build finishing in 33m 44s and no `helix` binary, which looked like
> a Helix defect and was not. Lesson: on shared-`/tmp` ephemeral boxes, use
> `$HOME` paths for logs and verify ownership (`ls -l`) before reading them.

## 9. Verdict

**🟡 PROMISING-BUT-ROUGH.** The core is genuinely good: `make build` succeeds
from a clean clone in 214s, `make install` works per-user without sudo, the
offline CLI is fast (13–18ms) and correct (`estimate`, `marketplace` behave
exactly as the quickstart says), and every claim that could be checked against the
real binary held up except the three named rows. But the one thing a first-time
user does first — run the documented test suite, or trust the README table —
lands on a **deterministically red suite on the default branch** and on three
phantom CLI names, and the flagship `helix dispatch` cannot read 15 of the
project's own 24 specs.

Fix order if the maintainer has one hour: **(1)** make `TestVerify` hermetic —
one change turns the default branch green after 14 days red; **(2)** add a
CLI-name assertion to the existing docs-consistency gate (the count gate already
exists and passes; extend it, don't build new machinery); **(3)** decide the
`DecomposeSpec` contract — either teach it the corpus's real heading style or
state plainly in the README which specs are dispatchable.
