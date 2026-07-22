// accessors_test.go — tests for SOPSSecretStore accessors (Path, KeyPath,
// Provider) and the error-wrapping helpers. These are pure-Go unit tests
// with no SOPS infrastructure — every function under test operates on
// plain Go values or strings.
package store

import (
	"errors"
	"testing"
)

// ─────────────────────────────────────────────
// Accessor tests
// ─────────────────────────────────────────────

func TestPathAccessor(t *testing.T) {
	s := &SOPSSecretStore{path: "/tmp/test/secrets.enc.yaml"}
	if got := s.Path(); got != "/tmp/test/secrets.enc.yaml" {
		t.Errorf("Path() = %q, want %q", got, "/tmp/test/secrets.enc.yaml")
	}
}

func TestKeyPathAccessor(t *testing.T) {
	s := &SOPSSecretStore{keyPath: "/tmp/test/keys/age.txt"}
	if got := s.KeyPath(); got != "/tmp/test/keys/age.txt" {
		t.Errorf("KeyPath() = %q, want %q", got, "/tmp/test/keys/age.txt")
	}
}

func TestProviderAccessor(t *testing.T) {
	s := &SOPSSecretStore{}
	if got := s.Provider(); got != ProviderSOPS {
		t.Errorf("Provider() = %q, want %q", got, ProviderSOPS)
	}
}

// ─────────────────────────────────────────────
// Error wrapper tests
// ─────────────────────────────────────────────

func TestWrapSecretNotFound(t *testing.T) {
	inner := errors.New("underlying error")
	wrapped := WrapSecretNotFound(inner)
	if !errors.Is(wrapped, ErrSecretNotFound) {
		t.Error("WrapSecretNotFound: errors.Is(wrapped, ErrSecretNotFound) is false")
	}
	var se SecretError
	if !errors.As(wrapped, &se) {
		t.Error("WrapSecretNotFound: wrapped does not satisfy SecretError")
	}
	if se.Code() != "secret_not_found" {
		t.Errorf("WrapSecretNotFound: Code() = %q, want %q", se.Code(), "secret_not_found")
	}
}

func TestWrapStoreCorrupted(t *testing.T) {
	inner := errors.New("corrupt file")
	wrapped := WrapStoreCorrupted(inner)
	if !errors.Is(wrapped, ErrStoreCorrupted) {
		t.Error("WrapStoreCorrupted: errors.Is(wrapped, ErrStoreCorrupted) is false")
	}
	var se SecretError
	if !errors.As(wrapped, &se) {
		t.Error("WrapStoreCorrupted: wrapped does not satisfy SecretError")
	}
	if se.Code() != "store_corrupted" {
		t.Errorf("WrapStoreCorrupted: Code() = %q, want %q", se.Code(), "store_corrupted")
	}
}

func TestWrapStoreLocked(t *testing.T) {
	inner := errors.New("permission denied")
	wrapped := WrapStoreLocked(inner)
	if !errors.Is(wrapped, ErrStoreLocked) {
		t.Error("WrapStoreLocked: errors.Is(wrapped, ErrStoreLocked) is false")
	}
	var se SecretError
	if !errors.As(wrapped, &se) {
		t.Error("WrapStoreLocked: wrapped does not satisfy SecretError")
	}
	if se.Code() != "store_locked" {
		t.Errorf("WrapStoreLocked: Code() = %q, want %q", se.Code(), "store_locked")
	}
}

func TestWrapStoreUninitialized(t *testing.T) {
	inner := errors.New("no store exists")
	wrapped := WrapStoreUninitialized(inner)
	if !errors.Is(wrapped, ErrStoreUninitialized) {
		t.Error("WrapStoreUninitialized: errors.Is(wrapped, ErrStoreUninitialized) is false")
	}
	var se SecretError
	if !errors.As(wrapped, &se) {
		t.Error("WrapStoreUninitialized: wrapped does not satisfy SecretError")
	}
	if se.Code() != "store_uninitialized" {
		t.Errorf("WrapStoreUninitialized: Code() = %q, want %q", se.Code(), "store_uninitialized")
	}
}

func TestWrapProviderNotSupported(t *testing.T) {
	inner := errors.New("unknown provider")
	wrapped := WrapProviderNotSupported(inner)
	if !errors.Is(wrapped, ErrProviderNotSupported) {
		t.Error("WrapProviderNotSupported: errors.Is(wrapped, ErrProviderNotSupported) is false")
	}
	var se SecretError
	if !errors.As(wrapped, &se) {
		t.Error("WrapProviderNotSupported: wrapped does not satisfy SecretError")
	}
	if se.Code() != "provider_not_supported" {
		t.Errorf("WrapProviderNotSupported: Code() = %q, want %q", se.Code(), "provider_not_supported")
	}
}

// ─────────────────────────────────────────────
// AsSecretError tests
// ─────────────────────────────────────────────

func TestAsSecretErrorExtended(t *testing.T) {
	// Wrapped error should be extractable.
	inner := errors.New("inner")
	wrapped := WrapSecretNotFound(inner)
	se := AsSecretError(wrapped)
	if se == nil {
		t.Fatal("AsSecretError(WrapSecretNotFound(...)) returned nil")
	}
	if se.Code() != "secret_not_found" {
		t.Errorf("Code() = %q, want %q", se.Code(), "secret_not_found")
	}

	// Plain error should return nil.
	plain := errors.New("plain error")
	if got := AsSecretError(plain); got != nil {
		t.Errorf("AsSecretError(plain error) = %v, want nil", got)
	}

	// Nil should return nil.
	if got := AsSecretError(nil); got != nil {
		t.Errorf("AsSecretError(nil) = %v, want nil", got)
	}
}

// ─────────────────────────────────────────────
// Sentinel error identity tests
// ─────────────────────────────────────────────

func TestErrSentinelCodes(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode string
	}{
		{"ErrSecretNotFound", ErrSecretNotFound, "secret_not_found"},
		{"ErrStoreCorrupted", ErrStoreCorrupted, "store_corrupted"},
		{"ErrStoreLocked", ErrStoreLocked, "store_locked"},
		{"ErrStoreUninitialized", ErrStoreUninitialized, "store_uninitialized"},
		{"ErrKeyExists", ErrKeyExists, "key_exists"},
		{"ErrRotateInProgress", ErrRotateInProgress, "rotate_in_progress"},
		{"ErrProviderNotSupported", ErrProviderNotSupported, "provider_not_supported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var se SecretError
			if !errors.As(tt.err, &se) {
				t.Fatalf("%T does not satisfy SecretError", tt.err)
			}
			if se.Code() != tt.wantCode {
				t.Errorf("Code() = %q, want %q", se.Code(), tt.wantCode)
			}
			if se.Message() == "" {
				t.Error("Message() is empty")
			}
		})
	}
}

func TestErrSentinelErrorString(t *testing.T) {
	// Verify that each sentinel renders as "code: message" without
	// extra noise.
	tests := []struct {
		name     string
		err      error
		wantPref string
	}{
		{"ErrSecretNotFound", ErrSecretNotFound, "secret_not_found: secret does not exist"},
		{"ErrStoreCorrupted", ErrStoreCorrupted, "store_corrupted: secret store is corrupted"},
		{"ErrStoreLocked", ErrStoreLocked, "store_locked: key file is missing"},
		{"ErrRotateInProgress", ErrRotateInProgress, "rotate_in_progress: rotate already in progress"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.err.Error()
			if len(s) < len(tt.wantPref) || s[:len(tt.wantPref)] != tt.wantPref {
				t.Errorf("Error() = %q, want prefix %q", s, tt.wantPref)
			}
		})
	}
}

// ─────────────────────────────────────────────
// Provider constant tests
// ─────────────────────────────────────────────

func TestProviderConstants(t *testing.T) {
	if ProviderSOPS != "sops" {
		t.Errorf("ProviderSOPS = %q, want %q", ProviderSOPS, "sops")
	}
	if ProviderEnv != "env" {
		t.Errorf("ProviderEnv = %q, want %q", ProviderEnv, "env")
	}
}
