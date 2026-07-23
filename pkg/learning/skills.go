// Package learning implements the Helix Phase 12 learning and knowledge transfer
// subsystem. This file (§12.2) provides the Skill Registry — a marketplace where
// high-trust agents publish reusable skills and other agents load them during
// context assembly. Skill effectiveness is tracked and ineffective skills lose
// trust weighting.
package learning

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Constants — trust gates and effectiveness deltas (§12.2)
// ─────────────────────────────────────────────────────────────────────────────

const (
	// MinPublishTrust is the Trusted-tier floor required to publish a skill.
	MinPublishTrust = 0.65
	// MinDomainMerges is the minimum successful merges in-domain required to publish.
	MinDomainMerges = 5
	// DefaultSkillTrustWeight is the starting effectiveness for a new skill.
	DefaultSkillTrustWeight = 1.0
	// DeprecateTrustThreshold auto-deprecates skills below this weight.
	DeprecateTrustThreshold = 0.3
	// SuccessTrustDelta is applied on a successful skill outcome.
	SuccessTrustDelta = 0.01
	// FailureTrustDelta is applied on a failed skill outcome (subtracted).
	FailureTrustDelta = 0.05
)

// ─────────────────────────────────────────────────────────────────────────────
// Skill types (§12.2)
// ─────────────────────────────────────────────────────────────────────────────

// Skill is a versioned, reusable package published by an agent.
type Skill struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Version       string    `json:"version"`
	AuthorAgentID string    `json:"author_agent_id"`
	Domain        string    `json:"domain"` // auth, database, api, infra, security, testing, docs
	Description   string    `json:"description"`
	Prompt        string    `json:"prompt"`
	EvidenceTags  []string  `json:"evidence_tags,omitempty"`
	TrustWeight   float64   `json:"trust_weight"` // 0.0–1.0
	UsageCount    int       `json:"usage_count"`
	SuccessCount  int       `json:"success_count,omitempty"`
	FailureCount  int       `json:"failure_count,omitempty"`
	SuccessRate   float64   `json:"success_rate"` // fraction of uses that led to successful merges
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Deprecated    bool      `json:"deprecated"`
	// DeprecationReason records why the skill was deprecated (manual or auto).
	DeprecationReason string `json:"deprecation_reason,omitempty"`
}

// SkillStore is the persistence backend for the skill marketplace.
type SkillStore interface {
	Publish(skill Skill) error
	Get(id string) (*Skill, error)
	List(domain string, minTrust float64) ([]Skill, error)
	Deprecate(id string, reason string) error
	RecordUsage(skillID string, success bool) error
	UpdateTrustWeight(skillID string, delta float64) error
}

// SkillRegistry gates publication, loads top skills, and records outcomes.
type SkillRegistry struct {
	store SkillStore
}

// NewSkillRegistry creates a registry backed by the given store.
func NewSkillRegistry(store SkillStore) *SkillRegistry {
	return &SkillRegistry{store: store}
}

// NewSkillID generates a UUID-like hex identifier.
// Returns an error if crypto/rand fails.
func NewSkillID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("learning: crypto/rand failed: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// DefaultSkillsPath returns ~/.helix/skills/skills.json.
func DefaultSkillsPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".helix", "skills", "skills.json")
	}
	return filepath.Join(home, ".helix", "skills", "skills.json")
}

// ─────────────────────────────────────────────────────────────────────────────
// FileSkillStore — in-memory + JSON file persistence
// ─────────────────────────────────────────────────────────────────────────────

// FileSkillStore persists skills as a single JSON array at path.
// Thread-safe; every mutation rewrites the file.
type FileSkillStore struct {
	mu     sync.RWMutex
	path   string
	skills map[string]*Skill // keyed by ID
}

// NewFileSkillStore opens (or creates) a JSON-backed store at path.
// Pass "" to use DefaultSkillsPath().
func NewFileSkillStore(path string) (*FileSkillStore, error) {
	if path == "" {
		path = DefaultSkillsPath()
	}
	s := &FileSkillStore{
		path:   path,
		skills: make(map[string]*Skill),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// Path returns the JSON file path.
func (s *FileSkillStore) Path() string { return s.path }

func (s *FileSkillStore) load() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create skills dir: %w", err)
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read skills file: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	var list []Skill
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("parse skills file: %w", err)
	}
	for i := range list {
		sk := list[i]
		if sk.ID == "" {
			continue
		}
		cp := sk
		s.skills[sk.ID] = &cp
	}
	return nil
}

func (s *FileSkillStore) persistLocked() error {
	list := make([]Skill, 0, len(s.skills))
	for _, sk := range s.skills {
		list = append(list, *sk)
	}
	// Stable order for diffs.
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal skills: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create skills dir: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write skills tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename skills file: %w", err)
	}
	return nil
}

// Publish stores a skill (must already be validated by SkillRegistry).
func (s *FileSkillStore) Publish(skill Skill) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if skill.ID == "" {
		return fmt.Errorf("skill id is required")
	}
	if _, exists := s.skills[skill.ID]; exists {
		return fmt.Errorf("skill already exists: %s", skill.ID)
	}
	cp := skill
	s.skills[skill.ID] = &cp
	return s.persistLocked()
}

// Get retrieves a skill by ID.
func (s *FileSkillStore) Get(id string) (*Skill, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sk, ok := s.skills[id]
	if !ok {
		return nil, fmt.Errorf("skill not found: %s", id)
	}
	cp := *sk
	return &cp, nil
}

// List returns skills filtered by domain (empty = all) and minimum trust weight.
// Deprecated skills are included so callers can inspect them; Load filters them out.
func (s *FileSkillStore) List(domain string, minTrust float64) ([]Skill, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Skill, 0, len(s.skills))
	for _, sk := range s.skills {
		if domain != "" && sk.Domain != domain {
			continue
		}
		if sk.TrustWeight < minTrust {
			continue
		}
		out = append(out, *sk)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TrustWeight == out[j].TrustWeight {
			return out[i].Name < out[j].Name
		}
		return out[i].TrustWeight > out[j].TrustWeight
	})
	return out, nil
}

// Deprecate marks a skill as deprecated with a reason.
func (s *FileSkillStore) Deprecate(id string, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sk, ok := s.skills[id]
	if !ok {
		return fmt.Errorf("skill not found: %s", id)
	}
	sk.Deprecated = true
	sk.DeprecationReason = reason
	sk.UpdatedAt = time.Now().UTC()
	return s.persistLocked()
}

// RecordUsage increments usage counters and updates SuccessRate.
func (s *FileSkillStore) RecordUsage(skillID string, success bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sk, ok := s.skills[skillID]
	if !ok {
		return fmt.Errorf("skill not found: %s", skillID)
	}
	sk.UsageCount++
	if success {
		sk.SuccessCount++
	} else {
		sk.FailureCount++
	}
	total := sk.SuccessCount + sk.FailureCount
	if total > 0 {
		sk.SuccessRate = float64(sk.SuccessCount) / float64(total)
	}
	sk.UpdatedAt = time.Now().UTC()
	return s.persistLocked()
}

// UpdateTrustWeight applies delta to trust_weight (clamped 0–1) and
// auto-deprecates when weight falls below DeprecateTrustThreshold.
func (s *FileSkillStore) UpdateTrustWeight(skillID string, delta float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sk, ok := s.skills[skillID]
	if !ok {
		return fmt.Errorf("skill not found: %s", skillID)
	}
	sk.TrustWeight = clampSkillWeight(sk.TrustWeight + delta)
	sk.UpdatedAt = time.Now().UTC()
	if sk.TrustWeight < DeprecateTrustThreshold && !sk.Deprecated {
		sk.Deprecated = true
		sk.DeprecationReason = fmt.Sprintf(
			"auto-deprecated: trust weight %.2f below %.1f threshold",
			sk.TrustWeight, DeprecateTrustThreshold,
		)
	}
	return s.persistLocked()
}

func clampSkillWeight(w float64) float64 {
	if w < 0 {
		return 0
	}
	if w > 1.0 {
		return 1.0
	}
	return w
}

// ─────────────────────────────────────────────────────────────────────────────
// SkillRegistry methods
// ─────────────────────────────────────────────────────────────────────────────

// PublishGateError indicates why a skill publication was rejected.
type PublishGateError struct {
	Code    string
	Message string
}

func (e *PublishGateError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Publish validates: author must be Trusted tier (≥0.65 trust) and have ≥5
// successful merges in domain. Returns error if gating fails.
func (r *SkillRegistry) Publish(authorAgentID string, authorTrust float64, domainMerges int, skill Skill) error {
	if r == nil || r.store == nil {
		return fmt.Errorf("skill registry: nil store")
	}
	if authorTrust < MinPublishTrust {
		return &PublishGateError{
			Code: "TRUST_GATE",
			Message: fmt.Sprintf(
				"agent trust score %.2f below required %.2f (Trusted tier)",
				authorTrust, MinPublishTrust,
			),
		}
	}
	if domainMerges < MinDomainMerges {
		return &PublishGateError{
			Code: "DOMAIN_GATE",
			Message: fmt.Sprintf(
				"agent has %d successful merges in domain %q, need ≥%d",
				domainMerges, skill.Domain, MinDomainMerges,
			),
		}
	}
	if skill.Name == "" {
		return fmt.Errorf("skill name is required")
	}
	if skill.Domain == "" {
		return fmt.Errorf("skill domain is required")
	}
	// Domain should match the learning taxonomy when provided as a known Domain.
	if skill.Domain != "" && !IsValidDomain(Domain(skill.Domain)) {
		// Allow non-canonical domains (spec also mentions go/typescript/python)
		// but prefer the canonical set; do not hard-fail for flexibility.
		_ = skill.Domain
	}
	if skill.Prompt == "" {
		return fmt.Errorf("skill prompt is required")
	}

	now := time.Now().UTC()
	if skill.ID == "" {
		id, err := NewSkillID()
		if err != nil {
			return err
		}
		skill.ID = id
	}
	if skill.Version == "" {
		skill.Version = "0.1.0"
	}
	if skill.AuthorAgentID == "" {
		skill.AuthorAgentID = authorAgentID
	}
	if skill.TrustWeight <= 0 {
		skill.TrustWeight = DefaultSkillTrustWeight
	}
	skill.CreatedAt = now
	skill.UpdatedAt = now
	skill.Deprecated = false
	skill.DeprecationReason = ""

	return r.store.Publish(skill)
}

// Load returns top-N non-deprecated skills by trust_weight for a domain.
// limit ≤ 0 returns all matching skills.
func (r *SkillRegistry) Load(domain string, limit int) ([]Skill, error) {
	if r == nil || r.store == nil {
		return nil, fmt.Errorf("skill registry: nil store")
	}
	all, err := r.store.List(domain, 0)
	if err != nil {
		return nil, err
	}
	out := make([]Skill, 0, len(all))
	for _, sk := range all {
		if sk.Deprecated {
			continue
		}
		if domain != "" && sk.Domain != domain {
			continue
		}
		out = append(out, sk)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].TrustWeight > out[j].TrustWeight
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Get retrieves a skill by ID.
func (r *SkillRegistry) Get(id string) (*Skill, error) {
	if r == nil || r.store == nil {
		return nil, fmt.Errorf("skill registry: nil store")
	}
	return r.store.Get(id)
}

// List returns skills filtered by domain (empty = all) and minimum trust weight.
func (r *SkillRegistry) List(domain string, minTrust float64) ([]Skill, error) {
	if r == nil || r.store == nil {
		return nil, fmt.Errorf("skill registry: nil store")
	}
	return r.store.List(domain, minTrust)
}

// Deprecate marks a skill as deprecated.
func (r *SkillRegistry) Deprecate(skillID string, reason string) error {
	if r == nil || r.store == nil {
		return fmt.Errorf("skill registry: nil store")
	}
	if reason == "" {
		reason = "deprecated"
	}
	return r.store.Deprecate(skillID, reason)
}

// RecordOutcome updates trust weight: success +0.01 (capped 1.0), failure -0.05.
// Auto-deprecates if trust_weight falls below 0.3.
func (r *SkillRegistry) RecordOutcome(skillID string, success bool) error {
	if r == nil || r.store == nil {
		return fmt.Errorf("skill registry: nil store")
	}
	if err := r.store.RecordUsage(skillID, success); err != nil {
		return err
	}
	delta := SuccessTrustDelta
	if !success {
		delta = -FailureTrustDelta
	}
	return r.store.UpdateTrustWeight(skillID, delta)
}
