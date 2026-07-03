package health

import (
	"fmt"
	"sync"
	"time"
)

// SLA targets from spec §11.

// SyncLatencyTarget encodes spec §11.1 sync latency targets.
type SyncLatencyTarget struct {
	TaskToAgentAssigned LatencyBudget // P50: 2s, P95: 10s, P99: 30s
	AgentToWorktree     LatencyBudget // P50: 3s, P95: 8s, P99: 15s
	WorktreeToFirstCall LatencyBudget // P50: 5s, P95: 15s, P99: 30s
}

// ReviewLatencyTarget encodes spec §11.2 review latency targets per gate.
type ReviewLatencyTarget struct {
	Tier1              LatencyBudget // P50: 2s, P95: 5s, P99: 8s
	Tier2              LatencyBudget // P50: 20s, P95: 60s, P99: 90s
	Chimera            LatencyBudget // P50: 90s, P95: 240s, P99: 300s
	Conscientiousness1 LatencyBudget // P50: 60s, P95: 180s, P99: 300s (1 iter)
	Conscientiousness3 LatencyBudget // P50: 180s, P95: 420s, P99: 600s (3 iter)
	PromptFoo          LatencyBudget // P50: 30s, P95: 90s, P99: 120s
}

// MergeThroughputTarget encodes spec §11.3 merge throughput targets.
type MergeThroughputTarget struct {
	SimplePerDay      int // 4-8 PRs/day
	MediumPerDay      int // 2-4 PRs/day
	SwarmPerDay       int // 12-20 PRs/day (3 agents parallel)
	PlatformMaxPerDay int // 80 PRs/day (20 agents, 4 repos)
}

// SandboxStartupTarget encodes spec §11.4 sandbox startup targets.
type SandboxStartupTarget struct {
	ColdStart time.Duration // 16s total
	WarmStart time.Duration // 4s total
}

// APILatencyTarget encodes spec §11.5 API latency targets.
type APILatencyTarget struct {
	ForgejoRead   LatencyBudget // P50: 15ms, P95: 50ms, P99: 100ms
	ForgejoWrite  LatencyBudget // P50: 30ms, P95: 100ms, P99: 200ms
	HivemindQuery LatencyBudget // P50: 20ms, P95: 80ms, P99: 150ms
	HivemindWrite LatencyBudget // P50: 30ms, P95: 100ms, P99: 200ms
	GitCloneWarm  LatencyBudget // P50: 1s, P95: 3s, P99: 5s
	GitCloneCold  LatencyBudget // P50: 8s, P95: 15s, P99: 25s
	LangFuseWrite LatencyBudget // P50: 50ms, P95: 200ms, P99: 500ms
}

// CostPerPRTarget encodes spec §11.6 cost per PR targets.
type CostPerPRTarget struct {
	SimpleLow        float64 // $0.10
	SimpleHigh       float64 // $0.30
	MediumLow        float64 // $0.50
	MediumHigh       float64 // $2.00
	ComplexLow       float64 // $3.00
	ComplexHigh      float64 // $10.00
	AdversarialExtra float64 // $0.50-2.00
	PromptFooPerRun  float64 // $0.05-0.15
}

// LatencyBudget holds P50/P95/P99 latency targets.
type LatencyBudget struct {
	P50 time.Duration
	P95 time.Duration
	P99 time.Duration
}

// DefaultSLATargets returns the spec §11 SLA targets.
func DefaultSLATargets() *SLATargets {
	return &SLATargets{
		Sync: SyncLatencyTarget{
			TaskToAgentAssigned: LatencyBudget{P50: 2 * time.Second, P95: 10 * time.Second, P99: 30 * time.Second},
			AgentToWorktree:     LatencyBudget{P50: 3 * time.Second, P95: 8 * time.Second, P99: 15 * time.Second},
			WorktreeToFirstCall: LatencyBudget{P50: 5 * time.Second, P95: 15 * time.Second, P99: 30 * time.Second},
		},
		Review: ReviewLatencyTarget{
			Tier1:              LatencyBudget{P50: 2 * time.Second, P95: 5 * time.Second, P99: 8 * time.Second},
			Tier2:              LatencyBudget{P50: 20 * time.Second, P95: 60 * time.Second, P99: 90 * time.Second},
			Chimera:            LatencyBudget{P50: 90 * time.Second, P95: 240 * time.Second, P99: 300 * time.Second},
			Conscientiousness1: LatencyBudget{P50: 60 * time.Second, P95: 180 * time.Second, P99: 300 * time.Second},
			Conscientiousness3: LatencyBudget{P50: 180 * time.Second, P95: 420 * time.Second, P99: 600 * time.Second},
			PromptFoo:          LatencyBudget{P50: 30 * time.Second, P95: 90 * time.Second, P99: 120 * time.Second},
		},
		Merge: MergeThroughputTarget{
			SimplePerDay:      4,
			MediumPerDay:      2,
			SwarmPerDay:       12,
			PlatformMaxPerDay: 80,
		},
		Sandbox: SandboxStartupTarget{
			ColdStart: 16 * time.Second,
			WarmStart: 4 * time.Second,
		},
		API: APILatencyTarget{
			ForgejoRead:   LatencyBudget{P50: 15 * time.Millisecond, P95: 50 * time.Millisecond, P99: 100 * time.Millisecond},
			ForgejoWrite:  LatencyBudget{P50: 30 * time.Millisecond, P95: 100 * time.Millisecond, P99: 200 * time.Millisecond},
			HivemindQuery: LatencyBudget{P50: 20 * time.Millisecond, P95: 80 * time.Millisecond, P99: 150 * time.Millisecond},
			HivemindWrite: LatencyBudget{P50: 30 * time.Millisecond, P95: 100 * time.Millisecond, P99: 200 * time.Millisecond},
			GitCloneWarm:  LatencyBudget{P50: 1 * time.Second, P95: 3 * time.Second, P99: 5 * time.Second},
			GitCloneCold:  LatencyBudget{P50: 8 * time.Second, P95: 15 * time.Second, P99: 25 * time.Second},
			LangFuseWrite: LatencyBudget{P50: 50 * time.Millisecond, P95: 200 * time.Millisecond, P99: 500 * time.Millisecond},
		},
		Cost: CostPerPRTarget{
			SimpleLow:        0.10,
			SimpleHigh:       0.30,
			MediumLow:        0.50,
			MediumHigh:       2.00,
			ComplexLow:       3.00,
			ComplexHigh:      10.00,
			AdversarialExtra: 2.00,
			PromptFooPerRun:  0.15,
		},
		Monitoring: MonitoringSLA{
			ScrapeInterval:  15 * time.Second,
			TraceIngestion:  5 * time.Second,
			AlertEvaluation: 30 * time.Second,
		},
	}
}

// SLATargets aggregates all spec §11 SLA targets.
type SLATargets struct {
	Sync       SyncLatencyTarget
	Review     ReviewLatencyTarget
	Merge      MergeThroughputTarget
	Sandbox    SandboxStartupTarget
	API        APILatencyTarget
	Cost       CostPerPRTarget
	Monitoring MonitoringSLA
}

// MonitoringSLA encodes spec §11.7 monitoring SLA targets.
type MonitoringSLA struct {
	ScrapeInterval  time.Duration // 15s
	TraceIngestion  time.Duration // <5s
	AlertEvaluation time.Duration // 30s
}

// SLABreach represents a single SLA violation.
type SLABreach struct {
	SLAName    string        // e.g. "sync.task_to_agent_assigned"
	Percentile string        // "P50", "P95", "P99"
	Target     time.Duration // expected
	Actual     time.Duration // observed
	Exceeded   time.Duration // actual - target
}

// IsBreached returns true if actual exceeds the target for the given percentile.
func (b LatencyBudget) IsBreached(percentile string, actual time.Duration) bool {
	var target time.Duration
	switch percentile {
	case "P50":
		target = b.P50
	case "P95":
		target = b.P95
	case "P99":
		target = b.P99
	default:
		return false
	}
	return actual > target
}

// CheckLatency compares an observed latency against the budget and returns a breach if exceeded.
func CheckLatency(name, percentile string, budget LatencyBudget, actual time.Duration) *SLABreach {
	if budget.IsBreached(percentile, actual) {
		var target time.Duration
		switch percentile {
		case "P50":
			target = budget.P50
		case "P95":
			target = budget.P95
		case "P99":
			target = budget.P99
		}
		return &SLABreach{
			SLAName:    name,
			Percentile: percentile,
			Target:     target,
			Actual:     actual,
			Exceeded:   actual - target,
		}
	}
	return nil
}

// SLARecorder tracks observed latencies and checks them against targets.
type SLARecorder struct {
	mu       sync.RWMutex
	targets  *SLATargets
	breaches []SLABreach
	samples  map[string][]time.Duration // key: "name.percentile"
}

// NewSLARecorder creates a new recorder with the given targets.
func NewSLARecorder(targets *SLATargets) *SLARecorder {
	if targets == nil {
		targets = DefaultSLATargets()
	}
	return &SLARecorder{
		targets: targets,
		samples: make(map[string][]time.Duration),
	}
}

// RecordSyncLatency records a sync latency observation and checks against targets.
func (r *SLARecorder) RecordSyncLatency(stage string, actual time.Duration) *SLABreach {
	r.mu.Lock()
	defer r.mu.Unlock()

	var budget LatencyBudget
	switch stage {
	case "task_to_agent_assigned":
		budget = r.targets.Sync.TaskToAgentAssigned
	case "agent_to_worktree":
		budget = r.targets.Sync.AgentToWorktree
	case "worktree_to_first_call":
		budget = r.targets.Sync.WorktreeToFirstCall
	default:
		return nil
	}

	key := fmt.Sprintf("sync.%s", stage)
	r.samples[key] = append(r.samples[key], actual)

	// Check P99 against the single sample (conservative)
	breach := CheckLatency(key, "P99", budget, actual)
	if breach != nil {
		r.breaches = append(r.breaches, *breach)
	}
	return breach
}

// RecordReviewLatency records a review gate latency observation.
func (r *SLARecorder) RecordReviewLatency(gate string, actual time.Duration) *SLABreach {
	r.mu.Lock()
	defer r.mu.Unlock()

	var budget LatencyBudget
	switch gate {
	case "tier1":
		budget = r.targets.Review.Tier1
	case "tier2":
		budget = r.targets.Review.Tier2
	case "chimera":
		budget = r.targets.Review.Chimera
	case "conscientiousness_1":
		budget = r.targets.Review.Conscientiousness1
	case "conscientiousness_3":
		budget = r.targets.Review.Conscientiousness3
	case "promptfoo":
		budget = r.targets.Review.PromptFoo
	default:
		return nil
	}

	key := fmt.Sprintf("review.%s", gate)
	r.samples[key] = append(r.samples[key], actual)

	breach := CheckLatency(key, "P99", budget, actual)
	if breach != nil {
		r.breaches = append(r.breaches, *breach)
	}
	return breach
}

// RecordAPILatency records an API latency observation.
func (r *SLARecorder) RecordAPILatency(endpoint string, actual time.Duration) *SLABreach {
	r.mu.Lock()
	defer r.mu.Unlock()

	var budget LatencyBudget
	switch endpoint {
	case "forgejo_read":
		budget = r.targets.API.ForgejoRead
	case "forgejo_write":
		budget = r.targets.API.ForgejoWrite
	case "hivemind_query":
		budget = r.targets.API.HivemindQuery
	case "hivemind_write":
		budget = r.targets.API.HivemindWrite
	case "git_clone_warm":
		budget = r.targets.API.GitCloneWarm
	case "git_clone_cold":
		budget = r.targets.API.GitCloneCold
	case "langfuse_write":
		budget = r.targets.API.LangFuseWrite
	default:
		return nil
	}

	key := fmt.Sprintf("api.%s", endpoint)
	r.samples[key] = append(r.samples[key], actual)

	breach := CheckLatency(key, "P99", budget, actual)
	if breach != nil {
		r.breaches = append(r.breaches, *breach)
	}
	return breach
}

// RecordSandboxStartup records a sandbox startup time observation.
func (r *SLARecorder) RecordSandboxStartup(cold bool, actual time.Duration) *SLABreach {
	r.mu.Lock()
	defer r.mu.Unlock()

	var target time.Duration
	key := "sandbox.warm"
	if cold {
		target = r.targets.Sandbox.ColdStart
		key = "sandbox.cold"
	} else {
		target = r.targets.Sandbox.WarmStart
	}

	r.samples[key] = append(r.samples[key], actual)

	if actual > target {
		breach := &SLABreach{
			SLAName:    key,
			Percentile: "P100",
			Target:     target,
			Actual:     actual,
			Exceeded:   actual - target,
		}
		r.breaches = append(r.breaches, *breach)
		return breach
	}
	return nil
}

// CheckCostPerPR checks if a PR cost is within the target range for its complexity.
func (r *SLARecorder) CheckCostPerPR(complexity string, cost float64) *CostBreach {
	var low, high float64
	switch complexity {
	case "simple":
		low = r.targets.Cost.SimpleLow
		high = r.targets.Cost.SimpleHigh
	case "medium":
		low = r.targets.Cost.MediumLow
		high = r.targets.Cost.MediumHigh
	case "complex":
		low = r.targets.Cost.ComplexLow
		high = r.targets.Cost.ComplexHigh
	default:
		return nil
	}

	if cost > high {
		return &CostBreach{
			Complexity:  complexity,
			ExpectedMax: high,
			ExpectedMin: low,
			Actual:      cost,
			OverBy:      cost - high,
		}
	}
	return nil
}

// CostBreach represents a cost-per-PR SLA violation.
type CostBreach struct {
	Complexity  string
	ExpectedMin float64
	ExpectedMax float64
	Actual      float64
	OverBy      float64
}

// Breaches returns all recorded SLA breaches.
func (r *SLARecorder) Breaches() []SLABreach {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]SLABreach, len(r.breaches))
	copy(out, r.breaches)
	return out
}

// HasBreaches returns true if any SLA violations have been recorded.
func (r *SLARecorder) HasBreaches() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.breaches) > 0
}

// Samples returns the recorded latency samples for a given key.
func (r *SLARecorder) Samples(key string) []time.Duration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out, ok := r.samples[key]
	if !ok {
		return nil
	}
	return append([]time.Duration(nil), out...)
}

// Reset clears all recorded breaches and samples.
func (r *SLARecorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.breaches = nil
	r.samples = make(map[string][]time.Duration)
}

// FormatBreach renders a single SLA breach as a human-readable string.
func FormatBreach(b *SLABreach) string {
	if b == nil {
		return ""
	}
	return fmt.Sprintf("SLA BREACH: %s %s — target %v, actual %v (exceeded by %v)",
		b.SLAName, b.Percentile, b.Target, b.Actual, b.Exceeded)
}

// FormatCostBreach renders a cost-per-PR breach.
func FormatCostBreach(b *CostBreach) string {
	if b == nil {
		return ""
	}
	return fmt.Sprintf("COST BREACH: %s PR — expected $%.2f-$%.2f, actual $%.2f (over by $%.2f)",
		b.Complexity, b.ExpectedMin, b.ExpectedMax, b.Actual, b.OverBy)
}
