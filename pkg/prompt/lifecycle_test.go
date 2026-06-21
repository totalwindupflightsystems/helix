package prompt

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// TestAllowedForAttestation
// ---------------------------------------------------------------------------

func TestAllowedForAttestation(t *testing.T) {
	tests := []struct {
		name        string
		status      LifecycleStatus
		wantAllowed bool
		wantWarn    bool
	}{
		{name: "attested", status: StatusAttested, wantAllowed: true, wantWarn: false},
		{name: "active", status: StatusActive, wantAllowed: true, wantWarn: false},
		{name: "deprecated_allowed_with_warning", status: StatusDeprecated, wantAllowed: true, wantWarn: true},
		{name: "draft_not_allowed", status: StatusDraft, wantAllowed: false, wantWarn: false},
		{name: "proposed_not_allowed", status: StatusProposed, wantAllowed: false, wantWarn: false},
		{name: "reviewed_not_allowed", status: StatusReviewed, wantAllowed: false, wantWarn: false},
		{name: "retired_not_allowed", status: StatusRetired, wantAllowed: false, wantWarn: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed, warn := AllowedForAttestation(tt.status)
			if allowed != tt.wantAllowed {
				t.Errorf("allowed = %v, want %v", allowed, tt.wantAllowed)
			}
			if warn != tt.wantWarn {
				t.Errorf("warn = %v, want %v", warn, tt.wantWarn)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestValidTransition
// ---------------------------------------------------------------------------

func TestValidTransition(t *testing.T) {
	tests := []struct {
		name string
		from LifecycleStatus
		to   LifecycleStatus
		want bool
	}{
		// Standard forward transitions
		{name: "draft_to_proposed", from: StatusDraft, to: StatusProposed, want: true},
		{name: "proposed_to_reviewed", from: StatusProposed, to: StatusReviewed, want: true},
		{name: "reviewed_to_attested", from: StatusReviewed, to: StatusAttested, want: true},
		{name: "attested_to_active", from: StatusAttested, to: StatusActive, want: true},
		{name: "active_to_deprecated", from: StatusActive, to: StatusDeprecated, want: true},
		{name: "deprecated_to_retired", from: StatusDeprecated, to: StatusRetired, want: true},

		// Rollback path
		{name: "deprecated_to_active_rollback", from: StatusDeprecated, to: StatusActive, want: true},

		// Backward path (proposed → draft)
		{name: "proposed_to_draft", from: StatusProposed, to: StatusDraft, want: true},

		// Invalid transitions
		{name: "draft_to_active_not_allowed", from: StatusDraft, to: StatusActive, want: false},
		{name: "draft_to_retired_not_allowed", from: StatusDraft, to: StatusRetired, want: false},
		{name: "active_to_draft_not_allowed", from: StatusActive, to: StatusDraft, want: false},
		{name: "retired_to_anything", from: StatusRetired, to: StatusActive, want: false},
		{name: "retired_to_draft", from: StatusRetired, to: StatusDraft, want: false},
		{name: "retired_to_deprecated", from: StatusRetired, to: StatusDeprecated, want: false},
		{name: "proposed_to_deprecated_skip", from: StatusProposed, to: StatusDeprecated, want: false},
		{name: "attested_to_deprecated_skip", from: StatusAttested, to: StatusDeprecated, want: false},
		{name: "draft_to_reviewed_skip", from: StatusDraft, to: StatusReviewed, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidTransition(tt.from, tt.to)
			if got != tt.want {
				t.Errorf("ValidTransition(%s, %s) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestAllowedTransitions
// ---------------------------------------------------------------------------

func TestAllowedTransitions(t *testing.T) {
	tests := []struct {
		name string
		from LifecycleStatus
		want []LifecycleStatus
	}{
		{name: "draft", from: StatusDraft, want: []LifecycleStatus{StatusProposed}},
		{name: "proposed", from: StatusProposed, want: []LifecycleStatus{StatusReviewed, StatusDraft}},
		{name: "reviewed", from: StatusReviewed, want: []LifecycleStatus{StatusAttested}},
		{name: "attested", from: StatusAttested, want: []LifecycleStatus{StatusActive}},
		{name: "active", from: StatusActive, want: []LifecycleStatus{StatusDeprecated}},
		{name: "deprecated", from: StatusDeprecated, want: []LifecycleStatus{StatusRetired, StatusActive}},
		{name: "retired_terminal", from: StatusRetired, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AllowedTransitions(tt.from)
			if tt.want == nil {
				if len(got) != 0 {
					t.Errorf("expected empty slice, got %v", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("len = %d, want %d; got %v", len(got), len(tt.want), got)
				return
			}
			gotSet := make(map[LifecycleStatus]bool)
			for _, s := range got {
				gotSet[s] = true
			}
			for _, s := range tt.want {
				if !gotSet[s] {
					t.Errorf("missing expected transition to %s", s)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestDeprecationGrace
// ---------------------------------------------------------------------------

func TestDeprecationGrace(t *testing.T) {
	tests := []struct {
		name   string
		status LifecycleStatus
		want   time.Duration
	}{
		{name: "deprecated_30_days", status: StatusDeprecated, want: 30 * 24 * time.Hour},
		{name: "retired_90_days", status: StatusRetired, want: 90 * 24 * time.Hour},
		{name: "draft_zero", status: StatusDraft, want: 0},
		{name: "proposed_zero", status: StatusProposed, want: 0},
		{name: "reviewed_zero", status: StatusReviewed, want: 0},
		{name: "attested_zero", status: StatusAttested, want: 0},
		{name: "active_zero", status: StatusActive, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeprecationGrace(tt.status)
			if got != tt.want {
				t.Errorf("DeprecationGrace(%s) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestValidateTransition
// ---------------------------------------------------------------------------

func TestValidateTransition(t *testing.T) {
	tests := []struct {
		name        string
		from        LifecycleStatus
		to          LifecycleStatus
		metadata    *Metadata
		wantErr     bool
		errContains string
	}{
		// Valid transitions without metadata
		{name: "draft_to_proposed_valid", from: StatusDraft, to: StatusProposed},
		{name: "proposed_to_reviewed_valid", from: StatusProposed, to: StatusReviewed},
		{name: "reviewed_to_attested_valid", from: StatusReviewed, to: StatusAttested},
		{name: "attested_to_active_valid", from: StatusAttested, to: StatusActive},
		{name: "active_to_deprecated_valid", from: StatusActive, to: StatusDeprecated},
		{name: "proposed_to_draft_valid", from: StatusProposed, to: StatusDraft},

		// Invalid transitions
		{
			name:        "draft_to_active_invalid",
			from:        StatusDraft,
			to:          StatusActive,
			wantErr:     true,
			errContains: "invalid transition",
		},
		{
			name:        "active_to_proposed_invalid",
			from:        StatusActive,
			to:          StatusProposed,
			wantErr:     true,
			errContains: "invalid transition",
		},

		// Rollback: deprecated → active requires WorkItem
		{
			name:        "rollback_without_workitem_fails",
			from:        StatusDeprecated,
			to:          StatusActive,
			wantErr:     true,
			errContains: "requires human approval",
		},
		{
			name:     "rollback_with_workitem_succeeds",
			from:     StatusDeprecated,
			to:       StatusActive,
			metadata: &Metadata{WorkItem: "WI-042"},
		},
		{
			name:        "rollback_with_nil_metadata_fails",
			from:        StatusDeprecated,
			to:          StatusActive,
			metadata:    nil,
			wantErr:     true,
			errContains: "requires human approval",
		},
		{
			name:        "rollback_with_empty_workitem_fails",
			from:        StatusDeprecated,
			to:          StatusActive,
			metadata:    &Metadata{WorkItem: ""},
			wantErr:     true,
			errContains: "requires human approval",
		},

		// Non-rollback with metadata doesn't require it
		{
			name:     "active_to_deprecated_with_workitem_still_valid",
			from:     StatusActive,
			to:       StatusDeprecated,
			metadata: &Metadata{WorkItem: "WI-043"},
		},

		// retired → anything
		{
			name:        "retired_to_active_invalid",
			from:        StatusRetired,
			to:          StatusActive,
			wantErr:     true,
			errContains: "invalid transition",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTransition(tt.from, tt.to, tt.metadata)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %q, want containing %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
