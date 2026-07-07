---
name: helix
description: "Helix — agent-first software development platform. CLI tool exposing identity, estimate, negotiate, prompt, marketplace, sandbox, review, verify, trust, dispatcher, mergegate, security, and forgejo subcommands."
metadata:
  author: Bane
  version: "0.1.0"
  language: go
  coding-hermes: true
  foreman: helix-coding-hermes-foreman
---

# Helix

Agent-first software development platform where humans and AI agents are equal participants in the code lifecycle.

## Quick Start

Clone and:

```bash
# Build
go build ./...

# Test
go test -short -count=1 ./...

# Lint
golangci-lint run --timeout 5m ./...

# Run the unified CLI
./helix --help
./helix status
```

## Subcommand Surface

The unified `helix` binary (cmd/helix) wires every package as a subcommand:

| Subcommand | Package | Purpose |
|------------|---------|---------|
| `identity` | cmd/helix-identity, pkg/identity | Forgejo agent provisioning (sync, provision, deprovision, keygen) |
| `estimate` | cmd/helix-estimate, pkg/estimate | Token burn estimation + OpenRouter budget queries |
| `negotiate` | cmd/helix-negotiate, pkg/negotiate | Agent PR debate + Chimera tie-break |
| `prompt` | cmd/helix-prompt, pkg/prompt | Prompt provenance + hash attestation |
| `marketplace` | cmd/helix-marketplace, pkg/marketplace | Agent registry + trust scoring |
| `sandbox` | cmd/sandbox, pkg/sandbox | Bubblewrap agent isolation |
| `dispatch` | cmd/helix | Spec → PR pipeline (ForgejoLoop) |
| `dispatcher` | cmd/helix | Ralph Loop operator surface (status, tick, list-tasks) |
| `forgejo` | cmd/helix | REST adapter operator inspection |
| `review` | cmd/helix | Adversarial review pipeline surface |
| `verify` | cmd/helix | Production verification (shadow/canary/contract) |
| `trust` | cmd/helix | Trust snapshot queries |
| `mergegate` | cmd/helix | Pre-merge validation gate |
| `security` | cmd/helix | Security scanning |
| `incident` | cmd/helix | Incident learning queries |
| `recovery` | cmd/helix | Disaster recovery procedures |
| `coapproval` | cmd/helix | Co-approval workflow |
| `ci` | cmd/helix | CI pipeline surface |
| `memory` | cmd/helix | Memory bank surface |
| `adversarial` | cmd/helix | Adversarial agent surface |
| `audit` | cmd/helix | Audit log queries |
| `backup` | cmd/helix | Backup procedures |
| `banner` | cmd/helix | Banner display |
| `degradation` | cmd/helix | Graceful degradation |
| `deploy` | cmd/helix | Deployment surface |
| `forcemerge` | cmd/helix | Force-merge override |
| `integration` | cmd/helix | Integration CLI surface |
| `pipeline` | cmd/helix | Pipeline surface |
| `retry` | cmd/helix | Retry middleware surface |
| `vuln` | cmd/helix | Vulnerability scanning |
| `webhook` | cmd/helix | Webhook subscriptions |
| `doctor` | cmd/helix | Platform diagnostics |
| `status` | cmd/helix | Aggregated health check |
| `version` | cmd/helix | Version reporter |

## Agent Context

This project is managed by the coding-hermes autonomous pipeline.

- **Foreman:** helix-coding-hermes-foreman (every 30 min)
- **Quality gates:** GitReins Tier 1 (secrets, lint, build, test) + Tier 2 (LLM evaluation)
- **Agent skills:** coding-hermes, coding-hermes-cron, hilo-usage, gitreins
- **Task board:** `.coding-hermes/tasks.md`
- **Specs:** `specs/SPECIFICATION.md` is the source of truth for platform architecture

## Stack

- **Language:** Go 1.22
- **CLI:** cobra-style dispatcher in cmd/helix/main.go (no cobra dependency — hand-rolled)
- **Forge:** Forgejo REST API (pkg/forgejo)
- **Review:** Chimera multi-model deliberation (pkg/negotiate, pkg/review)
- **Quality:** GitReins (.gitreins/, v0.7.9)
- **Sandbox:** Bubblewrap (pkg/sandbox, bwrap at /usr/bin/bwrap)

## Conventions

- Every commit: `Co-authored-by: wojons <wojonstech@gmail.com>`
- Every commit: `Prompt: prompts/coding-hermes/v1.md`
- Use `gitreins guard` before every commit (never `--no-verify`)
- Add task to `.coding-hermes/tasks.md` BEFORE picking it
- Mark task `[~]` (in progress) → `[x]` (complete) with Result field