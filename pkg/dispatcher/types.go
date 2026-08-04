// Package dispatcher provides the Helix orchestration layer that replaces
// Axiom. It decomposes specifications into tasks, assigns tasks to capable
// agents, and drives the Ralph Loop execution pipeline.
//
// Design goals:
//   - Thin orchestration: the dispatcher coordinates, agents execute.
//   - Capability-matched dispatch: tasks go to agents with the right skills
//     and available capacity.
//   - Spec-driven: all work originates from a spec markdown file.
//   - Stdlib only: no external Go dependencies.
package dispatcher

import (
	"errors"

	"github.com/totalwindupflightsystems/helix/pkg/identity"
	"github.com/totalwindupflightsystems/helix/pkg/source"
	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

// ---------------------------------------------------------------------------
// Task status
// ---------------------------------------------------------------------------

// TaskStatus represents the lifecycle state of a task.
type TaskStatus string

const (
	StatusPending    TaskStatus = "pending"
	StatusAssigned   TaskStatus = "assigned"
	StatusInProgress TaskStatus = "in_progress"
	StatusComplete   TaskStatus = "complete"
	StatusFailed     TaskStatus = "failed"
)

// IsValid reports whether s is a recognised task status.
func (s TaskStatus) IsValid() bool {
	switch s {
	case StatusPending, StatusAssigned, StatusInProgress, StatusComplete, StatusFailed:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// Step status
// ---------------------------------------------------------------------------

// StepStatus represents the lifecycle state of a single step within a work item.
type StepStatus string

const (
	StepPending    StepStatus = "pending"
	StepInProgress StepStatus = "in_progress"
	StepComplete   StepStatus = "complete"
	StepFailed     StepStatus = "failed"
)

// IsValid reports whether s is a recognised step status.
func (s StepStatus) IsValid() bool {
	switch s {
	case StepPending, StepInProgress, StepComplete, StepFailed:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// Core types
// ---------------------------------------------------------------------------

// Task is a single unit of work decomposed from a specification.
type Task struct {
	ID            string          `json:"id"`
	SpecRef       string          `json:"spec_ref"`
	Description   string          `json:"description"`
	Priority      int             `json:"priority"`
	AssignedAgent string          `json:"assigned_agent"`
	Status        TaskStatus      `json:"status"`
	RequiredTier  trust.TrustTier `json:"required_tier"`
}

// AgentProfile describes an agent available for task assignment.
type AgentProfile struct {
	Name         string                     `json:"name"`
	Capability   string                     `json:"capability"`
	Capabilities []identity.CapabilityClaim `json:"capabilities,omitempty"`
	CurrentLoad  int                        `json:"current_load"`
	MaxLoad      int                        `json:"max_load"`
	Tier         trust.TrustTier            `json:"tier"`
	TrustScore   float64                    `json:"trust_score"`
	CostProfile  float64                    `json:"cost_profile"`
}

// CanAcceptLoad reports whether the agent has capacity for one more task.
func (a AgentProfile) CanAcceptLoad() bool {
	return a.CurrentLoad < a.MaxLoad
}

// Step is a single actionable step within a work item.
type Step struct {
	Action         string     `json:"action"`
	ExpectedOutput string     `json:"expected_output"`
	Status         StepStatus `json:"status"`
}

// WorkItem binds a task to an assigned agent and its execution steps.
type WorkItem struct {
	Task  Task         `json:"task"`
	Agent AgentProfile `json:"agent"`
	Steps []Step       `json:"steps"`

	// SourceTools holds the capability-gated source tool sets attached at
	// dispatch time (SRC-005, SPEC-025 §5/§6). Each set carries its source
	// name (ToolSet.SourceName) so execution can key rate limiting on it.
	SourceTools []source.ToolSet `json:"source_tools,omitempty"`

	// SourceToolsError records why source-tool injection failed for this
	// work item (e.g. Muster unreachable). A non-empty value means the
	// item's SourceTools may be incomplete or empty; dispatch itself still
	// succeeded.
	SourceToolsError string `json:"source_tools_error,omitempty"`
}

// DispatchResult is the outcome of dispatching a single task.
type DispatchResult struct {
	WorkItem WorkItem `json:"work_item"`
	Error    string   `json:"error"`
}

// ---------------------------------------------------------------------------
// Dispatcher
// ---------------------------------------------------------------------------

// Dispatcher is the top-level orchestrator. It holds the agent registry and
// dispatches tasks through the full pipeline.
type Dispatcher struct {
	Agents []AgentProfile

	// SourceTools, when non-nil, injects capability-gated source tools
	// (SRC-005, SPEC-025 §6) into every successfully assigned WorkItem at
	// dispatch time.
	SourceTools *SourceToolInjector
}

// NewDispatcher creates a Dispatcher with the given agent pool.
func NewDispatcher(agents []AgentProfile) *Dispatcher {
	return &Dispatcher{Agents: agents}
}

// WithSourceTools attaches a source-tool injector to the dispatcher and
// returns the dispatcher for chaining. Nil-safe: calling it on a nil
// receiver returns nil. Pass nil to disable source-tool injection.
func (d *Dispatcher) WithSourceTools(in *SourceToolInjector) *Dispatcher {
	if d == nil {
		return nil
	}
	d.SourceTools = in
	return d
}

// ---------------------------------------------------------------------------
// Error taxonomy
// ---------------------------------------------------------------------------

// ErrNoAgents is returned when the dispatcher has no agents to assign to.
var ErrNoAgents = errors.New("dispatcher: no agents available")

// ErrNoCapableAgent is returned when no agent matches the required capability.
var ErrNoCapableAgent = errors.New("dispatcher: no agent with required capability")

// ErrAgentOverloaded is returned when all capable agents are at max load.
var ErrAgentOverloaded = errors.New("dispatcher: all capable agents are at max load")

// ErrTierTooLow is returned when no agent meets the required trust tier for a task.
var ErrTierTooLow = errors.New("dispatcher: no agent meets required trust tier")

// ErrSpecNotFound is returned when the spec file cannot be read.
var ErrSpecNotFound = errors.New("dispatcher: spec file not found")

// ErrDecomposeFailed is returned when spec decomposition produces no tasks.
var ErrDecomposeFailed = errors.New("dispatcher: spec decomposition failed")
