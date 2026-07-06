// Package api encodes the API contracts from spec §15 as typed Go structs
// with request/response validation. Each service's API (Forgejo, Chimera,
// Conscientiousness, Hivemind, Muster) has its endpoints defined as types
// with validation rules matching the spec.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// =============================================================================
// Service Identifiers
// =============================================================================

// ServiceID identifies a Helix platform service.
type ServiceID string

const (
	ServiceForgejo           ServiceID = "forgejo"
	ServiceChimera           ServiceID = "chimera"
	ServiceConscientiousness ServiceID = "conscientiousness"
	ServiceHivemind          ServiceID = "hivemind"
	ServiceMuster            ServiceID = "muster"
)

// ServiceInfo describes a service's API contract.
type ServiceInfo struct {
	ID         ServiceID
	Name       string
	BaseURL    string
	AuthHeader string // Header name for auth (e.g., "Authorization", "X-API-Key")
	AuthScheme string // Auth scheme (e.g., "token", "Bearer")
	HealthPath string // Health check endpoint path
}

// AllServices returns info for all 5 services defined in spec §15.
func AllServices() []ServiceInfo {
	return []ServiceInfo{
		{
			ID:         ServiceForgejo,
			Name:       "Forgejo",
			BaseURL:    "https://helixloop.dev/api/v1",
			AuthHeader: "Authorization",
			AuthScheme: "token",
			HealthPath: "/version",
		},
		{
			ID:         ServiceChimera,
			Name:       "Chimera",
			BaseURL:    "https://chimera.helixloop.dev",
			AuthHeader: "X-API-Key",
			AuthScheme: "",
			HealthPath: "/health",
		},
		{
			ID:         ServiceConscientiousness,
			Name:       "Conscientiousness",
			BaseURL:    "https://conscience.helixloop.dev",
			AuthHeader: "X-API-Key",
			AuthScheme: "",
			HealthPath: "/health",
		},
		{
			ID:         ServiceHivemind,
			Name:       "Hivemind",
			BaseURL:    "https://hivemind.helixloop.dev",
			AuthHeader: "X-API-Key",
			AuthScheme: "",
			HealthPath: "/health",
		},
		{
			ID:         ServiceMuster,
			Name:       "Muster",
			BaseURL:    "https://muster.helixloop.dev",
			AuthHeader: "X-API-Key",
			AuthScheme: "",
			HealthPath: "/health",
		},
	}
}

// =============================================================================
// Endpoint Definition
// =============================================================================

// HTTPMethod is a standard HTTP method.
type HTTPMethod string

const (
	MethodGet    HTTPMethod = "GET"
	MethodPost   HTTPMethod = "POST"
	MethodPut    HTTPMethod = "PUT"
	MethodDelete HTTPMethod = "DELETE"
)

// EndpointDef defines a single API endpoint from the spec.
type EndpointDef struct {
	Service      ServiceID
	Method       HTTPMethod
	Path         string
	Name         string
	RequestType  string // Expected request body type name
	ResponseType string // Expected response body type name
	StatusCodes  []int  // Expected HTTP status codes
}

// =============================================================================
// Forgejo API Contracts (§15.1)
// =============================================================================

// ForgejoCreateUserRequest per spec §15.1 Create User.
type ForgejoCreateUserRequest struct {
	Username           string `json:"username"`
	Email              string `json:"email"`
	Password           string `json:"password"`
	FullName           string `json:"full_name"`
	MustChangePassword bool   `json:"must_change_password"`
	SendNotify         bool   `json:"send_notify"`
}

// ForgejoCreateUserResponse per spec §15.1.
type ForgejoCreateUserResponse struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	FullName string `json:"full_name"`
	Login    string `json:"login"`
}

// ForgejoCreateSSHKeyRequest per spec §15.1.
type ForgejoCreateSSHKeyRequest struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	ReadOnly bool   `json:"read_only"`
}

// ForgejoCreateSSHKeyResponse per spec §15.1.
type ForgejoCreateSSHKeyResponse struct {
	ID        int64  `json:"id"`
	Key       string `json:"key"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
}

// ForgejoCreatePATRequest per spec §15.1.
type ForgejoCreatePATRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

// ForgejoCreatePATResponse per spec §15.1.
type ForgejoCreatePATResponse struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	SHA1           string `json:"sha1"`
	TokenLastEight string `json:"token_last_eight"`
}

// ForgejoGetPRResponse per spec §15.1 Get Pull Request.
type ForgejoGetPRResponse struct {
	ID        int64           `json:"id"`
	Number    int             `json:"number"`
	Title     string          `json:"title"`
	State     string          `json:"state"`
	Mergeable bool            `json:"mergeable"`
	Head      ForgejoPRBranch `json:"head"`
	Base      ForgejoPRBranch `json:"base"`
	User      ForgejoUser     `json:"user"`
}

// ForgejoPRBranch is a branch reference in a PR.
type ForgejoPRBranch struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

// ForgejoUser is a user reference.
type ForgejoUser struct {
	Login string `json:"login"`
}

// ForgejoMergePRRequest per spec §15.1 Merge Pull Request.
type ForgejoMergePRRequest struct {
	Do          string `json:"do"`
	MergeMethod string `json:"merge_method"`
}

// ForgejoMergePRResponse per spec §15.1.
type ForgejoMergePRResponse struct {
	Merged  bool   `json:"merged"`
	Message string `json:"message"`
	SHA     string `json:"sha"`
}

// ForgejoEndpoints returns all Forgejo API endpoints from spec §15.1.
func ForgejoEndpoints() []EndpointDef {
	return []EndpointDef{
		{Service: ServiceForgejo, Method: MethodPost, Path: "/admin/users", Name: "Create User", ResponseType: "ForgejoCreateUserResponse", StatusCodes: []int{201, 400, 403, 409, 422}},
		{Service: ServiceForgejo, Method: MethodPost, Path: "/admin/users/{username}/keys", Name: "Create SSH Key", ResponseType: "ForgejoCreateSSHKeyResponse", StatusCodes: []int{201}},
		{Service: ServiceForgejo, Method: MethodPost, Path: "/users/{username}/tokens", Name: "Create PAT", ResponseType: "ForgejoCreatePATResponse", StatusCodes: []int{201}},
		{Service: ServiceForgejo, Method: MethodGet, Path: "/repos/{owner}/{repo}/pulls/{index}", Name: "Get PR", ResponseType: "ForgejoGetPRResponse", StatusCodes: []int{200}},
		{Service: ServiceForgejo, Method: MethodPost, Path: "/repos/{owner}/{repo}/pulls/{index}/merge", Name: "Merge PR", ResponseType: "ForgejoMergePRResponse", StatusCodes: []int{200, 405}},
	}
}

// =============================================================================
// Chimera API Contracts (§15.2)
// =============================================================================

// ChimeraDeliberateRequest per spec §15.2.
type ChimeraDeliberateRequest struct {
	Prompt    string `json:"prompt"`
	Formation string `json:"formation"`
}

// ChimeraDeliberateResponse per spec §15.2.
type ChimeraDeliberateResponse struct {
	DeliberationID string         `json:"deliberation_id"`
	Formation      string         `json:"formation"`
	Score          float64        `json:"score"`
	Status         string         `json:"status"`
	Judges         []ChimeraJudge `json:"judges"`
	Audit          ChimeraAudit   `json:"audit"`
	TraceID        string         `json:"trace_id"`
}

// ChimeraJudge is a single judge's verdict in a Chimera deliberation.
type ChimeraJudge struct {
	Model  string  `json:"model"`
	Domain string  `json:"domain"`
	Score  float64 `json:"score"`
}

// ChimeraAudit is the audit model's verification.
type ChimeraAudit struct {
	Model     string  `json:"model"`
	Agreement float64 `json:"agreement"`
	Flagged   bool    `json:"flagged"`
}

// ChimeraFormation per spec §15.2 List Formations.
type ChimeraFormation struct {
	Name    string `json:"name"`
	Models  int    `json:"models"`
	Timeout int    `json:"timeout"`
}

// ChimeraListFormationsResponse per spec §15.2.
type ChimeraListFormationsResponse struct {
	Formations []ChimeraFormation `json:"formations"`
}

// ChimeraHealthResponse per spec §15.2.
type ChimeraHealthResponse struct {
	Status          string `json:"status"`
	ModelsAvailable int    `json:"models_available"`
}

// ChimeraEndpoints returns all Chimera API endpoints.
func ChimeraEndpoints() []EndpointDef {
	return []EndpointDef{
		{Service: ServiceChimera, Method: MethodPost, Path: "/deliberate", Name: "Run Deliberation", ResponseType: "ChimeraDeliberateResponse", StatusCodes: []int{200, 400, 429, 500, 504}},
		{Service: ServiceChimera, Method: MethodGet, Path: "/formations", Name: "List Formations", ResponseType: "ChimeraListFormationsResponse", StatusCodes: []int{200}},
		{Service: ServiceChimera, Method: MethodGet, Path: "/health", Name: "Health Check", ResponseType: "ChimeraHealthResponse", StatusCodes: []int{200, 503}},
	}
}

// =============================================================================
// Conscientiousness API Contracts (§15.3)
// =============================================================================

// ConscientiousnessEvaluateRequest per spec §15.3.
type ConscientiousnessEvaluateRequest struct {
	PRDiff            string                     `json:"pr_diff"`
	PRContext         ConscientiousnessPRContext `json:"pr_context"`
	MaxIterations     int                        `json:"max_iterations"`
	AdversarialAgents []string                   `json:"adversarial_agents"`
}

// ConscientiousnessPRContext is the PR metadata for evaluation.
type ConscientiousnessPRContext struct {
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Author string `json:"author"`
}

// ConscientiousnessEvaluateResponse per spec §15.3.
type ConscientiousnessEvaluateResponse struct {
	EvaluationID string                     `json:"evaluation_id"`
	Iterations   int                        `json:"iterations"`
	Status       string                     `json:"status"`
	Findings     []ConscientiousnessFinding `json:"findings"`
	Blocked      bool                       `json:"blocked"`
}

// ConscientiousnessFinding is a single adversarial finding.
type ConscientiousnessFinding struct {
	Severity string `json:"severity"`
	Agent    string `json:"agent"`
	Finding  string `json:"finding"`
	Resolved bool   `json:"resolved"`
}

// ConscientiousnessEndpoints returns all Conscientiousness API endpoints.
func ConscientiousnessEndpoints() []EndpointDef {
	return []EndpointDef{
		{Service: ServiceConscientiousness, Method: MethodPost, Path: "/evaluate", Name: "Evaluate PR", ResponseType: "ConscientiousnessEvaluateResponse", StatusCodes: []int{200}},
		{Service: ServiceConscientiousness, Method: MethodGet, Path: "/report/{evaluation_id}", Name: "Get Report", StatusCodes: []int{200}},
		{Service: ServiceConscientiousness, Method: MethodGet, Path: "/health", Name: "Health Check", StatusCodes: []int{200}},
	}
}

// =============================================================================
// Hivemind API Contracts (§15.4)
// =============================================================================

// HivemindWriteMemoryRequest per spec §15.4.
type HivemindWriteMemoryRequest struct {
	AgentID    string   `json:"agent_id"`
	Repo       string   `json:"repo"`
	EventType  string   `json:"event_type"`
	Summary    string   `json:"summary"`
	Resolution string   `json:"resolution"`
	Tags       []string `json:"tags"`
}

// HivemindWriteMemoryResponse per spec §15.4.
type HivemindWriteMemoryResponse struct {
	MemoryID  string `json:"memory_id"`
	Persisted bool   `json:"persisted"`
}

// HivemindAssignTaskRequest per spec §15.4.
type HivemindAssignTaskRequest struct {
	AgentID string             `json:"agent_id"`
	Repo    string             `json:"repo"`
	Task    HivemindTaskDetail `json:"task"`
}

// HivemindTaskDetail describes a task to assign.
type HivemindTaskDetail struct {
	Type      string `json:"type"`
	PRNumber  int    `json:"pr_number"`
	PromptRef string `json:"prompt_ref"`
}

// HivemindAssignTaskResponse per spec §15.4.
type HivemindAssignTaskResponse struct {
	TaskID  string `json:"task_id"`
	Status  string `json:"status"`
	AgentID string `json:"agent_id"`
}

// HivemindEndpoints returns all Hivemind API endpoints.
func HivemindEndpoints() []EndpointDef {
	return []EndpointDef{
		{Service: ServiceHivemind, Method: MethodPost, Path: "/memory/write", Name: "Write Memory", ResponseType: "HivemindWriteMemoryResponse", StatusCodes: []int{201}},
		{Service: ServiceHivemind, Method: MethodGet, Path: "/memory/query", Name: "Query Memory", StatusCodes: []int{200}},
		{Service: ServiceHivemind, Method: MethodGet, Path: "/agents", Name: "List Agents", StatusCodes: []int{200}},
		{Service: ServiceHivemind, Method: MethodPost, Path: "/tasks/assign", Name: "Assign Task", ResponseType: "HivemindAssignTaskResponse", StatusCodes: []int{200, 409, 402}},
		{Service: ServiceHivemind, Method: MethodGet, Path: "/health", Name: "Health Check", StatusCodes: []int{200}},
	}
}

// =============================================================================
// Muster API Contracts (§15.5)
// =============================================================================

// MusterGenerateRequest per spec §15.5.
type MusterGenerateRequest struct {
	OpenAPISpecURL   string `json:"openapi_spec_url"`
	OutputFormat     string `json:"output_format"`
	CacheTTLOseconds int    `json:"cache_ttl_seconds"`
}

// MusterGenerateResponse per spec §15.5.
type MusterGenerateResponse struct {
	Tools     []MusterTool `json:"tools"`
	ToolCount int          `json:"tool_count"`
	Cached    bool         `json:"cached"`
}

// MusterTool is a generated MCP tool.
type MusterTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Endpoint    string `json:"endpoint"`
}

// MusterEndpoints returns all Muster API endpoints.
func MusterEndpoints() []EndpointDef {
	return []EndpointDef{
		{Service: ServiceMuster, Method: MethodPost, Path: "/generate", Name: "Generate MCP Tools", ResponseType: "MusterGenerateResponse", StatusCodes: []int{200}},
		{Service: ServiceMuster, Method: MethodGet, Path: "/specs", Name: "List Specifications", StatusCodes: []int{200}},
		{Service: ServiceMuster, Method: MethodGet, Path: "/health", Name: "Health Check", StatusCodes: []int{200}},
	}
}

// =============================================================================
// Contract Validator
// =============================================================================

// ValidationError is a single validation failure.
type ValidationError struct {
	Service  ServiceID
	Endpoint string
	Field    string
	Message  string
}

// Error implements the error interface.
func (e ValidationError) Error() string {
	return fmt.Sprintf("[%s/%s] %s: %s", e.Service, e.Endpoint, e.Field, e.Message)
}

// ContractValidator validates API request/response objects against spec §15.
type ContractValidator struct {
	errors []ValidationError
}

// NewContractValidator creates a new validator.
func NewContractValidator() *ContractValidator {
	return &ContractValidator{}
}

// Errors returns all accumulated validation errors.
func (v *ContractValidator) Errors() []ValidationError {
	return v.errors
}

// HasErrors returns true if any validation errors were recorded.
func (v *ContractValidator) HasErrors() bool {
	return len(v.errors) > 0
}

// ValidateFromJSON validates a JSON body against the given service endpoint.
// It dispatches to the appropriate typed validator based on the service and
// endpoint name. This is the public entry point for external callers (e.g., CLI).
func (v *ContractValidator) ValidateFromJSON(svc ServiceID, endpointName string, body []byte) {
	validateEndpointDispatch(v, svc, endpointName, body)
}

// AllEndpoints returns all endpoints across all services.
func AllEndpoints() []EndpointDef {
	var all []EndpointDef
	all = append(all, ForgejoEndpoints()...)
	all = append(all, ChimeraEndpoints()...)
	all = append(all, ConscientiousnessEndpoints()...)
	all = append(all, HivemindEndpoints()...)
	all = append(all, MusterEndpoints()...)
	return all
}

// EndpointsForService returns all endpoints for a specific service.
func EndpointsForService(svc ServiceID) []EndpointDef {
	switch svc {
	case ServiceForgejo:
		return ForgejoEndpoints()
	case ServiceChimera:
		return ChimeraEndpoints()
	case ServiceConscientiousness:
		return ConscientiousnessEndpoints()
	case ServiceHivemind:
		return HivemindEndpoints()
	case ServiceMuster:
		return MusterEndpoints()
	default:
		return nil
	}
}

// =============================================================================
// Request Validation
// =============================================================================

// ValidateForgejoCreateUser validates a Forgejo Create User request.
func (v *ContractValidator) ValidateForgejoCreateUser(req ForgejoCreateUserRequest) {
	if req.Username == "" {
		v.errors = append(v.errors, ValidationError{Service: ServiceForgejo, Endpoint: "Create User", Field: "username", Message: "must not be empty"})
	}
	if req.Email == "" {
		v.errors = append(v.errors, ValidationError{Service: ServiceForgejo, Endpoint: "Create User", Field: "email", Message: "must not be empty"})
	}
	if req.Password == "" {
		v.errors = append(v.errors, ValidationError{Service: ServiceForgejo, Endpoint: "Create User", Field: "password", Message: "must not be empty"})
	}
	if len(req.Password) < 8 {
		v.errors = append(v.errors, ValidationError{Service: ServiceForgejo, Endpoint: "Create User", Field: "password", Message: "must be at least 8 characters"})
	}
}

// ValidateForgejoCreateSSHKey validates a Forgejo Create SSH Key request.
func (v *ContractValidator) ValidateForgejoCreateSSHKey(req ForgejoCreateSSHKeyRequest) {
	if req.Key == "" {
		v.errors = append(v.errors, ValidationError{Service: ServiceForgejo, Endpoint: "Create SSH Key", Field: "key", Message: "must not be empty"})
	}
	if !strings.HasPrefix(req.Key, "ssh-") {
		v.errors = append(v.errors, ValidationError{Service: ServiceForgejo, Endpoint: "Create SSH Key", Field: "key", Message: "must start with ssh- prefix"})
	}
	if req.Title == "" {
		v.errors = append(v.errors, ValidationError{Service: ServiceForgejo, Endpoint: "Create SSH Key", Field: "title", Message: "must not be empty"})
	}
}

// ValidateForgejoCreatePAT validates a Forgejo Create PAT request.
func (v *ContractValidator) ValidateForgejoCreatePAT(req ForgejoCreatePATRequest) {
	if req.Name == "" {
		v.errors = append(v.errors, ValidationError{Service: ServiceForgejo, Endpoint: "Create PAT", Field: "name", Message: "must not be empty"})
	}
	if len(req.Scopes) == 0 {
		v.errors = append(v.errors, ValidationError{Service: ServiceForgejo, Endpoint: "Create PAT", Field: "scopes", Message: "must have at least one scope"})
	}
}

// ValidateForgejoMergePR validates a Forgejo Merge PR request.
func (v *ContractValidator) ValidateForgejoMergePR(req ForgejoMergePRRequest) {
	if req.Do == "" {
		v.errors = append(v.errors, ValidationError{Service: ServiceForgejo, Endpoint: "Merge PR", Field: "do", Message: "must not be empty"})
	}
	if req.Do != "merge" && req.Do != "rebase" && req.Do != "squash" {
		v.errors = append(v.errors, ValidationError{Service: ServiceForgejo, Endpoint: "Merge PR", Field: "do", Message: fmt.Sprintf("must be merge, rebase, or squash (got %q)", req.Do)})
	}
}

// ValidateChimeraDeliberate validates a Chimera Run Deliberation request.
func (v *ContractValidator) ValidateChimeraDeliberate(req ChimeraDeliberateRequest) {
	if req.Prompt == "" {
		v.errors = append(v.errors, ValidationError{Service: ServiceChimera, Endpoint: "Run Deliberation", Field: "prompt", Message: "must not be empty"})
	}
	if req.Formation == "" {
		v.errors = append(v.errors, ValidationError{Service: ServiceChimera, Endpoint: "Run Deliberation", Field: "formation", Message: "must not be empty"})
	}
}

// ValidateConscientiousnessEvaluate validates a Conscientiousness Evaluate request.
func (v *ContractValidator) ValidateConscientiousnessEvaluate(req ConscientiousnessEvaluateRequest) {
	if req.PRDiff == "" {
		v.errors = append(v.errors, ValidationError{Service: ServiceConscientiousness, Endpoint: "Evaluate PR", Field: "pr_diff", Message: "must not be empty"})
	}
	if req.PRContext.Repo == "" {
		v.errors = append(v.errors, ValidationError{Service: ServiceConscientiousness, Endpoint: "Evaluate PR", Field: "pr_context.repo", Message: "must not be empty"})
	}
	if req.PRContext.Number == 0 {
		v.errors = append(v.errors, ValidationError{Service: ServiceConscientiousness, Endpoint: "Evaluate PR", Field: "pr_context.number", Message: "must not be zero"})
	}
	if len(req.AdversarialAgents) == 0 {
		v.errors = append(v.errors, ValidationError{Service: ServiceConscientiousness, Endpoint: "Evaluate PR", Field: "adversarial_agents", Message: "must have at least one agent"})
	}
}

// ValidateHivemindWriteMemory validates a Hivemind Write Memory request.
func (v *ContractValidator) ValidateHivemindWriteMemory(req HivemindWriteMemoryRequest) {
	if req.AgentID == "" {
		v.errors = append(v.errors, ValidationError{Service: ServiceHivemind, Endpoint: "Write Memory", Field: "agent_id", Message: "must not be empty"})
	}
	if req.Repo == "" {
		v.errors = append(v.errors, ValidationError{Service: ServiceHivemind, Endpoint: "Write Memory", Field: "repo", Message: "must not be empty"})
	}
	if req.EventType == "" {
		v.errors = append(v.errors, ValidationError{Service: ServiceHivemind, Endpoint: "Write Memory", Field: "event_type", Message: "must not be empty"})
	}
	if req.Summary == "" {
		v.errors = append(v.errors, ValidationError{Service: ServiceHivemind, Endpoint: "Write Memory", Field: "summary", Message: "must not be empty"})
	}
}

// ValidateHivemindAssignTask validates a Hivemind Assign Task request.
func (v *ContractValidator) ValidateHivemindAssignTask(req HivemindAssignTaskRequest) {
	if req.AgentID == "" {
		v.errors = append(v.errors, ValidationError{Service: ServiceHivemind, Endpoint: "Assign Task", Field: "agent_id", Message: "must not be empty"})
	}
	if req.Repo == "" {
		v.errors = append(v.errors, ValidationError{Service: ServiceHivemind, Endpoint: "Assign Task", Field: "repo", Message: "must not be empty"})
	}
	if req.Task.Type == "" {
		v.errors = append(v.errors, ValidationError{Service: ServiceHivemind, Endpoint: "Assign Task", Field: "task.type", Message: "must not be empty"})
	}
}

// ValidateMusterGenerate validates a Muster Generate MCP Tools request.
func (v *ContractValidator) ValidateMusterGenerate(req MusterGenerateRequest) {
	if req.OpenAPISpecURL == "" {
		v.errors = append(v.errors, ValidationError{Service: ServiceMuster, Endpoint: "Generate MCP Tools", Field: "openapi_spec_url", Message: "must not be empty"})
	}
	if !strings.HasPrefix(req.OpenAPISpecURL, "http") {
		v.errors = append(v.errors, ValidationError{Service: ServiceMuster, Endpoint: "Generate MCP Tools", Field: "openapi_spec_url", Message: "must be a valid URL (start with http)"})
	}
	if req.OutputFormat == "" {
		v.errors = append(v.errors, ValidationError{Service: ServiceMuster, Endpoint: "Generate MCP Tools", Field: "output_format", Message: "must not be empty"})
	}
}

// =============================================================================
// Response Validation
// =============================================================================

// ValidateForgejoCreateUserResponse validates a Forgejo Create User response.
func (v *ContractValidator) ValidateForgejoCreateUserResponse(resp ForgejoCreateUserResponse) {
	if resp.ID == 0 {
		v.errors = append(v.errors, ValidationError{Service: ServiceForgejo, Endpoint: "Create User", Field: "id", Message: "must not be zero in response"})
	}
	if resp.Username == "" {
		v.errors = append(v.errors, ValidationError{Service: ServiceForgejo, Endpoint: "Create User", Field: "username", Message: "must not be empty in response"})
	}
}

// ValidateChimeraDeliberateResponse validates a Chimera Run Deliberation response.
func (v *ContractValidator) ValidateChimeraDeliberateResponse(resp ChimeraDeliberateResponse) {
	if resp.DeliberationID == "" {
		v.errors = append(v.errors, ValidationError{Service: ServiceChimera, Endpoint: "Run Deliberation", Field: "deliberation_id", Message: "must not be empty in response"})
	}
	if len(resp.Judges) < 3 {
		v.errors = append(v.errors, ValidationError{Service: ServiceChimera, Endpoint: "Run Deliberation", Field: "judges", Message: "must have at least 3 judges (spec requirement)"})
	}
	if resp.Score < 0 || resp.Score > 1 {
		v.errors = append(v.errors, ValidationError{Service: ServiceChimera, Endpoint: "Run Deliberation", Field: "score", Message: fmt.Sprintf("must be 0-1 (got %.2f)", resp.Score)})
	}
	if resp.Status == "" {
		v.errors = append(v.errors, ValidationError{Service: ServiceChimera, Endpoint: "Run Deliberation", Field: "status", Message: "must not be empty in response"})
	}
}

// =============================================================================
// HTTP Status Code Validation
// =============================================================================

// IsValidStatusCode checks if a response status code is in the endpoint's expected codes.
func IsValidStatusCode(endpoint EndpointDef, statusCode int) bool {
	for _, code := range endpoint.StatusCodes {
		if code == statusCode {
			return true
		}
	}
	return false
}

// =============================================================================
// JSON Serialization Helpers
// =============================================================================

// MarshalRequest serializes a request body to JSON.
func MarshalRequest(req interface{}) ([]byte, error) {
	return json.Marshal(req)
}

// UnmarshalResponse deserializes a response body from JSON.
func UnmarshalResponse(body []byte, resp interface{}) error {
	return json.Unmarshal(body, resp)
}

// =============================================================================
// HTTP Request Builder
// =============================================================================

// BuildRequest constructs an *http.Request for a given endpoint.
func BuildRequest(svc ServiceInfo, endpoint EndpointDef, body []byte, apiKey string) (*http.Request, error) {
	url := svc.BaseURL + endpoint.Path
	var req *http.Request
	var err error

	if body != nil {
		req, err = http.NewRequest(string(endpoint.Method), url, strings.NewReader(string(body)))
	} else {
		req, err = http.NewRequest(string(endpoint.Method), url, nil)
	}
	if err != nil {
		return nil, err
	}

	// Auth header
	if apiKey != "" {
		if svc.AuthScheme != "" {
			req.Header.Set(svc.AuthHeader, svc.AuthScheme+" "+apiKey)
		} else {
			req.Header.Set(svc.AuthHeader, apiKey)
		}
	}

	// Content-Type for POST/PUT
	if endpoint.Method == MethodPost || endpoint.Method == MethodPut {
		req.Header.Set("Content-Type", "application/json")
	}

	return req, nil
}
