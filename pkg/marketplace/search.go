package marketplace

// search.go implements the marketplace search ranking algorithm per
// specs/agent-marketplace.md §8 (Search and Discovery).
//
// The SearchRanker computes a relevance score for each agent listing given
// a SearchQuery. Ranking factors (per spec §8.2):
//
//  1. Trust score (primary sort dimension)
//  2. Capability match (keyword + tag overlap)
//  3. Performance metrics (merge success rate, avg review time)
//  4. Human ratings
//  5. Cost-effectiveness
//
// Supports filtering by trust tier minimum, max cost, and capability tags.

import (
	"math"
	"sort"
)

// ============================================================================
// SearchResult
// ============================================================================

// SearchResult is a single ranked agent entry returned by SearchRanker.Search.
type SearchResult struct {
	Agent         *Agent
	Score         float64 // 0-100 relevance score
	CapMatchPct   int     // 0-100 percentage of query capabilities matched
	TrustScore    int
	RatingValue   float64
	Effectiveness float64 // performance-weighted score
	Rank          int     // 1-based position in results
}

// ============================================================================
// SearchRanker
// ============================================================================

// SearchRanker computes a relevance score for each agent listing and returns
// ranked results. It operates on a slice of agents from the registry.
type SearchRanker struct {
	// WeightTrust is the weight for trust score in the composite score (default 0.35).
	WeightTrust float64
	// WeightCapability is the weight for capability match (default 0.25).
	WeightCapability float64
	// WeightPerformance is the weight for performance metrics (default 0.15).
	WeightPerformance float64
	// WeightRating is the weight for human ratings (default 0.15).
	WeightRating float64
	// WeightCost is the weight for cost-effectiveness (default 0.10).
	WeightCost float64
}

// NewSearchRanker creates a SearchRanker with spec-compliant default weights.
func NewSearchRanker() *SearchRanker {
	return &SearchRanker{
		WeightTrust:       0.35,
		WeightCapability:  0.25,
		WeightPerformance: 0.15,
		WeightRating:      0.15,
		WeightCost:        0.10,
	}
}

// Search ranks agents by relevance score given a SearchQuery.
// Only active agents are included (spec §10.1).
func (r *SearchRanker) Search(agents []*Agent, query SearchQuery) []*SearchResult {
	// Filter agents based on query constraints.
	filtered := r.filter(agents, query)

	// Score each filtered agent.
	results := make([]*SearchResult, 0, len(filtered))
	for _, a := range filtered {
		result := r.scoreAgent(a, query)
		results = append(results, result)
	}

	// Sort by score descending.
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		// Tiebreaker: higher trust score wins.
		if results[i].TrustScore != results[j].TrustScore {
			return results[i].TrustScore > results[j].TrustScore
		}
		// Final tiebreaker: lower cost is better.
		return results[i].Agent.Budget.AverageTaskCost < results[j].Agent.Budget.AverageTaskCost
	})

	// Assign ranks.
	for i := range results {
		results[i].Rank = i + 1
	}

	// Apply limit.
	if query.Limit > 0 && len(results) > query.Limit {
		results = results[:query.Limit]
	}

	return results
}

// filter applies the query constraints to narrow the candidate pool.
func (r *SearchRanker) filter(agents []*Agent, query SearchQuery) []*Agent {
	var filtered []*Agent
	for _, a := range agents {
		// Only active agents (spec §10.1).
		if a.Status != StatusActive {
			continue
		}
		// Trust threshold filter.
		if query.MinTrust > 0 && a.TrustScore < query.MinTrust {
			continue
		}
		// Max cost filter.
		if query.MaxCost > 0 && a.Budget.AverageTaskCost > query.MaxCost {
			continue
		}
		// Capability filter — agent must have ALL requested capabilities.
		if len(query.Capabilities) > 0 {
			matched := true
			for _, c := range query.Capabilities {
				if !hasCapability(a, c) {
					matched = false
					break
				}
			}
			if !matched {
				continue
			}
		}
		filtered = append(filtered, a)
	}
	return filtered
}

// scoreAgent computes the composite relevance score for a single agent.
func (r *SearchRanker) scoreAgent(a *Agent, query SearchQuery) *SearchResult {
	result := &SearchResult{
		Agent:       a,
		TrustScore:  a.TrustScore,
		RatingValue: a.Ratings.Average,
	}

	// 1. Trust score component (normalized to 0-1).
	trustNorm := float64(a.TrustScore) / 100.0

	// 2. Capability match percentage.
	result.CapMatchPct = r.capabilityMatchPct(a, query)
	capNorm := float64(result.CapMatchPct) / 100.0

	// 3. Performance score (normalized 0-1).
	perfScore := r.performanceScore(a)
	result.Effectiveness = perfScore * 100

	// 4. Rating score (normalized 0-1).
	ratingNorm := a.Ratings.Average / 5.0

	// 5. Cost-effectiveness score (normalized 0-1).
	costNorm := r.costScore(a)

	// Composite weighted score.
	result.Score = (r.WeightTrust * trustNorm * 100) +
		(r.WeightCapability * capNorm * 100) +
		(r.WeightPerformance * perfScore * 100) +
		(r.WeightRating * ratingNorm * 100) +
		(r.WeightCost * costNorm * 100)

	// Round to 2 decimal places.
	result.Score = math.Round(result.Score*100) / 100

	return result
}

// capabilityMatchPct computes what percentage of the query's required
// capabilities the agent possesses.
func (r *SearchRanker) capabilityMatchPct(a *Agent, query SearchQuery) int {
	if len(query.Capabilities) == 0 {
		return 100
	}
	matched := 0
	for _, c := range query.Capabilities {
		if hasCapability(a, c) {
			matched++
		}
	}
	return matched * 100 / len(query.Capabilities)
}

// performanceScore computes a 0-1 normalized performance score from the
// agent's objective metrics (spec §7.2 component weights).
func (r *SearchRanker) performanceScore(a *Agent) float64 {
	// Weighted sum of performance metrics per spec §7.2:
	// PR acceptance rate: 0.40
	// Review accuracy: 0.30
	// Budget adherence: 0.20
	// Uptime: 0.10
	score := a.Performance.PrAcceptanceRate*0.40 +
		a.Performance.ReviewAccuracy*0.30 +
		a.Performance.BudgetAdherence*0.20 +
		a.Performance.Uptime*0.10

	// Clamp to [0, 1].
	if score > 1.0 {
		score = 1.0
	}
	if score < 0 {
		score = 0
	}
	return score
}

// costScore computes a 0-1 normalized cost-effectiveness score.
// Lower cost = higher score. Uses an inverse curve so that very cheap agents
// score near 1.0 and expensive agents approach 0.
func (r *SearchRanker) costScore(a *Agent) float64 {
	cost := a.Budget.AverageTaskCost
	if cost <= 0 {
		return 1.0
	}
	// Inverse curve: score = 1 / (1 + cost * k)
	// k=10 means $0.10 average task cost → score ≈ 0.5
	// $0.01 → score ≈ 0.91, $0.25 → score ≈ 0.29
	const k = 10.0
	score := 1.0 / (1.0 + cost*k)
	if score < 0 {
		score = 0
	}
	return score
}

// ============================================================================
// SearchRankerOption
// ============================================================================

// SearchRankerOption configures a SearchRanker.
type SearchRankerOption func(*SearchRanker)

// WithSearchWeights overrides the default ranking weights.
// The sum of all weights should equal 1.0.
func WithSearchWeights(trust, capability, performance, rating, cost float64) SearchRankerOption {
	return func(r *SearchRanker) {
		r.WeightTrust = trust
		r.WeightCapability = capability
		r.WeightPerformance = performance
		r.WeightRating = rating
		r.WeightCost = cost
	}
}

// NewSearchRankerWith creates a SearchRanker with custom options.
func NewSearchRankerWith(opts ...SearchRankerOption) *SearchRanker {
	r := NewSearchRanker()
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// ============================================================================
// Convenience: ScoreComparison
// ============================================================================

// ScoreComparison returns the score difference between two search results.
// Positive means result a scored higher.
func ScoreComparison(a, b *SearchResult) float64 {
	return a.Score - b.Score
}

// ============================================================================
// Search by text query (keyword matching)
// ============================================================================

// TextSearch performs a fuzzy keyword search over agent names, display names,
// and capabilities. Returns agents whose name or capabilities match any of
// the keywords. Results are ranked using the standard SearchRanker.
func (r *SearchRanker) TextSearch(agents []*Agent, query string, filter SearchQuery) []*SearchResult {
	if query == "" {
		return r.Search(agents, filter)
	}

	// Filter agents that match the text query.
	keywords := splitKeywords(query)
	var matched []*Agent
	for _, a := range agents {
		if a.Status != StatusActive {
			continue
		}
		if agentMatchesKeywords(a, keywords) {
			matched = append(matched, a)
		}
	}

	return r.Search(matched, filter)
}

// splitKeywords splits a query string into lowercase keywords.
func splitKeywords(query string) []string {
	var keywords []string
	current := ""
	for _, ch := range query {
		if ch == ' ' || ch == '\t' || ch == ',' {
			if current != "" {
				keywords = append(keywords, lower(current))
				current = ""
			}
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		keywords = append(keywords, lower(current))
	}
	return keywords
}

// agentMatchesKeywords checks if any keyword matches the agent name, display
// name, or any capability tag.
func agentMatchesKeywords(a *Agent, keywords []string) bool {
	if len(keywords) == 0 {
		return true
	}
	nameLower := lower(a.Name)
	displayLower := lower(a.DisplayName)
	capStrs := make([]string, len(a.Capabilities))
	for i, c := range a.Capabilities {
		capStrs[i] = lower(string(c))
	}

	for _, kw := range keywords {
		matched := false
		if containsStr(nameLower, kw) || containsStr(displayLower, kw) {
			matched = true
		}
		if !matched {
			for _, cap := range capStrs {
				if containsStr(cap, kw) {
					matched = true
					break
				}
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// lower converts a string to lowercase (ASCII only, sufficient for agent names).
func lower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		result[i] = c
	}
	return string(result)
}

// containsStr checks if s contains substr (case-sensitive, both pre-lowered).
func containsStr(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
