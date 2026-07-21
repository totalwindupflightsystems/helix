// Package store implements the Helix SecretStore abstraction per
// specs/secret-management.md.
//
// SecretStore is a provider-agnostic interface for runtime secret
// storage. The current implementation (SOPSSecretStore) encrypts
// secrets at rest with SOPS+age keys. A future Vault adapter can be
// added under a sibling package implementing the same interface.
//
// Every method on the interface returns typed errors from this package's
// error catalog (ErrSecretNotFound, ErrStoreCorrupted, ErrStoreLocked,
// ErrStoreUninitialized, ErrKeyExists, ErrRotateInProgress,
// ErrProviderNotSupported). Callers can use errors.Is to match.
package store

import (
	"context"
	"time"
)

// SecretStore is the provider-agnostic interface for runtime secret
// storage. Provider implementations live in sub-packages (sops, future
// vault) and expose the same CRUD + rotate surface so the rest of the
// platform can swap providers without changing call sites.
//
// Concurrency: implementations MUST be safe for concurrent use. The SOPS
// implementation guards its in-memory state with an internal mutex and
// serialises Set/Delete/Rotate mutations against concurrent Get/List reads.
type SecretStore interface {
	// Get retrieves a secret by key. Returns ErrSecretNotFound if the
	// key does not exist. Implementations should decrypt on demand and
	// avoid long-lived plaintext caches.
	Get(ctx context.Context, key string) (string, error)

	// Set creates or updates a secret. Provider-specific encryption
	// (SOPS+age, Vault transit, etc.) is applied before storage.
	Set(ctx context.Context, key, value string) error

	// Delete removes a secret. Implementations MUST NOT return an error
	// if the key does not exist (delete is idempotent).
	Delete(ctx context.Context, key string) error

	// List returns all secret keys known to this store. The order is
	// implementation-defined; callers that need deterministic output
	// should sort the result.
	List(ctx context.Context) ([]string, error)

	// Rotate re-encrypts the store with a new identity (e.g. a new
	// age key file). Existing values are decrypted with the old
	// identity and re-encrypted with the new one. Returns
	// ErrRotateInProgress if a concurrent Rotate is already running.
	Rotate(ctx context.Context, newIdentity string) error
}

// SecretMeta holds metadata about a stored secret.
//
// NOTE: SecretMeta is not exposed by SecretStore today; SOPS-on-disk
// files only carry encrypted values and SOPS metadata (recipient, MAC,
// last-modified). The struct is part of the spec and is exported for
// future providers (e.g. Vault) and CLI tools that surface per-secret
// timestamps to operators.
type SecretMeta struct {
	Key       string    `json:"key"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// Provider is one of "sops", "vault", "env". Matches the
	// Config.Secrets.Provider field in pkg/config.
	Provider string `json:"provider"`
}

// Provider name constants. Used by both the SOPS implementation and by
// callers (CLI, config loader, audit events) to identify which backend
// produced a given SecretStore.
const (
	ProviderSOPS string = "sops"
	ProviderEnv  string = "env"
	ProviderEnv2 string = "vault" // reserved for PROD-001b
)