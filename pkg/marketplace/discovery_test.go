package marketplace

import (
	"testing"
)

// ---------------------------------------------------------------------------
// TestFindAgents
// ---------------------------------------------------------------------------

func TestFindAgents_NoFiltersReturnsAllActive(t *testing.T) {
	r := &Registry{agents: map[string]*Agent{
		"agent-a": {Name: "agent-a", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{}, TrustScore: 50},
		"agent-b": {Name: "agent-b", Status: StatusActive, Capabilities: []Capability{CapTypeScript}, Budget: Budget{}, TrustScore: 80},
		"retired": {Name: "retired", Status: StatusRetired, Capabilities: []Capability{CapGo}, Budget: Budget{}, TrustScore: 90},
		"depr":    {Name: "depr", Status: StatusDeprecated, Capabilities: []Capability{CapGo}, Budget: Budget{}, TrustScore: 70},
	}}

	agents, err := r.FindAgents(SearchRequirements{})
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 active agents, got %d: %v", len(agents), names(agents))
	}
	// Agent-b has higher trust score, should be first
	if agents[0].Name != "agent-b" {
		t.Errorf("first = %q, want agent-b (highest trust)", agents[0].Name)
	}
	if agents[1].Name != "agent-a" {
		t.Errorf("second = %q, want agent-a", agents[1].Name)
	}
}

func TestFindAgents_ExcludesRetiredAndDeprecated(t *testing.T) {
	r := &Registry{agents: map[string]*Agent{
		"active":  {Name: "active", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{}},
		"retired": {Name: "retired", Status: StatusRetired, Capabilities: []Capability{CapGo}, Budget: Budget{}},
		"depr":    {Name: "depr", Status: StatusDeprecated, Capabilities: []Capability{CapGo}, Budget: Budget{}},
	}}

	agents, err := r.FindAgents(SearchRequirements{})
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 active agent, got %d", len(agents))
	}
	if agents[0].Name != "active" {
		t.Errorf("got %q, want active", agents[0].Name)
	}
}

func TestFindAgents_RequiredCapabilities(t *testing.T) {
	r := &Registry{agents: map[string]*Agent{
		"go-only":   {Name: "go-only", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{}},
		"go-and-ts": {Name: "go-and-ts", Status: StatusActive, Capabilities: []Capability{CapGo, CapTypeScript}, Budget: Budget{}},
		"ts-only":   {Name: "ts-only", Status: StatusActive, Capabilities: []Capability{CapTypeScript}, Budget: Budget{}},
	}}

	agents, err := r.FindAgents(SearchRequirements{
		RequiredCapabilities: []Capability{CapGo, CapTypeScript},
	})
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent with go+typescript, got %d", len(agents))
	}
	if agents[0].Name != "go-and-ts" {
		t.Errorf("got %q, want go-and-ts", agents[0].Name)
	}
}

func TestFindAgents_ExcludeList(t *testing.T) {
	r := &Registry{agents: map[string]*Agent{
		"agent-a": {Name: "agent-a", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{}},
		"agent-b": {Name: "agent-b", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{}},
		"agent-c": {Name: "agent-c", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{}},
	}}

	agents, err := r.FindAgents(SearchRequirements{
		ExcludeAgents: []string{"agent-b"},
	})
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents (b excluded), got %d", len(agents))
	}
	for _, a := range agents {
		if a.Name == "agent-b" {
			t.Error("agent-b should be excluded")
		}
	}
}

func TestFindAgents_MinTrust(t *testing.T) {
	r := &Registry{agents: map[string]*Agent{
		"low":  {Name: "low", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{}, TrustScore: 30},
		"mid":  {Name: "mid", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{}, TrustScore: 60},
		"high": {Name: "high", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{}, TrustScore: 90},
	}}

	agents, err := r.FindAgents(SearchRequirements{MinTrust: 60})
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents (trust >= 60), got %d", len(agents))
	}
	for _, a := range agents {
		if a.TrustScore < 60 {
			t.Errorf("%s trust=%d, want >= 60", a.Name, a.TrustScore)
		}
	}
}

func TestFindAgents_MaxCost(t *testing.T) {
	r := &Registry{agents: map[string]*Agent{
		"cheap":     {Name: "cheap", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{AverageTaskCost: 0.02}},
		"moderate":  {Name: "moderate", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{AverageTaskCost: 0.15}},
		"expensive": {Name: "expensive", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{AverageTaskCost: 0.50}},
	}}

	agents, err := r.FindAgents(SearchRequirements{MaxCost: 0.15})
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	// moderate (0.15) is <= 0.15 so included
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents (cost <= 0.15), got %d: %v", len(agents), names(agents))
	}
}

func TestFindAgents_TierFilter(t *testing.T) {
	r := &Registry{agents: map[string]*Agent{
		"pro-a":   {Name: "pro-a", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{}, Tier: TierPro},
		"pro-b":   {Name: "pro-b", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{}, Tier: TierPro},
		"flash-a": {Name: "flash-a", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{}, Tier: TierFlash},
	}}

	agents, err := r.FindAgents(SearchRequirements{Tier: TierFlash})
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 flash agent, got %d", len(agents))
	}
	if agents[0].Name != "flash-a" {
		t.Errorf("got %q, want flash-a", agents[0].Name)
	}
}

func TestFindAgents_Limit(t *testing.T) {
	r := &Registry{agents: map[string]*Agent{
		"a": {Name: "a", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{}, TrustScore: 10},
		"b": {Name: "b", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{}, TrustScore: 20},
		"c": {Name: "c", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{}, TrustScore: 30},
		"d": {Name: "d", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{}, TrustScore: 40},
		"e": {Name: "e", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{}, TrustScore: 50},
	}}

	agents, err := r.FindAgents(SearchRequirements{Limit: 3})
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if len(agents) != 3 {
		t.Fatalf("expected 3 agents (limit=3), got %d", len(agents))
	}
	// Should be top 3 by trust score (e, d, c)
	want := []string{"e", "d", "c"}
	for i, name := range want {
		if agents[i].Name != name {
			t.Errorf("position %d = %q, want %q", i, agents[i].Name, name)
		}
	}
}

func TestFindAgents_RankingByMatchPercent(t *testing.T) {
	r := &Registry{agents: map[string]*Agent{
		"full":    {Name: "full", Status: StatusActive, Capabilities: []Capability{CapGo, CapTypeScript, CapCodeReview}, Budget: Budget{}, TrustScore: 50},
		"partial": {Name: "partial", Status: StatusActive, Capabilities: []Capability{CapGo, CapTypeScript}, Budget: Budget{}, TrustScore: 90},
	}}

	agents, err := r.FindAgents(SearchRequirements{
		RequiredCapabilities:  []Capability{CapGo, CapTypeScript},
		PreferredCapabilities: []Capability{CapCodeReview},
	})
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	// "full" has 100% match, "partial" has 66% match (2/3)
	// matchPercent should rank before trust score
	if agents[0].Name != "full" {
		t.Errorf("first = %q, want full (100%% match vs 66%%, despite lower trust)",
			agents[0].Name)
	}
}

func TestFindAgents_RankingByTrustThenCost(t *testing.T) {
	r := &Registry{agents: map[string]*Agent{
		"agent-a": {Name: "agent-a", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{AverageTaskCost: 0.10}, TrustScore: 80},
		"agent-b": {Name: "agent-b", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{AverageTaskCost: 0.05}, TrustScore: 80},
		"agent-c": {Name: "agent-c", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{AverageTaskCost: 0.02}, TrustScore: 60},
	}}

	agents, err := r.FindAgents(SearchRequirements{})
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	// Same match % (all have CapGo, no reqs), so trust desc: a(80), b(80), c(60)
	// a and b have same trust, cost asc: b(0.05) before a(0.10)
	want := []string{"agent-b", "agent-a", "agent-c"}
	for i, name := range want {
		if agents[i].Name != name {
			t.Errorf("position %d = %q, want %q", i, agents[i].Name, name)
		}
	}
}

func TestFindAgents_ZeroMinTrustIncludesAll(t *testing.T) {
	r := &Registry{agents: map[string]*Agent{
		"zero": {Name: "zero", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{}, TrustScore: 0},
		"neg":  {Name: "neg", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{}, TrustScore: -5},
	}}

	agents, err := r.FindAgents(SearchRequirements{MinTrust: 0})
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents (MinTrust=0 should include all), got %d", len(agents))
	}
}

func TestFindAgents_ZeroMaxCostIncludesAll(t *testing.T) {
	r := &Registry{agents: map[string]*Agent{
		"free":   {Name: "free", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{AverageTaskCost: 0}},
		"costly": {Name: "costly", Status: StatusActive, Capabilities: []Capability{CapGo}, Budget: Budget{AverageTaskCost: 100.0}},
	}}

	agents, err := r.FindAgents(SearchRequirements{MaxCost: 0})
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents (MaxCost=0 should include all), got %d", len(agents))
	}
}

// ---------------------------------------------------------------------------
// TestLoadBalance
// ---------------------------------------------------------------------------

func TestLoadBalance_ReturnsCopy(t *testing.T) {
	original := []*Agent{
		{Name: "a", Budget: Budget{WeeklyLimit: 100, AverageTaskCost: 10}},
		{Name: "b", Budget: Budget{WeeklyLimit: 100, AverageTaskCost: 5}},
	}

	result := LoadBalance(original, nil)
	if len(result) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(result))
	}
	// Mutating result should not affect original
	result[0] = nil
	if original[0] == nil {
		t.Error("original was mutated (shallow copy issue)")
	}
}

func TestLoadBalance_FewerActiveTasksFirst(t *testing.T) {
	agents := []*Agent{
		{Name: "busy", Budget: Budget{WeeklyLimit: 100, AverageTaskCost: 10}},
		{Name: "idle", Budget: Budget{WeeklyLimit: 100, AverageTaskCost: 10}},
	}

	counts := map[string]int{
		"busy": 15,
		"idle": 2,
	}

	result := LoadBalance(agents, counts)
	if result[0].Name != "idle" {
		t.Errorf("first = %q, want idle (fewer active tasks)", result[0].Name)
	}
}

func TestLoadBalance_TiebreakerByBudgetUtilization(t *testing.T) {
	agents := []*Agent{
		{Name: "cheap-headroom", Budget: Budget{WeeklyLimit: 100, AverageTaskCost: 10}}, // util=0.1
		{Name: "expensive", Budget: Budget{WeeklyLimit: 100, AverageTaskCost: 90}},      // util=0.9
	}

	counts := map[string]int{
		"cheap-headroom": 5,
		"expensive":      5,
	}

	result := LoadBalance(agents, counts)
	if result[0].Name != "cheap-headroom" {
		t.Errorf("first = %q, want cheap-headroom (lower budget utilization)", result[0].Name)
	}
}

func TestLoadBalance_ZeroActiveCounts(t *testing.T) {
	agents := []*Agent{
		{Name: "a", Budget: Budget{WeeklyLimit: 100, AverageTaskCost: 50}},
		{Name: "b", Budget: Budget{WeeklyLimit: 100, AverageTaskCost: 10}},
	}

	result := LoadBalance(agents, nil) // nil map → all 0
	// All have 0 active tasks, tiebreaker = budget utilization: b(0.1) before a(0.5)
	if result[0].Name != "b" {
		t.Errorf("first = %q, want b (lower budget utilization on tie)", result[0].Name)
	}
}

// ---------------------------------------------------------------------------
// TestMatchPercent
// ---------------------------------------------------------------------------

func TestMatchPercent_EmptyRequirements(t *testing.T) {
	a := &Agent{Capabilities: []Capability{CapGo}}
	pct := matchPercent(a, SearchRequirements{})
	if pct != 100 {
		t.Errorf("matchPercent with no requirements = %d, want 100", pct)
	}
}

func TestMatchPercent_FullMatch(t *testing.T) {
	a := &Agent{Capabilities: []Capability{CapGo, CapTypeScript, CapCodeReview}}
	pct := matchPercent(a, SearchRequirements{
		RequiredCapabilities:  []Capability{CapGo, CapTypeScript},
		PreferredCapabilities: []Capability{CapCodeReview},
	})
	if pct != 100 {
		t.Errorf("matchPercent full match = %d, want 100", pct)
	}
}

func TestMatchPercent_PartialMatch(t *testing.T) {
	a := &Agent{Capabilities: []Capability{CapGo}}
	pct := matchPercent(a, SearchRequirements{
		RequiredCapabilities:  []Capability{CapGo},
		PreferredCapabilities: []Capability{CapTypeScript},
	})
	if pct != 50 {
		t.Errorf("matchPercent 1/2 = %d, want 50", pct)
	}
}

func TestMatchPercent_NoMatch(t *testing.T) {
	a := &Agent{Capabilities: []Capability{CapDocs}}
	pct := matchPercent(a, SearchRequirements{
		RequiredCapabilities: []Capability{CapGo, CapTypeScript},
	})
	if pct != 0 {
		t.Errorf("matchPercent no match = %d, want 0", pct)
	}
}

func TestMatchPercent_OnlyRequired(t *testing.T) {
	a := &Agent{Capabilities: []Capability{CapGo, CapTypeScript}}
	pct := matchPercent(a, SearchRequirements{
		RequiredCapabilities: []Capability{CapGo, CapTypeScript},
	})
	if pct != 100 {
		t.Errorf("matchPercent only required = %d, want 100", pct)
	}
}

func TestMatchPercent_OnlyPreferred(t *testing.T) {
	a := &Agent{Capabilities: []Capability{CapGo}}
	pct := matchPercent(a, SearchRequirements{
		PreferredCapabilities: []Capability{CapGo},
	})
	if pct != 100 {
		t.Errorf("matchPercent only preferred = %d, want 100", pct)
	}
}

func TestMatchPercent_IntegerDivision(t *testing.T) {
	// 2 of 3 capabilities = 66% (integer math)
	a := &Agent{Capabilities: []Capability{CapGo, CapTypeScript}}
	pct := matchPercent(a, SearchRequirements{
		RequiredCapabilities:  []Capability{CapGo},
		PreferredCapabilities: []Capability{CapTypeScript, CapCodeReview},
	})
	if pct != 66 {
		t.Errorf("matchPercent 2/3 = %d, want 66 (integer division)", pct)
	}
}

// ---------------------------------------------------------------------------
// TestBudgetUtilization
// ---------------------------------------------------------------------------

func TestBudgetUtilization_ZeroWeeklyLimit(t *testing.T) {
	a := &Agent{Budget: Budget{WeeklyLimit: 0, AverageTaskCost: 10}}
	u := budgetUtilization(a)
	if u != 1.0 {
		t.Errorf("budgetUtilization zero weekly limit = %f, want 1.0", u)
	}
}

func TestBudgetUtilization_NormalCase(t *testing.T) {
	a := &Agent{Budget: Budget{WeeklyLimit: 100, AverageTaskCost: 25}}
	u := budgetUtilization(a)
	if u != 0.25 {
		t.Errorf("budgetUtilization normal = %f, want 0.25", u)
	}
}

func TestBudgetUtilization_CappedAtOne(t *testing.T) {
	a := &Agent{Budget: Budget{WeeklyLimit: 50, AverageTaskCost: 100}}
	u := budgetUtilization(a)
	if u != 1.0 {
		t.Errorf("budgetUtilization capped = %f, want 1.0", u)
	}
}

func TestBudgetUtilization_ZeroCost(t *testing.T) {
	a := &Agent{Budget: Budget{WeeklyLimit: 100, AverageTaskCost: 0}}
	u := budgetUtilization(a)
	if u != 0.0 {
		t.Errorf("budgetUtilization zero cost = %f, want 0.0", u)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func names(agents []*Agent) []string {
	out := make([]string, len(agents))
	for i, a := range agents {
		out[i] = a.Name
	}
	return out
}
