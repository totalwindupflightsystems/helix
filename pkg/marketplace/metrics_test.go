package marketplace

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Tests for MetricsCollector (spec §14 — Observability)
// ---------------------------------------------------------------------------

func makeTestRegistry(t *testing.T, agents ...*Agent) *Registry {
	t.Helper()
	r := &Registry{agents: make(map[string]*Agent)}
	for _, a := range agents {
		r.agents[a.Name] = a
	}
	return r
}

func TestNewMetricsCollector(t *testing.T) {
	m := NewMetricsCollector()
	if m == nil {
		t.Fatal("NewMetricsCollector returned nil")
	}
	if m.QueryCount("capability") != 0 {
		t.Errorf("new collector queryCount should be 0, got %d", m.QueryCount("capability"))
	}
	if m.RatingsTotal() != 0 {
		t.Errorf("new collector ratingsTotal should be 0, got %d", m.RatingsTotal())
	}
	if m.AssignmentCount("agent1") != 0 {
		t.Errorf("new collector assignmentCount should be 0, got %d", m.AssignmentCount("agent1"))
	}
}

func TestRecordQuery(t *testing.T) {
	m := NewMetricsCollector()

	m.RecordQuery("capability")
	m.RecordQuery("capability")
	m.RecordQuery("trust")
	m.RecordQuery("cost")

	if m.QueryCount("capability") != 2 {
		t.Errorf("capability queries = %d, want 2", m.QueryCount("capability"))
	}
	if m.QueryCount("trust") != 1 {
		t.Errorf("trust queries = %d, want 1", m.QueryCount("trust"))
	}
	if m.QueryCount("cost") != 1 {
		t.Errorf("cost queries = %d, want 1", m.QueryCount("cost"))
	}
}

func TestRecordRating(t *testing.T) {
	m := NewMetricsCollector()

	m.RecordRating()
	m.RecordRating()
	m.RecordRating()

	if m.RatingsTotal() != 3 {
		t.Errorf("ratingsTotal = %d, want 3", m.RatingsTotal())
	}
}

func TestRecordAssignment(t *testing.T) {
	m := NewMetricsCollector()

	m.RecordAssignment("agent-alpha")
	m.RecordAssignment("agent-beta")
	m.RecordAssignment("agent-alpha")

	if m.AssignmentCount("agent-alpha") != 2 {
		t.Errorf("agent-alpha assignments = %d, want 2", m.AssignmentCount("agent-alpha"))
	}
	if m.AssignmentCount("agent-beta") != 1 {
		t.Errorf("agent-beta assignments = %d, want 1", m.AssignmentCount("agent-beta"))
	}
}

func TestAgentsByStatus(t *testing.T) {
	r := makeTestRegistry(t,
		&Agent{Name: "a1", Status: StatusActive, TrustScore: 80},
		&Agent{Name: "a2", Status: StatusActive, TrustScore: 60},
		&Agent{Name: "a3", Status: StatusDeprecated, TrustScore: 15},
		&Agent{Name: "a4", Status: StatusRetired, TrustScore: 0},
	)

	counts := AgentsByStatus(r)
	if counts[StatusActive] != 2 {
		t.Errorf("active count = %d, want 2", counts[StatusActive])
	}
	if counts[StatusDeprecated] != 1 {
		t.Errorf("deprecated count = %d, want 1", counts[StatusDeprecated])
	}
	if counts[StatusRetired] != 1 {
		t.Errorf("retired count = %d, want 1", counts[StatusRetired])
	}
}

func TestAgentsByStatus_Empty(t *testing.T) {
	r := makeTestRegistry(t)
	counts := AgentsByStatus(r)
	if counts[StatusActive] != 0 {
		t.Errorf("empty active count = %d, want 0", counts[StatusActive])
	}
}

func TestTrustScoreGauges(t *testing.T) {
	r := makeTestRegistry(t,
		&Agent{Name: "a1", Status: StatusActive, TrustScore: 80},
		&Agent{Name: "a2", Status: StatusDeprecated, TrustScore: 30},
		&Agent{Name: "a3", Status: StatusRetired, TrustScore: 99},
	)

	scores := TrustScoreGauges(r)

	// Retired agent should be excluded.
	if len(scores) != 2 {
		t.Fatalf("trust scores count = %d, want 2 (retired excluded)", len(scores))
	}
	if scores["a1"] != 80 {
		t.Errorf("a1 trust = %d, want 80", scores["a1"])
	}
	if scores["a2"] != 30 {
		t.Errorf("a2 trust = %d, want 30", scores["a2"])
	}
	if _, exists := scores["a3"]; exists {
		t.Errorf("retired agent a3 should not appear in trust gauges")
	}
}

func TestCollect_BasicFormat(t *testing.T) {
	r := makeTestRegistry(t,
		&Agent{Name: "alpha", Status: StatusActive, TrustScore: 85},
		&Agent{Name: "beta", Status: StatusActive, TrustScore: 50},
	)

	m := NewMetricsCollector()
	m.RecordQuery("capability")
	m.RecordRating()

	output := m.Collect(r)

	// Must contain all 3 metric types from the spec.
	requiredSubstrings := []string{
		"helix_marketplace_agents_total",
		"helix_marketplace_trust_score",
		"helix_marketplace_queries_total",
		"helix_marketplace_ratings_total",
	}
	for _, s := range requiredSubstrings {
		if !strings.Contains(output, s) {
			t.Errorf("Collect output missing %q\nOutput:\n%s", s, output)
		}
	}
}

func TestCollect_HasHELPAndTYPE(t *testing.T) {
	r := makeTestRegistry(t,
		&Agent{Name: "x", Status: StatusActive, TrustScore: 50},
	)
	m := NewMetricsCollector()
	output := m.Collect(r)

	if !strings.Contains(output, "# HELP") {
		t.Errorf("Collect output missing HELP comment")
	}
	if !strings.Contains(output, "# TYPE") {
		t.Errorf("Collect output missing TYPE comment")
	}
	if !strings.Contains(output, "gauge") {
		t.Errorf("Collect output missing gauge type")
	}
	if !strings.Contains(output, "counter") {
		t.Errorf("Collect output missing counter type")
	}
}

func TestCollect_StatusCounts(t *testing.T) {
	r := makeTestRegistry(t,
		&Agent{Name: "a1", Status: StatusActive, TrustScore: 80},
		&Agent{Name: "a2", Status: StatusActive, TrustScore: 60},
		&Agent{Name: "a3", Status: StatusDeprecated, TrustScore: 15},
	)

	m := NewMetricsCollector()
	output := m.Collect(r)

	if !strings.Contains(output, `status="active"`) {
		t.Errorf("missing active status label")
	}
	if !strings.Contains(output, `status="deprecated"`) {
		t.Errorf("missing deprecated status label")
	}
	if !strings.Contains(output, `status="retired"`) {
		t.Errorf("missing retired status label")
	}

	// Check active count appears as value.
	if !strings.Contains(output, `status="active"} 2`) {
		t.Errorf("active count wrong in output:\n%s", output)
	}
}

func TestCollect_TrustScores(t *testing.T) {
	r := makeTestRegistry(t,
		&Agent{Name: "alpha", Status: StatusActive, TrustScore: 85},
		&Agent{Name: "beta", Status: StatusActive, TrustScore: 50},
	)

	m := NewMetricsCollector()
	output := m.Collect(r)

	if !strings.Contains(output, `agent="alpha"`) {
		t.Errorf("missing alpha agent label")
	}
	if !strings.Contains(output, `agent="beta"`) {
		t.Errorf("missing beta agent label")
	}
}

func TestCollect_QueryCounters(t *testing.T) {
	r := makeTestRegistry(t)
	m := NewMetricsCollector()

	m.RecordQuery("capability")
	m.RecordQuery("capability")
	m.RecordQuery("trust")

	output := m.Collect(r)

	if !strings.Contains(output, `filter="capability"`) {
		t.Errorf("missing capability filter label")
	}
	if !strings.Contains(output, `filter="trust"`) {
		t.Errorf("missing trust filter label")
	}
}

func TestCollect_AssignmentCounters(t *testing.T) {
	r := makeTestRegistry(t)
	m := NewMetricsCollector()

	m.RecordAssignment("agent-x")
	m.RecordAssignment("agent-x")
	m.RecordAssignment("agent-y")

	output := m.Collect(r)

	if !strings.Contains(output, "helix_marketplace_assignments_total") {
		t.Errorf("missing assignments_total metric")
	}
	if !strings.Contains(output, `agent="agent-x"`) {
		t.Errorf("missing agent-x assignment label")
	}
}

func TestCollect_RatingsCounter(t *testing.T) {
	r := makeTestRegistry(t)
	m := NewMetricsCollector()

	for i := 0; i < 5; i++ {
		m.RecordRating()
	}

	output := m.Collect(r)

	if !strings.Contains(output, "helix_marketplace_ratings_total 5") {
		t.Errorf("ratings_total value wrong in output:\n%s", output)
	}
}

func TestCollect_EmptyRegistry(t *testing.T) {
	r := makeTestRegistry(t)
	m := NewMetricsCollector()

	output := m.Collect(r)

	// Should still produce all status gauges with value 0.
	if !strings.Contains(output, `status="active"} 0`) {
		t.Errorf("empty registry should show active count 0")
	}
}

func TestReset(t *testing.T) {
	m := NewMetricsCollector()

	m.RecordQuery("capability")
	m.RecordRating()
	m.RecordAssignment("agent-a")

	m.Reset()

	if m.QueryCount("capability") != 0 {
		t.Errorf("after Reset, queryCount should be 0")
	}
	if m.RatingsTotal() != 0 {
		t.Errorf("after Reset, ratingsTotal should be 0")
	}
	if m.AssignmentCount("agent-a") != 0 {
		t.Errorf("after Reset, assignmentCount should be 0")
	}
}

func TestLastCollect(t *testing.T) {
	r := makeTestRegistry(t)
	m := NewMetricsCollector()

	if !m.LastCollect().IsZero() {
		t.Errorf("LastCollect should be zero before first Collect")
	}

	m.Collect(r)

	if m.LastCollect().IsZero() {
		t.Errorf("LastCollect should be set after Collect")
	}
}

func TestCollect_ThreadSafe(t *testing.T) {
	r := makeTestRegistry(t,
		&Agent{Name: "concurrent-agent", Status: StatusActive, TrustScore: 50},
	)
	m := NewMetricsCollector()

	done := make(chan bool)

	// Writer goroutine.
	go func() {
		for i := 0; i < 100; i++ {
			m.RecordQuery("capability")
			m.RecordRating()
			m.RecordAssignment("concurrent-agent")
		}
		done <- true
	}()

	// Reader goroutine.
	go func() {
		for i := 0; i < 100; i++ {
			_ = m.Collect(r)
		}
		done <- true
	}()

	<-done
	<-done

	// Verify final state is consistent.
	if m.QueryCount("capability") != 100 {
		t.Errorf("queryCount after concurrent ops = %d, want 100", m.QueryCount("capability"))
	}
	if m.RatingsTotal() != 100 {
		t.Errorf("ratingsTotal after concurrent ops = %d, want 100", m.RatingsTotal())
	}
}

func TestCollect_AllSpecMetricsPresent(t *testing.T) {
	r := makeTestRegistry(t,
		&Agent{Name: "a", Status: StatusActive, TrustScore: 70},
	)
	m := NewMetricsCollector()
	m.RecordQuery("capability")
	m.RecordRating()
	m.RecordAssignment("a")

	output := m.Collect(r)

	// All 5 metrics from spec §14 must be present.
	allMetrics := []string{
		"helix_marketplace_agents_total",
		"helix_marketplace_trust_score",
		"helix_marketplace_queries_total",
		"helix_marketplace_ratings_total",
		"helix_marketplace_assignments_total",
	}
	for _, metric := range allMetrics {
		if !strings.Contains(output, metric) {
			t.Errorf("spec §14 metric missing: %s", metric)
		}
	}
}
