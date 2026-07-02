package health

import (
	"strings"
	"sync"
	"testing"
)

func TestNewPlatformMetricsRecorder(t *testing.T) {
	r := NewPlatformMetricsRecorder()
	if r == nil {
		t.Fatal("expected non-nil recorder")
	}
	if r.GetActiveAgents() != 0 {
		t.Errorf("new recorder activeAgents = %d, want 0", r.GetActiveAgents())
	}
	if r.GetQueuedTasks() != 0 {
		t.Errorf("new recorder queuedTasks = %d, want 0", r.GetQueuedTasks())
	}
}

func TestPlatformMetrics_RecordPRCycleTime(t *testing.T) {
	r := NewPlatformMetricsRecorder()
	r.RecordPRCycleTime("helix", 300)
	r.RecordPRCycleTime("helix", 600)
	r.RecordPRCycleTime("helix", 900)

	out := r.Collect()
	if !strings.Contains(out, "helix_pr_cycle_time_seconds") {
		t.Error("expected helix_pr_cycle_time_seconds in output")
	}
	if !strings.Contains(out, `repo="helix"`) {
		t.Error("expected repo=helix label in output")
	}
	if !strings.Contains(out, `quantile="0.5"`) {
		t.Error("expected quantile=0.5 in output")
	}
	if !strings.Contains(out, `quantile="0.95"`) {
		t.Error("expected quantile=0.95 in output")
	}
	if !strings.Contains(out, `quantile="0.99"`) {
		t.Error("expected quantile=0.99 in output")
	}
}

func TestPlatformMetrics_RecordPRCycleTime_Negative(t *testing.T) {
	r := NewPlatformMetricsRecorder()
	r.RecordPRCycleTime("helix", -100)
	out := r.Collect()
	if !strings.Contains(out, "helix_pr_cycle_time_seconds{repo=\"helix\",quantile=\"0.5\"} 0") {
		t.Error("expected negative cycle time to be clamped to 0")
	}
}

func TestPlatformMetrics_RecordPRCycleTime_MultipleRepos(t *testing.T) {
	r := NewPlatformMetricsRecorder()
	r.RecordPRCycleTime("helix", 300)
	r.RecordPRCycleTime("chimera", 500)

	out := r.Collect()
	if !strings.Contains(out, `repo="chimera"`) {
		t.Error("expected repo=chimera in output")
	}
	if !strings.Contains(out, `repo="helix"`) {
		t.Error("expected repo=helix in output")
	}
}

func TestPlatformMetrics_RecordGateResult(t *testing.T) {
	r := NewPlatformMetricsRecorder()

	// 4 passes, 1 fail
	r.RecordGateResult(GateTier1, true)
	r.RecordGateResult(GateTier1, true)
	r.RecordGateResult(GateTier1, true)
	r.RecordGateResult(GateTier1, true)
	r.RecordGateResult(GateTier1, false)

	rate := r.GetGatePassRate(GateTier1)
	if rate != 0.8 {
		t.Errorf("tier1 pass rate = %.2f, want 0.80", rate)
	}

	out := r.Collect()
	if !strings.Contains(out, "helix_gate_pass_rate") {
		t.Error("expected helix_gate_pass_rate in output")
	}
	if !strings.Contains(out, `gate="tier1"`) {
		t.Error("expected gate=tier1 label")
	}
}

func TestPlatformMetrics_RecordGateResult_AllGates(t *testing.T) {
	r := NewPlatformMetricsRecorder()

	for _, gate := range AllGateNames() {
		r.RecordGateResult(gate, true)
		r.RecordGateResult(gate, false)
	}

	out := r.Collect()
	for _, gate := range AllGateNames() {
		if !strings.Contains(out, `gate="`+string(gate)+`"`) {
			t.Errorf("expected gate=%q in output", string(gate))
		}
	}
}

func TestPlatformMetrics_RecordGateResult_EmptyGate(t *testing.T) {
	r := NewPlatformMetricsRecorder()
	rate := r.GetGatePassRate(GateChimera)
	if rate != 0 {
		t.Errorf("unrecorded gate rate = %.2f, want 0", rate)
	}
}

func TestPlatformMetrics_SetActiveAgents(t *testing.T) {
	r := NewPlatformMetricsRecorder()
	r.SetActiveAgents(12)

	if r.GetActiveAgents() != 12 {
		t.Errorf("activeAgents = %d, want 12", r.GetActiveAgents())
	}

	out := r.Collect()
	if !strings.Contains(out, "helix_active_agents 12") {
		t.Error("expected helix_active_agents 12 in output")
	}
}

func TestPlatformMetrics_SetActiveAgents_Negative(t *testing.T) {
	r := NewPlatformMetricsRecorder()
	r.SetActiveAgents(-5)

	if r.GetActiveAgents() != 0 {
		t.Errorf("activeAgents after negative set = %d, want 0", r.GetActiveAgents())
	}
}

func TestPlatformMetrics_SetQueuedTasks(t *testing.T) {
	r := NewPlatformMetricsRecorder()
	r.SetQueuedTasks(5)

	if r.GetQueuedTasks() != 5 {
		t.Errorf("queuedTasks = %d, want 5", r.GetQueuedTasks())
	}

	out := r.Collect()
	if !strings.Contains(out, "helix_queued_tasks 5") {
		t.Error("expected helix_queued_tasks 5 in output")
	}
}

func TestPlatformMetrics_SetQueuedTasks_Negative(t *testing.T) {
	r := NewPlatformMetricsRecorder()
	r.SetQueuedTasks(-3)

	if r.GetQueuedTasks() != 0 {
		t.Errorf("queuedTasks after negative set = %d, want 0", r.GetQueuedTasks())
	}
}

func TestPlatformMetrics_RecordAPILatency(t *testing.T) {
	r := NewPlatformMetricsRecorder()
	r.RecordAPILatency("GET /repos", 0.015)
	r.RecordAPILatency("GET /repos", 0.050)
	r.RecordAPILatency("GET /repos", 0.100)
	r.RecordAPILatency("POST /pulls", 0.030)

	out := r.Collect()
	if !strings.Contains(out, "helix_forgejo_api_latency_seconds") {
		t.Error("expected helix_forgejo_api_latency_seconds in output")
	}
	if !strings.Contains(out, `endpoint="GET /repos"`) {
		t.Error("expected endpoint=GET /repos")
	}
	if !strings.Contains(out, `endpoint="POST /pulls"`) {
		t.Error("expected endpoint=POST /pulls")
	}
}

func TestPlatformMetrics_RecordAPILatency_Negative(t *testing.T) {
	r := NewPlatformMetricsRecorder()
	r.RecordAPILatency("GET /health", -0.5)

	out := r.Collect()
	if !strings.Contains(out, "helix_forgejo_api_latency_seconds{endpoint=\"GET /health\",quantile=\"0.5\"} 0") {
		t.Error("expected negative latency clamped to 0")
	}
}

func TestPlatformMetrics_RecordPRCost(t *testing.T) {
	r := NewPlatformMetricsRecorder()
	r.RecordPRCost("helix", 1.50)
	r.RecordPRCost("helix", 2.50)
	r.RecordPRCost("helix", 3.00)

	avg := r.GetAvgCostPerPR("helix")
	expected := (1.50 + 2.50 + 3.00) / 3
	if avg != expected {
		t.Errorf("avg cost per PR = %.4f, want %.4f", avg, expected)
	}

	out := r.Collect()
	if !strings.Contains(out, "helix_cost_per_pr") {
		t.Error("expected helix_cost_per_pr in output")
	}
	if !strings.Contains(out, `repo="helix"`) {
		t.Error("expected repo=helix label in cost output")
	}
}

func TestPlatformMetrics_RecordPRCost_Negative(t *testing.T) {
	r := NewPlatformMetricsRecorder()
	r.RecordPRCost("helix", -10.0)

	out := r.Collect()
	if !strings.Contains(out, "helix_cost_per_pr{repo=\"helix\"} 0") {
		t.Error("expected negative cost clamped to 0")
	}
}

func TestPlatformMetrics_RecordMerge(t *testing.T) {
	r := NewPlatformMetricsRecorder()
	r.RecordMerge("helix", MergeHour)
	r.RecordMerge("helix", MergeHour)
	r.RecordMerge("helix", MergeDay)
	r.RecordMerge("chimera", MergeWeek)

	if r.GetMergeRate("helix", MergeHour) != 2 {
		t.Errorf("helix hour merges = %d, want 2", r.GetMergeRate("helix", MergeHour))
	}
	if r.GetMergeRate("helix", MergeDay) != 1 {
		t.Errorf("helix day merges = %d, want 1", r.GetMergeRate("helix", MergeDay))
	}
	if r.GetMergeRate("chimera", MergeWeek) != 1 {
		t.Errorf("chimera week merges = %d, want 1", r.GetMergeRate("chimera", MergeWeek))
	}

	out := r.Collect()
	if !strings.Contains(out, "helix_merge_rate") {
		t.Error("expected helix_merge_rate in output")
	}
	if !strings.Contains(out, `period="hour"`) {
		t.Error("expected period=hour in output")
	}
	if !strings.Contains(out, `period="day"`) {
		t.Error("expected period=day in output")
	}
	if !strings.Contains(out, `period="week"`) {
		t.Error("expected period=week in output")
	}
}

func TestPlatformMetrics_Collect_AllSevenMetrics(t *testing.T) {
	r := NewPlatformMetricsRecorder()
	r.RecordPRCycleTime("helix", 300)
	r.RecordGateResult(GateTier1, true)
	r.SetActiveAgents(5)
	r.SetQueuedTasks(3)
	r.RecordAPILatency("GET /version", 0.015)
	r.RecordPRCost("helix", 0.50)
	r.RecordMerge("helix", MergeDay)

	out := r.Collect()

	requiredMetrics := []string{
		"helix_pr_cycle_time_seconds",
		"helix_gate_pass_rate",
		"helix_active_agents",
		"helix_queued_tasks",
		"helix_forgejo_api_latency_seconds",
		"helix_cost_per_pr",
		"helix_merge_rate",
	}

	for _, metric := range requiredMetrics {
		if !strings.Contains(out, metric) {
			t.Errorf("expected metric %q in output", metric)
		}
	}
}

func TestPlatformMetricsRecorder_Collect_Empty(t *testing.T) {
	r := NewPlatformMetricsRecorder()
	out := r.Collect()

	if !strings.Contains(out, "helix_active_agents 0") {
		t.Error("expected helix_active_agents 0 in empty output")
	}
	if !strings.Contains(out, "helix_queued_tasks 0") {
		t.Error("expected helix_queued_tasks 0 in empty output")
	}
	if strings.Contains(out, "helix_pr_cycle_time_seconds{") {
		t.Error("expected no PR cycle time lines in empty output")
	}
}

func TestPlatformMetrics_Collect_Deterministic(t *testing.T) {
	r := NewPlatformMetricsRecorder()
	r.RecordPRCycleTime("zebra", 300)
	r.RecordPRCycleTime("alpha", 500)
	r.RecordPRCost("zebra", 1.0)
	r.RecordPRCost("alpha", 2.0)
	r.RecordAPILatency("POST /repos", 0.05)
	r.RecordAPILatency("GET /repos", 0.02)
	r.RecordMerge("zebra", MergeDay)
	r.RecordMerge("alpha", MergeDay)

	out1 := r.Collect()
	out2 := r.Collect()
	if out1 != out2 {
		t.Error("Collect output should be deterministic")
	}

	// alpha should come before zebra
	alphaIdx := strings.Index(out1, `repo="alpha"`)
	zebraIdx := strings.Index(out1, `repo="zebra"`)
	if alphaIdx < 0 || zebraIdx < 0 {
		t.Fatal("expected both repos in output")
	}
	if alphaIdx > zebraIdx {
		t.Error("expected alpha before zebra in deterministic output")
	}
}

func TestPlatformMetrics_MetricsSource_Interface(t *testing.T) {
	r := NewPlatformMetricsRecorder()
	r.RecordPRCycleTime("helix", 300)
	r.RecordGateResult(GateTier1, true)
	r.SetActiveAgents(5)

	if r.MetricsName() != "platform" {
		t.Errorf("MetricsName = %q, want %q", r.MetricsName(), "platform")
	}

	lines := r.CollectMetrics()
	if len(lines) == 0 {
		t.Fatal("expected non-empty metric lines")
	}

	// Check that key metrics are present
	foundPRCycle := false
	foundGatePass := false
	foundActiveAgents := false

	for _, line := range lines {
		switch line.Name {
		case "helix_pr_cycle_time_seconds":
			foundPRCycle = true
		case "helix_gate_pass_rate":
			foundGatePass = true
		case "helix_active_agents":
			foundActiveAgents = true
		}
	}

	if !foundPRCycle {
		t.Error("expected helix_pr_cycle_time_seconds in metric lines")
	}
	if !foundGatePass {
		t.Error("expected helix_gate_pass_rate in metric lines")
	}
	if !foundActiveAgents {
		t.Error("expected helix_active_agents in metric lines")
	}
}

func TestPlatformMetrics_MetricsSource_AggregatorIntegration(t *testing.T) {
	agg := NewPlatformMetricsCollector()
	recorder := NewPlatformMetricsRecorder()

	recorder.RecordPRCycleTime("helix", 300)
	recorder.RecordGateResult(GateTier1, true)
	recorder.SetActiveAgents(5)
	recorder.RecordMerge("helix", MergeDay)

	agg.RegisterSource(recorder)

	out := agg.Collect()
	if !strings.Contains(out, "helix_active_agents") {
		t.Error("expected helix_active_agents in aggregator output")
	}
	if !strings.Contains(out, "helix_gate_pass_rate") {
		t.Error("expected helix_gate_pass_rate in aggregator output")
	}
}

func TestPlatformMetrics_ToSnapshot(t *testing.T) {
	r := NewPlatformMetricsRecorder()

	// Record gate results
	r.RecordGateResult(GateTier1, true)
	r.RecordGateResult(GateTier1, true)
	r.RecordGateResult(GateTier1, false) // 2/3 = 0.667

	// Record PR cycle times
	r.RecordPRCycleTime("helix", 300)
	r.RecordPRCycleTime("helix", 600)
	r.RecordPRCycleTime("helix", 900)

	// Record costs
	r.RecordPRCost("helix", 1.0)
	r.RecordPRCost("helix", 3.0) // avg = 2.0

	snap := r.ToSnapshot()

	// Check gate pass rate
	tier1Rate, ok := snap.GatePassRates["tier1"]
	if !ok {
		t.Fatal("expected tier1 in gate pass rates")
	}
	expectedRate := 2.0 / 3.0
	if tier1Rate < expectedRate-0.01 || tier1Rate > expectedRate+0.01 {
		t.Errorf("tier1 rate = %.4f, want ~%.4f", tier1Rate, expectedRate)
	}

	// Check PR cycle time (P50 of [300, 600, 900] = 600)
	cycleTime, ok := snap.PRCycleTimes["helix"]
	if !ok {
		t.Fatal("expected helix in PR cycle times")
	}
	if cycleTime != 600 {
		t.Errorf("PR cycle P50 = %.0f, want 600", cycleTime)
	}

	// Check cost per PR
	cost, ok := snap.CostPerPR["helix"]
	if !ok {
		t.Fatal("expected helix in cost per PR")
	}
	if cost != 2.0 {
		t.Errorf("cost per PR = %.2f, want 2.0", cost)
	}

	// Check weekly avg matches
	weekly, ok := snap.WeeklyAvgCostPerPR["helix"]
	if !ok {
		t.Fatal("expected helix in weekly avg cost per PR")
	}
	if weekly != cost {
		t.Errorf("weekly avg = %.2f, want %.2f (= cost)", weekly, cost)
	}
}

func TestPlatformMetrics_ToSnapshot_Empty(t *testing.T) {
	r := NewPlatformMetricsRecorder()
	snap := r.ToSnapshot()

	if len(snap.GatePassRates) != 0 {
		t.Error("expected empty gate pass rates")
	}
	if len(snap.PRCycleTimes) != 0 {
		t.Error("expected empty PR cycle times")
	}
	if len(snap.CostPerPR) != 0 {
		t.Error("expected empty cost per PR")
	}
}

func TestPlatformMetrics_Reset(t *testing.T) {
	r := NewPlatformMetricsRecorder()

	r.RecordPRCycleTime("helix", 300)
	r.RecordGateResult(GateTier1, true)
	r.SetActiveAgents(5)
	r.SetQueuedTasks(3)
	r.RecordAPILatency("GET /health", 0.01)
	r.RecordPRCost("helix", 1.0)
	r.RecordMerge("helix", MergeDay)

	r.Reset()

	if r.GetActiveAgents() != 0 {
		t.Errorf("activeAgents after reset = %d, want 0", r.GetActiveAgents())
	}
	if r.GetQueuedTasks() != 0 {
		t.Errorf("queuedTasks after reset = %d, want 0", r.GetQueuedTasks())
	}
	if r.GetGatePassRate(GateTier1) != 0 {
		t.Errorf("gate pass rate after reset = %.2f, want 0", r.GetGatePassRate(GateTier1))
	}
	if r.GetAvgCostPerPR("helix") != 0 {
		t.Errorf("avg cost after reset = %.2f, want 0", r.GetAvgCostPerPR("helix"))
	}

	out := r.Collect()
	if strings.Contains(out, "helix_pr_cycle_time_seconds{") {
		t.Error("expected no PR cycle time after reset")
	}
}

func TestPlatformMetrics_GetSummary(t *testing.T) {
	r := NewPlatformMetricsRecorder()

	r.RecordPRCycleTime("helix", 300)
	r.RecordPRCycleTime("helix", 600)
	r.RecordPRCycleTime("chimera", 500)
	r.RecordGateResult(GateTier1, true)
	r.RecordGateResult(GateTier1, false)
	r.SetActiveAgents(8)
	r.SetQueuedTasks(4)
	r.RecordAPILatency("GET /health", 0.01)
	r.RecordAPILatency("GET /repos", 0.02)
	r.RecordPRCost("helix", 1.0)
	r.RecordMerge("helix", MergeDay)
	r.RecordMerge("chimera", MergeWeek)

	s := r.GetSummary()

	if s.TotalPRs != 3 {
		t.Errorf("TotalPRs = %d, want 3", s.TotalPRs)
	}
	if s.TotalGateResults != 2 {
		t.Errorf("TotalGateResults = %d, want 2", s.TotalGateResults)
	}
	if s.ActiveAgents != 8 {
		t.Errorf("ActiveAgents = %d, want 8", s.ActiveAgents)
	}
	if s.QueuedTasks != 4 {
		t.Errorf("QueuedTasks = %d, want 4", s.QueuedTasks)
	}
	if s.EndpointsTracked != 2 {
		t.Errorf("EndpointsTracked = %d, want 2", s.EndpointsTracked)
	}
	if s.ReposTracked != 2 {
		t.Errorf("ReposTracked = %d, want 2 (helix + chimera)", s.ReposTracked)
	}
}

func TestQuantile(t *testing.T) {
	tests := []struct {
		name   string
		sorted []float64
		p      float64
		want   float64
	}{
		{"empty", []float64{}, 50, 0},
		{"single", []float64{42}, 50, 42},
		{"p50_even", []float64{10, 20, 30, 40, 50}, 50, 30},
		{"p95", []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}, 95, 100},
		{"p99_short", []float64{1, 2, 3}, 99, 3},
		{"p0_clamped", []float64{10, 20, 30}, 0, 10},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := quantile(tc.sorted, tc.p)
			if got != tc.want {
				t.Errorf("quantile(%.0f) = %.1f, want %.1f", tc.p, got, tc.want)
			}
		})
	}
}

func TestAllGateNames(t *testing.T) {
	gates := AllGateNames()
	if len(gates) != 5 {
		t.Fatalf("expected 5 gate names, got %d", len(gates))
	}

	expected := []GateName{GateTier1, GateTier2, GateChimera, GateConscientiousness, GatePromptFoo}
	for i, g := range gates {
		if g != expected[i] {
			t.Errorf("gate[%d] = %q, want %q", i, string(g), string(expected[i]))
		}
	}
}

func TestPlatformMetrics_Concurrent(t *testing.T) {
	r := NewPlatformMetricsRecorder()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			r.RecordPRCycleTime("helix", float64(n)*10)
			r.RecordGateResult(GateTier1, n%5 != 0) // 80% pass rate
			r.RecordAPILatency("GET /repos", 0.01*float64(n))
			r.RecordPRCost("helix", float64(n)*0.5)
			r.RecordMerge("helix", MergeHour)
			r.SetActiveAgents(int64(n))
		}(i)
	}
	wg.Wait()

	// Should not panic — concurrent access is safe
	out := r.Collect()
	if !strings.Contains(out, "helix_pr_cycle_time_seconds") {
		t.Error("expected metrics after concurrent recording")
	}
	if !strings.Contains(out, "helix_merge_rate") {
		t.Error("expected merge rate after concurrent recording")
	}
}
