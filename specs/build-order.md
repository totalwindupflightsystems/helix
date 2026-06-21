# Helix Platform — Build Order & Bootstrap Sequence

**Status:** v1.0
**Spec version:** 1.0
**Last updated:** 2026-06-20
**Depends on:** Deployment spec, Configuration spec

This document specifies the exact order in which components must be built,
tested, and started for Helix to function. An agent following this document
SHOULD be able to bootstrap Helix from zero without circular dependencies.

---

## 1. Dependency Graph

```
                    ┌─────────────────┐
                    │    DOCKER        │ (host prerequisite)
                    └────────┬────────┘
                             │
                    ┌────────▼────────┐
                    │    FORGEJO       │ Phase 0 — Git forge
                    └────────┬────────┘
                             │
              ┌──────────────┼──────────────┐
              │              │              │
     ┌────────▼────────┐ ┌──▼───────────┐ ┌─▼───────────────┐
     │    CHIMERA       │ │  LANGFUSE    │ │  GITREINS        │
     │  (PR review)     │ │ (traces)     │ │  (quality gate)  │
     └────────┬────────┘ └──────────────┘ └─────────────────┘
              │
     ┌────────▼────────┐
     │ CONSENSUS        │ (was Conscientiousness — adversarial eval)
     └────────┬────────┘
              │
     ┌────────▼────────┐
     │  H4F + AGENTS    │ (agent hosting — known-friends.json)
     └────────┬────────┘
              │
     ┌────────▼────────────────────────────┐
     │          HELIX CLI TOOLS             │ Phase 4 — Features 1-5
     │  helix-identity  (Feature 1)         │
     │  helix-estimate   (Feature 2)         │
     │  helix-negotiate  (Feature 3)         │
     │  helix-prompt     (Feature 4)         │
     │  helix-marketplace(Feature 5)         │
     │  helix-sandbox    (primitive)         │
     └────────┬────────────────────────────┘
              │
     ┌────────▼────────┐
     │  MUSTER          │ Phase 5 — API glue (needs Forgejo + all CLI tools)
     └────────┬────────┘
              │
     ┌────────▼────────┐
     │  HIVEMIND        │ Phase 5 — Memory + scheduling (needs Forgejo + agents)
     └────────┬────────┘
              │
     ┌────────▼────────┐
     │ K-M (STRESS TEST)│ Phase 6 — Testing (needs everything else running)
     └─────────────────┘
```

---

## 2. Phase 0: Infrastructure Prerequisites

### Check before starting
```bash
docker --version          # ≥ 24.0
docker compose version    # ≥ v2
python3 --version         # ≥ 3.11
go version                # ≥ 1.22
```

### Create directories
```bash
mkdir -p ~/.helix/keys
mkdir -p ~/.helix/marketplace/agents
mkdir -p ~/.helix/negotiations
mkdir -p ~/.helix/estimates
mkdir -p /opt/helix
```

---

## 3. Phase 1: Forgejo (The Foundation)

### 3.1 Start Forgejo
```bash
docker run -d --name forgejo-helix \
  -p 3030:3000 \
  -e FORGEJO__server__DOMAIN=localhost \
  -e FORGEJO__server__ROOT_URL=http://localhost:3030 \
  -e FORGEJO__server__HTTP_PORT=3000 \
  -e FORGEJO__server__START_SSH_SERVER=false \
  -v forgejo-helix-data:/data \
  codeberg.org/forgejo/forgejo:1.21
```

### 3.2 Create admin user (via web installer or CLI)
```bash
# Wait for Forgejo to start (10-15s)
sleep 15

# If fresh install (no app.ini): POST to install page
# If installed: admin user already exists from prior session
curl -s -u helio:$FORGEJO_ADMIN_PASSWORD http://localhost:3030/api/v1/version
# Expected: {"version":"1.21.x"}
```

### 3.3 Verify
```bash
curl -s -u helio:$FORGEJO_ADMIN_PASSWORD http://localhost:3030/api/v1/user | python3 -c "import sys,json; d=json.load(sys.stdin); assert d['is_admin'], 'not admin'; print('OK')"
```

---

## 4. Phase 2: Chimera + LangFuse (Quality & Observability)

### 4.1 Chimera
```bash
cd /home/kara/chimera
.venv/bin/pip install -e .
.venv/bin/python -m pytest tests/ -x --tb=short  # Verify 90 tests pass

# Start as daemon
.venv/bin/python -c "
from chimera.api.server import app
import uvicorn
uvicorn.run(app, host='0.0.0.0', port=8765)
" &
```

### 4.2 LangFuse
```bash
# Basic deployment (no Postgres for dev)
docker run -d --name langfuse-helix \
  -p 3001:3000 \
  -e DATABASE_URL="file:/app/data/langfuse.db" \
  -e NEXTAUTH_SECRET="helix-dev-secret" \
  -e NEXTAUTH_URL="http://localhost:3001" \
  ghcr.io/langfuse/langfuse:latest
```

### 4.3 Verify
```bash
curl -s http://localhost:8765/v1/health          # Chimera
curl -s http://localhost:3001/api/public/health   # LangFuse
```

### 4.4 Install GitReins (quality gate, CLI tool)
```bash
cd /home/kara/gitreins-poc
pip install -e .
gitreins --version
```

---

## 5. Phase 3: Agent Hosting (H4F)

### 5.1 Verify known-friends.json
```bash
cat /opt/hermes-demo/.hermes/h4f/known-friends.json
# Must contain: wojons, llopez, dtoole, jrestrepo
# If missing or empty ({}): populate from H4F production host
```

### 5.2 Agent containers (started by H4F bridge, not manually)

The H4F bridge cron (`every 5 min`) handles:
- Container lifecycle (start/stop/restart)
- Key rotation (OpenRouter dead key detection)
- Budget synchronization
- known-friends.json consistency

For local Helix development without H4F:
```bash
# Simulate agent identity without full H4F deployment
cp /opt/hermes-demo/.hermes/h4f/known-friends.json ~/.helix/known-friends.json
```

---

## 6. Phase 4: Helix CLI Tools (Features 1-5)

Build ALL Helix binaries. Order of implementation matches dependency chain:

### 6.1 Build all binaries
```bash
cd /home/kara/helix
go build ./cmd/helix-identity/
go build ./cmd/sandbox/
go build ./cmd/helix-estimate/
go build ./cmd/helix-negotiate/
go build ./cmd/helix-prompt/
go build ./cmd/helix-marketplace/
```

### 6.2 Create config files
```bash
cp specs/helix-config.md /tmp/  # reference
# Manually create ~/.helix/config.yaml (see helix-config.md §2)
# Manually create ~/.helix/pricing.yaml (see helix-config.md §3)
# Manually create ~/.helix/.env (see helix-config.md §4)
```

### 6.3 Bootstrap identity (Feature 1)
```bash
# Dry-run first
./helix-identity sync --dry-run

# Real sync (provisions agents in Forgejo)
./helix-identity sync
```

### 6.4 Verify all CLIs
```bash
./helix-identity status
./helix-estimate --help
./helix-negotiate --help
./helix-prompt --help
./helix-marketplace --help
./helix-sandbox --help
```

---

## 7. Phase 5: Muster + Hivemind (Integration Layer)

### 7.1 Muster (when cloned)
```bash
cd /home/kara/Muster
go build ./cmd/muster/
./muster serve --port 9090 &
# Generates MCP tools from Forgejo OpenAPI spec
./muster generate --spec http://localhost:3030/swagger.v1.json
```

### 7.2 Hivemind (when cloned)
```bash
cd /home/kara/Hivemind
go build ./cmd/hivemind/
./hivemind serve --port 8081 &
```

---

## 8. Phase 6: Stress Testing (Kobayashi-Maru)

```bash
cd /home/kara/Kobayashi-Maru
go build ./...
./kobayashi-maru --help

# Run Helix-specific scenarios after all services are up
./kobayashi-maru run --scenario helix-full-stack
```

---

## 9. Full Bootstrap Script

```bash
#!/bin/bash
# /opt/helix/bootstrap.sh
# Bootstraps Helix from zero. Assumes Docker + Go + Python are available.
set -e

echo "=== Phase 0: Prerequisites ==="
mkdir -p ~/.helix/keys ~/.helix/marketplace/agents ~/.helix/negotiations ~/.helix/estimates

echo "=== Phase 1: Forgejo ==="
docker rm -f forgejo-helix 2>/dev/null || true
docker run -d --name forgejo-helix -p 3030:3000 \
  -e FORGEJO__server__DOMAIN=localhost \
  -e FORGEJO__server__ROOT_URL=http://localhost:3030 \
  -e FORGEJO__server__HTTP_PORT=3000 \
  -e FORGEJO__server__START_SSH_SERVER=false \
  -v forgejo-helix-data:/data \
  codeberg.org/forgejo/forgejo:1.21
sleep 15

echo "=== Phase 2: Chimera ==="
cd /home/kara/chimera
.venv/bin/pip install -e . -q
.venv/bin/python -c "from chimera.api.server import app; import uvicorn; uvicorn.run(app, host='0.0.0.0', port=8765)" &
sleep 5

echo "=== Phase 3: Verify ==="
curl -sf http://localhost:3030/api/v1/version > /dev/null && echo "Forgejo: OK" || echo "Forgejo: FAIL"
curl -sf http://localhost:8765/v1/health > /dev/null && echo "Chimera: OK" || echo "Chimera: FAIL"

echo "=== Phase 4: Helix CLIs ==="
cd /home/kara/helix
go build ./cmd/... && echo "Helix: BUILD OK" || echo "Helix: BUILD FAIL"

echo "=== Bootstrap complete ==="
echo "Forgejo:  http://localhost:3030 (admin: helio / helio123)"
echo "Chimera:  http://localhost:8765"
echo "Helix CLIs in /home/kara/helix/"
```

---

## 10. Verification After Bootstrap

```bash
# 1. Forgejo is running
curl -s http://localhost:3030/api/v1/version | python3 -c "import sys,json; assert 'version' in json.load(sys.stdin)"

# 2. Chimera is running
curl -s http://localhost:8765/v1/health | python3 -c "import sys,json; assert json.load(sys.stdin)['status']=='ok'"

# 3. Helix CLIs build
cd /home/kara/helix && go build ./cmd/... && echo "BUILD OK"

# 4. Helix identity dry-run works (Feature 1)
./helix-identity sync --dry-run --known-friends /opt/hermes-demo/.hermes/h4f/known-friends.json

# 5. Helix sandbox dry-run works
./helix-sandbox run --dry-run -- echo "hello"

# All 5 checks must pass before declaring Helix "bootstrapped."
```

---

## Document Status

- [x] Dependency graph (ASCII diagram, all 9 sub-projects + 5 features)
- [x] Phase 0: Infrastructure prerequisites (Docker, Go, Python, directories)
- [x] Phase 1: Forgejo bootstrap (Docker, admin user, verification)
- [x] Phase 2: Chimera + LangFuse (test suite, service start, health check)
- [x] Phase 3: Agent hosting (known-friends.json, H4F bridge)
- [x] Phase 4: Helix CLI tools (build all binaries, create configs, bootstrap identity)
- [x] Phase 5: Muster + Hivemind (when repos cloned)
- [x] Phase 6: Kobayashi-Maru stress testing
- [x] Full bootstrap script (zero-to-running in one bash script)
- [x] Post-bootstrap verification checklist (5 checks)
