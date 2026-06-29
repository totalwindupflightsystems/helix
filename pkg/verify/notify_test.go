package verify

import (
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Mock Notifier for testing
// ============================================================================

type mockNotifier struct {
	name     string
	received []*BreachNotification
	fail     bool
}

func (m *mockNotifier) Name() string { return m.name }

func (m *mockNotifier) Notify(n *BreachNotification) ChannelResult {
	if m.fail {
		return ChannelResult{Channel: m.name, Status: StatusFailed, Error: "mock failure"}
	}
	m.received = append(m.received, n)
	return ChannelResult{Channel: m.name, Status: StatusDelivered}
}

// ============================================================================
// BreachNotification and NotificationResult tests
// ============================================================================

func TestBreachNotification_Fields(t *testing.T) {
	n := &BreachNotification{
		ContractName:      "auth-v2",
		AgentID:           "agent-001",
		MergeCommit:       "abc123",
		FailedChecks:      []CheckResult{{Assertion: Assertion{Metric: "latency", Op: "lte", Value: 200}, Passed: false}},
		MetricsSnapshot:   map[string]float64{"latency": 350},
		EvidenceLinks:     []string{"grafana/dashboard"},
		RecommendedAction: ActionRollback,
		Timestamp:         time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC),
	}
	if n.ContractName != "auth-v2" {
		t.Errorf("ContractName = %q", n.ContractName)
	}
	if n.AgentID != "agent-001" {
		t.Errorf("AgentID = %q", n.AgentID)
	}
	if n.RecommendedAction != ActionRollback {
		t.Errorf("RecommendedAction = %q", n.RecommendedAction)
	}
}

// ============================================================================
// NotificationDispatcher tests
// ============================================================================

func TestNotificationDispatcher_Notify_AllDelivered(t *testing.T) {
	ch1 := &mockNotifier{name: "channel1"}
	ch2 := &mockNotifier{name: "channel2"}
	d := NewNotificationDispatcher(ch1, ch2)

	n := &BreachNotification{
		ContractName: "contract-a",
		AgentID:      "agent-1",
	}
	result := d.Notify(n)

	if !result.AllDelivered {
		t.Error("expected AllDelivered = true")
	}
	if len(result.Channels) != 2 {
		t.Fatalf("Channels = %d, want 2", len(result.Channels))
	}
	if len(ch1.received) != 1 {
		t.Error("channel1 should have received 1 notification")
	}
	if len(ch2.received) != 1 {
		t.Error("channel2 should have received 1 notification")
	}
}

func TestNotificationDispatcher_Notify_PartialFailure(t *testing.T) {
	ch1 := &mockNotifier{name: "good"}
	ch2 := &mockNotifier{name: "bad", fail: true}
	d := NewNotificationDispatcher(ch1, ch2)

	result := d.Notify(&BreachNotification{
		ContractName: "c",
		AgentID:      "a",
	})

	if result.AllDelivered {
		t.Error("expected AllDelivered = false when one channel fails")
	}
}

func TestNotificationDispatcher_Debounce(t *testing.T) {
	ch := &mockNotifier{name: "test"}
	d := NewNotificationDispatcher(ch)
	d.SetDebounceTTL(5 * time.Minute)

	n := &BreachNotification{
		ContractName: "contract-d",
		AgentID:      "agent-d",
	}

	// First send — should deliver.
	r1 := d.Notify(n)
	if !r1.AllDelivered {
		t.Fatal("first notify should deliver")
	}
	if len(ch.received) != 1 {
		t.Fatalf("after first: received %d, want 1", len(ch.received))
	}

	// Second send within debounce — should be skipped.
	r2 := d.Notify(n)
	if r2.AllDelivered {
		t.Fatal("second notify within debounce should not deliver")
	}
	if len(r2.Channels) != 1 || r2.Channels[0].Status != StatusSkipped {
		t.Errorf("expected 1 skipped channel, got %+v", r2.Channels)
	}
	if len(ch.received) != 1 {
		t.Fatalf("after second: received %d, want still 1 (debounced)", len(ch.received))
	}
}

func TestNotificationDispatcher_Debounce_DifferentContract(t *testing.T) {
	ch := &mockNotifier{name: "test"}
	d := NewNotificationDispatcher(ch)

	n1 := &BreachNotification{ContractName: "c1", AgentID: "agent-a"}
	n2 := &BreachNotification{ContractName: "c2", AgentID: "agent-a"}

	d.Notify(n1)
	d.Notify(n2)

	if len(ch.received) != 2 {
		t.Fatalf("different contracts should not debounce: received %d", len(ch.received))
	}
}

func TestNotificationDispatcher_Debounce_DifferentAgent(t *testing.T) {
	ch := &mockNotifier{name: "test"}
	d := NewNotificationDispatcher(ch)

	n1 := &BreachNotification{ContractName: "c1", AgentID: "agent-a"}
	n2 := &BreachNotification{ContractName: "c1", AgentID: "agent-b"}

	d.Notify(n1)
	d.Notify(n2)

	if len(ch.received) != 2 {
		t.Fatalf("different agents should not debounce: received %d", len(ch.received))
	}
}

func TestNotificationDispatcher_Debounce_Expired(t *testing.T) {
	ch := &mockNotifier{name: "test"}
	d := NewNotificationDispatcher(ch)
	d.SetDebounceTTL(50 * time.Millisecond)

	n := &BreachNotification{ContractName: "c", AgentID: "a"}

	d.Notify(n)
	time.Sleep(60 * time.Millisecond)
	r2 := d.Notify(n)

	if !r2.AllDelivered {
		t.Fatal("after debounce expires, should deliver again")
	}
	if len(ch.received) != 2 {
		t.Fatalf("after debounce expiry: received %d, want 2", len(ch.received))
	}
}

func TestNotificationDispatcher_ClearDebounce(t *testing.T) {
	ch := &mockNotifier{name: "test"}
	d := NewNotificationDispatcher(ch)
	d.SetDebounceTTL(5 * time.Minute)

	n := &BreachNotification{ContractName: "c", AgentID: "a"}
	d.Notify(n)
	d.Notify(n) // debounced

	d.ClearDebounce("a", "c")
	r3 := d.Notify(n)

	if !r3.AllDelivered {
		t.Fatal("after ClearDebounce, should deliver")
	}
	if len(ch.received) != 2 {
		t.Fatalf("after clear: received %d, want 2", len(ch.received))
	}
}

func TestNotificationDispatcher_AddChannel(t *testing.T) {
	ch1 := &mockNotifier{name: "ch1"}
	d := NewNotificationDispatcher(ch1)

	d.AddChannel(&mockNotifier{name: "ch2"})
	channels := d.Channels()
	if len(channels) != 2 {
		t.Fatalf("Channels = %v, want 2", channels)
	}
}

func TestNotificationDispatcher_Channels(t *testing.T) {
	d := NewNotificationDispatcher(
		&mockNotifier{name: "alpha"},
		&mockNotifier{name: "beta"},
		&mockNotifier{name: "gamma"},
	)
	names := d.Channels()
	if len(names) != 3 {
		t.Fatalf("len = %d", len(names))
	}
}

func TestNotificationDispatcher_SetDebounceTTL(t *testing.T) {
	d := NewNotificationDispatcher()
	d.SetDebounceTTL(100 * time.Millisecond)
	if d.debounceTTL != 100*time.Millisecond {
		t.Errorf("debounceTTL = %v", d.debounceTTL)
	}
}

// ============================================================================
// NotifyFromBreach tests
// ============================================================================

func TestNotifyFromBreach(t *testing.T) {
	ch := &mockNotifier{name: "test"}
	d := NewNotificationDispatcher(ch)

	breach := &Breach{
		ContractName: "auth-session",
		Agent:        "agent-x",
		MergeCommit:  "deadbeef",
		FailedChecks: []CheckResult{{
			Assertion:     Assertion{Metric: "success_rate", Op: "gte", Value: 0.999},
			Passed:        false,
			MeasuredValue: 0.95,
		}},
		ShouldRollback: true,
		ShouldNotify:   true,
	}

	metrics := map[string]float64{"success_rate": 0.95}
	links := []string{"grafana/abc123"}

	result := d.NotifyFromBreach(breach, metrics, links)

	if !result.AllDelivered {
		t.Fatal("should deliver")
	}
	if result.Notification.AgentID != "agent-x" {
		t.Errorf("AgentID = %q", result.Notification.AgentID)
	}
	if result.Notification.RecommendedAction != ActionRollback {
		t.Errorf("RecommendedAction = %q, want %q", result.Notification.RecommendedAction, ActionRollback)
	}
	if len(result.Notification.FailedChecks) != 1 {
		t.Errorf("FailedChecks = %d", len(result.Notification.FailedChecks))
	}
	if len(result.Notification.EvidenceLinks) != 1 {
		t.Errorf("EvidenceLinks = %d", len(result.Notification.EvidenceLinks))
	}
}

func TestNotifyFromBreach_NotifyOnly(t *testing.T) {
	d := NewNotificationDispatcher()

	breach := &Breach{
		ContractName:   "test-contract",
		Agent:          "a",
		FailedChecks:   []CheckResult{{Assertion: Assertion{Metric: "x"}, Passed: false}},
		ShouldRollback: false,
		ShouldNotify:   true,
	}

	result := d.NotifyFromBreach(breach, nil, nil)
	if result.Notification.RecommendedAction != ActionNotifyOnly {
		t.Errorf("Action = %q, want %q", result.Notification.RecommendedAction, ActionNotifyOnly)
	}
}

func TestNotifyFromBreach_Investigate(t *testing.T) {
	d := NewNotificationDispatcher()

	breach := &Breach{
		ContractName:   "test-contract",
		Agent:          "a",
		FailedChecks:   []CheckResult{{Assertion: Assertion{Metric: "x"}, Passed: false}},
		ShouldRollback: false,
		ShouldNotify:   false,
	}

	result := d.NotifyFromBreach(breach, nil, nil)
	if result.Notification.RecommendedAction != ActionInvestigate {
		t.Errorf("Action = %q, want %q", result.Notification.RecommendedAction, ActionInvestigate)
	}
}

// ============================================================================
// ForgejoPRNotifier tests
// ============================================================================

func TestForgejoPRNotifier_WithURL(t *testing.T) {
	f := &ForgejoPRNotifier{PRURL: "http://localhost:3030/owner/repo/pulls/42"}
	n := &BreachNotification{
		ContractName: "auth-v2",
		AgentID:      "agent-1",
		MergeCommit:  "abc123",
		FailedChecks: []CheckResult{{
			Assertion: Assertion{Metric: "latency", Op: "lte", Value: 200},
			Passed:    false,
		}},
		MetricsSnapshot:   map[string]float64{"latency": 350},
		Timestamp:         time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC),
		RecommendedAction: ActionRollback,
	}

	result := f.Notify(n)
	if result.Status != StatusDelivered {
		t.Fatalf("Status = %q, want delivered", result.Status)
	}
	if !strings.Contains(result.Detail, "Behavior Contract Breach") {
		t.Error("detail should contain breach comment header")
	}
	if !strings.Contains(result.Detail, "auth-v2") {
		t.Error("detail should contain contract name")
	}
}

func TestForgejoPRNotifier_NoURL(t *testing.T) {
	f := &ForgejoPRNotifier{}
	result := f.Notify(&BreachNotification{})
	if result.Status != StatusSkipped {
		t.Errorf("Status = %q, want skipped", result.Status)
	}
}

// ============================================================================
// TrustLedgerNotifier tests
// ============================================================================

func TestTrustLedgerNotifier_WithCallback(t *testing.T) {
	var calledAgent string
	var calledSeverity float64
	var calledDesc string

	tn := &TrustLedgerNotifier{
		PenaltyCallback: func(agentID string, severity float64, description string) {
			calledAgent = agentID
			calledSeverity = severity
			calledDesc = description
		},
	}

	n := &BreachNotification{
		AgentID:      "agent-1",
		ContractName: "test-c",
		FailedChecks: []CheckResult{{Passed: false}},
	}
	result := tn.Notify(n)

	if result.Status != StatusDelivered {
		t.Fatalf("Status = %q", result.Status)
	}
	if calledAgent != "agent-1" {
		t.Errorf("agent = %q", calledAgent)
	}
	if calledSeverity <= 0 {
		t.Error("severity should be > 0")
	}
	if !strings.Contains(calledDesc, "test-c") {
		t.Errorf("desc = %q", calledDesc)
	}
}

func TestTrustLedgerNotifier_NoCallback(t *testing.T) {
	tn := &TrustLedgerNotifier{}
	result := tn.Notify(&BreachNotification{})
	if result.Status != StatusSkipped {
		t.Errorf("Status = %q, want skipped", result.Status)
	}
}

// ============================================================================
// IncidentStoreNotifier tests
// ============================================================================

func TestIncidentStoreNotifier_WithCallback(t *testing.T) {
	is := &IncidentStoreNotifier{
		RecordCallback: func(n *BreachNotification) string {
			return "INC-001"
		},
	}
	result := is.Notify(&BreachNotification{AgentID: "agent-1"})
	if result.Status != StatusDelivered {
		t.Fatalf("Status = %q", result.Status)
	}
	if !strings.Contains(result.Detail, "INC-001") {
		t.Errorf("Detail = %q", result.Detail)
	}
}

func TestIncidentStoreNotifier_EmptyID(t *testing.T) {
	is := &IncidentStoreNotifier{
		RecordCallback: func(n *BreachNotification) string { return "" },
	}
	result := is.Notify(&BreachNotification{})
	if result.Status != StatusFailed {
		t.Errorf("Status = %q, want failed", result.Status)
	}
}

func TestIncidentStoreNotifier_NoCallback(t *testing.T) {
	is := &IncidentStoreNotifier{}
	result := is.Notify(&BreachNotification{})
	if result.Status != StatusSkipped {
		t.Errorf("Status = %q, want skipped", result.Status)
	}
}

// ============================================================================
// FormatBreachComment tests
// ============================================================================

func TestFormatBreachComment(t *testing.T) {
	n := &BreachNotification{
		ContractName: "auth-session",
		AgentID:      "agent-xyz",
		MergeCommit:  "abc123",
		FailedChecks: []CheckResult{{
			Assertion: Assertion{Metric: "p99_latency_ms", Op: "lte", Value: 200},
			Passed:    false,
		}},
		MetricsSnapshot:   map[string]float64{"p99_latency_ms": 350},
		EvidenceLinks:     []string{"grafana/abc", "logs/def"},
		RecommendedAction: ActionRollback,
		Timestamp:         time.Now(),
	}

	comment := FormatBreachComment(n)
	if !strings.Contains(comment, "Behavior Contract Breach") {
		t.Error("missing header")
	}
	if !strings.Contains(comment, "auth-session") {
		t.Error("missing contract name")
	}
	if !strings.Contains(comment, "agent-xyz") {
		t.Error("missing agent ID")
	}
	if !strings.Contains(comment, "p99_latency_ms") {
		t.Error("missing metric")
	}
	if !strings.Contains(comment, "grafana/abc") {
		t.Error("missing evidence link")
	}
	if !strings.Contains(comment, "rollback_and_notify") {
		t.Error("missing recommended action")
	}
}

func TestFormatBreachComment_NoEvidence(t *testing.T) {
	n := &BreachNotification{
		ContractName: "c",
		AgentID:      "a",
		FailedChecks: []CheckResult{{Passed: false, Assertion: Assertion{Metric: "m"}}},
	}
	comment := FormatBreachComment(n)
	if strings.Contains(comment, "Evidence") {
		t.Error("should not contain Evidence section when empty")
	}
}

func TestFormatBreachComment_MultipleFailures(t *testing.T) {
	n := &BreachNotification{
		ContractName: "c",
		AgentID:      "a",
		FailedChecks: []CheckResult{
			{Passed: false, Assertion: Assertion{Metric: "m1"}},
			{Passed: false, Assertion: Assertion{Metric: "m2"}},
			{Passed: false, Assertion: Assertion{Metric: "m3"}},
		},
		MetricsSnapshot: map[string]float64{},
	}
	comment := FormatBreachComment(n)
	// Should have 3 table rows.
	count := strings.Count(comment, "| ❌ |")
	if count != 3 {
		t.Errorf("expected 3 failed assertion rows, got %d", count)
	}
}

// ============================================================================
// breachSeverity tests
// ============================================================================

func TestBreachSeverity(t *testing.T) {
	tests := []struct {
		name   string
		checks int
		minSev float64
		maxSev float64
	}{
		{"zero", 0, 0.0, 0.0},
		{"one", 1, 0.05, 0.05},
		{"two", 2, 0.10, 0.10},
		{"three", 3, 0.20, 0.20},
		{"four", 4, 0.20, 0.20},
		{"five", 5, 0.40, 0.40},
		{"ten", 10, 0.40, 0.40},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			checks := make([]CheckResult, tc.checks)
			n := &BreachNotification{FailedChecks: checks}
			sev := breachSeverity(n)
			if sev < tc.minSev || sev > tc.maxSev {
				t.Errorf("severity = %.2f, want [%.2f, %.2f]", sev, tc.minSev, tc.maxSev)
			}
		})
	}
}

// ============================================================================
// Full pipeline: Monitor → Breach → Dispatcher → Channels
// ============================================================================

func TestFullPipeline_MonitorToNotification(t *testing.T) {
	// Create a monitor with a contract that will breach.
	m := NewMonitor()
	m.RegisterContract(&BehaviorContract{
		Contract: ContractBody{
			Name:        "test-contract",
			Agent:       "agent-pipeline",
			MergeCommit: "abc123",
			Assertions: []Assertion{
				{Metric: "success_rate", Op: "gte", Value: 0.99},
			},
			BreachAction: "rollback_and_notify",
		},
	})

	// Metrics that breach the contract.
	metrics := map[string]float64{"success_rate": 0.80}

	breaches := m.Evaluate(metrics)
	if len(breaches) != 1 {
		t.Fatalf("breaches = %d, want 1", len(breaches))
	}

	// Dispatch notifications.
	ch := &mockNotifier{name: "pipeline-test"}
	d := NewNotificationDispatcher(ch)

	result := d.NotifyFromBreach(&breaches[0], metrics, []string{"evidence/1"})

	if !result.AllDelivered {
		t.Fatal("pipeline should deliver")
	}
	if len(ch.received) != 1 {
		t.Fatalf("channel received %d, want 1", len(ch.received))
	}
	if ch.received[0].AgentID != "agent-pipeline" {
		t.Errorf("agent = %q", ch.received[0].AgentID)
	}
}

// ============================================================================
// Multi-channel integration test
// ============================================================================

func TestMultiChannel_AllThreeChannels(t *testing.T) {
	var penaltyAgent string
	var incidentID string

	dispatcher := NewNotificationDispatcher(
		&ForgejoPRNotifier{PRURL: "http://localhost:3030/owner/repo/pulls/1"},
		&TrustLedgerNotifier{
			PenaltyCallback: func(agentID string, sev float64, desc string) {
				penaltyAgent = agentID
			},
		},
		&IncidentStoreNotifier{
			RecordCallback: func(n *BreachNotification) string {
				incidentID = "INC-002"
				return incidentID
			},
		},
	)

	n := &BreachNotification{
		ContractName: "integration-test",
		AgentID:      "agent-multi",
		FailedChecks: []CheckResult{{Passed: false, Assertion: Assertion{Metric: "m"}}},
	}

	result := dispatcher.Notify(n)

	if !result.AllDelivered {
		t.Fatal("all channels should deliver")
	}
	if len(result.Channels) != 3 {
		t.Fatalf("channels = %d, want 3", len(result.Channels))
	}
	if penaltyAgent != "agent-multi" {
		t.Errorf("penaltyAgent = %q", penaltyAgent)
	}
	if incidentID != "INC-002" {
		t.Errorf("incidentID = %q", incidentID)
	}
}

func TestMultiChannel_DebounceBlocksAll(t *testing.T) {
	dispatcher := NewNotificationDispatcher(
		&ForgejoPRNotifier{PRURL: "http://x"},
		&TrustLedgerNotifier{
			PenaltyCallback: func(a string, s float64, d string) {},
		},
		&IncidentStoreNotifier{
			RecordCallback: func(n *BreachNotification) string { return "INC-3" },
		},
	)

	n := &BreachNotification{
		ContractName: "debounce-test",
		AgentID:      "agent-d",
		FailedChecks: []CheckResult{{Passed: false, Assertion: Assertion{Metric: "m"}}},
	}

	r1 := dispatcher.Notify(n)
	r2 := dispatcher.Notify(n) // same agent+contract — should debounce

	if !r1.AllDelivered {
		t.Fatal("first should deliver")
	}
	if r2.AllDelivered {
		t.Fatal("second should be debounced")
	}
}
