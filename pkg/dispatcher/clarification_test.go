package dispatcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewClarificationRequest(t *testing.T) {
	req := NewClarificationRequest("task-001", 2, "which validation approach?",
		ClarificationContext{
			SpecSection:  "Phase 1: Login form",
			RelevantCode: "src/utils/email.ts",
			RelevantADR:  "ADR-007",
		},
	)
	if req.Type != "CLARIFICATION_NEEDED" {
		t.Fatalf("expected type CLARIFICATION_NEEDED, got %q", req.Type)
	}
	if req.TaskID != "task-001" {
		t.Fatalf("expected task-001, got %q", req.TaskID)
	}
	if req.BlockedStep != 2 {
		t.Fatalf("expected blocked step 2, got %d", req.BlockedStep)
	}
	if req.Context.SpecSectionHash == "" {
		t.Fatal("spec section hash should be populated")
	}
	if req.BlockedSince == "" {
		t.Fatal("BlockedSince should be set")
	}
	// Verify ISO8601 format.
	if _, err := time.Parse(time.RFC3339, req.BlockedSince); err != nil {
		t.Fatalf("BlockedSince is not valid RFC3339: %v", err)
	}
}

func TestClarificationStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	cs := NewClarificationStore(dir)

	req := NewClarificationRequest("task-001", 1, "test question?", ClarificationContext{})
	rec := &ClarificationRecord{
		Request:   *req,
		Status:    "pending",
		CreatedAt: req.BlockedSince,
	}
	if err := cs.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := cs.Load("task-001")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Status != "pending" {
		t.Fatalf("expected pending, got %q", loaded.Status)
	}
	if loaded.Request.TaskID != "task-001" {
		t.Fatalf("expected task-001, got %q", loaded.Request.TaskID)
	}
}

func TestClarificationStore_LoadNotFound(t *testing.T) {
	dir := t.TempDir()
	cs := NewClarificationStore(dir)

	_, err := cs.Load("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if !os.IsNotExist(err) {
		t.Fatalf("expected os.ErrNotExist, got: %v", err)
	}
}

func TestClarificationStore_Resolve(t *testing.T) {
	dir := t.TempDir()
	cs := NewClarificationStore(dir)

	req := NewClarificationRequest("task-002", 3, "which library?", ClarificationContext{
		RelevantADR: "ADR-003",
	})
	resp := NewClarificationResponse("task-002", "use library X", "human:kara")

	rec, err := cs.Resolve(req, resp)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if rec.Status != "resolved" {
		t.Fatalf("expected resolved, got %q", rec.Status)
	}
	if rec.Response.Resolution != "use library X" {
		t.Fatalf("expected 'use library X', got %q", rec.Response.Resolution)
	}
	if rec.Response.ResolvedBy != "human:kara" {
		t.Fatalf("expected 'human:kara', got %q", rec.Response.ResolvedBy)
	}
}

func TestClarificationStore_List(t *testing.T) {
	dir := t.TempDir()
	cs := NewClarificationStore(dir)

	// Save two pending records.
	for _, taskID := range []string{"task-a", "task-b"} {
		req := NewClarificationRequest(taskID, 1, "question", ClarificationContext{})
		rec := &ClarificationRecord{Request: *req, Status: "pending", CreatedAt: req.BlockedSince}
		if err := cs.Save(rec); err != nil {
			t.Fatalf("Save %s: %v", taskID, err)
		}
	}

	// List all.
	all, err := cs.List(ClarificationFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 records, got %d", len(all))
	}

	// List only pending.
	pending, err := cs.List(ClarificationFilter{Status: "pending"})
	if err != nil {
		t.Fatalf("List(pending): %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}

	// Resolve task-a.
	reqA := NewClarificationRequest("task-a", 1, "question", ClarificationContext{})
	respA := NewClarificationResponse("task-a", "answer", "human:kara")
	if _, err := cs.Resolve(reqA, respA); err != nil {
		t.Fatalf("Resolve task-a: %v", err)
	}

	// List only resolved.
	resolved, err := cs.List(ClarificationFilter{Status: "resolved"})
	if err != nil {
		t.Fatalf("List(resolved): %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved, got %d", len(resolved))
	}

	// List pending again — should be 1 now.
	pending, err = cs.List(ClarificationFilter{Status: "pending"})
	if err != nil {
		t.Fatalf("List(pending): %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending after resolve, got %d", len(pending))
	}
}

func TestClarificationStore_ListFilterByAgent(t *testing.T) {
	dir := t.TempDir()
	cs := NewClarificationStore(dir)

	// Resolve two tasks by different agents.
	for i, pair := range []struct{ task, agent string }{
		{"task-1", "human:kara"},
		{"task-2", "human:bane"},
	} {
		req := NewClarificationRequest(pair.task, i+1, "q", ClarificationContext{})
		// Save as pending first.
		cs.Save(&ClarificationRecord{Request: *req, Status: "pending", CreatedAt: req.BlockedSince})
		resp := NewClarificationResponse(pair.task, "a", pair.agent)
		cs.Resolve(req, resp)
	}

	results, err := cs.List(ClarificationFilter{AgentName: "kara"})
	if err != nil {
		t.Fatalf("List(kara): %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for kara, got %d", len(results))
	}
}

func TestClarificationStore_Delete(t *testing.T) {
	dir := t.TempDir()
	cs := NewClarificationStore(dir)

	req := NewClarificationRequest("task-del", 1, "q", ClarificationContext{})
	cs.Save(&ClarificationRecord{Request: *req, Status: "pending", CreatedAt: req.BlockedSince})

	if err := cs.Delete("task-del"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := cs.Load("task-del")
	if !os.IsNotExist(err) {
		t.Fatalf("expected os.ErrNotExist after delete, got: %v", err)
	}

	// Delete nonexistent is no-op.
	if err := cs.Delete("nonexistent"); err != nil {
		t.Fatalf("Delete nonexistent: %v", err)
	}
}

func TestNewClarificationError(t *testing.T) {
	req := NewClarificationRequest("task-001", 3, "which library?", ClarificationContext{})
	err := NewClarificationError(req)

	cc, ok := IsClarificationRequest(err)
	if !ok {
		t.Fatal("expected IsClarificationRequest to return true")
	}
	if cc.TaskID != "task-001" {
		t.Fatalf("expected task-001, got %q", cc.TaskID)
	}
	if cc.BlockedStep != 3 {
		t.Fatalf("expected step 3, got %d", cc.BlockedStep)
	}
}

func TestIsClarificationRequest_Nil(t *testing.T) {
	_, ok := IsClarificationRequest(nil)
	if ok {
		t.Fatal("expected false for nil error")
	}
}

func TestIsClarificationRequest_RegularError(t *testing.T) {
	_, ok := IsClarificationRequest(os.ErrNotExist)
	if ok {
		t.Fatal("expected false for regular error")
	}
}

func TestAutoResolve_ExistingResolution(t *testing.T) {
	dir := t.TempDir()
	cs := NewClarificationStore(dir)

	req := NewClarificationRequest("task-001", 1,
		"which validation approach should we use?",
		ClarificationContext{RelevantADR: "ADR-007"},
	)
	resp := NewClarificationResponse("task-001", "Use HTML5 constraint validation.", "human:kara")
	cs.Resolve(req, resp)

	// Same question, slightly reworded.
	req2 := NewClarificationRequest("task-001", 2,
		"which validation approach?",
		ClarificationContext{RelevantADR: "ADR-007"},
	)
	answer, ok := AutoResolve(*req2, cs, nil)
	if !ok {
		t.Fatal("expected auto-resolve from existing resolution")
	}
	if !strings.Contains(answer, "HTML5") {
		t.Fatalf("expected HTML5 in answer, got %q", answer)
	}
}

func TestAutoResolve_NoMatch(t *testing.T) {
	dir := t.TempDir()
	cs := NewClarificationStore(dir)

	req := NewClarificationRequest("task-new", 1,
		"completely novel question never seen before?",
		ClarificationContext{},
	)
	_, ok := AutoResolve(*req, cs, nil)
	if ok {
		t.Fatal("expected no auto-resolve for novel question")
	}
}

type stubADRStore struct {
	entries map[string]string
}

func (s *stubADRStore) Search(query string) (string, bool) {
	for k, v := range s.entries {
		if strings.Contains(strings.ToLower(query), strings.ToLower(k)) {
			return v, true
		}
	}
	return "", false
}

func TestAutoResolve_ADRMatch(t *testing.T) {
	dir := t.TempDir()
	cs := NewClarificationStore(dir)

	adrStore := &stubADRStore{
		entries: map[string]string{
			"validation": "Use HTML5 constraint validation per ADR-007.",
		},
	}

	req := NewClarificationRequest("task-001", 1,
		"which validation approach?",
		ClarificationContext{RelevantADR: "validation"},
	)
	answer, ok := AutoResolve(*req, cs, adrStore)
	if !ok {
		t.Fatal("expected auto-resolve from ADR match")
	}
	if !strings.Contains(answer, "constraint validation") {
		t.Fatalf("expected constraint validation in answer, got %q", answer)
	}
}

func TestFuzzyMatch(t *testing.T) {
	tests := []struct {
		a, b  string
		match bool
	}{
		{"which validation approach should we use?", "which validation approach?", true},
		{"validate email with constraint validation", "email validation approach with constraint", true},
		{"use regex or HTML5?", "which database to choose?", false},
		{"", "something", false},
		{"short", "different", false},
	}
	for _, tt := range tests {
		got := fuzzyMatch(tt.a, tt.b)
		if got != tt.match {
			t.Errorf("fuzzyMatch(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.match)
		}
	}
}

func TestSanitizeTaskID(t *testing.T) {
	tests := []struct{ in, out string }{
		{"task-001", "task-001"},
		{"specs/feature-login.md:task-1", "specs-feature-login.md-task-1"},
		{"task 001", "task_001"},
	}
	for _, tt := range tests {
		got := sanitizeTaskID(tt.in)
		if got != tt.out {
			t.Errorf("sanitizeTaskID(%q) = %q, want %q", tt.in, got, tt.out)
		}
	}
}

func TestClarificationStore_ListEmptyDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nonexistent")
	cs := NewClarificationStore(dir)

	results, err := cs.List(ClarificationFilter{})
	if err != nil {
		t.Fatalf("List on empty dir: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}
