package forgejo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// mockForgejo creates a test Forgejo server with configurable handlers.
type mockForgejo struct {
	server *httptest.Server
	mux    *http.ServeMux
	calls  []string // tracks request paths
}

func newMockForgejo() *mockForgejo {
	mux := http.NewServeMux()
	mf := &mockForgejo{mux: mux}
	mf.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mf.calls = append(mf.calls, r.URL.Path)
		// Check BasicAuth
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	return mf
}

func (mf *mockForgejo) close() { mf.server.Close() }

func (mf *mockForgejo) url() string { return mf.server.URL }

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ---------------------------------------------------------------------------
// Client construction
// ---------------------------------------------------------------------------

func TestNewClient(t *testing.T) {
	c := NewClient("http://localhost:3030/", "admin", "secret")
	assert.Equal(t, "http://localhost:3030", c.baseURL) // trailing slash trimmed
	assert.Equal(t, "admin", c.username)
	assert.Equal(t, "secret", c.password)
	assert.NotNil(t, c.httpClient)
	assert.NotNil(t, c.circuit)
}

func TestClient_WithHTTPClient(t *testing.T) {
	c := NewClient("http://localhost:3030", "admin", "secret")
	custom := &http.Client{Timeout: 99 * time.Second}
	c.WithHTTPClient(custom)
	assert.Equal(t, custom, c.httpClient)
}

// ---------------------------------------------------------------------------
// NoopCircuitBreaker
// ---------------------------------------------------------------------------

func TestNoopCircuitBreaker(t *testing.T) {
	cb := NoopCircuitBreaker{}
	assert.True(t, cb.Allow())
	cb.RecordSuccess()
	cb.RecordFailure()
}

// ---------------------------------------------------------------------------
// GetUser
// ---------------------------------------------------------------------------

func TestGetUser_Success(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	mf.mux.HandleFunc("/api/v1/users/wojons", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		writeJSON(w, http.StatusOK, User{
			ID:       1,
			UserName: "wojons",
			Email:    "wojons@example.com",
			IsActive: true,
		})
	})

	c := NewClient(mf.url(), "admin", "secret")
	user, err := c.GetUser(context.Background(), "wojons")
	require.NoError(t, err)
	assert.Equal(t, "wojons", user.UserName)
	assert.Equal(t, int64(1), user.ID)
	assert.True(t, user.IsActive)
}

func TestGetUser_NotFound(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	mf.mux.HandleFunc("/api/v1/users/ghost", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "user not found"})
	})

	c := NewClient(mf.url(), "admin", "secret")
	_, err := c.GetUser(context.Background(), "ghost")
	require.Error(t, err)

	apiErr, ok := err.(*APIError)
	require.True(t, ok)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	assert.Contains(t, apiErr.Error(), "404")
}

func TestGetUser_Unauthorized(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	c := NewClient(mf.url(), "wrong", "creds")
	_, err := c.GetUser(context.Background(), "anyone")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// CreateUser
// ---------------------------------------------------------------------------

func TestCreateUser_Success(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	mf.mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		var req CreateUserRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "newagent", req.UserName)
		writeJSON(w, http.StatusCreated, User{
			ID:       42,
			UserName: req.UserName,
			Email:    req.Email,
		})
	})

	c := NewClient(mf.url(), "admin", "secret")
	user, err := c.CreateUser(context.Background(), CreateUserRequest{
		UserName: "newagent",
		Email:    "agent@helix.dev",
		Password: "temp-pass",
	})
	require.NoError(t, err)
	assert.Equal(t, "newagent", user.UserName)
	assert.Equal(t, int64(42), user.ID)
}

func TestCreateUser_Conflict(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	mf.mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusConflict, map[string]string{"message": "user already exists"})
	})

	c := NewClient(mf.url(), "admin", "secret")
	_, err := c.CreateUser(context.Background(), CreateUserRequest{
		UserName: "existing",
		Email:    "e@e.com",
		Password: "p",
	})
	require.Error(t, err)
	apiErr, ok := err.(*APIError)
	require.True(t, ok)
	assert.Equal(t, http.StatusConflict, apiErr.StatusCode)
}

// ---------------------------------------------------------------------------
// DeleteUser
// ---------------------------------------------------------------------------

func TestDeleteUser_Success(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	mf.mux.HandleFunc("/api/v1/admin/users/oldagent", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	})

	c := NewClient(mf.url(), "admin", "secret")
	err := c.DeleteUser(context.Background(), "oldagent")
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// SSH Key operations
// ---------------------------------------------------------------------------

func TestCreateSSHKey_Success(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	mf.mux.HandleFunc("/api/v1/user/keys", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "agent-key", body["title"])
		assert.Contains(t, body["key"], "ssh-ed25519")
		writeJSON(w, http.StatusCreated, SSHKey{
			ID:    1,
			Title: body["title"],
			Key:   body["key"],
		})
	})

	c := NewClient(mf.url(), "admin", "secret")
	key, err := c.CreateSSHKey(context.Background(), "agent-key", "ssh-ed25519 AAAA...")
	require.NoError(t, err)
	assert.Equal(t, "agent-key", key.Title)
}

func TestListSSHKeys_Success(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	mf.mux.HandleFunc("/api/v1/user/keys", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		writeJSON(w, http.StatusOK, []SSHKey{
			{ID: 1, Title: "key1", Key: "ssh-rsa AAAA..."},
			{ID: 2, Title: "key2", Key: "ssh-ed25519 BBBB..."},
		})
	})

	c := NewClient(mf.url(), "admin", "secret")
	keys, err := c.ListSSHKeys(context.Background())
	require.NoError(t, err)
	assert.Len(t, keys, 2)
	assert.Equal(t, "key1", keys[0].Title)
}

// ---------------------------------------------------------------------------
// PAT operations
// ---------------------------------------------------------------------------

func TestCreatePAT_Success(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	mf.mux.HandleFunc("/api/v1/users/wojons/tokens", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "helix-pat", body["name"])
		writeJSON(w, http.StatusCreated, PAT{
			ID:   1,
			Name: "helix-pat",
			SHA1: "abc123sha1token",
		})
	})

	c := NewClient(mf.url(), "admin", "secret")
	pat, err := c.CreatePAT(context.Background(), "wojons", "helix-pat")
	require.NoError(t, err)
	assert.Equal(t, "helix-pat", pat.Name)
	assert.Equal(t, "abc123sha1token", pat.SHA1)
}

// ---------------------------------------------------------------------------
// PR operations
// ---------------------------------------------------------------------------

func TestListPRs_Success(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	mf.mux.HandleFunc("/api/v1/repos/helix/core/pulls", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "open", r.URL.Query().Get("state"))
		writeJSON(w, http.StatusOK, []PullRequest{
			{ID: 1, Number: 1, Title: "Add feature X", State: "open"},
			{ID: 2, Number: 2, Title: "Fix bug Y", State: "open"},
		})
	})

	c := NewClient(mf.url(), "admin", "secret")
	prs, err := c.ListPRs(context.Background(), "helix", "core", "open")
	require.NoError(t, err)
	assert.Len(t, prs, 2)
	assert.Equal(t, "Add feature X", prs[0].Title)
}

func TestGetPRReviews_Success(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	mf.mux.HandleFunc("/api/v1/repos/helix/core/pulls/5/reviews", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		writeJSON(w, http.StatusOK, []PRReview{
			{ID: 1, Body: "LGTM", State: "APPROVED"},
			{ID: 2, Body: "Fix this", State: "REQUEST_CHANGES"},
		})
	})

	c := NewClient(mf.url(), "admin", "secret")
	reviews, err := c.GetPRReviews(context.Background(), "helix", "core", 5)
	require.NoError(t, err)
	assert.Len(t, reviews, 2)
	assert.Equal(t, "APPROVED", reviews[0].State)
	assert.Equal(t, "REQUEST_CHANGES", reviews[1].State)
}

func TestCreatePRReview_Success(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	mf.mux.HandleFunc("/api/v1/repos/helix/core/pulls/5/reviews", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		var body CreatePRReviewRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "COMMENT", body.Event)
		assert.Contains(t, body.Body, "Chimera review")
		w.WriteHeader(http.StatusCreated)
	})

	c := NewClient(mf.url(), "admin", "secret")
	err := c.CreatePRReview(context.Background(), "helix", "core", 5, CreatePRReviewRequest{
		Body:  "Chimera review: APPROVED",
		Event: "COMMENT",
	})
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Circuit breaker integration
// ---------------------------------------------------------------------------

type mockCircuitBreaker struct {
	allow     bool
	successes int
	failures  int
}

func (m *mockCircuitBreaker) Allow() bool    { return m.allow }
func (m *mockCircuitBreaker) RecordSuccess() { m.successes++ }
func (m *mockCircuitBreaker) RecordFailure() { m.failures++ }

func TestClient_CircuitBreakerOpen(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	cb := &mockCircuitBreaker{allow: false}
	c := NewClient(mf.url(), "admin", "secret").WithCircuitBreaker(cb)

	_, err := c.GetUser(context.Background(), "anyone")
	require.Error(t, err)
	assert.Equal(t, ErrCircuitOpen, err)

	// Server should not have been called
	assert.Empty(t, mf.calls)
}

func TestClient_CircuitBreakerRecordsSuccess(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	mf.mux.HandleFunc("/api/v1/users/test", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, User{UserName: "test"})
	})

	cb := &mockCircuitBreaker{allow: true}
	c := NewClient(mf.url(), "admin", "secret").WithCircuitBreaker(cb)

	_, err := c.GetUser(context.Background(), "test")
	require.NoError(t, err)
	assert.Equal(t, 1, cb.successes)
	assert.Equal(t, 0, cb.failures)
}

func TestClient_CircuitBreakerRecordsFailure(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	mf.mux.HandleFunc("/api/v1/users/test", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "oops"})
	})

	cb := &mockCircuitBreaker{allow: true}
	c := NewClient(mf.url(), "admin", "secret").WithCircuitBreaker(cb)

	_, err := c.GetUser(context.Background(), "test")
	require.Error(t, err)
	assert.Equal(t, 0, cb.successes)
	assert.Equal(t, 1, cb.failures)
}

// ---------------------------------------------------------------------------
// Network errors
// ---------------------------------------------------------------------------

func TestClient_NetworkError(t *testing.T) {
	c := NewClient("http://127.0.0.1:59995", "admin", "secret")

	cb := &mockCircuitBreaker{allow: true}
	c.WithCircuitBreaker(cb)

	_, err := c.GetUser(context.Background(), "anyone")
	require.Error(t, err)
	assert.Equal(t, 1, cb.failures)
}

// ---------------------------------------------------------------------------
// Context cancellation
// ---------------------------------------------------------------------------

func TestClient_ContextCancelled(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	mf.mux.HandleFunc("/api/v1/users/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		writeJSON(w, http.StatusOK, User{UserName: "slow"})
	})

	c := NewClient(mf.url(), "admin", "secret")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.GetUser(ctx, "slow")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// APIError
// ---------------------------------------------------------------------------

func TestAPIError(t *testing.T) {
	err := &APIError{
		StatusCode: 422,
		Message:    "validation failed",
		URL:        "http://localhost:3030/api/v1/admin/users",
	}
	assert.Contains(t, err.Error(), "422")
	assert.Contains(t, err.Error(), "validation failed")
	assert.Contains(t, err.Error(), "api/v1/admin/users")
}

// ---------------------------------------------------------------------------
// Auth propagation
// ---------------------------------------------------------------------------

func TestClient_BasicAuth(t *testing.T) {
	var authUser, authPass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authUser, authPass, _ = r.BasicAuth()
		writeJSON(w, http.StatusOK, []SSHKey{})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "helio", "helio123")
	_, _ = c.ListSSHKeys(context.Background())

	assert.Equal(t, "helio", authUser)
	assert.Equal(t, "helio123", authPass)
}

func TestClient_TrailingSlashTrimmed(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	mf.mux.HandleFunc("/api/v1/users/test", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, User{UserName: "test"})
	})

	c := NewClient(mf.url()+"/", "admin", "secret")
	_, err := c.GetUser(context.Background(), "test")
	require.NoError(t, err)
}
