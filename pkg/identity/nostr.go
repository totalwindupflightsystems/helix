// Package identity provides the Nostr compatibility bridge for Helix Identity
// Documents (SPEC-022: Portable Agent Identity).
package identity

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// NostrKindMetadata is the NIP-01 event kind for profile metadata.
const NostrKindMetadata = 0

// NostrEvent is the NIP-01 wire representation used by the HID bridge.
//
// Native Nostr keys and signatures use secp256k1 Schnorr. HIDs use Ed25519;
// this documented extension therefore emits the HID Ed25519 public key as hex
// and signs the canonical event payload with the HID Ed25519 private key.
type NostrEvent struct {
	PubKey    string     `json:"pubkey"`
	CreatedAt int64      `json:"created_at"`
	Kind      int        `json:"kind"`
	Tags      [][]string `json:"tags"`
	Content   string     `json:"content"`
	Sig       string     `json:"sig"`
}

// NostrMetadata is the JSON object encoded into a kind-0 event's Content.
type NostrMetadata struct {
	Name         string            `json:"name"`
	Fingerprint  string            `json:"fingerprint"`
	Capabilities []NostrCapability `json:"capabilities"`
	TrustScore   float64           `json:"trust_score"`
	ForgeHandles map[string]string `json:"forge_handles"`
}

// NostrCapability is the portable subset of an HID capability claim.
type NostrCapability struct {
	Domain   string `json:"domain"`
	Strength int    `json:"strength"`
}

// NewNostrEventFromHID converts an AgentIdentity into an unsigned Nostr kind-0
// metadata event. Call Sign to set created_at and sig before publishing it.
func NewNostrEventFromHID(id *AgentIdentity) (*NostrEvent, error) {
	if id == nil {
		return nil, NewConfigError("agent identity is nil", nil)
	}
	if len(id.PubKey) != ed25519.PublicKeySize {
		return nil, NewConfigError(
			fmt.Sprintf("invalid ed25519 public key size: got %d, want %d", len(id.PubKey), ed25519.PublicKeySize), nil)
	}

	capabilities := make([]NostrCapability, len(id.Capabilities))
	for i, claim := range id.Capabilities {
		capabilities[i] = NostrCapability{
			Domain:   claim.Domain,
			Strength: claim.Strength,
		}
	}

	forgeHandles := make(map[string]string, len(id.ForgeHandles))
	for forgeURL, handle := range id.ForgeHandles {
		forgeHandles[forgeURL] = handle.Username
	}

	metadata := NostrMetadata{
		Name:         id.ID,
		Fingerprint:  id.Fingerprint(),
		Capabilities: capabilities,
		TrustScore:   id.TrustScore.Score,
		ForgeHandles: forgeHandles,
	}
	content, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal Nostr metadata: %w", err)
	}

	return &NostrEvent{
		PubKey:  hex.EncodeToString(id.PubKey),
		Kind:    NostrKindMetadata,
		Tags:    make([][]string, 0),
		Content: string(content),
	}, nil
}

// Sign timestamps the event and signs its canonical serialization. The
// serialization is the JSON encoding of [pubkey, created_at, kind, tags,
// content], preserving the field order required by this HID extension.
func (e *NostrEvent) Sign(privKey ed25519.PrivateKey) error {
	if e == nil {
		return NewConfigError("Nostr event is nil", nil)
	}
	if len(privKey) != ed25519.PrivateKeySize {
		return NewConfigError(
			fmt.Sprintf("invalid ed25519 private key size: got %d, want %d", len(privKey), ed25519.PrivateKeySize), nil)
	}

	e.CreatedAt = time.Now().UTC().Unix()
	payload, err := e.canonicalPayload()
	if err != nil {
		return fmt.Errorf("serialize Nostr event for signing: %w", err)
	}
	e.Sig = hex.EncodeToString(ed25519.Sign(privKey, payload))
	return nil
}

// Verify checks the Ed25519 signature against the public key embedded in the
// event. Any mutation to a signed event field causes verification to fail.
func (e *NostrEvent) Verify() (bool, error) {
	if e == nil {
		return false, NewConfigError("Nostr event is nil", nil)
	}

	pubKey, err := hex.DecodeString(e.PubKey)
	if err != nil {
		return false, fmt.Errorf("decode Nostr event public key: %w", err)
	}
	if len(pubKey) != ed25519.PublicKeySize {
		return false, NewConfigError(
			fmt.Sprintf("invalid ed25519 public key size: got %d, want %d", len(pubKey), ed25519.PublicKeySize), nil)
	}

	sig, err := hex.DecodeString(e.Sig)
	if err != nil {
		return false, fmt.Errorf("decode Nostr event signature: %w", err)
	}
	if len(sig) != ed25519.SignatureSize {
		return false, fmt.Errorf("invalid signature size: got %d, want %d", len(sig), ed25519.SignatureSize)
	}

	payload, err := e.canonicalPayload()
	if err != nil {
		return false, fmt.Errorf("serialize Nostr event for verification: %w", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pubKey), payload, sig) {
		return false, fmt.Errorf("signature verification failed")
	}
	return true, nil
}

// Marshal returns the event's NIP-01 JSON representation.
func (e *NostrEvent) Marshal() ([]byte, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("marshal Nostr event: %w", err)
	}
	return data, nil
}

func (e *NostrEvent) canonicalPayload() ([]byte, error) {
	return json.Marshal([]any{e.PubKey, e.CreatedAt, e.Kind, e.Tags, e.Content})
}
