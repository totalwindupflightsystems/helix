package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Hivemind memory bank — spec §8.6
// ─────────────────────────────────────────────────────────────────────────────
//
// The Hivemind memory bank pipeline:
//   Inbox (raw events)
//     → Compiler (deduplicates, categorizes, enriches)
//       → Compiled memory (structured, searchable)
//         → _index (human-readable navigation)
//           → DuckBrain (persistent, version-controlled)
//
// Rules encoded here:
//   - Inbox events within 5 minutes with same agent+repo+event_type are batched.
//   - Deduplication: same file touched + same operation = one event.
//   - Each compiled entry gets UUID, timestamp, agent attribution, repo
//     context, tags.
//   - Compiled entries are persisted to DuckBrain as MemoryEntry records.
//   - _index provides human-readable navigation per namespace.

const (
	// BatchWindow is the lookback window for batching inbox events. From the
	// spec: "Events within 5 minutes with same agent+repo+event_type".
	BatchWindow = 5 * time.Minute

	// DefaultMaxBatchSize caps the number of events coalesced into a single
	// compiled entry (also flushes on size).
	DefaultMaxBatchSize = 50
)

// ─────────────────────────────────────────────────────────────────────────────
// Event types
// ─────────────────────────────────────────────────────────────────────────────

// EventType categorizes an inbox event. The set is illustrative — callers may
// supply any string but the constants here are the canonical platform values.
type EventType string

const (
	EventGateFailure   EventType = "gate_failure"
	EventMerge         EventType = "merge"
	EventPRReview      EventType = "pr_review"
	EventPromptRewrite EventType = "prompt_rewrite"
	EventIncident      EventType = "incident"
	EventDecision      EventType = "decision"
	EventAntiPattern   EventType = "anti_pattern"
)

// ─────────────────────────────────────────────────────────────────────────────
// InboxEvent (raw, unprocessed)
// ─────────────────────────────────────────────────────────────────────────────

// InboxEvent is a single observation landing in the memory bank. Events
// arrive continuously from many sources (GitReins, merge gate, marketplace
// reranker, the agent itself).
type InboxEvent struct {
	ID         string    `json:"id"`    // optional — populated on append
	Agent      string    `json:"agent"` // agent ID (matches /helix/<agent>/ keys)
	Repo       string    `json:"repo"`  // org/repo slug
	EventType  EventType `json:"event_type"`
	File       string    `json:"file,omitempty"`      // primary file touched
	Operation  string    `json:"operation,omitempty"` // e.g. "write", "delete", "rename"
	Summary    string    `json:"summary"`             // human-readable description
	Resolution string    `json:"resolution,omitempty"`
	Tags       []string  `json:"tags,omitempty"` // free-form
	ReceivedAt time.Time `json:"received_at"`    // set by Inbox.Append
	FlushAfter time.Time `json:"flush_after"`    // next allowed batch boundary
}

// EventFingerprint uniquely identifies an event for dedupe purposes. Two
// events with the same fingerprint are considered duplicates and collapsed.
func (e InboxEvent) EventFingerprint() string {
	return strings.Join([]string{
		e.Agent,
		e.Repo,
		string(e.EventType),
		e.File,
		e.Operation,
	}, "|")
}

// ─────────────────────────────────────────────────────────────────────────────
// Inbox — append-only queue of raw events
// ─────────────────────────────────────────────────────────────────────────────

// Inbox is a goroutine-safe, append-only queue of raw events.
type Inbox struct {
	mu        sync.Mutex
	events    []InboxEvent
	capacity  int
	nowFn     func() time.Time
	nextBatch map[string]int // fingerprint -> count of pending flush-window extensions
}

// NewInbox returns an inbox with the given capacity (0 = unbounded).
func NewInbox(capacity int) *Inbox {
	return &Inbox{
		capacity:  capacity,
		nowFn:     time.Now,
		nextBatch: make(map[string]int),
	}
}

// SetClock replaces the time source. Tests use this for deterministic IDs
// and batch-boundary math.
func (in *Inbox) SetClock(fn func() time.Time) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.nowFn = fn
}

// Append adds an event to the inbox. Events with duplicate fingerprints
// extending the existing batch window are coalesced (count++) rather than
// duplicated. Events whose fingerprint already exists beyond the batch
// window are still appended as a new event.
func (in *Inbox) Append(e InboxEvent) (InboxEvent, error) {
	if e.Agent == "" {
		return InboxEvent{}, errors.New("memory: inbox event missing agent")
	}
	if e.Repo == "" {
		return InboxEvent{}, errors.New("memory: inbox event missing repo")
	}
	if e.EventType == "" {
		return InboxEvent{}, errors.New("memory: inbox event missing event_type")
	}
	if e.Summary == "" {
		return InboxEvent{}, errors.New("memory: inbox event missing summary")
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	now := in.nowFn()
	e.ReceivedAt = now
	e.FlushAfter = now.Add(BatchWindow)
	if e.ID == "" {
		e.ID = "evt-" + nextID(e)
	}
	fp := e.EventFingerprint()
	// Look for a recent open batch with the same fingerprint within BatchWindow.
	for i := range in.events {
		existing := &in.events[i]
		if existing.EventFingerprint() != fp {
			continue
		}
		if now.Sub(existing.ReceivedAt) <= BatchWindow {
			// Coalesce: increment the in-window count and extend the flush time.
			existing.FlushAfter = now.Add(BatchWindow)
			in.nextBatch[fp]++
			// Update summary if newer is "better" (longer, more detailed).
			if len(e.Summary) > len(existing.Summary) {
				existing.Summary = e.Summary
			}
			if e.Resolution != "" {
				existing.Resolution = e.Resolution
			}
			if len(e.Tags) > 0 {
				merged := mergeTags(existing.Tags, e.Tags)
				existing.Tags = merged
			}
			// Return a synthetic ID referencing the batch.
			return InboxEvent{}, fmt.Errorf("merged into existing event %s (count=%d)", existing.ID, in.nextBatch[fp])
		}
	}
	in.events = append(in.events, e)
	if in.capacity > 0 && len(in.events) > in.capacity {
		// Drop oldest to maintain capacity (should be rare in real systems).
		in.events = in.events[len(in.events)-in.capacity:]
	}
	return e, nil
}

// Count returns the number of events currently in the inbox (excluding
// pending batches that were coalesced into existing entries).
func (in *Inbox) Count() int {
	in.mu.Lock()
	defer in.mu.Unlock()
	return len(in.events)
}

// Snapshot returns a copy of all inbox events.
func (in *Inbox) Snapshot() []InboxEvent {
	in.mu.Lock()
	defer in.mu.Unlock()
	out := make([]InboxEvent, len(in.events))
	copy(out, in.events)
	return out
}

// Reset clears the inbox — primarily for tests.
func (in *Inbox) Reset() {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.events = nil
	in.nextBatch = make(map[string]int)
}

// ─────────────────────────────────────────────────────────────────────────────
// CompiledEntry (after Compiler)
// ─────────────────────────────────────────────────────────────────────────────

// CompiledEntry is the structured record produced by the Compiler. It carries
// the full provenance metadata required by the DuckBrain schema.
type CompiledEntry struct {
	ID                string    `json:"id"`
	Timestamp         time.Time `json:"timestamp"`
	Agent             string    `json:"agent"`
	Repo              string    `json:"repo"`
	EventType         EventType `json:"event_type"`
	Summary           string    `json:"summary"`
	Resolution        string    `json:"resolution,omitempty"`
	Tags              []string  `json:"tags"`
	SourceEventIDs    []string  `json:"source_event_ids"`
	EventCount        int       `json:"event_count"` // number of inbox events coalesced
	Domain            Domain    `json:"domain"`
	PersistedToDuckDB bool      `json:"persisted_to_duckbrain"`
	DuckDBKey         string    `json:"duckbrain_key,omitempty"`
	PersistedAt       time.Time `json:"persisted_at,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Compiler
// ─────────────────────────────────────────────────────────────────────────────

// Compiler turns an Inbox into CompiledEntries. It is goroutine-safe and
// stateless beyond the inbox it reads from; callers may invoke Batch() and
// Compile() repeatedly to fold new events.
type Compiler struct {
	inbox *Inbox
	idSeq int
	mu    sync.Mutex
	nowFn func() time.Time
}

// NewCompiler returns a compiler that reads from the supplied inbox.
func NewCompiler(in *Inbox) *Compiler {
	return &Compiler{inbox: in, nowFn: time.Now}
}

// SetClock replaces the compiler's time source.
func (c *Compiler) SetClock(fn func() time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nowFn = fn
}

// Batch groups events that share agent+repo+event_type within BatchWindow
// into aggregate CompiledEntry records. The resulting slice is deterministic
// in batch order.
//
// maxBatchSize caps how many source events one compiled entry can absorb.
// Passing <=0 uses DefaultMaxBatchSize.
func (c *Compiler) Batch(maxBatchSize int) []CompiledEntry {
	if maxBatchSize <= 0 {
		maxBatchSize = DefaultMaxBatchSize
	}
	events := c.inbox.Snapshot()
	// Group by agent+repo+event_type fingerprint. Events within BatchWindow
	// of the previous one merge into the same group.
	type bucket struct {
		agent    string
		repo     string
		etype    EventType
		earliest time.Time
		latest   time.Time
		events   []InboxEvent
		tags     map[string]struct{}
	}
	groups := make([]*bucket, 0)
	bucketByFingerprint := make(map[string]*bucket)
	for _, e := range events {
		key := e.Agent + "|" + e.Repo + "|" + string(e.EventType)
		b, ok := bucketByFingerprint[key]
		if !ok || e.ReceivedAt.Sub(b.latest) > BatchWindow {
			b = &bucket{
				agent:    e.Agent,
				repo:     e.Repo,
				etype:    e.EventType,
				earliest: e.ReceivedAt,
				latest:   e.ReceivedAt,
				tags:     make(map[string]struct{}),
			}
			groups = append(groups, b)
			bucketByFingerprint[key] = b
		}
		b.events = append(b.events, e)
		b.latest = e.ReceivedAt
		if b.earliest.After(e.ReceivedAt) {
			b.earliest = e.ReceivedAt
		}
		for _, t := range e.Tags {
			b.tags[t] = struct{}{}
		}
	}
	out := make([]CompiledEntry, 0, len(groups))
	for _, g := range groups {
		// Split into chunks of maxBatchSize so very long-running incidents
		// produce multiple compiled entries instead of unbounded growth.
		for start := 0; start < len(g.events); start += maxBatchSize {
			end := start + maxBatchSize
			if end > len(g.events) {
				end = len(g.events)
			}
			chunk := g.events[start:end]
			tags := make([]string, 0, len(g.tags))
			for t := range g.tags {
				tags = append(tags, t)
			}
			sort.Strings(tags)
			c.mu.Lock()
			c.idSeq++
			seq := c.idSeq
			c.mu.Unlock()
			out = append(out, CompiledEntry{
				ID:             fmt.Sprintf("mem-%s-%05d", timeNow(c.nowFn).Format("20060102"), seq),
				Timestamp:      chunk[0].ReceivedAt,
				Agent:          g.agent,
				Repo:           g.repo,
				EventType:      g.etype,
				Summary:        chunk[0].Summary,
				Resolution:     chunk[len(chunk)-1].Resolution,
				Tags:           tags,
				SourceEventIDs: eventIDs(chunk),
				EventCount:     len(chunk),
				Domain:         domainFromType(g.etype),
			})
		}
	}
	// Deterministic order: sort by Timestamp, then Agent, Repo, EventType.
	sort.Slice(out, func(i, j int) bool {
		ti, tj := out[i].Timestamp, out[j].Timestamp
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		if out[i].Agent != out[j].Agent {
			return out[i].Agent < out[j].Agent
		}
		if out[i].Repo != out[j].Repo {
			return out[i].Repo < out[j].Repo
		}
		return out[i].EventType < out[j].EventType
	})
	return out
}

// Compile runs the full pipeline: read the inbox, batch and deduplicate, and
// return the resulting compiled entries. The inbox is left unchanged so
// callers may inspect it afterwards.
func (c *Compiler) Compile() []CompiledEntry {
	return c.Batch(DefaultMaxBatchSize)
}

// Deduplicate removes events with identical file+operation from the working
// set, even if their agent or repo differs. (Spec rule: "same file touched +
// same operation = one event".) Returns the count removed.
func (in *Inbox) Deduplicate() int {
	in.mu.Lock()
	defer in.mu.Unlock()
	seen := make(map[string]int) // file|operation -> index in deduped slice
	deduped := make([]InboxEvent, 0, len(in.events))
	removed := 0
	for _, ev := range in.events {
		k := strings.Join([]string{ev.File, ev.Operation}, "|")
		if k == "|" {
			// No file/operation means we can't dedupe (keep as-is).
			deduped = append(deduped, ev)
			continue
		}
		if _, ok := seen[k]; ok {
			removed++
			continue
		}
		seen[k] = len(deduped)
		deduped = append(deduped, ev)
	}
	in.events = deduped
	return removed
}

// ─────────────────────────────────────────────────────────────────────────────
// Compiled → DuckBrain memory entry conversion (PersistenceBridge)
// ─────────────────────────────────────────────────────────────────────────────

// PersistenceBridge converts compiled entries to DuckBrain MemoryEntry
// records and persists them via a MemoryStore.
type PersistenceBridge struct {
	store MemoryStore
	nowFn func() time.Time
}

// NewPersistenceBridge returns a bridge that writes to the supplied store.
func NewPersistenceBridge(store MemoryStore) *PersistenceBridge {
	return &PersistenceBridge{store: store, nowFn: time.Now}
}

// SetClock replaces the bridge's time source.
func (b *PersistenceBridge) SetClock(fn func() time.Time) {
	b.nowFn = fn
}

// CompileAndPersist persists one compiled entry as a DuckBrain MemoryEntry.
// The key path is /helix/agents/<agent>/<namespace>/<slug>-<id> where
// namespace is inferred from the event type.
//
// If the entry is an incident, the key is rooted in /helix/platform/incidents/
// instead so cross-agent incidents have a platform-level home.
//
// Returns the MemoryEntry actually persisted (with CreatedAt populated) and
// the final key used.
func (b *PersistenceBridge) CompileAndPersist(entry CompiledEntry) (MemoryEntry, error) {
	ns, key := pathFor(entry)
	if err := ValidateKey(key); err != nil {
		return MemoryEntry{}, fmt.Errorf("persistence bridge: %w", err)
	}
	memEntry := MemoryEntry{
		Key:    key,
		Domain: domainFor(ns),
		Attributes: Attributes{
			Decision: entry.Summary,
			Rationale: fmt.Sprintf(
				"%s on repo %s by agent %s (compiled from %d event(s))",
				entry.EventType, entry.Repo, entry.Agent, entry.EventCount,
			),
			Tradeoffs: nil,
		},
		EmbeddingText: fmt.Sprintf(
			"%s by agent %s in repo %s: %s",
			entry.EventType, entry.Agent, entry.Repo, entry.Summary,
		),
	}
	now := b.nowFn()
	memEntry.CreatedAt = now
	memEntry.UpdatedAt = now

	if err := b.store.Write(memEntry, true); err != nil {
		return MemoryEntry{}, err
	}
	entry.PersistedToDuckDB = true
	entry.PersistedAt = now
	entry.DuckDBKey = key
	return memEntry, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// _index builder — human-readable navigation
// ─────────────────────────────────────────────────────────────────────────────

// IndexEntry is a single line in the human-readable _index file.
type IndexEntry struct {
	Key        string
	Domain     Domain
	Summary    string
	Tags       []string
	EventCount int
	UpdatedAt  time.Time
}

// Index is a sortable collection of IndexEntry with stable serialization.
type Index struct {
	Namespace Namespace
	Generated time.Time
	Entries   []IndexEntry
}

// Format renders the index in human-readable markdown. The output is
// deterministic: entries are sorted by Key before rendering.
func (idx Index) Format() string {
	sorted := make([]IndexEntry, len(idx.Entries))
	copy(sorted, idx.Entries)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })

	var b strings.Builder
	fmt.Fprintf(&b, "# Memory Index — %s\n", idx.Namespace)
	fmt.Fprintf(&b, "Generated: %s\n\n", idx.Generated.UTC().Format(time.RFC3339))
	for _, e := range sorted {
		fmt.Fprintf(&b, "## %s\n", e.Key)
		fmt.Fprintf(&b, "- Domain: %s\n", e.Domain)
		fmt.Fprintf(&b, "- Updated: %s\n", e.UpdatedAt.UTC().Format(time.RFC3339))
		if e.EventCount > 0 {
			fmt.Fprintf(&b, "- Events: %d\n", e.EventCount)
		}
		if len(e.Tags) > 0 {
			fmt.Fprintf(&b, "- Tags: %s\n", strings.Join(e.Tags, ", "))
		}
		fmt.Fprintf(&b, "- Summary: %s\n\n", e.Summary)
	}
	return b.String()
}

// BuildIndex constructs an Index over all entries in store whose key falls
// under the supplied namespace. Returns an empty Index (not an error) when
// the namespace has no entries.
func BuildIndex(store MemoryStore, ns Namespace, now time.Time) (Index, error) {
	if store == nil {
		return Index{}, errors.New("memory: nil store in BuildIndex")
	}
	entries, err := store.Query(MemoryQuery{Namespace: ns})
	if err != nil {
		return Index{}, err
	}
	index := Index{
		Namespace: ns,
		Generated: now.UTC(),
		Entries:   make([]IndexEntry, 0, len(entries)),
	}
	for _, e := range entries {
		index.Entries = append(index.Entries, IndexEntry{
			Key:        e.Key,
			Domain:     e.Domain,
			Summary:    e.Attributes.Decision,
			Tags:       nil,
			EventCount: 0,
			UpdatedAt:  e.UpdatedAt,
		})
	}
	return index, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func timeNow(fn func() time.Time) time.Time {
	if fn == nil {
		return time.Now().UTC()
	}
	return fn().UTC()
}

// nextID returns a short hex hash for use as an event ID when the caller
// did not supply one.
func nextID(e InboxEvent) string {
	h := sha256.New()
	h.Write([]byte(e.Agent + "|" + e.Repo + "|" + string(e.EventType) + "|" + e.Summary))
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// mergeTags returns a sorted union of left and right.
func mergeTags(left, right []string) []string {
	set := make(map[string]struct{}, len(left)+len(right))
	for _, t := range left {
		set[t] = struct{}{}
	}
	for _, t := range right {
		set[t] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// eventIDs returns a copy of all event IDs in chunk.
func eventIDs(chunk []InboxEvent) []string {
	out := make([]string, 0, len(chunk))
	for _, e := range chunk {
		if e.ID != "" {
			out = append(out, e.ID)
		}
	}
	return out
}

// domainFromType maps event types to the most appropriate MemoryEntry
// Domain. This is a heuristic; callers can override at Compile time.
func domainFromType(t EventType) Domain {
	switch t {
	case EventDecision, EventPromptRewrite:
		return DomainConcept
	case EventAntiPattern:
		return DomainConcept
	case EventIncident:
		return DomainEvent
	case EventGateFailure:
		return DomainEvent
	case EventMerge, EventPRReview:
		return DomainEvent
	default:
		return DomainRawNote
	}
}

// resolvedNamespace returns the canonical (root+sub) namespace for an entry.
type resolvedNamespace struct {
	root string
	sub  string
}

// domainFor maps a resolved namespace to its DuckBrain Domain.
func domainFor(r resolvedNamespace) Domain {
	switch r.root {
	case "agents":
		switch r.sub {
		case "anti-patterns":
			return DomainConcept
		case "decisions":
			return DomainConcept
		case "preferences":
			return DomainConfig
		default:
			return DomainRawNote
		}
	case "repos":
		return DomainConcept
	case "platform":
		switch r.sub {
		case "incidents":
			return DomainEvent
		case "runbooks":
			return DomainConcept
		case "config":
			return DomainConfig
		default:
			return DomainRawNote
		}
	default:
		return DomainRawNote
	}
}

// pathFor returns the canonical (root+sub) namespace and key to use when
// persisting an entry.
func pathFor(e CompiledEntry) (resolvedNamespace, string) {
	slug := strings.ReplaceAll(entrySlug(e), "/", "_")
	switch e.EventType {
	case EventIncident:
		key := fmt.Sprintf("/helix/platform/incidents/%s", e.ID)
		return resolvedNamespace{root: "platform", sub: "incidents"}, key
	case EventAntiPattern:
		key := fmt.Sprintf("/helix/agents/%s/anti-patterns/%s", e.Agent, slug)
		return resolvedNamespace{root: "agents", sub: "anti-patterns"}, key
	case EventPRReview, EventMerge:
		key := fmt.Sprintf("/helix/agents/%s/decisions/%s", e.Agent, slug)
		return resolvedNamespace{root: "agents", sub: "decisions"}, key
	case EventGateFailure:
		key := fmt.Sprintf("/helix/agents/%s/anti-patterns/%s", e.Agent, slug)
		return resolvedNamespace{root: "agents", sub: "anti-patterns"}, key
	case EventPromptRewrite, EventDecision:
		key := fmt.Sprintf("/helix/agents/%s/decisions/%s", e.Agent, slug)
		return resolvedNamespace{root: "agents", sub: "decisions"}, key
	default:
		key := fmt.Sprintf("/helix/agents/%s/preferences/%s", e.Agent, slug)
		return resolvedNamespace{root: "agents", sub: "preferences"}, key
	}
}

// entrySlug produces a stable filesystem-safe slug for an entry. We use the
// timestamp in YYYYMMDD format plus the first 16 chars of the summary.
func entrySlug(e CompiledEntry) string {
	date := e.Timestamp.UTC().Format("20060102")
	summary := strings.ToLower(strings.TrimSpace(e.Summary))
	// Take only words from the summary up to a reasonable length.
	summary = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-' || r == '_':
			return r
		}
		return '-'
	}, summary)
	if len(summary) > 48 {
		summary = summary[:48]
	}
	summary = strings.Trim(summary, "-")
	if summary == "" {
		summary = "event"
	}
	return fmt.Sprintf("%s-%s", date, summary)
}

// ─────────────────────────────────────────────────────────────────────────────
// Lifecycle writer — assembles the full pipeline
// ─────────────────────────────────────────────────────────────────────────────

// Lifecycle composes the Inbox → Compiler → PersistenceBridge → Index chain
// and runs them as a single Run call. Returned compiled entries are also
// available via Compiled for inspection.
type Lifecycle struct {
	Inbox    *Inbox
	Compiler *Compiler
	Bridge   *PersistenceBridge
	Store    MemoryStore

	mu        sync.Mutex
	compiled  []CompiledEntry
	indexedNS []Namespace
}

// NewLifecycle returns a lifecycle with all stages wired.
func NewLifecycle(in *Inbox, store MemoryStore) *Lifecycle {
	if in == nil {
		in = NewInbox(0)
	}
	if store == nil {
		store = NewMemStore()
	}
	return &Lifecycle{
		Inbox:    in,
		Compiler: NewCompiler(in),
		Bridge:   NewPersistenceBridge(store),
		Store:    store,
	}
}

// Run executes the full pipeline: batch and compile the inbox, persist each
// compiled entry via the bridge, then build an index for every namespace
// touched by the run.
//
// Returns the persisted entries (in compilation order) and the indexes
// produced.
func (l *Lifecycle) Run(now time.Time) ([]MemoryEntry, []Index, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	compiled := l.Compiler.Compile()
	persisted := make([]MemoryEntry, 0, len(compiled))
	namespaces := make(map[Namespace]struct{})
	for _, entry := range compiled {
		memEntry, err := l.Bridge.CompileAndPersist(entry)
		if err != nil {
			return persisted, nil, fmt.Errorf("lifecycle: persist %s: %w", entry.ID, err)
		}
		persisted = append(persisted, memEntry)
		ns, err := NamespaceOf(memEntry.Key)
		if err == nil {
			namespaces[ns] = struct{}{}
		}
	}
	indexes := make([]Index, 0, len(namespaces))
	for ns := range namespaces {
		idx, err := BuildIndex(l.Store, ns, now)
		if err != nil {
			return persisted, indexes, fmt.Errorf("lifecycle: build index %s: %w", ns, err)
		}
		indexes = append(indexes, idx)
	}
	sort.Slice(indexes, func(i, j int) bool { return indexes[i].Namespace < indexes[j].Namespace })
	l.mu.Lock()
	l.compiled = compiled
	for _, idx := range indexes {
		l.indexedNS = append(l.indexedNS, idx.Namespace)
	}
	l.mu.Unlock()
	return persisted, indexes, nil
}

// Compiled returns the entries produced by the most recent Run. Useful in
// tests and CLI integrations.
func (l *Lifecycle) Compiled() []CompiledEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]CompiledEntry, len(l.compiled))
	copy(out, l.compiled)
	return out
}
