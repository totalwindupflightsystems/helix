package ideation

import (
	"path/filepath"
	"testing"
)

func TestPrioritizeDeterministicRanking(t *testing.T) {
	p := NewIdeaPrioritizer("")
	ideas := []*Idea{
		{
			ID:        "b",
			Title:     "Beta idea",
			Body:      "A reasonably long body for beta prioritization testing.",
			Tags:      []string{"ops"},
			Status:    StatusValidated,
			RiskScore: 30,
		},
		{
			ID:        "a",
			Title:     "Alpha idea",
			Body:      "A reasonably long body for alpha prioritization testing.",
			Tags:      []string{"ops"},
			Status:    StatusValidated,
			RiskScore: 30,
		},
		{
			ID:        "c",
			Title:     "High risk costly",
			Body:      "Another body used to force lower composite score via risk.",
			Tags:      []string{"a", "b", "c", "d", "e", "f", "g", "h"},
			Status:    StatusDraft,
			RiskScore: 80,
			Evidence: []EvidenceRef{
				{Type: "file", Ref: "1"}, {Type: "file", Ref: "2"},
				{Type: "file", Ref: "3"}, {Type: "file", Ref: "4"},
			},
		},
	}

	// Advocacy: favor Alpha.
	if err := p.SubmitAdvocacy("a", AdvocacyRecord{AgentID: "agent-1", Position: PositionFor}); err != nil {
		t.Fatalf("advocacy: %v", err)
	}
	if err := p.SubmitAdvocacy("a", AdvocacyRecord{AgentID: "agent-2", Position: PositionPriority}); err != nil {
		t.Fatalf("advocacy: %v", err)
	}
	if err := p.SubmitAdvocacy("c", AdvocacyRecord{AgentID: "agent-3", Position: PositionAgainst}); err != nil {
		t.Fatalf("advocacy: %v", err)
	}

	r1, err := p.Prioritize(ideas)
	if err != nil {
		t.Fatalf("Prioritize: %v", err)
	}
	r2, err := p.Prioritize(ideas)
	if err != nil {
		t.Fatalf("Prioritize2: %v", err)
	}
	if len(r1.Ideas) != 3 || len(r2.Ideas) != 3 {
		t.Fatalf("len mismatch %d %d", len(r1.Ideas), len(r2.Ideas))
	}
	for i := range r1.Ideas {
		if r1.Ideas[i].ID != r2.Ideas[i].ID {
			t.Fatalf("order nondeterministic: run1=%v run2=%v", ids(r1), ids(r2))
		}
		if r1.Ideas[i].Rank != i+1 {
			t.Fatalf("rank = %d want %d", r1.Ideas[i].Rank, i+1)
		}
		if r1.Ideas[i].Score != r2.Ideas[i].Score {
			t.Fatalf("score nondeterministic")
		}
	}
	// Alpha (advocated) should rank above high-risk C.
	if r1.Ideas[0].ID == "c" {
		t.Fatalf("high-risk idea ranked first: %v", ids(r1))
	}
	// Status should flip to prioritized for draft/validated.
	for _, pi := range r1.Ideas {
		if pi.ID == "c" || pi.ID == "a" || pi.ID == "b" {
			if pi.Status != StatusPrioritized {
				t.Fatalf("id %s status = %q, want prioritized", pi.ID, pi.Status)
			}
		}
	}
}

func TestPrioritizeTieBreakTitleAsc(t *testing.T) {
	p := NewIdeaPrioritizer("")
	ideas := []*Idea{
		{ID: "2", Title: "Zulu", Body: "same body length here!!", Status: StatusValidated, RiskScore: 20},
		{ID: "1", Title: "Alpha", Body: "same body length here!!", Status: StatusValidated, RiskScore: 20},
	}
	r, err := p.Prioritize(ideas)
	if err != nil {
		t.Fatalf("Prioritize: %v", err)
	}
	if r.Ideas[0].Title != "Alpha" {
		t.Fatalf("tie-break title: got %q then %q", r.Ideas[0].Title, r.Ideas[1].Title)
	}
}

func TestEstimateCostRounding(t *testing.T) {
	idea := &Idea{
		Title:  "t",
		Body:   "hello", // len 5
		Tags:   []string{"a", "b"},
		Status: StatusValidated,
	}
	// 0.05 + 0.02*2 + 0.0001*5 + 0 = 0.0905 → ceil cents → 0.10
	cost := EstimateCost(idea)
	if cost != 0.10 {
		t.Fatalf("cost = %v, want 0.10", cost)
	}
	// Draft multiplies by 1.2
	idea.Status = StatusDraft
	cost2 := EstimateCost(idea)
	// 0.0905 * 1.2 = 0.1086 → 0.11
	if cost2 != 0.11 {
		t.Fatalf("draft cost = %v, want 0.11", cost2)
	}
}

func TestSubmitAdvocacyValidation(t *testing.T) {
	p := NewIdeaPrioritizer("")
	if err := p.SubmitAdvocacy("", AdvocacyRecord{AgentID: "a", Position: PositionFor}); err == nil {
		t.Fatal("expected empty idea id error")
	}
	if err := p.SubmitAdvocacy("x", AdvocacyRecord{Position: PositionFor}); err == nil {
		t.Fatal("expected empty agent error")
	}
	if err := p.SubmitAdvocacy("x", AdvocacyRecord{AgentID: "a", Position: "maybe"}); err == nil {
		t.Fatal("expected invalid position")
	}
}

func TestAdvocacyPersistence(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "ideas.jsonl")
	p1 := NewIdeaPrioritizer(storePath)
	if err := p1.SubmitAdvocacy("id1", AdvocacyRecord{AgentID: "agent-a", Position: PositionFor}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	p2 := NewIdeaPrioritizer(storePath)
	recs := p2.AdvocacyFor("id1")
	if len(recs) != 1 || recs[0].AgentID != "agent-a" {
		t.Fatalf("persisted advocacy = %+v", recs)
	}
}

func ids(r *Roadmap) []string {
	out := make([]string, len(r.Ideas))
	for i, pi := range r.Ideas {
		out[i] = pi.ID
	}
	return out
}
