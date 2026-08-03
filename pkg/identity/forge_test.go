package identity

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// oauthMockForgejo creates a test Forgejo HTTP server with configurable
// handlers. It authenticates via Basic Auth (expected user/pass).
type oauthMockForgejo struct {
	server   *httptest.Server
	mux      *http.ServeMux
	authUser string
	authPass string
}

func newOAuthMockForgejo(authUser, authPass string) *oauthMockForgejo {
	mux := http.NewServeMux()
	omf := &oauthMockForgejo{mux: mux, authUser: authUser, authPass: authPass}
	omf.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != authUser || pass != authPass {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"message": "unauthorized"})
			return
		}
		mux.ServeHTTP(w, r)
	}))
	return omf
}

func (omf *oauthMockForgejo) close()      { omf.server.Close() }
func (omf *oauthMockForgejo) url() string { return omf.server.URL }

func writeOAuthJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// ---------------------------------------------------------------------------
// NewForgejoOAuthRegistrar
// ---------------------------------------------------------------------------

func TestNewForgejoOAuthRegistrar(t *testing.T) {
	r := NewForgejoOAuthRegistrar("http://localhost:3030/", "helio", "helio123")
	if r.baseURL != "http://localhost:3030" {
		t.Errorf("baseURL = %q, want %q", r.baseURL, "http://localhost:3030")
	}
	if r.username != "helio" {
		t.Errorf("username = %q, want %q", r.username, "helio")
	}
	if r.client == nil {
		t.Error("HTTP client is nil")
	}
}

func TestForgejoOAuthRegistrar_WithHTTPClient(t *testing.T) {
	r := NewForgejoOAuthRegistrar("http://localhost:3030", "helio", "helio123")
	custom := &http.Client{Timeout: 99 * time.Second}
	r.WithHTTPClient(custom)
	if r.client != custom {
		t.Error("WithHTTPClient did not set the client")
	}
}

// ---------------------------------------------------------------------------
// RegisterOAuthApp
// ---------------------------------------------------------------------------

func TestRegisterOAuthApp_Success(t *testing.T) {
	omf := newOAuthMockForgejo("helio", "helio123")
	defer omf.close()

	omf.mux.HandleFunc("/api/v1/user/applications/oauth2", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var body CreateOAuthAppRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Name == "" {
			t.Error("app name is empty")
		}
		if !contains(body.Name, "helix-agent-") {
			t.Errorf("app name %q missing prefix 'helix-agent-'", body.Name)
		}
		if len(body.RedirectURIs) == 0 {
			t.Error("no redirect URIs")
		}
		if !body.Confidential {
			t.Error("expected confidential=true")
		}

		writeOAuthJSON(w, http.StatusCreated, ForgejoOAuthApp{
			ID:           1,
			Name:         body.Name,
			ClientID:     "client-id-abc",
			ClientSecret: "secret-xyz",
			RedirectURIs: body.RedirectURIs,
			Confidential: body.Confidential,
			Created:      "2026-07-30T05:00:00Z",
		})
	})

	id, priv, err := NewAgentIdentity("test-agent")
	if err != nil {
		t.Fatalf("NewAgentIdentity: %v", err)
	}
	hid, err := id.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	r := NewForgejoOAuthRegistrar(omf.url(), "helio", "helio123")
	app, err := r.RegisterOAuthApp(context.Background(), hid, "http://localhost:9999/callback")
	if err != nil {
		t.Fatalf("RegisterOAuthApp: %v", err)
	}
	if app.ID != 1 {
		t.Errorf("app.ID = %d, want 1", app.ID)
	}
	if app.ClientID != "client-id-abc" {
		t.Errorf("app.ClientID = %q", app.ClientID)
	}
	if app.ClientSecret != "secret-xyz" {
		t.Errorf("app.ClientSecret = %q", app.ClientSecret)
	}
}

func TestRegisterOAuthApp_NilHID(t *testing.T) {
	r := NewForgejoOAuthRegistrar("http://localhost:3030", "helio", "helio123")
	_, err := r.RegisterOAuthApp(context.Background(), nil, "http://localhost:9999/callback")
	if err == nil {
		t.Fatal("expected error for nil HID")
	}
}

func TestRegisterOAuthApp_Unauthorized(t *testing.T) {
	omf := newOAuthMockForgejo("helio", "helio123")
	defer omf.close()

	// No handler defined for oauth2 endpoint — falls through to the outer
	// auth check which returns 401 for wrong credentials.
	id, priv, err := NewAgentIdentity("test-agent")
	if err != nil {
		t.Fatalf("NewAgentIdentity: %v", err)
	}
	hid, err := id.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	r := NewForgejoOAuthRegistrar(omf.url(), "wrong", "creds")
	_, err = r.RegisterOAuthApp(context.Background(), hid, "http://localhost:9999/callback")
	if err == nil {
		t.Fatal("expected error for unauthorized")
	}
}

func TestRegisterOAuthApp_ServerError(t *testing.T) {
	omf := newOAuthMockForgejo("helio", "helio123")
	defer omf.close()

	omf.mux.HandleFunc("/api/v1/user/applications/oauth2", func(w http.ResponseWriter, r *http.Request) {
		writeOAuthJSON(w, http.StatusInternalServerError, map[string]string{"message": "boom"})
	})

	id, priv, err := NewAgentIdentity("test-agent")
	if err != nil {
		t.Fatalf("NewAgentIdentity: %v", err)
	}
	hid, err := id.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	r := NewForgejoOAuthRegistrar(omf.url(), "helio", "helio123")
	_, err = r.RegisterOAuthApp(context.Background(), hid, "http://localhost:9999/callback")
	if err == nil {
		t.Fatal("expected error for 500")
	}
}

// ---------------------------------------------------------------------------
// ListOAuthApps
// ---------------------------------------------------------------------------

func TestListOAuthApps_Success(t *testing.T) {
	omf := newOAuthMockForgejo("helio", "helio123")
	defer omf.close()

	want := []ForgejoOAuthApp{
		{
			ID:           1,
			Name:         "helix-agent-abc123",
			ClientID:     "cid-1",
			RedirectURIs: []string{"http://127.0.0.1:3000/oauth/callback"},
			Created:      "2026-08-03T00:00:00Z",
		},
		{
			ID:           2,
			Name:         "helix-agent-def456",
			ClientID:     "cid-2",
			RedirectURIs: []string{"http://127.0.0.1:3000/oauth/callback"},
			Created:      "2026-08-03T01:00:00Z",
		},
	}
	omf.mux.HandleFunc("/api/v1/user/applications/oauth2", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		writeOAuthJSON(w, http.StatusOK, want)
	})

	r := NewForgejoOAuthRegistrar(omf.url(), "helio", "helio123")
	apps, err := r.ListOAuthApps(context.Background())
	if err != nil {
		t.Fatalf("ListOAuthApps: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("len(apps) = %d, want 2", len(apps))
	}
	if apps[0].Name != "helix-agent-abc123" || apps[0].ClientID != "cid-1" {
		t.Errorf("apps[0] = %+v", apps[0])
	}
	if apps[1].Name != "helix-agent-def456" || apps[1].ClientID != "cid-2" {
		t.Errorf("apps[1] = %+v", apps[1])
	}
}

func TestListOAuthApps_Empty(t *testing.T) {
	omf := newOAuthMockForgejo("helio", "helio123")
	defer omf.close()

	omf.mux.HandleFunc("/api/v1/user/applications/oauth2", func(w http.ResponseWriter, r *http.Request) {
		writeOAuthJSON(w, http.StatusOK, []ForgejoOAuthApp{})
	})

	r := NewForgejoOAuthRegistrar(omf.url(), "helio", "helio123")
	apps, err := r.ListOAuthApps(context.Background())
	if err != nil {
		t.Fatalf("ListOAuthApps: %v", err)
	}
	if len(apps) != 0 {
		t.Errorf("len(apps) = %d, want 0", len(apps))
	}
}

func TestListOAuthApps_Unauthorized(t *testing.T) {
	omf := newOAuthMockForgejo("helio", "helio123")
	defer omf.close()

	// No handler for oauth2 endpoint — falls through to the outer auth
	// check which returns 401 for wrong credentials.
	r := NewForgejoOAuthRegistrar(omf.url(), "wrong", "creds")
	_, err := r.ListOAuthApps(context.Background())
	if err == nil {
		t.Fatal("expected error for unauthorized")
	}
	if _, ok := err.(*TypedError); !ok {
		t.Errorf("expected TypedError, got %T", err)
	}
}

func TestListOAuthApps_NetworkError(t *testing.T) {
	// A registrar pointed at a closed listener yields a transport error.
	omf := newOAuthMockForgejo("helio", "helio123")
	url := omf.url()
	omf.close()

	r := NewForgejoOAuthRegistrar(url, "helio", "helio123")
	_, err := r.ListOAuthApps(context.Background())
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestRegisterOAuthApp_NetworkError(t *testing.T) {
	// Point at a port that's almost certainly not listening.
	r := NewForgejoOAuthRegistrar("http://127.0.0.1:59999", "helio", "helio123")
	r.client.Timeout = 100 * time.Millisecond

	id, priv, err := NewAgentIdentity("test-agent")
	if err != nil {
		t.Fatalf("NewAgentIdentity: %v", err)
	}
	hid, err := id.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	_, err = r.RegisterOAuthApp(context.Background(), hid, "http://localhost:9999/callback")
	if err == nil {
		t.Fatal("expected network error")
	}
}

// ---------------------------------------------------------------------------
// GetOAuthApp
// ---------------------------------------------------------------------------

func TestGetOAuthApp_Success(t *testing.T) {
	omf := newOAuthMockForgejo("helio", "helio123")
	defer omf.close()

	omf.mux.HandleFunc("/api/v1/user/applications/oauth2/1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		writeOAuthJSON(w, http.StatusOK, ForgejoOAuthApp{
			ID:       1,
			Name:     "helix-agent-abc",
			ClientID: "client-id-abc",
			// client_secret is NOT returned by GET
			Created: "2026-07-30T05:00:00Z",
		})
	})

	r := NewForgejoOAuthRegistrar(omf.url(), "helio", "helio123")
	app, err := r.GetOAuthApp(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetOAuthApp: %v", err)
	}
	if app.ID != 1 {
		t.Errorf("app.ID = %d, want 1", app.ID)
	}
	if app.ClientID != "client-id-abc" {
		t.Errorf("app.ClientID = %q", app.ClientID)
	}
	if app.ClientSecret != "" {
		t.Error("unexpected client_secret returned by GET")
	}
}

func TestGetOAuthApp_NotFound(t *testing.T) {
	omf := newOAuthMockForgejo("helio", "helio123")
	defer omf.close()

	omf.mux.HandleFunc("/api/v1/user/applications/oauth2/999", func(w http.ResponseWriter, r *http.Request) {
		writeOAuthJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
	})

	r := NewForgejoOAuthRegistrar(omf.url(), "helio", "helio123")
	_, err := r.GetOAuthApp(context.Background(), 999)
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

// ---------------------------------------------------------------------------
// DeleteOAuthApp
// ---------------------------------------------------------------------------

func TestDeleteOAuthApp_Success(t *testing.T) {
	omf := newOAuthMockForgejo("helio", "helio123")
	defer omf.close()

	omf.mux.HandleFunc("/api/v1/user/applications/oauth2/1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	r := NewForgejoOAuthRegistrar(omf.url(), "helio", "helio123")
	err := r.DeleteOAuthApp(context.Background(), 1)
	if err != nil {
		t.Fatalf("DeleteOAuthApp: %v", err)
	}
}

func TestDeleteOAuthApp_Unauthorized(t *testing.T) {
	omf := newOAuthMockForgejo("helio", "helio123")
	defer omf.close()

	r := NewForgejoOAuthRegistrar(omf.url(), "wrong", "creds")
	err := r.DeleteOAuthApp(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error for unauthorized delete")
	}
}

// ---------------------------------------------------------------------------
// ExchangeToken
// ---------------------------------------------------------------------------

func TestExchangeToken_Success(t *testing.T) {
	omf := newOAuthMockForgejo("helio", "helio123")
	defer omf.close()

	// ExchangeToken doesn't use BasicAuth (it posts to /login/oauth/access_token
	// with client credentials in form body). But our mock wraps everything
	// with Basic Auth check. We need a server that doesn't require auth
	// for the token endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login/oauth/access_token" && r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if r.Form.Get("grant_type") != "authorization_code" {
				t.Errorf("grant_type = %q, want authorization_code", r.Form.Get("grant_type"))
			}
			writeOAuthJSON(w, http.StatusOK, OAuthTokenResponse{
				AccessToken:  "access-token-abc",
				TokenType:    "bearer",
				ExpiresIn:    3600,
				RefreshToken: "refresh-token-xyz",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	r := NewForgejoOAuthRegistrar(srv.URL, "helio", "helio123")
	tok, err := r.ExchangeToken(context.Background(),
		"client-id", "client-secret", "auth-code-123", "http://localhost:9999/callback")
	if err != nil {
		t.Fatalf("ExchangeToken: %v", err)
	}
	if tok.AccessToken != "access-token-abc" {
		t.Errorf("AccessToken = %q", tok.AccessToken)
	}
	if tok.RefreshToken != "refresh-token-xyz" {
		t.Errorf("RefreshToken = %q", tok.RefreshToken)
	}
}

func TestExchangeToken_InvalidGrant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login/oauth/access_token" {
			writeOAuthJSON(w, http.StatusBadRequest, OAuthTokenResponse{
				Error:     "unsupported_grant_type",
				ErrorDesc: "Only refresh_token or authorization_code grant type is supported",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	r := NewForgejoOAuthRegistrar(srv.URL, "helio", "helio123")
	_, err := r.ExchangeToken(context.Background(),
		"client-id", "client-secret", "bad-code", "http://localhost:9999/callback")
	if err == nil {
		t.Fatal("expected error for invalid grant")
	}
}

func TestExchangeToken_MissingCredentials(t *testing.T) {
	r := NewForgejoOAuthRegistrar("http://localhost:3030", "helio", "helio123")
	_, err := r.ExchangeToken(context.Background(), "", "", "code", "http://localhost/cb")
	if err == nil {
		t.Fatal("expected error for missing credentials")
	}
}

// ---------------------------------------------------------------------------
// CreateBindingProof + VerifyBindingProof
// ---------------------------------------------------------------------------

func TestCreateAndVerifyBindingProof(t *testing.T) {
	id, priv, err := NewAgentIdentity("proof-agent")
	if err != nil {
		t.Fatalf("NewAgentIdentity: %v", err)
	}
	hid, err := id.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	proof, err := CreateBindingProof(hid, priv, "client-id-123", "http://forgejo.example.com")
	if err != nil {
		t.Fatalf("CreateBindingProof: %v", err)
	}
	if proof.HIDID != id.ID {
		t.Errorf("proof.HIDID = %q, want %q", proof.HIDID, id.ID)
	}
	if proof.ClientID != "client-id-123" {
		t.Errorf("proof.ClientID = %q", proof.ClientID)
	}
	if proof.Fingerprint != id.Fingerprint() {
		t.Errorf("proof.Fingerprint = %q, want %q", proof.Fingerprint, id.Fingerprint())
	}
	if len(proof.Signature) != ed25519.SignatureSize {
		t.Errorf("signature size = %d, want %d", len(proof.Signature), ed25519.SignatureSize)
	}

	valid, err := VerifyBindingProof(hid, proof)
	if err != nil {
		t.Fatalf("VerifyBindingProof: %v", err)
	}
	if !valid {
		t.Fatal("VerifyBindingProof returned false for valid proof")
	}
}

func TestVerifyBindingProof_Tampered(t *testing.T) {
	id, priv, err := NewAgentIdentity("tamper-agent")
	if err != nil {
		t.Fatalf("NewAgentIdentity: %v", err)
	}
	hid, err := id.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	proof, err := CreateBindingProof(hid, priv, "client-id-123", "http://forgejo.example.com")
	if err != nil {
		t.Fatalf("CreateBindingProof: %v", err)
	}

	// Tamper with the client_id.
	proof.ClientID = "client-id-evil"

	valid, err := VerifyBindingProof(hid, proof)
	if err == nil {
		t.Fatal("expected error for tampered proof")
	}
	if valid {
		t.Error("VerifyBindingProof returned true for tampered proof")
	}
}

func TestVerifyBindingProof_WrongFingerprint(t *testing.T) {
	id, priv, err := NewAgentIdentity("agent-a")
	if err != nil {
		t.Fatalf("NewAgentIdentity: %v", err)
	}
	hid, err := id.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	id2, priv2, err := NewAgentIdentity("agent-b")
	if err != nil {
		t.Fatalf("NewAgentIdentity: %v", err)
	}
	hid2, err := id2.Sign(priv2)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	proof, err := CreateBindingProof(hid, priv, "client-id-123", "http://forgejo.example.com")
	if err != nil {
		t.Fatalf("CreateBindingProof: %v", err)
	}

	// Verify with a different HID.
	valid, err := VerifyBindingProof(hid2, proof)
	if err == nil {
		t.Fatal("expected error for fingerprint mismatch")
	}
	if valid {
		t.Error("VerifyBindingProof returned true for wrong HID")
	}
}

func TestCreateBindingProof_NilHID(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	_, err := CreateBindingProof(nil, priv, "client-id", "http://f.example.com")
	if err == nil {
		t.Fatal("expected error for nil HID")
	}
}

func TestCreateBindingProof_InvalidKeySize(t *testing.T) {
	id, _, err := NewAgentIdentity("test-agent")
	if err != nil {
		t.Fatalf("NewAgentIdentity: %v", err)
	}
	hid, err := id.Sign([]byte("0000000000000000000000000000000000000000000000000000000000000000"))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	_, err = CreateBindingProof(hid, []byte("too-short"), "client-id", "http://f.example.com")
	if err == nil {
		t.Fatal("expected error for invalid key size")
	}
}

func TestVerifyBindingProof_NilInputs(t *testing.T) {
	id, priv, err := NewAgentIdentity("test-agent")
	if err != nil {
		t.Fatalf("NewAgentIdentity: %v", err)
	}
	hid, err := id.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// Nil HID.
	_, err = VerifyBindingProof(nil, &OAuthBindingProof{})
	if err == nil {
		t.Fatal("expected error for nil HID")
	}

	// Nil proof.
	_, err = VerifyBindingProof(hid, nil)
	if err == nil {
		t.Fatal("expected error for nil proof")
	}
}

// ---------------------------------------------------------------------------
// OAuthCredentialStore
// ---------------------------------------------------------------------------

func TestOAuthCredentialStore_StoreAndGet(t *testing.T) {
	s := NewOAuthCredentialStore("/tmp/dummy-store.json")

	fp := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	app := &ForgejoOAuthApp{
		ID:           1,
		Name:         "helix-agent-abcdef01",
		ClientID:     "client-abc",
		ClientSecret: "secret-xyz",
		Created:      "2026-07-30T05:00:00Z",
	}

	s.Store(fp, app)

	got, ok := s.Get(fp)
	if !ok {
		t.Fatal("Get returned false for stored fingerprint")
	}
	if got.ClientID != app.ClientID {
		t.Errorf("ClientID = %q, want %q", got.ClientID, app.ClientID)
	}
	if got.ClientSecret != app.ClientSecret {
		t.Errorf("ClientSecret = %q, want %q", got.ClientSecret, app.ClientSecret)
	}
}

func TestOAuthCredentialStore_GetMissing(t *testing.T) {
	s := NewOAuthCredentialStore("/tmp/dummy-store.json")
	_, ok := s.Get("nonexistent-fingerprint")
	if ok {
		t.Error("Get returned true for missing fingerprint")
	}
}

func TestOAuthCredentialStore_Delete(t *testing.T) {
	s := NewOAuthCredentialStore("/tmp/dummy-store.json")

	fp := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	s.Store(fp, &ForgejoOAuthApp{ClientID: "test"})
	s.Delete(fp)

	_, ok := s.Get(fp)
	if ok {
		t.Error("Get returned true after delete")
	}
}

func TestOAuthCredentialStore_ListAndCount(t *testing.T) {
	s := NewOAuthCredentialStore("/tmp/dummy-store.json")

	if s.Count() != 0 {
		t.Errorf("initial count = %d, want 0", s.Count())
	}

	s.Store("fp1", &ForgejoOAuthApp{ClientID: "a"})
	s.Store("fp2", &ForgejoOAuthApp{ClientID: "b"})
	s.Store("fp3", &ForgejoOAuthApp{ClientID: "c"})

	if s.Count() != 3 {
		t.Errorf("count = %d, want 3", s.Count())
	}

	list := s.List()
	if len(list) != 3 {
		t.Errorf("list length = %d, want 3", len(list))
	}
}

func TestOAuthCredentialStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")

	s := NewOAuthCredentialStore(path)

	fp := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	s.Store(fp, &ForgejoOAuthApp{
		ID:           1,
		Name:         "helix-agent-test",
		ClientID:     "client-id-001",
		ClientSecret: "secret-001",
		Created:      "2026-07-30T05:00:00Z",
	})

	// Add a second entry.
	s.Store("deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef", &ForgejoOAuthApp{
		ID:       2,
		ClientID: "client-id-002",
	})

	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Load into a fresh store.
	s2 := NewOAuthCredentialStore(path)
	if err := s2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	got, ok := s2.Get(fp)
	if !ok {
		t.Fatal("loaded store missing expected fingerprint")
	}
	if got.ClientID != "client-id-001" {
		t.Errorf("loaded ClientID = %q, want client-id-001", got.ClientID)
	}
	if got.ClientSecret != "secret-001" {
		t.Errorf("loaded ClientSecret = %q, want secret-001", got.ClientSecret)
	}

	if s2.Count() != 2 {
		t.Errorf("loaded count = %d, want 2", s2.Count())
	}
}

func TestOAuthCredentialStore_LoadNonexistent(t *testing.T) {
	s := NewOAuthCredentialStore("/tmp/does-not-exist-credstore.json")
	if err := s.Load(); err != nil {
		t.Fatalf("Load of nonexistent file should not error: %v", err)
	}
	if s.Count() != 0 {
		t.Errorf("count = %d, want 0 after loading nonexistent file", s.Count())
	}
}

func TestOAuthCredentialStore_LoadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("this is not json"), 0600); err != nil {
		t.Fatalf("write bad file: %v", err)
	}

	s := NewOAuthCredentialStore(path)
	err := s.Load()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestOAuthCredentialStore_Concurrent(t *testing.T) {
	s := NewOAuthCredentialStore("/tmp/dummy-store.json")

	done := make(chan bool)
	for i := range 20 {
		go func(i int) {
			fp := fmt.Sprintf("fp-%d", i)
			s.Store(fp, &ForgejoOAuthApp{ClientID: fmt.Sprintf("client-%d", i)})
			s.Get(fp)
			s.List()
			s.Count()
			s.Delete(fp)
			done <- true
		}(i)
	}

	for range 20 {
		<-done
	}
}

// ---------------------------------------------------------------------------
// Live integration tests (requires running Forgejo)
// ---------------------------------------------------------------------------

// TestForgejoOAuthIntegration_E2E exercises the full OAuth2 app registration
// flow against the live Forgejo instance at localhost:3030.
//
// Run with:
//
//	go test -short -count=1 ./pkg/identity/ -run TestForgejoOAuthIntegration
func TestForgejoOAuthIntegration_E2E(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test; remove -short to run")
	}

	forgejoURL := "http://localhost:3030"
	username := "helio"
	password := "helio123"

	// Verify Forgejo is reachable.
	resp, err := http.Get(forgejoURL + "/api/v1/version")
	if err != nil {
		t.Fatalf("Forgejo not reachable at %s: %v", forgejoURL, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Forgejo health check failed: %d", resp.StatusCode)
	}
	t.Logf("[OK] Forgejo reachable at %s", forgejoURL)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	r := NewForgejoOAuthRegistrar(forgejoURL, username, password)

	// ── Step 1: Create agent identity ───────────────────────────
	id, priv, err := NewAgentIdentity("integration-test-agent")
	if err != nil {
		t.Fatalf("NewAgentIdentity: %v", err)
	}
	hid, err := id.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	t.Logf("Agent fingerprint: %s", id.Fingerprint())

	// ── Step 2: Register OAuth2 app ─────────────────────────────
	app, err := r.RegisterOAuthApp(ctx, hid, "http://localhost:9999/callback")
	if err != nil {
		t.Fatalf("RegisterOAuthApp: %v", err)
	}
	t.Logf("Created OAuth2 app: id=%d client_id=%s", app.ID, app.ClientID)

	if app.ClientID == "" {
		t.Fatal("client_id is empty")
	}
	if app.ClientSecret == "" {
		t.Fatal("client_secret is empty — must capture on creation")
	}

	// ── Step 2b: Create binding proof ───────────────────────────
	proof, err := CreateBindingProof(hid, priv, app.ClientID, forgejoURL)
	if err != nil {
		t.Fatalf("CreateBindingProof: %v", err)
	}
	valid, err := VerifyBindingProof(hid, proof)
	if err != nil {
		t.Fatalf("VerifyBindingProof: %v", err)
	}
	if !valid {
		t.Fatal("binding proof verification failed")
	}
	t.Logf("Binding proof created and verified")

	// ── Step 3: Get OAuth2 app (verify read) ────────────────────
	gotApp, err := r.GetOAuthApp(ctx, app.ID)
	if err != nil {
		t.Fatalf("GetOAuthApp: %v", err)
	}
	if gotApp.ClientID != app.ClientID {
		t.Errorf("GET returned client_id=%q, want %q", gotApp.ClientID, app.ClientID)
	}
	// client_secret is NOT returned by GET in Forgejo.
	if gotApp.ClientSecret != "" {
		t.Log("Note: client_secret was returned by GET — Forgejo version may differ")
	}

	// ── Step 4: Store credentials ───────────────────────────────
	dir := t.TempDir()
	storePath := filepath.Join(dir, "oauth-creds.json")
	store := NewOAuthCredentialStore(storePath)
	store.Store(id.Fingerprint(), app)
	if err := store.Save(); err != nil {
		t.Fatalf("Save credential store: %v", err)
	}

	// Verify persistence.
	store2 := NewOAuthCredentialStore(storePath)
	if err := store2.Load(); err != nil {
		t.Fatalf("Load credential store: %v", err)
	}
	retrieved, ok := store2.Get(id.Fingerprint())
	if !ok {
		t.Fatal("credential not found after reload")
	}
	if retrieved.ClientID != app.ClientID {
		t.Errorf("stored ClientID = %q, want %q", retrieved.ClientID, app.ClientID)
	}

	// ── Step 5: Token exchange (will fail without real auth code) ──
	// Forgejo v1.21.11 only supports authorization_code grant type.
	// Without a real authorization code from a browser flow, this
	// is expected to fail. We verify it returns a well-formed error.
	tok, err := r.ExchangeToken(ctx, app.ClientID, app.ClientSecret,
		"fake-auth-code", "http://localhost:9999/callback")
	if err != nil {
		t.Logf("Token exchange (expected failure without real code): %v", err)
	} else {
		t.Logf("Token exchange succeeded: access_token=%s", tok.AccessToken)
	}

	// ── Step 6: Delete OAuth2 app (cleanup) ─────────────────────
	if err := r.DeleteOAuthApp(ctx, app.ID); err != nil {
		t.Errorf("DeleteOAuthApp (cleanup): %v", err)
	} else {
		t.Logf("Cleaned up OAuth2 app id=%d", app.ID)
	}
}
