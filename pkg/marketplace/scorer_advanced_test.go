package marketplace

import (
	"strings"
	"testing"
	"time"
)

// =============================================================================
// NewScorer
// =============================================================================

func TestNewScorer(t *testing.T) {
	s := NewScorer()
	if s == nil {
		t.Fatal("NewScorer returned nil")
	}
	if s.mergeHistory == nil {
		t.Error("mergeHistory not initialized")
	}
	if s.reviewCount == nil {
		t.Error("reviewCount not initialized")
	}
	if s.lastActivity == nil {
		t.Error("lastActivity not initialized")
	}
	if len(s.mergeHistory) != 0 {
		t.Error("mergeHistory should be empty")
	}
	if len(s.reviewCount) != 0 {
		t.Error("reviewCount should be empty")
	}
	if len(s.lastActivity) != 0 {
		t.Error("lastActivity should be empty")
	}
}

// =============================================================================
// CalculateReputation
// =============================================================================

func TestScorer_CalculateReputation(t *testing.T) {
	t.Run("agent with no history — base score 30", func(t *testing.T) {
		s := NewScorer()
		score, err := s.CalculateReputation("unknown-agent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if score != 30.0 {
			t.Errorf("score = %v, want 30.0", score)
		}
	})

	t.Run("agent with merge history — boosts score", func(t *testing.T) {
		s := NewScorer()
		s.mergeHistory["alice"] = &MergeStats{
			MergedPRs: 10, // +20 (capped at 40)
		}
		score, err := s.CalculateReputation("alice")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// base 30 + min(40, 10*2) = 30 + 20 = 50
		if score != 50.0 {
			t.Errorf("score = %v, want 50.0", score)
		}
	})

	t.Run("agent with penalties — reduces score", func(t *testing.T) {
		s := NewScorer()
		s.mergeHistory["bob"] = &MergeStats{
			RejectedPRs:    3, // -9 (capped at 20)
			Incidents:      2, // -20
			ForceMerges:    1, // -5
			BudgetOverruns: 0,
		}
		score, err := s.CalculateReputation("bob")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// base 30 - 9 - min(30, 20+5+0) = 30 - 9 - 25 = -4 → clamped to 0
		if score != 0.0 {
			t.Errorf("score = %v, want 0.0", score)
		}
	})

	t.Run("agent capped acceptance bonus", func(t *testing.T) {
		s := NewScorer()
		s.mergeHistory["charlie"] = &MergeStats{
			MergedPRs: 50, // 50*2 = 100, capped at 40
		}
		score, err := s.CalculateReputation("charlie")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// base 30 + 40(capped) = 70
		if score != 70.0 {
			t.Errorf("score = %v, want 70.0", score)
		}
	})

	t.Run("agent with activity — no decay applies", func(t *testing.T) {
		s := NewScorer()
		s.mergeHistory["dave"] = &MergeStats{MergedPRs: 5} // +10
		s.lastActivity["dave"] = time.Now().UTC().Format(time.RFC3339)
		score, err := s.CalculateReputation("dave")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// base 30 + min(40, 10) = 40, no decay (active now)
		if score != 40.0 {
			t.Errorf("score = %v, want 40.0", score)
		}
	})

	t.Run("inactive agent — decay applies", func(t *testing.T) {
		s := NewScorer()
		s.mergeHistory["eve"] = &MergeStats{MergedPRs: 20} // +40
		// Last activity: 100 days ago (3 full 30-day periods beyond 30-day grace)
		// periods = (100 - 30) / 30 = 2 → 2 decay cycles
		s.lastActivity["eve"] = time.Now().UTC().Add(-100 * 24 * time.Hour).Format(time.RFC3339)
		score, err := s.CalculateReputation("eve")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// base 30 + 40 = 70, then decay: 70 * 0.95 * 0.95 = 63.175
		expected := 70.0 * 0.95 * 0.95
		if score != expected {
			t.Errorf("score = %v, want %v", score, expected)
		}
	})

	t.Run("inactive agent — decay floors at base 30", func(t *testing.T) {
		s := NewScorer()
		s.mergeHistory["fay"] = &MergeStats{} // no bonus
		// Last activity: 200 days ago → many decay cycles, but floor at 30
		s.lastActivity["fay"] = time.Now().UTC().Add(-200 * 24 * time.Hour).Format(time.RFC3339)
		score, err := s.CalculateReputation("fay")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// base 30, decay can't go below 30
		if score != 30.0 {
			t.Errorf("score = %v, want 30.0 (floor)", score)
		}
	})
}

// =============================================================================
// RecordReview
// =============================================================================

func TestScorer_RecordReview(t *testing.T) {
	t.Run("valid rating increments review count", func(t *testing.T) {
		s := NewScorer()
		err := s.RecordReview("alice", Review{Rating: 4})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.reviewCount["alice"] != 1 {
			t.Errorf("reviewCount = %d, want 1", s.reviewCount["alice"])
		}
		if s.lastActivity["alice"] == "" {
			t.Error("lastActivity should be set")
		}
	})

	t.Run("multiple reviews accumulate", func(t *testing.T) {
		s := NewScorer()
		_ = s.RecordReview("bob", Review{Rating: 5})
		_ = s.RecordReview("bob", Review{Rating: 3})
		if s.reviewCount["bob"] != 2 {
			t.Errorf("reviewCount = %d, want 2", s.reviewCount["bob"])
		}
	})

	t.Run("rating below 1 returns error", func(t *testing.T) {
		s := NewScorer()
		err := s.RecordReview("charlie", Review{Rating: 0})
		if err == nil {
			t.Fatal("expected error for rating 0")
		}
		exitErr, ok := err.(*ExitError)
		if !ok {
			t.Fatalf("expected ExitError, got %T", err)
		}
		if exitErr.Code != ExitInvalidRating {
			t.Errorf("code = %d, want %d", exitErr.Code, ExitInvalidRating)
		}
		if !strings.Contains(exitErr.Message, "INVALID_RATING") {
			t.Errorf("message should contain INVALID_RATING: %v", exitErr.Message)
		}
		if s.reviewCount["charlie"] != 0 {
			t.Error("reviewCount should not increment on error")
		}
	})

	t.Run("rating above 5 returns error", func(t *testing.T) {
		s := NewScorer()
		err := s.RecordReview("dave", Review{Rating: 6})
		if err == nil {
			t.Fatal("expected error for rating 6")
		}
		exitErr := err.(*ExitError)
		if exitErr.Code != ExitInvalidRating {
			t.Errorf("code = %d, want %d", exitErr.Code, ExitInvalidRating)
		}
	})

	t.Run("rating 1 is valid", func(t *testing.T) {
		s := NewScorer()
		err := s.RecordReview("eve", Review{Rating: 1})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("rating 5 is valid", func(t *testing.T) {
		s := NewScorer()
		err := s.RecordReview("fay", Review{Rating: 5})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("negative rating returns error", func(t *testing.T) {
		s := NewScorer()
		err := s.RecordReview("grace", Review{Rating: -1})
		if err == nil {
			t.Fatal("expected error for rating -1")
		}
	})
}

// =============================================================================
// RecordMerge
// =============================================================================

func TestScorer_RecordMerge(t *testing.T) {
	t.Run("successful merge increments MergedPRs", func(t *testing.T) {
		s := NewScorer()
		err := s.RecordMerge("alice", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		stats := s.mergeHistory["alice"]
		if stats == nil {
			t.Fatal("mergeHistory should have entry")
		}
		if stats.MergedPRs != 1 {
			t.Errorf("MergedPRs = %d, want 1", stats.MergedPRs)
		}
		if stats.RejectedPRs != 0 {
			t.Errorf("RejectedPRs = %d, want 0", stats.RejectedPRs)
		}
		if s.lastActivity["alice"] == "" {
			t.Error("lastActivity should be set")
		}
	})

	t.Run("failed merge increments RejectedPRs", func(t *testing.T) {
		s := NewScorer()
		err := s.RecordMerge("bob", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		stats := s.mergeHistory["bob"]
		if stats.RejectedPRs != 1 {
			t.Errorf("RejectedPRs = %d, want 1", stats.RejectedPRs)
		}
		if stats.MergedPRs != 0 {
			t.Errorf("MergedPRs = %d, want 0", stats.MergedPRs)
		}
	})

	t.Run("first merge creates stats entry", func(t *testing.T) {
		s := NewScorer()
		if err := s.RecordMerge("charlie", true); err != nil {
			t.Fatalf("RecordMerge: %v", err)
		}
		stats, ok := s.mergeHistory["charlie"]
		if !ok || stats == nil {
			t.Fatal("mergeHistory should create entry on first merge")
		}
	})

	t.Run("multiple merges accumulate correctly", func(t *testing.T) {
		s := NewScorer()
		_ = s.RecordMerge("dave", true)
		_ = s.RecordMerge("dave", true)
		_ = s.RecordMerge("dave", false)
		stats := s.mergeHistory["dave"]
		if stats.MergedPRs != 2 {
			t.Errorf("MergedPRs = %d, want 2", stats.MergedPRs)
		}
		if stats.RejectedPRs != 1 {
			t.Errorf("RejectedPRs = %d, want 1", stats.RejectedPRs)
		}
	})
}

// =============================================================================
// applyDecay
// =============================================================================

func TestScorer_ApplyDecay(t *testing.T) {
	t.Run("no activity recorded — returns score unchanged", func(t *testing.T) {
		s := NewScorer()
		decayed := s.applyDecay("unknown", 50.0)
		if decayed != 50.0 {
			t.Errorf("decayed = %v, want 50.0", decayed)
		}
	})

	t.Run("unparseable timestamp — returns score unchanged", func(t *testing.T) {
		s := NewScorer()
		s.lastActivity["alice"] = "not-a-timestamp"
		decayed := s.applyDecay("alice", 50.0)
		if decayed != 50.0 {
			t.Errorf("decayed = %v, want 50.0", decayed)
		}
	})

	t.Run("recent activity — within grace period, no decay", func(t *testing.T) {
		s := NewScorer()
		s.lastActivity["bob"] = time.Now().UTC().Add(-10 * 24 * time.Hour).Format(time.RFC3339)
		decayed := s.applyDecay("bob", 70.0)
		if decayed != 70.0 {
			t.Errorf("decayed = %v, want 70.0 (within grace)", decayed)
		}
	})

	t.Run("exactly at grace boundary — no decay", func(t *testing.T) {
		s := NewScorer()
		s.lastActivity["charlie"] = time.Now().UTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339)
		decayed := s.applyDecay("charlie", 70.0)
		if decayed != 70.0 {
			t.Errorf("decayed = %v, want 70.0 (at grace boundary)", decayed)
		}
	})

	t.Run("one period beyond grace — 5% decay", func(t *testing.T) {
		s := NewScorer()
		// Grace = 30 days, decay window = 30 days.
		// Need > 60 days to get 1 full period: (61 - 30) / 30 = 1
		s.lastActivity["dave"] = time.Now().UTC().Add(-61 * 24 * time.Hour).Format(time.RFC3339)
		decayed := s.applyDecay("dave", 100.0)
		expected := 100.0 * 0.95
		if decayed != expected {
			t.Errorf("decayed = %v, want %v", decayed, expected)
		}
	})

	t.Run("two periods beyond grace — 5% twice", func(t *testing.T) {
		s := NewScorer()
		s.lastActivity["eve"] = time.Now().UTC().Add(-95 * 24 * time.Hour).Format(time.RFC3339)
		decayed := s.applyDecay("eve", 100.0)
		// periods = (95 - 30) / 30 = 2 → 100 * 0.95^2 = 90.25
		expected := 100.0 * 0.95 * 0.95
		if decayed != expected {
			t.Errorf("decayed = %v, want %v", decayed, expected)
		}
	})

	t.Run("decay floors at base score 30", func(t *testing.T) {
		s := NewScorer()
		s.lastActivity["fay"] = time.Now().UTC().Add(-1000 * 24 * time.Hour).Format(time.RFC3339)
		// Start at 35, apply max decay — should floor at 30
		decayed := s.applyDecay("fay", 35.0)
		if decayed != 30.0 {
			t.Errorf("decayed = %v, want 30.0 (floor)", decayed)
		}
	})

	t.Run("decay with high initial score — eventually floors", func(t *testing.T) {
		s := NewScorer()
		s.lastActivity["grace"] = time.Now().UTC().Add(-500 * 24 * time.Hour).Format(time.RFC3339)
		// periods = (500 - 30) / 30 = 15 → 100 * 0.95^15 ≈ 46.33
		decayed := s.applyDecay("grace", 100.0)
		expected := 100.0
		for i := 0; i < 15; i++ {
			expected *= 0.95
		}
		if decayed != expected {
			t.Errorf("decayed = %v, want %v", decayed, expected)
		}
	})

	t.Run("day 61 in UTC should trigger single decay", func(t *testing.T) {
		s := NewScorer()
		// Day 61: one full 30-day period beyond 30-day grace
		s.lastActivity["hank"] = time.Now().UTC().Add(-(61 * 24) * time.Hour).Format(time.RFC3339)
		decayed := s.applyDecay("hank", 80.0)
		expected := 80.0 * 0.95
		if decayed != expected {
			t.Errorf("decayed = %v, want %v", decayed, expected)
		}
	})
}

// =============================================================================
// nowISO — ensures it works (used by RecordReview and RecordMerge)
// =============================================================================

func TestNowISO(t *testing.T) {
	ts := nowISO()
	if ts == "" {
		t.Error("nowISO returned empty string")
	}
	// Should be parseable as RFC3339
	_, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t.Errorf("nowISO returned unparseable timestamp: %v", err)
	}
}
