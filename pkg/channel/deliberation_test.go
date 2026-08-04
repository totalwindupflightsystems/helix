// Package channel — deliberation_test.go
//
// Tests for Chimera auto-trigger orchestration (SPEC-024 §5 step 4 / §6).
//
// Coverage matrix (from the CH-003 task spec):
//  1. no trigger: single agent author, or message count below threshold,
//     or disagreement score ≤ 0.3
//  2. trigger fires: 2+ agents + disagreement → verdict message posted
//     with correct fields
//  3. non-deliberation channel type never triggers
//  4. trigger-loop guard: second call with same messages does not post
//     a second verdict
//  5. FAIL verdict invokes handler; PASS-with-conditions passes conditions
//  6. Chimera HTTP error (500 / timeout / malformed JSON) → error
//     returned, no verdict posted
//  7. disagreement scorer default heuristic unit tests
package channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newDeliberationChannel builds a deliberation channel with the given
// member list and stores it in a fresh MemChannelStore.
func newDeliberationChannel(t *testing.T, members ...string) (*Channel, *MemChannelStore) {
	t.Helper()
	cs := NewMemChannelStore()
	ch := NewChannel("delib-"+t.Name(), ChannelDeliberation, members)
	if err := cs.Create(ch); err != nil {
		t.Fatalf("create deliberation channel: %v", err)
	}
	return ch, cs
}

// sendMessage persists a message through a fresh MemMessageStore and
// returns it. The store is created per-call because each test owns its own
// message history.
func sendMessage(t *testing.T, ch *Channel, author string, authorType AuthorType, msgType MessageType, content string) (*ChannelMessage, *MemMessageStore) {
	t.Helper()
	ms := NewMemMessageStore()
	msg := NewChannelMessage(ch.ID, author, authorType, msgType, content)
	if err := ms.Send(msg); err != nil {
		t.Fatalf("send message: %v", err)
	}
	return msg, ms
}

// allMessages returns the full message list for a channel from a store.
func allMessages(t *testing.T, store MessageStore, channelID string) []*ChannelMessage {
	t.Helper()
	msgs, err := store.GetByChannel(channelID)
	if err != nil {
		t.Fatalf("GetByChannel: %v", err)
	}
	return msgs
}

// disagreeingMessages returns a deterministic 3-message sequence from two
// agents with disagreement-language content that crosses the 0.3 threshold.
func disagreeingMessages(ch *Channel, store MessageStore) []*ChannelMessage {
	store.Send(NewChannelMessage(ch.ID, "agent-a", AuthorAgent, MsgText, "I disagree with the proposed approach."))
	store.Send(NewChannelMessage(ch.ID, "agent-b", AuthorAgent, MsgText, "I oppose — this approach is wrong and we cannot accept it."))
	store.Send(NewChannelMessage(ch.ID, "agent-a", AuthorAgent, MsgText, "Blocking this PR until we reconsider the design."))
	msgs, _ := store.GetByChannel(ch.ID)
	return msgs
}

// fakeChimeraServer returns an httptest.Server that records every prompt it
// receives and replies with the given body. Pass responseStatus to simulate
// non-2xx responses; leave responseBody empty and statusCode 0 to send no
// body at all.
type fakeChimeraServer struct {
	*httptest.Server
	mu         sync.Mutex
	prompts    []string
	formations []string
	calls      int
}

func newFakeChimera(t *testing.T, responseStatus int, responseBody string) *fakeChimeraServer {
	t.Helper()
	fc := &fakeChimeraServer{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fc.mu.Lock()
		fc.calls++
		var req map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&req)
		}
		if p, ok := req["prompt"].(string); ok {
			fc.prompts = append(fc.prompts, p)
		}
		if f, ok := req["formation"].(string); ok {
			fc.formations = append(fc.formations, f)
		}
		fc.mu.Unlock()

		if responseStatus != 0 {
			w.WriteHeader(responseStatus)
			if responseBody != "" {
				_, _ = w.Write([]byte(responseBody))
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responseBody))
	})
	fc.Server = httptest.NewServer(handler)
	t.Cleanup(fc.Close)
	return fc
}

// recordingHandler is a VerdictHandler that captures every verdict it sees
// so tests can assert handler invocation.
type recordingHandler struct {
	mu      sync.Mutex
	calls   []recordedVerdict
	failErr error
}

type recordedVerdict struct {
	ChannelID string
	Verdict   Verdict
}

func (h *recordingHandler) OnVerdict(_ context.Context, channelID string, v Verdict) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, recordedVerdict{ChannelID: channelID, Verdict: v})
	return h.failErr
}

// chimeraVerdictJSON returns the wire payload Chimera would send for a
// verdict with a structured header.
func chimeraVerdictJSON(result, formation string, trace any) string {
	traceJSON, _ := json.Marshal(trace)
	return fmt.Sprintf(`{"result":%q,"trace":%s,"formation":%q}`, result, string(traceJSON), formation)
}

// ---------------------------------------------------------------------------
// Scenario 1 — no trigger conditions
// ---------------------------------------------------------------------------

func TestDeliberator_NoTrigger_SingleAgentAuthor(t *testing.T) {
	ch, _ := newDeliberationChannel(t, "agent-a")
	store := NewMemMessageStore()
	// Three messages, all from the same agent — even with disagreement
	// language, the scorer requires 2+ distinct agent authors.
	store.Send(NewChannelMessage(ch.ID, "agent-a", AuthorAgent, MsgText, "I disagree with myself."))
	store.Send(NewChannelMessage(ch.ID, "agent-a", AuthorAgent, MsgText, "Actually I oppose it."))
	store.Send(NewChannelMessage(ch.ID, "agent-a", AuthorAgent, MsgText, "Wrong approach."))
	msgs := allMessages(t, store, ch.ID)

	delib := NewDeliberator(DeliberatorOpts{ChimeraBaseURL: "http://localhost:0"})
	_, err := delib.DeliberateIfNeeded(context.Background(), ch, store, msgs, "")
	if !errors.Is(err, ErrNotTriggered) {
		t.Fatalf("expected ErrNotTriggered for single agent, got %v", err)
	}
}

func TestDeliberator_NoTrigger_BelowMessageCount(t *testing.T) {
	// Default threshold is 2 — we must have MORE than 2 messages.
	ch, _ := newDeliberationChannel(t, "agent-a", "agent-b")
	store := NewMemMessageStore()
	store.Send(NewChannelMessage(ch.ID, "agent-a", AuthorAgent, MsgText, "I disagree."))
	store.Send(NewChannelMessage(ch.ID, "agent-b", AuthorAgent, MsgText, "I oppose."))
	msgs := allMessages(t, store, ch.ID)

	// Bump threshold above the message count.
	delib := NewDeliberator(DeliberatorOpts{
		MessageCountThreshold: 5,
		ChimeraBaseURL:        "http://localhost:0",
	})
	_, err := delib.DeliberateIfNeeded(context.Background(), ch, store, msgs, "")
	if !errors.Is(err, ErrNotTriggered) {
		t.Fatalf("expected ErrNotTriggered when below threshold, got %v", err)
	}
}

func TestDeliberator_NoTrigger_DisagreementBelowThreshold(t *testing.T) {
	ch, _ := newDeliberationChannel(t, "agent-a", "agent-b")
	store := NewMemMessageStore()
	// Polite, agreeable messages — disagreement score stays at 0.
	store.Send(NewChannelMessage(ch.ID, "agent-a", AuthorAgent, MsgText, "Sounds good."))
	store.Send(NewChannelMessage(ch.ID, "agent-b", AuthorAgent, MsgText, "Agreed, let's ship it."))
	store.Send(NewChannelMessage(ch.ID, "agent-a", AuthorAgent, MsgText, "+1 from me."))
	msgs := allMessages(t, store, ch.ID)

	delib := NewDeliberator(DeliberatorOpts{ChimeraBaseURL: "http://localhost:0"})
	_, err := delib.DeliberateIfNeeded(context.Background(), ch, store, msgs, "")
	if !errors.Is(err, ErrNotTriggered) {
		t.Fatalf("expected ErrNotTriggered for agreeable messages, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Scenario 2 — trigger fires; verdict message has correct fields
// ---------------------------------------------------------------------------

func TestDeliberator_TriggerFires_PostsVerdictMessage(t *testing.T) {
	fc := newFakeChimera(t, http.StatusOK, chimeraVerdictJSON(
		"VERDICT: PASS\nSummary: The proposal is sound.\n",
		"balanced",
		[]string{"claude:ok", "gpt:ok", "kimi:ok"},
	))

	ch, _ := newDeliberationChannel(t, "agent-a", "agent-b")
	store := NewMemMessageStore()
	msgs := disagreeingMessages(ch, store)

	handler := &recordingHandler{}
	delib := NewDeliberator(DeliberatorOpts{
		Client:  NewHTTPChimeraClient(fc.URL, "", 5*time.Second),
		Handler: handler,
	})

	verdict, err := delib.DeliberateIfNeeded(context.Background(), ch, store, msgs, "file.go:\nfunc Foo() {}")
	if err != nil {
		t.Fatalf("DeliberateIfNeeded: %v", err)
	}
	if verdict.Outcome != VerdictPass {
		t.Errorf("expected VerdictPass, got %q", verdict.Outcome)
	}

	posted := allMessages(t, store, ch.ID)
	if len(posted) != 4 {
		t.Fatalf("expected 4 messages after trigger (3 inputs + verdict), got %d", len(posted))
	}
	last := posted[len(posted)-1]
	if last.Type != MsgChimeraVerdict {
		t.Errorf("expected last message Type %q, got %q", MsgChimeraVerdict, last.Type)
	}
	if last.Author != ChimeraAuthorName {
		t.Errorf("expected Author %q, got %q", ChimeraAuthorName, last.Author)
	}
	if last.AuthorType != AuthorChimera {
		t.Errorf("expected AuthorType %q, got %q", AuthorChimera, last.AuthorType)
	}
	if last.Content != "The proposal is sound." {
		t.Errorf("expected Content to mirror verdict Summary, got %q", last.Content)
	}
	if last.ChimeraTrace == nil {
		t.Error("expected ChimeraTrace to be populated")
	}

	// Handler must have been invoked exactly once with the same verdict.
	if len(handler.calls) != 1 {
		t.Fatalf("expected handler called once, got %d", len(handler.calls))
	}
	if handler.calls[0].ChannelID != ch.ID {
		t.Errorf("expected handler channelID %q, got %q", ch.ID, handler.calls[0].ChannelID)
	}
	if handler.calls[0].Verdict.Outcome != VerdictPass {
		t.Errorf("expected handler verdict outcome pass, got %q", handler.calls[0].Verdict.Outcome)
	}

	// Prompt must include the channel name and code context.
	if fc.calls != 1 {
		t.Errorf("expected 1 chimera call, got %d", fc.calls)
	}
	if !strings.Contains(fc.prompts[0], ch.Name) {
		t.Errorf("expected prompt to contain channel name %q, got %q", ch.Name, fc.prompts[0])
	}
	if !strings.Contains(fc.prompts[0], "file.go:") {
		t.Errorf("expected prompt to include code context, got %q", fc.prompts[0])
	}
	if fc.formations[0] != "balanced" {
		t.Errorf("expected default formation %q, got %q", "balanced", fc.formations[0])
	}
}

// ---------------------------------------------------------------------------
// Scenario 3 — non-deliberation channel types never trigger
// ---------------------------------------------------------------------------

func TestDeliberator_NoTrigger_NonDeliberationChannel(t *testing.T) {
	for _, ctype := range []ChannelType{ChannelTask, ChannelReview, ChannelIncident} {
		t.Run(string(ctype), func(t *testing.T) {
			cs := NewMemChannelStore()
			ch := NewChannel("non-delib", ctype, []string{"agent-a", "agent-b"})
			if err := cs.Create(ch); err != nil {
				t.Fatalf("create: %v", err)
			}

			store := NewMemMessageStore()
			store.Send(NewChannelMessage(ch.ID, "agent-a", AuthorAgent, MsgText, "I disagree."))
			store.Send(NewChannelMessage(ch.ID, "agent-b", AuthorAgent, MsgText, "I oppose."))
			store.Send(NewChannelMessage(ch.ID, "agent-a", AuthorAgent, MsgText, "Wrong."))
			msgs := allMessages(t, store, ch.ID)

			delib := NewDeliberator(DeliberatorOpts{ChimeraBaseURL: "http://localhost:0"})
			_, err := delib.DeliberateIfNeeded(context.Background(), ch, store, msgs, "")
			if !errors.Is(err, ErrNotTriggered) {
				t.Fatalf("expected ErrNotTriggered for %s channel, got %v", ctype, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Scenario 4 — trigger-loop guard
// ---------------------------------------------------------------------------

func TestDeliberator_TriggerLoopGuard(t *testing.T) {
	fc := newFakeChimera(t, http.StatusOK, chimeraVerdictJSON(
		"VERDICT: PASS\nSummary: Consensus.\n", "balanced", nil,
	))

	ch, _ := newDeliberationChannel(t, "agent-a", "agent-b")
	store := NewMemMessageStore()
	msgs := disagreeingMessages(ch, store)

	delib := NewDeliberator(DeliberatorOpts{
		Client: NewHTTPChimeraClient(fc.URL, "", 5*time.Second),
	})

	// First call: should trigger and post a verdict.
	if _, err := delib.DeliberateIfNeeded(context.Background(), ch, store, msgs, ""); err != nil {
		t.Fatalf("first DeliberateIfNeeded: %v", err)
	}
	if fc.calls != 1 {
		t.Fatalf("expected 1 chimera call after first run, got %d", fc.calls)
	}

	// Second call with the same message slice — ShouldTrigger must
	// observe the verdict message at the tail and refuse to fire again.
	msgs = allMessages(t, store, ch.ID)
	if _, err := delib.DeliberateIfNeeded(context.Background(), ch, store, msgs, ""); !errors.Is(err, ErrNotTriggered) {
		t.Fatalf("expected ErrNotTriggered on second call (loop guard), got %v", err)
	}
	if fc.calls != 1 {
		t.Fatalf("expected chimera call count to remain 1, got %d", fc.calls)
	}
}

// ---------------------------------------------------------------------------
// Scenario 5 — FAIL invokes handler; PASS-with-conditions passes conditions
// ---------------------------------------------------------------------------

func TestDeliberator_FAILVerdict_InvokesHandler(t *testing.T) {
	fc := newFakeChimera(t, http.StatusOK, chimeraVerdictJSON(
		"VERDICT: FAIL\nSummary: The proposal is unsafe.\n",
		"balanced",
		map[string]string{"claude": "reject"},
	))

	ch, _ := newDeliberationChannel(t, "agent-a", "agent-b")
	store := NewMemMessageStore()
	msgs := disagreeingMessages(ch, store)

	handler := &recordingHandler{}
	delib := NewDeliberator(DeliberatorOpts{
		Client:  NewHTTPChimeraClient(fc.URL, "", 5*time.Second),
		Handler: handler,
	})

	verdict, err := delib.DeliberateIfNeeded(context.Background(), ch, store, msgs, "")
	if err != nil {
		t.Fatalf("DeliberateIfNeeded: %v", err)
	}
	if verdict.Outcome != VerdictFail {
		t.Errorf("expected VerdictFail, got %q", verdict.Outcome)
	}
	if len(handler.calls) != 1 {
		t.Fatalf("expected handler invoked once on FAIL, got %d", len(handler.calls))
	}
	if handler.calls[0].Verdict.Outcome != VerdictFail {
		t.Errorf("handler received verdict %q, want %q", handler.calls[0].Verdict.Outcome, VerdictFail)
	}
}

func TestDeliberator_ConditionalVerdict_PassesConditions(t *testing.T) {
	fc := newFakeChimera(t, http.StatusOK, chimeraVerdictJSON(
		"VERDICT: CONDITIONAL\nSummary: Acceptable with mitigations.\nCondition: add rate limit\nCondition: add audit log\n",
		"balanced",
		nil,
	))

	ch, _ := newDeliberationChannel(t, "agent-a", "agent-b")
	store := NewMemMessageStore()
	msgs := disagreeingMessages(ch, store)

	handler := &recordingHandler{}
	delib := NewDeliberator(DeliberatorOpts{
		Client:  NewHTTPChimeraClient(fc.URL, "", 5*time.Second),
		Handler: handler,
	})

	verdict, err := delib.DeliberateIfNeeded(context.Background(), ch, store, msgs, "")
	if err != nil {
		t.Fatalf("DeliberateIfNeeded: %v", err)
	}
	if verdict.Outcome != VerdictConditional {
		t.Errorf("expected VerdictConditional, got %q", verdict.Outcome)
	}
	if len(verdict.Conditions) != 2 {
		t.Fatalf("expected 2 conditions, got %d (%v)", len(verdict.Conditions), verdict.Conditions)
	}
	if verdict.Conditions[0] != "add rate limit" {
		t.Errorf("condition[0] = %q, want %q", verdict.Conditions[0], "add rate limit")
	}
	if verdict.Conditions[1] != "add audit log" {
		t.Errorf("condition[1] = %q, want %q", verdict.Conditions[1], "add audit log")
	}

	// Handler must receive the conditions alongside the verdict.
	if len(handler.calls) != 1 {
		t.Fatalf("expected handler called once, got %d", len(handler.calls))
	}
	if len(handler.calls[0].Verdict.Conditions) != 2 {
		t.Errorf("handler received %d conditions, want 2", len(handler.calls[0].Verdict.Conditions))
	}

	// Conditions should also surface in the posted message content.
	posted := allMessages(t, store, ch.ID)
	last := posted[len(posted)-1]
	if !strings.Contains(last.Content, "Conditions:") {
		t.Errorf("expected posted verdict to surface conditions, got %q", last.Content)
	}
	if !strings.Contains(last.Content, "add rate limit") {
		t.Errorf("expected posted verdict to mention condition[0], got %q", last.Content)
	}
}

// ---------------------------------------------------------------------------
// Scenario 6 — Chimera HTTP / parse / timeout errors
// ---------------------------------------------------------------------------

func TestDeliberator_ChimeraReturns500_NoVerdictPosted(t *testing.T) {
	fc := newFakeChimera(t, http.StatusInternalServerError, `{"error":"boom"}`)

	ch, _ := newDeliberationChannel(t, "agent-a", "agent-b")
	store := NewMemMessageStore()
	msgs := disagreeingMessages(ch, store)

	delib := NewDeliberator(DeliberatorOpts{
		Client: NewHTTPChimeraClient(fc.URL, "", 5*time.Second),
	})
	_, err := delib.DeliberateIfNeeded(context.Background(), ch, store, msgs, "")
	if err == nil {
		t.Fatal("expected error from 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to mention status 500, got %v", err)
	}

	// No verdict should have been posted to the store.
	posted := allMessages(t, store, ch.ID)
	if len(posted) != 3 {
		t.Errorf("expected 3 messages (no verdict posted), got %d", len(posted))
	}
}

func TestDeliberator_ChimeraMalformedJSON_NoVerdictPosted(t *testing.T) {
	fc := newFakeChimera(t, http.StatusOK, `{"not": "the right shape"`)

	ch, _ := newDeliberationChannel(t, "agent-a", "agent-b")
	store := NewMemMessageStore()
	msgs := disagreeingMessages(ch, store)

	delib := NewDeliberator(DeliberatorOpts{
		Client: NewHTTPChimeraClient(fc.URL, "", 5*time.Second),
	})
	_, err := delib.DeliberateIfNeeded(context.Background(), ch, store, msgs, "")
	if err == nil {
		t.Fatal("expected error from malformed JSON")
	}

	posted := allMessages(t, store, ch.ID)
	if len(posted) != 3 {
		t.Errorf("expected 3 messages (no verdict posted), got %d", len(posted))
	}
}

func TestDeliberator_ChimeraMalformedVerdictText_NoVerdictPosted(t *testing.T) {
	// JSON parses fine, but the embedded "result" is junk — ParseVerdict
	// must reject it (or, at minimum, never crash). We treat the lenient
	// fallback as a verdict rather than an error here — but a recognised
	// unrecognised outcome header MUST surface an error and prevent the
	// verdict post.
	fc := newFakeChimera(t, http.StatusOK, chimeraVerdictJSON(
		"VERDICT: UNKNOWN_OUTCOME\nSummary: gibberish.\n",
		"balanced", nil,
	))

	ch, _ := newDeliberationChannel(t, "agent-a", "agent-b")
	store := NewMemMessageStore()
	msgs := disagreeingMessages(ch, store)

	delib := NewDeliberator(DeliberatorOpts{
		Client: NewHTTPChimeraClient(fc.URL, "", 5*time.Second),
	})
	_, err := delib.DeliberateIfNeeded(context.Background(), ch, store, msgs, "")
	if err == nil {
		t.Fatal("expected error for unrecognised verdict outcome")
	}
	posted := allMessages(t, store, ch.ID)
	if len(posted) != 3 {
		t.Errorf("expected 3 messages (no verdict posted), got %d", len(posted))
	}
}

func TestDeliberator_ChimeraTimeout_NoVerdictPosted(t *testing.T) {
	// Hand-rolled server that holds the connection until the client times
	// out. Using http://localhost:0 would be flaky; an httptest server
	// that sleeps is deterministic.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"VERDICT: PASS\nSummary: late\n"}`))
	}))
	t.Cleanup(srv.Close)

	ch, _ := newDeliberationChannel(t, "agent-a", "agent-b")
	store := NewMemMessageStore()
	msgs := disagreeingMessages(ch, store)

	delib := NewDeliberator(DeliberatorOpts{
		Client: NewHTTPChimeraClient(srv.URL, "", 50*time.Millisecond),
	})
	_, err := delib.DeliberateIfNeeded(context.Background(), ch, store, msgs, "")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	posted := allMessages(t, store, ch.ID)
	if len(posted) != 3 {
		t.Errorf("expected 3 messages (no verdict posted on timeout), got %d", len(posted))
	}
}

func TestDeliberator_ContextCancellation_NoVerdictPosted(t *testing.T) {
	// Server that respects cancellation by sleeping until ctx deadline.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(500 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"result":"VERDICT: PASS\n"}`))
		case <-r.Context().Done():
			return
		}
	}))
	t.Cleanup(srv.Close)

	ch, _ := newDeliberationChannel(t, "agent-a", "agent-b")
	store := NewMemMessageStore()
	msgs := disagreeingMessages(ch, store)

	delib := NewDeliberator(DeliberatorOpts{
		Client: NewHTTPChimeraClient(srv.URL, "", 5*time.Second),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := delib.DeliberateIfNeeded(ctx, ch, store, msgs, "")
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	posted := allMessages(t, store, ch.ID)
	if len(posted) != 3 {
		t.Errorf("expected 3 messages (no verdict posted on cancel), got %d", len(posted))
	}
}

// ---------------------------------------------------------------------------
// Scenario 7 — DisagreementScorer default heuristic unit tests
// ---------------------------------------------------------------------------

func TestKeywordDisagreementScorer_AboveThreshold(t *testing.T) {
	s := NewKeywordDisagreementScorer()
	msgs := []*ChannelMessage{
		{Author: "agent-a", AuthorType: AuthorAgent, Type: MsgText, Content: "I disagree with this."},
		{Author: "agent-b", AuthorType: AuthorAgent, Type: MsgText, Content: "I oppose — wrong approach."},
		{Author: "agent-a", AuthorType: AuthorAgent, Type: MsgText, Content: "Blocking this PR."},
	}
	score := s.Score(msgs)
	if score <= DefaultDisagreementThreshold {
		t.Fatalf("expected score > %v, got %v", DefaultDisagreementThreshold, score)
	}
}

func TestKeywordDisagreementScorer_BelowThreshold(t *testing.T) {
	s := NewKeywordDisagreementScorer()
	msgs := []*ChannelMessage{
		{Author: "agent-a", AuthorType: AuthorAgent, Type: MsgText, Content: "Sounds good."},
		{Author: "agent-b", AuthorType: AuthorAgent, Type: MsgText, Content: "Agreed."},
		{Author: "agent-a", AuthorType: AuthorAgent, Type: MsgText, Content: "+1."},
	}
	score := s.Score(msgs)
	if score > DefaultDisagreementThreshold {
		t.Fatalf("expected score <= %v for agreeable traffic, got %v",
			DefaultDisagreementThreshold, score)
	}
}

func TestKeywordDisagreementScorer_IgnoresHumans(t *testing.T) {
	s := NewKeywordDisagreementScorer()
	// Humans + chimera cannot trigger — only AuthorAgent counts.
	msgs := []*ChannelMessage{
		{Author: "alice", AuthorType: AuthorHuman, Type: MsgText, Content: "I disagree."},
		{Author: "bob", AuthorType: AuthorHuman, Type: MsgText, Content: "I oppose."},
		{Author: "chimera", AuthorType: AuthorChimera, Type: MsgText, Content: "wrong."},
	}
	score := s.Score(msgs)
	if score != 0 {
		t.Errorf("expected score 0 when only humans/chimera speak, got %v", score)
	}
}

func TestKeywordDisagreementScorer_RequiresMultipleAgents(t *testing.T) {
	s := NewKeywordDisagreementScorer()
	msgs := []*ChannelMessage{
		{Author: "agent-a", AuthorType: AuthorAgent, Type: MsgText, Content: "I disagree."},
		{Author: "agent-a", AuthorType: AuthorAgent, Type: MsgText, Content: "I oppose."},
	}
	score := s.Score(msgs)
	if score != 0 {
		t.Errorf("expected score 0 with only one agent, got %v", score)
	}
}

func TestKeywordDisagreementScorer_NilReceiver(t *testing.T) {
	var s *KeywordDisagreementScorer
	if score := s.Score([]*ChannelMessage{
		{Author: "agent-a", AuthorType: AuthorAgent, Type: MsgText, Content: "disagree"},
		{Author: "agent-b", AuthorType: AuthorAgent, Type: MsgText, Content: "oppose"},
	}); score != 0 {
		t.Errorf("expected nil receiver to return 0, got %v", score)
	}
}

func TestKeywordDisagreementScorer_CustomKeywords(t *testing.T) {
	s := &KeywordDisagreementScorer{Keywords: []string{"banana"}}
	msgs := []*ChannelMessage{
		{Author: "agent-a", AuthorType: AuthorAgent, Type: MsgText, Content: "banana."},
		{Author: "agent-b", AuthorType: AuthorAgent, Type: MsgText, Content: "banana split."},
	}
	if score := s.Score(msgs); score <= DefaultDisagreementThreshold {
		t.Errorf("expected custom keywords to push score above threshold, got %v", score)
	}
}

// ---------------------------------------------------------------------------
// ShouldTrigger pre-flight tests
// ---------------------------------------------------------------------------

func TestDeliberator_ShouldTrigger_HappyPath(t *testing.T) {
	ch, _ := newDeliberationChannel(t, "agent-a", "agent-b")
	store := NewMemMessageStore()
	msgs := disagreeingMessages(ch, store)

	delib := NewDeliberator()
	if !delib.ShouldTrigger(ch, msgs) {
		t.Error("expected ShouldTrigger=true for disagreeing 2-agent traffic")
	}
}

func TestDeliberator_ShouldTrigger_WrongChannelType(t *testing.T) {
	ch := NewChannel("task-1", ChannelTask, []string{"agent-a", "agent-b"})
	msgs := disagreeingMessages(ch, NewMemMessageStore())

	delib := NewDeliberator()
	if delib.ShouldTrigger(ch, msgs) {
		t.Error("expected ShouldTrigger=false for non-deliberation channel")
	}
}

// ---------------------------------------------------------------------------
// Deliberator config accessors
// ---------------------------------------------------------------------------

func TestDeliberator_Defaults(t *testing.T) {
	d := NewDeliberator()
	if d.Client() == nil {
		t.Error("expected default Client")
	}
	if _, ok := d.Handler().(NopVerdictHandler); !ok {
		t.Errorf("expected default NopVerdictHandler, got %T", d.Handler())
	}
	if d.Scorer() == nil {
		t.Error("expected default Scorer")
	}
	if d.MessageCountThreshold() != DefaultMessageCountThreshold {
		t.Errorf("expected default message threshold %d, got %d",
			DefaultMessageCountThreshold, d.MessageCountThreshold())
	}
	if d.DisagreementThreshold() != DefaultDisagreementThreshold {
		t.Errorf("expected default disagreement threshold %v, got %v",
			DefaultDisagreementThreshold, d.DisagreementThreshold())
	}
	if d.Formation() != DefaultFormation {
		t.Errorf("expected default formation %q, got %q", DefaultFormation, d.Formation())
	}
}

func TestDeliberator_OptsOverride(t *testing.T) {
	custom := &recordingHandler{}
	scorer := NewKeywordDisagreementScorer()
	d := NewDeliberator(DeliberatorOpts{
		Handler:               custom,
		Scorer:                scorer,
		MessageCountThreshold: 7,
		DisagreementThreshold: 0.5,
		Formation:             "rigorous",
	})
	if d.Handler() != custom {
		t.Error("expected custom handler")
	}
	if d.Scorer() != scorer {
		t.Error("expected custom scorer")
	}
	if d.MessageCountThreshold() != 7 {
		t.Errorf("expected threshold 7, got %d", d.MessageCountThreshold())
	}
	if d.DisagreementThreshold() != 0.5 {
		t.Errorf("expected disagreement threshold 0.5, got %v", d.DisagreementThreshold())
	}
	if d.Formation() != "rigorous" {
		t.Errorf("expected formation %q, got %q", "rigorous", d.Formation())
	}
}

// ---------------------------------------------------------------------------
// ParseVerdict unit tests
// ---------------------------------------------------------------------------

func TestParseVerdict_Pass(t *testing.T) {
	v, err := ParseVerdict("VERDICT: PASS\nSummary: Looks good.")
	if err != nil {
		t.Fatalf("ParseVerdict: %v", err)
	}
	if v.Outcome != VerdictPass {
		t.Errorf("expected PASS, got %q", v.Outcome)
	}
	if v.Summary != "Looks good." {
		t.Errorf("unexpected summary %q", v.Summary)
	}
}

func TestParseVerdict_Fail(t *testing.T) {
	v, err := ParseVerdict("VERDICT: FAIL\nSummary: Reject.")
	if err != nil {
		t.Fatalf("ParseVerdict: %v", err)
	}
	if v.Outcome != VerdictFail {
		t.Errorf("expected FAIL, got %q", v.Outcome)
	}
}

func TestParseVerdict_Conditional(t *testing.T) {
	v, err := ParseVerdict("VERDICT: CONDITIONAL\nSummary: Conditional.\nCondition: add audit\nCondition: ship small")
	if err != nil {
		t.Fatalf("ParseVerdict: %v", err)
	}
	if v.Outcome != VerdictConditional {
		t.Errorf("expected CONDITIONAL, got %q", v.Outcome)
	}
	if len(v.Conditions) != 2 {
		t.Errorf("expected 2 conditions, got %d", len(v.Conditions))
	}
}

func TestParseVerdict_UnrecognisedOutcome(t *testing.T) {
	_, err := ParseVerdict("VERDICT: MAYBE\nSummary: who knows")
	if err == nil {
		t.Fatal("expected error for unrecognised outcome")
	}
}

func TestParseVerdict_LenientFallback(t *testing.T) {
	// No structured header — lenient fallback should still produce a
	// verdict outcome based on keyword sniffing.
	v, err := ParseVerdict("I think we should reject this design.")
	if err != nil {
		t.Fatalf("ParseVerdict: %v", err)
	}
	if v.Outcome != VerdictFail {
		t.Errorf("expected lenient FAIL, got %q", v.Outcome)
	}
}

// ---------------------------------------------------------------------------
// BuildPrompt unit test
// ---------------------------------------------------------------------------

func TestBuildPrompt_IncludesChannelAndCode(t *testing.T) {
	ch := NewChannel("review-x", ChannelDeliberation, []string{"agent-a"})
	ch.ID = "ch-fixed-123"
	msgs := []*ChannelMessage{
		{ChannelID: ch.ID, Author: "agent-a", AuthorType: AuthorAgent, Type: MsgText, Content: "disagree", Timestamp: time.Unix(1700000000, 0).UTC()},
	}
	prompt := BuildPrompt(ch, msgs, "// file content")
	if !strings.Contains(prompt, "ch-fixed-123") {
		t.Errorf("expected prompt to contain channel id, got %q", prompt)
	}
	if !strings.Contains(prompt, "review-x") {
		t.Errorf("expected prompt to contain channel name, got %q", prompt)
	}
	if !strings.Contains(prompt, "// file content") {
		t.Errorf("expected prompt to contain code context, got %q", prompt)
	}
	if !strings.Contains(prompt, "VERDICT: PASS|FAIL|CONDITIONAL") {
		t.Errorf("expected prompt to instruct verdict format, got %q", prompt)
	}
}

// ---------------------------------------------------------------------------
// Nil-arg safety
// ---------------------------------------------------------------------------

func TestDeliberator_DeliberateIfNeeded_NilChannel(t *testing.T) {
	d := NewDeliberator()
	_, err := d.DeliberateIfNeeded(context.Background(), nil, NewMemMessageStore(), nil, "")
	if err == nil {
		t.Fatal("expected error for nil channel")
	}
}

func TestDeliberator_DeliberateIfNeeded_NilDeliberator(t *testing.T) {
	var d *Deliberator
	_, err := d.DeliberateIfNeeded(context.Background(), NewChannel("x", ChannelDeliberation, nil),
		NewMemMessageStore(), nil, "")
	if err == nil {
		t.Fatal("expected error for nil deliberator")
	}
}

// ---------------------------------------------------------------------------
// NopVerdictHandler smoke test
// ---------------------------------------------------------------------------

func TestNopVerdictHandler_NoError(t *testing.T) {
	h := NopVerdictHandler{}
	if err := h.OnVerdict(context.Background(), "ch-1", Verdict{Outcome: VerdictPass}); err != nil {
		t.Errorf("expected nil error from nop handler, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Handler error surfaces but verdict is still returned
// ---------------------------------------------------------------------------

func TestDeliberator_HandlerError_ReturnsVerdictAndError(t *testing.T) {
	fc := newFakeChimera(t, http.StatusOK, chimeraVerdictJSON(
		"VERDICT: FAIL\nSummary: not safe.\n", "balanced", nil,
	))

	ch, _ := newDeliberationChannel(t, "agent-a", "agent-b")
	store := NewMemMessageStore()
	msgs := disagreeingMessages(ch, store)

	handler := &recordingHandler{failErr: errors.New("PR close failed")}
	delib := NewDeliberator(DeliberatorOpts{
		Client:  NewHTTPChimeraClient(fc.URL, "", 5*time.Second),
		Handler: handler,
	})

	verdict, err := delib.DeliberateIfNeeded(context.Background(), ch, store, msgs, "")
	if err == nil {
		t.Fatal("expected handler error to surface")
	}
	if verdict.Outcome != VerdictFail {
		t.Errorf("expected verdict to still be returned, got outcome %q", verdict.Outcome)
	}
}
