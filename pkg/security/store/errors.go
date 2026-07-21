// errors.go — typed error catalog for pkg/security/store per
// specs/secret-management.md §7. All errors returned by SecretStore
// implementations must use these sentinels so callers can use
// errors.Is to match.
//
// Each error implements { Code(); Message(); Unwrap() error } per the
// spec. We deliberately do not satisfy net/http's error interface —
// the "HTTP-like" column in the spec is informational, mapping roughly
// to status codes for future REST adapters, not an actual HTTP contract.

package store

import (
	"errors"
	"fmt"
)

// SecretError is the shared interface implemented by every sentinel in
// this package. Callers that want to surface the stable Code() string
// (for logs / audit events) can type-assert to SecretError after a
// successful errors.Is match.
type SecretError interface {
	error
	Code() string
	Message() string
	Unwrap() error
}

// baseError carries the stable Code/Message pair plus the optional
// underlying cause. It is unexported so callers cannot construct
// arbitrary errors that satisfy SecretError — they must use one of the
// helpers below (or wrap one with fmt.Errorf("%w", ...)).
type baseError struct {
	code    string
	message string
	cause   error
}

func (e *baseError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.code, e.message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.code, e.message)
}

func (e *baseError) Code() string    { return e.code }
func (e *baseError) Message() string { return e.message }
func (e *baseError) Unwrap() error   { return e.cause }

// newBase builds a baseError with no underlying cause. Helpers below
// wrap it via fmt.Errorf when they need to attach context.
func newBase(code, message string) *baseError {
	return &baseError{code: code, message: message}
}

// withCause returns a copy of the baseError with the supplied cause.
//
//nolint:unused // Available for error-wrapping in future implementations.
func (e *baseError) withCause(cause error) *baseError {
	return &baseError{code: e.code, message: e.message, cause: cause}
}

// Error catalog. These are the package-level sentinels returned
// directly by SecretStore implementations. Wrap them with cause as
// needed for richer diagnostics.
var (
	// ErrSecretNotFound — requested key does not exist in store.
	// HTTP-like: 404.
	ErrSecretNotFound = newBase("secret_not_found", "secret does not exist")

	// ErrStoreCorrupted — SOPS file cannot be decrypted or parsed.
	// Typically a wrong key, tampered ciphertext, or unsupported
	// SOPS version. HTTP-like: 500.
	ErrStoreCorrupted = newBase("store_corrupted", "secret store is corrupted or unreadable")

	// ErrStoreLocked — key file unreadable (perms, missing).
	// HTTP-like: 403.
	ErrStoreLocked = newBase("store_locked", "key file is missing or unreadable")

	// ErrStoreUninitialized — no store exists; operator must run
	// `helix secrets init`. HTTP-like: 503.
	ErrStoreUninitialized = newBase("store_uninitialized", "secret store is not initialised; run `helix secrets init`")

	// ErrKeyExists — Set with create-only semantics (currently
	// unused by SOPSSecretStore which does upsert, but reserved
	// for future strict-create providers). HTTP-like: 409.
	ErrKeyExists = newBase("key_exists", "secret already exists and create-only mode is set")

	// ErrRotateInProgress — Rotate already in progress.
	// HTTP-like: 409.
	ErrRotateInProgress = newBase("rotate_in_progress", "rotate already in progress")

	// ErrProviderNotSupported — secrets.provider value not
	// recognised. HTTP-like: 501.
	ErrProviderNotSupported = newBase("provider_not_supported", "secret provider not supported")
)

// Wrap helpers — used by implementations to attach an underlying
// cause while preserving errors.Is matching against the sentinels.
//
// Usage:
//
//	return store.WrapSecretNotFound(fmt.Errorf("key %q: %w", k, err))
//
// Errors.Is(err, store.ErrSecretNotFound) will be true.

// WrapSecretNotFound wraps cause in ErrSecretNotFound.
func WrapSecretNotFound(cause error) error {
	return fmt.Errorf("%w: %v", ErrSecretNotFound, cause)
}

// WrapStoreCorrupted wraps cause in ErrStoreCorrupted.
func WrapStoreCorrupted(cause error) error {
	return fmt.Errorf("%w: %v", ErrStoreCorrupted, cause)
}

// WrapStoreLocked wraps cause in ErrStoreLocked.
func WrapStoreLocked(cause error) error {
	return fmt.Errorf("%w: %v", ErrStoreLocked, cause)
}

// WrapStoreUninitialized wraps cause in ErrStoreUninitialized.
func WrapStoreUninitialized(cause error) error {
	return fmt.Errorf("%w: %v", ErrStoreUninitialized, cause)
}

// WrapProviderNotSupported wraps cause in ErrProviderNotSupported.
func WrapProviderNotSupported(cause error) error {
	return fmt.Errorf("%w: %v", ErrProviderNotSupported, cause)
}

// AsSecretError extracts the SecretError interface from err, if err
// (or any error in its chain) implements it. Returns nil if no match.
// Useful for callers that want Code()/Message() without an errors.As
// to a concrete type.
func AsSecretError(err error) SecretError {
	var se SecretError
	if errors.As(err, &se) {
		return se
	}
	return nil
}
