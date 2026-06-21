package marketplace

import (
	"testing"
)

// ---------------------------------------------------------------------------
// TestAutoDeprecationRules
// ---------------------------------------------------------------------------

func TestAutoDeprecationRules(t *testing.T) {
	tests := []struct {
		name    string
		agents  map[string]*Agent
		want    []string // expected deprecated names (order-insensitive)
		wantLen int      // override for len check when ordering varies
	}{
		{
			name:    "no_agents",
			agents:  map[string]*Agent{},
			wantLen: 0,
		},
		{
			name: "rule1_trust_below_20",
			agents: map[string]*Agent{
				"low-trust": {Name: "low-trust", Status: StatusActive, TrustScore: 19},
			},
			want: []string{"low-trust"},
		},
		{
			name: "rule1_trust_at_20_not_deprecated",
			agents: map[string]*Agent{
				"borderline": {
					Name: "borderline", Status: StatusActive, TrustScore: 20,
					Performance: Performance{TasksCompleted: 1}, // has tasks → Rule 2 won't fire
				},
			},
			wantLen: 0,
		},
		{
			name: "rule1_trust_at_19_deprecated",
			agents: map[string]*Agent{
				"barely": {Name: "barely", Status: StatusActive, TrustScore: 19},
			},
			want: []string{"barely"},
		},
		{
			name: "rule2_no_tasks_and_trust_below_30",
			agents: map[string]*Agent{
				"idle": {
					Name: "idle", Status: StatusActive, TrustScore: 25,
					Performance: Performance{TasksCompleted: 0},
				},
			},
			want: []string{"idle"},
		},
		{
			name: "rule2_no_tasks_but_trust_30_not_deprecated",
			agents: map[string]*Agent{
				"idle-but-trusted": {
					Name: "idle-but-trusted", Status: StatusActive, TrustScore: 30,
					Performance: Performance{TasksCompleted: 0},
				},
			},
			wantLen: 0,
		},
		{
			name: "rule2_has_tasks_low_trust_not_deprecated",
			agents: map[string]*Agent{
				"busy": {
					Name: "busy", Status: StatusActive, TrustScore: 25,
					Performance: Performance{TasksCompleted: 5},
				},
			},
			wantLen: 0,
		},
		{
			name: "rule3_budget_exhausted",
			agents: map[string]*Agent{
				"overspent": {
					Name: "overspent", Status: StatusActive, TrustScore: 50,
					Budget: Budget{WeeklyLimit: 100, AverageTaskCost: 100},
				},
			},
			want: []string{"overspent"},
		},
		{
			name: "rule3_budget_exhausted_above_limit",
			agents: map[string]*Agent{
				"way-over": {
					Name: "way-over", Status: StatusActive, TrustScore: 50,
					Budget: Budget{WeeklyLimit: 100, AverageTaskCost: 200},
				},
			},
			want: []string{"way-over"},
		},
		{
			name: "rule3_budget_not_exhausted",
			agents: map[string]*Agent{
				"under-budget": {
					Name: "under-budget", Status: StatusActive, TrustScore: 50,
					Budget: Budget{WeeklyLimit: 100, AverageTaskCost: 50},
				},
			},
			wantLen: 0,
		},
		{
			name: "rule3_zero_weekly_limit_skipped",
			agents: map[string]*Agent{
				"no-limit": {
					Name: "no-limit", Status: StatusActive, TrustScore: 50,
					Budget: Budget{WeeklyLimit: 0, AverageTaskCost: 999},
				},
			},
			wantLen: 0,
		},
		{
			name: "non_active_agents_skipped",
			agents: map[string]*Agent{
				"deprecated-trust-10": {Name: "deprecated-trust-10", Status: StatusDeprecated, TrustScore: 10},
				"retired-trust-5":     {Name: "retired-trust-5", Status: StatusRetired, TrustScore: 5},
			},
			wantLen: 0,
		},
		{
			name: "multiple_rules_triggered",
			agents: map[string]*Agent{
				"trust-10": {
					Name: "trust-10", Status: StatusActive, TrustScore: 10,
				},
				"no-tasks-low-trust": {
					Name: "no-tasks-low-trust", Status: StatusActive, TrustScore: 20,
					Performance: Performance{TasksCompleted: 0},
				},
				"overspent": {
					Name: "overspent", Status: StatusActive, TrustScore: 50,
					Budget: Budget{WeeklyLimit: 50, AverageTaskCost: 100},
				},
				"fine": {Name: "fine", Status: StatusActive, TrustScore: 80},
			},
			want: []string{"trust-10", "no-tasks-low-trust", "overspent"},
		},
		{
			name: "status_updated_after_deprecation",
			agents: map[string]*Agent{
				"doomed": {Name: "doomed", Status: StatusActive, TrustScore: 10},
			},
			want: []string{"doomed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Registry{agents: tt.agents}
			got, err := r.AutoDeprecationRules()
			if err != nil {
				t.Fatalf("AutoDeprecationRules() unexpected error: %v", err)
			}

			expectedLen := len(tt.want)
			if tt.wantLen != 0 || tt.want == nil {
				expectedLen = tt.wantLen
			}

			if len(got) != expectedLen {
				t.Errorf("got %d deprecated (names=%v), want %d", len(got), got, expectedLen)
				return
			}

			// Check each expected name appears
			gotSet := make(map[string]bool, len(got))
			for _, name := range got {
				gotSet[name] = true
			}
			for _, wantName := range tt.want {
				if !gotSet[wantName] {
					t.Errorf("expected %q to be deprecated, but it wasn't (got: %v)", wantName, got)
				}
			}

			// Verify deprecated agents have their status and timestamps updated
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
		errStr  string // substring to match in error
	}{
		{
			name: "success_deprecated_to_active",
			agents: map[string]*Agent{
				"old-agent": {
					Name: "old-agent", Status: StatusDeprecated, TrustScore: 40,
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

			// Verify agent is now active
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
