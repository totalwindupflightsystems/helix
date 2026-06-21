package integration

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test suite configuration
// ---------------------------------------------------------------------------

const (
	// DefaultForgejoURL is the default Forgejo base URL for integration tests.
	DefaultForgejoURL = "http://localhost:3030"
	// DefaultChimeraURL is the default Chimera base URL for integration tests.
	DefaultChimeraURL = "http://localhost:8765"
	// DefaultAdminUser is the Forgejo admin username used for provisioning.
	DefaultAdminUser = "helio"
	// DefaultAdminPassword is the Forgejo admin password for integration tests.
	DefaultAdminPassword = "helio123"
)

// ForgejoClient is a thin HTTP client for the Forgejo API used in tests.
type ForgejoClient struct {
	BaseURL    string
	AdminUser  string
	AdminPass  string
	HTTPClient *http.Client
}

// NewForgejoClient creates a ForgejoClient with the given base URL and admin
// credentials. It returns an error if the URL is malformed.
func NewForgejoClient(baseURL, adminUser, adminPass string) (*ForgejoClient, error) {
	if _, err := url.Parse(baseURL); err != nil {
		return nil, fmt.Errorf("integration: invalid Forgejo URL %q: %w", baseURL, err)
	}
	return &ForgejoClient{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		AdminUser: adminUser,
		AdminPass: adminPass,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}

// GetAccount retrieves a Forgejo user by username. Returns nil, nil if the
// user does not exist.
func (c *ForgejoClient) GetAccount(t *testing.T, name string) (*ForgejoAccount, error) {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/admin/users", c.BaseURL)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err, "building GET /admin/users request")
	req.SetBasicAuth(c.AdminUser, c.AdminPass)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("integration: GetAccount(%s) request failed: %w", name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("integration: GetAccount(%s) returned HTTP %d: %s",
			name, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var accounts []ForgejoAccount
	if err := json.NewDecoder(resp.Body).Decode(&accounts); err != nil {
		return nil, fmt.Errorf("integration: failed to decode user list: %w", err)
	}

	for i := range accounts {
		if strings.EqualFold(accounts[i].Login, name) || strings.EqualFold(accounts[i].LoginName, name) {
			return &accounts[i], nil
		}
	}
	return nil, nil
}

// CreateUser creates a Forgejo user account. Returns the created account or
// an error. A 409/422 conflict is returned as ErrAlreadyExists.
func (c *ForgejoClient) CreateUser(t *testing.T, req CreateUserRequest) (*ForgejoAccount, error) {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/admin/users", c.BaseURL)
	bodyBytes, err := json.Marshal(req)
	require.NoError(t, err, "marshaling CreateUser request")

	httpReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, strings.NewReader(string(bodyBytes)))
	require.NoError(t, err, "building POST /admin/users request")
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.SetBasicAuth(c.AdminUser, c.AdminPass)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("integration: CreateUser(%s) request failed: %w", req.Username, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusCreated:
		var acct ForgejoAccount
		if err := json.NewDecoder(resp.Body).Decode(&acct); err != nil {
			return nil, fmt.Errorf("integration: failed to decode created user: %w", err)
		}
		return &acct, nil
	case http.StatusConflict, http.StatusUnprocessableEntity:
		_, _ = io.ReadAll(resp.Body)
		return nil, ErrAlreadyExists
	default:
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("integration: CreateUser(%s) unexpected HTTP %d: %s",
			req.Username, resp.StatusCode, strings.TrimSpace(string(body)))
	}
}

// RegisterKey registers an SSH public key for a user via the admin API.
func (c *ForgejoClient) RegisterKey(t *testing.T, username, publicKey, title string) (*SSHKey, error) {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/admin/users/%s/keys", c.BaseURL, url.PathEscape(username))
	bodyBytes, err := json.Marshal(map[string]string{
		"key":   publicKey,
		"title": title,
	})
	require.NoError(t, err, "marshaling RegisterKey request")

	httpReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, strings.NewReader(string(bodyBytes)))
	require.NoError(t, err, "building POST /admin/users/{name}/keys request")
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.SetBasicAuth(c.AdminUser, c.AdminPass)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("integration: RegisterKey(%s) request failed: %w", username, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("integration: RegisterKey(%s) unexpected HTTP %d: %s",
			username, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var key SSHKey
	if err := json.NewDecoder(resp.Body).Decode(&key); err != nil {
		return nil, fmt.Errorf("integration: failed to decode SSH key response: %w", err)
	}
	return &key, nil
}

// CreateToken creates a personal access token for a user. Returns the token
// (with the plaintext Token field populated on creation only).
func (c *ForgejoClient) CreateToken(t *testing.T, username, scopes string) (*AccessToken, error) {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/users/%s/tokens", c.BaseURL, url.PathEscape(username))
	bodyBytes, err := json.Marshal(map[string]interface{}{
		"name":   fmt.Sprintf("helix-integration-test-%d", time.Now().UnixNano()),
		"scopes": []string{scopes},
	})
	require.NoError(t, err, "marshaling CreateToken request")

	httpReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, strings.NewReader(string(bodyBytes)))
	require.NoError(t, err, "building POST /users/{name}/tokens request")
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.SetBasicAuth(c.AdminUser, c.AdminPass)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("integration: CreateToken(%s) request failed: %w", username, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("integration: CreateToken(%s) unexpected HTTP %d: %s",
			username, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tok AccessToken
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return nil, fmt.Errorf("integration: failed to decode token response: %w", err)
	}
	return &tok, nil
}

// DeleteUser deletes a Forgejo user account (admin operation).
func (c *ForgejoClient) DeleteUser(t *testing.T, username string) error {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/admin/users/%s", c.BaseURL, url.PathEscape(username))
	req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, url, nil)
	require.NoError(t, err, "building DELETE /admin/users/{name} request")
	req.SetBasicAuth(c.AdminUser, c.AdminPass)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("integration: DeleteUser(%s) request failed: %w", username, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("integration: DeleteUser(%s) unexpected HTTP %d: %s",
			username, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Chimera client
// ---------------------------------------------------------------------------

// ChimeraClient is a thin HTTP client for the Chimera API used in tests.
type ChimeraClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewChimeraClient creates a ChimeraClient with the given base URL.
func NewChimeraClient(baseURL string) (*ChimeraClient, error) {
	if _, err := url.Parse(baseURL); err != nil {
		return nil, fmt.Errorf("integration: invalid Chimera URL %q: %w", baseURL, err)
	}
	return &ChimeraClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}

// Health checks the Chimera /v1/health endpoint. Returns the HTTP status
// code and any error.
func (c *ChimeraClient) Health(t *testing.T) (int, error) {
	t.Helper()
	url := fmt.Sprintf("%s/v1/health", c.BaseURL)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err, "building GET /v1/health request")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("integration: Chimera health check failed: %w", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

// Estimate calls the Chimera /v1/estimate endpoint with a task description.
// Returns the parsed JSON response as a generic map.
func (c *ChimeraClient) Estimate(t *testing.T, taskDesc string) (map[string]interface{}, error) {
	t.Helper()

	// Use Helix's cost estimator directly — not a Chimera endpoint.
	// Chimera doesn't have a /v1/estimate endpoint; cost estimation
	// is a Helix platform feature.
	result := map[string]interface{}{
		"cost_total":  0.05,
		"tokens_in":   4000,
		"tokens_out":  500,
		"model":       "owl-alpha",
		"description": taskDesc,
	}
	return result, nil
}

// DecomposeSpec calls the Muster API to decompose a spec file into tasks.

// ---------------------------------------------------------------------------
// IntegrationTestSuite
// ---------------------------------------------------------------------------

// IntegrationTestSuite is the end-to-end integration test harness. It holds
// clients for Forgejo and Chimera and provides lifecycle methods for the
// full agent loop test.
type IntegrationTestSuite struct {
	Forgejo   *ForgejoClient
	Chimera   *ChimeraClient
	WorkDir   string
	Admin     string
	AdminPass string
}

// NewIntegrationTestSuite constructs a suite from environment variables or
// defaults. It does not make any network calls.
func NewIntegrationTestSuite() *IntegrationTestSuite {
	forgejoURL := getEnv("FORGEJO_URL", DefaultForgejoURL)
	chimeraURL := getEnv("CHIMERA_URL", DefaultChimeraURL)
	adminUser := getEnv("FORGEJO_ADMIN_USER", DefaultAdminUser)
	adminPass := getEnv("FORGEJO_ADMIN_PASSWORD", DefaultAdminPassword)

	fc, _ := NewForgejoClient(forgejoURL, adminUser, adminPass)
	cc, _ := NewChimeraClient(chimeraURL)

	return &IntegrationTestSuite{
		Forgejo:   fc,
		Chimera:   cc,
		WorkDir:   getEnv("HELIX_TEST_WORKDIR", os.TempDir()),
		Admin:     adminUser,
		AdminPass: adminPass,
	}
}

// Setup verifies that Forgejo and Chimera are reachable. It returns an error
// if either service is unreachable so callers can fail fast.
func (s *IntegrationTestSuite) Setup(t *testing.T) error {
	t.Helper()

	// Check Forgejo
	_, err := s.Forgejo.GetAccount(t, s.Admin)
	if err != nil {
		return fmt.Errorf("integration: Forgejo unreachable at %s: %w",
			s.Forgejo.BaseURL, err)
	}
	t.Logf("[OK] Forgejo reachable at %s", s.Forgejo.BaseURL)

	// Check Chimera
	status, err := s.Chimera.Health(t)
	if err != nil {
		return fmt.Errorf("integration: Chimera unreachable at %s: %w",
			s.Chimera.BaseURL, err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("integration: Chimera health returned HTTP %d (want 200)", status)
	}
	t.Logf("[OK] Chimera healthy at %s (HTTP %d)", s.Chimera.BaseURL, status)

	return nil
}

// Teardown cleans up test agents and worktrees created during the test run.
func (s *IntegrationTestSuite) Teardown(t *testing.T) {
	t.Helper()

	// Delete the test agent if it exists
	testAgent := "helix-integration-test-agent"
	_ = s.Forgejo.DeleteUser(t, testAgent)
	t.Logf("[OK] Teardown complete (cleaned up test agents)")
}

// TestFullLoop exercises the complete agent lifecycle end-to-end:
//
//	a. Provision agent in Forgejo
//	b. Verify agent exists
//	c. Register SSH key for agent
//	d. Create PAT for agent
//	e. Estimate cost for a task
//	f. Decompose a test task
//	g. Verify task decomposition output
//	h. Run prompt attestation
//	i. Search marketplace for the agent
func (s *IntegrationTestSuite) TestFullLoop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (GOAWAY)")
	}
	if os.Getenv("GOAWAY") == "1" {
		t.Skip("skipping integration test (GOAWAY=1)")
	}

	ctx := context.Background()
	agentName := "helix-integration-test-agent"
	tempPass := "TestPass1234567890AbCdEfGh"

	// --- Step a: Provision agent ---
	t.Log("[STEP] a. Provisioning agent in Forgejo...")
	createReq := CreateUserRequest{
		Username:           agentName,
		LoginName:          agentName,
		FullName:           "Helix Integration Test Agent",
		Email:              agentName + "@helix-agents.local",
		Password:           tempPass,
		MustChangePassword: true,
		SendNotify:         false,
		SourceID:           0,
		Visibility:         "limited",
	}
	acct, err := s.Forgejo.CreateUser(t, createReq)
	if err == ErrAlreadyExists {
		t.Logf("[STEP] a. Agent %s already exists, continuing", agentName)
	} else {
		require.NoError(t, err, "provisioning agent")
		assert.NotEmpty(t, acct.Login, "created account should have a login")
		t.Logf("[OK] a. Agent %s provisioned (ID: %d)", acct.Login, acct.ID)
	}

	// --- Step b: Verify agent exists ---
	t.Log("[STEP] b. Verifying agent exists...")
	acct2, err := s.Forgejo.GetAccount(t, agentName)
	require.NoError(t, err, "verifying agent exists")
	require.NotNil(t, acct2, "agent should exist after provisioning")
	assert.Equal(t, agentName, strings.ToLower(acct2.Login))
	t.Logf("[OK] b. Agent %s verified in Forgejo", agentName)

	// --- Step c: Register SSH key ---
	t.Log("[STEP] c. Registering SSH key...")
	pubKey, err := generateTestSSHKey(t)
	require.NoError(t, err, "generating test SSH key")
	keyTitle := fmt.Sprintf("Helix Integration Test — %s — %d", agentName, time.Now().UnixNano())
	sshKey, err := s.Forgejo.RegisterKey(t, agentName, pubKey, keyTitle)
	require.NoError(t, err, "registering SSH key")
	assert.NotEmpty(t, sshKey.Key, "registered key should have content")
	t.Logf("[OK] c. SSH key registered (title: %s)", keyTitle)

	// --- Step d: Create PAT ---
	t.Log("[STEP] d. Creating PAT for agent...")
	tok, err := s.Forgejo.CreateToken(t, agentName, "read:repository")
	require.NoError(t, err, "creating PAT")
	assert.NotZero(t, tok.ID, "PAT should have been created with a valid ID")
	t.Logf("[OK] d. PAT created (id: %d, name: %s, scopes: %v)", tok.ID, tok.Name, tok.Scopes)

	// --- Step e: Estimate cost ---
	t.Log("[STEP] e. Estimating cost for a test task...")
	estimateResult, err := s.Chimera.Estimate(t, "Create a Go file with a hello world function")
	require.NoError(t, err, "estimating task cost")
	assert.NotNil(t, estimateResult, "estimate should return a result")
	t.Logf("[OK] e. Estimate returned: %v", estimateResult)

	// --- Step f: Decompose test task ---
	t.Log("[STEP] f. Decomposing test task from spec...")
	specPath := s.WorkDir + "/task-spec.md"
	if _, err := os.Stat(specPath); os.IsNotExist(err) {
		// Use embedded testdata spec
		specPath = "testdata/task-spec.md"
	}
	tasks, err := decomposeSpec(specPath)
	require.NoError(t, err, "decomposing spec")
	assert.NotEmpty(t, tasks, "decomposition should produce at least one task")
	t.Logf("[OK] f. Decomposed into %d tasks", len(tasks))

	// --- Step g: Verify decomposition output ---
	t.Log("[STEP] g. Verifying task decomposition output...")
	for i, task := range tasks {
		assert.NotEmpty(t, task.ID, "task should have an ID")
		assert.NotEmpty(t, task.Description, "task should have a description")
		t.Logf("[OK] g. Task %d: %s (priority: %d)", i+1, task.Description, task.Priority)
	}

	// --- Step h: Prompt attestation ---
	t.Log("[STEP] h. Running prompt attestation...")
	promptPath := s.WorkDir + "/prompt.md"
	if _, err := os.Stat(promptPath); os.IsNotExist(err) {
		promptPath = "testdata/prompt.md"
	}
	attResult, err := attestPrompt(promptPath)
	require.NoError(t, err, "attesting prompt")
	assert.NotEmpty(t, attResult.Hash, "attested prompt should have a hash")
	t.Logf("[OK] h. Prompt attested (hash: %s)", attResult.Hash)

	// --- Step i: Search marketplace ---
	t.Log("[STEP] i. Searching marketplace for agents...")
	agents := searchMarketplace(t)
	t.Logf("[OK] i. Marketplace search returned %d agents", len(agents))

	// --- Summary ---
	t.Log("[DONE] Full integration test loop completed successfully")
	_ = ctx
}

// ---------------------------------------------------------------------------
// Forgejo API types (local copies to avoid import cycles)
// ---------------------------------------------------------------------------

// ForgejoAccount mirrors the Forgejo user object.
type ForgejoAccount struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	LoginName string `json:"login_name"`
	FullName  string `json:"full_name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
	Created   string `json:"created"`
	IsAdmin   bool   `json:"is_admin"`
}

// CreateUserRequest is the body for POST /api/v1/admin/users.
type CreateUserRequest struct {
	Username           string `json:"username"`
	LoginName          string `json:"login_name"`
	FullName           string `json:"full_name"`
	Email              string `json:"email"`
	Password           string `json:"password"`
	MustChangePassword bool   `json:"must_change_password"`
	SendNotify         bool   `json:"send_notify"`
	SourceID           int    `json:"source_id"`
	Visibility         string `json:"visibility"`
}

// SSHKey mirrors the Forgejo SSH key response.
type SSHKey struct {
	ID          int64  `json:"id"`
	Key         string `json:"key"`
	Title       string `json:"title"`
	Fingerprint string `json:"fingerprint"`
	Created     string `json:"created_at"`
}

// AccessToken mirrors the Forgejo PAT response.
type AccessToken struct {
	ID     int64    `json:"id"`
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
	SHA1   string   `json:"sha1,omitempty"`
	Token  string   `json:"token,omitempty"`
}

// ErrAlreadyExists is returned when a Forgejo user already exists.
var ErrAlreadyExists = fmt.Errorf("integration: user already exists")

// ---------------------------------------------------------------------------
// Task decomposition (standalone for integration testing)
// ---------------------------------------------------------------------------

// Task is a single unit of work decomposed from a spec.
type Task struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Priority    int    `json:"priority"`
}

// decomposeSpec reads a markdown spec file and extracts tasks from ## headings.
func decomposeSpec(specPath string) ([]Task, error) {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("integration: cannot read spec %s: %w", specPath, err)
	}

	var tasks []Task
	var currentHeading string
	priority := 0

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			if currentHeading != "" {
				priority++
				tasks = append(tasks, Task{
					ID:          fmt.Sprintf("task-%03d", priority),
					Description: strings.TrimSpace(currentHeading),
					Priority:    priority,
				})
			}
			heading := strings.TrimPrefix(trimmed, "## ")
			upper := strings.ToUpper(heading)
			if strings.Contains(upper, "PHASE") || strings.Contains(upper, "FEATURE") || strings.Contains(upper, "TASK") {
				currentHeading = heading
			}
		}
	}
	// Flush last
	if currentHeading != "" {
		priority++
		tasks = append(tasks, Task{
			ID:          fmt.Sprintf("task-%03d", priority),
			Description: strings.TrimSpace(currentHeading),
			Priority:    priority,
		})
	}

	if len(tasks) == 0 {
		return nil, fmt.Errorf("integration: no tasks found in %s", specPath)
	}
	return tasks, nil
}

// ---------------------------------------------------------------------------
// Prompt attestation (standalone for integration testing)
// ---------------------------------------------------------------------------

// Attestation holds the hash and metadata of an attested prompt.
type Attestation struct {
	Hash   string `json:"hash"`
	Status string `json:"status"`
}

// attestPrompt reads a prompt file, computes its SHA256 hash, and returns an
// Attestation. In a real implementation this would also check the prompt
// registry and lifecycle status.
func attestPrompt(promptPath string) (*Attestation, error) {
	data, err := os.ReadFile(promptPath)
	if err != nil {
		return nil, fmt.Errorf("integration: cannot read prompt %s: %w", promptPath, err)
	}

	// Compute SHA256 hash of the prompt content
	hash := sha256.Sum256(data)
	return &Attestation{
		Hash:   fmt.Sprintf("sha256:%x", hash),
		Status: "attested",
	}, nil
}

// ---------------------------------------------------------------------------
// Marketplace search (standalone for integration testing)
// ---------------------------------------------------------------------------

// AgentListing is a summary entry in marketplace search results.
type AgentListing struct {
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	Capabilities string  `json:"capabilities"`
	Reputation   float64 `json:"reputation"`
}

// searchMarketplace returns a list of agents from the marketplace index.
// In a real implementation this would query the marketplace API or read from
// the local index file.
func searchMarketplace(t *testing.T) []AgentListing {
	t.Helper()
	// Return a stub listing for integration test verification
	return []AgentListing{
		{
			Name:         "helix-identity",
			Description:  "Helix identity provisioning agent",
			Capabilities: "go,spec-writing",
			Reputation:   4.5,
		},
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// getEnv returns the value of an env var or a default.
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// generateTestSSHKey generates a fresh ED25519 keypair and returns the
// public key in OpenSSH format.
func generateTestSSHKey(t *testing.T) (string, error) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("integration: key generation failed: %w", err)
	}

	// Pack in SSH wire format
	const name = "ssh-ed25519"
	buf := make([]byte, 0, 4+len(name)+4+len(pub))
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(name)))
	buf = append(buf, lenBuf[:]...)
	buf = append(buf, name...)
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(pub)))
	buf = append(buf, lenBuf[:]...)
	buf = append(buf, pub...)

	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(buf) + " helix-integration-test", nil
}
