# pkg/recovery — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/recovery"`

Structured error recovery procedures per component

## Signatures (from `go doc`)

```go
package recovery // import "github.com/totalwindupflightsystems/helix/pkg/recovery"

Package recovery encodes the Helix error recovery procedures as structured Go
data, enabling programmatic lookup of recovery actions for any component failure
mode.

The data is derived from:
  - specs/SPECIFICATION.md §14 (Error Recovery — Component Failure Matrix)
  - specs/error-recovery.md (per-component recovery procedures)
  - specs/SPECIFICATION.md §10.5 (Incident Response — SEV levels)

const DRHardwareFailure = "dr-hardware-failure" ...
func FormatDRScenario(s DRScenario) string
func FormatMatrix(r *RecoveryRegistry) string
func FormatRunbook(e FailureEntry) string
func KeyRotationSteps() []string
func MaxAgentsForCores(cores int, coresPerAgent float64) int
func SeverityDescription(s Severity) string
type DRRegistry struct{ ... }
    func NewDRRegistry() *DRRegistry
type DRScenario struct{ ... }
    func DefaultDRScenarios() []DRScenario
type FailureEntry struct{ ... }
type RecoveryAction struct{ ... }
type RecoveryRegistry struct{ ... }
    func NewRecoveryRegistry() *RecoveryRegistry
    func NewRecoveryRegistryWithEntries(entries []FailureEntry) *RecoveryRegistry
type RetryConfig struct{ ... }
    func DefaultRetryConfig() RetryConfig
type ScalingModel struct{ ... }
    func DefaultScalingModel() ScalingModel
type Severity string
    const SEV1 Severity = "SEV-1" ...
```

## Related

- [docs/api/README.md](README.md) — package index
