# Adversarial Review — Multi-Model Defense Against AI Code Failures

## Why This Exists

Current AI code review fails in systematic, documented ways:

1. **Confirmation bias exploit** (arXiv 2603.18740): Crafting a commit message to frame code as "correct" causes LLMs to miss vulnerabilities they would otherwise detect. This is a supply-chain attack vector — malicious actors can bypass security review by writing reassuring commit messages.

2. **Systematic overcorrection** (arXiv 2603.00539): "Richer prompting" does not improve review accuracy. Prompt enhancements shift the error profile without reducing total error rate. LLMs asked to "explain their reasoning" become more verbose but no more accurate.

3. **Blind spot on architecture:** LLMs evaluate syntax and patterns correctly but cannot judge architectural decisions, business logic correctness, or cross-component impact.

4. **False positive tax:** Top AI review tools produce 5–10% false positives. Human reviewers waste time investigating findings that are not real bugs.

5. **Context collapse on large PRs:** PRs exceeding 500 changed lines degrade review quality sharply.

6. **Anthropic's own admission:** Regressions passed "multiple human and automated code reviews, unit tests, end-to-end tests, automated verification, and dogfooding." If Anthropic's review pipeline failed, a single LLM reviewer has no chance.

Helix's adversarial review system is designed to catch what single-model review misses by forcing disagreement, testing assumptions, and verifying claims with evidence — not assertions.

## Architecture

### Three-Layer Review Pipeline

```
GitReins Tier 1 (static)
    ↓ PASS
Tier 2 — Multi-Model Adversarial Review
    ├── Model A: Primary review (structural, correctness)
    ├── Model B: Adversarial review (find what A missed)
    ├── Model C: Audit review (verify B's claims are real)
    └── Consensus Engine: merge verdicts with scoring
    ↓ CONSENSUS
Tier 3 — Evidence Verification
    ├── Run tests from all three models' suggestions
    ├── Verify edge cases actually fail
    └── Confirm fixes actually resolve issues
    ↓ VERIFIED
Merge gate
```

### Confirmation Bias Defense

Before any review model sees the code, the commit message is rewritten by a `bias-stripper`:

```
Original: "Fixed the auth edge case, all tests pass, ready to merge"
Stripped:  "Modified auth module. Verify correctness."
```

The bias-stripper:
- Removes all evaluative language ("fixed," "correct," "ready," "passes")
- Removes confidence assertions ("tested locally," "works on my machine")
- Strips emoji and emotional framing ("🚀", "🔥", "should be fine")
- Normalizes formatting to prevent priming effects
- Preserves factual information (which files changed, what the intent was)

All three review models receive the stripped commit message. The original is archived for audit but never shown to reviewers.

### Model Formation Strategy

Chimera formations for adversarial review use intentional model diversity:

| Role | Model Property | Example |
|---|---|---|
| Primary reviewer | Strongest code reasoning | GPT-5.5, Claude Opus 4.6 |
| Adversarial reviewer | Different architecture/training | DeepSeek V4, Gemini 2.5 Pro |
| Audit reviewer | Fast, cheap, different provider | Owl Alpha, Llama 4 |

**Diversity rules:**
- No two models from the same provider
- At least one model trained with different RLHF preferences
- At least one model with different context window architecture
- Rotation: model assignments change per-review to prevent adversarial adaptation

### Anti-Overcorrection Protocol

Based on the arXiv finding that "richer prompting shifts error profile without improving accuracy":

1. **Fixed prompt structure** — Review prompts are version-locked and hash-attested. No per-PR prompt engineering.
2. **Binary pass/fail criteria** — Each review criterion is verifiable as true/false, not "somewhat improved."
3. **Evidence requirement** — Every finding MUST cite a specific line or test failure. "Seems like" findings are discarded.
4. **False positive tracking** — Every finding a human dismisses feeds back into model evaluation. Models with high false positive rates are down-weighted.

## Review Criteria by Change Category

### Contract Changes (API signatures, data schemas, auth)
- Full adversarial formation (all 3 models)
- Consensus threshold: 3/3 agreement required
- Must pass integration test suite
- Must have migration path for existing consumers

### Behavioral Changes (business logic, algorithms, state machines)
- Primary + adversarial review (2 models)
- Consensus threshold: 2/2 agreement
- Must include edge case test coverage
- Race condition analysis required for concurrent code

### Resilience Changes (error handling, retry, circuit breakers)
- Primary review + automated property testing
- Must demonstrate failure mode recovery
- Timeout and retry behavior must be explicit

### Cosmetic Changes (formatting, comments, variable names)
- Single-model review
- Auto-merge if all Tier 1 guards pass

## Adversarial Agent Techniques

Helix dispatches specialized adversarial agents during review:

| Agent | Trigger | Function |
|---|---|---|
| `@assumption-buster` | Any behavioral change | Enumerates and challenges every implicit assumption |
| `@redteam` | Auth, crypto, secrets handling | Attempts to find exploit paths |
| `@chaos-engineer` | Resilience changes | Injects faults to verify recovery behavior |
| `@cost-auditor` | All changes | Estimates token cost and flags budget overruns |

These agents are NOT reviewers. They are prosecutors. Their job is to prove the code wrong. If they can't, the code passes.

## Evidence Bundles

Every review produces an evidence bundle — a signed, hash-chained artifact:

```json
{
  "pr_url": "https://forgejo.example.com/org/repo/pulls/42",
  "review_id": "uuid",
  "timestamp": "ISO8601",
  "formation": {
    "primary": {"model": "gpt-5.5", "provider": "openai"},
    "adversarial": {"model": "deepseek-v4-pro", "provider": "deepseek"},
    "audit": {"model": "owl-alpha", "provider": "openrouter"}
  },
  "bias_stripped_commit": "sha256-of-stripped-message",
  "original_commit": "sha256-of-original",
  "findings": [
    {
      "model": "adversarial",
      "severity": "high",
      "type": "race_condition",
      "file": "pkg/auth/session.go",
      "line": 142,
      "description": "Token refresh and invalidation race under concurrent access",
      "evidence": "test_run_id: uuid, output: 'FAIL: TestConcurrentTokenRefresh'"
    }
  ],
  "consensus": {
    "primary_verdict": "pass_with_notes",
    "adversarial_verdict": "block",
    "audit_verdict": "confirm_adversarial",
    "resolution": "blocked",
    "tie_breaker": null
  },
  "signatures": {
    "primary": "ed25519-sig",
    "adversarial": "ed25519-sig",
    "audit": "ed25519-sig"
  }
}
```

Evidence bundles are stored in DuckBrain and linked from the merge commit. Any merge without a valid evidence bundle is auto-reverted.

## False Positive Feedback Loop

When a human dismisses a review finding as false positive:

1. The finding is tagged `human_dismissed` with reason
2. The model that produced it receives a trust penalty
3. After 10 dismissed findings from the same model, it is flagged for re-evaluation
4. Re-evaluation runs the model against a curated test suite of known-true and known-false findings
5. Models with >15% false positive rate are removed from rotation

This prevents human reviewers from drowning in AI-generated noise — the exact problem killing AI review tool adoption.

## Integration Points

- **Chimera:** Formation engine for multi-model assignments
- **GitReins Tier 2:** LLM evaluator that consumes evidence bundles
- **GitReins pre-commit:** Blocks merges without valid evidence bundles
- **Forgejo:** PR status checks display review verdicts inline
- **Marketplace:** Agent trust scores reflect review participation quality
- **PromptFoo:** Regression tests for review prompt templates
