package health

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// DegradationChecker implements the spec §14.2 "What Still Works" matrix.
// Given the current health state of all subsystems, it computes which
// platform capabilities remain available and which are degraded/blocked.
// This enables `helix status` to report not just "something is down" but
// exactly what users and agents can still do.

// Capability represents a discrete platform function.
type Capability string

const (
	CapWriteCode      Capability = "write_code"         // agents write/edit code locally
	CapPushCode       Capability = "push_code"          // git push to branches
	CapOpenPR         Capability = "open_pr"            // create pull requests
	CapMergePR        Capability = "merge_pr"           // merge approved PRs
	CapHumanReview    Capability = "human_review"       // humans can review code
	CapAgentReview    Capability = "agent_review"       // Chimera multi-model review
	CapAdversarial    Capability = "adversarial_review" // Conscientiousness loop
	CapCostEstimate   Capability = "cost_estimate"      // pre-flight token cost
	CapTrustScore     Capability = "trust_scoring"      // trust ledger replay
	CapNegotiation    Capability = "negotiation"        // agent-to-agent debate
	CapTaskSchedule   Capability = "task_scheduling"    // Hivemind task assignment
	CapTracing        Capability = "tracing"            // LangFuse traces
	CapMetrics        Capability = "metrics"            // Prometheus metrics
	CapAlerting       Capability = "alerting"           // Prometheus alerting
	CapExternalAccess Capability = "external_access"    // access from outside the host
	CapSandbox        Capability = "sandbox_isolation"  // bubblewrap execution
)

// CapState is the availability state of a capability.
type CapState string

const (
	CapAvailable CapState = "available"
	CapDegraded  CapState = "degraded"
	CapBlocked   CapState = "blocked"
)

// CapabilityStatus describes a single capability's state during degradation.
type CapabilityStatus struct {
	Capability Capability `json:"capability"`
	State      CapState   `json:"state"`
	Reason     string     `json:"reason,omitempty"`
}

// DegradationRule maps a down subsystem to the capabilities it affects.
// Each rule defines: when this subsystem is down, what happens to each
// dependent capability?
type DegradationRule struct {
	Subsystem string
	// Impact defines what happens to each capability when this subsystem is down.
	Impact map[Capability]CapState
}

// degradationRules is the spec §14.2 matrix encoded as rules.
var degradationRules = []DegradationRule{
	{
		Subsystem: "forgejo",
		Impact: map[Capability]CapState{
			CapPushCode:     CapBlocked,   // can't push without forge
			CapOpenPR:       CapBlocked,   // can't create PRs
			CapMergePR:      CapBlocked,   // can't merge
			CapHumanReview:  CapBlocked,   // can't review in-forge
			CapAgentReview:  CapBlocked,   // no PRs to review
			CapAdversarial:  CapBlocked,   // no PRs to evaluate
			CapNegotiation:  CapBlocked,   // nothing to negotiate
			CapCostEstimate: CapAvailable, // cost estimation doesn't need forge
			CapTrustScore:   CapAvailable, // trust ledger is local
			CapTaskSchedule: CapAvailable, // scheduling can queue tasks
			CapWriteCode:    CapAvailable, // agents can still write locally
		},
	},
	{
		Subsystem: "chimera",
		Impact: map[Capability]CapState{
			CapAgentReview: CapBlocked,   // multi-model review unavailable
			CapMergePR:     CapDegraded,  // can merge with human-only approval
			CapNegotiation: CapBlocked,   // Chimera is the tie-break arbiter
			CapHumanReview: CapAvailable, // humans can still review
			CapAdversarial: CapAvailable, // Conscientiousness runs independently
		},
	},
	{
		Subsystem: "conscientiousness",
		Impact: map[Capability]CapState{
			CapAdversarial: CapDegraded, // adversarial layer absent
			// Everything else still works; elevated risk accepted per spec
		},
	},
	{
		Subsystem: "hivemind",
		Impact: map[Capability]CapState{
			CapTaskSchedule: CapBlocked, // scheduling stops
			// Agents operate from local memory, existing work continues
		},
	},
	{
		Subsystem: "langfuse",
		Impact: map[Capability]CapState{
			CapTracing: CapDegraded, // traces lost for outage window only
		},
	},
	{
		Subsystem: "prometheus",
		Impact: map[Capability]CapState{
			CapMetrics:  CapDegraded, // metrics gap
			CapAlerting: CapDegraded, // alerts may miss events
		},
	},
	{
		Subsystem: "sandbox",
		Impact: map[Capability]CapState{
			CapSandbox:   CapBlocked,  // can't run isolated agents
			CapWriteCode: CapDegraded, // agents can write but not in isolation
		},
	},
	{
		Subsystem: "trust",
		Impact: map[Capability]CapState{
			CapTrustScore: CapDegraded, // can't replay ledger
			CapMergePR:    CapDegraded, // can't verify tier requirements
		},
	},
	{
		Subsystem: "review",
		Impact: map[Capability]CapState{
			CapAgentReview: CapBlocked, // review pipeline down
		},
	},
	{
		Subsystem: "negotiate",
		Impact: map[Capability]CapState{
			CapNegotiation: CapBlocked, // negotiation engine down
		},
	},
	{
		Subsystem: "dispatcher",
		Impact: map[Capability]CapState{
			CapTaskSchedule: CapDegraded, // can't dispatch new tasks
		},
	},
	{
		Subsystem: "marketplace",
		Impact: map[Capability]CapState{
			CapCostEstimate: CapAvailable, // estimate works independently
		},
	},
	{
		Subsystem: "estimate",
		Impact: map[Capability]CapState{
			CapCostEstimate: CapBlocked, // estimation engine down
		},
	},
}

// DegradationReport is the result of evaluating the degradation matrix
// against the current subsystem health states.
type DegradationReport struct {
	Capabilities []CapabilityStatus `json:"capabilities"`
	DownSubsys   []string           `json:"down_subsystems"`
	GeneratedAt  time.Time          `json:"generated_at"`
}

// HasBlocked returns true if any capability is completely unavailable.
func (r *DegradationReport) HasBlocked() bool {
	for _, c := range r.Capabilities {
		if c.State == CapBlocked {
			return true
		}
	}
	return false
}

// HasDegraded returns true if any capability is degraded (but not blocked).
func (r *DegradationReport) HasDegraded() bool {
	for _, c := range r.Capabilities {
		if c.State == CapDegraded {
			return true
		}
	}
	return false
}

// Blocked returns all fully-blocked capabilities.
func (r *DegradationReport) Blocked() []Capability {
	var result []Capability
	for _, c := range r.Capabilities {
		if c.State == CapBlocked {
			result = append(result, c.Capability)
		}
	}
	return result
}

// Degraded returns all degraded capabilities.
func (r *DegradationReport) Degraded() []Capability {
	var result []Capability
	for _, c := range r.Capabilities {
		if c.State == CapDegraded {
			result = append(result, c.Capability)
		}
	}
	return result
}

// Summary returns a human-readable summary of what's available and what's not.
func (r *DegradationReport) Summary() string {
	if !r.HasBlocked() && !r.HasDegraded() {
		return "All capabilities available"
	}
	var parts []string
	if blocked := r.Blocked(); len(blocked) > 0 {
		names := make([]string, len(blocked))
		for i, c := range blocked {
			names[i] = string(c)
		}
		parts = append(parts, fmt.Sprintf("BLOCKED: %s", strings.Join(names, ", ")))
	}
	if degraded := r.Degraded(); len(degraded) > 0 {
		names := make([]string, len(degraded))
		for i, c := range degraded {
			names[i] = string(c)
		}
		parts = append(parts, fmt.Sprintf("DEGRADED: %s", strings.Join(names, ", ")))
	}
	return strings.Join(parts, " | ")
}

// AllCapabilities returns every capability the platform tracks.
func AllCapabilities() []Capability {
	seen := make(map[Capability]bool)
	for _, rule := range degradationRules {
		for cap := range rule.Impact {
			seen[cap] = true
		}
	}
	result := make([]Capability, 0, len(seen))
	for cap := range seen {
		result = append(result, cap)
	}
	sort.Slice(result, func(i, j int) bool {
		return string(result[i]) < string(result[j])
	})
	return result
}

// EvaluateDegradation takes a set of down/degraded subsystem names and
// returns a DegradationReport describing what capabilities remain available.
// downSubsystems is a set of subsystem names that are currently StateDown.
// degradedSubsystems is a set of subsystem names that are StateDegraded.
func EvaluateDegradation(downSubsystems, degradedSubsystems []string) *DegradationReport {
	downSet := make(map[string]bool)
	for _, s := range downSubsystems {
		downSet[s] = true
	}
	degradedSet := make(map[string]bool)
	for _, s := range degradedSubsystems {
		degradedSet[s] = true
	}

	// Initialize all capabilities as available.
	states := make(map[Capability]CapState)
	for _, cap := range AllCapabilities() {
		states[cap] = CapAvailable
	}

	// Apply rules for down subsystems.
	for _, rule := range degradationRules {
		if !downSet[rule.Subsystem] {
			continue
		}
		for cap, impact := range rule.Impact {
			current := states[cap]
			// Blocked > Degraded > Available — never downgrade severity.
			if severityRank(impact) > severityRank(current) {
				states[cap] = impact
			}
		}
	}

	// Apply rules for degraded subsystems (downgrade severity by one level).
	for _, rule := range degradationRules {
		if !degradedSet[rule.Subsystem] {
			continue
		}
		for cap, impact := range rule.Impact {
			degradedImpact := degradeImpact(impact)
			current := states[cap]
			if severityRank(degradedImpact) > severityRank(current) {
				states[cap] = degradedImpact
			}
		}
	}

	// Build report.
	caps := AllCapabilities()
	report := &DegradationReport{
		Capabilities: make([]CapabilityStatus, 0, len(caps)),
		DownSubsys:   downSubsystems,
		GeneratedAt:  time.Now().UTC(),
	}
	for _, cap := range caps {
		state := states[cap]
		entry := CapabilityStatus{
			Capability: cap,
			State:      state,
		}
		if state != CapAvailable {
			entry.Reason = findDownCause(cap, downSet, degradedSet)
		}
		report.Capabilities = append(report.Capabilities, entry)
	}

	return report
}

// EvaluateFromDashboard takes a DashboardReport and produces a degradation report.
func EvaluateFromDashboard(dash *DashboardReport) *DegradationReport {
	var down, degraded []string
	for _, s := range dash.Subsystems {
		switch s.State {
		case StateDown:
			down = append(down, s.Name)
		case StateDegraded:
			degraded = append(degraded, s.Name)
		}
	}
	return EvaluateDegradation(down, degraded)
}

// severityRank returns a numeric ranking: Available=0, Degraded=1, Blocked=2.
func severityRank(s CapState) int {
	switch s {
	case CapBlocked:
		return 2
	case CapDegraded:
		return 1
	default:
		return 0
	}
}

// degradeImpact reduces the severity by one level: Blocked→Degraded, Degraded→Degraded.
func degradeImpact(s CapState) CapState {
	if s == CapBlocked {
		return CapDegraded
	}
	return s
}

// findDownCause finds which down subsystem(s) caused a capability to be affected.
func findDownCause(cap Capability, downSet, degradedSet map[string]bool) string {
	var causes []string
	for _, rule := range degradationRules {
		if impact, ok := rule.Impact[cap]; ok && impact != CapAvailable {
			if downSet[rule.Subsystem] {
				causes = append(causes, rule.Subsystem+" (down)")
			} else if degradedSet[rule.Subsystem] {
				causes = append(causes, rule.Subsystem+" (degraded)")
			}
		}
	}
	sort.Strings(causes)
	return strings.Join(causes, ", ")
}

// FormatDegradationReport renders a DegradationReport as human-readable text.
func FormatDegradationReport(report *DegradationReport) string {
	var b strings.Builder
	b.WriteString("Platform Degradation Report\n")
	b.WriteString("===========================\n\n")

	if len(report.DownSubsys) > 0 {
		b.WriteString(fmt.Sprintf("Down subsystems: %s\n\n", strings.Join(report.DownSubsys, ", ")))
	}

	// Group by state.
	blocked := report.Blocked()
	degraded := report.Degraded()

	if len(blocked) == 0 && len(degraded) == 0 {
		b.WriteString("✓ All capabilities available\n")
		return b.String()
	}

	if len(blocked) > 0 {
		b.WriteString("BLOCKED (unavailable):\n")
		for _, c := range report.Capabilities {
			if c.State == CapBlocked {
				b.WriteString(fmt.Sprintf("  ✗ %s — %s\n", c.Capability, c.Reason))
			}
		}
		b.WriteString("\n")
	}

	if len(degraded) > 0 {
		b.WriteString("DEGRADED (reduced function):\n")
		for _, c := range report.Capabilities {
			if c.State == CapDegraded {
				b.WriteString(fmt.Sprintf("  ⚠ %s — %s\n", c.Capability, c.Reason))
			}
		}
		b.WriteString("\n")
	}

	available := 0
	for _, c := range report.Capabilities {
		if c.State == CapAvailable {
			available++
		}
	}
	if available > 0 {
		b.WriteString(fmt.Sprintf("AVAILABLE: %d capabilities still operational\n", available))
	}

	return b.String()
}
