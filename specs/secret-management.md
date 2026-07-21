# Helix — Secret Management Specification

**Spec version:** 1.0
**Status:** Draft
**Last updated:** 2026-07-21
**Implements:** PROD-001
**Depends on:** config spec (`helix-config.md`), secrets scanner (`SPECIFICATION.md §6.2`)

---

## 1. Overview

Helix currently stores secrets in environment variables and `.env` files (gitignored). The existing secrets scanner (`pkg/security/secrets/`) detects accidental commits of credentials but does not provide a runtime secret store. This spec adds a programmable secret-management layer that operators and agents can use to store, retrieve, and rotate credentials without touching `.env` files manually.

**Key goals:**
- Replace manual `.env` editing with a `helix secrets set/get/rotate` CLI
- Support SOPS-encrypted files as the backing store (chosen over Vault for zero-infrastructure bootstrap)
- Provide a Go interface (`SecretStore`) that can be backed by SOPS files now, and by Vault later
- Integrate with the existing config loader so `~/.helix/config.yaml` can reference secrets by name

---

## 2. Dependencies

| Dependency | Version | Purpose |
|------------|---------|---------|
| `github.com/getsops/sops/v3` | v3.9+ | File encryption/decryption with age, PGP, or AWS KMS |
| `go.mozilla.org/sops/v3/age` | v3.9+ | Age key encryption (default identity) |
| External: `age` binary | v1.2+ | Key generation (`age-keygen`), optional for manual ops |
| Config loader (`pkg/config`) | existing | Store `secrets.provider` and `secrets.sops_key_path` settings |

**Decision: SOPS over Vault**
- Vault requires a running server, storage backend, and unseal ceremony — inappropriate for a standalone CLI tool
- SOPS encrypts files at rest with age/PGP keys; no server, no state, fits the gitops workflow
- The `SecretStore` interface is provider-agnostic; Vault adapter can be added as PROD-001b

---

## 3. Interface

```go
// Package secrets implements the Helix SecretStore abstraction.
// Provider implementations live in sub-packages (sops/, vault/).
package pkg/security/store

import (
    "context"
    "time"
)

// SecretStore is the provider-agnostic interface for runtime secret storage.
// Every method returns typed errors from the errors.go catalog (§7).
type SecretStore interface {
    // Get retrieves a secret by key. Returns ErrSecretNotFound if the key
    // does not exist.
    Get(ctx context.Context, key string) (string, error)

    // Set creates or updates a secret. provider-specific encryption is
    // applied before storage.
    Set(ctx context.Context, key, value string) error

    // Delete removes a secret. No error if the key does not exist.
    Delete(ctx context.Context, key string) error

    // List returns all secret keys known to this store.
    List(ctx context.Context) ([]string, error)

    // Rotate re-encrypts the store with a new identity (e.g. new age key).
    // Existing values are decrypted with the old key and re-encrypted.
    Rotate(ctx context.Context, newIdentity string) error
}

// SecretMeta holds metadata about a stored secret.
type SecretMeta struct {
    Key         string    `json:"key"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
    Provider    string    `json:"provider"` // "sops", "vault", "env"
}
```

**SOPS-backed implementation:**

```go
// SOPSSecretStore implements SecretStore via SOPS-encrypted YAML files.
// The default path is ~/.helix/secrets.enc.yaml.
type SOPSSecretStore struct {
    path    string          // encrypted file path
    keyPath string          // age key path (~/.helix/keys/age.txt)
    data    map[string]any  // in-memory decrypted cache
}

func NewSOPSStore(path, keyPath string) (*SOPSSecretStore, error)
func (s *SOPSSecretStore) Get(ctx context.Context, key string) (string, error)
func (s *SOPSSecretStore) Set(ctx context.Context, key, value string) error
func (s *SOPSSecretStore) Delete(ctx context.Context, key string) error
func (s *SOPSSecretStore) List(ctx context.Context) ([]string, error)
func (s *SOPSSecretStore) Rotate(ctx context.Context, newIdentity string) error
```

---

## 4. Behavior

### 4.1 CLI Surface — `helix secrets`

Extends the existing `helix secrets` subcommand (currently `scan`, `list-rules`, `help` only).

```
helix secrets set <key> <value>       Create/update a secret
helix secrets get <key>               Retrieve and print a secret
helix secrets delete <key>            Remove a secret
helix secrets list                    List all secret keys
helix secrets rotate <new-key-path>   Re-encrypt with new identity
helix secrets init                    Create ~/.helix/secrets.enc.yaml
```

### 4.2 Config Integration

Add a `secrets` section to `~/.helix/config.yaml`:

```yaml
secrets:
  provider: sops                      # "sops" | "env" (default: "env" for compat)
  sops_key_path: "~/.helix/keys/age.txt"  # age identity file
  store_path: "~/.helix/secrets.enc.yaml" # encrypted store file
```

When `provider: sops`, the config loader automatically initializes the SOPS store on startup. When `provider: env` (default), behavior is unchanged — secrets come from environment variables.

### 4.3 Auto-Init on First Use

When `helix secrets init` (or any `secrets` command with `provider: sops`) detects that no age key exists:
1. Generate a new age key at `~/.helix/keys/age.txt` (file perms 600)
2. Create an empty encrypted store at `~/.helix/secrets.enc.yaml`
3. Print the public key for backup

### 4.4 Integration with Identity Provisioner

The `pkg/identity.ProvisionerConfig` secrets (`AdminToken`, `AdminPassword`) should support reading from the secret store by key name when the CLI flag value is a `ref:` prefix:

```go
// If value starts with "ref:", look up the remainder in the secret store
if strings.HasPrefix(token, "ref:") {
    token, err = store.Get(ctx, strings.TrimPrefix(token, "ref:"))
}
```

---

## 5. Data Model

Encrypted file format (YAML with SOPS metadata):

```yaml
# ~/.helix/secrets.enc.yaml (SOPS-encrypted)
helix_secrets:
  forgejo_admin_token: ENC[AES256_GCM,data:...]
  forgejo_admin_password: ENC[AES256_GCM,data:...]
  chimera_api_key: ENC[AES256_GCM,data:...]
sops:
  kms: null
  gcp_kms: null
  azure_kv: null
  hc_vault: null
  age:
    - recipient: age1abc123...
      enc: |
        ---
        ...
  lastmodified: "2026-07-21T17:00:00Z"
  mac: ENC[AES256_GCM,data:...]
  pgp: null
  encrypted_regex: "^helix_secrets$"
  version: 3.9.0
```

---

## 6. States

| State | Description | Trigger |
|-------|-------------|---------|
| **Uninitialized** | No age key or encrypted store exists | First `helix secrets` command with `provider: sops` |
| **Initialized** | Age key + encrypted store present, store loaded | `helix secrets init` or auto-init |
| **Active** | Store loaded, CRUD operations available | After any `secrets` command |
| **Corrupted** | SOPS file fails decryption (wrong key, tampered) | Decryption error on any operation |
| **Rotating** | Re-encrypting with new identity mid-transaction | `helix secrets rotate` |
| **Locked** | Key file unreadable (missing, wrong perms) | Key file read error |

---

## 7. Error Catalog

| Error Code | HTTP-like | Description |
|------------|-----------|-------------|
| `ErrSecretNotFound` | 404 | Requested key does not exist in store |
| `ErrStoreCorrupted` | 500 | SOPS file cannot be decrypted or parsed |
| `ErrStoreLocked` | 403 | Key file unreadable (perms, missing) |
| `ErrStoreUninitialized` | 503 | No store exists; run `helix secrets init` |
| `ErrKeyExists` | 409 | Key already exists (Set with create-only semantics, if implemented) |
| `ErrRotateInProgress` | 409 | Rotate already in progress |
| `ErrProviderNotSupported` | 501 | `secrets.provider` value not recognized |

All errors implement `interface { Code() string; Message() string; Unwrap() error }`.

---

## 8. Testing

### 8.1 Unit Tests (SOPSStore)

| Test | Description |
|------|-------------|
| `TestNewSOPSStore_AutoInit` | No key + no store → auto-generates both |
| `TestNewSOPSStore_ExistingKey` | Key exists → opens store without re-init |
| `TestSetGet` | Set a value, Get it back, compare |
| `TestSetUpdate` | Set same key twice, Get returns latest |
| `TestDelete_Existing` | Delete existing key, Get returns ErrSecretNotFound |
| `TestDelete_Missing` | Delete non-existent key → no error |
| `TestList_Empty` | New store → List returns empty |
| `TestList_Populated` | Set 3 keys → List returns all 3 |
| `TestRotate` | Set key, Rotate with new age key, Get returns same value |
| `TestConcurrency` | Concurrent Set/Get on same store → no race |
| `TestCorruptedFile` | Tampered file → ErrStoreCorrupted |
| `TestMissingKeyFile` | Key file deleted → ErrStoreLocked |

### 8.2 Integration Tests

- `helix secrets init` → creates store + key
- `helix secrets set foo bar && helix secrets get foo` → "bar"
- `helix secrets rotate` → new key, Get still works
- Config with `secrets.provider: sops` → store auto-loaded on startup

### 8.3 Test Fixtures

Test keys should use `ref:` format from tests with temporary age keys generated in:
```go
tmpDir := t.TempDir()
keyPath := filepath.Join(tmpDir, "age.txt")
storePath := filepath.Join(tmpDir, "secrets.enc.yaml")
```

---

## 9. Security

### 9.1 Key File Protection

- `~/.helix/keys/age.txt` MUST have file perms 600 after creation
- The age public key should be printed during `init` for backup
- The private key file should be backed up manually (not by Helix)

### 9.2 Memory Safety

- Decrypted values SHOULD be cleared from memory after use (via `runtime.KeepAlive` + explicit zeroing where practical in Go)
- SOPS values are decrypted on-demand per `Get()` call, not bulk-loaded

### 9.3 Audit Trail

- Every `Set`, `Delete`, and `Rotate` operation writes an audit event to `pkg/audit`
- `Get` and `List` are NOT audited (too frequent, low sensitivity)

### 9.4 Fallback to Env

When `secrets.provider: env` (the default), all secret lookups fall through to `os.Getenv()`. This maintains backward compatibility with existing `.env` workflows.

---

## 10. Performance

| Operation | Latency Target | Notes |
|-----------|---------------|-------|
| `Get` (SOPS cached) | < 1ms | In-memory map after initial decrypt |
| `Get` (SOPS cold) | < 50ms | SOPS AES decryption of single value |
| `Set` | < 100ms | Encrypt + write entire file |
| `List` | < 1ms | Return keys from in-memory map |
| `Rotate` | < 500ms | Decrypt all, re-encrypt all with new key |
| Init / Auto-init | < 200ms | Key generation + file creation |

The `SOPSSecretStore` holds decrypted data in memory only for the lifetime of the `Get()` method call (decrypt, return, drop). The file is re-read and re-decrypted on every mutation (`Set`, `Delete`, `Rotate`). No long-lived cache to avoid stale plaintext accumulation.

---

## 11. Build Order

1. `pkg/security/store/store.go` — `SecretStore` interface + errors
2. `pkg/security/store/sops.go` — SOPS-backed implementation
3. `pkg/security/store/sops_test.go` — Unit tests with temp age keys
4. `cmd/helix/secrets_crud.go` — CLI commands (set/get/delete/list/rotate/init)
5. `pkg/config/loader.go` — Add `secrets` section to config struct + env-var binding
6. `cmd/helix/main.go` — Wire `secrets` subcommands into root command
7. Integration test: full `init → set → get → rotate → get` cycle
