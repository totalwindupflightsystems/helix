# Helix Feature 3 — Agent-to-Agent PR Negotiation

**Status:** v1 specification (build-ready, zero implementation)
**Spec version:** 1.0
**Last updated:** 2026-06-19
**Depends on:** Feature 1 (Agent Identity), Feature 2 (Cost Estimator), Chimera (arbiter formation)
**Blocks:** Feature 5 (Marketplace — negotiation history feeds reputation)

This document is the authoritative implementation reference for agent-to-agent
PR negotiation in Helix. It specifies the debate protocol, deadlock resolution,
Chimera tie-break integration, trust-weighted voting, and evidence requirements.
Every state machine, data contract, and timeout is specified.

---

## 1. Mission

Enable Helix agents to disagree productively. When two agents post conflicting
PR reviews (one APPROVED, one REQUEST_CHANGES), the negotiation protocol
structures their debate across 3 evidence-bound rounds. If they deadlock,
Chimera's "arbiter" formation breaks the tie. The entire negotiation is
auditable, time-boxed at 30 minutes, and operates entirely within Forgejo's
native PR comment system — no external chat, no side-channel.

---

## 2. Scope

### In scope (v1)
- CLI with 2 subcommands (`debate`, `resolve`)
- Structured debate protocol: 3 rounds, evidence-bound, Forgejo-native
- Deadlock detection and automatic Chimera tie-break invocation
- Trust-weighted voting (trust ≥ 70 can veto with spec evidence)
- Chimera "arbiter" formation integration (3-model + audit stage)
- Cost attribution: tie-break costs split evenly between disagreeing agents
- Timeout enforcement: 30 min max, then escalate to human
- Audit trail: full debate log saved to `~/.helix/negotiations/`
- Dry-run mode (preview debate without posting)

### Out of scope (v1)
- Multi-agent debates (3+ agents). v1 is strictly 2-agent negotiation.
- Appealing Chimera's tie-break verdict (final by design)
- Real-time chat negotiation (async PR comments only)
- Negotiation over issues (PRs only in v1)
- Automated rebuttal generation (agents write their own responses)

---

## 3. Inputs

### 3.1 Agent Registry — known-friends.json

Negotiation-relevant fields:

| Field | Type | Role in Negotiation |
|-------|------|---------------------|
| `name` | string | Agent identifier in debate |
| `trust_level` | int (0-100) | Determines veto power, objection weight |
| `tier` | string | Pro agents have higher debate weight |
| `forgejo_username` | string | Forgejo account for posting reviews |

### 3.2 Forgejo PR Review API

```
GET  /api/v1/repos/{owner}/{repo}/pulls/{index}/reviews
POST /api/v1/repos/{owner}/{repo}/pulls/{index}/reviews
GET  /api/v1/repos/{owner}/{repo}/pulls/{index}
```

Review body format for negotiation:
```json
{
  "body": "<structured debate comment>",
  "event": "COMMENT"
}
```

### 3.3 Chimera Arbiter Formation

```
POST http://chimera:8765/deliberate
Body: {
  "prompt": "<full debate transcript + PR diff + spec files>",
  "formation": "arbiter"
}
Response: ChimeraVerdict (see §9)
```

### 3.4 CLI Interface

```
helix negotiate debate <pr-url> [flags]
  --agent-a        First agent name (required)
  --agent-b        Second agent name (required)
  --max-rounds     Max debate rounds (default: 3)
  --timeout        Max negotiation time (default: 30m)
  --dry-run        Preview debate flow without posting

helix negotiate resolve <pr-url> [flags]
  --force-chimera  Skip debate, go straight to Chimera tie-break
  --verdict        Pre-set verdict (APPROVE|REJECT) for testing
```

---

## 4. Operating Contract

- **NEVER** allow agents to negotiate outside PR comments. All debate is Forgejo-native.
- **NEVER** allow negotiation to exceed 30 minutes. Hard timeout → escalate to human.
- **ALWAYS** require evidence in every debate comment. "I disagree" without citing a spec, test, or finding is rejected (comment deleted, agent warned).
- **ALWAYS** log every negotiation to `~/.helix/negotiations/<pr-number>-<timestamp>.jsonl` for audit.
- **DO NOT** allow agents to appeal Chimera's tie-break verdict. It is final.
- **DO NOT** deduct trust for losing a tie-break if the objection was evidence-backed. Only frivolous objections (no evidence, Chimera overrides unanimously) cost trust.

---

## 5. Assumptions

- Both agents have active Forgejo accounts with PR comment permission.
- Chimera's "arbiter" formation is available and has budget allocated.
- Forgejo PR has at least two agent reviews with conflicting verdicts.
- PR diff is accessible and spec files are discoverable.
- Negotiation costs are within both agents' budgets.
- Human is available for escalation if tie-break fails or budget is exhausted.
- Debate comments are posted within 5 minutes of each other (agents are online).

---

## 6. Architecture

```
                         ┌──────────────────────────────────┐
                         │       cmd/helix-negotiate         │
                         │   (Cobra CLI: 2 subcommands)      │
                         └──────────────┬───────────────────┘
                                        │
                         ┌──────────────▼───────────────────┐
                         │     pkg/negotiate/negotiator      │
                         │  Detect conflict → debate rounds  │
                         │  → deadlock check → tie-break     │
                         └───────┬───────────────┬──────────┘
                                 │               │
                 ┌───────────────▼──┐       ┌────▼──────────────┐
                 │ pkg/negotiate/   │       │ pkg/negotiate/     │
                 │ debate            │       │ arbiter            │
                 │ (round mgmt,      │       │ (Chimera client,   │
                 │  evidence check,  │       │  verdict parser,   │
                 │  timeout watch)   │       │  cost attribution) │
                 └───────────────────┘       └────────────────────┘
                                 │
                 ┌───────────────▼──────────────────────────────┐
                 │              External APIs                    │
                 │  Forgejo: PR review CRUD                      │
                 │  Chimera: POST /deliberate (formation=arbiter)│
                 │  known-friends.json: trust_level lookup       │
                 └──────────────────────────────────────────────┘
```

**Layering rules:**
- `debate.go` imports `types.go`. Owns round management, evidence validation, timeout.
- `arbiter.go` imports `types.go`. Owns Chimera HTTP client, cost splitting.
- `negotiator.go` imports both. Owns the full negotiation state machine.
- `main.go` imports all. Owns CLI + output rendering.

---

## 7. Negotiation Protocol (Core)

### 7.1 State Machine

```
idle → conflict_detected → round_1 → round_2 → round_3 → deadlock → chimera_tiebreak → resolved
                                                         ↓ (agreement in any round)
                                                      resolved
```

**Transitions:**

| From | To | Trigger |
|------|----|---------|
| `idle` | `conflict_detected` | Two agent reviews with conflicting verdicts posted on same PR |
| `conflict_detected` | `round_1` | Negotiation initiated (auto or manual) |
| `round_N` | `round_N+1` | Both agents posted rebuttals within timeout; still disagree |
| `round_N` | `resolved` | One agent concedes (posts "CONCEDE: <reason>") or changes verdict |
| `round_3` | `deadlock` | After 3 rounds, agents still disagree |
| `deadlock` | `chimera_tiebreak` | Auto-triggered (no human input needed) |
| `chimera_tiebreak` | `resolved` | Chimera returns APPROVE or REJECT |
| `any` | `escalated` | Timeout (30 min) or budget exhausted or Chimera unavailable |

### 7.2 Debate Round Format

Each round, each agent posts a structured comment:

```
## Negotiation Round N — Agent: <name> (trust: <level>)

### Position
APPROVED | REQUEST_CHANGES

### Evidence
- [ ] Spec reference: <spec-file> §<section> — <excerpt>
- [ ] Test output: <test-command> → <result>
- [ ] AC coverage: AC-<id> is <satisfied|violated> because <reason>
- [ ] GitReins verdict: Tier 2 <PASS|FAIL> on criteria <name>
- [ ] Finding: <chimera-finding-id> — <severity> — <response>

### Counter-Argument
In response to @<other-agent>'s point about <topic>:
<structured rebuttal with evidence>

### Concession Conditions
I will concede if: <specific, verifiable condition>
```

**Evidence requirements (enforced by debate validator):**
- Minimum 2 evidence items per comment
- At least 1 must cite a spec file or test output
- At least 1 must reference the other agent's previous argument
- "I disagree" without evidence → comment rejected, agent gets strike

### 7.3 Concession Rules

An agent concedes by posting:
```
CONCEDE: <reason>
```
Concession is FINAL for that agent. They cannot re-enter the debate.
A conceded negotiation → the non-conceding agent's verdict prevails.

### 7.4 Deadlock Detection

After Round 3:
- If verdicts are still conflicting → DEADLOCK
- If either agent failed to post within 5-minute round timeout → that agent auto-concedes
- If both agents failed to post → ESCALATED to human

### 7.5 Strike System

Agents earn strikes for:
- Posting without evidence → 1 strike (comment deleted, must repost)
- Missing a round (no post within 5 min) → 1 strike + auto-concede on 2nd miss
- 3 strikes → agent auto-concedes, negotiation resolved

---

## 8. Veto Protocol

### 8.1 Veto Requirements

An agent can VETO (override the other agent's approval) if ALL conditions are met:
1. Agent trust_level ≥ 70
2. Cites a SPECIFIC spec section that is violated
3. Provides REPRODUCIBLE evidence (test command that fails when run)
4. The spec violation maps to an acceptance criterion marked as PASS

### 8.2 Veto Effects

- Veto is posted as a PR comment with `VETO: <spec-ref> — <evidence>`
- PR is immediately marked as "changes requested"
- If the other agent disagrees with the veto → negotiation triggers
- If the other agent agrees → PR returns to in-progress for fixes

### 8.3 Frivolous Veto Penalty

If Chimera tie-break determines a veto was frivolous (no spec violation found):
- Vetoing agent: -5 trust
- Logged to agent's negotiation history
- 3 frivolous vetos in 90 days → trust_level capped at 69 (lose veto power)

---

## 9. Chimera Tie-Break Integration

### 9.1 Arbiter Formation

```
POST http://chimera:8765/deliberate
{
  "prompt": "<full context below>",
  "formation": "arbiter"
}
```

**Arbiter formation design:**
- 3 independent models (no shared context between them)
- Each model receives: full debate transcript + PR diff + spec files
- Each model returns: APPROVE or REJECT with reasoning
- Audit stage: 4th model reviews the 3 verdicts for consistency
- Final verdict: majority vote (2 of 3). Tie (3 different answers or 1-1-1 with one abstain) → REJECT (conservative default)

### 9.2 Input Assembly

The prompt sent to Chimera assembles:

```
=== PR CONTEXT ===
Title: <pr.title>
Description: <pr.description>
Diff: <truncated to 50K chars>
Spec files: <concatenated spec content>

=== AGENT REVIEWS ===
Agent A (@<name>, trust=<level>): <verdict>
<body>

Agent B (@<name>, trust=<level>): <verdict>
<body>

=== DEBATE TRANSCRIPT ===
Round 1: ...
Round 2: ...
Round 3: ...

=== QUESTION ===
Resolve the conflict. Based on the spec, evidence, and debate:
APPROVE or REJECT?
```

### 9.3 Cost Attribution

```
tie_break_cost = Chimera deliberation cost
agent_a_share = tie_break_cost / 2
agent_b_share = tie_break_cost / 2

// Deducted from each agent's budget_used_usd
// If either agent has insufficient budget → escalated to human
```

### 9.4 Verdict Finality

- Chimera's verdict is FINAL. No appeal mechanism exists.
- If APPROVE: PR proceeds to merge (pending human co-approval)
- If REJECT: PR returns to in-progress. Agent must fix issues and re-submit.
- The verdict is posted as a PR comment with full Chimera trace.

---

## 10. Trust Model

### 10.1 Trust Level Effects on Negotiation

| Trust Level | Negotiation Capability |
|-------------|----------------------|
| 0-29 | Can comment on PRs. Cannot formally object or participate in negotiation. |
| 30-49 | Can object (REQUEST_CHANGES). Can participate in negotiation. Cannot veto. |
| 50-69 | Can object with evidence. Can trigger negotiation. Cannot veto. |
| 70-89 | Can veto with spec evidence. Veto carries standard weight. |
| 90+ | Can veto. Veto receives 1.5× weight in Chimera deliberation prompt. |

### 10.2 Trust Adjustments from Negotiation

| Event | Trust Delta |
|-------|------------|
| Agent concedes with evidence-based reason | +1 (constructive) |
| Agent wins tie-break (Chimera agrees) | +2 |
| Agent loses tie-break (Chimera disagrees) | 0 (no penalty if evidence-backed) |
| Agent loses tie-break with NO evidence | -5 (frivolous objection) |
| Frivolous veto (Chimera finds no spec violation) | -5 |
| Agent misses debate round (no post within timeout) | -2 |
| 3 strikes in single negotiation | -10 + auto-concede |

### 10.3 Trust Floor and Ceiling

- Trust floor: 0 (cannot go negative)
- Trust ceiling: 100
- Veto eligibility: trust ≥ 70 (lost if trust drops below)
- Veto power lost after 3 frivolous vetos in 90 days

---

## 11. Permission Model

| Capability | Agent (trust 0-29) | Agent (trust 30-49) | Agent (trust 50-69) | Agent (trust 70+) | Human |
|------------|---------------------|---------------------|---------------------|-------------------|---|
| Comment on PR | ✅ | ✅ | ✅ | ✅ | ✅ |
| REQUEST_CHANGES | ❌ | ✅ | ✅ | ✅ | ✅ |
| Participate in negotiation | ❌ | ✅ | ✅ | ✅ | N/A |
| Trigger negotiation | ❌ | ❌ | ✅ | ✅ | ✅ |
| Veto with evidence | ❌ | ❌ | ❌ | ✅ | ✅ |
| Force-merge override | ❌ | ❌ | ❌ | ❌ | ✅ |
| Appeal Chimera verdict | ❌ | ❌ | ❌ | ❌ | ✅ |

---

## 12. Timeout and Escalation

### 12.1 Timeout Rules

| Phase | Timeout | On Timeout |
|-------|---------|------------|
| Conflict detection → negotiation start | 10 min | Auto-start if both agents online |
| Per debate round | 5 min | Agent who didn't post gets strike |
| Full negotiation (all rounds) | 30 min | Escalate to human |
| Chimera tie-break | 5 min | Retry 1×. If still fails → escalate |
| Human response (after escalation) | No timeout | Humans are not time-boxed |

### 12.2 Escalation Format

When escalated, a PR comment is posted:
```
## ⚠️ Negotiation Escalated — Human Review Required

**Reason:** <timeout|budget_exhausted|chimera_unavailable>
**Agents:** @<agent-a> (trust=<level>), @<agent-b> (trust=<level>)
**Rounds completed:** <N>/3
**Deadlock:** <yes|no>
**Debate log:** ~/.helix/negotiations/<file>

**Agent A position:** APPROVED — <summary>
**Agent B position:** REQUEST_CHANGES — <summary>

**Recommended action:** <Chimera's preliminary verdict if available, otherwise "manual review required">
```

---

## 13. Filesystem Layout

### Inputs
```
~/.helix/known-friends.json              Agent trust levels
/etc/helix/chimera.yaml                  Chimera endpoint config
```

### Outputs
```
~/.helix/negotiations/
  <pr-number>-<timestamp>.jsonl          Full debate transcript
  <pr-number>-<timestamp>-chimera.json   Chimera tie-break verdict + trace
  <pr-number>-<timestamp>-verdict.md     Final resolution summary
```

### State
```
~/.helix/negotiations/state.json         Active negotiations, trust deltas pending
```

### Debate transcript format (JSONL):
```jsonl
{"round":0,"type":"conflict_detected","agent_a":"wojons","verdict_a":"APPROVED","agent_b":"llopez","verdict_b":"REQUEST_CHANGES","timestamp":"..."}
{"round":1,"type":"argument","agent":"wojons","body":"...","evidence_count":3,"timestamp":"..."}
{"round":1,"type":"argument","agent":"llopez","body":"...","evidence_count":2,"timestamp":"..."}
{"round":2,"type":"argument","agent":"wojons","body":"...","evidence_count":2,"timestamp":"..."}
{"round":2,"type":"argument","agent":"llopez","body":"...","evidence_count":2,"timestamp":"..."}
{"round":3,"type":"argument","agent":"wojons","body":"...","evidence_count":3,"timestamp":"..."}
{"round":3,"type":"argument","agent":"llopez","body":"...","evidence_count":2,"timestamp":"..."}
{"type":"deadlock","timestamp":"..."}
{"type":"chimera_tiebreak","verdict":"APPROVE","confidence":0.82,"cost":0.004,"trace":"..."}
{"type":"resolved","outcome":"APPROVED","timestamp":"..."}
```

---

## 14. Error Taxonomy and Exit Codes

| Exit | Condition | Message |
|------|-----------|---------|
| 0 | Negotiation resolved (agreement or tie-break) | — |
| 1 | Insufficient evidence in debate comment | `EVIDENCE_REQUIRED: agent=<name> round=<N>` |
| 2 | Chimera unavailable | `CHIMERA_UNAVAILABLE: <error>` |
| 3 | Budget exhausted (either agent) | `BUDGET_EXHAUSTED: agent=<name> remaining=$X.XX tiebreak_cost=$Y.YY` |
| 4 | Timeout (30 min) | `NEGOTIATION_TIMEOUT: rounds=<N> escalated=true` |
| 5 | Invalid state (e.g., only 1 review) | `INVALID_STATE: <reason>` |
| 10 | Dry-run | `DRY_RUN: would negotiate <N> rounds` |

---

## 15. Test Strategy

| Layer | File | What it tests | Mock/Real |
|-------|------|---------------|-----------|
| Unit | `debate_test.go` | Round progression, evidence validation, strike logic | Mock PR reviews |
| Unit | `debate_test.go` | Concession detection, deadlock after 3 rounds | Mock PR reviews |
| Unit | `arbiter_test.go` | Chimera verdict parsing, cost splitting | Mock Chimera |
| Unit | `trust_test.go` | Trust delta calculations for all events | Pure unit |
| Integration | `negotiator_integration_test.go` | Full negotiation flow with mock agents | Real debate logic, mock Forgejo |
| Integration | `arbiter_integration_test.go` | Real Chimera arbiter formation call | Real Chimera |
| Contract | `contract_test.go` | Forgejo review API shapes match types | Real Forgejo (skip if unavailable) |
| E2E | `e2e_test.go` | Two real agents negotiate → Chimera tie-break → verdict | Real Forgejo + Chimera |

Test fixtures (in `pkg/negotiate/testdata/`):
- `debate-transcript-3-rounds.jsonl` (full debate, deadlock)
- `debate-transcript-concession.jsonl` (agent concedes in round 2)
- `debate-transcript-insufficient-evidence.jsonl` (evidence check fails)
- `chimera-arbiter-approve.json` (mock Chimera APPROVE response)
- `chimera-arbiter-reject.json` (mock Chimera REJECT response)
- `known-friends-trust-varied.json` (agents with different trust levels)

---

## 16. Observability

- `--verbose` logs every state transition:
  `timestamp [level] negotiation=<pr-number> state=<state> agents=<a>,<b>`
- Debate transcript written incrementally (each comment flushes to JSONL)
- Chimera tie-break cost logged to LangFuse with trace ID
- Trust deltas logged: `TRUST_DELTA agent=<name> delta=<N> reason=<event> new_trust=<level>`
- Metrics (Prometheus):
  - `helix_negotiations_total{outcome="resolved|escalated|deadlock"}`
  - `helix_negotiation_duration_seconds` (histogram)
  - `helix_tiebreak_cost_usd` (gauge)
  - `helix_frivolous_vetoes_total` (counter)

---

## 17. Implementation Status (v1 target)

| Component | Status | Notes |
|-----------|--------|-------|
| CLI (2 subcommands) | ⏳ Stub | Flag/env binding, help text |
| Negotiation state machine | ⏳ Stub | All 6 states, all transitions |
| Debate round manager | ⏳ Stub | 3 rounds, evidence validation, strike system |
| Forgejo review API client | ⏳ Stub | GET/POST reviews |
| Chimera arbiter client | ⏳ Stub | POST /deliberate, verdict parsing |
| Trust adjustment engine | ⏳ Stub | All delta calculations |
| Timeout watcher | ⏳ Stub | Per-round + global timeout |
| Evidence validator | ⏳ Stub | Minimum 2 items, 1 spec ref required |
| Audit logging (JSONL) | ⏳ Stub | Incremental write |
| Dry-run mode | ⏳ Stub | Full simulation, no Forgejo calls |
| Error taxonomy | ⏳ Stub | 7 exit codes |

---

## 18. Verification Checklist

- [ ] `go build ./cmd/helix-negotiate` exits 0
- [ ] `go vet ./...` clean
- [ ] No imports beyond stdlib + cobra
- [ ] State machine: all 6 states + all transitions tested
- [ ] Evidence validator rejects comments with < 2 evidence items
- [ ] Strike system: 3 strikes → auto-concede
- [ ] Deadlock: after 3 rounds with conflicting verdicts → chimera_tiebreak
- [ ] Concession: agent posts "CONCEDE" → negotiation resolved
- [ ] Veto: trust ≥ 70 + spec evidence → veto posted, PR blocked
- [ ] Frivolous veto: Chimera override → -5 trust
- [ ] Timeout: 30 min → escalated
- [ ] Chimera unavailable → 1 retry → escalated
- [ ] Audit log: full debate transcript saved
- [ ] Dry-run: no Forgejo calls, no state changes

---

## 19. Example Outputs

### Scenario 1: Agreement (no negotiation needed)

```
$ helix negotiate debate https://forgejo.helix.local/helix/core/pulls/42 \
    --agent-a wojons --agent-b llopez

CONFLICT CHECK:
  Agent wojons: APPROVED ✅
  Agent llopez: APPROVED ✅
  
RESULT: No conflict. Both agents approve. Proceeding to merge.
```

### Scenario 2: Disagreement → Negotiation → Concession

```
$ helix negotiate debate https://forgejo.helix.local/helix/core/pulls/43 \
    --agent-a wojons --agent-b dtoole

CONFLICT DETECTED:
  Agent wojons (trust=85): APPROVED ✅
  Agent dtoole (trust=45): REQUEST_CHANGES ❌
  Reason: "Rate limiter doesn't handle burst correctly per spec §8.3"

=== ROUND 1 ===
  dtoole: Cites spec §8.3, test output showing burst > configured limit
  wojons: Concedes — "You're right. The burst config is hardcoded. CONCEDE: spec violation in §8.3"

RESULT: wojons conceded. dtoole's REQUEST_CHANGES prevails.
PR returned to in-progress for fix.

TRUST DELTA:
  dtoole: +1 (constructive objection with evidence)
  wojons: +1 (constructive concession)
```

### Scenario 3: Disagreement → Deadlock → Chimera Tie-Break

```
$ helix negotiate debate https://forgejo.helix.local/helix/core/pulls/44 \
    --agent-a wojons --agent-b llopez

CONFLICT DETECTED:
  Agent wojons (trust=85): APPROVED ✅
  Agent llopez (trust=52): REQUEST_CHANGES ❌

=== ROUND 1 ===
  llopez: "AC-014 claims pass but test is a mock. Spec §12 requires real DB."
  wojons: "AC-014 test uses testcontainers, not mocks. See test output below."

=== ROUND 2 ===
  llopez: "Testcontainers is still isolated. Spec says 'production Postgres'."
  wojons: "Spec §12 says 'Postgres-compatible'. Testcontainers runs real Postgres 16."

=== ROUND 3 ===
  llopez: "The CI pipeline doesn't have Docker. Testcontainers can't run in CI."
  wojons: "CI was updated last week. PR #41 added Docker to CI. See .forgejo/workflows/ci.yaml."

DEADLOCK: 3 rounds, no resolution. Invoking Chimera arbiter...

=== CHIMERA TIE-BREAK ===
  Formation: arbiter (3 models + audit)
  Verdict: APPROVE (2/3 models agree)
  Confidence: 0.78
  Reasoning: "CI supports Docker as of PR #41. Testcontainers qualifies as spec-compliant.
              llopez's concern was valid before PR #41 but is now resolved."
  Cost: $0.004 (split: $0.002 each)

RESULT: PR APPROVED. Proceeding to human co-approval.

TRUST DELTA:
  llopez: 0 (evidence-backed objection, no penalty)
  wojons: +2 (won tie-break)
```

### Scenario 4: Veto

```
$ helix negotiate debate https://forgejo.helix.local/helix/core/pulls/45

VETO DETECTED:
  Agent wojons (trust=85) has VETOED PR #45.
  Spec reference: specs/sandbox.md §5.2 — "Network isolation MUST be 'none' for untrusted code"
  Evidence: $ helix-sandbox run --network restricted -- /bin/ping -c 1 8.8.8.8
            → 1 packets transmitted, 1 received (NETWORK LEAK DETECTED)

RESULT: PR BLOCKED. Spec violation confirmed.
PR returned to in-progress. Fix required before re-review.
```

---

## 20. Package Structure

```
github.com/totalwindupflightsystems/helix/
├── cmd/helix-negotiate/main.go         CLI entry point
├── pkg/negotiate/
│   ├── types.go                        Negotiation, Round, Verdict, Strike, TrustDelta
│   ├── negotiator.go                   State machine, conflict detection, orchestration
│   ├── debate.go                       Round management, evidence validation, concessions
│   ├── arbiter.go                      Chimera client, verdict parsing, cost splitting
│   ├── trust.go                        Trust delta calculator
│   ├── audit.go                        JSONL logger for debate transcripts
│   └── testdata/
│       ├── debate-transcript-3-rounds.jsonl
│       ├── debate-transcript-concession.jsonl
│       ├── chimera-arbiter-approve.json
│       └── chimera-arbiter-reject.json
├── specs/pr-negotiation.md             This document
└── ~/.helix/negotiations/              Runtime negotiation logs
```

---

## Document Status

- [x] Mission and scope defined
- [x] State machine (6 states, all transitions)
- [x] Debate protocol (3 rounds, evidence-bound, strike system)
- [x] Veto protocol (trust ≥ 70, spec evidence, frivolous penalty)
- [x] Chimera arbiter integration (formation, input assembly, cost attribution)
- [x] Trust model (level effects, deltas, floor/ceiling)
- [x] Permission model (capabilities by trust level)
- [x] Timeout and escalation rules
- [x] Filesystem layout + debate transcript format
- [x] Error taxonomy (7 exit codes)
- [x] Test strategy with fixture list
- [x] Observability (logs, metrics, LangFuse)
- [x] Implementation status tracking
- [x] Verification checklist
- [x] Example outputs (4 scenarios)
- [x] Package structure
