package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Load reads the Helix configuration from the given path and returns it merged
// with defaults. In the full 5-tier system this is tier 2 (config.yaml) — the
// caller is responsible for any additional merges from pricing, env vars, and
// CLI flags.
func Load(path string) (*Config, error) {
	cfg := Defaults()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("config: cannot read %s: %w", path, err)
	}
	var fileCfg Config
	if err := yaml.Unmarshal(data, &fileCfg); err != nil {
		return nil, fmt.Errorf("config: invalid YAML in %s: %w", path, err)
	}
	// Shallow merge: file values override defaults (non-zero semantics).
	cfg.Merge(&fileCfg)
	return cfg, nil
}

// Merge overlays non-zero values from src onto dst. Only fields that differ
// from their zero value in src are copied. This implements the "later
// overrides earlier" merge strategy used in the 5-tier loading order.
func (dst *Config) Merge(src *Config) {
	if src == nil {
		return
	}
	if src.Forgejo.URL != "" {
		dst.Forgejo = src.Forgejo
	}
	if src.Chimera.URL != "" {
		dst.Chimera = src.Chimera
	}
	if src.LangFuse.URL != "" {
		dst.LangFuse = src.LangFuse
	}
	if src.GitReins.Model != "" {
		dst.GitReins = src.GitReins
	}
	if src.Identity.KnownFriendsPath != "" {
		dst.Identity = src.Identity
	}
	if src.Estimator.PricingPath != "" {
		dst.Estimator = src.Estimator
	}
	if src.Marketplace.RegistryPath != "" {
		dst.Marketplace = src.Marketplace
	}
	if src.Negotiation.MaxRounds != 0 {
		dst.Negotiation = src.Negotiation
	}
	if src.Prompts.RegistryPath != "" {
		dst.Prompts = src.Prompts
	}
	if src.Budget.OveragePolicy != "" {
		dst.Budget = src.Budget
	}
	// Secrets.Provider is the discriminator — any explicit provider
	// (including "env") means the operator took control of the section,
	// so we copy the whole block.
	if src.Secrets.Provider != "" {
		dst.Secrets = src.Secrets
	}
}
