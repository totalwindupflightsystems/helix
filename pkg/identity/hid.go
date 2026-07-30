// Package identity — Helix Identity Document (HID) types and cryptographic
// operations (SPEC-022: Portable Agent Identity).
//
// Every Helix agent gets a self-signed JSON HID containing its public key,
// forge handles, capabilities, and trust scores. The HID is signed with the
// agent's Ed25519 private key and can be verified by any party without a
// centralized registry.
//
// This file defines AgentIdentity, HID, and all supporting types, plus the
// core Sign / Verify / Import / Export / Fingerprint operations.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Core HID types
// ---------------------------------------------------------------------------

// AgentIdentity is the core portable agent identity document (SPEC-022 §2).
// It contains the agent's cryptographic public key, forge handles, capability
// claims, trust snapshots, and any third-party signatures.
type AgentIdentity struct {
	ID           string                 `json:"agent_id"`
	PubKey       ed25519.PublicKey      `json:"pubkey"`
	ForgeHandles map[string]ForgeHandle `json:"forge_handles"`
	Capabilities []CapabilityClaim      `json:"capabilities"`
	TrustScore   TrustSnapshot          `json:"trust_score"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	Signatures   []Signature            `json:"signatures,omitempty"`
}

// HID is a signed Helix Identity Document — the AgentIdentity plus an
// Ed25519 signature over its canonical JSON representation (excluding the
// Signatures field so third-party attestations don't break verification).
type HID struct {
	Identity AgentIdentity `json:"identity"`
	SigBytes []byte        `json:"signature"`
}

// ForgeHandle links the agent to a specific forge instance with a
// cryptographic proof that the agent controls both the HID and the forge
// account.
type ForgeHandle struct {
	ForgeURL       string `json:"forge_url"`
	Username       string `json:"username"`
	ProofSignature []byte `json:"proof_signature"`
}

// CapabilityClaim asserts a domain proficiency with an evidence-backed
// strength score (1–10).
type CapabilityClaim struct {
	Domain   string `json:"domain"`
	Strength int    `json:"strength"` // 1–10
	Evidence string `json:"evidence,omitempty"`
}

// TrustSnapshot captures a portable trust score at a point in time.
type TrustSnapshot struct {
	Score     float64   `json:"score"`
	Timestamp time.Time `json:"timestamp"`
}

// Signature is a third-party attestation on this HID, identified by the
// signer's key ID.
type Signature struct {
	KeyID     string    `json:"key_id"`
	SigBytes  []byte    `json:"sig_bytes"`
	Timestamp time.Time `json:"timestamp"`
}

// ---------------------------------------------------------------------------
// Constructors
// ---------------------------------------------------------------------------

// NewAgentIdentity creates a new AgentIdentity with a fresh UUIDv7 and
// ED25519 keypair. The name is used as a display label; the agent_id is a
// UUIDv7 so identities are globally unique without a central registry.
//
// Returns the identity, the private key (caller must protect it), and any
// error from key or UUID generation.
func NewAgentIdentity(name string) (*AgentIdentity, ed25519.PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, NewInternalError("ed25519 key generation failed", err)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, nil, NewInternalError("uuid v7 generation failed", err)
	}

	now := time.Now().UTC()
	return &AgentIdentity{
		ID:           id.String(),
		PubKey:       pub,
		ForgeHandles: make(map[string]ForgeHandle),
		Capabilities: make([]CapabilityClaim, 0),
		TrustScore: TrustSnapshot{
			Score:     0.0,
			Timestamp: now,
		},
		CreatedAt:  now,
		UpdatedAt:  now,
		Signatures: make([]Signature, 0),
	}, priv, nil
}

// ---------------------------------------------------------------------------
// Sign / Verify
// ---------------------------------------------------------------------------

// signPayload returns the canonical JSON bytes used for signing and
// verification. It serializes the identity without the Signatures field so
// that adding third-party attestations doesn't invalidate the core signature.
func (id *AgentIdentity) signPayload() ([]byte, error) {
	// Marshal a copy without Signatures to keep the payload stable.
	stripped := *id
	stripped.Signatures = nil
	return json.Marshal(stripped)
}

// Sign produces a signed HID by signing the identity's canonical JSON with
// the provided ED25519 private key. The returned HID includes the full
// identity and the signature bytes. The identity's UpdatedAt is set to the
// current time before signing so the timestamp is included in the signature.
func (id *AgentIdentity) Sign(privKey ed25519.PrivateKey) (*HID, error) {
	if len(privKey) != ed25519.PrivateKeySize {
		return nil, NewConfigError(
			fmt.Sprintf("invalid ed25519 private key size: got %d, want %d",
				len(privKey), ed25519.PrivateKeySize), nil)
	}

	id.UpdatedAt = time.Now().UTC()

	payload, err := id.signPayload()
	if err != nil {
		return nil, NewInternalError("sign payload marshal failed", err)
	}

	sig := ed25519.Sign(privKey, payload)

	return &HID{
		Identity: *id,
		SigBytes: sig,
	}, nil
}

// Verify checks that the HID's signature is valid for its embedded identity
// using the identity's own public key. Returns (true, nil) on success or
// (false, <reason>) when verification fails.
func (id *AgentIdentity) Verify(hid *HID) (bool, error) {
	if hid == nil {
		return false, NewConfigError("hid is nil", nil)
	}

	if len(id.PubKey) != ed25519.PublicKeySize {
		return false, NewConfigError(
			fmt.Sprintf("invalid ed25519 public key size: got %d, want %d",
				len(id.PubKey), ed25519.PublicKeySize), nil)
	}

	if len(hid.SigBytes) != ed25519.SignatureSize {
		return false, fmt.Errorf("invalid signature size: got %d, want %d",
			len(hid.SigBytes), ed25519.SignatureSize)
	}

	payload, err := hid.Identity.signPayload()
	if err != nil {
		return false, NewInternalError("verify payload marshal failed", err)
	}

	valid := ed25519.Verify(id.PubKey, payload, hid.SigBytes)
	if !valid {
		return false, fmt.Errorf("signature verification failed")
	}

	return true, nil
}

// ---------------------------------------------------------------------------
// Fingerprint
// ---------------------------------------------------------------------------

// Fingerprint returns the hex-encoded SHA-256 hash of the agent's public key.
// This is the portable identifier that can be included in headers like
// Helix-Agent-ID to decouple agent identity from forge identity.
func (id *AgentIdentity) Fingerprint() string {
	sum := sha256.Sum256(id.PubKey)
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// Import / Export
// ---------------------------------------------------------------------------

// ImportHID reads a HID JSON file from disk and returns the parsed
// AgentIdentity. The file must be a valid HID document.
func ImportHID(path string) (*AgentIdentity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, NewConfigError(
			fmt.Sprintf("failed to read HID file %q", path), err)
	}

	var hid HID
	if err := json.Unmarshal(data, &hid); err != nil {
		return nil, NewConfigError(
			fmt.Sprintf("failed to parse HID JSON from %q", path), err)
	}

	return &hid.Identity, nil
}

// Export writes the AgentIdentity as a signed HID JSON document to the given
// path. The caller must provide the corresponding private key so the
// resulting file is a verifiable, self-signed identity document.
func (id *AgentIdentity) Export(path string, privKey ed25519.PrivateKey) error {
	hid, err := id.Sign(privKey)
	if err != nil {
		return fmt.Errorf("signing HID for export: %w", err)
	}

	data, err := json.MarshalIndent(hid, "", "  ")
	if err != nil {
		return NewInternalError("hid marshal failed", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return NewInternalError(
			fmt.Sprintf("failed to write HID to %q", path), err)
	}

	return nil
}
