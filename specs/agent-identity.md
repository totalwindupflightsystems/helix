# Helix Feature 1 — Agent Identity in Forgejo

**Status:** v1 stub (build-ready, transport pending review)
**Spec version:** 1.0
**Last updated:** 2026-06-19
**Depends on:** known-friends.json (H4F), a self-hosted Forgejo instance
**Blocks:** Feature 2 (Cost Estimator), Feature 3 (PR Negotiation), Feature 4 (Prompt Registry), Feature 5 (Marketplace)

This document is the authoritative implementation reference for provisioning
Helix agent accounts in Forgejo. It is intended to be sufficient for an
engineer to implement the real Forgejo transport without asking clarifying
questions. The Go stubs in `pkg/identity/` and `cmd/helix-identity/` define
every interface; this document explains the contracts those interfaces encode.

---

## 1. Mission

Produce a build-ready implementation specification and Go stubs for
provisioning Helix agent accounts in a self-hosted Forgejo instance. This is
Priority 1 in a 5-feature roadmap. Every subsequent feature depends on
agents having real git accounts with SSH keys and scoped PATs.

The v1 deliverable is **not** a working Forgejo integration — it is a
specification + compilable Go stubs that define every interface, plus all
the non-transport logic (CLI, key generation, state management, dry-run,
error taxonomy) implemented and exercised.

---

## 2. Scope

### In scope (v1)
- CLI with 5 subcommands (`sync`, `provision`, `deprovision`, `status`, `keygen`)
- known-friends.json parsing and validation
- Forgejo API request/response type modeling (6 endpoints)
- ED25519 keypair generation + OpenSSH/PEM serialization (no x/crypto)
- Idempotency state file (~/.helix/state.json)
- Dry-run mode (full preview without touching Forgejo or filesystem)
- Error taxonomy with machine-readable exit codes
- Rate limiter + retry policy data structures

### Out of scope (v1)
- Real Forgejo HTTP transport (stubs return `ErrNotImplemented`)
- Branch protection enforcement (Forgejo repo settings, not code)
- Email sending (agents use @helix-agents.local pseudo-domain)
- Key passphrase encryption (filesystem mode 0600 protects at rest)
- Web UI / dashboard

---

## 3. Inputs

### 3.1 Agent Registry — known-friends.json

Location: `/opt/hermes-demo/.hermes/h4f/known-friends.json` (H4F host:
37.27.250.128). Overridable via `--known-friends` / `HELIX_KNOWN_FRIENDS`.

```json
{
  "version": 1,
  "updated_at": "2026-06-19T10:00:00Z",
  "agents": {
    "wojons": {
      "display_name": "wojons",
      "status": "active",
      "tier": "pro",
      "openrouter_key": "sk-or-...",
      "coolify_service_uuid": "...",
      "telegram_bot_token": "...",
      "model_preferences": {"chat": "...", "vision": "...", "image_gen": "..."}
    }
  }
}
```

| Field | Type | Values | Notes |
|-------|------|--------|-------|
| `name` (map key) | string | lowercase, unique | Becomes Forgejo login |
| `display_name` | string | human-readable | Becomes Forgejo full_name |
| `status` | enum | `active` \| `pending` \| `offboarded` | Drives sync behavior |
| `tier` | enum | `pro` \| `flash` | Capacity tier (informational in v1) |
| `openrouter_key` | string | `sk-or-...` | Held for reference; not used by identity |
| `coolify_service_uuid` | string | UUID | Held for reference |
| `telegram_bot_token` | string | bot token | Held for reference |
| `model_preferences` | object | `{chat, vision, image_gen}` | Held for reference |

**Current roster:**
- Active: `wojons` (pro), `llopez` (flash), `dtoole` (flash), `jrestrepo` (flash)
- Offboarded: `kellyv`, `bbala`

### 3.2 Forgejo API

| Setting | Flag | Env var |
|---------|------|---------|
| Base URL | `--forgejo-url` | `FORGEJO_URL` |
| Admin token | `--admin-token` | `FORGEJO_ADMIN_TOKEN` |
| Admin user (BasicAuth) | `--admin-user` | `FORGEJO_ADMIN_USER` |
| Admin password (BasicAuth) | `--admin-password` | `FORGEJO_ADMIN_PASSWORD` |

API reference: https://forgejo.org/docs/latest/user/api-admin/

**Important auth distinction:**
- `/admin/users` endpoints accept the admin **token** (`Authorization: token <T>`)
- `/users/{name}/tokens` endpoints require **BasicAuth** (admin username + password)
- `/user/keys` requires BasicAuth **as the agent** (agent username + temp password)

### 3.3 Output Directory

`/home/kara/helix/` — existing repo (`github.com/helixloop/helix`, Go 1.22.0).

---

## 4. Operating Contract

- **NEVER** write secrets to files. All credentials arrive via env vars or flags.
- **NEVER** invent API endpoints. Use only documented Forgejo admin API paths.
- **ALWAYS** return `ErrNotImplemented` from transport stubs in v1.
- **ALWAYS** produce buildable code: `go build ./cmd/helix-identity` must exit 0.
- **DO NOT** import packages beyond stdlib + `github.com/spf13/cobra`.
- For ED25519: use `crypto/ed25519` + `crypto/x509`. OpenSSH wire format is
  assembled by hand (4-byte length prefixes). `golang.org/x/crypto/ssh` is
  deliberately avoided for v1.

---

## 5. Assumptions

- Forgejo admin token is pre-provisioned by the operator.
- Forgejo instance is reachable at the configured URL.
- known-friends.json schema is stable (fields documented in §3.1).
- Agent accounts use synthesized emails: `<name>@helix-agents.local`.
- Temporary passwords are 32-char random strings (185 bits entropy).
- SSH key passphrase encryption uses unencrypted PKCS#8 for v1 (mode 0600).

---

## 6. Architecture

```
                    ┌─────────────────────────────────────┐
                    │           cmd/helix-identity         │
                    │  (Cobra CLI: 5 subcommands, flags)   │
                    └───────────────┬─────────────────────┘
                                    │
                    ┌───────────────▼─────────────────────┐
                    │          pkg/identity/syncer         │
                    │  Load → Classify → Provision/        │
                    │  Deprovision → Record → Persist      │
                    └───────┬───────────────┬─────────────┘
                            │               │
            ┌───────────────▼──┐       ┌────▼────────────────┐
            │ pkg/identity/    │       │ pkg/identity/        │
            │ provisioner      │       │ types                │
            │ (HTTP client,    │       │ (models, enums,      │
            │  rate limiter,   │       │  ED25519 keygen,     │
            │  retry policy)   │       │  error taxonomy)     │
            └──────────────────┘       └──────────────────────┘
```

**Layering rules:**
- `types.go` imports only stdlib. No dependencies on other package files.
- `provisioner.go` imports `types.go`. Owns all HTTP concerns.
- `syncer.go` imports both. Owns orchestration + filesystem state.
- `main.go` imports all three. Owns CLI + output rendering.

---

## 7. CLI Interface

```
helix identity sync [--forgejo-url URL] [--admin-token TOKEN] \
                    [--known-friends PATH] [--ssh-key-dir DIR] \
                    [--dry-run] [--verbose]

helix identity provision <name>   [flags]
helix identity deprovision <name> [flags]
helix identity status             [flags]
helix identity keygen <name>      [flags]
```

Every persistent flag resolves as: **CLI flag > env var > default**.

| Flag | Env var | Default |
|------|---------|---------|
| `--forgejo-url` | `FORGEJO_URL` | (none — required) |
| `--admin-token` | `FORGEJO_ADMIN_TOKEN` | (none — required) |
| `--admin-user` | `FORGEJO_ADMIN_USER` | (none — required for sync) |
| `--admin-password` | `FORGEJO_ADMIN_PASSWORD` | (none — required for sync) |
| `--known-friends` | `HELIX_KNOWN_FRIENDS` | `/opt/hermes-demo/.hermes/h4f/known-friends.json` |
| `--ssh-key-dir` | `HELIX_SSH_KEY_DIR` | `~/.helix/keys` |
| `--state-path` | `HELIX_STATE_PATH` | `~/.helix/state.json` |
| `--dry-run` | — | `false` |
| `--verbose`, `-v` | — | `false` |

---

## 8. Forgejo API Contract

### 8.1 Endpoints (all 6 modeled)

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| `POST` | `/api/v1/admin/users` | admin token | Create user account |
| `GET` | `/api/v1/admin/users/{name}` | admin token | Idempotency probe (200=exists, 404=create) |
| `POST` | `/api/v1/user/keys` | BasicAuth as agent | Register SSH public key |
| `POST` | `/api/v1/users/{name}/tokens` | BasicAuth as admin | Create PAT (capture `.Token` once) |
| `DELETE` | `/api/v1/users/{name}/tokens/{id}` | BasicAuth as admin | Revoke PAT (deprovision) |
| `DELETE` | `/api/v1/admin/users/{name}` | admin token | **NEVER USED** — we archive, never delete |

### 8.2 Request/Response Shapes

**POST /admin/users** — body:
```json
{
  "username": "wojons",
  "login_name": "wojons",
  "full_name": "wojons",
  "email": "wojons@helix-agents.local",
  "password": "<32-char random>",
  "must_change_password": true,
  "send_notify": false,
  "source_id": 0,
  "visibility": "limited"
}
```
Response: `ForgejoAccount` (`id`, `login`, `login_name`, `full_name`, `email`,
`avatar_url`, `created`, `is_admin`).

**POST /user/keys** — body:
```json
{"key": "ssh-ed25519 AAAA... helix-identity", "title": "Helix Agent — wojons (pro)"}
```
Response: `SSHKey` (`id`, `key`, `title`, `fingerprint`, `created_at`).

**POST /users/{name}/tokens** — body:
```json
{"name": "helix-identity-pat", "scopes": ["write:repository", "read:repository", "write:issue", "read:issue", "write:user", "read:user"]}
```
Response: `AccessToken` (`id`, `name`, `scopes`, `sha1`, `token`).
**The `token` field is only present on creation.** Capture immediately.

**DELETE /users/{name}/tokens/{id}** — response: `204 No Content`.

---

## 9. Permission Model

| Capability | Granted | Enforcement |
|------------|---------|-------------|
| Read all repos | yes | PAT scope `read:repository` |
| Create repos | yes | PAT scope `write:repository` |
| Push to `feat/*` | yes | PAT scope `write:repository` |
| Push to `main`/`master` | **NO** | Forgejo branch protection (not code) |
| Open PRs | yes | PAT scope `write:issue` |
| Merge solo | **NO** | Forgejo branch protection (requires human co-approval) |

PAT scopes (derived from `DefaultPermission().PermissionScopes()`):
```
read:repository, write:repository, read:issue, write:issue, read:user, write:user
```

Branch protection is configured separately on each repository via Forgejo
settings — this code does not touch repo settings, only user accounts.

---

## 10. Provisioning Flow

### 10.1 Per active agent
```
1. GET /admin/users/{name}
   ├─ 200 → account exists → ActionUnchanged (skip)
   └─ 404 → proceed to create
2. Generate 32-char temp password
3. POST /admin/users → ForgejoAccount
4. Generate ED25519 keypair → write 3 files to ~/.helix/keys/{name}/
5. POST /user/keys (BasicAuth as agent) → SSHKey
6. POST /users/{name}/tokens (BasicAuth as admin) → AccessToken
7. Mask token to ****<last8>, record state, ActionCreated
```

### 10.2 Per offboarded agent
```
1. Look up PAT id from state file
   ├─ no PAT → ActionSkipped
   └─ PAT exists → proceed
2. DELETE /users/{name}/tokens/{id}
3. Archive ~/.helix/keys/{name}/ → ~/.helix/keys/{name}/archive/YYYY-MM-DD/
4. Remove from state file, ActionDeprovisioned
```

### 10.3 Idempotency

Every operation is safe to re-run:
- Account exists → skip creation
- Key already registered → Forgejo dedupes by fingerprint (treated as success)
- PAT exists → Forgejo returns existing token list; we skip creation
- State file is the source of truth for "what we provisioned"

---

## 11. Filesystem Layout

### Inputs
```
/opt/hermes-demo/.hermes/h4f/known-friends.json   (or --known-friends path)
```

### Outputs (per agent)
```
~/.helix/keys/<agent>/id_ed25519         (private key, PKCS#8 PEM, mode 0600)
~/.helix/keys/<agent>/id_ed25519.pub     (OpenSSH public key, mode 0644)
~/.helix/keys/<agent>/id_ed25519.state   (JSON: fingerprint, created_at, mode 0600)
```

### State
```
~/.helix/state.json   (idempotency tracking, mode 0600)
```

Schema:
```json
{
  "version": 1,
  "last_sync": "2026-06-19T10:00:00Z",
  "agents": {
    "wojons": {
      "forgejo_account_id": 42,
      "ssh_key_id": 101,
      "ssh_fingerprint": "SHA256:abc123...",
      "pat_last_eight": "****5678",
      "pat_id": 201,
      "last_provisioned": "2026-06-19T10:00:00Z"
    }
  }
}
```

State writes are atomic (temp file + rename) to survive crashes mid-sync.

---

## 12. Error Taxonomy and Exit Codes

| Exit | Condition | Message format |
|------|-----------|----------------|
| 0 | Success (or dry-run, or empty agents) | — |
| 1 | Forgejo unreachable | `CONNECTION_REFUSED: <url>` |
| 2 | Unspecified operational error | — |
| 3 | File not found / auth failed / bad config | `FILE_NOT_FOUND: <path>` / `AUTH_FAILED: ...` |
| 4 | Partial sync (some agents failed) | `partial sync: N succeeded, M failed` |

**Error kinds** (mapped to exit codes via `TypedError.ExitCode()`):
- `config` → 3 (missing env, malformed flags, missing files)
- `network` → 1 (timeout, connection refused, 5xx)
- `api` → 3 (400/403/404/409 from Forgejo; 401 = AUTH_FAILED)
- `partial` → 4 (some agents failed during sync)
- `internal` → 2 (key generation, state write)

**Partial sync handling:** A failure on one agent does NOT abort the run.
The sync is best-effort across all agents; the result table shows ✅/❌ per
agent and the exit code reflects aggregate failure.

---

## 13. Rate Limiting and Retry

### Rate limiter (token bucket)
- Steady state: 10 requests/second
- Burst: 2 requests
- Hand-rolled (no `x/time/rate` dependency)
- v1 stub: `Acquire()` is a no-op (no real requests made)

### Retry policy
- **Connection refused** → exponential backoff: 1s, 2s, 4s, cap 30s (max 4 attempts)
- **HTTP 429** → honor `Retry-After` header duration
- **HTTP 5xx** → up to 3 retries with 2s spacing
- **HTTP 4xx (other than 429)** → no retry (client error)

Retry is transport-level and kicks in before the syncer sees an error.

---

## 14. ED25519 Key Generation

Implemented entirely with stdlib (`crypto/ed25519`, `crypto/x509`,
`crypto/sha256`, `encoding/pem`). No `golang.org/x/crypto`.

**OpenSSH public key format** (assembled by hand):
```
ssh-ed25519 <base64(packed)> helix-identity
```
where `packed` = `[4-byte BE len]["ssh-ed25519"][4-byte BE len][32-byte key]`.

**Private key format:** PKCS#8 PEM (`BEGIN PRIVATE KEY`), unencrypted.
Filesystem mode 0600 provides at-rest protection in v1.

**Fingerprint:** `SHA256:<base64(sha256(packed))>` with base64 padding stripped,
matching `ssh-keygen -l -f <file>`.

**Verified:** `ssh-keygen -l -f` independently confirms fingerprints generated
by this code match byte-for-byte.

---

## 15. Test Strategy

| Layer | File | What it tests | Mock/Real |
|-------|------|---------------|-----------|
| Unit | `types_test.go` | JSON marshaling, enum validity, `DefaultPermission()` | Pure unit |
| Unit | `types_test.go` | `GenerateKeyPair` → valid PEM, correct fingerprint | Pure unit |
| Unit | `syncer_test.go` | known-friends.json parsing, status filtering | File fixtures |
| Integration | `provisioner_test.go` | All 6 endpoints | Real Forgejo |
| Integration | `syncer_test.go` | Full sync → verify → deprovision | Real Forgejo |
| Contract | `contract_test.go` | known-friends.json schema | File fixtures |
| Contract | `contract_test.go` | Forgejo response shapes match types | Real Forgejo |
| E2E | `e2e_test.go` | provision → verify → deprovision → verify removed | Real Forgejo |

**Integration tests skip if `FORGEJO_URL` is unset.** Unit tests never make
network calls.

Test fixtures (to be created in `pkg/identity/testdata/`):
- `known-friends.json` (4 active + 2 offboarded)
- `known-friends-empty.json` (0 agents)
- `known-friends-pending.json` (only pending agents)

---

## 16. Observability

- `--verbose` logs every API call with timing:
  `timestamp [level] agent=NAME action=ACTION status=CODE duration=MS`
- Default logging via stdlib `log` package to stderr.
- Exit codes are machine-readable for cron jobs.
- Result table shows per-agent outcome (✅/❌, action, SSH fingerprint, PAT mask).

---

## 17. Implementation Status (v1 stub)

| Component | Status | Notes |
|-----------|--------|-------|
| CLI (5 subcommands) | ✅ Live | All wired, flag/env binding works |
| known-friends.json parsing | ✅ Live | Validation + status filtering |
| Forgejo type modeling | ✅ Live | All 6 endpoints, JSON tags verified |
| ED25519 keygen | ✅ Live | ssh-keygen-verified fingerprints |
| State file management | ✅ Live | Atomic writes, load/save |
| Dry-run mode | ✅ Live | Full preview, no side effects |
| Error taxonomy | ✅ Live | 5 kinds → 4 exit codes |
| Rate limiter structure | ✅ Live | Token bucket (Acquire is no-op stub) |
| Retry policy structure | ✅ Live | Constants + BackoffFor() |
| Forgejo HTTP transport | ⏳ Stub | All methods return `ErrNotImplemented` |

The transport is the only piece between this spec and a working integration.
Drop-in replacement: implement the 6 methods in `provisioner.go` without
touching any caller.

---

## 18. Verification Checklist

- [x] `go build ./cmd/helix-identity` exits 0
- [x] `go vet ./...` clean
- [x] All stub methods return `ErrNotImplemented`
- [x] No imports beyond stdlib + cobra
- [x] No hardcoded credentials, URLs, or tokens
- [x] All 6 Forgejo endpoints modeled
- [x] All 5 CLI commands wired
- [x] State file schema documented
- [x] Error taxonomy covers all failure modes
- [x] ED25519 fingerprints verified against `ssh-keygen`
- [x] Dry-run output matches §19 example format
- [x] Exit codes match §12 taxonomy

---

## 19. Example Outputs

### Dry-run (`sync --dry-run`)
```
[DRY RUN] POST /api/v1/admin/users {"username":"wojons","login_name":"wojons","full_name":"wojons","email":"wojons@helix-agents.local","password":"<redacted>","must_change_password":true,"send_notify":false,"source_id":0,"visibility":"limited"}
[DRY RUN] POST /api/v1/user/keys {"key":"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGx... helix-identity","title":"Helix Agent — wojons (pro)"}
[DRY RUN] POST /api/v1/users/wojons/tokens {"name":"helix-identity-pat","scopes":["write:repository","read:repository","write:issue","read:issue","write:user","read:user"]}
──────────────────────────────────────────────────────────────────
AGENT      STATUS       ACTION
wojons     active       would create
llopez     active       would create
dtoole     active       would create
jrestrepo  active       would create
kellyv     offboarded   would deprovision
bbala      offboarded   would deprovision
──────────────────────────────────────────────────────────────────
DRY RUN COMPLETE — 6 operations simulated, 0 executed
```

### Status (`status`)
```
AGENT       FORGEJO ID  SSH KEY              PAT        LAST SYNC
wojons      42          SHA256:abc123def...  ****5678   2026-06-19T10:00:00Z
llopez      43          SHA256:def456ghi...  ****9012   2026-06-19T10:00:00Z
```

### Result table (post-sync)
```
AGENT        STATUS      ACTION   SSH KEY                      PAT      DURATION
✅ wojons     active      created  SHA256:b8sZlCUb9BXx6ekDu...  —        1ms
✅ llopez     active      created  SHA256:doYFvwjRqjInXQFAs...  —        1ms
❌ dtoole     active      failed   —                            —        2ms
```
