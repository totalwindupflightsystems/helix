package recovery

import (
	"strings"
	"testing"
	"time"
)

func TestSeverityDescription(t *testing.T) {
	tests := []struct {
		sev      Severity
		contains string
	}{
		{SEV1, "Platform unavailable"},
		{SEV2, "Degraded service"},
		{SEV3, "Non-critical"},
		{Severity("INVALID"), "Unknown"},
	}
	for _, tt := range tests {
		desc := SeverityDescription(tt.sev)
		if !strings.Contains(desc, tt.contains) {
			t.Errorf("SeverityDescription(%s) = %q, want substring %q", tt.sev, desc, tt.contains)
		}
	}
}

func TestNewRecoveryRegistry_Count(t *testing.T) {
	r := NewRecoveryRegistry()
	if r.Count() < 14 {
		t.Errorf("NewRecoveryRegistry().Count() = %d, want >= 14 (spec §14.1 has 14+ entries)", r.Count())
	}
}

func TestNewRecoveryRegistry_All(t *testing.T) {
	r := NewRecoveryRegistry()
	all := r.All()
	if len(all) != r.Count() {
		t.Errorf("All() returned %d entries, Count() = %d", len(all), r.Count())
	}
	// Verify All returns a copy, not internal slice
	original := r.All()
	original[0].Component = "MODIFIED"
	if r.All()[0].Component == "MODIFIED" {
		t.Error("All() returned a reference to internal slice, not a copy")
	}
}

func TestNewRecoveryRegistryWithEntries(t *testing.T) {
	custom := []FailureEntry{
		{Component: "TestComp", FailureMode: "test mode"},
	}
	r := NewRecoveryRegistryWithEntries(custom)
	if r.Count() != 1 {
		t.Errorf("Count() = %d, want 1", r.Count())
	}
	if r.All()[0].Component != "TestComp" {
		t.Errorf("entry component = %q, want %q", r.All()[0].Component, "TestComp")
	}
}

func TestRecoveryRegistry_LookupByComponent(t *testing.T) {
	r := NewRecoveryRegistry()

	tests := []struct {
		component string
		minCount  int
	}{
		{"Forgejo", 3},
		{"Chimera", 2},
		{"Hivemind", 2},
		{"LangFuse", 2},
		{"NonExistent", 0},
	}
	for _, tt := range tests {
		entries := r.LookupByComponent(tt.component)
		if len(entries) < tt.minCount {
			t.Errorf("LookupByComponent(%q) returned %d entries, want >= %d",
				tt.component, len(entries), tt.minCount)
		}
	}
}

func TestRecoveryRegistry_LookupByComponent_CaseInsensitive(t *testing.T) {
	r := NewRecoveryRegistry()

	lower := r.LookupByComponent("forgejo")
	upper := r.LookupByComponent("FORGEJO")
	mixed := r.LookupByComponent("Forgejo")

	if len(lower) != len(upper) || len(lower) != len(mixed) {
		t.Errorf("case insensitive lookup mismatch: lower=%d upper=%d mixed=%d",
			len(lower), len(upper), len(mixed))
	}
}

func TestRecoveryRegistry_LookupByFailureMode(t *testing.T) {
	r := NewRecoveryRegistry()

	tests := []struct {
		mode     string
		minCount int
	}{
		{"crash", 4},
		{"corruption", 2},
		{"timeout", 3},
		{"nonexistent failure mode", 0},
	}
	for _, tt := range tests {
		entries := r.LookupByFailureMode(tt.mode)
		if len(entries) < tt.minCount {
			t.Errorf("LookupByFailureMode(%q) returned %d entries, want >= %d",
				tt.mode, len(entries), tt.minCount)
		}
	}
}

func TestRecoveryRegistry_LookupByID(t *testing.T) {
	r := NewRecoveryRegistry()

	tests := []struct {
		id    string
		found bool
		comp  string
	}{
		{"FJ-001", true, "Forgejo"},
		{"FJ-002", true, "Forgejo"},
		{"CH-001", true, "Chimera"},
		{"CH-002", true, "Chimera"},
		{"CH-003", true, "Chimera"},
		{"INVALID", false, ""},
		{"", false, ""},
	}
	for _, tt := range tests {
		entry := r.LookupByID(tt.id)
		if tt.found {
			if entry == nil {
				t.Errorf("LookupByID(%q) returned nil, want non-nil", tt.id)
				continue
			}
			if entry.Component != tt.comp {
				t.Errorf("LookupByID(%q).Component = %q, want %q", tt.id, entry.Component, tt.comp)
			}
		} else {
			if entry != nil {
				t.Errorf("LookupByID(%q) returned non-nil, want nil", tt.id)
			}
		}
	}
}

func TestRecoveryRegistry_LookupByID_CaseInsensitive(t *testing.T) {
	r := NewRecoveryRegistry()
	lower := r.LookupByID("fj-001")
	if lower == nil {
		t.Error("LookupByID('fj-001') returned nil, expected case-insensitive match")
	}
}

func TestRecoveryRegistry_LookupBySeverity(t *testing.T) {
	r := NewRecoveryRegistry()

	sev1 := r.LookupBySeverity(SEV1)
	if len(sev1) == 0 {
		t.Error("LookupBySeverity(SEV1) returned 0 entries")
	}
	for _, e := range sev1 {
		if e.Severity != SEV1 {
			t.Errorf("SEV1 filter returned entry with severity %q", e.Severity)
		}
	}

	sev2 := r.LookupBySeverity(SEV2)
	if len(sev2) == 0 {
		t.Error("LookupBySeverity(SEV2) returned 0 entries")
	}

	sev3 := r.LookupBySeverity(SEV3)
	if len(sev3) == 0 {
		t.Error("LookupBySeverity(SEV3) returned 0 entries")
	}
}

func TestRecoveryRegistry_Components(t *testing.T) {
	r := NewRecoveryRegistry()
	comps := r.Components()

	if len(comps) == 0 {
		t.Fatal("Components() returned empty list")
	}

	// Check sorted
	for i := 1; i < len(comps); i++ {
		if comps[i] < comps[i-1] {
			t.Errorf("Components() not sorted: %q before %q", comps[i-1], comps[i])
		}
	}

	// Check uniqueness
	seen := make(map[string]bool)
	for _, c := range comps {
		if seen[c] {
			t.Errorf("Components() has duplicate: %q", c)
		}
		seen[c] = true
	}

	// Check expected components exist
	expected := []string{"Forgejo", "Chimera", "Hivemind", "LangFuse"}
	for _, e := range expected {
		if !seen[e] {
			t.Errorf("Components() missing expected component: %q", e)
		}
	}
}

func TestRecoveryRegistry_RecoveryMatrix(t *testing.T) {
	r := NewRecoveryRegistry()
	matrix := r.RecoveryMatrix()

	if len(matrix) == 0 {
		t.Fatal("RecoveryMatrix() returned empty map")
	}

	// Verify matrix keys match Components()
	comps := r.Components()
	for _, c := range comps {
		entries, ok := matrix[c]
		if !ok {
			t.Errorf("RecoveryMatrix() missing component %q", c)
			continue
		}
		if len(entries) == 0 {
			t.Errorf("RecoveryMatrix()[%q] has 0 entries", c)
		}
		// Verify all entries belong to this component
		for _, e := range entries {
			if e.Component != c {
				t.Errorf("RecoveryMatrix()[%q] contains entry for %q", c, e.Component)
			}
		}
	}
}

func TestFormatRunbook(t *testing.T) {
	entry := FailureEntry{
		Component:   "TestComponent",
		FailureMode: "Test failure",
		Detection:   "Test detection",
		Impact:      "Test impact",
		Actions: []RecoveryAction{
			{Description: "Step 1", Command: "echo hello", VerifyCmd: "echo verify", VerifyExpected: "verify"},
			{Description: "Step 2"},
		},
		RTO:      "5 min",
		RPO:      "0",
		Severity: SEV2,
		ErrorID:  "TC-001",
	}

	out := FormatRunbook(entry)

	checks := []string{
		"TC-001",
		"[SEV-2]",
		"TestComponent",
		"Test failure",
		"Test detection",
		"Test impact",
		"5 min",
		"Step 1",
		"echo hello",
		"Verify: echo verify",
		"Step 2",
	}
	for _, c := range checks {
		if !strings.Contains(out, c) {
			t.Errorf("FormatRunbook() missing %q in output:\n%s", c, out)
		}
	}
}

func TestFormatRunbook_NoActions(t *testing.T) {
	entry := FailureEntry{
		Component:   "TestComp",
		FailureMode: "No action failure",
		Severity:    SEV3,
	}
	out := FormatRunbook(entry)
	if !strings.Contains(out, "No action failure") {
		t.Error("FormatRunbook should include failure mode even without actions")
	}
}

func TestFormatMatrix(t *testing.T) {
	r := NewRecoveryRegistry()
	out := FormatMatrix(r)

	if !strings.Contains(out, "Helix Recovery Matrix") {
		t.Error("FormatMatrix missing header")
	}
	if !strings.Contains(out, "Forgejo") {
		t.Error("FormatMatrix missing Forgejo component")
	}
	if !strings.Contains(out, "SEV-1") {
		t.Error("FormatMatrix missing severity tags")
	}
}

func TestFormatMatrix_EmptyRegistry(t *testing.T) {
	r := NewRecoveryRegistryWithEntries(nil)
	out := FormatMatrix(r)
	if !strings.Contains(out, "No recovery procedures") {
		t.Errorf("FormatMatrix on empty registry should say 'No recovery procedures', got: %q", out)
	}
}

func TestFormatMatrix_NilRegistry(t *testing.T) {
	out := FormatMatrix(nil)
	if !strings.Contains(out, "No recovery procedures") {
		t.Errorf("FormatMatrix(nil) should say 'No recovery procedures', got: %q", out)
	}
}

func TestDefaultRetryConfig(t *testing.T) {
	c := DefaultRetryConfig()
	if c.MaxAttempts != 4 {
		t.Errorf("MaxAttempts = %d, want 4", c.MaxAttempts)
	}
	if c.BaseDelay != 2*time.Second {
		t.Errorf("BaseDelay = %v, want 2s", c.BaseDelay)
	}
	if c.MaxDelay != 8*time.Second {
		t.Errorf("MaxDelay = %v, want 8s", c.MaxDelay)
	}
}

func TestRetryConfig_BackoffFor(t *testing.T) {
	c := DefaultRetryConfig()

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 0},                 // immediate
		{1, 2 * time.Second},   // 2^1 * 1s = 2s
		{2, 4 * time.Second},   // 2^2 * 1s = 4s
		{3, 8 * time.Second},   // 2^3 * 1s = 8s (max)
		{4, 8 * time.Second},   // capped at max
		{100, 8 * time.Second}, // still capped
		{-1, 0},                // negative → 0
	}

	for _, tt := range tests {
		got := c.BackoffFor(tt.attempt)
		if got != tt.want {
			t.Errorf("BackoffFor(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestRetryConfig_CustomConfig(t *testing.T) {
	c := RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    500 * time.Millisecond,
	}

	if c.BackoffFor(0) != 0 {
		t.Error("attempt 0 should be immediate")
	}
	if c.BackoffFor(1) != 100*time.Millisecond {
		t.Errorf("attempt 1 = %v, want 100ms", c.BackoffFor(1))
	}
	if c.BackoffFor(2) != 200*time.Millisecond {
		t.Errorf("attempt 2 = %v, want 200ms", c.BackoffFor(2))
	}
	if c.BackoffFor(3) != 400*time.Millisecond {
		t.Errorf("attempt 3 = %v, want 400ms", c.BackoffFor(3))
	}
	// 5th attempt would be 800ms but capped at 500ms
	if c.BackoffFor(5) != 500*time.Millisecond {
		t.Errorf("attempt 5 = %v, want 500ms (capped)", c.BackoffFor(5))
	}
}

// Integration-style test: verify the full spec coverage
func TestRegistry_HasAllSpecErrorIDs(t *testing.T) {
	r := NewRecoveryRegistry()

	expectedIDs := []string{"FJ-001", "FJ-002", "CH-001", "CH-002", "CH-003"}
	for _, id := range expectedIDs {
		entry := r.LookupByID(id)
		if entry == nil {
			t.Errorf("Registry missing spec error ID %q", id)
		}
	}
}

func TestRegistry_ForgejoFailuresAreSEV1OrSEV2(t *testing.T) {
	r := NewRecoveryRegistry()
	forgejoFailures := r.LookupByComponent("Forgejo")
	if len(forgejoFailures) == 0 {
		t.Fatal("No Forgejo failures in registry")
	}
	for _, f := range forgejoFailures {
		if f.Severity != SEV1 && f.Severity != SEV2 {
			t.Errorf("Forgejo failure %q has severity %q, expected SEV-1 or SEV-2",
				f.FailureMode, f.Severity)
		}
	}
}
