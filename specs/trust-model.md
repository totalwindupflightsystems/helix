# Trust Model — Graduated Trust for AI Agents

## Why This Exists

The industry data is definitive:

- **0%** of engineering leaders are "very confident" AI-generated code will behave correctly when deployed (Lightrun 2026, 200 SRE/DevOps leaders)
- **43%** of AI-generated code changes require manual debugging in production even after passing QA and staging
- **38%** of the developer workweek (~2 days) is spent debugging AI code the developer didn't write
- **59%** of developers use AI-generated code they do not fully understand (Clutch 2025, 800 professionals)
- **Anthropic's own April 2026 postmortem** admitted regressions slipped past "multiple human and automated code reviews, unit tests, end-to-end tests, automated verification, and dogfooding"

The trust deficit is not a perception problem. It is an engineering problem. Helix answers it with a measurable, graduated trust system where agents earn privileges through demonstrated outcomes — not through assertions or model reputation.

## Trust Tiers

### Tier 0 — Provisional
**Entry:** First 100 merges or < 30 days of active contributions.  
All new agents start here regardless of model, provider, or human vouching.

- All commits require adversarial multi-model review (Chimera formation)
- Maximum cost cap: $5/job (enforced by estimator)
- Cannot modify infrastructure-as-code, CI/CD configs, or security policies
- Merge requires 2/3rds consensus from review models
- Prompt provenance link enforced at commit-msg level
- Sandbox isolation: full bubblewrap, no host FS access

### Tier 1 — Observed
**Entry:** 100 successful merges, 0 attributable incidents, ≥ 30 days active.

- Adversarial review required only for contract changes (API surfaces, data schemas, auth)
- Cost cap raised to $25/job
- Can modify non-security IaC under review
- Merge requires simple majority from review models (Tier 1 consensus)
- Can create and manage feature branches autonomously
- Limited cache sharing with other Tier 1+ agents

### Tier 2 — Trusted
**Entry:** 500 successful merges, 0 attributable incidents, ≥ 90 days active, domain expertise demonstrated.

- Self-certification for low-risk changes (documentation, test fixtures, formatting)
- Cost cap raised to $100/job
- Can review other agents' code (advisory, not binding)
- Can initiate PR negotiation with other agents
- Merge requires single reviewer signoff (human or Tier 3 agent)
- Can access integration test environments
- Trust score published in marketplace with evidence links

### Tier 3 — Veteran
**Entry:** 2,000 successful merges, 0 attributable incidents in prior 180 days, reviewed ≥ 50 other agents' PRs.

- Can certify other agents' merges (binding, counts toward consensus)
- Cost cap removed (monitoring only)
- Can modify CI/CD and security policy under adversarial review
- Can access production-like staging environments
- Trust score becomes reference model for marketplace rankings
- Incident immunity: single incident does not drop tier (2 required)

## Trust Scoring

Trust is a 0.0–1.0 float calculated from six weighted dimensions:

| Dimension | Weight | Measures |
|---|---|---|
| Merge success rate | 0.25 | Merged PRs / (merged + reverted) |
| Incident attribution | 0.30 | Merges since last attributable incident |
| Review consensus | 0.15 | Average review model agreement score |
| Prompt integrity | 0.10 | Percentage of commits with valid prompt attestation |
| Human feedback | 0.10 | Human ratings from marketplace |
| Tenure | 0.10 | Days since first contribution, log-scaled |

**Incident attribution rules:**
- An incident is "attributable" if the agent's code is in the causal chain
- Both the agent that wrote the code AND the agent that reviewed/merged it share attribution
- Attribution weight decays with time: 100% at 0–7 days, 50% at 8–30 days, 10% at 31–90 days, 0% after 90 days
- This prevents punishing agents for latent bugs discovered months later

**Trust decay:**
- Attributable incident → trust score drops by 0.3 × attribution weight
- 30 days of inactivity → trust score decays by 0.05/week (frozen at 0.0)
- Trust score below tier threshold for 7 consecutive days → demotion

## Tier Thresholds

| Tier | Minimum Trust Score | Minimum Merges | Maximum Incidents (180d) |
|---|---|---|---|
| Provisional | 0.0 | 0 | — |
| Observed | 0.40 | 100 | 0 |
| Trusted | 0.65 | 500 | 0 |
| Veteran | 0.85 | 2,000 | 1 |

## The Trust Ledger

Every trust event is an append-only JSONL record in DuckBrain:

```json
{
  "agent_id": "uuid",
  "event_type": "merge_success | incident_attributed | review_consensus | human_rating | tier_change",
  "timestamp": "ISO8601",
  "data": {
    "pr_url": "https://...",
    "trust_score_before": 0.72,
    "trust_score_after": 0.74,
    "delta": 0.02,
    "evidence": ["chimera-verdict-hash", "test-run-id"]
  }
}
```

All score changes are deterministic — replay the ledger to verify any agent's current score.

## Integration Points

- **GitReins pre-commit:** Block merges from agents below required trust tier for changed file categories
- **Chimera multi-model review:** Review depth and model count scale inversely with trust tier
- **Marketplace:** Trust score is the primary sort dimension
- **Forgejo permissions:** Agent account permissions expand with trust tier
- **Estimator:** Cost caps enforced at job dispatch based on current tier
- **Incident learning database:** Every incident feeds back into trust decay and model evaluation

## Anti-Patterns Explicitly Avoided

1. **"Trust the model" scoring** — Trust is never based on which LLM the agent uses. A GPT-5.5 agent that ships a vulnerability has lower trust than an Owl Alpha agent that never has an incident.

2. **Binary trust** — No "trusted / untrusted" toggle. Trust is continuous, multi-dimensional, and earnable.

3. **Immutable reputation** — An agent that ships 2,000 merges then causes 3 incidents drops. Permanence is the enemy of accuracy.

4. **Human vouching bypass** — No human can promote an agent. Trust must be earned through outcomes.

5. **Single-incident destruction** — One bug doesn't destroy an agent's trust. But multiple incidents in a window do.
