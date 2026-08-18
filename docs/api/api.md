# pkg/api — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/api"`

Typed Go structs from spec API contracts

## Signatures (from `go doc`)

```go
package api // import "github.com/totalwindupflightsystems/helix/pkg/api"

Package api encodes the API contracts from spec §15 as typed Go structs
with request/response validation. Each service's API (Forgejo, Chimera,
Conscientiousness, Hivemind, Muster) has its endpoints defined as types with
validation rules matching the spec.

func BuildRequest(svc ServiceInfo, endpoint EndpointDef, body []byte, apiKey string) (*http.Request, error)
func IsValidStatusCode(endpoint EndpointDef, statusCode int) bool
func MarshalRequest(req interface{}) ([]byte, error)
func UnmarshalResponse(body []byte, resp interface{}) error
type ChimeraAudit struct{ ... }
type ChimeraDeliberateRequest struct{ ... }
type ChimeraDeliberateResponse struct{ ... }
type ChimeraFormation struct{ ... }
type ChimeraHealthResponse struct{ ... }
type ChimeraJudge struct{ ... }
type ChimeraListFormationsResponse struct{ ... }
type ConscientiousnessEvaluateRequest struct{ ... }
type ConscientiousnessEvaluateResponse struct{ ... }
type ConscientiousnessFinding struct{ ... }
type ConscientiousnessPRContext struct{ ... }
type ContractServer struct{ ... }
    func NewContractServer() *ContractServer
type ContractValidator struct{ ... }
    func NewContractValidator() *ContractValidator
type EndpointDef struct{ ... }
    func AllEndpoints() []EndpointDef
    func ChimeraEndpoints() []EndpointDef
    func ConscientiousnessEndpoints() []EndpointDef
    func EndpointsForService(svc ServiceID) []EndpointDef
    func ForgejoEndpoints() []EndpointDef
    func HivemindEndpoints() []EndpointDef
    func MusterEndpoints() []EndpointDef
type ForgejoCreatePATRequest struct{ ... }
type ForgejoCreatePATResponse struct{ ... }
type ForgejoCreateSSHKeyRequest struct{ ... }
type ForgejoCreateSSHKeyResponse struct{ ... }
type ForgejoCreateUserRequest struct{ ... }
type ForgejoCreateUserResponse struct{ ... }
type ForgejoGetPRResponse struct{ ... }
type ForgejoMergePRRequest struct{ ... }
type ForgejoMergePRResponse struct{ ... }
type ForgejoPRBranch struct{ ... }
type ForgejoUser struct{ ... }
type HTTPMethod string
    const MethodGet HTTPMethod = "GET" ...
type HivemindAssignTaskRequest struct{ ... }
type HivemindAssignTaskResponse struct{ ... }
type HivemindTaskDetail struct{ ... }
type HivemindWriteMemoryRequest struct{ ... }
type HivemindWriteMemoryResponse struct{ ... }
type MusterGenerateRequest struct{ ... }
type MusterGenerateResponse struct{ ... }
type MusterTool struct{ ... }
type ServiceID string
    const ServiceForgejo ServiceID = "forgejo" ...
type ServiceInfo struct{ ... }
    func AllServices() []ServiceInfo
type ValidationError struct{ ... }
```

## Related

- [docs/api/README.md](README.md) — package index
