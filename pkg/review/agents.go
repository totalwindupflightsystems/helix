package review

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// =============================================================================
// Adversarial Agent Dispatcher
//
// Per spec §Adversarial Agent Techniques:
//
//	Helix dispatches specialized adversarial agents during review:
//
//	| Agent             | Trigger                        | Function                              |
//	|-------------------|--------------------------------|---------------------------------------|
//	| @assumption-buster| Any behavioral change          | Enumerates and challenges assumptions |
//	| @redteam          | Auth, crypto, secrets handling | Attempts to find exploit paths        |
//	| @chaos-engineer   | Resilience changes             | Injects faults to verify recovery     |
//	| @cost-auditor     | All changes                    | Estimates token cost, flags budget    |
//
//	These agents are NOT reviewers. They are prosecutors. Their job is to
//	prove the code wrong. If they can't, the code passes.
// =============================================================================

// AgentType identifies a specialized adversarial agent.
type AgentType string

const (
	AgentAssumptionBuster AgentType = "assumption-buster"
	AgentRedTeam          AgentType = "redteam"
	AgentChaosEngineer    AgentType = "chaos-engineer"
	AgentCostAuditor      AgentType = "cost-auditor"
)

// AgentInfo describes an adversarial agent's identity and mission.
type AgentInfo struct {
	Type        AgentType `json:"type"`
	Name        string    `json:"name"`
	Mission     string    `json:"mission"`
	ModelHint   string    `json:"model_hint,omitempty"`
	ProviderHint string   `json:"provider_hint,omitempty"`
}

// AgentTrigger defines when an agent should be dispatched based on
// the change category and code patterns detected.
type AgentTrigger struct {
	Agent     AgentType
	Category  ChangeCategory // empty = any
	FilePattern string      // empty = any; e.g. "auth", "crypto", "secret"
	Required  bool           // if true, the agent is mandatory for matching changes
}

// Default triggers per spec.
var DefaultTriggers = []AgentTrigger{
	{Agent: AgentAssumptionBuster, Category: CategoryBehavioral, Required: true},
	{Agent: AgentAssumptionBuster, Category: CategoryContract, Required: true},
	{Agent: AgentRedTeam, FilePattern: "auth", Required: true},
	{Agent: AgentRedTeam, FilePattern: "crypto", Required: true},
	{Agent: AgentRedTeam, FilePattern: "secret", Required: true},
	{Agent: AgentRedTeam, Category: CategoryContract, FilePattern: "auth", Required: true},
	{Agent: AgentChaosEngineer, Category: CategoryResilience, Required: true},
	{Agent: AgentCostAuditor, Required: true}, // all changes
}

// =============================================================================
// ProsecutorAgent interface
// =============================================================================

// ProsecutorAgent is the interface each adversarial agent implements.
// Unlike ModelClient (which reviews code), a prosecutor actively tries to
// break it.
type ProsecutorAgent interface {
	// Prosecute attempts to find faults, exploits, or hidden costs.
	Prosecute(ctx context.Context, req AgentRequest) (*AgentResult, error)
	// Identity returns the agent's metadata.
	Identity() AgentInfo
}

// AgentRequest is the input sent to a prosecutor agent.
type AgentRequest struct {
	Diff           string        `json:"diff"`
	CommitMsg      string        `json:"commit_msg"`
	ChangeCategory ChangeCategory `json:"change_category"`
	ChangedFiles   []string      `json:"changed_files"`
	PRURL          string        `json:"pr_url"`
}

// AgentResult is what a prosecutor agent returns after attempting to break code.
type AgentResult struct {
	AgentType         AgentType        `json:"agent_type"`
	ExploitPaths      []ExploitPath    `json:"exploit_paths,omitempty"`
	AssumptionsFound  []Assumption     `json:"assumptions_challenged,omitempty"`
	FaultInjections   []FaultResult    `json:"fault_injections,omitempty"`
	CostEstimate      *CostEstimate    `json:"cost_estimate,omitempty"`
	Findings          []Finding        `json:"findings,omitempty"`
	Verdict           string           `json:"verdict"` // "clean", "suspicious", "exploited"
	Duration          time.Duration    `json:"duration"`
}

// ExploitPath describes a security vulnerability an agent found.
type ExploitPath struct {
	Description string `json:"description"`
	Severity    string `json:"severity"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Steps       []string `json:"steps"` // attack steps
	Evidence    string `json:"evidence"`
}

// Assumption is an implicit assumption the code makes that may not hold.
type Assumption struct {
	Description    string `json:"description"`
	File           string `json:"file"`
	Line           int    `json:"line"`
	RiskLevel      string `json:"risk_level"` // "low", "medium", "high"
	Challenge      string `json:"challenge"`  // why it might not hold
}

// FaultResult is the outcome of a chaos-engineer fault injection.
type FaultResult struct {
	FaultType   string `json:"fault_type"` // "network", "disk", "memory", "timeout"
	Description string `json:"description"`
	Outcome     string `json:"outcome"` // "recovered", "crashed", "degraded"
	Severity    string `json:"severity"`
}

// CostEstimate is the cost-auditor's assessment.
type CostEstimate struct {
	EstimatedTokens  int64   `json:"estimated_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	BudgetRemaining  float64 `json:"budget_remaining"`
	OverBudget       bool    `json:"over_budget"`
	Notes            string  `json:"notes,omitempty"`
}

// =============================================================================
// Agent registry
// =============================================================================

// agentRegistry maps agent types to their implementations.
type agentRegistry struct {
	mu     sync.RWMutex
	agents map[AgentType]ProsecutorAgent
}

func newAgentRegistry() *agentRegistry {
	return &agentRegistry{agents: make(map[AgentType]ProsecutorAgent)}
}

func (r *agentRegistry) Register(agent ProsecutorAgent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[agent.Identity().Type] = agent
}

func (r *agentRegistry) Get(at AgentType) (ProsecutorAgent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.agents[at]
	return a, ok
}

func (r *agentRegistry) All() []ProsecutorAgent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]ProsecutorAgent, 0, len(r.agents))
	for _, a := range r.agents {
		result = append(result, a)
	}
	return result
}

// =============================================================================
// AdversarialAgentDispatcher
// =============================================================================

// AdversarialAgentDispatcher selects and launches specialized adversarial
// agents based on the change category and file patterns.
type AdversarialAgentDispatcher struct {
	registry  *agentRegistry
	triggers  []AgentTrigger
	fpTracker *FPTracker
}

// DispatcherOption configures the dispatcher.
type DispatcherOption func(*AdversarialAgentDispatcher)

// WithDispatcherTriggers sets custom trigger rules.
func WithDispatcherTriggers(triggers []AgentTrigger) DispatcherOption {
	return func(d *AdversarialAgentDispatcher) { d.triggers = triggers }
}

// WithDispatcherFPTracker sets a shared false positive tracker.
func WithDispatcherFPTracker(fp *FPTracker) DispatcherOption {
	return func(d *AdversarialAgentDispatcher) { d.fpTracker = fp }
}

// NewAdversarialAgentDispatcher creates a dispatcher with default triggers.
func NewAdversarialAgentDispatcher(opts ...DispatcherOption) *AdversarialAgentDispatcher {
	d := &AdversarialAgentDispatcher{
		registry: newAgentRegistry(),
		triggers: DefaultTriggers,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Register adds a prosecutor agent to the dispatcher.
func (d *AdversarialAgentDispatcher) Register(agent ProsecutorAgent) {
	d.registry.Register(agent)
}

// SelectAgents determines which agents should be dispatched for a given
// change category and set of changed files. Returns the selected agent types
// and an error if any required agent is missing from the registry.
func (d *AdversarialAgentDispatcher) SelectAgents(cat ChangeCategory, changedFiles []string) ([]AgentType, error) {
	filePatterns := extractFilePatterns(changedFiles)

	var selected []AgentType
	seen := make(map[AgentType]bool)

	for _, trigger := range d.triggers {
		// Check category match (empty category = matches all)
		if trigger.Category != "" && trigger.Category != cat {
			continue
		}
		// Check file pattern match (empty pattern = matches all)
		if trigger.FilePattern != "" && !filePatterns[trigger.FilePattern] {
			continue
		}
		// Check if agent is registered
		agent, ok := d.registry.Get(trigger.Agent)
		if !ok {
			if trigger.Required {
				return nil, fmt.Errorf("required agent %q not registered", trigger.Agent)
			}
			continue
		}
		if !seen[trigger.Agent] {
			selected = append(selected, trigger.Agent)
			seen[trigger.Agent] = true
		}
		_ = agent // agent lookup for validation only
	}

	return selected, nil
}

// Dispatch launches all selected agents concurrently for a review request.
// Returns a combined report with all agent results.
func (d *AdversarialAgentDispatcher) Dispatch(ctx context.Context, req AgentRequest) (*DispatchReport, error) {
	if req.Diff == "" && len(req.ChangedFiles) == 0 {
		return nil, fmt.Errorf("dispatch request needs diff or changed files")
	}

	// Select agents for this change.
	selected, err := d.SelectAgents(req.ChangeCategory, req.ChangedFiles)
	if err != nil {
		return nil, fmt.Errorf("agent selection: %w", err)
	}

	if len(selected) == 0 {
		return &DispatchReport{
			PRURL:      req.PRURL,
			AgentsRun:  0,
			Results:    []AgentResult{},
			CleanBill:  true,
		}, nil
	}

	// Dispatch each agent concurrently.
	type agentOutput struct {
		result *AgentResult
		err    error
	}

	results := make(chan agentOutput, len(selected))
	var wg sync.WaitGroup

	for _, at := range selected {
		agent, ok := d.registry.Get(at)
		if !ok {
			continue
		}
		wg.Add(1)
		go func(a ProsecutorAgent) {
			defer wg.Done()
			start := time.Now()
			res, err := a.Prosecute(ctx, req)
			if res != nil {
				res.Duration = time.Since(start)
			}
			results <- agentOutput{result: res, err: err}
		}(agent)
	}
	wg.Wait()
	close(results)

	report := &DispatchReport{
		PRURL:     req.PRURL,
		Timestamp: time.Now().UTC(),
		Results:   []AgentResult{},
		CleanBill: true, // starts clean, set false when any agent finds issues
	}

	for out := range results {
		if out.err != nil {
			report.Errors = append(report.Errors, out.err.Error())
			continue
		}
		if out.result != nil {
			report.Results = append(report.Results, *out.result)
			if out.result.Verdict != "clean" {
				report.CleanBill = false
			}
		}
	}

	report.AgentsRun = len(report.Results)
	if report.AgentsRun == 0 && len(report.Errors) > 0 {
		return nil, fmt.Errorf("all agents failed: %v", report.Errors)
	}

	return report, nil
}

// =============================================================================
// DispatchReport
// =============================================================================

// DispatchReport aggregates results from all dispatched adversarial agents.
type DispatchReport struct {
	PRURL     string        `json:"pr_url"`
	Timestamp time.Time     `json:"timestamp"`
	AgentsRun int           `json:"agents_run"`
	Results   []AgentResult `json:"results"`
	Errors    []string      `json:"errors,omitempty"`
	// CleanBill is true when all agents returned "clean" verdict.
	CleanBill bool `json:"clean_bill"`
}

// TotalExploits returns the total number of exploit paths found across all agents.
func (r *DispatchReport) TotalExploits() int {
	total := 0
	for _, res := range r.Results {
		total += len(res.ExploitPaths)
	}
	return total
}

// TotalAssumptions returns the total assumptions challenged.
func (r *DispatchReport) TotalAssumptions() int {
	total := 0
	for _, res := range r.Results {
		total += len(res.AssumptionsFound)
	}
	return total
}

// CriticalExploits returns exploits with "critical" severity from all agents.
func (r *DispatchReport) CriticalExploits() []ExploitPath {
	var critical []ExploitPath
	for _, res := range r.Results {
		for _, ep := range res.ExploitPaths {
			if ep.Severity == "critical" {
				critical = append(critical, ep)
			}
		}
	}
	return critical
}

// AllFindings flattens findings from all agents into a single slice.
func (r *DispatchReport) AllFindings() []Finding {
	var all []Finding
	for _, res := range r.Results {
		all = append(all, res.Findings...)
	}
	return all
}

// Summary returns a one-line summary of the dispatch.
func (r *DispatchReport) Summary() string {
	return fmt.Sprintf("%d agents ran, %d exploits found, %d assumptions challenged, clean=%v",
		r.AgentsRun, r.TotalExploits(), r.TotalAssumptions(), r.CleanBill)
}

// =============================================================================
// Helpers
// =============================================================================

// extractFilePatterns scans changed file paths and returns a set of
// pattern keywords found (e.g., "auth" if any file contains "auth").
func extractFilePatterns(files []string) map[string]bool {
	patterns := make(map[string]bool)
	keywords := []string{"auth", "crypto", "secret", "token", "password", "key", "session", "permission", "rbac"}
	for _, f := range files {
		lower := toLower(f)
		for _, kw := range keywords {
			if indexOf(lower, kw) >= 0 {
				patterns[kw] = true
			}
		}
	}
	return patterns
}

func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		result[i] = c
	}
	return string(result)
}

// containsStr is defined in orchestrator_test.go (test-only helper).
// We use indexOf directly for non-test code.

// =============================================================================
// Default agent implementations (stub prosecutors)
// =============================================================================

// StubAgent is a configurable prosecutor agent for testing and default behavior.
type StubAgent struct {
	info   AgentInfo
	result *AgentResult
	err    error
}

// NewStubAgent creates a prosecutor that returns a preset result.
func NewStubAgent(info AgentInfo, result *AgentResult) *StubAgent {
	return &StubAgent{info: info, result: result}
}

// NewErrorAgent creates a prosecutor that always errors.
func NewErrorAgent(info AgentInfo, err error) *StubAgent {
	return &StubAgent{info: info, err: err}
}

func (s *StubAgent) Prosecute(_ context.Context, _ AgentRequest) (*AgentResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func (s *StubAgent) Identity() AgentInfo {
	return s.info
}

// DefaultAgentInfo returns the standard AgentInfo for a given agent type.
func DefaultAgentInfo(at AgentType) AgentInfo {
	switch at {
	case AgentAssumptionBuster:
		return AgentInfo{
			Type:    at,
			Name:    "@assumption-buster",
			Mission: "Enumerate and challenge every implicit assumption in the code",
		}
	case AgentRedTeam:
		return AgentInfo{
			Type:    at,
			Name:    "@redteam",
			Mission: "Find exploit paths, privilege escalation, and data leakage",
		}
	case AgentChaosEngineer:
		return AgentInfo{
			Type:    at,
			Name:    "@chaos-engineer",
			Mission: "Inject faults to verify recovery and resilience behavior",
		}
	case AgentCostAuditor:
		return AgentInfo{
			Type:    at,
			Name:    "@cost-auditor",
			Mission: "Estimate token cost and flag budget overruns",
		}
	default:
		return AgentInfo{Type: at, Name: string(at), Mission: "unknown"}
	}
}
