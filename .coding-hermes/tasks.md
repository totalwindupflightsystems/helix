# Helix Coding Tasks — Foreman Queue

## [x] Implement trust scoring engine — pkg/trust/
- **Priority:** high
- **Spec:** specs/trust-model.md
- **Model:** direct write — Go package, pure math + JSONL
- **Files:** pkg/trust/scorer.go, pkg/trust/ledger.go, pkg/trust/tiers.go, pkg/trust/scorer_test.go
- **AC:** `go build ./... && go test ./pkg/trust/... -count=1 -cover` passes with >80% coverage
- **Logic:** TrustScore calculation (6 dimensions: merge success 0.25, incident attribution 0.30, review consensus 0.15, prompt integrity 0.10, human feedback 0.10, tenure 0.10), tier thresholds (Provisional/Observed/Trusted/Veteran), incident attribution with time-decay weight (100% at 0-7d, 50% at 8-30d, 10% at 31-90d, 0% after 90d), trust decay on inactivity (0.05/week), tier demotion logic, JSONL ledger append + replay verification.
- **Result:** [x] 59 tests, 86.8% coverage. Committed at `f06918d`.

## [x] Implement bias-stripper for adversarial review — pkg/review/
- **Priority:** high
- **Spec:** specs/adversarial-review.md §Confirmation Bias Defense
- **Model:** direct write — Go package, pure text processing
- **Files:** pkg/review/bias_stripper.go, pkg/review/bias_stripper_test.go
- **AC:** `go build ./... && go test ./pkg/review/... -count=1 -cover` passes with >85% coverage
- **Logic:** Strip evaluative language ("fixed", "correct", "ready", "passes"), remove confidence assertions ("tested locally", "works on my machine"), strip emoji and emotional framing, normalize formatting, preserve factual information (files changed, intent). Tested with 8 documented disaster commit messages.
- **Result:** [x] 33 tests, 97.4% coverage. Committed at `d821703`.

## [x] Implement production verification contracts — pkg/verify/
- **Priority:** medium
- **Spec:** specs/production-verification.md §Behavior Contracts
- **Model:** direct write — Go package, YAML contracts + metrics assertions
- **Files:** pkg/verify/contract.go, pkg/verify/monitor.go, pkg/verify/contract_test.go
- **AC:** `go build ./... && go test ./pkg/verify/... -count=1 -cover` passes with >80% coverage
- **Logic:** Behavior contract YAML parsing, assertion types (success rate gte/lte, latency p50/p95/p99, error count eq), breach detection, auto-rollback trigger logic, agent notification on breach, canary ramp schedule by trust tier (Provisional: 96h, Observed: 60h, Trusted: 36h, Veteran: 12h), drift detection, shadow rollback triggers.
- **Result:** [x] 51 tests, 96.9% coverage. Committed at `1b2b6d3`.

## [x] Implement evidence bundle signing — pkg/review/

## [x] Fix CI: Helix CI — golangci-lint errcheck failures in test files
- **Priority:** high
- **Branch:** master
- **CI URL:** https://github.com/totalwindupflightsystems/helix/actions/runs/28345972923
- **Error:** golangci-lint failing on unchecked `os.MkdirAll` and `os.WriteFile` return values in pkg/dispatcher/loop_test.go (lines 271, 272, 292). Lint job fails, all other jobs pass.
- **Result:** [x] Fixed 13 unchecked error returns across 6 subtests in loop_test.go. Applied `_ = os.MkdirAll`, `_ = os.Chdir`, `_ = os.WriteFile` pattern. Also fixed gofmt struct alignment and empty-branch SA9003. Lint clear, tests pass (0.004s), build OK. Committed at `d6a20ba`.

## [x] Upgrade deps: helix — 5 outdated Go packages
- **Priority:** medium
- **Updates:** cpuguy83/go-md2man/v2 v2.0.6→v2.0.7, spf13/pflag v1.0.9→v1.0.10, stretchr/testify v1.10.0→v1.11.1, stretchr/objx v0.5.2→v0.5.3, gopkg.in/check.v1→v1.0.0-20201130134442
- **Result:** [x] All 5 upgraded via `go mod edit -require` + `go mod tidy`. Build OK, full suite 20/20 packages pass, lint guard PASS. Committed at `bec8a7a`.

## [x] Add trust tier enforcement to GitReins pre-commit hook
- **Priority:** high
- **Spec:** specs/trust-model.md §Integration Points
- **Files:** .gitreins/config.yaml, scripts/check-trust-tier.sh (NEW)
- **AC:** GitReins pre-commit blocks merges from agents below required trust tier for changed file categories
- **Logic:** File category mapping (IaC → Tier 1+, CI/CD → Tier 3+, auth → Tier 2+), trust tier from agent, hook: query agent trust → compare to file requirements → block/report.
- **Result:** [x] Script created with 7 file category classifiers. Integrated into GitReins tier1 pipeline. Committed at `0b80427`.

## [x] Implement false positive feedback loop — pkg/review/
- **Priority:** low
- **Spec:** specs/adversarial-review.md §False Positive Feedback Loop
- **Model:** direct write — Go package, counters + thresholds
- **Files:** pkg/review/false_positive.go, pkg/review/false_positive_test.go
- **AC:** `go build ./... && go test ./pkg/review/... -count=1 -cover` passes with >85% coverage
- **Logic:** False positive tracking (human_dismissed counter per model), threshold (10 dismissals → flag), model re-evaluation trigger, model rotation (>15% FP rate).
- **Result:** [x] 19 FP tests, 100% line coverage on FPTracker. 88.9% total pkg/review.

## [x] Write incident learning database schema — pkg/incident/
- **Priority:** medium
- **Spec:** specs/incident-learning.md (TO BE WRITTEN)
- **Files:** pkg/incident/types.go, pkg/incident/types_test.go
- **AC:** `go build ./...` passes
- **Logic:** Incident struct (agent_id, pr_url, severity, causal_chain, timestamp), shared learning across agents, incident → trust penalty pipeline.
- **Result:** [x] 11 tests, 100% coverage.

## [x] Create Helix bootstrap script
- **Priority:** high
- **Spec:** specs/build-order.md §9
- **Result:** [x] Bootstrap script with 4-phase automation: prerequisites check (Docker/Go/Python/curl), Forgejo container start with health retry, Chimera venv install + start, Helix CLI build, 5-point verification. 199 lines. Committed.

## [x] Create Docker Compose for Helix platform
- **Priority:** high
- **Spec:** specs/deployment.md §2
- **Result:** [x] docker-compose.yaml with Forgejo + Chimera + LangFuse + Postgres, all on helix-net bridge with health checks. Placeholder templates for Consensus/Muster/Hivemind (uncomment when repos cloned). Committed.

## [x] Implement circuit breaker for cross-service HTTP calls
- **Priority:** medium
- **Spec:** specs/cross-component-wiring.md §8
- **Result:** [x] Already implemented in pkg/integration/types.go — CircuitBreaker with Allow/RecordSuccess/RecordFailure, half-open probe, configurable MaxFailures/ResetTimeout. 10 tests, 100% coverage on all methods. No new files needed.

## [x] Create platform config templates
- **Priority:** low
- **Spec:** specs/helix-config.md
- **Result:** [x] deploy/config.yaml.example (all 10 sections: forgejo, chimera, langfuse, gitreins, identity, estimator, marketplace, negotiation, prompts, budget) + deploy/pricing.yaml.example (6 providers, cache config, 5 task types). Committed.

## [x] Implement health checker for startup validation
- **Priority:** high
- **Spec:** specs/cross-component-wiring.md §8 + specs/helix-config.md §7
- **Model:** direct write — Go package
- **Files:** pkg/health/checker.go, pkg/health/checker_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/health/... -count=1 -cover` passes with >80% coverage
- **Logic:** HealthChecker struct that probes all configured services at startup. Concurrency-safe parallel health checks. Returns aggregated HealthReport (pass/fail per service). Configurable timeouts per service. Used by all CLI tools to fail-fast on unreachable services.

## [x] Implement Forgejo API client wrapper
- **Priority:** high
- **Spec:** specs/cross-component-wiring.md §2, specs/agent-identity.md
- **Model:** direct write — Go package
- **Files:** pkg/forgejo/client.go, pkg/forgejo/client_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/forgejo/... -count=1 -cover` passes with >80% coverage
- **Logic:** ForgejoClient struct wrapping REST API calls: CreateUser, GetUser, CreateSSHKey, CreatePAT, ListPRs, GetPRReviews, CreatePRReview. BasicAuth support. Circuit breaker integration. Retry with backoff on 5xx. Test with httptest.NewServer mock.

## [x] Create `.forgejo/workflows` CI/CD pipeline files
- **Priority:** medium
- **Spec:** specs/deployment.md §5
- **Result:** [x] 3 workflow files created: gitreins.yaml (Tier 1 on push, Tier 2 on PR), chimera-review.yaml (multi-model PR review with fallback), promptfoo.yaml (prompt regression tests on prompt changes). All reference correct service URLs from deployment.md §3.

## [x] Wire real bwrap execution in sandbox executor — pkg/sandbox/
- **Priority:** high
- **Spec:** specs/sandbox.md §12 (Implementation Status → Wiring the Real Execution)
- **Model:** direct write — Go package, bwrap IS installed at /usr/bin/bwrap
- **Files:** pkg/sandbox/executor.go, pkg/sandbox/executor_test.go, pkg/sandbox/executor_extended_test.go
- **AC:** `go build ./... && go test ./pkg/sandbox/... -count=1 -cover` passes with >80% coverage
- **Logic:** Replace ErrNotImplemented stub in Run() with real bwrap invocation. Handle IsolationNone (direct exec), workspace/full (bwrap args). Context-aware timeout enforcement. Process group management for clean SIGKILL on timeout. Defer chain for session dir + cgroup cleanup. Promote underscore-prefixed helpers to real functions.
- **Result:** [x] Run() now executes real bwrap for workspace/full isolation, runs directly for IsolationNone. Added WritePID to CgroupV2 for PID→cgroup.procs wiring. Promoted 5 underscore-prefixed helpers to real functions. 11 new tests covering real bwrap execution, timeout enforcement, session cleanup, WritePID, bwrap-not-found, empty-command, and binary discovery. 93.8% coverage (up from 92.5%). Full suite 24/24 pass.

## [ ] Wire dispatcher to Forgejo — agent spawn pipeline
- **Priority:** critical
- **Spec:** specs/dispatcher.md + specs/agent-identity.md
- **Model:** deepseek-v4-pro — integration work, needs live services
- **Files:** pkg/dispatcher/forgejo_spawn.go, pkg/dispatcher/spawn_test.go
- **AC:** `helix dispatch --spec specs/agent-identity.md --agent test-agent` creates a branch in Forgejo, provisions an agent, and returns a PR URL
- **Logic:** Full Ralph Loop: acquire lock → create worktree → spawn agent → wait for completion → run GitReins guards → open PR → return URL. Requires Forgejo running on :3030.
- **Note:** Blocked until Forgejo is running. Cannot test without live service.

## [x] Implement OpenRouter key budget client — pkg/estimate/
- **Priority:** high
- **Spec:** specs/cost-estimator.md §9.1 (OpenRouter Key Budget Query)
- **Model:** direct write — Go package, real HTTP client
- **Files:** pkg/estimate/openrouter.go, pkg/estimate/openrouter_test.go
- **AC:** `go build ./... && go test ./pkg/estimate/... -count=1 -cover` passes with >85% coverage
- **Logic:** Replace ErrNotImplemented stubs with real HTTP GET calls to OpenRouter API (/api/v1/key). Parse JSON response for usage and limit. Return cost data. Handle 401 (dead key), 429 (rate limited), 5xx (retry). Test with httptest mock server. Context-aware with timeout.
- **Result:** [x] Real HTTP client with GetKeyUsage, GetKeyLimit, GetKeyRemaining, GetKeyInfo. Context-aware. Error sentinels: ErrAuthFailed (401), ErrRateLimited (429). KeyInfo with BudgetRemaining/BudgetUsed fraction helpers. 13 tests with httptest mock: success, 401, 429, 500, empty key, malformed JSON, context cancelled, auth header verification, full response parsing. 92.8% pkg/estimate coverage. Full suite 24/24 pass.

## [x] Implement marketplace daily trust recalculation — pkg/marketplace/
- **Priority:** medium
- **Spec:** specs/agent-marketplace.md §7.4 (Daily Trust Recalculation)
- **Model:** direct write — Go package, data aggregation
- **Files:** pkg/marketplace/scorer.go (extend), pkg/marketplace/scorer_extended_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/marketplace/... -count=1 -cover` passes with >85% coverage
- **Logic:** Replace no-op stub in DailyRecalculation. Read agent manifests from marketplaceDir, recompute trust from existing Scorer data (merge success, review quality, task completion). Write updated manifests back to disk. Log to recalculation.log. Handles missing directories gracefully.
- **Result:** [x] DailyRecalculation now reads manifests, recomputes trust from Performance metrics (PR acceptance rate, budget adherence, human ratings), applies time-based decay, writes updated manifests back, logs to recalculation.log. Handles PrAcceptanceRate/BudgetAdherence=0 as "not tracked". 11 tests: single agent, multiple agents, retired skip, no-tasks base score, budget overruns, human rating bonus, malformed skip, log written, empty/nonexistent dirs. 93.6% pkg/marketplace coverage. Full suite 24/24 pass.

## [x] Implement merge gate validator — pkg/mergegate/
- **Priority:** high
- **Spec:** specs/adversarial-review.md §Integration Points + specs/production-verification.md §Integration Points
- **Model:** direct write — Go package, composes existing components
- **Files:** pkg/mergegate/gate.go, pkg/mergegate/gate_test.go
- **AC:** `go build ./... && go test ./pkg/mergegate/... -count=1 -cover` passes with >85% coverage
- **Logic:** MergeGate that validates all preconditions before allowing a merge:
  1. Evidence bundle exists and signatures are valid (pkg/review.EvidenceBundle)
  2. Behavior contract exists and assertions are well-formed (pkg/verify.BehaviorContract)
  3. Trust tier meets minimum requirement for changed file categories (scripts/check-trust-tier.sh logic)
  4. Consensus threshold was met (from review.ReviewOrchestrator)
  5. Cost guard was approved (pkg/dispatcher.CostGuard)
  Returns MergeDecision (ALLOWED/BLOCKED/ESCALATED) with per-check results and reason messages.
- **Result:** [x] MergeGate composes 5 checks: evidence bundle, consensus, behavior contract, trust tier, cost guard. ALLOWED/BLOCKED/ESCALATED decisions. 55 tests, 95.7% coverage. Full suite 24/24 pass.

## [x] Implement PR negotiation cost reconciliation — pkg/negotiate/
- **Priority:** medium
- **Spec:** specs/pr-negotiation.md §9.3 (Cost Split)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/negotiate/cost_recon.go (NEW), pkg/negotiate/cost_recon_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/negotiate/... -count=1 -cover` passes with >85% coverage
- **Logic:** CostReconciler tracks debate costs across rounds, splits tie-break costs between disagreeing agents, checks against agent weekly budgets (pkg/estimate.BudgetTracker), and flags cost overruns. Report with per-agent cost breakdown.
- **Result:** [x] CostReconciler with round-by-round cost tracking, even tie-break split (spec §9.3), budget exhaustion detection (spec §14 exit 3), escalation flagging with BUDGET_EXHAUSTED reason. 28 tests, 97.9% pkg/negotiate coverage (up from 97.3%). Full suite 24/24 pass.

## [x] Implement incident learning feedback loop — pkg/incident/
- **Priority:** medium
- **Spec:** specs/adversarial-review.md §Integration Points: "All incidents → learning database → future review training"
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/incident/learning.go (NEW), pkg/incident/learning_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/incident/... -count=1 -cover` passes with >85% coverage
- **Logic:** LearningDatabase stores incident patterns, maps them to review criteria. When a similar code change is detected (by file category, change type), the system surfaces relevant past incidents as review context. Pattern similarity scoring (keyword overlap + severity match). FeedReviewContext returns past incidents relevant to a new PR.
- **Result:** [x] LearningDatabase with similarity-ranked retrieval. Jaccard category overlap (40%), keyword overlap (40%), change type match (10%), high-severity boost (10%). CategorizeFile for 12 categories. StoreFromIncident with keyword extraction. FeedReviewContext returns ranked items + accumulated review criteria. 40 tests, 98.4% pkg/incident coverage (up from 100% — now includes new code). Full suite 24/24 pass.

## [x] Implement retry middleware with exponential backoff
- **Priority:** medium
- **Spec:** specs/cross-component-wiring.md §7
- **Result:** [x] Generic WithBackoff[T] function with exponential backoff + jitter. IsRetryable detects network errors, 5xx, 429. DoHTTP convenience wrapper for http.Client. Context-aware cancellation. 30 tests, 95.0% coverage.

## [x] Implement trust tier promotion engine — pkg/trust/
- **Priority:** high
- **Spec:** specs/trust-model.md §Trust Tiers + §Tier Thresholds
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/trust/promotion.go (NEW), pkg/trust/promotion_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/trust/... -count=1 -cover` passes with >85% coverage
- **Logic:** TierPromotionEngine evaluates whether an agent qualifies for tier promotion. Checks ALL entry criteria from spec: trust score threshold (Provisional 0.0, Observed 0.40, Trusted 0.65, Veteran 0.85), minimum merge count (100/500/2000), maximum attributable incidents (0 for Observed/Trusted, 1 for Veteran in 180d), minimum days active (30/90/180), and for Veteran: minimum PR reviews (50). ShouldPromote returns bool + reason. PromoteTo returns the target tier. EvaluatePromotion checks all criteria and returns a PromotionResult with per-criterion pass/fail. Integrates with existing ShouldDemote/DemoteTo for a complete tier lifecycle.
- **Result:** [x] EvaluatePromotion with per-criterion pass/fail (score, merges, incidents, days active, PR reviews for Veteran). ShouldPromote/PromoteTo for single-step promotion check. EvaluateFullTierCycle for complete lifecycle (promotion-first, demotion-aware). TierRank/IsPromotion/IsDemotion helpers. 38 tests, 91.3% pkg/trust coverage (up from 89.8%). Full suite 24/24 pass.

## [x] Implement cross-service error propagation — pkg/integration/
- **Priority:** medium
- **Spec:** specs/cross-component-wiring.md §7 (Error Propagation)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/integration/errors.go (NEW), pkg/integration/errors_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/integration/... -count=1 -cover` passes with >85% coverage
- **Logic:** Centralized error type mapping for all cross-service failures per spec §7 table. Each service pair has specific error format: Forgejo→Chimera unreachable → "Chimera unavailable — manual review required"; negotiate→Chimera budget exhausted → "BUDGET_EXHAUSTED: tie-break cost $X > remaining"; identity→Forgejo 503 → "CONNECTION_REFUSED: retry in Ns (attempt N/M)"; estimate→OpenRouter 401 → "AUTH_FAILED: agent key is dead — trigger key rotation"; Axiom→Forgejo 409 → "BRANCH_CONFLICT: feat/X exists — use --force-branch". ServiceError type with Code, Message, Retryable flag, RetryAfter duration. ClassifyError maps HTTP status codes to error types. IsRetryable for circuit breaker integration.
- **Result:** [x] 49 tests, 100% coverage on errors.go. All 5 spec §7 error rows implemented as constructors (NewChimeraUnavailableError, NewBudgetExhaustedError, NewConnectionRefusedError, NewAuthFailedError, NewBranchConflictError). ClassifyError dispatches by caller/callee/status. ClassifyHTTP handles 401/403/404/409/429/500/502/503/504 + generic 4xx/5xx. IsRetryable/IsCode/GetRetryAfter helpers for circuit breaker integration. Full suite 24/24 pass.

## [x] Implement agent notification dispatcher — pkg/verify/
- **Priority:** medium
- **Spec:** specs/production-verification.md §Behavior Contracts + §Integration Points
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/verify/notify.go (NEW), pkg/verify/notify_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/verify/... -count=1 -cover` passes with >85% coverage
- **Logic:** NotificationDispatcher sends breach alerts to responsible agents when behavior contracts are violated. Per spec: on breach → (1) immediate agent notification with evidence, (2) auto-rollback if configured, (3) trust penalty, (4) incident record. Notifier interface with Notify(agentID, breach, evidence) method. Channels: Forgejo PR comment (structured markdown with breach details), trust ledger event, incident store entry. BreachNotification with contract name, failed checks, metrics snapshot, evidence links, recommended action. NotificationResult tracking delivery status per channel. Debounce: don't spam the same agent for the same breach within 5 minutes.
- **Result:** [x] 44 tests, 100% coverage on notify.go. Three channels: ForgejoPRNotifier (markdown comment), TrustLedgerNotifier (penalty callback), IncidentStoreNotifier (incident record). 5-minute debounce per (agent, contract) pair. NotifyFromBreach converts Monitor Breach → notification. Full pipeline test: Monitor.Evaluate → breach → dispatcher → all channels. 97.7% pkg/verify coverage (up from 97.3%). Full suite 24/24 pass.

## [x] Implement cost estimation engine
- **Priority:** high
- **Spec:** specs/cost-estimator.md
- **Model:** direct write — Go package
- **Files:** pkg/estimate/calculator.go, pkg/estimate/calculator_test.go (NEW or extend existing)
- **AC:** `go build ./... && go test ./pkg/estimate/... -count=1 -cover` passes with >80% coverage
- **Logic:** Pre-flight token burn estimation: parse task type (spec/code/review/refactor/test), multiply by estimated token counts, apply cache hit ratios, compute dollar cost per provider, compare against agent weekly budget, return APPROVED/BLOCKED/ESCALATED. Use pricing.yaml data structure.
- **Result:** [x] Already implemented — 12 files across pkg/estimate/ (types, pricing, estimator, budget, reconciliation, calibrator, openrouter stub, CLI) + cmd/helix-estimate/ (3 subcommands: estimate, check, report). 94.0% coverage. Build + vet clean.

## [x] Implement shadow deployment manager
- **Priority:** medium
- **Spec:** specs/production-verification.md §Shadow Verification
- **Model:** direct write — Go package
- **Files:** pkg/verify/shadow.go, pkg/verify/shadow_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/verify/... -count=1 -cover` passes with >80% coverage
- **Logic:** ShadowLaunch(agent, config): deploy agent to dark path, route 0% production traffic, collect behavior metrics, compare against baseline. PromoteToCanary(agent, tier): route 1% traffic by trust tier. AutoRollback(agent): revert on contract breach. Configurable observation window.
- **Result:** [x] 38 new tests in shadow_test.go, 97.2% pkg/verify coverage (up from 96.9%). ShadowDeployment lifecycle: Idle→Shadowing→ShadowPassed/Failed→Canaried→Promoted/RolledBack. Full DifferentialReport with per-metric deltas (success rate, P99 latency, error types, memory growth). Auto-rollback on all 4 spec triggers. Tier-specific canary schedules (Provisional 96h, Observed 60h, Trusted 36h, Veteran 12h). Thread-safe with sync.RWMutex.

## [x] Implement multi-model adversarial review orchestrator — pkg/review/
- **Priority:** high
- **Spec:** specs/adversarial-review.md §Multi-Model Adversarial Review
- **Model:** direct write — Go package
- **Files:** pkg/review/orchestrator.go, pkg/review/orchestrator_test.go
- **AC:** `go build ./... && go test ./pkg/review/... -count=1 -cover` passes with >80% coverage
- **Logic:** ReviewOrchestrator that dispatches a review to 3 models from different providers, collects independent findings, reconciles consensus (all 3 agree → PASS, 2/3 agree → WARN, 1/3 or divergence → FLAG), builds evidence bundle with model diversity score, integrates with existing BiasStripper (strip bias before each model sees the code). Provider diversity requirement: at least 2 different provider families in every review panel.
- **Result:** [x] 31 tests, 100% coverage on all orchestrator functions, 93.4% total pkg/review. Full pipeline: bias strip → validate diversity → concurrent dispatch to N models → collect findings → reconcile consensus → build evidence bundle. ChangeCategory formation routing (Contract=3 models, Behavioral=2, Resilience/Cosmetic=1). FPTracker integration (removed models rejected). Context-aware with cancellation support.

## [x] Implement prompt lifecycle state machine — pkg/prompt/
- **Priority:** high
- **Spec:** specs/prompt-registry-v2.md §Lifecycle State Machine
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/prompt/lifecycle.go (extend), pkg/prompt/lifecycle_extended_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/prompt/... -count=1 -cover` passes with >80% coverage
- **Logic:** State machine: draft → proposed → reviewed → attested → active → deprecated → retired. Transition validation (only valid transitions allowed), atomic state transitions, state persistence in metadata.yaml, age-based auto-deprecation (promotes active→deprecated after N days if no activity). Integrate with existing attester.go and registry.go.
- **Result:** [x] Extended lifecycle.go with ApplyTransition (atomic state writes with audit trail + timestamp tracking), AutoDeprecationConfig (spec defaults: 90d inactivity deprecation, 90d in deprecated retire, 180d no-commits retire, 3+ newer commits auto-deprecate), ShouldDeprecate (dual trigger: inactivity + newer version commits), ShouldRetire (dual trigger: time in deprecated + no-commit inactivity). 23 new tests, 100% coverage on all new functions. 93.4% total pkg/prompt.

## [x] Implement incident attribution engine — pkg/incident/
- **Priority:** medium
- **Spec:** specs/production-verification.md §Production Incident Attribution
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/incident/attribution.go, pkg/incident/attribution_test.go
- **AC:** `go build ./... && go test ./pkg/incident/... -count=1 -cover` passes with >80% coverage
- **Logic:** Trace causal chain from incident → changed code paths → merge commit → responsible agent. Attribution weights: author 70%, reviewers 20% (shared), approving human 10%. Feed attribution result into trust scoring engine (pkg/trust). Record evidence links in incident record. Multiple agents → shared responsibility distribution.
- **Result:** [x] AttributionEngine with spec-compliant weights (author 70%, reviewers 20% shared, approver 10%). Multi-path normalization (sums to 1.0). TrustPenalty with severity multipliers (low 0.05, medium 0.10, high 0.20, critical 0.40). ApplyTrustPenalties callback for trust engine integration. FindResponsiblePaths filters by causal chain. MergeAttribution for multi-incident aggregation. 28 tests, **100% coverage** on entire pkg/incident.

## [x] Fix CI: Helix CI — golangci-lint failures (gofmt, errcheck, unused funcs, SA9003)
- **Priority:** high
- **Branch:** master
- **CI Run:** https://github.com/totalwindupflightsystems/helix/actions/runs/28372979462
- **Errors:**
  1. `os.Chmod` unchecked in pkg/sandbox/cgroups_test.go (lines 221, 322)
  2. `s.RecordMerge` unchecked in pkg/marketplace/scorer_advanced_test.go (lines 288, 289)
  3. `func executeRoot` unused in cmd/helix-prompt/main_test.go
  4. gofmt issues: pkg/verify/contract.go, monitor.go, shadow.go, contract_test.go
  5. SA9003 empty branches: pkg/prompt/registry_extended_test.go:592, pkg/review/bias_stripper_test.go:200, pkg/verify/shadow_test.go:642

## [x] Wire trust scoring to incident attribution — pkg/trust + pkg/incident
- **Priority:** high
- **Spec:** specs/trust-model.md §Integration Points + specs/production-verification.md §Production Incident Attribution
- **Model:** direct write — Go packages, cross-package integration
- **Files:** pkg/trust/integration.go (NEW), pkg/trust/integration_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/trust/... ./pkg/incident/... -count=1 -cover` passes with >80% coverage
- **Logic:** Bridge incident.AttributionEngine to trust.Ledger: when an incident is attributed, create TrustEvents (type=incident_attribution, agent_id, severity, attribution_weight, evidence_links) and append to the JSONL ledger. Replay the ledger to verify the trust score reflects the incident penalty. Incident → TrustEvent mapping function. Batch processing: multiple incidents → multiple events. Verify trust score decreases after incident attribution.
- **Result:** [x] IncidentBridge connecting AttributionEngine → JSONL ledger. ProcessResult writes dual events (attribution + penalty) per agent, updates in-memory score cache. ProcessIncident convenience method. ProcessBatch for multi-incident. Ledger replay verified deterministic. 37 new tests, 89.8% pkg/trust coverage (up from 86.8%). Full suite 23/23 packages pass.

## [x] Implement evidence verification layer (Tier 3) — pkg/review/
- **Priority:** medium
- **Spec:** specs/adversarial-review.md §Three-Layer Review Pipeline (Tier 3)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/review/verification.go (NEW), pkg/review/verification_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/review/... -count=1 -cover` passes with >80% coverage
- **Logic:** EvidenceVerificationLayer that takes the consensus findings from ReviewOrchestrator and verifies them: (1) run tests from model suggestions, (2) verify edge cases actually fail as claimed, (3) confirm fixes resolve issues. VerificationResult with per-finding status (verified/false_positive/unverifiable). Integration point: after ReviewOrchestrator.Review() completes, EvidenceVerifier.VerifyFindings() runs the claims.
- **Result:** [x] EvidenceVerifier with TestRunner interface, concurrent finding verification. Finding classification: testable (has test_run_id) → run test, mitigation present → verify structure, no evidence → unverifiable. Test failure = finding verified; test pass = false positive (feeds FPTracker). 29 new tests, 94.8% pkg/review coverage. Full suite 23/23 pass.

## [x] Implement adversarial agent dispatcher — pkg/review/
- **Priority:** medium
- **Spec:** specs/adversarial-review.md §Adversarial Agent Techniques
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/review/agents.go (NEW), pkg/review/agents_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/review/... -count=1 -cover` passes with >80% coverage
- **Logic:** AdversarialAgentDispatcher that launches specialized agents (@assumption-buster, @redteam, @chaos-engineer, @cost-auditor) based on change category. Each agent is a ProsecutorAgent with a specific mission (find what's wrong, not what's right). AgentTrigger rules (behavioral→assumption-buster, auth/crypto→redteam, resilience→chaos-engineer, all→cost-auditor). AgentResult with exploit paths found, assumptions challenged, fault injection results.
- **Result:** [x] AdversarialAgentDispatcher with trigger-based agent selection. ProsecutorAgent interface with Prosecute/Identity methods. 4 specialized agents (assumption-buster, redteam, chaos-engineer, cost-auditor) with DefaultTriggers mapping. Concurrent dispatch with DispatchReport aggregation (exploits, assumptions, fault results, cost estimates). StubAgent for testing. 38 new tests, 94.4% pkg/review coverage. Full suite 23/23 pass.

## [x] Implement drift detection for production verification — pkg/verify/
- **Priority:** medium
- **Spec:** specs/production-verification.md §Shadow Verification + §Behavior Contracts
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/verify/drift.go (NEW), pkg/verify/drift_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/verify/... -count=1 -cover` passes with >80% coverage
- **Logic:** DriftDetector compares shadow deployment metrics against baseline. Detect metric drift (success rate drop >2%, latency increase >10%, error type distribution shift). Configurable sensitivity thresholds per metric. Time-windowed comparison (rolling 5-min windows). DriftReport with per-metric delta, trend direction, and breach severity. Integration with existing ShadowDeployment and BehaviorContract.
- **Result:** [x] DriftDetector with rolling time-windowed MetricsSnapshot samples. Per-metric sensitivity thresholds (success_rate 2%, p99_latency 10%, p50 15%, errors 50%, memory 10%, new_error_types 0). Trend direction (stable/improving/degrading) with higher/lower_is_better hint. Breach severity (none/warning/critical) based on overshoot ratio. AssessDeployment integrates with ShadowDeployment. 38 new tests, 97.3% pkg/verify coverage (up from 97.2%). Full suite 23/23 pass.

## [x] Bridge marketplace trust score to trust engine — pkg/marketplace + pkg/trust
- **Priority:** high
- **Spec:** specs/agent-marketplace.md §Trust Scoring + specs/trust-model.md §Integration Points
- **Model:** direct write — Go packages, cross-package integration
- **Files:** pkg/marketplace/trust_bridge.go (NEW), pkg/marketplace/trust_bridge_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/marketplace/... -count=1 -cover` passes with >80% coverage
- **Logic:** Marketplace uses TrustScore int (0-100), trust engine uses TrustScore float64 (0.0-1.0). Build a TrustSync bridge that reads the JSONL trust ledger, computes the current score via ReplayToScore, converts to the 0-100 marketplace scale, and updates the agent profile. Periodic sync + on-demand query. Direction: trust engine is the source of truth, marketplace reads from it.
- **Result:** [x] TrustSync bridge with interval-based sync caching. SyncAgent (single agent), SyncAll (full registry), GetLiveScore (read-only source-of-truth query). ScoreToMarketplace/MarketplaceToScore conversion with rounding + clamping. 16 tests, trust_bridge functions 75-100% coverage. 97.1% total pkg/marketplace coverage (up from 96.8%). Full suite 23/23 pass.

## [x] Implement tier-gated permission expansion — pkg/identity + pkg/trust
- **Priority:** high
- **Spec:** specs/trust-model.md §Integration Points: "Forgejo permissions expand with trust tier"
- **Model:** direct write — Go packages, cross-package integration
- **Files:** pkg/identity/permissions.go (NEW), pkg/identity/permissions_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/identity/... -count=1 -cover` passes with >80% coverage
- **Logic:** PermissionExpansion maps trust tiers to Forgejo permission sets. Provisional: read-only + own branches. Observed: create branches + PRs. Trusted: merge own PRs + create repos. Veteran: admin + delete repos. When an agent's tier changes (via trust ledger replay), the identity system updates their Forgejo permissions accordingly. TierTransition event handler.
- **Result:** [x] PermissionExpansion with monotonic tier→permission mapping. PermissionSet (16 capability flags + cost cap + sandbox level). TierTransition with IsPromotion/IsDemotion. ComputeDelta/HandleTransition for tier change events. CanPerformAction action checker with shorthand aliases. 28 tests, 87.5% pkg/identity coverage. Full suite 23/23 pass.

## [x] Implement cost-tier enforcement at dispatch — pkg/dispatcher + pkg/estimate + pkg/trust
- **Priority:** medium
- **Spec:** specs/trust-model.md §Integration Points: "Cost caps enforced at job dispatch based on current tier" + specs/cost-estimator.md
- **Model:** direct write — Go packages, cross-package integration
- **Files:** pkg/dispatcher/cost_guard.go (NEW), pkg/dispatcher/cost_guard_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/dispatcher/... -count=1 -cover` passes with >80% coverage
- **Logic:** CostGuard runs before dispatching a work item. It queries the agent's trust tier, looks up the tier-specific cost cap (Provisional: $5/day, Observed: $25/day, Trusted: $100/day, Veteran: $500/day), calls pkg/estimate to pre-flight the token cost, and blocks/escalates based on the result. Returns APPROVED/BLOCKED/ESCALATED. Integrates with existing dispatcher.ExecuteLoop as a pre-dispatch check.
- **Result:** [x] CostGuard with Check (task desc → estimate → tier cap comparison) and CheckWithEstimate (pre-computed estimate). APPROVED/BLOCKED/ESCALATED decisions. 80% warn zone (approaching limit). Veteran unlimited cap. 18 tests, cost_guard functions 65-100% coverage. 91.2% pkg/dispatcher coverage. Full suite 23/23 pass.

## [x] Implement review depth scaling by trust tier — pkg/review + pkg/trust
- **Priority:** medium
- **Spec:** specs/trust-model.md §Integration Points: "Review depth and model count scale inversely with trust tier" + specs/adversarial-review.md §Model Formation Strategy
- **Model:** direct write — Go packages, cross-package integration
- **Files:** pkg/review/tier_scaling.go (NEW), pkg/review/tier_scaling_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/review/... -count=1 -cover` passes with >80% coverage
- **Logic:** TierReviewPolicy maps trust tiers to review formation requirements. Provisional: full 3-model adversarial + all prosecutor agents + 100% evidence verification. Observed: 2-model + prosecutor agents. Trusted: single-model + spot-check verification. Veteran: single-model review. The ReviewOrchestrator queries the agent's tier and adjusts the panel size, consensus threshold, and verification depth accordingly.
- **Result:** [x] TierScaling with TierReviewPolicy per tier. AdjustFormation (min of category × tier), AdjustConsensusThreshold, ShouldVerifyEvidence, ShouldDispatchProsecutors (cosmetic always skips, trusted+ only for contract). 24 tests, tier_scaling functions 75-100% coverage. 94.2% pkg/review coverage. Full suite 23/23 pass.

## [x] Implement veto protocol — 4-condition validation + frivolous veto tracker
- **Priority:** high
- **Spec:** specs/pr-negotiation.md §8 (Veto Protocol)
- **Model:** direct write — Go package, pure logic
- **Files:** pkg/negotiate/veto.go (NEW), pkg/negotiate/veto_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/negotiate/... -count=1 -cover` passes with >85% coverage
- **Logic:** ValidateVeto checks all 4 spec §8.1 conditions (trust≥70, spec section cited, test command, AC reference). VetoTracker tracks frivolous vetoes with 90-day rolling window. 3 frivolous vetoes → trust capped at 69 (loses veto power). VetoWeight returns 1.5× for trust≥90 agents. Body parsers extract spec refs, test commands, and AC references from veto body text.
- **Result:** [x] 30 tests, 97.3% pkg/negotiate coverage. Full suite 23/23 pass. Committed at `64ae24a`.

## [x] Implement escalation comment formatter — pkg/negotiate/
- **Priority:** medium
- **Spec:** specs/pr-negotiation.md §12.2 (Escalation Format)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/negotiate/escalation.go (NEW), pkg/negotiate/escalation_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/negotiate/... -count=1 -cover` passes with >85% coverage
- **Logic:** FormatEscalationComment renders the spec §12.2 escalation PR comment template: reason (timeout|budget_exhausted|chimera_unavailable), agent names + trust levels, rounds completed, deadlock status, debate log path, agent positions with summaries, recommended action. EscalationData struct with all fields. EscalationReason constants. Integration with Negotiator.Escalate — when escalated, generate the comment body.
- **Result:** [x] 18 tests, 100% coverage on escalation.go. FormatEscalationComment renders complete spec §12.2 markdown template. EscalationFromNegotiator extracts data from live Negotiator state. EscalationExitCode maps reasons to spec §14 codes. EscalationMessage formats exit messages. IsEscalatable validates state. 98.2% pkg/negotiate coverage (up from 97.3%). Full suite 24/24 pass.

## [x] Implement evidence bundle file store — pkg/review/
- **Priority:** medium
- **Spec:** specs/adversarial-review.md §Evidence Bundles — "stored in DuckBrain and linked from the merge commit"
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/review/store.go (NEW), pkg/review/store_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/review/... -count=1 -cover` passes with >85% coverage
- **Logic:** EvidenceStore persists evidence bundles to disk as JSON files. Store(bundle) writes to ~/.helix/evidence/<review_id>.json. Load(reviewID) reads and verifies signatures. ListByAgent(agentID) returns all bundles for an agent. ListByPR(prURL) returns bundles for a PR. VerifyIntegrity re-checks all signatures on load. LinkFromMerge returns the path to embed in merge commit message.
- **Result:** [x] 30 tests, 92.5% pkg/review coverage. EvidenceStore with Store/Load/LoadRaw/VerifyIntegrity/VerifyAllIntegrity/ListAll/ListByAgent/ListByPR/Search/Delete/Count/LinkFromMerge. StoreEntry wrapper with agent_id + stored_at metadata. Round-trip signature integrity verified. Full suite 24/24 pass.

## [x] Implement trust snapshot query API — pkg/trust/
- **Priority:** medium
- **Spec:** specs/trust-model.md §The Trust Ledger — "replay the ledger to verify any agent's current score"
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/trust/snapshot.go (NEW), pkg/trust/snapshot_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/trust/... -count=1 -cover` passes with >85% coverage
- **Logic:** TrustSnapshot captures a point-in-time view of an agent's trust state: current score, tier, score breakdown by dimension, recent events (last 30 days), tier history. GetSnapshot(agentID) replays the ledger and returns the full snapshot. GetScoreBreakdown returns per-dimension scores. GetTierHistory returns promotion/demotion events. ScoreTrend returns the score change over N days.
- **Result:** [x] 25 tests, 91.6% pkg/trust coverage. GetSnapshot replays ledger → full TrustSnapshot (score, tier, breakdown, recent events, tier history, score trend). GetScoreBreakdown with 6 dimensions (weight × estimated score = contribution). GetTierHistory extracts promotion/demotion transitions. ScoreTrendOver with up/down/stable direction detection. GetRecentEvents for N-day window queries. Full suite 24/24 pass.

## [x] Implement debate round validator — pkg/negotiate/
- **Priority:** high
- **Spec:** specs/pr-negotiation.md §7.2 (Debate Round Format) + §7.5 (Strike System)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/negotiate/debate_validator.go (NEW), pkg/negotiate/debate_validator_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/negotiate/... -count=1 -cover` passes with >85% coverage
- **Logic:** DebateValidator validates structured debate comments per spec §7.2. ValidateEvidence checks: minimum 2 evidence items per comment, at least 1 cites a spec file or test output, at least 1 references the other agent's argument. "I disagree" without evidence → comment rejected, agent gets strike. StrikeTracker accumulates strikes per agent: posting without evidence → 1 strike, missing a round → 1 strike + auto-concede on 2nd miss, 3 strikes → auto-concede. ParseRoundComment extracts position, evidence items, counter-argument, concession conditions from a structured comment body.
- **Result:** [x] 45 tests, 98.3% pkg/negotiate coverage (up from 97.3%). Full suite 24/24 pass. ParseRoundComment parses §7.2 markdown format (round number, agent name, trust level, position, evidence items by type, counter-argument with @mention extraction, concession conditions). ValidateEvidence enforces all 3 §7.2 requirements (min 2 items, ≥1 spec/test, ≥1 counter-arg ref). StrikeTracker with auto-concede on 3 strikes or 2 round misses, thread-safe with sync.Mutex, full strike audit log.

## [x] Implement canary promotion decision engine — pkg/verify/
- **Priority:** medium
- **Spec:** specs/production-verification.md §Canary Promotion
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/verify/canary.go (NEW), pkg/verify/canary_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/verify/... -count=1 -cover` passes with >85% coverage
- **Logic:** CanaryPromoter evaluates whether a shadow deployment is ready for canary promotion. EvaluatePromotion(shadowResult) checks: behavior contract passed all assertions, drift detector shows no degradation, success rate within threshold of baseline, no new error types introduced, minimum observation window elapsed. Returns PromotionDecision (READY/NOT_READY/NEEDS_MORE_DATA) with per-check results. ComputeCanaryPercentage decides traffic ramp: Provisional 1%, Observed 5%, Trusted 10%, Veteran 25%. AutoRampSchedule generates gradual ramp-up schedule with observation gaps between increments.
- **Result:** [x] 45 tests, 97.7% pkg/verify coverage (up from 97.3%). Full suite 24/24 pass. CanaryPromoter with 5 readiness checks (contract, drift, success rate, new errors, observation window). READY/NOT_READY/NEEDS_MORE_DATA decision logic with nil-input skip semantics. ComputeCanaryPercentage with 4 trust tiers. AutoRampSchedule generates tier-specific ramp steps from CanarySchedule. DriftAssessment helpers (HasCriticalBreach, DriftCount).

## [x] Implement prompt attestation validator — pkg/prompt/
- **Priority:** medium
- **Spec:** specs/prompt-registry.md §Attestation
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/prompt/attestation_validator.go (NEW), pkg/prompt/attestation_validator_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/prompt/... -count=1 -cover` passes with >85% coverage
- **Logic:** AttestationValidator verifies that every commit in a PR has a valid prompt attestation link. ValidateCommitMessage checks the `Prompt: prompts/<name>/v<N>.md` trailer format. VerifyPromptExists confirms the referenced prompt file exists in the registry. VerifyHashMatch confirms the prompt file's hash matches the attested hash. ValidatePR scans all commits in a PR and returns AttestationReport with per-commit status (VALID/MISSING/MALFORMED/HASH_MISMATCH/FILE_NOT_FOUND). Integrate with merge gate: no attestation → merge blocked.
- **Result:** [x] 38 tests, 92.5% pkg/prompt coverage. Full suite 24/24 pass. AttestationValidator supports both path format (prompts/<name>/v<N>.md) and hash format (sha256:<hex>). Per-commit validation with 5 status types. AttestationReport with AllValid/HasInvalid/ShouldBlockMerge/Summary. Tamper detection integration test with registry Register+Lookup. Convenience functions (HasPromptTrailer, HasValidPromptTrailer, ExtractPromptRef, IsPathFormat, IsHashFormat).

## [x] Implement negotiation timeout watcher — pkg/negotiate/
- **Priority:** high
- **Spec:** specs/pr-negotiation.md §12.1 (Timeout Rules) + §7.4 (Deadlock Detection)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/negotiate/timeout.go (NEW), pkg/negotiate/timeout_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/negotiate/... -count=1 -cover` passes with >85% coverage
- **Logic:** TimeoutWatcher enforces the per-round (5 min) and global (30 min) timeouts from spec §12.1. StartRound begins a per-round timer; CheckRoundTimeout returns true when expired (→ agent who didn't post gets strike per §7.5). StartNegotiation begins the global timer; CheckGlobalTimeout returns true when the full negotiation exceeds 30 min (→ escalate to human). Context-aware: cancel via context.Context. OnGlobalTimeout returns a spec-compliant escalation event. OnRoundTimeout returns a strike event with agent + round number.
- **Result:** [x] 52 tests, 98.0% pkg/negotiate coverage. Full suite 24/24 pass. TimeoutWatcher enforces all 3 spec §12.1 timeouts (round 5m, global 30m, Chimera 5m with 1 retry). OnRoundTimeout auto-records strikes for missing agents (integrates with StrikeTracker). OnChimeraTimeout handles retry-then-escalate flow. Status() snapshot for diagnostics. Context-aware cancellation. ValidateTimeoutConfig for config validation.

## [x] Implement Chimera arbiter input assembly — pkg/negotiate/
- **Priority:** high
- **Spec:** specs/pr-negotiation.md §9.2 (Input Assembly)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/negotiate/input_assembly.go (NEW), pkg/negotiate/input_assembly_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/negotiate/... -count=1 -cover` passes with >85% coverage
- **Logic:** AssembleArbiterInput builds the prompt sent to Chimera's arbiter formation per spec §9.2. Input sections: PR Context (title, description, diff truncated to 50K chars, spec files concatenated), Agent Reviews (both agent names, trust levels, verdicts, bodies), Debate Transcript (all rounds), Question (APPROVE or REJECT). TruncateDiff clips diffs to 50K chars with a truncation notice. ConcatSpecFiles merges referenced spec files. AssembleArbiterInput takes a Negotiation + debate rounds + PR context and returns the formatted prompt string.
- **Result:** [x] 26 tests, 98.1% pkg/negotiate coverage. Full suite 24/24 pass. AssembleArbiterInput builds spec §9.2 prompt with all 4 sections (PR Context, Agent Reviews, Debate Transcript, Question). TruncateDiff with percentage notice. ConcatSpecFiles with labeled file paths. AssembleFromNegotiator convenience wrapper. EstimatePromptSize for pre-flight budget checks.

## [x] Implement negotiation trust adjustment engine — pkg/negotiate/
- **Priority:** medium
- **Spec:** specs/pr-negotiation.md §10.2 (Trust Adjustments from Negotiation)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/negotiate/trust_adjustment.go (NEW), pkg/negotiate/trust_adjustment_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/negotiate/... -count=1 -cover` passes with >85% coverage
- **Logic:** TrustAdjustmentEngine computes trust deltas for all negotiation events per spec §10.2 table: concession with evidence (+1), wins tie-break (+2), loses with evidence (0), loses without evidence (-5), frivolous veto (-5), missed round (-2), 3 strikes (-10 + auto-concede). TrustDelta struct with Agent, Delta, Reason, Event type. ApplyTrustDelta clamps to 0-100 range (spec §10.3 floor/ceiling). AdjustForNegotiationOutcome computes all deltas for both agents after a negotiation completes. RecordTrustHistory stores the adjustment events for audit.
- **Result:** [x] 38 tests, 98.2% pkg/negotiate coverage. Full suite 24/24 pass. All 7 spec §10.2 event types with exact deltas. AdjustForNegotiationOutcome computes all deltas from a NegotiationOutcome struct. ApplyAdjustments batch-applies with TrustHistoryEntry audit trail. ApplyTrustDelta clamps to [0,100] per spec §10.3. TrustAdjustmentSummary for human-readable output. EventDescription for each type.

## [x] Implement negotiation dry-run simulator — pkg/negotiate/
- **Priority:** medium
- **Spec:** specs/pr-negotiation.md §2 (Dry-run mode) + §14 (Exit code 10)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/negotiate/dry_run.go (NEW), pkg/negotiate/dry_run_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/negotiate/... -count=1 -cover` passes with >85% coverage
- **Logic:** DryRunSimulator runs the full negotiation protocol without making Forgejo or Chimera calls. Simulates all 3 debate rounds with stub agents, produces the same DebateEvent JSONL transcript as a real negotiation, returns a DryRunReport with rounds simulated, would-be-resolution, estimated cost, and exit code 10 (DRY_RUN). Used for previewing debate flow.
- **Result:** [x] 22 tests, 98.3% pkg/negotiate coverage. Full suite pass. DryRunSimulator with Simulate (full 3-round conflict → deadlock → Chimera) and SimulateConcession (agent concedes in round N). Full lifecycle event ordering verified. FormatDryRunReport for CLI output. Exit code 10 (spec §14).

## [x] Implement negotiation error taxonomy — pkg/negotiate/
- **Priority:** medium
- **Spec:** specs/pr-negotiation.md §14 (Error Taxonomy and Exit Codes)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/negotiate/errors.go (NEW), pkg/negotiate/errors_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/negotiate/... -count=1 -cover` passes with >85% coverage
- **Logic:** NegotiationError type with Code, Message, Detail. Map all 7 exit codes (0=resolved, 1=evidence_required, 2=chimera_unavailable, 3=budget_exhausted, 4=timeout, 5=invalid_state, 10=dry_run) to error constructors. ExitCodeFromError extracts the code from an error. FormatExitMessage renders the spec §14 message format. IsTerminalExit checks if the code means negotiation is done.
- **Result:** [x] 25 tests, 98.2% pkg/negotiate coverage. All 7 spec §14 exit codes with exact values. 7 typed constructors matching spec §14 message formats. IsTerminal/IsRetryable for flow control. FormatExitMessage for CLI output. ExitCodeFromError for error-to-code extraction. errors.As compatible.

## [x] Implement trust recovery tracking — pkg/trust/
- **Priority:** medium
- **Spec:** specs/trust-model.md §Anti-Patterns (trust must be earnable, not permanent)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/trust/recovery.go (NEW), pkg/trust/recovery_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/trust/... -count=1 -cover` passes with >85% coverage
- **Logic:** RecoveryTracker monitors agents who have dropped tiers or received incident penalties. Tracks recovery progress: consecutive clean merges since last incident, days without incident, trust score trend. IsRecovering returns true if an agent has had incidents but is now on an upward trend. RecoveryProgress returns a percentage (0-100) of how close the agent is to recovering to their pre-incident trust level. Uses the existing trust ledger for event history.
- **Result:** [x] 31 tests, 91.6% pkg/trust coverage. RecoverySnapshot with IsRecovering, RecoveryProgress (0-100), PreIncidentScore, ConsecutiveCleanMerges, DaysSinceLastIncident, EstimatedDaysToRecover. Post-incident-only trend computation (incident drop excluded). GetRecoveryBatch for multi-agent. Configurable RecoveryConfig. 6 health labels (healthy/recovered/recovering-strong/recovering/recovering-slow/recovering-early/at-risk). Full suite 24/24 pass.

## [x] Implement evidence bundle chain-of-custody — pkg/review/
- **Priority:** medium
- **Spec:** specs/adversarial-review.md §Evidence Bundles (signatures + integrity)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/review/custody.go (NEW), pkg/review/custody_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/review/... -count=1 -cover` passes with >85% coverage
- **Logic:** ChainOfCustody tracks the full lifecycle of an evidence bundle: creation timestamp, signing model IDs, verification history, mutation log. Any modification to the bundle after creation is tracked as a custody event. VerifyChain checks that no tampering occurred since the last valid signature. CustodyReport summarizes the chain for audit display. Integrates with existing EvidenceStore for persistence.
- **Result:** [x] 27 tests, 92.9% pkg/review coverage. ChainOfCustody with 7 event types (created/signed/verified/modified/finding_added/consensus_set/re_signed). VerifyChain detects: unsigned modifications (tampering), verification failures, missing signatures. Re-signing after modification clears the tamper flag. CustodyReport with IsValid/ShouldBlockMerge/FormatReport. CustodyStore wraps EvidenceStore for init/track/verify. Full suite 24/24 pass.

## [x] Implement steady-state surveillance aggregator — pkg/verify/
- **Priority:** medium
- **Spec:** specs/production-verification.md §Phase 3 — Steady-State Surveillance (72h+)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/verify/surveillance.go (NEW), pkg/verify/surveillance_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/verify/... -count=1 -cover` passes with >85% coverage
- **Logic:** SteadyStateAggregator runs continuous behavior contract checks on deployed agents. Aggregates metrics from multiple sources (success rate, latency, error types), evaluates contracts periodically, and emits surveillance events. LongRunningMonitor detects gradual degradation over 7-day windows. AlertEscalation triggers when sustained drift exceeds thresholds. Integrates with existing DriftDetector, BehaviorContract, and NotificationDispatcher.
- **Result:** [x] 68 tests, 94.8% pkg/verify coverage. SteadyStateAggregator with multi-agent surveillance. LongRunningMonitor with daily summary aggregation and 4-metric degradation analysis (success rate, P99 latency, error rate, memory). AlertEscalation with 4 levels (none→notify→investigate→rollback) and sustained drift tracking. Full lifecycle: healthy→breach→recovery. NotificationDispatcher integration. Full suite 24/24 pass.

## [x] Implement marketplace search ranking algorithm — pkg/marketplace/
- **Priority:** medium
- **Spec:** specs/agent-marketplace.md §Discovery (search + ranking)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/marketplace/search.go (NEW), pkg/marketplace/search_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/marketplace/... -count=1 -cover` passes with >85% coverage
- **Logic:** SearchRanker computes a relevance score for each agent listing given a SearchQuery. Ranking factors: trust score (primary sort dimension per spec), capability match (keyword + tag overlap), performance metrics (merge success rate, avg review time), human ratings, cost-effectiveness. Return ranked AgentListing slice. Supports filtering by trust tier minimum, max cost, and capability tags.
- **Result:** [x] 52 tests, 96.3% pkg/marketplace coverage. SearchRanker with 5-factor composite scoring (trust 35%, capability 25%, performance 15%, rating 15%, cost 10%). Filter by capabilities (ALL must match), min trust, max cost. TextSearch for keyword/name/capability fuzzy matching. Custom weight override via WithSearchWeights. Full suite 24/24 pass.

## [x] Implement Forgejo PR status integration — pkg/forgejo/
- **Priority:** medium
- **Spec:** specs/cross-component-wiring.md §2.1 (Forgejo → Chimera PR review) + specs/deployment.md §5
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/forgejo/pr_status.go (NEW), pkg/forgejo/pr_status_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/forgejo/... -count=1 -cover` passes with >85% coverage
- **Logic:** PRStatusManager posts review verdicts and deployment status as Forgejo PR comments and commit statuses. PostReviewComment renders Chimera verdict as structured markdown comment. PostCommitStatus sets CI-style status check (success/failure/pending) on commits. PostDeploymentStatus shows canary/shadow progress inline. ParsePRReviews reads existing review comments. Integrates with existing ForgejoClient for REST API calls.
- **Result:** [x] 60 tests, 94.4% pkg/forgejo coverage. PRStatusManager with PostReviewComment (Chimera verdict → markdown), PostCommitStatus (CI-style checks), PostReviewStatus (verdict → commit state), PostDeploymentStatus (canary/shadow → pending/success/error/warning), PostDeploymentComment (progress bar + breach display). ParsePRReviews extracts structured data from existing Helix review comments. httptest mock servers for all API calls. Full suite 24/24 pass.

## [x] Implement negotiation transcript replay + verdict file writer — pkg/negotiate/
- **Priority:** high
- **Spec:** specs/pr-negotiation.md §13 (Filesystem Layout)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/negotiate/transcript.go (NEW), pkg/negotiate/transcript_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/negotiate/... -count=1 -cover` passes with >85% coverage
- **Logic:** ReplayTranscript reads JSONL debate transcripts and returns a TranscriptSummary (agents, rounds, outcome, deadlock/chimera/escalation flags). WriteVerdictFile renders spec §13 `*-verdict.md` markdown summary. WriteStateFile/LoadStateFile manage the `state.json` active negotiation recovery file.
- **Result:** [x] 18 tests. ReplayTranscript handles: empty, full debate, concession, escalation, blank lines, malformed JSON, agent collection, large buffer. VerdictFile: file creation, filename convention, no-chimera case, nested dir. StateFile: write/load round-trip, not-found. Full suite 24/24 pass.

## [x] Implement dispatcher stale lock recovery — PID liveness check
- **Priority:** high
- **Spec:** specs/dispatcher.md — "acquireLock prevents concurrent pipeline runs"
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/dispatcher/loop.go (extend), pkg/dispatcher/loop_test.go (extend)
- **AC:** `go build ./... && go test ./pkg/dispatcher/... -count=1` passes
- **Logic:** Replace the "fail fast" lock behavior with PID liveness checking. When a lock file exists, parse the PID, check if the process is alive (signal 0). Dead PID → stale lock, safe to overwrite. Live PID → block. parseLockPID extracts PID from lock file format. isProcessAlive uses syscall.Signal(0) for non-destructive check. Tests updated: live lock uses os.Getpid(), stale lock test added.
- **Result:** [x] 10 new tests (parseLockPID 8 cases, isProcessAlive 3 scenarios, stale/live acquireLock). Existing lock-held tests updated to use current PID. Full suite 24/24 pass.

## [x] Implement marketplace metrics collector (Observability) — pkg/marketplace/
- **Priority:** medium
- **Spec:** specs/agent-marketplace.md §14 (Observability)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/marketplace/metrics.go (NEW), pkg/marketplace/metrics_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/marketplace/... -count=1 -cover` passes with >85% coverage
- **Logic:** MetricsCollector implementing all 5 Prometheus metrics from spec §14: helix_marketplace_agents_total{status} (gauge), helix_marketplace_trust_score{agent} (gauge), helix_marketplace_queries_total{filter} (counter), helix_marketplace_ratings_total (counter), helix_marketplace_assignments_total{agent} (counter). Collect() emits Prometheus text exposition format with HELP/TYPE headers. Thread-safe with sync.RWMutex. AgentsByStatus and TrustScoreGauges derive gauges from registry state.
- **Result:** [x] 20 tests, 94.2% pkg/marketplace coverage (up from 93.6%). All 5 spec §14 metrics implemented. Prometheus text format with HELP/TYPE headers, deterministic ordering, thread-safe. Full suite 24/24 pass.

## [x] Implement negotiation history query + audit trail — pkg/negotiate/
- **Priority:** medium
- **Spec:** specs/pr-negotiation.md §13 (Filesystem Layout) — audit trail query
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/negotiate/history.go (NEW), pkg/negotiate/history_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/negotiate/... -count=1 -cover` passes with >85% coverage
- **Logic:** QueryHistory scans the negotiations directory for JSONL debate transcripts, replays each via existing ReplayTranscript, and returns matching HistoryEntry items. Filters: by agent name, PR number, outcome, time range (Since/Until), and result limit. Results sorted by StartedAt descending (most recent first). FormatHistory renders a human-readable table for CLI output. Skips non-JSONL files (verdict.md, state.json) and malformed transcripts.
- **Result:** [x] 17 tests, 97.3% pkg/negotiate coverage. Filters for agent/PR/outcome/time-range all verified. Sorted descending. Malformed transcripts skipped gracefully. Full suite 24/24 pass.

## [x] Implement budget period reset manager — pkg/estimate/
- **Priority:** medium
- **Spec:** specs/cost-estimator.md §8.3 (Budget Period Management)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/estimate/period.go (NEW), pkg/estimate/period_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/estimate/... -count=1 -cover` passes with >85% coverage
- **Logic:** PeriodManager for weekly budget period management per spec §8.3. Period: Sunday 00:00 UTC to Saturday 23:59:59 UTC. ResetBudgets sets budget_used_usd = 0 for all agents. NextReset returns time until next Sunday 00:00 UTC. IsInPeriod checks if a timestamp falls in the current period. CanRollover always returns false in v1 (spec: no rollover). ResetAgent resets a single agent's budget. ResetAgentList resets multiple agents in batch. ShouldResetAlert returns true when within 1 hour of reset (cron trigger window).
- **Result:** [x] 25 tests, 92.9% pkg/estimate coverage. Period boundary tests (Sunday reset, Saturday last second, non-UTC time). Alert window edge cases. Custom reset hour support. ResetBudgets non-mutating. Full suite 24/24 pass.

## [x] Implement estimation drift tracker — pkg/estimate/
- **Priority:** medium
- **Spec:** specs/cost-estimator.md §8.2 (Post-Execution Reconciliation) + §9.2 (Reconciliation Strategy)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/estimate/drift.go (NEW), pkg/estimate/drift_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/estimate/... -count=1 -cover` passes with >85% coverage
- **Logic:** DriftTracker logs estimation drift per spec §8.2 step 4. RecordDrift(agent, estimated, actual) stores an entry with timestamp. DriftReport returns {agent, count, avg_drift_pct, max_drift, recent_entries}. IsOverThreshold returns true when avg drift > 10% per spec §9.2. ExportDriftLog writes all entries as JSONL. Integrates with existing Calibrator — feeds calibration records weekly.
- **Result:** [x] 29 tests, 94.1% pkg/estimate coverage (up from 92.9%). DriftTracker with RecordDrift/RecordDriftEntry, DriftReport (avg/max/min/recent entries/period), IsOverThreshold (10% per spec §9.2), Count, Clear, ExportDriftLog/ImportDriftLog (JSONL round-trip), FeedCalibrator (drift→CalibrationRecord bridge with cache ratio inference), AgentsWithDrift, FormatDriftReport. Thread-safe with sync.RWMutex. Concurrent test (10 writers × 10 records). Full suite 24/24 pass.

## [x] Implement marketplace agent auto-deprecation time-window enforcement — pkg/marketplace/
- **Priority:** medium
- **Spec:** specs/agent-marketplace.md §10.2 (Auto-Deprecation Rules)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/marketplace/lifecycle.go (extend), pkg/marketplace/lifecycle_extended_test.go (NEW), pkg/marketplace/types.go (add History field)
- **AC:** `go build ./... && go test ./pkg/marketplace/... -count=1 -cover` passes with >85% coverage
- **Logic:** Replace stub proxies in AutoDeprecationRules with spec-compliant time-window checks. Rule 1: trust < 20 for 30 consecutive days (track trust_dropped_at timestamp). Rule 2: no completed tasks in 90 days (track last_task_completed_at). Rule 3: budget exhausted for 14 consecutive days (track budget_exhausted_at). Add AgentHistory struct with these timestamps to Agent. ShouldAutoDeprecate evaluates a single agent against all 3 rules with proper time windows. Reactivate auto-check per §10.3: trust > 20 for 7 days → auto-reactivation candidate.
- **Result:** [x] 54 tests, 94.5% pkg/marketplace coverage. AgentHistory with 4 lifecycle timestamps. ShouldAutoDeprecate with all 3 spec §10.2 time-window rules + DeprecationDecision/Reason. ShouldReactivate for spec §10.3 (trust recovery 7d + budget replenishment). AutoReactivationRules batch. UpdateTrustHistory/MarkTaskCompleted/UpdateBudgetStatus for daily cron integration. parseTimestamp/daysSince/isBudgetExhausted helpers. Existing lifecycle tests updated to new time-window semantics. Full suite 24/24 pass.

## [x] Implement prompt normalization pipeline for fenced code blocks — pkg/prompt/
- **Priority:** medium
- **Spec:** specs/prompt-registry-v2.md §8.2-§8.3 (Normalization + Fenced-Code-Block Exemption)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/prompt/normalize.go (NEW), pkg/prompt/normalize_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/prompt/... -count=1 -cover` passes with >85% coverage
- **Logic:** Standalone normalization pipeline per spec §8.2 steps 1-5: (1) normalize line endings CRLF/CR→LF, (2) collapse runs of spaces/tabs within a line to single space — suppressed inside fenced code blocks (``` or ~~~), (3) strip trailing whitespace per line, (4) ensure exactly one trailing newline at EOF, (5) preserve leading whitespace. The fence-exempt normalizer tracks fence state line-by-line. An unclosed fence is treated as "inside" until EOF. YAML frontmatter (leading `---`...`---`) is stripped before normalization. Export NormalizeForHash(raw string) string as a reusable function the existing hasher.go can call.
- **Result:** [x] 55 tests, 92.9% pkg/prompt coverage. NormalizeForHash implements all 5 spec §8.2 steps. collapseSpacesAndTabs collapses both spaces AND tabs (step 2) while preserving leading whitespace (step 5). Fenced code block exemption (``` and ~~~) with unclosed-fence-until-EOF handling. YAML frontmatter stripping. Idempotent, deterministic, content-equivalence verified. Full suite 24/24 pass.

## [x] Implement cost estimate reconciliation pipeline — pkg/estimate/
- **Priority:** medium
- **Spec:** specs/cost-estimator.md §8.2 (Post-Execution Reconciliation) steps 1-5
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/estimate/pipeline.go (NEW), pkg/estimate/pipeline_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/estimate/... -count=1 -cover` passes with >85% coverage
- **Logic:** ReconcilePipeline chains the reconciliation steps per spec §8.2: (1) receive GitReins LLMUsage from evaluator, (2) compute actual cost via existing ActualCost(), (3) update budget_used in BudgetInfo, (4) log drift via existing DriftTracker, (5) feed DriftTracker into Calibrator for weekly recalibration. ReconciliationResult with estimated, actual, drift_pct, budget_remaining_after. ReconcileAgent convenience method that takes agent BudgetInfo + Usage + estimated CostEstimate and returns full ReconciliationResult. This wires together the existing reconciliation.go, drift.go, calibrator.go, and budget.go into a single pipeline.
- **Result:** [x] 18 tests, 94.4% pkg/estimate coverage (up from 94.1%). ReconcilePipeline chains all 5 spec §8.2 steps. Non-mutating (returns updated BudgetInfo copy). Nil-safe for tracker/calibrator. ReconcileAgent convenience wrapper. FormatReconciliation for CLI output. Full integration test (3 reconciliations → tracker + calibrator fed). Full suite 24/24 pass.

## [x] Implement review consensus report formatter — pkg/review/
- **Priority:** low
- **Spec:** specs/adversarial-review.md §Evidence Bundles (consensus display)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/review/consensus_report.go (NEW), pkg/review/consensus_report_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/review/... -count=1 -cover` passes with >85% coverage
- **Logic:** ConsensusReport renders the ReviewOrchestrator results as a structured markdown report for Forgejo PR comments. Sections: header (PR URL, review ID, timestamp), formation summary (models + providers used, diversity score), findings table (per-finding: model, severity, type, file:line, description, evidence), consensus block (per-model verdicts + resolution: PASS/WARN/BLOCK/FLAG), bias-stripped commit hash, original commit hash, evidence bundle link. FormatConsensusReport(evidence EvidenceBundle) string. RenderFindingsTable([]Finding) string. RenderConsensusBlock(Consensus) string.
- **Result:** [x] 22 tests, 93.5% pkg/review coverage. FormatConsensusReport renders structured markdown with all sections. RenderFindingsTable with empty/single/multiple/no-line cases. RenderConsensusBlock with all verdict types + resolutions. formatVerdict/formatResolution with emoji labels. shortSHA display helper. Full suite 24/24 pass.

## [x] Implement PR lifecycle coordinator — pkg/coordinator/
- **Priority:** high
- **Spec:** specs/cross-component-wiring.md (component discovery + interaction)
- **Model:** direct write — Go package, composes existing components
- **Files:** pkg/coordinator/lifecycle.go (NEW), pkg/coordinator/lifecycle_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/coordinator/... -count=1 -cover` passes with >80% coverage
- **Logic:** PRLifecycleCoordinator orchestrates the full PR lifecycle: PR opened → cost estimate (pkg/estimate) → adversarial review (pkg/review) → PR negotiation if contested (pkg/negotiate) → merge gate validation (pkg/mergegate) → shadow deployment if approved (pkg/verify) → steady-state surveillance (pkg/verify). Coordinator holds references to each subsystem and calls them in sequence. Returns PRLifecycleResult with per-stage status. Handles failures gracefully (each stage can fail independently without crashing the pipeline).
- **Result:** [x] 57 tests, 89.6% coverage. 6-stage lifecycle pipeline: cost estimate, adversarial review, negotiation (contested PRs), merge gate, shadow deploy, steady-state surveillance. PRLifecycleCoordinator with WithStages() for selective execution. LifecycleResult with StageByName/HasStage/AllPassed/HasFailure/Summary. Short-circuit on failure (REJECTED) or escalation (ESCALATED). Full suite 25/25 pass. Lint clean.

## [x] Implement trust audit runner — pkg/trust/
- **Priority:** medium
- **Spec:** specs/trust-model.md §The Trust Ledger — "replay the ledger to verify any agent's current score"
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/trust/audit.go (NEW), pkg/trust/audit_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/trust/... -count=1 -cover` passes with >85% coverage
- **Logic:** TrustAuditRunner performs a full audit of the trust system: (1) replay all JSONL ledger entries for every agent, (2) verify each agent's computed score matches their stored score, (3) detect anomalies (score drift, missing events, corrupted entries), (4) generate an AuditReport with per-agent findings (PASS/FAIL/ANOMALY), (5) flag agents whose tier doesn't match their score. Batch processing for all agents in the ledger. Used by a periodic cron to catch ledger corruption or stale caches.
- **Result:** [x] 45 tests, 91.2% coverage. TrustAuditRunner with 6 anomaly types (score_drift, tier_mismatch, backwards_score, no_activity, corrupted_entry, missing_events). AuditReport with per-agent findings, summary, FormatReport. Configurable tolerance and inactivity threshold. Full suite 25/25 pass. Lint clean.

## [x] Implement Forgejo webhook event handler — pkg/forgejo/
- **Priority:** medium
- **Spec:** specs/cross-component-wiring.md §2 (Forgejo as event source)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/forgejo/webhook.go (NEW), pkg/forgejo/webhook_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/forgejo/... -count=1 -cover` passes with >85% coverage
- **Logic:** WebhookHandler receives Forgejo webhook events (PR opened, PR updated, push, review submitted) and dispatches them to the appropriate handler. ParseWebhook extracts event type + payload. HandlePROpened triggers the review pipeline. HandleReviewSubmitted checks consensus. Each handler returns a WebhookResult (processed/skipped/error). HMAC signature verification for webhook authenticity. Event type routing table.
- **Result:** [x] 44 tests, 95.7% coverage. WebhookHandler with HMAC-SHA256 signature verification (Forgejo + Gitea header support). EventHandler interface with 5 callbacks. ParsePRInfo/ParsePushInfo/ParseReviewInfo for structured payload extraction. Action-based dispatch (opened/reopened→OnPROpened, closed→OnPRClosed, other→OnPRUpdated). NoOpHandler default. Full suite 25/25 pass. Lint clean.

## [x] Implement platform health aggregation dashboard — pkg/health/
- **Priority:** medium
- **Spec:** specs/cross-component-wiring.md §8 (Health Checks) + specs/deployment.md §4.3 (Fail Fast)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/health/aggregator.go (NEW), pkg/health/aggregator_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/health/... -count=1 -cover` passes with >80% coverage
- **Logic:** PlatformHealthAggregator collects health status from all Helix subsystems (trust, review, negotiate, verify, marketplace, estimate, sandbox) and produces a unified dashboard report. Each subsystem reports its own health status (healthy/degraded/down) with optional metrics. Aggregator runs periodic checks, caches results with TTL, and exposes a JSON dashboard endpoint. Includes degradation detection: if any critical subsystem is down, the entire platform is marked degraded. Used by CLI `helix status` to show platform health at a glance.
- **Result:** [x] 55 tests, 99.0% pkg/health coverage. PlatformHealthAggregator with SubsystemHealth interface (each subsystem implements HealthCheck). Concurrent health checks with TTL-based caching (15s default). DashboardReport with overall state (healthy/degraded/down) computed from critical/non-critical subsystem states. FormatDashboard for CLI output. ServiceHealthAdapter bridges existing Checker-based checks. Full suite 25/25 pass. Lint clean.

## [x] Implement sandbox resource usage tracker — pkg/sandbox/
- **Priority:** medium
- **Spec:** specs/sandbox.md §6 (Resource Limits) + §7 (Five Isolation Layers)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/sandbox/usage.go (NEW), pkg/sandbox/usage_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/sandbox/... -count=1 -cover` passes with >80% coverage
- **Logic:** ResourceUsageTracker monitors sandboxed agent sessions: peak memory usage (from cgroup memory.events), CPU time consumed, wall-clock duration, network access attempts, filesystem writes count. UsageReport with per-session metrics. SessionSummary aggregates across all sessions for an agent. EnforceResourceLimits checks if a session exceeded its configured memory/time limits. Integration with existing CgroupV2 for reading memory.events and cpu.stat.
- **Result:** [x] 47 tests, 93.8% pkg/sandbox coverage. ResourceUsageTracker with StartSession/EndSession/Sample lifecycle. Reads memory.current, cpu.stat (usage_usec), memory.events (oom count) from cgroup v2. Peak memory tracking (monotonic). Network/Fs write counters. EnforceResourceLimits for memory + time. SummarizeAgent for per-agent aggregation. Fake cgroup filesystem in tests. Full suite 25/25 pass. Lint clean.

## [x] Implement negotiation consensus calculator — pkg/negotiate/
- **Priority:** medium
- **Spec:** specs/pr-negotiation.md §11 (Consensus Rules) + §10.1 (Weighted Consensus)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/negotiate/consensus.go (NEW), pkg/negotiate/consensus_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/negotiate/... -count=1 -cover` passes with >85% coverage
- **Logic:** ConsensusCalculator computes the final verdict from multiple review signals. Weighted consensus per spec §10.1: each reviewer's trust level determines their vote weight (trust 90+ = 1.5×, trust 70+ = 1.0×, trust <70 = 0.5×). Required quorum per change category (contract = 3/3, behavioral = 2/2, cosmetic = 1/1). Override detection: a trust-90+ reviewer can override a single dissent from a trust-<70 reviewer. ComputeConsensus returns ConsensusResult with per-reviewer weights, total weighted score, and final verdict.
- **Result:** [x] 42 tests, 97.4% pkg/negotiate coverage. ConsensusCalculator with ComputeWeight (spec §10.1: 90+→1.5×, 70+→1.0×, <70→0.5×), RequiredQuorum (contract 3, behavioral 2, resilience/cosmetic 1), CheckOverride (trust-90+ overrides trust-<70 dissent unless a veto-capable reviewer also dissents), ComputeConsensus (weighted approve/reject, quorum check, tie→reject safety), FormatConsensus for audit logs. Full suite 25/25 pass. Lint clean.

## [x] Implement budget approval gate engine — pkg/estimate/
- **Priority:** high
- **Spec:** specs/cost-estimator.md §8.1 (Approval Gates)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/estimate/approval.go (NEW), pkg/estimate/approval_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/estimate/... -count=1 -cover` passes with >85% coverage
- **Logic:** ApprovalGate evaluates estimated cost against remaining budget. AUTO_APPROVED if cost ≤ remaining. AUTO_APPROVED_WITH_WARNING if cost ≤ remaining × 1.5 AND trust ≥ 70. BLOCKED if cost > remaining (with 3 options: wait, increase, cheaper model). ESCALATED if cost > weekly cap (requires human approval). Returns ApprovalDecision with reason, remaining budget after, and suggested alternatives (cheaper model IDs).
- **Result:** [x] 29 tests, 94.9% pkg/estimate coverage (up from 94.4%). ApprovalGate with Evaluate (full spec §8.1 logic), EvaluateWithTrust (trust override), BatchEvaluate (multi-agent). GateApprovalResult with RemainingBefore/After, BlockedOptions (wait/increase/cheaper_model), CheaperAlternatives (sorted, ≤5, skips original model). estimateCheaperCost recalculates with different model pricing + markup. AnyApproved/AllBlocked batch helpers. FormatGateResult for CLI. Full suite 25/25 pass. Lint clean.

## [x] Implement production verification breach reporter — pkg/verify/
- **Priority:** medium
- **Spec:** specs/production-verification.md §Behavior Contracts (breach display) + specs/adversarial-review.md §Evidence Bundles (structured display)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/verify/breach_report.go (NEW), pkg/verify/breach_report_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/verify/... -count=1 -cover` passes with >85% coverage
- **Logic:** BreachReporter generates structured breach reports for Forgejo PR comments when behavior contracts are violated. Report sections: contract name, agent ID, deployment phase (shadow/canary/steady-state), failed assertions with actual vs expected values, metrics snapshot at breach time, drift summary, recommended action (rollback/investigate/waive), evidence bundle link. FormatBreachReport renders markdown suitable for Forgejo comment rendering.
- **Result:** [x] 25 tests, 95.5% pkg/verify coverage. BreachReporter with ReportFromBreach (Monitor.Breach → BreachReportData). Phase-aware recommended action (shadow→rollback safe, canary→investigate, steady-state→rollback). FormatBreachReport renders full markdown (header, action badge, failed assertions table, metrics table, drift table, evidence link). PhaseFromState maps ShadowState→DeploymentPhase. BreachSummary for log output. Full pipeline integration test (Monitor.Evaluate → breach → report). Full suite 25/25 pass. Lint clean.

## [x] Implement prompt index consistency checker with auto-rebuild — pkg/prompt/
- **Priority:** high
- **Spec:** specs/prompt-registry-v2.md §8.4 (Index Consistency)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/prompt/consistency.go (NEW), pkg/prompt/consistency_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/prompt/... -count=1 -cover` passes with >85% coverage
- **Logic:** CheckIndex per spec §8.4: recompute hash from prompt.md, compare against metadata.hash and index.hash. INDEX_STALE (index != metadata but metadata == disk) → non-blocking warning + auto-rebuild. TAMPER_DETECTED (metadata != recomputed) → blocking. MISSING (metadata.yaml or prompt.md absent) → report. ORPHANED (prompt directory exists but not in index) → report. RebuildIndex reconstructs _index.yaml from disk by scanning all component/version directories. Report with per-entry status, summary counts, and CLI formatting.
- **Result:** [x] 28 tests, 93.5% pkg/prompt coverage. CheckIndex with 5 consistency statuses (ok/index_stale/tamper_detected/missing_on_disk/orphaned_on_disk). Auto-rebuild on stale entries only (never on tamper). RebuildIndex from disk with underscore-dir/invalid-entry skipping. ConsistencyReport with HasIssues/ShouldBlock/FormatReport. Round-trip integration tests. Full suite 25/25 pass. Lint clean.

## [x] Implement trust ledger compaction — pkg/trust/
- **Priority:** medium
- **Spec:** specs/trust-model.md §The Trust Ledger — "replay the ledger to verify any agent's current score" (ledger grows unbounded without compaction)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/trust/compaction.go (NEW), pkg/trust/compaction_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/trust/... -count=1 -cover` passes with >85% coverage
- **Logic:** LedgerCompactor reduces JSONL trust ledger size by summarizing old events. Events older than the compaction threshold (default 90 days) are summarized into a single CompactionSummary entry per agent (score snapshot, event count, date range). Recent events (within threshold) are preserved verbatim. Compact reads the ledger, partitions by age, writes a new ledger with summary prefix + recent events. VerifyCompaction replays the compacted ledger and confirms scores match the pre-compaction replay.
- **Result:** [x] 19 tests, 89.5% pkg/trust coverage. LedgerCompactor with Compact (90d default, 10-event min threshold), in-place compaction with .bak backup. CompactionSummary captures score snapshot. VerifyCompaction with FP-tolerant score matching. NeedsCompaction (>30% old threshold). GetStats for ledger diagnostics. replayToScoreFromEvents handles EventCompactionSummary. Replaces pre-existing summaries. Full suite 25/25 pass. Lint clean.

## [x] Implement model rotation for adversarial review — pkg/review/
- **Priority:** high
- **Spec:** specs/adversarial-review.md §Model Formation Strategy: "Rotation: model assignments change per-review to prevent adversarial adaptation"
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/review/rotation.go (NEW), pkg/review/rotation_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/review/... -count=1 -cover` passes with >85% coverage
- **Logic:** RotationTracker records model→role assignments across reviews to enforce rotation fairness. FormationAssigner selects models from pool and assigns roles per change category (contract=3, behavioral=2, resilience/cosmetic=1). Selection prioritizes models with lower consecutive-same-role counts (prevents any model from being "stuck" in one role). Provider diversity enforced (no two from same provider). RLHF diversity configurable. Deterministic per-review seed (same PR → same assignment). CheckDiversity validates formation against diversity rules. SeedFromPR for deterministic seed generation.
- **Result:** [x] 27 tests, 94.2% pkg/review coverage. RotationTracker with consecutive/total tracking. FormationAssigner with rotation-priority sorting and diversity-enforced selection. CheckDiversity with provider + RLHF diversity checks. PanelSizeForCategory + rolesForPanelSize helpers. Deterministic seeding via SHA-256 hash. Thread-safe. Full suite 25/25 pass. Lint clean.

## [x] Implement LangFuse HTTP client — pkg/integration/
- **Priority:** medium
- **Spec:** specs/cross-component-wiring.md §3.1 (Chimera → LangFuse observability)
- **Model:** direct write — Go package, concrete HTTP client
- **Files:** pkg/integration/langfuse_client.go (NEW), pkg/integration/langfuse_client_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/integration/... -count=1 -cover` passes with >85% coverage
- **Logic:** Concrete HTTP implementation of LangFuseAdapter interface. IngestTrace posts to /api/public/ingestion with BasicAuth. GetTrace retrieves by ID from /api/public/traces/{id}. ListTraces queries with project filter + pagination. Health checks /api/public/health with context-aware timeout. All methods use httptest mock servers for testing. parseLangFuseTrace converts raw JSON maps to typed structs.
- **Result:** [x] 15 tests. IngestTrace with auth verification + error handling (500/401/connection error). GetTrace with 404 handling. ListTraces with project filter + empty results. Health with down/connection-error detection. WithTimeout + WithCustomHTTPClient options. Full suite 25/25 pass. Lint clean.
## [x] Implement real rate limiter (token bucket) — pkg/identity/
- **Priority:** high
- **Spec:** specs/agent-identity.md §13 (Rate Limiting and Retry)
- **Model:** direct write — Go package, replace no-op stub
- **Files:** pkg/identity/provisioner.go (extend), pkg/identity/provisioner_http_test.go (extend), pkg/identity/types_test.go (extend)
- **AC:** `go build ./... && go test ./pkg/identity/... -count=1` passes
- **Logic:** Replace the no-op Acquire() stub with a real token bucket using time.Ticker + buffered channel. Background goroutine refills tokens at rate per second. Close() method stops the goroutine. Steady state: 10 req/s, burst: configurable. Spec §13 compliance.
- **Result:** [x] Real token bucket with background refill goroutine. Acquire() now blocks when tokens exhausted. Close() for clean shutdown. 4 new tests: real throttle timing, burst exhaustion, concurrent acquire, idempotent close. Existing tests updated with Close() cleanup. Full suite 25/25 pass. Lint clean.

## [x] Implement prompt provenance display formatter — pkg/prompt/
- **Priority:** medium
- **Spec:** specs/prompt-registry.md §11.2 (Chain Verification display format) + §11.3 (Tamper Detection)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/prompt/provenance_display.go (NEW), pkg/prompt/provenance_display_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/prompt/... -count=1 -cover` passes with >85% coverage
- **Logic:** FormatProvenanceChain renders the spec §11.2 structured display format (COMMIT/PROMPT/SPEC/WORK ITEM/INTENT with ✅/❌ markers). FormatTamperReport renders the §11.3 tamper detection output. SummarizeProvenance returns a compact machine-readable summary for audit logs.
- **Result:** [x] 11 tests. FormatProvenanceChain (complete/incomplete/nil/short-SHA), FormatTamperReport, SummarizeProvenance (complete/with-failures), stageDisplayLabel, shortSHA. Full suite 25/25 pass. Lint clean.

## [x] Implement cost estimator structured observability logger — pkg/estimate/
- **Priority:** medium
- **Spec:** specs/cost-estimator.md §14 (Observability)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/estimate/observability.go (NEW), pkg/estimate/observability_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/estimate/... -count=1 -cover` passes with >85% coverage
- **Logic:** EstimationLogger implementing spec §14: verbose structured logging (timestamp [level] agent=NAME task_type=CODE model=X estimated=$Y cache_hit=Z% decision=D), JSON estimation record files for reconciliation, drift metric gauge logging, recalibration flag (>20% drift over 20 tasks). WriteEstimationRecord/ReadEstimationRecords for JSONL persistence.
- **Result:** [x] 12 tests. LogVerbose (human-readable spec §14 format), LogEstimation (JSON), LogDrift (gauge metric), LogRecalibration (threshold flag), LogError, nil-safety. WriteEstimationRecord/ReadEstimationRecords JSONL round-trip. CheckRecalibration (triggered/not-triggered/too-few). splitJSONL helper. Full suite 25/25 pass. Lint clean.

## [x] Implement marketplace agent display formatter — pkg/marketplace/
- **Priority:** high
- **Spec:** specs/agent-marketplace.md §17 (Example Outputs)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/marketplace/display.go (NEW), pkg/marketplace/display_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/marketplace/... -count=1 -cover` passes with >85% coverage
- **Logic:** FormatAgentTable renders spec §17 list agents table (NAME, TIER, TRUST, RATING, TASKS, COST/AVG, CAPABILITIES). FormatAgentDetail renders detailed agent view with capabilities, cost profile, performance metrics, ratings, recent reviews, deprecation warnings. FormatRatingSubmission renders rating confirmation. FormatDeprecationNotice renders auto-deprecation progress warning. Star rating formatters (integer and float with half-star support). FormatTrustDistribution histogram. FormatRegistrySummary marketplace overview. 95.0% pkg/marketplace coverage.
- **Result:** [x] 22 tests. Full suite 25/25 pass. Lint clean.

## [x] Implement prompt Prometheus metrics collector — pkg/prompt/
- **Priority:** medium
- **Spec:** specs/prompt-registry-v2.md §19 (Observability)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/metrics.go (NEW), pkg/prompt/metrics_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/prompt/... -count=1 -cover` passes with >85% coverage
- **Logic:** MetricsCollector implementing all 5 spec §19 Prometheus metrics: helix_prompts_total{status} (gauge), helix_prompt_attestations_total (counter), helix_prompt_attestation_failures_total{reason} (counter), helix_prompt_versions_total{component} (gauge), helix_prompt_overrides_total (counter). Prometheus text exposition format with HELP/TYPE headers. Deterministic ordering (sorted by metric name then label). Thread-safe with sync.RWMutex. UpdateFromIndex populates from registry Index. 93.0% pkg/prompt coverage.
- **Result:** [x] 12 tests. Full suite 25/25 pass. Lint clean.

## [x] Implement sandbox security property validator — pkg/sandbox/
- **Priority:** high
- **Spec:** specs/sandbox.md §9 (Security Properties)
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/sandbox/security.go (NEW), pkg/sandbox/security_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/sandbox/... -count=1 -cover` passes with >85% coverage
- **Logic:** SecurityValidator checks all 7 spec §9 security properties: (1) no-home-access — /home, /root, ~/.ssh never mounted, (2) no-network-access — workspace/full unshare network, (3) pid-isolation — private PID namespace, (4) memory-bounds — cgroup v2 memory.max, (5) time-bounds — context deadline + SIGKILL, (6) no-gpu-full-mode — GPU never enabled, (7) die-with-parent — cleanup on exit. ValidateStrict returns error if any check fails. CheckSessionPermissions rejects path traversal. ValidateMountSpec rejects forbidden source/dest paths. RequiredMountPoints returns spec-mandated bind mounts. ForbiddenMountSources lists never-mount paths. 93.1% pkg/sandbox coverage.
- **Result:** [x] 20 tests. Full suite 25/25 pass. Lint clean.

## [x] Implement Chimera HTTP client — pkg/integration/
- **Priority:** high
- **Spec:** specs/integrations.md §2 (Chimera Adapter) + specs/cross-component-wiring.md §3
- **Model:** direct write — Go package, concrete HTTP client (follows LangFuse client pattern)
- **Files:** pkg/integration/chimera_client.go (NEW), pkg/integration/chimera_client_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/integration/... -count=1 -cover` passes with >85% coverage
- **Logic:** ChimeraClient implements ChimeraAdapter interface with real HTTP calls. Review() POSTs to /api/v1/deliberate with ChimeraPR payload. Formations() GETs /api/v1/formations. Models() GETs /api/v1/models. Health() GETs /api/v1/health. All methods use httptest mock servers for testing. Context-aware with timeout. Auth via Bearer token. Error handling for 401/429/5xx. parseChimeraVerdict converts raw JSON to typed ChimeraVerdict.
- **Result:** [x] 25 tests (Review success/auth/rate-limit/budget/server-error/conn-error/malformed, Formations success/auth/empty/error, Models success/auth/empty/error, Health success/down/conn/malformed, parseChimeraVerdict empty/nil/multiple-findings, serializeAgentReviews, with-agent-reviews). All new functions 86-100% coverage. Full suite 25/25 pass. Lint clean.

## [x] Implement GitReins HTTP client — pkg/integration/
- **Priority:** high
- **Spec:** specs/integrations.md §1 (GitReins Adapter) + specs/cross-component-wiring.md §1
- **Model:** direct write — Go package, concrete HTTP client (follows LangFuse client pattern)
- **Files:** pkg/integration/gitreins_client.go (NEW), pkg/integration/gitreins_client_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/integration/... -count=1 -cover` passes with >85% coverage
- **Logic:** GitReinsClient implements GitReinsAdapter interface with real HTTP/subprocess calls. Guard() invokes `gitreins guard` subprocess or HTTP endpoint. Evaluate() POSTs diff to GitReins API. Cost() computes from LLMUsage in EvalResult using pricing data. All methods use httptest mock servers. Context-aware. Error handling for all spec §1 error scenarios.
- **Result:** [x] 21 tests (Guard success/auth/rate-limit/server-error/conn-error/malformed, Evaluate success/auth/timeout/rate-limit/server-error/conn-error, Cost with-pricing/nil/zero-tokens, Health success/conn-error/server-error, parseGuardResult/parseEvalResult empty/nil). All new functions 80-100% coverage. Full suite 25/25 pass. Lint clean.

## [x] Generate behavior contract assertions from review findings — pkg/review + pkg/verify
- **Priority:** medium
- **Spec:** specs/production-verification.md §Integration Points: "Chimera: Generates behavior contract assertions from review findings"
- **Model:** direct write — Go packages, cross-package bridge
- **Files:** pkg/review/contract_gen.go (NEW), pkg/review/contract_gen_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/review/... -count=1 -cover` passes with >85% coverage
- **Logic:** ContractGenerator converts EvidenceBundle findings into BehaviorContract assertions. Each finding (high severity, performance, logic) maps to a contract assertion: performance finding → latency_p99 lte Xms, logic finding → success_rate gte 99%, security finding → error_count lte 0. GenerateFromFindings takes an EvidenceBundle and returns a *verify.BehaviorContract with auto-generated assertions. Includes confidence-based assertion thresholds (high-confidence findings → stricter assertions).
- **Result:** [x] 25 tests, 100% coverage on contract_gen.go, 93.5% total pkg/review. Category-aware mapping: security→error_count+success_rate, performance→latency_p99, logic→success_rate, race→error_count+latency, spec_violation→success_rate. Severity-based thresholds (critical stricter than high). Confidence weight scaling. Consensus-based breach action. Full suite 25/25 pass. Lint clean.

## [x] Implement end-to-end deployment trace pipeline — pkg/verify + pkg/integration
- **Priority:** low
- **Spec:** specs/production-verification.md §Integration Points: "LangFuse: Full trace of agent → merge → shadow → canary → production → incident"
- **Model:** direct write — Go package, cross-package bridge
- **Files:** pkg/verify/trace.go (NEW), pkg/verify/trace_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/verify/... -count=1 -cover` passes with >85% coverage
- **Logic:** DeploymentTracePipeline records every lifecycle stage of a deployment as a LangFuse trace span. From agent commit → GitReins guard → merge → shadow deploy → canary → production → incident (if any). Each stage is a trace with duration, status, cost, and evidence links. ExportTrace converts to LangFuseTrace for ingestion. Enables full observability of the agent → production pipeline.
- **Result:** [x] 42 tests, 96.0% pkg/verify coverage. DeploymentTracePipeline with 8 lifecycle stages (commit, guard, review, merge, shadow, canary, production, incident). TraceSpan with DurationMs/IsComplete. Convenience methods for each stage (RecordGuardSpan, RecordMergeSpan, RecordShadowSpan, RecordCanarySpan, RecordProductionSpan, RecordIncidentSpan). ExportTrace → LangFuseTraceExport with per-span metadata merging (evidence + metadata + cost/duration). TraceSummary with IsComplete/HasIncident/FinalStage. Thread-safe with sync.RWMutex. Concurrent access verified. Full suite 25/25 pass.

## [x] Implement platform metrics aggregator — pkg/health/
- **Priority:** medium
- **Spec:** specs/SPECIFICATION.md §8 (Observability) — aggregate metrics from all subsystems
- **Model:** direct write — Go package, extend existing
- **Files:** pkg/health/metrics.go (NEW), pkg/health/metrics_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/health/... -count=1 -cover` passes with >85% coverage
- **Logic:** PlatformMetricsCollector aggregates Prometheus metrics from all Helix subsystems into a single /metrics endpoint. Combines: trust (trust score distribution, tier counts), review (reviews total, findings by severity, consensus resolution rate), estimate (estimates total, budget utilization), marketplace (agents active, queries), verify (deployments shadowing/canaried/promoted, breaches), negotiate (negotiations total, resolutions). Prometheus text exposition format. Thread-safe.
- **Result:** [x] 23 tests, 100% coverage on metrics.go, 99.3% total pkg/health. MetricsSource interface for pluggable subsystem registration. Deterministic metric+label sorting. Header deduplication. Internal counter support. Large metric set handling (100+ lines). Full suite 25/25 pass. Lint clean.

## [ ] Implement PromptFoo CI result processor CLI — cmd/helix-prompt/
- **Priority:** medium
- **Spec:** specs/prompt-registry-v2.md §11.3 (postci command) + §11 (PromptFoo CI Integration)
- **Model:** direct write — Go package, extend CLI
- **Files:** cmd/helix-prompt/main.go (extend), cmd/helix-prompt/main_test.go (extend)
- **AC:** `go build ./... && go test ./cmd/helix-prompt/... -count=1` passes
- **Logic:** Add `postci` subcommand to helix-prompt CLI. Reads PromptFoo eval results JSON, parses pass/fail per test case, updates metadata.yaml promptfoo status for each affected component, writes summary to stdout. Exit code: 0 if all pass, 1 if any fail. Integrates with existing GeneratePromptFooYAML and ParsePromptFooResults.

## [ ] Implement Conscientiousness adapter HTTP client — pkg/integration/
- **Priority:** medium
- **Spec:** specs/integrations.md §3 (Conscientiousness → Helix Adversarial Review Adapter)
- **Model:** direct write — Go package, concrete HTTP client
- **Files:** pkg/integration/conscientiousness_client.go (NEW), pkg/integration/conscientiousness_client_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/integration/... -count=1 -cover` passes with >85% coverage
- **Logic:** ConscientiousnessClient implements ConscientiousnessAdapter interface with real HTTP calls. SubmitReview() POSTs adversarial review findings to Conscientiousness for feedback loop. QueryPatterns() GETs known adversarial patterns. Health() checks service. All methods use httptest mock servers. Context-aware. Error handling for 401/429/5xx. Follows ChimeraClient pattern.

## [ ] Implement Muster adapter HTTP client — pkg/integration/
- **Priority:** medium
- **Spec:** specs/integrations.md §4 (Muster → Helix API Glue Adapter)
- **Model:** direct write — Go package, concrete HTTP client
- **Files:** pkg/integration/muster_client.go (NEW), pkg/integration/muster_client_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/integration/... -count=1 -cover` passes with >85% coverage
- **Logic:** MusterClient implements MusterAdapter interface with real HTTP calls. GenerateCommands() POSTs OpenAPI spec for muster tool generation. ListTools() GETs available muster-generated tools. ExecuteTool() calls a muster-generated tool. Health() checks service. httptest mock servers for all methods. Context-aware. Follows GitReinsClient pattern.

## [ ] Implement Axiom adapter HTTP client — pkg/integration/
- **Priority:** medium
- **Spec:** specs/integrations.md §6 (Axiom → Helix Orchestration Adapter)
- **Model:** direct write — Go package, concrete HTTP client
- **Files:** pkg/integration/axiom_client.go (NEW), pkg/integration/axiom_client_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/integration/... -count=1 -cover` passes with >85% coverage
- **Logic:** AxiomClient implements AxiomAdapter interface with real HTTP calls. CreateWorkItem() POSTs a new work item. GetWorkItem() GETs work item status. ListWorkItems() GETs filtered list. AssignAgent() PUTs agent assignment. Health() checks service. httptest mock servers. Context-aware. Follows ChimeraClient pattern.

## [ ] Implement Hivemind adapter HTTP client — pkg/integration/
- **Priority:** low
- **Spec:** specs/integrations.md §7 (Hivemind → Helix Memory & Scheduling Adapter)
- **Model:** direct write — Go package, concrete HTTP client
- **Files:** pkg/integration/hivemind_client.go (NEW), pkg/integration/hivemind_client_test.go (NEW)
- **AC:** `go build ./... && go test ./pkg/integration/... -count=1 -cover` passes with >85% coverage
- **Logic:** HivemindClient implements HivemindAdapter interface with real HTTP calls. QueryMemory() searches shared agent memory. StoreMemory() persists a learning or observation. ScheduleTask() queues a periodic task. GetSchedule() retrieves schedule. Health() checks service. httptest mock servers. Context-aware. Follows LangFuseClient pattern.
