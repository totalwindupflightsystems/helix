package health

import (
	"testing"
	"time"
)

func TestDefaultSLATargets(t *testing.T) {
	s := DefaultSLATargets()
	if s.Sync.TaskToAgentAssigned.P50 != 2*time.Second {
		t.Errorf("sync P50 = %v, want 2s", s.Sync.TaskToAgentAssigned.P50)
	}
	if s.Review.Tier1.P99 != 8*time.Second {
		t.Errorf("tier1 P99 = %v, want 8s", s.Review.Tier1.P99)
	}
	if s.Sandbox.ColdStart != 16*time.Second {
		t.Errorf("cold start = %v, want 16s", s.Sandbox.ColdStart)
	}
	if s.Cost.SimpleLow != 0.10 {
		t.Errorf("simple low = %v, want 0.10", s.Cost.SimpleLow)
	}
	if s.Monitoring.ScrapeInterval != 15*time.Second {
		t.Errorf("scrape interval = %v, want 15s", s.Monitoring.ScrapeInterval)
	}
}

func TestLatencyBudgetIsBreached(t *testing.T) {
	b := LatencyBudget{P50: 2 * time.Second, P95: 10 * time.Second, P99: 30 * time.Second}

	tests := []struct {
		name       string
		percentile string
		actual     time.Duration
		want       bool
	}{
		{"under P50", "P50", 1 * time.Second, false},
		{"at P50", "P50", 2 * time.Second, false},
		{"over P50", "P50", 3 * time.Second, true},
		{"under P95", "P95", 9 * time.Second, false},
		{"over P95", "P95", 11 * time.Second, true},
		{"under P99", "P99", 29 * time.Second, false},
		{"over P99", "P99", 31 * time.Second, true},
		{"bad percentile", "P100", 100 * time.Second, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := b.IsBreached(tt.percentile, tt.actual)
			if got != tt.want {
				t.Errorf("IsBreached(%s, %v) = %v, want %v", tt.percentile, tt.actual, got, tt.want)
			}
		})
	}
}

func TestCheckLatency(t *testing.T) {
	budget := LatencyBudget{P50: 5 * time.Second, P95: 10 * time.Second, P99: 20 * time.Second}

	// No breach
	b := CheckLatency("test", "P95", budget, 8*time.Second)
	if b != nil {
		t.Error("expected nil breach for under-target latency")
	}

	// Breach
	b = CheckLatency("test", "P95", budget, 12*time.Second)
	if b == nil {
		t.Fatal("expected breach for over-target latency")
	}
	if b.SLAName != "test" {
		t.Errorf("SLAName = %s, want test", b.SLAName)
	}
	if b.Percentile != "P95" {
		t.Errorf("Percentile = %s, want P95", b.Percentile)
	}
	if b.Target != 10*time.Second {
		t.Errorf("Target = %v, want 10s", b.Target)
	}
	if b.Exceeded != 2*time.Second {
		t.Errorf("Exceeded = %v, want 2s", b.Exceeded)
	}
}

func TestSLARecorderSyncLatency(t *testing.T) {
	r := NewSLARecorder(nil)

	// Under target — no breach
	b := r.RecordSyncLatency("task_to_agent_assigned", 1*time.Second)
	if b != nil {
		t.Error("expected no breach for 1s sync (P99=30s)")
	}

	// Over target — breach
	b = r.RecordSyncLatency("task_to_agent_assigned", 35*time.Second)
	if b == nil {
		t.Fatal("expected breach for 35s sync (P99=30s)")
	}
	if b.SLAName != "sync.task_to_agent_assigned" {
		t.Errorf("SLAName = %s", b.SLAName)
	}
	if !r.HasBreaches() {
		t.Error("HasBreaches should be true")
	}
}

func TestSLARecorderReviewLatency(t *testing.T) {
	r := NewSLARecorder(nil)

	b := r.RecordReviewLatency("tier1", 1*time.Second)
	if b != nil {
		t.Error("expected no breach for 1s tier1 (P99=8s)")
	}

	b = r.RecordReviewLatency("tier1", 10*time.Second)
	if b == nil {
		t.Fatal("expected breach for 10s tier1 (P99=8s)")
	}
	if b.SLAName != "review.tier1" {
		t.Errorf("SLAName = %s, want review.tier1", b.SLAName)
	}
}

func TestSLARecorderAPILatency(t *testing.T) {
	r := NewSLARecorder(nil)

	b := r.RecordAPILatency("forgejo_read", 10*time.Millisecond)
	if b != nil {
		t.Error("expected no breach for 10ms forgejo_read (P99=100ms)")
	}

	b = r.RecordAPILatency("forgejo_read", 150*time.Millisecond)
	if b == nil {
		t.Fatal("expected breach for 150ms forgejo_read (P99=100ms)")
	}

	b = r.RecordAPILatency("unknown_endpoint", 10*time.Second)
	if b != nil {
		t.Error("expected nil for unknown endpoint")
	}
}

func TestSLARecorderSandboxStartup(t *testing.T) {
	r := NewSLARecorder(nil)

	// Cold start under target
	b := r.RecordSandboxStartup(true, 10*time.Second)
	if b != nil {
		t.Error("expected no breach for 10s cold start (target=16s)")
	}

	// Cold start over target
	b = r.RecordSandboxStartup(true, 20*time.Second)
	if b == nil {
		t.Fatal("expected breach for 20s cold start (target=16s)")
	}
	if b.SLAName != "sandbox.cold" {
		t.Errorf("SLAName = %s, want sandbox.cold", b.SLAName)
	}

	// Warm start under target
	b = r.RecordSandboxStartup(false, 3*time.Second)
	if b != nil {
		t.Error("expected no breach for 3s warm start (target=4s)")
	}

	// Warm start over target
	b = r.RecordSandboxStartup(false, 6*time.Second)
	if b == nil {
		t.Fatal("expected breach for 6s warm start (target=4s)")
	}
	if b.SLAName != "sandbox.warm" {
		t.Errorf("SLAName = %s, want sandbox.warm", b.SLAName)
	}
}

func TestSLARecorderCheckCostPerPR(t *testing.T) {
	r := NewSLARecorder(nil)

	// Simple PR within range
	cb := r.CheckCostPerPR("simple", 0.20)
	if cb != nil {
		t.Error("expected no breach for $0.20 simple PR")
	}

	// Simple PR over range
	cb = r.CheckCostPerPR("simple", 0.50)
	if cb == nil {
		t.Fatal("expected breach for $0.50 simple PR (max=$0.30)")
	}
	if cb.OverBy != 0.20 {
		t.Errorf("OverBy = %v, want 0.20", cb.OverBy)
	}

	// Medium PR within range
	cb = r.CheckCostPerPR("medium", 1.00)
	if cb != nil {
		t.Error("expected no breach for $1.00 medium PR")
	}

	// Complex PR over range
	cb = r.CheckCostPerPR("complex", 15.00)
	if cb == nil {
		t.Fatal("expected breach for $15.00 complex PR (max=$10.00)")
	}
	if cb.OverBy != 5.00 {
		t.Errorf("OverBy = %v, want 5.00", cb.OverBy)
	}

	// Unknown complexity
	cb = r.CheckCostPerPR("unknown", 100.00)
	if cb != nil {
		t.Error("expected nil for unknown complexity")
	}
}

func TestSLARecorderBreachesAndReset(t *testing.T) {
	r := NewSLARecorder(nil)

	_ = r.RecordSyncLatency("task_to_agent_assigned", 35*time.Second)
	_ = r.RecordReviewLatency("tier1", 10*time.Second)

	breaches := r.Breaches()
	if len(breaches) != 2 {
		t.Fatalf("Breaches() = %d, want 2", len(breaches))
	}

	r.Reset()
	if r.HasBreaches() {
		t.Error("HasBreaches should be false after Reset")
	}
	breaches = r.Breaches()
	if len(breaches) != 0 {
		t.Errorf("Breaches() after Reset = %d, want 0", len(breaches))
	}
}

func TestSLARecorderSamples(t *testing.T) {
	r := NewSLARecorder(nil)

	_ = r.RecordSyncLatency("task_to_agent_assigned", 5*time.Second)
	_ = r.RecordSyncLatency("task_to_agent_assigned", 10*time.Second)

	samples := r.Samples("sync.task_to_agent_assigned")
	if len(samples) != 2 {
		t.Fatalf("Samples count = %d, want 2", len(samples))
	}
	if samples[0] != 5*time.Second || samples[1] != 10*time.Second {
		t.Errorf("Samples = %v, want [5s, 10s]", samples)
	}

	// Non-existent key
	none := r.Samples("nonexistent")
	if none != nil {
		t.Errorf("Samples for nonexistent key = %v, want nil", none)
	}
}

func TestFormatBreach(t *testing.T) {
	b := &SLABreach{
		SLAName:    "sync.test",
		Percentile: "P99",
		Target:     30 * time.Second,
		Actual:     35 * time.Second,
		Exceeded:   5 * time.Second,
	}
	s := FormatBreach(b)
	if s == "" {
		t.Error("FormatBreach returned empty string")
	}
	if s == "" {
		t.Error("expected non-empty format")
	}

	// Nil
	s = FormatBreach(nil)
	if s != "" {
		t.Errorf("FormatBreach(nil) = %q, want empty", s)
	}
}

func TestFormatCostBreach(t *testing.T) {
	b := &CostBreach{
		Complexity:  "simple",
		ExpectedMin: 0.10,
		ExpectedMax: 0.30,
		Actual:      0.50,
		OverBy:      0.20,
	}
	s := FormatCostBreach(b)
	if s == "" {
		t.Error("FormatCostBreach returned empty string")
	}

	s = FormatCostBreach(nil)
	if s != "" {
		t.Errorf("FormatCostBreach(nil) = %q, want empty", s)
	}
}

func TestSLARecorderUnknownStage(t *testing.T) {
	r := NewSLARecorder(nil)
	b := r.RecordSyncLatency("unknown_stage", 100*time.Second)
	if b != nil {
		t.Error("expected nil for unknown sync stage")
	}
	b = r.RecordReviewLatency("unknown_gate", 100*time.Second)
	if b != nil {
		t.Error("expected nil for unknown review gate")
	}
}

func TestNewSLARecorderNilTargets(t *testing.T) {
	r := NewSLARecorder(nil)
	if r.targets == nil {
		t.Error("targets should default to DefaultSLATargets")
	}
}

func TestSLARecorderThreadSafe(t *testing.T) {
	r := NewSLARecorder(nil)
	done := make(chan struct{})

	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				_ = r.RecordSyncLatency("task_to_agent_assigned", 5*time.Second)
				_ = r.Breaches()
			}
			done <- struct{}{}
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// All 5s, P99=30s, no breaches expected
	if r.HasBreaches() {
		t.Errorf("expected no breaches for under-target latencies, got %d", len(r.Breaches()))
	}
	samples := r.Samples("sync.task_to_agent_assigned")
	if len(samples) != 100 {
		t.Errorf("samples count = %d, want 100", len(samples))
	}
}
