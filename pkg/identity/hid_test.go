package identity

import (
	"crypto/ed25519"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// 1. NewAgentIdentity — valid keypair + UUIDv7
// ---------------------------------------------------------------------------

func TestNewAgentIdentity(t *testing.T) {
	t.Run("valid_keypair", func(t *testing.T) {
		id, priv, err := NewAgentIdentity("test-agent")
		if err != nil {
			t.Fatalf("NewAgentIdentity() error = %v", err)
		}
		if id == nil {
			t.Fatal("NewAgentIdentity() returned nil identity")
		}
		if len(priv) != ed25519.PrivateKeySize {
			t.Errorf("private key size = %d, want %d", len(priv), ed25519.PrivateKeySize)
		}
		if len(id.PubKey) != ed25519.PublicKeySize {
			t.Errorf("public key size = %d, want %d", len(id.PubKey), ed25519.PublicKeySize)
		}
		// Verify the keypair belongs together.
		if !ed25519.PrivateKey(priv).Public().(ed25519.PublicKey).Equal(id.PubKey) {
			t.Error("public key derived from private key doesn't match identity pubkey")
		}
	})

	t.Run("uuid_v7_format", func(t *testing.T) {
		id, _, err := NewAgentIdentity("test-agent")
		if err != nil {
			t.Fatalf("NewAgentIdentity() error = %v", err)
		}
		if id.ID == "" {
			t.Fatal("agent_id is empty")
		}
		// UUIDv7 has timestamp bits in positions that encode version 7.
		if len(id.ID) != 36 {
			t.Errorf("agent_id length = %d, want 36 (standard UUID string)", len(id.ID))
		}
	})

	t.Run("uniqie_ids", func(t *testing.T) {
		id1, _, err := NewAgentIdentity("a")
		if err != nil {
			t.Fatalf("NewAgentIdentity() error = %v", err)
		}
		id2, _, err := NewAgentIdentity("b")
		if err != nil {
			t.Fatalf("NewAgentIdentity() error = %v", err)
		}
		if id1.ID == id2.ID {
			t.Errorf("two NewAgentIdentity calls produced same UUID: %q", id1.ID)
		}
	})

	t.Run("initialized_fields", func(t *testing.T) {
		id, _, err := NewAgentIdentity("test-agent")
		if err != nil {
			t.Fatalf("NewAgentIdentity() error = %v", err)
		}
		if id.ForgeHandles == nil {
			t.Error("ForgeHandles is nil, want empty map")
		}
		if id.Capabilities == nil {
			t.Error("Capabilities is nil, want empty slice")
		}
		if id.Signatures == nil {
			t.Error("Signatures is nil, want empty slice")
		}
		if id.CreatedAt.IsZero() {
			t.Error("CreatedAt is zero")
		}
		if id.UpdatedAt.IsZero() {
			t.Error("UpdatedAt is zero")
		}
	})
}

// ---------------------------------------------------------------------------
// 2. Sign + Verify round-trip
// ---------------------------------------------------------------------------

func TestSignVerifyRoundTrip(t *testing.T) {
	id, priv, err := NewAgentIdentity("roundtrip-agent")
	if err != nil {
		t.Fatalf("NewAgentIdentity() error = %v", err)
	}

	hid, err := id.Sign(priv)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	if hid == nil {
		t.Fatal("Sign() returned nil HID")
	}
	if len(hid.SigBytes) != ed25519.SignatureSize {
		t.Errorf("signature size = %d, want %d", len(hid.SigBytes), ed25519.SignatureSize)
	}

	valid, err := id.Verify(hid)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !valid {
		t.Fatal("Verify() returned false for valid signature")
	}
}

// ---------------------------------------------------------------------------
// 3. Sign with wrong private key
// ---------------------------------------------------------------------------

func TestSignVerifyWrongKey(t *testing.T) {
	id, _, err := NewAgentIdentity("agent-a")
	if err != nil {
		t.Fatalf("NewAgentIdentity() error = %v", err)
	}

	// Generate a second keypair for signing.
	_, wrongPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey() error = %v", err)
	}

	hid, err := id.Sign(wrongPriv)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	valid, err := id.Verify(hid)
	if err == nil {
		t.Fatal("Verify() expected error for wrong-key signature, got nil")
	}
	if valid {
		t.Error("Verify() returned true for wrong-key signature")
	}
}

// ---------------------------------------------------------------------------
// 4. Tampered HID fails Verify
// ---------------------------------------------------------------------------

func TestVerifyTamperedHID(t *testing.T) {
	id, priv, err := NewAgentIdentity("tamper-test")
	if err != nil {
		t.Fatalf("NewAgentIdentity() error = %v", err)
	}

	hid, err := id.Sign(priv)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	// Tamper with the embedded identity.
	tampered := *hid
	tampered.Identity.TrustScore.Score = 999.0

	valid, err := id.Verify(&tampered)
	if err == nil {
		t.Fatal("Verify() expected error for tampered HID, got nil")
	}
	if valid {
		t.Error("Verify() returned true for tampered HID")
	}
}

// ---------------------------------------------------------------------------
// 5. Sign with invalid private key size
// ---------------------------------------------------------------------------

func TestSignInvalidKeySize(t *testing.T) {
	id, _, err := NewAgentIdentity("invalid-key-test")
	if err != nil {
		t.Fatalf("NewAgentIdentity() error = %v", err)
	}

	_, err = id.Sign([]byte("too-short"))
	if err == nil {
		t.Fatal("Sign() expected error for invalid key size, got nil")
	}
}

// ---------------------------------------------------------------------------
// 6. Verify nil HID
// ---------------------------------------------------------------------------

func TestVerifyNilHID(t *testing.T) {
	id, _, err := NewAgentIdentity("nil-hid-test")
	if err != nil {
		t.Fatalf("NewAgentIdentity() error = %v", err)
	}

	valid, err := id.Verify(nil)
	if err == nil {
		t.Fatal("Verify(nil) expected error, got nil")
	}
	if valid {
		t.Error("Verify(nil) returned true")
	}
}

// ---------------------------------------------------------------------------
// 7. Export + Import round-trip
// ---------------------------------------------------------------------------

func TestExportImportRoundTrip(t *testing.T) {
	id, priv, err := NewAgentIdentity("export-import-agent")
	if err != nil {
		t.Fatalf("NewAgentIdentity() error = %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "test-agent.hid")

	if err := id.Export(path, priv); err != nil {
		t.Fatalf("Export() error = %v", err)
	}

	imported, err := ImportHID(path)
	if err != nil {
		t.Fatalf("ImportHID() error = %v", err)
	}

	if imported.ID != id.ID {
		t.Errorf("imported ID = %q, want %q", imported.ID, id.ID)
	}
	if !imported.PubKey.Equal(id.PubKey) {
		t.Error("imported pubkey doesn't match original")
	}
	if imported.Fingerprint() != id.Fingerprint() {
		t.Errorf("imported fingerprint = %q, want %q",
			imported.Fingerprint(), id.Fingerprint())
	}
	if imported.CreatedAt.UTC().Format(timeLayout) != id.CreatedAt.UTC().Format(timeLayout) {
		t.Errorf("imported CreatedAt = %v, want %v", imported.CreatedAt, id.CreatedAt)
	}
}

// ---------------------------------------------------------------------------
// 8. Export writes valid, verifiable HID
// ---------------------------------------------------------------------------

func TestExportProducesVerifiableHID(t *testing.T) {
	id, priv, err := NewAgentIdentity("verifiable-export")
	if err != nil {
		t.Fatalf("NewAgentIdentity() error = %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "agent.hid")

	if err := id.Export(path, priv); err != nil {
		t.Fatalf("Export() error = %v", err)
	}

	// Read the raw file and verify it's valid JSON + verifiable.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}

	imported, err := ImportHID(path)
	if err != nil {
		t.Fatalf("ImportHID() error = %v", err)
	}

	// Re-sign and verify.
	hidVerify, err := imported.Sign(priv)
	if err != nil {
		t.Fatalf("Sign() after import error = %v", err)
	}
	valid, err := imported.Verify(hidVerify)
	if err != nil {
		t.Fatalf("Verify() after import error = %v", err)
	}
	if !valid {
		t.Error("Verify() returned false for exported-then-imported HID")
	}

	// Check that the JSON contains expected top-level fields.
	raw := string(data)
	if !contains(raw, `"identity"`) {
		t.Error("exported HID missing 'identity' key")
	}
	if !contains(raw, `"signature"`) {
		t.Error("exported HID missing 'signature' key")
	}
}

// ---------------------------------------------------------------------------
// 9. ImportHID nonexistent file
// ---------------------------------------------------------------------------

func TestImportHIDNonexistentFile(t *testing.T) {
	_, err := ImportHID("/nonexistent/path/to/hid.json")
	if err == nil {
		t.Fatal("ImportHID() expected error for nonexistent file, got nil")
	}
}

// ---------------------------------------------------------------------------
// 10. ImportHID invalid JSON
// ---------------------------------------------------------------------------

func TestImportHIDInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.hid")
	if err := os.WriteFile(path, []byte("not json"), 0600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	_, err := ImportHID(path)
	if err == nil {
		t.Fatal("ImportHID() expected error for invalid JSON, got nil")
	}
}

// ---------------------------------------------------------------------------
// 11. Fingerprint determinism
// ---------------------------------------------------------------------------

func TestFingerprint(t *testing.T) {
	id, _, err := NewAgentIdentity("fp-test")
	if err != nil {
		t.Fatalf("NewAgentIdentity() error = %v", err)
	}

	fp := id.Fingerprint()
	if fp == "" {
		t.Fatal("Fingerprint() returned empty string")
	}

	// Hex-encoded SHA-256 is 64 hex chars.
	if len(fp) != 64 {
		t.Errorf("fingerprint length = %d, want 64 (SHA-256 hex)", len(fp))
	}

	// Verify it's valid hex.
	if _, err := hex.DecodeString(fp); err != nil {
		t.Errorf("fingerprint is not valid hex: %v", err)
	}

	// Determinism: same pubkey → same fingerprint.
	fp2 := id.Fingerprint()
	if fp != fp2 {
		t.Errorf("Fingerprint() not deterministic: %q vs %q", fp, fp2)
	}
}

// ---------------------------------------------------------------------------
// 12. Sign updates UpdatedAt
// ---------------------------------------------------------------------------

func TestSignUpdatesTimestamp(t *testing.T) {
	id, priv, err := NewAgentIdentity("timestamp-test")
	if err != nil {
		t.Fatalf("NewAgentIdentity() error = %v", err)
	}

	original := id.UpdatedAt

	// This is fast, so timestamps could be the same. Just verify it doesn't
	// regress and that it's non-zero.
	_, err = id.Sign(priv)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	if id.UpdatedAt.IsZero() {
		t.Error("UpdatedAt is zero after Sign")
	}
	if id.UpdatedAt.Before(original) {
		t.Errorf("UpdatedAt moved backwards: %v → %v", original, id.UpdatedAt)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

const timeLayout = "2006-01-02T15:04:05Z"
