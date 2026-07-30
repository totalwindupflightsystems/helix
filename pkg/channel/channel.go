// Package channel implements agent communication channels — named, persistent
// rooms where agents and humans converse in real time via SSE streaming.
//
// SPEC-024 defines channel types (task, review, deliberation, incident),
// message types, in-memory stores, and an SSE broker for push-based delivery.
// Every message carries an optional HID proof so events can be verified and
// archived to DuckBrain for audit.
//
// This file defines the core data model, store interfaces with in-memory
// implementations, and the SSE subscription broker. Follow-on files will add
// HID signing/verification (message.go), Chimera auto-trigger logic
// (deliberation.go), and DuckBrain archival (archive.go).
package channel

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Channel type enumeration
// ---------------------------------------------------------------------------

// ChannelType classifies the purpose and semantics of a channel.
type ChannelType string

const (
	// ChannelTask is for agent work coordination — splitting tasks,
	// negotiating scope, and synchronising parallel workstreams.
	ChannelTask ChannelType = "task"

	// ChannelReview is for pre-PR code discussion — agents discuss
	// their approach with peers or humans before committing.
	ChannelReview ChannelType = "review"

	// ChannelDeliberation is for Chimera-mediated debate — when two or
	// more agents disagree, Chimera joins the channel and posts a verdict.
	ChannelDeliberation ChannelType = "deliberation"

	// ChannelIncident is for post-merge firefighting — rollback
	// decisions, root-cause discussion, and incident handoff.
	ChannelIncident ChannelType = "incident"
)

// ValidChannelTypes is the set of recognised ChannelType values.
var ValidChannelTypes = map[ChannelType]bool{
	ChannelTask:         true,
	ChannelReview:       true,
	ChannelDeliberation: true,
	ChannelIncident:     true,
}

// Valid reports whether t is a recognised channel type.
func (t ChannelType) Valid() bool { return ValidChannelTypes[t] }

// ---------------------------------------------------------------------------
// Channel status enumeration
// ---------------------------------------------------------------------------

// ChannelStatus describes the lifecycle phase of a channel.
type ChannelStatus string

const (
	ChannelStatusActive   ChannelStatus = "active"
	ChannelStatusArchived ChannelStatus = "archived"
)

// ---------------------------------------------------------------------------
// Channel
// ---------------------------------------------------------------------------

// Channel represents a named, persistent communication room. Members are
// agent IDs or human usernames. Channels start in the active state and can
// be archived when no longer needed — archived channels reject new messages
// but remain readable for audit.
type Channel struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Type      ChannelType   `json:"type"`
	Members   []string      `json:"members"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
	Status    ChannelStatus `json:"status"`
}

// NewChannel creates a Channel with a UUIDv7 ID, the current timestamp,
// and status set to active.
func NewChannel(name string, ctype ChannelType, members []string) *Channel {
	now := time.Now().UTC()
	return &Channel{
		ID:        uuid.Must(uuid.NewV7()).String(),
		Name:      name,
		Type:      ctype,
		Members:   members,
		CreatedAt: now,
		UpdatedAt: now,
		Status:    ChannelStatusActive,
	}
}

// IsActive reports whether the channel is in the active state.
func (c *Channel) IsActive() bool { return c.Status == ChannelStatusActive }

// IsArchived reports whether the channel has been archived.
func (c *Channel) IsArchived() bool { return c.Status == ChannelStatusArchived }

// HasMember reports whether the given member is in the channel's member list.
func (c *Channel) HasMember(member string) bool {
	for _, m := range c.Members {
		if m == member {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Author type enumeration
// ---------------------------------------------------------------------------

// AuthorType identifies the kind of entity that authored a message.
type AuthorType string

const (
	AuthorHuman   AuthorType = "human"
	AuthorAgent   AuthorType = "agent"
	AuthorChimera AuthorType = "chimera"
)

// ---------------------------------------------------------------------------
// Message type enumeration
// ---------------------------------------------------------------------------

// MessageType classifies the semantic content of a channel message.
type MessageType string

const (
	// MsgText is a plain-text conversational message.
	MsgText MessageType = "text"

	// MsgCodeReview is a code review comment, inline suggestion,
	// or diff annotation.
	MsgCodeReview MessageType = "code_review"

	// MsgEvidenceBundle is a structured evidence payload (logs, metrics,
	// test output) attached to a message.
	MsgEvidenceBundle MessageType = "evidence"

	// MsgTaskAssign is a formal task assignment from one agent to another
	// within a coordination channel.
	MsgTaskAssign MessageType = "task_assign"

	// MsgTrustUpdate carries a trust-score change notification.
	MsgTrustUpdate MessageType = "trust_update"

	// MsgChimeraVerdict is a Chimera deliberation result posted into a
	// deliberation channel.
	MsgChimeraVerdict MessageType = "chimera_verdict"
)

// ValidMessageTypes is the set of recognised MessageType values.
var ValidMessageTypes = map[MessageType]bool{
	MsgText:           true,
	MsgCodeReview:     true,
	MsgEvidenceBundle: true,
	MsgTaskAssign:     true,
	MsgTrustUpdate:    true,
	MsgChimeraVerdict: true,
}

// Valid reports whether t is a recognised message type.
func (t MessageType) Valid() bool { return ValidMessageTypes[t] }

// ---------------------------------------------------------------------------
// Attachment
// ---------------------------------------------------------------------------

// Attachment is an arbitrary payload attached to a channel message —
// a code diff, an evidence bundle, a screenshot, or any other binary blob.
type Attachment struct {
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Data        []byte `json:"data"`
}

// ---------------------------------------------------------------------------
// HIDSignature
// ---------------------------------------------------------------------------

// HIDSignature is a lightweight Helix Identity proof attached to every
// agent-authored message. It carries the agent's public key fingerprint
// and an Ed25519 signature over the message payload so receivers can
// verify provenance without a central registry.
//
// This is a stub for the full HID chain in pkg/identity; it will be
// replaced with the real HID type once cross-package wiring is complete.
type HIDSignature struct {
	KeyID       string `json:"key_id"`
	SigBytes    []byte `json:"sig_bytes"`
	Fingerprint string `json:"fingerprint"`
}

// ---------------------------------------------------------------------------
// ChannelMessage
// ---------------------------------------------------------------------------

// ChannelMessage is a single message in a channel. Every message is
// uniquely identified, carries an author type and optional HID proof,
// and can include attachments. ChimeraTrace is populated for deliberation
// verdicts that carry full multi-model traces.
type ChannelMessage struct {
	ID           string        `json:"id"`
	ChannelID    string        `json:"channel_id"`
	Author       string        `json:"author"`
	AuthorType   AuthorType    `json:"author_type"`
	Type         MessageType   `json:"type"`
	Content      string        `json:"content"`
	Attachments  []Attachment  `json:"attachments,omitempty"`
	HIDProof     *HIDSignature `json:"hid_proof,omitempty"`
	ChimeraTrace any           `json:"chimera_trace,omitempty"`
	Timestamp    time.Time     `json:"timestamp"`
}

// NewChannelMessage creates a ChannelMessage with a UUIDv7 ID and the
// current UTC timestamp. Attachments and HIDProof default to nil/empty and
// should be populated by the caller before persistence.
func NewChannelMessage(channelID, author string, authorType AuthorType, msgType MessageType, content string) *ChannelMessage {
	return &ChannelMessage{
		ID:         uuid.Must(uuid.NewV7()).String(),
		ChannelID:  channelID,
		Author:     author,
		AuthorType: authorType,
		Type:       msgType,
		Content:    content,
		Timestamp:  time.Now().UTC(),
	}
}

// ---------------------------------------------------------------------------
// ChannelStore
// ---------------------------------------------------------------------------

// ChannelStore is the persistence interface for channels. An in-memory
// implementation is provided; a future DuckBrain-backed implementation
// will satisfy the same interface for audit-grade durability.
type ChannelStore interface {
	// Create persists a new channel. Returns an error if a channel with
	// the same ID already exists.
	Create(ch *Channel) error

	// Get retrieves a channel by ID. Returns nil if not found.
	Get(id string) (*Channel, error)

	// List returns all channels, optionally filtered by status.
	// A nil status filter returns every channel.
	List(status *ChannelStatus) ([]*Channel, error)

	// Archive marks a channel as archived. Returns an error if the
	// channel is already archived or does not exist.
	Archive(id string) error

	// AddMember adds a member to the channel's member list. Returns
	// an error if the channel is archived or the member is already
	// present.
	AddMember(channelID, member string) error

	// RemoveMember removes a member from the channel's member list.
	// Returns an error if the channel is archived or the member is
	// not present.
	RemoveMember(channelID, member string) error
}

// ---------------------------------------------------------------------------
// MessageStore
// ---------------------------------------------------------------------------

// MessageStore is the persistence interface for channel messages. An
// in-memory implementation is provided; a DuckBrain-backed implementation
// will provide signed-event archival.
type MessageStore interface {
	// Send persists a message. Returns an error if the target channel
	// does not exist or is archived.
	Send(msg *ChannelMessage) error

	// GetByChannel returns all messages in a channel, ordered by
	// timestamp ascending.
	GetByChannel(channelID string) ([]*ChannelMessage, error)

	// List returns all messages across all channels, optionally
	// filtered by message type. A nil type filter returns every message.
	List(msgType *MessageType) ([]*ChannelMessage, error)
}

// ---------------------------------------------------------------------------
// In-memory channel store
// ---------------------------------------------------------------------------

// MemChannelStore is a thread-safe, in-memory implementation of
// ChannelStore backed by a map.
type MemChannelStore struct {
	mu       sync.RWMutex
	channels map[string]*Channel
}

// NewMemChannelStore returns an initialised MemChannelStore.
func NewMemChannelStore() *MemChannelStore {
	return &MemChannelStore{channels: make(map[string]*Channel)}
}

func (s *MemChannelStore) Create(ch *Channel) error {
	if ch == nil {
		return fmt.Errorf("channel: cannot create nil channel")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.channels[ch.ID]; exists {
		return fmt.Errorf("channel: channel %q already exists", ch.ID)
	}
	cp := *ch
	s.channels[ch.ID] = &cp
	return nil
}

func (s *MemChannelStore) Get(id string) (*Channel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ch, ok := s.channels[id]
	if !ok {
		return nil, nil
	}
	cp := *ch
	return &cp, nil
}

func (s *MemChannelStore) List(status *ChannelStatus) ([]*Channel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Channel, 0, len(s.channels))
	for _, ch := range s.channels {
		if status == nil || ch.Status == *status {
			cp := *ch
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *MemChannelStore) Archive(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.channels[id]
	if !ok {
		return fmt.Errorf("channel: channel %q not found", id)
	}
	if ch.Status == ChannelStatusArchived {
		return fmt.Errorf("channel: channel %q is already archived", id)
	}
	ch.Status = ChannelStatusArchived
	ch.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *MemChannelStore) AddMember(channelID, member string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.channels[channelID]
	if !ok {
		return fmt.Errorf("channel: channel %q not found", channelID)
	}
	if ch.Status == ChannelStatusArchived {
		return fmt.Errorf("channel: cannot add member to archived channel %q", channelID)
	}
	for _, m := range ch.Members {
		if m == member {
			return fmt.Errorf("channel: member %q is already in channel %q", member, channelID)
		}
	}
	ch.Members = append(ch.Members, member)
	ch.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *MemChannelStore) RemoveMember(channelID, member string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.channels[channelID]
	if !ok {
		return fmt.Errorf("channel: channel %q not found", channelID)
	}
	if ch.Status == ChannelStatusArchived {
		return fmt.Errorf("channel: cannot remove member from archived channel %q", channelID)
	}
	for i, m := range ch.Members {
		if m == member {
			ch.Members = append(ch.Members[:i], ch.Members[i+1:]...)
			ch.UpdatedAt = time.Now().UTC()
			return nil
		}
	}
	return fmt.Errorf("channel: member %q not in channel %q", member, channelID)
}

// ---------------------------------------------------------------------------
// In-memory message store
// ---------------------------------------------------------------------------

// MemMessageStore is a thread-safe, in-memory implementation of
// MessageStore. Messages are stored per-channel in insertion order.
type MemMessageStore struct {
	mu       sync.RWMutex
	messages map[string][]*ChannelMessage // channelID -> ordered messages
}

// NewMemMessageStore returns an initialised MemMessageStore.
func NewMemMessageStore() *MemMessageStore {
	return &MemMessageStore{messages: make(map[string][]*ChannelMessage)}
}

func (s *MemMessageStore) Send(msg *ChannelMessage) error {
	if msg == nil {
		return fmt.Errorf("channel: cannot send nil message")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *msg
	s.messages[msg.ChannelID] = append(s.messages[msg.ChannelID], &cp)
	return nil
}

func (s *MemMessageStore) GetByChannel(channelID string) ([]*ChannelMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	msgs := s.messages[channelID]
	out := make([]*ChannelMessage, len(msgs))
	for i, m := range msgs {
		cp := *m
		out[i] = &cp
	}
	return out, nil
}

func (s *MemMessageStore) List(msgType *MessageType) ([]*ChannelMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*ChannelMessage
	for _, msgs := range s.messages {
		for _, m := range msgs {
			if msgType == nil || m.Type == *msgType {
				cp := *m
				out = append(out, &cp)
			}
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// SSE broker
// ---------------------------------------------------------------------------

// SSEBroker manages per-channel client subscriptions for server-sent
// events. It is safe for concurrent use.
//
// Each client is identified by a clientID string (typically a connection
// UUID). Subscribe returns a buffered channel that receives copies of
// every message published to the target channel. Unsubscribe removes the
// client and closes its channel. Publish broadcasts a message to every
// subscriber of a channel without blocking — if a subscriber's buffer is
// full the message is dropped for that client.
type SSEBroker struct {
	mu  sync.RWMutex
	subs map[string]map[string]chan ChannelMessage // channelID -> clientID -> chan
}

// NewSSEBroker returns an initialised SSEBroker.
func NewSSEBroker() *SSEBroker {
	return &SSEBroker{subs: make(map[string]map[string]chan ChannelMessage)}
}

// Subscribe registers a new client for the given channel and returns a
// buffered channel on which the client will receive published messages.
// The buffer size is an internal constant chosen to tolerate short bursts
// without back-pressure into publishers.
func (b *SSEBroker) Subscribe(channelID, clientID string) chan ChannelMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subs[channelID]; !ok {
		b.subs[channelID] = make(map[string]chan ChannelMessage)
	}
	ch := make(chan ChannelMessage, 64)
	b.subs[channelID][clientID] = ch
	return ch
}

// Unsubscribe removes a client subscription and closes the channel. It
// is a no-op if the client is not subscribed to the given channel.
func (b *SSEBroker) Unsubscribe(channelID, clientID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	clients, ok := b.subs[channelID]
	if !ok {
		return
	}
	ch, ok := clients[clientID]
	if !ok {
		return
	}
	delete(clients, clientID)
	if len(clients) == 0 {
		delete(b.subs, channelID)
	}
	close(ch)
}

// Publish broadcasts a copy of msg to every subscriber of the channel
// identified by msg.ChannelID. Delivery is non-blocking — if a
// subscriber's buffer is full the message is silently dropped for that
// client.
func (b *SSEBroker) Publish(msg ChannelMessage) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	clients, ok := b.subs[msg.ChannelID]
	if !ok {
		return
	}
	for _, ch := range clients {
		select {
		case ch <- msg:
		default:
			// drop — client buffer full
		}
	}
}

// SubscriberCount returns the number of active subscriptions for a given
// channel. This is useful for health checks and monitoring.
func (b *SSEBroker) SubscriberCount(channelID string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs[channelID])
}
