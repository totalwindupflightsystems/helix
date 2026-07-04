# Changelog

All notable changes to Helix are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `pkg/deploy/systemd/` — Systemd unit template generator for spec §9.4
  (helix-platform.service, helix-backup.service, helix-backup.timer).
  27 tests, 98.1% coverage.
- `pkg/deploy/agent/` — Per-agent container template generator for spec §9.5
  (4 tiers, 11 env vars, security_opt, VPN routing). 35 tests, 99.0% coverage.
- `pkg/degradation/` — Graceful degradation policy pack for spec §14.2
  (9 services × 3-4 health states, 28 policies, ApplyPolicy with structured
  decision output). 36 tests, 99.2% coverage.
- `pkg/adversarial/` — Adversarial test scenario pack for spec §12.4
  (5 spec scenarios, 6 adversarial roles, Library + RunAll + Report).
  35 tests, 92.7% coverage.

## [0.7.0] — 2026-07-04 — CLI hardening + recent batch

### Added
- `pkg/deploy/systemd/` — Spec §9.4 systemd unit templates
- `pkg/deploy/agent/` — Spec §9.5 per-agent container template generator
- `pkg/degradation/` — Spec §14.2 graceful degradation policy pack
- `pkg/adversarial/` — Spec §12.4 adversarial test scenario pack
- `cmd/helix/dispatch.go` — Built-in `dispatch` subcommand for spec dispatch
  (`helix dispatch --spec <path> --agent <name> --repo <r> --dry-run`).
  Forged PR lifecycle (CreateBranch + CreatePR) with idempotent 409 handling.
- Coverage for `cmd/helix-negotiate` (35.7% → 70.1%) and `cmd/helix-identity`
  (47.1% → 78.1%) — 16 new tests.

### Changed
- Global `--dry-run` now honoured by `dispatch` via `runDispatchWithDryRun`
  helper (refactor of `cmd/helix/main.go`).

## [0.6.0] — 2026-06-30 — Memory + Observability

### Added
- `pkg/memory/` — DuckBrain memory schema (spec §8.5) + Hivemind lifecycle
  (spec §8.6). 25 schema tests + 19 lifecycle tests.
- `pkg/config/envvars.go` — Spec §9.6 env var inventory with secret
  redaction. 15 tests, 95.2% coverage.
- `pkg/api/contracts.go` — Spec §15 API contracts for 5 services
  (Forgejo, Chimera, Conscientiousness, Hivemind, Muster). 48 tests, 91.0%.
- `pkg/security/incident.go` — Spec §6.7 incident response engine (4 severity
  levels, 5 incident types). 40 tests, 96.2% coverage.

## [0.5.0] — 2026-06-29 — Audit + Co-Approval

### Added
- `pkg/audit/` — 12-step audit trail checker for spec §6.5. 47 tests, 86.4%.
- `pkg/audit/builder/` — 12-step evidence chain builder (fluent API). 30 tests, 89.0%.
- `pkg/security/blast/` — Blast radius containment verifier for spec §6.4.
  30 tests, 93.4%.
- `pkg/security/secrets/` — Centralized secret-pattern scanner for spec §6.2.
  39 tests, 95.2%.
- `pkg/security/hardening.go` — Spec §6.6 security hardening checklist
  (22 checks). 35 tests, 97.2%.
- `pkg/coapproval/` — Spec §7.2 co-approval gate (1 human + 1 agent).
- `pkg/pipeline/` — 6-gate pipeline executor for spec §7.2 (Tier 1 → Tier 2
  → Chimera → Conscientiousness → PromptFoo → Co-Approval). 17 tests, 96.6%.

## [0.4.0] — 2026-06-28 — Production Verification + Memory Bank

### Added
- `pkg/verify/contract.go` + `pkg/verify/monitor.go` — Behavior contracts
  for spec production-verification.md (success rate, latency, error count
  assertions; canary ramp schedule by trust tier). 51 tests, 96.9%.
- `pkg/prompt/` — Spec prompt-registry.md (prompt hashing, attestation).
- `pkg/recovery/` — Spec §10.3 disaster recovery scenarios (5 scenarios)
  + §10.4 scaling model. 13 tests, 100% coverage.
- `pkg/health/sla.go` — Spec §11 performance SLAs (sync/review/merge/sandbox/
  API/cost/monitoring). 16 tests, 94.3%.
- `pkg/estimate/attribution.go` — Spec §8.3 cost attribution model
  (4-level hierarchy). 15 tests, 94.3%.

## [0.3.0] — 2026-06-27 — Trust + Adversarial Review

### Added
- `pkg/trust/` — Spec trust-model.md graduated trust tiers (Provisional →
  Observed → Trusted → Veteran) + 6-dimension scoring + incident-linked decay
  + JSONL ledger. 59 tests, 86.8% coverage.
- `pkg/review/bias_stripper.go` — Spec adversarial-review.md confirmation
  bias defense. 33 tests, 97.4% coverage.
- `pkg/review/evidence_bundle.go` — ED25519 signing for evidence bundles.
- `pkg/review/false_positive.go` — Spec §false positive feedback loop.
  19 tests, 100% line coverage.
- `pkg/incident/` — Incident learning database schema.

## [0.2.0] — 2026-06-26 — Security Model

### Added
- `pkg/health/` — Startup validation health checker (probes all configured
  services). Concurrency-safe parallel checks.
- `pkg/forgejo/` — Forgejo API client wrapper (CreateUser, CreateSSHKey,
  CreatePAT, ListPRs, GetPRReviews, CreatePRReview). Circuit breaker
  integration + retry with backoff.
- `pkg/sandbox/` — Real Bubblewrap execution wired (was ErrNotImplemented stub).
  Promoted 5 underscore-prefixed helpers to real functions.
- `.forgejo/workflows/` — 3 CI/CD pipeline files (gitreins.yaml, chimera-review.yaml,
  promptfoo.yaml).
- `scripts/check-trust-tier.sh` — File category → required tier mapping
  (IaC → Tier 1+, CI/CD → Tier 3+, auth → Tier 2+).
- `deploy/` — Docker Compose + config templates + pricing examples.

## [0.1.0] — 2026-06-21 — Initial Scaffolding

### Added
- `cmd/helix/` — Built-in `helix` CLI dispatcher.
- `cmd/helix-identity/` — Agent identity provisioning (5 subcommands).
- `cmd/helix-estimate/` — Cost estimation.
- `cmd/helix-negotiate/` — PR negotiation + Chimera tie-break.
- `cmd/helix-prompt/` — Prompt registry.
- `cmd/helix-marketplace/` — Agent marketplace.
- `cmd/sandbox/` — Bubblewrap sandbox runner.
- `pkg/dispatcher/` — Ralph Loop engine with worktree management.
- `pkg/integration/` — 9 component adapters (Axiom, Chimera, Conscientiousness,
  GitReins, Hivemind, Kobayashi-Maru, LangFuse, Muster, OpenCode, PromptFoo).
- `specs/` — 17 specification documents totaling 12,556 lines.
- `.gitreins/` — Tier 1 + Tier 2 evaluation pipeline.

## Spec Coverage Matrix

| Spec Section | Status | Package |
|--------------|--------|---------|
| §1 Platform Architecture | ✅ covered | (architecture-level, no code) |
| §2 Data Flow + 12 Steps | ✅ covered | `pkg/audit`, `pkg/audit/builder`, `pkg/dispatcher` |
| §3 Component Specs (17) | ✅ covered | Each external component has an adapter in `pkg/integration` |
| §4 Integration Contracts | ✅ covered | `pkg/api`, `pkg/integration` |
| §5 Identity & Access (IAM) | ✅ covered | `pkg/identity`, `pkg/coapproval` |
| §6.1 Threat Model | ✅ covered | `pkg/security` |
| §6.2 Secrets Management | ✅ covered | `pkg/security/secrets` |
| §6.3 Network Isolation | ✅ covered | `pkg/sandbox` |
| §6.4 Blast Radius | ✅ covered | `pkg/security/blast` |
| §6.5 Audit Trail | ✅ covered | `pkg/audit`, `pkg/audit/builder` |
| §6.6 Security Hardening | ✅ covered | `pkg/security` |
| §6.7 Incident Response | ✅ covered | `pkg/security/incident.go` |
| §7 Quality Gates | ✅ covered | `pkg/pipeline`, `pkg/coapproval` |
| §8.1 Observability Overview | ✅ covered | `pkg/health` |
| §8.2 LangFuse Trace Format | ✅ covered | `pkg/integration/adapter_langfuse.go` |
| §8.3 Cost Attribution | ✅ covered | `pkg/estimate/attribution` |
| §8.4 Prometheus Metrics | ✅ covered | `pkg/health/platform_metrics.go`, `pkg/health/agent_metrics.go` |
| §8.5 DuckBrain Memory Schema | ✅ covered | `pkg/memory` |
| §8.6 Hivemind Memory Bank | ✅ covered | `pkg/memory` |
| §9.1 Host Topology | ✅ covered | `specs/deployment.md` |
| §9.2 Docker Compose Topology | ✅ covered | `deploy/docker-compose.yaml` |
| §9.3 Caddy Reverse Proxy | ✅ covered | `deploy/docker-compose.yaml` |
| §9.4 systemd Units | ✅ covered | `pkg/deploy/systemd` |
| §9.5 Agent Container Template | ✅ covered | `pkg/deploy/agent` |
| §9.6 Env Var Inventory | ✅ covered | `pkg/config/envvars` |
| §10.1 Backup Strategy | ✅ covered | `pkg/backup` |
| §10.2 Restore Procedure | ✅ covered | `pkg/backup` |
| §10.3 Disaster Recovery | ✅ covered | `pkg/recovery/dr.go` |
| §10.4 Scaling Model | ✅ covered | `pkg/recovery/dr.go` |
| §10.5 Incident Response | ✅ covered | `pkg/security/incident.go` |
| §11 Performance SLAs | ✅ covered | `pkg/health/sla.go` |
| §12.1-12.5 Test Strategy | ✅ covered | `pkg/adversarial` + per-package tests |
| §13 Build Order | ✅ covered | `specs/build-order.md` |
| §14.1 Failure Matrix | ✅ covered | `pkg/recovery` |
| §14.2 Graceful Degradation | ✅ covered | `pkg/degradation` + `pkg/health/degradation.go` |
| §14.3 Retry Policies | ✅ covered | `pkg/retry` |
| §14.4 Data Recovery | ✅ covered | `pkg/recovery/dr.go` |
| §15 API Contracts | ✅ covered | `pkg/api` |
| Trust Model | ✅ covered | `pkg/trust` |
| Adversarial Review | ✅ covered | `pkg/review` |
| Production Verification | ✅ covered | `pkg/verify` |
| Agent Identity | ✅ covered | `pkg/identity` |
| Cost Estimator | ✅ covered | `pkg/estimate` |
| PR Negotiation | ✅ covered | `pkg/negotiate` |
| Prompt Registry | ✅ covered | `pkg/prompt` |
| Agent Marketplace | ✅ covered | `pkg/marketplace` |

## Coverage Summary

| Package | Tests | Coverage |
|---------|-------|----------|
| pkg/adversarial | 35 | 92.7% |
| pkg/api | — | 91.0% |
| pkg/audit | — | 86.4% |
| pkg/audit/builder | — | 89.0% |
| pkg/backup | — | 93.1% |
| pkg/coapproval | — | 100.0% |
| pkg/config | — | 95.2% |
| pkg/coordinator | — | 89.6% |
| pkg/degradation | 36 | 99.2% |
| pkg/deploy/agent | 35 | 99.0% |
| pkg/deploy/systemd | 27 | 98.1% |
| pkg/dispatcher | — | 89.1% |
| pkg/estimate | — | 94.3% |
| pkg/forgejo | — | 96.2% |
| pkg/health | — | 94.3% |
| pkg/identity | — | 86.8% |
| pkg/incident | — | 98.4% |
| pkg/integration | — | 80.3% |
| pkg/marketplace | — | 95.0% |
| pkg/memory | — | 86.5% |
| pkg/mergegate | — | 96.6% |
| pkg/negotiate | — | 97.4% |
| pkg/pipeline | — | 94.9% |
| pkg/prompt | — | 91.5% |
| pkg/recovery | — | 100.0% |
| pkg/retry | — | 95.0% |
| pkg/review | — | 93.5% |
| pkg/sandbox | — | 93.1% |
| pkg/security | — | 96.2% |
| pkg/security/blast | — | 93.4% |
| pkg/security/secrets | — | 95.2% |
| pkg/trust | — | 89.5% |
| pkg/verify | — | 96.0% |

**Total:** 33 packages, 124 tasks completed across 7 releases, all packages ≥80% coverage, GitReins Tier 1 PASS on every commit, all 7 CLI binaries build clean.

[Unreleased]: https://github.com/totalwindupflightsystems/helix/compare/v0.7.0...HEAD
[0.7.0]: https://github.com/totalwindupflightsystems/helix/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/totalwindupflightsystems/helix/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/totalwindupflightsystems/helix/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/totalwindupflightsystems/helix/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/totalwindupflightsystems/helix/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/totalwindupflightsystems/helix/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/totalwindupflightsystems/helix/releases/tag/v0.1.0