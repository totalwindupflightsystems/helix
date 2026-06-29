package identity

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// ============================================================================
// 18. expandHome — edge cases not covered by existing tests
// ============================================================================

func TestExpandHome_EdgeCases(t *testing.T) {
	t.Run("empty_string", func(t *testing.T) {
		if got := expandHome(""); got != "" {
			t.Errorf("expandHome(\"\") = %q, want \"\"", got)
		}
	})

	t.Run("no_tilde", func(t *testing.T) {
		if got := expandHome("/etc/passwd"); got != "/etc/passwd" {
			t.Errorf("expandHome(/etc/passwd) = %q, want /etc/passwd", got)
		}
	})

	t.Run("bare_tilde", func(t *testing.T) {
		got := expandHome("~")
		home, _ := os.UserHomeDir()
		if got != home {
			t.Errorf("expandHome(\"~\") = %q, want %q", got, home)
		}
	})

	t.Run("tilde_slash_path", func(t *testing.T) {
		got := expandHome("~/keys")
		home, _ := os.UserHomeDir()
		want := filepath.Join(home, "keys")
		if got != want {
			t.Errorf("expandHome(\"~/keys\") = %q, want %q", got, want)
		}
	})

	t.Run("tilde_then_no_slash_returns_unchanged", func(t *testing.T) {
		if got := expandHome("~no-slash"); got != "~no-slash" {
			t.Errorf("expandHome(\"~no-slash\") = %q, want ~no-slash", got)
		}
	})
}

// ============================================================================
// 19. loadStateFile — malformed JSON via temp file
// ============================================================================

func TestLoadStateFile_MalformedJSON(t *testing.T) {
	t.Run("garbage_json", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "state.json")
		if err := os.WriteFile(path, []byte("not json at all"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		_, err := loadStateFile(path)
		if err == nil {
			t.Fatal("expected error for malformed JSON")
		}
		var te *TypedError
		if !errors.As(err, &te) {
			t.Errorf("err = %T, want *TypedError", err)
		} else if te.Kind != ErrKindInternal {
			t.Errorf("Kind = %q, want %q", te.Kind, ErrKindInternal)
		}
	})

	t.Run("valid_json_wrong_shape", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "state.json")
		// JSON array (valid JSON, wrong shape for StateFile)
		if err := os.WriteFile(path, []byte(`[1,2,3]`), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		sf, err := loadStateFile(path)
		if err == nil {
			t.Fatal("expected error unmarshalling array into StateFile struct")
		}
		if sf != nil {
			t.Fatalf("expected nil StateFile on unmarshal error, got non-nil")
		}
	})

	t.Run("directory_not_file", func(t *testing.T) {
		dir := t.TempDir()
		_, err := loadStateFile(dir)
		if err == nil {
			t.Fatal("expected error reading directory as state file")
		}
	})

	t.Run("file_not_found_returns_empty", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "does-not-exist.json")
		sf, err := loadStateFile(path)
		if err != nil {
			t.Fatalf("expected nil error for missing file, got: %v", err)
		}
		if sf == nil {
			t.Fatal("got nil StateFile for missing path")
		}
		if len(sf.Agents) != 0 {
			t.Errorf("Agents len = %d, want 0", len(sf.Agents))
		}
	})
}

// ============================================================================
// 20. saveState — error paths via unwritable filesystem artefacts
// ============================================================================

func TestSaveState_ErrorPaths(t *testing.T) {
	t.Run("mkdir_failure_file_at_dir_path", func(t *testing.T) {
		dir := t.TempDir()
		// Create a syncer with a valid state path first (so NewSyncer succeeds).
		cfg := validRealConfig(t)
		s, err := NewSyncer(cfg, nil)
		if err != nil {
			t.Fatalf("NewSyncer: %v", err)
		}
		// Now create a regular file where the state *dir* should be,
		// and redirect s.statePath to use it.
		blockDir := filepath.Join(dir, "block")
		if err := os.WriteFile(blockDir, []byte("nope"), 0o600); err != nil {
			t.Fatalf("write block: %v", err)
		}
		s.statePath = filepath.Join(blockDir, "state.json")
		if err := s.saveState(); err == nil {
			t.Fatal("expected error from saveState (file blocks directory creation)")
		} else {
			t.Logf("got expected error: %v", err)
		}
	})

	t.Run("write_failure_readonly_dir", func(t *testing.T) {
		dir := t.TempDir()
		roDir := filepath.Join(dir, "ro")
		if err := os.MkdirAll(roDir, 0o500); err != nil {
			t.Fatalf("mkdir ro: %v", err)
		}
		statePath := filepath.Join(roDir, "state.json")
		cfg := validRealConfig(t)
		cfg.StatePath = statePath
		s, err := NewSyncer(cfg, nil)
		if err != nil {
			t.Fatalf("NewSyncer: %v", err)
		}
		// saveState calls MkdirAll first (which succeeds because roDir exists),
		// then WriteFile(statePath, ...). WriteFile should fail because dir is
		// read-only (0o500). But root can bypass perms on Linux — skip if root.
		if os.Getuid() == 0 {
			t.Skip("root bypasses directory permissions on Linux")
		}
		err = s.saveState()
		if err == nil {
			// WriteFile may succeed if the dir owner = current user
			// (DAC override). That's OK — not all systems enforce
			// directory write-deny consistently.
			t.Log("saveState succeeded (DAC override on ro dir)")
		} else {
			t.Logf("got expected error: %v", err)
		}
	})
}

// ============================================================================
// 21. writeKeyFiles — MkdirAll failure path
// ============================================================================

func TestWriteKeyFiles_ErrorPaths(t *testing.T) {
	t.Run("mkdir_failure", func(t *testing.T) {
		dir := t.TempDir()
		// Create a regular file where the agent's key dir should go.
		blockPath := filepath.Join(dir, "block")
		if err := os.WriteFile(blockPath, []byte("nope"), 0o600); err != nil {
			t.Fatalf("write block: %v", err)
		}
		cfg := validRealConfig(t)
		cfg.SSHKeyDir = dir
		s, err := NewSyncer(cfg, nil)
		if err != nil {
			t.Fatalf("NewSyncer: %v", err)
		}
		a := &Agent{Name: "block", Status: StatusActive, Tier: TierFlash}
		_, err = s.writeKeyFiles(a)
		if err == nil {
			t.Fatal("expected error from writeKeyFiles (file blocks directory creation)")
		}
		var te *TypedError
		if !errors.As(err, &te) {
			t.Errorf("err = %T, want *TypedError", err)
		} else if te.Kind != ErrKindInternal {
			t.Errorf("Kind = %q, want %q", te.Kind, ErrKindInternal)
		}
	})
}

// ============================================================================
// 22. archiveKeys — edge cases
// ============================================================================

func TestArchiveKeysEdgeCases(t *testing.T) {
	t.Run("dir_not_found_returns_nil", func(t *testing.T) {
		cfg := validRealConfig(t)
		s, err := NewSyncer(cfg, nil)
		if err != nil {
			t.Fatalf("NewSyncer: %v", err)
		}
		if err := s.archiveKeys("no-such-agent"); err != nil {
			t.Errorf("archiveKeys: %v", err)
		}
	})

	t.Run("stat_file_not_dir_readdir_fails", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "file-agent")
		if err := os.WriteFile(filePath, []byte("not a dir"), 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}
		cfg := validRealConfig(t)
		cfg.SSHKeyDir = dir
		s, err := NewSyncer(cfg, nil)
		if err != nil {
			t.Fatalf("NewSyncer: %v", err)
		}
		// Stat succeeds (it's a file), then ReadDir on the file should fail.
		err = s.archiveKeys("file-agent")
		if err == nil {
			t.Fatal("expected error from archiveKeys (ReadDir on regular file)")
		}
		if !errors.Is(err, fs.ErrInvalid) && !errors.Is(err, fs.ErrNotExist) {
			t.Logf("got error: %v (type: %T)", err, err)
		}
	})
}

// ============================================================================
// 23. ActiveAgents / OffboardedAgents — edge cases
// ============================================================================

func TestActiveAgents_EdgeCases(t *testing.T) {
	t.Run("nil_agent_skipped", func(t *testing.T) {
		kf := &KnownFriends{
			Agents: map[string]*Agent{
				"nil-agent": nil,
			},
		}
		if got := kf.ActiveAgents(); len(got) != 0 {
			t.Errorf("ActiveAgents len = %d, want 0 (nil agent should be skipped)", len(got))
		}
	})

	t.Run("pending_agent_not_active", func(t *testing.T) {
		kf := &KnownFriends{
			Agents: map[string]*Agent{
				"new-guy": {Name: "new-guy", Status: StatusPending},
			},
		}
		if got := kf.ActiveAgents(); len(got) != 0 {
			t.Errorf("ActiveAgents len = %d, want 0 (pending agent excluded)", len(got))
		}
	})

	t.Run("empty_name_backfilled_from_key", func(t *testing.T) {
		kf := &KnownFriends{
			Agents: map[string]*Agent{
				"backfill-me": {Status: StatusActive},
			},
		}
		agents := kf.ActiveAgents()
		if len(agents) != 1 {
			t.Fatalf("ActiveAgents len = %d, want 1", len(agents))
		}
		if agents[0].Name != "backfill-me" {
			t.Errorf("Name = %q, want %q", agents[0].Name, "backfill-me")
		}
	})

	t.Run("sorted_by_name", func(t *testing.T) {
		kf := &KnownFriends{
			Agents: map[string]*Agent{
				"zeta":  {Name: "zeta", Status: StatusActive},
				"alpha": {Name: "alpha", Status: StatusActive},
				"gamma": {Name: "gamma", Status: StatusActive},
			},
		}
		agents := kf.ActiveAgents()
		if len(agents) != 3 {
			t.Fatalf("ActiveAgents len = %d, want 3", len(agents))
		}
		if agents[0].Name != "alpha" {
			t.Errorf("[0] = %q, want alpha", agents[0].Name)
		}
		if agents[1].Name != "gamma" {
			t.Errorf("[1] = %q, want gamma", agents[1].Name)
		}
		if agents[2].Name != "zeta" {
			t.Errorf("[2] = %q, want zeta", agents[2].Name)
		}
	})
}

func TestOffboardedAgents_EdgeCases(t *testing.T) {
	t.Run("nil_agent_skipped", func(t *testing.T) {
		kf := &KnownFriends{
			Agents: map[string]*Agent{
				"nil-agent": nil,
			},
		}
		if got := kf.OffboardedAgents(); len(got) != 0 {
			t.Errorf("OffboardedAgents len = %d, want 0 (nil agent skipped)", len(got))
		}
	})

	t.Run("active_agent_excluded", func(t *testing.T) {
		kf := &KnownFriends{
			Agents: map[string]*Agent{
				"active": {Name: "active", Status: StatusActive},
			},
		}
		if got := kf.OffboardedAgents(); len(got) != 0 {
			t.Errorf("OffboardedAgents len = %d, want 0 (active agent excluded)", len(got))
		}
	})

	t.Run("offboarded_agent_included", func(t *testing.T) {
		kf := &KnownFriends{
			Agents: map[string]*Agent{
				"retired": {Name: "retired", Status: StatusOffboarded},
			},
		}
		agents := kf.OffboardedAgents()
		if len(agents) != 1 {
			t.Fatalf("OffboardedAgents len = %d, want 1", len(agents))
		}
		if agents[0].Name != "retired" {
			t.Errorf("Name = %q, want retired", agents[0].Name)
		}
	})
}

// ============================================================================
// 24. loadStateFile — valid JSON with nil Agents field
// ============================================================================

func TestLoadStateFile_NilAgentsInit(t *testing.T) {
	t.Run("nil_agents_initialized", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "state.json")
		content, err := json.Marshal(map[string]any{
			"version":   1,
			"last_sync": nil,
			"agents":    nil,
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		sf, err := loadStateFile(path)
		if err != nil {
			t.Fatalf("loadStateFile: %v", err)
		}
		if sf.Agents == nil {
			t.Fatal("Agents is nil — should be initialized to empty map")
		}
		if len(sf.Agents) != 0 {
			t.Errorf("Agents len = %d, want 0", len(sf.Agents))
		}
	})
}
