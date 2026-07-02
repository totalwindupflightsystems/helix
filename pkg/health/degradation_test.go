package health

import (
	"testing"
)

func TestEvaluateDegradation_AllHealthy(t *testing.T) {
	report := EvaluateDegradation(nil, nil)
	if report.HasBlocked() {
		t.Error("expected no blocked capabilities with no down subsystems")
	}
	if report.HasDegraded() {
		t.Error("expected no degraded capabilities with no down subsystems")
	}
	if report.Summary() != "All capabilities available" {
		t.Errorf("expected 'All capabilities available', got %q", report.Summary())
	}
}

func TestEvaluateDegradation_ForgejoDown(t *testing.T) {
	report := EvaluateDegradation([]string{"forgejo"}, nil)

	// Per spec §14.2: agents can write code locally, everything else blocked
	assertCapState(t, report, CapWriteCode, CapAvailable)
	assertCapState(t, report, CapPushCode, CapBlocked)
	assertCapState(t, report, CapOpenPR, CapBlocked)
	assertCapState(t, report, CapMergePR, CapBlocked)
	assertCapState(t, report, CapCostEstimate, CapAvailable)
	assertCapState(t, report, CapTrustScore, CapAvailable)
}

func TestEvaluateDegradation_ChimeraDown(t *testing.T) {
	report := EvaluateDegradation([]string{"chimera"}, nil)

	// Per spec §14.2: human review works, Conscientiousness still runs,
	// PRs can merge with human-only approval (degraded)
	assertCapState(t, report, CapAgentReview, CapBlocked)
	assertCapState(t, report, CapMergePR, CapDegraded)
	assertCapState(t, report, CapNegotiation, CapBlocked)
	assertCapState(t, report, CapHumanReview, CapAvailable)
	assertCapState(t, report, CapAdversarial, CapAvailable)
}

func TestEvaluateDegradation_ConscientiousnessDown(t *testing.T) {
	report := EvaluateDegradation([]string{"conscientiousness"}, nil)

	assertCapState(t, report, CapAdversarial, CapDegraded)
	assertCapState(t, report, CapAgentReview, CapAvailable)
	assertCapState(t, report, CapHumanReview, CapAvailable)
}

func TestEvaluateDegradation_HivemindDown(t *testing.T) {
	report := EvaluateDegradation([]string{"hivemind"}, nil)

	assertCapState(t, report, CapTaskSchedule, CapBlocked)
	assertCapState(t, report, CapWriteCode, CapAvailable)
}

func TestEvaluateDegradation_LangFuseDown(t *testing.T) {
	report := EvaluateDegradation([]string{"langfuse"}, nil)

	assertCapState(t, report, CapTracing, CapDegraded)
	assertCapState(t, report, CapMetrics, CapAvailable)
	assertCapState(t, report, CapMergePR, CapAvailable)
}

func TestEvaluateDegradation_PrometheusDown(t *testing.T) {
	report := EvaluateDegradation([]string{"prometheus"}, nil)

	assertCapState(t, report, CapMetrics, CapDegraded)
	assertCapState(t, report, CapAlerting, CapDegraded)
	assertCapState(t, report, CapMergePR, CapAvailable)
}

func TestEvaluateDegradation_SandboxDown(t *testing.T) {
	report := EvaluateDegradation([]string{"sandbox"}, nil)

	assertCapState(t, report, CapSandbox, CapBlocked)
	assertCapState(t, report, CapWriteCode, CapDegraded)
}

func TestEvaluateDegradation_TrustDown(t *testing.T) {
	report := EvaluateDegradation([]string{"trust"}, nil)

	assertCapState(t, report, CapTrustScore, CapDegraded)
	assertCapState(t, report, CapMergePR, CapDegraded)
}

func TestEvaluateDegradation_MultipleDown(t *testing.T) {
	report := EvaluateDegradation([]string{"forgejo", "chimera"}, nil)

	// Forgejo blocks everything; Chimera can't add more blocking on top
	assertCapState(t, report, CapPushCode, CapBlocked)
	assertCapState(t, report, CapMergePR, CapBlocked)
	assertCapState(t, report, CapAgentReview, CapBlocked)
	assertCapState(t, report, CapWriteCode, CapAvailable)

	if !report.HasBlocked() {
		t.Error("expected blocked capabilities with forgejo+chimera down")
	}
}

func TestEvaluateDegradation_DegradedSubsystem(t *testing.T) {
	report := EvaluateDegradation(nil, []string{"chimera"})

	// Chimera degraded → agent review degraded (not blocked)
	assertCapState(t, report, CapAgentReview, CapDegraded)
	assertCapState(t, report, CapHumanReview, CapAvailable)
}

func TestEvaluateDegradation_BlockedTakesPriority(t *testing.T) {
	// If one subsystem blocks a cap and another degrades it, blocked wins
	report := EvaluateDegradation([]string{"forgejo"}, []string{"chimera"})

	assertCapState(t, report, CapMergePR, CapBlocked) // forgejo blocks
}

func TestEvaluateFromDashboard(t *testing.T) {
	dash := &DashboardReport{
		Overall: StateDegraded,
		Subsystems: []SubsystemStatus{
			{Name: "forgejo", State: StateHealthy},
			{Name: "chimera", State: StateDown},
			{Name: "langfuse", State: StateDegraded},
		},
	}

	report := EvaluateFromDashboard(dash)

	if len(report.DownSubsys) != 1 || report.DownSubsys[0] != "chimera" {
		t.Errorf("expected chimera in down subsystems, got %v", report.DownSubsys)
	}
	assertCapState(t, report, CapAgentReview, CapBlocked)
	assertCapState(t, report, CapTracing, CapDegraded)
}

func TestDegradationReport_BlockedList(t *testing.T) {
	report := EvaluateDegradation([]string{"forgejo"}, nil)
	blocked := report.Blocked()

	if len(blocked) == 0 {
		t.Fatal("expected blocked capabilities with forgejo down")
	}

	found := false
	for _, c := range blocked {
		if c == CapPushCode {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected push_code in blocked list")
	}
}

func TestDegradationReport_DegradedList(t *testing.T) {
	report := EvaluateDegradation([]string{"langfuse"}, nil)
	degraded := report.Degraded()

	if len(degraded) == 0 {
		t.Fatal("expected degraded capabilities with langfuse down")
	}
}

func TestFormatDegradationReport_AllAvailable(t *testing.T) {
	report := EvaluateDegradation(nil, nil)
	output := FormatDegradationReport(report)

	if output == "" {
		t.Fatal("expected non-empty output")
	}
}

func TestFormatDegradationReport_WithBlocked(t *testing.T) {
	report := EvaluateDegradation([]string{"forgejo"}, nil)
	output := FormatDegradationReport(report)

	if output == "" {
		t.Fatal("expected non-empty output")
	}
}

func TestFormatDegradationReport_WithDegraded(t *testing.T) {
	report := EvaluateDegradation([]string{"langfuse"}, nil)
	output := FormatDegradationReport(report)

	if output == "" {
		t.Fatal("expected non-empty output")
	}
}

func TestSeverityRank(t *testing.T) {
	if severityRank(CapAvailable) != 0 {
		t.Error("available rank should be 0")
	}
	if severityRank(CapDegraded) != 1 {
		t.Error("degraded rank should be 1")
	}
	if severityRank(CapBlocked) != 2 {
		t.Error("blocked rank should be 2")
	}
}

func TestDegradeImpact(t *testing.T) {
	if degradeImpact(CapBlocked) != CapDegraded {
		t.Error("blocked should degrade to degraded")
	}
	if degradeImpact(CapDegraded) != CapDegraded {
		t.Error("degraded stays degraded")
	}
	if degradeImpact(CapAvailable) != CapAvailable {
		t.Error("available stays available")
	}
}

func TestAllCapabilities(t *testing.T) {
	caps := AllCapabilities()

	if len(caps) == 0 {
		t.Fatal("expected at least some capabilities")
	}

	// Ensure no duplicates
	seen := make(map[Capability]bool)
	for _, c := range caps {
		if seen[c] {
			t.Errorf("duplicate capability: %s", c)
		}
		seen[c] = true
	}

	// Ensure sorted
	for i := 1; i < len(caps); i++ {
		if string(caps[i-1]) > string(caps[i]) {
			t.Error("capabilities not sorted")
			break
		}
	}
}

func TestFindDownCause(t *testing.T) {
	cause := findDownCause(CapAgentReview, map[string]bool{"chimera": true}, nil)
	if cause == "" {
		t.Error("expected non-empty cause for agent_review with chimera down")
	}
}

func TestEvaluateDegradation_AllSubsystemsDown(t *testing.T) {
	allDown := []string{
		"forgejo", "chimera", "conscientiousness", "hivemind",
		"langfuse", "prometheus", "sandbox", "trust", "review",
		"negotiate", "dispatcher", "marketplace", "estimate",
	}
	report := EvaluateDegradation(allDown, nil)

	if !report.HasBlocked() {
		t.Error("expected blocked capabilities with all subsystems down")
	}
}

// --- helpers ---

func assertCapState(t *testing.T, report *DegradationReport, cap Capability, expected CapState) {
	t.Helper()
	for _, c := range report.Capabilities {
		if c.Capability == cap {
			if c.State != expected {
				t.Errorf("capability %s: expected %s, got %s (reason: %s)",
					cap, expected, c.State, c.Reason)
			}
			return
		}
	}
	t.Errorf("capability %s not found in report", cap)
}
