package learning

import (
	"math"
	"sync"
	"testing"
	"time"
)

func TestNewModelEvaluator(t *testing.T) {
	me := NewModelEvaluator()
	if me == nil {
		t.Fatal("NewModelEvaluator returned nil")
	}
	if len(me.ListModels()) != 0 {
		t.Error("new evaluator should have no models")
	}
	if me.FleetAvgIncidentRate() != 0 {
		t.Error("new evaluator fleet avg should be 0")
	}
}

func TestRecordMerge(t *testing.T) {
	me := NewModelEvaluator()
	me.RecordMerge("openai:gpt-5.1", true)
	me.RecordMerge("openai:gpt-5.1", true)
	me.RecordMerge("openai:gpt-5.1", false)

	m, ok := me.GetMetrics("openai:gpt-5.1")
	if !ok {
		t.Fatal("model not found")
	}
	if m.TotalMerges != 3 {
		t.Errorf("expected 3 merges, got %d", m.TotalMerges)
	}
}

func TestRecordIncident(t *testing.T) {
	me := NewModelEvaluator()
	me.RecordIncident("openai:gpt-5.1")
	me.RecordIncident("openai:gpt-5.1")

	m, ok := me.GetMetrics("openai:gpt-5.1")
	if !ok {
		t.Fatal("model not found")
	}
	if m.IncidentsAttributed != 2 {
		t.Errorf("expected 2 incidents, got %d", m.IncidentsAttributed)
	}
}

func TestRecordReview(t *testing.T) {
	me := NewModelEvaluator()
	me.RecordReview("openai:gpt-5.1", true)  // false positive
	me.RecordReview("openai:gpt-5.1", false) // correct finding
	me.RecordReview("openai:gpt-5.1", false)

	m, ok := me.GetMetrics("openai:gpt-5.1")
	if !ok {
		t.Fatal("model not found")
	}
	if m.TotalReviews != 3 {
		t.Errorf("expected 3 reviews, got %d", m.TotalReviews)
	}
	if m.FalsePositives != 1 {
		t.Errorf("expected 1 false positive, got %d", m.FalsePositives)
	}
}

func TestEvaluate_IncidentRate(t *testing.T) {
	me := NewModelEvaluator()

	// 10 merges, 2 incidents → 20% incident rate.
	for i := 0; i < 10; i++ {
		me.RecordMerge("openai:gpt-5.1", true)
	}
	me.RecordIncident("openai:gpt-5.1")
	me.RecordIncident("openai:gpt-5.1")

	me.Evaluate("openai:gpt-5.1")

	m, ok := me.GetMetrics("openai:gpt-5.1")
	if !ok {
		t.Fatal("model not found")
	}
	if m.IncidentRate != 0.2 {
		t.Errorf("expected incident rate 0.2, got %f", m.IncidentRate)
	}
}

func TestEvaluate_FPRate(t *testing.T) {
	me := NewModelEvaluator()

	// 20 reviews, 4 false positives → 20% FP rate.
	for i := 0; i < 20; i++ {
		fp := i < 4
		me.RecordReview("openai:gpt-5.1", fp)
	}

	me.Evaluate("openai:gpt-5.1")

	m, ok := me.GetMetrics("openai:gpt-5.1")
	if !ok {
		t.Fatal("model not found")
	}
	if m.FalsePositiveRate != 0.2 {
		t.Errorf("expected FP rate 0.2, got %f", m.FalsePositiveRate)
	}
}

func TestFPRateAboveThreshold_RemovesFromRotation(t *testing.T) {
	me := NewModelEvaluator()

	// 10 reviews, 2 false positives → 20% FP rate (>15% threshold).
	for i := 0; i < 10; i++ {
		fp := i < 2
		me.RecordReview("openai:gpt-5.1", fp)
	}

	me.EvaluateAll()

	if me.IsInReviewRotation("openai:gpt-5.1") {
		t.Error("model should be removed from rotation (20% FP > 15% threshold)")
	}

	events := me.ListEvents()
	if len(events) == 0 {
		t.Fatal("expected rotation events")
	}
	found := false
	for _, e := range events {
		if e.EventType == RotationEventRemoved && e.ModelID == "openai:gpt-5.1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a 'removed' rotation event")
	}
}

func TestFPRateBelowThreshold_StaysInRotation(t *testing.T) {
	me := NewModelEvaluator()

	// 10 reviews, 1 false positive → 10% FP rate (below 15% threshold).
	for i := 0; i < 10; i++ {
		fp := i < 1
		me.RecordReview("openai:gpt-5.1", fp)
	}

	me.EvaluateAll()

	if !me.IsInReviewRotation("openai:gpt-5.1") {
		t.Error("model should still be in rotation (10% FP < 15% threshold)")
	}
}

func TestReAdmitToRotation(t *testing.T) {
	me := NewModelEvaluator()

	// Trigger removal.
	for i := 0; i < 10; i++ {
		me.RecordReview("openai:gpt-5.1", i < 2)
	}
	me.EvaluateAll()

	if me.IsInReviewRotation("openai:gpt-5.1") {
		t.Fatal("model should be removed")
	}

	// Re-admit manually.
	me.ReAdmitToRotation("openai:gpt-5.1")

	if !me.IsInReviewRotation("openai:gpt-5.1") {
		t.Error("model should be re-admitted to rotation")
	}

	events := me.ListEvents()
	found := false
	for _, e := range events {
		if e.EventType == RotationEventReAdmitted && e.ModelID == "openai:gpt-5.1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a 're_admitted' rotation event")
	}
}

func TestReAdmitAfter30CleanDays(t *testing.T) {
	me := NewModelEvaluator()

	// Trigger removal.
	for i := 0; i < 10; i++ {
		me.RecordReview("openai:gpt-5.1", i < 2)
	}
	me.EvaluateAll()

	if me.IsInReviewRotation("openai:gpt-5.1") {
		t.Fatal("model should be removed")
	}

	// Manually set lastCleanEval to 31 days ago.
	me.mu.Lock()
	me.lastCleanEval["openai:gpt-5.1"] = time.Now().Add(-31 * 24 * time.Hour)
	me.mu.Unlock()

	// Re-evaluate should trigger re-admission.
	me.EvaluateAll()

	if !me.IsInReviewRotation("openai:gpt-5.1") {
		t.Error("model should be re-admitted after 30 clean days")
	}
}

func TestSelectionScore(t *testing.T) {
	me := NewModelEvaluator()

	// 10 merges, 1 incident → 10% incident rate.
	for i := 0; i < 10; i++ {
		me.RecordMerge("openai:gpt-5.1", true)
	}
	me.RecordIncident("openai:gpt-5.1")
	me.RecordIncident("openai:gpt-5.1") // 2 incidents, 10 merges = 20%
	me.Evaluate("openai:gpt-5.1")

	score := me.SelectionScore("openai:gpt-5.1", 0.8, 0.01)
	// trustScore*0.70 + (1-0.2)*0.20 + (1-0)*0.10 = 0.56 + 0.16 + 0.10 = 0.82
	expected := 0.8*0.70 + 0.8*0.20 + 0.10
	if math.Abs(score-expected) > 0.001 {
		t.Errorf("expected score ~%f, got %f", expected, score)
	}
}

func TestSelectionScore_NoData(t *testing.T) {
	me := NewModelEvaluator()
	score := me.SelectionScore("unknown:model", 0.9, 0.01)
	expected := 0.9 * 0.70 // no data: trust only
	if math.Abs(score-expected) > 0.001 {
		t.Errorf("expected score %f, got %f", expected, score)
	}
}

func TestFleetAvgIncidentRate(t *testing.T) {
	me := NewModelEvaluator()

	// Model A: 10 merges, 2 incidents → 20%.
	for i := 0; i < 10; i++ {
		me.RecordMerge("a:model", true)
	}
	me.RecordIncident("a:model")
	me.RecordIncident("a:model")

	// Model B: 10 merges, 0 incidents → 0%.
	for i := 0; i < 10; i++ {
		me.RecordMerge("b:model", true)
	}

	me.EvaluateAll()

	avg := me.FleetAvgIncidentRate()
	expected := 0.1 // (2+0)/(10+10) = 2/20
	if math.Abs(avg-expected) > 0.001 {
		t.Errorf("expected fleet avg %f, got %f", expected, avg)
	}
}

func TestIncidentRateFlagging(t *testing.T) {
	me := NewModelEvaluator()

	// Model A (the target): 10 merges, 5 incidents → 50% incident rate.
	for i := 0; i < 10; i++ {
		me.RecordMerge("a:model", true)
	}
	for i := 0; i < 5; i++ {
		me.RecordIncident("a:model")
	}

	// Model B (baseline): 100 merges, 1 incident → 1% incident rate.
	for i := 0; i < 100; i++ {
		me.RecordMerge("b:model", true)
	}
	me.RecordIncident("b:model")

	me.EvaluateAll()

	// Fleet avg = (5+1)/(10+100) = 6/110 ≈ 0.0545
	// 2× fleet avg ≈ 0.109
	// Model A incident rate = 0.5 > 2× fleet avg → should be flagged.
	if !me.IsFlagged("a:model") {
		t.Error("model A should be flagged (incident rate 0.5 > 2× fleet avg 0.109)")
	}
}

func TestConcurrentAccess(t *testing.T) {
	me := NewModelEvaluator()
	var wg sync.WaitGroup

	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				me.RecordMerge("openai:gpt-5.1", true)
				me.RecordIncident("openai:gpt-5.1")
				me.RecordReview("openai:gpt-5.1", false)
				_ = me.IsInReviewRotation("openai:gpt-5.1")
				_ = me.ListModels()
			}
		}(g)
	}

	wg.Wait()

	// Verify no data races by checking consistency.
	m, ok := me.GetMetrics("openai:gpt-5.1")
	if !ok {
		t.Fatal("model not found after concurrent access")
	}
	if m.TotalMerges != 1000 {
		t.Errorf("expected 1000 merges, got %d", m.TotalMerges)
	}
}

func TestConcurrentEvaluate(t *testing.T) {
	me := NewModelEvaluator()

	// Seed data.
	for i := 0; i < 100; i++ {
		me.RecordMerge("openai:gpt-5.1", true)
		me.RecordIncident("openai:gpt-5.1")
	}

	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			me.EvaluateAll()
		}()
	}
	wg.Wait()

	m, ok := me.GetMetrics("openai:gpt-5.1")
	if !ok {
		t.Fatal("model lost during concurrent evaluation")
	}
	if m.TotalMerges != 100 {
		t.Errorf("expected 100 merges, got %d", m.TotalMerges)
	}
}

func TestModelsSortedBy(t *testing.T) {
	me := NewModelEvaluator()

	// Model A: high incident rate.
	for i := 0; i < 10; i++ {
		me.RecordMerge("a:model", true)
	}
	for i := 0; i < 5; i++ {
		me.RecordIncident("a:model")
	}

	// Model B: low incident rate.
	for i := 0; i < 10; i++ {
		me.RecordMerge("b:model", true)
	}
	me.RecordIncident("b:model")

	me.EvaluateAll()

	sorted := me.ModelsSortedBy("incident-rate")
	if len(sorted) < 2 {
		t.Fatal("expected at least 2 models")
	}
	// Lower incident rate should come first.
	if sorted[0].IncidentRate > sorted[1].IncidentRate {
		t.Error("models not correctly sorted by incident rate")
	}
}

func TestUpdateAvgTrustScore(t *testing.T) {
	me := NewModelEvaluator()
	me.UpdateAvgTrustScore("openai:gpt-5.1", 0.9)
	me.UpdateAvgTrustScore("openai:gpt-5.1", 0.5)

	m, ok := me.GetMetrics("openai:gpt-5.1")
	if !ok {
		t.Fatal("model not found")
	}
	// EMA: 0.3*0.9 = 0.27; then 0.3*0.5 + 0.7*0.27 = 0.15 + 0.189 = 0.339
	expected := 0.339
	if math.Abs(m.AvgTrustScore-expected) > 0.01 {
		t.Errorf("expected AvgTrustScore ~%f, got %f", expected, m.AvgTrustScore)
	}
}

func TestUpdateCost(t *testing.T) {
	me := NewModelEvaluator()
	me.UpdateCost("openai:gpt-5.1", 0.01)
	me.UpdateCost("openai:gpt-5.1", 0.02)

	m, ok := me.GetMetrics("openai:gpt-5.1")
	if !ok {
		t.Fatal("model not found")
	}
	// Moving average (always starts from 0):
	// First: 0*0.8 + 0.01*0.2 = 0.002
	// Second: 0.002*0.8 + 0.02*0.2 = 0.0016 + 0.004 = 0.0056
	if math.Abs(m.AvgCostPerMerge-0.0056) > 0.001 {
		t.Errorf("expected AvgCostPerMerge 0.0056, got %f", m.AvgCostPerMerge)
	}
}

func TestListEvents_Chronological(t *testing.T) {
	me := NewModelEvaluator()
	me.RemoveFromRotation("a:model", "reason 1")
	time.Sleep(10 * time.Millisecond)
	me.RemoveFromRotation("b:model", "reason 2")

	events := me.ListEvents()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if !events[0].Timestamp.Before(events[1].Timestamp) {
		t.Error("events should be chronological")
	}
}

func TestSelectionScoreWithMetrics(t *testing.T) {
	m := ModelMetrics{
		IncidentRate:    0.1,
		AvgCostPerMerge: 0.05,
	}

	// trustScore*0.70 + (1-0.1)*0.20 + (1-0.05/0.10)*0.10
	// = 0.8*0.70 + 0.9*0.20 + (1-0.5)*0.10
	// = 0.56 + 0.18 + 0.05 = 0.79
	score := SelectionScoreWithMetrics(m, 0.8, 0.10)
	expected := 0.79
	if math.Abs(score-expected) > 0.001 {
		t.Errorf("expected score %f, got %f", expected, score)
	}
}

func TestSetActiveAgents(t *testing.T) {
	me := NewModelEvaluator()
	me.SetActiveAgents("openai:gpt-5.1", 5)

	m, ok := me.GetMetrics("openai:gpt-5.1")
	if !ok {
		t.Fatal("model not found")
	}
	if m.ActiveAgents != 5 {
		t.Errorf("expected 5 active agents, got %d", m.ActiveAgents)
	}
}

func TestZeroMerges_ZeroIncidentRate(t *testing.T) {
	me := NewModelEvaluator()
	me.Evaluate("openai:gpt-5.1")

	m, ok := me.GetMetrics("openai:gpt-5.1")
	if !ok {
		t.Fatal("model not found")
	}
	if m.IncidentRate != 0 {
		t.Errorf("expected 0 incident rate for zero merges, got %f", m.IncidentRate)
	}
}

func TestZeroReviews_ZeroFPRate(t *testing.T) {
	me := NewModelEvaluator()
	me.Evaluate("openai:gpt-5.1")

	m, ok := me.GetMetrics("openai:gpt-5.1")
	if !ok {
		t.Fatal("model not found")
	}
	if m.FalsePositiveRate != 0 {
		t.Errorf("expected 0 FP rate for zero reviews, got %f", m.FalsePositiveRate)
	}
}
