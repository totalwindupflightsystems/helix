// Package config provides platform-wide configuration loading for Helix CLI
// tools. It implements the configuration model from specs/helix-config.md
// including the ~/.helix/config.yaml schema, defaults, validation, and the
// 5-tier configuration loading order (defaults → file → pricing → env → flags).
package config

import "fmt"

// Config is the top-level Helix platform configuration. It maps directly to
// ~/.helix/config.yaml.
type Config struct {
	Version      int          `yaml:"version"`
	Forgejo      ForgejoConfig `yaml:"forgejo"`
	Chimera      ChimeraConfig `yaml:"chimera"`
	LangFuse     LangFuseConfig `yaml:"langfuse"`
	GitReins     GitReinsConfig `yaml:"gitreins"`
	Identity     IdentityConfig `yaml:"identity"`
	Estimator    EstimatorConfig `yaml:"estimator"`
	Marketplace  MarketplaceConfig `yaml:"marketplace"`
	Negotiation  NegotiationConfig `yaml:"negotiation"`
	Prompts      PromptsConfig `yaml:"prompts"`
	Services     ServicesConfig `yaml:"services"`
	Budget       BudgetConfig `yaml:"budget"`
}

// ForgejoConfig holds the Forgejo forge connection parameters.
type ForgejoConfig struct {
	URL         string `yaml:"url"`
	InternalURL string `yaml:"internal_url"`
	AdminUser   string `yaml:"admin_user"`
}

// ChimeraConfig holds the Chimera multi-model review service parameters.
type ChimeraConfig struct {
	URL               string `yaml:"url"`
	InternalURL       string `yaml:"internal_url"`
	DefaultFormation  string `yaml:"default_formation"`
	ArbiterFormation  string `yaml:"arbiter_formation"`
	BudgetFormation   string `yaml:"budget_formation"`
	Timeout           string `yaml:"timeout"`
}

// LangFuseConfig holds the LangFuse observability service parameters.
type LangFuseConfig struct {
	URL         string `yaml:"url"`
	InternalURL string `yaml:"internal_url"`
	Enabled     bool   `yaml:"enabled"`
}

// GitReinsConfig holds the GitReins quality gate parameters.
type GitReinsConfig struct {
	Model         string `yaml:"model"`
	Provider      string `yaml:"provider"`
	MaxIterations int    `yaml:"max_iterations"`
	MaxTime       string `yaml:"max_time"`
	TestMode      string `yaml:"test_mode"`
}

// IdentityConfig holds the Agent Identity (Feature 1) parameters.
type IdentityConfig struct {
	KnownFriendsPath string `yaml:"known_friends_path"`
	SSHKeyDir       string `yaml:"ssh_key_dir"`
	StatePath       string `yaml:"state_path"`
}

// EstimatorConfig holds the Cost Estimator (Feature 2) parameters.
type EstimatorConfig struct {
	PricingPath       string  `yaml:"pricing_path"`
	CacheHitRatioPro  float64 `yaml:"cache_hit_ratio_pro"`
	CacheHitRatioFlash float64 `yaml:"cache_hit_ratio_flash"`
	BudgetResetDay    string  `yaml:"budget_reset_day"`
	BudgetResetTime   string  `yaml:"budget_reset_time"`
}

// MarketplaceConfig holds the Agent Marketplace (Feature 5) parameters.
type MarketplaceConfig struct {
	RegistryPath        string `yaml:"registry_path"`
	AutoDeprecationDays int    `yaml:"auto_deprecation_days"`
	TrustRecalcSchedule string `yaml:"trust_recalc_schedule"`
}

// NegotiationConfig holds the PR Negotiation (Feature 3) parameters.
type NegotiationConfig struct {
	MaxRounds      int    `yaml:"max_rounds"`
	RoundTimeout   string `yaml:"round_timeout"`
	GlobalTimeout  string `yaml:"global_timeout"`
	TranscriptDir  string `yaml:"transcript_dir"`
}

// PromptsConfig holds the Prompt Registry (Feature 4) parameters.
type PromptsConfig struct {
	RegistryPath         string `yaml:"registry_path"`
	DeprecatedGraceDays  int    `yaml:"deprecated_grace_days"`
	RetiredAfterDays     int    `yaml:"retired_after_days"`
}

// ServiceTarget defines a health-check target for a platform service.
type ServiceTarget struct {
	URL             string `yaml:"url"`
	HealthEndpoint  string `yaml:"health_endpoint"`
}

// ServicesConfig holds health-check targets for all platform services.
type ServicesConfig struct {
	Forgejo  ServiceTarget `yaml:"forgejo"`
	Chimera  ServiceTarget `yaml:"chimera"`
	LangFuse ServiceTarget `yaml:"langfuse"`
}

// BudgetConfig holds the platform-wide budget parameters.
type BudgetConfig struct {
	DefaultWeeklyUSD    BudgetTierConfig `yaml:"default_weekly_usd"`
	OveragePolicy       string           `yaml:"overage_policy"`
	EscalationThreshold float64          `yaml:"escalation_threshold"`
}

// BudgetTierConfig holds per-tier budget caps.
type BudgetTierConfig struct {
	Pro   float64 `yaml:"pro"`
	Flash float64 `yaml:"flash"`
}

// Validate runs all configuration validation checks. It returns an error
// describing the first validation failure, or nil if the config is valid.
func (c *Config) Validate() error {
	if c.Forgejo.URL == "" {
		return fmt.Errorf("forgejo.url is required")
	}
	if c.Chimera.URL == "" {
		return fmt.Errorf("chimera.url is required")
	}
	if c.Budget.OveragePolicy != "" &&
		c.Budget.OveragePolicy != "block" &&
		c.Budget.OveragePolicy != "warn" {
		return fmt.Errorf("budget.overage_policy must be 'block' or 'warn', got %q", c.Budget.OveragePolicy)
	}
	if c.Estimator.CacheHitRatioPro < 0 || c.Estimator.CacheHitRatioPro > 1 {
		return fmt.Errorf("estimator.cache_hit_ratio_pro must be between 0 and 1, got %v", c.Estimator.CacheHitRatioPro)
	}
	if c.Estimator.CacheHitRatioFlash < 0 || c.Estimator.CacheHitRatioFlash > 1 {
		return fmt.Errorf("estimator.cache_hit_ratio_flash must be between 0 and 1, got %v", c.Estimator.CacheHitRatioFlash)
	}
	if c.Version != 0 && c.Version != 1 {
		return fmt.Errorf("config version must be 1, got %d", c.Version)
	}
	return nil
}
