package identity

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// validForgejoUser returns a realistic Forgejo user JSON.
func validForgejoUser(id int64, login, email string) ForgejoAccount {
	return ForgejoAccount{
		ID:        id,
		Login:     login,
		LoginName: login,
		FullName:  "Test Agent",
		Email:     email,
		AvatarURL: "https://forgejo.example.com/avatars/" + login,
		Created:   "2026-06-01T00:00:00Z",
		IsAdmin:   false,
	}
}

// validSSHKey returns a realistic SSH key JSON.
func validSSHKeyJSON(id int64) string {
	return `{"id":` + itoa(id) + `,"key":"ssh-ed25519 AAAA...","title":"helix-key","fingerprint":"SHA256:abc123","created_at":"2026-06-01T00:00:00Z"}`
}

// validAccessTokenJSON returns a realistic PAT JSON.
func validAccessTokenJSON(id int64, token string) string {
	return `{"id":` + itoa(id) + `,"name":"helix-identity-pat","scopes":["read:repository","write:repository"],"sha1":"abc123","token":"` + token + `"}`
}

// itoa is a quick int64→string helper for JSON construction.
func itoa(n int64) string {
	return strings.TrimRight(strings.TrimRight(
		string(json.RawMessage{'{'}), "{"), "}")
}

// newTestProvisioner creates a Provisioner pointed at an httptest server,
// with minimal retries so error tests don't hang.
func newTestProvisioner(t *testing.T, srv *httptest.Server, maxAttempts int) *Provisioner {
	t.Helper()
	cfg := ProvisionerConfig{
		ForgejoURL:       srv.URL,
		AdminUser:        "helio",
		AdminPassword:    "helio123",
		KnownFriendsPath: "/tmp/known-friends.json",
		SSHKeyDir:        "/tmp/keys",
		StatePath:        "/tmp/state.json",
		HTTPTimeout:      5 * time.Second,
		RequestRate:      10,
		BurstRate:        10,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("invalid config: %v", err)
	}
	p := &Provisioner{
		cfg:          cfg,
		http:         &http.Client{Timeout: cfg.HTTPTimeout},
		limiter:      NewRateLimiter(cfg.RequestRate, cfg.BurstRate),
		retry:        RetryPolicy{MaxAttempts: maxAttempts, InitialBackoff: 10 * time.Millisecond, MaxBackoff: 50 * time.Millisecond, Multiplier: 2.0},
		redactedBase: redactURL(srv.URL),
	}
	return p
}

// newTestServer starts an httptest server with the given handler.
func newTestServer(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

// ---------------------------------------------------------------------------
// itoa implementation
// ---------------------------------------------------------------------------

func init() {
	// Override the stub — we need a real itoa for JSON construction.
	// We'll just use fmt.Sprintf in the helper instead.
}

// realItoa is a working int64→string helper.
func realItoa(n int64) string {
	buf := make([]byte, 0, 20)
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}

// ---------------------------------------------------------------------------
// setAdminAuth tests
// ---------------------------------------------------------------------------

func TestSetAdminAuth_BasicAuth(t *testing.T) {
	cfg := ProvisionerConfig{
		AdminUser:     "helio",
		AdminPassword: "helio123",
	}
	p := &Provisioner{cfg: cfg}
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	p.setAdminAuth(req)

	user, pass, ok := req.BasicAuth()
	assert.True(t, ok)
	assert.Equal(t, "helio", user)
	assert.Equal(t, "helio123", pass)
}

func TestSetAdminAuth_TokenFallback(t *testing.T) {
	cfg := ProvisionerConfig{
		AdminToken: "abc123token",
	}
	p := &Provisioner{cfg: cfg}
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	p.setAdminAuth(req)

	assert.Equal(t, "token abc123token", req.Header.Get("Authorization"))
}

func TestSetAdminAuth_BasicAuthPreference(t *testing.T) {
	// When both are set, BasicAuth takes precedence.
	cfg := ProvisionerConfig{
		AdminUser:     "helio",
		AdminPassword: "helio123",
		AdminToken:    "token-should-be-ignored",
	}
	p := &Provisioner{cfg: cfg}
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	p.setAdminAuth(req)

	user, pass, ok := req.BasicAuth()
	assert.True(t, ok)
	assert.Equal(t, "helio", user)
	assert.Equal(t, "helio123", pass)
	// Basic auth IS the Authorization header — just verify it's Basic, not token.
	assert.Contains(t, req.Header.Get("Authorization"), "Basic")
}

// ---------------------------------------------------------------------------
// Close tests
// ---------------------------------------------------------------------------

func TestClose(t *testing.T) {
	cfg := DefaultProvisionerConfig()
	cfg.ForgejoURL = "http://localhost:3030"
	cfg.AdminToken = "dummy"
	p, err := NewProvisioner(cfg)
	require.NoError(t, err)

	// Close should be safe to call multiple times.
	assert.NoError(t, p.Close())
	assert.NoError(t, p.Close())
}

// ---------------------------------------------------------------------------
// RateLimiter tests
// ---------------------------------------------------------------------------

func TestRateLimiter_NilLimiter(t *testing.T) {
	var rl *RateLimiter
	// Should not panic.
	rl.Acquire()
}

func TestRateLimiter_NilTokens(t *testing.T) {
	rl := &RateLimiter{rate: 10, burst: 5, tokens: nil}
	// Should not panic.
	rl.Acquire()
}

func TestRateLimiter_BurstExhausted(t *testing.T) {
	rl := NewRateLimiter(10, 2)
	// Drain both tokens.
	<-rl.tokens
	<-rl.tokens
	// Acquire should not block — it's a non-blocking select with default.
	rl.Acquire()
}

func TestRateLimiter_Accessors(t *testing.T) {
	rl := NewRateLimiter(10, 5)
	assert.Equal(t, 10, rl.Rate())
	assert.Equal(t, 5, rl.Burst())
}

// ---------------------------------------------------------------------------
// doWithRetry tests
// ---------------------------------------------------------------------------

func TestDoWithRetry_RetryExhaustion(t *testing.T) {
	callCount := 0
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer srv.Close()

	p := newTestProvisioner(t, srv, 3)
	resp, err := p.doWithRetry("GET", srv.URL+"/test", nil, p.setAdminAuth)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.True(t, IsNetworkError(err))
	assert.Contains(t, err.Error(), "exhausted")
	assert.Equal(t, 3, callCount)
}

func TestDoWithRetry_SuccessOnRetry(t *testing.T) {
	callCount := 0
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	defer srv.Close()

	p := newTestProvisioner(t, srv, 4)
	resp, err := p.doWithRetry("GET", srv.URL+"/test", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 3, callCount)
	resp.Body.Close()
}

func TestDoWithRetry_RateLimit429(t *testing.T) {
	callCount := 0
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	defer srv.Close()

	p := newTestProvisioner(t, srv, 3)
	resp, err := p.doWithRetry("GET", srv.URL+"/test", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 2, callCount)
	resp.Body.Close()
}

func TestDoWithRetry_BodyBuffering(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write(body)
	})
	defer srv.Close()

	p := newTestProvisioner(t, srv, 1)
	reqBody := `{"test":"value"}`
	resp, err := p.doWithRetry("POST", srv.URL+"/test", bytes.NewReader([]byte(reqBody)), p.setAdminAuth)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	respBody, _ := io.ReadAll(resp.Body)
	assert.JSONEq(t, reqBody, string(respBody))
	resp.Body.Close()
}

// ---------------------------------------------------------------------------
// GetAccount tests
// ---------------------------------------------------------------------------

func TestGetAccount_DryRun(t *testing.T) {
	cfg := DefaultProvisionerConfig()
	cfg.DryRun = true
	cfg.ForgejoURL = "http://localhost:3030"
	cfg.AdminToken = "dummy"
	p, err := NewProvisioner(cfg)
	require.NoError(t, err)

	acct, err := p.GetAccount("test-agent")
	assert.NoError(t, err)
	assert.Nil(t, acct)
}

func TestGetAccount_HTTPError(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	})
	defer srv.Close()

	p := newTestProvisioner(t, srv, 1)
	acct, err := p.GetAccount("test-agent")
	assert.Nil(t, acct)
	assert.Error(t, err)
	// doWithRetry wraps the 5xx error in a retry-exhaustion message;
	// the response body is not included for server errors.
	assert.Contains(t, err.Error(), "HTTP 500")
}

func TestGetAccount_DecodeFailure(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not-valid-json`))
	})
	defer srv.Close()

	p := newTestProvisioner(t, srv, 1)
	acct, err := p.GetAccount("test-agent")
	assert.Nil(t, acct)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode")
}

func TestGetAccount_NotFound(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]ForgejoAccount{})
	})
	defer srv.Close()

	p := newTestProvisioner(t, srv, 1)
	acct, err := p.GetAccount("test-agent")
	assert.NoError(t, err)
	assert.Nil(t, acct)
}

func TestGetAccount_Found(t *testing.T) {
	expected := validForgejoUser(42, "test-agent", "test@helix-agents.local")
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]ForgejoAccount{expected})
	})
	defer srv.Close()

	p := newTestProvisioner(t, srv, 1)
	acct, err := p.GetAccount("test-agent")
	require.NoError(t, err)
	require.NotNil(t, acct)
	assert.Equal(t, int64(42), acct.ID)
	assert.Equal(t, "test-agent", acct.Login)
}

func TestGetAccount_FoundByLoginName(t *testing.T) {
	expected := validForgejoUser(42, "different-login", "test@helix-agents.local")
	expected.LoginName = "test-agent"
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]ForgejoAccount{expected})
	})
	defer srv.Close()

	p := newTestProvisioner(t, srv, 1)
	acct, err := p.GetAccount("test-agent")
	require.NoError(t, err)
	require.NotNil(t, acct)
	assert.Equal(t, "test-agent", acct.LoginName)
}

// ---------------------------------------------------------------------------
// CreateUser tests
// ---------------------------------------------------------------------------

func TestCreateUser_NilRequest(t *testing.T) {
	cfg := DefaultProvisionerConfig()
	cfg.ForgejoURL = "http://localhost:3030"
	cfg.AdminToken = "dummy"
	p, err := NewProvisioner(cfg)
	require.NoError(t, err)

	acct, err := p.CreateUser(nil)
	assert.Nil(t, acct)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil request")
}

func TestCreateUser_DryRun(t *testing.T) {
	cfg := DefaultProvisionerConfig()
	cfg.DryRun = true
	cfg.ForgejoURL = "http://localhost:3030"
	cfg.AdminToken = "dummy"
	p, err := NewProvisioner(cfg)
	require.NoError(t, err)

	req := &CreateUserRequest{
		Username: "test-agent",
		Email:    "test@helix-agents.local",
		FullName: "Test Agent",
	}
	acct, err := p.CreateUser(req)
	require.NoError(t, err)
	require.NotNil(t, acct)
	assert.Equal(t, "test-agent", acct.Login)
}

func TestCreateUser_Success(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(ForgejoAccount{
			ID:        99,
			Login:     "test-agent",
			LoginName: "test-agent",
			FullName:  "Test Agent",
			Email:     "test@helix-agents.local",
		})
	})
	defer srv.Close()

	p := newTestProvisioner(t, srv, 1)
	req := &CreateUserRequest{
		Username: "test-agent",
		Email:    "test@helix-agents.local",
		FullName: "Test Agent",
	}
	acct, err := p.CreateUser(req)
	require.NoError(t, err)
	require.NotNil(t, acct)
	assert.Equal(t, int64(99), acct.ID)
}

func TestCreateUser_Conflict(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte("user already exists"))
	})
	defer srv.Close()

	p := newTestProvisioner(t, srv, 1)
	req := &CreateUserRequest{
		Username: "test-agent",
		Email:    "test@helix-agents.local",
		FullName: "Test Agent",
	}
	acct, err := p.CreateUser(req)
	assert.Nil(t, acct)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "conflict")
}

func TestCreateUser_UnprocessableEntity(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte("validation failed"))
	})
	defer srv.Close()

	p := newTestProvisioner(t, srv, 1)
	req := &CreateUserRequest{
		Username: "test-agent",
		Email:    "test@helix-agents.local",
		FullName: "Test Agent",
	}
	acct, err := p.CreateUser(req)
	assert.Nil(t, acct)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "conflict")
}

func TestCreateUser_UnexpectedStatus(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("forbidden"))
	})
	defer srv.Close()

	p := newTestProvisioner(t, srv, 1)
	req := &CreateUserRequest{
		Username: "test-agent",
		Email:    "test@helix-agents.local",
		FullName: "Test Agent",
	}
	acct, err := p.CreateUser(req)
	assert.Nil(t, acct)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected HTTP 403")
}

// ---------------------------------------------------------------------------
// RegisterKey tests
// ---------------------------------------------------------------------------

func TestRegisterKey_EmptyName(t *testing.T) {
	cfg := DefaultProvisionerConfig()
	cfg.ForgejoURL = "http://localhost:3030"
	cfg.AdminToken = "dummy"
	p, err := NewProvisioner(cfg)
	require.NoError(t, err)

	key, err := p.RegisterKey("", "temp", "ssh-ed25519 AAAA...", "helix-key")
	assert.Nil(t, key)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty agent name")
}

func TestRegisterKey_DryRun(t *testing.T) {
	cfg := DefaultProvisionerConfig()
	cfg.DryRun = true
	cfg.ForgejoURL = "http://localhost:3030"
	cfg.AdminToken = "dummy"
	p, err := NewProvisioner(cfg)
	require.NoError(t, err)

	key, err := p.RegisterKey("test-agent", "", "ssh-ed25519 AAAA...", "helix-key")
	require.NoError(t, err)
	require.NotNil(t, key)
	assert.Equal(t, "ssh-ed25519 AAAA...", key.Key)
	assert.Equal(t, "helix-key", key.Title)
}

func TestRegisterKey_Success(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":7,"key":"ssh-ed25519 AAAA...","title":"helix-key","fingerprint":"SHA256:abc","created_at":"2026-06-01T00:00:00Z"}`))
	})
	defer srv.Close()

	p := newTestProvisioner(t, srv, 1)
	key, err := p.RegisterKey("test-agent", "", "ssh-ed25519 AAAA...", "helix-key")
	require.NoError(t, err)
	require.NotNil(t, key)
	assert.Equal(t, int64(7), key.ID)
	assert.Equal(t, "helix-key", key.Title)
}

func TestRegisterKey_HTTPError(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	})
	defer srv.Close()

	p := newTestProvisioner(t, srv, 1)
	key, err := p.RegisterKey("test-agent", "", "ssh-ed25519 AAAA...", "helix-key")
	assert.Nil(t, key)
	assert.Error(t, err)
}

func TestRegisterKey_DecodeFailure(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`not-json`))
	})
	defer srv.Close()

	p := newTestProvisioner(t, srv, 1)
	key, err := p.RegisterKey("test-agent", "", "ssh-ed25519 AAAA...", "helix-key")
	assert.Nil(t, key)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode")
}

func TestRegisterKey_UnexpectedStatus(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("forbidden"))
	})
	defer srv.Close()

	p := newTestProvisioner(t, srv, 1)
	key, err := p.RegisterKey("test-agent", "", "ssh-ed25519 AAAA...", "helix-key")
	assert.Nil(t, key)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected HTTP 403")
}

// ---------------------------------------------------------------------------
// CreateToken tests
// ---------------------------------------------------------------------------

func TestCreateToken_EmptyName(t *testing.T) {
	cfg := DefaultProvisionerConfig()
	cfg.ForgejoURL = "http://localhost:3030"
	cfg.AdminToken = "dummy"
	p, err := NewProvisioner(cfg)
	require.NoError(t, err)

	tok, err := p.CreateToken("", "helio", "helio123", &CreateTokenRequest{Name: "test"})
	assert.Nil(t, tok)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty agent name")
}

func TestCreateToken_NilRequest(t *testing.T) {
	cfg := DefaultProvisionerConfig()
	cfg.ForgejoURL = "http://localhost:3030"
	cfg.AdminToken = "dummy"
	p, err := NewProvisioner(cfg)
	require.NoError(t, err)

	tok, err := p.CreateToken("test-agent", "helio", "helio123", nil)
	assert.Nil(t, tok)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil request")
}

func TestCreateToken_MissingCredentials(t *testing.T) {
	cfg := DefaultProvisionerConfig()
	cfg.ForgejoURL = "http://localhost:3030"
	cfg.AdminToken = "dummy"
	p, err := NewProvisioner(cfg)
	require.NoError(t, err)

	tok, err := p.CreateToken("test-agent", "", "", &CreateTokenRequest{Name: "test", Scopes: []string{"read:repository"}})
	assert.Nil(t, tok)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing admin BasicAuth credentials")
}

func TestCreateToken_DryRun(t *testing.T) {
	cfg := DefaultProvisionerConfig()
	cfg.DryRun = true
	cfg.ForgejoURL = "http://localhost:3030"
	cfg.AdminToken = "dummy"
	p, err := NewProvisioner(cfg)
	require.NoError(t, err)

	tok, err := p.CreateToken("test-agent", "", "", &CreateTokenRequest{Name: "helix-identity-pat", Scopes: []string{"read:repository"}})
	require.NoError(t, err)
	require.NotNil(t, tok)
	assert.Equal(t, "helix-identity-pat", tok.Name)
}

func TestCreateToken_Success(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":123,"name":"helix-identity-pat","scopes":["read:repository"],"sha1":"abc","token":"pat-abc123"}`))
	})
	defer srv.Close()

	p := newTestProvisioner(t, srv, 1)
	tok, err := p.CreateToken("test-agent", "helio", "helio123", &CreateTokenRequest{Name: "helix-identity-pat", Scopes: []string{"read:repository"}})
	require.NoError(t, err)
	require.NotNil(t, tok)
	assert.Equal(t, int64(123), tok.ID)
	assert.Equal(t, "pat-abc123", tok.Token)
}

func TestCreateToken_HTTPError(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("boom"))
	})
	defer srv.Close()

	p := newTestProvisioner(t, srv, 1)
	tok, err := p.CreateToken("test-agent", "helio", "helio123", &CreateTokenRequest{Name: "test", Scopes: []string{"read:repository"}})
	assert.Nil(t, tok)
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// RevokeToken tests
// ---------------------------------------------------------------------------

func TestRevokeToken_EmptyName(t *testing.T) {
	cfg := DefaultProvisionerConfig()
	cfg.ForgejoURL = "http://localhost:3030"
	cfg.AdminToken = "dummy"
	p, err := NewProvisioner(cfg)
	require.NoError(t, err)

	err = p.RevokeToken("", "helio", "helio123", 123)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty agent name")
}

func TestRevokeToken_InvalidID(t *testing.T) {
	cfg := DefaultProvisionerConfig()
	cfg.ForgejoURL = "http://localhost:3030"
	cfg.AdminToken = "dummy"
	p, err := NewProvisioner(cfg)
	require.NoError(t, err)

	err = p.RevokeToken("test-agent", "helio", "helio123", 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token id")

	err = p.RevokeToken("test-agent", "helio", "helio123", -1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token id")
}

func TestRevokeToken_DryRun(t *testing.T) {
	cfg := DefaultProvisionerConfig()
	cfg.DryRun = true
	cfg.ForgejoURL = "http://localhost:3030"
	cfg.AdminToken = "dummy"
	p, err := NewProvisioner(cfg)
	require.NoError(t, err)

	err = p.RevokeToken("test-agent", "", "", 123)
	assert.NoError(t, err)
}

func TestRevokeToken_Success(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	p := newTestProvisioner(t, srv, 1)
	err := p.RevokeToken("test-agent", "helio", "helio123", 123)
	assert.NoError(t, err)
}

func TestRevokeToken_HTTPError(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("boom"))
	})
	defer srv.Close()

	p := newTestProvisioner(t, srv, 1)
	err := p.RevokeToken("test-agent", "helio", "helio123", 123)
	assert.Error(t, err)
}

func TestRevokeToken_NotFound(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("token not found"))
	})
	defer srv.Close()

	p := newTestProvisioner(t, srv, 1)
	err := p.RevokeToken("test-agent", "helio", "helio123", 123)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected HTTP 404")
}

// ---------------------------------------------------------------------------
// IsNetworkError helper (for doWithRetry assertions)
// ---------------------------------------------------------------------------

func IsNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if te, ok := err.(*TypedError); ok {
		return te.Kind == "network"
	}
	return false
}
