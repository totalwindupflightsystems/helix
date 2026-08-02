package mergegate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain builds the real helix binary once (into TMPDIR — the Makefile
// redirects it to /home/kara/.cache/go-tmp because /tmp is a constrained
// tmpfs) so the pre-receive integration test exercises the actual CLI exit
// codes instead of in-process logic. The build is skipped when git is
// unavailable (the test itself skips in that case too).
var (
	testHelixBin      string
	testHelixBuildErr error
)

func TestMain(m *testing.M) {
	if _, err := exec.LookPath("git"); err != nil {
		testHelixBuildErr = fmt.Errorf("git not available: %w", err)
		os.Exit(m.Run())
	}

	root, err := findRepoRoot()
	if err != nil {
		testHelixBuildErr = err
		os.Exit(m.Run())
	}

	binDir, err := os.MkdirTemp("", "helix-pre-receive-*")
	if err != nil {
		testHelixBuildErr = fmt.Errorf("MkdirTemp: %w", err)
		os.Exit(m.Run())
	}
	testHelixBin = filepath.Join(binDir, "helix")

	cmd := exec.Command("go", "build", "-o", testHelixBin, "./cmd/helix")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		testHelixBuildErr = fmt.Errorf("go build ./cmd/helix: %w\n%s", err, out)
	}

	code := m.Run()
	os.RemoveAll(binDir)
	os.Exit(code)
}

// findRepoRoot walks up from the package dir to the module root (go.mod).
func findRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", cwd)
		}
		dir = parent
	}
}

// TestPreReceiveHookBlocksRejectedPush proves the gate BLOCKS at the git
// level: a real pre-receive hook installed in a bare repo rejects an
// unattested push to a protected branch (exit != 0, "REJECTED" on stderr)
// while the same push to a non-protected branch succeeds (exit 0).
func TestPreReceiveHookBlocksRejectedPush(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	if testHelixBuildErr != nil {
		t.Skipf("helix binary unavailable: %v", testHelixBuildErr)
	}

	root, err := findRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join(root, "scripts", "helix-pre-receive.sh"))
	if err != nil {
		t.Fatalf("read scripts/helix-pre-receive.sh: %v", err)
	}

	tmpDir := t.TempDir()
	bareDir := filepath.Join(tmpDir, "repo.git")
	workDir := filepath.Join(tmpDir, "work")

	// Install the real hook as the bare repo's pre-receive hook, pointing
	// HELIX_BIN at the freshly built binary. HELIX_TRUST_TIER=provisional
	// makes the trust check evaluate (empty tier is SKIPPED); the rejection
	// comes from the commit-attestation check failing on the unattested
	// commit — deterministic either way.
	hookDir := filepath.Join(bareDir, "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatalf("MkdirAll hooks: %v", err)
	}
	hookPath := filepath.Join(hookDir, "pre-receive")
	if err := os.WriteFile(hookPath, script, 0o755); err != nil {
		t.Fatalf("write pre-receive hook: %v", err)
	}

	hookEnv := append(os.Environ(),
		"HELIX_BIN="+testHelixBin,
		"HELIX_TRUST_TIER=provisional",
	)

	runGit := func(dir string, args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = hookEnv
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	mustGit := func(dir string, args ...string) string {
		out, err := runGit(dir, args...)
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return out
	}

	// Bare repo + work clone.
	mustGit(tmpDir, "init", "--bare", "repo.git")
	mustGit(tmpDir, "init", "-b", "main", "work")
	mustGit(workDir, "config", "user.email", "test@helix.dev")
	mustGit(workDir, "config", "user.name", "Test")

	// Initial commit + push: creates refs/heads/main in the bare repo.
	// Branch creation is attestation-skipped, so this push succeeds.
	if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte("# Test\n"), 0o644); err != nil {
		t.Fatalf("WriteFile README.md: %v", err)
	}
	mustGit(workDir, "add", ".")
	mustGit(workDir, "commit", "-m", "Initial commit")
	mustGit(workDir, "remote", "add", "origin", bareDir)
	mustGit(workDir, "push", "origin", "main")

	// Unattested change (no Co-authored-by / Helix-Agent trailer) on the
	// protected branch → the gate must REJECT the push.
	if err := os.WriteFile(filepath.Join(workDir, "feature.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile feature.go: %v", err)
	}
	mustGit(workDir, "add", ".")
	mustGit(workDir, "commit", "-m", "Unattested change")

	out, err := runGit(workDir, "push", "origin", "main")
	if err == nil {
		t.Fatalf("expected push to protected branch main to be REJECTED, but it succeeded:\n%s", out)
	}
	if !strings.Contains(out, "REJECTED") && !strings.Contains(out, "blocked") {
		t.Errorf("expected 'REJECTED'/'blocked' in push output, got:\n%s", out)
	}

	// Same unattested change on a NON-protected branch → gate must allow.
	mustGit(workDir, "checkout", "-b", "feature/x")
	if err := os.WriteFile(filepath.Join(workDir, "more.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile more.go: %v", err)
	}
	mustGit(workDir, "add", ".")
	mustGit(workDir, "commit", "-m", "Unattested feature change")

	out, err = runGit(workDir, "push", "origin", "feature/x")
	if err != nil {
		t.Fatalf("expected push to non-protected branch feature/x to succeed, got error: %v\n%s", err, out)
	}
}
