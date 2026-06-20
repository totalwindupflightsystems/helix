# Helix Platform — Configuration Specification

**Status:** v1.0
**Spec version:** 1.0
**Last updated:** 2026-06-20
**Depends on:** Deployment spec (docker-compose + services running)

This document specifies every configuration file an agent needs to create
before any Helix component can operate. No config = nothing works. Every
file path, every default, every required-vs-optional field is specified.

---

## 1. Configuration Files Map

```
~/.helix/
├── config.yaml              Top-level Helix config
├── pricing.yaml             Provider pricing data (Feature 2)
├── .env                     Credentials (gitignored)
├── keys/                    Agent SSH keys (Feature 1)
│   └── <agent>/
│       ├── id_ed25519
│       ├── id_ed25519.pub
│       └── id_ed25519.state
├── state.json               Idempotency state (Feature 1)
├── marketplace/             Agent registry (Feature 5)
│   ├── agents/
│   │   └── <agent>.yaml
│   └── _index.yaml
├── negotiations/            Debate transcripts (Feature 3)
├── estimates/               Cost estimation records (Feature 2)
└── prompts/                 Prompt registry (Feature 4)

repo/prompts/
├── _index.yaml
└── <component>/<version>/
    ├── prompt.md
    └── metadata.yaml
```

---

## 2. ~/.helix/config.yaml

```yaml
# Helix platform configuration
# Created by: helix init
# Modified by: operator

version: 1

# ── Forgejo ────────────────────────────────────────────────
forgejo:
  url: "http://localhost:3030"        # External URL (host)
  internal_url: "http://forgejo-helix:3000"  # Docker network URL
  admin_user: "helio"
  # admin_password set via HELIX_FORGEJO_ADMIN_PASSWORD env var (never in config)

# ── Chimera ────────────────────────────────────────────────
chimera:
  url: "http://localhost:8765"
  internal_url: "http://chimera-helix:8765"
  default_formation: "standard"
  arbiter_formation: "arbiter"
  budget_formation: "budget"
  timeout: 120s

# ── LangFuse ───────────────────────────────────────────────
langfuse:
  url: "http://localhost:3001"
  internal_url: "http://langfuse-helix:3000"
  enabled: true

# ── GitReins ───────────────────────────────────────────────
gitreins:
  model: "deepseek-v4-flash"
  provider: "deepseek"
  max_iterations: 25
  max_time: "5m"
  test_mode: "diff"

# ── Agent Identity (Feature 1) ─────────────────────────────
identity:
  known_friends_path: "/opt/hermes-demo/.hermes/h4f/known-friends.json"
  ssh_key_dir: "~/.helix/keys"
  state_path: "~/.helix/state.json"

# ── Cost Estimator (Feature 2) ─────────────────────────────
estimator:
  pricing_path: "~/.helix/pricing.yaml"
  cache_hit_ratio_pro: 0.60
  cache_hit_ratio_flash: 0.80
  budget_reset_day: "sunday"
  budget_reset_time: "00:00 UTC"

# ── Agent Marketplace (Feature 5) ──────────────────────────
marketplace:
  registry_path: "~/.helix/marketplace"
  auto_deprecation_days: 30
  trust_recalc_schedule: "0 2 * * *"  # Daily at 02:00 UTC

# ── Negotiation (Feature 3) ────────────────────────────────
negotiation:
  max_rounds: 3
  round_timeout: "5m"
  global_timeout: "30m"
  transcript_dir: "~/.helix/negotiations"

# ── Prompt Registry (Feature 4) ────────────────────────────
prompts:
  registry_path: "prompts"            # Relative to repo root
  deprecated_grace_days: 30
  retired_after_days: 180

# ── Services (health check targets) ────────────────────────
services:
  forgejo:
    url: "http://forgejo-helix:3000"
    health_endpoint: "/api/v1/version"
  chimera:
    url: "http://chimera-helix:8765"
    health_endpoint: "/v1/health"
  langfuse:
    url: "http://langfuse-helix:3000"
    health_endpoint: "/api/public/health"

# ── Budget ─────────────────────────────────────────────────
budget:
  default_weekly_usd:
    pro: 10.00
    flash: 5.00
  overage_policy: "block"             # "block" or "warn"
  escalation_threshold: 1.5           # Auto-escalate if single task > 1.5x weekly cap
```

---

## 3. ~/.helix/pricing.yaml

```yaml
# Provider pricing — updated manually when providers change rates
# Used by Feature 2 (Cost Estimator)
version: 1
updated: "2026-06-20"

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

## 4. ~/.helix/.env

```bash
# Helix credentials — NEVER commit to git
# Sourced by all Helix CLI tools at startup

# Forgejo
FORGEJO_URL=http://localhost:3030
FORGEJO_ADMIN_USER=helio
FORGEJO_ADMIN_PASSWORD=helio123

# Chimera
CHIMERA_URL=http://localhost:8765

# LangFuse
LANGFUSE_PUBLIC_KEY=pk-lf-...
LANGFUSE_SECRET_KEY=sk-lf-...
LANGFUSE_URL=http://localhost:3001

# LLM Providers (for agent operations)
DEEPSEEK_API_KEY=sk-...
ZAI_API_KEY=...
MINIMAX_API_KEY=...
ANTHROPIC_API_KEY=...
OPENROUTER_API_KEY=sk-or-...
```

---

## 5. chimera.yaml (Chimera Service Config)

```yaml
# /home/kara/chimera/chimera.yaml
# Mounted read-only into Chimera container
# Contains provider API keys — gitignored

server:
  port: 8765
  host: 0.0.0.0

models:
  catalog: models.dev
  sync_interval: 24h

formations:
  standard:
    stages:
      - type: rewrite
      - type: dispatch
      - type: execute
      - type: judge
      - type: audit
  arbiter:
    description: "3-model tie-breaker with audit for PR negotiation"
    stages:
      - type: execute
        models: 3
        independent: true
      - type: judge
        strategy: majority
      - type: audit
  budget:
    description: "Single-model budget review"
    stages:
      - type: rewrite
      - type: execute
        models: 1

providers:
  openrouter:
    api_key: "${OPENROUTER_API_KEY}"
    base_url: https://openrouter.ai/api/v1
  deepseek:
    api_key: "${DEEPSEEK_API_KEY}"
    base_url: https://api.deepseek.com/v1
  zai:
    api_key: "${ZAI_API_KEY}"
    base_url: https://api.z.ai/api/coding/paas/v4

routing:
  rules:
    - domain: backend
      prefer: [deepseek/deepseek-chat]
    - domain: frontend
      prefer: [anthropic/claude-sonnet-4]
    - domain: review
      prefer: [deepseek/deepseek-v4-pro]

rate_limiting:
  max_concurrent: 5
  max_queue_depth: 20

observability:
  langfuse:
    enabled: true
    host: "http://langfuse-helix:3000"
  logging:
    level: "INFO"
    format: "json"
```

---

## 6. Configuration Loading Order

Helix CLI tools resolve configuration in this order (later overrides earlier):

1. **Defaults** — hardcoded in Go source (safe fallback values)
2. **~/.helix/config.yaml** — user/operator configuration
3. **~/.helix/pricing.yaml** — provider pricing (separate file, updated independently)
4. **Environment variables** — `HELIX_*`, `FORGEJO_*`, `CHIMERA_*`
5. **CLI flags** — `--forgejo-url`, `--admin-token`, etc.

```go
// pkg/config/loader.go
func Load() (*Config, error) {
    cfg := Defaults()                     // 1. Hardcoded defaults
    cfg.Merge(LoadYAML(configPath))       // 2. ~/.helix/config.yaml
    cfg.Merge(LoadYAML(pricingPath))     // 3. ~/.helix/pricing.yaml
    cfg.Merge(LoadEnv())                  // 4. HELIX_* env vars
    cfg.Merge(ParseFlags())               // 5. CLI flags
    return cfg.Validate()
}
```

---

## 7. Configuration Validation

At startup, every Helix CLI tool MUST:

1. Verify `~/.helix/config.yaml` exists and is valid YAML
2. Verify `~/.helix/pricing.yaml` exists and has required providers
3. Verify `~/.helix/.env` is readable (warn if missing)
4. Verify Forgejo is reachable: `GET <forgejo.url>/api/v1/version`
5. Verify Chimera is reachable: `GET <chimera.url>/v1/health`
6. Fail fast if any required service is unreachable

```go
func (c *Config) Validate() error {
    if c.Forgejo.URL == "" {
        return fmt.Errorf("forgejo.url is required")
    }
    if c.Chimera.URL == "" {
        return fmt.Errorf("chimera.url is required")
    }
    return nil
}
```

---

## Document Status

- [x] Top-level config.yaml schema (all sections)
- [x] pricing.yaml schema (all providers + cache config + task types)
- [x] .env contract (credentials, gitignored)
- [x] chimera.yaml schema (formations, providers, routing, rate limiting)
- [x] Configuration loading order (5-tier: defaults → file → pricing → env → flags)
- [x] Startup validation contract (files exist + services reachable)
