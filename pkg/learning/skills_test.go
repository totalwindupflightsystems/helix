package learning

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tempStore(t *testing.T) (*FileSkillStore, *SkillRegistry) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "skills.json")
	store, err := NewFileSkillStore(path)
	require.NoError(t, err)
	return store, NewSkillRegistry(store)
}

func sampleSkill(name, domain string) Skill {
	return Skill{
		Name:          name,
		Domain:        domain,
		Description:   "test skill",
		Prompt:        "do the thing carefully",
		AuthorAgentID: "agent-1",
		EvidenceTags:  []string{"PR-1"},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Publishing gates
// ─────────────────────────────────────────────────────────────────────────────

func TestPublish_TrustGateRejectsLowTrust(t *testing.T) {
	_, reg := tempStore(t)
	err := reg.Publish("agent-low", 0.50, 10, sampleSkill("s", "database"))
	require.Error(t, err)
	pge, ok := err.(*PublishGateError)
	require.True(t, ok)
	assert.Equal(t, "TRUST_GATE", pge.Code)
}

func TestPublish_DomainGateRejectsLowExpertise(t *testing.T) {
	_, reg := tempStore(t)
	err := reg.Publish("agent-1", 0.80, 3, sampleSkill("s", "database"))
	require.Error(t, err)
	pge, ok := err.(*PublishGateError)
	require.True(t, ok)
	assert.Equal(t, "DOMAIN_GATE", pge.Code)
}

func TestPublish_SuccessAndPersistence(t *testing.T) {
	store, reg := tempStore(t)
	err := reg.Publish("agent-1", 0.85, 10, sampleSkill("db-migration", "database"))
	require.NoError(t, err)

	// Persist to disk as JSON array.
	data, err := os.ReadFile(store.Path())
	require.NoError(t, err)
	var list []Skill
	require.NoError(t, json.Unmarshal(data, &list))
	require.Len(t, list, 1)
	assert.Equal(t, "db-migration", list[0].Name)
	assert.Equal(t, DefaultSkillTrustWeight, list[0].TrustWeight)
	assert.Equal(t, "0.1.0", list[0].Version)
	assert.Equal(t, "agent-1", list[0].AuthorAgentID)
	assert.NotEmpty(t, list[0].ID)

	// Reload from disk.
	store2, err := NewFileSkillStore(store.Path())
	require.NoError(t, err)
	got, err := store2.Get(list[0].ID)
	require.NoError(t, err)
	assert.Equal(t, "db-migration", got.Name)
}

func TestPublish_RequiresNameDomainPrompt(t *testing.T) {
	_, reg := tempStore(t)
	err := reg.Publish("a", 0.9, 10, Skill{Domain: "api", Prompt: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name")

	err = reg.Publish("a", 0.9, 10, Skill{Name: "n", Prompt: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "domain")

	err = reg.Publish("a", 0.9, 10, Skill{Name: "n", Domain: "api"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt")
}

// ─────────────────────────────────────────────────────────────────────────────
// Load / List / Get
// ─────────────────────────────────────────────────────────────────────────────

func TestLoad_TopNByTrustWeight(t *testing.T) {
	store, reg := tempStore(t)
	// Seed via store (bypass gate for weight control).
	now := time.Now().UTC()
	require.NoError(t, store.Publish(Skill{ID: "low", Name: "low", Domain: "go", TrustWeight: 0.3, CreatedAt: now, UpdatedAt: now}))
	require.NoError(t, store.Publish(Skill{ID: "high", Name: "high", Domain: "go", TrustWeight: 0.9, CreatedAt: now, UpdatedAt: now}))
	require.NoError(t, store.Publish(Skill{ID: "mid", Name: "mid", Domain: "go", TrustWeight: 0.6, CreatedAt: now, UpdatedAt: now}))
	require.NoError(t, store.Publish(Skill{ID: "other", Name: "other", Domain: "api", TrustWeight: 1.0, CreatedAt: now, UpdatedAt: now}))

	skills, err := reg.Load("go", 0)
	require.NoError(t, err)
	require.Len(t, skills, 3)
	assert.Equal(t, "high", skills[0].ID)
	assert.Equal(t, "mid", skills[1].ID)
	assert.Equal(t, "low", skills[2].ID)

	limited, err := reg.Load("go", 2)
	require.NoError(t, err)
	require.Len(t, limited, 2)
	assert.Equal(t, "high", limited[0].ID)
}

func TestLoad_SkipsDeprecated(t *testing.T) {
	store, reg := tempStore(t)
	now := time.Now().UTC()
	require.NoError(t, store.Publish(Skill{ID: "active", Name: "a", Domain: "go", TrustWeight: 0.8, CreatedAt: now, UpdatedAt: now}))
	require.NoError(t, store.Publish(Skill{ID: "gone", Name: "g", Domain: "go", TrustWeight: 0.5, Deprecated: true, CreatedAt: now, UpdatedAt: now}))

	skills, err := reg.Load("go", 0)
	require.NoError(t, err)
	require.Len(t, skills, 1)
	assert.Equal(t, "active", skills[0].ID)
}

func TestList_FiltersDomainAndMinTrust(t *testing.T) {
	store, reg := tempStore(t)
	now := time.Now().UTC()
	require.NoError(t, store.Publish(Skill{ID: "a", Name: "a", Domain: "go", TrustWeight: 0.9, CreatedAt: now, UpdatedAt: now}))
	require.NoError(t, store.Publish(Skill{ID: "b", Name: "b", Domain: "go", TrustWeight: 0.4, CreatedAt: now, UpdatedAt: now}))
	require.NoError(t, store.Publish(Skill{ID: "c", Name: "c", Domain: "api", TrustWeight: 0.95, CreatedAt: now, UpdatedAt: now}))

	list, err := reg.List("go", 0.7)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "a", list[0].ID)

	all, err := reg.List("", 0)
	require.NoError(t, err)
	require.Len(t, all, 3)
}

func TestGet(t *testing.T) {
	store, reg := tempStore(t)
	now := time.Now().UTC()
	require.NoError(t, store.Publish(Skill{ID: "s1", Name: "S", Domain: "go", TrustWeight: 0.5, CreatedAt: now, UpdatedAt: now}))

	got, err := reg.Get("s1")
	require.NoError(t, err)
	assert.Equal(t, "S", got.Name)

	_, err = reg.Get("missing")
	require.Error(t, err)
}

// ─────────────────────────────────────────────────────────────────────────────
// Effectiveness tracking
// ─────────────────────────────────────────────────────────────────────────────

func TestRecordOutcome_SuccessIncreasesWeight(t *testing.T) {
	store, reg := tempStore(t)
	now := time.Now().UTC()
	require.NoError(t, store.Publish(Skill{ID: "s1", Name: "s", Domain: "go", TrustWeight: 0.7, CreatedAt: now, UpdatedAt: now}))

	require.NoError(t, reg.RecordOutcome("s1", true))
	got, err := reg.Get("s1")
	require.NoError(t, err)
	assert.InDelta(t, 0.71, got.TrustWeight, 1e-9)
	assert.Equal(t, 1, got.UsageCount)
	assert.Equal(t, 1, got.SuccessCount)
	assert.InDelta(t, 1.0, got.SuccessRate, 1e-9)
}

func TestRecordOutcome_SuccessCappedAtOne(t *testing.T) {
	store, reg := tempStore(t)
	now := time.Now().UTC()
	require.NoError(t, store.Publish(Skill{ID: "s1", Name: "s", Domain: "go", TrustWeight: 0.99, CreatedAt: now, UpdatedAt: now}))

	require.NoError(t, reg.RecordOutcome("s1", true))
	got, _ := reg.Get("s1")
	assert.Equal(t, 1.0, got.TrustWeight)
}

func TestRecordOutcome_FailureDecreasesWeight(t *testing.T) {
	store, reg := tempStore(t)
	now := time.Now().UTC()
	require.NoError(t, store.Publish(Skill{ID: "s1", Name: "s", Domain: "go", TrustWeight: 0.7, CreatedAt: now, UpdatedAt: now}))

	require.NoError(t, reg.RecordOutcome("s1", false))
	got, err := reg.Get("s1")
	require.NoError(t, err)
	assert.InDelta(t, 0.65, got.TrustWeight, 1e-9)
	assert.Equal(t, 1, got.FailureCount)
}

func TestRecordOutcome_AutoDeprecatesBelowThreshold(t *testing.T) {
	store, reg := tempStore(t)
	now := time.Now().UTC()
	require.NoError(t, store.Publish(Skill{ID: "s1", Name: "s", Domain: "go", TrustWeight: 0.32, CreatedAt: now, UpdatedAt: now}))

	require.NoError(t, reg.RecordOutcome("s1", false))
	got, err := reg.Get("s1")
	require.NoError(t, err)
	// 0.32 - 0.05 = 0.27 < 0.3 → auto-deprecated
	assert.True(t, got.Deprecated)
	assert.InDelta(t, 0.27, got.TrustWeight, 1e-9)
	assert.Contains(t, got.DeprecationReason, "auto-deprecated")

	// Deprecated skills no longer Load.
	loaded, err := reg.Load("go", 0)
	require.NoError(t, err)
	assert.Empty(t, loaded)
}

func TestDeprecate_Manual(t *testing.T) {
	store, reg := tempStore(t)
	now := time.Now().UTC()
	require.NoError(t, store.Publish(Skill{ID: "s1", Name: "s", Domain: "go", TrustWeight: 0.9, CreatedAt: now, UpdatedAt: now}))

	require.NoError(t, reg.Deprecate("s1", "superseded by v2"))
	got, err := reg.Get("s1")
	require.NoError(t, err)
	assert.True(t, got.Deprecated)
	assert.Equal(t, "superseded by v2", got.DeprecationReason)
}

func TestSuccessRate_MixedOutcomes(t *testing.T) {
	store, reg := tempStore(t)
	now := time.Now().UTC()
	require.NoError(t, store.Publish(Skill{ID: "s1", Name: "s", Domain: "go", TrustWeight: 0.8, CreatedAt: now, UpdatedAt: now}))

	require.NoError(t, reg.RecordOutcome("s1", true))
	require.NoError(t, reg.RecordOutcome("s1", true))
	require.NoError(t, reg.RecordOutcome("s1", true))
	require.NoError(t, reg.RecordOutcome("s1", false))

	got, err := reg.Get("s1")
	require.NoError(t, err)
	assert.InDelta(t, 0.75, got.SuccessRate, 1e-9)
	assert.Equal(t, 3, got.SuccessCount)
	assert.Equal(t, 1, got.FailureCount)
	assert.Equal(t, 4, got.UsageCount)
}

func TestDefaultSkillsPath(t *testing.T) {
	p := DefaultSkillsPath()
	assert.Contains(t, p, "skills.json")
	assert.Contains(t, p, ".helix")
}

func TestPublish_OtherAgentCanLoad(t *testing.T) {
	// Acceptance: high-trust agent publishes → other agents load it.
	_, reg := tempStore(t)
	err := reg.Publish("veteran-agent", 0.90, 12, Skill{
		Name:        "auth-patterns",
		Domain:      "auth",
		Description: "safe JWT refresh",
		Prompt:      "Always validate iss/aud...",
	})
	require.NoError(t, err)

	loaded, err := reg.Load("auth", 5)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "auth-patterns", loaded[0].Name)
	assert.Equal(t, "veteran-agent", loaded[0].AuthorAgentID)
}
