# pkg/learning — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/learning"`

Cross-agent notification and context sharing with domain pub/sub

## Signatures (from `go doc`)

```go
package learning // import "github.com/totalwindupflightsystems/helix/pkg/learning"

Package learning implements the Helix Phase 12 learning and knowledge transfer
subsystem — cross-agent notification bus, pattern discovery, and skill
marketplace.

§12.3 — Cross-Agent Notification Bus: agents publish findings to domain-scoped
topics, subscribe to domains relevant to their active tasks, and receive
budget-tracked notifications so knowledge transfers between concurrent agents
without flooding context windows.

Package learning implements the Helix Phase 12 learning and knowledge transfer
subsystem. This file (§12.2) provides the Skill Registry — a marketplace where
high-trust agents publish reusable skills and other agents load them during
context assembly. Skill effectiveness is tracked and ineffective skills lose
trust weighting.

const FindingTokenCost = 500 ...
const HypothesisThreshold = 0.4 ...
const FPRateRemovalThreshold = 0.15 ...
const MinPublishTrust = 0.65 ...
const DefaultFindingRetention = 30 * 24 * time.Hour
var AllDomains = []Domain{ ... }
func DailyBudget(tier string) int
func DefaultSkillsPath() string
func DefaultStoreDir() string
func IsValidDomain(d Domain) bool
func NewFindingID() (string, error)
func NewSkillID() (string, error)
func SelectionScoreWithMetrics(m ModelMetrics, trustScore float64, fleetMaxCost float64) float64
type ContextBus struct{ ... }
    func NewContextBus(dir string) (*ContextBus, error)
type DiscoveredPattern struct{ ... }
type Domain string
    const DomainAuth Domain = "auth" ...
type FileSkillStore struct{ ... }
    func NewFileSkillStore(path string) (*FileSkillStore, error)
type IncidentDataSource interface{ ... }
type IncidentSliceSource struct{ ... }
    func NewIncidentSliceSource(incidents []*incident.Incident) *IncidentSliceSource
type ModelEvaluator struct{ ... }
    func NewModelEvaluator() *ModelEvaluator
type ModelMetrics struct{ ... }
type PatternDataSource interface{ ... }
type PatternMiner struct{ ... }
    func NewPatternMiner(incidents IncidentDataSource, patterns PatternDataSource) *PatternMiner
type PatternSliceSource struct{ ... }
    func NewPatternSliceSource(patterns []*incident.IncidentPattern) *PatternSliceSource
type PatternType string
    const PatternCategoryClustering PatternType = "category_clustering" ...
type Priority string
    const PriorityInfo Priority = "info" ...
type PublishGateError struct{ ... }
type RotationEvent struct{ ... }
type RotationEventType string
    const RotationEventRemoved RotationEventType = "removed" ...
type SharedFinding struct{ ... }
type Skill struct{ ... }
type SkillRegistry struct{ ... }
    func NewSkillRegistry(store SkillStore) *SkillRegistry
type SkillStore interface{ ... }
type Subscription struct{ ... }
```

## Related

- [docs/api/README.md](README.md) — package index
