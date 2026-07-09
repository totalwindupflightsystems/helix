package ideation

import (
	"testing"
)

func TestValidateAgentsAndRiskBounds(t *testing.T) {
	v := NewIdeaValidator()
	idea := &Idea{
		ID:     "abc123",
		Title:  "Add rate limiting to auth",
		Body:   "Protect login endpoints from brute force with token bucket",
		Tags:   []string{"auth", "security"},
		Source: SourceHuman,
	}
	report, err := v.Validate(idea)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(report.AgentsRun) < 2 {
		t.Fatalf("agents_run = %v, want >=2", report.AgentsRun)
	}
	if report.RiskScore < 0 || report.RiskScore > 100 {
		t.Fatalf("risk_score = %v out of bounds", report.RiskScore)
	}
	if report.IdeaID != idea.ID {
		t.Fatalf("idea_id = %q", report.IdeaID)
	}
	switch report.Verdict {
	case VerdictPass, VerdictFail, VerdictNeedsClarification:
		// ok
	default:
		t.Fatalf("unexpected verdict %q", report.Verdict)
	}
	if len(report.Findings) == 0 {
		t.Fatal("expected findings")
	}
}

func TestValidateHighRiskBigBang(t *testing.T) {
	v := NewIdeaValidator()
	idea := &Idea{
		ID:    "risk1",
		Title: "big bang rewrite everything",
		Body:  "We should just rewrite everything with a big bang migration simply and easily",
		// no tags, no evidence → assumption findings too
	}
	report, err := v.Validate(idea)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if report.RiskScore < 40 {
		t.Fatalf("expected elevated risk, got %v", report.RiskScore)
	}
	// Should at least need clarification or fail.
	if report.Verdict == VerdictPass {
		t.Fatalf("expected non-pass verdict, got pass with risk %v", report.RiskScore)
	}
	hasArch := false
	for _, f := range report.Findings {
		if f.AgentType == AgentArchitectureFit && f.Severity == SeverityHigh {
			hasArch = true
		}
	}
	if !hasArch {
		t.Fatal("expected high architecture-fit finding for big bang language")
	}
}

func TestValidateNilIdea(t *testing.T) {
	v := NewIdeaValidator()
	if _, err := v.Validate(nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := v.Validate(&Idea{}); err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestValidateLowRiskCompleteIdea(t *testing.T) {
	v := NewIdeaValidator()
	idea := &Idea{
		ID:    "good1",
		Title: "Instrument auth latency histograms",
		Body:  "Add Prometheus histograms for login and token refresh latency percentiles.",
		Tags:  []string{"observability", "metrics"},
		Evidence: []EvidenceRef{
			{Type: EvidenceFile, Ref: "pkg/auth/login.go", Description: "hot path"},
		},
	}
	report, err := v.Validate(idea)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	// Base 20 + info findings only from both agents if clean → around 30.
	if report.RiskScore > 55 {
		t.Fatalf("unexpected high risk for well-formed idea: %v findings=%+v", report.RiskScore, report.Findings)
	}
}
