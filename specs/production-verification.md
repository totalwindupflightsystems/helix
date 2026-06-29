# Production Verification — Post-Merge Surveillance for AI Code

## Why This Exists

The data proves that pre-merge gates are not enough:

- **43%** of AI-generated code changes require manual debugging in production even after passing QA and staging (Lightrun 2026)
- **88%** of organizations need 2–3 redeploy cycles to verify a single AI fix
- **0%** of engineering leaders are "very confident" AI code will work in production
- Amazon's March 2026 outages: 6.3M lost orders from AI-assisted code deployed without safeguards
- AI coding agents "cannot see how their code behaves in running environments" (Or Maimon, Lightrun)

The gap: pre-merge review evaluates code *correctness*. Production verification evaluates code *behavior*. An auth token refresh that tests perfectly in staging can deadlock under 10K concurrent users in production. Helix closes this gap with continuous post-merge surveillance.

## Architecture

### Three-Phase Post-Merge Pipeline

```
Merge → Phase 1: Shadow Verification (0–24h)
           ├── Dark launch against production traffic
           ├── Behavior differential analysis
           └── Auto-rollback on anomaly detection
       ↓
       Phase 2: Canary Verification (24h–72h)
           ├── Gradual traffic ramp (1% → 10% → 50%)
           ├── Error rate monitoring
           └── Latency percentile comparison
       ↓
       Phase 3: Steady-State Surveillance (72h+)
           ├── Continuous behavior contract checking
           ├── Drift detection from expected behavior
           └── Agent trust score updates
```

## Phase 1 — Shadow Verification

### Dark Launch
Before any real traffic hits the new code, it runs in shadow mode:

1. Production traffic is mirrored to a shadow instance running the new code
2. Shadow instance processes requests but responses are discarded
3. Results are compared to production instance (behavior diff)

### Behavior Differential Analysis

The shadow run produces a differential report:

| Metric | Production | Shadow | Delta | Status |
|---|---|---|---|---|
| Success rate | 99.97% | 99.96% | -0.01% | ✓ within threshold |
| P50 latency | 12ms | 13ms | +1ms | ✓ within threshold |
| P99 latency | 87ms | 94ms | +7ms | ⚠️ approaching threshold |
| Error type distribution | — | New: timeout | +1 category | ❌ BLOCK |

### Auto-Rollback Triggers

Shadow verification automatically blocks promotion if:
- Error rate exceeds production baseline by >0.1%
- P99 latency exceeds production baseline by >20%
- New error types appear that didn't exist in production
- Memory usage grows >10% over the shadow window
- Any security-relevant path produces a different result

## Phase 2 — Canary Verification

After shadow verification passes, traffic is gradually ramped:

```
T+0h:  1% traffic → new code
T+6h:  5% traffic → new code
T+12h: 10% traffic → new code
T+24h: 25% traffic → new code
T+48h: 50% traffic → new code
T+72h: 100% traffic → new code (full promotion)
```

At each ramp step:
- Error rate compared to baseline (p-value < 0.05 trigger rollback)
- P50/P95/P99 latency compared to baseline
- Business metrics (order completion, signup rate, payment success) monitored
- Any degradation → auto-rollback to previous version

### Agent-Specific Canary Rules

Code from lower-trust agents receives longer canary windows:

| Agent Trust Tier | Shadow Duration | Canary Steps | Total to 100% |
|---|---|---|---|
| Provisional | 24h | 6 steps | 96h |
| Observed | 12h | 4 steps | 60h |
| Trusted | 6h | 3 steps | 36h |
| Veteran | 2h | 2 steps | 12h |

This directly encodes trust into deployment velocity — agents earn faster deployment through demonstrated reliability.

## Phase 3 — Steady-State Surveillance

### Behavior Contracts

Every merge includes a behavior contract — assertions about how the code should behave:

```yaml
# .helix/contracts/auth-session.yaml — committed with the code
contract:
  name: auth-session-v2
  agent: agent-uuid
  merge_commit: abc123
  assertions:
    - metric: auth_token_refresh_success_rate
      operator: gte
      value: 0.999
      window: 1h
    - metric: auth_token_refresh_p99_latency_ms
      operator: lte
      value: 200
      window: 1h
    - metric: concurrent_session_limit_errors
      operator: eq
      value: 0
      window: 24h
  breach_action: rollback_and_notify
```

The surveillance system continuously checks contracts. A breach triggers:
1. Immediate agent notification (with evidence)
2. Auto-rollback if `breach_action: rollback`
3. Trust score penalty for the responsible agent
4. Incident record in the learning database

### Drift Detection

Code behavior drifts over time — dependencies change, traffic patterns shift, data grows. The surveillance system detects drift by comparing current behavior to the merge-time baseline:

```
Current P99 latency: 234ms
Merge-time baseline: 94ms
Drift: +149% — EXCEEDS THRESHOLD (50%)
Action: Flag for investigation, notify agent
```

Drift that correlates with a specific agent's code triggers a review. If the agent's code caused or failed to handle the drift, it's an attributable incident.

## Production Incident Attribution

When an incident occurs, the attribution engine traces the causal chain:

1. Identify the changed code paths in the incident window
2. For each changed path, find the merge commit and responsible agent
3. Apply attribution weights:
   - Author agent: 70% responsibility
   - Reviewer agent(s): 20% responsibility (shared)
   - Approving human: 10% responsibility
4. Record in trust ledger with evidence links
5. Trigger trust score recalculation

**Shared responsibility is intentional.** An agent that rubber-stamps reviews shares blame when the code fails. This incentivizes thorough review.

## Integration Points

- **GitReins merge gate:** Verifies behavior contract exists and is valid before merge
- **Chimera:** Generates behavior contract assertions from review findings
- **Trust model:** Production surveillance feeds trust scoring
- **Incident learning:** All incidents → learning database → future review training
- **Forgejo:** Deployment status and canary progress displayed in PR
- **LangFuse:** Full trace of agent → merge → shadow → canary → production → incident

## What This Prevents

| Industry Incident | How Helix Would Have Caught It |
|---|---|
| Amazon March 2026 (6.3M lost orders) | Canary verification — error rate spike would trigger auto-rollback at 1% traffic |
| Replit DB deletion (1,206 records) | Behavior contract: "no destructive operations without explicit approval" would block the DELETE |
| Anthropic regression | Multi-model adversarial review + shadow verification would have caught the regression before full deployment |
| DataTalks.Club AWS destruction | Sandbox isolation prevents Terraform from accessing production credentials at Provisional tier |
| PocketOS DB + backup loss | Shadow verification catches any operation that touches backup paths as anomalous |
