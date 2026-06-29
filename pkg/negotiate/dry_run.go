package negotiate

// dry_run.go implements the negotiation dry-run simulator per
// specs/pr-negotiation.md §2 (Dry-run mode) and §14 (Exit code 10).
//
// Dry-run mode previews the debate flow without posting to Forgejo or calling
// Chimera. It simulates all 3 debate rounds with stub agents, produces the
// same DebateEvent JSONL transcript format as a real negotiation, and returns
// a DryRunReport with the would-be resolution and exit code 10 (DRY_RUN).

import (
	"fmt"
	"time"
)

// ExitCodeDryRun is the spec §14 exit code for dry-run mode.
const ExitCodeDryRun = 10

// StubAgentConfig configures a simulated agent for dry-run mode.
type StubAgentConfig struct {
	Name         string  `json:"name"`
	TrustLevel   int     `json:"trust_level"`
	Verdict      Verdict `json:"verdict"`
	EvidenceBody string  `json:"evidence_body"` // pre-written debate body
}

// DryRunReport captures the result of a dry-run negotiation simulation.
type DryRunReport struct {
	ExitCode            int             `json:"exit_code"`
	PRNumber            int             `json:"pr_number"`
	AgentA              StubAgentConfig `json:"agent_a"`
	AgentB              StubAgentConfig `json:"agent_b"`
	RoundsSimulated     int             `json:"rounds_simulated"`
	ConflictDetected    bool            `json:"conflict_detected"`
	Deadlocked          bool            `json:"deadlocked"`
	WouldResolve        bool            `json:"would_resolve"`
	WouldEscalate       bool            `json:"would_escalate"`
	SimulatedResolution string          `json:"simulated_resolution,omitempty"`
	Events              []DebateEvent   `json:"events"`
	EstimatedCost       float64         `json:"estimated_cost"`
	Timestamp           time.Time       `json:"timestamp"`
}

// DryRunSimulator runs the negotiation protocol in simulation mode (spec §2).
type DryRunSimulator struct{}

// NewDryRunSimulator creates a new simulator.
func NewDryRunSimulator() *DryRunSimulator {
	return &DryRunSimulator{}
}

// Simulate runs the full negotiation protocol in dry-run mode.
// It does NOT call Forgejo or Chimera — it produces a simulated transcript.
func (s *DryRunSimulator) Simulate(agentA, agentB StubAgentConfig, prNumber int) *DryRunReport {
	now := time.Now()
	report := &DryRunReport{
		ExitCode:  ExitCodeDryRun,
		PRNumber:  prNumber,
		AgentA:    agentA,
		AgentB:    agentB,
		Timestamp: now,
	}

	// Check for conflict
	if !DetectConflict(agentA.Verdict, agentB.Verdict) {
		report.ConflictDetected = false
		report.WouldResolve = true
		report.SimulatedResolution = fmt.Sprintf("No conflict — both agents %s", agentA.Verdict)
		report.Events = append(report.Events, DebateEvent{
			Type:      "no_conflict",
			VerdictA:  string(agentA.Verdict),
			VerdictB:  string(agentB.Verdict),
			Timestamp: now,
		})
		return report
	}

	// Conflict detected
	report.ConflictDetected = true
	report.Events = append(report.Events, DebateEvent{
		Type:      "conflict_detected",
		VerdictA:  string(agentA.Verdict),
		VerdictB:  string(agentB.Verdict),
		Timestamp: now,
	})

	// Simulate 3 debate rounds
	for round := 1; round <= 3; round++ {
		// Agent A posts
		report.Events = append(report.Events, DebateEvent{
			Round:         round,
			Type:          "argument",
			Agent:         agentA.Name,
			Body:          agentA.EvidenceBody,
			EvidenceCount: 3,
			Timestamp:     now.Add(time.Duration(round) * time.Minute),
		})

		// Agent B posts
		report.Events = append(report.Events, DebateEvent{
			Round:         round,
			Type:          "argument",
			Agent:         agentB.Name,
			Body:          agentB.EvidenceBody,
			EvidenceCount: 2,
			Timestamp:     now.Add(time.Duration(round)*time.Minute + 30*time.Second),
		})

		report.RoundsSimulated = round
	}

	// After 3 rounds with no concession → deadlock
	report.Deadlocked = true
	report.Events = append(report.Events, DebateEvent{
		Type:      "deadlock",
		Timestamp: now.Add(4 * time.Minute),
	})

	// Simulate Chimera tie-break
	report.Events = append(report.Events, DebateEvent{
		Type:      "chimera_tiebreak",
		Verdict:   "APPROVE",
		Timestamp: now.Add(5 * time.Minute),
	})

	report.WouldResolve = true
	report.SimulatedResolution = "Chimera arbiter: APPROVE (simulated)"
	report.EstimatedCost = 0.004 // typical arbiter cost

	report.Events = append(report.Events, DebateEvent{
		Type:      "resolved",
		Outcome:   "APPROVED",
		Timestamp: now.Add(5*time.Minute + 10*time.Second),
	})

	return report
}

// SimulateConcession runs a dry-run where one agent concedes during the debate.
func (s *DryRunSimulator) SimulateConcession(concedingAgent string, agentA, agentB StubAgentConfig, prNumber int, concedeRound int) *DryRunReport {
	now := time.Now()
	report := &DryRunReport{
		ExitCode:  ExitCodeDryRun,
		PRNumber:  prNumber,
		AgentA:    agentA,
		AgentB:    agentB,
		Timestamp: now,
	}

	if !DetectConflict(agentA.Verdict, agentB.Verdict) {
		report.ConflictDetected = false
		report.WouldResolve = true
		report.SimulatedResolution = "No conflict"
		return report
	}

	report.ConflictDetected = true
	report.Events = append(report.Events, DebateEvent{
		Type:      "conflict_detected",
		VerdictA:  string(agentA.Verdict),
		VerdictB:  string(agentB.Verdict),
		Timestamp: now,
	})

	// Simulate rounds up to the concession round
	for round := 1; round <= concedeRound; round++ {
		if round < concedeRound {
			// Normal round — both agents post
			report.Events = append(report.Events, DebateEvent{
				Round:         round,
				Type:          "argument",
				Agent:         agentA.Name,
				Body:          agentA.EvidenceBody,
				EvidenceCount: 3,
				Timestamp:     now.Add(time.Duration(round) * time.Minute),
			})
			report.Events = append(report.Events, DebateEvent{
				Round:         round,
				Type:          "argument",
				Agent:         agentB.Name,
				Body:          agentB.EvidenceBody,
				EvidenceCount: 2,
				Timestamp:     now.Add(time.Duration(round)*time.Minute + 30*time.Second),
			})
		} else {
			// Concession round
			nonConcedingAgent := agentA
			if concedingAgent == agentA.Name {
				nonConcedingAgent = agentB
			}
			// Non-conceding agent posts normally
			report.Events = append(report.Events, DebateEvent{
				Round:         round,
				Type:          "argument",
				Agent:         nonConcedingAgent.Name,
				Body:          nonConcedingAgent.EvidenceBody,
				EvidenceCount: 3,
				Timestamp:     now.Add(time.Duration(round) * time.Minute),
			})
			// Conceding agent posts concession
			report.Events = append(report.Events, DebateEvent{
				Round:     round,
				Type:      "concession",
				Agent:     concedingAgent,
				Body:      "CONCEDE: evidence was compelling",
				Timestamp: now.Add(time.Duration(round)*time.Minute + 30*time.Second),
			})
		}
		report.RoundsSimulated = round
	}

	report.WouldResolve = true
	winner := agentB.Name
	if concedingAgent == agentB.Name {
		winner = agentA.Name
	}
	report.SimulatedResolution = fmt.Sprintf("%s conceded in round %d — %s's verdict prevails", concedingAgent, concedeRound, winner)

	report.Events = append(report.Events, DebateEvent{
		Type:      "resolved",
		Outcome:   "concession",
		Timestamp: now.Add(time.Duration(concedeRound)*time.Minute + 40*time.Second),
	})

	return report
}

// FormatDryRunReport renders the report as a human-readable string (spec §19 examples).
func FormatDryRunReport(report *DryRunReport) string {
	if report == nil {
		return "DRY_RUN: no report"
	}

	var sb []string
	sb = append(sb, fmt.Sprintf("DRY_RUN: would negotiate %d rounds", report.RoundsSimulated))

	if !report.ConflictDetected {
		sb = append(sb, "RESULT: No conflict. Both agents agree.")
	} else if report.Deadlocked {
		sb = append(sb, "RESULT: Deadlock after 3 rounds → Chimera tie-break (simulated)")
	} else if report.WouldResolve {
		sb = append(sb, fmt.Sprintf("RESULT: %s", report.SimulatedResolution))
	}

	if report.WouldEscalate {
		sb = append(sb, "ESCALATION: Would escalate to human review")
	}

	sb = append(sb, fmt.Sprintf("Events: %d simulated", len(report.Events)))
	sb = append(sb, fmt.Sprintf("Estimated cost: $%.4f", report.EstimatedCost))

	result := ""
	for i, line := range sb {
		if i > 0 {
			result += "\n"
		}
		result += line
	}
	return result
}
