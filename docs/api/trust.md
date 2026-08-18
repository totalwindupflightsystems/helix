# pkg/trust — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/trust"`

Graduated multi-dimensional trust with tier assignment and decay

## Signatures (from `go doc`)

```go
package trust // import "github.com/totalwindupflightsystems/helix/pkg/trust"

Package trust implements a graduated, multi-dimensional trust scoring system for
AI agents per specs/trust-model.md.

Trust is a 0.0–1.0 float64 calculated from six weighted dimensions. Every trust
event is recorded in an append-only JSONL ledger whose replay is deterministic —
any observer can independently verify an agent's score.

const EventMergeSuccess = "merge_success" ...
const TrendUp = "up" ...
const DaysPerWeek = 7
const EventCompactionSummary = "compaction_summary"
const InactivityDecayRate = 0.05
const InactivityGraceDays = 30
const SnapshotWindow30Days = 30 * 24 * time.Hour
var DimensionWeights = map[string]float64{ ... }
var TierOrdinal = map[TrustTier]int{ ... }
func CompareTiers(a, b TrustTier) int
func EvaluateFullTierCycle(metrics AgentMetrics) (TrustTier, *PromotionResult, bool)
func GetRecoveryBatch(ledgerPath string, agentIDs []string) (map[string]*RecoverySnapshot, error)
func IncidentAttributionWeight(daysSince float64) float64
func IncidentTrackScore(mergesSinceLastIncident int) float64
func IsDemotion(from, to TrustTier) bool
func IsPromotion(from, to TrustTier) bool
func MergeSuccessScore(merged, reverted int) float64
func NeedsCompaction(path string, config CompactionConfig) (bool, error)
func ShouldDemote(currentTier TrustTier, currentScore float64, consecutiveDaysBelow int) bool
func ShouldPromote(metrics AgentMetrics) bool
func TenureScore(daysSinceFirstContribution int) float64
func TierRank(tier TrustTier) int
func VerifyCompaction(originalPath, compactedPath string) error
type AgentMetrics struct{ ... }
type AgentPenalty struct{ ... }
type Anomaly struct{ ... }
type AnomalyType string
    const AnomalyScoreDrift AnomalyType = "score_drift" ...
type AuditFinding struct{ ... }
type AuditFindingStatus string
    const AuditPass AuditFindingStatus = "PASS" ...
type AuditOption func(*TrustAuditRunner)
    func WithMaxInactivityDays(d int) AuditOption
    func WithScoreTolerance(t float64) AuditOption
type AuditReport struct{ ... }
type AuditSummary struct{ ... }
type BatchIncident struct{ ... }
type CompactionConfig struct{ ... }
    func DefaultCompactionConfig() CompactionConfig
type CompactionResult struct{ ... }
type CompactionSummary struct{ ... }
type CriterionResult struct{ ... }
type DimensionContribution struct{ ... }
type DimensionScores struct{ ... }
type EventData struct{ ... }
type IncidentBridge struct{ ... }
    func NewIncidentBridge(ledger *Ledger) *IncidentBridge
    func NewIncidentBridgeWithFile(ledgerPath string) (*IncidentBridge, error)
type Ledger struct{ ... }
    func NewLedger(path string) (*Ledger, error)
type LedgerCompactor struct{ ... }
    func NewLedgerCompactor(config CompactionConfig) *LedgerCompactor
type LedgerStats struct{ ... }
    func GetStats(path string) (*LedgerStats, error)
type ProcessSummary struct{ ... }
type PromotionResult struct{ ... }
    func EvaluatePromotion(metrics AgentMetrics, targetTier TrustTier) *PromotionResult
type RecoveryConfig struct{ ... }
    func DefaultRecoveryConfig() RecoveryConfig
type RecoverySnapshot struct{ ... }
    func GetRecoverySnapshot(ledgerPath, agentID string) (*RecoverySnapshot, error)
    func GetRecoverySnapshotWithConfig(ledgerPath, agentID string, cfg RecoveryConfig) (*RecoverySnapshot, error)
type ScoreBreakdown struct{ ... }
    func GetScoreBreakdown(ledgerPath, agentID string) (ScoreBreakdown, error)
type ScoreTrend struct{ ... }
    func ScoreTrendOver(ledgerPath, agentID string, window time.Duration) (ScoreTrend, error)
type TierThresholds struct{ ... }
type TierTransition struct{ ... }
    func GetTierHistory(ledgerPath, agentID string) ([]TierTransition, error)
type TrustAuditRunner struct{ ... }
    func NewTrustAuditRunner(ledgerPath string, opts ...AuditOption) *TrustAuditRunner
type TrustEvent struct{ ... }
    func GetRecentEvents(ledgerPath, agentID string, days int) ([]TrustEvent, error)
    func Replay(path string) ([]TrustEvent, error)
type TrustScore float64
    func ApplyInactivityDecay(current TrustScore, daysInactive int) TrustScore
    func ApplyIncidentPenalty(current TrustScore, attributionWeight float64) TrustScore
    func Calculate(dims DimensionScores) TrustScore
    func ReplayToScore(path, agentID string) (TrustScore, error)
type TrustSnapshot struct{ ... }
    func GetSnapshot(ledgerPath, agentID string) (*TrustSnapshot, error)
type TrustTier string
    const TierProvisional TrustTier = "provisional" ...
    func AllTiers() []TrustTier
    func DemoteTo(currentTier TrustTier) TrustTier
    func DetermineTier(score float64, totalMerges int, incidents180d int) TrustTier
    func PromoteTo(metrics AgentMetrics) TrustTier
```

## Related

- [docs/api/README.md](README.md) — package index
