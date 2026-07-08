# Helix Interaction Map — Human/Agent Symmetry in the SDLC

Every interaction point in the software lifecycle must serve TWO first-class users: the human (who needs change management — understanding risk, blast radius, intent, tradeoffs) and the AI agent (who needs verification — verifiable criteria, evidence bundles, structured assertions). Helix provides both interfaces from the same event.

---

## Phase 1: Idea & Concept Formation

### 1.1 Idea Capture
- **Type:** COLLABORATION
- **Participants:** human ↔ human, human ↔ agent
- **Flow:** Human has idea → captures as text/voice/sketch. Agent can also surface ideas from pattern detection in codebase/incidents/marketplace.
- **Today:** IDE scratch files, Notion, Slack threads. Human-only.
- **Helix must provide:** A shared ideation surface where both human and agent ideas live as first-class `Idea` entities with attribution, links to evidence (code patterns, incident data, market trends), and a promotion path to Specification.

### 1.2 Concept Validation
- **Type:** COLLABORATION
- **Participants:** human ↔ agent, agent ↔ agent
- **Flow:** Idea is stress-tested against existing architecture, team capacity, and prior incident data.
- **Today:** Human discusses with team, makes gut-call.
- **Helix must provide:** Adversarial concept validation — agents challenge ideas with evidence from incident database, architectural constraints, and resource modeling. Chimera formation evaluates feasibility from multiple angles.

### 1.3 Prioritization & Roadmapping
- **Type:** NEGOTIATION
- **Participants:** human ↔ human, human ↔ agent
- **Flow:** Ideas compete for slots. Agents estimate cost and risk.
- **Today:** Product manager makes call, sometimes with data.
- **Helix must provide:** Cost estimator runs against every candidate idea. Agents can advocate for ideas with risk/reward evidence. Human decides; agents inform.

---

## Phase 2: Specification & Design

### 2.1 Spec Authoring
- **Type:** COLLABORATION
- **Participants:** human ↔ agent, agent ↔ agent
- **Flow:** Requirements become structured specification. Human provides intent and constraints; agent fills in exhaustive edge cases, failure modes, and consistency checks.
- **Today:** Human writes spec doc, reviews with team. Or AI writes spec with no adversary.
- **Helix must provide:** Spec co-authoring with adversarial annotation. Agent proposes spec sections; another agent challenges them. Human signs off on intent; agents verify completeness.

### 2.2 Architecture Decision Records (ADRs)
- **Type:** COLLABORATION → GATE
- **Participants:** human ↔ agent, agent ↔ agent
- **Flow:** Architecture choices documented with tradeoffs, alternatives considered, and predicted impact.
- **Today:** Human writes ADR, team reviews. No agent participation.
- **Helix must provide:** Agent-authored ADRs with multi-model review. Agents can propose architectures using pattern detection from marketplace agents. All ADRs are evidence-linked to specs and incidents.

### 2.3 Design Review
- **Type:** GATE
- **Participants:** human ↔ agent, agent ↔ agent
- **Flow:** Design is reviewed for consistency, security, performance, and alignment with platform architecture.
- **Today:** Human design review meeting. Ad-hoc, undocumented.
- **Helix must provide:** Automated design review via adversarial agents. @assumption-buster challenges every implicit assumption. @redteam maps threat surface. @cost-auditor estimates token/resource budget. Human receives a structured change management view: what's risky, what's missing, what tradeoffs were buried.

### 2.4 API Contract Definition
- **Type:** COLLABORATION → GATE
- **Participants:** agent ↔ agent, human ↔ agent
- **Flow:** API contracts (OpenAPI, protobuf, GraphQL schema) are authored and validated.
- **Today:** Human writes, reviews manually. Or AI generates with no adversary.
- **Helix must provide:** Multi-model contract validation. One agent generates; another verifies consistency, checks for breaking changes, validates against existing consumers. Contract is signed and immutable before implementation begins.

---

## Phase 3: Task Decomposition & Assignment

### 3.1 Work Breakdown
- **Type:** HANDOFF
- **Participants:** agent ↔ agent, human ↔ agent
- **Flow:** Spec → atomic, verifiable tasks with dependencies and acceptance criteria.
- **Today:** Human breaks down work, estimates effort. Subjective, inconsistent.
- **Helix must provide:** Agent-driven decomposition with human approval. Each task has verifiable ACs (binary pass/fail), estimated cost, and required trust tier. Tasks are linked to spec sections for traceability.

### 3.2 Agent Assignment
- **Type:** HANDOFF
- **Participants:** agent ↔ system, human ↔ system
- **Flow:** Tasks are assigned to agents based on trust tier, domain expertise, current load, and cost profile.
- **Today:** Human assigns to human. Or "assign to AI" with no trust/cost consideration.
- **Helix must provide:** Dispatcher that matches task requirements (tier, capability, budget) to agent profiles (trust score, domain history, cost efficiency). Human can override; agents cannot self-assign outside their tier.

### 3.3 Context Packaging
- **Type:** HANDOFF
- **Participants:** system ↔ agent
- **Flow:** When a task is assigned, the agent receives a context package: relevant specs, prior related PRs, architectural constraints, and code context.
- **Today:** Developer reads docs, asks questions, searches codebase. Context is human-discovered.
- **Helix must provide:** Automated context assembly from spec links, codebase indexing, incident history, and marketplace knowledge. Context is budget-constrained (fits in model window). Agent can request more; each expansion costs tokens.

---

## Phase 4: Implementation

### 4.1 Code Generation
- **Type:** HANDOFF
- **Participants:** agent ↔ system
- **Flow:** Agent writes code against spec, ACs, and context package in an isolated worktree.
- **Today:** Human writes code. Or AI writes in shared editor with no sandbox.
- **Helix must provide:** Sandboxed worktrees per agent per task. Bubblewrap isolation. No cross-task contamination. Agent's entire session is traceable.

### 4.2 In-Progress Collaboration
- **Type:** COLLABORATION
- **Participants:** agent ↔ agent, human ↔ agent
- **Flow:** Agent encounters ambiguity → requests clarification. Human or another agent responds.
- **Today:** Developer asks teammate or searches internally. Ad-hoc, lossy.
- **Helix must provide:** Structured ambiguity resolution. Agent files a `CLARIFICATION_NEEDED` with specific question, context, and blocked progress. Human or trusted agent responds. Resolution is linked to task and spec for audit.

### 4.3 Self-Verification
- **Type:** GATE
- **Participants:** agent ↔ system
- **Flow:** Agent runs tests, lint, build before considering work complete.
- **Today:** Developer runs tests locally. AI may or may not.
- **Helix must provide:** Pre-commit verification enforced at agent level. Agent cannot mark task complete until GitReins Tier 1 passes. Evidence is captured.

---

## Phase 5: Pre-Commit Verification

### 5.1 Tier 1 Guards (Static)
- **Type:** GATE
- **Participants:** agent ↔ system
- **Flow:** Secrets scan, lint, tests (diff mode), build — must pass before any commit.
- **Today:** Git hooks exist but are human-configured and bypassable.
- **Helix must provide:** GitReins pre-commit hook. No --no-verify for agents. Secrets scan uses .gitleaks.toml. Test mode is diff by default. Failure blocks commit. Evidence of each check is recorded.

### 5.2 Commit Attestation
- **Type:** GATE
- **Participants:** agent ↔ system
- **Flow:** Every commit links to a prompt version and includes agent co-author attribution.
- **Today:** Commit messages are unstructured. No prompt provenance.
- **Helix must provide:** Commit-msg hook enforcing `Co-authored-by:` and `Prompt:` trailers. Prompt hash must match registered prompt. Commit is signed with agent's ED25519 key.

### 5.3 Pre-Push Verification
- **Type:** GATE
- **Participants:** agent ↔ system
- **Flow:** Before pushing to remote, all checks re-run against the full branch.
- **Today:** CI catches failures post-push. Wasteful.
- **Helix must provide:** Pre-push hook that runs full test suite, lint, and build. Failure blocks push. Agent cannot bypass.

---

## Phase 6: Code Review

### 6.1 Human Review Interface (Change Management)
- **Type:** GATE → COLLABORATION
- **Participants:** human ↔ system, human ↔ agent
- **Flow:** Human sees: why this change, blast radius map, architectural impact, risk assessment, tradeoffs made, what edge cases were considered.
- **Today:** Human reads diff, asks questions in PR comments. Subjective, inconsistent.
- **Helix must provide:** Change management dashboard. Auto-generated blast radius from codebase dependency graph. Risk score from incident database correlation. Architectural fit analysis from ADR lineage. Human approves the CHANGE, not the code.

### 6.2 Agent Review Interface (Verification)
- **Type:** GATE
- **Participants:** agent ↔ system, agent ↔ agent
- **Flow:** Agents see: structured acceptance criteria (binary pass/fail), test evidence, bias-stripped commit message, multi-model adversary findings.
- **Today:** Single LLM review with no adversary. Or no agent review at all.
- **Helix must provide:** Adversarial multi-model review. Bias-stripper removes evaluative language. Three models from different providers review independently. Evidence bundles signed with ED25519. Consensus engine merges verdicts.

### 6.3 Review Load Balancing
- **Type:** HANDOFF
- **Participants:** system ↔ human, system ↔ agent
- **Flow:** Incoming PRs are routed to available reviewers (human and agent) based on expertise, trust tier, and current load.
- **Today:** Human tags reviewers manually. Bottleneck formation.
- **Helix must provide:** Review queue with automatic routing. Agents can review PRs within their trust tier. Humans see only PRs that need human judgment (architectural decisions, novel patterns, rejected agent reviews).

---

## Phase 7: PR Negotiation & Conflict Resolution

### 7.1 Agent-Agent Disagreement
- **Type:** NEGOTIATION
- **Participants:** agent ↔ agent
- **Flow:** Two agents disagree on a review finding. Structured debate ensues.
- **Today:** Does not exist. Agents don't debate.
- **Helix must provide:** Structured debate protocol. Each agent states position with evidence. Positions scored for evidence quality. If deadlock, escalate to Chimera as tiebreaker.

### 7.2 Human-Agent Disagreement
- **Type:** NEGOTIATION
- **Participants:** human ↔ agent
- **Flow:** Human dismisses agent's review finding. Agent must accept but records the dismissal.
- **Today:** Human overrides but no learning occurs.
- **Helix must provide:** Structured dismissal with reason. Dismissal feeds false positive tracker. Agent trust is adjusted if pattern of bad reviews emerges. Human trust is noted (frequent overrides reduce weight of their agent's reviews).

### 7.3 Chimera Tiebreak
- **Type:** NEGOTIATION → GATE
- **Participants:** agent ↔ system
- **Flow:** Deadlocked agent debate → Chimera formation adjudicates with multi-model consensus.
- **Today:** Human makes judgment call.
- **Helix must provide:** Chimera as arbitration layer. Formation selects models with no prior stake in the PR. Consensus must reach threshold (configurable per risk level). Verdict is final and signed.

---

## Phase 8: Merge Gating & Quality Enforcement

### 8.1 Merge Gate Checks
- **Type:** GATE
- **Participants:** system ↔ agent, system ↔ human
- **Flow:** All gates must be green before merge is possible: Tier 1 (secrets, lint, test, build), Tier 2 (adversarial review), evidence bundle valid, behavior contract present, trust tier sufficient.
- **Today:** GitHub branch protection rules. Limited, bypassable.
- **Helix must provide:** GitReins merge gate that cannot be bypassed. All evidence artifacts must be present and signed. Trust tier enforcement per file category. Merge is atomic and traceable.

### 8.2 Cost Gate
- **Type:** GATE
- **Participants:** system ↔ agent
- **Flow:** Before merge, verify actual cost vs estimated cost. Flag variance >20%.
- **Today:** No cost tracking.
- **Helix must provide:** Reconciliation engine comparing estimated vs actual token spend. Variance > threshold triggers review. Agent cost accuracy feeds trust score.

### 8.3 Prompt Provenance Gate
- **Type:** GATE
- **Participants:** system ↔ agent
- **Flow:** Verify that the prompt version used matches the registered prompt. Detect prompt drift.
- **Today:** Prompts change silently. No versioning.
- **Helix must provide:** Prompt registry with hash verification. Every commit links to a prompt version. PromptFoo regression tests in CI. Prompt changes require new version and re-attestation.

---

## Phase 9: Deployment & Release

### 9.1 Shadow Verification
- **Type:** GATE
- **Participants:** system ↔ agent, system ↔ system
- **Flow:** Deploy to shadow environment. Mirror production traffic. Compare behavior.
- **Today:** Staging environment. Limited traffic mirroring.
- **Helix must provide:** Dark launch against production traffic. Behavior differential analysis (success rate, latency percentiles, error types). Auto-rollback on anomaly. Trust tier gates shadow duration.

### 9.2 Canary Deployment
- **Type:** GATE
- **Participants:** system ↔ agent
- **Flow:** Gradual traffic ramp (1% → 10% → 50% → 100%) gated by trust tier.
- **Today:** Manual canary with human monitoring.
- **Helix must provide:** Automated canary with behavior contract verification at each step. Any breach → auto-rollback. Veteran agents deploy 8x faster than Provisional.

### 9.3 Release Signoff
- **Type:** GATE
- **Participants:** human ↔ system, agent ↔ system
- **Flow:** Human approves release for intent. Agents verify all gates passed.
- **Today:** Human-only signoff.
- **Helix must provide:** Dual signoff dashboard. Human approves the CHANGE (intent, timing, risk acceptance). Agent verifies all TECHNICAL gates are green. Both signatures required.

---

## Phase 10: Production Monitoring & Incident Response

### 10.1 Behavior Contract Surveillance
- **Type:** OBSERVATION → GATE
- **Participants:** system ↔ agent
- **Flow:** Continuous monitoring of deployed code against its behavior contract (assertions on latency, error rate, resource usage).
- **Today:** Dashboards and alerts. Reactive, noisy.
- **Helix must provide:** Contract-aware monitoring. Each deployed change has a linked contract. Breach → auto-rollback → agent notification → trust penalty. Drift detection from merge-time baseline.

### 10.2 Incident Detection
- **Type:** OBSERVATION
- **Participants:** system ↔ agent, system ↔ human
- **Flow:** Anomaly detected in production.
- **Today:** Alert fires, on-call human responds.
- **Helix must provide:** Agent receives incident with full context package (recent deploys, changed code paths, related agents). Agent produces initial diagnosis (causal chain, affected services, severity estimate) before human engages. Human reviews and confirms/redirects.

### 10.3 Incident Attribution
- **Type:** LEARNING
- **Participants:** system ↔ agent
- **Flow:** Post-incident, trace causal chain back to specific commits and agents.
- **Today:** Blameless postmortem. Subjective, incomplete.
- **Helix must provide:** Automated causal chain tracing. Every incident attributes to author agent (70%), reviewer agent(s) (20%), approving human (10%). Time-weighted decay prevents punishing agents for latent bugs. Attribution feeds trust ledger.

### 10.4 Incident Learning
- **Type:** LEARNING
- **Participants:** agent ↔ agent, system ↔ agent
- **Flow:** Incident is recorded in shared learning database. All agents can query: "has this failure pattern occurred before?"
- **Today:** Postmortem doc. Siloed, not machine-readable.
- **Helix must provide:** Structured incident database queryable by agents. Incident type, causal chain, resolution, and affected code patterns are indexed. Future agent reviews reference incident history for similar changes.

---

## Phase 11: Trust & Reputation Feedback

### 11.1 Trust Score Recalculation
- **Type:** GATE
- **Participants:** system ↔ agent
- **Flow:** After every merge, review, or incident, recalculate trust score from ledger replay.
- **Today:** No automated trust.
- **Helix must provide:** Deterministic trust scoring from append-only ledger. Six dimensions (merge success rate, incident attribution, review consensus, prompt integrity, human feedback, tenure). Trust score is replay-verifiable.

### 11.2 Tier Promotion/Demotion
- **Type:** GATE
- **Participants:** system ↔ agent
- **Flow:** Agent crosses tier threshold → permissions, cost caps, and review requirements change automatically.
- **Today:** Human decides access levels. Slow, political.
- **Helix must provide:** Automatic tier transitions based on trust score thresholds. No human can promote an agent. Demotion is automatic on sustained trust decay. Transition events are recorded in trust ledger.

### 11.3 Human Feedback Integration
- **Type:** COLLABORATION
- **Participants:** human ↔ system
- **Flow:** Human rates agent's work (code quality, review helpfulness, communication). Rating feeds trust score.
- **Today:** Informal feedback, if any.
- **Helix must provide:** Structured rating interface. Human rates specific dimensions. Rating is weighted low (10%) to prevent popularity contests — outcomes dominate.

---

## Phase 12: Learning & Knowledge Transfer

### 12.1 Pattern Discovery
- **Type:** LEARNING
- **Participants:** agent ↔ system
- **Flow:** Agent analyzes incident database, trust ledger, and codebase to discover systemic patterns: "auth bugs cluster in session refresh," "agents from provider X have higher incident rates on database migrations."
- **Today:** Human intuition. Postmortem themes discovered slowly.
- **Helix must provide:** Automated pattern mining across all system data. Patterns become annotated knowledge that future reviews reference.

### 12.2 Agent Skill Transfer
- **Type:** LEARNING
- **Participants:** agent ↔ agent
- **Flow:** Agent that excels at a domain (e.g., database migrations) packages its approach as a skill that other agents can load.
- **Today:** Doesn't exist. Each agent starts from zero.
- **Helix must provide:** Marketplace-published skills. An agent with high trust in a domain can publish reusable patterns. Other agents load these skills. Skill effectiveness is tracked — ineffective skills lose trust weighting.

### 12.3 Cross-Agent Context Sharing
- **Type:** COLLABORATION
- **Participants:** agent ↔ agent
- **Flow:** Agent working on a task discovers something relevant to another agent's active task. Shares finding.
- **Today:** Human relays information. Lossy, slow.
- **Helix must provide:** Agent-to-agent notification bus. Structured findings with evidence links. Agents can subscribe to domains. Context is budget-tracked.

### 12.4 Model Evaluation & Rotation
- **Type:** GATE
- **Participants:** system ↔ agent
- **Flow:** Models are continuously evaluated against incident attribution rates, false positive rates, and trust score trends of agents using them.
- **Today:** Benchmarks disconnected from production outcomes.
- **Helix must provide:** Production-correlated model scoring. Model X's incident rate vs Model Y's. Models with high false positive rates (>15%) are removed from review rotation. Model performance feeds agent selection recommendations.
