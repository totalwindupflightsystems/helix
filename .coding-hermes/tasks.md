# Helix Coding Tasks — Foreman Queue

## [x] HIGH: Wire BwrapExecutor to actually run bwrap (pkg/sandbox)
- **Priority:** critical — blocks all agent code execution
- **Completed:** 2026-07-08 — Run() fully implemented in executor.go:271-346. Creates session dirs, sets up cgroups, builds bwrap args, executes, handles timeout, cleans up. bwrap 0.11.1 present. go test ./pkg/sandbox/... PASS (1.044s).

## [x] HIGH: Wire dispatcher to live Forgejo (pkg/dispatcher)
- **Priority:** critical — this is the spine of Helix
- **Completed:** 2026-07-08 — ForgejoLoop fully implemented in forgejo_loop.go. Creates branches (idempotent on 409), opens PRs (idempotent on 409), has dry-run mode, lock acquisition, worktree creation, step execution. CLI subcommands `helix dispatch` and `helix dispatcher` wired. go test ./pkg/dispatcher/... PASS.

## [~] HIGH: Implement real model clients for adversarial review (pkg/review)
- **Priority:** critical — adversarial review is all stubs right now
- **Plan:** specs/plans/phase-5-6-review.md §What's Missing §1
- **Gap:** `pkg/review/orchestrator.go` defines `ModelClient` interface but only `StubAgent` exists. Three-model adversarial formation cannot run.
- **Files:** pkg/review/client_chimera.go (NEW), pkg/review/client_deepseek.go (NEW), pkg/review/client.go (refactor)
- **AC:** `helix review run --pr 42` dispatches to 3 real models (Chimera, DeepSeek, Owl), produces signed evidence bundle, outputs consensus verdict.
- **Logic:** Implement `ModelClient` for Chimera API, OpenRouter, and direct provider APIs. Each returns structured `review.Finding` with evidence refs. Wire into `AdversarialAgentDispatcher`.

## [ ] HIGH: Build change management dashboard (human review interface)
- **Priority:** high — human needs structured review, not diffs
- **Plans:** specs/plans/phase-5-6-review.md §6.1, specs/interaction-map.md §6.1
- **Gap:** Human reviewers see raw diffs. Need blast radius map, risk assessment, architectural fit analysis, tradeoff surfacing.
- **Files:** cmd/helix-review/main.go (NEW), pkg/review/dashboard.go (NEW), pkg/review/blast_radius.go (NEW)
- **AC:** `helix review dashboard --pr 42` outputs: (1) Blast radius — which packages/services/teams are affected, (2) Risk score — correlation with incident DB for similar changes, (3) Architectural fit — ADR lineage comparison, (4) Trust context — agent track record. Output in terminal + JSON for CI.
- **Logic:** Dependency graph traversal for blast radius. Incident DB query for risk correlation. ADR index for architecture fit. Trust ledger lookup for agent context.

## [ ] HIGH: Enforce merge gates at Git level (pre-receive hook)
- **Priority:** high — merge gates exist in code but not in git
- **Plan:** specs/plans/phase-7-8-negotiate-merge.md §Gap 2-4
- **Gap:** Merge gate pipeline is stubbed. No Forgejo pre-receive hook blocks merges. Branch protection not synced.
- **Files:** scripts/gitreins-pre-receive.sh (NEW), .gitreins/config.yaml
- **AC:** Push to protected branch with failing gates → rejected with structured message showing which gate failed. Push with all gates passing → accepted. Works in Forgejo pre-receive hook.
- **Logic:** Pre-receive hook: capture pushed refs → run gitreins gate pipeline → check evidence bundle exists → check trust tier for changed files → accept or reject with detailed output.

## [ ] MEDIUM: Implement ideation system — Idea capture, validation, prioritization
- **Priority:** medium — new capability, not blocking existing flow
- **Plan:** specs/plans/phase-1-2-ideation-spec.md §1.1-1.3
- **Gap:** No structured ideation. Ideas live in Slack/Notion/heads. Agents can't surface or challenge ideas.
- **Files:** pkg/ideation/types.go, pkg/ideation/store.go, pkg/ideation/validator.go, pkg/ideation/priority.go, cmd/helix-idea/main.go (all NEW)
- **AC:** (1) `helix idea capture "Add rate limiting to auth"` → persisted with evidence refs. (2) Agent creates idea via API → same store. (3) `helix idea validate <id>` runs adversarial concept validation via Chimera. (4) `helix idea prioritize` ranks by cost/risk/evidence. (5) `helix idea promote <id> --to spec` creates spec placeholder.
- **Logic:** `Idea` struct with Source (human/agent/chimera), EvidenceRefs, Status. JSONL store. `IdeaValidator` dispatches concept agents (@assumption-buster, @redteam, @cost-auditor). `IdeaPrioritizer` ranks by multi-dimensional scoring.

## [ ] MEDIUM: Implement spec co-authoring with adversarial annotation
- **Priority:** medium
- **Plan:** specs/plans/phase-1-2-ideation-spec.md §2.1
- **Gap:** Specs are written by humans with no agent challenge. Completeness not verified.
- **Files:** pkg/spec/types.go, pkg/spec/coauthor.go, pkg/spec/completeness.go, cmd/helix-spec/main.go (all NEW)
- **AC:** `helix spec create <idea-id>` creates spec template. Agent proposes additional sections (edge cases, failure modes). `helix spec review <spec-id>` shows agent annotations with accept/reject. `helix spec gap-analysis <spec-id>` shows 12-dimension completeness score.
- **Logic:** `SpecCoAuthor` uses two agents: @spec-generator proposes, @spec-challenger challenges. Annotations inline with severity. `SpecCompleteness` scores 12 dimensions. Spec store as markdown with YAML frontmatter.

## [ ] MEDIUM: Implement ADR co-authoring and multi-model review
- **Priority:** medium
- **Plan:** specs/plans/phase-1-2-ideation-spec.md §2.2
- **Gap:** No structured ADR system. Architecture decisions are ad-hoc.
- **Files:** pkg/adr/types.go, pkg/adr/coauthor.go, pkg/adr/review.go, cmd/helix-adr/main.go (all NEW)
- **AC:** `helix adr create "Use event sourcing for audit log"` → agent co-authors with tradeoffs. `helix adr review <id>` → multi-model review. `helix adr supersede <old-id> <new-id>` → links ADR lineage.
- **Logic:** `ADR` struct with alternatives, tradeoffs, evidence links. `ADRCoAuthor` proposes alternatives; human selects. `ADRReviewer` uses multi-model dispatch for consistency, security, performance review.

## [ ] MEDIUM: Implement design review with adversarial agents
- **Priority:** medium
- **Plan:** specs/plans/phase-1-2-ideation-spec.md §2.3
- **Gap:** Design reviews are meetings with no structured output. No adversarial challenge.
- **Files:** pkg/design/types.go, pkg/design/review.go, cmd/helix-design/main.go (NEW)
- **AC:** `helix design review <spec-id>` outputs: assumption risk ranked, threat surface map, cost projection, completeness gaps, consensus verdict (PASS/WARN/FAIL).
- **Logic:** `DesignReviewDispatcher` wraps adversarial agent dispatch. Trigger types: @assumption-buster, @redteam, @cost-auditor, @completeness-checker. Threat map as ASCII/service visualization.

## [ ] MEDIUM: Implement API contract generation and breaking change detection
- **Priority:** medium
- **Plan:** specs/plans/phase-1-2-ideation-spec.md §2.4
- **Gap:** Contracts defined manually. No breaking change detection. No consumer impact analysis.
- **Files:** pkg/contract/types.go, pkg/contract/generate.go, pkg/contract/validate.go, pkg/contract/breaking.go, cmd/helix-contract/main.go (all NEW)
- **AC:** `helix contract create <spec-id> --format openapi` generates schema. `helix contract validate <contract-id>` runs multi-model validation. `helix contract diff <old> <new>` shows breaking changes and consumer impacts.
- **Logic:** `ContractAuthor` generates OpenAPI/protobuf/GraphQL from spec. `ContractValidator` uses multi-model dispatch. `BreakingChangeDetector` diffs schemas + consumer catalog.

## [ ] MEDIUM: Add trust-tier-gated task assignment to dispatcher
- **Priority:** medium
- **Plan:** specs/plans/phase-3-4-task-impl.md §3.2
- **Gap:** Tasks don't enforce required trust tier. Agent could be assigned above their tier.
- **Files:** pkg/dispatcher/assignment.go (NEW), pkg/dispatcher/assignment_test.go (NEW)
- **AC:** Dispatcher refuses to assign task requiring Tier 2 to Provisional agent. Error message shows required tier and agent's current tier. Agent can't self-assign above tier.
- **Logic:** `AssignAgent` checks `Task.RequiredTier` against `Agent.TrustTier`. Returns structured error on mismatch. File-category-based tier requirements (IaC→Tier1, CI/CD→Tier3, auth→Tier2).

## [ ] MEDIUM: Implement context auto-assembly for agent tasks
- **Priority:** medium
- **Plan:** specs/plans/phase-3-4-task-impl.md §3.3
- **Gap:** Agents start tasks with no context. Must discover specs, related PRs, architectural constraints manually.
- **Files:** pkg/dispatcher/context.go (NEW), pkg/dispatcher/context_test.go (NEW)
- **AC:** When task assigned, agent receives context package: (1) linked spec sections, (2) prior related PRs (from git log), (3) architectural constraints (from ADRs), (4) incident history for similar changes. Context fits in budget window. Agent can request expansion (costs tokens).
- **Logic:** `ContextAssembler` queries spec links, git history, ADR index, incident DB. Budget-constrained assembly. Token-aware trimming.

## [ ] MEDIUM: Implement structured clarification protocol
- **Priority:** medium
- **Plan:** specs/plans/phase-3-4-task-impl.md §4.2
- **Gap:** Agent hits ambiguity → no structured way to ask. Human gets "help" messages.
- **Files:** pkg/clarify/types.go, pkg/clarify/protocol.go, cmd/helix-clarify/main.go (all NEW)
- **AC:** Agent files `CLARIFICATION_NEEDED` with: specific question, relevant spec section, blocked progress, context. Human or trusted agent responds. Resolution linked to task and spec. Ambiguity metrics feed spec quality scoring.
- **Logic:** `Clarification` struct with question, spec ref, context. Async protocol — agent blocks until resolution. Resolution recorded in task audit. Ambiguity patterns aggregated.

## [ ] MEDIUM: Implement review load balancing and priority queue
- **Priority:** medium
- **Plan:** specs/plans/phase-5-6-review.md §6.3
- **Gap:** PRs reviewed FIFO. No routing by expertise or urgency. Agent reviews not balanced.
- **Files:** pkg/review/queue.go (NEW), pkg/review/queue_test.go (NEW)
- **AC:** `helix review queue` shows PRs sorted by priority score (risk × staleness). `helix review assign --pr 42` routes to best reviewer (agent or human) based on expertise and load. Agent review counts tracked and balanced.
- **Logic:** Priority scoring: risk score × hours waiting. Reviewer matching: domain expertise + current load + trust tier. Agent reviews capped per tick to prevent review spam.

## [ ] MEDIUM: Implement structured dismissal protocol for human-agent disagreement
- **Priority:** medium
- **Plan:** specs/plans/phase-7-8-negotiate-merge.md §7.2
- **Gap:** Human dismisses agent finding with "ignore." No learning. No accountability.
- **Files:** pkg/review/dismissal.go (NEW), pkg/review/dismissal_test.go (NEW)
- **AC:** Human dismisses finding with structured reason (not_valid/not_applicable/already_addressed/risk_accepted). Dismissal feeds false positive tracker. Frequent incorrect dismissals reduce human override weight.
- **Logic:** `Dismissal` struct with reason enum, evidence. Feeds `FPTracker.Increment`. After N dismissals, human override weight decreases. Mutual accountability — both agent and human reviews tracked.

## [ ] MEDIUM: Implement risk-level consensus thresholds for Chimera tiebreak
- **Priority:** medium
- **Plan:** specs/plans/phase-7-8-negotiate-merge.md §7.3
- **Gap:** Consensus threshold is hardcoded. Auth changes should require higher consensus than formatting.
- **Files:** pkg/negotiate/consensus.go (NEW), pkg/negotiate/consensus_test.go (NEW)
- **AC:** Auth/authz changes → 3/3 consensus required. Behavioral changes → 2/3. Cosmetic changes → 1/3. Threshold configurable per file category. Chimera unavailable → fallback to human.
- **Logic:** `ConsensusThreshold` per risk level. File category classifier maps changed files to risk level. Threshold comparison in `ConsensusEngine`. Fallback protocol when Chimera unreachable.

## [ ] MEDIUM: Implement incident attribution engine CLI
- **Priority:** medium
- **Plan:** specs/plans/phase-9-10-deploy-monitor.md §10.3
- **Gap:** pkg/incident has types but no CLI. Can't attribute incidents to agents.
- **Files:** cmd/helix-incident/main.go (NEW)
- **AC:** `helix incident attribute --since 24h` traces causal chain from recent deploys → changed code paths → responsible agents. Output: author 70%, reviewer(s) 20%, approver 10%. Feeds trust ledger.
- **Logic:** Query deployment history → find changed files → git blame → agent identity lookup → attribution split. Time-weighted decay applied. Trust ledger update triggered.

## [ ] MEDIUM: Implement shadow verification CLI
- **Priority:** medium
- **Plan:** specs/plans/phase-9-10-deploy-monitor.md §9.1
- **Gap:** pkg/verify has contracts but no CLI to run shadow deployment.
- **Files:** cmd/helix-verify/main.go (NEW)
- **AC:** `helix verify shadow --contract <id> --duration 24h` launches shadow, mirrors production traffic, compares behavior, outputs differential. `helix verify canary --contract <id> --step 5` promotes canary to next step if healthy.
- **Logic:** `ShadowManager` wraps pkg/verify types. Launches shadow instance, captures behavior diff, auto-rollback on anomaly. `CanaryPromoter` ramps by trust tier schedule.

## [ ] MEDIUM: Implement cross-agent notification bus and context sharing
- **Priority:** medium
- **Plan:** specs/plans/phase-11-12-trust-learn.md §12.3
- **Gap:** Agents work in isolation. If agent A discovers a pattern, agent B never learns.
- **Files:** pkg/notify/bus.go (NEW), pkg/notify/subscription.go (NEW), cmd/helix-notify/main.go (NEW)
- **AC:** Agent publishes finding → agents subscribed to domain receive it. Budget-tracked — each share costs tokens. Human can observe inter-agent discoveries via `helix notify stream`.
- **Logic:** Pub/sub bus with domain subscriptions. Structured `Notification` with evidence links. Token budget per notification. Human-observable stream.

## [ ] MEDIUM: Implement model evaluation and rotation based on production outcomes
- **Priority:** medium
- **Plan:** specs/plans/phase-11-12-trust-learn.md §12.4
- **Gap:** Models evaluated by benchmarks, not production outcomes. Underperforming models not rotated.
- **Files:** pkg/review/model_eval.go (NEW), pkg/review/model_eval_test.go (NEW)
- **AC:** Model X incident rate vs Model Y computed from incident DB. Models with >15% false positive rate auto-removed from review rotation. Dashboard: `helix review models` shows per-model stats.
- **Logic:** `ModelEvaluator` queries incident DB + false positive tracker. Computes per-model incident rate, FP rate, review accuracy. Auto-rotation trigger: >15% FP rate or >2x incident rate of next best model.

## [ ] LOW: Implement pattern discovery across incident database
- **Priority:** low
- **Plan:** specs/plans/phase-11-12-trust-learn.md §12.1
- **Gap:** Incident patterns are discovered by humans in postmortems. Slow, incomplete.
- **Files:** pkg/incident/patterns.go (NEW), pkg/incident/patterns_test.go (NEW)
- **AC:** `helix incident patterns` outputs: "auth bugs cluster in session refresh (12 incidents)," "agents from provider X have 3x incident rate on DB migrations." Patterns feed review context and agent assignment.
- **Logic:** Query incident DB grouped by (file_category, error_type, provider). Statistical comparison against baseline. Significant clusters flagged. Review context enriched with pattern matches.

## [ ] LOW: Implement agent skill transfer marketplace
- **Priority:** low
- **Plan:** specs/plans/phase-11-12-trust-learn.md §12.2
- **Gap:** Agents start from zero. Knowledge doesn't transfer between agents.
- **Files:** Extend pkg/marketplace with skill publication/loading
- **AC:** Agent with high trust in domain publishes skill → other agents load it. Skill effectiveness tracked — ineffective skills lose weight. Human quality gate on publication.
- **Logic:** Extend `marketplace.AgentProfile` with `SkillsPublished`. `Skill` struct with domain, effectiveness score, usage count. Skill loading in agent context assembly. Weighted by outcomes.

## [ ] LOW: Implement release signoff CLI with dual human+agent signature
- **Priority:** low
- **Plan:** specs/plans/phase-9-10-deploy-monitor.md §9.3
- **Gap:** Release signoff is human-only. No technical gate verification.
- **Files:** cmd/helix-release/main.go (NEW)
- **AC:** `helix release signoff --version v1.2.3` shows: all technical gates green/red, canary health, contract status. Requires BOTH human approval (intent) AND agent signoff (verification) before release proceeds.
- **Logic:** Dual signature dashboard. Human signs for intent/timing. Agent verifies all technical gates (Tier 1, Tier 2, evidence, contract, canary). Both required — neither sufficient alone.

## [ ] FOREMAN RULE: Always run wiring check before generating new tasks
Run: `for pkg in pkg/*/; do name=$(basename "$pkg"); [ ! -d "cmd/$name" ] && [ ! -d "cmd/helix-$name" ] && echo "UNWIRED: $name"; done`
If unwired packages exist, tasks MUST prioritize wiring them before any new package builds.
