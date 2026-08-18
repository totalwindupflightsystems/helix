# pkg/marketplace — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/marketplace"`

Agent discoverability, trust scoring, human ratings

## Signatures (from `go doc`)

```go
package marketplace // import "github.com/totalwindupflightsystems/helix/pkg/marketplace"

Package marketplace implements the Helix Agent Marketplace — the discoverable
registry where agents are listed, searched, rated, and selected for work items.
See specs/agent-marketplace.md for the full design.

This file defines the core domain types: Capability tags, Agent manifests,
reviews, search queries, and exit codes.

const TrustLowWindowDays = 30 ...
const ExitSuccess = 0 ...
const DefaultSyncInterval = 5 * time.Minute
func AgentsByStatus(r *Registry) map[AgentStatus]int
func CalculateTrustScore(mergedPRs, rejectedPRs, incidents, forceMerges, budgetOverruns int, ...) int
func DailyRecalculation(marketplaceDir string) error
func FormatAgentDetail(a *Agent) string
func FormatAgentTable(agents []*Agent) string
func FormatAgentsByStatus(agents []*Agent) string
func FormatDeprecationNotice(agentName string, trustScore int, daysBelowThreshold int, ...) string
func FormatRatingSubmission(agentName string, rating int, author string, comment string, ...) string
func FormatRegistrySummary(agents []*Agent) string
func FormatSearchResults(results []*Agent, query SearchQuery) string
func FormatTrustDistribution(agents []*Agent) string
func MarkTaskCompleted(a *Agent, now time.Time)
func MarketplaceToScore(marketplaceScore int) float64
func ScoreComparison(a, b *SearchResult) float64
func ScoreToMarketplace(score trust.TrustScore) int
func ShouldReactivate(a *Agent, now time.Time) bool
func TrustLabel(score int) string
func TrustScoreGauges(r *Registry) map[string]int
func UpdateBudgetStatus(a *Agent, now time.Time)
func UpdateTrustHistory(a *Agent, newTrust int, now time.Time)
func VerifyHuman(author string) bool
type Agent struct{ ... }
    func LoadBalance(agents []*Agent, activeTaskCounts map[string]int) []*Agent
type AgentHistory struct{ ... }
type AgentListing struct{ ... }
type AgentProfile struct{ ... }
type AgentStatus string
    const StatusActive AgentStatus = "active" ...
type Budget struct{ ... }
type Capability string
    const CapGo Capability = "go" ...
    func ValidCapability(s string) (Capability, bool)
type CostProfile string
    const CostLow CostProfile = "low" ...
type DeprecationDecision struct{ ... }
    func ShouldAutoDeprecate(a *Agent, now time.Time) DeprecationDecision
type DeprecationReason string
    const ReasonNone DeprecationReason = "" ...
type ExitError struct{ ... }
type Forgejo struct{ ... }
type ManifestIndexEntry struct{ ... }
type MergeStats struct{ ... }
type MetricLine struct{ ... }
type MetricsCollector struct{ ... }
    func NewMetricsCollector() *MetricsCollector
type ModelPreferences struct{ ... }
type Performance struct{ ... }
type Ratings struct{ ... }
type Registry struct{ ... }
    func NewRegistry(marketplaceDir string) (*Registry, error)
type ReputationPoint struct{ ... }
type Review struct{ ... }
type ReviewSummary struct{ ... }
type Scorer struct{ ... }
    func NewScorer() *Scorer
type SearchQuery struct{ ... }
type SearchRanker struct{ ... }
    func NewSearchRanker() *SearchRanker
    func NewSearchRankerWith(opts ...SearchRankerOption) *SearchRanker
type SearchRankerOption func(*SearchRanker)
    func WithSearchWeights(trust, capability, performance, rating, cost float64) SearchRankerOption
type SearchRequirements struct{ ... }
type SearchResult struct{ ... }
type SyncError struct{ ... }
type SyncResult struct{ ... }
type Tier string
    const TierPro Tier = "pro" ...
type TrustSync struct{ ... }
    func NewTrustSync(reg *Registry, ledgerPath string) *TrustSync
```

## Related

- [docs/api/README.md](README.md) — package index
