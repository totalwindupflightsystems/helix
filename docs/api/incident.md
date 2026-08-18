# pkg/incident — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/incident"`

Incident learning database with attribution engine

## Signatures (from `go doc`)

```go
package incident // import "github.com/totalwindupflightsystems/helix/pkg/incident"

Package incident implements the incident learning database schema and store for
tracking AI-agent-caused production incidents and feeding them into the trust
penalty pipeline.

const SeverityLow = "low" ...
func MergeAttribution(results []*AttributionResult) map[string]float64
func TrustPenalty(attributionShare float64, severity string) float64
type AttributionEngine struct{ ... }
    func NewAttributionEngine(store *Store) *AttributionEngine
type AttributionResult struct{ ... }
type AttributionSummary struct{ ... }
type AttributionWeights struct{ ... }
    func DefaultAttributionWeights() AttributionWeights
type ChangePath struct{ ... }
    func FindResponsiblePaths(paths []ChangePath, causalChain []string) []ChangePath
type ChangeType string
    const ChangeNew ChangeType = "new" ...
type FileCategory string
    const CategoryAuth FileCategory = "auth" ...
    func CategorizeFile(path string) FileCategory
    func CategorizeFiles(paths []string) []FileCategory
type Incident struct{ ... }
type IncidentPattern struct{ ... }
type LearningDatabase struct{ ... }
    func NewLearningDatabase() *LearningDatabase
type PRContext struct{ ... }
type ReviewContextItem struct{ ... }
type ReviewContextReport struct{ ... }
type Store struct{ ... }
    func NewStore() *Store
type TrustPenaltyCallback func(agentID string, penalty float64, evidence []string) error
```

## Related

- [docs/api/README.md](README.md) — package index
