package integration

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSuiteUnit_Forgejo_GetAccount_Found tests GetAccount returning a found user.
func TestSuiteUnit_Forgejo_GetAccount_Found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/users" {
			t.Errorf("path = %s, want /api/v1/admin/users", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "pass" {
			t.Errorf("BasicAuth = %s/%s, want admin/pass", user, pass)
		}
		_, _ = w.Write([]byte(`[{"id":1,"login":"helix-agent","login_name":"helix-agent","email":"a@b.c"}]`))
	}))
	defer srv.Close()

	c, _ := NewForgejoClient(srv.URL, "admin", "pass")
	acct, err := c.GetAccount(t, "helix-agent")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if acct == nil || acct.Login != "helix-agent" {
		t.Errorf("acct = %+v, want helix-agent login", acct)
	}
}

// TestSuiteUnit_Forgejo_GetAccount_NotFound tests GetAccount returning nil for
// a missing user.
func TestSuiteUnit_Forgejo_GetAccount_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":1,"login":"someone-else"}]`))
	}))
	defer srv.Close()

	c, _ := NewForgejoClient(srv.URL, "admin", "pass")
	acct, err := c.GetAccount(t, "ghost")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if acct != nil {
		t.Errorf("acct = %+v, want nil", acct)
	}
}

// TestSuiteUnit_Forgejo_GetAccount_LoginNameMatch verifies LoginName fallback.
func TestSuiteUnit_Forgejo_GetAccount_LoginNameMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":2,"login":"x","login_name":"helix-agent"}]`))
	}))
	defer srv.Close()

	c, _ := NewForgejoClient(srv.URL, "admin", "pass")
	acct, err := c.GetAccount(t, "helix-agent")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if acct == nil || acct.ID != 2 {
		t.Errorf("acct = %+v, want ID 2 via LoginName", acct)
	}
}

// TestSuiteUnit_Forgejo_GetAccount_HTTPError tests GetAccount on a non-200.
func TestSuiteUnit_Forgejo_GetAccount_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"internal error"}`))
	}))
	defer srv.Close()

	c, _ := NewForgejoClient(srv.URL, "admin", "pass")
	_, err := c.GetAccount(t, "anyone")
	if err == nil {
		t.Fatal("err = nil, want HTTP-error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %v, want message containing 500", err)
	}
}

// TestSuiteUnit_Forgejo_CreateUser_Success tests CreateUser returning the created account.
func TestSuiteUnit_Forgejo_CreateUser_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":42,"login":"helix-agent","login_name":"helix-agent"}`))
	}))
	defer srv.Close()

	c, _ := NewForgejoClient(srv.URL, "admin", "pass")
	acct, err := c.CreateUser(t, CreateUserRequest{
		Username: "helix-agent",
		Email:    "a@b.c",
		Password: "TestPass1234567890AbCdEfGh",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if acct == nil || acct.ID != 42 {
		t.Errorf("acct = %+v, want ID 42", acct)
	}
}

// TestSuiteUnit_Forgejo_CreateUser_AlreadyExists tests the 409 → ErrAlreadyExists mapping.
func TestSuiteUnit_Forgejo_CreateUser_AlreadyExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"user exists"}`))
	}))
	defer srv.Close()

	c, _ := NewForgejoClient(srv.URL, "admin", "pass")
	_, err := c.CreateUser(t, CreateUserRequest{Username: "existing"})
	if err != ErrAlreadyExists {
		t.Errorf("err = %v, want ErrAlreadyExists", err)
	}
}

// TestSuiteUnit_Forgejo_CreateUser_UnprocessableEntity tests the 422 → ErrAlreadyExists mapping.
func TestSuiteUnit_Forgejo_CreateUser_UnprocessableEntity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"validation"}`))
	}))
	defer srv.Close()

	c, _ := NewForgejoClient(srv.URL, "admin", "pass")
	_, err := c.CreateUser(t, CreateUserRequest{Username: "invalid"})
	if err != ErrAlreadyExists {
		t.Errorf("err = %v, want ErrAlreadyExists", err)
	}
}

// TestSuiteUnit_Forgejo_CreateUser_UnexpectedStatus tests the default branch.
func TestSuiteUnit_Forgejo_CreateUser_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
	}))
	defer srv.Close()

	c, _ := NewForgejoClient(srv.URL, "admin", "pass")
	_, err := c.CreateUser(t, CreateUserRequest{Username: "x"})
	if err == nil {
		t.Fatal("err = nil, want server-error")
	}
	if err == ErrAlreadyExists {
		t.Errorf("err = ErrAlreadyExists, want non-ErrAlreadyExists server-error")
	}
}

// TestSuiteUnit_Forgejo_RegisterKey_Success tests RegisterKey happy path.
func TestSuiteUnit_Forgejo_RegisterKey_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/users/helix-agent/keys" {
			t.Errorf("path = %s, want /api/v1/admin/users/helix-agent/keys", r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":7,"key":"ssh-ed25519 AAAA","title":"integration-test"}`))
	}))
	defer srv.Close()

	c, _ := NewForgejoClient(srv.URL, "admin", "pass")
	key, err := c.RegisterKey(t, "helix-agent", "ssh-ed25519 AAAA", "integration-test")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if key == nil || key.ID != 7 {
		t.Errorf("key = %+v, want ID 7", key)
	}
}

// TestSuiteUnit_Forgejo_RegisterKey_BadStatus tests the failure branch.
func TestSuiteUnit_Forgejo_RegisterKey_BadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"invalid key"}`))
	}))
	defer srv.Close()

	c, _ := NewForgejoClient(srv.URL, "admin", "pass")
	_, err := c.RegisterKey(t, "helix-agent", "bad-key", "test")
	if err == nil {
		t.Fatal("err = nil, want HTTP-error")
	}
}

// TestSuiteUnit_Forgejo_CreateToken_Success tests CreateToken happy path.
func TestSuiteUnit_Forgejo_CreateToken_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users/helix-agent/tokens" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1,"name":"helix-token","sha1":"abc123","token":"plaintext-token-value"}`))
	}))
	defer srv.Close()

	c, _ := NewForgejoClient(srv.URL, "admin", "pass")
	tok, err := c.CreateToken(t, "helix-agent", "read,write")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if tok == nil || tok.Token != "plaintext-token-value" {
		t.Errorf("tok = %+v, want Token=plaintext-token-value", tok)
	}
}

// TestSuiteUnit_Forgejo_CreateToken_BadStatus tests the failure branch.
func TestSuiteUnit_Forgejo_CreateToken_BadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"token create failed"}`))
	}))
	defer srv.Close()

	c, _ := NewForgejoClient(srv.URL, "admin", "pass")
	_, err := c.CreateToken(t, "helix-agent", "read")
	if err == nil {
		t.Fatal("err = nil, want HTTP-error")
	}
}

// TestSuiteUnit_Forgejo_DeleteUser_NoContent tests DeleteUser with 204.
func TestSuiteUnit_Forgejo_DeleteUser_NoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c, _ := NewForgejoClient(srv.URL, "admin", "pass")
	if err := c.DeleteUser(t, "helix-agent"); err != nil {
		t.Fatalf("err = %v", err)
	}
}

// TestSuiteUnit_Forgejo_DeleteUser_OK tests DeleteUser with 200.
func TestSuiteUnit_Forgejo_DeleteUser_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c, _ := NewForgejoClient(srv.URL, "admin", "pass")
	if err := c.DeleteUser(t, "helix-agent"); err != nil {
		t.Fatalf("err = %v", err)
	}
}

// TestSuiteUnit_Forgejo_DeleteUser_NotFound tests the failure branch.
func TestSuiteUnit_Forgejo_DeleteUser_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"user not found"}`))
	}))
	defer srv.Close()

	c, _ := NewForgejoClient(srv.URL, "admin", "pass")
	err := c.DeleteUser(t, "ghost")
	if err == nil {
		t.Fatal("err = nil, want not-found error")
	}
}

// TestSuiteUnit_IntegrationSuite_Setup_OK tests Setup() with both services healthy.
func TestSuiteUnit_IntegrationSuite_Setup_OK(t *testing.T) {
	// Forgejo server
	forgejoSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/admin/users" {
			_, _ = w.Write([]byte(`[{"id":1,"login":"helio"}]`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer forgejoSrv.Close()

	// Chimera server
	chimeraSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/health" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer chimeraSrv.Close()

	fc, _ := NewForgejoClient(forgejoSrv.URL, "admin", "pass")
	cc, _ := NewChimeraClient(chimeraSrv.URL)
	suite := &IntegrationTestSuite{
		Forgejo: fc, Chimera: cc, Admin: "admin", AdminPass: "pass",
	}
	if err := suite.Setup(t); err != nil {
		t.Fatalf("Setup err = %v", err)
	}
}

// TestSuiteUnit_IntegrationSuite_Setup_ForgejoDown tests Setup with Forgejo unreachable.
func TestSuiteUnit_IntegrationSuite_Setup_ForgejoDown(t *testing.T) {
	// Chimera OK, Forgejo down (use a closed port via httptest URL with Close())
	forgejoSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	forgejoURL := forgejoSrv.URL
	forgejoSrv.Close() // immediately close → connection refused

	chimeraSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer chimeraSrv.Close()

	fc, _ := NewForgejoClient(forgejoURL, "admin", "pass")
	cc, _ := NewChimeraClient(chimeraSrv.URL)
	suite := &IntegrationTestSuite{
		Forgejo: fc, Chimera: cc, Admin: "admin", AdminPass: "pass",
	}
	if err := suite.Setup(t); err == nil {
		t.Fatal("err = nil, want Forgejo-down error")
	}
}

// TestSuiteUnit_IntegrationSuite_Setup_ChimeraDown tests Setup with Chimera returning non-200.
func TestSuiteUnit_IntegrationSuite_Setup_ChimeraDown(t *testing.T) {
	forgejoSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":1,"login":"helio"}]`))
	}))
	defer forgejoSrv.Close()

	chimeraSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer chimeraSrv.Close()

	fc, _ := NewForgejoClient(forgejoSrv.URL, "admin", "pass")
	cc, _ := NewChimeraClient(chimeraSrv.URL)
	suite := &IntegrationTestSuite{
		Forgejo: fc, Chimera: cc, Admin: "admin", AdminPass: "pass",
	}
	if err := suite.Setup(t); err == nil {
		t.Fatal("err = nil, want Chimera-down error")
	}
}

// TestSuiteUnit_IntegrationSuite_Teardown_OK tests Teardown() with a real agent
// to delete (deletes succeed even if agent doesn't exist → no error).
func TestSuiteUnit_IntegrationSuite_Teardown_OK(t *testing.T) {
	// Forgejo returns 204 on delete (silent no-op for missing user)
	forgejoSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
		} else {
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer forgejoSrv.Close()

	fc, _ := NewForgejoClient(forgejoSrv.URL, "admin", "pass")
	suite := &IntegrationTestSuite{Forgejo: fc, Admin: "admin", AdminPass: "pass"}
	suite.Teardown(t) // should not panic, should not error
}

// TestSuiteUnit_IntegrationTestSuite_FromEnv tests NewIntegrationTestSuite with
// env vars that point at closed ports — exercises the env-var reading paths.
func TestSuiteUnit_IntegrationTestSuite_FromEnv(t *testing.T) {
	t.Setenv("FORGEJO_URL", "http://localhost:1")
	t.Setenv("CHIMERA_URL", "http://localhost:2")
	t.Setenv("FORGEJO_ADMIN_USER", "u")
	t.Setenv("FORGEJO_ADMIN_PASSWORD", "p")
	t.Setenv("HELIX_TEST_WORKDIR", "/tmp")

	suite := NewIntegrationTestSuite()
	if suite == nil {
		t.Fatal("suite is nil")
	}
	if suite.Admin != "u" {
		t.Errorf("Admin = %q, want u", suite.Admin)
	}
	if suite.AdminPass != "p" {
		t.Errorf("AdminPass = %q, want p", suite.AdminPass)
	}
	if suite.WorkDir != "/tmp" {
		t.Errorf("WorkDir = %q, want /tmp", suite.WorkDir)
	}
	if suite.Forgejo == nil || suite.Chimera == nil {
		t.Error("Forgejo or Chimera client is nil")
	}
}

// TestSuiteUnit_IntegrationTestSuite_Defaults tests NewIntegrationTestSuite
// picks up the documented defaults when env vars are unset.
func TestSuiteUnit_IntegrationTestSuite_Defaults(t *testing.T) {
	// Clear env so we fall through to defaults
	t.Setenv("FORGEJO_URL", "")
	t.Setenv("CHIMERA_URL", "")
	t.Setenv("FORGEJO_ADMIN_USER", "")
	t.Setenv("FORGEJO_ADMIN_PASSWORD", "")
	t.Setenv("HELIX_TEST_WORKDIR", "")

	suite := NewIntegrationTestSuite()
	if suite == nil {
		t.Fatal("suite is nil")
	}
	if suite.Admin != DefaultAdminUser {
		t.Errorf("Admin = %q, want %q", suite.Admin, DefaultAdminUser)
	}
	if suite.Forgejo == nil || suite.Forgejo.BaseURL != DefaultForgejoURL {
		t.Errorf("Forgejo.BaseURL = %+v, want %q", suite.Forgejo, DefaultForgejoURL)
	}
	if suite.Chimera == nil || suite.Chimera.BaseURL != DefaultChimeraURL {
		t.Errorf("Chimera.BaseURL = %+v, want %q", suite.Chimera, DefaultChimeraURL)
	}
}
