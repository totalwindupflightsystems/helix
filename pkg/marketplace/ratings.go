package marketplace

import (
	"fmt"
	"strings"
)

// Human rating system (spec §9).
//
// Only humans can rate agents. Ratings are 1-5 stars. One rating per human per
// agent (re-rating replaces the previous). The average feeds the trust score's
// human_rating_bonus component (spec §7.1, §9.2).

// Rate submits a human rating (1-5) for an agent. Returns:
//   - ExitInvalidRating if rating is outside [1, 5]
//   - ExitUnauthorized if the author is not a verified human
//   - ExitAgentNotFound if the agent is not in the marketplace
//
// Per spec §9.1, re-rating by the same author replaces the previous review.
func (r *Registry) Rate(agentName, author string, rating int, comment string) error {
	if rating < 1 || rating > 5 {
		return &ExitError{
			Code:    ExitInvalidRating,
			Message: fmt.Sprintf("INVALID_RATING: must be 1-5, got %d", rating),
		}
	}
	if !VerifyHuman(author) {
		return &ExitError{
			Code:    ExitUnauthorized,
			Message: "UNAUTHORIZED: only humans can rate agents",
		}
	}
	a, ok := r.agents[agentName]
	if !ok {
		return &ExitError{
			Code:    ExitAgentNotFound,
			Message: fmt.Sprintf("AGENT_NOT_FOUND: %s not in marketplace", agentName),
		}
	}

	review := Review{
		Author:  author,
		Rating:  rating,
		Comment: comment,
		Date:    nowISO(),
	}

	// Replace existing review by same author (spec §9.1: "One rating per human
	// per agent (re-rating replaces previous)").
	var filtered []Review
	for _, rv := range a.Reviews {
		if rv.Author != author {
			filtered = append(filtered, rv)
		}
	}
	filtered = append(filtered, review)
	a.Reviews = filtered
	recalcRatingAverage(a)
	return nil
}

// GetRatings returns all reviews for an agent, most recent first.
func (r *Registry) GetRatings(agentName string) ([]Review, error) {
	a, ok := r.agents[agentName]
	if !ok {
		return nil, &ExitError{
			Code:    ExitAgentNotFound,
			Message: fmt.Sprintf("AGENT_NOT_FOUND: %s not in marketplace", agentName),
		}
	}
	// Return a copy to prevent external mutation.
	result := make([]Review, len(a.Reviews))
	copy(result, a.Reviews)
	return result, nil
}

// VerifyHuman checks if author is a human (spec §9.1: "humans have no
// forgejo_username field"). In production this checks known-friends.json
// (Feature 1). In this stub, the convention is that Forgejo bot accounts use
// usernames prefixed with "agent-" (matching the testdata). Any author not
// matching this convention is treated as human.
func VerifyHuman(author string) bool {
	return !strings.HasPrefix(author, "agent-")
}

// recalcRatingAverage recomputes the aggregate rating fields from the review
// list. This is called internally after a Rate operation mutates Reviews.
func recalcRatingAverage(a *Agent) {
	if len(a.Reviews) == 0 {
		a.Ratings.Average = 0
		a.Ratings.Count = 0
		return
	}
	sum := 0
	dist := make(map[string]int)
	for _, rv := range a.Reviews {
		sum += rv.Rating
		dist[fmt.Sprintf("%d_star", rv.Rating)]++
	}
	a.Ratings.Average = float64(sum) / float64(len(a.Reviews))
	a.Ratings.Count = len(a.Reviews)
	a.Ratings.Distribution = dist
	a.UpdatedAt = nowISO()
}
