# pkg/spec — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/spec"`

Multi-agent spec creation with adversarial annotation, 12-dim completeness

## Signatures (from `go doc`)

```go
package spec // import "github.com/totalwindupflightsystems/helix/pkg/spec"

Package spec implements Helix spec co-authoring with adversarial annotation
(Phase 2 §2.1). A SpecCoAuthor dispatches two deterministic rule-based agent
personas — @spec-generator and @spec-challenger — that annotate a spec
with edge cases, failure modes, consistency issues, and missing coverage.
A SpecCompleteness scores the spec across 12 dimensions.

const StatusDraft = "draft" ...
const AnnEdgeCase = "edge_case" ...
const ApprovalPending = "pending" ...
const AnnotationProposed = "proposed" ...
const SeverityInfo = "info" ...
const AgentSpecGenerator = "spec-generator" ...
const DefaultSpecsDir = ".helix/specs"
func NewSpecID() string
type CompletenessGap struct{ ... }
type CompletenessReport struct{ ... }
type DimensionScore struct{ ... }
type Spec struct{ ... }
type SpecAnnotation struct{ ... }
type SpecCoAuthor struct{}
    func NewSpecCoAuthor() *SpecCoAuthor
type SpecCompleteness struct{}
    func NewSpecCompleteness() *SpecCompleteness
type SpecSection struct{ ... }
type SpecStore struct{ ... }
    func NewSpecStore(root string) (*SpecStore, error)
```

## Related

- [docs/api/README.md](README.md) — package index
