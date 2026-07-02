package config

import (
	"testing"
)

func validConfig() *Config {
	return &Config{
		Version: 1,
		Forgejo: ForgejoConfig{
			URL:       "http://localhost:3000",
			AdminUser: "helio",
		},
		Chimera: ChimeraConfig{
			URL:              "http://localhost:8765",
			DefaultFormation: "auto",
			Timeout:          "5m",
		},
		LangFuse: LangFuseConfig{
			Enabled: false,
		},
		GitReins: GitReinsConfig{
			TestMode:      "full",
			MaxIterations: 50,
			MaxTime:       "10m",
		},
		Estimator: EstimatorConfig{
			CacheHitRatioPro:   0.3,
			CacheHitRatioFlash: 0.5,
		},
		Marketplace: MarketplaceConfig{
			RegistryPath:        "/tmp/marketplace",
			AutoDeprecationDays: 30,
		},
		Negotiation: NegotiationConfig{
			MaxRounds:     3,
			RoundTimeout:  "5m",
			GlobalTimeout: "30m",
		},
		Prompts: PromptsConfig{
			RegistryPath:        "prompts/",
			DeprecatedGraceDays: 30,
		},
		Services: ServicesConfig{
			Forgejo: ServiceTarget{URL: "http://localhost:3000"},
		},
		Budget: BudgetConfig{
			OveragePolicy:       "block",
			EscalationThreshold: 0.8,
			DefaultWeeklyUSD:    BudgetTierConfig{Pro: 100, Flash: 10},
		},
	}
}

func TestValidateAll_ValidConfig(t *testing.T) {
	cfg := validConfig()
	errors := cfg.ValidateAll()
	if errors.HasErrors() {
		t.Errorf("valid config should have no errors, got: %v", errors)
	}
}

func TestValidateAll_MissingForgejoURL(t *testing.T) {
	cfg := validConfig()
	cfg.Forgejo.URL = ""
	errors := cfg.ValidateAll()

	found := false
	for _, e := range errors {
		if e.Section == "forgejo" && e.Field == "url" && e.Severity != SeverityWarning {
			found = true
		}
	}
	if !found {
		t.Error("expected error for missing forgejo.url")
	}
}

func TestValidateAll_MissingChimeraURL(t *testing.T) {
	cfg := validConfig()
	cfg.Chimera.URL = ""
	errors := cfg.ValidateAll()

	found := false
	for _, e := range errors {
		if e.Section == "chimera" && e.Field == "url" {
			found = true
		}
	}
	if !found {
		t.Error("expected error for missing chimera.url")
	}
}

func TestValidateAll_LangFuseEnabledNoURL(t *testing.T) {
	cfg := validConfig()
	cfg.LangFuse.Enabled = true
	cfg.LangFuse.URL = ""
	errors := cfg.ValidateAll()

	found := false
	for _, e := range errors {
		if e.Section == "langfuse" && e.Field == "url" {
			found = true
		}
	}
	if !found {
		t.Error("expected error for langfuse enabled without URL")
	}
}

func TestValidateAll_LangFuseDisabledNoURL(t *testing.T) {
	cfg := validConfig()
	cfg.LangFuse.Enabled = false
	cfg.LangFuse.URL = ""
	errors := cfg.ValidateAll()

	for _, e := range errors {
		if e.Section == "langfuse" {
			t.Errorf("langfuse disabled should not produce errors, got: %v", e)
		}
	}
}

func TestValidateAll_InvalidTestMode(t *testing.T) {
	cfg := validConfig()
	cfg.GitReins.TestMode = "invalid"
	errors := cfg.ValidateAll()

	found := false
	for _, e := range errors {
		if e.Section == "gitreins" && e.Field == "test_mode" {
			found = true
		}
	}
	if !found {
		t.Error("expected error for invalid gitreins.test_mode")
	}
}

func TestValidateAll_LowMaxIterations(t *testing.T) {
	cfg := validConfig()
	cfg.GitReins.MaxIterations = 5
	errors := cfg.ValidateAll()

	found := false
	for _, e := range errors {
		if e.Section == "gitreins" && e.Field == "max_iterations" && e.Severity == SeverityWarning {
			found = true
		}
	}
	if !found {
		t.Error("expected warning for low gitreins.max_iterations")
	}
}

func TestValidateAll_InvalidBudgetResetDay(t *testing.T) {
	cfg := validConfig()
	cfg.Estimator.BudgetResetDay = "funday"
	errors := cfg.ValidateAll()

	found := false
	for _, e := range errors {
		if e.Section == "estimator" && e.Field == "budget_reset_day" {
			found = true
		}
	}
	if !found {
		t.Error("expected error for invalid budget_reset_day")
	}
}

func TestValidateAll_ValidBudgetResetDay(t *testing.T) {
	cfg := validConfig()
	cfg.Estimator.BudgetResetDay = "sunday"
	errors := cfg.ValidateAll()

	for _, e := range errors {
		if e.Section == "estimator" && e.Field == "budget_reset_day" {
			t.Errorf("valid budget_reset_day should not produce error, got: %v", e)
		}
	}
}

func TestValidateAll_NegotiationMaxRoundsExceeded(t *testing.T) {
	cfg := validConfig()
	cfg.Negotiation.MaxRounds = 15
	errors := cfg.ValidateAll()

	found := false
	for _, e := range errors {
		if e.Section == "negotiation" && e.Field == "max_rounds" {
			found = true
		}
	}
	if !found {
		t.Error("expected error for negotiation.max_rounds > 10")
	}
}

func TestValidateAll_InvalidOveragePolicy(t *testing.T) {
	cfg := validConfig()
	cfg.Budget.OveragePolicy = "ignore"
	errors := cfg.ValidateAll()

	found := false
	for _, e := range errors {
		if e.Section == "budget" && e.Field == "overage_policy" {
			found = true
		}
	}
	if !found {
		t.Error("expected error for invalid budget.overage_policy")
	}
}

func TestValidateAll_NegativeBudget(t *testing.T) {
	cfg := validConfig()
	cfg.Budget.DefaultWeeklyUSD.Pro = -10
	errors := cfg.ValidateAll()

	found := false
	for _, e := range errors {
		if e.Section == "budget" && e.Field == "default_weekly_usd.pro" {
			found = true
		}
	}
	if !found {
		t.Error("expected error for negative budget")
	}
}

func TestValidateAll_EscalationThresholdOutOfRange(t *testing.T) {
	cfg := validConfig()
	cfg.Budget.EscalationThreshold = 1.5
	errors := cfg.ValidateAll()

	found := false
	for _, e := range errors {
		if e.Section == "budget" && e.Field == "escalation_threshold" {
			found = true
		}
	}
	if !found {
		t.Error("expected error for escalation_threshold > 1")
	}
}

func TestValidateAll_AggressiveDeprecation(t *testing.T) {
	cfg := validConfig()
	cfg.Marketplace.AutoDeprecationDays = 3
	errors := cfg.ValidateAll()

	found := false
	for _, e := range errors {
		if e.Section == "marketplace" && e.Field == "auto_deprecation_days" && e.Severity == SeverityWarning {
			found = true
		}
	}
	if !found {
		t.Error("expected warning for aggressive auto_deprecation_days")
	}
}

func TestValidateAll_InvalidDuration(t *testing.T) {
	cfg := validConfig()
	cfg.Chimera.Timeout = "not-a-duration"
	errors := cfg.ValidateAll()

	found := false
	for _, e := range errors {
		if e.Section == "chimera" && e.Field == "timeout" {
			found = true
		}
	}
	if !found {
		t.Error("expected error for invalid chimera.timeout")
	}
}

func TestValidateAll_ValidDuration(t *testing.T) {
	durations := []string{"5m", "30s", "2h", "1h30m", "500ms", "10s"}
	for _, d := range durations {
		cfg := validConfig()
		cfg.Chimera.Timeout = d
		errors := cfg.ValidateAll()
		for _, e := range errors {
			if e.Section == "chimera" && e.Field == "timeout" {
				t.Errorf("valid duration %q should not produce error: %v", d, e)
			}
		}
	}
}

func TestValidateAll_MultipleErrors(t *testing.T) {
	cfg := &Config{
		Version:   2,
		Forgejo:   ForgejoConfig{},
		Chimera:   ChimeraConfig{},
		Budget:    BudgetConfig{OveragePolicy: "invalid"},
		Estimator: EstimatorConfig{CacheHitRatioPro: 1.5},
	}
	errors := cfg.ValidateAll()
	if len(errors) < 4 {
		t.Errorf("expected at least 4 errors, got %d", len(errors))
	}
	if !errors.HasErrors() {
		t.Error("expected HasErrors to be true")
	}
}

func TestValidateAll_OnlyWarnings(t *testing.T) {
	cfg := validConfig()
	cfg.GitReins.MaxIterations = 5
	errors := cfg.ValidateAll()

	if errors.HasErrors() {
		t.Error("expected no errors, only warnings")
	}
	if !errors.HasWarnings() {
		t.Error("expected warnings")
	}
}

func TestValidateAll_NoErrorsNoWarnings(t *testing.T) {
	cfg := validConfig()
	errors := cfg.ValidateAll()

	if errors.HasErrors() {
		t.Error("expected no errors")
	}
	if errors.HasWarnings() {
		t.Error("expected no warnings for clean config")
	}
}

func TestConfigErrors_ErrorMessages(t *testing.T) {
	cfg := &Config{
		Forgejo: ForgejoConfig{},
		Chimera: ChimeraConfig{},
	}
	errors := cfg.ValidateAll()
	messages := errors.ErrorMessages()

	if len(messages) == 0 {
		t.Fatal("expected at least 1 message")
	}
	for _, msg := range messages {
		if msg == "" {
			t.Error("message should not be empty")
		}
	}
}

func TestConfigErrors_FormatErrors(t *testing.T) {
	cfg := validConfig()
	errors := cfg.ValidateAll()
	output := errors.FormatErrors()
	if output != "Configuration valid" {
		t.Errorf("expected 'Configuration valid', got %q", output)
	}

	cfg.Forgejo.URL = ""
	errors = cfg.ValidateAll()
	output = errors.FormatErrors()
	if output == "Configuration valid" {
		t.Error("expected non-valid output for invalid config")
	}
}

func TestConfigError_Error(t *testing.T) {
	err := ConfigError{
		Section:  "test",
		Field:    "field",
		Message:  "test message",
		Severity: SeverityError,
	}
	if err.Error() != "test message" {
		t.Errorf("expected 'test message', got %q", err.Error())
	}
}

func TestIsValidDurationString(t *testing.T) {
	valid := []string{"5m", "30s", "2h", "1h30m", "500ms", "10s", "1.5h", "100ns"}
	for _, d := range valid {
		if !isValidDurationString(d) {
			t.Errorf("expected %q to be valid", d)
		}
	}

	invalid := []string{"", "abc", "5", "m", "5x", "--", "not-a-duration"}
	for _, d := range invalid {
		if isValidDurationString(d) {
			t.Errorf("expected %q to be invalid", d)
		}
	}
}
