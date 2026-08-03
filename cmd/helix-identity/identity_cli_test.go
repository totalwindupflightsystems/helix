package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/helix/pkg/identity"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// makeHID runs runCreate in a temp dir and returns the HID and key paths.
func makeHID(t *testing.T, name string) (hidPath, keyPath string) {
	t.Helper()
	dir := t.TempDir()
	hidPath = filepath.Join(dir, name+".hid")
	keyPath = hidPath + ".key"
	if err := runCreate(createFlags{name: name, output: hidPath}); err != nil {
		t.Fatalf("runCreate(%q): %v", name, err)
	}
	if _, err := os.Stat(hidPath); err != nil {
		t.Fatalf("HID file %s not created: %v", hidPath, err)
	}
	return hidPath, keyPath
}

// tamperAgentID corrupts the signed payload (agent_id) so verification must
// fail, while keeping the file valid JSON.
func tamperAgentID(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(data), `"agent_id": "`, `"agent_id": "t`, 1)
	if tampered == string(data) {
		t.Fatal("tamper: agent_id field not found in HID file")
	}
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
}

// withForgejoCreds sets rootFlags admin creds for register/list tests and
// restores the prior state.
func withForgejoCreds(t *testing.T, user, pass string) {
	t.Helper()
	orig := *rootFlags
	t.Cleanup(func() { *rootFlags = orig })
	rootFlags.adminUser = user
	rootFlags.adminPassword = pass
}

// mockForgejo serves the OAuth2 applications endpoints used by register/list.
type mockForgejo struct {
	server *httptest.Server
	apps   []identity.ForgejoOAuthApp
	posted []identity.CreateOAuthAppRequest
}

func newMockForgejo(apps []identity.ForgejoOAuthApp) *mockForgejo {
	mf := &mockForgejo{apps: apps}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/user/applications/oauth2", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(mf.apps)
		case http.MethodPost:
			var req identity.CreateOAuthAppRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mf.posted = append(mf.posted, req)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(identity.ForgejoOAuthApp{
				ID:           int64(len(mf.posted)),
				Name:         req.Name,
				ClientID:     fmt.Sprintf("client-id-%d", len(mf.posted)),
				ClientSecret: "secret-shown-once",
				RedirectURIs: req.RedirectURIs,
				Confidential: req.Confidential,
				Created:      "2026-08-03T00:00:00Z",
			})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mf.server = httptest.NewServer(mux)
	return mf
}

func (mf *mockForgejo) close() { mf.server.Close() }

// ---------------------------------------------------------------------------
// create
// ---------------------------------------------------------------------------

func TestRunCreate_WritesValidHID(t *testing.T) {
	dir := t.TempDir()
	hidPath := filepath.Join(dir, "builder-01.hid")
	if err := runCreate(createFlags{name: "builder-01", output: hidPath}); err != nil {
		t.Fatalf("runCreate: %v", err)
	}
	// Round-trip through ImportHID.
	agent, err := identity.ImportHID(hidPath)
	if err != nil {
		t.Fatalf("ImportHID round-trip: %v", err)
	}
	if agent.ID == "" {
		t.Error("imported identity has empty agent_id")
	}
	if len(agent.Fingerprint()) != 64 {
		t.Errorf("fingerprint = %q, want 64 hex chars", agent.Fingerprint())
	}
	// The companion private key must exist with mode 0600.
	keyPath := hidPath + ".key"
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("private key %s not written: %v", keyPath, err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("private key mode = %o, want 600", info.Mode().Perm())
	}
}

func TestRunCreate_DefaultOutput(t *testing.T) {
	dir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	if err := runCreate(createFlags{name: "smoke"}); err != nil {
		t.Fatalf("runCreate without --output: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "smoke.hid")); err != nil {
		t.Errorf("default output smoke.hid not created: %v", err)
	}
}

func TestRunCreate_MissingName(t *testing.T) {
	err := runCreate(createFlags{output: filepath.Join(t.TempDir(), "x.hid")})
	if err == nil {
		t.Fatal("expected error for missing --name")
	}
	if !strings.Contains(err.Error(), "--name") {
		t.Errorf("error should mention --name, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// verify
// ---------------------------------------------------------------------------

func TestRunVerify_ValidHID(t *testing.T) {
	hidPath, _ := makeHID(t, "verifier")
	out := captureOutput(func() {
		if err := runVerify(verifyFlags{hid: hidPath}); err != nil {
			t.Errorf("runVerify: %v", err)
		}
	})
	if !strings.Contains(out, "verified") {
		t.Errorf("output should say verified: %q", out)
	}
}

func TestRunVerify_TamperedHID(t *testing.T) {
	hidPath, _ := makeHID(t, "tampered")
	tamperAgentID(t, hidPath)
	err := runVerify(verifyFlags{hid: hidPath})
	if err == nil {
		t.Fatal("expected verification failure for tampered HID")
	}
	if !strings.Contains(err.Error(), "verification") {
		t.Errorf("error should mention verification, got: %v", err)
	}
}

func TestRunVerify_MalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "garbage.hid")
	if err := os.WriteFile(path, []byte(`not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runVerify(verifyFlags{hid: path})
	if err == nil {
		t.Fatal("expected error for malformed HID file")
	}
}

func TestRunVerify_MissingFlag(t *testing.T) {
	err := runVerify(verifyFlags{})
	if err == nil {
		t.Fatal("expected error for missing --hid")
	}
	if !strings.Contains(err.Error(), "--hid") {
		t.Errorf("error should mention --hid, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// export
// ---------------------------------------------------------------------------

func TestRunExport_JSON(t *testing.T) {
	hidPath, _ := makeHID(t, "exporter")
	out := captureOutput(func() {
		if err := runExport(exportFlags{hid: hidPath, format: "json"}); err != nil {
			t.Errorf("runExport json: %v", err)
		}
	})
	var hid identity.HID
	if err := json.Unmarshal([]byte(out), &hid); err != nil {
		t.Fatalf("export json output is not a HID document: %v\n%s", err, out)
	}
	if len(hid.SigBytes) == 0 {
		t.Error("exported HID has no signature")
	}
}

func TestRunExport_Nostr(t *testing.T) {
	hidPath, keyPath := makeHID(t, "nostr-agent")
	out := captureOutput(func() {
		if err := runExport(exportFlags{hid: hidPath, format: "nostr", key: keyPath}); err != nil {
			t.Errorf("runExport nostr: %v", err)
		}
	})
	var event identity.NostrEvent
	if err := json.Unmarshal([]byte(out), &event); err != nil {
		t.Fatalf("export nostr output is not a Nostr event: %v\n%s", err, out)
	}
	if event.Kind != identity.NostrKindMetadata {
		t.Errorf("event kind = %d, want %d", event.Kind, identity.NostrKindMetadata)
	}
	ok, err := event.Verify()
	if err != nil || !ok {
		t.Fatalf("exported Nostr event failed self-verification: ok=%v err=%v", ok, err)
	}
	// The event pubkey must match the HID's public key.
	agent, err := identity.ImportHID(hidPath)
	if err != nil {
		t.Fatal(err)
	}
	wantPub := fmt.Sprintf("%x", agent.PubKey)
	if event.PubKey != wantPub {
		t.Errorf("event pubkey = %q, want %q", event.PubKey, wantPub)
	}
}

func TestRunExport_Nostr_MissingKey(t *testing.T) {
	hidPath, _ := makeHID(t, "nokey")
	err := runExport(exportFlags{hid: hidPath, format: "nostr"})
	if err == nil {
		t.Fatal("expected error when --key is missing for nostr export")
	}
	if !strings.Contains(err.Error(), "--key") {
		t.Errorf("error should mention --key, got: %v", err)
	}
}

func TestRunExport_BadFormat(t *testing.T) {
	hidPath, _ := makeHID(t, "badfmt")
	err := runExport(exportFlags{hid: hidPath, format: "yaml"})
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if !strings.Contains(err.Error(), "yaml") {
		t.Errorf("error should mention the format, got: %v", err)
	}
}

func TestRunExport_MissingHID(t *testing.T) {
	err := runExport(exportFlags{format: "json"})
	if err == nil {
		t.Fatal("expected error for missing --hid")
	}
}

// ---------------------------------------------------------------------------
// import
// ---------------------------------------------------------------------------

func TestRunImport_PrintsIdentity(t *testing.T) {
	hidPath, _ := makeHID(t, "importer")
	agent, err := identity.ImportHID(hidPath)
	if err != nil {
		t.Fatal(err)
	}
	out := captureOutput(func() {
		if err := runImport(importFlags{path: hidPath}); err != nil {
			t.Errorf("runImport: %v", err)
		}
	})
	if !strings.Contains(out, agent.Fingerprint()) {
		t.Errorf("output should contain fingerprint %q: %q", agent.Fingerprint(), out)
	}
	if !strings.Contains(out, agent.ID) {
		t.Errorf("output should contain agent_id %q: %q", agent.ID, out)
	}
}

func TestRunImport_MissingFile(t *testing.T) {
	err := runImport(importFlags{path: filepath.Join(t.TempDir(), "nope.hid")})
	if err == nil {
		t.Fatal("expected error for missing HID file")
	}
}

func TestRunImport_MissingFlag(t *testing.T) {
	err := runImport(importFlags{})
	if err == nil {
		t.Fatal("expected error for missing --path")
	}
}

// ---------------------------------------------------------------------------
// register
// ---------------------------------------------------------------------------

func TestRunRegister_Success(t *testing.T) {
	mf := newMockForgejo(nil)
	defer mf.close()
	withForgejoCreds(t, "helio", "helio123")

	hidPath, _ := makeHID(t, "registered")
	out := captureOutput(func() {
		if err := runRegister(registerFlags{forge: mf.server.URL, agent: hidPath}); err != nil {
			t.Errorf("runRegister: %v", err)
		}
	})
	if !strings.Contains(out, "client-id-1") {
		t.Errorf("output should contain client_id: %q", out)
	}
	if !strings.Contains(out, "secret-shown-once") {
		t.Errorf("output should contain client_secret: %q", out)
	}
	if len(mf.posted) != 1 {
		t.Fatalf("expected 1 registration request, got %d", len(mf.posted))
	}
	if !strings.HasPrefix(mf.posted[0].Name, "helix-agent-") {
		t.Errorf("app name %q missing helix-agent- prefix", mf.posted[0].Name)
	}
}

func TestRunRegister_RefusesInvalidHID(t *testing.T) {
	mf := newMockForgejo(nil)
	defer mf.close()
	withForgejoCreds(t, "helio", "helio123")

	hidPath, _ := makeHID(t, "bad-reg")
	tamperAgentID(t, hidPath)
	err := runRegister(registerFlags{forge: mf.server.URL, agent: hidPath})
	if err == nil {
		t.Fatal("expected refusal for tampered HID")
	}
	if !strings.Contains(err.Error(), "refusing to register") {
		t.Errorf("error should say refusing to register, got: %v", err)
	}
	if len(mf.posted) != 0 {
		t.Error("no registration request should have been sent for an invalid HID")
	}
}

func TestRunRegister_ForgejoUnreachable(t *testing.T) {
	mf := newMockForgejo(nil)
	url := mf.server.URL
	mf.close() // closed → connection refused
	withForgejoCreds(t, "helio", "helio123")

	hidPath, _ := makeHID(t, "unreachable")
	err := runRegister(registerFlags{forge: url, agent: hidPath})
	if err == nil {
		t.Fatal("expected error when Forgejo is unreachable")
	}
}

func TestRunRegister_MissingFlags(t *testing.T) {
	if err := runRegister(registerFlags{agent: "x.hid"}); err == nil {
		t.Error("expected error for missing --forge")
	}
	if err := runRegister(registerFlags{forge: "http://localhost:3000"}); err == nil {
		t.Error("expected error for missing --agent")
	}
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

func TestRunList_Success(t *testing.T) {
	apps := []identity.ForgejoOAuthApp{
		{ID: 1, Name: "helix-agent-abc123", ClientID: "cid-1",
			RedirectURIs: []string{"http://127.0.0.1:3000/oauth/callback"}, Created: "2026-08-03T00:00:00Z"},
		{ID: 2, Name: "helix-agent-def456", ClientID: "cid-2",
			RedirectURIs: []string{"http://127.0.0.1:3000/oauth/callback"}, Created: "2026-08-03T01:00:00Z"},
	}
	mf := newMockForgejo(apps)
	defer mf.close()
	withForgejoCreds(t, "helio", "helio123")

	out := captureOutput(func() {
		if err := runList(listFlags{forge: mf.server.URL}); err != nil {
			t.Errorf("runList: %v", err)
		}
	})
	if !strings.Contains(out, "helix-agent-abc123") || !strings.Contains(out, "helix-agent-def456") {
		t.Errorf("list should contain both apps: %q", out)
	}
	if !strings.Contains(out, "cid-1") || !strings.Contains(out, "cid-2") {
		t.Errorf("list should contain client ids: %q", out)
	}
}

func TestRunList_Empty(t *testing.T) {
	mf := newMockForgejo(nil)
	defer mf.close()
	withForgejoCreds(t, "helio", "helio123")

	out := captureOutput(func() {
		if err := runList(listFlags{forge: mf.server.URL}); err != nil {
			t.Errorf("runList empty: %v", err)
		}
	})
	if !strings.Contains(out, "NO_REGISTERED_AGENTS") {
		t.Errorf("empty list should print NO_REGISTERED_AGENTS: %q", out)
	}
}

func TestRunList_ForgejoUnreachable(t *testing.T) {
	mf := newMockForgejo(nil)
	url := mf.server.URL
	mf.close()
	withForgejoCreds(t, "helio", "helio123")

	err := runList(listFlags{forge: url})
	if err == nil {
		t.Fatal("expected error when Forgejo is unreachable")
	}
}

func TestRunList_MissingCreds(t *testing.T) {
	withForgejoCreds(t, "", "")
	err := runList(listFlags{forge: "http://localhost:3000"})
	if err == nil {
		t.Fatal("expected error when Forgejo credentials are missing")
	}
	if !strings.Contains(err.Error(), "FORGEJO_ADMIN_USER") {
		t.Errorf("error should mention FORGEJO_ADMIN_USER, got: %v", err)
	}
}
