# Helix Platform — Error Recovery Procedures

**Status:** v1.0
**Spec version:** 1.0
**Last updated:** 2026-06-20

This appendix documents the recovery procedure for every failure mode an
agent can encounter while building or operating Helix. No agent should
need to guess what to do when a component fails — this document provides
the exact recovery procedure.

---

## 1. Forgejo Recovery

### FJ-001: Forgejo Unreachable
**Symptom:** `curl http://localhost:3030/api/v1/version` times out
**Root causes:** Container stopped, port conflict, disk full
**Recovery:**
```bash
# 1. Check container status
docker ps -a --filter name=forgejo-helix

# 2. If stopped: restart
docker start forgejo-helix && sleep 15

# 3. If port conflict: find and kill competing process
fuser -k 3030/tcp

# 4. If disk full: clean Docker
docker system prune -af --volumes
```

### FJ-002: Forgejo Admin Auth Failed
**Symptom:** `curl -u helio:$FORGEJO_ADMIN_PASSWORD` returns 401
**Root causes:** Password changed, account locked, DB corruption
**Recovery:**
```bash
# 1. Reset admin password via CLI
docker exec forgejo-helix forgejo admin user change-password --username helio --password newpassword123

# 2. Verify new password works
curl -s -u helio:$FORGEJO_ADMIN_NEW_PASSWORD http://localhost:3030/api/v1/version
```

### FJ-003: Agent Provisioning Failed Mid-Sync
**Symptom:** `helix identity sync` fails after creating some agents but not all
**Recovery:**
```bash
# 1. Run status to see which agents were provisioned
./helix-identity status

# 2. Rerun sync (idempotent — created agents are skipped)
./helix-identity sync

# 3. If state file corrupted: remove and re-sync
rm ~/.helix/state.json
./helix-identity sync
```

---

## 2. Chimera Recovery

### CH-001: Chimera Unreachable
**Symptom:** `curl http://localhost:8765/v1/health` times out
**Recovery:**
```bash
# 1. Check if process is running
pgrep -f "chimera.api.server"

# 2. Restart Chimera
cd /home/kara/chimera
.venv/bin/python -c "from chimera.api.server import app; import uvicorn; uvicorn.run(app, host='0.0.0.0', port=8765)" &

# 3. Verify
sleep 5 && curl -s http://localhost:8765/v1/health
```

### CH-002: Chimera Package Import Error
**Symptom:** `from chimera.api.server import app` fails with ImportError
**Root cause:** Package pointing at wrong source (chimera-v2 vs chimera)
**Recovery:**
```bash
cd /home/kara/chimera
.venv/bin/pip install -e .
.venv/bin/python -c "import chimera; print(chimera.__file__)"
# Must show: /home/kara/chimera/src/chimera/__init__.py
```

### CH-003: Chimera Formation Timeout
**Symptom:** Deliberation returns HTTP 504 or takes >120s
**Root cause:** Provider rate limiting, model unavailable
**Recovery:**
```bash
# 1. Check provider API keys
grep -c 'api_key' /home/kara/chimera/chimera.yaml

# 2. Test a single provider
curl -s https://api.deepseek.com/v1/models -H "Authorization: Bearer $DEEPSEEK_API_KEY"

# 3. Fall back to budget formation (DeepSeek only, cheaper, faster)
curl -X POST http://localhost:8765/v1/deliberate \
  -H "Content-Type: application/json" \
  -d '{"prompt": "test", "formation": "budget"}'
```

### CH-004: Chimera Test Suite Broken
**Symptom:** `pytest tests/` shows collection errors or import failures
**Recovery:**
```bash
cd /home/kara/chimera
.venv/bin/pip install -e .           # Reinstall package
.venv/bin/pip install pytest httpx fastapi pydantic pyyaml  # Core deps
.venv/bin/python -m pytest tests/ -x --tb=short
# Expected: 90 passed
```

---

## 3. Helix CLI Recovery

### HC-001: Build Failure
**Symptom:** `go build ./cmd/...` exits non-zero
**Recovery:**
```bash
cd /home/kara/helix

# 1. Check Go version (must be >= 1.22)
go version

# 2. Download dependencies
go mod download

# 3. Clean build cache
go clean -cache

# 4. Build with verbose errors
go build -v ./cmd/... 2>&1 | tail -20

# 5. If cobra import fails: go get github.com/spf13/cobra@latest
go get github.com/spf13/cobra@latest
```

### HC-002: Config Not Found
**Symptom:** `helix identity sync` exits with "config file not found"
**Recovery:**
```bash
# 1. Create config directory
mkdir -p ~/.helix

# 2. Copy config template (see specs/helix-config.md)
# Or create minimal config:
cat > ~/.helix/config.yaml << 'EOF'
version: 1
forgejo:
  url: "http://localhost:3030"
  admin_user: "helio"
chimera:
  url: "http://localhost:8765"
EOF

# 3. Create .env
echo "FORGEJO_ADMIN_PASSWORD=helio123" > ~/.helix/.env
```

### HC-003: State File Corruption
**Symptom:** `helix identity status` shows incorrect or missing state
**Recovery:**
```bash
# 1. Backup corrupted state
cp ~/.helix/state.json ~/.helix/state.json.bak.$(date +%Y%m%d)

# 2. Remove and re-sync
rm ~/.helix/state.json
./helix-identity sync --known-friends /opt/hermes-demo/.hermes/h4f/known-friends.json
```

### HC-004: SSH Key Conflict
**Symptom:** Forgejo returns 409 when registering SSH key (key already exists)
**Recovery:**
```bash
# 1. Check existing keys in Forgejo
curl -s -u helio:$FORGEJO_ADMIN_PASSWORD http://localhost:3030/api/v1/user/keys

# 2. Delete stale key if needed
curl -s -u helio:$FORGEJO_ADMIN_PASSWORD -X DELETE http://localhost:3030/api/v1/user/keys/{key_id}

# 3. Regenerate and re-register
./helix-identity keygen agent-name
./helix-identity provision agent-name
```

---

## 4. Docker Recovery

### DK-001: Container Won't Start
**Symptom:** `docker start <container>` fails or exits immediately
**Recovery:**
```bash
# 1. Check logs
docker logs <container> --tail 50

# 2. Check port conflicts
ss -tlnp | grep -E '3000|3030|8765'

# 3. Check disk
df -h /var/lib/docker

# 4. If port conflict: restart with different port
docker rm -f <container>
docker run -d --name <container> -p <new_port>:<internal_port> ...
```

### DK-002: Docker Network Broken
**Symptom:** Services on `helix-net` can't reach each other
**Recovery:**
```bash
# 1. Check network exists
docker network ls | grep helix-net

# 2. Create if missing
docker network create helix-net

# 3. Reconnect containers
docker network connect helix-net <container>
```

---

## 5. GitReins Recovery

### GR-001: Guard Fails on secrets
**Symptom:** `gitreins guard` shows "✗ secrets"
**Recovery:**
```bash
# 1. Find the secret
grep -rn 'sk-\|api_key\|token' --include='*.yaml' --include='*.json' --include='*.go' .
# Skip lines in .gitignore'd files

# 2. Either: add file to .gitignore (if config file)
echo "chimera.yaml" >> .gitignore

# 3. Or: redact the secret from the file
# Replace real keys with ${ENV_VAR} references

# 4. Re-run guard
gitreins guard
```

### GR-002: Guard Fails on tests
**Symptom:** `gitreins guard` shows "✗ tests"
**Recovery:**
```bash
# 1. Run tests directly to see failures
cd /home/kara/helix && go test ./... 2>&1

# 2. Fix failing tests
# 3. Re-run guard
gitreins guard
```

---

## 6. OpenRouter / LLM Provider Recovery

### OR-001: API Key Dead
**Symptom:** 401 Unauthorized from OpenRouter
**Recovery:**
```bash
# 1. Test key directly
curl -s https://openrouter.ai/api/v1/auth/key \
  -H "Authorization: Bearer $OPENROUTER_API_KEY"

# 2. If dead: trigger key rotation via H4F bridge (production)
# Or: manually create new key at openrouter.ai

# 3. Update .env with new key
```

### OR-002: Rate Limited
**Symptom:** HTTP 429 from provider
**Recovery:**
```bash
# 1. Check rate limit headers
curl -s -D - https://api.deepseek.com/v1/chat/completions \
  -H "Authorization: Bearer $DEEPSEEK_API_KEY" \
  -d '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"test"}]}' | grep -i 'ratelimit\|retry-after'

# 2. Wait for Retry-After duration
# 3. Or: switch provider temporarily
```

---

## 7. General Recovery Principles

1. **Read the logs first.** Every component logs to stdout/stderr. `docker logs` for containers, direct output for CLI tools.
2. **Don't guess.** The error message tells you exactly what failed. Read it.
3. **Restart is safe.** All Helix components are designed for crash recovery. Restarting a service doesn't corrupt state.
4. **State files are idempotent.** Remove `~/.helix/state.json` and re-sync — it will rebuild from Forgejo's actual state.
5. **Escalate don't hack.** If a recovery procedure isn't documented here, escalate to human rather than experimenting with production state.

---

## Document Status

- [x] Forgejo recovery (unreachable, auth failed, mid-sync failure)
- [x] Chimera recovery (unreachable, import error, formation timeout, test suite)
- [x] Helix CLI recovery (build failure, config not found, state corruption, SSH conflict)
- [x] Docker recovery (container won't start, network broken)
- [x] GitReins recovery (secrets, tests)
- [x] OpenRouter recovery (dead key, rate limited)
- [x] General recovery principles (5 rules)
