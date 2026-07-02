// Package pipeline encodes the 12-step Helix PR lifecycle as a state machine
// with transitions, preconditions, data contracts, and latency budgets.
//
// Data is derived from specs/SPECIFICATION.md §1.5 (12-Step Flow) and
// §2.2 (Step-by-Step State Transitions and Data Contracts).
package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ---------------------------------------------------------------------------
// Step definitions (spec §1.5)
// ---------------------------------------------------------------------------

// StepID identifies a pipeline step.
type StepID string

const (
	StepIdle             StepID = "idle"
	StepTaskCreated      StepID = "task_created"
	StepSwarmAssembled   StepID = "swarm_assembled"
	StepWorktreeAcquired StepID = "worktree_acquired"
	StepAgentWriting     StepID = "agent_writing"
	StepAgentCommitted   StepID = "agent_committed"
	StepGuardFired       StepID = "guard_fired"
	StepPROpened         StepID = "pr_opened"
	StepReviewComplete   StepID = "review_complete"
	StepAdversarialDone  StepID = "adversarial_complete"
	StepPromptFooPassed  StepID = "promptfoo_passed"
	StepCoApproved       StepID = "co_approved"
	StepMergedDeployed   StepID = "merged_deployed"
)

// Terminal/failure states
const (
	StateFailed    StepID = "failed"
	StateEscalated StepID = "escalated"
	StateBlocked   StepID = "blocked"
)

// StepInfo describes a pipeline step.
type StepInfo struct {
	ID             StepID `json:"id"`
	Order          int    `json:"order"`
	Description    string `json:"description"`
	Owner          string `json:"owner"`
	MinLatency     string `json:"min_latency"`
	TypicalLatency string `json:"typical_latency"`
	MaxLatency     string `json:"max_latency"`
}

// IsValid reports whether s is a valid step ID.
func (s StepID) IsValid() bool {
	switch s {
	case StepIdle, StepTaskCreated, StepSwarmAssembled, StepWorktreeAcquired,
		StepAgentWriting, StepAgentCommitted, StepGuardFired, StepPROpened,
		StepReviewComplete, StepAdversarialDone, StepPromptFooPassed,
		StepCoApproved, StepMergedDeployed,
		StateFailed, StateEscalated, StateBlocked:
		return true
	default:
		return false
	}
}

// IsTerminal returns true if the step is a terminal state (no further transitions).
func (s StepID) IsTerminal() bool {
	return s == StepMergedDeployed || s == StateFailed
}

// IsFailed returns true if the step represents a failure state.
func (s StepID) IsFailed() bool {
	return s == StateFailed || s == StateEscalated || s == StateBlocked
}

// IsBlocked returns true if the step represents a blocked state.
func (s StepID) IsBlocked() bool {
	return s == StateBlocked || s == StateEscalated
}

// ---------------------------------------------------------------------------
// Data contract (spec §2.2 — input → output per step)
// ---------------------------------------------------------------------------

// DataContract describes what a step receives and produces.
type DataContract struct {
	InputFields  []string `json:"input_fields"`
	OutputFields []string `json:"output_fields"`
}

// ---------------------------------------------------------------------------
// Pipeline state machine
// ---------------------------------------------------------------------------

// PipelineState tracks the current state of a PR pipeline.
type PipelineState struct {
	PRID        string            `json:"pr_id"`
	CurrentStep StepID            `json:"current_step"`
	History     []Transition      `json:"history"`
	Data        map[string]string `json:"data"`
	StartedAt   time.Time         `json:"started_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// Transition records a state change.
type Transition struct {
	From      StepID    `json:"from"`
	To        StepID    `json:"to"`
	Timestamp time.Time `json:"timestamp"`
	Reason    string    `json:"reason,omitempty"`
}

// PipelineStateMachine manages transitions for a single PR pipeline.
type PipelineStateMachine struct {
	state PipelineState
}

// NewPipelineStateMachine creates a new state machine for a given PR.
func NewPipelineStateMachine(prID string) *PipelineStateMachine {
	now := time.Now()
	return &PipelineStateMachine{
		state: PipelineState{
			PRID:        prID,
			CurrentStep: StepIdle,
			History:     nil,
			Data:        make(map[string]string),
			StartedAt:   now,
			UpdatedAt:   now,
		},
	}
}

// CurrentStep returns the current step ID.
func (sm *PipelineStateMachine) CurrentStep() StepID {
	return sm.state.CurrentStep
}

// State returns the full pipeline state.
func (sm *PipelineStateMachine) State() PipelineState {
	return sm.state
}

// CanTransition returns true if transitioning from current step to target is valid.
func (sm *PipelineStateMachine) CanTransition(target StepID) bool {
	return isValidTransition(sm.state.CurrentStep, target)
}

// Transition moves the pipeline to the next step. Returns error if invalid.
func (sm *PipelineStateMachine) Transition(target StepID, reason string) error {
	current := sm.state.CurrentStep
	if !isValidTransition(current, target) {
		return fmt.Errorf("invalid transition: %s → %s", current, target)
	}

	sm.state.History = append(sm.state.History, Transition{
		From:      current,
		To:        target,
		Timestamp: time.Now(),
		Reason:    reason,
	})
	sm.state.CurrentStep = target
	sm.state.UpdatedAt = time.Now()
	return nil
}

// IsComplete returns true if the pipeline has reached the terminal success state.
func (sm *PipelineStateMachine) IsComplete() bool {
	return sm.state.CurrentStep.IsTerminal()
}

// IsFailed returns true if the pipeline is in a failure state.
func (sm *PipelineStateMachine) IsFailed() bool {
	return sm.state.CurrentStep.IsFailed()
}

// IsBlocked returns true if the pipeline is blocked or escalated.
func (sm *PipelineStateMachine) IsBlocked() bool {
	return sm.state.CurrentStep.IsBlocked()
}

// SetData stores a key-value pair in the pipeline data.
func (sm *PipelineStateMachine) SetData(key, value string) {
	sm.state.Data[key] = value
	sm.state.UpdatedAt = time.Now()
}

// GetData retrieves a value from the pipeline data.
func (sm *PipelineStateMachine) GetData(key string) string {
	return sm.state.Data[key]
}

// History returns the transition history.
func (sm *PipelineStateMachine) History() []Transition {
	out := make([]Transition, len(sm.state.History))
	copy(out, sm.state.History)
	return out
}

// Elapsed returns the total elapsed time since pipeline start.
func (sm *PipelineStateMachine) Elapsed() time.Duration {
	return time.Since(sm.state.StartedAt)
}

// ---------------------------------------------------------------------------
// Transition rules (spec §2.2 state machines)
// ---------------------------------------------------------------------------

func isValidTransition(from, to StepID) bool {
	// Any step can transition to failure states
	switch to {
	case StateFailed, StateEscalated, StateBlocked:
		return true
	}

	switch from {
	case StepIdle:
		return to == StepTaskCreated
	case StepTaskCreated:
		return to == StepSwarmAssembled
	case StepSwarmAssembled:
		return to == StepWorktreeAcquired
	case StepWorktreeAcquired:
		return to == StepAgentWriting
	case StepAgentWriting:
		return to == StepAgentCommitted || to == StepAgentWriting // can re-enter on rebase
	case StepAgentCommitted:
		return to == StepGuardFired
	case StepGuardFired:
		return to == StepPROpened || to == StepAgentCommitted // guard can reject → recommit
	case StepPROpened:
		return to == StepReviewComplete
	case StepReviewComplete:
		return to == StepAdversarialDone || to == StepCoApproved // adversarial can be skipped
	case StepAdversarialDone:
		return to == StepPromptFooPassed || to == StepCoApproved
	case StepPromptFooPassed:
		return to == StepCoApproved
	case StepCoApproved:
		return to == StepMergedDeployed
	case StepMergedDeployed:
		return false // terminal — no further transitions
	// Failure states: can retry back to specific steps
	case StateBlocked:
		return to == StepAgentWriting || to == StepPROpened || to == StepReviewComplete
	case StateEscalated:
		return to == StepCoApproved // human override
	case StateFailed:
		return false // terminal failure
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// Step metadata (spec §2.3 latency budgets)
// ---------------------------------------------------------------------------

// GetStepInfo returns metadata for a given step.
func GetStepInfo(step StepID) StepInfo {
	switch step {
	case StepTaskCreated:
		return StepInfo{ID: step, Order: 1, Description: "Human creates task", Owner: "Human", MinLatency: "0s", TypicalLatency: "30s", MaxLatency: "5m"}
	case StepSwarmAssembled:
		return StepInfo{ID: step, Order: 2, Description: "Axiom assembles agent swarm", Owner: "Axiom", MinLatency: "5s", TypicalLatency: "30s", MaxLatency: "2m"}
	case StepWorktreeAcquired:
		return StepInfo{ID: step, Order: 3, Description: "Ralph Loop acquires lock + worktree", Owner: "Ralph Loop", MinLatency: "2s", TypicalLatency: "10s", MaxLatency: "30m (contention)"}
	case StepAgentWriting:
		return StepInfo{ID: step, Order: 4, Description: "Agent writes code in container", Owner: "Agent", MinLatency: "2m", TypicalLatency: "15m", MaxLatency: "2h"}
	case StepAgentCommitted:
		return StepInfo{ID: step, Order: 5, Description: "Agent commits with attestation", Owner: "Agent", MinLatency: "1s", TypicalLatency: "5s", MaxLatency: "30s"}
	case StepGuardFired:
		return StepInfo{ID: step, Order: 6, Description: "GitReins pre-receive hook fires", Owner: "GitReins", MinLatency: "5s", TypicalLatency: "30s", MaxLatency: "5m"}
	case StepPROpened:
		return StepInfo{ID: step, Order: 7, Description: "Agent opens PR with metadata", Owner: "Agent", MinLatency: "2s", TypicalLatency: "10s", MaxLatency: "1m"}
	case StepReviewComplete:
		return StepInfo{ID: step, Order: 8, Description: "Chimera runs multi-model review", Owner: "Chimera", MinLatency: "10s", TypicalLatency: "2m", MaxLatency: "10m"}
	case StepAdversarialDone:
		return StepInfo{ID: step, Order: 9, Description: "Conscientiousness adversarial self-eval", Owner: "Conscientiousness", MinLatency: "30s", TypicalLatency: "5m", MaxLatency: "15m"}
	case StepPromptFooPassed:
		return StepInfo{ID: step, Order: 10, Description: "PromptFoo CI verifies no regression", Owner: "PromptFoo CI", MinLatency: "10s", TypicalLatency: "1m", MaxLatency: "5m"}
	case StepCoApproved:
		return StepInfo{ID: step, Order: 11, Description: "Human + Agent co-approve", Owner: "Human + Agent", MinLatency: "5m", TypicalLatency: "30m", MaxLatency: "4h"}
	case StepMergedDeployed:
		return StepInfo{ID: step, Order: 12, Description: "Merge + Deploy", Owner: "System", MinLatency: "5s", TypicalLatency: "2m", MaxLatency: "10m"}
	case StateFailed:
		return StepInfo{ID: step, Order: -1, Description: "Pipeline failed", Owner: "System", MinLatency: "-", TypicalLatency: "-", MaxLatency: "-"}
	case StateEscalated:
		return StepInfo{ID: step, Order: -1, Description: "Escalated to human", Owner: "Human", MinLatency: "-", TypicalLatency: "-", MaxLatency: "-"}
	case StateBlocked:
		return StepInfo{ID: step, Order: -1, Description: "Blocked — waiting for resolution", Owner: "System", MinLatency: "-", TypicalLatency: "-", MaxLatency: "-"}
	default:
		return StepInfo{ID: step, Order: 0, Description: "Unknown step", Owner: "Unknown"}
	}
}

// AllSteps returns all defined step IDs in order.
func AllSteps() []StepID {
	return []StepID{
		StepIdle,
		StepTaskCreated,
		StepSwarmAssembled,
		StepWorktreeAcquired,
		StepAgentWriting,
		StepAgentCommitted,
		StepGuardFired,
		StepPROpened,
		StepReviewComplete,
		StepAdversarialDone,
		StepPromptFooPassed,
		StepCoApproved,
		StepMergedDeployed,
	}
}

// ---------------------------------------------------------------------------
// Persistence (spec §2.2 — states are persisted for crash recovery)
// ---------------------------------------------------------------------------

// PersistState writes the pipeline state to a JSON file.
func (sm *PipelineStateMachine) PersistState(path string) error {
	if path == "" {
		return fmt.Errorf("persist path is empty")
	}
	data, err := json.MarshalIndent(sm.state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	dir := filepath.Dir(path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create state dir: %w", err)
		}
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write state file: %w", err)
	}
	return nil
}

// LoadState reads pipeline state from a JSON file.
func LoadState(path string) (*PipelineStateMachine, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read state file: %w", err)
	}
	var state PipelineState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}
	return &PipelineStateMachine{state: state}, nil
}

// ---------------------------------------------------------------------------
// Data contracts (spec §2.2 per-step input/output)
// ---------------------------------------------------------------------------

// GetDataContract returns the data contract for a step.
func GetDataContract(step StepID) DataContract {
	switch step {
	case StepTaskCreated:
		return DataContract{
			InputFields:  []string{"title", "description", "spec_ref"},
			OutputFields: []string{"task_id", "task_metadata"},
		}
	case StepSwarmAssembled:
		return DataContract{
			InputFields:  []string{"task_id"},
			OutputFields: []string{"agent_assignments", "worktree_path", "branch_name"},
		}
	case StepWorktreeAcquired:
		return DataContract{
			InputFields:  []string{"agent_id", "repo_url"},
			OutputFields: []string{"worktree_path", "branch_name", "lock_acquired"},
		}
	case StepAgentWriting:
		return DataContract{
			InputFields:  []string{"worktree_path", "branch_name", "plan_ref"},
			OutputFields: []string{"modified_files"},
		}
	case StepAgentCommitted:
		return DataContract{
			InputFields:  []string{"worktree_path", "modified_files"},
			OutputFields: []string{"commit_sha", "attestation_hash"},
		}
	case StepGuardFired:
		return DataContract{
			InputFields:  []string{"commit_sha", "branch_name"},
			OutputFields: []string{"guard_result", "tier1_passed"},
		}
	case StepPROpened:
		return DataContract{
			InputFields:  []string{"commit_sha", "branch_name", "spec_ref"},
			OutputFields: []string{"pr_url", "pr_number"},
		}
	case StepReviewComplete:
		return DataContract{
			InputFields:  []string{"pr_url", "diff"},
			OutputFields: []string{"review_verdict", "review_findings", "evidence_bundle"},
		}
	case StepAdversarialDone:
		return DataContract{
			InputFields:  []string{"pr_url", "review_verdict"},
			OutputFields: []string{"adversarial_report", "attack_vectors"},
		}
	case StepPromptFooPassed:
		return DataContract{
			InputFields:  []string{"pr_url", "affected_prompts"},
			OutputFields: []string{"promptfoo_results", "regression_detected"},
		}
	case StepCoApproved:
		return DataContract{
			InputFields:  []string{"pr_url", "review_verdict", "adversarial_report"},
			OutputFields: []string{"human_approved", "agent_approved", "approval_timestamp"},
		}
	case StepMergedDeployed:
		return DataContract{
			InputFields:  []string{"pr_url", "approval_data"},
			OutputFields: []string{"merge_sha", "deployment_status"},
		}
	default:
		return DataContract{}
	}
}
