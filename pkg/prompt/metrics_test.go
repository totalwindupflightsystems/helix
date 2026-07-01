package prompt

import (
	"strings"
	"testing"
)

func TestMetricsCollector_AttestationAndFailures(t *testing.T) {
	mc := NewMetricsCollector()

	mc.RecordAttestation()
	mc.RecordAttestation()
	mc.RecordAttestation()
	mc.RecordAttestationFailure("missing")
	mc.RecordAttestationFailure("missing")
	mc.RecordAttestationFailure("tamper")

	if mc.AttestationTotal() != 3 {
		t.Errorf("AttestationTotal = %d, want 3", mc.AttestationTotal())
	}

	failures := mc.AttestationFailures()
	if failures["missing"] != 2 {
		t.Errorf("failures[missing] = %d, want 2", failures["missing"])
	}
	if failures["tamper"] != 1 {
		t.Errorf("failures[tamper] = %d, want 1", failures["tamper"])
	}
}

func TestMetricsCollector_Overrides(t *testing.T) {
	mc := NewMetricsCollector()
	mc.RecordOverride()
	mc.RecordOverride()

	if mc.OverridesTotal() != 2 {
		t.Errorf("OverridesTotal = %d, want 2", mc.OverridesTotal())
	}
}

func TestMetricsCollector_SetStatusCounts(t *testing.T) {
	mc := NewMetricsCollector()
	mc.SetStatusCounts(map[LifecycleStatus]int{
		StatusActive:     10,
		StatusDeprecated: 2,
		StatusDraft:      3,
	})

	counts := mc.StatusCounts()
	if counts[StatusActive] != 10 {
		t.Errorf("StatusCounts[active] = %d, want 10", counts[StatusActive])
	}
	if counts[StatusDeprecated] != 2 {
		t.Errorf("StatusCounts[deprecated] = %d, want 2", counts[StatusDeprecated])
	}
}

func TestMetricsCollector_SetComponentVersions(t *testing.T) {
	mc := NewMetricsCollector()
	mc.SetComponentVersions(map[string]int{
		"agent-identity": 3,
		"cost-estimator": 2,
	})

	counts := mc.ComponentVersionCounts()
	if counts["agent-identity"] != 3 {
		t.Errorf("componentVersionCount[agent-identity] = %d, want 3", counts["agent-identity"])
	}
}

func TestMetricsCollector_UpdateFromIndex(t *testing.T) {
	mc := NewMetricsCollector()
	idx := Index{
		"agent-identity": {
			"v1": &PromptEntry{Hash: "h1", Status: StatusActive},
			"v2": &PromptEntry{Hash: "h2", Status: StatusActive},
			"v3": &PromptEntry{Hash: "h3", Status: StatusDeprecated},
		},
		"cost-estimator": {
			"v1": &PromptEntry{Hash: "h4", Status: StatusDraft},
		},
	}

	mc.UpdateFromIndex(idx)

	counts := mc.StatusCounts()
	if counts[StatusActive] != 2 {
		t.Errorf("statusCounts[active] = %d, want 2", counts[StatusActive])
	}
	if counts[StatusDeprecated] != 1 {
		t.Errorf("statusCounts[deprecated] = %d, want 1", counts[StatusDeprecated])
	}
	if counts[StatusDraft] != 1 {
		t.Errorf("statusCounts[draft] = %d, want 1", counts[StatusDraft])
	}

	compCounts := mc.ComponentVersionCounts()
	if compCounts["agent-identity"] != 3 {
		t.Errorf("componentVersionCount[agent-identity] = %d, want 3", compCounts["agent-identity"])
	}
	if compCounts["cost-estimator"] != 1 {
		t.Errorf("componentVersionCount[cost-estimator] = %d, want 1", compCounts["cost-estimator"])
	}
}

func TestMetricsCollector_UpdateFromIndex_NilEntries(t *testing.T) {
	mc := NewMetricsCollector()
	idx := Index{
		"comp": {
			"v1": nil,
			"v2": &PromptEntry{Hash: "h2", Status: StatusActive},
		},
	}
	mc.UpdateFromIndex(idx)

	counts := mc.StatusCounts()
	if counts[StatusActive] != 1 {
		t.Errorf("statusCounts[active] = %d, want 1 (nil entry should be skipped)", counts[StatusActive])
	}
}

func TestMetricsCollector_Collect(t *testing.T) {
	mc := NewMetricsCollector()
	mc.RecordAttestation()
	mc.RecordAttestationFailure("missing")
	mc.RecordOverride()
	mc.SetStatusCounts(map[LifecycleStatus]int{StatusActive: 5})
	mc.SetComponentVersions(map[string]int{"agent-identity": 3})

	output := mc.Collect()

	// Check HELP/TYPE headers
	if !strings.Contains(output, "# HELP helix_prompts_total") {
		t.Error("missing helix_prompts_total HELP header")
	}
	if !strings.Contains(output, "# TYPE helix_prompts_total gauge") {
		t.Error("missing helix_prompts_total TYPE header")
	}
	if !strings.Contains(output, "# HELP helix_prompt_attestations_total") {
		t.Error("missing attestations HELP header")
	}
	if !strings.Contains(output, "# TYPE helix_prompt_attestations_total counter") {
		t.Error("missing attestations TYPE header")
	}
	if !strings.Contains(output, "# HELP helix_prompt_overrides_total") {
		t.Error("missing overrides HELP header")
	}

	// Check metric values
	if !strings.Contains(output, "helix_prompts_total{status=\"active\"} 5") {
		t.Error("missing prompts_total metric line")
	}
	if !strings.Contains(output, "helix_prompt_attestations_total 1") {
		t.Error("missing attestations metric line")
	}
	if !strings.Contains(output, "helix_prompt_attestation_failures_total{reason=\"missing\"} 1") {
		t.Error("missing failures metric line")
	}
	if !strings.Contains(output, "helix_prompt_versions_total{component=\"agent-identity\"} 3") {
		t.Error("missing versions metric line")
	}
	if !strings.Contains(output, "helix_prompt_overrides_total 1") {
		t.Error("missing overrides metric line")
	}
}

func TestMetricsCollector_Collect_DeterministicOrdering(t *testing.T) {
	mc := NewMetricsCollector()
	mc.SetStatusCounts(map[LifecycleStatus]int{
		StatusActive:     3,
		StatusDeprecated: 1,
		StatusDraft:      5,
		StatusRetired:    2,
	})
	mc.SetComponentVersions(map[string]int{
		"zebra":  1,
		"alpha":  2,
		"monkey": 3,
	})

	output1 := mc.Collect()
	output2 := mc.Collect()

	if output1 != output2 {
		t.Error("Collect output is not deterministic")
	}

	// Check alphabetical ordering of components
	alphaIdx := strings.Index(output1, "component=\"alpha\"")
	monkeyIdx := strings.Index(output1, "component=\"monkey\"")
	zebraIdx := strings.Index(output1, "component=\"zebra\"")
	if alphaIdx >= monkeyIdx || monkeyIdx >= zebraIdx {
		t.Error("components not alphabetically sorted")
	}
}

func TestMetricsCollector_Collect_Empty(t *testing.T) {
	mc := NewMetricsCollector()
	output := mc.Collect()

	if !strings.Contains(output, "# HELP helix_prompts_total") {
		t.Error("missing headers even when empty")
	}
	if !strings.Contains(output, "helix_prompt_attestations_total 0") {
		t.Error("missing zero attestation counter")
	}
}

func TestMetricsCollector_Reset(t *testing.T) {
	mc := NewMetricsCollector()
	mc.RecordAttestation()
	mc.RecordAttestationFailure("test")
	mc.RecordOverride()
	mc.SetStatusCounts(map[LifecycleStatus]int{StatusActive: 5})

	mc.Reset()

	if mc.AttestationTotal() != 0 {
		t.Error("attestation total not reset")
	}
	if mc.OverridesTotal() != 0 {
		t.Error("overrides not reset")
	}
	if len(mc.AttestationFailures()) != 0 {
		t.Error("failures not reset")
	}
	if len(mc.StatusCounts()) != 0 {
		t.Error("status counts not reset")
	}
}

func TestMetricsCollector_Concurrent(t *testing.T) {
	mc := NewMetricsCollector()

	done := make(chan bool, 2)
	go func() {
		for i := 0; i < 100; i++ {
			mc.RecordAttestation()
		}
		done <- true
	}()
	go func() {
		for i := 0; i < 50; i++ {
			mc.RecordAttestationFailure("test")
		}
		done <- true
	}()
	<-done
	<-done

	if mc.AttestationTotal() != 100 {
		t.Errorf("AttestationTotal = %d, want 100", mc.AttestationTotal())
	}
	failures := mc.AttestationFailures()
	if failures["test"] != 50 {
		t.Errorf("failures[test] = %d, want 50", failures["test"])
	}
}
