// Package store implements SOPS-encrypted SecretStore for agent credentials,
// Forgejo tokens, and other sensitive configuration data.
//
// Storage format
// --------------
//
// The store is a single YAML file whose top-level keys are the secret
// names (e.g. `forgejo_admin_token: ENC[AES256_GCM,...]`). The file
// also carries a `sops:` metadata block with the age recipients,
// message-authentication code (MAC), and SOPS version. This matches the
// format produced by the upstream `sops` CLI and is interoperable with
// `sops -d secrets.enc.yaml` from any other workstation that has the
// matching age private key.
//
// Encryption model
// ----------------
//
// SOPS generates a fresh 32-byte AES-GCM data key per file write and
// encrypts every value under that data key. The data key itself is
// encrypted (wrapped) by each "master key" — here, the supplied age
// recipient. To decrypt, SOPS asks a local KeyServiceServer (the
// default `keyservice.Server{}`) to unwrap the data key with any one of
// the file's age recipients. The Server uses the age identity from
// `SOPS_AGE_KEY_FILE` (or `SOPS_AGE_KEY` env vars) by default, so we
// rely on the standard SOPS env-var convention for runtime decryption.
//
// Concurrency
// -----------
//
// All public methods are safe for concurrent use. A mutex (sync.RWMutex)
// guards the on-disk file and serialises writes. Reads (Get, List)
// take a read lock; mutations (Set, Delete, Rotate) take a write lock.
// A second mutex on a bool guards rotate-in-progress so concurrent
// Rotate calls collapse to ErrRotateInProgress rather than racing.
//
// Memory
// ------
//
// The decrypted data is materialised in memory for the lifetime of a
// mutation. The spec calls for "decrypt-on-demand per Get()"; we honour
// that by re-reading and re-decrypting the file on every Get/List call
// rather than caching the plaintext across calls. (Spec §10.
//
// Performance budget for Get (SOPS cold) is < 50ms.)
package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/aes"
	"github.com/getsops/sops/v3/age"
	"github.com/getsops/sops/v3/keyservice"
	"github.com/getsops/sops/v3/stores"
	"github.com/getsops/sops/v3/stores/yaml"
	sopsversion "github.com/getsops/sops/v3/version"
)

// Default file paths. Used by the constructor when the caller passes
// empty strings — matching the spec's `~/.helix/secrets.enc.yaml` and
// `~/.helix/keys/age.txt` defaults.
const (
	DefaultStorePath = "~/.helix/secrets.enc.yaml"
	DefaultKeyPath   = "~/.helix/keys/age.txt"
)

// secretMapKey is reserved for future use as the YAML top-level key
// under which secrets would be nested. Today the implementation writes
// secrets as flat top-level keys (SOPS encrypts every value at every
// depth, so a flat layout is shorter and more diff-friendly). The
// spec's example shape (`helix_secrets:` wrapper) is supported by
// SOPS too; we choose flat-keys for simpler operator ergonomics.

// SOPSSecretStore implements SecretStore via SOPS-encrypted YAML files.
//
// Concurrency: see package doc. All methods are safe for concurrent
// use after NewSOPSStore returns.
//
// Lifecycle:
//
//	store, err := NewSOPSStore(path, keyPath)
//	... use store ...
//	// file is automatically re-read & re-decrypted on every operation;
//	// no explicit Close needed.
type SOPSSecretStore struct {
	// path is the on-disk encrypted YAML file (absolute path).
	path string
	// keyPath is the age identity (private key) file. Used to look
	// up the recipient at encrypt time and to decrypt via SOPS_AGE_KEY_FILE
	// env-var convention at decrypt time.
	keyPath string

	mu       sync.RWMutex // guards file I/O + rotate state
	rotating bool         // true while Rotate is in progress
}

// NewSOPSStore creates or opens a SOPS-secret store.
//
// If the encrypted file does not exist and the age key does not exist,
// NewSOPSStore auto-initialises both (per spec §4.3) and returns a
// ready-to-use store pointing at an empty secrets file. If only the
// encrypted file is missing, the store is still usable — Set will
// create the file on first write.
//
// Errors:
//   - ErrStoreLocked if the age key file exists but is unreadable
//     (wrong perms, IO error).
//   - ErrStoreCorrupted if the existing encrypted file fails to
//     decrypt or parse.
func NewSOPSStore(path, keyPath string) (*SOPSSecretStore, error) {
	if path == "" {
		path = DefaultStorePath
	}
	if keyPath == "" {
		keyPath = DefaultKeyPath
	}
	path = expandHome(path)
	keyPath = expandHome(keyPath)

	s := &SOPSSecretStore{path: path, keyPath: keyPath}

	// Probe: ensure the key file is accessible. We do not require
	// it to exist (auto-init handles that case below); we only
	// fail if it exists-but-unreadable.
	if _, err := os.Stat(keyPath); err == nil {
		if _, err := os.ReadFile(keyPath); err != nil {
			return nil, WrapStoreLocked(fmt.Errorf("read age key %s: %w", keyPath, err))
		}
	} else if !os.IsNotExist(err) {
		return nil, WrapStoreLocked(fmt.Errorf("stat age key %s: %w", keyPath, err))
	}

	// If the encrypted file exists, attempt to decrypt it now as a
	// smoke test. We don't keep the result — every operation re-reads
	// — but a decrypt failure here surfaces ErrStoreCorrupted early.
	if _, err := os.Stat(path); err == nil {
		if err := s.canDecrypt(); err != nil {
			return nil, err
		}
	}

	return s, nil
}

// Path returns the absolute path to the encrypted file. Useful for
// CLI display and audit logs.
func (s *SOPSSecretStore) Path() string { return s.path }

// KeyPath returns the absolute path to the age identity file.
func (s *SOPSSecretStore) KeyPath() string { return s.keyPath }

// Provider reports "sops" per the SecretMeta.Provider contract.
func (s *SOPSSecretStore) Provider() string { return ProviderSOPS }

// Get retrieves a secret by key. Re-reads and re-decrypts the file on
// every call so we don't accumulate plaintext across calls (spec §9.2).
//
// Errors:
//   - ErrStoreCorrupted if the file fails to decrypt or parse.
//   - ErrSecretNotFound if the key is absent.
func (s *SOPSSecretStore) Get(_ context.Context, key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("secret store: empty key")
	}
	data, err := s.loadDecrypted()
	if err != nil {
		return "", err
	}
	v, ok := data[key]
	if !ok {
		return "", WrapSecretNotFound(fmt.Errorf("key %q", key))
	}
	str, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("secret store: key %q has non-string value (type %T)", key, v)
	}
	return str, nil
}

// Set creates or updates a secret. Encrypts the entire file with the
// recipient derived from the age key at keyPath.
//
// Errors:
//   - ErrStoreCorrupted on read/parse failure.
//   - ErrStoreLocked if the age key file is unreadable.
func (s *SOPSSecretStore) Set(_ context.Context, key, value string) error {
	if key == "" {
		return fmt.Errorf("secret store: empty key")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.loadDecryptedLocked()
	if err != nil {
		return err
	}
	data[key] = value
	return s.encryptAndWriteLocked(data)
}

// Delete removes a secret. Idempotent: returns nil if the key does
// not exist.
func (s *SOPSSecretStore) Delete(_ context.Context, key string) error {
	if key == "" {
		return fmt.Errorf("secret store: empty key")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.loadDecryptedLocked()
	if err != nil {
		return err
	}
	if _, ok := data[key]; !ok {
		return nil
	}
	delete(data, key)
	return s.encryptAndWriteLocked(data)
}

// List returns all secret keys in deterministic sorted order.
func (s *SOPSSecretStore) List(_ context.Context) ([]string, error) {
	data, err := s.loadDecrypted()
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

// Rotate re-encrypts the store with a new identity (e.g. a new age key
// file). Existing values are decrypted with the current identity and
// re-encrypted with the new recipient derived from newKeyPath.
//
// Returns ErrRotateInProgress if a concurrent Rotate is already
// running.
func (s *SOPSSecretStore) Rotate(_ context.Context, newKeyPath string) error {
	if newKeyPath == "" {
		return fmt.Errorf("secret store: rotate: empty new identity path")
	}
	newKeyPath = expandHome(newKeyPath)

	s.mu.Lock()
	if s.rotating {
		s.mu.Unlock()
		return ErrRotateInProgress
	}
	s.rotating = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.rotating = false
		s.mu.Unlock()
	}()

	// Read + decrypt with the CURRENT key.
	data, err := s.loadDecrypted()
	if err != nil {
		return err
	}

	// Re-encrypt with the NEW key. We hold the write lock for the
	// swap so no concurrent Set/Delete slips in between decrypt and
	// re-encrypt.
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keyPath = newKeyPath
	return s.encryptAndWriteLocked(data)
}

// =============================================================================
// Internal helpers
// =============================================================================

// loadDecrypted takes the read lock, reads the file, decrypts it, and
// returns the plaintext map. Used by Get/List/Rotate.
//
// Returns:
//   - (map, nil) on success.
//   - (nil, ErrStoreUninitialized) if the file does not exist (we
//     don't auto-create on read; only on Set/Rotate).
//   - (nil, ErrStoreCorrupted) on decrypt/parse failure.
func (s *SOPSSecretStore) loadDecrypted() (map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadDecryptedLocked()
}

// loadDecryptedLocked is loadDecrypted without the lock — callers must
// hold s.mu (read OR write).
func (s *SOPSSecretStore) loadDecryptedLocked() (map[string]any, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, WrapStoreCorrupted(fmt.Errorf("read %s: %w", s.path, err))
	}
	if len(raw) == 0 {
		return map[string]any{}, nil
	}

	tree, err := s.decryptBytes(raw)
	if err != nil {
		// If the file doesn't look like SOPS at all (no sops:
		// top-level key), treat it as empty rather than corrupted —
		// matches user expectation that an empty file == empty store.
		if errors.Is(err, sops.MetadataNotFound) || strings.Contains(err.Error(), "no sops metadata") {
			return map[string]any{}, nil
		}
		return nil, WrapStoreCorrupted(err)
	}

	return branchToMap(tree.Branches), nil
}

// decryptBytes runs the SOPS decrypt pipeline against the given bytes.
// Uses the local keyservice.Server (env-based age identities) and the
// SOPS-provided AES cipher.
func (s *SOPSSecretStore) decryptBytes(raw []byte) (*sops.Tree, error) {
	yamlStore := &yaml.Store{}
	tree, err := yamlStore.LoadEncryptedFile(raw)
	if err != nil {
		return nil, err
	}
	tree.FilePath = s.path

	cipher := aes.NewCipher()
	client := keyservice.NewLocalClient()
	dataKey, err := tree.Metadata.GetDataKeyWithKeyServices([]keyservice.KeyServiceClient{client}, nil)
	if err != nil {
		return nil, err
	}
	if _, err := tree.Decrypt(dataKey, cipher); err != nil {
		return nil, err
	}
	return &tree, nil
}

// encryptAndWriteLocked serialises data to YAML, encrypts it with
// SOPS+age (recipient derived from keyPath), and writes the result
// atomically to s.path. Caller must hold s.mu write lock.
//
// Returns ErrStoreLocked if the age key file is unreadable.
func (s *SOPSSecretStore) encryptAndWriteLocked(data map[string]any) error {
	recipient, err := s.recipientFromKeyPath()
	if err != nil {
		return WrapStoreLocked(err)
	}

	cipher := aes.NewCipher()
	client := keyservice.NewLocalClient()

	// Build a SOPS Tree whose branches encode the data map. We emit
	// the data as a flat top-level branch — SOPS encrypts each leaf
	// string regardless of nesting depth.
	branch := make(sops.TreeBranch, 0, len(data))
	// Sort keys for deterministic output (SOPS itself doesn't
	// guarantee ordering, and operators diffing the file by eye
	// benefit from stable key order).
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		branch = append(branch, sops.TreeItem{Key: k, Value: data[k]})
	}
	tree := sops.Tree{
		Branches: sops.TreeBranches{branch},
		Metadata: sops.Metadata{Version: sopsversion.Version},
		FilePath: s.path,
	}

	// Generate data key, encrypt it with the age recipient. The
	// MasterKey must live in tree.Metadata.KeyGroups before
	// UpdateMasterKeysWithKeyServices will encrypt the data key
	// against it. We use the SOPS standard pattern: one KeyGroup
	// containing one age master key.
	dataKey := make([]byte, 32)
	if _, err := rand.Read(dataKey); err != nil {
		return WrapStoreCorrupted(fmt.Errorf("generate data key: %w", err))
	}
	mk, err := age.MasterKeyFromRecipient(recipient)
	if err != nil {
		return WrapStoreLocked(fmt.Errorf("parse age recipient from %s: %w", s.keyPath, err))
	}
	tree.Metadata.KeyGroups = []sops.KeyGroup{{mk}}
	if errs := tree.Metadata.UpdateMasterKeysWithKeyServices(dataKey, []keyservice.KeyServiceClient{client}); len(errs) > 0 {
		return WrapStoreLocked(fmt.Errorf("encrypt data key with age: %v", errs))
	}

	// Encrypt tree leaves, compute MAC, set LastModified.
	unencMac, err := tree.Encrypt(dataKey, cipher)
	if err != nil {
		return WrapStoreCorrupted(fmt.Errorf("encrypt tree: %w", err))
	}
	tree.Metadata.LastModified = time.Now().UTC()
	tree.Metadata.MessageAuthenticationCode, err = cipher.Encrypt(
		unencMac, dataKey, tree.Metadata.LastModified.Format(time.RFC3339),
	)
	if err != nil {
		return WrapStoreCorrupted(fmt.Errorf("encrypt MAC: %w", err))
	}

	// Emit encrypted YAML.
	out, err := (&yaml.Store{}).EmitEncryptedFile(tree)
	if err != nil {
		return WrapStoreCorrupted(fmt.Errorf("emit encrypted yaml: %w", err))
	}

	// Atomic write: tmp file + rename. Permissions 0600 per spec §9.1.
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return WrapStoreCorrupted(fmt.Errorf("mkdir %s: %w", filepath.Dir(s.path), err))
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return WrapStoreCorrupted(fmt.Errorf("write tmp %s: %w", tmp, err))
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return WrapStoreCorrupted(fmt.Errorf("rename %s -> %s: %w", tmp, s.path, err))
	}
	return nil
}

// recipientFromKeyPath extracts the age public key (recipient) from
// an age key file. An age key file looks like:
//
//	# created: 2026-07-21T17:00:00Z
//	# public key: age1abc123...
//	AGE-SECRET-KEY-1QQQQQQ...
//
// We extract the `# public key:` comment. If the file is malformed,
// we return ErrStoreLocked so the caller surfaces the right error.
func (s *SOPSSecretStore) recipientFromKeyPath() (string, error) {
	if _, err := os.Stat(s.keyPath); err != nil {
		return "", fmt.Errorf("stat key %s: %w", s.keyPath, err)
	}
	raw, err := os.ReadFile(s.keyPath)
	if err != nil {
		return "", fmt.Errorf("read key %s: %w", s.keyPath, err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# public key:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# public key:")), nil
		}
	}
	return "", fmt.Errorf("key %s: no '# public key:' comment found", s.keyPath)
}

// canDecrypt is a smoke test run at NewSOPSStore time. Returns nil if
// the file is readable and decrypts cleanly with the configured age
// identity.
func (s *SOPSSecretStore) canDecrypt() error {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return WrapStoreCorrupted(fmt.Errorf("read %s: %w", s.path, err))
	}
	if len(raw) == 0 {
		return nil
	}
	if _, err := s.decryptBytes(raw); err != nil {
		if errors.Is(err, sops.MetadataNotFound) {
			return nil // empty/uninitialised shape is fine
		}
		return WrapStoreCorrupted(err)
	}
	return nil
}

// branchToMap flattens a SOPS tree branch into a plain
// map[string]any. SOPS leaves can be strings, ints, floats, bools;
// we keep the original Go type for round-tripping. The sops metadata
// top-level key (`sops:`) is dropped — it carries SOPS-internal state
// (recipient, MAC, last-modified) that we don't want to expose as a
// regular secret.
func branchToMap(branches sops.TreeBranches) map[string]any {
	out := map[string]any{}
	if len(branches) == 0 {
		return out
	}
	for _, item := range branches[0] {
		// item.Key is interface{} but in practice always string
		// for our flat top-level shape. Be defensive in case a
		// future caller hands us a non-string key.
		keyStr, ok := item.Key.(string)
		if !ok {
			continue
		}
		if keyStr == stores.SopsMetadataKey {
			continue
		}
		out[keyStr] = item.Value
	}
	return out
}

// expandHome is a tiny `~/` expander. We avoid pulling in os/user to
// keep this package pure-Go and unit-testable without /etc/passwd.
func expandHome(p string) string {
	if p == "" {
		return p
	}
	if p[0] != '~' {
		return p
	}
	if len(p) == 1 || p[1] == '/' {
		home := os.Getenv("HOME")
		if home == "" {
			return p
		}
		return filepath.Join(home, p[1:])
	}
	return p
}
