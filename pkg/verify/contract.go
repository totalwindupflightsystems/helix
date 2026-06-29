// Package verify implements post-merge production verification and behavior
// contract monitoring per specs/production-verification.md.
//
// Behavior contracts are YAML assertions committed alongside code that define
// expected runtime behavior. The surveillance system continuously checks these
// contracts and triggers auto-rollback on breach, integrating with the trust
// model for agent accountability.
package verify

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// =============================================================================
// BehaviorContract
// =============================================================================

// BehaviorContract is the top-level contract document, typically committed as
// .helix/contracts/<name>.yaml alongside the code it governs.
type BehaviorContract struct {
	Contract ContractBody `yaml:"contract"`
}

// ContractBody carries the contract metadata and assertions.
type ContractBody struct {
	Name         string      `yaml:"name"`
	Agent        string      `yaml:"agent"`
	MergeCommit  string      `yaml:"merge_commit"`
	Assertions   []Assertion `yaml:"assertions"`
	BreachAction string      `yaml:"breach_action"`
}

// Assertion defines a single behavior expectation.
type Assertion struct {
	Metric string  `yaml:"metric"`
	Op     string  `yaml:"operator"` // "gte", "lte", "eq"
	Value  float64 `yaml:"value"`
	Window string  `yaml:"window"` // e.g., "1h", "24h"
}

// AssertionOperator returns the operator for display/validation.
type AssertionOperator int

const (
	OpInvalid AssertionOperator = iota
	OpGte                       // >=
	OpLte                       // <=
	OpEq                        // ==
)

// Operator resolves a string operator to the typed constant.
func Operator(s string) (AssertionOperator, error) {
	switch s {
	case "gte":
		return OpGte, nil
	case "lte":
		return OpLte, nil
	case "eq":
		return OpEq, nil
	default:
		return OpInvalid, fmt.Errorf("unknown operator %q", s)
	}
}

// ParseContract parses a YAML behavior contract.
func ParseContract(data []byte) (*BehaviorContract, error) {
	var c BehaviorContract
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse contract: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate checks that the contract has all required fields and valid assertions.
func (c *BehaviorContract) Validate() error {
	b := c.Contract
	if b.Name == "" {
		return fmt.Errorf("contract name is required")
	}
	if b.Agent == "" {
		return fmt.Errorf("contract agent is required")
	}
	if b.MergeCommit == "" {
		return fmt.Errorf("merge_commit is required")
	}
	if len(b.Assertions) == 0 {
		return fmt.Errorf("at least one assertion is required")
	}
	for i, a := range b.Assertions {
		if a.Metric == "" {
			return fmt.Errorf("assertion %d: metric is required", i)
		}
		if _, err := Operator(a.Op); err != nil {
			return fmt.Errorf("assertion %d: %w", i, err)
		}
		if a.Window == "" {
			return fmt.Errorf("assertion %d: window is required", i)
		}
	}
	ba := b.BreachAction
	if ba != "" && ba != "rollback" && ba != "rollback_and_notify" && ba != "notify_only" {
		return fmt.Errorf("unknown breach_action %q (expected rollback, rollback_and_notify, or notify_only)", ba)
	}
	return nil
}

// ShouldRollback returns true if the breach action requires auto-rollback.
func (c *BehaviorContract) ShouldRollback() bool {
	ba := c.Contract.BreachAction
	return ba == "rollback" || ba == "rollback_and_notify"
}

// ShouldNotify returns true if the breach action requires notification.
func (c *BehaviorContract) ShouldNotify() bool {
	ba := c.Contract.BreachAction
	return ba == "rollback_and_notify" || ba == "notify_only"
}

// =============================================================================
// CheckResult
// =============================================================================

// CheckResult is the outcome of checking one assertion against a measured value.
type CheckResult struct {
	Assertion     Assertion `json:"assertion"`
	MeasuredValue float64   `json:"measured_value"`
	Passed        bool      `json:"passed"`
	Reason        string    `json:"reason,omitempty"`
}

// =============================================================================
// Checker
// =============================================================================

// Checker evaluates assertions against measured values.
type Checker struct{}

// NewChecker creates a Checker.
func NewChecker() *Checker {
	return &Checker{}
}

// Check evaluates a single assertion.
func (ch *Checker) Check(a Assertion, measured float64) CheckResult {
	op, err := Operator(a.Op)
	if err != nil {
		return CheckResult{Assertion: a, MeasuredValue: measured, Passed: false, Reason: err.Error()}
	}
	switch op {
	case OpGte:
		if measured >= a.Value {
			return CheckResult{Assertion: a, MeasuredValue: measured, Passed: true}
		}
		return CheckResult{
			Assertion: a, MeasuredValue: measured, Passed: false,
			Reason: fmt.Sprintf("measured %.4f < threshold %.4f (expected >=)", measured, a.Value),
		}
	case OpLte:
		if measured <= a.Value {
			return CheckResult{Assertion: a, MeasuredValue: measured, Passed: true}
		}
		return CheckResult{
			Assertion: a, MeasuredValue: measured, Passed: false,
			Reason: fmt.Sprintf("measured %.4f > threshold %.4f (expected <=)", measured, a.Value),
		}
	case OpEq:
		if measured == a.Value {
			return CheckResult{Assertion: a, MeasuredValue: measured, Passed: true}
		}
		return CheckResult{
			Assertion: a, MeasuredValue: measured, Passed: false,
			Reason: fmt.Sprintf("measured %.4f != threshold %.4f (expected ==)", measured, a.Value),
		}
	default:
		return CheckResult{Assertion: a, MeasuredValue: measured, Passed: false, Reason: "unknown operator"}
	}
}

// CheckAll evaluates all assertions in a contract against a map of metric→value.
func (ch *Checker) CheckAll(c *BehaviorContract, metrics map[string]float64) []CheckResult {
	var results []CheckResult
	for _, a := range c.Contract.Assertions {
		val, ok := metrics[a.Metric]
		if !ok {
			results = append(results, CheckResult{
				Assertion: a, Passed: false,
				Reason: fmt.Sprintf("metric %q not found in measured data", a.Metric),
			})
			continue
		}
		results = append(results, ch.Check(a, val))
	}
	return results
}

// AllPassed returns true if every check result passed.
func AllPassed(results []CheckResult) bool {
	for _, r := range results {
		if !r.Passed {
			return false
		}
	}
	return true
}

// =============================================================================
// Canary Schedule
// =============================================================================

// CanaryStep defines one step in the canary ramp.
type CanaryStep struct {
	Step       int           `json:"step"`
	TrafficPct float64       `json:"traffic_pct"` // percentage of traffic to new code
	Duration   time.Duration `json:"duration"`    // how long this step lasts
}

// CanarySchedule returns the deployment ramp schedule for a given trust tier.
// Per spec §Agent-Specific Canary Rules:
//
//	Provisional: 24h shadow + 6 steps = 96h total to 100%
//	Observed:    12h shadow + 4 steps = 60h total
//	Trusted:      6h shadow + 3 steps = 36h total
//	Veteran:      2h shadow + 2 steps = 12h total
func CanarySchedule(tier string) (shadowDuration time.Duration, steps []CanaryStep) {
	switch tier {
	case "provisional":
		shadowDuration = 24 * time.Hour
		steps = []CanaryStep{
			{Step: 1, TrafficPct: 1, Duration: 12 * time.Hour},
			{Step: 2, TrafficPct: 5, Duration: 12 * time.Hour},
			{Step: 3, TrafficPct: 10, Duration: 12 * time.Hour},
			{Step: 4, TrafficPct: 25, Duration: 24 * time.Hour},
			{Step: 5, TrafficPct: 50, Duration: 24 * time.Hour},
			{Step: 6, TrafficPct: 100, Duration: 12 * time.Hour},
		}
	case "observed":
		shadowDuration = 12 * time.Hour
		steps = []CanaryStep{
			{Step: 1, TrafficPct: 1, Duration: 6 * time.Hour},
			{Step: 2, TrafficPct: 10, Duration: 12 * time.Hour},
			{Step: 3, TrafficPct: 25, Duration: 18 * time.Hour},
			{Step: 4, TrafficPct: 100, Duration: 24 * time.Hour},
		}
	case "trusted":
		shadowDuration = 6 * time.Hour
		steps = []CanaryStep{
			{Step: 1, TrafficPct: 1, Duration: 6 * time.Hour},
			{Step: 2, TrafficPct: 10, Duration: 12 * time.Hour},
			{Step: 3, TrafficPct: 100, Duration: 18 * time.Hour},
		}
	case "veteran":
		shadowDuration = 2 * time.Hour
		steps = []CanaryStep{
			{Step: 1, TrafficPct: 10, Duration: 4 * time.Hour},
			{Step: 2, TrafficPct: 100, Duration: 8 * time.Hour},
		}
	default:
		shadowDuration = 24 * time.Hour
		steps = []CanaryStep{
			{Step: 1, TrafficPct: 1, Duration: 12 * time.Hour},
			{Step: 2, TrafficPct: 100, Duration: 84 * time.Hour},
		}
	}
	return
}

// TotalCanaryDuration returns the sum of all canary step durations.
func TotalCanaryDuration(steps []CanaryStep) time.Duration {
	var total time.Duration
	for _, s := range steps {
		total += s.Duration
	}
	return total
}
