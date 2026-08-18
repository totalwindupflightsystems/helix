# pkg/contract — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/contract"`

OpenAPI/protobuf generation, validation, breaking change detection

## Signatures (from `go doc`)

```go
package contract // import "github.com/totalwindupflightsystems/helix/pkg/contract"

Package contract implements API contract generation, validation, breaking change
detection, and immutable storage (Phase 2 §2.4).

const DefaultContractsDir = ".helix/contracts" ...
func ComputeHash(c *Contract) string
func Freeze(c *Contract) error
func ValidFormat(f ContractFormat) bool
type BreakingChange struct{ ... }
    func DetectChanges(new, old *Contract) []BreakingChange
type ConsumerImpact struct{ ... }
    func ConsumerImpactReport(changes []BreakingChange, consumerCatalog map[string][]string) []ConsumerImpact
type Contract struct{ ... }
type ContractAuthor struct{ ... }
    func NewContractAuthor(specDir string) (*ContractAuthor, error)
type ContractFormat string
    const FormatOpenAPI ContractFormat = "openapi" ...
type ContractStore struct{ ... }
    func NewContractStore(root string) (*ContractStore, error)
type ContractValidator struct{}
    func NewContractValidator() *ContractValidator
type SpecMeta struct{ ... }
type ValidationReport struct{ ... }
```

## Related

- [docs/api/README.md](README.md) — package index
