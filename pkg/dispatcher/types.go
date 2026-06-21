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

import "errors"

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
	ID           string     `json:"id"`
	SpecRef      string     `json:"spec_ref"`
	Description  string     `json:"description"`
	Priority     int        `json:"priority"`
	AssignedAgent string    `json:"assigned_agent"`
	Status       TaskStatus `json:"status"`
}

// AgentProfile describes an agent available for task assignment.
type AgentProfile struct {
	Name         string `json:"name"`
	Capability   string `json:"capability"`
	CurrentLoad  int    `json:"current_load"`
	MaxLoad      int    `json:"max_load"`
}

// CanAcceptLoad reports whether the agent has capacity for one more task.
func (a AgentProfile) CanAcceptLoad() bool {
	return a.CurrentLoad < a.MaxLoad
}

// Step is a single actionable step within a work item.
type Step struct {
	Action          string     `json:"action"`
	ExpectedOutput  string     `json:"expected_output"`
	Status          StepStatus `json:"status"`
}

// WorkItem binds a task to an assigned agent and its execution steps.
type WorkItem struct {
	Task   Task         `json:"task"`
	Agent  AgentProfile `json:"agent"`
	Steps  []Step       `json:"steps"`
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
}

// NewDispatcher creates a Dispatcher with the given agent pool.
func NewDispatcher(agents []AgentProfile) *Dispatcher {
	return &Dispatcher{Agents: agents}
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

// ErrSpecNotFound is returned when the spec file cannot be read.
var ErrSpecNotFound = errors.New("dispatcher: spec file not found")

// ErrDecomposeFailed is returned when spec decomposition produces no tasks.
var ErrDecomposeFailed = errors.New("dispatcher: spec decomposition failed")
