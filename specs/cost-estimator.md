# Helix Feature 2 — Pre-Flight Cost Estimator

**Status:** v1 specification (build-ready, zero implementation)
**Spec version:** 1.0
**Last updated:** 2026-06-19
**Depends on:** Feature 1 (Agent Identity), GitReins v0.4.1+ (cache token tracking), OpenRouter API
**Blocks:** Feature 3 (PR Negotiation), Feature 5 (Marketplace)

This document is the authoritative implementation reference for the Helix
pre-flight cost estimator. It is intended to be sufficient for an engineer
to implement the estimator without asking clarifying questions. Every
interface, data contract, estimation algorithm, and budget enforcement rule
is specified.

---

## 1. Mission

Provide a Go CLI that estimates the token cost of any Helix agent task
**before execution begins**, checks it against the agent's remaining budget,
and either approves, denies, or escalates for human approval. The estimator
leverages GitReins v0.4.1's cache token tracking to produce accurate costs
that distinguish fresh input ($0.14/M) from cache hits ($0.014/M) — a 10x
difference that makes naive estimation useless.

The v1 deliverable is a specification + compilable Go stubs that define every
interface, plus all non-network logic (CLI, cost model, budget arithmetic,
estimation algorithm, dry-run mode, error taxonomy) implemented.

---

## 2. Scope

### In scope (v1)
- CLI with 3 subcommands (`estimate`, `check`, `report`)
- Multi-model cost model (DeepSeek, Z.AI/GLM, MiniMax, Anthropic, Google, OpenRouter)
- Estimation algorithm: task complexity → expected tokens → cost projection
- Cache-aware pricing: fresh input vs cache-hit vs cache-write vs output tokens
- Budget enforcement: per-agent weekly cap from known-friends.json
- OpenRouter API integration for real-time budget queries
- GitReins v0.4.1 `LLMUsage` struct integration (cache_read_tokens, cache_write_tokens)
- Dry-run mode (estimate without enforcing)
- Error taxonomy with machine-readable exit codes
- Concurrent estimation for multi-agent dispatch scenarios

### Out of scope (v1)
- Real-time cost tracking during agent execution (LangFuse handles this)
- Dynamic budget adjustment (manual via known-friends.json edits)
- Multi-week budget rollover
- Provider-specific discount negotiation
- Cost optimization recommendations (v2 — "use Flash instead of Pro for this task")
- Web UI / dashboard

---

## 3. Inputs

### 3.1 Agent Budget Registry — known-friends.json

Source: `/opt/hermes-demo/.hermes/h4f/known-friends.json` (H4F host).

Budget-relevant fields per agent:

| Field | Type | Example | Description |
|-------|------|---------|-------------|
| `name` (map key) | string | `"wojons"` | Agent identifier |
| `tier` | enum | `"pro"` \| `"flash"` | Pro = all models, high budget. Flash = budget models only. |
| `budget_usd_weekly` | float | `10.00` | Weekly spend cap in USD |
| `budget_used_usd` | float | `3.42` | Current period spend (updated by estimator after each approved task) |
| `openrouter_key_hash` | string | `"sha256:hex"` | Used to query OpenRouter for real-time spend |
| `trust_level` | int | `85` | Determines auto-approval threshold |

### 3.2 Provider Pricing (June 2026)

| Provider | Model | Input/M | Cache Hit/M | Output/M | Source |
|----------|-------|---------|-------------|----------|--------|
| DeepSeek | deepseek-v4-pro | $0.14 | $0.014 | $0.28 | api.deepseek.com |
| DeepSeek | deepseek-v4-flash | $0.07 | $0.007 | $0.14 | api.deepseek.com |
| Z.AI | glm-5.2 | $0.10 | $0.01 | $0.20 | api.z.ai |
| MiniMax | MiniMax-M3 | $0.20 | N/A | $0.40 | api.minimax.io |
| Anthropic | claude-sonnet-4 | $3.00 | $0.30 | $15.00 | api.anthropic.com |
| Google | gemini-2.5-pro | $1.25 | $0.31 | $10.00 | ai.google.dev |
| OpenRouter | (any) | +5% markup | varies | +5% markup | openrouter.ai |

Pricing is loaded from `~/.helix/pricing.yaml` and can be updated without code changes.

### 3.3 GitReins Evaluator Output

GitReins v0.4.1 evaluator returns `LLMUsage` structs with:
```python
@dataclass
class LLMUsage:
    prompt_tokens: int       # Total input tokens
    completion_tokens: int   # Output tokens
    total_tokens: int        # Sum
    cache_read_tokens: int   # Tokens served from cache (10x cheaper)
    cache_write_tokens: int  # Tokens written to cache (future discount)
```

For estimation (pre-execution), we cannot know actual cache tokens. We use:
- **Cache hit ratio:** Pro tier = 60%, Flash tier = 80% (budget models use cache more)
- **Cache write ratio:** 50% of non-cached input
- These ratios are configurable in `~/.helix/pricing.yaml`

### 3.4 Task Complexity Signals

The estimator receives a task description and produces a token estimate. Signals:

| Signal | Source | Weight | How it maps to tokens |
|--------|--------|--------|----------------------|
| Task type | CLI flag `--task-type` | High | `spec`=80K, `code`=120K, `review`=40K, `refactor`=200K, `test`=30K |
| File count | `--files-changed` | Medium | 5K per file for code tasks |
| Spec lines | Length of spec file | Medium | 2 tokens per spec line for planning |
| Model | `--model` flag | Direct | Determines per-token price |
| Agent count | `--agents` flag | Multiplier | N agents × per-agent estimate (capped at 5) |
| Iteration cap | `--max-iterations` | Low | +10K tokens per iteration beyond 10 |
| Git diff size | `--diff-lines` | Low | 1 token per 10 diff lines |

### 3.5 CLI Flags

```
helix estimate <task-description> [flags]

Flags:
  --task-type           spec|code|review|refactor|test (default: code)
  --model               Model name (e.g., "deepseek-v4-pro", "glm-5.2")
  --provider            Provider name (e.g., "deepseek", "zai-glm")
  --files-changed       Estimated files to modify (default: 1)
  --spec-file           Path to spec file for spec-type tasks
  --agents              Number of agents for multi-agent dispatch (default: 1)
  --max-iterations      Max reasoning iterations (default: 20)
  --diff-lines          Estimated diff lines (default: 0)
  --dry-run             Estimate without checking budget
  --output              json|table|summary (default: table)

helix estimate check <agent-name> <task-description> [flags]
  --auto-approve        Auto-approve if within budget (default: true)
  --require-human       Require human approval even if within budget

helix estimate report [agent-name] [flags]
  --period              current|last|all (default: current)
  --format              json|table (default: table)
```

---

## 4. Operating Contract

- **NEVER** execute an agent task without a prior `estimate check` call. This is a blocking gate.
- **NEVER** hardcode pricing. All prices come from `~/.helix/pricing.yaml`.
- **ALWAYS** use cache-aware pricing when the model supports it (DeepSeek, Anthropic, Google). Models without cache pricing (MiniMax) use flat input rate for all input tokens.
- **ALWAYS** round cost estimates UP to the nearest cent (overestimate, never underestimate).
- **DO NOT** make OpenRouter API calls in unit tests. Use mock pricing.
- **ALWAYS** produce buildable code: `go build ./cmd/helix-estimate` must exit 0.
- **DO NOT** import packages beyond stdlib + `github.com/spf13/cobra` + `gopkg.in/yaml.v3`.

---

## 5. Assumptions

- OpenRouter API key is configured per agent and accessible for budget queries.
- known-friends.json `budget_used_usd` is updated atomically after each approved task.
- Pricing data changes monthly at most. `~/.helix/pricing.yaml` is the source of truth.
- Cache hit ratios (60% pro, 80% flash) are conservative defaults. Real ratios improve over time as the codebase stabilizes.
- Multi-agent tasks assume independent work (no inter-agent communication overhead).
- Budget period resets Sunday 00:00 UTC.

---

## 6. Architecture

```
                     ┌──────────────────────────────────────┐
                     │         cmd/helix-estimate            │
                     │   (Cobra CLI: 3 subcommands, flags)   │
                     └──────────────┬───────────────────────┘
                                    │
                     ┌──────────────▼───────────────────────┐
                     │        pkg/estimate/estimator         │
                     │  TaskDesc → TokenEstimate → CostUSD   │
                     │  Cache-aware: 60% pro / 80% flash     │
                     └───────┬───────────────┬──────────────┘
                             │               │
             ┌───────────────▼──┐       ┌─────▼──────────────┐
             │ pkg/estimate/    │       │ pkg/estimate/       │
             │ budget            │       │ pricing             │
             │ (weekly cap,      │       │ (yaml loader,       │
             │  spent tracking,  │       │  model→price map,   │
             │  approval gates)  │       │  cache multipliers) │
             └───────────────────┘       └─────────────────────┘
                             │
             ┌───────────────▼──────────────────────────────┐
             │           External APIs (v1 stubs)            │
             │  OpenRouter: GET /api/v1/key (budget query)   │
             │  GitReins:   LLMUsage struct (post-hoc)       │
             └──────────────────────────────────────────────┘
```

**Layering rules:**
- `pricing.go` imports only stdlib + `gopkg.in/yaml.v3`. No dependencies on other package files.
- `budget.go` imports `pricing.go` + `types.go`. Owns budget gates.
- `estimator.go` imports `pricing.go` + `budget.go` + `types.go`. Owns the estimation algorithm.
- `main.go` imports all. Owns CLI + output rendering.

---

## 7. Estimation Algorithm

### 7.1 Core Formula

```
estimated_cost_usd = total_cost_input + total_cost_output

where:
  fresh_input_tokens  = total_input_tokens × (1 − cache_hit_ratio)
  cache_hit_tokens    = total_input_tokens × cache_hit_ratio
  cache_write_tokens  = fresh_input_tokens × cache_write_ratio
  output_tokens       = total_input_tokens × output_ratio

  total_cost_input = fresh_input_tokens × price_input_per_M
                   + cache_hit_tokens × price_cache_hit_per_M
                   + cache_write_tokens × price_cache_write_per_M

  total_cost_output = output_tokens × price_output_per_M
```

### 7.2 Task Type → Token Budget

| Task Type | Input Tokens Base | Output Ratio | Max Iterations |
|-----------|-------------------|--------------|----------------|
| `spec` | 80,000 | 2.0 (output is large) | 5 |
| `code` | 120,000 | 0.8 | 20 |
| `review` | 40,000 | 0.3 | 3 |
| `refactor` | 200,000 | 0.5 | 15 |
| `test` | 30,000 | 1.0 | 10 |

Adjustment factors:
- `files_changed`: +5,000 input tokens per file
- `max_iterations`: +10,000 input tokens per iteration past 10
- `diff_lines`: +1 input token per 10 diff lines
- `agents`: N × base estimate (parallel work, no communication overhead factored)

### 7.3 Cache-Aware Pricing

| Provider | Supports Cache? | Cache Read Multiplier | Cache Write Price |
|----------|----------------|----------------------|-------------------|
| DeepSeek | ✅ | 0.10 (10x cheaper) | Same as input |
| Z.AI (GLM) | ✅ | 0.10 | Same as input |
| Anthropic | ✅ | 0.10 | 1.25 × input |
| Google (Gemini) | ✅ | 0.25 | Same as input |
| MiniMax | ❌ (no cache API) | N/A (all input at full price) | N/A |
| OpenRouter | Varies by upstream | Pass-through | Varies |

Cache hit ratios (configurable):
- **Pro tier (default):** 60% cache hit, 50% cache write of fresh input
- **Flash tier (default):** 80% cache hit, 70% cache write of fresh input
- **New repo (first 10 tasks):** 0% cache hit (cold start), 50% cache write

### 7.4 Example Calculation

```
Task: code review of a 3-file PR
Model: deepseek-v4-pro
Agent tier: pro

fresh_input_tokens  = 40,000 × (1 − 0.60) = 16,000
cache_hit_tokens    = 40,000 × 0.60       = 24,000
cache_write_tokens  = 16,000 × 0.50       = 8,000
output_tokens       = 40,000 × 0.3        = 12,000

cost_input  = (16,000/1M × $0.14) + (24,000/1M × $0.014) + (8,000/1M × $0.14)
            = $0.00224 + $0.000336 + $0.00112
            = $0.00370

cost_output = 12,000/1M × $0.28
            = $0.00336

total       = $0.00370 + $0.00336 = $0.00706 → rounded UP: $0.01
```

Without cache awareness, this task would be estimated at:
```
(40,000/1M × $0.14) + (12,000/1M × $0.28) = $0.0056 + $0.00336 = $0.009
```
The difference for a single review is small ($0.001). But across 1,000 reviews/month,
cache-awareness prevents $1.00 of over-estimation — keeping more budget available.

---

## 8. Budget Enforcement

### 8.1 Approval Gates

```
estimated_cost ≤ (budget_usd_weekly − budget_used_usd):
    → AUTO_APPROVED (proceeds immediately)

estimated_cost ≤ (budget_usd_weekly − budget_used_usd) × 1.5
    AND trust_level ≥ 70:
    → AUTO_APPROVED_WITH_WARNING (proceeds, warning logged)

estimated_cost > (budget_usd_weekly − budget_used_usd):
    → BLOCKED. Options:
      1. Wait for budget reset (Sunday 00:00 UTC)
      2. Request human budget increase
      3. Switch to cheaper model (flash models are 50% cheaper)

estimated_cost > budget_usd_weekly (single task > weekly cap):
    → ESCALATED. Requires explicit human approval.
```

### 8.2 Post-Execution Reconciliation

After agent execution completes:
1. GitReins evaluator returns actual `LLMUsage`
2. Actual cost is calculated from real token counts (not estimates)
3. `budget_used_usd` is updated in known-friends.json
4. If actual > estimated: difference is logged as `ESTIMATION_DRIFT`
5. Estimation ratios are recalibrated weekly from drift data

### 8.3 Budget Period Management

- Period: Sunday 00:00 UTC to Saturday 23:59:59 UTC
- Reset: Cron job runs Sunday 00:01 UTC → `budget_used_usd = 0` for all agents
- Rollover: NOT supported in v1. Unused budget expires. (v2 may add rollover.)
- Overdraft: NOT allowed. Tasks that would exceed budget are blocked pre-flight.

---

## 9. OpenRouter Budget Integration

### 9.1 API Endpoint

```
GET https://openrouter.ai/api/v1/key
Headers: Authorization: Bearer <agent_openrouter_key>
Response:
{
  "data": {
    "label": "agent-wojons",
    "limit": 10.00,           // USD limit set on key
    "usage": 3.42,            // USD used this period
    "limit_remaining": 6.58,  // USD remaining
    "rate_limit": {
      "requests": 200,
      "interval": "1m"
    },
    "is_free_tier": false,
    "created_at": "2026-06-01T00:00:00Z"
  }
}
```

### 9.2 Reconciliation Strategy

OpenRouter's usage data is authoritative for actual spend. Helix's `budget_used_usd`
is a projection. Reconciliation:

1. **Before estimation:** Query OpenRouter for current `usage` → set as baseline
2. **After execution:** Query OpenRouter for new `usage` → delta is actual cost
3. **Drift detection:** If Helix projection differs from OpenRouter by >10%, log warning
4. **Hard cap enforcement:** OpenRouter key limit is set to `budget_usd_weekly`. Even if Helix estimation fails, the key itself cannot exceed the budget at the provider level.

---

## 10. Filesystem Layout

### Inputs
```
~/.helix/pricing.yaml              Provider pricing data (manual updates)
/opt/hermes-demo/.hermes/h4f/
  known-friends.json               Agent budget + tier data
```

### Outputs
```
~/.helix/estimates/
  <agent>/<timestamp>.json          Per-task estimate records
  <agent>/reconciliation.jsonl      Post-execution actuals vs estimates
```

### State
```
~/.helix/estimates/state.json       Estimation calibration data
```

`~/.helix/pricing.yaml` schema:
```yaml
version: 1
updated: "2026-06-19"
providers:
  deepseek:
    models:
      deepseek-v4-pro:
        input_per_1k: 0.00014
        cache_read_per_1k: 0.000014
        output_per_1k: 0.00028
      deepseek-v4-flash:
        input_per_1k: 0.00007
        cache_read_per_1k: 0.000007
        output_per_1k: 0.00014
  zai-glm:
    models:
      glm-5.2:
        input_per_1k: 0.00010
        cache_read_per_1k: 0.000010
        output_per_1k: 0.00020
  minimax:
    models:
      MiniMax-M3:
        input_per_1k: 0.00020
        cache_read_per_1k: null   # No cache API
        output_per_1k: 0.00040
  anthropic:
    models:
      claude-sonnet-4:
        input_per_1k: 0.00300
        cache_read_per_1k: 0.00030
        cache_write_per_1k: 0.00375
        output_per_1k: 0.01500
  google:
    models:
      gemini-2.5-pro:
        input_per_1k: 0.00125
        cache_read_per_1k: 0.00031
        output_per_1k: 0.01000
  openrouter:
    markup_percent: 5

cache:
  pro_hit_ratio: 0.60
  flash_hit_ratio: 0.80
  pro_write_ratio: 0.50
  flash_write_ratio: 0.70
  new_repo_threshold: 10
  new_repo_hit_ratio: 0.0

tasks:
  spec:
    input_tokens: 80000
    output_ratio: 2.0
    max_iterations: 5
  code:
    input_tokens: 120000
    output_ratio: 0.8
    max_iterations: 20
  review:
    input_tokens: 40000
    output_ratio: 0.3
    max_iterations: 3
  refactor:
    input_tokens: 200000
    output_ratio: 0.5
    max_iterations: 15
  test:
    input_tokens: 30000
    output_ratio: 1.0
    max_iterations: 10
```

---

## 11. Error Taxonomy and Exit Codes

| Exit | Condition | Message format |
|------|-----------|----------------|
| 0 | Success (or dry-run) | — |
| 1 | Budget exhausted | `BUDGET_EXHAUSTED: agent=<name> remaining=$X.XX estimated=$Y.YY` |
| 2 | Estimation failed (bad input) | `ESTIMATION_FAILED: <reason>` |
| 3 | Config/file error | `CONFIG_ERROR: <path> <reason>` |
| 4 | OpenRouter unreachable | `OPENROUTER_UNREACHABLE: <url>` |
| 5 | Budget exceeds weekly cap (>1.5x) | `BUDGET_EXCEEDS_CAP: single task $X.XX > weekly $Y.YY` |
| 10 | Dry-run (no enforcement) | `DRY_RUN: estimated $X.XX (would be approved/denied)` |

**Error kinds:**
- `budget` → 1 (budget exhausted, over cap)
- `input` → 2 (invalid task description, unknown model)
- `config` → 3 (missing pricing file, bad yaml)
- `network` → 4 (OpenRouter timeout, connection refused)
- `dryrun` → 10 (informational, not an error)

---

## 12. Cache Token Economics (GitReins v0.4.1 Integration)

GitReins v0.4.1 introduces cache token tracking in `LLMUsage`. This is the bridge
between estimation (pre-flight) and actuals (post-hoc).

### 12.1 Why Cache Matters for Helix

Helix agents perform deterministic operations that hit cache frequently:
- Reading `AGENTS.md`, specs, `plan.yaml` → same files every task → 80%+ cache hit
- Running pre-commit checks → same patterns → 90%+ cache hit
- Code review of incremental PRs → diff context overlaps → 60%+ cache hit

Without cache awareness, cost estimates are inflated by 5-10x. An agent with a
$10/week budget would be unnecessarily blocked from tasks that actually cost $0.50.

### 12.2 Integration Points

```
Pre-flight:
  estimator.go → reads pricing.yaml → applies cache_hit_ratio → produces estimate

Post-hoc (after agent execution):
  GitReins evaluator → returns LLMUsage(cache_read_tokens=N, cache_write_tokens=M)
  reconciliation.go → computes actual cost → updates budget_used_usd
  drift_detector.go → compares estimate vs actual → recalibrates ratios

Recalibration (weekly cron):
  For each agent: (sum actual_cache_hits) / (sum total_input_tokens) = new ratio
  Write to ~/.helix/estimates/state.json → used next week's estimates
```

### 12.3 Cold Start Handling

New agents (first 10 tasks) have no cache history. Estimator uses:
- `new_repo_hit_ratio: 0.0` (zero cache hits)
- `new_repo_write_ratio: 0.50` (normal cache writes)

This means first 10 tasks are estimated at full price. After 10 tasks,
the ratio transitions to the agent's tier default (pro: 60%, flash: 80%).

---

## 13. Test Strategy

| Layer | File | What it tests | Mock/Real |
|-------|------|---------------|-----------|
| Unit | `pricing_test.go` | YAML loading, model→price lookup, missing provider fallback | File fixtures |
| Unit | `estimator_test.go` | Token estimation for all 5 task types | Mock pricing |
| Unit | `estimator_test.go` | Cache-aware vs naive estimation difference | Mock pricing |
| Unit | `budget_test.go` | Approval gates: auto-approve, warn, block, escalate | Mock budget data |
| Unit | `budget_test.go` | Budget exhausted → all tasks blocked | Mock budget data |
| Unit | `budget_test.go` | Period reset: Sunday 00:01 → budget resets | Clock injection |
| Integration | `estimator_integration_test.go` | Real pricing.yaml → real estimates | Real pricing file |
| Integration | `openrouter_test.go` | GET /api/v1/key → parse response → check limit | Real OpenRouter |
| Contract | `pricing_test.go` | All providers in pricing.yaml have valid model entries | File fixtures |
| Contract | `known_friends_test.go` | Budget fields present and valid | File fixtures |
| E2E | `e2e_test.go` | estimate → check → (mock) execute → reconcile | Mock execution |

Test fixtures (in `pkg/estimate/testdata/`):
- `pricing.yaml` (all 6 providers, all models)
- `pricing-missing-cache.yaml` (MiniMax — no cache fields)
- `pricing-empty.yaml` (error case)
- `known-friends.json` (4 active agents with budgets)
- `known-friends-exhausted.json` (all agents at budget cap)
- `known-friends-new.json` (agent with 0 tasks, cold start)

---

## 14. Observability

- `--verbose` logs every estimation step:
  `timestamp [level] agent=NAME task_type=CODE model=deepseek-v4-pro estimated=$1.23 cache_hit=60% budget_remaining=$8.77 decision=AUTO_APPROVED`
- Default logging via stdlib `log` package to stderr.
- Exit codes are machine-readable (see §11).
- Estimation records written to JSON files for later reconciliation.
- Drift metric: `(actual - estimated) / estimated` logged as a gauge for each task.
  Persistent drift >20% over 20 tasks triggers a recalibration flag.

---

## 15. Implementation Status (v1 target)

| Component | Status | Notes |
|-----------|--------|-------|
| CLI (3 subcommands) | ⏳ Stub | Flag/env binding, help text |
| pricing.yaml loader | ⏳ Stub | gopkg.in/yaml.v3, validation |
| Estimation algorithm | ⏳ Stub | Cache-aware formula, all 5 task types |
| Budget enforcement | ⏳ Stub | Approval gates, period management |
| OpenRouter API client | ⏳ Stub | GET /api/v1/key, parse response |
| Cache ratio recalibration | ⏳ Stub | Weekly cron, drift detection |
| Error taxonomy | ⏳ Stub | 7 exit codes, typed errors |
| Dry-run mode | ⏳ Stub | Full estimation, no enforcement |
| JSON/table/summary output | ⏳ Stub | 3 output formats |
| Reconciliation (post-hoc) | ⏳ Stub | GitReins LLMUsage → actual cost |

Unlike Feature 1 (which was implemented as stubs with real CLI/logic),
Feature 2 is currently a PURE specification. All components are stubs.

---

## 16. Verification Checklist

- [ ] `go build ./cmd/helix-estimate` exits 0
- [ ] `go vet ./...` clean
- [ ] No imports beyond stdlib + cobra + yaml.v3
- [ ] No hardcoded prices — all from pricing.yaml
- [ ] Cache-aware estimation produces different results from naive estimation
- [ ] Approval gates tested: auto-approve, warn, block, escalate
- [ ] Budget period reset tested: Sunday transition
- [ ] Cold start (new agent, 0 tasks) uses 0% cache ratio
- [ ] All 7 exit codes documented and tested
- [ ] Dry-run mode produces estimate without modifying state
- [ ] pricing.yaml validates successfully
- [ ] OpenRouter API response parsed correctly (including error cases)

---

## 17. Example Outputs

### Estimate (dry-run, table format)

```
$ helix estimate "Review PR #42 — add rate limiter to provisioner" \
    --task-type review --model deepseek-v4-pro --provider deepseek \
    --files-changed 3 --dry-run

TASK:           Review PR #42 — add rate limiter to provisioner
TYPE:           review
MODEL:          deepseek-v4-pro (deepseek)
TIER:           pro (60% cache hit)

TOKEN ESTIMATE:
  Fresh input:     20,800  (40% of 52,000)
  Cache hits:      31,200  (60% of 52,000)
  Cache writes:    10,400  (50% of fresh)
  Output:          15,600  (30% of 52,000)

COST ESTIMATE:
  Fresh input:     $0.0029  (20,800 × $0.14/M)
  Cache hits:      $0.0004  (31,200 × $0.014/M)
  Cache writes:    $0.0015  (10,400 × $0.14/M)
  Output:          $0.0044  (15,600 × $0.28/M)
  ─────────────────────────
  TOTAL:           $0.01    (rounded up)

DRY RUN — no budget check performed.
```

### Estimate check (within budget)

```
$ helix estimate check wojons "Review PR #42 — add rate limiter" \
    --task-type review --model deepseek-v4-pro

ESTIMATED COST:  $0.01
BUDGET REMAINING: $6.58 of $10.00 (weekly)
DECISION:         AUTO_APPROVED ✅
CONFIDENCE:       HIGH (cache hit ratio: 60%)
```

### Estimate check (budget exhausted)

```
$ helix estimate check llopez "Refactor identity provisioner" \
    --task-type refactor --model glm-5.2

ESTIMATED COST:  $0.08
BUDGET REMAINING: $0.03 of $5.00 (weekly)
DECISION:         BLOCKED ❌
REASON:           Budget exhausted. $0.08 estimated > $0.03 remaining.
OPTIONS:
  1. Wait for budget reset (Sunday 00:00 UTC)
  2. Switch to deepseek-v4-flash ($0.04 estimated — still over budget)
  3. Request human budget increase via /helix budget increase llopez $5.00
```

### Estimate check (single task exceeds weekly cap)

```
$ helix estimate check dtoole "Full repo refactor — all 42 files" \
    --task-type refactor --model claude-sonnet-4 --files-changed 42

ESTIMATED COST:  $3.26
WEEKLY CAP:      $5.00
DECISION:         ESCALATED ⚠️
REASON:           Single task ($3.26) > 50% of weekly cap ($5.00).
                  Requires explicit human approval.
APPROVAL CMD:     helix approve dtoole refactor-20260619-001
```

### Report (current period)

```
$ helix estimate report wojons --period current

AGENT:       wojons (pro tier)
PERIOD:      2026-06-15 to 2026-06-21 (3d 14h remaining)
BUDGET:      $10.00/week
SPENT:       $3.42 (34.2%)
REMAINING:   $6.58
TASKS:       47 approved, 0 blocked, 2 escalated

RECENT TASKS:
  DATE       TYPE      MODEL              EST    ACTUAL   DRIFT
  06-19 14:22 review   deepseek-v4-pro    $0.01  $0.01    0%
  06-19 13:05 code     glm-5.2            $0.42  $0.38    -9.5%
  06-19 12:01 code     MiniMax-M3         $0.15  $0.12    -20% ⚠️
  06-19 10:55 spec     glm-5.2            $0.28  $0.31    +10.7%

CACHE EFFICIENCY:
  Average cache hit: 62% (target: 60%)
  Savings from cache: $1.87 this period (35% of spend)
```

---

## 18. Package Structure

```
github.com/totalwindupflightsystems/helix/
├── cmd/helix-estimate/main.go       CLI entry point
├── pkg/estimate/
│   ├── types.go                     TaskDesc, TokenEstimate, CostEstimate, ApprovalDecision
│   ├── pricing.go                   PricingYAML, ProviderPricing, ModelPrice, LoadPricing()
│   ├── estimator.go                 Estimate(task TaskDesc) → CostEstimate
│   ├── budget.go                    BudgetTracker, CheckBudget(), ApproveOrBlock()
│   ├── openrouter.go                OpenRouter client (v1 stub: ErrNotImplemented)
│   ├── reconciliation.go            Reconcile(estimate, actual LLMUsage) → drift
│   ├── calibrator.go                RecalibrateCacheRatios() from historical data
│   └── testdata/
│       ├── pricing.yaml
│       ├── known-friends.json
│       └── known-friends-exhausted.json
├── specs/cost-estimator.md          This document
└── ~/.helix/pricing.yaml            Production pricing data
```

---

## Document Status

- [x] Mission and scope defined
- [x] All inputs documented (known-friends.json, pricing.yaml, task signals, CLI flags)
- [x] Estimation algorithm specified with cache-aware formula
- [x] Budget enforcement gates defined (auto-approve, warn, block, escalate)
- [x] OpenRouter API integration specified
- [x] GitReins v0.4.1 cache token integration specified
- [x] Cache token economics documented (cold start, recalibration)
- [x] Filesystem layout specified
- [x] Error taxonomy defined (7 exit codes)
- [x] Test strategy with fixture list
- [x] Observability requirements
- [x] Implementation status tracking
- [x] Verification checklist
- [x] Example outputs (5 scenarios)
- [x] Package structure
