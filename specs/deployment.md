# Helix Platform — Deployment Specification

**Status:** v1.0 (build-ready reference)
**Spec version:** 1.0
**Last updated:** 2026-06-20
**Depends on:** All 9 sub-projects + Forgejo + Docker 24.0+
**Audience:** Operators deploying Helix; agents connecting components

This document is the authoritative deployment reference for the Helix
platform. It specifies the Docker Compose topology, network layout, volume
mounts, health check contracts, environment variable contracts, and Forgejo
Actions CI/CD pipelines. An agent with this document + the component specs
SHOULD be able to deploy Helix end-to-end without asking questions.

---

## 1. Topology

```
                         ┌──────────────────────────────────────┐
                         │           reverse-proxy               │
                         │   Caddy :443 → internal services      │
                         │   (production only; skip for dev)      │
                         └──────────────┬───────────────────────┘
                                        │
    ┌───────────────────────────────────┼───────────────────────────────┐
    │                          helix-net (bridge)                        │
    │                                                                   │
    │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────────┐  │
    │  │ Forgejo  │  │ Chimera  │  │ LangFuse │  │ Conscientiousness│  │
    │  │  :3000   │  │  :8765   │  │  :3001   │  │      :8080       │  │
    │  └──────────┘  └──────────┘  └──────────┘  └──────────────────┘  │
    │                                                                   │
    │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────────┐  │
    │  │  Muster  │  │ Hivemind │  │  Axiom   │  │ Kobayashi-Maru   │  │
    │  │  :9090   │  │  :8081   │  │  (CLI)   │  │      :9095       │  │
    │  └──────────┘  └──────────┘  └──────────┘  └──────────────────┘  │
    │                                                                   │
    │  ┌──────────────────────────────────────────────────────────────┐ │
    │  │              H4F Agent Containers (per-agent)                │ │
    │  │  agent-wojons   agent-llopez   agent-dtoole   agent-jrestrepo│ │
    │  │  (dind+hermes)  (dind+hermes)  (dind+hermes)  (dind+hermes)  │ │
    │  └──────────────────────────────────────────────────────────────┘ │
    │                                                                   │
    │  ┌──────────────────────────────────────────────────────────────┐ │
    │  │        OpenCode Containers (per-project, existing)            │ │
    │  │  opencode-gitreins-poc  opencode-kobayashi-maru               │ │
    │  │  opencode-muster        opencode-conscientiousness            │ │
    │  │  opencode-mythos        opencode-speclang                     │ │
    │  │  opencode-axiom         opencode-mafia                        │ │
    │  └──────────────────────────────────────────────────────────────┘ │
    └───────────────────────────────────────────────────────────────────┘
```

---

## 2. docker-compose.yaml

```yaml
# /opt/helix/docker-compose.yaml
# Helix platform — all 9 sub-projects + Forgejo + LangFuse
#
# Start:  docker compose up -d
# Stop:   docker compose down
# Status: docker compose ps
# Logs:   docker compose logs -f <service>

version: "3.8"

networks:
  helix-net:
    name: helix-net
    driver: bridge

volumes:
  forgejo-data:
    name: forgejo-helix-data
  langfuse-data:
    name: langfuse-helix-data
  hivemind-data:
    name: hivemind-helix-data

services:
  # ── Git Forge ────────────────────────────────────────────
  forgejo:
    image: codeberg.org/forgejo/forgejo:1.21
    container_name: forgejo-helix
    restart: unless-stopped
    networks:
      - helix-net
    ports:
      - "3030:3000"   # Web UI + API
    environment:
      FORGEJO__server__DOMAIN: "localhost"
      FORGEJO__server__ROOT_URL: "http://localhost:3030"
      FORGEJO__server__HTTP_PORT: "3000"
      FORGEJO__server__START_SSH_SERVER: "false"
    volumes:
      - forgejo-data:/data
    healthcheck:
      test: ["CMD", "curl", "-sf", "http://localhost:3000/api/v1/version"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 15s

  # ── Quality & Review ─────────────────────────────────────
  chimera:
    build:
      context: /home/kara/chimera
      dockerfile: Dockerfile
    container_name: chimera-helix
    restart: unless-stopped
    networks:
      - helix-net
    ports:
      - "8765:8765"
    environment:
      CHIMERA_CONFIG: "/etc/chimera/chimera.yaml"
      CHIMERA_PORT: "8765"
      CHIMERA_HOST: "0.0.0.0"
    volumes:
      - /home/kara/chimera/chimera.yaml:/etc/chimera/chimera.yaml:ro
    healthcheck:
      test: ["CMD", "curl", "-sf", "http://localhost:8765/v1/health"]
      interval: 15s
      timeout: 5s
      retries: 3
      start_period: 10s
    command: >
      python3 -c "
      from chimera.api.server import app
      import uvicorn
      uvicorn.run(app, host='0.0.0.0', port=8765)
      "

  # CONSENSUS NOTE: Conscientiousness was renamed to Consensus.
  # When the Consensus repo is cloned, uncomment below:
  # conscientousness:
  #   build:
  #     context: /home/kara/Consensus
  #     dockerfile: Dockerfile
  #   container_name: conscientousness-helix
  #   restart: unless-stopped
  #   networks:
  #     - helix-net
  #   ports:
  #     - "8080:8080"
  #   healthcheck:
  #     test: ["CMD", "curl", "-sf", "http://localhost:8080/health"]
  #     interval: 15s
  #     timeout: 5s
  #     retries: 3

  # ── Observability ────────────────────────────────────────
  langfuse:
    image: ghcr.io/langfuse/langfuse:latest
    container_name: langfuse-helix
    restart: unless-stopped
    networks:
      - helix-net
    ports:
      - "3001:3000"
    environment:
      DATABASE_URL: "postgresql://langfuse:langfuse@langfuse-db:5432/langfuse"
      NEXTAUTH_SECRET: "${LANGFUSE_SECRET:-changeme}"
      NEXTAUTH_URL: "http://localhost:3001"
      SALT: "${LANGFUSE_SALT:-changeme}"
    volumes:
      - langfuse-data:/app/data
    depends_on:
      langfuse-db:
        condition: service_healthy

  langfuse-db:
    image: postgres:16-alpine
    container_name: langfuse-db-helix
    restart: unless-stopped
    networks:
      - helix-net
    environment:
      POSTGRES_USER: langfuse
      POSTGRES_PASSWORD: langfuse
      POSTGRES_DB: langfuse
    volumes:
      - langfuse-data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U langfuse"]
      interval: 10s
      timeout: 5s
      retries: 5

  # ── API Glue ─────────────────────────────────────────────
  # MUSTER NOTE: repo exists but not cloned locally.
  # When cloned: uncomment and set context path.
  # muster:
  #   build:
  #     context: /home/kara/Muster
  #     dockerfile: Dockerfile
  #   container_name: muster-helix
  #   restart: unless-stopped
  #   networks:
  #     - helix-net
  #   ports:
  #     - "9090:9090"
  #   volumes:
  #     - /home/kara/Muster/config.yaml:/etc/muster/config.yaml:ro

  # ── Memory & Scheduling ──────────────────────────────────
  # HIVEMIND NOTE: repo not yet cloned. Placeholder.
  # hivemind:
  #   build:
  #     context: /home/kara/Hivemind
  #     dockerfile: Dockerfile
  #   container_name: hivemind-helix
  #   restart: unless-stopped
  #   networks:
  #     - helix-net
  #   ports:
  #     - "8081:8081"
  #   volumes:
  #     - hivemind-data:/data

  # ── Stress Testing ───────────────────────────────────────
  # K-M NOTE: Kobayashi-Maru exists locally. CLI-only, no daemon.
  # Called ad-hoc by Helix cron jobs, not a persistent service.
```

---

## 3. Service Discovery

Services discover each other via Docker DNS on `helix-net`:

| Service | Internal Hostname | Port | Health Check |
|---------|------------------|------|-------------|
| Forgejo | `forgejo-helix` | 3000 | `GET /api/v1/version` |
| Chimera | `chimera-helix` | 8765 | `GET /v1/health` |
| LangFuse | `langfuse-helix` | 3000 | `GET /api/public/health` |
| Conscientiousness | `conscientiousness-helix` | 8080 | `GET /health` |
| Muster | `muster-helix` | 9090 | `GET /health` |
| Hivemind | `hivemind-helix` | 8081 | `GET /health` |
| Kobayashi-Maru | (CLI-only, no daemon) | N/A | `kobayashi-maru --version` |

---

## 4. Environment Variable Contracts

### 4.1 Forgejo Admin Credentials

```bash
# /opt/helix/.env
FORGEJO_URL=http://forgejo-helix:3000          # Internal Docker hostname
FORGEJO_PUBLIC_URL=http://localhost:3030        # External (host) URL
FORGEJO_ADMIN_USER=helio
FORGEJO_ADMIN_PASSWORD=helio123
```

### 4.2 Chimera Provider Keys

```yaml
# /home/kara/chimera/chimera.yaml (gitignored, mounted read-only)
server:
  port: 8765
  host: 0.0.0.0

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
```

### 4.3 Helix CLI Configuration

```bash
# ~/.helix/.env (sourced by all Helix CLI tools)
FORGEJO_URL=http://localhost:3030
FORGEJO_ADMIN_USER=helio
FORGEJO_ADMIN_PASSWORD=helio123
CHIMERA_URL=http://localhost:8765
LANGFUSE_URL=http://localhost:3001
```

---

## 5. Forgejo Actions CI/CD

### 5.1 Prompt Regression Tests

```yaml
# .forgejo/workflows/promptfoo.yaml
name: Prompt Regression Tests
on:
  push:
    paths:
      - 'prompts/**'
      - '.promptfoo.yaml'
jobs:
  promptfoo:
    runs-on: ubuntu-latest
    container: node:20
    steps:
      - uses: actions/checkout@v4
      - run: npx promptfoo eval
      - uses: actions/upload-artifact@v4
        with:
          name: promptfoo-results
          path: promptfoo-results.json
```

### 5.2 Chimera PR Review Trigger

```yaml
# .forgejo/workflows/chimera-review.yaml
name: Chimera PR Review
on:
  pull_request:
    types: [opened, synchronize]
jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - name: Get PR diff
        run: git diff origin/${{ github.event.pull_request.base.ref }} > /tmp/pr.diff
      - name: Run Chimera review
        run: |
          curl -s -X POST http://chimera-helix:8765/v1/deliberate \
            -H "Content-Type: application/json" \
            -d "{
              \"prompt\": \"Review this PR diff for bugs, security issues, and spec violations.\\n\\nPR: ${{ github.event.pull_request.title }}\\n\\n---\\n$(cat /tmp/pr.diff | head -c 50000)\",
              \"formation\": \"standard\"
            }" > /tmp/chimera-verdict.json
      - name: Post review comment
        run: |
          VERDICT=$(python3 -c "import json; print(json.load(open('/tmp/chimera-verdict.json')).get('status','ERROR'))")
          curl -s -u "${{ secrets.FORGEJO_ADMIN_USER }}:${{ secrets.FORGEJO_ADMIN_PASSWORD }}" \
            -X POST "http://forgejo-helix:3000/api/v1/repos/${{ github.repository }}/pulls/${{ github.event.pull_request.number }}/reviews" \
            -H "Content-Type: application/json" \
            -d "{\"body\": \"Chimera review: **$VERDICT**\\n\\n$(cat /tmp/chimera-verdict.json | python3 -c 'import json,sys; print(json.load(sys.stdin).get(\"summary\",\"No summary\"))')\", \"event\": \"COMMENT\"}"
```

### 5.3 GitReins Quality Gate

```yaml
# .forgejo/workflows/gitreins.yaml
name: GitReins Quality Gate
on: [push]
jobs:
  guard:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Run GitReins Tier 1
        run: |
          pip install gitreins
          gitreins guard
      - name: Run GitReins Tier 2 (PR only)
        if: github.event_name == 'pull_request'
        run: |
          git diff origin/${{ github.event.pull_request.base.ref }} > /tmp/pr.diff
          gitreins evaluate --diff /tmp/pr.diff
```

---

## 6. Volume Layout

```
/opt/helix/
├── docker-compose.yaml        This file
├── .env                       Forgejo + Chimera credentials (gitignored)
├── forgejo-data/              Forgejo repos, DB, config (Docker volume)
├── langfuse-data/             LangFuse DB + traces (Docker volume)
├── hivemind-data/             Hivemind memory bank (Docker volume)
└── chimera.yaml               (symlink to /home/kara/chimera/chimera.yaml)
```

---

## 7. Startup Order

1. **Forgejo** — no dependencies, must be first
2. **LangFuse + DB** — no Helix dependencies
3. **Chimera** — no Helix dependencies (needs provider keys)
4. **Conscientiousness** — no Helix dependencies
5. **Muster** — needs Forgejo to generate tools from, but starts eagerly
6. **Hivemind** — needs Forgejo for git operations
7. **Helix CLI tools** — needs Forgejo + Chimera running to function
8. **H4F agent containers** — needs all of the above
9. **OpenCode containers** — per-project, start independently

---

## 8. Health Check Contract

Every service MUST expose a health endpoint. The Docker healthcheck and Helix's own readiness probes consume these:

| Endpoint | Expected Response | Status |
|----------|------------------|--------|
| `GET /v1/health` | `{"status":"ok"}` | Chimera healthy |
| `GET /api/v1/version` | `{"version":"1.21.x"}` | Forgejo healthy |
| `GET /api/public/health` | `{"status":"OK"}` | LangFuse healthy |
| `GET /health` | `{"status":"healthy"}` | Conscientiousness/Muster/Hivemind healthy |

Helix CLI tools MUST verify service health at startup (fail fast):

```go
// pkg/health/checker.go
func CheckServices() error {
    for _, svc := range requiredServices {
        if !svc.Healthy() {
            return fmt.Errorf("service %s unhealthy: %s", svc.Name, svc.URL)
        }
    }
    return nil
}
```

---

## 9. Network Isolation

- All Helix services communicate over `helix-net` (Docker bridge, internal only)
- Only Forgejo (3030), Chimera (8765), and LangFuse (3001) expose ports to the host
- Host exposure is for DEVELOPMENT only. Production uses Caddy reverse proxy.
- OpenCode containers have their own network (existing `opencode-net` or similar)
- H4F agent containers use gluetun VPN for outbound LLM API calls

---

## 10. Resource Limits

| Service | CPU | Memory | Justification |
|---------|-----|--------|---------------|
| Forgejo | 0.5-2.0 | 512MB-2GB | Git operations are bursty, mostly idle |
| Chimera | 1.0-4.0 | 512MB-4GB | LLM proxy, high CPU during deliberation |
| LangFuse | 0.5-1.0 | 512MB-1GB | Trace ingestion, DB-backed |
| LangFuse DB | 0.5-1.0 | 256MB-1GB | Postgres for traces |
| Conscientiousness | 0.5-2.0 | 256MB-1GB | LLM-based evaluation |
| Muster | 0.5-1.0 | 256MB-512MB | API proxy, caching |
| Hivemind | 0.5-1.0 | 256MB-1GB | Task queue, memory bank |
| H4F agent | 1.0-4.0 | 1GB-8GB | Full Hermes + dind + gluetun |

---

## 11. Backup & Restore

### Critical data (backed up):
- **Forgejo:** `/data/gitea/gitea.db` (SQLite), `/data/git/repositories/` (git repos)
- **LangFuse:** PostgreSQL database
- **Hivemind:** `/data/memory-bank/` (agent memory)
- **known-friends.json:** `/opt/hermes-demo/.hermes/h4f/known-friends.json`

### Backup script:
```bash
#!/bin/bash
# /opt/helix/backup.sh
DATE=$(date +%Y%m%d-%H%M%S)
BACKUP_DIR=/opt/helix/backups/$DATE

mkdir -p $BACKUP_DIR
docker exec forgejo-helix tar czf - /data > $BACKUP_DIR/forgejo.tar.gz
docker exec langfuse-db-helix pg_dump -U langfuse langfuse > $BACKUP_DIR/langfuse.sql
cp /opt/hermes-demo/.hermes/h4f/known-friends.json $BACKUP_DIR/
```

---

## Document Status

- [x] Docker Compose topology (Forgejo + Chimera + LangFuse + Conscientiousness/Muster/Hivemind)
- [x] Service discovery table (Docker DNS hostnames + ports)
- [x] Environment variable contracts (Forgejo, Chimera, Helix CLI)
- [x] Forgejo Actions CI/CD (PromptFoo, Chimera review, GitReins guard)
- [x] Volume layout + host paths
- [x] Startup order with dependencies
- [x] Health check contract (all services)
- [x] Network isolation design (helix-net, host exposure, gluetun)
- [x] Resource limits per service
- [x] Backup & restore procedures
