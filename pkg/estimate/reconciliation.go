package estimate

import (
	"fmt"
	"math"
)

// ---------------------------------------------------------------------------
// Post-execution reconciliation (spec §8.2, §12)
// ---------------------------------------------------------------------------

// Usage represents the post-execution token counts reported by the GitReins
// v0.4.1 evaluator (spec §3.3). Unlike the estimator's projections, these are
// real observed counts, so reconciliation uses actual prices with no cache
// ratio estimation.
type Usage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
}

// ActualCost computes a CostEstimate from observed Usage and pricing data. The
// token breakdown maps cleanly onto the cache-aware model:
//
//   - FreshInput  = prompt_tokens - cache_read_tokens (tokens charged at full input rate)
//   - CacheHits   = cache_read_tokens
//   - CacheWrites = cache_write_tokens
//   - Output      = completion_tokens
//
// because GitReins reports real cache hits rather than a ratio, no cache ratio
// estimation is performed. CostTotal is rounded UP to the nearest cent.
func ActualCost(usage Usage, pricing *PricingYAML, provider, model string) (CostEstimate, error) {
	if pricing == nil {
		return CostEstimate{}, fmt.Errorf("pricing data is nil")
	}
	price, err := pricing.GetModelPrice(provider, model)
	if err != nil {
		return CostEstimate{}, err
	}

	perM := 1000.0 // prices are per_1k

	fresh := usage.PromptTokens - usage.CacheReadTokens
	if fresh < 0 {
		fresh = 0
	}
	cacheHit := usage.CacheReadTokens
	if cacheHit < 0 {
		cacheHit = 0
	}
	cacheWrite := usage.CacheWriteTokens
	if cacheWrite < 0 {
		cacheWrite = 0
	}
	output := usage.CompletionTokens
	if output < 0 {
		output = 0
	}

	costFresh := float64(fresh) / perM * price.InputPer1K
	costHit := float64(cacheHit) / perM * price.GetCacheReadPrice()
	costWrite := float64(cacheWrite) / perM * price.GetCacheWritePrice()
	costOutput := float64(output) / perM * price.OutputPer1K

	costInput := costFresh + costHit + costWrite
	rawTotal := costInput + costOutput
	costTotal := math.Ceil(rawTotal*100) / 100

	var ratio float64
	if usage.PromptTokens > 0 {
		ratio = float64(usage.CacheReadTokens) / float64(usage.PromptTokens)
	}

	return CostEstimate{
		CostInput:  costInput,
		CostOutput: costOutput,
		CostTotal:  costTotal,
		Tokens: TokenEstimate{
			TotalInput:    usage.PromptTokens,
			FreshInput:    fresh,
			CacheHits:     cacheHit,
			CacheWrites:   cacheWrite,
			Output:        output,
			CacheHitRatio: ratio,
		},
		Model:    model,
		Provider: provider,
	}, nil
}

// ReconcileDrift computes the percentage drift between an estimate and the
// actual cost: (actual - estimated) / estimated * 100. Positive drift means the
// estimate was too low; negative means it was too high. Persistent drift > 20%
// over 20+ tasks triggers recalibration (spec §12, §14).
//
// A zero estimated cost returns +Inf drift when actual is non-zero (the
// estimate was wildly wrong) and 0 otherwise.
func ReconcileDrift(estimated CostEstimate, actual CostEstimate) float64 {
	if estimated.CostTotal == 0 {
		if actual.CostTotal == 0 {
			return 0
		}
		return math.Inf(1)
	}
	return (actual.CostTotal - estimated.CostTotal) / estimated.CostTotal * 100
}
