package channel

import (
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/identity"
)

// fixedTimestamp is a constant time used by tests that need deterministic
// payloads (so that signature differences are due to content, not timestamp).
var fixedTimestamp = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// newTestIdentity is a helper that generates a fresh agent identity + private
// key for tests. It fails the test immediately on any error so callers can
// use the returned values without nil checks.
func newTestIdentity(t *testing.T, name string) (*identity.AgentIdentity, ed25519.PrivateKey) {
	t.Helper()
	id, priv, err := identity.NewAgentIdentity(name)
	if err != nil {
		t.Fatalf("NewAgentIdentity(%q): %v", name, err)
	}
	return id, priv
}

// ---------------------------------------------------------------------------
// Sign → Verify round-trip
// ---------------------------------------------------------------------------

func TestSignMessage_VerifyRoundTrip(t *testing.T) {
	id, priv := newTestIdentity(t, "agent-alpha")

	msg := NewChannelMessage("ch-1", "agent-alpha", AuthorAgent, MsgText, "hello world")

	if err := SignMessage(msg, id, priv); err != nil {
		t.Fatalf("SignMessage: %v", err)
	}

	if msg.HIDProof == nil {
		t.Fatal("expected HIDProof to be populated after signing")
	}
	if len(msg.HIDProof.SigBytes) != ed25519.SignatureSize {
		t.Errorf("expected sig size %d, got %d", ed25519.SignatureSize, len(msg.HIDProof.SigBytes))
	}

	if err := VerifyMessage(msg, id); err != nil {
		t.Fatalf("VerifyMessage: %v", err)
	}
}

func TestSignMessage_PopulatesHIDProofFields(t *testing.T) {
	id, priv := newTestIdentity(t, "agent-beta")

	msg := NewChannelMessage("ch-2", "agent-beta", AuthorAgent, MsgText, "check fields")

	if err := SignMessage(msg, id, priv); err != nil {
		t.Fatalf("SignMessage: %v", err)
	}

	proof := msg.HIDProof
	if proof == nil {
		t.Fatal("expected non-nil HIDProof")
	}
	if proof.KeyID != id.ID {
		t.Errorf("expected KeyID %q, got %q", id.ID, proof.KeyID)
	}
	if proof.Fingerprint != id.Fingerprint() {
		t.Errorf("expected Fingerprint %q, got %q", id.Fingerprint(), proof.Fingerprint)
	}
	if len(proof.SigBytes) != ed25519.SignatureSize {
		t.Errorf("expected SigBytes len %d, got %d", ed25519.SignatureSize, len(proof.SigBytes))
	}
}

// ---------------------------------------------------------------------------
// Tamper detection
// ---------------------------------------------------------------------------

func TestVerifyMessage_TamperedContent(t *testing.T) {
	id, priv := newTestIdentity(t, "agent-gamma")

	msg := NewChannelMessage("ch-3", "agent-gamma", AuthorAgent, MsgText, "original content")
	if err := SignMessage(msg, id, priv); err != nil {
		t.Fatalf("SignMessage: %v", err)
	}

	// Tamper with content after signing.
	msg.Content = "tampered content"

	if err := VerifyMessage(msg, id); err == nil {
		t.Error("expected verification to fail on tampered content")
	}
}

func TestVerifyMessage_TamperedAuthor(t *testing.T) {
	id, priv := newTestIdentity(t, "agent-delta")

	msg := NewChannelMessage("ch-4", "agent-delta", AuthorAgent, MsgText, "check author")
	if err := SignMessage(msg, id, priv); err != nil {
		t.Fatalf("SignMessage: %v", err)
	}

	// Tamper with author after signing.
	msg.Author = "impostor"

	if err := VerifyMessage(msg, id); err == nil {
		t.Error("expected verification to fail on tampered author")
	}
}

func TestVerifyMessage_TamperedChannelID(t *testing.T) {
	id, priv := newTestIdentity(t, "agent-epsilon")

	msg := NewChannelMessage("ch-5", "agent-epsilon", AuthorAgent, MsgText, "check channel")
	if err := SignMessage(msg, id, priv); err != nil {
		t.Fatalf("SignMessage: %v", err)
	}

	// Tamper with channel ID after signing.
	msg.ChannelID = "wrong-channel"

	if err := VerifyMessage(msg, id); err == nil {
		t.Error("expected verification to fail on tampered channel ID")
	}
}

// ---------------------------------------------------------------------------
// Wrong-key detection
// ---------------------------------------------------------------------------

func TestVerifyMessage_WrongKey(t *testing.T) {
	signer, priv := newTestIdentity(t, "agent-real")
	verifier, _ := newTestIdentity(t, "agent-other")

	msg := NewChannelMessage("ch-6", "agent-real", AuthorAgent, MsgText, "signed by one, verified by another")
	if err := SignMessage(msg, signer, priv); err != nil {
		t.Fatalf("SignMessage: %v", err)
	}

	// Verify with a different identity's public key. This should fail on the
	// fingerprint mismatch (proof was created with signer's key, not verifier's).
	err := VerifyMessage(msg, verifier)
	if err == nil {
		t.Error("expected verification to fail with wrong key")
	}
}

// ---------------------------------------------------------------------------
// Fingerprint correctness
// ---------------------------------------------------------------------------

func TestSignMessage_FingerprintMatchesIdentity(t *testing.T) {
	id, priv := newTestIdentity(t, "agent-zeta")

	msg := NewChannelMessage("ch-7", "agent-zeta", AuthorAgent, MsgText, "fp check")
	if err := SignMessage(msg, id, priv); err != nil {
		t.Fatalf("SignMessage: %v", err)
	}

	if msg.HIDProof.Fingerprint != id.Fingerprint() {
		t.Errorf("expected fingerprint %q, got %q",
			id.Fingerprint(), msg.HIDProof.Fingerprint)
	}
}

// ---------------------------------------------------------------------------
// Nil-proof handling
// ---------------------------------------------------------------------------

func TestVerifyMessage_NilProof(t *testing.T) {
	id, _ := newTestIdentity(t, "agent-eta")

	msg := NewChannelMessage("ch-8", "agent-eta", AuthorAgent, MsgText, "no proof here")

	if msg.HIDProof != nil {
		t.Fatal("expected nil HIDProof before signing")
	}

	err := VerifyMessage(msg, id)
	if err == nil {
		t.Fatal("expected error when verifying message with nil HIDProof")
	}
}

// ---------------------------------------------------------------------------
// Nil-argument handling
// ---------------------------------------------------------------------------

func TestSignMessage_NilMessage(t *testing.T) {
	id, priv := newTestIdentity(t, "agent-theta")
	if err := SignMessage(nil, id, priv); err == nil {
		t.Error("expected error when signing nil message")
	}
}

func TestSignMessage_NilIdentity(t *testing.T) {
	_, priv := newTestIdentity(t, "agent-iota")
	msg := NewChannelMessage("ch-9", "agent-iota", AuthorAgent, MsgText, "nil id")
	if err := SignMessage(msg, nil, priv); err == nil {
		t.Error("expected error when signing with nil identity")
	}
}

func TestSignMessage_InvalidPrivateKey(t *testing.T) {
	id, _ := newTestIdentity(t, "agent-kappa")
	msg := NewChannelMessage("ch-10", "agent-kappa", AuthorAgent, MsgText, "bad key")
	if err := SignMessage(msg, id, ed25519.PrivateKey{}); err == nil {
		t.Error("expected error when signing with invalid private key")
	}
}

func TestVerifyMessage_NilMessage(t *testing.T) {
	id, _ := newTestIdentity(t, "agent-lambda")
	if err := VerifyMessage(nil, id); err == nil {
		t.Error("expected error when verifying nil message")
	}
}

func TestVerifyMessage_NilIdentity(t *testing.T) {
	msg := NewChannelMessage("ch-11", "agent-mu", AuthorAgent, MsgText, "nil verify id")
	if err := VerifyMessage(msg, nil); err == nil {
		t.Error("expected error when verifying with nil identity")
	}
}

// ---------------------------------------------------------------------------
// Attachments are signed (content-bearing)
// ---------------------------------------------------------------------------

func TestSignVerify_WithAttachments(t *testing.T) {
	id, priv := newTestIdentity(t, "agent-nu")

	msg := NewChannelMessage("ch-12", "agent-nu", AuthorAgent, MsgEvidenceBundle, "see evidence")
	msg.Attachments = []Attachment{
		{Name: "log.txt", ContentType: "text/plain", Data: []byte("error: something broke")},
		{Name: "diff.patch", ContentType: "text/x-diff", Data: []byte("+fixed")},
	}

	if err := SignMessage(msg, id, priv); err != nil {
		t.Fatalf("SignMessage: %v", err)
	}
	if err := VerifyMessage(msg, id); err != nil {
		t.Fatalf("VerifyMessage (valid): %v", err)
	}

	// Tamper with attachment data → verification must fail.
	msg.Attachments[0].Data = []byte("tampered")
	if err := VerifyMessage(msg, id); err == nil {
		t.Error("expected verification to fail after attachment tampering")
	}
}

// ---------------------------------------------------------------------------
// Re-signing produces a different signature (different timestamp or content)
// ---------------------------------------------------------------------------

func TestSignMessage_DifferentContentDifferentSignature(t *testing.T) {
	id, priv := newTestIdentity(t, "agent-xi")

	msg1 := NewChannelMessage("ch-13", "agent-xi", AuthorAgent, MsgText, "version 1")
	msg1.Timestamp = fixedTimestamp
	if err := SignMessage(msg1, id, priv); err != nil {
		t.Fatalf("SignMessage v1: %v", err)
	}
	sig1 := make([]byte, len(msg1.HIDProof.SigBytes))
	copy(sig1, msg1.HIDProof.SigBytes)

	msg2 := NewChannelMessage("ch-13", "agent-xi", AuthorAgent, MsgText, "version 2")
	msg2.Timestamp = fixedTimestamp
	if err := SignMessage(msg2, id, priv); err != nil {
		t.Fatalf("SignMessage v2: %v", err)
	}
	sig2 := msg2.HIDProof.SigBytes

	// The two signatures must differ (different content → different signature).
	same := true
	for i := range sig1 {
		if sig1[i] != sig2[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("expected different signatures for different content")
	}
}
