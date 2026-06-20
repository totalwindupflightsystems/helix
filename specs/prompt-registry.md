# Helix Feature 4 — Prompt Registry

**Status:** v1 specification (build-ready, zero implementation)
**Spec version:** 1.0
**Last updated:** 2026-06-19
**Depends on:** Feature 1 (Agent Identity), Feature 2 (Cost Estimator), GitReins v0.3.0+ (commit-msg hook)
**Blocks:** Feature 5 (Marketplace — prompt quality feeds reputation)

This document is the authoritative implementation reference for the Helix
Prompt Registry. It specifies prompt storage, versioning, immutability,
commit attestation, PromptFoo CI integration, and the prompt lifecycle state
machine. Every commit in Helix links to an attested prompt — this is the
system that makes that enforceable.

---

## 1. Mission

Make prompts first-class, versioned, immutable artifacts. Every commit
MUST carry a prompt attestation (SHA-256 hash of the prompt that generated
the code). The prompt registry stores, versions, and validates prompts.
GitReins commit-msg hook enforces attestation. PromptFoo CI runs regression
tests on every prompt change. The result is a complete provenance chain:
human intent → spec → prompt → code → commit → merge.

---

## 2. Scope

### In scope (v1)
- CLI with 4 subcommands (`register`, `attest`, `verify`, `list`)
- Per-repo prompt storage: `prompts/<component>/<version>/prompt.md` + `metadata.yaml`
- SHA-256 content hashing (normalized: whitespace-collapsed, newline-normalized)
- Prompt lifecycle state machine: draft → proposed → reviewed → attested → active → deprecated → retired
- Commit attestation enforcement via GitReins commit-msg hook
- PromptFoo CI integration (Forgejo Action, .promptfoo.yaml)
- Provenance chain: prompt → spec → work item → commit
- Tamper detection: hash mismatch → commit blocked
- Dry-run mode

### Out of scope (v1)
- Cross-repo prompt sharing (each repo owns its prompts)
- Prompt templating or variable substitution
- Prompt A/B testing or canary deployment
- Automated prompt optimization
- Prompt analytics dashboard

---

## 3. Inputs

### 3.1 Prompt Storage Structure

```
prompts/
├── _index.yaml                        Registry index (all prompts)
├── agent-identity/
│   ├── v1.0.0/
│   │   ├── prompt.md                  The prompt text
│   │   └── metadata.yaml              Version metadata
│   └── v1.1.0/
│       ├── prompt.md
│       └── metadata.yaml
├── cost-estimator/
│   └── v1.0.0/
│       ├── prompt.md
│       └── metadata.yaml
└── ...
```

### 3.2 Metadata Schema

```yaml
# prompts/<component>/<version>/metadata.yaml
version: "1.1.0"
component: "agent-identity"
hash: "sha256:a1b2c3d4e5f6..."
model: "glm-5.2"
provider: "zai-glm"
author: "agent-wojons"
author_trust: 85
spec_ref: "specs/agent-identity.md"
spec_version: "1.0"
work_item: "WI-helix-001"
created_at: "2026-06-19T10:00:00Z"
attested_at: "2026-06-19T10:05:00Z"
status: "active"
previous_version: "v1.0.0"
changes: "Added rate limiter configuration, fixed ED25519 fingerprint format"
cost:
  estimated_input_tokens: 80000
  estimated_output_tokens: 15000
  estimated_cost_usd: 0.024
commits:
  - sha: "abc123def456..."
    repo: "totalwindupflightsystems/helix"
    files_changed: 3
    merged: true
promptfoo:
  test_suite: "prompts/agent-identity/promptfoo.yaml"
  last_run: "2026-06-19T10:05:00Z"
  status: "pass"
```

### 3.3 CLI Interface

```
helix prompt register <component> <version> [flags]
  --prompt-file      Path to prompt.md (default: prompts/<component>/<version>/prompt.md)
  --model            Model that produced this prompt
  --provider         Provider name
  --spec-ref         Link to spec file this prompt implements
  --dry-run          Validate without writing

helix prompt attest <component> <version> [flags]
  --commit-sha       The commit being attested (default: HEAD)
  --force            Skip validation (emergency only)

helix prompt verify <commit-sha> [flags]
  --check-hash       Verify prompt hash matches content
  --check-lifecycle  Verify prompt is in 'active' state
  --check-promptfoo  Verify PromptFoo tests passed

helix prompt list [flags]
  --component        Filter by component
  --status           Filter by lifecycle status (default: active)
  --model            Filter by model
  --format           json|table (default: table)
```

---

## 4. Operating Contract

- **NEVER** allow a commit without a prompt attestation. GitReins commit-msg hook enforces this.
- **NEVER** modify an attested prompt. Editing = new version.
- **ALWAYS** hash the normalized prompt (whitespace-collapsed, newline-normalized, trailing newline stripped).
- **ALWAYS** run PromptFoo tests when a prompt file changes. Block commit if tests fail.
- **DO NOT** allow deprecated or retired prompts in commit attestation.
- **DO NOT** accept attestation for a prompt whose hash doesn't match stored content.
- **DO NOT** store secrets in prompts. Prompts are committed to the repo. Secrets belong in env vars.

---

## 5. Assumptions

- Each repo contains its own `prompts/` directory (no global registry in v1).
- Prompts are plain markdown files with optional YAML frontmatter.
- SHA-256 is sufficient for content addressing (collision resistance).
- PromptFoo is configured via `.promptfoo.yaml` in the repo root.
- GitReins commit-msg hook has access to the prompt registry.
- Agents self-declare which prompt they used (attestation is agent-driven, hook-verified).

---

## 6. Architecture

```
                          ┌──────────────────────────────────┐
                          │       cmd/helix-prompt            │
                          │    (Cobra CLI: 4 subcommands)     │
                          └──────────────┬───────────────────┘
                                         │
                          ┌──────────────▼───────────────────┐
                          │      pkg/prompt/registry           │
                          │  Register → Hash → Store → Attest  │
                          │  Verify → Lifecycle → Audit        │
                          └───────┬───────────────┬───────────┘
                                  │               │
                  ┌───────────────▼──┐       ┌─────▼──────────────┐
                  │ pkg/prompt/      │       │ pkg/prompt/         │
                  │ hasher           │       │ lifecycle           │
                  │ (SHA-256,        │       │ (state machine,     │
                  │  normalization,  │       │  transitions,       │
                  │  tamper detect)  │       │  status checks)     │
                  └──────────────────┘       └─────────────────────┘
                                  │
                  ┌───────────────▼──────────────────────────────┐
                  │              External Systems                 │
                  │  GitReins: commit-msg hook                    │
                  │  PromptFoo: Forgejo Action CI                 │
                  │  Git: commit history for provenance chain     │
                  └──────────────────────────────────────────────┘
```

---

## 7. Prompt Lifecycle State Machine

```
draft → proposed → reviewed → attested → active → deprecated → retired
  │        │          │                                  │
  └────────┴──────────┴──────────────────────────────────┘
                    (can move backward to draft)
```

| State | Meaning | Allowed in Attestation? |
|-------|---------|------------------------|
| `draft` | Prompt is being written. Not yet proposed. | ❌ No |
| `proposed` | Submitted for review. Chimera reviews against spec. | ❌ No |
| `reviewed` | Chimera approved. Ready for attestation. | ❌ No |
| `attested` | First commit links to this prompt. Now immutable. | ✅ Yes |
| `active` | In active use. Multiple commits reference it. | ✅ Yes |
| `deprecated` | Superseded by newer version. Still allowed for transition period. | ⚠️ Warn (30-day grace) |
| `retired` | No longer valid. Commits referencing it are blocked. | ❌ No |

**Transition rules:**
- `draft → proposed`: Agent submits prompt. Triggers Chimera spec-conformance review.
- `proposed → reviewed`: Chimera review passes. Automatically advances.
- `proposed → draft`: Chimera review fails. Agent must revise.
- `reviewed → attested`: First commit references this prompt. Automatically advances.
- `attested → active`: Second commit references this prompt. Automatically advances.
- `active → deprecated`: Newer version published. Manual or auto (if newer version has 3+ commits).
- `deprecated → retired`: 90 days in deprecated OR no commits in 180 days.
- `deprecated → active`: Rollback (emergency). Requires human approval.

---

## 8. Commit Attestation Format

### 8.1 Commit Message Template

```
<type>(<component>): <description>

Prompt: sha256:<hex-hash>
Model: <model-name>
Provider: <provider-name>
Spec: <spec-file> §<section>
Cost: $<estimated-cost>

Co-authored-by: <agent-author>
```

Example:
```
feat(identity): add rate limiter to Forgejo provisioner

Prompt: sha256:a1b2c3d4e5f6789012345678901234567890abcdef
Model: glm-5.2
Provider: zai-glm
Spec: specs/agent-identity.md §13
Cost: $0.024

Co-authored-by: wojons <wojonstech@gmail.com>
```

### 8.2 GitReins Commit-Msg Hook

The hook runs on every commit:

```
1. Parse commit message for "Prompt: sha256:<hash>" line
2. If missing → REJECT: "Commit must include prompt attestation"
3. Look up hash in prompts/_index.yaml
4. If not found → REJECT: "Prompt hash not in registry"
5. Check prompt status:
   - active/attested → PASS
   - deprecated → WARN (allowed but logged)
   - retired/draft/proposed/reviewed → REJECT
6. Verify hash matches stored prompt content → if mismatch → REJECT: "Tamper detected"
7. Verify PromptFoo last_run status is "pass" → if fail → REJECT: "PromptFoo regression"
```

### 8.3 Override

```
git commit --no-verify -m "emergency: fix production outage"
# Allowed. Logged to audit trail with:
# timestamp, user, commit SHA, reason="--no-verify override"
```

---

## 9. Prompt Hashing

### 9.1 Normalization Pipeline

```go
func Normalize(prompt string) string {
    // 1. Normalize line endings to \n
    // 2. Collapse whitespace: multiple spaces → single space
    // 3. Strip trailing whitespace from each line
    // 4. Strip trailing newline at end of file
    // 5. Keep leading whitespace (markdown indentation matters)
    return normalized
}

func Hash(prompt string) string {
    normalized := Normalize(prompt)
    hash := sha256.Sum256([]byte(normalized))
    return "sha256:" + hex.EncodeToString(hash[:])
}
```

### 9.2 Why Normalization Matters

Without normalization, identical prompts differ by:
- Line endings (CRLF vs LF) → different hash
- Trailing whitespace → different hash
- Extra newline at end → different hash

Normalization ensures content-equivalent prompts produce the same hash.

---

## 10. PromptFoo CI Integration

### 10.1 Forgejo Action Trigger

```yaml
# .forgejo/workflows/promptfoo.yaml
name: Prompt Regression Tests
on:
  push:
    paths:
      - 'prompts/**'
      - '.promptfoo.yaml'
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
```

### 10.2 Test Suite Structure

```yaml
# .promptfoo.yaml
prompts:
  - file://prompts/agent-identity/v1.1.0/prompt.md
  - file://prompts/cost-estimator/v1.0.0/prompt.md

providers:
  - id: deepseek:deepseek-v4-flash
  - id: deepseek:deepseek-v4-pro
  - id: zai-glm:glm-5.2

tests:
  - description: "Agent identity prompt produces valid Go"
    vars:
      spec_file: "specs/agent-identity.md"
    assert:
      - type: contains
        value: "package identity"
      - type: not-contains
        value: "TODO"
      - type: llm-rubric
        value: "The output contains a valid Go struct definition with JSON tags"

  - description: "Cost estimator prompt handles cache-aware pricing"
    vars:
      model: "deepseek-v4-pro"
    assert:
      - type: contains
        value: "cache_hit_ratio"
      - type: javascript
        value: "output.includes('$0.014')"

  - description: "Prompt does not hallucinate APIs"
    assert:
      - type: not-contains
        value: "api.openai.com"  # Helix never calls OpenAI directly
      - type: not-contains
        value: "TODO"             # No unfinished stubs
```

### 10.3 Blocking Rules

| PromptFoo Result | Commit Allowed? |
|-----------------|----------------|
| All tests pass | ✅ Yes |
| Some tests fail | ❌ No (unless `--no-verify`) |
| Provider unavailable | ⚠️ Warning (non-blocking — providers are external) |
| Timeout (CI runner killed) | ❌ No (must re-run) |

---

## 11. Prompt Provenance Chain

### 11.1 Traceability Path

```
git commit abc123
  → attestation: Prompt: sha256:def456
    → prompts/cost-estimator/v1.0.0/metadata.yaml
      → spec_ref: specs/cost-estimator.md
        → work_item: WI-helix-002
          → intent: "Build pre-flight cost estimator"
            → human: @bane (Telegram, 2026-06-19)
```

Every commit can be traced back to the human intent that spawned it.

### 11.2 Chain Verification

```bash
$ helix prompt verify abc123def --full-chain

COMMIT: abc123def
  PROMPT: sha256:a1b2c3d4 (cost-estimator v1.0.0)
    STATUS: active ✅
    HASH MATCH: verified ✅
    PROMPTFOO: pass ✅
    SPEC: specs/cost-estimator.md §7 ✅
    WORK ITEM: WI-helix-002 (complete) ✅
    INTENT: "Build pre-flight cost estimator" ✅

PROVENANCE CHAIN: COMPLETE ✅
```

### 11.3 Tamper Detection

If a prompt file is edited after attestation:
```
$ helix prompt verify abc123def

COMMIT: abc123def
  PROMPT: sha256:a1b2c3d4 (cost-estimator v1.0.0)
    HASH MISMATCH ❌
    Stored hash: sha256:a1b2c3d4
    Computed hash: sha256:e5f6a7b8
    File modified after attestation!

VERDICT: TAMPER DETECTED. Prompt content does not match attested hash.
```

---

## 12. Filesystem Layout

### Repository

```
repo/
├── prompts/
│   ├── _index.yaml                    # Registry index
│   └── <component>/
│       └── <version>/
│           ├── prompt.md              # The prompt text
│           ├── metadata.yaml          # Version metadata
│           └── promptfoo.yaml         # Per-prompt test suite (optional)
├── .promptfoo.yaml                    # Global prompt test config
├── .forgejo/workflows/promptfoo.yaml  # CI trigger
└── .git/hooks/commit-msg             # GitReins hook
```

### State

```
~/.helix/prompts/
  registry.db                          # SQLite index (fast lookups)
  audit.jsonl                          # All registry operations
```

---

## 13. Error Taxonomy and Exit Codes

| Exit | Condition | Message |
|------|-----------|---------|
| 0 | Success | — |
| 1 | Missing attestation | `ATTESTATION_MISSING: commit message must include 'Prompt: sha256:<hash>'` |
| 2 | Hash not found | `PROMPT_NOT_FOUND: sha256:<hash> not in registry` |
| 3 | Hash mismatch | `TAMPER_DETECTED: stored hash != computed hash for <component>/<version>` |
| 4 | Invalid lifecycle | `LIFECYCLE_VIOLATION: prompt status is <status>, must be active or attested` |
| 5 | PromptFoo failure | `PROMPTFOO_FAILED: <N>/<M> tests failed` |
| 6 | Invalid metadata | `METADATA_INVALID: <field> is <reason>` |
| 10 | Dry-run | `DRY_RUN: would attest <hash>` |

---

## 14. Test Strategy

| Layer | File | What it tests | Mock/Real |
|-------|------|---------------|-----------|
| Unit | `hasher_test.go` | Normalization: CRLF→LF, whitespace collapse, boundary cases | Pure unit |
| Unit | `hasher_test.go` | SHA-256 produces consistent output for normalized input | Pure unit |
| Unit | `lifecycle_test.go` | All 7 states, all valid transitions, all invalid transitions blocked | Pure unit |
| Unit | `registry_test.go` | Register, lookup by hash, lookup by component/version | File fixtures |
| Integration | `attest_test.go` | Full attest workflow: register → attest → verify | Real filesystem |
| Integration | `gitreins_hook_test.go` | Commit-msg hook: accept attested, reject missing, reject tampered | Real git repo |
| Contract | `metadata_test.go` | metadata.yaml schema validation | File fixtures |
| E2E | `e2e_test.go` | Register prompt → commit with attestation → verify chain → tamper → detect | Real git repo |

Test fixtures (in `pkg/prompt/testdata/`):
- `prompt-normalized.md` (canonical form)
- `prompt-crlf.md` (Windows line endings — should normalize to same hash)
- `prompt-trailing-whitespace.md` (should normalize)
- `prompt-extra-newline.md` (should normalize)
- `_index.yaml` (registry with 3 prompts, various statuses)
- `metadata-valid.yaml`
- `metadata-invalid-missing-field.yaml`
- `metadata-invalid-status.yaml`

---

## 15. Observability

- `--verbose` logs all registry operations:
  `timestamp [level] operation=REGISTER component=<name> version=<ver> hash=<sha256> status=draft`
- GitReins hook logs attestation results to stderr
- Registry audit trail in `~/.helix/prompts/audit.jsonl`:
  ```jsonl
  {"timestamp":"...","operation":"register","component":"agent-identity","version":"v1.0.0","hash":"sha256:...","agent":"wojons"}
  {"timestamp":"...","operation":"attest","commit":"abc123","hash":"sha256:...","status":"pass"}
  {"timestamp":"...","operation":"transition","component":"agent-identity","version":"v1.0.0","from":"attested","to":"active","trigger":"second_commit"}
  ```
- Metrics (Prometheus):
  - `helix_prompts_total{status="active|deprecated|retired"}`
  - `helix_prompt_attestations_total` (counter)
  - `helix_prompt_attestation_failures_total{reason="missing|tamper|lifecycle|promptfoo"}` (counter)
  - `helix_prompt_versions_total{component="..."}` (gauge)

---

## 16. Implementation Status (v1 target)

| Component | Status | Notes |
|-----------|--------|-------|
| CLI (4 subcommands) | ⏳ Stub | register, attest, verify, list |
| SHA-256 hasher with normalization | ⏳ Stub | 5-step normalize pipeline |
| Registry (_index.yaml) | ⏳ Stub | Read/write/query |
| Metadata validator | ⏳ Stub | YAML schema validation |
| Lifecycle state machine | ⏳ Stub | 7 states, all transitions |
| GitReins commit-msg hook integration | ⏳ Stub | Parse → verify → pass/reject |
| PromptFoo CI action | ⏳ Stub | Forgejo Action YAML |
| Provenance chain verifier | ⏳ Stub | commit → prompt → spec → work item → intent |
| Tamper detector | ⏳ Stub | Hash mismatch detection |
| Audit logger (JSONL) | ⏳ Stub | All operations logged |
| Dry-run mode | ⏳ Stub | All subcommands support --dry-run |

---

## 17. Verification Checklist

- [ ] `go build ./cmd/helix-prompt` exits 0
- [ ] `go vet ./...` clean
- [ ] SHA-256 produces same hash for CRLF and LF versions of same content
- [ ] SHA-256 produces different hash for different content
- [ ] Registry: register → lookup returns correct metadata
- [ ] Lifecycle: draft can't be attested, active can
- [ ] Lifecycle: retired prompt attestation rejected
- [ ] Lifecycle: transition to invalid state rejected
- [ ] GitReins hook: missing attestation → REJECT
- [ ] GitReins hook: hash not in registry → REJECT
- [ ] GitReins hook: valid attestation → PASS
- [ ] Tamper detection: modified prompt → hash mismatch → detected
- [ ] Provenance chain: commit → prompt → spec → work item traceable
- [ ] PromptFoo CI: prompt change triggers workflow
- [ ] PromptFoo CI: test failure blocks commit
- [ ] Prompts committed to git, not stored externally
- [ ] Metadata YAML validates all required fields

---

## 18. Example Outputs

### Register a Prompt

```
$ helix prompt register agent-identity v1.1.0 \
    --model glm-5.2 --provider zai-glm \
    --spec-ref specs/agent-identity.md

PROMPT REGISTERED:
  Component:  agent-identity
  Version:    v1.1.0
  Hash:       sha256:a1b2c3d4e5f6789012345678901234567890abcdef
  Model:      glm-5.2 (zai-glm)
  Spec:       specs/agent-identity.md
  Status:     draft
  Location:   prompts/agent-identity/v1.1.0/

Next steps:
  1. Review prompt: cat prompts/agent-identity/v1.1.0/prompt.md
  2. Propose for review: helix prompt transition agent-identity v1.1.0 proposed
  3. After review: attest with commit
```

### Commit with Attestation

```
$ git commit -m "feat(identity): add rate limiter

Prompt: sha256:a1b2c3d4e5f6789012345678901234567890abcdef
Model: glm-5.2
Provider: zai-glm
Cost: $0.024

Co-authored-by: wojons <wojonstech@gmail.com>"

GitReins commit-msg hook:
  [PASS] Attestation: sha256:a1b2c3d4 (agent-identity v1.1.0, active)
  [PASS] PromptFoo: 12/12 tests passed
  [PASS] Hash match: verified
  [PASS] All checks passed. Commit allowed.
```

### Commit Without Attestation (Rejected)

```
$ git commit -m "fix: typo in provisioner"

GitReins commit-msg hook:
  [FAIL] Attestation: missing 'Prompt: sha256:<hash>' in commit message
  
Commit rejected. To override: git commit --no-verify
```

### Verify Provenance Chain

```
$ helix prompt verify abc123def --full-chain

COMMIT:      abc123def
  MESSAGE:   feat(identity): add rate limiter
  PROMPT:    sha256:a1b2c3d4 (agent-identity v1.1.0)
    STATUS:  active ✅
    HASH:    verified ✅
    PROMPTFOO: pass (12/12) ✅
    SPEC:    specs/agent-identity.md §13 ✅
    WORK ITEM: WI-helix-001 ✅
    INTENT:  "Build agent identity feature in Forgejo" ✅

PROVENANCE CHAIN: COMPLETE ✅
All links verified. No tampering detected.
```

---

## 19. Package Structure

```
github.com/totalwindupflightsystems/helix/
├── cmd/helix-prompt/main.go             CLI entry point
├── pkg/prompt/
│   ├── types.go                         PromptVersion, Metadata, LifecycleStatus
│   ├── registry.go                      Register, Lookup, List, UpdateStatus
│   ├── hasher.go                        Normalize + SHA-256
│   ├── lifecycle.go                     State machine, valid transitions
│   ├── attester.go                      Commit message parser, attestation verifier
│   ├── provenance.go                    Chain walker: commit → prompt → spec → intent
│   ├── hook.go                          GitReins commit-msg hook logic
│   └── testdata/
│       ├── prompt-normalized.md
│       ├── prompt-crlf.md
│       ├── _index.yaml
│       ├── metadata-valid.yaml
│       └── metadata-invalid.yaml
├── .promptfoo.yaml                      Global prompt test config
├── .forgejo/workflows/promptfoo.yaml    CI trigger
├── prompts/                             Per-component prompt versions
└── specs/prompt-registry.md             This document
```

---

## Document Status

- [x] Mission and scope defined
- [x] Prompt storage structure (per-component, per-version)
- [x] Metadata schema (all fields)
- [x] Lifecycle state machine (7 states, all transitions)
- [x] Commit attestation format + GitReins hook integration
- [x] SHA-256 hashing with normalization pipeline
- [x] PromptFoo CI integration (Forgejo Action, test suite, blocking rules)
- [x] Provenance chain (commit → prompt → spec → work item → intent)
- [x] Tamper detection (hash mismatch)
- [x] Filesystem layout
- [x] Error taxonomy (8 exit codes)
- [x] Test strategy with fixture list
- [x] Observability (logs, metrics, audit trail)
- [x] Implementation status tracking
- [x] Verification checklist
- [x] Example outputs (4 scenarios)
- [x] Package structure
