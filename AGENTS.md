# AGENTS.md — Helix

Helix is an Agent-First Code Platform where humans and AI agents are equal
participants in the software development lifecycle.

## Project Structure

```
cmd/
  helix-identity/    Agent provisioning in Forgejo
  helix-estimate/    Pre-flight cost estimation
  helix-negotiate/   Agent PR debate + Chimera tie-break
  helix-prompt/      Prompt provenance + hash attestation
  helix-marketplace/ Agent discoverability + trust scoring
  sandbox/           Bubblewrap agent isolation
pkg/
  identity/          Forgejo OAuth, SSH keys, PAT management
  estimate/          Token burn estimation, cache-aware pricing
  negotiate/         Debate protocol, Chimera arbiter, audit logging
  prompt/            Prompt hashing, attestation, PromptFoo bridge
  marketplace/       Agent registry, trust scoring, human ratings
  dispatcher/        Ralph Loop engine, task decomposition, agent assignment
  sandbox/           Bubblewrap executor, cgroup isolation
  integration/       End-to-end integration tests
specs/               Feature specifications + platform docs
  ├── agent-identity.md         Agent provisioning in Forgejo
  ├── cost-estimator.md         Pre-flight token burn estimation
  ├── pr-negotiation.md         Agent debate protocol + Chimera arbiter
  ├── prompt-registry.md        Prompt provenance + hash attestation
  ├── agent-marketplace.md      Agent registry + trust scoring
  ├── sandbox.md                Bubblewrap agent isolation
  ├── trust-model.md            ★ NEW — graduated trust tiers, incident-linked decay
  ├── adversarial-review.md     ★ NEW — confirmation bias defense, multi-model gates
  ├── production-verification.md ★ NEW — shadow deployment, canary by trust tier
  ├── integrations.md           9 sub-project adapter interfaces
  ├── deployment.md             Docker Compose + CI/CD
  ├── config.md                 Platform configuration spec
  ├── build-order.md            Bootstrap and build sequence
  ├── cross-component-wiring.md Component discovery and interaction
  └── error-recovery.md         Recovery procedures per component
prompts/             Prompt files (linked in commits via GitReins)
```

## Tech Stack

- **Language:** Go 1.24 (cobra/viper CLIs)
- **Forge:** Forgejo REST API (agent identity)
- **Review:** Chimera multi-model deliberation (PR negotiation)
- **Quality:** GitReins (6 gates + Tier 2 evaluator)
- **Sandbox:** Bubblewrap (Linux namespace isolation)

## GitReins Quality Harness (MANDATORY)

This repo uses GitReins as its quality gate. Every commit runs 6 checks. If any
fail, the commit is BLOCKED.

### Gates

1. **Secrets scan** — API keys, tokens, passwords (BLOCKS)
2. **Lint** — golangci-lint (BLOCKS)
3. **Tests** — `go test -short -count=1 ./...` for changed packages (BLOCKS)
4. **Build** — `go build ./...` (BLOCKS)
5. **Commit attestation** — `Co-authored-by:` trailer required (BLOCKS)
6. **Prompt link** — `Prompt: prompts/<name>/v<N>.md` in body (BLOCKS)

### Quick check

```bash
make lint && make test && make build
```

### If guards fail

1. Read the output — it tells you exactly what failed
2. Fix the issues — never `--no-verify` for code changes
3. Re-commit

## Development

```bash
make build    # Build all CLIs
make test     # Run unit tests (fast)
make lint     # Run linter
make all      # lint + test + build

# Integration tests (require Forgejo + Chimera running)
make test-integration
```

## Commit Rules

- Every commit MUST include `Co-authored-by: wojons <wojonstech@gmail.com>`
- Every commit MUST include `Prompt: prompts/<name>/v<N>.md`
- Never commit secrets, tokens, or passwords
- Never use `--no-verify` for code changes
- Run `git pull --rebase` before pushing
