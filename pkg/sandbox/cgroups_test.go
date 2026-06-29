package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// =============================================================================
// NewCgroup tests
// =============================================================================

func TestNewCgroup_CreatesWithCorrectPath(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:  "test-session",
		CgroupRoot: "/sys/fs/cgroup",
	}
	cg := NewCgroup(cfg)
	if cg == nil {
		t.Fatal("expected non-nil CgroupV2")
	}
	expectedPath := filepath.Join(cfg.CgroupRoot, "helix", cfg.SessionID)
	if cg.Path != expectedPath {
		t.Errorf("Path = %q, want %q", cg.Path, expectedPath)
	}
	if cg.Enabled {
		t.Error("expected Enabled=false before Setup")
	}
}

func TestNewCgroup_PreservesConfig(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:   "cfg-test",
		CgroupRoot:  "/custom/cgroup",
		MemoryLimit: 512,
	}
	cg := NewCgroup(cfg)
	if cg.Config.SessionID != cfg.SessionID {
		t.Errorf("Config.SessionID = %q, want %q", cg.Config.SessionID, cfg.SessionID)
	}
	if cg.Config.MemoryLimit != cfg.MemoryLimit {
		t.Errorf("Config.MemoryLimit = %d, want %d", cg.Config.MemoryLimit, cfg.MemoryLimit)
	}
}

// =============================================================================
// CgroupPIDPath tests
// =============================================================================

func TestCgroupPIDPath_ReturnsCorrectPath(t *testing.T) {
	cg := &CgroupV2{Path: "/sys/fs/cgroup/helix/session-1"}
	got := cg.CgroupPIDPath()
	expected := "/sys/fs/cgroup/helix/session-1/cgroup.procs"
	if got != expected {
		t.Errorf("CgroupPIDPath() = %q, want %q", got, expected)
	}
}

func TestCgroupPIDPath_EmptyPath(t *testing.T) {
	cg := &CgroupV2{Path: ""}
	got := cg.CgroupPIDPath()
	if got != "cgroup.procs" {
		t.Errorf("CgroupPIDPath() = %q, want %q", got, "cgroup.procs")
	}
}

func TestCgroupPIDPath_WithTrailingSlash(t *testing.T) {
	cg := &CgroupV2{Path: "/sys/fs/cgroup/helix/session-2/"}
	got := cg.CgroupPIDPath()
	expected := "/sys/fs/cgroup/helix/session-2/cgroup.procs"
	if got != expected {
		t.Errorf("CgroupPIDPath() = %q, want %q", got, expected)
	}
}

// =============================================================================
// mkdirIfNotExist tests
// =============================================================================

func TestMkdirIfNotExist_CreatesNewDirectory(t *testing.T) {
	root := t.TempDir()
	newDir := filepath.Join(root, "new-dir")
	err := mkdirIfNotExist(newDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info, err := os.Stat(newDir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected a directory")
	}
}

func TestMkdirIfNotExist_AlreadyExists(t *testing.T) {
	root := t.TempDir()
	existingDir := filepath.Join(root, "exists")
	if err := os.MkdirAll(existingDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Should succeed without error when dir already exists.
	err := mkdirIfNotExist(existingDir)
	if err != nil {
		t.Fatalf("unexpected error on existing dir: %v", err)
	}
}

func TestMkdirIfNotExist_UnwritablePath(t *testing.T) {
	// Use a path under /root which is not writable by non-root users.
	err := mkdirIfNotExist("/root/should-fail")
	if err == nil {
		t.Skip("running as root — can't test unwritable path")
	}
}

func TestMkdirIfNotExist_NestedPath(t *testing.T) {
	root := t.TempDir()
	nestedDir := filepath.Join(root, "a", "b", "c")
	err := mkdirIfNotExist(nestedDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info, err := os.Stat(nestedDir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected a directory")
	}
}

// =============================================================================
// writeFile tests
// =============================================================================

func TestWriteFile_CreatesFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "test-file")
	err := writeFile(path, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("content = %q, want %q", string(data), "hello")
	}
}

func TestWriteFile_OverwritesExisting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "overwrite-file")
	// Write initial content directly.
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Overwrite via writeFile.
	err := writeFile(path, "replaced")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "replaced" {
		t.Errorf("content = %q, want %q", string(data), "replaced")
	}
}

func TestWriteFile_EmptyData(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "empty-file")
	err := writeFile(path, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty file, got %d bytes", len(data))
	}
}

func TestWriteFile_UnwritablePath(t *testing.T) {
	err := writeFile("/root/should-fail", "data")
	if err == nil {
		t.Skip("running as root — can't test unwritable path")
	}
}

// =============================================================================
// isWritable tests
// =============================================================================

func TestIsWritable_WritableDir(t *testing.T) {
	dir := t.TempDir()
	if !isWritable(dir) {
		t.Error("isWritable returned false for writable temp dir")
	}
}

func TestIsWritable_NonExistentDir(t *testing.T) {
	if isWritable("/nonexistent/path/for/testing") {
		t.Error("isWritable returned true for non-existent dir")
	}
}

func TestIsWritable_ReadOnlyDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer func() { _ = os.Chmod(dir, 0o755) }() // restore for cleanup
	if isWritable(dir) {
		t.Error("isWritable returned true for read-only dir")
	}
}

func TestIsWritable_CurrentDir(t *testing.T) {
	// The current working directory is typically writable.
	if !isWritable(".") {
		// Might not be writable in restricted containers; not a hard failure.
		t.Log("current dir is not writable per isWritable; may be restricted env")
	}
}

// =============================================================================
// Cleanup tests
// =============================================================================

func TestCleanup_EmptyPathReturnsNil(t *testing.T) {
	cg := &CgroupV2{Path: "", Enabled: true}
	err := cg.Cleanup()
	if err != nil {
		t.Errorf("expected nil for empty path, got: %v", err)
	}
}

func TestCleanup_NotEnabledReturnsNil(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "should-not-exist")
	cg := &CgroupV2{Path: dir, Enabled: false}
	err := cg.Cleanup()
	if err != nil {
		t.Errorf("expected nil when not enabled, got: %v", err)
	}
}

func TestCleanup_RemovesDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "cg-test")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	cg := &CgroupV2{Path: dir, Enabled: true}
	err := cg.Cleanup()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected directory to be removed, stat: %v", err)
	}
}

func TestCleanup_RemovesNonEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "cg-nonempty")
	subdir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Add a file inside.
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("data"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	cg := &CgroupV2{Path: dir, Enabled: true}
	err := cg.Cleanup()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected directory to be removed, stat: %v", err)
	}
}

// =============================================================================
// Setup tests
// =============================================================================

func TestSetup_CgroupRootDoesNotExist(t *testing.T) {
	cfg := SandboxConfig{
		SessionID:  "test-session",
		CgroupRoot: "/nonexistent/cgroup/root",
	}
	cg := NewCgroup(cfg)
	err := cg.Setup()
	if err == nil {
		t.Fatal("expected error for non-existent cgroup root")
	}
	if !errors.Is(err, ErrSetupFailed) {
		t.Errorf("error should wrap ErrSetupFailed, got: %v", err)
	}
	if cg.Enabled {
		t.Error("expected Enabled=false after failed Setup")
	}
}

func TestSetup_RootNotWritable_ReturnsNil(t *testing.T) {
	root := t.TempDir()
	// Make root unwritable.
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer func() { _ = os.Chmod(root, 0o755) }()

	cfg := SandboxConfig{
		SessionID:  "test-session",
		CgroupRoot: root,
	}
	cg := NewCgroup(cfg)
	err := cg.Setup()
	if err != nil {
		t.Fatalf("expected nil (non-fatal when root not writable), got: %v", err)
	}
	if cg.Enabled {
		t.Error("expected Enabled=false when root not writable")
	}
}

func TestSetup_FullSuccess(t *testing.T) {
	root := t.TempDir()
	cfg := SandboxConfig{
		SessionID:   "test-session",
		CgroupRoot:  root,
		MemoryLimit: 256, // 256 MB
	}
	cg := NewCgroup(cfg)
	err := cg.Setup()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cg.Enabled {
		t.Error("expected Enabled=true after successful Setup")
	}

	// Verify directory structure.
	helixDir := filepath.Join(root, "helix")
	if _, err := os.Stat(helixDir); err != nil {
		t.Errorf("helix dir should exist: %v", err)
	}
	cgroupDir := filepath.Join(root, "helix", "test-session")
	if _, err := os.Stat(cgroupDir); err != nil {
		t.Errorf("cgroup dir should exist: %v", err)
	}

	// Verify cpu.max was written.
	cpuMaxContent, err := os.ReadFile(filepath.Join(cgroupDir, "cpu.max"))
	if err != nil {
		t.Errorf("cpu.max should exist: %v", err)
	}
	if string(cpuMaxContent) != "max 100000" {
		t.Errorf("cpu.max = %q, want %q", string(cpuMaxContent), "max 100000")
	}

	// Verify memory.max was written with correct value.
	memMaxContent, err := os.ReadFile(filepath.Join(cgroupDir, "memory.max"))
	if err != nil {
		t.Errorf("memory.max should exist: %v", err)
	}
	expectedMem := "268435456" // 256 * 1024 * 1024
	if strings.TrimSpace(string(memMaxContent)) != expectedMem {
		t.Errorf("memory.max = %q, want %q", string(memMaxContent), expectedMem)
	}
}

func TestSetup_NoMemoryLimit_DoesNotWriteMemoryMax(t *testing.T) {
	root := t.TempDir()
	cfg := SandboxConfig{
		SessionID:   "no-mem",
		CgroupRoot:  root,
		MemoryLimit: 0, // No limit.
	}
	cg := NewCgroup(cfg)
	err := cg.Setup()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cgroupDir := filepath.Join(root, "helix", "no-mem")
	// memory.max should not exist when MemoryLimit is 0.
	if _, err := os.Stat(filepath.Join(cgroupDir, "memory.max")); !errors.Is(err, os.ErrNotExist) {
		t.Error("memory.max should not exist when MemoryLimit is 0")
	}
	// cpu.max should still be written.
	if _, err := os.Stat(filepath.Join(cgroupDir, "cpu.max")); err != nil {
		t.Errorf("cpu.max should exist: %v", err)
	}
}
