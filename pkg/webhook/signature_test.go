// Tests for pkg/webhook/signature.go — covers HMAC-SHA256 verification,
// constant-time comparison, and the canonical "sha256=" prefix handling.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// computeSig is a tiny test helper that produces the canonical Forgejo
// signature for (payload, secret).
func computeSig(payload []byte, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// -----------------------------------------------------------------------------
// VerifySignature — happy paths
// -----------------------------------------------------------------------------

func TestVerifySignature_HappyPath(t *testing.T) {
	payload := []byte(`{"action":"opened"}`)
	secret := []byte("test-secret")
	sig := computeSig(payload, secret)
	if !VerifySignature(payload, sig, secret) {
		t.Errorf("VerifySignature returned false for valid signature")
	}
}

func TestVerifySignature_NoPrefixAccepted(t *testing.T) {
	// Some non-Forgejo Git hosts ship signatures without the "sha256="
	// prefix. We accept either form.
	payload := []byte(`{"action":"opened"}`)
	secret := []byte("test-secret")
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	sig := hex.EncodeToString(mac.Sum(nil)) // no prefix
	if !VerifySignature(payload, sig, secret) {
		t.Errorf("VerifySignature should accept hex-only signature")
	}
}

func TestVerifySignature_DifferentSecretsDontMatch(t *testing.T) {
	payload := []byte(`{"action":"opened"}`)
	sig := computeSig(payload, []byte("secret-A"))
	if VerifySignature(payload, sig, []byte("secret-B")) {
		t.Error("VerifySignature matched wrong secret")
	}
}

func TestVerifySignature_DifferentPayloadsDontMatch(t *testing.T) {
	secret := []byte("test-secret")
	sig := computeSig([]byte("payload-A"), secret)
	if VerifySignature([]byte("payload-B"), sig, secret) {
		t.Error("VerifySignature matched different payload")
	}
}

// -----------------------------------------------------------------------------
// VerifySignature — failure modes
// -----------------------------------------------------------------------------

func TestVerifySignature_EmptyHeader(t *testing.T) {
	if VerifySignature([]byte("payload"), "", []byte("secret")) {
		t.Error("empty signature header should fail")
	}
}

func TestVerifySignature_EmptySecret(t *testing.T) {
	// An empty secret should NEVER validate any signature.
	payload := []byte("payload")
	sig := computeSig(payload, []byte("actual-secret"))
	if VerifySignature(payload, sig, nil) {
		t.Error("empty secret should reject signature")
	}
	if VerifySignature(payload, sig, []byte{}) {
		t.Error("zero-length secret should reject signature")
	}
}

func TestVerifySignature_MalformedHex(t *testing.T) {
	// Non-hex characters in the signature should fail.
	if VerifySignature([]byte("payload"), "sha256=zzzz", []byte("secret")) {
		t.Error("malformed hex signature should fail")
	}
}

func TestVerifySignature_TruncatedSignature(t *testing.T) {
	// SHA256 produces 64 hex chars; a shorter signature should fail.
	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write([]byte("payload"))
	short := hex.EncodeToString(mac.Sum(nil))[:32] // half length
	if VerifySignature([]byte("payload"), short, []byte("secret")) {
		t.Error("truncated signature should fail")
	}
}

// -----------------------------------------------------------------------------
// verifySignatureError — explicit error variants
// -----------------------------------------------------------------------------

func TestVerifySignatureError_Missing(t *testing.T) {
	err := verifySignatureError([]byte("payload"), "", []byte("secret"))
	if err == nil {
		t.Fatal("expected error for missing signature")
	}
	if err != ErrMissingSignature {
		t.Errorf("expected ErrMissingSignature, got %v", err)
	}
}

func TestVerifySignatureError_Mismatch(t *testing.T) {
	err := verifySignatureError([]byte("payload"), "sha256=deadbeef", []byte("secret"))
	if err == nil {
		t.Fatal("expected error for mismatched signature")
	}
	if err != ErrSignatureMismatch {
		t.Errorf("expected ErrSignatureMismatch, got %v", err)
	}
}

func TestVerifySignatureError_OK(t *testing.T) {
	payload := []byte("payload")
	sig := computeSig(payload, []byte("secret"))
	err := verifySignatureError(payload, sig, []byte("secret"))
	if err != nil {
		t.Errorf("expected nil error for valid signature, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// StaticSecret
// -----------------------------------------------------------------------------

func TestStaticSecret_ReturnsSameSecret(t *testing.T) {
	want := []byte("the-secret")
	fn := StaticSecret(want)
	for i := 0; i < 3; i++ {
		got := fn()
		if string(got) != string(want) {
			t.Errorf("call %d: got %q, want %q", i, got, want)
		}
	}
}

func TestStaticSecret_EmptySecret(t *testing.T) {
	fn := StaticSecret(nil)
	if got := fn(); len(got) != 0 {
		t.Errorf("expected empty secret, got %d bytes", len(got))
	}
}
