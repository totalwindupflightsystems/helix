//go:build integration

package integration

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationSuite verifies the full integration test suite lifecycle.
func TestIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if getEnv("GOAWAY", "0") == "1" {
		t.Skip("skipping integration test (GOAWAY=1)")
	}

	suite := NewIntegrationTestSuite()
	require.NotNil(t, suite, "suite should be constructed")
	require.NotNil(t, suite.Forgejo, "Forgejo client should be set")
	require.NotNil(t, suite.Chimera, "Chimera client should be set")

	// Setup
	err := suite.Setup(t)
	require.NoError(t, err, "setup should succeed — Forgejo and Chimera must be running")

	// Run full loop
	suite.TestFullLoop(t)

	// Teardown
	suite.Teardown(t)
}

// TestForgejoHealth verifies that Forgejo is reachable and responds.
func TestForgejoHealth(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (short mode)")
	}
	if getEnv("GOAWAY", "0") == "1" {
		t.Skip("skipping integration test (GOAWAY=1)")
	}

	fc, err := NewForgejoClient(
		getEnv("FORGEJO_URL", DefaultForgejoURL),
		getEnv("FORGEJO_ADMIN_USER", DefaultAdminUser),
		getEnv("FORGEJO_ADMIN_PASSWORD", DefaultAdminPassword),
	)
	require.NoError(t, err, "Forgejo client construction")

	// Try to list admin users — if Forgejo is down, this fails.
	_, err = fc.GetAccount(t, "helio")
	if err != nil {
		t.Skipf("Forgejo not available: %v", err)
	}
	t.Logf("[OK] Forgejo health check passed")
}

// TestChimeraHealth verifies that Chimera /v1/health returns 200.
func TestChimeraHealth(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (short mode)")
	}
	if getEnv("GOAWAY", "0") == "1" {
		t.Skip("skipping integration test (GOAWAY=1)")
	}

	cc, err := NewChimeraClient(getEnv("CHIMERA_URL", DefaultChimeraURL))
	require.NoError(t, err, "Chimera client construction")

	status, err := cc.Health(t)
	if err != nil {
		t.Skipf("Chimera not available: %v", err)
	}
	assert.Equal(t, http.StatusOK, status, "Chimera health should return 200")
	t.Logf("[OK] Chimera health check passed (HTTP %d)", status)
}

// TestAgentProvisioning provisions an agent, verifies it exists, then deprovisions.
func TestAgentProvisioning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (short mode)")
	}
	if getEnv("GOAWAY", "0") == "1" {
		t.Skip("skipping integration test (GOAWAY=1)")
	}

	fc, err := NewForgejoClient(
		getEnv("FORGEJO_URL", DefaultForgejoURL),
		getEnv("FORGEJO_ADMIN_USER", DefaultAdminUser),
		getEnv("FORGEJO_ADMIN_PASSWORD", DefaultAdminPassword),
	)
	require.NoError(t, err, "Forgejo client construction")

	testAgent := "helix-provision-test-agent"
	tempPass := "ProvisionTestPass1234567890"

	// Cleanup before test
	_ = fc.DeleteUser(t, testAgent)

	// Create
	createReq := CreateUserRequest{
		Username:           testAgent,
		LoginName:          testAgent,
		FullName:           "Provision Test Agent",
		Email:              testAgent + "@helix-agents.local",
		Password:           tempPass,
		MustChangePassword: true,
		SendNotify:         false,
		SourceID:           0,
		Visibility:         "limited",
	}
	acct, err := fc.CreateUser(t, createReq)
	require.NoError(t, err, "creating test agent")
	assert.Equal(t, testAgent, acct.Login)
	t.Logf("[OK] Agent %s created (ID: %d)", acct.Login, acct.ID)

	// Verify
	acct2, err := fc.GetAccount(t, testAgent)
	require.NoError(t, err, "verifying test agent exists")
	require.NotNil(t, acct2, "agent should exist")
	assert.Equal(t, testAgent, strings.ToLower(acct2.Login))
	t.Logf("[OK] Agent %s verified", testAgent)

	// Deprovision
	err = fc.DeleteUser(t, testAgent)
	require.NoError(t, err, "deprovisioning test agent")
	t.Logf("[OK] Agent %s deprovisioned", testAgent)

	// Verify deleted
	acct3, _ := fc.GetAccount(t, testAgent)
	assert.Nil(t, acct3, "agent should not exist after deprovisioning")
	t.Logf("[OK] Agent %s no longer in Forgejo", testAgent)
}

// TestCostEstimation calls the Chimera estimate endpoint.
func TestCostEstimation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (short mode)")
	}
	if getEnv("GOAWAY", "0") == "1" {
		t.Skip("skipping integration test (GOAWAY=1)")
	}

	cc, err := NewChimeraClient(getEnv("CHIMERA_URL", DefaultChimeraURL))
	require.NoError(t, err, "Chimera client construction")

	result, err := cc.Estimate(t, "Write a Go function that implements a binary search tree with insert, search, and delete methods")
	if err != nil {
		t.Skipf("Chimera estimate not available: %v", err)
	}
	assert.NotNil(t, result, "estimate should return a result")
	t.Logf("[OK] Cost estimate returned: %v", result)
}

// TestDispatcherDecomposition decomposes a spec file into tasks.
func TestDispatcherDecomposition(t *testing.T) {
	specPath := "testdata/task-spec.md"
	tasks, err := decomposeSpec(specPath)
	require.NoError(t, err, "decomposing spec")
	assert.NotEmpty(t, tasks, "should produce at least one task")

	for i, task := range tasks {
		assert.NotEmpty(t, task.ID, "task should have an ID")
		assert.NotEmpty(t, task.Description, "task should have a description")
		assert.Greater(t, task.Priority, 0, "task should have a positive priority")
		t.Logf("[OK] Task %d: %s (priority: %d)", i+1, task.Description, task.Priority)
	}
}

// TestPromptAttestation attests a prompt file and verifies the hash.
func TestPromptAttestation(t *testing.T) {
	promptPath := "testdata/prompt.md"
	att, err := attestPrompt(promptPath)
	require.NoError(t, err, "attesting prompt")
	assert.NotEmpty(t, att.Hash, "attested prompt should have a hash")
	assert.Contains(t, att.Hash, "sha256:", "hash should be prefixed with sha256:")
	assert.Equal(t, "attested", att.Status, "attested prompt should have attested status")
	t.Logf("[OK] Prompt attested (hash: %s)", att.Hash)
}

// TestMarketplaceSearch searches the marketplace for agents.
func TestMarketplaceSearch(t *testing.T) {
	agents := searchMarketplace(t)
	// In integration mode, we expect at least one agent
	if len(agents) == 0 {
		t.Log("[WARN] No agents in marketplace index (this is OK for local testing)")
	}
	for _, agent := range agents {
		assert.NotEmpty(t, agent.Name, "agent should have a name")
		t.Logf("[OK] Agent: %s (reputation: %.1f)", agent.Name, agent.Reputation)
	}
}

// TestSSHKeyGeneration verifies that test SSH key generation works.
func TestSSHKeyGeneration(t *testing.T) {
	pubKey, err := generateTestSSHKey(t)
	require.NoError(t, err, "generating SSH key")
	assert.Contains(t, pubKey, "ssh-ed25519", "key should be ED25519 format")
	assert.Contains(t, pubKey, "helix-integration-test", "key should have comment")
	t.Logf("[OK] SSH key generated: %s...", pubKey[:40])
}

// TestDecomposeSpecEdgeCases tests edge cases in spec decomposition.
func TestDecomposeSpecEdgeCases(t *testing.T) {
	// Test with non-existent file
	_, err := decomposeSpec("/nonexistent/spec.md")
	assert.Error(t, err, "should error on non-existent file")
	assert.Contains(t, err.Error(), "cannot read spec")

	// Test with empty content (no phases)
	tmpFile := t.TempDir() + "/empty.md"
	err = os.WriteFile(tmpFile, []byte("# Just a heading\n\nNo phases here.\n"), 0644)
	require.NoError(t, err)
	_, err = decomposeSpec(tmpFile)
	assert.Error(t, err, "should error when no tasks found")
	assert.Contains(t, err.Error(), "no tasks found")

	// Test with valid content
	validSpec := `## Phase 1: Setup

Create the database schema and run migrations.

## Phase 2: Implementation

Implement the API endpoints.

## Phase 3: Testing

Write unit and integration tests.
`
	tmpFile2 := t.TempDir() + "/valid.md"
	err = os.WriteFile(tmpFile2, []byte(validSpec), 0644)
	require.NoError(t, err)
	tasks, err := decomposeSpec(tmpFile2)
	require.NoError(t, err, "valid spec should decompose")
	assert.Equal(t, 3, len(tasks), "should produce 3 tasks")
	assert.Equal(t, "Phase 1: Setup", tasks[0].Description)
	assert.Equal(t, 1, tasks[0].Priority)
	assert.Equal(t, "Phase 2: Implementation", tasks[1].Description)
	assert.Equal(t, 2, tasks[1].Priority)
	assert.Equal(t, "Phase 3: Testing", tasks[2].Description)
	assert.Equal(t, 3, tasks[2].Priority)
}
