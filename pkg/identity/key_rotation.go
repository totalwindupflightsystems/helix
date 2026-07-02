package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// KeyRotator implements the key rotation procedure from spec §5.5 and §14.
// It tracks key ages, detects expired or dead keys, and orchestrates rotation.
//
// Key types:
//   - SSH keys (agent → Forgejo)
//   - Forgejo PATs (agent API access)
//   - OpenRouter API keys (agent LLM budget)
//
// The rotator does NOT directly call external APIs — it produces a RotationPlan
// that the caller (CLI, cron) executes. This makes it fully unit-testable.

// KeyType identifies what kind of credential is being rotated.
type KeyType string

const (
	KeyTypeSSH        KeyType = "ssh"
	KeyTypePAT        KeyType = "pat"
	KeyTypeOpenRouter KeyType = "openrouter"
)

// KeyInfo tracks metadata about a single agent key.
type KeyInfo struct {
	Type        KeyType   `json:"type"`
	KeyHash     string    `json:"key_hash"` // sha256:hex — never the raw key
	CreatedAt   time.Time `json:"created_at"`
	LastRotated time.Time `json:"last_rotated"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"` // PATs expire; SSH/OpenRouter don't auto-expire
	Status      KeyStatus `json:"status"`
}

// KeyStatus tracks the health of a key.
type KeyStatus string

const (
	KeyStatusActive   KeyStatus = "active"
	KeyStatusExpiring KeyStatus = "expiring" // PAT nearing expiry
	KeyStatusExpired  KeyStatus = "expired"
	KeyStatusDead     KeyStatus = "dead" // API returns 401
	KeyStatusUnknown  KeyStatus = "unknown"
)

// RotationPolicy defines when a key should be rotated.
type RotationPolicy struct {
	// MaxAge is the maximum age before mandatory rotation. Zero = no age limit.
	MaxAge time.Duration
	// ExpiryWarning is how long before expiry to warn. Default 7 days.
	ExpiryWarning time.Duration
}

// DefaultRotationPolicies returns the spec-recommended rotation intervals.
// SSH keys: rotate every 90 days. PATs: rotate 7 days before expiry.
// OpenRouter keys: rotate every 30 days.
func DefaultRotationPolicies() map[KeyType]RotationPolicy {
	return map[KeyType]RotationPolicy{
		KeyTypeSSH:        {MaxAge: 90 * 24 * time.Hour, ExpiryWarning: 7 * 24 * time.Hour},
		KeyTypePAT:        {MaxAge: 0, ExpiryWarning: 7 * 24 * time.Hour}, // PATs use ExpiresAt
		KeyTypeOpenRouter: {MaxAge: 30 * 24 * time.Hour, ExpiryWarning: 3 * 24 * time.Hour},
	}
}

// RotationReason explains why a key needs rotation.
type RotationReason string

const (
	RotationAgeExceeded    RotationReason = "age_exceeded"
	RotationExpiringSoon   RotationReason = "expiring_soon"
	RotationAlreadyExpired RotationReason = "already_expired"
	RotationDeadKey        RotationReason = "dead_key"
	RotationManualTrigger  RotationReason = "manual"
)

// RotationAction describes a single key rotation to perform.
type RotationAction struct {
	AgentName string          `json:"agent_name"`
	KeyType   KeyType         `json:"key_type"`
	OldHash   string          `json:"old_hash"`
	Reason    RotationReason  `json:"reason"`
	Urgency   RotationUrgency `json:"urgency"`
}

// RotationUrgency indicates how quickly rotation should happen.
type RotationUrgency string

const (
	UrgencyImmediate RotationUrgency = "immediate" // expired/dead — rotate NOW
	UrgencyHigh      RotationUrgency = "high"      // expiring within warning window
	UrgencyNormal    RotationUrgency = "normal"    // age exceeded but functional
	UrgencyLow       RotationUrgency = "low"       // manual trigger, no urgency
)

// RotationPlan is the full set of rotations to perform.
type RotationPlan struct {
	Actions   []RotationAction `json:"actions"`
	Generated time.Time        `json:"generated_at"`
	Skipped   int              `json:"skipped"` // keys that don't need rotation
}

// HasImmediate returns true if any action requires immediate rotation.
func (p *RotationPlan) HasImmediate() bool {
	for _, a := range p.Actions {
		if a.Urgency == UrgencyImmediate {
			return true
		}
	}
	return false
}

// HasHigh returns true if any action has high urgency.
func (p *RotationPlan) HasHigh() bool {
	for _, a := range p.Actions {
		if a.Urgency == UrgencyHigh {
			return true
		}
	}
	return false
}

// Count returns the number of actions for a given urgency level.
func (p *RotationPlan) Count(urgency RotationUrgency) int {
	count := 0
	for _, a := range p.Actions {
		if a.Urgency == urgency {
			count++
		}
	}
	return count
}

// AgentKeyRegistry holds the key metadata for all agents.
type AgentKeyRegistry struct {
	// Keys is indexed by "agentName:keyType" for fast lookup.
	Keys map[string]KeyInfo `json:"keys"`
}

// NewAgentKeyRegistry creates an empty registry.
func NewAgentKeyRegistry() *AgentKeyRegistry {
	return &AgentKeyRegistry{Keys: make(map[string]KeyInfo)}
}

// RegisterKey adds or updates a key in the registry.
func (r *AgentKeyRegistry) RegisterKey(agentName string, keyType KeyType, keyHash string, createdAt time.Time, expiresAt time.Time) {
	key := agentKey(agentName, keyType)
	r.Keys[key] = KeyInfo{
		Type:        keyType,
		KeyHash:     keyHash,
		CreatedAt:   createdAt,
		LastRotated: createdAt,
		ExpiresAt:   expiresAt,
		Status:      KeyStatusActive,
	}
}

// MarkRotated updates the LastRotated timestamp and optionally the new hash.
func (r *AgentKeyRegistry) MarkRotated(agentName string, keyType KeyType, newHash string, rotatedAt time.Time) {
	key := agentKey(agentName, keyType)
	info, ok := r.Keys[key]
	if !ok {
		info = KeyInfo{
			Type:      keyType,
			CreatedAt: rotatedAt,
		}
	}
	info.LastRotated = rotatedAt
	if newHash != "" {
		info.KeyHash = newHash
	}
	info.Status = KeyStatusActive
	r.Keys[key] = info
}

// MarkDead marks a key as dead (API returns 401).
func (r *AgentKeyRegistry) MarkDead(agentName string, keyType KeyType) {
	key := agentKey(agentName, keyType)
	if info, ok := r.Keys[key]; ok {
		info.Status = KeyStatusDead
		r.Keys[key] = info
	}
}

// GetKey retrieves a key's info from the registry.
func (r *AgentKeyRegistry) GetKey(agentName string, keyType KeyType) (KeyInfo, bool) {
	info, ok := r.Keys[agentKey(agentName, keyType)]
	return info, ok
}

// EvaluateKey checks whether a key needs rotation per the given policy.
func EvaluateKey(info KeyInfo, policy RotationPolicy, now time.Time) (needsRotation bool, reason RotationReason, urgency RotationUrgency) {
	// Dead key — immediate rotation
	if info.Status == KeyStatusDead {
		return true, RotationDeadKey, UrgencyImmediate
	}

	// Expired PAT — immediate rotation
	if info.Status == KeyStatusExpired || (!info.ExpiresAt.IsZero() && now.After(info.ExpiresAt)) {
		return true, RotationAlreadyExpired, UrgencyImmediate
	}

	// Expiring soon — high urgency
	if !info.ExpiresAt.IsZero() && policy.ExpiryWarning > 0 {
		timeUntilExpiry := info.ExpiresAt.Sub(now)
		if timeUntilExpiry <= policy.ExpiryWarning && timeUntilExpiry > 0 {
			return true, RotationExpiringSoon, UrgencyHigh
		}
	}

	// Age exceeded — normal urgency
	if policy.MaxAge > 0 {
		age := now.Sub(info.LastRotated)
		if age > policy.MaxAge {
			// If more than 2x the max age, escalate to high
			if age > 2*policy.MaxAge {
				return true, RotationAgeExceeded, UrgencyHigh
			}
			return true, RotationAgeExceeded, UrgencyNormal
		}
	}

	return false, "", ""
}

// PlanRotation evaluates all keys in the registry and produces a RotationPlan.
func (r *AgentKeyRegistry) PlanRotation(policies map[KeyType]RotationPolicy, now time.Time) *RotationPlan {
	plan := &RotationPlan{
		Generated: now,
	}

	for key, info := range r.Keys {
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 {
			continue
		}
		agentName := parts[0]

		policy, ok := policies[info.Type]
		if !ok {
			// No policy for this key type — skip
			plan.Skipped++
			continue
		}

		needsRotation, reason, urgency := EvaluateKey(info, policy, now)
		if !needsRotation {
			plan.Skipped++
			continue
		}

		plan.Actions = append(plan.Actions, RotationAction{
			AgentName: agentName,
			KeyType:   info.Type,
			OldHash:   info.KeyHash,
			Reason:    reason,
			Urgency:   urgency,
		})
	}

	return plan
}

// HashKey computes a sha256:hex hash of a raw key value for storage in the registry.
// Never store raw keys — always store hashes.
func HashKey(rawKey string) string {
	h := sha256.Sum256([]byte(rawKey))
	return "sha256:" + hex.EncodeToString(h[:])
}

// VerifyKeyHash checks whether a raw key matches a stored hash.
func VerifyKeyHash(rawKey, storedHash string) bool {
	return HashKey(rawKey) == storedHash
}

// keyEntry creates the registry key for an agent + key type pair.
func agentKey(agentName string, keyType KeyType) string {
	return fmt.Sprintf("%s:%s", agentName, keyType)
}

// FormatRotationPlan renders a human-readable summary of the rotation plan.
func FormatRotationPlan(plan *RotationPlan) string {
	if len(plan.Actions) == 0 {
		return fmt.Sprintf("Key rotation: %d keys checked, 0 need rotation (%d skipped)\n", len(plan.Actions)+plan.Skipped, plan.Skipped)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Key Rotation Plan (%d actions, %d skipped)\n", len(plan.Actions), plan.Skipped))
	b.WriteString("=============================================\n\n")

	// Group by urgency
	urgencies := []RotationUrgency{UrgencyImmediate, UrgencyHigh, UrgencyNormal, UrgencyLow}
	for _, urgency := range urgencies {
		var actions []RotationAction
		for _, a := range plan.Actions {
			if a.Urgency == urgency {
				actions = append(actions, a)
			}
		}
		if len(actions) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("%s (%d):\n", urgency, len(actions)))
		for _, a := range actions {
			b.WriteString(fmt.Sprintf("  %s [%s] — %s\n", a.AgentName, a.KeyType, a.Reason))
		}
		b.WriteString("\n")
	}

	return b.String()
}
