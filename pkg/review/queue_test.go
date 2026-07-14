package review

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

func TestReviewQueue_AddAndGet(t *testing.T) {
	q := NewReviewQueue()
	item := &ReviewQueueItem{
		ID:        "pr-1",
		PRURL:     "https://example.com/pr/1",
		Category:  CategoryBehavioral,
		RiskScore: 50,
		SubmittedAt: time.Now(),
		Status:    ReviewStatusPending,
	}

	if err := q.Add(item); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, ok := q.Get("pr-1")
	if !ok {
		t.Fatal("Get returned false")
	}
	if got.ID != "pr-1" {
		t.Errorf("ID = %q, want %q", got.ID, "pr-1")
	}
	if got.Status != ReviewStatusPending {
		t.Errorf("Status = %q, want %q", got.Status, ReviewStatusPending)
	}
}

func TestReviewQueue_ListPendingSorted(t *testing.T) {
	q := NewReviewQueue()
	now := time.Now()

	// Older, lower risk -> lower priority
	q.Add(&ReviewQueueItem{
		ID: "old-low-risk", RiskScore: 10,
		SubmittedAt: now.Add(-10 * time.Hour), Status: ReviewStatusPending,
	})
	// Newer, higher risk -> higher priority
	q.Add(&ReviewQueueItem{
		ID: "new-high-risk", RiskScore: 90,
		SubmittedAt: now.Add(-1 * time.Hour), Status: ReviewStatusPending,
	})
	// Old, high risk -> should be top
	q.Add(&ReviewQueueItem{
		ID: "old-high-risk", RiskScore: 80,
		SubmittedAt: now.Add(-8 * time.Hour), Status: ReviewStatusPending,
	})

	items := q.ListPendingSorted()
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}

	// "old-high-risk": 80 * 8 = 640 (highest)
	// "new-high-risk": 90 * 1 = 90
	// "old-low-risk":  10 * 10 = 100 (higher than new-high-risk since 10h)
	if items[0].ID != "old-high-risk" && items[0].ID != "old-low-risk" {
		t.Errorf("unexpected first item: %s (priority=%.0f)", items[0].ID, items[0].PriorityScore)
	}
}

func TestReviewQueue_PersistAndLoad(t *testing.T) {
	q := NewReviewQueue()
	q.Add(&ReviewQueueItem{
		ID: "pr-1", PRURL: "https://example.com/pr/1",
		Category: CategoryContract, RiskScore: 50,
		SubmittedAt: time.Now(), Status: ReviewStatusPending,
		Tier: trust.TierProvisional,
	})

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "reviews.json")

	if err := q.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	q2 := NewReviewQueue()
	if err := q2.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	got, ok := q2.Get("pr-1")
	if !ok {
		t.Fatal("item not found after load")
	}
	if got.PRURL != "https://example.com/pr/1" {
		t.Errorf("PRURL = %q", got.PRURL)
	}
	if got.Tier != trust.TierProvisional {
		t.Errorf("Tier = %q", got.Tier)
	}
}

func TestReviewQueue_LoadNonExistent(t *testing.T) {
	q := NewReviewQueue()
	err := q.Load("/nonexistent/path/reviews.json")
	if err != nil {
		t.Errorf("Load on non-existent file should not error: %v", err)
	}
}

func TestReviewAssigner_SelfReviewPrevention(t *testing.T) {
	ra := NewReviewAssigner()
	item := ReviewQueueItem{
		ID: "pr-1", AuthorAgentID: "model-a",
		Category: CategoryBehavioral, RiskScore: 50,
		SubmittedAt: time.Now(), Tier: trust.TierProvisional,
	}

	pool := []ModelPoolEntry{
		{Model: ModelInfo{Model: "model-a", Provider: "openai"}, Provider: "openai", RLHF: "helpful"},
		{Model: ModelInfo{Model: "model-b", Provider: "deepseek"}, Provider: "deepseek", RLHF: "constitutional"},
	}

	result, err := ra.AssignReviewers(item, pool)
	if err != nil {
		t.Fatalf("AssignReviewers: %v", err)
	}

	// model-a should be excluded (self-review)
	for _, m := range result.AssignedModels {
		if m == "model-a" {
			t.Error("model-a should have been excluded (self-review)")
		}
	}
	// model-b should be assigned
	found := false
	for _, m := range result.AssignedModels {
		if m == "model-b" {
			found = true
			break
		}
	}
	if !found {
		t.Error("model-b should have been assigned")
	}
}

func TestReviewAssigner_TrustTierRouting(t *testing.T) {
	ra := NewReviewAssigner()

	pool := []ModelPoolEntry{
		{Model: ModelInfo{Model: "model-a", Provider: "openai"}, Provider: "openai", RLHF: "helpful"},
		{Model: ModelInfo{Model: "model-b", Provider: "deepseek"}, Provider: "deepseek", RLHF: "constitutional"},
		{Model: ModelInfo{Model: "model-c", Provider: "anthropic"}, Provider: "anthropic", RLHF: "dpo"},
	}

	// Provisional -> full adversarial (3 models if pool allows)
	item := ReviewQueueItem{
		ID: "pr-1", AuthorAgentID: "agent-x",
		Category: CategoryContract, RiskScore: 70,
		SubmittedAt: time.Now(), Tier: trust.TierProvisional,
	}
	result, _ := ra.AssignReviewers(item, pool)
	if result.PanelSize > 3 || result.PanelSize < 2 {
		t.Errorf("provisional contract panel size = %d, expect >=2", result.PanelSize)
	}

	// Veteran -> light review
	item2 := ReviewQueueItem{
		ID: "pr-2", AuthorAgentID: "agent-x",
		Category: CategoryCosmetic, RiskScore: 5,
		SubmittedAt: time.Now(), Tier: trust.TierVeteran,
	}
	result2, _ := ra.AssignReviewers(item2, pool)
	if result2.PanelSize > 1 {
		t.Errorf("veteran cosmetic panel size = %d, expect 1", result2.PanelSize)
	}
}

func TestReviewAssigner_HumanNeeded(t *testing.T) {
	ra := NewReviewAssigner()

	// Contract changes always need human
	item := ReviewQueueItem{
		ID: "pr-1", AuthorAgentID: "agent-x",
		Category: CategoryContract, RiskScore: 80,
		SubmittedAt: time.Now(), Tier: trust.TierTrusted,
	}
	result, _ := ra.AssignReviewers(item, []ModelPoolEntry{
		{Model: ModelInfo{Model: "model-a", Provider: "openai"}, Provider: "openai", RLHF: "helpful"},
	})
	if !result.HumanNeeded {
		t.Error("contract changes should require human review")
	}

	// Cosmetic + veteran -> no human needed
	item2 := ReviewQueueItem{
		ID: "pr-2", AuthorAgentID: "agent-x",
		Category: CategoryCosmetic, RiskScore: 5,
		SubmittedAt: time.Now(), Tier: trust.TierVeteran,
		GatesPassed: true,
	}
	result2, _ := ra.AssignReviewers(item2, []ModelPoolEntry{})
	if result2.HumanNeeded {
		t.Error("cosmetic veteran changes should not require human review")
	}
	if !result2.AutoMerge {
		t.Error("cosmetic veteran changes should be auto-mergeable")
	}
}

func TestHumanReviewFilter_MustSeeHuman(t *testing.T) {
	f := NewHumanReviewFilter()

	tests := []struct {
		name     string
		item     ReviewQueueItem
		mustSee  bool
	}{
		{"contract change", ReviewQueueItem{Category: CategoryContract, Tier: trust.TierVeteran}, true},
		{"provisional tier", ReviewQueueItem{Category: CategoryCosmetic, Tier: trust.TierProvisional}, true},
		{"veteran cosmetic", ReviewQueueItem{Category: CategoryCosmetic, Tier: trust.TierVeteran}, false},
		{"trusted behavioral", ReviewQueueItem{Category: CategoryBehavioral, Tier: trust.TierTrusted}, false},
		{"observed behavioral", ReviewQueueItem{Category: CategoryBehavioral, Tier: trust.TierObserved}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := f.MustSeeHuman(tc.item); got != tc.mustSee {
				t.Errorf("MustSeeHuman = %v, want %v", got, tc.mustSee)
			}
		})
	}
}

func TestHumanReviewFilter_CanAutoMerge(t *testing.T) {
	f := NewHumanReviewFilter()

	// Auto-mergeable: cosmetic + veteran + gates passed
	item := ReviewQueueItem{
		Category:    CategoryCosmetic,
		Tier:        trust.TierVeteran,
		GatesPassed: true,
	}
	if !f.CanAutoMerge(item) {
		t.Error("cosmetic+veteran+gates should be auto-mergeable")
	}

	// Not auto-mergeable: cosmetic + veteran but gates failed
	item2 := ReviewQueueItem{
		Category:    CategoryCosmetic,
		Tier:        trust.TierVeteran,
		GatesPassed: false,
	}
	if f.CanAutoMerge(item2) {
		t.Error("should not auto-merge when gates failed")
	}

	// Not auto-mergeable: contract change
	item3 := ReviewQueueItem{
		Category:    CategoryContract,
		Tier:        trust.TierVeteran,
		GatesPassed: true,
	}
	if f.CanAutoMerge(item3) {
		t.Error("contract changes should never auto-merge")
	}
}

func TestSLATracker_SLABreached(t *testing.T) {
	s := NewSLATracker()

	// Contract has 4h SLA, assign 5h ago -> breached
	s.RecordAssignment("pr-1", "human", time.Now().Add(-5*time.Hour))
	if !s.IsBreached("pr-1", CategoryContract) {
		t.Error("contract SLA should be breached after 5h (limit 4h)")
	}

	// Cosmetic has 72h SLA, assign 1h ago -> not breached
	s.RecordAssignment("pr-2", "human", time.Now().Add(-1*time.Hour))
	if s.IsBreached("pr-2", CategoryCosmetic) {
		t.Error("cosmetic SLA should NOT be breached after 1h (limit 72h)")
	}

	// Resolution before deadline -> not breached
	s.RecordAssignment("pr-3", "human", time.Now().Add(-2*time.Hour))
	s.RecordResolution("pr-3", time.Now().Add(-1*time.Hour))
	if s.IsBreached("pr-3", CategoryContract) {
		t.Error("resolved before deadline should not be breached")
	}
}

func TestSLATracker_SLAMet(t *testing.T) {
	s := NewSLATracker()

	now := time.Now()
	s.RecordAssignment("pr-1", "human", now.Add(-1*time.Hour))
	s.RecordFirstReview("pr-1", now.Add(-30*time.Minute))
	s.RecordResolution("pr-1", now)

	ttr := s.TimeToResolution("pr-1")
	if ttr < 0 {
		t.Error("time to resolution should be >= 0")
	}

	tfr := s.TimeToFirstReview("pr-1")
	if tfr < 0 {
		t.Error("time to first review should be >= 0")
	}
}

func TestSLATracker_Escalate(t *testing.T) {
	s := NewSLATracker()
	s.RecordAssignment("pr-1", "human", time.Now())
	s.Escalate("pr-1")

	// Verify escalation state persisted
	s.mu.RLock()
	entry := s.entries["pr-1"]
	s.mu.RUnlock()
	if entry == nil {
		t.Fatal("entry not found")
	}
	if !entry.Escalated {
		t.Error("entry should be escalated")
	}
}

func TestReviewQueue_AssignHuman(t *testing.T) {
	q := NewReviewQueue()
	q.Add(&ReviewQueueItem{
		ID: "pr-1", Status: ReviewStatusPending,
		SubmittedAt: time.Now(),
	})

	err := q.AssignHuman("pr-1", "human-reviewer-1")
	if err != nil {
		t.Fatalf("AssignHuman: %v", err)
	}

	item, _ := q.Get("pr-1")
	if item.AssignedHuman != "human-reviewer-1" {
		t.Errorf("AssignedHuman = %q, want %q", item.AssignedHuman, "human-reviewer-1")
	}
	if item.Status != ReviewStatusInProgress {
		t.Errorf("Status = %q, want %q", item.Status, ReviewStatusInProgress)
	}
}

func TestReviewQueue_UpdateStatus(t *testing.T) {
	q := NewReviewQueue()
	q.Add(&ReviewQueueItem{
		ID: "pr-1", Status: ReviewStatusPending,
		SubmittedAt: time.Now(),
	})

	err := q.UpdateStatus("pr-1", ReviewStatusComplete)
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	item, _ := q.Get("pr-1")
	if item.Status != ReviewStatusComplete {
		t.Errorf("Status = %q, want %q", item.Status, ReviewStatusComplete)
	}
}

func TestLoadTracker_SelectWithLoadAwareness(t *testing.T) {
	lt := NewLoadTracker()
	lt.IncrementLoad("model-busy")
	lt.IncrementLoad("model-busy")
	lt.IncrementLoad("model-busy")

	pool := []ModelPoolEntry{
		{Model: ModelInfo{Model: "model-idle", Provider: "openai"}, Provider: "openai"},
		{Model: ModelInfo{Model: "model-busy", Provider: "deepseek"}, Provider: "deepseek"},
		{Model: ModelInfo{Model: "model-light", Provider: "anthropic"}, Provider: "anthropic"},
	}
	lt.IncrementLoad("model-light") // 1 load

	selected := lt.SelectWithLoadAwareness(pool, 2)

	// model-idle (0 load) should be first, model-light (1 load) second
	if len(selected) != 2 {
		t.Fatalf("got %d models, want 2", len(selected))
	}
	if selected[0].Model.Model != "model-idle" {
		t.Errorf("first model = %q, want %q (lowest load)", selected[0].Model.Model, "model-idle")
	}
}

func TestQueueSaveLoadToDefaultPath(t *testing.T) {
	q := NewReviewQueue()
	q.Add(&ReviewQueueItem{
		ID: "test-1", PRURL: "https://example.com/1",
		Category: CategoryContract, Status: ReviewStatusPending,
		SubmittedAt: time.Now(), Tier: trust.TierObserved,
	})

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test-reviews.json")
	if err := q.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file was written
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not saved: %v", err)
	}

	q2 := NewReviewQueue()
	if err := q2.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, ok := q2.Get("test-1")
	if !ok {
		t.Fatal("not found after load")
	}
	if got.Category != CategoryContract {
		t.Errorf("Category = %q", got.Category)
	}
}
