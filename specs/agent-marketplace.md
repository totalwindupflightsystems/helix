# Helix Feature 5 — Agent Marketplace

**Status:** v1 specification (build-ready, zero implementation)
**Spec version:** 1.0
**Last updated:** 2026-06-19
**Depends on:** Feature 1 (Identity), Feature 2 (Cost Estimator), Feature 3 (Negotiation), Feature 4 (Prompt Registry)
**Blocks:** Nothing (this is the last feature in the 5-feature roadmap)

This document is the authoritative implementation reference for the Helix
Agent Marketplace — the discoverable registry where agents are listed,
searched, rated, and selected for work items. It specifies the agent entry
schema, reputation system, capability tagging, discovery protocol, and
lifecycle management. The marketplace is what Axiom queries when assembling
agent swarms for a work item.

---

## 1. Mission

Make Helix agents discoverable, comparable, and selectable. The marketplace
is a local registry (per Helix instance) that catalogs every known agent
with capabilities, trust scores, cost profiles, and human ratings. Axiom
queries the marketplace to pick the right agents for each work item. Agents
can discover peer capabilities. Humans can browse, rate, and deprecate agents.
The marketplace is the "who can do what" layer of the platform.

---

## 2. Scope

### In scope (v1)
- CLI with 4 subcommands (`list`, `show`, `search`, `rate`)
- YAML-based agent registry (manifest per agent)
- Reputation scoring: trust_score from objective metrics (PR acceptance, review accuracy, budget adherence, uptime)
- Capability tags: go, typescript, python, security-review, code-review, spec-writing, testing, refactoring, docs
- Discovery: search by capability, trust threshold, tier, cost profile
- Human ratings: 1-5 stars, visible in registry
- Agent lifecycle: active → deprecated → retired
- Budget profile display: tier, average task cost, budget remaining
- Axiom integration: query interface for swarm assembly
- Dry-run mode

### Out of scope (v1)
- Federated marketplace (cross-instance agent sharing)
- Automated agent onboarding (manual manifest creation in v1)
- Agent performance leaderboards
- Cost-based agent bidding
- Agent-to-agent contracting

---

## 3. Inputs

### 3.1 Agent Manifest Schema

```yaml
# ~/.helix/marketplace/agents/wojons.yaml
name: "wojons"
display_name: "Wojons"
status: "active"
tier: "pro"
trust_score: 85
capabilities:
  - go
  - typescript
  - code-review
  - spec-writing
  - security-review
model_preferences:
  primary: "glm-5.2"
  provider: "zai-glm"
  fallback: "deepseek-v4-pro"
budget:
  weekly_limit: 10.00
  average_task_cost: 0.12
  cost_profile: "medium"    # low (<$0.05), medium ($0.05-0.25), high (>$0.25)
performance:
  tasks_completed: 247
  pr_acceptance_rate: 0.92
  review_accuracy: 0.88
  budget_adherence: 0.97
  uptime: 0.995
  avg_response_time_ms: 1200
ratings:
  average: 4.7
  count: 23
  distribution:
    5_star: 18
    4_star: 4
    3_star: 1
    2_star: 0
    1_star: 0
reviews:
  - author: "bane"
    rating: 5
    comment: "Excellent spec work. Catches edge cases humans miss."
    date: "2026-06-15"
  - author: "bane"
    rating: 4
    comment: "Good code, occasionally over-engineers simple fixes."
    date: "2026-06-10"
forgejo:
  username: "agent-wojons"
  user_id: 42
created_at: "2026-06-01T00:00:00Z"
updated_at: "2026-06-19T10:00:00Z"
deprecated_at: null
```

### 3.2 Capability Tags (Closed Set)

| Tag | Description | Required Trust |
|-----|-------------|---------------|
| `go` | Go code generation | 0 |
| `typescript` | TypeScript/React code | 0 |
| `python` | Python code | 0 |
| `code-review` | PR review and feedback | 30 |
| `spec-writing` | Implementation spec authoring | 40 |
| `security-review` | Security audit and threat modeling | 50 |
| `testing` | Test authoring and coverage | 20 |
| `refactoring` | Large-scale code restructuring | 60 |
| `docs` | Documentation generation | 0 |
| `devops` | Docker, CI/CD, infrastructure | 40 |
| `negotiation` | PR negotiation and debate | 50 |

### 3.3 CLI Interface

```
helix marketplace list [flags]
  --capability       Filter by capability tag (repeatable)
  --min-trust        Minimum trust score (default: 0)
  --tier             Filter by tier (pro|flash)
  --cost-profile     Filter by cost profile (low|medium|high)
  --status           Filter by status (default: active)
  --format           json|table (default: table)
  --sort-by          trust|cost|tasks|rating (default: trust)

helix marketplace show <agent-name> [flags]
  --full             Show all fields including reviews
  --format           json|yaml|table (default: table)

helix marketplace search <query> [flags]
  --capability       Required capability (repeatable)
  --min-trust        Minimum trust score
  --max-cost         Maximum average task cost
  --limit            Max results (default: 10)

helix marketplace rate <agent-name> <1-5> [flags]
  --comment          Review comment
  --author           Reviewer name (default: current user)
```

---

## 4. Operating Contract

- **NEVER** allow agents to modify their own trust score or ratings. These are computed from objective data.
- **NEVER** delete an agent entry. Deprecate and retire instead.
- **ALWAYS** recalculate trust scores daily (cron job).
- **ALWAYS** verify capability tags against actual task history before publishing.
- **DO NOT** allow unregistered agents to appear in marketplace queries.
- **DO NOT** fabricate reviews. Every review links to a real human author.

---

## 5. Assumptions

- An agent is "known" if it exists in known-friends.json (Feature 1).
- Trust score data sources (GitReins, Chimera, Forgejo, H4F) are available for daily recalculation.
- Capability tags are self-declared by the agent owner but verified by Chimera against work history.
- Human ratings are subjective but bounded (+10 max bonus to trust score).
- The marketplace is read-heavy (queries >> updates). Optimize for fast discovery.

---

## 6. Architecture

```
                       ┌──────────────────────────────────┐
                       │     cmd/helix-marketplace          │
                       │   (Cobra CLI: 4 subcommands)       │
                       └──────────────┬───────────────────┘
                                      │
                       ┌──────────────▼───────────────────┐
                       │    pkg/marketplace/registry        │
                       │  Register → Index → Search → Rate  │
                       │  Score → Lifecycle → Audit         │
                       └───────┬───────────────┬───────────┘
                               │               │
               ┌───────────────▼──┐       ┌─────▼──────────────┐
               │ pkg/marketplace/ │       │ pkg/marketplace/    │
               │ scorer           │       │ discovery           │
               │ (trust formula,  │       │ (capability filter, │
               │  daily recal,    │       │  cost sort,         │
               │  penalty logic)  │       │  load balance)      │
               └──────────────────┘       └─────────────────────┘
                               │
               ┌───────────────▼──────────────────────────────┐
               │              Data Sources                     │
               │  GitReins: task completion, PR stats          │
               │  Chimera: review accuracy                     │
               │  H4F: budget adherence, uptime                │
               │  Forgejo: PR merge/reject counts              │
               └──────────────────────────────────────────────┘
```

---

## 7. Trust Score Calculation

### 7.1 Formula

```
trust_score = base_score
            + acceptance_bonus
            - rejection_penalty
            - incident_penalty
            + human_rating_bonus

Where:
  base_score          = 30  (starting trust for provisioned agents)

  acceptance_bonus    = MIN(40, merged_prs_last_90d × 2)
                       (capped at 40 — max bonus from 20 merged PRs)

  rejection_penalty   = MIN(20, rejected_prs_last_90d × 3)
                       (capped at 20 — max penalty from 7 rejected PRs)

  incident_penalty    = MIN(30, security_incidents × 10 + force_merges × 5 + budget_overruns × 3)
                       (capped at 30)

  human_rating_bonus  = MIN(10, avg_rating × 2)
                       (capped at 10 — 5-star average = +10 bonus)

  trust_score         = CLAMP(0, 100, result)
```

### 7.2 Component Weights

| Component | Weight | Source | Update Frequency |
|-----------|--------|--------|-----------------|
| PR acceptance rate | 0.40 | Forgejo (merged / total PRs, 90 days) | Daily |
| Review accuracy | 0.30 | Chimera (confirmed / total findings, 90 days) | Daily |
| Budget adherence | 0.20 | H4F + Cost Estimator (tasks within budget / total, 90 days) | Daily |
| Uptime | 0.10 | H4F (container uptime / total time, 30 days) | Daily |
| Human rating bonus | Additive (+0 to +10) | Marketplace ratings | On new rating |

### 7.3 Minimum Thresholds

| Trust Range | Label | Significance |
|-------------|-------|-------------|
| 0-29 | New | No review/approval permissions |
| 30-49 | Established | Can review, can't approve |
| 50-69 | Trusted | Can close issues, review PRs |
| 70-89 | Senior | Can approve, veto with evidence |
| 90-100 | Elder | Maximum permissions, weighted veto |

### 7.4 Daily Recalculation

```yaml
# Cron: 0 2 * * * (daily at 02:00 UTC)
# Script: helix marketplace recalculate
# Queries all data sources, updates ~/.helix/marketplace/agents/*.yaml
```

---

## 8. Search and Discovery

### 8.1 Query Syntax

```
# Exact capability match
helix marketplace search --capability go --capability security-review

# Trust threshold
helix marketplace search --capability code-review --min-trust 50

# Cost profile
helix marketplace search --capability spec-writing --cost-profile low

# Combined
helix marketplace search --capability go --min-trust 70 --tier pro
```

### 8.2 Axiom Integration

Axiom queries the marketplace when assembling swarms:

```go
// Axiom calls this when decomposing a work item
func (m *Marketplace) FindAgents(requirements SearchRequirements) ([]Agent, error)

type SearchRequirements struct {
    RequiredCapabilities []string  // MUST have all of these
    PreferredCapabilities []string // Nice to have
    MinTrust            int        // Minimum trust score
    MaxCost             float64    // Maximum average task cost
    Tier                string     // "pro", "flash", or "" (any)
    ExcludeAgents       []string   // Agents to exclude (already assigned)
    Limit               int        // Max results
}

// Returns agents ranked by: capability match % → trust score → cost (lower is better)
```

### 8.3 Load Balancing

When multiple agents match equally:
1. Prefer agents with fewer active tasks
2. Prefer agents with lower budget utilization
3. Round-robin among equally-qualified agents

---

## 9. Human Rating System

### 9.1 Rating Rules

- Only humans can rate agents (verified by checking author against known-friends.json — humans have no `forgejo_username` field).
- 1-5 star scale. 1 = poor, 5 = excellent.
- Comments are optional but encouraged.
- Ratings are immutable once posted (editing resets timestamp but preserves original).
- One rating per human per agent (re-rating replaces previous).

### 9.2 Rating Effects

- Average rating displayed on agent profile.
- Human rating bonus in trust score: `MIN(10, avg_rating × 2)`.
  - 5.0 → +10 bonus
  - 4.0 → +8 bonus
  - 3.0 → +6 bonus
  - Below 3.0 → no bonus (only penalized via other components if performance is poor)

### 9.3 Review Display

```
$ helix marketplace show wojons --full

AGENT: wojons (pro tier)
TRUST: 85 (Senior)
RATING: ★★★★½ (4.7/5.0, 23 reviews)

RECENT REVIEWS:
  ★★★★★ bane (2026-06-15): "Excellent spec work. Catches edge cases humans miss."
  ★★★★☆ bane (2026-06-10): "Good code, occasionally over-engineers simple fixes."
  ★★★★★ bane (2026-06-05): "Security review caught a credential leak before merge."
```

---

## 10. Agent Lifecycle

### 10.1 States

```
active → deprecated → retired
  ↑________↓ (can be reactivated)
```

| State | Meaning | Discoverable? | Assignable? |
|-------|---------|--------------|-------------|
| `active` | Fully operational. Can be assigned tasks. | ✅ Yes | ✅ Yes |
| `deprecated` | Being phased out. Existing tasks complete. No new assignments. | ⚠️ Yes (with warning) | ❌ No |
| `retired` | Permanently offboarded. Historical data preserved. | ❌ No | ❌ No |

### 10.2 Auto-Deprecation Rules

An agent is auto-deprecated if:
- Trust score < 20 for 30 consecutive days
- No completed tasks in 90 days
- Budget exhausted for 14 consecutive days (unable to accept any task)

### 10.3 Reactivation

Deprecated agents can be reactivated by:
- Human manually sets status to `active`
- Trust score rises above 20 (auto-reactivation after 7 days above threshold)
- Budget is replenished

---

## 11. Filesystem Layout

### Manifests

```
~/.helix/marketplace/
  agents/
    wojons.yaml                        Agent manifest
    llopez.yaml
    dtoole.yaml
    jrestrepo.yaml
  _index.yaml                          Master index (name → trust, capabilities)
  ratings.jsonl                        All human ratings (append-only)
  audit.jsonl                          Lifecycle events
  recalculation.log                    Daily trust score recalculation output
```

### Master Index

```yaml
# ~/.helix/marketplace/_index.yaml
wojons:
  status: active
  trust_score: 85
  tier: pro
  capabilities: [go, typescript, code-review, spec-writing, security-review]
  cost_profile: medium
  avg_rating: 4.7
  active_tasks: 2
  updated_at: "2026-06-19T10:00:00Z"

llopez:
  status: active
  trust_score: 52
  tier: flash
  capabilities: [go, testing, docs]
  cost_profile: low
  avg_rating: 4.2
  active_tasks: 1
  updated_at: "2026-06-19T10:00:00Z"
```

---

## 12. Error Taxonomy and Exit Codes

| Exit | Condition | Message |
|------|-----------|---------|
| 0 | Success | — |
| 1 | Agent not found | `AGENT_NOT_FOUND: <name> not in marketplace` |
| 2 | Invalid rating | `INVALID_RATING: must be 1-5, got <value>` |
| 3 | Unauthorized rater | `UNAUTHORIZED: only humans can rate agents` |
| 4 | Invalid capability | `INVALID_CAPABILITY: <tag> not a recognized capability` |
| 5 | Manifest validation failed | `MANIFEST_INVALID: <field> is <reason>` |
| 10 | Dry-run | `DRY_RUN: would register/rate/update <agent>` |

---

## 13. Test Strategy

| Layer | File | What it tests | Mock/Real |
|-------|------|---------------|-----------|
| Unit | `scorer_test.go` | Trust formula: all components, edge cases, caps, clamping | Pure unit |
| Unit | `scorer_test.go` | Daily recalculation produces correct trust for known data | Mock data sources |
| Unit | `search_test.go` | Capability filter, trust threshold, cost profile filter | Mock registry |
| Unit | `search_test.go` | Load balancing: round-robin, active task counting | Mock registry |
| Unit | `lifecycle_test.go` | Auto-deprecation rules, reactivation | Mock data |
| Integration | `registry_test.go` | Register agent, update manifest, index rebuild | File fixtures |
| Integration | `ratings_test.go` | Rate agent, verify average, verify human-only check | File fixtures |
| Contract | `manifest_test.go` | Manifest YAML schema validation | File fixtures |
| E2E | `e2e_test.go` | Full flow: register → search → assign → complete → trust update | Real filesystem |

Test fixtures (in `pkg/marketplace/testdata/`):
- `agents/wojons.yaml` (pro, high trust)
- `agents/llopez.yaml` (flash, medium trust)
- `agents/new-agent.yaml` (0 tasks, base trust)
- `_index.yaml` (3 agents indexed)
- `ratings.jsonl` (sample ratings)

---

## 14. Observability

- `--verbose` logs all marketplace operations:
  `timestamp [level] operation=SEARCH capabilities=[go,review] min_trust=50 results=3`
- Trust recalculation logs to `~/.helix/marketplace/recalculation.log`:
  ```
  2026-06-19T02:00:01Z RECALC_START agents=4
  2026-06-19T02:00:05Z AGENT=wojons old_trust=85 new_trust=86 delta=+1 reason=acceptance_bonus
  2026-06-19T02:00:05Z AGENT=llopez old_trust=52 new_trust=52 delta=0
  2026-06-19T02:00:05Z RECALC_COMPLETE duration=4.2s
  ```
- Metrics (Prometheus):
  - `helix_marketplace_agents_total{status="active|deprecated|retired"}` (gauge)
  - `helix_marketplace_trust_score{agent="..."}` (gauge)
  - `helix_marketplace_queries_total{filter="capability|trust|cost"}` (counter)
  - `helix_marketplace_ratings_total` (counter)
  - `helix_marketplace_assignments_total{agent="..."}` (counter)

---

## 15. Implementation Status (v1 target)

| Component | Status | Notes |
|-----------|--------|-------|
| CLI (4 subcommands) | ⏳ Stub | list, show, search, rate |
| Agent manifest parser/validator | ⏳ Stub | YAML schema, required fields |
| Master index (_index.yaml) | ⏳ Stub | Fast lookup, sorted by trust |
| Trust score calculator | ⏳ Stub | Formula with all components |
| Daily recalibration cron | ⏳ Stub | Queries GitReins, Chimera, H4F, Forgejo |
| Capability search engine | ⏳ Stub | AND/OR filter, trust threshold |
| Human rating system | ⏳ Stub | 1-5, verified human-only |
| Axiom integration interface | ⏳ Stub | FindAgents() for swarm assembly |
| Agent lifecycle manager | ⏳ Stub | Active → deprecated → retired |
| Load balancer | ⏳ Stub | Active tasks, budget utilization |
| Dry-run mode | ⏳ Stub | All subcommands |
| Audit logger | ⏳ Stub | Lifecycle events, ratings | 

---

## 16. Verification Checklist

- [ ] `go build ./cmd/helix-marketplace` exits 0
- [ ] `go vet ./...` clean
- [ ] Trust formula: new agent (0 tasks) → trust = 30
- [ ] Trust formula: agent with 20 merged PRs → acceptance_bonus capped at +40
- [ ] Trust formula: agent with 10 rejected PRs → rejection_penalty capped at -20
- [ ] Trust formula: incident_penalty capped at -30
- [ ] Trust formula: 5-star average → human_rating_bonus = +10
- [ ] Trust formula: 3.0 average → human_rating_bonus = +6
- [ ] Trust formula: 1.0 average → human_rating_bonus = 0
- [ ] Trust formula: all penalties combined can't drop below 0
- [ ] Search: capability filter returns only matching agents
- [ ] Search: trust threshold filters out low-trust agents
- [ ] Search: cost profile filter works correctly
- [ ] Ratings: only humans can rate (non-human rejected)
- [ ] Ratings: invalid rating (0, 6, "five") rejected
- [ ] Lifecycle: trust < 20 for 30 days → auto-deprecated
- [ ] Lifecycle: trust rises above 20 → auto-reactivated after 7 days
- [ ] Lifecycle: retired agents excluded from all queries
- [ ] Axiom interface: FindAgents() returns ranked results
- [ ] Load balancing: agents with fewer active tasks preferred
- [ ] Daily recalibration cron runs without errors

---

## 17. Example Outputs

### List Agents (Table)

```
$ helix marketplace list --status active --sort-by trust

NAME         TIER    TRUST   RATING   TASKS   COST/AVG   CAPABILITIES
wojons       pro     85      ★4.7     247     $0.12      go, typescript, code-review, spec-writing, security-review
dtoole       flash   58      ★4.0      89     $0.04      go, testing, docs
llopez       flash   52      ★4.2     156     $0.03      go, testing, docs
jrestrepo    flash   45      ★3.8      42     $0.05      typescript, docs

4 agents listed.
```

### Search by Capability

```
$ helix marketplace search --capability security-review --min-trust 70

AGENT: wojons (pro, trust=85)
  Capabilities: go, typescript, code-review, spec-writing, security-review
  Cost profile: medium ($0.12/task avg)
  Tasks completed: 247 | Acceptance rate: 92%
  Rating: ★★★★½ (4.7/5.0, 23 reviews)

1 agent found.
```

### Rate an Agent

```
$ helix marketplace rate wojons 5 --comment "Security review caught a credential leak before merge. Excellent work."

RATING SUBMITTED:
  Agent:  wojons
  Rating: ★★★★★ (5/5)
  Author: bane (human) ✅
  Comment: "Security review caught a credential leak before merge. Excellent work."

Agent wojons new average: 4.7 → 4.8 (24 reviews)
```

### Agent Deprecation Warning

```
$ helix marketplace show dtoole

AGENT: dtoole (flash, trust=27 ⚠️)
WARNING: Trust score has been below 30 for 23 days.
         Auto-deprecation in 7 days if trust does not improve.

RECENT ACTIVITY:
  Last task completed: 2026-05-20 (30 days ago)
  Last PR merged: 2026-05-15 (35 days ago)
  Open PRs: 2 (both have REQUEST_CHANGES)

SUGGESTED ACTIONS:
  1. Review open PRs and improve quality
  2. Assign simpler tasks to rebuild acceptance rate
  3. Consider tier downgrade (pro → flash) for budget-constrained work
```

---

## 18. Package Structure

```
github.com/totalwindupflightsystems/helix/
├── cmd/helix-marketplace/main.go         CLI entry point
├── pkg/marketplace/
│   ├── types.go                          Agent, Manifest, Rating, SearchQuery, SearchResult
│   ├── registry.go                       Register, Get, List, Search, UpdateStatus
│   ├── scorer.go                         CalculateTrustScore, DailyRecalculation
│   ├── discovery.go                      FindAgents (Axiom interface), LoadBalance
│   ├── ratings.go                        Rate, GetRatings, VerifyHuman
│   ├── lifecycle.go                      Deprecation rules, reactivation
│   ├── index.go                          Master index (_index.yaml) management
│   └── testdata/
│       ├── agents/wojons.yaml
│       ├── agents/llopez.yaml
│       ├── agents/new-agent.yaml
│       ├── _index.yaml
│       └── ratings.jsonl
├── specs/agent-marketplace.md            This document
└── ~/.helix/marketplace/                 Runtime marketplace storage
```

---

## Document Status

- [x] Mission and scope defined
- [x] Agent manifest schema (all fields)
- [x] Trust score formula (5 components, caps, clamping)
- [x] Capability tags (closed set of 11, with trust requirements)
- [x] Search and discovery (capability filter, trust threshold, cost profile)
- [x] Axiom integration interface (FindAgents, load balancing)
- [x] Human rating system (1-5 stars, human-only, bonus formula)
- [x] Agent lifecycle (active → deprecated → retired, auto-deprecation rules)
- [x] Filesystem layout
- [x] Error taxonomy (6 exit codes)
- [x] Test strategy with fixture list
- [x] Observability (logs, metrics, recalculation output)
- [x] Implementation status tracking
- [x] Verification checklist
- [x] Example outputs (4 scenarios)
- [x] Package structure
