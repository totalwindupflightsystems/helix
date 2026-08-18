# pkg/negotiate — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/negotiate"`

Agent debate protocol + Chimera tie-break

## Signatures (from `go doc`)

```go
package negotiate // import "github.com/totalwindupflightsystems/helix/pkg/negotiate"

Package negotiate implements the Helix agent-to-agent PR negotiation protocol.

See specs/pr-negotiation.md for the full design: structured debate across
3 evidence-bound rounds, deadlock detection, Chimera arbiter tie-break,
trust-weighted voting, and evidence requirements.

const StrikeNoEvidence = "posting_without_evidence" ...
const ExitCodeResolved = 0 ...
const DefaultRoundTimeout = 5 * time.Minute ...
const TrustFloor = 0 ...
const ExitCodeDryRun = 10
const FrivolousVetoThreshold = 3
const FrivolousVetoWindow = 90 * 24 * time.Hour
const MaxDiffChars = 50000
const NegotiationTimeout = 30 * time.Minute
const RoundTimeout = 5 * time.Minute
const StrikeMaxStrikes = 3
const StrikeRoundMissThreshold = 2
const TrustCapAfterFrivolousVetoes = 69
var TransitionTable = map[State]map[string]State{ ... }
func AllExitCodes() []int
func ApplyTrustDelta(current int, delta int) int
func AssembleArbiterInput(input *ArbiterInput) string
func AssembleFromNegotiator(n *Negotiator, title, description, diff string, specFiles []SpecFile) string
func CanParticipate(trust TrustLevel) bool
func CanVeto(trust TrustLevel) bool
func CheckConcession(body string) bool
func ConcatSpecFiles(files []SpecFile) string
func DetectConflict(a, b Verdict) bool
func EscalationExitCode(reason EscalationReason) int
func EscalationMessage(reason EscalationReason, detail string) string
func EscalationTimestamp() string
func EstimatePromptSize(input *ArbiterInput) int
func EventDescription(eventType NegotiationEventType) string
func ExitCodeDescription(code int) string
func ExitCodeFromError(err error) int
func FormatConsensus(result ConsensusResult) string
func FormatDryRunReport(report *DryRunReport) string
func FormatEscalationComment(data *EscalationData) string
func FormatExitMessage(code int, detail string) string
func FormatHistory(r *HistoryResult) string
func FormatVerdictMarkdown(v *VerdictFile) string
func FrivolousPenalty() int
func HasQuorum(signals []ReviewSignal, category ConsensusCategory) bool
func IsEscalatable(state State) bool
func IsRetryableExit(code int) bool
func IsTerminalExit(code int) bool
func IsVeto(agent Agent, body string) bool
func RequiredQuorum(category ConsensusCategory) int
func SplitCost(cost float64) (agentAShare, agentBShare float64)
func TruncateDiff(diff string) string
func TrustAdjustmentSummary(adjustments []TrustAdjustment) string
func ValidateComment(body string) (ParsedRoundComment, EvidenceValidation)
func ValidateTimeoutConfig(cfg TimeoutConfig) error
func VetoWeight(agent Agent) float64
func WriteStateFile(dir string, state *StateFile) (string, error)
func WriteVerdictFile(dir string, v *VerdictFile) (string, error)
type ActiveNegotiation struct{ ... }
type Agent struct{ ... }
type AgentBudget struct{ ... }
type AgentCostBreakdown struct{ ... }
type ArbiterClient struct{ ... }
    func NewArbiterClient(baseURL string) *ArbiterClient
type ArbiterInput struct{ ... }
type AuditLogger struct{ ... }
    func NewAuditLogger(path string) (*AuditLogger, error)
type ChimeraVerdict struct{ ... }
type ConsensusCalculator struct{ ... }
    func NewConsensusCalculator(category ConsensusCategory) *ConsensusCalculator
type ConsensusCategory string
    const CategoryContract ConsensusCategory = "contract" ...
type ConsensusResult struct{ ... }
type CostReconciler struct{ ... }
    func NewCostReconciler(budgets []AgentBudget) *CostReconciler
type CostReport struct{ ... }
type Debate struct{ ... }
    func NewDebate(neg *Negotiation) *Debate
type DebateEvent struct{ ... }
type DryRunReport struct{ ... }
type DryRunSimulator struct{}
    func NewDryRunSimulator() *DryRunSimulator
type EscalationData struct{ ... }
    func EscalationFromNegotiator(n *Negotiator, reason EscalationReason) *EscalationData
type EscalationReason string
    const EscalationTimeout EscalationReason = "timeout" ...
type EvidenceItem struct{ ... }
type EvidenceType string
    const EvidenceSpec EvidenceType = "spec" ...
type EvidenceValidation struct{ ... }
    func ValidateEvidence(comment ParsedRoundComment) EvidenceValidation
type HistoryEntry struct{ ... }
type HistoryQuery struct{ ... }
type HistoryResult struct{ ... }
    func QueryHistory(dir string, q HistoryQuery) (*HistoryResult, error)
type Negotiation struct{ ... }
type NegotiationConfig struct{ ... }
type NegotiationError struct{ ... }
    func NewBudgetExhaustedError(agent string, remaining, tiebreakCost float64) *NegotiationError
    func NewChimeraUnavailableError(detail string) *NegotiationError
    func NewDryRunError(rounds int) *NegotiationError
    func NewEvidenceRequiredError(agent string, round int) *NegotiationError
    func NewInvalidStateError(reason string) *NegotiationError
    func NewTimeoutError(rounds int) *NegotiationError
type NegotiationEventType string
    const EventConcessionEvidence NegotiationEventType = "concession_with_evidence" ...
    func AllEventTypes() []NegotiationEventType
type NegotiationOutcome struct{ ... }
type Negotiator struct{ ... }
    func NewNegotiator(prNumber int, agentA, agentB Agent, verdictA, verdictB Verdict, ...) (*Negotiator, error)
    func NewNegotiatorFromConfig(cfg NegotiationConfig, pr PRContext) (*Negotiator, error)
type OverrideResult struct{ ... }
    func CheckOverride(signals []ReviewSignal) OverrideResult
type PRContext struct{ ... }
type ParsedRoundComment struct{ ... }
    func ParseRoundComment(body string) ParsedRoundComment
type Position struct{ ... }
type Resolution struct{ ... }
type ReviewSignal struct{ ... }
type ReviewerContribution struct{ ... }
type ReviewerWeight float64
    const WeightStandard ReviewerWeight = 1.0 ...
    func ComputeWeight(trust TrustLevel) ReviewerWeight
type Round struct{ ... }
type RoundCost struct{ ... }
type RoundTimeoutEvent struct{ ... }
type RoundTimer struct{ ... }
type SpecFile struct{ ... }
type State string
    const StateIdle State = "idle" ...
type StateFile struct{ ... }
    func LoadStateFile(dir string) (*StateFile, error)
type StrikeRecord struct{ ... }
type StrikeTracker struct{ ... }
    func NewStrikeTracker() *StrikeTracker
type StubAgentConfig struct{ ... }
type TimeoutConfig struct{ ... }
    func DefaultTimeoutConfig() TimeoutConfig
type TimeoutStatus struct{ ... }
type TimeoutWatcher struct{ ... }
    func NewTimeoutWatcher(cfg TimeoutConfig, tracker *StrikeTracker) *TimeoutWatcher
type TranscriptSummary struct{ ... }
    func ReplayTranscript(path string) (*TranscriptSummary, error)
type TrustAdjustment struct{ ... }
type TrustAdjustmentEngine struct{}
    func NewTrustAdjustmentEngine() *TrustAdjustmentEngine
type TrustDelta struct{ ... }
    func TrustDeltas(eventType string, wonTiebreak bool) []TrustDelta
type TrustHistoryEntry struct{ ... }
    func ApplyAdjustments(adjustments []TrustAdjustment, currentTrusts map[string]int, ...) (map[string]int, []TrustHistoryEntry)
    func RecordTrustHistory(agent string, currentTrust int, adj TrustAdjustment, negotiationID string) (TrustHistoryEntry, int)
type TrustLevel int
    func ApplyTrust(current TrustLevel, delta int) TrustLevel
type Verdict string
    const VerdictApproved Verdict = "APPROVED" ...
type VerdictFile struct{ ... }
type VetoAttempt struct{ ... }
type VetoTracker struct{ ... }
    func NewVetoTracker() *VetoTracker
type VetoValidation struct{ ... }
    func ValidateVeto(agent Agent, body string) VetoValidation
```

## Related

- [docs/api/README.md](README.md) — package index
