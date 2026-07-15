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

## [x] MEDIUM: Implement API contract generation and breaking change detection
- **Priority:** medium
- **Plan:** specs/plans/phase-1-2-ideation-spec.md §2.4
- **Gap:** Contracts defined manually. No breaking change detection. No consumer impact analysis.
- **Files:** pkg/contract/types.go, pkg/contract/generate.go, pkg/contract/validate.go, pkg/contract/breaking.go, pkg/contract/store.go, cmd/helix/contract.go (all NEW; wired into monorepo CLI)
- **AC:** `helix contract create <spec-id> --format openapi` generates schema. `helix contract validate <contract-id>` runs multi-model validation. `helix contract diff <old> <new>` shows breaking changes and consumer impacts. `helix contract freeze` hashes + immutability.
- **Logic:** `ContractAuthor` generates OpenAPI/protobuf/GraphQL from spec. `ContractValidator` uses multi-model dispatch. `BreakingChangeDetector` diffs schemas + consumer catalog.
- **Completed:** 2026-07-12 — Foreman implemented directly (two workers failed: glm-5.2 silent exit, MiniMax-M3 diff-without-write). 8 files, +1471 lines, 15 tests. Build+vet+test PASS (full project). GitReins Tier 1 PASS. Commit d086bc5.

## [x] MEDIUM: Add trust-tier-gated task assignment to dispatcher
- **Priority:** medium
- **Plan:** specs/plans/phase-3-4-task-impl.md §3.2
- **Completed:** 2026-07-12 — Commit ec73389 (parallel tick). AssignAgent filters by tier, ValidateTierAssignment blocks self-assign above tier, FileCategoryTier maps IaC→Observed/auth→Trusted/CI/CD→Veteran. 8 files, +525/-33, 14 new tests. Guard PASS.

- **Completed:** 2026-07-12 — Commit 6d91f7f. Implemented ContextPackage, ContextResource, ExpandableResource, AssembleContext with budget-constrained assembly. Queries spec store, ADR store, incident store, git log, codebase walk. Tier-based token budgets (Provisional 12K, Observed 24K, Trusted 48K, Veteran 96K). 2 files, +636 lines, 11 tests. Guard PASS.

## [x] MEDIUM: Implement context auto-assembly for agent tasks
- **Priority:** medium
- **Plan:** specs/plans/phase-3-4-task-impl.md §3.3
- **Gap:** Agents start tasks with no context. Must discover specs, related PRs, architectural constraints manually.
- **Files:** pkg/dispatcher/context.go (NEW), pkg/dispatcher/context_test.go (NEW)
- **AC:** When task assigned, agent receives context package: (1) linked spec sections, (2) prior related PRs (from git log), (3) architectural constraints (from ADRs), (4) incident history for similar changes. Context fits in budget window. Agent can request expansion (costs tokens).
- **Logic:** `ContextAssembler` queries spec links, git history, ADR index, incident DB. Budget-constrained assembly. Token-aware trimming.

## [x] MEDIUM: Implement structured clarification protocol
- **Priority:** medium
- **Plan:** specs/plans/phase-3-4-task-impl.md §4.2
- **Completed:** 2026-07-13 — Foreman implemented directly (3 files, +902/-10 lines, 15 tests). pkg/dispatcher/clarification.go (ClarificationRequest, ClarificationResponse, ClarificationStore, AutoResolve), cmd/helix/dispatcher.go (clarify + clarifications subcommands, --answer flag). go build+vet+test PASS (50 packages), GitReins Tier 1 PASS. Commit 60d48cb.
- **AC met:** (1) Agent emits structured ClarificationRequest JSON when blocked, (2) ClarificationStore persists to ~/.helix/clarifications/<task-id>.json, (3) Human resolves via `helix dispatcher clarify <task-id> --answer "..."`, (4) AutoResolve checks existing resolutions and ADR store, (5) Clarification error wrapping propagates through step execution.

## [x] MEDIUM: Implement review load balancing and priority queue
- **Priority:** medium
- **Plan:** specs/plans/phase-5-6-review.md §6.3
- **Completed:** 2026-07-14 — Foreman implemented directly (both GLM-5.2 and MiniMax-M3 workers failed silently). 3 files, +1145 lines, 10 tests. ReviewQueue with priority scoring, ReviewAssigner with self-review prevention, HumanReviewFilter, SLATracker, LoadTracker. CLI: `helix review queue --status` and `helix review assign --pr <url>`. Build+vet+test PASS (50 packages), GitReins Tier 1 PASS. Commit 7b1d0c3.
- **AC met:** (1) ReviewQueue persists to ~/.helix/queue/reviews.json with priority scoring (risk × staleness), (2) ReviewAssigner distributes load with self-review prevention, tier routing, human gating, (3) HumanReviewFilter: CategoryContract always needs human, CategoryCosmetic+TierVeteran auto-merges, (4) Self-review prevention filters by AuthorAgentID, (5) SLATracker with per-category SLA durations (4h/24h/48h/72h), (6) LoadTracker for load-aware model selection, (7) `helix review queue --status` CLI, (8) `helix review assign --pr <url>` CLI.

## [x] MEDIUM: Implement structured dismissal protocol for human-agent disagreement
- **Priority:** medium
- **Plan:** specs/plans/phase-7-8-negotiate-merge.md §7.2
- **Gap:** Human dismisses agent finding with "ignore." No learning. No accountability.
- **Files:** pkg/review/dismissal.go (NEW), pkg/review/dismissal_test.go (NEW)
- **AC:** Human dismisses finding with structured reason (not_valid/not_applicable/already_addressed/risk_accepted). Dismissal feeds false positive tracker. Frequent incorrect dismissals reduce human override weight.
- **Logic:** `Dismissal` struct with reason enum, evidence. Feeds `FPTracker.Increment`. After N dismissals, human override weight decreases. Mutual accountability — both agent and human reviews tracked.
- **Completed:** 2026-07-14 — Foreman implemented directly (GLM-5.2 worker silent exit). 3 files, +806 lines, 21 new tests. DismissalReason enum (false_positive|already_handled|out_of_scope|architectural_decision), Dismissal struct, DismissalStore with JSONL persistence + override weight (0-5→1.0, 6-15→0.75, 16-30→0.50, 31+→0.25), DismissalHandler coordinating with FPTracker, ParseDismissal for DISMISS: comment blocks, `helix review dismiss` CLI. Build+vet+test PASS (53 packages). GitReins Tier 1 PASS. Commit 13978bc.

## [x] MEDIUM: Implement risk-level consensus thresholds for Chimera tiebreak
- **Priority:** medium
- **Plan:** specs/plans/phase-7-8-negotiate-merge.md §7.3
- **Completed:** 2026-07-14 — Board was stale; work committed as 03f9715. consensus.go (383 lines) with ConsensusCategory (contract/behavioral/resilience/cosmetic), RequiredQuorum (3/2/1/1), ComputeWeight, CheckOverride, ConsensusCalculator, HasQuorum. 19 tests. escalation.go handles ChimeraUnavailable → human fallback. All ACs verified: (1) contract=3/3, (2) behavioral=2/2, (3) cosmetic=1/1, (4) configurable per category, (5) escalation with structured PR comment.

## [x] MEDIUM: Implement incident attribution engine CLI
- **Priority:** medium
- **Plan:** specs/plans/phase-9-10-deploy-monitor.md §10.3
- **Gap:** pkg/incident has types but no CLI. Can't attribute incidents to agents.
- **Files:** cmd/helix/incident.go (extended, not new file — monorepo CLI)
- **AC:** `helix incident attribute --since 24h` traces causal chain from recent deploys → changed code paths → responsible agents. Output: author 70%, reviewer(s) 20%, approver 10%. Feeds trust ledger.
- **Logic:** Query deployment history → find changed files → git blame → agent identity lookup → attribution split. Time-weighted decay applied. Trust ledger update triggered.
- **Completed:** 2026-07-15 — Foreman implemented directly. Added `attribute` subcommand with --since (with shorthand conversion: 24h→24.hours, 7d→7.days), --limit, --json, --verbose flags. discoverChangePaths() queries git log → unique files → git blame porcelain → ChangePath list → AttributionEngine.Attribute() → table/JSON output. +286/-5 in cmd/helix/incident.go. Build+vet+test PASS. GitReins Tier 1 PASS. E2E verified: `helix incident attribute --since 48h` found 12 change paths (totalwindupflightsystems 83.3%, Bane 16.7%). Commit b2d20ff.

## [x] MEDIUM: Implement shadow verification CLI
- **Priority:** medium
- **Plan:** specs/plans/phase-9-10-deploy-monitor.md §9.1
- **Gap:** pkg/verify has contracts but no CLI to run shadow deployment.
- **Files:** cmd/helix-verify/main.go (NEW)
- **AC:** `helix verify shadow --contract <id> --duration 24h` launches shadow, mirrors production traffic, compares behavior, outputs differential. `helix verify canary --contract <id> --step 5` promotes canary to next step if healthy.
- **Logic:** `ShadowManager` wraps pkg/verify types. Launches shadow instance, captures behavior diff, auto-rollback on anomaly. `CanaryPromoter` ramps by trust tier schedule.
- **Completed:** 2026-07-15 — Commit 72193db. Created cmd/helix-verify/main.go (+419 lines) with 4 subcommands (shadow, canary, status, rollback) using cobra CLI. Wraps existing pkg/verify ShadowManager, CanaryPromoter, CanarySchedule, AutoRollback, ObservationWindowRemaining. Build+vet+test PASS (53 packages). GitReins Tier 1 PASS + Judge 7/7 PASS.

## [x] MEDIUM: Implement cross-agent notification bus and context sharing
- **Priority:** medium
- **Plan:** specs/plans/phase-11-12-trust-learn.md §12.3
- **Completed:** 2026-07-15 — Commit 0422604. Implemented pkg/learning/context_bus.go (552 lines: ContextBus, SharedFinding, domain pub/sub, tier budgets, critical bypass, JSONL persistence), pkg/learning/context_bus_test.go (371 lines, 18 tests), cmd/helix/notify.go (486 lines: publish|inbox|subscribe|unsubscribe|stream CLI). Wired into cmd/helix/main.go (+13 lines). Build+vet+test PASS (54 packages). GitReins Tier 1 PASS. E2E verified.
- **AC met:** (1) Publish/GetInbox with domain matching and direct addressing, (2) Tier-based daily budgets (Provisional=10, Veteran=50), critical bypass, (3) `helix notify stream` for human observation, (4) JSONL persistence + subscription store at ~/.helix/context_bus/.

## [x] MEDIUM: Implement model evaluation and rotation based on production outcomes
- **Priority:** medium
- **Plan:** specs/plans/phase-11-12-trust-learn.md §12.4
- **Gap:** Models evaluated by benchmarks, not production outcomes. Underperforming models not rotated.
- **Files:** pkg/learning/model_eval.go (NEW), pkg/learning/model_eval_test.go (NEW), cmd/helix/models.go (NEW — wired into monorepo CLI)
- **AC:** Model X incident rate vs Model Y computed from incident DB. Models with >15% false positive rate auto-removed from review rotation. Dashboard: `helix review models` shows per-model stats.
- **Logic:** `ModelEvaluator` queries incident DB + false positive tracker. Computes per-model incident rate, FP rate, review accuracy. Auto-rotation trigger: >15% FP rate or >2x incident rate of next best model.
- **Completed:** 2026-07-15 — Foreman implemented directly (both GLM-5.2 and kimi-k2.7 workers silent-exit). 4 files, +1255/-3 lines, 25 tests. ModelEvaluator with RecordMerge/RecordIncident/RecordReview/Evaluate/EvaluateAll. Rotation rules: FPR>15% removal, IR>2x fleet avg flagging, 14 consecutive days permanent removal, 30 clean days re-admission. Selection scoring formula (trust*0.70 + (1-IR)*0.20 + (1-costEff)*0.10). CLI: `helix models list|show|evaluate|rotate` with --json output. Build+vet+test PASS (55 packages). GitReins Tier 1 PASS. Commit a4656fb.
- **AC met:** (1) ModelEvaluator tracks per-model incident and FP rates with RecordMerge/RecordIncident/RecordReview, (2) FPR>15% auto-removal, IR>2x fleet avg flagging, 14-day permanent + 30-day re-admit, (3) SelectionScore formula weights trust over model metrics, (4) `helix models list --sort-by incident-rate|false-positive-rate|cost` with table and JSON output.

## [x] LOW: Implement pattern discovery across incident database
- **Priority:** low
- **Plan:** specs/plans/phase-11-12-trust-learn.md §12.1
- **Gap:** Incident patterns are discovered by humans in postmortems. Slow, incomplete.
- **Files:** pkg/learning/miner.go (NEW), pkg/learning/miner_test.go (NEW)
- **AC:** `helix incident patterns` outputs: "auth bugs cluster in session refresh (12 incidents)," "agents from provider X have 3x incident rate on DB migrations." Patterns feed review context and agent assignment.
- **Logic:** Query incident DB grouped by (file_category, error_type, provider). Statistical comparison against baseline. Significant clusters flagged. Review context enriched with pattern matches.
- **Completed:** 2026-07-15 — Foreman implemented directly. 3 files, +1417 lines, 17 tests. PatternMiner with 6 discovery algorithms (category clusters, provider correlation, change type risk, time-based, review gaps, severity clusters). Confidence scoring with hypothesis/established thresholds. CLI: `helix incident patterns list|show|discover` with --json/--category/--min-confidence flags. Build+vet+test PASS (55 packages), GitReins Tier 1 PASS. Commit 5dc5c9e.

## [x] LOW: Implement agent skill transfer marketplace
- **Priority:** low
- **Plan:** specs/plans/phase-11-12-trust-learn.md §12.2
- **Completed:** 2026-07-15 — grok-4.5 @ xai-oauth worker. Commit c551c6c. 5 files, +1238 lines, 19 tests. pkg/learning/skills.go (SkillRegistry+FileSkillStore with publish gates: trust≥0.65 + domain≥5 merges, Load/List/Deprecate/RecordOutcome with auto-deprecation <0.3), pkg/learning/skills_test.go, pkg/marketplace/types.go (AgentProfile.SkillsPublished), cmd/helix/learn.go (list|publish|deprecate|show CLI), cmd/helix/main.go (learn case + usage). Build+vet+test PASS (55 packages), GitReins Tier 1 PASS.
- **AC met:** (1) SkillRegistry gates publication by trust tier + domain merges, (2) Skills loaded by Load(domain, limit) sorted by trust_weight, (3) RecordOutcome tracks success/failure with weight updates + auto-deprecation, (4) `helix learn skills list|publish|deprecate|show` CLI wired.

## [x] LOW: Implement release signoff CLI with dual human+agent signature
- **Priority:** low
- **Plan:** specs/plans/phase-9-10-deploy-monitor.md §9.3
- **Completed:** 2026-07-15 — Foreman implemented directly. Commit eb5a607. 1 file, +738 lines. cmd/helix-release/main.go with 3 subcommands: signoff (dashboard), approve (human intent), status (signoff state). Agent evaluates 4 technical gates (shadow_verification, canary_readiness, trust_tier, behavior_contracts) automatically. Human approves change intent; cannot override agent gates. Both signatures mandatory. Persists to ~/.helix/releases/<version>.json. build+vet+test PASS (55 packages), GitReins Tier 1 PASS, Judge 5/5 PASS.
- **AC met:** (1) Agent technical signoff fully automated, (2) Human signoff dashboard shows change-management view, (3) Both signatures mandatory, (4) Signoff events recorded in append-only JSONL store, (5) No human override of agent technical gates.
- **Gap:** Release signoff was human-only with no technical gate verification.
- **Files:** cmd/helix-release/main.go (NEW)

## [ ] DOC: Update README component table — stale, only lists 7 of 42 packages
- **Priority:** low — documentation hygiene
- **Gap:** README component table (lines 76-82) lists only 7 components: identity, estimate, negotiate, prompt, marketplace, dispatcher, sandbox. Project has 42 pkg/ directories and 10 cmd/ directories including recent additions: review (dashboard/load-balance/dismissal), learning (context_bus/model_eval/miner/skills), incident (attribution/patterns), ideation, spec, adr, design, contract, mergegate, verify, release, notify, models. Multiple standalone binaries missing from table: helix-release, helix-verify, sandbox.
- **AC:** Component table in README lists all major packages with descriptions. Table format matches existing style (Component | Package | CLI | Description).

## [ ] FOREMAN RULE: Always run wiring check before generating new tasks
Run: `for pkg in pkg/*/; do name=$(basename "$pkg"); [ ! -d "cmd/$name" ] && [ ! -d "cmd/helix-$name" ] && echo "UNWIRED: $name"; done`
If unwired packages exist, tasks MUST prioritize wiring them before any new package builds.

## [x] Fix CI: Helix CI — consecutive failures (#204-#208), lint failures resolved
- **Root cause:** golangci-lint failures — 4 gofmt, 8 errcheck in test files, 1 unused function (tokenizeReader)
- **Fix:** Commit c5c1777 — gofmt'd 4 files, added `_ =` to 8 unchecked error returns, removed unused tokenizeReader+bufio import
- **Verification:** go build+vet+test PASS (all packages), golangci-lint CLEAN (exit 0)

## [x] Fix CI: totalwindupflightsystems/helix — run #223
- **Root cause:** golangci-lint failures — gofmt, unused function, unnecessary fmt.Sprintf
- **Fix:** Commit 9d4b442 — gofmt alignment + removed unused categoryCluster + fmt.Sprintf clean
- **Verification:** CI run #223 PASS (completed success, 49s). All subsequent runs green.