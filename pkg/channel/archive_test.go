package channel

import (
	"crypto/ed25519"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/identity"
	"github.com/totalwindupflightsystems/helix/pkg/memory"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// signNMessages creates n signed ChannelMessages in the given channel for the
// supplied identity. Each message has a unique ID, content, and timestamp.
func signNMessages(t *testing.T, ch *Channel, id *identity.AgentIdentity, priv ed25519.PrivateKey, n int) []ChannelMessage {
	t.Helper()
	msgs := make([]ChannelMessage, n)
	for i := 0; i < n; i++ {
		m := NewChannelMessage(ch.ID, id.ID, AuthorAgent, MsgText, "message content "+itoa(i))
		m.Timestamp = fixedTimestamp.Add(time.Duration(i) * time.Second)
		if err := SignMessage(m, id, priv); err != nil {
			t.Fatalf("SignMessage %d: %v", i, err)
		}
		msgs[i] = *m
	}
	return msgs
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}

// ---------------------------------------------------------------------------
// N messages → N valid entries with correct key pattern / domain
// ---------------------------------------------------------------------------

func TestArchiveChannel_BasicArchive(t *testing.T) {
	id, priv := newTestIdentity(t, "agent-archive-basic")
	ch := NewChannel("test-basic", ChannelTask, []string{"a", "b"})
	store := memory.NewMemStore()

	msgs := signNMessages(t, ch, id, priv, 3)

	result, err := ArchiveChannel(ch, msgs, store, id)
	if err != nil {
		t.Fatalf("ArchiveChannel: %v", err)
	}

	if result.Written != 3 {
		t.Errorf("expected 3 written, got %d", result.Written)
	}
	if result.Skipped != 0 {
		t.Errorf("expected 0 skipped, got %d", result.Skipped)
	}

	// Verify each entry in the store has the correct key pattern, domain,
	// and a re-parseable signed event envelope.
	for i := range msgs {
		key := archiveKey(ch.ID, msgs[i].ID)
		entry, err := store.Read(key)
		if err != nil {
			t.Fatalf("store.Read(%q): %v", key, err)
		}

		if entry.Domain != memory.DomainMessage {
			t.Errorf("entry %d: expected domain %q, got %q", i, memory.DomainMessage, entry.Domain)
		}

		// Key must pass ValidateKey.
		if err := memory.ValidateKey(entry.Key); err != nil {
			t.Errorf("entry %d: key %q failed ValidateKey: %v", i, entry.Key, err)
		}

		// EmbeddingText must be a valid JSON serialization of ChannelMessage.
		var decoded ChannelMessage
		if err := json.Unmarshal([]byte(entry.EmbeddingText), &decoded); err != nil {
			t.Fatalf("entry %d: EmbeddingText is not valid ChannelMessage JSON: %v", i, err)
		}

		if decoded.ID != msgs[i].ID {
			t.Errorf("entry %d: expected message ID %q, got %q", i, msgs[i].ID, decoded.ID)
		}

		// The archived message must carry the HIDProof so audit can re-verify.
		if decoded.HIDProof == nil {
			t.Fatalf("entry %d: archived message has no HIDProof", i)
		}
		if len(decoded.HIDProof.SigBytes) == 0 {
			t.Errorf("entry %d: archived HIDProof has empty SigBytes", i)
		}
		if decoded.HIDProof.Fingerprint != id.Fingerprint() {
			t.Errorf("entry %d: fingerprint mismatch: got %q, want %q",
				i, decoded.HIDProof.Fingerprint, id.Fingerprint())
		}

		// The archived message must re-verify against the identity.
		if err := VerifyMessage(&decoded, id); err != nil {
			t.Errorf("entry %d: archived message failed re-verification: %v", i, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Tampered message fails, naming the ID
// ---------------------------------------------------------------------------

func TestArchiveChannel_TamperedMessageFails(t *testing.T) {
	id, priv := newTestIdentity(t, "agent-archive-tamper")
	ch := NewChannel("test-tamper", ChannelReview, []string{"a"})
	store := memory.NewMemStore()

	msgs := signNMessages(t, ch, id, priv, 2)

	// Tamper with the first message's content after signing.
	msgs[0].Content = "tampered after signing"

	_, err := ArchiveChannel(ch, msgs, store, id)
	if err == nil {
		t.Fatal("expected archival to fail on tampered message")
	}

	// Error must name the offending message ID.
	if !strings.Contains(err.Error(), msgs[0].ID) {
		t.Errorf("error should name message ID %q, got: %v", msgs[0].ID, err)
	}

	// Fail closed: nothing should have been written.
	entries, _ := store.Query(memory.MemoryQuery{})
	if len(entries) != 0 {
		t.Errorf("expected 0 entries written on failure, got %d", len(entries))
	}
}

// ---------------------------------------------------------------------------
// Unsigned message fails
// ---------------------------------------------------------------------------

func TestArchiveChannel_UnsignedMessageFails(t *testing.T) {
	id, _ := newTestIdentity(t, "agent-archive-unsigned")
	ch := NewChannel("test-unsigned", ChannelIncident, []string{"a"})
	store := memory.NewMemStore()

	// Create a message with no HIDProof.
	unsig := NewChannelMessage(ch.ID, id.ID, AuthorAgent, MsgText, "unsigned content")

	_, err := ArchiveChannel(ch, []ChannelMessage{*unsig}, store, id)
	if err == nil {
		t.Fatal("expected archival to fail on unsigned message")
	}

	if !strings.Contains(err.Error(), unsig.ID) {
		t.Errorf("error should name message ID %q, got: %v", unsig.ID, err)
	}
}

// ---------------------------------------------------------------------------
// Idempotent re-archive: skips existing entries
// ---------------------------------------------------------------------------

func TestArchiveChannel_IdempotentReArchive(t *testing.T) {
	id, priv := newTestIdentity(t, "agent-archive-idempotent")
	ch := NewChannel("test-idempotent", ChannelTask, []string{"a"})
	store := memory.NewMemStore()

	msgs := signNMessages(t, ch, id, priv, 3)

	// First archival: all written.
	result1, err := ArchiveChannel(ch, msgs, store, id)
	if err != nil {
		t.Fatalf("first ArchiveChannel: %v", err)
	}
	if result1.Written != 3 || result1.Skipped != 0 {
		t.Fatalf("first: expected 3 written / 0 skipped, got %d written / %d skipped",
			result1.Written, result1.Skipped)
	}

	// Second archival: all skipped.
	result2, err := ArchiveChannel(ch, msgs, store, id)
	if err != nil {
		t.Fatalf("second ArchiveChannel: %v", err)
	}
	if result2.Written != 0 || result2.Skipped != 3 {
		t.Fatalf("second: expected 0 written / 3 skipped, got %d written / %d skipped",
			result2.Written, result2.Skipped)
	}

	// Store should still have exactly 3 entries (no duplicates, no overwrites).
	entries, _ := store.Query(memory.MemoryQuery{})
	if len(entries) != 3 {
		t.Errorf("expected 3 entries in store, got %d", len(entries))
	}
}

// ---------------------------------------------------------------------------
// Empty channel: no-op success
// ---------------------------------------------------------------------------

func TestArchiveChannel_EmptyChannel(t *testing.T) {
	id, _ := newTestIdentity(t, "agent-archive-empty")
	ch := NewChannel("test-empty", ChannelTask, []string{"a"})
	store := memory.NewMemStore()

	result, err := ArchiveChannel(ch, nil, store, id)
	if err != nil {
		t.Fatalf("empty channel ArchiveChannel: %v", err)
	}
	if result.Written != 0 || result.Skipped != 0 {
		t.Errorf("expected 0 written / 0 skipped, got %d / %d", result.Written, result.Skipped)
	}

	// Also test empty slice.
	result2, err := ArchiveChannel(ch, []ChannelMessage{}, store, id)
	if err != nil {
		t.Fatalf("empty slice ArchiveChannel: %v", err)
	}
	if result2.Written != 0 || result2.Skipped != 0 {
		t.Errorf("expected 0/0 for empty slice, got %d / %d", result2.Written, result2.Skipped)
	}
}

// ---------------------------------------------------------------------------
// Nil arguments error
// ---------------------------------------------------------------------------

func TestArchiveChannel_NilChannel(t *testing.T) {
	id, _ := newTestIdentity(t, "agent-archive-nilch")
	store := memory.NewMemStore()
	_, err := ArchiveChannel(nil, nil, store, id)
	if err == nil {
		t.Fatal("expected error on nil channel")
	}
}

func TestArchiveChannel_NilStore(t *testing.T) {
	id, _ := newTestIdentity(t, "agent-archive-nilstore")
	ch := NewChannel("test-nil-store", ChannelTask, []string{"a"})
	_, err := ArchiveChannel(ch, nil, nil, id)
	if err == nil {
		t.Fatal("expected error on nil store")
	}
}

func TestArchiveChannel_NilIdentity(t *testing.T) {
	ch := NewChannel("test-nil-id", ChannelTask, []string{"a"})
	store := memory.NewMemStore()
	_, err := ArchiveChannel(ch, nil, store, nil)
	if err == nil {
		t.Fatal("expected error on nil identity")
	}
}

// ---------------------------------------------------------------------------
// Wrong-key detection: signed by one identity, archived with another
// ---------------------------------------------------------------------------

func TestArchiveChannel_WrongKeyFails(t *testing.T) {
	signer, priv := newTestIdentity(t, "agent-archive-signer")
	other, _ := newTestIdentity(t, "agent-archive-other")

	ch := NewChannel("test-wrong-key", ChannelDeliberation, []string{signer.ID, other.ID})
	store := memory.NewMemStore()

	msgs := signNMessages(t, ch, signer, priv, 1)

	// Archive with the wrong identity — should fail on fingerprint mismatch.
	_, err := ArchiveChannel(ch, msgs, store, other)
	if err == nil {
		t.Fatal("expected archival to fail when identity does not match signer")
	}

	// Error must name the message ID.
	if !strings.Contains(err.Error(), msgs[0].ID) {
		t.Errorf("error should name message ID %q, got: %v", msgs[0].ID, err)
	}
}
