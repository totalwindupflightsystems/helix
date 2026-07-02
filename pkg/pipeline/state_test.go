package pipeline

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStepID_IsValid(t *testing.T) {
	valid := []StepID{
		StepIdle, StepTaskCreated, StepSwarmAssembled, StepWorktreeAcquired,
		StepAgentWriting, StepAgentCommitted, StepGuardFired, StepPROpened,
		StepReviewComplete, StepAdversarialDone, StepPromptFooPassed,
		StepCoApproved, StepMergedDeployed,
		StateFailed, StateEscalated, StateBlocked,
	}
	for _, s := range valid {
		if !s.IsValid() {
			t.Errorf("StepID(%q).IsValid() = false, want true", s)
		}
	}

	invalid := []StepID{"", "unknown", "invalid_step"}
	for _, s := range invalid {
		if s.IsValid() {
			t.Errorf("StepID(%q).IsValid() = true, want false", s)
		}
	}
}

func TestStepID_IsTerminal(t *testing.T) {
	if !StepMergedDeployed.IsTerminal() {
		t.Error("StepMergedDeployed should be terminal")
	}
	if !StateFailed.IsTerminal() {
		t.Error("StateFailed should be terminal")
	}
	if StepTaskCreated.IsTerminal() {
		t.Error("StepTaskCreated should not be terminal")
	}
}

func TestStepID_IsFailed(t *testing.T) {
	failedStates := []StepID{StateFailed, StateEscalated, StateBlocked}
	for _, s := range failedStates {
		if !s.IsFailed() {
			t.Errorf("StepID(%q).IsFailed() = false, want true", s)
		}
	}

	if StepTaskCreated.IsFailed() {
		t.Error("StepTaskCreated should not be failed")
	}
}

func TestStepID_IsBlocked(t *testing.T) {
	if !StateBlocked.IsBlocked() {
		t.Error("StateBlocked should be blocked")
	}
	if !StateEscalated.IsBlocked() {
		t.Error("StateEscalated should be blocked")
	}
	if StepTaskCreated.IsBlocked() {
		t.Error("StepTaskCreated should not be blocked")
	}
}

func TestNewPipelineStateMachine(t *testing.T) {
	sm := NewPipelineStateMachine("pr-123")
	if sm.CurrentStep() != StepIdle {
		t.Errorf("initial step = %q, want %q", sm.CurrentStep(), StepIdle)
	}
	if sm.State().PRID != "pr-123" {
		t.Errorf("PRID = %q, want 'pr-123'", sm.State().PRID)
	}
}

func TestPipelineStateMachine_CanTransition(t *testing.T) {
	sm := NewPipelineStateMachine("pr-1")

	// Valid: idle → task_created
	if !sm.CanTransition(StepTaskCreated) {
		t.Error("idle → task_created should be valid")
	}

	// Invalid: idle → merged_deployed (skip steps)
	if sm.CanTransition(StepMergedDeployed) {
		t.Error("idle → merged_deployed should be invalid")
	}
}

func TestPipelineStateMachine_Transition_Success(t *testing.T) {
	sm := NewPipelineStateMachine("pr-1")

	err := sm.Transition(StepTaskCreated, "task created by human")
	if err != nil {
		t.Fatalf("Transition to task_created failed: %v", err)
	}
	if sm.CurrentStep() != StepTaskCreated {
		t.Errorf("step = %q, want %q", sm.CurrentStep(), StepTaskCreated)
	}

	// Check history
	history := sm.History()
	if len(history) != 1 {
		t.Fatalf("history length = %d, want 1", len(history))
	}
	if history[0].From != StepIdle || history[0].To != StepTaskCreated {
		t.Errorf("history entry = %+v", history[0])
	}
}

func TestPipelineStateMachine_Transition_Invalid(t *testing.T) {
	sm := NewPipelineStateMachine("pr-1")

	err := sm.Transition(StepMergedDeployed, "skip")
	if err == nil {
		t.Error("invalid transition should return error")
	}
}

func TestPipelineStateMachine_Transition_AnyToFailure(t *testing.T) {
	sm := NewPipelineStateMachine("pr-1")
	_ = sm.Transition(StepTaskCreated, "step 1")

	// Any step → failed is valid
	err := sm.Transition(StateFailed, "agent crashed")
	if err != nil {
		t.Fatalf("transition to failed should succeed: %v", err)
	}
	if !sm.IsFailed() {
		t.Error("pipeline should be in failed state")
	}
	if !sm.IsComplete() {
		t.Error("failed state should be terminal/complete")
	}
}

func TestPipelineStateMachine_FullHappyPath(t *testing.T) {
	sm := NewPipelineStateMachine("pr-1")

	steps := []struct {
		target StepID
		reason string
	}{
		{StepTaskCreated, "human creates task"},
		{StepSwarmAssembled, "axiom assembles swarm"},
		{StepWorktreeAcquired, "ralph loop acquires lock"},
		{StepAgentWriting, "agent starts writing"},
		{StepAgentCommitted, "agent commits"},
		{StepGuardFired, "gitreins guard fires"},
		{StepPROpened, "agent opens PR"},
		{StepReviewComplete, "chimera review done"},
		{StepAdversarialDone, "conscientiousness done"},
		{StepPromptFooPassed, "promptfoo passes"},
		{StepCoApproved, "human + agent approve"},
		{StepMergedDeployed, "merged and deployed"},
	}

	for _, s := range steps {
		err := sm.Transition(s.target, s.reason)
		if err != nil {
			t.Fatalf("Transition to %s failed: %v", s.target, err)
		}
	}

	if !sm.IsComplete() {
		t.Error("pipeline should be complete after merged_deployed")
	}
	if sm.IsFailed() {
		t.Error("happy path should not be failed")
	}
	if len(sm.History()) != len(steps) {
		t.Errorf("history length = %d, want %d", len(sm.History()), len(steps))
	}
}

func TestPipelineStateMachine_SkipAdversarial(t *testing.T) {
	sm := NewPipelineStateMachine("pr-1")
	_ = sm.Transition(StepTaskCreated, "")
	_ = sm.Transition(StepSwarmAssembled, "")
	_ = sm.Transition(StepWorktreeAcquired, "")
	_ = sm.Transition(StepAgentWriting, "")
	_ = sm.Transition(StepAgentCommitted, "")
	_ = sm.Transition(StepGuardFired, "")
	_ = sm.Transition(StepPROpened, "")
	_ = sm.Transition(StepReviewComplete, "")

	// Can skip adversarial and go directly to co_approved
	err := sm.Transition(StepCoApproved, "adversarial skipped — trusted agent")
	if err != nil {
		t.Fatalf("review_complete → co_approved should be valid: %v", err)
	}
}

func TestPipelineStateMachine_GuardReject(t *testing.T) {
	sm := NewPipelineStateMachine("pr-1")
	_ = sm.Transition(StepTaskCreated, "")
	_ = sm.Transition(StepSwarmAssembled, "")
	_ = sm.Transition(StepWorktreeAcquired, "")
	_ = sm.Transition(StepAgentWriting, "")
	_ = sm.Transition(StepAgentCommitted, "")
	_ = sm.Transition(StepGuardFired, "")

	// Guard can reject → back to agent_committed
	err := sm.Transition(StepAgentCommitted, "guard rejected, recommit needed")
	if err != nil {
		t.Fatalf("guard_fired → agent_committed should be valid: %v", err)
	}
}

func TestPipelineStateMachine_Rebase(t *testing.T) {
	sm := NewPipelineStateMachine("pr-1")
	_ = sm.Transition(StepTaskCreated, "")
	_ = sm.Transition(StepSwarmAssembled, "")
	_ = sm.Transition(StepWorktreeAcquired, "")
	_ = sm.Transition(StepAgentWriting, "")

	// Agent can re-enter agent_writing (rebase loop)
	err := sm.Transition(StepAgentWriting, "rebase needed")
	if err != nil {
		t.Fatalf("agent_writing → agent_writing (rebase) should be valid: %v", err)
	}
}

func TestPipelineStateMachine_BlockedRetry(t *testing.T) {
	sm := NewPipelineStateMachine("pr-1")
	_ = sm.Transition(StepTaskCreated, "")
	_ = sm.Transition(StepSwarmAssembled, "")
	_ = sm.Transition(StepWorktreeAcquired, "")
	_ = sm.Transition(StepAgentWriting, "")
	_ = sm.Transition(StateBlocked, "merge conflict")

	if !sm.IsBlocked() {
		t.Error("pipeline should be blocked")
	}

	// Can retry from blocked → agent_writing
	err := sm.Transition(StepAgentWriting, "retry after rebase")
	if err != nil {
		t.Fatalf("blocked → agent_writing should be valid: %v", err)
	}
}

func TestPipelineStateMachine_EscalatedOverride(t *testing.T) {
	sm := NewPipelineStateMachine("pr-1")
	_ = sm.Transition(StepTaskCreated, "")
	_ = sm.Transition(StepSwarmAssembled, "")
	_ = sm.Transition(StepWorktreeAcquired, "")
	_ = sm.Transition(StepAgentWriting, "")
	_ = sm.Transition(StepAgentCommitted, "")
	_ = sm.Transition(StepGuardFired, "")
	_ = sm.Transition(StepPROpened, "")
	_ = sm.Transition(StepReviewComplete, "")
	_ = sm.Transition(StateEscalated, "human escalation")

	// Can override from escalated → co_approved
	err := sm.Transition(StepCoApproved, "human override")
	if err != nil {
		t.Fatalf("escalated → co_approved should be valid: %v", err)
	}
}

func TestPipelineStateMachine_DataOperations(t *testing.T) {
	sm := NewPipelineStateMachine("pr-1")

	sm.SetData("task_id", "task-456")
	sm.SetData("worktree", "/worktrees/agent-1")

	if sm.GetData("task_id") != "task-456" {
		t.Errorf("GetData('task_id') = %q, want 'task-456'", sm.GetData("task_id"))
	}
	if sm.GetData("nonexistent") != "" {
		t.Error("GetData on missing key should return empty string")
	}
}

func TestPipelineStateMachine_Elapsed(t *testing.T) {
	sm := NewPipelineStateMachine("pr-1")
	elapsed := sm.Elapsed()
	if elapsed < 0 {
		t.Error("Elapsed should be non-negative")
	}
}

func TestGetStepInfo(t *testing.T) {
	info := GetStepInfo(StepTaskCreated)
	if info.Order != 1 {
		t.Errorf("StepTaskCreated order = %d, want 1", info.Order)
	}
	if info.Description == "" {
		t.Error("StepTaskCreated description should not be empty")
	}

	info = GetStepInfo(StepMergedDeployed)
	if info.Order != 12 {
		t.Errorf("StepMergedDeployed order = %d, want 12", info.Order)
	}

	info = GetStepInfo(StepID("invalid"))
	if info.Order != 0 {
		t.Error("invalid step should have order 0")
	}
}

func TestAllSteps(t *testing.T) {
	steps := AllSteps()
	if len(steps) != 13 {
		t.Errorf("AllSteps returned %d, want 13", len(steps))
	}
	if steps[0] != StepIdle {
		t.Errorf("first step = %q, want %q", steps[0], StepIdle)
	}
	if steps[len(steps)-1] != StepMergedDeployed {
		t.Errorf("last step = %q, want %q", steps[len(steps)-1], StepMergedDeployed)
	}
}

func TestPipelineStateMachine_PersistState(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "pipeline-state.json")

	sm := NewPipelineStateMachine("pr-99")
	_ = sm.Transition(StepTaskCreated, "test")
	sm.SetData("task_id", "task-1")

	err := sm.PersistState(path)
	if err != nil {
		t.Fatalf("PersistState failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file not created: %v", err)
	}
}

func TestPipelineStateMachine_PersistState_EmptyPath(t *testing.T) {
	sm := NewPipelineStateMachine("pr-1")
	err := sm.PersistState("")
	if err == nil {
		t.Error("PersistState with empty path should return error")
	}
}

func TestPipelineStateMachine_PersistState_CreateDir(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "subdir", "nested", "state.json")

	sm := NewPipelineStateMachine("pr-1")
	err := sm.PersistState(path)
	if err != nil {
		t.Fatalf("PersistState should create nested dirs: %v", err)
	}
}

func TestLoadState(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "state.json")

	// Persist
	original := NewPipelineStateMachine("pr-777")
	_ = original.Transition(StepTaskCreated, "persist test")
	original.SetData("key", "value")

	if err := original.PersistState(path); err != nil {
		t.Fatalf("PersistState failed: %v", err)
	}

	// Load
	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	if loaded.CurrentStep() != StepTaskCreated {
		t.Errorf("loaded step = %q, want %q", loaded.CurrentStep(), StepTaskCreated)
	}
	if loaded.State().PRID != "pr-777" {
		t.Errorf("loaded PRID = %q, want 'pr-777'", loaded.State().PRID)
	}
	if loaded.GetData("key") != "value" {
		t.Errorf("loaded data = %q, want 'value'", loaded.GetData("key"))
	}
}

func TestLoadState_NotFound(t *testing.T) {
	_, err := LoadState("/nonexistent/path/state.json")
	if err == nil {
		t.Error("LoadState on nonexistent file should return error")
	}
}

func TestGetDataContract(t *testing.T) {
	contract := GetDataContract(StepTaskCreated)
	if len(contract.InputFields) == 0 {
		t.Error("StepTaskCreated should have input fields")
	}
	if len(contract.OutputFields) == 0 {
		t.Error("StepTaskCreated should have output fields")
	}

	// All steps should have contracts (except idle)
	steps := []StepID{
		StepTaskCreated, StepSwarmAssembled, StepWorktreeAcquired,
		StepAgentWriting, StepAgentCommitted, StepGuardFired,
		StepPROpened, StepReviewComplete, StepAdversarialDone,
		StepPromptFooPassed, StepCoApproved, StepMergedDeployed,
	}
	for _, step := range steps {
		c := GetDataContract(step)
		if len(c.OutputFields) == 0 {
			t.Errorf("step %q has no output fields in data contract", step)
		}
	}

	// Unknown step
	c := GetDataContract(StepID("unknown"))
	if len(c.InputFields) != 0 || len(c.OutputFields) != 0 {
		t.Error("unknown step should have empty data contract")
	}
}

func TestPipelineStateMachine_TerminalNoTransitions(t *testing.T) {
	sm := NewPipelineStateMachine("pr-1")
	// Fast-forward to merged
	for _, step := range []StepID{
		StepTaskCreated, StepSwarmAssembled, StepWorktreeAcquired,
		StepAgentWriting, StepAgentCommitted, StepGuardFired,
		StepPROpened, StepReviewComplete, StepAdversarialDone,
		StepPromptFooPassed, StepCoApproved, StepMergedDeployed,
	} {
		if err := sm.Transition(step, ""); err != nil {
			t.Fatalf("transition to %s: %v", step, err)
		}
	}

	// Can't transition from terminal
	err := sm.Transition(StepTaskCreated, "can't go back")
	if err == nil {
		t.Error("transition from terminal state should fail")
	}
}

func TestPipelineStateMachine_StateFailedTerminal(t *testing.T) {
	sm := NewPipelineStateMachine("pr-1")
	_ = sm.Transition(StateFailed, "immediate failure")

	err := sm.Transition(StepTaskCreated, "can't recover from failed")
	if err == nil {
		t.Error("transition from StateFailed should fail")
	}
}

func TestHistory_ReturnsCopy(t *testing.T) {
	sm := NewPipelineStateMachine("pr-1")
	_ = sm.Transition(StepTaskCreated, "")

	history := sm.History()
	if len(history) != 1 {
		t.Fatalf("history length = %d, want 1", len(history))
	}

	// Modify the copy
	history[0].Reason = "MODIFIED"

	// Original should be unchanged
	if sm.History()[0].Reason == "MODIFIED" {
		t.Error("History() should return a copy, not a reference")
	}
}

func TestPipelineStateMachine_StartedAt(t *testing.T) {
	before := time.Now()
	sm := NewPipelineStateMachine("pr-1")
	after := time.Now()

	if sm.State().StartedAt.Before(before) || sm.State().StartedAt.After(after) {
		t.Error("StartedAt should be between creation time bounds")
	}
}

func TestGetStepInfo_AllSteps(t *testing.T) {
	steps := []struct {
		step     StepID
		order    int
		notEmpty bool
	}{
		{StepTaskCreated, 1, true},
		{StepSwarmAssembled, 2, true},
		{StepWorktreeAcquired, 3, true},
		{StepAgentWriting, 4, true},
		{StepAgentCommitted, 5, true},
		{StepGuardFired, 6, true},
		{StepPROpened, 7, true},
		{StepReviewComplete, 8, true},
		{StepAdversarialDone, 9, true},
		{StepPromptFooPassed, 10, true},
		{StepCoApproved, 11, true},
		{StepMergedDeployed, 12, true},
		{StateFailed, -1, true},
		{StateEscalated, -1, true},
		{StateBlocked, -1, true},
	}
	for _, tt := range steps {
		info := GetStepInfo(tt.step)
		if info.Order != tt.order {
			t.Errorf("GetStepInfo(%q).Order = %d, want %d", tt.step, info.Order, tt.order)
		}
		if tt.notEmpty && info.Description == "" {
			t.Errorf("GetStepInfo(%q).Description should not be empty", tt.step)
		}
	}
}
