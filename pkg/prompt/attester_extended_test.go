package prompt

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// TestAttestPrompt
// ---------------------------------------------------------------------------

func TestAttestPrompt(t *testing.T) {
	tests := []struct {
		name   string
		prompt Prompt
		want   *Attestation
	}{
		{
			name: "full_prompt_all_fields",
			prompt: Prompt{
				Hash:     "sha256:abcdef1234567890",
				Model:    "deepseek-v4-pro",
				Provider: "deepseek",
			},
			want: &Attestation{
				Hash:     "sha256:abcdef1234567890",
				Model:    "deepseek-v4-pro",
				Provider: "deepseek",
			},
		},
		{
			name: "partial_prompt_only_hash",
			prompt: Prompt{
				Hash: "sha256:1111222233334444",
			},
			want: &Attestation{
				Hash: "sha256:1111222233334444",
			},
		},
		{
			name: "partial_prompt_hash_and_model",
			prompt: Prompt{
				Hash:  "sha256:aaaabbbbccccdddd",
				Model: "MiniMax-M3",
			},
			want: &Attestation{
				Hash:  "sha256:aaaabbbbccccdddd",
				Model: "MiniMax-M3",
			},
		},
		{
			name:   "empty_prompt_returns_zero_attestation",
			prompt: Prompt{},
			want:   &Attestation{},
		},
		{
			name: "only_provider_no_hash",
			prompt: Prompt{
				Provider: "minimax",
			},
			want: &Attestation{
				Provider: "minimax",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			att, err := AttestPrompt(tt.prompt)
			if err != nil {
				t.Fatalf("AttestPrompt returned error: %v", err)
			}
			if att.Hash != tt.want.Hash {
				t.Errorf("Hash = %q, want %q", att.Hash, tt.want.Hash)
			}
			if att.Model != tt.want.Model {
				t.Errorf("Model = %q, want %q", att.Model, tt.want.Model)
			}
			if att.Provider != tt.want.Provider {
				t.Errorf("Provider = %q, want %q", att.Provider, tt.want.Provider)
			}
			// AttestPrompt never fills SpecRef, Cost, or Author
			if att.SpecRef != "" {
				t.Errorf("SpecRef should be empty, got %q", att.SpecRef)
			}
			if att.EstimatedCostUSD != 0 {
				t.Errorf("EstimatedCostUSD should be 0, got %f", att.EstimatedCostUSD)
			}
			if att.AgentAuthor != "" {
				t.Errorf("AgentAuthor should be empty, got %q", att.AgentAuthor)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestVerify — Verify calls GetCommitAttestation(commitRef, RegistryDir)
// which runs 'git log' in RegistryDir. RegistryDir must point to a git repo.
// ---------------------------------------------------------------------------

func TestVerify(t *testing.T) {
	repoRoot, err := findGitRoot()
	if err != nil {
		t.Skipf("not in a git repo: %v", err)
	}

	origRegistryDir := RegistryDir
	defer func() { RegistryDir = origRegistryDir }()

	tests := []struct {
		name       string
		commitRef  string
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:       "invalid_commit_ref_returns_git_error",
			commitRef:  "deadbeef1234567890deadbeef1234567890deadbeef",
			wantErr:    true,
			wantErrMsg: "git log failed",
		},
		{
			name:       "head_commit_with_no_sha256_attestation",
			commitRef:  "HEAD",
			wantErr:    true,
			wantErrMsg: "ATTESTATION_MISSING",
		},
		{
			name:       "HEAD_slash_missing_attestation",
			commitRef:  "HEAD",
			wantErr:    true,
			wantErrMsg: "ATTESTATION_MISSING",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			RegistryDir = repoRoot

			_, err := Verify(tt.commitRef)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.wantErrMsg != "" && !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Errorf("expected error containing %q, got %v", tt.wantErrMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestGetCommitAttestation_ErrorPaths
// ---------------------------------------------------------------------------

func TestGetCommitAttestation_ErrorPaths(t *testing.T) {
	repoRoot, err := findGitRoot()
	if err != nil {
		t.Skipf("not in a git repo: %v", err)
	}

	// Invalid commit SHA
	_, err = GetCommitAttestation("nonexistent-ref-999999999999", repoRoot)
	if err == nil {
		t.Error("expected error for nonexistent commit ref")
	} else if !strings.Contains(err.Error(), "git log failed") {
		t.Errorf("expected 'git log failed' error, got: %v", err)
	}

	// Invalid workDir (non-git directory)
	tmpDir := t.TempDir()
	_, err = GetCommitAttestation("HEAD", tmpDir)
	if err != nil {
		t.Logf("GetCommitAttestation in non-git dir: %v (expected)", err)
	}
}

// findGitRoot walks up from cwd to find the .git directory.

// ---------------------------------------------------------------------------
// TestVerify_HappyPath — creates a temp git repo with an attested commit
// and verifies that Verify succeeds end-to-end.
// ---------------------------------------------------------------------------

func TestVerify_HappyPath(t *testing.T) {
	// Create a temp directory that will serve as both:
	// 1. The git repo (so 'git log' works in RegistryDir)
	// 2. The prompt registry (RegistryDir has _index.yaml and prompt files)
	tmpDir := t.TempDir()

	// Init a git repo
	initCmd := exec.Command("git", "init", "-b", "main")
	initCmd.Dir = tmpDir
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	// Configure git user for the commit
	for _, cfg := range [][2]string{
		{"user.email", "test@helix.local"},
		{"user.name", "Test User"},
	} {
		cmd := exec.Command("git", "config", cfg[0], cfg[1])
		cmd.Dir = tmpDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git config %s failed: %v\n%s", cfg[0], err, out)
		}
	}

	// Create and register a prompt in the registry
	promptContent := "Be concise and accurate.\n"
	promptDir := filepath.Join(tmpDir, "test-component", "v1.0.0")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "prompt.md"), []byte(promptContent), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	hash := Hash(promptContent)

	// Write metadata
	metaContent := "version: v1.0.0\ncomponent: test-component\nhash: " + hash + "\nstatus: active\n"
	if err := os.WriteFile(filepath.Join(promptDir, "metadata.yaml"), []byte(metaContent), 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	// Write index
	indexContent := "test-component:\n  v1.0.0:\n    hash: " + hash + "\n    status: active\n    model: test-model\n    provider: test-provider\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "_index.yaml"), []byte(indexContent), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	// Stage and commit with attestation in the commit message
	stageCmd := exec.Command("git", "add", "-A")
	stageCmd.Dir = tmpDir
	if out, err := stageCmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v\n%s", err, out)
	}

	commitMsg := "feat: test attestation\n\nPrompt: sha256:" + hash[7:] + "\nModel: test-model\nProvider: test-provider\n"
	commitCmd := exec.Command("git", "commit", "-m", commitMsg)
	commitCmd.Dir = tmpDir
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %v\n%s", err, out)
	}

	// Override RegistryDir and run Verify
	origRegistryDir := RegistryDir
	RegistryDir = tmpDir
	defer func() { RegistryDir = origRegistryDir }()

	att, err := Verify("HEAD")
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if att.Hash != hash {
		t.Errorf("Hash = %q, want %q", att.Hash, hash)
	}
	if att.Model != "test-model" {
		t.Errorf("Model = %q, want test-model", att.Model)
	}
	if att.Provider != "test-provider" {
		t.Errorf("Provider = %q, want test-provider", att.Provider)
	}
}

// findGitRoot walks up from cwd to find the .git directory.
func findGitRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
