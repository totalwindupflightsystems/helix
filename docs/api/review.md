# pkg/review — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/review"`

Multi-model review pipeline, blast radius, dashboard, load balancing

## Signatures (from `go doc`)

```go
package review // import "github.com/totalwindupflightsystems/helix/pkg/review"

Package review implements the Helix adversarial multi-model review pipeline per
specs/adversarial-review.md.

The bias-stripper removes evaluative language and confidence assertions from
commit messages before they are presented to review models, preventing the
confirmation-bias exploit documented in arXiv 2603.18740.

const ResolutionApproved = "approved" ...
const VerdictApproved = "approved" ...
const DefaultEvidenceDir = ".helix/evidence"
var DefaultTriggers = []AgentTrigger{ ... }
var SLADurations = map[ChangeCategory]time.Duration{ ... }
func CategoryRiskWeight(cat ChangeCategory) int
func CheckDiversity(formation []ModelPoolEntry, config RotationConfig) error
func ConsensusThreshold(cat ChangeCategory) int
func DashboardJSON(d *ChangeDashboard) ([]byte, error)
func DefaultQueuePath() string
func DetermineFormation(cat ChangeCategory) int
func DiversityScore(f Formation) int
func FormatAssignment(selected []ModelPoolEntry, roles []ReviewRole) string
func FormatConsensusReport(bundle EvidenceBundle) string
func FormatDashboard(d *ChangeDashboard) string
func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error)
func HashSeed(seed string) string
func PanelSizeForCategory(category ChangeCategory) int
func RenderConsensusBlock(consensus Consensus) string
func RenderFindingsTable(findings []Finding) string
func SeedFromPR(prURL string, timestamp time.Time) string
func TierRiskMultiplier(tier trust.TrustTier) float64
type ADRRef struct{ ... }
type AdversarialAgentDispatcher struct{ ... }
    func NewAdversarialAgentDispatcher(opts ...DispatcherOption) *AdversarialAgentDispatcher
type AgentInfo struct{ ... }
    func DefaultAgentInfo(at AgentType) AgentInfo
type AgentRequest struct{ ... }
type AgentResult struct{ ... }
type AgentTrigger struct{ ... }
type AgentType string
    const AgentAssumptionBuster AgentType = "assumption-buster" ...
type ArchitectureFit struct{ ... }
    func AssessArchitectureFit(adrDir string, changedFiles []string, blast *BlastRadiusMap) ArchitectureFit
type AssignmentReport struct{ ... }
type AssignmentResult struct{ ... }
type Assumption struct{ ... }
type BiasStripper struct{ ... }
    func NewBiasStripper() *BiasStripper
type BlastPackage struct{ ... }
type BlastRadiusMap struct{ ... }
    func BuildBlastRadius(changedFiles []string, opts BlastRadiusOptions) (*BlastRadiusMap, error)
type BlastRadiusOptions struct{ ... }
type ChainOfCustody struct{ ... }
    func NewChainOfCustody(bundle *EvidenceBundle) *ChainOfCustody
type ChangeCategory string
    const CategoryContract ChangeCategory = "contract" ...
    func InferChangeCategory(files []string) ChangeCategory
type ChangeDashboard struct{ ... }
    func BuildDashboard(in DashboardInput) (*ChangeDashboard, error)
type ChimeraModelClient struct{ ... }
    func NewChimeraClient(cfg ModelClientConfig) *ChimeraModelClient
type Consensus struct{ ... }
type ContractGenerator struct{ ... }
    func NewContractGenerator() *ContractGenerator
type CostEstimate struct{ ... }
type CustodyEvent struct{ ... }
type CustodyEventResult struct{ ... }
type CustodyEventStatus string
    const CustodyOK CustodyEventStatus = "ok" ...
type CustodyEventType string
    const CustodyCreated CustodyEventType = "created" ...
type CustodyReport struct{ ... }
type CustodyStore struct{ ... }
    func NewCustodyStore(store *EvidenceStore) *CustodyStore
type DashboardInput struct{ ... }
type DeepSeekModelClient struct{ ... }
    func NewDeepSeekClient(cfg ModelClientConfig) *DeepSeekModelClient
type Dismissal struct{ ... }
    func ParseDismissal(commentText, humanID, agentID string, prNumber int) (Dismissal, error)
type DismissalHandler struct{ ... }
    func NewDismissalHandler(store *DismissalStore, tracker *FPTracker) *DismissalHandler
type DismissalReason string
    const DismissalFalsePositive DismissalReason = "false_positive" ...
type DismissalStore struct{ ... }
    func NewDismissalStore(path string) (*DismissalStore, error)
type DispatchReport struct{ ... }
type DispatcherOption func(*AdversarialAgentDispatcher)
    func WithDispatcherFPTracker(fp *FPTracker) DispatcherOption
    func WithDispatcherTriggers(triggers []AgentTrigger) DispatcherOption
type EvidenceBundle struct{ ... }
    func NewEvidenceBundle(prURL, reviewID string, formation Formation, ...) *EvidenceBundle
type EvidenceStore struct{ ... }
    func NewEvidenceStore() (*EvidenceStore, error)
    func NewEvidenceStoreWithDir(dir string) (*EvidenceStore, error)
type EvidenceVerifier struct{ ... }
    func NewEvidenceVerifier(opts ...VerifierOption) *EvidenceVerifier
type ExploitPath struct{ ... }
type FPTracker struct{ ... }
    func NewFPTracker() *FPTracker
type FaultResult struct{ ... }
type Finding struct{ ... }
type FindingClass int
    const ClassTestable FindingClass = iota ...
type FindingStatus string
    const StatusVerified FindingStatus = "verified" ...
type Formation struct{ ... }
type FormationAssigner struct{ ... }
    func NewFormationAssigner(tracker *RotationTracker, config RotationConfig) *FormationAssigner
type HumanReviewFilter struct{}
    func NewHumanReviewFilter() *HumanReviewFilter
type LoadTracker struct{ ... }
    func NewLoadTracker() *LoadTracker
type ModelClient interface{ ... }
type ModelClientConfig struct{ ... }
type ModelInfo struct{ ... }
type ModelPoolEntry struct{ ... }
type ModelReviewResult struct{ ... }
type ModelUsageStat struct{ ... }
type NoopTestRunner struct{}
type OrchestratorOption func(*ReviewOrchestrator)
    func WithBiasStripper(bs *BiasStripper) OrchestratorOption
    func WithFPTracker(fp *FPTracker) OrchestratorOption
    func WithMinProviderDiversity(n int) OrchestratorOption
type ProsecutorAgent interface{ ... }
type RelatedIncident struct{ ... }
type ReviewAssigner struct{ ... }
    func NewReviewAssigner() *ReviewAssigner
type ReviewContext struct{ ... }
type ReviewOrchestrator struct{ ... }
    func NewReviewOrchestrator(opts ...OrchestratorOption) *ReviewOrchestrator
type ReviewPanel struct{ ... }
type ReviewQueue struct{ ... }
    func NewReviewQueue() *ReviewQueue
type ReviewQueueItem struct{ ... }
type ReviewRequest struct{ ... }
type ReviewResult struct{ ... }
type ReviewRole string
    const RolePrimary ReviewRole = "primary" ...
type ReviewStatus string
    const ReviewStatusPending ReviewStatus = "pending" ...
type RiskAssessment struct{ ... }
    func ComputeRiskScore(cat ChangeCategory, tier trust.TrustTier, incidents []RelatedIncident) RiskAssessment
type RotationConfig struct{ ... }
    func DefaultRotationConfig() RotationConfig
type RotationTracker struct{ ... }
    func NewRotationTracker() *RotationTracker
type SLAEntry struct{ ... }
type SLATracker struct{ ... }
    func NewSLATracker() *SLATracker
type Signatures struct{ ... }
type StoreEntry struct{ ... }
type StubAgent struct{ ... }
    func NewErrorAgent(info AgentInfo, err error) *StubAgent
    func NewStubAgent(info AgentInfo, result *AgentResult) *StubAgent
type TestRunner interface{ ... }
type TierReviewPolicy struct{ ... }
type TierScaling struct{}
    func NewTierScaling() *TierScaling
type TrustContext struct{ ... }
type VerificationReport struct{ ... }
type VerificationResult struct{ ... }
type VerifierOption func(*EvidenceVerifier)
    func WithTestRunner(r TestRunner) VerifierOption
    func WithVerifyFPTracker(fp *FPTracker) VerifierOption
    func WithVerifyTimeout(d time.Duration) VerifierOption
```

## Related

- [docs/api/README.md](README.md) — package index
