package marketplace

import (
	"errors"
	"testing"
)

// =============================================================================
// ListByCapability tests
// =============================================================================

func TestListByCapability_MatchingCapability(t *testing.T) {
	r := &Registry{
		agents: map[string]*Agent{
			"gopher": {
				Name:         "gopher",
				Status:       StatusActive,
				TrustScore:   80,
				Capabilities: []Capability{CapCodeReview, CapTesting},
			},
			"piper": {
				Name:         "piper",
				Status:       StatusActive,
				TrustScore:   60,
				Capabilities: []Capability{CapTesting},
			},
			"raven": {
				Name:         "raven",
				Status:       StatusActive,
				TrustScore:   90,
				Capabilities: []Capability{CapCodeReview, CapRefactoring},
			},
		},
	}

	listings, err := r.ListByCapability(CapCodeReview)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(listings) != 2 {
		t.Fatalf("got %d listings, want 2 (gopher + raven)", len(listings))
	}

	// Sorted by trust score descending: raven (90) first, gopher (80) second.
	if listings[0].Name != "raven" {
		t.Errorf("first listing = %q, want raven", listings[0].Name)
	}
	if listings[1].Name != "gopher" {
		t.Errorf("second listing = %q, want gopher", listings[1].Name)
	}

	// Check listing fields.
	for _, l := range listings {
		if l.Reputation < 0 {
			t.Errorf("listing %q: reputation should be non-negative, got %f", l.Name, l.Reputation)
		}
		if l.ActiveProjects != 0 {
			t.Errorf("listing %q: ActiveProjects should be 0 (stub), got %d", l.Name, l.ActiveProjects)
		}
	}
}

func TestListByCapability_NonMatchingCapability(t *testing.T) {
	r := &Registry{
		agents: map[string]*Agent{
			"gopher": {
				Name:         "gopher",
				Status:       StatusActive,
				TrustScore:   80,
				Capabilities: []Capability{CapTesting},
			},
		},
	}

	listings, err := r.ListByCapability(CapCodeReview)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(listings) != 0 {
		t.Errorf("expected 0 listings for non-matching capability, got %d", len(listings))
	}
}

func TestListByCapability_EmptyRegistry(t *testing.T) {
	r := &Registry{
		agents: make(map[string]*Agent),
	}

	listings, err := r.ListByCapability(CapCodeReview)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(listings) != 0 {
		t.Errorf("expected 0 listings for empty registry, got %d", len(listings))
	}
}

func TestListByCapability_MultipleMatchesSortedByTrust(t *testing.T) {
	r := &Registry{
		agents: map[string]*Agent{
			"alpha": {Name: "alpha", Status: StatusActive, TrustScore: 50, Capabilities: []Capability{CapDocs}},
			"beta":  {Name: "beta", Status: StatusActive, TrustScore: 90, Capabilities: []Capability{CapDocs}},
			"gamma": {Name: "gamma", Status: StatusActive, TrustScore: 70, Capabilities: []Capability{CapDocs}},
		},
	}

	listings, err := r.ListByCapability(CapDocs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(listings) != 3 {
		t.Fatalf("got %d listings, want 3", len(listings))
	}
	// Trust score descending: beta(90), gamma(70), alpha(50).
	expected := []string{"beta", "gamma", "alpha"}
	for i, name := range expected {
		if listings[i].Name != name {
			t.Errorf("listing[%d] = %q, want %q", i, listings[i].Name, name)
		}
	}
}

func TestListByCapability_RetiredAgentsExcluded(t *testing.T) {
	r := &Registry{
		agents: map[string]*Agent{
			"active":  {Name: "active", Status: StatusActive, TrustScore: 80, Capabilities: []Capability{CapTesting}},
			"retired": {Name: "retired", Status: StatusRetired, TrustScore: 95, Capabilities: []Capability{CapTesting}},
		},
	}

	listings, err := r.ListByCapability(CapTesting)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(listings) != 1 {
		t.Fatalf("got %d listings, want 1 (active only)", len(listings))
	}
	if listings[0].Name != "active" {
		t.Errorf("listing = %q, want active", listings[0].Name)
	}
}

func TestListByCapability_DeprecatedAgentsIncluded(t *testing.T) {
	r := &Registry{
		agents: map[string]*Agent{
			"active":     {Name: "active", Status: StatusActive, TrustScore: 70, Capabilities: []Capability{CapSpecWriting}},
			"deprecated": {Name: "deprecated", Status: StatusDeprecated, TrustScore: 85, Capabilities: []Capability{CapSpecWriting}},
		},
	}

	listings, err := r.ListByCapability(CapSpecWriting)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(listings) != 2 {
		t.Fatalf("got %d listings, want 2 (deprecated agents are not retired)", len(listings))
	}
	// Deprecated but high trust should come first.
	if listings[0].Name != "deprecated" {
		t.Errorf("first listing = %q, want deprecated", listings[0].Name)
	}
}

func TestListByCapability_ListingFieldsCorrect(t *testing.T) {
	r := &Registry{
		agents: map[string]*Agent{
			"agent1": {
				Name:         "agent1",
				DisplayName:  "Agent One",
				Status:       StatusActive,
				TrustScore:   85,
				Capabilities: []Capability{CapCodeReview, CapTesting},
				Ratings:      Ratings{Average: 4.5, Count: 12},
			},
		},
	}

	listings, err := r.ListByCapability(CapCodeReview)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(listings) != 1 {
		t.Fatalf("got %d listings, want 1", len(listings))
	}
	l := listings[0]
	if l.Name != "agent1" {
		t.Errorf("Name = %q, want agent1", l.Name)
	}
	if l.Description != "Agent One" {
		t.Errorf("Description = %q, want 'Agent One'", l.Description)
	}
	if l.Reputation != 85.0 {
		t.Errorf("Reputation = %f, want 85.0", l.Reputation)
	}
	if l.Reviews != 12 {
		t.Errorf("Reviews = %d, want 12", l.Reviews)
	}
	if len(l.Capabilities) != 2 {
		t.Errorf("Capabilities count = %d, want 2", len(l.Capabilities))
	}
}

// =============================================================================
// GetAgent tests
// =============================================================================

func TestGetAgent_Found(t *testing.T) {
	r := &Registry{
		agents: map[string]*Agent{
			"gopher": {
				Name:         "gopher",
				Status:       StatusActive,
				TrustScore:   75,
				Capabilities: []Capability{CapCodeReview},
				Ratings:      Ratings{Average: 4.2, Count: 5},
				CreatedAt:    "2026-01-01T00:00:00Z",
				UpdatedAt:    "2026-06-01T00:00:00Z",
			},
		},
	}

	profile, err := r.GetAgent("gopher")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile == nil {
		t.Fatal("expected non-nil profile")
	}
	if profile.Name != "gopher" {
		t.Errorf("Name = %q, want gopher", profile.Name)
	}
	if profile.TrustScore != 75 {
		t.Errorf("TrustScore = %d, want 75", profile.TrustScore)
	}

	// ReputationHistory should contain one point.
	if len(profile.ReputationHistory) != 1 {
		t.Errorf("ReputationHistory length = %d, want 1", len(profile.ReputationHistory))
	}
	if profile.ReputationHistory[0].Score != 75.0 {
		t.Errorf("ReputationHistory[0].Score = %f, want 75.0", profile.ReputationHistory[0].Score)
	}

	// ReviewSummary.
	if profile.ReviewSummary.Average != 4.2 {
		t.Errorf("ReviewSummary.Average = %f, want 4.2", profile.ReviewSummary.Average)
	}
	if profile.ReviewSummary.Count != 5 {
		t.Errorf("ReviewSummary.Count = %d, want 5", profile.ReviewSummary.Count)
	}
}

func TestGetAgent_NotFound(t *testing.T) {
	r := &Registry{
		agents: map[string]*Agent{
			"gopher": {Name: "gopher", Status: StatusActive},
		},
	}

	profile, err := r.GetAgent("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent agent")
	}
	if profile != nil {
		t.Error("expected nil profile on error")
	}

	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *ExitError, got %T", err)
	}
	if exitErr.Code != ExitAgentNotFound {
		t.Errorf("Code = %d, want ExitAgentNotFound (%d)", exitErr.Code, ExitAgentNotFound)
	}
}

func TestGetAgent_NilAgentsMap(t *testing.T) {
	r := &Registry{
		agents: nil,
	}

	profile, err := r.GetAgent("any")
	if err == nil {
		t.Fatal("expected error for nil agents map")
	}
	if profile != nil {
		t.Error("expected nil profile on error")
	}
}

func TestGetAgent_EmptyAgentsMap(t *testing.T) {
	r := &Registry{
		agents: make(map[string]*Agent),
	}

	profile, err := r.GetAgent("any")
	if err == nil {
		t.Fatal("expected error for empty agents map")
	}
	if profile != nil {
		t.Error("expected nil profile on error")
	}
}

func TestGetAgent_WithReviewsSortedRecentFirst(t *testing.T) {
	r := &Registry{
		agents: map[string]*Agent{
			"reviewed": {
				Name:       "reviewed",
				Status:     StatusActive,
				TrustScore: 88,
				Ratings:    Ratings{Average: 4.0, Count: 3},
				UpdatedAt:  "2026-06-15T00:00:00Z",
				Reviews: []Review{
					{Author: "alice", Rating: 4, Comment: "old", Date: "2026-01-01"},
					{Author: "bob", Rating: 5, Comment: "new", Date: "2026-06-01"},
					{Author: "carol", Rating: 3, Comment: "mid", Date: "2026-03-15"},
				},
			},
		},
	}

	profile, err := r.GetAgent("reviewed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Reviews should be sorted most recent first.
	recent := profile.ReviewSummary.Recent
	if len(recent) != 3 {
		t.Fatalf("got %d recent reviews, want 3", len(recent))
	}
	if recent[0].Date != "2026-06-01" {
		t.Errorf("first review date = %q, want 2026-06-01", recent[0].Date)
	}
	if recent[1].Date != "2026-03-15" {
		t.Errorf("second review date = %q, want 2026-03-15", recent[1].Date)
	}
	if recent[2].Date != "2026-01-01" {
		t.Errorf("third review date = %q, want 2026-01-01", recent[2].Date)
	}
}

func TestGetAgent_SingleReview(t *testing.T) {
	r := &Registry{
		agents: map[string]*Agent{
			"solo": {
				Name:       "solo",
				Status:     StatusActive,
				TrustScore: 50,
				Ratings:    Ratings{Average: 5.0, Count: 1},
				UpdatedAt:  "2026-06-01T00:00:00Z",
				Reviews: []Review{
					{Author: "user1", Rating: 5, Comment: "great", Date: "2026-06-01"},
				},
			},
		},
	}

	profile, err := r.GetAgent("solo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(profile.ReviewSummary.Recent) != 1 {
		t.Errorf("got %d recent reviews, want 1", len(profile.ReviewSummary.Recent))
	}
}

func TestGetAgent_ZeroReviews(t *testing.T) {
	r := &Registry{
		agents: map[string]*Agent{
			"new-agent": {
				Name:       "new-agent",
				Status:     StatusActive,
				TrustScore: 50,
				Ratings:    Ratings{Count: 0},
				UpdatedAt:  "2026-06-01T00:00:00Z",
				Reviews:    nil,
			},
		},
	}

	profile, err := r.GetAgent("new-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(profile.ReviewSummary.Recent) != 0 {
		t.Errorf("got %d recent reviews, want 0", len(profile.ReviewSummary.Recent))
	}
	if profile.ReviewSummary.Count != 0 {
		t.Errorf("ReviewSummary.Count = %d, want 0", profile.ReviewSummary.Count)
	}
}

func TestGetAgent_MoreThanFiveReviewsTruncated(t *testing.T) {
	reviews := make([]Review, 10)
	for i := 0; i < 10; i++ {
		reviews[i] = Review{
			Author: "user",
			Rating: 4,
			Date:   "2026-0" + string(rune('1'+i/2)) + "-01", // not critical, just unique-enough
		}
	}
	// Make sure they're distinct for sorting.
	reviews[0].Date = "2026-01-01"
	reviews[1].Date = "2026-02-01"
	reviews[2].Date = "2026-03-01"
	reviews[3].Date = "2026-04-01"
	reviews[4].Date = "2026-05-01"
	reviews[5].Date = "2026-06-01"
	reviews[6].Date = "2026-07-01"
	reviews[7].Date = "2026-08-01"
	reviews[8].Date = "2026-09-01"
	reviews[9].Date = "2026-10-01"

	r := &Registry{
		agents: map[string]*Agent{
			"popular": {
				Name:       "popular",
				Status:     StatusActive,
				TrustScore: 90,
				Ratings:    Ratings{Average: 4.5, Count: 10},
				UpdatedAt:  "2026-10-01T00:00:00Z",
				Reviews:    reviews,
			},
		},
	}

	profile, err := r.GetAgent("popular")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Recent should be truncated to 5 most recent.
	if len(profile.ReviewSummary.Recent) != 5 {
		t.Errorf("got %d recent reviews, want 5 (truncated)", len(profile.ReviewSummary.Recent))
	}
	// First should be most recent.
	if profile.ReviewSummary.Recent[0].Date != "2026-10-01" {
		t.Errorf("first review date = %q, want 2026-10-01", profile.ReviewSummary.Recent[0].Date)
	}
}
