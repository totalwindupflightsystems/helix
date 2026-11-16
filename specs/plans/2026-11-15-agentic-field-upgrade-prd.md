# Helix PRD v2.0 — Field-Upgrade Brief: Agentic SDLC, November 2026

**Status:** Draft for review → specs v2.0 upgrade wave
**Date:** 2026-11-15
**Trigger:** Bane directive — "a lot has changed in this field for helix; research how OpenAI and Anthropic build code, how they replaced or re-leveraged git, and how others work with agents — upgrade specs and features."
**Method:** Three parallel research passes (Anthropic, OpenAI, ecosystem), 40+ sources, every claim cited inline. Helix spec baseline read at HEAD (`specs/SPECIFICATION.md` v1.0.0, last updated **June 19, 2026** — five months stale).

---

## 1. Executive summary

The field converged hard in the 12 months since helix's spec was written. The single most important finding: **nobody replaced git — everybody re-leveraged it.** Git worktrees became the concurrency primitive for parallel agents, the pull request became the agent's evidence bundle and handback contract, and the issue became an executable work specification. What DID change around git is the real upgrade surface for helix: orchestration state machines moved out of git into task trackers, execution moved into isolated per-task sandboxes (containers/VMs/worktrees), specs became machine-checkable sources of truth with convergence loops, dispatch became event-driven, and review became an automated control plane that runs before humans see anything.

Helix's 2026 bet — self-hosted, agent-first, real agent identities with budgets and trust tiers — is now mainstream-validated. Its gap is not thesis; it is that the five newest patterns (event-driven dispatch, spec-convergence loops, first-class run objects, review control plane, fan-out budgeting) exist in the field but not yet in the specs.

## 2. The verdict on git: re-leveraged, not replaced

Evidence across all three passes:

- **Anthropic**: parallel Claude sessions are built on **one git worktree per agent**; the `/batch` pattern decomposes large changes into 5–30 units, each an isolated worktree agent opening its own PR ([Claude Code best practices](https://www.anthropic.com/engineering/claude-code-best-practices)). Their 16-agent Rust C-compiler experiment (~100k lines, ~2,000 sessions, ~$20k API spend, compiled Linux 6.9 on x86/ARM/RISC-V) coordinated through **task-lock files outside git** ([building-c-compiler, 2026-02-05](https://www.anthropic.com/engineering/building-c-compiler)).
- **OpenAI**: Codex cloud runs each task in an isolated container preloaded with the repo and proposes a PR ([introducing-codex, 2025-05-16](https://openai.com/index/introducing-codex/)); the Codex app ships **built-in worktree isolation** ([2026-02-02](https://openai.com/index/introducing-the-codex-app/)); their agent-first harness repo reached ~1,500 PRs / ~1M lines in five months with the app bootable **per worktree** ([harness-engineering, 2026-02-11](https://openai.com/index/harness-engineering/)).
- **GitHub**: Copilot coding agent = issue → branch → draft PR → tests → human review ([2025-05-19](https://github.blog/ai-and-ml/github-copilot/github-copilot-coding-agent-101-getting-started-with-agentic-workflows/)); the 2026 Copilot app gives every local session an isolated worktree and every cloud session a hosted environment ([2026-06-02](https://github.blog/news-insights/product-news/github-copilot-app-the-agent-native-desktop-experience/)).
- **Git alternatives** (Jujutsu, GitButler, Sapling) are evaluated as **local** change-management layers while git stays the canonical remote, PR, CI, and audit surface — evidence for wholesale replacement is weak ([repowise comparison](https://repowise.dev/blog/comparisons/git-alternatives-for-ai-agents); [jujutsu workflow proposal](https://github.com/netresearch/jujutsu-workflow-skill)).

**Implication for helix:** Forgejo stays the spine. The PRD does NOT propose replacing it. It proposes making what runs *around* Forgejo first-class.

## 3. The eight patterns that define the late-2026 field

1. **Worktree-per-agent as an operational primitive, not a convention.** Every vendor converged (Anthropic, OpenAI, GitHub, Cursor VMs, Devin VMs). Helix has this as the Ralph Loop pattern — it must become a **platform primitive with leases** (agent, task, branch, workspace, budget, start, expiry).
2. **The PR is an agent mailbox + evidence bundle.** Task context, commits, test results, tool trace, review comments, merge decision — all attached to the PR ([Copilot coding agent 101](https://github.blog/ai-and-ml/github-copilot/github-copilot-coding-agent-101-getting-started-with-agentic-workflows/); [Cursor cloud agent security](https://cursor.com/docs/cloud-agent/security)).
3. **Orchestration state lives outside git.** OpenAI's Symphony explicitly makes the **issue tracker the workflow state machine** and decouples work from agent sessions and PRs ([open-source-codex-orchestration-symphony, 2026-04-27](https://openai.com/index/open-source-codex-orchestration-symphony/)). Anthropic's compiler experiment needed task-lock files. Cognition's guidance: a manager decomposes and delegates; read-only subagents gather context; uncontrolled write-swarms are the failure mode ([multi-agents-working](https://cognition.com/blog/multi-agents-working)).
4. **Specs are the source of truth that generates and evaluates code.** GitHub Spec Kit formalizes Specify → Plan → Tasks → Implement → **Converge**, where convergence checks implementation against spec and appends missing tasks ([spec-kit](https://github.github.com/spec-kit/); [spec-driven.md](https://github.com/github/spec-kit/blob/main/spec-driven.md)). Helix has 26 spec files but **no convergence loop** — and its own flagship dispatch cannot decompose 15 of its 24 specs (board row DF-HELIX-7).
5. **Dispatch is event-driven.** Labels, issue assignment, PR comments, CI failures, schedules, monitoring alerts, and webhooks all start or resume agent runs ([GitHub Agentic Workflows GA, 2026-10-28](https://github.blog/changelog/2026-10-28-github-agentic-workflows-generally-available/); [Jules proactive pods](https://blog.google/innovation-and-ai/technology/developers-tools/jules-proactive-updates/)). Helix dispatch today is task-body/manual.
6. **Review is an automated control plane before human review.** OpenAI reports Codex reviews the vast majority of its PRs and catches hundreds of issues per day ([2025-09-23](https://openai.com/index/introducing-upgrades-to-codex/)). Helix owns Chimera (multi-model deliberation) — it must be wired as the **first reviewer on every PR**, with the verdict parser fixed (REJECTED-misparsed-as-APPROVE bug, foreman event #198).
7. **The agent run is a first-class durable object.** Linked to issue, branch/worktree, commits, PR, logs, tests, model usage, reviewer decisions — a chat transcript is not provenance (all three passes converge here).
8. **Fan-out is a schedulable, budgeted resource.** Parallelism multiplies API spend, CI load, and review capacity; Anthropic's $20k compiler run and the field's admission-control patterns make per-agent/per-task/per-project budgets and concurrency caps core product features, not ops afterthoughts. Helix's cost estimator is per-agent; it needs **wave-level** budget admission.

Plus two operational warnings the field already paid for: **agent runtimes are lifecycle-bound** (Google Jules announced a read-only transition 2026-10-29 — external agents can change availability; helix's self-hosted runner plane is the right answer) ([jules changelog](https://jules.google/docs/changelog/2026-10-29/)), and **agent-generated PR volume needs product features**: queueing, dedup, stale-run recovery, review-load metrics ([33,000-PR empirical study, arXiv 2601.15195](https://arxiv.org/html/2601.15195)).

## 4. Helix today vs the field (gap analysis)

| # | Field pattern (late 2026) | Helix state (spec v1.0.0 / board) | Gap |
|---|---|---|---|
| 1 | Worktree-per-agent primitive with leases | Ralph Loop pattern embedded in components | Formalize: lease object + expiry + cleanup (SPEC) |
| 2 | PR as evidence bundle | Commits carry attestation blocks | Extend: PR-level evidence bundle (tests, tool trace, risks) |
| 3 | Orchestration state machine outside git | Board/tasks.jsonl internal; Forgejo issues are intent objects | Promote Forgejo issue to canonical state machine w/ lifecycle states |
| 4 | Spec-convergence loop (Spec Kit) | 26 specs, no machine conformance check; dispatch fails on own specs (DF-HELIX-7) | NEW capability: converge command |
| 5 | Event-driven dispatch | HMAC webhooks verified live; used for observability, not dispatch | Wire webhooks/labels/schedules → auto-dispatch |
| 6 | Automated review control plane | Chimera exists; manual; verdict parser bug | Wire Chimera as mandatory first reviewer; fix parser |
| 7 | First-class run objects | Session logs scattered | Run object w/ linked evidence |
| 8 | Wave-level budget admission | Per-agent estimate only | Wave budget + concurrency caps before fan-out |
| 9 | Scheduled maintenance pods | None | Pods for deps/security/coverage on a schedule |
| 10 | Self-hosted runner plane | Already the thesis | Strengthen; cite Jules lifecycle risk as validation |

**Already field-leading (keep, document, don't regress):** real Forgejo agent identities with scoped SSH keys; budgets; earned trust tiers; tier-1+tier-2 quality gates (GitReins); multi-model adversarial review (Chimera + Conscientiousness); API→MCP tooling (Muster); git-backed agent memory (DuckBrain); no-win stress testing (Kobayashi-Maru). Very few platforms have identity+budget+trust as first-class — that is helix's differentiator and the PRD protects it.

## 5. Proposed feature set (PRD v2.0)

**P0 — make the core loop match the field:**
- **F1 Run objects.** Every dispatch creates a durable run: issue id, agent id, branch/worktree, commit set, PR link, test results, model usage, verdict. AC: any merged PR can be traced to a complete run record queryable from the CLI.
- **F2 Event-driven dispatch.** Forgejo webhook (already HMAC-verified) on issue open/assign/label + schedule triggers start runs. AC: labeling an issue `agent:build` produces a run without human CLI invocation.
- **F3 Chimera review gate.** Every PR gets a Chimera deliberation before human review; verdict parser fixed (APPROVE/REJECT precedence); REJECT blocks merge admission. AC: a deliberately-broken PR is rejected automatically with the reasoning attached.

**P1 — orchestration depth:**
- **F4 Workspace leases.** Worktree-per-agent with lease object (agent, task, branch, workspace, budget, expiry, parent); stale-lease reaper. Mirrors Anthropic task-locks + OpenAI per-worktree booting.
- **F5 Spec-convergence loop** (`helix converge`). Check implementation against specs; file missing tasks automatically. Fixes DF-HELIX-7's root cause by construction.
- **F6 Wave budgeting.** Fan-out requests get a pre-flight cost forecast; per-project/per-wave caps enforced before launch.

**P2 — product surface:**
- **F7 Maintenance pods.** Scheduled agents for dependency updates, security patches, coverage — the Jules-proactive pattern, self-hosted.
- **F8 Rich lifecycle states.** planned / admitted / running / awaiting-evidence / awaiting-review / blocked / superseded / merged / reverted / failed (replaces open/closed).
- **F9 PR volume tooling.** Queueing, dedup, stale-run recovery, review-load metrics (acceptance rate, review latency, rework cycles, rollback rate).

## 6. Spec upgrade list (specs v2.0 wave)

1. `SPECIFICATION.md` — §2 Data Flow: add Run + Lease objects; §1.5 12-step flow → event-driven flow; bump version to 2.0.0, date to 2026-11.
2. NEW `specs/event-driven-dispatch.md` — webhook/label/schedule → run admission contracts (F2).
3. NEW `specs/spec-convergence.md` — `helix converge` contract, Spec Kit-style phases (F5).
4. NEW `specs/run-objects.md` — run record schema + query surface (F1).
5. `specs/pr-negotiation.md` — Chimera-first review gate; verdict parse contract (APPROVE-before-REJECT bug becomes a stated MUST NOT); timeout default with verdict (F3).
6. `specs/cost-estimator.md` — extend to wave-level budget admission (F6).
7. `specs/agent-identity.md` — read-only analyst agent class (no write credentials) per Cognition guidance.
8. `specs/deployment.md` — self-hosted runner plane hardening + Jules-style lifecycle-risk statement.

## 7. Build order

Wave 1 (unblocks everything, already CoS hygiene initiative t_01f4e472): board lint, dup ids, red main (DF-HELIX-6/12, INT-CI-3).
Wave 2: F1 run objects + F3 Chimera gate (parser fix is small, high-value).
Wave 3: F2 event-driven dispatch + F4 leases.
Wave 4: F5 convergence + F6 wave budgeting.
Wave 5: F7–F9 product surface.

## 8. Sources (primary, with ages at 2026-11-15)

Anthropic: [best practices](https://www.anthropic.com/engineering/claude-code-best-practices) · [C-compiler experiment 2026-02-05](https://www.anthropic.com/engineering/building-c-compiler) · [autonomy + Agent SDK 2025-09-29](https://www.anthropic.com/news/enabling-claude-code-to-work-more-autonomously) · [Claude Code on the web 2026-09-23](https://www.anthropic.com/news/claude-code-on-the-web) · [Opus 4.8 dynamic workflows 2026-05-28](https://www.anthropic.com/news/claude-opus-4-8) · [internal AI-at-work study 2025-12-02](https://www.anthropic.com/research/how-ai-is-transforming-work-at-anthropic) · [400k-session analysis](https://www.anthropic.com/research/claude-code-expertise)
OpenAI: [introducing Codex 2025-05-16](https://openai.com/index/introducing-codex/) · [Codex upgrades 2025-09-23](https://openai.com/index/introducing-upgrades-to-codex/) · [Codex app 2026-02-02](https://openai.com/index/introducing-the-codex-app/) · [harness engineering 2026-02-11](https://openai.com/index/harness-engineering/) · [Symphony 2026-04-27](https://openai.com/index/open-source-codex-orchestration-symphony/) · [DevDay 2026-09-29](https://openai.com/index/devday-2026-recap/) · [Codex docs](https://developers.openai.com/codex/llms-full.txt)
Ecosystem: [Copilot coding agent 101 2025-05-19](https://github.blog/ai-and-ml/github-copilot/github-copilot-coding-agent-101-getting-started-with-agentic-workflows/) · [Copilot app 2026-06-02](https://github.blog/news-insights/product-news/github-copilot-app-the-agent-native-desktop-experience/) · [Agentic Workflows GA 2026-10-28](https://github.blog/changelog/2026-10-28-github-agentic-workflows-generally-available/) · [Spec Kit](https://github.github.com/spec-kit/) · [spec-driven.md](https://github.com/github/spec-kit/blob/main/spec-driven.md) · [Jules 2025-05-20](https://blog.google/innovation-and-ai/models-and-research/google-labs/jules/) · [Jules proactive](https://blog.google/innovation-and-ai/technology/developers-tools/jules-proactive-updates/) · [Jules read-only 2026-10-29](https://jules.google/docs/changelog/2026-10-29/) · [Cursor cloud agents](https://cursor.com/docs/cloud-agent) · [Cursor self-hosted 2026-03-25](https://cursor.com/blog/self-hosted-cloud-agents) · [Cognition multi-agents](https://cognition.com/blog/multi-agents-working) · [Devin manages Devins 2026-03](https://cognition.com/blog/devin-can-now-manage-devins) · [OpenHands Agent Canvas 2026-06-16](https://www.openhands.dev/blog/introducing-agent-canvas) · [Aider git/repomap](https://aider.chat/docs/git.html) · [33k-PR study arXiv 2601.15195](https://arxiv.org/html/2601.15195) · [git alternatives for agents](https://repowise.dev/blog/comparisons/git-alternatives-for-ai-agents)

*Research passes: 3 parallel subagents (Anthropic / OpenAI / ecosystem), 10 + 10 + 28 sources; full transcripts in delegation deleg_07e5192f.*
