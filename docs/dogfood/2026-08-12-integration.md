# Helix Dogfood Integration Report — 2026-08-12

Real-use run against live services (Forgejo :3030, Chimera :8765, scheduler
:9090). Verdict: 🟡 PROMISING-BUT-ROUGH. The flagship promise now **works
end-to-end for real** — an agent was provisioned into a live Forgejo with a
real account, SSH key, and PAT — but the idempotent repair path lies about
incomplete provisioning, and deprovision leaves credentials behind.

Previous run: 2026-08-02 (DF-001..009). All DF/GAP findings from that run
were re-verified fixed in reality (not just tests) — see "Verified fixes".

## What was verified working (live)

### Agent identity lifecycle (flagship — new since last run)

```bash
# 1. Create a portable HID (Ed25519)
helix identity create --name <agent>          # writes <agent>.hid + .key (mode 0600)

# 2. Verify + export (json / signed Nostr kind-0)
helix identity verify --hid <agent>.hid
helix identity export --hid <agent>.hid --format json
helix identity export --hid <agent>.hid --key <agent>.hid.key --format nostr

# 3. Provision into Forgejo (REAL account + SSH key + PAT) — needs BOTH:
#    --admin-token (user creation) AND --admin-user/--admin-password
#    (BasicAuth, PAT creation). known-friends.json schema:
#    {"version":1,"agents":{"<name>":{"display_name":"...","status":"active","tier":"flash"}}}
helix-identity provision <agent> \
  --forgejo-url http://localhost:3030 \
  --admin-token "$FORGEJO_ADMIN_TOKEN" \
  --admin-user <admin> --admin-password <pw> \
  --known-friends <path> --state-path <path>/state.json

# 4. Deprovision (revokes PAT; NOTE: SSH key stays registered — DF-012)
helix-identity deprovision <agent> --forgejo-url ... --admin-token ... \
  --admin-user ... --admin-password ... --known-friends ... --state-path ...
```

Verified against live Forgejo v1.21.11: user created (id 5), SSH key
registered ("Helix Agent — <agent> (flash)"), PAT `helix-identity-pat`
created, state file records forgejo_account_id/ssh_key_id/pat_id. Full
happy path in ~1s.

### Channels (new surface, CH-005) — flawless

```bash
export HELIX_CHANNELS_FILE=/tmp/channels.yaml   # default .helix/channels.yaml
helix channel create --name <ch> --type task --members <agent>
helix channel send --channel <ch> --message "..."
helix channel history --name <ch> --limit 5
helix channel list
helix channel archive --name <ch>               # idempotent
```

### Sources (new surface, SRC-006) — add/list/test work; test probe false-fails

```bash
export HELIX_SOURCES_FILE=/tmp/sources.yaml
helix source add --name forgejo-dev --type rest \
  --spec /tmp/forgejo-swagger.json --base-url http://localhost:3030/api/v1 --read-only
helix source list
helix source test --name forgejo-dev   # ✗ fails: base_url 404 (see DF-013)
helix source tools --name forgejo-dev  # needs Muster running
```

### Contracts (new surface) — registry workflow works, naming is confusing

```bash
export HELIX_CONTRACT_STORE=/tmp/contracts
helix contract create agent-identity     # registers id "agent-identity-openapi"
helix contract list
helix contract validate agent-identity-openapi   # bare id fails: "not found" (DF-014)
helix contract diff <new> <old>          # arg order matches usage text (SPEC-GAP-001 closed)
```

### Verified fixes from 2026-08-02 (re-checked live, all hold)

| Finding | Verification |
|---|---|
| DF-001 mergegate exit code | unattested push → `allowed:false` **and hook exit 1** |
| DF-002 subcommand --help | `helix estimate --help` shows estimate usage |
| DF-004/DF-005/GAP-005 CLI from /tmp | `helix estimate estimate "..." --model .. --provider ..` works from /tmp, ~$0.08 for a Go server task |
| GAP-001 status hang | `helix status` → healthy in 3.4s |
| GAP-004 prompt layouts | `helix prompt verify HEAD` OK (path-style refs) |
| GAP-007 dispatcher decompose | `helix dispatcher list-tasks --spec specs/agent-identity.md` → 1 task |

## Errors hit and their fixes (new this run)

| Error | Root cause | Right way |
|---|---|---|
| `cannot unmarshal array into ... map[string]*identity.Agent` | known-friends.json `agents` must be a MAP name→agent, not array | See schema above; docs gap → DF-015 |
| `ERROR: partial: provision ... failed: config: CreateToken: missing admin BasicAuth credentials` (exit 4, AFTER user+key created) | PAT creation needs BasicAuth (--admin-user/--admin-password), not just --admin-token | Pass all four creds; docs gap → DF-015 |
| retry then says `action=unchanged` exit 0 while Forgejo has **0 tokens** | Idempotency probe = user existence only; missing PAT never repaired | → DF-011 (P1) |
| `WARN key archive failed: rename ... archive/2026-08-12/id_ed25519: no such file or directory` | Deprovision never MkdirAll's the archive dir; SSH key also stays on Forgejo | → DF-012 (P2) |
| `helix source test` → `✗ rest base_url http://localhost:3030/api/v1: HTTP 404` | Probe GETs the base URL; Forgejo's /api/v1 root is 404 by design | → DF-013 (P2) |
| `contract validate agent-identity` → `contract: agent-identity not found` | Created id is `agent-identity-openapi`; bare id fails | → DF-014 (P2) |
| `contract validate --file /tmp/x.yaml` → `contract: /tmp/x.yaml not found` | Unknown flags silently dropped; path consumed as positional id | → DF-016 (P3) |
| `helix identity verify --name X` → `unknown flag: --name` | verify takes `--hid <path>`, not --name | Run `helix identity` bare for usage (accurate) |

## Integration lessons (the "aha")

1. **The identity flow is the real deal** — provisioning a genuine Forgejo
   user with SSH key + PAT takes one command and ~1s. It's the platform's
   core promise and it delivers.
2. **The trust gap is in the repair path, not the happy path.** Partial
   failures (missing BasicAuth) create half-provisioned agents that every
   later run reports as fine. State file vs live reconciliation is the fix.
3. **Security posture lesson:** deprovision ≠ revoke-all. Verify token AND
   key counts server-side after deprovision; currently only the PAT goes.

## Working scratch commands (with placeholders)

```bash
# Full lifecycle in ~/scratch, nothing written to the repo
export HELIX_STATE_PATH=~/scratch/state
export HELIX_CHANNELS_FILE=~/scratch/channels.yaml
export HELIX_SOURCES_FILE=~/scratch/sources.yaml
export HELIX_CONTRACT_STORE=~/scratch/contracts
```
