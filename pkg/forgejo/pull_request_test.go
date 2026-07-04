package forgejo

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// CreatePR
// ---------------------------------------------------------------------------

func TestCreatePR_Success(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	mf.mux.HandleFunc("/api/v1/repos/helix-org/helix/pulls", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)

		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "feature/wi-001", body["head"])
		assert.Equal(t, "main", body["base"])
		assert.Equal(t, "Implement wi-001", body["title"])

		writeJSON(w, http.StatusCreated, map[string]any{
			"id":         42,
			"number":     7,
			"state":      "open",
			"title":      "Implement wi-001",
			"body":       body["body"],
			"head_ref":   "feature/wi-001",
			"base_ref":   "main",
			"url":        mf.url() + "/api/v1/repos/helix-org/helix/pulls/7",
			"html_url":   mf.url() + "/helix-org/helix/pulls/7",
			"created_at": "2026-07-03T12:00:00Z",
		})
	})

	c := NewClient(mf.url(), "admin", "secret")
	pr, err := c.CreatePR(context.Background(), "helix-org", "helix",
		"feature/wi-001", "main", "Implement wi-001", "Body of the PR")
	require.NoError(t, err)
	assert.Equal(t, int64(7), pr.Number)
	assert.Equal(t, "open", pr.State)
	assert.Equal(t, "feature/wi-001", pr.HeadRef)
	assert.Equal(t, "main", pr.BaseRef)
	assert.Contains(t, pr.HTMLURL, "/pulls/7")
}

func TestCreatePR_DefaultsState(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	mf.mux.HandleFunc("/api/v1/repos/helix-org/helix/pulls", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":       1,
			"number":   1,
			"head_ref": "f",
			"base_ref": "main",
			"title":    "t",
			// state intentionally omitted
		})
	})

	c := NewClient(mf.url(), "admin", "secret")
	pr, err := c.CreatePR(context.Background(), "helix-org", "helix", "f", "main", "t", "b")
	require.NoError(t, err)
	assert.Equal(t, "open", pr.State, "empty state should default to 'open'")
}

func TestCreatePR_AlreadyExists_409(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	mf.mux.HandleFunc("/api/v1/repos/helix-org/helix/pulls", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"pull request already exists"}`))
	})

	c := NewClient(mf.url(), "admin", "secret")
	_, err := c.CreatePR(context.Background(), "helix-org", "helix", "feature/wi-001", "main", "x", "y")
	require.Error(t, err)
	assert.True(t, IsAlreadyExists(err))
}

func TestCreatePR_InvalidBranches_422(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	mf.mux.HandleFunc("/api/v1/repos/helix-org/helix/pulls", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"head branch must differ from base"}`))
	})

	c := NewClient(mf.url(), "admin", "secret")
	_, err := c.CreatePR(context.Background(), "helix-org", "helix", "main", "main", "x", "y")
	require.Error(t, err)
	apiErr, ok := err.(*APIError)
	require.True(t, ok)
	assert.Equal(t, http.StatusUnprocessableEntity, apiErr.StatusCode)
	// 422 is NOT "already exists" — surface distinctly
	assert.False(t, IsAlreadyExists(err),
		"422 is validation error, not duplicate — should NOT be treated as already exists")
}

func TestCreatePR_ValidationEmpty(t *testing.T) {
	c := NewClient("http://localhost", "u", "p")
	_, err := c.CreatePR(context.Background(), "", "r", "h", "main", "t", "b")
	assert.Error(t, err)
	_, err = c.CreatePR(context.Background(), "o", "", "h", "main", "t", "b")
	assert.Error(t, err)
	_, err = c.CreatePR(context.Background(), "o", "r", "", "main", "t", "b")
	assert.Error(t, err)
	_, err = c.CreatePR(context.Background(), "o", "r", "h", "", "t", "b")
	assert.Error(t, err)
}

func TestCreatePR_AuthFailure(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	c := NewClient(mf.url(), "wrong", "wrong")
	_, err := c.CreatePR(context.Background(), "helix-org", "helix", "f", "main", "t", "b")
	require.Error(t, err)
	apiErr, ok := err.(*APIError)
	require.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
}

// ---------------------------------------------------------------------------
// IsAlreadyExists
// ---------------------------------------------------------------------------

func TestIsAlreadyExists_NilError(t *testing.T) {
	assert.False(t, IsAlreadyExists(nil))
}

func TestIsAlreadyExists_NotAPIError(t *testing.T) {
	assert.False(t, IsAlreadyExists(assert.AnError))
}

func TestIsAlreadyExists_NonConflict(t *testing.T) {
	// 422 (validation error) and 500 (server error) are NOT "already exists"
	for _, code := range []int{422, 500, 404, 401} {
		err := &APIError{StatusCode: code, Message: "test"}
		assert.False(t, IsAlreadyExists(err),
			"status %d should NOT map to already-exists", code)
	}
}

func TestIsAlreadyExists_ConflictOnly(t *testing.T) {
	for _, code := range []int{409} {
		err := &APIError{StatusCode: code, Message: "already exists"}
		assert.True(t, IsAlreadyExists(err),
			"status %d SHOULD map to already-exists", code)
	}
}

// ---------------------------------------------------------------------------
// End-to-end: CreateBranch then CreatePR against the same mock server
// (dispatcher scenario without involving pkg/dispatcher)
// ---------------------------------------------------------------------------

func TestBranchThenPR_EndToEnd(t *testing.T) {
	mf := newMockForgejo()
	defer mf.close()

	var branchCalls, prCalls int
	mf.mux.HandleFunc("/api/v1/repos/helix-org/helix/branches", func(w http.ResponseWriter, r *http.Request) {
		branchCalls++
		writeJSON(w, http.StatusCreated, map[string]any{
			"name":       "feature/wi-007",
			"ref":        "refs/heads/feature/wi-007",
			"commit_sha": "deadbeef",
		})
	})
	mf.mux.HandleFunc("/api/v1/repos/helix-org/helix/pulls", func(w http.ResponseWriter, r *http.Request) {
		prCalls++
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":       99,
			"number":   99,
			"state":    "open",
			"head_ref": "feature/wi-007",
			"base_ref": "main",
			"html_url": mf.url() + "/helix-org/helix/pulls/99",
		})
	})

	c := NewClient(mf.url(), "admin", "secret")

	// 1. Create branch from main
	branch, err := c.CreateBranch(context.Background(), "helix-org", "helix",
		"feature/wi-007", "main")
	require.NoError(t, err)
	assert.Equal(t, "deadbeef", branch.CommitSHA)

	// 2. Open PR
	pr, err := c.CreatePR(context.Background(), "helix-org", "helix",
		"feature/wi-007", "main", "WI-007", strings.Repeat("body ", 4))
	require.NoError(t, err)
	assert.Equal(t, int64(99), pr.Number)
	assert.Contains(t, pr.HTMLURL, "/pulls/99")

	assert.Equal(t, 1, branchCalls, "exactly one branch POST")
	assert.Equal(t, 1, prCalls, "exactly one PR POST")
	assert.Contains(t, mf.calls[0], "/branches")
	assert.Contains(t, mf.calls[1], "/pulls")
}
