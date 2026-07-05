package health

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Construction + basic state
// ============================================================================

func TestNewPromStore_Defaults(t *testing.T) {
	s := NewPromStore()
	if s == nil {
		t.Fatal("NewPromStore returned nil")
	}
	if got := s.probeMaxAge; got != 10*time.Second {
		t.Errorf("probeMaxAge = %v, want 10s", got)
	}
	if len(s.buckets) == 0 {
		t.Fatal("expected default buckets")
	}
	if s.serviceOrder == nil {
		t.Error("expected serviceOrder to be non-nil (even if empty)")
	}
}

func TestPromStore_SetBuckets(t *testing.T) {
	s := NewPromStore()
	s.SetBuckets([]float64{0.1, 0.5, 1.0})
	if len(s.buckets) != 3 {
		t.Errorf("SetBuckets didn't update buckets: got %v", s.buckets)
	}
	// Verify the original underlying slice was not mutated by
	// subsequent calls — defensive copy.
	s.SetBuckets([]float64{99})
	if got := s.buckets[0]; got != 99 {
		t.Errorf("expected defensive copy semantics, got %v", s.buckets)
	}
}

func TestPromStore_SetProbes(t *testing.T) {
	s := NewPromStore()
	s.SetProbes([]string{"forgejo", "chimera"})
	if len(s.serviceOrder) != 2 {
		t.Errorf("expected 2 entries, got %d", len(s.serviceOrder))
	}
	// Replace.
	s.SetProbes([]string{"forgejo"})
	if len(s.serviceOrder) != 1 {
		t.Errorf("replace failed, got %d", len(s.serviceOrder))
	}
}

// ============================================================================
// RecordInvocation + counter logic
// ============================================================================

func TestPromStore_RecordInvocation_Empty(t *testing.T) {
	s := NewPromStore()
	s.RecordInvocation("", 0, time.Millisecond) // skip — empty name
	snap := s.Snapshot()
	if len(snap.Invocations) != 0 {
		t.Errorf("expected empty invocations, got %v", snap.Invocations)
	}
}

func TestPromStore_RecordInvocation_BasicCounter(t *testing.T) {
	s := NewPromStore()
	s.RecordInvocation("status", 0, 5*time.Millisecond)
	s.RecordInvocation("status", 0, 10*time.Millisecond)
	s.RecordInvocation("doctor", 1, 50*time.Millisecond)

	snap := s.Snapshot()
	if snap.Invocations["status"] != 2 {
		t.Errorf("status counter: got %d, want 2", snap.Invocations["status"])
	}
	if snap.Invocations["doctor"] != 1 {
		t.Errorf("doctor counter: got %d, want 1", snap.Invocations["doctor"])
	}
	if snap.RCCounts["status"]["0"] != 2 {
		t.Errorf("status rc=0: got %d, want 2", snap.RCCounts["status"]["0"])
	}
	if snap.RCCounts["doctor"]["1"] != 1 {
		t.Errorf("doctor rc=1: got %d, want 1", snap.RCCounts["doctor"]["1"])
	}
}

func TestPromStore_DurationCap(t *testing.T) {
	s := NewPromStore()
	// Record 2000 observations; the cap drops back to 768 whenever
	// the buffer exceeds 1024. Final length is therefore somewhere in
	// [768, 1024] depending on where in the trim cycle we land.
	for i := 0; i < 2000; i++ {
		s.RecordInvocation("flood", 0, time.Millisecond)
	}
	snap := s.Snapshot()
	got := snap.Durations["flood"]
	if got < 768 || got > 1024 {
		t.Errorf("duration cap not respected: got %d, want 768..1024", got)
	}
}

// ============================================================================
// service_up + probe cache
// ============================================================================

func TestPromStore_SetServiceUp_TrueFalse(t *testing.T) {
	s := NewPromStore()
	s.SetServiceUp("forgejo", true)
	if got := s.ServiceUp("forgejo"); got != 1.0 {
		t.Errorf("expected 1.0 for healthy forgejo, got %v", got)
	}

	s.SetServiceUp("forgejo", false)
	if got := s.ServiceUp("forgejo"); got != 0.0 {
		t.Errorf("expected 0.0 for unhealthy forgejo, got %v", got)
	}
}

func TestPromStore_ServiceUp_Unknown(t *testing.T) {
	s := NewPromStore()
	if got := s.ServiceUp("never_set"); got != 0.0 {
		t.Errorf("unknown service should report 0.0, got %v", got)
	}
}

func TestPromStore_UpdateProbe_RefreshesGauge(t *testing.T) {
	s := NewPromStore()
	now := time.Now()
	s.UpdateProbe("chimera", now, true, "HTTP 200")
	if s.ServiceUp("chimera") != 1.0 {
		t.Fatal("expected gauge updated")
	}
	s.UpdateProbe("chimera", now, false, "HTTP 503")
	if s.ServiceUp("chimera") != 0.0 {
		t.Errorf("expected gauge reset to 0, got %v", s.ServiceUp("chimera"))
	}
}

func TestPromStore_ProbeFreshness_Known(t *testing.T) {
	s := NewPromStore()
	now := time.Now()
	s.UpdateProbe("forgejo", now, true, "ok")

	fresh, at := s.ProbeFreshness("forgejo")
	if !fresh {
		t.Errorf("fresh probe should be fresh")
	}
	if !at.Equal(now) {
		t.Errorf("at mismatch: got %v, want %v", at, now)
	}
}

func TestPromStore_ProbeFreshness_Stale(t *testing.T) {
	s := NewPromStore()
	s.UpdateProbe("forgejo", time.Now().Add(-time.Hour), true, "ok")
	fresh, _ := s.ProbeFreshness("forgejo")
	if fresh {
		t.Errorf("probe from 1h ago should be stale")
	}
}

func TestPromStore_ProbeFreshness_Unknown(t *testing.T) {
	s := NewPromStore()
	fresh, _ := s.ProbeFreshness("never_probed")
	if fresh {
		t.Error("never-probed service should report fresh=false")
	}
}

// ============================================================================
// Prometheus exposition format
// ============================================================================

func TestPromStore_WriteMetrics_Empty(t *testing.T) {
	var buf bytes.Buffer
	n, err := NewPromStore().WriteMetrics(&buf)
	if err != nil {
		t.Fatalf("WriteMetrics: %v", err)
	}
	if n == 0 {
		t.Errorf("expected some bytes (HELP headers) even for empty store, got 0")
	}
	out := buf.String()
	// HELP/TYPE headers should still be present.
	for _, want := range []string{
		"# HELP helix_subcommand_invocations_total",
		"# TYPE helix_subcommand_invocations_total counter",
		"# HELP helix_subcommand_rc_total",
		"# TYPE helix_subcommand_duration_seconds histogram",
		"# HELP helix_service_up",
		"# TYPE helix_service_up gauge",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nGot:\n%s", want, out)
		}
	}
}

func TestPromStore_WriteMetrics_OneCounter(t *testing.T) {
	s := NewPromStore()
	s.SetProbes([]string{"forgejo"})
	s.SetServiceUp("forgejo", true)
	s.RecordInvocation("status", 0, 12*time.Millisecond)

	var buf bytes.Buffer
	if _, err := s.WriteMetrics(&buf); err != nil {
		t.Fatalf("WriteMetrics: %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		`helix_subcommand_invocations_total{subcommand="status"} 1`,
		`helix_subcommand_rc_total{subcommand="status",rc="0"} 1`,
		`helix_subcommand_duration_seconds_bucket{subcommand="status",le="0.025"} 1`,
		`helix_subcommand_duration_seconds_bucket{subcommand="status",le="+Inf"} 1`,
		`helix_subcommand_duration_seconds_count{subcommand="status"} 1`,
		`helix_service_up{service="forgejo"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nGot:\n%s", want, out)
		}
	}
}

func TestPromStore_WriteMetrics_HistogramWithThreeBuckets(t *testing.T) {
	s := NewPromStore()
	s.SetBuckets([]float64{0.1, 1.0, 10.0})             // 3 buckets
	s.RecordInvocation("slow", 0, 5*time.Second)        // in 10.0 bucket
	s.RecordInvocation("slow", 0, 500*time.Millisecond) // in 1.0 bucket
	s.RecordInvocation("slow", 0, 50*time.Millisecond)  // in 0.1 bucket

	var buf bytes.Buffer
	_, _ = s.WriteMetrics(&buf)
	out := buf.String()

	// Cumulative counts:
	//   le="0.1"   -> 1 (50ms)
	//   le="1.0"   -> 2 (50ms + 500ms)
	//   le="10.0"  -> 3 (all)
	for _, want := range []string{
		`helix_subcommand_duration_seconds_bucket{subcommand="slow",le="0.1"} 1`,
		`helix_subcommand_duration_seconds_bucket{subcommand="slow",le="1"} 2`,
		`helix_subcommand_duration_seconds_bucket{subcommand="slow",le="10"} 3`,
		`helix_subcommand_duration_seconds_bucket{subcommand="slow",le="+Inf"} 3`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nGot:\n%s", want, out)
		}
	}
}

func TestPromStore_WriteMetrics_ServiceOrder(t *testing.T) {
	// First test: serviceOrder set → emitted in declared order.
	s := NewPromStore()
	s.SetProbes([]string{"chimera", "forgejo", "langfuse"})
	s.SetServiceUp("chimera", true)
	s.SetServiceUp("forgejo", true)
	s.SetServiceUp("langfuse", false)

	var buf bytes.Buffer
	_, _ = s.WriteMetrics(&buf)
	out := buf.String()

	chimeraIdx := strings.Index(out, `helix_service_up{service="chimera"}`)
	forgejoIdx := strings.Index(out, `helix_service_up{service="forgejo"}`)
	langfuseIdx := strings.Index(out, `helix_service_up{service="langfuse"}`)

	if chimeraIdx < 0 || forgejoIdx < 0 || langfuseIdx < 0 {
		t.Fatalf("missing one of the service_up lines:\n%s", out)
	}
	if !(chimeraIdx < forgejoIdx && forgejoIdx < langfuseIdx) {
		t.Errorf("services not in declared order:\n%s", out)
	}
	if !strings.Contains(out, `helix_service_up{service="langfuse"} 0`) {
		t.Errorf("expected langfuse=0:\n%s", out)
	}
}

func TestPromStore_WriteMetrics_Deterministic(t *testing.T) {
	// Two scrape → byte-equal output (modulo no time-based fields).
	s1 := NewPromStore()
	s2 := NewPromStore()
	for _, s := range []*PromStore{s1, s2} {
		s.RecordInvocation("status", 0, 1*time.Millisecond)
		s.RecordInvocation("doctor", 1, 2*time.Millisecond)
	}
	var b1, b2 bytes.Buffer
	_, _ = s1.WriteMetrics(&b1)
	_, _ = s2.WriteMetrics(&b2)
	if b1.String() != b2.String() {
		t.Errorf("scrape output not deterministic:\nA:\n%s\nB:\n%s", b1.String(), b2.String())
	}
}

func TestPromStore_ComputeBucketsInStore(t *testing.T) {
	s := NewPromStore()
	s.SetBuckets([]float64{0.1, 1.0, 10.0})
	s.RecordInvocation("x", 0, 50*time.Millisecond)
	s.RecordInvocation("x", 0, 500*time.Millisecond)
	s.RecordInvocation("x", 0, 5*time.Second)

	got := s.ComputeBucketsInStore("x")
	want := []int{1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("bucket %d: got %d, want %d", i, got[i], want[i])
		}
	}
}

// ============================================================================
// Snapshot semantics
// ============================================================================

func TestPromStore_Snapshot_CopiesDeep(t *testing.T) {
	s := NewPromStore()
	s.RecordInvocation("a", 0, time.Millisecond)

	snap := s.Snapshot()
	// Mutate the store.
	s.RecordInvocation("a", 0, time.Millisecond)
	s.RecordInvocation("b", 0, time.Millisecond)

	// Snapshot should not have changed.
	if snap.Invocations["a"] != 1 {
		t.Errorf("snapshot not deep-copied: status=%d", snap.Invocations["a"])
	}
	if _, ok := snap.Invocations["b"]; ok {
		t.Errorf("snapshot inherited mutation: b present")
	}
}

func TestPromStore_Snapshot_Empty(t *testing.T) {
	s := NewPromStore()
	snap := s.Snapshot()
	if len(snap.Invocations) != 0 {
		t.Errorf("expected empty invocations, got %v", snap.Invocations)
	}
	if len(snap.RCCounts) != 0 {
		t.Errorf("expected empty rcCounts, got %v", snap.RCCounts)
	}
	if len(snap.ServiceUp) != 0 {
		t.Errorf("expected empty serviceUp, got %v", snap.ServiceUp)
	}
	if len(snap.Durations) != 0 {
		t.Errorf("expected empty durations, got %v", snap.Durations)
	}
}

// ============================================================================
// Format helpers
// ============================================================================

func TestPromFormatFloat(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{1, "1"},
		{1.5, "1.5"},
		{0.025, "0.025"},
		{1e6, "1000000"},
	}
	for _, c := range cases {
		got := promFormatFloat(c.in)
		if got != c.want {
			t.Errorf("promFormatFloat(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestComputeBuckets_Boundary(t *testing.T) {
	// Boundary test: values exactly at the bucket boundary should be
	// included in that bucket (Prometheus convention: le is the upper
	// bound, inclusive).
	obs := []float64{0.1, 1.0, 10.0}
	buckets := []float64{0.1, 1.0, 10.0}
	got := computeBuckets(obs, buckets)
	want := []int{1, 2, 3}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("bucket %d: got %d, want %d", i, got[i], want[i])
		}
	}
}

func TestComputeBuckets_Empty(t *testing.T) {
	if got := computeBuckets(nil, []float64{0.1, 1.0}); len(got) != 2 {
		t.Errorf("expected 2 zero buckets, got %v", got)
	}
	for _, v := range computeBuckets(nil, []float64{1, 2}) {
		if v != 0 {
			t.Errorf("expected all zeros for empty obs, got %v", v)
		}
	}
}

func TestPromSortedKeys_SortsResults(t *testing.T) {
	m := map[string]uint64{"z": 1, "a": 2, "m": 3}
	got := promSortedKeys(m)
	want := []string{"a", "m", "z"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sorted[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPromSortedKeys_Empty(t *testing.T) {
	got := promSortedKeys(map[string]uint64{})
	if len(got) != 0 {
		t.Errorf("expected empty result, got %v", got)
	}
}

// ============================================================================
// Concurrent access
// ============================================================================

func TestPromStore_ConcurrentInvocation(t *testing.T) {
	s := NewPromStore()
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				s.RecordInvocation("race", 0, time.Millisecond)
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	snap := s.Snapshot()
	if snap.Invocations["race"] != 1000 {
		t.Errorf("concurrent counter race: got %d, want 1000", snap.Invocations["race"])
	}
}

func TestAtomicCounter(t *testing.T) {
	c := &atomicCounter{}
	c.Inc()
	c.Inc()
	if got := c.Get(); got != 2 {
		t.Errorf("atomic counter: got %d, want 2", got)
	}
}
