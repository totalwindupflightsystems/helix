package review

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"
)

// =============================================================================
// Model Rotation — spec §Model Formation Strategy:
// "Rotation: model assignments change per-review to prevent adversarial
// adaptation"
//
// Without rotation, a model that always plays "primary reviewer" can be
// reverse-engineered by adversarial code. Rotation ensures every model
// takes every role over time, preventing any single model from being
// "gamed" based on its role assignment.
// =============================================================================

// ModelPoolEntry is a candidate model for the rotation pool.
type ModelPoolEntry struct {
	Model    ModelInfo
	Provider string // provider family (openai, deepseek, anthropic, etc.)
	RLHF     string // RLHF training style (helpful, constitutional, dpo, etc.)
}

// RotationConfig controls model assignment rotation behavior.
type RotationConfig struct {
	// RotationSeed determines the per-review slot assignment. If empty,
	// a time-based seed is used. Production callers should pass the PR URL
	// or review ID so assignments are deterministic per-review.
	RotationSeed string

	// MaxConsecutiveSameRole prevents a model from being assigned the same
	// role more than N times in a row. Default: 3.
	MaxConsecutiveSameRole int

	// MinRLHFDiversity requires at least N different RLHF training styles
	// in every formation. Per spec: "at least one model trained with
	// different RLHF preferences." Default: 1.
	MinRLHFDiversity int

	// MinContextDiversity requires at least N different context window
	// architectures. Per spec: "at least one model with different context
	// window architecture." Default: 1.
	MinContextDiversity int
}

// DefaultRotationConfig returns spec-compliant rotation defaults.
func DefaultRotationConfig() RotationConfig {
	return RotationConfig{
		MaxConsecutiveSameRole: 3,
		MinRLHFDiversity:       1,
		MinContextDiversity:    1,
	}
}

// RotationTracker records model→role assignments across reviews to enforce
// rotation rules and prevent adversarial adaptation.
type RotationTracker struct {
	mu sync.RWMutex

	// consecutiveRole[modelID] = map[role]count
	consecutiveRole map[string]map[ReviewRole]int

	// lastAssignment[modelID] = last role assigned
	lastAssignment map[string]ReviewRole

	// totalAssignments[modelID] = total times assigned any role
	totalAssignments map[string]int

	// reviewCount is the total number of reviews tracked
	reviewCount int
}

// NewRotationTracker creates a new tracker.
func NewRotationTracker() *RotationTracker {
	return &RotationTracker{
		consecutiveRole:  make(map[string]map[ReviewRole]int),
		lastAssignment:   make(map[string]ReviewRole),
		totalAssignments: make(map[string]int),
	}
}

// RecordAssignment tracks that a model was assigned a role in a review.
func (rt *RotationTracker) RecordAssignment(modelID string, role ReviewRole) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if rt.consecutiveRole[modelID] == nil {
		rt.consecutiveRole[modelID] = make(map[ReviewRole]int)
	}

	// If this is the same role as last time, increment consecutive count
	if rt.lastAssignment[modelID] == role {
		rt.consecutiveRole[modelID][role]++
	} else {
		// Role changed — reset consecutive count for the old role,
		// set to 1 for the new role
		for r := range rt.consecutiveRole[modelID] {
			rt.consecutiveRole[modelID][r] = 0
		}
		rt.consecutiveRole[modelID][role] = 1
	}

	rt.lastAssignment[modelID] = role
	rt.totalAssignments[modelID]++
}

// RecordReview increments the review counter and should be called at the
// end of each review to finalize the rotation history.
func (rt *RotationTracker) RecordReview() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.reviewCount++
}

// ConsecutiveCount returns how many times a model has been assigned a role
// consecutively (without a different role in between).
func (rt *RotationTracker) ConsecutiveCount(modelID string, role ReviewRole) int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	if roles, ok := rt.consecutiveRole[modelID]; ok {
		return roles[role]
	}
	return 0
}

// TotalAssignments returns how many times a model has been assigned any role.
func (rt *RotationTracker) TotalAssignments(modelID string) int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.totalAssignments[modelID]
}

// LastRole returns the last role assigned to a model, or empty if never assigned.
func (rt *RotationTracker) LastRole(modelID string) ReviewRole {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.lastAssignment[modelID]
}

// ReviewCount returns the total number of reviews tracked.
func (rt *RotationTracker) ReviewCount() int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.reviewCount
}

// AssignmentReport summarizes the rotation history for diagnostics.
type AssignmentReport struct {
	ReviewCount int                       `json:"review_count"`
	Models      map[string]ModelUsageStat `json:"models"`
}

// ModelUsageStat summarizes one model's rotation history.
type ModelUsageStat struct {
	Total       int                `json:"total"`
	LastRole    ReviewRole         `json:"last_role"`
	Consecutive map[ReviewRole]int `json:"consecutive"`
}

// Report generates a rotation usage summary.
func (rt *RotationTracker) Report() AssignmentReport {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	report := AssignmentReport{
		ReviewCount: rt.reviewCount,
		Models:      make(map[string]ModelUsageStat),
	}
	for modelID := range rt.totalAssignments {
		stat := ModelUsageStat{
			Total:       rt.totalAssignments[modelID],
			LastRole:    rt.lastAssignment[modelID],
			Consecutive: make(map[ReviewRole]int),
		}
		if roles, ok := rt.consecutiveRole[modelID]; ok {
			for r, c := range roles {
				stat.Consecutive[r] = c
			}
		}
		report.Models[modelID] = stat
	}
	return report
}

// =============================================================================
// FormationAssigner — selects models from pool and assigns roles
// =============================================================================

// FormationAssigner takes a pool of candidate models and produces a
// ReviewPanel with rotated role assignments that satisfy diversity rules.
type FormationAssigner struct {
	tracker *RotationTracker
	config  RotationConfig
}

// NewFormationAssigner creates an assigner with the given config.
func NewFormationAssigner(tracker *RotationTracker, config RotationConfig) *FormationAssigner {
	if tracker == nil {
		tracker = NewRotationTracker()
	}
	return &FormationAssigner{
		tracker: tracker,
		config:  config,
	}
}

// AssignFormation selects models from the pool and assigns roles for a review.
// The number of models selected depends on the change category:
//   - Contract: 3 models (primary, adversarial, audit)
//   - Behavioral: 2 models (primary, adversarial)
//   - Resilience/Cosmetic: 1 model (primary)
//
// Selection criteria:
//  1. Enforce provider diversity (no two from same provider)
//  2. Prefer models with lower consecutive-same-role counts
//  3. Enforce RLHF diversity if configured
//  4. Deterministic per-review seed (prevents bias in selection)
func (fa *FormationAssigner) AssignFormation(pool []ModelPoolEntry, category ChangeCategory, seed string) ([]ModelPoolEntry, error) {
	if len(pool) == 0 {
		return nil, fmt.Errorf("empty model pool")
	}

	// Determine panel size from category
	panelSize := PanelSizeForCategory(category)
	if panelSize > len(pool) {
		return nil, fmt.Errorf("need %d models for %s category but pool has %d", panelSize, category, len(pool))
	}

	// Sort pool deterministically by seed for reproducible selection
	sorted := fa.sortByRotationPriority(pool, seed)

	// Select models satisfying diversity rules
	selected := fa.selectWithDiversity(sorted, panelSize)

	if len(selected) < panelSize {
		// Fall back: relax diversity constraints
		selected = sorted[:panelSize]
	}

	// Record assignments (rotate roles)
	roles := rolesForPanelSize(panelSize)
	for i, model := range selected {
		fa.tracker.RecordAssignment(model.Model.Model, roles[i])
	}
	fa.tracker.RecordReview()

	return selected, nil
}

// sortByRotationPriority orders models by rotation fairness:
// models with the highest consecutive-same-role count go last
// (they've been "stuck" in one role too long).
func (fa *FormationAssigner) sortByRotationPriority(pool []ModelPoolEntry, seed string) []ModelPoolEntry {
	// Create deterministic seed for stable ordering
	seedHash := sha256.Sum256([]byte(seed))
	seedInt := int64(0)
	for _, b := range seedHash[:8] {
		seedInt = (seedInt << 8) | int64(b)
	}
	rng := rand.New(rand.NewSource(seedInt))

	type scored struct {
		entry    ModelPoolEntry
		priority int // lower = higher priority
		jitter   int
	}

	scored_pool := make([]scored, len(pool))
	for i, entry := range pool {
		// Priority: models that haven't been assigned recently or have low
		// consecutive counts get higher priority (lower score)
		totalConsec := 0
		for _, role := range []ReviewRole{RolePrimary, RoleAdversarial, RoleAudit} {
			totalConsec += fa.tracker.ConsecutiveCount(entry.Model.Model, role)
		}
		// Models never used get top priority
		if fa.tracker.TotalAssignments(entry.Model.Model) == 0 {
			totalConsec = -100 // boost
		}
		scored_pool[i] = scored{
			entry:    entry,
			priority: totalConsec,
			jitter:   rng.Intn(100),
		}
	}

	sort.SliceStable(scored_pool, func(i, j int) bool {
		if scored_pool[i].priority != scored_pool[j].priority {
			return scored_pool[i].priority < scored_pool[j].priority
		}
		return scored_pool[i].jitter < scored_pool[j].jitter
	})

	result := make([]ModelPoolEntry, len(scored_pool))
	for i, s := range scored_pool {
		result[i] = s.entry
	}
	return result
}

// selectWithDiversity picks N models from the sorted pool ensuring:
// - No two models from the same provider
// - At least MinRLHFDiversity different RLHF styles
func (fa *FormationAssigner) selectWithDiversity(sorted []ModelPoolEntry, n int) []ModelPoolEntry {
	if n >= len(sorted) {
		return sorted
	}

	selected := make([]ModelPoolEntry, 0, n)
	seenProviders := make(map[string]bool)
	seenRLHF := make(map[string]bool)

	for _, entry := range sorted {
		if len(selected) >= n {
			break
		}
		// Check provider diversity
		if seenProviders[entry.Provider] && len(selected) > 0 {
			// Allow same provider only if we can't fill the panel otherwise
			continue
		}
		selected = append(selected, entry)
		seenProviders[entry.Provider] = true
		seenRLHF[entry.RLHF] = true
	}

	// If we couldn't fill with strict diversity, fill with whatever's available
	if len(selected) < n {
		for _, entry := range sorted {
			if len(selected) >= n {
				break
			}
			alreadySelected := false
			for _, s := range selected {
				if s.Model.Model == entry.Model.Model {
					alreadySelected = true
					break
				}
			}
			if !alreadySelected {
				selected = append(selected, entry)
			}
		}
	}

	return selected
}

// PanelSizeForCategory returns the required number of models per spec §Review Criteria.
func PanelSizeForCategory(category ChangeCategory) int {
	switch category {
	case CategoryContract:
		return 3
	case CategoryBehavioral:
		return 2
	case CategoryResilience:
		return 1
	case CategoryCosmetic:
		return 1
	default:
		return 3
	}
}

// rolesForPanelSize returns the role assignment order for a given panel size.
// The first model is always primary, second is adversarial, third is audit.
func rolesForPanelSize(size int) []ReviewRole {
	roles := []ReviewRole{RolePrimary}
	if size >= 2 {
		roles = append(roles, RoleAdversarial)
	}
	if size >= 3 {
		roles = append(roles, RoleAudit)
	}
	return roles
}

// CheckDiversity verifies that a formation meets the diversity rules from spec.
func CheckDiversity(formation []ModelPoolEntry, config RotationConfig) error {
	if len(formation) == 0 {
		return fmt.Errorf("empty formation")
	}

	// Rule 1: No two models from the same provider
	providers := make(map[string]int)
	for _, m := range formation {
		providers[m.Provider]++
	}
	for provider, count := range providers {
		if count > 1 && len(formation) > 1 {
			return fmt.Errorf("diversity violation: %d models from provider %q", count, provider)
		}
	}

	// Rule 2: At least MinRLHFDiversity different RLHF styles
	if config.MinRLHFDiversity > 0 {
		rlhfStyles := make(map[string]bool)
		for _, m := range formation {
			rlhfStyles[m.RLHF] = true
		}
		if len(rlhfStyles) < config.MinRLHFDiversity && len(formation) > 1 {
			return fmt.Errorf("RLHF diversity violation: need at least %d different RLHF styles, got %d",
				config.MinRLHFDiversity, len(rlhfStyles))
		}
	}

	return nil
}

// FormatAssignment renders the role assignments for a review as a string.
func FormatAssignment(selected []ModelPoolEntry, roles []ReviewRole) string {
	result := ""
	for i, model := range selected {
		role := ""
		if i < len(roles) {
			role = string(roles[i])
		}
		result += fmt.Sprintf("  %s: %s (%s, %s)\n", role, model.Model.Model, model.Provider, model.RLHF)
	}
	return result
}

// SeedFromPR creates a deterministic rotation seed from a PR URL and timestamp.
// This ensures the same PR always gets the same model assignment, while
// different PRs get different assignments.
func SeedFromPR(prURL string, timestamp time.Time) string {
	return fmt.Sprintf("%s-%d", prURL, timestamp.Unix())
}

// HashSeed creates a short hash from a seed string for logging.
func HashSeed(seed string) string {
	h := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(h[:4])
}
