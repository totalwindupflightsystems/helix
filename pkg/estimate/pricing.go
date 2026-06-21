package estimate

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Pricing data model (spec §3.2, §10)
// ---------------------------------------------------------------------------

// CacheRatios holds the configurable cache hit/write ratios per tier, plus the
// cold-start (new repo) defaults. These drive the cache-aware estimation
// formula in estimator.go (spec §7.3, §12.3).
type CacheRatios struct {
	ProHitRatio      float64 `yaml:"pro_hit_ratio"`
	FlashHitRatio    float64 `yaml:"flash_hit_ratio"`
	ProWriteRatio    float64 `yaml:"pro_write_ratio"`
	FlashWriteRatio  float64 `yaml:"flash_write_ratio"`
	NewRepoThreshold int     `yaml:"new_repo_threshold"`
	NewRepoHitRatio  float64 `yaml:"new_repo_hit_ratio"`
	NewRepoWriteRatio float64 `yaml:"new_repo_write_ratio"`
}

// ModelPrice holds per-1K-token prices for a single model. Prices are stored
// per 1K tokens (matching the pricing.yaml schema in spec §10) and converted
// to per-token at computation time. A nil CacheReadPer1K means the model has
// no cache API (e.g. MiniMax): all input is charged at the full input rate.
// A nil CacheWritePer1K means cache writes are priced the same as input.
type ModelPrice struct {
	InputPer1K      float64  `yaml:"input_per_1k"`
	CacheReadPer1K  *float64 `yaml:"cache_read_per_1k"`
	CacheWritePer1K *float64 `yaml:"cache_write_per_1k"`
	OutputPer1K     float64  `yaml:"output_per_1k"`
}

// IsCacheSupported reports whether the model exposes a cache-read price. Models
// without cache pricing (MiniMax) are estimated using the flat input rate for
// all input tokens (spec §4, §7.3).
func (m ModelPrice) IsCacheSupported() bool {
	return m.CacheReadPer1K != nil
}

// GetCacheReadPrice returns the cache-read price per 1K tokens. If the model
// has no cache API, the regular input price is used (no discount applied).
func (m ModelPrice) GetCacheReadPrice() float64 {
	if m.CacheReadPer1K != nil {
		return *m.CacheReadPer1K
	}
	return m.InputPer1K
}

// GetCacheWritePrice returns the cache-write price per 1K tokens. Falls back to
// the regular input price when the model does not publish a separate cache
// write rate (spec §7.3: "Same as input" for DeepSeek/Z.AI/Google).
func (m ModelPrice) GetCacheWritePrice() float64 {
	if m.CacheWritePer1K != nil {
		return *m.CacheWritePer1K
	}
	return m.InputPer1K
}

// ProviderPricing maps model names to their prices for a single provider.
// MarkupPercent is used by pass-through providers like OpenRouter (spec §3.2);
// direct providers leave it zero.
type ProviderPricing struct {
	Models        map[string]ModelPrice `yaml:"models"`
	MarkupPercent float64               `yaml:"markup_percent,omitempty"`
}

// TaskDefaults holds the token profile for a task type (spec §7.2). These are
// loaded from pricing.yaml under the `tasks` map and may be overridden without
// code changes. The adjustment factors default to the spec values when absent.
type TaskDefaults struct {
	InputTokens     int     `yaml:"input_tokens"`
	OutputRatio     float64 `yaml:"output_ratio"`
	MaxIterations   int     `yaml:"max_iterations"`
	InputPerFile    int     `yaml:"input_per_file"`     // +5000 per file
	TokensPerIter   int     `yaml:"tokens_per_iter"`    // +10000 per iter past 10
	TokensPer10Diff int     `yaml:"tokens_per_10_diff"` // +1 per 10 diff lines
}

// PricingYAML is the in-memory representation of ~/.helix/pricing.yaml.
type PricingYAML struct {
	Version   string                    `yaml:"version"`
	Updated   string                    `yaml:"updated"`
	Providers map[string]ProviderPricing `yaml:"providers"`
	Cache     CacheRatios               `yaml:"cache"`
	Tasks     map[string]TaskDefaults   `yaml:"tasks"`
}

// GetModelPrice returns the ModelPrice for a given provider and model. It
// returns an error if the provider or model is unknown (spec §11: ESTIMATION_FAILED).
func (p *PricingYAML) GetModelPrice(provider, model string) (ModelPrice, error) {
	if p == nil || p.Providers == nil {
		return ModelPrice{}, fmt.Errorf("no providers loaded")
	}
	pp, ok := p.Providers[provider]
	if !ok {
		return ModelPrice{}, fmt.Errorf("unknown provider %q", provider)
	}
	if mp, ok := pp.Models[model]; ok {
		return mp, nil
	}
	return ModelPrice{}, fmt.Errorf("unknown model %q for provider %q", model, provider)
}

// GetTaskDefaults returns the token profile for a task type. Unknown task types
// fall back to the "code" profile if present, otherwise an error.
func (p *PricingYAML) GetTaskDefaults(t TaskType) (TaskDefaults, error) {
	if p == nil || p.Tasks == nil {
		return TaskDefaults{}, fmt.Errorf("no task profiles loaded")
	}
	if td, ok := p.Tasks[string(t)]; ok {
		return applyTaskDefaults(td), nil
	}
	if td, ok := p.Tasks[string(TaskCode)]; ok {
		return applyTaskDefaults(td), nil
	}
	return TaskDefaults{}, fmt.Errorf("no token profile for task type %q", t)
}

// applyTaskDefaults fills in the adjustment-factor defaults (spec §7.2) when
// the YAML omits them, so a minimal pricing file still works.
func applyTaskDefaults(td TaskDefaults) TaskDefaults {
	if td.InputPerFile == 0 {
		td.InputPerFile = 5000
	}
	if td.TokensPerIter == 0 {
		td.TokensPerIter = 10000
	}
	if td.TokensPer10Diff == 0 {
		td.TokensPer10Diff = 1
	}
	return td
}

// Validate performs basic structural checks on loaded pricing data so the
// estimator fails fast (spec §4) rather than producing nonsense estimates.
func (p *PricingYAML) Validate() error {
	if p == nil {
		return fmt.Errorf("pricing data is nil")
	}
	if len(p.Providers) == 0 {
		return fmt.Errorf("no providers defined in pricing data")
	}
	if len(p.Tasks) == 0 {
		return fmt.Errorf("no task profiles defined in pricing data")
	}
	// Sanity-check cache ratios are within [0,1].
	for label, r := range map[string]float64{
		"pro_hit_ratio": p.Cache.ProHitRatio, "flash_hit_ratio": p.Cache.FlashHitRatio,
		"new_repo_hit_ratio": p.Cache.NewRepoHitRatio,
	} {
		if r < 0 || r > 1 {
			return fmt.Errorf("cache ratio %q = %v is out of range [0,1]", label, r)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Loader
// ---------------------------------------------------------------------------

// LoadPricing reads and parses a pricing.yaml file, applies default adjustment
// factors, and validates the result. The path is typically
// ~/.helix/pricing.yaml in production or a testdata fixture in tests.
func LoadPricing(path string) (*PricingYAML, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read pricing file %q: %w", path, err)
	}
	var p PricingYAML
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse pricing yaml %q: %w", path, err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("invalid pricing data in %q: %w", path, err)
	}
	// Normalize all task profiles with default adjustment factors.
	for k, td := range p.Tasks {
		p.Tasks[k] = applyTaskDefaults(td)
	}
	return &p, nil
}
