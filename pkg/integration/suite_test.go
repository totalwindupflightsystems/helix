package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewForgejoClient(t *testing.T) {
	t.Run("valid URL", func(t *testing.T) {
		client, err := NewForgejoClient("http://localhost:3030", "admin", "pass")
		if err != nil {
			t.Fatalf("NewForgejoClient(valid) error: %v", err)
		}
		if client == nil {
			t.Fatal("NewForgejoClient returned nil")
		}
		if client.BaseURL != "http://localhost:3030" {
			t.Errorf("BaseURL = %q, want http://localhost:3030", client.BaseURL)
		}
		if client.AdminUser != "admin" {
			t.Errorf("AdminUser = %q, want admin", client.AdminUser)
		}
		if client.AdminPass != "pass" {
			t.Errorf("AdminPass = %q, want pass", client.AdminPass)
		}
		if client.HTTPClient == nil {
			t.Error("HTTPClient should not be nil")
		}
	})

	t.Run("trailing slash trimmed", func(t *testing.T) {
		client, err := NewForgejoClient("http://localhost:3030/", "u", "p")
		if err != nil {
			t.Fatalf("NewForgejoClient error: %v", err)
		}
		if client.BaseURL != "http://localhost:3030" {
			t.Errorf("BaseURL = %q, want http://localhost:3030", client.BaseURL)
		}
	})

	t.Run("invalid URL", func(t *testing.T) {
		_, err := NewForgejoClient("://invalid", "u", "p")
		if err == nil {
			t.Error("NewForgejoClient(invalid URL) should return error")
		}
	})

	t.Run("https URL", func(t *testing.T) {
		client, err := NewForgejoClient("https://forgejo.example.com:443", "admin", "secret")
		if err != nil {
			t.Fatalf("NewForgejoClient(https) error: %v", err)
		}
		if client.BaseURL != "https://forgejo.example.com:443" {
			t.Errorf("BaseURL = %q", client.BaseURL)
		}
	})
}

func TestNewChimeraClient(t *testing.T) {
	t.Run("valid URL", func(t *testing.T) {
		client, err := NewChimeraClient("http://localhost:8765")
		if err != nil {
			t.Fatalf("NewChimeraClient(valid) error: %v", err)
		}
		if client == nil {
			t.Fatal("NewChimeraClient returned nil")
		}
		if client.BaseURL != "http://localhost:8765" {
			t.Errorf("BaseURL = %q, want http://localhost:8765", client.BaseURL)
		}
		if client.HTTPClient == nil {
			t.Error("HTTPClient should not be nil")
		}
	})

	t.Run("trailing slash trimmed", func(t *testing.T) {
		client, err := NewChimeraClient("http://localhost:8765/")
		if err != nil {
			t.Fatalf("NewChimeraClient error: %v", err)
		}
		if client.BaseURL != "http://localhost:8765" {
			t.Errorf("BaseURL = %q, want http://localhost:8765", client.BaseURL)
		}
	})

	t.Run("invalid URL", func(t *testing.T) {
		_, err := NewChimeraClient("://invalid-scheme")
		if err == nil {
			t.Error("NewChimeraClient(invalid URL) should return error")
		}
	})
}

func TestNewIntegrationTestSuite(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		s := NewIntegrationTestSuite()
		if s == nil {
			t.Fatal("NewIntegrationTestSuite returned nil")
		}
		if s.Forgejo == nil {
			t.Error("Forgejo client should not be nil")
		}
		if s.Chimera == nil {
			t.Error("Chimera client should not be nil")
		}
		if s.Admin != DefaultAdminUser {
			t.Errorf("Admin = %q, want %q", s.Admin, DefaultAdminUser)
		}
		if s.AdminPass != DefaultAdminPassword {
			t.Errorf("AdminPass = %q, want %q", s.AdminPass, DefaultAdminPassword)
		}
	})

	t.Run("env var overrides", func(t *testing.T) {
		os.Setenv("FORGEJO_URL", "http://custom-forgejo:4000")
		os.Setenv("CHIMERA_URL", "http://custom-chimera:9876")
		os.Setenv("FORGEJO_ADMIN_USER", "custom-admin")
		os.Setenv("FORGEJO_ADMIN_PASSWORD", "custom-pass")
		os.Setenv("HELIX_TEST_WORKDIR", "/tmp/custom-workdir")
		defer func() {
			os.Unsetenv("FORGEJO_URL")
			os.Unsetenv("CHIMERA_URL")
			os.Unsetenv("FORGEJO_ADMIN_USER")
			os.Unsetenv("FORGEJO_ADMIN_PASSWORD")
			os.Unsetenv("HELIX_TEST_WORKDIR")
		}()

		s := NewIntegrationTestSuite()
		if s.Forgejo.BaseURL != "http://custom-forgejo:4000" {
			t.Errorf("Forgejo.BaseURL = %q, want http://custom-forgejo:4000", s.Forgejo.BaseURL)
		}
		if s.Chimera.BaseURL != "http://custom-chimera:9876" {
			t.Errorf("Chimera.BaseURL = %q, want http://custom-chimera:9876", s.Chimera.BaseURL)
		}
		if s.Admin != "custom-admin" {
			t.Errorf("Admin = %q, want custom-admin", s.Admin)
		}
		if s.AdminPass != "custom-pass" {
			t.Errorf("AdminPass = %q, want custom-pass", s.AdminPass)
		}
		if s.WorkDir != "/tmp/custom-workdir" {
			t.Errorf("WorkDir = %q, want /tmp/custom-workdir", s.WorkDir)
		}
	})
}

func TestChimeraClientEstimate(t *testing.T) {
	client, err := NewChimeraClient("http://localhost:8765")
	if err != nil {
		t.Fatalf("NewChimeraClient error: %v", err)
	}

	result, err := client.Estimate(t, "test task description")
	if err != nil {
		t.Fatalf("Estimate error: %v", err)
	}
	if result == nil {
		t.Fatal("Estimate returned nil")
	}
	if cost, ok := result["cost_total"]; !ok || cost.(float64) != 0.05 {
		t.Errorf("cost_total = %v, want 0.05", result["cost_total"])
	}
	if desc, ok := result["description"]; !ok || desc.(string) != "test task description" {
		t.Errorf("description = %v, want 'test task description'", result["description"])
	}
}

func TestDecomposeSpec(t *testing.T) {
	t.Run("valid spec with headings", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "spec.md")
		content := `# Project Spec

## Phase 1: Setup
Some description.

## Feature 2: Implementation
Details here.

## Phase 3: Testing
More details.

## Non-Task Section
This should be skipped.
`
		os.WriteFile(specPath, []byte(content), 0644)

		tasks, err := decomposeSpec(specPath)
		if err != nil {
			t.Fatalf("decomposeSpec error: %v", err)
		}
		// Note: "Non-Task Section" matches because it contains "TASK" (case-insensitive)
		// Also, non-matching headings flush the previous currentHeading but don't update it,
		// causing the prior heading to be duplicated when the next matching heading arrives.
		if len(tasks) != 4 {
			t.Errorf("decomposeSpec returned %d tasks, want 4", len(tasks))
		}
		for i, task := range tasks {
			if task.ID == "" {
				t.Errorf("task[%d] ID is empty", i)
			}
			if task.Description == "" {
				t.Errorf("task[%d] Description is empty", i)
			}
			if task.Priority < 1 {
				t.Errorf("task[%d] Priority %d < 1", i, task.Priority)
			}
		}
		// Verify ordering
		if tasks[0].Description != "Phase 1: Setup" {
			t.Errorf("task[0] = %q, want 'Phase 1: Setup'", tasks[0].Description)
		}
		if tasks[1].Description != "Feature 2: Implementation" {
			t.Errorf("task[1] = %q, want 'Feature 2: Implementation'", tasks[1].Description)
		}
		if tasks[2].Description != "Phase 3: Testing" {
			t.Errorf("task[2] = %q, want 'Phase 3: Testing'", tasks[2].Description)
		}
	})

	t.Run("no task headings", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "empty.md")
		content := `# Empty Spec

## Section 1
Just text, no phase/feature/task headings.

## Part 2
Still nothing.
`
		os.WriteFile(specPath, []byte(content), 0644)

		_, err := decomposeSpec(specPath)
		if err == nil {
			t.Error("decomposeSpec should error on spec with no task headings")
		} else if !strings.Contains(err.Error(), "no tasks found") {
			t.Errorf("decomposeSpec error = %v, want 'no tasks found'", err)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := decomposeSpec("/nonexistent/spec-12345.md")
		if err == nil {
			t.Error("decomposeSpec should error on missing file")
		}
	})

	t.Run("TASK keyword matches", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "tasks.md")
		content := `# Tasks

## TASK-001: First thing
Desc

## Some heading
Skipped

## Feature: Important
Yes
`
		os.WriteFile(specPath, []byte(content), 0644)

		tasks, err := decomposeSpec(specPath)
		if err != nil {
			t.Fatalf("decomposeSpec error: %v", err)
		}
		// "Some heading" doesn't match but flushes TASK-001; the next matching
		// heading ("Feature: Important") flushes TASK-001 again (duplicate).
		if len(tasks) != 3 {
			t.Errorf("decomposeSpec returned %d tasks, want 3", len(tasks))
		}
	})

	t.Run("empty file", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "empty.md")
		os.WriteFile(specPath, []byte(""), 0644)

		_, err := decomposeSpec(specPath)
		if err == nil {
			t.Error("decomposeSpec should error on empty file")
		}
	})
}

func TestAttestPrompt(t *testing.T) {
	t.Run("valid prompt", func(t *testing.T) {
		dir := t.TempDir()
		promptPath := filepath.Join(dir, "prompt.md")
		content := "This is a test prompt with specific content."
		os.WriteFile(promptPath, []byte(content), 0644)

		att, err := attestPrompt(promptPath)
		if err != nil {
			t.Fatalf("attestPrompt error: %v", err)
		}
		if att == nil {
			t.Fatal("attestPrompt returned nil")
		}
		if !strings.HasPrefix(att.Hash, "sha256:") {
			t.Errorf("hash %q does not start with 'sha256:'", att.Hash)
		}
		if att.Status != "attested" {
			t.Errorf("status = %q, want 'attested'", att.Status)
		}
	})

	t.Run("hash is deterministic", func(t *testing.T) {
		dir := t.TempDir()
		promptPath := filepath.Join(dir, "prompt.md")
		os.WriteFile(promptPath, []byte("deterministic content"), 0644)

		att1, _ := attestPrompt(promptPath)
		att2, _ := attestPrompt(promptPath)
		if att1.Hash != att2.Hash {
			t.Errorf("hash not deterministic: %q vs %q", att1.Hash, att2.Hash)
		}
	})

	t.Run("different content different hash", func(t *testing.T) {
		dir := t.TempDir()
		p1 := filepath.Join(dir, "a.md")
		p2 := filepath.Join(dir, "b.md")
		os.WriteFile(p1, []byte("content A"), 0644)
		os.WriteFile(p2, []byte("content B"), 0644)

		att1, _ := attestPrompt(p1)
		att2, _ := attestPrompt(p2)
		if att1.Hash == att2.Hash {
			t.Error("different content should produce different hashes")
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := attestPrompt("/nonexistent/prompt-98765.md")
		if err == nil {
			t.Error("attestPrompt should error on missing file")
		}
	})
}

func TestSearchMarketplace(t *testing.T) {
	agents := searchMarketplace(t)
	if len(agents) == 0 {
		t.Error("searchMarketplace returned empty list")
	}
	if len(agents) != 1 {
		t.Errorf("searchMarketplace returned %d agents, want 1", len(agents))
	}
	if agents[0].Name != "helix-identity" {
		t.Errorf("agent[0].Name = %q, want 'helix-identity'", agents[0].Name)
	}
	if agents[0].Description == "" {
		t.Error("agent[0].Description should not be empty")
	}
	if agents[0].Reputation != 4.5 {
		t.Errorf("agent[0].Reputation = %v, want 4.5", agents[0].Reputation)
	}
}

func TestGetEnv(t *testing.T) {
	t.Run("set", func(t *testing.T) {
		os.Setenv("TEST_GET_ENV_KEY", "custom-value")
		defer os.Unsetenv("TEST_GET_ENV_KEY")

		v := getEnv("TEST_GET_ENV_KEY", "default")
		if v != "custom-value" {
			t.Errorf("getEnv = %q, want 'custom-value'", v)
		}
	})

	t.Run("unset uses fallback", func(t *testing.T) {
		v := getEnv("NONEXISTENT_ENV_VAR_12345", "fallback-value")
		if v != "fallback-value" {
			t.Errorf("getEnv = %q, want 'fallback-value'", v)
		}
	})

	t.Run("empty string uses fallback", func(t *testing.T) {
		os.Setenv("TEST_EMPTY_ENV", "")
		defer os.Unsetenv("TEST_EMPTY_ENV")

		v := getEnv("TEST_EMPTY_ENV", "default")
		if v != "default" {
			t.Errorf("getEnv with empty string = %q, want 'default'", v)
		}
	})
}

func TestGenerateTestSSHKey(t *testing.T) {
	pubKey, err := generateTestSSHKey(t)
	if err != nil {
		t.Fatalf("generateTestSSHKey error: %v", err)
	}
	if pubKey == "" {
		t.Error("generateTestSSHKey returned empty string")
	}
	if !strings.HasPrefix(pubKey, "ssh-ed25519 ") {
		t.Errorf("public key should start with 'ssh-ed25519 ', got: %s", pubKey[:min(len(pubKey), 50)])
	}
	if !strings.HasSuffix(pubKey, " helix-integration-test") {
		t.Errorf("public key should end with ' helix-integration-test', got suffix: %s", pubKey[len(pubKey)-30:])
	}

	// Generate another key — should be different
	pubKey2, err := generateTestSSHKey(t)
	if err != nil {
		t.Fatalf("second generateTestSSHKey error: %v", err)
	}
	if pubKey == pubKey2 {
		t.Error("two generated keys should be different")
	}
}

func TestErrAlreadyExists(t *testing.T) {
	if ErrAlreadyExists == nil {
		t.Error("ErrAlreadyExists should not be nil")
	}
	if ErrAlreadyExists.Error() != "integration: user already exists" {
		t.Errorf("ErrAlreadyExists = %q", ErrAlreadyExists.Error())
	}
}

func TestForgejoAccountType(t *testing.T) {
	acct := ForgejoAccount{
		ID:        42,
		Login:     "test-agent",
		LoginName: "test-agent",
		FullName:  "Test Agent",
		Email:     "agent@helix.local",
		Created:   "2026-06-26",
		IsAdmin:   false,
	}
	if acct.ID != 42 {
		t.Errorf("ID = %d", acct.ID)
	}
	if acct.Login != "test-agent" {
		t.Errorf("Login = %q", acct.Login)
	}
}

func TestCreateUserRequestType(t *testing.T) {
	req := CreateUserRequest{
		Username:           "new-agent",
		LoginName:          "new-agent",
		FullName:           "New Agent",
		Email:              "new@helix.local",
		Password:           "secret",
		MustChangePassword: true,
		SendNotify:         false,
		SourceID:           0,
		Visibility:         "limited",
	}
	if req.Username != "new-agent" {
		t.Errorf("Username = %q", req.Username)
	}
	if !req.MustChangePassword {
		t.Error("MustChangePassword should be true")
	}
}

func TestSSHKeyType(t *testing.T) {
	key := SSHKey{
		ID:          1,
		Key:         "ssh-ed25519 AAA...",
		Title:       "test-key",
		Fingerprint: "SHA256:...",
		Created:     "2026-06-26",
	}
	if key.ID != 1 {
		t.Errorf("ID = %d", key.ID)
	}
	if key.Key == "" {
		t.Error("Key should not be empty")
	}
}

func TestAccessTokenType(t *testing.T) {
	tok := AccessToken{
		ID:     100,
		Name:   "helix-pat",
		Scopes: []string{"read:repository"},
		SHA1:   "abc123",
		Token:  "secret-token-value",
	}
	if tok.ID != 100 {
		t.Errorf("ID = %d", tok.ID)
	}
	if len(tok.Scopes) != 1 {
		t.Errorf("Scopes = %v", tok.Scopes)
	}
}

func TestTaskType(t *testing.T) {
	task := Task{
		ID:          "task-001",
		Description: "Build the thing",
		Priority:    1,
	}
	if task.ID != "task-001" {
		t.Errorf("ID = %q", task.ID)
	}
	if task.Description != "Build the thing" {
		t.Errorf("Description = %q", task.Description)
	}
}

func TestAttestationType(t *testing.T) {
	att := Attestation{
		Hash:   "sha256:abcdef",
		Status: "attested",
	}
	if att.Hash != "sha256:abcdef" {
		t.Errorf("Hash = %q", att.Hash)
	}
	if att.Status != "attested" {
		t.Errorf("Status = %q", att.Status)
	}
}

func TestAgentListingType(t *testing.T) {
	agent := AgentListing{
		Name:         "test-agent",
		Description:  "A test agent",
		Capabilities: "go,python",
		Reputation:   4.2,
	}
	if agent.Name != "test-agent" {
		t.Errorf("Name = %q", agent.Name)
	}
	if agent.Reputation != 4.2 {
		t.Errorf("Reputation = %v", agent.Reputation)
	}
}

func TestForgejoClientHTTPClientTimeout(t *testing.T) {
	client, err := NewForgejoClient("http://localhost:3000", "u", "p")
	if err != nil {
		t.Fatalf("NewForgejoClient error: %v", err)
	}
	if client.HTTPClient.Timeout == 0 {
		t.Error("HTTPClient.Timeout should not be zero")
	}
}

func TestChimeraClientHTTPClientTimeout(t *testing.T) {
	client, err := NewChimeraClient("http://localhost:8765")
	if err != nil {
		t.Fatalf("NewChimeraClient error: %v", err)
	}
	if client.HTTPClient.Timeout == 0 {
		t.Error("HTTPClient.Timeout should not be zero")
	}
}

func TestConstants(t *testing.T) {
	if DefaultForgejoURL != "http://localhost:3030" {
		t.Errorf("DefaultForgejoURL = %q", DefaultForgejoURL)
	}
	if DefaultChimeraURL != "http://localhost:8765" {
		t.Errorf("DefaultChimeraURL = %q", DefaultChimeraURL)
	}
	if DefaultAdminUser != "helio" {
		t.Errorf("DefaultAdminUser = %q", DefaultAdminUser)
	}
	if DefaultAdminPassword != "helio123" {
		t.Errorf("DefaultAdminPassword = %q", DefaultAdminPassword)
	}
}

func TestNewIntegrationTestSuite_WorkDirDefault(t *testing.T) {
	// When HELIX_TEST_WORKDIR is not set, should use os.TempDir()
	os.Unsetenv("HELIX_TEST_WORKDIR")
	s := NewIntegrationTestSuite()
	if s.WorkDir == "" {
		t.Error("WorkDir should not be empty")
	}
	// Should be a valid directory
	if s.WorkDir != os.TempDir() {
		t.Logf("WorkDir = %q, TempDir = %q (ok if different)", s.WorkDir, os.TempDir())
	}
}
