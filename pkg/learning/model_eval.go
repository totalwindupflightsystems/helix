// Package learning — model_eval.go
//
// ModelEvaluator tracks per-model production outcomes (incident rates,
// false positive rates, review accuracy) and enforces rotation rules.
// Per spec Phase 12 §12.4.

package learning

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// ModelMetrics tracks production outcomes for a single model
// (provider:model combination, e.g. "openai:gpt-5.1").
type ModelMetrics struct {
	ModelID             string    `json:"model_id"`
	IncidentsAttributed int       `json:"incidents_attributed"`
	TotalMerges         int       `json:"total_merges"`
	IncidentRate        float64   `json:"incident_rate"`
	FalsePositives      int       `json:"false_positives"`
	TotalReviews        int       `json:"total_reviews"`
	FalsePositiveRate   float64   `json:"false_positive_rate"`
	AvgTrustScore       float64   `json:"avg_trust_score"`
	AvgCostPerMerge     float64   `json:"avg_cost_per_merge"`
	ActiveAgents        int       `json:"active_agents"`
	LastEvaluated       time.Time `json:"last_evaluated"`
}

// RotationEventType classifies model rotation events.
type RotationEventType string

const (
	RotationEventRemoved    RotationEventType = "removed"
	RotationEventReAdmitted RotationEventType = "re_admitted"
	RotationEventFlagged    RotationEventType = "flagged"
)

// RotationEvent records a model rotation decision with reason.
type RotationEvent struct {
	ModelID   string            `json:"model_id"`
	EventType RotationEventType `json:"event_type"`
	Reason    string            `json:"reason"`
	Timestamp time.Time         `json:"timestamp"`
}

// Rotation thresholds from spec §12.4 step 3.
const (
	FPRateRemovalThreshold  = 0.15 // 15%
	IncidentRateMultiplier  = 2.0  // 2× fleet average
	ConsecutiveDaysForPerm  = 14   // 14 days above threshold → permanent
	CleanDaysForReAdmission = 30   // 30 clean days → re-admitted
)

// ModelEvaluator tracks per-model metrics and enforces rotation rules.
// Thread-safe via sync.RWMutex.
type ModelEvaluator struct {
	mu sync.RWMutex

	metrics         map[string]*ModelMetrics
	events          []RotationEvent
	removedModels   map[string]bool
	flaggedModels   map[string]bool
	consecutiveDays map[string]int       // modelID → consecutive days above threshold
	lastCleanEval   map[string]time.Time // modelID → first clean eval day
}

// NewModelEvaluator creates an empty evaluator.
func NewModelEvaluator() *ModelEvaluator {
	return &ModelEvaluator{
		metrics:         make(map[string]*ModelMetrics),
		events:          make([]RotationEvent, 0),
		removedModels:   make(map[string]bool),
		flaggedModels:   make(map[string]bool),
		consecutiveDays: make(map[string]int),
		lastCleanEval:   make(map[string]time.Time),
	}
}

// ensureModel returns or creates the metrics entry for modelID.
// Caller must hold mu.
func (me *ModelEvaluator) ensureModel(modelID string) *ModelMetrics {
	m, ok := me.metrics[modelID]
	if !ok {
		m = &ModelMetrics{ModelID: modelID}
		me.metrics[modelID] = m
	}
	return m
}

// RecordMerge records a merge by an agent using the given model.
func (me *ModelEvaluator) RecordMerge(modelID string, success bool) {
	me.mu.Lock()
	defer me.mu.Unlock()
	m := me.ensureModel(modelID)
	m.TotalMerges++
}

// RecordIncident records a production incident for the given model.
func (me *ModelEvaluator) RecordIncident(modelID string) {
	me.mu.Lock()
	defer me.mu.Unlock()
	m := me.ensureModel(modelID)
	m.IncidentsAttributed++
}

// RecordReview records a review outcome for the given model.
func (me *ModelEvaluator) RecordReview(modelID string, falsePositive bool) {
	me.mu.Lock()
	defer me.mu.Unlock()
	m := me.ensureModel(modelID)
	m.TotalReviews++
	if falsePositive {
		m.FalsePositives++
	}
}

// Evaluate recalculates rates for a single model.
func (me *ModelEvaluator) Evaluate(modelID string) {
	me.mu.Lock()
	defer me.mu.Unlock()

	now := time.Now()
	m := me.ensureModel(modelID)
	me.recalcRates(m, now)
}

// EvaluateAll recalculates rates for all models and applies rotation rules.
func (me *ModelEvaluator) EvaluateAll() {
	me.mu.Lock()
	defer me.mu.Unlock()

	now := time.Now()

	for _, m := range me.metrics {
		me.recalcRates(m, now)
	}

	// Apply rotation rules after recalculating all rates.
	fleetAvg := me.fleetAvgIncidentRateLocked()
	for _, m := range me.metrics {
		me.applyRotationRules(m, fleetAvg, now)
	}
}

// recalcRates computes incident rate and FP rate from accumulated counts.
// Caller must hold mu.
func (me *ModelEvaluator) recalcRates(m *ModelMetrics, now time.Time) {
	if m.TotalMerges > 0 {
		m.IncidentRate = float64(m.IncidentsAttributed) / float64(m.TotalMerges)
	}
	if m.TotalReviews > 0 {
		m.FalsePositiveRate = float64(m.FalsePositives) / float64(m.TotalReviews)
	}
	m.LastEvaluated = now
}

// applyRotationRules checks and enforces rotation rules for a single model.
// Caller must hold mu.
func (me *ModelEvaluator) applyRotationRules(m *ModelMetrics, fleetAvg float64, now time.Time) {
	fprExceeded := m.FalsePositiveRate > FPRateRemovalThreshold && m.TotalReviews > 0
	irExceeded := m.IncidentRate > fleetAvg*IncidentRateMultiplier && m.TotalMerges > 0

	// Check re-admission for previously removed models (runs regardless of current rates).
	if me.removedModels[m.ModelID] {
		lastClean, exists := me.lastCleanEval[m.ModelID]
		if !exists {
			me.lastCleanEval[m.ModelID] = now
		} else if now.Sub(lastClean) >= CleanDaysForReAdmission*24*time.Hour {
			me.reAdmitToRotationLocked(m.ModelID, now)
			return
		}
		// Still removed; don't apply other rules.
		return
	}

	if fprExceeded {
		me.removeFromRotationLocked(m.ModelID, fmt.Sprintf("FPR %.1f%% exceeds %.0f%% threshold", m.FalsePositiveRate*100, FPRateRemovalThreshold*100), now)
		return
	}

	if irExceeded {
		me.consecutiveDays[m.ModelID]++
		me.flaggedModels[m.ModelID] = true
		me.recordEventLocked(m.ModelID, RotationEventFlagged, fmt.Sprintf("incident rate %.4f > %.4f (2x fleet avg %.4f)", m.IncidentRate, fleetAvg*IncidentRateMultiplier, fleetAvg), now)

		if me.consecutiveDays[m.ModelID] >= ConsecutiveDaysForPerm {
			me.removeFromRotationLocked(m.ModelID, fmt.Sprintf("%d consecutive days above rotation threshold", me.consecutiveDays[m.ModelID]), now)
		}
	} else {
		me.consecutiveDays[m.ModelID] = 0
	}
}

// removeFromRotationLocked marks a model as removed and logs the event.
// Caller must hold mu.
func (me *ModelEvaluator) removeFromRotationLocked(modelID, reason string, now time.Time) {
	me.removedModels[modelID] = true
	me.recordEventLocked(modelID, RotationEventRemoved, reason, now)
}

// reAdmitToRotationLocked marks a model as re-admitted and logs the event.
// Caller must hold mu.
func (me *ModelEvaluator) reAdmitToRotationLocked(modelID string, now time.Time) {
	delete(me.removedModels, modelID)
	delete(me.lastCleanEval, modelID)
	me.recordEventLocked(modelID, RotationEventReAdmitted, "30 days of clean metrics", now)
}

// recordEventLocked appends a rotation event.
// Caller must hold mu.
func (me *ModelEvaluator) recordEventLocked(modelID string, eventType RotationEventType, reason string, now time.Time) {
	me.events = append(me.events, RotationEvent{
		ModelID:   modelID,
		EventType: eventType,
		Reason:    reason,
		Timestamp: now,
	})
}

// IsInReviewRotation reports whether a model is currently eligible for reviews.
func (me *ModelEvaluator) IsInReviewRotation(modelID string) bool {
	me.mu.RLock()
	defer me.mu.RUnlock()
	return !me.removedModels[modelID]
}

// RemoveFromRotation manually removes a model from rotation with a reason.
func (me *ModelEvaluator) RemoveFromRotation(modelID string, reason string) {
	me.mu.Lock()
	defer me.mu.Unlock()
	me.removeFromRotationLocked(modelID, reason, time.Now())
}

// ReAdmitToRotation manually re-admits a model to review rotation.
func (me *ModelEvaluator) ReAdmitToRotation(modelID string) {
	me.mu.Lock()
	defer me.mu.Unlock()
	me.reAdmitToRotationLocked(modelID, time.Now())
}

// SelectionScore computes a weighted selection score for a model.
// Formula: trustScore*0.70 + (1-incidentRate)*0.20 + (1-costEfficiency)*0.10
// where costEfficiency = costEstimate / fleetMaxCost.
func (me *ModelEvaluator) SelectionScore(modelID string, trustScore, costEstimate float64) float64 {
	me.mu.RLock()
	defer me.mu.RUnlock()

	m, ok := me.metrics[modelID]
	if !ok {
		return trustScore * 0.70 // no data: trust only
	}

	// Clamp incident rate to [0, 1].
	ir := m.IncidentRate
	if ir > 1.0 {
		ir = 1.0
	}
	if ir < 0 {
		ir = 0
	}

	// Cost efficiency: cap at 1.0, normalize against fleet max.
	costEff := 0.0
	if costEstimate > 0 {
		fleetMax := me.fleetMaxCostLocked()
		if fleetMax > 0 {
			costEff = costEstimate / fleetMax
			if costEff > 1.0 {
				costEff = 1.0
			}
		}
	}

	return trustScore*0.70 + (1.0-ir)*0.20 + (1.0-costEff)*0.10
}

// fleetMaxCostLocked returns the highest AvgCostPerMerge across all models.
// Caller must hold mu (at least RLock).
func (me *ModelEvaluator) fleetMaxCostLocked() float64 {
	maxCost := 0.0
	for _, m := range me.metrics {
		if m.AvgCostPerMerge > maxCost {
			maxCost = m.AvgCostPerMerge
		}
	}
	return maxCost
}

// FleetAvgIncidentRate returns the average incident rate across all models.
func (me *ModelEvaluator) FleetAvgIncidentRate() float64 {
	me.mu.RLock()
	defer me.mu.RUnlock()
	return me.fleetAvgIncidentRateLocked()
}

// fleetAvgIncidentRateLocked computes average incident rate.
// Caller must hold mu (at least RLock).
func (me *ModelEvaluator) fleetAvgIncidentRateLocked() float64 {
	if len(me.metrics) == 0 {
		return 0
	}
	totalIncidents := 0
	totalMerges := 0
	for _, m := range me.metrics {
		totalIncidents += m.IncidentsAttributed
		totalMerges += m.TotalMerges
	}
	if totalMerges == 0 {
		return 0
	}
	return float64(totalIncidents) / float64(totalMerges)
}

// GetMetrics returns the metrics for a given model.
// Returns a zero ModelMetrics with ok=false when model is unknown.
func (me *ModelEvaluator) GetMetrics(modelID string) (ModelMetrics, bool) {
	me.mu.RLock()
	defer me.mu.RUnlock()
	m, ok := me.metrics[modelID]
	if !ok {
		return ModelMetrics{ModelID: modelID}, false
	}
	return *m, true
}

// ListModels returns all tracked model IDs, sorted alphabetically.
func (me *ModelEvaluator) ListModels() []string {
	me.mu.RLock()
	defer me.mu.RUnlock()
	ids := make([]string, 0, len(me.metrics))
	for id := range me.metrics {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// ListEvents returns all rotation events, chronologically.
func (me *ModelEvaluator) ListEvents() []RotationEvent {
	me.mu.RLock()
	defer me.mu.RUnlock()
	out := make([]RotationEvent, len(me.events))
	copy(out, me.events)
	return out
}

// IsFlagged returns whether a model has been flagged for review.
func (me *ModelEvaluator) IsFlagged(modelID string) bool {
	me.mu.RLock()
	defer me.mu.RUnlock()
	return me.flaggedModels[modelID]
}

// UpdateAvgTrustScore updates the average trust score for a model.
// Uses exponential moving average: new_avg = α*newScore + (1-α)*old_avg.
func (me *ModelEvaluator) UpdateAvgTrustScore(modelID string, newScore float64) {
	me.mu.Lock()
	defer me.mu.Unlock()
	m := me.ensureModel(modelID)
	const alpha = 0.3
	m.AvgTrustScore = alpha*newScore + (1.0-alpha)*m.AvgTrustScore
}

// UpdateCost updates the average cost per merge for a model.
func (me *ModelEvaluator) UpdateCost(modelID string, cost float64) {
	me.mu.Lock()
	defer me.mu.Unlock()
	m := me.ensureModel(modelID)
	// Moving average always (handles TotalMerges==0 gracefully).
	m.AvgCostPerMerge = m.AvgCostPerMerge*0.8 + cost*0.2
}

// SetActiveAgents sets the active agent count for a model.
func (me *ModelEvaluator) SetActiveAgents(modelID string, count int) {
	me.mu.Lock()
	defer me.mu.Unlock()
	m := me.ensureModel(modelID)
	m.ActiveAgents = count
}

// ModelsSortedBy returns model IDs sorted by the given sort key.
// Valid keys: "incident-rate", "false-positive-rate", "cost".
func (me *ModelEvaluator) ModelsSortedBy(sortBy string) []*ModelMetrics {
	me.mu.RLock()
	defer me.mu.RUnlock()

	models := make([]*ModelMetrics, 0, len(me.metrics))
	for _, m := range me.metrics {
		models = append(models, m)
	}

	switch sortBy {
	case "incident-rate":
		sort.Slice(models, func(i, j int) bool {
			return models[i].IncidentRate < models[j].IncidentRate
		})
	case "false-positive-rate":
		sort.Slice(models, func(i, j int) bool {
			return models[i].FalsePositiveRate < models[j].FalsePositiveRate
		})
	case "cost":
		sort.Slice(models, func(i, j int) bool {
			return models[i].AvgCostPerMerge < models[j].AvgCostPerMerge
		})
	default:
		// Default: sort by model ID.
		sort.Slice(models, func(i, j int) bool {
			return models[i].ModelID < models[j].ModelID
		})
	}

	return models
}

// SelectionScoreWithMetrics computes selection score using model metrics.
// Formula: trustScore*0.70 + (1-incidentRate)*0.20 + (1-costEfficiency)*0.10.
func SelectionScoreWithMetrics(m ModelMetrics, trustScore float64, fleetMaxCost float64) float64 {
	ir := m.IncidentRate
	if ir > 1.0 {
		ir = 1.0
	}
	if ir < 0 {
		ir = 0
	}

	costEff := 0.0
	if fleetMaxCost > 0 && m.AvgCostPerMerge > 0 {
		costEff = math.Min(m.AvgCostPerMerge/fleetMaxCost, 1.0)
	}

	return trustScore*0.70 + (1.0-ir)*0.20 + (1.0-costEff)*0.10
}
