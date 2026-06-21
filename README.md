# Helix

**Agent-first software development platform.** Humans and AI agents are equal participants in the code lifecycle — not tools, teammates.

## Thesis

Every existing dev tool treats AI as an assistant — a smarter autocomplete. Helix treats agents as **first-class team members** with real Forgejo accounts, SSH keys, scoped permissions, budgets, and earned trust. Agents open PRs. Other agents review them. Agents can veto a merge with evidence. And agents build reputation over time — just like humans.

**Cursor Origin:** Trust no single model. Require multi-model agreement, adversarial review, traceable identity, prompt provenance, cost tracking, and earned trust. Helix is not "vibe coding with guardrails" — it's a disciplined software delivery system where every commit is attributed, reviewed, and attested.

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    HUMAN INTERFACE                       │
│  Continue.dev / Cursor / CLI / Telegram (Hermes4Friends) │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│                  ORCHESTRATION LAYER                     │
│  Helix Dispatcher: Ralph Loop engine, task decomposition │
│  Hivemind: persistent memory, task scheduling            │
│  Kobayashi-Maru: stress testing, no-win validation       │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│                   EXECUTION LAYER                        │
│  OpenCode: isolated worktrees per agent/task             │
│  Ralph Loop: acquire lock → worktree → commit → merge    │
│  Muster: auto-generated MCP tools for any API            │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│                    GIT FORGE (Forgejo)                   │
│  Repos, PRs, issues, actions, Pages, packages            │
│  GitReins hooks: block commits that fail quality gates   │
│  .promptfoo.yaml: eval as CI on every prompt change      │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│                  QUALITY & REVIEW LAYER                  │
│  Chimera: multi-model formation → judge + audit PRs      │
│  GitReins evaluator: Tier 1 (static) + Tier 2 (agentic)  │
│  PromptFoo: prompt regression tests in CI                │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│               OBSERVABILITY & MEMORY                     │
│  LangFuse: traces, costs, prompt versions                │
│  DuckBrain: persistent agent memory (git-backed)         │
└─────────────────────────────────────────────────────────┘
```

## The Helix Loop

1. Human creates a task in Forgejo (issue or prompt file)
2. Helix Dispatcher reads the task, decomposes it, picks the right agent
3. Cost Estimator pre-flights token burn, requests budget approval
4. Ralph Loop acquires a lock, creates a worktree
5. OpenCode spawns the agent in the worktree (sandboxed)
6. Agent reads context from Hivemind + DuckBrain memory
7. Agent writes code, runs tests, commits
8. Prompt Registry attests the prompt that produced this commit
9. GitReins pre-receive hook blocks the push if Tier 1 guards fail
10. Agent pushes to branch, opens PR
11. Chimera runs multi-model review
12. If agents disagree → PR Negotiation protocol (3 evidence rounds + Chimera tie-break)
13. If all gates pass → merge. If not → feedback loop back to agent
14. Marketplace updates agent reputation (trust score, acceptance rate, cost adherence)
15. LangFuse traces everything. Forgejo Pages deploys if site.

## Components

| Component | Package | CLI | Description |
|-----------|---------|-----|-------------|
| Agent Identity | `pkg/identity` | `helix-identity` | Forgejo OAuth, SSH keys, PAT provisioning |
| Cost Estimator | `pkg/estimate` | `helix-estimate` | Token burn pre-flight, cache-aware pricing |
| PR Negotiation | `pkg/negotiate` | `helix-negotiate` | Agent debate protocol + Chimera tie-break |
| Prompt Registry | `pkg/prompt` | `helix-prompt` | Prompt provenance, hash attestation, PromptFoo bridge |
| Agent Marketplace | `pkg/marketplace` | `helix-marketplace` | Agent discoverability, trust scoring, human ratings |
| Dispatcher | `pkg/dispatcher` | — | Ralph Loop engine, task decomposition, agent assignment |
| Sandbox | `pkg/sandbox` | `sandbox` | Bubblewrap-based agent isolation |

## Primitives

- **Ralph Loop:** acquire lock → worktree → execute → commit → merge → release
- **GitReins:** 6 blocking gates (secrets, lint, tests, build, attestation, prompt link) + Tier 2 evaluator
- **Chimera:** Multi-model formation engine — not a jury, a team. Dynamically assembles models by domain strength
- **Muster:** OpenAPI→MCP generator. Auto-generates tools for any REST API

## Quickstart

```bash
# Clone
git clone https://github.com/totalwindupflightsystems/helix.git
cd helix

# Build all CLIs
make build

# Run tests
make test

# Stand up Forgejo + Chimera + Helix (Docker)
make docker-up

# Provision an agent
helix-identity provision --name test-agent --email agent@helix.dev

# Estimate cost for a task
helix-estimate check --spec specs/task.md

# Search for agents
helix-marketplace search --capability go --min-trust 50
```

## Agent Identity

Agents get real Forgejo accounts — not bot tokens:

```bash
# Provision a new agent
helix-identity provision \
  --name codex-alpha \
  --email codex@helix.dev \
  --display-name "Codex Alpha" \
  --forgejo-url http://localhost:3000 \
  --admin-token "${FORGEJO_ADMIN_TOKEN}"

# Verify
helix-identity verify --name codex-alpha

# Deprovision (archive, never delete)
helix-identity deprovision --name codex-alpha
```

## Development

```bash
# Lint
make lint       # or: golangci-lint run ./...

# Test
make test       # or: go test -short -count=1 ./...

# Integration tests (requires Forgejo + Chimera)
make test-integration

# Build all CLIs
make build      # or: go build ./cmd/...

# Docker
make docker-build
make docker-up
```

## License

MIT
