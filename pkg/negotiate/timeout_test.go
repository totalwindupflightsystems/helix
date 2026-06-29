package negotiate

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultTimeoutConfig(t *testing.T) {
	cfg := DefaultTimeoutConfig()
	if cfg.RoundTimeout != DefaultRoundTimeout {
		t.Errorf("RoundTimeout = %v, want %v", cfg.RoundTimeout, DefaultRoundTimeout)
	}
	if cfg.GlobalTimeout != DefaultGlobalTimeout {
		t.Errorf("GlobalTimeout = %v, want %v", cfg.GlobalTimeout, DefaultGlobalTimeout)
	}
	if cfg.ChimeraTimeout != DefaultChimeraTimeout {
		t.Errorf("ChimeraTimeout = %v, want %v", cfg.ChimeraTimeout, DefaultChimeraTimeout)
	}
}

func TestNewTimeoutWatcher_AppliesDefaults(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	if tw.config.RoundTimeout != DefaultRoundTimeout {
		t.Errorf("RoundTimeout = %v, want default %v", tw.config.RoundTimeout, DefaultRoundTimeout)
	}
	if tw.config.GlobalTimeout != DefaultGlobalTimeout {
		t.Errorf("GlobalTimeout = %v, want default %v", tw.config.GlobalTimeout, DefaultGlobalTimeout)
	}
	if tw.config.ChimeraTimeout != DefaultChimeraTimeout {
		t.Errorf("ChimeraTimeout = %v, want default %v", tw.config.ChimeraTimeout, DefaultChimeraTimeout)
	}
}

func TestNewTimeoutWatcher_CustomConfig(t *testing.T) {
	cfg := TimeoutConfig{
		RoundTimeout:   3 * time.Minute,
		GlobalTimeout:  20 * time.Minute,
		ChimeraTimeout: 2 * time.Minute,
	}
	tw := NewTimeoutWatcher(cfg, NewStrikeTracker())
	if tw.config.RoundTimeout != 3*time.Minute {
		t.Errorf("RoundTimeout = %v, want 3m", tw.config.RoundTimeout)
	}
	if tw.config.GlobalTimeout != 20*time.Minute {
		t.Errorf("GlobalTimeout = %v, want 20m", tw.config.GlobalTimeout)
	}
}

func TestNewTimeoutWalker_NilTracker(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	if tw.strikeTracker == nil {
		t.Error("strikeTracker should be auto-created when nil is passed")
	}
}

func TestStartNegotiation(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartNegotiationAt(context.Background(), start)

	if !tw.negotiationStart.Equal(start) {
		t.Errorf("negotiationStart = %v, want %v", tw.negotiationStart, start)
	}
	expectedDeadline := start.Add(DefaultGlobalTimeout)
	if !tw.negotiationDeadline.Equal(expectedDeadline) {
		t.Errorf("negotiationDeadline = %v, want %v", tw.negotiationDeadline, expectedDeadline)
	}
}

func TestCheckGlobalTimeout_NotExpired(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartNegotiationAt(context.Background(), start)

	if tw.CheckGlobalTimeout(start.Add(10 * time.Minute)) {
		t.Error("global timeout should not fire after 10 min (limit 30m)")
	}
}

func TestCheckGlobalTimeout_Expired(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartNegotiationAt(context.Background(), start)

	if !tw.CheckGlobalTimeout(start.Add(31 * time.Minute)) {
		t.Error("global timeout should fire after 31 min (limit 30m)")
	}
}

func TestCheckGlobalTimeout_ExactlyAtDeadline(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartNegotiationAt(context.Background(), start)

	if !tw.CheckGlobalTimeout(start.Add(30 * time.Minute)) {
		t.Error("global timeout should fire exactly at deadline")
	}
}

func TestCheckGlobalTimeout_NotStarted(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	if tw.CheckGlobalTimeout(time.Now()) {
		t.Error("global timeout should not fire when negotiation not started")
	}
}

func TestGlobalTimeRemaining(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartNegotiationAt(context.Background(), start)

	remaining := tw.GlobalTimeRemaining(start.Add(10 * time.Minute))
	if remaining != 20*time.Minute {
		t.Errorf("remaining = %v, want 20m", remaining)
	}
}

func TestGlobalTimeRemaining_Expired(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartNegotiationAt(context.Background(), start)

	remaining := tw.GlobalTimeRemaining(start.Add(35 * time.Minute))
	if remaining != 0 {
		t.Errorf("remaining = %v, want 0", remaining)
	}
}

func TestStartRound(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartRoundAt(1, start, "agentA", "agentB")

	if tw.currentRound == nil {
		t.Fatal("currentRound should not be nil after StartRound")
	}
	if tw.currentRound.Round != 1 {
		t.Errorf("Round = %d, want 1", tw.currentRound.Round)
	}
	expectedDeadline := start.Add(DefaultRoundTimeout)
	if !tw.currentRound.Deadline.Equal(expectedDeadline) {
		t.Errorf("Deadline = %v, want %v", tw.currentRound.Deadline, expectedDeadline)
	}
}

func TestCheckRoundTimeout_NotExpired(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartRoundAt(1, start, "agentA", "agentB")

	if tw.CheckRoundTimeout(start.Add(3 * time.Minute)) {
		t.Error("round timeout should not fire after 3 min (limit 5m)")
	}
}

func TestCheckRoundTimeout_Expired(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartRoundAt(1, start, "agentA", "agentB")

	if !tw.CheckRoundTimeout(start.Add(6 * time.Minute)) {
		t.Error("round timeout should fire after 6 min (limit 5m)")
	}
}

func TestCheckRoundTimeout_NoRound(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	if tw.CheckRoundTimeout(time.Now()) {
		t.Error("round timeout should not fire when no round started")
	}
}

func TestRoundTimeRemaining(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartRoundAt(1, start, "agentA", "agentB")

	remaining := tw.RoundTimeRemaining(start.Add(2 * time.Minute))
	if remaining != 3*time.Minute {
		t.Errorf("remaining = %v, want 3m", remaining)
	}
}

func TestMarkPosted(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartRoundAt(1, start, "agentA", "agentB")

	tw.MarkPosted("agentA")

	if !tw.currentRound.Posted["agentA"] {
		t.Error("agentA should be marked as posted")
	}
	if tw.currentRound.Posted["agentB"] {
		t.Error("agentB should NOT be marked as posted")
	}
}

func TestGetMissingAgents_None(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartRoundAt(1, start, "agentA", "agentB")

	tw.MarkPosted("agentA")
	tw.MarkPosted("agentB")

	missing := tw.GetMissingAgents()
	if len(missing) != 0 {
		t.Errorf("missing agents = %v, want empty", missing)
	}
}

func TestGetMissingAgents_OneMissing(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartRoundAt(1, start, "agentA", "agentB")

	tw.MarkPosted("agentA")

	missing := tw.GetMissingAgents()
	if len(missing) != 1 || missing[0] != "agentB" {
		t.Errorf("missing = %v, want [agentB]", missing)
	}
}

func TestGetMissingAgents_NoRound(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	missing := tw.GetMissingAgents()
	if missing != nil {
		t.Errorf("missing = %v, want nil when no round started", missing)
	}
}

func TestOnRoundTimeout_OneAgentMissed(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartRoundAt(1, start, "agentA", "agentB")
	tw.MarkPosted("agentA")

	// Time = start + 6 min → expired
	event := tw.OnRoundTimeout(start.Add(6 * time.Minute))

	if event == nil {
		t.Fatal("event should not be nil for expired round")
	}
	if event.Round != 1 {
		t.Errorf("Round = %d, want 1", event.Round)
	}
	if len(event.MissingAgents) != 1 || event.MissingAgents[0] != "agentB" {
		t.Errorf("MissingAgents = %v, want [agentB]", event.MissingAgents)
	}
	if event.AllAgentsMissed {
		t.Error("AllAgentsMissed should be false when one agent posted")
	}

	// Verify strike was recorded
	count := tw.strikeTracker.StrikeCount("agentB")
	if count != 1 {
		t.Errorf("agentB strike count = %d, want 1", count)
	}
}

func TestOnRoundTimeout_BothAgentsMissed(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartRoundAt(1, start, "agentA", "agentB")

	event := tw.OnRoundTimeout(start.Add(6 * time.Minute))

	if event == nil {
		t.Fatal("event should not be nil")
	}
	if !event.AllAgentsMissed {
		t.Error("AllAgentsMissed should be true when both agents missed")
	}
	if len(event.MissingAgents) != 2 {
		t.Errorf("MissingAgents count = %d, want 2", len(event.MissingAgents))
	}

	// Both agents should have strikes
	if tw.strikeTracker.StrikeCount("agentA") != 1 {
		t.Error("agentA should have 1 strike")
	}
	if tw.strikeTracker.StrikeCount("agentB") != 1 {
		t.Error("agentB should have 1 strike")
	}
}

func TestOnRoundTimeout_NotExpired(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartRoundAt(1, start, "agentA", "agentB")

	event := tw.OnRoundTimeout(start.Add(3 * time.Minute))
	if event != nil {
		t.Error("event should be nil when round not expired")
	}
}

func TestOnRoundTimeout_AllPosted(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartRoundAt(1, start, "agentA", "agentB")
	tw.MarkPosted("agentA")
	tw.MarkPosted("agentB")

	event := tw.OnRoundTimeout(start.Add(6 * time.Minute))
	if event != nil {
		t.Error("event should be nil when all agents posted (no one timed out)")
	}
}

func TestOnRoundTimeout_NoRound(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	event := tw.OnRoundTimeout(time.Now())
	if event != nil {
		t.Error("event should be nil when no round started")
	}
}

func TestOnRoundTimeout_AutoConcedeOnSecondMiss(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)

	// Round 1: agentB misses
	tw.StartRoundAt(1, start, "agentA", "agentB")
	tw.MarkPosted("agentA")
	tw.OnRoundTimeout(start.Add(6 * time.Minute))

	if tw.strikeTracker.ShouldAutoConcede("agentB") {
		t.Error("agentB should not auto-concede after 1 miss")
	}

	// Round 2: agentB misses again
	tw.StartRoundAt(2, start.Add(10*time.Minute), "agentA", "agentB")
	tw.MarkPosted("agentA")
	tw.OnRoundTimeout(start.Add(16 * time.Minute))

	// Per spec §7.5: auto-concede on 2nd miss
	if !tw.strikeTracker.ShouldAutoConcede("agentB") {
		t.Error("agentB should auto-concede after 2 misses")
	}
}

func TestOnGlobalTimeout_NotExpired(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartNegotiationAt(context.Background(), start)

	esc := tw.OnGlobalTimeout(nil, start.Add(10*time.Minute))
	if esc != nil {
		t.Error("should return nil when global timeout not expired")
	}
}

func TestOnGlobalTimeout_Expired_WithNegotiator(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartNegotiationAt(context.Background(), start)

	agentA := Agent{Name: "alpha", TrustLevel: 60, ForgejoUser: "alpha"}
	agentB := Agent{Name: "beta", TrustLevel: 70, ForgejoUser: "beta"}
	dir := t.TempDir()
	neg, err := NewNegotiator(42, agentA, agentB, VerdictApproved, VerdictRequestChanges,
		"http://localhost:8765", filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatalf("NewNegotiator error: %v", err)
	}

	esc := tw.OnGlobalTimeout(neg, start.Add(31*time.Minute))
	if esc == nil {
		t.Fatal("escalation should not be nil")
	}
	if esc.Reason != EscalationTimeout {
		t.Errorf("Reason = %s, want %s", esc.Reason, EscalationTimeout)
	}
}

func TestOnGlobalTimeout_NotStarted(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	esc := tw.OnGlobalTimeout(nil, time.Now())
	if esc != nil {
		t.Error("should return nil when negotiation not started")
	}
}

func TestCancel(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	tw.StartNegotiation(context.Background())

	tw.Cancel()

	if !tw.IsCancelled() {
		t.Error("watcher should be cancelled after Cancel()")
	}
}

func TestIsCancelled_NotStarted(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	if tw.IsCancelled() {
		t.Error("watcher should not be cancelled before StartNegotiation")
	}
}

func TestIsCancelled_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	tw.StartNegotiation(ctx)

	cancel() // cancel the parent context

	if !tw.IsCancelled() {
		t.Error("watcher should be cancelled when parent context is cancelled")
	}
}

func TestCurrentRound_NoRound(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	if tw.CurrentRound() != 0 {
		t.Errorf("CurrentRound = %d, want 0 when no round started", tw.CurrentRound())
	}
}

func TestCurrentRound_Active(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	tw.StartRound(3, "agentA", "agentB")
	if tw.CurrentRound() != 3 {
		t.Errorf("CurrentRound = %d, want 3", tw.CurrentRound())
	}
}

func TestStartChimeraTimeout(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartChimeraTimeoutAt(start)

	if tw.chimeraStart != start {
		t.Errorf("chimeraStart = %v, want %v", tw.chimeraStart, start)
	}
	expectedDeadline := start.Add(DefaultChimeraTimeout)
	if tw.chimeraDeadline != expectedDeadline {
		t.Errorf("chimeraDeadline = %v, want %v", tw.chimeraDeadline, expectedDeadline)
	}
}

func TestCheckChimeraTimeout_NotExpired(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartChimeraTimeoutAt(start)

	if tw.CheckChimeraTimeout(start.Add(3 * time.Minute)) {
		t.Error("chimera timeout should not fire after 3 min (limit 5m)")
	}
}

func TestCheckChimeraTimeout_Expired(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartChimeraTimeoutAt(start)

	if !tw.CheckChimeraTimeout(start.Add(6 * time.Minute)) {
		t.Error("chimera timeout should fire after 6 min (limit 5m)")
	}
}

func TestCheckChimeraTimeout_NotStarted(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	if tw.CheckChimeraTimeout(time.Now()) {
		t.Error("chimera timeout should not fire when not started")
	}
}

func TestShouldRetryChimera_FirstTimeout(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	if !tw.ShouldRetryChimera() {
		t.Error("first Chimera timeout should return retry=true")
	}
}

func TestShouldRetryChimera_SecondTimeout(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	tw.ShouldRetryChimera() // first → retry
	if tw.ShouldRetryChimera() {
		t.Error("second Chimera timeout should return retry=false (escalate)")
	}
}

func TestChimeraRetryCount(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	if tw.ChimeraRetryCount() != 0 {
		t.Errorf("retry count = %d, want 0 initially", tw.ChimeraRetryCount())
	}
	tw.ShouldRetryChimera()
	if tw.ChimeraRetryCount() != 1 {
		t.Errorf("retry count = %d, want 1 after one retry", tw.ChimeraRetryCount())
	}
}

func TestOnChimeraTimeout_FirstTimeout_Retry(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartChimeraTimeoutAt(start)

	retry, esc := tw.OnChimeraTimeout(nil, start.Add(6*time.Minute))
	if !retry {
		t.Error("first Chimera timeout should return retry=true")
	}
	if esc != nil {
		t.Error("first Chimera timeout should return escalation=nil")
	}
}

func TestOnChimeraTimeout_SecondTimeout_Escalate(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartChimeraTimeoutAt(start)

	// First timeout → retry
	tw.OnChimeraTimeout(nil, start.Add(6*time.Minute))

	// Restart timer for second attempt
	tw.StartChimeraTimeoutAt(start.Add(7 * time.Minute))

	// Second timeout → escalate
	retry, esc := tw.OnChimeraTimeout(nil, start.Add(13*time.Minute))
	if retry {
		t.Error("second Chimera timeout should return retry=false")
	}
	if esc == nil {
		t.Fatal("second Chimera timeout should return escalation != nil")
	}
	if esc.Reason != EscalationChimeraUnavailable {
		t.Errorf("Reason = %s, want %s", esc.Reason, EscalationChimeraUnavailable)
	}
}

func TestOnChimeraTimeout_NotExpired(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartChimeraTimeoutAt(start)

	retry, esc := tw.OnChimeraTimeout(nil, start.Add(3*time.Minute))
	if retry {
		t.Error("should not retry when not expired")
	}
	if esc != nil {
		t.Error("should not escalate when not expired")
	}
}

func TestOnChimeraTimeout_WithNegotiator(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartChimeraTimeoutAt(start)

	// First timeout → retry
	tw.OnChimeraTimeout(nil, start.Add(6*time.Minute))

	// Restart for second attempt
	tw.StartChimeraTimeoutAt(start.Add(7 * time.Minute))

	agentA := Agent{Name: "alpha", TrustLevel: 60}
	agentB := Agent{Name: "beta", TrustLevel: 70}
	dir := t.TempDir()
	neg, err := NewNegotiator(42, agentA, agentB, VerdictApproved, VerdictRequestChanges,
		"http://localhost:8765", filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatalf("NewNegotiator error: %v", err)
	}

	_, esc := tw.OnChimeraTimeout(neg, start.Add(13*time.Minute))
	if esc == nil {
		t.Fatal("escalation should not be nil")
	}
	if esc.AgentA.Name != "alpha" {
		t.Errorf("AgentA.Name = %s, want alpha", esc.AgentA.Name)
	}
}

func TestStatus_NoActiveTimers(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	status := tw.Status(time.Now())

	if status.NegotiationActive {
		t.Error("should not be active before StartNegotiation")
	}
	if status.CurrentRound != 0 {
		t.Errorf("CurrentRound = %d, want 0", status.CurrentRound)
	}
	if status.ChimeraActive {
		t.Error("chimera should not be active")
	}
}

func TestStatus_NegotiationActive(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartNegotiationAt(context.Background(), start)

	status := tw.Status(start.Add(10 * time.Minute))
	if !status.NegotiationActive {
		t.Error("negotiation should be active")
	}
	if !status.GlobalExpired {
		// 10 min into 30 min → not expired
		if status.GlobalExpired {
			t.Error("global should not be expired at 10 min")
		}
	}
}

func TestStatus_GlobalExpired(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartNegotiationAt(context.Background(), start)

	status := tw.Status(start.Add(35 * time.Minute))
	if !status.GlobalExpired {
		t.Error("global should be expired at 35 min")
	}
	if status.GlobalTimeLeft != 0 {
		t.Errorf("GlobalTimeLeft = %v, want 0", status.GlobalTimeLeft)
	}
}

func TestStatus_RoundActive(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartNegotiationAt(context.Background(), start)
	tw.StartRoundAt(2, start, "agentA", "agentB")
	tw.MarkPosted("agentA")

	status := tw.Status(start.Add(2 * time.Minute))
	if status.CurrentRound != 2 {
		t.Errorf("CurrentRound = %d, want 2", status.CurrentRound)
	}
	if status.RoundExpired {
		t.Error("round should not be expired at 2 min")
	}
	if len(status.MissingAgents) != 1 {
		t.Errorf("MissingAgents count = %d, want 1 (agentB)", len(status.MissingAgents))
	}
}

func TestStatus_RoundExpired(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartRoundAt(1, start, "agentA", "agentB")

	status := tw.Status(start.Add(6 * time.Minute))
	if !status.RoundExpired {
		t.Error("round should be expired at 6 min")
	}
}

func TestStatus_ChimeraActive(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartChimeraTimeoutAt(start)

	status := tw.Status(start.Add(3 * time.Minute))
	if !status.ChimeraActive {
		t.Error("chimera should be active")
	}
	if status.ChimeraExpired {
		t.Error("chimera should not be expired at 3 min")
	}
}

func TestStatus_ChimeraExpired(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartChimeraTimeoutAt(start)

	status := tw.Status(start.Add(6 * time.Minute))
	if !status.ChimeraExpired {
		t.Error("chimera should be expired at 6 min")
	}
	if status.ChimeraTimeLeft != 0 {
		t.Errorf("ChimeraTimeLeft = %v, want 0", status.ChimeraTimeLeft)
	}
}

func TestStatus_ChimeraRetries(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	tw.StartChimeraTimeout()
	tw.ShouldRetryChimera()

	status := tw.Status(time.Now())
	if status.ChimeraRetries != 1 {
		t.Errorf("ChimeraRetries = %d, want 1", status.ChimeraRetries)
	}
}

func TestValidateTimeoutConfig_Valid(t *testing.T) {
	cfg := DefaultTimeoutConfig()
	if err := ValidateTimeoutConfig(cfg); err != nil {
		t.Errorf("valid config should not return error: %v", err)
	}
}

func TestValidateTimeoutConfig_NegativeRound(t *testing.T) {
	cfg := TimeoutConfig{RoundTimeout: -1 * time.Minute, GlobalTimeout: 30 * time.Minute, ChimeraTimeout: 5 * time.Minute}
	if err := ValidateTimeoutConfig(cfg); err == nil {
		t.Error("negative round timeout should return error")
	}
}

func TestValidateTimeoutConfig_NegativeGlobal(t *testing.T) {
	cfg := TimeoutConfig{RoundTimeout: 5 * time.Minute, GlobalTimeout: -1 * time.Minute, ChimeraTimeout: 5 * time.Minute}
	if err := ValidateTimeoutConfig(cfg); err == nil {
		t.Error("negative global timeout should return error")
	}
}

func TestValidateTimeoutConfig_NegativeChimera(t *testing.T) {
	cfg := TimeoutConfig{RoundTimeout: 5 * time.Minute, GlobalTimeout: 30 * time.Minute, ChimeraTimeout: -1 * time.Minute}
	if err := ValidateTimeoutConfig(cfg); err == nil {
		t.Error("negative chimera timeout should return error")
	}
}

func TestRoundTimer_AgentsTracked(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	tw.StartRound(1, "alice", "bob")

	tw.MarkPosted("alice")
	tw.MarkPosted("bob")

	missing := tw.GetMissingAgents()
	if len(missing) != 0 {
		t.Errorf("both agents posted, missing = %v, want empty", missing)
	}
}

// Integration test: full negotiation lifecycle with timeouts
func TestTimeoutWatcher_FullLifecycle(t *testing.T) {
	tracker := NewStrikeTracker()
	tw := NewTimeoutWatcher(TimeoutConfig{}, tracker)

	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartNegotiationAt(context.Background(), start)

	// Round 1: both agents post on time
	tw.StartRoundAt(1, start, "agentA", "agentB")
	tw.MarkPosted("agentA")
	tw.MarkPosted("agentB")
	// At start+3min, round 1 has not timed out
	if tw.CheckRoundTimeout(start.Add(3 * time.Minute)) {
		t.Error("round 1 should not time out at 3 min")
	}

	// Round 2: agentB misses
	round2Start := start.Add(5 * time.Minute)
	tw.StartRoundAt(2, round2Start, "agentA", "agentB")
	tw.MarkPosted("agentA")
	event := tw.OnRoundTimeout(round2Start.Add(6 * time.Minute))
	if event == nil {
		t.Fatal("round 2 should time out for agentB")
	}
	if len(event.MissingAgents) != 1 || event.MissingAgents[0] != "agentB" {
		t.Errorf("MissingAgents = %v, want [agentB]", event.MissingAgents)
	}

	// Round 3: agentB misses again → auto-concede
	round3Start := start.Add(11 * time.Minute)
	tw.StartRoundAt(3, round3Start, "agentA", "agentB")
	tw.MarkPosted("agentA")
	tw.OnRoundTimeout(round3Start.Add(6 * time.Minute))

	if !tracker.ShouldAutoConcede("agentB") {
		t.Error("agentB should auto-concede after 2 misses")
	}

	// Global timeout not yet expired
	if tw.CheckGlobalTimeout(start.Add(20 * time.Minute)) {
		t.Error("global timeout should not fire at 20 min")
	}

	// Global timeout expired at 31 min
	if !tw.CheckGlobalTimeout(start.Add(31 * time.Minute)) {
		t.Error("global timeout should fire at 31 min")
	}
}

// Test custom timeouts
func TestTimeoutWatcher_CustomTimeouts(t *testing.T) {
	cfg := TimeoutConfig{
		RoundTimeout:   1 * time.Minute,
		GlobalTimeout:  10 * time.Minute,
		ChimeraTimeout: 30 * time.Second,
	}
	tw := NewTimeoutWatcher(cfg, nil)

	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartNegotiationAt(context.Background(), start)
	tw.StartRoundAt(1, start, "a", "b")

	// Round expires at 1 min
	if !tw.CheckRoundTimeout(start.Add(2 * time.Minute)) {
		t.Error("custom round timeout (1m) should fire at 2 min")
	}
	// Global expires at 10 min
	if !tw.CheckGlobalTimeout(start.Add(11 * time.Minute)) {
		t.Error("custom global timeout (10m) should fire at 11 min")
	}
}

func TestOnRoundTimeout_RoundTimeoutEvent_Fields(t *testing.T) {
	tw := NewTimeoutWatcher(TimeoutConfig{}, nil)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tw.StartRoundAt(2, start, "agentA", "agentB")
	// Neither agent posted

	event := tw.OnRoundTimeout(start.Add(6 * time.Minute))

	if event == nil {
		t.Fatal("event should not be nil")
	}
	if event.Round != 2 {
		t.Errorf("Round = %d, want 2", event.Round)
	}
	if !event.AllAgentsMissed {
		t.Error("AllAgentsMissed should be true when both missed")
	}
	if !event.Timestamp.Equal(start.Add(6 * time.Minute)) {
		t.Errorf("Timestamp = %v, want %v", event.Timestamp, start.Add(6*time.Minute))
	}
}
