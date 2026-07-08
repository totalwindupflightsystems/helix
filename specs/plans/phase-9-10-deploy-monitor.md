# Helix Resolution Plan: Phases 9–10 — Deployment + Production Monitoring

> **Status:** Draft  
> **Spec Reference:** `specs/interaction-map.md` — Interactions 9.1–9.3, 10.1–10.4  
> **Package References:** `pkg/verify`, `pkg/deploy`, `pkg/incident`, `pkg/trust`  
> **Date:** 2026-07-07

---

## Executive Summary

Phases 9 and 10 form the **production boundary** between code authorship and running behavior. Phase 9 (Deployment & Release) controls *how* agent-authored code reaches production through three gated stages: shadow verification, canary deployment by trust tier, and dual human+agent release signoff. Phase 10 (Production Monitoring & Incident Response) surveils deployed code continuously and closes the feedback loop — breach → rollback → attribution → trust penalty → learning.

Every interaction point is designed for **dual first-class users**: the human (who needs change management — risk, blast radius, intent) and the AI agent (who needs verifiable criteria, evidence bundles, structured assertions).

---

## Phase 9: Deployment & Release

### 9.1 Shadow Verification

| Attribute | Value |
|---|---|
| **Type** | GATE |
| **Participants** | system ↔ agent, system ↔ system |
| **Spec Flow** | Deploy to shadow → mirror production traffic → compare behavior |

#### What Exists (`pkg/verify`)

| Component | File | Status |
|---|---|---|
| `ShadowManager` | `shadow.go` | ✅ Full lifecycle: `LaunchShadow` → `EvaluateShadow` → `PromoteToCanary` / `AutoRollback` |
| `ShadowConfig` | `shadow.go` | ✅ Configurable thresholds matching spec: MaxErrorRateDelta (0.1%), MaxLatencyOverheadPct (20%), MaxMemoryGrowthPct (10%), MaxNewErrorTypes (0) |
| `DifferentialReport` | `shadow.go` | ✅ Per-metric pass/fail with `BlockReason` on failure |
| `MetricsSnapshot` | `shadow.go` | ✅ Captures success rate, P50/P99 latency, error counts, memory growth |
| `ShadowState` machine | `shadow.go` | ✅ 7-state lifecycle: `idle → shadowing → shadow_passed/failed → canaried → rolled_back/promoted` |
| `ShadowAutoRollbackTriggers` | `monitor.go` | ✅ Five auto-rollback conditions from spec |
| `DriftDetector` | `drift.go` | ✅ Immediate and cumulative drift detection with `DriftSensitivity` |

#### Resolution Plan

**Goal:** Wire `ShadowManager` into the deployment pipeline so every merge triggers dark-launch verification automatically, with trust-tier-aware observation windows.

**Implementation Steps:**

1. **Shadow Launch Trigger**
   - Hook `ShadowManager.LaunchShadow(agentID, tier, baseline, config)` to the merge-gate event stream.
   - Automatically capture the production `MetricsSnapshot` at merge time as the baseline.
   - Derive `ShadowConfig.effectiveObservationWindow(tier)` from the trust tier's `CanarySchedule`: Provisional=24h, Observed=12h, Trusted=6h, Veteran=2h.

2. **Traffic Mirroring Integration**
   - Deploy agent code to a shadow container (dark launch — responses discarded, 0% production traffic).
   - Mirror a representative sample of production requests to the shadow instance.
   - Feed shadow metrics into `ShadowManager.RecordMetrics(agentID, snapshot)` at regular intervals (every 5 min per `SurveillanceConfig` default).

3. **Differential Evaluation**
   - After the observation window expires, call `ShadowManager.EvaluateShadow(agentID)`.
   - The evaluation produces a `DifferentialReport` with per-metric delta analysis:
     - Success rate delta must be ≤ 0.001 (0.1%)
     - P99 latency overhead must be ≤ 20%
     - Zero new error types allowed (config: `MaxNewErrorTypes: 0`)
     - Memory growth ≤ 10%

4. **Auto-Rollback on Shadow Failure**
   - If `EvaluateShadow` returns `AllPassed=false`, the `ShadowManager` auto-transitions to `StateRolledBack`.
   - Rollback reason recorded in `ShadowDeployment.RollbackReason`.
   - `BreachReporter` generates a structured markdown report for the PR comment (`PhaseFromState` → `PhaseShadow`).

5. **Shadow-Passed → Canary Gate**
   - On `StateShadowPassed`, the deployment is eligible for canary promotion (9.2).
   - The `ShadowDeployment.Report` (differential report) becomes evidence in the release signoff dashboard (9.3).

**Acceptance Criteria:**
- [ ] Every merge triggers shadow launch automatically within 60 seconds.
- [ ] Shadow observation duration matches trust tier (Provisional 24h, Veteran 2h).
- [ ] Differential report produced with per-metric pass/fail for all 4 metrics + new error types.
- [ ] Auto-rollback fires within 30 seconds of any threshold breach.
- [ ] Human-visible breach report posted to PR comment via `BreachReporter.FormatBreachReport`.

---

### 9.2 Canary Deployment by Trust Tier

| Attribute | Value |
|---|---|
| **Type** | GATE |
| **Participants** | system ↔ agent |
| **Spec Flow** | Gradual traffic ramp gated by trust tier. Veteran agents deploy 8× faster than Provisional. |

#### What Exists (`pkg/verify`)

| Component | File | Status |
|---|---|---|
| `CanarySchedule(tier)` | `contract.go` | ✅ Tier-specific schedules: Provisional 6 steps/96h, Veteran 2 steps/12h |
| `CanaryStep` | `contract.go` | ✅ Step number, traffic %, duration |
| `ComputeCanaryPercentage(tier)` | `canary.go` | ✅ Initial traffic: Pro 1%, Observed 5%, Trusted 10%, Veteran 25% |
| `CanaryPromoter.EvaluatePromotion(...)` | `canary.go` | ✅ 5-check promotion evaluation (contract, drift, success rate, errors, obs window) |
| `AutoRampSchedule(tier)` | `canary.go` | ✅ Gradual ramp with per-step observation gaps |
| `ShadowManager.PromoteToCanary` | `shadow.go` | ✅ State transition: shadow_passed → canaried |
| `ShadowManager.AdvanceCanary` | `shadow.go` | ✅ Step-by-step advancement through the schedule |
| `ShadowManager.CurrentCanaryStep` | `shadow.go` | ✅ Query current canary step info |

#### Resolution Plan

**Goal:** Automate the canary ramp pipeline: shadow-passed → gradual traffic increase → continuous breach checking at each step → auto-rollback or full promotion.

**Implementation Steps:**

1. **Canary Promotion Gate**
   - After `ShadowManager.EvaluateShadow` returns `StateShadowPassed`, invoke `CanaryPromoter.EvaluatePromotion(...)` with:
     - `report`: the `DifferentialReport` from shadow evaluation
     - `contractResults`: from `Monitor.Evaluate(metrics)` or `Checker.CheckAll(contract, metrics)`
     - `driftAssessment`: from `DriftDetector.Evaluate()`
     - `elapsed`: time since `LaunchedAt`
     - `requiredWindow`: from `CanarySchedule(tier)`
   - Only promote if `PromotionResult.Decision == PromotionReady`.

2. **Tier-Specific Initial Traffic**
   - Call `ComputeCanaryPercentage(tier)` to set the starting traffic allocation:
     - Provisional: 1%
     - Observed: 5%
     - Trusted: 10%
     - Veteran: 25%
   - Pass this through `ShadowManager.PromoteToCanary(agentID)` → `AutoRampSchedule(tier)`.

3. **Step-by-Step Canary Advancement**
   - At each canary step, before advancing:
     - Run `Monitor.Evaluate(metrics)` to check all behavior contracts.
     - Run `DriftDetector` against the baseline.
     - If any breach or drift exceeds threshold → `ShadowManager.AutoRollback(agentID, reason)`.
   - If all checks pass → `ShadowManager.AdvanceCanary(agentID)`.
   - Final step (100% traffic) → `StatePromoted`.

4. **Canary Breach Handling**
   - Breaches in canary phase are especially critical (real user traffic affected).
   - `BreachReporter.recommendAction` handles `PhaseCanary` — rollback actions include "Auto-rollback triggered to prevent further impact."
   - Post-rollback: `BreachNotification` dispatched via `NotificationDispatcher` to all channels (Forgejo PR, trust ledger, incident store).

5. **Trust Tier Acceleration**
   - Veteran agents (2 steps, 12h total) deploy 8× faster than Provisional (6 steps, 96h total).
   - This is the direct economic incentive for agents to maintain high trust.
   - Trust tier determines: shadow duration, canary steps, initial traffic %, and per-step observation gaps.

**Acceptance Criteria:**
- [ ] Canary ramp automatically follows `CanarySchedule(tier)` — correct step count and durations.
- [ ] Each step transition is gated by contract + drift + success-rate checks.
- [ ] Auto-rollback within 30 seconds of any canary-step breach detection.
- [ ] Veteran agents reach 100% in ≤12h; Provisional takes 96h.
- [ ] Canary breach reports posted to PR with phase-specific messaging.

---

### 9.3 Release Signoff (Dual Human + Agent)

| Attribute | Value |
|---|---|
| **Type** | GATE |
| **Participants** | human ↔ system, agent ↔ system |
| **Spec Flow** | Human approves change (intent). Agent verifies technical gates. Both signatures required. |

#### What Exists

| Component | File | Status |
|---|---|---|
| `ShadowDeployment` state tracking | `shadow.go` | ✅ Full lifecycle state with timestamps |
| `DifferentialReport` | `shadow.go` | ✅ Evidence artifact for signoff dashboard |
| `BreachReportData` | `breach_report.go` | ✅ Structured data for human-readable reports |
| `BreachReporter` | `breach_report.go` | ✅ Phase-aware report generation |
| `NotificationDispatcher` | `notify.go` | ✅ Multi-channel (Forgejo PR + trust ledger + incident store) |
| `TrustSnapshot.GetSnapshot` | `snapshot.go` | ✅ Full trust state queryable from ledger replay |
| `PromotionResult` | `promotion.go` | ✅ Per-criterion pass/fail for tier promotion |

#### Resolution Plan

**Goal:** Build a dual signoff dashboard where the human approves the *change* (intent, timing, risk acceptance) and the agent verifies all *technical gates* are green. Both signatures are required before any deployment reaches production.

**Implementation Steps:**

1. **Agent Technical Signoff (Automated)**
   - Before any deployment proceeds past shadow, the system must produce an `AgentSignoff` evidence bundle containing:
     - Shadow verification: `DifferentialReport` with `AllPassed=true`
     - Canary readiness: `PromotionResult` with `Decision=READY` and all 5 checks passed
     - Behavior contracts: all registered contracts checked by `Monitor.Evaluate`, zero `Breach` events
     - Drift assessment: `DriftAssessment` with `CriticalCount=0` and `AnyBreach=false`
     - Trust tier verification: agent's current `TrustTier` from `GetSnapshot` meets the deployment's tier requirements
     - Cost gate: actual token spend vs. estimated (Phase 8.2 data) with variance ≤20%
     - Prompt provenance: commit's `Prompt:` trailer hash matches registered prompt (Phase 8.3 data)
   - If ANY of these checks fail → technical signoff is `DENIED` with specific blocking reasons.
   - The signoff is cryptographically signed with the system's ED25519 key.

2. **Human Change Signoff (Intent)**
   - A dashboard presents the human with the *change management* view:
     - **What changed:** commit diff summary, files affected, blast-radius map from dependency graph
     - **Why:** linked ADR (Architecture Decision Record), linked spec section, agent's stated intent
     - **Risk:** risk score from incident database correlation, architectural fit analysis, agent trust tier and recent incident history
     - **Timing:** suggested deployment window, conflict detection with other ongoing deployments
     - **Tradeoffs:** what was considered, what was rejected, edge cases documented
   - The human approves the **change intent** — NOT reviewing code line-by-line (that happened in Phase 6).
   - Human signature is recorded as a `TrustEvent` in the ledger (`EventHumanRating`).

3. **Dual-Signoff Gate**
   - Both signatures (human + agent) must be present before deployment proceeds.
   - The gate produces a `ReleaseSignoff` record:
     ```
     {
       "release_id": "rel-2026-07-07-001",
       "agent_id": "agent-sandbox-7",
       "merge_commit": "abc123def",
       "human_signoff": { "human_id": "alice", "timestamp": "...", "approved": true },
       "agent_signoff": { "all_gates_passed": true, "blocking_criteria": [] },
       "trust_tier_at_signoff": "trusted",
       "canary_schedule": { "tier": "trusted", "total_duration": "36h" }
     }
     ```

4. **Signoff Dashboard API**
   - `GET /api/releases/{id}/signoff-status` — returns both signature states
   - `POST /api/releases/{id}/human-approve` — records human approval
   - `GET /api/releases/{id}/agent-signoff-evidence` — returns the full evidence bundle
   - Dashboard integrates with Forgejo PR view via webhook comment

5. **Override & Audit Trail**
   - Human **cannot** override the agent technical signoff (no `--force`).
   - If human disagrees with agent technical signoff, they file a `CLARIFICATION_NEEDED` (Phase 4.2 pattern).
   - Every signoff event is recorded in the trust ledger (`EventHumanRating`, `EventTierChange`).
   - Full audit trail: who approved what, when, with what evidence.

**Acceptance Criteria:**
- [ ] Agent technical signoff is fully automated — zero human intervention needed for gate checks.
- [ ] Human signoff dashboard shows change-management view (intent, risk, blast radius, timing).
- [ ] Both signatures mandatory — missing either blocks deployment.
- [ ] Signoff events recorded in append-only trust ledger.
- [ ] No human override of agent technical gates.

---

## Phase 10: Production Monitoring & Incident Response

### 10.1 Behavior Contract Surveillance

| Attribute | Value |
|---|---|
| **Type** | OBSERVATION → GATE |
| **Participants** | system ↔ agent |
| **Spec Flow** | Continuous contract monitoring → breach → auto-rollback → agent notification → trust penalty |

#### What Exists (`pkg/verify`)

| Component | File | Status |
|---|---|---|
| `BehaviorContract` | `contract.go` | ✅ YAML contract parsing, validation, `ShouldRollback()`, `ShouldNotify()` |
| `Assertion` | `contract.go` | ✅ Metric + operator (gte/lte/eq) + value + window |
| `Checker` | `contract.go` | ✅ Single-assertion `Check()` and bulk `CheckAll()` |
| `Monitor` | `monitor.go` | ✅ `Evaluate(metrics)` across all registered contracts, `Breach` detection |
| `Breach` | `monitor.go` | ✅ Captures contract name, agent, merge commit, failed assertions, rollback/notify flags |
| `SteadyStateAggregator` | `surveillance.go` | ✅ Phase 3 steady-state: 7-day degradation detection, sustained drift escalation |
| `SurveillanceConfig` | `surveillance.go` | ✅ Defaults: 5-min check interval, 7-day long window, 30-min sustained drift, 2h rollback escalation |
| `DegradationReport` | `surveillance.go` | ✅ Per-metric trend analysis over long windows |
| `AlertEscalation` | (in `surveillance.go`) | ✅ Escalation levels: none → notify → investigate → rollback |
| `BreachReporter` | `breach_report.go` | ✅ Phase-aware breach → structured markdown report |
| `NotificationDispatcher` | `notify.go` | ✅ Multi-channel: Forgejo PR, trust ledger, incident store, with 5-min debounce |

#### Resolution Plan

**Goal:** Wire `SteadyStateAggregator` into the production runtime so every deployed agent is continuously surveilled against its behavior contracts. Breach triggers auto-rollback, notification, trust penalty, and incident recording — fully automated.

**Implementation Steps:**

1. **Surveillance Registration**
   - On `StatePromoted` (full deployment), call `SteadyStateAggregator.RegisterAgent(agentID, baseline)`.
   - Baseline = the production `MetricsSnapshot` captured at merge time.
   - Call `SteadyStateAggregator.RegisterContract(contract)` for each `.helix/contracts/*.yaml` linked to this deployment.

2. **Continuous Check Loop**
   - `SteadyStateAggregator.CheckAgent(agentID, metrics)` runs every 5 minutes (`SurveillanceConfig.CheckInterval`).
   - Each cycle:
     - Evaluates all registered behavior contracts via `Monitor.Evaluate(metrics)`.
     - Checks immediate drift via `DriftDetector` against the baseline.
     - Rolls up daily summaries for long-window analysis.
     - Detects gradual degradation over 7-day windows via `LongRunningMonitor`.
     - Updates escalation level via `AlertEscalation`.
   - Emits a `SurveillanceEvent` with status (`healthy`, `warning`, `degraded`, `breach`).

3. **Breach → Response Pipeline**
   - When `SurveillanceEvent.Status == StatusBreach`:
     1. **Auto-Rollback**: `ShadowManager.AutoRollback(agentID, reason)` if contract has `breach_action: rollback` or `rollback_and_notify`.
     2. **Notification**: `NotificationDispatcher.NotifyFromBreach(breach, metrics, evidenceLinks)` → posts to Forgejo PR comment (`ForgejoPRNotifier`), records trust penalty (`TrustLedgerNotifier`), creates incident record (`IncidentStoreNotifier`).
     3. **Trust Penalty**: `TrustLedgerNotifier.PenaltyCallback` feeds into `trust.ApplyIncidentPenalty` weighted by `breachSeverity` (0.05–0.40 based on failed assertion count).
     4. **5-Minute Debounce**: prevents notification spam from flapping metrics.

4. **Gradual Degradation Detection**
   - `LongRunningMonitor` analyzes 7-day rolling windows of `DailySummary` data.
   - Thresholds from `LongRunningThresholds`:
     - Success rate decline >5%
     - P99 latency increase >20%
     - Error rate increase >50%
     - Memory growth >15%
   - When `DegradationReport.IsDegrading == true` and `EscalationLevel == EscalationInvestigate`:
     - Posts a warning to the agent's PR (not a full breach — an early warning).
     - If sustained beyond `EscalationRollbackDuration` (2h default) → escalates to `EscalationRollback`.

5. **Sustained Drift Escalation**
   - Drift that persists > `SustainedDriftDuration` (30 min) triggers `EscalationInvestigate`.
   - Drift that persists > `EscalationRollbackDuration` (2h) triggers `EscalationRollback`.
   - `AlertEscalation` tracks per-agent escalation state across check cycles.

**Acceptance Criteria:**
- [ ] Every promoted deployment is registered for surveillance within 60 seconds.
- [ ] Contract evaluation runs every 5 minutes per agent.
- [ ] Breach → auto-rollback latency ≤ 30 seconds.
- [ ] Breach → PR notification ≤ 60 seconds (with 5-min debounce for repeat breaches).
- [ ] 7-day degradation detection catches slow leaks (memory, latency creep).
- [ ] Escalation from warning → investigate → rollback follows configured timeouts.

---

### 10.2 Incident Detection

| Attribute | Value |
|---|---|
| **Type** | OBSERVATION |
| **Participants** | system ↔ agent, system ↔ human |
| **Spec Flow** | Anomaly → incident context package → agent produces initial diagnosis → human reviews |

#### What Exists (`pkg/verify`, `pkg/incident`)

| Component | File | Status |
|---|---|---|
| `SurveillanceEvent` | `surveillance.go` | ✅ Full context: status, metrics, breaches, drift, degradation, escalation |
| `BreachNotification` | `notify.go` | ✅ Structured payload with failed checks, evidence links, recommended action |
| `Incident` | `types.go` | ✅ ID, agent, PR URL, severity, causal chain, evidence |
| `Store` | `types.go` | ✅ Thread-safe store with by-agent and by-severity indexing |

#### Resolution Plan

**Goal:** When a `SurveillanceEvent` is actionable (`IsActionable() == true`), automatically assemble an incident context package and dispatch it to an on-call agent for initial diagnosis before human engagement.

**Implementation Steps:**

1. **Incident Context Package Assembly**
   - When `SurveillanceEvent.IsActionable()` returns `true`, auto-assemble an `IncidentContext`:
     ```go
     type IncidentContext struct {
         Event           SurveillanceEvent
         RecentDeploys   []ShadowDeployment    // last 7 days
         ChangedPaths    []ChangePath          // from git diff of suspect commits
         RelatedIncidents []IncidentPattern    // from LearningDatabase.FeedReviewContext
         AgentTrust      TrustSnapshot         // current trust state
         EvidenceLinks   []string              // log links, metric dashboards
     }
     ```
   - `ChangedPaths` are derived from the merge commits in the surveillance window.
   - `RelatedIncidents` use `LearningDatabase.FeedReviewContext` with `PRContext` derived from changed file paths.

2. **Agent Initial Diagnosis**
   - The incident context is dispatched to a designated "incident responder" agent (or the agent whose code is under surveillance).
   - That agent produces an `InitialDiagnosis`:
     ```go
     type InitialDiagnosis struct {
         IncidentID     string
         CausalChain    []string    // suspected code paths
         SeverityEstimate string   // low/medium/high/critical
         AffectedServices []string
         Confidence     float64     // 0.0–1.0
         RecommendedActions []string
         EvidenceLinks  []string
     }
     ```
   - The diagnosis is posted to the PR as a structured comment before the human on-call engages.

3. **Human Review & Confirmation**
   - Human on-call receives the incident alert with the agent's initial diagnosis already present.
   - Human reviews the causal chain and either:
     - Confirms → incident proceeds to attribution (10.3) and learning (10.4)
     - Redirects → human provides corrected causal chain; agent's diagnosis accuracy is tracked for trust scoring
   - The human confirmation/redirect is recorded in the incident record.

4. **Incident Store Integration**
   - On human confirmation, an `Incident` record is created in `incident.Store`:
     ```go
     inc := &Incident{
         ID:          uuid.New().String(),
         AgentID:     surveillanceEvent.AgentID,
         PRURL:       mergeCommitPRURL,
         Severity:    mapSurveillanceStatusToSeverity(event.Status),
         CausalChain: diagnosis.CausalChain,
         Timestamp:   time.Now().UTC(),
         Description: diagnosis.Reason,
         Evidence:    diagnosis.EvidenceLinks,
     }
     store.Add(inc)
     ```

**Acceptance Criteria:**
- [ ] Incident context package auto-assembled within 30 seconds of actionable surveillance event.
- [ ] Agent initial diagnosis produced within 2 minutes of incident detection.
- [ ] Human on-call sees agent diagnosis alongside the alert.
- [ ] Human confirmation/redirect tracked for agent diagnosis accuracy.
- [ ] Incident record created in store after human confirmation.

---

### 10.3 Incident Attribution

| Attribute | Value |
|---|---|
| **Type** | LEARNING |
| **Participants** | system ↔ agent |
| **Spec Flow** | Trace causal chain → commits → agents → responsibility split: author 70%, reviewers 20%, approver 10% |

#### What Exists (`pkg/incident`, `pkg/trust`)

| Component | File | Status |
|---|---|---|
| `AttributionEngine` | `attribution.go` | ✅ Full attribution computation with configurable weights |
| `AttributionWeights` | `attribution.go` | ✅ Default: Author 0.70, Reviewers 0.20, Approver 0.10 |
| `ChangePath` | `attribution.go` | ✅ File path → merge SHA → author/reviewer/approver IDs |
| `AttributionResult` | `attribution.go` | ✅ Responsibility map, evidence links, summarization |
| `TrustPenalty` | `attribution.go` | ✅ Severity-weighted: low=0.05, medium=0.10, high=0.20, critical=0.40 |
| `FindResponsiblePaths` | `attribution.go` | ✅ Filter change paths by causal chain |
| `IncidentAttributionWeight` | `tiers.go` | ✅ Time-decay: 0-7d=100%, 8-30d=50%, 31-90d=10%, >90d=0% |
| `ApplyIncidentPenalty` | `tiers.go` | ✅ 0.3 × attributionWeight penalty applied to trust score |
| `TrustPenaltyCallback` | `attribution.go` | ✅ Integration point: `ApplyTrustPenalties(result, severity, cb)` |
| `Ledger.Append` | `ledger.go` | ✅ Append-only JSONL for `EventIncidentPenalty` and `EventIncidentAttrib` |
| `Store.ListByAgent` | `types.go` | ✅ Query incidents by agent for trust recalculation |

#### Resolution Plan

**Goal:** After human-confirmed incident detection (10.2), automatically trace the causal chain to responsible agents, compute weighted responsibility, apply trust penalties with time-decay, and record everything in the trust ledger.

**Implementation Steps:**

1. **Causal Chain → Change Path Mapping**
   - From the confirmed `Incident.CausalChain` (list of file paths), build `[]ChangePath`:
     - For each file path in the causal chain, query git for the most recent merge commit that touched it within the incident window.
     - Extract `AuthorID`, `ReviewerIDs`, and `ApproverID` from the merge commit's `Co-authored-by:` and review metadata.
   - Call `FindResponsiblePaths(allChangePaths, incident.CausalChain)` to filter to relevant paths.

2. **Attribution Computation**
   - Call `engine.Attribute(incidentID, responsiblePaths, evidenceLinks)`:
     - For each `ChangePath`: author gets 70%, reviewers split 20% equally, approver gets 10%.
     - Multiple change paths → responsibility accumulates, then normalizes to 1.0.
   - Result: `AttributionResult` with `Responsibility: map[agentID]float64`.

3. **Trust Penalty Application**
   - For each responsible agent, compute `TrustPenalty(share, severity)`:
     - Low severity × share: e.g., 0.70 × 0.05 = 0.035 penalty
     - Critical severity × share: e.g., 0.70 × 0.40 = 0.280 penalty
   - Apply time-decay via `IncidentAttributionWeight(daysSinceIncident)`:
     - Within 7 days: full weight
     - 8–30 days: 50% weight
     - 31–90 days: 10% weight
     - >90 days: 0% (expired)
   - Call `engine.ApplyTrustPenalties(result, severity, callback)` where callback writes to the trust ledger:
     ```
     TrustEvent{
       EventType: "incident_penalty",
       Data: { attribution_weight: 0.70, trust_score_before: 0.82, trust_score_after: 0.61 }
     }
     ```

4. **Shared Responsibility Design**
   - An agent that rubber-stamps reviews shares blame when the code fails.
   - Reviewer responsibility split equally: if 2 reviewers, each gets 10% (20% ÷ 2).
   - Approving human always gets 10% — this incentivizes human reviewers to take agent reviews seriously rather than blindly approving.
   - No agent can escape blame by not being the author — review accountability is tracked.

5. **Trust Ledger Integration**
   - Every incident attribution writes two events to `trust.Ledger`:
     1. `EventIncidentAttrib` — records the raw attribution with evidence links
     2. `EventIncidentPenalty` — records the trust score change with before/after
   - The ledger is append-only and replay-verifiable — any observer can independently verify an agent's trust score by replaying.

6. **Post-Attribution Trust Recalculation**
   - After penalties are applied, call `trust.EvaluateFullTierCycle(agentMetrics)`:
     - Check if agent qualifies for promotion (unlikely after incident, but possible if score still high).
     - Check if agent triggers demotion (score below tier threshold for 7+ consecutive days).
     - Record tier changes as `EventTierChange` or `EventDemotion` in the ledger.

**Acceptance Criteria:**
- [ ] Causal chain → change path mapping completes within 30 seconds of incident confirmation.
- [ ] Attribution weights sum to 1.0 across all responsible parties.
- [ ] Trust penalties applied with correct severity multiplier and time-decay.
- [ ] All attribution and penalty events recorded in append-only ledger.
- [ ] Ledger replay produces identical trust scores (deterministic verification).
- [ ] Shared reviewer responsibility incentivizes thorough reviews.

---

### 10.4 Incident Learning

| Attribute | Value |
|---|---|
| **Type** | LEARNING |
| **Participants** | agent ↔ agent, system ↔ agent |
| **Spec Flow** | Incident → learning database → future reviews reference past incidents for similar changes |

#### What Exists (`pkg/incident`)

| Component | File | Status |
|---|---|---|
| `LearningDatabase` | `learning.go` | ✅ Thread-safe pattern store with similarity-based retrieval |
| `IncidentPattern` | `learning.go` | ✅ Structured: categories, change type, severity, keywords, root cause, lessons learned |
| `PRContext` | `learning.go` | ✅ PR metadata for similarity matching |
| `FeedReviewContext` | `learning.go` | ✅ Ranked similarity search: category overlap (40%), keyword overlap (40%), change type (10%), severity boost (10%) |
| `ReviewContextReport` | `learning.go` | ✅ Ranked items + accumulated review criteria (lessons learned) |
| `CategorizeFile` / `CategorizeFiles` | `learning.go` | ✅ 12 file categories: auth, crypto, database, api, infra, config, test, doc, iac, ci, networking, other |
| `StoreFromIncident` | `learning.go` | ✅ Create pattern from Incident record + metadata |

#### Resolution Plan

**Goal:** After every incident is resolved and attributed (10.3), store it as a structured `IncidentPattern` in the `LearningDatabase`. When future PRs touch similar files or keywords, surface past incidents as review context — closing the feedback loop from production failure back to pre-merge review.

**Implementation Steps:**

1. **Incident → Pattern Storage**
   - After incident attribution completes, call `db.StoreFromIncident(inc, categories, changeType, keywords, rootCause, lessonsLearned)`.
   - `categories` = `CategorizeFiles(incident.CausalChain)` — maps file paths to `FileCategory` enums.
   - `changeType` = inferred from git diff: `new`, `modify`, `delete`, `refactor`, or `migration`.
   - `keywords` = extracted from incident description, evidence links, and causal chain (auto-tokenized).
   - `rootCause` = the primary causal factor identified during diagnosis.
   - `lessonsLearned` = actionable review criteria derived from the incident (e.g., "Verify session refresh under high concurrency", "Check for nil pointer on empty result set").

2. **Review Context Feed**
   - During Phase 6 (Code Review), when a new PR is created, build a `PRContext` from the changed files:
     ```go
     ctx := PRContext{
         Categories: CategorizeFiles(pr.ChangedFiles),
         ChangeType: inferChangeType(pr.Diff),
         Keywords:   extractKeywords(pr.Title, pr.Description),
         Files:      pr.ChangedFiles,
     }
     ```
   - Call `db.FeedReviewContext(ctx)` to get a ranked list of past incidents.
   - Surface the top-5 matches in the agent review interface with:
     - Similarity score
     - Match reasons (e.g., "matching file categories", "shared keywords")
     - Lessons learned (actionable review criteria)

3. **Adversarial Review Integration**
   - The `@redteam` adversarial reviewer (Phase 2.3) queries the learning database for incidents matching the changed code patterns.
   - `@assumption-buster` uses past incidents to challenge implicit assumptions.
   - Example: "This PR changes the auth session handler. 3 past incidents in this category were traced to race conditions in token refresh. Verify that `refreshToken` is concurrency-safe."

4. **Agent Query Interface**
   - Agents can query: "Has this failure pattern occurred before?"
   - `LearningDatabase.FeedReviewContext` serves this — agents provide a `PRContext` describing what they're about to change and get back ranked past incidents.
   - This is the "institutional memory" — agents don't start from zero knowledge of past failures.

5. **Learning Database Metrics**
   - Track learning database effectiveness:
     - How often do surfaced incidents prevent repeat failures?
     - What % of reviews reference incident history?
     - Which incident patterns are most frequently matched (high-signal categories)?
   - These metrics feed into `Agent Skill Transfer` (Phase 12.2) — agents that consistently prevent repeat incidents gain trust in that domain.

6. **Incident Pattern Lifecycle**
   - Patterns age out of high-relevance over time (90-day decay for similarity scoring).
   - Patterns with high match frequency and zero repeat incidents gain "validated" status.
   - Patterns that are frequently matched but continue to recur signal that lessons aren't being followed → trust penalty for agents ignoring learning context.

**Acceptance Criteria:**
- [ ] Every resolved incident is stored as an `IncidentPattern` within 60 seconds of attribution.
- [ ] `FeedReviewContext` returns ranked past incidents for any PR within 100ms.
- [ ] Top-5 matches surfaced in agent review interface with similarity scores and lessons learned.
- [ ] Adversarial reviewers (`@redteam`, `@assumption-buster`) query the learning database automatically.
- [ ] Agents can query "has this pattern failed before?" and get structured results.
- [ ] Pattern match metrics tracked for learning database effectiveness.

---

## Cross-Cutting Integration Points

### Trust Feedback Loop (Phases 9–10 → 11)

Every event in Phases 9–10 feeds the trust system:

| Event | Trust Dimension | Effect |
|---|---|---|
| Shadow verification passes | Merge success rate | Positive — successful deploy |
| Shadow/canary breach → auto-rollback | Incident attribution | Negative — penalty via `ApplyIncidentPenalty` |
| Canary promotion completes cleanly | Merge success rate | Positive — deployment quality |
| Incident attributed to agent | Incident attribution (30%) | Negative — weighted by severity + decay |
| Reviewer attributed for incident | Review consensus (15%) | Negative — rubber-stamping detected |
| Human signoff confirmed | Human feedback (10%) | Positive — human confidence |
| Surveillance shows no breaches for 30 days | Tenure (10%) | Positive — sustained reliability |

### Package Dependency Map

```
pkg/verify (shadow, canary, contracts, surveillance, breach, notify)
    │
    ├──► pkg/incident (types, store, attribution, learning)
    │       │
    │       └──► pkg/trust (tiers, ledger, snapshot, promotion, scorer)
    │
    └──► pkg/deploy (agent templates, systemd units, caddy)
```

### Data Flow Summary

```
Merge (Phase 8)
  │
  ├─→ ShadowManager.LaunchShadow          [verify/shadow.go]
  │     │
  │     ├─→ Metrics collection (5-min)     [verify/shadow.go: RecordMetrics]
  │     ├─→ EvaluateShadow                 [verify/shadow.go: EvaluateShadow]
  │     │     ├─→ Pass → PromoteToCanary   [verify/shadow.go: PromoteToCanary]
  │     │     └─→ Fail → AutoRollback      [verify/shadow.go: AutoRollback]
  │     │
  │     └─→ PromoteToCanary
  │           │
  │           ├─→ CanaryPromoter.Evaluate  [verify/canary.go]
  │           ├─→ Step-by-step advancement [verify/shadow.go: AdvanceCanary]
  │           └─→ Breach → AutoRollback    [verify/shadow.go: AutoRollback]
  │
  ├─→ Release Signoff (dual)               [Phase 9.3]
  │     ├─→ Agent: automated gate check
  │     └─→ Human: change-management approval
  │
  └─→ Production (StatePromoted)
        │
        ├─→ SteadyStateAggregator.Register [verify/surveillance.go]
        │     │
        │     ├─→ CheckAgent (every 5 min)  [verify/surveillance.go: CheckAgent]
        │     │     ├─→ Monitor.Evaluate     [verify/monitor.go]
        │     │     ├─→ DriftDetector        [verify/drift.go]
        │     │     └─→ LongRunningMonitor   [verify/surveillance.go]
        │     │
        │     ├─→ Breach detected
        │     │     ├─→ AutoRollback         [verify/shadow.go: AutoRollback]
        │     │     └─→ NotificationDispatch [verify/notify.go]
        │     │           ├─→ Forgejo PR      [notify.go: ForgejoPRNotifier]
        │     │           ├─→ Trust Ledger    [notify.go: TrustLedgerNotifier]
        │     │           └─→ Incident Store  [notify.go: IncidentStoreNotifier]
        │     │
        │     └─→ Degradation detected
        │           └─→ Escalation            [verify/surveillance.go: AlertEscalation]
        │
        └─→ Incident Response (Phase 10)
              │
              ├─→ Incident context assembly   [Phase 10.2]
              ├─→ Agent initial diagnosis      [Phase 10.2]
              ├─→ Human confirmation           [Phase 10.2]
              │
              ├─→ AttributionEngine.Attribute  [incident/attribution.go]
              │     └─→ TrustPenalty → Ledger  [trust/ledger.go]
              │
              └─→ LearningDatabase.Store       [incident/learning.go]
                    └─→ FeedReviewContext       [incident/learning.go]
                          └─→ Phase 6 Review    (future PRs)
```

---

## Implementation Order

| Step | Interaction | What to Wire | Priority |
|---|---|---|---|
| 1 | 9.1 Shadow | Merge gate → `ShadowManager.LaunchShadow` | HIGH |
| 2 | 9.1 Shadow | `EvaluateShadow` → differential report → PR comment | HIGH |
| 3 | 9.2 Canary | `PromoteToCanary` + `AdvanceCanary` with breach gates | HIGH |
| 4 | 9.3 Signoff | Agent technical signoff evidence bundle (automated) | MEDIUM |
| 5 | 9.3 Signoff | Human change-management dashboard | MEDIUM |
| 6 | 10.1 Surveillance | `SteadyStateAggregator.RegisterAgent` on promotion | HIGH |
| 7 | 10.1 Surveillance | Breach → auto-rollback + notification pipeline | HIGH |
| 8 | 10.1 Surveillance | Degradation detection + escalation | MEDIUM |
| 9 | 10.2 Detection | Incident context assembly + agent diagnosis | MEDIUM |
| 10 | 10.3 Attribution | Causal chain → `AttributionEngine` → trust penalty | HIGH |
| 11 | 10.4 Learning | `StoreFromIncident` → `FeedReviewContext` | MEDIUM |

---

## Risks & Mitigations

| Risk | Mitigation |
|---|---|
| Shadow deployment doubles infrastructure cost | Shadow instances are short-lived (max 24h); auto-scaling groups spin them up/down on demand |
| Canary ramp takes too long for urgent hotfixes | Emergency hotfix path: human signoff can accelerate canary to Veteran-equivalent schedule with audit trail |
| False positive breaches erode trust in the system | 5-minute debounce on notifications; sustained-drift threshold (30 min) before escalation; false positive tracker feeds Phase 7.2 learning |
| Attribution punishes agents for latent bugs found months later | Time-decay formula (`IncidentAttributionWeight`): full weight only within 7 days, decays to 0% after 90 days |
| Learning database grows unbounded | Patterns older than 90 days receive zero similarity weight; periodic compaction of tombstoned patterns |
| Human on-call ignores agent diagnosis | Human override rate tracked; agents with consistently overridden diagnoses lose trust in "review consensus" dimension |

---

## References

- `specs/interaction-map.md` — Interaction points 9.1–9.3, 10.1–10.4
- `specs/production-verification.md` — Three-phase post-merge pipeline specification
- `specs/trust-model.md` — Trust tiers, scoring dimensions, ledger design
- `specs/deployment.md` — Deployment architecture
- `pkg/verify/shadow.go` — `ShadowManager`, `ShadowConfig`, `DifferentialReport`
- `pkg/verify/canary.go` — `CanaryPromoter`, `ComputeCanaryPercentage`, `AutoRampSchedule`
- `pkg/verify/contract.go` — `BehaviorContract`, `CanarySchedule`, `Checker`
- `pkg/verify/monitor.go` — `Monitor`, `Breach`, `DriftReport`
- `pkg/verify/surveillance.go` — `SteadyStateAggregator`, `SurveillanceConfig`, `DegradationReport`
- `pkg/verify/notify.go` — `NotificationDispatcher`, `BreachNotification`, channel implementations
- `pkg/verify/breach_report.go` — `BreachReporter`, `BreachReportData`, phase-aware actions
- `pkg/incident/types.go` — `Incident`, `Store`
- `pkg/incident/attribution.go` — `AttributionEngine`, `AttributionWeights`, `TrustPenalty`
- `pkg/incident/learning.go` — `LearningDatabase`, `IncidentPattern`, `FeedReviewContext`
- `pkg/trust/tiers.go` — `TrustTier`, `DetermineTier`, `IncidentAttributionWeight`, `ApplyIncidentPenalty`
- `pkg/trust/promotion.go` — `EvaluatePromotion`, `EvaluateFullTierCycle`
- `pkg/trust/ledger.go` — `Ledger`, `TrustEvent`, `Replay`
- `pkg/trust/snapshot.go` — `GetSnapshot`, `TrustSnapshot`, `ScoreBreakdown`
- `pkg/deploy/agent/template.go` — Agent container template generation
- `pkg/deploy/systemd/unit.go` — Systemd unit templates
