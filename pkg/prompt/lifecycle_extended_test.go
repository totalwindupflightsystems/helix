package prompt

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// TestApplyTransition
// ---------------------------------------------------------------------------

func TestApplyTransition_ValidForward(t *testing.T) {
	tests := []struct {
		name string
		from LifecycleStatus
		to   LifecycleStatus
	}{
		{"draft_to_proposed", StatusDraft, StatusProposed},
		{"proposed_to_reviewed", StatusProposed, StatusReviewed},
		{"reviewed_to_attested", StatusReviewed, StatusAttested},
		{"attested_to_active", StatusAttested, StatusActive},
		{"active_to_deprecated", StatusActive, StatusDeprecated},
		{"proposed_to_draft", StatusProposed, StatusDraft},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Metadata{Status: tt.from}
			err := ApplyTransition(m, tt.to)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if m.Status != tt.to {
				t.Errorf("status = %s, want %s", m.Status, tt.to)
			}
			if !strings.Contains(m.Changes, string(tt.from)) {
				t.Errorf("changes should contain old status %s, got: %s", tt.from, m.Changes)
			}
			if !strings.Contains(m.Changes, string(tt.to)) {
				t.Errorf("changes should contain new status %s, got: %s", tt.to, m.Changes)
			}
		})
	}
}

func TestApplyTransition_InvalidTransition(t *testing.T) {
	m := &Metadata{Status: StatusDraft}
	err := ApplyTransition(m, StatusActive)
	if err == nil {
		t.Fatal("expected error for draft → active")
	}
	if !strings.Contains(err.Error(), "invalid transition") {
		t.Errorf("error should mention invalid transition, got: %v", err)
	}
	if m.Status != StatusDraft {
		t.Errorf("status should remain draft after failed transition, got %s", m.Status)
	}
}

func TestApplyTransition_SetsAttestedTimestamp(t *testing.T) {
	m := &Metadata{Status: StatusReviewed}
	err := ApplyTransition(m, StatusAttested)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.AttestedAt.IsZero() {
		t.Error("expected AttestedAt to be set")
	}
}

func TestApplyTransition_DoesNotOverwriteAttestedTimestamp(t *testing.T) {
	original := time.Now().Add(-24 * time.Hour)
	m := &Metadata{Status: StatusReviewed, AttestedAt: original}
	err := ApplyTransition(m, StatusAttested)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.AttestedAt.Equal(original) {
		t.Error("expected AttestedAt to be preserved, not overwritten")
	}
}

func TestApplyTransition_RollbackRequiresWorkItem(t *testing.T) {
	m := &Metadata{Status: StatusDeprecated}
	err := ApplyTransition(m, StatusActive)
	if err == nil {
		t.Fatal("expected error for rollback without WorkItem")
	}
}

func TestApplyTransition_RollbackWithWorkItem(t *testing.T) {
	m := &Metadata{Status: StatusDeprecated, WorkItem: "WI-100"}
	err := ApplyTransition(m, StatusActive)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Status != StatusActive {
		t.Errorf("expected status active, got %s", m.Status)
	}
}

func TestApplyTransition_NilMetadata(t *testing.T) {
	err := ApplyTransition(nil, StatusActive)
	if err == nil {
		t.Fatal("expected error for nil metadata")
	}
}

func TestApplyTransition_ChangesContainsTimestamp(t *testing.T) {
	m := &Metadata{Status: StatusActive}
	err := ApplyTransition(m, StatusDeprecated)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The Changes field should contain a parseable RFC3339 timestamp.
	ts := extractTransitionTime(m.Changes)
	if ts.IsZero() {
		t.Errorf("expected Changes to contain parseable timestamp, got: %s", m.Changes)
	}
}

// ---------------------------------------------------------------------------
// TestDefaultAutoDeprecationConfig
// ---------------------------------------------------------------------------

func TestDefaultAutoDeprecationConfig(t *testing.T) {
	c := DefaultAutoDeprecationConfig()
	if c.DeprecateAfterDays != 90 {
		t.Errorf("DeprecateAfterDays = %d, want 90", c.DeprecateAfterDays)
	}
	if c.RetireAfterDeprecatedDays != 90 {
		t.Errorf("RetireAfterDeprecatedDays = %d, want 90", c.RetireAfterDeprecatedDays)
	}
	if c.RetireAfterNoCommitsDays != 180 {
		t.Errorf("RetireAfterNoCommitsDays = %d, want 180", c.RetireAfterNoCommitsDays)
	}
	if c.AutoDeprecateOnNewerCommits != 3 {
		t.Errorf("AutoDeprecateOnNewerCommits = %d, want 3", c.AutoDeprecateOnNewerCommits)
	}
}

// ---------------------------------------------------------------------------
// TestShouldDeprecate
// ---------------------------------------------------------------------------

func TestShouldDeprecate_InactivityTrigger(t *testing.T) {
	config := DefaultAutoDeprecationConfig()
	now := time.Now().UTC()
	// Created 100 days ago → exceeds 90-day deprecation threshold.
	m := &Metadata{
		Status:    StatusActive,
		CreatedAt: now.Add(-100 * 24 * time.Hour),
	}
	if !ShouldDeprecate(m, now, config, 0) {
		t.Error("expected ShouldDeprecate to return true for 100-day-old active prompt")
	}
}

func TestShouldDeprecate_RecentActivityNoDeprecation(t *testing.T) {
	config := DefaultAutoDeprecationConfig()
	now := time.Now().UTC()
	// Created 10 days ago, promptfoo ran 5 days ago.
	m := &Metadata{
		Status:    StatusActive,
		CreatedAt: now.Add(-10 * 24 * time.Hour),
		Promptfoo: PromptfooResult{LastRun: now.Add(-5 * 24 * time.Hour)},
	}
	if ShouldDeprecate(m, now, config, 0) {
		t.Error("expected ShouldDeprecate to return false for recent activity")
	}
}

func TestShouldDeprecate_NewerVersionWithCommits(t *testing.T) {
	config := DefaultAutoDeprecationConfig()
	now := time.Now().UTC()
	m := &Metadata{
		Status:    StatusActive,
		CreatedAt: now.Add(-1 * 24 * time.Hour), // very recent
	}
	// Newer version has 3+ commits.
	if !ShouldDeprecate(m, now, config, 3) {
		t.Error("expected ShouldDeprecate when newer version has 3+ commits")
	}
}

func TestShouldDeprecate_NewerVersionWithFewCommits(t *testing.T) {
	config := DefaultAutoDeprecationConfig()
	now := time.Now().UTC()
	m := &Metadata{
		Status:    StatusActive,
		CreatedAt: now.Add(-1 * 24 * time.Hour),
	}
	// Only 2 commits on newer version — below threshold.
	if ShouldDeprecate(m, now, config, 2) {
		t.Error("expected ShouldDeprecate to be false when newer version has <3 commits")
	}
}

func TestShouldDeprecate_NotActive(t *testing.T) {
	config := DefaultAutoDeprecationConfig()
	now := time.Now().UTC()
	m := &Metadata{Status: StatusDraft}
	if ShouldDeprecate(m, now, config, 5) {
		t.Error("expected ShouldDeprecate to return false for non-active status")
	}
}

func TestShouldDeprecate_DisabledDeprecationAfterDays(t *testing.T) {
	config := AutoDeprecationConfig{
		DeprecateAfterDays:          0, // disabled
		AutoDeprecateOnNewerCommits: 0,
	}
	now := time.Now().UTC()
	m := &Metadata{
		Status:    StatusActive,
		CreatedAt: now.Add(-365 * 24 * time.Hour),
	}
	if ShouldDeprecate(m, now, config, 0) {
		t.Error("expected ShouldDeprecate to return false when deprecation is disabled")
	}
}

// ---------------------------------------------------------------------------
// TestShouldRetire
// ---------------------------------------------------------------------------

func TestShouldRetire_TimeInDeprecatedTrigger(t *testing.T) {
	config := DefaultAutoDeprecationConfig()
	now := time.Now().UTC()
	// Deprecated 100 days ago → exceeds 90-day retirement threshold.
	deprecationTime := now.Add(-100 * 24 * time.Hour)
	m := &Metadata{
		Status:  StatusDeprecated,
		Changes: "active → deprecated (transition at " + deprecationTime.Format(time.RFC3339) + ")",
	}
	if !ShouldRetire(m, now, config) {
		t.Error("expected ShouldRetire for 100-day-old deprecated prompt")
	}
}

func TestShouldRetire_RecentlyDeprecatedNoRetire(t *testing.T) {
	config := DefaultAutoDeprecationConfig()
	now := time.Now().UTC()
	deprecationTime := now.Add(-10 * 24 * time.Hour)
	m := &Metadata{
		Status:  StatusDeprecated,
		Changes: "active → deprecated (transition at " + deprecationTime.Format(time.RFC3339) + ")",
		Promptfoo: PromptfooResult{LastRun: now.Add(-5 * 24 * time.Hour)},
	}
	if ShouldRetire(m, now, config) {
		t.Error("expected ShouldRetire to return false for recently deprecated prompt")
	}
}

func TestShouldRetire_NoCommitsTrigger(t *testing.T) {
	config := DefaultAutoDeprecationConfig()
	now := time.Now().UTC()
	// Deprecated 10 days ago (below 90-day threshold).
	deprecationTime := now.Add(-10 * 24 * time.Hour)
	// But no promptfoo activity for 200 days (exceeds 180-day threshold).
	m := &Metadata{
		Status:  StatusDeprecated,
		Changes: "active → deprecated (transition at " + deprecationTime.Format(time.RFC3339) + ")",
		Promptfoo: PromptfooResult{LastRun: now.Add(-200 * 24 * time.Hour)},
	}
	if !ShouldRetire(m, now, config) {
		t.Error("expected ShouldRetire for prompt with no commits in 180+ days")
	}
}

func TestShouldRetire_NotDeprecated(t *testing.T) {
	config := DefaultAutoDeprecationConfig()
	now := time.Now().UTC()
	m := &Metadata{Status: StatusActive}
	if ShouldRetire(m, now, config) {
		t.Error("expected ShouldRetire to return false for non-deprecated status")
	}
}

// ---------------------------------------------------------------------------
// TestExtractTransitionTime
// ---------------------------------------------------------------------------

func TestExtractTransitionTime_ValidFormat(t *testing.T) {
	ts := time.Now().UTC()
	changes := "active → deprecated (transition at " + ts.Format(time.RFC3339) + ")"
	parsed := extractTransitionTime(changes)
	if parsed.IsZero() {
		t.Error("expected non-zero time from valid changes string")
	}
}

func TestExtractTransitionTime_EmptyString(t *testing.T) {
	parsed := extractTransitionTime("")
	if !parsed.IsZero() {
		t.Error("expected zero time for empty string")
	}
}

func TestExtractTransitionTime_NoMarker(t *testing.T) {
	parsed := extractTransitionTime("some random changes without marker")
	if !parsed.IsZero() {
		t.Error("expected zero time for string without marker")
	}
}

func TestExtractTransitionTime_InvalidTimestamp(t *testing.T) {
	parsed := extractTransitionTime("(transition at not-a-timestamp)")
	if !parsed.IsZero() {
		t.Error("expected zero time for invalid timestamp")
	}
}

// ---------------------------------------------------------------------------
// TestIndexOf helper
// ---------------------------------------------------------------------------

func TestIndexOf(t *testing.T) {
	tests := []struct {
		s, sub string
		want   int
	}{
		{"hello world", "world", 6},
		{"hello world", "hello", 0},
		{"hello world", "xyz", -1},
		{"abc", "", 0},
		{"", "a", -1},
		{"aaa", "aa", 0},
	}
	for _, tt := range tests {
		got := indexOf(tt.s, tt.sub)
		if got != tt.want {
			t.Errorf("indexOf(%q, %q) = %d, want %d", tt.s, tt.sub, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// TestLastActivityTime
// ---------------------------------------------------------------------------

func TestLastActivityTime(t *testing.T) {
	now := time.Now().UTC()
	m := &Metadata{
		CreatedAt:  now.Add(-100 * 24 * time.Hour),
		AttestedAt: now.Add(-50 * 24 * time.Hour),
		Promptfoo:  PromptfooResult{LastRun: now.Add(-10 * 24 * time.Hour)},
	}
	lat := lastActivityTime(m)
	expected := now.Add(-10 * 24 * time.Hour)
	if !lat.Equal(expected) {
		t.Errorf("lastActivityTime = %v, want %v", lat, expected)
	}
}

func TestLastActivityTime_NoPromptfoo(t *testing.T) {
	now := time.Now().UTC()
	m := &Metadata{
		CreatedAt:  now.Add(-100 * 24 * time.Hour),
		AttestedAt: now.Add(-50 * 24 * time.Hour),
	}
	lat := lastActivityTime(m)
	expected := now.Add(-50 * 24 * time.Hour)
	if !lat.Equal(expected) {
		t.Errorf("lastActivityTime = %v, want %v", lat, expected)
	}
}

func TestLastActivityTime_OnlyCreatedAt(t *testing.T) {
	now := time.Now().UTC()
	m := &Metadata{
		CreatedAt: now.Add(-30 * 24 * time.Hour),
	}
	lat := lastActivityTime(m)
	expected := now.Add(-30 * 24 * time.Hour)
	if !lat.Equal(expected) {
		t.Errorf("lastActivityTime = %v, want %v", lat, expected)
	}
}
