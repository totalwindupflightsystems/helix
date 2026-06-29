package trust

import (
	"testing"
)

// --- entryRequirementsFor ---

func TestEntryRequirements_Provisional(t *testing.T) {
	req := entryRequirementsFor(TierProvisional)
	if req.MinScore != 0.0 || req.MinMerges != 0 || req.MinDays != 0 {
		t.Errorf("Provisional should have zero requirements, got %+v", req)
	}
}

func TestEntryRequirements_Observed(t *testing.T) {
	req := entryRequirementsFor(TierObserved)
	if req.MinScore != 0.40 {
		t.Errorf("Observed MinScore = %.2f, want 0.40", req.MinScore)
	}
	if req.MinMerges != 100 {
		t.Errorf("Observed MinMerges = %d, want 100", req.MinMerges)
	}
	if req.MaxIncidents != 0 {
		t.Errorf("Observed MaxIncidents = %d, want 0", req.MaxIncidents)
	}
	if req.MinDays != 30 {
		t.Errorf("Observed MinDays = %d, want 30", req.MinDays)
	}
	if req.MinReviews != 0 {
		t.Errorf("Observed MinReviews = %d, want 0", req.MinReviews)
	}
}

func TestEntryRequirements_Trusted(t *testing.T) {
	req := entryRequirementsFor(TierTrusted)
	if req.MinScore != 0.65 {
		t.Errorf("Trusted MinScore = %.2f, want 0.65", req.MinScore)
	}
	if req.MinMerges != 500 {
		t.Errorf("Trusted MinMerges = %d, want 500", req.MinMerges)
	}
	if req.MaxIncidents != 0 {
		t.Errorf("Trusted MaxIncidents = %d, want 0", req.MaxIncidents)
	}
	if req.MinDays != 90 {
		t.Errorf("Trusted MinDays = %d, want 90", req.MinDays)
	}
}

func TestEntryRequirements_Veteran(t *testing.T) {
	req := entryRequirementsFor(TierVeteran)
	if req.MinScore != 0.85 {
		t.Errorf("Veteran MinScore = %.2f, want 0.85", req.MinScore)
	}
	if req.MinMerges != 2000 {
		t.Errorf("Veteran MinMerges = %d, want 2000", req.MinMerges)
	}
	if req.MaxIncidents != 1 {
		t.Errorf("Veteran MaxIncidents = %d, want 1", req.MaxIncidents)
	}
	if req.MinDays != 180 {
		t.Errorf("Veteran MinDays = %d, want 180", req.MinDays)
	}
	if req.MinReviews != 50 {
		t.Errorf("Veteran MinReviews = %d, want 50", req.MinReviews)
	}
}

// --- EvaluatePromotion ---

func TestEvaluatePromotion_AllCriteriaMet_Observed(t *testing.T) {
	metrics := AgentMetrics{
		TrustScore:    0.45,
		TotalMerges:   150,
		Incidents180d: 0,
		DaysActive:    35,
		CurrentTier:   TierProvisional,
	}
	result := EvaluatePromotion(metrics, TierObserved)

	if !result.Qualified {
		t.Error("expected qualification for Observed")
	}
	if result.BlockingCount != 0 {
		t.Errorf("expected 0 blocking criteria, got %d", result.BlockingCount)
	}
	if len(result.Criteria) != 4 {
		t.Errorf("expected 4 criteria, got %d", len(result.Criteria))
	}
}

func TestEvaluatePromotion_AllCriteriaMet_Veteran(t *testing.T) {
	metrics := AgentMetrics{
		TrustScore:    0.90,
		TotalMerges:   2500,
		Incidents180d: 1,
		DaysActive:    200,
		PRsReviewed:   60,
		CurrentTier:   TierTrusted,
	}
	result := EvaluatePromotion(metrics, TierVeteran)

	if !result.Qualified {
		t.Error("expected qualification for Veteran")
	}
	// Veteran has 5 criteria (including PR reviews)
	if len(result.Criteria) != 5 {
		t.Errorf("expected 5 criteria for Veteran, got %d", len(result.Criteria))
	}
}

func TestEvaluatePromotion_ScoreTooLow(t *testing.T) {
	metrics := AgentMetrics{
		TrustScore:    0.30,
		TotalMerges:   200,
		Incidents180d: 0,
		DaysActive:    50,
	}
	result := EvaluatePromotion(metrics, TierObserved)

	if result.Qualified {
		t.Error("should not qualify with score below threshold")
	}
	if result.BlockingCount != 1 {
		t.Errorf("expected 1 blocking criterion (score), got %d", result.BlockingCount)
	}
	// Check the score criterion specifically
	scoreCrit := findCriterion(result.Criteria, "trust_score")
	if scoreCrit == nil || scoreCrit.Passed {
		t.Error("expected trust_score criterion to fail")
	}
}

func TestEvaluatePromotion_MergesTooLow(t *testing.T) {
	metrics := AgentMetrics{
		TrustScore:    0.70,
		TotalMerges:   300, // enough for Observed but not Trusted
		Incidents180d: 0,
		DaysActive:    100,
	}
	result := EvaluatePromotion(metrics, TierTrusted)

	if result.Qualified {
		t.Error("should not qualify with merges below 500")
	}
	mergesCrit := findCriterion(result.Criteria, "total_merges")
	if mergesCrit == nil || mergesCrit.Passed {
		t.Error("expected total_merges criterion to fail")
	}
}

func TestEvaluatePromotion_TooManyIncidents(t *testing.T) {
	metrics := AgentMetrics{
		TrustScore:    0.50,
		TotalMerges:   150,
		Incidents180d: 1, // Observed requires 0
		DaysActive:    35,
	}
	result := EvaluatePromotion(metrics, TierObserved)

	if result.Qualified {
		t.Error("should not qualify with incidents > 0 for Observed")
	}
	incCrit := findCriterion(result.Criteria, "max_incidents_180d")
	if incCrit == nil || incCrit.Passed {
		t.Error("expected max_incidents_180d to fail")
	}
}

func TestEvaluatePromotion_DaysActiveTooLow(t *testing.T) {
	metrics := AgentMetrics{
		TrustScore:    0.50,
		TotalMerges:   150,
		Incidents180d: 0,
		DaysActive:    20, // Observed requires 30
	}
	result := EvaluatePromotion(metrics, TierObserved)

	if result.Qualified {
		t.Error("should not qualify with insufficient days active")
	}
}

func TestEvaluatePromotion_VeteranReviewsTooLow(t *testing.T) {
	metrics := AgentMetrics{
		TrustScore:    0.90,
		TotalMerges:   2500,
		Incidents180d: 0,
		DaysActive:    200,
		PRsReviewed:   40, // Veteran requires 50
	}
	result := EvaluatePromotion(metrics, TierVeteran)

	if result.Qualified {
		t.Error("should not qualify with insufficient PR reviews")
	}
	reviewCrit := findCriterion(result.Criteria, "min_pr_reviews")
	if reviewCrit == nil || reviewCrit.Passed {
		t.Error("expected min_pr_reviews to fail")
	}
}

func TestEvaluatePromotion_MultipleCriteriaFail(t *testing.T) {
	metrics := AgentMetrics{
		TrustScore:    0.50,
		TotalMerges:   100,
		Incidents180d: 2, // too many for Trusted (requires 0)
		DaysActive:    50, // too few for Trusted (requires 90)
	}
	result := EvaluatePromotion(metrics, TierTrusted)

	if result.Qualified {
		t.Error("should not qualify")
	}
	if result.BlockingCount < 2 {
		t.Errorf("expected at least 2 blocking criteria, got %d", result.BlockingCount)
	}
}

func TestEvaluatePromotion_ProvisionalAlwaysQualifies(t *testing.T) {
	metrics := AgentMetrics{
		TrustScore:    0.0,
		TotalMerges:   0,
		Incidents180d: 10,
		DaysActive:    0,
	}
	result := EvaluatePromotion(metrics, TierProvisional)

	if !result.Qualified {
		t.Error("Provisional should always qualify (no entry requirements)")
	}
}

func TestEvaluatePromotion_ReasonString(t *testing.T) {
	metrics := AgentMetrics{
		TrustScore:    0.90,
		TotalMerges:   2500,
		Incidents180d: 0,
		DaysActive:    200,
		PRsReviewed:   60,
	}
	result := EvaluatePromotion(metrics, TierVeteran)
	if result.Reason == "" {
		t.Error("reason should not be empty")
	}
}

func TestEvaluatePromotion_BlockingReasonString(t *testing.T) {
	metrics := AgentMetrics{
		TrustScore: 0.30,
		TotalMerges: 200,
		DaysActive:  35,
	}
	result := EvaluatePromotion(metrics, TierObserved)
	if result.Reason == "" {
		t.Error("blocking reason should not be empty")
	}
}

// --- ShouldPromote ---

func TestShouldPromote_FromProvisional(t *testing.T) {
	metrics := AgentMetrics{
		TrustScore:    0.45,
		TotalMerges:   150,
		Incidents180d: 0,
		DaysActive:    35,
		CurrentTier:   TierProvisional,
	}
	if !ShouldPromote(metrics) {
		t.Error("should promote from Provisional to Observed")
	}
}

func TestShouldPromote_NotEnoughMerges(t *testing.T) {
	metrics := AgentMetrics{
		TrustScore:    0.50,
		TotalMerges:   50, // not enough for Observed (needs 100)
		Incidents180d: 0,
		DaysActive:    35,
		CurrentTier:   TierProvisional,
	}
	if ShouldPromote(metrics) {
		t.Error("should not promote with insufficient merges")
	}
}

func TestShouldPromote_VeteranNoPromotion(t *testing.T) {
	metrics := AgentMetrics{
		TrustScore:    0.99,
		TotalMerges:   5000,
		Incidents180d: 0,
		DaysActive:    500,
		PRsReviewed:   100,
		CurrentTier:   TierVeteran,
	}
	if ShouldPromote(metrics) {
		t.Error("Veteran should not promote (already highest)")
	}
}

// --- PromoteTo ---

func TestPromoteTo_FromProvisionalToObserved(t *testing.T) {
	metrics := AgentMetrics{
		TrustScore:    0.50,
		TotalMerges:   200,
		Incidents180d: 0,
		DaysActive:    40,
		CurrentTier:   TierProvisional,
	}
	if PromoteTo(metrics) != TierObserved {
		t.Errorf("expected promotion to Observed, got %s", PromoteTo(metrics))
	}
}

func TestPromoteTo_FromObservedToTrusted(t *testing.T) {
	metrics := AgentMetrics{
		TrustScore:    0.70,
		TotalMerges:   600,
		Incidents180d: 0,
		DaysActive:    100,
		CurrentTier:   TierObserved,
	}
	if PromoteTo(metrics) != TierTrusted {
		t.Errorf("expected promotion to Trusted, got %s", PromoteTo(metrics))
	}
}

func TestPromoteTo_FromTrustedToVeteran(t *testing.T) {
	metrics := AgentMetrics{
		TrustScore:    0.90,
		TotalMerges:   2500,
		Incidents180d: 0,
		DaysActive:    200,
		PRsReviewed:   60,
		CurrentTier:   TierTrusted,
	}
	if PromoteTo(metrics) != TierVeteran {
		t.Errorf("expected promotion to Veteran, got %s", PromoteTo(metrics))
	}
}

func TestPromoteTo_NotQualified(t *testing.T) {
	metrics := AgentMetrics{
		TrustScore:    0.35,
		TotalMerges:   50,
		Incidents180d: 0,
		DaysActive:    10,
		CurrentTier:   TierProvisional,
	}
	if PromoteTo(metrics) != TierProvisional {
		t.Errorf("should stay at Provisional, got %s", PromoteTo(metrics))
	}
}

func TestPromoteTo_VeteranStays(t *testing.T) {
	metrics := AgentMetrics{
		TrustScore:    0.99,
		TotalMerges:   10000,
		CurrentTier:   TierVeteran,
	}
	if PromoteTo(metrics) != TierVeteran {
		t.Error("Veteran should stay Veteran")
	}
}

// --- nextTierUp ---

func TestNextTierUp(t *testing.T) {
	tests := []struct {
		from, expected TrustTier
	}{
		{TierProvisional, TierObserved},
		{TierObserved, TierTrusted},
		{TierTrusted, TierVeteran},
		{TierVeteran, TierVeteran}, // no higher tier
	}
	for _, tc := range tests {
		if nextTierUp(tc.from) != tc.expected {
			t.Errorf("nextTierUp(%s) = %s, want %s", tc.from, nextTierUp(tc.from), tc.expected)
		}
	}
}

// --- EvaluateFullTierCycle ---

func TestEvaluateFullTierCycle_Promotion(t *testing.T) {
	metrics := AgentMetrics{
		TrustScore:    0.50,
		TotalMerges:   200,
		Incidents180d: 0,
		DaysActive:    40,
		CurrentTier:   TierProvisional,
	}
	tier, result, changed := EvaluateFullTierCycle(metrics)
	if !changed || tier != TierObserved {
		t.Errorf("expected promotion to Observed, got tier=%s changed=%v", tier, changed)
	}
	if result == nil {
		t.Error("expected non-nil promotion result")
	}
}

func TestEvaluateFullTierCycle_NoChange(t *testing.T) {
	metrics := AgentMetrics{
		TrustScore:    0.35,
		TotalMerges:   50,
		CurrentTier:   TierProvisional,
	}
	tier, _, changed := EvaluateFullTierCycle(metrics)
	if changed {
		t.Error("expected no change")
	}
	if tier != TierProvisional {
		t.Errorf("expected Provisional, got %s", tier)
	}
}

func TestEvaluateFullTierCycle_VeteranNoPromotion(t *testing.T) {
	metrics := AgentMetrics{
		TrustScore:    0.99,
		TotalMerges:   5000,
		CurrentTier:   TierVeteran,
	}
	_, _, changed := EvaluateFullTierCycle(metrics)
	if changed {
		t.Error("Veteran should not change")
	}
}

// --- TierRank, IsPromotion, IsDemotion ---

func TestTierRank(t *testing.T) {
	if TierRank(TierProvisional) != 0 {
		t.Error("Provisional rank should be 0")
	}
	if TierRank(TierObserved) != 1 {
		t.Error("Observed rank should be 1")
	}
	if TierRank(TierTrusted) != 2 {
		t.Error("Trusted rank should be 2")
	}
	if TierRank(TierVeteran) != 3 {
		t.Error("Veteran rank should be 3")
	}
}

func TestIsPromotion(t *testing.T) {
	if !IsPromotion(TierProvisional, TierObserved) {
		t.Error("Provisional→Observed is a promotion")
	}
	if !IsPromotion(TierObserved, TierVeteran) {
		t.Error("Observed→Veteran is a promotion")
	}
	if IsPromotion(TierVeteran, TierTrusted) {
		t.Error("Veteran→Trusted is NOT a promotion")
	}
	if IsPromotion(TierObserved, TierObserved) {
		t.Error("same tier is NOT a promotion")
	}
}

func TestIsDemotion(t *testing.T) {
	if !IsDemotion(TierVeteran, TierTrusted) {
		t.Error("Veteran→Trusted is a demotion")
	}
	if !IsDemotion(TierTrusted, TierProvisional) {
		t.Error("Trusted→Provisional is a demotion")
	}
	if IsDemotion(TierProvisional, TierObserved) {
		t.Error("Provisional→Observed is NOT a demotion")
	}
}

// --- Full lifecycle integration ---

func TestFullLifecycle_ProvisionalToVeteran(t *testing.T) {
	// Start at Provisional
	metrics := AgentMetrics{
		TrustScore:    0.0,
		TotalMerges:   0,
		CurrentTier:   TierProvisional,
	}

	// After 100 merges and 30 days, should promote to Observed
	metrics.TrustScore = 0.45
	metrics.TotalMerges = 150
	metrics.Incidents180d = 0
	metrics.DaysActive = 35
	tier, _, changed := EvaluateFullTierCycle(metrics)
	if !changed || tier != TierObserved {
		t.Fatalf("expected Provisional→Observed, got %s (changed=%v)", tier, changed)
	}

	// Continue to Trusted
	metrics.CurrentTier = TierObserved
	metrics.TrustScore = 0.70
	metrics.TotalMerges = 600
	metrics.DaysActive = 100
	tier, _, changed = EvaluateFullTierCycle(metrics)
	if !changed || tier != TierTrusted {
		t.Fatalf("expected Observed→Trusted, got %s (changed=%v)", tier, changed)
	}

	// Continue to Veteran
	metrics.CurrentTier = TierTrusted
	metrics.TrustScore = 0.90
	metrics.TotalMerges = 2500
	metrics.DaysActive = 200
	metrics.PRsReviewed = 60
	tier, _, changed = EvaluateFullTierCycle(metrics)
	if !changed || tier != TierVeteran {
		t.Fatalf("expected Trusted→Veteran, got %s (changed=%v)", tier, changed)
	}
}

func TestFullLifecycle_IncidentBlocksPromotion(t *testing.T) {
	metrics := AgentMetrics{
		TrustScore:    0.50,
		TotalMerges:   200,
		Incidents180d: 1, // had an incident
		DaysActive:    35,
		CurrentTier:   TierProvisional,
	}
	// Should NOT promote to Observed (requires 0 incidents)
	tier, _, changed := EvaluateFullTierCycle(metrics)
	if changed {
		t.Errorf("should not promote with incident, got %s", tier)
	}
}

// --- Helper ---

func findCriterion(criteria []CriterionResult, name string) *CriterionResult {
	for i := range criteria {
		if criteria[i].Name == name {
			return &criteria[i]
		}
	}
	return nil
}
