package health

import (
	"strings"
	"testing"
)

func TestNewPlatformMetricsCollector(t *testing.T) {
	p := NewPlatformMetricsCollector()
	if p.SourceCount() != 0 {
		t.Errorf("SourceCount = %d, want 0", p.SourceCount())
	}
}

func TestPlatformMetrics_RegisterSource(t *testing.T) {
	p := NewPlatformMetricsCollector()
	p.RegisterSource(NewStubMetricsSource("test", []MetricLine{
		{Name: "helix_test_total", Type: "counter", Value: 1},
	}))
	if p.SourceCount() != 1 {
		t.Errorf("SourceCount = %d, want 1", p.SourceCount())
	}
}

func TestPlatformMetrics_Collect_Empty(t *testing.T) {
	p := NewPlatformMetricsCollector()
	output := p.Collect()
	if output != "" {
		t.Errorf("Collect() = %q, want empty", output)
	}
}

func TestPlatformMetrics_Collect_SingleSource(t *testing.T) {
	p := NewPlatformMetricsCollector()
	p.RegisterSource(NewStubMetricsSource("trust", []MetricLine{
		{Name: "helix_trust_score", Help: "Trust score", Type: "gauge",
			Labels: map[string]string{"agent": "a1"}, Value: 85},
		{Name: "helix_trust_tier_total", Help: "Tier counts", Type: "gauge",
			Labels: map[string]string{"tier": "provisional"}, Value: 5},
	}))
	output := p.Collect()
	if !strings.Contains(output, "helix_trust_score") {
		t.Error("missing trust score metric")
	}
	if !strings.Contains(output, "helix_trust_tier_total") {
		t.Error("missing tier total metric")
	}
	if !strings.Contains(output, "# HELP helix_trust_score Trust score") {
		t.Error("missing HELP header")
	}
	if !strings.Contains(output, "# TYPE helix_trust_score gauge") {
		t.Error("missing TYPE header")
	}
	if !strings.Contains(output, `helix_trust_score{agent="a1"} 85`) {
		t.Error("missing formatted metric line")
	}
}

func TestPlatformMetrics_Collect_MultipleSources(t *testing.T) {
	p := NewPlatformMetricsCollector()
	p.RegisterSource(NewStubMetricsSource("review", []MetricLine{
		{Name: "helix_reviews_total", Type: "counter", Value: 100},
	}))
	p.RegisterSource(NewStubMetricsSource("estimate", []MetricLine{
		{Name: "helix_estimates_total", Type: "counter", Value: 50},
	}))
	output := p.Collect()
	if !strings.Contains(output, "helix_reviews_total") {
		t.Error("missing reviews metric")
	}
	if !strings.Contains(output, "helix_estimates_total") {
		t.Error("missing estimates metric")
	}
}

func TestPlatformMetrics_Collect_DeterministicOrdering(t *testing.T) {
	lines := []MetricLine{
		{Name: "helix_z_metric", Type: "gauge", Value: 1},
		{Name: "helix_a_metric", Type: "gauge", Value: 2},
		{Name: "helix_m_metric", Type: "gauge", Value: 3},
	}
	p := NewPlatformMetricsCollector()
	p.RegisterSource(NewStubMetricsSource("test", lines))

	output1 := p.Collect()
	output2 := p.Collect()
	if output1 != output2 {
		t.Error("non-deterministic ordering across Collect calls")
	}
	// Verify sorted: a before m before z
	idxA := strings.Index(output1, "helix_a_metric")
	idxM := strings.Index(output1, "helix_m_metric")
	idxZ := strings.Index(output1, "helix_z_metric")
	if idxA >= idxM || idxM >= idxZ {
		t.Error("metrics not sorted alphabetically")
	}
}

func TestPlatformMetrics_Collect_LabelSorting(t *testing.T) {
	p := NewPlatformMetricsCollector()
	p.RegisterSource(NewStubMetricsSource("test", []MetricLine{
		{Name: "helix_metric", Type: "gauge",
			Labels: map[string]string{"zebra": "z", "apple": "a", "mango": "m"}, Value: 1},
	}))
	output := p.Collect()
	// Labels should be sorted: apple before mango before zebra
	appleIdx := strings.Index(output, "apple")
	mangoIdx := strings.Index(output, "mango")
	zebraIdx := strings.Index(output, "zebra")
	if appleIdx >= mangoIdx || mangoIdx >= zebraIdx {
		t.Error("labels not sorted alphabetically")
	}
}

func TestPlatformMetrics_Collect_InternalCounters(t *testing.T) {
	p := NewPlatformMetricsCollector()
	p.RecordCounter("helix_platform_requests_total")
	p.RecordCounter("helix_platform_requests_total")
	p.RecordCounter("helix_platform_errors_total")
	output := p.Collect()
	if !strings.Contains(output, "helix_platform_requests_total 2") {
		t.Error("missing counter with value 2")
	}
	if !strings.Contains(output, "helix_platform_errors_total 1") {
		t.Error("missing counter with value 1")
	}
}

func TestPlatformMetrics_Collect_CounterValue(t *testing.T) {
	p := NewPlatformMetricsCollector()
	p.RecordCounterValue("helix_tokens_total", 5000)
	p.RecordCounterValue("helix_tokens_total", 3000)
	output := p.Collect()
	if !strings.Contains(output, "helix_tokens_total 8000") {
		t.Error("missing summed counter value")
	}
}

func TestPlatformMetrics_Collect_MixedSourcesAndCounters(t *testing.T) {
	p := NewPlatformMetricsCollector()
	p.RegisterSource(NewStubMetricsSource("marketplace", []MetricLine{
		{Name: "helix_marketplace_agents_total", Type: "gauge",
			Labels: map[string]string{"status": "active"}, Value: 10},
	}))
	p.RecordCounter("helix_negotiations_total")
	output := p.Collect()
	if !strings.Contains(output, "helix_marketplace_agents_total") {
		t.Error("missing source metric")
	}
	if !strings.Contains(output, "helix_negotiations_total") {
		t.Error("missing internal counter")
	}
}

func TestPlatformMetrics_Collect_NoLabels(t *testing.T) {
	p := NewPlatformMetricsCollector()
	p.RegisterSource(NewStubMetricsSource("test", []MetricLine{
		{Name: "helix_simple_total", Type: "counter", Value: 42},
	}))
	output := p.Collect()
	if !strings.Contains(output, "helix_simple_total 42") {
		t.Errorf("missing unlabeled metric line, got: %s", output)
	}
}

func TestPlatformMetrics_Collect_FloatValue(t *testing.T) {
	p := NewPlatformMetricsCollector()
	p.RegisterSource(NewStubMetricsSource("test", []MetricLine{
		{Name: "helix_avg_latency", Type: "gauge", Value: 99.95},
	}))
	output := p.Collect()
	if !strings.Contains(output, "helix_avg_latency 99.95") {
		t.Errorf("missing float value, got: %s", output)
	}
}

func TestPlatformMetrics_LastCollectTime(t *testing.T) {
	p := NewPlatformMetricsCollector()
	if !p.LastCollectTime().IsZero() {
		t.Error("lastCollect should be zero before first Collect")
	}
	p.Collect()
	if p.LastCollectTime().IsZero() {
		t.Error("lastCollect should be non-zero after Collect")
	}
}

func TestPlatformMetrics_Summary(t *testing.T) {
	p := NewPlatformMetricsCollector()
	p.RegisterSource(NewStubMetricsSource("trust", nil))
	p.RegisterSource(NewStubMetricsSource("review", nil))
	p.RecordCounter("helix_test_counter")
	summary := p.Summary()
	if !strings.Contains(summary, "2") {
		t.Error("summary missing source count")
	}
	if !strings.Contains(summary, "1") {
		t.Error("summary missing counter count")
	}
	if !strings.Contains(summary, "helix_test_counter") {
		t.Error("summary missing counter name")
	}
}

func TestFormatMetricLine_WithLabels(t *testing.T) {
	line := MetricLine{
		Name:   "helix_test",
		Labels: map[string]string{"a": "1", "b": "2"},
		Value:  10,
	}
	result := formatMetricLine(line)
	if !strings.Contains(result, `helix_test{`) {
		t.Errorf("unexpected format: %s", result)
	}
	if !strings.Contains(result, `a="1"`) {
		t.Errorf("missing label a: %s", result)
	}
	if !strings.Contains(result, `b="2"`) {
		t.Errorf("missing label b: %s", result)
	}
	if !strings.Contains(result, " 10") {
		t.Errorf("missing value: %s", result)
	}
}

func TestFormatMetricLine_NoLabels(t *testing.T) {
	line := MetricLine{
		Name:  "helix_simple",
		Value: 5,
	}
	result := formatMetricLine(line)
	expected := "helix_simple 5"
	if result != expected {
		t.Errorf("formatMetricLine = %q, want %q", result, expected)
	}
}

func TestFormatValue_Integer(t *testing.T) {
	if formatValue(42.0) != "42" {
		t.Errorf("formatValue(42.0) = %q", formatValue(42.0))
	}
}

func TestFormatValue_Float(t *testing.T) {
	if formatValue(99.95) != "99.95" {
		t.Errorf("formatValue(99.95) = %q", formatValue(99.95))
	}
}

func TestLabelKey_Empty(t *testing.T) {
	if labelKey(nil) != "" {
		t.Errorf("labelKey(nil) = %q, want empty", labelKey(nil))
	}
}

func TestLabelKey_Sorted(t *testing.T) {
	result := labelKey(map[string]string{"b": "2", "a": "1"})
	if result != "a=1,b=2" {
		t.Errorf("labelKey = %q, want a=1,b=2", result)
	}
}

func TestStubMetricsSource(t *testing.T) {
	lines := []MetricLine{{Name: "test", Value: 1}}
	s := NewStubMetricsSource("stub", lines)
	if s.MetricsName() != "stub" {
		t.Errorf("MetricsName = %q", s.MetricsName())
	}
	if len(s.CollectMetrics()) != 1 {
		t.Errorf("CollectMetrics len = %d", len(s.CollectMetrics()))
	}
}

func TestPlatformMetrics_Collect_DeduplicatesHeaders(t *testing.T) {
	p := NewPlatformMetricsCollector()
	// Two sources emitting the same metric name
	p.RegisterSource(NewStubMetricsSource("a", []MetricLine{
		{Name: "helix_shared", Help: "Shared metric", Type: "gauge",
			Labels: map[string]string{"src": "a"}, Value: 1},
	}))
	p.RegisterSource(NewStubMetricsSource("b", []MetricLine{
		{Name: "helix_shared", Help: "Shared metric", Type: "gauge",
			Labels: map[string]string{"src": "b"}, Value: 2},
	}))
	output := p.Collect()
	// HELP/TYPE should appear only once for helix_shared
	helpCount := strings.Count(output, "# HELP helix_shared")
	if helpCount != 1 {
		t.Errorf("HELP header count = %d, want 1", helpCount)
	}
	typeCount := strings.Count(output, "# TYPE helix_shared")
	if typeCount != 1 {
		t.Errorf("TYPE header count = %d, want 1", typeCount)
	}
}

func TestPlatformMetrics_Collect_LargeMetricSet(t *testing.T) {
	var lines []MetricLine
	for i := 0; i < 100; i++ {
		lines = append(lines, MetricLine{
			Name:   "helix_metric",
			Type:   "gauge",
			Labels: map[string]string{"id": string(rune('a' + i%26))},
			Value:  float64(i),
		})
	}
	p := NewPlatformMetricsCollector()
	p.RegisterSource(NewStubMetricsSource("large", lines))
	output := p.Collect()
	// Should contain all 100 metric lines
	metricCount := strings.Count(output, "helix_metric")
	if metricCount < 100 {
		t.Errorf("metric line count = %d, want >= 100", metricCount)
	}
}
