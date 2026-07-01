package marketplace

import (
	"strings"
	"testing"
)

func testAgent(name string, trust int, tier Tier) *Agent {
	return &Agent{
		Name:         name,
		DisplayName:  strings.ToUpper(name),
		Status:       StatusActive,
		Tier:         tier,
		TrustScore:   trust,
		Capabilities: []Capability{CapGo, CapTesting},
		Budget: Budget{
			AverageTaskCost: 0.12,
			CostProfile:     CostMedium,
		},
		Performance: Performance{
			TasksCompleted:   100,
			PrAcceptanceRate: 0.90,
		},
		Ratings: Ratings{
			Average: 4.5,
			Count:   10,
		},
	}
}

func TestFormatAgentTable(t *testing.T) {
	agents := []*Agent{
		testAgent("alpha", 85, TierPro),
		testAgent("beta", 45, TierFlash),
		testAgent("gamma", 92, TierPro),
	}

	result := FormatAgentTable(agents)

	if !strings.Contains(result, "NAME") {
		t.Error("missing header row")
	}
	if !strings.Contains(result, "alpha") {
		t.Error("missing agent alpha")
	}
	if !strings.Contains(result, "3 agents listed") {
		t.Error("missing agent count")
	}

	// Check sorted by trust descending
	gammaIdx := strings.Index(result, "gamma")
	alphaIdx := strings.Index(result, "alpha")
	betaIdx := strings.Index(result, "beta")
	if gammaIdx >= alphaIdx || alphaIdx >= betaIdx {
		t.Error("agents not sorted by trust descending (gamma=92 should come before alpha=85 before beta=45)")
	}
}

func TestFormatAgentTable_Empty(t *testing.T) {
	result := FormatAgentTable(nil)
	if !strings.Contains(result, "No agents found") {
		t.Errorf("expected 'No agents found', got %q", result)
	}
}

func TestFormatAgentDetail_Normal(t *testing.T) {
	a := testAgent("wojons", 85, TierPro)
	a.Reviews = []Review{
		{Author: "bane", Rating: 5, Comment: "Great work", Date: "2026-06-15"},
	}
	result := FormatAgentDetail(a)

	if !strings.Contains(result, "AGENT: wojons") {
		t.Error("missing agent header")
	}
	if !strings.Contains(result, "trust=85") {
		t.Error("missing trust score")
	}
	if !strings.Contains(result, "Capabilities: go, testing") {
		t.Error("missing capabilities")
	}
	if !strings.Contains(result, "Acceptance rate: 90%") {
		t.Error("missing acceptance rate")
	}
	if !strings.Contains(result, "RECENT REVIEWS") {
		t.Error("missing recent reviews section")
	}
}

func TestFormatAgentDetail_LowTrust(t *testing.T) {
	a := testAgent("riskagent", 15, TierFlash)
	result := FormatAgentDetail(a)

	if !strings.Contains(result, "⚠️") {
		t.Error("missing trust warning emoji for low-trust agent")
	}
	if !strings.Contains(result, "WARNING: Trust score has been below 30") {
		t.Error("missing warning text")
	}
}

func TestFormatAgentDetail_Deprecated(t *testing.T) {
	a := testAgent("oldagent", 25, TierFlash)
	a.Status = StatusDeprecated
	a.DeprecatedAt = "2026-05-01"
	result := FormatAgentDetail(a)

	if !strings.Contains(result, "DEPRECATED") {
		t.Error("missing deprecated status")
	}
	if !strings.Contains(result, "since 2026-05-01") {
		t.Error("missing deprecation date")
	}
}

func TestFormatRatingSubmission(t *testing.T) {
	result := FormatRatingSubmission("wojons", 5, "bane", "Excellent", 4.8, 24, true)

	if !strings.Contains(result, "RATING SUBMITTED") {
		t.Error("missing header")
	}
	if !strings.Contains(result, "Agent:  wojons") {
		t.Error("missing agent name")
	}
	if !strings.Contains(result, "★★★★★ (5/5)") {
		t.Error("missing star rating")
	}
	if !strings.Contains(result, "bane (human) ✅") {
		t.Error("missing human verification mark")
	}
	if !strings.Contains(result, "4.8 (24 reviews)") {
		t.Error("missing new average")
	}
}

func TestFormatRatingSubmission_NonHuman(t *testing.T) {
	result := FormatRatingSubmission("agent1", 3, "bot1", "", 3.5, 5, false)

	if strings.Contains(result, "✅") {
		t.Error("human mark should not appear for non-human")
	}
	if !strings.Contains(result, "3.5 (5 reviews)") {
		t.Error("missing average")
	}
}

func TestFormatDeprecationNotice(t *testing.T) {
	result := FormatDeprecationNotice("dtoole", 27, 23, 7)

	if !strings.Contains(result, "below 30 for 23 days") {
		t.Error("missing days below threshold")
	}
	if !strings.Contains(result, "Auto-deprecation in 7 days") {
		t.Error("missing days until deprecation")
	}
}

func TestFormatStarRating(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{5, "★★★★★"},
		{4, "★★★★☆"},
		{3, "★★★☆☆"},
		{2, "★★☆☆☆"},
		{1, "★☆☆☆☆"},
		{0, "☆☆☆☆☆"},
		{-1, "☆☆☆☆☆"},
		{6, "★★★★★"},
	}
	for _, tt := range tests {
		got := formatStarRating(tt.input)
		if got != tt.expected {
			t.Errorf("formatStarRating(%d) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestFormatStarRatingFloat(t *testing.T) {
	tests := []struct {
		input    float64
		expected string
	}{
		{5.0, "★★★★★"},
		{4.7, "★★★★½"},
		{4.0, "★★★★☆"},
		{3.5, "★★★½☆"},
		{3.0, "★★★☆☆"},
		{0.0, "☆☆☆☆☆"},
		{-1, "☆☆☆☆☆"},
		{6, "★★★★★"},
	}
	for _, tt := range tests {
		got := formatStarRatingFloat(tt.input)
		if got != tt.expected {
			t.Errorf("formatStarRatingFloat(%.1f) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestFormatStarRatingShort(t *testing.T) {
	got := formatStarRatingShort(4.7)
	if got != "★4.7" {
		t.Errorf("formatStarRatingShort(4.7) = %q, want %q", got, "★4.7")
	}
}

func TestFormatCostPerTask(t *testing.T) {
	got := formatCostPerTask(0.038)
	if got != "$0.04" {
		t.Errorf("formatCostPerTask(0.038) = %q, want %q", got, "$0.04")
	}
}

func TestFormatAgentsByStatus(t *testing.T) {
	agents := []*Agent{
		testAgent("alpha", 85, TierPro),
		{Name: "beta", Status: StatusDeprecated, Tier: TierFlash, TrustScore: 25},
		{Name: "gamma", Status: StatusRetired, Tier: TierFlash, TrustScore: 10},
	}
	result := FormatAgentsByStatus(agents)

	if !strings.Contains(result, "=== ACTIVE (1) ===") {
		t.Error("missing active group header")
	}
	if !strings.Contains(result, "=== DEPRECATED (1) ===") {
		t.Error("missing deprecated group header")
	}
	if !strings.Contains(result, "=== RETIRED (1) ===") {
		t.Error("missing retired group header")
	}
}

func TestFormatTrustDistribution(t *testing.T) {
	agents := []*Agent{
		testAgent("a1", 15, TierFlash), // 0-19
		testAgent("a2", 25, TierFlash), // 20-39
		testAgent("a3", 55, TierFlash), // 40-59
		testAgent("a4", 75, TierPro),   // 60-79
		testAgent("a5", 95, TierPro),   // 80-100
		testAgent("a6", 10, TierFlash), // 0-19
	}
	result := FormatTrustDistribution(agents)

	if !strings.Contains(result, "TRUST DISTRIBUTION") {
		t.Error("missing header")
	}
	if !strings.Contains(result, "0-19") {
		t.Error("missing bin 0-19")
	}
	if !strings.Contains(result, "80-100") {
		t.Error("missing bin 80-100")
	}
}

func TestFormatSearchResults(t *testing.T) {
	results := []*Agent{
		testAgent("alpha", 85, TierPro),
	}
	query := SearchQuery{MinTrust: 70}
	result := FormatSearchResults(results, query)

	if !strings.Contains(result, "1 agent(s) found") {
		t.Error("missing result count")
	}
}

func TestFormatSearchResults_Empty(t *testing.T) {
	result := FormatSearchResults(nil, SearchQuery{})
	if !strings.Contains(result, "No agents found") {
		t.Error("expected empty message")
	}
}

func TestFormatRegistrySummary(t *testing.T) {
	agents := []*Agent{
		testAgent("alpha", 85, TierPro),
		{Name: "beta", Status: StatusDeprecated, Tier: TierFlash, TrustScore: 25},
	}
	result := FormatRegistrySummary(agents)

	if !strings.Contains(result, "MARKETPLACE SUMMARY") {
		t.Error("missing header")
	}
	if !strings.Contains(result, "Total agents: 2") {
		t.Error("missing total count")
	}
	if !strings.Contains(result, "active=1, deprecated=1, retired=0") {
		t.Error("missing status breakdown")
	}
}

func TestFormatRegistrySummary_Empty(t *testing.T) {
	result := FormatRegistrySummary(nil)
	if !strings.Contains(result, "empty") {
		t.Errorf("expected empty message, got %q", result)
	}
}
