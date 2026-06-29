package marketplace

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

// writeTrustLedger creates a temp ledger file with the given events.
func writeTrustLedger(t *testing.T, events []trust.TrustEvent) string {
	t.Helper()
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "trust.jsonl")

	ledger, err := trust.NewLedger(ledgerPath)
	if err != nil {
		t.Fatalf("create ledger: %v", err)
	}
	defer ledger.Close()

	for _, evt := range events {
		if err := ledger.Append(evt); err != nil {
			t.Fatalf("append event: %v", err)
		}
	}
	return ledgerPath
}

func TestScoreToMarketplace_Boundaries(t *testing.T) {
	tests := []struct {
		name  string
		score float64
		want  int
	}{
		{"zero", 0.0, 0},
		{"one", 1.0, 100},
		{"half", 0.5, 50},
		{"quarter", 0.25, 25},
		{"three_quarters", 0.75, 75},
		{"above_max", 1.5, 100},
		{"below_min", -0.5, 0},
		{"rounds_up", 0.555, 56},
		{"rounds_down", 0.554, 55},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ScoreToMarketplace(trust.TrustScore(tc.score))
			if got != tc.want {
				t.Errorf("ScoreToMarketplace(%.3f) = %d, want %d", tc.score, got, tc.want)
			}
		})
	}
}

func TestMarketplaceToScore(t *testing.T) {
	tests := []struct {
		mp   int
		want float64
	}{
		{0, 0.0},
		{100, 1.0},
		{50, 0.5},
		{25, 0.25},
		{-10, 0.0}, // clamped
		{200, 1.0}, // clamped
	}
	for _, tc := range tests {
		got := MarketplaceToScore(tc.mp)
		if got != tc.want {
			t.Errorf("MarketplaceToScore(%d) = %.3f, want %.3f", tc.mp, got, tc.want)
		}
	}
}

func TestNewTrustSync(t *testing.T) {
	reg, _ := NewRegistry(t.TempDir())
	ledgerPath := filepath.Join(t.TempDir(), "trust.jsonl")

	ts := NewTrustSync(reg, ledgerPath)
	if ts == nil {
		t.Fatal("TrustSync is nil")
	}
	if ts.ledgerPath != ledgerPath {
		t.Errorf("ledgerPath = %q, want %q", ts.ledgerPath, ledgerPath)
	}
	if ts.syncInterval != DefaultSyncInterval {
		t.Errorf("default syncInterval = %v, want %v", ts.syncInterval, DefaultSyncInterval)
	}
	if ts.lastSync == nil {
		t.Error("lastSync map not initialized")
	}
}

func TestSyncAgent_UpdatesScore(t *testing.T) {
	ledgerPath := writeTrustLedger(t, []trust.TrustEvent{
		{
			AgentID:   "agent-a",
			EventType: trust.EventMergeSuccess,
			Timestamp: time.Now(),
			Data:      trust.EventData{ScoreBefore: 0.5, ScoreAfter: 0.8},
		},
	})

	reg, _ := NewRegistry(t.TempDir())
	_ = reg.Register(&Agent{
		Name:         "agent-a",
		Status:       StatusActive,
		TrustScore:   50, // stale score
		Capabilities: []Capability{CapGo},
	})

	ts := NewTrustSync(reg, ledgerPath)
	ms, rs, err := ts.SyncAgent("agent-a")
	if err != nil {
		t.Fatalf("SyncAgent: %v", err)
	}

	if ms != 80 {
		t.Errorf("marketplace score = %d, want 80", ms)
	}
	if rs < 0.79 || rs > 0.81 {
		t.Errorf("raw score = %.3f, want ~0.80", rs)
	}

	// Verify registry was updated.
	agent, _ := reg.Get("agent-a")
	if agent.TrustScore != 80 {
		t.Errorf("registry trust score = %d, want 80", agent.TrustScore)
	}
}

func TestSyncAgent_AgentNotFound(t *testing.T) {
	ledgerPath := writeTrustLedger(t, nil)
	reg, _ := NewRegistry(t.TempDir())
	ts := NewTrustSync(reg, ledgerPath)

	ms, rs, err := ts.SyncAgent("nonexistent")
	if err != nil {
		t.Fatalf("SyncAgent should not error for unknown agent (ledger still readable): %v", err)
	}
	// Ledger exists but empty → default score 0.5 → marketplace 50.
	if ms != 50 {
		t.Errorf("marketplace score = %d, want 50", ms)
	}
	if rs != 0.5 {
		t.Errorf("raw score = %.3f, want 0.50", rs)
	}
}

func TestSyncAgent_LedgerMissing(t *testing.T) {
	reg, _ := NewRegistry(t.TempDir())
	// Missing ledger → ReplayToScore treats nonexistent file as empty (returns 0.5 default).
	// This is by design — an agent with no history starts at neutral trust.
	ts := NewTrustSync(reg, "/nonexistent/path/trust.jsonl")

	ms, _, err := ts.SyncAgent("agent-a")
	if err != nil {
		t.Fatalf("SyncAgent should not error for missing ledger (treated as empty): %v", err)
	}
	if ms != 50 {
		t.Errorf("marketplace score = %d, want 50 (default for empty ledger)", ms)
	}
}

func TestSyncAgent_IntervalSkip(t *testing.T) {
	ledgerPath := writeTrustLedger(t, []trust.TrustEvent{
		{
			AgentID:   "agent-a",
			EventType: trust.EventMergeSuccess,
			Timestamp: time.Now(),
			Data:      trust.EventData{ScoreBefore: 0.5, ScoreAfter: 0.9},
		},
	})

	reg, _ := NewRegistry(t.TempDir())
	_ = reg.Register(&Agent{
		Name:         "agent-a",
		Status:       StatusActive,
		TrustScore:   50,
		Capabilities: []Capability{CapGo},
	})

	ts := NewTrustSync(reg, ledgerPath)

	// First sync reads ledger.
	ms1, _, err := ts.SyncAgent("agent-a")
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if ms1 != 90 {
		t.Fatalf("first sync score = %d, want 90", ms1)
	}

	// Second sync within interval should return cached score.
	// Change the registry score to verify it's not re-read from ledger.
	agent, _ := reg.Get("agent-a")
	agent.TrustScore = 42 // simulate external modification

	ms2, _, err := ts.SyncAgent("agent-a")
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if ms2 != 42 {
		t.Errorf("cached return score = %d, want 42 (cached, not re-read from ledger)", ms2)
	}
}

func TestSyncAgent_ForceSyncWithZeroInterval(t *testing.T) {
	ledgerPath := writeTrustLedger(t, []trust.TrustEvent{
		{
			AgentID:   "agent-a",
			EventType: trust.EventMergeSuccess,
			Timestamp: time.Now(),
			Data:      trust.EventData{ScoreBefore: 0.5, ScoreAfter: 0.9},
		},
	})

	reg, _ := NewRegistry(t.TempDir())
	_ = reg.Register(&Agent{
		Name:         "agent-a",
		Status:       StatusActive,
		TrustScore:   50,
		Capabilities: []Capability{CapGo},
	})

	ts := NewTrustSync(reg, ledgerPath)
	ts.SetSyncInterval(0) // force every call to re-read

	// First sync.
	_, _, err := ts.SyncAgent("agent-a")
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// Second sync should re-read ledger (not cached).
	ms, _, err := ts.SyncAgent("agent-a")
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if ms != 90 {
		t.Errorf("forced sync score = %d, want 90", ms)
	}
}

func TestSyncAll(t *testing.T) {
	ledgerPath := writeTrustLedger(t, []trust.TrustEvent{
		{
			AgentID:   "agent-a",
			EventType: trust.EventMergeSuccess,
			Timestamp: time.Now(),
			Data:      trust.EventData{ScoreBefore: 0.5, ScoreAfter: 0.8},
		},
		{
			AgentID:   "agent-b",
			EventType: trust.EventMergeSuccess,
			Timestamp: time.Now(),
			Data:      trust.EventData{ScoreBefore: 0.5, ScoreAfter: 0.6},
		},
		{
			AgentID:   "agent-c",
			EventType: trust.EventIncidentPenalty,
			Timestamp: time.Now(),
			Data:      trust.EventData{ScoreBefore: 0.7, AttributionWt: 0.5},
		},
	})

	reg, _ := NewRegistry(t.TempDir())
	_ = reg.Register(&Agent{Name: "agent-a", Status: StatusActive, TrustScore: 50, Capabilities: []Capability{CapGo}})
	_ = reg.Register(&Agent{Name: "agent-b", Status: StatusActive, TrustScore: 50, Capabilities: []Capability{CapGo}})
	_ = reg.Register(&Agent{Name: "agent-c", Status: StatusActive, TrustScore: 70, Capabilities: []Capability{CapGo}})

	ts := NewTrustSync(reg, ledgerPath)
	ts.SetSyncInterval(0) // force sync

	result, err := ts.SyncAll()
	if err != nil {
		t.Fatalf("SyncAll: %v", err)
	}

	if result.Synced != 3 {
		t.Errorf("synced = %d, want 3", result.Synced)
	}
	if result.Updated != 3 {
		t.Errorf("updated = %d, want 3", result.Updated)
	}

	// Verify each agent got the right score.
	agentA, _ := reg.Get("agent-a")
	if agentA.TrustScore != 80 {
		t.Errorf("agent-a score = %d, want 80", agentA.TrustScore)
	}

	agentB, _ := reg.Get("agent-b")
	if agentB.TrustScore != 60 {
		t.Errorf("agent-b score = %d, want 60", agentB.TrustScore)
	}

	// agent-c: 0.7 - (0.3 * 0.5) = 0.55 → 55
	agentC, _ := reg.Get("agent-c")
	if agentC.TrustScore != 55 {
		t.Errorf("agent-c score = %d, want 55", agentC.TrustScore)
	}
}

func TestSyncAll_NoChanges(t *testing.T) {
	ledgerPath := writeTrustLedger(t, nil) // empty ledger → default 0.5 → 50

	reg, _ := NewRegistry(t.TempDir())
	_ = reg.Register(&Agent{Name: "agent-a", Status: StatusActive, TrustScore: 50, Capabilities: []Capability{CapGo}})

	ts := NewTrustSync(reg, ledgerPath)
	ts.SetSyncInterval(0)

	result, err := ts.SyncAll()
	if err != nil {
		t.Fatalf("SyncAll: %v", err)
	}

	if result.Synced != 1 {
		t.Errorf("synced = %d, want 1", result.Synced)
	}
	if result.Updated != 0 {
		t.Errorf("updated = %d, want 0 (no changes)", result.Updated)
	}
}

func TestSyncAll_EmptyRegistry(t *testing.T) {
	ledgerPath := writeTrustLedger(t, nil)
	reg, _ := NewRegistry(t.TempDir())

	ts := NewTrustSync(reg, ledgerPath)
	result, err := ts.SyncAll()
	if err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	if result.Synced != 0 {
		t.Errorf("synced = %d, want 0", result.Synced)
	}
	if result.Updated != 0 {
		t.Errorf("updated = %d, want 0", result.Updated)
	}
}

func TestGetLiveScore(t *testing.T) {
	ledgerPath := writeTrustLedger(t, []trust.TrustEvent{
		{
			AgentID:   "agent-a",
			EventType: trust.EventMergeSuccess,
			Timestamp: time.Now(),
			Data:      trust.EventData{ScoreBefore: 0.5, ScoreAfter: 0.85},
		},
	})

	reg, _ := NewRegistry(t.TempDir())
	ts := NewTrustSync(reg, ledgerPath)

	score, err := ts.GetLiveScore("agent-a")
	if err != nil {
		t.Fatalf("GetLiveScore: %v", err)
	}
	if score < 0.84 || score > 0.86 {
		t.Errorf("live score = %.3f, want ~0.85", score)
	}

	// Verify registry was NOT updated (GetLiveScore is read-only).
	agents, _ := reg.List(nil)
	if len(agents) != 0 {
		t.Errorf("registry should be empty, got %d agents", len(agents))
	}
}

func TestGetLiveScore_UnknownAgent(t *testing.T) {
	ledgerPath := writeTrustLedger(t, nil)
	reg, _ := NewRegistry(t.TempDir())
	ts := NewTrustSync(reg, ledgerPath)

	// Unknown agent in empty ledger → default 0.5 score (no error).
	score, err := ts.GetLiveScore("unknown")
	if err != nil {
		t.Fatalf("GetLiveScore should not error: %v", err)
	}
	if score != 0.5 {
		t.Errorf("live score = %.3f, want 0.50", score)
	}
}

func TestSetSyncInterval(t *testing.T) {
	reg, _ := NewRegistry(t.TempDir())
	ts := NewTrustSync(reg, "")

	ts.SetSyncInterval(10 * time.Second)
	if ts.syncInterval != 10*time.Second {
		t.Errorf("syncInterval = %v, want 10s", ts.syncInterval)
	}
}

func TestScoreToMarketplace_Rounding(t *testing.T) {
	// Verify rounding to nearest int.
	tests := []struct {
		score float64
		want  int
	}{
		{0.495, 50},
		{0.504, 50},
		{0.505, 51}, // rounds up at .5
		{0.334, 33},
		{0.335, 34}, // rounds up
		{0.666, 67},
	}
	for _, tc := range tests {
		got := ScoreToMarketplace(trust.TrustScore(tc.score))
		if got != tc.want {
			t.Errorf("ScoreToMarketplace(%.3f) = %d, want %d", tc.score, got, tc.want)
		}
	}
}

func TestSyncAgent_IncidentPenaltyUpdatesScore(t *testing.T) {
	ledgerPath := writeTrustLedger(t, []trust.TrustEvent{
		{
			AgentID:   "agent-a",
			EventType: trust.EventMergeSuccess,
			Timestamp: time.Now().Add(-48 * time.Hour),
			Data:      trust.EventData{ScoreBefore: 0.5, ScoreAfter: 0.9},
		},
		{
			AgentID:   "agent-a",
			EventType: trust.EventIncidentPenalty,
			Timestamp: time.Now(),
			Data:      trust.EventData{ScoreBefore: 0.9, AttributionWt: 0.5},
		},
	})

	reg, _ := NewRegistry(t.TempDir())
	_ = reg.Register(&Agent{
		Name:         "agent-a",
		Status:       StatusActive,
		TrustScore:   90,
		Capabilities: []Capability{CapGo},
	})

	ts := NewTrustSync(reg, ledgerPath)
	ts.SetSyncInterval(0)

	ms, _, err := ts.SyncAgent("agent-a")
	if err != nil {
		t.Fatalf("SyncAgent: %v", err)
	}

	// 0.9 - (0.3 * 0.5) = 0.75 → 75
	if ms != 75 {
		t.Errorf("marketplace score after incident = %d, want 75", ms)
	}
}

func TestSyncResult_JSON(t *testing.T) {
	// Verify SyncResult has proper JSON serialization.
	result := &SyncResult{
		SyncedAt: time.Now(),
		Synced:   3,
		Updated:  2,
	}
	if result.Synced != 3 {
		t.Errorf("Synced = %d", result.Synced)
	}
}

func TestTrustSync_ConcurrentSafe(t *testing.T) {
	ledgerPath := writeTrustLedger(t, nil)
	reg, _ := NewRegistry(t.TempDir())
	_ = reg.Register(&Agent{Name: "agent-a", Status: StatusActive, TrustScore: 50, Capabilities: []Capability{CapGo}})

	ts := NewTrustSync(reg, ledgerPath)
	ts.SetSyncInterval(0)

	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_, _, err := ts.SyncAgent("agent-a")
			done <- err
		}()
	}
	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent SyncAgent error: %v", err)
		}
	}
}

func TestSyncAgent_LedgerReadPermission(t *testing.T) {
	// Create a ledger file then make it unreadable.
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "trust.jsonl")

	ledger, err := trust.NewLedger(ledgerPath)
	if err != nil {
		t.Fatalf("create ledger: %v", err)
	}
	ledger.Close()

	_ = os.Chmod(ledgerPath, 0o000)
	defer func() { _ = os.Chmod(ledgerPath, 0o644) }()

	reg, _ := NewRegistry(t.TempDir())
	ts := NewTrustSync(reg, ledgerPath)
	ts.SetSyncInterval(0)

	_, _, err = ts.SyncAgent("agent-a")
	if err == nil {
		t.Error("expected error for unreadable ledger")
	}
}
