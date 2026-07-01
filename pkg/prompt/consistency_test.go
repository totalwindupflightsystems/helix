package prompt

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func parseTestTime() time.Time {
	return time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
}

func setupConsistencyTestDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	old := RegistryDir
	RegistryDir = dir
	t.Cleanup(func() { RegistryDir = old })
}

func writeTestPrompt(t *testing.T, component, version, content string) {
	t.Helper()
	dir := filepath.Join(RegistryDir, component, version)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompt.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func writeTestMetadata(t *testing.T, component, version, hash string, status LifecycleStatus) {
	t.Helper()
	meta := &Metadata{
		Version:   version,
		Component: component,
		Hash:      hash,
		Status:    status,
		Model:     "test-model",
		Provider:  "test-provider",
	}
	if err := writeMetadata(filepath.Join(RegistryDir, component, version, "metadata.yaml"), meta); err != nil {
		t.Fatal(err)
	}
}

// --- CheckIndex tests ---

func TestCheckIndex_Empty(t *testing.T) {
	setupConsistencyTestDir(t)
	report, err := CheckIndex(false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Checked != 0 {
		t.Errorf("expected 0 checked, got %d", report.Checked)
	}
	if report.HasIssues() {
		t.Error("empty index should have no issues")
	}
}

func TestCheckIndex_AllOK(t *testing.T) {
	setupConsistencyTestDir(t)

	content := "# Test prompt\nHello world."
	hash := Hash(content)

	writeTestPrompt(t, "auth", "v1", content)
	writeTestMetadata(t, "auth", "v1", hash, StatusActive)

	// Build index
	idx := Index{
		"auth": {"v1": &PromptEntry{Hash: hash, Status: StatusActive, Model: "test-model"}},
	}
	if err := saveIndex(&idx); err != nil {
		t.Fatal(err)
	}

	report, err := CheckIndex(false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Checked != 1 {
		t.Errorf("expected 1 checked, got %d", report.Checked)
	}
	if report.OK != 1 {
		t.Errorf("expected 1 OK, got %d", report.OK)
	}
	if report.HasIssues() {
		t.Error("should have no issues")
	}
	if report.ShouldBlock() {
		t.Error("should not block")
	}
}

func TestCheckIndex_Stale(t *testing.T) {
	setupConsistencyTestDir(t)

	content := "# Test prompt\nHello world."
	correctHash := Hash(content)
	staleHash := "sha256:0000000000000000000000000000000000000000000000000000000000000000"

	writeTestPrompt(t, "auth", "v1", content)
	writeTestMetadata(t, "auth", "v1", correctHash, StatusActive)

	// Index has stale hash
	idx := Index{
		"auth": {"v1": &PromptEntry{Hash: staleHash, Status: StatusActive}},
	}
	if err := saveIndex(&idx); err != nil {
		t.Fatal(err)
	}

	report, err := CheckIndex(false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Stale != 1 {
		t.Errorf("expected 1 stale, got %d", report.Stale)
	}
	if report.OK != 0 {
		t.Errorf("expected 0 OK, got %d", report.OK)
	}
	if !report.HasIssues() {
		t.Error("should have issues")
	}
	if report.ShouldBlock() {
		t.Error("stale index should NOT block (non-blocking per spec)")
	}
}

func TestCheckIndex_TamperDetected(t *testing.T) {
	setupConsistencyTestDir(t)

	originalContent := "# Original\nLine one."
	storedHash := Hash(originalContent)

	writeTestPrompt(t, "auth", "v1", originalContent)
	writeTestMetadata(t, "auth", "v1", storedHash, StatusActive)

	// Simulate tampering: change prompt.md after attestation
	tamperedContent := "# Tampered\nDifferent content."
	if err := os.WriteFile(
		filepath.Join(RegistryDir, "auth", "v1", "prompt.md"),
		[]byte(tamperedContent), 0644,
	); err != nil {
		t.Fatal(err)
	}

	idx := Index{
		"auth": {"v1": &PromptEntry{Hash: storedHash, Status: StatusActive}},
	}
	if err := saveIndex(&idx); err != nil {
		t.Fatal(err)
	}

	report, err := CheckIndex(false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Tampered != 1 {
		t.Errorf("expected 1 tampered, got %d (report: %+v)", report.Tampered, report)
	}
	if !report.ShouldBlock() {
		t.Error("tamper should block")
	}
}

func TestCheckIndex_MissingMetadata(t *testing.T) {
	setupConsistencyTestDir(t)

	idx := Index{
		"auth": {"v1": &PromptEntry{Hash: "sha256:abc", Status: StatusActive}},
	}
	if err := saveIndex(&idx); err != nil {
		t.Fatal(err)
	}

	report, err := CheckIndex(false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Missing != 1 {
		t.Errorf("expected 1 missing, got %d", report.Missing)
	}
}

func TestCheckIndex_MissingPromptFile(t *testing.T) {
	setupConsistencyTestDir(t)

	// metadata.yaml exists but prompt.md doesn't
	writeTestMetadata(t, "auth", "v1", "sha256:abc", StatusActive)

	idx := Index{
		"auth": {"v1": &PromptEntry{Hash: "sha256:abc", Status: StatusActive}},
	}
	if err := saveIndex(&idx); err != nil {
		t.Fatal(err)
	}

	report, err := CheckIndex(false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Missing != 1 {
		t.Errorf("expected 1 missing, got %d", report.Missing)
	}
}

func TestCheckIndex_OrphanedPrompt(t *testing.T) {
	setupConsistencyTestDir(t)

	content := "# Orphan\nNot in index."
	hash := Hash(content)

	// Write prompt on disk but don't add to index
	writeTestPrompt(t, "orphan", "v1", content)
	writeTestMetadata(t, "orphan", "v1", hash, StatusDraft)

	// Index is empty
	idx := Index{}
	if err := saveIndex(&idx); err != nil {
		t.Fatal(err)
	}

	report, err := CheckIndex(false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Orphaned != 1 {
		t.Errorf("expected 1 orphaned, got %d", report.Orphaned)
	}
}

func TestCheckIndex_MultipleEntries(t *testing.T) {
	setupConsistencyTestDir(t)

	// Entry 1: OK
	content1 := "# Prompt 1"
	hash1 := Hash(content1)
	writeTestPrompt(t, "comp1", "v1", content1)
	writeTestMetadata(t, "comp1", "v1", hash1, StatusActive)

	// Entry 2: Stale
	content2 := "# Prompt 2"
	hash2 := Hash(content2)
	writeTestPrompt(t, "comp2", "v1", content2)
	writeTestMetadata(t, "comp2", "v1", hash2, StatusActive)

	// Entry 3: Tampered
	content3 := "# Prompt 3"
	hash3 := Hash(content3)
	writeTestPrompt(t, "comp3", "v1", content3)
	writeTestMetadata(t, "comp3", "v1", hash3, StatusActive)
	// Tamper: overwrite prompt
	if err := os.WriteFile(
		filepath.Join(RegistryDir, "comp3", "v1", "prompt.md"),
		[]byte("# Different"), 0644,
	); err != nil {
		t.Fatal(err)
	}

	idx := Index{
		"comp1": {"v1": &PromptEntry{Hash: hash1, Status: StatusActive}},
		"comp2": {"v1": &PromptEntry{Hash: "sha256:stale0000000000000000000000000000000000000000000000", Status: StatusActive}},
		"comp3": {"v1": &PromptEntry{Hash: hash3, Status: StatusActive}},
	}
	if err := saveIndex(&idx); err != nil {
		t.Fatal(err)
	}

	report, err := CheckIndex(false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Checked != 3 {
		t.Errorf("expected 3 checked, got %d", report.Checked)
	}
	if report.OK != 1 {
		t.Errorf("expected 1 OK, got %d", report.OK)
	}
	if report.Stale != 1 {
		t.Errorf("expected 1 stale, got %d", report.Stale)
	}
	if report.Tampered != 1 {
		t.Errorf("expected 1 tampered, got %d", report.Tampered)
	}
}

// --- Auto-rebuild tests ---

func TestCheckIndex_AutoRebuild(t *testing.T) {
	setupConsistencyTestDir(t)

	content := "# Rebuild me"
	correctHash := Hash(content)
	staleHash := "sha256:1111111111111111111111111111111111111111111111111111111111111111"

	writeTestPrompt(t, "auth", "v1", content)
	writeTestMetadata(t, "auth", "v1", correctHash, StatusActive)

	idx := Index{
		"auth": {"v1": &PromptEntry{Hash: staleHash, Status: StatusActive}},
	}
	if err := saveIndex(&idx); err != nil {
		t.Fatal(err)
	}

	report, err := CheckIndex(true)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Rebuilt {
		t.Error("expected index to be rebuilt")
	}

	// Verify index was actually fixed
	reloaded, err := loadIndex()
	if err != nil {
		t.Fatal(err)
	}
	entry := (*reloaded)["auth"]["v1"]
	if entry.Hash != correctHash {
		t.Errorf("after rebuild, index hash should be %s, got %s", correctHash, entry.Hash)
	}
}

func TestCheckIndex_NoRebuildWhenOK(t *testing.T) {
	setupConsistencyTestDir(t)

	content := "# Good prompt"
	hash := Hash(content)

	writeTestPrompt(t, "auth", "v1", content)
	writeTestMetadata(t, "auth", "v1", hash, StatusActive)

	idx := Index{
		"auth": {"v1": &PromptEntry{Hash: hash, Status: StatusActive}},
	}
	if err := saveIndex(&idx); err != nil {
		t.Fatal(err)
	}

	report, err := CheckIndex(true)
	if err != nil {
		t.Fatal(err)
	}
	if report.Rebuilt {
		t.Error("should not rebuild when everything is OK")
	}
}

func TestCheckIndex_NoRebuildWhenTamper(t *testing.T) {
	setupConsistencyTestDir(t)

	content := "# Original"
	hash := Hash(content)
	writeTestPrompt(t, "auth", "v1", content)
	writeTestMetadata(t, "auth", "v1", hash, StatusActive)

	// Tamper
	if err := os.WriteFile(
		filepath.Join(RegistryDir, "auth", "v1", "prompt.md"),
		[]byte("# Tampered"), 0644,
	); err != nil {
		t.Fatal(err)
	}

	idx := Index{
		"auth": {"v1": &PromptEntry{Hash: hash, Status: StatusActive}},
	}
	if err := saveIndex(&idx); err != nil {
		t.Fatal(err)
	}

	report, err := CheckIndex(true)
	if err != nil {
		t.Fatal(err)
	}
	if report.Rebuilt {
		t.Error("should NOT auto-rebuild on tamper (tamper is not a staleness issue)")
	}
}

// --- RebuildIndex tests ---

func TestRebuildIndex_Empty(t *testing.T) {
	setupConsistencyTestDir(t)
	if err := RebuildIndex(); err != nil {
		t.Fatal(err)
	}
	idx, err := loadIndex()
	if err != nil {
		t.Fatal(err)
	}
	if len(*idx) != 0 {
		t.Errorf("expected empty index, got %d components", len(*idx))
	}
}

func TestRebuildIndex_NoRegistryDir(t *testing.T) {
	dir := t.TempDir()
	old := RegistryDir
	RegistryDir = filepath.Join(dir, "nonexistent")
	t.Cleanup(func() { RegistryDir = old })

	if err := RebuildIndex(); err != nil {
		t.Fatalf("should handle missing dir: %v", err)
	}
}

func TestRebuildIndex_FullRebuild(t *testing.T) {
	setupConsistencyTestDir(t)

	// Create prompts on disk
	content1 := "# Prompt 1"
	hash1 := Hash(content1)
	writeTestPrompt(t, "comp1", "v1", content1)
	writeTestMetadata(t, "comp1", "v1", hash1, StatusActive)

	content2 := "# Prompt 2"
	hash2 := Hash(content2)
	writeTestPrompt(t, "comp2", "v1", content2)
	writeTestMetadata(t, "comp2", "v1", hash2, StatusDeprecated)

	content3 := "# Prompt 3"
	hash3 := Hash(content3)
	writeTestPrompt(t, "comp1", "v2", content3)
	writeTestMetadata(t, "comp1", "v2", hash3, StatusActive)

	// Rebuild
	if err := RebuildIndex(); err != nil {
		t.Fatal(err)
	}

	idx, err := loadIndex()
	if err != nil {
		t.Fatal(err)
	}

	if len(*idx) != 2 {
		t.Fatalf("expected 2 components, got %d", len(*idx))
	}
	if len((*idx)["comp1"]) != 2 {
		t.Errorf("expected 2 versions for comp1, got %d", len((*idx)["comp1"]))
	}
	if (*idx)["comp1"]["v1"].Hash != hash1 {
		t.Errorf("comp1/v1 hash mismatch")
	}
	if (*idx)["comp1"]["v2"].Hash != hash3 {
		t.Errorf("comp1/v2 hash mismatch")
	}
	if (*idx)["comp2"]["v1"].Hash != hash2 {
		t.Errorf("comp2/v1 hash mismatch")
	}
	if (*idx)["comp2"]["v1"].Status != StatusDeprecated {
		t.Errorf("comp2/v1 status mismatch")
	}
}

func TestRebuildIndex_SkipsInvalidEntries(t *testing.T) {
	setupConsistencyTestDir(t)

	// Valid prompt
	content := "# Valid"
	hash := Hash(content)
	writeTestPrompt(t, "valid", "v1", content)
	writeTestMetadata(t, "valid", "v1", hash, StatusActive)

	// Invalid: directory exists but no metadata.yaml
	dir := filepath.Join(RegistryDir, "invalid", "v1")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	// Invalid: random non-prompt directory
	dir2 := filepath.Join(RegistryDir, "randomdir")
	if err := os.MkdirAll(dir2, 0755); err != nil {
		t.Fatal(err)
	}

	if err := RebuildIndex(); err != nil {
		t.Fatal(err)
	}

	idx, err := loadIndex()
	if err != nil {
		t.Fatal(err)
	}
	if len(*idx) != 1 {
		t.Errorf("expected 1 valid component, got %d", len(*idx))
	}
	if (*idx)["valid"] == nil {
		t.Error("valid component should be in rebuilt index")
	}
}

func TestRebuildIndex_SkipsUnderscoreDirs(t *testing.T) {
	setupConsistencyTestDir(t)

	// Valid prompt
	content := "# Valid"
	hash := Hash(content)
	writeTestPrompt(t, "valid", "v1", content)
	writeTestMetadata(t, "valid", "v1", hash, StatusActive)

	// _internal directory should be skipped
	internalDir := filepath.Join(RegistryDir, "_internal", "v1")
	if err := os.MkdirAll(internalDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(internalDir, "metadata.yaml"),
		[]byte("version: v1\ncomponent: internal\nhash: test\n"), 0644,
	); err != nil {
		t.Fatal(err)
	}

	if err := RebuildIndex(); err != nil {
		t.Fatal(err)
	}

	idx, err := loadIndex()
	if err != nil {
		t.Fatal(err)
	}
	if (*idx)["_internal"] != nil {
		t.Error("_internal directory should be skipped")
	}
}

// --- Report formatting tests ---

func TestConsistencyReport_FormatReport_OK(t *testing.T) {
	report := &ConsistencyReport{
		Checked:   5,
		OK:        5,
		Checks:    []ConsistencyCheck{},
		CheckedAt: parseTestTime(),
	}
	out := report.FormatReport()
	if out == "" {
		t.Error("should produce non-empty output")
	}
}

func TestConsistencyReport_FormatReport_Tamper(t *testing.T) {
	report := &ConsistencyReport{
		Checked:  3,
		OK:       2,
		Tampered: 1,
		Checks: []ConsistencyCheck{
			{Component: "auth", Version: "v1", Status: ConsistencyTamper, Detail: "hash mismatch"},
		},
		CheckedAt: parseTestTime(),
	}
	out := report.FormatReport()
	if out == "" {
		t.Error("should produce output")
	}
}

func TestConsistencyReport_FormatReport_Stale(t *testing.T) {
	report := &ConsistencyReport{
		Checked: 3,
		OK:      2,
		Stale:   1,
		Rebuilt: true,
		Checks: []ConsistencyCheck{
			{Component: "auth", Version: "v1", Status: ConsistencyIndexStale, Detail: "rebuilt"},
		},
		CheckedAt: parseTestTime(),
	}
	out := report.FormatReport()
	if out == "" {
		t.Error("should produce output")
	}
}

func TestConsistencyReport_FormatReport_Orphaned(t *testing.T) {
	report := &ConsistencyReport{
		Checked:  1,
		OK:       1,
		Orphaned: 2,
		Checks: []ConsistencyCheck{
			{Component: "orphan", Version: "v1", Status: ConsistencyOrphaned, Detail: "on disk only"},
		},
		CheckedAt: parseTestTime(),
	}
	out := report.FormatReport()
	if out == "" {
		t.Error("should produce output")
	}
}

func TestConsistencyReport_ShouldBlock(t *testing.T) {
	tests := []struct {
		name     string
		report   *ConsistencyReport
		expected bool
	}{
		{
			name:     "all OK",
			report:   &ConsistencyReport{OK: 5, Checked: 5},
			expected: false,
		},
		{
			name:     "stale only",
			report:   &ConsistencyReport{Stale: 2, OK: 3, Checked: 5},
			expected: false,
		},
		{
			name:     "orphaned only",
			report:   &ConsistencyReport{Orphaned: 1, OK: 4, Checked: 5},
			expected: false,
		},
		{
			name:     "missing only",
			report:   &ConsistencyReport{Missing: 1, OK: 4, Checked: 5},
			expected: false,
		},
		{
			name:     "tampered",
			report:   &ConsistencyReport{Tampered: 1, OK: 4, Checked: 5},
			expected: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.report.ShouldBlock() != tc.expected {
				t.Errorf("ShouldBlock() = %v, want %v", tc.report.ShouldBlock(), tc.expected)
			}
		})
	}
}

func TestConsistencyReport_HasIssues(t *testing.T) {
	tests := []struct {
		name     string
		report   *ConsistencyReport
		expected bool
	}{
		{"all OK", &ConsistencyReport{OK: 5}, false},
		{"stale", &ConsistencyReport{Stale: 1, OK: 4}, true},
		{"tampered", &ConsistencyReport{Tampered: 1, OK: 4}, true},
		{"missing", &ConsistencyReport{Missing: 1, OK: 4}, true},
		{"orphaned", &ConsistencyReport{Orphaned: 1, OK: 4}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.report.HasIssues() != tc.expected {
				t.Errorf("HasIssues() = %v, want %v", tc.report.HasIssues(), tc.expected)
			}
		})
	}
}

// --- Round-trip integration tests ---

func TestCheckIndex_StaleThenRebuildThenOK(t *testing.T) {
	setupConsistencyTestDir(t)

	content := "# Prompt"
	correctHash := Hash(content)
	staleHash := "sha256:2222222222222222222222222222222222222222222222222222222222222222"

	writeTestPrompt(t, "auth", "v1", content)
	writeTestMetadata(t, "auth", "v1", correctHash, StatusActive)

	// Stale index
	idx := Index{"auth": {"v1": &PromptEntry{Hash: staleHash, Status: StatusActive}}}
	if err := saveIndex(&idx); err != nil {
		t.Fatal(err)
	}

	// Check without rebuild → stale
	report1, err := CheckIndex(false)
	if err != nil {
		t.Fatal(err)
	}
	if report1.Stale != 1 {
		t.Fatalf("expected 1 stale, got %d", report1.Stale)
	}

	// Check with rebuild → fixes the stale entry
	report2, err := CheckIndex(true)
	if err != nil {
		t.Fatal(err)
	}
	if !report2.Rebuilt {
		t.Error("expected rebuild")
	}

	// Subsequent check → all OK
	report3, err := CheckIndex(false)
	if err != nil {
		t.Fatal(err)
	}
	if report3.OK != 1 {
		t.Errorf("expected 1 OK after rebuild, got %d", report3.OK)
	}
	if report3.HasIssues() {
		t.Error("should be clean after rebuild")
	}
}

func TestCheckIndex_RebuildPreservesValidEntries(t *testing.T) {
	setupConsistencyTestDir(t)

	content1 := "# Valid"
	hash1 := Hash(content1)
	writeTestPrompt(t, "valid", "v1", content1)
	writeTestMetadata(t, "valid", "v1", hash1, StatusActive)

	content2 := "# Stale"
	hash2 := Hash(content2)
	writeTestPrompt(t, "stale", "v1", content2)
	writeTestMetadata(t, "stale", "v1", hash2, StatusActive)

	// One valid, one stale
	idx := Index{
		"valid": {"v1": &PromptEntry{Hash: hash1, Status: StatusActive}},
		"stale": {"v1": &PromptEntry{Hash: "sha256:3333333333333333333333333333333333333333333333333333333333333333", Status: StatusActive}},
	}
	if err := saveIndex(&idx); err != nil {
		t.Fatal(err)
	}

	report, err := CheckIndex(true)
	if err != nil {
		t.Fatal(err)
	}
	if report.Rebuilt != true {
		t.Error("expected rebuild")
	}

	// Both should be correct now
	idx2, err := loadIndex()
	if err != nil {
		t.Fatal(err)
	}
	if (*idx2)["valid"]["v1"].Hash != hash1 {
		t.Error("valid entry hash should be preserved")
	}
	if (*idx2)["stale"]["v1"].Hash != hash2 {
		t.Error("stale entry hash should be corrected")
	}
}
