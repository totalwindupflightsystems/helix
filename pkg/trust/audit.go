package trust

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

// ============================================================================
// Audit Types
// ============================================================================

// AuditFindingStatus classifies an individual audit finding.
type AuditFindingStatus string

const (
	AuditPass    AuditFindingStatus = "PASS"
	AuditFail    AuditFindingStatus = "FAIL"
	AuditAnomaly AuditFindingStatus = "ANOMALY"
)

// AuditFinding is a per-agent finding from the trust audit.
type AuditFinding struct {
	AgentID       string             `json:"agent_id"`
	Status        AuditFindingStatus `json:"status"`
	ComputedScore float64            `json:"computed_score"`
	ComputedTier  TrustTier          `json:"computed_tier"`
	EventCount    int                `json:"event_count"`
	Warnings      []string           `json:"warnings,omitempty"`
	Errors        []string           `json:"errors,omitempty"`
}

// IsPassing returns true if the finding is PASS (no issues).
func (f AuditFinding) IsPassing() bool {
	return f.Status == AuditPass
}

// HasAnomaly returns true if the finding has anomalies (non-blocking but noteworthy).
func (f AuditFinding) HasAnomaly() bool {
	return f.Status == AuditAnomaly
}

// AuditReport is the full result of a trust audit run.
type AuditReport struct {
	LedgerPath string         `json:"ledger_path"`
	CheckedAt  time.Time      `json:"checked_at"`
	AgentCount int            `json:"agent_count"`
	Findings   []AuditFinding `json:"findings"`
	Summary    AuditSummary   `json:"summary"`
}

// AuditSummary counts findings by status.
type AuditSummary struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Anomaly int `json:"anomaly"`
}

// IsHealthy returns true if no failures (anomalies are acceptable).
func (r AuditReport) IsHealthy() bool {
	return r.Summary.Failed == 0
}

// FindingByAgent returns the finding for a specific agent, or nil.
func (r AuditReport) FindingByAgent(agentID string) *AuditFinding {
	for i := range r.Findings {
		if r.Findings[i].AgentID == agentID {
			return &r.Findings[i]
		}
	}
	return nil
}

// FailedAgents returns the agent IDs with FAIL status.
func (r AuditReport) FailedAgents() []string {
	var agents []string
	for i := range r.Findings {
		if r.Findings[i].Status == AuditFail {
			agents = append(agents, r.Findings[i].AgentID)
		}
	}
	return agents
}

// AnomalyAgents returns the agent IDs with ANOMALY status.
func (r AuditReport) AnomalyAgents() []string {
	var agents []string
	for i := range r.Findings {
		if r.Findings[i].Status == AuditAnomaly {
			agents = append(agents, r.Findings[i].AgentID)
		}
	}
	return agents
}

// FormatReport renders the audit report as a human-readable string.
func (r AuditReport) FormatReport() string {
	s := fmt.Sprintf("Trust Audit Report — %s\n", r.CheckedAt.Format(time.RFC3339))
	s += fmt.Sprintf("Ledger: %s\n", r.LedgerPath)
	s += fmt.Sprintf("Agents: %d (pass=%d, fail=%d, anomaly=%d)\n\n",
		r.Summary.Total, r.Summary.Passed, r.Summary.Failed, r.Summary.Anomaly)

	for i := range r.Findings {
		f := &r.Findings[i]
		s += fmt.Sprintf("  %s %s: score=%.4f tier=%s events=%d\n",
			f.Status, f.AgentID, f.ComputedScore, f.ComputedTier, f.EventCount)
		for _, w := range f.Warnings {
			s += fmt.Sprintf("    ⚠ %s\n", w)
		}
		for _, e := range f.Errors {
			s += fmt.Sprintf("    ✗ %s\n", e)
		}
	}
	return s
}

// ============================================================================
// Anomaly Types
// ============================================================================

// AnomalyType classifies a detected anomaly in the trust ledger.
type AnomalyType string

const (
	AnomalyScoreDrift     AnomalyType = "score_drift"
	AnomalyMissingEvents  AnomalyType = "missing_events"
	AnomalyCorruptedEntry AnomalyType = "corrupted_entry"
	AnomalyTierMismatch   AnomalyType = "tier_mismatch"
	AnomalyBackwardsScore AnomalyType = "backwards_score"
	AnomalyNoActivity     AnomalyType = "no_activity"
)

// Anomaly is a single detected anomaly.
type Anomaly struct {
	Type     AnomalyType `json:"type"`
	AgentID  string      `json:"agent_id"`
	Detail   string      `json:"detail"`
	Severity string      `json:"severity"`
}

// ============================================================================
// TrustAuditRunner
// ============================================================================

// TrustAuditRunner performs a full audit of the trust system.
// It replays all JSONL ledger entries for every agent, verifies each
// agent's computed score is consistent, and detects anomalies.
type TrustAuditRunner struct {
	ledgerPath string
	// scoreTolerance is the allowed difference between a stored score and
	// the replayed score before it's flagged as drift. Default: 0.01.
	scoreTolerance float64
	// maxInactivityDays is the threshold for flagging an agent as inactive.
	// Default: 90 days.
	maxInactivityDays int
}

// AuditOption configures a TrustAuditRunner.
type AuditOption func(*TrustAuditRunner)

// WithScoreTolerance sets the allowed score drift tolerance.
func WithScoreTolerance(t float64) AuditOption {
	return func(r *TrustAuditRunner) { r.scoreTolerance = t }
}

// WithMaxInactivityDays sets the inactivity threshold.
func WithMaxInactivityDays(d int) AuditOption {
	return func(r *TrustAuditRunner) { r.maxInactivityDays = d }
}

// NewTrustAuditRunner creates an audit runner for the given ledger path.
func NewTrustAuditRunner(ledgerPath string, opts ...AuditOption) *TrustAuditRunner {
	r := &TrustAuditRunner{
		ledgerPath:        ledgerPath,
		scoreTolerance:    0.01,
		maxInactivityDays: 90,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Run performs a full audit of the trust ledger.
// It replays all events, computes scores, detects anomalies, and returns
// an AuditReport with per-agent findings.
func (r *TrustAuditRunner) Run() (*AuditReport, error) {
	events, err := Replay(r.ledgerPath)
	if err != nil {
		return nil, fmt.Errorf("replay ledger: %w", err)
	}

	// Detect corrupted entries (lines that failed to parse).
	corruptedLines, err := r.countCorruptedLines()
	if err != nil {
		corruptedLines = 0 // non-fatal
	}

	// Group events by agent.
	agentEvents := groupEventsByAgent(events)
	agentIDs := make([]string, 0, len(agentEvents))
	for id := range agentEvents {
		agentIDs = append(agentIDs, id)
	}
	sort.Strings(agentIDs)

	report := &AuditReport{
		LedgerPath: r.ledgerPath,
		CheckedAt:  time.Now().UTC(),
		AgentCount: len(agentIDs),
		Findings:   make([]AuditFinding, 0, len(agentIDs)),
	}

	for _, agentID := range agentIDs {
		finding := r.auditAgent(agentID, agentEvents[agentID], events)
		if corruptedLines > 0 {
			finding.Warnings = append(finding.Warnings,
				fmt.Sprintf("%d corrupted ledger lines detected", corruptedLines))
		}
		report.Findings = append(report.Findings, finding)
	}

	// Build summary.
	for i := range report.Findings {
		report.Summary.Total++
		switch report.Findings[i].Status {
		case AuditPass:
			report.Summary.Passed++
		case AuditFail:
			report.Summary.Failed++
		case AuditAnomaly:
			report.Summary.Anomaly++
		}
	}

	return report, nil
}

// auditAgent audits a single agent's trust data.
func (r *TrustAuditRunner) auditAgent(agentID string, agentEvents []TrustEvent, allEvents []TrustEvent) AuditFinding {
	finding := AuditFinding{
		AgentID:    agentID,
		EventCount: len(agentEvents),
	}

	// Compute score via replay.
	score, err := ReplayToScore(r.ledgerPath, agentID)
	if err != nil {
		finding.Status = AuditFail
		finding.Errors = append(finding.Errors, fmt.Sprintf("score replay failed: %v", err))
		return finding
	}
	finding.ComputedScore = float64(score)

	// Compute tier from events.
	finding.ComputedTier = computeTierFromEvents(agentEvents, score)

	// Check for anomalies.
	var anomalies []Anomaly

	// 1. Score drift: last stored score_after differs from computed.
	if a := r.checkScoreDrift(agentID, agentEvents, score); a != nil {
		anomalies = append(anomalies, *a)
	}

	// 2. Tier mismatch: stored tier doesn't match computed tier.
	if a := r.checkTierMismatch(agentEvents, finding.ComputedTier); a != nil {
		anomalies = append(anomalies, *a)
	}

	// 3. Backwards score: score decreased without incident/decay event.
	if a := r.checkBackwardsScore(agentID, agentEvents); a != nil {
		anomalies = append(anomalies, *a)
	}

	// 4. No activity: agent has no events in maxInactivityDays.
	if a := r.checkNoActivity(agentID, agentEvents); a != nil {
		anomalies = append(anomalies, *a)
	}

	// 5. Missing events: agent has zero events.
	if len(agentEvents) == 0 {
		finding.Status = AuditAnomaly
		finding.Warnings = append(finding.Warnings, "agent has zero trust events")
	}

	// Classify anomalies into warnings/errors.
	hasError := false
	for _, a := range anomalies {
		msg := fmt.Sprintf("%s: %s", a.Type, a.Detail)
		if a.Severity == "error" {
			finding.Errors = append(finding.Errors, msg)
			hasError = true
		} else {
			finding.Warnings = append(finding.Warnings, msg)
		}
	}

	// Determine status.
	if finding.Status == AuditFail || hasError {
		finding.Status = AuditFail
	} else if len(finding.Warnings) > 0 || finding.Status == AuditAnomaly {
		finding.Status = AuditAnomaly
	} else {
		finding.Status = AuditPass
	}

	return finding
}

// checkScoreDrift detects when the last stored score_after differs
// significantly from the replayed score.
func (r *TrustAuditRunner) checkScoreDrift(agentID string, events []TrustEvent, computed TrustScore) *Anomaly {
	if len(events) == 0 {
		return nil
	}

	// Find the last event with a score_after value.
	var lastStored float64
	var found bool
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Data.ScoreAfter != 0 {
			lastStored = events[i].Data.ScoreAfter
			found = true
			break
		}
	}
	if !found {
		return nil
	}

	diff := lastStored - float64(computed)
	if diff < 0 {
		diff = -diff
	}
	if diff > r.scoreTolerance {
		return &Anomaly{
			Type:     AnomalyScoreDrift,
			AgentID:  agentID,
			Detail:   fmt.Sprintf("stored score %.4f differs from computed %.4f (diff=%.4f, tolerance=%.4f)", lastStored, float64(computed), diff, r.scoreTolerance),
			Severity: "error",
		}
	}
	return nil
}

// checkTierMismatch detects when the last stored tier doesn't match
// the computed tier from the current score.
func (r *TrustAuditRunner) checkTierMismatch(events []TrustEvent, computedTier TrustTier) *Anomaly {
	// Find the last tier change event.
	var lastTier string
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].EventType == EventTierChange && events[i].Data.NewTier != "" {
			lastTier = events[i].Data.NewTier
			break
		}
		if events[i].EventType == EventDemotion && events[i].Data.NewTier != "" {
			lastTier = events[i].Data.NewTier
			break
		}
	}
	if lastTier == "" {
		return nil // no stored tier to compare
	}
	if lastTier != string(computedTier) {
		return &Anomaly{
			Type:     AnomalyTierMismatch,
			AgentID:  events[0].AgentID,
			Detail:   fmt.Sprintf("stored tier %q doesn't match computed tier %q", lastTier, computedTier),
			Severity: "warning",
		}
	}
	return nil
}

// checkBackwardsScore detects when the score decreased without a corresponding
// incident penalty or decay event.
func (r *TrustAuditRunner) checkBackwardsScore(agentID string, events []TrustEvent) *Anomaly {
	if len(events) < 2 {
		return nil
	}

	// Look at consecutive score-change events.
	for i := 1; i < len(events); i++ {
		prev := events[i-1].Data.ScoreAfter
		curr := events[i].Data.ScoreAfter
		if prev == 0 || curr == 0 {
			continue
		}
		if curr < prev {
			// Score dropped — check if it's justified.
			justified := events[i].EventType == EventIncidentPenalty ||
				events[i].EventType == EventDecayApplied ||
				events[i].EventType == EventDemotion
			if !justified {
				return &Anomaly{
					Type:     AnomalyBackwardsScore,
					AgentID:  agentID,
					Detail:   fmt.Sprintf("score dropped from %.4f to %.4f at %s without incident/decay event", prev, curr, events[i].Timestamp.Format(time.RFC3339)),
					Severity: "warning",
				}
			}
		}
	}
	return nil
}

// checkNoActivity flags agents with no recent trust events.
func (r *TrustAuditRunner) checkNoActivity(agentID string, events []TrustEvent) *Anomaly {
	if len(events) == 0 {
		return nil
	}

	lastEvent := events[len(events)-1].Timestamp
	daysSince := time.Since(lastEvent).Hours() / 24
	if int(daysSince) > r.maxInactivityDays {
		return &Anomaly{
			Type:     AnomalyNoActivity,
			AgentID:  agentID,
			Detail:   fmt.Sprintf("no trust events for %.0f days (last: %s)", daysSince, lastEvent.Format(time.RFC3339)),
			Severity: "warning",
		}
	}
	return nil
}

// countCorruptedLines counts lines in the ledger that fail JSON parsing.
func (r *TrustAuditRunner) countCorruptedLines() (int, error) {
	data, err := os.ReadFile(r.ledgerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	corrupted := 0
	for _, line := range splitLines(string(data)) {
		if line == "" {
			continue
		}
		var evt TrustEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			corrupted++
		}
	}
	return corrupted, nil
}

// ============================================================================
// Helpers
// ============================================================================

// groupEventsByAgent groups trust events by agent ID.
func groupEventsByAgent(events []TrustEvent) map[string][]TrustEvent {
	result := make(map[string][]TrustEvent)
	for _, evt := range events {
		result[evt.AgentID] = append(result[evt.AgentID], evt)
	}
	return result
}
