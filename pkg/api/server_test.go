package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// doRequest creates and executes a test request against the server.
func doRequest(t *testing.T, srv *ContractServer, method, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader *strings.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	var req *http.Request
	if bodyReader != nil {
		req = httptest.NewRequest(method, path, bodyReader)
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

// TestHandleHealth verifies the health endpoint returns 200.
func TestHandleHealth(t *testing.T) {
	srv := NewContractServer()
	w := doRequest(t, srv, "GET", "/health", "")
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, "ok", result["status"])
}

// TestHandleListContracts verifies listing all service contracts.
func TestHandleListContracts(t *testing.T) {
	srv := NewContractServer()
	w := doRequest(t, srv, "GET", "/api/v1/contracts", "")
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	services, ok := result["services"].([]any)
	require.True(t, ok)
	assert.Equal(t, 5, len(services))
	assert.EqualValues(t, 5, result["total"])
}

// TestHandleListContracts_PostMethod verifies non-GET is rejected.
func TestHandleListContracts_PostMethod(t *testing.T) {
	srv := NewContractServer()
	w := doRequest(t, srv, "POST", "/api/v1/contracts", "")
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// TestHandleGetServiceContract_Forgejo verifies getting one service contract.
func TestHandleGetServiceContract_Forgejo(t *testing.T) {
	srv := NewContractServer()
	w := doRequest(t, srv, "GET", "/api/v1/contracts/forgejo", "")
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, "forgejo", result["id"])
	assert.Equal(t, "Forgejo", result["name"])
	endpoints, ok := result["endpoints"].([]any)
	require.True(t, ok)
	assert.Greater(t, len(endpoints), 0)
}

// TestHandleGetServiceContract_Chimera verifies Chimera contract.
func TestHandleGetServiceContract_Chimera(t *testing.T) {
	srv := NewContractServer()
	w := doRequest(t, srv, "GET", "/api/v1/contracts/chimera", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestHandleGetServiceContract_UnknownService verifies 404 for unknown service.
func TestHandleGetServiceContract_UnknownService(t *testing.T) {
	srv := NewContractServer()
	w := doRequest(t, srv, "GET", "/api/v1/contracts/nonexistent", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestHandleGetServiceContract_EmptyService verifies 400 for empty service.
func TestHandleGetServiceContract_EmptyService(t *testing.T) {
	srv := NewContractServer()
	w := doRequest(t, srv, "GET", "/api/v1/contracts/", "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandleGetServiceContract_PostMethod verifies non-GET is rejected.
func TestHandleGetServiceContract_PostMethod(t *testing.T) {
	srv := NewContractServer()
	w := doRequest(t, srv, "POST", "/api/v1/contracts/forgejo", "")
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// TestHandleValidate_ForgejoCreateUser_Valid verifies validation of a valid request.
func TestHandleValidate_ForgejoCreateUser_Valid(t *testing.T) {
	srv := NewContractServer()
	body := `{"username":"agent-001","email":"agent@example.com","password":"securepass123"}`
	w := doRequest(t, srv, "POST", "/api/v1/validate/forgejo/create-user", body)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["valid"])
	assert.EqualValues(t, 0, result["error_count"])
}

// TestHandleValidate_ForgejoCreateUser_Invalid verifies validation catches errors.
func TestHandleValidate_ForgejoCreateUser_Invalid(t *testing.T) {
	srv := NewContractServer()
	body := `{"username":"","email":"","password":"short"}`
	w := doRequest(t, srv, "POST", "/api/v1/validate/forgejo/create-user", body)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, false, result["valid"])
	assert.EqualValues(t, 3, result["error_count"])

	errs, ok := result["errors"].([]any)
	require.True(t, ok)
	assert.Greater(t, len(errs), 0)
}

// TestHandleValidate_UnknownEndpoint verifies 404 for unknown endpoint.
func TestHandleValidate_UnknownEndpoint(t *testing.T) {
	srv := NewContractServer()
	body := `{}`
	w := doRequest(t, srv, "POST", "/api/v1/validate/forgejo/nonexistent", body)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestHandleValidate_UnknownService verifies 404 for unknown service.
func TestHandleValidate_UnknownService(t *testing.T) {
	srv := NewContractServer()
	body := `{}`
	w := doRequest(t, srv, "POST", "/api/v1/validate/nonexistent/some-endpoint", body)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestHandleValidate_MalformedJSON verifies 400 for malformed JSON body.
func TestHandleValidate_MalformedJSON(t *testing.T) {
	srv := NewContractServer()
	body := `{not valid json}`
	w := doRequest(t, srv, "POST", "/api/v1/validate/forgejo/create-user", body)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandleValidate_GetMethod verifies GET is rejected.
func TestHandleValidate_GetMethod(t *testing.T) {
	srv := NewContractServer()
	w := doRequest(t, srv, "GET", "/api/v1/validate/forgejo/create-user", "")
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// TestHandleValidate_MissingPath verifies 400 for missing path parts.
func TestHandleValidate_MissingPath(t *testing.T) {
	srv := NewContractServer()
	w := doRequest(t, srv, "POST", "/api/v1/validate/forgejo", "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandleListServices verifies listing all services.
func TestHandleListServices(t *testing.T) {
	srv := NewContractServer()
	w := doRequest(t, srv, "GET", "/api/v1/services", "")
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	services, ok := result["services"].([]any)
	require.True(t, ok)
	assert.Equal(t, 5, len(services))
}

// TestHandleListServices_PostMethod verifies non-GET is rejected.
func TestHandleListServices_PostMethod(t *testing.T) {
	srv := NewContractServer()
	w := doRequest(t, srv, "POST", "/api/v1/services", "")
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// TestHandleValidate_AllServices verifies all 5 services have at least one validatable endpoint.
func TestHandleValidate_AllServices(t *testing.T) {
	tests := []struct {
		service string
		body    string
	}{
		{"forgejo/create-user", `{"username":"a","email":"a@b.com","password":"password123"}`},
		{"chimera/run-deliberation", `{"prompt":"test prompt","formation":"auto"}`},
		{"conscientiousness/evaluate-pr", `{"pr_diff":"diff content","pr_context":{"repo":"helix","number":1},"adversarial_agents":["@redteam"]}`},
		{"hivemind/write-memory", `{"agent_id":"agent-001","repo":"helix","event_type":"merge","summary":"merged PR 1","resolution":"approved"}`},
		{"muster/generate-mcp-tools", `{"openapi_spec_url":"https://example.com/spec.json","output_format":"json"}`},
	}

	srv := NewContractServer()
	for _, tt := range tests {
		t.Run(tt.service, func(t *testing.T) {
			w := doRequest(t, srv, "POST", "/api/v1/validate/"+tt.service, tt.body)
			assert.Equal(t, http.StatusOK, w.Code, "service %s should be validatable", tt.service)
		})
	}
}

// TestSlugify verifies the slug generation.
func TestSlugify(t *testing.T) {
	assert.Equal(t, "create-user", slugify("Create User"))
	assert.Equal(t, "merge-pr", slugify("Merge PR"))
	assert.Equal(t, "deliberate", slugify("Deliberate"))
}

// TestContractServer_ContentType verifies JSON content type.
func TestContractServer_ContentType(t *testing.T) {
	srv := NewContractServer()
	w := doRequest(t, srv, "GET", "/api/v1/contracts", "")
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
}

// TestContractServer_NotFoundRoute verifies unknown routes return 404.
func TestContractServer_NotFoundRoute(t *testing.T) {
	srv := NewContractServer()
	w := doRequest(t, srv, "GET", "/api/v1/nonexistent", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestContractServer_HandlesAllFiveServices verifies all 5 services are returned.
func TestContractServer_HandlesAllFiveServices(t *testing.T) {
	srv := NewContractServer()
	w := doRequest(t, srv, "GET", "/api/v1/contracts", "")
	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	services, ok := result["services"].([]any)
	require.True(t, ok)

	serviceIDs := make(map[string]bool)
	for _, s := range services {
		svc, ok := s.(map[string]any)
		require.True(t, ok)
		serviceIDs[svc["id"].(string)] = true
	}
	assert.True(t, serviceIDs["forgejo"])
	assert.True(t, serviceIDs["chimera"])
	assert.True(t, serviceIDs["conscientiousness"])
	assert.True(t, serviceIDs["hivemind"])
	assert.True(t, serviceIDs["muster"])
}
