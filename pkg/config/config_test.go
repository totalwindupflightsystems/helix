package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()

	// Version
	if cfg.Version != 1 {
		t.Errorf("expected version 1, got %d", cfg.Version)
	}

	// Forgejo
	if cfg.Forgejo.URL != "http://localhost:3030" {
		t.Errorf("expected forgejo URL http://localhost:3030, got %s", cfg.Forgejo.URL)
	}
	if cfg.Forgejo.AdminUser != "helio" {
		t.Errorf("expected admin user helio, got %s", cfg.Forgejo.AdminUser)
	}

	// Chimera
	if cfg.Chimera.URL != "http://localhost:8765" {
		t.Errorf("expected chimera URL http://localhost:8765, got %s", cfg.Chimera.URL)
	}
	if cfg.Chimera.DefaultFormation != "standard" {
		t.Errorf("expected default formation standard, got %s", cfg.Chimera.DefaultFormation)
	}

	// GitReins
	if cfg.GitReins.Model != "deepseek-v4-flash" {
		t.Errorf("expected gitreins model deepseek-v4-flash, got %s", cfg.GitReins.Model)
	}
	if cfg.GitReins.MaxIterations != 25 {
		t.Errorf("expected max_iterations 25, got %d", cfg.GitReins.MaxIterations)
	}

	// Estimator
	if cfg.Estimator.CacheHitRatioPro != 0.60 {
		t.Errorf("expected cache_hit_ratio_pro 0.60, got %v", cfg.Estimator.CacheHitRatioPro)
	}
	if cfg.Estimator.CacheHitRatioFlash != 0.80 {
		t.Errorf("expected cache_hit_ratio_flash 0.80, got %v", cfg.Estimator.CacheHitRatioFlash)
	}

	// Negotiation
	if cfg.Negotiation.MaxRounds != 3 {
		t.Errorf("expected max_rounds 3, got %d", cfg.Negotiation.MaxRounds)
	}

	// Budget
	if cfg.Budget.DefaultWeeklyUSD.Pro != 10.00 {
		t.Errorf("expected pro budget 10.00, got %v", cfg.Budget.DefaultWeeklyUSD.Pro)
	}
	if cfg.Budget.OveragePolicy != "block" {
		t.Errorf("expected overage_policy block, got %s", cfg.Budget.OveragePolicy)
	}

	// LangFuse
	if !cfg.LangFuse.Enabled {
		t.Error("expected langfuse enabled true")
	}

	// Prompts
	if cfg.Prompts.RegistryPath != "prompts" {
		t.Errorf("expected registry_path prompts, got %s", cfg.Prompts.RegistryPath)
	}
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg := Defaults()
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config, got error: %v", err)
	}
}

func TestValidate_MissingForgejoURL(t *testing.T) {
	cfg := Defaults()
	cfg.Forgejo.URL = ""
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for missing forgejo URL")
	}
	if err.Error() != "forgejo.url is required" {
		t.Errorf("expected 'forgejo.url is required', got %q", err.Error())
	}
}

func TestValidate_MissingChimeraURL(t *testing.T) {
	cfg := Defaults()
	cfg.Chimera.URL = ""
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for missing chimera URL")
	}
	if err.Error() != "chimera.url is required" {
		t.Errorf("expected 'chimera.url is required', got %q", err.Error())
	}
}

func TestValidate_InvalidOveragePolicy(t *testing.T) {
	cfg := Defaults()
	cfg.Budget.OveragePolicy = "allow"
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for invalid overage policy")
	}
}

func TestValidate_OveragePolicyBlock(t *testing.T) {
	cfg := Defaults()
	cfg.Budget.OveragePolicy = "block"
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected 'block' to be valid, got error: %v", err)
	}
}

func TestValidate_OveragePolicyWarn(t *testing.T) {
	cfg := Defaults()
	cfg.Budget.OveragePolicy = "warn"
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected 'warn' to be valid, got error: %v", err)
	}
}

func TestValidate_EmptyOveragePolicy(t *testing.T) {
	cfg := Defaults()
	cfg.Budget.OveragePolicy = ""
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected empty overage_policy to be valid, got error: %v", err)
	}
}

func TestValidate_InvalidVersion(t *testing.T) {
	cfg := Defaults()
	cfg.Version = 2
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for invalid version")
	}
}

func TestValidate_VersionZero(t *testing.T) {
	cfg := Defaults()
	cfg.Version = 0
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected version 0 to be valid, got error: %v", err)
	}
}

func TestValidate_CacheHitRatioProOutOfRange(t *testing.T) {
	tests := []struct {
		name  string
		value float64
	}{
		{"negative", -0.1},
		{"above_one", 1.1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Defaults()
			cfg.Estimator.CacheHitRatioPro = tt.value
			err := cfg.Validate()
			if err == nil {
				t.Error("expected error for out-of-range cache_hit_ratio_pro")
			}
		})
	}
}

func TestValidate_CacheHitRatioFlashOutOfRange(t *testing.T) {
	tests := []struct {
		name  string
		value float64
	}{
		{"negative", -0.1},
		{"above_one", 1.1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Defaults()
			cfg.Estimator.CacheHitRatioFlash = tt.value
			err := cfg.Validate()
			if err == nil {
				t.Error("expected error for out-of-range cache_hit_ratio_flash")
			}
		})
	}
}

func TestValidate_CacheHitRatioBoundaries(t *testing.T) {
	cfg := Defaults()
	cfg.Estimator.CacheHitRatioPro = 0.0
	cfg.Estimator.CacheHitRatioFlash = 1.0
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected boundaries (0.0, 1.0) to be valid, got error: %v", err)
	}
}

func TestMerge_NilSource(t *testing.T) {
	cfg := Defaults()
	cfg.Merge(nil)
	if cfg.Forgejo.URL != "http://localhost:3030" {
		t.Error("merge with nil should not modify config")
	}
}

func TestMerge_OverridesForgejoURL(t *testing.T) {
	cfg := Defaults()
	src := &Config{
		Forgejo: ForgejoConfig{
			URL: "http://custom-forgejo:4000",
		},
	}
	cfg.Merge(src)
	if cfg.Forgejo.URL != "http://custom-forgejo:4000" {
		t.Errorf("expected forgejo URL override, got %s", cfg.Forgejo.URL)
	}
	// Other fields should stay at defaults
	if cfg.Chimera.URL != "http://localhost:8765" {
		t.Error("merge should not touch unrelated fields")
	}
}

func TestMerge_OverridesChimeraURL(t *testing.T) {
	cfg := Defaults()
	src := &Config{
		Chimera: ChimeraConfig{
			URL: "http://custom-chimera:9000",
		},
	}
	cfg.Merge(src)
	if cfg.Chimera.URL != "http://custom-chimera:9000" {
		t.Errorf("expected chimera URL override, got %s", cfg.Chimera.URL)
	}
}

func TestMerge_OverridesBudget(t *testing.T) {
	cfg := Defaults()
	src := &Config{
		Budget: BudgetConfig{
			OveragePolicy:       "warn",
			EscalationThreshold: 2.0,
		},
	}
	cfg.Merge(src)
	if cfg.Budget.OveragePolicy != "warn" {
		t.Errorf("expected overage_policy warn, got %s", cfg.Budget.OveragePolicy)
	}
	if cfg.Budget.EscalationThreshold != 2.0 {
		t.Errorf("expected escalation_threshold 2.0, got %v", cfg.Budget.EscalationThreshold)
	}
}

func TestMerge_OverridesNegotiation(t *testing.T) {
	cfg := Defaults()
	src := &Config{
		Negotiation: NegotiationConfig{
			MaxRounds: 5,
		},
	}
	cfg.Merge(src)
	if cfg.Negotiation.MaxRounds != 5 {
		t.Errorf("expected max_rounds 5, got %d", cfg.Negotiation.MaxRounds)
	}
}

func TestMerge_DoesNotOverrideWithZeroValues(t *testing.T) {
	cfg := Defaults()
	src := &Config{} // all zero values
	cfg.Merge(src)
	// Defaults should be preserved
	if cfg.Forgejo.URL != "http://localhost:3030" {
		t.Error("merge with zero values should preserve defaults")
	}
}

func TestLoad_EmptyPathReturnsDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("expected no error for empty path, got: %v", err)
	}
	if cfg.Forgejo.URL != "http://localhost:3030" {
		t.Errorf("expected default forgejo URL, got %s", cfg.Forgejo.URL)
	}
}

func TestLoad_MissingFileReturnsDefaults(t *testing.T) {
	cfg, err := Load("/tmp/nonexistent-helix-config.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if cfg.Forgejo.URL != "http://localhost:3030" {
		t.Errorf("expected default forgejo URL, got %s", cfg.Forgejo.URL)
	}
}

func TestLoad_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `version: 1
forgejo:
  url: "http://forgejo-test:3999"
  admin_user: "test-admin"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("expected no error loading valid YAML, got: %v", err)
	}
	if cfg.Forgejo.URL != "http://forgejo-test:3999" {
		t.Errorf("expected forgejo URL from YAML, got %s", cfg.Forgejo.URL)
	}
	if cfg.Forgejo.AdminUser != "test-admin" {
		t.Errorf("expected admin_user from YAML, got %s", cfg.Forgejo.AdminUser)
	}
	// Chimera should still be default
	if cfg.Chimera.URL != "http://localhost:8765" {
		t.Errorf("expected chimera URL from defaults, got %s", cfg.Chimera.URL)
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("forgejo: [malformed: yaml: ["), 0o644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoad_UnreadableFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("forgejo:"), 0o000); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for unreadable file")
	}
}

func TestConfig_AllFieldsDefaultAccessible(t *testing.T) {
	cfg := Defaults()

	// Verify every section is non-nil and has expected defaults
	if cfg.Services.Forgejo.HealthEndpoint != "/api/v1/version" {
		t.Error("services.forgejo.health_endpoint mismatch")
	}
	if cfg.Services.Chimera.HealthEndpoint != "/v1/health" {
		t.Error("services.chimera.health_endpoint mismatch")
	}
	if cfg.Services.LangFuse.HealthEndpoint != "/api/public/health" {
		t.Error("services.langfuse.health_endpoint mismatch")
	}
	if cfg.Marketplace.AutoDeprecationDays != 30 {
		t.Error("marketplace.auto_deprecation_days mismatch")
	}
	if cfg.Prompts.DeprecatedGraceDays != 30 {
		t.Error("prompts.deprecated_grace_days mismatch")
	}
	if cfg.Prompts.RetiredAfterDays != 180 {
		t.Error("prompts.retired_after_days mismatch")
	}
	if cfg.Identity.KnownFriendsPath == "" {
		t.Error("identity.known_friends_path is empty")
	}
}
