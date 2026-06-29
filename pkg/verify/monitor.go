package verify

import (
	"fmt"
	"time"
)

// =============================================================================
// Breach
// =============================================================================

// Breach represents a contract breach event.
type Breach struct {
	ContractName string       `json:"contract_name"`
	Agent        string       `json:"agent"`
	MergeCommit  string       `json:"merge_commit"`
	Timestamp    time.Time    `json:"timestamp"`
	FailedChecks []CheckResult `json:"failed_checks"`
	ShouldRollback bool      `json:"should_rollback"`
	ShouldNotify  bool       `json:"should_notify"`
}

// BreachError returns a human-readable summary of the breach.
func (b Breach) Error() string {
	return fmt.Sprintf("contract %q breached: %d of %d assertions failed (rollback=%v, notify=%v)",
		b.ContractName, len(b.FailedChecks), b.TotalChecks(), b.ShouldRollback, b.ShouldNotify)
}

// TotalChecks returns the total number of failed checks this breach represents.
func (b Breach) TotalChecks() int {
	return len(b.FailedChecks)
}

// =============================================================================
// Monitor
// =============================================================================

// Monitor continuously checks behavior contracts against measured metrics.
// It detects breaches and determines the appropriate response (rollback, notify,
// or both).
type Monitor struct {
	contracts     map[string]*BehaviorContract
	checker       *Checker
}

// NewMonitor creates a Monitor with no registered contracts.
func NewMonitor() *Monitor {
	return &Monitor{
		contracts: make(map[string]*BehaviorContract),
		checker:   NewChecker(),
	}
}

// RegisterContract adds a behavior contract to the monitor.
func (m *Monitor) RegisterContract(c *BehaviorContract) {
	m.contracts[c.Contract.Name] = c
}

// UnregisterContract removes a contract from monitoring.
func (m *Monitor) UnregisterContract(name string) {
	delete(m.contracts, name)
}

// Contracts returns all registered contract names.
func (m *Monitor) Contracts() []string {
	var names []string
	for n := range m.contracts {
		names = append(names, n)
	}
	return names
}

// Evaluate checks all registered contracts against the provided metrics.
// Returns a slice of Breach events (one per breached contract).
// Contracts with no breached assertions are excluded from results.
func (m *Monitor) Evaluate(metrics map[string]float64) []Breach {
	var breaches []Breach

	for name, c := range m.contracts {
		results := m.checker.CheckAll(c, metrics)
		var failed []CheckResult
		for _, r := range results {
			if !r.Passed {
				failed = append(failed, r)
			}
		}
		if len(failed) > 0 {
			b := Breach{
				ContractName:   name,
				Agent:          c.Contract.Agent,
				MergeCommit:    c.Contract.MergeCommit,
				Timestamp:      time.Now().UTC(),
				FailedChecks:   failed,
				ShouldRollback: c.ShouldRollback(),
				ShouldNotify:   c.ShouldNotify(),
			}
			breaches = append(breaches, b)
		}
	}

	return breaches
}

// EvaluateOne checks a single contract by name against the provided metrics.
// Returns nil if the contract is not registered or if all assertions pass.
func (m *Monitor) EvaluateOne(name string, metrics map[string]float64) *Breach {
	c, ok := m.contracts[name]
	if !ok {
		return nil
	}

	results := m.checker.CheckAll(c, metrics)
	var failed []CheckResult
	for _, r := range results {
		if !r.Passed {
			failed = append(failed, r)
		}
	}
	if len(failed) == 0 {
		return nil
	}

	return &Breach{
		ContractName:   name,
		Agent:          c.Contract.Agent,
		MergeCommit:    c.Contract.MergeCommit,
		Timestamp:      time.Now().UTC(),
		FailedChecks:   failed,
		ShouldRollback: c.ShouldRollback(),
		ShouldNotify:   c.ShouldNotify(),
	}
}

// =============================================================================
// Drift detection
// =============================================================================

// DriftReport describes a behavioral drift from baseline.
type DriftReport struct {
	Metric       string  `json:"metric"`
	Baseline     float64 `json:"baseline"`
	Current      float64 `json:"current"`
	DriftPct     float64 `json:"drift_pct"`     // percentage change from baseline
	ThresholdPct float64 `json:"threshold_pct"` // allowed drift before flagging
	Exceeds      bool    `json:"exceeds"`
}

// DetectDrift compares current metric values to a baseline and reports drift.
// thresholdPct is the maximum allowed drift before flagging (e.g., 50.0 = 50%).
func DetectDrift(baseline, current map[string]float64, thresholdPct float64) []DriftReport {
	var reports []DriftReport

	for metric, baseVal := range baseline {
		curVal, ok := current[metric]
		if !ok {
			continue
		}

		var driftPct float64
		if baseVal != 0 {
			driftPct = ((curVal - baseVal) / baseVal) * 100.0
		} else if curVal != 0 {
			driftPct = 100.0 // went from 0 to non-zero = infinite drift
		}

		exceeds := driftPct > thresholdPct || driftPct < -thresholdPct

		reports = append(reports, DriftReport{
			Metric:       metric,
			Baseline:     baseVal,
			Current:      curVal,
			DriftPct:     driftPct,
			ThresholdPct: thresholdPct,
			Exceeds:      exceeds,
		})
	}

	return reports
}

// =============================================================================
// Shadow auto-rollback triggers (Phase 1)
// =============================================================================

// ShadowAutoRollbackTriggers evaluates the five auto-rollback conditions from
// spec §Auto-Rollback Triggers. Returns a human-readable reason if any trigger
// fires, or empty string if all pass.
func ShadowAutoRollbackTriggers(prodSuccessRate, shadowSuccessRate float64, prodP99, shadowP99 float64, newErrors int, memGrowthPct float64) string {
	// Error rate exceeds production baseline by >0.1%
	if prodSuccessRate > 0 && shadowSuccessRate < prodSuccessRate-0.001 {
		return fmt.Sprintf("error rate exceeded: production %.4f, shadow %.4f (delta >0.1%%)", prodSuccessRate, shadowSuccessRate)
	}
	// P99 latency exceeds production baseline by >20%
	if prodP99 > 0 && shadowP99 > prodP99*1.20 {
		return fmt.Sprintf("P99 latency exceeded: production %.0fms, shadow %.0fms (>20%% over baseline)", prodP99, shadowP99)
	}
	// New error types appeared
	if newErrors > 0 {
		return fmt.Sprintf("new error types detected: %d new error categories", newErrors)
	}
	// Memory usage grows >10%
	if memGrowthPct > 10.0 {
		return fmt.Sprintf("memory growth exceeded: %.1f%% (>10%% threshold)", memGrowthPct)
	}
	return ""
}
