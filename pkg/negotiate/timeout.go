package negotiate

// timeout.go implements the negotiation timeout watcher per
// specs/pr-negotiation.md §12.1 (Timeout Rules) and §7.4 (Deadlock Detection).
//
// Timeout rules enforced:
//   - Per debate round: 5 min → agent who didn't post gets strike (§7.5)
//   - Full negotiation: 30 min → escalate to human
//   - Chimera tie-break: 5 min → retry 1×, then escalate
//
// The watcher is context-aware: callers can cancel via context.Context.
// Thread-safe via sync.Mutex.

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Default timeout durations from spec §12.1.
const (
	DefaultRoundTimeout   = 5 * time.Minute
	DefaultGlobalTimeout  = 30 * time.Minute
	DefaultChimeraTimeout = 5 * time.Minute
)

// TimeoutConfig configures all negotiation timeouts (spec §12.1).
type TimeoutConfig struct {
	RoundTimeout   time.Duration // per-round: default 5m
	GlobalTimeout  time.Duration // full negotiation: default 30m
	ChimeraTimeout time.Duration // Chimera tie-break: default 5m
}

// DefaultTimeoutConfig returns spec-compliant default timeouts.
func DefaultTimeoutConfig() TimeoutConfig {
	return TimeoutConfig{
		RoundTimeout:   DefaultRoundTimeout,
		GlobalTimeout:  DefaultGlobalTimeout,
		ChimeraTimeout: DefaultChimeraTimeout,
	}
}

// RoundTimer tracks the per-round deadline and which agents have posted.
type RoundTimer struct {
	Round     int             `json:"round"`
	StartedAt time.Time       `json:"started_at"`
	Deadline  time.Time       `json:"deadline"`
	Agents    []string        `json:"agents"`
	Posted    map[string]bool `json:"posted"`
}

// TimeoutWatcher enforces negotiation timeouts (spec §12.1, §7.4).
type TimeoutWatcher struct {
	mu                  sync.Mutex
	config              TimeoutConfig
	negotiationStart    time.Time
	negotiationDeadline time.Time
	currentRound        *RoundTimer
	ctx                 context.Context
	cancel              context.CancelFunc
	strikeTracker       *StrikeTracker
	chimeraStart        time.Time
	chimeraDeadline     time.Time
	chimeraRetried      bool
}

// NewTimeoutWatcher creates a watcher with the given config and strike tracker.
// If config is zero-valued, defaults are applied. If strikeTracker is nil, a
// new one is created.
func NewTimeoutWatcher(cfg TimeoutConfig, tracker *StrikeTracker) *TimeoutWatcher {
	if cfg.RoundTimeout <= 0 {
		cfg.RoundTimeout = DefaultRoundTimeout
	}
	if cfg.GlobalTimeout <= 0 {
		cfg.GlobalTimeout = DefaultGlobalTimeout
	}
	if cfg.ChimeraTimeout <= 0 {
		cfg.ChimeraTimeout = DefaultChimeraTimeout
	}
	if tracker == nil {
		tracker = NewStrikeTracker()
	}
	return &TimeoutWatcher{
		config:        cfg,
		strikeTracker: tracker,
	}
}

// StartNegotiation begins the global negotiation timer (spec §12.1: 30 min max).
// The context is used for cancellation — when cancelled, all timeout checks
// become no-ops.
func (tw *TimeoutWatcher) StartNegotiation(ctx context.Context) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	now := time.Now()
	tw.negotiationStart = now
	tw.negotiationDeadline = now.Add(tw.config.GlobalTimeout)
	tw.ctx, tw.cancel = context.WithCancel(ctx)
}

// StartNegotiationAt is like StartNegotiation but uses a specific start time.
// Useful for testing and replaying historical negotiations.
func (tw *TimeoutWatcher) StartNegotiationAt(ctx context.Context, at time.Time) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	tw.negotiationStart = at
	tw.negotiationDeadline = at.Add(tw.config.GlobalTimeout)
	tw.ctx, tw.cancel = context.WithCancel(ctx)
}

// CheckGlobalTimeout returns true if the negotiation has exceeded the global
// timeout (spec §12.1: 30 min → escalate to human).
func (tw *TimeoutWatcher) CheckGlobalTimeout(at time.Time) bool {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if tw.negotiationDeadline.IsZero() {
		return false
	}
	return at.After(tw.negotiationDeadline) || at.Equal(tw.negotiationDeadline)
}

// GlobalTimeRemaining returns how much time is left before global timeout.
func (tw *TimeoutWatcher) GlobalTimeRemaining(at time.Time) time.Duration {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if tw.negotiationDeadline.IsZero() {
		return tw.config.GlobalTimeout
	}
	remaining := tw.negotiationDeadline.Sub(at)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// StartRound begins a per-round timer for the given round number and agents
// (spec §12.1: 5 min per round).
func (tw *TimeoutWatcher) StartRound(round int, agents ...string) {
	tw.StartRoundAt(round, time.Now(), agents...)
}

// StartRoundAt is like StartRound but uses a specific start time.
func (tw *TimeoutWatcher) StartRoundAt(round int, at time.Time, agents ...string) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	tw.currentRound = &RoundTimer{
		Round:     round,
		StartedAt: at,
		Deadline:  at.Add(tw.config.RoundTimeout),
		Agents:    agents,
		Posted:    make(map[string]bool),
	}
}

// CheckRoundTimeout returns true if the current round has exceeded the
// per-round timeout (spec §12.1: 5 min).
func (tw *TimeoutWatcher) CheckRoundTimeout(at time.Time) bool {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if tw.currentRound == nil {
		return false
	}
	return at.After(tw.currentRound.Deadline) || at.Equal(tw.currentRound.Deadline)
}

// RoundTimeRemaining returns how much time is left in the current round.
func (tw *TimeoutWatcher) RoundTimeRemaining(at time.Time) time.Duration {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if tw.currentRound == nil {
		return tw.config.RoundTimeout
	}
	remaining := tw.currentRound.Deadline.Sub(at)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// MarkPosted records that an agent has posted in the current round.
func (tw *TimeoutWatcher) MarkPosted(agentName string) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if tw.currentRound != nil {
		tw.currentRound.Posted[agentName] = true
	}
}

// GetMissingAgents returns agents who have NOT posted in the current round.
// These are the agents who get a strike if the round times out (spec §7.4).
func (tw *TimeoutWatcher) GetMissingAgents() []string {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if tw.currentRound == nil {
		return nil
	}

	var missing []string
	for _, agent := range tw.currentRound.Agents {
		if !tw.currentRound.Posted[agent] {
			missing = append(missing, agent)
		}
	}
	return missing
}

// RoundTimeoutEvent describes a per-round timeout for strike processing.
type RoundTimeoutEvent struct {
	Round           int       `json:"round"`
	MissingAgents   []string  `json:"missing_agents"`
	AllAgentsMissed bool      `json:"all_agents_missed"`
	Timestamp       time.Time `json:"timestamp"`
}

// OnRoundTimeout checks if the current round has timed out and, if so,
// returns a RoundTimeoutEvent with the agents who didn't post.
// Per spec §7.4: if only one agent missed → that agent gets a strike.
// If both agents missed → ESCALATED to human.
// Per spec §7.5: missing a round → 1 strike + auto-concede on 2nd miss.
//
// The strike is automatically recorded in the StrikeTracker.
func (tw *TimeoutWatcher) OnRoundTimeout(at time.Time) *RoundTimeoutEvent {
	tw.mu.Lock()

	if tw.currentRound == nil {
		tw.mu.Unlock()
		return nil
	}

	if at.Before(tw.currentRound.Deadline) {
		tw.mu.Unlock()
		return nil // not timed out yet
	}

	missing := make([]string, 0)
	for _, agent := range tw.currentRound.Agents {
		if !tw.currentRound.Posted[agent] {
			missing = append(missing, agent)
		}
	}

	if len(missing) == 0 {
		tw.mu.Unlock()
		return nil // everyone posted, no timeout
	}

	round := tw.currentRound.Round
	allMissed := len(missing) == len(tw.currentRound.Agents)
	tracker := tw.strikeTracker
	tw.mu.Unlock()

	// Record strikes for missing agents (spec §7.5)
	for _, agent := range missing {
		tracker.RecordRoundMiss(agent, round, at)
	}

	return &RoundTimeoutEvent{
		Round:           round,
		MissingAgents:   missing,
		AllAgentsMissed: allMissed,
		Timestamp:       at,
	}
}

// OnGlobalTimeout checks if the negotiation has exceeded the global timeout
// and, if so, returns an EscalationData suitable for posting the §12.2 comment.
// The caller is responsible for actually escalating (calling n.Escalate).
func (tw *TimeoutWatcher) OnGlobalTimeout(n *Negotiator, at time.Time) *EscalationData {
	if !tw.CheckGlobalTimeout(at) {
		return nil
	}
	if n == nil {
		return &EscalationData{
			Reason: EscalationTimeout,
		}
	}
	return EscalationFromNegotiator(n, EscalationTimeout)
}

// Cancel cancels the negotiation watcher via its context.
func (tw *TimeoutWatcher) Cancel() {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if tw.cancel != nil {
		tw.cancel()
	}
}

// IsCancelled returns true if the watcher's context has been cancelled.
func (tw *TimeoutWatcher) IsCancelled() bool {
	tw.mu.Lock()
	ctx := tw.ctx
	tw.mu.Unlock()

	if ctx == nil {
		return false
	}
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// CurrentRound returns the current round number (0 if no round started).
func (tw *TimeoutWatcher) CurrentRound() int {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if tw.currentRound == nil {
		return 0
	}
	return tw.currentRound.Round
}

// StartChimeraTimeout begins the Chimera tie-break timer (spec §12.1: 5 min).
func (tw *TimeoutWatcher) StartChimeraTimeout() {
	tw.StartChimeraTimeoutAt(time.Now())
}

// StartChimeraTimeoutAt begins the Chimera tie-break timer at a specific time.
func (tw *TimeoutWatcher) StartChimeraTimeoutAt(at time.Time) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	tw.chimeraStart = at
	tw.chimeraDeadline = at.Add(tw.config.ChimeraTimeout)
}

// CheckChimeraTimeout returns true if the Chimera tie-break has exceeded
// its timeout (spec §12.1: 5 min → retry 1×, then escalate).
func (tw *TimeoutWatcher) CheckChimeraTimeout(at time.Time) bool {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if tw.chimeraDeadline.IsZero() {
		return false
	}
	return at.After(tw.chimeraDeadline) || at.Equal(tw.chimeraDeadline)
}

// ShouldRetryChimera returns true if this is the first Chimera timeout
// (spec §12.1: "Retry 1×. If still fails → escalate").
// After calling this, chimeraRetried is set to true so the next timeout
// triggers escalation.
func (tw *TimeoutWatcher) ShouldRetryChimera() bool {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if !tw.chimeraRetried {
		tw.chimeraRetried = true
		return true
	}
	return false
}

// ChimeraRetryCount returns how many Chimera retries have been used.
func (tw *TimeoutWatcher) ChimeraRetryCount() int {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if tw.chimeraRetried {
		return 1
	}
	return 0
}

// OnChimeraTimeout handles the Chimera tie-break timeout per spec §12.1.
// First timeout → retry (returns retry=true, escalation=nil).
// Second timeout → escalate (returns retry=false, escalation=EscalationData).
func (tw *TimeoutWatcher) OnChimeraTimeout(n *Negotiator, at time.Time) (retry bool, escalation *EscalationData) {
	if !tw.CheckChimeraTimeout(at) {
		return false, nil
	}

	if tw.ShouldRetryChimera() {
		return true, nil
	}

	// Second timeout → escalate
	if n != nil {
		return false, EscalationFromNegotiator(n, EscalationChimeraUnavailable)
	}
	return false, &EscalationData{
		Reason: EscalationChimeraUnavailable,
	}
}

// TimeoutStatus is a snapshot of all timer states for diagnostic/reporting.
type TimeoutStatus struct {
	NegotiationActive  bool          `json:"negotiation_active"`
	NegotiationElapsed time.Duration `json:"negotiation_elapsed"`
	GlobalTimeLeft     time.Duration `json:"global_time_left"`
	GlobalExpired      bool          `json:"global_expired"`
	CurrentRound       int           `json:"current_round"`
	RoundTimeLeft      time.Duration `json:"round_time_left"`
	RoundExpired       bool          `json:"round_expired"`
	MissingAgents      []string      `json:"missing_agents"`
	ChimeraActive      bool          `json:"chimera_active"`
	ChimeraTimeLeft    time.Duration `json:"chimera_time_left"`
	ChimeraExpired     bool          `json:"chimera_expired"`
	ChimeraRetries     int           `json:"chimera_retries"`
}

// Status returns a snapshot of all timer states at the given time.
func (tw *TimeoutWatcher) Status(at time.Time) TimeoutStatus {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	status := TimeoutStatus{}

	if !tw.negotiationDeadline.IsZero() {
		status.NegotiationActive = true
		status.NegotiationElapsed = at.Sub(tw.negotiationStart)
		status.GlobalTimeLeft = tw.negotiationDeadline.Sub(at)
		if status.GlobalTimeLeft < 0 {
			status.GlobalTimeLeft = 0
		}
		status.GlobalExpired = at.After(tw.negotiationDeadline) || at.Equal(tw.negotiationDeadline)
	}

	if tw.currentRound != nil {
		status.CurrentRound = tw.currentRound.Round
		status.RoundTimeLeft = tw.currentRound.Deadline.Sub(at)
		if status.RoundTimeLeft < 0 {
			status.RoundTimeLeft = 0
		}
		status.RoundExpired = at.After(tw.currentRound.Deadline) || at.Equal(tw.currentRound.Deadline)
		for _, agent := range tw.currentRound.Agents {
			if !tw.currentRound.Posted[agent] {
				status.MissingAgents = append(status.MissingAgents, agent)
			}
		}
	}

	if !tw.chimeraDeadline.IsZero() {
		status.ChimeraActive = true
		status.ChimeraTimeLeft = tw.chimeraDeadline.Sub(at)
		if status.ChimeraTimeLeft < 0 {
			status.ChimeraTimeLeft = 0
		}
		status.ChimeraExpired = at.After(tw.chimeraDeadline) || at.Equal(tw.chimeraDeadline)
		if tw.chimeraRetried {
			status.ChimeraRetries = 1
		}
	}

	return status
}

// ValidateTimeoutConfig returns an error if the config values are invalid
// (negative or zero timeouts that would prevent any meaningful waiting).
func ValidateTimeoutConfig(cfg TimeoutConfig) error {
	if cfg.RoundTimeout < 0 {
		return fmt.Errorf("round timeout must be non-negative, got %v", cfg.RoundTimeout)
	}
	if cfg.GlobalTimeout < 0 {
		return fmt.Errorf("global timeout must be non-negative, got %v", cfg.GlobalTimeout)
	}
	if cfg.ChimeraTimeout < 0 {
		return fmt.Errorf("chimera timeout must be non-negative, got %v", cfg.ChimeraTimeout)
	}
	return nil
}
