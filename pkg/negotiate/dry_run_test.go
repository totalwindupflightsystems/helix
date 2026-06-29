package negotiate

import (
	"strings"
	"testing"
	"time"
)

func TestDryRunSimulator_NoConflict(t *testing.T) {
	sim := NewDryRunSimulator()
	agentA := StubAgentConfig{Name: "alice", TrustLevel: 85, Verdict: VerdictApproved, EvidenceBody: "tests pass"}
	agentB := StubAgentConfig{Name: "bob", TrustLevel: 72, Verdict: VerdictApproved, EvidenceBody: "agreed"}

	report := sim.Simulate(agentA, agentB, 42)

	if report.ExitCode != ExitCodeDryRun {
		t.Errorf("ExitCode = %d, want %d", report.ExitCode, ExitCodeDryRun)
	}
	if report.ConflictDetected {
		t.Error("should not detect conflict when both approve")
	}
	if !report.WouldResolve {
		t.Error("should resolve when no conflict")
	}
	if report.RoundsSimulated != 0 {
		t.Errorf("RoundsSimulated = %d, want 0 (no conflict, no rounds)", report.RoundsSimulated)
	}
}

func TestDryRunSimulator_Conflict(t *testing.T) {
	sim := NewDryRunSimulator()
	agentA := StubAgentConfig{Name: "alice", TrustLevel: 85, Verdict: VerdictApproved, EvidenceBody: "looks good"}
	agentB := StubAgentConfig{Name: "bob", TrustLevel: 72, Verdict: VerdictRequestChanges, EvidenceBody: "missing tests"}

	report := sim.Simulate(agentA, agentB, 43)

	if !report.ConflictDetected {
		t.Error("should detect conflict when verdicts differ")
	}
	if report.RoundsSimulated != 3 {
		t.Errorf("RoundsSimulated = %d, want 3", report.RoundsSimulated)
	}
	if !report.Deadlocked {
		t.Error("should be deadlocked after 3 rounds with no concession")
	}
	if !report.WouldResolve {
		t.Error("should resolve via simulated Chimera tie-break")
	}
}

func TestDryRunSimulator_Events(t *testing.T) {
	sim := NewDryRunSimulator()
	agentA := StubAgentConfig{Name: "alice", TrustLevel: 85, Verdict: VerdictApproved, EvidenceBody: "ok"}
	agentB := StubAgentConfig{Name: "bob", TrustLevel: 72, Verdict: VerdictRequestChanges, EvidenceBody: "no"}

	report := sim.Simulate(agentA, agentB, 44)

	// Expected events: conflict_detected + 6 arguments (3 rounds × 2 agents) + deadlock + chimera_tiebreak + resolved = 10
	if len(report.Events) != 10 {
		t.Errorf("Events count = %d, want 10", len(report.Events))
	}

	// Check event types
	types := make(map[string]int)
	for _, e := range report.Events {
		types[e.Type]++
	}
	if types["conflict_detected"] != 1 {
		t.Error("should have 1 conflict_detected event")
	}
	if types["argument"] != 6 {
		t.Error("should have 6 argument events (3 rounds × 2 agents)")
	}
	if types["deadlock"] != 1 {
		t.Error("should have 1 deadlock event")
	}
	if types["chimera_tiebreak"] != 1 {
		t.Error("should have 1 chimera_tiebreak event")
	}
	if types["resolved"] != 1 {
		t.Error("should have 1 resolved event")
	}
}

func TestDryRunSimulator_PRNumber(t *testing.T) {
	sim := NewDryRunSimulator()
	agentA := StubAgentConfig{Name: "a", TrustLevel: 50, Verdict: VerdictApproved}
	agentB := StubAgentConfig{Name: "b", TrustLevel: 50, Verdict: VerdictApproved}

	report := sim.Simulate(agentA, agentB, 99)
	if report.PRNumber != 99 {
		t.Errorf("PRNumber = %d, want 99", report.PRNumber)
	}
}

func TestDryRunSimulator_Timestamp(t *testing.T) {
	sim := NewDryRunSimulator()
	agentA := StubAgentConfig{Name: "a", TrustLevel: 50, Verdict: VerdictApproved}
	agentB := StubAgentConfig{Name: "b", TrustLevel: 50, Verdict: VerdictApproved}

	before := time.Now()
	report := sim.Simulate(agentA, agentB, 1)
	after := time.Now()

	if report.Timestamp.Before(before) || report.Timestamp.After(after) {
		t.Error("timestamp should be between before and after")
	}
}

func TestDryRunSimulator_EstimatedCost(t *testing.T) {
	sim := NewDryRunSimulator()
	agentA := StubAgentConfig{Name: "a", TrustLevel: 85, Verdict: VerdictApproved}
	agentB := StubAgentConfig{Name: "b", TrustLevel: 72, Verdict: VerdictRequestChanges}

	report := sim.Simulate(agentA, agentB, 1)
	if report.EstimatedCost <= 0 {
		t.Error("conflict resolution should have estimated cost > 0")
	}
}

func TestDryRunSimulator_Concession(t *testing.T) {
	sim := NewDryRunSimulator()
	agentA := StubAgentConfig{Name: "alice", TrustLevel: 85, Verdict: VerdictApproved, EvidenceBody: "approved"}
	agentB := StubAgentConfig{Name: "bob", TrustLevel: 72, Verdict: VerdictRequestChanges, EvidenceBody: "rejected"}

	report := sim.SimulateConcession("alice", agentA, agentB, 50, 2)

	if !report.WouldResolve {
		t.Error("should resolve via concession")
	}
	if report.Deadlocked {
		t.Error("should NOT deadlock when concession occurs")
	}
	if report.RoundsSimulated != 2 {
		t.Errorf("RoundsSimulated = %d, want 2 (concede in round 2)", report.RoundsSimulated)
	}
	if !strings.Contains(report.SimulatedResolution, "alice conceded") {
		t.Errorf("SimulatedResolution = %q, should mention alice conceded", report.SimulatedResolution)
	}
}

func TestDryRunSimulator_Concession_BobConcedes(t *testing.T) {
	sim := NewDryRunSimulator()
	agentA := StubAgentConfig{Name: "alice", TrustLevel: 85, Verdict: VerdictApproved, EvidenceBody: "approved"}
	agentB := StubAgentConfig{Name: "bob", TrustLevel: 72, Verdict: VerdictRequestChanges, EvidenceBody: "rejected"}

	report := sim.SimulateConcession("bob", agentA, agentB, 51, 1)

	if !strings.Contains(report.SimulatedResolution, "bob conceded") {
		t.Errorf("SimulatedResolution = %q, should mention bob conceded", report.SimulatedResolution)
	}
	if !strings.Contains(report.SimulatedResolution, "alice") {
		t.Error("should mention alice as winner")
	}
}

func TestDryRunSimulator_Concession_Round1(t *testing.T) {
	sim := NewDryRunSimulator()
	agentA := StubAgentConfig{Name: "a", TrustLevel: 50, Verdict: VerdictApproved, EvidenceBody: "x"}
	agentB := StubAgentConfig{Name: "b", TrustLevel: 50, Verdict: VerdictRequestChanges, EvidenceBody: "y"}

	report := sim.SimulateConcession("a", agentA, agentB, 1, 1)
	if report.RoundsSimulated != 1 {
		t.Errorf("RoundsSimulated = %d, want 1", report.RoundsSimulated)
	}
}

func TestDryRunSimulator_Concession_Round3(t *testing.T) {
	sim := NewDryRunSimulator()
	agentA := StubAgentConfig{Name: "a", TrustLevel: 50, Verdict: VerdictApproved, EvidenceBody: "x"}
	agentB := StubAgentConfig{Name: "b", TrustLevel: 50, Verdict: VerdictRequestChanges, EvidenceBody: "y"}

	report := sim.SimulateConcession("b", agentA, agentB, 1, 3)
	if report.RoundsSimulated != 3 {
		t.Errorf("RoundsSimulated = %d, want 3", report.RoundsSimulated)
	}
}

func TestDryRunSimulator_Concession_NoConflict(t *testing.T) {
	sim := NewDryRunSimulator()
	agentA := StubAgentConfig{Name: "a", TrustLevel: 50, Verdict: VerdictApproved, EvidenceBody: "x"}
	agentB := StubAgentConfig{Name: "b", TrustLevel: 50, Verdict: VerdictApproved, EvidenceBody: "y"}

	report := sim.SimulateConcession("a", agentA, agentB, 1, 2)
	if report.ConflictDetected {
		t.Error("should not detect conflict when both approve")
	}
	if !report.WouldResolve {
		t.Error("should resolve when no conflict")
	}
}

func TestDryRunSimulator_Concession_Events(t *testing.T) {
	sim := NewDryRunSimulator()
	agentA := StubAgentConfig{Name: "alice", TrustLevel: 85, Verdict: VerdictApproved, EvidenceBody: "ok"}
	agentB := StubAgentConfig{Name: "bob", TrustLevel: 72, Verdict: VerdictRequestChanges, EvidenceBody: "no"}

	report := sim.SimulateConcession("alice", agentA, agentB, 50, 2)

	// Round 1: both agents argue (2 events)
	// Round 2: bob argues + alice concedes (2 events)
	// conflict_detected (1) + resolved (1) = total 6
	if len(report.Events) != 6 {
		t.Errorf("Events = %d, want 6", len(report.Events))
	}

	// Check for concession event
	hasConcession := false
	for _, e := range report.Events {
		if e.Type == "concession" {
			hasConcession = true
			if e.Agent != "alice" {
				t.Errorf("concession agent = %s, want alice", e.Agent)
			}
		}
	}
	if !hasConcession {
		t.Error("events should contain a concession event")
	}
}

func TestFormatDryRunReport_Conflict(t *testing.T) {
	report := &DryRunReport{
		ExitCode:            ExitCodeDryRun,
		RoundsSimulated:     3,
		ConflictDetected:    true,
		Deadlocked:          true,
		WouldResolve:        true,
		SimulatedResolution: "Chimera arbiter: APPROVE",
		EstimatedCost:       0.004,
	}

	output := FormatDryRunReport(report)
	if !strings.Contains(output, "DRY_RUN") {
		t.Error("output should contain DRY_RUN")
	}
	if !strings.Contains(output, "3 rounds") {
		t.Error("output should mention 3 rounds")
	}
	if !strings.Contains(output, "Deadlock") {
		t.Error("output should mention deadlock")
	}
}

func TestFormatDryRunReport_NoConflict(t *testing.T) {
	report := &DryRunReport{
		ExitCode:         ExitCodeDryRun,
		RoundsSimulated:  0,
		ConflictDetected: false,
		WouldResolve:     true,
	}

	output := FormatDryRunReport(report)
	if !strings.Contains(output, "No conflict") {
		t.Error("output should mention no conflict")
	}
}

func TestFormatDryRunReport_Nil(t *testing.T) {
	output := FormatDryRunReport(nil)
	if !strings.Contains(output, "no report") {
		t.Error("nil report should say no report")
	}
}

func TestFormatDryRunReport_Escalation(t *testing.T) {
	report := &DryRunReport{
		ExitCode:         ExitCodeDryRun,
		RoundsSimulated:  3,
		WouldEscalate:    true,
		ConflictDetected: true,
	}

	output := FormatDryRunReport(report)
	if !strings.Contains(output, "ESCALATION") {
		t.Error("output should mention escalation")
	}
}

func TestFormatDryRunReport_Cost(t *testing.T) {
	report := &DryRunReport{
		ExitCode:      ExitCodeDryRun,
		EstimatedCost: 0.0042,
	}

	output := FormatDryRunReport(report)
	if !strings.Contains(output, "0.0042") {
		t.Error("output should show estimated cost")
	}
}

func TestExitCodeDryRun_Value(t *testing.T) {
	if ExitCodeDryRun != 10 {
		t.Errorf("ExitCodeDryRun = %d, want 10 (spec §14)", ExitCodeDryRun)
	}
}

func TestDryRunSimulator_StubAgentConfig(t *testing.T) {
	agent := StubAgentConfig{
		Name:         "test-agent",
		TrustLevel:   75,
		Verdict:      VerdictApproved,
		EvidenceBody: "evidence here",
	}
	if agent.Name != "test-agent" {
		t.Error("name mismatch")
	}
	if agent.TrustLevel != 75 {
		t.Error("trust level mismatch")
	}
	if agent.Verdict != VerdictApproved {
		t.Error("verdict mismatch")
	}
}

func TestDryRunSimulator_FullConflictFlow(t *testing.T) {
	sim := NewDryRunSimulator()
	agentA := StubAgentConfig{
		Name:         "reviewer-alpha",
		TrustLevel:   85,
		Verdict:      VerdictApproved,
		EvidenceBody: "Spec §3.2 requires JWT auth. Tests pass.",
	}
	agentB := StubAgentConfig{
		Name:         "reviewer-beta",
		TrustLevel:   78,
		Verdict:      VerdictRequestChanges,
		EvidenceBody: "Missing rate limiting per spec §8.3.",
	}

	report := sim.Simulate(agentA, agentB, 100)

	// Full conflict flow: conflict → 3 rounds → deadlock → chimera → resolved
	if !report.ConflictDetected {
		t.Error("should detect conflict")
	}
	if report.RoundsSimulated != 3 {
		t.Errorf("RoundsSimulated = %d, want 3", report.RoundsSimulated)
	}
	if !report.Deadlocked {
		t.Error("should deadlock after 3 rounds")
	}
	if !report.WouldResolve {
		t.Error("should resolve via Chimera")
	}

	// Verify events form the expected lifecycle
	lifecycleTypes := []string{
		"conflict_detected",
		"argument", // round 1 A
		"argument", // round 1 B
		"argument", // round 2 A
		"argument", // round 2 B
		"argument", // round 3 A
		"argument", // round 3 B
		"deadlock",
		"chimera_tiebreak",
		"resolved",
	}

	if len(report.Events) != len(lifecycleTypes) {
		t.Fatalf("events count = %d, want %d", len(report.Events), len(lifecycleTypes))
	}

	for i, wantType := range lifecycleTypes {
		if report.Events[i].Type != wantType {
			t.Errorf("event[%d].Type = %s, want %s", i, report.Events[i].Type, wantType)
		}
	}

	// Verify round numbers
	for _, round := range []int{1, 2, 3} {
		argCount := 0
		for _, e := range report.Events {
			if e.Type == "argument" && e.Round == round {
				argCount++
			}
		}
		if argCount != 2 {
			t.Errorf("round %d argument count = %d, want 2", round, argCount)
		}
	}
}
