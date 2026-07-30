# SPEC-025: Multi-Source Integration via Muster

**Status:** Draft · **Author:** Helix Foreman (DeepSeek V4 Pro) · **Date:** 2026-07-30
**Gap:** Helix only integrates with Forgejo. Buzz agents connect to databases, CRMs, filesystems, and APIs. Muster was built for this but was never wired in.

## 1. Problem

Helix agents can only interact with a git forge. Buzz agents have pluggable integrations to databases, CRMs, filesystems, and external APIs. Muster (26 packages, OpenAPI→MCP generator) is the missing bridge.

## 2. Solution

Muster Integration Layer: Helix agents get capability-scoped access to external systems through auto-generated MCP tools. Sources are defined in YAML, Muster generates MCP tools from OpenAPI specs, and capability gating ensures agents only access what they're trusted with.

## 3. Source Definition

```yaml
# .helix/sources.yaml
sources:
  database:
    type: postgres
    connection: ${DATABASE_URL}
    openapi: ./schemas/database.openapi.yaml
    allowed_agents: ["stepfun-tester"]
    rate_limit: 10/s
  crm:
    type: rest
    base_url: https://crm.internal/api
    openapi: ./schemas/crm.openapi.yaml
    token_env: CRM_API_TOKEN
  filesystem:
    type: local
    root: /data/shared
    read_only: true
```

## 4. Tool Generation Pipeline

OpenAPI spec → Muster Parser → Go CLI → MCP tool descriptors → Rate-limited HTTP wrapper

Each source becomes MCP tools: `source_database_query`, `source_crm_get_contact`, `source_filesystem_read`

## 5. Capability Gating

Agents only receive tools matching their fingerprints:
- Agent with "database: 82%" → read-only SQL
- Agent with "database: 98%" → read-write SQL
- Agent with no database capability → zero database tools

## 6. Integration Points

| Component | Location | Role |
|-----------|----------|------|
| Muster | /home/kara/muster (26 pkgs) | OpenAPI→MCP generation |
| Rate Limiter | pkg/forgejo/rate.go | Token bucket (extend for sources) |
| Cost Estimator | pkg/estimate/ | Pre-flight estimates (extend) |
| Sandbox | pkg/sandbox/ | Bubblewrap source access |
| Dispatcher | pkg/dispatch/ | Inject source tools at dispatch |

## 7. CLI

```
helix source add    --name STRING --type [postgres|rest|local] --spec PATH
helix source list   [--enabled]
helix source test   --name STRING
helix source tools  --name STRING   → list generated MCP tools
```

## 8. Files

```
pkg/source/
  config.go          # Source definition + YAML parsing
  config_test.go
  gateway.go         # Capability gating + tool filtering
  gateway_test.go
  muster_bridge.go   # Muster: spec → MCP tools
  muster_bridge_test.go
  ratelimit.go       # Per-source rate limiting
  ratelimit_test.go

cmd/helix/source.go  # CLI commands
```

## 9. Build Order

1. `pkg/source/config.go` — source YAML parsing + validation
2. `pkg/source/muster_bridge.go` — Muster integration for tool generation
3. `pkg/source/gateway.go` — capability-gated tool filtering
4. `pkg/source/ratelimit.go` — per-source rate limiting
5. Wire into dispatcher: inject source tools at task dispatch
6. `cmd/helix/source.go` — CLI commands
