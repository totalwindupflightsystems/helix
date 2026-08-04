package channel

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/totalwindupflightsystems/helix/pkg/identity"
	"github.com/totalwindupflightsystems/helix/pkg/memory"
)

// ---------------------------------------------------------------------------
// DuckBrain archival of closed channels (SPEC-024 §5 step 5 + §7)
// ---------------------------------------------------------------------------

// ArchiveResult reports the outcome of an archival pass.
type ArchiveResult struct {
	// Written is the number of entries newly persisted to the store.
	Written int
	// Skipped is the number of entries that already existed (idempotent
	// re-archive).
	Skipped int
}

// ArchiveChannel verifies every message's HIDProof and writes each as a
// signed-event MemoryEntry to the supplied DuckBrain-compatible store. This
// implements SPEC-024 §5 step 5 (ARCHIVE: "Channel closed → messages archived
// to DuckBrain as signed events") and §7 ("all channel messages archived as
// signed events for audit").
//
// The function fails closed: if ANY agent message fails HIDProof verification
// (unsigned, tampered, or wrong key) the entire archival is aborted and the
// offending message ID is named in the error. No partial writes are committed.
//
// Archival is idempotent: entries that already exist in the store (detected
// via memory.ErrAlreadyExists on a non-overwrite Write) are counted as Skipped
// rather than treated as errors. The result reports how many entries were
// newly written vs. skipped.
//
// An empty message list writes no entries and returns a zero result with no
// error — archiving an empty channel is a successful no-op.
//
// The identity is used to verify each message's signature. It must be the
// identity whose private key signed the messages; a different identity will
// fail on fingerprint mismatch.
func ArchiveChannel(
	ch *Channel,
	msgs []ChannelMessage,
	store memory.MemoryStore,
	id *identity.AgentIdentity,
) (ArchiveResult, error) {
	if ch == nil {
		return ArchiveResult{}, fmt.Errorf("channel: cannot archive nil channel")
	}
	if store == nil {
		return ArchiveResult{}, fmt.Errorf("channel: cannot archive to nil store")
	}
	if id == nil {
		return ArchiveResult{}, fmt.Errorf("channel: cannot archive with nil identity")
	}

	var result ArchiveResult

	for i := range msgs {
		msg := &msgs[i]

		// Fail closed: verify the HIDProof before persisting. An unsigned or
		// tampered message must not enter the audit trail.
		if err := VerifyMessage(msg, id); err != nil {
			return ArchiveResult{}, fmt.Errorf("channel: archival aborted — message %q failed verification: %w",
				msg.ID, err)
		}

		key := archiveKey(ch.ID, msg.ID)

		entry, err := archiveEntry(key, ch, msg)
		if err != nil {
			return ArchiveResult{}, fmt.Errorf("channel: archival aborted — entry for message %q: %w",
				msg.ID, err)
		}

		// Write with overwrite=false so re-archiving is idempotent.
		if err := store.Write(entry, false); err != nil {
			if errors.Is(err, memory.ErrAlreadyExists) {
				result.Skipped++
				continue
			}
			return ArchiveResult{}, fmt.Errorf("channel: archival aborted — write for message %q: %w",
				msg.ID, err)
		}

		result.Written++
	}

	return result, nil
}

// archiveKey returns the deterministic DuckBrain key for a channel message
// archive entry. The key path nests under platform/incidents because channel
// archives are time-stamped audit events, and "incidents" is the platform
// sub-namespace that passes memory.ValidateKey for this class of data.
func archiveKey(channelID, messageID string) string {
	return fmt.Sprintf("/helix/platform/incidents/channels/%s/messages/%s", channelID, messageID)
}

// archiveEntry builds the MemoryEntry for a single signed message. The
// embedding text is the full JSON serialization of the ChannelMessage
// (including HIDProof with sig bytes and fingerprint) so that an auditor can
// re-verify the signature from the archived event alone.
func archiveEntry(key string, ch *Channel, msg *ChannelMessage) (memory.MemoryEntry, error) {
	envelope, err := json.Marshal(msg)
	if err != nil {
		return memory.MemoryEntry{}, fmt.Errorf("channel: marshal message %q: %w", msg.ID, err)
	}

	return memory.MemoryEntry{
		Key:    key,
		Domain: memory.DomainMessage,
		Attributes: memory.Attributes{
			Decision: fmt.Sprintf("archived message %q from channel %q (%s)", msg.ID, ch.Name, ch.ID),
			Rationale: fmt.Sprintf(
				"Channel %q (type %s) archived message %q by %s (%s) — signed event for audit",
				ch.ID, ch.Type, msg.ID, msg.Author, msg.AuthorType,
			),
		},
		EmbeddingText: string(envelope),
	}, nil
}
