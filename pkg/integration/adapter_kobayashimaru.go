package integration

// ---------------------------------------------------------------------------
// Kobayashi-Maru Adapter — specs/integrations.md §5
// ---------------------------------------------------------------------------
//
// Kobayashi-Maru is the no-win scenario training system. Runs adversarial
// stress tests against Helix agents to verify they can't cheat quality gates.
// Exhaustive specification, Ralph Loop engine, penetration testing, Prometheus
// + Loki monitoring.

// KobayashiMaruAdapter defines the contract for stress-testing Helix agents.
type KobayashiMaruAdapter interface {
	// RunScenario executes a no-win scenario against a target agent or component.
	RunScenario(scenario Scenario, opts ScenarioOpts) (*ScenarioResult, error)

	// ListScenarios returns available stress test scenarios.
	ListScenarios() ([]Scenario, error)

	// Metrics returns Prometheus metrics from the last run.
	Metrics() (*MaruMetrics, error)
}

// Scenario describes a stress test scenario.
type Scenario struct {
	ID          string
	Name        string
	Description string
	Target      string // "gitreins", "chimera", "conscientiousness", "agent-identity", "all"
	Difficulty  string // "kobayashi-maru" (unwinnable), "difficult", "standard"
}

// ScenarioOpts configures a scenario run.
type ScenarioOpts struct {
	Timeout     string            // "5m", "30m", "2h"
	Params      map[string]string // Scenario-specific parameters
	RecordVideo bool              // Record terminal session
}

// ScenarioResult captures the outcome of a stress test.
type ScenarioResult struct {
	Passed         bool    // Did the target survive? (Almost always false for unwinnable)
	Score          float64 // 0.0-1.0
	Attempts       int
	CheatsDetected []CheatAttempt
	Findings       []ScenarioFinding
	Logs           string // Loki query reference
	Cost           float64
}

// CheatAttempt records a detected cheating attempt during a scenario.
type CheatAttempt struct {
	Type       string // "skip_gate", "fake_evidence", "backdoor", "privilege_escalation"
	DetectedBy string // Which check caught it
	Timestamp  string
}

// ScenarioFinding documents a discovered vulnerability or weakness.
type ScenarioFinding struct {
	Severity    string
	Component   string
	Description string
	Remediation string
}

// MaruMetrics provides aggregate stress-testing statistics.
type MaruMetrics struct {
	TotalRuns          int
	SurvivalRate       float64
	AvgScore           float64
	CheatDetectionRate float64
}
