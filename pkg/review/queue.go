// Package review — queue.go
//
// Review load balancing and priority queue.
// Implements spec §6.3:
//   - ReviewQueue: persistent queue with priority scoring (risk × staleness)
//   - ReviewAssigner: self-review prevention, trust-tier routing, human gating
//   - HumanReviewFilter: determines what a human MUST see vs. can auto-merge
//   - SLATracker: per-category SLA durations with breach detection

package review

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

// ReviewStatus tracks the lifecycle of a review in the queue.
type ReviewStatus string

const (
	ReviewStatusPending    ReviewStatus = "pending"
	ReviewStatusInProgress ReviewStatus = "in-progress"
	ReviewStatusComplete   ReviewStatus = "complete"
	ReviewStatusEscalated  ReviewStatus = "escalated"
)

// ReviewQueueItem is a reviewable PR in the queue.
type ReviewQueueItem struct {
	ID                     string            `json:"id"`
	PRURL                  string            `json:"pr_url"`
	AuthorAgentID          string            `json:"author_agent_id"`
	Category               ChangeCategory    `json:"change_category"`
	RiskScore              float64           `json:"risk_score"`
	SubmittedAt            time.Time         `json:"submitted_at"`
	Status                 ReviewStatus      `json:"status"`
	AssignedHuman          string            `json:"assigned_human,omitempty"`
	AssignedModels         []string          `json:"assigned_models,omitempty"`
	PriorityScore          float64           `json:"priority_score"`
	EstimatedReviewMinutes int               `json:"estimated_review_minutes"`
	Tier                   trust.TrustTier   `json:"trust_tier"`
	GatesPassed            bool              `json:"gates_passed"`
}

// PriorityScore calculates the priority as risk × staleness_hours.
func (item *ReviewQueueItem) CalculatePriority() {
	hours := time.Since(item.SubmittedAt).Hours()
	if hours < 0 {
		hours = 0
	}
	item.PriorityScore = item.RiskScore * hours
}

// ReviewQueue is a persistent, priority-ordered queue of reviews.
type ReviewQueue struct {
	mu    sync.RWMutex
	items map[string]*ReviewQueueItem
}

// NewReviewQueue creates an empty queue.
func NewReviewQueue() *ReviewQueue {
	return &ReviewQueue{
		items: make(map[string]*ReviewQueueItem),
	}
}

// Add inserts an item into the queue, recalculating its priority first.
func (q *ReviewQueue) Add(item *ReviewQueueItem) error {
	if item.ID == "" {
		return fmt.Errorf("queue item must have an ID")
	}
	item.CalculatePriority()
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items[item.ID] = item
	return nil
}

// Get retrieves an item by ID.
func (q *ReviewQueue) Get(id string) (ReviewQueueItem, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	item, ok := q.items[id]
	if !ok {
		return ReviewQueueItem{}, false
	}
	return *item, true
}

// List returns all items matching an optional status filter.
func (q *ReviewQueue) List(status ReviewStatus) []ReviewQueueItem {
	q.mu.RLock()
	defer q.mu.RUnlock()
	var result []ReviewQueueItem
	for _, item := range q.items {
		if status == "" || item.Status == status {
			result = append(result, *item)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].PriorityScore > result[j].PriorityScore
	})
	return result
}

// ListPendingSorted returns pending/in-progress items sorted by priority descending.
func (q *ReviewQueue) ListPendingSorted() []ReviewQueueItem {
	q.mu.RLock()
	defer q.mu.RUnlock()
	var result []ReviewQueueItem
	for _, item := range q.items {
		// Recalculate priority (stale hours may have increased)
		item.CalculatePriority()
		if item.Status == ReviewStatusPending || item.Status == ReviewStatusInProgress {
			result = append(result, *item)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].PriorityScore > result[j].PriorityScore
	})
	return result
}

// AssignHuman records a human reviewer assignment.
func (q *ReviewQueue) AssignHuman(itemID, humanID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	item, ok := q.items[itemID]
	if !ok {
		return fmt.Errorf("item %q not found", itemID)
	}
	item.AssignedHuman = humanID
	if item.Status == ReviewStatusPending {
		item.Status = ReviewStatusInProgress
	}
	return nil
}

// AssignModels records model reviewer assignments.
func (q *ReviewQueue) AssignModels(itemID string, modelIDs []string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	item, ok := q.items[itemID]
	if !ok {
		return fmt.Errorf("item %q not found", itemID)
	}
	item.AssignedModels = modelIDs
	if item.Status == ReviewStatusPending {
		item.Status = ReviewStatusInProgress
	}
	return nil
}

// UpdateStatus changes the review status.
func (q *ReviewQueue) UpdateStatus(itemID string, status ReviewStatus) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	item, ok := q.items[itemID]
	if !ok {
		return fmt.Errorf("item %q not found", itemID)
	}
	item.Status = status
	return nil
}

// Save persists the queue to a JSON file.
func (q *ReviewQueue) Save(path string) error {
	q.mu.RLock()
	defer q.mu.RUnlock()

	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create queue dir: %w", err)
	}

	var items []ReviewQueueItem
	for _, item := range q.items {
		items = append(items, *item)
	}

	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal queue: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// Load restores the queue from a JSON file. Returns no error if file doesn't exist.
func (q *ReviewQueue) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read queue file: %w", err)
	}

	var items []ReviewQueueItem
	if err := json.Unmarshal(data, &items); err != nil {
		return fmt.Errorf("unmarshal queue: %w", err)
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	for i := range items {
		item := items[i]
		q.items[item.ID] = &item
	}
	return nil
}

// DefaultQueuePath returns the canonical path for the review queue JSON file.
func DefaultQueuePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	return filepath.Join(home, ".helix", "queue", "reviews.json")
}

// =============================================================================
// HumanReviewFilter
// =============================================================================

// HumanReviewFilter determines what a human MUST review vs. what can be auto-merged.
type HumanReviewFilter struct{}

// NewHumanReviewFilter creates a filter with spec-default rules.
func NewHumanReviewFilter() *HumanReviewFilter {
	return &HumanReviewFilter{}
}

// MustSeeHuman returns true if the change requires human judgment.
// Per spec §6.3: architectural decisions, novel patterns, rejected agent reviews,
// override requests, or Provisional-tier escalation.
func (f *HumanReviewFilter) MustSeeHuman(item ReviewQueueItem) bool {
	// Contract changes always need human intent approval
	if item.Category == CategoryContract {
		return true
	}

	// Provisional-tier agents always need human oversight
	if item.Tier == trust.TierProvisional {
		return true
	}

	// Observed-tier agents need human review for behavioral changes
	if item.Tier == trust.TierObserved && (item.Category == CategoryBehavioral || item.Category == CategoryResilience) {
		return true
	}

	return false
}

// CanAutoMerge returns true if the change can be merged without human review.
// Per spec: cosmetic + Veteran tier + Tier1/Tier2 gates passed.
func (f *HumanReviewFilter) CanAutoMerge(item ReviewQueueItem) bool {
	if item.Category != CategoryCosmetic {
		return false
	}
	if item.Tier != trust.TierVeteran {
		return false
	}
	if !item.GatesPassed {
		return false
	}
	return true
}

// =============================================================================
// SLATracker
// =============================================================================

// SLAEntry records the timeline of a review assignment.
type SLAEntry struct {
	ItemID       string    `json:"item_id"`
	ReviewerType string    `json:"reviewer_type"` // "human" or "agent"
	AssignedAt   time.Time `json:"assigned_at"`
	FirstReview  time.Time `json:"first_review,omitempty"`
	ResolvedAt   time.Time `json:"resolved_at,omitempty"`
	Escalated    bool      `json:"escalated"`
}

// SLADurations defines category-to-SLA mappings.
var SLADurations = map[ChangeCategory]time.Duration{
	CategoryContract:   4 * time.Hour,
	CategoryBehavioral: 24 * time.Hour,
	CategoryResilience: 48 * time.Hour,
	CategoryCosmetic:   72 * time.Hour,
}

// SLATracker tracks review SLA compliance.
type SLATracker struct {
	mu      sync.RWMutex
	entries map[string]*SLAEntry
}

// NewSLATracker creates a new SLA tracker.
func NewSLATracker() *SLATracker {
	return &SLATracker{
		entries: make(map[string]*SLAEntry),
	}
}

// RecordAssignment logs that a reviewer was assigned to an item.
func (s *SLATracker) RecordAssignment(itemID, reviewerType string, timestamp time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[itemID] = &SLAEntry{
		ItemID:       itemID,
		ReviewerType: reviewerType,
		AssignedAt:   timestamp,
	}
}

// RecordFirstReview records that the first review action was taken.
func (s *SLATracker) RecordFirstReview(itemID string, timestamp time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.entries[itemID]; ok {
		entry.FirstReview = timestamp
	}
}

// RecordResolution records that the review was resolved.
func (s *SLATracker) RecordResolution(itemID string, timestamp time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.entries[itemID]; ok {
		entry.ResolvedAt = timestamp
	}
}

// TimeToFirstReview returns how long until the first review action, or -1 if not yet reviewed.
func (s *SLATracker) TimeToFirstReview(itemID string) time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[itemID]
	if !ok || entry.FirstReview.IsZero() {
		return -1
	}
	return entry.FirstReview.Sub(entry.AssignedAt)
}

// TimeToResolution returns how long until resolution, or -1 if not resolved.
func (s *SLATracker) TimeToResolution(itemID string) time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[itemID]
	if !ok || entry.ResolvedAt.IsZero() {
		return -1
	}
	return entry.ResolvedAt.Sub(entry.AssignedAt)
}

// IsBreached returns true if the SLA for the given category has been breached
// since assignment. It uses the current time if not yet resolved.
func (s *SLATracker) IsBreached(itemID string, category ChangeCategory) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[itemID]
	if !ok {
		return false
	}

	sla, ok := SLADurations[category]
	if !ok {
		sla = 72 * time.Hour // default: cosmetic SLA
	}

	deadline := entry.AssignedAt.Add(sla)
	if !entry.ResolvedAt.IsZero() {
		return entry.ResolvedAt.After(deadline)
	}
	return time.Now().After(deadline)
}

// Escalate marks an item for human escalation.
func (s *SLATracker) Escalate(itemID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.entries[itemID]; ok {
		entry.Escalated = true
	}
}

// =============================================================================
// ReviewAssigner
// =============================================================================

// ReviewAssigner assigns human and model reviewers to a queue item.
type ReviewAssigner struct {
	scaling *TierScaling
	filter  *HumanReviewFilter
}

// NewReviewAssigner creates a new assigner.
func NewReviewAssigner() *ReviewAssigner {
	return &ReviewAssigner{
		scaling: NewTierScaling(),
		filter:  NewHumanReviewFilter(),
	}
}

// AssignmentResult captures the outcome of a review assignment.
type AssignmentResult struct {
	AssignedModels  []string `json:"assigned_models"`
	HumanNeeded     bool     `json:"human_needed"`
	HumanReason     string   `json:"human_reason,omitempty"`
	PanelSize       int      `json:"panel_size"`
	ConsensusNeeded int      `json:"consensus_needed"`
	AutoMerge       bool     `json:"auto_merge"`
}

// AssignReviewers determines reviewer assignments for a queue item.
// It prevents self-review, applies trust-tier gates, and routes to humans when needed.
func (ra *ReviewAssigner) AssignReviewers(item ReviewQueueItem, pool []ModelPoolEntry) (*AssignmentResult, error) {
	result := &AssignmentResult{}

	// Check if this can be auto-merged
	if ra.filter.CanAutoMerge(item) {
		result.AutoMerge = true
		result.HumanNeeded = false
		return result, nil
	}

	// Determine if human review is required
	result.HumanNeeded = ra.filter.MustSeeHuman(item)
	if result.HumanNeeded {
		switch {
		case item.Category == CategoryContract:
			result.HumanReason = "contract changes require human intent approval"
		case item.Tier == trust.TierProvisional:
			result.HumanReason = "provisional-tier agents require human oversight"
		default:
			result.HumanReason = "change requires human judgment per review policy"
		}
	}

	// Filter out self-review
	var eligible []ModelPoolEntry
	for _, entry := range pool {
		if entry.Model.Model == item.AuthorAgentID {
			continue
		}
		eligible = append(eligible, entry)
	}

	// If no eligible models after self-review filter, human MUST review
	if len(eligible) == 0 && !result.HumanNeeded {
		result.HumanNeeded = true
		result.HumanReason = "no eligible models (all match author ID)"
		return result, nil
	}

	if len(eligible) == 0 {
		return result, nil
	}

	// Get tier policy and adjust panel size
	policy, err := ra.scaling.PolicyForTier(item.Tier)
	if err != nil {
		policy, _ = ra.scaling.PolicyForTier(trust.TierProvisional) // fallback to strictest
	}

	result.PanelSize = policy.PanelSize
	categoryPanelSize := PanelSizeForCategory(item.Category)
	if result.PanelSize > categoryPanelSize {
		result.PanelSize = categoryPanelSize
	}
	if result.PanelSize > len(eligible) {
		result.PanelSize = len(eligible)
	}
	result.ConsensusNeeded = policy.ConsensusThreshold
	if result.ConsensusNeeded > result.PanelSize {
		result.ConsensusNeeded = result.PanelSize
	}

	// Use FormationAssigner for model selection with diversity
	tracker := NewRotationTracker()
	assigner := NewFormationAssigner(tracker, DefaultRotationConfig())

	seed := fmt.Sprintf("%s-%d", item.ID, time.Now().UnixNano())
	selected, err := assigner.AssignFormation(eligible, item.Category, seed)
	if err != nil {
		// Fall back to first N eligible models
		for i := 0; i < result.PanelSize && i < len(eligible); i++ {
			result.AssignedModels = append(result.AssignedModels, eligible[i].Model.Model)
		}
	} else {
		for _, m := range selected {
			result.AssignedModels = append(result.AssignedModels, m.Model.Model)
		}
	}

	return result, nil
}

// =============================================================================
// LoadAwareModelSelection
// =============================================================================

// LoadTracker tracks concurrent review assignments per model.
type LoadTracker struct {
	mu     sync.RWMutex
	counts map[string]int
}

// NewLoadTracker creates a new load tracker.
func NewLoadTracker() *LoadTracker {
	return &LoadTracker{
		counts: make(map[string]int),
	}
}

// IncrementLoad increases the concurrent review count for a model.
func (lt *LoadTracker) IncrementLoad(modelID string) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	lt.counts[modelID]++
}

// DecrementLoad decreases the concurrent review count.
func (lt *LoadTracker) DecrementLoad(modelID string) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	if lt.counts[modelID] > 0 {
		lt.counts[modelID]--
	}
}

// GetModelLoad returns the current concurrent review count for a model.
func (lt *LoadTracker) GetModelLoad(modelID string) int {
	lt.mu.RLock()
	defer lt.mu.RUnlock()
	return lt.counts[modelID]
}

// SelectWithLoadAwareness filters and sorts a model pool, preferring models
// with lower concurrent load.
func (lt *LoadTracker) SelectWithLoadAwareness(pool []ModelPoolEntry, n int) []ModelPoolEntry {
	lt.mu.RLock()
	defer lt.mu.RUnlock()

	type loadScored struct {
		entry ModelPoolEntry
		load  int
	}
	var entries []loadScored
	for _, entry := range pool {
		entries = append(entries, loadScored{
			entry: entry,
			load:  lt.counts[entry.Model.Model],
		})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].load < entries[j].load
	})

	result := make([]ModelPoolEntry, 0, n)
	for i := 0; i < n && i < len(entries); i++ {
		result = append(result, entries[i].entry)
	}
	return result
}

// TokenBudgetRemaining returns a mock token budget. In production this would
// query the agent's actual token tracking system.
func (lt *LoadTracker) TokenBudgetRemaining(modelID string) int64 {
	// Default budget: 1M tokens. Production would query a real token tracker.
	_ = modelID
	return int64(math.MaxInt64)
}
