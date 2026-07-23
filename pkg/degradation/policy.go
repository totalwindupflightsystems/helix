// Package degradation encodes Helix platform graceful-degradation policies
// as structured Go data, enabling programmatic lookup of which platform
// action to take (continue / use fallback / fail fast / notify) when a
// dependent service is unhealthy.
//
// Data is derived from specs/SPECIFICATION.md §14.2 (Graceful Degradation).
// Where §14.2 specifies which *capabilities* remain available (handled by
// pkg/health.DegradationChecker), this package encodes which concrete
// *platform actions* to take when a service is degraded.
package degradation

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// -----------------------------------------------------------------------------
// Action — what to do when a service is degraded
// -----------------------------------------------------------------------------

// Action is the platform's response when a dependency is unhealthy.
type Action string

const (
	// ActionContinueWithCache serves from local cache when the dependency is down.
	ActionContinueWithCache Action = "continue_with_cache"
	// ActionUseFallback swaps in a pre-configured fallback component.
	ActionUseFallback Action = "use_fallback"
	// ActionFailFast returns an error to the caller immediately.
	ActionFailFast Action = "fail_fast"
	// ActionPause pauses the dependent operation until the service recovers.
	ActionPause Action = "pause"
)

// IsValid reports whether a is a recognized action.
func (a Action) IsValid() bool {
	switch a {
	case ActionContinueWithCache, ActionUseFallback, ActionFailFast, ActionPause:
		return true
	default:
		return false
	}
}

// -----------------------------------------------------------------------------
// NotificationLevel — how loudly to alert humans
// -----------------------------------------------------------------------------

// NotificationLevel is how loud the user-facing alert should be.
type NotificationLevel string

const (
	NotifySilent   NotificationLevel = "silent"   // no notification
	NotifyInfo     NotificationLevel = "info"     // log line only
	NotifyWarning  NotificationLevel = "warning"  // dashboard alert
	NotifyCritical NotificationLevel = "critical" // page + Telegram
)

// -----------------------------------------------------------------------------
// HealthState — service health
// -----------------------------------------------------------------------------

// HealthState describes the observed health of a dependent service.
type HealthState string

const (
	HealthHealthy  HealthState = "healthy"
	HealthDegraded HealthState = "degraded"
	HealthDown     HealthState = "down"
	HealthUnknown  HealthState = "unknown"
)

// -----------------------------------------------------------------------------
// Service — a platform-internal or external service this package can advise on
// -----------------------------------------------------------------------------

// Service identifies a platform dependency (Forgejo, Chimera, …).
type Service string

const (
	ServiceForgejo           Service = "forgejo"
	ServiceChimera           Service = "chimera"
	ServiceConscientiousness Service = "conscientiousness"
	ServiceHivemind          Service = "hivemind"
	ServiceLangFuse          Service = "langfuse"
	ServicePrometheus        Service = "prometheus"
	ServiceCaddy             Service = "caddy"
	ServiceDuckBrain         Service = "duckbrain"
	ServiceMuster            Service = "muster"
)

// AllServices returns all known services in canonical (alphabetical) order.
func AllServices() []Service {
	return []Service{
		ServiceCaddy,
		ServiceChimera,
		ServiceConscientiousness,
		ServiceDuckBrain,
		ServiceForgejo,
		ServiceHivemind,
		ServiceLangFuse,
		ServiceMuster,
		ServicePrometheus,
	}
}

// -----------------------------------------------------------------------------
// Policy — service → action table
// -----------------------------------------------------------------------------

// Policy describes the platform's response when a service is in a given state.
// It composes Action + NotificationLevel + optional Fallback component name.
type Policy struct {
	// Service is the dependent service this policy applies to.
	Service Service
	// State is the observed health state of the dependent service.
	State HealthState
	// Action is what the platform should do.
	Action Action
	// Fallback is the name of the fallback component to use when
	// Action=ActionUseFallback. Ignored for other actions.
	Fallback string
	// Notification is the user-facing alert level.
	Notification NotificationLevel
	// Rationale is a one-line explanation of why this policy applies.
	Rationale string
}

// IsValid reports whether the policy has consistent fields.
func (p Policy) IsValid() error {
	if p.Service == "" {
		return fmt.Errorf("policy Service is required")
	}
	if !p.Action.IsValid() {
		return fmt.Errorf("policy %q state %q: invalid Action %q", p.Service, p.State, p.Action)
	}
	if p.Action == ActionUseFallback && p.Fallback == "" {
		return fmt.Errorf("policy %q state %q: Action=use_fallback requires Fallback", p.Service, p.State)
	}
	return nil
}

// -----------------------------------------------------------------------------
// Registry
// -----------------------------------------------------------------------------

// Registry holds policies keyed by (Service, State).
type Registry struct {
	mu       sync.RWMutex
	policies map[Service]map[HealthState]Policy
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		policies: make(map[Service]map[HealthState]Policy),
	}
}

// Register adds a policy. Returns an error if a policy for the same
// (Service, State) pair already exists or the policy is invalid.
func (r *Registry) Register(p Policy) error {
	if err := p.IsValid(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.policies[p.Service]; !exists {
		r.policies[p.Service] = make(map[HealthState]Policy)
	}
	if _, exists := r.policies[p.Service][p.State]; exists {
		return fmt.Errorf("policy for service %q state %q already registered", p.Service, p.State)
	}
	r.policies[p.Service][p.State] = p
	return nil
}

// MustRegister adds a policy and panics on error.
func (r *Registry) MustRegister(p Policy) {
	if err := r.Register(p); err != nil {
		panic(err)
	}
}

// Lookup returns the policy for (service, state) and whether one was found.
func (r *Registry) Lookup(service Service, state HealthState) (Policy, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	byState, ok := r.policies[service]
	if !ok {
		return Policy{}, false
	}
	p, ok := byState[state]
	return p, ok
}

// LookupForState returns the most-applicable policy for a service given an
// observed state. Unknown/healthy degrade to ActionContinueWithCache for
// services not explicitly configured.
func (r *Registry) LookupForState(service Service, state HealthState) Policy {
	if p, ok := r.Lookup(service, state); ok {
		return p
	}
	if state == HealthHealthy || state == HealthUnknown {
		return Policy{
			Service: service, State: state,
			Action: ActionContinueWithCache, Notification: NotifySilent,
			Rationale: "service healthy/unknown; default to continue",
		}
	}
	return Policy{
		Service: service, State: state,
		Action: ActionFailFast, Notification: NotifyCritical,
		Rationale: "service degraded/down with no policy defined; fail fast",
	}
}

// Services returns all services that have at least one policy registered.
func (r *Registry) Services() []Service {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Service, 0, len(r.policies))
	for svc := range r.policies {
		out = append(out, svc)
	}
	sort.Slice(out, func(i, j int) bool { return string(out[i]) < string(out[j]) })
	return out
}

// PoliciesForService returns all policies registered for a single service.
func (r *Registry) PoliciesForService(service Service) []Policy {
	r.mu.RLock()
	defer r.mu.RUnlock()
	byState, ok := r.policies[service]
	if !ok {
		return nil
	}
	out := make([]Policy, 0, len(byState))
	for _, p := range byState {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return string(out[i].State) < string(out[j].State) })
	return out
}

// -----------------------------------------------------------------------------
// Spec canonical policies (specs/SPECIFICATION.md §14.2)
// -----------------------------------------------------------------------------

// DefaultRegistry returns a Registry pre-populated with the spec §14.2
// graceful degradation policies for all 9 services.
func DefaultRegistry() (*Registry, error) {
	r := NewRegistry()
	policies := []Policy{
		// Forgejo: can't push/PR/merge when down. Local writes OK.
		{Service: ServiceForgejo, State: HealthDown, Action: ActionFailFast, Notification: NotifyCritical, Rationale: "Forgejo down: agents write locally, no PRs possible"},
		{Service: ServiceForgejo, State: HealthDegraded, Action: ActionPause, Notification: NotifyWarning, Rationale: "Forgejo degraded: pause PR flow, retry"},
		{Service: ServiceForgejo, State: HealthHealthy, Action: ActionContinueWithCache, Notification: NotifySilent, Rationale: "Forgejo healthy"},
		{Service: ServiceForgejo, State: HealthUnknown, Action: ActionContinueWithCache, Notification: NotifyInfo, Rationale: "Forgejo health unknown; proceed with caution"},

		// Chimera: human review still works.
		{Service: ServiceChimera, State: HealthDown, Action: ActionUseFallback, Fallback: "human_review_only", Notification: NotifyWarning, Rationale: "Chimera down: PRs merge with human-only co-approval"},
		{Service: ServiceChimera, State: HealthDegraded, Action: ActionContinueWithCache, Notification: NotifyInfo, Rationale: "Chimera degraded: use cached verdicts"},
		{Service: ServiceChimera, State: HealthHealthy, Action: ActionContinueWithCache, Notification: NotifySilent, Rationale: "Chimera healthy"},

		// Conscientiousness: adversarial layer absent.
		{Service: ServiceConscientiousness, State: HealthDown, Action: ActionContinueWithCache, Notification: NotifyCritical, Rationale: "Conscientiousness down: adversarial layer absent, elevated risk"},
		{Service: ServiceConscientiousness, State: HealthDegraded, Action: ActionContinueWithCache, Notification: NotifyWarning, Rationale: "Conscientiousness degraded: pass through with warning"},
		{Service: ServiceConscientiousness, State: HealthHealthy, Action: ActionContinueWithCache, Notification: NotifySilent, Rationale: "Conscientiousness healthy"},

		// Hivemind: schedule pauses.
		{Service: ServiceHivemind, State: HealthDown, Action: ActionUseFallback, Fallback: "local_memory", Notification: NotifyWarning, Rationale: "Hivemind down: agents operate from local memory, scheduling pauses"},
		{Service: ServiceHivemind, State: HealthDegraded, Action: ActionContinueWithCache, Notification: NotifyInfo, Rationale: "Hivemind degraded: queue tasks, retry"},
		{Service: ServiceHivemind, State: HealthHealthy, Action: ActionContinueWithCache, Notification: NotifySilent, Rationale: "Hivemind healthy"},

		// LangFuse: traces lost for the window only.
		{Service: ServiceLangFuse, State: HealthDown, Action: ActionContinueWithCache, Notification: NotifyWarning, Rationale: "LangFuse down: traces lost for the outage window only"},
		{Service: ServiceLangFuse, State: HealthDegraded, Action: ActionContinueWithCache, Notification: NotifyInfo, Rationale: "LangFuse degraded: partial trace capture"},
		{Service: ServiceLangFuse, State: HealthHealthy, Action: ActionContinueWithCache, Notification: NotifySilent, Rationale: "LangFuse healthy"},

		// Prometheus: metrics gap.
		{Service: ServicePrometheus, State: HealthDown, Action: ActionContinueWithCache, Notification: NotifyWarning, Rationale: "Prometheus down: metrics gap, alerts may miss events"},
		{Service: ServicePrometheus, State: HealthDegraded, Action: ActionContinueWithCache, Notification: NotifyInfo, Rationale: "Prometheus degraded: partial metrics"},
		{Service: ServicePrometheus, State: HealthHealthy, Action: ActionContinueWithCache, Notification: NotifySilent, Rationale: "Prometheus healthy"},

		// Caddy: external access blocked.
		{Service: ServiceCaddy, State: HealthDown, Action: ActionFailFast, Notification: NotifyCritical, Rationale: "Caddy down: external access blocked, human intervention needed"},
		{Service: ServiceCaddy, State: HealthDegraded, Action: ActionPause, Notification: NotifyWarning, Rationale: "Caddy degraded: pause external merges"},
		{Service: ServiceCaddy, State: HealthHealthy, Action: ActionContinueWithCache, Notification: NotifySilent, Rationale: "Caddy healthy"},

		// DuckBrain: similar to LangFuse — pause but don't fail.
		{Service: ServiceDuckBrain, State: HealthDown, Action: ActionUseFallback, Fallback: "local_ledger", Notification: NotifyWarning, Rationale: "DuckBrain down: use local ledger, replay later"},
		{Service: ServiceDuckBrain, State: HealthDegraded, Action: ActionContinueWithCache, Notification: NotifyInfo, Rationale: "DuckBrain degraded: queue writes"},
		{Service: ServiceDuckBrain, State: HealthHealthy, Action: ActionContinueWithCache, Notification: NotifySilent, Rationale: "DuckBrain healthy"},

		// Muster: tools unavailable — degrade gracefully.
		{Service: ServiceMuster, State: HealthDown, Action: ActionUseFallback, Fallback: "static_tools", Notification: NotifyWarning, Rationale: "Muster down: use static tool registry"},
		{Service: ServiceMuster, State: HealthDegraded, Action: ActionContinueWithCache, Notification: NotifyInfo, Rationale: "Muster degraded: cache generated tools"},
		{Service: ServiceMuster, State: HealthHealthy, Action: ActionContinueWithCache, Notification: NotifySilent, Rationale: "Muster healthy"},
	}
	for _, policy := range policies {
		if err := r.Register(policy); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// -----------------------------------------------------------------------------
// ApplyPolicy — execute the policy
// -----------------------------------------------------------------------------

// ApplyResult is the structured result of consulting a policy.
type ApplyResult struct {
	Policy      Policy
	ShouldBlock bool   // Action=FailFast
	ShouldPause bool   // Action=Pause
	UseFallback string // Action=UseFallback (non-empty = use this fallback)
	NotifyLevel NotificationLevel
	Rationale   string
}

// ApplyPolicy consults the registry and returns a structured decision.
func (r *Registry) ApplyPolicy(service Service, state HealthState) ApplyResult {
	p := r.LookupForState(service, state)
	res := ApplyResult{
		Policy:      p,
		ShouldBlock: p.Action == ActionFailFast,
		ShouldPause: p.Action == ActionPause,
		UseFallback: p.Fallback,
		NotifyLevel: p.Notification,
		Rationale:   p.Rationale,
	}
	return res
}

// -----------------------------------------------------------------------------
// Report formatting
// -----------------------------------------------------------------------------

// Report summarizes all policies in the registry.
type Report struct {
	TotalPolicies int
	ByService     map[Service][]Policy
}

// GenerateReport returns a Report of all registered policies.
func (r *Registry) GenerateReport() *Report {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rep := &Report{
		ByService: make(map[Service][]Policy),
	}
	for svc := range r.policies {
		for _, p := range r.policies[svc] {
			rep.ByService[svc] = append(rep.ByService[svc], p)
			rep.TotalPolicies++
		}
	}
	for svc := range rep.ByService {
		sort.Slice(rep.ByService[svc], func(i, j int) bool {
			return string(rep.ByService[svc][i].State) < string(rep.ByService[svc][j].State)
		})
	}
	return rep
}

// FormatReport returns a human-readable summary of the registry.
func (r *Registry) FormatReport() string {
	rep := r.GenerateReport()
	var b strings.Builder
	fmt.Fprintf(&b, "Helix Degradation Policy Pack — %d policies across %d services\n",
		rep.TotalPolicies, len(rep.ByService))
	b.WriteString("=============================================================\n\n")
	for _, svc := range r.Services() {
		policies := rep.ByService[svc]
		fmt.Fprintf(&b, "%s (%d states)\n", svc, len(policies))
		for _, p := range policies {
			fmt.Fprintf(&b, "  [%s] action=%s notification=%s", p.State, p.Action, p.Notification)
			if p.Fallback != "" {
				fmt.Fprintf(&b, " fallback=%s", p.Fallback)
			}
			fmt.Fprintf(&b, " — %s\n", p.Rationale)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// FormatApplyResult returns a one-line summary of an ApplyPolicy result.
func FormatApplyResult(res ApplyResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s [%s] → %s", res.Policy.Service, res.Policy.State, res.Policy.Action)
	if res.UseFallback != "" {
		fmt.Fprintf(&b, " (fallback=%s)", res.UseFallback)
	}
	fmt.Fprintf(&b, " [notify=%s]", res.NotifyLevel)
	if res.Rationale != "" {
		fmt.Fprintf(&b, " — %s", res.Rationale)
	}
	return b.String()
}
