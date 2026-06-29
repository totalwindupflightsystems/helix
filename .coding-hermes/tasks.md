# Helix Coding Tasks — Foreman Queue

## [x] Implement trust scoring engine — pkg/trust/
- **Priority:** high
- **Spec:** specs/trust-model.md
- **Model:** direct write — Go package, pure math + JSONL
- **Files:** pkg/trust/scorer.go, pkg/trust/ledger.go, pkg/trust/tiers.go, pkg/trust/scorer_test.go
- **AC:** `go build ./... && go test ./pkg/trust/... -count=1 -cover` passes with >80% coverage
- **Logic:** TrustScore calculation (6 dimensions: merge success 0.25, incident attribution 0.30, review consensus 0.15, prompt integrity 0.10, human feedback 0.10, tenure 0.10), tier thresholds (Provisional/Observed/Trusted/Veteran), incident attribution with time-decay weight (100% at 0-7d, 50% at 8-30d, 10% at 31-90d, 0% after 90d), trust decay on inactivity (0.05/week), tier demotion logic, JSONL ledger append + replay verification.
- **Result:** [x] 59 tests, 86.8% coverage, all passing. Build + vet clean. Committed at #NEXT.

## [] Implement bias-stripper for adversarial review — pkg/review/
- **Priority:** high
- **Spec:** specs/adversarial-review.md §Confirmation Bias Defense
- **Model:** direct write — Go package, pure text processing
- **Files:** pkg/review/bias_stripper.go, pkg/review/bias_stripper_test.go
- **AC:** `go build ./... && go test ./pkg/review/... -count=1 -cover` passes with >85% coverage
- **Logic:** Strip evaluative language ("fixed", "correct", "ready", "passes"), remove confidence assertions ("tested locally", "works on my machine"), strip emoji and emotional framing, normalize formatting, preserve factual information (files changed, intent). Test with real commit messages from the 8 documented disasters.

## [] Implement production verification contracts — pkg/verify/
- **Priority:** medium
- **Spec:** specs/production-verification.md §Behavior Contracts
- **Model:** direct write — Go package, YAML contracts + metrics assertions
- **Files:** pkg/verify/contract.go, pkg/verify/monitor.go, pkg/verify/contract_test.go
- **AC:** `go build ./... && go test ./pkg/verify/... -count=1 -cover` passes with >80% coverage
- **Logic:** Behavior contract YAML parsing, assertion types (success rate gte/lte, latency p50/p95/p99, error count eq), breach detection, auto-rollback trigger logic, agent notification on breach, canary ramp schedule by trust tier (Provisional: 96h, Observed: 60h, Trusted: 36h, Veteran: 12h).

## [] Wire dispatcher to Forgejo — agent spawn pipeline
- **Priority:** critical
- **Spec:** specs/dispatcher.md + specs/agent-identity.md
- **Model:** deepseek-v4-pro — integration work, needs live services
- **Files:** pkg/dispatcher/forgejo_spawn.go, pkg/dispatcher/spawn_test.go
- **AC:** `helix dispatch --spec specs/agent-identity.md --agent test-agent` creates a branch in Forgejo, provisions an agent, and returns a PR URL
- **Logic:** Full Ralph Loop: acquire lock → create worktree → spawn agent → wait for completion → run GitReins guards → open PR → return URL. Requires Forgejo running on :3030.

## [] Implement evidence bundle signing — pkg/review/
- **Priority:** medium
- **Spec:** specs/adversarial-review.md §Evidence Bundles
- **Model:** direct write — Go package, ED25519 + JSON canonicalization
- **Files:** pkg/review/evidence.go, pkg/review/evidence_test.go
- **AC:** `go build ./... && go test ./pkg/review/... -count=1 -cover` passes with >80% coverage
- **Logic:** EvidenceBundle struct, ED25519 signing per model, consensus resolution (3/3, 2/3, 2/2, blocked, tie-breaker), JSON canonicalization for deterministic hashing, bundle verification (signature check + hash chain), DuckBrain storage integration.

## [] Implement false positive feedback loop — pkg/review/
- **Priority:** low
- **Spec:** specs/adversarial-review.md §False Positive Feedback Loop
- **Model:** direct write — Go package, counters + thresholds
- **Files:** pkg/review/false_positive.go, pkg/review/false_positive_test.go
- **AC:** `go build ./... && go test ./pkg/review/... -count=1 -cover` passes with >85% coverage
- **Logic:** False positive tracking (human_dismissed counter per model), threshold (10 dismissals → flag), model re-evaluation trigger (curated test suite of known-true + known-false findings), model rotation (remove models with >15% FP rate).

## [] Add trust tier enforcement to GitReins pre-commit hook
- **Priority:** high
- **Spec:** specs/trust-model.md §Integration Points
- **Files:** .gitreins/config.yaml (update), scripts/check-trust-tier.sh (NEW)
- **AC:** GitReins pre-commit blocks merges from agents below required trust tier for changed file categories
- **Logic:** File category mapping (IaC → Tier 1+, CI/CD → Tier 3+, auth → Tier 2+), trust tier from Helix marketplace, hook: query agent trust → compare to file requirements → block/report.

## [] Write incident learning database schema — pkg/incident/
- **Priority:** medium
- **Spec:** specs/incident-learning.md (TO BE WRITTEN)
- **Files:** pkg/incident/types.go, pkg/incident/store.go
- **AC:** `go build ./...` passes
- **Logic:** Incident struct (agent_id, pr_url, severity, causal_chain, timestamp), shared learning across agents, incident → trust penalty pipeline.
