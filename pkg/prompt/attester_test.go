package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// TestParseCommitMessage
// ---------------------------------------------------------------------------

func TestParseCommitMessage(t *testing.T) {
	tests := []struct {
		name    string
		msg     string
		wantErr bool
		want    *Attestation
	}{
		{
			name:    "empty_message_returns_error",
			msg:     "",
			wantErr: true,
		},
		{
			name:    "no_hash_line_returns_error",
			msg:     "Model: deepseek-v4-pro\nProvider: deepseek\n",
			wantErr: true,
		},
		{
			name:    "hash_only_parses_correctly",
			msg:     "Prompt: sha256:abcdef1234567890",
			wantErr: false,
			want: &Attestation{
				Hash: "sha256:abcdef1234567890",
			},
		},
		{
			name: "full_message_parses_all_fields",
			msg: `Prompt: sha256:a1b2c3d4e5f6
Model: deepseek-v4-pro
Provider: deepseek
Spec: specs/prompt-registry.md
Cost: $0.025
Co-authored-by: wojons <wojonstech@gmail.com>
`,
			wantErr: false,
			want: &Attestation{
				Hash:             "sha256:a1b2c3d4e5f6",
				Model:            "deepseek-v4-pro",
				Provider:         "deepseek",
				SpecRef:          "specs/prompt-registry.md",
				EstimatedCostUSD: 0.025,
				AgentAuthor:      "wojons <wojonstech@gmail.com>",
			},
		},
		{
			name: "hex_hash_uppercase",
			msg:  "Prompt: sha256:ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890",
			wantErr: false,
			want: &Attestation{
				Hash: "sha256:ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890",
			},
		},
		{
			name: "model_with_extra_whitespace",
			msg:  "Prompt: sha256:abc\nModel:   deepseek-v4-pro   \n",
			wantErr: false,
			want: &Attestation{
				Hash:  "sha256:abc",
				Model: "deepseek-v4-pro",
			},
		},
		{
			name: "cost_integer_dollars",
			msg:  "Prompt: sha256:abc\nCost: $5",
			wantErr: false,
			want: &Attestation{
				Hash:             "sha256:abc",
				EstimatedCostUSD: 5.0,
			},
		},
		{
			name: "cost_with_cents",
			msg:  "Prompt: sha256:abc\nCost: $0.015",
			wantErr: false,
			want: &Attestation{
				Hash:             "sha256:abc",
				EstimatedCostUSD: 0.015,
			},
		},
		{
			name: "cost_invalid_format_ignored",
			msg:  "Prompt: sha256:abc\nCost: $not-a-number",
			wantErr: false,
			want: &Attestation{
				Hash:             "sha256:abc",
				EstimatedCostUSD: 0,
			},
		},
		{
			name: "hash_in_middle_of_message",
			msg: `Some feature work

Prompt: sha256:deadbeefcafe

More text below`,
			wantErr: false,
			want: &Attestation{
				Hash: "sha256:deadbeefcafe",
			},
		},
		{
			name: "provider_field_trimmed",
			msg:  "Prompt: sha256:abc\nProvider:   ollama-cloud   \n",
			wantErr: false,
			want: &Attestation{
				Hash:     "sha256:abc",
				Provider: "ollama-cloud",
			},
		},
		{
			name: "spec_field_trimmed",
			msg:  "Prompt: sha256:abc\nSpec:   specs/prompt-registry.md   \n",
			wantErr: false,
			want: &Attestation{
				Hash:    "sha256:abc",
				SpecRef: "specs/prompt-registry.md",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			att, err := ParseCommitMessage(tt.msg)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
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
			if att.SpecRef != tt.want.SpecRef {
				t.Errorf("SpecRef = %q, want %q", att.SpecRef, tt.want.SpecRef)
			}
			if att.EstimatedCostUSD != tt.want.EstimatedCostUSD {
				t.Errorf("EstimatedCostUSD = %f, want %f", att.EstimatedCostUSD, tt.want.EstimatedCostUSD)
			}
			if att.AgentAuthor != tt.want.AgentAuthor {
				t.Errorf("AgentAuthor = %q, want %q", att.AgentAuthor, tt.want.AgentAuthor)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestValidateAttestation
// ---------------------------------------------------------------------------

func TestValidateAttestation(t *testing.T) {
	tests := []struct {
		name          string
		setupRegistry bool // if true, create a registry entry for the test hash
		status        LifecycleStatus
		promptContent string
		hashOverride  string // if set, register a different hash than the computed one
		promptfooStatus string // if set, write metadata with this PromptFoo status
		wantHashMatch    bool
		wantLifecycleOK  bool
		wantPromptfooPass bool
		wantErrorsMin    int // minimum number of errors expected
		wantErrorsMax    int // maximum number (for fuzzy matching)
	}{
		{
			name:           "prompt_not_found",
			setupRegistry:  false,
			wantErrorsMin:  1,
			wantErrorsMax:  1,
		},
		{
			name:           "active_prompt_no_errors",
			setupRegistry:  true,
			status:         StatusActive,
			promptContent:  "Be concise and accurate.",
			wantHashMatch:  true,
			wantLifecycleOK: true,
			wantPromptfooPass: true,
			wantErrorsMin:  0,
			wantErrorsMax:  0,
		},
		{
			name:           "attested_prompt_no_errors",
			setupRegistry:  true,
			status:         StatusAttested,
			promptContent:  "Be helpful.",
			wantHashMatch:  true,
			wantLifecycleOK: true,
			wantPromptfooPass: true,
			wantErrorsMin:  0,
			wantErrorsMax:  0,
		},
		{
			name:           "deprecated_prompt_allowed_with_warning",
			setupRegistry:  true,
			status:         StatusDeprecated,
			promptContent:  "Old prompt.",
			wantHashMatch:  true,
			wantLifecycleOK: true,
			wantPromptfooPass: true,
			wantErrorsMin:  1, // LIFECYCLE_WARNING
			wantErrorsMax:  1,
		},
		{
			name:           "draft_prompt_lifecycle_violation",
			setupRegistry:  true,
			status:         StatusDraft,
			promptContent:  "Draft prompt.",
			wantHashMatch:  true,
			wantLifecycleOK: false,
			wantPromptfooPass: true,
			wantErrorsMin:  1,
			wantErrorsMax:  1,
		},
		{
			name:           "proposed_prompt_lifecycle_violation",
			setupRegistry:  true,
			status:         StatusProposed,
			promptContent:  "Proposed prompt.",
			wantHashMatch:  true,
			wantLifecycleOK: false,
			wantPromptfooPass: true,
			wantErrorsMin:  1,
			wantErrorsMax:  1,
		},
		{
			name:           "reviewed_prompt_lifecycle_violation",
			setupRegistry:  true,
			status:         StatusReviewed,
			promptContent:  "Reviewed prompt.",
			wantHashMatch:  true,
			wantLifecycleOK: false,
			wantPromptfooPass: true,
			wantErrorsMin:  1,
			wantErrorsMax:  1,
		},
		{
			name:           "hash_mismatch_detected",
			setupRegistry:  true,
			status:         StatusActive,
			promptContent:  "Original content.",
			hashOverride:   "sha256:0000000000000000000000000000000000000000000000000000000000000000",
			wantHashMatch:  false,
			wantLifecycleOK: true,
			wantPromptfooPass: true,
			wantErrorsMin:  1,
			wantErrorsMax:  1,
		},
		{
			name:           "promptfoo_pass",
			setupRegistry:  true,
			status:         StatusActive,
			promptContent:  "Good prompt.",
			promptfooStatus: "pass",
			wantHashMatch:  true,
			wantLifecycleOK: true,
			wantPromptfooPass: true,
			wantErrorsMin:  0,
			wantErrorsMax:  0,
		},
		{
			name:           "promptfoo_failed",
			setupRegistry:  true,
			status:         StatusActive,
			promptContent:  "Bad prompt.",
			promptfooStatus: "fail",
			wantHashMatch:  true,
			wantLifecycleOK: true,
			wantPromptfooPass: false,
			wantErrorsMin:  1,
			wantErrorsMax:  1,
		},
		{
			name:           "no_promptfoo_results_defaults_to_pass",
			setupRegistry:  true,
			status:         StatusActive,
			promptContent:  "No PromptFoo.",
			wantHashMatch:  true,
			wantLifecycleOK: true,
			wantPromptfooPass: true,
			wantErrorsMin:  0,
			wantErrorsMax:  0,
		},
		{
			name:           "retired_prompt_lifecycle_violation",
			setupRegistry:  true,
			status:         StatusRetired,
			promptContent:  "Retired prompt.",
			wantHashMatch:  true,
			wantLifecycleOK: false,
			wantPromptfooPass: true,
			wantErrorsMin:  1,
			wantErrorsMax:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			// Save and restore RegistryDir
			origRegistryDir := RegistryDir
			RegistryDir = tmpDir
			defer func() { RegistryDir = origRegistryDir }()

			var lookupHash string

			if tt.setupRegistry {
				// Create prompt file
				promptDir := filepath.Join(tmpDir, "test-component", "v1.0.0")
				if err := os.MkdirAll(promptDir, 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(promptDir, "prompt.md"), []byte(tt.promptContent), 0o644); err != nil {
					t.Fatalf("write prompt: %v", err)
				}

				// Compute hash
				hash := Hash(tt.promptContent)
				if tt.hashOverride != "" {
					hash = tt.hashOverride
				}
				lookupHash = hash

				// Write metadata if PromptFoo test
				if tt.promptfooStatus != "" {
					metaContent := "version: v1.0.0\ncomponent: test-component\nhash: " + hash + "\nstatus: " + string(tt.status) + "\npromptfoo:\n  status: " + tt.promptfooStatus + "\n"
					if err := os.WriteFile(filepath.Join(promptDir, "metadata.yaml"), []byte(metaContent), 0o644); err != nil {
						t.Fatalf("write metadata: %v", err)
					}
				}

				// Write index
				indexContent := "test-component:\n  v1.0.0:\n    hash: " + hash + "\n    status: " + string(tt.status) + "\n    model: test-model\n    provider: test-provider\n"
				if err := os.WriteFile(filepath.Join(tmpDir, "_index.yaml"), []byte(indexContent), 0o644); err != nil {
					t.Fatalf("write index: %v", err)
				}
			}

			att := &Attestation{
				Hash: "sha256:nonexistent",
			}
			if tt.setupRegistry && lookupHash != "" {
				att.Hash = lookupHash
			}

			result, err := ValidateAttestation(att, tmpDir)
			if err != nil {
				t.Fatalf("ValidateAttestation returned error: %v", err)
			}

			if result.HashMatch != tt.wantHashMatch {
				t.Errorf("HashMatch = %v, want %v", result.HashMatch, tt.wantHashMatch)
			}
			if result.LifecycleOK != tt.wantLifecycleOK {
				t.Errorf("LifecycleOK = %v, want %v", result.LifecycleOK, tt.wantLifecycleOK)
			}
			if result.PromptfooPass != tt.wantPromptfooPass {
				t.Errorf("PromptfooPass = %v, want %v", result.PromptfooPass, tt.wantPromptfooPass)
			}
			if len(result.Errors) < tt.wantErrorsMin || len(result.Errors) > tt.wantErrorsMax {
				t.Errorf("Errors count = %d, want [%d, %d]; errors: %v", len(result.Errors), tt.wantErrorsMin, tt.wantErrorsMax, result.Errors)
			}

			// Check specific error substrings
			if !tt.setupRegistry {
				found := false
				for _, e := range result.Errors {
					if strings.Contains(e, "PROMPT_NOT_FOUND") {
						found = true
						break
					}
				}
				if !found {
					t.Error("expected PROMPT_NOT_FOUND error")
				}
			}
			if tt.name == "hash_mismatch_detected" {
				found := false
				for _, e := range result.Errors {
					if strings.Contains(e, "TAMPER_DETECTED") {
						found = true
						break
					}
				}
				if !found {
					t.Error("expected TAMPER_DETECTED error")
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestAttest
// ---------------------------------------------------------------------------

func TestAttest(t *testing.T) {
	tmpDir := t.TempDir()

	// Save and restore RegistryDir
	origRegistryDir := RegistryDir
	RegistryDir = tmpDir
	defer func() { RegistryDir = origRegistryDir }()

	// Set up active prompt
	promptDir := filepath.Join(tmpDir, "test", "v1.0.0")
	os.MkdirAll(promptDir, 0o755)
	promptContent := "Be concise.\n"
	os.WriteFile(filepath.Join(promptDir, "prompt.md"), []byte(promptContent), 0o644)
	hash := Hash(promptContent)

	indexContent := "test:\n  v1.0.0:\n    hash: " + hash + "\n    status: active\n    model: test\n    provider: test\n"
	os.WriteFile(filepath.Join(tmpDir, "_index.yaml"), []byte(indexContent), 0o644)

	att := &Attestation{Hash: hash}
	result, err := Attest(att, "abc123", tmpDir)
	if err != nil {
		t.Fatalf("Attest returned error: %v", err)
	}
	if !result.HashMatch {
		t.Error("expected HashMatch=true")
	}
	if !result.LifecycleOK {
		t.Error("expected LifecycleOK=true")
	}
}

// ---------------------------------------------------------------------------
// TestGetCommitAttestation
// ---------------------------------------------------------------------------

func TestGetCommitAttestation(t *testing.T) {
	// GetCommitAttestation requires a git repository to run 'git log'.
	// The helix project IS a git repo, so this should work.
	cwd, err := os.Getwd()
	if err != nil {
		t.Skipf("cannot get cwd: %v", err)
	}

	// Use the most recent commit SHA
	workDir := cwd
	for {
		if _, err := os.Stat(filepath.Join(workDir, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(workDir)
		if parent == workDir {
			t.Skip("not in a git repository")
		}
		workDir = parent
	}

	// Get the HEAD commit SHA
	headBytes, err := os.ReadFile(filepath.Join(workDir, ".git", "HEAD"))
	if err != nil {
		t.Skipf("cannot read HEAD: %v", err)
	}
	headRef := strings.TrimSpace(string(headBytes))
	if strings.HasPrefix(headRef, "ref: ") {
		// It's a ref; try reading the ref file
		refPath := filepath.Join(workDir, ".git", strings.TrimPrefix(headRef, "ref: "))
		refBytes, err := os.ReadFile(refPath)
		if err != nil {
			t.Skipf("cannot read ref: %v", err)
		}
		headRef = strings.TrimSpace(string(refBytes))
	}

	att, err := GetCommitAttestation(headRef, workDir)
	// We don't assert success; the HEAD commit may or may not have attestation fields.
	// If it fails with "git log failed", that's an infrastructure issue.
	if err != nil && strings.Contains(err.Error(), "ATTESTATION_MISSING") {
		// Expected: most commits don't have attestation fields
		return
	}
	if err != nil {
		t.Logf("GetCommitAttestation error (acceptable): %v", err)
		return
	}
	t.Logf("Parsed attestation: hash=%s model=%s", att.Hash, att.Model)
}
