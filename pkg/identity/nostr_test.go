package identity

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"
)

func TestNostrKindZeroEventFromHID(t *testing.T) {
	id, priv, err := NewAgentIdentity("nostr-agent")
	if err != nil {
		t.Fatalf("NewAgentIdentity() error = %v", err)
	}
	id.Capabilities = []CapabilityClaim{
		{Domain: "go", Strength: 9, Evidence: "verified commits"},
		{Domain: "security", Strength: 7},
	}
	id.TrustScore = TrustSnapshot{Score: 0.91, Timestamp: time.Now().UTC()}
	id.ForgeHandles["https://forge.example"] = ForgeHandle{
		ForgeURL: "https://forge.example",
		Username: "nostr-agent",
	}

	event, err := NewNostrEventFromHID(id)
	if err != nil {
		t.Fatalf("NewNostrEventFromHID() error = %v", err)
	}
	if err := event.Sign(priv); err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	valid, err := event.Verify()
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !valid {
		t.Fatal("Verify() returned false for valid event")
	}

	if event.Kind != NostrKindMetadata {
		t.Errorf("Kind = %d, want %d", event.Kind, NostrKindMetadata)
	}
	if event.CreatedAt <= 0 {
		t.Errorf("CreatedAt = %d, want Unix timestamp", event.CreatedAt)
	}
	wantPubKey := hex.EncodeToString(id.PubKey)
	if event.PubKey != wantPubKey {
		t.Errorf("PubKey = %q, want %q", event.PubKey, wantPubKey)
	}
	if len(event.Sig) != ed25519.SignatureSize*2 {
		t.Errorf("signature hex length = %d, want %d", len(event.Sig), ed25519.SignatureSize*2)
	}

	var metadata NostrMetadata
	if err := json.Unmarshal([]byte(event.Content), &metadata); err != nil {
		t.Fatalf("event content is not valid metadata JSON: %v", err)
	}
	if metadata.Name != id.ID {
		t.Errorf("metadata name = %q, want %q", metadata.Name, id.ID)
	}
	if metadata.Fingerprint != id.Fingerprint() {
		t.Errorf("metadata fingerprint = %q, want %q", metadata.Fingerprint, id.Fingerprint())
	}
	if metadata.TrustScore != id.TrustScore.Score {
		t.Errorf("metadata trust score = %v, want %v", metadata.TrustScore, id.TrustScore.Score)
	}
	if len(metadata.Capabilities) != 2 {
		t.Fatalf("metadata capabilities length = %d, want 2", len(metadata.Capabilities))
	}
	if metadata.Capabilities[0].Domain != "go" || metadata.Capabilities[0].Strength != 9 {
		t.Errorf("first capability = %+v, want go strength 9", metadata.Capabilities[0])
	}
	if got := metadata.ForgeHandles["https://forge.example"]; got != "nostr-agent" {
		t.Errorf("forge handle = %q, want %q", got, "nostr-agent")
	}

	data, err := event.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("marshaled event is invalid JSON: %v", err)
	}
	for _, field := range []string{"pubkey", "created_at", "kind", "tags", "content", "sig"} {
		if _, ok := fields[field]; !ok {
			t.Errorf("marshaled event missing NIP-01 field %q", field)
		}
	}
}

func TestNostrEventTamperDetection(t *testing.T) {
	id, priv, err := NewAgentIdentity("tamper-agent")
	if err != nil {
		t.Fatalf("NewAgentIdentity() error = %v", err)
	}
	event, err := NewNostrEventFromHID(id)
	if err != nil {
		t.Fatalf("NewNostrEventFromHID() error = %v", err)
	}
	if err := event.Sign(priv); err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	event.Content = `{"name":"attacker"}`
	valid, err := event.Verify()
	if err == nil {
		t.Fatal("Verify() expected error for tampered content, got nil")
	}
	if valid {
		t.Error("Verify() returned true for tampered content")
	}
}

func TestNostrEventInvalidInputs(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "nil identity",
			run: func() error {
				_, err := NewNostrEventFromHID(nil)
				return err
			},
		},
		{
			name: "invalid public key",
			run: func() error {
				_, err := NewNostrEventFromHID(&AgentIdentity{PubKey: []byte("short")})
				return err
			},
		},
		{
			name: "invalid private key",
			run: func() error {
				event := &NostrEvent{}
				return event.Sign([]byte("short"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}
