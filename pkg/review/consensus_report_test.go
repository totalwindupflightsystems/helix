package review

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// FormatConsensusReport
// ---------------------------------------------------------------------------

func TestFormatConsensusReport_FullBundle(t *testing.T) {
	bundle := EvidenceBundle{
		PRURL:           "https://forgejo.example.com/owner/repo/pulls/42",
		ReviewID:        "rev-abc123",
		Timestamp:       time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC),
		BiasStrippedSHA: "abcdef1234567890abcdef1234567890abcdef12",
		OriginalCommit:  "fedcba0987654321fedcba0987654321fedcba09",
		Formation: Formation{
			Primary:     ModelInfo{Model: "glm-5.2", Provider: "zai-glm"},
			Adversarial: ModelInfo{Model: "minimax-m3", Provider: "minimax"},
			Audit:       ModelInfo{Model: "deepseek-v4-flash", Provider: "deepseek"},
		},
		Findings: []Finding{
			{
				Model:       "minimax-m3",
				Severity:    "high",
				Type:        "security",
				File:        "auth/handler.go",
				Line:        45,
				Description: "SQL injection in login handler",
				Evidence:    "User input concatenated into query string",
			},
			{
				Model:       "deepseek-v4-flash",
				Severity:    "medium",
				Type:        "performance",
				File:        "cache/redis.go",
				Line:        120,
				Description: "N+1 query pattern in cache warming",
				Evidence:    "Loop calls Get() for each key individually",
			},
		},
		Consensus: Consensus{
			PrimaryVerdict:     VerdictApproved,
			AdversarialVerdict: VerdictBlock,
			AuditVerdict:       VerdictPassWithNotes,
			Resolution:         ResolutionTieBreak,
			TieBreaker:         "Chimera arbiter invoked",
		},
	}

	report := FormatConsensusReport(bundle)

	// Check key sections are present.
	if !containsStr(report, "Helix Adversarial Review Report") {
		t.Error("Missing report header")
	}
	if !containsStr(report, "pulls/42") {
		t.Error("Missing PR URL")
	}
	if !containsStr(report, "rev-abc123") {
		t.Error("Missing review ID")
	}
	if !containsStr(report, "glm-5.2") {
		t.Error("Missing primary model")
	}
	if !containsStr(report, "minimax-m3") {
		t.Error("Missing adversarial model")
	}
	if !containsStr(report, "SQL injection") {
		t.Error("Missing finding description")
	}
	if !containsStr(report, "auth/handler.go:45") {
		t.Error("Missing file:line location")
	}
	if !containsStr(report, "Tie-breaker") {
		t.Error("Missing tie-breaker resolution")
	}
	if !containsStr(report, "abcdef123456") {
		t.Error("Missing bias-stripped SHA")
	}
	if !containsStr(report, "rev-abc123.json") {
		t.Error("Missing evidence bundle link")
	}
}

func TestFormatConsensusReport_NoFindings(t *testing.T) {
	bundle := EvidenceBundle{
		PRURL:    "https://example.com/pr/1",
		ReviewID: "rev-clean",
		Formation: Formation{
			Primary: ModelInfo{Model: "glm-5.2", Provider: "zai-glm"},
		},
		Findings: nil,
		Consensus: Consensus{
			PrimaryVerdict: VerdictApproved,
			Resolution:     ResolutionApproved,
		},
	}

	report := FormatConsensusReport(bundle)

	if !containsStr(report, "No findings reported") {
		t.Error("Expected 'No findings reported' for empty findings")
	}
	if !containsStr(report, "Approved") {
		t.Error("Expected Approved resolution")
	}
}

func TestFormatConsensusReport_MinimalBundle(t *testing.T) {
	bundle := EvidenceBundle{
		PRURL:    "https://example.com/pr/1",
		ReviewID: "rev-min",
		Formation: Formation{
			Primary: ModelInfo{Model: "test-model", Provider: "test-provider"},
		},
	}

	report := FormatConsensusReport(bundle)

	if !containsStr(report, "test-model") {
		t.Error("Missing model name")
	}
	// With no adversarial/audit, diversity is 1.
	if !containsStr(report, "1 distinct providers") {
		t.Error("Expected 1 distinct provider for single-model formation")
	}
}

// ---------------------------------------------------------------------------
// RenderFindingsTable
// ---------------------------------------------------------------------------

func TestRenderFindingsTable_Empty(t *testing.T) {
	result := RenderFindingsTable(nil)
	if !containsStr(result, "No findings reported") {
		t.Error("Expected no-findings message for nil")
	}
}

func TestRenderFindingsTable_SingleFinding(t *testing.T) {
	findings := []Finding{
		{
			Model:       "model-a",
			Severity:    "critical",
			Type:        "bug",
			File:        "main.go",
			Line:        100,
			Description: "Null pointer dereference",
		},
	}

	result := RenderFindingsTable(findings)
	if !containsStr(result, "model-a") {
		t.Error("Missing model name")
	}
	if !containsStr(result, "critical") {
		t.Error("Missing severity")
	}
	if !containsStr(result, "main.go:100") {
		t.Error("Missing file:line")
	}
	if !containsStr(result, "Null pointer dereference") {
		t.Error("Missing description")
	}
}

func TestRenderFindingsTable_NoLine(t *testing.T) {
	findings := []Finding{
		{
			Model:       "model-b",
			Severity:    "low",
			Type:        "style",
			File:        "config.yaml",
			Line:        0,
			Description: "Inconsistent indentation",
		},
	}

	result := RenderFindingsTable(findings)
	// When Line is 0, location should just be the file name.
	if !containsStr(result, "config.yaml") {
		t.Error("Missing file name")
	}
	if containsStr(result, "config.yaml:0") {
		t.Error("Should not show :0 for zero line number")
	}
}

func TestRenderFindingsTable_MultipleFindings(t *testing.T) {
	findings := []Finding{
		{Model: "m1", Severity: "high", Type: "security", File: "a.go", Line: 1, Description: "bug1"},
		{Model: "m2", Severity: "medium", Type: "performance", File: "b.go", Line: 2, Description: "bug2"},
		{Model: "m3", Severity: "low", Type: "style", File: "c.go", Line: 3, Description: "bug3"},
	}

	result := RenderFindingsTable(findings)
	if !containsStr(result, "bug1") || !containsStr(result, "bug2") || !containsStr(result, "bug3") {
		t.Error("Missing one or more finding descriptions")
	}
}

// ---------------------------------------------------------------------------
// RenderConsensusBlock
// ---------------------------------------------------------------------------

func TestRenderConsensusBlock_AllVerdicts(t *testing.T) {
	consensus := Consensus{
		PrimaryVerdict:     VerdictApproved,
		AdversarialVerdict: VerdictBlock,
		AuditVerdict:       VerdictPassWithNotes,
		Resolution:         ResolutionTieBreak,
		TieBreaker:         "Chimera decided: APPROVE",
	}

	result := RenderConsensusBlock(consensus)

	if !containsStr(result, "Approved") {
		t.Error("Missing approved verdict")
	}
	if !containsStr(result, "Block") {
		t.Error("Missing block verdict")
	}
	if !containsStr(result, "Tie-breaker required") {
		t.Error("Missing tie-breaker resolution")
	}
	if !containsStr(result, "Chimera decided") {
		t.Error("Missing tie-breaker detail")
	}
}

func TestRenderConsensusBlock_PrimaryOnly(t *testing.T) {
	consensus := Consensus{
		PrimaryVerdict: VerdictApproved,
		Resolution:     ResolutionApproved,
	}

	result := RenderConsensusBlock(consensus)

	if !containsStr(result, "Primary") {
		t.Error("Missing primary role")
	}
	// Primary approved + resolution approved → report should reflect approval.
	if !containsStr(result, "Approved") {
		t.Error("Missing approved status")
	}
}

func TestRenderConsensusBlock_BlockedResolution(t *testing.T) {
	consensus := Consensus{
		PrimaryVerdict:     VerdictBlock,
		AdversarialVerdict: VerdictBlock,
		Resolution:         ResolutionBlocked,
	}

	result := RenderConsensusBlock(consensus)

	if !containsStr(result, "Blocked") {
		t.Error("Missing blocked resolution")
	}
}

func TestRenderConsensusBlock_EmptyVerdicts(t *testing.T) {
	consensus := Consensus{}

	result := RenderConsensusBlock(consensus)

	// Empty verdict should render as "—".
	if !containsStr(result, "—") {
		t.Error("Empty verdict should show as dash")
	}
}

// ---------------------------------------------------------------------------
// formatVerdict
// ---------------------------------------------------------------------------

func TestFormatVerdict(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{VerdictApproved, "✅ Approved"},
		{VerdictPassWithNotes, "⚠️ Pass with notes"},
		{VerdictBlock, "❌ Block"},
		{VerdictConfirmAdversarial, "🔴 Confirmed adversarial"},
		{VerdictOverrule, "↩️ Overruled"},
		{"", "—"},
		{"custom-verdict", "custom-verdict"},
	}

	for _, tc := range tests {
		got := formatVerdict(tc.input)
		if got != tc.want {
			t.Errorf("formatVerdict(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// formatResolution
// ---------------------------------------------------------------------------

func TestFormatResolution(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{ResolutionApproved, "✅ Approved — merge allowed"},
		{ResolutionBlocked, "❌ Blocked — merge denied"},
		{ResolutionTieBreak, "⚖️ Tie-breaker required — escalated to Chimera arbiter"},
		{"", "—"},
		{"custom", "custom"},
	}

	for _, tc := range tests {
		got := formatResolution(tc.input)
		if got != tc.want {
			t.Errorf("formatResolution(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// shortSHA
// ---------------------------------------------------------------------------

func TestShortSHA(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"abcdef1234567890abcdef", "abcdef123456"},
		{"short", "short"},
		{"", ""},
		{"12345678901234567890", "123456789012"},
	}

	for _, tc := range tests {
		got := shortSHA(tc.input)
		if got != tc.want {
			t.Errorf("shortSHA(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// containsStr is defined in orchestrator_test.go
