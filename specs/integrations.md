# Helix Integration Contracts — Sub-Project Adapters

**Status:** v1 specification (build-ready)
**Spec version:** 1.0
**Last updated:** 2026-06-19
**Depends on:** All 9 sub-projects exist and are operational
**Blocks:** Helix platform end-to-end integration testing

This document specifies the adapter contract for each of the 9 sub-projects
that plug into the Helix platform. Each contract defines the interface Helix
expects, the configuration surface, health check contract, and error handling
convention. These are NOT implementation specs — the sub-projects already
exist. These are ADAPTER specs: the thin layer that connects each sub-project
to Helix's orchestration, logging, and budget systems.

---

## 1. GitReins → Helix Quality Gate Adapter

**Project:** GitReins v0.4.1 (Python, 221 tests)
**Role in Helix:** Pre-commit and commit-msg quality gate. Blocks commits that fail
Tier 1 (static: secrets, lint, tests, build) and Tier 2 (agentic: LLM evaluator).

### 1.1 Adapter Interface (Go)

```go
// pkg/integration/gitreins/adapter.go

type GitReinsAdapter interface {
    // Guard runs Tier 1 checks against staged changes. Returns PASS/FAIL
    // with per-check results. Called by pre-commit hook.
    Guard(workdir string, opts GuardOpts) (*GuardResult, error)

    // Evaluate runs Tier 2 agentic evaluation against the diff.
    // Returns structured verdict with evidence. Called post-commit.
    Evaluate(workdir string, diff string, opts EvalOpts) (*EvalResult, error)

    // Cost returns the token cost of the last Evaluate call (from LLMUsage).
    // Required for Feature 2 (Cost Estimator) reconciliation.
    Cost(evalResult *EvalResult) CostBreakdown
}

type GuardOpts struct {
    SkipSecrets bool     // Skip secret scanning (dangerous, for trusted CI only)
    SkipLint    bool     // Skip lint checks
    SkipTests   bool     // Skip test execution
    SkipBuild   bool     // Skip build verification
    Timeout     int      // Seconds (default: 60)
}

type GuardResult struct {
    Passed   bool
    Checks   map[string]CheckResult  // key: "secrets", "lint", "tests", "build"
    Duration float64                 // seconds
}

type CheckResult struct {
    Passed   bool
    Output   string   // stdout/stderr summary
    Duration float64
}

type EvalOpts struct {
    MaxIterations   int     // Max LLM reasoning turns (default: 10)
    MaxTime         string  // Wall-clock cap: "30s", "5m" (default: "2m")
    MaxInputTokens  string  // Input token budget (default: "200k")
    MaxOutputTokens string  // Output token budget (default: "50k")
    ToolCallWeight  float64 // Fraction of an iteration per tool call (default: 0.1)
    Criteria        []string // Evaluation criteria
}

type EvalResult struct {
    Passed      bool
    Verdicts    map[string]Verdict  // keyed by criteria
    Evidence    []Evidence
    Usage       LLMUsage
    Duration    float64
}

type Verdict struct {
    Status   string  // "PASS", "FAIL", "INCONCLUSIVE"
    Reason   string
    Score    float64 // 0.0-1.0
}

type Evidence struct {
    Type    string  // "test_output", "file_content", "search_result", "tool_call"
    Content string
    Source  string  // file path or tool name
}

type LLMUsage struct {
    PromptTokens      int
    CompletionTokens  int
    TotalTokens       int
    CacheReadTokens   int   // v0.4.1+: tokens served from cache
    CacheWriteTokens  int   // v0.4.1+: tokens written to cache
}

type CostBreakdown struct {
    FreshInputCost  float64
    CacheHitCost    float64
    CacheWriteCost  float64
    OutputCost      float64
    TotalCost       float64
}

func (g *GitReinsAdapter) Guard(workdir string, opts GuardOpts) (*GuardResult, error)
func (g *GitReinsAdapter) Evaluate(workdir string, diff string, opts EvalOpts) (*EvalResult, error)
func (g *GitReinsAdapter) Cost(evalResult *EvalResult) CostBreakdown
```

### 1.2 Configuration

```yaml
# .gitreins/config.yaml (in repo root)
version: "0.4.1"
guard:
  secrets: true
  lint: true
  tests: true
  build: true
evaluator:
  model: "deepseek-v4-flash"  # Budget model for Tier 2
  provider: "deepseek"
  max_iterations: 10
  max_time: "2m"
  max_input_tokens: "200k"
  max_output_tokens: "50k"
  tool_call_weight: 0.1
commit_msg:
  require_attestation: true
  require_prompt_link: true
```

### 1.3 Health Check

```bash
# Verify GitReins hooks are installed
ls -la .git/hooks/pre-commit .git/hooks/commit-msg
# Verify guard runs clean
.git/hooks/pre-commit
# Expected: exit 0, no output (clean pass)
```

### 1.4 Error Handling

| Failure | Helix Response |
|---------|---------------|
| GitReins not installed | Hook returns 0 (non-blocking), logs WARN |
| Guard timeout | Hook returns 1, commit blocked |
| Evaluator model unavailable | Tier 2 skipped, commit allowed with WARN |
| Evaluator budget exhausted | Commit blocked, human override required |
| LLMUsage missing cache fields | Fallback to naive pricing (no cache discount) |

---

## 2. Chimera → Helix PR Review Adapter

**Project:** Chimera (Python, 90 tests)
**Role in Helix:** Multi-model PR review. Formation engine designs custom DAG,
dispatches to models by domain strength, judges merge with scoring, audit
independently verifies. Called by Forgejo Action on PR open/update.

### 2.1 Adapter Interface

```go
// pkg/integration/chimera/adapter.go

type ChimeraAdapter interface {
    // Review dispatches a full multi-model deliberation on a PR.
    Review(pr ChimeraPR, opts ReviewOpts) (*ChimeraVerdict, error)

    // Formations returns available deliberation formations.
    Formations() ([]Formation, error)

    // Models returns available models with category weights.
    Models() ([]ChimeraModel, error)

    // Health returns service health and version.
    Health() (*ChimeraHealth, error)
}

type ChimeraPR struct {
    RepoOwner   string
    RepoName    string
    PRNumber    int
    Title       string
    Description string
    Diff        string       // git diff of PR
    SpecFiles   []string     // paths to relevant spec files
    AgentReviews []AgentReview // existing agent reviews on the PR
}

type AgentReview struct {
    AgentName string
    Verdict   string  // "APPROVED", "REQUEST_CHANGES", "COMMENT"
    Body      string
    Evidence  []string
    TrustLevel int
}

type ReviewOpts struct {
    Formation     string   // Formation preset name (default: "standard")
    MaxBudget     float64  // Max USD for this review
    StageModels   map[string]string  // Override models per stage
    AllowCustomDAG bool    // Allow custom DAG (default: false)
}

type ChimeraVerdict struct {
    Status      string  // "APPROVE", "REJECT", "NEEDS_MORE_EVIDENCE"
    Confidence  float64 // 0.0-1.0
    Summary     string
    Findings    []Finding
    Trace       ChimeraTrace
    Cost        float64
}

type Finding struct {
    Severity    string  // "CRITICAL", "HIGH", "MEDIUM", "LOW"
    Category    string  // "security", "performance", "style", "logic", "spec_violation"
    File        string
    Line        int
    Description string
    Suggestion  string
}

type ChimeraTrace struct {
    Source      string  // "full" (multi-model) or "fallback" (single-model)
    Stages      []StageResult
    Duration    float64
    TotalTokens int
}

type StageResult struct {
    Stage       string
    Model       string
    Output      string
    Tokens      int
    Duration    float64
}

type Formation struct {
    Name        string
    Description string
    Stages      int
}

type ChimeraModel struct {
    Name     string
    Category string
    Weight   float64
}

type ChimeraHealth struct {
    Status  string  // "healthy", "degraded", "down"
    Version string
    Uptime  float64
    Models  int
}

func (c *ChimeraAdapter) Review(pr ChimeraPR, opts ReviewOpts) (*ChimeraVerdict, error)
func (c *ChimeraAdapter) Formations() ([]Formation, error)
func (c *ChimeraAdapter) Models() ([]ChimeraModel, error)
func (c *ChimeraAdapter) Health() (*ChimeraHealth, error)
```

### 2.2 Configuration

```yaml
# chimera.yaml (Helix-side config — Chimera has its own config too)
endpoint: "http://chimera:8765"
timeout: 120s
default_formation: "standard"
budget_formation: "budget"    # DeepSeek-only, cheaper
arbiter_formation: "arbiter"  # Used for PR negotiation tie-breaks
retry:
  max_attempts: 3
  backoff: 10s
circuit_breaker:
  max_failures: 5
  reset_timeout: 60s
```

### 2.3 Health Check

```bash
curl -s http://chimera:8765/health | jq .
# Expected: {"status": "healthy", "version": "1.x.x", "models": 12}
```

### 2.4 Error Handling

| Failure | Helix Response |
|---------|---------------|
| Chimera unreachable | Retry 3× with 10s backoff. After exhaustion, PR marked "review-unavailable" |
| Budget exhausted | `BudgetExhaustedError`. Human review required. |
| Formation fails | Fallback to single-model (DeepSeek V4 Flash). Comment notes "fallback review" |
| Trace shows `source: "fallback"` | PR comment includes WARNING header |
| Audit disagrees with judge | Finding severity escalated to CRITICAL |

---

## 3. Conscientiousness → Helix Adversarial Review Adapter

**Project:** Conscientiousness (Go, Dockerized)
**Role in Helix:** Agentic self-evaluation. Runs after Chimera review. Asks "are you
sure?" — adversarial evaluation of the PR with verification loops. Results feed
back to Axiom for work item confidence scoring.

### 3.1 Adapter Interface

```go
// pkg/integration/conscientiousness/adapter.go

type ConscientiousnessAdapter interface {
    // Evaluate runs adversarial self-evaluation on a completed PR.
    Evaluate(pr ConscientiousnessPR, opts EvalOpts) (*ConscientiousnessVerdict, error)

    // Health returns service health.
    Health() (*ConscientiousnessHealth, error)
}

type ConscientiousnessPR struct {
    RepoOwner    string
    RepoName     string
    PRNumber     int
    Diff         string
    ChimeraVerdict *ChimeraVerdict  // Chimera's review (input to adversarial eval)
    GitReinsEval  *EvalResult      // GitReins Tier 2 result
    EvidenceBundle string           // Path to verification.md
    ACs           []AcceptanceCriterion
}

type AcceptanceCriterion struct {
    ID     string
    Text   string
    Status string  // "pass", "fail", "untested"
}

type ConscientiousnessVerdict struct {
    Status      string  // "DEFENSIBLE", "VULNERABLE", "INDEFENSIBLE"
    Confidence  float64
    AttackVectors []AttackVector
    Mitigations []Mitigation
    Cost        float64
}

type AttackVector struct {
    Description string
    Severity    string
    Exploitability string  // "trivial", "moderate", "difficult", "theoretical"
}

type Mitigation struct {
    AttackVector string
    Mitigation   string
    Sufficient   bool
}

type ConscientiousnessHealth struct {
    Status  string
    Version string
    Uptime  float64
}

func (c *ConscientiousnessAdapter) Evaluate(pr ConscientiousnessPR, opts EvalOpts) (*ConscientiousnessVerdict, error)
func (c *ConscientiousnessAdapter) Health() (*ConscientiousnessHealth, error)
```

### 3.2 Configuration

```yaml
# conscientiousness.yaml
endpoint: "http://conscientiousness:8080"
timeout: 180s
retry:
  max_attempts: 2
  backoff: 15s
```

### 3.3 Health Check

```bash
curl -s http://conscientiousness:8080/health | jq .
```

### 3.4 Error Handling

| Failure | Helix Response |
|---------|---------------|
| Service down | PR proceeds without adversarial eval. Comment: "adversarial-review-unavailable" |
| INDEFENSIBLE verdict | PR blocked. Human must manually review and override. |
| VULNERABLE verdict | PR allowed with WARNING. Findings posted as PR comments. |
| Timeout | Retry 2×. After exhaustion, PR allowed without eval. |

---

## 4. Muster → Helix API Glue Adapter

**Project:** Muster (Go, 26+ packages)
**Role in Helix:** Auto-generates MCP tools from OpenAPI specs. Parses any REST API
→ produces CLI commands, MCP tools, shell completions, Starlark DSL. The
universal adapter for external services (Forgejo, LangFuse, OpenRouter).

### 4.1 Adapter Interface

```go
// pkg/integration/muster/adapter.go

type MusterAdapter interface {
    // GenerateTools parses an OpenAPI spec and returns MCP tool definitions.
    GenerateTools(specURL string, opts GenerateOpts) ([]MCPTool, error)

    // ExecuteTool calls a specific API endpoint defined by a MCP tool.
    ExecuteTool(tool MCPTool, params map[string]any, auth AuthConfig) (*ToolResult, error)

    // ListTools returns all currently loaded tools.
    ListTools() ([]MCPTool, error)

    // Health returns service health.
    Health() (*MusterHealth, error)
}

type GenerateOpts struct {
    CacheEnabled     bool   // Use multi-tier cache (default: true)
    RateLimitRPS     int    // Max requests/second to source API (default: 10)
    IncludeDeprecated bool  // Include deprecated endpoints (default: false)
}

type MCPTool struct {
    Name        string
    Description string
    Method      string
    Path        string
    Parameters  []ToolParam
    AuthRequired bool
    Scopes      []string
}

type ToolParam struct {
    Name        string
    Type        string
    Required    bool
    Description string
    Default     any
}

type AuthConfig struct {
    Type    string  // "bearer", "basic", "api_key"
    Token   string
    Header  string  // Header name for api_key type
}

type ToolResult struct {
    StatusCode int
    Body       string
    Headers    map[string]string
    Duration   float64
}

type MusterHealth struct {
    Status    string
    ToolsLoaded int
    CacheHitRate float64
}

func (m *MusterAdapter) GenerateTools(specURL string, opts GenerateOpts) ([]MCPTool, error)
func (m *MusterAdapter) ExecuteTool(tool MCPTool, params map[string]any, auth AuthConfig) (*ToolResult, error)
func (m *MusterAdapter) ListTools() ([]MCPTool, error)
func (m *MusterAdapter) Health() (*MusterHealth, error)
```

### 4.2 Primary Use Cases in Helix

| Service | OpenAPI Spec | Tools Generated |
|---------|-------------|-----------------|
| Forgejo | `/swagger.v1.json` | Repos, PRs, issues, users, branches |
| LangFuse | `/api/public/openapi.json` | Traces, prompts, datasets, scores |
| OpenRouter | Manual spec (no native OpenAPI) | Key management, usage queries, model list |

### 4.3 Configuration

```yaml
# muster.yaml
endpoint: "http://muster:9090"
specs:
  forgejo: "http://forgejo:3000/swagger.v1.json"
  langfuse: "http://langfuse:3000/api/public/openapi.json"
  openrouter: "file:///etc/muster/openrouter-openapi.yaml"  # Manual spec
cache:
  ttl: 3600s
  max_size: 100MB
rate_limit:
  default_rps: 10
  forgejo_rps: 20
```

### 4.4 Health Check

```bash
curl -s http://muster:9090/health | jq .
```

---

## 5. Kobayashi-Maru → Helix Stress Testing Adapter

**Project:** Kobayashi-Maru (Go + Python)
**Role in Helix:** No-win scenario training system. Runs adversarial stress tests
against Helix agents to verify they can't cheat quality gates. Exhaustive
specification, Ralph Loop engine, penetration testing, Prometheus + Loki monitoring.

### 5.1 Adapter Interface

```go
// pkg/integration/kobayashi-maru/adapter.go

type KobayashiMaruAdapter interface {
    // RunScenario executes a no-win scenario against a target agent or component.
    RunScenario(scenario Scenario, opts ScenarioOpts) (*ScenarioResult, error)

    // ListScenarios returns available stress test scenarios.
    ListScenarios() ([]Scenario, error)

    // Metrics returns Prometheus metrics from the last run.
    Metrics() (*MaruMetrics, error)
}

type Scenario struct {
    ID          string
    Name        string
    Description string
    Target      string  // "gitreins", "chimera", "conscientiousness", "agent-identity", "all"
    Difficulty  string  // "kobayashi-maru" (unwinnable), "difficult", "standard"
}

type ScenarioOpts struct {
    Timeout     string  // "5m", "30m", "2h"
    Params      map[string]string  // Scenario-specific parameters
    RecordVideo bool    // Record terminal session
}

type ScenarioResult struct {
    Passed      bool    // Did the target survive? (Almost always false for unwinnable)
    Score       float64 // 0.0-1.0
    Attempts    int
    CheatsDetected []CheatAttempt
    Findings    []ScenarioFinding
    Logs        string  // Loki query reference
    Cost        float64
}

type CheatAttempt struct {
    Type        string  // "skip_gate", "fake_evidence", "backdoor", "privilege_escalation"
    DetectedBy  string  // Which check caught it
    Timestamp   string
}

type ScenarioFinding struct {
    Severity    string
    Component   string
    Description string
    Remediation string
}

type MaruMetrics struct {
    TotalRuns       int
    SurvivalRate    float64
    AvgScore        float64
    CheatDetectionRate float64
}

func (km *KobayashiMaruAdapter) RunScenario(scenario Scenario, opts ScenarioOpts) (*ScenarioResult, error)
func (km *KobayashiMaruAdapter) ListScenarios() ([]Scenario, error)
func (km *KobayashiMaruAdapter) Metrics() (*MaruMetrics, error)
```

### 5.2 Configuration

```yaml
# kobayashi-maru.yaml
endpoint: "http://kobayashi-maru:9095"
scenarios_dir: "/etc/kobayashi-maru/scenarios"
default_timeout: "30m"
monitoring:
  prometheus: "http://prometheus:9090"
  loki: "http://loki:3100"
```

### 5.3 Integration Triggers

| Trigger | When | Scenario |
|---------|------|----------|
| Pre-release | Before any Helix version tag | `all` — full battery |
| Weekly cron | Sunday 03:00 UTC | `gitreins`, `chimera` |
| Agent onboard | New agent provisioned | `agent-identity` |
| Gate change | GitReins config modified | `gitreins` |

---

## 6. Axiom → Helix Orchestration Adapter

**Project:** Axiom (multi-language)
**Role in Helix:** Agent fleet management. Decomposes human intent → spec extraction
→ meta-plan → work items → build loop → verification → adversarial review → PR.
60+ adversarial/quality agents. Specs-as-contracts with evidence bundles.

### 6.1 Adapter Interface

```go
// pkg/integration/axiom/adapter.go

type AxiomAdapter interface {
    // Run executes the full Axiom pipeline on a work item.
    Run(intent string, repoPath string, opts RunOpts) (*AxiomResult, error)

    // Cmd executes a single Axiom command.
    Cmd(command string, repoPath string) (*CmdResult, error)

    // Status returns current Axiom pipeline status.
    Status(repoPath string) (*AxiomStatus, error)

    // ListWorkItems returns all work items for a repo.
    ListWorkItems(repoPath string) ([]WorkItem, error)
}

type RunOpts struct {
    InProcess    bool   // Run in-process (default: true)
    EntryCommand string // Single phase only (e.g., "/axiom-step")
    NoBranch     bool   // Skip worktree branching
    Yes          bool   // Skip approval prompts
    OpenCodeURL  string // External OpenCode server URL
    SpinUpOpenCode bool // Spin up fresh OpenCode server
}

type AxiomResult struct {
    WorkItemID   string
    Status       string  // "complete", "failed", "blocked"
    Confidence   float64
    Evidence     string  // Path to evidence bundle
    PR           string  // PR URL
    Cost         float64
    Duration     float64
}

type CmdResult struct {
    Status   string
    Output   string
    Duration float64
}

type AxiomStatus struct {
    ActiveRuns    int
    QueuedItems   int
    CurrentPhase  string
    BlockedItems  []string
}

type WorkItem struct {
    ID          string
    Title       string
    Status      string  // "pending", "in_progress", "complete", "blocked"
    Priority    string
    Assignee    string  // Agent name
    Confidence  float64
}

func (a *AxiomAdapter) Run(intent string, repoPath string, opts RunOpts) (*AxiomResult, error)
func (a *AxiomAdapter) Cmd(command string, repoPath string) (*CmdResult, error)
func (a *AxiomAdapter) Status(repoPath string) (*AxiomStatus, error)
func (a *AxiomAdapter) ListWorkItems(repoPath string) ([]WorkItem, error)
```

### 6.2 Configuration

```yaml
# axiom.yaml
# Axiom runs as a CLI, not a service. This config is for Helix to invoke it.
binary: "/home/kara/.axiom-venv/bin/axiom"
opencode_base_url: "http://127.0.0.1:4096"
default_timeout: 3600s
work_item_dir: ".memory-bank/work-items"
```

### 6.3 Health Check

```bash
# Axiom is CLI-only. Verify it's callable:
/home/kara/.axiom-venv/bin/axiom --version
which opencode  # Axiom requires opencode on PATH
```

---

## 7. Hivemind → Helix Memory & Scheduling Adapter

**Project:** Hivemind (Go + React TS)
**Role in Helix:** Persistent agent memory + task scheduling. IAM/auth, git operations,
Ralph Loop engine, SQLite + YAML memory bank, inbox/compiled pattern, hierarchical
rate limiting. The shared blackboard that agents read/write.

### 7.1 Adapter Interface

```go
// pkg/integration/hivemind/adapter.go

type HivemindAdapter interface {
    // ScheduleTask queues a task for agent execution.
    ScheduleTask(task HiveTask) (*HiveTask, error)

    // ClaimTask acquires the next available task for an agent.
    ClaimTask(agentName string) (*HiveTask, error)

    // CompleteTask marks a task as done with results.
    CompleteTask(taskID string, result TaskResult) error

    // ReadMemory reads from the shared memory bank.
    ReadMemory(key string) (*MemoryEntry, error)

    // WriteMemory writes to the shared memory bank.
    WriteMemory(key string, content string, domain string) error

    // Health returns service health.
    Health() (*HivemindHealth, error)
}

type HiveTask struct {
    ID          string
    Title       string
    Description string
    Priority    string
    Status      string  // "queued", "claimed", "in_progress", "complete", "failed"
    AssignedTo  string
    CreatedAt   string
    Deadline    string
    Dependencies []string  // Task IDs that must complete first
}

type TaskResult struct {
    Success   bool
    Output    string
    Evidence  string  // Path to evidence bundle
    Cost      float64
    Duration  float64
}

type MemoryEntry struct {
    Key       string
    Content   string
    Domain    string
    UpdatedAt string
    Version   int
}

type HivemindHealth struct {
    Status      string
    TasksQueued int
    TasksActive int
    MemorySize  int64
    Uptime      float64
}

func (h *HivemindAdapter) ScheduleTask(task HiveTask) (*HiveTask, error)
func (h *HivemindAdapter) ClaimTask(agentName string) (*HiveTask, error)
func (h *HivemindAdapter) CompleteTask(taskID string, result TaskResult) error
func (h *HivemindAdapter) ReadMemory(key string) (*MemoryEntry, error)
func (h *HivemindAdapter) WriteMemory(key string, content string, domain string) error
func (h *HivemindAdapter) Health() (*HivemindHealth, error)
```

### 7.2 Configuration

```yaml
# hivemind.yaml
endpoint: "http://hivemind:8081"
memory_bank: "/data/memory-bank"
rate_limit:
  read_rps: 100
  write_rps: 20
```

---

## 8. Hermes4Friends → Helix Agent Hosting Adapter

**Project:** Hermes4Friends (Docker Compose)
**Role in Helix:** Multi-tenant agent hosting. Per-friend Docker containers (gluetun
VPN + dind executor + hermes-agent), Nextcloud bridge, known-friends identity
management. The infrastructure that gives each agent its own sandbox.

### 8.1 Adapter Interface

```go
// pkg/integration/h4f/adapter.go

type H4FAdapter interface {
    // ListAgents returns all known agents from known-friends.json.
    ListAgents() ([]H4FAgent, error)

    // GetAgent returns a specific agent's details.
    GetAgent(name string) (*H4FAgent, error)

    // ProvisionAgent creates a new agent container and identity.
    ProvisionAgent(name string, tier string) (*H4FAgent, error)

    // DeprovisionAgent offboards an agent (archives, doesn't delete).
    DeprovisionAgent(name string) error

    // GetBudget returns an agent's current budget status.
    GetBudget(name string) (*AgentBudget, error)

    // ContainerStatus returns the Docker container status for an agent.
    ContainerStatus(name string) (*ContainerInfo, error)
}

type H4FAgent struct {
    Name              string
    DisplayName       string
    Status            string  // "active", "offboarded", "pending"
    Tier              string  // "pro", "flash"
    TrustLevel        int
    BudgetWeekly      float64
    BudgetUsed        float64
    Permissions       []string
    ForgejoUsername   string
    ForgejoUserID     int
    SSHKeyFingerprint string
    ContainerName     string
    CreatedAt         string
}

type AgentBudget struct {
    WeeklyLimit     float64
    UsedThisWeek    float64
    Remaining       float64
    PeriodStart     string
    PeriodEnd       string
    TasksThisWeek   int
    AvgCostPerTask  float64
}

type ContainerInfo struct {
    Name    string
    Status  string  // "running", "stopped", "paused"
    Uptime  float64
    Memory  int64
    CPU     float64
}

func (h *H4FAdapter) ListAgents() ([]H4FAgent, error)
func (h *H4FAdapter) GetAgent(name string) (*H4FAgent, error)
func (h *H4FAdapter) ProvisionAgent(name string, tier string) (*H4FAgent, error)
func (h *H4FAdapter) DeprovisionAgent(name string) error
func (h *H4FAdapter) GetBudget(name string) (*AgentBudget, error)
func (h *H4FAdapter) ContainerStatus(name string) (*ContainerInfo, error)
```

### 8.2 Configuration

```yaml
# h4f.yaml
known_friends_path: "/opt/hermes-demo/.hermes/h4f/known-friends.json"
compose_dir: "/opt/hermes-demo"
bridge_endpoint: "http://bridge:9000"
```

---

## 9. Ralph Loop → Helix Execution Pattern Adapter

**Project:** Ralph Loop (orchestration pattern, not a standalone service)
**Role in Helix:** The execution lock pattern: acquire lock → create worktree → write
code → commit with attestation → open PR → merge → release lock. Guarantees no
two agents edit the same branch concurrently. Every commit carries provenance.

### 9.1 Adapter Interface

```go
// pkg/integration/ralph-loop/adapter.go

type RalphLoopAdapter interface {
    // AcquireLock claims a branch for exclusive agent access.
    AcquireLock(branch string, agentName string, ttl int) (*Lock, error)

    // ReleaseLock releases a previously acquired lock.
    ReleaseLock(lockID string) error

    // ExtendLock extends the TTL of an active lock.
    ExtendLock(lockID string, additionalTTL int) error

    // ListLocks returns all active locks.
    ListLocks() ([]Lock, error)

    // CreateWorktree creates an isolated worktree for agent work.
    CreateWorktree(repoPath string, branch string) (*Worktree, error)

    // CleanupWorktree removes a worktree after merge or abort.
    CleanupWorktree(worktreePath string) error
}

type Lock struct {
    ID        string
    Branch    string
    AgentName string
    AcquiredAt string
    ExpiresAt string
    TTL       int
}

type Worktree struct {
    Path      string
    Branch    string
    CreatedAt string
}

func (r *RalphLoopAdapter) AcquireLock(branch string, agentName string, ttl int) (*Lock, error)
func (r *RalphLoopAdapter) ReleaseLock(lockID string) error
func (r *RalphLoopAdapter) ExtendLock(lockID string, additionalTTL int) error
func (r *RalphLoopAdapter) ListLocks() ([]Lock, error)
func (r *RalphLoopAdapter) CreateWorktree(repoPath string, branch string) (*Worktree, error)
func (r *RalphLoopAdapter) CleanupWorktree(worktreePath string) error
```

### 9.2 Configuration

```yaml
# ralph-loop.yaml
lock_backend: "file"  # "file", "redis", "postgres"
lock_dir: "/tmp/helix-locks"
default_ttl: 3600s    # 1 hour
max_ttl: 86400s       # 24 hours
worktree_dir: "/tmp/helix-worktrees"
```

### 9.3 Health Check

```bash
# List active locks
ls /tmp/helix-locks/
# List active worktrees
ls /tmp/helix-worktrees/
```

---

## 10. Cross-Cutting Integration Contracts

### 10.1 Error Handling Convention

All adapters follow the same error pattern:

```go
type AdapterError struct {
    Adapter    string  // "gitreins", "chimera", etc.
    Operation  string  // "Guard", "Review", etc.
    Kind       string  // "network", "timeout", "auth", "budget", "internal"
    Message    string
    Retryable  bool
    RetryAfter int     // seconds
}

func (e *AdapterError) Error() string
func (e *AdapterError) ExitCode() int
```

### 10.2 Logging Convention

All adapters log to stderr with a consistent format:
```
timestamp [level] adapter=NAME operation=OP status=PASS|FAIL duration=MS
```

When verbose: include full request/response (secrets redacted).

### 10.3 Circuit Breaker Pattern

Every adapter with a network dependency implements:

```go
type CircuitBreaker struct {
    Adapter       string
    MaxFailures   int           // Default: 5
    ResetTimeout  time.Duration // Default: 60s
    State         string        // "closed", "open", "half_open"
    Failures      int
    LastFailure   time.Time
}

// Before each call:
//   If State == "open" and time.Since(LastFailure) < ResetTimeout → fail fast
//   If State == "half_open" → allow one probe call
func (cb *CircuitBreaker) Allow() bool
func (cb *CircuitBreaker) RecordSuccess()
func (cb *CircuitBreaker) RecordFailure()
```

### 10.4 Budget Attribution

Every adapter call that incurs LLM cost attributes to the requesting agent:
- Chimera review → cost attributed to the PR author (agent or human)
- GitReins Tier 2 → cost attributed to the committing agent
- Conscientiousness → cost split between PR author and reviewing agent
- Kobayashi-Maru runs → platform overhead (not attributed to any agent)

---

## 11. Verification Checklist

- [ ] Each adapter interface compiles (`go build ./pkg/integration/...`)
- [ ] Each adapter has a documented health check
- [ ] Each adapter has error handling for: network down, timeout, auth failure, budget exhausted
- [ ] Circuit breaker pattern implemented for all 6 network adapters
- [ ] Logging format consistent across all adapters
- [ ] Budget attribution follows §10.4 for all LLM-cost adapters
- [ ] Adapter configs validated at startup (fail fast on bad config)

---

## Document Status

- [x] GitReins adapter specified
- [x] Chimera adapter specified
- [x] Conscientiousness adapter specified
- [x] Muster adapter specified
- [x] Kobayashi-Maru adapter specified
- [x] Axiom adapter specified
- [x] Hivemind adapter specified
- [x] Hermes4Friends adapter specified
- [x] Ralph Loop adapter specified
- [x] Cross-cutting contracts (errors, logging, circuit breaker, budget attribution)
- [x] Verification checklist
