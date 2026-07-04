// Package memory implements DuckBrain and Hivemind memory schema types and
// the Hivemind memory bank lifecycle. It encodes the spec §8.5 (DuckBrain
// Memory Schema) and §8.6 (Hivemind Memory Bank Lifecycle) contracts as Go
// types with full validation, supersession chain tracking, and a pluggable
// MemoryStore interface for write/query/delete operations.
//
// The package is pure: no I/O is performed directly by this code. Persisting
// entries is the responsibility of callers implementing MemoryStore (typically
// backed by git, an MCP client, or an embedded DuckDB).
package memory

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Domain enumeration (spec §8.5)
// ─────────────────────────────────────────────────────────────────────────────

// Domain categorizes a memory entry by its primary role. The five domains are
// taken verbatim from the spec.
type Domain string

const (
	// DomainConcept covers reusable knowledge (decisions, architectures, runbooks).
	DomainConcept Domain = "concept"
	// DomainEvent covers time-stamped occurrences (incidents, merges, rollbacks).
	DomainEvent Domain = "event"
	// DomainMessage covers inter-agent communication or hand-off notes.
	DomainMessage Domain = "message"
	// DomainRawNote covers unprocessed inbox entries awaiting compilation.
	DomainRawNote Domain = "raw_note"
	// DomainConfig covers platform configuration snapshots.
	DomainConfig Domain = "config"
)

// AllDomains returns every supported domain in canonical order.
func AllDomains() []Domain {
	return []Domain{DomainConcept, DomainEvent, DomainMessage, DomainRawNote, DomainConfig}
}

// Valid reports whether d is one of the recognized domains.
func (d Domain) Valid() bool {
	switch d {
	case DomainConcept, DomainEvent, DomainMessage, DomainRawNote, DomainConfig:
		return true
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Namespaces (spec §8.5)
// ─────────────────────────────────────────────────────────────────────────────

// Namespace enumerates the platform's logical memory regions. The Helix memory
// layout uses /helix/ as the root, with three top-level groupings
// (agents/, repos/, platform/) and named subtrees.
type Namespace string

const (
	NSAgentsDecisions    Namespace = "agents/decisions"
	NSAgentsAntiPatterns Namespace = "agents/anti-patterns"
	NSAgentsPreferences  Namespace = "agents/preferences"
	NSReposConventions   Namespace = "repos/conventions"
	NSReposKnownIssues   Namespace = "repos/known-issues"
	NSReposArchitecture  Namespace = "repos/architecture"
	NSPlatformIncidents  Namespace = "platform/incidents"
	NSPlatformRunbooks   Namespace = "platform/runbooks"
	NSPlatformConfig     Namespace = "platform/config"
)

// AllNamespaces returns the canonical list of supported namespaces.
func AllNamespaces() []Namespace {
	return []Namespace{
		NSAgentsDecisions,
		NSAgentsAntiPatterns,
		NSAgentsPreferences,
		NSReposConventions,
		NSReposKnownIssues,
		NSReposArchitecture,
		NSPlatformIncidents,
		NSPlatformRunbooks,
		NSPlatformConfig,
	}
}

// ValidNamespace reports whether the supplied Namespace is a recognized
// region of the platform memory.
func ValidNamespace(ns Namespace) bool {
	for _, n := range AllNamespaces() {
		if n == ns {
			return true
		}
	}
	return false
}

// validSubNamespace reports whether leaf is a recognized sub-namespace under
// any of the three roots (agents, repos, platform). Sub-namespaces are the
// leaf segment of the path (e.g. "decisions", not "agents/decisions").
func validSubNamespace(leaf string) bool {
	switch leaf {
	case "decisions", "anti-patterns", "preferences",
		"conventions", "known-issues", "architecture",
		"incidents", "runbooks", "config":
		return true
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Attributes — supersession chain + decision metadata
// ─────────────────────────────────────────────────────────────────────────────

// Attributes holds the structured fields attached to every MemoryEntry. All
// fields are optional; most consumers will populate decision/rationale but
// leave the supersession pair empty.
type Attributes struct {
	// Decision is the single-sentence statement of what was decided (for
	// decisions) or what happened (for events / raw notes).
	Decision string `json:"decision,omitempty"`
	// Rationale is the longer explanation backing the Decision.
	Rationale string `json:"rationale,omitempty"`
	// Tradeoffs lists the alternative approaches considered and rejected.
	Tradeoffs []string `json:"tradeoffs,omitempty"`
	// Supersedes is the key of the entry this entry replaces. Mutual with
	// SupersededBy: if A.Supersedes == B.Key then B.SupersededBy == A.Key.
	Supersedes string `json:"supersedes,omitempty"`
	// SupersededBy is the key of the entry that has replaced this one.
	SupersededBy string `json:"superseded_by,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// MemoryEntry (spec §8.5)
// ─────────────────────────────────────────────────────────────────────────────

// MemoryEntry is the canonical DuckBrain memory record. Validation enforces
// the spec's key naming contract and the embedded embedding_text.
type MemoryEntry struct {
	Key           string     `json:"key"`
	Domain        Domain     `json:"domain"`
	Attributes    Attributes `json:"attributes"`
	EmbeddingText string     `json:"embedding_text"`
	CreatedAt     time.Time  `json:"created_at,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Key validation
// ─────────────────────────────────────────────────────────────────────────────

// ValidAgentUUID is a permissive UUIDv4-style pattern. Used for the agents/<uuid>
// path segment. We accept any reasonable agent identifier including UUID,
// short id, or repository-friendly slug.
var (
	keyPathPattern = regexp.MustCompile(`^/helix/([a-zA-Z0-9_\-.]{1,64})/([a-zA-Z0-9_\-./]{1,256})$`)
	agentUUIDLike  = regexp.MustCompile(`^[a-zA-Z0-9_\-.]{1,64}$`)
)

// ValidateKey returns nil if key is well-formed for the Helix memory layout.
// Rules:
//   - Must start with "/helix/"
//   - All path segments must be filesystem-safe
//   - The second segment must be a single identifier (agent UUID or repo name
//     or the literal "platform")
//   - For agent and repo roots the next segment must be one of the
//     recognized sub-namespaces.
func ValidateKey(key string) error {
	if key == "" {
		return errors.New("memory: empty key")
	}
	if !strings.HasPrefix(key, "/helix/") {
		return fmt.Errorf("memory: key %q must start with /helix/", key)
	}
	m := keyPathPattern.FindStringSubmatch(key)
	if m == nil {
		return fmt.Errorf("memory: key %q is not a valid /helix/<root>/<rest> path", key)
	}
	root := m[1]
	if !agentUUIDLike.MatchString(root) {
		return fmt.Errorf("memory: key %q has invalid root segment %q", key, root)
	}
	rest := strings.Trim(m[2], "/")
	segments := strings.Split(rest, "/")
	if len(segments) < 2 {
		return fmt.Errorf("memory: key %q must have a namespace subtree under %q", key, root)
	}
	// Reject path traversal: no segment may be "." or ".." or contain
	// backslashes / NUL.
	for _, seg := range segments {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("memory: key %q contains disallowed segment %q", key, seg)
		}
		if strings.ContainsAny(seg, "\\\x00") {
			return fmt.Errorf("memory: key %q contains disallowed characters in segment %q", key, seg)
		}
	}
	// For agents/<agent_id>/<sub>/... the sub-namespace is in position 1
	// (the <agent_id> lives between "agents" and the sub-namespace).
	// For repos/<repo>/<sub>/... the same applies.
	// For platform/<sub>/... the <sub> is directly at position 0.
	var wantNS Namespace
	switch root {
	case "platform":
		wantNS = Namespace(segments[0])
		if !validSubNamespace(string(wantNS)) {
			return fmt.Errorf("memory: key %q: platform namespace %q is not recognized", key, wantNS)
		}
	default: // "agents" (UUID-ish) and "repos"
		if len(segments) < 2 {
			return fmt.Errorf("memory: key %q must have <id>/<namespace> structure", key)
		}
		wantNS = Namespace(segments[1])
		if !validSubNamespace(string(wantNS)) {
			return fmt.Errorf("memory: key %q: %s namespace %q is not recognized", key, root, wantNS)
		}
	}
	return nil
}

// NamespaceOf extracts the canonical Namespace from a key. For agent and
// repo roots the returned Namespace uses the two-segment canonical form
// (e.g. "agents/decisions", "repos/conventions", "platform/incidents").
func NamespaceOf(key string) (Namespace, error) {
	if err := ValidateKey(key); err != nil {
		return "", err
	}
	trimmed := strings.TrimPrefix(key, "/helix/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) < 2 {
		return "", fmt.Errorf("memory: key %q has no namespace segment", key)
	}
	root := parts[0]
	rest := parts[1]
	// For platform keys the sub-namespace is the first segment after /helix/.
	// For agents/repos the agent/repo id is first, the sub-namespace second.
	segments := strings.Split(rest, "/")
	switch root {
	case "platform":
		if len(segments) == 0 || segments[0] == "" {
			return "", fmt.Errorf("memory: key %q has no namespace segment", key)
		}
		return Namespace("platform/" + segments[0]), nil
	default:
		// "agents" or "repos". Sub-namespace lives at index 1.
		if len(segments) < 2 {
			return "", fmt.Errorf("memory: key %q has no sub-namespace segment", key)
		}
		return Namespace(root + "/" + segments[1]), nil
	}
}

// RootOf returns the top-level grouping (agent ID, repo name, or "platform").
func RootOf(key string) (string, error) {
	if err := ValidateKey(key); err != nil {
		return "", err
	}
	trimmed := strings.TrimPrefix(key, "/helix/")
	parts := strings.SplitN(trimmed, "/", 2)
	return parts[0], nil
}

// ─────────────────────────────────────────────────────────────────────────────
// MemoryStore interface
// ─────────────────────────────────────────────────────────────────────────────

// MemoryStore is the persistence contract for MemoryEntries. Implementations
// may back onto git, an MCP client, an embedded DB, or any combination thereof.
type MemoryStore interface {
	// Write persists entry. Implementations must return ErrAlreadyExists when
	// an entry with the same Key already exists and overwrite is false.
	Write(entry MemoryEntry, overwrite bool) error
	// Read returns the entry with the given key, or ErrNotFound.
	Read(key string) (MemoryEntry, error)
	// Delete removes the entry with the given key, or returns ErrNotFound.
	Delete(key string) error
	// Query returns matching entries. An empty query returns all entries.
	Query(q MemoryQuery) ([]MemoryEntry, error)
}

// ─────────────────────────────────────────────────────────────────────────────
// Query
// ─────────────────────────────────────────────────────────────────────────────

// MemoryQuery supports namespace-scoped retrieval with optional domain and
// agent/repo filter. All filters are AND-ed together.
type MemoryQuery struct {
	KeyPrefix string    // restrict to keys starting with this prefix
	Namespace Namespace `json:"namespace,omitempty"`
	Domains   []Domain  `json:"domains,omitempty"` // empty = any
	Limit     int       `json:"limit,omitempty"`   // 0 = unlimited
}

// ─────────────────────────────────────────────────────────────────────────────
// Sentinel errors
// ─────────────────────────────────────────────────────────────────────────────

var (
	// ErrNotFound is returned by MemoryStore.Read/Delete when the key is absent.
	ErrNotFound = errors.New("memory: entry not found")
	// ErrAlreadyExists is returned when writing a non-overwrite duplicate.
	ErrAlreadyExists = errors.New("memory: entry already exists")
	// ErrInvalidEntry is returned when validation fails.
	ErrInvalidEntry = errors.New("memory: invalid entry")
	// ErrSupersessionConflict is returned when an entry's supersession pointers
	// form a cycle (Supersedes points back through SupersededBy chain to self).
	ErrSupersessionConflict = errors.New("memory: supersession pointers form a cycle")
)

// Validate ensures the entry conforms to the spec requirements: domain is
// recognized, key is well-formed, embedding text is non-empty (required for
// downstream vector search), and supersession state is consistent.
func (e MemoryEntry) Validate() error {
	if err := ValidateKey(e.Key); err != nil {
		return err
	}
	if !e.Domain.Valid() {
		return fmt.Errorf("%w: domain %q", ErrInvalidEntry, e.Domain)
	}
	if strings.TrimSpace(e.EmbeddingText) == "" {
		return fmt.Errorf("%w: embedding_text required", ErrInvalidEntry)
	}
	// Self-reference is always a conflict (cannot supersede yourself and
	// cannot be superseded by yourself).
	if e.Attributes.Supersedes == e.Key {
		return ErrSupersessionConflict
	}
	if e.Attributes.SupersededBy == e.Key {
		return ErrSupersessionConflict
	}
	// Cross-references between Supersedes and SupersededBy cannot equal each
	// other (a 2-node cycle).
	if e.Attributes.Supersedes != "" && e.Attributes.SupersededBy != "" {
		if e.Attributes.Supersedes == e.Attributes.SupersededBy {
			return ErrSupersessionConflict
		}
	}
	if e.Attributes.Supersedes != "" {
		if err := ValidateKey(e.Attributes.Supersedes); err != nil {
			return fmt.Errorf("%w: supersedes %v", ErrInvalidEntry, err)
		}
	}
	if e.Attributes.SupersededBy != "" {
		if err := ValidateKey(e.Attributes.SupersededBy); err != nil {
			return fmt.Errorf("%w: superseded_by %v", ErrInvalidEntry, err)
		}
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Supersession chain helpers
// ─────────────────────────────────────────────────────────────────────────────

// SupersessionChain walks forward from a starting entry following SupersededBy
// pointers, returning the path from the original key to the newest entry.
// Returns a slice containing the input key first, then each successor.
//
// NOTE: This requires a MemoryStore to look up each successor; pass it in.
func SupersessionChain(store MemoryStore, startKey string) ([]MemoryEntry, error) {
	if store == nil {
		return nil, errors.New("memory: nil store in SupersessionChain")
	}
	if err := ValidateKey(startKey); err != nil {
		return nil, err
	}
	chain := make([]MemoryEntry, 0, 4)
	current, err := store.Read(startKey)
	if err != nil {
		return nil, err
	}
	chain = append(chain, current)
	seen := map[string]bool{current.Key: true}
	for current.Attributes.SupersededBy != "" {
		nextKey := current.Attributes.SupersededBy
		if seen[nextKey] {
			return chain, fmt.Errorf("memory: supersession cycle detected at %s", nextKey)
		}
		next, err := store.Read(nextKey)
		if err != nil {
			return chain, err
		}
		chain = append(chain, next)
		seen[nextKey] = true
		current = next
	}
	return chain, nil
}

// ApplySupersession sets the new entry's Supersedes field to previousKey and
// updates previousKey's SupersededBy field to newKey. Both writes are passed
// to store.Write with overwrite permitted. This is the recommended way to
// replace an outdated memory entry.
func ApplySupersession(store MemoryStore, previousKey, newKey string) error {
	if store == nil {
		return errors.New("memory: nil store in ApplySupersession")
	}
	prev, err := store.Read(previousKey)
	if err != nil {
		return err
	}
	prev.Attributes.SupersededBy = newKey
	if err := store.Write(prev, true); err != nil {
		return err
	}
	newer, err := store.Read(newKey)
	if err != nil {
		return err
	}
	newer.Attributes.Supersedes = previousKey
	return store.Write(newer, true)
}

// ─────────────────────────────────────────────────────────────────────────────
// In-memory store (for tests and ephemeral use)
// ─────────────────────────────────────────────────────────────────────────────

// MemStore is a goroutine-safe, in-memory MemoryStore. Suitable for unit
// tests and short-lived CLI runs that do not need persistent storage. Long
// term storage should use a git-backed or DuckDB-backed implementation.
type MemStore struct {
	mu      sync.RWMutex
	entries map[string]MemoryEntry
}

// NewMemStore returns an initialized empty MemStore.
func NewMemStore() *MemStore {
	return &MemStore{entries: make(map[string]MemoryEntry)}
}

// Write stores the entry. Returns ErrAlreadyExists if the key is taken and
// overwrite is false. Performs full Validate() of the entry.
func (m *MemStore) Write(entry MemoryEntry, overwrite bool) error {
	if err := entry.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.entries[entry.Key]; exists && !overwrite {
		return ErrAlreadyExists
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	entry.UpdatedAt = time.Now().UTC()
	m.entries[entry.Key] = entry
	return nil
}

// Read returns the entry for key or ErrNotFound.
func (m *MemStore) Read(key string) (MemoryEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.entries[key]
	if !ok {
		return MemoryEntry{}, ErrNotFound
	}
	return e, nil
}

// Delete removes the entry. Returns ErrNotFound if absent.
func (m *MemStore) Delete(key string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.entries[key]; !ok {
		return ErrNotFound
	}
	delete(m.entries, key)
	return nil
}

// Query returns entries matching q, ordered by key for determinism.
func (m *MemStore) Query(q MemoryQuery) ([]MemoryEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]MemoryEntry, 0)
	domainSet := make(map[Domain]bool, len(q.Domains))
	for _, d := range q.Domains {
		domainSet[d] = true
	}
	for _, e := range m.entries {
		if q.KeyPrefix != "" && !strings.HasPrefix(e.Key, q.KeyPrefix) {
			continue
		}
		if q.Namespace != "" {
			ns, err := NamespaceOf(e.Key)
			if err != nil || ns != q.Namespace {
				continue
			}
		}
		if len(domainSet) > 0 && !domainSet[e.Domain] {
			continue
		}
		out = append(out, e)
	}
	// Sort by key for deterministic ordering.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].Key > out[j].Key; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

// Count reports the number of entries currently in the store.
func (m *MemStore) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}

// ─────────────────────────────────────────────────────────────────────────────
// Display helpers
// ─────────────────────────────────────────────────────────────────────────────

// Path returns a clean, slash-normalized representation of key suitable for
// filesystem use. Useful for callers that back the store with a git tree.
func Path(key string) string {
	return path.Clean(key)
}

// Summary returns a one-line summary of the entry suitable for CLI output.
func (e MemoryEntry) Summary() string {
	dec := e.Attributes.Decision
	if dec == "" {
		dec = string(e.Domain)
	}
	if len(dec) > 64 {
		dec = dec[:61] + "..."
	}
	return fmt.Sprintf("%s [%s] %s", e.Key, e.Domain, dec)
}
