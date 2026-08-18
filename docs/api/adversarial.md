# pkg/adversarial — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/adversarial"`

Encoded testing scenario pack for adversarial review

## Signatures (from `go doc`)

```go
package adversarial // import "github.com/totalwindupflightsystems/helix/pkg/adversarial"

Package adversarial encodes the Helix adversarial testing scenario pack per
specs/SPECIFICATION.md §12.4. Each scenario describes a specific adversarial
condition an attacker or unintended misuse might trigger, the expected platform
outcome, and a Run function that exercises the actual helix components to verify
the behavior.

Adversarial tests are NOT CI-gated per spec. This package makes them
programmatic so they can run on schedule (daily) or before releases via `helix
adversarial run-all`.

func FormatResult(r Result) string
func FormatResults(results []Result) string
type AgentRole string
    const RoleAssumptionBuster AgentRole = "@assumption-buster" ...
    func AllRoles() []AgentRole
type Library struct{ ... }
    func DefaultLibrary() (*Library, error)
    func NewLibrary() *Library
type Outcome string
    const OutcomeBlocked Outcome = "blocked" ...
type Report struct{ ... }
    func GenerateReport(results []Result) *Report
type Result struct{ ... }
type Scenario struct{ ... }
    func SpecScenarios() []Scenario
type Severity string
    const SevLow Severity = "low" ...
```

## Related

- [docs/api/README.md](README.md) — package index
