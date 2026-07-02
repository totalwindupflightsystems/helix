package identity

import (
	"strings"
	"testing"
	"time"
)

func TestKeyRotation_ActiveKey_NoRotation(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	policies := DefaultRotationPolicies()
	reg := NewAgentKeyRegistry()

	reg.RegisterKey("agent-1", KeyTypeSSH, "sha256:abc",
		now.AddDate(0, 0, -30), // 30 days ago — well within 90-day max
		time.Time{})

	plan := reg.PlanRotation(policies, now)
	if len(plan.Actions) != 0 {
		t.Errorf("expected 0 actions, got %d", len(plan.Actions))
	}
	if plan.Skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", plan.Skipped)
	}
}

func TestKeyRotation_AgeExceeded_NormalUrgency(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	policies := DefaultRotationPolicies()
	reg := NewAgentKeyRegistry()

	// SSH key 100 days old (max 90 days)
	reg.RegisterKey("agent-1", KeyTypeSSH, "sha256:abc",
		now.AddDate(0, 0, -100), time.Time{})

	plan := reg.PlanRotation(policies, now)
	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(plan.Actions))
	}

	a := plan.Actions[0]
	if a.Reason != RotationAgeExceeded {
		t.Errorf("expected age_exceeded, got %s", a.Reason)
	}
	if a.Urgency != UrgencyNormal {
		t.Errorf("expected normal urgency, got %s", a.Urgency)
	}
}

func TestKeyRotation_DoubleAgeExceeded_HighUrgency(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	policies := DefaultRotationPolicies()
	reg := NewAgentKeyRegistry()

	// SSH key 200 days old (2x the 90-day max)
	reg.RegisterKey("agent-1", KeyTypeSSH, "sha256:abc",
		now.AddDate(0, 0, -200), time.Time{})

	plan := reg.PlanRotation(policies, now)
	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(plan.Actions))
	}

	if plan.Actions[0].Urgency != UrgencyHigh {
		t.Errorf("expected high urgency for 2x age, got %s", plan.Actions[0].Urgency)
	}
}

func TestKeyRotation_DeadKey_ImmediateRotation(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	policies := DefaultRotationPolicies()
	reg := NewAgentKeyRegistry()

	reg.RegisterKey("agent-1", KeyTypeOpenRouter, "sha256:dead",
		now.AddDate(0, 0, -5), time.Time{})
	reg.MarkDead("agent-1", KeyTypeOpenRouter)

	plan := reg.PlanRotation(policies, now)
	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(plan.Actions))
	}

	if plan.Actions[0].Urgency != UrgencyImmediate {
		t.Errorf("expected immediate urgency for dead key, got %s", plan.Actions[0].Urgency)
	}
	if plan.Actions[0].Reason != RotationDeadKey {
		t.Errorf("expected dead_key reason, got %s", plan.Actions[0].Reason)
	}
}

func TestKeyRotation_ExpiredPAT_ImmediateRotation(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	policies := DefaultRotationPolicies()
	reg := NewAgentKeyRegistry()

	// PAT that expired yesterday
	reg.RegisterKey("agent-1", KeyTypePAT, "sha256:pat",
		now.AddDate(0, 0, -30), now.AddDate(0, 0, -1))

	plan := reg.PlanRotation(policies, now)
	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(plan.Actions))
	}
	if plan.Actions[0].Urgency != UrgencyImmediate {
		t.Errorf("expected immediate urgency for expired PAT, got %s", plan.Actions[0].Urgency)
	}
}

func TestKeyRotation_ExpiringPAT_HighUrgency(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	policies := DefaultRotationPolicies()
	reg := NewAgentKeyRegistry()

	// PAT expiring in 3 days (within the 7-day warning window)
	reg.RegisterKey("agent-1", KeyTypePAT, "sha256:pat",
		now.AddDate(0, 0, -27), now.AddDate(0, 0, 3))

	plan := reg.PlanRotation(policies, now)
	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(plan.Actions))
	}
	if plan.Actions[0].Urgency != UrgencyHigh {
		t.Errorf("expected high urgency for expiring PAT, got %s", plan.Actions[0].Urgency)
	}
	if plan.Actions[0].Reason != RotationExpiringSoon {
		t.Errorf("expected expiring_soon, got %s", plan.Actions[0].Reason)
	}
}

func TestKeyRotation_MultipleKeysMixedStates(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	policies := DefaultRotationPolicies()
	reg := NewAgentKeyRegistry()

	// Agent 1: all keys fine
	reg.RegisterKey("agent-1", KeyTypeSSH, "sha256:k1",
		now.AddDate(0, 0, -10), time.Time{})
	reg.RegisterKey("agent-1", KeyTypeOpenRouter, "sha256:k2",
		now.AddDate(0, 0, -10), time.Time{})

	// Agent 2: dead OpenRouter key + old SSH key
	reg.RegisterKey("agent-2", KeyTypeSSH, "sha256:k3",
		now.AddDate(0, 0, -100), time.Time{})
	reg.RegisterKey("agent-2", KeyTypeOpenRouter, "sha256:k4",
		now.AddDate(0, 0, -5), time.Time{})
	reg.MarkDead("agent-2", KeyTypeOpenRouter)

	// Agent 3: expiring PAT
	reg.RegisterKey("agent-3", KeyTypePAT, "sha256:k5",
		now.AddDate(0, 0, -25), now.AddDate(0, 0, 5))

	plan := reg.PlanRotation(policies, now)

	if len(plan.Actions) != 3 {
		t.Fatalf("expected 3 actions, got %d", len(plan.Actions))
	}
	if plan.Skipped != 2 {
		t.Errorf("expected 2 skipped, got %d", plan.Skipped)
	}
	if !plan.HasImmediate() {
		t.Error("expected immediate actions")
	}
	if !plan.HasHigh() {
		t.Error("expected high urgency actions")
	}
}

func TestKeyRotation_MarkRotated_ClearsStatus(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	policies := DefaultRotationPolicies()
	reg := NewAgentKeyRegistry()

	reg.RegisterKey("agent-1", KeyTypeOpenRouter, "sha256:old",
		now.AddDate(0, 0, -60), time.Time{})

	// Before rotation: should need rotation (30-day max)
	plan := reg.PlanRotation(policies, now)
	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action before rotation, got %d", len(plan.Actions))
	}

	// Rotate
	reg.MarkRotated("agent-1", KeyTypeOpenRouter, "sha256:new", now)

	// After rotation: should be clean
	plan = reg.PlanRotation(policies, now)
	if len(plan.Actions) != 0 {
		t.Errorf("expected 0 actions after rotation, got %d", len(plan.Actions))
	}

	// Verify new hash
	info, ok := reg.GetKey("agent-1", KeyTypeOpenRouter)
	if !ok {
		t.Fatal("key not found after rotation")
	}
	if info.KeyHash != "sha256:new" {
		t.Errorf("expected sha256:new, got %s", info.KeyHash)
	}
	if info.Status != KeyStatusActive {
		t.Errorf("expected active status, got %s", info.Status)
	}
}

func TestKeyRotation_NoPolicyForType_Skipped(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	policies := map[KeyType]RotationPolicy{
		KeyTypeSSH: {MaxAge: 90 * 24 * time.Hour},
	}
	reg := NewAgentKeyRegistry()

	// Register a PAT but only have a policy for SSH
	reg.RegisterKey("agent-1", KeyTypePAT, "sha256:pat",
		now.AddDate(0, 0, -365), time.Time{})

	plan := reg.PlanRotation(policies, now)
	if len(plan.Actions) != 0 {
		t.Errorf("expected 0 actions (no policy for PAT), got %d", len(plan.Actions))
	}
	if plan.Skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", plan.Skipped)
	}
}

func TestKeyRotation_CountByUrgency(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	policies := DefaultRotationPolicies()
	reg := NewAgentKeyRegistry()

	// 2 dead keys (immediate)
	reg.RegisterKey("a1", KeyTypeOpenRouter, "h1", now, time.Time{})
	reg.MarkDead("a1", KeyTypeOpenRouter)
	reg.RegisterKey("a2", KeyTypeOpenRouter, "h2", now, time.Time{})
	reg.MarkDead("a2", KeyTypeOpenRouter)

	// 1 expired PAT (immediate)
	reg.RegisterKey("a3", KeyTypePAT, "h3", now.AddDate(0, 0, -30), now.AddDate(0, 0, -1))

	// 1 old SSH (normal)
	reg.RegisterKey("a4", KeyTypeSSH, "h4", now.AddDate(0, 0, -100), time.Time{})

	plan := reg.PlanRotation(policies, now)

	if plan.Count(UrgencyImmediate) != 3 {
		t.Errorf("expected 3 immediate, got %d", plan.Count(UrgencyImmediate))
	}
	if plan.Count(UrgencyNormal) != 1 {
		t.Errorf("expected 1 normal, got %d", plan.Count(UrgencyNormal))
	}
	if plan.Count(UrgencyHigh) != 0 {
		t.Errorf("expected 0 high, got %d", plan.Count(UrgencyHigh))
	}
}

func TestHashKey_Deterministic(t *testing.T) {
	h1 := HashKey("sk-test-key-123")
	h2 := HashKey("sk-test-key-123")

	if h1 != h2 {
		t.Error("same input should produce same hash")
	}
	if h1 == "" {
		t.Error("hash should not be empty")
	}
	if !strings.HasPrefix(h1, "sha256:") {
		t.Errorf("hash should start with sha256:, got %q", h1)
	}
}

func TestHashKey_DifferentInputs(t *testing.T) {
	h1 := HashKey("key-a")
	h2 := HashKey("key-b")

	if h1 == h2 {
		t.Error("different inputs should produce different hashes")
	}
}

func TestVerifyKeyHash_CorrectKey(t *testing.T) {
	rawKey := "sk-my-secret-key"
	hash := HashKey(rawKey)

	if !VerifyKeyHash(rawKey, hash) {
		t.Error("correct key should verify")
	}
}

func TestVerifyKeyHash_WrongKey(t *testing.T) {
	hash := HashKey("correct-key")

	if VerifyKeyHash("wrong-key", hash) {
		t.Error("wrong key should not verify")
	}
}

func TestFormatRotationPlan_NoActions(t *testing.T) {
	plan := &RotationPlan{
		Actions:   nil,
		Generated: time.Now(),
		Skipped:   3,
	}
	output := FormatRotationPlan(plan)
	if output == "" {
		t.Error("expected non-empty output")
	}
}

func TestFormatRotationPlan_WithActions(t *testing.T) {
	plan := &RotationPlan{
		Actions: []RotationAction{
			{AgentName: "agent-1", KeyType: KeyTypeOpenRouter, Reason: RotationDeadKey, Urgency: UrgencyImmediate},
			{AgentName: "agent-2", KeyType: KeyTypeSSH, Reason: RotationAgeExceeded, Urgency: UrgencyNormal},
		},
		Generated: time.Now(),
		Skipped:   1,
	}
	output := FormatRotationPlan(plan)
	if output == "" {
		t.Error("expected non-empty output")
	}
}

func TestDefaultRotationPolicies(t *testing.T) {
	policies := DefaultRotationPolicies()

	if policies[KeyTypeSSH].MaxAge != 90*24*time.Hour {
		t.Errorf("SSH max age should be 90 days, got %v", policies[KeyTypeSSH].MaxAge)
	}
	if policies[KeyTypeOpenRouter].MaxAge != 30*24*time.Hour {
		t.Errorf("OpenRouter max age should be 30 days, got %v", policies[KeyTypeOpenRouter].MaxAge)
	}
	if policies[KeyTypePAT].ExpiryWarning != 7*24*time.Hour {
		t.Errorf("PAT warning should be 7 days, got %v", policies[KeyTypePAT].ExpiryWarning)
	}
}

func TestEvaluateKey_FreshKey_NoRotation(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	info := KeyInfo{
		Type:        KeyTypeSSH,
		CreatedAt:   now.AddDate(0, 0, -1), // 1 day old
		LastRotated: now.AddDate(0, 0, -1),
		Status:      KeyStatusActive,
	}
	policy := RotationPolicy{MaxAge: 90 * 24 * time.Hour, ExpiryWarning: 7 * 24 * time.Hour}

	needs, _, _ := EvaluateKey(info, policy, now)
	if needs {
		t.Error("1-day-old key should not need rotation")
	}
}

func TestEvaluateKey_ExpiringSoon(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	info := KeyInfo{
		Type:        KeyTypePAT,
		CreatedAt:   now.AddDate(0, 0, -25),
		LastRotated: now.AddDate(0, 0, -25),
		ExpiresAt:   now.AddDate(0, 0, 3), // 3 days until expiry
		Status:      KeyStatusActive,
	}
	policy := RotationPolicy{ExpiryWarning: 7 * 24 * time.Hour}

	needs, reason, urgency := EvaluateKey(info, policy, now)
	if !needs {
		t.Error("expiring PAT should need rotation")
	}
	if reason != RotationExpiringSoon {
		t.Errorf("expected expiring_soon, got %s", reason)
	}
	if urgency != UrgencyHigh {
		t.Errorf("expected high urgency, got %s", urgency)
	}
}
