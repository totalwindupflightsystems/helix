package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// TestResolvePromptPath_Layouts — nested + flat layout resolution (DF-005)
// ============================================================================

func TestResolvePromptPath_Layouts(t *testing.T) {
	tests := []struct {
		name      string
		component string
		version   string
		layout    string // "nested", "flat", "bare", "none"
		wantRel   string // expected resolved path relative to RegistryDir
		wantErr   bool
		errHas    []string
	}{
		{
			name:      "nested_present",
			component: "comp-a",
			version:   "v1.0.0",
			layout:    "nested",
			wantRel:   filepath.Join("comp-a", "v1.0.0", "prompt.md"),
		},
		{
			name:      "flat_v_prefixed_version",
			component: "comp-b",
			version:   "v1",
			layout:    "flat",
			wantRel:   filepath.Join("comp-b", "v1.md"),
		},
		{
			name:      "flat_numeric_version",
			component: "comp-c",
			version:   "1",
			layout:    "flat",
			wantRel:   filepath.Join("comp-c", "v1.md"),
		},
		{
			name:      "bare_version_file",
			component: "comp-d",
			version:   "1.0",
			layout:    "bare",
			wantRel:   filepath.Join("comp-d", "1.0.md"),
		},
		{
			name:      "nested_preferred_over_flat",
			component: "comp-e",
			version:   "v1",
			layout:    "both",
			wantRel:   filepath.Join("comp-e", "v1", "prompt.md"),
		},
		{
			name:      "neither_exists",
			component: "comp-f",
			version:   "v9",
			layout:    "none",
			wantErr:   true,
			errHas:    []string{"comp-f/v9/prompt.md", "comp-f/vv9.md", "comp-f/v9.md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			setRegistryDir(t, dir)

			switch tt.layout {
			case "nested":
				p := filepath.Join(dir, tt.component, tt.version, "prompt.md")
				if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("nested"), 0644); err != nil {
					t.Fatal(err)
				}
			case "flat":
				p := filepath.Join(dir, tt.wantRel)
				if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("flat"), 0644); err != nil {
					t.Fatal(err)
				}
			case "bare":
				p := filepath.Join(dir, tt.component, tt.version+".md")
				if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("bare"), 0644); err != nil {
					t.Fatal(err)
				}
			case "both":
				flat := filepath.Join(dir, tt.component, "v1.md")
				if err := os.MkdirAll(filepath.Dir(flat), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(flat, []byte("flat"), 0644); err != nil {
					t.Fatal(err)
				}
				nested := filepath.Join(dir, tt.component, tt.version, "prompt.md")
				if err := os.MkdirAll(filepath.Dir(nested), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(nested, []byte("nested"), 0644); err != nil {
					t.Fatal(err)
				}
			}

			got, err := ResolvePromptPath(tt.component, tt.version)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (resolved %q)", got)
				}
				for _, want := range tt.errHas {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error = %q, want containing %q", err.Error(), want)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := filepath.Join(dir, tt.wantRel)
			if got != want {
				t.Errorf("resolved = %q, want %q", got, want)
			}
		})
	}
}

// ============================================================================
// TestRegister_FlatLayoutFallback — Register reads flat layout when the
// nested default is absent (DF-005)
// ============================================================================

func TestRegister_FlatLayoutFallback(t *testing.T) {
	// writeFlatFixture creates prompts/<component>/v1.md in the registry dir.
	writeFlatFixture := func(t *testing.T, dir, component, content string) {
		t.Helper()
		p := filepath.Join(dir, component, "v1.md")
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("dry_run_nested_default_falls_back_to_flat", func(t *testing.T) {
		dir := t.TempDir()
		setRegistryDir(t, dir)
		content := "flat layout prompt content"
		writeFlatFixture(t, dir, "flat-comp", content)

		nestedDefault := filepath.Join(dir, "flat-comp", "v1", "prompt.md")
		pv, err := Register("flat-comp", "v1", nestedDefault,
			"deepseek-v4-pro", "deepseek", "specs/flat.md", &RegisterOptions{DryRun: true})
		if err != nil {
			t.Fatalf("Register with flat fallback failed: %v", err)
		}
		if pv.Hash != Hash(content) {
			t.Errorf("Hash = %q, want %q", pv.Hash, Hash(content))
		}
		// The returned PromptVersion describes the canonical registration
		// location (write side is unchanged — always nested), not the source.
		if pv.PromptPath != filepath.Join(dir, "flat-comp", "v1", "prompt.md") {
			t.Errorf("PromptPath = %q, want canonical nested path", pv.PromptPath)
		}
		// Dry run must not write the nested layout
		if _, err := os.Stat(filepath.Join(dir, "flat-comp", "v1", "prompt.md")); err == nil {
			t.Error("dry run wrote nested prompt.md")
		}
	})

	t.Run("empty_prompt_file_falls_back_to_flat", func(t *testing.T) {
		dir := t.TempDir()
		setRegistryDir(t, dir)
		content := "empty default path prompt"
		writeFlatFixture(t, dir, "flat-comp", content)

		pv, err := Register("flat-comp", "v1", "",
			"deepseek-v4-pro", "deepseek", "", &RegisterOptions{DryRun: true})
		if err != nil {
			t.Fatalf("Register with empty prompt file failed: %v", err)
		}
		if pv.Hash != Hash(content) {
			t.Errorf("Hash = %q, want %q", pv.Hash, Hash(content))
		}
	})

	t.Run("full_register_writes_canonical_nested", func(t *testing.T) {
		dir := t.TempDir()
		setRegistryDir(t, dir)
		content := "flat source, nested destination"
		writeFlatFixture(t, dir, "flat-comp", content)

		nestedDefault := filepath.Join(dir, "flat-comp", "v1", "prompt.md")
		pv, err := Register("flat-comp", "v1", nestedDefault,
			"deepseek-v4-pro", "deepseek", "specs/flat.md", &RegisterOptions{Author: "test-author"})
		if err != nil {
			t.Fatalf("Register with flat fallback failed: %v", err)
		}
		if pv.Hash != Hash(content) {
			t.Errorf("Hash = %q, want %q", pv.Hash, Hash(content))
		}
		// Canonical nested write side must be created
		for _, p := range []string{
			filepath.Join(dir, "flat-comp", "v1", "prompt.md"),
			filepath.Join(dir, "flat-comp", "v1", "metadata.yaml"),
		} {
			if _, err := os.Stat(p); err != nil {
				t.Errorf("expected %s to exist: %v", p, err)
			}
		}
		// Index entry must be created
		idx, err := loadIndex()
		if err != nil {
			t.Fatal(err)
		}
		if (*idx)["flat-comp"] == nil || (*idx)["flat-comp"]["v1"] == nil {
			t.Error("index missing flat-comp/v1 entry")
		}
		// The flat source file must not be deleted or modified
		if _, err := os.Stat(filepath.Join(dir, "flat-comp", "v1.md")); err != nil {
			t.Errorf("flat source file removed: %v", err)
		}
	})

	t.Run("explicit_nonexistent_path_still_errors", func(t *testing.T) {
		dir := t.TempDir()
		setRegistryDir(t, dir)
		writeFlatFixture(t, dir, "flat-comp", "content")

		// An explicit path that is NOT the nested default must not fall back
		// to flat resolution.
		explicit := filepath.Join(dir, "other", "prompt.md")
		_, err := Register("flat-comp", "v1", explicit,
			"deepseek-v4-pro", "deepseek", "", nil)
		if err == nil {
			t.Fatal("expected error for explicit nonexistent path, got nil")
		}
		if !strings.Contains(err.Error(), "cannot read prompt file") {
			t.Errorf("error = %q, want containing %q", err.Error(), "cannot read prompt file")
		}
	})
}

// ============================================================================
// TestList_IncludesFlatUnregistered — list surfaces flat-layout prompts that
// are absent from _index.yaml without writing anything (DF-005)
// ============================================================================

func TestList_IncludesFlatUnregistered(t *testing.T) {
	dir := t.TempDir()
	setRegistryDir(t, dir)

	// Indexed component (registered via the standard helper)
	setupRegisteredPrompt(t, dir, "indexed-comp", "v1.0.0", "indexed content", StatusActive)

	// Flat-only components on disk, absent from the index
	for _, c := range []string{"flat-a", "flat-b"} {
		p := filepath.Join(dir, c, "v1.md")
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c+" content"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name    string
		filter  ListFilter
		wantLen int
		check   func(t *testing.T, results []*PromptVersion)
	}{
		{
			name:    "no_filter_includes_flat",
			filter:  ListFilter{},
			wantLen: 3,
			check: func(t *testing.T, results []*PromptVersion) {
				statuses := map[string]LifecycleStatus{}
				for _, r := range results {
					statuses[r.Component] = r.Status
				}
				if statuses["flat-a"] != StatusUnregistered {
					t.Errorf("flat-a status = %q, want %q", statuses["flat-a"], StatusUnregistered)
				}
				if statuses["flat-b"] != StatusUnregistered {
					t.Errorf("flat-b status = %q, want %q", statuses["flat-b"], StatusUnregistered)
				}
				if statuses["indexed-comp"] != StatusActive {
					t.Errorf("indexed-comp status = %q, want %q", statuses["indexed-comp"], StatusActive)
				}
				// Flat entries must carry the content hash of their file
				for _, r := range results {
					if r.Component == "flat-a" && r.Hash != Hash("flat-a content") {
						t.Errorf("flat-a hash = %q, want %q", r.Hash, Hash("flat-a content"))
					}
				}
			},
		},
		{
			name:    "component_filter_flat",
			filter:  ListFilter{Component: "flat-a"},
			wantLen: 1,
			check: func(t *testing.T, results []*PromptVersion) {
				if results[0].Component != "flat-a" || results[0].Status != StatusUnregistered {
					t.Errorf("got %s/%s, want flat-a unregistered", results[0].Component, results[0].Status)
				}
			},
		},
		{
			name:    "status_active_excludes_flat",
			filter:  ListFilter{Status: StatusActive},
			wantLen: 1,
		},
		{
			name:    "status_unregistered_only_flat",
			filter:  ListFilter{Status: StatusUnregistered},
			wantLen: 2,
		},
		{
			name:    "model_filter_excludes_flat",
			filter:  ListFilter{Model: "deepseek-v4-pro"},
			wantLen: 1,
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

	// List must not have written anything: the index still contains only
	// indexed-comp, and no metadata was created for the flat components.
	idxData, err := os.ReadFile(filepath.Join(dir, "_index.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(idxData), "flat-a") || strings.Contains(string(idxData), "flat-b") {
		t.Error("List wrote flat components into _index.yaml")
	}
	if _, err := os.Stat(filepath.Join(dir, "flat-a", "v1", "metadata.yaml")); err == nil {
		t.Error("List created nested metadata for flat component")
	}
}
