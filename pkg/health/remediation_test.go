package health

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNewRemediationRegistry_Empty(t *testing.T) {
	r := NewRemediationRegistry()
	if got := r.Len(); got != 0 {
		t.Fatalf("expected empty registry, got %d entries", got)
	}
	if got := r.Keys(); len(got) != 0 {
		t.Fatalf("expected empty Keys(), got %v", got)
	}
}

func TestRemediationRegistry_RegisterAndLookup(t *testing.T) {
	r := NewRemediationRegistry()
	r.Register("forgejo_reachable", Remediation{
		Check:    "Forgejo reachable",
		Reason:   "test reason",
		Severity: SeverityHigh,
		Steps:    []Step{{Cmd: "echo hi", Doc: "say hi"}},
	})

	got, ok := r.Lookup("forgejo_reachable")
	if !ok {
		t.Fatal("expected lookup to find registered entry")
	}
	if got.Reason != "test reason" {
		t.Errorf("reason mismatch: %q", got.Reason)
	}
	if got.Severity != SeverityHigh {
		t.Errorf("severity mismatch: %q", got.Severity)
	}
	if len(got.Steps) != 1 || got.Steps[0].Cmd != "echo hi" {
		t.Errorf("steps mismatch: %+v", got.Steps)
	}
	if r.Len() != 1 {
		t.Errorf("Len() = %d, want 1", r.Len())
	}
}

func TestRemediationRegistry_RegisterNilSafe(t *testing.T) {
	// A zero-value registry should still work — defensive check.
	var r RemediationRegistry
	r = *r.Register("anything", Remediation{Check: "anything"})
	if _, ok := r.Lookup("anything"); !ok {
		t.Fatal("register should work even if entries map was nil before")
	}
}

func TestRemediationRegistry_AddIsChainable(t *testing.T) {
	r := NewRemediationRegistry().
		Add("a", Remediation{Check: "a"}).
		Add("b", Remediation{Check: "b"})

	if r.Len() != 2 {
		t.Errorf("expected chained additions to register both, got %d", r.Len())
	}
}

func TestRemediationRegistry_LookupMissing(t *testing.T) {
	r := NewRemediationRegistry()
	_, ok := r.Lookup("nope")
	if ok {
		t.Fatal("expected Lookup to return ok=false for missing entry")
	}
}

func TestRemediationRegistry_LookupOrDefault_KnownCheck(t *testing.T) {
	r := NewRemediationRegistry()
	r.Register("known", Remediation{Check: "known", Reason: "explicit reason"})

	got := r.LookupOrDefault("known", "fallback")
	if got.Reason != "explicit reason" {
		t.Errorf("expected explicit reason, got %q", got.Reason)
	}
}

func TestRemediationRegistry_LookupOrDefault_UnknownCheck(t *testing.T) {
	r := NewRemediationRegistry()
	got := r.LookupOrDefault("never_seen", "fallback reason")
	if got.Reason != "fallback reason" {
		t.Errorf("expected fallback reason, got %q", got.Reason)
	}
	if got.Severity != SeverityMedium {
		t.Errorf("expected medium severity for unknown, got %q", got.Severity)
	}
	if got.Check != "never_seen" {
		t.Errorf("expected check preserved, got %q", got.Check)
	}
	if len(got.Steps) != 1 {
		t.Errorf("expected 1 default step, got %d", len(got.Steps))
	}
	if got.DocURL == "" {
		t.Error("expected non-empty DocURL")
	}
}

func TestRemediationRegistry_Keys_Sorted(t *testing.T) {
	r := NewRemediationRegistry()
	r.Add("z", Remediation{Check: "z"})
	r.Add("a", Remediation{Check: "a"})
	r.Add("m", Remediation{Check: "m"})

	keys := r.Keys()
	want := []string{"a", "m", "z"}
	if len(keys) != len(want) {
		t.Fatalf("expected %d keys, got %d", len(want), len(keys))
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("keys[%d] = %q, want %q", i, keys[i], want[i])
		}
	}
}

func TestDefault_HasAllKnownChecks(t *testing.T) {
	// Reset to ensure the populate path runs fresh.
	ResetDefaultRegistry()
	defer ResetDefaultRegistry()

	reg := Default()
	expected := []string{
		"Forgejo reachable",
		"Chimera healthy",
		"Conscientiousness healthy",
		"Hivemind healthy",
		"LangFuse reachable",
		"Prometheus scraping",
		"Disk usage",
		"Memory",
		"Backup freshness",
	}
	for _, name := range expected {
		_, ok := reg.Lookup(name)
		if !ok {
			t.Errorf("default registry missing check %q", name)
		}
	}
	if reg.Len() != len(expected) {
		t.Errorf("expected %d entries, got %d", len(expected), reg.Len())
	}
}

func TestDefault_IsCached(t *testing.T) {
	// Calling Default() twice should return the same instance
	// (process-global caching).
	a := Default()
	b := Default()
	if a != b {
		t.Error("Default() should return the same cached registry on repeated calls")
	}
}

func TestCheckOutcome_IsFailing(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{"PASS", false},
		{"FAIL", true},
		{"WARN", true},
		{"", false},
		{"unknown", false},
	}
	for _, c := range cases {
		got := CheckOutcome{Status: c.status}.IsFailing()
		if got != c.want {
			t.Errorf("IsFailing(%q) = %v, want %v", c.status, got, c.want)
		}
	}
}

func TestBuildRemediationReport_FailingKnown_ChecksRegistryEntryWithoutCheckField(t *testing.T) {
	// Regression: in production, the populate() chain builds entries
	// like `Add("Forgejo reachable", Remediation{Reason: "...", ...})`
	// without setting Check. Without the BuildRemediationReport
	// override, the rendered block prints "✗  — high" with a blank
	// check name. This test asserts the override restores the lookup
	// key as Check.
	reg := NewRemediationRegistry().
		Add("Forgejo reachable", Remediation{
			// intentionally no Check field set
			Reason:   "base reason",
			Severity: SeverityHigh,
		})

	checks := []CheckOutcome{
		{Name: "Forgejo reachable", Status: "FAIL"},
	}
	rep := BuildRemediationReport(reg, checks)
	if len(rep.Remediations) != 1 {
		t.Fatalf("expected 1 remediation, got %d", len(rep.Remediations))
	}
	got := rep.Remediations[0]
	if got.Check != "Forgejo reachable" {
		t.Errorf("expected rem.Check to be populated from lookup key, got %q", got.Check)
	}
}

func TestBuildRemediationReport_AllPass(t *testing.T) {
	reg := NewRemediationRegistry()
	checks := []CheckOutcome{
		{Name: "Forgejo reachable", Status: "PASS"},
		{Name: "Chimera healthy", Status: "PASS"},
	}
	rep := BuildRemediationReport(reg, checks)
	if rep.HasAny {
		t.Errorf("expected empty remediations for all-pass, got %d", len(rep.Remediations))
	}
	if len(rep.Unknown) != 0 {
		t.Errorf("expected no unknown, got %v", rep.Unknown)
	}
}

func TestBuildRemediationReport_FailingKnown(t *testing.T) {
	reg := NewRemediationRegistry().
		Add("Forgejo reachable", Remediation{
			Check:    "Forgejo reachable",
			Reason:   "base reason",
			Severity: SeverityHigh,
			Steps:    []Step{{Cmd: "echo ok", Doc: "ok"}},
		})

	checks := []CheckOutcome{
		{Name: "Forgejo reachable", Status: "FAIL", Detail: "503 Service Unavailable"},
	}
	rep := BuildRemediationReport(reg, checks)
	if !rep.HasAny {
		t.Fatal("expected at least one remediation")
	}
	if len(rep.Remediations) != 1 {
		t.Fatalf("expected 1 remediation, got %d", len(rep.Remediations))
	}
	got := rep.Remediations[0]
	if got.Check != "Forgejo reachable" {
		t.Errorf("expected Check=Forgejo reachable, got %q", got.Check)
	}
	if !strings.Contains(got.Reason, "503 Service Unavailable") {
		t.Errorf("expected reason to include detail, got %q", got.Reason)
	}
	if !strings.Contains(got.Reason, "base reason") {
		t.Errorf("expected reason to include base reason, got %q", got.Reason)
	}
}

func TestBuildRemediationReport_FailingUnknown(t *testing.T) {
	reg := NewRemediationRegistry()
	checks := []CheckOutcome{
		{Name: "Future check that doesn't exist yet", Status: "FAIL"},
	}
	rep := BuildRemediationReport(reg, checks)
	if rep.HasAny {
		t.Errorf("expected no remediations for unknown check, got %d", len(rep.Remediations))
	}
	if len(rep.Unknown) != 1 {
		t.Fatalf("expected 1 unknown, got %d", len(rep.Unknown))
	}
	if rep.Unknown[0] != "Future check that doesn't exist yet" {
		t.Errorf("unexpected unknown check: %q", rep.Unknown[0])
	}
}

func TestBuildRemediationReport_MixedPassFailUnknown(t *testing.T) {
	reg := NewRemediationRegistry().
		Add("known_fail", Remediation{
			Check:    "known_fail",
			Reason:   "base",
			Severity: SeverityLow,
		})

	checks := []CheckOutcome{
		{Name: "known_pass", Status: "PASS"},
		{Name: "known_fail", Status: "FAIL"},
		{Name: "unknown_fail", Status: "FAIL"},
		{Name: "warn_check", Status: "WARN"},
	}
	rep := BuildRemediationReport(reg, checks)

	// WARN should produce a remediation if the check is known (operators
	// want guidance on warnings too — they're not urgent but actionable).
	if len(rep.Remediations) != 1 {
		t.Errorf("expected 1 remediation (known_fail), got %d", len(rep.Remediations))
	}
	// unknown_fail → Unknown list
	if len(rep.Unknown) != 2 {
		t.Errorf("expected 2 unknown (unknown_fail + warn_check), got %v", rep.Unknown)
	}
	if !rep.HasAny {
		t.Error("expected HasAny=true when known_fail produces remediation")
	}
}

func TestFormatRemediation_BasicStructure(t *testing.T) {
	var buf bytes.Buffer
	rem := Remediation{
		Check:    "Forgejo reachable",
		Severity: SeverityHigh,
		Reason:   "test reason",
		DocURL:   "specs/foo.md",
		Steps: []Step{
			{Cmd: "docker compose ps forgejo", Doc: "Check container"},
			{Cmd: "docker compose up -d forgejo", Doc: "Start container"},
		},
	}
	FormatRemediation(rem, &buf)
	out := buf.String()

	for _, want := range []string{
		"Forgejo reachable",
		"high",
		"test reason",
		"specs/foo.md",
		"docker compose ps forgejo",
		"docker compose up -d forgejo",
		"Check container",
		"Start container",
		"1.",
		"2.",
		"$",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nGot:\n%s", want, out)
		}
	}
}

func TestFormatRemediation_NoSteps(t *testing.T) {
	var buf bytes.Buffer
	rem := Remediation{
		Check:    "mystery",
		Severity: SeverityLow,
		Reason:   "no idea",
		DocURL:   "specs/x.md",
	}
	FormatRemediation(rem, &buf)
	if !strings.Contains(buf.String(), "no automatic remediation known") {
		t.Error("expected fallback message for empty steps")
	}
}

func TestFormatRemediationJSON_ValidJSON(t *testing.T) {
	rem := Remediation{
		Check:    "Forgejo",
		Reason:   "down",
		Severity: SeverityHigh,
		DocURL:   "specs/foo.md",
		Steps: []Step{
			{Cmd: "echo \"hi\"", Doc: "say hi"},
		},
	}
	s, err := FormatRemediationJSON(rem)
	if err != nil {
		t.Fatalf("FormatRemediationJSON failed: %v", err)
	}

	// Round-trip through encoding/json to validate it parses.
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(s), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\nOutput: %s", err, s)
	}
	if got["check"] != "Forgejo" {
		t.Errorf("check field wrong: %v", got["check"])
	}
	if got["severity"] != "high" {
		t.Errorf("severity field wrong: %v", got["severity"])
	}
	if got["auto_applicable"] != false {
		t.Errorf("auto_applicable should default to false (safety)")
	}
	steps, ok := got["steps"].([]interface{})
	if !ok {
		t.Fatalf("steps is not an array: %T", got["steps"])
	}
	if len(steps) != 1 {
		t.Errorf("expected 1 step, got %d", len(steps))
	}
}

func TestFormatRemediationJSON_EscapesQuotesAndNewlines(t *testing.T) {
	rem := Remediation{
		Check:  `check "with quotes"`,
		Reason: "line1\nline2",
		Steps:  []Step{{Cmd: "cmd with \"quotes\"", Doc: "tab\there"}},
	}
	s, err := FormatRemediationJSON(rem)
	if err != nil {
		t.Fatalf("FormatRemediationJSON failed: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(s), &got); err != nil {
		t.Fatalf("output invalid JSON: %v\n%s", err, s)
	}
	if got["check"] != `check "with quotes"` {
		t.Errorf("check not round-tripped: %v", got["check"])
	}
	if got["reason"] != "line1\nline2" {
		t.Errorf("reason newline not preserved: %v", got["reason"])
	}
}

func TestJsonString_EdgeCases(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", `""`},
		{"simple", `"simple"`},
		{`with "quote"`, `"with \"quote\""`},
		{"with\nnewline", `"with\nnewline"`},
		{"with\ttab", `"with\ttab"`},
		{"with\\backslash", `"with\\backslash"`},
	}
	for _, c := range cases {
		got := jsonString(c.in)
		if got != c.want {
			t.Errorf("jsonString(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBoolString(t *testing.T) {
	if boolString(true) != "true" {
		t.Error("boolString(true) should be \"true\"")
	}
	if boolString(false) != "false" {
		t.Error("boolString(false) should be \"false\"")
	}
}

func TestDefault_SeverityForEachCheck(t *testing.T) {
	// Every default remediation should have a non-empty severity AND
	// a non-empty DocURL — these are the two safety invariants an
	// operator relies on when the registry grows in the future.
	ResetDefaultRegistry()
	defer ResetDefaultRegistry()

	reg := Default()
	for _, name := range reg.Keys() {
		rem, ok := reg.Lookup(name)
		if !ok {
			continue
		}
		if rem.Severity == "" {
			t.Errorf("check %q has empty severity", name)
		}
		if rem.DocURL == "" {
			t.Errorf("check %q has empty DocURL", name)
		}
		if rem.Reason == "" {
			t.Errorf("check %q has empty Reason", name)
		}
		if !rem.AutoApplicable == false {
			// i.e. AutoApplicable should always be false in this release
			t.Errorf("check %q has AutoApplicable=true (safety violation)", name)
		}
	}
}

func TestDefault_EveryCheckHasAtLeastOneStep(t *testing.T) {
	ResetDefaultRegistry()
	defer ResetDefaultRegistry()

	reg := Default()
	for _, name := range reg.Keys() {
		rem, ok := reg.Lookup(name)
		if !ok {
			continue
		}
		if len(rem.Steps) == 0 {
			t.Errorf("check %q has zero steps", name)
		}
		for i, step := range rem.Steps {
			if step.Cmd == "" {
				t.Errorf("check %q step %d has empty Cmd", name, i)
			}
			if step.Doc == "" {
				t.Errorf("check %q step %d has empty Doc", name, i)
			}
		}
	}
}

func TestResetDefaultRegistry_Idempotent(t *testing.T) {
	// Multiple resets in a row should not panic.
	ResetDefaultRegistry()
	ResetDefaultRegistry()
	ResetDefaultRegistry()
	Default() // rebuild
	ResetDefaultRegistry()
	Default() // rebuild again
}
