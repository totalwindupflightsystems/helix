package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// =============================================================================
// Contract HTTP Server
// =============================================================================

// ContractServer is a read-only HTTP server that exposes API contract schemas
// and validates requests against them. It does NOT proxy to real services —
// it's a development/debugging tool per spec §15.
type ContractServer struct {
	mux *http.ServeMux
}

// NewContractServer creates a new contract server with all routes registered.
func NewContractServer() *ContractServer {
	s := &ContractServer{mux: http.NewServeMux()}
	s.registerRoutes()
	return s
}

// ServeHTTP implements http.Handler.
func (s *ContractServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// registerRoutes sets up all API contract endpoints.
func (s *ContractServer) registerRoutes() {
	s.mux.HandleFunc("/api/v1/contracts", s.handleListContracts)
	s.mux.HandleFunc("/api/v1/contracts/", s.handleGetServiceContract)
	s.mux.HandleFunc("/api/v1/validate/", s.handleValidate)
	s.mux.HandleFunc("/api/v1/services", s.handleListServices)
	s.mux.HandleFunc("/health", s.handleHealth)
}

// =============================================================================
// Handlers
// =============================================================================

// handleHealth responds to health checks.
func (s *ContractServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "helix-api-contract-server",
	})
}

// handleListContracts lists all service contracts with their endpoints.
func (s *ContractServer) handleListContracts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	services := AllServices()
	type endpointInfo struct {
		Name         string `json:"name"`
		Method       string `json:"method"`
		Path         string `json:"path"`
		RequestType  string `json:"request_type,omitempty"`
		ResponseType string `json:"response_type,omitempty"`
	}

	type serviceContract struct {
		ID        string         `json:"id"`
		Name      string         `json:"name"`
		BaseURL   string         `json:"base_url"`
		Endpoints []endpointInfo `json:"endpoints"`
	}

	var result []serviceContract
	for _, svc := range services {
		endpoints := EndpointsForService(svc.ID)
		var eps []endpointInfo
		for _, ep := range endpoints {
			eps = append(eps, endpointInfo{
				Name:         ep.Name,
				Method:       string(ep.Method),
				Path:         ep.Path,
				RequestType:  ep.RequestType,
				ResponseType: ep.ResponseType,
			})
		}
		result = append(result, serviceContract{
			ID:        string(svc.ID),
			Name:      svc.Name,
			BaseURL:   svc.BaseURL,
			Endpoints: eps,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"services": result,
		"total":    len(result),
	})
}

// handleGetServiceContract returns one service's contract.
func (s *ContractServer) handleGetServiceContract(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Path: /api/v1/contracts/{service}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/contracts/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "service name required")
		return
	}

	svcID := ServiceID(parts[0])
	endpoints := EndpointsForService(svcID)
	if endpoints == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown service: %s", svcID))
		return
	}

	// Find service info
	var svcInfo ServiceInfo
	found := false
	for _, svc := range AllServices() {
		if svc.ID == svcID {
			svcInfo = svc
			found = true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown service: %s", svcID))
		return
	}

	type endpointInfo struct {
		Name         string `json:"name"`
		Method       string `json:"method"`
		Path         string `json:"path"`
		RequestType  string `json:"request_type,omitempty"`
		ResponseType string `json:"response_type,omitempty"`
		StatusCodes  []int  `json:"status_codes,omitempty"`
	}

	var eps []endpointInfo
	for _, ep := range endpoints {
		eps = append(eps, endpointInfo{
			Name:         ep.Name,
			Method:       string(ep.Method),
			Path:         ep.Path,
			RequestType:  ep.RequestType,
			ResponseType: ep.ResponseType,
			StatusCodes:  ep.StatusCodes,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":          string(svcInfo.ID),
		"name":        svcInfo.Name,
		"base_url":    svcInfo.BaseURL,
		"auth_header": svcInfo.AuthHeader,
		"health_path": svcInfo.HealthPath,
		"endpoints":   eps,
	})
}

// handleValidate validates a request body against a service endpoint contract.
func (s *ContractServer) handleValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Path: /api/v1/validate/{service}/{endpoint}
	pathPart := strings.TrimPrefix(r.URL.Path, "/api/v1/validate/")
	parts := strings.SplitN(pathPart, "/", 2)
	if len(parts) < 2 {
		writeError(w, http.StatusBadRequest, "path format: /api/v1/validate/{service}/{endpoint}")
		return
	}

	svcID := ServiceID(parts[0])
	endpointSlug := parts[1]

	// Find the endpoint
	endpoints := EndpointsForService(svcID)
	if endpoints == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown service: %s", svcID))
		return
	}

	var matched *EndpointDef
	for i := range endpoints {
		if slugify(endpoints[i].Name) == endpointSlug {
			matched = &endpoints[i]
			break
		}
	}
	if matched == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown endpoint: %s/%s", svcID, endpointSlug))
		return
	}

	// Read request body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("reading body: %v", err))
		return
	}

	// Verify it's valid JSON
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	// Validate based on endpoint
	validator := NewContractValidator()
	validateEndpointDispatch(validator, svcID, matched.Name, bodyBytes)

	result := map[string]any{
		"service":     string(svcID),
		"endpoint":    matched.Name,
		"valid":       !validator.HasErrors(),
		"error_count": len(validator.Errors()),
	}

	if validator.HasErrors() {
		errList := make([]map[string]string, len(validator.Errors()))
		for i, e := range validator.Errors() {
			errList[i] = map[string]string{
				"service":  string(e.Service),
				"endpoint": e.Endpoint,
				"field":    e.Field,
				"message":  e.Message,
			}
		}
		result["errors"] = errList
	} else {
		result["errors"] = []any{}
	}

	writeJSON(w, http.StatusOK, result)
}

// handleListServices lists all service IDs and names.
func (s *ContractServer) handleListServices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	services := AllServices()
	type svcBrief struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	var list []svcBrief
	for _, svc := range services {
		list = append(list, svcBrief{ID: string(svc.ID), Name: svc.Name})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"services": list,
		"total":    len(list),
	})
}

// =============================================================================
// Validation dispatch
// =============================================================================

// validateEndpointDispatch validates a JSON body against the appropriate typed
// validator based on service and endpoint name. Shared between ContractServer
// and the public ValidateFromJSON method.
func validateEndpointDispatch(v *ContractValidator, svc ServiceID, endpointName string, body []byte) {
	raw := body

	switch svc {
	case ServiceForgejo:
		switch endpointName {
		case "Create User":
			var req ForgejoCreateUserRequest
			if err := json.Unmarshal(raw, &req); err == nil {
				v.ValidateForgejoCreateUser(req)
			}
		case "Create SSH Key":
			var req ForgejoCreateSSHKeyRequest
			if err := json.Unmarshal(raw, &req); err == nil {
				v.ValidateForgejoCreateSSHKey(req)
			}
		case "Create PAT":
			var req ForgejoCreatePATRequest
			if err := json.Unmarshal(raw, &req); err == nil {
				v.ValidateForgejoCreatePAT(req)
			}
		case "Merge PR":
			var req ForgejoMergePRRequest
			if err := json.Unmarshal(raw, &req); err == nil {
				v.ValidateForgejoMergePR(req)
			}
		}
	case ServiceChimera:
		if endpointName == "Run Deliberation" {
			var req ChimeraDeliberateRequest
			if err := json.Unmarshal(raw, &req); err == nil {
				v.ValidateChimeraDeliberate(req)
			}
		}
	case ServiceConscientiousness:
		if endpointName == "Evaluate PR" {
			var req ConscientiousnessEvaluateRequest
			if err := json.Unmarshal(raw, &req); err == nil {
				v.ValidateConscientiousnessEvaluate(req)
			}
		}
	case ServiceHivemind:
		switch endpointName {
		case "Write Memory":
			var req HivemindWriteMemoryRequest
			if err := json.Unmarshal(raw, &req); err == nil {
				v.ValidateHivemindWriteMemory(req)
			}
		case "Assign Task":
			var req HivemindAssignTaskRequest
			if err := json.Unmarshal(raw, &req); err == nil {
				v.ValidateHivemindAssignTask(req)
			}
		}
	case ServiceMuster:
		if endpointName == "Generate MCP Tools" {
			var req MusterGenerateRequest
			if err := json.Unmarshal(raw, &req); err == nil {
				v.ValidateMusterGenerate(req)
			}
		}
	}
}

// =============================================================================
// Helpers
// =============================================================================

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// slugify converts an endpoint name to a URL-friendly slug.
func slugify(name string) string {
	return strings.ReplaceAll(strings.ToLower(name), " ", "-")
}
