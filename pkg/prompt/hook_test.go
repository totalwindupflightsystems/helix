package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// ParseCommitMsgFromFile
// ============================================================================

func TestParseCommitMsgFromFile(t *testing.T) {
	t.Run("valid file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "COMMIT_MSG")
		content := "feat: add tests\n\nPrompt: sha256:abcdef1234567890\nModel: deepseek-v4-pro"
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		got, err := ParseCommitMsgFromFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != content {
			t.Errorf("content = %q, want %q", got, content)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := ParseCommitMsgFromFile("/nonexistent/commit_msg")
		if err == nil {
			t.Error("expected error for missing file")
		}
	})

	t.Run("empty file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "EMPTY_MSG")
		if err := os.WriteFile(path, []byte(""), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		got, err := ParseCommitMsgFromFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "" {
			t.Errorf("content = %q, want empty", got)
		}
	})
}

// ============================================================================
// shortHash
// ============================================================================

func TestShortHash(t *testing.T) {
	t.Run("with sha256 prefix", func(t *testing.T) {
		got := shortHash("sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
		want := "sha256:abcdef123456"
		if got != want {
			t.Errorf("shortHash = %q, want %q", got, want)
		}
	})

	t.Run("without prefix", func(t *testing.T) {
		got := shortHash("abcdef1234567890abcdef")
		want := "sha256:abcdef123456"
		if got != want {
			t.Errorf("shortHash = %q, want %q", got, want)
		}
	})

	t.Run("short hash unchanged", func(t *testing.T) {
		got := shortHash("sha256:abc")
		want := "sha256:abc"
		if got != want {
			t.Errorf("shortHash = %q, want %q", got, want)
		}
	})

	t.Run("no colon", func(t *testing.T) {
		got := shortHash("abcd1234")
		want := "sha256:abcd1234"
		if got != want {
			t.Errorf("shortHash = %q, want %q", got, want)
		}
	})

	t.Run("exactly 12 after prefix", func(t *testing.T) {
		got := shortHash("sha256:123456789012")
		want := "sha256:123456789012"
		if got != want {
			t.Errorf("shortHash = %q, want %q", got, want)
		}
	})
}

// ============================================================================
// RunCommitMsgHook
// ============================================================================

// setupHookRegistry creates a registered prompt in a temp dir and returns
// the hash and registry dir. Caller must call setRegistryDir with the dir.
func setupHookRegistry(t *testing.T, dir, component, version, content string, status LifecycleStatus, promptfooStatus string) (hash string) {
	t.Helper()
	setRegistryDir(t, dir)

	// Create prompt file
	promptDir := filepath.Join(dir, component, version)
	if err := os.MkdirAll(promptDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "prompt.md"), []byte(content), 0644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	hash = Hash(content)

	// Create metadata.yaml
	meta := Metadata{
		Version:   version,
		Component: component,
		Hash:      hash,
		Model:     "deepseek-v4-pro",
		Provider:  "deepseek",
		Status:    status,
	}
	if promptfooStatus != "" {
		meta.Promptfoo = PromptfooResult{Status: promptfooStatus}
	}
	if err := writeMetadata(filepath.Join(promptDir, "metadata.yaml"), &meta); err != nil {
		t.Fatalf("writeMetadata: %v", err)
	}

	// Create _index.yaml
	idx := Index{
		component: {
			version: &PromptEntry{
				Hash:     hash,
				Status:   status,
				Model:    "deepseek-v4-pro",
				Provider: "deepseek",
			},
		},
	}
	if err := saveIndex(&idx); err != nil {
		t.Fatalf("saveIndex: %v", err)
	}

	return hash
}

// writeCommitMsgFile writes a commit message to a temp file and returns the path.
func writeCommitMsgFile(t *testing.T, dir, msg string) string {
	t.Helper()
	path := filepath.Join(dir, "COMMIT_MSG")
	if err := os.WriteFile(path, []byte(msg), 0644); err != nil {
		t.Fatalf("write commit msg: %v", err)
	}
	return path
}

func TestRunCommitMsgHook(t *testing.T) {
	t.Run("missing attestation line", func(t *testing.T) {
		dir := t.TempDir()
		setupHookRegistry(t, dir, "test-comp", "v1.0.0", "test content", StatusActive, "")

		msgPath := writeCommitMsgFile(t, dir, "feat: add feature\n\nNo prompt hash here.")
		err := RunCommitMsgHook(msgPath)
		if err == nil {
			t.Error("expected error for missing attestation")
		}
		if !strings.Contains(err.Error(), "ATTESTATION_MISSING") {
			t.Errorf("error should contain ATTESTATION_MISSING, got: %v", err)
		}
	})

	t.Run("empty commit message", func(t *testing.T) {
		dir := t.TempDir()
		setupHookRegistry(t, dir, "test-comp", "v1.0.0", "test content", StatusActive, "")

		msgPath := writeCommitMsgFile(t, dir, "")
		err := RunCommitMsgHook(msgPath)
		if err == nil {
			t.Error("expected error for empty commit message")
		}
	})

	t.Run("hash not in registry", func(t *testing.T) {
		dir := t.TempDir()
		setupHookRegistry(t, dir, "test-comp", "v1.0.0", "test content", StatusActive, "")

		msgPath := writeCommitMsgFile(t, dir, "feat: something\n\nPrompt: sha256:0000000000000000000000000000000000000000000000000000000000000000\nModel: test-model")
		err := RunCommitMsgHook(msgPath)
		if err == nil {
			t.Error("expected error for hash not in registry")
		}
		if !strings.Contains(err.Error(), "PROMPT_NOT_FOUND") {
			t.Errorf("error should contain PROMPT_NOT_FOUND, got: %v", err)
		}
	})

	t.Run("active prompt with matching hash passes", func(t *testing.T) {
		dir := t.TempDir()
		content := "This is the prompt content for verification."
		hash := setupHookRegistry(t, dir, "test-comp", "v1.0.0", content, StatusActive, "")

		msg := "feat: add prompt hook tests\n\nPrompt: " + hash + "\nModel: deepseek-v4-pro\nProvider: deepseek"
		msgPath := writeCommitMsgFile(t, dir, msg)
		err := RunCommitMsgHook(msgPath)
		if err != nil {
			t.Errorf("unexpected error for active+match: %v", err)
		}
	})

	t.Run("attested prompt passes", func(t *testing.T) {
		dir := t.TempDir()
		content := "Attested prompt content."
		hash := setupHookRegistry(t, dir, "test-comp", "v1.0.0", content, StatusAttested, "")

		msgPath := writeCommitMsgFile(t, dir, "feat: attested\n\nPrompt: "+hash+"\nModel: deepseek-v4-pro")
		err := RunCommitMsgHook(msgPath)
		if err != nil {
			t.Errorf("unexpected error for attested prompt: %v", err)
		}
	})

	t.Run("deprecated prompt passes with warning", func(t *testing.T) {
		dir := t.TempDir()
		content := "Deprecated prompt content."
		hash := setupHookRegistry(t, dir, "test-comp", "v1.0.0", content, StatusDeprecated, "")

		msgPath := writeCommitMsgFile(t, dir, "feat: deprecated\n\nPrompt: "+hash+"\nModel: deepseek-v4-pro")
		err := RunCommitMsgHook(msgPath)
		if err != nil {
			// Deprecated is allowed with warning — should NOT error
			t.Errorf("unexpected error for deprecated prompt: %v", err)
		}
	})

	t.Run("draft prompt lifecycle violation", func(t *testing.T) {
		dir := t.TempDir()
		content := "Draft content."
		hash := setupHookRegistry(t, dir, "test-comp", "v1.0.0", content, StatusDraft, "")

		msgPath := writeCommitMsgFile(t, dir, "feat: draft\n\nPrompt: "+hash+"\nModel: deepseek-v4-pro")
		err := RunCommitMsgHook(msgPath)
		if err == nil {
			t.Error("expected error for draft prompt")
		}
		if !strings.Contains(err.Error(), "LIFECYCLE_VIOLATION") {
			t.Errorf("error should contain LIFECYCLE_VIOLATION, got: %v", err)
		}
	})

	t.Run("proposed prompt lifecycle violation", func(t *testing.T) {
		dir := t.TempDir()
		content := "Proposed content."
		hash := setupHookRegistry(t, dir, "test-comp", "v1.0.0", content, StatusProposed, "")

		msgPath := writeCommitMsgFile(t, dir, "feat: proposed\n\nPrompt: "+hash+"\nModel: deepseek-v4-pro")
		err := RunCommitMsgHook(msgPath)
		if err == nil {
			t.Error("expected error for proposed prompt")
		}
	})

	t.Run("reviewed prompt lifecycle violation", func(t *testing.T) {
		dir := t.TempDir()
		content := "Reviewed content."
		hash := setupHookRegistry(t, dir, "test-comp", "v1.0.0", content, StatusReviewed, "")

		msgPath := writeCommitMsgFile(t, dir, "feat: reviewed\n\nPrompt: "+hash+"\nModel: deepseek-v4-pro")
		err := RunCommitMsgHook(msgPath)
		if err == nil {
			t.Error("expected error for reviewed prompt")
		}
	})

	t.Run("retired prompt lifecycle violation", func(t *testing.T) {
		dir := t.TempDir()
		content := "Retired content."
		hash := setupHookRegistry(t, dir, "test-comp", "v1.0.0", content, StatusRetired, "")

		msgPath := writeCommitMsgFile(t, dir, "feat: retired\n\nPrompt: "+hash+"\nModel: deepseek-v4-pro")
		err := RunCommitMsgHook(msgPath)
		if err == nil {
			t.Error("expected error for retired prompt")
		}
	})

	t.Run("tamper detected — content changed after attestation", func(t *testing.T) {
		dir := t.TempDir()
		originalContent := "Original prompt content that was attested."
		hash := setupHookRegistry(t, dir, "test-comp", "v1.0.0", originalContent, StatusActive, "")

		// Tamper: overwrite the prompt file with different content
		promptPath := filepath.Join(dir, "test-comp", "v1.0.0", "prompt.md")
		if err := os.WriteFile(promptPath, []byte("TAMPERED content!"), 0644); err != nil {
			t.Fatalf("tamper write: %v", err)
		}

		msgPath := writeCommitMsgFile(t, dir, "feat: tampered\n\nPrompt: "+hash+"\nModel: deepseek-v4-pro")
		err := RunCommitMsgHook(msgPath)
		if err == nil {
			t.Error("expected error for tampered content")
		}
		if !strings.Contains(err.Error(), "TAMPER_DETECTED") {
			t.Errorf("error should contain TAMPER_DETECTED, got: %v", err)
		}
	})

	t.Run("promptfoo pass status", func(t *testing.T) {
		dir := t.TempDir()
		content := "Content with promptfoo pass."
		hash := setupHookRegistry(t, dir, "test-comp", "v1.0.0", content, StatusActive, "pass")

		msgPath := writeCommitMsgFile(t, dir, "feat: promptfoo-pass\n\nPrompt: "+hash+"\nModel: deepseek-v4-pro")
		err := RunCommitMsgHook(msgPath)
		if err != nil {
			t.Errorf("unexpected error for promptfoo pass: %v", err)
		}
	})

	t.Run("promptfoo fail status", func(t *testing.T) {
		dir := t.TempDir()
		content := "Content with promptfoo fail."
		hash := setupHookRegistry(t, dir, "test-comp", "v1.0.0", content, StatusActive, "fail")

		msgPath := writeCommitMsgFile(t, dir, "feat: promptfoo-fail\n\nPrompt: "+hash+"\nModel: deepseek-v4-pro")
		err := RunCommitMsgHook(msgPath)
		if err == nil {
			t.Error("expected error for promptfoo fail")
		}
		if !strings.Contains(err.Error(), "PROMPTFOO_FAILED") {
			t.Errorf("error should contain PROMPTFOO_FAILED, got: %v", err)
		}
	})

	t.Run("promptfoo no results — warns but passes", func(t *testing.T) {
		dir := t.TempDir()
		content := "Content with no promptfoo results."
		hash := setupHookRegistry(t, dir, "test-comp", "v1.0.0", content, StatusActive, "") // empty promptfoo

		msgPath := writeCommitMsgFile(t, dir, "feat: no-promptfoo\n\nPrompt: "+hash+"\nModel: deepseek-v4-pro")
		err := RunCommitMsgHook(msgPath)
		if err != nil {
			t.Errorf("unexpected error when promptfoo has no results: %v", err)
		}
	})

	t.Run("cannot read commit message file", func(t *testing.T) {
		dir := t.TempDir()
		setupHookRegistry(t, dir, "test-comp", "v1.0.0", "content", StatusActive, "")

		err := RunCommitMsgHook("/nonexistent/commit_msg_file")
		if err == nil {
			t.Error("expected error for missing commit message file")
		}
	})

	t.Run("full message with all fields", func(t *testing.T) {
		dir := t.TempDir()
		content := "Full prompt verification content."
		hash := setupHookRegistry(t, dir, "test-comp", "v1.0.0", content, StatusActive, "pass")

		msg := "feat: comprehensive test\n\n" +
			"Prompt: " + hash + "\n" +
			"Model: deepseek-v4-pro\n" +
			"Provider: deepseek\n" +
			"Spec: specs/agent-identity.md\n" +
			"Cost: $1.50\n" +
			"Author: wojons <wojonstech@gmail.com>\n\n" +
			"This is the body of the commit message."
		msgPath := writeCommitMsgFile(t, dir, msg)
		err := RunCommitMsgHook(msgPath)
		if err != nil {
			t.Errorf("unexpected error for full message: %v", err)
		}
	})
}
