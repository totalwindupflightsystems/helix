package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// helper: override RegistryDir for isolation, restores on cleanup
func setRegistryDir(t *testing.T, dir string) {
	t.Helper()
	orig := RegistryDir
	RegistryDir = dir
	t.Cleanup(func() { RegistryDir = orig })
}

// helper: write a prompt file and return its path
func writePromptFixture(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "prompt.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	return path
}

// helper: create a full registered prompt (prompt.md + metadata.yaml + _index.yaml)
func setupRegisteredPrompt(t *testing.T, dir, component, version, content string, status LifecycleStatus) string {
	t.Helper()
	setRegistryDir(t, dir)
	path := writePromptFixture(t, filepath.Join(dir, component, version), content)
	hash := Hash(content)

	// Create metadata.yaml
	metaDir := filepath.Join(dir, component, version)
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatalf("mkdir meta: %v", err)
	}
	meta := Metadata{
		Version:   version,
		Component: component,
		Hash:      hash,
		Model:     "deepseek-v4-pro",
		Provider:  "deepseek",
		Status:    status,
		CreatedAt: time.Now().UTC(),
	}
	if err := writeMetadata(filepath.Join(metaDir, "metadata.yaml"), &meta); err != nil {
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

	return path
}

// ============================================================================
// TestRegister
// ============================================================================

func TestRegister(t *testing.T) {
	tests := []struct {
		name          string
		component     string
		version       string
		content       string
		model         string
		provider      string
		specRef       string
		dryRun        bool
		promptFile    string // override prompt file path
		wantErr       bool
		errContains   string
		wantStatus    LifecycleStatus
	}{
		{
			name:      "successful_register",
			component: "test-comp",
			version:   "v1.0.0",
			content:   "You are a test assistant.",
			model:     "deepseek-v4-pro",
			provider:  "deepseek",
			specRef:   "specs/test.md",
			wantErr:   false,
			wantStatus: StatusDraft,
		},
		{
			name:      "dry_run_no_files_written",
			component: "dry-comp",
			version:   "v1.0.0",
			content:   "You are a dry-run assistant.",
			model:     "mini-max-m3",
			provider:  "minimax",
			specRef:   "specs/dry.md",
			dryRun:    true,
			wantErr:   false,
			wantStatus: StatusDraft,
		},
		{
			name:        "nonexistent_prompt_file",
			component:   "bad-comp",
			version:     "v1.0.0",
			content:     "",
			model:       "deepseek-v4-pro",
			provider:    "deepseek",
			specRef:     "specs/bad.md",
			promptFile:  "/nonexistent/path/prompt.md",
			wantErr:     true,
			errContains: "cannot read prompt file",
		},
		{
			name:      "register_with_author_and_workitem",
			component: "auth-comp",
			version:   "v2.0.0",
			content:   "You are an authenticated prompt.",
			model:     "glm-5.2",
			provider:  "zai-glm",
			specRef:   "specs/auth.md",
			wantErr:   false,
			wantStatus: StatusDraft,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			setRegistryDir(t, tmpDir)

			promptFile := tt.promptFile
			if promptFile == "" {
				promptFile = writePromptFixture(t, tmpDir, tt.content)
			}

			opts := &RegisterOptions{
				DryRun: tt.dryRun,
				Author: "test-author",
				WorkItem: "WI-001",
			}

			got, err := Register(tt.component, tt.version, promptFile, tt.model, tt.provider, tt.specRef, opts)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %v, want containing %q", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.Version != tt.version {
				t.Errorf("Version = %q, want %q", got.Version, tt.version)
			}
			if got.Component != tt.component {
				t.Errorf("Component = %q, want %q", got.Component, tt.component)
			}
			if got.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", got.Status, tt.wantStatus)
			}
			if got.Model != tt.model {
				t.Errorf("Model = %q, want %q", got.Model, tt.model)
			}
			if got.Provider != tt.provider {
				t.Errorf("Provider = %q, want %q", got.Provider, tt.provider)
			}
			if got.Hash == "" {
				t.Error("Hash is empty")
			}

			if tt.dryRun {
				// Verify no files were written
				promptPath := filepath.Join(tmpDir, tt.component, tt.version, "prompt.md")
				if _, err := os.Stat(promptPath); err == nil {
					t.Error("dry run wrote prompt.md")
				}
			} else {
				// Verify files were written
				promptPath := filepath.Join(tmpDir, tt.component, tt.version, "prompt.md")
				if _, err := os.Stat(promptPath); err != nil {
					t.Errorf("prompt.md not created: %v", err)
				}
				metaPath := filepath.Join(tmpDir, tt.component, tt.version, "metadata.yaml")
				if _, err := os.Stat(metaPath); err != nil {
					t.Errorf("metadata.yaml not created: %v", err)
				}
				idxPath := filepath.Join(tmpDir, "_index.yaml")
				if _, err := os.Stat(idxPath); err != nil {
					t.Errorf("_index.yaml not created: %v", err)
				}
			}
		})
	}
}

func TestRegister_NilOpts(t *testing.T) {
	tmpDir := t.TempDir()
	setRegistryDir(t, tmpDir)

	promptPath := writePromptFixture(t, tmpDir, "You are a minimal assistant.")
	got, err := Register("nil-comp", "v1.0.0", promptPath, "deepseek-v4-pro", "deepseek", "", nil)
	if err != nil {
		t.Fatalf("unexpected error with nil opts: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil result")
	}
}

// ============================================================================
// TestLookup
// ============================================================================

func TestLookup_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	setRegistryDir(t, tmpDir)

	_, err := Lookup("sha256:deadbeef")
	if err == nil {
		t.Fatal("expected error for nonexistent hash")
	}
	if !strings.Contains(err.Error(), "PROMPT_NOT_FOUND") {
		t.Errorf("error = %v, want containing PROMPT_NOT_FOUND", err)
	}
}

// ============================================================================
// TestLookupByComponent
// ============================================================================

func TestLookupByComponent(t *testing.T) {
	tests := []struct {
		name        string
		setup       bool
		component   string
		version     string
		content     string
		wantErr     bool
		errContains string
	}{
		{
			name:      "found",
			setup:     true,
			component: "lookup-comp",
			version:   "v1.0.0",
			content:   "You are a lookup target.",
			wantErr:   false,
		},
		{
			name:        "not_found_wrong_component",
			setup:       true,
			component:   "nonexistent-comp",
			version:     "v1.0.0",
			content:     "You are a lookup target.",
			wantErr:     true,
			errContains: "PROMPT_NOT_FOUND",
		},
		{
			name:        "not_found_wrong_version",
			setup:       true,
			component:   "lookup-comp",
			version:     "v99.0.0",
			content:     "You are a lookup target.",
			wantErr:     true,
			errContains: "PROMPT_NOT_FOUND",
		},
		{
			name:        "not_found_no_setup",
			setup:       false,
			component:   "any-comp",
			version:     "v1.0.0",
			content:     "",
			wantErr:     true,
			errContains: "PROMPT_NOT_FOUND",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			setRegistryDir(t, tmpDir)

			if tt.setup {
				setupRegisteredPrompt(t, tmpDir, "lookup-comp", "v1.0.0", tt.content, StatusActive)
			}

			got, err := LookupByComponent(tt.component, tt.version)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %v, want containing %q", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Component != tt.component {
				t.Errorf("Component = %q, want %q", got.Component, tt.component)
			}
			if got.Version != tt.version {
				t.Errorf("Version = %q, want %q", got.Version, tt.version)
			}
		})
	}
}

// ============================================================================
// TestList
// ============================================================================

func TestList(t *testing.T) {
	// Setup: register two components with different statuses and models
	tmpDir := t.TempDir()
	setRegistryDir(t, tmpDir)

	// Register comp-a v1.0.0 first
	savedIdx := Index{}
	{
		tmpRegDir := t.TempDir()
		setRegistryDir(t, tmpRegDir)
		_ = writePromptFixture(t, filepath.Join(tmpRegDir, "comp-a", "v1.0.0"), "Prompt A v1")
		h1 := Hash("Prompt A v1")
		savedIdx["comp-a"] = map[string]*PromptEntry{
			"v1.0.0": {Hash: h1, Status: StatusActive, Model: "deepseek-v4-pro", Provider: "deepseek"},
		}
		// Write metadata + index
		metaDir1 := filepath.Join(tmpRegDir, "comp-a", "v1.0.0")
		os.MkdirAll(metaDir1, 0755)
		writeMetadata(filepath.Join(metaDir1, "metadata.yaml"), &Metadata{
			Version: "v1.0.0", Component: "comp-a", Hash: h1,
			Model: "deepseek-v4-pro", Provider: "deepseek", Status: StatusActive, CreatedAt: time.Now().UTC(),
		})
	}

	// Now use the main tmpDir for everything
	setRegistryDir(t, tmpDir)
	os.MkdirAll(filepath.Join(tmpDir, "comp-a", "v1.0.0"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "comp-a", "v1.0.0", "prompt.md"), []byte("Prompt A v1"), 0644)
	h1 := Hash("Prompt A v1")
	writeMetadata(filepath.Join(tmpDir, "comp-a", "v1.0.0", "metadata.yaml"), &Metadata{
		Version: "v1.0.0", Component: "comp-a", Hash: h1,
		Model: "deepseek-v4-pro", Provider: "deepseek", Status: StatusActive, CreatedAt: time.Now().UTC(),
	})

	// Register comp-b v1.0.0
	os.MkdirAll(filepath.Join(tmpDir, "comp-b", "v1.0.0"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "comp-b", "v1.0.0", "prompt.md"), []byte("Prompt B v1"), 0644)
	hB := Hash("Prompt B v1")
	writeMetadata(filepath.Join(tmpDir, "comp-b", "v1.0.0", "metadata.yaml"), &Metadata{
		Version: "v1.0.0", Component: "comp-b", Hash: hB,
		Model: "deepseek-v4-pro", Provider: "deepseek", Status: StatusDraft, CreatedAt: time.Now().UTC(),
	})

	// Add comp-a v2.0.0
	content2 := "Prompt A v2"
	os.MkdirAll(filepath.Join(tmpDir, "comp-a", "v2.0.0"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "comp-a", "v2.0.0", "prompt.md"), []byte(content2), 0644)
	hash2 := Hash(content2)
	writeMetadata(filepath.Join(tmpDir, "comp-a", "v2.0.0", "metadata.yaml"), &Metadata{
		Version: "v2.0.0", Component: "comp-a", Hash: hash2,
		Model: "glm-5.2", Provider: "zai-glm", Status: StatusDeprecated, CreatedAt: time.Now().UTC(),
	})

	// Build complete index manually
	idx := &Index{
		"comp-a": {
			"v1.0.0": {Hash: h1, Status: StatusActive, Model: "deepseek-v4-pro", Provider: "deepseek"},
			"v2.0.0": {Hash: hash2, Status: StatusDeprecated, Model: "glm-5.2", Provider: "zai-glm"},
		},
		"comp-b": {
			"v1.0.0": {Hash: hB, Status: StatusDraft, Model: "deepseek-v4-pro", Provider: "deepseek"},
		},
	}
	saveIndex(idx)

	tests := []struct {
		name    string
		filter  ListFilter
		wantLen int
		check   func(t *testing.T, results []*PromptVersion)
	}{
		{
			name:    "no_filter_returns_all",
			filter:  ListFilter{},
			wantLen: 3,
		},
		{
			name:    "filter_by_component",
			filter:  ListFilter{Component: "comp-a"},
			wantLen: 2,
			check: func(t *testing.T, results []*PromptVersion) {
				for _, r := range results {
					if r.Component != "comp-a" {
						t.Errorf("got component %q, want comp-a", r.Component)
					}
				}
			},
		},
		{
			name:    "filter_by_status_active",
			filter:  ListFilter{Status: StatusActive},
			wantLen: 1,
			check: func(t *testing.T, results []*PromptVersion) {
				if results[0].Status != StatusActive {
					t.Errorf("Status = %q, want %q", results[0].Status, StatusActive)
				}
			},
		},
		{
			name:    "filter_by_status_draft",
			filter:  ListFilter{Status: StatusDraft},
			wantLen: 1,
		},
		{
			name:    "filter_by_model",
			filter:  ListFilter{Model: "glm-5.2"},
			wantLen: 1,
		},
		{
			name:    "combined_filter",
			filter:  ListFilter{Component: "comp-a", Status: StatusDeprecated},
			wantLen: 1,
		},
		{
			name:    "filter_no_match",
			filter:  ListFilter{Component: "comp-a", Status: StatusRetired},
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := List(tt.filter)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(results) != tt.wantLen {
				t.Errorf("len = %d, want %d", len(results), tt.wantLen)
			}
			if tt.check != nil {
				tt.check(t, results)
			}
		})
	}
}

// ============================================================================
// TestUpdateStatus
// ============================================================================

func TestUpdateStatus(t *testing.T) {
	tests := []struct {
		name        string
		component   string
		version     string
		newStatus   LifecycleStatus
		wantErr     bool
		errContains string
		checkAttestedAt bool // verify AttestedAt is set
	}{
		{
			name:      "update_draft_to_reviewed",
			component: "update-comp",
			version:   "v1.0.0",
			newStatus: StatusReviewed,
			wantErr:   false,
		},
		{
			name:      "update_to_attested_sets_timestamp",
			component: "update-comp",
			version:   "v1.0.0",
			newStatus: StatusAttested,
			wantErr:   false,
			checkAttestedAt: true,
		},
		{
			name:        "not_found_component",
			component:   "nonexistent",
			version:     "v1.0.0",
			newStatus:   StatusActive,
			wantErr:     true,
			errContains: "PROMPT_NOT_FOUND",
		},
		{
			name:        "not_found_version",
			component:   "update-comp",
			version:     "v99.0.0",
			newStatus:   StatusActive,
			wantErr:     true,
			errContains: "PROMPT_NOT_FOUND",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			setRegistryDir(t, tmpDir)

			// Always set up the initial prompt for "update-comp"
			setupRegisteredPrompt(t, tmpDir, "update-comp", "v1.0.0", "Test prompt for status update", StatusDraft)

			err := UpdateStatus(tt.component, tt.version, tt.newStatus)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %v, want containing %q", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify index was updated
			idx, _ := loadIndex()
			entry := (*idx)[tt.component][tt.version]
			if entry.Status != tt.newStatus {
				t.Errorf("index status = %q, want %q", entry.Status, tt.newStatus)
			}

			// Verify metadata was updated
			if tt.checkAttestedAt {
				metaPath := filepath.Join(tmpDir, tt.component, tt.version, "metadata.yaml")
				meta, err := readMetadata(metaPath)
				if err != nil {
					t.Fatalf("read Metadata: %v", err)
				}
				if meta.AttestedAt.IsZero() {
					t.Error("AttestedAt is zero after status change to attested")
				}
			}
		})
	}
}

// ============================================================================
// TestTransitionStatus
// ============================================================================

func TestTransitionStatus(t *testing.T) {
	tests := []struct {
		name        string
		setupStatus LifecycleStatus
		workItem    string
		to          LifecycleStatus
		trigger     string
		wantErr     bool
		errContains string
	}{
		{
			name:        "valid_draft_to_proposed",
			setupStatus: StatusDraft,
			to:          StatusProposed,
			trigger:     "review-request",
			wantErr:     false,
		},
		{
			name:        "invalid_skip_proposed",
			setupStatus: StatusDraft,
			to:          StatusReviewed,
			trigger:     "skip",
			wantErr:     true,
			errContains: "invalid transition",
		},
		{
			name:        "not_found",
			setupStatus: StatusDraft,
			to:          StatusProposed,
			trigger:     "test",
			wantErr:     true,
			errContains: "PROMPT_NOT_FOUND",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			setRegistryDir(t, tmpDir)

			component := "trans-comp"
			version := "v1.0.0"

			if tt.setupStatus != "" && tt.name != "not_found" {
				setupRegisteredPrompt(t, tmpDir, component, version, "Test prompt for transition", tt.setupStatus)
				// If workItem is set, update metadata
				if tt.workItem != "" {
					metaPath := filepath.Join(tmpDir, component, version, "metadata.yaml")
					meta, _ := readMetadata(metaPath)
					meta.WorkItem = tt.workItem
					writeMetadata(metaPath, meta)
				}
			}

			// For "not_found" case, query a different component
			queryComponent := component
			if tt.name == "not_found" {
				queryComponent = "nonexistent"
			}

			err := TransitionStatus(queryComponent, version, tt.to, tt.trigger)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %v, want containing %q", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify status was updated
			idx, _ := loadIndex()
			if entry, ok := (*idx)[component][version]; !ok || entry.Status != tt.to {
				t.Errorf("expected status %q after transition", tt.to)
			}
		})
	}
}

func TestTransitionStatus_Rollback(t *testing.T) {
	tmpDir := t.TempDir()
	setRegistryDir(t, tmpDir)

	component := "rollback-comp"
	version := "v1.0.0"

	// Rollback WITHOUT WorkItem — should fail
	t.Run("rollback_without_workitem_fails", func(t *testing.T) {
		setupRegisteredPrompt(t, tmpDir, component, version, "Test prompt for rollback", StatusDeprecated)
		err := TransitionStatus(component, version, StatusActive, "rollback")
		if err == nil {
			t.Fatal("expected error for rollback without WorkItem")
		}
		if !strings.Contains(err.Error(), "human approval") {
			t.Errorf("error = %v, want containing 'human approval'", err)
		}
	})

	// Rollback WITH WorkItem — should succeed
	t.Run("rollback_with_workitem_succeeds", func(t *testing.T) {
		// Re-setup with WorkItem in metadata
		setupRegisteredPrompt(t, tmpDir, component, version, "Test prompt for rollback", StatusDeprecated)
		metaPath := filepath.Join(tmpDir, component, version, "metadata.yaml")
		meta, err := readMetadata(metaPath)
		if err != nil {
			t.Fatalf("readMetadata: %v", err)
		}
		meta.WorkItem = "WI-ROLLBACK-001"
		writeMetadata(metaPath, meta)

		err = TransitionStatus(component, version, StatusActive, "rollback-approved")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		idx, _ := loadIndex()
		if entry, ok := (*idx)[component][version]; !ok || entry.Status != StatusActive {
			t.Errorf("expected status active after rollback, got %v", entry)
		}
	})
}

// ============================================================================
// TestLoadIndex
// ============================================================================

func TestLoadIndex(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(dir string)
		wantNil  bool // expect *Index to be nil
		wantErr  bool
	}{
		{
			name:    "no_file_returns_empty_index",
			setup:   func(dir string) {},
			wantNil: false,
			wantErr: false,
		},
		{
			name: "empty_file_returns_empty_index",
			setup: func(dir string) {
				os.WriteFile(filepath.Join(dir, "_index.yaml"), []byte(""), 0644)
			},
			wantNil: false,
			wantErr: false,
		},
		{
			name: "whitespace_only_returns_empty_index",
			setup: func(dir string) {
				os.WriteFile(filepath.Join(dir, "_index.yaml"), []byte("  \n  \n"), 0644)
			},
			wantNil: false,
			wantErr: false,
		},
		{
			name: "invalid_yaml_returns_error",
			setup: func(dir string) {
				os.WriteFile(filepath.Join(dir, "_index.yaml"), []byte(":invalid: yaml: {{"), 0644)
			},
			wantNil: true,
			wantErr: true,
		},
		{
			name: "valid_yaml_loads",
			setup: func(dir string) {
				yaml := "comp-a:\n  v1.0.0:\n    hash: sha256:abc\n    status: active\n    model: deepseek-v4-pro\n    provider: deepseek\n"
				os.WriteFile(filepath.Join(dir, "_index.yaml"), []byte(yaml), 0644)
			},
			wantNil: false,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			setRegistryDir(t, tmpDir)

			tt.setup(tmpDir)

			idx, err := loadIndex()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.wantNil && idx != nil {
					t.Error("expected nil index on error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if idx == nil {
				t.Fatal("expected non-nil index")
			}
			if tt.name == "valid_yaml_loads" {
				if len(*idx) == 0 {
					t.Error("expected non-empty index for valid yaml")
				}
			}
		})
	}
}

// ============================================================================
// TestSaveIndex
// ============================================================================

func TestSaveIndex(t *testing.T) {
	tests := []struct {
		name  string
		index *Index
	}{
		{
			name:  "nil_index",
			index: nil,
		},
		{
			name: "valid_index",
			index: &Index{
				"comp-a": {
					"v1.0.0": &PromptEntry{
						Hash:     "sha256:abc123",
						Status:   StatusActive,
						Model:    "deepseek-v4-pro",
						Provider: "deepseek",
					},
				},
			},
		},
		{
			name:  "empty_index",
			index: &Index{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			setRegistryDir(t, tmpDir)

			err := saveIndex(tt.index)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify file was written
			idxPath := filepath.Join(tmpDir, "_index.yaml")
			data, err := os.ReadFile(idxPath)
			if err != nil {
				t.Fatalf("_index.yaml not created: %v", err)
			}
			if len(data) == 0 {
				t.Error("_index.yaml is empty")
			}

			// Verify it round-trips
			reloaded, err := loadIndex()
			if err != nil {
				t.Fatalf("reload error: %v", err)
			}
			if reloaded == nil {
				t.Fatal("reloaded index is nil")
			}
		})
	}
}

// ============================================================================
// TestWriteMetadata
// ============================================================================

func TestWriteMetadata(t *testing.T) {
	tmpDir := t.TempDir()

	meta := &Metadata{
		Version:   "v1.0.0",
		Component: "test-comp",
		Hash:      "sha256:abc123",
		Model:     "deepseek-v4-pro",
		Provider:  "deepseek",
		Author:    "test-author",
		SpecRef:   "specs/test.md",
		Status:    StatusDraft,
		CreatedAt: time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC),
	}

	metaPath := filepath.Join(tmpDir, "test-comp", "v1.0.0", "metadata.yaml")
	err := writeMetadata(metaPath, meta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read back
	reloaded, err := readMetadata(metaPath)
	if err != nil {
		t.Fatalf("readMetadata: %v", err)
	}
	if reloaded.Version != meta.Version {
		t.Errorf("Version = %q, want %q", reloaded.Version, meta.Version)
	}
	if reloaded.Component != meta.Component {
		t.Errorf("Component = %q, want %q", reloaded.Component, meta.Component)
	}
	if reloaded.Hash != meta.Hash {
		t.Errorf("Hash = %q, want %q", reloaded.Hash, meta.Hash)
	}
}

func TestReadMetadata_NotFound(t *testing.T) {
	_, err := readMetadata("/nonexistent/path/metadata.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestReadMetadata_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "metadata.yaml")
	os.WriteFile(path, []byte(":invalid: yaml: {{{"), 0644)

	_, err := readMetadata(path)
	if err == nil {
		t.Fatal("expected error for invalid yaml")
	}
}

// ============================================================================
// TestWritePrompt
// ============================================================================

func TestWritePrompt(t *testing.T) {
	tmpDir := t.TempDir()
	promptPath := filepath.Join(tmpDir, "comp", "v1.0.0", "prompt.md")
	content := "You are a test prompt."

	err := writePrompt(promptPath, content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read back
	data, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if string(data) != content {
		t.Errorf("content = %q, want %q", string(data), content)
	}
}

// ============================================================================
// TestEntryToPromptVersion
// ============================================================================

func TestEntryToPromptVersion(t *testing.T) {
	entry := &PromptEntry{
		Hash:     "sha256:abc",
		Status:   StatusActive,
		Model:    "deepseek-v4-pro",
		Provider: "deepseek",
	}

	pv := entryToPromptVersion("comp", "v1.0.0", entry)

	if pv.Version != "v1.0.0" {
		t.Errorf("Version = %q, want v1.0.0", pv.Version)
	}
	if pv.Component != "comp" {
		t.Errorf("Component = %q, want comp", pv.Component)
	}
	if pv.Hash != "sha256:abc" {
		t.Errorf("Hash = %q, want sha256:abc", pv.Hash)
	}
	if pv.Status != StatusActive {
		t.Errorf("Status = %q, want active", pv.Status)
	}
	if pv.Model != "deepseek-v4-pro" {
		t.Errorf("Model = %q, want deepseek-v4-pro", pv.Model)
	}
	if pv.Provider != "deepseek" {
		t.Errorf("Provider = %q, want deepseek", pv.Provider)
	}
}

// ============================================================================
// TestRegister_Verbose (covers Verbose branches)
// ============================================================================

func TestRegister_Verbose(t *testing.T) {
	tmpDir := t.TempDir()
	setRegistryDir(t, tmpDir)

	origVerbose := Verbose
	Verbose = true
	defer func() { Verbose = origVerbose }()

	promptPath := writePromptFixture(t, tmpDir, "You are a verbose test prompt.")

	// Dry run with verbose
	opts := &RegisterOptions{DryRun: true}
	_, err := Register("verbose-comp", "v1.0.0", promptPath, "deepseek-v4-pro", "deepseek", "specs/test.md", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Regular with verbose
	_, err = Register("verbose-comp", "v1.0.0", promptPath, "deepseek-v4-pro", "deepseek", "specs/test.md", &RegisterOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ============================================================================
// TestUpdateStatus_Verbose
// ============================================================================

func TestUpdateStatus_Verbose(t *testing.T) {
	tmpDir := t.TempDir()
	setRegistryDir(t, tmpDir)

	origVerbose := Verbose
	Verbose = true
	defer func() { Verbose = origVerbose }()

	setupRegisteredPrompt(t, tmpDir, "verbose-comp", "v1.0.0", "Test", StatusDraft)

	err := UpdateStatus("verbose-comp", "v1.0.0", StatusReviewed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ============================================================================
// TestTransitionStatus_Verbose
// ============================================================================

func TestTransitionStatus_Verbose(t *testing.T) {
	tmpDir := t.TempDir()
	setRegistryDir(t, tmpDir)

	origVerbose := Verbose
	Verbose = true
	defer func() { Verbose = origVerbose }()

	setupRegisteredPrompt(t, tmpDir, "verbose-comp", "v1.0.0", "Test", StatusDraft)

	err := TransitionStatus("verbose-comp", "v1.0.0", StatusProposed, "verbose-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
