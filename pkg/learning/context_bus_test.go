package learning

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tempDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "helix-learning-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

func TestNewContextBus_Empty(t *testing.T) {
	cb, err := NewContextBus(tempDir(t))
	require.NoError(t, err)

	assert.Empty(t, cb.ListFindings(""))
	assert.Empty(t, cb.ListSubscriptions())
	assert.Zero(t, cb.DailyUsage("agent-1"))
}

func TestPublish_ValidatesInput(t *testing.T) {
	cb, err := NewContextBus(tempDir(t))
	require.NoError(t, err)

	// Missing fromAgentID.
	_, err = cb.Publish("", "", DomainAuth, "finding", nil, PriorityInfo)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "from_agent_id")

	// Invalid domain.
	_, err = cb.Publish("agent-1", "", Domain("invalid"), "finding", nil, PriorityInfo)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid domain")

	// Empty finding.
	_, err = cb.Publish("agent-1", "", DomainAuth, "", nil, PriorityInfo)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "finding text")
}

func TestPublish_And_List(t *testing.T) {
	cb, err := NewContextBus(tempDir(t))
	require.NoError(t, err)

	sf, err := cb.Publish("agent-1", "", DomainAuth, "Session refresh fails at midnight", []string{"pr-123"}, PriorityWarning)
	require.NoError(t, err)
	assert.NotEmpty(t, sf.ID)
	assert.Equal(t, "agent-1", sf.FromAgentID)
	assert.Equal(t, DomainAuth, sf.Domain)

	findings := cb.ListFindings("")
	assert.Len(t, findings, 1)
	assert.Equal(t, sf.ID, findings[0].ID)

	authFindings := cb.ListFindings(DomainAuth)
	assert.Len(t, authFindings, 1)

	dbFindings := cb.ListFindings(DomainDatabase)
	assert.Empty(t, dbFindings)
}

func TestSubscribe_And_Unsubscribe(t *testing.T) {
	cb, err := NewContextBus(tempDir(t))
	require.NoError(t, err)

	// Subscribe.
	err = cb.Subscribe("agent-1", []Domain{DomainAuth, DomainSecurity})
	require.NoError(t, err)

	sub := cb.GetSubscription("agent-1")
	require.NotNil(t, sub)
	assert.Equal(t, "agent-1", sub.AgentID)
	assert.ElementsMatch(t, []Domain{DomainAuth, DomainSecurity}, sub.Domains)

	// Subscribe to additional domain (merge).
	err = cb.Subscribe("agent-1", []Domain{DomainAPI})
	require.NoError(t, err)
	sub = cb.GetSubscription("agent-1")
	assert.ElementsMatch(t, []Domain{DomainAuth, DomainSecurity, DomainAPI}, sub.Domains)

	// Unsubscribe from one domain.
	err = cb.Unsubscribe("agent-1", []Domain{DomainSecurity})
	require.NoError(t, err)
	sub = cb.GetSubscription("agent-1")
	assert.ElementsMatch(t, []Domain{DomainAuth, DomainAPI}, sub.Domains)

	// Unsubscribe from all domains.
	err = cb.Unsubscribe("agent-1", nil)
	require.NoError(t, err)
	assert.Nil(t, cb.GetSubscription("agent-1"))
}

func TestUnsubscribe_Nonexistent(t *testing.T) {
	cb, err := NewContextBus(tempDir(t))
	require.NoError(t, err)

	err = cb.Unsubscribe("nonexistent", []Domain{DomainAuth})
	assert.Error(t, err)
}

func TestInbox_DomainMatching(t *testing.T) {
	cb, err := NewContextBus(tempDir(t))
	require.NoError(t, err)

	// Setup: agent-1 subscribes to auth, agent-2 subscribes to database.
	require.NoError(t, cb.Subscribe("agent-1", []Domain{DomainAuth}))
	require.NoError(t, cb.Subscribe("agent-2", []Domain{DomainDatabase}))

	// Publish to auth domain.
	_, err = cb.Publish("agent-0", "", DomainAuth, "Auth finding", nil, PriorityInfo)
	require.NoError(t, err)

	// Publish to database domain.
	_, err = cb.Publish("agent-0", "", DomainDatabase, "DB finding", nil, PriorityInfo)
	require.NoError(t, err)

	// agent-1 should see auth finding but not db finding.
	inbox1, err := cb.GetInbox("agent-1", "veteran", false)
	require.NoError(t, err)
	assert.Len(t, inbox1, 1)
	assert.Equal(t, "Auth finding", inbox1[0].Finding)

	// agent-2 should see db finding but not auth finding.
	inbox2, err := cb.GetInbox("agent-2", "veteran", false)
	require.NoError(t, err)
	assert.Len(t, inbox2, 1)
	assert.Equal(t, "DB finding", inbox2[0].Finding)
}

func TestInbox_DirectAddressing(t *testing.T) {
	cb, err := NewContextBus(tempDir(t))
	require.NoError(t, err)

	// Subscribe agent-1 to auth.
	require.NoError(t, cb.Subscribe("agent-1", []Domain{DomainAuth}))

	// Publish directly to agent-2 (not subscribed to auth).
	_, err = cb.Publish("agent-0", "agent-2", DomainAuth, "Direct finding", nil, PriorityInfo)
	require.NoError(t, err)

	// agent-1 should NOT see it (it's directed at agent-2).
	inbox1, err := cb.GetInbox("agent-1", "veteran", false)
	require.NoError(t, err)
	assert.Empty(t, inbox1)

	// agent-2 SHOULD see it (direct target, regardless of subscription).
	inbox2, err := cb.GetInbox("agent-2", "veteran", false)
	require.NoError(t, err)
	assert.Len(t, inbox2, 1)
	assert.Equal(t, "Direct finding", inbox2[0].Finding)
}

func TestInbox_BudgetEnforcement(t *testing.T) {
	cb, err := NewContextBus(tempDir(t))
	require.NoError(t, err)

	require.NoError(t, cb.Subscribe("agent-1", []Domain{DomainAuth}))

	// Publish 15 findings (more than provisional budget of 10).
	for i := 0; i < 15; i++ {
		_, err = cb.Publish("agent-0", "", DomainAuth, "Finding", nil, PriorityInfo)
		require.NoError(t, err)
	}

	// Provisional tier: should only get 10.
	inbox, err := cb.GetInbox("agent-1", "provisional", false)
	require.NoError(t, err)
	assert.Len(t, inbox, 10)
}

func TestInbox_CriticalBypassesBudget(t *testing.T) {
	cb, err := NewContextBus(tempDir(t))
	require.NoError(t, err)

	require.NoError(t, cb.Subscribe("agent-1", []Domain{DomainAuth}))

	// Fill budget with 12 info findings (over provisional budget of 10).
	for i := 0; i < 12; i++ {
		_, err = cb.Publish("agent-0", "", DomainAuth, "Info finding", nil, PriorityInfo)
		require.NoError(t, err)
	}

	// Add 3 critical findings.
	for i := 0; i < 3; i++ {
		_, err = cb.Publish("agent-0", "", DomainAuth, "Critical finding", nil, PriorityCritical)
		require.NoError(t, err)
	}

	// Provisional: should get 10 info + 3 critical (critical bypasses budget).
	inbox, err := cb.GetInbox("agent-1", "provisional", false)
	require.NoError(t, err)
	assert.Len(t, inbox, 13)

	// Count critical vs info.
	critical := 0
	for _, f := range inbox {
		if f.Priority == PriorityCritical {
			critical++
		}
	}
	assert.Equal(t, 3, critical, "all 3 critical findings should be included")
}

func TestInbox_SortOrder(t *testing.T) {
	cb, err := NewContextBus(tempDir(t))
	require.NoError(t, err)

	require.NoError(t, cb.Subscribe("agent-1", []Domain{DomainAuth}))

	// Publish in reverse priority order (info first).
	infoF, _ := cb.Publish("agent-0", "", DomainAuth, "info", nil, PriorityInfo)
	warnF, _ := cb.Publish("agent-0", "", DomainAuth, "warning", nil, PriorityWarning)
	critF, _ := cb.Publish("agent-0", "", DomainAuth, "critical", nil, PriorityCritical)

	// Also publish another info (older).
	info2, _ := cb.Publish("agent-0", "", DomainAuth, "info2", nil, PriorityInfo)
	time.Sleep(10 * time.Millisecond)
	info3, _ := cb.Publish("agent-0", "", DomainAuth, "info3", nil, PriorityInfo)

	inbox, err := cb.GetInbox("agent-1", "veteran", false)
	require.NoError(t, err)

	// Expected order: critical, warning, info3 (newest), info2, info (oldest).
	assert.Equal(t, critF.ID, inbox[0].ID, "critical should be first")
	assert.Equal(t, warnF.ID, inbox[1].ID, "warning should be second")
	assert.Equal(t, info3.ID, inbox[2].ID, "newer info should come before older info")
	assert.Equal(t, info2.ID, inbox[3].ID)
	assert.Equal(t, infoF.ID, inbox[4].ID)
}

func TestInbox_UnreadOnly(t *testing.T) {
	cb, err := NewContextBus(tempDir(t))
	require.NoError(t, err)

	require.NoError(t, cb.Subscribe("agent-1", []Domain{DomainAuth}))

	_, err = cb.Publish("agent-0", "", DomainAuth, "finding", nil, PriorityInfo)
	require.NoError(t, err)

	// First read: should get the finding.
	inbox, err := cb.GetInbox("agent-1", "veteran", true)
	require.NoError(t, err)
	assert.Len(t, inbox, 1)

	// Second read with unreadOnly: should be empty.
	inbox, err = cb.GetInbox("agent-1", "veteran", true)
	require.NoError(t, err)
	assert.Empty(t, inbox)

	// Without unreadOnly: should still get it (consumed but not expired).
	inbox, err = cb.GetInbox("agent-1", "veteran", false)
	require.NoError(t, err)
	assert.Len(t, inbox, 1)
	assert.True(t, inbox[0].Consumed)
}

func TestInbox_Expiry(t *testing.T) {
	cb, err := NewContextBus(tempDir(t))
	require.NoError(t, err)

	require.NoError(t, cb.Subscribe("agent-1", []Domain{DomainAuth}))

	// Manually insert an old finding.
	old := SharedFinding{
		ID:          NewFindingID(),
		FromAgentID: "old-agent",
		Domain:      DomainAuth,
		Finding:     "expired finding",
		Priority:    PriorityInfo,
		Timestamp:   time.Now().Add(-DefaultFindingRetention - time.Hour),
	}
	cb.mu.Lock()
	cb.findings = append(cb.findings, old)
	cb.mu.Unlock()

	inbox, err := cb.GetInbox("agent-1", "veteran", false)
	require.NoError(t, err)
	assert.Empty(t, inbox, "expired finding should not appear")
}

func TestDailyBudget(t *testing.T) {
	assert.Equal(t, 10, DailyBudget("provisional"))
	assert.Equal(t, 20, DailyBudget("observed"))
	assert.Equal(t, 35, DailyBudget("trusted"))
	assert.Equal(t, 50, DailyBudget("veteran"))
	assert.Equal(t, 10, DailyBudget("unknown"))
}

func TestIsValidDomain(t *testing.T) {
	assert.True(t, IsValidDomain(DomainAuth))
	assert.True(t, IsValidDomain(DomainDocs))
	assert.False(t, IsValidDomain(Domain("invalid")))
	assert.False(t, IsValidDomain(Domain("")))
}

func TestPersistence_RoundTrip(t *testing.T) {
	dir := tempDir(t)
	cb, err := NewContextBus(dir)
	require.NoError(t, err)

	// Publish, subscribe.
	_, err = cb.Publish("agent-1", "", DomainAuth, "persisted finding", nil, PriorityWarning)
	require.NoError(t, err)
	require.NoError(t, cb.Subscribe("agent-1", []Domain{DomainAuth}))

	// Reload from disk.
	cb2, err := NewContextBus(dir)
	require.NoError(t, err)

	findings := cb2.ListFindings("")
	assert.Len(t, findings, 1)
	assert.Equal(t, "persisted finding", findings[0].Finding)

	sub := cb2.GetSubscription("agent-1")
	require.NotNil(t, sub)
	assert.ElementsMatch(t, []Domain{DomainAuth}, sub.Domains)
}

func TestValidateInputs(t *testing.T) {
	cb, err := NewContextBus(tempDir(t))
	require.NoError(t, err)

	// Subscribe: empty agent.
	assert.Error(t, cb.Subscribe("", []Domain{DomainAuth}))
	// Subscribe: empty domains.
	assert.Error(t, cb.Subscribe("agent-1", nil))
	// Subscribe: invalid domain.
	assert.Error(t, cb.Subscribe("agent-1", []Domain{Domain("bad")}))
	// Unsubscribe: empty agent.
	assert.Error(t, cb.Unsubscribe("", []Domain{DomainAuth}))
}

func TestFindingsFile(t *testing.T) {
	dir := tempDir(t)
	cb, err := NewContextBus(dir)
	require.NoError(t, err)

	_, err = cb.Publish("agent-1", "", DomainAuth, "test", nil, PriorityInfo)
	require.NoError(t, err)

	// Check file exists and has content.
	fp := filepath.Join(dir, "findings.jsonl")
	data, err := os.ReadFile(fp)
	require.NoError(t, err)
	assert.Contains(t, string(data), "test")
	assert.Contains(t, string(data), "agent-1")
}

func TestSubscriptionsFile(t *testing.T) {
	dir := tempDir(t)
	cb, err := NewContextBus(dir)
	require.NoError(t, err)

	require.NoError(t, cb.Subscribe("agent-1", []Domain{DomainAuth, DomainSecurity}))

	fp := filepath.Join(dir, "subscriptions.json")
	data, err := os.ReadFile(fp)
	require.NoError(t, err)
	assert.Contains(t, string(data), "agent-1")
	assert.Contains(t, string(data), "auth")
	assert.Contains(t, string(data), "security")
}
