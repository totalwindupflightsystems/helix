package verify

import (
	"sync"
	"time"
)

// ============================================================================
// AlertEscalation — Sustained Drift Escalation
// ============================================================================

// AlertEscalation tracks sustained drift and breach conditions, escalating
// alert levels when conditions persist beyond configured durations.
type AlertEscalation struct {
	mu                 sync.Mutex
	sustainedThreshold time.Duration // how long drift must persist before notify
	rollbackThreshold  time.Duration // how long before rollback escalation
	driftStart         map[string]time.Time
	breachStart        map[string]time.Time
	currentLevels      map[string]EscalationLevel
}

// NewAlertEscalation creates an escalation tracker.
func NewAlertEscalation(sustainedThreshold, rollbackThreshold time.Duration) *AlertEscalation {
	return &AlertEscalation{
		sustainedThreshold: sustainedThreshold,
		rollbackThreshold:  rollbackThreshold,
		driftStart:         make(map[string]time.Time),
		breachStart:        make(map[string]time.Time),
		currentLevels:      make(map[string]EscalationLevel),
	}
}

// Evaluate checks the current status and drift assessment for an agent and
// returns the appropriate escalation level. It tracks how long drift/breach
// conditions have persisted and escalates accordingly.
func (a *AlertEscalation) Evaluate(agentID string, status SurveillanceStatus, drift *DriftAssessment) EscalationLevel {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now().UTC()
	hasDrift := drift != nil && drift.AnyBreach
	hasCriticalDrift := drift != nil && drift.CriticalCount > 0
	hasBreach := status == StatusBreach
	hasDegradation := status == StatusDegraded

	// Track drift start time.
	if hasDrift {
		if _, ok := a.driftStart[agentID]; !ok {
			a.driftStart[agentID] = now
		}
	} else {
		delete(a.driftStart, agentID)
	}

	// Track breach start time.
	if hasBreach {
		if _, ok := a.breachStart[agentID]; !ok {
			a.breachStart[agentID] = now
		}
	} else {
		delete(a.breachStart, agentID)
	}

	// Determine escalation level based on persistence.
	level := EscalationNone

	if hasBreach {
		level = EscalationRollback
	} else if hasDegradation {
		level = EscalationInvestigate
	} else if hasDrift {
		driftDuration := now.Sub(a.driftStart[agentID])
		if hasCriticalDrift {
			// Critical drift → escalate faster.
			if driftDuration >= a.sustainedThreshold/2 {
				level = EscalationInvestigate
			} else if driftDuration >= a.sustainedThreshold/4 {
				level = EscalationNotify
			}
		} else if driftDuration >= a.rollbackThreshold {
			level = EscalationRollback
		} else if driftDuration >= a.sustainedThreshold {
			level = EscalationInvestigate
		} else if driftDuration >= a.sustainedThreshold/2 {
			level = EscalationNotify
		}
	}

	a.currentLevels[agentID] = level
	return level
}

// GetLevel returns the current escalation level for an agent.
func (a *AlertEscalation) GetLevel(agentID string) EscalationLevel {
	a.mu.Lock()
	defer a.mu.Unlock()
	if level, ok := a.currentLevels[agentID]; ok {
		return level
	}
	return EscalationNone
}

// Reset clears escalation tracking for an agent (e.g., after recovery).
func (a *AlertEscalation) Reset(agentID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.driftStart, agentID)
	delete(a.breachStart, agentID)
	delete(a.currentLevels, agentID)
}

// DriftDuration returns how long sustained drift has been occurring.
func (a *AlertEscalation) DriftDuration(agentID string) time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	if start, ok := a.driftStart[agentID]; ok {
		return time.Since(start)
	}
	return 0
}

// AllLevels returns the current escalation level for all tracked agents.
func (a *AlertEscalation) AllLevels() map[string]EscalationLevel {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make(map[string]EscalationLevel, len(a.currentLevels))
	for k, v := range a.currentLevels {
		result[k] = v
	}
	return result
}
