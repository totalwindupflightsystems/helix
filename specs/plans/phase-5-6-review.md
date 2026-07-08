# Helix Phase 5–6 Resolution Plan: Pre-Commit Verification + Code Review

**Status:** Draft  
**Last Updated:** 2026-07-07  
**Depends on:** `specs/interaction-map.md`, `specs/adversarial-review.md`, `specs/trust-model.md`  
**Covers interaction points:** 5.1, 5.2, 5.3, 6.1, 6.2, 6.3

---

## Executive Summary

Phases 5 and 6 are the most critical pair in the Helix interaction map — this is where the **human change management** view and the **agent verification** view diverge into two distinct interfaces that serve different users from the same underlying event stream. Phase 5 (Pre-Commit Verification) enforces mechanical quality gates that no agent can bypass. Phase 6 (Code Review) provides both a human-facing risk assessment dashboard (blast radius, architecture fit, tradeoffs) and an agent-facing verification pipeline (binary criteria, evidence bundles, bias-stripped messages, multi-model adversary review).

The `pkg/review` package is substantially complete — it provides the adversarial review pipeline, evidence bundles, false positive tracking, chain of custody, tier scaling, model rotation, and consensus formatting. What's missing is the **integration surface**: making the pre-commit hooks unbypassable for agents, rendering the human change management dashboard, routing reviews to the right humans and agents, and wiring the review pipeline end-to-end through Forgejo/Chimera/GitReins.

---

## Phase 5: Pre-Commit Verification

### 5.1 Tier 1 Guards (Static)

**Interaction type:** GATE  
**Participants:** agent ↔ system  
**Interaction map reference:** Phase 5, §5.1

#### Human Change Management View

The human sees **nothing** at this stage — Tier 1 is mechanical and should be invisible when it passes. When it fails, the human sees:
- Which specific guard failed (secrets, lint, test, build)
- The failing file/location and error message
- Whether the agent attempted a bypass (e.g., `--no-verify`)
- The agent's identity and trust tier for context

Humans want to know: "Is this agent habitually failing Tier 1? Is this a systemic problem or a one-off?" The change management view surfaces **agent quality trends** at Tier 1 — if an agent repeatedly pushes code that fails lints or tests, its trust score declines and review depth increases per `tier_scaling.go`.

#### Agent Verification View

The agent sees:
- **Criteria (binary):** secrets scan passes (0 findings), lint passes (0 errors), test suite passes (diff mode), build succeeds
- **Evidence:** per-guard output captured as structured log lines — gitleaks report, linter output, test result XML/JSON, build stdout
- **Bias-stripped messages:** N/A at Tier 1 — the commit message hasn't been generated yet
- **Bypass detection:** any `--no-verify` flag usage by an agent is a trust violation event logged to the trust ledger

#### What's Already Built

| Component | File(s) | Status |
|-----------|---------|--------|
| GitReins Tier 1 (secrets, lint, test, build) | `gitreins-poc` — `mcp_gitreins_guard_run` tool | **Operational** — runnable via MCP |
| GitReins pre-commit hook config | `gitreins-poc` — hook template generation | **Operational** — generates `.git/hooks/pre-commit` |
| Agent bypass prohibition | `specs/adversarial-review.md` §Integration Points | **Spec only** — no enforcement code |

#### What's Missing

1. **Agent bypass enforcement.** The GitReins pre-commit hook must detect whether the committer is a Helix agent (by checking registered agent signing keys or `Co-authored-by:` trailer) and **reject** any `--no-verify` flag. Today, the hook is just a template — agents can pass `--no-verify` like any human.

2. **Helix-side hook registration.** When `helix-identity sync` provisions an agent in Forgejo, it must also install the agent-specific GitReins hooks into the agent's worktree. Currently, hooks are repo-scoped and not agent-aware.

3. **Tier 1 failure → trust ledger event.** Every Tier 1 failure by an agent must be recorded in the trust ledger as a `guard_failure` event with the failing check type and diff context. This feeds `TierScaling` — Provisional agents with high Tier 1 failure rates get deeper review.

4. **Diff-mode test enforcement.** The spec says "test mode is diff by default" — the pre-commit hook must use `--diff-filter=AM` to only run tests for changed files. GitReins currently runs full suites; diff-mode detection needs implementation.

5. **Evidence capture to DuckBrain.** Every Tier 1 pass/fail result must be persisted as a structured memory under `/helix/guard/{agent_id}/{commit_sha}`. Currently, results are ephemeral — displayed on stdout and lost.

#### Acceptance Criteria

- [ ] `helix-identity sync` installs agent-specific pre-commit hooks that reject `--no-verify`
- [ ] Tier 1 failures by agents create trust ledger events
- [ ] Diff-mode test selection is default for agent commits
- [ ] Evidence of every Tier 1 run is persisted to DuckBrain
- [ ] `helix review` CLI can query Tier 1 history for an agent: `helix review guard-history --agent <id> --since <date>`

---

### 5.2 Commit Attestation

**Interaction type:** GATE  
**Participants:** agent ↔ system  
**Interaction map reference:** Phase 5, §5.2

#### Human Change Management View

The human sees a commit's **provenance chain**:
- Who wrote the code (author)
- Which agent co-authored it (`Co-authored-by:` trailer)
- Which prompt version was used (`Prompt:` trailer with SHA-256 hash)
- Whether the prompt hash matches the registered prompt in the prompt registry
- Whether the commit is ED25519-signed by the agent's key

The change management dashboard shows a **prompt drift indicator**: if the prompt hash doesn't match the registry, the commit is flagged with a warning — "This code may have been produced with an unauthorized or modified prompt." For audit purposes, the human can trace every commit back to the exact prompt and agent that created it.

#### Agent Verification View

The agent sees:
- **Criteria (binary):** commit-msg hook passes (trailers present, prompt hash matches), ED25519 signature valid
- **Evidence:** the commit object itself (SHA, trailers), the prompt registry entry, the signature verification result
- **Bias-stripped messages:** N/A — attestation is about metadata, not message content (though the commit message will be bias-stripped before review in 6.2)

#### What's Already Built

| Component | File(s) | Status |
|-----------|---------|--------|
| ED25519 key generation and signing | `pkg/review/evidence.go` — `GenerateKeyPair()`, `SignBundle()`, `VerifySignature()` | **Complete** — used for evidence bundles |
| Prompt registry (PromptFoo CI) | `specs/prompt-registry.md`, `specs/prompt-registry-v2.md` | **Spec only** — registry not implemented |
| Commit-msg hook spec | `specs/interaction-map.md` §5.2 | **Spec only** — no hook code |

#### What's Missing

1. **Commit-msg hook implementation.** A Git hook that:
   - Parses the commit message for `Co-authored-by:` and `Prompt:` trailers
   - For agent commits (detected via `Co-authored-by:` matching a registered agent), validates that:
     - `Prompt:` trailer is present
     - Hash matches the prompt registry (`helix-prompt verify --hash <sha>`)
   - Rejects commits missing either trailer for agent authors

2. **Prompt registry client.** `helix-prompt` must expose a `verify --hash <sha>` subcommand that checks whether a prompt hash exists in the registry. Currently `helix-prompt` only manages local prompts.

3. **Agent key provisioning for commits.** `helix-identity sync` must generate an ED25519 key pair per agent, store the private key in the agent's sandboxed worktree, and register the public key in Forgejo as a deploy key. The commit-msg hook uses the worktree-local key to sign.

4. **Commit signing.** The `prepare-commit-msg` or `commit-msg` hook must call `helix review evidence sign --key-role primary --key-path <agent_private_key>` against the commit object, appending the signature as an `X-Helix-Signature:` trailer.

5. **Signature verification at push/review time.** The pre-push hook (5.3) or the review orchestrator (6.2) must verify the `X-Helix-Signature` trailer against the agent's registered public key.

#### Acceptance Criteria

- [ ] `helix-identity sync` provisions ED25519 keys for each agent
- [ ] Commit-msg hook enforces `Co-authored-by:` and `Prompt:` trailers for agent commits
- [ ] `helix-prompt verify --hash <sha>` validates against the prompt registry
- [ ] Agent commits are ED25519-signed with `X-Helix-Signature:` trailer
- [ ] Unsigned agent commits are rejected at push time
- [ ] `helix review attestation verify <commit_sha>` validates signature + prompt hash

---

### 5.3 Pre-Push Verification

**Interaction type:** GATE  
**Participants:** agent ↔ system  
**Interaction map reference:** Phase 5, §5.3

#### Human Change Management View

The human sees a **pre-push gate status** per branch per agent. When a push is blocked:
- Which guard failed (full test suite, lint, build)
- How long the full suite took (latency tracking)
- Whether the agent is in a "push-retry loop" (≥3 push attempts blocked within 10 minutes → agent flagged)
- Cost impact: how many CI minutes were burned on failed pushes

The human can configure per-repo push policies:
- Contract changes: full integration suite required
- Cosmetic changes: push allowed with only Tier 1 passed
- Trust-tier overrides: Veteran agents may push cosmetic changes with reduced verification

#### Agent Verification View

The agent sees:
- **Criteria (binary):** full test suite passes, lint passes, build succeeds, signature verified, prompt hash matches
- **Evidence:** aggregated test results, lint output, build logs, signature verification result
- **Bias-stripped messages:** N/A
- **Bypass detection:** same as 5.1 — any force-push or bypass attempt is a trust violation

#### What's Already Built

| Component | File(s) | Status |
|-----------|---------|--------|
| GitReins full test suite | `gitreins-poc` — `guard_run` with full suite flag | **Operational** |
| Pre-push hook config | `gitreins-poc` — hook template for pre-push | **Operational** — generates `.git/hooks/pre-push` |
| Merge gate CLI (Phase 8 integration) | `cmd/helix/mergegate.go` | **Complete** — wraps evidence, consensus, contract, trust, cost checks |

#### What's Missing

1. **Pre-push bypass prohibition.** Same as 5.1 — the hook must reject `--no-verify` and `--force` for agent pushes. Force-push to `main`/`master` by any agent is always blocked regardless of trust tier.

2. **Trust-tier push policies.** `TierScaling` in `pkg/review/tier_scaling.go` defines review policies but not push policies. Need a `TierPushPolicy` that maps tiers to allowed push bypasses:
   - Provisional: no bypasses, full suite always
   - Observed: cosmetic changes skip full suite
   - Trusted: behavioral changes skip full suite, contract still requires full
   - Veteran: only contract changes require full suite

3. **Branch protection integration.** The pre-push hook must integrate with Forgejo branch protection rules so that even if the hook is somehow bypassed, the Forgejo server rejects the push. The `helix-identity sync` flow should configure branch protection via Forgejo API.

4. **Push-retry loop detection.** The push hook must check recent push history for the agent+branch combination. If an agent pushes → blocked → pushes again ≥3 times within 10 minutes, the agent is flagged for human review and the branch is temporarily locked.

5. **Cost tracking for CI minutes.** Every pre-push run must emit a cost event (CI minutes × compute cost rate) to LangFuse for the cost reconciliation engine (Phase 8.2).

#### Acceptance Criteria

- [ ] `git push --force` and `git push --no-verify` blocked for agents on protected branches
- [ ] Trust-tier push policies implemented and configurable per-repo
- [ ] Forgejo branch protection rules auto-configured during `helix-identity sync`
- [ ] Push-retry loop detection flags agents after 3+ blocked pushes in 10 minutes
- [ ] CI cost events emitted to LangFuse for every pre-push run
- [ ] `helix review push-history --agent <id>` shows blocked push count and reasons

---

## Phase 6: Code Review

### 6.1 Human Review Interface (Change Management)

**Interaction type:** GATE → COLLABORATION  
**Participants:** human ↔ system, human ↔ agent  
**Interaction map reference:** Phase 6, §6.1

#### Human Change Management View

This is the **executive summary** a human sees before approving or rejecting a change. It answers: "Do I understand the risk and impact of this change?" The human **approves the CHANGE, not the code** — code-level verification is the agents' job.

The dashboard shows:

| Section | Content | Source |
|---------|---------|--------|
| **Why this change?** | Bias-stripped commit intent + linked spec/issue | `pkg/review/bias_stripper.go` |
| **Blast radius map** | Affected files → transitive dependents → services impacted | Codebase dependency graph (not yet built) |
| **Risk score** | 0–100 composite: incident correlation + change category + agent trust tier | Incident database (not yet built) + `pkg/review/tier_scaling.go` |
| **Architecture fit** | ADR lineage check: does this change align with existing ADRs? | ADR registry (Phase 2.2) |
| **Tradeoffs made** | What alternatives were considered? What was rejected? | Agent-authored ADRs (Phase 2.2) |
| **Edge cases considered** | List of edge cases the agent claims to have addressed | Extracted from agent's self-verification evidence |
| **Agent review verdict** | Consensus summary from adversarial review: approved/blocked/tie-break | `pkg/review/consensus_report.go` — `FormatConsensusReport()` |
| **Evidence bundle link** | Direct link to the signed evidence bundle JSON | `pkg/review/evidence.go` — `BundleID()` |

The human has three possible actions:
1. **Approve** — merge is allowed; agent and human signatures both recorded
2. **Request changes** — specific feedback is attached to the PR; agent receives structured feedback
3. **Override agent block** — human overrides an agent's block verdict (rare, recorded in trust ledger as `human_override`)

#### Agent Verification View

N/A — this is the human-facing side. The human sees the output of the agent's verification work but doesn't perform the verification.

#### What's Already Built

| Component | File(s) | Status |
|-----------|---------|--------|
| Consensus report formatter | `pkg/review/consensus_report.go` — `FormatConsensusReport()` | **Complete** — renders markdown tables for Forgejo PR comments |
| Evidence bundle structure | `pkg/review/evidence.go` | **Complete** — all fields for the dashboard |
| Tier scaling (risk from trust tier) | `pkg/review/tier_scaling.go` | **Complete** — `ReviewDepthSummary()` |
| False positive tracker (model quality) | `pkg/review/false_positive.go` | **Complete** |
| Chain of custody for audit trail | `pkg/review/custody.go` | **Complete** |

#### What's Missing

1. **Blast radius engine.** A codebase dependency graph that, given a set of changed files, walks the import/call graph to identify:
   - Direct dependents (files that import changed files)
   - Transitive dependents (services that call changed endpoints)
   - Affected API consumers (from OpenAPI consumer registry — Phase 2.4)
   - This is the single most impactful missing piece for human change management.

2. **Risk score computation.** A scoring function combining:
   - Change category weight (contract=40, behavioral=25, resilience=15, cosmetic=5)
   - Agent trust tier modifier (provisional=×2.0, observed=×1.5, trusted=×1.0, veteran=×0.5)
   - Incident correlation: has the changed code path been implicated in past incidents?
   - File criticality: are the changed files in hot paths or rarely-touched areas?
   - This requires the incident database from Phase 10 and file criticality metadata (currently doesn't exist).

3. **Human review dashboard (UI).** A web interface or Forgejo PR template that renders the change management view. Currently, `FormatConsensusReport()` produces markdown suitable for PR comments, but there's no dedicated dashboard. Options:
   - **Option A:** Forgejo PR template that embeds the consensus report as a comment → low effort, high value
   - **Option B:** Standalone web dashboard served by Muster API → higher effort, richer UX
   - **Recommendation:** Start with Option A (PR comment integration), build Option B when human reviewer adoption demands it.

4. **Human override recording.** When a human overrides an agent's block verdict, the override must:
   - Record a `human_override` event in the trust ledger with reason
   - Feed the dismissal into `FPTracker.RecordDismissal()` for the blocking model
   - If the human consistently overrides, reduce the weight of that human's feedback (per §7.2)

5. **Edge case extraction.** The agent's self-verification step (4.3) must produce a structured list of edge cases considered, not just raw test output. This list feeds the human-facing "Edge cases considered" section.

#### Acceptance Criteria

- [ ] Blast radius map generated from codebase dependency graph for any PR
- [ ] Risk score computed and displayed in change management view
- [ ] Human review dashboard renders as Forgejo PR comment via `FormatConsensusReport()`
- [ ] Human override actions recorded in trust ledger with reason
- [ ] Override feeds false positive tracker for the overridden model
- [ ] Edge case list extracted from agent self-verification and displayed to human

---

### 6.2 Agent Review Interface (Verification)

**Interaction type:** GATE  
**Participants:** agent ↔ system, agent ↔ agent  
**Interaction map reference:** Phase 6, §6.2

#### Human Change Management View

N/A — this is the agent-to-agent interface. Humans see the **output** (the consensus report from 6.1) but don't interact with the verification pipeline directly.

#### Agent Verification View

This is the core of Helix's differentiation — adversarial multi-model review. The agent sees:

1. **Structured acceptance criteria (binary pass/fail):**
   - Per change category, derived from `specs/adversarial-review.md` §Review Criteria by Change Category
   - Contract changes: all 3 models must return `approved` or `pass_with_notes`
   - Behavioral changes: 2 of 2 models must approve
   - Resilience changes: primary approves + property tests pass
   - Cosmetic changes: primary approves, auto-merge if Tier 1 green

2. **Bias-stripped commit message:**
   - The original commit message is run through `BiasStripper.StripPreservingPrefix()`
   - All evaluative language ("fixed", "all tests pass", "ready to merge"), confidence assertions ("tested locally", "works on my machine"), emoji, and emotional framing are removed
   - The original is archived (SHA-256 stored in evidence bundle) but **never shown to reviewers**
   - All 3 review models receive the stripped version

3. **Evidence bundles:**
   - Signed with ED25519 by each review model (primary, adversarial, audit)
   - Hash-chained for tamper detection (`ChainOfCustody`)
   - Stored in `~/.helix/evidence/` via `EvidenceStore`
   - Each finding must cite a specific file, line, and test evidence

4. **Multi-model adversary findings:**
   - **Primary reviewer:** structural correctness, code quality, test coverage
   - **Adversarial reviewer:** find what primary missed — race conditions, edge cases, security issues
   - **Audit reviewer:** verify adversarial's claims are real (not hallucinations)
   - Consensus resolved by `resolveConsensus()` in `pkg/review/evidence.go`

5. **False positive feedback:**
   - Human dismissals increment `FPTracker.RecordDismissal(modelID)`
   - 10 dismissals → flag for re-evaluation
   - >15% FP rate → model removed from rotation
   - Affects `ReviewOrchestrator.ValidatePanel()` which rejects removed models

#### What's Already Built

| Component | File(s) | Status |
|-----------|---------|--------|
| BiasStripper | `pkg/review/bias_stripper.go` | **Complete** — 80+ patterns, emoji stripping, whitespace normalization |
| EvidenceBundle | `pkg/review/evidence.go` | **Complete** — Formation, Findings, Consensus, ED25519 signatures, canonical hashing |
| ReviewOrchestrator | `pkg/review/orchestrator.go` | **Complete** — concurrent 3-model dispatch, consensus reconciliation, provider diversity validation |
| EvidenceVerifier (Tier 3) | `pkg/review/verification.go` | **Complete** — test runner interface, finding classification, verification report |
| AdversarialAgentDispatcher | `pkg/review/agents.go` | **Complete** — @assumption-buster, @redteam, @chaos-engineer, @cost-auditor triggers |
| False positive tracker | `pkg/review/false_positive.go` | **Complete** — 10-dismissal flag, 15% FP rate removal |
| ChainOfCustody | `pkg/review/custody.go` | **Complete** — event trail, tamper detection, signing/re-signing |
| EvidenceStore | `pkg/review/store.go` | **Complete** — disk-based persistence, search, list by agent/PR |
| ConsensusReport | `pkg/review/consensus_report.go` | **Complete** — markdown table rendering |
| TierScaling | `pkg/review/tier_scaling.go` | **Complete** — 4 tiers, formation adjustment |
| ModelRotation | `pkg/review/rotation.go` | **Complete** — FormationAssigner with provider/RLHF diversity |
| ContractGenerator | `pkg/review/contract_gen.go` | **Complete** — findings → behavior contract assertions |
| CLI surface | `cmd/helix/review.go` | **Complete** — strip-bias, fp-stats, fp-record, evidence, custody subcommands |

#### What's Missing

1. **Model client implementations.** `pkg/review/orchestrator.go` defines `ModelClient` interface but has **no real implementations**. The orchestrator dispatches to `ModelClient.Review()` but only `StubAgent` exists for testing. Real model clients that call Chimera/OpenRouter/DeepSeek APIs are unimplemented.

2. **Chimera integration.** The review pipeline needs to instantiate `ModelClient` implementations that call Chimera formations:
   ```
   Primary reviewer → Chimera with formation: [GPT-5.5]
   Adversarial reviewer → Chimera with formation: [DeepSeek-V4]
   Audit reviewer → Chimera with formation: [Owl-Alpha]
   ```
   Or use Chimera's multi-model deliberation directly for the full adversarial panel.

3. **Per-change-category prompt templates.** Each review role needs version-locked prompt templates (per spec §Anti-Overcorrection Protocol: "Review prompts are version-locked and hash-attested"):
   - `prompts/review/primary-contract.txt`
   - `prompts/review/primary-behavioral.txt`
   - `prompts/review/adversarial-contract.txt`
   - `prompts/review/audit.txt`
   - These must be registered in the prompt registry and hash-verified per review.

4. **Forgejo PR event → review trigger.** The Forgejo webhook must trigger the review pipeline when:
   - A PR is opened by an agent
   - A PR is updated with new commits by an agent
   - `pkg/webhook/forgejo.go` exists but only handles basic webhook events, not PR review dispatch.

5. **Real `TestRunner` implementation.** `pkg/review/verification.go` defines `TestRunner` interface but only `NoopTestRunner` exists. A real runner must execute `go test -run <test_name>` (or equivalent) in the agent's sandboxed worktree and capture pass/fail.

6. **Evidence bundle → DuckBrain persistence.** `EvidenceStore` writes to disk (`~/.helix/evidence/*.json`) but the spec says bundles should be stored in DuckBrain with memory keys like `/helix/review/{review_id}`. Need a `DuckBrainStore` adapter or direct DuckBrain writes.

7. **Tier 3 evidence verification integration.** After the adversarial review produces findings, the `EvidenceVerifier` must run independently (different agent, different model, or pure test execution) to verify each finding. Currently the verifier exists in code but isn't wired into the pipeline.

#### Acceptance Criteria

- [ ] `ModelClient` implementations exist for Chimera, OpenRouter, and direct provider APIs
- [ ] Review pipeline triggers automatically on Forgejo PR open/update events
- [ ] Per-change-category prompt templates registered and hash-verified
- [ ] Real `TestRunner` implementation executes tests in sandboxed worktrees
- [ ] Evidence bundles persisted to DuckBrain under `/helix/review/{review_id}`
- [ ] Tier 3 evidence verification runs independently after Tier 2 adversarial review
- [ ] `helix review run --pr <url>` runs the full pipeline end-to-end and produces a consensus report
- [ ] False positive tracker persists across reviews and feeds model rotation decisions

---

### 6.3 Review Load Balancing

**Interaction type:** HANDOFF  
**Participants:** system ↔ human, system ↔ agent  
**Interaction map reference:** Phase 6, §6.3

#### Human Change Management View

Humans see a **review queue** that filters to changes requiring human judgment. The system routes PRs so humans only see:
- Architectural decisions (changes that require human intent approval)
- Novel patterns (code that doesn't match any known safe pattern)
- Rejected agent reviews (agent blocked but author appealed)
- Override requests (human dismissed a valid agent finding — needs review)
- Trust-tier escalation (Provisional agent's work that agent reviewers couldn't agree on)

Humans do **not** see:
- Cosmetic changes that passed Tier 1 + single-model review
- Changes from Veteran agents where all gates are green
- Changes where 3/3 models unanimously approved

The human review queue has:
- **Priority scoring:** risk × staleness (how long the PR has been waiting)
- **Estimated review time:** based on diff size and change complexity
- **Expertise matching:** which humans have reviewed similar code paths before

#### Agent Verification View

Agents see a **verification queue** where they are dispatched based on:
- **Trust tier:** Provisional agents can only review within their allowed file categories; Veteran agents can review anything
- **Domain expertise:** marketplace-published skills (Phase 12.2) determine which agents are best suited
- **Current load:** number of active reviews per agent, token budget remaining
- **Cost profile:** cheaper agents preferred for cosmetic/behavioral reviews

The dispatcher ensures:
- Every PR gets at least one human review (for intent) unless it's a Veteran-tier cosmetic change
- Every PR gets agent review depth proportional to risk and agent trust tier
- No agent reviews its own code (identity check)
- No single agent is a bottleneck (load distribution)

#### What's Already Built

| Component | File(s) | Status |
|-----------|---------|--------|
| Agent dispatcher (Phase 3.2) | `cmd/helix/dispatcher.go` | **Partial** — dispatches tasks, not reviews |
| Review orchestrator (concurrent dispatch) | `pkg/review/orchestrator.go` | **Complete** — concurrent model dispatch per review |
| Tier scaling (depth based on trust) | `pkg/review/tier_scaling.go` | **Complete** — `AdjustFormation()`, `PolicyForTier()` |
| Model rotation (fair assignment) | `pkg/review/rotation.go` | **Complete** — `FormationAssigner` with diversity enforcement |
| Adversarial agent dispatch | `pkg/review/agents.go` | **Complete** — `AdversarialAgentDispatcher` with triggers |

#### What's Missing

1. **Review queue data structure.** A persistent queue that tracks:
   - PR URL, author agent, change category, risk score, time submitted
   - Assigned reviewers (human + agent), review status (pending/in-progress/complete)
   - Priority score, estimated review time
   - This should be stored in DuckBrain under `/helix/queue/review/{pr_id}`

2. **Review assignment algorithm.** A load balancer that:
   - Queries active agent load from `helix-identity status` (needs load tracking)
   - Matches PR requirements (category, risk, trust tier) to agent capabilities
   - Ensures provider diversity (no two same-provider models on same review)
   - Respects agent token budgets (`CostEstimate` from `AgentResult`)
   - Falls back to human assignment when no agent is available

3. **Human review queue filtering.** Rules for what a human MUST see vs. what can be auto-merged:
   - `MustSeeHuman`: architectural change, first-time pattern, agent consensus tie-break, trust override
   - `CanAutoMerge`: cosmetic change + Veteran agent + Tier 1 pass + single-model approve
   - Configurable per-repo in `~/.helix/config.yaml`

4. **Review SLA tracking.** Every review assigned to a human must track:
   - Time to first review (SLA: 4 business hours for contract, 24h for behavioral)
   - Time to resolution (SLA: based on risk score)
   - Escalation path when SLA is breached
   - This feeds the trust ledger for human review responsiveness

5. **Self-review prevention.** The dispatcher must check that no agent is assigned to review its own PR. This requires the agent identity propagation chain:
   - Commit → `Co-authored-by:` trailer → agent ID
   - PR author (Forgejo) → agent ID
   - Review panel → exclude self

6. **Load-aware model selection.** `FormationAssigner` in `pkg/review/rotation.go` selects models based on rotation history but doesn't consider:
   - Current load (how many concurrent reviews each model is handling)
   - Token budget remaining
   - Cost (cheapest model that meets requirements)
   - Latency SLA (some reviews need fast turnaround)

#### Acceptance Criteria

- [ ] Review queue persists to DuckBrain with priority scoring
- [ ] Review assignment algorithm distributes load across agents and humans
- [ ] Human review queue filters to changes requiring human judgment only
- [ ] Self-review prevention: agents never review their own PRs
- [ ] Review SLA tracking with configurable timeouts per change category
- [ ] Load-aware model selection considers budget, latency, and concurrency
- [ ] `helix review queue --status` shows all pending/in-progress reviews
- [ ] `helix review assign --pr <url>` triggers automatic reviewer assignment

---

## Cross-Cutting Concerns

### Integration Wiring (Forgejo → Review Pipeline → Merge Gate)

```
PR opened/updated by agent
    │
    ├─[5.1] Forgejo webhook → GitReins Tier 1 (pre-commit hook already ran)
    │
    ├─[5.2] Commit attestation verified (signature + prompt hash)
    │
    ├─[5.3] Pre-push verification result checked (was push clean?)
    │
    ├─[6.2] Review pipeline triggered:
    │   ├── BiasStripper processes commit message
    │   ├── ReviewOrchestrator dispatches 1-3 models
    │   ├── AdversarialAgentDispatcher dispatches prosecutor agents
    │   ├── Consensus resolved (approved/blocked/tie-break)
    │   ├── Evidence bundle signed and stored
    │   └── ConsensusReport posted as Forgejo PR comment
    │
    ├─[6.1] Human review interface populated:
    │   ├── Blast radius computed
    │   ├── Risk score computed
    │   └── Change management dashboard rendered
    │
    ├─[6.3] Review load balanced:
    │   ├── Human assigned if change requires human judgment
    │   └── SLA timer started
    │
    └─[Phase 8] Merge gate:
        ├── Evidence bundle valid?
        ├── Consensus threshold met?
        ├── Behavior contract present?
        ├── Trust tier sufficient?
        └── Cost within budget?
```

### State Dependencies

| Phase | Depends On | Status |
|-------|-----------|--------|
| 5.1 | GitReins (operational), Agent identity (Phase 1→Feature 1) | Identity provisioning needs hook installation |
| 5.2 | Prompt registry (Phase 2→Feature 4), Agent ED25519 keys (Phase 3, Phase 11) | Prompt registry not implemented |
| 5.3 | 5.1 + 5.2 + Forgejo branch protection API | Branch protection not auto-configured |
| 6.1 | Blast radius engine (new), Incident database (Phase 10), ADR registry (Phase 2.2) | Blast radius + incident DB not built |
| 6.2 | Chimera (operational), ModelClient implementations (new), Review prompts (new), DuckBrain integration (new) | Model clients + prompts + DuckBrain needed |
| 6.3 | Agent load tracking (new), Review queue (new), Self-review prevention (new) | All new components |

### Risk Map

| Risk | Probability | Impact | Mitigation |
|------|------------|--------|------------|
| Chimera API changes break review pipeline | Medium | High | Version-locked API contract + integration test in CI |
| ModelClient implementations are provider-specific and fragile | High | Medium | Abstract behind `ModelClient` interface with provider adapters |
| Blast radius engine is complex (call graph analysis) | High | Medium | Start with file-level `import` analysis; add service-level later |
| Prompt templates need continuous tuning | Medium | Low | PromptFoo CI regressions per spec §8.3 |
| DuckBrain performance under review load | Low | Medium | Batch writes + evidence bundle compression |
| Human reviewer adoption (will they use the dashboard?) | High | Medium | Start with Forgejo PR comments (low friction); add dashboard later |

---

## Implementation Sequence

### Phase 5 — Priority Order

1. **5.1 — Agent bypass enforcement.** Most critical blocker. Without it, agents can skip all verification.
2. **5.2 — Commit-msg hook + prompt hash verification.** Foundation for provenance tracking.
3. **5.1/5.3 — Evidence capture to DuckBrain.** Makes guard results queryable.
4. **5.2 — Agent key provisioning + commit signing.** Completes the attestation chain.
5. **5.3 — Pre-push hook hardening + branch protection.** Locks down push path.
6. **5.3 — Push-retry loop detection + CI cost tracking.**

### Phase 6 — Priority Order

1. **6.2 — ModelClient implementations (Chimera adapter first).** Unlocks real adversarial review.
2. **6.2 — Per-change-category prompt templates.** Required for review quality.
3. **6.2 — Forgejo PR event → review trigger.** Wires the pipeline end-to-end.
4. **6.1 — Blast radius engine (file-level import analysis).** Most impactful for human review.
5. **6.1 — ConsensusReport → Forgejo PR comment integration.** Makes review results visible.
6. **6.3 — Review queue + assignment algorithm.** Prevents reviewer bottleneck.
7. **6.2 — Real TestRunner + Tier 3 evidence verification.** Closes the verification loop.
8. **6.1 — Risk score computation.** Requires incident database (Phase 10 dependency).
9. **6.3 — Human review queue filtering + SLA tracking.**
10. **6.2/6.3 — DuckBrain + load-aware model selection.**

---

## Document Status

- [x] 5.1 Tier 1 Guards: what's built, what's missing, acceptance criteria
- [x] 5.2 Commit Attestation: what's built, what's missing, acceptance criteria
- [x] 5.3 Pre-Push Verification: what's built, what's missing, acceptance criteria
- [x] 6.1 Human Review Interface: what's built, what's missing, acceptance criteria
- [x] 6.2 Agent Review Interface: what's built, what's missing, acceptance criteria
- [x] 6.3 Review Load Balancing: what's built, what's missing, acceptance criteria
- [x] Integration wiring diagram (Forgejo → Review → Merge Gate)
- [x] State dependencies table
- [x] Risk map
- [x] Implementation sequence with priority ordering
