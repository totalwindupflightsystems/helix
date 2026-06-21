# Helix Platform — Cross-Component Wiring

**Status:** v1.0
**Spec version:** 1.0
**Last updated:** 2026-06-20

This document specifies exactly how every Helix component discovers and
communicates with every other component. No component should need to guess
a URL, port, or auth method — this document is the wiring diagram.

---

## 1. Service Address Table

| Component | Internal URL | External URL | Auth | Health |
|-----------|-------------|-------------|------|--------|
| Forgejo | `http://forgejo-helix:3000` | `http://localhost:3030` | BasicAuth (admin) / PAT (agents) | `GET /api/v1/version` |
| Chimera | `http://chimera-helix:8765` | `http://localhost:8765` | None (internal) | `GET /v1/health` |
| LangFuse | `http://langfuse-helix:3000` | `http://localhost:3001` | Public/Secret key pair | `GET /api/public/health` |
| Conscientiousness | `http://conscientiousness-helix:8080` | `http://localhost:8080` | None (internal) | `GET /health` |
| Muster | `http://muster-helix:9090` | `http://localhost:9090` | None (internal) | `GET /health` |
| Hivemind | `http://hivemind-helix:8081` | `http://localhost:8081` | Internal token | `GET /health` |

Internal URLs are used by services on `helix-net`. External URLs are used by CLI tools running on the host.

---

## 2. Forgejo → Other Services

Forgejo publishes events that other services consume:

### 2.1 Forgejo → Chimera (PR Review)

**Trigger:** Forgejo Action on PR open/update
**File:** `.forgejo/workflows/chimera-review.yaml`

```
PR opened/updated
    → Forgejo Action runs
    → Checks out code, generates diff
    → POST http://chimera-helix:8765/v1/deliberate
       Body: { "prompt": "<PR title + diff>", "formation": "standard" }
    → Chimera returns: { "status": "APPROVE|REJECT|...", "summary": "...", "trace": {...} }
    → Action posts review comment to PR via Forgejo API
       POST http://forgejo-helix:3000/api/v1/repos/{owner}/{repo}/pulls/{number}/reviews
       Body: { "body": "Chimera review: **VERDICT**", "event": "COMMENT" }
```

### 2.2 Forgejo → GitReins (Quality Gate)

**Trigger:** Git push
**File:** `.forgejo/workflows/gitreins.yaml`

```
Git push
    → Forgejo Action runs
    → pip install gitreins
    → gitreins guard (Tier 1: secrets, lint, tests, build)
    → If PR: gitreins evaluate --diff <pr.diff> (Tier 2: agentic eval)
    → Results posted as commit status (✅/❌)
```

### 2.3 Forgejo → Conscientiousness (Adversarial Eval)

**Trigger:** After Chimera review completes (PR review posted)
**File:** `.forgejo/workflows/conscientiousness.yaml`

```
Chimera review posted
    → Forgejo Action triggers
    → POST http://conscientiousness-helix:8080/evaluate
       Body: { "pr_diff": "...", "chimera_verdict": {...}, "evidence_bundle": "verification.md" }
    → Conscientiousness returns: { "status": "DEFENSIBLE|VULNERABLE|...", "attack_vectors": [...] }
    → VULNERABLE → PR blocked. DEFENSIBLE → PR allowed.
```

---

## 3. Chimera → Other Services

### 3.1 Chimera → LangFuse (Observability)

```
Every Chimera deliberation
    → POST http://langfuse-helix:3000/api/public/ingestion
       Headers: Authorization: Basic <public:secret>
       Body: { "trace": { "name": "chimera-review", "input": "...", "output": "..." } }
    → LangFuse stores trace for cost tracking and debugging
```

### 3.2 Chimera → OpenRouter / DeepSeek / Z.AI (LLM Inference)

```
Chimera dispatches workers
    → ProviderGateway routes to configured provider
    → POST https://openrouter.ai/api/v1/chat/completions (or direct provider API)
       Headers: Authorization: Bearer <provider_api_key>
    → Response streamed back to worker
    → Cost tracked by Chimera, reported to LangFuse
```

---

## 4. Helix CLI → Other Services

### 4.1 helix-identity → Forgejo

```
helix identity sync
    → Reads known-friends.json
    → GET http://localhost:3030/api/v1/admin/users/{name}          (check exists)
    → POST http://localhost:3030/api/v1/admin/users                 (create agent)
    → POST http://localhost:3030/api/v1/user/keys                  (register SSH key)
    → POST http://localhost:3030/api/v1/users/{name}/tokens        (create PAT)
    → All calls use BasicAuth: helio:$FORGEJO_ADMIN_PASSWORD (admin) or agent:temp (for key registration)
```

### 4.2 helix-estimate → Chimera + OpenRouter

```
helix estimate check <agent> <task>
    → Reads ~/.helix/pricing.yaml
    → GET https://openrouter.ai/api/v1/key (agent's budget status)
    → Computes cache-aware estimate
    → Returns: APPROVED / BLOCKED / ESCALATED
    → (No Chimera call for estimation — Chimera is for review, not cost)
```

### 4.3 helix-negotiate → Forgejo + Chimera

```
helix negotiate debate <pr-url>
    → Reads PR reviews from Forgejo: GET /api/v1/repos/{owner}/{repo}/pulls/{number}/reviews
    → Detects conflict (APPROVED vs REQUEST_CHANGES)
    → Runs 3 debate rounds (posts comments via Forgejo API)
    → If deadlock: POST http://chimera-helix:8765/v1/deliberate (formation="arbiter")
    → Chimera returns APPROVE/REJECT → verdict posted to PR
    → Cost split between disagreeing agents
```

### 4.4 helix-prompt → Git (commit-msg hook)

```
helix prompt register <component> <version>
    → Hashes prompt (SHA-256, normalized)
    → Writes prompts/<component>/<version>/prompt.md + metadata.yaml
    → Updates prompts/_index.yaml

helix prompt verify <commit-sha>
    → Extracts prompt hash from commit message
    → Looks up in registry
    → Verifies hash matches stored content
    → Checks lifecycle state
    → Returns provenance chain: commit → prompt → spec → work item → intent
```

### 4.5 helix-marketplace → Forgejo + Chimera + H4F

```
helix marketplace search --capability go --min-trust 70
    → Reads ~/.helix/marketplace/agents/*.yaml
    → Filters by capability, trust threshold, tier, cost profile
    → Returns ranked results

Daily trust recalculation:
    → Queries Forgejo: PR merge/reject counts per agent
    → Queries Chimera: review accuracy per agent
    → Queries H4F: budget adherence, uptime
    → Recalculates trust_score formula
    → Updates ~/.helix/marketplace/agents/<agent>.yaml
    → Updates ~/.helix/marketplace/_index.yaml
```

---

## 5. H4F → Other Services

### 5.1 H4F → Forgejo (Agent Provisioning)

```
H4F bridge cron (every 5 min)
    → Reads known-friends.json
    → For each active agent without forgejo_user_id:
        → Calls helix-identity provision <name>
        → helix-identity creates Forgejo account + SSH key + PAT
        → H4F writes forgejo_user_id, forgejo_username, ssh_key_fingerprint to known-friends.json
```

### 5.2 H4F → OpenRouter (Key Management)

```
H4F bridge cron
    → For each active agent:
        → GET https://openrouter.ai/api/v1/key (verify key is alive)
        → If 401: create new key, assign guardrail, update Storage Box
        → Sync budget limits: key.limit = known-friends.budget_usd_weekly
```

---

## 6. Axiom → Other Services

### 6.1 Axiom → Forgejo (Work Item Lifecycle)

```
axiom run --intent "..." --repo /path/to/repo
    → Creates work item in .memory-bank/work-items/
    → Clones/uses repo from Forgejo
    → Agent writes code on feat/* branch
    → Commits with attestation (prompt hash + model + cost)
    → Pushes to Forgejo
    → Opens PR via Forgejo API
    → Updates Jira/Notion (if configured)
```

### 6.2 Axiom → Chimera (PR Review Trigger)

```
Axiom opens PR
    → Forgejo Action triggers Chimera review (see §2.1)
    → Axiom polls review status
    → If APPROVE: proceeds to Conscientiousness eval
    → If REJECT: work item back to in-progress
```

### 6.3 Axiom → Helix Marketplace (Swarm Assembly)

```
Axiom decomposes work item
    → Queries helix marketplace search --capability <required> --min-trust <threshold>
    → Marketplace returns ranked agents
    → Axiom assigns work item to best-matching agent
    → Agent claims task from Hivemind queue
```

---

## 7. Error Propagation

When a downstream service fails, the caller MUST propagate a specific error:

| Caller | Callee | Failure | Propagated Error |
|--------|--------|---------|-----------------|
| Forgejo Action | Chimera | Chimera unreachable | PR comment: "Chimera unavailable — manual review required" |
| helix-negotiate | Chimera | Budget exhausted | `BUDGET_EXHAUSTED: tie-break cost $X.XX > remaining` |
| helix-identity | Forgejo | 503 Service Unavailable | `CONNECTION_REFUSED: retry in 30s (attempt 1/4)` |
| helix-estimate | OpenRouter | 401 Unauthorized | `AUTH_FAILED: agent key is dead — trigger key rotation` |
| Axiom | Forgejo | 409 Conflict (branch exists) | `BRANCH_CONFLICT: feat/WI-001 exists — use --force-branch` |

---

## 8. Circuit Breaker Configuration

All cross-service HTTP calls use circuit breakers:

```go
type CircuitBreaker struct {
    MaxFailures  int           // 5
    ResetTimeout time.Duration // 60s
}

// Before each call:
if cb.State == "open" && time.Since(cb.LastFailure) < cb.ResetTimeout {
    return ErrCircuitOpen
}
```

| Service Pair | Max Failures | Reset Timeout | On Open |
|-------------|-------------|---------------|---------|
| Any → Forgejo | 5 | 60s | Retry with backoff (1s, 2s, 4s, 8s, 16s) |
| Any → Chimera | 5 | 60s | Degrade gracefully (skip review) |
| Any → LangFuse | 10 | 120s | Buffer traces locally, flush on reconnect |
| Any → OpenRouter | 3 | 30s | Fail fast, alert human |

---

## Document Status

- [x] Service address table (internal + external URLs, auth, health endpoints)
- [x] Forgejo → Chimera wiring (PR review trigger)
- [x] Forgejo → GitReins wiring (quality gate)
- [x] Forgejo → Conscientiousness wiring (adversarial eval)
- [x] Chimera → LangFuse wiring (observability)
- [x] Chimera → LLM providers (inference routing)
- [x] helix-identity → Forgejo (full API call sequence)
- [x] helix-estimate → Chimera + OpenRouter (budget query)
- [x] helix-negotiate → Forgejo + Chimera (debate + tie-break)
- [x] helix-prompt → Git (commit-msg hook, registry)
- [x] helix-marketplace → Forgejo + Chimera + H4F (trust recalculation)
- [x] H4F → Forgejo (agent provisioning)
- [x] H4F → OpenRouter (key management)
- [x] Axiom → Forgejo + Chimera + Marketplace (full workflow)
- [x] Error propagation table (all failure modes)
- [x] Circuit breaker configuration (all service pairs)
