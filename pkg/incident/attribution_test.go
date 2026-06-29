package incident

import (
	"fmt"
	"math"
	"testing"
	"time"
)

// =============================================================================
// Tests: AttributionWeights
// =============================================================================

func TestDefaultAttributionWeights(t *testing.T) {
	w := DefaultAttributionWeights()
	if w.Author != 0.70 {
		t.Errorf("author weight = %.2f, want 0.70", w.Author)
	}
	if w.Reviewers != 0.20 {
		t.Errorf("reviewers weight = %.2f, want 0.20", w.Reviewers)
	}
	if w.Approver != 0.10 {
		t.Errorf("approver weight = %.2f, want 0.10", w.Approver)
	}
}

func TestDefaultAttributionWeights_SumToOne(t *testing.T) {
	w := DefaultAttributionWeights()
	sum := w.Author + w.Reviewers + w.Approver
	if math.Abs(sum-1.0) > 0.001 {
		t.Errorf("weights should sum to 1.0, got %.4f", sum)
	}
}

// =============================================================================
// Tests: AttributionEngine.Attribute
// =============================================================================

func TestAttribute_SinglePath_FullAttribution(t *testing.T) {
	store := NewStore()
	engine := NewAttributionEngine(store)

	path := ChangePath{
		FilePath:    "pkg/auth/session.go",
		MergeSHA:    "abc123",
		AuthorID:    "agent-1",
		ReviewerIDs: []string{"agent-2"},
		ApproverID:  "human-1",
	}

	result, err := engine.Attribute("inc-1", []ChangePath{path}, []string{"evidence-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Author gets 70%, reviewer gets 20%, approver gets 10%.
	if got := result.Responsibility["agent-1"]; math.Abs(got-0.70) > 0.001 {
		t.Errorf("author responsibility = %.4f, want 0.70", got)
	}
	if got := result.Responsibility["agent-2"]; math.Abs(got-0.20) > 0.001 {
		t.Errorf("reviewer responsibility = %.4f, want 0.20", got)
	}
	if got := result.Responsibility["human-1"]; math.Abs(got-0.10) > 0.001 {
		t.Errorf("approver responsibility = %.4f, want 0.10", got)
	}
}

func TestAttribute_MultipleReviewers_SplitEqually(t *testing.T) {
	store := NewStore()
	engine := NewAttributionEngine(store)

	path := ChangePath{
		FilePath:    "pkg/auth/session.go",
		AuthorID:    "agent-1",
		ReviewerIDs: []string{"agent-2", "agent-3", "agent-4"},
		ApproverID:  "human-1",
	}

	result, err := engine.Attribute("inc-1", []ChangePath{path}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Each reviewer gets 20%/3 ≈ 6.67%.
	expectedReviewer := 0.20 / 3.0
	for _, rid := range []string{"agent-2", "agent-3", "agent-4"} {
		if got := result.Responsibility[rid]; math.Abs(got-expectedReviewer) > 0.001 {
			t.Errorf("reviewer %s responsibility = %.4f, want %.4f", rid, got, expectedReviewer)
		}
	}
}

func TestAttribute_MultiplePaths_NormalizedToOne(t *testing.T) {
	store := NewStore()
	engine := NewAttributionEngine(store)

	paths := []ChangePath{
		{
			FilePath: "pkg/a.go",
			AuthorID: "agent-1",
			ReviewerIDs: []string{"agent-2"},
			ApproverID: "human-1",
		},
		{
			FilePath: "pkg/b.go",
			AuthorID: "agent-1",
			ReviewerIDs: []string{"agent-3"},
			ApproverID: "human-1",
		},
	}

	result, err := engine.Attribute("inc-1", paths, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Total should sum to ~1.0.
	total := result.TotalResponsibility()
	if math.Abs(total-1.0) > 0.001 {
		t.Errorf("total responsibility = %.4f, want 1.0", total)
	}

	// Agent-1 authored both paths → should have the highest share.
	primary := result.PrimaryResponsible()
	if primary != "agent-1" {
		t.Errorf("expected agent-1 as primary, got %s", primary)
	}
}

func TestAttribute_NoAuthor(t *testing.T) {
	store := NewStore()
	engine := NewAttributionEngine(store)

	path := ChangePath{
		FilePath:    "pkg/a.go",
		ReviewerIDs: []string{"agent-2"},
		ApproverID:  "human-1",
	}

	result, err := engine.Attribute("inc-1", []ChangePath{path}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Total should still be 1.0 (normalized).
	total := result.TotalResponsibility()
	if math.Abs(total-1.0) > 0.001 {
		t.Errorf("total responsibility = %.4f, want 1.0", total)
	}
}

func TestAttribute_NoReviewers(t *testing.T) {
	store := NewStore()
	engine := NewAttributionEngine(store)

	path := ChangePath{
		FilePath: "pkg/a.go",
		AuthorID: "agent-1",
		ApproverID: "human-1",
	}

	result, err := engine.Attribute("inc-1", []ChangePath{path}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Author should have the larger share.
	if result.Responsibility["agent-1"] <= result.Responsibility["human-1"] {
		t.Error("expected author to have more responsibility than approver")
	}
}

func TestAttribute_EmptyIncidentID(t *testing.T) {
	store := NewStore()
	engine := NewAttributionEngine(store)

	_, err := engine.Attribute("", []ChangePath{{AuthorID: "a"}}, nil)
	if err == nil {
		t.Fatal("expected error for empty incident ID")
	}
}

func TestAttribute_EmptyPaths(t *testing.T) {
	store := NewStore()
	engine := NewAttributionEngine(store)

	_, err := engine.Attribute("inc-1", nil, nil)
	if err == nil {
		t.Fatal("expected error for empty paths")
	}
}

func TestAttribute_EvidenceLinks(t *testing.T) {
	store := NewStore()
	engine := NewAttributionEngine(store)

	path := ChangePath{
		FilePath: "pkg/a.go",
		AuthorID: "agent-1",
	}
	evidence := []string{"log: error in session.go", "trace: auth_token=nil"}

	result, err := engine.Attribute("inc-1", []ChangePath{path}, evidence)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.EvidenceLinks) != 2 {
		t.Errorf("expected 2 evidence links, got %d", len(result.EvidenceLinks))
	}
}

func TestAttribute_TimestampSet(t *testing.T) {
	store := NewStore()
	engine := NewAttributionEngine(store)

	path := ChangePath{AuthorID: "agent-1"}
	result, err := engine.Attribute("inc-1", []ChangePath{path}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Timestamp.IsZero() {
		t.Error("expected timestamp to be set")
	}
}

// =============================================================================
// Tests: Custom weights
// =============================================================================

func TestWithWeights(t *testing.T) {
	store := NewStore()
	engine := NewAttributionEngine(store).WithWeights(AttributionWeights{
		Author:    0.50,
		Reviewers: 0.30,
		Approver:  0.20,
	})

	path := ChangePath{
		AuthorID: "agent-1",
		ReviewerIDs: []string{"agent-2"},
		ApproverID: "human-1",
	}

	result, err := engine.Attribute("inc-1", []ChangePath{path}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := result.Responsibility["agent-1"]; math.Abs(got-0.50) > 0.001 {
		t.Errorf("author responsibility with custom weight = %.4f, want 0.50", got)
	}
}

// =============================================================================
// Tests: TrustPenalty
// =============================================================================

func TestTrustPenalty_SeverityMultipliers(t *testing.T) {
	tests := []struct {
		severity string
		share    float64
		expected float64
	}{
		{SeverityLow, 0.70, 0.035},      // 0.70 * 0.05
		{SeverityMedium, 0.70, 0.07},    // 0.70 * 0.10
		{SeverityHigh, 0.70, 0.14},      // 0.70 * 0.20
		{SeverityCritical, 0.70, 0.28},  // 0.70 * 0.40
		{"unknown", 0.70, 0.07},          // default to medium
	}
	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			got := TrustPenalty(tt.share, tt.severity)
			if math.Abs(got-tt.expected) > 0.001 {
				t.Errorf("TrustPenalty(%.2f, %s) = %.4f, want %.4f",
					tt.share, tt.severity, got, tt.expected)
			}
		})
	}
}

func TestTrustPenalty_ZeroShare(t *testing.T) {
	penalty := TrustPenalty(0.0, SeverityCritical)
	if penalty != 0 {
		t.Errorf("penalty for 0 share should be 0, got %.4f", penalty)
	}
}

// =============================================================================
// Tests: Summarize
// =============================================================================

func TestSummarize(t *testing.T) {
	store := NewStore()
	engine := NewAttributionEngine(store)

	path := ChangePath{
		AuthorID: "agent-1",
		ReviewerIDs: []string{"agent-2"},
		ApproverID: "human-1",
	}

	result, _ := engine.Attribute("inc-1", []ChangePath{path}, nil)
	summaries := result.Summarize(SeverityHigh)

	if len(summaries) != 3 {
		t.Errorf("expected 3 summaries, got %d", len(summaries))
	}

	found := map[string]bool{}
	for _, s := range summaries {
		found[s.AgentID] = true
		expectedPenalty := TrustPenalty(s.Responsibility, SeverityHigh)
		if math.Abs(s.TrustPenalty-expectedPenalty) > 0.001 {
			t.Errorf("penalty for %s = %.4f, want %.4f", s.AgentID, s.TrustPenalty, expectedPenalty)
		}
	}

	if !found["agent-1"] || !found["agent-2"] || !found["human-1"] {
		t.Errorf("missing expected agents in summaries: %v", found)
	}
}

// =============================================================================
// Tests: PrimaryResponsible
// =============================================================================

func TestPrimaryResponsible(t *testing.T) {
	store := NewStore()
	engine := NewAttributionEngine(store)

	path := ChangePath{
		AuthorID: "agent-1",
		ReviewerIDs: []string{"agent-2"},
		ApproverID: "human-1",
	}

	result, _ := engine.Attribute("inc-1", []ChangePath{path}, nil)
	primary := result.PrimaryResponsible()

	// Author has 70% → should be primary.
	if primary != "agent-1" {
		t.Errorf("expected agent-1 as primary, got %s", primary)
	}
}

func TestPrimaryResponsible_Empty(t *testing.T) {
	result := &AttributionResult{
		Responsibility: map[string]float64{},
	}
	primary := result.PrimaryResponsible()
	if primary != "" {
		t.Errorf("expected empty string for no responsibility, got %s", primary)
	}
}

// =============================================================================
// Tests: TotalResponsibility
// =============================================================================

func TestTotalResponsibility(t *testing.T) {
	result := &AttributionResult{
		Responsibility: map[string]float64{
			"a": 0.50,
			"b": 0.30,
			"c": 0.20,
		},
	}
	total := result.TotalResponsibility()
	if math.Abs(total-1.0) > 0.001 {
		t.Errorf("total = %.4f, want 1.0", total)
	}
}

// =============================================================================
// Tests: ApplyTrustPenalties
// =============================================================================

func TestApplyTrustPenalties(t *testing.T) {
	store := NewStore()
	engine := NewAttributionEngine(store)

	path := ChangePath{
		AuthorID: "agent-1",
		ReviewerIDs: []string{"agent-2"},
		ApproverID: "human-1",
	}
	result, _ := engine.Attribute("inc-1", []ChangePath{path}, []string{"ev1"})

	penalties := map[string]float64{}
	err := engine.ApplyTrustPenalties(result, SeverityHigh, func(agentID string, penalty float64, evidence []string) error {
		penalties[agentID] = penalty
		if len(evidence) == 0 {
			t.Error("expected evidence to be passed through")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(penalties) != 3 {
		t.Errorf("expected 3 penalty entries, got %d", len(penalties))
	}

	// Author should have the highest penalty.
	if penalties["agent-1"] <= penalties["human-1"] {
		t.Error("expected author to have highest penalty")
	}
}

func TestApplyTrustPenalties_CallbackError(t *testing.T) {
	store := NewStore()
	engine := NewAttributionEngine(store)

	path := ChangePath{AuthorID: "agent-1"}
	result, _ := engine.Attribute("inc-1", []ChangePath{path}, nil)

	err := engine.ApplyTrustPenalties(result, SeverityHigh, func(agentID string, penalty float64, evidence []string) error {
		return fmt.Errorf("trust engine unavailable")
	})
	if err == nil {
		t.Fatal("expected error from callback")
	}
}

// =============================================================================
// Tests: FindResponsiblePaths
// =============================================================================

func TestFindResponsiblePaths(t *testing.T) {
	paths := []ChangePath{
		{FilePath: "pkg/auth/session.go", AuthorID: "agent-1"},
		{FilePath: "pkg/util/helper.go", AuthorID: "agent-2"},
		{FilePath: "pkg/auth/token.go", AuthorID: "agent-3"},
	}
	causalChain := []string{"pkg/auth/session.go", "pkg/auth/token.go"}

	matched := FindResponsiblePaths(paths, causalChain)
	if len(matched) != 2 {
		t.Fatalf("expected 2 matched paths, got %d", len(matched))
	}

	for _, m := range matched {
		if m.FilePath == "pkg/util/helper.go" {
			t.Error("unexpected match on util/helper.go")
		}
	}
}

func TestFindResponsiblePaths_NoMatch(t *testing.T) {
	paths := []ChangePath{
		{FilePath: "pkg/a.go", AuthorID: "agent-1"},
	}
	causalChain := []string{"pkg/b.go"}

	matched := FindResponsiblePaths(paths, causalChain)
	if len(matched) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matched))
	}
}

func TestFindResponsiblePaths_EmptyCausalChain(t *testing.T) {
	paths := []ChangePath{
		{FilePath: "pkg/a.go", AuthorID: "agent-1"},
	}
	matched := FindResponsiblePaths(paths, nil)
	if len(matched) != 0 {
		t.Errorf("expected 0 matches for empty causal chain, got %d", len(matched))
	}
}

// =============================================================================
// Tests: MergeAttribution
// =============================================================================

func TestMergeAttribution(t *testing.T) {
	results := []*AttributionResult{
		{
			Responsibility: map[string]float64{
				"agent-1": 0.70,
				"agent-2": 0.30,
			},
		},
		{
			Responsibility: map[string]float64{
				"agent-1": 0.50,
				"agent-3": 0.50,
			},
		},
	}

	merged := MergeAttribution(results)
	if math.Abs(merged["agent-1"]-1.20) > 0.001 {
		t.Errorf("agent-1 merged = %.4f, want 1.20", merged["agent-1"])
	}
	if math.Abs(merged["agent-2"]-0.30) > 0.001 {
		t.Errorf("agent-2 merged = %.4f, want 0.30", merged["agent-2"])
	}
	if math.Abs(merged["agent-3"]-0.50) > 0.001 {
		t.Errorf("agent-3 merged = %.4f, want 0.50", merged["agent-3"])
	}
}

func TestMergeAttribution_Empty(t *testing.T) {
	merged := MergeAttribution(nil)
	if len(merged) != 0 {
		t.Errorf("expected empty map, got %d entries", len(merged))
	}
}

// =============================================================================
// Tests: severityMultiplier
// =============================================================================

func TestSeverityMultiplier(t *testing.T) {
	tests := []struct {
		severity string
		expected float64
	}{
		{SeverityLow, 0.05},
		{SeverityMedium, 0.10},
		{SeverityHigh, 0.20},
		{SeverityCritical, 0.40},
		{"unknown", 0.10}, // default
	}
	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			got := severityMultiplier(tt.severity)
			if got != tt.expected {
				t.Errorf("severityMultiplier(%s) = %.2f, want %.2f", tt.severity, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// Integration: Full incident flow
// =============================================================================

func TestIntegration_FullIncidentAttributionFlow(t *testing.T) {
	store := NewStore()
	engine := NewAttributionEngine(store)

	// Create an incident.
	inc := &Incident{
		ID:          "inc-prod-001",
		AgentID:     "agent-1",
		PRURL:       "https://forgejo.example.com/org/repo/pulls/42",
		Severity:    SeverityHigh,
		CausalChain: []string{"pkg/auth/session.go", "pkg/auth/token.go"},
		Timestamp:   time.Now().Add(-2 * time.Hour),
		Description: "Token refresh race condition caused 5% of requests to fail",
		Evidence:    []string{"trace: goroutine leak in token_refresh", "metric: 5xx spike at 14:23"},
	}
	store.Add(inc)

	// Change paths from the PR.
	paths := []ChangePath{
		{
			FilePath:    "pkg/auth/session.go",
			MergeSHA:    "abc123",
			AuthorID:    "agent-1",
			ReviewerIDs: []string{"agent-2", "agent-3"},
			ApproverID:  "human-reviewer",
			CommitTime:  time.Now().Add(-24 * time.Hour),
		},
		{
			FilePath:    "pkg/auth/token.go",
			MergeSHA:    "abc123",
			AuthorID:    "agent-1",
			ReviewerIDs: []string{"agent-2"},
			ApproverID:  "human-reviewer",
			CommitTime:  time.Now().Add(-24 * time.Hour),
		},
		{
			FilePath:    "pkg/util/helper.go",
			MergeSHA:    "abc123",
			AuthorID:    "agent-4", // unrelated path
			CommitTime:  time.Now().Add(-24 * time.Hour),
		},
	}

	// Filter to causal chain paths.
	responsiblePaths := FindResponsiblePaths(paths, inc.CausalChain)
	if len(responsiblePaths) != 2 {
		t.Fatalf("expected 2 responsible paths, got %d", len(responsiblePaths))
	}

	// Attribute.
	result, err := engine.Attribute(inc.ID, responsiblePaths, inc.Evidence)
	if err != nil {
		t.Fatalf("attribution failed: %v", err)
	}

	// Verify primary responsible is agent-1 (authored both responsible paths).
	if result.PrimaryResponsible() != "agent-1" {
		t.Errorf("expected agent-1 as primary, got %s", result.PrimaryResponsible())
	}

	// Apply trust penalties.
	var penalties []AttributionSummary
	err = engine.ApplyTrustPenalties(result, inc.Severity, func(agentID string, penalty float64, evidence []string) error {
		penalties = append(penalties, AttributionSummary{
			AgentID:      agentID,
			TrustPenalty: penalty,
		})
		return nil
	})
	if err != nil {
		t.Fatalf("trust penalty application failed: %v", err)
	}

	// Agent-1 should have the highest penalty.
	var maxPenalty float64
	var maxAgent string
	for _, p := range penalties {
		if p.TrustPenalty > maxPenalty {
			maxPenalty = p.TrustPenalty
			maxAgent = p.AgentID
		}
	}
	if maxAgent != "agent-1" {
		t.Errorf("expected agent-1 to have highest penalty, got %s", maxAgent)
	}
}
