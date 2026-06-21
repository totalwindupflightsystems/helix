package estimate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// =============================================================================
// ModelPrice methods
// =============================================================================

func TestModelPrice_IsCacheSupported(t *testing.T) {
	tests := []struct {
		name string
		mp   ModelPrice
		want bool
	}{
		{
			name: "cache supported (cache read set)",
			mp:   ModelPrice{InputPer1K: 0.1, CacheReadPer1K: ptr(0.01), OutputPer1K: 0.2},
			want: true,
		},
		{
			name: "cache not supported (nil)",
			mp:   ModelPrice{InputPer1K: 0.1, OutputPer1K: 0.2},
			want: false,
		},
		{
			name: "zero value — no cache",
			mp:   ModelPrice{},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mp.IsCacheSupported(); got != tt.want {
				t.Errorf("IsCacheSupported() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestModelPrice_GetCacheReadPrice(t *testing.T) {
	tests := []struct {
		name string
		mp   ModelPrice
		want float64
	}{
		{
			name: "has cache read price",
			mp:   ModelPrice{InputPer1K: 0.1, CacheReadPer1K: ptr(0.014), OutputPer1K: 0.2},
			want: 0.014,
		},
		{
			name: "no cache — falls back to input price",
			mp:   ModelPrice{InputPer1K: 0.1, OutputPer1K: 0.2},
			want: 0.1,
		},
		{
			name: "zero values",
			mp:   ModelPrice{},
			want: 0.0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mp.GetCacheReadPrice(); got != tt.want {
				t.Errorf("GetCacheReadPrice() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestModelPrice_GetCacheWritePrice(t *testing.T) {
	tests := []struct {
		name string
		mp   ModelPrice
		want float64
	}{
		{
			name: "has cache write price",
			mp:   ModelPrice{InputPer1K: 0.1, CacheWritePer1K: ptr(0.0375), OutputPer1K: 0.2},
			want: 0.0375,
		},
		{
			name: "no cache write — falls back to input price",
			mp:   ModelPrice{InputPer1K: 0.1, OutputPer1K: 0.2},
			want: 0.1,
		},
		{
			name: "zero values",
			mp:   ModelPrice{},
			want: 0.0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mp.GetCacheWritePrice(); got != tt.want {
				t.Errorf("GetCacheWritePrice() = %v, want %v", got, tt.want)
			}
		})
	}
}

// =============================================================================
// GetModelPrice
// =============================================================================

func TestPricingYAML_GetModelPrice(t *testing.T) {
	p := &PricingYAML{
		Providers: map[string]ProviderPricing{
			"deepseek": {
				Models: map[string]ModelPrice{
					"deepseek-v4-pro": {InputPer1K: 0.14, CacheReadPer1K: ptr(0.014), OutputPer1K: 0.28},
				},
			},
			"anthropic": {
				Models: map[string]ModelPrice{
					"claude-sonnet-4": {InputPer1K: 3.0, CacheReadPer1K: ptr(0.3), CacheWritePer1K: ptr(3.75), OutputPer1K: 15.0},
				},
			},
			"mini": {MarkupPercent: 5}, // no models
		},
	}

	tests := []struct {
		name     string
		p        *PricingYAML
		provider string
		model    string
		wantOK   bool
		errSub   string
	}{
		{
			name:     "valid provider and model",
			p:        p,
			provider: "deepseek",
			model:    "deepseek-v4-pro",
			wantOK:   true,
		},
		{
			name:     "valid anthropic with cache write",
			p:        p,
			provider: "anthropic",
			model:    "claude-sonnet-4",
			wantOK:   true,
		},
		{
			name:     "nil pricing",
			p:        nil,
			provider: "deepseek",
			model:    "deepseek-v4-pro",
			errSub:   "no providers loaded",
		},
		{
			name:     "nil providers map",
			p:        &PricingYAML{},
			provider: "deepseek",
			model:    "deepseek-v4-pro",
			errSub:   "no providers loaded",
		},
		{
			name:     "unknown provider",
			p:        p,
			provider: "nonexistent",
			model:    "deepseek-v4-pro",
			errSub:   "unknown provider",
		},
		{
			name:     "unknown model for known provider",
			p:        p,
			provider: "deepseek",
			model:    "nonexistent-model",
			errSub:   "unknown model",
		},
		{
			name:     "provider has no models",
			p:        p,
			provider: "mini",
			model:    "any-model",
			errSub:   "unknown model",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.p.GetModelPrice(tt.provider, tt.model)
			if tt.wantOK {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got.InputPer1K == 0 {
					t.Errorf("expected non-zero price")
				}
			} else {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errSub)
				}
				if !strings.Contains(err.Error(), tt.errSub) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errSub)
				}
			}
		})
	}
}

// =============================================================================
// GetTaskDefaults
// =============================================================================

func TestPricingYAML_GetTaskDefaults(t *testing.T) {
	p := &PricingYAML{
		Tasks: map[string]TaskDefaults{
			string(TaskSpec):     {InputTokens: 80000, OutputRatio: 2.0, MaxIterations: 5},
			string(TaskCode):     {InputTokens: 120000, OutputRatio: 0.8, MaxIterations: 20},
			string(TaskTest):     {InputTokens: 30000, OutputRatio: 1.0, MaxIterations: 10},
			"custom-task":        {InputTokens: 50000, OutputRatio: 0.5, MaxIterations: 3},
		},
	}

	tests := []struct {
		name    string
		p       *PricingYAML
		t       TaskType
		wantOK  bool
		errSub  string
		check   func(t *testing.T, td TaskDefaults)
	}{
		{
			name:   "known task — spec",
			p:      p,
			t:      TaskSpec,
			wantOK: true,
			check: func(t *testing.T, td TaskDefaults) {
				if td.InputTokens != 80000 {
					t.Errorf("InputTokens = %d, want 80000", td.InputTokens)
				}
				if td.InputPerFile != 5000 {
					t.Errorf("InputPerFile default = %d, want 5000", td.InputPerFile)
				}
			},
		},
		{
			name:   "known task — code (with defaults applied)",
			p:      p,
			t:      TaskCode,
			wantOK: true,
			check: func(t *testing.T, td TaskDefaults) {
				if td.TokensPerIter != 10000 {
					t.Errorf("TokensPerIter default = %d, want 10000", td.TokensPerIter)
				}
				if td.TokensPer10Diff != 1 {
					t.Errorf("TokensPer10Diff default = %d, want 1", td.TokensPer10Diff)
				}
			},
		},
		{
			name:   "fallback to code — unknown task type",
			p:      p,
			t:      TaskType("unknown"),
			wantOK: true,
			check: func(t *testing.T, td TaskDefaults) {
				if td.InputTokens != 120000 {
					t.Errorf("InputTokens fallback = %d, want 120000", td.InputTokens)
				}
			},
		},
		{
			name:   "nil pricing",
			p:      nil,
			t:      TaskCode,
			errSub: "no task profiles",
		},
		{
			name:   "nil tasks map",
			p:      &PricingYAML{},
			t:      TaskCode,
			errSub: "no task profiles",
		},
		{
			name: "unknown type — no code fallback",
			p: &PricingYAML{
				Tasks: map[string]TaskDefaults{
					string(TaskTest): {InputTokens: 30000},
				},
			},
			t:      TaskType("unknown"),
			errSub: "no token profile",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			td, err := tt.p.GetTaskDefaults(tt.t)
			if tt.wantOK {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if tt.check != nil {
					tt.check(t, td)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errSub)
				}
				if !strings.Contains(err.Error(), tt.errSub) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errSub)
				}
			}
		})
	}
}

// =============================================================================
// ApplyTaskDefaults
// =============================================================================

func TestApplyTaskDefaults(t *testing.T) {
	tests := []struct {
		name string
		td   TaskDefaults
		want TaskDefaults
	}{
		{
			name: "all defaults applied — zero values",
			td:   TaskDefaults{InputTokens: 100, OutputRatio: 0.5, MaxIterations: 1},
			want: TaskDefaults{InputTokens: 100, OutputRatio: 0.5, MaxIterations: 1,
				InputPerFile: 5000, TokensPerIter: 10000, TokensPer10Diff: 1},
		},
		{
			name: "no defaults — all already set",
			td:   TaskDefaults{InputTokens: 100, OutputRatio: 0.5, MaxIterations: 1,
				InputPerFile: 3000, TokensPerIter: 8000, TokensPer10Diff: 2},
			want: TaskDefaults{InputTokens: 100, OutputRatio: 0.5, MaxIterations: 1,
				InputPerFile: 3000, TokensPerIter: 8000, TokensPer10Diff: 2},
		},
		{
			name: "partial defaults — only input_per_file zero",
			td:   TaskDefaults{InputTokens: 100, OutputRatio: 0.5, MaxIterations: 1,
				TokensPerIter: 8000, TokensPer10Diff: 2},
			want: TaskDefaults{InputTokens: 100, OutputRatio: 0.5, MaxIterations: 1,
				InputPerFile: 5000, TokensPerIter: 8000, TokensPer10Diff: 2},
		},
		{
			name: "partial defaults — only tokens_per_iter zero",
			td:   TaskDefaults{InputTokens: 100, OutputRatio: 0.5, MaxIterations: 1,
				InputPerFile: 3000, TokensPer10Diff: 2},
			want: TaskDefaults{InputTokens: 100, OutputRatio: 0.5, MaxIterations: 1,
				InputPerFile: 3000, TokensPerIter: 10000, TokensPer10Diff: 2},
		},
		{
			name: "partial defaults — only tokens_per_10_diff zero",
			td:   TaskDefaults{InputTokens: 100, OutputRatio: 0.5, MaxIterations: 1,
				InputPerFile: 3000, TokensPerIter: 8000},
			want: TaskDefaults{InputTokens: 100, OutputRatio: 0.5, MaxIterations: 1,
				InputPerFile: 3000, TokensPerIter: 8000, TokensPer10Diff: 1},
		},
		{
			name: "all fields zero — output defaults",
			td:   TaskDefaults{},
			want: TaskDefaults{InputPerFile: 5000, TokensPerIter: 10000, TokensPer10Diff: 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyTaskDefaults(tt.td)
			if got != tt.want {
				t.Errorf("applyTaskDefaults() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// =============================================================================
// Validate
// =============================================================================

func TestPricingYAML_Validate(t *testing.T) {
	validPricing := &PricingYAML{
		Providers: map[string]ProviderPricing{
			"deepseek": {},
		},
		Tasks: map[string]TaskDefaults{
			"code": {},
		},
		Cache: CacheRatios{
			ProHitRatio:     0.6,
			FlashHitRatio:   0.8,
			NewRepoHitRatio: 0.0,
		},
	}

	tests := []struct {
		name   string
		p      *PricingYAML
		errSub string
	}{
		{
			name: "valid",
			p:    validPricing,
		},
		{
			name:   "nil pricing",
			p:      nil,
			errSub: "nil",
		},
		{
			name:   "no providers",
			p:      &PricingYAML{},
			errSub: "no providers",
		},
		{
			name: "no tasks",
			p: &PricingYAML{
				Providers: map[string]ProviderPricing{"deepseek": {}},
			},
			errSub: "no task profiles",
		},
		{
			name: "pro_hit_ratio out of range — negative",
			p: &PricingYAML{
				Providers: map[string]ProviderPricing{"deepseek": {}},
				Tasks:     map[string]TaskDefaults{"code": {}},
				Cache:     CacheRatios{ProHitRatio: -0.1, FlashHitRatio: 0.8, NewRepoHitRatio: 0.0},
			},
			errSub: "out of range",
		},
		{
			name: "pro_hit_ratio out of range — > 1",
			p: &PricingYAML{
				Providers: map[string]ProviderPricing{"deepseek": {}},
				Tasks:     map[string]TaskDefaults{"code": {}},
				Cache:     CacheRatios{ProHitRatio: 1.2, FlashHitRatio: 0.8, NewRepoHitRatio: 0.0},
			},
			errSub: "out of range",
		},
		{
			name: "flash_hit_ratio out of range — negative",
			p: &PricingYAML{
				Providers: map[string]ProviderPricing{"deepseek": {}},
				Tasks:     map[string]TaskDefaults{"code": {}},
				Cache:     CacheRatios{ProHitRatio: 0.6, FlashHitRatio: -0.1, NewRepoHitRatio: 0.0},
			},
			errSub: "out of range",
		},
		{
			name: "new_repo_hit_ratio out of range — > 1",
			p: &PricingYAML{
				Providers: map[string]ProviderPricing{"deepseek": {}},
				Tasks:     map[string]TaskDefaults{"code": {}},
				Cache:     CacheRatios{ProHitRatio: 0.6, FlashHitRatio: 0.8, NewRepoHitRatio: 1.5},
			},
			errSub: "out of range",
		},
		{
			name: "ratios at boundary — 0 and 1 are ok",
			p: &PricingYAML{
				Providers: map[string]ProviderPricing{"deepseek": {}},
				Tasks:     map[string]TaskDefaults{"code": {}},
				Cache:     CacheRatios{ProHitRatio: 0.0, FlashHitRatio: 1.0, NewRepoHitRatio: 0.0},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.p.Validate()
			if tt.errSub == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errSub)
				}
				if !strings.Contains(err.Error(), tt.errSub) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errSub)
				}
			}
		})
	}
}

// =============================================================================
// LoadPricing
// =============================================================================

func TestLoadPricing(t *testing.T) {
	t.Run("valid fixture", func(t *testing.T) {
		p, err := LoadPricing("testdata/pricing.yaml")
		if err != nil {
			t.Fatalf("LoadPricing: %v", err)
		}
		if p == nil {
			t.Fatal("expected non-nil")
		}
		// Verify defaults were applied to task profiles
		for _, td := range p.Tasks {
			if td.InputPerFile == 0 {
				t.Error("expected InputPerFile default to be applied")
			}
			if td.TokensPerIter == 0 {
				t.Error("expected TokensPerIter default to be applied")
			}
			if td.TokensPer10Diff == 0 {
				t.Error("expected TokensPer10Diff default to be applied")
			}
		}
		// Verify cache ratios loaded
		if p.Cache.ProHitRatio != 0.6 {
			t.Errorf("ProHitRatio = %v, want 0.6", p.Cache.ProHitRatio)
		}
		// Verify providers loaded
		if len(p.Providers) == 0 {
			t.Error("expected providers")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := LoadPricing("testdata/does-not-exist.yaml")
		if err == nil {
			t.Fatal("expected error for missing file")
		}
		if !strings.Contains(err.Error(), "read pricing file") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("invalid yaml", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.yaml")
		// Write invalid YAML
		if err := os.WriteFile(path, []byte(": bad yaml: ["), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadPricing(path)
		if err == nil {
			t.Fatal("expected error for invalid YAML")
		}
		if !strings.Contains(err.Error(), "parse pricing yaml") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("valid yaml but fails validation", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "invalid-pricing.yaml")
		// Valid YAML but missing providers and tasks
		data, err := yaml.Marshal(PricingYAML{Version: "1"})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
		_, err = LoadPricing(path)
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), "no providers") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("minimal valid yaml", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "pricing.yaml")
		data, err := yaml.Marshal(PricingYAML{
			Version:   "1",
			Updated:   "2026-06-21",
			Providers: map[string]ProviderPricing{"mini": {}},
			Tasks:     map[string]TaskDefaults{"code": {InputTokens: 100, OutputRatio: 0.5, MaxIterations: 1}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
		p, err := LoadPricing(path)
		if err != nil {
			t.Fatalf("LoadPricing: %v", err)
		}
		if p == nil {
			t.Fatal("expected non-nil")
		}
		// Verify defaults applied to task
		td := p.Tasks["code"]
		if td.InputPerFile != 5000 {
			t.Errorf("InputPerFile = %d, want 5000", td.InputPerFile)
		}
		if td.TokensPerIter != 10000 {
			t.Errorf("TokensPerIter = %d, want 10000", td.TokensPerIter)
		}
		if td.TokensPer10Diff != 1 {
			t.Errorf("TokensPer10Diff = %d, want 1", td.TokensPer10Diff)
		}
	})

	t.Run("verify fixture model specifics", func(t *testing.T) {
		p, err := LoadPricing("testdata/pricing.yaml")
		if err != nil {
			t.Fatal(err)
		}

		// DeepSeek: cache supported, both read and write
		ds, err := p.GetModelPrice("deepseek", "deepseek-v4-pro")
		if err != nil {
			t.Fatalf("get deepseek: %v", err)
		}
		if !ds.IsCacheSupported() {
			t.Error("DeepSeek should have cache support")
		}
		if ds.GetCacheReadPrice() != 0.000014 {
			t.Errorf("DeepSeek cache read = %v, want 0.000014", ds.GetCacheReadPrice())
		}
		// DeepSeek has no explicit cache write price — falls back to input
		if ds.GetCacheWritePrice() != 0.00014 {
			t.Errorf("DeepSeek cache write = %v, want 0.00014 (input fallback)", ds.GetCacheWritePrice())
		}

		// MiniMax: no cache at all (cache_read_per_1k: null)
		mm, err := p.GetModelPrice("minimax", "MiniMax-M3")
		if err != nil {
			t.Fatalf("get minimax: %v", err)
		}
		if mm.IsCacheSupported() {
			t.Error("MiniMax should NOT have cache support")
		}
		if mm.GetCacheReadPrice() != 0.00020 {
			t.Errorf("MiniMax cache read fallback = %v, want 0.00020", mm.GetCacheReadPrice())
		}
		if mm.GetCacheWritePrice() != 0.00020 {
			t.Errorf("MiniMax cache write fallback = %v, want 0.00020", mm.GetCacheWritePrice())
		}

		// Anthropic: has separate cache write price
		ant, err := p.GetModelPrice("anthropic", "claude-sonnet-4")
		if err != nil {
			t.Fatalf("get anthropic: %v", err)
		}
		if ant.GetCacheWritePrice() != 0.00375 {
			t.Errorf("Anthropic cache write = %v, want 0.00375", ant.GetCacheWritePrice())
		}
		if ant.GetCacheReadPrice() != 0.00030 {
			t.Errorf("Anthropic cache read = %v, want 0.00030", ant.GetCacheReadPrice())
		}

		// GLM: has cache read but no explicit cache write
		glm, err := p.GetModelPrice("zai-glm", "glm-5.2")
		if err != nil {
			t.Fatalf("get glm: %v", err)
		}
		if !glm.IsCacheSupported() {
			t.Error("GLM should have cache support")
		}
		if glm.GetCacheReadPrice() != 0.000010 {
			t.Errorf("GLM cache read = %v, want 0.000010", glm.GetCacheReadPrice())
		}
		if glm.GetCacheWritePrice() != 0.00010 {
			t.Errorf("GLM cache write fallback = %v, want 0.00010", glm.GetCacheWritePrice())
		}
	})
}

// =============================================================================
// TaskType.Valid
// =============================================================================

func TestTaskType_Valid(t *testing.T) {
	tests := []struct {
		name string
		t    TaskType
		want bool
	}{
		{"spec is valid", TaskSpec, true},
		{"code is valid", TaskCode, true},
		{"review is valid", TaskReview, true},
		{"refactor is valid", TaskRefactor, true},
		{"test is valid", TaskTest, true},
		{"empty string invalid", "", false},
		{"unknown type invalid", TaskType("deploy"), false},
		{"arbitrary string invalid", TaskType("not-a-real-type"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.t.Valid(); got != tt.want {
				t.Errorf("Valid() = %v, want %v", got, tt.want)
			}
		})
	}
}

// =============================================================================
// Helpers
// =============================================================================

func ptr[T any](v T) *T { return &v }
