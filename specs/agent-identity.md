# Helix Feature 1: Agent Identity in Forgejo — Implementation Specification

**Version:** 1.0.0  
**Status:** Specification — implementation pending  
**Date:** 2026-06-19  
**Dependencies:** Hermes4Friends known-friends.json, Forgejo admin API

---

## 1. Mission

Give every Helix agent a real Forgejo account with an ED25519 SSH key, scoped permissions, and a personal access token — provisioned automatically from H4F's known-friends.json. This is the foundational feature; every other Helix subsystem (cost estimator, PR negotiation, prompt registry, marketplace) depends on agents having real git accounts.

---

## 2. Inputs

| Input | Source | Format |
|-------|--------|--------|
| known-friends.json | `/opt/hermes-demo/.hermes/h4f/known-friends.json` (Hetzner) | JSON — map of agent name → Agent object |
| Forgejo admin token | `FORGEJO_ADMIN_TOKEN` env var | String — admin-scoped PAT |
| Forgejo base URL | `--forgejo-url` flag or `FORGEJO_URL` env var | e.g., `https://git.helixloop.dev` |
| SSH key directory | `--ssh-key-dir` flag or `HELIX_SSH_KEY_DIR` env var | Default: `~/.helix/keys/` |

---

## 3. Operating Contract

- The CLI **never** hardcodes secrets. All credentials come from environment variables.
- Provisioning is **idempotent** — running sync twice produces the same state, never duplicates.
- Sync failures are **partial** — if 3 of 4 agents succeed, the 1 failure is reported individually.
- Offboarded agents are **deprovisioned** — tokens revoked, keys archived, account preserved (not deleted, to maintain git history attribution).
- All Forgejo API calls are **rate-limited** to 10 req/s with a token-bucket algorithm.

---

## 4. CLI Commands

### 4.1 `helix identity sync`

Read known-friends.json, provision all active agents (status=active), deprovision all offboarded agents (status=offboarded).

```
helix identity sync [flags]

Flags:
  --forgejo-url       Forgejo base URL (env: FORGEJO_URL)
  --admin-token       Forgejo admin PAT (env: FORGEJO_ADMIN_TOKEN)
  --known-friends     Path to known-friends.json (env: HELIX_KNOWN_FRIENDS)
  --ssh-key-dir       SSH key storage directory (env: HELIX_SSH_KEY_DIR)
  --dry-run           Print what would happen, make no changes
  --verbose           Log every API call

Exit codes: 0 (complete success), 4 (partial — one or more failures)
```

### 4.2 `helix identity provision <name>`

Provision a single agent regardless of status.

```
helix identity provision <name> [flags]
```

### 4.3 `helix identity deprovision <name>`

Offboard one agent: revoke PAT, archive SSH key, update local state.

```
helix identity deprovision <name> [flags]
```

### 4.4 `helix identity status`

Display provisioned agents and Forgejo account status in a table.

```
helix identity status [flags]

Output:
  AGENT     STATUS       FORGEJO ID   SSH KEY            PAT            LAST SYNC
  wojons    active       42           SHA256:abc123...   ****5678        2026-06-19T10:00:00Z
  llopez    active       43           SHA256:def456...   ****9012        2026-06-19T10:00:00Z
  dtoole    active       44           SHA256:ghi789...   ****3456        2026-06-19T10:00:00Z
  jrestrepo active       45           SHA256:jkl012...   ****7890        2026-06-19T10:00:00Z
```

### 4.5 `helix identity keygen <name>`

Generate a fresh ED25519 keypair and register it with Forgejo.

```
helix identity keygen <name> [flags]

Artifacts produced:
  ~/.helix/keys/<name>/id_ed25519         (private key, mode 0600)
  ~/.helix/keys/<name>/id_ed25519.pub      (OpenSSH public key)
  ~/.helix/keys/<name>/id_ed25519.state    (JSON — key ID, fingerprint, created_at)
```

---

## 5. Data Models

### 5.1 H4F Agent (known-friends.json → Go)

```go
type Agent struct {
    Name               string           // "wojons" — Forgejo login
    DisplayName        string           // Forgejo full_name
    Status             AgentStatus      // active | pending | offboarded
    Tier               AgentTier        // pro | flash
    OpenRouterKey      string           // held for reference only
    CoolifyServiceUUID string           // links to Coolify deployment
    TelegramBotToken   string           // links to Telegram bot
    ModelPreferences   ModelPreferences // {chat, vision, image_gen}
}
```

### 5.2 Forgejo User (API representation)

```go
type ForgejoAccount struct {
    ID          int64     // Forgejo user ID
    Login       string    // username
    LoginName   string    // login_name (defaults to login)
    FullName    string    // display_name from known-friends
    Email       string    // name@helix-agents.local
    AvatarURL   string    // generated identicon
    Created     time.Time
    IsAdmin     bool
}
```

### 5.3 Provisioning Result

```go
type ProvisioningResult struct {
    Agent            string        // agent name
    Action           SyncAction    // created | updated | unchanged | deprovisioned | skipped | failed
    ForgejoAccountID int64         // 0 on failure
    SSHKeyID         int64         // 0 on failure
    PATLastEight     string        // last 8 chars of token
    Error            string        // empty on success
    Duration         time.Duration
    Skipped          bool          // dry-run
}
```

---

## 6. Forgejo API Integration

### 6.1 Endpoints Used

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/v1/admin/users` | POST | Create agent account |
| `/api/v1/admin/users/{name}` | GET | Check if account exists (idempotency) |
| `/api/v1/admin/users/{name}` | DELETE | ⚠️ NOT USED — we archive, never delete |
| `/api/v1/user/keys` | POST | Register SSH public key |
| `/api/v1/users/{name}/tokens` | POST | Create PAT (requires BasicAuth) |
| `/api/v1/users/{name}/tokens/{id}` | DELETE | Revoke PAT on deprovision |

### 6.2 Account Creation Payload

```json
{
  "username": "wojons",
  "login_name": "wojons",
  "full_name": "wojons",
  "email": "wojons@helix-agents.local",
  "password": "<generated-32-char-random>",
  "must_change_password": true,
  "send_notify": false,
  "source_id": 0,
  "visibility": "limited"
}
```

### 6.3 PAT Scopes

```
write:repository, read:repository, write:issue, read:issue,
write:user, read:user
```

Agents do NOT receive `write:admin` — they cannot create other users or modify site config.

### 6.4 Branch Protection (per-repo, not user-level)

- `feat/*` branches: agents can push (write:repository scope)
- `main` / `master`: blocked via Forgejo branch protection rule requiring 1 human approval
- PR merge: requires 1 human approval + 1 agent approval (two-party sign-off)

---

## 7. SSH Key Management

### 7.1 Key Generation

- Algorithm: ED25519 (crypto/ed25519 stdlib)
- Private key: PKCS#8 PEM, mode 0600, passphrase-encrypted
- Public key: OpenSSH authorized_keys format (`ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA... helix-identity`)
- Key directory: `~/.helix/keys/<agent-name>/`

### 7.2 Key Registration

POST `/api/v1/user/keys` with:
```json
{
  "key": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA... helix-identity",
  "title": "Helix Agent — wojons (pro)"
}
```

### 7.3 Key Rotation

`helix identity keygen <name>` generates a new keypair, registers with Forgejo, archives the old key. Old keys are moved to `~/.helix/keys/<name>/archive/YYYY-MM-DD/`.

---

## 8. Error Handling

### 8.1 Partial Sync Recovery

If Forgejo is reachable but an individual account creation fails:
- The error is recorded in the ProvisioningResult for that agent
- Other agents continue provisioning
- CLI exits with code 4 (partial failure)
- Output table shows ✅ / ❌ per agent

### 8.2 Idempotency

Before creating an account, check GET `/api/v1/admin/users/{name}`:
- 200 → account exists, skip creation
- 404 → create account
- Other → fail with error

### 8.3 Network Failures

- Connection refused → exponential backoff (1s, 2s, 4s, max 30s)
- HTTP 429 → wait Retry-After header duration
- HTTP 5xx → retry up to 3 times

### 8.4 Dry-Run Mode

`--dry-run` prints all API calls that WOULD be made without executing them:
```
[DRY RUN] POST /api/v1/admin/users {"username":"wojons",...}
[DRY RUN] POST /api/v1/user/keys {"key":"ssh-ed25519 AAAA...",...}
[DRY RUN] POST /api/v1/users/wojons/tokens {...}
```

---

## 9. Test Strategy

### 9.1 Unit Tests
- `types_test.go` — JSON marshaling/unmarshaling for all data models
- `types_test.go` — AgentStatus.IsValid() for all three statuses
- `types_test.go` — DefaultPermission() returns expected policy
- `types_test.go` — MarshalPrivateKeyPEM produces valid PEM blocks
- `syncer_test.go` — known-friends.json parsing, filter by status
- `provisioner_test.go` — idempotency logic (account exists → skip)

### 9.2 Integration Tests (against real Forgejo)
- `integration_test.go` — provision → verify account → deprovision → verify removed
- `integration_test.go` — sync with empty known-friends.json (no-op)
- `integration_test.go` — sync with mixed active/offboarded agents
- `integration_test.go` — keygen → verify SSH key registered

### 9.3 Contract Tests
- `contract_test.go` — known-friends.json schema validation
- `contract_test.go` — Forgejo API response shape validation

### 9.4 Test Fixtures
- `testdata/known-friends.json` — 4 active + 2 offboarded agents
- `testdata/known-friends-empty.json` — no agents
- `testdata/known-friends-pending.json` — 1 pending agent

---

## 10. Open Questions

1. **Forgejo admin token provisioning:** How does the initial admin token reach the CLI? Currently requires manual setup. Future: Muster auto-generates admin token from Forgejo's admin CLI.
2. **Passphrase-encrypted PEM in stdlib:** Current implementation uses unencrypted PKCS#8 (mode 0600 filesystem protection). Full AES-CBC encrypted PEM requires either `golang.org/x/crypto` or a hand-rolled PBES2 implementation. Decision: use `golang.org/x/crypto/ssh` for v2; stdlib-only for v1.
3. **Agent avatar generation:** Currently Forgejo auto-generates an identicon from the email hash. Future: custom agent avatars from H4F profile data or image generation.
4. **Multi-instance sync:** Server-side state file (`~/.helix/state.json`) tracks provisioned accounts. If known-friends.json changes on one machine, another machine may be out of sync. The sync command is currently local-only.
5. **Budget enforcement:** Agent tier (pro/flash) should eventually map to spending limits. Currently advisory only — no cost enforcement in the identity subsystem (Feature 2: Cost Estimator handles this).

---

## 11. Implementation Workflow

```
Phase 1 — Types + Parsing
  1. Implement types.go (all data models) ✅ STUB DONE
  2. Write unit tests for JSON marshaling
  3. Write known-friends.json loader

Phase 2 — Provisioner
  4. Implement provisioner.go Forgejo HTTP client ✅ STUB DONE
  5. Implement account creation (POST /admin/users)
  6. Implement key registration (POST /user/keys)
  7. Implement PAT creation (POST /users/{name}/tokens)
  8. Write integration tests against real Forgejo

Phase 3 — Syncer
  9. Implement syncer.go sync logic ✅ STUB DONE
  10. Implement deprovisioning (revoke PAT, archive keys)
  11. Implement status report

Phase 4 — CLI
  12. Implement cobra commands with flag binding ✅ STUB DONE
  13. Wire sync, provision, deprovision, status, keygen
  14. Implement --dry-run mode

Phase 5 — Polish
  15. Rate limiting (token bucket)
  16. Exponential backoff for retries
  17. State file (provisioned account tracking)
```

---

## 12. File Layout

```
helix/
├── cmd/
│   └── helix-identity/
│       └── main.go              # Cobra CLI (544 lines)
├── pkg/
│   └── identity/
│       ├── types.go             # Data models (332 lines)
│       ├── types_test.go        # Unit tests
│       ├── provisioner.go       # Forgejo HTTP client (336 lines)
│       ├── provisioner_test.go  # Integration tests
│       ├── syncer.go            # Sync logic (310 lines)
│       └── syncer_test.go       # Unit + contract tests
├── specs/
│   └── agent-identity.md        # This document
├── testdata/
│   ├── known-friends.json       # Full test fixture
│   ├── known-friends-empty.json
│   └── known-friends-pending.json
├── go.mod
└── go.sum
```

---

## 13. Verification Gates

Before merging this feature:

- [ ] `go build ./cmd/helix-identity` exits 0
- [ ] `go vet ./...` clean
- [ ] `go test ./pkg/identity/...` all pass
- [ ] `helix identity sync --dry-run` prints expected API calls
- [ ] Integration test: provision → verify → deprovision against real Forgejo
- [ ] Contract test: known-friends.json fixtures parse correctly
- [ ] No secrets in git history (verify with gitleaks)
- [ ] README.md documents env vars required

---

## 14. References

- Forgejo Admin API: https://forgejo.org/docs/latest/user/api-admin/
- H4F Infrastructure: https://github.com/Hermes4Friends/infrastructure
- Helix Architecture Blueprint: https://totalwindupflightsystems.github.io/reports/origin-blueprint.html
- Go stdlib crypto/ed25519: https://pkg.go.dev/crypto/ed25519
- Go stdlib crypto/ssh: https://pkg.go.dev/golang.org/x/crypto/ssh
