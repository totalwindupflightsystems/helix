package marketplace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// TestNewRegistry
// ---------------------------------------------------------------------------

func TestNewRegistry(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(dir string) // creates directory structure
		wantLen   int
		wantErr   bool
		wantNames []string // expected agent names loaded
	}{
		{
			name:    "missing_directory_returns_empty_registry",
			setup:   nil, // don't create anything
			wantLen: 0,
			wantErr: false,
		},
		{
			name: "empty_agents_dir_returns_empty_registry",
			setup: func(dir string) {
				_ = os.MkdirAll(filepath.Join(dir, "agents"), 0o755)
			},
			wantLen: 0,
			wantErr: false,
		},
		{
			name: "loads_valid_agent_manifests",
			setup: func(dir string) {
				agentsDir := filepath.Join(dir, "agents")
				_ = os.MkdirAll(agentsDir, 0o755)
				_ = os.WriteFile(filepath.Join(agentsDir, "coder-1.yaml"), []byte(`name: coder-1
display_name: "Code Agent"
status: active
tier: pro
trust_score: 85
capabilities: [go, code-review]
budget: {weekly_limit: 10.0, average_task_cost: 0.15, cost_profile: low}`), 0o644)
				_ = os.WriteFile(filepath.Join(agentsDir, "reviewer-2.yaml"), []byte(`name: reviewer-2
display_name: "Review Agent"
status: deprecated
tier: flash
trust_score: 50
capabilities: [code-review, security-review]
budget: {weekly_limit: 5.0, average_task_cost: 0.05, cost_profile: low}`), 0o644)
			},
			wantLen:   2,
			wantNames: []string{"coder-1", "reviewer-2"},
		},
		{
			name: "skips_non_yaml_files",
			setup: func(dir string) {
				agentsDir := filepath.Join(dir, "agents")
				_ = os.MkdirAll(agentsDir, 0o755)
				_ = os.WriteFile(filepath.Join(agentsDir, "readme.txt"), []byte("instructions"), 0o644)
				_ = os.WriteFile(filepath.Join(agentsDir, "agent.yaml"), []byte(`name: agent
status: active
tier: flash
capabilities: [go]
budget: {weekly_limit: 1.0, average_task_cost: 0.01, cost_profile: low}`), 0o644)
			},
			wantLen:   1,
			wantNames: []string{"agent"},
		},
		{
			name: "skips_malformed_yaml",
			setup: func(dir string) {
				agentsDir := filepath.Join(dir, "agents")
				_ = os.MkdirAll(agentsDir, 0o755)
				_ = os.WriteFile(filepath.Join(agentsDir, "bad.yaml"), []byte("{{{not valid yaml"), 0o644)
				_ = os.WriteFile(filepath.Join(agentsDir, "good.yaml"), []byte(`name: good
status: active
tier: flash
capabilities: [go]
budget: {weekly_limit: 1.0, average_task_cost: 0.01, cost_profile: low}`), 0o644)
			},
			wantLen:   1,
			wantNames: []string{"good"},
		},
		{
			name: "skips_agent_with_empty_name",
			setup: func(dir string) {
				agentsDir := filepath.Join(dir, "agents")
				_ = os.MkdirAll(agentsDir, 0o755)
				_ = os.WriteFile(filepath.Join(agentsDir, "noname.yaml"), []byte(`name: ""
display_name: "No Name"
status: active
tier: flash
capabilities: [go]
budget: {weekly_limit: 1.0, average_task_cost: 0.01, cost_profile: low}`), 0o644)
			},
			wantLen: 0,
		},
		{
			name: "defaults_empty_status_to_active",
			setup: func(dir string) {
				agentsDir := filepath.Join(dir, "agents")
				_ = os.MkdirAll(agentsDir, 0o755)
				_ = os.WriteFile(filepath.Join(agentsDir, "nostatus.yaml"), []byte(`name: nostatus
display_name: "No Status"
tier: flash
capabilities: [go]
budget: {weekly_limit: 1.0, average_task_cost: 0.01, cost_profile: low}`), 0o644)
			},
			wantLen:   1,
			wantNames: []string{"nostatus"},
		},
		{
			name: "skips_subdirectories",
			setup: func(dir string) {
				agentsDir := filepath.Join(dir, "agents")
				_ = os.MkdirAll(filepath.Join(agentsDir, "subdir"), 0o755)
				_ = os.WriteFile(filepath.Join(agentsDir, "agent.yaml"), []byte(`name: agent
status: active
tier: flash
capabilities: [go]
budget: {weekly_limit: 1.0, average_task_cost: 0.01, cost_profile: low}`), 0o644)
			},
			wantLen:   1,
			wantNames: []string{"agent"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.setup != nil {
				tt.setup(dir)
			}

			r, err := NewRegistry(dir)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(r.agents) != tt.wantLen {
				t.Fatalf("agent count: got %d, want %d", len(r.agents), tt.wantLen)
			}
			for _, name := range tt.wantNames {
				if _, ok := r.agents[name]; !ok {
					t.Errorf("expected agent %q not found", name)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestRegistry_Register
// ---------------------------------------------------------------------------

func TestRegistry_Register(t *testing.T) {
	tests := []struct {
		name     string
		agent    *Agent
		wantErr  bool
		wantCode int // exit code if error expected; 0 = no error
	}{
		{
			name:     "nil_agent_returns_error",
			agent:    nil,
			wantErr:  true,
			wantCode: ExitManifestInvalid,
		},
		{
			name: "empty_name_returns_error",
			agent: &Agent{
				Name: "",
			},
			wantErr:  true,
			wantCode: ExitManifestInvalid,
		},
		{
			name: "invalid_capability_returns_error",
			agent: &Agent{
				Name:         "bad-agent",
				Capabilities: []Capability{"not-real"},
			},
			wantErr:  true,
			wantCode: ExitInvalidCapability,
		},
		{
			name: "valid_register_sets_status_and_timestamps",
			agent: &Agent{
				Name:         "coder-1",
				DisplayName:  "Code Agent",
				Capabilities: []Capability{CapGo, CapCodeReview},
				Budget: Budget{
					WeeklyLimit:     10.0,
					AverageTaskCost: 0.15,
					CostProfile:     CostLow,
				},
			},
			wantErr: false,
		},
		{
			name: "overwrite_existing_agent_replaces_entry",
			agent: &Agent{
				Name:         "coder-1",
				DisplayName:  "Updated Code Agent",
				TrustScore:   90,
				Capabilities: []Capability{CapGo, CapPython, CapCodeReview},
				Budget: Budget{
					WeeklyLimit:     20.0,
					AverageTaskCost: 0.20,
					CostProfile:     CostMedium,
				},
			},
			wantErr: false,
		},
		{
			name: "defaults_empty_status_to_active",
			agent: &Agent{
				Name:         "nostatus",
				Capabilities: []Capability{CapGo},
				Budget: Budget{
					WeeklyLimit:     5.0,
					AverageTaskCost: 0.05,
					CostProfile:     CostLow,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Registry{
				agents: make(map[string]*Agent),
			}
			// Pre-register an agent for overwrite test
			if tt.name == "overwrite_existing_agent_replaces_entry" {
				r.agents["coder-1"] = &Agent{
					Name:         "coder-1",
					DisplayName:  "Old Agent",
					TrustScore:   50,
					Capabilities: []Capability{CapGo},
					Status:       StatusActive,
					Budget: Budget{
						WeeklyLimit:     5.0,
						AverageTaskCost: 0.10,
						CostProfile:     CostLow,
					},
				}
			}

			err := r.Register(tt.agent)
			if tt.wantErr != (err != nil) {
				t.Fatalf("Register() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr {
				var ee *ExitError
				if !errors.As(err, &ee) {
					t.Fatalf("expected ExitError, got %T: %v", err, err)
				}
				if ee.Code != tt.wantCode {
					t.Errorf("error code = %d, want %d", ee.Code, tt.wantCode)
				}
				return
			}

			// Verify agent was stored
			got, ok := r.agents[tt.agent.Name]
			if !ok {
				t.Fatalf("agent %q not found after Register", tt.agent.Name)
			}
			if got.Name != tt.agent.Name {
				t.Errorf("Name = %q, want %q", got.Name, tt.agent.Name)
			}
			if got.CreatedAt == "" {
				t.Error("CreatedAt should be set")
			}
			if got.UpdatedAt == "" {
				t.Error("UpdatedAt should be set")
			}

			// For overwrite test, verify the new data
			if tt.name == "overwrite_existing_agent_replaces_entry" {
				if got.TrustScore != 90 {
					t.Errorf("TrustScore = %d, want 90", got.TrustScore)
				}
				if len(got.Capabilities) != 3 {
					t.Errorf("Capabilities len = %d, want 3", len(got.Capabilities))
				}
			}

			// For nostatus test, verify status defaulted
			if tt.name == "defaults_empty_status_to_active" {
				if got.Status != StatusActive {
					t.Errorf("Status = %q, want %q (active)", got.Status, StatusActive)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestRegistry_Get
// ---------------------------------------------------------------------------

func TestRegistry_Get(t *testing.T) {
	r := &Registry{
		agents: map[string]*Agent{
			"coder-1": {
				Name:       "coder-1",
				TrustScore: 85,
				Status:     StatusActive,
			},
		},
	}

	tests := []struct {
		name     string
		lookup   string
		wantErr  bool
		wantCode int
		wantName string
	}{
		{
			name:     "found_returns_agent",
			lookup:   "coder-1",
			wantName: "coder-1",
		},
		{
			name:     "not_found_returns_error",
			lookup:   "nonexistent",
			wantErr:  true,
			wantCode: ExitAgentNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent, err := r.Get(tt.lookup)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				var ee *ExitError
				if !errors.As(err, &ee) {
					t.Fatalf("expected ExitError, got %T: %v", err, err)
				}
				if ee.Code != tt.wantCode {
					t.Errorf("error code = %d, want %d", ee.Code, tt.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if agent.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", agent.Name, tt.wantName)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestRegistry_Search
// ---------------------------------------------------------------------------

func TestRegistry_Search(t *testing.T) {
	// Shared registry with diverse agents
	r := &Registry{
		agents: map[string]*Agent{
			"coder-1": {
				Name:         "coder-1",
				Status:       StatusActive,
				TrustScore:   85,
				Capabilities: []Capability{CapGo, CapCodeReview},
				Budget: Budget{
					AverageTaskCost: 0.15,
				},
			},
			"reviewer-2": {
				Name:         "reviewer-2",
				Status:       StatusDeprecated,
				TrustScore:   50,
				Capabilities: []Capability{CapCodeReview, CapSecurityReview},
				Budget: Budget{
					AverageTaskCost: 0.05,
				},
			},
			"python-bot": {
				Name:         "python-bot",
				Status:       StatusActive,
				TrustScore:   92,
				Capabilities: []Capability{CapPython, CapTesting},
				Budget: Budget{
					AverageTaskCost: 0.25,
				},
			},
			"retired-old": {
				Name:         "retired-old",
				Status:       StatusRetired,
				TrustScore:   15,
				Capabilities: []Capability{CapGo},
				Budget: Budget{
					AverageTaskCost: 0.30,
				},
			},
			"docs-bot": {
				Name:         "docs-bot",
				Status:       StatusActive,
				TrustScore:   40,
				Capabilities: []Capability{CapDocs},
				Budget: Budget{
					AverageTaskCost: 0.02,
				},
			},
		},
	}

	tests := []struct {
		name      string
		query     SearchQuery
		wantNames []string // expected names in order
		wantLen   int
	}{
		{
			name:      "no_filters_returns_all_non_retired_sorted_by_trust",
			query:     SearchQuery{},
			wantNames: []string{"python-bot", "coder-1", "reviewer-2", "docs-bot"},
			wantLen:   4,
		},
		{
			name: "capability_filter_and",
			query: SearchQuery{
				Capabilities: []Capability{CapGo, CapCodeReview},
			},
			wantNames: []string{"coder-1"},
			wantLen:   1,
		},
		{
			name: "single_capability_filter",
			query: SearchQuery{
				Capabilities: []Capability{CapCodeReview},
			},
			wantNames: []string{"coder-1", "reviewer-2"},
			wantLen:   2,
		},
		{
			name: "trust_filter_excludes_below_threshold",
			query: SearchQuery{
				MinTrust: 60,
			},
			wantNames: []string{"python-bot", "coder-1"},
			wantLen:   2,
		},
		{
			name: "cost_filter_excludes_above_max",
			query: SearchQuery{
				MaxCost: 0.10,
			},
			wantNames: []string{"reviewer-2", "docs-bot"},
			wantLen:   2,
		},
		{
			name: "retired_agents_excluded",
			query: SearchQuery{
				Capabilities: []Capability{CapGo},
			},
			wantNames: []string{"coder-1"},
			wantLen:   1,
		},
		{
			name: "combined_filters",
			query: SearchQuery{
				Capabilities: []Capability{CapCodeReview},
				MinTrust:     60,
				MaxCost:      0.20,
			},
			wantNames: []string{"coder-1"},
			wantLen:   1,
		},
		{
			name: "limit_trims_results",
			query: SearchQuery{
				Limit: 2,
			},
			wantNames: []string{"python-bot", "coder-1"},
			wantLen:   2,
		},
		{
			name: "no_results_returns_empty",
			query: SearchQuery{
				Capabilities: []Capability{CapNegotiation},
			},
			wantNames: nil,
			wantLen:   0,
		},
		{
			name: "zero_limit_returns_all",
			query: SearchQuery{
				Limit: 0,
			},
			wantNames: []string{"python-bot", "coder-1", "reviewer-2", "docs-bot"},
			wantLen:   4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := r.Search(tt.query)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(results) != tt.wantLen {
				t.Fatalf("result count: got %d, want %d", len(results), tt.wantLen)
			}
			if tt.wantNames != nil {
				for i, name := range tt.wantNames {
					if results[i].Name != name {
						t.Errorf("result[%d] = %q, want %q", i, results[i].Name, name)
					}
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestRegistry_UpdateStatus
// ---------------------------------------------------------------------------

func TestRegistry_UpdateStatus(t *testing.T) {
	tests := []struct {
		name                  string
		existingAgent         *Agent // nil = don't register
		updateName            string
		newStatus             AgentStatus
		wantErr               bool
		wantCode              int
		wantStatus            AgentStatus // expected status after update
		wantDeprecatedSet     bool        // whether DeprecatedAt should be set
		wantDeprecatedCleared bool        // whether DeprecatedAt should be cleared
	}{
		{
			name:       "agent_not_found",
			updateName: "nonexistent",
			newStatus:  StatusDeprecated,
			wantErr:    true,
			wantCode:   ExitAgentNotFound,
		},
		{
			name: "invalid_status",
			existingAgent: &Agent{
				Name:   "coder-1",
				Status: StatusActive,
			},
			updateName: "coder-1",
			newStatus:  "bad-status",
			wantErr:    true,
			wantCode:   ExitManifestInvalid,
		},
		{
			name: "active_to_deprecated_sets_deprecated_at",
			existingAgent: &Agent{
				Name:   "coder-1",
				Status: StatusActive,
			},
			updateName:        "coder-1",
			newStatus:         StatusDeprecated,
			wantStatus:        StatusDeprecated,
			wantDeprecatedSet: true,
		},
		{
			name: "deprecated_to_active_clears_deprecated_at",
			existingAgent: &Agent{
				Name:         "coder-1",
				Status:       StatusDeprecated,
				DeprecatedAt: "2026-01-01T00:00:00Z",
			},
			updateName:            "coder-1",
			newStatus:             StatusActive,
			wantStatus:            StatusActive,
			wantDeprecatedCleared: true,
		},
		{
			name: "active_to_active_no_error",
			existingAgent: &Agent{
				Name:   "coder-1",
				Status: StatusActive,
			},
			updateName: "coder-1",
			newStatus:  StatusActive,
			wantStatus: StatusActive,
		},
		{
			name: "deprecated_to_retired",
			existingAgent: &Agent{
				Name:         "coder-1",
				Status:       StatusDeprecated,
				DeprecatedAt: "2026-01-01T00:00:00Z",
			},
			updateName: "coder-1",
			newStatus:  StatusRetired,
			wantStatus: StatusRetired,
		},
		{
			name: "updated_at_is_set_on_transition",
			existingAgent: &Agent{
				Name:   "coder-1",
				Status: StatusActive,
			},
			updateName:        "coder-1",
			newStatus:         StatusDeprecated,
			wantStatus:        StatusDeprecated,
			wantDeprecatedSet: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Registry{
				agents: make(map[string]*Agent),
			}
			if tt.existingAgent != nil {
				r.agents[tt.existingAgent.Name] = tt.existingAgent
			}

			err := r.UpdateStatus(tt.updateName, tt.newStatus)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				var ee *ExitError
				if !errors.As(err, &ee) {
					t.Fatalf("expected ExitError, got %T: %v", err, err)
				}
				if ee.Code != tt.wantCode {
					t.Errorf("error code = %d, want %d", ee.Code, tt.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			agent := r.agents[tt.updateName]
			if agent.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", agent.Status, tt.wantStatus)
			}

			if tt.wantDeprecatedSet && agent.DeprecatedAt == "" {
				t.Error("expected DeprecatedAt to be set")
			}
			if tt.wantDeprecatedCleared && agent.DeprecatedAt != "" {
				t.Errorf("expected DeprecatedAt to be cleared, got %q", agent.DeprecatedAt)
			}
			if agent.UpdatedAt == "" {
				t.Error("expected UpdatedAt to be set on transition")
			}
		})
	}
}
