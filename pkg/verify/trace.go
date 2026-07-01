package verify

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Deployment Trace Pipeline
//
// Per spec production-verification.md §Integration Points:
// "LangFuse: Full trace of agent → merge → shadow → canary → production → incident"
//
// Each deployment stage (commit, guard, merge, shadow, canary, production,
// incident) is recorded as a TraceSpan. The pipeline stores spans in order,
// allowing export to LangFuse for end-to-end observability.
// ---------------------------------------------------------------------------

// TraceStage identifies a lifecycle stage in the deployment trace.
type TraceStage string

const (
	StageCommit     TraceStage = "commit"     // agent code commit
	StageGuard      TraceStage = "guard"      // GitReins guard run
	StageReview     TraceStage = "review"     // adversarial review
	StageMerge      TraceStage = "merge"      // merge to main
	StageShadow     TraceStage = "shadow"     // dark launch verification
	StageCanary     TraceStage = "canary"     // partial traffic ramp
	StageProduction TraceStage = "production" // full traffic deployment
	StageIncident   TraceStage = "incident"   // production incident
)

// TraceStatus indicates the outcome of a trace span.
type TraceStatus string

const (
	TraceStatusSuccess TraceStatus = "success"
	TraceStatusFailed  TraceStatus = "failed"
	TraceStatusSkipped TraceStatus = "skipped"
	TraceStatusActive  TraceStatus = "active"
)

// TraceSpan represents a single lifecycle stage in the deployment pipeline.
type TraceSpan struct {
	ID          string            `json:"id"`
	Stage       TraceStage        `json:"stage"`
	Status      TraceStatus       `json:"status"`
	AgentID     string            `json:"agent_id"`
	StartedAt   time.Time         `json:"started_at"`
	EndedAt     time.Time         `json:"ended_at,omitempty"`
	Duration    time.Duration     `json:"duration,omitempty"`
	Cost        float64           `json:"cost,omitempty"`
	Evidence    map[string]string `json:"evidence,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Description string            `json:"description,omitempty"`
}

// DurationMs returns the span duration in milliseconds.
func (s *TraceSpan) DurationMs() float64 {
	if s.EndedAt.IsZero() || s.StartedAt.IsZero() {
		return 0
	}
	return float64(s.EndedAt.Sub(s.StartedAt).Microseconds()) / 1000.0
}

// IsComplete returns true if the span has both a start and end time.
func (s *TraceSpan) IsComplete() bool {
	return !s.StartedAt.IsZero() && !s.EndedAt.IsZero()
}

// DeploymentTrace is the full lifecycle trace for a single deployment.
type DeploymentTrace struct {
	mu    sync.RWMutex
	ID    string      `json:"id"`
	Spans []TraceSpan `json:"spans"`
}

// DeploymentTracePipeline records every lifecycle stage of a deployment as a
// trace span and enables export to LangFuse for full observability.
type DeploymentTracePipeline struct {
	mu     sync.RWMutex
	traces map[string]*DeploymentTrace // traceID → trace
}

// NewDeploymentTracePipeline creates an empty pipeline.
func NewDeploymentTracePipeline() *DeploymentTracePipeline {
	return &DeploymentTracePipeline{
		traces: make(map[string]*DeploymentTrace),
	}
}

// NewTrace creates and registers a new deployment trace, returning the trace ID.
// The first span (StageCommit) is recorded automatically from the provided
// agent ID and commit info.
func (p *DeploymentTracePipeline) NewTrace(agentID, commitSHA string) string {
	traceID := fmt.Sprintf("deploy-%s-%d", agentID, time.Now().UnixNano())

	trace := &DeploymentTrace{
		ID:    traceID,
		Spans: []TraceSpan{},
	}

	p.mu.Lock()
	p.traces[traceID] = trace
	p.mu.Unlock()

	// Record the commit span immediately.
	p.RecordSpan(traceID, SpanInput{
		Stage:   StageCommit,
		AgentID: agentID,
		Status:  TraceStatusSuccess,
		Evidence: map[string]string{
			"commit_sha": commitSHA,
		},
		Description: fmt.Sprintf("Agent %s committed code (%s)", agentID, shortSHA(commitSHA)),
	})

	return traceID
}

// SpanInput is the input for recording a trace span.
type SpanInput struct {
	Stage       TraceStage
	AgentID     string
	Status      TraceStatus
	StartedAt   time.Time
	Duration    time.Duration
	Cost        float64
	Evidence    map[string]string
	Metadata    map[string]string
	Description string
}

// RecordSpan adds a span to an existing trace. If StartedAt is zero, time.Now
// is used. If Duration is non-zero, EndedAt is computed automatically;
// otherwise EndedAt = StartedAt (point-in-time event).
func (p *DeploymentTracePipeline) RecordSpan(traceID string, input SpanInput) *TraceSpan {
	span := TraceSpan{
		ID:          fmt.Sprintf("span-%s-%d", traceID, time.Now().UnixNano()),
		Stage:       input.Stage,
		Status:      input.Status,
		AgentID:     input.AgentID,
		Cost:        input.Cost,
		Evidence:    input.Evidence,
		Metadata:    input.Metadata,
		Description: input.Description,
	}

	if input.StartedAt.IsZero() {
		span.StartedAt = time.Now().UTC()
	} else {
		span.StartedAt = input.StartedAt
	}

	if input.Duration > 0 {
		span.Duration = input.Duration
		span.EndedAt = span.StartedAt.Add(input.Duration)
	} else {
		span.EndedAt = span.StartedAt
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	trace, ok := p.traces[traceID]
	if !ok {
		return nil
	}

	trace.mu.Lock()
	trace.Spans = append(trace.Spans, span)
	trace.mu.Unlock()

	return &span
}

// RecordGuardSpan is a convenience method for recording a GitReins guard span.
func (p *DeploymentTracePipeline) RecordGuardSpan(traceID, agentID string, passed bool, duration time.Duration) *TraceSpan {
	status := TraceStatusSuccess
	if !passed {
		status = TraceStatusFailed
	}
	return p.RecordSpan(traceID, SpanInput{
		Stage:       StageGuard,
		AgentID:     agentID,
		Status:      status,
		Duration:    duration,
		Description: fmt.Sprintf("GitReins guard %s", statusVerb(status)),
	})
}

// RecordMergeSpan records a merge event.
func (p *DeploymentTracePipeline) RecordMergeSpan(traceID, agentID, mergeCommit string) *TraceSpan {
	return p.RecordSpan(traceID, SpanInput{
		Stage:   StageMerge,
		AgentID: agentID,
		Status:  TraceStatusSuccess,
		Evidence: map[string]string{
			"merge_commit": mergeCommit,
		},
		Description: fmt.Sprintf("Merged to main (%s)", shortSHA(mergeCommit)),
	})
}

// RecordShadowSpan records a shadow deployment result.
func (p *DeploymentTracePipeline) RecordShadowSpan(traceID, agentID string, deployment *ShadowDeployment) *TraceSpan {
	status := TraceStatusSuccess
	if deployment.GetState() == StateShadowFailed {
		status = TraceStatusFailed
	}
	return p.RecordSpan(traceID, SpanInput{
		Stage:   StageShadow,
		AgentID: agentID,
		Status:  status,
		Evidence: map[string]string{
			"state":    string(deployment.GetState()),
			"agent_id": deployment.AgentID,
		},
		Description: fmt.Sprintf("Shadow deployment: %s", deployment.GetState()),
	})
}

// RecordCanarySpan records a canary promotion result.
func (p *DeploymentTracePipeline) RecordCanarySpan(traceID, agentID string, percentage int, passed bool) *TraceSpan {
	status := TraceStatusSuccess
	if !passed {
		status = TraceStatusFailed
	}
	return p.RecordSpan(traceID, SpanInput{
		Stage:   StageCanary,
		AgentID: agentID,
		Status:  status,
		Evidence: map[string]string{
			"traffic_pct": fmt.Sprintf("%d", percentage),
		},
		Description: fmt.Sprintf("Canary at %d%% traffic", percentage),
	})
}

// RecordProductionSpan records a full production deployment.
func (p *DeploymentTracePipeline) RecordProductionSpan(traceID, agentID string) *TraceSpan {
	return p.RecordSpan(traceID, SpanInput{
		Stage:       StageProduction,
		AgentID:     agentID,
		Status:      TraceStatusSuccess,
		Description: "Promoted to full production",
	})
}

// RecordIncidentSpan records a production incident linked to the deployment.
func (p *DeploymentTracePipeline) RecordIncidentSpan(traceID, agentID string, breach *Breach) *TraceSpan {
	evidence := map[string]string{
		"contract":      breach.ContractName,
		"agent":         breach.Agent,
		"rollback":      fmt.Sprintf("%v", breach.ShouldRollback),
		"checks_failed": fmt.Sprintf("%d", breach.TotalChecks()),
	}
	if breach.MergeCommit != "" {
		evidence["merge_commit"] = breach.MergeCommit
	}
	return p.RecordSpan(traceID, SpanInput{
		Stage:       StageIncident,
		AgentID:     agentID,
		Status:      TraceStatusFailed,
		Evidence:    evidence,
		Description: fmt.Sprintf("Incident: contract %s breached (%d failures)", breach.ContractName, breach.TotalChecks()),
	})
}

// GetTrace returns the full trace by ID.
func (p *DeploymentTracePipeline) GetTrace(traceID string) *DeploymentTrace {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.traces[traceID]
}

// GetSpans returns a copy of the spans for a trace, sorted by start time.
func (p *DeploymentTracePipeline) GetSpans(traceID string) []TraceSpan {
	p.mu.RLock()
	trace, ok := p.traces[traceID]
	p.mu.RUnlock()
	if !ok {
		return nil
	}

	trace.mu.RLock()
	defer trace.mu.RUnlock()

	spans := make([]TraceSpan, len(trace.Spans))
	copy(spans, trace.Spans)
	sort.Slice(spans, func(i, j int) bool {
		return spans[i].StartedAt.Before(spans[j].StartedAt)
	})
	return spans
}

// GetSpanCount returns the number of spans in a trace.
func (p *DeploymentTracePipeline) GetSpanCount(traceID string) int {
	p.mu.RLock()
	trace, ok := p.traces[traceID]
	p.mu.RUnlock()
	if !ok {
		return 0
	}
	trace.mu.RLock()
	defer trace.mu.RUnlock()
	return len(trace.Spans)
}

// TotalCost returns the sum of all span costs for a trace.
func (p *DeploymentTracePipeline) TotalCost(traceID string) float64 {
	spans := p.GetSpans(traceID)
	var total float64
	for _, s := range spans {
		total += s.Cost
	}
	return total
}

// TotalDuration returns the wall-clock duration from the first span's start to
// the last span's end.
func (p *DeploymentTracePipeline) TotalDuration(traceID string) time.Duration {
	spans := p.GetSpans(traceID)
	if len(spans) == 0 {
		return 0
	}
	first := spans[0].StartedAt
	last := spans[len(spans)-1].EndedAt
	if last.IsZero() {
		last = spans[len(spans)-1].StartedAt
	}
	return last.Sub(first)
}

// HasStage returns true if the trace contains a span for the given stage.
func (p *DeploymentTracePipeline) HasStage(traceID string, stage TraceStage) bool {
	spans := p.GetSpans(traceID)
	for _, s := range spans {
		if s.Stage == stage {
			return true
		}
	}
	return false
}

// GetStageSpan returns the first span matching the given stage, or nil.
func (p *DeploymentTracePipeline) GetStageSpan(traceID string, stage TraceStage) *TraceSpan {
	spans := p.GetSpans(traceID)
	for i := range spans {
		if spans[i].Stage == stage {
			return &spans[i]
		}
	}
	return nil
}

// AllTraceIDs returns all trace IDs in the pipeline.
func (p *DeploymentTracePipeline) AllTraceIDs() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	ids := make([]string, 0, len(p.traces))
	for id := range p.traces {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// TraceSummary provides a compact view of a deployment trace.
type TraceSummary struct {
	TraceID       string        `json:"trace_id"`
	AgentID       string        `json:"agent_id"`
	StageCount    int           `json:"stage_count"`
	Stages        []TraceStage  `json:"stages"`
	TotalCost     float64       `json:"total_cost"`
	TotalDuration time.Duration `json:"total_duration"`
	HasIncident   bool          `json:"has_incident"`
	FinalStage    TraceStage    `json:"final_stage"`
	FinalStatus   TraceStatus   `json:"final_status"`
	IsComplete    bool          `json:"is_complete"`
}

// GetSummary returns a summary of a trace.
func (p *DeploymentTracePipeline) GetSummary(traceID string) *TraceSummary {
	spans := p.GetSpans(traceID)
	if len(spans) == 0 {
		return nil
	}

	var stages []TraceStage
	var hasIncident bool
	var agentID string

	for _, s := range spans {
		stages = append(stages, s.Stage)
		if s.Stage == StageIncident {
			hasIncident = true
		}
		if agentID == "" {
			agentID = s.AgentID
		}
	}

	last := spans[len(spans)-1]
	isComplete := last.Stage == StageProduction || last.Stage == StageIncident

	return &TraceSummary{
		TraceID:       traceID,
		AgentID:       agentID,
		StageCount:    len(spans),
		Stages:        stages,
		TotalCost:     p.TotalCost(traceID),
		TotalDuration: p.TotalDuration(traceID),
		HasIncident:   hasIncident,
		FinalStage:    last.Stage,
		FinalStatus:   last.Status,
		IsComplete:    isComplete,
	}
}

// AllSummaries returns summaries for all traces in the pipeline.
func (p *DeploymentTracePipeline) AllSummaries() []*TraceSummary {
	ids := p.AllTraceIDs()
	summaries := make([]*TraceSummary, 0, len(ids))
	for _, id := range ids {
		s := p.GetSummary(id)
		if s != nil {
			summaries = append(summaries, s)
		}
	}
	return summaries
}

// ---------------------------------------------------------------------------
// LangFuse Export
// ---------------------------------------------------------------------------

// LangFuseSpanExport is the LangFuse-compatible representation of a single
// trace span. It maps to a LangFuse observation/event.
type LangFuseSpanExport struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"` // "helix:deploy:<stage>"
	TraceID   string            `json:"traceId"`
	Input     string            `json:"input,omitempty"`
	Output    string            `json:"output,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	StartTime string            `json:"startTime"`
	EndTime   string            `json:"endTime,omitempty"`
}

// LangFuseTraceExport is the top-level LangFuse trace containing all spans
// from a deployment lifecycle. It maps to the integration.LangFuseTrace type.
type LangFuseTraceExport struct {
	ID        string               `json:"id"`
	Name      string               `json:"name"`
	Project   string               `json:"project"`
	Input     string               `json:"input,omitempty"`
	Output    string               `json:"output,omitempty"`
	Metadata  map[string]string    `json:"metadata,omitempty"`
	Timestamp string               `json:"timestamp"`
	Spans     []LangFuseSpanExport `json:"spans,omitempty"`
}

// ExportTrace converts a deployment trace into a LangFuse-compatible format
// for ingestion via the LangFuse adapter. The trace name follows the
// convention "helix:deploy:<agent_id>".
func (p *DeploymentTracePipeline) ExportTrace(traceID string) *LangFuseTraceExport {
	spans := p.GetSpans(traceID)
	if len(spans) == 0 {
		return nil
	}

	var agentID string
	var firstStart time.Time
	for _, s := range spans {
		if agentID == "" {
			agentID = s.AgentID
		}
		if firstStart.IsZero() || s.StartedAt.Before(firstStart) {
			firstStart = s.StartedAt
		}
	}

	exportSpans := make([]LangFuseSpanExport, 0, len(spans))
	for _, s := range spans {
		span := LangFuseSpanExport{
			ID:        s.ID,
			Name:      fmt.Sprintf("helix:deploy:%s", s.Stage),
			TraceID:   traceID,
			Input:     s.Description,
			Output:    string(s.Status),
			Metadata:  make(map[string]string),
			StartTime: s.StartedAt.Format(time.RFC3339Nano),
		}
		if s.EndedAt.IsZero() {
			span.EndTime = s.StartedAt.Format(time.RFC3339Nano)
		} else {
			span.EndTime = s.EndedAt.Format(time.RFC3339Nano)
		}
		span.Metadata["stage"] = string(s.Stage)
		span.Metadata["status"] = string(s.Status)
		if s.Cost > 0 {
			span.Metadata["cost"] = fmt.Sprintf("%.4f", s.Cost)
		}
		if s.Duration > 0 {
			span.Metadata["duration_ms"] = fmt.Sprintf("%.2f", s.DurationMs())
		}
		// Merge evidence into metadata for LangFuse.
		for k, v := range s.Evidence {
			span.Metadata[k] = v
		}
		// Merge explicit metadata (takes precedence).
		for k, v := range s.Metadata {
			span.Metadata[k] = v
		}
		exportSpans = append(exportSpans, span)
	}

	output := "completed"
	summary := p.GetSummary(traceID)
	if summary != nil {
		if summary.HasIncident {
			output = "incident"
		} else if !summary.IsComplete {
			output = "in_progress"
		}
	}

	return &LangFuseTraceExport{
		ID:        traceID,
		Name:      fmt.Sprintf("helix:deploy:%s", agentID),
		Project:   "helix",
		Input:     fmt.Sprintf("Deployment trace for agent %s", agentID),
		Output:    output,
		Timestamp: firstStart.Format(time.RFC3339Nano),
		Metadata: map[string]string{
			"agent_id":   agentID,
			"span_count": fmt.Sprintf("%d", len(spans)),
			"platform":   "helix",
		},
		Spans: exportSpans,
	}
}

// ExportAllTraces converts all traces in the pipeline to LangFuse format.
func (p *DeploymentTracePipeline) ExportAllTraces() []*LangFuseTraceExport {
	ids := p.AllTraceIDs()
	exports := make([]*LangFuseTraceExport, 0, len(ids))
	for _, id := range ids {
		exp := p.ExportTrace(id)
		if exp != nil {
			exports = append(exports, exp)
		}
	}
	return exports
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// statusVerb returns a human-readable verb for a trace status.
func statusVerb(status TraceStatus) string {
	switch status {
	case TraceStatusSuccess:
		return "passed"
	case TraceStatusFailed:
		return "failed"
	case TraceStatusSkipped:
		return "skipped"
	case TraceStatusActive:
		return "active"
	default:
		return string(status)
	}
}
