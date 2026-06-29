package marketplace

import (
	"fmt"
	"sync"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

// TrustSync bridges the trust engine (pkg/trust) to the marketplace.
// The trust engine maintains a JSONL ledger as the single source of truth for
// agent trust scores. The marketplace stores a snapshot (TrustScore int 0-100)
// for discovery and ranking. TrustSync periodically or on-demand replays the
// ledger, computes the live score, converts it to the 0-100 marketplace scale,
// and updates the agent profile.
type TrustSync struct {
	mu           sync.RWMutex
	registry     *Registry
	ledgerPath   string
	lastSync     map[string]time.Time // agentID → last sync time
	syncInterval time.Duration        // minimum interval between syncs for the same agent
}

// DefaultSyncInterval is the recommended interval between automatic syncs.
// Spec implies periodic sync without specifying exact cadence; 5 minutes is
// a reasonable default for near-real-time updates without excessive ledger reads.
const DefaultSyncInterval = 5 * time.Minute

// NewTrustSync creates a TrustSync bound to the given registry and ledger path.
func NewTrustSync(reg *Registry, ledgerPath string) *TrustSync {
	return &TrustSync{
		registry:     reg,
		ledgerPath:   ledgerPath,
		lastSync:     make(map[string]time.Time),
		syncInterval: DefaultSyncInterval,
	}
}

// SyncAgent replays the trust ledger for one agent and updates the marketplace
// trust score if the score has changed or if the sync interval has elapsed.
// Returns the new marketplace trust score (0-100) and the raw trust score
// (0.0-1.0).
func (ts *TrustSync) SyncAgent(agentID string) (marketplaceScore int, rawScore float64, err error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Check if we need to sync (interval-based skip).
	if last, ok := ts.lastSync[agentID]; ok {
		if time.Since(last) < ts.syncInterval {
			// Return current cached score without re-reading ledger.
			agent, aErr := ts.registry.Get(agentID)
			if aErr != nil {
				return 0, 0, fmt.Errorf("get agent: %w", aErr)
			}
			return agent.TrustScore, float64(agent.TrustScore) / 100.0, nil
		}
	}

	// Replay ledger for authoritative score.
	score, err := trust.ReplayToScore(ts.ledgerPath, agentID)
	if err != nil {
		return 0, 0, fmt.Errorf("replay ledger for %s: %w", agentID, err)
	}

	rawScore = float64(score)
	marketplaceScore = ScoreToMarketplace(score)

	// Update agent in registry if it exists.
	if agent, aErr := ts.registry.Get(agentID); aErr == nil {
		if agent.TrustScore != marketplaceScore {
			agent.TrustScore = marketplaceScore
			agent.UpdatedAt = nowISO()
		}
	}

	ts.lastSync[agentID] = time.Now()
	return marketplaceScore, rawScore, nil
}

// SyncAll syncs every agent in the registry. Returns a SyncResult summarizing
// how many agents were updated and any errors encountered.
func (ts *TrustSync) SyncAll() (*SyncResult, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	agents, err := ts.registry.List(nil)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}

	result := &SyncResult{
		SyncedAt: time.Now(),
	}

	for _, a := range agents {
		score, err := trust.ReplayToScore(ts.ledgerPath, a.Name)
		if err != nil {
			result.Errors = append(result.Errors, SyncError{
				AgentID: a.Name,
				Error:   err.Error(),
			})
			continue
		}

		newScore := ScoreToMarketplace(score)
		if a.TrustScore != newScore {
			a.TrustScore = newScore
			a.UpdatedAt = nowISO()
			result.Updated++
		}
		result.Synced++
		ts.lastSync[a.Name] = time.Now()
	}

	return result, nil
}

// GetLiveScore returns the authoritative trust score for an agent from the
// ledger without updating the registry. This is the "source of truth" query.
func (ts *TrustSync) GetLiveScore(agentID string) (float64, error) {
	score, err := trust.ReplayToScore(ts.ledgerPath, agentID)
	if err != nil {
		return 0, fmt.Errorf("get live score for %s: %w", agentID, err)
	}
	return float64(score), nil
}

// SetSyncInterval overrides the minimum interval between syncs for the same
// agent. Use 0 to force every SyncAgent call to re-read the ledger.
func (ts *TrustSync) SetSyncInterval(d time.Duration) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.syncInterval = d
}

// ScoreToMarketplace converts a trust engine score (0.0–1.0 float64) to the
// marketplace scale (0–100 int). The trust engine is the source of truth;
// the marketplace stores a derived integer snapshot.
func ScoreToMarketplace(score trust.TrustScore) int {
	s := float64(score) * 100
	if s < 0 {
		return 0
	}
	if s > 100 {
		return 100
	}
	return int(s + 0.5) // round to nearest int
}

// MarketplaceToScore converts a marketplace integer score (0–100) back to the
// trust engine scale (0.0–1.0). This is a lossy inverse — used when the
// ledger is unavailable and the marketplace snapshot is the only data.
func MarketplaceToScore(marketplaceScore int) float64 {
	if marketplaceScore < 0 {
		return 0
	}
	if marketplaceScore > 100 {
		return 1.0
	}
	return float64(marketplaceScore) / 100.0
}

// SyncResult summarizes a SyncAll operation.
type SyncResult struct {
	SyncedAt time.Time   `json:"synced_at"`
	Synced   int         `json:"synced"`
	Updated  int         `json:"updated"`
	Errors   []SyncError `json:"errors,omitempty"`
}

// SyncError captures a failure syncing one agent.
type SyncError struct {
	AgentID string `json:"agent_id"`
	Error   string `json:"error"`
}
