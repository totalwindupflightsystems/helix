package estimate

import (
	"fmt"
	"math"
)

// ---------------------------------------------------------------------------
// Estimator (spec §7)
// ---------------------------------------------------------------------------

// Estimator projects a TaskDesc into a CostEstimate using the cache-aware
// pricing formula. It holds the loaded pricing data and the agent tier, which
// determines the cache hit/write ratios applied (spec §7.3):
//
//   - "pro":   60% hit / 50% write (default)
//   - "flash": 80% hit / 70% write
//   - "cold":  0% hit / 50% write (first 10 tasks, spec §12.3)
//
// Cold-start selection is driven by budget.IsNewAgent in the CLI layer; the
// estimator simply honors whichever tier it is constructed with.
type Estimator struct {
	Pricing *PricingYAML
	Tier    string // "pro", "flash", or "cold"
}

// NewEstimator constructs an Estimator from loaded pricing data and a tier.
func NewEstimator(pricing *PricingYAML, tier string) *Estimator {
	if tier == "" {
		tier = "pro"
	}
	return &Estimator{Pricing: pricing, Tier: tier}
}

// hitRatio resolves the cache-hit ratio for the estimator's tier.
func (e *Estimator) hitRatio() float64 {
	switch e.Tier {
	case "flash":
		return e.Pricing.Cache.FlashHitRatio
	case "cold":
		return e.Pricing.Cache.NewRepoHitRatio
	default: // "pro" and any unrecognized value
		return e.Pricing.Cache.ProHitRatio
	}
}

// writeRatio resolves the cache-write ratio for the estimator's tier.
func (e *Estimator) writeRatio() float64 {
	switch e.Tier {
	case "flash":
		return e.Pricing.Cache.FlashWriteRatio
	case "cold":
		return e.Pricing.Cache.NewRepoWriteRatio
	default:
		return e.Pricing.Cache.ProWriteRatio
	}
}

// Estimate projects a TaskDesc into a CostEstimate using the cache-aware
// formula from spec §7.1:
//
//	fresh   = total_input * (1 - hit_ratio)
//	cacheHit = total_input * hit_ratio
//	cacheWrite = fresh * write_ratio
//	output  = total_input * output_ratio
//
// where total_input aggregates the task-type base plus the file, iteration, and
// diff adjustments (spec §7.2). Costs are summed across all agents (capped at
// 5 per spec §3.4) and the total is rounded UP to the nearest cent.
func (e *Estimator) Estimate(task TaskDesc) (CostEstimate, error) {
	if e == nil || e.Pricing == nil {
		return CostEstimate{}, fmt.Errorf("estimator not initialized")
	}

	// --- resolve task type + token profile ---
	tt := task.Type
	if tt == "" {
		tt = TaskCode
	}
	if !tt.Valid() {
		return CostEstimate{}, fmt.Errorf("invalid task type %q", tt)
	}
	td, err := e.Pricing.GetTaskDefaults(tt)
	if err != nil {
		return CostEstimate{}, err
	}

	// --- resolve model price ---
	model := task.Model
	if model == "" {
		return CostEstimate{}, fmt.Errorf("model is required")
	}
	price, err := e.Pricing.GetModelPrice(task.Provider, task.Model)
	if err != nil {
		return CostEstimate{}, err
	}

	// --- resolve TaskDesc field defaults (spec §3.5) ---
	files := task.FilesChanged
	if files <= 0 {
		files = 1
	}
	maxIter := task.MaxIterations
	if maxIter <= 0 {
		maxIter = td.MaxIterations
	}
	if maxIter <= 0 {
		maxIter = 20
	}
	diff := task.DiffLines
	if diff < 0 {
		diff = 0
	}
	agents := task.Agents
	if agents <= 0 {
		agents = 1
	}
	if agents > 5 { // spec §3.4: capped at 5
		agents = 5
	}

	// --- total input tokens (spec §7.2) ---
	base := int64(td.InputTokens)
	fileAdj := int64(files) * int64(td.InputPerFile)
	// +TokensPerIter per iteration PAST 10 (spec §3.4, §7.2).
	extraIters := maxIter - 10
	if extraIters < 0 {
		extraIters = 0
	}
	iterAdj := int64(extraIters) * int64(td.TokensPerIter)
	// +1 token per 10 diff lines.
	diffAdj := int64(diff) / 10 * int64(td.TokensPer10Diff)

	perAgentInput := base + fileAdj + iterAdj + diffAdj
	if perAgentInput < 0 {
		perAgentInput = 0
	}

	// --- cache-aware split (spec §7.1, §7.3) ---
	hr := e.hitRatio()
	wr := e.writeRatio()

	// Models without a cache API (MiniMax) get zero cache discount: every input
	// token is charged at the full input rate. We keep the token breakdown
	// honest by forcing the hit ratio to 0 for those models.
	if !price.IsCacheSupported() {
		hr = 0
		// cache writes are still meaningful for future runs but incur no
		// separate charge for MiniMax; charge them at input price (GetCacheWritePrice falls back to input).
	}

	fresh := int64(math.Round(float64(perAgentInput) * (1 - hr)))
	cacheHit := int64(math.Round(float64(perAgentInput) * hr))
	cacheWrite := int64(math.Round(float64(fresh) * wr))
	output := int64(math.Round(float64(perAgentInput) * td.OutputRatio))
	if fresh < 0 {
		fresh = 0
	}
	if cacheHit < 0 {
		cacheHit = 0
	}
	if cacheWrite < 0 {
		cacheWrite = 0
	}
	if output < 0 {
		output = 0
	}

	// --- per-agent cost (prices are per 1K tokens, spec §10) ---
	perM := 1000.0 // prices are per_1k, so divide tokens by 1000
	costFresh := float64(fresh) / perM * price.InputPer1K
	costHit := float64(cacheHit) / perM * price.GetCacheReadPrice()
	costWrite := float64(cacheWrite) / perM * price.GetCacheWritePrice()
	costOutput := float64(output) / perM * price.OutputPer1K

	perAgentInputCost := costFresh + costHit + costWrite
	perAgentOutputCost := costOutput

	// --- aggregate across agents ---
	totalInput := perAgentInput * int64(agents)
	totalFresh := fresh * int64(agents)
	totalCacheHit := cacheHit * int64(agents)
	totalCacheWrite := cacheWrite * int64(agents)
	totalOutput := output * int64(agents)

	costInput := perAgentInputCost * float64(agents)
	costOutputTotal := perAgentOutputCost * float64(agents)
	rawTotal := costInput + costOutputTotal

	// Round UP to the nearest cent (spec §4: overestimate, never underestimate).
	costTotal := math.Ceil(rawTotal*100) / 100

	return CostEstimate{
		CostInput:  costInput,
		CostOutput: costOutputTotal,
		CostTotal:  costTotal,
		Tokens: TokenEstimate{
			TotalInput:    totalInput,
			FreshInput:    totalFresh,
			CacheHits:     totalCacheHit,
			CacheWrites:   totalCacheWrite,
			Output:        totalOutput,
			CacheHitRatio: hr,
		},
		Model:    model,
		Provider: task.Provider,
	}, nil
}
