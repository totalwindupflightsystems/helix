// Package identity — Forgejo OAuth2 registration layer for Helix agent
// identities (ID-002: Forgejo OAuth Registration).
//
// Agents prove identity by registering an OAuth2 application in Forgejo,
// exchanging authorization tokens, and cryptographically binding the
// Forgejo app to the agent's Ed25519 HID via a signed challenge. The
// resulting credentials (client_id + client_secret) are stored keyed by
// the HID fingerprint so that any component can look up an agent's Forgejo
// identity without re-registration.
//
// Forgejo v1.21.11+ endpoints used:
//
//	POST /api/v1/user/applications/oauth2 – create OAuth2 app
//	GET  /api/v1/user/applications/oauth2/{id} – get app details
//	DELETE /api/v1/user/applications/oauth2/{id} – delete app
//	POST /login/oauth/access_token – exchange authorization code for token
package identity

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Forgejo OAuth2 API types
// ---------------------------------------------------------------------------

// ForgejoOAuthApp is the wire representation of a registered OAuth2
// application in Forgejo. It matches the JSON returned by the
// /api/v1/user/applications/oauth2 endpoint.
type ForgejoOAuthApp struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	RedirectURIs []string `json:"redirect_uris"`
	Confidential bool     `json:"confidential_client"`
	Created      string   `json:"created"`
}

// CreateOAuthAppRequest is the body for POST /api/v1/user/applications/oauth2.
type CreateOAuthAppRequest struct {
	Name         string   `json:"name"`
	RedirectURIs []string `json:"redirect_uris"`
	Confidential bool     `json:"confidential"`
}

// OAuthTokenResponse is returned by POST /login/oauth/access_token when
// exchanging an authorization code.
type OAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error,omitempty"`
	ErrorDesc    string `json:"error_description,omitempty"`
}

// OAuthBindingProof is a cryptographic proof that a HID controls a given
// Forgejo OAuth2 application. It contains the app's client_id signed by
// the HID's Ed25519 private key.
type OAuthBindingProof struct {
	HIDID       string `json:"hid_id"`
	Fingerprint string `json:"fingerprint"`
	ClientID    string `json:"client_id"`
	ForgeURL    string `json:"forge_url"`
	Timestamp   int64  `json:"timestamp"`
	Signature   []byte `json:"signature"`
}

// ---------------------------------------------------------------------------
// ForgejoOAuthRegistrar
// ---------------------------------------------------------------------------

// ForgejoOAuthRegistrar handles OAuth2 application registration and token
// exchange with a Forgejo instance. It authenticates via HTTP Basic Auth
// using the provided admin credentials.
//
// The registrar is safe for concurrent use.
type ForgejoOAuthRegistrar struct {
	baseURL  string
	username string
	password string
	client   *http.Client
}

// NewForgejoOAuthRegistrar creates a registrar pointed at the given Forgejo
// instance. baseURL must not include a trailing slash. Basic-Auth credentials
// must belong to a Forgejo user with permission to create OAuth2 applications
// (any logged-in user can create their own).
func NewForgejoOAuthRegistrar(baseURL, username, password string) *ForgejoOAuthRegistrar {
	return &ForgejoOAuthRegistrar{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// WithHTTPClient replaces the default HTTP client. Useful for testing
// with httptest servers.
func (r *ForgejoOAuthRegistrar) WithHTTPClient(c *http.Client) *ForgejoOAuthRegistrar {
	r.client = c
	return r
}

// RegisterOAuthApp creates a new OAuth2 application in Forgejo for the
// given HID. The application name embeds the agent's fingerprint for
// traceability, and the redirect URI is set to a local callback that the
// agent can use to complete the authorization-code flow.
//
// On success the returned ForgejoOAuthApp contains the client_id and
// client_secret — the caller MUST capture client_secret immediately as
// Forgejo never exposes it again after creation.
func (r *ForgejoOAuthRegistrar) RegisterOAuthApp(ctx context.Context, hid *HID, redirectURI string) (*ForgejoOAuthApp, error) {
	if hid == nil {
		return nil, NewConfigError("hid must not be nil", nil)
	}

	fp := hid.Identity.Fingerprint()
	shortFP := fp[:16] // first 16 hex chars for readability

	req := CreateOAuthAppRequest{
		Name:         fmt.Sprintf("helix-agent-%s", shortFP),
		RedirectURIs: []string{redirectURI},
		Confidential: true,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, NewInternalError("marshal OAuth app request", err)
	}

	apiURL := r.baseURL + "/api/v1/user/applications/oauth2"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, NewInternalError("create OAuth app request", err)
	}
	httpReq.SetBasicAuth(r.username, r.password)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := r.client.Do(httpReq)
	if err != nil {
		return nil, NewNetworkError(
			fmt.Sprintf("POST %s failed", apiURL), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errBody map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		msg := fmt.Sprintf("POST %s returned %d", apiURL, resp.StatusCode)
		if errBody["message"] != "" {
			msg = errBody["message"]
		}
		return nil, NewAPIError(msg, nil)
	}

	var app ForgejoOAuthApp
	if err := json.NewDecoder(resp.Body).Decode(&app); err != nil {
		return nil, NewInternalError("decode OAuth app response", err)
	}

	return &app, nil
}

// GetOAuthApp retrieves an existing OAuth2 application by ID. The
// client_secret is NOT returned — Forgejo only exposes it on creation.
func (r *ForgejoOAuthRegistrar) GetOAuthApp(ctx context.Context, appID int64) (*ForgejoOAuthApp, error) {
	apiURL := fmt.Sprintf("%s/api/v1/user/applications/oauth2/%d", r.baseURL, appID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, NewInternalError("create get OAuth app request", err)
	}
	httpReq.SetBasicAuth(r.username, r.password)
	httpReq.Header.Set("Accept", "application/json")

	resp, err := r.client.Do(httpReq)
	if err != nil {
		return nil, NewNetworkError(
			fmt.Sprintf("GET %s failed", apiURL), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, NewAPIError(
			fmt.Sprintf("GET %s returned %d", apiURL, resp.StatusCode), nil)
	}

	var app ForgejoOAuthApp
	if err := json.NewDecoder(resp.Body).Decode(&app); err != nil {
		return nil, NewInternalError("decode OAuth app response", err)
	}
	return &app, nil
}

// ListOAuthApps returns every OAuth2 application registered by the
// authenticated user — i.e. every agent that completed a `helix identity
// register` against this Forgejo instance (registrations are named
// helix-agent-<fingerprint-prefix>). Client secrets are NOT included:
// Forgejo only exposes them at creation time.
func (r *ForgejoOAuthRegistrar) ListOAuthApps(ctx context.Context) ([]ForgejoOAuthApp, error) {
	apiURL := r.baseURL + "/api/v1/user/applications/oauth2"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, NewInternalError("create list OAuth apps request", err)
	}
	httpReq.SetBasicAuth(r.username, r.password)
	httpReq.Header.Set("Accept", "application/json")

	resp, err := r.client.Do(httpReq)
	if err != nil {
		return nil, NewNetworkError(
			fmt.Sprintf("GET %s failed", apiURL), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, NewAPIError(
			fmt.Sprintf("GET %s returned %d", apiURL, resp.StatusCode), nil)
	}

	var apps []ForgejoOAuthApp
	if err := json.NewDecoder(resp.Body).Decode(&apps); err != nil {
		return nil, NewInternalError("decode OAuth apps response", err)
	}
	return apps, nil
}

// DeleteOAuthApp removes an OAuth2 application by ID. Returns nil on
// success (HTTP 204).
func (r *ForgejoOAuthRegistrar) DeleteOAuthApp(ctx context.Context, appID int64) error {
	apiURL := fmt.Sprintf("%s/api/v1/user/applications/oauth2/%d", r.baseURL, appID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, apiURL, nil)
	if err != nil {
		return NewInternalError("create delete OAuth app request", err)
	}
	httpReq.SetBasicAuth(r.username, r.password)
	httpReq.Header.Set("Accept", "application/json")

	resp, err := r.client.Do(httpReq)
	if err != nil {
		return NewNetworkError(
			fmt.Sprintf("DELETE %s failed", apiURL), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return NewAPIError(
			fmt.Sprintf("DELETE %s returned %d", apiURL, resp.StatusCode), nil)
	}
	return nil
}

// ExchangeToken exchanges an OAuth2 authorization code for an access token
// using the POST /login/oauth/access_token endpoint. This is the second
// step of the authorization-code flow.
//
// Forgejo v1.21.11 supports authorization_code and refresh_token grant
// types. The client_credentials grant is not available in this version, so
// agents must complete the browser-based authorization-code flow to obtain
// a code before calling this method.
func (r *ForgejoOAuthRegistrar) ExchangeToken(ctx context.Context, clientID, clientSecret, code, redirectURI string) (*OAuthTokenResponse, error) {
	if clientID == "" || clientSecret == "" {
		return nil, NewConfigError("client_id and client_secret are required", nil)
	}

	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)

	tokenURL := r.baseURL + "/login/oauth/access_token"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, NewInternalError("create token exchange request", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := r.client.Do(httpReq)
	if err != nil {
		return nil, NewNetworkError(
			fmt.Sprintf("POST %s failed", tokenURL), err)
	}
	defer resp.Body.Close()

	var tokenResp OAuthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, NewInternalError("decode token response", err)
	}

	if tokenResp.Error != "" {
		return nil, NewAPIError(
			fmt.Sprintf("token exchange failed: %s — %s",
				tokenResp.Error, tokenResp.ErrorDesc), nil)
	}

	return &tokenResp, nil
}

// CreateBindingProof cryptographically proves that a HID controls a Forgejo
// OAuth2 application by signing the client_id + timestamp with the HID's
// Ed25519 private key. Any party that trusts the HID's public key (via its
// fingerprint) can verify this proof independently.
func CreateBindingProof(hid *HID, privKey ed25519.PrivateKey, clientID, forgeURL string) (*OAuthBindingProof, error) {
	if hid == nil {
		return nil, NewConfigError("hid must not be nil", nil)
	}
	if len(privKey) != ed25519.PrivateKeySize {
		return nil, NewConfigError(
			fmt.Sprintf("invalid private key size: got %d, want %d",
				len(privKey), ed25519.PrivateKeySize), nil)
	}

	ts := time.Now().Unix()
	payload := fmt.Sprintf("%s:%s:%s:%d",
		hid.Identity.ID, clientID, forgeURL, ts)

	sig := ed25519.Sign(privKey, []byte(payload))

	return &OAuthBindingProof{
		HIDID:       hid.Identity.ID,
		Fingerprint: hid.Identity.Fingerprint(),
		ClientID:    clientID,
		ForgeURL:    forgeURL,
		Timestamp:   ts,
		Signature:   sig,
	}, nil
}

// VerifyBindingProof checks that an OAuthBindingProof is valid — the
// signature must verify against the identity's public key and the contents
// must match.
func VerifyBindingProof(hid *HID, proof *OAuthBindingProof) (bool, error) {
	if hid == nil || proof == nil {
		return false, NewConfigError("hid and proof must not be nil", nil)
	}

	if proof.Fingerprint != hid.Identity.Fingerprint() {
		return false, fmt.Errorf("fingerprint mismatch: proof claims %q, HID is %q",
			proof.Fingerprint, hid.Identity.Fingerprint())
	}

	payload := fmt.Sprintf("%s:%s:%s:%d",
		proof.HIDID, proof.ClientID, proof.ForgeURL, proof.Timestamp)

	valid := ed25519.Verify(hid.Identity.PubKey, []byte(payload), proof.Signature)
	if !valid {
		return false, fmt.Errorf("binding proof signature verification failed")
	}

	return true, nil
}

// ---------------------------------------------------------------------------
// OAuthCredentialStore — persistent HID → Forgejo OAuth2 mapping
// ---------------------------------------------------------------------------

// OAuthCredentialStore persists Forgejo OAuth2 credentials keyed by HID
// fingerprint so that components can look up an agent's Forgejo identity
// without re-registration.
//
// The store is a JSON file on disk. Credentials are read into memory at
// load time and written atomically on save (write to temp + rename).
type OAuthCredentialStore struct {
	path string
	mu   sync.RWMutex
	data map[string]*ForgejoOAuthApp // fingerprint → app
}

// NewOAuthCredentialStore creates or opens a credential store at the given
// path. If the file doesn't exist, an empty store is initialized in memory
// (call Save to persist).
func NewOAuthCredentialStore(path string) *OAuthCredentialStore {
	return &OAuthCredentialStore{
		path: path,
		data: make(map[string]*ForgejoOAuthApp),
	}
}

// Load reads the credential store from disk. If the file doesn't exist,
// the in-memory map is initialized empty (no error returned).
func (s *OAuthCredentialStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.data = make(map[string]*ForgejoOAuthApp)
			return nil
		}
		return NewConfigError(
			fmt.Sprintf("failed to read credential store %q", s.path), err)
	}

	// The file format is a JSON object mapping fingerprints to app objects.
	var raw map[string]*ForgejoOAuthApp
	if err := json.Unmarshal(data, &raw); err != nil {
		return NewConfigError(
			fmt.Sprintf("failed to parse credential store %q", s.path), err)
	}

	s.data = raw
	return nil
}

// Save writes the credential store to disk atomically by writing to a
// temporary file and renaming it over the target path.
func (s *OAuthCredentialStore) Save() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.data, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return NewInternalError("marshal credential store", err)
	}

	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return NewInternalError(
			fmt.Sprintf("write credential store tmp %q", tmpPath), err)
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		return NewInternalError(
			fmt.Sprintf("rename credential store %q → %q", tmpPath, s.path), err)
	}

	return nil
}

// Store records a ForgejoOAuthApp mapping for the given HID fingerprint.
// The change is NOT persisted to disk until Save() is called.
func (s *OAuthCredentialStore) Store(fingerprint string, app *ForgejoOAuthApp) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[fingerprint] = app
}

// Load retrieves a ForgejoOAuthApp by HID fingerprint. Returns nil if not
// found; the boolean indicates presence.
func (s *OAuthCredentialStore) Get(fingerprint string) (*ForgejoOAuthApp, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	app, ok := s.data[fingerprint]
	return app, ok
}

// Delete removes the credential mapping for a HID fingerprint.
// The change is NOT persisted until Save() is called.
func (s *OAuthCredentialStore) Delete(fingerprint string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, fingerprint)
}

// List returns all stored fingerprints.
func (s *OAuthCredentialStore) List() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.data))
	for fp := range s.data {
		out = append(out, fp)
	}
	return out
}

// Count returns the number of stored credentials.
func (s *OAuthCredentialStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data)
}
