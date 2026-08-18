# pkg/ci — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/ci"`

Forgejo Actions workflow generation and validation

## Signatures (from `go doc`)

```go
package ci // import "github.com/totalwindupflightsystems/helix/pkg/ci"

Package ci provides a Go API for generating and validating Forgejo Actions
workflow YAML, specifically the test CI pipeline defined in spec §12.5.

The materialised workflow (.forgejo/workflows/test.yml) is the canonical record;
this package allows it to be generated programmatically and round-tripped
against the spec example for CI compliance verification.

const DefaultWorkflowName = "Test" ...
func DefaultBranches() []string
func DefaultPRTypes() []string
type TestJob struct{ ... }
type TestService struct{ ... }
type TestStep struct{ ... }
type TestTrigger struct{ ... }
type TestTriggerRule struct{ ... }
type TestWorkflow struct{ ... }
    func DefaultTestWorkflow() *TestWorkflow
    func Parse(data []byte) (*TestWorkflow, error)
```

## Related

- [docs/api/README.md](README.md) — package index
