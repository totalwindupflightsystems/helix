# Helix Feature 4 — Prompt Registry (v2)

**Status:** v2.0 specification (build-ready, zero implementation)
**Spec version:** 2.0
**Last updated:** 2026-06-20
**Depends on:** Feature 1 (Agent Identity), Feature 2 (Cost Estimator), GitReins v0.3.0+ (commit-msg hook), Forgejo Actions
**Blocks:** Feature 5 (Marketplace — prompt quality feeds reputation)
**Supersedes:** `specs/prompt-registry.md` (v1)

This document is the authoritative, build-ready implementation reference for
the Helix Prompt Registry. It specifies prompt storage, content addressing,
immutability, the lifecycle state machine, commit attestation enforced by the
GitReins commit-msg hook, PromptFoo regression CI, and the prompt→spec→intent
provenance chain. It is sufficient for an engineer to implement the
`helix-prompt` CLI and the hook without asking clarifying questions. The Go
package layout in §16 defines every file; this document explains the contracts
those files encode. v2 refines the v1 design into implementation-precise
contracts — explicit normalization rules, an exact hook algorithm, atomic
state semantics, a complete transition table, and rename-robust tamper
detection. No v1 behavior is removed; v2 sharpens the edges.

---

## 1. Mission

Make prompts first-class, versioned, immutable artifacts and make every Helix
commit carry a verifiable prompt attestation. The registry stores, versions,
and content-addresses prompts. The GitReins commit-msg hook enforces that no
commit lands without a valid attestation pointing at an attested-or-active
prompt whose stored SHA-256 matches its bytes. PromptFoo CI runs regression
tests whenever a prompt file changes and gates merges.

The v2 deliverable is **not** a network integration — it is a specification
plus compilable Go stubs defining every interface, with the full local
pipeline (storage, hashing, lifecycle, attestation parser, hook logic,
provenance walker, tamper detector, audit log) implemented and exercised. The
result is a provenance chain traceable both ways: human intent → spec → prompt
→ code → commit → merge, and the reverse.

---

## 2. Scope

### In scope (v2)
- CLI with 4 subcommands (`register`, `attest`, `verify`, `list`)
- Per-repo prompt storage: `prompts/<component>/<version>/prompt.md` + `metadata.yaml`
- Registry index `prompts/_index.yaml` for hash/component/version lookups
- SHA-256 content hashing with a deterministic 5-step normalization pipeline
- Prompt lifecycle state machine: draft → proposed → reviewed → attested → active → deprecated → retired
- Commit attestation enforcement via the GitReins commit-msg hook
- PromptFoo CI integration (Forgejo Action, `.promptfoo.yaml`)
- Provenance chain: commit → prompt → spec → work item → intent
- Tamper detection: stored hash != recomputed hash → commit blocked
- Atomic state writes (temp file + rename) and append-only JSONL audit log
- Dry-run mode across all subcommands

### Out of scope (v2)
- Cross-repo prompt sharing (each repo owns its own `prompts/` directory)
- Prompt templating or variable substitution
- Prompt A/B testing, canary deployment, or automated optimization
- Prompt analytics dashboard
- A central networked registry service (the registry is the on-disk index)

---

## 3. Inputs

### 3.1 Prompt Storage Structure

```
prompts/
├── _index.yaml                        Registry index (all prompts)
├── agent-identity/
│   ├── v1.0.0/
│   │   ├── prompt.md                  The prompt text (immutable after attestation)
│   │   └── metadata.yaml              Version metadata + lifecycle status
│   └── v1.1.0/
│       ├── prompt.md
│       └── metadata.yaml
├── cost-estimator/
│   └── v1.0.0/
│       ├── prompt.md
│       └── metadata.yaml
└── ...
```

### 3.2 Metadata Schema (`metadata.yaml`)

```yaml
# prompts/<component>/<version>/metadata.yaml
version: "1.1.0"
component: "agent-identity"
hash: "sha256:a1b2c3d4e5f6..."          # hash of NORMALIZED prompt.md body
model: "glm-5.2"
provider: "zai-glm"
author: "agent-wojons"
author_trust: 85
spec_ref: "specs/agent-identity.md"
spec_version: "1.0"
work_item: "WI-helix-001"
created_at: "2026-06-19T10:00:00Z"
attested_at: "2026-06-19T10:05:00Z"      # set on first attestation
status: "active"                          # one of 7 lifecycle states (§10)
previous_version: "v1.0.0"
changes: "Added rate limiter, fixed ED25519 fingerprint format"
cost: { estimated_input_tokens: 80000, estimated_output_tokens: 15000, estimated_cost_usd: 0.024 }  # Feature 2
commits:                                  # appended on each attestation
  - { sha: "abc123def456...", repo: "totalwindupflightsystems/helix", files_changed: 3, merged: true }
promptfoo: { test_suite: "prompts/agent-identity/promptfoo.yaml", last_run: "2026-06-19T10:05:00Z", status: "pass" }  # pass|fail|unknown
```

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `version` | semver string | yes | Must match directory name |
| `component` | string | yes | Must match directory name |
| `hash` | `sha256:<64hex>` | yes | Recomputed on every verify |
| `model` / `provider` | string | yes | Model + provider key |
| `author` | string | yes | Agent identity (Feature 1) |
| `spec_ref` | path | yes | Linked specification file |
| `status` | enum | yes | Lifecycle state (§10) |
| `created_at` / `attested_at` | RFC3339 | yes / on attest | — |
| `cost` / `commits` / `promptfoo` | object | no | Enrichment fields |

### 3.3 Registry Index (`_index.yaml`)

The index is the hook's only fast-path lookup — an in-repo cache of a subset
of each `metadata.yaml` (component, version, hash, status). It MUST stay
consistent with metadata.yaml or `verify` reports `INDEX_STALE` (warning,
non-blocking) and rebuilds from disk. Example entry:

```yaml
version: 1
updated_at: "2026-06-20T09:00:00Z"
prompts:
  - { component: "agent-identity", version: "v1.1.0", hash: "sha256:a1b2c3d4...", status: "active" }
  - { component: "cost-estimator", version: "v1.0.0", hash: "sha256:fedcba09...", status: "attested" }
```

### 3.4 CLI Flags and Env Vars

Every persistent flag resolves as: **CLI flag > env var > default**.

| Flag | Env var | Default |
|------|---------|---------|
| `--prompts-dir` | `HELIX_PROMPTS_DIR` | `./prompts` |
| `--index-path` | `HELIX_PROMPT_INDEX` | `./prompts/_index.yaml` |
| `--audit-path` | `HELIX_PROMPT_AUDIT` | `~/.helix/prompts/audit.jsonl` |
| `--prompt-file` | — | `prompts/<component>/<version>/prompt.md` |
| `--model` / `--provider` | — | required for `register` |
| `--spec-ref` | — | recommended |
| `--commit-sha` | — | `HEAD` |
| `--format` | — | `table` (`table` \| `json`) |
| `--dry-run` / `--verbose,-v` / `--force` | — | `false` |

---

## 4. Operating Contract

- **NEVER** allow a commit without a prompt attestation. The GitReins commit-msg hook enforces this; `--no-verify` is the only escape and is audited.
- **NEVER** modify an attested-or-later prompt file in place. Editing = a new version directory. The hook detects in-place edits as tamper.
- **ALWAYS** hash the normalized prompt (§8.2): whitespace-collapsed, newline-normalized, trailing newline stripped, leading indentation preserved.
- **ALWAYS** run PromptFoo tests when a prompt file changes. A failing suite blocks attestation into `active`.
- **ALWAYS** write state atomically: temp file in the same directory + `os.Rename`. A crash mid-write never produces a corrupt index. Always append to `audit.jsonl`; never rewrite or truncate it.
- **DO NOT** accept attestation when the recomputed hash differs from the stored hash.
- **DO NOT** accept attestation for `draft`, `proposed`, `reviewed`, or `retired` prompts.
- **DO NOT** store secrets in prompts. Prompts are committed to the repo; secrets belong in env vars.

---

## 5. Assumptions

- Each repo contains its own `prompts/` directory. No global registry in v2. Prompts are plain markdown, optionally with YAML frontmatter (frontmatter is excluded from the hash).
- SHA-256 is sufficient for content addressing (collision resistance well beyond any registry size).
- PromptFoo is configured via `.promptfoo.yaml` in the repo root and run via Forgejo Actions. The GitReins commit-msg hook executes in the working tree where `prompts/` is present.
- Agents self-declare which prompt they used (attestation is agent-driven, hook-verified).
- `metadata.yaml` fields `component` and `version` are authoritative for directory location; a mismatch is a hard error.

---

## 6. Architecture

```
        ┌──────────────────────────────────┐
        │          cmd/helix-prompt          │  Cobra CLI: 4 subcommands
        └──────────────┬───────────────────┘
                       │
        ┌──────────────▼───────────────────┐
        │       pkg/prompt/registry          │  Load → Register → Hash → Store
        │                                    │  → Attest → Verify → Audit
        └───────┬───────────────┬───────────┘
                │               │
   ┌────────────▼─────────┐  ┌──▼──────────────────┐
   │ pkg/prompt/hasher    │  │ pkg/prompt/lifecycle │
   │ 5-step norm, SHA-256,│  │ 7-state machine,    │
   │ tamper detect        │  │ transition table    │
   └────────────┬─────────┘  └─────────────────────┘
                │
   ┌────────────▼─────────┐  ┌─────────────────────┐
   │ pkg/prompt/attester  │  │ pkg/prompt/provenance│
   │ commit-msg parser,   │  │ chain walker:       │
   │ verify               │  │ commit→prompt→spec  │
   └────────────┬─────────┘  │ →work item→intent   │
                │            └─────────────────────┘
   ┌────────────▼───────────────────────────────────┐
   │  External: GitReins (commit-msg hook) ·        │
   │  PromptFoo (Forgejo Action CI) · Git history   │
   └────────────────────────────────────────────────┘
```

**Layering rules:** `types.go` imports only stdlib; `hasher.go` and
`lifecycle.go` import `types.go`; `registry.go` imports all three; `attester.go`
and `provenance.go` import `registry.go`; `main.go` imports all and owns CLI
rendering. No cycles. One concern per file (hasher, lifecycle, registry,
attester, provenance).

---

## 7. CLI Interface

```
helix prompt register <component> <version> [flags]
  --prompt-file PATH   Prompt source (default prompts/<c>/<v>/prompt.md)
  --model NAME         Model that produced this prompt (required)
  --provider NAME      Provider key, e.g. zai-glm (required)
  --spec-ref PATH      Linked spec file
  --dry-run            Validate + compute hash, write nothing

helix prompt attest <component> <version> [flags]
  --commit-sha SHA     Commit being attested (default: HEAD)
  --force              Skip validation (emergency only; audited)

helix prompt verify <commit-sha> [flags]
  --check-hash         Verify recomputed hash == stored hash
  --check-lifecycle    Verify prompt is attested or active
  --check-promptfoo    Verify PromptFoo status is pass
  --full-chain         Walk the full provenance chain

helix prompt list [flags]
  --component NAME     Filter by component
  --status STATE       Filter by lifecycle status (default: active)
  --model NAME         Filter by model
  --format table|json  Output format (default: table)
```

**Subcommand contracts:** `register` reads the prompt, normalizes, hashes, writes `prompt.md` + `metadata.yaml` at status `draft` + an index entry (never above `draft`). `attest` advances toward `attested`, appends the commit SHA to `metadata.commits`, and prints the `Prompt: sha256:<hash>` line for the commit message. `verify` recomputes the hash, checks lifecycle + PromptFoo, and (with `--full-chain`) walks to the work item/intent — exit code per §13. `list` renders a filtered table or JSON array from the index.

---

## 8. Prompt Storage Model

### 8.1 Directory and Immutability

A prompt version is the pair `<component>/<version>`, holding exactly two
files: `prompt.md` and `metadata.yaml`. Once a prompt reaches `attested`,
`prompt.md` is immutable; the hook rejects any commit whose attested prompt
differs in bytes from the version recorded at attestation. To change a prompt,
create a new version directory and bump `version` + `previous_version`.

### 8.2 SHA-256 Hashing and Normalization

The hash is over the **normalized** prompt body, so content-equivalent prompts
share a hash regardless of OS line endings or stray whitespace. YAML
frontmatter (a leading `---` … `---` block) is stripped first; only the
markdown body is content-addressed.

Normalization pipeline (deterministic, 5 steps):
1. Normalize line endings to `\n` (CRLF and lone CR → LF).
2. Collapse runs of spaces/tabs within a line to a single space — suppressed inside fenced code blocks (§8.3).
3. Strip trailing whitespace from each line.
4. Ensure exactly one trailing newline at EOF (strip extras; add one if absent).
5. Preserve leading whitespace (markdown list/code indentation is semantically meaningful).

```
Hash(prompt) = "sha256:" + hex( sha256( Normalize(prompt) ) )
```

### 8.3 Fenced-Code-Block Exemption

Step 2 (whitespace collapse) is suppressed inside fenced code blocks
(delimited by ``` or ~~~) so code samples hash identically to their source.
The normalizer tracks fence state line-by-line; an unclosed fence is treated
as "inside" until EOF.

### 8.4 Index Consistency

On `verify`, the registry recomputes the hash from `prompt.md` and compares
against both `metadata.hash` and the index entry. If `metadata.hash` matches
the recomputed value but the index differs, the registry rebuilds the index
and logs `INDEX_STALE` (warning). If `metadata.hash` != recomputed, that is
`TAMPER_DETECTED` (§13, exit 3).

---

## 9. Commit Attestation

### 9.1 Commit Message Format

The commit body MUST include a `Prompt: sha256:<64hex>` line (the only line the
hook parses, along with optional `Model:`/`Provider:`). The rest of the
trailer is conventional:

```
feat(identity): add rate limiter to Forgejo provisioner

Prompt: sha256:a1b2c3d4e5f6789012345678901234567890abcdef0123456789abcdef012345
Model: glm-5.2
Provider: zai-glm
Spec: specs/agent-identity.md §13
Cost: $0.024

Co-authored-by: wojons <wojonstech@gmail.com>
```

### 9.2 GitReins Commit-Msg Hook Algorithm

The hook fires on every commit and runs this exact sequence:

```
1. Read commit message from the file Git passes as $1.
2. Extract the line matching ^Prompt:\s*sha256:[0-9a-f]{64}$
   ├─ missing   → REJECT (exit 1): ATTESTATION_MISSING
   └─ malformed → REJECT (exit 6): ATTESTATION_MALFORMED
3. Look up the hash in prompts/_index.yaml
   └─ not found → REJECT (exit 2): PROMPT_NOT_FOUND
4. Load prompts/<component>/<version>/metadata.yaml
   ├─ metadata.hash != index.hash → rebuild index, recheck; still off → REJECT (exit 6)
   └─ ok → continue
5. Recompute hash from prompts/<component>/<version>/prompt.md
   └─ recomputed != metadata.hash → REJECT (exit 3): TAMPER_DETECTED
6. Check lifecycle status (§10):
   - active / attested        → PASS
   - deprecated               → WARN (allowed, 30-day grace)
   - draft/proposed/reviewed  → REJECT (exit 4): LIFECYCLE_VIOLATION
   - retired                  → REJECT (exit 4): LIFECYCLE_VIOLATION
7. Check promptfoo.status: pass → PASS; fail → REJECT (exit 5); unknown → WARN
8. Append an audit record; print [PASS]/[FAIL] summary to stderr; exit 0.
```

### 9.3 Override and Audit

`--no-verify` bypasses the hook — this is git's contract and cannot be
prevented. Every bypass is logged to `audit.jsonl` with `operation=override`,
timestamp, actor, commit SHA, and `reason="--no-verify override"`. A weekly
report surfaces override frequency; a spike triggers review. Observability
(§15) is the guardrail, since the hook cannot block `--no-verify`.

---

## 10. Prompt Lifecycle

```
 draft ─▶ proposed ─▶ reviewed ─▶ attested ─▶ active ─▶ deprecated ─▶ retired
   ▲         │            │           │          │          │
   └─────────┴────────────┴───────────┴──────────┴──────────┘
              (review fail, or emergency rollback, returns toward draft)
```

| State | Meaning | Allowed in Attestation? |
|-------|---------|------------------------|
| `draft` | Being written; not yet proposed. | ❌ No |
| `proposed` | Submitted for review (Chimera spec-conformance). | ❌ No |
| `reviewed` | Chimera review passed; ready for attestation. | ❌ No |
| `attested` | First commit references this prompt; now immutable. | ✅ Yes |
| `active` | In active use; multiple commits reference it. | ✅ Yes |
| `deprecated` | Superseded; allowed during a 30-day grace window. | ⚠️ Warn |
| `retired` | No longer valid; commits referencing it are blocked. | ❌ No |

**Transition rules (complete):**

| From | To | Trigger | Automatic? |
|------|----|---------|-----------|
| `draft` | `proposed` | Agent submits; Chimera review requested | Manual |
| `proposed` | `reviewed` | Chimera spec-conformance review passes | Auto |
| `proposed` | `draft` | Chimera review fails; agent must revise | Auto |
| `reviewed` | `attested` | First commit references this prompt | Auto |
| `attested` | `active` | Second commit references this prompt | Auto |
| `active` | `deprecated` | Newer version published (manual, or auto when newer has 3+ commits) | Both |
| `deprecated` | `retired` | 90 days deprecated OR no commits in 180 days | Auto |
| `deprecated` | `active` | Rollback (emergency); requires human approval | Manual |
| `retired` | `active` | **Forbidden** — re-attest as a new version instead | — |

A forbidden transition is rejected with exit code 4 (`LIFECYCLE_VIOLATION`).
The state machine is the single source of truth; the hook consults it via the
`CanAttest(status)` predicate.

---

## 11. PromptFoo CI Integration

### 11.1 Forgejo Action Trigger

```yaml
# .forgejo/workflows/promptfoo.yaml
name: Prompt Regression Tests
on:
  push:
    paths: ['prompts/**', '.promptfoo.yaml']
  pull_request:
    paths: ['prompts/**']
jobs:
  promptfoo:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Run PromptFoo
        run: npx promptfoo eval
      - name: Store results
        uses: actions/upload-artifact@v4
        with:
          name: promptfoo-results
          path: promptfoo-results.json
      - name: Write back status
        run: helix prompt postci --results promptfoo-results.json
```

### 11.2 Test Suite Structure

```yaml
# .promptfoo.yaml
prompts:
  - file://prompts/agent-identity/v1.1.0/prompt.md
  - file://prompts/cost-estimator/v1.0.0/prompt.md
providers:
  - deepseek:deepseek-v4-flash
  - deepseek:deepseek-v4-pro
  - zai-glm:glm-5.2
tests:
  - description: "Agent identity prompt produces valid Go"
    assert:
      - { type: contains, value: "package identity" }
      - { type: not-contains, value: "TODO" }
      - { type: llm-rubric, value: "Output contains a valid Go struct with JSON tags" }
  - description: "Cost estimator handles cache-aware pricing"
    assert:
      - { type: contains, value: "cache_hit_ratio" }
      - { type: javascript, value: "output.includes('$0.014')" }
  - description: "Prompt does not hallucinate APIs"
    assert:
      - { type: not-contains, value: "api.openai.com" }   # Helix never calls OpenAI directly
      - { type: not-contains, value: "TODO" }
```

### 11.3 Blocking Rules

| PromptFoo Result | Commit Allowed? |
|-----------------|----------------|
| All tests pass | ✅ Yes |
| Some tests fail | ❌ No (unless `--no-verify`) |
| Provider unavailable | ⚠️ Warning (non-blocking — providers are external) |
| Timeout (CI runner killed) | ❌ No (must re-run) |
| `unknown` (no run yet) | ⚠️ Warn (first attestation before CI completes) |

The hook treats `fail` as a hard block and `unknown`/`unavailable` as a soft
warning, so external-provider outages cannot freeze all development while
genuine regressions are still blocked.

---

## 12. Provenance Chain

### 12.1 Traceability Path

```
git commit abc123
  └─ attestation:  Prompt: sha256:def456
     └─ prompts/cost-estimator/v1.0.0/metadata.yaml
        └─ spec_ref:     specs/cost-estimator.md
           └─ work_item: WI-helix-002
              └─ intent:    "Build pre-flight cost estimator"
                 └─ human:    @bane (Telegram, 2026-06-19)
```

Every commit traces back to the human intent that spawned it, and every intent
traces forward to its commits via the `commits[]` array on each prompt's
metadata.

### 12.2 Chain Verification

```
$ helix prompt verify abc123def --full-chain

COMMIT: abc123def
  MESSAGE:     feat(identity): add rate limiter
  PROMPT:      sha256:a1b2c3d4 (agent-identity v1.1.0)
    STATUS:    active ✅
    HASH:      verified ✅
    PROMPTFOO: pass (12/12) ✅
    SPEC:      specs/agent-identity.md §13 ✅
    WORK ITEM: WI-helix-001 (complete) ✅
    INTENT:    "Build agent identity feature in Forgejo" ✅

PROVENANCE CHAIN: COMPLETE ✅
All links verified. No tampering detected.
```

### 12.3 Tamper Detection

If a prompt file is edited in place after attestation, the recomputed hash
diverges from the stored hash regardless of where the file was moved —
detection is keyed on normalized content, not path:

```
$ helix prompt verify abc123def

COMMIT: abc123def
  PROMPT: sha256:a1b2c3d4 (agent-identity v1.1.0)
    HASH MISMATCH ❌
    Stored hash:    sha256:a1b2c3d4...
    Computed hash:  sha256:e5f6a7b8...
    File modified after attestation.

VERDICT: TAMPER DETECTED. Prompt content does not match attested hash.
```

---

## 13. Error Taxonomy and Exit Codes

| Exit | Constant | Condition | Message |
|------|----------|-----------|---------|
| 0 | — | Success (or dry-run, or empty registry) | — |
| 1 | `ATTESTATION_MISSING` | No `Prompt: sha256:<hash>` line in commit message | `commit message must include 'Prompt: sha256:<hash>'` |
| 2 | `PROMPT_NOT_FOUND` | Hash not in `_index.yaml` | `sha256:<hash> not in registry` |
| 3 | `TAMPER_DETECTED` | recomputed hash != metadata.hash | `stored hash != computed hash for <component>/<version>` |
| 4 | `LIFECYCLE_VIOLATION` | Status not in {attested, active} (deprecated warns) | `prompt status is <status>, must be active or attested` |
| 5 | `PROMPTFOO_FAILED` | PromptFoo suite failed | `<N>/<M> tests failed` |
| 6 | `METADATA_INVALID` / `ATTESTATION_MALFORMED` | Bad metadata field or malformed hash line | `<field> is <reason>` |
| 10 | `DRY_RUN` | Dry-run completed without writing | `would attest <hash>` |

**Error kinds → exit codes:** `config` → 6; `lifecycle` → 4; `integrity` → 3;
`regression` → 5; `missing` → 1 / 2. Partial operations are NOT supported in
v2: every subcommand either fully succeeds or fully fails and writes nothing
(atomic semantics). The only multi-record operation, `list`, never fails for
partial reasons — it reports what it found and exits 0.

---

## 14. Test Strategy

| Layer | File | What it tests | Mock/Real |
|-------|------|---------------|-----------|
| Unit | `hasher_test.go` | Normalization: CRLF→LF, whitespace collapse, trailing-ws strip, fence exemption | Pure unit |
| Unit | `hasher_test.go` | SHA-256 stable for normalized input; differs for different content | Pure unit |
| Unit | `lifecycle_test.go` | All 7 states; all valid transitions; all invalid transitions blocked | Pure unit |
| Unit | `registry_test.go` | Register, lookup by hash, lookup by component/version, atomic write | File fixtures |
| Integration | `attest_test.go` | Full workflow: register → attest → verify → transition active | Real filesystem |
| Integration | `hook_test.go` | Hook: accept attested; reject missing; reject tampered; warn deprecated | Real git repo |
| Contract | `metadata_test.go` | `metadata.yaml` + `_index.yaml` schema validation | File fixtures |
| E2E | `e2e_test.go` | Register → commit with attestation → verify chain → tamper → detect | Real git repo |

Integration/E2E tests skip if `HELIX_PROMPTS_DIR` is not writable or git is
unavailable. Unit tests never touch the filesystem outside temp dirs.

Test fixtures (in `pkg/prompt/testdata/`): `prompt-normalized.md` (canonical reference), `prompt-crlf.md`, `prompt-trailing-ws.md`, `prompt-extra-newline.md` (all must hash-equal the canonical); `prompt-fenced.md` (internal spaces, must NOT collapse); `prompt-different.md` (must hash-differ); `_index.yaml` (3 prompts, varied statuses); `metadata-valid.yaml`, `metadata-invalid-missing-field.yaml`, `metadata-invalid-status.yaml`.

---

## 15. Example Outputs

### Register a Prompt

```
$ helix prompt register agent-identity v1.1.0 \
    --model glm-5.2 --provider zai-glm --spec-ref specs/agent-identity.md

PROMPT REGISTERED:
  Component:  agent-identity
  Version:    v1.1.0
  Hash:       sha256:a1b2c3d4e5f6789012345678901234567890abcdef0123456789abcdef012345
  Model:      glm-5.2 (zai-glm)
  Status:     draft
  Location:   prompts/agent-identity/v1.1.0/
Next: helix prompt attest agent-identity v1.1.0, then commit with the printed `Prompt: sha256:<hash>` line.
```

### Commit With Attestation (hook passes) vs Without (rejected)

```
$ git commit -m "feat(identity): add rate limiter

Prompt: sha256:a1b2c3d4e5f6789012345678901234567890abcdef0123456789abcdef012345
Model: glm-5.2
Provider: zai-glm

Co-authored-by: wojons <wojonstech@gmail.com>"

GitReins commit-msg hook:
  [PASS] Attestation: sha256:a1b2c3d4 (agent-identity v1.1.0, active)
  [PASS] PromptFoo:   12/12 tests passed
  [PASS] Hash match:  verified
  [PASS] All checks passed. Commit allowed.
```

A commit missing the attestation line is rejected: the hook prints
`[FAIL] Attestation: missing 'Prompt: sha256:<hash>' in commit message` and
exits 1, telling the operator to re-run with the line or use `--no-verify`
(audited).

### List

```
$ helix prompt list

COMPONENT        VERSION   STATUS      MODEL           PROMPTFOO  COMMITS
agent-identity   v1.0.0    deprecated  glm-5.2         pass       7
agent-identity   v1.1.0    active      glm-5.2         pass       3
cost-estimator   v1.0.0    attested    deepseek-v4-pro pass       1
pr-negotiation   v0.9.0    draft       kimi-for-coding unknown    0
```

---

## 16. Implementation Status (v2 target)

| Component | Status | Notes |
|-----------|--------|-------|
| CLI (4 subcommands) | ⏳ Stub | register, attest, verify, list |
| SHA-256 hasher (5-step norm, fence-aware, frontmatter stripped) | ⏳ Stub | §8.2 |
| Registry (`_index.yaml` + atomic writes) + metadata/index validator | ⏳ Stub | read/write/query/rebuild, INDEX_STALE |
| Lifecycle state machine (7 states, full transition table) | ⏳ Stub | §10 |
| GitReins commit-msg hook (8-step algorithm) | ⏳ Stub | §9.2 |
| PromptFoo CI action + postci (status writeback) | ⏳ Stub | §11 |
| Provenance chain verifier + rename-robust tamper detector | ⏳ Stub | §12 |
| Audit logger (append-only JSONL) + dry-run mode | ⏳ Stub | all ops + overrides |

---

## 17. Verification Checklist

- [ ] `go build ./cmd/helix-prompt` exits 0; `go vet ./...` clean
- [ ] SHA-256 hash-equal for CRLF vs LF, with/without trailing whitespace and extra newlines
- [ ] SHA-256 preserves fenced-code-block internal spacing; differs for different content
- [ ] Registry: register → lookup returns correct metadata
- [ ] Lifecycle: `draft` cannot be attested; `active` can; `retired` rejected; every forbidden transition → exit 4
- [ ] Hook: missing → exit 1; malformed → exit 6; not-found → exit 2; valid → PASS; deprecated → WARN
- [ ] Tamper: modified prompt → exit 3; renamed-but-unedited prompt → still verifies
- [ ] Index: `metadata.hash` correct but index stale → rebuild + warn
- [ ] Provenance: commit → prompt → spec → work item fully traceable
- [ ] PromptFoo: prompt-file change triggers workflow; test failure blocks attestation into active
- [ ] Atomic writes: crash mid-write never corrupts the index
- [ ] Prompts committed to git, never stored externally; metadata YAML validates required fields

---

## 18. Package Structure

```
github.com/totalwindupflightsystems/helix/
├── cmd/helix-prompt/main.go             CLI entry point
├── pkg/prompt/
│   ├── types.go                         PromptVersion, Metadata, LifecycleStatus
│   ├── registry.go                      Register, Lookup, List, UpdateStatus, atomic write
│   ├── hasher.go                        Normalize + SHA-256 + frontmatter strip
│   ├── lifecycle.go                     State machine + transition table + CanAttest
│   ├── attester.go                      Commit-message parser + hook verification
│   ├── provenance.go                    Chain walker: commit → prompt → spec → intent
│   ├── hook.go                          GitReins commit-msg hook logic (8-step)
│   ├── audit.go                         Append-only JSONL logger
│   └── testdata/                        normalized/crlf/trailing-ws/fenced/different .md, _index.yaml, metadata-*.yaml
├── .promptfoo.yaml                      Global prompt test config
├── .forgejo/workflows/promptfoo.yaml    CI trigger
├── prompts/                             Per-component prompt versions
└── specs/prompt-registry-v2.md          This document
```

---

## 19. Observability

- `--verbose` logs every registry operation:
  `timestamp [level] operation=REGISTER component=<name> version=<ver> hash=<sha256> status=draft`.
  The GitReins hook logs attestation results to stderr.
- Registry audit trail in `~/.helix/prompts/audit.jsonl` (append-only):
  ```jsonl
  {"timestamp":"...","operation":"register","component":"agent-identity","version":"v1.1.0","hash":"sha256:...","agent":"wojons"}
  {"timestamp":"...","operation":"attest","commit":"abc123","hash":"sha256:...","status":"pass"}
  {"timestamp":"...","operation":"override","commit":"def456","actor":"wojons","reason":"--no-verify override"}
  {"timestamp":"...","operation":"transition","from":"attested","to":"active","trigger":"second_commit"}
  ```
- Metrics (Prometheus): `helix_prompts_total{status}`, `helix_prompt_attestations_total`,
  `helix_prompt_attestation_failures_total{reason}`, `helix_prompt_versions_total{component}`,
  `helix_prompt_overrides_total` (spike triggers review).

---

## Document Status

- [x] Mission and scope; prompt storage model (per-component, per-version, immutable after attestation)
- [x] Metadata schema (all fields, required flags) + registry index with consistency rules
- [x] SHA-256 hashing with 5-step normalization + fenced-block exemption
- [x] Lifecycle state machine (7 states, complete transition table)
- [x] Commit attestation format + 8-step GitReins hook algorithm
- [x] PromptFoo CI integration (Forgejo Action, test suite, blocking rules)
- [x] Provenance chain (commit → prompt → spec → work item → intent) + tamper detection
- [x] Filesystem layout + package structure; error taxonomy (exit codes 0–6, 10)
- [x] Test strategy with fixture list; observability (logs, metrics, audit trail)
- [x] Implementation status tracking; verification checklist; example outputs
