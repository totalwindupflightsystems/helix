package marketplace

import (
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// TestRate
// ---------------------------------------------------------------------------

func TestRate_Success(t *testing.T) {
	r := &Registry{agents: map[string]*Agent{
		"gopher": {
			Name:    "gopher",
			Status:  StatusActive,
			Reviews: nil,
			Ratings: Ratings{},
		},
	}}

	err := r.Rate("gopher", "alice", 4, "solid work")
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	a := r.agents["gopher"]
	if len(a.Reviews) != 1 {
		t.Fatalf("expected 1 review, got %d", len(a.Reviews))
	}
	rv := a.Reviews[0]
	if rv.Author != "alice" {
		t.Errorf("author = %q, want %q", rv.Author, "alice")
	}
	if rv.Rating != 4 {
		t.Errorf("rating = %d, want 4", rv.Rating)
	}
	if rv.Comment != "solid work" {
		t.Errorf("comment = %q, want %q", rv.Comment, "solid work")
	}
	if rv.Date == "" {
		t.Error("date should not be empty")
	}
	if a.Ratings.Average != 4.0 {
		t.Errorf("average = %f, want 4.0", a.Ratings.Average)
	}
	if a.Ratings.Count != 1 {
		t.Errorf("count = %d, want 1", a.Ratings.Count)
	}
	if a.UpdatedAt == "" {
		t.Error("UpdatedAt should be set by recalcRatingAverage")
	}
}

func TestRate_InvalidRating(t *testing.T) {
	tests := []struct {
		name   string
		rating int
	}{
		{"below range", 0},
		{"negative", -1},
		{"above range", 6},
		{"way above range", 999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Registry{agents: map[string]*Agent{
				"gopher": {Name: "gopher", Status: StatusActive},
			}}
			err := r.Rate("gopher", "alice", tt.rating, "bad")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			var exitErr *ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("expected ExitError, got %T: %v", err, err)
			}
			if exitErr.Code != ExitInvalidRating {
				t.Errorf("code = %d, want ExitInvalidRating (%d)", exitErr.Code, ExitInvalidRating)
			}
		})
	}
}

func TestRate_Unauthorized(t *testing.T) {
	r := &Registry{agents: map[string]*Agent{
		"gopher": {Name: "gopher", Status: StatusActive},
	}}

	err := r.Rate("gopher", "agent-minimax", 5, "great")
	if err == nil {
		t.Fatal("expected error for agent-minimax (non-human), got nil")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %T", err)
	}
	if exitErr.Code != ExitUnauthorized {
		t.Errorf("code = %d, want ExitUnauthorized (%d)", exitErr.Code, ExitUnauthorized)
	}
}

func TestRate_AgentNotFound(t *testing.T) {
	r := &Registry{agents: map[string]*Agent{}}

	err := r.Rate("nonexistent", "alice", 3, "ok")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %T", err)
	}
	if exitErr.Code != ExitAgentNotFound {
		t.Errorf("code = %d, want ExitAgentNotFound (%d)", exitErr.Code, ExitAgentNotFound)
	}
}

func TestRate_ReratingReplacesPrevious(t *testing.T) {
	r := &Registry{agents: map[string]*Agent{
		"gopher": {
			Name:   "gopher",
			Status: StatusActive,
			Reviews: []Review{
				{Author: "alice", Rating: 2, Comment: "first try", Date: "2026-01-01T00:00:00Z"},
				{Author: "bob", Rating: 5, Comment: "great!", Date: "2026-01-02T00:00:00Z"},
			},
			Ratings: Ratings{},
		},
	}}

	err := r.Rate("gopher", "alice", 4, "changed my mind")
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	a := r.agents["gopher"]
	if len(a.Reviews) != 2 {
		t.Fatalf("expected 2 reviews (bob + alice replacement), got %d", len(a.Reviews))
	}

	// Bob's review should be unchanged
	foundBob := false
	for _, rv := range a.Reviews {
		if rv.Author == "bob" {
			foundBob = true
			if rv.Rating != 5 {
				t.Errorf("bob's rating = %d, want 5", rv.Rating)
			}
		}
	}
	if !foundBob {
		t.Error("bob's review was lost")
	}

	// Alice's review should be replaced
	aliceFound := false
	for _, rv := range a.Reviews {
		if rv.Author == "alice" {
			aliceFound = true
			if rv.Rating != 4 {
				t.Errorf("alice's new rating = %d, want 4", rv.Rating)
			}
			if rv.Comment != "changed my mind" {
				t.Errorf("alice's new comment = %q, want %q", rv.Comment, "changed my mind")
			}
		}
	}
	if !aliceFound {
		t.Error("alice's review was lost entirely")
	}
}

func TestRate_MultipleHumansAccumulate(t *testing.T) {
	r := &Registry{agents: map[string]*Agent{
		"gopher": {
			Name:    "gopher",
			Status:  StatusActive,
			Reviews: nil,
			Ratings: Ratings{},
		},
	}}

	_ = r.Rate("gopher", "alice", 4, "good")
	_ = r.Rate("gopher", "bob", 2, "meh")
	_ = r.Rate("gopher", "carol", 5, "excellent")

	a := r.agents["gopher"]
	if len(a.Reviews) != 3 {
		t.Fatalf("expected 3 reviews, got %d", len(a.Reviews))
	}
	expectedAvg := float64(4+2+5) / 3.0
	if a.Ratings.Average != expectedAvg {
		t.Errorf("average = %f, want %f", a.Ratings.Average, expectedAvg)
	}
	if a.Ratings.Count != 3 {
		t.Errorf("count = %d, want 3", a.Ratings.Count)
	}
}

// ---------------------------------------------------------------------------
// TestGetRatings
// ---------------------------------------------------------------------------

func TestGetRatings_Success(t *testing.T) {
	r := &Registry{agents: map[string]*Agent{
		"gopher": {
			Name:   "gopher",
			Status: StatusActive,
			Reviews: []Review{
				{Author: "alice", Rating: 4, Comment: "good", Date: "2026-03-01T00:00:00Z"},
				{Author: "bob", Rating: 5, Comment: "great", Date: "2026-03-02T00:00:00Z"},
			},
		},
	}}

	reviews, err := r.GetRatings("gopher")
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if len(reviews) != 2 {
		t.Fatalf("expected 2 reviews, got %d", len(reviews))
	}
}

func TestGetRatings_ReturnsCopy(t *testing.T) {
	r := &Registry{agents: map[string]*Agent{
		"gopher": {
			Name:   "gopher",
			Status: StatusActive,
			Reviews: []Review{
				{Author: "alice", Rating: 4, Comment: "good", Date: "2026-03-01T00:00:00Z"},
			},
		},
	}}

	reviews, err := r.GetRatings("gopher")
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	// Mutating the returned slice must not affect the registry's copy
	reviews[0].Author = "hacker"
	_ = append(reviews, Review{Author: "injected", Rating: 1})

	if r.agents["gopher"].Reviews[0].Author != "alice" {
		t.Errorf("registry copy was mutated: author = %q, want %q",
			r.agents["gopher"].Reviews[0].Author, "alice")
	}
	if len(r.agents["gopher"].Reviews) != 1 {
		t.Errorf("registry count changed: got %d, want 1", len(r.agents["gopher"].Reviews))
	}
}

func TestGetRatings_AgentNotFound(t *testing.T) {
	r := &Registry{agents: map[string]*Agent{}}

	_, err := r.GetRatings("nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %T", err)
	}
	if exitErr.Code != ExitAgentNotFound {
		t.Errorf("code = %d, want ExitAgentNotFound (%d)", exitErr.Code, ExitAgentNotFound)
	}
}

func TestGetRatings_EmptyReviews(t *testing.T) {
	r := &Registry{agents: map[string]*Agent{
		"gopher": {
			Name:    "gopher",
			Status:  StatusActive,
			Reviews: nil,
		},
	}}

	reviews, err := r.GetRatings("gopher")
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if len(reviews) != 0 {
		t.Errorf("expected 0 reviews, got %d", len(reviews))
	}
}

// ---------------------------------------------------------------------------
// TestVerifyHuman
// ---------------------------------------------------------------------------

func TestVerifyHuman(t *testing.T) {
	tests := []struct {
		name   string
		author string
		want   bool
	}{
		{"normal human", "alice", true},
		{"human with dots and hyphens", "john.doe-smith", true},
		{"single word", "bob", true},
		{"empty string", "", true},
		{"agent-minimax", "agent-minimax", false},
		{"agent-glm", "agent-glm", false},
		{"agent prefix only", "agent-", false},
		{"agent without hyphen — not an agent", "agendum", true},
		{"prefix but no hyphen", "agent007", true},
		{"agent hyphen anywhere else", "my-agent-name", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VerifyHuman(tt.author)
			if got != tt.want {
				t.Errorf("VerifyHuman(%q) = %v, want %v", tt.author, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestRecalcRatingAverage
// ---------------------------------------------------------------------------

func TestRecalcRatingAverage_Empty(t *testing.T) {
	a := &Agent{
		Name:    "gopher",
		Reviews: nil,
		Ratings: Ratings{},
	}

	recalcRatingAverage(a)

	if a.Ratings.Average != 0 {
		t.Errorf("average = %f, want 0", a.Ratings.Average)
	}
	if a.Ratings.Count != 0 {
		t.Errorf("count = %d, want 0", a.Ratings.Count)
	}
	// UpdatedAt is only set when there are reviews (early-return optimization).
	// Empty case is a no-op beyond zeroing count/average.
}

func TestRecalcRatingAverage_SingleReview(t *testing.T) {
	a := &Agent{
		Name: "gopher",
		Reviews: []Review{
			{Author: "alice", Rating: 5},
		},
		Ratings: Ratings{},
	}

	recalcRatingAverage(a)

	if a.Ratings.Average != 5.0 {
		t.Errorf("average = %f, want 5.0", a.Ratings.Average)
	}
	if a.Ratings.Count != 1 {
		t.Errorf("count = %d, want 1", a.Ratings.Count)
	}
	if dist := a.Ratings.Distribution["5_star"]; dist != 1 {
		t.Errorf("distribution[5_star] = %d, want 1", dist)
	}
}

func TestRecalcRatingAverage_MultipleReviews(t *testing.T) {
	a := &Agent{
		Name: "gopher",
		Reviews: []Review{
			{Author: "alice", Rating: 5},
			{Author: "bob", Rating: 3},
			{Author: "carol", Rating: 1},
			{Author: "dave", Rating: 5},
		},
		Ratings: Ratings{},
	}

	recalcRatingAverage(a)

	expectedAvg := float64(5+3+1+5) / 4.0
	if a.Ratings.Average != expectedAvg {
		t.Errorf("average = %f, want %f", a.Ratings.Average, expectedAvg)
	}
	if a.Ratings.Count != 4 {
		t.Errorf("count = %d, want 4", a.Ratings.Count)
	}
	if a.Ratings.Distribution["5_star"] != 2 {
		t.Errorf("distribution[5_star] = %d, want 2", a.Ratings.Distribution["5_star"])
	}
	if a.Ratings.Distribution["3_star"] != 1 {
		t.Errorf("distribution[3_star] = %d, want 1", a.Ratings.Distribution["3_star"])
	}
	if a.Ratings.Distribution["1_star"] != 1 {
		t.Errorf("distribution[1_star] = %d, want 1", a.Ratings.Distribution["1_star"])
	}
}

func TestRecalcRatingAverage_SetsUpdatedAt(t *testing.T) {
	a := &Agent{
		Name: "gopher",
		Reviews: []Review{
			{Author: "alice", Rating: 3},
		},
		Ratings: Ratings{},
	}
	oldUpdated := a.UpdatedAt

	recalcRatingAverage(a)

	if a.UpdatedAt == "" {
		t.Error("UpdatedAt should be set")
	}
	if a.UpdatedAt == oldUpdated {
		t.Error("UpdatedAt should be updated")
	}
}
