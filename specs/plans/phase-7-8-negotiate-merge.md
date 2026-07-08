# Helix Resolution Plans — Phases 7–8: PR Negotiation + Merge Gating

**Status:** v1.0 — Build-ready resolution plan  
**Last updated:** 2026-07-07  
**References:** `specs/interaction-map.md` §7, §8; `specs/pr-negotiation.md`; `specs/adversarial-review.md`; `specs/trust-model.md`; `specs/cost-estimator.md`; `specs/prompt-registry-v2.md`; `specs/cross-component-wiring.md`  
**Packages:** `pkg/negotiate/`, `pkg/mergegate/`, `pkg/review/`, `pkg/trust/`, `pkg/estimate/`, `pkg/prompt/`, `pkg/dispatcher/`  
**External services:** Chimera (arbiter formation), GitReins (Tier 1+Tier 2 guards), Forgejo (PR comments + branch protection)

---

## Executive Summary

This document provides the detailed resolution plan for each of the 6 interaction points spanning
Phases 7 (PR Negotiation) and 8 (Merge Gating). For every interaction point, we specify:

- The **data flow** — what inputs arrive, how they are processed, and what outputs are produced
- The **existing implementation** — what code already exists in `pkg/negotiate` and `pkg/mergegate`
- The **gaps** — what remains to be wired, tested, or hardened
- The **resolution** — exact steps to close each gap, with references to package files and spec sections
- The **failure modes** — what happens when things go wrong and how escalation cascades

All six interaction points are covered:
| # | Phase | Interaction | Type |
|---|-------|------------|------|
| 7.1 | PR Negotiation | Agent-Agent Disagreement | NEGOTIATION |
| 7.2 | PR Negotiation | Human-Agent Disagreement | NEGOTIATION |
| 7.3 | PR Negotiation | Chimera Tiebreak | NEGOTIATION → GATE |
| 8.1 | Merge Gating | Merge Gate Checks | GATE |
| 8.2 | Merge Gating | Cost Gate | GATE |
| 8.3 | Merge Gating | Prompt Provenance Gate | GATE |

---

## 7.1 — Agent-Agent Disagreement

### Interaction Map Reference

> Two agents disagree on a review finding. Structured debate ensues. Each agent states position
> with evidence. If deadlock, escalate to Chimera as tiebreaker. Helix must provide a structured
> debate protocol where positions are scored for evidence quality.

### Existing Implementation (`pkg/negotiate/`)

The debate protocol is fully specified and partially implemented:

| Component | File | Status |
|-----------|------|--------|
| State machine (9 states) | `types.go` | **Complete** — `State` enum, all transitions |
| Negotiator orchestrator | `negotiator.go` | **Complete** — `Negotiate()`, `Advance()`, state machine loop |
| Debate round management | `debate.go` | **Complete** — round tracking, concession detection, agent turns |
| Evidence validation | `debate_validator.go` | **Complete** — minimum 2 evidence items, spec ref, test output |
| Debate transcript (JSONL) | `transcript.go` | **Complete** — incremental write, replay |
| Audit logger | `audit.go` | **Complete** — append-only JSONL |
| Timeout enforcement | `timeout.go` | **Complete** — 5 min/round, 30 min global |
| Strike system | (in `debate.go`) | **Complete** — 3 strikes → auto-concede |
| Dry-run mode | `dry_run.go` | **Complete** |
| Veto protocol | `veto.go` | **Complete** — validation, frivolous tracking |
| Trust adjustment | `trust_adjustment.go` | **Complete** — deltas per spec §10.2 |
| History/query | `history.go` | **Complete** — `QueryHistory()`, `FormatHistory()` |
| Error taxonomy | `errors.go` | **Complete** — exit codes per spec §14 |

### Resolution Plan

**Gap 1: Forgejo Integration Not Wired**

The negotiator currently operates entirely in-process. It does not post debate comments to
Forgejo PRs. The `Debate` struct tracks rounds in memory but the `PostRound` method (if it
exists) is not connected to the Forgejo PR Review API.

**Resolution:**
1. Add a `ForgejoPoster` interface to `pkg/negotiate/types.go`:
   ```go
   type CommentPoster interface {
       PostComment(ctx context.Context, prOwner, prRepo string, prNumber int, body string) error
   }
   ```
2. Implement in `pkg/negotiate/forgejo_poster.go` using the existing `pkg/forgejo/client.go`
   (`POST /api/v1/repos/{owner}/{repo}/pulls/{index}/reviews`).
3. Add `CommentPoster` as an optional field on `Negotiator`. When set, each `Advance()`
   round posts the debate comment before transitioning state.
4. Wire in `cmd/helix/negotiate.go` when the CLI parses `--forgejo-url`.

**Gap 2: No Live Multi-Agent Execution**

The current tests use mock agents. A real negotiation requires two agents to generate
structured debate comments in response to each other's positions. This requires an
agent executor that calls the LLM from within the debate loop.

**Resolution:**
1. Add `AgentExecutor` interface:
   ```go
   type AgentExecutor interface {
       GeneratePosition(ctx context.Context, agent Agent, pr PRContext, previousRounds []Round) (Position, error)
   }
   ```
2. Implement in `pkg/negotiate/agent_executor.go` using `pkg/integration/chimera_client.go`
   or direct provider API calls.
3. In `Negotiator.Advance()`, after transitioning to a new round, call
   `AgentExecutor.GeneratePosition()` for the agent whose turn it is, then post the
   result via `CommentPoster`.
4. Budget-track each agent's LLM call via `CostReconciler.RecordRoundCost()`.

**Gap 3: Consensus Engine for Multi-Agent Positions**

The `Negotiate()` method on `Negotiator` (the context-based API) only checks if all
positions agree — it doesn't implement the full 3-round debate when they don't. It
short-circuits directly to Chimera.

**Resolution:**
1. Expand `Negotiate()` to use the full state machine when positions disagree:
   - Conflict detected → enter debate loop (max 3 rounds)
   - Rounds complete → check for concession
   - If deadlock → `EscalateToChimera()`
2. The `Negotiate()` method should combine the context-based API with the state-machine API.
3. Add `WithDebateRounds(bool)` option on `NegotiationConfig` to allow callers to skip
   debate and go straight to Chimera (current behavior).

**Verification Gate (for 7.1):**

After wire-up, the following acceptance test must pass:
- Two agents with conflicting Forgejo reviews are detected
- 3-round structured debate posts comments to the PR via Forgejo API
- Each comment meets evidence requirements (≥2 items, ≥1 spec ref)
- Concession in round 2 resolves the debate
- Deadlock after 3 rounds triggers Chimera tiebreak
- Full audit trail in `~/.helix/negotiations/<pr>-<ts>.jsonl`
- Cost tracked per-agent per-round

---

## 7.2 — Human-Agent Disagreement

### Interaction Map Reference

> Human dismisses agent's review finding. Agent must accept but records the dismissal.
> Dismissal feeds false positive tracker. Agent trust is adjusted if pattern of bad
> reviews emerges. Human trust is noted (frequent overrides reduce weight of their agent's reviews).

### Existing Implementation

| Component | File | Status |
|-----------|------|--------|
| False positive tracker | `pkg/review/false_positive.go` | **Complete** — `RecordDismissal()`, model penalty, 10-dismissal threshold, 15% re-evaluation |
| Trust adjustment for overrides | `pkg/negotiate/trust_adjustment.go` | **Partial** — has negotiation trust deltas but no human-override-specific deltas |
| Human dismissal structured format | — | **Not implemented** |
| Review weight decay for frequent overriders | — | **Not implemented** |

### Resolution Plan

**Gap 1: Structured Dismissal Protocol**

There is no structured comment format for a human dismissing an agent's review finding.
The interaction map requires: "Structured dismissal with reason. Dismissal feeds false
positive tracker."

**Resolution:**
1. Define the `DISMISS:` comment format in `pkg/negotiate/types.go`:
   ```
   DISMISS: <finding-id>
   Reason: <one of: false_positive|already_handled|out_of_scope|architectural_decision>
   Note: <free-text explanation>
   ```
2. Add `Dismissal` struct to `pkg/negotiate/types.go`:
   ```go
   type Dismissal struct {
       FindingID  string          `json:"finding_id"`
       Reason     DismissalReason `json:"reason"`
       Note       string          `json:"note"`
       HumanID    string          `json:"human_id"`
       AgentID    string          `json:"agent_id"`
       PRNumber   int             `json:"pr_number"`
       Timestamp  time.Time       `json:"timestamp"`
   }
   type DismissalReason string
   const (
       DismissalFalsePositive       DismissalReason = "false_positive"
       DismissalAlreadyHandled      DismissalReason = "already_handled"
       DismissalOutOfScope          DismissalReason = "out_of_scope"
       DismissalArchitecturalDecision DismissalReason = "architectural_decision"
   )
   ```
3. Add `DismissalHandler` to `pkg/negotiate/negotiator.go` that:
   - Parses `DISMISS:` comments from PR comment stream
   - Calls `pkg/review/false_positive.go` → `RecordDismissal()` for `false_positive`
   - Logs all dismissals to the negotiation audit trail
   - For non-false-positive reasons, records but does NOT penalize the model

**Gap 2: Human Override Weight Tracking**

The interaction map states: "Human trust is noted (frequent overrides reduce weight of
their agent's reviews)." This is not implemented.

**Resolution:**
1. Add `HumanOverrideTracker` to `pkg/negotiate/human_override.go`:
   ```go
   type HumanOverrideTracker struct {
       OverrideCounts map[string]int       // human_id → override count in window
       Window         time.Duration         // 90 days
       OverrideWeight map[string]float64    // human_id → review weight multiplier
   }
   ```
2. Weight decay logic:
   - 0–5 overrides in 90 days → weight 1.0 (no penalty)
   - 6–15 overrides → weight 0.75
   - 16–30 overrides → weight 0.50
   - 31+ overrides → weight 0.25
3. When a human overrides an agent review, the agent whose review was overridden
   receives a trust delta of 0 (no penalty if the review was evidence-backed).
4. If the override was because the agent's review was frivolous (no evidence), apply
   -2 trust to the agent.
5. Store override counts in `~/.helix/negotiations/overrides.json`.

**Gap 3: Human Dismissal Workflow in Forgejo**

The human needs a UI path to dismiss findings. Forgejo PR comments are the channel.

**Resolution:**
1. Add a `DISMISS` button/label on PR comments from agents that contain review findings.
   This can be implemented as a Forgejo webhook handler in `pkg/webhook/forgejo.go`
   that watches for reaction emoji or a specific comment pattern.
2. Alternatively (simpler v1): parse PR comments for `DISMISS:` blocks. The human
   types the structured dismissal as a comment reply.
3. Wire the webhook or comment parser to call `DismissalHandler.ProcessDismissal()`.

**Verification Gate (for 7.2):**

- Human posts `DISMISS: finding-123 | Reason: false_positive` → parsed correctly
- False positive tracker records the dismissal
- Model with 10 dismissals flagged for re-evaluation
- Human with 20 overrides in 90 days has their review weight reduced to 0.50
- Agent whose review was frivolously dismissed (no evidence) receives no penalty
- Agent whose review was evidence-backed and dismissed gets -2 trust adjustment
- Full audit trail records every dismissal

---

## 7.3 — Chimera Tiebreak

### Interaction Map Reference

> Deadlocked agent debate → Chimera formation adjudicates with multi-model consensus.
> Formation selects models with no prior stake in the PR. Consensus must reach threshold
> (configurable per risk level). Verdict is final and signed.

### Existing Implementation

| Component | File | Status |
|-----------|------|--------|
| Arbiter client (HTTP) | `pkg/negotiate/arbiter.go` | **Complete** — `Deliberate()`, `POST /deliberate`, `formation=arbiter` |
| Input assembly | `pkg/negotiate/input_assembly.go` | **Complete** — PR context + agent reviews + debate transcript |
| Cost attribution (50/50 split) | `pkg/negotiate/cost_recon.go` | **Complete** — `ApplyTieBreakCost()`, budget checking |
| Trust adjustment for tiebreak | `pkg/negotiate/trust_adjustment.go` | **Complete** — +2 for winner, -5 for frivolous |
| Consensus scoring | `pkg/negotiate/consensus.go` | **Complete** |
| Verdict finality | (in `negotiator.go` `Advance()`) | **Complete** — no appeal mechanism |
| Chimera MCP integration | MCP tool `mcp_chimera_chimera_deliberate` | **Available** — `formation=arbiter`, `stage_models` |

### Resolution Plan

**Gap 1: Arbiter Formation Configuration**

The current arbiter client sends `{"formation": "arbiter"}` to Chimera but doesn't
configure the formation itself. The spec requires: 3 independent models + audit stage,
majority vote (2 of 3), and conservative default (REJECT on tie).

**Resolution:**
1. Verify that Chimera's `arbiter` formation is configured server-side with:
   - 3 independent models from different providers
   - No model that participated in the original PR review
   - Audit stage (4th model) reviewing the 3 verdicts
   - Majority vote → APPROVE; tie → REJECT
2. If formation configuration is client-side (via `stage_models`), update
   `ArbiterClient.Deliberate()` to pass:
   ```go
   payload := deliberationRequest{
       Prompt:       prompt,
       Formation:    "arbiter",
       StageModels:  map[string]string{
           "primary":       "deepseek-v4-pro",
           "adversary_1":   "glm-5.2",
           "adversary_2":   "claude-sonnet-4",
           "audit":         "gemini-2.5-pro",
       },
   }
   ```
3. Add `ArbiterFormation` to `NegotiationConfig` to allow per-risk-level customization:
   ```go
   type NegotiationConfig struct {
       // ... existing fields ...
       ArbiterFormation   string            // default: "arbiter"
       ArbiterModels      map[string]string  // optional per-stage model override
       ConsensusThreshold float64            // default: 0.67 (2 of 3)
   }
   ```

**Gap 2: Verdict Signing**

The interaction map says "Verdict is final and signed." The current code parses Chimera's
JSON response but does not verify a cryptographic signature on the verdict.

**Resolution:**
1. If Chimera signs its responses with ED25519 (check `mcp_chimera_chimera_deliberate`
   response format), verify the signature in `ArbiterClient.Deliberate()`.
2. If Chimera does not currently sign, add a `ChimeraSignature` field to `ChimeraVerdict`
   and a `VerifySignature(pubKey ed25519.PublicKey) bool` method. This is a soft gate in v1
   (warn on missing signature, reject on invalid signature).
3. Store the signed verdict alongside the debate transcript:
   `~/.helix/negotiations/<pr>-<ts>-chimera.json`

**Gap 3: Risk-Level Consensus Thresholds**

The spec (adversarial-review.md) defines different consensus thresholds per change category:
- Contract changes: 3/3
- Behavioral changes: 2/2
- Cosmetic changes: single model

The tiebreak consensus should align with these categories.

**Resolution:**
1. Add `ConsensusThreshold` to `PRContext`:
   ```go
   type PRContext struct {
       // ... existing fields ...
       ChangeCategory    string  // "contract" | "behavioral" | "resilience" | "cosmetic"
       ConsensusRequired float64 // 1.0 for contract, 0.67 for behavioral, etc.
   }
   ```
2. In `ArbiterClient.Deliberate()`, pass the required threshold to Chimera.
3. If Chimera returns confidence < required threshold, escalate to human rather
   than accepting a low-confidence verdict.

**Gap 4: Chimera Unavailable Fallback**

When Chimera is unreachable, the current code escalates to human. The escalation
comment format exists (`escalation.go`). What's missing is a retry policy.

**Resolution:**
1. Add retry-with-backoff in `ArbiterClient.Deliberate()`:
   - 1st attempt: immediate
   - 2nd attempt: 5s backoff
   - 3rd attempt: 15s backoff
   - After 3 failures → escalate with `chimera_unavailable`
2. Use circuit breaker from `pkg/retry/retry.go`.

**Verification Gate (for 7.3):**

- Deadlocked negotiation → Chimera arbiter called with full debate transcript
- 3 independent models deliberate → majority returns APPROVE or REJECT
- Verdict signed with ED25519 → signature verified
- Cost split 50/50 between agents → budget checked → overrun escalated
- Winning agent gets +2 trust, losing agent with evidence gets 0 delta
- Chimera unavailable → 3 retries → escalate to human with structured comment
- Verdict posted as PR comment with Chimera trace

---

## 8.1 — Merge Gate Checks

### Interaction Map Reference

> All gates must be green before merge is possible: Tier 1 (secrets, lint, test, build),
> Tier 2 (adversarial review), evidence bundle valid, behavior contract present, trust
> tier sufficient. Helix must provide a GitReins merge gate that cannot be bypassed.

### Existing Implementation

| Component | File | Status |
|-----------|------|--------|
| MergeGate (5-check evaluator) | `pkg/mergegate/gate.go` | **Complete** — evidence, consensus, contract, trust, cost |
| Gate pipeline (6-gate sequencer) | `pkg/mergegate/pipeline.go` | **Complete** — Tier1 → Tier2 → Chimera → Conscientiousness → PromptFoo → CoApproval |
| CLI (`helix mergegate`) | `cmd/helix/mergegate.go` | **Complete** — `check`, `checks` subcommands |
| Evidence bundle type | `pkg/review/evidence.go` | **Complete** — `EvidenceBundle`, `Consensus`, `Signatures` |
| Behavior contract | `pkg/verify/contract.go` | **Complete** — `BehaviorContract`, `Validate()` |
| Trust tier classification | `pkg/mergegate/gate.go` | **Complete** — `classifyFile()`, `minTierForCategory()` |
| Cost guard | `pkg/dispatcher/cost_guard.go` | **Complete** — `CostGuardResult`, tier-capped |
| GitReins Tier 1 | MCP `mcp_gitreins_guard_run` | **Available** |
| GitReins Tier 2 | MCP `mcp_gitreins_judge_evaluate` | **Available** |
| Branch protection | `pkg/forgejo/branch_protection.go` | **Complete** |
| Forgejo PR status checks | `pkg/forgejo/pr_status.go` | **Complete** |

### Resolution Plan

**Gap 1: Gate Pipeline Stubs Not Wired to Live Services**

The 6-gate pipeline in `pkg/mergegate/pipeline.go` (`NewDefaultPipeline()`) uses
`StubGate` instances that always pass. They need to be replaced with real gate
implementations that call GitReins, Chimera, Conscientiousness, etc.

**Resolution:**
1. Implement `GitReinsTier1Gate` that calls `mcp_gitreins_guard_run`:
   ```go
   func (g *GitReinsTier1Gate) Execute(ctx context.Context, input GateInput) GateResult {
       // Run gitreins guard (secrets, lint, tests, build)
       // Return PASS if all checks pass, FAIL with details otherwise
   }
   ```
2. Implement `GitReinsTier2Gate` that calls `mcp_gitreins_judge_evaluate`:
   ```go
   func (g *GitReinsTier2Gate) Execute(ctx context.Context, input GateInput) GateResult {
       // Run adversarial evaluation on the diff
       // Return PASS if evaluation passes, FAIL otherwise
   }
   ```
3. Implement `ChimeraReviewGate` that calls Chimera's `standard` formation.
4. Implement `ConscientiousnessGate` that calls Conscientiousness for adversarial eval.
5. Implement `PromptFooGate` that checks prompt regression test results.
6. Wire the full pipeline in `cmd/helix/mergegate.go` with a `--live` flag that
   swaps out stubs for real implementations.

**Gap 2: Merge Gate Not Enforceable at Git Level**

The merge gate currently runs as a CLI tool (`helix mergegate check`). It is not
enforced as a Git hook or branch protection rule that blocks the actual merge.

**Resolution:**
1. Add `pre-merge-commit` hook integration in `pkg/mergegate/hook.go`:
   - Install as a Git hook in `.git/hooks/pre-merge-commit`
   - Hook calls `helix mergegate check` with the current branch's artifacts
   - FAIL → merge blocked; BLOCKED exit code prevents `git merge`
2. Add Forgejo branch protection rule configuration in `pkg/forgejo/branch_protection.go`:
   - Configure required status checks that must pass before merge
   - Map each gate in the pipeline to a Forgejo commit status
3. In `cmd/helix/mergegate.go`, add `enforce` and `install-hook` subcommands:
   ```
   helix mergegate install-hook   # installs pre-merge-commit hook
   helix mergegate enforce <pr>   # posts status checks to Forgejo
   ```

**Gap 3: Merge Gate Does Not Check Prompt Provenance**

The existing 5-check `MergeGate.Evaluate()` (evidence, consensus, contract, trust, cost)
does not include prompt provenance. This is a separate interaction point (8.3) but must
be integrated as a 6th check in the merge gate.

**Resolution:**
1. Add `checkPromptProvenance()` to `MergeGate`:
   ```go
   func (g *MergeGate) checkPromptProvenance(req MergeRequest) CheckResult {
       // Extract Prompt: sha256:<hash> from commit message
       // Look up in prompt registry (prompts/_index.yaml)
       // Verify hash matches stored content
       // Verify lifecycle status is attested or active
       // Verify PromptFoo status is pass
   }
   ```
2. Add `PromptHash` field to `MergeRequest`.
3. Add `promptProvenanceCheck` to the `Evaluate()` method.
4. Wire `pkg/prompt/attestation_validator.go` as the verification backend.

**Gap 4: Branch Protection Policy Not Synced**

The Forgejo branch protection rules need to be programmatically synchronized with
the Helix trust model and file-category restrictions.

**Resolution:**
1. Use `pkg/forgejo/branch_protection.go` to configure:
   - `main` / `master` branch: require all 6 gate status checks
   - Feature branches: require Tier 1 + Tier 2 + evidence bundle
   - Per-file-category minimum reviewer count based on trust tier
2. Add `helix mergegate sync-protection` subcommand that reconciles Forgejo
   branch protection rules with the Helix gate configuration.

**Verification Gate (for 8.1):**

- All 6 gates pass → merge ALLOWED
- Any gate FAIL → merge BLOCKED with specific blocker message
- Any gate WARN → merge ESCALATED to human
- Evidence bundle missing → BLOCKED: "no evidence bundle attached to merge request"
- Consensus blocked → BLOCKED with model verdict details
- Behavior contract missing → BLOCKED: "no behavior contract committed"
- Trust tier insufficient for IaC file → BLOCKED: "requires observed+"
- Cost over cap → BLOCKED: estimated > tier cap
- `--no-verify` bypass → audited in JSONL log, weekly report surfaces
- All checks produce structured JSON report for CI/CD consumption

---

## 8.2 — Cost Gate

### Interaction Map Reference

> Before merge, verify actual cost vs estimated cost. Flag variance >20%.
> Helix must provide a reconciliation engine comparing estimated vs actual
> token spend. Variance > threshold triggers review. Agent cost accuracy feeds trust score.

### Existing Implementation

| Component | File | Status |
|-----------|------|--------|
| Pre-flight estimator | `pkg/estimate/estimator.go` | **Complete** — cache-aware, multi-model |
| Budget enforcement | `pkg/estimate/budget.go` | **Complete** — weekly caps, tier limits |
| Post-execution reconciliation | `pkg/estimate/reconciliation.go` | **Complete** — `Reconcile()` |
| Drift detection | `pkg/estimate/drift.go` | **Complete** — >10% drift logs warning |
| Negotiation cost reconciliation | `pkg/negotiate/cost_recon.go` | **Complete** — per-round + tiebreak split |
| Cost guard (dispatch) | `pkg/dispatcher/cost_guard.go` | **Complete** — tier-capped, escalate/block |
| OpenRouter budget integration | `pkg/estimate/openrouter.go` | **Complete** — key query, baseline sync |
| Merge gate cost check | `pkg/mergegate/gate.go:checkCostGuard()` | **Complete** — checks `CostGuardResult` |

### Resolution Plan

**Gap 1: Merge-Time Cost Reconciliation Not in Gate Pipeline**

The merge gate's `checkCostGuard()` only checks whether the pre-dispatch cost guard
was approved. It does NOT reconcile estimated vs actual token spend at merge time.

**Resolution:**
1. Add `checkCostReconciliation()` to `MergeGate`:
   ```go
   func (g *MergeGate) checkCostReconciliation(req MergeRequest) CheckResult {
       // If actual cost is available:
       //   variance = |actual - estimated| / estimated
       //   if variance > 0.20 (20%):
       //     return WARN with variance details
       //   if variance > 0.50 (50%):
       //     return FAIL (requires review)
       //   else:
       //     return PASS
       // If actual cost not available (pre-execution):
       //   return CheckSkipped
   }
   ```
2. Add `CostReconciliation` to `MergeRequest`:
   ```go
   type MergeRequest struct {
       // ... existing fields ...
       ActualCost   *float64 `json:"actual_cost,omitempty"`    // from GitReins LLMUsage
       EstimatedCost float64 `json:"estimated_cost"`           // from pre-flight
   }
   ```
3. Populate `ActualCost` from GitReins evaluator's `LLMUsage` struct (cache-aware).

**Gap 2: Cost Accuracy Does Not Feed Trust Score**

The interaction map states: "Agent cost accuracy feeds trust score." The current
trust model has no dimension for cost estimation accuracy.

**Resolution:**
1. Add `cost_accuracy` as a 7th trust dimension with weight 0.05 (reducing
   the next-lowest dimension by 0.05, e.g., `human_feedback` from 0.10 to 0.05,
   or redistributing across all dimensions proportionally):
   - Merge success rate: 0.25
   - Incident attribution: 0.28
   - Review consensus: 0.15
   - Prompt integrity: 0.10
   - Human feedback: 0.07
   - Tenure: 0.10
   - **Cost accuracy: 0.05** (new)
2. Cost accuracy score formula:
   - Calculate `avg_variance` over last 50 tasks: mean(|actual - estimated| / estimated)
   - Score = `1.0 - min(avg_variance, 1.0)` (clamped to [0, 1])
   - An agent whose estimates are within 5% on average → score 0.95
   - An agent with 50% average variance → score 0.50
3. Implement in `pkg/trust/scorer.go` (or `pkg/trust/scorer_test.go`, depending on
   where the calculation lives).
4. Update `DimensionWeights` in `pkg/trust/tiers.go` and `Calculate()`.

**Gap 3: Estimate Drift Not Visible in Merge Gate Report**

The current merge gate report shows `PASS`/`FAIL` for each check but doesn't
surface the estimated vs actual breakdown in human-readable form.

**Resolution:**
1. Add `CostSummary` field to `CheckResult.Details` when cost check passes:
   ```
   estimated=$0.12 actual=$0.14 variance=+16.7% cap=$5.00
   ```
2. When variance >20%, include recommended action:
   ```
   variance=+35% → WARN: "Review estimation parameters for agent <name>.
   Consider reducing cache_hit_ratio assumption from 0.60 to 0.45."
   ```
3. Add `--cost-report` flag to `helix mergegate check` that prints a detailed
   cost reconciliation table.

**Verification Gate (for 8.2):**

- Estimated cost $0.12, actual cost $0.14 → variance 16.7% < 20% → PASS
- Estimated cost $0.12, actual cost $0.20 → variance 66.7% > 50% → FAIL with review required
- Estimated cost $0.12, actual cost $0.15 → variance 25% > 20% → WARN
- No actual cost available → SKIPPED (still within budget check)
- Cost guard blocked → BLOCKED
- Agent with consistent <5% estimation variance → trust +0.02/event
- Agent with consistent >50% estimation variance → trust -0.02/event
- OpenRouter key budget exhausted → BLOCKED with "BUDGET_EXHAUSTED"
- 3+ tasks with >20% variance in 30 days → agent flagged for cost review

---

## 8.3 — Prompt Provenance Gate

### Interaction Map Reference

> Verify that the prompt version used matches the registered prompt. Detect prompt drift.
> Helix must provide a prompt registry with hash verification. Every commit links to a
> prompt version. PromptFoo regression tests in CI. Prompt changes require new version
> and re-attestation.

### Existing Implementation

| Component | File | Status |
|-----------|------|--------|
| Prompt registry | `pkg/prompt/registry.go` | **Complete** — register, lookup, index |
| Hasher (SHA-256, normalization) | `pkg/prompt/hasher.go` | **Complete** — 5-step pipeline |
| Lifecycle state machine | `pkg/prompt/lifecycle.go` | **Complete** — 7 states, transition table |
| Attestation (commit-msg hook) | `pkg/prompt/attester.go` | **Complete** |
| GitReins commit-msg hook | `pkg/prompt/hook.go` | **Complete** — extract, validate, block |
| PromptFoo CI integration | `pkg/prompt/promptfoo.go` | **Complete** |
| Provenance chain walker | `pkg/prompt/provenance.go` | **Complete** — commit→prompt→spec→intent |
| Attestation trailer parser | `pkg/prompt/attestation_trailer.go` | **Complete** |
| Attestation validator | `pkg/prompt/attestation_validator.go` | **Complete** |
| PromptFoo CI workflow gen | `pkg/prompt/ci/workflow.go` | **Complete** |

### Resolution Plan

**Gap 1: Prompt Provenance Not Checked at Merge Time**

The prompt provenance gate is enforced at commit time (via the commit-msg hook) but
is not re-verified at merge time. A commit that passed the hook could have its
prompt `retired` or tampered with between commit and merge.

**Resolution:**
1. Add `checkPromptProvenance()` to `MergeGate` (as noted in §8.1 Gap 3):
   ```go
   func (g *MergeGate) checkPromptProvenance(req MergeRequest) CheckResult {
       const name = "prompt_provenance"
       
       if req.PromptHash == "" {
           return CheckResult{
               Name: name, Status: CheckFail,
               Reason: "no prompt attestation in commit message",
           }
       }
       
       // Look up hash in prompt registry
       entry, err := g.promptRegistry.LookupByHash(req.PromptHash)
       if err != nil {
           return CheckResult{
               Name: name, Status: CheckFail,
               Reason: fmt.Sprintf("prompt hash %s not found in registry", req.PromptHash),
           }
       }
       
       // Verify lifecycle
       if !entry.CanAttest() {
           return CheckResult{
               Name: name, Status: CheckFail,
               Reason: fmt.Sprintf("prompt %s/%s has status %s (requires attested or active)",
                   entry.Component, entry.Version, entry.Status),
           }
       }
       
       // Verify PromptFoo status
       if entry.PromptFoo.Status == "fail" {
           return CheckResult{
               Name: name, Status: CheckFail,
               Reason: fmt.Sprintf("PromptFoo tests failed for %s/%s", entry.Component, entry.Version),
           }
       }
       
       // Verify hash integrity (tamper detection)
       recomputed, err := g.promptRegistry.RecomputeHash(entry.Component, entry.Version)
       if err != nil || recomputed != entry.Hash {
           return CheckResult{
               Name: name, Status: CheckFail,
               Reason: "TAMPER_DETECTED: prompt hash mismatch (file modified after attestation)",
           }
       }
       
       return CheckResult{
           Name: name, Status: CheckPass,
           Reason: fmt.Sprintf("prompt %s/%s verified (hash=%s, status=%s, promptfoo=%s)",
               entry.Component, entry.Version, entry.Hash[:16], entry.Status, entry.PromptFoo.Status),
           Details: fmt.Sprintf("full_chain: %s → %s → %s", 
               req.CommitSHA[:8], entry.SpecRef, entry.WorkItem),
       }
   }
   ```
2. Add `PromptHash`, `CommitSHA`, and `PromptRegistry` to `MergeRequest`.
3. Wire into `MergeGate.Evaluate()` and the pipeline.

**Gap 2: Prompt Drift Detection Between Commit and Merge**

If a prompt file is modified after commit attestation but before merge (e.g., in
a rebase or force-push), the merge gate should detect the drift.

**Resolution:**
1. The hash check in `checkPromptProvenance()` already detects tampering (recomputed
   hash vs stored hash). This covers the drift case.
2. Add `prompt_change_detected` metric to `pkg/prompt/metrics.go`:
   - Counter incremented on each tamper detection
   - Alert if >3 tamper detections in 24 hours (indicates systemic issue)
3. Add `PROMPT_DRIFT` event type to `pkg/negotiate/audit.go` for cross-component
   audit trail.

**Gap 3: Prompt Provenance Chain Not Visible in Merge Report**

The full provenance chain (commit → prompt → spec → work item → intent) should
be visible in the merge gate report for human reviewers.

**Resolution:**
1. Add `ProvenanceChain` to `GateReport`:
   ```go
   type GateReport struct {
       // ... existing fields ...
       Provenance *ProvenanceSummary `json:"provenance,omitempty"`
   }
   type ProvenanceSummary struct {
       CommitSHA    string `json:"commit_sha"`
       Component    string `json:"component"`
       Version      string `json:"version"`
       PromptHash   string `json:"prompt_hash"`
       SpecRef      string `json:"spec_ref"`
       WorkItem     string `json:"work_item"`
       Intent       string `json:"intent"`
       Verified     bool   `json:"verified"`
   }
   ```
2. Populate from `pkg/prompt/provenance.go` → `WalkChain()`.
3. Render in `printMergeGateReport()` as a provenance section.

**Gap 4: Prompt Drift Monitoring Across the Fleet**

Prompt drift should be monitored across all agents and repos, not just at
individual merge time.

**Resolution:**
1. Add `PromptDriftMonitor` in `pkg/prompt/drift_monitor.go`:
   - Periodic scan (hourly cron) of all active PRs
   - For each PR, extract `Prompt:` trailer from each commit
   - Verify hash against registry
   - Flag any drift to LangFuse
2. Metrics (Prometheus):
   ```
   helix_prompt_drift_total{component, version}  // counter
   helix_prompt_attestation_valid_total           // counter
   helix_prompt_attestation_invalid_total{reason} // counter by failure reason
   ```

**Verification Gate (for 8.3):**

- Commit has valid `Prompt: sha256:<hash>` trailer → hash found in registry → PASS
- Commit has malformed trailer → FAIL: "ATTESTATION_MALFORMED"
- Hash not in registry → FAIL: "PROMPT_NOT_FOUND"
- Prompt status is `deprecated` and within 30-day window → WARN
- Prompt status is `retired` → FAIL: "LIFECYCLE_VIOLATION"
- PromptFoo status is `fail` → FAIL: "promptfoo regression tests failed"
- Prompt file tampered after attestation → FAIL: "TAMPER_DETECTED"
- `--no-verify` bypass → audited to `audit.jsonl`
- Full provenance chain verified → commit → prompt → spec → work item → intent
- Prompt drift across fleet monitored via Prometheus metrics

---

## Cross-Cutting: Phase 7–8 Integration Flow

The complete flow from negotiation to merge:

```
PR Opened
  ↓
Agent A review (APPROVED) ← → Agent B review (REQUEST_CHANGES)
  ↓
Conflict detected → [7.1] Structured Debate (3 rounds)
  ↓ (concession in round N)       ↓ (deadlock after round 3)
  Resolved ← --------           [7.3] Chimera Tiebreak
                                    ↓
                              Chimera returns APPROVE or REJECT
                                    ↓
                              If REJECT → back to implementation
                                    ↓
All reviews resolved → [7.2] Human reviews agency findings
  ↓ (dismisses any false positives)
Merge gate activated:
  ↓
[8.1] Tier 1: secrets, lint, tests, build (GitReins)
  ↓ PASS
[8.1] Tier 2: adversarial multi-model review (Chimera + Conscientiousness)
  ↓ PASS
[8.1] Evidence bundle valid (signatures, completeness)
  ↓ PASS
[8.1] Behavior contract present (assertions well-formed)
  ↓ PASS
[8.1] Trust tier sufficient for changed file categories
  ↓ PASS
[8.2] Cost gate: estimated vs actual reconciliation
  ↓ PASS (or WARN on >20% variance)
[8.3] Prompt provenance: hash verified, lifecycle valid, no tamper
  ↓ PASS
  MERGE ALLOWED
     ↓
  Git merge + signed merge commit
```

### Service Wiring (from `specs/cross-component-wiring.md`)

| Caller | Callee | Endpoint | Purpose |
|--------|--------|----------|---------|
| `helix negotiate` | Forgejo | `GET /api/v1/repos/{o}/{r}/pulls/{n}/reviews` | Detect conflicting reviews |
| `helix negotiate` | Forgejo | `POST /api/v1/repos/{o}/{r}/pulls/{n}/reviews` | Post debate comments |
| `helix negotiate` | Chimera | `POST /deliberate` (formation=arbiter) | Tiebreak resolution |
| `helix mergegate` | GitReins | `gitreins guard` (MCP) | Tier 1 static checks |
| `helix mergegate` | GitReins | `gitreins evaluate` (MCP) | Tier 2 agentic eval |
| `helix mergegate` | Chimera | `POST /deliberate` (formation=standard) | Multi-model review |
| `helix mergegate` | Conscientiousness | `POST /evaluate` | Adversarial eval |
| `helix mergegate` | PromptFoo | CI artifact read | Regression test results |
| `helix mergegate` | Forgejo | `POST /repos/{o}/{r}/statuses/{sha}` | Status check posting |
| `helix mergegate` | LangFuse | `POST /api/public/ingestion` | Cost + trace observability |

### Error Propagation (from `specs/cross-component-wiring.md` §7)

| Caller | Callee Failure | Propagated Error |
|--------|---------------|-----------------|
| negotiate | Chimera unreachable | `CHIMERA_UNAVAILABLE: <error>` → escalate to human |
| negotiate | Forgejo unreachable | `CONNECTION_REFUSED: retry in 30s (attempt 1/4)` |
| mergegate | GitReins timeout | Gate FAIL with "GitReins Tier 1 timeout" |
| mergegate | Chimera unreachable | Gate FAIL with "Chimera unavailable — manual review required" |
| mergegate | Conscientiousness unavailable | Gate WARN (degraded — skip adversarial eval) |

### Circuit Breakers (from `specs/cross-component-wiring.md` §8)

| Service Pair | Max Failures | Reset Timeout | On Open |
|-------------|-------------|---------------|---------|
| negotiate → Forgejo | 5 | 60s | Retry with backoff |
| negotiate → Chimera | 5 | 60s | Escalate to human |
| mergegate → Chimera | 5 | 60s | Degrade gracefully (skip review) |
| mergegate → LangFuse | 10 | 120s | Buffer traces locally |

---

## Test Strategy Summary

### Phase 7 Tests (existing + new)

| Layer | What | Status |
|-------|------|--------|
| `debate_test.go` | Round progression, evidence validation, strikes, concession | **Existing** — 16 tests |
| `arbiter_test.go` | Chimera verdict parsing, cost splitting | **Existing** — 8 tests |
| `trust_test.go` / `trust_adjustment_test.go` | Trust delta calculations | **Existing** — 12 tests |
| `negotiator_test.go` | Full negotiation flow | **Existing** — 8 tests |
| `veto_test.go` | Veto validation, frivolous tracking | **Existing** — 10 tests |
| `escalation_test.go` | Escalation formatting, exit codes | **Existing** — 6 tests |
| `cost_recon_test.go` | Budget checking, 50/50 split | **Existing** — 8 tests |
| `forgejo_integration_test.go` | Forgejo comment posting | **NEEDED** (Gap 1) |
| `human_override_test.go` | Dismissal parsing, weight tracking | **NEEDED** (Gap 2) |
| `chimera_signing_test.go` | Verdict signature verification | **NEEDED** (Gap 3) |

### Phase 8 Tests (existing + new)

| Layer | What | Status |
|-------|------|--------|
| `gate_test.go` | All 5 gate checks + edge cases | **Existing** — 18 tests |
| `pipeline_test.go` | Pipeline sequencing, stop-on-first-fail | **Existing** — 10 tests |
| `mergegate_test.go` | CLI parsing, output formatting | **Existing** — 12 tests |
| `gate_live_test.go` | Real GitReins + Chimera calls | **NEEDED** (Gap 1) |
| `prompt_provenance_test.go` | Merge-time prompt verification | **NEEDED** (Gap 2) |
| `cost_reconciliation_test.go` | Merge-time cost variance check | **NEEDED** (Gap 3) |
| `drift_monitor_test.go` | Fleet-wide prompt drift scanning | **NEEDED** (Gap 4) |

---

## Implementation Priority

### Immediate (blocking production readiness)

1. **7.1 Gap 1** — Forgejo PR comment posting (`forgejo_poster.go`): Without this,
   negotiation is invisible to humans and not auditable on the PR.
2. **8.1 Gap 2** — Merge gate enforceability (Git hook + Forgejo branch protection):
   Without this, the gate can be skipped, defeating its purpose.
3. **8.3 Gap 1** — Prompt provenance at merge time: Without this, prompt drift
   between commit and merge is undetected.

### High Priority (next sprint)

4. **7.1 Gap 2** — Multi-agent execution loop: Required for fully-automated negotiation.
5. **7.2 Gap 1** — Structured dismissal protocol: Required for human-agent disagreement loop.
6. **8.2 Gap 1** — Cost reconciliation at merge time: Required for cost gate to be meaningful.
7. **8.1 Gap 1** — Live gate implementations (replace stubs): Required for real gates.

### Medium Priority (polish)

8. **7.3 Gap 1** — Arbiter formation configuration: Improves tiebreak quality.
9. **7.3 Gap 2** — Verdict signing: Defense-in-depth for verdict integrity.
10. **7.2 Gap 2** — Human override weight tracking: Closes the feedback loop.
11. **8.2 Gap 2** — Cost accuracy trust dimension: Closes the estimation quality loop.
12. **8.3 Gap 4** — Fleet-wide drift monitoring: Proactive tamper detection.

### Nice to Have (v2)

13. **7.1 Gap 3** — Consensus engine for multi-agent positions beyond 2 agents.
14. **7.3 Gap 3** — Risk-level consensus thresholds tied to change category.
15. **8.1 Gap 4** — Automated branch protection sync.

---

## Document Status

- [x] Phase 7.1 — Agent-Agent Disagreement (debate protocol, gaps, resolution)
- [x] Phase 7.2 — Human-Agent Disagreement (dismissal protocol, override tracking, gaps)
- [x] Phase 7.3 — Chimera Tiebreak (arbiter config, signing, thresholds, gaps)
- [x] Phase 8.1 — Merge Gate Checks (6-gate pipeline, enforceability, gaps)
- [x] Phase 8.2 — Cost Gate (reconciliation, trust dimension, gaps)
- [x] Phase 8.3 — Prompt Provenance Gate (merge-time check, drift, gaps)
- [x] Cross-cutting integration flow with service wiring
- [x] Error propagation and circuit breaker tables
- [x] Test strategy summary
- [x] Implementation priority ranking
