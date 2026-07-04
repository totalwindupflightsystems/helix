package forgejo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// CreateBranch
// ---------------------------------------------------------------------------

func TestCreateBranch_Success(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	mf.mux.HandleFunc("/api/v1/repos/helix-org/helix/branches", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		writeJSON(w, http.StatusCreated, map[string]string{
			"name":       "feature/wi-001",
			"ref":        "refs/heads/feature/wi-001",
			"commit_sha": "abc123def456",
			"url":        mf.url() + "/api/v1/repos/helix-org/helix/branches/feature/wi-001",
		})
	})

	c := NewClient(mf.url(), "admin", "secret")
	resp, err := c.CreateBranch(context.Background(), "helix-org", "helix", "feature/wi-001", "main")
	require.NoError(t, err)
	assert.Equal(t, "feature/wi-001", resp.Name)
	assert.Equal(t, "refs/heads/feature/wi-001", resp.Ref)
	assert.Equal(t, "abc123def456", resp.CommitSHA)
	assert.NotEmpty(t, resp.URL)
}

func TestCreateBranch_EmptyOldRef(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	var receivedBody map[string]string
	mf.mux.HandleFunc("/api/v1/repos/helix-org/helix/branches", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&receivedBody))
		writeJSON(w, http.StatusCreated, map[string]string{
			"name": "feature/wi-002",
			"ref":  "refs/heads/feature/wi-002",
		})
	})

	c := NewClient(mf.url(), "admin", "secret")
	resp, err := c.CreateBranch(context.Background(), "helix-org", "helix", "feature/wi-002", "")
	require.NoError(t, err)
	assert.Equal(t, "feature/wi-002", resp.Name)
	assert.Equal(t, "refs/heads/feature/wi-002", resp.Ref)
	// old_ref should be omitted when empty
	_, hasOld := receivedBody["old_ref"]
	assert.False(t, hasOld, "old_ref should be omitted when fromRef is empty")
}

func TestCreateBranch_AlreadyExists_409(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	mf.mux.HandleFunc("/api/v1/repos/helix-org/helix/branches", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"branch already exists"}`))
	})

	c := NewClient(mf.url(), "admin", "secret")
	_, err := c.CreateBranch(context.Background(), "helix-org", "helix", "feature/wi-001", "main")
	require.Error(t, err)
	apiErr, ok := err.(*APIError)
	require.True(t, ok, "expected APIError, got %T", err)
	assert.Equal(t, http.StatusConflict, apiErr.StatusCode)
	// Idempotency helper
	assert.True(t, IsAlreadyExists(err), "409 should map to IsAlreadyExists=true")
}

func TestCreateBranch_NotFound_404(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	mf.mux.HandleFunc("/api/v1/repos/helix-org/helix/branches", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"repo not found"}`))
	})

	c := NewClient(mf.url(), "admin", "secret")
	_, err := c.CreateBranch(context.Background(), "helix-org", "helix", "feature/wi-001", "main")
	require.Error(t, err)
	apiErr, ok := err.(*APIError)
	require.True(t, ok)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	assert.False(t, IsAlreadyExists(err), "404 is not 'already exists'")
}

func TestCreateBranch_ValidationEmpty(t *testing.T) {
	c := NewClient("http://localhost", "u", "p")
	_, err := c.CreateBranch(context.Background(), "", "r", "feature/wi-001", "main")
	assert.Error(t, err)
	_, err = c.CreateBranch(context.Background(), "o", "", "feature/wi-001", "main")
	assert.Error(t, err)
	_, err = c.CreateBranch(context.Background(), "o", "r", "", "main")
	assert.Error(t, err)
}

func TestCreateBranch_BackfillsCommitSHA(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	mf.mux.HandleFunc("/api/v1/repos/helix-org/helix/branches", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]string{
			"name": "feature/wi-003",
			"ref":  "refs/heads/feature/wi-003",
			// no commit_sha in response
		})
	})

	c := NewClient(mf.url(), "admin", "secret")
	resp, err := c.CreateBranch(context.Background(), "helix-org", "helix", "feature/wi-003", "main")
	require.NoError(t, err)
	// Backfill: commitSHA falls back to ref when missing
	assert.Equal(t, "refs/heads/feature/wi-003", resp.CommitSHA)
}

func TestCreateBranch_AuthFailure(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	// The mockForgejo enforces BasicAuth; use wrong creds.
	c := NewClient(mf.url(), "wrong", "wrong")
	_, err := c.CreateBranch(context.Background(), "helix-org", "helix", "feature/wi-001", "main")
	require.Error(t, err)
	apiErr, ok := err.(*APIError)
	require.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
}

func TestCreateBranch_ContextCancelled(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()
	mf.mux.HandleFunc("/api/v1/repos/helix-org/helix/branches", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]string{"name": "feature/wi-001"})
	})

	c := NewClient(mf.url(), "admin", "secret")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.CreateBranch(ctx, "helix-org", "helix", "feature/wi-001", "main")
	require.Error(t, err)
	// Context errors are returned before HTTP — we just need to know it
	// didn't make a request.
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestBranchRef(t *testing.T) {
	assert.Equal(t, "refs/heads/main", BranchRef("main"))
	assert.Equal(t, "refs/heads/feature/wi-001", BranchRef("feature/wi-001"))
}
