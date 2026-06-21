package marketplace

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// TestRebuildIndex
// ---------------------------------------------------------------------------

func TestRebuildIndex(t *testing.T) {
	tests := []struct {
		name    string
		agents  map[string]*Agent
		wantLen int
		want    map[string]*ManifestIndexEntry // nil = check len only
	}{
		{
			name:    "rebuilds_from_empty",
			agents:  map[string]*Agent{},
			wantLen: 0,
		},
		{
			name: "rebuilds_from_multiple_agents",
			agents: map[string]*Agent{
				"coder-1": {
					Name:       "coder-1",
					Status:     StatusActive,
					TrustScore: 85,
					Tier:       TierPro,
					Capabilities: []Capability{
						CapCodeReview, CapGo,
					},
					Budget: Budget{
						CostProfile: CostLow,
					},
					Ratings: Ratings{
						Average: 4.5,
					},
					UpdatedAt: "2026-06-22T10:00:00Z",
				},
				"reviewer-2": {
					Name:       "reviewer-2",
					Status:     StatusDeprecated,
					TrustScore: 50,
					Tier:       TierFlash,
					Capabilities: []Capability{
						CapCodeReview,
					},
					Budget: Budget{
						CostProfile: CostMedium,
					},
					Ratings: Ratings{
						Average: 3.0,
					},
					UpdatedAt: "2026-06-21T08:00:00Z",
				},
			},
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Registry{
				dir:    t.TempDir(),
				agents: tt.agents,
				index:  make(map[string]*ManifestIndexEntry),
			}
			err := r.RebuildIndex()
			if err != nil {
				t.Fatalf("RebuildIndex() error = %v", err)
			}
			if len(r.index) != tt.wantLen {
				t.Errorf("index len = %d, want %d", len(r.index), tt.wantLen)
			}
			for name, agent := range tt.agents {
				entry, ok := r.index[name]
				if !ok {
					t.Errorf("index missing entry for %s", name)
					continue
				}
				if entry.Status != agent.Status {
					t.Errorf("%s status = %s, want %s", name, entry.Status, agent.Status)
				}
				if entry.TrustScore != agent.TrustScore {
					t.Errorf("%s trust = %d, want %d", name, entry.TrustScore, agent.TrustScore)
				}
				if entry.Tier != agent.Tier {
					t.Errorf("%s tier = %s, want %s", name, entry.Tier, agent.Tier)
				}
			}
		})
	}
}

func TestRebuildIndex_CapabilityCopy(t *testing.T) {
	a := &Agent{
		Name:         "agent-1",
		Status:       StatusActive,
		TrustScore:   90,
		Tier:         TierPro,
		Capabilities: []Capability{CapGo, CapPython},
		Budget: Budget{
			CostProfile: CostMedium,
		},
		Ratings: Ratings{
			Average: 4.0,
		},
		UpdatedAt: "2026-06-22T10:00:00Z",
	}
	r := &Registry{
		dir:    t.TempDir(),
		agents: map[string]*Agent{"agent-1": a},
		index:  make(map[string]*ManifestIndexEntry),
	}
	if err := r.RebuildIndex(); err != nil {
		t.Fatalf("RebuildIndex() error = %v", err)
	}

	// Modify the original agent's capabilities — should not affect index.
	a.Capabilities[0] = CapSpecWriting

	entry := r.index["agent-1"]
	if entry == nil {
		t.Fatal("index missing entry for agent-1")
	}
	if len(entry.Capabilities) != 2 {
		t.Fatalf("capability count = %d, want 2", len(entry.Capabilities))
	}
	if entry.Capabilities[0] != CapGo {
		t.Errorf("capability[0] = %s, want %s (modification leaked into index)", entry.Capabilities[0], CapGo)
	}
}

func TestRebuildIndex_PersistsFile(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{
		Name:       "agent-1",
		Status:     StatusActive,
		TrustScore: 90,
		Tier:       TierPro,
		Capabilities: []Capability{
			CapGo, CapPython,
		},
		Budget: Budget{
			CostProfile: CostLow,
		},
		Ratings: Ratings{
			Average: 4.5,
		},
		UpdatedAt: "2026-06-22T10:00:00Z",
	}
	r := &Registry{
		dir:    dir,
		agents: map[string]*Agent{"agent-1": a},
		index:  make(map[string]*ManifestIndexEntry),
	}
	if err := r.RebuildIndex(); err != nil {
		t.Fatalf("RebuildIndex() error = %v", err)
	}

	indexPath := filepath.Join(dir, "_index.yaml")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("_index.yaml not written: %v", err)
	}
	if len(data) == 0 {
		t.Error("_index.yaml is empty")
	}
}

// ---------------------------------------------------------------------------
// TestIndexEntry
// ---------------------------------------------------------------------------

func TestIndexEntry(t *testing.T) {
	agent := &Agent{
		Name:       "coder-1",
		Status:     StatusActive,
		TrustScore: 85,
		Tier:       TierPro,
		Capabilities: []Capability{
			CapCodeReview, CapGo,
		},
		Budget: Budget{
			CostProfile: CostLow,
		},
		Ratings: Ratings{
			Average: 4.5,
		},
		UpdatedAt: "2026-06-22T10:00:00Z",
	}

	tests := []struct {
		name    string
		index   map[string]*ManifestIndexEntry
		agents  map[string]*Agent
		lookup  string
		wantErr bool
		want    string // expected status
	}{
		{
			name: "cache_hit",
			index: map[string]*ManifestIndexEntry{
				"coder-1": {
					Status:     StatusActive,
					TrustScore: 85,
					Tier:       TierPro,
					Capabilities: []Capability{
						CapCodeReview, CapGo,
					},
					CostProfile: CostLow,
					AvgRating:   4.5,
					UpdatedAt:   "2026-06-22T10:00:00Z",
				},
			},
			agents:  map[string]*Agent{},
			lookup:  "coder-1",
			wantErr: false,
			want:    string(StatusActive),
		},
		{
			name:    "cache_miss_computes_from_agent",
			index:   map[string]*ManifestIndexEntry{},
			agents:  map[string]*Agent{"coder-1": agent},
			lookup:  "coder-1",
			wantErr: false,
			want:    string(StatusActive),
		},
		{
			name:    "agent_not_found",
			index:   map[string]*ManifestIndexEntry{},
			agents:  map[string]*Agent{},
			lookup:  "nonexistent",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Registry{
				dir:    t.TempDir(),
				agents: tt.agents,
				index:  tt.index,
			}
			entry, err := r.IndexEntry(tt.lookup)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				ee, ok := err.(*ExitError)
				if !ok {
					t.Fatalf("expected *ExitError, got %T: %v", err, err)
				}
				if ee.Code != ExitAgentNotFound {
					t.Errorf("exit code = %d, want %d", ee.Code, ExitAgentNotFound)
				}
				return
			}
			if err != nil {
				t.Fatalf("IndexEntry() error = %v", err)
			}
			if entry == nil {
				t.Fatal("expected non-nil entry")
			}
			if string(entry.Status) != tt.want {
				t.Errorf("status = %s, want %s", entry.Status, tt.want)
			}
		})
	}
}

func TestIndexEntry_CachesAfterFirstLookup(t *testing.T) {
	a := &Agent{
		Name:       "coder-1",
		Status:     StatusActive,
		TrustScore: 85,
		Tier:       TierPro,
		Capabilities: []Capability{
			CapCodeReview, CapGo,
		},
		Budget: Budget{
			CostProfile: CostLow,
		},
		Ratings: Ratings{
			Average: 4.5,
		},
		UpdatedAt: "2026-06-22T10:00:00Z",
	}
	r := &Registry{
		dir:    t.TempDir(),
		agents: map[string]*Agent{"coder-1": a},
		index:  make(map[string]*ManifestIndexEntry),
	}

	// First lookup — cache miss, should compute.
	entry1, err := r.IndexEntry("coder-1")
	if err != nil {
		t.Fatalf("first IndexEntry() error = %v", err)
	}
	if entry1 == nil {
		t.Fatal("expected non-nil entry")
	}

	// Second lookup — should use cache (no error even if agents map changes).
	delete(r.agents, "coder-1") // simulate agent removed after first lookup
	entry2, err := r.IndexEntry("coder-1")
	if err != nil {
		t.Fatalf("second IndexEntry() error = %v", err)
	}
	if entry2 == nil {
		t.Fatal("expected cached entry")
	}
	if entry2.TrustScore != 85 {
		t.Errorf("cached trust = %d, want 85", entry2.TrustScore)
	}
}

// ---------------------------------------------------------------------------
// TestAgentToIndexEntry
// ---------------------------------------------------------------------------

func TestAgentToIndexEntry(t *testing.T) {
	agent := &Agent{
		Status:     StatusActive,
		TrustScore: 92,
		Tier:       TierPro,
		Capabilities: []Capability{
			CapGo, CapCodeReview, CapTesting,
		},
		Budget: Budget{
			CostProfile: CostMedium,
		},
		Ratings: Ratings{
			Average: 4.8,
		},
		UpdatedAt: "2026-06-22T12:00:00Z",
	}

	entry := agentToIndexEntry(agent)

	if entry.Status != StatusActive {
		t.Errorf("Status = %s, want %s", entry.Status, StatusActive)
	}
	if entry.TrustScore != 92 {
		t.Errorf("TrustScore = %d, want 92", entry.TrustScore)
	}
	if entry.Tier != TierPro {
		t.Errorf("Tier = %s, want %s", entry.Tier, TierPro)
	}
	if entry.CostProfile != CostMedium {
		t.Errorf("CostProfile = %s, want %s", entry.CostProfile, CostMedium)
	}
	if entry.AvgRating != 4.8 {
		t.Errorf("AvgRating = %f, want 4.8", entry.AvgRating)
	}
	if entry.ActiveTasks != 0 {
		t.Errorf("ActiveTasks = %d, want 0 (stub)", entry.ActiveTasks)
	}
	if entry.UpdatedAt != "2026-06-22T12:00:00Z" {
		t.Errorf("UpdatedAt = %s, want 2026-06-22T12:00:00Z", entry.UpdatedAt)
	}
	if len(entry.Capabilities) != 3 {
		t.Errorf("Capabilities len = %d, want 3", len(entry.Capabilities))
	}
}

func TestAgentToIndexEntry_CapabilityCopy(t *testing.T) {
	agent := &Agent{
		Status: StatusActive,
		Capabilities: []Capability{
			CapGo, CapPython,
		},
		Budget: Budget{
			CostProfile: CostLow,
		},
		Ratings: Ratings{
			Average: 3.5,
		},
	}

	entry := agentToIndexEntry(agent)

	// Modify the returned capabilities — should not affect the original agent.
	entry.Capabilities[0] = CapSpecWriting

	if agent.Capabilities[0] != CapGo {
		t.Errorf("original Capabilities[0] = %s, want %s (returned slice aliased to agent)", agent.Capabilities[0], CapGo)
	}
}
