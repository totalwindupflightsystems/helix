# pkg/dispatcher — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/dispatcher"`

Ralph Loop engine, task decomposition, agent assignment

## Signatures (from `go doc`)

```go
package dispatcher // import "github.com/totalwindupflightsystems/helix/pkg/dispatcher"

Package dispatcher provides the Helix orchestration layer that replaces Axiom.
It decomposes specifications into tasks, assigns tasks to capable agents,
and drives the Ralph Loop execution pipeline.

Package dispatcher provides the Helix orchestration layer that replaces Axiom.
It decomposes specifications into tasks, assigns tasks to capable agents,
and drives the Ralph Loop execution pipeline.

Package dispatcher — forgejo_loop.go

Wires the Ralph Loop (ExecuteLoop in loop.go) to a live Forgejo instance via
pkg/forgejo. The default ExecuteLoop opens "PRs" by writing a marker file and
printing the details — fine for tests, useless for an actual spawn pipeline.
ForgejoLoop replaces the commitWork + openPR stubs with real Forgejo calls.

Lifecycle:

 1. CreateBranch(owner, repo, "<branch-name>", baseRef) - 409 = branch already
    exists → idempotent re-run, treat as success
 2. CommitWork writes the per-step markers + commit msg to a worktree (this is
    the default commitWork from loop.go; we don't change it — in production the
    caller can replace it via WorkItemExecutor).
 3. CreatePR(owner, repo, head, base, title, body) - 409 = PR already exists
    → idempotent re-run, surface HTML_URL - 422 = head==base or invalid branch
    names → fail loudly
 4. Release the lock file.

DryRun mode stops after CreateBranch planning and prints the would-be PR
title/body — never touches the network. The DispatchOutcome in dry-run mode has
PRURL == "" and BranchName populated.

Standalone usage without provisioning:

    The provisioner arg is optional. When nil, ForgejoLoop uses a static
    agent profile constructed from the agentName argument, so the CLI can
    drive ForgejoLoop without first calling identity.Provisioner. This
    keeps the dispatch CLI runnable in dry-run or against a test mock
    without requiring a live Forgejo user-record.

Package dispatcher provides the Helix orchestration layer that replaces Axiom.
It decomposes specifications into tasks, assigns tasks to capable agents,
and drives the Ralph Loop execution pipeline.

Design goals:
  - Thin orchestration: the dispatcher coordinates, agents execute.
  - Capability-matched dispatch: tasks go to agents with the right skills and
    available capacity.
  - Spec-driven: all work originates from a spec markdown file.
  - Stdlib only: no external Go dependencies.

const SectionSpecs = "specs" ...
const DefaultBudget = 4096
const DefaultSourcesPath = ".helix/sources.yaml"
var ErrAgentOverloaded = errors.New("dispatcher: all capable agents are at max load")
var ErrClarificationNeeded = fmt.Errorf("dispatcher: clarification needed")
var ErrDecomposeFailed = errors.New("dispatcher: spec decomposition failed")
var ErrNoAgents = errors.New("dispatcher: no agents available")
var ErrNoCapableAgent = errors.New("dispatcher: no agent with required capability")
var ErrSpecNotFound = errors.New("dispatcher: spec file not found")
var ErrTierTooLow = errors.New("dispatcher: no agent meets required trust tier")
var FileCategoryTier = map[FileCategory]trust.TrustTier{ ... }
func AsClarificationError(err error, target **clarificationError) bool
func AutoResolve(req ClarificationRequest, clarStore *ClarificationStore, adrStore ADRStore) (string, bool)
func CanSelfAssign(agent AgentProfile, task Task) bool
func DefaultClarificationDir() string
func EstimateTokens(s string) int
func NewClarificationError(req *ClarificationRequest) error
func RequiredTierForFiles(files []string) trust.TrustTier
func ValidateTierAssignment(agent AgentProfile, task Task) error
type ADRStore interface{ ... }
type AgentProfile struct{ ... }
type AssembledContext struct{ ... }
type ClarificationContext struct{ ... }
type ClarificationFilter struct{ ... }
type ClarificationRecord struct{ ... }
type ClarificationRequest struct{ ... }
    func IsClarificationRequest(err error) (*ClarificationRequest, bool)
    func NewClarificationRequest(taskID string, blockedStep int, question string, ctx ClarificationContext) *ClarificationRequest
type ClarificationResponse struct{ ... }
    func NewClarificationResponse(taskID, resolution, resolvedBy string) *ClarificationResponse
type ClarificationStore struct{ ... }
    func NewClarificationStore(dir string) *ClarificationStore
type CodebaseIndex struct{ ... }
    func IndexRepo(repoPath string, ignorePatterns []string) (*CodebaseIndex, error)
type ContextAssembler struct{ ... }
type ContextBudget struct{ ... }
    func NewContextBudget(tokens int) *ContextBudget
type ContextSection struct{ ... }
type CostGuard struct{ ... }
    func NewCostGuard(permExp *identity.PermissionExpansion, est *estimate.Estimator) *CostGuard
type CostGuardDecision string
    const CostGuardApproved CostGuardDecision = "APPROVED" ...
type CostGuardResult struct{ ... }
type DispatchOutcome struct{ ... }
type DispatchResult struct{ ... }
    func AssignAgent(task Task, agents []AgentProfile) (*DispatchResult, error)
type Dispatcher struct{ ... }
    func NewDispatcher(agents []AgentProfile) *Dispatcher
type FileCategory string
    const CatInfrastructure FileCategory = "infrastructure" ...
    func ClassifyFileCategory(path string) FileCategory
type ForgejoLoop struct{ ... }
type IndexedFile struct{ ... }
type SourceToolInjector struct{ ... }
    func NewSourceToolInjector(sources map[string]source.Source, opts ...SourceToolInjectorOption) (*SourceToolInjector, error)
    func NewSourceToolInjectorFromFile(path string, opts ...SourceToolInjectorOption) (*SourceToolInjector, error)
type SourceToolInjectorOption func(*sourceToolInjectorConfig)
    func WithMusterBridge(b *source.MusterBridge) SourceToolInjectorOption
    func WithToolsProvider(p ToolsProvider) SourceToolInjectorOption
type Step struct{ ... }
    func DecomposeTask(taskDesc string) ([]Step, error)
type StepStatus string
    const StepPending StepStatus = "pending" ...
type Task struct{ ... }
    func DecomposeSpec(specPath string) ([]Task, error)
type TaskStatus string
    const StatusPending TaskStatus = "pending" ...
type ToolsProvider func(ctx context.Context, src *source.Source) (*source.ToolSet, error)
type WorkItem struct{ ... }
```

## Related

- [docs/api/README.md](README.md) — package index
