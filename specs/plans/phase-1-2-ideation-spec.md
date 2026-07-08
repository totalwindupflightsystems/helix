# Helix Resolution Plans — Phases 1-2: Idea Formation + Specification & Design

**Status:** Build-ready  
**Last updated:** 2026-07-07  
**Covers interaction points:** 1.1, 1.2, 1.3, 2.1, 2.2, 2.3, 2.4  
**References:** specs/interaction-map.md, pkg/dispatcher, pkg/trust, pkg/review, pkg/estimate, pkg/marketplace, pkg/api, pkg/security/blast, pkg/negotiate, pkg/adversarial

---

Every interaction point must serve TWO first-class users:
- **Human:** Change management interface (why, risk, blast radius, intent, tradeoffs)
- **Agent:** Verification interface (criteria, evidence, structured assertions)

---

## Phase 1: Idea & Concept Formation

---

### 1.1 — Idea Capture

**Interaction map source:** "Human has idea → captures as text/voice/sketch. Agent can also surface ideas from pattern detection."

| Question | Answer |
|----------|--------|
| **Component** | New `pkg/ideation` package + `helix idea` CLI |
| **Human interface** | `helix idea capture --title "..." --body "..." --tags auth,security --evidence ./screenshot.png` creates an `Idea` entity. `helix idea list` shows all ideas with source attribution (human `@kara`, agent `@assumption-buster`, model `deepseek-v4-pro`). `helix idea show <id>` displays full idea with evidence attachments, linked data (incident refs, code patterns). `helix idea promote <id> --to spec` promotes to Phase 2. |
| **Agent interface** | Agents call `POST /api/v1/ideas` (new endpoint on `pkg/api`) with `Idea` payload. Programmatic Go API in `pkg/ideation`: `ideation.Capture(ctx, idea)` writes to idea store. Agents surface ideas from: marketplace incident DB (`pkg/security/incident_store.go`), codebase pattern detection, marketplace trend analysis (`pkg/marketplace/discovery.go`). `Idea.Source` field distinguishes `human`, `agent`, or `chimera` attribution. |
| **What to build** | 1. `pkg/ideation/types.go` — `Idea` struct: `ID`, `Title`, `Body`, `Tags []string`, `Source IdeaSource` (human/agent/chimera), `SourceAgent string`, `SourceModel string`, `Evidence []EvidenceRef` (file paths, incident IDs, code pattern hashes), `Status IdeaStatus` (draft/validated/prioritized/promoted/closed), `CreatedAt`, `PromotedTo string` (spec ref if promoted). `EvidenceRef` struct: `Type` (incident/code_pattern/market_trend/file), `Ref string`, `Description string`. 2. `pkg/ideation/store.go` — `IdeaStore` interface + file-backed implementation (JSONL at `~/.helix/ideas/ideas.jsonl`). Methods: `Capture`, `Get`, `List`, `Update`, `Promote`. 3. `pkg/ideation/cli.go` — Cobra commands registered on `helix idea`: `capture`, `list`, `show`, `update`, `promote`, `close`. 4. `pkg/api` endpoint `POST /api/v1/ideas` + `GET /api/v1/ideas` + `GET /api/v1/ideas/{id}` for agent access. 5. Wire into `cmd/helix/main.go`. |
| **Existing packages used** | `pkg/marketplace` (agent identity for `SourceAgent`), `pkg/api` (new endpoints), `pkg/security/incident_store` (evidence linking) |
| **Acceptance criteria** | 1. Human captures idea via `helix idea capture` → persisted as `Idea` in JSONL store. 2. Agent creates idea via API → same store, with `Source=agent`. 3. `helix idea list` shows all ideas with Source column. 4. `helix idea show <id>` displays evidence refs as clickable links. 5. `helix idea promote <id> --to spec` creates placeholder spec file and updates `Idea.PromotedTo`. |

**Build path:** `pkg/ideation/types.go` → `pkg/ideation/store.go` → `pkg/ideation/cli.go` → `pkg/api` endpoints → wire CLI

---

### 1.2 — Concept Validation

**Interaction map source:** "Idea is stress-tested against existing architecture, team capacity, and prior incident data. Adversarial concept validation — agents challenge ideas with evidence."

| Question | Answer |
|----------|--------|
| **Component** | `pkg/ideation/validator.go` (new), leveraging `pkg/review` (AdversarialAgentDispatcher pattern), `pkg/trust` (tier-gated agent access), Chimera formation |
| **Human interface** | `helix idea validate <id>` — Outputs structured validation report: `PASS/FAIL/NEEDS_CLARIFICATION` with sections: (a) **Architectural Fit** — conflicts with existing ADRs, pattern mismatch with `pkg/marketplace` agent patterns, (b) **Feasibility** — resource estimate from `pkg/estimate` calculator, team capacity check, (c) **Risk Surface** — incident correlation via `pkg/security/incident_store`, similar-failure-pattern lookup, (d) **Agent Challenges** — list of specific challenges from adversarial agents with evidence citations. Change management view: `--risk` flag shows risk score 0-100, `--tradeoffs` shows buried assumptions. |
| **Agent interface** | `IdeaValidator` struct in `pkg/ideation`: `Validate(ctx, idea) (*ValidationReport, error)`. Internally: 1. Gathers context package (architecture graph, incident history, team capacity from `pkg/dispatcher` agent registry, marketplace agent profiles). 2. Dispatches to adversarial agents using `pkg/review.AdversarialAgentDispatcher` pattern — registers concept-validation agents: `@feasibility-checker` (resource modeling), `@architecture-fit` (ADR constraint checking), `@incident-correlator` (failure pattern lookup), `@assumption-buster` (already exists in `pkg/review`). 3. Chimera formation provides multi-model consensus on validation verdict. Each agent returns `ValidationFinding` with severity, evidence, and recommendation. |
| **What to build** | 1. `pkg/ideation/validator.go` — `IdeaValidator` struct, `Validate` method. `ValidationReport` struct: `Verdict` (pass/fail/needs_clarification), `RiskScore float64`, `Findings []ValidationFinding`, `ArchitecturalConflicts []ADRConflict`, `IncidentCorrelations []IncidentMatch`, `ResourceEstimate estimate.CostEstimate`. `ValidationFinding` struct: `AgentType`, `Severity`, `Description`, `Evidence []EvidenceRef`, `Recommendation`. 2. Concept-validation agent types registered via `pkg/review` pattern — stub implementations that call LLM with idea+context. 3. CLI `helix idea validate <id> [--risk] [--tradeoffs] [--json]`. 4. Wire `pkg/estimate/estimator.go` for cost projection on idea. |
| **Existing packages used** | `pkg/review` (AdversarialAgentDispatcher, AgentType, AgentRequest, AgentResult, Finding — reuse structural patterns), `pkg/trust` (TrustTier gating for which agents can validate), `pkg/estimate` (TaskDesc→CostEstimate for feasibility check), `pkg/marketplace` (agent capability matching for validation dispatch), `pkg/security/incident_store` (failure pattern lookup), `pkg/adversarial` (scenario definitions) |
| **Acceptance criteria** | 1. `helix idea validate <id>` returns structured report with risk score, architectural conflicts, incident correlations. 2. At least 2 adversarial agents run per validation (assumption-buster + one context-specific). 3. Validation report cites specific evidence (incident IDs, ADR references, code pattern hashes). 4. `--risk` flag surfaces change management view: why this risk level, what could go wrong. 5. Ideas validated as `fail` block promotion to spec until re-validated. |

**Build path:** `pkg/ideation/validator.go` (types) → concept agents (stubs using review dispatch) → CLI `helix idea validate` → integration with estimate/incident stores

---

### 1.3 — Prioritization & Roadmapping

**Interaction map source:** "Ideas compete for slots. Agents estimate cost and risk. Cost estimator runs against every candidate idea. Agents can advocate for ideas with risk/reward evidence. Human decides; agents inform."

| Question | Answer |
|----------|--------|
| **Component** | `pkg/ideation/priority.go` (new), `pkg/estimate` (cost projection), `pkg/marketplace` (agent advocacy) |
| **Human interface** | `helix idea prioritize` — Displays ranked list of validated ideas sorted by composite score: (cost_estimate/risk_score × strategic_weight). Columns: ID, Title, Cost ($), Risk (0-100), Advocates (agent names), Score. Human can: `--reorder 3,1,2` to manually reorder, `--promote <id>` to move to spec. `helix roadmap` — Shows promoted ideas in sequence with dependency graph. Change management view: `--detail <id>` shows breakdown of why this idea ranks where it does, what agents advocated, what evidence they cited. |
| **Agent interface** | `IdeaPrioritizer` struct in `pkg/ideation`: `Prioritize(ctx, ideas) (*Roadmap, error)`. Steps: 1. Runs `pkg/estimate` cost estimator against every idea using `TaskSpec` task type (spec-writing cost projection). 2. Queries risk scores from validation reports. 3. Agents can submit advocacy via `SubmitAdvocacy(ctx, ideaID, position)` — adds `AdvocacyRecord` with agent identity, position (for/against/priority), evidence. 4. Composite scoring: `score = (1/cost_normalized × 0.4) + ((1-risk_normalized) × 0.3) + (advocacy_count × 0.15) + (advocate_trust_avg × 0.15)`. 5. `Roadmap` struct: ordered `[]PrioritizedIdea` with `Rank`, `Score`, `Dependencies []string` (other idea IDs). |
| **What to build** | 1. `pkg/ideation/priority.go` — `PrioritizedIdea` struct extending `Idea` with `Rank int`, `Score float64`, `CostEstimate estimate.CostEstimate`, `RiskScore float64`, `AdvocacyRecords []AdvocacyRecord`, `Dependencies []string`. `AdvocacyRecord`: `AgentID`, `Position` (for/against/priority), `Evidence []EvidenceRef`, `SubmittedAt`. `Roadmap` struct: `Ideas []PrioritizedIdea`, `GeneratedAt`, `Version int`. 2. `IdeaPrioritizer` with `Prioritize` method. 3. `helix idea prioritize [--reorder] [--promote]` CLI. `helix roadmap [show|export]`. 4. Agent advocacy API endpoint `POST /api/v1/ideas/{id}/advocate`. 5. Roadmap persistence (`~/.helix/roadmap/roadmap.json`). |
| **Existing packages used** | `pkg/estimate` (cost estimator — `estimator.Estimate(taskDesc)` with `TaskSpec` type), `pkg/trust` (advocate agent trust score weights advocacy), `pkg/marketplace` (agent identity for advocates) |
| **Acceptance criteria** | 1. `helix idea prioritize` displays cost-ranked idea list where each idea has a cost estimate computed by `pkg/estimate`. 2. Human can reorder and the reordering is persisted. 3. Agents can submit advocacy via API — advocacy appears in `--detail` view. 4. `helix roadmap` shows promoted ideas in dependency order. 5. Composite score formula is deterministic and replay-verifiable. |

**Build path:** `pkg/ideation/priority.go` (types + Prioritizer) → estimate integration → advocacy API → CLI → roadmap persistence

---

## Phase 2: Specification & Design

---

### 2.1 — Spec Authoring

**Interaction map source:** "Spec co-authoring with adversarial annotation. Agent proposes spec sections; another agent challenges them. Human signs off on intent; agents verify completeness."

| Question | Answer |
|----------|--------|
| **Component** | New `pkg/spec` package + `helix spec` CLI, integrated with `pkg/review` (adversarial annotation) |
| **Human interface** | `helix spec create <idea-id>` — Creates spec file from promoted idea. Opens template with sections: Overview, Requirements, Non-Goals, Constraints, Acceptance Criteria. `helix spec edit <spec-id>` — Opens spec in editor; on save, triggers agent co-authoring pass. Agent proposes additional sections (edge cases, failure modes, consistency checks). Changes appear as annotations: `[AGENT: @completeness-checker] Missing edge case: empty input → ...` in the spec. Human reviews annotations with `helix spec review <spec-id>` — shows diff of agent-suggested changes with accept/reject per change. `helix spec approve <spec-id> --section requirements` — Human signs off on intent sections. Change management: `helix spec gap-analysis <spec-id>` shows what's missing vs similar specs in the codebase. |
| **Agent interface** | `SpecCoAuthor` in `pkg/spec`: `CoAuthor(ctx, spec) (*AnnotatedSpec, error)`. Workflow: 1. Human writes base sections (intent, constraints). 2. Agent A (`@spec-generator`) proposes additional sections, edge cases, failure modes. 3. Agent B (`@spec-challenger`) reviews proposals, marks inconsistencies, missing coverage. 4. `AnnotatedSpec` returned: `Spec` with `Annotations []SpecAnnotation` — each annotation has `Line int`, `Agent string`, `Type` (edge_case/failure_mode/consistency/incompleteness), `Content string`, `Severity`, `Status` (proposed/accepted/rejected). `SpecCompleteness` checker: `CheckCompleteness(spec) (*CompletenessReport, error)` — scores coverage across dimensions (requirements, error handling, security, performance, observability) using checklist validated against `pkg/adversarial` scenario patterns. |
| **What to build** | 1. `pkg/spec/types.go` — `Spec` struct: `ID`, `IdeaRef`, `Sections []SpecSection`, `Status SpecStatus` (draft/in_review/approved/frozen), `Annotations []SpecAnnotation`, `ADRRefs []string`, `ContractRefs []string`. `SpecSection`: `Title`, `Content`, `ApprovalStatus` (pending/approved/rejected), `ApprovedBy string`. `SpecAnnotation`: `Line int`, `AgentType string`, `AnnotationType`, `Content`, `Severity`, `Status`. 2. `pkg/spec/coauthor.go` — `SpecCoAuthor` using `pkg/review.AdversarialAgentDispatcher` pattern. Two specialized agents: `@spec-generator` (proposes content), `@spec-challenger` (challenges proposals). 3. `pkg/spec/completeness.go` — `SpecCompleteness` checker: 12-dimension scoring (requirements coverage, error states, security, auth, rate limiting, data validation, observability, testing, deployment, rollback, monitoring, documentation). 4. `helix spec create/edit/review/approve/gap-analysis` CLI. 5. Spec store: `~/.helix/specs/<spec-id>.md` (markdown with YAML frontmatter for machine-readable metadata). |
| **Existing packages used** | `pkg/review` (AdversarialAgentDispatcher for co-authoring, AgentType extension), `pkg/trust` (approval gating by human trust tier), `pkg/adversarial` (scenario patterns feed completeness checklist), `pkg/marketplace` (agent capability matching for spec-generation agents) |
| **Acceptance criteria** | 1. Human creates spec from idea → template populated with idea data. 2. Agent co-authoring adds annotations with line-level precision. 3. Human can accept/reject annotations individually. 4. Completeness report shows 12-dimension score, links to specific gaps with line references. 5. `helix spec approve --section` persists human signoff with timestamp. 6. Frozen spec is immutable (hash-locked) for downstream traceability. |

**Build path:** `pkg/spec/types.go` → `pkg/spec/coauthor.go` → `pkg/spec/completeness.go` → `helix spec` CLI → adversarial scenario integration

---

### 2.2 — Architecture Decision Records (ADRs)

**Interaction map source:** "Agent-authored ADRs with multi-model review. Agents can propose architectures using pattern detection from marketplace agents. All ADRs are evidence-linked to specs and incidents."

| Question | Answer |
|----------|--------|
| **Component** | New `pkg/adr` package + `helix adr` CLI, integrated with `pkg/review` (multi-model review), `pkg/marketplace` (pattern detection) |
| **Human interface** | `helix adr create` — Interactive or file-based ADR creation following MADR format: Title, Context, Decision, Alternatives Considered, Consequences. `helix adr list` — Shows all ADRs with status (proposed/accepted/deprecated/superseded). `helix adr review <id>` — Triggers multi-model review: each model reviews the ADR independently, results merged via consensus. Shows: model agreement %, conflicting assessments, suggested alternatives with rationale. Change management view: `--risk` shows architectural risk score, `--impact` shows blast radius from `pkg/security/blast`, `--tradeoffs` shows what alternatives were rejected and why. |
| **Agent interface** | `ADRCoAuthor` in `pkg/adr`: `CoAuthor(ctx, spec, architectureContext) (*ADR, error)`. Steps: 1. Agent queries `pkg/marketplace` for agent patterns related to spec domain (e.g., "auth pattern", "database migration pattern"). 2. Agent proposes ADR with: Decision, Alternatives (with tradeoff analysis from marketplace agent performance data), Predicted impact (blast radius, cost implications from `pkg/estimate`). 3. Multi-model review via `pkg/review` orchestrator — `ADRReviewRequest` extends `ReviewRequest` with ADR-specific fields. 4. `ADRReviewResult`: model verdicts, consensus score, conflicting assessments. Evidence-linked: every ADR decision cites spec sections, incident patterns, or agent performance data. |
| **What to build** | 1. `pkg/adr/types.go` — `ADR` struct: `ID`, `Title`, `Status` (proposed/accepted/deprecated/superseded), `Context`, `Decision`, `Alternatives []Alternative` (each with `Description`, `Tradeoffs`, `RejectedBecause`), `Consequences`, `EvidenceLinks []EvidenceLink` (spec ref, incident ref, marketplace pattern), `ReviewScore float64`, `Authors []string` (human+agent). `ADRReviewRequest`: `ADR ADR`, `Models []string`, `ConsensusThreshold float64`. 2. `pkg/adr/coauthor.go` — `ADRCoAuthor`. 3. `pkg/adr/review.go` — `ADRReviewer` using multi-model dispatch from `pkg/review`. 4. `helix adr create/list/show/review/supersede` CLI. 5. ADR store: `~/.helix/adrs/<NNNN>-<slug>.md` with YAML frontmatter. |
| **Existing packages used** | `pkg/review` (ReviewOrchestrator, ModelClient, ReviewRequest/Result, consensus engine, ChangeCategory), `pkg/marketplace` (agent pattern discovery via capabilities), `pkg/security/blast` (blast radius containment layers for architectural impact), `pkg/estimate` (cost implications of architectural choices) |
| **Acceptance criteria** | 1. ADR created with decision, alternatives, consequences. 2. Multi-model review returns consensus score with per-model verdicts. 3. ADR evidence-linked: at least one spec ref, incident ref, or marketplace pattern citation. 4. `--risk` shows blast radius mapped to `pkg/security/blast` containment layers. 5. `--tradeoffs` shows rejected alternatives with rationale. 6. Superseding creates new ADR that links back to superseded ADR. |

**Build path:** `pkg/adr/types.go` → `pkg/adr/coauthor.go` → `pkg/adr/review.go` → `helix adr` CLI → blast radius integration

---

### 2.3 — Design Review

**Interaction map source:** "Automated design review via adversarial agents. @assumption-buster challenges every implicit assumption. @redteam maps threat surface. @cost-auditor estimates token/resource budget. Human receives a structured change management view."

| Question | Answer |
|----------|--------|
| **Component** | `pkg/review` (existing `AdversarialAgentDispatcher` — extended for design review mode), new `pkg/design` for design representation, `helix design review` CLI |
| **Human interface** | `helix design review <spec-id>` — Dispatches adversarial agents against the design (spec + ADRs). Output: **Change Management View** divided into sections: (a) **Assumption Risk** — every implicit assumption challenged by `@assumption-buster`, ranked by risk level, (b) **Threat Surface** — `@redteam` maps attack vectors with file/service references, (c) **Cost Budget** — `@cost-auditor` projects token cost for implementation, flags budget thresholds, (d) **Completeness Gaps** — what's missing from the design, (e) **Consensus Verdict** — PASS/WARN/FAIL from multi-model review. Interactive: `--fix <finding-id>` opens editor to address specific finding. Change management summary: `--summary` for exec-level: risk level, cost, key blockers. |
| **Agent interface** | `DesignReviewDispatcher` extends `pkg/review.AdversarialAgentDispatcher` with design-specific triggers. Agent roster (existing in `pkg/review`): `@assumption-buster` — enumerates assumptions from spec text, maps to risk level, challenges each. `@redteam` — maps threat surface: auth paths, data flows, trust boundaries, privilege escalation paths. `@cost-auditor` — runs `pkg/estimate` against spec tasks, projects total implementation budget. `@chaos-engineer` — injects failure scenarios into design (service down, network partition, rate limit hit), checks recovery paths. New agent: `@consistency-checker` — cross-references spec↔ADR↔contract for contradictions. Each agent returns `DesignFinding` (extends `pkg/review.Finding` with design-specific fields). `DesignReviewReport` aggregates with consensus engine from `pkg/review`. |
| **What to build** | 1. `pkg/design/types.go` — `DesignReviewRequest`: `SpecRef string`, `ADRRefs []string`, `ContractSchema json.RawMessage`, `Context DesignContext` (team capacity, budget remaining, timeline). `DesignFinding`: extends `review.Finding` with `DesignAspect` (assumption/threat/cost/completeness/consistency), `AffectedComponent string`, `DesignLine int`. `DesignReviewReport`: `Findings []DesignFinding`, `RiskScore float64`, `ThreatSurface ThreatMap`, `CostProjection estimate.CostEstimate`, `Consensus review.ConsensusResult`. 2. `pkg/design/review.go` — `DesignReviewDispatcher` wrapping `pkg/review.AdversarialAgentDispatcher`. Registers design-review agent variants. Uses existing trigger system (`AgentTrigger`). 3. `helix design review <spec-id> [--summary] [--fix <id>]` CLI. 4. Threat map visualization (ASCII/terminal): shows services, data flows, trust boundaries, attack vectors. |
| **Existing packages used** | `pkg/review` (AdversarialAgentDispatcher, all existing agents, AgentTrigger, AgentResult, Finding, ConsensusReport, EvidenceVerifier), `pkg/estimate` (cost projection via estimator), `pkg/security/blast` (blast radius containment layers mapped to design components), `pkg/trust` (tier-gating for agent review depth) |
| **Acceptance criteria** | 1. `helix design review` dispatches all applicable agents (assumption-buster always, redteam if auth/crypto, cost-auditor always, chaos-engineer if resilience). 2. Assumption-buster enumerates ≥5 assumptions for any non-trivial design. 3. Threat map shows attack vectors with severity. 4. Cost projection within ±30% of actual implementation cost (validated in Phase 8 reconciliation). 5. Consensus verdict computed from multi-model agreement. 6. `--fix <finding-id>` allows human to address and re-run specific agent. |

**Build path:** `pkg/design/types.go` → `pkg/design/review.go` (wraps existing review) → `helix design review` CLI → threat map → cost projector integration

---

### 2.4 — API Contract Definition

**Interaction map source:** "Multi-model contract validation. One agent generates; another verifies consistency, checks for breaking changes, validates against existing consumers. Contract is signed and immutable before implementation begins."

| Question | Answer |
|----------|--------|
| **Component** | New `pkg/contract` package + `helix contract` CLI, extends `pkg/api` (existing ContractServer for schema serving) |
| **Human interface** | `helix contract create <spec-id> --format openapi` — Generates contract from spec. Opens editor with generated schema. `helix contract validate <contract-id>` — Multi-model validation: (a) **Consistency** — matches spec requirements 1:1, (b) **Completeness** — all spec endpoints covered, all error states documented, (c) **Breaking Changes** — diff against previous version, consumer impact analysis, (d) **Security** — auth scheme validation, input validation coverage, rate limiting defined. Change management: `--breaking` shows exactly what breaks for which consumers, `--diff` shows visual diff between versions. `helix contract freeze <contract-id>` — Signs contract (SHA-256 hash), makes immutable. `helix contract consumer-check <contract-id> --consumer <service-name>` — Validates contract against specific consumer's expectations. |
| **Agent interface** | `ContractAuthor` in `pkg/contract`: `Generate(ctx, spec) (*Contract, error)`. Generates OpenAPI/protobuf/GraphQL from spec. `ContractValidator`: `Validate(ctx, contract, previousVersion, consumers) (*ValidationReport, error)`. Multi-model validation: Agent A (`@contract-generator`) generates contract. Agent B (`@contract-verifier`) checks: schema consistency, endpoint completeness (all spec requirements covered), error response coverage (4xx, 5xx for every endpoint). Agent C (`@breaking-change-detector`) diffs against previous contract version, enumerates consumer impacts. `Contract` struct: `ID`, `SpecRef`, `Format` (openapi/protobuf/graphql), `Schema json.RawMessage`, `Hash string` (SHA-256 of canonicalized schema), `FrozenAt time.Time`, `Version int`. `ValidationReport`: `Consistent bool`, `BreakingChanges []BreakingChange`, `MissingEndpoints []string`, `ConsumerImpacts []ConsumerImpact`. |
| **What to build** | 1. `pkg/contract/types.go` — `Contract`, `ValidationReport`, `BreakingChange`, `ConsumerImpact`, `ContractFormat`. 2. `pkg/contract/generate.go` — `ContractAuthor` with LLM-based generation. 3. `pkg/contract/validate.go` — `ContractValidator` with multi-model validation. Reuses `pkg/review` multi-model dispatch pattern. 4. `pkg/contract/breaking.go` — `BreakingChangeDetector`: structural diff between schema versions, consumer catalog lookup. 5. `pkg/contract/store.go` — `ContractStore` (`~/.helix/contracts/<contract-id>.json`), consumer catalog (`~/.helix/contracts/consumers.yaml`). 6. `helix contract create/validate/freeze/diff/consumer-check` CLI. 7. Extend `pkg/api/server.go` to serve frozen contracts as raw schemas. |
| **Existing packages used** | `pkg/api` (`ContractServer`, existing endpoint patterns, service definitions), `pkg/review` (multi-model dispatch pattern for validation agents), `pkg/trust` (tier-gating for contract freeze authorization) |
| **Acceptance criteria** | 1. `helix contract create` generates valid OpenAPI/protobuf/GraphQL from spec. 2. Multi-model validation: at least 2 models check consistency independently, results merged. 3. Breaking change detection correctly identifies field removal, type change, required→optional, endpoint removal against previous version. 4. Consumer impact report lists every consumer affected by each breaking change. 5. `helix contract freeze` hashes contract; subsequent changes to schema are rejected unless version is incremented. 6. Contract is linked to spec and ADR for full traceability (spec→ADR→contract→implementation). |

**Build path:** `pkg/contract/types.go` → `pkg/contract/generate.go` → `pkg/contract/validate.go` → `pkg/contract/breaking.go` → `pkg/contract/store.go` → `helix contract` CLI → extend `pkg/api`

---

## Build Order (Phase 1-2)

```
Phase 1 (Idea Formation) — parallel tracks:
  Track A: pkg/ideation/types.go → store.go → CLI
  Track B: pkg/ideation/validator.go → review integration → validate CLI
  Track C: pkg/ideation/priority.go → estimate integration → prioritize/roadmap CLI

Phase 2 (Spec & Design) — sequential within, parallel across:
  Track D: pkg/spec/types.go → coauthor.go → completeness.go → CLI
  Track E: pkg/adr/types.go → coauthor.go → review.go → CLI
  Track F: pkg/design/types.go → review.go → CLI
  Track G: pkg/contract/types.go → generate.go → validate.go → breaking.go → store.go → CLI
```

## Package Dependency Map

```
pkg/ideation ──────► pkg/review, pkg/estimate, pkg/marketplace, pkg/security/incident_store
pkg/spec ──────────► pkg/review, pkg/trust, pkg/adversarial, pkg/marketplace
pkg/adr ───────────► pkg/review, pkg/marketplace, pkg/security/blast, pkg/estimate
pkg/design ────────► pkg/review, pkg/estimate, pkg/security/blast, pkg/trust
pkg/contract ──────► pkg/api, pkg/review, pkg/trust
```

## Existing Code Reuse Summary

| Existing Package | Reused For |
|-----------------|------------|
| `pkg/review` | Adversarial agent dispatch (1.2, 2.1, 2.3), multi-model review (2.2, 2.4), consensus engine (2.2, 2.3), bias stripper (2.1+), evidence verification pattern (all) |
| `pkg/estimate` | Cost projection for ideas (1.3), design budget (2.3), ADR cost implications (2.2), task estimation (all) |
| `pkg/trust` | Tier-gated validation (1.2), agent advocacy weighting (1.3), approval gating (2.1), freeze authorization (2.4), review depth (2.3) |
| `pkg/marketplace` | Agent identity/attribution (1.1), capability matching (1.2, 2.1), pattern discovery (2.2), agent registry (all) |
| `pkg/dispatcher` | Task type definitions (3.1 prep), agent capacity for feasibility (1.2) |
| `pkg/api` | Contract serving (2.4), idea CRUD endpoints (1.1), advocacy submission (1.3) |
| `pkg/security/blast` | Blast radius for architectural impact (2.2), threat surface containment (2.3) |
| `pkg/security/incident_store` | Failure pattern correlation (1.2), evidence linking (1.1, 2.2) |
| `pkg/adversarial` | Scenario patterns feed completeness checklist (2.1) |
| `pkg/negotiate` | Debate audit logging pattern for disagreement tracking (1.3 advocacy, 2.2 ADR review) |
