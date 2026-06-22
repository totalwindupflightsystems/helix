package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupMultiVersionPrompt registers two versions of the same component by
// properly merging the index (setupRegisteredPrompt overwrites the index).
func setupMultiVersionPrompt(t *testing.T, dir, component, v1, content1, v2, content2 string, status LifecycleStatus) {
	t.Helper()
	setRegistryDir(t, dir)

	// Register v1 via the normal helper
	setupRegisteredPrompt(t, dir, component, v1, content1, status)

	// Now manually add v2 to the existing index
	hash2 := Hash(content2)
	metaDir2 := filepath.Join(dir, component, v2)
	if err := os.MkdirAll(metaDir2, 0755); err != nil {
		t.Fatalf("mkdir meta v2: %v", err)
	}
	meta2 := Metadata{
		Version:   v2,
		Component: component,
		Hash:      hash2,
		Model:     "deepseek-v4-pro",
		Provider:  "deepseek",
		Status:    status,
		CreatedAt: time.Now().UTC(),
	}
	if err := writeMetadata(filepath.Join(metaDir2, "metadata.yaml"), &meta2); err != nil {
		t.Fatalf("writeMetadata v2: %v", err)
	}
	// Write prompt.md for v2
	if err := os.WriteFile(filepath.Join(metaDir2, "prompt.md"), []byte(content2), 0644); err != nil {
		t.Fatalf("write prompt v2: %v", err)
	}

	// Load existing index and append v2
	idx, err := loadIndex()
	if err != nil {
		t.Fatalf("loadIndex: %v", err)
	}
	if (*idx)[component] == nil {
		(*idx)[component] = map[string]*PromptEntry{}
	}
	(*idx)[component][v2] = &PromptEntry{
		Hash:     hash2,
		Status:   status,
		Model:    "deepseek-v4-pro",
		Provider: "deepseek",
	}
	if err := saveIndex(idx); err != nil {
		t.Fatalf("saveIndex: %v", err)
	}
}

// =============================================================================
// Diff tests
// =============================================================================

// TestDiff_ComponentNotFound verifies that Diff returns an error when the
// component doesn't exist in the registry.
func TestDiff_ComponentNotFound(t *testing.T) {
	dir := t.TempDir()
	setRegistryDir(t, dir)

	_, err := Diff("nonexistent", "v1", "v2")
	if err == nil {
		t.Fatal("expected error for nonexistent component")
	}
	if !strings.Contains(err.Error(), "PROMPT_NOT_FOUND") {
		t.Errorf("error should mention PROMPT_NOT_FOUND, got: %v", err)
	}
}

// TestDiff_VersionNotFound verifies that Diff returns an error when one of the
// versions doesn't exist.
func TestDiff_VersionNotFound(t *testing.T) {
	dir := t.TempDir()
	setRegistryDir(t, dir)

	content := "# Test Prompt"
	setupRegisteredPrompt(t, dir, "comp", "v1.0.0", content, StatusActive)

	_, err := Diff("comp", "v1.0.0", "v9.9.9")
	if err == nil {
		t.Fatal("expected error for nonexistent version")
	}
}

// TestDiff_ContentDiff verifies that Diff detects content differences
// and produces a ContentDiff string.
func TestDiff_ContentDiff(t *testing.T) {
	dir := t.TempDir()

	content1 := "# Prompt v1\n\nHello world.\n"
	content2 := "# Prompt v2\n\nHello world.\nGoodbye.\n"
	setupMultiVersionPrompt(t, dir, "comp", "v1.0.0", content1, "v2.0.0", content2, StatusActive)

	diff, err := Diff("comp", "v1.0.0", "v2.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff.Component != "comp" {
		t.Errorf("component = %q, want %q", diff.Component, "comp")
	}
	if diff.FromVersion != "v1.0.0" {
		t.Errorf("from = %q, want %q", diff.FromVersion, "v1.0.0")
	}
	if diff.ToVersion != "v2.0.0" {
		t.Errorf("to = %q, want %q", diff.ToVersion, "v2.0.0")
	}
	if diff.ContentDiff == "" {
		t.Error("expected non-empty ContentDiff")
	}
}

// TestDiff_MetadataDiff verifies that Diff detects metadata changes.
func TestDiff_MetadataDiff(t *testing.T) {
	dir := t.TempDir()

	content := "# Same Content"
	setupMultiVersionPrompt(t, dir, "comp", "v1.0.0", content, "v2.0.0", content, StatusActive)

	// Change metadata of v2
	v2pv, err := LookupByComponent("comp", "v2.0.0")
	if err != nil {
		t.Fatalf("lookup v2: %v", err)
	}
	v2meta, err := readMetadata(v2pv.MetadataPath)
	if err != nil {
		t.Fatalf("read v2 metadata: %v", err)
	}
	v2meta.Model = "glm-5.2"
	v2meta.Provider = "zai-glm"
	if err := writeMetadata(v2pv.MetadataPath, v2meta); err != nil {
		t.Fatalf("write v2 metadata: %v", err)
	}

	diff, err := Diff("comp", "v1.0.0", "v2.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(diff.MetadataDiff, "model") {
		t.Errorf("MetadataDiff should mention model change: %q", diff.MetadataDiff)
	}
	if !strings.Contains(diff.MetadataDiff, "provider") {
		t.Errorf("MetadataDiff should mention provider change: %q", diff.MetadataDiff)
	}
}

// TestDiff_SameContent verifies that Diff with identical content and metadata
// produces "(no metadata changes)".
func TestDiff_SameContent(t *testing.T) {
	dir := t.TempDir()

	content := "# Same Prompt"
	setupMultiVersionPrompt(t, dir, "comp", "v1.0.0", content, "v2.0.0", content, StatusActive)

	diff, err := Diff("comp", "v1.0.0", "v2.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff.MetadataDiff != "(no metadata changes)" {
		t.Errorf("MetadataDiff = %q, want %q", diff.MetadataDiff, "(no metadata changes)")
	}
}

// =============================================================================
// ListVersions tests
// =============================================================================

// TestListVersions_EmptyRegistry verifies that ListVersions returns nil,nil
// when the index doesn't exist (fresh repo).
func TestListVersions_EmptyRegistry(t *testing.T) {
	dir := t.TempDir()
	setRegistryDir(t, dir)

	versions, err := ListVersions("any-component")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if versions != nil {
		t.Errorf("expected nil for empty registry, got %v", versions)
	}
}

// TestListVersions_SingleVersion verifies that ListVersions returns one entry
// for a component with a single version.
func TestListVersions_SingleVersion(t *testing.T) {
	dir := t.TempDir()
	setRegistryDir(t, dir)

	setupRegisteredPrompt(t, dir, "comp", "v1.0.0", "# Test", StatusActive)

	versions, err := ListVersions("comp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("expected 1 version, got %d", len(versions))
	}
	if versions[0].Version != "v1.0.0" {
		t.Errorf("version = %q, want %q", versions[0].Version, "v1.0.0")
	}
}

// TestListVersions_MultipleVersionsSorted verifies that ListVersions returns
// versions sorted by version string.
func TestListVersions_MultipleVersionsSorted(t *testing.T) {
	dir := t.TempDir()

	content := "# Test"
	setupMultiVersionPrompt(t, dir, "comp", "v1.0.0", content, "v2.0.0", content, StatusActive)

	// Add a third version manually
	idx, err := loadIndex()
	if err != nil {
		t.Fatalf("loadIndex: %v", err)
	}
	hash3 := Hash(content)
	(*idx)["comp"]["v1.5.0"] = &PromptEntry{
		Hash: hash3, Status: StatusActive, Model: "deepseek-v4-pro", Provider: "deepseek",
	}
	if err := saveIndex(idx); err != nil {
		t.Fatalf("saveIndex: %v", err)
	}
	// Write metadata and prompt for v1.5.0
	metaDir3 := filepath.Join(dir, "comp", "v1.5.0")
	if err := os.MkdirAll(metaDir3, 0755); err != nil {
		t.Fatalf("mkdir v1.5.0: %v", err)
	}
	meta3 := Metadata{
		Version:   "v1.5.0",
		Component: "comp",
		Hash:      hash3,
		Model:     "deepseek-v4-pro",
		Provider:  "deepseek",
		Status:    StatusActive,
		CreatedAt: time.Now().UTC(),
	}
	if err := writeMetadata(filepath.Join(metaDir3, "metadata.yaml"), &meta3); err != nil {
		t.Fatalf("writeMetadata v1.5.0: %v", err)
	}
	if err := os.WriteFile(filepath.Join(metaDir3, "prompt.md"), []byte(content), 0644); err != nil {
		t.Fatalf("write prompt v1.5.0: %v", err)
	}

	versions, err := ListVersions("comp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(versions))
	}
	// Must be sorted by version string (ascending)
	if versions[0].Version > versions[1].Version {
		t.Errorf("versions not sorted: [0]=%q, [1]=%q", versions[0].Version, versions[1].Version)
	}
	if versions[1].Version > versions[2].Version {
		t.Errorf("versions not sorted: [1]=%q, [2]=%q", versions[1].Version, versions[2].Version)
	}
}

// TestListVersions_ComponentNotFound verifies that ListVersions returns nil,nil
// when the component doesn't exist.
func TestListVersions_ComponentNotFound(t *testing.T) {
	dir := t.TempDir()
	setRegistryDir(t, dir)

	setupRegisteredPrompt(t, dir, "comp", "v1.0.0", "# Test", StatusActive)

	versions, err := ListVersions("other-comp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if versions != nil {
		t.Errorf("expected nil for missing component, got %v", versions)
	}
}

// TestListVersions_HasHashAndStatus verifies that entries have hash, status,
// model, and provider fields populated.
func TestListVersions_HasHashAndStatus(t *testing.T) {
	dir := t.TempDir()
	setRegistryDir(t, dir)

	content := "# Test"
	setupRegisteredPrompt(t, dir, "comp", "v1.0.0", content, StatusAttested)
	expectedHash := Hash(content)

	versions, err := ListVersions("comp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("expected 1 version, got %d", len(versions))
	}
	v := versions[0]
	if v.Hash != expectedHash {
		t.Errorf("hash = %q, want %q", v.Hash, expectedHash)
	}
	if v.Status != StatusAttested {
		t.Errorf("status = %q, want %q", v.Status, StatusAttested)
	}
	if v.Model != "deepseek-v4-pro" {
		t.Errorf("model = %q, want %q", v.Model, "deepseek-v4-pro")
	}
	if v.Provider != "deepseek" {
		t.Errorf("provider = %q, want %q", v.Provider, "deepseek")
	}
}

// =============================================================================
// Resolve tests
// =============================================================================

// TestResolve_Found verifies that Resolve returns a full Prompt struct for a
// registered component/version.
func TestResolve_Found(t *testing.T) {
	dir := t.TempDir()
	setRegistryDir(t, dir)

	content := "# Test Prompt\n\nThis is test content."
	setupRegisteredPrompt(t, dir, "comp", "v1.0.0", content, StatusActive)

	prompt, err := Resolve("comp", "v1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompt.Component != "comp" {
		t.Errorf("component = %q, want %q", prompt.Component, "comp")
	}
	if prompt.Version != "v1.0.0" {
		t.Errorf("version = %q, want %q", prompt.Version, "v1.0.0")
	}
	if prompt.Content != content {
		t.Errorf("content = %q, want %q", prompt.Content, content)
	}
	if prompt.Hash == "" {
		t.Error("hash should not be empty")
	}
	if prompt.Model != "deepseek-v4-pro" {
		t.Errorf("model = %q, want %q", prompt.Model, "deepseek-v4-pro")
	}
}

// TestResolve_NotFound verifies that Resolve returns an error for an unknown
// version.
func TestResolve_NotFound(t *testing.T) {
	dir := t.TempDir()
	setRegistryDir(t, dir)

	setupRegisteredPrompt(t, dir, "comp", "v1.0.0", "# Test", StatusActive)

	_, err := Resolve("comp", "v9.9.9")
	if err == nil {
		t.Fatal("expected error for unknown version")
	}
	if !strings.Contains(err.Error(), "PROMPT_NOT_FOUND") {
		t.Errorf("error should mention PROMPT_NOT_FOUND, got: %v", err)
	}
}

// TestResolve_ComponentNotFound verifies that Resolve returns an error for an
// unknown component.
func TestResolve_ComponentNotFound(t *testing.T) {
	dir := t.TempDir()
	setRegistryDir(t, dir)

	_, err := Resolve("nonexistent", "v1.0.0")
	if err == nil {
		t.Fatal("expected error for unknown component")
	}
}

// =============================================================================
// computeLineDiff tests
// =============================================================================

// TestComputeLineDiff_Identical verifies that identical content produces no
// diff lines.
func TestComputeLineDiff_Identical(t *testing.T) {
	content := "line1\nline2\nline3"
	diff := computeLineDiff(content, content, "a", "b")

	lines := strings.Split(strings.TrimSpace(diff), "\n")
	// Should only have --- and +++ headers, no actual diff lines
	if len(lines) > 2 {
		t.Errorf("expected only headers, got extra lines: %q", diff)
	}
}

// TestComputeLineDiff_Different verifies that different content produces diff
// lines with + and - prefixes.
func TestComputeLineDiff_Different(t *testing.T) {
	a := "line1\nline2\nline3"
	b := "line1\nline2-modified\nline3"

	diff := computeLineDiff(a, b, "v1", "v2")
	if !strings.Contains(diff, "v1") || !strings.Contains(diff, "v2") {
		t.Errorf("diff should contain version labels: %q", diff)
	}
	if !strings.Contains(diff, "-line2") {
		t.Errorf("diff should contain removed line: %q", diff)
	}
	if !strings.Contains(diff, "+line2-modified") {
		t.Errorf("diff should contain added line: %q", diff)
	}
}

// TestComputeLineDiff_AddedLines verifies that added lines are shown with +
// prefix when the second content has more lines.
func TestComputeLineDiff_AddedLines(t *testing.T) {
	a := "line1"
	b := "line1\nline2\nline3"

	diff := computeLineDiff(a, b, "v1", "v2")
	if strings.Count(diff, "\n+") < 2 {
		t.Errorf("expected 2 added lines: %q", diff)
	}
	// Should not have any removed lines
	if strings.Contains(diff, "\n-") {
		t.Errorf("should not have removed lines: %q", diff)
	}
}

// TestComputeLineDiff_RemovedLines verifies that removed lines are shown with -
// prefix when the first content has more lines.
func TestComputeLineDiff_RemovedLines(t *testing.T) {
	a := "line1\nline2\nline3"
	b := "line1"

	diff := computeLineDiff(a, b, "v1", "v2")
	if strings.Count(diff, "\n-") < 2 {
		t.Errorf("expected 2 removed lines: %q", diff)
	}
}

// =============================================================================
// computeMetaDiff tests
// =============================================================================

// TestComputeMetaDiff_SameMetadata verifies that identical metadata produces
// "(no metadata changes)".
func TestComputeMetaDiff_SameMetadata(t *testing.T) {
	a := &Metadata{
		Model:    "deepseek-v4-pro",
		Provider: "deepseek",
		Status:   StatusActive,
	}
	b := &Metadata{
		Model:    "deepseek-v4-pro",
		Provider: "deepseek",
		Status:   StatusActive,
	}

	diff := computeMetaDiff(a, b)
	if diff != "(no metadata changes)" {
		t.Errorf("expected '(no metadata changes)', got %q", diff)
	}
}

// TestComputeMetaDiff_ModelChanged verifies that a model change is detected.
func TestComputeMetaDiff_ModelChanged(t *testing.T) {
	a := &Metadata{Model: "deepseek-v4-pro", Provider: "deepseek"}
	b := &Metadata{Model: "glm-5.2", Provider: "deepseek"}

	diff := computeMetaDiff(a, b)
	if !strings.Contains(diff, "model") {
		t.Errorf("expected model change, got: %q", diff)
	}
	if !strings.Contains(diff, "deepseek-v4-pro") && !strings.Contains(diff, "glm-5.2") {
		t.Errorf("expected model names in diff: %q", diff)
	}
}

// TestComputeMetaDiff_ProviderChanged verifies that a provider change is
// detected.
func TestComputeMetaDiff_ProviderChanged(t *testing.T) {
	a := &Metadata{Model: "m1", Provider: "p1"}
	b := &Metadata{Model: "m1", Provider: "p2"}

	diff := computeMetaDiff(a, b)
	if !strings.Contains(diff, "provider") {
		t.Errorf("expected provider change, got: %q", diff)
	}
}

// TestComputeMetaDiff_StatusChanged verifies that a status change is detected.
func TestComputeMetaDiff_StatusChanged(t *testing.T) {
	a := &Metadata{Model: "m1", Provider: "p1", Status: StatusDraft}
	b := &Metadata{Model: "m1", Provider: "p1", Status: StatusActive}

	diff := computeMetaDiff(a, b)
	if !strings.Contains(diff, "status") {
		t.Errorf("expected status change, got: %q", diff)
	}
}

// TestComputeMetaDiff_MultipleChanges verifies that multiple metadata changes
// are reported together.
func TestComputeMetaDiff_MultipleChanges(t *testing.T) {
	a := &Metadata{
		Model:    "m1",
		Provider: "p1",
		Status:   StatusDraft,
		Author:   "alice",
		SpecRef:  "specs/a.md",
	}
	b := &Metadata{
		Model:    "m2",
		Provider: "p2",
		Status:   StatusActive,
		Author:   "bob",
		SpecRef:  "specs/b.md",
	}

	diff := computeMetaDiff(a, b)
	// All 5 fields changed
	for _, field := range []string{"model", "provider", "status", "author", "spec_ref"} {
		if !strings.Contains(diff, field) {
			t.Errorf("expected %q change in diff: %q", field, diff)
		}
	}
	lines := strings.Split(diff, "\n")
	if len(lines) < 5 {
		t.Errorf("expected at least 5 change lines, got %d: %q", len(lines), diff)
	}
}

// TestComputeMetaDiff_AllFieldsChanged verifies that all 8 metadata fields can
// be compared (model, provider, status, author, spec_ref, work_item,
// previous_version, changes).
func TestComputeMetaDiff_AllFieldsChanged(t *testing.T) {
	a := &Metadata{
		Model:           "m1",
		Provider:        "p1",
		Status:          StatusDraft,
		Author:          "a1",
		SpecRef:         "s1",
		WorkItem:        "w1",
		PreviousVersion: "pv1",
		Changes:         "c1",
	}
	b := &Metadata{
		Model:           "m2",
		Provider:        "p2",
		Status:          StatusActive,
		Author:          "a2",
		SpecRef:         "s2",
		WorkItem:        "w2",
		PreviousVersion: "pv2",
		Changes:         "c2",
	}

	diff := computeMetaDiff(a, b)
	expectedFields := []string{"model", "provider", "status", "author", "spec_ref", "work_item", "previous_version", "changes"}
	for _, field := range expectedFields {
		if !strings.Contains(diff, field) {
			t.Errorf("expected %q change in diff: %q", field, diff)
		}
	}
}

// =============================================================================
// Diff edge cases
// =============================================================================

// TestDiff_SameContentDifferentMetadata verifies that Diff separates content
// and metadata differences correctly.
func TestDiff_SameContentDifferentMetadata(t *testing.T) {
	dir := t.TempDir()

	content := "# Same Content\n\nIdentical prompt text."
	setupMultiVersionPrompt(t, dir, "comp", "v1.0.0", content, "v2.0.0", content, StatusDraft)

	// Change v2 status to Active for metadata diff
	if err := UpdateStatus("comp", "v2.0.0", StatusActive); err != nil {
		t.Fatalf("UpdateStatus v2: %v", err)
	}

	diff, err := Diff("comp", "v1.0.0", "v2.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(strings.TrimSpace(diff.ContentDiff)) > 0 && !strings.Contains(diff.ContentDiff, "-") && !strings.Contains(diff.ContentDiff, "+") {
		// Content should be essentially identical (headers only, no diff lines)
	}
	if !strings.Contains(diff.MetadataDiff, "status") {
		t.Errorf("expected status change in metadata: %q", diff.MetadataDiff)
	}
}

// TestDiff_FirstVersionNotFound verifies that Diff returns an error when the
// first version doesn't exist (but component does).
func TestDiff_FirstVersionNotFound(t *testing.T) {
	dir := t.TempDir()
	setRegistryDir(t, dir)

	setupRegisteredPrompt(t, dir, "comp", "v2.0.0", "# Test", StatusActive)

	_, err := Diff("comp", "v1.0.0", "v2.0.0")
	if err == nil {
		t.Fatal("expected error when first version doesn't exist")
	}
}

// =============================================================================
// Resolve edge case — missing prompt file
// =============================================================================

// TestResolve_MissingPromptFile verifies that Resolve returns an error when the
// prompt file is missing from disk (registry entry exists but file deleted).
func TestResolve_MissingPromptFile(t *testing.T) {
	dir := t.TempDir()
	setRegistryDir(t, dir)

	content := "# Test"
	setupRegisteredPrompt(t, dir, "comp", "v1.0.0", content, StatusActive)

	// Delete the prompt file
	pv, err := LookupByComponent("comp", "v1.0.0")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if err := os.Remove(pv.PromptPath); err != nil {
		t.Fatalf("remove prompt file: %v", err)
	}

	_, err = Resolve("comp", "v1.0.0")
	if err == nil {
		t.Fatal("expected error when prompt file is missing")
	}
	if !strings.Contains(err.Error(), "cannot read prompt") {
		t.Errorf("error should mention cannot read prompt: %v", err)
	}
}
