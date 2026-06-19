package identity

// syncer.go implements the orchestration layer that sits above the
// Provisioner. Its job is to:
//
//   1. Load + validate known-friends.json
//   2. Classify agents by status (active → provision, offboarded → deprovision)
//   3. Drive the per-agent provisioning state machine
//   4. Track results, render the summary table, decide the exit code
//   5. Persist the idempotency state file
//
// The Syncer never makes HTTP calls directly — it always goes through the
// Provisioner, which is the only thing that touches the network. This makes
// the syncer trivially unit-testable: swap the Provisioner for a stub and
// the whole state machine runs without Forgejo.
//
// State machine (per active agent):
//
//   Load → Validate → GetAccount
//     ├─ exists   → ActionUnchanged (record, skip keygen)
//     └─ 404      → CreateUser → GenerateKeyPair → write files
//                    → RegisterKey → CreateToken → ActionCreated
//
// For offboarded agents:
//
//   Load → RevokeToken (if PAT known) → archive keys → ActionDeprovisioned
//
// Failures at any step are recorded as ActionFailed but do NOT abort the
// rest of the run — the sync is best-effort across agents, and the final
// exit code reflects partial failure (ExitPartialFailure = 4).

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Logger interface (so tests can capture output without a real *log.Logger)
// ---------------------------------------------------------------------------

// Logger is the minimal surface the Syncer needs. *log.Logger satisfies it.
type Logger interface {
	Printf(format string, args ...any)
}

// stdLogger is a default no-op logger used when none is provided.
type stdLogger struct{ l *log.Logger }

func (s *stdLogger) Printf(format string, args ...any) {
	if s.l != nil {
		s.l.Printf(format, args...)
	}
}

// ---------------------------------------------------------------------------
// Syncer
// ---------------------------------------------------------------------------

// Syncer orchestrates a sync run. It is constructed once per CLI invocation
// and is not safe to reuse across overlapping goroutines (the state file is
// not concurrency-safe).
type Syncer struct {
	cfg       ProvisionerConfig
	prov      *Provisioner
	log       Logger
	state     *StateFile
	statePath string
}

// NewSyncer constructs a Syncer. It validates config, builds the Provisioner,
// and loads any existing state file (creating an empty one if absent).
//
// adminUser/adminPassword are NOT taken here — they are passed per-call to
// CreateToken/RevokeToken because they are only needed for those endpoints.
// The CLI threads them through from env vars at call time.
func NewSyncer(cfg ProvisionerConfig, lg Logger) (*Syncer, error) {
	prov, err := NewProvisioner(cfg)
	if err != nil {
		return nil, err
	}
	if lg == nil {
		lg = &stdLogger{l: log.New(os.Stderr, "", log.LstdFlags)}
	}
	statePath := expandHome(cfg.StatePath)
	state, err := loadStateFile(statePath)
	if err != nil {
		return nil, err
	}
	return &Syncer{
		cfg:       cfg,
		prov:      prov,
		log:       lg,
		state:     state,
		statePath: statePath,
	}, nil
}

// Provisioner exposes the underlying provisioner for direct single-agent
// commands (provision/deprovision/keygen) that don't need the full sync flow.
func (s *Syncer) Provisioner() *Provisioner { return s.prov }

// State exposes the loaded state file (read-only snapshot).
func (s *Syncer) State() *StateFile { return s.state }

// ---------------------------------------------------------------------------
// known-friends.json loading
// ---------------------------------------------------------------------------

// LoadKnownFriends reads and validates known-friends.json from the configured
// path. It returns a TypedError of kind config (exit 3) if the file is
// missing or unparseable, and a TypedError of kind config (exit 3) with a
// specific "NO_AGENTS" message if the file is valid but empty — matching
// the §15 contract (empty agents → exit 0 with NO_AGENTS, handled by caller).
func LoadKnownFriends(path string) (*KnownFriends, error) {
	path = expandHome(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, NewConfigError(
				fmt.Sprintf("FILE_NOT_FOUND: %s", path), err)
		}
		return nil, NewConfigError(
			fmt.Sprintf("cannot read %s: %v", path, err), err)
	}
	var kf KnownFriends
	if err := json.Unmarshal(data, &kf); err != nil {
		return nil, NewConfigError(
			fmt.Sprintf("malformed known-friends.json at %s: %v", path, err), err)
	}
	// Backfill agent.Name from map keys so downstream code never sees empty.
	for name, a := range kf.Agents {
		if a == nil {
			continue
		}
		if a.Name == "" {
			a.Name = name
		}
		if err := a.Validate(); err != nil {
			return nil, err
		}
	}
	return &kf, nil
}

// ---------------------------------------------------------------------------
// Sync — the full state machine
// ---------------------------------------------------------------------------

// SyncOptions tunes a single sync run. Currently only carries admin
// credentials (needed by CreateToken/RevokeToken, which use BasicAuth).
type SyncOptions struct {
	AdminUser     string
	AdminPassword string
}

// Sync runs the full provisioning flow over all agents in known-friends.json.
// It returns a slice of ProvisioningResult (one per processed agent) and a
// typed error describing the overall outcome. The CLI maps the error's
// ExitCode() to a process exit.
//
// The function is best-effort: a failure on one agent does NOT abort the
// rest. Failures are recorded as ActionFailed; the returned error (if any)
// is a PartialError iff at least one agent failed AND at least one succeeded.
func (s *Syncer) Sync(kf *KnownFriends, opts SyncOptions) ([]ProvisioningResult, error) {
	start := time.Now()
	if kf == nil {
		return nil, NewConfigError("Sync: nil KnownFriends", nil)
	}

	active := kf.ActiveAgents()
	offboarded := kf.OffboardedAgents()
	results := make([]ProvisioningResult, 0, len(active)+len(offboarded))

	// 1. Provision active agents.
	for _, a := range active {
		s.log.Printf("identity: provisioning agent=%s status=%s tier=%s",
			a.Name, a.Status, a.Tier)
		r := s.provisionAgent(a, opts)
		results = append(results, r)
		if r.Action == ActionFailed {
			s.log.Printf("identity: FAILED agent=%s error=%s duration=%s",
				a.Name, r.Error, r.Duration)
		} else {
			s.log.Printf("identity: OK agent=%s action=%s duration=%s",
				a.Name, r.Action, r.Duration)
		}
	}

	// 2. Deprovision offboarded agents.
	for _, a := range offboarded {
		s.log.Printf("identity: deprovisioning agent=%s status=%s",
			a.Name, a.Status)
		r := s.deprovisionAgent(a, opts)
		results = append(results, r)
		if r.Action == ActionFailed {
			s.log.Printf("identity: FAILED deprovision agent=%s error=%s",
				a.Name, r.Error)
		}
	}

	// 3. Persist state (only if we actually changed something and we're not
	//    in dry-run mode — dry-run must never touch disk).
	if !s.cfg.DryRun {
		s.state.LastSync = time.Now().UTC()
		if err := s.saveState(); err != nil {
			// State save failure is serious but doesn't undo what we did.
			// Surface it as a partial error so the operator notices.
			return results, NewInternalError(
				"state file save failed (provisioning succeeded)", err)
		}
	}

	// 4. Decide the overall error.
	succeeded, failed := 0, 0
	for _, r := range results {
		if r.Succeeded() {
			succeeded++
		} else {
			failed++
		}
	}
	s.log.Printf("identity: sync complete agents=%d succeeded=%d failed=%d duration=%s",
		len(results), succeeded, failed, time.Since(start))

	if failed > 0 && succeeded > 0 {
		return results, NewPartialError(
			fmt.Sprintf("partial sync: %d succeeded, %d failed", succeeded, failed), nil)
	}
	if failed > 0 && succeeded == 0 {
		// All failed — pick the first failure's kind for the exit code.
		var firstErr *TypedError
		for _, r := range results {
			if !r.Succeeded() && r.Error != "" {
				firstErr = NewPartialError(r.Error, nil)
				break
			}
		}
		if firstErr == nil {
			firstErr = NewPartialError("all agents failed", nil)
		}
		return results, firstErr
	}
	return results, nil
}

// provisionAgent runs the per-active-agent state machine. It never returns
// an error directly — failures are captured into the returned result so the
// outer loop can continue with the next agent.
func (s *Syncer) provisionAgent(a *Agent, opts SyncOptions) ProvisioningResult {
	start := time.Now()
	r := ProvisioningResult{AgentName: a.Name, Status: a.Status}

	// Step 1: idempotency probe.
	existing, err := s.prov.GetAccount(a.Name)
	if err != nil {
		r.Action = ActionFailed
		r.Error = err.Error()
		r.Duration = time.Since(start)
		return r
	}
	if existing != nil {
		r.Account = existing
		r.Action = ActionUnchanged
		// Carry forward any previously-recorded state for display.
		if st := s.state.Agents[a.Name]; st != nil {
			r.SSHKeyID = st.SSHKeyID
			r.SSHFingerprint = st.SSHFingerprint
			r.PATID = st.PATID
			r.PATLastEight = st.PATLastEight
		}
		r.Duration = time.Since(start)
		return r
	}

	// Step 2: create the account.
	tempPassword, err := GenerateTempPassword()
	if err != nil {
		r.Action = ActionFailed
		r.Error = err.Error()
		r.Duration = time.Since(start)
		return r
	}
	acc, err := s.prov.CreateUser(NewCreateUserRequest(a, tempPassword))
	if err != nil {
		// 409 Conflict → downgrade to Unchanged (account already exists, e.g.
		// from a previous partial run). The transport maps 409 to a TypedError
		// of kind API; we detect that here.
		if te, ok := err.(*TypedError); ok && te.Kind == ErrKindAPI &&
			strings.Contains(err.Error(), "409") {
			r.Action = ActionUnchanged
			r.Duration = time.Since(start)
			return r
		}
		r.Action = ActionFailed
		r.Error = err.Error()
		r.Duration = time.Since(start)
		return r
	}
	r.Account = acc

	// Step 3: generate keypair + write files (skipped in dry-run).
	kp, err := s.writeKeyFiles(a)
	if err != nil {
		r.Action = ActionFailed
		r.Error = err.Error()
		r.Duration = time.Since(start)
		return r
	}
	r.SSHFingerprint = kp.Fingerprint

	// Step 4: register the public key with Forgejo.
	key, err := s.prov.RegisterKey(a.Name, tempPassword, kp.PublicKeyOpenSSH, a.KeyTitle())
	if err != nil {
		r.Action = ActionFailed
		r.Error = err.Error()
		r.Duration = time.Since(start)
		return r
	}
	if key != nil {
		r.SSHKeyID = key.ID
	}

	// Step 5: mint a PAT. The plaintext token is captured here and then
	// immediately masked — we only persist the last 8 chars.
	tok, err := s.prov.CreateToken(a.Name, opts.AdminUser, opts.AdminPassword,
		NewCreateTokenRequest(a))
	if err != nil {
		r.Action = ActionFailed
		r.Error = err.Error()
		r.Duration = time.Since(start)
		return r
	}
	if tok != nil {
		r.PATID = tok.ID
		if tok.Token != "" {
			r.PATLastEight = MaskToken(tok.Token)
		}
	}

	r.Action = ActionCreated
	r.Duration = time.Since(start)

	// Record state for idempotency (only when not in dry-run).
	if !s.cfg.DryRun {
		s.state.Agents[a.Name] = &AgentState{
			ForgejoAccountID: accountID(acc),
			SSHKeyID:         r.SSHKeyID,
			SSHFingerprint:   r.SSHFingerprint,
			PATLastEight:     r.PATLastEight,
			PATID:            r.PATID,
			LastProvisioned:  time.Now().UTC(),
		}
	}
	return r
}

// deprovisionAgent revokes an offboarded agent's PAT and archives their keys.
// The Forgejo account is preserved (never deleted) so historical git
// attribution remains intact.
func (s *Syncer) deprovisionAgent(a *Agent, opts SyncOptions) ProvisioningResult {
	start := time.Now()
	r := ProvisioningResult{AgentName: a.Name, Status: a.Status}

	st := s.state.Agents[a.Name]
	if st == nil || st.PATID == 0 {
		// Nothing to revoke — agent was never provisioned through us.
		r.Action = ActionSkipped
		r.Duration = time.Since(start)
		return r
	}

	if err := s.prov.RevokeToken(a.Name, opts.AdminUser, opts.AdminPassword, st.PATID); err != nil {
		r.Action = ActionFailed
		r.Error = err.Error()
		r.Duration = time.Since(start)
		return r
	}

	// Archive the key directory (best-effort; missing dir is fine).
	if !s.cfg.DryRun {
		if err := s.archiveKeys(a.Name); err != nil {
			// Non-fatal: PAT is revoked, that's the important part.
			s.log.Printf("identity: WARN agent=%s key archive failed: %v",
				a.Name, err)
		}
		delete(s.state.Agents, a.Name)
	}

	r.Action = ActionDeprovisioned
	r.Duration = time.Since(start)
	return r
}

// accountID safely extracts the ID from a possibly-nil ForgejoAccount.
func accountID(a *ForgejoAccount) int64 {
	if a == nil {
		return 0
	}
	return a.ID
}

// ---------------------------------------------------------------------------
// Single-agent operations (for provision/deprovision/keygen subcommands)
// ---------------------------------------------------------------------------

// ProvisionOne runs the provisioning state machine for a single named agent.
// Returns the result + the overall error (nil on success). Used by the
// `helix identity provision <name>` subcommand.
func (s *Syncer) ProvisionOne(a *Agent, opts SyncOptions) (ProvisioningResult, error) {
	r := s.provisionAgent(a, opts)
	if !r.Succeeded() {
		return r, NewPartialError(
			fmt.Sprintf("provision %s failed: %s", a.Name, r.Error), nil)
	}
	if !s.cfg.DryRun {
		s.state.LastSync = time.Now().UTC()
		if err := s.saveState(); err != nil {
			return r, NewInternalError("state save failed", err)
		}
	}
	return r, nil
}

// DeprovisionOne runs the deprovision path for a single named agent.
func (s *Syncer) DeprovisionOne(a *Agent, opts SyncOptions) (ProvisioningResult, error) {
	r := s.deprovisionAgent(a, opts)
	if !r.Succeeded() {
		return r, NewPartialError(
			fmt.Sprintf("deprovision %s failed: %s", a.Name, r.Error), nil)
	}
	if !s.cfg.DryRun {
		s.state.LastSync = time.Now().UTC()
		if err := s.saveState(); err != nil {
			return r, NewInternalError("state save failed", err)
		}
	}
	return r, nil
}

// KeyGenOnly generates a fresh keypair for an agent and writes the files,
// without touching Forgejo. Useful for rotating a compromised key without
// re-running the whole provisioning flow.
func (s *Syncer) KeyGenOnly(a *Agent) (ProvisioningResult, error) {
	start := time.Now()
	r := ProvisioningResult{AgentName: a.Name, Status: a.Status}
	kp, err := s.writeKeyFiles(a)
	if err != nil {
		r.Action = ActionFailed
		r.Error = err.Error()
		r.Duration = time.Since(start)
		return r, NewInternalError("keygen failed", err)
	}
	r.Action = ActionCreated
	r.SSHFingerprint = kp.Fingerprint
	r.Duration = time.Since(start)
	return r, nil
}

// ---------------------------------------------------------------------------
// Filesystem: key files + state file
// ---------------------------------------------------------------------------

// writeKeyFiles generates a keypair and materializes the three files per §9.3:
//
//	~/.helix/keys/<agent>/id_ed25519       (private key, mode 0600)
//	~/.helix/keys/<agent>/id_ed25519.pub   (OpenSSH public key)
//	~/.helix/keys/<agent>/id_ed25519.state (JSON: key id, fingerprint, created)
//
// In dry-run mode nothing is written; the function still returns the
// generated KeyPair so callers can inspect the would-be fingerprint.
func (s *Syncer) writeKeyFiles(a *Agent) (*KeyPair, error) {
	kp, err := GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	if s.cfg.DryRun {
		return kp, nil
	}

	dir := filepath.Join(expandHome(s.cfg.SSHKeyDir), a.Name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, NewInternalError(
			fmt.Sprintf("mkdir %s failed", dir), err)
	}

	privPath := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(privPath, []byte(kp.PrivateKeyPEM), 0o600); err != nil {
		return nil, NewInternalError("write private key failed", err)
	}
	pubPath := filepath.Join(dir, "id_ed25519.pub")
	if err := os.WriteFile(pubPath, []byte(kp.PublicKeyOpenSSH+"\n"), 0o644); err != nil {
		return nil, NewInternalError("write public key failed", err)
	}
	state := map[string]any{
		"fingerprint": kp.Fingerprint,
		"created_at":  time.Now().UTC().Format(time.RFC3339),
		"comment":     "helix-identity",
	}
	stateBytes, _ := json.MarshalIndent(state, "", "  ")
	statePath := filepath.Join(dir, "id_ed25519.state")
	if err := os.WriteFile(statePath, stateBytes, 0o600); err != nil {
		return nil, NewInternalError("write key state failed", err)
	}
	return kp, nil
}

// archiveKeys moves an agent's key directory into a dated archive subdirectory
// so the material is preserved for forensics but no longer live. The pattern
// matches the §11.1 deprovision flow.
func (s *Syncer) archiveKeys(name string) error {
	src := filepath.Join(expandHome(s.cfg.SSHKeyDir), name)
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to archive
		}
		return err
	}
	stamp := time.Now().UTC().Format("2006-01-02")
	dst := filepath.Join(src, "archive", stamp)
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	// Move each file (not the archive subdir itself) into the dated folder.
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() == "archive" {
			continue
		}
		oldPath := filepath.Join(src, e.Name())
		newPath := filepath.Join(dst, e.Name())
		if err := os.Rename(oldPath, newPath); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// State file load/save
// ---------------------------------------------------------------------------

// loadStateFile reads the state file, returning an empty StateFile if it
// doesn't exist yet (first run). A malformed state file is a hard error.
func loadStateFile(path string) (*StateFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewStateFile(), nil
		}
		// Permission errors etc. are real failures.
		if !errors.Is(err, fs.ErrPermission) {
			return nil, NewInternalError(
				fmt.Sprintf("cannot read state file %s", path), err)
		}
		return NewStateFile(), nil
	}
	var sf StateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, NewInternalError(
			fmt.Sprintf("malformed state file %s: %v", path, err), err)
	}
	if sf.Agents == nil {
		sf.Agents = make(map[string]*AgentState)
	}
	if sf.Version == 0 {
		sf.Version = StateVersion
	}
	return &sf, nil
}

// saveState writes the state file atomically: write to a temp file, fsync,
// rename. This guards against half-written state on crash.
func (s *Syncer) saveState() error {
	if err := os.MkdirAll(filepath.Dir(s.statePath), 0o700); err != nil {
		return fmt.Errorf("mkdir state dir: %w", err)
	}
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	tmp := s.statePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write state tmp: %w", err)
	}
	if err := os.Rename(tmp, s.statePath); err != nil {
		return fmt.Errorf("rename state: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Path helper
// ---------------------------------------------------------------------------

// expandHome replaces a leading "~" with the user's home directory. Returns
// the input unchanged if it doesn't start with "~".
func expandHome(path string) string {
	if path == "" {
		return path
	}
	if path[0] != '~' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if len(path) >= 2 && (path[1] == '/' || path[1] == filepath.Separator) {
		return filepath.Join(home, path[2:])
	}
	return path
}
