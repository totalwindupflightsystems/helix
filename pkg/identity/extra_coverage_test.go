package identity

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

// =====================================================================
// permissions.go — tierRank + Can + ComputeDelta edge cases
// =====================================================================

// TestTierRank_AllKnownTiers verifies that every documented tier maps to a
// strictly increasing rank.
func TestTierRank_AllKnownTiers(t *testing.T) {
	pairs := []struct {
		tier trust.TrustTier
		want int
	}{
		{trust.TierProvisional, 0},
		{trust.TierObserved, 1},
		{trust.TierTrusted, 2},
		{trust.TierVeteran, 3},
	}
	for _, p := range pairs {
		if got := tierRank(p.tier); got != p.want {
			t.Errorf("tierRank(%s) = %d, want %d", p.tier, got, p.want)
		}
	}
}

// TestTierRank_UnknownTier verifies the default-branch fallback to -1.
func TestTierRank_UnknownTier(t *testing.T) {
	if got := tierRank("nonexistent-tier"); got != -1 {
		t.Errorf("tierRank(unknown) = %d, want -1", got)
	}
	if got := tierRank(""); got != -1 {
		t.Errorf("tierRank(\"\") = %d, want -1", got)
	}
}

// TestTierRank_Ordering verifies the strict monotonic order across tiers.
func TestTierRank_Ordering(t *testing.T) {
	if !(tierRank(trust.TierProvisional) < tierRank(trust.TierObserved) &&
		tierRank(trust.TierObserved) < tierRank(trust.TierTrusted) &&
		tierRank(trust.TierTrusted) < tierRank(trust.TierVeteran)) {
		t.Error("tier ranks are not strictly monotonic")
	}
}

// TestCanPerformAction_UnknownActionExtra verifies that an unrecognised action
// returns false (rather than erroring or silently returning true). Extra
// coverage — the canonical test lives in permissions_test.go.
func TestCanPerformAction_UnknownActionExtra(t *testing.T) {
	pe := NewPermissionExpansion()
	ok, err := pe.CanPerformAction(trust.TierVeteran, "interrogate_the_database")
	if err != nil {
		t.Fatalf("CanPerformAction: %v", err)
	}
	if ok {
		t.Error("unknown action should return false (not in permission set)")
	}
}

// TestCanPerformAction_AllKnownActionsExtra verifies each canonical action
// maps to the expected boolean per tier.
func TestCanPerformAction_AllKnownActionsExtra(t *testing.T) {
	pe := NewPermissionExpansion()
	cases := []struct {
		tier   trust.TrustTier
		action string
		want   bool
	}{
		{trust.TierProvisional, "read", true},
		{trust.TierProvisional, "merge", false},
		{trust.TierTrusted, "merge", true},
		{trust.TierVeteran, "delete_repos", true}, // only veteran can delete_repos
		{trust.TierTrusted, "delete_repos", false},
	}
	for _, c := range cases {
		ok, err := pe.CanPerformAction(c.tier, c.action)
		if err != nil {
			t.Errorf("CanPerformAction(%s, %s): %v", c.tier, c.action, err)
			continue
		}
		if ok != c.want {
			t.Errorf("CanPerformAction(%s, %s) = %v, want %v", c.tier, c.action, ok, c.want)
		}
	}
}

// TestCan_AllKnownActions checks the PermissionSet.Can switch branches.
func TestCan_AllKnownActions(t *testing.T) {
	ps := PermissionSet{
		CanReadRepos:      true,
		CanCreateBranches: true,
		CanMergeOwnPRs:    true,
		CanCreateRepos:    true,
		CanDeleteRepos:    true,
		HasAdminAccess:    true,
	}
	// Each canonical alias should resolve to true when its flag is set.
	for _, a := range []string{"read", "branch", "merge"} {
		if !ps.Can(a) {
			t.Errorf("Can(%q) = false, want true", a)
		}
	}
	// Mixed case + whitespace should still match.
	if !ps.Can("READ") {
		t.Error("Can(READ) should be case-insensitive")
	}
	// Unknown action.
	if ps.Can("obliterate") {
		t.Error("Can(unknown) = true, want false")
	}
}

// TestComputeDelta_NoChange verifies the empty-delta path.
func TestComputeDelta_NoChange(t *testing.T) {
	pe := NewPermissionExpansion()
	transition := TierTransition{
		AgentID: "test",
		OldTier: trust.TierTrusted,
		NewTier: trust.TierTrusted,
	}
	delta, err := pe.ComputeDelta(transition)
	if err != nil {
		t.Fatalf("ComputeDelta: %v", err)
	}
	if len(delta.Granted) != 0 || len(delta.Revoked) != 0 {
		t.Errorf("no-change delta = %+v, want empty granted/revoked", delta)
	}
}

// TestComputeDelta_GrantAndRevoke verifies both directions of the delta.
func TestComputeDelta_GrantAndRevoke(t *testing.T) {
	pe := NewPermissionExpansion()

	// trusted → veteran: veteran has strictly more permissions, so some
	// fields should be Granted and none Revoked.
	upward := TierTransition{
		AgentID: "test", OldTier: trust.TierTrusted, NewTier: trust.TierVeteran,
	}
	delta, err := pe.ComputeDelta(upward)
	if err != nil {
		t.Fatalf("ComputeDelta upward: %v", err)
	}
	if len(delta.Granted) == 0 {
		t.Error("expected at least one Granted permission (trusted→veteran)")
	}

	// veteran → trusted: some fields Revoked.
	downward := TierTransition{
		AgentID: "test", OldTier: trust.TierVeteran, NewTier: trust.TierTrusted,
	}
	delta2, err := pe.ComputeDelta(downward)
	if err != nil {
		t.Fatalf("ComputeDelta downward: %v", err)
	}
	if len(delta2.Revoked) == 0 {
		t.Error("expected at least one Revoked permission (veteran→trusted)")
	}
}

// TestHandleTransition_TierEscalation verifies the promotion branch.
func TestHandleTransition_TierEscalation(t *testing.T) {
	pe := NewPermissionExpansion()
	transition := TierTransition{
		AgentID: "promote-me",
		OldTier: trust.TierObserved,
		NewTier: trust.TierTrusted,
	}
	_, err := pe.HandleTransition(transition)
	if err != nil {
		t.Fatalf("HandleTransition: %v", err)
	}
	if transition.OldTier != trust.TierObserved {
		t.Errorf("OldTier = %s, want Observed", transition.OldTier)
	}
	if transition.NewTier != trust.TierTrusted {
		t.Errorf("NewTier = %s, want Trusted", transition.NewTier)
	}
	if !transition.IsPromotion() {
		t.Error("Observed→Trusted should be a promotion")
	}
	if transition.IsDemotion() {
		t.Error("Observed→Trusted should NOT be a demotion")
	}
}

// TestHandleTransition_TierDemotion verifies the demotion branch.
func TestHandleTransition_TierDemotion(t *testing.T) {
	pe := NewPermissionExpansion()
	transition := TierTransition{
		AgentID: "demote-me",
		OldTier: trust.TierVeteran,
		NewTier: trust.TierTrusted,
	}
	_, err := pe.HandleTransition(transition)
	if err != nil {
		t.Fatalf("HandleTransition: %v", err)
	}
	if !transition.IsDemotion() {
		t.Error("Veteran→Trusted should be a demotion")
	}
	if transition.IsPromotion() {
		t.Error("Veteran→Trusted should NOT be a promotion")
	}
}

// =====================================================================
// syncer.go — KeyGenOnly output-write-error
// =====================================================================

// TestKeyGenOnly_OutputWriteFails verifies the KeyGenOnly branch where
// writeKeyFiles fails (e.g., the SSHKeyDir is read-only).
func TestKeyGenOnly_OutputWriteFails(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root — chmod restrictions don't apply")
	}
	cfg := validDryRunConfig(t)
	cfg.DryRun = false // force real-mode writeKeyFiles path
	readOnlyDir := t.TempDir()
	if err := os.Chmod(readOnlyDir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(readOnlyDir, 0o755) }()
	cfg.SSHKeyDir = filepath.Join(readOnlyDir, "subdir", "keys")

	s, err := NewSyncer(cfg, nil)
	if err != nil {
		t.Fatalf("NewSyncer: %v", err)
	}
	a := &Agent{Name: "agent-x", Tier: TierPro}
	r, err := s.KeyGenOnly(a)
	if err == nil {
		t.Fatal("KeyGenOnly on read-only dir = nil, want error")
	}
	if r.Action != ActionFailed {
		t.Errorf("Action = %s, want ActionFailed", r.Action)
	}
	var te *TypedError
	if !errorIs(err, &te) {
		t.Errorf("err = %T, want *TypedError", err)
	} else if te.Kind != ErrKindInternal {
		t.Errorf("Kind = %s, want ErrKindInternal", te.Kind)
	}
}

// TestKeyGenOnly_Success verifies the happy path populates fingerprint.
func TestKeyGenOnly_Success(t *testing.T) {
	cfg := validDryRunConfig(t)
	cfg.DryRun = false // writes real key files
	s, err := NewSyncer(cfg, nil)
	if err != nil {
		t.Fatalf("NewSyncer: %v", err)
	}
	a := &Agent{Name: "agent-x", Tier: TierPro}
	r, err := s.KeyGenOnly(a)
	if err != nil {
		t.Fatalf("KeyGenOnly: %v", err)
	}
	if r.Action != ActionCreated {
		t.Errorf("Action = %s, want ActionCreated", r.Action)
	}
	if r.SSHFingerprint == "" {
		t.Error("SSHFingerprint should be populated on success")
	}
}

// =====================================================================
// key_rotation.go — MarkRotated + HasImmediate/HasHigh edge cases
// =====================================================================

// TestMarkRotated_NotPending verifies the no-op branch for an event that's
// not in the pending map.
func TestMarkRotated_NotPending(t *testing.T) {
	registry := NewAgentKeyRegistry()
	now := time.Now()
	// Marking something that's not pending is a no-op (does not panic,
	// does not error).
	registry.MarkRotated("nonexistent-agent", KeyTypeSSH, "nonexistent-hash", now)
	// A fresh registry's plan has no immediate or high urgency events.
	plan := registry.PlanRotation(DefaultRotationPolicies(), now)
	if plan == nil {
		t.Fatal("PlanRotation = nil")
	}
	if plan.HasImmediate() {
		t.Error("HasImmediate should be false on empty registry")
	}
	if plan.HasHigh() {
		t.Error("HasHigh should be false on empty registry")
	}
}

// TestPlanRotation_NoEvents verifies the empty-events branch.
func TestPlanRotation_NoEvents(t *testing.T) {
	registry := NewAgentKeyRegistry()
	plan := registry.PlanRotation(DefaultRotationPolicies(), time.Now())
	if plan == nil {
		t.Fatal("PlanRotation = nil")
	}
	// Plan.Steps doesn't exist on *RotationPlan; the empty plan still
	// returns a usable *RotationPlan with no immediate/high items.
	if plan.HasImmediate() || plan.HasHigh() {
		t.Errorf("empty plan should have no immediate/high: %+v", plan)
	}
}

// errorIs is a small helper to avoid pulling errors.As into every test.
// It mirrors errors.As for the *TypedError target.
func errorIs(err error, target **TypedError) bool {
	if err == nil {
		return false
	}
	if t, ok := err.(*TypedError); ok {
		*target = t
		return true
	}
	return false
}

// =====================================================================
// syncer.go — Sync + provisionAgent + ProvisionOne + DeprovisionOne
// =====================================================================
//
// The following tests use httptest to drive the Provisioner's HTTP transport
// into specific success/failure branches. Tests that need a *Syncer pointed
// at a fake Forgejo build one via newHttptestSyncer(t, handler).

// newHttptestSyncer builds a non-dry-run Syncer whose Provisioner talks to
// the supplied httptest server. Returns the syncer + server for inspection.
func newHttptestSyncer(t *testing.T, handler http.Handler) (*Syncer, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := DefaultProvisionerConfig()
	cfg.ForgejoURL = srv.URL
	cfg.AdminUser = "helio"
	cfg.AdminPassword = "helio123"
	cfg.AdminToken = "tok"
	cfg.KnownFriendsPath = filepath.Join(t.TempDir(), "known-friends.json")
	cfg.SSHKeyDir = filepath.Join(t.TempDir(), "keys")
	cfg.StatePath = filepath.Join(t.TempDir(), "state.json")
	cfg.DryRun = false // real-mode so we exercise the per-method HTTP paths
	cfg.RequestRate = 50
	cfg.BurstRate = 50
	cfg.HTTPTimeout = 3 * time.Second

	s, err := NewSyncer(cfg, nil)
	if err != nil {
		t.Fatalf("NewSyncer: %v", err)
	}
	// Override default retry policy so a single HTTP failure surfaces
	// immediately rather than retrying 4 times (3s backoff each).
	s.prov.retry = RetryPolicy{
		MaxAttempts:    1,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
		Multiplier:     2.0,
	}
	return s, srv
}

// provisionOneAgent is a helper that creates a single-agent KnownFriends
// file and returns the loaded struct.
func provisionOneAgent(t *testing.T, name string, tier AgentTier) (*KnownFriends, *Agent) {
	t.Helper()
	kf := &KnownFriends{
		Version: 1,
		Agents: map[string]*Agent{
			name: {
				Name:        name,
				DisplayName: name,
				Status:      StatusActive,
				Tier:        tier,
			},
		},
	}
	return kf, kf.Agents[name]
}

// TestSync_PerAgentFailure_Branch covers the provisionAgent per-agent error
// path inside Sync (GetAccount fails for the only active agent).
func TestSync_PerAgentFailure_Branch(t *testing.T) {
	// Handler always returns 500 — every GetAccount call fails inside
	// the active-agent loop, so Sync must surface a partial-error result
	// with Action=ActionFailed on the result and a Partial TypedError.
	s, _ := newHttptestSyncer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"server error"}`))
	}))
	kf, _ := provisionOneAgent(t, "alice", TierPro)

	results, runErr := s.Sync(kf, SyncOptions{AdminUser: "helio", AdminPassword: "helio123"})
	if runErr == nil {
		t.Fatal("expected PartialError from Sync (per-agent failure)")
	}
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	r := results[0]
	if r.Action != ActionFailed {
		t.Errorf("Action = %q, want %q", r.Action, ActionFailed)
	}
	if r.Error == "" {
		t.Error("Error should be non-empty on failed result")
	}
	var te *TypedError
	if !errors.As(runErr, &te) {
		t.Errorf("runErr = %T, want *TypedError", runErr)
	} else if te.Kind != ErrKindPartial {
		t.Errorf("Kind = %q, want %q", te.Kind, ErrKindPartial)
	}
}

// TestProvisionAgent_ExistingAccount covers the "existing != nil" branch in
// provisionAgent (GetAccount returns the agent, so we skip CreateUser).
// The idempotency probe now also verifies the SSH key + PAT server-side
// (DF-011): with both present the result must be ActionUnchanged + the
// existing account attached + prior state copied forward.
func TestProvisionAgent_ExistingAccount(t *testing.T) {
	// Track that we did NOT call CreateUser (only the probe GETs).
	var createCalls int
	var keysListed, tokensListed bool

	existing := ForgejoAccount{
		ID:        42,
		Login:     "bob",
		LoginName: "bob",
		Email:     "bob@example.com",
	}
	existingJSON, _ := json.Marshal([]ForgejoAccount{existing})
	keysJSON, _ := json.Marshal([]SSHKey{{
		ID: 77, Key: "ssh-ed25519 AAAA", Title: "bob-key",
		Fingerprint: "SHA256:present", Created: "2026-06-01T00:00:00Z",
	}})
	tokensJSON, _ := json.Marshal([]AccessToken{{
		ID: 88, Name: PATName, Scopes: []string{"read:repository"},
	}})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/users":
			// GetAccount — admin user list.
			_, _ = w.Write(existingJSON)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/users/bob/keys":
			keysListed = true
			_, _ = w.Write(keysJSON)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/users/bob/tokens":
			tokensListed = true
			_, _ = w.Write(tokensJSON)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/users":
			createCalls++
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	s, _ := newHttptestSyncer(t, handler)

	// Pre-populate state so the carry-forward branch is exercised.
	prior := NewStateFile()
	prior.Agents["bob"] = &AgentState{
		ForgejoAccountID: 42,
		SSHKeyID:         77,
		SSHFingerprint:   "SHA256:prior",
		PATLastEight:     "****priorPAT",
		PATID:            88,
		LastProvisioned:  time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}
	writeStateFile(t, s.statePath, prior)

	// Re-load state into the syncer (NewSyncer already did this, but
	// re-load to ensure prior state is current).
	s.state.Agents["bob"] = prior.Agents["bob"]

	a := &Agent{Name: "bob", Status: StatusActive, Tier: TierPro}
	r, err := s.ProvisionOne(a, SyncOptions{AdminUser: "helio", AdminPassword: "helio123"})
	if err != nil {
		t.Fatalf("ProvisionOne: %v", err)
	}
	if r.Action != ActionUnchanged {
		t.Errorf("Action = %q, want %q", r.Action, ActionUnchanged)
	}
	if r.Account == nil || r.Account.ID != 42 {
		t.Errorf("Account = %+v, want ID=42", r.Account)
	}
	if r.SSHKeyID != 77 || r.SSHFingerprint != "SHA256:prior" {
		t.Errorf("carry-forward state lost: SSHKeyID=%d fingerprint=%q", r.SSHKeyID, r.SSHFingerprint)
	}
	if !keysListed {
		t.Error("ListUserKeys was not called (probe must verify the SSH key server-side)")
	}
	if !tokensListed {
		t.Error("ListUserTokens was not called (probe must verify the PAT server-side)")
	}
	if createCalls != 0 {
		t.Errorf("CreateUser was called %d times, want 0 (account already exists)", createCalls)
	}
}

// TestProvisionAgent_RepairMissingPAT covers the DF-011 repair path: the
// user + SSH key exist server-side but the PAT is missing (a previous run
// died between RegisterKey and CreateToken, or the PAT was revoked
// manually). The re-run must mint a fresh PAT, report ActionUpdated, and
// overwrite the stale state (pat_id reconciliation).
func TestProvisionAgent_RepairMissingPAT(t *testing.T) {
	var createTokenCalls int
	existing := ForgejoAccount{ID: 42, Login: "carol", LoginName: "carol", Email: "carol@example.com"}
	existingJSON, _ := json.Marshal([]ForgejoAccount{existing})
	keysJSON, _ := json.Marshal([]SSHKey{{
		ID: 77, Key: "ssh-ed25519 AAAA", Title: "carol-key",
		Fingerprint: "SHA256:carol", Created: "2026-06-01T00:00:00Z",
	}})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/users":
			_, _ = w.Write(existingJSON)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/users/carol/keys":
			_, _ = w.Write(keysJSON)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/users/carol/tokens":
			// No PAT server-side.
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/users/carol/tokens":
			createTokenCalls++
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"id":909,"name":"%s","scopes":["read:repository"],"token":"plaintext-token-abcdef"}`,
				PATName)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	s, _ := newHttptestSyncer(t, handler)

	// State records a pat_id that no longer exists server-side.
	s.state.Agents["carol"] = &AgentState{
		ForgejoAccountID: 42,
		SSHKeyID:         77,
		SSHFingerprint:   "SHA256:carol",
		PATLastEight:     "****stalePAT",
		PATID:            123, // stale — server has no such token
		LastProvisioned:  time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}

	a := &Agent{Name: "carol", Status: StatusActive, Tier: TierPro}
	r, err := s.ProvisionOne(a, SyncOptions{AdminUser: "helio", AdminPassword: "helio123"})
	if err != nil {
		t.Fatalf("ProvisionOne: %v", err)
	}
	if r.Action != ActionUpdated {
		t.Errorf("Action = %q, want %q (missing PAT must be repaired)", r.Action, ActionUpdated)
	}
	if createTokenCalls != 1 {
		t.Errorf("CreateToken called %d times, want 1", createTokenCalls)
	}
	if r.PATID != 909 {
		t.Errorf("PATID = %d, want 909 (fresh PAT must be recorded)", r.PATID)
	}
	if r.PATLastEight == "" {
		t.Error("PATLastEight should be set from the fresh token")
	}
	// State must be reconciled: stale pat_id replaced.
	st := s.state.Agents["carol"]
	if st == nil {
		t.Fatal("state entry for carol missing after repair")
	}
	if st.PATID != 909 {
		t.Errorf("state PATID = %d, want 909 (stale pat_id must be reconciled)", st.PATID)
	}
	if st.SSHKeyID != 77 {
		t.Errorf("state SSHKeyID = %d, want 77 (existing key preserved)", st.SSHKeyID)
	}
}

// TestProvisionAgent_RepairMissingKey covers the DF-011 repair path for a
// missing SSH key: user + PAT exist server-side but no key is registered.
// The re-run must generate a keypair, register it, and report ActionUpdated.
func TestProvisionAgent_RepairMissingKey(t *testing.T) {
	var registerKeyCalls int
	existing := ForgejoAccount{ID: 55, Login: "dave", LoginName: "dave", Email: "dave@example.com"}
	existingJSON, _ := json.Marshal([]ForgejoAccount{existing})
	tokensJSON, _ := json.Marshal([]AccessToken{{
		ID: 66, Name: PATName, Scopes: []string{"read:repository"},
	}})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/users":
			_, _ = w.Write(existingJSON)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/users/dave/keys":
			// No SSH key server-side.
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/users/dave/tokens":
			_, _ = w.Write(tokensJSON)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/users/dave/keys":
			registerKeyCalls++
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"id":313,"key":"ssh-ed25519 AAAA","title":"dave-key","fingerprint":"SHA256:dave"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	s, _ := newHttptestSyncer(t, handler)

	a := &Agent{Name: "dave", Status: StatusActive, Tier: TierPro}
	r, err := s.ProvisionOne(a, SyncOptions{AdminUser: "helio", AdminPassword: "helio123"})
	if err != nil {
		t.Fatalf("ProvisionOne: %v", err)
	}
	if r.Action != ActionUpdated {
		t.Errorf("Action = %q, want %q (missing SSH key must be repaired)", r.Action, ActionUpdated)
	}
	if registerKeyCalls != 1 {
		t.Errorf("RegisterKey called %d times, want 1", registerKeyCalls)
	}
	if r.SSHKeyID != 313 {
		t.Errorf("SSHKeyID = %d, want 313", r.SSHKeyID)
	}
	if r.SSHFingerprint == "" {
		t.Error("SSHFingerprint should be set after key repair")
	}
	// The local keypair must have been written so the agent can SSH-auth.
	privPath := filepath.Join(s.cfg.SSHKeyDir, "dave", "id_ed25519")
	if _, err := os.Stat(privPath); err != nil {
		t.Errorf("private key not written during repair: %v", err)
	}
	// PAT must be carried forward untouched.
	if r.PATID != 66 {
		t.Errorf("PATID = %d, want 66 (existing PAT preserved)", r.PATID)
	}
}

// TestProvisionAgent_RepairProbeFailsWithoutBasicAuth covers the DF-011
// probe requiring admin BasicAuth: when the PAT cannot be verified (missing
// credentials), the run must fail loudly instead of reporting unchanged.
func TestProvisionAgent_RepairProbeFailsWithoutBasicAuth(t *testing.T) {
	existing := ForgejoAccount{ID: 42, Login: "erin", LoginName: "erin", Email: "erin@example.com"}
	existingJSON, _ := json.Marshal([]ForgejoAccount{existing})
	keysJSON, _ := json.Marshal([]SSHKey{{
		ID: 77, Key: "ssh-ed25519 AAAA", Title: "erin-key",
		Fingerprint: "SHA256:erin", Created: "2026-06-01T00:00:00Z",
	}})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/users":
			_, _ = w.Write(existingJSON)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/users/erin/keys":
			_, _ = w.Write(keysJSON)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	s, _ := newHttptestSyncer(t, handler)

	a := &Agent{Name: "erin", Status: StatusActive, Tier: TierPro}
	// No admin BasicAuth creds — the PAT probe cannot run.
	r, err := s.ProvisionOne(a, SyncOptions{})
	if err == nil {
		t.Fatal("expected error from ProvisionOne (PAT probe without BasicAuth)")
	}
	if r.Action != ActionFailed {
		t.Errorf("Action = %q, want %q", r.Action, ActionFailed)
	}
	if !strings.Contains(r.Error, "BasicAuth") {
		t.Errorf("Error = %q, want mention of BasicAuth", r.Error)
	}
}

// TestProvisionAgent_CreateUserConflict covers the 409 Conflict branch in
// provisionAgent (CreateUser returns 409, downgrade to ActionUnchanged).
func TestProvisionAgent_CreateUserConflict(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/users"):
			// GetAccount returns empty list — agent does not exist yet.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/users"):
			// CreateUser returns 409 — agent already exists from prior run.
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"user already exists"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	s, _ := newHttptestSyncer(t, handler)

	a := &Agent{Name: "carol", Status: StatusActive, Tier: TierPro}
	r, err := s.ProvisionOne(a, SyncOptions{AdminUser: "helio", AdminPassword: "helio123"})
	if err != nil {
		t.Fatalf("ProvisionOne: %v", err)
	}
	if r.Action != ActionUnchanged {
		t.Errorf("Action = %q, want %q (409 → unchanged downgrade)", r.Action, ActionUnchanged)
	}
}

// TestProvisionAgent_RegisterKeyFailure covers the RegisterKey-error branch
// in provisionAgent (GetAccount=empty, CreateUser=OK, writeKeyFiles=OK,
// RegisterKey=500). RegisterKey hits POST /api/v1/admin/users/{name}/keys.
func TestProvisionAgent_RegisterKeyFailure(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/users"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/admin/users/dave/keys"):
			// RegisterKey fails
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"key registration failed"}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/admin/users"):
			// CreateUser OK
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"id":101,"login":"dave","login_name":"dave","email":"d@example.com"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	s, _ := newHttptestSyncer(t, handler)

	a := &Agent{Name: "dave", Status: StatusActive, Tier: TierPro}
	r, err := s.ProvisionOne(a, SyncOptions{AdminUser: "helio", AdminPassword: "helio123"})
	if err == nil {
		t.Fatal("expected error from ProvisionOne (RegisterKey 500)")
	}
	if r.Action != ActionFailed {
		t.Errorf("Action = %q, want %q", r.Action, ActionFailed)
	}
	if !strings.Contains(r.Error, "key") && !strings.Contains(r.Error, "500") {
		t.Errorf("Error = %q, want substring 'key' or '500'", r.Error)
	}
}

// TestProvisionAgent_CreateTokenFailure covers the CreateToken-error branch
// in provisionAgent (GetAccount=empty, CreateUser=OK, writeKeyFiles=OK,
// RegisterKey=OK, CreateToken=500).
func TestProvisionAgent_CreateTokenFailure(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/users"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/users/eve/tokens"):
			// CreateToken fails
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"token creation failed"}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/admin/users"):
			// CreateUser OK
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"id":202,"login":"eve","login_name":"eve","email":"e@example.com"}`)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/admin/users/eve/keys"):
			// RegisterKey OK
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"id":303,"key":"ssh-rsa AAAA...","title":"e-key"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	s, _ := newHttptestSyncer(t, handler)

	a := &Agent{Name: "eve", Status: StatusActive, Tier: TierPro}
	r, err := s.ProvisionOne(a, SyncOptions{AdminUser: "helio", AdminPassword: "helio123"})
	if err == nil {
		t.Fatal("expected error from ProvisionOne (CreateToken 500)")
	}
	if r.Action != ActionFailed {
		t.Errorf("Action = %q, want %q", r.Action, ActionFailed)
	}
	if !strings.Contains(r.Error, "token") && !strings.Contains(r.Error, "500") {
		t.Errorf("Error = %q, want substring 'token' or '500'", r.Error)
	}
}

// TestDeprovisionOne_NotInState covers the "no prior state" branch in
// deprovisionAgent (skipped, no error).
func TestDeprovisionOne_NotInState(t *testing.T) {
	s, _ := newHttptestSyncer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	a := &Agent{Name: "ghost", Status: StatusOffboarded, Tier: TierFlash}
	r, err := s.DeprovisionOne(a, SyncOptions{AdminUser: "helio", AdminPassword: "helio123"})
	if err != nil {
		t.Fatalf("DeprovisionOne: %v", err)
	}
	if r.Action != ActionSkipped {
		t.Errorf("Action = %q, want %q", r.Action, ActionSkipped)
	}
	// Per ProvisioningResult.Succeeded() definition: skipped is not a
	// failure, so it counts as "succeeded". The important assertion is
	// the Action above — verifying the no-prior-state branch was taken
	// rather than a real revoke call.
	if r.Action == ActionFailed {
		t.Error("Action should not be ActionFailed on a skipped deprovision")
	}
}

// TestDeprovisionOne_RevokeFailure covers the RevokeToken-error branch in
// deprovisionAgent (state has PAT, RevokeToken fails).
func TestDeprovisionOne_RevokeFailure(t *testing.T) {
	var revokeCalls int
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/tokens"):
			revokeCalls++
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"revoke failed"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	s, _ := newHttptestSyncer(t, handler)

	// Pre-populate state with a PAT for "frank".
	s.state.Agents["frank"] = &AgentState{
		ForgejoAccountID: 1,
		SSHKeyID:         2,
		SSHFingerprint:   "SHA256:frank",
		PATLastEight:     "****frankPAT",
		PATID:            404,
		LastProvisioned:  time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}

	a := &Agent{Name: "frank", Status: StatusOffboarded, Tier: TierFlash}
	r, err := s.DeprovisionOne(a, SyncOptions{AdminUser: "helio", AdminPassword: "helio123"})
	if err == nil {
		t.Fatal("expected error from DeprovisionOne (RevokeToken 500)")
	}
	if r.Action != ActionFailed {
		t.Errorf("Action = %q, want %q", r.Action, ActionFailed)
	}
	if revokeCalls == 0 {
		t.Error("RevokeToken endpoint was not called")
	}
}

// TestDeprovisionOne_RevokeSuccess covers the happy path: state has PAT,
// RevokeToken returns 204, result is ActionDeprovisioned + state cleared.
func TestDeprovisionOne_RevokeSuccess(t *testing.T) {
	var revokeCalls int
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/tokens"):
			revokeCalls++
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	s, _ := newHttptestSyncer(t, handler)

	s.state.Agents["grace"] = &AgentState{
		ForgejoAccountID: 1,
		SSHKeyID:         2,
		SSHFingerprint:   "SHA256:grace",
		PATLastEight:     "****gracePAT",
		PATID:            505,
		LastProvisioned:  time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}

	a := &Agent{Name: "grace", Status: StatusOffboarded, Tier: TierFlash}
	r, err := s.DeprovisionOne(a, SyncOptions{AdminUser: "helio", AdminPassword: "helio123"})
	if err != nil {
		t.Fatalf("DeprovisionOne: %v", err)
	}
	if r.Action != ActionDeprovisioned {
		t.Errorf("Action = %q, want %q", r.Action, ActionDeprovisioned)
	}
	if revokeCalls != 1 {
		t.Errorf("RevokeToken called %d times, want 1", revokeCalls)
	}
	// State should be cleared for "grace".
	if _, exists := s.state.Agents["grace"]; exists {
		t.Error("state.Agents[grace] should be deleted after deprovision")
	}
}

// TestSync_SaveStateFailure covers the state-save-failure branch in Sync
// (the inner saveState error after a successful agent provision). Hard to
// trigger without making the state directory read-only post-creation, so we
// use a path under a directory we can flip to read-only after NewSyncer
// loads the state.
func TestSync_SaveStateFailure(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root — chmod restrictions don't apply")
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/users"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/admin/users/henry/keys"):
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"id":707,"key":"ssh-rsa AAAA...","title":"h-key"}`)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/users/henry/tokens"):
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"id":808,"name":"henry","sha1":"abc","token":"plaintext-token-12345678"}`)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/admin/users"):
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"id":606,"login":"henry","login_name":"henry","email":"h@example.com"}`)
		default:
			w.WriteHeader(http.StatusOK)
		}
	})

	// Build a syncer whose StatePath lives inside a directory we'll
	// later chmod 0o555 so saveState fails on write.
	parent := t.TempDir()
	statePath := filepath.Join(parent, "state.json")

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := DefaultProvisionerConfig()
	cfg.ForgejoURL = srv.URL
	cfg.AdminUser = "helio"
	cfg.AdminPassword = "helio123"
	cfg.AdminToken = "tok"
	cfg.KnownFriendsPath = filepath.Join(t.TempDir(), "known-friends.json")
	cfg.SSHKeyDir = filepath.Join(t.TempDir(), "keys")
	cfg.StatePath = statePath
	cfg.DryRun = false
	cfg.RequestRate = 50
	cfg.BurstRate = 50
	cfg.HTTPTimeout = 3 * time.Second

	s, err := NewSyncer(cfg, nil)
	if err != nil {
		t.Fatalf("NewSyncer: %v", err)
	}
	s.prov.retry = RetryPolicy{
		MaxAttempts:    1,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
		Multiplier:     2.0,
	}

	// Now flip the parent dir to read-only so the state save fails.
	if err := os.Chmod(parent, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	kf := &KnownFriends{
		Version: 1,
		Agents: map[string]*Agent{
			"henry": {Name: "henry", Status: StatusActive, Tier: TierPro},
		},
	}

	results, runErr := s.Sync(kf, SyncOptions{AdminUser: "helio", AdminPassword: "helio123"})
	// Even though provisioning succeeded, saveState failure surfaces as
	// a typed Internal error.
	if runErr == nil {
		t.Fatal("expected error from Sync (saveState failed)")
	}
	var te *TypedError
	if !errors.As(runErr, &te) {
		t.Errorf("runErr = %T, want *TypedError", runErr)
	} else if te.Kind != ErrKindInternal {
		t.Errorf("Kind = %q, want %q", te.Kind, ErrKindInternal)
	}
	// The provisioning result must still be present and ActionCreated.
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].Action != ActionCreated {
		t.Errorf("Action = %q, want %q (provisioning succeeded before save failed)",
			results[0].Action, ActionCreated)
	}
}

// TestSync_AllFail_AllSucceededZero covers the Sync branch where all agents
// failed AND succeeded count is zero (all-failed case picks the first
// failure's error).
func TestSync_AllFail_NoSuccess(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"server error"}`))
	})
	s, _ := newHttptestSyncer(t, handler)

	kf := &KnownFriends{
		Version: 1,
		Agents: map[string]*Agent{
			"fail1": {Name: "fail1", Status: StatusActive, Tier: TierPro},
			"fail2": {Name: "fail2", Status: StatusActive, Tier: TierFlash},
		},
	}

	results, runErr := s.Sync(kf, SyncOptions{AdminUser: "helio", AdminPassword: "helio123"})
	if runErr == nil {
		t.Fatal("expected error from Sync (all-failed)")
	}
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2", len(results))
	}
	for _, r := range results {
		if r.Action != ActionFailed {
			t.Errorf("agent %q action = %q, want %q", r.AgentName, r.Action, ActionFailed)
		}
	}
	var te *TypedError
	if !errors.As(runErr, &te) {
		t.Errorf("runErr = %T, want *TypedError", runErr)
	} else if te.Kind != ErrKindPartial {
		t.Errorf("Kind = %q, want %q", te.Kind, ErrKindPartial)
	}
}

// TestSync_AllPass covers the all-success Sync branch (no error returned,
// LastSync timestamp updated on disk).
func TestSync_AllPass(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/users"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/admin/users/ivy/keys"):
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"id":909,"key":"ssh-rsa AAAA...","title":"i-key"}`)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/users/ivy/tokens"):
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"id":111,"name":"ivy","sha1":"abc","token":"plaintext-token-12345678"}`)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/admin/users"):
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"id":808,"login":"ivy","login_name":"ivy","email":"i@example.com"}`)
		default:
			w.WriteHeader(http.StatusOK)
		}
	})
	s, _ := newHttptestSyncer(t, handler)

	kf := &KnownFriends{
		Version: 1,
		Agents: map[string]*Agent{
			"ivy": {Name: "ivy", Status: StatusActive, Tier: TierPro},
		},
	}

	results, runErr := s.Sync(kf, SyncOptions{AdminUser: "helio", AdminPassword: "helio123"})
	if runErr != nil {
		t.Fatalf("Sync: %v", runErr)
	}
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].Action != ActionCreated {
		t.Errorf("Action = %q, want %q", results[0].Action, ActionCreated)
	}
	// State should now contain ivy with PAT.
	if _, exists := s.state.Agents["ivy"]; !exists {
		t.Error("state.Agents[ivy] should be present after successful provisioning")
	}
	// State file should be on disk.
	if _, err := os.Stat(s.statePath); err != nil {
		t.Errorf("state file should be written to disk: %v", err)
	}
}
