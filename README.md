# Helix

**Agent-first software development platform.** Humans and AI agents are equal participants in the code lifecycle — not tools, teammates.

## Thesis

Every existing dev tool treats AI as an assistant — a smarter autocomplete.
Helix treats agents as **first-class team members** with real Forgejo accounts,
SSH keys, scoped permissions, budgets, and earned trust. Agents open PRs.
Other agents review them. Agents can veto a merge with evidence. And agents
build reputation over time — just like humans.

**Cursor Origin:** Trust no single model. Require multi-model agreement,
adversarial review, traceable identity, prompt provenance, cost tracking, and
earned trust. Helix is not "vibe coding with guardrails" — it's a disciplined
software delivery system where every commit is attributed, reviewed, and
attested.

## Architecture

```
+---------------------------------------------------------+
|                    HUMAN INTERFACE                       |
|  Continue.dev / Cursor / CLI / Telegram (Hermes4Friends) |
|  Web UI (agent roster/PR review): hermes-canopy repo     |
+----------------------+----------------------------------+
                       |
+----------------------v----------------------------------+
|                  ORCHESTRATION LAYER                     |
|  Helix Dispatcher: Ralph Loop engine, task decomposition |
|  Hivemind: persistent memory, task scheduling            |
|  Kobayashi-Maru: stress testing, no-win validation       |
+----------------------+----------------------------------+
                       |
+----------------------v----------------------------------+
|                   EXECUTION LAYER                        |
|  OpenCode: isolated worktrees per agent/task             |
|  Ralph Loop: acquire lock -> worktree -> commit -> merge    |
|  Muster: auto-generated MCP tools for any API            |
+----------------------+----------------------------------+
                       |
+----------------------v----------------------------------+
|                    GIT FORGE (Forgejo)                   |
|  Repos, PRs, issues, actions, Pages, packages            |
|  GitReins hooks: block commits that fail quality gates   |
|  .promptfoo.yaml: eval as CI on every prompt change      |
+----------------------+----------------------------------+
                       |
+----------------------v----------------------------------+
|                  QUALITY & REVIEW LAYER                  |
|  Chimera: multi-model formation -> judge + audit PRs      |
|  GitReins evaluator: Tier 1 (static) + Tier 2 (agentic)  |
|  PromptFoo: prompt regression tests in CI                |
+----------------------+----------------------------------+
                       |
+----------------------v----------------------------------+
|               OBSERVABILITY & MEMORY                     |
|  LangFuse: traces, costs, prompt versions                |
|  DuckBrain: persistent agent memory (git-backed)         |
+---------------------------------------------------------+
```

> The shared-workspace web UI (agent roster, PR review dashboard) lives in the
> separate hermes-canopy project — Helix itself ships CLI-only (UI-001..004).

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

## Components (60 packages, 9 CLIs)

### Core Platform

| Component | Package | CLI | Description |
|-----------|---------|-----|-------------|
| Agent Identity | `pkg/identity` | `helix-identity` | Forgejo OAuth, SSH keys, PAT provisioning |
| Prompt Registry | `pkg/prompt` | `helix-prompt` | Prompt provenance, hash attestation, PromptFoo bridge |
| Agent Marketplace | `pkg/marketplace` | `helix-marketplace` | Agent discoverability, trust scoring, human ratings |
| Cost Estimator | `pkg/estimate` | `helix-estimate` | Token burn pre-flight, cache-aware pricing |
| PR Negotiation | `pkg/negotiate` | `helix-negotiate` | Agent debate protocol + Chimera tie-break |
| Dispatcher | `pkg/dispatcher` | `helix dispatch` | Ralph Loop engine, task decomposition, agent assignment |
| Sandbox | `pkg/sandbox` | `sandbox` | Bubblewrap-based agent isolation |
| Clarification Protocol | `pkg/dispatcher` | `helix dispatcher clarify` (deprecated — use `helix dispatch`) | Structured agent-human clarification with auto-resolve |

### Review & Quality Gates

| Component | Package | CLI | Description |
|-----------|---------|-----|-------------|
| Adversarial Review | `pkg/review` | `helix review` | Multi-model review pipeline, blast radius, dashboard, load balancing |
| Review Dismissal | `pkg/review` | `helix review dismiss` | Structured dismissal protocol with false-positive tracking |
| Merge Gate | `pkg/mergegate` | `helix mergegate` | Pre-receive hook: trust-tier, secrets, attestation enforcement |
| Co-Approval | `pkg/coapproval` | `helix coapproval` | Final merge approval gate with multi-model consensus |
| Force Merge Audit | `pkg/forcemerge` | `helix forcemerge` | Audit trail for every admin override merge |
| Audit Trail | `pkg/audit` | `helix audit` | 12-step audit trail checker per spec |
| Adversarial Scenarios | `pkg/adversarial` | `helix adversarial` | Encoded testing scenario pack for adversarial review |
| Consensus Thresholds | `pkg/negotiate` | `helix negotiate` | Risk-level quorum thresholds for Chimera tiebreak |

### Design & Planning

| Component | Package | CLI | Description |
|-----------|---------|-----|-------------|
| Ideation | `pkg/ideation` | `helix idea` | Offline idea capture, validation, prioritization, promotion |
| Spec Co-Authoring | `pkg/spec` | `helix spec` | Multi-agent spec creation with adversarial annotation, 12-dim completeness |
| ADR System | `pkg/adr` | `helix adr` | Architecture Decision Records with co-authoring and multi-model review |
| Design Review | `pkg/design` | `helix design` | Automated design review via adversarial agents (5 roles) |
| API Contracts | `pkg/contract` | `helix contract` | OpenAPI/protobuf generation, validation, breaking change detection |

### Orchestration & Pipeline

| Component | Package | CLI | Description |
|-----------|---------|-----|-------------|
| PR Coordinator | `pkg/coordinator` | `helix coordinator` | Full PR lifecycle orchestration composing all services |
| Pipeline State | `pkg/pipeline` | — | 12-step PR lifecycle state machine |
| Retry Logic | `pkg/retry` | — | Exponential backoff for cross-service calls |
| Platform Config | `pkg/config` | — | Unified platform configuration loading |

### Learning, Trust & Memory

| Component | Package | CLI | Description |
|-----------|---------|-----|-------------|
| Context Bus | `pkg/learning` | `helix notify` | Cross-agent notification and context sharing with domain pub/sub |
| Model Evaluation | `pkg/learning` | `helix models` | Production-outcome-based model rotation: FPR/IR tracking, auto-removal |
| Pattern Mining | `pkg/learning` | `helix incident patterns` | Incident pattern discovery across database: category, provider, time |
| Skill Transfer | `pkg/learning` | `helix learn` | Agent skill registry with trust-gated publication and outcome tracking |
| Trust Scoring | `pkg/trust` | `helix trust` | Graduated multi-dimensional trust with tier assignment and decay |
| Agent Memory | `pkg/memory` | — | DuckBrain and Hivemind memory schema types and interfaces |
| Incident DB | `pkg/incident` | `helix incident` | Incident learning database with attribution engine |

### Operations & Security

| Component | Package | CLI | Description |
|-----------|---------|-----|-------------|
| Release Signoff | — | `helix-release` | Dual human+agent signature with automated technical gate verification |
| Shadow Verification | `pkg/verify` | `helix-verify` | Shadow deployment, canary promotion, behavior diff, auto-rollback |
| Security Hardening | `pkg/security` | `helix security` | Security hardening checklist verifier |
| Vulnerability Scan | `pkg/vuln` | `helix vuln` | Dependency vulnerability scanner |
| Error Recovery | `pkg/recovery` | `helix recovery` | Structured error recovery procedures per component |
| Graceful Degradation | `pkg/degradation` | `helix degradation` | Platform graceful-degradation policies |
| Backup Strategy | `pkg/backup` | `helix backup` | Structured backup strategy data and validation |
| Health Metrics | `pkg/health` | `helix health` | Agent and platform health metrics |

### Infrastructure & Integration

| Component | Package | CLI | Description |
|-----------|---------|-----|-------------|
| API Contracts | `pkg/api` | — | Typed Go structs from spec API contracts |
| CI Generation | `pkg/ci` | `helix ci` | Forgejo Actions workflow generation and validation |
| Webhook Receiver | `pkg/webhook` | `helix webhook` | Forgejo webhook event receiver |
| Forgejo Client | `pkg/forgejo` | — | Forgejo REST API client: branches, repos, PRs |
| Integration Tests | `pkg/integration` | — | End-to-end integration test harnesses |
| Structured Logging | `pkg/log` | — | Dependency-free structured logging facility |
| CLI Banners | `pkg/banner` | — | ASCII art startup banners for Helix CLI |
| Deploy Targets | `pkg/deploy` | — | Platform deployment configuration

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

# Install all CLIs on your PATH (see "Install" below)
make install PREFIX=$(HOME)/.local    # no sudo; or: sudo make install
export PATH="$HOME/.local/bin:$PATH"

# Stand up Forgejo + Chimera + Helix (Docker)
make docker-up

# Register the example agent roster (includes `test-agent`) and the Forgejo
# admin credentials from docker-compose (defaults helio/changeme — override
# them in your shell profile, not in the repo)
mkdir -p ~/.helix
cp known-friends.example.json ~/.helix/known-friends.json
export FORGEJO_ADMIN_USER=helio FORGEJO_ADMIN_PASSWORD=changeme

# Use the unified CLI from any directory
helix identity provision test-agent    # requires Forgejo running — see `make docker-up`
helix estimate check wojons "Write a Go HTTP server" --model deepseek-v4-pro --provider deepseek
helix marketplace search --capability go --min-trust 50
```

> `helix <sub>` is a thin dispatcher: it delegates to a sibling binary
> (`helix-estimate`, `helix-identity`, ...). Make sure those binaries are
> installed or on your PATH — see [Install](#install) below. The individual
> binaries also work standalone (`helix-identity provision test-agent`).

## Install

The unified `helix` CLI delegates every subcommand to a sibling binary
(`helix-estimate`, `helix-identity`, `helix-marketplace`, `helix-negotiate`,
`helix-prompt`, `helix-release`, `helix-verify`, `sandbox`). When you run
`helix estimate …`, the CLI locates `helix-estimate` in this order:

1. the current directory (`./helix-estimate`),
2. `cmd/<name>/<name>` relative to the current directory,
3. your `PATH`.

`make build` compiles all binaries into the repository root, so `helix <sub>`
works from the repo root immediately — but from any other directory it only
works if the sibling binaries are on your `PATH`. To install everything in one
step:

```bash
# System-wide (requires sudo)
sudo make install          # installs all CLIs to /usr/local/bin

# Per-user (no sudo)
make install PREFIX=$(HOME)/.local
export PATH="$HOME/.local/bin:$PATH"   # add to your shell profile
```

`make install` builds and installs every CLI (`helix`, `helix-estimate`,
`helix-identity`, `helix-marketplace`, `helix-negotiate`, `helix-prompt`,
`helix-release`, `helix-verify`, `sandbox`) into `$(PREFIX)/bin` (default
`/usr/local/bin`; override the prefix with `PREFIX=...`). To install a single
binary by hand:

```bash
cd cmd/helix-estimate && go build -o helix-estimate && sudo mv helix-estimate /usr/local/bin/
```

If a sibling binary cannot be found, `helix` prints an install hint with the
exact commands to run.

## Agent Identity

Agents get real Forgejo accounts — not bot tokens. Each agent is declared in
`known-friends.json` as an entry in the top-level **`agents` map, keyed by
agent name** (a JSON object, not an array — a list-shaped file fails with
`cannot unmarshal array into map[string]*identity.Agent`). The repo ships
`known-friends.example.json` (a ready-to-use roster with a registered
`test-agent` entry) — copy it to `~/.helix/known-friends.json` (the CLI's
default location) to get started. See
[Getting Started §5](docs/GETTING-STARTED.md) for the full schema and an
example.

PAT creation requires **admin BasicAuth credentials** (`--admin-user` +
`--admin-password`, or `FORGEJO_ADMIN_USER` / `FORGEJO_ADMIN_PASSWORD`) —
Forgejo v1.21+ does not mint PATs for an admin token alone. Provisioning
without them creates the account + SSH key, then fails at token creation,
leaving a half-provisioned state; re-running with the credentials repairs it:

```bash
# Provision a new agent (positional name; agent must be registered in known-friends.json)
helix-identity provision test-agent \
  --forgejo-url http://localhost:3030 \
  --admin-user "${FORGEJO_ADMIN_USER}" \
  --admin-password "${FORGEJO_ADMIN_PASSWORD}"

# Verify
helix-identity status

# Deprovision (revokes PAT, deletes the SSH key server-side, archives local keys — never deletes the account)
helix-identity deprovision test-agent
```

## Documentation

- [Getting Started](docs/GETTING-STARTED.md) — onboarding guide
- [API Reference](docs/api/) — `pkg/forgejo`, `pkg/estimate`, `pkg/identity`

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

## Docker Compose

The full platform stack (Forgejo + Chimera + Helix) can be started with Docker Compose:

```bash
# Start all services
./scripts/up.sh

# Check status
docker exec -it helix-cli helix status

# Stop all services
./scripts/down.sh

# Stop and remove all data
./scripts/down.sh --clean
```

### Services

| Service | Port | Description |
|---------|------|-------------|
| Forgejo | 3030 (web), 2222 (SSH) | Self-hosted Git server for agent accounts |
| Chimera | 8765 | AI inference server (cost estimation, prompt attestation) |
| Helix | — | CLI container (use `docker exec -it helix-cli helix <command>`) |

### Configuration

Copy `.env.example` to `.env` and adjust values:

```bash
cp .env.example .env
# edit .env with your credentials
```

See `docker-compose.yml` for the full service definition and environment variables.

## License

MIT
