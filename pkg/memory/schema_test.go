package memory

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// helper to build a valid entry quickly.
func validEntry(key string, domain Domain) MemoryEntry {
	return MemoryEntry{
		Key:           key,
		Domain:        domain,
		Attributes:    Attributes{Decision: "test", Rationale: "because"},
		EmbeddingText: "embedding for " + key,
	}
}

func TestValidateKey(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"agent decision ok", "/helix/agents/sandbox-7/decisions/2026-06-19-sqlite-vs-postgres", false},
		{"agent decision nested ok", "/helix/agents/abc123/decisions/foo/bar/baz", false},
		{"platform incidents ok", "/helix/platform/incidents/2026-07-03-foo", false},
		{"repos architecture ok", "/helix/repos/helix/architecture/c4-models", false},
		{"missing prefix", "/decisions/foo", true},
		{"empty", "", true},
		{"bad namespace under platform", "/helix/platform/whatever/foo", true},
		{"bad namespace under repos", "/helix/repos/helix/bad-ns/foo", true},
		{"bad namespace under agents", "/helix/agents/abc/bad-ns/foo", true},
		{"agent only 2 segments (id only)", "/helix/agents/sandbox-7", true},
		{"path traversal", "/helix/agents/abc/decisions/../escape", true},
		{"only root", "/helix", true},
		{"uppercase extension ok", "/helix/agents/abc/decisions/My-Doc-2026", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateKey(tc.key)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q", tc.key)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.key, err)
			}
		})
	}
}

func TestNamespaceOf(t *testing.T) {
	cases := []struct {
		key  string
		want Namespace
	}{
		{"/helix/agents/sandbox-7/decisions/x", NSAgentsDecisions},
		{"/helix/agents/sandbox-7/anti-patterns/x", NSAgentsAntiPatterns},
		{"/helix/agents/sandbox-7/preferences/x", NSAgentsPreferences},
		{"/helix/repos/helix/conventions/x", NSReposConventions},
		{"/helix/repos/helix/known-issues/x", NSReposKnownIssues},
		{"/helix/repos/helix/architecture/x", NSReposArchitecture},
		{"/helix/platform/incidents/x", NSPlatformIncidents},
		{"/helix/platform/runbooks/x", NSPlatformRunbooks},
		{"/helix/platform/config/x", NSPlatformConfig},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			ns, err := NamespaceOf(tc.key)
			if err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if ns != tc.want {
				t.Fatalf("got %s want %s", ns, tc.want)
			}
		})
	}
}

func TestRootOf(t *testing.T) {
	tests := map[string]string{
		"/helix/agents/sandbox-7/decisions/x": "agents",
		"/helix/platform/incidents/2026":      "platform",
		"/helix/repos/helix/architecture/c4":  "repos",
	}
	for k, want := range tests {
		got, err := RootOf(k)
		if err != nil {
			t.Fatalf("unexpected err for %s: %v", k, err)
		}
		if got != want {
			t.Fatalf("got %q want %q for %s", got, want, k)
		}
	}
}

func TestDomainValidation(t *testing.T) {
	for _, d := range AllDomains() {
		if !d.Valid() {
			t.Fatalf("canonical domain %s must validate", d)
		}
	}
	bad := Domain("nonexistent")
	if bad.Valid() {
		t.Fatalf("unknown domain must not validate")
	}
}

func TestEntryValidation(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		e := validEntry("/helix/agents/agent/decisions/foo", DomainConcept)
		if err := e.Validate(); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
	})
	t.Run("bad domain", func(t *testing.T) {
		e := validEntry("/helix/agents/agent/decisions/foo", Domain("lol"))
		if err := e.Validate(); err == nil {
			t.Fatalf("expected error")
		}
	})
	t.Run("empty embedding", func(t *testing.T) {
		e := MemoryEntry{Key: "/helix/agents/agent/decisions/foo", Domain: DomainConcept}
		if err := e.Validate(); err == nil {
			t.Fatalf("expected error for empty embedding")
		}
	})
	t.Run("both pointing to each other is conflict", func(t *testing.T) {
		e := validEntry("/helix/agents/agent/decisions/foo", DomainConcept)
		e.Attributes.Supersedes = "/helix/agents/agent/decisions/old"
		e.Attributes.SupersededBy = "/helix/agents/agent/decisions/old" // same key
		if err := e.Validate(); !errors.Is(err, ErrSupersessionConflict) {
			t.Fatalf("got %v want ErrSupersessionConflict", err)
		}
	})
	t.Run("self-supersede is conflict", func(t *testing.T) {
		e := validEntry("/helix/agents/agent/decisions/foo", DomainConcept)
		e.Attributes.Supersedes = "/helix/agents/agent/decisions/foo" // same as self
		if err := e.Validate(); !errors.Is(err, ErrSupersessionConflict) {
			t.Fatalf("got %v want ErrSupersessionConflict", err)
		}
	})
	t.Run("mid-chain both set is allowed", func(t *testing.T) {
		e := validEntry("/helix/agents/agent/decisions/mid", DomainConcept)
		e.Attributes.Supersedes = "/helix/agents/agent/decisions/old"
		e.Attributes.SupersededBy = "/helix/agents/agent/decisions/new"
		if err := e.Validate(); err != nil {
			t.Fatalf("mid-chain entry should be valid, got %v", err)
		}
	})
	t.Run("bad supersedes key", func(t *testing.T) {
		e := validEntry("/helix/agents/agent/decisions/foo", DomainConcept)
		e.Attributes.Supersedes = "/bad/key"
		if err := e.Validate(); err == nil {
			t.Fatalf("expected error")
		}
	})
}

func TestMemStoreCRUD(t *testing.T) {
	s := NewMemStore()
	e := validEntry("/helix/agents/sandbox-7/decisions/x", DomainConcept)

	if err := s.Write(e, false); err != nil {
		t.Fatalf("write: %v", err)
	}
	if s.Count() != 1 {
		t.Fatalf("count = %d", s.Count())
	}

	// duplicate without overwrite -> error
	if err := s.Write(e, false); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
	if err := s.Write(e, true); err != nil {
		t.Fatalf("overwrite: %v", err)
	}

	got, err := s.Read(e.Key)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Key != e.Key {
		t.Fatalf("got %s want %s", got.Key, e.Key)
	}
	if got.CreatedAt.IsZero() {
		t.Fatalf("CreatedAt must be populated on Write")
	}
}

func TestMemStoreDelete(t *testing.T) {
	s := NewMemStore()
	_ = s.Write(validEntry("/helix/agents/agent/decisions/x", DomainConcept), false)
	if err := s.Delete("/helix/agents/agent/decisions/x"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.Delete("/helix/agents/agent/decisions/x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMemStoreQuery(t *testing.T) {
	s := NewMemStore()
	keys := []string{
		"/helix/agents/sandbox-7/decisions/a",
		"/helix/agents/sandbox-7/anti-patterns/b",
		"/helix/agents/sandbox-7/preferences/c",
		"/helix/platform/incidents/d",
		"/helix/repos/helix/conventions/e",
	}
	for i, k := range keys {
		d := DomainConcept
		if i == 3 {
			d = DomainEvent
		}
		_ = s.Write(validEntry(k, d), false)
	}
	// Filter by namespace.
	q := MemoryQuery{Namespace: NSAgentsDecisions}
	out, err := s.Query(q)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 result, got %d", len(out))
	}
	// Filter by key prefix.
	q2 := MemoryQuery{KeyPrefix: "/helix/agents/sandbox-7/"}
	out2, _ := s.Query(q2)
	if len(out2) != 3 {
		t.Fatalf("expected 3 with prefix, got %d", len(out2))
	}
	// Filter by domain list.
	q3 := MemoryQuery{Domains: []Domain{DomainEvent}}
	out3, _ := s.Query(q3)
	if len(out3) != 1 || out3[0].Key != keys[3] {
		t.Fatalf("expected 1 event, got %d", len(out3))
	}
	// Deterministic order: keys sorted ascending.
	q4 := MemoryQuery{KeyPrefix: "/helix/agents/sandbox-7/"}
	out4, _ := s.Query(q4)
	for i := 1; i < len(out4); i++ {
		if out4[i-1].Key > out4[i].Key {
			t.Fatalf("results must be ordered by key, got %s before %s", out4[i-1].Key, out4[i].Key)
		}
	}
	// Limit honored.
	q5 := MemoryQuery{Limit: 2}
	out5, _ := s.Query(q5)
	if len(out5) != 2 {
		t.Fatalf("expected 2 with limit 2, got %d", len(out5))
	}
}

func TestSupersessionChain(t *testing.T) {
	s := NewMemStore()
	_ = s.Write(validEntry("/helix/agents/agent/decisions/old", DomainConcept), false)
	_ = s.Write(validEntry("/helix/agents/agent/decisions/mid", DomainConcept), false)
	_ = s.Write(validEntry("/helix/agents/agent/decisions/new", DomainConcept), false)
	if err := ApplySupersession(s, "/helix/agents/agent/decisions/old", "/helix/agents/agent/decisions/mid"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := ApplySupersession(s, "/helix/agents/agent/decisions/mid", "/helix/agents/agent/decisions/new"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	chain, err := SupersessionChain(s, "/helix/agents/agent/decisions/old")
	if err != nil {
		t.Fatalf("chain: %v", err)
	}
	if len(chain) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(chain))
	}
	if chain[0].Key != "/helix/agents/agent/decisions/old" || chain[2].Key != "/helix/agents/agent/decisions/new" {
		t.Fatalf("bad chain: %+v", chain)
	}
}

func TestSupersessionCycle(t *testing.T) {
	s := NewMemStore()
	_ = s.Write(validEntry("/helix/agents/agent/decisions/a", DomainConcept), false)
	_ = s.Write(validEntry("/helix/agents/agent/decisions/b", DomainConcept), false)
	a, _ := s.Read("/helix/agents/agent/decisions/a")
	a.Attributes.SupersededBy = "/helix/agents/agent/decisions/b"
	_ = s.Write(a, true)
	b, _ := s.Read("/helix/agents/agent/decisions/b")
	b.Attributes.SupersededBy = "/helix/agents/agent/decisions/a"
	_ = s.Write(b, true)

	_, err := SupersessionChain(s, "/helix/agents/agent/decisions/a")
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle detection, got %v", err)
	}
}

func TestMemStoreConcurrent(t *testing.T) {
	s := NewMemStore()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := "/helix/agents/agent/decisions/concurrent-" + strings.Repeat("x", i%4+1) + "-" + time.Now().Format("150405.000000000")
			_ = s.Write(validEntry(key, DomainConcept), true)
			_, _ = s.Read(key)
		}(i)
	}
	wg.Wait()
	if s.Count() < 1 {
		t.Fatalf("expected at least 1 entry after concurrent writes")
	}
}

func TestEntrySummary(t *testing.T) {
	e := validEntry("/helix/agent/decisions/x", DomainConcept)
	e.Attributes.Decision = strings.Repeat("a", 200)
	if !strings.HasSuffix(e.Summary(), "...") {
		t.Fatalf("expected summary to truncate long decisions")
	}
	if !strings.Contains(e.Summary(), "/helix/agent/decisions/x") {
		t.Fatalf("summary should contain key")
	}
}

func TestAllNamespacesValid(t *testing.T) {
	for _, ns := range AllNamespaces() {
		if !ValidNamespace(ns) {
			t.Fatalf("namespace %s must be valid", ns)
		}
	}
	if ValidNamespace(Namespace("nope")) {
		t.Fatalf("garbage namespace must not be valid")
	}
}

func TestApplySupersessionNilStore(t *testing.T) {
	if err := ApplySupersession(nil, "x", "y"); err == nil {
		t.Fatalf("expected error for nil store")
	}
}

func TestPath(t *testing.T) {
	got := Path("/helix/agent/decisions/../escape")
	if strings.Contains(got, "..") {
		t.Fatalf("Path must clean traversal: got %s", got)
	}
}
