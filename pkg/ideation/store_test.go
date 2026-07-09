package ideation

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCaptureGetListRoundtrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "ideas.jsonl"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	idea := &Idea{
		Title:  "Add rate limiting",
		Body:   "Protect login endpoints from brute force with token bucket",
		Tags:   []string{"auth", "security"},
		Source: SourceHuman,
		Evidence: []EvidenceRef{
			{Type: EvidenceIncident, Ref: "inc-1", Description: "login spam"},
		},
	}
	if err := Capture(store, idea); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if idea.ID == "" {
		t.Fatal("expected ID assigned")
	}
	if idea.Status != StatusDraft {
		t.Fatalf("status = %q, want draft", idea.Status)
	}
	if idea.CreatedAt.IsZero() || idea.UpdatedAt.IsZero() {
		t.Fatal("expected timestamps")
	}

	got, err := store.Get(idea.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != idea.Title {
		t.Fatalf("title = %q, want %q", got.Title, idea.Title)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}
}

func TestCaptureEmptyTitle(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "ideas.jsonl"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Capture(&Idea{Body: "no title"}); err == nil {
		t.Fatal("expected error for empty title")
	}
}

func TestUpdatePromoteCloseAtomic(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "ideas.jsonl"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	idea := &Idea{Title: "Refactor cache", Body: "Improve cache hit rates across services"}
	if err := store.Capture(idea); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	id := idea.ID

	// Update
	idea.Body = "Improved body with more detail on cache layers"
	idea.Tags = []string{"performance"}
	if err := store.Update(idea); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.Body != idea.Body {
		t.Fatalf("body not updated")
	}
	if len(got.Tags) != 1 || got.Tags[0] != "performance" {
		t.Fatalf("tags = %v", got.Tags)
	}

	// Promote (risk must be < 70)
	specPath := "specs/ideas/" + id + "-refactor-cache.md"
	if err := store.Promote(id, specPath); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	got, err = store.Get(id)
	if err != nil {
		t.Fatalf("Get after promote: %v", err)
	}
	if got.Status != StatusPromoted {
		t.Fatalf("status = %q, want promoted", got.Status)
	}
	if got.PromotedTo != specPath {
		t.Fatalf("promoted_to = %q", got.PromotedTo)
	}

	// Close another idea
	idea2 := &Idea{Title: "Deprecated idea", Body: "Will be closed after capture for cleanup"}
	if err := store.Capture(idea2); err != nil {
		t.Fatalf("Capture2: %v", err)
	}
	if err := store.Close(idea2.ID, "duplicate of existing work"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got2, err := store.Get(idea2.ID)
	if err != nil {
		t.Fatalf("Get closed: %v", err)
	}
	if got2.Status != StatusClosed {
		t.Fatalf("status = %q, want closed", got2.Status)
	}
	if got2.ClosedReason == "" {
		t.Fatal("expected closed reason")
	}

	// List should still return both (latest status each)
	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list len = %d, want 2", len(list))
	}
}

func TestPromoteBlockedHighRisk(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "ideas.jsonl"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	idea := &Idea{Title: "Risky rewrite", Body: "Rewrite everything in a big bang migration overnight"}
	if err := store.Capture(idea); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	idea.RiskScore = 75
	if err := store.Update(idea); err != nil {
		t.Fatalf("Update risk: %v", err)
	}
	err = store.Promote(idea.ID, "specs/ideas/x.md")
	if err == nil {
		t.Fatal("expected promote blocked on high risk")
	}
}

func TestNewStoreDefaultPath(t *testing.T) {
	// Empty path should not error (uses home); we only check constructor.
	s, err := NewStore("")
	if err != nil {
		t.Fatalf("NewStore empty: %v", err)
	}
	if s.Path() == "" {
		t.Fatal("expected non-empty default path")
	}
}

func TestNewIdeaID(t *testing.T) {
	a := NewIdeaID()
	b := NewIdeaID()
	if a == "" || b == "" {
		t.Fatal("empty id")
	}
	if a == b {
		t.Fatal("ids should differ")
	}
	// 12 bytes → 24 hex chars
	if len(a) < 16 || len(a) > 32 {
		t.Fatalf("id length %d out of expected range", len(a))
	}
}

func TestListEmptyStore(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "ideas.jsonl"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("want empty list, got %d", len(list))
	}
	_, err = store.Get("missing")
	if err == nil {
		t.Fatal("expected not found")
	}
	_ = time.Now() // keep time import if unused elsewhere
}
