package learning

import (
	"strings"
	"testing"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/incident"
)

func makeIncident(id, agent, severity string, ts time.Time) *incident.Incident {
	return &incident.Incident{
		ID:        id,
		AgentID:   agent,
		Severity:  severity,
		Timestamp: ts,
	}
}

func makePattern(id, agent string, cats []incident.FileCategory, ct incident.ChangeType, lessons []string) *incident.IncidentPattern {
	return &incident.IncidentPattern{
		ID:             id,
		AgentID:        agent,
		Categories:     cats,
		ChangeType:     ct,
		Severity:       "medium",
		Keywords:       []string{"test", "bug"},
		Description:    "test pattern " + id,
		RootCause:      "test",
		LessonsLearned: lessons,
		Timestamp:      time.Now(),
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Category clustering tests
// ─────────────────────────────────────────────────────────────────────────────

func TestDiscoverCategoryClusters(t *testing.T) {
	// Seed: auth has 10 patterns, all other categories have 1 each.
	var patterns []*incident.IncidentPattern
	for i := 0; i < 10; i++ {
		patterns = append(patterns, makePattern("p-auth-"+string(rune('a'+i)), "glm:agent1",
			[]incident.FileCategory{incident.CategoryAuth}, incident.ChangeNew, nil))
	}
	patterns = append(patterns, makePattern("p-db-1", "glm:agent2",
		[]incident.FileCategory{incident.CategoryDatabase}, incident.ChangeNew, nil))
	patterns = append(patterns, makePattern("p-api-1", "glm:agent3",
		[]incident.FileCategory{incident.CategoryAPI}, incident.ChangeNew, nil))
	patterns = append(patterns, makePattern("p-cfg-1", "glm:agent4",
		[]incident.FileCategory{incident.CategoryConfig}, incident.ChangeNew, nil))
	patterns = append(patterns, makePattern("p-iac-1", "glm:agent5",
		[]incident.FileCategory{incident.CategoryIaC}, incident.ChangeNew, nil))

	var incidents []*incident.Incident
	for i := 0; i < 13; i++ {
		incidents = append(incidents, makeIncident("inc-"+string(rune('a'+i)), "glm:agent1", "medium", time.Now()))
	}

	m := NewPatternMiner(
		NewIncidentSliceSource(incidents),
		NewPatternSliceSource(patterns),
	)

	results := m.Discover()

	// Auth should be flagged since it has 10 patterns vs avg of (10+1+1+1+1)/5 = 2.8
	// ratio = 10/2.8 ≈ 3.6, which is > 1.5
	found := false
	for _, r := range results {
		if r.PatternType == PatternCategoryClustering {
			found = true
			if !containsCategory(r.Categories, incident.CategoryAuth) {
				t.Errorf("expected auth category in clustering result, got %v", r.Categories)
			}
		}
	}
	if !found {
		t.Error("expected category clustering pattern for auth")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Provider correlation tests
// ─────────────────────────────────────────────────────────────────────────────

func TestDiscoverProviderCorrelations(t *testing.T) {
	// Provider "zai": 10 patterns, 8 incidents → high rate.
	// Provider "minimax": 10 patterns, 2 incidents → low rate.
	var patterns []*incident.IncidentPattern
	for i := 0; i < 10; i++ {
		patterns = append(patterns, makePattern("zai-"+string(rune('a'+i)), "zai:glm:agent"+string(rune('a'+i)),
			[]incident.FileCategory{incident.CategoryAuth}, incident.ChangeNew, nil))
	}
	for i := 0; i < 10; i++ {
		patterns = append(patterns, makePattern("mm-"+string(rune('a'+i)), "minimax:agent"+string(rune('a'+i)),
			[]incident.FileCategory{incident.CategoryDatabase}, incident.ChangeModify, nil))
	}

	// zai agents have 8 incidents, minimax agents have 2.
	// Agent IDs must match pattern agent IDs for provider extraction.
	var incidents []*incident.Incident
	for i := 0; i < 8; i++ {
		incidents = append(incidents, makeIncident("inc-zai-"+string(rune('a'+i)), "zai:glm:agent"+string(rune('a'+i)), "medium", time.Now()))
	}
	for i := 0; i < 2; i++ {
		incidents = append(incidents, makeIncident("inc-mm-"+string(rune('a'+i)), "minimax:agent"+string(rune('a'+i)), "medium", time.Now()))
	}

	m := NewPatternMiner(
		NewIncidentSliceSource(incidents),
		NewPatternSliceSource(patterns),
	)

	results := m.Discover()

	// zai should be flagged for higher incident rate.
	found := false
	for _, r := range results {
		if r.PatternType == PatternAgentProviderCorrelation {
			if strings.Contains(r.Title, "zai") {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected provider correlation pattern for zai (higher incident rate)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Change type risk tests
// ─────────────────────────────────────────────────────────────────────────────

func TestDiscoverChangeTypeRisk(t *testing.T) {
	// Migration: 10 patterns, Modify: 2, New: 2
	var patterns []*incident.IncidentPattern
	for i := 0; i < 10; i++ {
		patterns = append(patterns, makePattern("mig-"+string(rune('a'+i)), "glm:agent1",
			[]incident.FileCategory{incident.CategoryDatabase}, incident.ChangeMigration, nil))
	}
	patterns = append(patterns, makePattern("mod-1", "glm:agent2",
		[]incident.FileCategory{incident.CategoryAPI}, incident.ChangeModify, nil))
	patterns = append(patterns, makePattern("mod-2", "glm:agent3",
		[]incident.FileCategory{incident.CategoryAuth}, incident.ChangeModify, nil))
	patterns = append(patterns, makePattern("new-1", "glm:agent4",
		[]incident.FileCategory{incident.CategoryInfra}, incident.ChangeNew, nil))
	patterns = append(patterns, makePattern("new-2", "glm:agent5",
		[]incident.FileCategory{incident.CategoryConfig}, incident.ChangeNew, nil))

	// Need at least 1 incident to avoid Discover() early return.
	incidents := []*incident.Incident{
		makeIncident("inc-1", "glm:agent1", "medium", time.Now()),
	}

	m := NewPatternMiner(
		NewIncidentSliceSource(incidents),
		NewPatternSliceSource(patterns),
	)

	results := m.Discover()

	found := false
	for _, r := range results {
		if r.PatternType == PatternChangeTypeRisk && strings.Contains(r.Title, "migration") {
			found = true
		}
	}
	if !found {
		t.Error("expected change type risk pattern for migration")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Review gap tests
// ─────────────────────────────────────────────────────────────────────────────

func TestDiscoverReviewGaps(t *testing.T) {
	var patterns []*incident.IncidentPattern
	// 3 patterns with review-gap keywords.
	for i := 0; i < 3; i++ {
		patterns = append(patterns, makePattern("rg-"+string(rune('a'+i)), "glm:agent1",
			[]incident.FileCategory{incident.CategoryAuth}, incident.ChangeNew,
			[]string{"this was missed in review", "review didn't catch the edge case"}))
	}
	// 7 patterns without review gaps.
	for i := 0; i < 7; i++ {
		patterns = append(patterns, makePattern("nr-"+string(rune('a'+i)), "glm:agent2",
			[]incident.FileCategory{incident.CategoryAPI}, incident.ChangeModify,
			[]string{"code was correct but config was wrong", "deployment issue"}))
	}

	// Need at least 1 incident to satisfy the "all incidents" check.
	incidents := []*incident.Incident{
		makeIncident("inc-1", "glm:agent1", "medium", time.Now()),
	}

	m := NewPatternMiner(
		NewIncidentSliceSource(incidents),
		NewPatternSliceSource(patterns),
	)

	results := m.Discover()

	found := false
	for _, r := range results {
		if r.PatternType == PatternReviewGap {
			found = true
			if r.SampleSize != 3 {
				t.Errorf("expected 3 review-gap patterns, got %d", r.SampleSize)
			}
		}
	}
	if !found {
		t.Error("expected review gap pattern")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Empty / edge case tests
// ─────────────────────────────────────────────────────────────────────────────

func TestDiscoverWithNoIncidents(t *testing.T) {
	m := NewPatternMiner(
		NewIncidentSliceSource(nil),
		NewPatternSliceSource(nil),
	)

	results := m.Discover()
	if len(results) != 0 {
		t.Errorf("expected no patterns, got %d", len(results))
	}
	if m.LastDiscovery().IsZero() {
		t.Error("expected LastDiscovery to be set")
	}
}

func TestDiscoverWithNoPatterns(t *testing.T) {
	incidents := []*incident.Incident{
		makeIncident("inc-1", "glm:agent1", "medium", time.Now()),
	}

	m := NewPatternMiner(
		NewIncidentSliceSource(incidents),
		NewPatternSliceSource(nil),
	)

	results := m.Discover()
	// Only time-based and severity clusters work without patterns.
	// With only 1 incident, neither will trigger (min 3 for time, min 5 for severity).
	if len(results) != 0 {
		t.Errorf("expected no patterns from single incident, got %d", len(results))
	}
}

func TestDiscoverFiltersBelowHypothesis(t *testing.T) {
	// Create data that produces very low confidence patterns.
	var patterns []*incident.IncidentPattern
	patterns = append(patterns, makePattern("p-1", "glm:agent1",
		[]incident.FileCategory{incident.CategoryAuth}, incident.ChangeNew, nil))
	patterns = append(patterns, makePattern("p-2", "glm:agent2",
		[]incident.FileCategory{incident.CategoryDatabase}, incident.ChangeNew, nil))

	incidents := []*incident.Incident{
		makeIncident("inc-1", "glm:agent1", "low", time.Now()),
	}

	m := NewPatternMiner(
		NewIncidentSliceSource(incidents),
		NewPatternSliceSource(patterns),
	)

	results := m.Discover()
	// With only 2 patterns spread across categories, confidence should be low.
	// All patterns below HypothesisThreshold should be filtered.
	for _, r := range results {
		if r.Confidence < HypothesisThreshold {
			t.Errorf("pattern %s has confidence %.2f below hypothesis threshold %.2f — should have been filtered",
				r.ID, r.Confidence, HypothesisThreshold)
		}
	}
}

func TestGetNonexistentPattern(t *testing.T) {
	m := NewPatternMiner(nil, nil)
	p := m.Get("nonexistent")
	if p != nil {
		t.Error("expected nil for nonexistent pattern")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Confidence thresholds
// ─────────────────────────────────────────────────────────────────────────────

func TestDiscoveredPattern_IsHypothesis(t *testing.T) {
	tests := []struct {
		conf     float64
		expected bool
	}{
		{0.3, false}, // below hypothesis threshold
		{0.4, true},  // at hypothesis threshold
		{0.6, true},  // between thresholds
		{0.79, true}, // just below established
		{0.8, false}, // at established threshold
		{0.9, false}, // above established
	}
	for _, tt := range tests {
		p := DiscoveredPattern{Confidence: tt.conf}
		if got := p.IsHypothesis(); got != tt.expected {
			t.Errorf("conf=%.2f: IsHypothesis() = %v, want %v", tt.conf, got, tt.expected)
		}
	}
}

func TestDiscoveredPattern_IsEstablished(t *testing.T) {
	tests := []struct {
		conf     float64
		expected bool
	}{
		{0.3, false},
		{0.5, false},
		{0.79, false},
		{0.8, true},
		{0.9, true},
	}
	for _, tt := range tests {
		p := DiscoveredPattern{Confidence: tt.conf}
		if got := p.IsEstablished(); got != tt.expected {
			t.Errorf("conf=%.2f: IsEstablished() = %v, want %v", tt.conf, got, tt.expected)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Extract provider
// ─────────────────────────────────────────────────────────────────────────────

func TestExtractProvider(t *testing.T) {
	tests := []struct {
		agentID  string
		expected string
	}{
		{"zai:glm:agent1", "zai"},
		{"minimax:agent1", "minimax"},
		{"openai:gpt:codex:agent1", "openai"},
		{"agent1", "unknown"},
		{"", "unknown"},
	}
	for _, tt := range tests {
		got := extractProvider(tt.agentID)
		if got != tt.expected {
			t.Errorf("extractProvider(%q) = %q, want %q", tt.agentID, got, tt.expected)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Time-based patterns
// ─────────────────────────────────────────────────────────────────────────────

func TestDiscoverTimeBasedPatts(t *testing.T) {
	// Create incidents on different days to test time-based patterns.
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC) // deterministic: Monday
	var incidents []*incident.Incident
	// 8 incidents on Monday, 2 each on other days.
	monday := now.AddDate(0, 0, -int(now.Weekday())+1) // previous Monday
	for i := 0; i < 8; i++ {
		incidents = append(incidents, makeIncident("inc-mon-"+string(rune('a'+i)), "glm:agent1",
			"medium", monday.Add(time.Duration(i)*time.Hour)))
	}
	tuesday := monday.AddDate(0, 0, 1)
	for i := 0; i < 2; i++ {
		incidents = append(incidents, makeIncident("inc-tue-"+string(rune('a'+i)), "glm:agent1",
			"medium", tuesday.Add(time.Duration(i)*time.Hour)))
	}
	wednesday := monday.AddDate(0, 0, 2)
	for i := 0; i < 2; i++ {
		incidents = append(incidents, makeIncident("inc-wed-"+string(rune('a'+i)), "glm:agent1",
			"medium", wednesday.Add(time.Duration(i)*time.Hour)))
	}

	m := NewPatternMiner(
		NewIncidentSliceSource(incidents),
		NewPatternSliceSource(nil),
	)

	results := m.Discover()

	found := false
	for _, r := range results {
		if r.PatternType == PatternTimeBased {
			found = true
			if !strings.Contains(r.Title, "Monday") {
				t.Errorf("expected Monday in time pattern title, got %q", r.Title)
			}
		}
	}
	if !found {
		t.Error("expected time-based pattern for Monday (elevated rate)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Severity cluster tests
// ─────────────────────────────────────────────────────────────────────────────

func TestDiscoverSeverityClusters(t *testing.T) {
	// 3 high, 2 critical, 2 medium → high-ratio = 5/7 ≈ 0.71
	var incidents []*incident.Incident
	for i := 0; i < 3; i++ {
		incidents = append(incidents, makeIncident("inc-high-"+string(rune('a'+i)), "glm:agent1",
			incident.SeverityHigh, time.Now()))
	}
	for i := 0; i < 2; i++ {
		incidents = append(incidents, makeIncident("inc-crit-"+string(rune('a'+i)), "glm:agent1",
			incident.SeverityCritical, time.Now()))
	}
	for i := 0; i < 2; i++ {
		incidents = append(incidents, makeIncident("inc-med-"+string(rune('a'+i)), "glm:agent1",
			incident.SeverityMedium, time.Now()))
	}

	m := NewPatternMiner(
		NewIncidentSliceSource(incidents),
		NewPatternSliceSource(nil),
	)

	results := m.Discover()

	found := false
	for _, r := range results {
		if r.PatternType == PatternSeverityCluster {
			found = true
			if r.SampleSize != 5 {
				t.Errorf("expected 5 high-severity incidents, got %d", r.SampleSize)
			}
		}
	}
	if !found {
		t.Error("expected severity cluster pattern")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Result ordering
// ─────────────────────────────────────────────────────────────────────────────

func TestDiscoverResultsSortedByConfidence(t *testing.T) {
	var patterns []*incident.IncidentPattern
	// Auth: many patterns → high-confidence category cluster.
	for i := 0; i < 15; i++ {
		patterns = append(patterns, makePattern("auth-"+string(rune('a'+i)), "zai:glm:agent1",
			[]incident.FileCategory{incident.CategoryAuth}, incident.ChangeNew, nil))
	}
	for i := 0; i < 3; i++ {
		patterns = append(patterns, makePattern("api-"+string(rune('a'+i)), "zai:glm:agent2",
			[]incident.FileCategory{incident.CategoryAPI}, incident.ChangeNew, nil))
	}

	incidents := []*incident.Incident{
		makeIncident("inc-1", "zai:glm:agent1", incident.SeverityHigh, time.Now()),
	}

	m := NewPatternMiner(
		NewIncidentSliceSource(incidents),
		NewPatternSliceSource(patterns),
	)

	results := m.Discover()

	// Verify sorted by confidence descending.
	for i := 1; i < len(results); i++ {
		if results[i-1].Confidence < results[i].Confidence {
			t.Errorf("results not sorted by confidence: results[%d].Confidence=%.2f < results[%d].Confidence=%.2f",
				i-1, results[i-1].Confidence, i, results[i].Confidence)
		}
	}
}

func containsCategory(cats []incident.FileCategory, target incident.FileCategory) bool {
	for _, c := range cats {
		if c == target {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Benchmarks
// ─────────────────────────────────────────────────────────────────────────────

func BenchmarkPatternMiner_CategoryClusters(b *testing.B) {
	var patterns []*incident.IncidentPattern
	for i := 0; i < 10; i++ {
		patterns = append(patterns, makePattern("p-auth-"+string(rune('a'+i)), "glm:agent1",
			[]incident.FileCategory{incident.CategoryAuth}, incident.ChangeNew, nil))
	}
	for _, cat := range []incident.FileCategory{incident.CategoryDatabase, incident.CategoryAPI, incident.CategoryConfig, incident.CategoryNetworking} {
		patterns = append(patterns, makePattern("p-"+string(cat), "glm:agent2",
			[]incident.FileCategory{cat}, incident.ChangeNew, nil))
	}

	incidents := []*incident.Incident{
		makeIncident("i1", "glm:agent1", "medium", time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)),
	}
	m := NewPatternMiner(NewIncidentSliceSource(incidents), NewPatternSliceSource(patterns))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.discoverCategoryClusters(incidents, patterns)
	}
}

func BenchmarkPatternMiner_ProviderCorrelation(b *testing.B) {
	var patterns []*incident.IncidentPattern
	for i := 0; i < 5; i++ {
		patterns = append(patterns, makePattern("p-openai-"+string(rune('a'+i)), "openai:agent"+string(rune('a'+i)),
			[]incident.FileCategory{incident.CategoryAuth}, incident.ChangeNew, nil))
	}
	for i := 0; i < 15; i++ {
		patterns = append(patterns, makePattern("p-glm-"+string(rune('a'+i)), "glm:agent"+string(rune('a'+i)),
			[]incident.FileCategory{incident.CategoryConfig}, incident.ChangeModify, nil))
	}

	incidents := []*incident.Incident{
		makeIncident("i1", "openai:agent0", "high", time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)),
		makeIncident("i2", "glm:agent0", "low", time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)),
		makeIncident("i3", "glm:agent1", "low", time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)),
	}
	m := NewPatternMiner(NewIncidentSliceSource(incidents), NewPatternSliceSource(patterns))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.discoverProviderCorrelations(incidents, patterns)
	}
}

func BenchmarkPatternMiner_Discover(b *testing.B) {
	var patterns []*incident.IncidentPattern
	for i := 0; i < 10; i++ {
		patterns = append(patterns, makePattern("p-auth-"+string(rune('a'+i)), "openai:agent"+string(rune('a'+i)),
			[]incident.FileCategory{incident.CategoryAuth}, incident.ChangeNew, nil))
	}
	for _, ct := range []incident.ChangeType{incident.ChangeModify, incident.ChangeDelete, incident.ChangeRefactor} {
		patterns = append(patterns, makePattern("p-"+string(ct), "glm:agent99",
			[]incident.FileCategory{incident.CategoryDatabase}, ct, []string{"check carefully"}))
	}

	incidents := []*incident.Incident{
		makeIncident("i1", "openai:agent0", "high", time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)),
		makeIncident("i2", "glm:agent99", "low", time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := NewPatternMiner(NewIncidentSliceSource(incidents), NewPatternSliceSource(patterns))
		m.Discover()
	}
}
