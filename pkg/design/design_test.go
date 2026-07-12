package design

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/review"
)

func TestDesignReviewRequestCreation(t *testing.T) {
	req := &DesignReviewRequest{
		SpecRef:        "spec-abc123",
		ADRRefs:        []string{"adr-1", "adr-2"},
		ContractSchema: json.RawMessage(`{"openapi":"3.0.0"}`),
		Context: DesignContext{
			TeamCapacity:    4.0,
			BudgetRemaining: 25.0,
			TimelineDays:    14,
		},
		SpecTitle: "Auth redesign",
		SpecText:  "Implement OAuth auth with rate limit and acceptance tests for error paths.",
		ADRTexts:  []string{"Decision: use OAuth2. Status: accepted."},
	}
	if req.SpecRef == "" || len(req.ADRRefs) != 2 {
		t.Fatalf("request not populated: %+v", req)
	}
	if req.Context.BudgetRemaining != 25.0 {
		t.Fatalf("budget: got %v", req.Context.BudgetRemaining)
	}
	if len(req.ContractSchema) == 0 {
		t.Fatal("expected contract schema")
	}
	doc := buildDesignDocument(req)
	if !strings.Contains(doc, "spec-abc123") || !strings.Contains(doc, "OAuth") {
		t.Fatalf("design document missing expected content:\n%s", doc)
	}
}

func TestDesignFindingWithDesignAspect(t *testing.T) {
	f := DesignFinding{
		Finding: review.Finding{
			Model:       "@assumption-buster",
			Severity:    "high",
			Type:        string(AspectAssumption),
			File:        "design",
			Line:        3,
			Description: "No rollback plan",
			Evidence:    "rollback keyword missing",
		},
		ID:                "DF-001",
		DesignAspect:      AspectAssumption,
		AffectedComponent: "design-surface",
		DesignLine:        3,
		AgentType:         review.AgentAssumptionBuster,
		RiskLevel:         FindRiskLevel("high"),
	}
	if !ValidDesignAspect(f.DesignAspect) {
		t.Fatalf("aspect should be valid: %s", f.DesignAspect)
	}
	if f.RiskLevel != "high" {
		t.Fatalf("risk level: got %q", f.RiskLevel)
	}
	if f.ID != "DF-001" {
		t.Fatalf("id: got %q", f.ID)
	}
	// Round-trip JSON.
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back DesignFinding
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.DesignAspect != AspectAssumption || back.Description != f.Description {
		t.Fatalf("round-trip mismatch: %+v", back)
	}
}

func TestThreatMapStructure(t *testing.T) {
	tm := ThreatMap{
		Services: []ThreatService{
			{Name: "auth-service", Components: []string{"token", "session"}, RiskScore: 70},
			{Name: "api-gateway", RiskScore: 40},
		},
		DataFlows: []DataFlow{
			{From: "client", To: "api-gateway", Description: "HTTPS", Sensitive: true},
		},
		TrustBoundaries: []TrustBoundary{
			{Name: "public-edge", Services: []string{"api-gateway"}},
		},
		AttackVectors: []AttackVector{
			{Description: "Token replay", Severity: "high", AffectedService: "auth-service", Mitigation: "bind tokens"},
		},
	}
	if len(tm.Services) != 2 || len(tm.AttackVectors) != 1 {
		t.Fatalf("threat map incomplete: %+v", tm)
	}
	if tm.AttackVectors[0].Severity != "high" {
		t.Fatalf("severity: %s", tm.AttackVectors[0].Severity)
	}
	raw, err := json.Marshal(tm)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), "auth-service") {
		t.Fatalf("json missing service: %s", raw)
	}
}

func TestFindRiskLevelClassification(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"critical", "high"},
		{"HIGH", "high"},
		{"error", "high"},
		{"medium", "medium"},
		{"warning", "medium"},
		{"warn", "medium"},
		{"low", "low"},
		{"info", "low"},
		{"", "low"},
		{"unknown-sev", "medium"},
	}
	for _, tc := range cases {
		got := FindRiskLevel(tc.in)
		if got != tc.want {
			t.Errorf("FindRiskLevel(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestDesignReviewDispatcher_Review(t *testing.T) {
	d := NewDesignReviewDispatcher()
	req := &DesignReviewRequest{
		SpecRef:   "spec-auth-1",
		ADRRefs:   []string{"adr-oauth"},
		Context:   DesignContext{BudgetRemaining: 100, TeamCapacity: 2, TimelineDays: 10},
		SpecTitle: "OAuth auth gateway",
		SpecText: strings.Join([]string{
			"## Overview",
			"Add OAuth authentication for the public API.",
			"Include rate limit, error handling, acceptance criteria, and tests.",
			"Crypto for token signing; secrets in sealed storage.",
			"Retry with circuit breaker for auth dependency timeouts.",
			"Rollback plan: feature flag disable.",
		}, "\n"),
		ADRTexts: []string{
			"Status: accepted\nDecision: use OAuth2 authorization code flow with short-lived tokens.",
		},
	}

	report, err := d.Review(context.Background(), req)
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if report.SpecRef != req.SpecRef {
		t.Fatalf("spec ref: %s", report.SpecRef)
	}
	if len(report.AgentsRun) < 3 {
		t.Fatalf("expected at least assumption-buster, cost-auditor, consistency-checker; got %v", report.AgentsRun)
	}
	// Assumption-buster must enumerate assumptions.
	assumptions := AssumptionsByRisk(report.Findings)
	if len(assumptions) < 5 {
		t.Fatalf("assumption-buster should enumerate ≥5 assumptions, got %d findings (assumptions=%d)", len(report.Findings), len(assumptions))
	}
	// Threat map should include attack vectors for auth/crypto design.
	if len(report.ThreatSurface.AttackVectors) == 0 {
		t.Fatalf("expected threat map attack vectors, got none: %+v", report.ThreatSurface)
	}
	// Consensus verdict is one of PASS/WARN/FAIL.
	switch report.Consensus.Verdict {
	case VerdictPASS, VerdictWARN, VerdictFAIL:
	default:
		t.Fatalf("unexpected consensus verdict: %q", report.Consensus.Verdict)
	}
	if report.RiskScore < 0 || report.RiskScore > 100 {
		t.Fatalf("risk score out of range: %v", report.RiskScore)
	}
	if report.CostProjection.CostTotal <= 0 {
		t.Fatalf("expected positive cost projection, got %+v", report.CostProjection)
	}
	if report.ReviewedAt.IsZero() {
		t.Fatal("reviewed_at not set")
	}
	// Ensure report is JSON-serializable for --json CLI path.
	if _, err := json.Marshal(report); err != nil {
		t.Fatalf("json marshal report: %v", err)
	}
	_ = time.Now()
}

func TestDesignReviewDispatcher_RequiresSpecRef(t *testing.T) {
	d := NewDesignReviewDispatcher()
	_, err := d.Review(context.Background(), &DesignReviewRequest{})
	if err == nil {
		t.Fatal("expected error for empty spec_ref")
	}
}

func TestFilterFindingsByID(t *testing.T) {
	findings := []DesignFinding{
		{ID: "DF-001", DesignAspect: AspectAssumption},
		{ID: "DF-002", DesignAspect: AspectThreat},
	}
	got := FilterFindingsByID(findings, "DF-002")
	if len(got) != 1 || got[0].DesignAspect != AspectThreat {
		t.Fatalf("filter: %+v", got)
	}
	if FilterFindingsByID(findings, "missing") != nil && len(FilterFindingsByID(findings, "missing")) != 0 {
		t.Fatal("expected empty for missing id")
	}
}
