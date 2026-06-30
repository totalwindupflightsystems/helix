package marketplace

import (
	"testing"
)

// ============================================================================
// Test helpers
// ============================================================================

func searchTestAgent(name string, trust int, caps []Capability, cost float64, rating float64, perf Performance) *Agent {
	return &Agent{
		Name:         name,
		DisplayName:  name,
		Status:       StatusActive,
		Tier:         TierPro,
		TrustScore:   trust,
		Capabilities: caps,
		Budget: Budget{
			WeeklyLimit:     10.0,
			AverageTaskCost: cost,
			CostProfile:     CostMedium,
		},
		Performance: perf,
		Ratings: Ratings{
			Average: rating,
			Count:   10,
		},
	}
}

func goodPerf() Performance {
	return Performance{
		PrAcceptanceRate:  0.92,
		ReviewAccuracy:    0.88,
		BudgetAdherence:   0.97,
		Uptime:            0.995,
		TasksCompleted:    247,
		AvgResponseTimeMs: 1200,
	}
}

func caps(c ...Capability) []Capability {
	return c
}

// ============================================================================
// SearchRanker — Basic Search
// ============================================================================

func TestSearchRanker_BasicSearch(t *testing.T) {
	ranker := NewSearchRanker()
	agents := []*Agent{
		searchTestAgent("agent-1", 85, caps(CapGo, CapCodeReview), 0.10, 4.5, goodPerf()),
		searchTestAgent("agent-2", 70, caps(CapGo, CapTesting), 0.05, 4.0, goodPerf()),
		searchTestAgent("agent-3", 90, caps(CapPython), 0.15, 5.0, goodPerf()),
	}

	results := ranker.Search(agents, SearchQuery{})
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// agent-3 (trust 90) should be ranked highest.
	if results[0].Agent.Name != "agent-3" {
		t.Errorf("expected agent-3 first, got %s (score=%.2f)", results[0].Agent.Name, results[0].Score)
	}
}

func TestSearchRanker_RanksByTrustScore(t *testing.T) {
	ranker := NewSearchRanker()
	agents := []*Agent{
		searchTestAgent("low-trust", 30, caps(CapGo), 0.10, 4.0, goodPerf()),
		searchTestAgent("high-trust", 90, caps(CapGo), 0.10, 4.0, goodPerf()),
		searchTestAgent("mid-trust", 60, caps(CapGo), 0.10, 4.0, goodPerf()),
	}

	results := ranker.Search(agents, SearchQuery{})
	if results[0].Agent.Name != "high-trust" {
		t.Errorf("expected high-trust first, got %s", results[0].Agent.Name)
	}
	if results[1].Agent.Name != "mid-trust" {
		t.Errorf("expected mid-trust second, got %s", results[1].Agent.Name)
	}
	if results[2].Agent.Name != "low-trust" {
		t.Errorf("expected low-trust third, got %s", results[2].Agent.Name)
	}
}

func TestSearchRanker_AssignsRanks(t *testing.T) {
	ranker := NewSearchRanker()
	agents := []*Agent{
		searchTestAgent("a1", 50, caps(CapGo), 0.10, 4.0, goodPerf()),
		searchTestAgent("a2", 80, caps(CapGo), 0.10, 4.0, goodPerf()),
		searchTestAgent("a3", 65, caps(CapGo), 0.10, 4.0, goodPerf()),
	}

	results := ranker.Search(agents, SearchQuery{})
	for i, expected := range []int{1, 2, 3} {
		if results[i].Rank != expected {
			t.Errorf("result %d: expected rank %d, got %d", i, expected, results[i].Rank)
		}
	}
}

// ============================================================================
// SearchRanker — Filtering
// ============================================================================

func TestSearchRanker_FilterByMinTrust(t *testing.T) {
	ranker := NewSearchRanker()
	agents := []*Agent{
		searchTestAgent("a1", 30, caps(CapGo), 0.10, 4.0, goodPerf()),
		searchTestAgent("a2", 70, caps(CapGo), 0.10, 4.0, goodPerf()),
		searchTestAgent("a3", 90, caps(CapGo), 0.10, 4.0, goodPerf()),
	}

	results := ranker.Search(agents, SearchQuery{MinTrust: 60})
	if len(results) != 2 {
		t.Fatalf("expected 2 results (trust >= 60), got %d", len(results))
	}
	for _, r := range results {
		if r.Agent.TrustScore < 60 {
			t.Errorf("agent %s has trust %d < 60", r.Agent.Name, r.Agent.TrustScore)
		}
	}
}

func TestSearchRanker_FilterByMaxCost(t *testing.T) {
	ranker := NewSearchRanker()
	agents := []*Agent{
		searchTestAgent("a1", 80, caps(CapGo), 0.30, 4.0, goodPerf()),
		searchTestAgent("a2", 70, caps(CapGo), 0.10, 4.0, goodPerf()),
		searchTestAgent("a3", 60, caps(CapGo), 0.05, 4.0, goodPerf()),
	}

	results := ranker.Search(agents, SearchQuery{MaxCost: 0.15})
	if len(results) != 2 {
		t.Fatalf("expected 2 results (cost <= 0.15), got %d", len(results))
	}
	for _, r := range results {
		if r.Agent.Budget.AverageTaskCost > 0.15 {
			t.Errorf("agent %s has cost %.2f > 0.15", r.Agent.Name, r.Agent.Budget.AverageTaskCost)
		}
	}
}

func TestSearchRanker_FilterByCapability(t *testing.T) {
	ranker := NewSearchRanker()
	agents := []*Agent{
		searchTestAgent("a1", 80, caps(CapGo, CapCodeReview), 0.10, 4.0, goodPerf()),
		searchTestAgent("a2", 70, caps(CapGo, CapTesting), 0.10, 4.0, goodPerf()),
		searchTestAgent("a3", 60, caps(CapPython, CapTesting), 0.10, 4.0, goodPerf()),
	}

	results := ranker.Search(agents, SearchQuery{Capabilities: []Capability{CapGo}})
	if len(results) != 2 {
		t.Fatalf("expected 2 results (cap=go), got %d", len(results))
	}
	for _, r := range results {
		if !hasCapability(r.Agent, CapGo) {
			t.Errorf("agent %s does not have go capability", r.Agent.Name)
		}
	}
}

func TestSearchRanker_FilterByMultipleCapabilities(t *testing.T) {
	ranker := NewSearchRanker()
	agents := []*Agent{
		searchTestAgent("a1", 80, caps(CapGo, CapCodeReview, CapTesting), 0.10, 4.0, goodPerf()),
		searchTestAgent("a2", 70, caps(CapGo, CapTesting), 0.10, 4.0, goodPerf()),
		searchTestAgent("a3", 60, caps(CapGo), 0.10, 4.0, goodPerf()),
	}

	// Must have both go AND testing.
	results := ranker.Search(agents, SearchQuery{Capabilities: []Capability{CapGo, CapTesting}})
	if len(results) != 2 {
		t.Fatalf("expected 2 results (cap=go AND testing), got %d", len(results))
	}
}

func TestSearchRanker_FilterExcludesInactiveAgents(t *testing.T) {
	ranker := NewSearchRanker()
	active := searchTestAgent("active", 80, caps(CapGo), 0.10, 4.0, goodPerf())
	deprecated := searchTestAgent("deprecated", 90, caps(CapGo), 0.10, 4.0, goodPerf())
	deprecated.Status = StatusDeprecated
	retired := searchTestAgent("retired", 70, caps(CapGo), 0.10, 4.0, goodPerf())
	retired.Status = StatusRetired

	agents := []*Agent{active, deprecated, retired}
	results := ranker.Search(agents, SearchQuery{})
	if len(results) != 1 {
		t.Fatalf("expected 1 active agent, got %d", len(results))
	}
	if results[0].Agent.Name != "active" {
		t.Errorf("expected active agent, got %s", results[0].Agent.Name)
	}
}

func TestSearchRanker_Limit(t *testing.T) {
	ranker := NewSearchRanker()
	agents := []*Agent{
		searchTestAgent("a1", 80, caps(CapGo), 0.10, 4.0, goodPerf()),
		searchTestAgent("a2", 70, caps(CapGo), 0.10, 4.0, goodPerf()),
		searchTestAgent("a3", 60, caps(CapGo), 0.10, 4.0, goodPerf()),
		searchTestAgent("a4", 50, caps(CapGo), 0.10, 4.0, goodPerf()),
	}

	results := ranker.Search(agents, SearchQuery{Limit: 2})
	if len(results) != 2 {
		t.Fatalf("expected 2 results (limit), got %d", len(results))
	}
}

func TestSearchRanker_CombinedFilters(t *testing.T) {
	ranker := NewSearchRanker()
	agents := []*Agent{
		searchTestAgent("a1", 80, caps(CapGo, CapSecurityReview), 0.10, 4.0, goodPerf()),
		searchTestAgent("a2", 70, caps(CapGo), 0.05, 4.0, goodPerf()),
		searchTestAgent("a3", 60, caps(CapGo, CapSecurityReview), 0.20, 4.0, goodPerf()),
	}

	// go + security-review, trust >= 65, cost <= 0.15.
	results := ranker.Search(agents, SearchQuery{
		Capabilities: []Capability{CapGo, CapSecurityReview},
		MinTrust:     65,
		MaxCost:      0.15,
	})
	if len(results) != 1 {
		t.Fatalf("expected 1 result with all filters, got %d", len(results))
	}
	if results[0].Agent.Name != "a1" {
		t.Errorf("expected a1, got %s", results[0].Agent.Name)
	}
}

func TestSearchRanker_NoResults(t *testing.T) {
	ranker := NewSearchRanker()
	agents := []*Agent{
		searchTestAgent("a1", 80, caps(CapGo), 0.10, 4.0, goodPerf()),
	}

	results := ranker.Search(agents, SearchQuery{MinTrust: 100})
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

// ============================================================================
// SearchRanker — Scoring Components
// ============================================================================

func TestSearchRanker_CapabilityMatchPct(t *testing.T) {
	ranker := NewSearchRanker()
	agent := searchTestAgent("a1", 80, caps(CapGo, CapCodeReview, CapTesting), 0.10, 4.0, goodPerf())

	// 2 out of 3 → 66%.
	pct := ranker.capabilityMatchPct(agent, SearchQuery{
		Capabilities: []Capability{CapGo, CapCodeReview, CapSecurityReview},
	})
	if pct != 66 {
		t.Errorf("expected 66%% match, got %d%%", pct)
	}
}

func TestSearchRanker_CapabilityMatchPctFull(t *testing.T) {
	ranker := NewSearchRanker()
	agent := searchTestAgent("a1", 80, caps(CapGo, CapCodeReview), 0.10, 4.0, goodPerf())

	pct := ranker.capabilityMatchPct(agent, SearchQuery{
		Capabilities: []Capability{CapGo, CapCodeReview},
	})
	if pct != 100 {
		t.Errorf("expected 100%% match, got %d%%", pct)
	}
}

func TestSearchRanker_CapabilityMatchPctNoQuery(t *testing.T) {
	ranker := NewSearchRanker()
	agent := searchTestAgent("a1", 80, caps(CapGo), 0.10, 4.0, goodPerf())

	pct := ranker.capabilityMatchPct(agent, SearchQuery{})
	if pct != 100 {
		t.Errorf("expected 100%% (no query caps), got %d%%", pct)
	}
}

func TestSearchRanker_PerformanceScore(t *testing.T) {
	ranker := NewSearchRanker()

	// High performance.
	highPerf := Performance{
		PrAcceptanceRate: 0.99,
		ReviewAccuracy:   0.95,
		BudgetAdherence:  0.98,
		Uptime:           1.0,
	}
	score := ranker.performanceScore(searchTestAgent("a", 50, nil, 0.1, 4.0, highPerf))
	if score < 0.95 {
		t.Errorf("expected performance score >= 0.95, got %.4f", score)
	}

	// Low performance.
	lowPerf := Performance{
		PrAcceptanceRate: 0.50,
		ReviewAccuracy:   0.60,
		BudgetAdherence:  0.70,
		Uptime:           0.80,
	}
	score = ranker.performanceScore(searchTestAgent("b", 50, nil, 0.1, 4.0, lowPerf))
	if score > 0.65 {
		t.Errorf("expected performance score <= 0.65, got %.4f", score)
	}
}

func TestSearchRanker_CostScore(t *testing.T) {
	ranker := NewSearchRanker()

	// Zero cost → max score.
	agent := searchTestAgent("a", 50, nil, 0, 4.0, goodPerf())
	score := ranker.costScore(agent)
	if score != 1.0 {
		t.Errorf("expected 1.0 for zero cost, got %.4f", score)
	}

	// Low cost → high score.
	agent = searchTestAgent("a", 50, nil, 0.01, 4.0, goodPerf())
	score = ranker.costScore(agent)
	if score < 0.9 {
		t.Errorf("expected score >= 0.9 for $0.01, got %.4f", score)
	}

	// High cost → low score.
	agent = searchTestAgent("a", 50, nil, 0.50, 4.0, goodPerf())
	score = ranker.costScore(agent)
	if score > 0.3 {
		t.Errorf("expected score <= 0.3 for $0.50, got %.4f", score)
	}
}

func TestSearchRanker_CostScoreMonotonicDecreasing(t *testing.T) {
	ranker := NewSearchRanker()

	costs := []float64{0.01, 0.05, 0.10, 0.15, 0.20, 0.25}
	scores := make([]float64, len(costs))
	for i, c := range costs {
		scores[i] = ranker.costScore(searchTestAgent("a", 50, nil, c, 4.0, goodPerf()))
	}

	for i := 1; i < len(scores); i++ {
		if scores[i] >= scores[i-1] {
			t.Errorf("cost %.2f scored %.4f >= cost %.2f scored %.4f (should be decreasing)",
				costs[i-1], scores[i-1], costs[i], scores[i])
		}
	}
}

// ============================================================================
// SearchRanker — Score Components in Results
// ============================================================================

func TestSearchRanker_ResultFields(t *testing.T) {
	ranker := NewSearchRanker()
	agent := searchTestAgent("a1", 85, caps(CapGo, CapCodeReview), 0.12, 4.7, goodPerf())

	results := ranker.Search([]*Agent{agent}, SearchQuery{
		Capabilities: []Capability{CapGo},
	})
	if len(results) != 1 {
		t.Fatal("expected 1 result")
	}
	r := results[0]
	if r.Agent.Name != "a1" {
		t.Errorf("expected a1, got %s", r.Agent.Name)
	}
	if r.TrustScore != 85 {
		t.Errorf("expected trust 85, got %d", r.TrustScore)
	}
	if r.RatingValue != 4.7 {
		t.Errorf("expected rating 4.7, got %f", r.RatingValue)
	}
	if r.CapMatchPct != 100 {
		t.Errorf("expected 100%% cap match, got %d%%", r.CapMatchPct)
	}
	if r.Rank != 1 {
		t.Errorf("expected rank 1, got %d", r.Rank)
	}
}

func TestSearchRanker_ScoreRange(t *testing.T) {
	ranker := NewSearchRanker()
	agents := []*Agent{
		searchTestAgent("a1", 80, caps(CapGo), 0.10, 4.0, goodPerf()),
		searchTestAgent("a2", 30, caps(CapGo), 0.10, 2.0, Performance{
			PrAcceptanceRate: 0.50, ReviewAccuracy: 0.50, BudgetAdherence: 0.50, Uptime: 0.50,
		}),
	}

	results := ranker.Search(agents, SearchQuery{})
	for _, r := range results {
		if r.Score < 0 || r.Score > 100 {
			t.Errorf("score %.2f out of range [0, 100]", r.Score)
		}
	}
}

func TestSearchRanker_HigherPerfScoresHigher(t *testing.T) {
	ranker := NewSearchRanker()
	highPerf := Performance{
		PrAcceptanceRate: 0.99, ReviewAccuracy: 0.98, BudgetAdherence: 0.99, Uptime: 1.0,
	}
	lowPerf := Performance{
		PrAcceptanceRate: 0.40, ReviewAccuracy: 0.50, BudgetAdherence: 0.60, Uptime: 0.70,
	}
	agents := []*Agent{
		searchTestAgent("low-perf", 70, caps(CapGo), 0.10, 4.0, lowPerf),
		searchTestAgent("high-perf", 70, caps(CapGo), 0.10, 4.0, highPerf),
	}

	results := ranker.Search(agents, SearchQuery{})
	if results[0].Agent.Name != "high-perf" {
		t.Errorf("expected high-perf first (same trust, better perf), got %s", results[0].Agent.Name)
	}
}

func TestSearchRanker_CheaperCostScoresHigher(t *testing.T) {
	ranker := NewSearchRanker()
	agents := []*Agent{
		searchTestAgent("expensive", 70, caps(CapGo), 0.30, 4.0, goodPerf()),
		searchTestAgent("cheap", 70, caps(CapGo), 0.02, 4.0, goodPerf()),
	}

	results := ranker.Search(agents, SearchQuery{})
	if results[0].Agent.Name != "cheap" {
		t.Errorf("expected cheap first (same trust/perf, lower cost), got %s", results[0].Agent.Name)
	}
}

func TestSearchRanker_TiebreakerByCost(t *testing.T) {
	ranker := NewSearchRanker()
	agents := []*Agent{
		searchTestAgent("expensive", 70, caps(CapGo), 0.20, 4.0, goodPerf()),
		searchTestAgent("cheap", 70, caps(CapGo), 0.05, 4.0, goodPerf()),
	}

	results := ranker.Search(agents, SearchQuery{})
	if results[0].Agent.Name != "cheap" {
		t.Errorf("expected cheap as tiebreaker winner, got %s", results[0].Agent.Name)
	}
}

// ============================================================================
// SearchRanker — Custom Weights
// ============================================================================

func TestSearchRanker_CustomWeights(t *testing.T) {
	// Prioritize cost above all.
	ranker := NewSearchRankerWith(WithSearchWeights(0.10, 0.10, 0.10, 0.10, 0.60))
	agents := []*Agent{
		searchTestAgent("high-trust-expensive", 95, caps(CapGo), 0.50, 5.0, goodPerf()),
		searchTestAgent("low-trust-cheap", 30, caps(CapGo), 0.01, 2.0, Performance{
			PrAcceptanceRate: 0.40, ReviewAccuracy: 0.40, BudgetAdherence: 0.40, Uptime: 0.40,
		}),
	}

	results := ranker.Search(agents, SearchQuery{})
	// With cost weighted at 0.60, the cheap agent should win despite low trust.
	if results[0].Agent.Name != "low-trust-cheap" {
		t.Errorf("expected low-trust-cheap with cost-weighted ranking, got %s", results[0].Agent.Name)
	}
}

// ============================================================================
// ScoreComparison
// ============================================================================

func TestScoreComparison(t *testing.T) {
	a := &SearchResult{Score: 85.5}
	b := &SearchResult{Score: 72.3}

	diff := ScoreComparison(a, b)
	if diff < 13.0 || diff > 14.0 {
		t.Errorf("expected ~13.2 difference, got %.2f", diff)
	}

	diff = ScoreComparison(b, a)
	if diff > -13.0 || diff > 0 {
		t.Errorf("expected negative difference, got %.2f", diff)
	}
}

// ============================================================================
// TextSearch
// ============================================================================

func TestTextSearch_NameMatch(t *testing.T) {
	ranker := NewSearchRanker()
	agents := []*Agent{
		searchTestAgent("go-expert", 80, caps(CapGo), 0.10, 4.0, goodPerf()),
		searchTestAgent("python-pro", 75, caps(CapPython), 0.10, 4.0, goodPerf()),
	}

	results := ranker.TextSearch(agents, "go", SearchQuery{})
	if len(results) != 1 {
		t.Fatalf("expected 1 result matching 'go', got %d", len(results))
	}
	if results[0].Agent.Name != "go-expert" {
		t.Errorf("expected go-expert, got %s", results[0].Agent.Name)
	}
}

func TestTextSearch_CapabilityMatch(t *testing.T) {
	ranker := NewSearchRanker()
	agents := []*Agent{
		searchTestAgent("a1", 80, caps(CapGo, CapSecurityReview), 0.10, 4.0, goodPerf()),
		searchTestAgent("a2", 70, caps(CapPython), 0.10, 4.0, goodPerf()),
	}

	results := ranker.TextSearch(agents, "security", SearchQuery{})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Agent.Name != "a1" {
		t.Errorf("expected a1, got %s", results[0].Agent.Name)
	}
}

func TestTextSearch_EmptyQuery(t *testing.T) {
	ranker := NewSearchRanker()
	agents := []*Agent{
		searchTestAgent("a1", 80, caps(CapGo), 0.10, 4.0, goodPerf()),
		searchTestAgent("a2", 70, caps(CapPython), 0.10, 4.0, goodPerf()),
	}

	results := ranker.TextSearch(agents, "", SearchQuery{})
	if len(results) != 2 {
		t.Errorf("expected 2 results (empty query = all), got %d", len(results))
	}
}

func TestTextSearch_MultiKeyword(t *testing.T) {
	ranker := NewSearchRanker()
	agents := []*Agent{
		searchTestAgent("go-pro", 80, caps(CapGo, CapCodeReview), 0.10, 4.0, goodPerf()),
		searchTestAgent("go-noob", 50, caps(CapGo, CapTesting), 0.10, 4.0, goodPerf()),
		searchTestAgent("python-pro", 70, caps(CapPython), 0.10, 4.0, goodPerf()),
	}

	// "go review" → should match agents with "go" in name AND "review" in cap or name.
	results := ranker.TextSearch(agents, "go review", SearchQuery{})
	// Only go-pro matches both "go" (name) and "review" (cap: code-review).
	if len(results) == 0 {
		t.Error("expected at least 1 result for 'go review'")
	}
}

func TestTextSearch_NoMatch(t *testing.T) {
	ranker := NewSearchRanker()
	agents := []*Agent{
		searchTestAgent("a1", 80, caps(CapGo), 0.10, 4.0, goodPerf()),
	}

	results := ranker.TextSearch(agents, "java", SearchQuery{})
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestTextSearch_CaseInsensitive(t *testing.T) {
	ranker := NewSearchRanker()
	agents := []*Agent{
		searchTestAgent("GoExpert", 80, caps(CapGo), 0.10, 4.0, goodPerf()),
	}

	results := ranker.TextSearch(agents, "goexpert", SearchQuery{})
	if len(results) != 1 {
		t.Errorf("expected 1 result (case insensitive), got %d", len(results))
	}
}

// ============================================================================
// Helper functions
// ============================================================================

func TestSplitKeywords(t *testing.T) {
	kw := splitKeywords("go python security-review")
	if len(kw) != 3 {
		t.Fatalf("expected 3 keywords, got %d", len(kw))
	}
	expected := []string{"go", "python", "security-review"}
	for i, e := range expected {
		if kw[i] != e {
			t.Errorf("keyword %d: expected %q, got %q", i, e, kw[i])
		}
	}
}

func TestSplitKeywords_Single(t *testing.T) {
	kw := splitKeywords("go")
	if len(kw) != 1 || kw[0] != "go" {
		t.Errorf("expected [go], got %v", kw)
	}
}

func TestSplitKeywords_Empty(t *testing.T) {
	kw := splitKeywords("")
	if len(kw) != 0 {
		t.Errorf("expected 0 keywords, got %d", len(kw))
	}
}

func TestContainsStr(t *testing.T) {
	if !containsStr("golang", "go") {
		t.Error("expected containsStr to be true")
	}
	if containsStr("python", "go") {
		t.Error("expected containsStr to be false")
	}
	if !containsStr("test", "") {
		t.Error("empty substring should always match")
	}
	if containsStr("ab", "abc") {
		t.Error("longer substring should not match shorter string")
	}
}

func TestLower(t *testing.T) {
	if lower("HELLO") != "hello" {
		t.Error("expected lowercase conversion")
	}
	if lower("Hello") != "hello" {
		t.Error("expected lowercase conversion")
	}
	if lower("hello") != "hello" {
		t.Error("expected lowercase conversion")
	}
}

// ============================================================================
// Empty agent pool
// ============================================================================

func TestSearchRanker_EmptyAgentPool(t *testing.T) {
	ranker := NewSearchRanker()
	results := ranker.Search(nil, SearchQuery{})
	if len(results) != 0 {
		t.Errorf("expected 0 results from empty pool, got %d", len(results))
	}
}

func TestSearchRanker_SingleAgent(t *testing.T) {
	ranker := NewSearchRanker()
	agent := searchTestAgent("only", 85, caps(CapGo), 0.10, 4.5, goodPerf())
	results := ranker.Search([]*Agent{agent}, SearchQuery{})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Rank != 1 {
		t.Errorf("expected rank 1, got %d", results[0].Rank)
	}
}
