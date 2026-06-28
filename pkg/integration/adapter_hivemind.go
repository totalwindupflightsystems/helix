package integration

// ---------------------------------------------------------------------------
// Hivemind Adapter — specs/integrations.md §7
// ---------------------------------------------------------------------------
//
// Hivemind provides persistent agent memory + task scheduling. IAM/auth, git
// operations, Ralph Loop engine, SQLite + YAML memory bank, inbox/compiled
// pattern, hierarchical rate limiting. The shared blackboard that agents
// read and write.

// HivemindAdapter defines the contract for Hivemind memory and task scheduling.
type HivemindAdapter interface {
	// ScheduleTask queues a task for agent execution.
	ScheduleTask(task HiveTask) (*HiveTask, error)

	// ClaimTask acquires the next available task for an agent.
	ClaimTask(agentName string) (*HiveTask, error)

	// CompleteTask marks a task as done with results.
	CompleteTask(taskID string, result TaskResult) error

	// ReadMemory reads from the shared memory bank.
	ReadMemory(key string) (*MemoryEntry, error)

	// WriteMemory writes to the shared memory bank.
	WriteMemory(key string, content string, domain string) error

	// Health returns service health.
	Health() (*HivemindHealth, error)
}

// HiveTask represents a scheduled task for agent execution.
type HiveTask struct {
	ID           string
	Title        string
	Description  string
	Priority     string // "high", "medium", "low"
	Status       string // "queued", "claimed", "in_progress", "complete", "failed"
	AssignedTo   string
	CreatedAt    string
	Deadline     string
	Dependencies []string // Task IDs that must complete first
}

// TaskResult captures the outcome of a completed task.
type TaskResult struct {
	Success  bool
	Output   string
	Evidence string // Path to evidence bundle
	Cost     float64
	Duration float64
}

// MemoryEntry represents a single key-value pair in the shared memory bank.
type MemoryEntry struct {
	Key       string
	Content   string
	Domain    string
	UpdatedAt string
	Version   int
}

// HivemindHealth reports Hivemind's operational status.
type HivemindHealth struct {
	Status      string
	TasksQueued int
	TasksActive int
	MemorySize  int64
	Uptime      float64
}
