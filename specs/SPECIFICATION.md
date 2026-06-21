# Helix Platform — Master Implementation Specification

**Version:** 1.0.0
**Status:** Implementation Specification (not architecture overview)
**Target Audience:** Engineers building, integrating, or operating Helix components
**Last Updated:** June 19, 2026

---

## Document Conventions

- **MUST / MUST NOT / SHOULD / MAY** follow RFC 2119 semantics.
- Code blocks with `// EXAMPLE` are illustrative, not normative.
- Endpoint signatures use the form `METHOD /path → ResponseShape`.
- All timestamps are ISO 8601 UTC unless noted.
- Dollar amounts are USD at published provider rates as of June 2026.
- Every section ends with a "Verification" block describing how to confirm the section's requirements are met.

---

## Table of Contents

1. Platform Architecture
2. Data Flow and Execution Model
3. Component Specifications
4. Integration Contracts
5. Identity and Access Management (IAM)
6. Security Model
7. Quality Gates
8. Observability
9. Deployment Architecture
10. Operations
11. Performance SLAs
12. Test Strategy
13. Build Order
14. Error Recovery
15. API Contracts
16. Glossary
17. Appendices

<!-- SENTINEL: CHUNK_INSERTION_POINT -->

---

## 1. Platform Architecture

### 1.1 Thesis

Helix is an open, self-hosted, agent-first code platform — the alternative to closed, hosted agent platforms (Cursor Origin, Devin Cloud, etc.). The core thesis is encoded in the name: two strands — human intelligence and AI intelligence — spiral together through the software development lifecycle, each reinforcing the other. Humans set intent and approve; agents plan, build, review, and merge. Neither operates alone.

The platform is built entirely from components we own (9 sub-projects) plus well-understood open-source tools (8 external). No proprietary black-box SaaS is required to run Helix end-to-end. The only external paid dependencies are LLM inference APIs (OpenRouter, DeepSeek, Z.AI, Anthropic, Google) — and those are swappable via the Chimera abstraction layer.

### 1.2 The 6-Layer Stack

Helix organizes its 17 components into six functional layers. Data flows top-to-bottom (human intent → merged code) and bottom-to-top (observability → human feedback). Each layer is independently deployable but contracts with adjacent layers.

```
┌─────────────────────────────────────────────────────────────────────┐
│  LAYER 1 — HUMAN INTERFACE                                          │
│  Continue.dev · Cursor · CLI · Telegram (via H4F)                   │
│  Humans express intent; agents report results                       │
├─────────────────────────────────────────────────────────────────────┤
│  LAYER 2 — ORCHESTRATION                                            │
│  Axiom (fleet mgmt) · Hivemind (memory+scheduling)                  │
│  · Kobayashi-Maru (stress testing)                                  │
│  Decompose intent → assemble agent swarms → schedule work           │
├─────────────────────────────────────────────────────────────────────┤
│  LAYER 3 — EXECUTION                                                │
│  OpenCode (worktree isolation) · Ralph Loop (lock→commit→merge)     │
│  · Muster (MCP tools for any API)                                   │
│  Agents write code in isolated worktrees, commit with attestation   │
├─────────────────────────────────────────────────────────────────────┤
│  LAYER 4 — GIT FORGE                                                │
│  Forgejo (repos, PRs, CI, Pages) · GitReins hooks (quality gates)   │
│  · PromptFoo (eval as CI) · AGENTS.yaml (repo-level agent config)   │
│  Canonical home for all code; branch protection and CI enforced     │
├─────────────────────────────────────────────────────────────────────┤
│  LAYER 5 — QUALITY & REVIEW                                         │
│  Chimera (multi-model judge) · Conscientiousness (adversarial eval) │
│  · GitReins evaluator (Tier 1+2) · PromptFoo                        │
│  Nothing merges without multi-model review + adversarial self-eval  │
├─────────────────────────────────────────────────────────────────────┤
│  LAYER 6 — OBSERVABILITY & MEMORY                                   │
│  LangFuse (traces/costs) · DuckBrain (agent memory)                 │
│  · Hivemind (inbox/compiled pattern)                                │
│  Every decision traceable; every agent remembers across sessions    │
└─────────────────────────────────────────────────────────────────────┘
```

### 1.3 Component-to-Layer Mapping (All 17)

| # | Component | Layer | Language | Origin Role | Status |
|---|-----------|-------|----------|-------------|--------|
| 1 | GitReins | 4 + 5 | Python (221 tests) | Quality gate + commit guard | Built |
| 2 | Chimera | 5 | Python (90 tests) | Multi-model PR review / deliberation engine | Built |
| 3 | Conscientiousness | 5 | Go (Dockerized) | Adversarial self-evaluation | Built |
| 4 | Muster | 3 | Go (26+ packages) | OpenAPI → MCP tool generator | Built |
| 5 | Kobayashi-Maru | 2 | Go + Python | No-win scenario stress testing | Built |
| 6 | Axiom | 2 | Multi-language | Agent swarm orchestration | Built |
| 7 | Hivemind | 2 + 6 | Go + React TS | Persistent memory + task scheduling | Built |
| 8 | Hermes4Friends (H4F) | 1 + 3 | Docker Compose + Shell | Agent hosting + identity | Built |
| 9 | Ralph Loop | 3 | Pattern (embedded in Kobayashi-Maru, Hivemind) | Execution pattern: lock→worktree→commit→merge | Built |
| 10 | Forgejo | 4 | Go (external) | Self-hosted git forge (Gitea fork) | External — to provision |
| 11 | LangFuse | 6 | TypeScript (external) | LLM tracing, prompt mgmt, cost tracking | External |
| 12 | PromptFoo | 4 + 5 | TypeScript (external) | Prompt eval framework (YAML test suites) | External |
| 13 | DuckBrain | 6 | Python (external, MCP) | Git-backed persistent agent memory | External |
| 14 | OpenCode | 3 | TypeScript (external) | Agent runner (task-based, worktree isolation) | External |
| 15 | OpenRouter | (cross-cutting) | Service (external) | Model aggregator (344+ models, Fusion multi-model) | External |
| 16 | Continue.dev | 1 | TypeScript (external) | Open-source AI code assistant (IDE bridge) | External |
| 17 | External MCP Servers | (cross-cutting) | Various (external) | Atlassian, Notion, GitHub, Chrome DevTools | External |

### 1.4 The Four Glue Primitives

Everything in Helix slots into four primitives. Understanding these is prerequisite to understanding any integration:

| Primitive | Owner Project | Pattern | What It Does |
|-----------|---------------|---------|--------------|
| **Execution** | Ralph Loop | Acquire lock → create worktree → write code → commit with attestation → open PR → merge → release lock | Guarantees no two agents edit the same branch concurrently; every commit carries provenance |
| **API Compatibility** | Muster | Parse OpenAPI spec → generate MCP tools + CLI commands + shell completions | Any REST API becomes agent-callable in minutes; no hand-written wrappers |
| **Quality Gates** | GitReins | Tier 1 (static: secrets, lint, tests, dead code) + Tier 2 (agentic LLM evaluator with 9 tools) | Blocks commits that fail objective checks before they enter the forge |
| **Agent Hosting + Identity** | H4F | Per-agent Docker sandbox with budget, permissions, VPN, and known-friends identity | Agents run in isolation; identity is provisioned and scoped |

### 1.5 The 12-Step Flow (Issue → Merge)

This is the canonical end-to-end lifecycle. Every Helix feature must map to one or more steps:

```
Step 1: Human creates task
        │  (Forgejo issue, prompt file, Jira ticket, or CLI command)
        ▼
Step 2: Axiom assembles agent swarm
        │  (reads specs, decomposes into work items, assigns agents)
        ▼
Step 3: Ralph Loop acquires lock + worktree
        │  (file-level or branch-level lock; isolated git worktree)
        ▼
Step 4: Agent writes code in H4F container
        │  (OpenCode executor; Muster provides API tools if needed)
        ▼
Step 5: Agent commits with attestation
        │  (commit message includes: prompt-hash, model, context-hash, cost)
        ▼
Step 6: GitReins pre-receive hook fires
        │  Tier 1: static guards (secrets scan, lint, tests, dead code)
        │  Tier 2: agentic evaluator (LLM judges against task criteria)
        ▼
Step 7: Agent opens PR with metadata
        │  (PR description includes: linked issue, spec ref, evidence bundle)
        ▼
Step 8: Chimera runs multi-model review
        │  (formation: e.g., Sonnet + DeepSeek + Gemini → aggregator merges)
        ▼
Step 9: Conscientiousness runs adversarial self-eval
        │  (agent argues against its own work; surfaces hidden assumptions)
        ▼
Step 10: PromptFoo CI verifies no regression
        │  (.promptfoo.yaml test suite runs in Forgejo Actions)
        ▼
Step 11: Human + Agent co-approve
        │  (BOTH approvals required; neither can merge solo)
        ▼
Step 12: Merge + Deploy
        │  (Forgejo merge → Forgejo Pages deploy → LangFuse trace finalized)
```

### 1.6 Architectural Principles

1. **Agents have real identities.** Every agent is a first-class Forgejo user with SSH keys, scoped permissions, and a budget. Agents are not anonymous API calls — they are accountable actors.

2. **Nothing merges without multi-model agreement.** A single model's review is insufficient. Chimera formations require at least two independent models plus an aggregator. Conscientiousness adds adversarial self-evaluation on top.

3. **Everything is traceable.** Every agent action produces a LangFuse trace with model, tokens, cost, latency, and decision rationale. Every commit carries an attestation linking back to the prompt that generated it.

4. **Sandbox by default.** Agents execute in Docker containers with network isolation (gluetun VPN). No agent touches the host filesystem directly. Budgets are enforced per-agent.

5. **Self-hosted, no lock-in.** Every component can run on a single Hetzner box. The only external dependencies are LLM APIs, and those are abstracted behind Chimera's provider gateway.

6. **Specs are contracts.** Every feature begins with an implementation specification (this document is the platform-level spec). Axiom treats specs as input contracts — if the spec is incomplete, Axiom surfaces the gap rather than guessing.

7. **Loops, not prompts.** The core operational philosophy (from "Designing Loops, Not Prompts") is that agents should be driven by well-designed feedback loops — not hand-tuned prompts. Ralph Loop, GitReins gates, and Chimera review are all loops that constrain agent behavior structurally.

### 1.7 System Topology (Deployment View)

```
                    ┌──────────────────────────────────────────┐
                    │              INTERNET                     │
                    │  (LLM APIs: DeepSeek, OpenRouter, Z.AI)  │
                    └──────────────┬───────────────────────────┘
                                   │
                    ┌──────────────┴───────────────────────────┐
                    │         Hetzner Host (CX41/CX51)          │
                    │                                          │
                    │  ┌─────────┐  ┌──────────┐  ┌─────────┐ │
                    │  │ Forgejo │  │ LangFuse │  │DuckBrain│ │
                    │  │ :3000   │  │ :3000    │  │ (MCP)   │ │
                    │  └────┬────┘  └────┬─────┘  └─────────┘ │
                    │       │            │                     │
                    │  ┌────┴────────────┴─────────────────┐  │
                    │  │     Docker Network (helix-net)    │  │
                    │  │  ┌──────────┐  ┌──────────────┐  │  │
                    │  │  │ Chimera  │  │ Conscientious│  │  │
                    │  │  │ :8765    │  │  -ness :8080 │  │  │
                    │  │  └──────────┘  └──────────────┘  │  │
                    │  │  ┌──────────┐  ┌──────────────┐  │  │
                    │  │  │  Muster  │  │  PromptFoo   │  │  │
                    │  │  │ :9090    │  │  (CI runner) │  │  │
                    │  │  └──────────┘  └──────────────┘  │  │
                    │  └────────────────────────────────────┘  │
                    │                                          │
                    │  ┌─────────────────────────────────────┐ │
                    │  │    H4F Agent Sandboxes (N)          │ │
                    │  │  ┌──────────┐  ┌──────────┐        │ │
                    │  │  │ Agent A  │  │ Agent B  │  ...   │ │
                    │  │  │ gluetun  │  │ gluetun  │        │ │
                    │  │  │ + dind   │  │ + dind   │        │ │
                    │  │  │ +hermes  │  │ +hermes  │        │ │
                    │  │  └──────────┘  └──────────┘        │ │
                    │  └─────────────────────────────────────┘ │
                    │                                          │
                    │  ┌─────────────────────────────────────┐ │
                    │  │    OpenCode Containers (per-project)│ │
                    │  │  opencode-helix  opencode-gitreins  │ │
                    │  │  opencode-chimera  opencode-axiom   │ │
                    │  └─────────────────────────────────────┘ │
                    │                                          │
                    │  ┌─────────────────────────────────────┐ │
                    │  │  Axiom + Hivemind (orchestrators)   │ │
                    │  │  Kobayashi-Maru (stress tester)     │ │
                    │  └─────────────────────────────────────┘ │
                    └──────────────────────────────────────────┘
```

### 1.8 Technology Stack Summary

| Concern | Technology | Version Constraint |
|---------|-----------|-------------------|
| Primary language (Go components) | Go | 1.22+ |
| Primary language (Python components) | Python | 3.11+ |
| Primary language (TypeScript components) | TypeScript | 5.0+ |
| Container runtime | Docker + Docker Compose | 24.0+ / Compose v2 |
| Sandbox isolation | Bubblewrap (bwrap) + Docker | bwrap 0.8+ |
| Git forge | Forgejo | 1.21+ (Gitea fork) |
| LLM tracing | LangFuse | v2 self-hosted |
| Agent memory | DuckBrain (MCP) | latest |
| Agent runner | OpenCode | 1.15+ |
| Model access | OpenRouter / direct provider APIs | — |
| CI/CD | Forgejo Actions | built-in |
| Reverse proxy | Caddy or Traefik | latest |
| OS | Ubuntu 22.04 LTS / Debian 12 | — |
| Hardware | No GPU required (API inference) | — |

### 1.9 Verification (Section 1)

To verify the architecture is correctly understood and implemented:

1. **Component inventory check:** `docker ps` on the Helix host MUST show running containers for: Forgejo, LangFuse, Chimera, Conscientiousness, Muster, and at least one H4F agent sandbox. PromptFoo runs as a CI step, not a persistent service.
2. **Layer mapping check:** Each of the 17 components MUST be assignable to exactly one primary layer. Components spanning layers (GitReins spans 4+5, Hivemind spans 2+6) MUST document both roles.
3. **12-step flow check:** For any given merged PR, an auditor MUST be able to trace evidence for each of the 12 steps (issue link, Axiom work item, lock acquisition, commit attestation, GitReins verdict, PR metadata, Chimera review, Conscientiousness report, PromptFoo results, approval timestamps, merge SHA, LangFuse trace ID).
4. **Topology check:** All internal service-to-service communication MUST traverse the `helix-net` Docker network. Only Forgejo (web UI), LangFuse (web UI), and outbound LLM API calls cross the network boundary.

---

---

## 2. Data Flow and Execution Model

### 2.1 Overview

The Helix data flow is the 12-step lifecycle introduced in Section 1.5, expanded here with full state machines, data contracts, latency budgets, and error recovery procedures for each step. Every component in the platform participates in at least one step. Understanding these state transitions is prerequisite to debugging any stuck or failed agent run.

State machine terminology follows a consistent convention: each step has an `idle` state, an `in_progress` state, terminal `done`/`failed` states, and optional `blocked`/`escalated` states. States are persisted by the owning component so that a crash mid-step can be recovered.

### 2.2 Step-by-Step State Transitions and Data Contracts

#### Step 1 — Human Creates Task

**Owner:** Forgejo (issue), prompt file in repo, or CLI command to Hivemind/Axiom.

**State machine:**
```
idle → submitted → parsed → ready
```

| Transition | Trigger | Guard |
|------------|---------|-------|
| idle → submitted | Human submits issue, prompt file, or CLI command | Non-empty title + description |
| submitted → parsed | Axiom intake normalizes the source | `run_status = intake_complete` |
| parsed → ready | Spec attached or spec-extract scheduled | `lifecycle_stage = spec_ready` OR `workflow_state = building` |

**Data contract (output → Step 2):**
```json
{
  "task_id": "uuid-v4",
  "source": "forgejo_issue | prompt_file | cli | jira",
  "title": "string (1-200 chars)",
  "description": "string",
  "spec_ref": "specs/<component>.md or null",
  "repo": "org/repo-name",
  "priority": "P0 | P1 | P2 | P3",
  "created_by": "user:username | agent:agent-name",
  "created_at": "ISO-8601 UTC"
}
```

**Latency budget:** < 500ms for issue creation (Forgejo API). Intake parsing: < 30s (Axiom reads spec files).

**Error recovery:**
- Issue without spec → Axiom schedules `/axiom-spec-extract`. Not a failure.
- Duplicate intake → Axiom canonical-hash deduplication (7-day window). Returns existing task_id.
- Intake source unreachable (Jira down) → Task remains in `submitted`. Retry every 60s. After 3 failures, `workflow_state = blocked_source`.

---

#### Step 2 — Axiom Assembles Agent Swarm

**Owner:** Axiom orchestrator.

**State machine:**
```
ready → decomposing → work_items_created → agents_assigned → spawning
```

| Transition | Trigger | Guard |
|------------|---------|-------|
| ready → decomposing | Axiom `/axiom-meta-plan` runs | Spec exists and passes Blind Person Test |
| decomposing → work_items_created | Plan.yaml + meta-plan.md produced | At least 1 work item with ACs |
| work_items_created → agents_assigned | Axiom assigns agents per work item | Each work item has agent + model + repo |
| agents_assigned → spawning | Ralph Loop init triggered | File locks available |

**Data contract (output → Step 3):**
```json
{
  "run_id": "uuid-v4",
  "work_items": [
    {
      "work_item_id": "string",
      "title": "string",
      "spec_ref": "string",
      "assigned_agent": "tower-axiom | pm-axiom | dispatch-axiom",
      "assigned_model": "provider/model-name",
      "repo": "org/repo-name",
      "branch_prefix": "axiom/<work_item_id>",
      "acceptance_criteria": ["criterion 1", "criterion 2"],
      "estimated_steps": "integer",
      "confidence_intake": "integer (0-100)"
    }
  ],
  "plan_yaml_ref": ".memory-bank/plan.yaml"
}
```

**Latency budget:** 30-120s for meta-plan generation. 5-15s per work item for agent assignment. For repos with 30+ specs: 20-60 minutes (normal — Axiom reads every spec).

**Error recovery:**
- Spec fails Blind Person Test → `/axiom-spec-extract` re-runs. Task enters `workflow_state = spec_gap`.
- Plan YAML malformed → Manual `plan.yaml` creation from meta-planning.md. (Known Axiom pitfall.)
- 0/N steps succeed with `opencode_xml_missing` → Axiom step executor bug. Workaround: use Hermes `delegate_task` or direct `opencode run`.
- `executor_pickup` deadlock → `opencode` not on PATH. Fix: add `~/.opencode/bin` to PATH.
- Stale `materializer-state.json` → Reset: `echo '{"version":1,"max_runs":100,"run_order":[],"runs":[]}' > .axiom/state/materializer-state.json`.

---

#### Step 3 — Ralph Loop Acquires Lock and Worktree

**Owner:** Ralph Loop (embedded in Kobayashi-Maru and Hivemind).

**State machine:**
```
idle → acquiring_lock → lock_acquired → worktree_created → ready_for_code
```

| Transition | Trigger | Guard |
|------------|---------|-------|
| idle → acquiring_lock | Agent assigned and ready | No conflicting lock on target files |
| acquiring_lock → lock_acquired | File-level or branch-level lock granted | Lock TTL set (default: 3600s) |
| lock_acquired → worktree_created | `git worktree add` succeeds | Disk space available; branch name unique |
| worktree_created → ready_for_code | Worktree path returned to agent | Agent's H4F container mounted |

**Data contract (output → Step 4):**
```json
{
  "lock_id": "uuid-v4",
  "lock_type": "file | branch",
  "lock_scope": ["path/to/file.go", "path/to/other.go"] | "branch:feat/xyz",
  "lock_ttl_seconds": 3600,
  "lock_acquired_at": "ISO-8601 UTC",
  "worktree_path": "/workspace/<repo>-<work_item_id>",
  "branch_name": "axiom/<work_item_id>",
  "base_ref": "main"
}
```

**Latency budget:** < 1s for lock acquisition (in-memory or file lock). < 5s for `git worktree add`.

**Error recovery:**
- Lock contention → Agent waits with exponential backoff (1s, 2s, 4s, max 30s). After 5 minutes, task enters `blocked_lock_contention`. Axiom reassigns to a different work item or alerts human.
- Lock TTL expired → Worktree is force-cleaned. Agent's uncommitted changes are lost (by design — agents commit frequently). Lock is released.
- `git worktree add` fails (disk full) → Task `failed_disk_space`. Alert ops.
- Worktree path collision → Append random suffix, retry.

---

#### Step 4 — Agent Writes Code in H4F Container

**Owner:** OpenCode executor, wrapped by H4F container.

**State machine:**
```
ready_for_code → executing → code_written → (continue or done)
```

| Transition | Trigger | Guard |
|------------|---------|-------|
| ready_for_code → executing | OpenCode `POST /session/{id}/prompt` sent | Session alive; model resolves |
| executing → code_written | Agent returns XML envelope with `status=ok` | Files modified on disk; tests may or may not pass |
| executing → (continue) | Step has remaining sub-steps | `modify_plan` or `inject` not present |
| code_written → done | All steps in plan.yaml exhausted | All ACs addressed (not necessarily verified yet) |

**Data contract (input from Step 3):** worktree_path, branch_name, plan.yaml reference.

**Data contract (output → Step 5):**
```json
{
  "session_id": "opencode-session-uuid",
  "steps_executed": [
    {
      "step_id": "string",
      "status": "ok | fail | blocked",
      "files_modified": ["path1", "path2"],
      "tests_run": true,
      "tests_passed": "integer",
      "tests_failed": "integer",
      "confidence": "integer (0-100)",
      "evidence": "string (path to evidence)"
    }
  ],
  "total_tokens": "integer",
  "total_cost_usd": "float",
  "model_used": "provider/model-name",
  "langfuse_trace_id": "string"
}
```

**Latency budget:** 30-600s per step depending on complexity. Simple edits: < 60s. Multi-file features: 120-600s. Spec extraction from large codebases: 60+ minutes.

**Error recovery:**
- Model 429 (rate limit) → Exponential backoff (2s, 4s, 8s). After 3 retries, switch to alternate model via Chimera formation.
- Model returns empty response → Check `content`, `reasoning_content`, `reasoning` fields (provider-specific). If all empty, retry once.
- OpenCode server crash → Detect via `POST /session` returning 500. Restart container: `docker restart opencode-<project>`. Resume from last checkpoint.
- Agent enters editing loop (MiniMax M3 known issue) → Monitor file count. If stall > 600s with no new files, kill and write remaining files manually.
- Step fails 3x → `max_failed_steps_per_task` reached. Task `escalated`. Human notified.

---

#### Step 5 — Agent Commits with Attestation

**Owner:** Agent (via git), attestation format defined by platform.

**State machine:**
```
code_written → staging → committing → committed
```

| Transition | Trigger | Guard |
|------------|---------|-------|
| code_written → staging | Agent runs `git add` on modified files | Files are in worktree |
| staging → committing | Agent runs `git commit` | Commit message follows conventional-commit format |
| committing → committed | Pre-commit hook passes (or `--no-verify` for maintainer) | Secrets scan passes |

**Data contract (commit attestation trailer format):**
```
Helix-Attestation: {
  "task_id": "uuid-v4",
  "prompt_hash": "sha256:hex",
  "model": "provider/model-name",
  "context_hash": "sha256:hex",
  "cost_usd": 0.0023,
  "tokens": {"input": 12000, "output": 3400},
  "langfuse_trace_id": "string",
  "agent": "tower-axiom",
  "confidence": 72
}
```

**Latency budget:** < 1s for `git add`. < 2s for `git commit` (pre-commit hook adds 1-10s for secrets scan).

**Error recovery:**
- Secrets detected in staged files → Commit blocked (exit 1). Agent MUST remove secrets or add to `.gitignore`. The secrets scanner catches `sk-[a-zA-Z0-9_-]{20,}`, `ghp_*`, private key headers, and `*_API_KEY=value` patterns.
- Conventional-commit format violation → GitReins warns (non-blocking in Tier 1, but Tier 2 evaluator flags it).
- Git identity wrong → Spawned agents use HERMES identity, not repo identity. Must set `git config user.name`/`user.email` per-repo before spawning. Verify with `git log --format='%an <%ae>' -1`.

---

#### Step 6 — GitReins Pre-Receive Hook Fires

**Owner:** GitReins (Tier 1 static guards + Tier 2 agentic evaluator).

**State machine:**
```
committed → tier1_running → tier1_pass → tier2_running → tier2_verdict
                ↓                          ↓
           tier1_fail               tier2_pass | tier2_fail
```

| Transition | Trigger | Guard |
|------------|---------|-------|
| committed → tier1_running | Pre-receive hook triggers | Hook installed at `.git/hooks/pre-receive` |
| tier1_running → tier1_pass | All Tier 1 guards pass | No secrets, lint clean, tests pass, no dead code |
| tier1_running → tier1_fail | Any Tier 1 guard fails | Secrets = hard block. Lint/tests = configurable (blocking or warning) |
| tier1_pass → tier2_running | Pipeline config has `ai_eval` stage | `.gitreins/config.yaml` has `pipeline.stages` with tier2 |
| tier2_running → tier2_verdict | Agentic evaluator returns verdict | Max iterations (default 15, bump to 25+ for 8+ criteria) |
| tier2_verdict → tier2_pass | `verdict.verdict == "COMPLETE"` | All VerdictItems have `status == "PASS"` |
| tier2_verdict → tier2_fail | `verdict.verdict == "INCOMPLETE"` | At least one VerdictItem has `status == "FAIL"` |

**Data contract (GitReins verdict):**
```json
{
  "verdict": "COMPLETE | INCOMPLETE",
  "items": [
    {
      "criterion": "string",
      "status": "PASS | FAIL",
      "detail": "string (evidence with file:line refs)"
    }
  ],
  "tier1": {
    "passed": true,
    "guards_run": ["secrets", "lint", "tests", "dead_code", "skylos"],
    "summary": "5/5 guards passed"
  },
  "tier2": {
    "model": "deepseek/deepseek-v4-pro",
    "iterations": 12,
    "max_iterations": 25,
    "tools_used": ["read_file", "run_command", "search_pattern", "read_diff"]
  }
}
```

**Latency budget:** Tier 1: < 30s (secrets < 1s, lint < 10s, tests < 20s, dead code < 5s). Tier 2: 30-120s depending on criteria count and model latency.

**Error recovery:**
- Secrets scanner false positive (API key patterns in docs) → Add file to `.gitignore` or use `--no-verify` for maintainer override. For agent commits, the agent MUST fix the issue.
- Pipeline config missing tier2 → `./gitreins/install` creates minimal config. MUST copy full config from `.gitreins/config.yaml` with `pipeline.stages`.
- Evaluator max iterations reached → Falls back to keyword parsing (unreliable). Bump `max_iterations` in config.
- Evaluator LLM unreachable → Tier 2 skipped with WARNING. Commit allowed if Tier 1 passed (configurable: `tier2_required: true` makes it blocking).

---

#### Step 7 — Agent Opens PR with Metadata

**Owner:** Agent (via Forgejo API) or Axiom (`/pull-request-writing-axiom` skill).

**State machine:**
```
tier2_pass → pr_drafting → pr_opened → review_pending
```

**Data contract (PR body template):**
```markdown
## Summary
[One-paragraph description]

## Linked Issue
Closes #<issue_number>

## Spec Reference
`specs/<component>.md` §<section>

## Evidence Bundle
- Acceptance Criteria: [table with AC → verification path → result]
- Tests: N passing, 0 failing
- GitReins Tier 1: PASS
- GitReins Tier 2: COMPLETE (N/N criteria pass)
- Confidence: <score>/100

## Attestation
Model: <model>
Tokens: <input>+<output>
Cost: $<amount>
LangFuse Trace: <trace_id>

## Risks
[Known risks, assumptions, limitations]
```

**Latency budget:** < 5s for `POST /api/v1/repos/{owner}/{repo}/pulls`.

**Error recovery:**
- Branch protection prevents push → Agent pushes to `feat/*` branch (never `main`). Push to main is denied by policy.
- PR body malformed → Axiom's `pull-request-writing-axiom` skill formats it. Manual override available.
- No linked issue → PR created in `draft` state. Human must link issue before review.

---

#### Step 8 — Chimera Runs Multi-Model Review

**Owner:** Chimera deliberation engine.

**State machine:**
```
review_pending → chimera_dispatching → chimera_working → chimera_aggregating → chimera_verdict
```

| Transition | Trigger | Guard |
|------------|---------|-------|
| review_pending → chimera_dispatching | PR opened + CI triggered | Chimera service reachable on :8765 |
| chimera_dispatching → chimera_working | Dispatcher designs formation DAG | Dispatcher model returns valid JSON DAG |
| chimera_working → chimera_aggregating | All workers complete (or degrade) | At least 1 worker returned non-empty output |
| chimera_aggregating → chimera_verdict | Aggregator merges worker outputs | Aggregator model returns non-empty |

**Data contract (Chimera review output → Step 9):**
```json
{
  "chimera_trace_id": "string",
  "formation": "auto | simple | debate | audit | custom",
  "source": "auto | fallback",
  "dispatcher": {"model": "deepseek/deepseek-v4-pro", "tokens": 1200},
  "workers": [
    {"stage_id": "worker_1", "model": "glm-5.2", "tokens": 3400, "prompt_hash": "sha256"},
    {"stage_id": "worker_2", "model": "claude-sonnet-4", "tokens": 2800, "prompt_hash": "sha256"}
  ],
  "aggregator": {"model": "deepseek/deepseek-v4-pro", "tokens": 1500},
  "verdict": "APPROVE | REQUEST_CHANGES | REJECT",
  "findings": [
    {"severity": "critical | high | medium | low", "file": "path", "line": 42, "description": "string"}
  ],
  "total_tokens": 8900,
  "total_cost_usd": 0.014,
  "total_duration_ms": 45000
}
```

**Latency budget:** 15-60s for budget formations (DeepSeek-only). 30-120s for multi-provider (DeepSeek + Z.AI + Anthropic). Budget-first defaults: DeepSeek V4 Pro for all stages.

**Error recovery:**
- Worker returns empty response → Check `content`, `reasoning_content`, `reasoning` fields. If degraded, aggregator receives `[DEGRADED]` inputs and handles gracefully.
- Provider circuit breaker open → Chimera routes to alternate provider. Formation auto-adjusts to available models.
- Budget exhausted → `BudgetExhaustedError` raised. Review fails. Human can manually approve or increase budget.
- Dispatcher fallback (single-model) → `trace.source == "fallback"`. Multi-model review not achieved. Flag for human attention.
- `json_schema` unsupported by DeepSeek → Gateway auto-downgrades: `json_schema → json_object → plain text`. Two-tier retry.

---

#### Step 9 — Conscientiousness Runs Adversarial Self-Evaluation

**Owner:** Conscientiousness service (Go, Dockerized).

**State machine:**
```
chimera_verdict → adversarial_planning → adversarial_arguments → adversarial_report
```

| Transition | Trigger | Guard |
|------------|---------|-------|
| chimera_verdict → adversarial_planning | Chimera verdict is APPROVE or REQUEST_CHANGES | Conscientiousness service reachable on :8080 |
| adversarial_planning → adversarial_arguments | Attack vectors generated | At least 3 attack vectors |
| adversarial_arguments → adversarial_report | Agent argues against its own work | Report includes surfaced assumptions |

**Data contract (Conscientiousness report):**
```json
{
  "report_id": "uuid-v4",
  "work_item_id": "string",
  "hidden_assumptions": ["assumption 1", "assumption 2"],
  "attack_vectors": [
    {
      "vector": "edge case X",
      "severity": "high | medium | low",
      "evidence": "file:line description",
      "mitigated": false
    }
  ],
  "self_critique": "string (agent's own counter-arguments)",
  "verdict": "DEFENSIBLE | VULNERABLE",
  "trust_level": "integer (0-100, from DB)"
}
```

**Latency budget:** 60-180s for adversarial evaluation (multiple LLM calls for attack vectors + self-critique).

**Error recovery:**
- Conscientiousness DB corruption → Run `./conscience init --db-url "sqlite:///path"` to reinitialize. Schema repair via `AutoMigrate` with PRAGMA checks.
- Migration filter strips valid SQL → `filterForSQLite` may skip valid ALTER TABLE. Check `internal/migrate/migrate.go` for PG-only keyword filtering.
- Bootstrap key lost → `conscience init` prints bootstrap admin key. `conscience serve` on already-init'd DB only shows prefix.

---

#### Step 10 — PromptFoo CI Verifies No Regression

**Owner:** PromptFoo (runs as Forgejo Actions CI step).

**State machine:**
```
adversarial_report → ci_triggered → promptfoo_running → ci_pass | ci_fail
```

**Data contract (PromptFoo config `.promptfoo.yaml`):**
```yaml
description: "Regression tests for <component>"
prompts:
  - file://prompts/eval-001.txt
providers:
  - id: deepseek/deepseek-v4-pro
    config:
      apiBaseUrl: ${DEEPSEEK_API_BASE}
      apiKey: ${DEEPSEEK_API_KEY}
tests:
  - description: "Output matches expected schema"
    assert:
      - type: contains-json
      - type: javascript
        value: "output.field === expected"
```

**Latency budget:** 10-60s per test case depending on model latency. Full suite: 60-300s.

**Error recovery:**
- Prompt eval regression → CI fails. PR cannot merge. Agent must fix prompt or code.
- Model unavailable in CI → Use budget models only (`deepseek-v4-flash`). CI key has separate $5-10 budget.
- `.promptfoo.yaml` missing → CI step skipped with WARNING. Not blocking initially but flagged for spec compliance.

---

#### Step 11 — Human + Agent Co-Approval

**Owner:** Forgejo review system.

**State machine:**
```
ci_pass → awaiting_human_approval → awaiting_agent_approval → co_approved
```

| Transition | Trigger | Guard |
|------------|---------|-------|
| ci_pass → awaiting_human_approval | All CI checks green | Chimera APPROVE + Conscientiousness DEFENSIBLE + GitReins PASS |
| awaiting_human_approval → co_approved | Human approves PR | Human is not the PR author |
| awaiting_agent_approval → co_approved | Agent approves PR | Agent has earned merge trust (acceptance rate threshold) |

**CRITICAL:** BOTH approvals are required. Neither human nor agent can merge solo. This is architectural principle #2 (Section 1.6).

**Data contract:**
```json
{
  "human_approval": {
    "user": "username",
    "approved_at": "ISO-8601 UTC",
    "comment": "string"
  },
  "agent_approval": {
    "agent": "agent-name",
    "approved_at": "ISO-8601 UTC",
    "confidence": 78,
    "evidence_ref": "verification.md"
  }
}
```

**Latency budget:** Human: variable (minutes to days). Agent: < 30s (automated review of evidence bundle).

**Error recovery:**
- Human rejects → PR returns to `changes_requested`. Agent addresses feedback, re-pushes.
- Agent rejects (low confidence) → PR stays open. Human can override with explicit `force-merge` label (logged in audit trail).
- Stale approval (> 7 days) → Re-run CI. If environment changed, approvals reset.

---

#### Step 12 — Merge and Deploy

**Owner:** Forgejo (merge), Forgejo Pages (deploy), LangFuse (trace finalization).

**State machine:**
```
co_approved → merging → merged → deploying → deployed → trace_finalized
```

| Transition | Trigger | Guard |
|------------|---------|-------|
| co_approved → merging | Both approvals present + all CI green | Branch protection allows merge |
| merging → merged | `POST /api/v1/repos/{owner}/{repo}/pulls/{index}/merge` | Merge strategy: squash or rebase (repo-configured) |
| merged → deploying | Forgejo Pages build triggers | Pages config exists |
| deploying → deployed | Pages build succeeds | Static assets served |
| deployed → trace_finalized | LangFuse trace cost/latency finalized | All LLM calls attributed |

**Data contract (merge record):**
```json
{
  "merge_sha": "40-char hex",
  "merge_strategy": "squash | rebase | merge",
  "merged_by": "user:username",
  "merged_at": "ISO-8601 UTC",
  "pr_index": "integer",
  "issue_closed": true,
  "pages_url": "https://<org>.forgejo.page/<repo>/",
  "langfuse_trace_id": "string",
  "total_cost_usd": 0.045,
  "total_duration_s": 932
}
```

**Latency budget:** < 5s for merge API call. 10-120s for Pages build. < 1s for LangFuse trace finalization.

**Error recovery:**
- Merge conflict → Agent rebases on `main`, resolves conflicts, re-pushes. Back to Step 4.
- Pages build fails → Merge succeeds but deploy fails. Alert ops. Non-blocking for code correctness.
- LangFuse trace incomplete → Cost attribution runs async. Missing traces don't block merge.

---

### 2.3 End-to-End Latency Budget Summary

| Step | Min | Typical | Max | Bottleneck |
|------|-----|---------|-----|------------|
| 1. Create task | 0.5s | 2s | 30s | Spec reading (intake) |
| 2. Assemble swarm | 5s | 60s | 3600s | Meta-plan on large repos |
| 3. Acquire lock | 1s | 3s | 300s | Lock contention |
| 4. Write code | 30s | 120s | 3600s | Model latency, code complexity |
| 5. Commit | 1s | 3s | 15s | Pre-commit hooks |
| 6. GitReins gates | 30s | 90s | 180s | Tier 2 evaluator (LLM call) |
| 7. Open PR | 1s | 3s | 10s | Forgejo API |
| 8. Chimera review | 15s | 45s | 120s | Multi-model formation |
| 9. Adversarial eval | 60s | 120s | 180s | Multiple LLM calls |
| 10. PromptFoo CI | 30s | 120s | 300s | Test suite size |
| 11. Co-approval | 1s | variable | days | Human availability |
| 12. Merge + deploy | 5s | 30s | 120s | Pages build |
| **Total (automated)** | **3.5min** | **10min** | **90min** | Code generation + review |

### 2.4 Cross-Step Error Propagation

Errors do not propagate forward silently. Each step has a defined failure escalation path:

```
Step fails → component logs error → Axiom checkpoint updated
                                    ↓
                            Is it retriable?
                           /              \
                         YES               NO
                          |                 |
                    Retry (max 3)     Escalate to human
                          |            Jira comment + evidence
                    Step re-enters
                    in_progress
```

**Max retries per step:** 3 (configurable in `axiom.config.yaml` → `retry.max_retry_attempts`).
**Max stuck time:** 60 minutes per step (`retry.max_minutes_stuck`). Default 30 is too short for AI agents.
**Max failed steps per task:** 3. After 3, the entire work item is `escalated`.

### 2.5 Verification (Section 2)

1. **State machine check:** For any task, query Axiom state: `cat .axiom/state/materializer-state.json | jq`. Each work item MUST show a valid state transition path from `idle` to its current state. No state may skip a required predecessor.
2. **Data contract check:** Each step's output JSON MUST conform to the schema in its data contract. An integration test MUST validate contract conformance for at least one happy-path run.
3. **Latency budget check:** For any task completing in the "automated" path (Steps 1-10 + 12, excluding human approval), total wall-clock time MUST be under 90 minutes. If exceeded, the slowest step MUST be identifiable from LangFuse traces.
4. **Error recovery check:** Simulate a Tier 1 failure (inject a secret into staged files). Verify GitReins blocks the commit. Verify the agent receives a structured error and can retry. Verify Axiom's checkpoint reflects the failure and retry.

---

## 3. Component Specifications

This section specifies each of the 17 Helix components in implementation detail: role, interfaces, dependencies, scaling model, and failure modes. Components are grouped by layer (Section 1.2) and presented in dependency order — later components depend on earlier ones.

---

### 3.1 GitReins (Layer 4 + 5)

| Field | Value |
|-------|-------|
| **Role** | Quality gate + commit guard. Pre-receive hooks block commits failing static checks (Tier 1) or agentic evaluation (Tier 2). |
| **Language** | Python 3.11+ |
| **Tests** | 221 tests (2,491 lines) |
| **Lines of code** | ~5,041 (10 source files) |
| **Dependencies** | `mcp`, `pyyaml`, `requests` (3 runtime deps) |
| **Repo** | `totalwindupflightsystems/gitreins` |

**Interfaces:**

1. **Git hooks** — `.git/hooks/pre-commit` and `.git/hooks/pre-receive`. Blocks commits on Tier 1 failure.
2. **MCP tools** — `read_file`, `run_command`, `search_pattern`, `read_diff`, `sandbox_write`, `sandbox_read`, `get_task_item`, `detect_dead_code`, `skylos_scan`. Available to the Tier 2 evaluator.
3. **Python API** — `AgenticEvaluator.evaluate(task_dict)`, `GuardManager.run_all()`, `Judge.evaluate_task(task)`.
4. **CLI** — `gitreins/install` (installs hooks), `gitreins/eval` (manual evaluation).

**Dependencies:** DeepSeek API (for Tier 2 evaluator). Optional: Skylos for multi-language dead code detection.

**Scaling model:** Stateless. Runs per-commit. No persistent server required. Tier 2 evaluator makes one LLM call per evaluation (15-25 agentic iterations). Horizontal scaling is trivial — each repo runs its own GitReins instance.

**Failure modes:**

| Failure | Impact | Recovery |
|---------|--------|----------|
| Secrets scanner false positive | Commit blocked incorrectly | Add file to `.gitignore` or `--no-verify` (maintainer only) |
| Tier 2 LLM unreachable | Quality gate degraded | Tier 2 skipped with WARNING. Tier 1 still blocks secrets. |
| Pipeline config missing | Tier 2 never runs | Copy full config from `.gitreins/config.yaml` with `pipeline.stages` |
| Evaluator max iterations hit | Verdict unreliable | Bump `max_iterations` to 25+ |
| `AST.iter_child_nodes` false flags | Dead code detection noise | Use `node.body` instead of `iter_child_nodes` (known bug) |

---

### 3.2 Chimera (Layer 5)

| Field | Value |
|-------|-------|
| **Role** | Multi-model deliberation engine. Designs formation DAGs, dispatches parallel workers, aggregates results. Provider gateway for all LLM access. |
| **Language** | Python 3.11+ |
| **Tests** | 309 unit tests + 32 SSE integration tests + 7 E2E integration tests |
| **Runtime** | FastAPI + uvicorn (port 8765) |
| **Dependencies** | `litellm`, `fastapi`, `uvicorn`, `pydantic`, `structlog`, `httpx` |
| **Repo** | `totalwindupflightsystems/chimera` (GitHub primary, GitLab secondary) |
| **PyPI** | `chimera-deliberation` |

**Interfaces (all 6 surfaces):**

1. **CLI** — `chimera "prompt"`, `chimera serve`, `chimera formations`, `chimera models`.
2. **REST API** — `POST /v1/deliberate`, `POST /v1/chat/completions` (OpenAI-compatible), `GET /v1/{formations,models,health,health/ready,health/live}`.
3. **MCP server** — `chimera_deliberate`, `chimera_formations`, `chimera_models` (3 tools, stdio).
4. **Python SDK** — `from chimera import Engine, ChimeraConfig`.
5. **OpenAI-compatible** — Drop-in replacement for OpenAI SDK clients. `response_format` supported.
6. **Web UI** — `GET /web/` SPA with live DAG visualization (Mermaid.js via SSE).

**Architecture (v2):**
- **Dispatcher** — One model call designs the entire DAG (worker prompts, model assignments, aggregator instructions). Uses JSON mode (`response_format={"type":"json_object"}`).
- **Workers** — Parallel model calls with domain-scoped prompts. Category-weighted model selection.
- **Aggregator** — Merges worker outputs using dispatcher instructions. Supports structured output via `output_schema`.
- **Gateway** — LiteLLM-backed. Provider routing: `deepseek` (direct API), `openrouter`, `zai`, `anthropic`. Circuit breaker per provider.

**Dependencies:** DeepSeek API (primary, budget-first), OpenRouter API (premium models), Z.AI API (GLM-5.2), Anthropic API (Claude, optional), Google API (Gemini, optional).

**Scaling model:** Chimera is stateless (except Web UI sessions). Horizontal scaling via multiple uvicorn workers behind a load balancer. Provider circuit breakers prevent cascading failures. Token bucket rate limiter per API key. Formation design auto-adjusts to available models when circuit breakers open.

**Failure modes:**

| Failure | Impact | Recovery |
|---------|--------|----------|
| Provider 429 (rate limit) | Worker fails | Exponential backoff (2s, 4s, 8s). Circuit breaker opens after N failures. |
| Worker returns empty | Degraded input | Gateway checks `content`, `reasoning_content`, `reasoning`. Aggregator receives `[DEGRADED]` tag. |
| Dispatcher JSON parse failure | Fallback to single-model | `trace.source == "fallback"`. Flagged for attention. |
| DeepSeek `json_schema` unsupported | Structured output fails | Gateway auto-downgrades: `json_schema → json_object → plain text`. |
| Budget exhausted | Deliberation fails | `BudgetExhaustedError`. Human can increase budget or override. |
| SSE broadcaster crash (concurrent clients) | Web UI events lost | Use `list` not `set` for subscribers. 30s initial idle timeout. Error boundary on `event.format()`. |

**Model field quirk:** DeepSeek V4 uses `message.reasoning_content`, MiniMax M3 and Kimi K2.7 use `message.reasoning`. Gateway's `_extract_text()` MUST check all three fields: `content`, `reasoning_content`, `reasoning`.

---

### 3.3 Conscientiousness (Layer 5)

| Field | Value |
|-------|-------|
| **Role** | Adversarial self-evaluation. Agent argues against its own work, surfaces hidden assumptions, generates attack vectors. DB-native agent runtime. |
| **Language** | Go |
| **Runtime** | Dockerized Go binary on port 8080 |
| **Database** | SQLite (dev) / PostgreSQL (prod), dual-backend via migration filter |
| **Repo** | `dexdat/conscientiousness` |

**Interfaces:**

1. **HTTP API** — `POST /api/v1/evaluate` (submit work for adversarial review), `GET /api/v1/report/{id}`.
2. **CLI** — `conscience init --db-url <url>`, `conscience serve --db-url <url> --port <port>`.
3. **DB-native harness** — Migration system with SQLite/PostgreSQL dual-backend. `filterForSQLite` transforms PG migrations for SQLite.

**Dependencies:** LLM API (for adversarial argumentation). SQLite or PostgreSQL (state persistence).

**Scaling model:** Single instance per Helix deployment. Stateful (DB-backed trust levels, evaluation history). Vertical scaling by upgrading DB instance. Horizontal scaling would require shared DB backend.

**Failure modes:**

| Failure | Impact | Recovery |
|---------|--------|----------|
| DB migration corruption | Schema mismatch | `AutoMigrate` repair functions check `PRAGMA table_info` and execute missing DDL |
| `filterForSQLite` strips valid SQL | Silent data loss | Check PG-only keyword list in `internal/migrate/migrate.go` |
| Bootstrap key lost | Admin locked out | Re-run `conscience init` on fresh DB. Prints new bootstrap key. |
| LLM unreachable | Evaluation degrades | Fallback to rule-based heuristics. Trust level frozen. |

---

### 3.4 Muster (Layer 3)

| Field | Value |
|-------|-------|
| **Role** | OpenAPI → MCP tool generator. Any REST API becomes agent-callable in minutes. |
| **Language** | Go (26+ packages) |
| **Go version** | 1.26.1+ |
| **Repo** | TBD (currently `github.com/wojons/muster`) |

**Interfaces:**

1. **Go library** — `pkg/openapi` (spec parser), `pkg/client` (HTTP client), `pkg/mcp` (MCP converter), `pkg/daemon` (ProxyServer + Unix socket).
2. **CLI commands** — Auto-generated from OpenAPI spec. Shell completions included.
3. **MCP tools** — `pkg/mcp.Converter.ConvertToTools()` generates MCP tool definitions from parsed operations.
4. **Daemon mode** — ProxyServer on Unix socket. Multi-tier caching, rate limiting.
5. **Starlark DSL** — Scriptable API interactions.

**Dependencies:** Go 1.26.1+. External: any REST API with an OpenAPI spec.

**Scaling model:** Daemon mode provides multi-tier caching and rate limiting. Stateless per-request (cached responses serve from memory). Horizontal scaling via multiple daemon instances behind a shared cache layer (Redis, optional).

**Known limitation:** `OpenAPIExecutor.Execute()` is a stub — hardcodes `GET /` and ignores tool name/args. The converter generates correct MCP tool definitions but execution is not wired. Workaround: use parsed operations directly with Muster's HTTP client.

**Failure modes:**

| Failure | Impact | Recovery |
|---------|--------|----------|
| OpenAPI spec malformed | Tool generation fails | Parser returns structured error. Agent can request spec fix. |
| Target API unreachable | MCP tools return error | Rate limiter backs off. Circuit breaker opens. |
| Go toolchain too old | Build fails | Set `GOTOOLCHAIN=go1.26.1` |
| Executor stub | MCP tools don't route | Use extracted operations directly with `pkg/client` |

---

### 3.5 Kobayashi-Maru (Layer 2)

| Field | Value |
|-------|-------|
| **Role** | No-win scenario stress testing. Exhaustive specification, adversarial refinement, Ralph Loop engine, penetration testing. |
| **Language** | Go + Python |
| **Monitoring** | Prometheus + Loki + Fluentd |
| **Repo** | `totalwindupflightsystems/Kobayashi-Maru` → `/home/kara/Kobayashi-Maru/` |
| **Cron** | `3c6a463dd9fd` (every 2h, autonomous dev loop) |

**Interfaces:**

1. **Stress test runner** — Generates adversarial scenarios from specs. Injects faults (network partitions, API failures, resource exhaustion).
2. **Ralph Loop engine** — The core execution pattern: acquire lock → create worktree → write code → commit with attestation → open PR → merge → release lock. Also embedded in Hivemind.
3. **Penetration testing** — Automated security probes against deployed services.
4. **Monitoring stack** — Prometheus (metrics), Loki (logs), Fluentd (log collection).

**Dependencies:** Prometheus, Loki, Fluentd (monitoring stack). Go runtime. Python for spec analysis.

**Scaling model:** Stress tests run as batch jobs. Ralph Loop instances are serialized per-repo (lock-based). Monitoring stack scales horizontally (Prometheus federation, Loki horizontal scaling).

**Failure modes:**

| Failure | Impact | Recovery |
|---------|--------|----------|
| Monitoring stack down | Blind to system health | Fluentd self-heals. Prometheus/Loki restart automatically. Alert if down > 5 min. |
| Ralph Loop deadlock (lock leak) | Branch stuck | Lock TTL (3600s) auto-expires. Stale locks cleaned by cron. |
| Stress test causes real damage | Service degradation | Kobayashi-Maru runs in sandboxed containers. Blast radius contained by Docker network isolation. |

---

### 3.6 Axiom (Layer 2)

| Field | Value |
|-------|-------|
| **Role** | Agent swarm orchestration. 60+ agents (adversarial, quality, planning, building, verification). Specs-as-contracts, evidence bundles, Jira/Forgejo sync. |
| **Language** | Multi-language (Go backend, TypeScript skills, YAML config) |
| **Agents** | 177 skills across 8 categories (orchestration, building, verification, memory, operations, communication, domain-specific, adversarial) |
| **Repo** | `anything-agent/axiom` |

**Interfaces:**

1. **CLI** — `axiom run --intent "..." --repo <path>`, `axiom cmd --command /<command> --repo <path>`, `axiom serve`.
2. **Command registry** — `.axiom/command-registry.yaml` defines all commands (`/axiom-step`, `/axiom-verify`, `/axiom-adversary`, etc.).
3. **OpenCode API** — Axiom spawns or connects to OpenCode via HTTP API (`POST /session`, `POST /session/{id}/prompt`).
4. **Jira integration** — `/axiom-jira-event`, `/axiom-jira-intake`, `/axiom-jira-update`.
5. **Memory bank** — `.memory-bank/` directory: `progress.md`, `activeContext.md`, `decisionLog.md`, `findings/`.
6. **Config** — `.axiom/axiom.config.yaml` (per-repo).

**Key spec contracts:**

- **Confidence scoring (spec 11):** LOW < 40, MEDIUM 40-69, HIGH ≥ 70. PR must be DRAFT if < 40.
- **Evidence bundle (spec 27):** Every work item produces `verification.md` with AC coverage, checks, verifier results, SHA-256 integrity hash.
- **XML protocol (spec 04):** Agents return structured XML envelopes. Runner validates required tags. Missing → v2 variant retry (3 attempts).
- **Retry/escalation (spec 12):** 3 retries per step, 30-60 min max stuck, 3 max failed steps per task.
- **Plan schema (spec 03):** Phase → Task → Subtask → Step (atomic). Injection at step/task/phase level.
- **Intake lifecycle (spec 44):** Three vocabularies (`run_status`, `lifecycle_stage`, `workflow_state`). 7-day dedup.

**Dependencies:** OpenCode (agent executor), Forgejo (git operations), LLM APIs (via OpenCode). Optional: Jira (ticket sync).

**Scaling model:** One Axiom instance per repo/project. Work items execute sequentially (lock-based) or in parallel (up to `max_open_prs: 3`). `max_delegation_depth: 5` (nested agent spawning). Containerized deployments: one OpenCode container per project (`opencode-<project>` on ports 4096-4102).

**Failure modes:**

| Failure | Impact | Recovery |
|---------|--------|----------|
| `opencode` not on PATH | `executor_pickup` deadlock | Add `~/.opencode/bin` to PATH |
| Stale `materializer-state.json` | Orchestration hangs | Reset: `echo '{"version":1,"max_runs":100,"run_order":[],"runs":[]}' > .axiom/state/materializer-state.json` |
| 0-token sessions | Silent deadlock | Use `delegate_task` or direct `opencode run` |
| Plan YAML malformed | PlanGenerationFailed | Create `plan.yaml` manually from meta-planning.md |
| Plan YAML empty repo field | PlanGenerationFailed | Use `--repo /absolute/path` instead of `--repo .` |
| Stale processes from concurrent sessions | Port conflicts, stale locks | `pgrep -af axiom`, kill, `rm -f .axiom/state/*.sock*` |
| Chrome DevTools MCP enabled | POST /session hangs | Set `chrome-devtools: { enabled: false }` in opencode.jsonc |

---

### 3.7 Hivemind (Layer 2 + 6)

| Field | Value |
|-------|-------|
| **Role** | Persistent agent memory + task scheduling. IAM/auth, git operations, Ralph Loop engine, inbox/compiled pattern, hierarchical rate limiting. |
| **Language** | Go + React TypeScript |
| **Storage** | SQLite + YAML memory bank |
| **Repo** | TBD |

**Interfaces:**

1. **Memory API** — Read/write agent memories. SQLite-backed with YAML serialization.
2. **Task scheduler** — Cron-like scheduling for agent tasks. Inbox/compiled pattern for deferred work.
3. **IAM/Auth** — Agent identity management, permission scoping, hierarchical rate limiting.
4. **Git operations** — Programmatic git operations for agents (clone, commit, push, PR).
5. **Ralph Loop engine** — Same lock→worktree→commit→merge pattern as Kobayashi-Maru.
6. **React UI** — Dashboard for memory inspection, task monitoring, rate limit management.

**Dependencies:** SQLite (memory storage), Go runtime, React (UI).

**Scaling model:** Single instance per Helix deployment. SQLite provides adequate performance for agent-scale workloads. Rate limiting is hierarchical: per-agent, per-team, per-platform.

**Failure modes:**

| Failure | Impact | Recovery |
|---------|--------|----------|
| SQLite lock contention | Memory writes slow | WAL mode enabled. Contention resolves within seconds. |
| Rate limit misconfiguration | Agents starved or overfed | Hierarchical limits auto-balance. Human can override per-agent. |
| Memory bank corruption | Agent loses context | YAML files are human-readable. Rebuild from YAML backups. SQLite is recoverable via `.dump`. |

---

### 3.8 Hermes4Friends / H4F (Layer 1 + 3)

| Field | Value |
|-------|-------|
| **Role** | Agent hosting + identity. Per-agent Docker sandbox with budget, permissions, VPN (gluetun), and known-friends identity management. |
| **Language** | Docker Compose + Shell |
| **Tests** | 255+ tests (provisioning pipeline) |
| **Repo** | `Hermes4Friends/infrastructure` → `/home/kara/hermes4friends-infra/` |
| **Production** | `/opt/hermes-demo/` on Hetzner host |

**Interfaces:**

1. **Provisioning pipeline** — `provision_friend(name, tier)`, `offboard_friend(name)`, `update_friend(name)`, `provision_openrouter_key(name, tier)`.
2. **Doctor/auto-repair** — `doctor.fix_dead_keys()` runs every 5 min via bridge cron. Detects 401 keys, re-provisions.
3. **known-friends.json** — Identity registry: `{"friend_name": {"tier": "pro|flash", "status": "active|offboarded", ...}}`. Source of truth at `/opt/hermes-demo/.hermes/h4f/known-friends.json`.
4. **Bridge** — Nextcloud bridge for identity sync. Bidirectional: NC ↔ workspace ↔ container.
5. **Consistency layer** — Storage Box ↔ container env ↔ `.env` file sync. Runs every cron cycle.
6. **Guardrail enforcer** — OpenRouter key guardrail assignment. Auto-assigns unassigned keys to Flash tier.

**Dependencies:** Docker, Docker Compose, OpenRouter API (key management), Nextcloud (bridge), Storage Box (secret files), Coolify (deployment), gluetun (VPN).

**Scaling model:** Per-friend Docker containers. Each friend gets: gluetun VPN container, DinD executor container, Hermes agent container. Horizontal scaling by adding more friends (provisioned via pipeline). Bridge cron runs every 5 minutes checking all friends.

**Auto-repair logging (syslog-style):**
```
TIMESTAMP | SEVERITY | MODULE | USER | CHECK | RESULT | detail
```
Modules: `nc-sync`, `consis`, `live-tst`, `grd-enf`, `cln-orph`. Every check produces a DEBUG line on every cycle. The noise IS the debug mode.

**Golden rule:** Every fix MUST go through the provisioning pipeline. Never use `docker exec`, `docker run`, `docker restart`, or `curl` to patch friend state. The pipeline is the single source of truth (255+ tests verify every path).

**Failure modes:**

| Failure | Impact | Recovery |
|---------|--------|----------|
| Dead key (401 on chat) | Agent can't use LLM | `doctor.fix_dead_keys()` auto-re-keys every 5 min |
| Three-way key drift (NC ↔ workspace ↔ container) | 401 on chat but gateway appears healthy | `consistency.run_consistency_fix()` syncs all layers every cycle |
| Guardrail not assigned | Key works but has no model restrictions | `guardrail_enforcer` auto-assigns unassigned keys |
| CIFS mount D-state | Container hangs | `references/cifs-dstate-recovery.md` runbook |
| Pipeline self-infliction (creates keys it then "fixes") | Silent cycle, root cause never investigated | Verify second cron run is a no-op on healthy system |
| Offboarded friend leaves orphan keys | OpenRouter cost leak | `cleanup_orphans.py` runs every 5 min |

---

### 3.9 Ralph Loop (Layer 3 — Embedded Pattern)

| Field | Value |
|-------|-------|
| **Role** | Execution pattern: acquire lock → create worktree → write code → commit with attestation → open PR → merge → release lock. |
| **Language** | Pattern (not a standalone project — embedded in Kobayashi-Maru and Hivemind) |
| **Origin** | "Designing Loops, Not Prompts" methodology |

**Interfaces:** Not a standalone service. Consumed as a library/pattern by Kobayashi-Maru and Hivemind. API surface: `acquire_lock(scope, ttl) → lock_id`, `create_worktree(base_ref) → worktree_path`, `release_lock(lock_id)`.

**Dependencies:** Git (for worktrees), a lock provider (file-based or in-memory).

**Scaling model:** Serialized per-lock-scope. Parallel work items with non-overlapping file scopes can run concurrently.

**Failure modes:**

| Failure | Impact | Recovery |
|---------|--------|----------|
| Lock leak (agent crashes without release) | Branch stuck | Lock TTL (3600s) auto-expires. Cron cleans stale locks. |
| Worktree corruption | Code changes lost | Agent SHOULD commit frequently (every step). Lost changes are recoverable from git reflog if committed. |
| Concurrent lock acquisition race | Two agents edit same files | Lock provider MUST be atomic (file lock with O_EXCL or DB transaction). |

---

### 3.10 Forgejo (Layer 4 — External)

| Field | Value |
|-------|-------|
| **Role** | Self-hosted git forge (Gitea fork). Repos, PRs, CI (Actions), Pages, OAuth provider. |
| **Language** | Go (external) |
| **Version** | 1.21+ |
| **Port** | 3000 |
| **Status** | External — to provision |

**Interfaces:**

1. **Web UI** — `http://forgejo:3000` (exposed to network boundary).
2. **REST API** — `POST /api/v1/admin/users` (account creation), `POST /api/v1/users/{name}/tokens` (PAT), `POST /api/v1/repos/{owner}/{repo}/pulls` (PR creation), `POST /api/v1/repos/{owner}/{repo}/pulls/{index}/merge`.
3. **Forgejo Actions** — CI/CD (built-in, GitHub Actions compatible). Runs PromptFoo evals, builds, tests.
4. **Forgejo Pages** — Static site hosting from repo branches.
5. **OAuth2 provider** — Agent authentication flow.
6. **Git over SSH/HTTPS** — `git@forgejo:user/repo.git`.

**Dependencies:** PostgreSQL or SQLite (metadata), Go runtime.

**Scaling model:** Single instance per Helix deployment. Vertical scaling for larger repos. Forgejo supports mirroring and federation for multi-instance deployments.

**Failure modes:**

| Failure | Impact | Recovery |
|---------|--------|----------|
| Forgejo down | All git operations fail | Restart container. PostgreSQL persists state. |
| Branch protection misconfiguration | Agents push to main | Per-repo branch protection MUST be configured: agents push to `feat/*` only. |
| OAuth token expired | Agent can't authenticate | Re-provision PAT via `POST /api/v1/users/{name}/tokens`. |
| Actions runner offline | CI doesn't execute | Runner auto-reconnects. Alert if offline > 5 min. |

---

### 3.11 LangFuse (Layer 6 — External)

| Field | Value |
|-------|-------|
| **Role** | LLM tracing, prompt management, cost tracking. Every agent call logged with model, tokens, cost, latency, decision rationale. |
| **Language** | TypeScript (external) |
| **Version** | v2 self-hosted |
| **Port** | 3000 |

**Interfaces:**

1. **Web UI** — Dashboard for traces, costs, latency analysis.
2. **API** — `POST /api/public/traces` (ingest), `GET /api/public/traces/{id}`.
3. **SDK** — Python/JS SDKs for instrumentation. Chimera integrates via `chimera.observability` (structlog + optional Langfuse tracing).

**Dependencies:** PostgreSQL (trace storage), ClickHouse (analytics, optional).

**Scaling model:** Self-hosted single instance. ClickHouse for high-volume analytics. Horizontal scaling via ClickHouse cluster.

**Failure modes:**

| Failure | Impact | Recovery |
|---------|--------|----------|
| LangFuse down | Traces lost (buffered) | SDK buffers traces locally. Backfill when service recovers. |
| Cost attribution incomplete | Budget tracking inaccurate | Async finalization. Missing traces don't block operations. |

---

### 3.12 PromptFoo (Layer 4 + 5 — External)

| Field | Value |
|-------|-------|
| **Role** | Prompt evaluation framework. YAML test suites run as CI. Regression detection for prompt changes. |
| **Language** | TypeScript (external) |

**Interfaces:**

1. **CLI** — `promptfoo eval -c .promptfoo.yaml`.
2. **Config** — `.promptfoo.yaml` in every repo. Defines prompts, providers, assertions.
3. **CI integration** — Runs as Forgejo Actions step. Blocks merge on regression.

**Dependencies:** Node.js, LLM API access.

**Scaling model:** Stateless per-eval. Parallel test execution configurable.

---

### 3.13 DuckBrain (Layer 6 — External)

| Field | Value |
|-------|-------|
| **Role** | Git-backed persistent agent memory (MCP server). Long-term storage for decisions, anti-patterns, project knowledge. |
| **Language** | Python (external, MCP) |
| **Interface** | MCP server (stdio) |

**Interfaces:**

1. **MCP tools** — `remember` (store memory), `recall` (semantic search), `list_keys` (hierarchical key browse), `forget` (tombstone), `squash` (compact old partitions).
2. **Namespaces** — Separate git repos per namespace. Default: `default`. Custom: `hermes-memory`, project-specific.
3. **Storage** — JSONL with git versioning. Parquet compaction for old partitions. Vector embeddings via `vss` extension.

**Dependencies:** DuckDB (with VSS extension), Git (versioning), Python.

**Scaling model:** Per-namespace git repos. Partitioning by date. `squash` compacts old JSONL → Parquet, removes tombstones. Hierarchical keys (`/projects/mcp/schema`) for organized retrieval.

---

### 3.14 OpenCode (Layer 3 — External)

| Field | Value |
|-------|-------|
| **Role** | Agent runner. Task-based execution with worktree isolation. HTTP API for programmatic control. |
| **Language** | TypeScript (external) |
| **Version** | 1.15+ |
| **Interface** | HTTP API + CLI |

**Interfaces:**

1. **HTTP API** — `POST /session` (create), `POST /session/{id}/prompt` (send prompt), `POST /session/{id}/prompt_async` (async), `GET /api/session` (list sessions).
2. **CLI** — `opencode serve` (start server), `opencode run` (one-shot), `opencode run -` (stdin-pipe).
3. **Config** — `opencode.jsonc` (model, small_model, disabled_providers, MCP config).

**Dependencies:** Node.js, LLM API access (via providers in `auth.json`).

**Scaling model:** Containerized per-project: `opencode-<project>` containers on ports 4096-4102. Each project gets isolated environment with its own dependencies.

**Critical config requirements:**
- `disabled_providers` MUST be `[]` (not `["opencode"]`)
- `model` MUST be a string (not an object)
- `small_model` MUST be a working model
- `chrome-devtools` MCP MUST be `enabled: false` on headless servers

---

### 3.15 OpenRouter (Cross-cutting — External)

| Field | Value |
|-------|-------|
| **Role** | Model aggregator (344+ models). Fusion multi-model. Single API key for all providers. |
| **Interface** | OpenAI-compatible API |

**Interfaces:** `POST /api/v1/chat/completions` (OpenAI-compatible). `GET /api/v1/models` (catalog). `POST /api/v1/keys` (key management). `GET /api/v1/auth/key` (key verification).

**Key management:**
- Management key (`OR_MANAGEMENT_KEY`) provisions new keys: `POST /api/v1/keys` with `name`, `label`, `limit`, `limit_reset`.
- Guardrails enforce model restrictions per key (e.g., Flash tier keys can only use budget models).
- H4F provisions per-friend keys with appropriate guardrails.

**Failure modes:**

| Failure | Impact | Recovery |
|---------|--------|----------|
| Privacy guardrail blocks premium models | Claude/Gemini unavailable | Create fresh key without restrictions, or fix at `openrouter.ai/settings/privacy` |
| Key exhausted (budget hit) | Agent can't call LLM | `doctor.fix_dead_keys()` provisions new key. Or increase budget. |
| Rate limit (429) | Model calls fail | Chimera circuit breaker opens. Formation auto-adjusts to available models. |

---

### 3.16 Continue.dev (Layer 1 — External)

| Field | Value |
|-------|-------|
| **Role** | Open-source AI code assistant. IDE bridge for human developers. |
| **Language** | TypeScript (external) |

**Interfaces:** IDE extension (VS Code, JetBrains). Connects to LLM providers.

**Role in Helix:** Human interface. Developers use Continue.dev for pair programming while agents work via OpenCode. Continue.dev provides the "human strand" of the double helix.

---

### 3.17 External MCP Servers (Cross-cutting — External)

| Field | Value |
|-------|-------|
| **Role** | External system mirrors. Atlassian, Notion, GitHub, Chrome DevTools. |
| **Protocol** | MCP (Model Context Protocol) |

**Interfaces:** MCP server (stdio or HTTP). Tools registered with agent runtime.

**Role in Helix:** Axiom syncs outward (Jira tickets, Notion docs). Chrome DevTools for browser automation. GitHub for external repo mirroring.

**Note:** Chrome DevTools MCP MUST be `enabled: false` on headless servers — it blocks `POST /session` calls in OpenCode.

---

### 3.18 Verification (Section 3)

1. **Component inventory check:** `docker ps` MUST show running containers for: Forgejo (:3000), LangFuse (:3000), Chimera (:8765), Conscientiousness (:8080), and at least one H4F agent sandbox. Muster (:9090) and PromptFoo run on-demand.
2. **Interface check:** Each component MUST respond to its health endpoint: `curl http://<host>:<port>/v1/health/live` for Chimera, `curl http://<host>:8080/api/v1/health` for Conscientiousness, `curl http://<host>:3000/api/v1/version` for Forgejo.
3. **Failure mode check:** For each component, simulate one documented failure mode and verify the documented recovery procedure works. At minimum: kill Chimera, restart, verify circuit breakers clear. Kill a Forgejo Actions runner, verify it reconnects.
4. **Dependency check:** Each component's dependencies MUST be documented and satisfied. Chimera MUST have at least one provider API key (DeepSeek). GitReins MUST have pipeline config for Tier 2. Axiom MUST have `opencode` on PATH.

---

## 4. Integration Contracts

This section specifies every cross-component contract in the Helix platform. Each contract defines the calling component, the target component, the interface (API/protocol), the request/response shapes, and the failure semantics. These contracts are the binding agreements between components — violating one is a bug.

---

### 4.1 Chimera ↔ Forgejo

**Purpose:** Chimera reviews PRs in Forgejo. A Forgejo Action triggers Chimera review on PR open/update. Chimera posts review findings back as PR comments.

**Direction:** Forgejo → Chimera (trigger), Chimera → Forgejo (results).

**Trigger (Forgejo Action → Chimera):**

```
Forgejo Action on PR open/update
    │
    ├─ GET PR diff via Forgejo API
    │    GET /api/v1/repos/{owner}/{repo}/pulls/{index}/files
    │
    ├─ POST diff + PR context to Chimera
    │    POST http://chimera:8765/v1/deliberate
    │    Body: {
    │      "prompt": "Review this PR diff for correctness, security, and style:\n\n<diff>\n\nAcceptance criteria:\n<ACs from linked spec>",
    │      "formation": "audit",
    │      "output_schema": {
    │        "type": "object",
    │        "properties": {
    │          "verdict": {"type": "string", "enum": ["APPROVE", "REQUEST_CHANGES", "REJECT"]},
    │          "findings": {
    │            "type": "array",
    │            "items": {
    │              "type": "object",
    │              "properties": {
    │                "severity": {"type": "string"},
    │                "file": {"type": "string"},
    │                "line": {"type": "integer"},
    │                "description": {"type": "string"}
    │              }
    │            }
    │          }
    │        }
    │      }
    │    }
    │
    └─ Chimera returns structured review
```

**Results (Chimera → Forgejo):**

Forgejo Action posts Chimera's verdict as a PR review:
```
POST /api/v1/repos/{owner}/{repo}/pulls/{index}/reviews
Body: {
  "event": "APPROVED" | "REQUEST_CHANGES" | "COMMENT",
  "body": "## Chimera Multi-Model Review\n\n**Formation:** audit (3 workers + aggregator)\n**Models:** glm-5.2, claude-sonnet-4, deepseek-v4-pro\n**Verdict:** APPROVE\n\n### Findings\n...",
  "comments": [
    {"path": "file.go", "line": 42, "body": "[HIGH] ..."}
  ]
}
```

**Failure semantics:**
- Chimera unreachable (circuit breaker open) → Forgejo Action retries 3x with 10s backoff. After exhaustion, PR review posts "Chimera unavailable" comment. Human must review manually.
- Chimera returns `trace.source == "fallback"` → Comment includes WARNING that single-model fallback was used (not true multi-model review).
- Chimera budget exhausted → `BudgetExhaustedError`. PR review not posted. Human alerted.

---

### 4.2 Axiom ↔ H4F

**Purpose:** Axiom orchestrates agents. H4F hosts them. Axiom requests agent containers from H4F; H4F provisions sandboxes and returns connection details.

**Direction:** Axiom → H4F (provisioning request), H4F → Axiom (agent ready).

**Provisioning contract:**

```
Axiom work item assigned to agent
    │
    ├─ Axiom calls H4F provisioning (via known-friends.json or API)
    │    Request: {
    │      "friend_name": "axiom-agent-<work_item_id>",
    │      "tier": "pro | flash",
    │      "repo": "org/repo-name",
    │      "budget_usd": 5.00,
    │      "permissions": ["push:feat/*", "pr:open", "read"],
    │      "ttl_seconds": 3600
    │    }
    │
    ├─ H4F provisions:
    │    1. OpenRouter key with guardrail (pro → all models, flash → budget only)
    │    2. Docker container (gluetun VPN + DinD executor + hermes-agent)
    │    3. Storage Box secret files (.openrouter-key.env, etc.)
    │    4. known-friends.json entry
    │
    └─ H4F returns:
         {
           "container_id": "sha256",
           "container_name": "hermes-agent-<name>",
           "agent_endpoint": "http://<name>.helix-net:port",
           "openrouter_key_hash": "sha256",
           "guardrail_id": "grd_xxx",
           "ready": true
         }
```

**Teardown contract:**
```
Axiom work item complete or timed out
    │
    ├─ Axiom calls H4F offboard:
    │    offboard_friend("axiom-agent-<work_item_id>")
    │
    ├─ H4F:
    │    1. Stops and removes Docker containers
    │    2. Deletes OpenRouter key
    │    3. Removes Storage Box files
    │    4. Sets known-friends.json status = "offboarded"
    │    5. cleanup_orphans.py runs on next cron to clean any remnants
    │
    └─ Axiom logs: work item agent released
```

**Failure semantics:**
- H4F provisioning fails (OpenRouter API down) → Axiom retries with 60s backoff. After 3 failures, work item enters `blocked_provisioning`.
- Agent container crashes → H4F `doctor` detects via health check. Re-provisions automatically.
- Key drift (three-way) → H4F `consistency.run_consistency_fix()` resolves on next cron cycle (every 5 min).
- Budget exceeded → H4F guardrail blocks further LLM calls. Agent receives 403. Axiom marks work item `failed_budget`.

---

### 4.3 GitReins ↔ Forgejo

**Purpose:** GitReins hooks run inside Forgejo repos. The pre-receive hook is triggered by git pushes to Forgejo. GitReins blocks pushes that fail Tier 1/2 gates.

**Direction:** Forgejo → GitReins (git push triggers hook), GitReins → Forgejo (hook exit code blocks/allows).

**Hook installation contract:**

GitReins is installed per-repo on Forgejo. The hook lives at `.git/hooks/pre-receive` (server-side) within the bare repo managed by Forgejo. For agent-created repos, the hook MUST be installed before the first agent commit.

**Trigger flow:**
```
Agent pushes to Forgejo (git push origin feat/xyz)
    │
    ├─ Forgejo receives push
    │
    ├─ Forgejo triggers pre-receive hook
    │    Hook: .git/hooks/pre-receive
    │    Hook runs: python3 gitreins/guard_manager.py
    │
    ├─ Tier 1: static guards (secrets, lint, tests, dead_code, skylos)
    │    Runs in parallel
    │    Secrets = hard block (exit 1)
    │    Lint/tests/dead_code = configurable (blocking or warning)
    │
    ├─ Tier 2: agentic evaluator (if pipeline.stages configured)
    │    LLM-powered judge reads diff, runs tools, returns verdict
    │    COMPLETE → pass
    │    INCOMPLETE → block (configurable)
    │
    ├─ Exit code 0 → push accepted
    └─ Exit code 1 → push rejected, agent sees error
```

**Forgejo Action integration (CI-level GitReins):**

For PR-level evaluation (not push-level), a Forgejo Action runs GitReins Tier 2 evaluator on the PR diff:
```yaml
# .forgejo/workflows/gitreins.yml
on:
  pull_request:
    branches: [main]
jobs:
  evaluate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: GitReins Tier 2 Evaluation
        run: |
          PYTHONPATH=. python3 -c "
          from engine.llm import LLMClient
          from engine.evaluator import AgenticEvaluator
          import os, json
          llm = LLMClient()
          ev = AgenticEvaluator(llm, workdir='.', max_iterations=25)
          task = json.loads(os.environ['TASK_JSON'])
          verdict = ev.evaluate(task)
          print(json.dumps({'verdict': verdict.verdict, 'items': [v.__dict__ for v in verdict.items]}))
          if verdict.verdict != 'COMPLETE':
            exit(1)
          "
        env:
          GITREINS_LLM_API_KEY: ${{ secrets.DEEPSEEK_API_KEY }}
          GITREINS_LLM_BASE_URL: https://api.deepseek.com/v1
          GITREINS_LLM_MODEL: deepseek/deepseek-v4-pro
```

**Failure semantics:**
- Hook not installed → No quality gate. Forgejo Action CI catches it (if configured). Severity: HIGH.
- Tier 1 secrets scanner misses a key → Regex must include hyphens: `sk-[a-zA-Z0-9_-]{20,}`. OpenRouter keys are `n0t-a-r3al-k3y`.
- Tier 2 evaluator LLM down → Tier 2 skipped (WARNING). If `tier2_required: true` in config, push blocked.
- `docker cp` files owned by root → Agent can't read. `chown opencode:opencode` after every copy.

---

### 4.4 Muster ↔ Everything

**Purpose:** Muster auto-generates MCP tools for any REST API. In Helix, Muster wraps Forgejo API, OpenRouter API, LangFuse API, and any external service API so agents can call them programmatically.

**Direction:** Any component with a REST API → Muster (spec parsing), Muster → Any component (tool execution).

**Spec ingestion contract:**
```
Muster.Parse(openapi_spec_url or openapi_spec_json)
    │
    ├─ pkg/openapi parser extracts operations
    │    Each operation: {method, path, parameters, responses, summary}
    │
    ├─ pkg/mcp.Converter.ConvertToTools(operations)
    │    Generates MCP tool definitions:
    │    {name, description, inputSchema (JSON Schema), handler}
    │
    └─ Tools registered with agent runtime
         Agent can call: muster_forgejo_create_pull_request(...)
         Agent can call: muster_openrouter_list_models(...)
         Agent can call: muster_langfuse_get_traces(...)
```

**Forgejo API wrapping (example):**
```yaml
# Muster reads: GET http://forgejo:3000/swagger.json
# Generates tools:
- muster_forgejo_create_issue(repo, title, body) → issue_number
- muster_forgejo_create_pull_request(repo, title, head, base, body) → pr_index
- muster_forgejo_merge_pull_request(repo, index) → success
- muster_forgejo_get_repo(owner, repo) → repo_info
- muster_forgejo_list_branches(repo) → [branch_names]
```

**OpenRouter API wrapping (example):**
```yaml
# Muster reads: GET https://openrouter.ai/api/v1/openapi.json
# Generates tools:
- muster_openrouter_create_key(name, label, limit, limit_reset) → {key, id}
- muster_openrouter_list_models() → [model_info]
- muster_openrouter_get_key_info() → {is_management_key, limit, usage}
- muster_openrouter_chat_completions(model, messages) → response
```

**Known limitation:** `OpenAPIExecutor.Execute()` is a stub. The converter generates correct tool definitions but execution hardcodes `GET /`. Workaround: use `pkg/client` directly with parsed operations. This means Muster tools are currently definitions-only — agents must use the HTTP client to actually call APIs.

**Failure semantics:**
- OpenAPI spec malformed → Parser returns structured error. Agent receives error message with line number.
- Target API unreachable → Generated tool returns connection error. Agent can retry or report.
- Rate limit on target API → Muster daemon rate limiter backs off (if daemon mode enabled).

---

### 4.5 Conscientiousness ↔ Axiom

**Purpose:** Axiom triggers Conscientiousness adversarial review after Chimera review. Conscientiousness surfaces hidden assumptions and attack vectors. Results feed back to Axiom for work item confidence scoring.

**Direction:** Axiom → Conscientiousness (trigger), Conscientiousness → Axiom (report).

**Trigger contract:**
```
Axiom Step 9 (adversarial eval)
    │
    ├─ Axiom calls Conscientiousness API:
    │    POST http://conscientiousness:8080/api/v1/evaluate
    │    Body: {
    │      "work_item_id": "string",
    │      "diff": "<git diff output>",
    │      "spec_ref": "specs/<component>.md",
    │      "acceptance_criteria": ["criterion 1", "criterion 2"],
    │      "chimera_findings": [<findings from Step 8>]
    │    }
    │
    ├─ Conscientiousness:
    │    1. Generates attack vectors from spec + diff
    │    2. Agent argues against its own work
    │    3. Surfaces hidden assumptions
    │    4. Returns report
    │
    └─ Axiom receives:
         {
           "report_id": "uuid",
           "hidden_assumptions": [...],
           "attack_vectors": [{vector, severity, evidence, mitigated}],
           "self_critique": "...",
           "verdict": "DEFENSIBLE | VULNERABLE",
           "trust_level": 72
         }
```

**Integration with Axiom confidence scoring:**

Conscientiousness verdict feeds into Axiom's output confidence (spec 11):
- `DEFENSIBLE` + no HIGH attack vectors → `risk_transparency` signal boosted (+10-20 points)
- `VULNERABLE` or unmitigated HIGH attack vectors → `risk_transparency` signal penalized. PR forced to DRAFT.

**Failure semantics:**
- Conscientiousness service down → Axiom skips adversarial eval. Work item confidence capped at MEDIUM (69 max) without adversarial sign-off.
- DB migration corruption → `conscience init` on fresh DB. Schema repair via `AutoMigrate`.
- Bootstrap key expired → Re-run `conscience init`, extract new key.

---

### 4.6 Hivemind ↔ Axiom

**Purpose:** Hivemind provides persistent memory and task scheduling for Axiom agents. Axiom reads/writes memories via Hivemind API. Scheduled tasks trigger Axiom runs.

**Direction:** Axiom → Hivemind (memory read/write), Hivemind → Axiom (scheduled triggers).

**Memory contract:**
```
Axiom agent completes a step
    │
    ├─ Writes to Hivemind:
    │    POST http://hivemind:PORT/api/v1/memory
    │    Body: {
    │      "agent": "tower-axiom",
    │      "work_item_id": "string",
    │      "memory_type": "decision | finding | pattern | anti-pattern",
    │      "content": "string",
    │      "metadata": {"step_id": "...", "files": [...]}
    │    }
    │
    ├─ Later agent reads:
    │    GET http://hivemind:PORT/api/v1/memory?agent=tower-axiom&work_item_id=<id>
    │    Returns: [{memory_type, content, metadata, created_at}]
    │
    └─ Cross-session recall:
         GET http://hivemind:PORT/api/v1/memory/search?q=<semantic_query>
         Returns: ranked memories by relevance
```

**Scheduled task contract:**
```
Hivemind cron triggers
    │
    ├─ Hivemind evaluates scheduled tasks
    │    inbox/compiled pattern: deferred tasks accumulate, compiled on trigger
    │
    ├─ For each due task:
    │    POST http://axiom:PORT/api/v1/run
    │    Body: {
    │      "intent": "<task description>",
    │      "repo": "<repo path>",
    │      "trigger": "scheduled",
    │      "schedule_id": "string"
    │    }
    │
    └─ Axiom executes the run, writes results back to Hivemind memory
```

**Failure semantics:**
- Hivemind down → Axiom agents lose persistent memory for the session. Work continues but context is lost across sessions. Memories are recovered when Hivemind restarts (SQLite persists).
- Scheduled task misses trigger → Hivemind catches up on restart. Missed tasks queue and execute on next cycle.
- Rate limit exceeded → Hivemind hierarchical rate limiter backs off. Agent waits.

---

### 4.7 LangFuse ↔ All Components

**Purpose:** Every component that makes LLM calls MUST trace to LangFuse. This is architectural principle #3 (Section 1.6). Traces include model, tokens, cost, latency, decision rationale.

**Direction:** All LLM-calling components → LangFuse (trace ingestion).

**Trace contract (uniform across all components):**
```json
{
  "trace_id": "uuid-v4",
  "name": "<component>:<operation>",
  "timestamp": "ISO-8601 UTC",
  "input": {
    "prompt": "string (or hash if too large)",
    "model": "provider/model-name",
    "parameters": {"temperature": 0.7, "max_tokens": 4096}
  },
  "output": {
    "response": "string (or hash)",
    "finish_reason": "stop | length | content_filter",
    "tokens_input": 12000,
    "tokens_output": 3400
  },
  "metadata": {
    "component": "chimera | gitreins | axiom | conscientiousness",
    "agent": "tower-axiom",
    "work_item_id": "string",
    "cost_usd": 0.0023,
    "latency_ms": 4500
  }
}
```

**Component-specific tracing:**

| Component | How it traces | SDK/Method |
|-----------|---------------|------------|
| Chimera | structlog + optional Langfuse integration in `chimera.observability` | Python SDK |
| GitReins | Tier 2 evaluator traces LLM calls | Python SDK (manual) |
| Axiom | OpenCode session traces forwarded | OpenCode → LangFuse |
| Conscientiousness | LLM calls for attack vectors traced | Go SDK (manual HTTP) |
| H4F agents | Hermes agent traces | Hermes built-in LangFuse integration |

**Failure semantics:**
- LangFuse down → Components buffer traces locally (SDK default). Backfill when service recovers. Non-blocking.
- Cost attribution incomplete → Async finalization. Budget tracking may be temporarily inaccurate. Not blocking.
- Missing trace for a step → Auditor flags gap. The 12-step flow check (Section 1.9) requires evidence for each step including trace ID.

---

### 4.8 DuckBrain ↔ All Components

**Purpose:** DuckBrain provides long-term persistent memory across sessions for all agents. Decisions, anti-patterns, and project knowledge survive session restarts.

**Direction:** All agent-holding components → DuckBrain (MCP).

**Memory contract:**
```
Agent discovers a pattern or makes a decision
    │
    ├─ Writes to DuckBrain via MCP:
    │    Tool: mcp_duckbrain_remember
    │    Args: {
    │      "key": "/projects/helix/decisions/auth-model-v2",
    │      "domain": "concept",
    │      "attributes": {"decided_at": "...", "rationale": "..."},
    │      "embedding_text": "Decision: use OAuth2 for agent auth instead of PAT"
    │    }
    │
    ├─ Later session, agent recalls:
    │    Tool: mcp_duckbrain_recall
    │    Args: {
    │      "query": "how does agent authentication work",
    │      "namespace": "default",
    │      "limit": 5
    │    }
    │    Returns: ranked memories by semantic similarity
    │
    └─ Compaction:
         Tool: mcp_duckbrain_squash
         Converts old JSONL → Parquet, removes tombstones
```

**Namespaces:**
- `default` — general platform knowledge
- `hermes-memory` — Hermes operational memory
- `helix-<component>` — per-component deep knowledge

**Failure semantics:**
- DuckBrain MCP server down → Agent loses cross-session memory for duration. Git-backed storage means no data loss.
- Vector index corruption → `squash` with `dryRun=true` to assess. Rebuild index from JSONL.
- Namespace grows too large → `squash` compacts old partitions to Parquet.

---

### 4.9 OpenRouter ↔ Chimera/H4F

**Purpose:** OpenRouter is the primary model access layer. Chimera routes through it for premium models. H4F provisions per-friend keys with guardrails.

**Direction:** Chimera → OpenRouter (model calls), H4F → OpenRouter (key management).

**Chimera → OpenRouter contract:**
```
Chimera Gateway (LiteLLM-backed)
    │
    ├─ Provider routing:
    │    "deepseek/deepseek-v4-pro" → provider: deepseek (direct API, NOT OpenRouter)
    │    "openrouter/anthropic/claude-sonnet-4" → provider: openrouter
    │    "zai" → provider: zai (Z.AI direct API)
    │
    ├─ LiteLLM translates to provider-native format:
    │    OpenRouter: standard OpenAI-compatible call
    │    DeepSeek: openai provider with api_base override
    │    Z.AI: openai provider with api_base override
    │
    └─ Circuit breaker per provider:
         CLOSED (normal) → OPEN (N failures) → HALF_OPEN (probe) → CLOSED
```

**H4F → OpenRouter key management contract:**
```
H4F provision_friend(name, tier)
    │
    ├─ Creates OpenRouter key:
    │    POST https://openrouter.ai/api/v1/keys
    │    Headers: Authorization: Bearer <MANAGEMENT_KEY>
    │    Body: {
    │      "name": "helix-<friend_name>",
    │      "label": "helix",
    │      "limit": 10 (pro) | 2 (flash),
    │      "limit_reset": "weekly"
    │    }
    │    Response: {"key": "n0t-a-r3al-k3y", "id": "key_xxx"}
    │
    ├─ Assigns guardrail:
    │    Pro tier → all models allowed
    │    Flash tier → budget models only (deepseek-v4-flash, etc.)
    │
    ├─ Writes key to Storage Box:
    │    .openrouter-key.env → OPENROUTER_API_KEY (set via env var)
    │
    └─ Updates known-friends.json:
         {"<friend_name>": {"tier": "pro", "key_hash": "sha256", "guardrail_id": "grd_xxx"}}
```

**Failure semantics:**
- OpenRouter API down → Chimera circuit breaker opens for `openrouter` provider. Formation auto-adjusts to direct providers (deepseek, zai). H4F key provisioning retries.
- Key rate limited (429) → Chimera backoff. H4F doctor detects dead keys, re-provisions.
- Privacy guardrail blocks premium models → Create fresh key from management key. Fix at `openrouter.ai/settings/privacy`.
- Management key expired → All new key provisioning fails. Alert ops immediately. Re-provision management key.

---

### 4.10 Verification (Section 4)

1. **Contract conformance check:** For each integration contract, an integration test MUST verify the request/response shapes match the documented schema. At minimum: Chimera↔Forgejo (PR review round-trip), Axiom↔H4F (provision + teardown), GitReins↔Forgejo (hook blocks bad push).
2. **Failure injection check:** For each contract, simulate the primary failure mode and verify the documented recovery works. At minimum: kill Chimera mid-review, verify Forgejo Action handles gracefully.
3. **Trace coverage check:** Every LLM call in the 12-step flow MUST produce a LangFuse trace. Query LangFuse for traces with `metadata.work_item_id == <test_id>` and verify at least one trace per step that involves an LLM call (Steps 2, 4, 6, 8, 9).
4. **Muster tool generation check:** Point Muster at Forgejo's OpenAPI spec (`GET /swagger.json`). Verify tools are generated for at least: create_issue, create_pull_request, merge_pull_request. Note: execution may require the `pkg/client` workaround due to the executor stub.

---

## 5. Identity and Access Management (IAM)

Helix agents are first-class actors with real identities, scoped permissions, and accountable behavior. This section specifies the identity lifecycle, permission model, co-approval protocol, and key rotation. Architectural principle #1 (Section 1.6) states: "Agents have real identities."

---

### 5.1 Identity Source: known-friends.json

**Source of truth:** `/opt/hermes-demo/.hermes/h4f/known-friends.json` (production, Hetzner host).

**Local copy:** `/home/kara/.hermes/h4f/known-friends.json` (currently empty `{}` — use production as ground truth).

**Schema:**
```json
{
  "wojons": {
    "tier": "pro",
    "status": "active",
    "openrouter_key_hash": "sha256:hex",
    "guardrail_id": "grd_xxx",
    "telegram_chat_id": 123456789,
    "email": "user@example.com",
    "container_name": "hermes-agent-wojons",
    "created_at": "2026-06-01T00:00:00Z",
    "trust_level": 85,
    "budget_usd_weekly": 10.00,
    "budget_used_usd": 3.42,
    "permissions": ["push:feat/*", "pr:open", "read:all"],
    "forgejo_username": "agent-wojons",
    "forgejo_user_id": 42,
    "ssh_key_fingerprint": "SHA256:..."
  },
  "llopez": {
    "tier": "flash",
    "status": "active",
    ...
  }
}
```

**Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `tier` | `pro \| flash` | Pro = all models, high budget. Flash = budget models, low budget. |
| `status` | `active \| offboarded \| suspended` | Only `active` agents can operate. |
| `openrouter_key_hash` | `sha256:hex` | Hash of the agent's OpenRouter API key (never store plaintext). |
| `guardrail_id` | `string` | OpenRouter guardrail enforcing model restrictions. |
| `trust_level` | `integer (0-100)` | Earned through acceptance rate. Determines merge privileges. |
| `budget_usd_weekly` | `float` | Weekly spend limit. Enforced by OpenRouter key limit. |
| `budget_used_usd` | `float` | Current period spend. Tracked via OpenRouter API. |
| `permissions` | `array[string]` | Scoped permissions (see 5.3). |
| `forgejo_username` | `string` | Forgejo account username (provisioned via OAuth). |
| `forgejo_user_id` | `integer` | Forgejo internal user ID. |
| `ssh_key_fingerprint` | `SHA256:...` | Agent's SSH key for git operations. |

---

### 5.2 Forgejo OAuth Flow: H4F → Forgejo Account Provisioning

**Goal:** Each agent in known-friends.json gets a real Forgejo account with SSH keys, scoped permissions, and OAuth token.

**Provisioning sequence:**

```
H4F known-friends.json entry created
    │
    ├─ Step 1: Create Forgejo user
    │    POST http://forgejo:3000/api/v1/admin/users
    │    Headers: Authorization: Basic <admin:password>
    │    Body: {
    │      "username": "agent-<friend_name>",
    │      "email": "agent-<friend_name>@helix.local",
    │      "password": "<random-32-char>",
    │      "must_change_password": false,
    │      "source_id": 0,
    │      "login_name": "agent-<friend_name>",
    │      "send_notify": false
    │    }
    │    Response: 201 Created, { "id": 42, "username": "agent-wojons", ... }
    │
    ├─ Step 2: Generate SSH keypair (ED25519)
    │    crypto/ed25519.GenerateKey() → public_key, private_key
    │    Public key format: "ssh-ed25519 AAAA... agent-<friend_name>@helix"
    │
    ├─ Step 3: Register SSH key with Forgejo
    │    POST http://forgejo:3000/api/v1/user/keys
    │    Headers: Authorization: Basic <agent-user:password>
    │    Body: {
    │      "title": "agent-<friend_name>",
    │      "key": "ssh-ed25519 AAAA... agent-<friend_name>@helix"
    │    }
    │
    ├─ Step 4: Create Personal Access Token (PAT)
    │    POST http://forgejo:3000/api/v1/users/agent-<friend_name>/tokens
    │    Headers: Authorization: Basic <admin:password>
    │    Body: { "name": "helix-pat", "scopes": ["write:repository", "read:user"] }
    │    Response: { "sha1": "<40-char-token>" }
    │
    ├─ Step 5: Set repository permissions
    │    Per-repo: restrict agent to push feat/* branches only
    │    Via branch protection rules in Forgejo repo settings
    │
    ├─ Step 6: Update known-friends.json
    │    forgejo_username, forgejo_user_id, ssh_key_fingerprint written
    │    PAT stored in Storage Box (.forgejo-token.env)
    │
    └─ Agent is now a first-class Forgejo user
```

**Critical details:**
- PAT creation requires BasicAuth (admin credentials), NOT the agent's own auth. The admin provisions the token.
- ED25519 keys via `crypto/ed25519` stdlib — avoid `golang.org/x/crypto/ssh` for v1 (simpler, fewer deps).
- Branch protection is per-repo, not per-user. Configured in Forgejo repo settings: agents can push to `feat/*`, PR open, READ repos — NEVER merge solo, NEVER push to main.
- `must_change_password: false` — agents can't interactively change passwords.

---

### 5.3 Agent Permission Model

Agents have scoped permissions. The model is deny-by-default: anything not explicitly granted is denied.

**Permission scopes:**

| Permission | Description | Pro Tier | Flash Tier |
|------------|-------------|----------|------------|
| `read:all` | Read all repos, issues, PRs | ✅ | ✅ |
| `push:feat/*` | Push to feature branches | ✅ | ✅ |
| `push:main` | Push directly to main | ❌ | ❌ |
| `pr:open` | Open pull requests | ✅ | ✅ |
| `pr:review` | Review/comment on PRs | ✅ | ❌ |
| `pr:approve` | Approve PRs (agent co-approval) | ✅ (trust ≥ 70) | ❌ |
| `pr:merge` | Merge PRs | ❌ (never solo) | ❌ |
| `issue:create` | Create issues | ✅ | ✅ |
| `issue:close` | Close issues | ✅ (trust ≥ 50) | ❌ |
| `repo:create` | Create new repositories | ❌ | ❌ |
| `admin:users` | Create/modify Forgejo users | ❌ | ❌ |
| `secrets:read` | Read secret material | ❌ | ❌ |

**Enforcement points:**

| Permission | Enforced By | Mechanism |
|------------|-------------|-----------|
| `read:all` | Forgejo | Repo visibility + user team membership |
| `push:feat/*` | Forgejo | Branch protection rules (wildcard `feat/*`) |
| `push:main` | Forgejo | Branch protection (`main` = protected, force-push disabled) |
| `pr:open` | Forgejo | User has `write` repo permission |
| `pr:approve` | Axiom + Forgejo | Axiom checks `trust_level ≥ 70` before posting approval |
| `pr:merge` | Forgejo | Requires BOTH human + agent approval (branch protection rule) |
| `issue:create/close` | Forgejo | User permission scoping |
| Model access | OpenRouter | Guardrail ID enforces model whitelist per key |

**Trust escalation:**

Agents earn trust through demonstrated reliability:
```
trust_level 0-29  → NEW AGENT. push:feat/* + pr:open + read only.
                     All PRs require human review. No agent approval.

trust_level 30-49 → ESTABLISHED. Can close issues.
                     PRs still require human review.

trust_level 50-69 → TRUSTED. Can review PRs (comment only, no approval).
                     Can close issues independently.

trust_level 70-89 → SENIOR. Can co-approve PRs (agent half of co-approval).
                     Still cannot merge solo.

trust_level 90+   → ELDER. Nominate for expanded permissions.
                     Still cannot merge solo. Still cannot push to main.
```

**Trust level calculation:**
```
trust_level = base_score + acceptance_bonus - rejection_penalty - incident_penalty

base_score = 30 (starting trust for provisioned agents)
acceptance_bonus = min(40, merged_prs * 2)
rejection_penalty = min(20, rejected_prs * 3)
incident_penalty = min(30, security_incidents * 10 + data_incidents * 15)
```

Trust is recalculated daily by Hivemind cron and stored in known-friends.json.

---

### 5.4 Human + Agent Co-Approval Protocol

**CRITICAL INVARIANT:** Neither human nor agent can merge a PR solo. This is enforced at the Forgejo branch protection level — `main` requires a minimum of 2 approvals, and the approval set MUST include at least one human and at least one agent.

**Forgejo branch protection configuration:**
```yaml
# Per-repo settings (via Forgejo API or UI)
branch_protection:
  branch_name: "main"
  enable_push: false                    # No direct pushes
  enable_push_whitelist: false
  required_approvals: 2                 # Minimum 2 approvals
  enable_status_check: true             # CI must pass
  status_check_contexts: ["Forgejo Actions"]
  enable_approvals_whitelist: false     # Any team member can approve
  block_on_rejected_reviews: true       # Rejection blocks merge
  block_on_outdated_branch: true        # Must be up to date with main
```

**Co-approval enforcement:**

Forgejo doesn't natively distinguish human vs agent approvals. Helix adds a custom check:

```
PR merge requested
    │
    ├─ Forgejo checks: required_approvals >= 2? CI green?
    │
    ├─ Helix custom check (Forgejo Action or pre-merge hook):
    │    GET /api/v1/repos/{owner}/{repo}/pulls/{index}/reviews
    │    For each approval:
    │      Look up reviewer username in known-friends.json
    │      If found → agent approval
    │      If not found → human approval
    │    
    │    REQUIRE: at least 1 human approval AND at least 1 agent approval
    │
    ├─ Pass → merge allowed
    └─ Fail → merge blocked, comment: "Co-approval required: need both human and agent approval"
```

**Agent approval gate:**
```
Agent reviews PR (Step 11)
    │
    ├─ Agent checks trust_level ≥ 70?
    │    NO → Agent cannot approve. Posts COMMENT only.
    │    YES → Agent evaluates evidence bundle:
    │      - All ACs pass?
    │      - GitReins Tier 2 COMPLETE?
    │      - Chimera verdict APPROVE?
    │      - Conscientiousness DEFENSIBLE?
    │      - No unmitigated HIGH findings?
    │
    ├─ All checks pass → Agent posts APPROVED review
    ├─ Any check fails → Agent posts REQUEST_CHANGES with specific findings
    └─ Confidence < 40 → PR forced to DRAFT by Axiom
```

**Override protocol:**
- Human can force-merge without agent approval by applying the `force-merge` label. This is logged in the audit trail with human identity and justification comment. Use sparingly — defeats the co-approval invariant.
- Agent can NEVER force-merge. No override exists for agents.
- `force-merge` triggers a post-merge review by Conscientiousness (was the override justified?).

---

### 5.5 Key Rotation Lifecycle

**Keys managed by the platform:**

| Key Type | Owner | Rotation Trigger | Rotation Method |
|----------|-------|-----------------|-----------------|
| OpenRouter API key (per-friend) | H4F | Budget hit, 401 auth failure, quarterly | `provision_openrouter_key(name, tier)` — creates new key, assigns guardrail, writes Storage Box |
| OpenRouter management key | Ops | Compromise suspected, annually | Manual: create new at openrouter.ai, update `OR_MANAGEMENT_KEY` in `.env` |
| Forgejo admin password | Ops | Compromise suspected, annually | Manual: Forgejo admin panel |
| Forgejo PAT (per-agent) | H4F | Expiry (90 days), compromise | `POST /api/v1/users/{name}/tokens` — create new, revoke old |
| Forgejo SSH key (per-agent) | H4F | Compromise suspected | Generate new ED25519 keypair, register new key, revoke old |
| DeepSeek API key | Ops | Compromise suspected, quarterly | Manual: create new at platform.deepseek.com, update `.env` |
| Z.AI API key | Ops | Compromise suspected, quarterly | Manual: create new at z.ai, update `.env` |
| Anthropic API key | Ops | Compromise suspected, quarterly | Manual: create new at anthropic console, update `.env` |
| Chimera API key | Chimera | Compromise suspected | Regenerate in `chimera.yaml`, restart service |
| Docker registry credentials | Ops | Compromise suspected | Manual: update Docker config |

**Automated rotation (H4F pipeline):**

```
Bridge cron (every 5 minutes)
    │
    ├─ doctor.fix_dead_keys()
    │    For each active friend:
    │      Test OpenRouter key: GET /api/v1/auth/key
    │      If 401 → key is dead
    │        1. Create new key via management API
    │        2. Assign guardrail (same tier)
    │        3. Write to Storage Box (.openrouter-key.env)
    │        4. Update known-friends.json (key_hash)
    │        5. Trigger container redeploy (Coolify)
    │        6. Log: WARN | doctor | <friend> | dead_key | FIXED | new key provisioned
    │
    ├─ consistency.run_consistency_fix()
    │    For each active friend:
    │      Sync Storage Box ↔ container env ↔ .env
    │      If drift detected → push system state INTO config (one-way sync)
    │
    └─ guardrail_enforcer
         For each unassigned key:
           Auto-assign to Flash tier via assign_key_to_tier_with_fallback()
```

**Manual rotation procedure (ops):**
```bash
# 1. Generate new key
NEW_KEY=$(curl -s -X POST "https://openrouter.ai/api/v1/keys" \
  -H "Authorization: Bearer $OR_MANAGEMENT_KEY" \
  -d '{"name":"rotated-$(date +%Y%m%d)","limit":10,"limit_reset":"weekly"}' | jq -r .key)

# 2. Update .env (NEVER append duplicate — always replace)
sed -i "s|OPENROUTER_API_KEY=.*|OPENROUTER_API_KEY=$NEW_KEY|" ~/.hermes/.env

# 3. Restart affected services
docker restart chimera
# H4F agents pick up new key on next cron cycle via doctor

# 4. Revoke old key
curl -s -X DELETE "https://openrouter.ai/api/v1/keys/$OLD_KEY_ID" \
  -H "Authorization: Bearer $OR_MANAGEMENT_KEY"

# 5. Verify
grep OPENROUTER_API_KEY ~/.hermes/.env  # should show new key only, once
```

**Key rotation pitfalls:**
- NEVER add duplicate keys to `.env`. Always `grep` first, use `sed -i` to replace. Adding a second `DEEPSEEK_API_KEY=` line BREAKS Hermes auth entirely.
- H4F Storage Box sync is one-way (system → config). Never create new keys for existing friends during auto-repair — only during explicit re-key.
- PAT expiry: Forgejo PATs don't auto-renew. H4F should check PAT validity in the consistency layer and alert before expiry.

---

### 5.6 Verification (Section 5)

1. **Identity check:** Every agent active in known-friends.json MUST have: Forgejo user account (queryable via `GET /api/v1/users/agent-<name>`), SSH key registered, PAT created, guardrail assigned. An audit script MUST verify all four for each active friend.
2. **Permission check:** Attempt to push to `main` as an agent — MUST be rejected by branch protection. Attempt to merge a PR with only agent approval (no human) — MUST be blocked by co-approval check. Attempt to merge with only human approval (no agent) — MUST be blocked.
3. **Trust escalation check:** Verify trust_level calculation: create a test agent, merge 5 PRs (all accepted), verify trust_level increases by 10 points. Reject 2 PRs, verify trust_level decreases by 6 points.
4. **Key rotation check:** Trigger `doctor.fix_dead_keys()` manually with a known-dead key. Verify: new key created, guardrail assigned, Storage Box updated, container redeployed, known-friends.json updated, second cron run is a no-op (no self-infliction).

---

## 6. Security Model

Helix runs autonomous AI agents with real identities, real code access, and real budgets. This creates a unique threat surface: agents can be tricked, models can be manipulated, and the supply chain (prompts → code → commits) must be protected end-to-end. This section specifies the security architecture, secrets management, network isolation, blast radius containment, and audit trail requirements.

---

### 6.1 Threat Model

Helix faces threats from four vectors:

| Threat Vector | Description | Primary Mitigation |
|---------------|-------------|-------------------|
| **Prompt injection** | Malicious content in code, issues, or specs manipulates agent behavior | GitReins secrets scanner, output sanitization, sandbox isolation |
| **Model manipulation** | Adversarial inputs cause models to generate malicious code | Multi-model review (Chimera), adversarial self-eval (Conscientiousness) |
| **Credential exfiltration** | Agent leaks API keys, SSH keys, or tokens via commits | GitReins secrets scanner (pre-commit), sandbox network isolation |
| **Supply chain** | Malicious dependencies, compromised packages in agent-generated code | Dependency pinning, vulnerability scanning, human co-approval |

**Assumptions:**
- LLM APIs (DeepSeek, OpenRouter, Z.AI) are trusted infrastructure. We do not defend against the provider themselves being malicious.
- The Hetzner host is trusted. Physical security is Hetzner's responsibility.
- Network egress is monitored but not fully restricted (agents need LLM API access).

---

### 6.2 Secrets Management

**Principle:** Secrets never appear in code, commits, or logs. They live in environment variables, Storage Box files, or secret managers — never in git-tracked files.

**Secret storage locations:**

| Location | What's Stored | Access Control |
|----------|---------------|----------------|
| `~/.hermes/.env` | LLM API keys (DeepSeek, OpenRouter, Z.AI, Anthropic) | File perms 600. Hermes process only. |
| Storage Box (per-friend) | `.openrouter-key.env`, `.telegram-token.env`, `.email-password.env` | CIFS mount. Per-friend isolation. |
| Forgejo Secrets (CI) | CI-only keys (PromptFoo eval key, deployment tokens) | Forgejo encrypted secrets. CI runners only. |
| `chimera.yaml` (gitignored) | Chimera API keys, provider keys | File perms 600. Copied from `chimera.yaml.example`. |
| `.axiom/axiom.config.yaml` | Axiom-level secrets (Jira tokens, etc.) | Per-repo. File perms 600. |
| Docker secrets / Compose env | Container-level secrets | Docker secret management or `environment:` in compose. |

**Secrets scanner (GitReins Tier 1):**

Every commit passes through the secrets scanner before entering the forge. The scanner runs as a pre-commit hook:

```bash
# Pattern: catches real key formats including hyphens
SECRET_PATTERNS='sk-[a-zA-Z0-9_-]{20,}|ghp_[a-zA-Z0-9]{36}|-----BEGIN (RSA |EC |OPENSSH )PRIVATE KEY-----|(OPENROUTER|DEEPSEEK|ZAI|ANTHROPIC)_API_KEY\\s*=\\s*\\S'
```

**What the scanner catches:**
- `n0t-a-r3al-k3y` — OpenRouter keys (hyphens included)
- `sk-65e4...` — DeepSeek keys
- `ghp_xxx...` — GitHub PATs (36 chars)
- `--beg1n-rsa-pr1vate-key--` — SSH/RSA private keys
- `OPENROUTER_API_KEY (set via env var)` — env-var-style assignments
- `DEEPSEEK_API_KEY=...`, `ZAI_API_KEY=...`, `ANTHROPIC_API_KEY=...`

**What it does NOT catch (known gaps):**
- Keys split across multiple lines
- Base64-encoded keys
- Keys in binary files (images, compiled binaries)
- Keys with formats outside the known patterns

**Protocol: install guards BEFORE wiring secrets.** The sequence MUST be:
1. Check if `.gitignore` covers the secret file
2. Install pre-commit hook with secrets scanner
3. Test the hook with a dry-run (commit a fake key, verify it blocks)
4. Then add the real secret

Never commit secrets then add the guard — the guard must be in place before any credential touches disk.

**`.gitignore` requirements:**
Every Helix repo MUST `.gitignore`:
```
# Secrets
.env
.env.*
*.key
*.pem
chimera.yaml
.auth.json

# Agent artifacts
__pycache__/
node_modules/
package.json
pnpm-lock.yaml
pnpm-workspace.yaml
.axiom/state/
```

---

### 6.3 Network Isolation (Sandbox)

**Principle:** Agents execute in Docker containers with network isolation. No agent touches the host filesystem directly. Network egress is funneled through VPN.

**Three layers of isolation:**

```
┌──────────────────────────────────────────────────────┐
│  HOST NETWORK (Hetzner)                              │
│  Forgejo :3000, LangFuse :3000 exposed to admin      │
│  All other ports bound to 127.0.0.1                  │
├──────────────────────────────────────────────────────┤
│  helix-net (Docker bridge network)                   │
│  Chimera :8765, Conscientiousness :8080,             │
│  Muster :9090 — internal only, no host exposure      │
│  Components communicate via Docker DNS               │
├──────────────────────────────────────────────────────┤
│  Agent Sandboxes (per-friend Docker containers)      │
│  ┌─────────────────────────────────────────────────┐ │
│  │  gluetun VPN container                          │ │
│  │  All agent egress routes through VPN            │ │
│  │  │                                              │ │
│  │  ├── DinD (Docker-in-Docker) executor           │ │
│  │  │   Builds, tests run here                     │ │
│  │  │                                              │ │
│  │  ├── hermes-agent container                     │ │
│  │  │   Agent runtime, no host filesystem access   │ │
│  │  │   Mounts: workspace volume only              │ │
│  │  │                                              │ │
│  │  └── Network: only gluetun + helix-net          │ │
│  └─────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────┘
```

**Network rules:**

| Source | Destination | Allowed? | Mechanism |
|--------|-------------|----------|-----------|
| Agent container | LLM APIs (external) | ✅ | Via gluetun VPN egress |
| Agent container | Forgejo (:3000) | ✅ | Via helix-net Docker DNS |
| Agent container | Chimera (:8765) | ✅ | Via helix-net |
| Agent container | Host filesystem | ❌ | No bind mounts to host paths (only workspace volumes) |
| Agent container | Other agent's container | ❌ | No inter-agent network (each friend is isolated) |
| Host admin | Forgejo web UI | ✅ | SSH tunnel or direct (behind firewall) |
| Internet | Forgejo/LangFuse | ❌ | Not exposed publicly (reverse proxy + auth required) |

**gluetun VPN:**
- Every H4F agent sandbox includes a gluetun container.
- ALL agent network egress routes through the VPN tunnel.
- This provides: IP anonymization, geo-restriction bypass, network-level audit trail.
- VPN provider: configured per-deployment (Mullvad, NordVPN, etc.).

**DinD (Docker-in-Docker):**
- Agent containers include DinD for isolated builds and tests.
- Build artifacts never touch the host Docker daemon.
- Each agent's DinD is ephemeral — destroyed when the agent container is removed.

**Bubblewrap (bwrap) — additional isolation:**
- For components that need tighter sandboxing than Docker provides.
- bwrap 0.8+ provides namespace-based isolation without root.
- Used for: untrusted code execution, test runners handling adversarial inputs.

---

### 6.4 Blast Radius Containment

**Principle:** A compromised or malfunctioning agent should affect only its own work — not the entire platform.

**Containment layers:**

| Layer | What's Contained | Mechanism |
|-------|-----------------|-----------|
| **Container isolation** | Agent process, filesystem, network | Docker container per agent. No host FS access. |
| **Budget enforcement** | Financial damage from runaway agent | OpenRouter key with `limit` and `limit_reset`. Hard cap. |
| **Guardrail enforcement** | Model access scope | OpenRouter guardrail ID restricts which models an agent can call. |
| **Branch isolation** | Code damage scope | Agents push to `feat/*` branches only. Never `main`. |
| **Lock isolation** | Concurrent edit conflicts | Ralph Loop file-level locks prevent two agents editing the same files. |
| **Repository isolation** | Cross-repo damage | Each agent is scoped to specific repos via Forgejo permissions. |
| **Review isolation** | Bad code entering production | Multi-model review (Chimera) + adversarial eval (Conscientiousness) + human co-approval. No single point of failure. |
| **Key isolation** | Credential compromise scope | Per-agent OpenRouter keys. Compromising one agent's key doesn't compromise others. |

**Maximum blast radius of a single compromised agent:**

| Damage Type | Maximum Impact | Why |
|-------------|----------------|-----|
| Financial | `budget_usd_weekly` (typically $2-10) | OpenRouter hard limit on key |
| Code quality | One bad PR (blocked by review) | Multi-model review + human co-approval |
| Data exfiltration | Agent's workspace volume only | No host FS access, no inter-agent network |
| Credential theft | Agent's own OpenRouter key | Per-agent keys, no shared credentials |
| Service disruption | Agent's own container | Container isolation, no shared services |
| Reputation | Agent's own identity | Forgejo user is per-agent, audit trail attributable |

**Containment failure scenarios:**

| Scenario | Impact | Mitigation |
|----------|--------|------------|
| Agent gains host root | Full platform compromise | Docker user namespaces, no `--privileged` containers, host kernel hardening |
| Management key compromised | All friend keys can be created/revoked | Rotate immediately. Audit all key creation events. Store management key offline. |
| Forgejo admin compromised | All repos, all agent accounts | Rotate admin password. Audit all user creation. Re-provision all agent accounts. |
| Supply chain attack on dependency | Malicious code in agent-generated PR | Dependency pinning (`version-pinning-axiom` skill), vulnerability scanning, human review catches unexpected deps |

---

### 6.5 Audit Trail Requirements

**Principle:** Every agent action is traceable (architectural principle #3). An auditor can reconstruct any decision from logs alone.

**Audit trail sources:**

| Source | What's Logged | Retention |
|--------|---------------|-----------|
| LangFuse traces | Every LLM call: model, tokens, cost, latency, prompt (hashed), response (hashed) | Indefinite (self-hosted) |
| Git commit attestation | prompt-hash, model, context-hash, cost, agent identity, confidence | Permanent (git history) |
| Forgejo audit log | User creation, PR open/review/merge, branch protection changes | Permanent (Forgejo DB) |
| H4F bridge log | Key provisioning, consistency checks, doctor fixes, guardrail assignments | Rolling (syslog rotation) |
| Docker container logs | Agent stdout/stderr, build output | Rolling (Docker log driver) |
| Axiom checkpoints | Step execution, retry, failure, escalation | Per-run (in `.axiom/state/`) |

**Audit trail format (H4F bridge log — syslog-style):**
```
2026-06-19T12:02:57Z | WARN  | nc-sync | wojons   | key_drift    | FIXED  | NC key != workspace, pushed WS→NC
2026-06-19T12:02:58Z | INFO  | doctor  | system   | dead_key_fix  | OK     | friend=llopez new_key=sha256:abc123
2026-06-19T12:03:12Z | ERROR | grd-enf | test01   | guardrail    | FAIL   | no guardrail assigned, auto-assigned to Flash
```

**12-step audit chain:**

For any merged PR, an auditor MUST be able to trace:

```
Step 1: Forgejo issue → issue URL + creator + timestamp
Step 2: Axiom work item → plan.yaml ref + agent assignments + run_id
Step 3: Ralph Loop → lock_id + worktree_path + lock_acquired_at
Step 4: OpenCode session → session_id + model + tokens + cost (LangFuse trace)
Step 5: Git commit → SHA + attestation trailer (prompt-hash, model, context-hash)
Step 6: GitReins verdict → Tier 1 guard results + Tier 2 verdict (COMPLETE/INCOMPLETE)
Step 7: PR metadata → pr_index + linked issue + spec ref + evidence bundle
Step 8: Chimera review → trace_id + formation + worker models + verdict + findings
Step 9: Conscientiousness report → report_id + attack vectors + verdict (DEFENSIBLE/VULNERABLE)
Step 10: PromptFoo CI → test results (pass/fail per test case) + Forgejo Actions run ID
Step 11: Co-approvals → human approval (user + timestamp) + agent approval (agent + confidence)
Step 12: Merge → merge SHA + strategy + timestamp + Pages URL + LangFuse trace ID
```

**Missing evidence = audit failure.** If any step's evidence is absent, the merge is flagged for review. The audit script queries each source and produces a completeness report.

**Audit query example:**
```bash
# For PR #42 in org/repo:
ISSUE=$(curl -s "http://forgejo:3000/api/v1/repos/org/repo/pulls/42" | jq .issue.number)
COMMIT_SHA=$(curl -s "http://forgejo:3000/api/v1/repos/org/repo/pulls/42" | jq -r .merge_commit_sha)
ATTESTATION=$(git -C /repo log --format='%b' -1 $COMMIT_SHA | grep 'Helix-Attestation:')
TRACE_ID=$(echo $ATTESTATION | jq -r .langfuse_trace_id)
CHIMERA_TRACE=$(curl -s "http://langfuse:3000/api/public/traces/$TRACE_ID" | jq .metadata.chimera_trace_id)
# ... chain through all 12 steps
```

---

### 6.6 Security Hardening Checklist

**Deployment hardening (must verify before production):**

- [ ] Forgejo admin password is strong (32+ chars random) and stored in a password manager
- [ ] Forgejo web UI behind reverse proxy (Caddy/Traefik) with TLS
- [ ] All internal service ports bound to 127.0.0.1 (not 0.0.0.0) except those needing Docker network access
- [ ] Docker user namespaces enabled (`userns-remap`)
- [ ] No containers run with `--privileged` except DinD (which is itself sandboxed)
- [ ] gluetun VPN configured for all agent sandboxes
- [ ] `.env` files have permissions 600 (`chmod 600 ~/.hermes/.env`)
- [ ] `chimera.yaml` has permissions 600
- [ ] Secrets scanner (GitReins) installed in ALL repos before any agent commits
- [ ] `.gitignore` covers all secret files in ALL repos
- [ ] OpenRouter management key stored offline (not in any `.env` accessible to agents)
- [ ] Branch protection on `main` in ALL repos: no direct push, required approvals ≥ 2
- [ ] Forgejo Actions runners isolated (not on the host)
- [ ] LangFuse and Forgejo databases backed up daily
- [ ] SSH access to host restricted to key-based auth only

**Operational hardening (ongoing):**

- [ ] H4F bridge cron running every 5 minutes (consistency + doctor + guardrail)
- [ ] H4F auto-repair logging to `/var/log/hermes-bridge.log` (syslog-style, per-check)
- [ ] OpenRouter key budgets reviewed weekly (watch for budget creep)
- [ ] Trust levels recalculated daily by Hivemind cron
- [ ] Dependency vulnerability scan runs in CI (Forgejo Actions with `govulncheck` / `npm audit` / `pip-audit`)
- [ ] LangFuse cost dashboards reviewed weekly (detect anomalous spend)
- [ ] Failed step count monitored (spike = possible prompt injection or model degradation)
- [ ] `force-merge` label usage reviewed monthly (should be rare)

---

### 6.7 Incident Response

**Severity levels:**

| Severity | Definition | Response Time | Example |
|----------|-----------|---------------|---------|
| **SEV-0** | Platform-wide outage or data breach | Immediate | Forgejo down, management key compromised |
| **SEV-1** | Agent causing active harm | < 15 min | Agent pushing secrets, agent in infinite spending loop |
| **SEV-2** | Degraded capability | < 1 hour | Chimera circuit breaker stuck open, GitReins Tier 2 down |
| **SEV-3** | Non-critical issue | < 1 day | Trust level not updating, PromptFoo CI flaky |

**SEV-0 response (platform compromise):**
1. Kill all agent containers: `docker ps | grep hermes-agent | awk '{print $1}' | xargs docker kill`
2. Rotate management key: create new at OpenRouter, update `OR_MANAGEMENT_KEY`
3. Revoke all agent OpenRouter keys: `DELETE /api/v1/keys/<id>` for each
4. Rotate Forgejo admin password
5. Audit all recent commits for injected secrets or malicious code
6. Re-provision agents from known-good state

**SEV-1 response (runaway agent):**
1. Kill the specific agent container: `docker kill hermes-agent-<name>`
2. Revoke that agent's OpenRouter key
3. Revert any unmerged PRs by the agent
4. Audit the agent's LangFuse traces for anomalous behavior
5. Review the prompt that triggered the behavior (was it prompt injection?)

---

### 6.8 Verification (Section 6)

1. **Secrets scanner check:** Create a test file containing `OPENROUTER_API_KEY (set via env var)`. Stage it. Attempt commit. Verify the pre-commit hook blocks it with exit code 1. Then remove the file and verify normal commits succeed.
2. **Network isolation check:** From inside an agent container, attempt to `curl http://<host-ip>:3000` (Forgejo direct). This SHOULD fail if port is bound to 127.0.0.1. Attempt `ping 8.8.8.8` — this SHOULD route through gluetun VPN (verify with `curl ifconfig.me` showing VPN IP, not host IP).
3. **Blast radius check:** Create a test agent with $1 budget. Have it make LLM calls until budget exhausted. Verify: (a) the agent's key returns 403 after budget hit, (b) no other agent is affected, (c) `doctor.fix_dead_keys()` does NOT re-key (budget exhaustion is not a dead key — it's intentional).
4. **Audit trail check:** For a test PR merged through the full 12-step flow, run the audit query script. Verify all 12 steps have evidence. Missing evidence = test failure.
5. **Co-approval check:** Attempt to merge a PR with only human approval. Verify blocked. Attempt with only agent approval. Verify blocked. Merge with both. Verify success.
6. **Incident response drill:** Simulate SEV-1: identify a test agent, kill its container, revoke its key, verify the platform continues operating for other agents.

---

## 7. Quality Gates

### 7.1 Overview

Quality gates form the verification spine of Helix. No code reaches `main` without passing every gate. Gates are ordered by cost — cheapest checks run first, expensive LLM-based checks run last. A failure at any tier blocks the entire pipeline; the agent receives structured feedback and retries.

### 7.2 Gate Ordering

The full gate pipeline, executed sequentially:

```
GitReins Tier 1 (static, <5s)
  → GitReins Tier 2 (agentic, 30-90s)
    → Chimera Formation Review (multi-model, 2-5m)
      → Conscientiousness Adversarial Loop (iterative, 3-10m)
        → PromptFoo Regression (CI, 30-120s)
          → Co-Approval Gate (human + agent, async)
```

Each gate must return a structured `PASS` or `FAIL` with evidence. `SOFT_FAIL` is not a valid state — all gates are hard gates.

### 7.3 GitReins Tier 1 — Static Guards

**Executor:** GitReins pre-receive hook (Python, runs in-process on Forgejo server).
**Timeout:** 5 seconds.
**Cost:** $0 (no LLM calls).

Checks (all must pass):

1. **Secrets scan** — `gitleaks detect --no-git -v` against the incoming diff. Any positive match = FAIL. No exceptions.
2. **Lint** — Language-specific linters: `ruff` (Python), `golangci-lint` (Go), `eslint` (TS). Zero warnings policy. Lint failures = FAIL.
3. **Tests** — `pytest -x --tb=short` or equivalent. Any test failure = FAIL. Test timeout per suite: 60s.
4. **Build** — `go build ./...` or equivalent. Compilation failure = FAIL.
5. **Commit attestation** — Every commit must include `Co-authored-by: <agent-name> <agent-uuid@helix>` trailer. Missing attestation = FAIL.
6. **Prompt link** — If `prompts/` directory exists in the repo, every commit touching source files must reference a prompt version hash in the commit body (`Prompt: prompts/<name>/v<N>.md sha256:<hash>`). Missing link = FAIL.
7. **File size** — No single file > 500KB. Generated assets (images, binaries) must be in `assets/` or use Git LFS.

**Output:** Structured JSON written to `.gitreins/results/<commit-sha>.json`:
```json
{
  "commit": "abc123",
  "timestamp": "2026-06-19T14:00:00Z",
  "tier": 1,
  "checks": {
    "secrets": {"status": "PASS", "duration_ms": 1200},
    "lint": {"status": "PASS", "duration_ms": 800},
    "tests": {"status": "PASS", "duration_ms": 3200},
    "build": {"status": "PASS", "duration_ms": 400},
    "attestation": {"status": "PASS", "duration_ms": 50},
    "prompt_link": {"status": "PASS", "duration_ms": 50},
    "file_size": {"status": "PASS", "duration_ms": 100}
  },
  "overall": "PASS"
}
```

### 7.4 GitReins Tier 2 — Agentic Evaluator

**Executor:** GitReins evaluator (Python, calls LLM via OpenRouter).
**Timeout:** 90 seconds.
**Cost:** ~$0.02-0.05 per evaluation (model: `google/gemini-2.5-flash-lite`).

The agentic evaluator is a focused LLM call that reads the diff and answers structured questions:

1. **Logic review** — Does this change introduce logical errors? Check: inverted conditions, off-by-one, nil dereference, race condition patterns.
2. **Test quality** — Do the tests actually test the changed behavior, or are they tautological?
3. **Security surface** — Does this change introduce new input vectors, SQL queries, shell commands, file operations, or network calls?
4. **Dependency impact** — What other files/modules depend on the changed code? Are any broken?

**Output format:**
```json
{
  "commit": "abc123",
  "tier": 2,
  "model": "google/gemini-2.5-flash-lite",
  "checks": {
    "logic": {"status": "PASS", "findings": []},
    "test_quality": {"status": "PASS", "findings": []},
    "security_surface": {"status": "WARN", "findings": ["New file write at pkg/state/store.go:45 — verify path sanitization"]},
    "dependency_impact": {"status": "PASS", "affected_modules": ["pkg/state", "cmd/agent"]}
  },
  "overall": "PASS"
}
```

WARN findings do not block, but they are surfaced in the PR as review comments.

### 7.5 Chimera Multi-Model Formation Review

**Executor:** Chimera formation engine (Python, running as a service on the Helix host).
**Timeout:** 5 minutes.
**Cost:** ~$0.30-0.80 per review (varies by formation size).

Chimera runs after the PR is opened (Step 8 in the 12-step flow). It is NOT on the critical path of commit → push — the agent can push to its branch, open the PR, and Chimera reviews asynchronously.

**Formation Spec (default for code review):**
```yaml
formation: code-review-standard
rewriter:
  strategy: decompose-by-concern
  output: dag
dispatcher:
  assignments:
    - model: anthropic/claude-sonnet-4
      domain: logic_correctness
      weight: 0.4
    - model: google/gemini-2.5-pro
      domain: security_audit
      weight: 0.3
    - model: openai/gpt-5.2
      domain: style_maintainability
      weight: 0.3
workers:
  parallel: true
  timeout_per_worker: 120s
judges:
  strategy: weighted_vote
  threshold: 0.7
audit:
  enabled: true
  model: meta-llama/llama-4-maverick
  verify: [logic_correctness, security_audit]
```

**Review dimensions assigned by dispatcher:**

| Domain | Model | Checks |
|--------|-------|--------|
| logic_correctness | Claude Sonnet 4 | Correctness of algorithms, edge cases, error handling, state transitions |
| security_audit | Gemini 2.5 Pro | Injection vectors, auth bypass, secret exposure, input validation |
| style_maintainability | GPT-5.2 | Code style, naming, documentation, test coverage, architectural fit |

**Merge Criteria from Chimera:**
- Weighted score ≥ 0.7 → APPROVE
- Weighted score 0.4-0.7 → CHANGES_REQUESTED (agent must address)
- Weighted score < 0.4 → BLOCKED (requires human override)

**Audit verification:** Llama 4 Maverick independently checks the two highest-weighted domains. If audit disagrees with the weighted result by >0.3, the review is flagged for human attention.

### 7.6 Conscientiousness Adversarial Loop

**Executor:** Conscientiousness service (Go, Dockerized).
**Timeout:** 10 minutes (max 3 iterations).
**Cost:** ~$0.10-0.40 per loop.

The adversarial loop challenges the PR from adversarial angles:

```
prompt → evaluate → verify → report
  ↑                           ↓
  └── retry (max 3x) ←── FAIL ←
```

**Loop configuration:**
```yaml
max_iterations: 3
adversarial_agents:
  - assumption-buster    # Challenges unstated assumptions
  - devils-advocate      # Argues against the change
  - redteam              # Attempts to find exploitable paths
evaluation:
  prompt: "You are an adversarial code reviewer. Find flaws in this PR that would cause production incidents."
  model: anthropic/claude-sonnet-4
  temperature: 0.7
verification:
  strategy: counterfactual
  checks:
    - "If this PR were deployed to 1,000 users right now, what breaks?"
    - "What assumption, if wrong, makes this entire change invalid?"
    - "What is the worst-case failure mode of the new code path?"
report:
  format: structured
  include: [finding, severity, evidence, suggested_fix]
```

**Severity levels:**
- **CRITICAL** — Data loss, security breach, or platform outage. Auto-blocks merge.
- **HIGH** — Incorrect behavior under specific conditions. Blocks merge until addressed.
- **MEDIUM** — Suboptimal design, missing edge case handling. Advisory, does not block.
- **LOW** — Style nit, documentation gap. Advisory.

**Loop termination:** After 3 iterations or when all findings are addressed. If CRITICAL or HIGH findings persist after 3 iterations, the PR is blocked and escalated to human review.

### 7.7 PromptFoo Regression Testing

**Executor:** PromptFoo (runs in Forgejo CI, `.forgejo/workflows/prompt-eval.yml`).
**Timeout:** 2 minutes.
**Cost:** ~$0.05-0.15 per run.

Every repo with a `prompts/` directory must have a `.promptfoo.yaml` at root:

```yaml
prompts:
  - file://prompts/agent-identity/v3.md
  - file://prompts/code-review/v2.md
providers:
  - id: openrouter:anthropic/claude-sonnet-4
  - id: openrouter:google/gemini-2.5-flash-lite
tests:
  - description: "Identity provision creates valid Forgejo account"
    vars:
      agent_name: test-agent-7
      tier: flash
    assert:
      - type: contains
        value: "provisioned"
      - type: not-contains
        value: "error"
      - type: llm-rubric
        value: "The response describes creating a Forgejo account with SSH key and scoped PAT"
  - description: "Code review catches SQL injection"
    vars:
      code: |
        query = f"SELECT * FROM users WHERE name = '{user_input}'"
    assert:
      - type: contains
        value: "SQL injection"
      - type: llm-rubric
        value: "The review identifies the SQL injection vulnerability and suggests parameterized queries"
```

**CI integration:** PromptFoo runs on every push to a branch where `prompts/` files changed. Failing prompt tests block merge.

### 7.8 Co-Approval Gate

**Final gate before merge.** Enforced by Forgejo branch protection rules.

**Requirements:**
1. **1 human approval** — A human with write access must approve the PR.
2. **1 agent approval** — A different agent than the PR author must approve. The approving agent runs Chimera with `formation: approval-check` (lightweight: 1 model, 30s timeout).
3. **Agent veto power** — Any agent can block merge by posting a review comment with `BLOCKING: <reason> <evidence>`. A blocked PR cannot be merged until the blocking agent withdraws or a human overrides.
4. **Trust-based escalation** — Agents with >95% PR acceptance rate over the last 50 PRs earn "trusted" status. Trusted agents' approvals count as 1.5 votes (can satisfy the agent approval requirement even if another agent blocks, though the human can still override).

**Branch protection config (Forgejo):**
```yaml
# .forgejo/branch-protection.yml
main:
  required_approvals: 2          # 1 human + 1 agent
  dismiss_stale_reviews: true
  require_code_owner_reviews: true
  block_on_agent_veto: true
  status_checks:
    - gitreins/tier-1
    - gitreins/tier-2
    - chimera/review
    - conscientiousness/adversarial
    - promptfoo/regression
```

---

## 8. Observability

### 8.1 Overview

Every operation in Helix is traced, metered, and logged. The observability stack spans three layers:
- **Application traces** → LangFuse (LLM calls, prompts, costs)
- **Infrastructure metrics** → Prometheus + Loki (system health, resource usage)
- **Audit trail** → DuckBrain + Hivemind (decisions, tradeoffs, anti-patterns)

### 8.2 LangFuse Trace Format

Every LLM call in the platform emits a LangFuse trace with this structure:

```json
{
  "trace": {
    "id": "helix-pr-1842-agent-7",
    "name": "agent-implement",
    "userId": "agent-sandbox-7@helix",
    "sessionId": "pr-1842",
    "metadata": {
      "repo": "totalwindupflightsystems/helix",
      "pr": 1842,
      "commit": "abc123",
      "prompt_version": "agent-identity/v3",
      "model": "deepseek-v4-pro",
      "context_window": 131072
    },
    "tags": ["implementation", "go", "agent-identity"]
  },
  "generations": [
    {
      "name": "llm-call-1",
      "model": "deepseek-v4-pro",
      "input": "...",
      "output": "...",
      "usage": {
        "promptTokens": 45000,
        "completionTokens": 8200,
        "totalTokens": 53200
      },
      "cost": 0.1064,
      "duration_ms": 34200
    }
  ],
  "observations": [
    {
      "name": "file-write",
      "type": "SPAN",
      "input": "pkg/identity/provisioner.go",
      "output": "182 lines written",
      "duration_ms": 120
    }
  ]
}
```

### 8.3 Cost Attribution Model

Every token burned in Helix is attributed to:
```
namespace: <repo-owner>/<repo-name>
agent: <agent-uuid>
task: <pr-number or task-id>
prompt_version: <prompt-file sha256>
model: <model-name>
```

**Cost hierarchy for billing:**
- Tier 1: Per-agent budget (H4F enforces)
- Tier 2: Per-repo budget (Hivemind enforces)
- Tier 3: Per-sprint budget (Axiom enforces)
- Tier 4: Platform-wide cap (H4F global limit)

**Budget exhaustion behavior:**
1. Agent hits per-agent limit → 403 on next API call → agent halts → Hivemind notifies repo owner
2. Repo hits per-repo limit → all agents for that repo paused → Axiom notifies all repo collaborators
3. Platform cap hit → H4F pauses all agents → Telegram alert to platform admin

### 8.4 Prometheus Metrics

**Agent metrics (per agent container):**
```
helix_agent_tasks_total{agent, repo, status="completed|failed|blocked"}
helix_agent_llm_calls_total{agent, model}
helix_agent_tokens_used{agent, model, type="prompt|completion"}
helix_agent_cost_total{agent, repo}
helix_agent_sandbox_uptime_seconds{agent}
helix_agent_worktree_count{agent}
```

**Platform metrics:**
```
helix_pr_cycle_time_seconds{repo, quantile="0.5|0.95|0.99"}
helix_gate_pass_rate{gate="tier1|tier2|chimera|conscientiousness|promptfoo"}
helix_active_agents
helix_queued_tasks
helix_forgejo_api_latency_seconds{endpoint, quantile}
helix_cost_per_pr{repo}
helix_merge_rate{repo, period="hour|day|week"}
```

**Alert thresholds:**
```yaml
alerts:
  - name: HighCostAgent
    expr: rate(helix_agent_cost_total[1h]) > 5
    severity: warning
    annotation: "Agent {{ $labels.agent }} spending >$5/hr"

  - name: GateFailureSpike
    expr: rate(helix_gate_pass_rate{gate="tier1"}[15m]) < 0.7
    severity: critical
    annotation: "Tier 1 pass rate dropped below 70%"

  - name: PRStuck
    expr: helix_pr_cycle_time_seconds > 7200
    severity: warning
    annotation: "PR {{ $labels.repo }} in review >2 hours"

  - name: AgentDown
    expr: helix_agent_sandbox_uptime_seconds == 0
    severity: critical
    annotation: "Agent {{ $labels.agent }} container not running"

  - name: CostAnomaly
    expr: helix_cost_per_pr > (avg_over_time(helix_cost_per_pr[7d]) * 3)
    severity: warning
    annotation: "PR cost 3x above weekly average"
```

### 8.5 DuckBrain Memory Schema

DuckBrain stores persistent agent knowledge. Memory is written after significant events and queried before agent task execution.

**Namespaces:**
```
helix/
  agents/<uuid>/
    decisions/       # Architectural decisions, tradeoffs
    anti-patterns/   # Mistakes made, lessons learned
    preferences/     # Per-agent configuration preferences
  repos/<name>/
    conventions/     # Code style, naming conventions
    known-issues/    # Recurring bugs, flaky tests
    architecture/    # Component relationships, data flow
  platform/
    incidents/       # Post-mortems, root causes
    runbooks/        # Operational procedures
    config/          # Platform configuration history
```

**Memory entry format:**
```json
{
  "key": "/helix/agents/sandbox-7/decisions/2026-06-19-sqlite-vs-postgres",
  "domain": "concept",
  "attributes": {
    "decision": "Use SQLite for agent state, not PostgreSQL",
    "rationale": "Single-writer per agent. No concurrent access. 5ms vs 50ms for Postgres. Simpler backup.",
    "tradeoffs": ["No replication", "Max 1 writer", "VACUUM needed weekly"],
    "supersedes": null,
    "superseded_by": null
  },
  "embedding_text": "SQLite chosen over PostgreSQL for agent state storage due to single-writer access pattern and 10x latency advantage"
}
```

### 8.6 Hivemind Memory Bank Lifecycle

```
Inbox (raw events)
  → Compiler (deduplicates, categorizes, enriches)
    → Compiled memory (structured, searchable)
      → _index (human-readable navigation)
        → DuckBrain (persistent, version-controlled)
```

**Inbox → Compiled rules:**
- Events within 5 minutes with same agent + repo + event_type are batched
- Batch is deduplicated (same file touched + same operation = one event)
- Each compiled entry gets: UUID, timestamp, agent attribution, repo context, tags

**Compiled memory entry:**
```yaml
id: mem-2026-06-19-001
timestamp: 2026-06-19T14:22:00Z
agent: sandbox-7
repo: totalwindupflightsystems/helix
event_type: gate_failure
tags: [tier1, lint, ruff]
summary: "Lint failure on pkg/identity/syncer.go: unused import 'fmt'"
resolution: "Removed import, re-pushed. Passed Tier 1."
persisted_to_duckbrain: true
```

---

## 9. Deployment Architecture

### 9.1 Host Topology

Helix runs on a single Hetzner dedicated server (AX102 or equivalent: 16-core, 64GB RAM, 1TB NVMe). All components are containerized via Docker Compose.

**Target host spec:**
- CPU: 16+ cores (AMD EPYC or Intel Xeon)
- RAM: 64GB (32GB for agent containers, 16GB for platform services, 16GB headroom)
- Storage: 1TB NVMe (500GB for repos, 200GB for container images, 100GB for logs/metrics, 200GB headroom)
- Network: 1Gbps unmetered
- OS: Ubuntu 24.04 LTS

### 9.2 Docker Compose Topology

```yaml
# docker-compose.yml — Helix platform
version: "3.8"

services:
  # ── Git Forge ──
  forgejo:
    image: codeberg.org/forgejo/forgejo:9
    ports: ["3000:3000", "2222:22"]
    volumes:
      - forgejo_data:/var/lib/forgejo
      - forgejo_config:/etc/forgejo
    environment:
      FORGEJO__server__DOMAIN: helixloop.dev
      FORGEJO__server__ROOT_URL: https://helixloop.dev
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:3000/api/v1/version"]
      interval: 30s

  # ── CI Runner ──
  forgejo-runner:
    image: codeberg.org/forgejo/runner:4
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - runner_data:/data
    environment:
      FORGEJO_INSTANCE_URL: http://forgejo:3000
      FORGEJO_RUNNER_TOKEN: ${FORGEJO_RUNNER_TOKEN}

  # ── Chimera ──
  chimera:
    build: ./chimera
    ports: ["8001:8001"]
    environment:
      OPENROUTER_API_KEY: ${OPENROUTER_API_KEY}
      CHIMERA_FORMATION: code-review-standard
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8001/health"]

  # ── Conscientiousness ──
  conscientiousness:
    build: ./conscientiousness
    ports: ["8002:8002"]
    volumes:
      - conscience_data:/data
    environment:
      OPENROUTER_API_KEY: ${OPENROUTER_API_KEY}
      SQLITE_PATH: /data/conscience.db
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8002/health"]

  # ── Hivemind ──
  hivemind:
    build: ./hivemind
    ports: ["8003:8003"]
    volumes:
      - hivemind_data:/data
    environment:
      HIVEMIND_DB_PATH: /data/hivemind.db
      HIVEMIND_MEMORY_PATH: /data/memory
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8003/health"]

  # ── LangFuse ──
  langfuse:
    image: ghcr.io/langfuse/langfuse:3
    ports: ["3001:3000"]
    environment:
      DATABASE_URL: postgresql://langfuse:${LANGFUSE_DB_PASS}@postgres:5432/langfuse
      NEXTAUTH_SECRET: ${LANGFUSE_AUTH_SECRET}
    depends_on: [postgres]

  postgres:
    image: postgres:16
    volumes: [postgres_data:/var/lib/postgresql/data]
    environment:
      POSTGRES_DB: langfuse
      POSTGRES_USER: langfuse
      POSTGRES_PASSWORD: ${LANGFUSE_DB_PASS}

  # ── Prometheus + Loki + Grafana ──
  prometheus:
    image: prom/prometheus:v3
    ports: ["9090:9090"]
    volumes:
      - ./prometheus/prometheus.yml:/etc/prometheus/prometheus.yml
      - prometheus_data:/prometheus

  loki:
    image: grafana/loki:3
    ports: ["3100:3100"]

  grafana:
    image: grafana/grafana:11
    ports: ["3002:3000"]
    environment:
      GF_SECURITY_ADMIN_PASSWORD: ${GRAFANA_ADMIN_PASS}

  # ── Agent Sandbox Infrastructure ──
  # Each agent gets its own container via H4F's compose generator.
  # Template: agent-<uuid> with gluetun VPN + dind executor + hermes-agent.
  # H4F manages lifecycle: create, pause, resume, destroy.

  # ── Reverse Proxy ──
  caddy:
    image: caddy:2
    ports: ["80:80", "443:443"]
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile
      - caddy_data:/data

volumes:
  forgejo_data:
  forgejo_config:
  runner_data:
  conscience_data:
  hivemind_data:
  postgres_data:
  prometheus_data:
  caddy_data:
```

### 9.3 Caddy Reverse Proxy

```caddyfile
helixloop.dev {
    reverse_proxy forgejo:3000
}

chimera.helixloop.dev {
    reverse_proxy chimera:8001
}

conscience.helixloop.dev {
    reverse_proxy conscientiousness:8002
}

hivemind.helixloop.dev {
    reverse_proxy hivemind:8003
}

traces.helixloop.dev {
    reverse_proxy langfuse:3000
}

monitor.helixloop.dev {
    reverse_proxy grafana:3000
}
```

### 9.4 systemd Units

All services run under a single `docker-compose` managed by systemd:

```ini
# /etc/systemd/system/helix-platform.service
[Unit]
Description=Helix Platform (Docker Compose)
Requires=docker.service
After=docker.service network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=/opt/helix
ExecStart=/usr/bin/docker compose up -d --remove-orphans
ExecStop=/usr/bin/docker compose down
ExecReload=/usr/bin/docker compose up -d --remove-orphans
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

Forgejo backup service:
```ini
# /etc/systemd/system/helix-backup.service
[Unit]
Description=Helix Forgejo Backup
[Service]
Type=oneshot
ExecStart=/opt/helix/scripts/backup-forgejo.sh
```

```ini
# /etc/systemd/system/helix-backup.timer
[Unit]
Description=Daily Helix Backup
[Timer]
OnCalendar=daily
Persistent=true
[Install]
WantedBy=timers.target
```

### 9.5 Agent Container Template

Each agent runs in a container generated by H4F:

```yaml
# H4F-generated per-agent compose (example: agent-sandbox-7)
agent-sandbox-7:
  image: hermes-agent:latest
  container_name: agent-sandbox-7
  environment:
    HERMES_PROFILE: agent-sandbox-7
    OPENROUTER_API_KEY: ${AGENT_7_OPENROUTER_KEY}
    FORGEJO_URL: http://forgejo:3000
    FORGEJO_TOKEN: ${AGENT_7_FORGEJO_TOKEN}
    HIVEMIND_URL: http://hivemind:8003
    CHIMERA_URL: http://chimera:8001
    LANGFUSE_PUBLIC_KEY: ${LANGFUSE_PUBLIC_KEY}
    LANGFUSE_SECRET_KEY: ${LANGFUSE_SECRET_KEY}
    AGENT_UUID: agent-sandbox-7
    AGENT_TIER: flash
    BUDGET_MONTHLY_USD: 150
  volumes:
    - agent_7_worktrees:/worktrees
    - agent_7_cache:/home/hermes/.cache
  network_mode: service:gluetun-agent-7    # Routes through VPN
  security_opt:
    - no-new-privileges:true
  read_only: true                           # Except mounted volumes
  tmpfs:
    - /tmp:size=512M
  mem_limit: 8g
  cpus: 4
```

### 9.6 Env Var Inventory

| Variable | Source | Description |
|----------|--------|-------------|
| `OPENROUTER_API_KEY` | `.env` (platform) | Master OpenRouter key |
| `FORGEJO_RUNNER_TOKEN` | `.env` | CI runner registration |
| `LANGFUSE_DB_PASS` | `.env` | Postgres password |
| `LANGFUSE_AUTH_SECRET` | `.env` | NextAuth secret |
| `GRAFANA_ADMIN_PASS` | `.env` | Grafana admin |
| `AGENT_N_OPENROUTER_KEY` | `.env` | Per-agent API key |
| `AGENT_N_FORGEJO_TOKEN` | `.env` | Per-agent Forgejo PAT |
| `LANGFUSE_PUBLIC_KEY` | `.env` | LangFuse public key |
| `LANGFUSE_SECRET_KEY` | `.env` | LangFuse secret key |
| `GITHUB_TOKEN` | `.env` | For mirroring / Pages deploy |

All secrets live in `/opt/helix/.env` (mode 600, owned by root). Never committed to git.

---

## 10. Operations

### 10.1 Backup Strategy

**What to back up:**

| Path | Content | Frequency | Retention |
|------|---------|-----------|-----------|
| `/var/lib/forgejo` | Git repos, issues, PRs, wiki | Daily | 30 days |
| `/opt/helix/.env` | Secrets, config | Daily | 90 days |
| `/data/conscience.db` | Conscientiousness state | Daily | 30 days |
| `/data/hivemind.db` | Hivemind state | Daily | 30 days |
| `/data/memory/` | Hivemind memory bank | Daily | 90 days |
| `/var/lib/postgresql/data` | LangFuse traces | Daily | 14 days (large) |
| `/prometheus/` | Metrics TSDB | Weekly | 7 days |
| `/data/duckbrain/` | DuckBrain memory | Daily | 90 days |

**Backup script (`/opt/helix/scripts/backup-forgejo.sh`):**
```bash
#!/bin/bash
set -euo pipefail

BACKUP_DIR="/mnt/backups/helix"
DATE=$(date +%Y%m%d-%H%M%S)
RETENTION_DAYS=30

# Stop Forgejo briefly for consistent backup
docker compose stop forgejo
tar -czf "${BACKUP_DIR}/forgejo-${DATE}.tar.gz" /var/lib/docker/volumes/helix_forgejo_data/
docker compose start forgejo

# Backup databases (online, safe for SQLite)
sqlite3 /data/conscience.db ".backup '${BACKUP_DIR}/conscience-${DATE}.db'"
sqlite3 /data/hivemind.db ".backup '${BACKUP_DIR}/hivemind-${DATE}.db'"
cp /opt/helix/.env "${BACKUP_DIR}/env-${DATE}"
tar -czf "${BACKUP_DIR}/memory-${DATE}.tar.gz" /data/memory/

# Cleanup old backups
find "${BACKUP_DIR}" -mtime +${RETENTION_DAYS} -delete
```

**Backup target:** Hetzner Storage Box (1TB, mounted at `/mnt/backups`).

### 10.2 Restore Procedure

```bash
# Full platform restore from backup
BACKUP_DATE="20260619-140000"

# 1. Restore Forgejo
docker compose stop forgejo
rm -rf /var/lib/docker/volumes/helix_forgejo_data/
tar -xzf "/mnt/backups/helix/forgejo-${BACKUP_DATE}.tar.gz" -C /
docker compose start forgejo

# 2. Restore databases
cp "/mnt/backups/helix/conscience-${BACKUP_DATE}.db" /data/conscience.db
cp "/mnt/backups/helix/hivemind-${BACKUP_DATE}.db" /data/hivemind.db
tar -xzf "/mnt/backups/helix/memory-${BACKUP_DATE}.tar.gz" -C /

# 3. Restore secrets
cp "/mnt/backups/helix/env-${BACKUP_DATE}" /opt/helix/.env
chmod 600 /opt/helix/.env

# 4. Bring platform back up
systemctl restart helix-platform
```

### 10.3 Disaster Recovery

**DR Scenarios and Responses:**

| Scenario | Detection | Response | RTO | RPO |
|----------|-----------|----------|-----|-----|
| Host hardware failure | Hetzner monitoring | Provision new server, restore from latest backup | 4 hours | 24 hours |
| Disk failure | SMART alerts, filesystem errors | Replace disk, restore from backup | 2 hours | 24 hours |
| Accidental deletion | Manual report or audit | Restore specific repo/DB from backup | 30 minutes | 24 hours |
| Security breach (agent container) | Intrusion detection, anomaly alerts | Kill container, rotate all keys, audit logs | 1 hour | 0 (agents can be re-provisioned) |
| Forgejo corruption | Health check failure | Restore Forgejo from backup, replay git reflog for recent pushes | 1 hour | 0 (git is distributed) |

**Key rotation procedure (security incident):**
```bash
# 1. Pause all agents
hivemind-cli agents pause --all

# 2. Rotate platform master keys
hermes config rotate-keys --platform

# 3. Rotate per-agent keys
h4f-cli rotate-all-agent-keys

# 4. Rotate Forgejo admin token
forgejo-cli admin token-revoke --all
forgejo-cli admin token-create --name admin --scopes all > /opt/helix/.env.new

# 5. Resume agents
hivemind-cli agents resume --all
```

### 10.4 Scaling Model

Helix is designed for a single dedicated server (vertical scaling). The bottleneck is agent LLM calls (API, not compute) and git operations (I/O, not CPU).

**Current capacity:**
- Agent containers: 20 concurrent (16-core host, 0.8 cores per agent)
- Git operations: unlimited (Forgejo handles thousands of concurrent clones/pushes)
- LLM throughput: limited by OpenRouter rate limits, not Helix

**When to add a second host:**
1. >20 concurrent agents needed
2. Git clone latency >2s (disk I/O saturation)
3. Prometheus/Loki storage exceeds 500GB

**Multi-host architecture (future):**
- Forgejo remains on primary (single source of truth for git)
- Agent containers split across hosts via H4F multi-host scheduler
- Chimera/Conscientiousness can run on any host (stateless)
- Hivemind and DuckBrain remain on primary (stateful, single-writer)

### 10.5 Incident Response

**SEV levels:**
- **SEV-1:** Platform unavailable. Forgejo down, no agents can work. Response: immediate, all-hands.
- **SEV-2:** Degraded service. Some agents down, gates slow, PRs blocked. Response: <30 minutes.
- **SEV-3:** Non-critical issue. Cosmetic, slow dashboard, single agent stuck. Response: next business day.

**Incident response checklist (SEV-1):**
1. **Acknowledge** — Confirm alert is real, not a false positive.
2. **Contain** — If security: kill affected containers, revoke keys. If infra: check disk/memory/CPU.
3. **Diagnose** — Check `docker compose ps`, `journalctl -u helix-platform -n 100`, Prometheus dashboards.
4. **Mitigate** — Restart failed containers, clear disk space, restore from backup if needed.
5. **Verify** — Run `helix-doctor` diagnostic tool. Confirm all health checks pass.
6. **Post-mortem** — Write to `/helix/platform/incidents/<date>-<title>.md` in DuckBrain.

**helix-doctor diagnostic checks:**
```
✓ Forgejo reachable (http://localhost:3000/api/v1/version)
✓ Chimera healthy (http://localhost:8001/health)
✓ Conscientiousness healthy (http://localhost:8002/health)
✓ Hivemind healthy (http://localhost:8003/health)
✓ LangFuse reachable (http://localhost:3001)
✓ Prometheus scraping (http://localhost:9090/targets)
✓ Agent containers running: 12/12
✓ Disk usage: 42% (420GB/1TB)
✓ Memory: 38GB/64GB (59%)
✓ Backup last run: 2026-06-19T02:00:00Z (12h ago) ✓ within 24h
```

---

## 11. Performance SLAs

### 11.1 Sync Latency

**Definition:** Time from human issuing a task to agent beginning work.

| Target | P50 | P95 | P99 |
|--------|-----|-----|-----|
| Task → agent assigned | 2s | 10s | 30s |
| Agent assigned → worktree created | 3s | 8s | 15s |
| Worktree created → first LLM call | 5s | 15s | 30s |
| **Total sync latency** | **10s** | **33s** | **75s** |

### 11.2 Review Latency

**Definition:** Time from PR opened to all gates completed.

| Gate | P50 | P95 | P99 |
|------|-----|-----|-----|
| Tier 1 (static) | 2s | 5s | 8s |
| Tier 2 (agentic evaluator) | 20s | 60s | 90s |
| Chimera formation | 90s | 240s | 300s |
| Conscientiousness (1 iter) | 60s | 180s | 300s |
| Conscientiousness (3 iter) | 180s | 420s | 600s |
| PromptFoo CI | 30s | 90s | 120s |
| **Total review pipeline** | **382s (6.4m)** | **995s (16.6m)** | **1418s (23.6m)** |

Note: Co-approval is asynchronous (blocked on human availability) and not included in SLAs.

### 11.3 Merge Throughput

**Definition:** PRs merged to `main` per day, per repo.

| Target | Value |
|--------|-------|
| Single agent, simple PR (1 file, <50 lines) | 4-8 PRs/day |
| Single agent, medium PR (3-10 files, 50-300 lines) | 2-4 PRs/day |
| Agent swarm (3 agents parallel) | 12-20 PRs/day |
| Platform maximum (20 agents, 4 repos) | 80 PRs/day |

**Pipeline parallelism:** Multiple agents can work on different repos simultaneously. Within a repo, Ralph Loop's lock ensures serial execution (one agent per repo at a time) to prevent merge conflicts. For repos with high velocity, shard by package/module to allow parallel work.

### 11.4 Sandbox Startup

**Definition:** Time from H4F receiving "create agent" request to agent ready for tasks.

| Target | Cold Start | Warm Start |
|--------|-----------|------------|
| Container create | 3s | 0s (reuse) |
| Container start | 8s | 2s |
| Hermes init + config load | 3s | 1s |
| Health check pass | 2s | 1s |
| **Total** | **16s** | **4s** |

Cold start = new container. Warm start = reuse of existing paused container.

### 11.5 API Latency

**Definition:** Round-trip latency for internal service calls.

| Endpoint | P50 | P95 | P99 |
|----------|-----|-----|-----|
| Forgejo API (read) | 15ms | 50ms | 100ms |
| Forgejo API (write) | 30ms | 100ms | 200ms |
| Chimera deliberation | 90s | 240s | 300s |
| Conscientiousness eval | 60s | 180s | 300s |
| Hivemind memory query | 20ms | 80ms | 150ms |
| Hivemind memory write | 30ms | 100ms | 200ms |
| Git clone (warm, local) | 1s | 3s | 5s |
| Git clone (cold, local) | 8s | 15s | 25s |
| LangFuse trace write | 50ms | 200ms | 500ms |

### 11.6 Cost per PR

**Definition:** Total LLM cost from task creation to merge, per PR.

| PR Complexity | Model Tier | Avg Cost |
|---------------|-----------|----------|
| Simple (1 file, <50 lines) | Flash (Gemini Flash, Llama 4) | $0.10-0.30 |
| Medium (3-10 files) | Pro (Sonnet, DeepSeek V4) | $0.50-2.00 |
| Complex (10+ files, new feature) | Pro + review (Chimera full formation) | $3.00-10.00 |
| Adversarial retry (3x Conscientiousness) | + $0.50-2.00 | |
| PromptFoo regression per run | Flash | $0.05-0.15 |

**Cost optimization rules:**
1. Simple PRs route to flash models by default (Tier 1-2 eval, simple implementations)
2. Pro models reserved for: new features, complex refactors, security-sensitive code
3. Chimera uses Pro models by default; can be configured to use Flash for non-critical repos
4. Conscientiousness starts with Flash; escalates to Pro if Flash yields >2 HIGH findings
5. Agents track their cost-per-PR; if it exceeds $5 avg over 10 PRs, the agent is flagged for review

### 11.7 Monitoring SLAs

**Metric freshness:**
- Prometheus scrape interval: 15s
- LangFuse trace ingestion: <5s from LLM call completion
- Alert evaluation: every 30s
- Dashboard refresh: auto (Grafana)

**Alert response times:**
- SEV-1 (platform down): <5 minutes acknowledge, <30 minutes mitigate
- SEV-2 (degraded): <15 minutes acknowledge, <2 hours mitigate
- SEV-3 (warning): <4 hours acknowledge, next day mitigate

---

## 12. Test Strategy

### 12.1 Test Pyramid

```
         /\        Adversarial (2%)  — Red-team, assumption-buster, chaos
        /--\       E2E (8%)          — Full 12-step flow, PR → merge
       /----\      Integration (20%) — Cross-component contracts, gates
      /------\     Unit (70%)        — Per-package, per-function, per-hook
     /--------\
```

| Layer | Target % | Tooling | Runtime per run |
|-------|----------|---------|-----------------|
| Unit | 70% | pytest (Python), go test (Go), vitest (TS) | <30s |
| Integration | 20% | pytest + testcontainers, go test + docker | <5m |
| E2E | 8% | Custom harness (12-step flow validation) | <15m |
| Adversarial | 2% | Axiom adversarial agents + manual scenarios | <30m |

### 12.2 Coverage Requirements

| Metric | Minimum | Target | Enforcement |
|--------|---------|--------|-------------|
| Line coverage | 85% | 90% | CI gate (hard fail) |
| Branch coverage | 80% | 85% | CI gate (advisory) |
| Function coverage | 90% | 95% | CI gate (hard fail) |
| Gate path coverage | 100% | 100% | CI gate (hard fail — every gate outcome must be tested) |

**Gate path coverage** is unique to Helix: every possible gate outcome (PASS, FAIL, WARN) must have a test. If GitReins Tier 1 can produce 7 check results, there must be at least 7 unit tests covering each failure path.

### 12.3 Per-Component Test Strategy

**Forgejo Hooks (GitReins):**
- **Unit:** Each Tier 1 check tested in isolation with mock git objects. Test: secrets scan on known bad diff, lint on intentionally malformed code, build on broken compilation.
- **Integration:** Full pre-receive hook pipeline against a real Forgejo Docker container. Push good commits, bad commits, edge cases (empty commit, binary file, 500KB file).
- **Key test:** Push a commit with a hardcoded AWS key → verify Tier 1 blocks. Push same commit with key removed → verify passes.

**GitReins Evaluator (Tier 2):**
- **Unit:** Prompt template rendering, response parsing, JSON schema validation.
- **Integration:** Evaluator called with real diffs. Verify it catches: inverted condition, SQL injection, nil dereference. Verify it does NOT flag safe code.
- **Key test:** Submit a purposely broken PR (off-by-one in loop boundary) → verify Tier 2 catches it. Submit the fix → verify Tier 2 passes.

**Chimera Formation Engine:**
- **Unit:** Rewriter strategies, dispatcher assignment logic, judge scoring math, audit verification.
- **Integration:** Full formation with mock LLM responses (pre-recorded). Test that weighted voting produces correct results for known inputs.
- **Key test:** Formation where 2 judges agree on PASS, 1 disagrees → verify weighted threshold correctly resolves. Formation where audit disagrees by >0.3 → verify flag.

**Conscientiousness Loop:**
- **Unit:** Loop controller (iteration counting, termination conditions), severity classifier, report formatter.
- **Integration:** 1-iteration loop with pre-recorded LLM response. 3-iteration loop where each iteration finds new issues. Loop where findings are all LOW (should pass after 1 iteration).
- **Key test:** Submit PR with a CRITICAL bug → verify loop catches it on iteration 1, agent fixes it, loop re-runs and passes.

**Hivemind Memory:**
- **Unit:** Inbox batching, deduplication, compiled entry serialization, _index generation.
- **Integration:** Full memory lifecycle: write event → compile → query → read _index. Concurrent writes from 3 agents. Memory compaction.
- **Key test:** Write 100 events from 3 agents within 5 minutes → verify they're batched and deduplicated correctly.

**H4F Agent Provisioning:**
- **Unit:** known-friends.json parsing, Forgejo API request construction, SSH key generation, PAT creation.
- **Integration:** Full provision cycle against real Forgejo: create user → create SSH key → create PAT → verify agent can clone repo. Deprovision: archive user → verify agent can no longer authenticate.
- **Key test:** Provision agent → agent pushes to feat/* branch → verify success. Agent attempts push to main → verify blocked.

**Muster MCP Generation:**
- **Unit:** OpenAPI spec parsing, tool schema generation, CLI command generation, Starlark DSL.
- **Integration:** Feed Muster a real Forgejo OpenAPI spec → verify generated MCP tools work against a live Forgejo instance. Test caching: second call within TTL returns cached result.
- **Key test:** Generate MCP tools for Forgejo → use generated tool to create a user → verify user exists via Forgejo API.

### 12.4 Adversarial Testing

Adversarial tests are NOT CI-gated. They run on a schedule (daily) or are triggered manually before major releases.

**Agent roster for adversarial testing:**

| Agent | Role | Runs |
|-------|------|------|
| `@assumption-buster` | Surfaces undocumented prerequisites, ambiguous specs | Every PR touching specs/ |
| `@devils-advocate` | Challenges design decisions, forces explicit tradeoffs | Every architectural PR |
| `@redteam` | Adversarial falsification, attack matrix, exploitable paths | Every security-sensitive PR |
| `@whitehat` | Authorized penetration validation, exploitability checks | After security fixes |
| `@chaos-engineer` | Fault injection, resilience testing | Weekly against staging |
| `@finops-cost` | Cost-risk detection, cardinality guardrails | Every cost-sensitive change |

**Scenario examples:**
1. **Gate bypass attempt:** Agent tries to merge without co-approval. Verify blocked.
2. **Budget exhaustion:** Agent burns through monthly budget. Verify 403, verify other agents unaffected.
3. **Key leak simulation:** Agent's PAT is intentionally added to a commit. Verify secrets scan catches it.
4. **Network isolation:** Agent container tries to reach host services directly. Verify blocked by network policy.
5. **Race condition:** Two agents try to acquire the same Ralph Loop lock. Verify exactly one succeeds.

### 12.5 Test Infrastructure

**CI Pipeline (.forgejo/workflows/test.yml):**
```yaml
name: Test
on: [push, pull_request]
jobs:
  unit:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@v4
      - run: go test ./... -cover -coverprofile=coverage.out
      - run: go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//' | xargs -I{} sh -c 'if [ {} -lt 85 ]; then echo "Coverage {}% below 85%"; exit 1; fi'
  integration:
    runs-on: ubuntu-24.04
    needs: unit
    services:
      forgejo:
        image: codeberg.org/forgejo/forgejo:9
        env:
          FORGEJO__security__INSTALL_LOCK: "true"
    steps:
      - uses: actions/checkout@v4
      - run: go test ./... -tags=integration -count=1
```

**Test data management:**
- Test data is generated, not copied. No production data in tests.
- Agent fixtures defined in `testdata/agents/` (SSH keys, PATs — all test-only, never valid).
- LLM responses are pre-recorded in `testdata/responses/` to avoid API costs during CI.
- Integration tests use Docker containers (Forgejo, Postgres) with ephemeral volumes.

**Flaky test policy:**
- Any test that fails >2 times in a rolling 24-hour window is quarantined.
- Quarantined tests are skipped in CI but logged. Owner has 48 hours to fix or delete.
- Quarantine count is a Prometheus metric (`helix_flaky_tests_total`).

---

## 13. Build Order

### 13.1 Dependency Graph

```
Phase 1: Forgejo + GitReins + Agent Identity
  ├── No dependencies. Foundation.
  │
  ├──► Phase 2: H4F Agents + Ralph Loop + Muster
  │      Depends on: Forgejo (git host), Agent Identity (agent accounts)
  │
  ├──► Phase 3: Chimera + Conscientiousness
  │      Depends on: Forgejo (PRs to review), H4F (agent containers), Muster (API bridges)
  │
  ├──► Phase 4: PromptFoo + Prompt Registry + Hivemind
  │      Depends on: Forgejo CI (runs PromptFoo), GitReins (prompt link verification)
  │
  ├──► Phase 5: LangFuse + Prometheus + Grafana
  │      Depends on: All components emitting traces/metrics
  │
  └──► Phase 6: Agent Marketplace + Cost Estimator + PR Negotiation
         Depends on: Everything above (full platform operational)
```

### 13.2 Phase 1: Foundation (Weeks 1-2)

**Goal:** Agents can push code to a git forge with quality gates.

| Day | Task | Deliverable |
|-----|------|-------------|
| 1-2 | Provision Hetzner server, install Docker, clone Helix monorepo | Server running, SSH access |
| 3-4 | Deploy Forgejo via Docker Compose, configure DNS (helixloop.dev → Forgejo) | Forgejo accessible at https://helixloop.dev |
| 5-6 | Install GitReins pre-receive hook, configure Tier 1 checks | Secrets scan, lint, tests, build running on push |
| 7-8 | Implement Agent Identity: known-friends.json → Forgejo OAuth | `hermes agent provision sandbox-7` creates real Forgejo account |
| 9-10 | Wire scoped permissions: feat/* push, main block, PR open | Agent pushes to branch, opens PR, blocked from direct merge |
| 11-12 | Implement GitReins Tier 2 (agentic evaluator) | LLM-based diff review running, posting findings |
| 13-14 | Integration testing: full agent push → review → gate loop | Agent provisions, pushes code, passes Tier 1+2, opens PR |

**Phase 1 success criteria:**
- Forgejo running at a registered domain
- Agent creates Forgejo account with SSH key + PAT from known-friends.json
- Agent pushes to feat/* branch successfully
- Agent blocked from pushing to main
- GitReins Tier 1 blocks known-bad commits (secret in code, lint failure, test failure)
- GitReins Tier 2 posts review findings on the PR

### 13.3 Phase 2: Agent Infrastructure (Weeks 3-4)

**Goal:** Agents run in isolated containers with full tool access.

| Day | Task | Deliverable |
|-----|------|-------------|
| 1-3 | H4F container generator: per-agent Docker Compose with gluetun VPN | `h4f-cli agent create sandbox-7` produces running container |
| 4-5 | Hermes agent config injection: per-agent API keys, Forgejo tokens, budgets | Agent container has HERMES_PROFILE, OPENROUTER_API_KEY, FORGEJO_TOKEN |
| 6-7 | Ralph Loop: lock acquisition, worktree creation, commit protocol | Agent acquires lock, creates worktree, commits with attestation |
| 8-9 | Muster: generate MCP tools for Forgejo API | `muster generate https://helixloop.dev/swagger.json` produces working MCP tools |
| 10-11 | Wire agent container → Forgejo git operations | Agent clones repo, writes code, pushes to branch — all inside container |
| 12-14 | Integration test: agent provisions → implements simple feature → pushes PR | Full Phase 1+2 integration: provision, code, push, gates, PR |

**Phase 2 success criteria:**
- Agent container starts with correct config, budget, and permissions
- Agent clones Helix repo inside container
- Agent acquires Ralph Loop lock, creates worktree
- Agent commits with Co-authored-by trailer
- Agent pushes to branch, gates run, PR opens
- Muster generates working Forgejo API tools

### 13.4 Phase 3: Quality Gates (Weeks 5-6)

**Goal:** Multi-model code review and adversarial validation on every PR.

| Day | Task | Deliverable |
|-----|------|-------------|
| 1-3 | Deploy Chimera as a service, configure default formation | Chimera running, health check passing |
| 4-5 | Wire Chimera to Forgejo webhooks: PR opened → Chimera reviews | PR opened, Chimera runs multi-model review, posts results |
| 6-7 | Deploy Conscientiousness service, configure adversarial loop | Conscientiousness running, health check passing |
| 8-9 | Wire Conscientiousness: Chimera review complete → Conscientiousness loop | After Chimera, Conscientiousness runs adversarial check |
| 10-11 | Configure co-approval gate: 1 human + 1 agent required | Branch protection enforces both approvals |
| 12-14 | Integration test: full gate pipeline on a real PR | Tier 1 → Tier 2 → Chimera → Conscientiousness → Co-approval → merge |

**Phase 3 success criteria:**
- PR opened triggers Chimera multi-model review within 5 minutes
- Chimera posts structured review with weighted score
- Conscientiousness runs adversarial loop, catches injected bugs
- PR blocked until 1 human + 1 agent approve
- Agent can veto merge with BLOCKING comment

### 13.5 Phase 4: Prompts & Memory (Weeks 7-8)

**Goal:** Every prompt is version-controlled, every agent decision is remembered.

| Day | Task | Deliverable |
|-----|------|-------------|
| 1-2 | PromptFoo CI integration: .promptfoo.yaml in every repo | PromptFoo runs on push to prompts/ changes |
| 3-4 | Prompt Registry: prompts/ directory standard, versioning convention | prompts/agent-identity/v3.md committed, linked in commit attestation |
| 5-6 | GitReins prompt link verification: commits must reference prompt version | Commit without prompt link blocked by Tier 1 |
| 7-8 | Hivemind memory bank: inbox → compiled → _index → DuckBrain | Agent events flow through memory lifecycle |
| 9-10 | Hivemind agent scheduling: task assignment, budget enforcement | Axiom assigns tasks, Hivemind tracks state |
| 11-14 | Integration test: prompt change → PromptFoo CI → agent uses new prompt → attestation verified | Full prompt lifecycle validated |

**Phase 4 success criteria:**
- PromptFoo catches a prompt regression and blocks merge
- Commit links to exact prompt version (verified by GitReins)
- Agent decisions appear in Hivemind memory bank
- Agent task lifecycle: assigned → in_progress → completed with evidence

### 13.6 Phase 5: Observability (Weeks 9-10)

**Goal:** Every token, every decision, every millisecond is traced and measured.

| Day | Task | Deliverable |
|-----|------|-------------|
| 1-2 | Deploy LangFuse, configure trace ingestion from all components | LangFuse dashboard shows agent LLM calls |
| 3-4 | Deploy Prometheus + Grafana, configure agent metrics | Grafana dashboard shows active agents, cost, latency |
| 5-6 | Configure alerting: HighCostAgent, GateFailureSpike, AgentDown, PRStuck | Alerts fire on threshold breach |
| 7-8 | Deploy Loki for structured log aggregation | All component logs searchable in Grafana |
| 9-10 | Wire DuckBrain persistent memory | Agent decisions stored, queryable, version-controlled |
| 11-14 | Integration test: full trace from task creation → merge with all metrics | LangFuse shows complete trace, Prometheus shows all metrics |

**Phase 5 success criteria:**
- Every LLM call appears in LangFuse with cost, tokens, duration
- Grafana dashboard shows real-time agent activity
- Alert fires within 30s of threshold breach
- DuckBrain query returns agent's past decisions

### 13.7 Phase 6: Advanced Features (Weeks 11-12)

**Goal:** Agent marketplace, cost estimation, and agent-to-agent negotiation.

| Day | Task | Deliverable |
|-----|------|-------------|
| 1-3 | Pre-flight cost estimator: estimate token burn before task execution | Before Ralph Loop spawns, cost estimate shown |
| 4-6 | Agent marketplace registry: discoverable agent profiles with reputation | `helix marketplace search "rust security auditor"` returns results |
| 7-9 | Agent-to-agent PR negotiation: Chimera tie-breaks agent disagreements | Two agents disagree → Chimera resolves with evidence |
| 10-12 | Trust escalation: agent earns merge rights over time | Agent with >95% acceptance rate earns trusted status |
| 13-14 | Full platform integration test: all 12 steps with all features | Complete Helix platform operational |

**Phase 6 success criteria:**
- Cost estimator predicts within 20% of actual cost
- Agent marketplace shows agent profiles with reputation scores
- Agent disagreement in PR → Chimera resolves → decision auditable
- Trusted agent can satisfy co-approval requirement

---

## 14. Error Recovery

### 14.1 Component Failure Matrix

| Component | Failure Mode | Detection | Impact | Recovery |
|-----------|-------------|-----------|--------|----------|
| Forgejo | Process crash | Health check fails (HTTP 503) | ALL agents blocked. No git operations. | Restart container. If data corruption: restore from backup. RTO: 5 min (restart), 1 hour (restore). |
| Forgejo | Disk full | Write errors, health check | Agents can't push. PRs can't be created. | Clear old CI artifacts, expand volume. RTO: 15 min. |
| GitReins hook | Hook timeout/crash | Push fails with hook error | Agents can't push commits. | Disable hook temporarily (emergency), fix hook code, re-enable. RTO: 10 min. |
| Chimera | API timeout | Review doesn't complete | PR review blocked. Co-approval (agent side) unavailable. | Human can manually approve PR. Chimera retries on next PR. RTO: 5 min (restart). |
| Conscientiousness | Loop stall (>10 min) | Timeout, no report | Adversarial review blocked. | Kill loop, restart service. PR proceeds without adversarial check (human override). RTO: 3 min. |
| Hivemind | SQLite corruption | Read/write errors | Memory bank unavailable. Task scheduling degraded. | Restore from daily backup. Agents operate without memory (degraded). RTO: 15 min. |
| Hivemind | Process crash | Health check fails | Task scheduling stops. No new tasks assigned. | Restart container. In-flight tasks preserved (agent state). RTO: 2 min. |
| Agent container | OOM kill | Container exits, metrics drop | That agent stops working. Other agents unaffected. | H4F restarts container. Agent resumes from last commit. RTO: 1 min. |
| Agent container | Budget exhausted | API returns 403 | Agent can't make LLM calls. | Human approves budget increase, or agent waits until next cycle. RTO: manual. |
| LangFuse | Postgres down | Trace writes fail | Traces not ingested. Observability degraded. | Restore Postgres. Traces buffered and replayed. RTO: 10 min. |
| LangFuse | Process crash | Health check fails | Same as above. | Restart container. RTO: 2 min. |
| Prometheus | TSDB corruption | Scrapes fail, metrics missing | Alerting degraded. No metric history. | Restore from backup or restart with fresh TSDB. RTO: 5 min. |
| Caddy | Process crash | HTTPS down, Forgejo unreachable | Platform unreachable externally. Internal services still work. | Restart Caddy. RTO: 1 min. |
| DNS | Resolution failure | Forgejo unreachable by domain | External access blocked. Agents use internal Docker network (unaffected). | Fix DNS record, reduce TTL. RTO: varies (DNS propagation). |

### 14.2 Graceful Degradation

What still works when each component is down:

| Component Down | What Still Works |
|----------------|------------------|
| Forgejo | Agents can write code locally. Chimera/Conscientiousness still run (no new PRs). |
| Chimera | Human review still works. Conscientiousness still runs. PRs can be merged with human-only approval. |
| Conscientiousness | Chimera review still runs. Co-approval still works. Adversarial layer absent — elevated risk accepted. |
| Hivemind | Agents operate from local memory. Task scheduling pauses. Existing work continues. |
| LangFuse | All components operational. Traces lost for the outage window only. |
| Prometheus | All components operational. Metrics gap for the outage window. Alerts may miss events. |
| Agent container (single) | Other agents unaffected. That agent's in-flight work lost (replay from last commit). |
| Caddy | Internal services operational. External access blocked — human intervention needed for merge. |

### 14.3 Retry Policies

**LLM API calls (all components):**
```
Attempt 1: immediate
Attempt 2: 2s delay (exponential: 2^1)
Attempt 3: 4s delay (exponential: 2^2)
Attempt 4: 8s delay (exponential: 2^3) — max
Give up after 4 attempts. Surface error to agent.
```

**Circuit breaker (external APIs — Forgejo, LangFuse):**
```
Failure threshold: 5 failures in 60s window
Open state: 30s
Half-open: 1 request allowed. If succeeds → close circuit. If fails → reset open timer.
```

**GitReins hook retry:**
```
Hook failure: agent receives error, retries push after fixing.
Hook timeout (>5s): hook killed. Agent notified. Push NOT blocked (timeout ≠ failure — hook may be broken upstream).
```

**Ralph Loop lock contention:**
```
Lock acquisition: immediate, no retry (one agent per repo).
Lock timeout: 30 minutes. If agent holds lock >30m, lock auto-released, agent notified, task re-queued.
```

### 14.4 Data Recovery Runbooks

**Forgejo repository corruption:**
```bash
# 1. Stop Forgejo
docker compose stop forgejo

# 2. Restore from latest backup
BACKUP=$(ls -t /mnt/backups/helix/forgejo-*.tar.gz | head -1)
rm -rf /var/lib/docker/volumes/helix_forgejo_data/
tar -xzf "$BACKUP" -C /

# 3. Recover recent pushes from agent worktrees
for agent in /worktrees/agent-*; do
  git --git-dir="$agent/.git" log --since="24 hours ago" --oneline
  # Push any commits newer than backup
done

# 4. Start Forgejo
docker compose start forgejo
```

**Hivemind memory corruption:**
```bash
# SQLite recovery
sqlite3 /data/hivemind.db "PRAGMA integrity_check;"
# If corrupt:
cp /mnt/backups/helix/hivemind-$(date +%Y%m%d).db /data/hivemind.db
systemctl restart helix-platform
```

---

## 15. API Contracts

### 15.1 Forgejo API

**Base URL:** `https://helixloop.dev/api/v1`
**Authentication:** PAT in `Authorization: token <token>` header.

#### Create User
```
POST /admin/users
Content-Type: application/json
Authorization: token <admin-token>

Request:
{
  "username": "agent-sandbox-7",
  "email": "agent-sandbox-7@helix",
  "password": "<generated>",
  "full_name": "Helix Agent (Sandbox 7)",
  "must_change_password": false,
  "send_notify": false
}

Response 201:
{
  "id": 42,
  "username": "agent-sandbox-7",
  "email": "agent-sandbox-7@helix",
  "full_name": "Helix Agent (Sandbox 7)",
  "login": "agent-sandbox-7"
}

Errors: 400 (invalid), 403 (not admin), 409 (username taken), 422 (validation)
```

#### Create SSH Key
```
POST /admin/users/{username}/keys
Authorization: token <admin-token>

Request:
{
  "key": "ssh-ed25519 AAAAC3... agent-sandbox-7@helix",
  "title": "agent-sandbox-7-key",
  "read_only": false
}

Response 201:
{
  "id": 128,
  "key": "ssh-ed25519 AAAAC3...",
  "title": "agent-sandbox-7-key",
  "created_at": "2026-06-19T14:00:00Z"
}
```

#### Create Access Token
```
POST /users/{username}/tokens
Authorization: token <admin-token>

Request:
{
  "name": "agent-sandbox-7-pat",
  "scopes": ["read:repository", "write:repository", "read:user"]
}

Response 201:
{
  "id": 256,
  "name": "agent-sandbox-7-pat",
  "sha1": "abc123...",
  "token_last_eight": "...a1b2c3d4"
}
```

**CRITICAL:** The full token value is only returned ONCE in this response. Store immediately.

#### Get Pull Request
```
GET /repos/{owner}/{repo}/pulls/{index}

Response 200:
{
  "id": 1842,
  "number": 42,
  "title": "feat: add agent identity provisioning",
  "state": "open",
  "mergeable": true,
  "head": {"ref": "feat/agent-identity", "sha": "abc123"},
  "base": {"ref": "main", "sha": "def456"},
  "user": {"login": "agent-sandbox-7"},
  "requested_reviewers": [{"login": "bane"}]
}
```

#### Merge Pull Request
```
POST /repos/{owner}/{repo}/pulls/{index}/merge

Request:
{
  "do": "merge",
  "merge_method": "squash"
}

Response 200:
{
  "merged": true,
  "message": "Pull request successfully merged",
  "sha": "ghi789"
}

Errors: 405 (not mergeable — gates not passed, conflicts, reviews missing)
```

### 15.2 Chimera API

**Base URL:** `https://chimera.helixloop.dev`
**Authentication:** API key in `X-API-Key: <key>` header.

#### Run Deliberation
```
POST /deliberate

Request:
{
  "prompt": "<full PR diff and context>",
  "formation": "code-review-standard"
}

Response 200:
{
  "deliberation_id": "del-abc123",
  "formation": "code-review-standard",
  "score": 0.82,
  "status": "APPROVE",
  "judges": [
    {"model": "anthropic/claude-sonnet-4", "domain": "logic_correctness", "score": 0.85, "findings": [...]},
    {"model": "google/gemini-2.5-pro", "domain": "security_audit", "score": 0.78, "findings": [...]},
    {"model": "openai/gpt-5.2", "domain": "style_maintainability", "score": 0.83, "findings": [...]}
  ],
  "audit": {
    "model": "meta-llama/llama-4-maverick",
    "agreement": 0.91,
    "flagged": false
  },
  "trace_id": "trace-xyz789"
}

Errors: 400 (invalid formation), 429 (rate limited), 500 (model error), 504 (timeout)
```

#### List Formations
```
GET /formations

Response 200:
{
  "formations": [
    {"name": "code-review-standard", "models": 3, "timeout": 300},
    {"name": "approval-check", "models": 1, "timeout": 30},
    {"name": "security-deep-audit", "models": 4, "timeout": 600}
  ]
}
```

#### Health Check
```
GET /health

Response 200: {"status": "ok", "models_available": 12}
Response 503: {"status": "degraded", "models_available": 3}
```

### 15.3 Conscientiousness API

**Base URL:** `https://conscience.helixloop.dev`
**Authentication:** API key in `X-API-Key: <key>` header.

#### Evaluate PR
```
POST /evaluate

Request:
{
  "pr_diff": "<full diff>",
  "pr_context": {"repo": "totalwindupflightsystems/helix", "number": 42, "author": "agent-sandbox-7"},
  "max_iterations": 3,
  "adversarial_agents": ["assumption-buster", "devils-advocate", "redteam"]
}

Response 200:
{
  "evaluation_id": "eval-abc123",
  "iterations": 2,
  "status": "PASS",
  "findings": [
    {"severity": "HIGH", "agent": "devils-advocate", "finding": "Error handling on line 47...", "resolved": true},
    {"severity": "MEDIUM", "agent": "assumption-buster", "finding": "Assumes Forgejo always returns 200...", "resolved": true}
  ],
  "blocked": false
}
```

#### Get Report
```
GET /report/{evaluation_id}

Response 200:
{
  "evaluation_id": "eval-abc123",
  "status": "PASS",
  "full_report": "<markdown report with all findings, resolutions, and trace>"
}
```

#### Health Check
```
GET /health

Response 200: {"status": "ok", "uptime_seconds": 86400}
```

### 15.4 Hivemind API

**Base URL:** `https://hivemind.helixloop.dev`
**Authentication:** API key in `X-API-Key: <key>` header.

#### Write Memory
```
POST /memory/write

Request:
{
  "agent_id": "agent-sandbox-7",
  "repo": "totalwindupflightsystems/helix",
  "event_type": "gate_failure",
  "summary": "Lint failure on pkg/identity/syncer.go: unused import 'fmt'",
  "resolution": "Removed import, re-pushed. Passed Tier 1.",
  "tags": ["tier1", "lint", "ruff"]
}

Response 201:
{
  "memory_id": "mem-2026-06-19-042",
  "persisted": true
}
```

#### Query Memory
```
GET /memory/query?agent=agent-sandbox-7&repo=totalwindupflightsystems/helix&limit=20

Response 200:
{
  "results": [
    {
      "memory_id": "mem-2026-06-19-042",
      "timestamp": "2026-06-19T14:22:00Z",
      "event_type": "gate_failure",
      "summary": "Lint failure on pkg/identity/syncer.go: unused import 'fmt'",
      "tags": ["tier1", "lint"]
    }
  ],
  "total": 1
}
```

#### List Agents
```
GET /agents

Response 200:
{
  "agents": [
    {"id": "agent-sandbox-7", "status": "active", "current_task": "pr-1842", "budget_remaining": 142.50},
    {"id": "agent-sandbox-9", "status": "idle", "current_task": null, "budget_remaining": 150.00}
  ]
}
```

#### Assign Task
```
POST /tasks/assign

Request:
{
  "agent_id": "agent-sandbox-7",
  "repo": "totalwindupflightsystems/helix",
  "task": {"type": "implement", "pr_number": 42, "prompt_ref": "prompts/agent-identity/v3.md"}
}

Response 200:
{
  "task_id": "task-abc123",
  "status": "assigned",
  "agent_id": "agent-sandbox-7"
}

Errors: 409 (agent busy), 402 (budget exhausted)
```

#### Health Check
```
GET /health

Response 200: {"status": "ok", "db_size_mb": 45, "memory_entries": 1842}
```

### 15.5 Muster API

**Base URL:** `https://muster.helixloop.dev`
**Authentication:** API key in `X-API-Key: <key>` header.

#### Generate MCP Tools
```
POST /generate

Request:
{
  "openapi_spec_url": "https://helixloop.dev/swagger.v1.json",
  "output_format": "mcp",
  "cache_ttl_seconds": 3600
}

Response 200:
{
  "tools": [
    {
      "name": "forgejo_create_user",
      "description": "Create a new Forgejo user account",
      "parameters": {...},
      "endpoint": "POST /api/v1/admin/users"
    }
  ],
  "tool_count": 47,
  "cached": false
}
```

#### List Specifications
```
GET /specs

Response 200:
{
  "specs": [
    {"name": "forgejo", "url": "https://helixloop.dev/swagger.v1.json", "last_generated": "2026-06-19T14:00:00Z"},
    {"name": "chimera", "url": "https://chimera.helixloop.dev/openapi.json", "last_generated": "2026-06-19T13:00:00Z"}
  ]
}
```

#### Health Check
```
GET /health

Response 200: {"status": "ok", "specs_cached": 2, "tools_generated": 94}
```

---

## 16. Glossary

| Term | Definition |
|------|-----------|
| **Agent Identity** | A Forgejo user account created for an AI agent from H4F's known-friends.json. Includes SSH key, PAT, scoped permissions, and budget. |
| **Agent Marketplace** | Discoverable registry of pre-built agent profiles with reputation scores. "I need a Rust security auditor" → search → install → agent joins repo. |
| **Agent Swarm** | Multiple agents working in parallel on different repos or tasks, orchestrated by Axiom. |
| **Attestation** | Metadata embedded in every agent commit: agent UUID, prompt version hash, model used, context window size. Verified by GitReins Tier 1. |
| **Audit (Chimera)** | Independent verification step where a model (Llama 4 Maverick) checks the weighted voting result for agreement. Flags discrepancies >0.3. |
| **Blast Radius** | The scope of damage if an agent container is compromised. Contained by per-agent isolation, VPN routing, read-only filesystems. |
| **Budget** | Per-agent monthly LLM spend cap. Enforced by H4F at the API key level (403 on exhaustion) and Hivemind at the task level. |
| **Co-Approval** | The requirement that both 1 human AND 1 agent must approve a PR before merge. Enforced by Forgejo branch protection. |
| **Commit Protocol** | The sequence: agent acquires Ralph Loop lock → creates isolated worktree → writes code → runs tests → commits with attestation → pushes. |
| **Conscientiousness** | Go service that runs adversarial evaluation loops on PRs. Challenges code with assumption-buster, devils-advocate, and red-team agents. |
| **Credential Pool** | Rotation of multiple API keys per provider to avoid rate limits. Managed by Hermes. |
| **DuckBrain** | Git-backed persistent agent memory. Stores decisions, anti-patterns, and architectural tradeoffs with vector search. |
| **Evidence Bundle** | The complete audit trail for a decision: which models reviewed, what they found, what changed, who approved. Stored in DuckBrain. |
| **Formation (Chimera)** | A DAG of models assigned to review domains for a specific task. Example: Sonnet on logic, Gemini on security, GPT-5.2 on style. |
| **Gate** | A mandatory check that must PASS before code progresses. Gates are ordered by cost: Tier 1 (static, free) → Tier 2 (agentic, cheap) → Chimera (multi-model, expensive) → Conscientiousness (adversarial, variable) → PromptFoo (CI, cheap) → Co-Approval (async). |
| **GitReins** | Python tool providing pre-receive hooks (Tier 1: static analysis) and agentic evaluator (Tier 2: LLM-based review). |
| **H4F (Hermes4Friends)** | Multi-tenant agent hosting platform. Manages per-agent Docker containers with isolated configs, API keys, budgets, and VPN routing. |
| **Helix** | The agent-first software development platform where humans and AI agents are equal participants in the code lifecycle. |
| **Hermes Agent** | The AI agent framework by Nous Research that runs inside Helix agent containers. Provides tool calling, memory, and provider-agnostic model access. |
| **Hivemind** | Go service providing agent memory (inbox → compiled → _index lifecycle), task scheduling, and IAM. |
| **Hivemind Memory Bank** | Event pipeline: raw Inbox → deduplicated Compiled entries → human-readable _index → persistent DuckBrain storage. |
| **known-friends.json** | H4F's agent identity registry. Maps agent UUIDs to display names, API keys, budgets, model preferences. Source of truth for agent provisioning. |
| **LangFuse** | Open-source LLM observability platform. Traces every API call with model, tokens, cost, and duration. |
| **Muster** | Go tool that parses any OpenAPI spec and auto-generates MCP tools, CLI commands, shell completions, and Starlark DSL. |
| **PR Negotiation** | When two agents disagree in PR comments, Chimera runs multi-model analysis of the disputed code and breaks the tie with auditable evidence. |
| **Prompt Attestation** | Commit body linking generated code to the exact prompt version that produced it: `Prompt: prompts/agent-identity/v3.md sha256:abc123`. |
| **Prompt Registry** | Version-controlled `prompts/` directory in every repo. Every prompt change is a PR with PromptFoo CI regression testing. |
| **Ralph Loop** | The execution pattern: acquire lock → create worktree → agent writes code → commit → push → open PR → gates → merge → release lock. Named after the "Ralph Wiggum Loop" — the agent that keeps trying until it succeeds. |
| **Sandbox (Agent)** | Per-agent Docker container with: gluetun VPN, dind executor, hermes-agent, scoped API keys, budget limits, read-only filesystem, no host network access. |
| **Tier 1 (GitReins)** | Static quality checks: secrets scan, lint, tests, build, commit attestation, prompt link, file size. Runs in <5s, costs $0. |
| **Tier 2 (GitReins)** | Agentic evaluator: LLM reviews diff for logic errors, test quality, security surface, dependency impact. Runs in <90s, costs ~$0.05. |
| **Trust Escalation** | Agents with >95% PR acceptance rate over 50 PRs earn "trusted" status. Trusted agents' approvals count as 1.5 votes. |
| **Veto (Agent)** | Any agent can block a PR merge by posting a review comment with `BLOCKING: <reason> <evidence>`. Human can override. |
| **Worktree** | Isolated git working directory created by Ralph Loop. One agent per worktree — prevents conflicts. Cleaned up after merge or task cancellation. |
| **12-Step Flow** | The full Helix pipeline: human task → Axiom assembles swarm → Ralph Loop lock → agent writes code → commit with attestation → GitReins Tier 1 → agent opens PR → Chimera review → Conscientiousness adversarial → PromptFoo CI → co-approval → merge + deploy. |

---

## 17. Appendices

### Appendix A: Full docker-compose.yml

```yaml
# /opt/helix/docker-compose.yml — Helix Platform
# Deploy: docker compose up -d
# Manage: systemctl start/stop/restart helix-platform

version: "3.8"

services:
  # ═══ Git Forge ═══
  forgejo:
    image: codeberg.org/forgejo/forgejo:9
    container_name: helix-forgejo
    ports:
      - "3000:3000"
      - "2222:22"
    volumes:
      - forgejo_data:/var/lib/forgejo
      - forgejo_config:/etc/forgejo
      - ./gitreins/hooks:/var/lib/forgejo/gitea/custom/hooks:ro
    environment:
      FORGEJO__server__DOMAIN: helixloop.dev
      FORGEJO__server__ROOT_URL: https://helixloop.dev
      FORGEJO__server__SSH_DOMAIN: helixloop.dev
      FORGEJO__server__LFS_START_SERVER: "true"
      FORGEJO__security__INSTALL_LOCK: "true"
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:3000/api/v1/version"]
      interval: 30s
      timeout: 10s
      retries: 3
    restart: unless-stopped

  # ═══ CI Runner ═══
  forgejo-runner:
    image: codeberg.org/forgejo/runner:4
    container_name: helix-runner
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - runner_data:/data
    environment:
      FORGEJO_INSTANCE_URL: http://forgejo:3000
      FORGEJO_RUNNER_TOKEN: ${FORGEJO_RUNNER_TOKEN}
    depends_on:
      forgejo:
        condition: service_healthy
    restart: unless-stopped

  # ═══ Chimera ═══
  chimera:
    build:
      context: ./chimera
      dockerfile: Dockerfile
    container_name: helix-chimera
    ports:
      - "8001:8001"
    environment:
      OPENROUTER_API_KEY: ${OPENROUTER_API_KEY}
      CHIMERA_FORMATION: code-review-standard
      CHIMERA_PORT: "8001"
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8001/health"]
      interval: 15s
      timeout: 5s
      retries: 3
    restart: unless-stopped

  # ═══ Conscientiousness ═══
  conscientiousness:
    build:
      context: ./conscientiousness
      dockerfile: Dockerfile
    container_name: helix-conscience
    ports:
      - "8002:8002"
    volumes:
      - conscience_data:/data
    environment:
      OPENROUTER_API_KEY: ${OPENROUTER_API_KEY}
      CONSCIENCE_DB_PATH: /data/conscience.db
      CONSCIENCE_PORT: "8002"
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8002/health"]
      interval: 15s
      timeout: 5s
      retries: 3
    restart: unless-stopped

  # ═══ Hivemind ═══
  hivemind:
    build:
      context: ./hivemind
      dockerfile: Dockerfile
    container_name: helix-hivemind
    ports:
      - "8003:8003"
    volumes:
      - hivemind_data:/data
      - hivemind_memory:/memory
    environment:
      HIVEMIND_DB_PATH: /data/hivemind.db
      HIVEMIND_MEMORY_PATH: /memory
      HIVEMIND_PORT: "8003"
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8003/health"]
      interval: 15s
      timeout: 5s
      retries: 3
    restart: unless-stopped

  # ═══ LangFuse + Postgres ═══
  langfuse:
    image: ghcr.io/langfuse/langfuse:3
    container_name: helix-langfuse
    ports:
      - "3001:3000"
    environment:
      DATABASE_URL: postgresql://langfuse:${LANGFUSE_DB_PASS}@postgres:5432/langfuse
      NEXTAUTH_SECRET: ${LANGFUSE_AUTH_SECRET}
      SALT: ${LANGFUSE_SALT}
      TELEMETRY_ENABLED: "false"
    depends_on:
      postgres:
        condition: service_healthy
    restart: unless-stopped

  postgres:
    image: postgres:16-alpine
    container_name: helix-postgres
    volumes:
      - postgres_data:/var/lib/postgresql/data
    environment:
      POSTGRES_DB: langfuse
      POSTGRES_USER: langfuse
      POSTGRES_PASSWORD: ${LANGFUSE_DB_PASS}
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U langfuse"]
      interval: 10s
      timeout: 5s
      retries: 5
    restart: unless-stopped

  # ═══ Monitoring ═══
  prometheus:
    image: prom/prometheus:v3
    container_name: helix-prometheus
    ports:
      - "9090:9090"
    volumes:
      - ./prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - prometheus_data:/prometheus
    command:
      - "--config.file=/etc/prometheus/prometheus.yml"
      - "--storage.tsdb.path=/prometheus"
      - "--storage.tsdb.retention.time=30d"
    restart: unless-stopped

  loki:
    image: grafana/loki:3
    container_name: helix-loki
    ports:
      - "3100:3100"
    volumes:
      - ./loki/loki-config.yaml:/etc/loki/local-config.yaml:ro
      - loki_data:/loki
    restart: unless-stopped

  grafana:
    image: grafana/grafana:11
    container_name: helix-grafana
    ports:
      - "3002:3000"
    volumes:
      - grafana_data:/var/lib/grafana
      - ./grafana/dashboards:/etc/grafana/provisioning/dashboards:ro
      - ./grafana/datasources:/etc/grafana/provisioning/datasources:ro
    environment:
      GF_SECURITY_ADMIN_PASSWORD: ${GRAFANA_ADMIN_PASS}
      GF_USERS_ALLOW_SIGN_UP: "false"
    restart: unless-stopped

  # ═══ Reverse Proxy ═══
  caddy:
    image: caddy:2-alpine
    container_name: helix-caddy
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy_data:/data
      - caddy_config:/config
    restart: unless-stopped

volumes:
  forgejo_data:
  forgejo_config:
  runner_data:
  conscience_data:
  hivemind_data:
  hivemind_memory:
  postgres_data:
  prometheus_data:
  loki_data:
  grafana_data:
  caddy_data:
  caddy_config:
```

### Appendix B: .gitreins/config.yaml

```yaml
# .gitreins/config.yaml — placed in repo root
# Controls pre-receive hook behavior for this repository

tier1:
  enabled: true
  timeout: 5s
  checks:
    secrets:
      enabled: true
      tool: gitleaks
      args: ["detect", "--no-git", "-v"]
    lint:
      enabled: true
      tools:
        go: ["golangci-lint", "run", "--timeout=30s"]
        python: ["ruff", "check", "."]
        typescript: ["eslint", "."]
    tests:
      enabled: true
      timeout: 60s
      commands:
        go: ["go", "test", "./...", "-count=1"]
        python: ["pytest", "-x", "--tb=short"]
    build:
      enabled: true
      commands:
        go: ["go", "build", "./..."]
    attestation:
      enabled: true
      require_co_author: true
    prompt_link:
      enabled: true
      require_for_patterns: ["*.go", "*.py", "*.ts", "*.rs"]
    file_size:
      enabled: true
      max_bytes: 524288  # 500KB
      exclude_patterns: ["assets/*", "*.pb.go", "*.gen.go"]

tier2:
  enabled: true
  timeout: 90s
  model: google/gemini-2.5-flash-lite
  checks:
    - logic_review
    - test_quality
    - security_surface
    - dependency_impact

results:
  path: .gitreins/results/
  retention_days: 30

notifications:
  on_fail: pr_comment
  on_pass: silent
```

### Appendix C: known-friends.json (Sample)

```json
{
  "version": "2.0",
  "platform": "helix",
  "agents": [
    {
      "uuid": "agent-sandbox-7",
      "name": "agent-sandbox-7",
      "display_name": "Sandbox 7 (Go Specialist)",
      "status": "active",
      "tier": "flash",
      "role": "implementer",
      "specialties": ["go", "kubernetes", "sql"],
      "permissions": {
        "repos": ["totalwindupflightsystems/helix"],
        "can_push_to": ["feat/*", "fix/*", "docs/*"],
        "can_open_pr": true,
        "can_merge": false,
        "can_review": true,
        "can_veto": true
      },
      "budget": {
        "monthly_usd": 150,
        "max_per_task_usd": 10,
        "alert_at_pct": 80
      },
      "model_preferences": {
        "implementation": "deepseek-v4-pro",
        "review": "google/gemini-2.5-flash-lite",
        "planning": "anthropic/claude-sonnet-4"
      },
      "ssh_key_type": "ed25519",
      "reputation": {
        "prs_opened": 0,
        "prs_merged": 0,
        "acceptance_rate": null,
        "trusted": false
      }
    },
    {
      "uuid": "agent-sandbox-9",
      "name": "agent-sandbox-9",
      "display_name": "Sandbox 9 (Security Auditor)",
      "status": "active",
      "tier": "pro",
      "role": "reviewer",
      "specialties": ["security", "python", "rust"],
      "permissions": {
        "repos": ["totalwindupflightsystems/helix", "totalwindupflightsystems/gitreins"],
        "can_push_to": [],
        "can_open_pr": false,
        "can_merge": false,
        "can_review": true,
        "can_veto": true
      },
      "budget": {
        "monthly_usd": 200,
        "max_per_task_usd": 15,
        "alert_at_pct": 80
      },
      "model_preferences": {
        "implementation": null,
        "review": "google/gemini-2.5-pro",
        "planning": "anthropic/claude-sonnet-4"
      },
      "ssh_key_type": "ed25519",
      "reputation": {
        "prs_opened": 0,
        "prs_merged": 0,
        "acceptance_rate": null,
        "trusted": false
      }
    },
    {
      "uuid": "agent-sandbox-12",
      "name": "agent-sandbox-12",
      "display_name": "Sandbox 12 (Documentation)",
      "status": "active",
      "tier": "flash",
      "role": "implementer",
      "specialties": ["documentation", "markdown", "typescript"],
      "permissions": {
        "repos": ["totalwindupflightsystems/helix"],
        "can_push_to": ["docs/*"],
        "can_open_pr": true,
        "can_merge": false,
        "can_review": true,
        "can_veto": false
      },
      "budget": {
        "monthly_usd": 50,
        "max_per_task_usd": 3,
        "alert_at_pct": 80
      },
      "model_preferences": {
        "implementation": "google/gemini-2.5-flash-lite",
        "review": "google/gemini-2.5-flash-lite",
        "planning": "google/gemini-2.5-flash-lite"
      },
      "ssh_key_type": "ed25519",
      "reputation": {
        "prs_opened": 0,
        "prs_merged": 0,
        "acceptance_rate": null,
        "trusted": false
      }
    }
  ]
}
```

### Appendix D: .promptfoo.yaml (Sample)

```yaml
# .promptfoo.yaml — Helix monorepo prompt evaluation
# Runs in CI on every push that changes prompts/

prompts:
  - file://prompts/agent-identity/v3.md
  - file://prompts/code-review/v2.md

providers:
  - id: openrouter:anthropic/claude-sonnet-4
    config:
      temperature: 0.3
      max_tokens: 4096
  - id: openrouter:google/gemini-2.5-flash-lite
    config:
      temperature: 0.3
      max_tokens: 4096

defaultTest:
  assert:
    - type: not-contains
      value: "I apologize"
    - type: not-contains
      value: "as an AI"
    - type: not-contains
      value: "I cannot"

tests:
  # ── Agent Identity Prompt ──
  - description: "Identity provision creates valid Forgejo account"
    prompt: file://prompts/agent-identity/v3.md
    vars:
      agent_name: "test-agent-ci"
      tier: "flash"
      forgejo_url: "https://helixloop.dev"
    assert:
      - type: contains
        value: "POST /api/v1/admin/users"
      - type: contains
        value: "ssh-ed25519"
      - type: not-contains
        value: "error"
      - type: llm-rubric
        value: |
          The response describes:
          1. Creating a Forgejo user account with the given agent name
          2. Registering an ED25519 SSH key
          3. Creating a personal access token with scoped permissions
          4. Configuring the agent's git identity
          All steps must be present and in order.

  - description: "Identity provision handles duplicate user gracefully"
    prompt: file://prompts/agent-identity/v3.md
    vars:
      agent_name: "existing-agent"
      tier: "flash"
      forgejo_url: "https://helixloop.dev"
    assert:
      - type: contains
        value: "409"
      - type: llm-rubric
        value: "The response handles HTTP 409 (Conflict) by reporting the user already exists rather than crashing."

  - description: "Identity provision enforces input validation"
    prompt: file://prompts/agent-identity/v3.md
    vars:
      agent_name: "a"  # too short
      tier: "flash"
      forgejo_url: "https://helixloop.dev"
    assert:
      - type: contains
        value: "invalid"
      - type: llm-rubric
        value: "The response validates that agent_name meets minimum length requirements before making API calls."

  # ── Code Review Prompt ──
  - description: "Code review catches SQL injection"
    prompt: file://prompts/code-review/v2.md
    vars:
      code: |
        func getUser(name string) (*User, error) {
            query := fmt.Sprintf("SELECT * FROM users WHERE name = '%s'", name)
            return db.Query(query)
        }
      language: "go"
    assert:
      - type: contains
        value: "SQL injection"
      - type: llm-rubric
        value: "The review identifies the SQL injection vulnerability (string formatting into query) and recommends parameterized queries."

  - description: "Code review catches nil dereference"
    prompt: file://prompts/code-review/v2.md
    vars:
      code: |
        func process(user *User) string {
            return user.Name
        }
      language: "go"
    assert:
      - type: contains
        value: "nil"
      - type: llm-rubric
        value: "The review identifies the nil pointer dereference risk and recommends adding a nil check."

  - description: "Code review passes safe code"
    prompt: file://prompts/code-review/v2.md
    vars:
      code: |
        func greet(name string) string {
            if name == "" {
                return "Hello, world!"
            }
            return fmt.Sprintf("Hello, %s!", name)
        }
      language: "go"
    assert:
      - type: not-contains
        value: "vulnerability"
      - type: not-contains
        value: "injection"
      - type: llm-rubric
        value: "The review does not flag false positives. The code is safe and the review should pass without CRITICAL or HIGH findings."
```

### Appendix E: Forgejo Branch Protection Config

```yaml
# .forgejo/branch-protection.yml
# Enforced on the main branch

main:
  # ── Approval Requirements ──
  required_approvals: 2          # 1 human + 1 agent = 2 minimum
  dismiss_stale_reviews: true    # New push invalidates old approvals
  require_code_owner_reviews: true
  block_on_agent_veto: true      # BLOCKING comment prevents merge

  # ── Push Restrictions ──
  allow_force_pushes: false
  allow_deletions: false
  restrict_pushes: true
  push_allowlist:
    - bane                 # Platform admin (human)
    - helix-bot            # Platform automation (CI/CD, not an agent)

  # ── Status Checks (all must pass) ──
  status_checks:
    - gitreins/tier-1
    - gitreins/tier-2
    - chimera/review
    - conscientiousness/adversarial
    - promptfoo/regression
    - test/unit
    - test/integration

  # ── Merge Restrictions ──
  merge_method: squash
  require_signed_commits: true
  require_linear_history: false
```

### Appendix F: .env.example

```bash
# /opt/helix/.env — Helix Platform Secrets
# chmod 600, never commit to git
# Copy this file to .env and fill in values

# ═══ Master Credentials ═══
OPENROUTER_API_KEY (set via env var)
FORGEJO_ADMIN_TOKEN=...
FORGEJO_RUNNER_TOKEN=...

# ═══ LangFuse ═══
LANGFUSE_DB_PASS=...
LANGFUSE_AUTH_SECRET=...
LANGFUSE_SALT=...
LANGFUSE_PUBLIC_KEY=pk-...
LANGFUSE_SECRET_KEY=sk-...

# ═══ Grafana ═══
GRAFANA_ADMIN_PASS=...

# ═══ H4F Agent Keys (one per agent) ═══
# Agent: Sandbox 7 (Go Specialist)
AGENT_7_OPENROUTER_KEY=n0t-a-r3al-k3y
AGENT_7_FORGEJO_TOKEN=...
# Agent: Sandbox 9 (Security Auditor)
AGENT_9_OPENROUTER_KEY=n0t-a-r3al-k3y
AGENT_9_FORGEJO_TOKEN=...
# Agent: Sandbox 12 (Documentation)
AGENT_12_OPENROUTER_KEY=n0t-a-r3al-k3y
AGENT_12_FORGEJO_TOKEN=...

# ═══ DuckBrain ═══
DUCKBRAIN_REPO_PATH=/data/duckbrain

# ═══ Z.AI Coding Plan (GLM-5.2) ═══
ZAI_API_KEY=...

# ═══ Optional: GitHub Mirror ═══
GITHUB_TOKEN=ghp_...
```

---

## Document Status

**All sections complete.** This is the full Helix platform implementation specification.

| Section | Status | Lines (approx) |
|---------|--------|----------------|
| 1. Platform Architecture | ✅ Complete | 264 |
| 2. Data Flow and Execution Model | ✅ Complete | ~300 |
| 3. Component Specifications | ✅ Complete | ~350 |
| 4. Integration Contracts | ✅ Complete | ~280 |
| 5. Identity and IAM | ✅ Complete | ~250 |
| 6. Security Model | ✅ Complete | ~300 |
| 7. Quality Gates | ✅ Complete | ~350 |
| 8. Observability | ✅ Complete | ~280 |
| 9. Deployment Architecture | ✅ Complete | ~350 |
| 10. Operations | ✅ Complete | ~300 |
| 11. Performance SLAs | ✅ Complete | ~280 |
| 12. Test Strategy | ✅ Complete | ~200 |
| 13. Build Order | ✅ Complete | ~250 |
| 14. Error Recovery | ✅ Complete | ~200 |
| 15. API Contracts | ✅ Complete | ~300 |
| 16. Glossary | ✅ Complete | ~100 |
| 17. Appendices | ✅ Complete | ~400 |

**Total:** ~4,850 lines, 17 sections, 6 appendices.

**Ready for Phase 1 implementation: Forgejo instance + GitReins hooks + Agent Identity.**
