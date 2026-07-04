package memory

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func frozenTime() func() time.Time {
	t := time.Date(2026, 6, 19, 14, 22, 0, 0, time.UTC)
	return func() time.Time { return t }
}

func TestInboxAppendValidates(t *testing.T) {
	in := NewInbox(0)
	cases := []struct {
		name string
		e    InboxEvent
		ok   bool
	}{
		{"missing agent", InboxEvent{Repo: "r", EventType: EventMerge, Summary: "x"}, false},
		{"missing repo", InboxEvent{Agent: "a", EventType: EventMerge, Summary: "x"}, false},
		{"missing type", InboxEvent{Agent: "a", Repo: "r", Summary: "x"}, false},
		{"missing summary", InboxEvent{Agent: "a", Repo: "r", EventType: EventMerge}, false},
		{"valid", InboxEvent{Agent: "a", Repo: "r", EventType: EventMerge, Summary: "merged PR"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := in.Append(tc.e)
			if tc.ok && err != nil {
				t.Fatalf("expected ok, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestInboxFingerprint(t *testing.T) {
	e := InboxEvent{Agent: "sandbox-7", Repo: "helix", EventType: EventGateFailure, File: "x.go", Operation: "write"}
	if !strings.Contains(e.EventFingerprint(), "sandbox-7|helix|gate_failure|x.go|write") {
		t.Fatalf("bad fingerprint: %s", e.EventFingerprint())
	}
}

func TestInboxBatchWindowCoalesce(t *testing.T) {
	in := NewInbox(0)
	in.SetClock(frozenTime())
	base := InboxEvent{Agent: "a", Repo: "r", EventType: EventGateFailure, File: "x.go", Operation: "write", Summary: "lint fail"}
	added, err := in.Append(base)
	if err != nil {
		t.Fatalf("first append: %v", err)
	}
	if added.ID == "" {
		t.Fatalf("first append should populate ID")
	}
	if in.Count() != 1 {
		t.Fatalf("after first append count=%d", in.Count())
	}
	// Same fingerprint within batch window → coalesce.
	_, err = in.Append(base)
	if err == nil {
		t.Fatalf("expected coalesce error")
	}
	if in.Count() != 1 {
		t.Fatalf("coalesced event should not increment count")
	}
	// Different file/op → new event.
	_, err = in.Append(InboxEvent{Agent: "a", Repo: "r", EventType: EventGateFailure, File: "y.go", Operation: "write", Summary: "x"})
	if err != nil {
		t.Fatalf("different event should add: %v", err)
	}
	if in.Count() != 2 {
		t.Fatalf("expected count=2 got %d", in.Count())
	}
}

func TestInboxNewBatchAfterWindow(t *testing.T) {
	clock := time.Date(2026, 6, 19, 14, 22, 0, 0, time.UTC)
	in := NewInbox(0)
	in.SetClock(func() time.Time { return clock })
	base := InboxEvent{Agent: "a", Repo: "r", EventType: EventGateFailure, File: "x.go", Operation: "write", Summary: "first"}
	if _, err := in.Append(base); err != nil {
		t.Fatalf("first: %v", err)
	}
	// Advance beyond the 5-minute window.
	clock = clock.Add(6 * time.Minute)
	if _, err := in.Append(base); err != nil {
		t.Fatalf("post-window append: %v", err)
	}
	if in.Count() != 2 {
		t.Fatalf("expected two events after window, got %d", in.Count())
	}
}

func TestInboxDeduplicateByFileOp(t *testing.T) {
	in := NewInbox(0)
	in.SetClock(frozenTime())
	base := InboxEvent{Agent: "a", Repo: "r", EventType: EventGateFailure, File: "x.go", Operation: "write", Summary: "lint"}
	if _, err := in.Append(base); err != nil {
		t.Fatalf("first: %v", err)
	}
	// Add an event with a different agent but same file+operation: dedupe
	// rules remove it because the spec says file+operation alone is the
	// collision key.
	_, _ = in.Append(InboxEvent{Agent: "b", Repo: "s", EventType: EventGateFailure, File: "x.go", Operation: "write", Summary: "again"})
	// Without dedupe we now have 2 events.
	if in.Count() != 2 {
		t.Fatalf("expected 2 events pre-dedupe, got %d", in.Count())
	}
	removed := in.Deduplicate()
	if removed != 1 {
		t.Fatalf("expected 1 removed, got %d", removed)
	}
	if in.Count() != 1 {
		t.Fatalf("expected 1 event post-dedupe, got %d", in.Count())
	}
}

func TestInboxDeduplicatePreservesNoFileEvents(t *testing.T) {
	in := NewInbox(0)
	in.SetClock(frozenTime())
	_, _ = in.Append(InboxEvent{Agent: "a", Repo: "r", EventType: EventDecision, Summary: "decision1"})
	_, _ = in.Append(InboxEvent{Agent: "a", Repo: "r", EventType: EventDecision, Summary: "decision2"})
	if removed := in.Deduplicate(); removed != 0 {
		t.Fatalf("expected 0 removed, got %d", removed)
	}
}

func TestCompilerBatchGroups(t *testing.T) {
	in := NewInbox(0)
	in.SetClock(frozenTime())
	// 3 same-fingerprint events within 4 minutes (within 5-min window).
	for i := 0; i < 3; i++ {
		_, err := in.Append(InboxEvent{
			Agent: "a", Repo: "r",
			EventType: EventGateFailure, File: "x.go", Operation: "write",
			Summary: "lint fail",
		})
		// First will append; subsequent coalesce but still in the inbox as 1 event.
		if err != nil && !strings.Contains(err.Error(), "merged into existing") {
			t.Fatalf("unexpected: %v", err)
		}
	}
	_, _ = in.Append(InboxEvent{Agent: "a", Repo: "r", EventType: EventDecision, Summary: "use SQLite"})
	c := NewCompiler(in)
	c.SetClock(frozenTime())
	groups := c.Batch(DefaultMaxBatchSize)
	if len(groups) != 2 {
		t.Fatalf("expected 2 batches, got %d", len(groups))
	}
}

func TestCompilerBatchSortedDeterministic(t *testing.T) {
	in := NewInbox(0)
	now := time.Date(2026, 6, 19, 14, 22, 0, 0, time.UTC)
	in.SetClock(func() time.Time { return now })
	_, _ = in.Append(InboxEvent{Agent: "b", Repo: "r", EventType: EventDecision, Summary: "x"})
	_, _ = in.Append(InboxEvent{Agent: "a", Repo: "r", EventType: EventDecision, Summary: "x"})
	c := NewCompiler(in)
	c.SetClock(func() time.Time { return now })
	g := c.Batch(DefaultMaxBatchSize)
	if len(g) < 2 {
		t.Fatalf("not enough batches")
	}
	if g[0].Agent > g[1].Agent {
		t.Fatalf("must be sorted by Agent then; got %s before %s", g[0].Agent, g[1].Agent)
	}
}

func TestCompilerIDStable(t *testing.T) {
	in := NewInbox(0)
	in.SetClock(frozenTime())
	_, _ = in.Append(InboxEvent{Agent: "a", Repo: "r", EventType: EventDecision, Summary: "use SQLite"})
	c := NewCompiler(in)
	c.SetClock(frozenTime())
	g := c.Batch(0)
	if !strings.HasPrefix(g[0].ID, "mem-") {
		t.Fatalf("id must start with mem-, got %s", g[0].ID)
	}
}

func TestDomainFromEvent(t *testing.T) {
	if domainFromType(EventIncident) != DomainEvent {
		t.Fatalf("incident should be event domain")
	}
	if domainFromType(EventDecision) != DomainConcept {
		t.Fatalf("decision should be concept domain")
	}
	if domainFromType("unknown") != DomainRawNote {
		t.Fatalf("unknown type should default to raw_note")
	}
}

func TestPathFor(t *testing.T) {
	e := CompiledEntry{
		ID:    "mem-1",
		Agent: "sandbox-7", Repo: "helix",
		EventType: EventIncident,
		Summary:   "reranker race",
		Timestamp: time.Date(2026, 6, 19, 14, 22, 0, 0, time.UTC),
	}
	_, key := pathFor(e)
	if !strings.HasPrefix(key, "/helix/platform/incidents/") {
		t.Fatalf("incident must land under platform/incidents, got %s", key)
	}
	e2 := CompiledEntry{Agent: "sandbox-7", EventType: EventDecision, Summary: "use sqlite"}
	_, key2 := pathFor(e2)
	if !strings.HasPrefix(key2, "/helix/agents/sandbox-7/decisions/") {
		t.Fatalf("decision must land under agents/<id>/decisions, got %s", key2)
	}
}

func TestEntrySlug(t *testing.T) {
	e := CompiledEntry{
		Summary:   "Use SQLite for AGENT state!",
		Timestamp: time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC),
	}
	slug := entrySlug(e)
	if !strings.HasPrefix(slug, "20260619-") {
		t.Fatalf("slug must start with date, got %s", slug)
	}
	if strings.Contains(slug, " ") || strings.Contains(slug, "!") {
		t.Fatalf("slug must be filesystem-safe, got %s", slug)
	}
}

func TestPersistenceBridgePersistsValidEntry(t *testing.T) {
	store := NewMemStore()
	b := NewPersistenceBridge(store)
	b.SetClock(frozenTime())
	e := CompiledEntry{
		ID: "mem-test-001", Agent: "a", Repo: "r", EventType: EventDecision,
		Summary: "use SQLite over Postgres", Timestamp: frozenTime()(),
		Tags: []string{"tier1", "agent-state"}, EventCount: 1,
	}
	mem, err := b.CompileAndPersist(e)
	if err != nil {
		t.Fatalf("persist: %v", err)
	}
	if !strings.Contains(mem.Key, "/agents/a/") {
		t.Fatalf("expected key under /agents/a/, got %s", mem.Key)
	}
	got, err := store.Read(mem.Key)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.Attributes.Decision != e.Summary {
		t.Fatalf("decision mismatch")
	}
}

func TestPersistenceBridgeIncidentGoesToPlatform(t *testing.T) {
	store := NewMemStore()
	b := NewPersistenceBridge(store)
	b.SetClock(frozenTime())
	e := CompiledEntry{
		ID: "mem-incident-001", Agent: "a", Repo: "r",
		EventType: EventIncident, Summary: "SEV-1 agent crash",
		Timestamp: frozenTime()(), EventCount: 1,
	}
	mem, err := b.CompileAndPersist(e)
	if err != nil {
		t.Fatalf("persist: %v", err)
	}
	if !strings.HasPrefix(mem.Key, "/helix/platform/incidents/") {
		t.Fatalf("incident must land under platform, got %s", mem.Key)
	}
}

func TestLifecycleRunFullPipeline(t *testing.T) {
	store := NewMemStore()
	in := NewInbox(0)
	in.SetClock(frozenTime())
	_, _ = in.Append(InboxEvent{
		Agent: "a", Repo: "r",
		EventType: EventGateFailure, File: "x.go", Operation: "write",
		Summary: "lint fail: unused fmt",
	})
	_, _ = in.Append(InboxEvent{
		Agent: "a", Repo: "r",
		EventType: EventDecision, Summary: "switch from Postgres to SQLite",
	})
	lc := NewLifecycle(in, store)
	lc.Compiler.SetClock(frozenTime())
	lc.Bridge.SetClock(frozenTime())
	entries, indexes, err := lc.Run(frozenTime()())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected entries")
	}
	if len(indexes) == 0 {
		t.Fatalf("expected at least one index")
	}
	for _, idx := range indexes {
		if !strings.HasPrefix(idx.Format(), "# Memory Index —") {
			t.Fatalf("index format broken")
		}
	}
}

func TestLifecycleRunEmptyInbox(t *testing.T) {
	store := NewMemStore()
	in := NewInbox(0)
	lc := NewLifecycle(in, store)
	lc.Compiler.SetClock(frozenTime())
	entries, indexes, err := lc.Run(frozenTime()())
	if err != nil {
		t.Fatalf("empty run: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries")
	}
	if len(indexes) != 0 {
		t.Fatalf("expected 0 indexes, got %d", len(indexes))
	}
}

func TestBuildIndexEmptyNamespace(t *testing.T) {
	store := NewMemStore()
	idx, err := BuildIndex(store, NSPlatformIncidents, frozenTime()())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(idx.Entries) != 0 {
		t.Fatalf("empty ns should produce empty index")
	}
}

func TestBuildIndexFromMultipleEntries(t *testing.T) {
	store := NewMemStore()
	_ = store.Write(MemoryEntry{
		Key:           "/helix/agents/a/decisions/one",
		Domain:        DomainConcept,
		Attributes:    Attributes{Decision: "first"},
		EmbeddingText: "first",
	}, true)
	_ = store.Write(MemoryEntry{
		Key:           "/helix/agents/a/decisions/two",
		Domain:        DomainConcept,
		Attributes:    Attributes{Decision: "second"},
		EmbeddingText: "second",
	}, true)
	idx, err := BuildIndex(store, NSAgentsDecisions, frozenTime()())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(idx.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(idx.Entries))
	}
	// Verify ordering.
	if idx.Entries[0].Key > idx.Entries[1].Key {
		t.Fatalf("entries must be in key order")
	}
}

func TestBuildIndexNilStoreErrors(t *testing.T) {
	if _, err := BuildIndex(nil, NSAgentsDecisions, time.Now()); err == nil {
		t.Fatalf("expected error")
	}
}

func TestLifecycleCompilePropagatesError(t *testing.T) {
	badStore := &failingStore{}
	in := NewInbox(0)
	in.SetClock(frozenTime())
	_, _ = in.Append(InboxEvent{Agent: "a", Repo: "r", EventType: EventDecision, Summary: "x"})
	lc := NewLifecycle(in, badStore)
	lc.Compiler.SetClock(frozenTime())
	lc.Bridge.SetClock(frozenTime())
	_, _, err := lc.Run(frozenTime()())
	if err == nil {
		t.Fatalf("expected persistence failure")
	}
}

type failingStore struct{}

func (failingStore) Write(MemoryEntry, bool) error { return errors.New("disk full") }
func (failingStore) Read(string) (MemoryEntry, error) {
	return MemoryEntry{}, ErrNotFound
}
func (failingStore) Delete(string) error                      { return nil }
func (failingStore) Query(MemoryQuery) ([]MemoryEntry, error) { return nil, nil }

func TestNextIDStable(t *testing.T) {
	id := nextID(InboxEvent{Agent: "a", Repo: "r", EventType: EventDecision, Summary: "x"})
	if len(id) != 12 {
		t.Fatalf("expected 12-char hex, got %s", id)
	}
}

func TestInboxReset(t *testing.T) {
	in := NewInbox(0)
	in.SetClock(frozenTime())
	_, _ = in.Append(InboxEvent{Agent: "a", Repo: "r", EventType: EventDecision, Summary: "x"})
	if in.Count() == 0 {
		t.Fatalf("setup failed")
	}
	in.Reset()
	if in.Count() != 0 {
		t.Fatalf("Reset should clear inbox")
	}
}

func TestMergeTags(t *testing.T) {
	out := mergeTags([]string{"a", "b"}, []string{"b", "c"})
	if len(out) != 3 {
		t.Fatalf("expected 3 unique, got %d", len(out))
	}
	expected := map[string]bool{"a": true, "b": true, "c": true}
	for _, tag := range out {
		if !expected[tag] {
			t.Fatalf("unexpected tag %s", tag)
		}
	}
	// Empty inputs.
	if got := mergeTags(nil, nil); len(got) != 0 {
		t.Fatalf("nil+nil should return empty, got %v", got)
	}
}

func TestLifecycleCompiled(t *testing.T) {
	store := NewMemStore()
	in := NewInbox(0)
	in.SetClock(frozenTime())
	_, _ = in.Append(InboxEvent{Agent: "a", Repo: "r", EventType: EventDecision, Summary: "x"})
	lc := NewLifecycle(in, store)
	lc.Compiler.SetClock(frozenTime())
	_, _, err := lc.Run(frozenTime()())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	out := lc.Compiled()
	if len(out) != 1 {
		t.Fatalf("expected 1 compiled, got %d", len(out))
	}
}
