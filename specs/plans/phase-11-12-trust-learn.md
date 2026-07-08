# Helix Resolution Plans — Phase 11–12: Trust Loops + Learning

**Status:** v1.0 — Build-ready resolution plan
**Spec version:** 1.0
**Last updated:** 2026-07-07
**Depends on:** Phase 1–10 (full interaction map), pkg/trust, pkg/marketplace, pkg/incident, DuckBrain
**Blocks:** Nothing downstream — these are the feedback loops that close the system

---

## Overview

Phases 11–12 close the Helix feedback loop. Phase 11 (Trust & Reputation Feedback) ensures agent trust is continuously recalculated from objective outcomes, tiers auto-adjust, and human feedback feeds the system without dominating it. Phase 12 (Learning & Knowledge Transfer) mines patterns from incidents and trusted agents, transfers skills across agents, shares context between concurrent tasks, and evaluates models against production outcomes.

All seven interaction points (11.1–11.3, 12.1–12.4) are specified below with:
- **Resolution** — what needs to be built or wired
- **Existing foundation** — what's already implemented and can be referenced
- **Implementation steps** — concrete build tasks
- **Test strategy** — how to verify correctness
- **Integration points** — how it connects to other components

---

## Phase 11: Trust & Reputation Feedback

### 11.1 — Trust Score Recalculation

**Interaction Type:** GATE  
**Participants:** System ↔ Agent  
**Trigger:** After every merge, review, or incident  
**Purpose:** Deterministic trust scoring from append-only ledger, replay-verifiable by any observer

#### Resolution

The scoring engine (`pkg/trust/tiers.go`) and ledger (`pkg/trust/ledger.go`) already implement the six-dimension weighted formula and append-only JSONL storage. What remains is wiring the **recalculation triggers** into every event producer and adding a **daily recalculation cron** for inactivity decay.

#### Existing Foundation

| Component | File | Status |
|-----------|------|--------|
| Six-dimension scoring | `pkg/trust/tiers.go` — `Calculate()`, `DimensionScores`, `DimensionWeights` | ✅ Built, tested |
| Incident time-decay | `pkg/trust/tiers.go` — `IncidentAttributionWeight()`, `ApplyIncidentPenalty()` | ✅ Built, tested |
| Inactivity decay | `pkg/trust/tiers.go` — `ApplyInactivityDecay()`, 0.05/week after 30d grace | ✅ Built, tested |
| Tenure scoring | `pkg/trust/tiers.go` — `TenureScore()`, log-scaled to 730 days | ✅ Built, tested |
| Merge success scoring | `pkg/trust/tiers.go` — `MergeSuccessScore()` | ✅ Built, tested |
| Incident track scoring | `pkg/trust/tiers.go` — `IncidentTrackScore()` | ✅ Built, tested |
| Ledger (JSONL append) | `pkg/trust/ledger.go` — `Ledger`, `Append()`, `Replay()` | ✅ Built, tested |
| Snapshot query | `pkg/trust/snapshot.go` — `GetSnapshot()`, `ScoreBreakdown`, `ScoreTrend` | ✅ Built, tested |
| Incident → trust bridge | `pkg/trust/integration.go` — `IncidentBridge`, `ProcessResult()` | ✅ Built, tested |
| DuckBrain trust storage | `specs/trust-model.md` §The Trust Ledger — events stored in DuckBrain as JSONL | ✅ Specified |

#### Implementation Steps

1. **`RecalculationScheduler`** — New file: `pkg/trust/scheduler.go`
   - Runs daily at 02:00 UTC (cron or internal ticker)
   - Replays the full ledger via `Replay(path)`
   - For each agent, computes current score with `ReplayToScore()`
   - Applies inactivity decay since last event
   - Produces a `RecalculationReport`: agent → (old_score, new_score, delta, reason)
   - Logs to `~/.helix/trust/recalculation.log`

2. **Event-Producer Wiring**
   - **Merge gate** (`cmd/helix/mergegate.go`): After successful merge → `Ledger.Append(EventMergeSuccess)`
   - **Review completion** (`cmd/helix/review.go`): After adversarial review → `Ledger.Append(EventReviewConsensus)`
   - **Incident closure** (`cmd/helix/incident.go`): After incident resolution → `IncidentBridge.ProcessResult()`
   - **Human rating** (`cmd/helix-marketplace/`): After human rates agent → `Ledger.Append(EventHumanRating)`

3. **`RecalculationTrigger` interface**
   ```go
   type RecalculationTrigger interface {
       OnMergeSuccess(agentID string, prURL string, evidence []string) error
       OnReviewConsensus(agentID string, score float64, evidence []string) error
       OnIncident(agentID string, attributionWeight float64, evidence []string) error
       OnHumanRating(agentID string, rating float64) error
   }
   ```

4. **Trust ledger compaction** — `pkg/trust/compaction.go` already exists
   - Daily cron triggers compaction after recalculation
   - Removes tombstoned entries, converts older partitions to Parquet via DuckBrain

5. **Observability**
   - Prometheus gauge: `helix_trust_score{agent="..."}`
   - Prometheus counter: `helix_trust_recalculations_total`
   - Prometheus counter: `helix_trust_events_total{type="merge|review|incident|rating"}`

#### Test Strategy

| Layer | What to test | Method |
|-------|-------------|--------|
| Unit | Recalculation produces correct score for known ledger | `scheduler_test.go` — feed known events, assert score |
| Unit | Inactivity decay applied correctly after recalculation | Create events with old timestamps, run recalculation, assert decay |
| Unit | Event producers append correct event types | `mergegate_test.go`, `review_test.go` — check ledger after operation |
| Integration | Full cycle: merge → review → incident → recalculation → score update | End-to-end test with temp ledger file |
| Replay | Deterministic replay: two observers replay same ledger → same score | `ReplayToScore()` tested in `ledger_test.go` |

#### Integration Points

```
Merge Gate ──→ Ledger.Append(merge_success) ──→ trust ledger (DuckBrain)
Review Engine ──→ Ledger.Append(review_consensus) ──→ trust ledger
Incident Engine ──→ IncidentBridge.ProcessResult() ──→ trust ledger
Marketplace Rating ──→ Ledger.Append(human_rating) ──→ trust ledger
Daily Cron ──→ RecalculationScheduler.Run() ──→ all agent scores updated
```

---

### 11.2 — Tier Promotion/Demotion

**Interaction Type:** GATE  
**Participants:** System ↔ Agent  
**Trigger:** Agent crosses tier threshold  
**Purpose:** Automatic tier transitions — permissions, cost caps, and review requirements change without human intervention

#### Resolution

The promotion engine (`pkg/trust/promotion.go`) and demotion logic (`pkg/trust/tiers.go`) already implement the full ruleset. What remains is a **`TierController`** that watches score changes and triggers the tier lifecycle automatically, plus **permission enforcement** that reacts to tier transitions.

#### Existing Foundation

| Component | File | Status |
|-----------|------|--------|
| Tier thresholds (4 tiers) | `pkg/trust/tiers.go` — `thresholdsFor()`, `DetermineTier()` | ✅ Built, tested |
| Promotion evaluation | `pkg/trust/promotion.go` — `EvaluatePromotion()`, `ShouldPromote()`, `PromoteTo()` | ✅ Built, tested |
| Full cycle evaluation | `pkg/trust/promotion.go` — `EvaluateFullTierCycle()` | ✅ Built, tested |
| Demotion check | `pkg/trust/tiers.go` — `ShouldDemote()`, `DemoteTo()` | ✅ Built, tested |
| Tier ranking | `pkg/trust/promotion.go` — `TierRank()`, `IsPromotion()`, `IsDemotion()` | ✅ Built, tested |
| Agent metrics struct | `pkg/trust/promotion.go` — `AgentMetrics` | ✅ Built |
| Promotion criteria (all 5) | `pkg/trust/promotion.go` — `entryRequirementsFor()` | ✅ Built |
| Tier privilege definitions | `specs/trust-model.md` §Trust Tiers | ✅ Specified |

#### Implementation Steps

1. **`TierController`** — New file: `pkg/trust/controller.go`
   - Runs after every score recalculation
   - Computes `AgentMetrics` for each agent: trust score, total merges, incidents in 180d, days active, PRs reviewed, current tier
   - Calls `EvaluateFullTierCycle(metrics)` — checks promotion first, then demotion risk
   - On promotion: emits `EventTierChange` to ledger, updates agent metadata, triggers permission expansion
   - On demotion (7 consecutive days below threshold): emits `EventDemotion` to ledger, triggers permission contraction
   - Never allows human promotion — only outcomes-based auto-promotion

2. **Permission Enforcer** — New file: `pkg/trust/permissions.go`
   - Maps tier → Forgejo roles, cost caps, review requirements
   - On tier change: updates Forgejo user permissions via API, updates marketplace manifest, updates cost estimator caps
   - Provisional: `$5/job` cap, adversarial review required, sandbox isolation
   - Observed: `$25/job` cap, adversarial only for contract changes
   - Trusted: `$100/job` cap, can review other agents, single-reviewer signoff
   - Veteran: no cap, can certify merges, incident immunity (1 allowed)

3. **Consecutive-Days Tracker** — New file: `pkg/trust/days_tracker.go`
   - Maintains a counter of consecutive days an agent's score has been below their tier threshold
   - Stored in DuckBrain as a simple key-value record per agent
   - Reset to 0 when score rises above threshold
   - Incremented daily during recalculation
   - Used by `ShouldDemote()` — triggers at 7 consecutive days

4. **Tier Transition Audit**
   - Every tier change is logged to `~/.helix/trust/tier_transitions.jsonl`
   - Includes: timestamp, agent, from_tier, to_tier, reason, criteria_met/blocked
   - DuckBrain stores the full audit trail
   - CLI: `helix trust history --agent <name>` already implemented

#### Test Strategy

| Layer | What to test | Method |
|-------|-------------|--------|
| Unit | Promotion triggers when all 5 criteria met | `controller_test.go` — set metrics above threshold, verify promotion |
| Unit | Promotion blocked by single criterion | Set 4/5 criteria, verify NOT promoted |
| Unit | Demotion triggers after 7 consecutive days below | `controller_test.go` — simulate 7 days below, verify demotion |
| Unit | Demotion NOT triggered at 6 days | 6 days below threshold, verify no demotion |
| Unit | Tier permissions map correctly | `permissions_test.go` — verify each tier's caps and requirements |
| Integration | Full cycle: score drops → 7 days → demotion → permissions contract | End-to-end with mock Forgejo API |
| Integration | Score rises above threshold → promotion → permissions expand | End-to-end with mock marketplace |

#### Integration Points

```
RecalculationScheduler ──→ TierController.Evaluate() ──→ Promotion/Demotion Decision
                                                             │
                          ┌──────────────────────────────────┤
                          ▼                                  ▼
               PermissionEnforcer.Apply()          Ledger.Append(tier_change)
                          │
                          ├──→ Forgejo API (role update)
                          ├──→ Marketplace manifest (tier field)
                          └──→ Cost Estimator (cap update)
```

#### Anti-Patterns Explicitly Avoided

1. **Human promotion bypass** — No CLI command, API, or admin action can promote an agent. Promotions are purely algorithmic.
2. **Single-incident destruction** — Veterans have 1-incident immunity. Demotion requires sustained score decay (7 days).
3. **Binary trust toggle** — Trust is continuous (0.0–1.0) and multi-dimensional. Tiers are earned thresholds, not toggles.
4. **"Trust the model" scoring** — Tier is based on outcomes (merges, incidents, reviews), never on which LLM the agent uses.

---

### 11.3 — Human Feedback Integration

**Interaction Type:** COLLABORATION  
**Participants:** Human ↔ System  
**Trigger:** Human rates agent's work (code quality, review helpfulness, communication)  
**Purpose:** Structured rating interface that feeds trust score at low weight (10%) to prevent popularity contests

#### Resolution

The marketplace spec (`specs/agent-marketplace.md`) defines the rating system (1-5 stars, human-only, one rating per human per agent). What remains is building the **structured rating API**, wiring it into the **trust score recalculation**, and ensuring the **10% weight is enforced** so that human ratings inform but do not dominate trust.

#### Existing Foundation

| Component | File | Status |
|-----------|------|--------|
| Marketplace rating schema | `specs/agent-marketplace.md` §9 — Human Rating System | ✅ Specified |
| Rating rules (human-only, 1-5, immutable) | `specs/agent-marketplace.md` §9.1 | ✅ Specified |
| Rating effects on trust (+0 to +10 bonus) | `specs/agent-marketplace.md` §9.2 | ✅ Specified |
| Trust ledger human_rating event | `pkg/trust/ledger.go` — `EventHumanRating` | ✅ Event type defined |
| Human feedback dimension (10%) | `pkg/trust/tiers.go` — `DimensionWeights["human_feedback"] = 0.10` | ✅ Weight defined |
| Human feedback dimension in breakdown | `pkg/trust/snapshot.go` — `estimateHumanFeedback()` | ✅ Estimation built |

#### Implementation Steps

1. **Structured Rating CLI** — Extend `cmd/helix-marketplace/`
   ```bash
   helix marketplace rate <agent-name> <1-5> \
     --dimension code-quality=<1-5> \
     --dimension review-helpfulness=<1-5> \
     --dimension communication=<1-5> \
     --comment "Detailed review text"
   ```
   - Multi-dimensional rating: overall (1-5) + per-dimension scores
   - Only humans can rate (verified against `known-friends.json` — humans have no `forgejo_username`)
   - One rating per human per agent (re-rating replaces previous)
   - Ratings are immutable once posted (editing preserves original timestamp)

2. **Rating Store** — New file: `pkg/marketplace/ratings.go`
   - Stores ratings in DuckBrain: key = `/marketplace/ratings/{agent_id}/{human_id}`
   - Append-only JSONL at `~/.helix/marketplace/ratings.jsonl`
   - Computes aggregate: average, count, distribution (1-5 star histogram)
   - Updates agent manifest `ratings` section on each new rating

3. **Trust Score Wiring**
   - On each rating, append `EventHumanRating` to trust ledger
   - `EventData.ScoreAfter` = normalized human feedback score (0.0–1.0)
   - Trust recalculation applies human_feedback dimension at 10% weight
   - Human rating bonus is bounded: max contribution is 10% of total score
   - Formula: `human_feedback_contribution = 0.10 × normalized_rating`

4. **Dismissal Tracking** (from Phase 7.2 — Human-Agent Disagreement)
   - When human dismisses agent's review finding, record dismissal in DuckBrain
   - If pattern of dismissals emerges (human frequently overrides), reduce weight of that human's ratings
   - Dismissal feeds false-positive tracker for the agent
   - Agent trust adjusted if pattern of bad reviews emerges

5. **Rating Display**
   ```bash
   $ helix marketplace show wojons --full
   AGENT: wojons (Veteran)
   TRUST: 0.87
   RATING: ★★★★½ (4.7/5.0, 23 reviews)
   DIMENSIONS:
     Code Quality:        ★★★★★ (4.9)
     Review Helpfulness:  ★★★★☆ (4.4)
     Communication:       ★★★★★ (4.8)
   ```

#### Test Strategy

| Layer | What to test | Method |
|-------|-------------|--------|
| Unit | Human-only rating enforcement | Attempt agent rating → `UNAUTHORIZED` |
| Unit | Rating bounds (1-5) | Attempt 0 or 6 → `INVALID_RATING` |
| Unit | Re-rating replaces previous | Rate twice, verify only latest stored |
| Unit | Aggregate calculation (avg, distribution) | Seed known ratings, verify math |
| Unit | Trust score dimension: human feedback at 10% | Verify max human contribution ≤ 0.10 |
| Integration | Full flow: human rates → trust ledger updated → recalculation reflects rating | End-to-end with temp ledger |
| Integration | Dismissal tracking → false-positive detection | Multiple dismissals → trust penalty |

#### Integration Points

```
helix marketplace rate ──→ ratings.jsonl (DuckBrain)
                       ──→ trust ledger (EventHumanRating)
                       ──→ RecalculationScheduler (next cycle picks it up)
Human dismissal ──→ DuckBrain dismissal log
                ──→ false-positive tracker
                ──→ trust ledger (potential penalty if pattern emerges)
```

---

## Phase 12: Learning & Knowledge Transfer

### 12.1 — Pattern Discovery

**Interaction Type:** LEARNING  
**Participants:** Agent ↔ System  
**Trigger:** Agent analyzes incident database, trust ledger, and codebase  
**Purpose:** Discover systemic patterns that become annotated knowledge for future reviews

#### Resolution

The incident learning database (`pkg/incident/learning.go`) already implements pattern storage and similarity-based retrieval. What remains is building the **`PatternMiner`** that actively analyzes cross-system data to discover patterns, and wiring those patterns into the **review context feed** so future reviews reference relevant past incidents.

#### Existing Foundation

| Component | File | Status |
|-----------|------|--------|
| Incident pattern storage | `pkg/incident/learning.go` — `LearningDatabase`, `IncidentPattern` | ✅ Built, tested |
| Similarity scoring | `pkg/incident/learning.go` — `scoreSimilarity()` (category 40%, keyword 40%, change type 10%, severity 10%) | ✅ Built, tested |
| Review context feed | `pkg/incident/learning.go` — `FeedReviewContext()` | ✅ Built, tested |
| File categorization | `pkg/incident/learning.go` — `CategorizeFile()`, `CategorizeFiles()` | ✅ Built, tested |
| Keyword extraction | `pkg/incident/learning.go` — `mergeKeywords()`, `tokenize()` | ✅ Built |
| DuckBrain memory | `pkg/memory/` — MemoryEntry schema, persistence bridge | ✅ Built |

#### Implementation Steps

1. **`PatternMiner`** — New file: `pkg/learning/miner.go`
   - Runs periodically (daily cron or on-demand)
   - Queries three data sources:
     - **Incident database** (`~/.helix/incidents.jsonl`): all resolved incidents with categories, keywords
     - **Trust ledger** (`~/.helix/trust/ledger.jsonl`): agent scores, incident penalties, tier changes
     - **Codebase** (Forgejo API): file categorization, change frequency, agent assignments
   - Discovers patterns:
     - *Category clustering*: "auth bugs cluster in session refresh files" (high-incident files in CategoryAuth)
     - *Agent-provider correlation*: "agents using provider X have 2.3× the incident rate on database migrations"
     - *Change-type risk*: "migration changes have 3× the incident rate of new features"
     - *Time-based patterns*: "Friday deployments have 1.8× the incident rate"
     - *Review gap patterns*: "incidents where reviewer had < 50% consensus with author → 4× more likely to fail"
   - Each discovered pattern becomes an `IncidentPattern` with high-confidence annotations
   - Patterns stored in DuckBrain: key = `/learning/patterns/{pattern_id}`

2. **Pattern Confidence Scoring**
   - Each pattern has a confidence score (0.0–1.0) based on:
     - Sample size: more incidents → higher confidence
     - Statistical significance: p-value < 0.05 required
     - Recency: newer data weighted higher
   - Patterns below 0.6 confidence are stored as "hypotheses" (not surfaced in review context)
   - Patterns above 0.8 confidence become "established knowledge" (always surfaced)

3. **Review Context Integration**
   - `FeedReviewContext()` already surfaces past incidents for a PR
   - Extend it to also surface discovered patterns matching the PR's categories/keywords
   - Example output for a PR touching `session.go`:
     ```
     ⚠️ PATTERN ALERT: "auth bugs cluster in session refresh" (confidence: 0.87)
        → 12 incidents in this file category last 90 days
        → Common root cause: token expiry edge case at boundary
        → Review checklist: verify token refresh handles clock skew, concurrent refresh, and 401 retry loops
     ```

4. **Pattern CLI**
   ```bash
   helix learn patterns list [--category auth] [--min-confidence 0.7]
   helix learn patterns show <pattern-id>
   helix learn patterns discover  # Trigger manual discovery run
   ```

5. **DuckBrain Integration**
   - Patterns stored with domain=`concept`, key=`/learning/patterns/{id}`
   - Embedding text includes pattern description + keywords for semantic search
   - Agents can query: `mcp_duckbrain_recall(query="auth session bugs", domain="concept")`
   - Cross-referenced with incidents via evidence links

#### Test Strategy

| Layer | What to test | Method |
|-------|-------------|--------|
| Unit | Pattern discovery from known incident data | Seed incidents with known pattern → verify miner discovers it |
| Unit | Confidence scoring | Test edge cases: 1 incident → low confidence, 50 incidents → high confidence |
| Unit | Statistical significance filter | Feed noise data → verify no false patterns surfaced |
| Integration | Review context includes discovered patterns | Submit PR with matching categories → verify patterns in context |
| Integration | DuckBrain storage and recall | Store pattern → semantic search finds it |

#### Integration Points

```
Daily Cron ──→ PatternMiner.Discover()
                  │
                  ├──→ Incident DB (load all resolved incidents)
                  ├──→ Trust Ledger (load all events)
                  └──→ Forgejo API (file categories, agent assignments)
                  │
                  ▼
            Discovered Patterns → DuckBrain (/learning/patterns/*)
                  │
                  ▼
            Review Context Feed (surfaced during PR review)
```

---

### 12.2 — Agent Skill Transfer

**Interaction Type:** LEARNING  
**Participants:** Agent ↔ Agent  
**Trigger:** Agent excels at a domain → packages approach as a reusable skill  
**Purpose:** Marketplace-published skills that other agents can load, with effectiveness tracking

#### Resolution

The marketplace spec (`specs/agent-marketplace.md`) defines capability tags and trust scoring. This interaction extends it with a **`SkillRegistry`** where high-trust agents can publish reusable patterns as skills, and other agents load those skills during context assembly. Skill effectiveness is tracked and ineffective skills lose trust weighting.

#### Existing Foundation

| Component | File | Status |
|-----------|------|--------|
| Marketplace capability tags | `specs/agent-marketplace.md` §3.2 — 11 closed-set tags | ✅ Specified |
| Trust scoring for capability gating | `specs/agent-marketplace.md` §3.2 — Required Trust column | ✅ Specified |
| Agent manifest (capabilities, trust, ratings) | `specs/agent-marketplace.md` §3.1 — YAML schema | ✅ Specified |
| DuckBrain memory persistence | `pkg/memory/` — MemoryEntry, domain/namespace | ✅ Built |
| Context packaging (Phase 3.3) | `specs/interaction-map.md` §3.3 — budget-constrained context | ✅ Specified |

#### Implementation Steps

1. **`SkillRegistry`** — New file: `pkg/learning/skills.go`
   - Skills are versioned packages stored in DuckBrain
   - Schema:
     ```go
     type Skill struct {
         ID            string            // unique identifier
         Name          string            // human-readable
         Version       string            // semver
         AuthorAgentID string            // agent that created it
         Domain        string            // go, typescript, python, security-review, etc.
         Description   string            // what problem it solves
         Prompt        string            // the reusable prompt/approach
         EvidenceTags  []string          // links to successful PRs using this skill
         TrustWeight   float64           // effectiveness score (0.0–1.0)
         UsageCount    int               // times loaded by other agents
         SuccessRate   float64           // % of uses that led to successful merges
         CreatedAt     time.Time
         UpdatedAt     time.Time
     }
     ```
   - Storage: DuckBrain key = `/skills/{domain}/{skill_id}`, domain = `concept`

2. **Skill Publishing**
   - Only agents at Trusted tier (≥0.65) or above can publish skills
   - Publisher must have demonstrated expertise in the domain (≥5 successful merges in that capability tag)
   - Skill includes: prompt template, evidence links to PRs where approach was used, test strategy
   - Chimera validates skills: adversarial model checks for safety, correctness, and applicability
   - Published skills appear in marketplace: `helix marketplace search --skill auth-patterns`

3. **Skill Loading**
   - During context assembly (Phase 3.3), agents query: "what skills exist for my assigned domain?"
   - `SkillLoader` fetches top-N skills by trust_weight for the domain
   - Skills are injected into agent's context package (budget-constrained — skills cost tokens)
   - Agent can request more skills; each expansion costs tokens

4. **Effectiveness Tracking**
   - After each merge, track which skills were loaded
   - If merge succeeds: skill.success_count++, skill.trust_weight += 0.01 (capped at 1.0)
   - If merge fails/incident: skill.failure_count++, skill.trust_weight -= 0.05
   - Skills below trust_weight 0.3 are auto-deprecated
   - Usage metrics visible in marketplace: `helix marketplace show --skill auth-patterns`

5. **Skill CLI**
   ```bash
   helix learn skills list [--domain go] [--min-trust 0.7]
   helix learn skills publish --name "db-migration-safety" --domain database --prompt-file ./skill.md
   helix learn skills deprecate <skill-id> --reason "superseded by v2"
   helix learn skills show <skill-id> --include-evidence
   ```

#### Test Strategy

| Layer | What to test | Method |
|-------|-------------|--------|
| Unit | Skill publishing gated by trust tier | Attempt publish as Provisional → rejected |
| Unit | Skill publishing gated by domain expertise | Agent with 0 DB merges can't publish DB skill |
| Unit | Trust weight increases on successful use | Simulate merge success → weight +0.01 |
| Unit | Trust weight decreases on failure | Simulate incident → weight -0.05 |
| Unit | Auto-deprecation at trust_weight < 0.3 | Simulate decay → verify deprecated status |
| Integration | Full cycle: publish → load → use → succeed → weight update | End-to-end with DuckBrain |
| Integration | Skill context injection respects budget | Load skills until budget exhausted → verify cap |

#### Integration Points

```
Trusted/Veteran Agent ──→ SkillRegistry.Publish() ──→ DuckBrain (/skills/{domain}/{id})
                                                      ──→ Chimera validation
                                                      ──→ Marketplace index

Context Assembler ──→ SkillLoader.Load(domain, budget) ──→ DuckBrain query
                    ──→ Injects skills into context package

Merge/Incident ──→ EffectivenessTracker.Record(skill_id, outcome)
               ──→ Skill.trust_weight updated in DuckBrain
```

---

### 12.3 — Cross-Agent Context Sharing

**Interaction Type:** COLLABORATION  
**Participants:** Agent ↔ Agent  
**Trigger:** Agent discovers something relevant to another agent's active task  
**Purpose:** Agent-to-agent notification bus for structured findings with evidence links

#### Resolution

Build a **`ContextBus`** that allows agents to publish findings and subscribe to domains. When Agent A working on auth discovers a pattern relevant to Agent B working on session management, the finding is shared with evidence links. Context is budget-tracked so agents don't flood each other.

#### Existing Foundation

| Component | File | Status |
|-----------|------|--------|
| Hivemind memory bank | `pkg/memory/lifecycle.go` — task lifecycle, compiled entries | ✅ Built |
| DuckBrain persistence | `pkg/memory/` — MemoryEntry, key-value storage | ✅ Built |
| Agent identity | `pkg/identity/` — agent ID, known-friends.json | ✅ Built |
| In-progress collaboration (Phase 4.2) | `specs/interaction-map.md` §4.2 — CLARIFICATION_NEEDED protocol | ✅ Specified |

#### Implementation Steps

1. **`ContextBus`** — New file: `pkg/learning/context_bus.go`
   - Publish-subscribe model with domain-based subscriptions
   - Domains: `auth`, `database`, `api`, `infra`, `security`, `testing`, `docs`
   - Schema:
     ```go
     type SharedFinding struct {
         ID            string            // unique finding ID
         FromAgentID   string            // discovering agent
         ToAgentID     string            // target agent (or empty for broadcast)
         Domain        string            // auth, database, etc.
         Finding       string            // structured description
         EvidenceLinks []string          // PRs, commits, incidents
         Priority      string            // "info", "warning", "critical"
         Timestamp     time.Time
         Consumed      bool              // has the target agent seen it?
     }
     ```
   - Storage: DuckBrain key = `/context_bus/findings/{id}`, domain = `event`

2. **Agent Subscription**
   - Agents declare domain subscriptions at task start: "I'm working on auth → subscribe to auth, security, api"
   - Subscription stored in Hivemind memory bank for the work item
   - `ContextBus` matches findings to subscribers based on domain overlap
   - Direct addressing: Agent A can target Agent B specifically (by agent ID)

3. **Budget Tracking**
   - Each finding costs tokens (from the receiving agent's budget)
   - Small fixed cost per finding: ~500 tokens (the context window cost of reading it)
   - Agents have a daily finding budget: 10 findings/day at Provisional, 50 at Veteran
   - Priority "critical" findings bypass budget limits
   - Budget tracked via Cost Estimator integration

4. **Context Assembly Integration**
   - When assembling context for a task (Phase 3.3), include:
     - Relevant findings from the ContextBus since task started
     - Ranked by priority, then recency
     - Budget-constrained (total findings + spec + code context ≤ model window)

5. **Finding CLI**
   ```bash
   helix context share --domain auth --finding "Session refresh fails at UTC midnight boundary" \
     --evidence pr-1234,incident-5678 --priority warning

   helix context inbox [--agent <name>] [--unread-only]
   helix context subscribe --domain auth --domain security
   helix context unsubscribe --domain docs
   ```

6. **DuckBrain Memory Integration**
   - All findings persist in DuckBrain for cross-session recall
   - Agents can query: `mcp_duckbrain_recall(query="auth session findings", domain="event")`
   - Findings auto-expire after 30 days (configurable)

#### Test Strategy

| Layer | What to test | Method |
|-------|-------------|--------|
| Unit | Publish-subscribe matching | Publish to auth domain → only auth subscribers receive |
| Unit | Direct addressing | Publish to specific agent → only that agent receives |
| Unit | Budget enforcement | Exceed finding budget → blocked |
| Unit | Critical priority bypasses budget | Critical finding → delivered regardless of budget |
| Integration | Full cycle: Agent A publishes → Agent B's context assembly includes finding | End-to-end with DuckBrain |
| Integration | Expiry: finding older than 30 days not surfaced | Create old finding → verify not in context |

#### Integration Points

```
Agent A ──→ ContextBus.Publish(finding)
               │
               ├──→ DuckBrain storage (/context_bus/findings/{id})
               ├──→ Hivemind notification (if target agent is active)
               └──→ Subscriber matching (domain overlap)

Agent B's Context Assembler ──→ ContextBus.GetFindings(agentID, domains, budget)
                             ──→ Findings injected into context package
```

---

### 12.4 — Model Evaluation & Rotation

**Interaction Type:** GATE  
**Participants:** System ↔ Agent  
**Trigger:** Continuous evaluation against production outcomes  
**Purpose:** Production-correlated model scoring, auto-removal of underperforming models, agent selection recommendations

#### Resolution

Build a **`ModelEvaluator`** that continuously scores models against real production outcomes — incident attribution rates, false positive rates in reviews, and trust score trends of agents using each model. Models with high false positive rates are automatically removed from review rotation. Model performance data feeds agent selection so Axiom picks the right model for each task.

#### Existing Foundation

| Component | File | Status |
|-----------|------|--------|
| Incident attribution | `pkg/incident/attribution.go` — per-agent responsibility, severity multipliers | ✅ Built, tested |
| Trust scoring (per-agent, not per-model) | `pkg/trust/tiers.go` — six dimensions | ✅ Built, tested |
| Review consensus | `pkg/trust/ledger.go` — `EventReviewConsensus` | ✅ Event type defined |
| Chimera multi-model review | `specs/adversarial-review.md` — multiple models review independently | ✅ Specified |
| Cost tracking | `specs/cost-estimator.md` — per-task cost estimation and reconciliation | ✅ Specified |
| DuckBrain for audit trail | `pkg/memory/` — persistent storage | ✅ Built |

#### Implementation Steps

1. **`ModelEvaluator`** — New file: `pkg/learning/model_eval.go`
   - Tracks metrics per model (not per agent — a model is a provider+model combination, e.g., `deepseek-v4-pro`, `claude-sonnet-4-20250514`)
   - Metrics:
     ```go
     type ModelMetrics struct {
         ModelID          string    // provider:model, e.g. "openai:gpt-5.1"
         IncidentsAttributed int    // incidents where this model was the primary agent's model
         TotalMerges      int       // total merges by agents using this model
         IncidentRate     float64   // incidents / merges (lower is better)
         FalsePositives   int       // review REJECTs that were overridden by human
         TotalReviews     int       // total reviews performed
         FalsePositiveRate float64  // false positives / total reviews
         AvgTrustScore    float64   // average trust score of agents using this model
         AvgCostPerMerge  float64   // average token cost per successful merge
         ActiveAgents     int       // number of active agents using this model
         LastEvaluated    time.Time
     }
     ```

2. **Data Collection**
   - After each merge: record which model the author agent uses → increment `TotalMerges`
   - After each incident: lookup author agent's model → increment `IncidentsAttributed`
   - After each review override (human overrides agent review): increment `FalsePositives`
   - Daily recalculation: compute rates from accumulated counts
   - Data stored in DuckBrain: key = `/model_metrics/{model_id}`, domain = `config`

3. **Rotation Rules**
   - **False positive rate > 15%**: Model removed from review rotation (can still author code)
   - **Incident rate > 2× the fleet average**: Model flagged for review, agents using it receive advisory warning
   - **14 consecutive days above rotation threshold**: Model permanently removed from review rotation
   - **30 days of clean metrics**: Model can be re-admitted to review rotation
   - Model removal/re-admission events logged to DuckBrain: `/model_events/{event_id}`

4. **Agent Selection Integration**
   - Axiom queries `ModelEvaluator` when selecting agents for a task
   - Preference scoring: models with lower incident rates and lower cost get higher selection weight
   - Trust tier requirements override model preference (a Veteran agent on a mediocre model beats a Provisional on a great model)
   - Selection formula:
     ```go
     selectionScore = agent.trustScore * 0.70 +
                      (1.0 - model.incidentRate) * 0.20 +
                      (1.0 - model.costEfficiency) * 0.10
     ```

5. **Model CLI**
   ```bash
   helix models list [--sort-by incident-rate|false-positive-rate|cost]
   helix models show <model-id>
   helix models evaluate  # Trigger manual evaluation
   helix models rotate <model-id> --reason "FPR 22% for 14+ days"
   ```

6. **Dashboards & Alerts**
   - Prometheus gauge: `helix_model_incident_rate{model="..."}`
   - Prometheus gauge: `helix_model_fpr{model="..."}`
   - Alert: `ModelFPRHigh` when any model exceeds 15% FPR for 7+ days
   - Alert: `ModelIncidentRateSpike` when incident rate doubles week-over-week

#### Test Strategy

| Layer | What to test | Method |
|-------|-------------|--------|
| Unit | Metric accumulation: merge → TotalMerges++ | `model_eval_test.go` — simulate merge events |
| Unit | Incident rate calculation | Seed known merges/incidents → verify rate |
| Unit | False positive rate > 15% triggers rotation removal | Set FPR to 16%, verify model flagged |
| Unit | Rotation re-admission after 30 clean days | Set clean metrics for 30 days, verify re-admitted |
| Unit | Selection scoring weights trust over model metrics | Veteran on bad model > Provisional on good model |
| Integration | Full cycle: merge → incident → evaluate → rotate | End-to-end with DuckBrain |
| Integration | Prometheus metrics exported correctly | Verify gauge values after evaluation |

#### Integration Points

```
Merge Gate ──→ ModelEvaluator.RecordMerge(model_id, success)
Incident Engine ──→ ModelEvaluator.RecordIncident(model_id)
Review Engine ──→ ModelEvaluator.RecordReview(model_id, false_positive?)

Daily Cron ──→ ModelEvaluator.Evaluate()
                  │
                  ├──→ Rotation check (FPR > 15%?)
                  ├──→ DuckBrain store (/model_metrics/*)
                  ├──→ Prometheus metrics update
                  └──→ Axiom agent selection feed

Axiom ──→ ModelEvaluator.GetSelectionScores(capabilities, budget)
       ──→ Returns agents ranked by trust_score × model_performance
```

---

## Cross-Cutting Concerns

### DuckBrain Integration Summary

All Phase 11–12 data is persisted in DuckBrain for audit, replay, and cross-session durability:

| Data | DuckBrain Key Pattern | Domain |
|------|----------------------|--------|
| Trust events (JSONL) | `/trust/ledger/events` | `event` |
| Tier transitions | `/trust/tier_history/{agent_id}` | `event` |
| Human ratings | `/marketplace/ratings/{agent_id}/{human_id}` | `config` |
| Discovered patterns | `/learning/patterns/{pattern_id}` | `concept` |
| Agent skills | `/skills/{domain}/{skill_id}` | `concept` |
| Shared findings | `/context_bus/findings/{finding_id}` | `event` |
| Model metrics | `/model_metrics/{model_id}` | `config` |
| Model events | `/model_events/{event_id}` | `event` |

### Budget Tracking

All learning interactions consume tokens from agent budgets:
- Pattern discovery: varies by data volume (estimated 5K–50K tokens per run)
- Skill loading: ~500 tokens per skill injected into context
- Context bus findings: ~500 tokens per finding delivered
- Model evaluation: ~2K tokens per evaluation run

Cost Estimator (`specs/cost-estimator.md`) tracks all learning-related token spend.

### Circuit Breakers

| Service | Max Failures | Reset Timeout | On Open |
|---------|-------------|---------------|---------|
| DuckBrain | 5 | 60s | Degrade to local JSONL, replay later |
| Forgejo API | 5 | 60s | Skip codebase queries, use cached data |
| Chimera | 5 | 60s | Skip skill validation, flag for later review |

### Observability

All components expose Prometheus metrics:
- `helix_trust_score{agent="..."}` — per-agent trust score
- `helix_trust_events_total{type="..."}` — event counts by type
- `helix_tier_transitions_total{direction="promote|demote"}` — tier change counts
- `helix_human_ratings_total{agent="..."}` — rating counts
- `helix_patterns_discovered_total` — pattern discovery count
- `helix_skills_published_total` — skill publication count
- `helix_findings_shared_total` — cross-agent findings count
- `helix_model_incident_rate{model="..."}` — per-model incident rate
- `helix_model_fpr{model="..."}` — per-model false positive rate

---

## Build Order

1. **`pkg/trust/scheduler.go`** — RecalculationScheduler (11.1)
2. **`pkg/trust/controller.go`** — TierController (11.2)
3. **`pkg/trust/permissions.go`** — PermissionEnforcer (11.2)
4. **`pkg/trust/days_tracker.go`** — Consecutive-Days Tracker (11.2)
5. **Event-producer wiring** — mergegate, review, incident → trust ledger (11.1)
6. **`cmd/helix-marketplace/` extend** — Structured ratings (11.3)
7. **`pkg/marketplace/ratings.go`** — Rating store + trust wiring (11.3)
8. **`pkg/learning/miner.go`** — PatternMiner (12.1)
9. **`cmd/helix/learn.go`** — Pattern discovery CLI (12.1)
10. **`pkg/learning/skills.go`** — SkillRegistry (12.2)
11. **`cmd/helix/learn.go` extend** — Skill CLI (12.2)
12. **`pkg/learning/context_bus.go`** — ContextBus (12.3)
13. **`cmd/helix/context.go`** — Context sharing CLI (12.3)
14. **`pkg/learning/model_eval.go`** — ModelEvaluator (12.4)
15. **`cmd/helix/models.go`** — Model evaluation CLI (12.4)
16. **Integration tests** — Full feedback loop: merge → review → incident → recalculation → tier change → pattern discovery → skill update → model evaluation

---

## Verification Checklist

- [ ] Trust score is deterministically replayable from ledger
- [ ] No human can promote an agent — only outcomes-based auto-promotion
- [ ] Demotion requires 7 consecutive days below threshold
- [ ] Human ratings are weighted at exactly 10% of trust score
- [ ] Pattern discovery uses statistical significance (p < 0.05)
- [ ] Skills auto-deprecate when trust_weight < 0.3
- [ ] Cross-agent findings respect budget limits
- [ ] Models with FPR > 15% are removed from review rotation
- [ ] All data is DuckBrain-persisted for cross-session durability
- [ ] Prometheus metrics exported for all components
- [ ] Circuit breakers prevent cascading failures
