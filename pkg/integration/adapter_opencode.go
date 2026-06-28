package integration

// ---------------------------------------------------------------------------
// OpenCode Adapter — specs/SPECIFICATION.md §3.14
// ---------------------------------------------------------------------------
//
// OpenCode is the agent-first coding executor (Layer 3). It provides HTTP API
// session management for agent code execution in Docker sandboxes. Each project
// gets its own OpenCode container (opencode-<project> on ports 4096-4102).
//
// OpenCode's role in Helix: executes coding tasks, runs build/test cycles,
// writes code in isolated worktrees. Axiom orchestrates it; Forgejo stores
// the output.

// OpenCodeAdapter defines the contract for OpenCode session management.
type OpenCodeAdapter interface {
	// CreateSession starts a new coding session for an agent on a project.
	CreateSession(project string, agent string, opts SessionOpts) (*Session, error)

	// SendPrompt sends a coding prompt to an active session.
	SendPrompt(sessionID string, prompt string) (*SessionResult, error)

	// GetSession returns the current state of a session.
	GetSession(sessionID string) (*Session, error)

	// ListSessions returns all active sessions.
	ListSessions() ([]Session, error)

	// CancelSession terminates a running session.
	CancelSession(sessionID string) error

	// Health returns service health.
	Health() (*OpenCodeHealth, error)
}

// SessionOpts configures a new OpenCode session.
type SessionOpts struct {
	Model      string // LLM model for this session
	WorkDir    string // Working directory inside the sandbox
	Timeout    int    // Session timeout in seconds
	MaxTurns   int    // Max reasoning turns before auto-cancel
}

// Session represents an active OpenCode coding session.
type Session struct {
	ID        string
	Project   string
	Agent     string
	Status    string // "active", "idle", "completed", "timed_out", "cancelled"
	Model     string
	Turns     int
	CreatedAt string
}

// SessionResult captures the outcome of a prompt sent to a session.
type SessionResult struct {
	Output   string
	Tokens   OpenCodeTokens
	Cost     float64
	Duration float64
	Status   string // "success", "error", "timeout"
}

// OpenCodeTokens breaks down token usage for a session result.
type OpenCodeTokens struct {
	InputTokens       int
	OutputTokens      int
	CacheReadTokens   int
	CacheWriteTokens  int
	TotalTokens       int
}

// OpenCodeHealth reports OpenCode's operational status.
type OpenCodeHealth struct {
	Status         string
	Version        string
	ActiveSessions int
	Uptime         float64
}
