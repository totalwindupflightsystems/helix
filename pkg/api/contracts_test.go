package api

import (
	"strings"
	"testing"
)

// =============================================================================
// Service Info Tests
// =============================================================================

func TestAllServices_Count(t *testing.T) {
	services := AllServices()
	if len(services) != 5 {
		t.Errorf("AllServices() returned %d, want 5", len(services))
	}
}

func TestAllServices_HaveBaseURLs(t *testing.T) {
	for _, svc := range AllServices() {
		if svc.BaseURL == "" {
			t.Errorf("service %q has empty BaseURL", svc.ID)
		}
		if svc.AuthHeader == "" {
			t.Errorf("service %q has empty AuthHeader", svc.ID)
		}
		if svc.HealthPath == "" {
			t.Errorf("service %q has empty HealthPath", svc.ID)
		}
	}
}

func TestAllServices_ForgejoUsesTokenAuth(t *testing.T) {
	var forgejo *ServiceInfo
	for _, svc := range AllServices() {
		if svc.ID == ServiceForgejo {
			forgejo = &svc
			break
		}
	}
	if forgejo == nil {
		t.Fatal("Forgejo service not found")
	}
	if forgejo.AuthHeader != "Authorization" {
		t.Errorf("Forgejo AuthHeader = %q, want Authorization", forgejo.AuthHeader)
	}
	if forgejo.AuthScheme != "token" {
		t.Errorf("Forgejo AuthScheme = %q, want token", forgejo.AuthScheme)
	}
}

// =============================================================================
// Endpoint Tests
// =============================================================================

func TestForgejoEndpoints_Count(t *testing.T) {
	endpoints := ForgejoEndpoints()
	if len(endpoints) != 5 {
		t.Errorf("ForgejoEndpoints() returned %d, want 5", len(endpoints))
	}
}

func TestChimeraEndpoints_Count(t *testing.T) {
	endpoints := ChimeraEndpoints()
	if len(endpoints) != 3 {
		t.Errorf("ChimeraEndpoints() returned %d, want 3", len(endpoints))
	}
}

func TestConscientiousnessEndpoints_Count(t *testing.T) {
	endpoints := ConscientiousnessEndpoints()
	if len(endpoints) != 3 {
		t.Errorf("ConscientiousnessEndpoints() returned %d, want 3", len(endpoints))
	}
}

func TestHivemindEndpoints_Count(t *testing.T) {
	endpoints := HivemindEndpoints()
	if len(endpoints) != 5 {
		t.Errorf("HivemindEndpoints() returned %d, want 5", len(endpoints))
	}
}

func TestMusterEndpoints_Count(t *testing.T) {
	endpoints := MusterEndpoints()
	if len(endpoints) != 3 {
		t.Errorf("MusterEndpoints() returned %d, want 3", len(endpoints))
	}
}

func TestAllEndpoints_Count(t *testing.T) {
	all := AllEndpoints()
	// 5 + 3 + 3 + 5 + 3 = 19
	if len(all) != 19 {
		t.Errorf("AllEndpoints() returned %d, want 19", len(all))
	}
}

func TestEndpointsForService(t *testing.T) {
	tests := []struct {
		svc  ServiceID
		want int
	}{
		{ServiceForgejo, 5},
		{ServiceChimera, 3},
		{ServiceConscientiousness, 3},
		{ServiceHivemind, 5},
		{ServiceMuster, 3},
		{ServiceID("unknown"), 0},
	}
	for _, tt := range tests {
		endpoints := EndpointsForService(tt.svc)
		if len(endpoints) != tt.want {
			t.Errorf("EndpointsForService(%q) = %d, want %d", tt.svc, len(endpoints), tt.want)
		}
	}
}

func TestAllEndpoints_AllHaveNames(t *testing.T) {
	for _, ep := range AllEndpoints() {
		if ep.Name == "" {
			t.Errorf("endpoint %s %s has empty Name", ep.Method, ep.Path)
		}
		if ep.Method == "" {
			t.Errorf("endpoint %q has empty Method", ep.Name)
		}
		if ep.Path == "" {
			t.Errorf("endpoint %q has empty Path", ep.Name)
		}
		if len(ep.StatusCodes) == 0 {
			t.Errorf("endpoint %q has no status codes", ep.Name)
		}
	}
}

// =============================================================================
// Forgejo Request Validation Tests
// =============================================================================

func TestValidateForgejoCreateUser_Valid(t *testing.T) {
	v := NewContractValidator()
	v.ValidateForgejoCreateUser(ForgejoCreateUserRequest{
		Username: "agent-sandbox-7",
		Email:    "agent@example.com",
		Password: "secure-password-123",
	})
	if v.HasErrors() {
		t.Errorf("unexpected errors: %v", v.Errors())
	}
}

func TestValidateForgejoCreateUser_EmptyFields(t *testing.T) {
	v := NewContractValidator()
	v.ValidateForgejoCreateUser(ForgejoCreateUserRequest{})
	if !v.HasErrors() {
		t.Error("HasErrors = false, want true (empty request)")
	}
	errs := v.Errors()
	if len(errs) < 3 {
		t.Errorf("Errors count = %d, want at least 3 (username, email, password)", len(errs))
	}
}

func TestValidateForgejoCreateUser_ShortPassword(t *testing.T) {
	v := NewContractValidator()
	v.ValidateForgejoCreateUser(ForgejoCreateUserRequest{
		Username: "agent-7",
		Email:    "agent@example.com",
		Password: "short",
	})
	if !v.HasErrors() {
		t.Error("HasErrors = false, want true (short password)")
	}
	found := false
	for _, e := range v.Errors() {
		if strings.Contains(e.Message, "at least 8 characters") {
			found = true
		}
	}
	if !found {
		t.Error("missing password length error")
	}
}

func TestValidateForgejoCreateSSHKey_Valid(t *testing.T) {
	v := NewContractValidator()
	v.ValidateForgejoCreateSSHKey(ForgejoCreateSSHKeyRequest{
		Key:   "ssh-ed25519 AAAAC3... agent@example.com",
		Title: "agent-key",
	})
	if v.HasErrors() {
		t.Errorf("unexpected errors: %v", v.Errors())
	}
}

func TestValidateForgejoCreateSSHKey_InvalidPrefix(t *testing.T) {
	v := NewContractValidator()
	v.ValidateForgejoCreateSSHKey(ForgejoCreateSSHKeyRequest{
		Key:   "AAAAC3...",
		Title: "agent-key",
	})
	if !v.HasErrors() {
		t.Error("HasErrors = false, want true (no ssh- prefix)")
	}
}

func TestValidateForgejoCreatePAT_Valid(t *testing.T) {
	v := NewContractValidator()
	v.ValidateForgejoCreatePAT(ForgejoCreatePATRequest{
		Name:   "agent-pat",
		Scopes: []string{"read:repository", "write:repository"},
	})
	if v.HasErrors() {
		t.Errorf("unexpected errors: %v", v.Errors())
	}
}

func TestValidateForgejoCreatePAT_NoScopes(t *testing.T) {
	v := NewContractValidator()
	v.ValidateForgejoCreatePAT(ForgejoCreatePATRequest{
		Name: "agent-pat",
	})
	if !v.HasErrors() {
		t.Error("HasErrors = false, want true (no scopes)")
	}
}

func TestValidateForgejoMergePR_Valid(t *testing.T) {
	v := NewContractValidator()
	v.ValidateForgejoMergePR(ForgejoMergePRRequest{
		Do:          "merge",
		MergeMethod: "squash",
	})
	if v.HasErrors() {
		t.Errorf("unexpected errors: %v", v.Errors())
	}
}

func TestValidateForgejoMergePR_InvalidDo(t *testing.T) {
	v := NewContractValidator()
	v.ValidateForgejoMergePR(ForgejoMergePRRequest{
		Do: "invalid",
	})
	if !v.HasErrors() {
		t.Error("HasErrors = false, want true (invalid do)")
	}
}

// =============================================================================
// Chimera Validation Tests
// =============================================================================

func TestValidateChimeraDeliberate_Valid(t *testing.T) {
	v := NewContractValidator()
	v.ValidateChimeraDeliberate(ChimeraDeliberateRequest{
		Prompt:    "Review this PR diff...",
		Formation: "code-review-standard",
	})
	if v.HasErrors() {
		t.Errorf("unexpected errors: %v", v.Errors())
	}
}

func TestValidateChimeraDeliberate_Empty(t *testing.T) {
	v := NewContractValidator()
	v.ValidateChimeraDeliberate(ChimeraDeliberateRequest{})
	if !v.HasErrors() {
		t.Error("HasErrors = false, want true (empty request)")
	}
}

func TestValidateChimeraDeliberateResponse_Valid(t *testing.T) {
	v := NewContractValidator()
	v.ValidateChimeraDeliberateResponse(ChimeraDeliberateResponse{
		DeliberationID: "del-abc",
		Score:          0.82,
		Status:         "APPROVE",
		Judges: []ChimeraJudge{
			{Model: "claude", Domain: "logic", Score: 0.85},
			{Model: "gemini", Domain: "security", Score: 0.78},
			{Model: "gpt-5.2", Domain: "style", Score: 0.83},
		},
	})
	if v.HasErrors() {
		t.Errorf("unexpected errors: %v", v.Errors())
	}
}

func TestValidateChimeraDeliberateResponse_TooFewJudges(t *testing.T) {
	v := NewContractValidator()
	v.ValidateChimeraDeliberateResponse(ChimeraDeliberateResponse{
		DeliberationID: "del-abc",
		Score:          0.5,
		Status:         "APPROVE",
		Judges:         []ChimeraJudge{{Model: "claude", Domain: "logic", Score: 0.5}},
	})
	if !v.HasErrors() {
		t.Error("HasErrors = false, want true (only 1 judge, need 3+)")
	}
}

func TestValidateChimeraDeliberateResponse_ScoreOutOfRange(t *testing.T) {
	v := NewContractValidator()
	v.ValidateChimeraDeliberateResponse(ChimeraDeliberateResponse{
		DeliberationID: "del-abc",
		Score:          1.5,
		Status:         "APPROVE",
		Judges:         []ChimeraJudge{{}, {}, {}},
	})
	if !v.HasErrors() {
		t.Error("HasErrors = false, want true (score > 1)")
	}
}

// =============================================================================
// Conscientiousness Validation Tests
// =============================================================================

func TestValidateConscientiousnessEvaluate_Valid(t *testing.T) {
	v := NewContractValidator()
	v.ValidateConscientiousnessEvaluate(ConscientiousnessEvaluateRequest{
		PRDiff: "diff --git a/file.go b/file.go...",
		PRContext: ConscientiousnessPRContext{
			Repo:   "org/repo",
			Number: 42,
			Author: "agent-sandbox-7",
		},
		AdversarialAgents: []string{"assumption-buster", "devils-advocate"},
	})
	if v.HasErrors() {
		t.Errorf("unexpected errors: %v", v.Errors())
	}
}

func TestValidateConscientiousnessEvaluate_Empty(t *testing.T) {
	v := NewContractValidator()
	v.ValidateConscientiousnessEvaluate(ConscientiousnessEvaluateRequest{})
	if !v.HasErrors() {
		t.Error("HasErrors = false, want true (empty request)")
	}
}

func TestValidateConscientiousnessEvaluate_NoAgents(t *testing.T) {
	v := NewContractValidator()
	v.ValidateConscientiousnessEvaluate(ConscientiousnessEvaluateRequest{
		PRDiff:            "diff content",
		PRContext:         ConscientiousnessPRContext{Repo: "org/repo", Number: 42},
		AdversarialAgents: []string{},
	})
	if !v.HasErrors() {
		t.Error("HasErrors = false, want true (no adversarial agents)")
	}
}

// =============================================================================
// Hivemind Validation Tests
// =============================================================================

func TestValidateHivemindWriteMemory_Valid(t *testing.T) {
	v := NewContractValidator()
	v.ValidateHivemindWriteMemory(HivemindWriteMemoryRequest{
		AgentID:   "agent-sandbox-7",
		Repo:      "org/repo",
		EventType: "gate_failure",
		Summary:   "Lint failure",
	})
	if v.HasErrors() {
		t.Errorf("unexpected errors: %v", v.Errors())
	}
}

func TestValidateHivemindWriteMemory_Empty(t *testing.T) {
	v := NewContractValidator()
	v.ValidateHivemindWriteMemory(HivemindWriteMemoryRequest{})
	if !v.HasErrors() {
		t.Error("HasErrors = false, want true (empty request)")
	}
}

func TestValidateHivemindAssignTask_Valid(t *testing.T) {
	v := NewContractValidator()
	v.ValidateHivemindAssignTask(HivemindAssignTaskRequest{
		AgentID: "agent-sandbox-7",
		Repo:    "org/repo",
		Task:    HivemindTaskDetail{Type: "implement", PRNumber: 42},
	})
	if v.HasErrors() {
		t.Errorf("unexpected errors: %v", v.Errors())
	}
}

func TestValidateHivemindAssignTask_NoType(t *testing.T) {
	v := NewContractValidator()
	v.ValidateHivemindAssignTask(HivemindAssignTaskRequest{
		AgentID: "agent-sandbox-7",
		Repo:    "org/repo",
	})
	if !v.HasErrors() {
		t.Error("HasErrors = false, want true (no task type)")
	}
}

// =============================================================================
// Muster Validation Tests
// =============================================================================

func TestValidateMusterGenerate_Valid(t *testing.T) {
	v := NewContractValidator()
	v.ValidateMusterGenerate(MusterGenerateRequest{
		OpenAPISpecURL:   "https://example.com/swagger.json",
		OutputFormat:     "mcp",
		CacheTTLOseconds: 3600,
	})
	if v.HasErrors() {
		t.Errorf("unexpected errors: %v", v.Errors())
	}
}

func TestValidateMusterGenerate_InvalidURL(t *testing.T) {
	v := NewContractValidator()
	v.ValidateMusterGenerate(MusterGenerateRequest{
		OpenAPISpecURL: "not-a-url",
		OutputFormat:   "mcp",
	})
	if !v.HasErrors() {
		t.Error("HasErrors = false, want true (invalid URL)")
	}
}

func TestValidateMusterGenerate_Empty(t *testing.T) {
	v := NewContractValidator()
	v.ValidateMusterGenerate(MusterGenerateRequest{})
	if !v.HasErrors() {
		t.Error("HasErrors = false, want true (empty request)")
	}
}

// =============================================================================
// Response Validation Tests
// =============================================================================

func TestValidateForgejoCreateUserResponse_Valid(t *testing.T) {
	v := NewContractValidator()
	v.ValidateForgejoCreateUserResponse(ForgejoCreateUserResponse{
		ID:       42,
		Username: "agent-sandbox-7",
		Email:    "agent@example.com",
	})
	if v.HasErrors() {
		t.Errorf("unexpected errors: %v", v.Errors())
	}
}

func TestValidateForgejoCreateUserResponse_ZeroID(t *testing.T) {
	v := NewContractValidator()
	v.ValidateForgejoCreateUserResponse(ForgejoCreateUserResponse{
		Username: "agent-sandbox-7",
	})
	if !v.HasErrors() {
		t.Error("HasErrors = false, want true (zero ID)")
	}
}

// =============================================================================
// Status Code Validation Tests
// =============================================================================

func TestIsValidStatusCode_Valid(t *testing.T) {
	endpoint := EndpointDef{StatusCodes: []int{200, 400, 403}}
	if !IsValidStatusCode(endpoint, 200) {
		t.Error("IsValidStatusCode(200) = false, want true")
	}
	if !IsValidStatusCode(endpoint, 400) {
		t.Error("IsValidStatusCode(400) = false, want true")
	}
}

func TestIsValidStatusCode_Invalid(t *testing.T) {
	endpoint := EndpointDef{StatusCodes: []int{200, 400}}
	if IsValidStatusCode(endpoint, 500) {
		t.Error("IsValidStatusCode(500) = true, want false")
	}
}

// =============================================================================
// JSON Serialization Tests
// =============================================================================

func TestMarshalRequest_ForgejoCreateUser(t *testing.T) {
	req := ForgejoCreateUserRequest{
		Username: "agent-7",
		Email:    "agent@example.com",
		Password: "password123",
	}
	data, err := MarshalRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"username":"agent-7"`) {
		t.Errorf("missing username in JSON: %s", data)
	}
}

func TestUnmarshalResponse_ForgejoCreateUser(t *testing.T) {
	body := []byte(`{"id":42,"username":"agent-7","email":"agent@example.com","full_name":"Agent 7","login":"agent-7"}`)
	var resp ForgejoCreateUserResponse
	if err := UnmarshalResponse(body, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != 42 {
		t.Errorf("ID = %d, want 42", resp.ID)
	}
	if resp.Username != "agent-7" {
		t.Errorf("Username = %q, want agent-7", resp.Username)
	}
}

func TestUnmarshalResponse_ChimeraDeliberate(t *testing.T) {
	body := []byte(`{
		"deliberation_id": "del-abc",
		"formation": "code-review-standard",
		"score": 0.82,
		"status": "APPROVE",
		"judges": [
			{"model": "claude", "domain": "logic", "score": 0.85},
			{"model": "gemini", "domain": "security", "score": 0.78},
			{"model": "gpt-5.2", "domain": "style", "score": 0.83}
		],
		"audit": {"model": "llama", "agreement": 0.91, "flagged": false},
		"trace_id": "trace-xyz"
	}`)
	var resp ChimeraDeliberateResponse
	if err := UnmarshalResponse(body, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.DeliberationID != "del-abc" {
		t.Errorf("DeliberationID = %q, want del-abc", resp.DeliberationID)
	}
	if len(resp.Judges) != 3 {
		t.Errorf("Judges count = %d, want 3", len(resp.Judges))
	}
	if resp.Audit.Agreement != 0.91 {
		t.Errorf("Audit.Agreement = %f, want 0.91", resp.Audit.Agreement)
	}
}

// =============================================================================
// HTTP Request Builder Tests
// =============================================================================

func TestBuildRequest_ForgejoCreateUser(t *testing.T) {
	svc := ServiceInfo{
		BaseURL:    "https://forgejo.example.com/api/v1",
		AuthHeader: "Authorization",
		AuthScheme: "token",
	}
	endpoint := EndpointDef{Method: MethodPost, Path: "/admin/users"}
	body := []byte(`{"username":"agent-7"}`)
	req, err := BuildRequest(svc, endpoint, body, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != "POST" {
		t.Errorf("Method = %q, want POST", req.Method)
	}
	if req.URL.String() != "https://forgejo.example.com/api/v1/admin/users" {
		t.Errorf("URL = %q, want full URL", req.URL.String())
	}
	authHeader := req.Header.Get("Authorization")
	if authHeader != "token test-token" {
		t.Errorf("Auth header = %q, want 'token test-token'", authHeader)
	}
	ct := req.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestBuildRequest_ChimeraGetNoBody(t *testing.T) {
	svc := ServiceInfo{
		BaseURL:    "https://chimera.example.com",
		AuthHeader: "X-API-Key",
		AuthScheme: "",
	}
	endpoint := EndpointDef{Method: MethodGet, Path: "/formations"}
	req, err := BuildRequest(svc, endpoint, nil, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != "GET" {
		t.Errorf("Method = %q, want GET", req.Method)
	}
	apiKey := req.Header.Get("X-API-Key")
	if apiKey != "test-key" {
		t.Errorf("X-API-Key = %q, want test-key", apiKey)
	}
	// GET should not have Content-Type
	ct := req.Header.Get("Content-Type")
	if ct != "" {
		t.Errorf("Content-Type = %q, want empty for GET", ct)
	}
}

func TestBuildRequest_NoAuth(t *testing.T) {
	svc := ServiceInfo{
		BaseURL:    "https://example.com",
		AuthHeader: "X-API-Key",
	}
	endpoint := EndpointDef{Method: MethodGet, Path: "/health"}
	req, err := BuildRequest(svc, endpoint, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("X-API-Key") != "" {
		t.Error("X-API-Key should be empty when no key provided")
	}
}

// =============================================================================
// ValidationError Tests
// =============================================================================

func TestValidationError_Error(t *testing.T) {
	ve := ValidationError{
		Service:  ServiceForgejo,
		Endpoint: "Create User",
		Field:    "username",
		Message:  "must not be empty",
	}
	msg := ve.Error()
	if !strings.Contains(msg, "forgejo") {
		t.Errorf("Error() missing service: %s", msg)
	}
	if !strings.Contains(msg, "username") {
		t.Errorf("Error() missing field: %s", msg)
	}
}

// =============================================================================
// Accumulated Errors Tests
// =============================================================================

func TestContractValidator_MultipleErrors(t *testing.T) {
	v := NewContractValidator()
	v.ValidateForgejoCreateUser(ForgejoCreateUserRequest{}) // 4 errors (username, email, password empty, password short)
	v.ValidateChimeraDeliberate(ChimeraDeliberateRequest{}) // 2 errors
	if len(v.Errors()) != 6 {
		t.Errorf("Errors() = %d, want 6 (4+2)", len(v.Errors()))
	}
}
