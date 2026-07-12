# Helix Coding Tasks — Foreman Queue

## [x] HIGH: Wire BwrapExecutor to actually run bwrap (pkg/sandbox)
- **Priority:** critical — blocks all agent code execution
- **Completed:** 2026-07-08 — Run() fully implemented in executor.go:271-346. Creates session dirs, sets up cgroups, builds bwrap args, executes, handles timeout, cleans up. bwrap 0.11.1 present. go test ./pkg/sandbox/... PASS (1.044s).

## [x] HIGH: Wire dispatcher to live Forgejo (pkg/dispatcher)
- **Priority:** critical — this is the spine of Helix
- **Completed:** 2026-07-08 — ForgejoLoop fully implemented in forgejo_loop.go. Creates branches (idempotent on 409), opens PRs (idempotent on 409), has dry-run mode, lock acquisition, worktree creation, step execution. CLI subcommands `helix dispatch` and `helix dispatcher` wired. go test ./pkg/dispatcher/... PASS.

## [x] HIGH: Implement real model clients for adversarial review (pkg/review)
- **Priority:** critical — adversarial review is all stubs right now
- **Completed:** 2026-07-08 — Created ChimeraModelClient (multi-model deliberation API) and DeepSeekModelClient (OpenAI-compatible chat completions). Both implement ModelClient interface. Added `helix review run --pr N` CLI subcommand with multi-model panel construction, concurrent dispatch, evidence bundle generation, and JSON output. 16 new tests. go test ./... PASS (49 packages), GitReins Tier 1 PASS.

## [x] HIGH: Build change management dashboard (human review interface)
- **Priority:** high — human needs structured review, not diffs
- **Plans:** specs/plans/phase-5-6-review.md §6.1, specs/interaction-map.md §6.1
- **Completed:** 2026-07-09 — Implemented pkg/review/blast_radius.go (Go import-graph BFS via go/parser), pkg/review/dashboard.go (risk=category×tier+incident boost, ADR fit, trust context), wired `helix review dashboard --pr N --files ...` terminal+JSON. Root help lists verify/trust/mergegate/security. Tests pkg/review + cmd/helix PASS.
- **AC met:** (1) Blast radius packages/services/teams, (2) Risk score 0–100 with rationale, (3) Architectural fit via --adr-dir, (4) Trust context via --ledger/--tier. CI-ready --json output.

## [x] HIGH: Enforce merge gates at Git level (pre-receive hook)
- **Priority:** high — merge gates exist in code but not in git
- **Plan:** specs/plans/phase-7-8-negotiate-merge.md §Gap 2-4
- **Completed:** 2026-07-09 — Implemented pkg/mergegate/hook.go (pre-receive evaluation: stdin ref parsing, protected branch matching with glob patterns, changed-file collection via git diff-tree/ls-tree, 4 gate checks: evidence-on-disk, trust-tier, secrets-scan, commit-attestation). Added `helix mergegate hook` CLI subcommand. Created scripts/helix-pre-receive.sh (Forgejo pre-receive hook wrapper). 14 new tests including integration test with real git repo. E2E verified: protected branch push → evaluated, non-protected → skipped, tag push → skipped, branch deletion → rejected. make lint+test+build PASS (49 packages).
- **AC met:** (1) Push to protected branch with failing gates → rejected with structured JSON message, (2) Push with all gates passing → accepted, (3) Works via `helix mergegate hook` reading pre-receive stdin, (4) HELIX_SKIP_GATE=1 bypass for emergencies.

## [x] MEDIUM: Implement ideation system — Idea capture, validation, prioritization
- **Priority:** medium — new capability, not blocking existing flow
- **Plan:** specs/plans/phase-1-2-ideation-spec.md §1.1-1.3
- **Completed:** 2026-07-09 — pkg/ideation (types/store/validator/priority + tests), cmd/helix/idea.go wired into monorepo CLI. Offline concept agents @assumption-buster + @architecture-fit. Deterministic prioritizer with advocacy JSONL. Promote writes specs/ideas/*.md, blocks risk_score>=70. Ad-hoc verification 23/23. go test ./pkg/ideation + cmd/helix Idea tests PASS.
- **AC met:** (1) `helix idea capture --title/--body` → JSONL store, (2) Go API Capture/Store for agents, (3) `helix idea validate` offline multi-agent report, (4) `helix idea prioritize` composite score ranking, (5) `helix idea promote --to spec` creates placeholder + PromotedTo.

## [x] MEDIUM: Implement spec co-authoring with adversarial annotation
- **Priority:** medium
- **Completed:** 2026-07-11 — Implemented pkg/spec/types.go (Spec, SpecSection, SpecAnnotation, CompletenessReport, DimensionScore, CompletenessGap), pkg/spec/coauthor.go (SpecCoAuthor with spec-generator + spec-challenger rule-based agents), pkg/spec/completeness.go (SpecCompleteness with 12-dimension scoring), pkg/spec/store.go (SpecStore with YAML frontmatter markdown at ~/.helix/specs/). Wired `helix spec create|review|gap-analysis|approve|show|list` into unified CLI (cmd/helix/spec.go + cmd/helix/main.go). GitReins judge PASS (7/7 criteria). 29/29 ad-hoc verification.
- **AC met:** (1) `helix spec create <idea-id>` creates spec template with 5 standard sections, (2) Agent co-authoring adds annotations (edge_cases, failure_modes, incompleteness) with severity, (3) `helix spec review` shows annotations, (4) `helix spec gap-analysis` shows 12-dimension completeness score with gap identification.

## [x] MEDIUM: Implement ADR co-authoring and multi-model review
- **Priority:** medium
- **Plan:** specs/plans/phase-1-2-ideation-spec.md §2.2
- **Gap:** No structured ADR system. Architecture decisions are ad-hoc.
- **Completed:** 2026-07-11 — Implemented pkg/adr/types.go (ADR struct with alternatives, evidence links, review types), pkg/adr/coauthor.go (ADRCoAuthor proposes ADRs with tradeoff analysis), pkg/adr/review.go (ADRReviewer with multi-model consensus scoring), cmd/helix/adr.go (helix adr create|list|show|review|supersede CLI). Wired into cmd/helix/main.go. Unit tests in pkg/adr/adr_test.go. Ad-hoc verification 13/13. go build && go vet && go test -short PASS. Grok 4.5 worker + GitReins Tier 1 PASS. Two commits pushed.

## [x] MEDIUM: Implement design review with adversarial agents
- **Priority:** medium
- **Plan:** specs/plans/phase-1-2-ideation-spec.md §2.3
- **Gap:** Design reviews are meetings with no structured output. No adversarial challenge.
- **Completed:** 2026-07-11 — Implemented pkg/design/types.go (DesignReviewRequest, DesignReviewReport, ThreatMap types), pkg/design/review.go (DesignReviewDispatcher wrapping AdversarialAgentDispatcher with assumption-buster, redteam, cost-auditor, chaos-engineer, consistency-checker), cmd/helix/design.go (helix design review CLI with Change Management View). Wired into cmd/helix/main.go. Unit tests in pkg/design/design_test.go. Inline verification: go build && go vet && go test -short PASS. Grok 4.5 worker + GitReins Tier 1 PASS.

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
