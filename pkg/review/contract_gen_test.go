package review

import (
	"strings"
	"testing"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/verify"
)

func TestNewContractGenerator(t *testing.T) {
	g := NewContractGenerator()
	if g.ConfidenceWeight != 1.0 {
		t.Errorf("ConfidenceWeight = %f, want 1.0", g.ConfidenceWeight)
	}
}

func TestContractGenerator_GenerateFromFindings_Nil(t *testing.T) {
	g := NewContractGenerator()
	contract := g.GenerateFromFindings(nil)
	if contract != nil {
		t.Error("expected nil for nil bundle")
	}
}

func TestContractGenerator_GenerateFromFindings_EmptyBundle(t *testing.T) {
	g := NewContractGenerator()
	bundle := &EvidenceBundle{
		ReviewID: "rev-001",
		PRURL:    "https://forgejo.example.com/org/repo/pulls/42",
	}
	contract := g.GenerateFromFindings(bundle)
	if contract == nil {
		t.Fatal("expected non-nil contract")
	}
	if len(contract.Contract.Assertions) != 1 {
		t.Fatalf("expected 1 baseline assertion, got %d", len(contract.Contract.Assertions))
	}
	if contract.Contract.Assertions[0].Metric != "success_rate" {
		t.Errorf("baseline metric = %q, want success_rate", contract.Contract.Assertions[0].Metric)
	}
	if contract.Contract.BreachAction != breachActionAlert {
		t.Errorf("breach action = %q, want alert", contract.Contract.BreachAction)
	}
}

func TestContractGenerator_GenerateFromFindings_SecurityCritical(t *testing.T) {
	g := NewContractGenerator()
	bundle := &EvidenceBundle{
		ReviewID: "rev-002",
		PRURL:    "https://forgejo.example.com/org/repo/pulls/42",
		Findings: []Finding{
			{
				Severity:    severityCritical,
				Type:        typeSecurity,
				File:        "auth.go",
				Line:        10,
				Description: "SQL injection vulnerability",
			},
		},
		Consensus: Consensus{Resolution: ResolutionApproved},
	}
	contract := g.GenerateFromFindings(bundle)
	if contract == nil {
		t.Fatal("expected non-nil contract")
	}
	if len(contract.Contract.Assertions) != 2 {
		t.Fatalf("expected 2 assertions for critical security finding, got %d", len(contract.Contract.Assertions))
	}
	// First: error_count eq 0
	if contract.Contract.Assertions[0].Metric != "error_count" || contract.Contract.Assertions[0].Op != "eq" {
		t.Errorf("assertion[0] = %+v", contract.Contract.Assertions[0])
	}
	// Second: success_rate gte 99.9
	if contract.Contract.Assertions[1].Metric != "success_rate" || contract.Contract.Assertions[1].Value != 99.9 {
		t.Errorf("assertion[1] = %+v", contract.Contract.Assertions[1])
	}
	// Approved resolution → rollback action
	if contract.Contract.BreachAction != breachActionRollback {
		t.Errorf("breach action = %q, want rollback", contract.Contract.BreachAction)
	}
}

func TestContractGenerator_GenerateFromFindings_PerformanceHigh(t *testing.T) {
	g := NewContractGenerator()
	bundle := &EvidenceBundle{
		ReviewID: "rev-003",
		PRURL:    "https://forgejo.example.com/org/repo/pulls/42",
		Findings: []Finding{
			{
				Severity: severityHigh,
				Type:     typePerformance,
				File:     "handler.go",
				Line:     50,
			},
		},
		Consensus: Consensus{Resolution: ResolutionApproved},
	}
	contract := g.GenerateFromFindings(bundle)
	if len(contract.Contract.Assertions) != 1 {
		t.Fatalf("expected 1 assertion, got %d", len(contract.Contract.Assertions))
	}
	a := contract.Contract.Assertions[0]
	if a.Metric != "latency_p99" || a.Op != "lte" {
		t.Errorf("assertion = %+v, want latency_p99 lte", a)
	}
	if a.Value != 500.0 {
		t.Errorf("threshold = %f, want 500.0 (high severity)", a.Value)
	}
	if a.Window != "5m" {
		t.Errorf("window = %q, want 5m", a.Window)
	}
}

func TestContractGenerator_GenerateFromFindings_PerformanceCritical(t *testing.T) {
	g := NewContractGenerator()
	bundle := &EvidenceBundle{
		ReviewID: "rev-004",
		Findings: []Finding{
			{Severity: severityCritical, Type: typePerformance},
		},
		Consensus: Consensus{Resolution: ResolutionApproved},
	}
	contract := g.GenerateFromFindings(bundle)
	if contract.Contract.Assertions[0].Value != 100.0 {
		t.Errorf("critical perf threshold = %f, want 100.0", contract.Contract.Assertions[0].Value)
	}
}

func TestContractGenerator_GenerateFromFindings_LogicHigh(t *testing.T) {
	g := NewContractGenerator()
	bundle := &EvidenceBundle{
		ReviewID: "rev-005",
		Findings: []Finding{
			{Severity: severityHigh, Type: typeLogic},
		},
		Consensus: Consensus{Resolution: ResolutionApproved},
	}
	contract := g.GenerateFromFindings(bundle)
	if len(contract.Contract.Assertions) != 1 {
		t.Fatalf("expected 1 assertion, got %d", len(contract.Contract.Assertions))
	}
	a := contract.Contract.Assertions[0]
	if a.Metric != "success_rate" || a.Value != 99.5 {
		t.Errorf("assertion = %+v, want success_rate gte 99.5", a)
	}
}

func TestContractGenerator_GenerateFromFindings_LogicCritical(t *testing.T) {
	g := NewContractGenerator()
	bundle := &EvidenceBundle{
		ReviewID: "rev-006",
		Findings: []Finding{
			{Severity: severityCritical, Type: typeLogic},
		},
		Consensus: Consensus{Resolution: ResolutionApproved},
	}
	contract := g.GenerateFromFindings(bundle)
	if contract.Contract.Assertions[0].Value != 99.9 {
		t.Errorf("critical logic threshold = %f, want 99.9", contract.Contract.Assertions[0].Value)
	}
}

func TestContractGenerator_GenerateFromFindings_RaceCondition(t *testing.T) {
	g := NewContractGenerator()
	bundle := &EvidenceBundle{
		ReviewID: "rev-007",
		Findings: []Finding{
			{Severity: severityHigh, Type: typeRace},
		},
		Consensus: Consensus{Resolution: ResolutionApproved},
	}
	contract := g.GenerateFromFindings(bundle)
	if len(contract.Contract.Assertions) != 2 {
		t.Fatalf("expected 2 assertions for race condition, got %d", len(contract.Contract.Assertions))
	}
	// error_count lte 0
	if contract.Contract.Assertions[0].Metric != "error_count" {
		t.Errorf("assertion[0] = %+v", contract.Contract.Assertions[0])
	}
	// latency_p99 lte 200
	if contract.Contract.Assertions[1].Metric != "latency_p99" || contract.Contract.Assertions[1].Value != 200.0 {
		t.Errorf("assertion[1] = %+v", contract.Contract.Assertions[1])
	}
}

func TestContractGenerator_GenerateFromFindings_SpecViolation(t *testing.T) {
	g := NewContractGenerator()
	bundle := &EvidenceBundle{
		ReviewID: "rev-008",
		Findings: []Finding{
			{Severity: severityHigh, Type: typeAPISurface},
		},
		Consensus: Consensus{Resolution: ResolutionApproved},
	}
	contract := g.GenerateFromFindings(bundle)
	if len(contract.Contract.Assertions) != 1 {
		t.Fatalf("expected 1 assertion, got %d", len(contract.Contract.Assertions))
	}
	if contract.Contract.Assertions[0].Value != 99.5 {
		t.Errorf("spec violation threshold = %f, want 99.5", contract.Contract.Assertions[0].Value)
	}
}

func TestContractGenerator_GenerateFromFindings_LowSeverity_Skipped(t *testing.T) {
	g := NewContractGenerator()
	bundle := &EvidenceBundle{
		ReviewID: "rev-009",
		Findings: []Finding{
			{Severity: severityLow, Type: typeSecurity},
			{Severity: severityMedium, Type: typePerformance},
		},
		Consensus: Consensus{Resolution: ResolutionApproved},
	}
	contract := g.GenerateFromFindings(bundle)
	// Low/medium findings produce no assertions → baseline assertion added
	if len(contract.Contract.Assertions) != 1 {
		t.Fatalf("expected 1 baseline assertion, got %d", len(contract.Contract.Assertions))
	}
	if contract.Contract.Assertions[0].Metric != "success_rate" {
		t.Errorf("baseline metric = %q", contract.Contract.Assertions[0].Metric)
	}
}

func TestContractGenerator_GenerateFromFindings_MultipleFindings(t *testing.T) {
	g := NewContractGenerator()
	bundle := &EvidenceBundle{
		ReviewID: "rev-010",
		Findings: []Finding{
			{Severity: severityCritical, Type: typeSecurity},
			{Severity: severityHigh, Type: typePerformance},
			{Severity: severityHigh, Type: typeLogic},
			{Severity: severityLow, Type: typeLogic}, // skipped
		},
		Consensus: Consensus{Resolution: ResolutionApproved},
	}
	contract := g.GenerateFromFindings(bundle)
	// security critical = 2 assertions, perf high = 1, logic high = 1, low = 0
	if len(contract.Contract.Assertions) != 4 {
		t.Fatalf("expected 4 assertions, got %d", len(contract.Contract.Assertions))
	}
}

func TestContractGenerator_GenerateFromFindings_BlockedResolution(t *testing.T) {
	g := NewContractGenerator()
	bundle := &EvidenceBundle{
		ReviewID: "rev-011",
		Findings: []Finding{
			{Severity: severityHigh, Type: typeLogic},
		},
		Consensus: Consensus{Resolution: ResolutionBlocked},
	}
	contract := g.GenerateFromFindings(bundle)
	if contract.Contract.BreachAction != breachActionBlock {
		t.Errorf("breach action = %q, want block", contract.Contract.BreachAction)
	}
}

func TestContractGenerator_GenerateFromFindings_TieBreakResolution(t *testing.T) {
	g := NewContractGenerator()
	bundle := &EvidenceBundle{
		ReviewID: "rev-012",
		Findings: []Finding{
			{Severity: severityHigh, Type: typeLogic},
		},
		Consensus: Consensus{Resolution: ResolutionTieBreak},
	}
	contract := g.GenerateFromFindings(bundle)
	if contract.Contract.BreachAction != breachActionRollback {
		t.Errorf("breach action = %q, want rollback", contract.Contract.BreachAction)
	}
}

func TestContractGenerator_GenerateForAgent(t *testing.T) {
	g := NewContractGenerator()
	bundle := &EvidenceBundle{
		ReviewID: "rev-013",
		Findings: []Finding{
			{Severity: severityCritical, Type: typeSecurity},
		},
		Consensus: Consensus{Resolution: ResolutionApproved},
	}
	contract := g.GenerateForAgent(bundle, "agent-007", "abc123")
	if contract.Contract.Agent != "agent-007" {
		t.Errorf("Agent = %q, want agent-007", contract.Contract.Agent)
	}
	if contract.Contract.MergeCommit != "abc123" {
		t.Errorf("MergeCommit = %q, want abc123", contract.Contract.MergeCommit)
	}
}

func TestContractGenerator_GenerateForAgent_NilBundle(t *testing.T) {
	g := NewContractGenerator()
	contract := g.GenerateForAgent(nil, "agent", "commit")
	if contract != nil {
		t.Error("expected nil for nil bundle")
	}
}

func TestContractGenerator_ConfidenceWeight(t *testing.T) {
	g := NewContractGenerator()
	g.ConfidenceWeight = 0.9

	bundle := &EvidenceBundle{
		ReviewID: "rev-014",
		Findings: []Finding{
			{Severity: severityHigh, Type: typeLogic},
		},
		Consensus: Consensus{Resolution: ResolutionApproved},
	}
	contract := g.GenerateFromFindings(bundle)
	// 99.5 * 0.9 = 89.55
	if contract.Contract.Assertions[0].Value != 89.55 {
		t.Errorf("weighted threshold = %f, want 89.55", contract.Contract.Assertions[0].Value)
	}
}

func TestContractGenerator_ConfidenceWeight_DefaultUnchanged(t *testing.T) {
	g := NewContractGenerator()
	// Default confidence = 1.0 → no adjustment
	bundle := &EvidenceBundle{
		ReviewID: "rev-015",
		Findings: []Finding{
			{Severity: severityHigh, Type: typeLogic},
		},
		Consensus: Consensus{Resolution: ResolutionApproved},
	}
	contract := g.GenerateFromFindings(bundle)
	if contract.Contract.Assertions[0].Value != 99.5 {
		t.Errorf("threshold = %f, want 99.5 (unmodified)", contract.Contract.Assertions[0].Value)
	}
}

func TestContractGenerator_UnknownCategory_HighSeverity(t *testing.T) {
	g := NewContractGenerator()
	bundle := &EvidenceBundle{
		ReviewID: "rev-016",
		Findings: []Finding{
			{Severity: severityHigh, Type: "unknown_category"},
		},
		Consensus: Consensus{Resolution: ResolutionApproved},
	}
	contract := g.GenerateFromFindings(bundle)
	if len(contract.Contract.Assertions) != 1 {
		t.Fatalf("expected 1 assertion for unknown high severity, got %d", len(contract.Contract.Assertions))
	}
	if contract.Contract.Assertions[0].Value != 99.5 {
		t.Errorf("unknown high threshold = %f, want 99.5", contract.Contract.Assertions[0].Value)
	}
}

func TestContractGenerator_UnknownCategory_CriticalSeverity(t *testing.T) {
	g := NewContractGenerator()
	bundle := &EvidenceBundle{
		ReviewID: "rev-017",
		Findings: []Finding{
			{Severity: severityCritical, Type: "unknown_category"},
		},
		Consensus: Consensus{Resolution: ResolutionApproved},
	}
	contract := g.GenerateFromFindings(bundle)
	if len(contract.Contract.Assertions) != 1 {
		t.Fatalf("expected 1 assertion, got %d", len(contract.Contract.Assertions))
	}
	if contract.Contract.Assertions[0].Value != 99.9 {
		t.Errorf("unknown critical threshold = %f, want 99.9", contract.Contract.Assertions[0].Value)
	}
}

func TestContractGenerator_MultipleSecurityFindings(t *testing.T) {
	g := NewContractGenerator()
	bundle := &EvidenceBundle{
		ReviewID: "rev-018",
		Findings: []Finding{
			{Severity: severityCritical, Type: typeSecurity, File: "a.go"},
			{Severity: severityCritical, Type: typeSecurity, File: "b.go"},
			{Severity: severityHigh, Type: typeSecurity, File: "c.go"},
		},
		Consensus: Consensus{Resolution: ResolutionApproved},
	}
	contract := g.GenerateFromFindings(bundle)
	// Each critical security = 2 assertions, high security = 2 assertions
	// Total = 6
	if len(contract.Contract.Assertions) != 6 {
		t.Fatalf("expected 6 assertions (3 findings × 2 each), got %d", len(contract.Contract.Assertions))
	}
}

func TestBreachActionFromConsensus(t *testing.T) {
	tests := []struct {
		resolution string
		expected   string
	}{
		{ResolutionApproved, breachActionRollback},
		{ResolutionBlocked, breachActionBlock},
		{ResolutionTieBreak, breachActionRollback},
		{"unknown", breachActionAlert},
		{"", breachActionAlert},
	}
	for _, tt := range tests {
		action := breachActionFromConsensus(Consensus{Resolution: tt.resolution})
		if action != tt.expected {
			t.Errorf("resolution %q → action %q, want %q", tt.resolution, action, tt.expected)
		}
	}
}

func TestExtractAgentFromPR(t *testing.T) {
	tests := []struct {
		prURL    string
		expected string
	}{
		{"https://forgejo.example.com/org/repo/pulls/42", "42"},
		{"https://forgejo.example.com/org/repo/pulls/agent-7", "agent-7"},
		{"", ""},
		{"not-a-url", ""},
		{"https://example.com/no/pulls/here", "here"}, // best-effort
	}
	for _, tt := range tests {
		result := extractAgentFromPR(tt.prURL)
		if result != tt.expected {
			t.Errorf("extractAgentFromPR(%q) = %q, want %q", tt.prURL, result, tt.expected)
		}
	}
}

func TestContractGenerator_Summary(t *testing.T) {
	g := NewContractGenerator()
	contract := &verify.BehaviorContract{
		Contract: verify.ContractBody{
			Name:         "test-contract",
			Agent:        "agent-1",
			BreachAction: breachActionRollback,
			Assertions: []verify.Assertion{
				{Metric: "success_rate", Op: "gte", Value: 99.5, Window: "1h"},
				{Metric: "latency_p99", Op: "lte", Value: 500.0, Window: "5m"},
			},
		},
	}
	summary := g.Summary(contract)
	if !strings.Contains(summary, "test-contract") {
		t.Error("summary missing contract name")
	}
	if !strings.Contains(summary, "agent-1") {
		t.Error("summary missing agent")
	}
	if !strings.Contains(summary, "2") {
		t.Error("summary missing assertion count")
	}
	if !strings.Contains(summary, "success_rate") {
		t.Error("summary missing metric name")
	}
}

func TestContractGenerator_Summary_Nil(t *testing.T) {
	g := NewContractGenerator()
	summary := g.Summary(nil)
	if !strings.Contains(summary, "no contract") {
		t.Errorf("nil summary = %q", summary)
	}
}

func TestContractGenerator_GenerateFromFindings_NameDerivation(t *testing.T) {
	g := NewContractGenerator()
	bundle := &EvidenceBundle{
		ReviewID:  "rev-special-id",
		PRURL:     "https://forgejo.example.com/org/repo/pulls/42",
		Timestamp: time.Now(),
		Consensus: Consensus{Resolution: ResolutionApproved},
	}
	contract := g.GenerateFromFindings(bundle)
	if contract.Contract.Name != "review-rev-special-id" {
		t.Errorf("Name = %q, want review-rev-special-id", contract.Contract.Name)
	}
}
