package channel

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"

	"github.com/totalwindupflightsystems/helix/pkg/identity"
)

// ---------------------------------------------------------------------------
// HID signing + verification for ChannelMessage (SPEC-024 §4 / §7)
// ---------------------------------------------------------------------------

// signedPayload is the deterministic subset of ChannelMessage fields that are
// covered by the Ed25519 signature. It excludes HIDProof and ChimeraTrace —
// the signature must not sign itself, and ChimeraTrace is an opaque verdict
// payload that may be added after the author has signed.
//
// Fields signed (in struct field order, which JSON preserves deterministically):
//   - ID           — message identity
//   - ChannelID    — which channel the message belongs to
//   - Author       — the author identifier
//   - AuthorType   — human / agent / chimera
//   - Type         — message semantic type
//   - Content      — the message body
//   - Attachments  — content-bearing payloads (diffs, evidence, screenshots)
//   - Timestamp    — when the message was created
//
// HIDProof and ChimeraTrace are deliberately excluded so that signing a
// message does not require a pre-existing proof, and so that Chimera can
// attach a deliberation trace after the original author has signed without
// invalidating the proof.
type signedPayload struct {
	ID          string       `json:"id"`
	ChannelID   string       `json:"channel_id"`
	Author      string       `json:"author"`
	AuthorType  AuthorType   `json:"author_type"`
	Type        MessageType  `json:"type"`
	Content     string       `json:"content"`
	Attachments []Attachment `json:"attachments,omitempty"`
	Timestamp   payloadTime  `json:"timestamp"`
}

// payloadTime wraps time.Time so that the canonical payload uses RFC 3339
// nano formatting with a fixed precision, guaranteeing deterministic
// serialization regardless of the original time.Time's internal monotonic
// clock reading or wall-clock precision.
type payloadTime struct {
	timeStr string
}

// canonicalPayload returns the deterministic JSON bytes that Sign and Verify
// both operate on. The same set of fields must be used on both sides for
// the round-trip to succeed.
func canonicalPayload(msg *ChannelMessage) ([]byte, error) {
	p := signedPayload{
		ID:          msg.ID,
		ChannelID:   msg.ChannelID,
		Author:      msg.Author,
		AuthorType:  msg.AuthorType,
		Type:        msg.Type,
		Content:     msg.Content,
		Attachments: msg.Attachments,
		Timestamp:   payloadTime{timeStr: msg.Timestamp.UTC().Format(timeRFC3339Nano)},
	}
	return json.Marshal(p)
}

// timeRFC3339Nano is the canonical timestamp format for signed payloads.
const timeRFC3339Nano = "2006-01-02T15:04:05.999999999Z07:00"

// MarshalJSON serializes payloadTime as a JSON string in RFC 3339 nano format.
func (pt payloadTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(pt.timeStr)
}

// SignMessage signs a ChannelMessage with the agent's Ed25519 private key and
// populates msg.HIDProof. The signature covers the canonical payload (see
// signedPayload docs). KeyID is set to the agent's ID, Fingerprint is set to
// id.Fingerprint(), and SigBytes holds the raw Ed25519 signature.
//
// This wires the stub HIDSignature (channel.go) to the real HID chain in
// pkg/identity (ID-001, SPEC-022/024). Unlike identity.AgentIdentity.Sign —
// which signs the full identity document — SignMessage signs only the message
// fields, using the agent's private key directly via ed25519.Sign.
func SignMessage(msg *ChannelMessage, id *identity.AgentIdentity, priv ed25519.PrivateKey) error {
	if msg == nil {
		return fmt.Errorf("channel: cannot sign nil message")
	}
	if id == nil {
		return fmt.Errorf("channel: cannot sign with nil identity")
	}
	if len(priv) != ed25519.PrivateKeySize {
		return fmt.Errorf("channel: invalid ed25519 private key size: got %d, want %d",
			len(priv), ed25519.PrivateKeySize)
	}

	payload, err := canonicalPayload(msg)
	if err != nil {
		return fmt.Errorf("channel: canonical payload marshal failed: %w", err)
	}

	sig := ed25519.Sign(priv, payload)

	msg.HIDProof = &HIDSignature{
		KeyID:       id.ID,
		SigBytes:    sig,
		Fingerprint: id.Fingerprint(),
	}

	return nil
}

// VerifyMessage verifies that msg.HIDProof is a valid Ed25519 signature over
// the canonical message payload, produced by the private key corresponding to
// id.PubKey. It also confirms that the proof's Fingerprint matches
// id.Fingerprint(), binding the signature to the claimed identity.
//
// Returns nil if the signature is valid, or an error describing the failure:
//   - nil message or identity → error
//   - nil HIDProof → "channel: message has no HID proof"
//   - fingerprint mismatch → error (proof is not bound to this identity)
//   - signature verification failure → error (tampered content or wrong key)
func VerifyMessage(msg *ChannelMessage, id *identity.AgentIdentity) error {
	if msg == nil {
		return fmt.Errorf("channel: cannot verify nil message")
	}
	if id == nil {
		return fmt.Errorf("channel: cannot verify with nil identity")
	}
	if msg.HIDProof == nil {
		return fmt.Errorf("channel: message %q has no HID proof", msg.ID)
	}
	if len(id.PubKey) != ed25519.PublicKeySize {
		return fmt.Errorf("channel: invalid ed25519 public key size: got %d, want %d",
			len(id.PubKey), ed25519.PublicKeySize)
	}
	if len(msg.HIDProof.SigBytes) != ed25519.SignatureSize {
		return fmt.Errorf("channel: invalid signature size: got %d, want %d",
			len(msg.HIDProof.SigBytes), ed25519.SignatureSize)
	}

	// Bind the proof to the claimed identity via fingerprint.
	if msg.HIDProof.Fingerprint != id.Fingerprint() {
		return fmt.Errorf("channel: fingerprint mismatch: proof has %q, identity has %q",
			msg.HIDProof.Fingerprint, id.Fingerprint())
	}

	payload, err := canonicalPayload(msg)
	if err != nil {
		return fmt.Errorf("channel: canonical payload marshal failed: %w", err)
	}

	if !ed25519.Verify(id.PubKey, payload, msg.HIDProof.SigBytes) {
		return fmt.Errorf("channel: signature verification failed for message %q", msg.ID)
	}

	return nil
}
