# Helix API Reference

Package-level reference for the Helix platform libraries. These pages list the
exported types and method signatures so integrators can use the packages
without reading Go source first.

## Packages

| Package | Page | Purpose |
|---------|------|---------|
| `pkg/identity` | [identity.md](identity.md) | Forgejo OAuth, SSH keys, PAT provisioning |
| `pkg/prompt` | [prompt.md](prompt.md) | Prompt provenance, hash attestation, PromptFoo bridge |
| `pkg/marketplace` | [marketplace.md](marketplace.md) | Agent discoverability, trust scoring, human ratings |
| `pkg/estimate` | [estimate.md](estimate.md) | Token burn pre-flight, cache-aware pricing |
| `pkg/negotiate` | [negotiate.md](negotiate.md) | Agent debate protocol + Chimera tie-break |
| `pkg/dispatcher` | [dispatcher.md](dispatcher.md) | Ralph Loop engine, task decomposition, agent assignment |
| `pkg/sandbox` | [sandbox.md](sandbox.md) | Bubblewrap-based agent isolation |
| `pkg/review` | [review.md](review.md) | Multi-model review pipeline, blast radius, dashboard, load balancing |
| `pkg/mergegate` | [mergegate.md](mergegate.md) | Pre-receive hook: trust-tier, secrets, attestation enforcement |
| `pkg/coapproval` | [coapproval.md](coapproval.md) | Final merge approval gate with multi-model consensus |
| `pkg/forcemerge` | [forcemerge.md](forcemerge.md) | Audit trail for every admin override merge |
| `pkg/audit` | [audit.md](audit.md) | 12-step audit trail checker per spec |
| `pkg/adversarial` | [adversarial.md](adversarial.md) | Encoded testing scenario pack for adversarial review |
| `pkg/ideation` | [ideation.md](ideation.md) | Offline idea capture, validation, prioritization, promotion |
| `pkg/spec` | [spec.md](spec.md) | Multi-agent spec creation with adversarial annotation, 12-dim completeness |
| `pkg/adr` | [adr.md](adr.md) | Architecture Decision Records with co-authoring and multi-model review |
| `pkg/design` | [design.md](design.md) | Automated design review via adversarial agents (5 roles) |
| `pkg/contract` | [contract.md](contract.md) | OpenAPI/protobuf generation, validation, breaking change detection |
| `pkg/coordinator` | [coordinator.md](coordinator.md) | Full PR lifecycle orchestration composing all services |
| `pkg/pipeline` | [pipeline.md](pipeline.md) | 12-step PR lifecycle state machine |
| `pkg/retry` | [retry.md](retry.md) | Exponential backoff for cross-service calls |
| `pkg/config` | [config.md](config.md) | Unified platform configuration loading |
| `pkg/learning` | [learning.md](learning.md) | Cross-agent notification and context sharing with domain pub/sub |
| `pkg/trust` | [trust.md](trust.md) | Graduated multi-dimensional trust with tier assignment and decay |
| `pkg/memory` | [memory.md](memory.md) | DuckBrain and Hivemind memory schema types and interfaces |
| `pkg/incident` | [incident.md](incident.md) | Incident learning database with attribution engine |
| `pkg/verify` | [verify.md](verify.md) | Shadow deployment, canary promotion, behavior diff, auto-rollback |
| `pkg/security` | [security.md](security.md) | Security hardening checklist verifier |
| `pkg/vuln` | [vuln.md](vuln.md) | Dependency vulnerability scanner |
| `pkg/recovery` | [recovery.md](recovery.md) | Structured error recovery procedures per component |
| `pkg/degradation` | [degradation.md](degradation.md) | Platform graceful-degradation policies |
| `pkg/backup` | [backup.md](backup.md) | Structured backup strategy data and validation |
| `pkg/health` | [health.md](health.md) | Agent and platform health metrics |
| `pkg/api` | [api.md](api.md) | Typed Go structs from spec API contracts |
| `pkg/ci` | [ci.md](ci.md) | Forgejo Actions workflow generation and validation |
| `pkg/webhook` | [webhook.md](webhook.md) | Forgejo webhook event receiver |
| `pkg/forgejo` | [forgejo.md](forgejo.md) | Forgejo REST API client: branches, repos, PRs |
| `pkg/integration` | [integration.md](integration.md) | End-to-end integration test harnesses |
| `pkg/log` | [log.md](log.md) | Dependency-free structured logging facility |
| `pkg/banner` | [banner.md](banner.md) | ASCII art startup banners for Helix CLI |
| `pkg/deploy` | [deploy.md](deploy.md) | Platform deployment configuration |

## Related

- [Getting Started](../GETTING-STARTED.md) — onboarding guide
- [Specifications](../specs/) — feature specs and platform docs

## Conventions

- All packages are Go 1.25+, importable as
  `github.com/totalwindupflightsystems/helix/pkg/<name>`.
- Errors are returned as Go `error` values; `pkg/forgejo` and `pkg/identity`
  expose typed errors (`APIError`, `TypedError`) for classification.
- Method signatures below were generated from `go doc` on `master` and match
  the current source. When in doubt, `go doc pkg/<name>` in the repo root is
  authoritative.
