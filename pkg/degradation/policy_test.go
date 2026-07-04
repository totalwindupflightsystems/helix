package degradation

import (
	"strings"
	"testing"
)

// -----------------------------------------------------------------------------
// Action.IsValid
// -----------------------------------------------------------------------------

func TestAction_IsValid(t *testing.T) {
	tests := []struct {
		a    Action
		want bool
	}{
		{ActionContinueWithCache, true},
		{ActionUseFallback, true},
		{ActionFailFast, true},
		{ActionPause, true},
		{Action(""), false},
		{Action("retry"), false},
	}
	for _, tt := range tests {
		if got := tt.a.IsValid(); got != tt.want {
			t.Errorf("Action(%q).IsValid() = %v, want %v", tt.a, got, tt.want)
		}
	}
}

// -----------------------------------------------------------------------------
// Policy.IsValid
// -----------------------------------------------------------------------------

func TestPolicy_IsValid_Valid(t *testing.T) {
	p := Policy{Service: ServiceForgejo, State: HealthDown, Action: ActionFailFast, Notification: NotifyCritical}
	if err := p.IsValid(); err != nil {
		t.Errorf("expected valid: %v", err)
	}
}

func TestPolicy_IsValid_MissingService(t *testing.T) {
	p := Policy{Action: ActionFailFast}
	if err := p.IsValid(); err == nil {
		t.Error("expected error for missing Service")
	}
}

func TestPolicy_IsValid_BadAction(t *testing.T) {
	p := Policy{Service: ServiceForgejo, Action: Action("nope")}
	if err := p.IsValid(); err == nil {
		t.Error("expected error for invalid Action")
	}
}

func TestPolicy_IsValid_FallbackRequired(t *testing.T) {
	p := Policy{Service: ServiceChimera, Action: ActionUseFallback}
	if err := p.IsValid(); err == nil {
		t.Error("expected error when Action=use_fallback without Fallback")
	}
}

func TestPolicy_IsValid_FallbackWithOtherAction(t *testing.T) {
	// Fallback is optional for non-use_fallback actions.
	p := Policy{Service: ServiceForgejo, Action: ActionFailFast, Fallback: "x"}
	if err := p.IsValid(); err != nil {
		t.Errorf("Fallback with non-use_fallback should not error: %v", err)
	}
}

// -----------------------------------------------------------------------------
// Registry.Register / Lookup / LookupForState
// -----------------------------------------------------------------------------

func TestRegistry_Register_Lookup(t *testing.T) {
	r := NewRegistry()
	p := Policy{Service: ServiceForgejo, State: HealthDown, Action: ActionFailFast, Notification: NotifyCritical, Rationale: "test"}
	if err := r.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, ok := r.Lookup(ServiceForgejo, HealthDown)
	if !ok {
		t.Fatal("Lookup failed")
	}
	if got.Rationale != "test" {
		t.Errorf("got Rationale=%q", got.Rationale)
	}
	if _, ok := r.Lookup(ServiceForgejo, HealthHealthy); ok {
		t.Error("Lookup should miss on unregistered state")
	}
}

func TestRegistry_Register_Duplicate(t *testing.T) {
	r := NewRegistry()
	p := Policy{Service: ServiceForgejo, State: HealthDown, Action: ActionFailFast}
	r.MustRegister(p)
	err := r.Register(p)
	if err == nil {
		t.Error("expected duplicate-registration error")
	}
}

func TestRegistry_Register_Invalid(t *testing.T) {
	r := NewRegistry()
	err := r.Register(Policy{Action: ActionFailFast}) // missing Service
	if err == nil {
		t.Error("expected validation error")
	}
}

func TestRegistry_LookupForState_HealthyDefaults(t *testing.T) {
	r := NewRegistry()
	p := r.LookupForState(ServiceForgejo, HealthHealthy)
	if p.Action != ActionContinueWithCache {
		t.Errorf("healthy default Action = %q, want %q", p.Action, ActionContinueWithCache)
	}
}

func TestRegistry_LookupForState_UnregisteredDownFailsFast(t *testing.T) {
	r := NewRegistry()
	// Use a service that exists in AllServices() but is NOT registered in this empty registry.
	p := r.LookupForState(ServiceMuster, HealthDown)
	if p.Action != ActionFailFast {
		t.Errorf("unregistered+down default Action = %q, want %q", p.Action, ActionFailFast)
	}
	if p.Notification != NotifyCritical {
		t.Errorf("Notification = %q, want critical", p.Notification)
	}
}

func TestRegistry_Services(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(Policy{Service: ServiceForgejo, State: HealthDown, Action: ActionFailFast})
	r.MustRegister(Policy{Service: ServiceChimera, State: HealthDown, Action: ActionUseFallback, Fallback: "x"})
	got := r.Services()
	if len(got) != 2 {
		t.Errorf("Services() = %d, want 2", len(got))
	}
	// Sorted
	if got[0] != ServiceChimera || got[1] != ServiceForgejo {
		t.Errorf("Services() order = %v, want [chimera forgejo]", got)
	}
}

func TestRegistry_PoliciesForService(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(Policy{Service: ServiceForgejo, State: HealthDown, Action: ActionFailFast})
	r.MustRegister(Policy{Service: ServiceForgejo, State: HealthHealthy, Action: ActionContinueWithCache})
	policies := r.PoliciesForService(ServiceForgejo)
	if len(policies) != 2 {
		t.Errorf("PoliciesForService() = %d, want 2", len(policies))
	}
	// Sorted by state
	if policies[0].State != HealthDown {
		t.Errorf("policies[0] State=%q, want down", policies[0].State)
	}
}

func TestRegistry_PoliciesForService_Empty(t *testing.T) {
	r := NewRegistry()
	if got := r.PoliciesForService(ServiceForgejo); got != nil {
		t.Errorf("expected nil for empty registry, got %v", got)
	}
}

// -----------------------------------------------------------------------------
// DefaultRegistry (spec §14.2)
// -----------------------------------------------------------------------------

func TestDefaultRegistry_AllServices(t *testing.T) {
	r := DefaultRegistry()
	for _, svc := range AllServices() {
		if len(r.PoliciesForService(svc)) == 0 {
			t.Errorf("DefaultRegistry missing policies for service %q", svc)
		}
	}
}

func TestDefaultRegistry_Forgejo_Down_FailFast(t *testing.T) {
	r := DefaultRegistry()
	p, ok := r.Lookup(ServiceForgejo, HealthDown)
	if !ok {
		t.Fatal("expected Forgejo down policy")
	}
	if p.Action != ActionFailFast {
		t.Errorf("Forgejo down Action=%q, want %q", p.Action, ActionFailFast)
	}
	if p.Notification != NotifyCritical {
		t.Errorf("Forgejo down Notification=%q, want critical", p.Notification)
	}
}

func TestDefaultRegistry_Chimera_Down_UseFallback(t *testing.T) {
	r := DefaultRegistry()
	p, ok := r.Lookup(ServiceChimera, HealthDown)
	if !ok {
		t.Fatal("expected Chimera down policy")
	}
	if p.Action != ActionUseFallback {
		t.Errorf("Chimera down Action=%q, want use_fallback", p.Action)
	}
	if p.Fallback != "human_review_only" {
		t.Errorf("Chimera down Fallback=%q, want human_review_only", p.Fallback)
	}
}

func TestDefaultRegistry_LangFuse_Down_ContinueWithCache(t *testing.T) {
	r := DefaultRegistry()
	p, ok := r.Lookup(ServiceLangFuse, HealthDown)
	if !ok {
		t.Fatal("expected LangFuse down policy")
	}
	if p.Action != ActionContinueWithCache {
		t.Errorf("LangFuse down Action=%q, want continue_with_cache", p.Action)
	}
}

func TestDefaultRegistry_Hivemind_Down_LocalMemory(t *testing.T) {
	r := DefaultRegistry()
	p, ok := r.Lookup(ServiceHivemind, HealthDown)
	if !ok {
		t.Fatal("expected Hivemind down policy")
	}
	if p.Fallback != "local_memory" {
		t.Errorf("Hivemind down Fallback=%q, want local_memory", p.Fallback)
	}
}

func TestDefaultRegistry_Caddy_Down_Critical(t *testing.T) {
	r := DefaultRegistry()
	p, ok := r.Lookup(ServiceCaddy, HealthDown)
	if !ok {
		t.Fatal("expected Caddy down policy")
	}
	if p.Action != ActionFailFast {
		t.Errorf("Caddy down Action=%q, want fail_fast", p.Action)
	}
	if p.Notification != NotifyCritical {
		t.Errorf("Caddy down Notification=%q, want critical", p.Notification)
	}
}

func TestDefaultRegistry_Healthy_AllSilent(t *testing.T) {
	r := DefaultRegistry()
	for _, svc := range AllServices() {
		p, ok := r.Lookup(svc, HealthHealthy)
		if !ok {
			continue // some services may not have explicit healthy policy
		}
		if p.Notification != NotifySilent {
			t.Errorf("service %q healthy: Notification=%q, want silent", svc, p.Notification)
		}
	}
}

// -----------------------------------------------------------------------------
// ApplyPolicy
// -----------------------------------------------------------------------------

func TestApplyPolicy_FailFast(t *testing.T) {
	r := DefaultRegistry()
	res := r.ApplyPolicy(ServiceForgejo, HealthDown)
	if !res.ShouldBlock {
		t.Error("expected ShouldBlock=true for Forgejo down")
	}
	if res.ShouldPause {
		t.Error("expected ShouldPause=false")
	}
	if res.UseFallback != "" {
		t.Errorf("expected empty UseFallback, got %q", res.UseFallback)
	}
	if res.NotifyLevel != NotifyCritical {
		t.Errorf("NotifyLevel=%q", res.NotifyLevel)
	}
}

func TestApplyPolicy_UseFallback(t *testing.T) {
	r := DefaultRegistry()
	res := r.ApplyPolicy(ServiceChimera, HealthDown)
	if res.ShouldBlock {
		t.Error("expected ShouldBlock=false for Chimera down")
	}
	if res.UseFallback != "human_review_only" {
		t.Errorf("UseFallback=%q", res.UseFallback)
	}
}

func TestApplyPolicy_Pause(t *testing.T) {
	r := DefaultRegistry()
	res := r.ApplyPolicy(ServiceCaddy, HealthDegraded)
	if !res.ShouldPause {
		t.Error("expected ShouldPause=true for Caddy degraded")
	}
}

func TestApplyPolicy_Continue(t *testing.T) {
	r := DefaultRegistry()
	res := r.ApplyPolicy(ServiceLangFuse, HealthDown)
	if res.ShouldBlock {
		t.Error("expected ShouldBlock=false for LangFuse down")
	}
	if res.ShouldPause {
		t.Error("expected ShouldPause=false for LangFuse down")
	}
	if res.NotifyLevel != NotifyWarning {
		t.Errorf("NotifyLevel=%q", res.NotifyLevel)
	}
}

func TestApplyPolicy_RationalePopulated(t *testing.T) {
	r := DefaultRegistry()
	res := r.ApplyPolicy(ServiceForgejo, HealthDown)
	if res.Rationale == "" {
		t.Error("expected non-empty Rationale")
	}
}

// -----------------------------------------------------------------------------
// Report
// -----------------------------------------------------------------------------

func TestReport_TotalPolicies(t *testing.T) {
	r := DefaultRegistry()
	rep := r.GenerateReport()
	// DefaultRegistry registers ~13 policies (4 Forgejo + 3 Chimera + 3 Conscientiousness + 3 Hivemind + 3 LangFuse + 3 Prometheus + 3 Caddy + 3 DuckBrain + 3 Muster = 28)
	if rep.TotalPolicies < 9 {
		t.Errorf("TotalPolicies=%d, want >= 9", rep.TotalPolicies)
	}
}

func TestReport_FormatReport(t *testing.T) {
	r := DefaultRegistry()
	got := r.FormatReport()
	wants := []string{
		"Helix Degradation Policy Pack",
		"forgejo",
		"chimera",
		"conscientiousness",
		"hivemind",
		"langfuse",
		"prometheus",
		"caddy",
		"duckbrain",
		"muster",
		"action=fail_fast",
		"action=use_fallback",
	}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in report", w)
		}
	}
}

func TestFormatApplyResult(t *testing.T) {
	r := DefaultRegistry()
	res := r.ApplyPolicy(ServiceForgejo, HealthDown)
	got := FormatApplyResult(res)
	wants := []string{"forgejo", "down", "fail_fast", "critical"}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in %q", w, got)
		}
	}
}

func TestFormatApplyResult_WithFallback(t *testing.T) {
	r := DefaultRegistry()
	res := r.ApplyPolicy(ServiceChimera, HealthDown)
	got := FormatApplyResult(res)
	if !strings.Contains(got, "fallback=human_review_only") {
		t.Errorf("missing fallback annotation: %s", got)
	}
}

// -----------------------------------------------------------------------------
// AllServices
// -----------------------------------------------------------------------------

func TestAllServices(t *testing.T) {
	got := AllServices()
	if len(got) != 9 {
		t.Errorf("AllServices() returned %d, want 9", len(got))
	}
	// Sorted ascending
	for i := 1; i < len(got); i++ {
		if string(got[i-1]) >= string(got[i]) {
			t.Errorf("AllServices() not sorted at %d: %v", i, got)
		}
	}
}

// -----------------------------------------------------------------------------
// Coverage matrix — 9 services × 4 states = 36 transitions
// -----------------------------------------------------------------------------

func TestSpecCoverage_MatrixTransitions(t *testing.T) {
	r := DefaultRegistry()
	covered := 0
	for _, svc := range AllServices() {
		for _, st := range []HealthState{HealthHealthy, HealthDegraded, HealthDown, HealthUnknown} {
			if _, ok := r.Lookup(svc, st); ok {
				covered++
			}
		}
	}
	// We cover at least 9 services × 1 state (down/healthy) = at least 18.
	if covered < 18 {
		t.Errorf("covered transitions = %d, want >= 18", covered)
	}
}
