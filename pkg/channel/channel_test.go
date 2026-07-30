package channel

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Channel lifecycle tests
// ---------------------------------------------------------------------------

func TestNewChannel(t *testing.T) {
	ch := NewChannel("auth-refactor", ChannelTask, []string{"agent-a", "agent-b"})

	if ch.ID == "" {
		t.Error("expected non-empty ID")
	}
	if ch.Name != "auth-refactor" {
		t.Errorf("expected Name %q, got %q", "auth-refactor", ch.Name)
	}
	if ch.Type != ChannelTask {
		t.Errorf("expected Type %q, got %q", ChannelTask, ch.Type)
	}
	if len(ch.Members) != 2 {
		t.Errorf("expected 2 members, got %d", len(ch.Members))
	}
	if ch.Status != ChannelStatusActive {
		t.Errorf("expected Status %q, got %q", ChannelStatusActive, ch.Status)
	}
	if ch.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
	if !ch.IsActive() {
		t.Error("expected IsActive() to be true")
	}
	if ch.IsArchived() {
		t.Error("expected IsArchived() to be false")
	}
}

func TestChannelTypeValid(t *testing.T) {
	if !ChannelTask.Valid() {
		t.Error("expected ChannelTask to be valid")
	}
	if !ChannelReview.Valid() {
		t.Error("expected ChannelReview to be valid")
	}
	if !ChannelDeliberation.Valid() {
		t.Error("expected ChannelDeliberation to be valid")
	}
	if !ChannelIncident.Valid() {
		t.Error("expected ChannelIncident to be valid")
	}
	if ChannelType("unknown").Valid() {
		t.Error("expected unknown channel type to be invalid")
	}
}

func TestMessageTypeValid(t *testing.T) {
	types := []MessageType{MsgText, MsgCodeReview, MsgEvidenceBundle, MsgTaskAssign, MsgTrustUpdate, MsgChimeraVerdict}
	for _, mt := range types {
		if !mt.Valid() {
			t.Errorf("expected %q to be valid", mt)
		}
	}
	if MessageType("unknown").Valid() {
		t.Error("expected unknown message type to be invalid")
	}
}

func TestChannelHasMember(t *testing.T) {
	ch := NewChannel("test", ChannelTask, []string{"alice", "bob"})
	if !ch.HasMember("alice") {
		t.Error("expected alice to be a member")
	}
	if !ch.HasMember("bob") {
		t.Error("expected bob to be a member")
	}
	if ch.HasMember("charlie") {
		t.Error("expected charlie NOT to be a member")
	}
}

func TestNewChannelMessage(t *testing.T) {
	msg := NewChannelMessage("ch-1", "agent-a", AuthorAgent, MsgText, "hello world")

	if msg.ID == "" {
		t.Error("expected non-empty message ID")
	}
	if msg.ChannelID != "ch-1" {
		t.Errorf("expected ChannelID %q, got %q", "ch-1", msg.ChannelID)
	}
	if msg.Author != "agent-a" {
		t.Errorf("expected Author %q, got %q", "agent-a", msg.Author)
	}
	if msg.AuthorType != AuthorAgent {
		t.Errorf("expected AuthorType %q, got %q", AuthorAgent, msg.AuthorType)
	}
	if msg.Timestamp.IsZero() {
		t.Error("expected non-zero Timestamp")
	}
	if msg.HIDProof != nil {
		t.Error("expected nil HIDProof by default")
	}
}

// ---------------------------------------------------------------------------
// ChannelStore tests
// ---------------------------------------------------------------------------

func TestMemChannelStore_CreateAndGet(t *testing.T) {
	store := NewMemChannelStore()
	ch := NewChannel("deliberation-1", ChannelDeliberation, []string{"agent-1", "agent-2"})

	if err := store.Create(ch); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.Get(ch.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil channel")
	}
	if got.ID != ch.ID || got.Name != ch.Name {
		t.Error("retrieved channel does not match")
	}

	// Mutate the returned copy should not affect the store.
	got.Name = "modified"
	got2, _ := store.Get(ch.ID)
	if got2.Name != "deliberation-1" {
		t.Error("store was mutated via returned copy")
	}
}

func TestMemChannelStore_CreateDuplicate(t *testing.T) {
	store := NewMemChannelStore()
	ch := NewChannel("test", ChannelTask, nil)
	if err := store.Create(ch); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if err := store.Create(ch); err == nil {
		t.Error("expected error on duplicate Create")
	}
}

func TestMemChannelStore_CreateNil(t *testing.T) {
	store := NewMemChannelStore()
	if err := store.Create(nil); err == nil {
		t.Error("expected error on nil Create")
	}
}

func TestMemChannelStore_GetNotFound(t *testing.T) {
	store := NewMemChannelStore()
	got, err := store.Get("nonexistent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Error("expected nil for missing channel")
	}
}

func TestMemChannelStore_List(t *testing.T) {
	store := NewMemChannelStore()
	store.Create(NewChannel("ch-1", ChannelTask, nil))
	store.Create(NewChannel("ch-2", ChannelReview, nil))
	store.Create(NewChannel("ch-3", ChannelIncident, nil))

	all, err := store.List(nil)
	if err != nil {
		t.Fatalf("List(nil): %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 channels, got %d", len(all))
	}

	statusActive := ChannelStatusActive
	active, err := store.List(&statusActive)
	if err != nil {
		t.Fatalf("List(active): %v", err)
	}
	if len(active) != 3 {
		t.Errorf("expected 3 active channels, got %d", len(active))
	}

	statusArchived := ChannelStatusArchived
	archived, err := store.List(&statusArchived)
	if err != nil {
		t.Fatalf("List(archived): %v", err)
	}
	if len(archived) != 0 {
		t.Errorf("expected 0 archived, got %d", len(archived))
	}
}

func TestMemChannelStore_Archive(t *testing.T) {
	store := NewMemChannelStore()
	ch := NewChannel("incident-1", ChannelIncident, []string{"agent-a"})
	store.Create(ch)

	if err := store.Archive(ch.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	got, _ := store.Get(ch.ID)
	if got.Status != ChannelStatusArchived {
		t.Errorf("expected archived, got %q", got.Status)
	}
	if got.IsActive() {
		t.Error("expected IsActive() to be false after archive")
	}
	if !got.IsArchived() {
		t.Error("expected IsArchived() to be true")
	}
}

func TestMemChannelStore_ArchiveTwiceFails(t *testing.T) {
	store := NewMemChannelStore()
	ch := NewChannel("test", ChannelTask, nil)
	store.Create(ch)
	store.Archive(ch.ID)
	if err := store.Archive(ch.ID); err == nil {
		t.Error("expected error on second Archive")
	}
}

func TestMemChannelStore_ArchiveNotFound(t *testing.T) {
	store := NewMemChannelStore()
	if err := store.Archive("nonexistent"); err == nil {
		t.Error("expected error on Archive of nonexistent channel")
	}
}

func TestMemChannelStore_AddMember(t *testing.T) {
	store := NewMemChannelStore()
	ch := NewChannel("test", ChannelTask, []string{"alice"})
	store.Create(ch)

	if err := store.AddMember(ch.ID, "bob"); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	got, _ := store.Get(ch.ID)
	if !got.HasMember("bob") {
		t.Error("expected bob to be a member")
	}
	if len(got.Members) != 2 {
		t.Errorf("expected 2 members, got %d", len(got.Members))
	}
}

func TestMemChannelStore_AddMemberDuplicate(t *testing.T) {
	store := NewMemChannelStore()
	ch := NewChannel("test", ChannelTask, []string{"alice"})
	store.Create(ch)

	if err := store.AddMember(ch.ID, "alice"); err == nil {
		t.Error("expected error on duplicate AddMember")
	}
}

func TestMemChannelStore_AddMemberArchived(t *testing.T) {
	store := NewMemChannelStore()
	ch := NewChannel("test", ChannelTask, []string{"alice"})
	store.Create(ch)
	store.Archive(ch.ID)

	if err := store.AddMember(ch.ID, "bob"); err == nil {
		t.Error("expected error on AddMember to archived channel")
	}
}

func TestMemChannelStore_RemoveMember(t *testing.T) {
	store := NewMemChannelStore()
	ch := NewChannel("test", ChannelTask, []string{"alice", "bob"})
	store.Create(ch)

	if err := store.RemoveMember(ch.ID, "alice"); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}

	got, _ := store.Get(ch.ID)
	if got.HasMember("alice") {
		t.Error("expected alice to be removed")
	}
	if !got.HasMember("bob") {
		t.Error("expected bob to still be a member")
	}
	if len(got.Members) != 1 {
		t.Errorf("expected 1 member, got %d", len(got.Members))
	}
}

func TestMemChannelStore_RemoveMemberNotFound(t *testing.T) {
	store := NewMemChannelStore()
	ch := NewChannel("test", ChannelTask, []string{"alice"})
	store.Create(ch)

	if err := store.RemoveMember(ch.ID, "charlie"); err == nil {
		t.Error("expected error on RemoveMember of missing member")
	}
}

// ---------------------------------------------------------------------------
// MessageStore tests
// ---------------------------------------------------------------------------

func TestMemMessageStore_SendAndGetByChannel(t *testing.T) {
	store := NewMemMessageStore()

	msg1 := NewChannelMessage("ch-1", "agent-a", AuthorAgent, MsgText, "hello")
	msg2 := NewChannelMessage("ch-1", "agent-b", AuthorAgent, MsgCodeReview, "LGTM")
	msg3 := NewChannelMessage("ch-2", "human-1", AuthorHuman, MsgText, "status?")

	if err := store.Send(msg1); err != nil {
		t.Fatalf("Send msg1: %v", err)
	}
	if err := store.Send(msg2); err != nil {
		t.Fatalf("Send msg2: %v", err)
	}
	if err := store.Send(msg3); err != nil {
		t.Fatalf("Send msg3: %v", err)
	}

	ch1Msgs, err := store.GetByChannel("ch-1")
	if err != nil {
		t.Fatalf("GetByChannel(ch-1): %v", err)
	}
	if len(ch1Msgs) != 2 {
		t.Errorf("expected 2 messages for ch-1, got %d", len(ch1Msgs))
	}
	if ch1Msgs[0].Author != "agent-a" {
		t.Errorf("expected first message from agent-a, got %s", ch1Msgs[0].Author)
	}

	ch2Msgs, err := store.GetByChannel("ch-2")
	if err != nil {
		t.Fatalf("GetByChannel(ch-2): %v", err)
	}
	if len(ch2Msgs) != 1 {
		t.Errorf("expected 1 message for ch-2, got %d", len(ch2Msgs))
	}
}

func TestMemMessageStore_GetByChannelEmpty(t *testing.T) {
	store := NewMemMessageStore()
	msgs, err := store.GetByChannel("nonexistent")
	if err != nil {
		t.Fatalf("GetByChannel: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected empty slice, got %d messages", len(msgs))
	}
}

func TestMemMessageStore_SendNil(t *testing.T) {
	store := NewMemMessageStore()
	if err := store.Send(nil); err == nil {
		t.Error("expected error on nil Send")
	}
}

func TestMemMessageStore_List(t *testing.T) {
	store := NewMemMessageStore()
	store.Send(NewChannelMessage("ch-1", "a", AuthorAgent, MsgText, "t1"))
	store.Send(NewChannelMessage("ch-1", "a", AuthorAgent, MsgCodeReview, "cr1"))
	store.Send(NewChannelMessage("ch-2", "b", AuthorAgent, MsgEvidenceBundle, "ev1"))

	all, err := store.List(nil)
	if err != nil {
		t.Fatalf("List(nil): %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 messages, got %d", len(all))
	}

	mt := MsgText
	textOnly, err := store.List(&mt)
	if err != nil {
		t.Fatalf("List(text): %v", err)
	}
	if len(textOnly) != 1 {
		t.Errorf("expected 1 text message, got %d", len(textOnly))
	}
	if textOnly[0].Type != MsgText {
		t.Errorf("expected text type, got %q", textOnly[0].Type)
	}
}

func TestMemMessageStore_ListEmpty(t *testing.T) {
	store := NewMemMessageStore()
	all, err := store.List(nil)
	if err != nil {
		t.Fatalf("List(nil): %v", err)
	}
	if len(all) != 0 {
		t.Errorf("expected empty list, got %d messages", len(all))
	}
}

// ---------------------------------------------------------------------------
// SSE broker tests
// ---------------------------------------------------------------------------

func TestSSEBroker_SubscribeAndPublish(t *testing.T) {
	broker := NewSSEBroker()
	ch := broker.Subscribe("ch-1", "client-1")

	msg := ChannelMessage{
		ID:        "msg-1",
		ChannelID: "ch-1",
		Author:    "agent-a",
		Content:   "hello via SSE",
		Timestamp: time.Now(),
	}

	go broker.Publish(msg)

	received := <-ch
	if received.Content != "hello via SSE" {
		t.Errorf("expected 'hello via SSE', got %q", received.Content)
	}
	if received.ID != "msg-1" {
		t.Errorf("expected msg-1, got %q", received.ID)
	}
}

func TestSSEBroker_MultipleSubscribers(t *testing.T) {
	broker := NewSSEBroker()
	ch1 := broker.Subscribe("ch-1", "client-1")
	ch2 := broker.Subscribe("ch-1", "client-2")

	msg := ChannelMessage{ID: "msg-1", ChannelID: "ch-1", Content: "broadcast", Timestamp: time.Now()}

	// Publish synchronously so both receivers get the message.
	broker.Publish(msg)

	// Drain ch1.
	select {
	case r1 := <-ch1:
		if r1.Content != "broadcast" {
			t.Errorf("client-1 got %q", r1.Content)
		}
	default:
		t.Error("client-1 did not receive message")
	}

	// Drain ch2.
	select {
	case r2 := <-ch2:
		if r2.Content != "broadcast" {
			t.Errorf("client-2 got %q", r2.Content)
		}
	default:
		t.Error("client-2 did not receive message")
	}
}

func TestSSEBroker_Unsubscribe(t *testing.T) {
	broker := NewSSEBroker()
	ch := broker.Subscribe("ch-1", "client-1")

	broker.Unsubscribe("ch-1", "client-1")

	// Channel should be closed — read should return zero value with ok==false.
	msg, ok := <-ch
	if ok {
		t.Errorf("expected channel to be closed, got message %+v", msg)
	}
}

func TestSSEBroker_UnsubscribeNoop(t *testing.T) {
	broker := NewSSEBroker()
	// Should not panic.
	broker.Unsubscribe("nonexistent", "client-1")
	broker.Subscribe("ch-1", "client-1")
	broker.Unsubscribe("ch-1", "nonexistent-client")
}

func TestSSEBroker_PublishNoSubscribers(t *testing.T) {
	broker := NewSSEBroker()
	msg := ChannelMessage{ID: "msg-1", ChannelID: "ch-1", Content: "no one listening", Timestamp: time.Now()}
	// Should not panic.
	broker.Publish(msg)
}

func TestSSEBroker_PublishWrongChannel(t *testing.T) {
	broker := NewSSEBroker()
	ch := broker.Subscribe("ch-1", "client-1")

	msg := ChannelMessage{ID: "msg-1", ChannelID: "ch-2", Content: "wrong channel", Timestamp: time.Now()}
	broker.Publish(msg)

	// ch-1 subscriber should not receive a ch-2 message.
	select {
	case <-ch:
		t.Error("should not have received message for wrong channel")
	default:
		// expected
	}
}

func TestSSEBroker_SubscriberCount(t *testing.T) {
	broker := NewSSEBroker()

	if n := broker.SubscriberCount("ch-1"); n != 0 {
		t.Errorf("expected 0 subscribers, got %d", n)
	}

	broker.Subscribe("ch-1", "client-1")
	broker.Subscribe("ch-1", "client-2")
	broker.Subscribe("ch-2", "client-3")

	if n := broker.SubscriberCount("ch-1"); n != 2 {
		t.Errorf("expected 2 subscribers for ch-1, got %d", n)
	}
	if n := broker.SubscriberCount("ch-2"); n != 1 {
		t.Errorf("expected 1 subscriber for ch-2, got %d", n)
	}

	broker.Unsubscribe("ch-1", "client-1")
	if n := broker.SubscriberCount("ch-1"); n != 1 {
		t.Errorf("expected 1 subscriber after unsubscribe, got %d", n)
	}
}

func TestSSEBroker_BufferDrop(t *testing.T) {
	broker := NewSSEBroker()
	// Subscribe and immediately fill the buffer without draining.
	ch := broker.Subscribe("ch-1", "client-1")

	for i := 0; i < 64+10; i++ {
		msg := ChannelMessage{
			ID:        "msg",
			ChannelID: "ch-1",
			Content:   "fill",
			Timestamp: time.Now(),
		}
		broker.Publish(msg)
	}

	// Drain what we can; we should get exactly 64 messages (buffer size)
	// and then the channel has no more pending.
	count := 0
drainLoop:
	for {
		select {
		case <-ch:
			count++
		default:
			break drainLoop
		}
	}
	// We filled the buffer (64) then the next 10 were dropped.
	if count != 64 {
		t.Errorf("expected exactly 64 buffered messages, got %d", count)
	}
}

func TestSSEBroker_ConcurrentPublish(t *testing.T) {
	broker := NewSSEBroker()
	ch := broker.Subscribe("ch-1", "client-1")

	const numPublishers = 10
	const msgsPerPublisher = 50
	done := make(chan struct{})

	for i := 0; i < numPublishers; i++ {
		go func() {
			for j := 0; j < msgsPerPublisher; j++ {
				broker.Publish(ChannelMessage{
					ID:        "msg",
					ChannelID: "ch-1",
					Content:   "concurrent",
					Timestamp: time.Now(),
				})
			}
			done <- struct{}{}
		}()
	}

	// Wait for all publishers.
	for i := 0; i < numPublishers; i++ {
		<-done
	}

	// Drain the channel.
	received := 0
drain:
	for {
		select {
		case <-ch:
			received++
		default:
			break drain
		}
	}
	if received < 64 || received > 500 {
		t.Errorf("unexpected received count: %d (buffer=64, total published=%d)", received, numPublishers*msgsPerPublisher)
	}
}

// ---------------------------------------------------------------------------
// Attachment tests
// ---------------------------------------------------------------------------

func TestAttachmentFields(t *testing.T) {
	a := Attachment{
		Name:        "diff.patch",
		ContentType: "text/x-patch",
		Data:        []byte("+added line\n-removed line\n"),
	}
	if a.Name != "diff.patch" {
		t.Errorf("expected Name %q, got %q", "diff.patch", a.Name)
	}
	if a.ContentType != "text/x-patch" {
		t.Errorf("expected ContentType %q, got %q", "text/x-patch", a.ContentType)
	}
	if len(a.Data) == 0 {
		t.Error("expected non-empty Data")
	}
}

func TestChannelMessageWithAttachment(t *testing.T) {
	msg := NewChannelMessage("ch-1", "agent-a", AuthorAgent, MsgEvidenceBundle, "see attached")
	msg.Attachments = []Attachment{
		{Name: "log.txt", ContentType: "text/plain", Data: []byte("error log")},
	}

	if len(msg.Attachments) != 1 {
		t.Errorf("expected 1 attachment, got %d", len(msg.Attachments))
	}
	if msg.Attachments[0].Name != "log.txt" {
		t.Errorf("expected attachment name %q, got %q", "log.txt", msg.Attachments[0].Name)
	}
}

// ---------------------------------------------------------------------------
// HIDSignature tests
// ---------------------------------------------------------------------------

func TestHIDSignatureFields(t *testing.T) {
	sig := HIDSignature{
		KeyID:       "key-1",
		SigBytes:    []byte{0x01, 0x02, 0x03},
		Fingerprint: "abc123",
	}
	if sig.KeyID != "key-1" {
		t.Errorf("expected key-1, got %q", sig.KeyID)
	}
	if len(sig.SigBytes) != 3 {
		t.Errorf("expected 3 sig bytes, got %d", len(sig.SigBytes))
	}
}

func TestChannelMessageWithHIDProof(t *testing.T) {
	msg := NewChannelMessage("ch-1", "agent-a", AuthorAgent, MsgText, "signed message")
	msg.HIDProof = &HIDSignature{
		KeyID:       "key-agent-a",
		Fingerprint: "def456",
	}

	if msg.HIDProof == nil {
		t.Fatal("expected non-nil HIDProof")
	}
	if msg.HIDProof.KeyID != "key-agent-a" {
		t.Errorf("expected key-agent-a, got %q", msg.HIDProof.KeyID)
	}
}

// ---------------------------------------------------------------------------
// AuthorType tests
// ---------------------------------------------------------------------------

func TestAuthorTypes(t *testing.T) {
	types := map[AuthorType]string{
		AuthorHuman:   "human",
		AuthorAgent:   "agent",
		AuthorChimera: "chimera",
	}
	for at, expected := range types {
		if string(at) != expected {
			t.Errorf("AuthorType %q string is %q, expected %q", at, string(at), expected)
		}
	}
}

// ---------------------------------------------------------------------------
// End-to-end: channel + message + SSE lifecycle
// ---------------------------------------------------------------------------

func TestE2E_ChannelMessageSSE(t *testing.T) {
	chStore := NewMemChannelStore()
	msgStore := NewMemMessageStore()
	broker := NewSSEBroker()

	// Create a channel.
	ch := NewChannel("e2e-test", ChannelTask, []string{"agent-1", "human-1"})
	if err := chStore.Create(ch); err != nil {
		t.Fatalf("Create channel: %v", err)
	}

	// Subscribe a client.
	clientCh := broker.Subscribe(ch.ID, "ws-1")

	// Send a message.
	msg := NewChannelMessage(ch.ID, "agent-1", AuthorAgent, MsgText, "integration test")
	if err := msgStore.Send(msg); err != nil {
		t.Fatalf("Send message: %v", err)
	}

	// Publish to SSE.
	broker.Publish(*msg)

	// Verify SSE delivery.
	select {
	case received := <-clientCh:
		if received.Content != "integration test" {
			t.Errorf("SSE got %q", received.Content)
		}
	default:
		t.Error("SSE did not receive message")
	}

	// Verify message persistence.
	msgs, _ := msgStore.GetByChannel(ch.ID)
	if len(msgs) != 1 {
		t.Errorf("expected 1 persisted message, got %d", len(msgs))
	}

	// Unsubscribe and verify channel closure.
	broker.Unsubscribe(ch.ID, "ws-1")
	_, ok := <-clientCh
	if ok {
		t.Error("expected closed channel after unsubscribe")
	}
}

// ---------------------------------------------------------------------------
// Channel status transition test
// ---------------------------------------------------------------------------

func TestChannelStatusTransitions(t *testing.T) {
	store := NewMemChannelStore()
	ch := NewChannel("transitions", ChannelTask, []string{"a"})
	store.Create(ch)

	// Start active.
	got, _ := store.Get(ch.ID)
	if got.Status != ChannelStatusActive {
		t.Errorf("expected active, got %q", got.Status)
	}

	// Archive.
	if err := store.Archive(ch.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	got, _ = store.Get(ch.ID)
	if got.Status != ChannelStatusArchived {
		t.Errorf("expected archived, got %q", got.Status)
	}

	// Cannot un-archive (by design).
	if err := store.Archive(ch.ID); err == nil {
		t.Error("expected error on double archive")
	}
}

// ---------------------------------------------------------------------------
// List with filter after archive
// ---------------------------------------------------------------------------

func TestMemChannelStore_ListAfterArchive(t *testing.T) {
	store := NewMemChannelStore()
	ch1 := NewChannel("keep", ChannelTask, nil)
	ch2 := NewChannel("archive-me", ChannelReview, nil)
	store.Create(ch1)
	store.Create(ch2)
	store.Archive(ch2.ID)

	statusActive := ChannelStatusActive
	active, _ := store.List(&statusActive)
	if len(active) != 1 {
		t.Errorf("expected 1 active, got %d", len(active))
	}
	if active[0].Name != "keep" {
		t.Errorf("expected 'keep', got %q", active[0].Name)
	}

	statusArchived := ChannelStatusArchived
	archived, _ := store.List(&statusArchived)
	if len(archived) != 1 {
		t.Errorf("expected 1 archived, got %d", len(archived))
	}
	if archived[0].Name != "archive-me" {
		t.Errorf("expected 'archive-me', got %q", archived[0].Name)
	}
}
