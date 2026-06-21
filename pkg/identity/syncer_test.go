package identity

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// Test helpers
// -----------------------------------------------------------------------------

// validDryRunConfig returns a ProvisionerConfig that passes Validate() and
// has DryRun=true. KnownFriendsPath / StatePath / SSHKeyDir are pointed at
// temp directories so Sync() can be exercised without touching real files.
func validDryRunConfig(t *testing.T) ProvisionerConfig {
	t.Helper()
	cfg := DefaultProvisionerConfig()
	cfg.ForgejoURL = "https://forgejo.example.com"
	cfg.AdminToken = "tok"
	cfg.KnownFriendsPath = filepath.Join(t.TempDir(), "known-friends.json")
	cfg.SSHKeyDir = filepath.Join(t.TempDir(), "keys")
	cfg.StatePath = filepath.Join(t.TempDir(), "state.json")
	cfg.DryRun = true
	return cfg
}

// validRealConfig is the same as validDryRunConfig but DryRun=false — used
// for tests that exercise real (non-dry-run) code paths.
func validRealConfig(t *testing.T) ProvisionerConfig {
	t.Helper()
	cfg := validDryRunConfig(t)
	cfg.DryRun = false
	return cfg
}

// newSyncer constructs a dry-run Syncer with a nil logger. Returns the
// syncer and its state path (which must NOT be touched on disk in dry-run).
func newDryRunSyncer(t *testing.T) (*Syncer, string) {
	t.Helper()
	cfg := validDryRunConfig(t)
	s, err := NewSyncer(cfg, nil)
	if err != nil {
		t.Fatalf("NewSyncer: %v", err)
	}
	return s, cfg.StatePath
}

// writeStateFile writes a state file at path so a subsequent NewSyncer can
// load it as prior state. Used to test deprovision with non-empty state.
func writeStateFile(t *testing.T, path string, sf *StateFile) {
	t.Helper()
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
}

// -----------------------------------------------------------------------------
// 1. LoadKnownFriends
// -----------------------------------------------------------------------------

func TestLoadKnownFriends(t *testing.T) {
	t.Run("valid_fixture", func(t *testing.T) {
		path := "testdata/known-friends.json"
		kf, err := LoadKnownFriends(path)
		if err != nil {
			t.Fatalf("LoadKnownFriends(%s): %v", path, err)
		}
		if kf == nil {
			t.Fatal("LoadKnownFriends returned nil")
		}
		if len(kf.Agents) != 6 {
			t.Errorf("Agents len = %d, want 6", len(kf.Agents))
		}
		// Sanity-check the size assertion (fixture is ~1630 bytes).
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if info.Size() < 1612 {
			t.Errorf("fixture size = %d, want >= 1612", info.Size())
		}
		// Backfilled Name from map key.
		if kf.Agents["wojons"] == nil || kf.Agents["wojons"].Name != "wojons" {
			t.Errorf("wojons agent missing or Name not backfilled")
		}
		// Status distribution.
		active := kf.ActiveAgents()
		if len(active) != 4 {
			t.Errorf("ActiveAgents len = %d, want 4", len(active))
		}
		offboarded := kf.OffboardedAgents()
		if len(offboarded) != 2 {
			t.Errorf("OffboardedAgents len = %d, want 2", len(offboarded))
		}
	})
	t.Run("empty_fixture", func(t *testing.T) {
		kf, err := LoadKnownFriends("testdata/known-friends-empty.json")
		if err != nil {
			t.Fatalf("LoadKnownFriends: %v", err)
		}
		if len(kf.Agents) != 0 {
			t.Errorf("Agents len = %d, want 0", len(kf.Agents))
		}
		if len(kf.ActiveAgents()) != 0 {
			t.Error("ActiveAgents should be empty for empty fixture")
		}
	})
	t.Run("pending_only_fixture", func(t *testing.T) {
		kf, err := LoadKnownFriends("testdata/known-friends-pending.json")
		if err != nil {
			t.Fatalf("LoadKnownFriends: %v", err)
		}
		if len(kf.Agents) != 1 {
			t.Fatalf("Agents len = %d, want 1", len(kf.Agents))
		}
		a := kf.Agents["new-agent"]
		if a == nil {
			t.Fatal("new-agent not in fixture")
		}
		if a.Status != StatusPending {
			t.Errorf("Status = %q, want pending", a.Status)
		}
		if len(kf.ActiveAgents()) != 0 {
			t.Error("ActiveAgents should not include pending agents")
		}
	})
	t.Run("nonexistent_file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "does-not-exist.json")
		_, err := LoadKnownFriends(path)
		if err == nil {
			t.Fatal("expected error for nonexistent file")
		}
		if !strings.Contains(err.Error(), "FILE_NOT_FOUND") {
			t.Errorf("err = %q, want substring FILE_NOT_FOUND", err.Error())
		}
		var te *TypedError
		if !errors.As(err, &te) {
			t.Errorf("err is not *TypedError: %T", err)
		} else if te.Kind != ErrKindConfig {
			t.Errorf("kind = %q, want %q", te.Kind, ErrKindConfig)
		}
	})
	t.Run("invalid_json", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad.json")
		if err := os.WriteFile(path, []byte("not valid json {{{"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		_, err := LoadKnownFriends(path)
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
		if !strings.Contains(err.Error(), "malformed known-friends.json") {
			t.Errorf("err = %q, want substring %q", err.Error(), "malformed known-friends.json")
		}
	})
}

// -----------------------------------------------------------------------------
// 2. expandHome
// -----------------------------------------------------------------------------

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("os.UserHomeDir failed: %v", err)
	}
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"tilde_only", "~", home},
		{"tilde_slash_foo", "~/foo", filepath.Join(home, "foo")},
		{"tilde_dot_helix", "~/.helix", filepath.Join(home, ".helix")},
		{"absolute_path", "/abs", "/abs"},
		{"empty", "", ""},
		{"relative", "relative", "relative"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := expandHome(tc.in); got != tc.want {
				t.Errorf("expandHome(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 3. loadStateFile
// -----------------------------------------------------------------------------

func TestLoadStateFile(t *testing.T) {
	t.Run("nonexistent_returns_empty", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		sf, err := loadStateFile(path)
		if err != nil {
			t.Fatalf("loadStateFile: %v", err)
		}
		if sf == nil {
			t.Fatal("loadStateFile returned nil state")
		}
		if sf.Version != 1 {
			t.Errorf("Version = %d, want 1", sf.Version)
		}
		if sf.Agents == nil {
			t.Error("Agents is nil, want empty map")
		}
		if len(sf.Agents) != 0 {
			t.Errorf("Agents len = %d, want 0", len(sf.Agents))
		}
	})
	t.Run("malformed_returns_error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad-state.json")
		if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		_, err := loadStateFile(path)
		if err == nil {
			t.Fatal("expected error for malformed state file")
		}
		var te *TypedError
		if !errors.As(err, &te) {
			t.Errorf("err is not *TypedError: %T", err)
		} else if te.Kind != ErrKindInternal {
			t.Errorf("kind = %q, want %q", te.Kind, ErrKindInternal)
		}
	})
	t.Run("valid_round_trip", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		want := NewStateFile()
		want.LastSync = stamp
		want.Agents["wojons"] = &AgentState{
			ForgejoAccountID: 42,
			SSHKeyID:         7,
			SSHFingerprint:   "SHA256:abc",
			PATLastEight:     "****12345678",
			PATID:            99,
			LastProvisioned:  stamp,
		}
		writeStateFile(t, path, want)
		got, err := loadStateFile(path)
		if err != nil {
			t.Fatalf("loadStateFile: %v", err)
		}
		if got.Version != 1 {
			t.Errorf("Version = %d, want 1", got.Version)
		}
		if !got.LastSync.Equal(stamp) {
			t.Errorf("LastSync = %s, want %s", got.LastSync, stamp)
		}
		if got.Agents["wojons"] == nil {
			t.Fatal("wojons missing from loaded state")
		}
		ws := got.Agents["wojons"]
		if ws.ForgejoAccountID != 42 || ws.SSHKeyID != 7 || ws.PATID != 99 ||
			ws.SSHFingerprint != "SHA256:abc" || ws.PATLastEight != "****12345678" {
			t.Errorf("AgentState mismatch: %+v", ws)
		}
	})
}

// -----------------------------------------------------------------------------
// 4. saveState
// -----------------------------------------------------------------------------

func TestSaveState_RoundTrip(t *testing.T) {
	cfg := validRealConfig(t)
	s, err := NewSyncer(cfg, nil)
	if err != nil {
		t.Fatalf("NewSyncer: %v", err)
	}
	// Populate state in memory.
	stamp := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	s.state.LastSync = stamp
	s.state.Agents["wojons"] = &AgentState{
		ForgejoAccountID: 1,
		SSHKeyID:         2,
		SSHFingerprint:   "SHA256:deadbeef",
		PATLastEight:     "****abcd1234",
		PATID:            3,
		LastProvisioned:  stamp,
	}
	s.state.Agents["llopez"] = &AgentState{
		ForgejoAccountID: 10,
		SSHKeyID:         20,
		SSHFingerprint:   "SHA256:cafebabe",
		PATLastEight:     "****5678efgh",
		PATID:            30,
		LastProvisioned:  stamp,
	}

	// Write to disk.
	if err := s.saveState(); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	// Verify file exists.
	if _, err := os.Stat(cfg.StatePath); err != nil {
		t.Fatalf("state file missing after saveState: %v", err)
	}
	// Read it back.
	loaded, err := loadStateFile(cfg.StatePath)
	if err != nil {
		t.Fatalf("loadStateFile: %v", err)
	}
	if loaded.Version != 1 {
		t.Errorf("Version = %d, want 1", loaded.Version)
	}
	if !loaded.LastSync.Equal(stamp) {
		t.Errorf("LastSync = %s, want %s", loaded.LastSync, stamp)
	}
	if len(loaded.Agents) != 2 {
		t.Errorf("Agents len = %d, want 2", len(loaded.Agents))
	}
	if loaded.Agents["wojons"] == nil || loaded.Agents["wojons"].SSHFingerprint != "SHA256:deadbeef" {
		t.Errorf("wojons AgentState mismatch: %+v", loaded.Agents["wojons"])
	}
	if loaded.Agents["llopez"] == nil || loaded.Agents["llopez"].PATID != 30 {
		t.Errorf("llopez AgentState mismatch: %+v", loaded.Agents["llopez"])
	}

	// Also verify the on-disk JSON is parseable by stdlib and contains
	// the expected top-level keys.
	raw, err := os.ReadFile(cfg.StatePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("on-disk state not valid JSON: %v", err)
	}
	for _, key := range []string{"version", "last_sync", "agents"} {
		if _, ok := probe[key]; !ok {
			t.Errorf("on-disk JSON missing key %q", key)
		}
	}
}

// -----------------------------------------------------------------------------
// 5. NewSyncer
// -----------------------------------------------------------------------------

func TestNewSyncer(t *testing.T) {
	t.Run("valid_config", func(t *testing.T) {
		cfg := validDryRunConfig(t)
		s, err := NewSyncer(cfg, nil)
		if err != nil {
			t.Fatalf("NewSyncer: %v", err)
		}
		if s == nil {
			t.Fatal("NewSyncer returned nil syncer")
		}
	})
	t.Run("invalid_config_empty_url", func(t *testing.T) {
		cfg := validDryRunConfig(t)
		cfg.ForgejoURL = "" // breaks Validate()
		_, err := NewSyncer(cfg, nil)
		if err == nil {
			t.Fatal("expected error for empty ForgejoURL")
		}
	})
	t.Run("nil_logger_uses_default", func(t *testing.T) {
		cfg := validDryRunConfig(t)
		s, err := NewSyncer(cfg, nil)
		if err != nil {
			t.Fatalf("NewSyncer: %v", err)
		}
		// The default logger wraps log.Logger; just confirm it is non-nil
		// and responds to Printf without panicking.
		if s.log == nil {
			t.Error("logger is nil after NewSyncer with nil arg")
		}
		s.log.Printf("smoke test") // must not panic
	})
	t.Run("custom_logger_honored", func(t *testing.T) {
		cfg := validDryRunConfig(t)
		buf := &threadSafeBuf{}
		lg := newCapturingLogger(buf)
		s, err := NewSyncer(cfg, lg)
		if err != nil {
			t.Fatalf("NewSyncer: %v", err)
		}
		s.log.Printf("hello-from-test")
		if !strings.Contains(buf.String(), "hello-from-test") {
			t.Errorf("custom logger did not capture output: %q", buf.String())
		}
	})
}

// -----------------------------------------------------------------------------
// 6. Syncer.Provisioner / Syncer.State
// -----------------------------------------------------------------------------

func TestSyncer_Accessors(t *testing.T) {
	s, _ := newDryRunSyncer(t)
	if s.Provisioner() == nil {
		t.Error("Provisioner() returned nil")
	}
	if s.State() == nil {
		t.Error("State() returned nil")
	}
}

// -----------------------------------------------------------------------------
// 7. Syncer.Sync (dry-run)
// -----------------------------------------------------------------------------

func TestSyncer_Sync_DryRun(t *testing.T) {
	s, statePath := newDryRunSyncer(t)

	t.Run("nil_known_friends", func(t *testing.T) {
		results, err := s.Sync(nil, SyncOptions{})
		if err == nil {
			t.Fatal("expected error for nil KnownFriends")
		}
		if results != nil {
			t.Errorf("results = %v, want nil", results)
		}
		var te *TypedError
		if !errors.As(err, &te) || te.Kind != ErrKindConfig {
			t.Errorf("err = %v, want *TypedError with kind=config", err)
		}
	})

	t.Run("empty_known_friends", func(t *testing.T) {
		kf := &KnownFriends{Version: 1, Agents: map[string]*Agent{}}
		results, err := s.Sync(kf, SyncOptions{})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("results len = %d, want 0", len(results))
		}
	})

	t.Run("fixture_six_agents", func(t *testing.T) {
		kf, err := LoadKnownFriends("testdata/known-friends.json")
		if err != nil {
			t.Fatalf("LoadKnownFriends: %v", err)
		}
		results, runErr := s.Sync(kf, SyncOptions{AdminUser: "admin", AdminPassword: "adminpass"})
		if runErr != nil {
			t.Fatalf("Sync err: %v", runErr)
		}
		if len(results) != 6 {
			t.Fatalf("results len = %d, want 6", len(results))
		}

		// Tally actions.
		created, skipped := 0, 0
		for _, r := range results {
			switch r.Action {
			case ActionCreated:
				created++
			case ActionSkipped:
				skipped++
			}
		}
		if created != 4 {
			t.Errorf("created count = %d, want 4 (active agents)", created)
		}
		if skipped != 2 {
			t.Errorf("skipped count = %d, want 2 (offboarded, no prior state)", skipped)
		}

		// Verify result fields.
		names := make([]string, 0, len(results))
		for _, r := range results {
			if r.AgentName == "" {
				t.Error("result with empty AgentName")
			}
			if r.Status == "" {
				t.Errorf("result %q has empty Status", r.AgentName)
			}
			if r.Action == "" {
				t.Errorf("result %q has empty Action", r.AgentName)
			}
			if r.Duration < 0 {
				t.Errorf("result %q has negative Duration: %s", r.AgentName, r.Duration)
			}
			names = append(names, r.AgentName)
		}
		sort.Strings(names)
		wantNames := []string{"bbala", "dtoole", "jrestrepo", "kellyv", "llopez", "wojons"}
		for i, n := range wantNames {
			if names[i] != n {
				t.Errorf("names[%d] = %q, want %q (full: %v)", i, names[i], n, names)
			}
		}

		// Active agents should have an Account (dry-run returns synthetic).
		// Offboarded agents (skipped) have no account because they were
		// never provisioned through this run.
		//
		// Note: dry-run CreateToken returns a synthetic AccessToken with
		// only Name/Scopes set (no .Token, no .ID), so r.PATID and
		// r.PATLastEight remain zero in dry-run. The real-mode path
		// populates them from the Forgejo response.
		for _, r := range results {
			if r.Action == ActionCreated && r.Account == nil {
				t.Errorf("active result %q missing Account", r.AgentName)
			}
			if r.Action == ActionCreated && r.SSHFingerprint == "" {
				t.Errorf("active result %q missing SSHFingerprint", r.AgentName)
			}
		}
	})

	t.Run("no_state_file_written_in_dry_run", func(t *testing.T) {
		// Re-run a tiny sync; the state file at statePath must NOT exist
		// after the call (dry-run must never touch disk).
		if _, err := os.Stat(statePath); err == nil {
			// Defensive: if the file leaked from an earlier test, wipe it.
			if rmErr := os.Remove(statePath); rmErr != nil {
				t.Fatalf("pre-state leaked and could not remove: %v", rmErr)
			}
		}
		kf := &KnownFriends{Version: 1, Agents: map[string]*Agent{}}
		if _, err := s.Sync(kf, SyncOptions{}); err != nil {
			t.Fatalf("Sync: %v", err)
		}
		if _, err := os.Stat(statePath); err == nil {
			t.Errorf("state file was written in dry-run at %s", statePath)
		} else if !os.IsNotExist(err) {
			t.Errorf("unexpected stat error: %v", err)
		}
	})
}

// -----------------------------------------------------------------------------
// 8. writeKeyFiles
// -----------------------------------------------------------------------------

func TestWriteKeyFiles(t *testing.T) {
	t.Run("dry_run_returns_keypair_no_files", func(t *testing.T) {
		s, _ := newDryRunSyncer(t)
		a := &Agent{Name: "wojons", Status: StatusActive, Tier: TierPro}
		kp, err := s.writeKeyFiles(a)
		if err != nil {
			t.Fatalf("writeKeyFiles: %v", err)
		}
		if kp == nil {
			t.Fatal("writeKeyFiles returned nil KeyPair in dry-run")
		}
		if !strings.HasPrefix(kp.PublicKeyOpenSSH, "ssh-ed25519 ") {
			t.Errorf("PublicKeyOpenSSH prefix = %q, want ssh-ed25519", kp.PublicKeyOpenSSH)
		}
		// No files written under dry-run SSHKeyDir.
		dir := filepath.Join(s.cfg.SSHKeyDir, "wojons")
		if _, err := os.Stat(dir); err == nil {
			t.Errorf("key dir %s should not exist in dry-run", dir)
		} else if !os.IsNotExist(err) {
			t.Errorf("unexpected stat error: %v", err)
		}
	})

	t.Run("real_mode_writes_three_files", func(t *testing.T) {
		cfg := validRealConfig(t)
		s, err := NewSyncer(cfg, nil)
		if err != nil {
			t.Fatalf("NewSyncer: %v", err)
		}
		a := &Agent{Name: "wojons", Status: StatusActive, Tier: TierPro}
		kp, err := s.writeKeyFiles(a)
		if err != nil {
			t.Fatalf("writeKeyFiles: %v", err)
		}
		if kp == nil {
			t.Fatal("writeKeyFiles returned nil KeyPair in real mode")
		}
		dir := filepath.Join(cfg.SSHKeyDir, "wojons")
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir %s: %v", dir, err)
		}
		gotNames := make([]string, 0, len(entries))
		for _, e := range entries {
			gotNames = append(gotNames, e.Name())
		}
		sort.Strings(gotNames)
		want := []string{"id_ed25519", "id_ed25519.pub", "id_ed25519.state"}
		if len(gotNames) != len(want) {
			t.Errorf("files written = %v, want %v", gotNames, want)
		}
		for i, n := range want {
			if i >= len(gotNames) || gotNames[i] != n {
				t.Errorf("file[%d] = %q, want %q (full: %v)", i, gotNames[i], n, gotNames)
			}
		}
		// Verify file permissions on the private key (mode 0600).
		info, err := os.Stat(filepath.Join(dir, "id_ed25519"))
		if err != nil {
			t.Fatalf("stat private key: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("private key perm = %o, want 600", perm)
		}
		// Verify the public key file is non-empty and starts with the
		// expected algorithm tag.
		pubBytes, err := os.ReadFile(filepath.Join(dir, "id_ed25519.pub"))
		if err != nil {
			t.Fatalf("read pub: %v", err)
		}
		if !strings.HasPrefix(string(pubBytes), "ssh-ed25519 ") {
			t.Errorf("pub file content = %q, want prefix ssh-ed25519", string(pubBytes))
		}
	})
}

// -----------------------------------------------------------------------------
// 9. archiveKeys
// -----------------------------------------------------------------------------

func TestArchiveKeys(t *testing.T) {
	t.Run("directory_with_files_moves_to_archive", func(t *testing.T) {
		s, _ := newDryRunSyncer(t)
		// Create the key dir and a couple of files (real filesystem ops;
		// archiveKeys does not honor DryRun).
		name := "kellyv"
		src := filepath.Join(s.cfg.SSHKeyDir, name)
		if err := os.MkdirAll(src, 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		files := map[string]string{
			"id_ed25519":       "ZmFrZS1wcml2YXRlLWtleS1kYXRh", // base64 "fake-private-key-data" — not a real key
			"id_ed25519.pub":   "ssh-ed25519 AAAA",
			"id_ed25519.state": `{"fingerprint":"SHA256:abc"}`,
		}
		for fname, content := range files {
			if err := os.WriteFile(filepath.Join(src, fname), []byte(content), 0o600); err != nil {
				t.Fatalf("write %s: %v", fname, err)
			}
		}

		// archiveKeys computes the dated subdir from time.Now() and
		// creates the parent (`src/archive/`) — but does NOT create the
		// dated subdir itself before renaming. To exercise the happy
		// "files get moved" path, pre-create the dated subdir the way
		// a second archive call on the same day would.
		dated := filepath.Join(src, "archive", time.Now().UTC().Format("2006-01-02"))
		if err := os.MkdirAll(dated, 0o700); err != nil {
			t.Fatalf("mkdir dated: %v", err)
		}

		if err := s.archiveKeys(name); err != nil {
			t.Fatalf("archiveKeys: %v", err)
		}

		// After archival: src/ should contain only "archive/", and the
		// files should now live under src/archive/<date>/.
		entries, err := os.ReadDir(src)
		if err != nil {
			t.Fatalf("ReadDir src: %v", err)
		}
		if len(entries) != 1 || entries[0].Name() != "archive" {
			gotNames := make([]string, len(entries))
			for i, e := range entries {
				gotNames[i] = e.Name()
			}
			t.Errorf("src contents = %v, want only [archive]", gotNames)
		}
		archiveEntries, err := os.ReadDir(filepath.Join(src, "archive"))
		if err != nil {
			t.Fatalf("ReadDir archive: %v", err)
		}
		if len(archiveEntries) == 0 {
			t.Fatal("archive subdir is empty")
		}
		// Find the dated subdir.
		var datedDir string
		for _, e := range archiveEntries {
			if e.IsDir() {
				datedDir = filepath.Join(src, "archive", e.Name())
				break
			}
		}
		if datedDir == "" {
			t.Fatal("no dated subdir under archive/")
		}
		// All three original files should now be in the dated subdir.
		datedEntries, err := os.ReadDir(datedDir)
		if err != nil {
			t.Fatalf("ReadDir dated: %v", err)
		}
		gotNames := make([]string, 0, len(datedEntries))
		for _, e := range datedEntries {
			gotNames = append(gotNames, e.Name())
		}
		sort.Strings(gotNames)
		want := []string{"id_ed25519", "id_ed25519.pub", "id_ed25519.state"}
		if len(gotNames) != len(want) {
			t.Errorf("archived files = %v, want %v", gotNames, want)
		}
		for i, n := range want {
			if i >= len(gotNames) || gotNames[i] != n {
				t.Errorf("archived[%d] = %q, want %q (full: %v)", i, gotNames[i], n, gotNames)
			}
		}
	})

	t.Run("directory_does_not_exist_returns_nil", func(t *testing.T) {
		s, _ := newDryRunSyncer(t)
		if err := s.archiveKeys("never-existed"); err != nil {
			t.Errorf("archiveKeys on missing dir = %v, want nil", err)
		}
	})
}

// -----------------------------------------------------------------------------
// 10. ProvisionOne (dry-run)
// -----------------------------------------------------------------------------

func TestProvisionOne_DryRun(t *testing.T) {
	s, _ := newDryRunSyncer(t)
	a := &Agent{
		Name:        "wojons",
		DisplayName: "Wojons",
		Status:      StatusActive,
		Tier:        TierPro,
	}
	r, err := s.ProvisionOne(a, SyncOptions{AdminUser: "admin", AdminPassword: "adminpass"})
	if err != nil {
		t.Fatalf("ProvisionOne: %v", err)
	}
	if r.AgentName != "wojons" {
		t.Errorf("AgentName = %q, want wojons", r.AgentName)
	}
	if r.Action != ActionCreated {
		t.Errorf("Action = %q, want %q", r.Action, ActionCreated)
	}
	if r.SSHFingerprint == "" {
		t.Error("SSHFingerprint is empty")
	}
	if !r.Succeeded() {
		t.Error("Succeeded() = false, want true")
	}
}

// -----------------------------------------------------------------------------
// 11. DeprovisionOne
// -----------------------------------------------------------------------------

func TestDeprovisionOne_DryRun(t *testing.T) {
	t.Run("no_prior_state_returns_skipped", func(t *testing.T) {
		s, _ := newDryRunSyncer(t)
		a := &Agent{
			Name:   "kellyv",
			Status: StatusOffboarded,
			Tier:   TierFlash,
		}
		r, err := s.DeprovisionOne(a, SyncOptions{})
		if err != nil {
			t.Fatalf("DeprovisionOne: %v", err)
		}
		if r.Action != ActionSkipped {
			t.Errorf("Action = %q, want %q", r.Action, ActionSkipped)
		}
		if !r.Succeeded() {
			t.Error("Succeeded() = false, want true (skipped is success)")
		}
	})

	t.Run("with_prior_state_returns_deprovisioned", func(t *testing.T) {
		cfg := validDryRunConfig(t)
		// Pre-populate the state file with kellyv as previously provisioned.
		stamp := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
		pre := NewStateFile()
		pre.Agents["kellyv"] = &AgentState{
			ForgejoAccountID: 99,
			SSHKeyID:         100,
			SSHFingerprint:   "SHA256:prior",
			PATLastEight:     "****priorPAT",
			PATID:            101,
			LastProvisioned:  stamp,
		}
		writeStateFile(t, cfg.StatePath, pre)

		s, err := NewSyncer(cfg, nil)
		if err != nil {
			t.Fatalf("NewSyncer: %v", err)
		}
		a := &Agent{
			Name:   "kellyv",
			Status: StatusOffboarded,
			Tier:   TierFlash,
		}
		r, err := s.DeprovisionOne(a, SyncOptions{AdminUser: "admin", AdminPassword: "adminpass"})
		if err != nil {
			t.Fatalf("DeprovisionOne: %v", err)
		}
		if r.Action != ActionDeprovisioned {
			t.Errorf("Action = %q, want %q", r.Action, ActionDeprovisioned)
		}
	})
}

// -----------------------------------------------------------------------------
// 12. KeyGenOnly
// -----------------------------------------------------------------------------

func TestKeyGenOnly(t *testing.T) {
	t.Run("dry_run_returns_created_with_fingerprint", func(t *testing.T) {
		s, _ := newDryRunSyncer(t)
		a := &Agent{
			Name:   "wojons",
			Status: StatusActive,
			Tier:   TierPro,
		}
		r, err := s.KeyGenOnly(a)
		if err != nil {
			t.Fatalf("KeyGenOnly: %v", err)
		}
		if r.Action != ActionCreated {
			t.Errorf("Action = %q, want %q", r.Action, ActionCreated)
		}
		if r.SSHFingerprint == "" {
			t.Error("SSHFingerprint is empty")
		}
		if !strings.HasPrefix(r.SSHFingerprint, "SHA256:") {
			t.Errorf("Fingerprint = %q, want SHA256: prefix", r.SSHFingerprint)
		}
		// Dry-run must not write files.
		dir := filepath.Join(s.cfg.SSHKeyDir, "wojons")
		if _, err := os.Stat(dir); err == nil {
			t.Errorf("key dir %s should not exist in dry-run", dir)
		}
	})

	t.Run("real_mode_writes_key_files", func(t *testing.T) {
		cfg := validRealConfig(t)
		s, err := NewSyncer(cfg, nil)
		if err != nil {
			t.Fatalf("NewSyncer: %v", err)
		}
		a := &Agent{
			Name:   "wojons",
			Status: StatusActive,
			Tier:   TierPro,
		}
		r, err := s.KeyGenOnly(a)
		if err != nil {
			t.Fatalf("KeyGenOnly: %v", err)
		}
		if r.Action != ActionCreated {
			t.Errorf("Action = %q, want %q", r.Action, ActionCreated)
		}
		if r.SSHFingerprint == "" {
			t.Error("SSHFingerprint is empty")
		}
		// Real mode should have written the three key files.
		dir := filepath.Join(cfg.SSHKeyDir, "wojons")
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir %s: %v", dir, err)
		}
		if len(entries) != 3 {
			names := make([]string, len(entries))
			for i, e := range entries {
				names[i] = e.Name()
			}
			t.Errorf("wrote %d files (%v), want 3", len(entries), names)
		}
	})
}

// -----------------------------------------------------------------------------
// Test logger plumbing (used only by TestNewSyncer/custom_logger_honored)
// -----------------------------------------------------------------------------

// threadSafeBuf is a minimal sync.Mutex-guarded bytes.Buffer substitute
// so the capturing logger can be used from concurrent goroutines if needed
// (it isn't in these tests, but it's cheap insurance).
type threadSafeBuf struct {
	mu  sync.Mutex
	buf []byte
}

func (b *threadSafeBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *threadSafeBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

// -----------------------------------------------------------------------------
// 13. accountID
// -----------------------------------------------------------------------------

func TestAccountID(t *testing.T) {
	t.Run("nil_account", func(t *testing.T) {
		if got := accountID(nil); got != 0 {
			t.Errorf("accountID(nil) = %d, want 0", got)
		}
	})
	t.Run("valid_account", func(t *testing.T) {
		a := &ForgejoAccount{ID: 42}
		if got := accountID(a); got != 42 {
			t.Errorf("accountID({ID:42}) = %d, want 42", got)
		}
	})
	t.Run("zero_id_account", func(t *testing.T) {
		a := &ForgejoAccount{ID: 0}
		if got := accountID(a); got != 0 {
			t.Errorf("accountID({ID:0}) = %d, want 0", got)
		}
	})
}

// -----------------------------------------------------------------------------
// 14. Sync all-failure path (non-dry-run, all agents get ErrNotImplemented)
// -----------------------------------------------------------------------------

func TestSyncer_Sync_AllFail(t *testing.T) {
	if testing.Short() {
		t.Skip("makes real HTTP calls — use -count=1 without -short")
	}
	cfg := validRealConfig(t)
	s, err := NewSyncer(cfg, nil)
	if err != nil {
		t.Fatalf("NewSyncer: %v", err)
	}
	// Create a known-friends file with two active agents so Sync has
	// something to process. In non-dry-run mode every method returns
	// ErrNotImplemented, so every agent fails.
	kfPath := filepath.Join(t.TempDir(), "known-friends.json")
	kf := &KnownFriends{
		Version: 1,
		Agents: map[string]*Agent{
			"wojons": {Name: "wojons", DisplayName: "W", Status: StatusActive, Tier: TierPro},
			"bbala":  {Name: "bbala", DisplayName: "B", Status: StatusActive, Tier: TierFlash},
		},
	}
	data, _ := json.Marshal(kf)
	if err := os.WriteFile(kfPath, data, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	loaded, err := LoadKnownFriends(kfPath)
	if err != nil {
		t.Fatalf("LoadKnownFriends: %v", err)
	}

	results, runErr := s.Sync(loaded, SyncOptions{AdminUser: "admin", AdminPassword: "adminpass"})
	if runErr == nil {
		t.Fatal("expected error from Sync (all agents failed)")
	}
	// All agents should be ActionFailed.
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2", len(results))
	}
	for _, r := range results {
		if r.Action != ActionFailed {
			t.Errorf("agent %q action = %q, want %q (all fail in real mode)", r.AgentName, r.Action, ActionFailed)
		}
	}
	// Error should be a TypedError of kind partial (all-failed case).
	var te *TypedError
	if !errors.As(runErr, &te) {
		t.Errorf("runErr = %T, want *TypedError", runErr)
	} else if te.Kind != ErrKindPartial {
		t.Errorf("Kind = %q, want %q", te.Kind, ErrKindPartial)
	}
}

// -----------------------------------------------------------------------------
// 15. ProvisionOne failure path (non-dry-run → ErrNotImplemented)
// -----------------------------------------------------------------------------

func TestProvisionOne_Failure(t *testing.T) {
	cfg := validRealConfig(t)
	s, err := NewSyncer(cfg, nil)
	if err != nil {
		t.Fatalf("NewSyncer: %v", err)
	}
	a := &Agent{
		Name:        "wojons",
		DisplayName: "Wojons",
		Status:      StatusActive,
		Tier:        TierPro,
	}
	r, runErr := s.ProvisionOne(a, SyncOptions{AdminUser: "admin", AdminPassword: "adminpass"})
	if runErr == nil {
		t.Fatal("expected error from ProvisionOne (ErrNotImplemented)")
	}
	if r.Action != ActionFailed {
		t.Errorf("Action = %q, want %q", r.Action, ActionFailed)
	}
	if r.Error == "" {
		t.Error("Error is empty, want non-empty")
	}
	if r.Succeeded() {
		t.Error("Succeeded() = true, want false")
	}
}

// -----------------------------------------------------------------------------
// 16. DeprovisionOne failure path (non-dry-run → ErrNotImplemented)
// -----------------------------------------------------------------------------

func TestDeprovisionOne_Failure(t *testing.T) {
	cfg := validRealConfig(t)
	// Pre-populate state so deprovisionAgent finds a PAT to revoke
	// (otherwise it skips and we don't hit the failure path).
	stamp := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	pre := NewStateFile()
	pre.Agents["kellyv"] = &AgentState{
		ForgejoAccountID: 99,
		SSHKeyID:         100,
		SSHFingerprint:   "SHA256:prior",
		PATLastEight:     "****priorPAT",
		PATID:            101,
		LastProvisioned:  stamp,
	}
	writeStateFile(t, cfg.StatePath, pre)

	s, err := NewSyncer(cfg, nil)
	if err != nil {
		t.Fatalf("NewSyncer: %v", err)
	}
	a := &Agent{
		Name:   "kellyv",
		Status: StatusOffboarded,
		Tier:   TierFlash,
	}
	r, runErr := s.DeprovisionOne(a, SyncOptions{AdminUser: "admin", AdminPassword: "adminpass"})
	if runErr == nil {
		t.Fatal("expected error from DeprovisionOne (ErrNotImplemented)")
	}
	if r.Action != ActionFailed {
		t.Errorf("Action = %q, want %q", r.Action, ActionFailed)
	}
	if r.Error == "" {
		t.Error("Error is empty, want non-empty")
	}
	if r.Succeeded() {
		t.Error("Succeeded() = true, want false")
	}
}

// -----------------------------------------------------------------------------
// 17. loadStateFile with permission error (when state file exists but
//     is unreadable — this path is normally hard to hit on Linux, so we
//     use a path that will trigger the read-error branch).
// -----------------------------------------------------------------------------

func TestLoadStateFile_PermissionError(t *testing.T) {
	// Use a path that exists but is a directory — os.ReadFile on a
	// directory returns an error on Linux, exercising the non-IsNotExist
	// branch of loadStateFile.
	path := t.TempDir()
	_, err := loadStateFile(path)
	if err == nil {
		t.Fatal("expected error reading directory as state file")
	}
	var te *TypedError
	if !errors.As(err, &te) {
		t.Errorf("err = %T, want *TypedError", err)
	} else if te.Kind != ErrKindInternal {
		t.Errorf("Kind = %q, want %q", te.Kind, ErrKindInternal)
	}
}

// capturingLogger adapts threadSafeBuf to the identity.Logger interface so
// tests can inspect what the syncer wrote.
type capturingLogger struct {
	w *threadSafeBuf
}

func newCapturingLogger(w *threadSafeBuf) *capturingLogger { return &capturingLogger{w: w} }
func (c *capturingLogger) Printf(format string, args ...any) {
	fmt.Fprintf(c.w, format, args...)
}
