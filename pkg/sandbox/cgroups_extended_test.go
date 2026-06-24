package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// =============================================================================
// Setup error paths for mkdirIfNotExist and os.MkdirAll failures
// =============================================================================

// TestSetup_HelixParentDirBlockedByFile verifies that Setup returns an error
// when a file blocks creation of the helix parent cgroup directory.
func TestSetup_HelixParentDirBlockedByFile(t *testing.T) {
	root := t.TempDir()
	// Place a FILE at the helix parent path so mkdirIfNotExist fails.
	helixDir := filepath.Join(root, "helix")
	if err := os.WriteFile(helixDir, []byte("block"), 0o600); err != nil {
		t.Fatalf("create blocking file: %v", err)
	}

	cfg := SandboxConfig{
		SessionID:   "test-session",
		CgroupRoot:  root,
		MemoryLimit: 128,
	}
	cg := NewCgroup(cfg)
	err := cg.Setup()
	if err == nil {
		t.Fatal("expected error when a file blocks helix parent dir creation")
	}
	if !errors.Is(err, ErrSetupFailed) {
		t.Errorf("error should wrap ErrSetupFailed, got: %v", err)
	}
	if cg.Enabled {
		t.Error("expected Enabled=false after failed Setup")
	}
}

// TestSetup_SessionDirBlockedByFile verifies that Setup returns an error when
// a file blocks creation of the per-session cgroup directory.
func TestSetup_SessionDirBlockedByFile(t *testing.T) {
	root := t.TempDir()
	// Create the helix parent dir first.
	helixDir := filepath.Join(root, "helix")
	if err := os.MkdirAll(helixDir, 0o755); err != nil {
		t.Fatalf("create helix dir: %v", err)
	}
	// Place a FILE at the session dir path.
	sessionDir := filepath.Join(helixDir, "test-session")
	if err := os.WriteFile(sessionDir, []byte("block"), 0o600); err != nil {
		t.Fatalf("create blocking file: %v", err)
	}

	cfg := SandboxConfig{
		SessionID:   "test-session",
		CgroupRoot:  root,
		MemoryLimit: 128,
	}
	cg := NewCgroup(cfg)
	err := cg.Setup()
	if err == nil {
		t.Fatal("expected error when a file blocks session dir creation")
	}
	if !errors.Is(err, ErrSetupFailed) {
		t.Errorf("error should wrap ErrSetupFailed, got: %v", err)
	}
	if cg.Enabled {
		t.Error("expected Enabled=false after failed Setup")
	}
}
