# Helix Resolution Plans — Phases 3–4: Task Decomposition & Implementation

**Status:** v1.0 — build-ready plan  
**Spec version:** 1.0  
**Last updated:** 2026-07-07  
**Depends on:** [interaction-map.md](../interaction-map.md), specs/trust-model.md, specs/cost-estimator.md, specs/sandbox.md, specs/cross-component-wiring.md  
**References:** `pkg/dispatcher`, `pkg/sandbox`, `pkg/estimate`, `pkg/trust`

---

This document provides concrete resolution plans for all 6 interaction points in Phases 3–4 of the [Helix Interaction Map](../interaction-map.md). Each plan identifies the responsible Helix component, distinguishes what the human sees from what the agent sees, enumerates what needs building at the code level, and defines binary acceptance criteria.

---

## Interaction Point 3.1 — Work Breakdown

> **Type:** HANDOFF → Agent-driven decomposition with human approval.  
> **Flow:** Spec → atomic, verifiable tasks with dependencies, acceptance criteria, estimated cost, and required trust tier.  
> **Goal:** Each task has verifiable ACs (binary pass/fail), estimated cost, and required trust tier. Tasks are linked to spec sections for traceability.

### Component Responsibility

| Layer | Component | File(s) |
|-------|-----------|---------|
| Decomposition engine | `pkg/dispatcher` | `decomposer.go` (`DecomposeSpec`) |
| Cost estimation | `pkg/estimate` | `estimator.go`, `types.go` |
| Trust tier binding | `pkg/trust` | `tiers.go` (`DetermineTier`), `ledger.go` |
| CLI surface | `cmd/helix` | `dispatcher.go` (`runDispatcherStatus`, `runDispatcherListTasks`) |

### What the Human Sees

```
┌─────────────────────────────────────────────────────────────────┐
│                    HELIX WORK BREAKDOWN                         │
│  Spec: specs/feature-login.md                  12 tasks found   │
├─────────────────────────────────────────────────────────────────┤
│  ID        DESCRIPTION                     PRIO   TIER    COST  │
│  task-001  Phase 1: Create login form       1     Observed $0.02│
│  task-002  Phase 2: Add password hashing    2     Trusted  $0.05│
│  task-003  Phase 3: Session management      3     Trusted  $0.08│
│  task-004  Feature: OAuth integration       4     Trusted  $0.12│
│  ...                                                             │
├─────────────────────────────────────────────────────────────────┤
│  Total estimated cost: $1.47                                     │
│  Agent: wojons (Observed, weekly budget $25.00, spent $3.42)    │
│  Cost guard: APPROVED (all tasks within tier cap)                │
│                                                                  │
│  [APPROVE ALL] [APPROVE SELECTED] [MODIFY] [REJECT]             │
└─────────────────────────────────────────────────────────────────┘
```

Human actions:
- Reviews decomposition (correct sections, sensible priority ordering).
- Approves/rejects individual tasks or the whole batch.
- Can modify task descriptions, reassign priority, or split/merge tasks.
- Sees the total cost footprint before any agent executes.

### What the Agent Sees

When an agent is later dispatched to a specific task (3.2), it receives the task as a `WorkItem` struct:

```json
{
  "task": {
    "id": "task-001",
    "spec_ref": "specs/feature-login.md",
    "description": "Phase 1: Create login form with email and password fields, client-side validation, and accessible error states",
    "priority": 1,
    "assigned_agent": "",
    "status": "pending"
  }
}
```

The agent also sees the spec section hash (for traceability) and its binary acceptance criteria in the context package (3.3).

### What Needs Building

#### 3.1.1 — Structured Acceptance Criteria on Tasks

**Current state:** `Task.Description` is a flat string. `Step` has `Action` and `ExpectedOutput` but no binary pass/fail verdict.

**Gap:** No structured ACs. Acceptance criteria are implicit in the task description text.

**Plan:**

1. Add an `AcceptanceCriteria` field to the `Task` struct (`pkg/dispatcher/types.go`):

```go
type Task struct {
    ID                 string              `json:"id"`
    SpecRef            string              `json:"spec_ref"`
    SpecSectionHash    string              `json:"spec_section_hash"`  // NEW: sha256 of spec section text
    Description        string              `json:"description"`
    Priority           int                 `json:"priority"`
    AssignedAgent      string              `json:"assigned_agent"`
    Status             TaskStatus          `json:"status"`
    AcceptanceCriteria []AcceptanceCriterion `json:"acceptance_criteria"` // NEW
    EstimatedCost      float64             `json:"estimated_cost"`    // NEW
    RequiredTier       trust.TrustTier     `json:"required_tier"`     // NEW
    DependsOn          []string            `json:"depends_on"`        // NEW: task IDs
}

type AcceptanceCriterion struct {
    ID          string `json:"id"`          // e.g. "ac-001"
    Description string `json:"description"` // e.g. "Form submits successfully with valid email"
    Verdict     string `json:"verdict"`     // "pending" | "pass" | "fail"
}
```

2. Extend `DecomposeSpec` to extract ACs from spec markdown. A spec section can contain a bullet list under `### Acceptance Criteria`:

```
## Phase 1: Create login form

### Acceptance Criteria
- AC-1: Email field validates RFC 5322 addresses
- AC-2: Password field minimum 8 characters
- AC-3: Submit button disabled until both fields are valid
- AC-4: Error states are announced by screen readers
```

Parser logic: After detecting a `## Phase` or `## Feature` heading, scan subsequent `### Acceptance Criteria` blocks. Each bullet becomes an `AcceptanceCriterion`.

**CLI commands:**

```bash
# Decompose a spec and show tasks with ACs
helix dispatcher status --spec specs/feature-login.md --json

# Output:
# {
#   "spec": "specs/feature-login.md",
#   "task_count": 3,
#   "tasks": [
#     {
#       "id": "task-001",
#       "description": "Phase 1: Create login form",
#       "acceptance_criteria": [
#         {"id": "ac-001", "description": "Email validates RFC 5322", "verdict": "pending"},
#         {"id": "ac-002", "description": "Password min 8 chars", "verdict": "pending"}
#       ],
#       "estimated_cost": 0.02,
#       "required_tier": "observed",
#       "priority": 1
#     }
#   ]
# }
```

#### 3.1.2 — Spec Section Hashing for Traceability

**New function** in `decomposer.go`:

```go
// HashSection returns the SHA-256 hex digest of a spec section's raw text.
// This allows every task to prove its lineage to the exact spec text.
func HashSection(sectionText string) string {
    h := sha256.Sum256([]byte(sectionText))
    return hex.EncodeToString(h[:])
}
```

The section hash appears in the context package (3.3) so the agent can verify "I am implementing exactly this spec section, and here is the hash to prove it."

#### 3.1.3 — Dependency Graph Construction

**New file:** `pkg/dispatcher/dependency.go`

```go
// BuildDependencyGraph parses dependency markers in spec sections.
// A section with "Depends on: task-001, task-002" produces edges.
func BuildDependencyGraph(tasks []Task) (*DependencyGraph, error)

type DependencyGraph struct {
    Nodes map[string]*TaskNode
    Edges map[string][]string  // task ID → IDs it depends on
}

// ValidateCycle checks the graph is a DAG. Returns the first cycle if found.
func (dg *DependencyGraph) ValidateCycle() ([]string, bool)

// TopologicalSort returns tasks in dependency order (no task before its deps).
func (dg *DependencyGraph) TopologicalSort() ([]Task, error)
```

The dispatcher uses the topological sort to respect `DependsOn` when dispatching tasks. A task whose dependency is still `pending` is skipped.

#### 3.1.4 — Cost Assignment at Decomposition Time

**Current state:** Cost estimation is a separate `helix estimate check` call. Tasks don't carry their own cost.

**Plan:** Extend `runDispatcherStatus` to call `estimate.Estimator.Estimate(taskDesc)` for each task and attach the result. Add a `--estimate-cost` flag:

```bash
# Decompose with per-task cost estimates
helix dispatcher status --spec specs/feature-login.md --agent '{"name":"wojons","capability":"web","max_load":3}' --tier observed --estimate-cost --json
```

The `runDispatcherStatus` function:
1. Decomposes the spec into tasks.
2. If `--estimate-cost`, builds a `TaskDesc` from each task and calls `estimator.Estimate()`.
3. Populates `Task.EstimatedCost` and `Task.RequiredTier`.

### Acceptance Criteria

| # | Criterion | Verification |
|---|-----------|-------------|
| AC-3.1.1 | `helix dispatcher list-tasks --spec <path>` shows one line per task | Compare output line count to spec section count |
| AC-3.1.2 | Tasks include acceptance criteria parsed from `### Acceptance Criteria` blocks | JSON output contains `acceptance_criteria` array |
| AC-3.1.3 | Each task has a `spec_section_hash` (SHA-256 of its section text) | `echo -n "$section" \| sha256sum` matches |
| AC-3.1.4 | `helix dispatcher status --estimate-cost` populates `estimated_cost` per task | Cost is > 0 and ≤ tier cap |
| AC-3.1.5 | Dependency graph rejects cycles with a clear error | `Dispatched depends on: task-003 (circular)` |
| AC-3.1.6 | Topological sort respects `DependsOn` | task with deps is dispatched after deps are complete |

---

## Interaction Point 3.2 — Agent Assignment

> **Type:** HANDOFF → Dispatcher matches tasks to agents based on trust tier, domain expertise, current load, and cost profile.  
> **Flow:** Tasks are assigned to agents based on trust tier, domain expertise, current load, and cost profile.  
> **Goal:** Human can override; agents cannot self-assign outside their tier.

### Component Responsibility

| Layer | Component | File(s) |
|-------|-----------|---------|
| Assignment engine | `pkg/dispatcher` | `assigner.go` (`AssignAgent`, `Dispatch`) |
| Cost guard | `pkg/dispatcher` | `cost_guard.go` (`CostGuard`, `Check`) |
| Trust tier authority | `pkg/trust` | `tiers.go` (`DetermineTier`), `snapshot.go` |
| Permission expansion | `pkg/identity` | `provisioner.go` (`PermissionExpansion`, `CostCapForTier`) |
| CLI surface | `cmd/helix` | `dispatcher.go` (`runDispatcherTick`) |

### What the Human Sees

```
┌──────────────────────────────────────────────────────────────────┐
│                    AGENT ASSIGNMENT                              │
│  Task: task-001 — Phase 1: Create login form                    │
│  Required tier: Observed | Est. cost: $0.02                     │
├──────────────────────────────────────────────────────────────────┤
│  AGENT        TIER        LOAD    CAPABILITY    TRUST    COST    │
│  wojons       Observed    1/3     web,go        0.72     $0.02   │ ← auto-assigned
│  llopez       Trusted     2/3     web,rust      0.81     $0.02   │
│  dtoole       Provisional 0/3     web,python    0.12     BLOCKED │ ← tier too low
│  jrestrepo    Veteran     1/5     go,rust       0.91     $0.01   │
├──────────────────────────────────────────────────────────────────┤
│  Auto-assigned: wojons (lowest load among capable, tier-met)     │
│                                                                  │
│  [ACCEPT] [REASSIGN TO: llopez ▾] [BLOCK TASK] [FORCE VETERAN]  │
└──────────────────────────────────────────────────────────────────┘
```

Human actions:
- Sees which agents are eligible (meet tier requirement, have capacity, capability match).
- Sees the auto-assigned winner and why (lowest load, tier ok, cost within cap).
- Can reassign to any eligible agent or force a higher-tier agent for risk mitigation.
- Sees BLOCKED agents with the reason (tier too low, overloaded, no capability match).

### What the Agent Sees

The agent receives a `WorkItem` with its `Agent` field populated:

```json
{
  "work_item": {
    "task": { "id": "task-001", "description": "...", "acceptance_criteria": [...] },
    "agent": { "name": "wojons", "capability": "web", "current_load": 1, "max_load": 3 },
    "steps": [
      { "action": "Create login form component", "expected_output": "", "status": "pending" },
      { "action": "Write validation logic", "expected_output": "", "status": "pending" }
    ]
  },
  "cost_guard": {
    "decision": "APPROVED",
    "tier": "observed",
    "cost_cap_per_job": 25.00,
    "estimated_cost": 0.02,
    "reason": "cost $0.02 within tier observed cap $25.00"
  }
}
```

The agent knows:
- It was assigned (not self-assigned).
- The cost guard approved the assignment.
- What steps it must execute.

### What Needs Building

#### 3.2.1 — Tier-Gated Assignment

**Current state:** `AssignAgent` filters by capability and load, but does NOT check trust tier against the task's `RequiredTier`.

**Gap:** An agent could be assigned to a task requiring a higher tier than they hold.

**Plan:** Add a `TierGate` to `AssignAgent`:

```go
// AssignAgent now takes a tier parameter. Agents whose tier is below requiredTier
// are excluded from consideration.
func AssignAgent(task Task, agents []AgentProfile, requiredTier trust.TrustTier) (*DispatchResult, error) {
    // 1. Filter by tier: agent's tier must be >= requiredTier
    var tierOk []AgentProfile
    for _, a := range agents {
        if trust.CompareTiers(a.Tier, requiredTier) >= 0 && a.CanAcceptLoad() {
            tierOk = append(tierOk, a)
        }
    }
    if len(tierOk) == 0 {
        return nil, fmt.Errorf("%w: no agent meets tier %s for task %s", ErrNoCapableAgent, requiredTier, task.ID)
    }
    // 2. Then capability match + load as before
    // ...
}
```

New type in `pkg/trust/tiers.go`:

```go
// CompareTiers returns -1 if a < b, 0 if equal, 1 if a > b.
// Veteran > Trusted > Observed > Provisional.
func CompareTiers(a, b TrustTier) int { ... }

// TierOrdinal maps a tier to its numeric rank for comparison.
var TierOrdinal = map[TrustTier]int{
    TierProvisional: 0,
    TierObserved:    1,
    TierTrusted:     2,
    TierVeteran:     3,
}
```

#### 3.2.2 — Agent Profile with Tier and Trust Score

**Current state:** `AgentProfile` has `Name`, `Capability`, `CurrentLoad`, `MaxLoad`. No tier or trust score.

**Plan:** Extend `AgentProfile` in `pkg/dispatcher/types.go`:

```go
type AgentProfile struct {
    Name        string          `json:"name"`
    Capability  string          `json:"capability"`
    CurrentLoad int             `json:"current_load"`
    MaxLoad     int             `json:"max_load"`
    Tier        trust.TrustTier `json:"tier"`          // NEW
    TrustScore  float64         `json:"trust_score"`   // NEW
    CostProfile float64         `json:"cost_profile"`  // NEW: avg cost per task (USD)
}
```

The `parseAgentJSON` helper in `cmd/helix/dispatcher.go` already accepts arbitrary JSON fields — it just needs the new fields defined.

When loading agents from the trust ledger, `pkg/trust/snapshot.go`'s `GetSnapshot` returns tier and score.

#### 3.2.3 — Cost Guard Integration in Assignment

**Current state:** `runDispatcherTick` calls `disp.AssignAgent` then `disp.ExecuteLoop` — no cost guard between them.

**Plan:** Insert the cost guard between assignment and execution in `runDispatcherTick`:

```go
func runDispatcherTick(flags dispFlags, stdout, stderr io.Writer) int {
    // ... parse spec, decompose, parse agent ...
    
    // NEW: cost guard check before execution
    tier, err := parseTrustTier(flags.tier)
    td := estimate.TaskDesc{
        Description:  tasks[0].Description,
        Type:         estimate.TaskCode,
        FilesChanged: 1,
    }
    cgResult, err := evaluateCostGuard(&agent, tier, tasks)
    if cgResult.IsBlocked() {
        fmt.Fprintf(stderr, "BLOCKED: %s\n", cgResult.Reason)
        return dispExitBlock
    }
    if cgResult.IsEscalated() {
        fmt.Fprintf(stderr, "ESCALATED: requires human approval — %s\n", cgResult.Reason)
        return dispExitBlock
    }
    
    // ... assign and execute ...
}
```

The cost guard tier caps (from `pkg/identity/provisioner.go`) are:
- Provisional: $5/job
- Observed: $25/job
- Trusted: $100/job
- Veteran: unlimited (-1 sentinel)

#### 3.2.4 — Human Override Interface

**New CLI subcommand:** `helix dispatcher reassign`

```bash
# View current assignment
helix dispatcher status --spec specs/feature-login.md --agent '{"name":"wojons","capability":"web","max_load":3}' --tier observed

# Reassign task-001 to llopez (overrides auto-assignment)
helix dispatcher reassign task-001 --agent llopez --tier trusted

# Force-assign even if tier normally insufficient (human override, logged)
helix dispatcher reassign task-001 --agent dtoole --force --reason "Training task for new agent"
```

A `--force` override is recorded in the audit chain (`pkg/audit/chain.go`).

### Acceptance Criteria

| # | Criterion | Verification |
|---|-----------|-------------|
| AC-3.2.1 | Agent with tier below `required_tier` is never auto-assigned | Unit test: Provisional agent + Trusted task → `ErrNoCapableAgent` |
| AC-3.2.2 | Cost guard BLOCKED stops execution before any steps run | Exit code 1, no worktree directory created |
| AC-3.2.3 | Agent with highest trust + lowest load wins when capability matches | Deterministic test with 3 agents: tier-tie → load-tie → alphabetical |
| AC-3.2.4 | `helix dispatcher reassign --force` logs the human override to audit | Audit chain entry with `event_type: "human_override"` |
| AC-3.2.5 | Overloaded agent (current_load ≥ max_load) is skipped | Unit test: agent at 3/3 load → not in candidates |

---

## Interaction Point 3.3 — Context Packaging

> **Type:** HANDOFF → Automated context assembly from spec links, codebase indexing, incident history, and marketplace knowledge.  
> **Flow:** When a task is assigned, the agent receives a context package.  
> **Goal:** Context is budget-constrained (fits in model window). Agent can request more; each expansion costs tokens.

### Component Responsibility

| Layer | Component | File(s) |
|-------|-----------|---------|
| Context assembler | `pkg/dispatcher` | **NEW:** `context.go` |
| Codebase index | `pkg/dispatcher` | **NEW:** `indexer.go` |
| Incident store | `pkg/security` | `incident_store.go` |
| Spec linkage | `pkg/dispatcher` | `decomposer.go` (section hashing) |
| Budget tracking | `pkg/estimate` | `budget.go` |

### What the Human Sees

```
┌──────────────────────────────────────────────────────────────────┐
│                    CONTEXT PACKAGE                               │
│  Task: task-001 — Phase 1: Create login form                    │
│  Agent: wojons (Observed)                                       │
├──────────────────────────────────────────────────────────────────┤
│  INCLUDED IN PACKAGE                     TOKENS (est.)           │
│  ───────────────────────────────────────────────────────────────│
│  ✓ Spec section (§2.1)                   1,200 tokens            │
│  ✓ Acceptance criteria (4 items)          180 tokens             │
│  ✓ Related ADR (ADR-003: Auth pattern)    850 tokens             │
│  ✓ Prior PR (#142: auth refactor)        2,100 tokens            │
│  ✓ Incident #47 (session fixation)         320 tokens            │
│  ✓ Codebase files (3 files)              4,800 tokens            │
│  ───────────────────────────────────────────────────────────────│
│  Total context: ~9,450 tokens (max: 12,000)                     │
│                                                                  │
│  EXPANDABLE (not included, cost on request)                     │
│  ○ Full codebase index (35,000 tokens)                          │
│  ○ Incident history (all auth bugs: 2,400 tokens)               │
│  ○ Prior PRs (last 10 auth PRs: 8,500 tokens)                   │
│                                                                  │
│  [DISPATCH WITH THIS CONTEXT] [EXPAND...] [CUSTOMIZE]           │
└──────────────────────────────────────────────────────────────────┘
```

Human actions:
- Reviews what context the agent will receive.
- Can expand the package (add more prior PRs, more codebase files).
- Sees the token budget and the cost of each expansion.

### What the Agent Sees

The agent receives a `ContextPackage` as a single concatenated prompt with delimiters:

```
=== HELIX TASK CONTEXT ===
Task: task-001
Spec Section: Phase 1: Create login form (hash: sha256:a1b2c3...)
Agent: wojons | Tier: observed | Cost Cap: $25.00/job

=== ACCEPTANCE CRITERIA ===
- AC-1: Email field validates RFC 5322 addresses
- AC-2: Password field minimum 8 characters
- AC-3: Submit button disabled until both fields are valid
- AC-4: Error states are announced by screen readers

=== SPEC SECTION ===
[full text of spec section §2.1]

=== RELEVANT ADR ===
ADR-003: Auth Pattern — bcrypt for passwords, JWT for sessions
[full ADR text]

=== PRIOR PR (#142) ===
Title: auth refactor — extract token validation
Diff: [summarized diff]

=== INCIDENT #47 ===
Type: session_fixation | Severity: high | Date: 2026-05-12
Causal chain: missing SameSite=Strict cookie attribute
Resolution: added SameSite=Strict and Secure flags

=== CODEBASE FILES ===
File: src/auth/login.ts [full content]
File: src/auth/validation.ts [full content]
File: src/components/Form.tsx [full content]

=== CONTEXT BUDGET ===
Used: 9,450 / 12,000 tokens
Request more: reply with "EXPAND <resource_name>"
```

The agent can request expansions by replying with `EXPAND <resource>`. Each expansion is logged and charged against the task's token budget.

### What Needs Building

#### 3.3.1 — Context Package Struct

**New file:** `pkg/dispatcher/context.go`

```go
// ContextPackage is the fully assembled context sent to an agent for a task.
type ContextPackage struct {
    TaskID         string              `json:"task_id"`
    AgentID        string              `json:"agent_id"`
    SpecSection    ContextResource     `json:"spec_section"`
    AcceptanceCriteria []ContextResource `json:"acceptance_criteria"`
    ADRs           []ContextResource   `json:"adrs"`
    PriorPRs       []ContextResource   `json:"prior_prs"`
    Incidents      []ContextResource   `json:"incidents"`
    CodeFiles      []ContextResource   `json:"code_files"`
    TotalTokens    int64               `json:"total_tokens_est"`
    TokenBudget    int64               `json:"token_budget"`
    Expandable     []ExpandableResource `json:"expandable"`
}

// ContextResource is a single piece of context with estimated token count.
type ContextResource struct {
    Type        string `json:"type"`         // "spec_section", "adr", "pr", "incident", "code_file"
    Title       string `json:"title"`        // Human label
    Content     string `json:"content"`      // Full text
    TokenEst    int64  `json:"token_est"`    // Estimated tokens (content length / 4)
    SourceHash  string `json:"source_hash"`  // SHA-256 for integrity verification
}

// ExpandableResource is a resource available on agent request.
type ExpandableResource struct {
    ID          string `json:"id"`
    Title       string `json:"title"`
    TokenEst    int64  `json:"token_est"`
    TokenCost   float64 `json:"token_cost_usd"`
}
```

#### 3.3.2 — Context Assembler

**New function** in `context.go`:

```go
// AssembleContext builds a ContextPackage for a task against a budget.
// It queries the spec, codebase index, incident store, and ADR registry.
// The budget (max tokens) constrains how much context fits.
func AssembleContext(task Task, agent AgentProfile, budgetTokens int64) (*ContextPackage, error)
```

Assembly algorithm:
1. **Always included** (unconditional):
   - Spec section referenced by `task.SpecRef` + `task.SpecSectionHash`
   - Acceptance criteria from `task.AcceptanceCriteria`
2. **Included if within budget** (priority order):
   - ADRs matching keywords in task description (tag match)
   - Prior PRs that touched files in the codebase index related to the task
   - Incidents matching the same domain (e.g., "auth" → auth incidents)
   - Codebase files: the top N files by relevance, sorted by TF-IDF of task description against file content
3. **Expandable** (excluded from initial package):
   - Full codebase index
   - All incidents for the domain
   - All prior PRs for the domain

The budget check: `totalTokens + nextResource.TokenEst > budgetTokens` → skip and add to `Expandable`.

#### 3.3.3 — Codebase Indexer

**New file:** `pkg/dispatcher/indexer.go`

```go
// CodebaseIndex maps file paths to tokenized content for TF-IDF matching.
type CodebaseIndex struct {
    Files    map[string]IndexedFile
    Inverted map[string][]string  // token → file paths
}

type IndexedFile struct {
    Path       string
    TokenCount int64
    Tokens     []string
    ImportedBy []string  // files that import this file
}

// IndexRepo walks a repository and builds a CodebaseIndex.
func IndexRepo(repoPath string, ignorePatterns []string) (*CodebaseIndex, error)

// Search finds the top N files most relevant to a query (task description).
func (idx *CodebaseIndex) Search(query string, topN int) []string
```

Ignored patterns: `node_modules/`, `.git/`, `vendor/`, `dist/`, `*.min.js`, `*.generated.*`.

CLI command:

```bash
# Index the current repo
helix dispatcher index --path . --output ~/.helix/indexes/myproject.json

# Search the index for a query
helix dispatcher search-index --index ~/.helix/indexes/myproject.json --query "login form validation" --top 5
```

#### 3.3.4 — Token Budget Per Tier

Token budgets are tier-gated to prevent context overflow:

| Tier | Max Context Tokens | Max Expand Tokens | Total Cap |
|------|--------------------|--------------------|------------|
| Provisional | 8,000 | 4,000 | 12,000 |
| Observed | 16,000 | 8,000 | 24,000 |
| Trusted | 32,000 | 16,000 | 48,000 |
| Veteran | 64,000 | 32,000 | 96,000 |

These are configurable in `~/.helix/config.yaml`:

```yaml
context:
  budgets:
    provisional: 12000
    observed: 24000
    trusted: 48000
    veteran: 96000
  expand_multiplier: 1.5  # expansion costs 1.5x normal token rate (disincentivizes waste)
```

### Acceptance Criteria

| # | Criterion | Verification |
|---|-----------|-------------|
| AC-3.3.1 | Context package always includes spec section and ACs | Unit test: `AssembleContext` returns non-empty `SpecSection` and `AcceptanceCriteria` |
| AC-3.3.2 | Context stays within token budget | `TotalTokens ≤ TokenBudget` for every assembled package |
| AC-3.3.3 | Expandable resources are listed with token costs | `Expandable[]` populated when resources exceed budget |
| AC-3.3.4 | Codebase index can find relevant files by task description | Search "login form validation" returns `src/auth/login.ts`, `src/auth/validation.ts` |
| AC-3.3.5 | Index ignores build artifacts and dependencies | `node_modules/` files never appear in search results |
| AC-3.3.6 | Token budget is tier-gated (Provisional < Observed < Trusted < Veteran) | Verify budget values from config match tier |

---

## Interaction Point 4.1 — Code Generation

> **Type:** HANDOFF → Agent writes code against spec, ACs, and context package in an isolated worktree.  
> **Flow:** Sandboxed worktrees per agent per task. Bubblewrap isolation. No cross-task contamination.  
> **Goal:** Agent's entire session is traceable.

### Component Responsibility

| Layer | Component | File(s) |
|-------|-----------|---------|
| Sandbox executor | `pkg/sandbox` | `executor.go` (`BwrapExecutor`), `types.go` (`SandboxConfig`) |
| Resource tracker | `pkg/sandbox` | `usage.go` (`ResourceUsageTracker`, `UsageReport`) |
| Worktree manager | `pkg/dispatcher` | `loop.go` (`ExecuteLoop`, `acquireLock`, `commitWork`) |
| Forgejo dispatch | `pkg/dispatcher` | `forgejo_loop.go` (`ForgejoLoop`, `Run`) |
| Step execution | `pkg/dispatcher` | `loop.go` (`executeStep`) |

### What the Human Sees

```
┌──────────────────────────────────────────────────────────────────┐
│                    CODE GENERATION — LIVE                        │
│  Task: task-001 — Create login form                   Agent: wojons
│  Branch: feature/wojons-task-001                      Started: 14:32:05 UTC
├──────────────────────────────────────────────────────────────────┤
│  STEP                            STATUS          DURATION        │
│  ───────────────────────────────────────────────────────────────│
│  Create login form component     ✓ COMPLETE      2.3s            │
│  Write validation logic          ▶ IN PROGRESS   1.1s (running)  │
│  Add accessible error states     ○ PENDING                       │
│  Write unit tests                ○ PENDING                       │
├──────────────────────────────────────────────────────────────────┤
│  SANDBOX RESOURCES                                             │
│  Memory:  124 MB / 2048 MB     CPU: 0.8s      Network: 0      │
│  Fs writes: 3 files           OOM: 0         Time limit: 9:52 │
├──────────────────────────────────────────────────────────────────┤
│  [PAUSE] [CANCEL] [VIEW FILES] [INSPECT SANDBOX]               │
└──────────────────────────────────────────────────────────────────┘
```

After completion:

```
┌──────────────────────────────────────────────────────────────────┐
│                    CODE GENERATION — COMPLETE                    │
│  Task: task-001 | Duration: 18.4s | PR: #231 (opened)           │
├──────────────────────────────────────────────────────────────────┤
│  Files created: src/auth/LoginForm.tsx (142 lines)              │
│                 src/auth/validation.ts (68 lines)                │
│                 src/auth/LoginForm.test.tsx (94 lines)          │
│                                                                  │
│  Resources used:  142 MB peak memory | 3.2s CPU                 │
│  Tokens consumed: ~4,200 input | ~2,800 output | Cost: $0.01    │
│                                                                  │
│  [VIEW PR] [VIEW DIFF] [RUN TESTS] [DISMISS]                    │
└──────────────────────────────────────────────────────────────────┘
```

### What the Agent Sees

The agent operates inside a sandboxed worktree. Its session:

1. Receives the `ContextPackage` (3.3) as its initial prompt.
2. Executes each `Step` from the `WorkItem`.
3. For each step, it reads the task ACs, writes code to the worktree, and moves to the next step.
4. The sandbox enforces:
   - No network access (`--unshare-net`)
   - Read-only `/usr`, `/bin`, `/lib`, `/lib64`
   - Writable only: `/workspace` (the worktree), `/tmp`
   - Memory limit: tier-dependent (Provisional: 512 MB, Observed: 2 GB, Trusted: 4 GB, Veteran: 8 GB)
   - Time limit: tier-dependent (Provisional: 5 min, Observed: 10 min, Trusted: 30 min, Veteran: 60 min)
5. On completion, the agent writes a `COMMIT_MSG` and the dispatcher creates the branch + PR on Forgejo.

### What Needs Building

#### 4.1.1 — Wired Sandbox Execution

**Current state:** `executeStep` in `loop.go` writes a marker file — it doesn't actually run code generation. The real bwrap execution in `pkg/sandbox/executor.go` returns `ErrNotImplemented`.

**Gap:** The sandbox exec stub needs to be wired to actually invoke `bwrap`.

**Plan:**

1. Wire `BwrapExecutor.Run()` to call `exec.CommandContext` with the constructed bwrap args (currently stubbed behind `ErrNotImplemented`):

```go
// In pkg/sandbox/executor.go — replace ErrNotImplemented with real execution:
func (e *BwrapExecutor) Run(ctx context.Context, cfg SandboxConfig) (*RunResult, error) {
    // 1. Create session directory.
    // 2. Build bwrap arguments from isolation level.
    // 3. exec.CommandContext(ctx, cfg.BwrapPath, args...)
    // 4. Write PID to cgroup.procs if cgroup enabled.
    // 5. Capture stdout/stderr.
    // 6. On context deadline → killProcessGroup.
    // 7. Return RunResult with exit code + output.
}

type RunResult struct {
    ExitCode int
    Stdout   string
    Stderr   string
    Duration time.Duration
    Pid      int
}
```

2. Integrate sandbox execution into `ForgejoLoop.Run()`:

```go
// In forgejo_loop.go — between step marker writing and commit:
// 4b. Execute agent in sandbox
sandboxCfg := sandbox.SandboxConfig{
    SessionID:   fmt.Sprintf("helix-%s-%s", task.ID, agent),
    Isolation:   sandbox.IsolationWorkspace,
    Workdir:     "/workspace",
    TimeLimit:   tierTimeLimit(tier),   // e.g., 600 for Observed
    MemoryLimit: tierMemoryLimit(tier), // e.g., 2048 for Observed
    Network:     sandbox.NetworkNone,
    Command:     []string{"helix-agent", "execute", "--task", taskJSON, "--context", contextJSON},
    SessionRoot: filepath.Join(outcome.WorktreePath, "sandbox"),
}
executor := sandbox.NewBwrapExecutor()
result, err := executor.Run(ctx, sandboxCfg)
```

3. The agent CLI (`cmd/helix-agent` or equivalent) receives the task + context as JSON via stdin or temp files, runs the LLM loop to generate code, and writes results to the worktree.

#### 4.1.2 — Tier-Gated Sandbox Limits

**New file:** `pkg/dispatcher/sandbox_limits.go`

```go
// SandboxLimits returns the appropriate sandbox config for a given trust tier.
func SandboxLimits(tier trust.TrustTier) (timeLimitSec, memoryLimitMB int, isolation sandbox.IsolationLevel) {
    switch tier {
    case trust.TierProvisional:
        return 300, 512, sandbox.IsolationFull
    case trust.TierObserved:
        return 600, 2048, sandbox.IsolationWorkspace
    case trust.TierTrusted:
        return 1800, 4096, sandbox.IsolationWorkspace
    case trust.TierVeteran:
        return 3600, 8192, sandbox.IsolationWorkspace
    default:
        return 300, 512, sandbox.IsolationFull
    }
}
```

Provisional agents get **full isolation** (no GPU, cleared environment). Higher tiers get workspace isolation with GPU pass-through.

#### 4.1.3 — Per-Step Traceability

**Current state:** `executeStep` writes a marker file but doesn't capture `UsageReport`.

**Plan:** After each step executes, call `ResourceUsageTracker.Sample(sessionID)` and append the report to the work item's step history:

```go
type Step struct {
    Action         string              `json:"action"`
    ExpectedOutput string              `json:"expected_output"`
    Status         StepStatus          `json:"status"`
    UsageReport    *sandbox.UsageReport `json:"usage_report,omitempty"` // NEW
    StartedAt      string              `json:"started_at,omitempty"`    // NEW
    CompletedAt    string              `json:"completed_at,omitempty"`  // NEW
}
```

Every step's resource consumption is recorded in the `DispatchOutcome`, providing a full trace: "step 2 (validation logic) used 124 MB memory and 1.1s CPU."

#### 4.1.4 — Dry-Run Mode for Agent Work

```bash
# Dry-run: plan branches and PRs but don't create them
helix dispatch --spec specs/feature-login.md --agent wojons --dry-run --json

# Output:
# {
#   "spec_path": "specs/feature-login.md",
#   "agent": "wojons",
#   "task_id": "task-001",
#   "branch_name": "feature/wojons-task-001",
#   "pr_url": "http://localhost:3030/owner/repo/compare/main...feature/wojons-task-001",
#   "dry_run": true,
#   "mode": "dry-run"
# }
```

This is already partially implemented in `ForgejoLoop` via `DryRun` field — needs the CLI flag wired in `cmd/helix/dispatch.go`.

### Acceptance Criteria

| # | Criterion | Verification |
|---|-----------|-------------|
| AC-4.1.1 | Agent code generation runs inside `bwrap` sandbox (not host) | `cat /proc/self/mountinfo` from sandbox shows only worktree + system mounts |
| AC-4.1.2 | Network access is blocked in sandbox | `curl` or `ping` from sandbox fails |
| AC-4.1.3 | Memory limit enforcement kills over-limit processes | Allocate > limit → OOM-kill, `UsageReport.ExceededMemory = true` |
| AC-4.1.4 | Time limit enforcement kills long-running tasks | `sleep 999` in sandbox → killed at time limit, exit code 6 |
| AC-4.1.5 | Provisional agents run in `full` isolation, Observed+ in `workspace` | GPU devices present in Observed sandbox, absent in Provisional |
| AC-4.1.6 | Each step's resource usage is captured in `Step.UsageReport` | After tick: `steps[0].usage_report.peak_memory_bytes > 0` |
| AC-4.1.7 | Dry-run mode produces planned branch/PR URLs without network calls | `--dry-run` output matches expected URL format, no Forgejo API call |

---

## Interaction Point 4.2 — In-Progress Collaboration

> **Type:** COLLABORATION → Agent encounters ambiguity → requests clarification. Human or another agent responds.  
> **Flow:** Agent files a `CLARIFICATION_NEEDED` with specific question, context, and blocked progress.  
> **Goal:** Resolution is linked to task and spec for audit.

### Component Responsibility

| Layer | Component | File(s) |
|-------|-----------|---------|
| Clarification protocol | `pkg/dispatcher` | **NEW:** `clarification.go` |
| Forgejo integration | `pkg/dispatcher` | `forgejo_loop.go` (PR comment posting) |
| Audit chain | `pkg/audit` | `chain.go` |
| Human notification | `pkg/health` | `notifier.go` (alerts) |

### What the Human Sees

```
┌──────────────────────────────────────────────────────────────────┐
│                 CLARIFICATION NEEDED                             │
│  Task: task-001 — Create login form                   Agent: wojons
│  Blocked at step 2 of 4 (Write validation logic)                │
│  Elapsed: 4m 32s | Cost so far: $0.005                          │
├──────────────────────────────────────────────────────────────────┤
│  QUESTION FROM AGENT:                                            │
│  "The spec says 'validate email format' but doesn't specify     │
│   whether to use HTML5 constraint validation, a regex, or a     │
│   third-party library. Which approach should I use?"            │
│                                                                  │
│  Context:                                                        │
│  - Spec section §2.1, line 15: 'Email field validates format'   │
│  - Existing code at src/utils/email.ts uses regex validation    │
│  - ADR-007 recommends 'prefer platform APIs over regex for      │
│    validation when available'                                    │
│                                                                  │
│  SUGGESTED ANSWER (auto-drafted from context):                  │
│  "Use HTML5 constraint validation (type='email' + pattern       │
│   attribute) per ADR-007. Fall back to the existing regex in    │
│   src/utils/email.ts only for non-HTML5 contexts (e.g., Node    │
│   server-side validation)."                                      │
│                                                                  │
│  [ACCEPT SUGGESTION] [ANSWER MANUALLY] [ESCALATE TO TRUSTED]    │
└──────────────────────────────────────────────────────────────────┘
```

After resolution:

```
┌──────────────────────────────────────────────────────────────────┐
│  RESOLVED — Agent resumed.                                       │
│  Answer: "Use HTML5 constraint validation per ADR-007."         │
│  Resolved by: human (kara) | Time to resolve: 2m 18s            │
│  Resolution linked to task-001, spec §2.1, ADR-007              │
│  [VIEW AUDIT TRAIL]                                              │
└──────────────────────────────────────────────────────────────────┘
```

Human actions:
- Sees the agent's question with full context and a suggested answer.
- Can accept the suggestion, answer manually, or escalate to a more trusted agent.
- Every clarification is audited: who asked, who answered, what was decided.

### What the Agent Sees

When blocked, the agent emits a `ClarificationRequest`:

```json
{
  "type": "CLARIFICATION_NEEDED",
  "task_id": "task-001",
  "blocked_step": 2,
  "question": "The spec says 'validate email format' but doesn't specify whether to use HTML5 constraint validation, a regex, or a third-party library. Which approach should I use?",
  "context": {
    "spec_section": "Phase 1: Create login form, §2.1 line 15",
    "spec_section_hash": "sha256:a1b2c3...",
    "relevant_code": "src/utils/email.ts (regex-based validation)",
    "relevant_adr": "ADR-007: Prefer platform APIs over regex"
  },
  "suggested_answer": "Use HTML5 constraint validation per ADR-007.",
  "blocked_since": "2026-07-07T14:36:37Z"
}
```

The agent then polls for a `ClarificationResponse`:

```json
{
  "type": "CLARIFICATION_RESOLVED",
  "task_id": "task-001",
  "resolution": "Use HTML5 constraint validation per ADR-007. Fall back to regex for server-side.",
  "resolved_by": "human:kara",
  "resolved_at": "2026-07-07T14:38:55Z"
}
```

### What Needs Building

#### 4.2.1 — Clarification Protocol Types

**New file:** `pkg/dispatcher/clarification.go`

```go
// ClarificationRequest is emitted by an agent when it encounters ambiguity.
type ClarificationRequest struct {
    Type            string              `json:"type"`             // always "CLARIFICATION_NEEDED"
    TaskID          string              `json:"task_id"`
    BlockedStep     int                 `json:"blocked_step"`
    Question        string              `json:"question"`
    Context         ClarificationContext `json:"context"`
    SuggestedAnswer string              `json:"suggested_answer"`
    BlockedSince    string              `json:"blocked_since"`   // ISO8601
}

type ClarificationContext struct {
    SpecSection     string `json:"spec_section"`
    SpecSectionHash string `json:"spec_section_hash"`
    RelevantCode    string `json:"relevant_code"`
    RelevantADR     string `json:"relevant_adr"`
}

// ClarificationResponse resolves a pending clarification.
type ClarificationResponse struct {
    Type       string `json:"type"`        // "CLARIFICATION_RESOLVED"
    TaskID     string `json:"task_id"`
    Resolution string `json:"resolution"`
    ResolvedBy string `json:"resolved_by"` // "human:<name>" or "agent:<name>"
    ResolvedAt string `json:"resolved_at"` // ISO8601
}

// ClarificationStore manages pending clarifications.
type ClarificationStore struct {
    path string // ~/.helix/clarifications/<task-id>.json
}
```

#### 4.2.2 — Clarification Flow in ForgejoLoop

**Plan:** Insert a clarification check between step execution in `ForgejoLoop.Run()`:

```go
// In forgejo_loop.go Run(), after each step:
for i := range steps {
    steps[i].Status = StepInProgress
    if err := executeStep(steps[i], outcome.WorktreePath); err != nil {
        // Check if error is a clarification request
        if clar, ok := IsClarificationRequest(err); ok {
            // Post clarification as PR comment on Forgejo
            f.postClarification(ctx, outcome, clar)
            // Poll for resolution
            resolution, pollErr := f.pollForResolution(ctx, outcome.TaskID, 30*time.Minute)
            if pollErr != nil {
                return outcome, fmt.Errorf("clarification timed out: %w", pollErr)
            }
            // Retry step with resolution
            continue
        }
        steps[i].Status = StepFailed
        return outcome, fmt.Errorf("step %d failed: %w", i, err)
    }
    steps[i].Status = StepComplete
}
```

#### 4.2.3 — Human Notification

When a clarification is posted, `pkg/health/notifier.go` fires an alert:

```go
// ClarificationAlert sends a notification to the human operator.
func (n *Notifier) ClarificationAlert(taskID, agentID, question string) error {
    msg := fmt.Sprintf("[HELIX] Agent %s needs clarification on task %s:\n\n%s\n\nReply with: helix dispatcher clarify %s --answer \"...\"",
        agentID, taskID, question, taskID)
    return n.Send(AlertLevelInfo, "clarification_needed", msg)
}
```

Human resolves via CLI:

```bash
# View pending clarifications
helix dispatcher clarifications list

# Resolve a clarification
helix dispatcher clarify task-001 --answer "Use HTML5 constraint validation per ADR-007."

# Delegate to a trusted agent
helix dispatcher clarify task-001 --delegate-to agent:llopez
```

#### 4.2.4 — Auto-Resolution from Context

Before escalating to human, the agent should attempt self-resolution by querying the `ClarificationContext`:

1. Check relevant ADRs → if an ADR directly answers the question, auto-resolve.
2. Check prior similar clarifications (from `ClarificationStore`) → if same question was answered before, reuse.
3. Check agent marketplace knowledge → if a skill covers this pattern, apply it.

Only if all three fail does the agent escalate to human.

```go
// AutoResolve attempts to answer a clarification without human involvement.
func AutoResolve(req ClarificationRequest, adrStore ADRStore, clarStore *ClarificationStore, skillIndex SkillIndex) (string, bool) {
    // 1. ADR match
    if answer, ok := adrStore.FindAnswer(req.Question, req.Context.RelevantADR); ok {
        return answer, true
    }
    // 2. Prior clarification match
    if answer, ok := clarStore.FindSimilar(req.Question, req.TaskID); ok {
        return answer, true
    }
    // 3. Skill match
    if answer, ok := skillIndex.FindAnswer(req.Question, req.Context.RelevantCode); ok {
        return answer, true
    }
    return "", false
}
```

### Acceptance Criteria

| # | Criterion | Verification |
|---|-----------|-------------|
| AC-4.2.1 | Agent emits structured `ClarificationRequest` JSON when blocked | Simulate ambiguity → JSON output matches schema |
| AC-4.2.2 | Clarification is posted as PR comment on Forgejo | Check PR #N for comment with `CLARIFICATION_NEEDED` marker |
| AC-4.2.3 | Human can resolve via `helix dispatcher clarify --answer` | Agent resumes execution within 10s of resolution |
| AC-4.2.4 | Auto-resolution works when ADR or prior clarification matches | No human notification fired for auto-resolved clarifications |
| AC-4.2.5 | Clarification timeout (default 30 min) fails the task gracefully | Task status → `failed`, error: `clarification timed out` |
| AC-4.2.6 | Every clarification + resolution is audited | `AuditChain` has `clarification_requested` and `clarification_resolved` events |

---

## Interaction Point 4.3 — Self-Verification

> **Type:** GATE → Agent runs tests, lint, build before considering work complete.  
> **Flow:** Pre-commit verification enforced at agent level. Agent cannot mark task complete until GitReins Tier 1 passes.  
> **Goal:** Evidence is captured.

### Component Responsibility

| Layer | Component | File(s) |
|-------|-----------|---------|
| GitReins integration | `cmd/helix` | `verify.go` (`runVerify`) |
| CI workflow engine | `pkg/ci` | `workflow.go` |
| Evidence bundle | `pkg/audit` | `builder/persist.go` |
| Step execution | `pkg/dispatcher` | `loop.go` |

### What the Human Sees

```
┌──────────────────────────────────────────────────────────────────┐
│                 SELF-VERIFICATION                                │
│  Task: task-001 — Create login form                   Agent: wojons
│  Branch: feature/wojons-task-001                                │
├──────────────────────────────────────────────────────────────────┤
│  CHECK                           STATUS          DETAILS         │
│  ───────────────────────────────────────────────────────────────│
│  Secrets scan (gitleaks)         ✓ PASS          0 findings      │
│  Lint (eslint)                   ✓ PASS          0 errors        │
│  Tests (vitest, diff mode)       ✓ PASS          12/12 passed    │
│  Build (tsc)                     ✓ PASS          no errors       │
│  ───────────────────────────────────────────────────────────────│
│  ALL TIER 1 CHECKS: ✓ PASS                                       │
│  Evidence bundle: sha256:d4e5f6...  (8 files, 2.1 KB)           │
│                                                                  │
│  [VIEW EVIDENCE] [RE-RUN CHECKS] [OVERRIDE] [CANCEL]            │
└──────────────────────────────────────────────────────────────────┘
```

After failure:

```
┌──────────────────────────────────────────────────────────────────┐
│                 SELF-VERIFICATION — FAILED                       │
│  Task: task-001 | Agent: wojons                                 │
├──────────────────────────────────────────────────────────────────┤
│  CHECK                           STATUS          DETAILS         │
│  ───────────────────────────────────────────────────────────────│
│  Secrets scan                    ✓ PASS          0 findings      │
│  Lint                            ✗ FAIL          3 errors        │
│    src/auth/LoginForm.tsx:45 — 'handleSubmit' is defined but never used
│    src/auth/LoginForm.tsx:78 — Missing return type annotation   │
│    src/auth/validation.ts:12 — 'emailRegex' is assigned but never used
│  Tests                           ✓ PASS          12/12 passed    │
│  Build                           — SKIPPED       lint failed     │
│  ───────────────────────────────────────────────────────────────│
│  AGENT MUST FIX LINT ERRORS BEFORE RETRYING.                     │
│  Attempt 1 of 3. Agent notified automatically.                  │
└──────────────────────────────────────────────────────────────────┘
```

Human actions:
- Sees the gate status for all checks.
- Can view evidence bundle (what exactly was checked, exact output).
- Can override (human gate override, logged to audit).
- Failed checks → agent retries automatically up to 3 attempts.

### What the Agent Sees

The agent's `verify` step in the Ralph Loop:

1. Runs `helix verify --worktree <path>` inside the sandbox.
2. Receives a structured result:

```json
{
  "verification": {
    "status": "pass",
    "checks": [
      {"name": "secrets", "status": "pass", "findings": 0},
      {"name": "lint", "status": "pass", "errors": 0, "warnings": 0},
      {"name": "test", "status": "pass", "passed": 12, "failed": 0, "skipped": 0},
      {"name": "build", "status": "pass", "errors": 0}
    ],
    "evidence_bundle_hash": "sha256:d4e5f6...",
    "attempt": 1,
    "retries_remaining": 2
  }
}
```

If any check fails, the agent receives the failure details and must fix the code before retrying. The agent cannot mark the task complete until all checks pass.

### What Needs Building

#### 4.3.1 — Verification Step in Ralph Loop

**Current state:** The `ExecuteLoop` and `ForgejoLoop.Run()` execute steps but have no verification gate between step completion and commit/PR creation.

**Plan:** Add a verification step to `ForgejoLoop.Run()` after all code steps complete but before `commitWork`:

```go
// In forgejo_loop.go Run(), after step 4 (execute steps):
// 5a. Self-verification gate
verifyResult, err := f.runVerification(ctx, outcome)
if err != nil || verifyResult.Status != "pass" {
    outcome.Steps = steps
    return outcome, fmt.Errorf("forgejo_loop: verification failed: %v", verifyResult)
}

// 5b. Commit stub (only if verification passed).
```

The `runVerification` method:

```go
func (f *ForgejoLoop) runVerification(ctx context.Context, outcome *DispatchOutcome) (*ci.VerificationResult, error) {
    // Build verification config
    vcfg := ci.VerificationConfig{
        WorktreePath: outcome.WorktreePath,
        SandboxPath:  filepath.Join(outcome.WorktreePath, "sandbox"),
        Checks:       []string{"secrets", "lint", "test", "build"},
        TestMode:     "diff",         // only tests affected by changed files
        MaxRetries:   3,
        EvidencePath: filepath.Join(outcome.WorktreePath, "evidence.json"),
    }
    
    // Run via sandbox
    sandboxCfg := sandbox.SandboxConfig{
        Isolation:   sandbox.IsolationWorkspace,
        Command:     []string{"helix", "verify", "--worktree", outcome.WorktreePath, "--json"},
        TimeLimit:   300,
        MemoryLimit: 2048,
    }
    
    return ci.RunVerification(ctx, sandboxCfg, vcfg)
}
```

#### 4.3.2 — GitReins Tier 1 Integration

**Current state:** `cmd/helix/verify.go` has a `runVerify` function that calls `gitreins guard` as an external process. The `pkg/ci/workflow.go` provides a structured workflow engine.

**Plan:** Wire `ci.Workflow` to call GitReins directly via subprocess:

```go
// In pkg/ci/workflow.go:

type VerificationConfig struct {
    WorktreePath string
    Checks       []string  // "secrets", "lint", "test", "build"
    TestMode     string    // "diff" or "full"
    MaxRetries   int
    EvidencePath string
}

type VerificationResult struct {
    Status          string              `json:"status"`   // "pass" | "fail"
    Checks          []CheckResult       `json:"checks"`
    EvidenceBundle  EvidenceBundle      `json:"evidence_bundle"`
    Attempt         int                 `json:"attempt"`
    RetriesRemaining int                `json:"retries_remaining"`
}

type CheckResult struct {
    Name      string `json:"name"`
    Status    string `json:"status"`   // "pass" | "fail" | "skipped"
    Output    string `json:"output"`   // raw stdout/stderr
    Duration  string `json:"duration"` // e.g. "2.3s"
    Findings  int    `json:"findings"` // for secrets/lint
    Passed    int    `json:"passed"`   // for tests
    Failed    int    `json:"failed"`   // for tests
    Errors    int    `json:"errors"`   // for lint/build
}

type EvidenceBundle struct {
    Hash      string   `json:"hash"`      // SHA-256 of all evidence files
    Files     []string `json:"files"`     // paths to evidence files
    SizeBytes int      `json:"size_bytes"`
    CreatedAt string   `json:"created_at"`
}

func RunVerification(ctx context.Context, sandboxCfg sandbox.SandboxConfig, cfg VerificationConfig) (*VerificationResult, error) {
    result := &VerificationResult{Attempt: 1, RetriesRemaining: cfg.MaxRetries}
    
    // Run each check in sequence (lint depends on build artifacts, tests depend on build)
    checks := []struct{ name, command string }{
        {"secrets", "gitreins guard --check secrets --worktree %s"},
        {"lint",    "gitreins guard --check lint --worktree %s"},
        {"build",   "gitreins guard --check build --worktree %s"},
        {"test",    fmt.Sprintf("gitreins guard --check test --mode %s --worktree %%s", cfg.TestMode)},
    }
    
    allPassed := true
    for _, c := range checks {
        cmd := fmt.Sprintf(c.command, cfg.WorktreePath)
        output, exitCode, err := runInSandbox(ctx, sandboxCfg, cmd)
        checkResult := CheckResult{
            Name:     c.name,
            Status:   "pass",
            Output:   output,
            Duration: "...",
        }
        if exitCode != 0 || err != nil {
            checkResult.Status = "fail"
            allPassed = false
        }
        result.Checks = append(result.Checks, checkResult)
    }
    
    if allPassed {
        result.Status = "pass"
    } else {
        result.Status = "fail"
    }
    
    // Build evidence bundle
    bundle, err := buildEvidenceBundle(cfg, result.Checks)
    if err != nil {
        return result, fmt.Errorf("evidence bundle: %w", err)
    }
    result.EvidenceBundle = bundle
    
    return result, nil
}
```

#### 4.3.3 — Auto-Retry with Fix Loop

When verification fails, the agent receives the failure output and automatically attempts to fix:

```
VERIFICATION FAILED:
  lint: 3 errors
    - src/auth/LoginForm.tsx:45 — 'handleSubmit' is defined but never used
    - src/auth/LoginForm.tsx:78 — Missing return type annotation
    - src/auth/validation.ts:12 — 'emailRegex' is assigned but never used
  
  Attempt 1 of 3. Fix these errors and retry.
```

The agent retries with the failure context. After 3 failed attempts, the task is marked `failed` and a human is notified.

```go
func (f *ForgejoLoop) runVerificationWithRetry(ctx context.Context, outcome *DispatchOutcome) (*ci.VerificationResult, error) {
    maxRetries := 3
    for attempt := 1; attempt <= maxRetries; attempt++ {
        result, err := f.runVerification(ctx, outcome)
        if err != nil {
            return nil, err
        }
        result.Attempt = attempt
        result.RetriesRemaining = maxRetries - attempt
        
        if result.Status == "pass" {
            return result, nil
        }
        
        // Feed failure back to agent for fix attempt
        if attempt < maxRetries {
            // Post failure as PR comment, agent retries
            f.postVerificationFailure(ctx, outcome, result)
        }
    }
    
    // All retries exhausted
    return nil, fmt.Errorf("verification: failed after %d attempts", maxRetries)
}
```

#### 4.3.4 — Evidence Bundle Format

Evidence bundles are stored as signed JSON in the worktree:

```json
{
  "task_id": "task-001",
  "agent_id": "wojons",
  "verification": {
    "status": "pass",
    "checks": [...]
  },
  "signature": "ed25519:abcd1234...",
  "signed_by": "agent:wojons",
  "signed_at": "2026-07-07T14:35:22Z",
  "bundle_hash": "sha256:d4e5f6..."
}
```

Each evidence bundle is written to `~/.helix/evidence/<task-id>/bundle-<attempt>.json` and linked from the audit chain.

### Acceptance Criteria

| # | Criterion | Verification |
|---|-----------|-------------|
| AC-4.3.1 | All 4 Tier 1 checks (secrets, lint, test, build) run before commit | `helix verify --worktree <path>` runs all 4 and reports pass/fail |
| AC-4.3.2 | Agent cannot mark task complete until all Tier 1 checks pass | `task.status` stays `in_progress` until verification passes |
| AC-4.3.3 | Secrets scan uses `.gitleaks.toml` config | Introduce a test secret → secrets check fails |
| AC-4.3.4 | Test mode defaults to `diff` (only changed files) | Changing file A → only file A's tests run |
| AC-4.3.5 | Auto-retry on failure: agent gets up to 3 fix attempts | Introduce lint error → agent retries 3 times → marks failed |
| AC-4.3.6 | Evidence bundle is signed with agent's ED25519 key | `openssl pkeyutl -verify` confirms signature |
| AC-4.3.7 | Verification evidence is linked to audit chain | Audit chain has `verification_complete` event with bundle hash |

---

## Cross-Cutting Concerns

### Data Flow Through the Dispatcher

```
SPEC FILE (.md)
    │
    ▼
┌──────────────────────────────────────────────────────────────────┐
│  DECOMPOSE (3.1)                                                 │
│  DecomposeSpec() → []Task with ACs, section hashes, dependencies │
│  Estimate cost per task                                          │
└──────────────────────────┬───────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────────┐
│  ASSIGN (3.2)                                                    │
│  AssignAgent() → tier-gated, capability-matched, load-balanced   │
│  CostGuard.Check() → APPROVED / BLOCKED / ESCALATED             │
└──────────────────────────┬───────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────────┐
│  CONTEXT PACKAGE (3.3)                                           │
│  AssembleContext() → budget-constrained context for agent       │
│  Includes spec, ACs, ADRs, prior PRs, incidents, code files     │
└──────────────────────────┬───────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────────┐
│  CODE GENERATION (4.1)                                           │
│  ForgejoLoop.Run() → sandboxed worktree → step execution        │
│  ResourceTracker monitors memory, CPU, network, fs writes       │
└──────────────────────────┬───────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────────┐
│  CLARIFICATION (4.2) — if agent blocked                          │
│  ClarificationRequest → auto-resolve or human resolve           │
│  Resolution → agent resumes                                      │
└──────────────────────────┬───────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────────┐
│  SELF-VERIFICATION (4.3)                                         │
│  GitReins Tier 1: secrets, lint, test, build                    │
│  Pass → evidence bundle signed → commit → CreateBranch → CreatePR│
│  Fail → auto-retry up to 3 attempts → escalate to human         │
└──────────────────────────────────────────────────────────────────┘
```

### Trust Tier Impact on Phases 3-4

| Concern | Provisional | Observed | Trusted | Veteran |
|---------|-------------|----------|---------|---------|
| Max context tokens | 12,000 | 24,000 | 48,000 | 96,000 |
| Sandbox isolation | full | workspace | workspace | workspace |
| Sandbox memory | 512 MB | 2,048 MB | 4,096 MB | 8,192 MB |
| Sandbox time | 5 min | 10 min | 30 min | 60 min |
| Cost cap per job | $5 | $25 | $100 | unlimited |
| Auto-approval threshold | 0% | 0% | 80% of cap | any |
| GPU access | no | yes | yes | yes |
| Network in sandbox | no | no | restricted | restricted |

### CLI Command Summary

```bash
# Phase 3: Task Decomposition & Assignment
helix dispatcher status --spec <path> [--agent JSON] [--tier TIER] [--estimate-cost] [--json]
helix dispatcher list-tasks --spec <path> [--json]
helix dispatcher tick --spec <path> --agent JSON [--tier TIER] [--json]
helix dispatcher reassign <task-id> --agent <name> [--force] [--reason "..."]
helix dispatcher index --path <repo-path> --output <index-file>
helix dispatcher search-index --index <file> --query "..." --top N

# Phase 4: Implementation
helix dispatch --spec <path> --agent <name> [--dry-run] [--json]
helix dispatcher clarifications list [--agent <name>]
helix dispatcher clarify <task-id> --answer "..."
helix verify --worktree <path> [--checks secrets,lint,test,build] [--json]

# Trust queries (cross-cutting)
helix trust show --ledger <path> --agent <name> [--json]
helix trust list --ledger <path> [--json]
```

### Files to Create/Modify

| File | Action | Interaction Point |
|------|--------|-------------------|
| `pkg/dispatcher/types.go` | MODIFY: Add `AcceptanceCriteria`, `EstimatedCost`, `RequiredTier`, `DependsOn` to `Task` | 3.1 |
| `pkg/dispatcher/decomposer.go` | MODIFY: Parse AC blocks, compute section hashes, extract dependencies | 3.1 |
| `pkg/dispatcher/assigner.go` | MODIFY: Add tier-gating to `AssignAgent` | 3.2 |
| `pkg/dispatcher/cost_guard.go` | MODIFY: Already complete — needs wiring into `runDispatcherTick` | 3.2 |
| `pkg/dispatcher/context.go` | **CREATE**: `ContextPackage`, `AssembleContext`, `ExpandableResource` | 3.3 |
| `pkg/dispatcher/indexer.go` | **CREATE**: `CodebaseIndex`, `IndexRepo`, `Search` | 3.3 |
| `pkg/dispatcher/clarification.go` | **CREATE**: `ClarificationRequest`, `AutoResolve`, `ClarificationStore` | 4.2 |
| `pkg/dispatcher/sandbox_limits.go` | **CREATE**: `SandboxLimits` tier-gated config | 4.1 |
| `pkg/dispatcher/dependency.go` | **CREATE**: `DependencyGraph`, cycle detection, topological sort | 3.1 |
| `pkg/dispatcher/loop.go` | MODIFY: Wire verification gate, clarification check, sandbox execution | 4.1, 4.2, 4.3 |
| `pkg/dispatcher/forgejo_loop.go` | MODIFY: Wire sandbox, verification, clarification into `Run()` | 4.1, 4.2, 4.3 |
| `pkg/sandbox/executor.go` | MODIFY: Wire real `bwrap` execution (replace `ErrNotImplemented`) | 4.1 |
| `pkg/ci/workflow.go` | MODIFY: Add `RunVerification`, `VerificationConfig`, `CheckResult` | 4.3 |
| `cmd/helix/dispatcher.go` | MODIFY: New subcommands (`reassign`, `index`, `search-index`, `clarify`, `clarifications`) | 3.2, 3.3, 4.2 |
| `cmd/helix/dispatch.go` | MODIFY: Wire `--dry-run` flag, sandbox, verification in `runDispatch` | 4.1, 4.3 |
| `cmd/helix/verify.go` | MODIFY: Already exists — wire GitReins integration and evidence bundles | 4.3 |
| `pkg/trust/tiers.go` | MODIFY: Add `CompareTiers`, `TierOrdinal` | 3.2 |
| `pkg/dispatcher/types.go` | MODIFY: Add `Tier`, `TrustScore`, `CostProfile` to `AgentProfile` | 3.2 |

### Build Order

1. **Foundation:** Wire sandbox execution (`pkg/sandbox/executor.go` → real bwrap).
2. **Decomposition:** Add ACs, hashes, dependencies to `Task` (`decomposer.go`, `dependency.go`).
3. **Assignment:** Add tier-gating to `AssignAgent` (`assigner.go`, `tiers.go`).
4. **Cost guard:** Already implemented — wire into dispatch flow.
5. **Context:** Build `AssembleContext` and `CodebaseIndex` (`context.go`, `indexer.go`).
6. **Code generation:** Wire sandbox into `ForgejoLoop.Run()`.
7. **Clarification:** Build clarification protocol (`clarification.go`).
8. **Verification:** Wire GitReins integration into loop (`verify.go`, `workflow.go`).
9. **Integration:** End-to-end test: spec → decompose → assign → context → sandbox → verify → commit → PR.

---

## Document Status

- [x] Interaction Point 3.1 — Work Breakdown: ACs, hashes, dependencies, cost assignment
- [x] Interaction Point 3.2 — Agent Assignment: tier-gating, cost guard, human override
- [x] Interaction Point 3.3 — Context Packaging: assembler, codebase index, budget constraints
- [x] Interaction Point 4.1 — Code Generation: sandbox execution, tier-gated limits, traceability
- [x] Interaction Point 4.2 — In-Progress Collaboration: clarification protocol, auto-resolution, human notification
- [x] Interaction Point 4.3 — Self-Verification: GitReins integration, evidence bundles, auto-retry
- [x] Cross-cutting: data flow diagram, trust tier impact table, CLI command summary, file inventory, build order
