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

## [ ] Create Helix bootstrap script
- **Priority:** high
- **Spec:** specs/build-order.md §9
- **Model:** direct write — bash script
- **Files:** scripts/bootstrap.sh (NEW)
- **AC:** `scripts/bootstrap.sh` starts Forgejo + Chimera, builds all Helix CLIs, and runs verification (all 5 checks from build-order.md §10)
- **Logic:** Automate the Phase 0→4 sequence: create directories, docker run Forgejo, pip install Chimera + start, verify both health, go build ./cmd/...

## [ ] Create Docker Compose for Helix platform
- **Priority:** high
- **Spec:** specs/deployment.md §2
- **Model:** direct write — docker-compose.yaml
- **Files:** deploy/docker-compose.yaml (NEW)
- **AC:** `docker compose -f deploy/docker-compose.yaml up -d` starts Forgejo + Chimera + LangFuse + all 9 services with health checks
- **Logic:** Define all service containers with helix-net, volumes, health checks, environment variables from deployment.md topology.

## [ ] Implement circuit breaker for cross-service HTTP calls
- **Priority:** medium
- **Spec:** specs/cross-component-wiring.md §8
- **Model:** direct write — Go package
- **Files:** pkg/integration/circuitbreaker.go, pkg/integration/circuitbreaker_test.go (NEW)
- **AC:** `go test ./pkg/integration/... -count=1` passes — circuit breaker allows/denies calls based on MaxFailures (5), ResetTimeout (60s), half-open probe state
- **Logic:** CircuitBreaker struct with Allow(), RecordSuccess(), RecordFailure(). Configurable MaxFailures (default 5), ResetTimeout (default 60s). Half-open state allows one probe call on timeout expiry.

## [ ] Create platform config templates
- **Priority:** low
- **Spec:** specs/helix-config.md
- **Model:** direct write — YAML files
- **Files:** deploy/config.yaml.example, deploy/pricing.yaml.example
- **AC:** Example config files compile against helix CLI validation
- **Logic:** Create template versions of `~/.helix/config.yaml` and `~/.helix/pricing.yaml` from the config spec.

## [] Wire dispatcher to Forgejo — agent spawn pipeline
- **Priority:** critical
- **Spec:** specs/dispatcher.md + specs/agent-identity.md
- **Model:** deepseek-v4-pro — integration work, needs live services
- **Files:** pkg/dispatcher/forgejo_spawn.go, pkg/dispatcher/spawn_test.go
- **AC:** `helix dispatch --spec specs/agent-identity.md --agent test-agent` creates a branch in Forgejo, provisions an agent, and returns a PR URL
- **Logic:** Full Ralph Loop: acquire lock → create worktree → spawn agent → wait for completion → run GitReins guards → open PR → return URL. Requires Forgejo running on :3030.
