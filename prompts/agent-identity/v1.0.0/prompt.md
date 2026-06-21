# Helix Agent Identity — Provisioning Prompt

You are implementing agent identity provisioning for the Helix platform. Your
task is to provision Helix agent accounts in a self-hosted Forgejo instance.

## Context

Helix agents are autonomous coding agents that need:
- A Forgejo user account (username = agent ID)
- An ED25519 SSH keypair for git operations
- A scoped Personal Access Token (PAT) for API access
- Idempotent lifecycle management (create once, update if exists)

## Forgejo API Reference

Base URL: FORGEJO_URL (env var, default http://localhost:3030)
Admin credentials: FORGEJO_ADMIN_USER / FORGEJO_ADMIN_PASSWORD

### Endpoints

1. **GET /api/v1/users/{username}** — Check if user exists
   - 200: user exists (return user object)
   - 404: user does not exist

2. **POST /api/v1/admin/users** — Create user
   - Body: `{"username": "...", "email": "...", "password": "...", "login_name": "..."}`
   - Auth: admin basic auth
   - 201: created
   - 422: validation error

3. **POST /api/v1/admin/users/{username}/keys** — Add SSH key
   - Body: `{"key": "ssh-ed25519 AAAA...", "title": "helix-{agent}"}`
   - Auth: admin basic auth
   - 201: created

4. **POST /api/v1/users/{username}/tokens** — Create PAT
   - Body: `{"name": "helix-pat", "scopes": ["read:repository", "write:repository"]}`
   - Auth: user basic auth (the newly created user's credentials)
   - 201: created (returns token value — THIS IS THE ONLY TIME YOU SEE IT)

5. **GET /api/v1/admin/users/{username}/keys** — List user's SSH keys
   - Auth: admin basic auth
   - 200: array of keys

6. **DELETE /api/v1/admin/users/{username}** — Delete user (deprovision)
   - Auth: admin basic auth
   - 204: deleted

## Behaviour Requirements

### Sync (idempotent)
1. Parse known-friends.json to get agent list
2. For each agent:
   a. Check if user exists (GET /api/v1/users/{username})
   b. If not: create user (POST /api/v1/admin/users)
   c. Check if SSH key exists (GET /api/v1/admin/users/{username}/keys)
   d. If not: generate ED25519 keypair, register public key
   e. Create scoped PAT if none exists
   f. Save state to ~/.helix/state.json
3. Report: created N, updated M, unchanged K

### Provision (single agent)
1. Create user if not exists
2. Generate and register ED25519 keypair
3. Create scoped PAT
4. Return: username, key fingerprint, PAT token

### Deprovision (single agent)
1. Verify user exists
2. Delete user (cascades: keys + tokens are removed)
3. Remove from state file
4. Return: confirmation

### Error Handling
- Network errors: retry with exponential backoff (3 attempts, 1s/2s/4s)
- 409 Conflict: user already exists → proceed to key registration
- 422 Validation: log error details, skip agent
- 401/403: check admin credentials, abort

### Dry Run
- `--dry-run` flag: preview all changes without making API calls
- Show what would be created/updated/deleted
- Still parse known-friends.json and validate

## Output Format

```
HELIX AGENT IDENTITY SYNC
=========================
Forgejo: http://localhost:3030
Agents in known-friends.json: 5

[DRY RUN] agent-1: would create user + SSH key + PAT
[DRY RUN] agent-2: user exists, would register SSH key + PAT
...
Summary: 5 agents, 3 would be created, 2 already provisioned
```

## Constraints
- Use only stdlib + github.com/totalwindupflightsystems/helix/pkg/identity
- ED25519 key generation: use crypto/ed25519 (no x/crypto dependency)
- State file: JSON at ~/.helix/state.json, atomic write (write temp + rename)
- All API calls must use context.WithTimeout (30s default)
- Never log PAT values after creation — mask in logs
