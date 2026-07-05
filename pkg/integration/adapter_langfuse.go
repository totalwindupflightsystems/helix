package integration

// ---------------------------------------------------------------------------
// LangFuse Adapter — specs/integrations.md §9 + cross-component-wiring.md §3.1
// ---------------------------------------------------------------------------
//
// LangFuse is the observability layer. Chimera posts deliberation traces,
// GitReins posts evaluation results, and Axiom posts work-item traces.
// All LLM costs flow through LangFuse for cross-component budget tracking.
// Muster auto-generates tools for LangFuse's OpenAPI spec.

// LangFuseAdapter defines the contract for LLM observability tracing.
type LangFuseAdapter interface {
	// IngestTrace posts a trace (prompt, completion, metadata) for
	// cost tracking and debugging. Called by Chimera after deliberation,
	// GitReins after Tier 2 evaluation, and Axiom after work-item completion.
	IngestTrace(trace LangFuseTrace) (*LangFuseIngestResult, error)

	// GetTrace retrieves a previously ingested trace by ID.
	GetTrace(traceID string) (*LangFuseTrace, error)

	// ListTraces returns traces filtered by project and optional time range.
	ListTraces(project string, opts TraceFilter) ([]LangFuseTrace, error)

	// Health returns service health.
	Health() (*LangFuseHealth, error)
}

// LangFuseTrace represents a single LLM interaction trace per spec §8.2.
type LangFuseTrace struct {
	ID           string
	Name         string
	Project      string
	UserID       string // spec §8.2: userId (e.g. "agent-sandbox-7@helix")
	SessionID    string // spec §8.2: sessionId (e.g. "pr-1842")
	Input        string // Prompt text (trace-level, backward compat)
	Output       string // Completion text (trace-level, backward compat)
	Model        string
	Provider     string
	Usage        LangFuseUsage
	Cost         float64
	Metadata     map[string]string
	Tags         []string              // spec §8.2: tags (e.g. ["implementation", "go"])
	Generations  []LangFuseGeneration  // spec §8.2: generations[]
	Observations []LangFuseObservation // spec §8.2: observations[]
	Timestamp    string
}

// LangFuseGeneration represents a single LLM generation within a trace (spec §8.2).
type LangFuseGeneration struct {
	Name       string
	Model      string
	Input      string
	Output     string
	Usage      LangFuseUsage
	Cost       float64
	DurationMs int64
}

// LangFuseObservation represents a non-LLM observation within a trace (spec §8.2).
type LangFuseObservation struct {
	Name       string
	Type       string // "SPAN" or "EVENT"
	Input      string
	Output     string
	DurationMs int64
}

// LangFuseUsage breaks down token usage for a trace.
type LangFuseUsage struct {
	InputTokens      int
	OutputTokens     int
	TotalTokens      int
	CacheReadTokens  int
	CacheWriteTokens int
}

// TraceFilter limits trace queries by time range.
type TraceFilter struct {
	From   string // ISO 8601 start
	To     string // ISO 8601 end
	Limit  int
	Offset int
}

// LangFuseIngestResult confirms trace ingestion.
type LangFuseIngestResult struct {
	ID     string
	Status string // "accepted", "queued", "rejected"
}

// LangFuseHealth reports LangFuse's operational status.
type LangFuseHealth struct {
	Status  string
	Version string
	Uptime  float64
}
