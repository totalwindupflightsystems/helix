package marketplace

import (
	"testing"
)

// ---------------------------------------------------------------------------
// TestAutoDeprecationRules — spec §10.2 time-window enforcement
// ---------------------------------------------------------------------------

func TestAutoDeprecationRules(t *testing.T) {
	// Fixed reference time for deterministic time-window checks.
	now := parseTimestamp("2026-06-30T12:00:00Z")
	nowStr := "2026-06-30T12:00:00Z"

	// Helper: N days ago as RFC3339.
	daysAgo := func(days int) string {
		return now.AddDate(0, 0, -days).Format("2006-01-02T15:04:05Z")
	}

	tests := []struct {
		name    string
		agents  map[string]*Agent
		want    []string
		wantLen int
	}{
		{
			name:    "no_agents",
			agents:  map[string]*Agent{},
			wantLen: 0,
		},
		{
			name: "rule1_trust_below_20_for_30d",
			agents: map[string]*Agent{
				"low-trust": {
					Name:        "low-trust",
					Status:      StatusActive,
					TrustScore:  19,
					Performance: Performance{TasksCompleted: 1},
					History:     AgentHistory{TrustDroppedAt: daysAgo(35)},
				},
			},
			want: []string{"low-trust"},
		},
		{
			name: "rule1_trust_below_20_but_not_30d_yet",
			agents: map[string]*Agent{
				"recently-low": {
					Name:        "recently-low",
					Status:      StatusActive,
					TrustScore:  19,
					Performance: Performance{TasksCompleted: 1},
					History:     AgentHistory{TrustDroppedAt: daysAgo(10)},
				},
			},
			wantLen: 0,
		},
		{
			name: "rule1_trust_at_20_not_deprecated",
			agents: map[string]*Agent{
				"borderline": {
					Name:        "borderline",
					Status:      StatusActive,
					TrustScore:  20,
					Performance: Performance{TasksCompleted: 1},
				},
			},
			wantLen: 0,
		},
		{
			name: "rule1_trust_below_20_no_timestamp_not_deprecated",
			agents: map[string]*Agent{
				"low-trust-no-ts": {
					Name:        "low-trust-no-ts",
					Status:      StatusActive,
					TrustScore:  19,
					Performance: Performance{TasksCompleted: 1},
				},
			},
			wantLen: 0,
		},
		{
			name: "rule2_no_tasks_90d",
			agents: map[string]*Agent{
				"idle": {
					Name:        "idle",
					Status:      StatusActive,
					TrustScore:  25,
					Performance: Performance{TasksCompleted: 0},
					CreatedAt:   daysAgo(95),
				},
			},
			want: []string{"idle"},
		},
		{
			name: "rule2_no_tasks_but_created_recently",
			agents: map[string]*Agent{
				"new-agent": {
					Name:        "new-agent",
					Status:      StatusActive,
					TrustScore:  25,
					Performance: Performance{TasksCompleted: 0},
					CreatedAt:   daysAgo(30),
				},
			},
			wantLen: 0,
		},
		{
			name: "rule2_last_task_95d_ago",
			agents: map[string]*Agent{
				"stale-agent": {
					Name:        "stale-agent",
					Status:      StatusActive,
					TrustScore:  50,
					Performance: Performance{TasksCompleted: 10},
					History:     AgentHistory{LastTaskCompletedAt: daysAgo(95)},
				},
			},
			want: []string{"stale-agent"},
		},
		{
			name: "rule2_last_task_10d_ago_not_deprecated",
			agents: map[string]*Agent{
				"active-agent": {
					Name:        "active-agent",
					Status:      StatusActive,
					TrustScore:  50,
					Performance: Performance{TasksCompleted: 10},
					History:     AgentHistory{LastTaskCompletedAt: daysAgo(10)},
				},
			},
			wantLen: 0,
		},
		{
			name: "rule3_budget_exhausted_14d",
			agents: map[string]*Agent{
				"overspent": {
					Name:       "overspent",
					Status:     StatusActive,
					TrustScore: 50,
					Budget:     Budget{WeeklyLimit: 100, AverageTaskCost: 100},
					History:    AgentHistory{BudgetExhaustedAt: daysAgo(15)},
				},
			},
			want: []string{"overspent"},
		},
		{
			name: "rule3_budget_exhausted_but_only_5d",
			agents: map[string]*Agent{
				"recently-broke": {
					Name:       "recently-broke",
					Status:     StatusActive,
					TrustScore: 50,
					Budget:     Budget{WeeklyLimit: 100, AverageTaskCost: 100},
					History:    AgentHistory{BudgetExhaustedAt: daysAgo(5)},
				},
			},
			wantLen: 0,
		},
		{
			name: "rule3_budget_exhausted_no_timestamp",
			agents: map[string]*Agent{
				"broke-no-ts": {
					Name:       "broke-no-ts",
					Status:     StatusActive,
					TrustScore: 50,
					Budget:     Budget{WeeklyLimit: 100, AverageTaskCost: 100},
				},
			},
			wantLen: 0,
		},
		{
			name: "rule3_budget_not_exhausted",
			agents: map[string]*Agent{
				"under-budget": {
					Name:       "under-budget",
					Status:     StatusActive,
					TrustScore: 50,
					Budget:     Budget{WeeklyLimit: 100, AverageTaskCost: 50},
					History:    AgentHistory{BudgetExhaustedAt: daysAgo(20)},
				},
			},
			wantLen: 0,
		},
		{
			name: "rule3_zero_weekly_limit_skipped",
			agents: map[string]*Agent{
				"no-limit": {
					Name:       "no-limit",
					Status:     StatusActive,
					TrustScore: 50,
					Budget:     Budget{WeeklyLimit: 0, AverageTaskCost: 999},
				},
			},
			wantLen: 0,
		},
		{
			name: "non_active_agents_skipped",
			agents: map[string]*Agent{
				"deprecated-trust-10": {
					Name:       "deprecated-trust-10",
					Status:     StatusDeprecated,
					TrustScore: 10,
					History:    AgentHistory{TrustDroppedAt: daysAgo(60)},
				},
				"retired-trust-5": {
					Name:       "retired-trust-5",
					Status:     StatusRetired,
					TrustScore: 5,
					History:    AgentHistory{TrustDroppedAt: daysAgo(60)},
				},
			},
			wantLen: 0,
		},
		{
			name: "multiple_rules_triggered",
			agents: map[string]*Agent{
				"trust-low-30d": {
					Name:        "trust-low-30d",
					Status:      StatusActive,
					TrustScore:  10,
					Performance: Performance{TasksCompleted: 1},
					History:     AgentHistory{TrustDroppedAt: daysAgo(35)},
				},
				"no-tasks-90d": {
					Name:        "no-tasks-90d",
					Status:      StatusActive,
					TrustScore:  25,
					Performance: Performance{TasksCompleted: 0},
					CreatedAt:   daysAgo(100),
				},
				"overspent-14d": {
					Name:       "overspent-14d",
					Status:     StatusActive,
					TrustScore: 50,
					Budget:     Budget{WeeklyLimit: 50, AverageTaskCost: 100},
					History:    AgentHistory{BudgetExhaustedAt: daysAgo(20)},
				},
				"fine": {
					Name:        "fine",
					Status:      StatusActive,
					TrustScore:  80,
					Performance: Performance{TasksCompleted: 10},
					History:     AgentHistory{LastTaskCompletedAt: daysAgo(1)},
				},
			},
			want: []string{"trust-low-30d", "no-tasks-90d", "overspent-14d"},
		},
		{
			name: "status_updated_after_deprecation",
			agents: map[string]*Agent{
				"doomed": {
					Name:        "doomed",
					Status:      StatusActive,
					TrustScore:  10,
					Performance: Performance{TasksCompleted: 1},
					History:     AgentHistory{TrustDroppedAt: daysAgo(40)},
				},
			},
			want: []string{"doomed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Registry{agents: tt.agents}
			got, err := r.AutoDeprecationRulesAt(now)
			if err != nil {
				t.Fatalf("AutoDeprecationRulesAt() unexpected error: %v", err)
			}

			expectedLen := len(tt.want)
			if tt.wantLen != 0 || tt.want == nil {
				expectedLen = tt.wantLen
			}

			if len(got) != expectedLen {
				t.Errorf("got %d deprecated (names=%v), want %d", len(got), got, expectedLen)
				return
			}

			gotSet := make(map[string]bool, len(got))
			for _, name := range got {
				gotSet[name] = true
			}
			for _, wantName := range tt.want {
				if !gotSet[wantName] {
					t.Errorf("expected %q to be deprecated, but it wasn't (got: %v)", wantName, got)
				}
			}

			for _, name := range got {
				a := r.agents[name]
				if a.Status != StatusDeprecated {
					t.Errorf("agent %q status = %q, want %q", name, a.Status, StatusDeprecated)
				}
				if a.DeprecatedAt == "" {
					t.Errorf("agent %q DeprecatedAt not set", name)
				}
				if a.UpdatedAt == "" {
					t.Errorf("agent %q UpdatedAt not set", name)
				}
			}
		})
	}
	_ = nowStr // used in closure
}

// ---------------------------------------------------------------------------
// TestReactivate
// ---------------------------------------------------------------------------

func TestReactivate(t *testing.T) {
	tests := []struct {
		name    string
		agents  map[string]*Agent
		target  string
		wantErr bool
		errStr  string
	}{
		{
			name: "success_deprecated_to_active",
			agents: map[string]*Agent{
				"old-agent": {
					Name:         "old-agent",
					Status:       StatusDeprecated,
					TrustScore:   40,
					DeprecatedAt: "2026-01-15T00:00:00Z",
				},
			},
			target:  "old-agent",
			wantErr: false,
		},
		{
			name:    "agent_not_found",
			agents:  map[string]*Agent{},
			target:  "nobody",
			wantErr: true,
			errStr:  "AGENT_NOT_FOUND",
		},
		{
			name: "not_deprecated_active",
			agents: map[string]*Agent{
				"active-one": {Name: "active-one", Status: StatusActive},
			},
			target:  "active-one",
			wantErr: true,
			errStr:  "only deprecated agents can be reactivated",
		},
		{
			name: "not_deprecated_retired",
			agents: map[string]*Agent{
				"retired-one": {Name: "retired-one", Status: StatusRetired},
			},
			target:  "retired-one",
			wantErr: true,
			errStr:  "only deprecated agents can be reactivated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Registry{agents: tt.agents}
			err := r.Reactivate(tt.target)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Reactivate(%q) expected error, got nil", tt.target)
				}
				if tt.errStr != "" {
					errMsg := err.Error()
					if !contains(errMsg, tt.errStr) {
						t.Errorf("error %q does not contain %q", errMsg, tt.errStr)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("Reactivate(%q) unexpected error: %v", tt.target, err)
			}

			a := r.agents[tt.target]
			if a.Status != StatusActive {
				t.Errorf("agent %q status = %q, want %q", tt.target, a.Status, StatusActive)
			}
			if a.DeprecatedAt != "" {
				t.Errorf("agent %q DeprecatedAt = %q, should be cleared", tt.target, a.DeprecatedAt)
			}
			if a.UpdatedAt == "" {
				t.Errorf("agent %q UpdatedAt not set after reactivation", tt.target)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
