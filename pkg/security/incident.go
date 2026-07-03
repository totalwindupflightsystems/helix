// Package security — incident response engine per spec §6.7.
//
// Encodes the 4-level severity classification (SEV-0 through SEV-3) with
// structured response procedures. Each severity has a ordered list of
// ResponseSteps with action, verification, and expected outcome.
package security

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// =============================================================================
// Severity Levels
// =============================================================================

// Severity classifies incident urgency per spec §6.7.
type Severity string

const (
	SeveritySEV0 Severity = "SEV-0" // Platform-wide outage or data breach
	SeveritySEV1 Severity = "SEV-1" // Agent causing active harm
	SeveritySEV2 Severity = "SEV-2" // Degraded capability
	SeveritySEV3 Severity = "SEV-3" // Non-critical issue
)

// SeverityInfo describes a severity level.
type SeverityInfo struct {
	Level        Severity
	Definition   string
	ResponseTime string // Expected response time
	Example      string
}

// AllSeverities returns all 4 severity levels in order (SEV-0 first).
func AllSeverities() []SeverityInfo {
	return []SeverityInfo{
		{
			Level:        SeveritySEV0,
			Definition:   "Platform-wide outage or data breach",
			ResponseTime: "Immediate",
			Example:      "Forgejo down, management key compromised",
		},
		{
			Level:        SeveritySEV1,
			Definition:   "Agent causing active harm",
			ResponseTime: "< 15 min",
			Example:      "Agent pushing secrets, agent in infinite spending loop",
		},
		{
			Level:        SeveritySEV2,
			Definition:   "Degraded capability",
			ResponseTime: "< 1 hour",
			Example:      "Chimera circuit breaker stuck open, GitReins Tier 2 down",
		},
		{
			Level:        SeveritySEV3,
			Definition:   "Non-critical issue",
			ResponseTime: "< 1 day",
			Example:      "Trust level not updating, PromptFoo CI flaky",
		},
	}
}

// SeverityOrder returns a numeric ordering for severity (0 = most severe).
func SeverityOrder(s Severity) int {
	switch s {
	case SeveritySEV0:
		return 0
	case SeveritySEV1:
		return 1
	case SeveritySEV2:
		return 2
	case SeveritySEV3:
		return 3
	default:
		return 99
	}
}

// =============================================================================
// Response Steps
// =============================================================================

// ResponseStep is a single action in an incident response procedure.
type ResponseStep struct {
	Order           int    // Step number (1-based)
	Action          string // What to do
	Command         string // Shell command to execute (if applicable)
	Verification    string // How to verify the step was completed
	ExpectedOutcome string // What should happen after this step
}

// ResponseProcedure is the full response procedure for a severity level.
type ResponseProcedure struct {
	Severity Severity
	Trigger  string         // What triggers this response
	Steps    []ResponseStep // Ordered response steps
}

// =============================================================================
// Incident Record
// =============================================================================

// IncidentStatus tracks the lifecycle of an incident.
type IncidentStatus string

const (
	IncidentOpen       IncidentStatus = "open"
	IncidentInProgress IncidentStatus = "in_progress"
	IncidentResolved   IncidentStatus = "resolved"
	IncidentEscalated  IncidentStatus = "escalated"
)

// IncidentRecord tracks a single active incident.
type IncidentRecord struct {
	ID             string         // Unique incident ID
	Severity       Severity       // Incident severity level
	Title          string         // Short description
	Description    string         // Detailed description
	Status         IncidentStatus // Current status
	CreatedAt      time.Time      // When the incident was detected
	ResolvedAt     time.Time      // When the incident was resolved (zero = ongoing)
	AgentID        string         // Agent involved (if applicable)
	StepsCompleted []int          // Which response steps have been completed
}

// Duration returns how long the incident was active.
func (r *IncidentRecord) Duration() time.Duration {
	if r.ResolvedAt.IsZero() {
		return time.Since(r.CreatedAt)
	}
	return r.ResolvedAt.Sub(r.CreatedAt)
}

// IsResolved returns true if the incident has been resolved.
func (r *IncidentRecord) IsResolved() bool {
	return r.Status == IncidentResolved
}

// =============================================================================
// Incident Response Engine
// =============================================================================

// IncidentResponseEngine manages incident classification and response procedures.
type IncidentResponseEngine struct {
	procedures map[Severity]*ResponseProcedure
	incidents  []*IncidentRecord
}

// NewIncidentResponseEngine creates an engine with default procedures from spec §6.7.
func NewIncidentResponseEngine() *IncidentResponseEngine {
	e := &IncidentResponseEngine{
		procedures: make(map[Severity]*ResponseProcedure),
	}
	for _, proc := range DefaultProcedures() {
		e.procedures[proc.Severity] = &proc
	}
	return e
}

// GetProcedure returns the response procedure for a severity level.
func (e *IncidentResponseEngine) GetProcedure(s Severity) (*ResponseProcedure, bool) {
	proc, ok := e.procedures[s]
	return proc, ok
}

// RegisterIncident records a new incident.
func (e *IncidentResponseEngine) RegisterIncident(record IncidentRecord) {
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	if record.Status == "" {
		record.Status = IncidentOpen
	}
	e.incidents = append(e.incidents, &record)
}

// ActiveIncidents returns all incidents that are not resolved.
func (e *IncidentResponseEngine) ActiveIncidents() []*IncidentRecord {
	var active []*IncidentRecord
	for _, inc := range e.incidents {
		if !inc.IsResolved() {
			active = append(active, inc)
		}
	}
	return active
}

// IncidentCount returns the total number of registered incidents.
func (e *IncidentResponseEngine) IncidentCount() int {
	return len(e.incidents)
}

// IncidentsBySeverity returns incidents grouped by severity.
func (e *IncidentResponseEngine) IncidentsBySeverity() map[Severity][]*IncidentRecord {
	result := make(map[Severity][]*IncidentRecord)
	for _, inc := range e.incidents {
		result[inc.Severity] = append(result[inc.Severity], inc)
	}
	return result
}

// ResolveIncident marks an incident as resolved.
func (e *IncidentResponseEngine) ResolveIncident(id string) bool {
	for _, inc := range e.incidents {
		if inc.ID == id {
			inc.Status = IncidentResolved
			inc.ResolvedAt = time.Now().UTC()
			return true
		}
	}
	return false
}

// EscalateIncident raises an incident's severity by one level.
func (e *IncidentResponseEngine) EscalateIncident(id string) bool {
	for _, inc := range e.incidents {
		if inc.ID == id {
			switch inc.Severity {
			case SeveritySEV3:
				inc.Severity = SeveritySEV2
			case SeveritySEV2:
				inc.Severity = SeveritySEV1
			case SeveritySEV1:
				inc.Severity = SeveritySEV0
			}
			inc.Status = IncidentEscalated
			return true
		}
	}
	return false
}

// CompleteStep marks a response step as completed for an incident.
func (e *IncidentResponseEngine) CompleteStep(id string, stepOrder int) bool {
	for _, inc := range e.incidents {
		if inc.ID == id {
			inc.StepsCompleted = append(inc.StepsCompleted, stepOrder)
			return true
		}
	}
	return false
}

// FormatIncident renders an incident record as a human-readable string.
func FormatIncident(inc *IncidentRecord) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Incident %s — %s [%s]\n", inc.ID, inc.Severity, inc.Status)
	fmt.Fprintf(&b, "  Title: %s\n", inc.Title)
	if inc.AgentID != "" {
		fmt.Fprintf(&b, "  Agent: %s\n", inc.AgentID)
	}
	fmt.Fprintf(&b, "  Created: %s\n", inc.CreatedAt.Format(time.RFC3339))
	if inc.IsResolved() {
		fmt.Fprintf(&b, "  Resolved: %s\n", inc.ResolvedAt.Format(time.RFC3339))
		fmt.Fprintf(&b, "  Duration: %s\n", inc.Duration().Round(time.Second))
	}
	if len(inc.StepsCompleted) > 0 {
		fmt.Fprintf(&b, "  Steps completed: %v\n", inc.StepsCompleted)
	}
	return b.String()
}

// FormatProcedure renders a response procedure as a human-readable string.
func FormatProcedure(proc *ResponseProcedure) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s Response Procedure\n", proc.Severity)
	fmt.Fprintf(&b, "Trigger: %s\n", proc.Trigger)
	fmt.Fprintf(&b, "Steps:\n")
	for _, step := range proc.Steps {
		fmt.Fprintf(&b, "  %d. %s\n", step.Order, step.Action)
		if step.Command != "" {
			fmt.Fprintf(&b, "     Command: %s\n", step.Command)
		}
		if step.Verification != "" {
			fmt.Fprintf(&b, "     Verify: %s\n", step.Verification)
		}
		if step.ExpectedOutcome != "" {
			fmt.Fprintf(&b, "     Expected: %s\n", step.ExpectedOutcome)
		}
	}
	return b.String()
}

// =============================================================================
// Default Procedures from Spec §6.7
// =============================================================================

// DefaultProcedures returns the response procedures defined in spec §6.7.
func DefaultProcedures() []ResponseProcedure {
	return []ResponseProcedure{
		{
			Severity: SeveritySEV0,
			Trigger:  "Platform-wide outage or data breach (Forgejo down, management key compromised)",
			Steps: []ResponseStep{
				{
					Order:           1,
					Action:          "Kill all agent containers",
					Command:         "docker ps | grep hermes-agent | awk '{print $1}' | xargs docker kill",
					Verification:    "docker ps | grep hermes-agent returns empty",
					ExpectedOutcome: "All agent containers stopped",
				},
				{
					Order:           2,
					Action:          "Rotate management key",
					Command:         "Create new key at OpenRouter, update OR_MANAGEMENT_KEY",
					Verification:    "New key returns 200 from OpenRouter API",
					ExpectedOutcome: "Management key rotated, old key revoked",
				},
				{
					Order:           3,
					Action:          "Revoke all agent OpenRouter keys",
					Command:         "DELETE /api/v1/keys/<id> for each key",
					Verification:    "All agent keys return 401",
					ExpectedOutcome: "All agent API access revoked",
				},
				{
					Order:           4,
					Action:          "Rotate Forgejo admin password",
					Command:         "Generate new 32+ char password, update in password manager",
					Verification:    "New password works, old password rejected",
					ExpectedOutcome: "Admin access secured",
				},
				{
					Order:           5,
					Action:          "Audit all recent commits for injected secrets or malicious code",
					Command:         "git log --since='24 hours ago' --all --patch | grep -i 'secret\\|key\\|token'",
					Verification:    "No secrets found in recent commits",
					ExpectedOutcome: "Audit complete, any injected secrets identified",
				},
				{
					Order:           6,
					Action:          "Re-provision agents from known-good state",
					Command:         "Rebuild agent containers from clean images",
					Verification:    "All agents running with fresh keys",
					ExpectedOutcome: "Platform restored to safe state",
				},
			},
		},
		{
			Severity: SeveritySEV1,
			Trigger:  "Agent causing active harm (pushing secrets, infinite spending loop)",
			Steps: []ResponseStep{
				{
					Order:           1,
					Action:          "Kill the specific agent container",
					Command:         "docker kill hermes-agent-<name>",
					Verification:    "docker ps shows agent container is stopped",
					ExpectedOutcome: "Agent process terminated",
				},
				{
					Order:           2,
					Action:          "Revoke that agent's OpenRouter key",
					Command:         "DELETE /api/v1/keys/<agent-key-id>",
					Verification:    "Agent key returns 401 from OpenRouter",
					ExpectedOutcome: "Agent API access revoked",
				},
				{
					Order:           3,
					Action:          "Revert any unmerged PRs by the agent",
					Command:         "git push origin --delete <branch> for each agent branch",
					Verification:    "No open PRs from the agent",
					ExpectedOutcome: "Agent's unreviewed code removed",
				},
				{
					Order:           4,
					Action:          "Audit the agent's LangFuse traces for anomalous behavior",
					Command:         "Query LangFuse for agent's session traces",
					Verification:    "Anomalous patterns identified and documented",
					ExpectedOutcome: "Root cause analysis complete",
				},
				{
					Order:           5,
					Action:          "Review the prompt that triggered the behavior",
					Command:         "Examine agent's last prompt for injection indicators",
					Verification:    "Prompt source identified (injection vs model error)",
					ExpectedOutcome: "Prompt injection or model degradation identified",
				},
			},
		},
		{
			Severity: SeveritySEV2,
			Trigger:  "Degraded capability (Chimera circuit breaker stuck open, GitReins Tier 2 down)",
			Steps: []ResponseStep{
				{
					Order:           1,
					Action:          "Identify the degraded component",
					Command:         "Check health endpoints for all services",
					Verification:    "Degraded component identified",
					ExpectedOutcome: "Root cause isolated",
				},
				{
					Order:           2,
					Action:          "Restart or repair the degraded component",
					Command:         "docker compose restart <component>",
					Verification:    "Component health check passes",
					ExpectedOutcome: "Service restored",
				},
				{
					Order:           3,
					Action:          "Verify platform operations are restored",
					Command:         "Run health check suite",
					Verification:    "All health checks pass",
					ExpectedOutcome: "Normal operations resumed",
				},
			},
		},
		{
			Severity: SeveritySEV3,
			Trigger:  "Non-critical issue (trust level not updating, PromptFoo CI flaky)",
			Steps: []ResponseStep{
				{
					Order:           1,
					Action:          "Document the issue with logs and timestamps",
					Command:         "Collect relevant logs from affected component",
					Verification:    "Issue documented in incident tracker",
					ExpectedOutcome: "Issue recorded for investigation",
				},
				{
					Order:           2,
					Action:          "Schedule investigation during next maintenance window",
					Command:         "Create work item for investigation",
					Verification:    "Work item created and assigned",
					ExpectedOutcome: "Investigation scheduled",
				},
			},
		},
	}
}

// =============================================================================
// Escalation from Alert Engine
// =============================================================================

// AlertSignal is a minimal alert signal from the health/alerts package.
// We define it here to avoid circular imports — the real AlertResult in
// pkg/health can be converted to this type.
type AlertSignal struct {
	AlertName string
	Severity  string // "critical", "warning"
	Labels    map[string]string
	Message   string
}

// ClassifyFromAlert maps an alert signal to an incident severity.
// Returns SEV-0 for platform compromise/breach signals,
// SEV-1 for agent-related critical alerts,
// SEV-2 for other critical alerts (degraded service),
// SEV-3 for warnings.
func ClassifyFromAlert(alert AlertSignal) Severity {
	switch alert.Severity {
	case "critical":
		// Platform-level compromise → SEV-0
		if strings.Contains(alert.Message, "platform") ||
			strings.Contains(alert.Message, "compromise") ||
			strings.Contains(alert.Message, "breach") {
			return SeveritySEV0
		}
		// Agent-related critical → SEV-1
		if strings.Contains(alert.AlertName, "Agent") ||
			strings.Contains(alert.Message, "agent") {
			return SeveritySEV1
		}
		// Other critical → SEV-2
		return SeveritySEV2
	case "warning":
		return SeveritySEV3
	default:
		return SeveritySEV3
	}
}

// CreateFromAlert creates an IncidentRecord from an alert signal.
func (e *IncidentResponseEngine) CreateFromAlert(alert AlertSignal) *IncidentRecord {
	severity := ClassifyFromAlert(alert)
	agentID := ""
	if alert.Labels != nil {
		agentID = alert.Labels["agent"]
	}
	record := &IncidentRecord{
		ID:          fmt.Sprintf("INC-%d-%s", time.Now().Unix(), alert.AlertName),
		Severity:    severity,
		Title:       alert.AlertName,
		Description: alert.Message,
		Status:      IncidentOpen,
		CreatedAt:   time.Now().UTC(),
		AgentID:     agentID,
	}
	e.incidents = append(e.incidents, record)
	return record
}

// =============================================================================
// Incident Statistics
// =============================================================================

// IncidentStats provides aggregate statistics over all incidents.
type IncidentStats struct {
	Total           int
	BySeverity      map[Severity]int
	ByStatus        map[IncidentStatus]int
	Resolved        int
	Active          int
	MeanResolveTime time.Duration
}

// ComputeStats aggregates statistics over all incidents.
func (e *IncidentResponseEngine) ComputeStats() IncidentStats {
	stats := IncidentStats{
		Total:      len(e.incidents),
		BySeverity: make(map[Severity]int),
		ByStatus:   make(map[IncidentStatus]int),
	}
	var totalResolveTime time.Duration
	for _, inc := range e.incidents {
		stats.BySeverity[inc.Severity]++
		stats.ByStatus[inc.Status]++
		if inc.IsResolved() {
			stats.Resolved++
			totalResolveTime += inc.Duration()
		} else {
			stats.Active++
		}
	}
	if stats.Resolved > 0 {
		stats.MeanResolveTime = totalResolveTime / time.Duration(stats.Resolved)
	}
	return stats
}

// FormatStats renders incident statistics as a human-readable string.
func FormatStats(stats IncidentStats) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Incident Statistics\n")
	fmt.Fprintf(&b, "  Total: %d (active: %d, resolved: %d)\n", stats.Total, stats.Active, stats.Resolved)
	if stats.Resolved > 0 {
		fmt.Fprintf(&b, "  Mean resolve time: %s\n", stats.MeanResolveTime.Round(time.Second))
	}
	fmt.Fprintf(&b, "  By severity:\n")
	sevs := []Severity{SeveritySEV0, SeveritySEV1, SeveritySEV2, SeveritySEV3}
	for _, s := range sevs {
		if count, ok := stats.BySeverity[s]; ok && count > 0 {
			fmt.Fprintf(&b, "    %s: %d\n", s, count)
		}
	}
	fmt.Fprintf(&b, "  By status:\n")
	statuses := []IncidentStatus{IncidentOpen, IncidentInProgress, IncidentEscalated, IncidentResolved}
	for _, st := range statuses {
		if count, ok := stats.ByStatus[st]; ok && count > 0 {
			fmt.Fprintf(&b, "    %s: %d\n", st, count)
		}
	}
	return b.String()
}

// SortedIncidents returns incidents sorted by severity (most severe first),
// then by creation time (newest first).
func (e *IncidentResponseEngine) SortedIncidents() []*IncidentRecord {
	sorted := make([]*IncidentRecord, len(e.incidents))
	copy(sorted, e.incidents)
	sort.SliceStable(sorted, func(i, j int) bool {
		si := SeverityOrder(sorted[i].Severity)
		sj := SeverityOrder(sorted[j].Severity)
		if si != sj {
			return si < sj
		}
		return sorted[i].CreatedAt.After(sorted[j].CreatedAt)
	})
	return sorted
}
