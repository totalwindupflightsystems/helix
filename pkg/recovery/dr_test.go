package recovery

import (
	"testing"
	"time"
)

func TestDefaultDRScenarios(t *testing.T) {
	scenarios := DefaultDRScenarios()
	if len(scenarios) != 5 {
		t.Fatalf("DefaultDRScenarios = %d, want 5", len(scenarios))
	}

	ids := make(map[string]bool)
	for _, s := range scenarios {
		ids[s.ID] = true
	}
	expected := []string{DRHardwareFailure, DRDiskFailure, DRAccidentalDeletion, DRSecurityBreach, DRForgejoCorruption}
	for _, id := range expected {
		if !ids[id] {
			t.Errorf("missing scenario: %s", id)
		}
	}
}

func TestDRRegistryGet(t *testing.T) {
	r := NewDRRegistry()

	s, ok := r.Get(DRHardwareFailure)
	if !ok {
		t.Fatal("expected to find dr-hardware-failure")
	}
	if s.Scenario != "Host hardware failure" {
		t.Errorf("Scenario = %s, want 'Host hardware failure'", s.Scenario)
	}
	if s.RTO != "4 hours" {
		t.Errorf("RTO = %s, want '4 hours'", s.RTO)
	}
	if s.RPO != "24 hours" {
		t.Errorf("RPO = %s, want '24 hours'", s.RPO)
	}
	if s.Severity != SEV1 {
		t.Errorf("Severity = %v, want SEV1", s.Severity)
	}
}

func TestDRRegistryGetNotFound(t *testing.T) {
	r := NewDRRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("expected not found for nonexistent ID")
	}
}

func TestDRRegistryAll(t *testing.T) {
	r := NewDRRegistry()
	all := r.All()
	if len(all) != 5 {
		t.Errorf("All() = %d, want 5", len(all))
	}
}

func TestDRRegistryBySeverity(t *testing.T) {
	r := NewDRRegistry()

	sev1 := r.BySeverity(SEV1)
	if len(sev1) != 4 {
		t.Errorf("SEV1 count = %d, want 4 (hardware, disk, security, forgejo)", len(sev1))
	}

	sev2 := r.BySeverity(SEV2)
	if len(sev2) != 1 {
		t.Errorf("SEV2 count = %d, want 1 (accidental deletion)", len(sev2))
	}
}

func TestKeyRotationSteps(t *testing.T) {
	steps := KeyRotationSteps()
	if len(steps) != 5 {
		t.Fatalf("KeyRotationSteps = %d, want 5", len(steps))
	}
	if steps[0] == "" {
		t.Error("first step should not be empty")
	}
	// Verify the key steps are present
	if steps[3] == "" || steps[4] == "" {
		t.Error("token rotation and resume steps should not be empty")
	}
}

func TestFormatDRScenario(t *testing.T) {
	s := DRScenario{
		Scenario:  "Test scenario",
		Detection: "Test detection",
		Response:  "Test response",
		RTO:       "1 hour",
		RPO:       "0",
		Severity:  SEV1,
	}
	out := FormatDRScenario(s)
	if out == "" {
		t.Error("FormatDRScenario returned empty string")
	}
}

func TestDefaultScalingModel(t *testing.T) {
	sm := DefaultScalingModel()
	if sm.MaxConcurrentAgents != 20 {
		t.Errorf("MaxConcurrentAgents = %d, want 20", sm.MaxConcurrentAgents)
	}
	if sm.HostCores != 16 {
		t.Errorf("HostCores = %d, want 16", sm.HostCores)
	}
	if sm.CoresPerAgent != 0.8 {
		t.Errorf("CoresPerAgent = %v, want 0.8", sm.CoresPerAgent)
	}
}

func TestShouldAddHostAgents(t *testing.T) {
	sm := DefaultScalingModel()
	if sm.ShouldAddHost(25, 1*time.Second, 100) {
		// 25 > 20 → should add host
	} else {
		t.Error("ShouldAddHost(25 agents) = false, want true")
	}
	if sm.ShouldAddHost(15, 1*time.Second, 100) {
		t.Error("ShouldAddHost(15 agents) = true, want false")
	}
}

func TestShouldAddHostGitLatency(t *testing.T) {
	sm := DefaultScalingModel()
	if !sm.ShouldAddHost(10, 3*time.Second, 100) {
		t.Error("ShouldAddHost(3s git latency) = false, want true")
	}
	if sm.ShouldAddHost(10, 1*time.Second, 100) {
		t.Error("ShouldAddHost(1s git latency) = true, want false")
	}
}

func TestShouldAddHostPrometheus(t *testing.T) {
	sm := DefaultScalingModel()
	if !sm.ShouldAddHost(10, 1*time.Second, 600) {
		t.Error("ShouldAddHost(600GB Prometheus) = false, want true")
	}
	if sm.ShouldAddHost(10, 1*time.Second, 400) {
		t.Error("ShouldAddHost(400GB Prometheus) = true, want false")
	}
}

func TestMaxAgentsForCores(t *testing.T) {
	if n := MaxAgentsForCores(16, 0.8); n != 20 {
		t.Errorf("MaxAgentsForCores(16, 0.8) = %d, want 20", n)
	}
	if n := MaxAgentsForCores(32, 0.8); n != 40 {
		t.Errorf("MaxAgentsForCores(32, 0.8) = %d, want 40", n)
	}
	if n := MaxAgentsForCores(16, 0); n != 0 {
		t.Errorf("MaxAgentsForCores(16, 0) = %d, want 0", n)
	}
}
